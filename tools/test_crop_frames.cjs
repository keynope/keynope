// Real crop dialog: natural PNG/SVG/GIF frames stay fixed in both treatments.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http'),{execFileSync}=require('node:child_process');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright'),{waitForAsync}=require('./wait_for_async.cjs');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),engine=process.env.TEST_BROWSER||'webkit',proof='/tmp/keynope-crop-frames-'+engine;
execFileSync('go',['test','.','-run','^TestCropFrameBrowserFixture$','-count=1'],{env:{...process.env,GOCACHE:process.env.GOCACHE||'/tmp/keynope-go-cache',KEYNOPE_CROP_FRAME_PROOF:proof},stdio:'pipe'});
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const browser=await pw[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
 try{
  const page=await browser.newPage({viewport:{width:1600,height:1000},reducedMotion:'reduce'}),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());await page.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const action=data=>page.evaluate(data=>editorAction(data),data),state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
  async function open(name,buffer=fs.readFileSync(path.join(proof,name))){const picker=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await picker).setFiles({name,mimeType:'text/markdown',buffer:Buffer.from(buffer)});await waitForAsync(page,async name=>(await fetch('/api/editor/state').then(r=>r.json())).path.endsWith(name),name);}
  const frame=id=>page.evaluate(id=>{const o=deck.pages[pageIndex].scene.objects.find(o=>o.id===id),n=document.querySelector('#stage [data-object-id="'+id+'"]');return {bounds:o.bounds,rotation:o.rotation,css:n&&[n.style.left,n.style.top,n.style.width,n.style.height,n.style.transform]};},id);
  function same(a,b){assert.equal(a.rotation,b.rotation);for(const k of ['x','y','width','height'])assert(Math.abs(a.bounds[k]-b.bounds[k])<1e-6,JSON.stringify({before:a,after:b}));assert.deepEqual(a.css,b.css);}
  await open('crop-frames.md');const count=(await state()).slides.length,records=[];assert.equal(count,6);
  await page.evaluate(()=>{const original=window.fetch;window.cropFramesDraft=[];window.fetch=async(...args)=>{const response=await original(...args);if(String(args[0]).endsWith('/media-draft'))cropFramesDraft.push({status:response.status,body:response.ok?await response.clone().json():await response.clone().text()});return response;};});
  for(let slide=0;slide<count;slide++){
   await action({action:'select-slide',slide});const s=await state(),id=s.slides[slide].elements[0].id,before=await frame(id);
   await page.locator('.keynope-canvas-element[data-element-id="'+id+'"]').dblclick();const dialog=page.getByRole('dialog',{name:'Crop image',exact:true});await dialog.waitFor();
   for(const [side,value]of [['left','30'],['top','10']]){const input=dialog.getByRole('spinbutton',{name:'Crop '+side+' percent',exact:true});await input.fill(value);await input.press('Tab');}
   const retro=await page.evaluate(id=>!!deck.pages[pageIndex].scene.objects.find(o=>o.id===id).retroLines,id);
   if(retro)await waitForAsync(page,()=>cropFramesDraft.some(r=>r.status===200&&r.body.objects?.[0]?.media?.crop?.left===.3&&r.body.objects?.[0]?.media?.crop?.top===.1));
   else assert.equal(await page.evaluate(()=>cropFramesDraft.length),0,'Modern preview should stay local');
   assert.deepEqual((await state()).slides,s.slides,'Draft modified authored content');same(before,await frame(id));
   await dialog.getByRole('button',{name:'Apply crop',exact:true}).click();await dialog.waitFor({state:'detached'});const changed=await state();assert.equal(changed.mutationRevision,s.mutationRevision+1);same(before,await frame(id));
   await action({action:'undo'});assert.deepEqual((await state()).slides,s.slides);same(before,await frame(id));await action({action:'redo'});assert.deepEqual((await state()).slides,changed.slides);same(before,await frame(id));
   records.push({slide,id,frame:before});await page.screenshot({path:path.join(proof,'crop-'+slide+'.png')});await page.evaluate(()=>{cropFramesDraft=[];});
  }
  const saved=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Saved.md'})}).then(r=>r.json()).then(r=>r.content));await open('Saved.md',saved);
  for(const record of records){await action({action:'select-slide',slide:record.slide});same(record.frame,await frame(record.id));}
  for(const field of ['mask','crop']){
   await open('inherited.md');const before=await state(),id='instance',original=await frame(id);
   await page.locator('.keynope-canvas-element[data-element-id="'+id+'"]').dblclick();const dialog=page.getByRole('dialog',{name:'Crop image',exact:true});await dialog.waitFor();
   if(field==='mask')await dialog.getByRole('combobox',{name:'Image mask',exact:true}).selectOption('rounded');
   else{const number=dialog.getByRole('spinbutton',{name:'Crop left percent',exact:true});await number.fill('20');await number.press('Tab');}
   await dialog.getByRole('button',{name:'Apply crop',exact:true}).click();await dialog.waitFor({state:'detached'});
   const after=await state(),query=new URLSearchParams(after.slides[0].elements.find(e=>e.id===id).query),untouched=field==='mask'?'modern-crop':'modern-mask';
   assert(!query.has(untouched),'Dialog materialized untouched '+untouched);assert(!(query.get('master-clear')||'').split(',').includes(untouched),'Dialog suppressed untouched '+untouched);assert.deepEqual(after.masters,before.masters);same(original,await frame(id));
   await action({action:'undo'});assert.deepEqual((await state()).slides,before.slides);await action({action:'redo'});assert.deepEqual((await state()).slides,after.slides);
   const content=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Partial.md'})}).then(r=>r.json()).then(r=>r.content));await open('Partial.md',content);assert.equal(new URLSearchParams((await state()).slides[0].elements.find(e=>e.id===id).query).has(untouched),false);same(original,await frame(id));
  }
  assert.deepEqual(errors,[]);fs.writeFileSync(path.join(proof,'report.json'),JSON.stringify(records,null,2));console.log('PASS '+engine+': six natural PNG/SVG/GIF crop dialogs, fixed frames/rotation, isolated preview, independent inherited crop/mask ownership, one Apply, exact Undo/Redo and Markdown reopen.');
 }finally{await browser.close();server.close();}
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
