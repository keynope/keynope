// Real shared inspector/style-transfer controls with fixed detached defaults.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const {waitForAsync}=require('./wait_for_async.cjs');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),output=path.resolve(process.env.TEST_OUTPUT||'/tmp/keynope-shape-style-defaults');
const server=http.createServer((req,res)=>{
 let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';
 const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}
 res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));
});
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));fs.mkdirSync(output,{recursive:true});
 for(const engine of ['chromium','webkit']){
  const browser=await playwright[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
  try{
   const context=await browser.newContext({viewport:{width:1440,height:1000}});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
   const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
   await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
   const content=`<!-- keynope width=245 height=56 -->

<!-- top=8 left=10 width=70 height=24 shape=square fg=#55aaff default-stroke=#aaff55 default-stroke-width=0 -->
[shape:square]

<!-- top=8 left=120 width=70 height=24 shape=circle fg=#ff55aa default-stroke=#55ffff default-stroke-width=3 shape-stroke=#ff0055 shape-stroke-width=7 -->
[shape:circle]
`;
   const picker=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await picker).setFiles({name:'Shape style defaults.md',mimeType:'text/markdown',buffer:Buffer.from(content)});
   const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
   await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[0].elements.length===2&&s.slides[0].elements.every(e=>e.kind==='shape');});
   const initial=await state(),source=initial.slides[0].elements.findIndex(e=>new URLSearchParams(e.query).get('shape')==='square'),target=1-source;
   const select=async index=>{await page.locator('.keynope-canvas-element[data-element="'+index+'"]').click();await page.getByRole('tab',{name:'Style',exact:true}).click();};
   const width=page.getByRole('spinbutton',{name:'Selection stroke width',exact:true});
   await select(source);assert.equal(await width.inputValue(),'0');
   await page.getByRole('button',{name:'Copy style',exact:true}).click();
   await select(target);assert.equal(await width.inputValue(),'7');
   await page.getByRole('button',{name:'Paste style',exact:true}).click();
   await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities.properties['paint.strokeWidth'].value===0;});
   assert.equal(await width.inputValue(),'0');
   const pasted=await state(),q=new URLSearchParams(pasted.slides[0].elements[target].query);
   assert.equal(q.get('shape-stroke'),'#aaff55');assert.equal(q.get('shape-stroke-width'),'0');assert.equal(q.get('shape'),'circle');assert.equal(q.get('left'),'120');
   await page.getByRole('button',{name:'Undo',exact:true}).click();await select(target);assert.equal(await width.inputValue(),'7');
   await page.getByRole('button',{name:'Redo',exact:true}).click();await select(target);assert.equal(await width.inputValue(),'0');
   await page.getByRole('button',{name:'Reset style',exact:true}).click();await select(target);assert.equal(await width.inputValue(),'3');
   const scene=await page.evaluate(()=>fetch('/api/editor/scene?slide=0').then(r=>r.json()));
   const object=scene.objects.find(o=>o.id===initial.slides[0].elements[target].id);assert.equal(object.paint.stroke,'#55ffff');assert.equal(object.paint.strokeWidth,3);
   await page.screenshot({path:path.join(output,engine+'.png')});assert.deepEqual(errors,[]);
   console.log('PASS '+engine+': inherited zero-width inspector, resolved stroke Copy/Paste, Undo/Redo, retained geometry and Reset to concrete defaults');
  }finally{await browser.close();}
 }
 server.close();
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
