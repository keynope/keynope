// Editable native character gradients through the actual web editor.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const {execFileSync}=require('node:child_process'),{waitForAsync}=require('./wait_for_async.cjs'),pw=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),engine=process.env.TEST_BROWSER||'chromium',proof='/tmp/keynope-text-gradient-proof-'+engine;
fs.mkdirSync(proof,{recursive:true});
execFileSync('go',['test','./internal/pptx','-run','^TestEditableCharacterGradientPackage$','-count=1'],{env:{...process.env,GOCACHE:process.env.GOCACHE||'/tmp/keynope-go-cache',KEYNOPE_TEXT_GRADIENT_FIXTURE:path.join(proof,'gradients.pptx')},stdio:'pipe'});
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const browser=await pw[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
 try{
  const page=await browser.newPage({viewport:{width:1440,height:1000}}),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());await page.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles(path.join(proof,'gradients.pptx'));
  await waitForAsync(page,async()=>(await fetch('/api/editor/state').then(r=>r.json())).path.endsWith('gradients.md')&&deck.pages.length===9);
  const report=page.getByRole('dialog').filter({hasText:'PowerPoint imported'});if(await report.isVisible())await report.getByRole('button',{name:'Close',exact:true}).click();
  const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
  const snapshots=()=>page.evaluate(()=>deck.pages.map(p=>{const s=structuredClone(p.scene);delete s.revision;return s;}));
  const initial=await snapshots();
  initial.forEach((scene,i)=>{assert.equal(scene.background.toLowerCase(),'#101820','native slide background lost');const object=scene.objects.find(o=>o.text?.runs?.some(r=>r.text.includes('Editable')));assert(object,'missing editable text '+i);assert.equal(scene.objects.length,1,'background became selectable artwork');assert.equal(object.paint.gradientStart.toLowerCase(),'#ff0055');assert.equal(object.paint.gradientEnd.toLowerCase(),'#55ffff');assert.equal(object.paint.gradientDirection,['horizontal','vertical','diagonal'][Math.floor(i/3)]);assert(Math.abs(object.paint.opacity-[0,.35,1][i%3])<1e-6);});
  await page.evaluate(async()=>{await editorAction({action:'select-slide',slide:5});const s=await fetch('/api/editor/state').then(r=>r.json());await editorAction({action:'select-element',element:0,elementData:s.slides[5].elements[0]});});
  const before=await state(),id=before.slides[5].elements[0].id;
  await page.evaluate(({id,revision})=>editorAction({action:'set-selection-property',slide:5,sceneMaster:false,sceneRevision:revision,objectIds:[id],property:'paint.gradientEnd',propertyValue:'#ffaa00'}),{id,revision:before.version});
  await page.waitForFunction(()=>deck.pages.find(p=>p.slide===5).scene.objects.some(o=>o.paint.gradientEnd==='#ffaa00'));
  const edited=await snapshots();assert.equal((await state()).version,before.version+1);
  await page.locator('.keynope-engagement-toast').waitFor({state:'detached'});
  await page.locator('#stage').screenshot({path:path.join(proof,'edited-gradient.png')});
  await page.evaluate(()=>editorAction({action:'undo'}));assert.deepEqual(await snapshots(),initial);
  await page.evaluate(()=>editorAction({action:'redo'}));assert.deepEqual(await snapshots(),edited);
  const content=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Gradients.md'})}).then(r=>r.json()).then(r=>r.content));
  const picker=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await picker).setFiles({name:'Gradients.md',mimeType:'text/markdown',buffer:Buffer.from(content)});
  await waitForAsync(page,async()=>(await fetch('/api/editor/state').then(r=>r.json())).path.endsWith('Gradients.md')&&deck.pages.length===9);assert.deepEqual(await snapshots(),edited);
  const exported=await page.evaluate(async()=>{const r=await fetch('/api/editor/export-pptx',{method:'POST'});if(!r.ok)throw Error(await r.text());return r.json();});
  assert(exported.content);assert.equal(exported.report.warnings.filter(w=>w.code==='text-gradient-reader-dependent').length,9);
  const bytes=Buffer.from(exported.content,'base64');fs.writeFileSync(path.join(proof,'native-background.pptx'),bytes);
  const nativePicker=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await nativePicker).setFiles({name:'native-background.pptx',mimeType:'application/vnd.openxmlformats-officedocument.presentationml.presentation',buffer:bytes});
  await waitForAsync(page,async()=>(await fetch('/api/editor/state').then(r=>r.json())).path.endsWith('native-background.md')&&deck.pages.length===9);
  (await snapshots()).forEach((scene,i)=>{assert.equal(scene.background.toLowerCase(),'#101820');assert.equal(scene.objects.length,1,'export/reimport introduced a background object');const text=scene.objects[0];assert.equal(text.paint.gradientEnd.toLowerCase(),i===5?'#ffaa00':'#55ffff');});
  assert.deepEqual(errors,[]);console.log('PASS '+engine+': nine native character gradients, zero/partial/full alpha, editable paint, exact history, Markdown reopen and native-background PPTX export/reimport.');
 }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);server.close();process.exitCode=1;});
