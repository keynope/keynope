// Actual WASM pointer lifecycle for connected multi-object resize and shape
// adjustment, including non-mutating traffic and inherited-state cancellation.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const {waitForAsync}=require('./wait_for_async.cjs');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site');
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));const browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
 try{
  const context=await browser.newContext({viewport:{width:1500,height:1000}});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json())),action=data=>page.evaluate(data=>editorAction(data),data);
  await waitForAsync(page,async()=>{await Promise.resolve();window.pollProof=(window.pollProof||0)+1;return window.pollProof>=3;});assert.equal(await page.evaluate(()=>pollProof),3,'Async polling stopped on a false Promise');
  const until=async predicate=>{const deadline=Date.now()+10000;while(!await predicate()){if(Date.now()>deadline)throw Error('Timed out awaiting gesture state');await new Promise(resolve=>setTimeout(resolve,40));}};
  const picker=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await picker).setFiles({name:'Owned gestures.md',mimeType:'text/markdown',buffer:Buffer.from('<!-- keynope width=245 height=56 -->\n\n<!-- top=2 -->\nOwned gestures\n')});
  await until(async()=>(await state()).path.endsWith('Owned gestures.md'));
  await page.evaluate(()=>{window.gestureActions=[];const original=window.fetch.bind(window);window.fetch=async(input,init={})=>{const response=await original(input,init);if(String(input)==='/api/editor/action'){const result=await response.clone().json();gestureActions.push({request:JSON.parse(init.body),status:response.status,version:result.version,mutationRevision:result.mutationRevision});}return response;};});
  const start=async locator=>{await locator.waitFor({state:'visible'});const r=await locator.boundingBox();assert(r);const p={x:r.x+r.width/2,y:r.y+r.height/2};await page.mouse.move(p.x,p.y);await page.mouse.down();return p;};
  const select=async ids=>{const s=await state(),elements=s.slides[s.current].elements,indices=ids.map(id=>elements.findIndex(e=>e.id===id));await action({action:'select-elements',elementIndices:indices,elementsData:indices.map(i=>elements[i]),element:indices[0]});};
  const editTheme=async()=>{const s=await state(),theme=structuredClone(s.theme);assert(theme);theme.colors.accent[0]=theme.colors.accent[0]==='#ff55aa'?'#55ffaa':'#ff55aa';await action({action:'set-deck-theme',themeData:theme});};
  for(const master of [false,true]){
   if(master)await action({action:'toggle-master-mode'});
   const ids=[];
   for(const [shape,x,y,w,h]of [['arrow-right',35,12,50,12],['square',125,28,45,14]]){await action({action:'add-element',kind:'shape',name:shape});const s=await state(),e=s.slides[s.current].elements[s.selected];ids.push(e.id);await action({action:'update-element',element:s.selected,elementData:{...e,query:new URLSearchParams({shape,left:String(x),top:String(y),width:String(w),height:String(h),fg:'#55aaff','shape-depth':'0.4'}).toString()}});}
   const cq=new URLSearchParams({'connector-from':ids[0],'connector-from-side':'right','connector-to':ids[1],'connector-to-side':'left','connector-mode':'elbow'});
   await action({action:'add-element',kind:'connector',elementData:{kind:'connector',query:cq.toString()}});
   await select(ids);const handle=page.locator('.keynope-selection-resize .keynope-resize-handle.se');
   const before=await state(),original=before.slides[before.current].elements.find(e=>e.id===ids[0]),owner=page.locator('#stage [data-object-id="'+ids[0]+'"]'),width=await owner.evaluate(n=>n.style.width),p=await start(handle);
   await page.mouse.move(p.x+16,p.y+7,{steps:3});await page.waitForFunction(({id,width})=>document.querySelector('#stage [data-object-id="'+id+'"]')?.style.width!==width,{id:ids[0],width});assert.deepEqual((await state()).slides,before.slides,'Group preview mutated authored state');
   await select(ids);const selected=await state();assert.equal(selected.mutationRevision,before.mutationRevision);
   await handle.evaluate(n=>{for(const type of ['pointermove','pointerup','pointercancel'])n.parentElement.dispatchEvent(new PointerEvent(type,{pointerId:999,clientX:1,clientY:1,bubbles:true}));});
   assert.deepEqual((await state()).slides,before.slides,'Foreign pointer ended the group gesture');await page.mouse.up();
   await until(async()=>{const s=await state();return s.mutationRevision===selected.mutationRevision+1&&s.slides[s.current].elements.find(e=>e.id===ids[0])?.query!==original.query;}).catch(async error=>{console.error('Release diagnostic',JSON.stringify({master,state:await state(),actions:await page.evaluate(()=>gestureActions.slice(-4)),errors}));throw error;});
   await select(ids); // Drain the command response/workspace publication too.
   const after=await state(),changed=after.slides[after.current].elements.find(e=>e.id===ids[0]);assert.notEqual(changed.query,original.query,JSON.stringify({master,versions:[before.version,selected.version,after.version],mutations:[before.mutationRevision,selected.mutationRevision,after.mutationRevision],errors,selected:after.selection,actions:await page.evaluate(()=>gestureActions.slice(-8)),notice:await page.locator('.keynope-command-error').textContent()}));
   await action({action:'undo'});assert.deepEqual((await state()).slides,before.slides);await action({action:'redo'});assert.deepEqual((await state()).slides,after.slides);await action({action:'undo'});
   // Private autosaves must neither cancel the captured gesture nor freeze its
   // live preview when a master response replaces an equivalent scene owner.
   await select(ids);const noteBefore=await state(),np=await start(handle);
   await page.mouse.move(np.x+8,np.y+4);
   await action({action:'update-slide-notes',slide:noteBefore.current,notes:'Notes saved during resize'});
   const noted=await state();assert.equal(noted.mutationRevision,noteBefore.mutationRevision);
   const noteWidth=await owner.evaluate(n=>n.style.width);
   await page.mouse.move(np.x+28,np.y+12,{steps:3});
   await page.waitForFunction(({id,width})=>document.querySelector('#stage [data-object-id="'+id+'"]')?.style.width!==width,{id:ids[0],width:noteWidth});
   await page.mouse.up();await until(async()=>(await state()).mutationRevision===noted.mutationRevision+1);
   await select(ids);assert.equal((await state()).slides[noteBefore.current].notes,'Notes saved during resize');
   await action({action:'undo'});assert.deepEqual((await state()).slides,noted.slides,'Resize Undo removed autosaved notes');
   await action({action:'undo'});assert.deepEqual((await state()).slides,noteBefore.slides,'Notes Undo changed objects');
   for(const cancel of ['escape','blur','capture']){await select(ids);const s=await state(),p=await start(handle);await page.mouse.move(p.x+13,p.y+6);if(cancel==='escape')await page.keyboard.press('Escape');if(cancel==='blur')await page.evaluate(()=>window.dispatchEvent(new Event('blur')));if(cancel==='capture')await handle.evaluate(n=>n.parentElement.dispatchEvent(new PointerEvent('lostpointercapture',{pointerId:1,bubbles:true})));await page.mouse.up();assert.deepEqual((await state()).slides,s.slides,cancel+' changed group document');assert.equal((await state()).mutationRevision,s.mutationRevision);}
   // A theme mutation need not change either selected authored element. Once
   // observed, it must invalidate the gesture rather than restore old artwork.
   await select(ids);const staleBefore=await state(),p2=await start(handle);await page.mouse.move(p2.x+15,p2.y+8);await editTheme();const themed=await state();assert.deepEqual(themed.slides,staleBefore.slides);await page.keyboard.press('Escape');await page.mouse.up();assert.equal((await state()).mutationRevision,themed.mutationRevision);assert.deepEqual((await state()).theme,themed.theme);await action({action:'undo'});
   await select([ids[0]]);const adjustment=page.getByRole('slider',{name:'Adjust head depth',exact:true}),q=await start(adjustment),adjustBefore=await state();await page.mouse.move(q.x-12,q.y,{steps:3});
   await select([ids[0]]);const adjustSelected=await state();assert.equal(adjustSelected.mutationRevision,adjustBefore.mutationRevision);await page.mouse.up();
   await until(async()=>(await state()).mutationRevision===adjustBefore.mutationRevision+1);const adjusted=await state();assert.notEqual(adjusted.slides[adjusted.current].elements.find(e=>e.id===ids[0]).query,adjustBefore.slides[adjustBefore.current].elements.find(e=>e.id===ids[0]).query);await action({action:'undo'});assert.deepEqual((await state()).slides,adjustBefore.slides);
   await select([ids[0]]);const q2=await start(adjustment),staleAdjustment=await state();await page.mouse.move(q2.x-10,q2.y);await editTheme();const newTheme=await state();assert.deepEqual(newTheme.slides,staleAdjustment.slides);await page.mouse.up();assert.equal((await state()).mutationRevision,newTheme.mutationRevision,'Stale shape adjustment committed');await action({action:'undo'});
  }
  assert.deepEqual(errors,[]);await page.screenshot({path:'/tmp/keynope-group-gesture-ownership.png'});console.log('PASS: normal/master connected group resize and shape adjustment, live isolation, selection-only traffic, foreign pointer, Escape/blur/capture cancellation, stale inherited edits and one-step history.');
 }finally{await browser.close();server.close();}
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
