// Shared Mac/Web editor grouping, exercised against the real WASM editor.
const fs=require('node:fs'),path=require('node:path'),http=require('node:http'),assert=require('node:assert/strict');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve('terraform/site');
const server=http.createServer((req,res)=>{
 let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';
 const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);res.end();return;}
 res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');fs.createReadStream(file).pipe(res);
});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));
 for(const engine of ['chromium','webkit']){
  const browser=await pw[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
  try{
   const page=await browser.newPage({viewport:{width:1600,height:1000}}),errors=[];
   page.on('pageerror',e=>{errors.push(e.message);console.error('Browser error:',e.message);});
   await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');
   await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:120000});
   const fixture='<!-- keynope width=245 height=56 -->\n\n<!-- top=3 left=10 width=40 height=5 text-box=1 render=truetype -->\nText\n\n<!-- top=13 left=60 width=12 height=6 shape=square -->\n[shape:square]\n\n<!-- top=25 left=120 width=40 height=5 text-box=1 render=truetype -->\nOutside';
   await page.evaluate(async md=>{const r=JSON.parse(keynopeWasmInit(md,'Groups.md',false));if(r.status!==200)throw Error(r.body);const w=JSON.parse(JSON.parse(keynopeWasmWorkspace()).body);w.current=0;await keynopeReloadWebDocument(w);},fixture);
   const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
   const hit=i=>page.locator('.keynope-canvas-element[data-element="'+i+'"]');
   const waitSelection=n=>page.waitForFunction(async n=>(await fetch('/api/editor/state').then(r=>r.json())).selection.length===n,n);
   const canvas=await page.locator('.keynope-canvas-overlay').boundingBox();
   const point=(x,y)=>({x:canvas.x+x*canvas.width/245,y:canvas.y+y*canvas.height/56});
   const p0=point(2,1),p1=point(80,22),small=point(30,10);
   await page.keyboard.down('Shift');await page.mouse.move(p0.x,p0.y);await page.mouse.down();await page.mouse.move(p1.x,p1.y,{steps:8});
   await page.locator('.keynope-canvas-marquee').waitFor({state:'visible'});
   assert.equal(await page.locator('.marquee-candidate').count(),2);assert.equal((await state()).selection.length,0,'selection commits only on release');
   await page.mouse.move(small.x,small.y,{steps:4});assert.equal(await page.locator('.marquee-candidate').count(),1,'preview removes items outside the shrinking box');
   await page.mouse.move(p1.x,p1.y,{steps:4});await page.screenshot({path:'/tmp/keynope-marquee-'+engine+'.png'});await page.mouse.up();await page.keyboard.up('Shift');await waitSelection(2);
   await hit(0).click({button:'right'});
   await page.getByRole('button',{name:'Group',exact:true}).last().click();
   await page.locator('.keynope-canvas-element[data-group]').waitFor();
   const group=()=>page.locator('.keynope-canvas-element[data-group]');
   let s=await state();assert.equal(s.selection.length,2);const ids=s.slides[0].elements.slice(0,2).map(e=>e.id);
   assert.equal(await group().locator('.keynope-resize-handle').count(),0,'group must not resize a member');
   let box=await group().boundingBox();
   const canvasBox=await page.locator('#presenter-canvas').boundingBox();
   assert(Math.abs(box.width/canvasBox.width-62/245)<.005,'outer group box spans both members');
   await page.mouse.move(box.x+box.width/2,box.y+box.height/2);await page.mouse.down();await page.mouse.move(box.x+box.width/2+55,box.y+box.height/2+30,{steps:8});await page.mouse.up();
   await page.waitForFunction(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[0].elements.filter(e=>new URLSearchParams(e.query).get('group')).every(e=>new URLSearchParams(e.query).has('left'));});
   s=await state();const members=s.slides[0].elements.filter(e=>ids.includes(e.id));
   const q=members.map(e=>new URLSearchParams(e.query));
   assert.equal(Number(q[1].get('top'))-Number(q[0].get('top')),10,'relative vertical spacing');
   assert.equal(Number(q[1].get('left'))-Number(q[0].get('left')),50,'relative horizontal spacing');
   // Arrow moves both, and one undo restores both.
   await page.keyboard.press('ArrowDown');await page.waitForFunction(async top=>{const s=await fetch('/api/editor/state').then(r=>r.json());return Number(new URLSearchParams(s.slides[0].elements[0].query).get('top'))===top+1;},Number(q[0].get('top')));
   await page.getByRole('button',{name:'Undo',exact:true}).click();
   await group().click();await waitSelection(2);
   await group().dblclick();await page.waitForFunction(async()=>!!(await fetch('/api/editor/state').then(r=>r.json())).editingGroup);
   await group().waitFor({state:'detached'});await page.locator('.keynope-group-edit-boundary').waitFor();
   await hit(0).click();await waitSelection(1);assert.equal(await hit(0).locator('.keynope-resize-handle').count(),4);
   const memberBefore=await state(),memberBox=await hit(0).boundingBox();
   await page.mouse.move(memberBox.x+memberBox.width/2,memberBox.y+memberBox.height/2);await page.mouse.down();await page.mouse.move(memberBox.x+memberBox.width/2+30,memberBox.y+memberBox.height/2+20,{steps:5});await page.mouse.up();
   await page.waitForFunction(async old=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[0].elements.find(e=>e.id===old.id)?.query!==old.query;},memberBefore.slides[0].elements[0]);
   const memberAfter=await state();assert(memberAfter.editingGroup,'moving a member must retain group editing');assert.equal(memberAfter.selection.length,1);
   assert.equal(memberAfter.slides[0].elements.find(e=>e.id===memberBefore.slides[0].elements[1].id).query,memberBefore.slides[0].elements[1].query,'moving a member leaves its sibling unchanged');
   await hit(1).click();await waitSelection(1);
   const shapeBefore=await hit(1).boundingBox(),textBeforeShapeMove=(await state()).slides[0].elements[0].query;
   await page.mouse.move(shapeBefore.x+shapeBefore.width/2,shapeBefore.y+shapeBefore.height/2);await page.mouse.down();await page.mouse.move(shapeBefore.x+shapeBefore.width/2+canvas.width*5/245,shapeBefore.y+shapeBefore.height/2,{steps:5});await page.mouse.up();
   await page.waitForFunction(async id=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.editingGroup&&s.selected===s.slides[0].elements.findIndex(e=>e.id===id);},memberBefore.slides[0].elements[1].id);
   await page.waitForFunction(({left,delta})=>Math.abs(document.querySelector('.keynope-canvas-element[data-element="1"]').getBoundingClientRect().left-left-delta)<1,{left:shapeBefore.x,delta:canvas.width*5/245});
   assert.equal((await state()).slides[0].elements[0].query,textBeforeShapeMove,'individual shape movement leaves text unchanged');
   await page.keyboard.press('Escape');await group().waitFor();await waitSelection(2);
   // Shared colour applies to both text and shape; H1 applies only to text.
   await group().click({button:'right'});
   await page.locator('.keynope-slide-context.open').getByRole('button',{name:'Pick color',exact:true}).click();
   await page.getByRole('button',{name:'#ff0000',exact:true}).click();
   await page.waitForFunction(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[0].elements.slice(0,2).every(e=>new URLSearchParams(e.query).get('fg')==='#ff0000');});
   await group().click({button:'right'});await page.getByRole('button',{name:'Convert to heading 1',exact:true}).last().click();
   await page.waitForFunction(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[0].elements[0].kind==='heading';});
   s=await state();assert.equal(s.slides[0].elements[1].kind,'shape');assert.equal(s.slides[0].elements[2].kind,'text');
   assert.equal(new URLSearchParams(s.slides[0].elements[0].query).get('header'),'#ff0000');
   await group().click({button:'right'});await page.getByRole('button',{name:'Convert to plain text',exact:true}).last().click();
   await page.waitForFunction(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[0].elements[0].kind==='text';});
   // Mixed selection menu must not expose tools that silently edit one member.
   await group().click({button:'right'});
   assert.equal(await page.getByRole('button',{name:'Group',exact:true}).count(),0);
   assert(await page.getByRole('button',{name:'Ungroup',exact:true}).count()>0);
   await page.keyboard.press('Escape');
   // Reverse-direction, additive selection; Escape cancels without changing state.
   const reverseStart=point(170,35),reverseEnd=point(110,23);
   await page.keyboard.down('Shift');await page.mouse.move(reverseStart.x,reverseStart.y);await page.mouse.down();await page.mouse.move(reverseEnd.x,reverseEnd.y,{steps:6});
   assert.equal((await state()).selection.length,2);assert.equal(await page.locator('.marquee-candidate').count(),2,'group and outsider preview');
   await page.keyboard.press('Escape');await page.mouse.up();await page.keyboard.up('Shift');assert.equal((await state()).selection.length,2);
   await page.locator('.keynope-canvas-marquee').waitFor({state:'detached'});
   await page.keyboard.down('Shift');await page.mouse.move(reverseStart.x,reverseStart.y);await page.mouse.down();await page.mouse.move(reverseEnd.x,reverseEnd.y,{steps:6});await page.mouse.up();await page.keyboard.up('Shift');await waitSelection(3);
   await group().click({button:'right'});await page.getByRole('button',{name:'Group',exact:true}).last().click();
   await page.waitForFunction(()=>document.querySelectorAll('.keynope-canvas-element').length===1);
   s=await state();assert.equal(new Set(s.slides[0].elements.map(e=>new URLSearchParams(e.query).get('group'))).size,1);
   await group().click({button:'right'});await page.getByRole('button',{name:'Ungroup',exact:true}).last().click();
   await page.waitForFunction(()=>document.querySelectorAll('.keynope-canvas-element').length===3);
   assert((await state()).slides[0].elements.every(e=>!new URLSearchParams(e.query).has('group')));
   await page.getByRole('button',{name:'Undo',exact:true}).click();await group().waitFor();
   await group().click();await waitSelection(3);await group().click({button:'right'});
   await page.locator('.keynope-slide-context.open').getByRole('button',{name:'Centre',exact:true}).click();
   await page.waitForFunction(()=>{const g=document.querySelector('.keynope-canvas-element[data-group]');return g&&Math.abs(parseFloat(g.style.left)*2+parseFloat(g.style.width)-100)<.85;});
   await page.keyboard.press('Escape');
   await group().click({button:'right'});
   await page.screenshot({path:'/tmp/keynope-groups-'+engine+'.png'});
   await page.keyboard.press('Escape');
   // Measure rendered geometry, not merely query values, over repeated drags.
   const rigid='<!-- keynope width=245 height=56 -->\n\n<!-- top=3 left_pct=0.45 width=30 height=5 text-box=1 render=truetype group=rigid -->\nText\n\n<!-- top=14 left_pct=0.65 width=19.5 height=6.5 shape-offset-x=0.5 shape-offset-y=0.5 shape=square group=rigid -->\n[shape:square]\n\n<!-- top=24 left_pct=0.53 width=35 height=5 text-box=1 render=truetype group=rigid -->\nThird';
   await page.evaluate(async md=>{const r=JSON.parse(keynopeWasmInit(md,'Rigid.md',false));if(r.status!==200)throw Error(r.body);const w=JSON.parse(JSON.parse(keynopeWasmWorkspace()).body);w.current=0;await keynopeReloadWebDocument(w);},rigid);
   const geometry=async()=>{
     await group().dblclick();await page.locator('.keynope-group-edit-boundary').waitFor();
     const boxes=await page.locator('.keynope-canvas-element').evaluateAll(hits=>hits.map(h=>({index:Number(h.dataset.element),x:parseFloat(h.style.left)*245/100,y:parseFloat(h.style.top)*56/100,w:parseFloat(h.style.width)*245/100,h:parseFloat(h.style.height)*56/100})).sort((a,b)=>a.index-b.index));
     await hit(0).click();await page.keyboard.press('Escape');await group().waitFor();return boxes;
   };
   let previous=await geometry();
   for(const delta of [9,11,8,-12,7,-10,6,-9]){
     const beforeVersion=(await state()).version,b=await group().boundingBox(),r=await page.locator('.keynope-canvas-overlay').boundingBox();
     await page.mouse.move(b.x+b.width/2,b.y+b.height/2);await page.mouse.down();await page.mouse.move(b.x+b.width/2+delta*r.width/245,b.y+b.height/2,{steps:6});await page.mouse.up();
     await page.waitForFunction(async version=>(await fetch('/api/editor/state').then(r=>r.json())).version>version,beforeVersion);
     const next=await geometry();
     for(let i=0;i<next.length;i++){
       assert(Math.abs(next[i].x-previous[i].x-delta)<.01,engine+' member '+i+' drifted on '+delta+': '+JSON.stringify({previous:previous[i],next:next[i]}));
       for(const dim of ['y','w','h'])assert(Math.abs(next[i][dim]-previous[i][dim])<.01,'rigid move preserves '+dim+' for member '+i);
     }
     previous=next;
   }
   assert.deepEqual(errors,[]);
   console.log('PASS '+engine+': marquee preview/commit/cancel, additive selection, repeated rigid movement, individual text/shape edits, undo, regroup and ungroup');
  }finally{await browser.close();}
 }
})().catch(e=>{console.error(e);process.exitCode=1}).finally(()=>server.close());
