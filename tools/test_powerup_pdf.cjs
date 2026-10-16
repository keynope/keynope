const {waitForAsync}=require("./wait_for_async.cjs");
// Owned fixture, real WASM export endpoint, browser print and PDF output.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),output=path.resolve('output/pdf/powerup');
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const browser=await playwright.chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
  try {
    const context=await browser.newContext({viewport:{width:1440,height:1000}}),errors=[];
    await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());
    await context.addInitScript(()=>{window.print=()=>{window.keynopePrintInvocations=(window.keynopePrintInvocations||0)+1;};});
    context.on('page',page=>page.on('pageerror',error=>errors.push(error.message)));
    const page=await context.newPage();page.setDefaultTimeout(30000);page.on('dialog',dialog=>dialog.accept());
    await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');
    await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
    const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles(path.join(output,'quality-proof.md'));
    await waitForAsync(page,async()=>{const state=await fetch('/api/editor/state').then(r=>r.json());return state.slides?.length===5&&state.slides[0].elements.some(e=>e.text==='Modern quality proof');});
    const before=await page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
    const popupEvent=page.waitForEvent('popup');await page.getByRole('button',{name:'Print / Save as PDF',exact:true}).click();const popup=await popupEvent;
    await popup.waitForFunction(()=>window.keynopePrintReady===true,{},{timeout:90000});
    assert.equal(await popup.locator('.keynope-print-page').count(),4);
    assert.equal(await popup.locator('iframe').count(),1,'Sampled animated page did not use exact fallback');
    assert.equal(await popup.evaluate(()=>window.keynopePrintInvocations),1);
    const info=await popup.evaluate(()=>({pages:keynopePDFManifest.pages.map(({label,quality,width,height})=>({label,quality,width,height})),text:document.getElementById('keynope-print-data').textContent}));
    assert.deepEqual(info.pages.map(p=>p.quality),[true,true,true,false]);assert.ok(!info.text.includes('TAB ONLY DO NOT EXPORT'));assert.ok(!info.text.includes('PRIVATE NOTE'));
    for(const p of info.pages)assert.equal(p.width/p.height,16/9);
    await popup.pdf({path:path.join(output,'browser-proof.pdf'),preferCSSPageSize:true,printBackground:true});
    await popup.screenshot({path:path.join(output,'browser-print-window.png')});
    const after=await page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));assert.equal(after.version,before.version,'Print mutated the active document');
    await popup.evaluate(()=>keynopeCancelPrint());assert.equal(await popup.locator('.keynope-print-page').count(),0,'Cancel retained export views');
    await popup.close();assert.deepEqual(errors,[]);
    const exported=await page.evaluate(()=>fetch('/api/editor/export-pdf',{method:'POST'}).then(r=>r.json()));
    const dataPattern=/(<script id="keynope-print-data" type="application\/json">)([\s\S]*?)(<\/script>)/;
    for(const test of ['cancel-snapshot','timeout-snapshot','cancel-fonts']) {
      console.log('Checking browser PDF '+test);
      const probe=await context.newPage();
      let html=exported.content;
      if(test!=='cancel-fonts')html=html.replace(dataPattern,(_all,before,json,after)=>{
        const payload=JSON.parse(json);payload.manifest.pages=[payload.manifest.pages.find(p=>!p.quality)];
        payload.fallback=payload.fallback.replace('</body>','<script>window.keynopePrepareSnapshot=()=>{window.__pdfStalled=true;return new Promise(()=>{});};</script></body>');
        return before+JSON.stringify(payload).replace(/</g,'\\u003c')+after;
      });
      await probe.setContent(html,{waitUntil:'domcontentloaded'});
      await probe.clock.install();
      const started=Date.now();
      await probe.evaluate(test=>{
        if(test==='cancel-fonts')globalThis.KeynopeScene={...KeynopeScene,ensureFonts:()=>{window.__pdfFontsStalled=true;return new Promise(()=>{});}};
        window.__pdfOutcome=null;
        window.__pdfTask=keynopePrepareBrowserPrint().then(()=>{window.__pdfOutcome='unexpected-success';},error=>{window.__pdfOutcome=error.name+':'+error.message;});
      },test);
      if(test==='cancel-fonts')await probe.waitForFunction(()=>window.__pdfFontsStalled);
      else await probe.waitForFunction(()=>document.querySelector('iframe')?.contentWindow?.__pdfStalled);
      if(test==='timeout-snapshot')await probe.clock.fastForward(30001);
      else await probe.evaluate(()=>keynopeCancelPrint());
      const outcome=await probe.evaluate(async()=>{await window.__pdfTask;return window.__pdfOutcome;});
      assert.match(outcome,test==='timeout-snapshot'?/took too long to prepare/:/^AbortError:/,test);
      assert.ok(Date.now()-started<10000,'Cancellation/virtual timeout waited on stalled resource');
      assert.equal(await probe.locator('.keynope-print-page').count(),0);
      assert.equal(await probe.evaluate(()=>keynopePrintReady),false);
      assert.equal(await probe.evaluate(()=>window.keynopePrintInvocations||0),0,'Cancelled/stalled export opened Print');
      await probe.close();
    }
    assert.deepEqual(errors,[]);
    console.log('PASS: WASM PDF endpoint, explicit browser print, four landscape pages, exact fallback, privacy, unchanged editor, stalled snapshot/font cancellation and bounded timeout');
  } finally {await browser.close();server.close();}
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
