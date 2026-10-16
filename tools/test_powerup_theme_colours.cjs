const {waitForAsync}=require("./wait_for_async.cjs");
// Actual WASM inspector, owned master-bound text, theme-role/RGB inheritance.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=process.env.TEST_SITE||'/tmp/keynope-powerup-site',output='/tmp/keynope-theme-colour-proof';
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm','.json':'application/json'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));const browser=await playwright.chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
  try{
    const context=await browser.newContext({viewport:{width:1500,height:1000}});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
    const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',async d=>{assert.equal(d.type(),'confirm');await d.accept();});
    await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
    const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles({name:'Colours.md',mimeType:'text/markdown',buffer:fs.readFileSync(output+'/colours.md')});
    await page.waitForFunction(()=>deck.pages[pageIndex]?.scene?.objects.some(o=>o.id==='instance'));
    const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
    const select=async id=>{const s=await state(),i=s.slides[s.current].elements.findIndex(e=>e.id===id);await page.evaluate(({i,e})=>editorAction({action:'select-element',element:i,elementData:e}),{i,e:s.slides[s.current].elements[i]});};
    const color=()=>page.evaluate(()=>deck.pages[pageIndex].scene.objects.find(o=>o.id==='instance').paint.color);
    await select('instance');await page.getByRole('tab',{name:'Style',exact:true}).click();
    const role=page.getByRole('combobox',{name:'Selection theme colour',exact:true});await role.waitFor();assert.equal(await role.inputValue(),'custom');assert.equal(await color(),'#aa0055');
    assert.equal(await page.locator('select[aria-label="Theme colour"]').count(),0,'Detached legacy token selector duplicates shared control');
    const theme=(await state()).theme;await role.selectOption('accent-1');
    await page.waitForFunction(c=>deck.pages[pageIndex]?.scene?.objects.find(o=>o.id==='instance')?.paint.color===c,theme.colors.accent[0]);assert.equal(await role.inputValue(),'accent-1');
    let q=new URLSearchParams((await state()).slides[0].elements.find(e=>e.id==='instance').query);assert.equal(q.get('theme-fg'),'accent-1');assert.equal(q.get('master-clear'),'fg,header,shadow');
    await page.evaluate(()=>{window.__colourOwner=document.querySelector('#stage [data-object-id="instance"]');});
    await page.getByRole('button',{name:'Selection base colour',exact:true}).click();const picker=page.getByRole('dialog',{name:'Pick color',exact:true});await picker.getByRole('textbox',{name:'HTML color',exact:true}).fill('#55AAFF');await picker.getByRole('button',{name:'Apply',exact:true}).click();
    await page.waitForFunction(()=>document.querySelector('[data-selection-property="paint.colorToken"]')?.value==='custom'&&deck.pages[pageIndex]?.scene?.objects.find(o=>o.id==='instance')?.paint.color==='#55aaff');
    await page.getByRole('button',{name:'Undo',exact:true}).click();await page.waitForFunction(()=>document.querySelector('[data-selection-property="paint.colorToken"]')?.value==='accent-1');
    // Edit the palette through the real theme dialog and shared RGB picker.
    await page.getByRole('button',{name:'Slide',exact:true}).click();await page.getByRole('button',{name:'Edit theme…',exact:true}).click();
    const dialog=page.getByRole('dialog',{name:'Edit deck theme',exact:true});await dialog.getByRole('button',{name:'Theme accent 1',exact:true}).click();
    await picker.getByRole('textbox',{name:'HTML color',exact:true}).fill('#0055AA');await picker.getByRole('button',{name:'Apply',exact:true}).click();await dialog.getByRole('button',{name:'Apply theme',exact:true}).click();
    await page.waitForFunction(()=>deck.pages[pageIndex]?.scene?.objects.find(o=>o.id==='instance')?.paint.color==='#0055aa');
    assert(await page.evaluate(()=>window.__colourOwner===document.querySelector('#stage [data-object-id="instance"]')),'Palette edit remounted text');
    await select('instance');await page.getByRole('tab',{name:'Style',exact:true}).click();assert((await role.locator('option[value="accent-1"]').textContent()).includes('#0055aa'));
    const swatch=page.getByRole('button',{name:'Selection base colour',exact:true});
    await swatch.hover();assert.equal(await swatch.evaluate(n=>getComputedStyle(n,'::before').backgroundColor),'rgb(0, 85, 170)','Hover recoloured the swatch');
    await swatch.focus();assert.equal(await swatch.evaluate(n=>getComputedStyle(n,'::before').backgroundColor),'rgb(0, 85, 170)','Focus recoloured the swatch');
    await page.screenshot({path:output+'/theme-role-inspector.png'});
    const saved=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Colours.md'})}).then(r=>r.json()));assert(saved.content.includes('theme-fg=accent-1'));fs.writeFileSync(output+'/colours-saved.md',saved.content);
    await role.selectOption('');await page.waitForFunction(()=>deck.pages[pageIndex]?.scene?.objects.find(o=>o.id==='instance')?.paint.color==='#aa0055');
    q=new URLSearchParams((await state()).slides[0].elements.find(e=>e.id==='instance').query);assert.equal(q.get('master-clear'),'shadow');
    await page.getByRole('button',{name:'Reset style',exact:true}).click();
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities?.properties?.['paint.shadow']?.value==='solid'&&!new URLSearchParams(s.slides[0].elements.find(e=>e.id==='instance').query).has('master-clear');});
    await page.getByRole('button',{name:'Undo',exact:true}).click();
    await waitForAsync(page,async()=>new URLSearchParams((await fetch('/api/editor/state').then(r=>r.json())).slides[0].elements.find(e=>e.id==='instance').query).get('master-clear')==='shadow');
    await page.setViewportSize({width:520,height:900});await page.screenshot({path:output+'/theme-role-narrow.png'});
    assert.deepEqual(errors,[]);console.log('PASS: real role selector/RGB/Undo/theme picker, master precedence/Inherit, portable tokens and retained text owner. Screenshots: '+output);
  }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);process.exitCode=1;server.close();});
