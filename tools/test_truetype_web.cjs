// Run against a locally served, freshly built /editor/.
const assert=require('node:assert/strict');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
 try{
  const page=await browser.newPage({viewport:{width:1440,height:1000}}),errors=[];
  page.on('pageerror',e=>errors.push(e.message));
  await page.goto(process.argv[2]||'http://127.0.0.1:8119/editor/');
  await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:60000});
  const button=name=>page.getByRole('button',{name,exact:true});
  const tab=name=>page.getByRole('tab',{name,exact:true});
  const selected=()=>page.evaluate(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[s.current].elements[s.selected];});
  await button('Add text').click();
  const size=page.getByRole('spinbutton',{name:'TrueType font size',exact:true});await size.waitFor();
  assert.equal(await size.inputValue(),'97');
  const inserted=await selected(),initial=new URLSearchParams(inserted.query);
  assert.equal(inserted.text,'Text');assert(Number(initial.get('height'))<10&&Number(initial.get('width'))<110);
  await size.fill('96');await size.press('Tab');
  await button('Edit text').click();
  const input=page.getByRole('textbox',{name:'Edit element text',exact:true});
  await input.fill('**** Standard text wraps in its own box ****');
  await button('Commit').click();await input.waitFor({state:'detached'});
  const index=await page.evaluate(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[s.current].elements.findIndex(e=>e.text==='**** Standard text wraps in its own box ****');});
  await page.locator('.keynope-canvas-element[data-element="'+index+'"]').click();
  await size.waitFor();
  assert.equal(new URLSearchParams((await selected()).query).has('ttf-weight'),false,'typing stars does not make text bold');
  await button('Bold TrueType text').click();
  assert.equal(new URLSearchParams((await selected()).query).get('ttf-weight'),'bold');
  assert.equal((await selected()).text,'**** Standard text wraps in its own box ****','B preserves literal text');
  await button('Bold TrueType text').click();
  assert.equal(new URLSearchParams((await selected()).query).has('ttf-weight'),false,'B toggles bold off');
  const handle=page.locator('.keynope-canvas-element.active .keynope-resize-handle.se');
  await handle.waitFor({state:'visible'});
  let box;
  for(let attempt=0;attempt<30&&!box;attempt++){
   box=await handle.boundingBox();
   if(!box)await page.waitForTimeout(100);
  }
  assert(box);
  await page.mouse.move(box.x+4,box.y+4);await page.mouse.down();await page.mouse.move(box.x+100,box.y+60,{steps:8});await page.mouse.up();
  await page.waitForTimeout(500);
  const expanded=new URLSearchParams((await selected()).query);
  assert.equal(expanded.get('ttf-size'),'96');
  assert(Number(expanded.get('width'))>Number(initial.get('width')));
  assert(Number(expanded.get('height'))>Number(initial.get('height')));
  await tab('Style').click();
  for(const mode of ['braille','ascii','dense','blocks','']){
   await page.getByRole('combobox',{name:'Text rendering style',exact:true}).selectOption(mode);
   await page.waitForTimeout(150);
   const q=new URLSearchParams((await selected()).query);
   assert.equal(q.get('render'),'truetype');assert.equal(q.get('glyph')||'',mode);
   for(const key of ['ttf-size','width','height'])assert.equal(q.get(key),expanded.get(key));
  }
  await button('Add gradient').click();await page.getByRole('menuitemradio',{name:'Horizontal',exact:true}).click();
  await button('Add shadow').click();await page.getByRole('menuitemradio',{name:'Soft',exact:true}).click();
  await button('Add outline').click();await button('Enable see through').click();
  await tab('Text').click();await button('Justify text').click();
  await tab('Arrange').click();await button('Rotate').click();
  const q=new URLSearchParams((await selected()).query);
  assert.equal(q.get('text-align'),'justify');assert.equal(q.get('orientation'),'cw');
  assert.equal(q.get('transparent'),'1');assert.equal(q.get('shadow'),'soft');assert(q.has('outline'));assert(q.has('gradient-start'));
  await tab('Text').click();await button('Convert to code block').click();
  assert.equal((await selected()).kind,'code');assert.equal(new URLSearchParams((await selected()).query).get('render'),'truetype');
  await button('Convert to bullet points').click();assert.equal((await selected()).kind,'bullet');
  await button('Convert to plain text').click();assert.equal(new URLSearchParams((await selected()).query).get('ttf-size'),'97');
  await button('Delete element').click();await button('Undo').click();
  assert.deepEqual(errors,[]);
  console.log('WASM: standard text, wrapping box, styles, effects, rotation, justification, code/bullet presets, delete and undo passed.');
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
