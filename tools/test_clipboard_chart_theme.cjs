// Actual clipboard endpoint, production WASM, source/plain/themed chart rendering.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),output=process.env.KEYNOPE_CLIPBOARD_CHART_EVIDENCE||'/tmp/keynope-clipboard-chart-proof';
const rendered=chart=>{const copy=structuredClone(chart);delete copy.data;return copy;};
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));const browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
 try{
  const context=await browser.newContext({viewport:{width:1440,height:1000},reducedMotion:'reduce'});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());
  const page=await context.newPage(),errors=[];page.on('pageerror',error=>errors.push(error.message));page.on('dialog',dialog=>dialog.accept());
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const open=async(name,content)=>{const picker=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await picker).setFiles({name,mimeType:'text/markdown',buffer:Buffer.from(content)});await page.waitForFunction(name=>document.title==='Keynope — '+name||document.title==='Keynope — '+name+' *',name);await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));};
  for(const kind of ['column-stacked','pie']){
   await open(kind+'-source.md',fs.readFileSync(path.join(output,kind+'-source.md')));
   const source=await page.evaluate(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json()),r=await fetch('/api/editor/clipboard',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({version:s.version,ids:['chart-theme']})});if(!r.ok)throw Error(await r.text());return {bundle:await r.json(),chart:structuredClone(deck.pages[pageIndex].scene.objects[0].chart)};});
   assert(source.bundle.chartThemeFallbacks['chart-theme']);
   for(const target of ['plain','themed']){
    await open(kind+'-'+target+'.md',fs.readFileSync(path.join(output,kind+'-'+target+'.md')));
    const result=await page.evaluate(async bundle=>{const state=()=>fetch('/api/editor/state').then(r=>r.json()),before=await state();await editorAction({action:'paste-objects',clipboard:bundle});const after=await state(),chart=structuredClone(deck.pages[pageIndex].scene.objects[0].chart);await editorAction({action:'undo'});if(JSON.stringify((await state()).slides)!==JSON.stringify(before.slides))throw Error('Chart undo failed');await editorAction({action:'redo'});if(JSON.stringify((await state()).slides)!==JSON.stringify(after.slides))throw Error('Chart redo failed');const saved=await fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Chart saved.md'})}).then(r=>r.json());return {chart,content:saved.content};},source.bundle);
    if(target==='plain')assert.deepEqual(rendered(result.chart),rendered(source.chart));else{assert.equal(result.chart.labels[0].text.fontId,'sans');const colors=kind==='pie'?result.chart.categoryColors:result.chart.seriesColors;assert.equal(colors[0],kind==='pie'?'#2255dd':'#ff8822');}
    await open(kind+'-'+target+'-reopened.md',result.content);const reopened=await page.evaluate(()=>structuredClone(deck.pages[pageIndex].scene.objects[0].chart));assert.deepEqual(reopened,result.chart);await page.evaluate(()=>document.fonts.ready);await page.screenshot({path:path.join(output,kind+'-'+target+'.png')});
   }
   await open(kind+'-source.md',fs.readFileSync(path.join(output,kind+'-source.md')));await page.evaluate(()=>editorAction({action:'select-element',element:0}));await page.getByRole('button',{name:'Edit chart',exact:true}).click();const dialog=page.getByRole('dialog',{name:'Edit chart',exact:true}),label=kind==='pie'?'Slice 1 label colour':'Series 1 label colour';
   await dialog.getByRole('button',{name:label,exact:true}).click();const picker=page.locator('.keynope-colour-picker');await picker.getByRole('textbox',{name:'HTML color',exact:true}).fill('#ffcc22');await picker.getByRole('button',{name:'Apply',exact:true}).click();await dialog.getByRole('button',{name:'Apply',exact:true}).click();await dialog.waitFor({state:'detached'});const styled=await page.evaluate(()=>structuredClone(deck.pages[pageIndex].scene.objects[0].chart));assert(styled.labels.some(label=>label.paint.color==='#ffcc22'));await page.screenshot({path:path.join(output,kind+'-custom-label.png')});
  }
  assert.deepEqual(errors,[]);console.log('PASS: chart palette/font/grid/contrast preserved in plain deck; destination theme adoption, label-colour editing, Undo/Redo and Markdown reopen.');
 }finally{await browser.close();server.close();}
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
