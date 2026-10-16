// Production WASM contextual controls: absence is not applicability.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const {waitForAsync}=require('./wait_for_async.cjs');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright'),engine=process.env.TEST_BROWSER||'chromium';
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site');
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));const browser=await playwright[engine].launch({headless:true,...(engine==='webkit'?{}:{executablePath:process.env.CHROME_PATH})});
 try{
  const context=await browser.newContext({viewport:{width:1500,height:1000}});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles({name:'Contexts.md',mimeType:'text/markdown',buffer:Buffer.from('<!-- keynope width=245 height=56 -->\n\n<!-- element-id=box top=10 left=10 width=35 height=12 shape=square -->\n[shape:square]\n\n<!-- element-id=text top=30 left=70 width=80 height=10 render=truetype -->\nOwned text\n')});
  await waitForAsync(page,async()=>(await fetch('/api/editor/state').then(r=>r.json())).path.endsWith('Contexts.md'));
  const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json())),action=data=>page.evaluate(data=>editorAction(data),data);
  const select=async ids=>{const s=await state(),indices=ids.map(id=>s.slides[s.current].elements.findIndex(e=>e.id===id));assert(indices.every(i=>i>=0));await action({action:'select-elements',elementIndices:indices,elementsData:ids.map(id=>({id})),element:indices[0]});await page.getByRole('tab',{name:'Style',exact:true}).click();};
  const imageStyle=page.getByRole('combobox',{name:'Image style',exact:true}),fill=page.getByRole('combobox',{name:'Selection shape fill',exact:true});
  await select(['box']);assert.equal(await imageStyle.isVisible(),false,'Shape selection exposes image controls');assert(await fill.isVisible());assert.equal(await page.getByRole('button',{name:'Enable see through',exact:true}).isVisible(),false,'Vector shape exposes ineffective Retro blending');
  await select(['text']);assert.equal(await imageStyle.isVisible(),false,'Text selection exposes image controls');assert.equal(await fill.isVisible(),false,'Text selection exposes shape fill');
  await action({action:'select-element',element:-1});await page.getByRole('button',{name:'Insert',exact:true}).click();const imageChooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Import image',exact:true}).click();
  const source=await page.evaluate(()=>{const c=document.createElement('canvas');c.width=32;c.height=16;const x=c.getContext('2d');x.fillStyle='#ff0055';x.fillRect(0,0,32,16);return c.toDataURL();});await(await imageChooser).setFiles({name:'Owned.png',mimeType:'image/png',buffer:Buffer.from(source.split(',')[1],'base64')});
  await waitForAsync(page,async()=>(await fetch('/api/editor/state').then(r=>r.json())).slides[0].elements.some(e=>e.kind==='image'));
  const image=(await state()).slides[0].elements.find(e=>e.kind==='image').id;await select([image]);assert(await imageStyle.isVisible());assert.equal(await fill.isVisible(),false);assert.equal(await page.getByRole('button',{name:'Enable see through',exact:true}).isVisible(),false,'Modern image exposes ineffective Retro blending');
  await select(['box',image]);assert(await imageStyle.isVisible());assert(await fill.isVisible());assert((await imageStyle.locator('..').locator('..').textContent()).includes('1 of 2'),'Image subset is not labelled');
  const before=await state();await imageStyle.selectOption('retro');await waitForAsync(page,async id=>new URLSearchParams((await fetch('/api/editor/state').then(r=>r.json())).slides[0].elements.find(e=>e.id===id).query).get('image-style')==='retro',image);
  const after=await state();assert.deepEqual(after.slides[0].elements.find(e=>e.id==='box'),before.slides[0].elements.find(e=>e.id==='box'),'Image style mutated shape');
  await select([image]);await action({action:'set-object-lock',value:1});assert(await imageStyle.isVisible(),'Locked image lost its relevant controls');assert(await imageStyle.isDisabled(),'Locked image control is enabled');assert((await imageStyle.getAttribute('title')).length>0);
  await select(['box',image]);const locked=await state();assert.equal(locked.selectionCapabilities.properties['paint.opacity'].editable,false,'Locked eligible member must disable the shared property');
  const rejection=await action({action:'set-selection-property',slide:0,sceneRevision:locked.version,objectIds:['box',image],property:'paint.opacity',propertyValue:.5}).then(()=>'',e=>e.message);
  assert(rejection,'Locked mixed mutation was accepted');const rejected=await state();assert.deepEqual(rejected.slides,locked.slides);assert.equal(rejected.version,locked.version);assert.equal(rejected.canUndo,locked.canUndo);
  await select([image]);await action({action:'set-object-lock',value:0});await select(['text','box',image]);const shared=await state();
  assert.deepEqual(shared.selectionCapabilities.properties['text.size'].eligibleIds,['text']);assert.equal(shared.selectionCapabilities.properties['text.size'].totalCount,3);
  await action({action:'set-selection-property',slide:0,sceneRevision:shared.version,objectIds:['text','box',image],property:'paint.opacity',propertyValue:.5});const painted=await state();assert.equal(painted.mutationRevision,shared.mutationRevision+1);
  for(const original of shared.slides[0].elements){const changed=painted.slides[0].elements.find(e=>e.id===original.id);if(!['text','box',image].includes(original.id)){assert.deepEqual(changed,original);continue;}const q=new URLSearchParams(changed.query);assert.equal(q.get('modern-opacity'),'0.5');q.delete('modern-opacity');const old=new URLSearchParams(original.query);old.delete('modern-opacity');assert.deepEqual([...q].sort(),[...old].sort(),'Shared opacity changed unrelated style/geometry');assert.deepEqual({...changed,query:''},{...original,query:''});}
  await action({action:'undo'});assert.deepEqual((await state()).slides,shared.slides,'Mixed edit was not one undo step');await action({action:'redo'});assert.deepEqual((await state()).slides,painted.slides);
  await select(['box']);assert.equal(await imageStyle.isVisible(),false,'Image controls survived selection change');
  // Check actual painted label fragments, not just the grid-cell rectangles:
  // a minmax(0,1fr) label can overflow into a neighbouring colour button.
  const labelCollisions=()=>page.evaluate(()=>{
   const hits=[];for(const row of document.querySelectorAll('.keynope-paint-field')){
    if(!row.getClientRects().length)continue;const label=row.querySelector(':scope > span');if(!label)continue;
    const range=document.createRange();range.selectNodeContents(label);
    for(const text of range.getClientRects())for(const control of row.querySelectorAll(':scope > button,:scope > input,:scope > select')){
     const b=control.getBoundingClientRect();if(b.width&&b.height&&text.width&&text.height&&Math.min(text.right,b.right)-Math.max(text.left,b.left)>.5&&Math.min(text.bottom,b.bottom)-Math.max(text.top,b.top)>.5)hits.push(control.getAttribute('aria-label'));
    }
   }return hits;
  });
  for(const width of [260,300,360]){await page.evaluate(width=>document.documentElement.style.setProperty('--keynope-inspector-width',width+'px'),width);await page.evaluate(()=>new Promise(requestAnimationFrame));assert.deepEqual(await labelCollisions(),[],'Inspector paint labels overlap controls at '+width+'px');}
  await page.evaluate(()=>{document.documentElement.style.setProperty('--keynope-inspector-width','260px');const style=document.createElement('style');style.id='old-stroke-grid';style.textContent='.keynope-paint-field-stroke{grid-template-columns:minmax(0,1fr) minmax(65px,105px) auto auto}.keynope-paint-field-stroke>span{grid-column:auto;overflow-wrap:normal}';document.head.append(style);});
  await page.evaluate(()=>new Promise(requestAnimationFrame));
  assert((await labelCollisions()).length>0,'Overlap regression check failed to detect the previous stroke layout');await page.locator('#old-stroke-grid').evaluate(n=>n.remove());assert.deepEqual(await labelCollisions(),[]);await page.evaluate(()=>document.documentElement.style.removeProperty('--keynope-inspector-width'));
  await page.evaluate(()=>{window.paintRefreshRecords=[];window.paintRefreshObserver=new MutationObserver(records=>paintRefreshRecords.push(...records.map(r=>({type:r.type,attribute:r.attributeName,property:r.target.dataset?.selectionProperty||r.target.className}))));paintRefreshObserver.observe(document.querySelector('.keynope-selection-paint'),{subtree:true,attributes:true,childList:true,characterData:true});});
  for(let i=0;i<3;i++)await action({action:'select-element',elementData:{id:'box'}});
  await page.evaluate(()=>new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r))));
  assert.deepEqual(await page.evaluate(()=>paintRefreshRecords),[],'Unchanged paint inspector refresh rewrote retained chrome');
  const widthControl=page.getByRole('spinbutton',{name:'Selection stroke width',exact:true}),beforeWidth=await state();await widthControl.evaluate(n=>window.retainedPaintWidth=n);
  await widthControl.fill('2.25');await widthControl.press('Tab');await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities?.properties['paint.strokeWidth']?.value===2.25;});
  assert.equal(await widthControl.inputValue(),'2.25','Accepted width stayed stale');assert(await widthControl.evaluate(n=>n===window.retainedPaintWidth),'Accepted paint edit replaced the control');
  assert((await page.evaluate(()=>paintRefreshRecords.length))>0,'Changed paint state did not update inspector chrome');await action({action:'undo'});assert.deepEqual((await state()).slides,beforeWidth.slides);
  await page.evaluate(()=>paintRefreshObserver.disconnect());
  await page.screenshot({path:'/tmp/keynope-contextual-inspector-'+engine+'.png'});assert.deepEqual(errors,[]);console.log('PASS '+engine+': contextual visibility/subsets, locked mixed rejection, shared text/shape/image opacity isolation and one-step history.');
 }finally{await browser.close();server.close();}
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
