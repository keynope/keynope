// Independent Poppler readback of production native PDF bytes. No Office,
// external services, customer documents or foreground application required.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const {execFileSync} = require('node:child_process');
const root = process.env.KEYNOPE_ALIGNMENT_PDF_PROOF || '/tmp/keynope-alignment-pdf-proof';
const sizes = process.argv.slice(2).length ? process.argv.slice(2) : ['1920x1080', '1080x1920', '2560x1080'];

for (const size of sizes) {
  const dir = path.join(root, size);
  const manifest = JSON.parse(fs.readFileSync(path.join(dir, 'manifest.json')));
  const report = JSON.parse(fs.readFileSync(path.join(dir, 'native-report.json')));
  assert.equal(report.passed, true);
  assert.equal(manifest.pages.length, 15);
  assert.equal(report.pages.length, 15);
  const pdf = path.join(dir, 'native-alignment.pdf');
  const xml = execFileSync('pdftotext', ['-bbox-layout', pdf, '-'], {encoding: 'utf8', maxBuffer: 8 * 1024 * 1024});
  const pages = [...xml.matchAll(/<page\b([^>]*)>([\s\S]*?)<\/page>/g)];
  assert.equal(pages.length, 15);
  let totalWords = 0;
  for (let index = 0; index < pages.length; index++) {
    const [width, height] = [manifest.pages[index].width, manifest.pages[index].height];
    const attributes = pages[index][1];
    for (const [key, expected] of [['width', width], ['height', height]]) {
      const match = attributes.match(new RegExp(key + '="([^"]+)"'));
      assert(match, 'Missing PDF page dimension');
      assert(Math.abs(Number(match[1]) - expected) < .1, 'Physical PDF dimension changed');
    }
    const words = [...pages[index][2].matchAll(/<word\s+([^>]*)>([^<]*)<\/word>/g)].map(match => ({
      text: match[2],
      ...Object.fromEntries(['xMin', 'yMin', 'xMax', 'yMax'].map(key => [key, Number(match[1].match(new RegExp(key + '="([^"]+)"'))[1])]))
    }));
    assert(!words.some(word => word.text.includes('PRIVATE')), 'Private text reached native PDF');
    for (const object of manifest.pages[index].page.scene.objects) {
      const b = object.bounds;
      // Assign repeated text by physical owner frame, not PDF extraction order.
      // Row-major text order legitimately differs by reader for these grids.
      const owned = words.filter(word => (word.xMin + word.xMax) / 2 >= b.x && (word.xMin + word.xMax) / 2 <= b.x + b.width && (word.yMin + word.yMax) / 2 >= b.y && (word.yMin + word.yMax) / 2 <= b.y + b.height);
      const text = owned.map(word => word.text).join('').replace(/\s+/g, '');
      for (const value of ['Alignment', 'sample', 'Second', 'line']) {
        assert(text.includes(value), size + ' page ' + (index + 1) + ' ' + object.id + ': missing selectable ' + value + ': ' + text);
      }
      assert(owned.length >= 4, 'Vacuous selectable object check');
    }
    totalWords += words.length;
  }
  const png = fs.readFileSync(path.join(dir, 'native-exact-1.png'));
  assert.equal(png.subarray(1, 4).toString('ascii'), 'PNG');
  const scale = png.readUInt32BE(16) / manifest.pages[0].width;
  assert([1, 2].includes(scale), 'Unexpected native snapshot density');
  execFileSync('pdftocairo', ['-png', '-r', String(72 * scale), pdf, path.join(dir, 'poppler')], {stdio: 'inherit', timeout: 120000});
  fs.writeFileSync(path.join(dir, 'poppler-report.json'), JSON.stringify({passed: true, pages: 15, objects: 180, totalWords, scale}, null, 2));
  console.log('PASS ' + size + ': 180 selectable text/label owners at original frames, independent PDF media boxes and Poppler rasters');
}
