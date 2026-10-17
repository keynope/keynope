// Actual shared editor alignment buttons, frames and inheritance; no Mac app.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http'),{execFileSync}=require('node:child_process');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright'),{waitForAsync}=require('./wait_for_async.cjs');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),engine=process.env.TEST_BROWSER||'chromium',proof='/tmp/keynope-alignment-'+engine;
execFileSync('go',['test','./internal/app','-run','^TestAlignmentBrowserFixture$','-count=1'],{env:{...process.env,GOCACHE:process.env.GOCACHE||'/tmp/keynope-go-cache',KEYNOPE_ALIGNMENT_PROOF:proof},stdio:'pipe'});
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
  const open=async(name,content=fs.readFileSync(path.join(proof,name)))=>{
   const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles({name,mimeType:'text/markdown',buffer:Buffer.from(content)});
   await waitForAsync(page,async name=>(await fetch('/api/editor/state').then(r=>r.json())).path.endsWith(name),name);
  };
  const select=async(id,tab='Text')=>{await page.locator('.keynope-canvas-element[data-element-id="'+id+'"]').click();await page.getByRole('tab',{name:tab,exact:true}).click();};
  const check=(property,value,provenance)=>waitForAsync(page,async({property,value,provenance})=>{const s=await fetch('/api/editor/state').then(r=>r.json()),p=s.selectionCapabilities?.properties?.[property];return p?.value===value&&p?.provenance===provenance;},{property,value,provenance});
  const frame=()=>page.evaluate(()=>{const object=deck.pages[pageIndex].scene.objects.find(o=>o.id==='child-text');return object.bounds;});
  await open('alignment.md');await select('child-text');const original=await state(),bounds=await frame();
  await check('text.align','center','inherited');await check('text.vertical','middle','inherited');
  await page.getByRole('button',{name:'Align text right',exact:true}).click();await check('text.align','right','explicit');
  await page.getByRole('button',{name:'Align text bottom',exact:true}).click();await check('text.vertical','bottom','explicit');
  assert.deepEqual(await frame(),bounds);assert.deepEqual((await state()).masters,original.masters);
  const q=new URLSearchParams((await state()).slides[0].elements[0].query);for(const key of ['left','top','width','height'])assert.equal(q.has(key),false,'Alignment froze '+key);
  await page.getByRole('button',{name:'Undo',exact:true}).click();await check('text.vertical','middle','inherited');await check('text.align','right','explicit');
  await page.getByRole('button',{name:'Redo',exact:true}).click();await check('text.vertical','bottom','explicit');
  await page.getByRole('button',{name:'Inherit horizontal text alignment',exact:true}).click();await check('text.align','center','inherited');await check('text.vertical','bottom','explicit');
  await page.getByRole('button',{name:'Inherit vertical text alignment',exact:true}).click();await check('text.vertical','middle','inherited');assert.deepEqual(await frame(),bounds);
  const saved=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Saved.md'})}).then(r=>r.json()).then(r=>r.content));
  await open('Saved.md',saved);await select('child-text');await check('text.align','center','inherited');await check('text.vertical','middle','inherited');assert.deepEqual(await frame(),bounds);
  await page.screenshot({path:path.join(proof,'alignment.png')});
  await open('legacy.md');await select('child-text');await check('text.align','justify','explicit');
  assert.equal(await page.evaluate(()=>deck.pages[pageIndex].scene.objects.find(o=>o.id==='child-text').text.align),'justify');
  await page.getByRole('button',{name:'Inherit horizontal text alignment',exact:true}).click();await check('text.align','center','inherited');
  await open('labels.md');await select('label-a','Style');await page.locator('details[data-selection-context="shape-label"] > summary').click();const labels=await state();
  for(const [property,caption,value,inherited,button]of [['label.align','Selection label alignment','right','center','Inherit label alignment'],['label.vertical','Selection label vertical alignment','bottom','middle','Inherit label vertical alignment']]){
   await page.getByRole('combobox',{name:caption,exact:true}).selectOption(value);await check(property,value,'explicit');
   await page.getByRole('button',{name:button,exact:true}).click();await check(property,inherited,'inherited');
  }
  assert.deepEqual((await state()).slides,labels.slides,'Label inheritance changed shape/content/other styles');
  await page.screenshot({path:path.join(proof,'labels.png')});
  await open('legacy-label.md');await select('label-a','Style');
  if(!await page.locator('details[data-selection-context="shape-label"]').evaluate(node=>node.open))await page.locator('details[data-selection-context="shape-label"] > summary').click();
  await check('label.align','justify','explicit');
  await page.getByRole('combobox',{name:'Selection label alignment',exact:true}).selectOption('right');await check('label.align','right','explicit');
  await page.getByRole('button',{name:'Inherit label alignment',exact:true}).click();await check('label.align','justify','explicit');
  await page.screenshot({path:path.join(proof,'legacy-label.png')});
  await page.getByRole('button',{name:'Reset label style',exact:true}).click();await check('label.align','center','inherited');
  const output=process.env.KEYNOPE_ALIGNMENT_OUTPUT_PROOF;
  if(output){
   const cases=JSON.parse(fs.readFileSync(path.join(output,'cases.json')));assert.equal(cases.length,15);
   for(const item of cases){
    const md=fs.readFileSync(path.join(output,item.file.replace('.html','.md')),'utf8');
    const result=await page.evaluate(md=>{
     const response=JSON.parse(keynopeWasmRenderParticipantPage(md,0,7,20));
     if(response.status!==200)throw Error(response.body);
     const doc=new DOMParser().parseFromString(response.body,'text/html');return JSON.parse(doc.querySelector('#keynope-data').textContent);
    },md);
    assert.equal(result.pages.length,1,item.file);assert.equal(result.pages[0].slide,7);assert.equal(result.pages[0].slideCount,20);
    const actual=result.pages[0].scene,expected=item.scenes[0];assert(actual,item.file+' did not use the shared scene');
    assert.equal(actual.width,expected.width);assert.equal(actual.height,expected.height);assert.equal(actual.objects.length,12);
    for(const object of expected.objects){const restored=actual.objects.find(o=>o.id===object.id);assert(restored,item.file+'/'+object.id);for(const field of ['bounds','text','label','paint','labelPaint'])assert.deepEqual(restored[field],object[field],item.file+'/'+object.id+'/'+field);}
   }
  }
  assert.deepEqual(errors,[]);
  console.log('PASS '+engine+': alignment keeps master frame, independent Inherit, accepted controls, history/reopen, legacy label justification and concrete WASM participant output. '+proof);
 }finally{await browser.close();server.close();}
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
