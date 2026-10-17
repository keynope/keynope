// Numeric text inspector choices distinguish explicit values from inheritance.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http'),{execFileSync}=require('node:child_process');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright'),{waitForAsync}=require('./wait_for_async.cjs');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),engine=process.env.TEST_BROWSER||'chromium',proof='/tmp/keynope-text-metric-'+engine;
execFileSync('go',['test','./internal/app','-run','^TestTextMetricBrowserFixture$','-count=1'],{env:{...process.env,GOCACHE:process.env.GOCACHE||'/tmp/keynope-go-cache',KEYNOPE_METRIC_PROOF:proof},stdio:'pipe'});
const server=http.createServer((req,res)=>{
 let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);
 if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}
 res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));
});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const browser=await pw[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
 try{
  const page=await browser.newPage({viewport:{width:1600,height:1100}}),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());page.setDefaultTimeout(20000);
  await page.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
  const frame=()=>page.evaluate(()=>fetch('/api/editor/scene?slide=0&styles=resolved').then(r=>r.json()).then(s=>s.objects.find(o=>o.id==='child-text').bounds));
  const open=async(name,content=fs.readFileSync(path.join(proof,name)))=>{
   const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles({name,mimeType:'text/markdown',buffer:Buffer.from(content)});
   await waitForAsync(page,async name=>(await fetch('/api/editor/state').then(r=>r.json())).path.endsWith(name),name);
  };
  const select=async(id,tab='Text')=>{await page.locator('.keynope-canvas-element[data-element-id="'+id+'"]').click();await page.getByRole('tab',{name:tab,exact:true}).click();};
  const check=async(property,value,provenance)=>waitForAsync(page,async({property,value,provenance})=>{const s=await fetch('/api/editor/state').then(r=>r.json()),p=s.selectionCapabilities?.properties?.[property];return p?.state==='value'&&p?.value===value&&p?.provenance===provenance;},{property,value,provenance});
  await open('metrics.md');await select('child-text');const masters=(await state()).masters;
  for(const [property,caption,value]of [['text.size','Font size',64],['text.width','TrueType width percent',75]]){
   await check(property,value,'inherited');const before=await state();
   const input=page.getByRole('spinbutton',{name:caption,exact:true});await input.fill(String(value));await input.dispatchEvent('change');await check(property,value,'explicit');
   assert.equal((await state()).version,before.version+1);assert.equal(await page.getByRole('spinbutton',{name:caption,exact:true}).inputValue(),String(value));
   await page.getByRole('button',{name:'Undo',exact:true}).click();await check(property,value,'inherited');await page.getByRole('button',{name:'Redo',exact:true}).click();await check(property,value,'explicit');
  }
  assert.deepEqual((await state()).masters,masters);
  const saved=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Saved.md'})}).then(r=>r.json()).then(r=>r.content));
  await open('Saved.md',saved);await select('child-text');await check('text.size',64,'explicit');await check('text.width',75,'explicit');
  for(const [property,caption,value]of [['text.size','Font size',64],['text.width','TrueType width percent',75]]){
   const input=page.getByRole('spinbutton',{name:caption,exact:true});await input.fill('');await input.press('Tab');await check(property,value,'inherited');
  }
  await page.getByRole('button',{name:'Increase font size',exact:true}).click();await check('text.size',65,'explicit');
  await waitForAsync(page,()=>document.querySelector('[aria-label="Font size"]')?.value==='65');
  await page.getByRole('button',{name:'Decrease font size',exact:true}).click();await check('text.size',64,'explicit');
  const widthFrame=await frame();
  await page.getByRole('button',{name:'Increase font width',exact:true}).click();await check('text.width',76,'explicit');assert.deepEqual(await frame(),widthFrame);
  await waitForAsync(page,()=>document.querySelector('[aria-label="TrueType width percent"]')?.value==='76');
  await page.getByRole('button',{name:'Decrease font width',exact:true}).click();await check('text.width',75,'explicit');assert.deepEqual(await frame(),widthFrame);
  await page.screenshot({path:path.join(proof,'metrics.png')});
  await open('labels.md');await select('label-a','Style');await page.locator('details[data-selection-context="shape-label"] > summary').click();
  for(const [property,caption,value,button]of [['label.size','Selection label size',55,'Inherit label size'],['label.width','Selection label width',120,'Inherit label width']]){
   await page.getByRole('spinbutton',{name:caption,exact:true}).fill(String(value));await page.getByRole('spinbutton',{name:caption,exact:true}).press('Tab');await check(property,value,'explicit');
   await page.getByRole('button',{name:button,exact:true}).click();
   const expected=property==='label.size'?40:100;
   await waitForAsync(page,async({property,expected})=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities?.properties?.[property]?.value===expected;},{property,expected});
  }
  assert.deepEqual(errors,[]);console.log('PASS '+engine+': explicit equal size/width, independent inheritance, clear, +/− input refresh, history, save/reopen, shape-label Inherit. '+proof);
 }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);server.close();process.exitCode=1;});
