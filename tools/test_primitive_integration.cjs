const {waitForAsync}=require("./wait_for_async.cjs");
// Cross-context mixed primitive acceptance, owned Go fixtures, no cloud/app launch.
const assert=require('node:assert/strict'),fs=require('node:fs'),http=require('node:http'),path=require('node:path');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright'),engine=process.env.TEST_BROWSER||'chromium';
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),fixtures=process.env.KEYNOPE_PRIMITIVE_EVIDENCE||'/tmp/keynope-primitive-integration-proof',output=process.env.PROOF_OUTPUT||fixtures;
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 fs.mkdirSync(output,{recursive:true});await new Promise(r=>server.listen(0,'127.0.0.1',r));const browser=await playwright[engine].launch({headless:true,...(engine==='webkit'?{}:{executablePath:process.env.CHROME_PATH})});
 try{
  const context=await browser.newContext({viewport:{width:1500,height:1000},reducedMotion:'reduce'});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());const report=[];
  for(const mode of ['slide','tab','base','layout']){
   const page=await context.newPage(),errors=[];page.setDefaultTimeout(30000);page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
   const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
   const action=data=>page.evaluate(data=>editorAction(data),data);
   const open=async(file,name)=>{const picker=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await picker).setFiles({name,mimeType:'text/markdown',buffer:Buffer.from(file)});await waitForAsync(page,async name=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.path?.endsWith(name)&&s.slides?.length===3&&s.masters?.layouts?.length>=3;},name);};
   const navigate=async()=>{if(mode==='base'||mode==='layout')await action({action:'toggle-master-mode'});await action({action:'select-slide',slide:mode==='base'?0:mode==='layout'?3:2});await page.waitForFunction(()=>deck.pages[pageIndex]?.scene?.objects?.length>=52);};
   await open(fs.readFileSync(fixtures+'/'+mode+'.md'),mode+'.md');await navigate();
   let s=await state();const target=s.slides[s.current],ids=target.elements.map(e=>e.id);assert.equal(ids.length,52);
   await action({action:'select-element',element:0,elementData:target.elements[0]});s=await state();assert.equal(s.selection.length,52,'Selecting mixed group omitted members');
   await page.evaluate(()=>window.primitiveOwners=new Map([...document.querySelectorAll('#stage .keynope-modern-scene [data-object-id]')].map(n=>[n.dataset.objectId,n])));
   const scope={slide:s.current,sceneMaster:s.masterMode,sceneRevision:s.version};await action({action:'set-selection-property',...scope,objectIds:ids,property:'paint.opacity',propertyValue:.75});
   const changed=await state();assert.equal(changed.version,s.version+1);for(const e of changed.slides[changed.current].elements)assert.equal(new URLSearchParams(e.query).get('modern-opacity'),'0.75');
   assert.deepEqual(await page.evaluate(()=>[...primitiveOwners].filter(([id,node])=>node!==document.querySelector('#stage .keynope-modern-scene [data-object-id="'+CSS.escape(id)+'"]')).map(([id,node])=>({id,tag:node.tagName,kind:deck.pages[pageIndex].scene.objects.find(o=>o.id===id)?.kind}))),[],'Mixed paint remounted owners');
   assert.equal(await page.locator('#stage .keynope-modern-scene polyline[data-object-id]').evaluate(n=>n.style.opacity),'0.75','Retained connector did not accept opacity');
   await page.getByRole('button',{name:'Undo',exact:true}).click();const undone=await state();assert.deepEqual(undone.slides[undone.current].elements,target.elements,'Mixed group Undo drifted');await page.getByRole('button',{name:'Redo',exact:true}).click();
   await action({action:'enter-group',element:0,elementData:target.elements[0]});const table=(await state()).slides[s.current].elements.find(e=>e.kind==='table');await action({action:'select-element',element:ids.indexOf(table.id),elementData:table});assert.equal((await state()).selection.length,1,'Entered group cannot select one semantic table');
   const before=await page.evaluate(()=>JSON.parse(JSON.stringify(deck.pages[pageIndex].scene)));await page.screenshot({path:output+'/'+mode+'-mixed-scene.png'});
   const saved=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Saved integration.md'})}).then(r=>r.json()).then(r=>r.content));assert(saved.includes('keynope-objects'));
   await open(saved,'Saved integration.md');await navigate();const after=await page.evaluate(()=>JSON.parse(JSON.stringify(deck.pages[pageIndex].scene)));delete before.revision;delete after.revision;assert.deepEqual(after,before,'Browser save/reopen changed mixed scene');
   if(mode==='tab'){const reopened=await state();assert.equal(reopened.slides[2].tabId,'destination-tab');assert.equal(reopened.tabs.length,1);}
   // Reopened group members must still accept independent shape/label/route
   // edits, not merely retain their saved pixels and selectable bounds.
   let fresh=await state();const freshOwner=structuredClone(fresh.slides[fresh.current].elements);
   assert.equal(await page.evaluate(()=>deck.pages[pageIndex].scene.objects.find(o=>o.kind==='chart').chart.data.options.fontRole),'heading','Chart family role lost on first reopen');
   await action({action:'enter-group',element:0,elementData:freshOwner[0]});
   const shape=freshOwner.find(e=>e.kind==='shape'&&new URLSearchParams(e.query).has('shape-label')),line=freshOwner.find(e=>e.kind==='connector');assert(shape&&line);
   const scoped=async data=>{const s=await state();await action({...data,slide:s.current,sceneMaster:s.masterMode,sceneRevision:s.version});const next=await state();assert.equal(next.version,s.version+1,'Fresh edit failed or did not commit exactly once');await page.waitForFunction(v=>window.keynopeWorkspaceAppliedRevision===v,next.version);};
   await scoped({action:'set-selection-property',objectIds:[shape.id],property:'paint.strokeWidth',propertyValue:1.375});
   await scoped({action:'set-selection-property',objectIds:[shape.id],property:'label.size',propertyValue:43});
   const adjusted=await page.evaluate(id=>{const o=deck.pages[pageIndex].scene.objects.find(o=>o.id===id);return o.shapeGeometry?.handles?.[0];},shape.id);assert(adjusted,'Fixture lost adjustable shape');
   await scoped({action:'set-shape-adjustment',objectId:shape.id,shapeAdjustment:{key:adjusted.key,value:adjusted.min+(adjusted.max-adjusted.min)*.63}});
   const replacement=structuredClone(line),q=new URLSearchParams(replacement.query);q.set('connector-width','2.25');q.set('connector-arrow-width','7.5');q.set('connector-arrows','both');replacement.query=q.toString();
   fresh=await state();await scoped({action:'update-elements',elementIndices:[fresh.slides[fresh.current].elements.findIndex(e=>e.id===line.id)],elementsData:[replacement]});
   const edited=await state(),editedElements=structuredClone(edited.slides[edited.current].elements);
   const accepted=await page.evaluate(({shape,line})=>{const scene=deck.pages[pageIndex].scene,objects=scene.objects,scale=scene.width/245;return {labelSize:objects.find(o=>o.id===shape).label.size,lineWidth:objects.find(o=>o.id===line).paint.strokeWidth/scale,arrowWidth:objects.find(o=>o.id===line).arrowWidth/scale,arrows:objects.find(o=>o.id===line).arrows};},{shape:shape.id,line:line.id});assert.equal(accepted.labelSize,43);assert.equal(accepted.arrows,'both');assert(Math.abs(accepted.lineWidth-2.25)<1e-10&&Math.abs(accepted.arrowWidth-7.5)<1e-10,'Fresh connector sizes did not reach the scene in authored units');
   for(let i=0;i<4;i++)await action({action:'undo'});const restored=await state();assert.deepEqual(restored.slides[restored.current].elements,freshOwner,'Fresh group edits failed complete Undo');
   for(let i=0;i<4;i++)await action({action:'redo'});const redone=await state();assert.deepEqual(redone.slides[redone.current].elements,editedElements,'Fresh group edits failed complete Redo');
   const changedScene=await page.evaluate(()=>structuredClone(deck.pages[pageIndex].scene));delete changedScene.revision;
   const secondSave=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Edited integration.md'})}).then(r=>r.json()).then(r=>r.content));
   await open(secondSave,'Edited integration.md');await navigate();const secondScene=await page.evaluate(()=>structuredClone(deck.pages[pageIndex].scene));delete secondScene.revision;assert.deepEqual(secondScene,changedScene,'Second reopen lost shape/label/connector edits');await page.screenshot({path:output+'/'+mode+'-edited-reopen.png'});
   const connectorProof=await page.evaluate(async()=>{
    const original=structuredClone(deck.pages[pageIndex].scene),line=original.objects.find(o=>o.kind==='connector'&&!o.retroLines);if(!line)throw Error('Missing native connector fixture');
    line.arrows='end';
    original.objects=[line];const host=document.createElement('div');host.style.cssText='position:fixed;left:-10000px;width:1920px;height:1080px';document.body.append(host);const view=KeynopeScene.mount(host,original);await view.ready;const node=host.querySelector('polyline[data-object-id]'),defs=host.querySelector('defs');
    try{for(const opacity of [0,.5,undefined,1]){const next=structuredClone(original);if(opacity===undefined)delete next.objects[0].paint.opacity;else next.objects[0].paint.opacity=opacity;if(!view.update(next))throw Error('Opacity reconciliation rejected');if(host.querySelector('polyline[data-object-id]')!==node||host.querySelector('defs')!==defs)throw Error('Opacity replaced connector resources');if(node.style.opacity!==(opacity===undefined?'':String(opacity)))throw Error('Opacity not rendered: '+opacity);}
     const changed=structuredClone(original);changed.objects[0].paint.color='#123456';changed.objects[0].paint.strokeWidth=7;changed.objects[0].arrows='both';if(!view.update(changed))throw Error('Other connector paint rejected');const updated=host.querySelector('polyline[data-object-id]');if(updated.getAttribute('stroke')!=='#123456'||updated.getAttribute('stroke-width')!=='7'||!updated.hasAttribute('marker-start')||!updated.hasAttribute('marker-end'))throw Error('Retained signature swallowed other connector paint');return true;
    }finally{view.dispose();host.remove();}
   });assert.equal(connectorProof,true);
   assert.deepEqual(errors,[]);report.push({engine,mode,objects:after.objects.length,groupMembers:ids.length,retainedPaint:true,reopenParity:true,freshShapeLabelAdjustmentConnectorEdits:true,secondReopenParity:true});await page.close();
  }
  fs.writeFileSync(output+'/browser-report.json',JSON.stringify(report,null,2));console.log('PASS: normal/tab/base/layout mixed groups, shared paint, retained owners, fresh shape/label/adjustment/connector edits, complete Undo/Redo and second-reopen parity');
 }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);server.close();process.exitCode=1;});
