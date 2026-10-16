// Pure geometry/ownership checks plus real production WASM marquee gestures.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const {waitForAsync}=require('./wait_for_async.cjs');require('../web/workspace.js');const W=globalThis.KeynopeWorkspace;
const b={minX:40,minY:24,maxX:60,maxY:26,rotation:45};
assert(W.boxIntersectsRect(b,{minX:49.9,minY:24.9,maxX:50.1,maxY:25.1},2));
assert(!W.boxIntersectsRect(b,{minX:57,minY:21,maxX:58,maxY:22},2),'Rotated AABB empty corner selected');
assert(W.boxIntersectsRect(b,{minX:49.9,minY:10,maxX:50.1,maxY:40},2),'Crossing rectangles without contained vertices missed');
assert(!W.boxIntersectsRect({minX:0,minY:0,maxX:1,maxY:1},{minX:1,minY:0,maxX:2,maxY:1}),'Touching edges count as area');
assert(!W.boxIntersectsRect({...b,rotation:Infinity},{minX:0,minY:0,maxX:100,maxY:100},2));
assert(!W.boxIntersectsRect(b,{minX:1,minY:1,maxX:1,maxY:5},2));
const elements=[{query:'group=g'},{query:'group=g'},{query:'group=g&object-hidden=1'},{query:'group=g&object-locked=1'}],bounds=new Map([[0,{minX:10,minY:10,maxX:20,maxY:20}],[1,{minX:100,minY:10,maxX:110,maxY:20}],[2,{minX:50,minY:10,maxX:60,maxY:20}],[3,{minX:70,minY:10,maxX:80,maxY:20}]]);
const units=W.marqueeSelectionUnits(elements,bounds,[0]);
for(const x of [50,70])assert.deepEqual([...W.marqueeCandidates(units,{minX:x,minY:12,maxX:x+2,maxY:14},2)],[],'Empty/hidden/locked group region selected');
assert.deepEqual([...W.marqueeCandidates(units,{minX:11,minY:12,maxX:12,maxY:14},2)],[0,1,2,3],'Group identity not retained');
const entered=W.marqueeSelectionUnits(elements,bounds,[0,1],'g');assert.deepEqual([...W.marqueeCandidates(entered,{minX:11,minY:12,maxX:12,maxY:14},2)],[0]);
assert.deepEqual([...W.marqueeCandidates(units,{minX:50,minY:12,maxX:52,maxY:14},2,[7])],[7],'Additive selection lost');
bounds.get(0).minX=500;elements[0].query='object-hidden=1';assert.equal(units[0].boxes[0].minX,10,'Captured bounds alias source');assert.equal(units[0].boxes.length,2,'Captured visibility alias source');
let reads=0,gets=0;class CountedBounds extends Map{get(key){gets++;return super.get(key);}}
const dense=Array.from({length:2000},(_,i)=>({get query(){reads++;return '';},id:'dense-'+i})),denseBounds=new CountedBounds(dense.map((_,i)=>[i,{minX:i%200,minY:Math.floor(i/200)*4,maxX:i%200+.5,maxY:Math.floor(i/200)*4+1}]));
const captured=W.marqueeSelectionUnits(dense,denseBounds,dense.map((_,i)=>i));assert.equal(reads,2000);assert.equal(gets,2000);
for(let frame=0;frame<30;frame++)W.marqueeCandidates(captured,{minX:0,minY:0,maxX:50+frame,maxY:30},2);
assert.equal(reads,2000,'Pointer frames reparsed source queries');assert.equal(gets,2000,'Pointer frames rebuilt source bounds');

