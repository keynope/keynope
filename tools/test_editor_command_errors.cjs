const {waitForAsync}=require("./wait_for_async.cjs");
// Shared HTTP/WASM error negotiation, readable UI and obsolete-document replies.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site');
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));const browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
 try{
  const context=await browser.newContext({viewport:{width:1440,height:1000}});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());const page=await context.newPage(),errors=[];
  page.on('pageerror',error=>errors.push(error.message));page.on('dialog',dialog=>dialog.accept());
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const open=async(name,text)=>{const chooserEvent=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();const chooser=await chooserEvent;assert.equal(await chooser.element().evaluate(input=>input.isConnected),true,'File chooser owner must remain mounted');await chooser.setFiles({name,mimeType:'text/markdown',buffer:Buffer.from('<!-- keynope width=245 height=56 -->\n\n<!-- element-id=body top=5 left=5 width=150 height=15 render=truetype -->\n'+text+'\n')});await waitForAsync(page,async name=>(await fetch('/api/editor/state').then(r=>r.json())).path.endsWith(name),name);await page.waitForFunction(()=>![...document.querySelectorAll('input[type=file]')].some(input=>input.accept.startsWith('.md,.markdown,.pptx')));};
  await open('Owned errors.md','Owned error contract');
  await page.evaluate(async()=>{
   const state=()=>fetch('/api/editor/state').then(r=>r.json()),before=await state(),action={action:'set-selection-property',slide:0,sceneMaster:false,sceneRevision:before.version,objectIds:['body'],property:'text.size',propertyValue:100000};
   const response=await fetch('/api/editor/action',{method:'POST',headers:{'Content-Type':'application/json','Accept':'application/problem+json'},body:JSON.stringify(action)}),problem=await response.json();
   if(response.status!==400||problem.version!==1||problem.code!=='invalid-value'||problem.field!=='text.size'||problem.objectId!=='body'||response.headers.get('X-Keynope-Error-Code')!==problem.code)throw Error('WASM structured failure contract missing');
   const legacy=await fetch('/api/editor/action',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(action)});if((await legacy.text()).trim()!==problem.message||!legacy.headers.get('Content-Type').startsWith('text/plain'))throw Error('Legacy readable response changed');
   const focus=document.activeElement;let failure;try{await editorAction(action);}catch(error){failure=error;}const notice=document.querySelector('.keynope-command-error');if(!failure||failure.code!==problem.code||failure.field!==problem.field||failure.objectId!=='body'||failure.message!==problem.message||notice.hidden||notice.querySelector('span').textContent!==problem.message||document.activeElement!==focus)throw Error('Shared editor displayed JSON, stole focus or lost failure metadata');
   const after=await state();if(after.version!==before.version||JSON.stringify(after.slides)!==JSON.stringify(before.slides))throw Error('Rejected edits changed document');
  });
  await page.screenshot({path:process.env.TEST_SCREENSHOT||'/tmp/keynope-command-errors.png'});
  await page.getByRole('button',{name:'Dismiss editor error',exact:true}).click();assert.equal(await page.locator('.keynope-command-error').isVisible(),false);
  await page.evaluate(async()=>{const state=await fetch('/api/editor/state').then(r=>r.json());try{await editorAction({action:'set-selection-property',sceneRevision:state.version,sceneMaster:false,slide:0,objectIds:['body'],property:'text.size',propertyValue:100000});}catch(_){};});assert.equal(await page.locator('.keynope-command-error').isVisible(),true);
  await page.evaluate(async()=>{
   const original=window.fetch.bind(window),state=await original('/api/editor/state').then(r=>r.json());let started;const ready=new Promise(resolve=>started=resolve);
   window.fetch=async(input,init={})=>{if(String(input)==='/api/editor/action'&&JSON.parse(init.body||'{}').property==='text.size'){started();return new Promise(resolve=>window.releaseObsoleteFailure=()=>resolve(new Response(JSON.stringify({version:1,code:'stale-target',message:'OBSOLETE FAILURE',status:400}),{status:400,headers:{'Content-Type':'application/problem+json'}})));}return original(input,init);};
   window.pendingObsoleteCommand=editorAction({action:'set-selection-property',sceneRevision:state.version,sceneMaster:false,slide:0,objectIds:['body'],property:'text.size',propertyValue:120});await ready;
  });
  await open('Replacement errors.md','Replacement document');
  const beforeRelease=await page.evaluate(async()=>({hidden:document.querySelector('.keynope-command-error').hidden,state:await fetch('/api/editor/state').then(r=>r.json())}));assert.equal(beforeRelease.hidden,true);
  await page.evaluate(async()=>{window.releaseObsoleteFailure();await window.pendingObsoleteCommand;});
  const afterRelease=await page.evaluate(async()=>({hidden:document.querySelector('.keynope-command-error').hidden,state:await fetch('/api/editor/state').then(r=>r.json())}));assert.deepEqual(afterRelease,beforeRelease);
  assert.match(await page.title(),/Replacement errors\.md/);
  // A failure can arrive before replacement but finish reading its body after
  // replacement. It is just as obsolete as a delayed response header.
  await page.evaluate(async()=>{
   const original=window.fetch.bind(window),state=await original('/api/editor/state').then(r=>r.json());let reading;const ready=new Promise(resolve=>reading=resolve);
   window.fetch=async(input,init={})=>{if(String(input)==='/api/editor/action'&&JSON.parse(init.body||'{}').property==='text.size')return {ok:false,headers:new Headers({'Content-Type':'application/problem+json'}),text:()=>{reading();return new Promise(resolve=>window.releaseObsoleteBody=()=>resolve(JSON.stringify({version:1,code:'stale-target',message:'OBSOLETE BODY',status:400})));}};return original(input,init);};
   window.pendingObsoleteBody=editorAction({action:'set-selection-property',sceneRevision:state.version,sceneMaster:false,slide:0,objectIds:['body'],property:'text.size',propertyValue:120});await ready;
  });
  await open('Final errors.md','Final document');
  const beforeBody=await page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
  await page.evaluate(async()=>{window.releaseObsoleteBody();await window.pendingObsoleteBody;});assert.equal(await page.locator('.keynope-command-error').isVisible(),false);assert.deepEqual(await page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json())),beforeBody);
  await page.evaluate(async()=>{await editorAction({action:'add-element',kind:'text'});const state=await fetch('/api/editor/state').then(r=>r.json());if(state.slides[0].elements.length!==2)throw Error('Obsolete failure broke the command queue');});
  assert.deepEqual(errors,[]);console.log('PASS: structured/plain errors, WASM header negotiation, readable UI, rejected-edit ownership and obsolete-document failure isolation.');
 }finally{await browser.close();server.close();}
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
