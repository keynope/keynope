// Actual WASM label appearance across object clipboard and document boundaries.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),output=process.env.KEYNOPE_CLIPBOARD_LABEL_EVIDENCE||'/tmp/keynope-clipboard-label-proof';
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));const browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
 try{
  const context=await browser.newContext({viewport:{width:1440,height:1000},reducedMotion:'reduce'});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());
  const page=await context.newPage(),errors=[],records=[];page.on('pageerror',error=>errors.push(error.message));page.on('dialog',dialog=>dialog.accept());
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const open=async(name,content)=>{const picker=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await picker).setFiles({name,mimeType:'text/markdown',buffer:Buffer.from(content)});await page.waitForFunction(name=>document.title==='Keynope — '+name||document.title==='Keynope — '+name+' *',name);await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));};
  await open('label-source.md',fs.readFileSync(path.join(output,'source.md')));
  const source=await page.evaluate(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json()),r=await fetch('/api/editor/clipboard',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({version:s.version,ids:['label-a']})});if(!r.ok)throw Error(await r.text());return {bundle:await r.json(),object:structuredClone(deck.pages[pageIndex].scene.objects.find(o=>o.id==='label-a'))};});
  assert(source.bundle.labelThemeFallbacks['label-a']);
  for(const kind of ['plain','themed']){
   await open(kind+'.md',fs.readFileSync(path.join(output,kind+'.md')));
   const result=await page.evaluate(async bundle=>{const state=()=>fetch('/api/editor/state').then(r=>r.json()),before=await state();await editorAction({action:'paste-objects',clipboard:bundle});const after=await state(),object=structuredClone(deck.pages[pageIndex].scene.objects[0]);await document.fonts.ready;await new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)));
    const copy=structuredClone(object);await editorAction({action:'undo'});if(JSON.stringify((await state()).slides)!==JSON.stringify(before.slides))throw Error('Label clipboard undo failed');await editorAction({action:'redo'});if(JSON.stringify((await state()).slides)!==JSON.stringify(after.slides))throw Error('Label clipboard redo failed');
    const saved=await fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Labels saved.md'})}).then(r=>r.json());return {object:copy,content:saved.content};
   },source.bundle);
   assert.deepEqual(result.object.bounds,source.object.bounds);assert.equal(result.object.paint.color,source.object.paint.color);
   if(kind==='plain'){assert.deepEqual(result.object.paint,source.object.paint);assert.deepEqual(result.object.label,source.object.label);assert.deepEqual(result.object.labelPaint,source.object.labelPaint);}else{assert.equal(result.object.label.fontId,'sans');assert.equal(result.object.label.size,52);assert.equal(result.object.labelPaint.color,'#33ccaa');}
   await open(kind+'-reopened.md',result.content);const reopened=await page.evaluate(()=>structuredClone(deck.pages[pageIndex].scene.objects[0]));assert.deepEqual(reopened.label,result.object.label);assert.deepEqual(reopened.labelPaint,result.object.labelPaint);await page.evaluate(()=>document.fonts.ready);await page.screenshot({path:path.join(output,kind+'.png')});records.push({kind,font:result.object.label.fontId,size:result.object.label.size,colour:result.object.labelPaint.color});
  }
  assert.deepEqual(errors,[]);fs.writeFileSync(path.join(output,'report.json'),JSON.stringify({browser:browser.version(),records,errors},null,2));console.log('PASS: label typography/colour preserve source in plain deck, adopt destination theme in themed deck; body/bounds, Undo/Redo and save/reopen retained.');
 }finally{await browser.close();server.close();}
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
