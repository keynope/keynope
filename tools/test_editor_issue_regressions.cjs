// Build a fresh TEST_SITE first. Exercises the real editor without cloud writes.
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
  const page=await browser.newPage({viewport:{width:1440,height:1000}}),errors=[];
  page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
  await page.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');
  await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const chooser=page.waitForEvent('filechooser');
  await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();
  await(await chooser).setFiles({name:'issue-regressions.md',mimeType:'text/markdown',buffer:Buffer.from('# Heading\n\nBody text\n\n[shape:square]\n')});
  await page.waitForFunction(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.path.endsWith('issue-regressions.md');});
  const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
  const count=(await state()).slides[0].elements.length;assert.equal(count,3);
  await page.evaluate(()=>document.activeElement?.blur());
  await page.keyboard.press('Control+a');
  await page.waitForFunction(n=>fetch('/api/editor/state').then(r=>r.json()).then(s=>s.selection.length===n),count);
  // A text field keeps browser Select All, with the canvas selection untouched.
  await page.evaluate(()=>{const input=document.createElement('input');input.id='selection-test';input.value='Select this text';document.body.append(input);input.focus();});
  await page.keyboard.press(process.platform==='darwin'?'Meta+a':'Control+a');
  assert.equal(await page.locator('#selection-test').evaluate(n=>n.selectionEnd-n.selectionStart),16);
  assert.equal((await state()).selection.length,count);
  await page.locator('#selection-test').evaluate(n=>n.remove());
  await page.keyboard.press('Control+Shift+d');
  await page.waitForFunction(()=>fetch('/api/editor/state').then(r=>r.json()).then(s=>s.slides.length===2));
  assert.equal((await state()).slides[1].elements.length,count);
  await page.keyboard.press('Control+z');
  await page.waitForFunction(()=>fetch('/api/editor/state').then(r=>r.json()).then(s=>s.slides.length===1));
  await page.getByRole('tab',{name:'Slide',exact:true}).click();
  await page.getByRole('button',{name:'Duplicate slide',exact:true}).click();
  await page.waitForFunction(()=>fetch('/api/editor/state').then(r=>r.json()).then(s=>s.slides.length===2));
  await page.evaluate(()=>keynopeWorkspaceCommand('rulers'));
  await page.locator('.keynope-ruler-horizontal').waitFor({state:'visible'});
  const canvas=await page.locator('#presenter-canvas').boundingBox();
  await page.mouse.move(canvas.x+canvas.width/2,canvas.y+canvas.height/2);
  await page.waitForFunction(()=>document.querySelectorAll('.keynope-ruler-position').length===2);
  const coordinates=await page.locator('.keynope-ruler-position').allTextContents();
  const units=await page.evaluate(()=>{const s=getComputedStyle(document.documentElement);return ['--cols','--rows'].map(k=>Number(s.getPropertyValue(k)));});
  coordinates.forEach((value,i)=>assert(Math.abs(Number(value)-units[i]/2)<.6));
  const horizontal=await page.locator('.keynope-ruler-horizontal').boundingBox();
  assert(Math.abs(horizontal.x-canvas.x)<1);assert(horizontal.y+horizontal.height<=canvas.y+1);
  const beforeGuides=await state();
  const drag=async(from,to)=>{await page.mouse.move(from.x,from.y);await page.mouse.down();await page.mouse.move(to.x,to.y,{steps:12});await page.mouse.up();};
  const yGuide=page.getByRole('slider',{name:'Horizontal alignment guide'}),xGuide=page.getByRole('slider',{name:'Vertical alignment guide'});
  await drag({x:horizontal.x+horizontal.width*.4,y:horizontal.y+10},{x:canvas.x+canvas.width*.4,y:canvas.y+canvas.height*.35});
  await yGuide.waitFor();
  assert(Math.abs(Number(await yGuide.getAttribute('aria-valuenow'))-units[1]*.35)<.2);
  const vertical=await page.locator('.keynope-ruler-vertical').boundingBox();
  await drag({x:vertical.x+10,y:vertical.y+vertical.height*.6},{x:canvas.x+canvas.width*.65,y:canvas.y+canvas.height*.6});
  await xGuide.waitFor();
  assert(Math.abs(Number(await xGuide.getAttribute('aria-valuenow'))-units[0]*.65)<.3);
  const guidePosition=async(locator)=>{const b=await locator.boundingBox();return {x:b.x+b.width/2,y:b.y+b.height/2};};
  const yBefore=Number(await yGuide.getAttribute('aria-valuenow'));
  const p=await guidePosition(yGuide);
  await drag(p,{x:p.x,y:canvas.y+canvas.height*.45});
  const yAfter=Number(await yGuide.getAttribute('aria-valuenow'));assert(yAfter>yBefore);
  // Escape restores an existing guide, or discards a guide pulled from a ruler.
  const existing=await guidePosition(yGuide);await page.mouse.move(existing.x,existing.y);await page.mouse.down();await page.mouse.move(existing.x,existing.y+30);await page.keyboard.press('Escape');await page.mouse.up();
  assert.equal(Number(await yGuide.getAttribute('aria-valuenow')),yAfter);
  await page.mouse.move(horizontal.x+30,horizontal.y+10);await page.mouse.down();await page.mouse.move(canvas.x+30,canvas.y+40);await page.keyboard.press('Escape');await page.mouse.up();
  assert.equal(await page.locator('.keynope-ruler-guide').count(),2);
  await xGuide.focus();const xBefore=Number(await xGuide.getAttribute('aria-valuenow'));await page.keyboard.press('ArrowRight');assert.equal(Number(await xGuide.getAttribute('aria-valuenow')),xBefore+1);
  await page.evaluate(()=>keynopeWorkspaceCommand('zoom-in'));
  const moved=await page.locator('#presenter-canvas').boundingBox();assert(moved.width>canvas.width);
  const zoomY=await guidePosition(yGuide),zoomX=await guidePosition(xGuide);
  assert(Math.abs(zoomY.y-(moved.y+yAfter/units[1]*moved.height))<1);
  assert(Math.abs(zoomX.x-(moved.x+(xBefore+1)/units[0]*moved.width))<1);
  await page.locator('#stage').dispatchEvent('wheel',{deltaX:30,deltaY:25,bubbles:true});
  const panned=await page.locator('#presenter-canvas').boundingBox();assert(panned.x!==moved.x||panned.y!==moved.y);
  const panY=await guidePosition(yGuide);assert(Math.abs(panY.y-(panned.y+yAfter/units[1]*panned.height))<1);
  await page.evaluate(()=>keynopeWorkspaceCommand('fit'));
  await page.evaluate(()=>keynopeWorkspaceCommand('rulers'));await page.locator('.keynope-ruler-guides').waitFor({state:'hidden'});
  await page.evaluate(()=>keynopeWorkspaceCommand('rulers'));await yGuide.waitFor();assert.equal(Number(await yGuide.getAttribute('aria-valuenow')),yAfter);
  const output=process.env.TEST_OUTPUT;if(output){fs.mkdirSync(output,{recursive:true});await page.screenshot({path:path.join(output,engine+'-rulers.png')});}
  const remove=await guidePosition(yGuide);await drag(remove,{x:canvas.x+canvas.width*.5,y:horizontal.y+5});assert.equal(await yGuide.count(),0);
  await xGuide.focus();await page.keyboard.press('Delete');assert.equal(await xGuide.count(),0);
  const addPair=async()=>{
    await drag({x:horizontal.x+40,y:horizontal.y+10},{x:canvas.x+40,y:canvas.y+canvas.height*.35});
    await drag({x:vertical.x+10,y:vertical.y+40},{x:canvas.x+canvas.width*.65,y:canvas.y+40});
    assert.equal(await page.locator('.keynope-ruler-guide').count(),2);
  };
  await addPair();
  await yGuide.click();assert.equal(await yGuide.count(),0);assert.equal(await xGuide.count(),1);
  await drag({x:horizontal.x+40,y:horizontal.y+10},{x:canvas.x+40,y:canvas.y+canvas.height*.35});
  // Small pointer jitter is still a click, and must not delete another guide.
  const clickPoint=await guidePosition(xGuide);
  await page.mouse.move(clickPoint.x,clickPoint.y);await page.mouse.down();await page.mouse.move(clickPoint.x+2,clickPoint.y+1);await page.mouse.up();
  assert.equal(await xGuide.count(),0);assert.equal(await yGuide.count(),1);
  await page.locator('.keynope-ruler-horizontal').click({position:{x:40,y:10}});
  assert.equal(await page.locator('.keynope-ruler-guide').count(),0);
  await addPair();
  await page.locator('.keynope-ruler-vertical').click({position:{x:10,y:40}});
  assert.equal(await page.locator('.keynope-ruler-guide').count(),0);
  const afterGuides=await state();assert.equal(afterGuides.version,beforeGuides.version);assert.deepEqual(afterGuides.slides,beforeGuides.slides);assert.deepEqual(afterGuides.selection,beforeGuides.selection);
  await page.reload();await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  assert.equal(await page.evaluate(()=>keynopeWorkspaceState().rulers),true);
  assert.deepEqual(errors,[]);
  console.log('PASS '+engine+': canvas/text Select All, duplication, guide dragging/cancellation, single-click removal/clear, zoom/pan, keyboard, and unchanged document state.');
 }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);server.close();process.exitCode=1});
