const {waitForAsync}=require("./wait_for_async.cjs");
// Owned synthetic table; headless browser, no external resources or user app.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site');
const server=http.createServer((req,res)=>{
  let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';
  const file=path.resolve(root,'.'+name);
  if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}
  res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));
});
(async()=>{
  await new Promise(r=>server.listen(0,'127.0.0.1',r));
  const browser=await playwright.chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
  try{
    const context=await browser.newContext({viewport:{width:1440,height:1000}});
    await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
    const page=await context.newPage(),errors=[];page.setDefaultTimeout(20000);page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
    const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
    const scene=()=>page.evaluate(()=>fetch('/api/editor/scene?slide=0').then(r=>r.json()));
    const action=async data=>{
      const result=await page.evaluate(async data=>{const r=await fetch('/api/editor/action',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(data)});if(!r.ok)throw Error(await r.text());return r.json();},data);
      await page.waitForFunction(version=>window.keynopeWorkspaceAppliedRevision===version,result.version);
      return result;
    };
    await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');
    await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
    const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();
    await(await chooser).setFiles({name:'table-theme.md',mimeType:'text/markdown',buffer:Buffer.from('<!-- keynope width=245 height=56 -->\n\n<!-- top=1 left=24 render=truetype modern-font=sans modern-size=70 -->\n# Workshop plan\n')});
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[0].elements.some(e=>e.text==='Workshop plan');});
    const opened=await state();await action({action:'insert-table',slide:0,sceneRevision:opened.version,rows:3,cols:2});
    let s=await state();const id=s.selectedTable.objectId,data=s.selectedTable.data;
    data.cells[0][0]={colSpan:2,text:'Plan the workshop'};data.cells[0][1]={hidden:true};
    data.cells[1][0].text='Explore ideas';data.cells[1][1]={text:'Explicit C64',style:{color:'#55aaff',fontId:'c64',fontSize:46,fill:'none'}};
    data.cells[2][0].text='Build together';data.cells[2][1].text='Share findings';
    await action({action:'set-table',slide:0,sceneRevision:s.version,objectId:id,tableData:data});
    const owned=structuredClone((await state()).selectedTable.data);
    const font={fontId:'mono',size:43,width:81,lineHeight:1.4};
    const theme={version:1,name:'Table theme',colors:{background:'#ffffff',surface:'#eef2f6',text:'#263445',heading:'#172033',muted:'#64748b',line:'#94a3b8',accent:['#713fd0','#0e7490','#b45309','#be123c','#15803d','#4338ca']},fonts:{body:font,heading:font,mono:font}};
    await action({action:'set-deck-theme',themeData:theme});
    const projected=(await scene()).objects.find(o=>o.id===id).table;
    assert.equal(projected.cells[0].fill,'#713fd0');assert.equal(projected.cells[1].text.size,43);assert.equal(projected.cells[1].paint.color,'#263445');
    assert.equal(projected.cells[2].text.fontId,'c64');assert.equal(projected.cells[2].text.size,46);
    assert.deepEqual((await state()).selectedTable.data,owned,'Theme materialized cell overrides');
    const table=page.locator('#stage [data-object-id="'+id+'"] .keynope-scene-table');
    await table.locator('[data-table-cell="0:0"]').waitFor();
    assert.equal(await table.locator('[data-table-cell="0:0"]').evaluate(e=>e.style.background),'rgb(113, 63, 208)');
    assert.equal(await table.locator('[data-table-cell="1:0"]').evaluate(e=>e.style.borderColor),'rgb(148, 163, 184)');
    assert.equal(await table.locator('[data-table-cell="1:0"]').evaluate(e=>e.style.borderWidth),'0px');
    assert.equal(await table.locator('[data-table-border="vertical:1:1:3"]').count(),1,'Merged interior/adjacent seams were not resolved once');
    fs.mkdirSync('output/powerup',{recursive:true});await table.screenshot({path:'output/powerup/table-theme-light.png'});
    // Canvas controls display effective inherited values without writing them.
    await page.locator('.keynope-canvas-element.active').dblclick();
    const dialog=page.getByRole('dialog',{name:'Edit table cells',exact:true});
    await dialog.locator('[contenteditable=true]').waitFor();
    if(await dialog.locator('[contenteditable=true]').count())await dialog.locator('[contenteditable=true]').press('Escape');
    await dialog.getByRole('gridcell',{name:'Cell A2',exact:true}).click();
    const size=dialog.getByRole('spinbutton',{name:'Canvas cell font size',exact:true});
    assert.equal(await size.inputValue(),'43');
    // Track geometry updates retain the exact SVG layer and border node. No
    // redundant cell borders or full table rebuild during the gesture.
    const edge=dialog.locator('[data-table-border="vertical:1:1:3"]');
    const oldX=Number(await edge.getAttribute('x1'));
    await edge.evaluate(node=>{window.tableBorderProof=node;window.tableBorderLayerProof=node.parentElement;});
    await dialog.getByRole('separator',{name:'Resize canvas column A',exact:true}).press('ArrowRight');
    assert.ok(Number(await edge.getAttribute('x1'))>oldX);
    assert.equal(await edge.evaluate(node=>node===window.tableBorderProof&&node.parentElement===window.tableBorderLayerProof),true);
    await dialog.getByRole('button',{name:'Undo cell change',exact:true}).click();
    assert.equal(Number(await edge.getAttribute('x1')),oldX);
    const borderWidth=dialog.getByRole('spinbutton',{name:'Canvas cell border width',exact:true});
    await borderWidth.fill('0');await borderWidth.press('Tab');
    await page.waitForFunction(()=>!document.querySelector('dialog[aria-label="Edit table cells"]')?.hasAttribute('aria-busy'));
    assert.equal(await dialog.locator('[data-table-border="horizontal:1:0:2"]').count(),0,'Explicit Off retained the old whole seam');
    assert.equal(await dialog.locator('[data-table-border="horizontal:1:1:2"]').count(),1,'Explicit Off removed a neighbouring visible segment');
    await dialog.screenshot({path:'output/powerup/table-shared-border-off.png'});
    assert.deepEqual((await state()).selectedTable.data,owned,'Draft border change escaped into authored data');
    await dialog.getByRole('button',{name:'Undo cell change',exact:true}).click();
    await page.waitForFunction(()=>!document.querySelector('dialog[aria-label="Edit table cells"]')?.hasAttribute('aria-busy'));
    assert.equal(await dialog.locator('[data-table-border="horizontal:1:0:2"]').count(),1);
    await dialog.getByRole('button',{name:'Cancel',exact:true}).click();
    const dark=structuredClone(theme);dark.colors={...dark.colors,background:'#111827',surface:'#1f2937',text:'#e5e7eb',heading:'#ffffff',line:'#64748b',accent:['#ffbf00',...dark.colors.accent.slice(1)]};dark.fonts.body={...dark.fonts.body,size:47,fontId:'sans'};
    await action({action:'set-deck-theme',themeData:dark});
    const changed=(await scene()).objects.find(o=>o.id===id).table;
    assert.equal(changed.cells[0].fill,'#ffbf00');assert.equal(changed.cells[0].paint.color,'#111827');assert.equal(changed.cells[1].text.fontId,'sans');assert.equal(changed.cells[1].text.size,47);assert.equal(changed.cells[2].text.size,46);
    await table.screenshot({path:'output/powerup/table-theme-dark.png'});
    await action({action:'undo'});assert.deepEqual((await scene()).objects.find(o=>o.id===id).table,projected);
    await action({action:'redo'});assert.deepEqual((await scene()).objects.find(o=>o.id===id).table,changed);
    assert.deepEqual((await state()).selectedTable.data,owned);assert.deepEqual(errors,[]);
    const legacy=await page.evaluate(async()=>{const scene=await fetch('/api/editor/scene?slide=0').then(r=>r.json()),object=scene.objects.find(o=>o.kind==='table');delete object.table.borders;const host=document.createElement('div');document.body.append(host);const view=KeynopeScene.mount(host,{...scene,objects:[object]});try{await view.ready;return {layer:host.querySelectorAll('.keynope-table-borders').length,width:host.querySelector('td,th').style.borderWidth};}finally{view.dispose();host.remove();}});
    assert.deepEqual(legacy,{layer:0,width:'1px'},'Legacy table scenes lost their borders');
    // Imported four-sided borders remain editable; all-side controls must not
    // be defeated by their old per-edge overrides or pretend they are uniform.
    const edged=structuredClone(owned);edged.cells[1][0].style={borders:{left:{color:'none',width:0},right:{color:'#ff0055',width:4},top:{color:'#00ffaa',width:2},bottom:{color:'#4455ff',width:1}}};
    await action({action:'set-table',slide:0,sceneRevision:(await state()).version,objectId:id,tableData:edged});
    assert.equal(await table.locator('[data-table-border="vertical:1:1:2"]').getAttribute('stroke'),'#ff0055');
    assert.equal(await table.locator('[data-table-border="horizontal:1:0:1"]').getAttribute('stroke'),'#00ffaa');
    assert.equal(await table.locator('[data-table-border="vertical:0:1:2"]').count(),0);
    await page.locator('.keynope-canvas-element.active').dblclick();
    await dialog.locator('[contenteditable=true]').waitFor();await dialog.locator('[contenteditable=true]').press('Escape');
    await dialog.getByRole('gridcell',{name:'Cell A2',exact:true}).click();
    assert.equal(await borderWidth.getAttribute('data-mixed'),'true');assert.equal(await borderWidth.inputValue(),'');
    await borderWidth.fill('5');await borderWidth.press('Tab');
    await page.waitForFunction(()=>!document.querySelector('dialog[aria-label="Edit table cells"]')?.hasAttribute('aria-busy'));
    assert.equal(await dialog.locator('[data-table-border="vertical:1:1:2"]').getAttribute('stroke-width'),'5');
    await dialog.getByRole('button',{name:'Increase canvas cell border width',exact:true}).click();
    await page.waitForFunction(()=>!document.querySelector('dialog[aria-label="Edit table cells"]')?.hasAttribute('aria-busy'));
    assert.equal(await borderWidth.inputValue(),'6');
    assert.equal(await dialog.locator('[data-table-border="horizontal:2:0:1"]').getAttribute('stroke-width'),'6');
    await dialog.screenshot({path:'output/powerup/table-four-sided-borders.png'});
    await dialog.getByRole('button',{name:'Done',exact:true}).click();
    await page.waitForFunction(()=>!document.querySelector('dialog[aria-label="Edit table cells"]'));
    const applied=(await state()).selectedTable.data.cells[1][0].style;
    assert.equal(applied.borderWidth,6);assert.deepEqual(applied.borders,{left:{color:'none'},right:{color:'#ff0055'},top:{color:'#00ffaa'},bottom:{color:'#4455ff'}});
    await action({action:'undo'});assert.deepEqual((await state()).selectedTable.data,edged);
    await action({action:'redo'});assert.deepEqual((await state()).selectedTable.data.cells[1][0].style,applied);
    assert.deepEqual(errors,[]);
    console.log('PASS: theme-aware merged table paint, shared seams, retained border geometry during resize, inherited canvas values, explicit C64 overrides, undo/redo and immutable payload');
  }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);server.close();process.exitCode=1;});
