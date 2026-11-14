// Uses an isolated private engine; never opens the desktop app or a user deck.
const assert=require('node:assert/strict'),fs=require('node:fs'),os=require('node:os'),path=require('node:path'),cp=require('node:child_process');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright');
(async()=>{
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'keynope-reload-')),file=path.join(root,'deck.md');
 const write=text=>{fs.writeFileSync(file+'.tmp','# '+text+'\n');fs.renameSync(file+'.tmp',file);};write('Before reload');
 let browser,engine;
 try{
  // The app helper inherits its parent's App Sandbox and cannot run directly
  // from a shell. Build an isolated CLI engine unless one is supplied.
  const enginePath=process.env.KEYNOPE_ENGINE||path.join(root,'engine');
  if(!process.env.KEYNOPE_ENGINE){
   const build=cp.spawnSync('go',['build','-o',enginePath,'./cmd/keynope'],{cwd:path.resolve(__dirname,'..'),env:{...process.env,GOCACHE:process.env.GOCACHE||path.join(os.tmpdir(),'keynope-go-cache')},encoding:'utf8'});
   assert.equal(build.status,0,build.error?.message||build.stderr);
  }
  engine=cp.spawn(enginePath,['--app',file],{stdio:['pipe','pipe','pipe']});
  const url=await new Promise((resolve,reject)=>{let output='';const timer=setTimeout(()=>reject(Error('Engine did not start')),30000);engine.once('error',error=>{clearTimeout(timer);reject(error);});engine.once('exit',(code,signal)=>{clearTimeout(timer);reject(Error('Engine exited '+(signal||code)));});engine.stdout.on('data',chunk=>{output+=chunk;const match=output.match(/KEYNOPE_URL=(\S+)/);if(match){clearTimeout(timer);resolve(match[1]);}});});
  browser=await pw[process.env.TEST_BROWSER||'chromium'].launch({headless:true});
  const page=await browser.newPage({viewport:{width:1440,height:1000}}),errors=[];page.setDefaultTimeout(15000);page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
  await page.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
  await page.goto(url+'&keynopeSurface=app');await page.waitForFunction(()=>typeof window.keynopeSelectAll==='function');
  const state=()=>page.evaluate(()=>fetch('/api/editor/state?deferReload=1').then(r=>r.json()));
  const original=await state();assert.equal(original.dirty,false);
  write('After external save');
  await page.waitForFunction(async()=>{const s=await fetch('/api/editor/state?deferReload=1').then(r=>r.json());return s.slides[0].elements.some(e=>e.text==='After external save');});
  await page.waitForFunction(()=>document.querySelector('#stage').textContent.replace(/\s/g,'').includes('Afterexternalsave'));
  assert.notEqual((await state()).notesSessionId,original.notesSessionId);
  await page.evaluate(()=>editorAction({action:'add-element',kind:'text'}));
  write('External conflict');
  await page.getByRole('button',{name:'Reload from disk',exact:true}).waitFor();
  const before=await state();assert.equal(before.dirty,true);assert(before.slides[0].elements.some(e=>e.text==='After external save'));
  await page.getByRole('button',{name:'Reload from disk',exact:true}).click();
  await page.waitForFunction(()=>document.querySelector('#stage').textContent.replace(/\s/g,'').includes('Externalconflict'));
  const after=await state();assert.equal(after.dirty,false);assert.equal(after.slides[0].elements.length,1);
  assert.deepEqual(errors,[]);
  console.log('PASS: native editor hot reload updates the canvas, preserves local edits and confirms conflict replacement.');
 }finally{await browser?.close();engine?.stdin.end();engine?.kill();fs.rmSync(root,{recursive:true,force:true});}
})().catch(error=>{console.error(error);process.exitCode=1});
