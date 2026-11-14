// Real shared editor, synthetic deck, no cloud or desktop-app dependencies.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-issue-fixes/site');
const server=http.createServer((req,res)=>{
 let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';
 const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}
 res.setHeader('Content-Type',({'.html':'text/html','.js':'text/javascript','.css':'text/css','.wasm':'application/wasm','.json':'application/json'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));
});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));
 const engine=process.env.TEST_BROWSER||'chromium',browser=await pw[engine].launch({headless:true});
 try{
  const page=await browser.newPage({viewport:{width:1440,height:1000}}),errors=[];page.setDefaultTimeout(20000);
  page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
  await page.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');
  await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();
  await(await chooser).setFiles({name:'context-menus.md',mimeType:'text/markdown',buffer:Buffer.from(`<!-- keynope width=245 height=56 -->

<!-- top=5 left_pct=0.1 width=60 height=8 render=truetype modern-size=80 modern-font=sans -->
# First

<!-- top=20 left_pct=0.1 width=25 height=10 -->
[shape:square]

---

# Second

---

# Third
`)});
  const state=()=>page.evaluate(()=>fetch('/api/editor/state?deferReload=1').then(r=>r.json()));
  const action=data=>page.evaluate(data=>editorAction(data),data);
  const current=async n=>page.waitForFunction(n=>fetch('/api/editor/state').then(r=>r.json()).then(s=>s.current===n),n);
  await page.waitForFunction(()=>fetch('/api/editor/state').then(r=>r.json()).then(s=>s.path==='context-menus.md'));
  await page.locator('#stage').focus();
  for(const [key,target]of [['ArrowDown',1],['ArrowDown',2],['ArrowUp',1],['ArrowLeft',0],['ArrowRight',1],['ArrowUp',0]]){await page.keyboard.press(key);await current(target);}
  let s=await state();const shapeIndex=s.slides[0].elements.findIndex(e=>e.kind==='shape');
  await action({action:'select-element',element:shapeIndex});await page.locator('#stage').focus();
  await page.keyboard.press('ArrowDown');await current(0);
  await page.waitForFunction(({i,before})=>fetch('/api/editor/state').then(r=>r.json()).then(s=>s.slides[0].elements[i].query!==before),{i:shapeIndex,before:s.slides[0].elements[shapeIndex].query});
  const canvas=await page.locator('#presenter-canvas').boundingBox();
  const blank={x:canvas.x+canvas.width*.9,y:canvas.y+canvas.height*.9};
  await page.mouse.click(blank.x,blank.y);
  await page.waitForFunction(()=>fetch('/api/editor/state').then(r=>r.json()).then(s=>s.selected===-1));
  assert.equal(await page.getByRole('tab',{name:'Slide',exact:true}).getAttribute('aria-selected'),'true');
  // Right-click navigates to its target and opens commands, not a settings form.
  const slide=page.locator('.keynope-slide-item[data-master-index="1"]');await slide.click({button:'right'});
  const slideMenu=page.getByRole('menu',{name:'Slide actions',exact:true});await slideMenu.waitFor();await current(1);
  assert.equal(await slideMenu.locator('input,select').count(),0);
  assert.equal(await slideMenu.getByRole('menuitem',{name:'Duplicate slide',exact:true}).count(),1);
  await slideMenu.getByRole('menuitem',{name:'Layout',exact:true}).focus();await page.keyboard.press('ArrowRight');
  const layouts=page.getByRole('menu',{name:'Layout',exact:true});await layouts.waitFor();assert(await layouts.getByRole('menuitemradio').count()>0);
  await page.keyboard.press('ArrowLeft');assert.equal(await layouts.count(),0);
  await slideMenu.getByRole('menuitem',{name:'Layout',exact:true}).click();
  await layouts.getByRole('menuitemradio',{name:'Blank',exact:true}).click();
  await page.waitForFunction(()=>fetch('/api/editor/state').then(r=>r.json()).then(s=>s.slides[1].layoutId==='blank'));
  await slide.click({button:'right'});await slideMenu.waitFor();
  await slideMenu.getByRole('menuitem',{name:'Duplicate slide',exact:true}).click();
  await page.waitForFunction(()=>fetch('/api/editor/state').then(r=>r.json()).then(s=>s.slides.length===4));
  s=await state();assert.equal(s.current,2);assert.equal(s.slides[1].elements[0].text,s.slides[2].elements[0].text);
  await page.locator('.keynope-slide-item[data-master-index="2"]').click({button:'right'});
  await slideMenu.getByRole('menuitem',{name:'Delete slide',exact:true}).click();
  await page.waitForFunction(()=>fetch('/api/editor/state').then(r=>r.json()).then(s=>s.slides.length===3));
  await action({action:'select-slide',slide:0});
  s=await state();let textIndex=s.slides[0].elements.findIndex(e=>e.kind==='heading');const textID=s.slides[0].elements[textIndex].id;
  const hit=i=>page.locator('.keynope-canvas-element[data-element="'+i+'"]');
  await hit(textIndex).click({button:'right'});
  const objectMenu=page.getByRole('menu',{name:'Object actions',exact:true});await objectMenu.waitFor();
  assert.equal(await objectMenu.getByRole('menuitem',{name:'Edit text',exact:true}).count(),1);
  assert.equal(await objectMenu.locator('input,select').count(),0);
  const output=process.env.TEST_OUTPUT;if(output){fs.mkdirSync(output,{recursive:true});await page.screenshot({path:path.join(output,engine+'-text-menu.png')});}
  await page.keyboard.press('Escape');assert.equal(await objectMenu.count(),0);
  await page.locator('#stage').focus();await page.keyboard.press('Shift+F10');await objectMenu.waitFor();
  await objectMenu.getByRole('menuitem',{name:'Arrange',exact:true}).click();
  await page.getByRole('menu',{name:'Arrange',exact:true}).getByRole('menuitem',{name:'Bring to front',exact:true}).click();
  await page.waitForFunction(i=>fetch('/api/editor/state').then(r=>r.json()).then(s=>new URLSearchParams(s.slides[0].elements[i].query).has('z-index')),textIndex);
  // Existing multi-selection is retained when right-clicking one member.
  s=await state();await action({action:'select-elements',elementIndices:[0,1],elementsData:s.slides[0].elements.map(e=>({id:e.id}))});
  await hit(textIndex).click({button:'right'});await objectMenu.waitFor();assert.equal((await state()).selection.length,2);
  await objectMenu.getByRole('menuitem',{name:'Group',exact:true}).click();
  await page.waitForFunction(()=>fetch('/api/editor/state').then(r=>r.json()).then(s=>s.slides[0].elements.every(e=>new URLSearchParams(e.query).get('group'))));
  await page.locator('.keynope-group-member-hit').first().click({button:'right'});await objectMenu.getByRole('menuitem',{name:'Ungroup',exact:true}).click();
  await page.waitForFunction(()=>fetch('/api/editor/state').then(r=>r.json()).then(s=>s.slides[0].elements.every(e=>!new URLSearchParams(e.query).get('group'))));
  // Upload an owned one-pixel PNG to exercise the image menu with real media.
  await page.evaluate(async()=>{const canvas=document.createElement('canvas');canvas.width=canvas.height=16;const ctx=canvas.getContext('2d');ctx.fillStyle='#dc5a25';ctx.fillRect(0,0,16,16);const png=await new Promise(resolve=>canvas.toBlob(resolve,'image/png'));const form=new FormData();form.append('image',png,'pixel.png');const r=await fetch('/api/editor/upload',{method:'POST',body:form});if(!r.ok)throw Error(await r.text());});
  await page.waitForFunction(()=>fetch('/api/editor/state').then(r=>r.json()).then(s=>s.slides[0].elements.some(e=>e.kind==='image')));
  s=await state();const imageIndex=s.slides[0].elements.findIndex(e=>e.kind==='image');assert(imageIndex>=0);
  textIndex=s.slides[0].elements.findIndex(e=>e.id===textID);
  await action({action:'select-element',element:imageIndex});await page.locator('#stage').focus();await page.keyboard.press('Shift+F10');await objectMenu.waitFor();
  assert(await objectMenu.getByRole('menuitem',{name:'Crop image…',exact:true}).isEnabled());
  assert.equal(await objectMenu.getByRole('menuitem',{name:'Edit text',exact:true}).count(),0);
  if(output)await page.screenshot({path:path.join(output,engine+'-image-menu.png')});
  await objectMenu.getByRole('menuitem',{name:'Format image…',exact:true}).click();
  assert.equal(await page.getByRole('tab',{name:'Style',exact:true}).getAttribute('aria-selected'),'true');
  await page.mouse.click(blank.x,blank.y);await page.waitForFunction(()=>fetch('/api/editor/state').then(r=>r.json()).then(s=>s.selected===-1));
  assert.equal(await page.getByRole('tab',{name:'Slide',exact:true}).getAttribute('aria-selected'),'true');
  // Editing text and then clicking blank canvas commits the draft and returns
  // the inspector to Slide, rather than leaving text settings active.
  await hit(textIndex).click({button:'right'});await objectMenu.getByRole('menuitem',{name:'Edit text',exact:true}).click();
  const textDialog=page.locator('dialog[data-keynope-scene-dialog="text"]');await textDialog.waitFor();
  await textDialog.getByRole('textbox',{name:'Edit slide text',exact:true}).fill('Edited through context menu');
  await page.mouse.click(blank.x,blank.y);await textDialog.waitFor({state:'detached'});
  await page.waitForFunction(()=>fetch('/api/editor/state').then(r=>r.json()).then(s=>s.selected===-1&&s.slides[0].elements.some(e=>e.text==='Edited through context menu')));
  assert.equal(await page.getByRole('tab',{name:'Slide',exact:true}).getAttribute('aria-selected'),'true');
  await page.mouse.click(blank.x,blank.y,{button:'right'});await slideMenu.waitFor();
  await page.keyboard.press('Escape');assert.equal(await slideMenu.count(),0);
  // A stale menu disappears on document mutation instead of acting on a new target.
  await hit(textIndex).click({button:'right'});await objectMenu.waitFor();await action({action:'select-slide',slide:1});assert.equal(await objectMenu.count(),0);
  assert.deepEqual(errors,[]);
  console.log('PASS '+engine+': four-arrow slide navigation, blank-canvas deselection/Slide inspector, slide/object/image menus, layout/arrange submenus, grouping, keyboard and stale-menu guards.');
 }finally{await browser.close();server.close();}
})().catch(error=>{console.error(error);server.close();process.exitCode=1});
