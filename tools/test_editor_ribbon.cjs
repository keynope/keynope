// Build first: tools/build_web_editor.sh /tmp/keynope-ribbon-site/editor
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-ribbon-site');
const server=http.createServer((req,res)=>{
 let p=new URL(req.url,'http://localhost').pathname;if(p.endsWith('/'))p+='index.html';
 const file=path.resolve(root,'.'+p);
 if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}
 res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm','.json':'application/json'})[path.extname(file)]||'application/octet-stream');
 res.end(fs.readFileSync(file));
});
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 const browser=await playwright[process.env.TEST_BROWSER||'chromium'].launch({headless:true,...(process.env.TEST_BROWSER==='webkit'?{}:{executablePath:process.env.CHROME_PATH})});
 try{
  const context=await browser.newContext({viewport:{width:1440,height:1000}});
  await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());
  const page=await context.newPage(),errors=[];page.setDefaultTimeout(15000);page.on('pageerror',e=>{errors.push(e.message);console.error(e.message);});
  page.on('dialog',async dialog=>{console.error('Unexpected dialog: '+dialog.message());await dialog.dismiss();});
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');
  await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:60000});
  const bar=page.locator('.keynope-editor-topbar');
  const tab=name=>bar.getByRole('tab',{name,exact:true});
  const button=name=>bar.getByRole('button',{name,exact:true});
  const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
  const bounds=()=>bar.locator('.keynope-ribbon-quick,.keynope-ribbon-trailing').evaluateAll(nodes=>nodes.map(n=>{const r=n.getBoundingClientRect();return [r.x,r.y,r.width,r.height];}));
  await tab('Insert').waitFor();
  assert(await button('Import image').isVisible());
  assert(await button('New presentation').isVisible());
  assert(await button('Undo').isDisabled());assert(await button('Redo').isDisabled());
  const fixed=await bounds(),height=await bar.evaluate(e=>e.offsetHeight);
  await tab('Slide').click();
  assert(await button('Add slide').isVisible());assert(await button('Import image').isHidden());
  assert.deepEqual(await bounds(),fixed);
  await tab('Insert').click();assert.equal(await button('Add TrueType text (experimental)').count(),0);await button('Add text').click();
  await tab('Text').waitFor();await button('Edit text').waitFor();
  assert(await button('Save presentation').isVisible());assert(await button('Import image').isHidden());
  assert(await button('Undo').isEnabled());assert.deepEqual(await bounds(),fixed);
  const beforeWidth=await state(),beforeElement=beforeWidth.slides[beforeWidth.current].elements[beforeWidth.selected];
  assert.equal(beforeElement.text,'Text');
  const widthInput=bar.getByRole('spinbutton',{name:'TrueType width percent',exact:true});
  assert.equal(await widthInput.inputValue(),'100');
  await widthInput.fill('50');await widthInput.press('Tab');
  await page.waitForFunction(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return new URLSearchParams(s.slides[s.current].elements[s.selected].query).get('ttf-width')==='50';});
  const afterWidth=await state(),afterQuery=new URLSearchParams(afterWidth.slides[afterWidth.current].elements[afterWidth.selected].query);
  for(const key of ['width','height','ttf-size'])assert.equal(afterQuery.get(key),new URLSearchParams(beforeElement.query).get(key),'glyph width preserves '+key);
  assert.equal(await bar.getByRole('spinbutton',{name:'TrueType font size',exact:true}).inputValue(),'97');
  await button('Increase font width').click();
  await page.waitForFunction(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return new URLSearchParams(s.slides[s.current].elements[s.selected].query).get('ttf-width')==='51';});
  await button('Decrease font width').click();
  await page.waitForFunction(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return new URLSearchParams(s.slides[s.current].elements[s.selected].query).get('ttf-width')==='50';});
  for(const [title,size,kind,level] of [['Convert to heading 1',386,'heading',1],['Convert to heading 2',193,'heading',2],['Convert to plain text',97,'text',0]]){
    await button(title).click();
    await page.waitForFunction(async expected=>{const s=await fetch('/api/editor/state').then(r=>r.json()),e=s.slides[s.current].elements[s.selected],q=new URLSearchParams(e.query);return q.get('render')==='truetype'&&Number(q.get('ttf-size'))===expected;},size);
    const s=await state(),e=s.slides[s.current].elements[s.selected],q=new URLSearchParams(e.query);
    assert.equal(e.kind,kind);assert.equal(e.level||0,level);
    for(const key of ['width','height','ttf-width','top','left_pct'])assert.equal(q.get(key),afterQuery.get(key),'TTF preset preserves '+key);
    assert.equal(q.has('fg')||q.has('header'),false,'preset keeps inherited colour');
    assert.equal(await bar.getByRole('spinbutton',{name:'TrueType font size',exact:true}).inputValue(),String(size));
  }
  await tab('Style').click();assert(await button('Add outline').isVisible());
  assert.equal(await page.getByRole('combobox',{name:'Text rendering style',exact:true}).count(),0,'retired text treatments are not offered');
  await button('Add outline').click();await page.waitForTimeout(300);
  assert.equal(await tab('Style').getAttribute('aria-selected'),'true');
  await tab('Arrange').click();assert(await button('Justify text').isHidden());
  await button('Align centre').click();
  await tab('Text').click();assert(await button('Justify text').isVisible());
  fs.mkdirSync('output/ribbon-audit',{recursive:true});await page.screenshot({path:'output/ribbon-audit/ttf-typography.png'});
  await button('Align text right').click();
  await page.waitForFunction(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json()),e=s.slides[s.current].elements[s.selected],q=new URLSearchParams(e.query);return q.get('align')==='center'&&q.get('text-align')==='right';});
  await tab('Arrange').click();await button('Align left').click();
  await page.waitForFunction(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json()),e=s.slides[s.current].elements[s.selected],q=new URLSearchParams(e.query);return q.get('align')==='left'&&q.get('text-align')==='right';});
  await tab('Text').click();await button('Edit text').click();
  const input=page.getByRole('textbox',{name:'Edit element text',exact:true});
  await input.fill('Ribbon editing');
  assert(await tab('Arrange').isDisabled());
  await tab('Style').click();assert.equal(await input.count(),1);
  await button('Commit').click();await input.waitFor({state:'detached'});
  await tab('Insert').waitFor();
  assert((await state()).slides.some(s=>s.elements.some(e=>e.text==='Ribbon editing')));
  await button('Undo').click();await page.waitForTimeout(350);assert(await button('Redo').isEnabled());
  await button('Redo').click();await page.waitForTimeout(350);
  await tab('Slide').click();
  await page.locator('.keynope-settings-menu summary').click();
  await button('Tabs').waitFor({state:'visible'});
  await page.locator('.keynope-settings-menu summary').click();
  for(const width of [1000,800,640]){
   await page.setViewportSize({width,height:800});
   for(const name of ['Save presentation','Undo','Redo','Delete slide']){
    const b=await button(name).boundingBox();assert(b&&b.x>=0&&b.x+b.width<=width,name+' visible at '+width);
   }
   assert.equal(await bar.evaluate(e=>e.offsetHeight),height);
  }
  await page.setViewportSize({width:1440,height:1000});
  await tab('Insert').click();
  fs.mkdirSync('output/ribbon-audit',{recursive:true});await page.screenshot({path:'output/ribbon-audit/insert.png'});
  await button('Add shape').click();
  await page.locator('.keynope-add-shape-menu button').first().click();
  await tab('Shape').waitFor();assert(await tab('Text').count()===0);
  await tab('Style').click();await button('Add outline').waitFor();
  await button('Done').click();await tab('Insert').waitFor();
  await bar.locator('input[type=file][accept="image/*"]').setInputFiles({
   name:'test.png',mimeType:'image/png',
   buffer:Buffer.from(await page.evaluate(()=>{const c=document.createElement('canvas');c.width=c.height=20;const ctx=c.getContext('2d');ctx.fillStyle='red';ctx.fillRect(0,0,20,20);return c.toDataURL().split(',')[1];}),'base64')
  });
  await tab('Image').waitFor();await button('Adjust image').waitFor();
  await button('Adjust image').click();
  const tint=page.getByRole('checkbox',{name:'Enable monochrome tint'});
  await tint.check();
  await page.waitForFunction(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return new URLSearchParams(s.slides[s.current].elements[s.selected].query).get('tint')==='#ffffff';});
  assert(await tint.isVisible(),'adjustments stay open after toggling tint');
  await page.getByRole('button',{name:'Choose monochrome tint',exact:true}).click();
  const picker=page.getByRole('dialog',{name:'Pick color',exact:true});
  await picker.getByRole('textbox',{name:'HTML color',exact:true}).fill('#00ff00');
  await picker.getByRole('button',{name:'Apply',exact:true}).click();
  await page.waitForFunction(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return new URLSearchParams(s.slides[s.current].elements[s.selected].query).get('tint')==='#00ff00';});
  assert(await tint.isVisible(),'adjustments stay open after choosing tint');
  assert(await page.locator('.keynope-visual-panel').evaluate(panel=>panel.scrollWidth<=panel.clientWidth),'image adjustments fit the panel');
  await page.screenshot({path:'output/ribbon-audit/image-tint.png'});
  await tint.uncheck();
  await page.waitForFunction(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return !new URLSearchParams(s.slides[s.current].elements[s.selected].query).has('tint');});
  await button('Adjust image').click();await tint.waitFor({state:'detached'});
  await tab('Arrange').click();await button('Align centre').waitFor();
  await button('Done').click();await tab('Insert').waitFor();
  await page.locator('.keynope-slides-header button').click();
  await page.waitForFunction(()=>!document.querySelector('.keynope-page-number-button').hidden);
  await page.locator('.keynope-page-number-button').waitFor();
  await tab('Slide').click();await button('Appearance').click();
  const appearance=page.locator('.keynope-slide-context');
  await appearance.getByLabel('Font size (TTF)',{exact:true}).fill('110');await appearance.getByLabel('Font size (TTF)',{exact:true}).press('Tab');
  await page.waitForFunction(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[s.current].ttfSize===110;});
  await appearance.getByLabel('Font width',{exact:true}).fill('90');await appearance.getByLabel('Font width',{exact:true}).press('Tab');
  await page.waitForFunction(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[s.current].ttfWidth===90;});
  await page.keyboard.press('Escape');
  await page.locator('.keynope-slides-header button').click();
  await page.waitForFunction(()=>document.querySelector('.keynope-page-number-button').hidden);
  await button('Add text').click();await tab('Text').waitFor();await tab('Text').click();
  assert.equal(await tab('Text').getAttribute('aria-selected'),'true');
  await page.screenshot({path:'output/ribbon-audit/text.png'});
  await tab('Arrange').click();await page.screenshot({path:'output/ribbon-audit/arrange.png'});
  await tab('Text').focus();await tab('Text').press('ArrowRight');
  assert.equal(await tab('Style').getAttribute('aria-selected'),'true');
  await tab('Style').press('Home');
  assert.equal(await tab('Text').getAttribute('aria-selected'),'true');
  await button('Edit text').click();
  await input.fill('Delete while editing');
  await button('Delete element').click();
  await tab('Insert').waitFor();await input.waitFor({state:'detached'});
  assert(!(await state()).slides.some(s=>s.elements.some(e=>e.text==='Delete while editing')));
  assert.deepEqual(errors,[]);
  console.log('PASS: contextual ribbon, fixed controls, editing, history, menus and compact layout');
 }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);server.close();process.exitCode=1;});
