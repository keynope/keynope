const {waitForAsync}=require("./wait_for_async.cjs");
// Shared production WASM style-transfer and painter interaction checks.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),output=path.resolve(process.env.TEST_OUTPUT||'output/powerup');
const server=http.createServer((req,res)=>{
  let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';
  const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}
  res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm','.json':'application/json'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));
});
(async()=>{
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const engine=process.env.TEST_BROWSER||'chromium',browser=await playwright[engine].launch({headless:true,...(engine==='webkit'?{}:{executablePath:process.env.CHROME_PATH})});
  try{
    const context=await browser.newContext({viewport:{width:1440,height:1000}});
    await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());
    const page=await context.newPage(),errors=[];page.setDefaultTimeout(20000);page.on('pageerror',e=>errors.push(e.message));
    page.on('dialog',async dialog=>{assert.equal(dialog.type(),'confirm');await dialog.accept();});
    const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
    await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');
    await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
    const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();
    await (await chooser).setFiles({name:'styles.md',mimeType:'text/markdown',buffer:Buffer.from(`<!-- keynope width=245 height=56 -->

<!-- top=4 left=10 width=85 height=10 render=truetype modern-font=c64 modern-size=120 fg=#ff55aa ttf-weight=bold shadow=solid shadow-color=#5555ff shadow-x=1 shadow-y=1 -->
Source style

<!-- top=22 left=110 width=90 height=10 render=truetype modern-font=sans modern-size=72 -->
Keep this content

<!-- top=40 left=150 width=30 height=10 fg=#55aaff shape=square -->
[shape:square]
`)});
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[0].elements?.some(e=>e.text==='Keep this content');});
    const initial=await state(),elements=initial.slides[0].elements;
    const source=elements.findIndex(e=>e.text==='Source style'),target=elements.findIndex(e=>e.text==='Keep this content'),shape=elements.findIndex(e=>e.kind==='shape');
    const select=async index=>{
      await page.locator('.keynope-canvas-element[data-element="'+index+'"]').click();
      await waitForAsync(page,async id=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities?.targetIds.length===1&&s.selectionCapabilities.targetIds[0]===id;},elements[index].id);
      await page.getByRole('tab',{name:'Style',exact:true}).click();
    };
    await select(source);const beforeCopy=await state();
    await page.getByRole('button',{name:'Copy style',exact:true}).click();
    await page.waitForFunction(()=>document.querySelector('[aria-label="Paste style"]')?.disabled===false);
    const copied=await state();assert.equal(copied.version,beforeCopy.version);assert.deepEqual(copied.slides,beforeCopy.slides);assert.equal(copied.canUndo,beforeCopy.canUndo);
    await select(target);const beforePaste=await state();
    await page.getByRole('button',{name:'Paste style',exact:true}).click();
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities?.properties['text.size'].value===120;});
    const pasted=await state(),q=new URLSearchParams(pasted.slides[0].elements[target].query),old=new URLSearchParams(beforePaste.slides[0].elements[target].query);
    assert.equal(pasted.version,beforePaste.version+1);assert.equal(pasted.slides[0].elements[target].text,'Keep this content');assert.equal(q.get('shadow'),'solid');assert.equal(q.get('fg'),'#ff55aa');
    for(const key of ['left','top','width','height'])assert.equal(q.get(key),old.get(key));
    await page.getByRole('button',{name:'Undo',exact:true}).click();
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[0].elements.some(e=>e.text==='Keep this content'&&new URLSearchParams(e.query).get('modern-size')==='72');});
    await select(target);await page.getByRole('button',{name:'Reset style',exact:true}).click();
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return !new URLSearchParams(s.slides[0].elements.find(e=>e.text==='Keep this content').query).has('modern-size');});
    const reset=await state();assert.equal(reset.slides[0].elements[target].text,'Keep this content');assert.equal(new URLSearchParams(reset.slides[0].elements[target].query).get('width'),'90');
    await select(source);await page.getByRole('button',{name:'Format painter',exact:true}).click();
    await page.waitForFunction(()=>document.querySelector('[aria-label="Format painter"]')?.getAttribute('aria-pressed')==='true');
    const hit=page.locator('.keynope-canvas-element[data-element="'+target+'"]');await hit.hover();
    assert.equal(await hit.evaluate(node=>node.classList.contains('style-painter-target')),true);
    await hit.click();
    await page.waitForFunction(()=>document.querySelector('[aria-label="Format painter"]')?.getAttribute('aria-pressed')==='false');
    const painted=await state();assert.equal(new URLSearchParams(painted.slides[0].elements[target].query).get('modern-size'),'120');
    assert.equal(painted.selectionCapabilities.targetIds[0],elements[source].id,'painter should not change the selected source');
    await page.getByRole('button',{name:'Format painter',exact:true}).click();
    await page.getByRole('checkbox',{name:'Keep format painter active',exact:true}).check();
    await page.locator('.keynope-canvas-element[data-element="'+shape+'"]').click();
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return new URLSearchParams(s.slides[0].elements.find(e=>e.kind==='shape').query).get('fg')==='#ff55aa';});
    assert.equal(await page.getByRole('button',{name:'Format painter',exact:true}).getAttribute('aria-pressed'),'true');
    const beforeCancel=await state();await page.keyboard.press('Escape');
    await page.waitForFunction(()=>document.querySelector('[aria-label="Format painter"]')?.getAttribute('aria-pressed')==='false');
    assert.equal((await state()).version,beforeCancel.version);
    fs.mkdirSync(output,{recursive:true});await page.screenshot({path:path.join(output,'style-transfer.png')});
    // Open replaces the Go/WASM session but the in-memory UI style buffer survives.
    const crossChooser=page.waitForEvent('filechooser');
    await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();
    await (await crossChooser).setFiles({name:'style-recipient.md',mimeType:'text/markdown',buffer:Buffer.from('<!-- keynope width=245 height=56 -->\n\n<!-- top=12 left=20 width=100 height=10 render=truetype modern-size=72 modern-font=sans -->\nNew deck recipient\n')});
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[0].elements?.some(e=>e.text==='New deck recipient');});
    const crossState=await state(),recipient=crossState.slides[0].elements.findIndex(e=>e.text==='New deck recipient');
    assert.ok(!crossState.styleClipboard,'new engine session must not persist the style clipboard');
    await page.locator('.keynope-canvas-element[data-element="'+recipient+'"]').click();
    await page.getByRole('tab',{name:'Style',exact:true}).click();
    await page.getByRole('button',{name:'Paste style',exact:true}).click();
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities?.properties['text.size'].value===120;});
    const crossPasted=await state(),crossQ=new URLSearchParams(crossPasted.slides[0].elements[recipient].query);
    assert.equal(crossPasted.slides[0].elements[recipient].text,'New deck recipient');
    assert.equal(crossQ.get('left'),'20');assert.equal(crossQ.get('width'),'100');assert.equal(crossQ.get('fg'),'#ff55aa');
    assert.deepEqual(errors,[]);console.log('PASS: copy/paste/reset style, content and geometry isolation, undo, painter target preview, single/sticky modes, cancellation and cross-deck paste');
  }finally{await browser.close();await new Promise(resolve=>server.close(resolve));}
})().catch(error=>{console.error(error);process.exitCode=1;server.close();});
