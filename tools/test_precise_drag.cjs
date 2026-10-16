// Continuous pointer movement, fractional guides and rigid group/history
// behaviour against the production WASM editor, with no external requests.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const {waitForAsync}=require('./wait_for_async.cjs');
require('../web/workspace.js');
const W=globalThis.KeynopeWorkspace,near=(a,b,message)=>assert(Math.abs(a-b)<1e-7,message+': '+a+' != '+b);
assert.deepEqual(W.pointerMove(1,2,1000,500,245,56),{dx:0,dy:0,engaged:false});
let delta=W.pointerMove(7,4,1000,500,245,56);near(delta.dx,1.715,'continuous X');near(delta.dy,.448,'continuous Y');assert(delta.engaged);
delta=W.pointerMove(1,1,1000,500,245,56,true);near(delta.dx,.245,'engaged return');near(delta.dy,.112,'engaged return Y');
assert.deepEqual(W.pointerMove(3,0,0,500,245,56),{dx:0,dy:0,engaged:false});
const box={minX:10.25,maxX:20.25,minY:10,maxY:20},target={minX:30.6,maxX:40.6,minY:30,maxY:40};
near(W.snapMove(box,[target],20.2,0,.5,.1,0).dx,20.35,'fractional edge snap');
near(W.snapMove(box,[target],20.2,0,.5,.1).dx,20.2,'legacy stepped API');
const spaced=W.snapMove({minX:10,maxX:20,minY:10,maxY:20},[{minX:0,maxX:9.2,minY:10,maxY:20},{minX:30,maxX:40,minY:10,maxY:20}],4.8,0,.3,.01,0);
near(spaced.dx,4.6,'fractional equal gaps');assert(spaced.guides.some(g=>g.spacing));
const clamped=W.clampMove([{minX:220.2,maxX:244.7,minY:10,maxY:20},{minX:20,maxX:35,minY:40,maxY:55.8}],1,.5,245,56,0);
near(clamped.dx,.3,'continuous slide edge');near(clamped.dy,.2,'continuous slide bottom');

