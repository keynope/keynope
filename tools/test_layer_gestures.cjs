const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const {waitForAsync}=require('./wait_for_async.cjs');require('../web/workspace.js');
const owner={epoch:1,owner:'document-a',slide:0,master:false,group:'',mutationRevision:4,revision:8};
assert(KeynopeWorkspace.sameLayerContext(owner,{...owner,revision:9}),'Selection-only version cancelled layer scope');
for(const [key,value]of Object.entries({epoch:2,owner:'document-b',slide:1,master:true,group:'group',mutationRevision:5}))assert(!KeynopeWorkspace.sameLayerContext(owner,{...owner,[key]:value}),key+' not captured');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright'),root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site');
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.html':'text/html','.css':'text/css','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
const elements=['a','b','c','d'].map((id,i)=>({id,kind:'text',text:'Object '+id.toUpperCase(),query:'render=truetype&modern-font=mono&modern-size=80&left=10&top='+(4+i*10)+'&width=60&height=6&text-box=1'}));
const masters={version:1,base:{id:'base',name:'Base',slide:{elements}},layouts:[{id:'blank',name:'Blank',slide:{elements:elements.map(e=>({...e,text:'Other layout '+e.id}))}}]};
const markup=list=>list.map(e=>'<!-- element-id='+e.id+' '+e.query.replaceAll('&',' ')+' -->\n'+e.text).join('\n\n');
const fixture='<!-- keynope width=245 height=56 -->\n<!-- keynope-masters version=1 base64:'+Buffer.from(JSON.stringify(masters)).toString('base64')+' -->\n\n'+markup(elements)+'\n\n---\n'+markup(elements.map(e=>({...e,text:'Other slide '+e.id})))+'\n';
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 for(const engine of (process.env.TEST_BROWSER?[process.env.TEST_BROWSER]:['chromium','webkit'])){
  const browser=await pw[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
  try{
   const context=await browser.newContext({viewport:{width:1500,height:1000}});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
   await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
   const open=content=>page.evaluate(async content=>{const r=JSON.parse(keynopeWasmInit(content,'Owned layers.md',false));if(r.status!==200)throw Error(r.body);await keynopeReloadWebDocument(JSON.parse(JSON.parse(keynopeWasmWorkspace()).body));},content);
   const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json())),action=data=>page.evaluate(data=>editorAction(data),data),row=id=>page.locator('[data-object-key="'+id+'"]');
   const settle=()=>page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));
   const untilMutation=version=>waitForAsync(page,async version=>(await fetch('/api/editor/state').then(r=>r.json())).mutationRevision>version,version);
   const order=()=>page.locator('#keynope-object-navigator [data-object-key]').evaluateAll(nodes=>nodes.map(n=>n.dataset.objectKey));
   await page.evaluate(()=>{const original=window.fetch.bind(window);window.layerResponses=[];window.fetch=async(input,options)=>{const response=await original(input,options);if(String(input)==='/api/editor/action'){let data={};try{data=JSON.parse(await response.clone().text());}catch{}layerResponses.push({request:JSON.parse(options.body),status:response.status,version:data.version});}return response;};document.addEventListener('pointerdown',event=>{if(event.target.closest?.('[data-object-key]'))window.layerPointerID=event.pointerId;},true);});
   const drag=async(source='a',target='c')=>{await row(source).scrollIntoViewIfNeeded();const a=await row(source).boundingBox(),b=await row(target).boundingBox();await page.mouse.move(a.x+a.width/2,a.y+a.height/2);await page.mouse.down();await page.mouse.move(b.x+b.width/2,b.y+b.height-3,{steps:4});await page.locator('.layer-dragging').waitFor();};
   const responses=()=>page.evaluate(()=>layerResponses.filter(item=>item.request.action==='move-object-layer'));
   for(const master of [false,true]){
    await open(fixture);if(master)await action({action:'toggle-master-mode'});await page.getByRole('tab',{name:'Objects',exact:true}).click();
    assert.deepEqual(await order(),['a','b','c','d']);await row('a').click();await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[s.current].elements[s.selected]?.id==='a';});
    const before=await state();await drag();
    // Selection-only traffic must retain the source row and pointer capture.
    await action({action:'select-element',elementData:{id:'b'}});assert.equal(await row('a').evaluate(n=>n.hasPointerCapture(window.layerPointerID)),true);
    await row('a').evaluate(n=>{for(const type of ['pointermove','pointerup','pointercancel'])n.dispatchEvent(new PointerEvent(type,{pointerId:999,clientX:1,clientY:1,bubbles:true}));});assert.equal(await page.locator('.layer-dragging').count(),1);
    await page.mouse.up();await untilMutation(before.mutationRevision);await settle();assert.deepEqual(await order(),['b','c','a','d']);const moved=await state();assert.equal(moved.slides[moved.current].elements[moved.selected].id,'b','Layer drag changed independent selection');
    const request=(await responses()).at(-1).request;assert.equal(request.sceneMaster,master);assert.equal(request.notesSessionId,before.notesSessionId);assert.equal(request.mutationRevision,before.mutationRevision);assert.equal(moved.mutationRevision,before.mutationRevision+1);
    await action({action:'undo'});assert.deepEqual((await state()).slides,before.slides);await action({action:'redo'});assert.deepEqual((await state()).slides,moved.slides);await action({action:'undo'});
    // Keyboard stack movement keeps its stable target even after paint order changed.
    await row('a').focus();const keyboardBefore=await state();await row('a').press('Alt+ArrowDown');await untilMutation(keyboardBefore.mutationRevision);await settle();assert.deepEqual(await order(),['b','a','c','d']);await action({action:'undo'});
    const boundary=await state();await row('a').focus();await row('a').press('Alt+ArrowUp');await page.evaluate(()=>queueEditorOperation(()=>true));await settle();assert.equal((await state()).version,boundary.version);assert.deepEqual((await state()).slides,boundary.slides);
    for(const cancel of ['escape','blur','pointercancel','capture']){
     const clean=await state();await drag();if(cancel==='escape')await page.keyboard.press('Escape');if(cancel==='blur')await page.evaluate(()=>window.dispatchEvent(new Event('blur')));if(cancel==='pointercancel')await row('a').evaluate(n=>n.dispatchEvent(new PointerEvent('pointercancel',{pointerId:window.layerPointerID,bubbles:true})));if(cancel==='capture')await row('a').evaluate(n=>n.releasePointerCapture(window.layerPointerID));await page.mouse.up();await settle();assert.equal(await page.locator('.layer-dragging').count(),0);assert.deepEqual((await state()).slides,clean.slides,engine+' '+master+' '+cancel+' mutated layers');assert.equal((await state()).version,clean.version,engine+' '+master+' '+cancel+' advanced revision');
     await row('b').focus();await row('b').press('Enter');await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[s.current].elements[s.selected]?.id==='b';});
    }
    await drag();const changedBefore=await state(),element=changedBefore.slides[changedBefore.current].elements.find(e=>e.id==='d');await action({action:'update-element',element:changedBefore.slides[changedBefore.current].elements.findIndex(e=>e.id==='d'),elementData:{...element,text:'Changed while dragging'}});const changed=await state(),count=(await responses()).length;await page.mouse.up();await settle();assert.deepEqual((await state()).slides,changed.slides);assert.equal((await state()).version,changed.version);assert.equal((await responses()).length,count,'Stale drag submitted a reorder');
    // Locked objects remain selectable/unlockable but cannot begin a layer drag.
    await row('a').click();await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[s.current].elements[s.selected]?.id==='a';});await action({action:'set-object-lock',value:1});const locked=await state(),a=await row('a').boundingBox(),c=await row('c').boundingBox();await page.mouse.move(a.x+a.width/2,a.y+a.height/2);await page.mouse.down();await page.mouse.move(c.x+c.width/2,c.y+c.height-3,{steps:3});assert.equal(await page.locator('.layer-dragging').count(),0);await page.mouse.up();assert.deepEqual((await state()).slides,locked.slides);await action({action:'select-element',elementData:{id:'a'}});await action({action:'set-object-lock',value:0});
    // A queued request can race native navigation without the GUI observing it.
    // Either the client cancels or the scoped write lock rejects; neither may
    // mutate the newly-current slide/master.
    await page.evaluate(()=>{queueEditorOperation(()=>new Promise(resolve=>window.releaseLayerQueue=resolve));});await drag();await page.mouse.up();
    await page.evaluate(async()=>{const response=await fetch('/api/editor/action',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'select-slide',slide:1})});if(!response.ok)throw Error(await response.text());});const navigation=await state();
    const queuedStart=(await responses()).length;await page.evaluate(()=>releaseLayerQueue());await page.evaluate(()=>queueEditorOperation(()=>true));await settle();const afterRace=await state();assert.equal(afterRace.version,navigation.version);assert.deepEqual(afterRace.slides,navigation.slides);assert((await responses()).slice(queuedStart).every(r=>r.status!==200),'Queued layer request mutated another target');await action({action:'select-slide',slide:0});
    // Replacing the document with the same stable IDs still cancels ownership.
    await page.getByRole('tab',{name:'Objects',exact:true}).click();await drag();const oldOwner=(await state()).notesSessionId;await open(fixture);const replacement=await state();assert.notEqual(replacement.notesSessionId,oldOwner);await page.mouse.up();await settle();assert.deepEqual((await state()).slides,replacement.slides);assert.equal((await state()).version,replacement.version);
    await page.getByRole('tab',{name:'Objects',exact:true}).click();await page.screenshot({path:'/tmp/keynope-layer-gestures-'+engine+'-'+(master?'master':'normal')+'.png'});
   }
   assert.deepEqual(errors,[]);console.log('PASS '+engine+': owned layer drag/keyboard/history, unchanged pointer capture, Escape/blur/capture, locks, no-op, stale mutation/navigation and same-ID document replacement');
  }finally{await browser.close();}
 }
})().catch(e=>{console.error(e);process.exitCode=1;}).finally(()=>server.close());
