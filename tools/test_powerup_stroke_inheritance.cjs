const {waitForAsync}=require("./wait_for_async.cjs");
// Owned master-bound shape; production WASM commands and retained renderer.
const assert = require('node:assert/strict'), fs = require('node:fs'), path = require('node:path'), http = require('node:http');
const playwright = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const root = path.resolve(process.env.TEST_SITE || '/tmp/keynope-stroke-inheritance-site');
const output = process.env.KEYNOPE_STROKE_INHERITANCE_EVIDENCE || '/tmp/keynope-stroke-inheritance-proof';
const server = http.createServer((req, res) => {
  let name = new URL(req.url, 'http://localhost').pathname;
  if (name.endsWith('/')) name += 'index.html';
  const file = path.resolve(root, '.' + name);
  if (!file.startsWith(root + '/') || !fs.existsSync(file)) { res.writeHead(404); return res.end(); }
  res.setHeader('Content-Type', ({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm','.json':'application/json'})[path.extname(file)] || 'application/octet-stream');
  res.end(fs.readFileSync(file));
});
(async () => {
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const browser = await playwright.chromium.launch({headless:true, executablePath:process.env.CHROME_PATH});
  try {
    const context = await browser.newContext({viewport:{width:1500,height:1000}});
    await context.route(/https?:\/\/(?!127\.0\.0\.1).*/, route => route.abort());
    const page = await context.newPage(), errors = [];
    page.on('pageerror', error => errors.push(error.message));
    page.on('dialog', async dialog => { assert.equal(dialog.type(), 'confirm'); await dialog.accept(); });
    await page.goto('http://127.0.0.1:' + server.address().port + '/editor/');
    await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
    const state = () => page.evaluate(() => fetch('/api/editor/state').then(r => r.json()));
    let opens = 0;
    const open = async content => {
      const name = 'Stroke-inheritance-' + (++opens) + '.md', chooser = page.waitForEvent('filechooser');
      await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();
      await (await chooser).setFiles({name,mimeType:'text/markdown',buffer:Buffer.from(content)});
      await waitForAsync(page,async name => (await fetch('/api/editor/state').then(r=>r.json())).path === name, name);
    };
    const select = async id => {
      const s = await state(), i = s.slides[0].elements.findIndex(e => e.id === id);
      assert(i >= 0, 'Missing fixture object ' + id);
      await page.evaluate(({i,e}) => editorAction({action:'select-element',element:i,elementData:e}), {i,e:s.slides[0].elements[i]});
      await page.waitForFunction(id => deck.pages[pageIndex]?.scene?.objects.some(o=>o.id===id), id);
      await page.getByRole('tab',{name:'Style',exact:true}).click();
    };
    const edge = (color, width) => page.waitForFunction(({color,width}) => {
      const o = deck.pages[pageIndex]?.scene?.objects.find(o=>o.id==='instance');
      const p = document.querySelector('#stage [data-object-id="instance"] > svg > path');
      return o && p && (o.paint.stroke || '') === color && o.paint.strokeWidth === width && (p.getAttribute('stroke') || '') === color;
    }, {color,width});
    await open(fs.readFileSync(path.join(output,'inheritance.md'),'utf8'));
    await select('instance'); await edge('#00aa55',7.5);
    const original = (await state()).slides[0].elements.find(e=>e.id==='instance');
    await page.evaluate(() => { window.__strokeInstance = document.querySelector('#stage [data-object-id="instance"]'); });
    const outline = page.getByRole('combobox',{name:'Selection outline',exact:true});
    const strokeRole=page.getByRole('combobox',{name:'Selection stroke theme colour',exact:true});
    const roleColor=(await state()).theme.colors.accent[1];
    await strokeRole.selectOption('accent-2');await edge(roleColor,7.5);
    assert.equal(await strokeRole.inputValue(),'accent-2');
    assert.equal(new URLSearchParams((await state()).slides[0].elements.find(e=>e.id==='instance').query).get('shape-stroke'),'theme:accent-2');
    await page.screenshot({path:path.join(output,'theme-stroke.png')});
    await page.getByRole('button',{name:'Undo',exact:true}).click();await edge('#00aa55',7.5);
    assert.equal(await strokeRole.inputValue(),'custom');
    await outline.selectOption('none'); await edge('',0);
    assert.equal(await outline.inputValue(),'none');
    let q = new URLSearchParams((await state()).slides[0].elements.find(e=>e.id==='instance').query);
    assert.equal(q.get('master-clear'),'shadow,shape-stroke,shape-stroke-width');
    assert.equal(q.get('shape-label'),new URLSearchParams(original.query).get('shape-label'));
    await page.getByRole('button',{name:'Undo',exact:true}).click(); await edge('#00aa55',7.5);
    await page.getByRole('button',{name:'Redo',exact:true}).click(); await edge('',0);
    for (const preset of ['light','dark']) {
      await outline.selectOption(preset);
      await edge(preset==='light'?'#ffffff':'#111111',2);
      assert.equal(await outline.inputValue(),preset);
    }
    await page.getByRole('button',{name:'Selection stroke colour',exact:true}).click();
    const picker = page.getByRole('dialog',{name:'Pick color',exact:true});
    await picker.getByRole('textbox',{name:'HTML color',exact:true}).fill('#FF55AA');
    await picker.getByRole('button',{name:'Apply',exact:true}).click();
    await edge('#ff55aa',2);
    assert.equal(await page.getByRole('spinbutton',{name:'Selection stroke width',exact:true}).inputValue(),'2');
    await page.getByRole('button',{name:'Undo',exact:true}).click(); await edge('#111111',2);
    await page.getByRole('button',{name:'Redo',exact:true}).click(); await edge('#ff55aa',2);
    await outline.selectOption(''); await edge('#00aa55',7.5);
    q = new URLSearchParams((await state()).slides[0].elements.find(e=>e.id==='instance').query);
    assert.equal(q.get('master-clear'),'shadow');
    await select('source'); await page.getByRole('button',{name:'Copy style',exact:true}).click();
    await select('instance'); await page.getByRole('button',{name:'Paste style',exact:true}).click();
    await edge('#111111',2); assert.equal(await outline.inputValue(),'dark');
    q = new URLSearchParams((await state()).slides[0].elements.find(e=>e.id==='instance').query);
    assert.equal(q.get('master-clear'),'header,shape-stroke,shape-stroke-width,theme-fg');
    assert.equal(q.get('left'),'30'); assert.equal(q.get('top'),'10');
    assert.equal(q.get('shape-label'),new URLSearchParams(original.query).get('shape-label'));
    assert(await page.evaluate(()=>window.__strokeInstance===document.querySelector('#stage [data-object-id="instance"]')),'Outline edits remounted the shape owner');
    await page.screenshot({path:path.join(output,'outline-inheritance.png')});
    const saved = await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Stroke-inheritance.md'})}).then(r=>r.json()));
    assert(saved.content.includes('shape-stroke-width')); fs.writeFileSync(path.join(output,'saved.md'),saved.content);
    await open(saved.content); await select('instance'); assert.equal(await outline.inputValue(),'dark');
    await page.getByRole('button',{name:'Reset style',exact:true}).click(); await edge('#00aa55',7.5);
    q = new URLSearchParams((await state()).slides[0].elements.find(e=>e.id==='instance').query);
    assert.equal(q.get('master-clear'),null); assert.equal(q.get('left'),'30'); assert.equal(q.get('top'),'10');
    assert.deepEqual(errors,[]);
    fs.writeFileSync(path.join(output,'browser-report.json'),JSON.stringify({passed:true,checks:['Off overrides master stroke','presets and Inherit','colour picker preserves preset width','Undo/Redo','Copy/Paste style alias precedence','retained shape owner and label','Markdown save/reopen','Reset restores master']},null,2));
    console.log('PASS: real WASM outline/master stroke precedence, history, style transfer, retained owner, save/reopen and reset');
  } finally { await browser.close(); await new Promise(resolve=>server.close(resolve)); }
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
