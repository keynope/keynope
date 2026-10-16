// Production WASM editor + shared renderer proof corpus, no external requests.
const fs=require('node:fs'),path=require('node:path'),http=require('node:http'),assert=require('node:assert/strict');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright'),engine=process.env.TEST_BROWSER||'chromium';
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),output=process.env.PROOF_OUTPUT||'/tmp/keynope-corpus-render-proof';
const corpus=path.resolve('testdata/powerup'),manifest=JSON.parse(fs.readFileSync(path.join(corpus,'manifest.json')));
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 fs.mkdirSync(output,{recursive:true});await new Promise(r=>server.listen(0,'127.0.0.1',r));
 const browser=await playwright[engine].launch({headless:true,...(engine==='webkit'?{}:{executablePath:process.env.CHROME_PATH})});
 try {
  const context=await browser.newContext({viewport:manifest.viewport,deviceScaleFactor:manifest.deviceScaleFactor});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
  const page=await context.newPage(),errors=[],records=[],failures=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  await page.evaluate(()=>{const init=keynopeWasmInit;window.proofLoads=0;window.keynopeWasmInit=(...args)=>{const raw=init(...args);if(JSON.parse(raw).status===200)proofLoads++;return raw;};});
  for(const fixture of manifest.decks) {
   const expected=await page.evaluate(()=>proofLoads+1),chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles({name:fixture.file,mimeType:'text/markdown',buffer:fs.readFileSync(path.join(corpus,fixture.file))});
   await page.waitForFunction(n=>proofLoads===n,expected);
   assert.equal(await page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()).then(s=>s.slides.length)),fixture.pages);
   for(let slide=0;slide<fixture.pages;slide++) {
    await page.evaluate(async slide=>{await editorAction({action:'select-slide',slide});},slide);
    await page.waitForFunction(slide=>deck.pages[pageIndex]?.slide===slide,slide);
    const name=fixture.file.replace('.md','')+'-'+String(slide+1).padStart(2,'0');
    const record=await page.evaluate(async({slide,time})=>{
     const response=await fetch('/api/editor/scene?slide='+slide);if(!response.ok)throw Error(await response.text());const scene=await response.json();
     if(!scene.objects.length)throw Error('Empty production scene');
     const host=document.createElement('div');host.id='proof-canvas';host.style.cssText='position:fixed;inset:0;z-index:100000;width:100vw;height:100vh;background:#111;';document.body.append(host);
     const view=KeynopeScene.mount(host,scene);window.proofView=view;
     const scale=Math.min(host.clientWidth/scene.width,host.clientHeight/scene.height);Object.assign(view.element.style,{transformOrigin:'0 0',transform:'scale('+scale+')',left:(host.clientWidth-scene.width*scale)/2+'px',top:(host.clientHeight-scene.height*scale)/2+'px'});
     await document.fonts.ready;await view.snapshotAt(time);
     return {slide,objects:scene.objects.length,diagnostics:view.diagnostics,fonts:document.fonts.status,routes:scene.objects.filter(o=>o.kind==='connector').map(o=>({id:o.id,bounds:o.bounds,points:o.points})),overflowDetails:view.diagnostics.filter(d=>d.code==='text-overflow').map(d=>({diagnostic:d,object:scene.objects.find(o=>o.id===d.objectId),dom:[...host.querySelectorAll('[data-object-id]')].filter(n=>n.dataset.objectId===d.objectId).map(n=>({html:n.outerHTML,scrollWidth:n.scrollWidth,scrollHeight:n.scrollHeight,clientWidth:n.clientWidth,clientHeight:n.clientHeight}))}))};
    },{slide,time:manifest.freezeTimeMS});
    const diagnosticKeys=values=>values.map(d=>d.objectId+':'+d.code).sort();
    try { assert.deepEqual(diagnosticKeys(record.diagnostics),diagnosticKeys((fixture.expectedDiagnostics||[]).filter(d=>d.slide===slide)),'Unexpected renderer diagnostic on '+name); }
    catch(error) { failures.push({fixture,record,error:error.message});await page.locator('#proof-canvas').screenshot({path:path.join(output,name+'-failure.png')}); }
    await page.locator('#proof-canvas').screenshot({path:path.join(output,name+'.png'),animations:'disabled'});
    await page.evaluate(()=>{proofView.dispose();document.querySelector('#proof-canvas').remove();window.proofView=null;});
    records.push({fixture:fixture.file,capture:name+'.png',...record});
   }
  }
  assert.deepEqual(errors,[]);
  const sheet=await context.newPage();await sheet.route('**/proof-images/*',r=>{const file=path.join(output,path.basename(new URL(r.request().url()).pathname));r.fulfill({contentType:'image/png',body:fs.readFileSync(file)});});
  await sheet.setViewportSize({width:1600,height:1300});await sheet.setContent('<style>body{margin:0;background:#111;color:white;font:12px monospace;display:grid;grid-template-columns:repeat(4,1fr);gap:8px}figure{margin:0}img{width:100%}</style>'+records.map(r=>'<figure><img src="http://127.0.0.1:'+server.address().port+'/proof-images/'+r.capture+'"><figcaption>'+r.capture+'</figcaption></figure>').join(''));
  await sheet.locator('img').evaluateAll(images=>Promise.all(images.map(i=>i.decode())));await sheet.screenshot({path:path.join(output,'contact-sheet.png'),fullPage:true});
  fs.writeFileSync(path.join(output,'report.json'),JSON.stringify({engine,browser:browser.version(),manifest,records,errors,failures},null,2));assert.deepEqual(failures,[],'Unexpected corpus diagnostics');console.log('PASS: '+records.length+' '+engine+' production editor/renderer pages captured; visual approval is separate.');
 } finally {await browser.close();server.close();}
})().catch(e=>{console.error(e);server.close();process.exitCode=1;});
