const {waitForAsync}=require("./wait_for_async.cjs");
// Owned mixed fixture: real WASM Open, builds preview, save and PPTX download.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const http = require('node:http');
const playwright = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const root = path.resolve(process.env.TEST_SITE || '/tmp/keynope-powerup-site');
const output = path.resolve(process.env.TIMING_PROOF || '/tmp/keynope-timing-proof');
const server = http.createServer((req, res) => {
  let name = new URL(req.url, 'http://localhost').pathname;
  if (name.endsWith('/')) name += 'index.html';
  const file = path.resolve(root, '.' + name);
  if (!file.startsWith(root + '/') || !fs.existsSync(file)) { res.writeHead(404); return res.end(); }
  res.setHeader('Content-Type', ({'.js':'text/javascript','.html':'text/html','.css':'text/css','.wasm':'application/wasm'})[path.extname(file)] || 'application/octet-stream');
  res.end(fs.readFileSync(file));
});
(async () => {
  fs.mkdirSync(output, {recursive:true});
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const browser = await playwright.chromium.launch({headless:true, executablePath:process.env.CHROME_PATH});
  try {
    const context = await browser.newContext({viewport:{width:1500,height:1000}});
    // Native Chrome file pickers cannot be driven headlessly. Exercise the
    // actual supported download fallback, not a mocked export endpoint.
    await context.addInitScript(() => { delete window.showSaveFilePicker; });
    await context.route(/https?:\/\/(?!127\.0\.0\.1).*/, route => route.abort());
    const page = await context.newPage(), errors=[];
    page.setDefaultTimeout(30000);
    page.on('pageerror', error => errors.push(error.message));
    page.on('dialog', dialog => dialog.accept());
    const state = () => page.evaluate(() => fetch('/api/editor/state').then(r=>r.json()));
    const open = async file => {
      const chooser = page.waitForEvent('filechooser');
      await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();
      await (await chooser).setFiles(file);
      await waitForAsync(page,async () => (await fetch('/api/editor/state').then(r=>r.json())).slideBuild?.steps?.length === 4);
      await page.waitForFunction(() => document.querySelector('#stage .keynope-scene-table') && document.querySelector('#stage .keynope-scene-chart'));
    };
    const signature = s => {
      const kinds = new Map(s.slides[s.current].elements.map(e=>[e.id,e.kind]));
      return {transition:s.slideBuild.transition, steps:s.slideBuild.steps.map(step=>({...step,targetIds:step.targetIds.map(id=>kinds.get(id))}))};
    };
    await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');
    await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
    await open(path.join(output,'timing-editable.pptx'));
    const imported = await state(), expected = signature(imported);
    assert.deepEqual(expected.steps.map(s=>s.targetIds),[['text'],['shape','table'],['chart'],['image']]);
    assert.deepEqual(expected.steps.map(s=>s.effect),['fade','appear','wipe','zoom']);
    assert.equal(expected.transition.effect,'fade'); assert.equal(expected.transition.durationMs,1200);
    await page.getByRole('button',{name:'Slide',exact:true}).click();
    await page.getByRole('button',{name:'Edit slide builds',exact:true}).click();
    const dialog=page.getByRole('dialog',{name:'Slide builds',exact:true}); await dialog.waitFor();
    const preview=dialog.locator('.keynope-build-preview');
    await preview.locator('.keynope-scene-table').waitFor({state:'attached'});
    const ids=imported.slideBuild.steps.flatMap(s=>s.targetIds);
    for(const id of ids) assert.equal(await preview.locator(`[data-object-id="${id}"]`).evaluate(n=>getComputedStyle(n).visibility),'hidden');
    await dialog.getByRole('button',{name:'Next build',exact:true}).click();
    await page.waitForTimeout(600);
    assert.equal(await preview.locator(`[data-object-id="${ids[0]}"]`).evaluate(n=>getComputedStyle(n).visibility),'visible');
    await dialog.getByRole('button',{name:'Next build',exact:true}).click();
    // Preview may automatically continue the following after-previous steps.
    await page.waitForTimeout(1700);
    for(const id of ids) assert.equal(await preview.locator(`[data-object-id="${id}"]`).evaluate(n=>getComputedStyle(n).visibility),'visible');
    await page.screenshot({path:path.join(output,'timing-browser-preview.png')});
    assert.equal((await state()).version,imported.version,'Preview changed authored history');
    const effect=dialog.getByRole('combobox',{name:'Build effect',exact:true});
    await effect.selectOption((await effect.inputValue())==='fade'?'appear':'fade');
    await dialog.getByRole('button',{name:'Apply',exact:true}).click();
    await dialog.waitFor({state:'detached'});
    await page.evaluate(()=>editorAction({action:'undo'}));
    assert.deepEqual(signature(await state()),expected);
    await page.evaluate(()=>editorAction({action:'redo'}));
    await page.evaluate(()=>editorAction({action:'undo'}));
    const saved=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Timeline.md'})}).then(r=>r.json()));
    fs.writeFileSync(path.join(output,'timing-browser-saved.md'),saved.content);
    await page.getByRole('button',{name:'Insert',exact:true}).click();
    const downloadEvent=page.waitForEvent('download');
    await page.getByRole('button',{name:'Export PowerPoint presentation',exact:true}).click();
    const download=await downloadEvent, file=path.join(output,'timing-browser.pptx');
    await download.saveAs(file);
    await open(file);
    assert.deepEqual(signature(await state()),expected,'Browser PPTX round trip lost timing or targets');
    await page.waitForFunction(()=>!document.querySelector('.keynope-engagement-toast'));
    await page.screenshot({path:path.join(output,'timing-browser-imported.png')});
    assert.deepEqual(errors,[]);
    fs.writeFileSync(path.join(output,'browser-report.json'),JSON.stringify({passed:true,checks:['actual PPTX file chooser','mixed editable target remapping','hidden initial and automatic preview reveals','preview history isolation','timeline Apply/Undo/Redo','Markdown save','actual PPTX download and re-open']},null,2)+'\n');
    console.log('PASS: WASM mixed-object PPTX timing import, preview, history, save and download/re-open');
  } finally { await browser.close(); await new Promise(resolve=>server.close(resolve)); }
})().catch(error=>{console.error(error);process.exitCode=1;server.close();});
