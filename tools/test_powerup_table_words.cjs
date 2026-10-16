const {waitForAsync}=require("./wait_for_async.cjs");
// Owned table, actual WASM/caret/colour picker/history and exported HTML.
// Off-origin HTTP is blocked. No working-app activation or cloud sessions.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-table-word-site'),output=process.env.KEYNOPE_TABLE_WORD_EVIDENCE||'/tmp/keynope-table-word-proof';let exported='';
const server=http.createServer((req,res)=>{
  let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';
  if(name==='/words.html'){res.setHeader('Content-Type','text/html');return res.end(exported);}
  const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}
  res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));
});
(async()=>{
  await new Promise(r=>server.listen(0,'127.0.0.1',r));const browser=await playwright.chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
  try{
    const context=await browser.newContext({viewport:{width:1600,height:1100}});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
    const page=await context.newPage(),errors=[];page.setDefaultTimeout(25000);page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
    const origin='http://127.0.0.1:'+server.address().port;
    await page.goto(origin+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
    const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
    const open=async(content,name)=>{const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles({name,mimeType:'text/markdown',buffer:Buffer.from(content)});await waitForAsync(page,async name=>(await fetch('/api/editor/state').then(r=>r.json())).path.endsWith(name),name);};
    await open(fs.readFileSync(output+'/table-words.md'),'Table words.md');await page.waitForFunction(()=>deck.pages[0]?.scene?.objects.find(o=>o.id==='table-word')?.table.cells[1].text.runs[0].colorToken==='accent-1');
    await page.evaluate(()=>editorAction({action:'select-element',element:0}));const before=await state();
    await page.locator('.keynope-canvas-element[data-element-id="table-word"]').dblclick();const dialog=page.getByRole('dialog',{name:'Edit table cells',exact:true});await dialog.waitFor();
    if(await dialog.locator('[contenteditable=true]').count())await dialog.locator('[contenteditable=true]').press('Escape');
    await dialog.getByRole('gridcell',{name:'Cell B1',exact:true}).dblclick();const caret=dialog.getByLabel('Edit cell B1',{exact:true});await caret.waitFor();
    const role=dialog.getByRole('combobox',{name:'Word colour role',exact:true});
    const select=async(a,b)=>{await page.evaluate(([a,b])=>{const root=document.querySelector('[aria-label="Edit cell B1"]');root.focus();const walker=document.createTreeWalker(root,NodeFilter.SHOW_TEXT),nodes=[];let node,offset=0;while(node=walker.nextNode()){nodes.push({node,start:offset,end:offset+node.length});offset+=node.length;}const point=x=>{const n=nodes.find(n=>x<=n.end)||nodes.at(-1);return[n.node,x-n.start];};document.getSelection().setBaseAndExtent(...point(a),...point(b));},[a,b]);await page.waitForFunction(([a,b])=>{const f=document.querySelector('fieldset[aria-label="Word formatting"]');return f?.dataset.rangeStart===String(a)&&f.dataset.rangeEnd===String(b)&&f.getAttribute('aria-busy')==='false';},[a,b]);};
    const ink=()=>page.evaluate(()=>getComputedStyle(document.getSelection().focusNode.parentElement).color);
    await select(0,4);await role.waitFor();assert.equal(await role.inputValue(),'theme:accent-1');assert.equal(await ink(),'rgb('+[1,3,5].map(i=>parseInt(before.theme.colors.accent[0].slice(i,i+2),16)).join(', ')+')');
    await role.selectOption('theme:accent-2');await select(0,4);assert.equal(await role.inputValue(),'theme:accent-2');assert.equal((await state()).version,before.version);
    await caret.press('Meta+z');await select(0,4);assert.equal(await role.inputValue(),'theme:accent-1');await caret.press('Meta+y');await select(0,4);assert.equal(await role.inputValue(),'theme:accent-2');
    await select(0,10);assert.equal(await role.inputValue(),'mixed','linked and fixed words collapsed to a single role');
    await select(5,10);assert.equal(await role.inputValue(),'custom');
    await role.selectOption('theme:accent-2');await select(5,10);assert.equal(await role.inputValue(),'theme:accent-2');
    await dialog.getByRole('button',{name:'Canvas cell text colour',exact:true}).click();const picker=page.getByRole('dialog',{name:'Pick color',exact:true});await picker.getByRole('textbox',{name:'HTML color',exact:true}).fill('#00ff55');await picker.getByRole('button',{name:'Apply',exact:true}).click();
    await select(5,10);assert.equal(await role.inputValue(),'custom');assert.equal(await ink(),'rgb(0, 255, 85)');
    await select(0,4);await role.selectOption('');await select(0,4);assert.equal(await role.inputValue(),'');await caret.press('Meta+z');await select(0,4);assert.equal(await role.inputValue(),'theme:accent-2');
    await dialog.screenshot({path:output+'/table-word-controls.png'});await dialog.getByRole('button',{name:'Done',exact:true}).click();await dialog.waitFor({state:'detached'});
    const committed=await state();assert.equal(committed.version,before.version+1);const data=committed.selectedTable.data;
    assert.equal(data.cells[0][1].runs[0].colorToken,'accent-2');assert.deepEqual(data.cells[0][1].runs[0].future,{keep:true});assert.equal(data.cells[0][1].runs.find(r=>r.text.includes('Fixed')).color,'#00ff55');assert.equal(data.cells[0][1].runs.find(r=>r.text.includes('Fixed')).colorToken||'','');
    assert.deepEqual(data.cells[0][0],before.selectedTable.data.cells[0][0]);assert.deepEqual(data.style,before.selectedTable.data.style);assert.deepEqual(data.rows,before.selectedTable.data.rows);assert.deepEqual(data.columns,before.selectedTable.data.columns);
    await page.getByRole('button',{name:'Undo',exact:true}).click();await waitForAsync(page,async()=>(await fetch('/api/editor/state').then(r=>r.json())).selectedTable?.data.cells[0][1].runs[0].colorToken==='accent-1');assert.deepEqual((await state()).selectedTable.data,before.selectedTable.data);
    await page.getByRole('button',{name:'Redo',exact:true}).click();await waitForAsync(page,async()=>(await fetch('/api/editor/state').then(r=>r.json())).selectedTable?.data.cells[0][1].runs[0].colorToken==='accent-2');
    await page.evaluate(()=>{window.__tableWordOwner=document.querySelector('#stage [data-object-id="table-word"]');window.__tableWordCell=document.querySelector('#stage [data-table-cell="0:1"]');});
    const theme=structuredClone((await state()).theme);theme.colors.accent[1]='#aa0055';await page.evaluate(theme=>editorAction({action:'set-deck-theme',themeData:theme}),theme);
    await page.waitForFunction(()=>deck.pages[0].scene.objects[0].table.cells[1].text.runs[0].color==='#aa0055');assert.equal(await page.evaluate(()=>window.__tableWordOwner===document.querySelector('#stage [data-object-id="table-word"]')&&window.__tableWordCell===document.querySelector('#stage [data-table-cell="0:1"]')),true,'palette remounted table/cell');assert.deepEqual((await state()).selectedTable.data,data);
    const saved=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Saved table words.md'})}).then(r=>r.json()).then(r=>r.content));await open(saved,'Saved table words.md');await page.waitForFunction(()=>deck.pages[0]?.scene?.objects[0].table.cells[1].text.runs[0].color==='#aa0055');
    exported=await page.evaluate(()=>fetch('/api/editor/export-document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Table words.html'})}).then(r=>r.json()).then(r=>r.content));const external=await context.newPage();external.on('pageerror',e=>errors.push(e.message));await external.goto(origin+'/words.html');await external.waitForFunction(()=>document.querySelector('.keynope-modern-presentation')?.style.visibility==='visible');
    const words=await external.evaluate(()=>deck.pages[0].scene.objects[0].table.cells[1].text.runs);assert.equal(words[0].color,'#aa0055');assert.equal(words.find(r=>r.text.includes('Fixed')).color,'#00ff55');await external.screenshot({path:output+'/table-word-export.png'});await external.close();
    assert.deepEqual(errors,[]);console.log('PASS: table word role/RGB/mixed/Inherit, local/document Undo/Redo, one commit, unchanged cell/default/track scope, future metadata, palette consumer and retained table/cell, save/reopen/HTML. '+output);
  }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);process.exitCode=1;server.close();});
