// Build first: tools/build_web_editor.sh /tmp/keynope-powerup-site/editor
// Production WASM/shared-inspector checks using an owned synthetic deck.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const {waitForAsync}=require('./wait_for_async.cjs');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),output=path.resolve(process.env.TEST_OUTPUT||'output/powerup');
const server=http.createServer((req,res)=>{
  let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';
  const file=path.resolve(root,'.'+name);
  if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}
  res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm','.json':'application/json'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));
});
(async()=>{
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const engine=process.env.TEST_BROWSER||'chromium',browser=await playwright[engine].launch({headless:true,...(engine==='webkit'?{}:{executablePath:process.env.CHROME_PATH})});
  try{
    const context=await browser.newContext({viewport:{width:1440,height:1000}});
    await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());
    const page=await context.newPage(),errors=[];page.setDefaultTimeout(20000);page.on('pageerror',e=>errors.push(e.message));
    page.on('dialog',async dialog=>{assert.equal(dialog.type(),'confirm');await dialog.accept();});
    const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
    const action=data=>page.evaluate(async data=>{const r=await fetch('/api/editor/action',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(data)});return {status:r.status,body:r.ok?await r.json():await r.text()};},data);
    await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');
    await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
    const fileChooser=page.waitForEvent('filechooser');
    await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();
    await (await fileChooser).setFiles({name:'selection.md',mimeType:'text/markdown',buffer:Buffer.from(`<!-- keynope width=245 height=56 -->

<!-- top=4 left_pct=0.1 width=90 height=8 render=truetype modern-size=120 modern-font=c64 -->
First text

<!-- top=20 left_pct=0.1 width=90 height=8 render=truetype modern-size=160 modern-font=mono -->
Second text

<!-- top=35 left_pct=0.1 width=30 height=10 fg=#55aaff shape=square -->
[shape:square]
`)});
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[0].elements?.some(e=>e.text==='Second text');});
    const initial=await state(),indices=initial.slides[0].elements.map((_,i)=>i);
    assert.equal(indices.length,3);
    const selection=await action({action:'select-elements',elementIndices:indices,elementsData:initial.slides[0].elements,element:0});
    assert.equal(selection.status,200,selection.body);
    const size=page.getByRole('spinbutton',{name:'Font size',exact:true});
    await size.waitFor();
    assert.equal(await size.inputValue(),'');assert.equal(await size.getAttribute('placeholder'),'Mixed');
    const mixed=await state();assert.equal(mixed.selectionCapabilities.properties['text.size'].eligibleIds.length,2);
    assert.equal(mixed.selectionCapabilities.properties['text.size'].totalCount,3);
    assert.equal(await page.getByRole('combobox',{name:'Font family',exact:true}).inputValue(),'mixed');
    fs.mkdirSync(output,{recursive:true});await page.screenshot({path:path.join(output,'selection-mixed.png')});
    await size.fill('180');await size.press('Tab');
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities?.properties['text.size'].value===180;});
    await page.waitForFunction(()=>document.querySelector('[data-selection-property="text.size"]')?.value==='180');
    const after=await state();assert.equal(after.version,mixed.version+1);
    assert.equal(new URLSearchParams(after.slides[0].elements.find(e=>e.kind==='shape').query).has('modern-size'),false);
    await page.getByRole('button',{name:'Increase font size',exact:true}).click();
    await page.waitForFunction(()=>document.querySelector('[data-selection-property="text.size"]')?.value==='181');
    await page.getByRole('button',{name:'Undo',exact:true}).click();
    await page.waitForFunction(()=>document.querySelector('[data-selection-property="text.size"]')?.value==='180');
    await page.getByRole('button',{name:'Undo',exact:true}).click();
    await page.waitForFunction(()=>document.querySelector('[data-selection-property="text.size"]')?.placeholder==='Mixed');
    const locked=await action({action:'set-object-lock',value:1});assert.equal(locked.status,200);
    await page.waitForFunction(()=>document.querySelector('[data-selection-property="text.size"]')?.disabled===true);
    const lockedState=await state();
    const invalid=await action({action:'set-selection-property',slide:0,sceneRevision:lockedState.version,objectIds:lockedState.selectionCapabilities.targetIds,property:'text.size',propertyValue:190});
    assert.equal(invalid.status,400);assert.deepEqual(await state(),lockedState);
    assert.equal((await action({action:'set-object-lock',value:0})).status,200);
    await page.waitForFunction(()=>document.querySelector('[data-selection-property="text.size"]')?.disabled===false);
    await page.getByRole('tab',{name:'Style',exact:true}).click();
    const opacity=page.getByRole('spinbutton',{name:'Object opacity',exact:true});
    await opacity.fill('40');await opacity.press('Tab');
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities?.properties['paint.opacity'].value===0.4;});
    const painted=await state();assert.equal(painted.selectionCapabilities.properties['paint.opacity'].eligibleIds.length,3);
    await page.screenshot({path:path.join(output,'selection-opacity.png')});
    const paintPanel=page.getByRole('region',{name:'Selection paint',exact:true});
    await paintPanel.waitFor();
    await page.getByRole('button',{name:'Selection base colour',exact:true}).click();
    let colorPicker=page.getByRole('dialog',{name:'Pick color',exact:true});
    await colorPicker.getByRole('textbox',{name:'HTML color',exact:true}).fill('#AA55FF');
    await colorPicker.getByRole('button',{name:'Apply',exact:true}).click();
    await page.waitForFunction(()=>document.querySelector('[data-selection-property="paint.color"]')?.getAttribute('aria-description')==='#aa55ff');
    const colored=await state();assert.equal(colored.selectionCapabilities.properties['paint.color'].value,'#aa55ff');assert.equal(colored.version,painted.version+1);
    await page.locator('.keynope-selection-paint summary').filter({hasText:'Gradient'}).click();
    await page.getByRole('combobox',{name:'Selection gradient direction',exact:true}).selectOption('vertical');
    await page.waitForFunction(()=>document.querySelector('[data-selection-property="paint.gradientDirection"]')?.value==='vertical');
    assert.equal((await state()).selectionCapabilities.properties['paint.gradientStart'].value,'#55aaff');
    await page.getByRole('combobox',{name:'Selection gradient direction',exact:true}).selectOption('none');
    await page.waitForFunction(()=>document.querySelector('[data-selection-property="paint.gradientDirection"]')?.value==='none');
    await page.locator('.keynope-selection-paint summary').filter({hasText:'Shadow'}).click();
    await page.getByRole('combobox',{name:'Selection shadow mode',exact:true}).selectOption('soft');
    await page.waitForFunction(()=>document.querySelector('[data-selection-property="paint.shadow"]')?.value==='soft');
    const shadowX=page.getByRole('spinbutton',{name:'Selection shadow x',exact:true});await shadowX.fill('-2');await shadowX.press('Tab');
    await page.waitForFunction(()=>document.querySelector('[data-selection-property="paint.shadowX"]')?.value==='-2');
    await page.getByRole('combobox',{name:'Selection outline',exact:true}).selectOption('light');
    await page.waitForFunction(()=>document.querySelector('[data-selection-property="paint.outline"]')?.value==='light');
    await page.getByRole('combobox',{name:'Selection outline',exact:true}).selectOption('none');
    await page.waitForFunction(()=>document.querySelector('[data-selection-property="paint.outline"]')?.value==='none');
    await page.getByRole('button',{name:'Undo',exact:true}).click();
    await page.waitForFunction(()=>document.querySelector('[data-selection-property="paint.outline"]')?.value==='light');
    await page.screenshot({path:path.join(output,'selection-paint.png')});
    await page.setViewportSize({width:520,height:900});
    const paintFits=await paintPanel.evaluate(node=>node.scrollWidth<=node.clientWidth);
    if(!paintFits){await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));console.error('Paint after two frames',await paintPanel.evaluate(node=>({client:node.clientWidth,scroll:node.scrollWidth,descendants:[...node.querySelectorAll('*')].filter(n=>n.scrollWidth>n.clientWidth).map(n=>({tag:n.tagName,class:n.className,client:n.clientWidth,scroll:n.scrollWidth,open:n.open,text:n.textContent.slice(0,100)}))})));}
    if(!paintFits){console.error('Narrow paint overflow',await paintPanel.evaluate(node=>({client:node.clientWidth,scroll:node.scrollWidth,rect:node.getBoundingClientRect().toJSON(),children:[...node.querySelectorAll('*')].filter(n=>n.getBoundingClientRect().right>node.getBoundingClientRect().right).map(n=>({tag:n.tagName,class:n.className,text:n.textContent.slice(0,150),label:n.getAttribute('aria-label'),rect:n.getBoundingClientRect().toJSON(),font:getComputedStyle(n).font}))})));await page.screenshot({path:path.join(output,'selection-paint-overflow.png')});}assert(paintFits,'shared paint controls fit the narrow inspector');
    await page.screenshot({path:path.join(output,'selection-paint-narrow.png')});
    await page.setViewportSize({width:1440,height:1000});
    // The picker owns stable targets/revision. A selection change while it is
    // open must reject the old paint operation rather than colour the new item.
    await page.getByRole('button',{name:'Selection base colour',exact:true}).click();
    colorPicker=page.getByRole('dialog',{name:'Pick color',exact:true});
    const beforeStale=await state();
    assert.equal((await action({action:'select-element',element:2,elementData:beforeStale.slides[0].elements[2]})).status,200);
    await colorPicker.getByRole('textbox',{name:'HTML color',exact:true}).fill('#123456');
    await colorPicker.getByRole('button',{name:'Apply',exact:true}).click();
    await page.waitForFunction(()=>{const alert=document.querySelector('.keynope-selection-paint [role="alert"]');return alert&&!alert.hidden&&alert.textContent.includes('document changed');});
    assert.equal(new URLSearchParams((await state()).slides[0].elements[2].query).get('fg'),'#aa55ff');
    assert.equal((await action({action:'select-elements',elementIndices:indices,elementsData:beforeStale.slides[0].elements,element:0})).status,200);
    await page.waitForFunction(()=>document.querySelector('[data-selection-property="text.size"]')?.placeholder==='Mixed');
    await page.getByRole('tab',{name:'Arrange',exact:true}).click();
    assert.equal(await page.getByRole('button',{name:'Distribute horizontally',exact:true}).isEnabled(),true);
    assert.equal(await page.getByRole('button',{name:'Rotate together',exact:true}).isEnabled(),true);
    const alignmentReference=page.getByRole('combobox',{name:'Alignment reference',exact:true});
    await alignmentReference.selectOption('selection');
    const topBefore=await page.evaluate(()=>Math.min(...deck.pages[pageIndex].scene.objects.filter(o=>o.editable).map(o=>o.bounds.y)));
    // Hold the shared queue, then advance only selection state. Alignment must
    // retain its captured targets rather than silently drop on the new version.
    await page.evaluate(async()=>{const original=window.fetch.bind(window);let started;const ready=new Promise(resolve=>started=resolve);window.fetch=async(input,init={})=>{if(String(input)==='/api/editor/action'&&JSON.parse(init.body||'{}').name==='arrange-barrier'){started();return new Promise(resolve=>window.releaseArrangeBarrier=async()=>resolve(new Response(JSON.stringify(await original('/api/editor/state').then(r=>r.json())),{status:200,headers:{'Content-Type':'application/json'}})));}return original(input,init);};window.arrangeBarrier=editorAction({action:'select-element',name:'arrange-barrier',element:-1});await ready;});
    await page.getByRole('button',{name:'Align top',exact:true}).click();
    const beforeAlign=await state();
    assert.equal((await action({action:'select-elements',elementIndices:indices,elementsData:beforeAlign.slides[0].elements,element:0})).status,200);
    const selectedAlign=await state();assert(selectedAlign.version>beforeAlign.version);assert.equal(selectedAlign.mutationRevision,beforeAlign.mutationRevision);
    await page.evaluate(async()=>{await releaseArrangeBarrier();await arrangeBarrier;});
    await page.waitForFunction(top=>{
      const objects=deck.pages[pageIndex]?.scene?.objects.filter(o=>o.editable)||[];
      return objects.length===3&&objects.every(o=>Math.abs(o.bounds.y-top)<0.01);
    },topBefore).catch(async error=>{console.error('Alignment failure:',JSON.stringify(await page.evaluate(()=>({scene:deck.pages[pageIndex]?.scene?.objects.map(o=>({id:o.id,bounds:o.bounds,editable:o.editable})),notice:document.querySelector('.keynope-command-error')?.textContent}))),JSON.stringify(await state()));throw error;});
    assert.equal((await state()).mutationRevision,selectedAlign.mutationRevision+1,'Queued alignment must commit exactly once');
    await alignmentReference.selectOption('slide');
    await page.getByRole('button',{name:'Undo',exact:true}).click();
    await page.waitForFunction(()=>{
      const objects=deck.pages[pageIndex]?.scene?.objects.filter(o=>o.editable)||[];
      return new Set(objects.map(o=>o.bounds.y)).size>1;
    });
    const moves=await page.evaluate(()=>KeynopeWorkspace.alignmentMoves([
      {id:'group',bounds:{minX:10,minY:5,maxX:40,maxY:15}},
      {id:'other',bounds:{minX:60,minY:20,maxX:80,maxY:40}}
    ],'right',false,{minX:10,minY:5,maxX:80,maxY:40}));
    assert.deepEqual(moves,[{id:'group',dx:40,dy:0},{id:'other',dx:0,dy:0}]);
    const textIndex=painted.slides[0].elements.findIndex(e=>e.text==='First text');
    assert.equal((await action({action:'select-element',element:textIndex,elementData:painted.slides[0].elements[textIndex]})).status,200);
    const x=page.getByRole('spinbutton',{name:'Object x',exact:true}),y=page.getByRole('spinbutton',{name:'Object y',exact:true});
    const w=page.getByRole('spinbutton',{name:'Object width',exact:true}),h=page.getByRole('spinbutton',{name:'Object height',exact:true});
    // The API setup bypasses the editor's action-response path; let the poll
    // replace the preceding multi-selection form before driving its fields.
    await page.waitForFunction(()=>document.querySelector('[aria-label="Object width"]')?.disabled===false);
    await x.fill('35.125');await y.fill('4.25');await w.fill('110.375');await h.fill('10.125');
    const draftBefore=await state();
    await page.evaluate(slide=>editorAction({action:'update-slide-notes',slide,notes:'Save notes while Arrange fields are drafted'}),draftBefore.current);
    assert.equal((await state()).mutationRevision,draftBefore.mutationRevision);
    await page.getByRole('tab',{name:'Text',exact:true}).click();
    await page.getByRole('tab',{name:'Arrange',exact:true}).click();
    assert.equal(await x.inputValue(),'35.125','Note save discarded drafted X');
    assert.equal(await w.inputValue(),'110.375','Note save discarded drafted width');
    const beforeGeometry=await state();
    await page.getByRole('button',{name:'Apply geometry',exact:true}).click();
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities?.geometry?.width===110.375;});
    try{await page.waitForFunction(()=>document.querySelector('[aria-label="Object x"]')?.value==='35.125'&&document.querySelector('[aria-label="Object width"]')?.value==='110.375');}
    catch(error){console.error('Geometry diagnostic',JSON.stringify({state:await state(),fields:await page.locator('[aria-label^="Object "]').evaluateAll(nodes=>nodes.map(n=>({label:n.getAttribute('aria-label'),value:n.value}))),errors}));await page.screenshot({path:path.join(output,'selection-geometry-failure.png')});throw error;}
    const positioned=await state();assert.equal(positioned.version,beforeGeometry.version+1);
    assert.equal(positioned.selectionCapabilities.properties['text.size'].value,120);
    assert.equal(positioned.selectionCapabilities.properties['paint.opacity'].value,0.4);
    const aspect=page.getByRole('checkbox',{name:'Lock object aspect ratio',exact:true});
    await aspect.check();await w.fill('55.1875');assert.equal(Number(await h.inputValue()),5.0625);
    await page.getByRole('button',{name:'Reset fields',exact:true}).click();
    assert.equal(Number(await w.inputValue()),110.375);assert.equal(Number(await h.inputValue()),10.125);
    assert.equal((await state()).version,positioned.version,'Reset fields must not mutate the deck');
    assert.equal(await aspect.isChecked(),true);
    await w.fill('55.1875');await page.getByRole('button',{name:'Apply geometry',exact:true}).click();
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities?.geometry?.width===55.1875&&s.selectionCapabilities?.geometry?.height===5.0625;});
    assert.equal((await state()).selectionCapabilities.properties['text.size'].value,120,'Aspect resizing changes the box, not typography');
    assert.equal(await aspect.isChecked(),true,'Aspect lock survives accepted geometry');
    await h.fill('10.125');assert.equal(Number(await w.inputValue()),110.375);
    await page.getByRole('button',{name:'Apply geometry',exact:true}).click();
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities?.geometry?.width===110.375&&s.selectionCapabilities?.geometry?.height===10.125;});
    await aspect.uncheck();await w.fill('100');assert.equal(Number(await h.inputValue()),10.125);
    await page.getByRole('button',{name:'Reset fields',exact:true}).click();
    const rotation=page.getByRole('spinbutton',{name:'Object rotation degrees',exact:true});
    await rotation.fill('22.5');await rotation.press('Tab');
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities?.properties['geometry.rotation'].value===22.5;});
    await page.waitForFunction(()=>document.querySelector('[aria-label="Object rotation degrees"]')?.value==='22.5');
    await page.screenshot({path:path.join(output,'selection-geometry.png')});
    await page.getByRole('button',{name:'Undo',exact:true}).click();
    await page.waitForFunction(()=>document.querySelector('[aria-label="Object rotation degrees"]')?.value==='0');
    assert.equal((await action({action:'set-object-lock',value:1})).status,200);
    await page.waitForFunction(()=>document.querySelector('[data-selection-command="arrange.geometry"]')?.disabled===true);
    assert.equal(await page.getByRole('button',{name:'Align left',exact:true}).isDisabled(),true);
    // A master-bound shape must use the accepted numeric position, including
    // whole coordinates which remove inherited fractional offsets.
    const masters={version:1,base:{id:'base',name:'Base',slide:{}},layouts:[{id:'position',name:'Position',slide:{elements:[{id:'master-shape',slotId:'slot',placeholderRole:'body',kind:'shape',query:'shape=square&width=40&height=12&align=center&valign=middle&left_pct=0.6&bottom=2&object-offset-x=0.25&shape-offset-x=0.5&object-offset-y=0.25&shape-offset-y=0.5&fg=%2300aa55'}]}}]};
    await page.evaluate(()=>editorAction({action:'select-element',element:-1}));
    const chooser=page.waitForEvent('filechooser');
    await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();
    await (await chooser).setFiles({name:'geometry-master.md',mimeType:'text/markdown',buffer:Buffer.from(`<!-- keynope width=245 height=56 -->\n<!-- keynope-masters version=1 base64:${Buffer.from(JSON.stringify(masters)).toString('base64')} -->\n<!-- layout=position -->\n<!-- master-slot=slot -->\n<!-- element-id=instance shape=square -->\n[shape:square]\n`)});
    await waitForAsync(page,async()=> (await fetch('/api/editor/state').then(r=>r.json())).path==='geometry-master.md');
    const inherited=await state(),shapeIndex=inherited.slides[0].elements.findIndex(e=>e.kind==='shape');
    assert(shapeIndex>=0);
    const id=inherited.slides[0].elements[shapeIndex].id;
    await page.evaluate(({i,e})=>editorAction({action:'select-element',element:i,elementData:e}),{i:shapeIndex,e:inherited.slides[0].elements[shapeIndex]});
    await page.getByRole('tab',{name:'Arrange',exact:true}).click();
    await page.waitForFunction(()=>document.querySelector('[aria-label="Object x"]')?.disabled===false);
    assert.equal(await aspect.isChecked(),false,'New documents must not inherit geometry drafts');
    // Exercise the non-numeric path before the numeric form has established
    // any explicit anchor suppression on this master-bound instance.
    await page.getByRole('button',{name:'Align left',exact:true}).click();
    await page.waitForFunction(id=>Math.abs(deck.pages[pageIndex]?.scene?.objects.find(o=>o.id===id)?.bounds.x)<0.001,id);
    const alignedMaster=await state();
    const alignedQuery=new URLSearchParams(alignedMaster.slides[0].elements.find(e=>e.id===id).query);
    assert((alignedQuery.get('master-clear')||'').split(',').includes('left_pct'));
    assert((alignedQuery.get('master-clear')||'').split(',').includes('shape-offset-x'));
    await page.getByRole('button',{name:'Undo',exact:true}).click();
    await page.waitForFunction(id=>deck.pages[pageIndex]?.scene?.objects.find(o=>o.id===id)?.bounds.x>1,id);
    for(const point of [[12.875,7.625],[12,7]]){
      await x.fill(String(point[0]));await y.fill(String(point[1]));await w.fill('40');await h.fill('12');
      await page.getByRole('button',{name:'Apply geometry',exact:true}).click();
      await page.waitForFunction(({id,point})=>{
        const o=deck.pages[pageIndex]?.scene?.objects.find(o=>o.id===id);
        return o&&Math.abs(o.bounds.x-point[0]*1920/245)<0.001&&Math.abs(o.bounds.y-point[1]*1080/56)<0.001;
      },{id,point});
      assert.equal(await x.inputValue(),String(point[0]));assert.equal(await y.inputValue(),String(point[1]));
    }
    await page.screenshot({path:path.join(output,'selection-master-geometry.png')});
    // Group movement cancellation must restore the preview without a history
    // entry, and release its handlers so a following drag still works.
    await page.evaluate(()=>editorAction({action:'select-element',element:-1}));
    const groupChooser=page.waitForEvent('filechooser');
    await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();
    await (await groupChooser).setFiles({name:'group-gestures.md',mimeType:'text/markdown',buffer:Buffer.from(`<!-- keynope width=245 height=56 -->
<!-- top=10 left=20 width=30 height=10 shape=square -->
[shape:square]

<!-- top=25 left=70 width=25 height=8 shape=square -->
[shape:square]
`)});
    await waitForAsync(page,async()=> (await fetch('/api/editor/state').then(r=>r.json())).path==='group-gestures.md');
    const members=(await state()).slides[0].elements;
    await page.evaluate(async members=>{await editorAction({action:'select-elements',elementIndices:members.map((_,i)=>i),elementsData:members,element:0});await editorAction({action:'group-elements'});},members);
    const groupHit=page.locator('.keynope-canvas-element[data-group]').first();
    await groupHit.waitFor();
    const startGroup=await state();
    const pointerStart=async()=>{
      const box=await groupHit.locator('.keynope-group-member-hit').first().boundingBox();assert(box);
      await page.mouse.move(box.x+box.width/2,box.y+box.height/2);await page.mouse.down();
      await page.mouse.move(box.x+box.width/2+24,box.y+box.height/2+16,{steps:3});
    };
    for(const cancellation of ['escape','blur']){
      await pointerStart();
      if(cancellation==='escape')await page.keyboard.press('Escape');
      else await page.evaluate(()=>window.dispatchEvent(new Event('blur')));
      await page.mouse.up();
      assert.deepEqual((await state()).slides,startGroup.slides,cancellation+' must not change geometry');
      assert.equal((await state()).version,startGroup.version,cancellation+' must not create history');
    }
    await pointerStart();await page.mouse.up();
    await waitForAsync(page,async version=>(await fetch('/api/editor/state').then(r=>r.json())).version>version,startGroup.version);
    const movedGroup=await state();assert.notDeepEqual(movedGroup.slides,startGroup.slides);
    await page.getByRole('button',{name:'Undo',exact:true}).click();
    assert.deepEqual((await state()).slides,startGroup.slides,'One undo restores the entire group');
    assert.deepEqual(errors,[]);
    console.log('PASS: mixed/subset values, typography, numeric +/− and undo, lock preflight, shared opacity/paint/effects, stale picker targets, precise geometry, rotation and command eligibility');
  }finally{await browser.close();server.close();}
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
