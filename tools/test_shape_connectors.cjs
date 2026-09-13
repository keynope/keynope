const fs=require('node:fs'),path=require('node:path'),http=require('node:http'),assert=require('node:assert/strict');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright'),root=path.resolve('terraform/site');
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);res.end();return;}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');fs.createReadStream(file).pipe(res);});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));
 for(const engine of ['chromium','webkit']){
  const browser=await pw[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
  try{
   const page=await browser.newPage({viewport:{width:1600,height:1000}}),errors=[];page.on('pageerror',e=>errors.push(e.message));
   await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:120000});
   await page.evaluate(async md=>{keynopeWasmInit(md,'Connectors.md',false);const w=JSON.parse(JSON.parse(keynopeWasmWorkspace()).body);w.current=0;await keynopeReloadWebDocument(w);},'<!-- keynope width=245 height=56 -->\n\n<!-- top=6 left=20 width=40 height=16 shape=circle fg=#0055aa -->\n[shape:circle]\n\n<!-- top=25 left=130 width=50 height=22 shape=triangle fg=#55aa55 -->\n[shape:triangle]');
   await page.waitForFunction(()=>document.querySelectorAll('.keynope-canvas-element').length===2);
   const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
   const shapes=(await state()).slides[0].elements;
   const port=(id,side)=>page.locator('.keynope-connect-dot[data-id="'+id+'"][data-side="'+side+'"]');
   await page.getByRole('button',{name:'Connect shapes',exact:true}).click();assert.equal(await page.locator('.keynope-connect-dot').count(),8);
   assert.equal(await page.getByLabel('Line width',{exact:true}).inputValue(),'1');assert.equal(await page.getByLabel('Line width',{exact:true}).getAttribute('min'),'1');
   assert.equal(await page.getByLabel('Arrow width',{exact:true}).inputValue(),'6');assert.equal(await page.getByLabel('Arrowheads',{exact:true}).inputValue(),'none');
   await page.screenshot({path:'/tmp/keynope-connectors-ports-'+engine+'.png'});
   await port(shapes[0].id,'right').click();await port(shapes[1].id,'left').click();
   await page.waitForFunction(async()=> (await fetch('/api/editor/state').then(r=>r.json())).slides[0].elements.some(e=>e.kind==='connector'));
   await page.locator('.keynope-connector-hit').waitFor();
   assert.equal((await state()).slides[0].elements.filter(e=>e.kind==='connector').length,1);
   await page.getByRole('button',{name:'Undo',exact:true}).click();await page.waitForFunction(()=>!document.querySelector('.keynope-connector-hit'));
   await page.getByRole('button',{name:'Connect shapes',exact:true}).click();
   const start=await port(shapes[0].id,'bottom').boundingBox(),end=await port(shapes[1].id,'top').boundingBox();
   await page.mouse.move(start.x+start.width/2,start.y+start.height/2);await page.mouse.down();await page.mouse.move(end.x+end.width/2,end.y+end.height/2,{steps:12});await page.mouse.up();
   await page.locator('.keynope-connector-hit').waitFor();
   let s=await state(),connector=s.slides[0].elements.find(e=>e.kind==='connector');assert.equal(new URLSearchParams(connector.query).get('connector-from-side'),'bottom');
   // Move the shape across the order of the other objects: identity must hold.
   const original=await page.locator('.keynope-connector-hit').getAttribute('points');
   await page.getByRole('button',{name:'Done',exact:true}).click();
   const sourceIndex=s.slides[0].elements.findIndex(e=>e.id===shapes[0].id),hit=page.locator('.keynope-canvas-element[data-element="'+sourceIndex+'"]'),box=await hit.boundingBox();
   await page.mouse.move(box.x+box.width/2,box.y+box.height/2);await page.mouse.down();await page.mouse.move(box.x+box.width/2+130,box.y+box.height/2+100,{steps:10});await page.mouse.up();
   await page.waitForFunction(x=>{const h=document.querySelector('.keynope-connector-hit');return h&&h.getAttribute('points')!==x},original);
   await page.getByRole('button',{name:'Connect shapes',exact:true}).click();await port(shapes[0].id,'top').click();await page.keyboard.press('Escape');assert.equal(await page.locator('.keynope-connect-dot').count(),0);
   assert.equal((await state()).slides[0].elements.filter(e=>e.kind==='connector').length,1,'cancel must not insert');
   await page.screenshot({path:'/tmp/keynope-connectors-'+engine+'.png'});
   // The line itself, not its rectangular extent, is the selectable object.
   await page.locator('.keynope-connector-hit').dispatchEvent('pointerdown',{button:0});
   await page.getByLabel('Arrowheads',{exact:true}).selectOption('both');
   await page.getByLabel('Line width',{exact:true}).fill('2');await page.getByLabel('Line width',{exact:true}).press('Tab');
   await page.getByLabel('Arrow width',{exact:true}).fill('8');await page.getByLabel('Arrow width',{exact:true}).press('Tab');
   await page.locator('.keynope-connector-hit').dispatchEvent('dblclick');
   await page.locator('.keynope-connector-segment').first().waitFor();
   const handles=page.locator('.keynope-connector-segment'),n=await handles.count(),handle=handles.nth(Math.min(1,n-1)),hb=await handle.boundingBox(),horizontal=(await handle.getAttribute('aria-label')).includes('horizontal');
   await page.mouse.move(hb.x+hb.width/2,hb.y+hb.height/2);await page.mouse.down();await page.mouse.move(hb.x+hb.width/2+(horizontal?0:35),hb.y+hb.height/2+(horizontal?35:0),{steps:8});await page.mouse.up();
   await page.waitForFunction(async()=>new URLSearchParams((await fetch('/api/editor/state').then(r=>r.json())).slides[0].elements.find(e=>e.kind==='connector').query).has('connector-route'));
   await page.screenshot({path:'/tmp/keynope-elbow-'+engine+'.png'});
   await page.getByRole('button',{name:'Line color',exact:true}).click();
   await page.getByLabel('HTML color',{exact:true}).fill('#ff0055');await page.getByRole('button',{name:'Apply',exact:true}).click();
   await page.waitForFunction(async()=>{const e=(await fetch('/api/editor/state').then(r=>r.json())).slides[0].elements.find(e=>e.kind==='connector');return new URLSearchParams(e.query).get('fg')==='#ff0055';});
   assert.equal(await page.locator('.keynope-connector-selection').count(),0,'yellow line overlay must not exist');
   await page.locator('.keynope-connector-hit').dispatchEvent('contextmenu');assert.equal(await page.locator('.keynope-slide-context.open').count(),0);
   await page.keyboard.press('Backspace');await page.waitForFunction(()=>!document.querySelector('.keynope-connector-hit'));
   await page.getByRole('button',{name:'Undo',exact:true}).click();await page.locator('.keynope-connector-hit').waitFor();
   // Fresh Insert shapes used to lack IDs: preview appeared but no line was saved.
   await page.evaluate(async()=>{const initialized=JSON.parse(keynopeWasmInit('<!-- keynope width=245 height=56 -->\n\nText\n','New shapes.md',false));if(initialized.status!==200)throw new Error(initialized.body);const w=JSON.parse(JSON.parse(keynopeWasmWorkspace()).body);w.current=0;await keynopeReloadWebDocument(w);});
   for(const name of ['circle','square']){
     await page.getByRole('button',{name:'Add shape',exact:true}).click();await page.getByRole('button',{name:'Add '+name,exact:true}).click();await page.getByRole('button',{name:'Done',exact:true}).click();
   }
   const fresh=(await state()).slides[0].elements.filter(e=>e.kind==='shape');assert.equal(fresh.length,2);assert(fresh.every(e=>e.id));assert.notEqual(fresh[0].id,fresh[1].id);
   // Separate the inserted shapes so overlapping ports cannot mask one another.
   await page.evaluate(async shapes=>{const s=await fetch('/api/editor/state').then(r=>r.json());const elements=shapes.map((e,i)=>{const q=new URLSearchParams(e.query);for(const k of ['align','left_pct','right','right_pct','row_delta','bottom','valign'])q.delete(k);q.set('left',String(30+i*100));q.set('top',String(10+i*20));return {...e,query:q.toString()};});const r=await fetch('/api/editor/action',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'update-elements',slide:0,elementIndices:shapes.map(e=>s.slides[0].elements.findIndex(x=>x.id===e.id)),elementsData:elements})});if(!r.ok)throw new Error(await r.text());const w=JSON.parse(JSON.parse(keynopeWasmWorkspace()).body);w.current=0;await keynopeReloadWebDocument(w);},fresh);
   await page.getByRole('button',{name:'Connect shapes',exact:true}).click();
   await page.getByRole('button',{name:'Inherit color',exact:true}).click();
   await page.getByLabel('Line width',{exact:true}).fill('1');await page.getByLabel('Line width',{exact:true}).press('Tab');
   await page.getByLabel('Arrowheads',{exact:true}).selectOption('both');
   await port(fresh[0].id,'right').click();
   const target=await port(fresh[1].id,'left').boundingBox();
   await page.evaluate(()=>{const original=window.fetch;window.__connectorPreview=null;window.fetch=async(...args)=>{const response=await original(...args);if(String(args[0]).endsWith('/api/editor/connector-preview')){window.__connectorPreviewStatus=response.status;if(response.ok)window.__connectorPreview=await response.clone().json();}return response;};});
   await page.mouse.move(target.x+target.width/2,target.y+target.height/2,{steps:6});
   await page.waitForFunction(()=>window.__connectorPreview);const preview=await page.evaluate(()=>window.__connectorPreview);assert(preview.lines.some(l=>l.parts.some(p=>p.text.trim())),'no semi-block preview');
   await page.waitForTimeout(150);await page.screenshot({path:'/tmp/keynope-styled-connector-preview-'+engine+'.png'});
   assert.equal(await page.locator('.keynope-connector-selection,svg line[stroke="#ffd166"],svg polyline[stroke="#ffd166"]').count(),0,'yellow preview appeared');
   assert.equal((await state()).slides[0].elements.filter(e=>e.kind==='connector').length,0,'preview persisted a line');
   await port(fresh[1].id,'left').click();await page.locator('.keynope-connector-hit').waitFor();
   assert(!new URLSearchParams((await state()).slides[0].elements.find(e=>e.kind==='connector').query).has('fg'),'inherited color was made explicit');
   const whiteInk=()=>page.evaluate(()=>{const c=document.querySelector('#presenter-canvas'),p=c.getContext('2d').getImageData(0,0,c.width,c.height).data;let n=0;for(let i=0;i<p.length;i+=4)if(p[i]>245&&p[i+1]>245&&p[i+2]>245)n++;return n;});
   const beforeToggle=await whiteInk();
   await page.getByRole('button',{name:'Done',exact:true}).click();await page.getByRole('button',{name:'Connect shapes',exact:true}).click();
   assert.equal((await state()).slides[0].elements.filter(e=>e.kind==='connector').length,1,'reopening connect mode deleted line');
   assert((await whiteInk())>=beforeToggle*.9,'existing line vanished from canvas when connection mode opened');
   await page.keyboard.press('Escape');
   await page.getByRole('button',{name:'Connect shapes',exact:true}).click();await port(fresh[0].id,'bottom').click();await port(fresh[1].id,'top').click();await page.waitForFunction(()=>document.querySelectorAll('.keynope-connector-hit').length===2);
   assert.equal((await state()).slides[0].elements.filter(e=>e.kind==='connector').length,2,'adding another line replaced first');
   await page.getByRole('button',{name:'Delete element',exact:true}).click();await page.waitForFunction(()=>document.querySelectorAll('.keynope-connector-hit').length===1);
   assert.deepEqual(errors,[]);console.log('PASS '+engine+': connectors, segment editing, line/arrow widths, colour, fresh-shape anchors, multiple lines, cancel and undo');
  }finally{await browser.close();}
 }
})().catch(e=>{console.error(e);process.exitCode=1}).finally(()=>server.close());
