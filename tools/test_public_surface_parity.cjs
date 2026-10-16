// Actual standalone HTML versus an independent mount of its verified public
// scene. No working app, cloud connection, PDF authoring or customer content.
const assert = require('node:assert/strict'), fs = require('node:fs'), path = require('node:path');
const pw = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const root = process.env.KEYNOPE_SURFACE_EVIDENCE || '/tmp/keynope-public-surface-proof';
const engine = process.env.TEST_BROWSER || 'chromium';
const cases = JSON.parse(fs.readFileSync(path.join(root, 'cases.json')));
const expectedPages = Number(process.env.KEYNOPE_SURFACE_EXPECTED_PAGES || 20);
assert(Number.isSafeInteger(expectedPages) && expectedPages > 0, 'Expected public page count must be positive');
assert.equal(cases.reduce((total, item) => total + item.scenes.length, 0), expectedPages, 'Public surface fixture page-count contract changed');
(async () => {
  const browser = await pw[engine].launch({ headless: true, ...(engine === 'webkit' ? {} : { executablePath: process.env.CHROME_PATH }) });
  try {
    const errors = [], records = [];
    for (const item of cases) {
      assert(item.scenes.length > 0, 'Empty public proof case');
      const width = item.scenes[0].width, height = item.scenes[0].height;
      assert(Number.isSafeInteger(width) && width > 0 && Number.isSafeInteger(height) && height > 0, 'Invalid proof canvas');
      assert(item.scenes.every(scene => scene.width === width && scene.height === height), 'Mixed dimensions within one deck');
      // Fresh physical viewport per fixture: resizing a reused Chromium
      // compositor can retain different one-channel alpha quantization even
      // when the source/reference scene JSON is byte-identical.
      const context = await browser.newContext({ viewport: { width, height }, deviceScaleFactor: 1, reducedMotion: 'reduce' });
      await context.route(/https?:\/\/.*/, r => r.abort());
      const page = await context.newPage();
      page.on('pageerror', e => errors.push(e.message));
      await page.goto('file://' + path.join(root, item.file));
      for (let index = 0; index < item.scenes.length; index++) {
        const scene = item.scenes[index], name = `${engine}-${item.file.replace('.html', '')}-${index + 1}`;
        const rect = await page.evaluate(index => keynopePrepareSnapshot(index), index);
        assert(Math.abs(rect.width - width) < .01 && Math.abs(rect.height - height) < .01, JSON.stringify(rect));
        const actual = await page.screenshot({ clip: { x: rect.x, y: rect.y, width: rect.width, height: rect.height } });
        fs.writeFileSync(path.join(root, name + '-standalone.png'), actual);
        await page.evaluate(async scene => {
          // Keep the actual backdrop and GPU-compositing ancestry. A separate
          // opaque overlay changes Chromium alpha/edge quantization even with
          // identical scene data; that is not a projection/rendering mismatch.
          const old = modernPageView.element, styles = old.style.cssText;
          window.publicProofOriginal = { old, display: old.style.display };
          old.style.display = 'none';
          window.publicProofView = KeynopeScene.mount(old.parentElement, scene);
          publicProofView.element.style.cssText = styles;
          publicProofView.element.id = 'public-proof';
          await publicProofView.snapshotAt(0); await document.fonts.ready; publicProofView.element.getBoundingClientRect();
        }, scene);
        const reference = await page.screenshot({ clip: { x: rect.x, y: rect.y, width: rect.width, height: rect.height } });
        fs.writeFileSync(path.join(root, name + '-scene.png'), reference);
        await page.evaluate(() => { publicProofView.element.style.opacity = '0'; });
        const erased = await page.screenshot({ clip: { x: rect.x, y: rect.y, width: rect.width, height: rect.height } });
        const mismatch = await page.evaluate(async ({ sources, width, height }) => {
          const canvas = document.createElement('canvas'); canvas.width = width; canvas.height = height;
          const context = canvas.getContext('2d', { willReadFrequently: true });
          const pixels = [];
          for (const source of sources) {
            const img = new Image(); img.src = 'data:image/png;base64,' + source; await img.decode();
            if (img.naturalWidth !== width || img.naturalHeight !== height) throw Error('Screenshot dimensions differ from physical slide');
            context.clearRect(0, 0, width, height); context.drawImage(img, 0, 0);
            pixels.push(context.getImageData(0, 0, width, height).data);
          }
          let changed = 0, aboveOne = 0, aboveFour = 0, aboveTwelve = 0, maximum = 0, erasedDifference = 0;
          for (let i = 0; i < pixels[0].length; i += 4) {
            let delta = 0;
            for (let c = 0; c < 4; c++) delta = Math.max(delta, Math.abs(pixels[0][i + c] - pixels[1][i + c]));
            if (delta) changed++;
            if (delta > 1) aboveOne++;
            if (delta > 4) aboveFour++;
            if (delta > 12) aboveTwelve++;
            maximum = Math.max(maximum, delta);
            if ([0, 1, 2, 3].some(c => pixels[1][i + c] !== pixels[2][i + c])) erasedDifference++;
          }
          return { changed, aboveOne, aboveFour, aboveTwelve, maximum, erasedDifference };
        }, { sources: [actual.toString('base64'), reference.toString('base64'), erased.toString('base64')], width, height });
        assert(mismatch.erasedDifference > 0, 'Comparator accepted erased scene: ' + name);
        await page.evaluate(() => { publicProofView.dispose(); publicProofOriginal.old.style.display = publicProofOriginal.display; });
        records.push({ file: item.file, page: index + 1, width, height, objects: scene.objects.length, mismatch });
        console.log(name + ': ' + JSON.stringify(mismatch));
      }
      await context.close();
    }
    fs.writeFileSync(path.join(root, engine + '-report.json'), JSON.stringify({ engine, browser: browser.version(), records, errors }, null, 2));
    assert.deepEqual(errors, []); assert.equal(records.length, expectedPages);
    assert.deepEqual(records.filter(r => r.mismatch.changed), [], 'Actual HTML output differs from verified scene rendering');
    console.log('PASS: '+expectedPages+' standalone/scene pixel comparisons');
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
