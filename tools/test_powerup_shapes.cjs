const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const {waitForAsync}=require('./wait_for_async.cjs');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),output=path.resolve(process.env.KEYNOPE_SHAPE_EVIDENCE||'output/powerup/shapes');
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const proof=name.startsWith('/proof/');const base=proof?output:root;const file=path.resolve(base,'.'+(proof?name.slice(6):name));if(!file.startsWith(base+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.html':'text/html','.css':'text/css','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const engine=process.env.TEST_BROWSER||'chromium',browser=await playwright[engine].launch({headless:true,...(engine==='webkit'?{}:{executablePath:process.env.CHROME_PATH})});
  try{
    const context=await browser.newContext({viewport:{width:1500,height:1000}}),errors=[];
    await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());context.on('page',p=>p.on('pageerror',e=>errors.push(e.message)));
    const page=await context.newPage();page.setDefaultTimeout(30000);page.on('dialog',d=>d.accept());
    const origin='http://127.0.0.1:'+server.address().port;
    await page.goto(origin+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
    const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
    const action=data=>page.evaluate(data=>editorAction(data),data);
    const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles(path.join(output,'curated-shapes.md'));
    await page.waitForFunction(()=>document.querySelectorAll('#stage .keynope-modern-scene [data-object-id^="shape-"]>svg').length===29);
    await page.screenshot({path:path.join(output,'editor-shape-grid.png')});
    await action({action:'add-slide'});
    await page.getByRole('button',{name:'Insert',exact:true}).click();await page.getByRole('button',{name:'Add shape',exact:true}).click();
    const gallery=page.getByRole('menu',{name:'Add shape',exact:true});assert.equal(await gallery.getByRole('menuitem').count(),29);
    await page.screenshot({path:path.join(output,'shape-gallery.png')});await gallery.getByRole('menuitem',{name:'Add right arrow',exact:true}).click();
    const added=await state(),selected=added.slides[added.current].elements[added.selected],id=selected.id,slide=added.current;
    assert.equal(new URLSearchParams(selected.query).get('shape'),'arrow-right');
    // Place an owned, high-contrast example away from the canvas edge so the
    // inspected handles and silhouette are legible at ordinary window zoom.
    const visibleArrow={...selected,query:new URLSearchParams({...Object.fromEntries(new URLSearchParams(selected.query)),left:'35',top:'12',width:'80',height:'18',fg:'#087e99'}).toString()};
    await action({action:'update-element',element:added.selected,elementData:visibleArrow});
    const type=page.getByRole('combobox',{name:'Shape type',exact:true}).first();await type.waitFor({state:'visible'});
    const depth=page.getByRole('spinbutton',{name:'Head depth',exact:true}).first();await depth.fill('55');await depth.press('Tab');
    await waitForAsync(page,async id=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[s.current].elements.find(e=>e.id===id)?.query.includes('shape-depth=0.55');},id);
    const geometry=await page.evaluate(async({id,slide})=>(await fetch('/api/editor/scene?slide='+slide).then(r=>r.json())).objects.find(o=>o.id===id).shapeGeometry,{id,slide});
    assert.equal(geometry.spec.adjustments.depth,.55);
    const painted=page.locator(`#stage .keynope-modern-scene [data-object-id="${id}"]>svg>path`).first();await painted.waitFor();assert.equal(await painted.getAttribute('d'),geometry.path);
    const node=await painted.evaluate(node=>{window.__shapeNode=node.parentElement;return node.tagName;});assert.equal(node,'path');
    await depth.fill('30');await depth.press('Tab');await page.waitForFunction(()=>document.querySelector('#stage .keynope-modern-scene [data-object-id] path')&&window.__shapeNode?.isConnected);
    await waitForAsync(page,async({id,slide})=>(await fetch('/api/editor/scene?slide='+slide).then(r=>r.json())).objects.find(o=>o.id===id)?.shapeGeometry.spec.adjustments.depth===.3,{id,slide});
    assert.equal(await painted.evaluate(node=>node.parentElement===window.__shapeNode),true,'Adjustment remounted the vector owner unnecessarily');
    await action({action:'undo'});await waitForAsync(page,async({id,slide})=>(await fetch('/api/editor/scene?slide='+slide).then(r=>r.json())).objects.find(o=>o.id===id)?.shapeGeometry.spec.adjustments.depth===.55,{id,slide});

    // Real pointer gestures use isolated production drafts, not editor commands
    // for every frame. Model revision must remain unchanged until release.
    const headHandle=page.getByRole('slider',{name:'Adjust head depth',exact:true});await headHandle.waitFor();
    const initial=await state(),box=await headHandle.boundingBox();assert.ok(box);
    const originPoint={x:box.x+box.width/2,y:box.y+box.height/2};
    await page.evaluate(()=>{window.__shapeDraftErrors=[];document.addEventListener('pointerdown',e=>window.__shapeDraftErrors.push({event:'pointerdown',class:e.target.className}),true);const fetchOriginal=window.fetch;window.fetch=async(...args)=>{const action=String(args[0]).includes('/api/editor/action')?JSON.parse(args[1]?.body||'{}').action:null;if(action)window.__shapeDraftErrors.push({event:'action-request',action});try{const response=await fetchOriginal(...args);if(action)window.__shapeDraftErrors.push({event:'action-response',action,status:response.status});if(String(args[0]).includes('shape-adjustment-draft'))window.__shapeDraftErrors.push({event:'draft',status:response.status,body:response.ok?'ok':await response.clone().text()});return response;}catch(error){window.__shapeDraftErrors.push({event:'fetch-error',message:error.message});throw error;}};});
    const pathBefore=await painted.getAttribute('d');
    await page.mouse.move(originPoint.x,originPoint.y);await page.mouse.down();await page.mouse.move(originPoint.x+15,originPoint.y,{steps:4});
    try{await page.waitForFunction(({id,before})=>document.querySelector(`#stage [data-object-id="${id}"]>svg>path`)?.getAttribute('d')!==before,{id,before:pathBefore});}
    catch(error){console.error(await page.evaluate(()=>({status:document.querySelector('#editor-status')?.textContent,handles:[...document.querySelectorAll('.keynope-shape-adjustment')].map(n=>({key:n.dataset.adjustment,value:n.getAttribute('aria-valuenow'),left:n.style.left,top:n.style.top})),errors:window.__shapeDraftErrors||[]})));throw error;}
    assert.equal((await state()).version,initial.version,'Live draft mutated the document');
    await page.screenshot({path:path.join(output,'shape-handle-live.png')});
    await page.mouse.up();await waitForAsync(page,async v=>(await fetch('/api/editor/state').then(r=>r.json())).version===v+1,initial.version);
    const committed=await state(),committedArrow=committed.slides[committed.current].elements.find(e=>e.id===id);
    assert.ok(Number(new URLSearchParams(committedArrow.query).get('shape-depth'))<.55);
    await page.waitForFunction(value=>Math.abs(Number(document.querySelector('input[aria-label="Head depth"]').value)/100-value)<1e-6,Number(new URLSearchParams(committedArrow.query).get('shape-depth')));
    await action({action:'undo'});await waitForAsync(page,async({id,slide})=>(await fetch('/api/editor/scene?slide='+slide).then(r=>r.json())).objects.find(o=>o.id===id)?.shapeGeometry.spec.adjustments.depth===.55,{id,slide});
    const cancelled=await state(),cancelBox=await headHandle.boundingBox(),cancelPath=await painted.getAttribute('d');
    await page.mouse.move(cancelBox.x+cancelBox.width/2,cancelBox.y+cancelBox.height/2);await page.mouse.down();await page.mouse.move(cancelBox.x+cancelBox.width/2-12,cancelBox.y+cancelBox.height/2,{steps:3});
    await page.waitForFunction(({id,before})=>document.querySelector(`#stage [data-object-id="${id}"]>svg>path`)?.getAttribute('d')!==before,{id,before:cancelPath});
    await page.keyboard.press('Escape');await page.mouse.up();
    await page.waitForFunction(({id,before})=>document.querySelector(`#stage [data-object-id="${id}"]>svg>path`)?.getAttribute('d')===before,{id,before:cancelPath});
    assert.equal((await state()).version,cancelled.version,'Escape committed a geometry draft');
    await page.evaluate(()=>{window.__fetchBeforeShapeDelay=window.fetch;window.fetch=(...args)=>String(args[0]).includes('shape-adjustment-draft')?new Promise(resolve=>{window.__releaseShapeDraft=()=>resolve(window.__fetchBeforeShapeDelay(...args));}):window.__fetchBeforeShapeDelay(...args);});
    const delayedBox=await headHandle.boundingBox(),delayedState=await state(),delayedPath=await painted.getAttribute('d');
    await page.mouse.move(delayedBox.x+delayedBox.width/2,delayedBox.y+delayedBox.height/2);await page.mouse.down();await page.mouse.move(delayedBox.x+delayedBox.width/2+8,delayedBox.y+delayedBox.height/2);
    await page.waitForFunction(()=>typeof window.__releaseShapeDraft==='function');
    await page.keyboard.press('Escape');await page.mouse.up();
    await page.evaluate(async()=>{window.__releaseShapeDraft();window.fetch=window.__fetchBeforeShapeDelay;await new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)));});
    assert.equal((await state()).version,delayedState.version);assert.equal(await painted.getAttribute('d'),delayedPath,'Late draft overwrote the cancelled geometry');
    const noopState=await state(),noopBox=await headHandle.boundingBox();
    const noopPoint={x:noopBox.x+noopBox.width/2+2,y:noopBox.y+noopBox.height/2+2};
    await page.mouse.move(noopPoint.x,noopPoint.y);await page.mouse.down();await page.mouse.move(noopPoint.x+10,noopPoint.y,{steps:2});await page.mouse.move(noopPoint.x,noopPoint.y,{steps:2});await page.mouse.up();
    assert.equal((await state()).version,noopState.version,'Returning to the origin created a geometry edit');
    await headHandle.press('ArrowRight');
    await waitForAsync(page,async({id,slide})=>(await fetch('/api/editor/scene?slide='+slide).then(r=>r.json())).objects.find(o=>o.id===id)?.shapeGeometry.spec.adjustments.depth===.56,{id,slide});
    try{await page.waitForFunction(()=>document.querySelector('input[aria-label="Head depth"]').value==='56');}
    catch(error){console.error(await page.evaluate(()=>({events:window.__shapeDraftErrors,values:[...document.querySelectorAll('input[aria-label="Head depth"]')].map(n=>({value:n.value,html:n.outerHTML})),handle:document.querySelector('.keynope-shape-adjustment[data-adjustment="depth"]')?.outerHTML,workspace:window.keynopeWorkspaceAppliedRevision})));throw error;}
    assert.equal(await depth.inputValue(),'56','Keyboard handle did not update the numerical field');
    await action({action:'undo'});
    // Rotation is undone before pointer-to-parameter projection; the shape's
    // placement/size/rotation remain untouched by geometry handles.
    const rotated=(await state()).slides[slide].elements.find(e=>e.id===id);
    rotated.query=new URLSearchParams({...Object.fromEntries(new URLSearchParams(rotated.query)),'object-rotation':'35'}).toString();
    await action({action:'update-element',element:(await state()).selected,elementData:rotated});
    const rotatedHandle=await headHandle.boundingBox(),rotatedState=await state();
    await page.mouse.move(rotatedHandle.x+rotatedHandle.width/2,rotatedHandle.y+rotatedHandle.height/2);await page.mouse.down();await page.mouse.move(rotatedHandle.x+rotatedHandle.width/2+10,rotatedHandle.y+rotatedHandle.height/2+7,{steps:3});await page.mouse.up();
    await waitForAsync(page,async v=>(await fetch('/api/editor/state').then(r=>r.json())).version===v+1,rotatedState.version);
    const rotatedResult=new URLSearchParams((await state()).slides[slide].elements.find(e=>e.id===id).query);
    assert.equal(rotatedResult.get('object-rotation'),'35');assert.notEqual(Number(rotatedResult.get('shape-depth')),.55);
    await action({action:'undo'});await action({action:'undo'});
    await type.selectOption('speech-bubble');await waitForAsync(page,async id=>{const s=await fetch('/api/editor/state').then(r=>r.json());return new URLSearchParams(s.slides[s.current].elements.find(e=>e.id===id).query).get('shape')==='speech-bubble';},id);
    await page.getByRole('spinbutton',{name:'Tail depth',exact:true}).first().waitFor({state:'visible'});
    await page.screenshot({path:path.join(output,'shape-adjustments.png')});
    const saved=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Shapes.md'})}).then(r=>r.json()));assert.ok(saved.content.includes('shape=speech-bubble'));
    for(const style of ['modern','retro']){
      const proof=await context.newPage();await proof.goto(origin+'/proof/'+style+'-shapes.html');
      await proof.evaluate(()=>keynopePrepareSnapshot(0));
      if(style==='retro')assert.ok(await proof.locator('.keynope-scene-retro-text').count()>=29,'Legacy Retro proof did not use sampled geometry');
      await proof.screenshot({path:path.join(output,style+'-shape-grid.png')});
      await proof.evaluate(()=>keynopePrepareSnapshot(1));await proof.screenshot({path:path.join(output,style+'-diagram.png')});await proof.close();
    }
    assert.deepEqual(errors,[]);console.log('PASS: 29-shape gallery, live numeric/drag/keyboard adjustment paths, draft immutability, Escape cancellation, rotated gestures, one-step undo, retained vector owner, type conversion, persistence and production modern/Retro diagrams');
  }finally{await browser.close();server.close();}
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
