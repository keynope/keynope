// Owned structured PDF document; real print painter and Chromium PDF backend.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.KEYNOPE_STRUCTURED_PDF_EVIDENCE||'output/pdf/structured');
(async()=>{
  const browser=await playwright.chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
  try{
    const context=await browser.newContext({viewport:{width:1920,height:1080}});await context.route(/https?:\/\/.*/,r=>r.abort());const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
    await context.addInitScript(()=>window.print=()=>{});
    await page.goto('file://'+path.join(root,'quality.html'));const expected=JSON.parse(fs.readFileSync(path.join(root,'expected.json')));
    await page.evaluate(()=>keynopePrepareBrowserPrint());assert.equal(await page.locator('.keynope-print-page').count(),expected.length);assert.equal(await page.locator('iframe').count(),0);
    await page.pdf({path:path.join(root,'browser-quality.pdf'),preferCSSPageSize:true,printBackground:true});
    const records=[],fallback=await context.newPage();await fallback.goto('file://'+path.join(root,'raster.html'));
    const sample=page=>page.evaluate(()=>{const scene=document.querySelector('.keynope-modern-scene');const base=scene.getBoundingClientRect();return [...scene.querySelectorAll('.keynope-shaped-text')].map(n=>{const r=n.getBoundingClientRect(),s=getComputedStyle(n);return {text:n.textContent,x:r.x-base.x,y:r.y-base.y,width:r.width,height:r.height,font:s.fontFamily,size:s.fontSize,align:s.textAlign,letter:s.letterSpacing};});});
    for(let i=0;i<expected.length;i++){
      await page.evaluate(i=>keynopePrepareSnapshot(i),i);
      const proof=await page.evaluate(()=>{const scene=document.querySelector('.keynope-modern-scene'),bounds=scene.getBoundingClientRect();return {width:bounds.width,height:bounds.height,text:scene.textContent,controls:[...scene.querySelectorAll('.keynope-chart-data-access')].filter(n=>getComputedStyle(n).display!=='none').length};});
      assert.equal(proof.width,1920);assert.equal(proof.height,1080);assert(proof.text.includes(expected[i]));assert.equal(proof.controls,0);assert(!proof.text.includes('PRIVATE STRUCTURED'));
      await page.screenshot({path:path.join(root,'browser-slide-'+(i+1)+'.png')});records.push(proof);
      await fallback.evaluate(i=>keynopePrepareSnapshot(i),i);
      const qualityText=await sample(page),fallbackText=await sample(fallback);
      fs.writeFileSync(path.join(root,'layout-'+(i+1)+'.json'),JSON.stringify({quality:qualityText,fallback:fallbackText},null,2));
    }
    assert.deepEqual(errors,[]);fs.writeFileSync(path.join(root,'browser-report.json'),JSON.stringify({passed:true,pages:records},null,2));console.log('PASS: '+expected.length+' landscape browser quality pages and isolated snapshots; no private metadata or printable accessibility UI.');
  }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