const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright'),root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site');
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.html':'text/html','.css':'text/css','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 for(const engine of (process.env.TEST_BROWSER?[process.env.TEST_BROWSER]:['chromium','webkit'])){
  const browser=await pw[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
  try{
   const context=await browser.newContext({viewport:{width:1500,height:1000}});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
   await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
   await page.evaluate(async()=>{const r=JSON.parse(keynopeWasmInit('<!-- keynope width=245 height=56 -->\n\nOwned marquee proof','Marquee.md',false));if(r.status!==200)throw Error(r.body);await keynopeReloadWebDocument(JSON.parse(JSON.parse(keynopeWasmWorkspace()).body));});
   const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json())),action=data=>page.evaluate(data=>editorAction(data),data);
   await page.evaluate(()=>{const original=window.fetch.bind(window);window.marqueeActions=[];window.fetch=async(input,options)=>{const response=await original(input,options);if(String(input)==='/api/editor/action'){const data=await response.clone().json();marqueeActions.push({request:JSON.parse(options.body),status:response.status,version:data.version,selection:data.selection});}return response;};});
   const settle=()=>page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));
   for(const master of [false,true]){
    if(master)await action({action:'toggle-master-mode'});
    const ids=[];
    for(const [x,y,w,h,extras]of [[10,8,20,6,{}],[80,8,20,6,{}],[130,22,40,4,{'object-rotation':'45'}],[10,35,20,6,{'object-locked':'1'}],[45,35,20,6,{'object-hidden':'1'}]]){
     await action({action:'add-element',kind:'shape',name:'square'});const s=await state(),e=s.slides[s.current].elements[s.selected];ids.push(e.id);await action({action:'update-element',element:s.selected,elementData:{...e,query:new URLSearchParams({shape:'square',left:String(x),top:String(y),width:String(w),height:String(h),...extras}).toString()}});
    }
    const grouping=await state(),groupIndices=ids.slice(0,2).map(id=>grouping.slides[grouping.current].elements.findIndex(e=>e.id===id));await action({action:'select-elements',elementIndices:groupIndices,elementsData:groupIndices.map(i=>grouping.slides[grouping.current].elements[i])});await action({action:'group-elements',elementData:{id:ids[0]}});
    const start=async(box,reverse=false)=>{const rect=await page.locator('.keynope-canvas-overlay').boundingBox(),point=(x,y)=>({x:rect.x+x*rect.width/245,y:rect.y+y*rect.height/56});const a=point(reverse?box.maxX:box.minX,reverse?box.maxY:box.minY),b=point(reverse?box.minX:box.maxX,reverse?box.minY:box.maxY);await page.keyboard.down('Shift');await page.mouse.move(a.x,a.y);await page.mouse.down();await page.mouse.move(b.x,b.y,{steps:4});await settle();return b;};
    const check=async(box,want,preview,reverse=false)=>{
     await action({action:'select-element',element:-1});const before=await state();await start(box,reverse);await page.locator('.keynope-canvas-marquee').waitFor({state:'visible'});assert.equal(await page.locator('.marquee-candidate').count(),preview);assert.deepEqual((await state()).slides,before.slides);
     await page.mouse.up();await page.keyboard.up('Shift');await waitForAsync(page,async({version,want})=>{const s=await fetch('/api/editor/state').then(r=>r.json()),ids=s.selection.map(i=>s.slides[s.current].elements[i].id).sort();return s.version>version&&JSON.stringify(ids)===JSON.stringify([...want].sort());},{version:before.version,want}).catch(async error=>{const s=await state();console.error(JSON.stringify({engine,master,box,want,beforeVersion:before.version,version:s.version,selection:s.selection,elements:s.slides[s.current].elements.map(e=>({id:e.id,query:e.query})),actions:await page.evaluate(()=>marqueeActions.slice(-4)),errors}));throw error;});assert.equal((await state()).mutationRevision,before.mutationRevision);
    };
    await check({minX:45,minY:9,maxX:55,maxY:13},[],0);
    await check({minX:12,minY:9,maxX:16,maxY:12},ids.slice(0,2),1,true);
    await check({minX:165.5,minY:17.5,maxX:166.5,maxY:18.5},[],0);
    await check({minX:149,minY:23,maxX:151,maxY:25},[ids[2]],1,true);
    await check({minX:9,minY:34,maxX:66,maxY:42},[],0);
    // Additive selection retains an already-selected group when a second
    // rotated member is swept, without selecting empty group space.
    await action({action:'select-element',elementData:{id:ids[0]}});const selected=await state();await start({minX:149,minY:23,maxX:151,maxY:25});assert.equal(await page.locator('.marquee-candidate').count(),2);await page.keyboard.press('Escape');await page.mouse.up();await page.keyboard.up('Shift');assert.deepEqual((await state()).selection,selected.selection);assert.deepEqual((await state()).slides,selected.slides);
    await action({action:'select-element',element:-1});await start({minX:149,minY:23,maxX:151,maxY:25});
    await page.evaluate(()=>{for(const type of ['pointermove','pointerup','pointercancel'])window.dispatchEvent(new PointerEvent(type,{pointerId:999,clientX:1,clientY:1,bubbles:true}));});assert.equal(await page.locator('.keynope-canvas-marquee').count(),1,'Foreign pointer cancelled the marquee');
    const beforeMutation=await state(),outside=beforeMutation.slides[beforeMutation.current].elements.find(e=>e.id===ids[1]);await action({action:'update-element',elementData:{...outside,query:outside.query+'&fg=%23ff55aa'},element:beforeMutation.slides[beforeMutation.current].elements.findIndex(e=>e.id===outside.id)});const changed=await state();await page.mouse.up();await page.keyboard.up('Shift');await settle();assert.deepEqual((await state()).selection,changed.selection,'Stale marquee replaced newer selection');assert.equal(await page.locator('.keynope-canvas-marquee').count(),0);
    await action({action:'select-element',element:-1});await start({minX:149,minY:23,maxX:151,maxY:25});await page.screenshot({path:'/tmp/keynope-marquee-geometry-'+engine+'-'+(master?'master':'normal')+'.png'});await page.mouse.up();await page.keyboard.up('Shift');await settle();
   }
   assert.deepEqual(errors,[]);console.log('PASS '+engine+': captured linear marquee geometry, rotated/group empty-space hit tests, visibility, additive preview, Escape, foreign-pointer and stale-gesture handling in normal/master views');
  }finally{await browser.close();}
 }
})().catch(e=>{console.error(e);process.exitCode=1;}).finally(()=>server.close());