const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright'),root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site');
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.html':'text/html','.css':'text/css','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 for(const engine of (process.env.TEST_BROWSER?[process.env.TEST_BROWSER]:['chromium','webkit'])){
  const browser=await pw[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
  try{
   const context=await browser.newContext({viewport:{width:1500,height:1000}});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());
   const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
   await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
   await page.evaluate(async()=>{const r=JSON.parse(keynopeWasmInit('<!-- keynope width=245 height=56 -->\n\nOwned drag proof','Precise drag.md',false));if(r.status!==200)throw Error(r.body);await keynopeReloadWebDocument(JSON.parse(JSON.parse(keynopeWasmWorkspace()).body));});
   const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json())),action=data=>page.evaluate(data=>editorAction(data),data);
   const untilMutation=old=>waitForAsync(page,async old=>(await fetch('/api/editor/state').then(r=>r.json())).mutationRevision>old,old);
   const select=async ids=>{const s=await state(),elements=s.slides[s.current].elements,indices=ids.map(id=>elements.findIndex(e=>e.id===id));await action({action:'select-elements',elementIndices:indices,elementsData:indices.map(i=>elements[i]),element:indices[0]});};
   const origin=e=>{const q=new URLSearchParams(e.query);return {x:Number(q.get('left'))+Number(q.get('object-offset-x')||0)+Number(q.get('shape-offset-x')||0),y:Number(q.get('top'))+Number(q.get('object-offset-y')||0)+Number(q.get('shape-offset-y')||0)};};
   const source=(s,id)=>s.slides[s.current].elements.find(e=>e.id===id);
   const hit=id=>page.locator('.keynope-canvas-element[data-element-id="'+id+'"]');
   const start=async locator=>{const r=await locator.boundingBox();assert(r);const p={x:r.x+r.width*.35,y:r.y+r.height*.35};await page.mouse.move(p.x,p.y);await page.mouse.down();return p;};
   const persisted=[];
   for(const master of [false,true]){
    if(master)await action({action:'toggle-master-mode'});
    const ids=[];
    for(const [kind,x,y,w,h]of [['text',22.25,8.2,40,5],['shape',100.6,18.4,30,12]]){
     await action({action:'add-element',kind,name:kind==='shape'?'square':undefined});const s=await state(),e=s.slides[s.current].elements[s.selected];ids.push(e.id);
     const q=new URLSearchParams({left:String(Math.floor(x)),top:String(Math.floor(y)),'object-offset-x':String(x%1),'object-offset-y':String(y%1),width:String(w),height:String(h),...(kind==='shape'?{shape:'square'}:{render:'truetype','modern-font':'mono','modern-size':'80','text-box':'1'})});
     await action({action:'update-element',element:s.selected,elementData:{...e,query:q.toString()}});
    }
    for(const id of ids){
     await select([id]);const before=await state(),canvas=await page.locator('.keynope-canvas-overlay').boundingBox(),p=await start(hit(id));
     await page.keyboard.down('Alt');await page.mouse.move(p.x+7,p.y+4,{steps:3});
     assert.deepEqual((await state()).slides,before.slides,'Preview changed authored state');
     const preview=await hit(id).boundingBox();assert(Math.abs(preview.x-(p.x-((await hit(id).boundingBox()).width*.35)+7))<1,'Live drag rounded horizontally');
     await page.mouse.up();await page.keyboard.up('Alt');await untilMutation(before.mutationRevision);await select([id]);
     const after=await state(),a=origin(source(before,id)),b=origin(source(after,id));near(b.x-a.x,7*245/canvas.width,'pointer X');near(b.y-a.y,4*56/canvas.height,'pointer Y');
     assert.equal(after.mutationRevision,before.mutationRevision+1);assert.equal(source(after,id).text,source(before,id).text);
     const rendered=await hit(id).boundingBox();assert(Math.abs(rendered.x-preview.x)<.5&&Math.abs(rendered.y-preview.y)<.5,'Release jumped from preview');
     await action({action:'undo'});assert.deepEqual((await state()).slides,before.slides);await action({action:'redo'});assert.deepEqual((await state()).slides,after.slides);await action({action:'undo'});
     // A one-pixel click wobble must not create a mutation or block selection.
     await select([id]);const clean=await state(),click=await start(hit(id));await page.mouse.move(click.x+1,click.y+1);await page.mouse.up();await select([id]);assert.deepEqual((await state()).slides,clean.slides);assert.equal((await state()).mutationRevision,clean.mutationRevision);
    }
    await select([ids[1]]);const guideBefore=await state(),guideCanvas=await page.locator('.keynope-canvas-overlay').boundingBox(),gp=await start(hit(ids[1]));
    await page.mouse.move(gp.x+(22.25-100.6)*guideCanvas.width/245+2,gp.y+4,{steps:5});
    await page.locator('.keynope-alignment-guide').first().waitFor({state:'visible'});await page.mouse.up();await untilMutation(guideBefore.mutationRevision);await select([ids[1]]);
    near(origin(source(await state(),ids[1])).x,22.25,'actual fractional guide');await action({action:'undo'});assert.deepEqual((await state()).slides,guideBefore.slides);
    await select(ids);await action({action:'group-elements',elementData:{id:ids[0]}});
    const group=page.locator('.keynope-canvas-element[data-group]');await group.waitFor();
    for(const [dx,dy]of [[7,4],[-4,-3],[5,1]]){
     const before=await state(),canvas=await page.locator('.keynope-canvas-overlay').boundingBox();
     // Hit actual member pixels, not empty space in the group's union.
     const member=group.locator('.keynope-group-member-hit').first(),p=await start(member);
     await page.keyboard.down('Alt');await page.mouse.move(p.x+dx,p.y+dy,{steps:3});assert.deepEqual((await state()).slides,before.slides);await page.mouse.up();await page.keyboard.up('Alt');await untilMutation(before.mutationRevision);await select(ids);
     const after=await state();for(const id of ids){const a=origin(source(before,id)),b=origin(source(after,id));near(b.x-a.x,dx*245/canvas.width,'rigid group X');near(b.y-a.y,dy*56/canvas.height,'rigid group Y');}
     await action({action:'undo'});assert.deepEqual((await state()).slides,before.slides);await action({action:'redo'});assert.deepEqual((await state()).slides,after.slides);
    }
    const nudgeBefore=await state();await page.evaluate(()=>document.activeElement?.blur());await page.keyboard.press('ArrowDown');await untilMutation(nudgeBefore.mutationRevision);await select(ids);
    const nudged=await state();for(const id of ids){const a=origin(source(nudgeBefore,id)),b=origin(source(nudged,id));near(b.x,a.x,'keyboard X unchanged');near(b.y-a.y,1,'keyboard whole-cell Y');}await action({action:'undo'});assert.deepEqual((await state()).slides,nudgeBefore.slides);
    const before=await state(),p=await start(group.locator('.keynope-group-member-hit').first());await page.mouse.move(p.x+12,p.y+6);await page.keyboard.press('Escape');await page.mouse.up();assert.deepEqual((await state()).slides,before.slides);assert.equal((await state()).mutationRevision,before.mutationRevision);
    await page.screenshot({path:'/tmp/keynope-precise-drag-'+engine+'-'+(master?'master':'normal')+'.png'});
    const saved=await state();persisted.push({master,items:ids.map(id=>({id,...origin(source(saved,id))}))});
   }
   await action({action:'toggle-master-mode'});
   await page.evaluate(async()=>{const response=await fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Precise drag.md'})});if(!response.ok)throw Error(await response.text());const saved=await response.json(),opened=JSON.parse(keynopeWasmInit(saved.content,'Precise drag.md',false));if(opened.status!==200)throw Error(opened.body);await keynopeReloadWebDocument(JSON.parse(JSON.parse(keynopeWasmWorkspace()).body));});
   for(const record of persisted){if(record.master)await action({action:'toggle-master-mode'});const reopened=await state();for(const item of record.items){const value=origin(source(reopened,item.id));near(value.x,item.x,'reopened fractional X');near(value.y,item.y,'reopened fractional Y');}}
   assert.deepEqual(errors,[]);console.log('PASS '+engine+': subcell single/group drag, preview/release parity, dead zone, history and Escape in normal/master views');
  }finally{await browser.close();}
 }
})().catch(e=>{console.error(e);process.exitCode=1;}).finally(()=>server.close());
