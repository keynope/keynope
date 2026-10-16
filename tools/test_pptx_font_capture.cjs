// Real WASM + shared-renderer capture. No system PowerPoint or foreground app.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const {waitForAsync}=require('./wait_for_async.cjs');
const engine=process.env.TEST_BROWSER||'chromium',playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),proof=process.env.KEYNOPE_FONT_CAPTURE_EVIDENCE||'/tmp/keynope-font-capture-proof';
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));const browser=await playwright[engine].launch({headless:true,...(engine==='webkit'?{}:{executablePath:process.env.CHROME_PATH})});
 try{
  const context=await browser.newContext({viewport:{width:1440,height:1000}});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles(path.join(proof,'font-art.md'));
  await waitForAsync(page,async()=>(await fetch('/api/editor/state').then(r=>r.json())).path.endsWith('font-art.md'));
  const result=await page.evaluate(async()=>{
    const before=await fetch('/api/editor/state').then(r=>r.json()),body=await window.keynopePreparePPTXExport(),request=JSON.parse(body);
    const response=await fetch('/api/editor/export-pptx',{method:'POST',headers:{'Content-Type':'application/json'},body});if(!response.ok)throw Error(await response.text());
    const payload=await response.json(),after=await fetch('/api/editor/state').then(r=>r.json());
    const controller=new AbortController(),original=HTMLCanvasElement.prototype.toDataURL;let midCaptureCancel='';
    HTMLCanvasElement.prototype.toDataURL=function(...args){const source=original.apply(this,args);if(this.closest('[data-keynope-pptx-capture]'))controller.abort();return source;};
    try{await KeynopeScene.capturePPTX({signal:controller.signal});}catch(error){midCaptureCancel=error.name;}finally{HTMLCanvasElement.prototype.toDataURL=original;}
    const remainingCaptureHosts=document.querySelectorAll('[data-keynope-pptx-capture]').length;
    const cancelled=window.keynopePreparePPTXExport();window.keynopeCancelPPTXCapture();let cancelName='';try{await cancelled;}catch(error){cancelName=error.name;}
    await editorAction({action:'add-element',kind:'text'});
    const stale=await fetch('/api/editor/export-pptx',{method:'POST',headers:{'Content-Type':'application/json'},body});
    return {...payload,request,cancelName,midCaptureCancel,remainingCaptureHosts,staleStatus:stale.status,beforeVersion:before.version,afterVersion:after.version,unchanged:JSON.stringify(before.slides)===JSON.stringify(after.slides)};
  });
  assert.equal(result.request.captures.length,5);assert.equal(result.request.captures.filter(c=>c.frames.length===2).length,1);assert.equal(result.beforeVersion,result.afterVersion);assert(result.unchanged);assert.equal(result.cancelName,'AbortError');assert.equal(result.staleStatus,409);
  assert.equal(result.midCaptureCancel,'AbortError');assert.equal(result.remainingCaptureHosts,0);
  const notes=result.report.warnings||[];assert.equal(notes.filter(w=>w.code==='retro-font-artwork-rasterized').length,5);assert(!notes.some(w=>w.code==='retro-image-capture-unavailable'));assert.deepEqual(errors,[]);
  await page.evaluate(async captures=>{
    const sheet=document.createElement('section');sheet.style.cssText='position:fixed;inset:0;z-index:99999;background:#171b20;color:white;overflow:auto;padding:24px;font:16px monospace';sheet.id='font-proof';document.body.append(sheet);
    for(const capture of captures){const heading=document.createElement('h2');heading.textContent=capture.objectId;sheet.append(heading);const row=document.createElement('div');row.style.cssText='display:flex;gap:16px';sheet.append(row);for(const frame of capture.frames){const image=new Image();image.src=frame;image.style.cssText='max-width:600px;max-height:300px;object-fit:contain;background:#080b10;border:1px solid #666';await image.decode();row.append(image);}}
  },result.request.captures);
  await page.screenshot({path:path.join(proof,'font-art-'+engine+'.png')});await page.evaluate(()=>{const s=document.querySelector('#font-proof');s.scrollTop=s.scrollHeight;});await page.screenshot({path:path.join(proof,'font-animation-background-'+engine+'.png')});
  fs.writeFileSync(path.join(proof,'browser-export.json'),JSON.stringify(result));console.log('PASS '+engine+': 5 captured font-dependent images/background, rendered GIF frames, unchanged source, cancellation and stale revision rejection.');
  if(fs.existsSync(path.join(proof,'legacy-diagram.md'))){
    await page.evaluate(()=>document.querySelector('#font-proof').remove());
    const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles(path.join(proof,'legacy-diagram.md'));
    await waitForAsync(page,async()=>(await fetch('/api/editor/state').then(r=>r.json())).path.endsWith('legacy-diagram.md'));
    const legacy=await page.evaluate(async()=>{const body=await window.keynopePreparePPTXExport(),request=JSON.parse(body);const response=await fetch('/api/editor/export-pptx',{method:'POST',headers:{'Content-Type':'application/json'},body});if(!response.ok)throw Error(await response.text());return {...await response.json(),captureCount:request.captures.length};});
    assert.equal(legacy.captureCount,0);assert(!(legacy.report.warnings||[]).some(w=>w.code==='retro-artwork-omitted'));
    await page.screenshot({path:path.join(proof,'legacy-diagram-'+engine+'.png')});fs.writeFileSync(path.join(proof,'legacy-browser-export.json'),JSON.stringify(legacy));console.log('PASS '+engine+': legacy diagram exported as native shapes, label and attached connector without raster capture.');
  }
 }finally{await browser.close();server.close();}
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
