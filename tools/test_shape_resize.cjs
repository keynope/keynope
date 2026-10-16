const {waitForAsync}=require("./wait_for_async.cjs");
// Build first: make web-editor. Exercises precise sub-cell drags in both engines.
const fs=require('node:fs'),path=require('node:path'),http=require('node:http'),assert=require('node:assert/strict');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-site');
const server=http.createServer((req,res)=>{
 let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';
 const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);res.end();return;}
 res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm','.json':'application/json'})[path.extname(file)]||'application/octet-stream');fs.createReadStream(file).pipe(res);
});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));
 for(const engine of ['chromium','webkit']){
  const browser=await pw[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
  try{
   const page=await browser.newPage({viewport:{width:1600,height:1100}}),errors=[];
   await page.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
   page.on('pageerror',e=>errors.push(e.message));
   page.on('dialog',d=>d.accept());
   await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');
   await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:120000});
   const b=name=>page.getByRole('button',{name,exact:true});
   const chooser=page.waitForEvent('filechooser');await b('Open a Keynope or PowerPoint presentation').click();await(await chooser).setFiles({name:'Resize proof.md',mimeType:'text/markdown',buffer:Buffer.from('<!-- keynope width=245 height=56 -->\n\nShape resize proof\n')});
   await waitForAsync(page,async()=>(await fetch('/api/editor/state').then(r=>r.json())).path.endsWith('Resize proof.md'));
   await b('Insert').click();await b('Add shape').click();
   await page.locator('.keynope-add-shape-menu button').first().click();
   await page.evaluate(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json()),e=s.slides[s.current].elements[s.selected];await editorAction({action:'set-selection-geometry',sceneRevision:s.version,sceneMaster:false,slide:s.current,objectIds:[e.id],geometry:{x:80,y:20,width:35,height:12},geometryResize:true});});
   const selected=()=>page.evaluate(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[s.current].elements[s.selected];});
   const hit=page.locator('.keynope-canvas-element.active');
   await hit.waitFor();
   await page.evaluate(()=>{window.shapeResizePointers={};window.addEventListener('pointerdown',e=>{shapeResizePointers={down:{x:e.clientX,y:e.clientY,id:e.pointerId}};},true);window.addEventListener('pointerup',e=>{if(shapeResizePointers.down?.id===e.pointerId)shapeResizePointers.up={x:e.clientX,y:e.clientY};},true);});
   for(const corner of ['se','nw','sw','ne','se']){
    if(corner==='ne') {
      await page.getByRole('tab',{name:'Style',exact:true}).click();
      assert.equal(await b('Enable see through').isVisible(),false,'Vector shape exposes unsupported sampled blending');
      const opacity=page.getByRole('spinbutton',{name:'Object opacity',exact:true});await opacity.fill('50');await opacity.press('Tab');
      await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return new URLSearchParams(s.slides[s.current].elements[s.selected].query).get('modern-opacity')==='0.5';});
    }
    const element=await selected(),query=new URLSearchParams(element.query),before=await hit.boundingBox();
    const overlay=await page.locator('.keynope-canvas-overlay').boundingBox();
    const cols=245,rows=56,cw=overlay.width/cols,ch=overlay.height/rows;
    const handle=await hit.locator('[data-corner="'+corner+'"]').boundingBox();
    const x=handle.x+handle.width/2,y=handle.y+handle.height/2;
    const dx=(corner.includes('w')?-.5:.5)*cw,dy=(corner.includes('n')?-.5:.5)*ch;
    await page.mouse.move(x,y);await page.mouse.down();await page.mouse.move(x+dx,y+dy);
    await page.waitForTimeout(150);await page.mouse.up();
    // Browser engines quantize client coordinates differently. Assert the
    // precise authored delta from the delivered pointer events, not a forced
    // half-cell rounding which the current shape tools deliberately removed.
    const points=await page.evaluate(()=>shapeResizePointers);assert(points.down&&points.up);
    const width=Number(query.get('width')||12)+(points.up.x-points.down.x)/cw*(corner.includes('w')?-1:1),height=Number(query.get('height')||6)+(points.up.y-points.down.y)/ch*(corner.includes('n')?-1:1);
    try { await waitForAsync(page,async({width,height})=>{const s=await fetch('/api/editor/state').then(r=>r.json()),q=new URLSearchParams(s.slides[s.current].elements[s.selected].query);return Math.abs(Number(q.get('width'))-width)<1e-8&&Math.abs(Number(q.get('height'))-height)<1e-8;},{width,height},{timeout:5000}); }
    catch(error){console.error('Resize diagnostic',JSON.stringify({engine,corner,expected:{width,height},points,before:element,after:await selected(),errors}));await page.screenshot({path:'/tmp/keynope-shape-resize-failure.png'});throw error;}
    await page.waitForTimeout(150);
    const after=await hit.boundingBox();
    const fixedX=r=>corner.includes('w')?r.x+r.width:r.x;
    const fixedY=r=>corner.includes('n')?r.y+r.height:r.y;
    assert(Math.abs(fixedX(after)-fixedX(before))<.15*cw,corner+' opposite x moved');
    assert(Math.abs(fixedY(after)-fixedY(before))<.15*ch,corner+' opposite y moved');
   }
   fs.mkdirSync('output/shape-resize-tests',{recursive:true});await page.screenshot({path:'output/shape-resize-tests/'+engine+'.png'});
   const final=await selected();await b('Undo').click();await page.waitForTimeout(150);await b('Redo').click();
   await waitForAsync(page,async query=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[s.current].elements.some(e=>e.query===query);},final.query);
   assert.deepEqual(errors,[]);console.log('PASS '+engine+': all four precise sub-cell corners, supported opacity, fixed opposite anchors, undo/redo');
  }finally{await browser.close();}
 }
})().catch(e=>{console.error(e);process.exitCode=1;}).finally(()=>server.close());
