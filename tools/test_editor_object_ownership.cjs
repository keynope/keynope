const {waitForAsync}=require("./wait_for_async.cjs");
// Shared WASM authoring/history/save acceptance for owned master clones.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http'),zlib=require('node:zlib');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),output='/tmp/keynope-object-ownership-proof';
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
 try{
  const context=await browser.newContext({viewport:{width:1500,height:1000},reducedMotion:'reduce'});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
  for(const mode of ['base','layout']){
   const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
   const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
   const action=data=>page.evaluate(data=>editorAction(data),data);
   const open=async(name,content)=>{const picker=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await picker).setFiles({name,mimeType:'text/markdown',buffer:Buffer.from(content)});await waitForAsync(page,async name=>(await fetch('/api/editor/state').then(r=>r.json())).path?.endsWith(name),name);};
   await open(mode+'.md',fs.readFileSync(output+'/'+mode+'.md'));
   await action({action:'toggle-master-mode'});await action({action:'select-slide',slide:mode==='base'?0:3});
   const before=await state(),source=mode==='base'?before.masters.base.slide:before.masters.layouts[2].slide,sourceIDs=new Set(source.elements.map(e=>e.id));
   await action({action:'clone-slide'});const cloned=await state(),index=cloned.current,copied=cloned.masters.layouts[index-1].slide;
   assert.notEqual(copied.id,source.id);assert.equal(copied.elements.length,source.elements.length);
   for(const e of copied.elements){assert(!sourceIDs.has(e.id));if(e.placeholderRole)assert.equal(e.slotId,e.id);if(e.kind==='table')assert.notEqual(e.objectId,'table-1');if(e.kind==='chart')assert.notEqual(e.objectId,'chart-1');}
   const line=copied.elements.find(e=>e.kind==='connector'),q=new URLSearchParams(line.query),ids=new Set(copied.elements.map(e=>e.id));assert(ids.has(q.get('connector-from'))&&ids.has(q.get('connector-to')));assert(!sourceIDs.has(q.get('connector-from'))&&!sourceIDs.has(q.get('connector-to')));
   const body=copied.elements.findIndex(e=>e.id===q.get('connector-from'));await action({action:'select-element',element:body,elementData:copied.elements[body]});assert.equal((await state()).selection.length,2,'Cloned group merged with inherited Base members');
   await page.screenshot({path:output+'/'+mode+'-cloned.png'});
   await page.getByRole('button',{name:'Undo',exact:true}).click();assert.deepEqual((await state()).masters,before.masters);
   await page.getByRole('button',{name:'Redo',exact:true}).click();assert.deepEqual((await state()).masters,cloned.masters);
   const saved=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Owned saved.md'})}).then(r=>r.json()).then(r=>r.content));
   const objects=JSON.parse(zlib.gunzipSync(Buffer.from(saved.match(/keynope-objects version=1 base64:([^ ]+)/)[1],'base64')));
   const build=objects.builds[copied.id];assert(build);assert.equal(build.steps.length,3);const targets=new Set();for(const step of build.steps){assert(Array.isArray(step.targetIds)&&step.targetIds.length>0);for(const id of step.targetIds){assert(ids.has(id));targets.add(id);}}assert.equal(targets.size,6);
   assert(objects.tables[copied.elements.find(e=>e.kind==='table').objectId]);assert(objects.charts[copied.elements.find(e=>e.kind==='chart').objectId]);
   await open('Owned saved.md',saved);await action({action:'toggle-master-mode'});await action({action:'select-slide',slide:index});const reopened=await state();assert.deepEqual(reopened.masters.layouts[index-1].slide,copied);
   assert.deepEqual(errors,[]);await page.close();
  }
  console.log('PASS: Base/named-layout diagram clones own endpoint IDs, groups, placeholders, table/chart payloads and build targets through actual WASM clone/select/Undo/Redo/save/reopen');
 }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);server.close();process.exitCode=1;});
