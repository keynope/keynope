const {waitForAsync}=require("./wait_for_async.cjs");
// Owned diagrams through the production WASM editor, package and shared SVG.
const assert=require('node:assert/strict'),fs=require('node:fs'),http=require('node:http'),path=require('node:path');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),output='/tmp/keynope-connections-proof';
const server=http.createServer((req,res)=>{
 let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);
 if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}
 res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));
});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
 try{
  const context=await browser.newContext({viewport:{width:1500,height:1000},reducedMotion:'reduce'});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
  const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const open=async(name,buffer,mimeType)=>{const picker=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await picker).setFiles({name,buffer,mimeType});};
  const lines=()=>page.evaluate(()=>deck.pages.map(p=>p.scene.objects.find(o=>o.kind==='connector')));
  const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
  const near=(a,b)=>{assert.equal(a.length,b.length);a.forEach((p,i)=>assert(Math.hypot(p.x-b[i].x,p.y-b[i].y)<.02,'Imported bend or endpoint moved'));};
  await open('connected.md',fs.readFileSync(output+'/connected.md'),'text/markdown');
  await page.waitForFunction(()=>deck.pages.length===4&&deck.pages.every(p=>p.scene?.objects.some(o=>o.kind==='connector')));
  const before=await lines();
  for(let i=0;i<4;i++){await page.locator('.keynope-slide-item').nth(i).click();await page.waitForFunction(i=>pageIndex===i,i);await page.screenshot({path:output+'/original-'+i+'.png'});}
  const exported=await page.evaluate(()=>fetch('/api/editor/export-pptx',{method:'POST'}).then(r=>r.json()));assert(exported.content);assert(!(exported.report?.warnings||[]).some(w=>/attachment|route|no-stroke/.test(w.code)));
  fs.writeFileSync(output+'/wasm-connected.pptx',Buffer.from(exported.content,'base64'));
  await open('connected.pptx',Buffer.from(exported.content,'base64'),'application/vnd.openxmlformats-officedocument.presentationml.presentation');
  await page.waitForFunction(()=>deck.pages.length===4&&deck.pages.every(p=>p.scene?.objects.some(o=>o.id.startsWith('pptx-')&&o.kind==='connector')));
  const after=await lines();before.forEach((a,i)=>{near(a.points,after[i].points);assert.equal(after[i].arrows,'both');assert.equal(after[i].paint.opacity,.6);});
  for(let i=0;i<4;i++){await page.locator('.keynope-slide-item').nth(i).click();await page.waitForFunction(i=>pageIndex===i,i);assert.equal((await page.locator('#stage polyline[data-object-id="'+after[i].id+'"]').getAttribute('stroke')).toLowerCase(),'#ff0055');await page.screenshot({path:output+'/reimported-'+i+'.png'});}
  await page.locator('.keynope-slide-item').nth(0).click();await page.waitForFunction(()=>pageIndex===0);
  const s=await state(),wire=s.slides[0].elements.find(e=>e.kind==='connector'),from=new URLSearchParams(wire.query).get('connector-from'),index=s.slides[0].elements.findIndex(e=>e.id===from),body=structuredClone(s.slides[0].elements[index]);
  const q=new URLSearchParams(body.query);q.set('object-offset-x','2');body.query=q.toString();
  await page.evaluate(({index,body})=>editorAction({action:'update-element',element:index,elementData:body}),{index,body});
  await page.waitForFunction(({id,x})=>Math.abs(deck.pages[0].scene.objects.find(o=>o.id===id).points[0].x-x)>1,{id:after[0].id,x:after[0].points[0].x});
  await page.getByRole('button',{name:'Undo',exact:true}).click();near((await lines())[0].points,after[0].points);
  const saved=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Connected saved.md'})}).then(r=>r.json()).then(r=>r.content));assert(saved.includes('connector-from'));
  await open('Connected saved.md',Buffer.from(saved),'text/markdown');await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.path?.endsWith('Connected saved.md')&&deck.pages.length===4&&deck.pages.every(p=>p.scene?.objects.some(o=>o.kind==='connector'));});
  (await lines()).forEach((o,i)=>near(o.points,after[i].points));assert.deepEqual(errors,[]);
  console.log('PASS: four rotated/curved attached diagrams retain routed bends, double arrows, colour/opacity, live endpoint following, Undo and Markdown reopen through actual WASM PPTX conversion');
 }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);server.close();process.exitCode=1;});
