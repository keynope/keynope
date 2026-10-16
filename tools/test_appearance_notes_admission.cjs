// Optional appearance recovery versus strict speaker-note ownership. Actual
// file selection/WASM commands, owned data only, no desktop application.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const {waitForAsync}=require('./wait_for_async.cjs');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site');
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
const appearance=payload=>'<!-- keynope-appearance base64:'+Buffer.from(payload).toString('base64')+' -->';
const notes=text=>'<!-- notes=base64:'+Buffer.from(text).toString('base64')+' -->';
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));const browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
 try{
  const context=await browser.newContext({viewport:{width:1440,height:1000}});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());const page=await context.newPage(),errors=[],alerts=[];page.on('pageerror',e=>errors.push(e.message));let settled=Promise.resolve();page.on('dialog',d=>{if(d.type()==='alert')alerts.push(d.message());settled=d.accept();});
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json())),action=data=>page.evaluate(data=>editorAction(data),data);
  const open=async(name,content)=>{await settled;const picker=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await picker).setFiles({name,mimeType:'text/markdown',buffer:Buffer.from(content)});};
  for(const [name,metadata]of [['duplicate',appearance('{"version":1,"version":2,"mode":"retro"}')],['profile',appearance('{"version":1,"mode":"retro","profiles":{"modern":{"textScale":0.5,"TEXTSCALE":1}}}')],['envelope',appearance('{"version":1,"mode":"modern"}')+'\n<!-- keynope-appearance broken -->']]){
   const file=name+'.md';await open(file,metadata+'\n# Kept '+name+'\n'+notes('Private notes 🦀'));
   await waitForAsync(page,async name=>(await fetch('/api/editor/state').then(r=>r.json())).path.endsWith(name),file);
   const s=await state();assert(s.diagnostics.some(d=>d.code==='appearance-recovered'));assert(s.slides[0].elements.some(e=>e.text==='Kept '+name));assert.equal(s.slides[0].notes,'Private notes 🦀');assert.equal(s.dirty,false);
   const saved=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Saved.md'})}).then(r=>r.json()));assert(saved.content.includes('Kept '+name));assert(!saved.content.includes(metadata));
  }
  await action({action:'add-element',kind:'text'});const before=await state();
  for(const metadata of [notes('One')+'\n'+notes('Two'),'<!-- notes=base64:%%%% -->','<!-- notes=base64:/w== -->',notes('x'.repeat((1<<20)+1))]){
   const alert=page.waitForEvent('dialog',{predicate:d=>d.type()==='alert'});await open('Rejected.md','<!-- keynope width=100 height=80 -->\n# Rejected\n'+metadata);await alert;await settled;const s=await state();assert.equal(s.path,before.path);assert.equal(s.version,before.version);assert.deepEqual(s.slides,before.slides);assert.equal(s.mutationRevision,before.mutationRevision);
  }
  for(const master of [false,true]){
   if(master)await action({action:'toggle-master-mode'});
   const s=await state(),result=await page.evaluate(async slide=>{const r=await fetch('/api/editor/action',{method:'POST',headers:{'Content-Type':'application/json','Accept':'application/problem+json'},body:JSON.stringify({action:'update-slide-notes',slide,notes:'x'.repeat((1<<20)+1)})});return {status:r.status,problem:await r.json()};},s.current);
   assert.equal(result.status,400);assert.equal(result.problem.code,'invalid-value');assert.equal(result.problem.field,'notes');const unchanged=await state();assert.equal(unchanged.version,s.version);assert.deepEqual(unchanged.slides,s.slides);
   await action({action:'update-slide-notes',slide:s.current,notes:'Changed notes 🦀'});assert.equal((await state()).slides[s.current].notes,'Changed notes 🦀');await action({action:'undo'});assert.deepEqual((await state()).slides,s.slides);
  }
  assert.equal(alerts.length,4);assert.deepEqual(errors,[]);console.log('PASS: optional appearance recovery, private-note preservation, malformed/duplicate/oversized note rejection, retained dirty document/history and normal/master note command admission.');
 }finally{await browser.close();server.close();}
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
