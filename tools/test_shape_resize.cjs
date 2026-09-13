// Build first: make web-editor. Exercises real half-cell drags in both engines.
const fs=require('node:fs'),path=require('node:path'),http=require('node:http'),assert=require('node:assert/strict');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'terraform/site');
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
   page.on('pageerror',e=>errors.push(e.message));
   await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');
   await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:120000});
   const b=name=>page.getByRole('button',{name,exact:true});
   await page.getByRole('tab',{name:'Insert',exact:true}).click();await b('Add shape').click();
   await page.locator('.keynope-add-shape-menu button').first().click();
   const selected=()=>page.evaluate(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[s.current].elements[s.selected];});
   const hit=page.locator('.keynope-canvas-element.active');
   await hit.waitFor();
   for(const corner of ['se','nw','sw','ne','se']){
    if(corner==='ne') { await page.getByRole('tab',{name:'Style',exact:true}).click();await b('Enable see through').click();await b('Disable see through').waitFor(); }
    const element=await selected(),query=new URLSearchParams(element.query),before=await hit.boundingBox();
    const overlay=await page.locator('.keynope-canvas-overlay').boundingBox();
    const cols=245,rows=56,cw=overlay.width/cols,ch=overlay.height/rows;
    const handle=await hit.locator('[data-corner="'+corner+'"]').boundingBox();
    const x=handle.x+handle.width/2,y=handle.y+handle.height/2;
    const dx=(corner.includes('w')?-.5:.5)*cw,dy=(corner.includes('n')?-.5:.5)*ch;
    await page.mouse.move(x,y);await page.mouse.down();await page.mouse.move(x+dx,y+dy);
    await page.waitForTimeout(150);await page.mouse.up();
    const width=Number(query.get('width')||12)+.5,height=Number(query.get('height')||6)+.5;
    await page.waitForFunction(async({width,height})=>{const s=await fetch('/api/editor/state').then(r=>r.json()),q=new URLSearchParams(s.slides[s.current].elements[s.selected].query);return Number(q.get('width'))===width&&Number(q.get('height'))===height;},{width,height});
    await page.waitForTimeout(150);
    const after=await hit.boundingBox();
    const fixedX=r=>corner.includes('w')?r.x+r.width:r.x;
    const fixedY=r=>corner.includes('n')?r.y+r.height:r.y;
    assert(Math.abs(fixedX(after)-fixedX(before))<.15*cw,corner+' opposite x moved');
    assert(Math.abs(fixedY(after)-fixedY(before))<.15*ch,corner+' opposite y moved');
   }
   fs.mkdirSync('output/shape-resize-tests',{recursive:true});await page.screenshot({path:'output/shape-resize-tests/'+engine+'.png'});
   const final=await selected();await b('Undo').click();await page.waitForTimeout(150);await b('Redo').click();
   await page.waitForFunction(async query=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[s.current].elements.some(e=>e.query===query);},final.query);
   assert.deepEqual(errors,[]);console.log('PASS '+engine+': all four half-cell corners, fixed opposite anchors, undo/redo');
  }finally{await browser.close();}
 }
})().catch(e=>{console.error(e);process.exitCode=1;}).finally(()=>server.close());
