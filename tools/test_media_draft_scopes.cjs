const {waitForAsync}=require("./wait_for_async.cjs");
// Actual crop-dialog previews/commits on normal and compacted layout scopes.
const fs=require('node:fs'),path=require('node:path'),http=require('node:http'),assert=require('node:assert/strict');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=process.env.TEST_SITE||'/tmp/keynope-powerup-site',output=path.resolve(process.env.KEYNOPE_DRAFT_SCOPE_FIXTURE||'/tmp/keynope-draft-scope-proof');
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const engine=process.env.TEST_BROWSER||'chromium',browser=await playwright[engine].launch({headless:true,...(engine==='webkit'?{}:{executablePath:process.env.CHROME_PATH})});
 try{
  const context=await browser.newContext({viewport:{width:1440,height:1000},reducedMotion:'reduce'});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const picker=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await picker).setFiles(output+'/media-scopes.md');
  const action=data=>page.evaluate(data=>editorAction(data),data),state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
  await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides?.length===3&&s.slides[2].elements?.some(e=>e.id==='alpha');});
  await page.evaluate(()=>{window.draftScopeRecords=[];const original=window.fetch;window.fetch=async(...args)=>{const response=await original(...args);if(String(args[0]).endsWith('/media-draft'))draftScopeRecords.push({status:response.status,body:response.ok?await response.clone().json():await response.clone().text()});return response;};});
  for(const master of [false,true]){
   if(master)await action({action:'toggle-master-mode'});await action({action:'select-slide',slide:master?3:2});await page.locator('.keynope-canvas-element[data-element-id="alpha"]').dblclick();const dialog=page.getByRole('dialog',{name:'Crop image',exact:true});await dialog.waitFor();const s=await state(),index=s.current,elements=s.slides[index].elements,before=JSON.stringify(elements);
   const number=dialog.getByRole('spinbutton',{name:'Crop left percent',exact:true});await number.fill('20');await number.press('Tab');
   await page.waitForFunction(master=>draftScopeRecords.some(r=>r.status===200&&!!r.body.master===master&&r.body.objects?.[0]?.media?.crop?.left===.2),master);
   assert.equal(JSON.stringify((await state()).slides[index].elements),before,'Preview changed authored image');assert.equal((await state()).version,s.version,'Preview changed history revision');await page.screenshot({path:output+'/'+(master?'master':'normal')+'-crop-preview.png'});
   await dialog.getByRole('button',{name:'Apply crop',exact:true}).click();await dialog.waitFor({state:'detached'});const after=await state();assert.equal(after.version,s.version+1,'Crop was not one commit');assert.equal(new URLSearchParams(after.slides[index].elements.find(e=>e.id==='alpha').query).get('modern-crop').split(',')[0],'0.2');
   await page.getByRole('button',{name:'Undo',exact:true}).click();assert.equal(JSON.stringify((await state()).slides[index].elements),before,'Crop Undo changed unrelated authored fields');
   await page.locator('.keynope-canvas-element[data-element-id="alpha"]').dblclick();await dialog.waitFor();await dialog.getByRole('spinbutton',{name:'Crop left percent',exact:true}).fill('30');await dialog.getByRole('spinbutton',{name:'Crop left percent',exact:true}).press('Tab');await dialog.getByRole('button',{name:'Cancel',exact:true}).click();assert.equal(JSON.stringify((await state()).slides[index].elements),before,'Cancelled crop persisted');
  }
  assert.deepEqual(errors,[]);const records=await page.evaluate(()=>draftScopeRecords);assert(records.every(r=>r.status===200),'Crop draft rejected: '+JSON.stringify(records));fs.writeFileSync(output+'/browser-report.json',JSON.stringify(records,null,2));console.log('PASS: normal/layout crop live preview, original indices, one Apply/Undo and Cancel, no source mutation');
 }finally{await browser.close();server.close();}
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
