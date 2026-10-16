// Production file chooser -> owned copy endpoint -> cross-page paste/Undo/Redo.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http'),{execFileSync}=require('node:child_process');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright'),{waitForAsync}=require('./wait_for_async.cjs');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),engine=process.env.TEST_BROWSER||'webkit',proof='/tmp/keynope-clipboard-geometry-'+engine;
execFileSync('go',['test','.','-run','^TestClipboardGeometryBrowserFixtures$','-count=1'],{env:{...process.env,GOCACHE:process.env.GOCACHE||'/tmp/keynope-go-cache',KEYNOPE_CLIPBOARD_GEOMETRY_PROOF:proof},stdio:'pipe'});
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const browser=await pw[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
 try{
  const page=await browser.newPage({viewport:{width:1600,height:1000}}),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());await page.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  async function open(name,content=fs.readFileSync(path.join(proof,name))){const picker=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await picker).setFiles({name,mimeType:'text/markdown',buffer:Buffer.from(content)});await waitForAsync(page,async name=>(await fetch('/api/editor/state').then(r=>r.json())).path.endsWith(name),name);}
  await open('source.md');
  const copied=await page.evaluate(async()=>{const state=await fetch('/api/editor/state').then(r=>r.json()),response=await fetch('/api/editor/clipboard',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({version:state.version,ids:state.slides[0].elements.map(e=>e.id)})});if(!response.ok)throw Error(await response.text());return {bundle:await response.json(),objects:deck.pages[0].scene.objects};});
  assert.equal(copied.bundle.geometry.width,1440);assert.equal(copied.bundle.geometry.height,1080);assert(!JSON.stringify(copied.bundle).includes('PRIVATE-NOTES'));
  await open('target.md');await page.evaluate(bundle=>editorAction({action:'paste-objects',clipboard:bundle}),copied.bundle);
  await waitForAsync(page,async()=>deck.pages[0].scene.objects.length===7);
  const pasted=await page.evaluate(()=>deck.pages[0].scene.objects),original=new Map(copied.objects.map(o=>[o.name,o]));
  for(const object of pasted){const old=original.get(object.name);assert(old);assert.notEqual(object.id,old.id);assert(object.group&&object.group!==old.group);if(object.kind==='connector'){for(const [a,b]of [[object.points[0],old.points[0]],[object.points.at(-1),old.points.at(-1)]])assert(Math.hypot(a.x-b.x,a.y-b.y)<1e-5);continue;}for(const axis of ['x','y','width','height'])assert(Math.abs(object.bounds[axis]-old.bounds[axis])<1e-5,object.name+' '+axis);if(object.text)assert.deepEqual(object.text,old.text);}
  await page.locator('.keynope-engagement-toast').waitFor({state:'detached',timeout:12000});
  await page.locator('#stage').screenshot({path:path.join(proof,'pasted.png')});
  await page.evaluate(()=>editorAction({action:'undo'}));await waitForAsync(page,async()=>!deck.pages[0].scene.objects.length);
  await page.evaluate(()=>editorAction({action:'redo'}));await waitForAsync(page,async()=>deck.pages[0].scene.objects.length===7);assert.deepEqual(await page.evaluate(()=>deck.pages[0].scene.objects.map(o=>({id:o.id,bounds:o.bounds}))),pasted.map(o=>({id:o.id,bounds:o.bounds})));
  const saved=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Saved.md'})}).then(r=>r.json()).then(r=>r.content));await open('Saved.md',saved);
  const reopened=await page.evaluate(()=>deck.pages[0].scene.objects);for(const o of reopened){const old=pasted.find(p=>p.id===o.id);assert(old);for(const axis of ['x','y','width','height'])assert(Math.abs(o.bounds[axis]-old.bounds[axis])<1e-5);}
  assert.deepEqual(errors,[]);console.log('PASS '+engine+': cross-page physical frames, natural/bounded fonts, structured objects, rotated labelled shapes, grouped registration, attached endpoints, private-data exclusion, Undo/Redo and reopen.');
 }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);server.close();process.exitCode=1;});
