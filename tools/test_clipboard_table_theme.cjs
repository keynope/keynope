// Production clipboard endpoint, paste/history/save and source/plain/themed rendering.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),output=process.env.KEYNOPE_CLIPBOARD_TABLE_EVIDENCE||'/tmp/keynope-clipboard-table-proof';
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));const browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
 try{
  const context=await browser.newContext({viewport:{width:1440,height:1000},reducedMotion:'reduce'});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());
  const page=await context.newPage(),errors=[];page.on('pageerror',error=>errors.push(error.message));page.on('dialog',dialog=>dialog.accept());
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const open=async(name,content)=>{const picker=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await picker).setFiles({name,mimeType:'text/markdown',buffer:Buffer.from(content)});await page.waitForFunction(name=>document.title==='Keynope — '+name||document.title==='Keynope — '+name+' *',name);await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));};
  await open('source.md',fs.readFileSync(path.join(output,'source.md')));
  const source=await page.evaluate(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json()),r=await fetch('/api/editor/clipboard',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({version:s.version,ids:['portable-table']})});if(!r.ok)throw Error(await r.text());return {bundle:await r.json(),table:structuredClone(deck.pages[pageIndex].scene.objects.find(o=>o.id==='portable-table').table)};});
  assert(source.bundle.tableThemeFallbacks['portable-table']);await page.evaluate(()=>document.fonts.ready);await page.screenshot({path:path.join(output,'source.png')});
  for(const kind of ['plain','themed']){
   await open(kind+'-target.md',fs.readFileSync(path.join(output,kind+'-target.md')));
   const result=await page.evaluate(async bundle=>{const state=()=>fetch('/api/editor/state').then(r=>r.json()),before=await state();await editorAction({action:'paste-objects',clipboard:bundle});const after=await state(),table=structuredClone(deck.pages[pageIndex].scene.objects.find(o=>o.kind==='table').table);await editorAction({action:'undo'});if(JSON.stringify((await state()).slides)!==JSON.stringify(before.slides))throw Error('Clipboard undo failed');await editorAction({action:'redo'});if(JSON.stringify((await state()).slides)!==JSON.stringify(after.slides))throw Error('Clipboard redo failed');const saved=await fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Table saved.md'})}).then(r=>r.json());return {table,content:saved.content};},source.bundle);
   if(kind==='plain')assert.deepEqual(result.table,source.table);else{assert.equal(result.table.cells[0].text.fontId,'sans');assert.notEqual(result.table.cells[0].fill,source.table.cells[0].fill);}
   await open(kind+'-reopened.md',result.content);const reopened=await page.evaluate(()=>structuredClone(deck.pages[pageIndex].scene.objects.find(o=>o.kind==='table').table));assert.deepEqual(reopened,result.table);await page.evaluate(()=>document.fonts.ready);await page.screenshot({path:path.join(output,kind+'.png')});
  }
  assert.deepEqual(errors,[]);console.log('PASS: table source appearance in plain deck, theme adoption, Undo/Redo and Markdown reopening; merged cells, borders and typography retained.');
 }finally{await browser.close();server.close();}
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
