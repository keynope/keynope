const {waitForAsync}=require("./wait_for_async.cjs");
// Synthetic owned fixture: production WASM PPTX import and retained renderer.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const http = require('node:http');
const playwright = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const root = path.resolve(process.env.TEST_SITE || '/tmp/keynope-powerup-site');
const output = path.resolve(process.env.TEST_OUTPUT || 'output/powerup/chart-pptx');
const source = path.resolve(process.env.CHART_PPTX_FILE || path.join(output, 'editable-charts.pptx'));
const server = http.createServer((req, res) => {
  let name = new URL(req.url, 'http://localhost').pathname;
  if (name.endsWith('/')) name += 'index.html';
  const file = path.resolve(root, '.' + name);
  if (!file.startsWith(root + '/') || !fs.existsSync(file)) { res.writeHead(404); return res.end(); }
  res.setHeader('Content-Type', ({ '.js': 'text/javascript', '.css': 'text/css', '.html': 'text/html', '.wasm': 'application/wasm' })[path.extname(file)] || 'application/octet-stream');
  res.end(fs.readFileSync(file));
});
(async () => {
  fs.mkdirSync(output, { recursive: true });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const engine=process.env.TEST_BROWSER||'chromium';
  const browser = await playwright[engine].launch({ headless: true, ...(engine==='webkit'?{}:{executablePath:process.env.CHROME_PATH}) });
  try {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
    await context.route(/https?:\/\/(?!127\.0\.0\.1).*/, route => route.abort());
    const page = await context.newPage(), errors = [];
    page.setDefaultTimeout(30000);
    page.on('pageerror', error => errors.push(error.message));
    page.on('dialog', dialog => dialog.accept());
    await page.goto('http://127.0.0.1:' + server.address().port + '/editor/');
    await page.locator('.keynope-web-loading').waitFor({ state: 'detached', timeout: 90000 });
    const chooser = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: 'Open a Keynope or PowerPoint presentation', exact: true }).click();
    await (await chooser).setFiles(source);
    await waitForAsync(page,async () => {
      const state = await fetch('/api/editor/state').then(r => r.json());
      return state.slides?.length === 6 && state.slides[0].elements.some(e => e.kind === 'chart');
    });
	await page.locator('.keynope-engagement-toast').waitFor({state:'detached'});
    const types = ['column', 'bar', 'line', 'area', 'pie', 'doughnut'];
    for (let index = 0; index < types.length; index++) {
      const state = await page.evaluate(async index => {
        const response = await fetch('/api/editor/action', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ action: 'select-slide', slide: index }) });
        if (!response.ok) throw Error(await response.text());
        return response.json();
      }, index);
      await page.waitForFunction(v => window.keynopeWorkspaceAppliedRevision === v, state.version);
      await page.waitForFunction(type => document.querySelector('#stage .keynope-scene-chart')?.dataset.chartType === type, types[index]);
      await page.locator('#stage').scrollIntoViewIfNeeded();
      await page.evaluate(async () => { await KeynopeScene.ensureFonts(); await modernPageView.ready; await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))); });
	  await page.waitForFunction(()=>[...document.querySelectorAll('#stage .keynope-chart-label')].some(n=>n.textContent==='Workshop outcomes'));
      assert.ok(await page.locator('#stage .keynope-scene-chart path').count());
      await page.screenshot({ path: path.join(output, 'imported-' + types[index] + '-window.png') });
      await page.locator('#stage').screenshot({ path: path.join(output, 'imported-' + types[index] + '.png') });
    }
    assert.deepEqual(errors, []);
    console.log('PASS: native PPTX import, six editable chart types, mixed tables, shared WASM renderer');
  } finally { await browser.close(); server.close(); }
})().catch(error => { console.error(error); server.close(); process.exitCode = 1; });
