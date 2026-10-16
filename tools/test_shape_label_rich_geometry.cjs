// Shared rich-label measurement and continuous one-axis content growth.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http'),{execFileSync}=require('node:child_process');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright'),{waitForAsync}=require('./wait_for_async.cjs');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),engine=process.env.TEST_BROWSER||'webkit',proof='/tmp/keynope-rich-label-'+engine;
execFileSync('go',['test','.','-run','^TestShapeLabelRichBrowserFixture$','-count=1'],{env:{...process.env,GOCACHE:process.env.GOCACHE||'/tmp/keynope-go-cache',KEYNOPE_RICH_LABEL_PROOF:proof},stdio:'pipe'});
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const browser=await pw[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
 try{
  const page=await browser.newPage({viewport:{width:1600,height:1000}}),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());await page.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const picker=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await picker).setFiles(path.join(proof,'labels.md'));
  await waitForAsync(page,async()=>(await fetch('/api/editor/state').then(r=>r.json())).path.endsWith('labels.md'));await page.evaluate(()=>KeynopeScene.ensureFonts());
  const objects=await page.evaluate(()=>deck.pages[pageIndex].scene.objects);assert.equal(objects.length,3);
  const mixed=objects.find(o=>o.id==='rich-0');assert.equal(mixed.label.runs[0].fontSize,160);assert(mixed.bounds.width>10.125*1920/245);
  const overflow=await page.locator('#stage .keynope-scene-label .keynope-shaped-text').evaluateAll(nodes=>nodes.map(n=>({width:n.parentElement.parentElement.clientWidth,scrollWidth:n.scrollWidth,height:n.parentElement.parentElement.clientHeight,scrollHeight:n.scrollHeight})));
  assert.equal(overflow.length,3);assert(overflow.every(o=>o.scrollWidth<=o.width+1&&o.scrollHeight<=o.height+1),JSON.stringify(overflow));
  await page.locator('#stage').screenshot({path:path.join(proof,'rich-labels.png')});
  const before=await page.evaluate(()=>deck.pages[pageIndex].scene.objects.find(o=>o.id==='rich-2').bounds);
  await page.locator('.keynope-canvas-element[data-element-id="rich-2"]').dblclick();await page.getByRole('textbox',{name:'Edit slide text',exact:true}).fill('A considerably wider line now\nLine two');await page.getByRole('button',{name:'Apply text',exact:true}).click();
  await waitForAsync(page,async()=>deck.pages[pageIndex].scene.objects.find(o=>o.id==='rich-2').label.runs.map(r=>r.text).join('')==='A considerably wider line now\nLine two');
  const after=await page.evaluate(()=>deck.pages[pageIndex].scene.objects.find(o=>o.id==='rich-2').bounds);
  assert(after.width>before.width);assert(Math.abs(after.x-before.x)<1e-7);assert(Math.abs(after.y-before.y)<1e-7);assert.equal(after.height,before.height);
  await page.evaluate(()=>editorAction({action:'undo'}));await waitForAsync(page,async()=>deck.pages[pageIndex].scene.objects.find(o=>o.id==='rich-2').label.runs.map(r=>r.text).join('')==='Line one\nLine two');
  assert.deepEqual(await page.evaluate(()=>deck.pages[pageIndex].scene.objects.find(o=>o.id==='rich-2').bounds),before);
  await page.evaluate(()=>editorAction({action:'redo'}));assert.deepEqual(await page.evaluate(()=>deck.pages[pageIndex].scene.objects.find(o=>o.id==='rich-2').bounds),after);
  assert.deepEqual(errors,[]);console.log('PASS '+engine+': mixed run sizes/fonts, tabs/trailing lines, paragraph padding, actual label editing, continuous origin and one-axis growth, Undo/Redo.');
 }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);server.close();process.exitCode=1;});
