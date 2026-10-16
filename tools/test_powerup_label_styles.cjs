const {waitForAsync}=require("./wait_for_async.cjs");
// Owned fixture, actual WASM label style context, copy/paste/reset/painter.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright'),root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-label-style-site'),output='/tmp/keynope-label-style-proof';
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
  fs.mkdirSync(output,{recursive:true});await new Promise(r=>server.listen(0,'127.0.0.1',r));const browser=await playwright.chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
  try{
    const context=await browser.newContext({viewport:{width:1500,height:1000}});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',async d=>{assert.equal(d.type(),'confirm');await d.accept();});
    await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
    const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles({name:'Labels.md',mimeType:'text/markdown',buffer:fs.readFileSync('/tmp/keynope-label-proof/labels.md')});
    await page.waitForFunction(()=>deck.pages[0]?.scene?.objects.some(o=>o.id==='label-a'));
    const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
    await page.evaluate(async()=>{for(const [property,propertyValue]of [['label.size',80],['label.colorToken','accent-3'],['label.bold',true]]){const s=await fetch('/api/editor/state').then(r=>r.json());await editorAction({action:'set-selection-property',slide:0,sceneMaster:false,sceneRevision:s.version,objectIds:['label-a'],property,propertyValue});}});
    const select=async index=>{await page.locator('.keynope-canvas-element[data-element="'+index+'"]').click();await waitForAsync(page,async index=>(await fetch('/api/editor/state').then(r=>r.json())).selected===index,index);await page.getByRole('tab',{name:'Style',exact:true}).click();};
    await select(0);const beforeCopy=await state();await page.getByRole('button',{name:'Copy label style',exact:true}).click();
    await page.waitForFunction(()=>document.querySelector('[aria-label="Paste label style"]')?.disabled===false);
    const copied=await state();assert.equal(copied.version,beforeCopy.version);assert.equal(copied.styleClipboard.context,'shape-label');assert.equal(await page.getByRole('button',{name:'Paste style',exact:true}).isDisabled(),true);
    await select(1);const before=await state(),old=new URLSearchParams(before.slides[0].elements[1].query);
    await page.getByRole('button',{name:'Paste label style',exact:true}).click();await page.waitForFunction(()=>deck.pages[0].scene.objects.find(o=>o.id==='label-b').label.size===80);
    let s=await state(),q=new URLSearchParams(s.slides[0].elements[1].query);assert.equal(s.version,before.version+1);assert.equal(q.get('label-theme-fg'),'accent-3');
    for(const key of ['shape-label','fg','left','top','width','height'])assert.equal(q.get(key),old.get(key));
    await page.getByRole('button',{name:'Undo',exact:true}).click();await page.waitForFunction(()=>deck.pages[0].scene.objects.find(o=>o.id==='label-b').label.size===40);
    await select(0);await page.getByRole('button',{name:'Label format painter',exact:true}).click();await page.waitForFunction(()=>document.querySelector('[aria-label="Label format painter"]')?.getAttribute('aria-pressed')==='true');
    const unrelated=page.locator('.keynope-canvas-element[data-element="2"]');await unrelated.hover();assert.equal(await unrelated.evaluate(n=>n.classList.contains('style-painter-target')),false);
    const hit=page.locator('.keynope-canvas-element[data-element="1"]');await hit.hover();assert.equal(await hit.evaluate(n=>n.classList.contains('style-painter-target')),true);await hit.click();
    await page.waitForFunction(()=>document.querySelector('[aria-label="Label format painter"]')?.getAttribute('aria-pressed')==='false');
    assert.equal((await state()).selectionCapabilities.targetIds[0],'label-a');
    await select(1);await page.getByRole('button',{name:'Reset label style',exact:true}).click();await waitForAsync(page,async()=>!new URLSearchParams((await fetch('/api/editor/state').then(r=>r.json())).slides[0].elements[1].query).has('label-modern-size'));
    s=await state();q=new URLSearchParams(s.slides[0].elements[1].query);const payload=JSON.parse(Buffer.from(q.get('shape-label'),'base64').toString());assert.equal(payload.text,'Shared label');assert.equal(payload.future.keep,true);assert.equal(q.get('fg'),old.get('fg'));assert.equal(new URLSearchParams(payload.query).has('modern-size'),false);
    await select(0);await page.getByRole('button',{name:'Label format painter',exact:true}).click();await page.getByRole('checkbox',{name:'Keep label format painter active',exact:true}).check();await hit.click();
    assert.equal(await page.getByRole('button',{name:'Label format painter',exact:true}).getAttribute('aria-pressed'),'true');const version=(await state()).version;await page.keyboard.press('Escape');await page.waitForFunction(()=>document.querySelector('[aria-label="Label format painter"]')?.getAttribute('aria-pressed')==='false');assert.equal((await state()).version,version);
    await page.screenshot({path:output+'/label-style-transfer.png'});assert.deepEqual(errors,[]);console.log('PASS: label copy is read-only, context-safe paste, body/content preserved, one-step Undo, Reset, eligible painter hover, sticky/cancel and stable source selection. '+output);
  }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);process.exitCode=1;server.close();});
