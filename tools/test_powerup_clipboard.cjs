const {waitForAsync}=require("./wait_for_async.cjs");
// Build first: tools/build_web_editor.sh /tmp/keynope-powerup-site/editor
// Synthetic, self-contained fixtures only; no workshop/customer deck is used.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http'),zlib=require('node:zlib');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site');
const server=http.createServer((req,res)=>{
  let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';
  const file=path.resolve(root,'.'+name);
  if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}
  res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm','.json':'application/json'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));
});
(async()=>{
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const browser=await playwright.chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
  try{
    const context=await browser.newContext({viewport:{width:1440,height:1000},permissions:['clipboard-read','clipboard-write']});
    await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());
    const page=await context.newPage(),errors=[];page.setDefaultTimeout(20000);page.on('pageerror',e=>errors.push(e.message));
    page.on('dialog',async dialog=>{assert.equal(dialog.type(),'confirm');await dialog.accept();});
    const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
    const document=()=>page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'test.md'})}).then(r=>r.json()));
    const newBlank=async()=>{
      await page.getByRole('button',{name:'New presentation',exact:true}).click();
      await page.getByRole('button',{name:'Use Blank template',exact:true}).click();
      await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides.length===1&&!s.slides[0].elements?.length;});
    };
    await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');
    await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
    const chooserEvent=page.waitForEvent('filechooser');
    await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();
    await (await chooserEvent).setFiles({name:'clipboard-source.md',mimeType:'text/markdown',buffer:Buffer.from('<!-- keynope width=245 height=56 -->\n\n<!-- top=8 left_pct=0.1 width=150 height=15 render=truetype ttf-size=97 -->\nPortable clipboard text\n')});
    await waitForAsync(page,async()=>(await fetch('/api/editor/state').then(r=>r.json())).slides[0].elements[0]?.text==='Portable clipboard text');
    await page.locator('.keynope-canvas-overlay .keynope-canvas-element').first().click();
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.selected>=0&&s.slides[0].elements[0].text==='Portable clipboard text';});
    const source=await state();
    await page.evaluate(()=>window.keynopeClipboardCommand('copy'));
    const readObjects=()=>page.evaluate(async()=>{
      for(const item of await navigator.clipboard.read())if(item.types.includes('web application/x-keynope-objects'))return await (await item.getType('web application/x-keynope-objects')).text();
      throw Error('Keynope clipboard type not found');
    });
    const raw=await readObjects();
    assert.equal(await page.evaluate(()=>navigator.clipboard.readText()),'Portable clipboard text','Other applications receive readable text');
    assert(raw.startsWith('keynope-elements:{'));
    const bundle=JSON.parse(raw.slice('keynope-elements:'.length));
    assert.equal(bundle.version,1);assert.equal(bundle.elements.length,1);
    assert.deepEqual(await state(),source,'Copy must not change the document/history/revision');
    await newBlank();
    await page.evaluate(()=>window.keynopeClipboardCommand('paste'));
    let pasted=await state();
    assert.equal(pasted.slides[0].elements[0].text,'Portable clipboard text');
    assert.notEqual(pasted.slides[0].elements[0].id,bundle.elements[0].id);
    assert.equal(pasted.canUndo,true);
    await page.getByRole('button',{name:'Undo',exact:true}).click();
    await waitForAsync(page,async()=>!(await fetch('/api/editor/state').then(r=>r.json())).slides[0].elements?.length);
    await page.getByRole('button',{name:'Redo',exact:true}).click();
    await waitForAsync(page,async()=>!!(await fetch('/api/editor/state').then(r=>r.json())).slides[0].elements?.length);
    pasted=await state();
    const rejected=await page.evaluate(async data=>{
      const bad=JSON.parse(JSON.stringify(data));bad.elements[0].kind='chart';bad.elements[0].objectId='missing-chart';
      const r=await fetch('/api/editor/paste-clipboard',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'paste-objects',clipboard:bad})});return r.status;
    },bundle);
    assert.equal(rejected,400);assert.deepEqual(await state(),pasted,'Invalid paste must be atomic');
    const stalePaste=await page.evaluate(async({data,version})=>{
      const r=await fetch('/api/editor/paste-clipboard',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'paste-objects',clipboard:data,sceneRevision:version-1})});return r.status;
    },{data:bundle,version:pasted.version});
    assert.equal(stalePaste,409);assert.deepEqual(await state(),pasted,'Stale paste must not change document/view/history');
    // Exercise the shared command boundary through the actual WASM dispatcher.
    await page.evaluate(()=>window.keynopeClipboardCommand('paste'));
    const batchBefore=await state();assert.equal(batchBefore.slides[0].elements.length,2);
    const updateBatch=async elements=>page.evaluate(async({elements,revision})=>{
      const r=await fetch('/api/editor/action',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'update-elements',sceneRevision:revision,elementIndices:[1,0],elementsData:elements})});
      return r.status;
    },{elements,revision:batchBefore.version});
    let edits=batchBefore.slides[0].elements.map(e=>({...e}));
    edits[0].text='Atomic first edit';edits[1].query='fg=%zz';
    assert.equal(await updateBatch(edits),400);
    assert.deepEqual(await state(),batchBefore,'A late batch validation error must preserve document/view/history');
    edits=batchBefore.slides[0].elements.map((e,i)=>({...e,text:'Atomic edit '+i}));
    assert.equal(await updateBatch(edits),200);
    const batchAfter=await state();
    for(const e of edits)assert.equal(batchAfter.slides[0].elements.find(item=>item.id===e.id).text,e.text,'Stale positions must still target IDs');
    await page.getByRole('button',{name:'Undo',exact:true}).click();
    await waitForAsync(page,async()=>!(await fetch('/api/editor/state').then(r=>r.json())).slides[0].elements.some(e=>e.text.startsWith('Atomic edit')));
    assert.deepEqual((await state()).slides,batchBefore.slides,'One undo restores the complete batch');
    await page.getByRole('button',{name:'Redo',exact:true}).click();
    await waitForAsync(page,async()=>(await fetch('/api/editor/state').then(r=>r.json())).slides[0].elements.every(e=>e.text.startsWith('Atomic edit')));
    assert.deepEqual((await state()).slides,batchAfter.slides,'One redo restores the complete batch');
    await newBlank();
    const image=await page.evaluate(async()=>{
      const canvas=document.createElement('canvas');canvas.width=32;canvas.height=24;
      const ctx=canvas.getContext('2d');ctx.fillStyle='#ff0080';ctx.fillRect(0,0,16,24);ctx.fillStyle='#00aaff';ctx.fillRect(16,0,16,24);
      return canvas.toDataURL('image/png').split(',')[1];
    });
    await page.getByRole('button',{name:'Insert',exact:true}).click();
    const [imageChooser]=await Promise.all([page.waitForEvent('filechooser'),page.getByRole('button',{name:'Import image',exact:true}).click()]);
    await imageChooser.setFiles({name:'fixture.png',mimeType:'image/png',buffer:Buffer.from(image,'base64')});
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[0].elements?.some(e=>e.kind==='image')&&s.selected>=0;});
    await page.evaluate(()=>window.keynopeClipboardCommand('copy'));
    const imageBundle=JSON.parse((await readObjects()).slice('keynope-elements:'.length));
    assert.equal(Object.keys(imageBundle.assets).length,1);assert(!imageBundle.elements[0].path);
    await newBlank();
    await page.evaluate(()=>window.keynopeClipboardCommand('paste'));
    const withImage=await document();assert(withImage.content.includes('keynope-assets version=1'));
    assert(withImage.content.includes('keynope-asset:'));
    await page.waitForFunction(()=>{const img=document.querySelector('.keynope-modern-scene img');return !!img&&img.complete&&img.naturalWidth>0;});
    fs.mkdirSync('output/powerup',{recursive:true});
    await page.screenshot({path:'output/powerup/clipboard-image.png'});
    // Safari/native interoperability uses sanitized HTML rather than a
    // Chromium-specific custom format. It must retain the portable bundle.
    await page.evaluate(async()=>{
      const item=(await navigator.clipboard.read())[0];
      await navigator.clipboard.write([new ClipboardItem({'text/html':await item.getType('text/html'),'text/plain':new Blob(['[image]'],{type:'text/plain'})})]);
    });
    await newBlank();
    await page.evaluate(()=>window.keynopeClipboardCommand('paste'));
    assert((await state()).slides[0].elements.some(e=>e.kind==='image'),'HTML clipboard transport preserves image dependencies');
    await page.evaluate(()=>navigator.clipboard.writeText('A plain-text clipboard fallback'));
    await page.evaluate(()=>window.keynopeClipboardCommand('paste'));
    assert((await state()).slides[0].elements.some(e=>e.text==='A plain-text clipboard fallback'));
    const beforeEmpty=await state();
    await page.evaluate(()=>navigator.clipboard.writeText(''));
    await page.evaluate(()=>window.keynopeClipboardCommand('paste'));
    assert.deepEqual(await state(),beforeEmpty,'Empty system clipboard must not paste stale internal data');
    // Real system clipboard: merged rich cells own their payload, survive
    // destination ID collisions and stay directly editable after save/Open.
    const mergedData={version:1,rows:[{height:1},{height:1},{height:1}],columns:[{width:2},{width:3},{width:2}],
      style:{header:true,banded:true,headerFill:'#163c62',bandFill:'#243044',cell:{fontId:'mono',fontSize:48,color:'#ffffff',padding:8,align:'center',vertical:'middle',borderColor:'#55aaff'}},
      cells:[[{colSpan:3,runs:[{text:'Workshop ',bold:true},{text:'plan',fontId:'c64',fontSize:64,color:'#55aaff'}]},{hidden:true},{hidden:true}],
        [{rowSpan:2,text:'Shared owner'},{runs:[{text:'Discuss ',fontSize:50},{text:'together',italic:true,underline:true,color:'#ff0055'}]},{text:'10 min'}],
        [{hidden:true},{text:'Build'},{text:'20 min'}]]};
    const tableDeck=data=>[
      '<!-- keynope width=245 height=56 -->',
      '<!-- keynope-objects version=1 base64:'+zlib.gzipSync(JSON.stringify({version:1,tables:{'portable-data':data}})).toString('base64')+' -->',
      '<!-- keynope-slide-id=portable-slide -->',
      '<!-- object-id=portable-data element-id=portable-table top=8 left=24 width=180 height=32 -->','[table]'
    ].join('\n\n');
    const openTable=async(data,name)=>{
      const [picker]=await Promise.all([page.waitForEvent('filechooser'),page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click()]);
      await picker.setFiles({name,mimeType:'text/markdown',buffer:Buffer.from(tableDeck(data))});
      await waitForAsync(page,async expected=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[0].id==='portable-slide'&&s.slides[0].elements?.length===1&&document.querySelector('#stage .keynope-scene-table')?.textContent.includes(expected);},data.cells[0][0].text||'Workshop');
    };
    await openTable(mergedData,'merged-source.md');
    const tableWorkspaceRevision=await page.evaluate(()=>{
      window.clipboardTableOwner=document.querySelector('#stage .keynope-scene-table');
      return window.keynopeWorkspaceAppliedRevision;
    });
    await page.locator('.keynope-canvas-overlay .keynope-canvas-element').first().click();
    // Selection is a view-only command: it updates the inspector/hit targets,
    // not the content workspace revision. Waiting for a content refresh here
    // incorrectly requires the redraw the retained-scene path avoids.
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectedTable?.objectId==='portable-table'&&document.querySelector('.keynope-canvas-overlay .keynope-canvas-element.active')?.dataset.elementId==='portable-table';});
    assert.equal(await page.evaluate(()=>window.keynopeWorkspaceAppliedRevision),tableWorkspaceRevision,'Selection must not refresh content');
    assert.ok(await page.evaluate(()=>window.clipboardTableOwner===document.querySelector('#stage .keynope-scene-table')),'Selection must retain the rendered table');
    await page.evaluate(()=>window.keynopeClipboardCommand('copy'));
    const mergedBundle=JSON.parse((await readObjects()).slice('keynope-elements:'.length));
    assert.deepEqual(mergedBundle.objects.tables['portable-data'],mergedData);
    await page.screenshot({path:'output/powerup/clipboard-merged-source.png'});
    const collisionData={version:1,rows:[{height:1}],columns:[{width:1}],cells:[[{text:'Destination preserved'}]],style:{cell:{}}};
    await openTable(collisionData,'merged-destination.md');
    const destinationBefore=await state();
    await page.evaluate(()=>window.keynopeClipboardCommand('paste'));
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[0].elements?.length===2&&s.selectedTable?.data.cells[0][0].colSpan===3;});
    const mergedPaste=await state(),mergedElement=mergedPaste.slides[0].elements.find(e=>e.id!=='portable-table');
    assert.notEqual(mergedElement.objectId,'portable-data');assert.deepEqual(mergedPaste.selectedTable.data,mergedData);
    await page.getByRole('button',{name:'Undo',exact:true}).click();
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[0].elements?.length===1;});
    assert.deepEqual((await state()).slides,destinationBefore.slides);
    await page.getByRole('button',{name:'Redo',exact:true}).click();
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[0].elements?.length===2;});
    const mergedSaved=await document(),mergedArchive=JSON.parse(zlib.gunzipSync(Buffer.from(mergedSaved.content.match(/keynope-objects version=1 base64:([A-Za-z0-9+/=]+)/)[1],'base64')));
    assert.deepEqual(mergedArchive.tables['portable-data'],collisionData);assert.deepEqual(mergedArchive.tables[mergedElement.objectId],mergedData);
    await page.evaluate(async id=>{const s=await fetch('/api/editor/state').then(r=>r.json());const r=await fetch('/api/editor/action',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'select-element',element:s.slides[0].elements.findIndex(e=>e.id===id)})});if(!r.ok)throw Error(await r.text());},mergedElement.id);
    await page.waitForFunction(()=>document.querySelector('.keynope-canvas-element.active'));
    await page.locator('.keynope-canvas-element.active').dblclick();
    const mergedDialog=page.getByRole('dialog',{name:'Edit table cells',exact:true});await mergedDialog.waitFor();
    await mergedDialog.locator('[contenteditable=true]').press('Escape');
    assert.equal(await mergedDialog.getByRole('gridcell').count(),6,'Covered merged cells must not become independent editing targets');
    await mergedDialog.getByRole('gridcell',{name:'Cell B3',exact:true}).dblclick();
    await mergedDialog.getByRole('textbox',{name:'Edit cell B3',exact:true}).fill('Build a copied table');
    await mergedDialog.getByRole('button',{name:'Done',exact:true}).click();
    await page.waitForFunction(()=>[...document.querySelectorAll('#stage .keynope-scene-table')].some(node=>node.textContent.includes('Build a copied table')));
    assert.deepEqual(JSON.parse((await readObjects()).slice('keynope-elements:'.length)).objects.tables['portable-data'],mergedData,'Editing the pasted table must not mutate system clipboard');
    await page.screenshot({path:'output/powerup/clipboard-merged-edit.png'});
    const publicOutput=await page.evaluate(async()=>{
      const scene=await fetch('/api/editor/scene?slide=0').then(r=>r.json()),participant=await fetch('/api/editor/participant-page?slide=0&format=rendered').then(r=>r.json());
      const mdResponse=await fetch('/api/editor/participant-page?slide=0');if(!mdResponse.ok)throw Error(await mdResponse.text());
      const htmlResponse=await fetch('/api/editor/export-document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'portable-table.html'})});if(!htmlResponse.ok)throw Error(await htmlResponse.text());
      return {local:scene.objects.filter(o=>o.kind==='table').map(o=>o.table),public:participant.rendered.pages[0].scene.objects.filter(o=>o.kind==='table').map(o=>o.table),markdown:(await mdResponse.json()).markdown,html:(await htmlResponse.json()).content};
    });
    assert.deepEqual(publicOutput.public,publicOutput.local,'Participant merged tables use the exact local scene');
    const publicArchive=JSON.parse(zlib.gunzipSync(Buffer.from(publicOutput.markdown.match(/keynope-objects version=1 base64:([A-Za-z0-9+/=]+)/)[1],'base64')));
    const publicEditedCell=publicArchive.tables[mergedElement.objectId].cells[2][1];
    assert.equal(publicEditedCell.text||(publicEditedCell.runs||[]).map(r=>r.text).join(''),'Build a copied table','Legacy participant Markdown retains editable table data');
    const outputPage=await context.newPage();
    await outputPage.route('**/portable-table-output.html',route=>route.fulfill({status:200,contentType:'text/html',body:publicOutput.html}));
    await outputPage.goto('http://127.0.0.1:'+server.address().port+'/portable-table-output.html');
    await outputPage.waitForFunction(()=>[...document.querySelectorAll('#stage .keynope-scene-table')].some(node=>node.textContent.includes('Build a copied table')));
    const outputCells=await outputPage.locator('#stage .keynope-scene-table').last().evaluate(table=>[...table.querySelectorAll('td,th')].map(cell=>({text:cell.textContent,rowSpan:cell.rowSpan,colSpan:cell.colSpan,fill:cell.style.background,border:cell.style.borderColor})));
    const localCells=await page.locator('#stage .keynope-scene-table').last().evaluate(table=>[...table.querySelectorAll('td,th')].map(cell=>({text:cell.textContent,rowSpan:cell.rowSpan,colSpan:cell.colSpan,fill:cell.style.background,border:cell.style.borderColor})));
    assert.deepEqual(outputCells,localCells,'Standalone HTML uses the same merged cells, fills and borders');
    const borderSnapshot=table=>({
      cells:[...table.querySelectorAll('td,th')].map(cell=>getComputedStyle(cell).borderLeftWidth),
      viewBox:table.querySelector('.keynope-table-borders')?.getAttribute('viewBox'),
      lines:[...table.querySelectorAll('.keynope-table-borders line')].map(line=>Object.fromEntries(
        ['data-table-border','x1','y1','x2','y2','stroke','stroke-width','stroke-linecap'].map(name=>[name,line.getAttribute(name)])
      ))
    });
    const outputBorders=await outputPage.locator('#stage .keynope-scene-table').last().evaluate(borderSnapshot);
    const localBorders=await page.locator('#stage .keynope-scene-table').last().evaluate(borderSnapshot);
    assert(outputBorders.lines.length>0,'Standalone HTML must render the shared-border layer');
    assert(outputBorders.cells.every(width=>width==='0px'),'Shared borders replace doubled per-cell CSS seams');
    assert.deepEqual(outputBorders,localBorders,'Standalone HTML uses exactly the same shared border keys, geometry and paint');
    await outputPage.screenshot({path:'output/powerup/table-portable-html.png'});await outputPage.close();
    const portableSaved=await document();
    const [portableReopen]=await Promise.all([page.waitForEvent('filechooser'),page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click()]);
    await portableReopen.setFiles({name:'portable-saved.md',mimeType:'text/markdown',buffer:Buffer.from(portableSaved.content)});
    await waitForAsync(page,async id=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[0].elements?.length===2&&s.slides[0].elements.some(e=>e.id===id)&&document.querySelector('#stage [data-object-id="'+id+'"]')?.textContent.includes('Build a copied table');},mergedElement.id);
    await page.evaluate(async id=>{const s=await fetch('/api/editor/state').then(r=>r.json());await fetch('/api/editor/action',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'select-element',element:s.slides[0].elements.findIndex(e=>e.id===id)})});},mergedElement.id);
    await waitForAsync(page,async id=>(await fetch('/api/editor/state').then(r=>r.json())).selectedTable?.objectId===id,mergedElement.id);
    const reopenedMerged=(await state()).selectedTable.data;
    assert.equal(reopenedMerged.cells[0][0].colSpan,3);assert.equal(reopenedMerged.cells[1][0].rowSpan,2);assert.equal(reopenedMerged.cells[0][0].runs[1].fontId,'c64');
    // Read a portable authored diagram/timeline through the production parser.
    const objects={version:1,builds:{'browser-diagram':{version:1,steps:[
      {targetIds:['left-shape','right-shape'],effect:'appear'},
      {targetIds:['joining-line'],effect:'fade'}
    ]}}};
    const diagram=[
      '<!-- keynope width=245 height=56 -->',
      '<!-- keynope-objects version=1 base64:'+zlib.gzipSync(JSON.stringify(objects)).toString('base64')+' -->',
      '<!-- keynope-slide-id=browser-diagram -->',
      '<!-- top=8 left_pct=0.12 width=45 height=12 fg=#ff0080 shape=square element-id=left-shape -->','[shape:square]',
      '<!-- top=8 left_pct=0.64 width=45 height=12 fg=#00aaff shape=square element-id=right-shape -->','[shape:square]',
      '<!-- element-id=joining-line connector-from=left-shape connector-from-side=right connector-to=right-shape connector-to-side=left connector-width=1 -->','[connector]'
    ].join('\n\n');
    const [diagramChooser]=await Promise.all([page.waitForEvent('filechooser'),page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click()]);
    await diagramChooser.setFiles({name:'references.md',mimeType:'text/markdown',buffer:Buffer.from(diagram)});
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[0].id==='browser-diagram'&&s.slides[0].elements?.length===3;});
    const command=async body=>page.evaluate(async body=>{
      const r=await fetch('/api/editor/action',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});return r.status;
    },body);
    const readArchive=async()=>{
      const saved=await document(),match=saved.content.match(/keynope-objects version=1 base64:([A-Za-z0-9+/=]+)/);
      assert(match,'Structured archive is preserved when saving');return JSON.parse(zlib.gunzipSync(Buffer.from(match[1],'base64')));
    };
    const parsed=await state();
    assert.equal(await command({action:'update-slide',slideData:{...parsed.slides[0],id:undefined,bg:'#222244'}}),200);
    assert.equal((await state()).slides[0].id,'browser-diagram','Settings must not erase slide identity');
    assert.deepEqual((await readArchive()).builds['browser-diagram'].steps,objects.builds['browser-diagram'].steps);
    const beforeBadReference=await state(),line=beforeBadReference.slides[0].elements.find(e=>e.kind==='connector');
    assert(line,'The authored connector is parsed');
    const badQuery=new URLSearchParams(line.query);badQuery.set('connector-to','unknown-shape');
    assert.equal(await command({action:'update-element',element:beforeBadReference.slides[0].elements.indexOf(line),elementData:{...line,query:badQuery.toString()}}),400);
    assert.deepEqual(await state(),beforeBadReference,'Invalid connector edit preserves state/history');
    await page.screenshot({path:'output/powerup/reference-diagram.png'});
    const leftIndex=beforeBadReference.slides[0].elements.findIndex(e=>e.id==='left-shape');
    assert.equal(await command({action:'delete-element',element:leftIndex}),200);
    const afterDelete=await state();
    assert.deepEqual(afterDelete.slides[0].elements.map(e=>e.id),['right-shape'],'Endpoint deletion cleans its connector');
    assert.deepEqual((await readArchive()).builds['browser-diagram'].steps,[{targetIds:['right-shape'],effect:'appear'}]);
    assert.equal(await command({action:'undo'}),200);
    assert.deepEqual((await state()).slides,beforeBadReference.slides,'One undo restores the diagram');
    assert.deepEqual((await readArchive()).builds['browser-diagram'].steps,objects.builds['browser-diagram'].steps,'One undo restores timeline references');
    assert.equal(await command({action:'redo'}),200);
    assert.deepEqual((await state()).slides,afterDelete.slides,'One redo removes the complete dependent graph');
    // Prepared WASM paste must carry the destination theme, not flatten linked
    // chart/table styles as if the destination were an unthemed legacy deck.
    await newBlank();
    const destinationTheme=(await state()).theme;
    destinationTheme.colors.accent[1]='#abcdef';destinationTheme.fonts.body.fontId='mono';
    assert.equal(await command({action:'set-deck-theme',themeData:destinationTheme}),200);
    const linkedBundle={version:1,elements:[
      {id:'linked-chart',kind:'chart',objectId:'linked-chart-data',query:'left=20&top=4&width=180&height=25'},
      {id:'linked-table',kind:'table',objectId:'linked-table-data',query:'left=20&top=35&width=180&height=15'}
    ],objects:{version:1,charts:{'linked-chart-data':{version:1,type:'column',categories:['One'],series:[{name:'Linked',values:[3],color:'#112233',colorToken:'accent-2'}]}},tables:{'linked-table-data':{version:1,rows:[{height:1}],columns:[{width:1}],cells:[[{runs:[{text:'Destination-linked words',color:'#112233',colorToken:'accent-2',fontRole:'body',fontId:'sans'}]}]]}}}};
    const linkedStatus=await page.evaluate(async clipboard=>{
      const response=await fetch('/api/editor/paste-clipboard',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'paste-objects',clipboard})});
      if(!response.ok)throw Error(await response.text());return response.status;
    },linkedBundle);
    assert.equal(linkedStatus,200);
    const linkedArchive=await readArchive(),linkedState=await state();
    const chartElement=linkedState.slides[0].elements.find(e=>e.kind==='chart'),tableElement=linkedState.slides[0].elements.find(e=>e.kind==='table');
    assert.equal(linkedArchive.charts[chartElement.objectId].series[0].colorToken,'accent-2');
    const linkedRun=linkedArchive.tables[tableElement.objectId].cells[0][0].runs[0];
    assert.equal(linkedRun.colorToken,'accent-2');assert.equal(linkedRun.fontRole,'body');
    const linkedScene=await page.evaluate(()=>fetch('/api/editor/scene?slide=0').then(r=>r.json()));
    assert.equal(linkedScene.objects.find(o=>o.kind==='chart').chart.seriesColors[0],'#abcdef');
    assert.equal(await command({action:'undo'}),200);assert.equal((await state()).slides[0].elements?.length||0,0);
    assert.equal(await command({action:'redo'}),200);assert.deepEqual(await readArchive(),linkedArchive);
    assert.deepEqual(errors,[]);
    console.log('PASS: browser clipboard, merged table portability/editing, audience/HTML parity, destination theme roles, stable-ID batches, reference cleanup, history and fallbacks');
  }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);server.close();process.exitCode=1;});
