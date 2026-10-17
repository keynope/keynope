// Reuse the exact dense gesture assertions against the real native Go HTTP
// engine. Isolated loopback fixture; never launches/activates a desktop app.
const fs=require('node:fs'),path=require('node:path'),cp=require('node:child_process');
const root=fs.mkdtempSync('/tmp/keynope-native-dense-drag-');
(async()=>{
 let logs='',code=null;
 const go=cp.spawn('go',['test','-run','^TestNativeEditingLatencyBrowserFixture$','-count=1','./internal/app'],{env:{...process.env,GOCACHE:process.env.GOCACHE||'/tmp/keynope-go-cache',KEYNOPE_NATIVE_EDITING_EVIDENCE:root,KEYNOPE_NATIVE_LATENCY_COMPARISON:'demand'},stdio:['ignore','pipe','pipe']});
 go.stdout.on('data',b=>logs+=b);go.stderr.on('data',b=>logs+=b);
 const exited=new Promise(resolve=>go.on('exit',value=>{code=value;resolve(value);}));
 try{
  const deadline=Date.now()+90000;
  while(!fs.existsSync(path.join(root,'connection.json'))){
   if(code!==null)throw Error('Native fixture failed: '+logs);
   if(Date.now()>deadline)throw Error('Native fixture startup timed out');
   await new Promise(r=>setTimeout(r,50));
  }
  const {url}=JSON.parse(fs.readFileSync(path.join(root,'connection.json')));
  for(const engine of ['chromium','webkit']){
   const output=path.join(root,engine);
   const status=await new Promise((resolve,reject)=>{
    const child=cp.spawn(process.execPath,[path.join(__dirname,'test_dense_drag.cjs')],{env:{...process.env,NATIVE_EDITOR_URL:url,TEST_BROWSER:engine,TEST_OUTPUT:output},stdio:'inherit'});
    child.on('error',reject);child.on('exit',resolve);
   });
   if(status!==0)throw Error('Native dense drag failed in '+engine+'; artifacts: '+root);
  }
  console.log('PASS native dense gesture transports: '+root);
 }finally{
  fs.writeFileSync(path.join(root,'stop'),'done');
  const result=await Promise.race([exited,new Promise(r=>setTimeout(()=>r('running'),10000))]);
  if(result==='running'){go.kill('SIGTERM');await exited;throw Error('Native fixture did not stop');}
  if(result!==0)throw Error('Native fixture failed: '+logs);
 }
})().catch(error=>{console.error(error);process.exitCode=1;});
