// Owned fixture, actual WASM command/capability contract and shared inspector.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright'),root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-label-site'),output='/tmp/keynope-label-proof';
let presentation='';
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name==='/presentation.html'){res.setHeader('Content-Type','text/html');return res.end(presentation);}if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
  fs.mkdirSync(output,{recursive:true});await new Promise(r=>server.listen(0,'127.0.0.1',r));const browser=await playwright.chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
  try{
    const context=await browser.newContext({viewport:{width:1500,height:1000}});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',async d=>{assert.equal(d.type(),'confirm');await d.accept();});
    await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
    const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles({name:'Labels.md',mimeType:'text/markdown',buffer:fs.readFileSync(output+'/labels.md')});
    await page.waitForFunction(()=>deck.pages[0]?.scene?.objects.some(o=>o.id==='label-a'));
    const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));let s=await state();const before=structuredClone(s.slides[0]);
    await page.evaluate(elements=>editorAction({action:'select-elements',elementIndices:[0,1,2],elementsData:elements.map(e=>({id:e.id}))}),s.slides[0].elements);
    await page.getByRole('tab',{name:'Style',exact:true}).click();const panel=page.locator('[data-selection-context="shape-label"]');await panel.locator('summary').click();
    await page.evaluate(()=>window.__labelOwner=document.querySelector('#stage [data-object-id="label-a"]'));
    const size=panel.getByRole('spinbutton',{name:'Selection label size',exact:true});await size.fill('80');await size.press('Tab');
    await page.waitForFunction(()=>deck.pages[0]?.scene?.objects.find(o=>o.id==='label-a')?.label?.size===80&&document.querySelector('[data-selection-property="label.size"]')?.value==='80');
    await panel.getByRole('combobox',{name:'Selection label theme colour',exact:true}).selectOption('accent-2');
    await page.waitForFunction(()=>deck.pages[0]?.scene?.objects.find(o=>o.id==='label-a')?.labelPaint?.color==='#7c3aed');
    await panel.getByRole('combobox',{name:'Selection label font',exact:true}).selectOption('sans');
    await page.waitForFunction(()=>deck.pages[0]?.scene?.objects.find(o=>o.id==='label-a')?.label?.fontId==='sans');
    await panel.getByRole('combobox',{name:'Selection label alignment',exact:true}).selectOption('right');await page.waitForFunction(()=>deck.pages[0]?.scene?.objects.find(o=>o.id==='label-a')?.label?.align==='end');
    s=await state();for(let i=0;i<2;i++){const old=new URLSearchParams(before.elements[i].query),now=new URLSearchParams(s.slides[0].elements[i].query);assert.equal(now.get('shape-label'),old.get('shape-label'));assert.equal(now.get('fg'),old.get('fg'));assert.equal(now.get('width'),old.get('width'));}assert.equal(s.slides[0].elements[2].query,before.elements[2].query);
    assert.equal(await page.evaluate(()=>document.querySelector('#stage [data-object-id="label-a"]')===window.__labelOwner),true);
    assert.equal(await page.getByRole('combobox',{name:'Shape label font family',exact:true}).count(),0,'Legacy label controls duplicate shared controls');
    await page.screenshot({path:output+'/label-inspector.png'});
    await page.getByRole('button',{name:'Undo',exact:true}).click();await page.waitForFunction(()=>document.querySelector('[data-selection-property="label.align"]')?.value==='center');
    // Exercise the actual on-canvas editor, not a synthetic text command.
    await page.locator('.keynope-canvas-element[data-element="0"]').dblclick();
    const editable=page.locator('[contenteditable="true"]');await editable.waitFor({state:'visible'});
    await editable.fill('Edited label');await editable.press('ControlOrMeta+Enter');await editable.waitFor({state:'detached'});
    await page.waitForFunction(()=>deck.pages[0]?.scene?.objects.find(o=>o.id==='label-a')?.label?.runs?.some(r=>r.text==='Edited label'));
    s=await state();const edited=new URLSearchParams(s.slides[0].elements[0].query);
    assert.equal(edited.get('label-modern-size'),'80');assert.equal(edited.get('label-theme-fg'),'accent-2');
    assert.equal(await page.evaluate(()=>deck.pages[0].scene.objects.find(o=>o.id==='label-a').labelPaint.color),'#7c3aed');
    await page.screenshot({path:output+'/label-inline-committed.png'});
    const saved=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Labels.md'})}).then(r=>r.json()));fs.writeFileSync(output+'/labels-saved.md',saved.content);assert.match(saved.content,/label-modern-size=80/);
    presentation=await page.evaluate(()=>fetch('/api/editor/export-document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Labels.html'})}).then(r=>r.json()).then(r=>r.content));
    const exported=await context.newPage();exported.on('pageerror',e=>errors.push(e.message));await exported.goto('http://127.0.0.1:'+server.address().port+'/presentation.html');
    await exported.waitForFunction(()=>document.querySelector('.keynope-modern-presentation')?.style.visibility==='visible');
    const labelState=await exported.evaluate(()=>{const o=deck.pages[0].scene.objects.find(o=>o.id==='label-a');return {size:o.label.size,font:o.label.fontId,color:o.labelPaint.color,text:o.label.runs.map(r=>r.text).join('')};});
    assert.deepEqual(labelState,{size:80,font:'sans',color:'#7c3aed',text:'Edited label'});
    await exported.screenshot({path:output+'/label-presentation.png'});
    assert.deepEqual(errors,[]);console.log('PASS: shared label subset/context/font/size/role/alignment controls, sparse content ownership, retained shape node, Undo, actual inline edit preserves styles, Markdown and standalone presentation. '+output);
  }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);process.exitCode=1;server.close();});
