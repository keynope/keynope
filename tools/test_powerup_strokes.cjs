const {waitForAsync}=require("./wait_for_async.cjs");
// Real WASM inspector and shared renderer, owned fixtures, no cloud traffic.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),output=process.env.KEYNOPE_STROKE_EVIDENCE||'/tmp/keynope-stroke-proof';
const server=http.createServer((req,res)=>{
  let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';
  const file=name==='/strokes.html'?path.join(output,'strokes.html'):path.resolve(root,'.'+name);
  if(!fs.existsSync(file)||(name!=='/strokes.html'&&!file.startsWith(root+'/'))){res.writeHead(404);return res.end();}
  res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm','.json':'application/json'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));
});
(async()=>{
  fs.mkdirSync(output,{recursive:true});await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const browser=await playwright.chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
  try{
    const context=await browser.newContext({viewport:{width:1600,height:1040}});
    await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());
    const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));page.setDefaultTimeout(20000);
    page.on('dialog',async dialog=>{assert.equal(dialog.type(),'confirm');await dialog.accept();});
    await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');
    await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
    const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
    const action=data=>page.evaluate(async data=>{const r=await fetch('/api/editor/action',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(data)});if(!r.ok)throw Error(await r.text());return r.json();},data);
    let openCount=0;
    const open=async content=>{const name='Strokes-'+(++openCount)+'.md',chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles({name,mimeType:'text/markdown',buffer:Buffer.from(content)});await waitForAsync(page,async name=>(await fetch('/api/editor/state').then(r=>r.json())).path===name,name);};
    const original=fs.readFileSync(path.join(output,'strokes.md'),'utf8');
    await open(original+'\n\n<!-- top=1 left=18 width=210 height=4 render=truetype modern-font=c64 modern-size=72 -->\nModern / Retro stroke controls\n');
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[0].elements?.some(e=>e.id==='retro-rounded');});
    const select=async id=>{const s=await state(),i=s.slides[0].elements.findIndex(e=>e.id===id);await action({action:'select-element',element:i,elementData:s.slides[0].elements[i]});await page.waitForFunction(i=>document.querySelector('.keynope-canvas-element.active')?.dataset.element===String(i),i);};
    await select('vector-rounded');await page.getByRole('tab',{name:'Style',exact:true}).click();
    const width=page.getByRole('spinbutton',{name:'Selection stroke width',exact:true});await width.waitFor();
    await page.waitForFunction(()=>document.querySelector('[data-selection-property="paint.strokeWidth"]')?.value==='8.25');
    const shape=page.locator('#stage [data-object-id="vector-rounded"]>svg>path').first();await shape.waitFor();
    await shape.evaluate(n=>{window.__strokeOwner=n.parentElement;});
    await page.getByRole('button',{name:'Selection stroke colour',exact:true}).click();
    const picker=page.getByRole('dialog',{name:'Pick color',exact:true});await picker.getByRole('textbox',{name:'HTML color',exact:true}).fill('#FF55AA');await picker.getByRole('button',{name:'Apply',exact:true}).click();
    await page.waitForFunction(()=>document.querySelector('#stage [data-object-id="vector-rounded"]>svg>path')?.getAttribute('stroke')==='#ff55aa');
    await width.fill('12.25');await width.press('Tab');
    await page.waitForFunction(()=>document.querySelector('[data-selection-property="paint.strokeWidth"]')?.value==='12.25'&&document.querySelector('#stage [data-object-id="vector-rounded"]>svg>path')?.getAttribute('stroke-width')==='12.25');
    assert(await shape.evaluate(n=>n.parentElement===window.__strokeOwner),'stroke edit remounted vector owner');
    await page.screenshot({path:path.join(output,'stroke-inspector.png')});
    await page.getByRole('button',{name:'Turn shape stroke off',exact:true}).click();await page.waitForFunction(()=>document.querySelector('[data-selection-property="paint.strokeColor"]')?.textContent==='Off');
    assert.equal(await shape.getAttribute('stroke'),null);
    await page.getByRole('button',{name:'Undo',exact:true}).click();await page.waitForFunction(()=>document.querySelector('[data-selection-property="paint.strokeColor"]')?.textContent==='#ff55aa');
    await page.getByRole('button',{name:'Inherit stroke width',exact:true}).click();await page.waitForFunction(()=>document.querySelector('[data-selection-property="paint.strokeWidth"]')?.value==='2');
    await page.getByRole('button',{name:'Copy style',exact:true}).click();await select('retro-rounded');
    await page.waitForFunction(()=>document.querySelector('[data-selection-property="paint.strokeColor"]')?.textContent==='#55ffff');
    await page.getByRole('button',{name:'Paste style',exact:true}).click();
    await page.waitForFunction(()=>document.querySelector('#stage [data-object-id="retro-rounded"]>svg>path')?.getAttribute('stroke')==='#ff55aa'&&document.querySelector('[data-selection-property="paint.strokeColor"]')?.textContent==='#ff55aa');
    const after=await state(),retro=after.slides[0].elements.find(e=>e.id==='retro-rounded'),q=new URLSearchParams(retro.query);
    assert.equal(q.get('shape-stroke'),'#ff55aa');assert(q.get('shape-label'),'stroke paste lost label');assert.equal(q.get('left'),'138');
    await page.getByRole('button',{name:'Turn shape stroke off',exact:true}).click();await page.waitForFunction(()=>!document.querySelector('#stage [data-object-id="retro-rounded"]>svg>path')?.hasAttribute('stroke'));
    await page.getByRole('button',{name:'Undo',exact:true}).click();await page.waitForFunction(()=>document.querySelector('#stage [data-object-id="retro-rounded"]>svg>path')?.getAttribute('stroke')==='#ff55aa');
    const saved=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Strokes.md'})}).then(r=>r.json()));
    assert(saved.content.includes('shape-stroke=#ff55aa'));fs.writeFileSync(path.join(output,'strokes-browser-saved.md'),saved.content);
    await open(saved.content);await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[0].elements?.some(e=>e.id==='retro-rounded'&&new URLSearchParams(e.query).get('shape-stroke')==='#ff55aa');});
    const s=await state(),indices=[s.slides[0].elements.findIndex(e=>e.id==='retro-rounded'),s.slides[0].elements.findIndex(e=>e.kind==='text')];
    assert(indices.every(i=>i>=0),'missing mixed-selection fixture: '+JSON.stringify(s.slides[0].elements.map(e=>({id:e.id,kind:e.kind,text:e.text}))));
    await action({action:'select-elements',elementIndices:indices,elementsData:indices.map(i=>s.slides[0].elements[i]),element:indices[0]});await page.getByRole('tab',{name:'Style',exact:true}).click();
    await page.waitForFunction(()=>document.querySelector('[data-selection-property="paint.strokeColor"]')?.parentElement.textContent.includes('1 of 2'));
    await page.setViewportSize({width:520,height:900});assert(await page.locator('.keynope-selection-paint').evaluate(n=>n.scrollWidth<=n.clientWidth),'stroke controls overflow narrow inspector');
    await page.screenshot({path:path.join(output,'stroke-inspector-narrow.png')});
    // Actual standalone export paints the same block border, including thin
    // continuous coverage and arbitrary rotation, not an SVG over Retro art.
    const exported=await context.newPage();exported.on('pageerror',e=>errors.push(e.message));await exported.goto('http://127.0.0.1:'+server.address().port+'/strokes.html');
    await exported.waitForFunction(()=>document.querySelectorAll('.keynope-scene-retro-stroke').length===3);
    const coverage=await exported.locator('.keynope-scene-retro-stroke').evaluateAll(nodes=>nodes.map(n=>{const pixels=n.getContext('2d').getImageData(0,0,n.width,n.height).data;let count=0;for(let i=3;i<pixels.length;i+=4)if(pixels[i])count++;return count;}));assert(coverage.every(n=>n>100));
    await exported.screenshot({path:path.join(output,'stroke-modern-retro.png')});
    assert.deepEqual(errors,[]);fs.writeFileSync(path.join(output,'browser-report.json'),JSON.stringify({passed:true,coverage,checks:['actual colour picker','fractional accepted width','retained vector owner','Off/Undo/inherit','style-copy label isolation','concrete vector shapes','Markdown reopen','eligible mixed subset','narrow inspector','legacy standalone rotated block borders']},null,2)+'\n');
    console.log('PASS: production WASM stroke inspector, history/style/inheritance/save, retained vector paint and standalone Retro border');
  }finally{await browser.close();await new Promise(resolve=>server.close(resolve));}
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
