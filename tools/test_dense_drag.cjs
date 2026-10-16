const {waitForAsync}=require("./wait_for_async.cjs");
// Targeted single-slide gesture proof, not participant/workshop load testing.
const fs=require('node:fs'),path=require('node:path'),http=require('node:http'),assert=require('node:assert/strict');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright'),engine=process.env.TEST_BROWSER||'chromium';
const root=process.env.TEST_SITE||'/tmp/keynope-powerup-site',output=process.env.TEST_OUTPUT||'/tmp/keynope-dense-drag-proof';
const nativeURL=process.env.NATIVE_EDITOR_URL;
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 fs.mkdirSync(output,{recursive:true});if(!nativeURL)await new Promise(r=>server.listen(0,'127.0.0.1',r));const browser=await playwright[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
 try{
  const context=await browser.newContext({viewport:{width:1440,height:1000},reducedMotion:'reduce'});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
  await page.goto(nativeURL||'http://127.0.0.1:'+server.address().port+'/editor/');
  if(!nativeURL){await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});const picker=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await picker).setFiles({name:'dense.md',mimeType:'text/markdown',buffer:fs.readFileSync('/tmp/keynope-dense-editing.md')});}
  await page.waitForFunction(()=>typeof deck!=='undefined'&&deck.pages[pageIndex]?.scene?.objects.length===49,{},{timeout:90000});
  const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
  const settle=()=>page.evaluate(()=>new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r))));
  // Native engines share one owned document. Restore the media treatment so
  // both runs exercise modern PNG as well as the final explicit Retro case.
  {const s=await state();await page.evaluate(revision=>editorAction({action:'set-selection-property',slide:0,sceneMaster:false,sceneRevision:revision,objectIds:['alpha'],property:'image.style',propertyValue:'modern'}),s.version);await settle();}
  const records=[];
  for(const test of [{ids:['dense-text-0'],finish:'commit'},{ids:['dense-text-0','dense-text-1'],finish:'commit'},{ids:['from','obstacle'],finish:'commit'},{ids:['dense-text-0'],finish:'escape'},{ids:['dense-text-0','dense-text-1'],finish:'escape'},{ids:['from','obstacle'],finish:'escape',delay:true},{ids:['dense-text-0'],finish:'blur'},
   {ids:['dense-table'],finish:'commit'},{ids:['dense-chart'],finish:'commit'},{ids:['alpha'],finish:'commit'},{ids:['vector'],finish:'commit'},{ids:['animation'],finish:'commit'},{ids:['dense-text-0','dense-table','alpha'],finish:'escape'},{ids:['alpha'],finish:'commit',style:'retro'}]){
   if(test.style){const s=await state();await page.evaluate(({revision,ids,style})=>editorAction({action:'set-selection-property',slide:0,sceneMaster:false,sceneRevision:revision,objectIds:ids,property:'image.style',propertyValue:style}),{revision:s.version,ids:test.ids,style:test.style});await settle();}
   let s=await state();const indices=test.ids.map(id=>s.slides[0].elements.findIndex(e=>e.id===id));await page.evaluate(({ids,indices})=>editorAction({action:'select-elements',elementIndices:indices,elementsData:ids.map(id=>({id}))}),{ids:test.ids,indices});await settle();s=await state();const before=JSON.stringify(s.slides[0].elements);
   const hit=page.locator('.keynope-canvas-element[data-element="'+indices[0]+'"]');const box=await hit.boundingBox();assert(box,'Missing canvas hit');
   await page.evaluate(({ids,native})=>{
    const host=document.querySelector('#stage .keynope-modern-scene');
    window.dragProof={ids,host,owners:new Map([...host.querySelectorAll('[data-object-id]')].map(n=>[n.dataset.objectId,n])),positions:new Map(ids.map(id=>[id,host.querySelector('[data-object-id="'+id+'"]').getBoundingClientRect().x])),images:[...host.querySelectorAll('img')],actions:[],previews:0,samples:[],pointerAt:0,view:modernPageView,originalUpdate:modernPageView.update,originalPositions:modernPageView.previewPositions};
    const p=dragProof;p.pointer=event=>{p.pointerAt=performance.now();p.pointerTrusted=event.isTrusted;if(event.isTrusted)p.pointerId=event.pointerId;};window.addEventListener('pointermove',p.pointer,true);
    const measure=(operation,run)=>(...input)=>{
     const start=performance.now(),pointer=p.pointerAt,result=run(...input);
     if(operation==='positions')p.lastPreviewPositions=input[0].map(position=>({...position}));
     if(pointer){const sample={operation,trustedPointer:!!p.pointerTrusted,routeCount:input[1]?.length||0,updateMS:performance.now()-start,eventToUpdateMS:start-pointer};p.samples.push(sample);requestAnimationFrame(()=>sample.eventToNextFrameMS=performance.now()-pointer);}
     return result;
    };
    modernPageView.update=measure('scene',p.originalUpdate);
    modernPageView.previewPositions=measure('positions',p.originalPositions);
    const record=(method,url,body)=>{if(method==='POST'&&String(url).endsWith('/action'))p.actions.push(JSON.parse(body).action);if(String(url).endsWith('/preview'))p.previews++;};
    p.originalFetch=window.fetch;
    if(native){window.fetch=(url,options)=>{record(options?.method||'GET',url,options?.body);return p.originalFetch.call(window,url,options);};}
    else{p.originalRequest=window.keynopeWasmRequest;window.keynopeWasmRequest=(...args)=>{record(...args);return p.originalRequest(...args);};}
   },{ids:test.ids,native:!!nativeURL});
   await page.evaluate(()=>dragProof.sourceJSON=JSON.stringify(deck.pages[pageIndex].scene));
   if(test.delay)await page.evaluate(()=>{const p=dragProof;p.delayedOriginalFetch=window.fetch;p.waiting=0;p.gate=new Promise(resolve=>p.release=resolve);window.fetch=async(...args)=>{const result=await p.delayedOriginalFetch.apply(window,args);if(String(args[0]).endsWith('/connector-draft')){p.waiting++;await p.gate;}return result;};});
   const painted=await page.locator('#stage .keynope-modern-scene [data-object-id="'+test.ids[0]+'"]').boundingBox();
   // Dense fixtures deliberately overlap text and shapes. Pick a real exposed
   // hit region, not the covered centre (which selects text in WebKit).
   const point=await hit.evaluate((hit,box)=>{
    for(const fy of [.5,.2,.8,.1,.9])for(const fx of [.5,.2,.8,.1,.9]){
     const x=box.x+box.width*fx,y=box.y+box.height*fy,target=document.elementFromPoint(x,y);
     if(target?.closest('.keynope-canvas-element')===hit&&!target.closest('.keynope-resize-handle'))return {x,y};
    }
    throw Error('No exposed pointer region for '+hit.dataset.elementId);
   },painted);
   await page.keyboard.down('Alt');const {x,y}=point;await page.mouse.move(x,y);await page.mouse.down();await page.mouse.move(x+31,y+9,{steps:12});await settle();
   const foreign=await page.evaluate(index=>{const p=dragProof,node=p.host.querySelector('[data-object-id="'+p.ids[0]+'"]'),before=node.getBoundingClientRect().x,hit=document.querySelector('.keynope-canvas-element[data-element="'+index+'"]');for(const type of ['pointermove','pointerup','pointercancel'])hit.dispatchEvent(new PointerEvent(type,{pointerId:77,clientX:1000,clientY:900,bubbles:true}));return Math.abs(node.getBoundingClientRect().x-before)<.1;},indices[0]);assert(foreign,'Foreign pointer moved the active selection');
   const burst=await page.evaluate(({index,x,y})=>{const p=dragProof,before=p.samples.length,hit=document.querySelector('.keynope-canvas-element[data-element="'+index+'"]');for(let i=1;i<=20;i++)hit.dispatchEvent(new PointerEvent('pointermove',{pointerId:p.pointerId,clientX:x+31+i,clientY:y+9,bubbles:true}));return {before,events:20};},{index:indices[0],x,y});await settle();
   if(test.ids.includes('from')&&test.finish==='commit')await page.waitForFunction(()=>dragProof.samples.some(sample=>sample.routeCount>0));
   const during=await page.evaluate(before=>{const p=dragProof;return {moves:p.ids.map(id=>p.host.querySelector('[data-object-id="'+id+'"]').getBoundingClientRect().x-p.positions.get(id)),owners:[...p.owners].every(([id,node])=>p.host.querySelector('[data-object-id="'+id+'"]')===node),images:p.images.every(node=>p.host.contains(node)),actions:p.actions.slice(),previews:p.previews,burstUpdates:p.samples.length-before,samples:p.samples.slice()};},burst.before);
   if(!during.moves.every(x=>x>15))await page.screenshot({path:output+'/failed-'+test.ids.join('-')+'.png'});
   assert(during.moves.every(x=>x>15),'Only selection bounds moved, not rendered objects '+test.ids+': '+JSON.stringify(during));assert(during.owners&&during.images,'Gesture remounted objects/media');assert.equal(JSON.stringify((await state()).slides[0].elements),before,'Pointer preview changed authored elements');assert(!during.actions.includes('update-elements'),'Gesture committed before release');assert.equal(during.previews,0,'Move requested a full-page mutation preview');
   if(test.ids.length>1)assert(during.moves.every(x=>Math.abs(x-during.moves[0])<.1),'Members did not move rigidly together');
   assert(await page.evaluate(()=>JSON.stringify(deck.pages[pageIndex].scene)===dragProof.sourceJSON),'Live preview mutated committed scene data');
   if(test.ids.every(id=>id.startsWith('dense-text'))){assert.equal(during.burstUpdates,1,'Pointer burst was not coalesced to one frame');assert(during.samples.every(sample=>sample.operation==='positions'),'Text dragging reconciled a full scene');}assert(during.samples.length,'No live renderer updates measured');
   if(test.ids.includes('from')&&test.finish==='commit'){
    assert(during.samples.some(sample=>sample.routeCount>0),'Connected drag never applied an authoritative route response');
    assert(during.samples.every(sample=>sample.operation==='positions'),'Vector route response reconciled unrelated scene objects');
   }
   if(test.finish==='commit')await page.screenshot({path:output+'/live-'+test.ids.join('-')+'.png'});
   if(test.delay)await page.waitForFunction(()=>dragProof.waiting>0);
   if(test.finish==='escape')await page.keyboard.press('Escape');else if(test.finish==='blur')await page.evaluate(()=>window.dispatchEvent(new Event('blur')));
   if(test.delay){await page.evaluate(()=>dragProof.release());await settle();}
   await page.mouse.up();await page.keyboard.up('Alt');await settle();
   if(test.finish==='commit'){
    await waitForAsync(page,async before=>JSON.stringify((await fetch('/api/editor/state').then(r=>r.json())).slides[0].elements)!==before,before);
    await page.evaluate(()=>queueEditorOperation(()=>true));await settle();
    assert(await page.evaluate(()=>dragProof.lastPreviewPositions?.length===dragProof.ids.length&&dragProof.lastPreviewPositions.every(position=>{const object=deck.pages[pageIndex].scene.objects.find(object=>object.id===position.id);return object&&Math.abs(object.bounds.x-position.x)<1e-6&&Math.abs(object.bounds.y-position.y)<1e-6;})),'Authoritative release jumped from the final pointer preview');
    assert.equal(await page.evaluate(()=>dragProof.actions.filter(a=>a==='update-elements').length),1,'Move did not commit as one atomic command');await page.getByRole('button',{name:'Undo',exact:true}).click();await settle();assert.equal(JSON.stringify((await state()).slides[0].elements),before,'Move Undo was not atomic');
   }else{
    assert.equal(JSON.stringify((await state()).slides[0].elements),before,'Cancelled drag changed the document');assert.equal(await page.evaluate(()=>dragProof.actions.filter(a=>a==='update-elements').length),0);const restored=await page.evaluate(()=>dragProof.ids.every(id=>Math.abs(dragProof.host.querySelector('[data-object-id="'+id+'"]').getBoundingClientRect().x-dragProof.positions.get(id))<.1));assert(restored,'Cancelled drag did not restore rendered content');
   }
   await page.evaluate(()=>{if(dragProof.originalRequest)window.keynopeWasmRequest=dragProof.originalRequest;dragProof.view.update=dragProof.originalUpdate;dragProof.view.previewPositions=dragProof.originalPositions;window.fetch=dragProof.originalFetch;window.removeEventListener('pointermove',dragProof.pointer,true);});records.push({...test,...during});
  }
  assert.deepEqual(errors,[]);await page.screenshot({path:output+'/dense-drag.png'});fs.writeFileSync(output+'/report.json',JSON.stringify({transport:nativeURL?'native-http':'wasm',browser:browser.version(),records},null,2));console.log('PASS '+(nativeURL?'native-http':'wasm')+' '+engine+': live single/multi movement; retained 49 owners/media; no preview commits; atomic release/Undo; Escape/blur restore');
 }finally{await browser.close();server.close();}
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
