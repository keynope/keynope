const {waitForAsync}=require("./wait_for_async.cjs");
// Rejected authored metadata must not replace the working document or its history.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http'),zlib=require('node:zlib');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site');
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));const browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
 try{
  const context=await browser.newContext({viewport:{width:1440,height:1000}});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());const page=await context.newPage(),errors=[],alerts=[];
  let opening='',dialogSettled=Promise.resolve();page.on('pageerror',error=>{errors.push(error.message);if(process.env.KEYNOPE_METADATA_TRACE)console.error('Page error:',opening,error.message);});page.on('console',message=>{if(process.env.KEYNOPE_METADATA_TRACE&&message.type()==='error')console.error('Console:',opening,message.text());});page.on('dialog',dialog=>{if(process.env.KEYNOPE_METADATA_TRACE)console.log('Dialog:',opening,dialog.type(),dialog.message().slice(0,150));if(dialog.type()==='alert')alerts.push(dialog.message());dialogSettled=dialog.accept().catch(error=>errors.push(error.message));});
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  if(process.env.KEYNOPE_METADATA_TRACE)await page.evaluate(()=>{const text=File.prototype.text;File.prototype.text=async function(){console.error('Reading file:',this.name);const content=await text.call(this);console.error('Read file:',this.name);return content;};const init=window.keynopeWasmInit;window.keynopeWasmInit=(...args)=>{console.error('Parsing:',args[1]);const result=init(...args);console.error('Parsed:',args[1]);return result;};});
  const open=async(name,content)=>{await dialogSettled;opening=name;if(process.env.KEYNOPE_METADATA_TRACE)console.log('Open:',name);const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();if(process.env.KEYNOPE_METADATA_TRACE)console.log('Choosing:',name);await(await chooser).setFiles({name,mimeType:'text/markdown',buffer:Buffer.from(content)});if(process.env.KEYNOPE_METADATA_TRACE)console.log('Chosen:',name);};
  await open('Owned.md','<!-- keynope width=245 height=56 -->\n\n<!-- top=8 width=200 height=20 render=truetype -->\nKeep this document\n');await waitForAsync(page,async()=>(await fetch('/api/editor/state').then(r=>r.json())).path.endsWith('Owned.md'));
  const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
  await page.evaluate(()=>editorAction({action:'add-element',kind:'text'}));const before=await state(),title=await page.title(),slideJSON=await page.evaluate(()=>JSON.stringify(deck.pages[pageIndex].scene));
  const bad='<!-- keynope-objects version=1 base64:'+zlib.gzipSync('{"version":1,"Version":1}').toString('base64')+' -->\n\nBad document';
  await Promise.all([page.waitForEvent('dialog',{predicate:dialog=>dialog.type()==='alert'}),open('Ambiguous.md',bad)]);await dialogSettled;assert.equal(alerts.length,1);assert.match(alerts[0],/ambiguous metadata property/);assert.equal(await page.title(),title);assert.deepEqual((await state()).slides,before.slides);assert.equal((await state()).version,before.version);assert.equal(await page.evaluate(()=>JSON.stringify(deck.pages[pageIndex].scene)),slideJSON);
  const dimensions=()=>page.evaluate(()=>{const r=JSON.parse(keynopeWasmWorkspace());if(r.status!==200)throw Error(r.body);const w=JSON.parse(r.body);return [w.cols,w.rows];}),originalDimensions=await dimensions();
  const rejectOpen=async(name,content,reason)=>{const [dialog]=await Promise.all([page.waitForEvent('dialog',{predicate:dialog=>dialog.type()==='alert'}),open(name,content)]);await dialogSettled;assert.match(dialog.message(),reason);const current=await state();assert.deepEqual(current.slides,before.slides);assert.equal(current.version,before.version);assert.equal(await page.title(),title);assert.deepEqual(await dimensions(),originalDimensions);};
  const assets='<!-- keynope-assets version=1 base64:'+zlib.gzipSync('{}').toString('base64')+' -->';
  for(const [name,comment]of [
   ['Duplicate placement','top=1 top=2'],
   ['Encoded duplicate placement','top=1&%74op=2'],
   ['Placement colour alias','fg=red color=blue'],
   ['Malformed placement escape','top=1 link=https%ZZ'],
  ])await rejectOpen(name+'.md','<!-- keynope width=100 height=80 -->\n<!-- '+comment+' -->\nRejected',/duplicate placement|invalid placement/);
  for(const [name,content]of [
   ['Nonfinite placement','<!-- top=1 brightness=NaN -->\nRejected'],
   ['Conflicting link targets','<!-- link=https%3A%2F%2Fexample.com slide=2 -->\nRejected'],
   ['Nonfinite image query','![image](https://example.com/image.png?brightness=NaN)'],
   ['Unbounded finite width','<!-- width=1e308 -->\nRejected'],
   ['Unbounded integer anchor','<!-- top=9223372036854775807 -->\nRejected'],
   ['Unbounded image displacement','![image](https://example.com/image.png?row_delta=9223372036854775807)'],
  ])await rejectOpen(name+'.md','<!-- keynope width=100 height=80 -->\n'+content,/numeric placement|duplicate placement/);
  await rejectOpen('Too many comments.md','<!-- keynope width=100 height=80 -->\n'+('<!-- x -->\n').repeat(100001)+'Rejected',/metadata comments/);
  if(process.env.KEYNOPE_COMMENT_BUDGET_ONLY==='1'){
   assert.equal(await page.evaluate(()=>JSON.stringify(deck.pages[pageIndex].scene)),slideJSON);
   await page.evaluate(()=>editorAction({action:'undo'}));assert.equal((await state()).slides[0].elements.length,before.slides[0].elements.length-1);
   await page.evaluate(()=>editorAction({action:'redo'}));assert.deepEqual((await state()).slides,before.slides);assert.deepEqual(errors,[]);
   console.log('PASS: aggregate comment rejection preserves document, render, dimensions, revision and Undo/Redo.');return;
  }
  for(const [name,content,reason]of [
   ['Duplicate effect','<!-- effect=warp -->\n<!-- effect=none -->\nRejected',/duplicate slide effect/],
   ['Duplicate background','<!-- background=aurora -->\n<!-- background=none -->\nRejected',/duplicate slide background/],
   ['Duplicate style','Rejected\n\n<!-- fg=#ff0055 FG=#0055ff -->',/duplicate slide style/],
   ['Invalid style size','Rejected\n\n<!-- ttf-default-size=513 -->',/invalid slide style/],
   ['Invalid style width','Rejected\n\n<!-- ttf-default-width=NaN -->',/invalid slide style/],
  ])await rejectOpen(name+'.md','<!-- keynope width=100 height=80 -->\n'+content,reason);
  {
   const payload=JSON.stringify({corrupt:{mime:'image/png',width:2,height:2,data:Buffer.from('corrupt pixels').toString('base64')}}),archive='<!-- keynope-assets version=1 base64:'+zlib.gzipSync(payload).toString('base64')+' -->';await rejectOpen('Corrupt pixels.md','<!-- keynope width=100 height=80 -->\n'+archive+'\n\n# Rejected',/image bytes|pixels/);
  }
  for(const [name,metadata,reason]of [['Duplicate canvas','<!-- keynope width=245 height=56 -->\n<!-- keynope width=100 height=80 -->',/duplicate canvas/],['Oversized canvas','<!-- keynope width=4097 height=56 -->',/canvas must/],['Invalid QR','<!-- keynope-activity-qr=maybe -->',/malformed activity QR/],['Duplicate QR','<!-- keynope-activity-qr=on -->\n<!-- keynope-activity-qr=off -->',/duplicate activity QR/],['Duplicate slide identity','<!-- keynope width=100 height=80 -->\n<!-- keynope-slide-id=one -->\n<!-- keynope-slide-id=two -->',/duplicate slide identity/],['Duplicate layout','<!-- keynope width=100 height=80 -->\n<!-- layout=one -->\n<!-- layout=two -->',/duplicate slide layout/]]){
   await rejectOpen(name+'.md',metadata+'\n\n# Rejected',reason);
  }
  for(const [name,payload]of [['Asset fields','{"photo":{"mime":"image/png","MIME":"image/jpeg"}}'],['Original fields','{"photo":{"source":{"width":1,"Width":2}}}'],['Frame fields','{"photo":{"frames":[{"delayMs":50,"delayMs":100}]}}'],['Vector fields','{"photo":{"vector":{"version":1,"Version":2}}}']]){
   const archive='<!-- keynope-assets version=1 base64:'+zlib.gzipSync(payload).toString('base64')+' -->';await rejectOpen(name+'.md','<!-- keynope width=100 height=80 -->\n'+archive+'\n\n# Rejected',/ambiguous/);
  }
  for(const [namespace,payload]of [['masters','{"version":1}'],['tabs','[{"id":"guide","name":"Guide","page":1}]'],['document-extra','{"future":true}']]){
   const archive='<!-- keynope-'+namespace+' version=1 base64:'+Buffer.from(payload).toString('base64')+' -->';
   for(const [kind,metadata,reason]of [['duplicate',archive+'\n'+archive,/duplicate/],['future',archive.replace('version=1','version=2'),/unsupported/],['null','<!-- keynope-'+namespace+' version=1 base64:bnVsbA== -->',/JSON (object|array)/]]){
    await rejectOpen(namespace+'-'+kind+'.md','<!-- keynope width=100 height=80 -->\n'+metadata+'\n\n# Rejected',reason);
   }
  }
  for(const namespace of ['slide-extra','element-extra']){
   const archive='<!-- keynope-'+namespace+' version=1 base64:'+Buffer.from('{"future":true}').toString('base64')+' -->';
   for(const [kind,metadata,reason]of [['duplicate',archive+'\n'+archive,/duplicate/],['future',archive.replace('version=1','version=2'),/unsupported/],['malformed','<!-- keynope-'+namespace+' version=1 base64:%%% -->',/malformed/]]){
    await rejectOpen(namespace+'-'+kind+'.md','<!-- keynope width=100 height=80 -->\n'+metadata+'\n\n# Rejected',reason);
   }
  }
  for(const [namespace,payload]of [['engagement',Buffer.from('{"id":"storm-1","kind":"storm","prompt":"Ideas?"}')],['engagement-results',zlib.gzipSync('{"version":1,"activityId":"storm-1","kind":"storm","state":{"phase":3,"ideas":["Keep"]}}')],['background-media',Buffer.from('{"assetId":"background","fit":"contain"}')]]){
   const archive='<!-- keynope-'+namespace+' version=1 base64:'+payload.toString('base64')+' -->';
   for(const [kind,metadata,reason]of [['duplicate',archive+'\n'+archive,/duplicate/],['future',archive.replace('version=1','version=2'),/unsupported/],['malformed','<!-- keynope-'+namespace+' version=1 base64:%%% -->',/malformed/]]){
    await rejectOpen(namespace+'-'+kind+'.md','<!-- keynope width=100 height=80 -->\n'+metadata+'\n\n# Rejected',reason);
   }
  }
  for(const [name,content,reason]of [['Duplicate assets.md',assets+'\n'+assets+'\n\n# Rejected',/duplicate/],['Unsupported assets.md',assets.replace('version=1','version=2')+'\n\n# Rejected',/unsupported/],['No slides.md','<!-- keynope width=100 height=80 -->\n'+assets,/no slides/],['Invalid refs.md','<!-- keynope width=100 height=80 -->\n\n<!-- object-id=missing -->\n[table]',/payload|identity|table/]]){
   await rejectOpen(name,content,reason);
  }
  const rejected=await page.evaluate(async()=>{const r=await fetch('/api/editor/paste-clipboard',{method:'POST',headers:{'Content-Type':'application/json'},body:'{"action":"paste-objects","clipboard":{"version":1,"assets":{"same":{},"same":{}}}}'});return {status:r.status,body:await r.text()};});assert.equal(rejected.status,400);assert.match(rejected.body,/invalid object clipboard/);assert.deepEqual((await state()).slides,before.slides);assert.equal((await state()).version,before.version);
  await page.evaluate(()=>editorAction({action:'undo'}));assert.equal((await state()).slides[0].elements.length,before.slides[0].elements.length-1);await page.evaluate(()=>editorAction({action:'redo'}));assert.deepEqual((await state()).slides,before.slides);
  const literal=['<!-- keynope-masters version=9 base64:%%% -->','<!-- keynope-tabs version=9 base64:%%% -->','<!-- keynope-document-extra version=9 base64:%%% -->','<!-- keynope-objects version=9 base64:%%% -->','<!-- keynope-theme version=9 base64:%%% -->','<!-- keynope-assets version=9 base64:%%% -->','<!-- keynope-appearance broken -->','<!-- keynope-fonts version=9 base64:%%% -->','<!-- truetype-text broken -->','<!-- keynope width=100 height=80 -->','<!-- keynope-activity-qr=off -->','```','---','Literal 🙂'].join('\n');
  await open('Literal code.md','<!-- keynope width=245 height=56 -->\n\n````md\n'+literal+'\n````\n');await waitForAsync(page,async()=>(await fetch('/api/editor/state').then(r=>r.json())).path.endsWith('Literal code.md'));const codeState=await state();assert.equal(codeState.slides.length,1);assert.equal(codeState.slides[0].elements.length,1);assert.equal(codeState.slides[0].elements[0].kind,'code');assert.equal(codeState.slides[0].elements[0].text,literal);assert.deepEqual(await dimensions(),[245,56]);assert.equal(codeState.hideActivityQR,false);
  const priorAlerts=alerts.length;
  for(const suffix of ['-future','.future']){
   const metadata=['keynope-objects','keynope-theme','keynope-fonts','truetype-text'].map(namespace=>'<!-- '+namespace+suffix+' version=9 base64:RVhURU5TSU9O -->').join('\n');
   const name='Namespace'+suffix+'.md';await open(name,'<!-- keynope width=245 height=56 -->\n'+metadata+'\n\n# Original\n');
   await waitForAsync(page,async name=>(await fetch('/api/editor/state').then(r=>r.json())).path.endsWith(name),name);
   const current=await state();assert.equal(current.slides.length,1);assert.equal(current.slides[0].elements.length,2);assert.equal(current.slides[0].elements[1].text,'Original');
   for(const comment of metadata.split('\n'))assert(current.slides[0].elements[0].text.includes(comment),'Unknown literal markup was rewritten/discarded');
   assert(!current.slides[0].elements.some(e=>e.text==='EXTENSION'),'Foreign text decoded as legacy content');
   assert.equal(await page.evaluate(()=>deck.pages[pageIndex].scene.objects.length),2);assert.equal(alerts.length,priorAlerts,'Foreign namespace rejected as known metadata');
  }
  assert.deepEqual(errors,[]);console.log('PASS: ambiguous metadata rejection preserves document/history; foreign namespaces retain literal markup without legacy decoding or font-data removal.');
 }finally{await browser.close();server.close();}
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
