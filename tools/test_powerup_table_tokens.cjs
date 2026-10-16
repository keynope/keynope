const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),output='/tmp/keynope-table-token-proof';
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm','.json':'application/json'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));const browser=await playwright.chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
  try{
    const context=await browser.newContext({viewport:{width:1500,height:1000}});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());const page=await context.newPage(),errors=[];
    page.on('pageerror',e=>errors.push(e.message));page.on('dialog',async d=>{assert.equal(d.type(),'confirm');await d.accept();});
    await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
    const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles({name:'Table tokens.md',mimeType:'text/markdown',buffer:fs.readFileSync(output+'/tokens.md')});
    await page.waitForFunction(()=>deck.pages[pageIndex]?.scene?.objects.some(o=>o.id==='portable-table'));
    const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
    const s=await state(),i=s.slides[0].elements.findIndex(e=>e.id==='portable-table');await page.evaluate(({i,e})=>editorAction({action:'select-element',element:i,elementData:e}),{i,e:s.slides[0].elements[i]});
    const before=await state();await page.locator('.keynope-canvas-element.active').dblclick();const dialog=page.getByRole('dialog',{name:'Edit table cells',exact:true});await dialog.waitFor();
    const caret=dialog.locator('[contenteditable=true]');if(await caret.count())await caret.first().press('Escape');await dialog.getByRole('gridcell',{name:'Cell B2',exact:true}).click();
    const fill=dialog.getByRole('combobox',{name:'Canvas cell fill theme',exact:true}),text=dialog.getByRole('combobox',{name:'Canvas cell text theme',exact:true});
    await page.waitForFunction(()=>{const field=document.querySelector('select[aria-label="Canvas cell fill theme"]');return field?.value==='theme:accent-2'&&!field.disabled;});assert.equal(await fill.inputValue(),'theme:accent-2');assert.equal(await text.inputValue(),'theme:heading');
    await dialog.getByRole('gridcell',{name:'Cell C2',exact:true}).click({modifiers:['Shift']});
    await page.waitForFunction(()=>{const field=document.querySelector('select[aria-label="Canvas cell fill theme"]');return field?.value==='mixed'&&!field.disabled;});assert.equal(await fill.inputValue(),'mixed');assert.equal(await text.inputValue(),'mixed');
    await fill.selectOption('theme:accent-4');await page.waitForFunction(()=>document.querySelector('select[aria-label="Canvas cell fill theme"]')?.disabled===false);
    await text.selectOption('theme:accent-5');await page.waitForFunction(()=>document.querySelector('select[aria-label="Canvas cell text theme"]')?.disabled===false);
    await dialog.getByRole('combobox',{name:'Canvas cell border theme',exact:true}).selectOption('theme:line');await page.waitForFunction(()=>document.querySelector('select[aria-label="Canvas cell border theme"]')?.disabled===false);
    await dialog.screenshot({path:output+'/canvas-tokens.png'});
    await dialog.getByRole('button',{name:'Done',exact:true}).click();await dialog.waitFor({state:'detached'});
    const after=await state();assert.equal(after.version,before.version+1);assert.equal(after.selectedTable.data.cells[1][1].style.fill,'theme:accent-4');assert.equal(after.selectedTable.data.cells[1][1].style.color,'theme:accent-5');
    assert.equal(after.selectedTable.data.cells[1][2].style.fill,'theme:accent-4');assert.equal(after.selectedTable.data.cells[1][2].style.color,'theme:accent-5');
    assert.equal(after.selectedTable.data.cells[1][1].runs[1].color,'#ff0055');
    const theme=JSON.parse(JSON.stringify(after.theme));theme.colors.accent[3]='#003366';theme.colors.accent[4]='#ffdd66';
    await page.evaluate(theme=>editorAction({action:'set-deck-theme',themeData:theme}),theme);
    await page.waitForFunction(()=>{const t=deck.pages[pageIndex]?.scene?.objects.find(o=>o.id==='portable-table')?.table,c=t?.cells.find(c=>c.row===1&&c.column===1);return c?.fill==='#003366'&&c.paint.color==='#ffdd66';});
    await page.screenshot({path:output+'/palette-linked-table.png'});
    // Full draft editor preserves the role, supports changing it and cancelling.
    await page.evaluate(()=>editSelectedTable());const full=page.getByRole('dialog',{name:'Edit table',exact:true});await full.waitFor();
    await full.getByRole('textbox',{name:'Cell 2, 2',exact:true}).click();await page.waitForFunction(()=>{const field=document.querySelector('select[aria-label="Cell fill theme"]');return field?.value==='theme:accent-4'&&!field.disabled;});assert.equal(await full.getByRole('combobox',{name:'Cell fill theme',exact:true}).inputValue(),'theme:accent-4');
    await full.getByRole('combobox',{name:'Cell fill theme',exact:true}).selectOption('theme:surface');await full.getByRole('button',{name:'Cancel',exact:true}).click();await full.waitFor({state:'detached'});
    assert.equal((await state()).selectedTable.data.cells[1][1].style.fill,'theme:accent-4');
    const saved=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Table tokens.md'})}).then(r=>r.json()));fs.writeFileSync(output+'/tokens-saved.md',saved.content);
    assert.deepEqual(errors,[]);console.log('PASS: canvas/draft table roles, merged cell inheritance, one-command commit, custom rich-run preservation, palette update and Cancel. '+output);
  }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);process.exitCode=1;server.close();});
