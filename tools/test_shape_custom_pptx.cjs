// Owned catalogue fixture: actual WASM export/reimport, no external services.
const assert = require('node:assert/strict');
const fs = require('node:fs'), http = require('node:http'), path = require('node:path');
const {chromium} = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const root = path.resolve(process.env.TEST_SITE || '/tmp/keynope-powerup-site');
const output = '/tmp/keynope-custom-geometry-proof';
const server = http.createServer((req, res) => {
  let name = new URL(req.url, 'http://localhost').pathname;
  if (name.endsWith('/')) name += 'index.html';
  const file = path.resolve(root, '.' + name);
  if (!file.startsWith(root + '/') || !fs.existsSync(file)) { res.writeHead(404); return res.end(); }
  res.setHeader('Content-Type', ({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)] || 'application/octet-stream');
  res.end(fs.readFileSync(file));
});
(async () => {
  await new Promise(r => server.listen(0, '127.0.0.1', r));
  const browser = await chromium.launch({headless:true, executablePath:process.env.CHROME_PATH});
  try {
    const context = await browser.newContext({viewport:{width:1500,height:1000}, reducedMotion:'reduce'});
    await context.route(/https?:\/\/(?!127\.0\.0\.1).*/, r => r.abort());
    const page = await context.newPage(), errors = [];
    page.on('pageerror', e => errors.push(e.message));
    page.on('dialog', d => d.accept());
    await page.goto('http://127.0.0.1:' + server.address().port + '/editor/');
    await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
    const open = async (name, buffer, mimeType) => {
      const picker = page.waitForEvent('filechooser');
      await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();
      await (await picker).setFiles({name,mimeType,buffer});
    };
    const shapes = () => page.evaluate(() => deck.pages.map(p => p.scene.objects.filter(o => o.kind === 'shape' && ['#0e7490','#ff0055'].includes(o.paint.color.toLowerCase())).sort((a,b) => a.bounds.y-b.bounds.y || a.bounds.x-b.bounds.x)));
    await open('custom.md',fs.readFileSync(output+'/custom.md'),'text/markdown');
    await page.waitForFunction(() => deck.pages.length === 4 && deck.pages.every(p => p.scene?.objects.filter(o => o.kind === 'shape').length >= 5));
    const before = await shapes();
    await page.screenshot({path:output+'/original.png'});
    await page.locator('.keynope-slide-item').nth(1).click();
    await page.waitForFunction(() => pageIndex === 1);
    await page.screenshot({path:output+'/original-page2.png'});
    await page.locator('.keynope-slide-item').nth(2).click();
    await page.waitForFunction(() => pageIndex === 2);
    await page.screenshot({path:output+'/original-page3.png'});
    await page.locator('.keynope-slide-item').nth(3).click();
    await page.waitForFunction(() => pageIndex === 3);
    await page.screenshot({path:output+'/original-page4.png'});
    const exported = await page.evaluate(() => fetch('/api/editor/export-pptx',{method:'POST'}).then(r=>r.json()));
    assert(exported.content);
    assert(!(exported.report?.warnings||[]).some(w => /shape.*approximated/.test(w.code)));
    fs.writeFileSync(output+'/wasm-custom.pptx',Buffer.from(exported.content,'base64'));
    await open('custom.pptx',Buffer.from(exported.content,'base64'),'application/vnd.openxmlformats-officedocument.presentationml.presentation');
    await page.waitForFunction(() => deck.pages[0]?.scene?.objects.some(o=>o.id.startsWith('pptx-')));
    const after = await shapes();
    const number = /[-+]?(?:\d*\.?\d+)(?:[eE][-+]?\d+)?/g;
    assert.equal(after.length,4);
    for (let p=0;p<4;p++) {
      assert.equal(before[p].length,5); assert.equal(after[p].length,5);
      for (let i=0;i<5;i++) {
        const a=before[p][i], b=after[p][i];
        assert.equal(b.shape,a.shape); assert.equal(b.editable,true);
        const x=a.shapeGeometry.path,y=b.shapeGeometry.path;
        assert.equal(x.replace(number,'#'),y.replace(number,'#'));
        const ax=x.match(number).map(Number),by=y.match(number).map(Number);
        ax.forEach((v,j)=>assert(Math.abs(v-by[j])<.00002,`${a.shape} outline drift`));
        assert.deepEqual(a.shapeGeometry.details,b.shapeGeometry.details,`${a.shape} detail drift`);
        assert.equal((a.paint.stroke||'').toLowerCase(),(b.paint.stroke||'').toLowerCase(),`${a.shape} stroke paint drift`);
        if(a.paint.stroke) assert(Math.abs(a.paint.strokeWidth-b.paint.strokeWidth)<.001,`${a.shape} stroke width drift`);
        for (const key of ['x','y','width','height']) assert(Math.abs(a.bounds[key]-b.bounds[key])<.001,`${a.shape} frame drift`);
      }
    }
    await page.screenshot({path:output+'/reimported.png'});
    await page.locator('.keynope-slide-item').nth(1).click();
    await page.waitForFunction(() => pageIndex === 1);
    await page.screenshot({path:output+'/reimported-page2.png'});
    await page.locator('.keynope-slide-item').nth(2).click();
    await page.waitForFunction(() => pageIndex === 2);
    await page.screenshot({path:output+'/reimported-page3.png'});
    await page.locator('.keynope-slide-item').nth(3).click();
    await page.waitForFunction(() => pageIndex === 3);
    await page.screenshot({path:output+'/reimported-page4.png'});
    assert.deepEqual(errors,[]);
    console.log('PASS: ten native custom catalogue shapes, database details, four manual-input slopes and inherited star retain editable contours, paint and frames through WASM PPTX export/reimport');
  } finally { await browser.close(); server.close(); }
})().catch(e=>{console.error(e);server.close();process.exitCode=1;});
