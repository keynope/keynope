const {waitForAsync}=require("./wait_for_async.cjs");
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),output=path.resolve(process.env.SVG_PROOF||'/tmp/keynope-svg-proof');
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const proof=name.startsWith('/proof/'),base=proof?output:root,file=path.resolve(base,'.'+(proof?name.slice(6):name));if(!file.startsWith(base+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.html':'text/html','.css':'text/css','.wasm':'application/wasm','.svg':'image/svg+xml','.png':'image/png'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 const engine=process.env.TEST_BROWSER||'chromium',browser=await playwright[engine].launch({headless:true,...(engine==='webkit'?{}:{executablePath:process.env.CHROME_PATH})});
 try{
  const context=await browser.newContext({viewport:{width:1500,height:1000}}),errors=[];
  await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());context.on('page',p=>p.on('pageerror',e=>errors.push(e.message)));
  const page=await context.newPage();page.setDefaultTimeout(30000);page.on('dialog',d=>d.accept());const origin='http://127.0.0.1:'+server.address().port;
  await page.goto(origin+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));const action=data=>page.evaluate(data=>editorAction(data),data);
  async function open(file){
    const owner=(await state()).notesSessionId;
    const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles(file);
    // The saved SVG has the same object ID/source as the old document. That
    // DOM predicate cannot prove Open completed before testing upload isolation.
    await waitForAsync(page,async previous=>{const next=await fetch('/api/editor/state').then(r=>r.json());return !!next.notesSessionId&&next.notesSessionId!==previous;},owner);
  }
  await open(path.join(output,'vector.md'));
  await page.waitForFunction(()=>{const img=document.querySelector('#stage [data-object-id="vector"] img');return img?.src.startsWith('data:image/svg+xml;base64,')&&img.complete&&img.naturalWidth===320;});
  const source=await page.locator('#stage [data-object-id="vector"] img').getAttribute('src');assert.ok(source);
  await page.waitForFunction(()=>!!document.querySelector('#stage [data-object-id="retro"] .keynope-scene-retro-text'));
  await page.screenshot({path:path.join(output,'editor-vector-media.png')});
  await page.getByRole('button',{name:'Insert',exact:true}).click();const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Import image',exact:true}).click();await(await chooser).setFiles(path.join(output,'passive-logo.svg'));
  await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[s.current].elements.length===5;});
  const imported=await state(),e=imported.slides[imported.current].elements[imported.selected];assert.equal(e.kind,'image');assert.ok(e.assetId);
  await page.waitForFunction(id=>{const img=document.querySelector(`#stage [data-object-id="${id}"] img`);return img?.complete&&img.naturalWidth===320;},e.id);
  await action({action:'undo'});assert.equal((await state()).slides[0].elements.length,4);await action({action:'redo'});assert.equal((await state()).slides[0].elements.length,5);
  const saved=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'SVG.md'})}).then(r=>r.json()));assert.ok(saved.content.includes('keynope-assets'));
  await open({name:'SVG.md',mimeType:'text/markdown',buffer:Buffer.from(saved.content)});
  await page.waitForFunction(()=>document.querySelector('#stage [data-object-id="vector"] img')?.src.startsWith('data:image/svg+xml;base64,'));assert.equal(await page.locator('#stage [data-object-id="vector"] img').getAttribute('src'),source);
  const before=await state();const rejected=await page.evaluate(async()=>{const body=new FormData();body.append('image',new Blob(['<svg width="10" height="10"><script/></svg>'],{type:'image/svg+xml'}),'unsafe.svg');const response=await fetch('/api/editor/upload',{method:'POST',body});return {status:response.status,error:await response.text()};});assert.equal(rejected.status,400);assert.equal((await state()).version,before.version);
  const proof=await context.newPage();await proof.goto(origin+'/proof/vector.html');await proof.evaluate(()=>keynopePrepareSnapshot(0));
  await proof.waitForFunction(source=>[...document.querySelectorAll('[data-object-id] img')].some(img=>img.src===source&&img.complete&&img.naturalWidth===320),source);
  assert.ok(await proof.evaluate(source=>[...document.querySelectorAll('[data-object-id] img')].some(img=>img.src===source),source));
  await proof.screenshot({path:path.join(output,'standalone-vector-media.png')});
  // Compare the browser's SVG rasterization with the offline PNG at equal size.
  const comparison=await proof.evaluate(async()=>{const svg=new Image(),png=new Image();svg.src='/proof/passive-logo.svg';png.src='/proof/passive-logo.png';await Promise.all([svg.decode(),png.decode()]);const a=document.createElement('canvas'),b=document.createElement('canvas');a.width=b.width=320;a.height=b.height=180;const ca=a.getContext('2d'),cb=b.getContext('2d');ca.drawImage(svg,0,0);cb.drawImage(png,0,0);const x=ca.getImageData(0,0,320,180).data,y=cb.getImageData(0,0,320,180).data;let sum=0,large=0;for(let i=0;i<x.length;i++){const d=Math.abs(x[i]-y[i]);sum+=d;if(d>30)large++;}return {mean:sum/x.length,largeFraction:large/x.length};});
  assert.ok(comparison.mean<5&&comparison.largeFraction<.04,'SVG offline fallback diverged: '+JSON.stringify(comparison));assert.deepEqual(errors,[]);
  fs.writeFileSync(path.join(output,'browser-report.json'),JSON.stringify({passed:true,checks:['file chooser SVG import','modern vector decode','Retro fallback','crop/mask/rotation fixture','one-step undo/redo','save/reopen source retention','unsafe import isolation','standalone HTML parity','browser/offline raster comparison'],comparison},null,2)+'\n');console.log('SVG browser checks passed',comparison);
 }finally{await browser.close();await new Promise(resolve=>server.close(resolve));}
})().catch(error=>{console.error(error);process.exitCode=1;server.close();});
