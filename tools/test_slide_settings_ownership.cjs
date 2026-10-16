const {waitForAsync}=require("./wait_for_async.cjs");
// Real WASM settings ownership and layout-consumer reference lifecycle.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),output='/tmp/keynope-slide-settings-proof';
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
 try{
  const context=await browser.newContext({viewport:{width:1500,height:1000},reducedMotion:'reduce'});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
  const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
  const action=data=>page.evaluate(data=>editorAction(data),data);
  const open=async(name,content,expectedName=name)=>{const picker=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await picker).setFiles({name,mimeType:name.endsWith('.pptx')?'application/vnd.openxmlformats-officedocument.presentationml.presentation':'text/markdown',buffer:Buffer.from(content)});await waitForAsync(page,async name=>(await fetch('/api/editor/state').then(r=>r.json())).path?.endsWith(name),expectedName);await page.waitForFunction(name=>document.title==='Keynope — '+name||document.title==='Keynope — '+name+' *',expectedName);await page.evaluate(()=>new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r))));};
  const owner=(s,scope)=>scope==='base'?s.masters.base.slide:scope==='layout'?s.masters.layouts[2].slide:s.slides[1];
  const select=async scope=>{if(scope==='base'||scope==='layout')await action({action:'toggle-master-mode'});await action({action:'select-slide',slide:scope==='base'?0:scope==='layout'?3:1});};
  for(const scope of ['slide','tab','base','layout']){
   await open(scope+'.md',fs.readFileSync(output+'/'+scope+'.md'));await select(scope);
   const before=await state(),original=owner(before,scope),incoming=structuredClone(original);
   Object.assign(incoming,{bg:'48;2;18;52;86',bgSet:true,id:'hijack',layoutId:'missing',tabId:'fake',notes:'',elements:[],engagement:null,engagementResult:null,futureOwner:null});
   await action({action:'update-slide',slideData:incoming});const accepted=await state(),expected={...original,bg:'48;2;18;52;86',bgSet:true};
   assert.deepEqual(owner(accepted,scope),expected,scope+' settings modified protected content');
   await action({action:'undo'});assert.deepEqual(owner(await state(),scope),original);
   await action({action:'redo'});assert.deepEqual(owner(await state(),scope),expected);
   await page.screenshot({path:output+'/'+scope+'-settings.png'});
   const saved=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Settings saved.md'})}).then(r=>r.json()).then(r=>r.content));
   assert.equal(typeof saved,'string');assert(saved.length>0);
   await open('Settings saved.md',saved);await select(scope);assert.deepEqual(owner(await state(),scope),expected,scope+' save/reopen changed owner');
  }
  await open('references.md',fs.readFileSync('/tmp/keynope-layout-reference-proof/references.md'));
  const before=await state();await action({action:'update-layout',kind:'title-subtitle',slideData:{pageNumber:'hide'}});const after=await state();
  assert.deepEqual(after.slides[0],before.slides[0]);
  for(const i of [1,2]){assert(before.slides[i].elements.some(e=>e.kind==='connector'));assert(!after.slides[i].elements.some(e=>e.kind==='connector'));assert.equal(after.slides[i].notes,before.slides[i].notes);assert.equal(after.slides[i].tabId,before.slides[i].tabId);}
  await action({action:'undo'});assert.deepEqual((await state()).slides,before.slides);await action({action:'redo'});assert.deepEqual((await state()).slides,after.slides);
  await open('bound.md',fs.readFileSync('/tmp/keynope-master-metadata-proof/bound.md'));
  const boundBefore=await state();
  await action({action:'set-scene-text',slide:0,sceneRevision:boundBefore.version,objectId:'child-title',textRuns:[{text:'Edited title'}]});
  const boundAfter=await state();
  assert.equal(boundAfter.slides[0].elements[0].text,'Edited title');
  assert.deepEqual(boundAfter.slides[0].elements[0].futureChild,{target:'payload'});assert.deepEqual(boundAfter.slides[0].futureSlide,{keep:true});assert.deepEqual(boundAfter.masters,boundBefore.masters);
  await action({action:'undo'});assert.deepEqual((await state()).slides,boundBefore.slides);await action({action:'redo'});assert.deepEqual((await state()).slides,boundAfter.slides);
  const boundSaved=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Bound saved.md'})}).then(r=>r.json()).then(r=>r.content));
  await open('Bound saved.md',boundSaved);const boundReopened=await state();assert.deepEqual(boundReopened.slides[0].elements[0].futureChild,{target:'payload'});assert.deepEqual(boundReopened.slides[0].futureSlide,{keep:true});
  await page.screenshot({path:'/tmp/keynope-master-metadata-proof/bound-edited.png'});
  await open('clipboard-source.md',fs.readFileSync('/tmp/keynope-clipboard-inheritance-proof/source.md'));
  const copyState=await state();
  const bundle=await page.evaluate(async version=>{const r=await fetch('/api/editor/clipboard',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({version,ids:['child-title']})});if(!r.ok)throw Error(await r.text());return r.json();},copyState.version);
  const sourceObject=await page.evaluate(()=>deck.pages[0].scene.objects.find(o=>o.id==='child-title'));
  const copiedQuery=new URLSearchParams(bundle.elements[0].query);
  for(const [key,value] of Object.entries({'modern-font':'mono','modern-size':'155','modern-width':'82',fg:'#ff5500',left:'11',top:'9',width:'80',height:'20'}))assert.equal(copiedQuery.get(key),value);
  assert(!copiedQuery.has('master-clear')&&!copiedQuery.has('shadow'));assert.deepEqual((await state()).slides,copyState.slides);
  await open('clipboard-destination.md','<!-- keynope width=245 height=56 -->\n\nDestination\n');
  const destinationBefore=await state();await action({action:'paste-objects',clipboard:bundle});const destinationAfter=await state(),pasted=destinationAfter.slides[0].elements.at(-1);
  assert.notEqual(pasted.id,'child-title');assert.deepEqual(pasted.futureChild,{target:'payload'});
  const pastedObject=await page.evaluate(id=>deck.pages[0].scene.objects.find(o=>o.id===id),pasted.id);
  for(const field of ['bounds','paint','text'])assert.deepEqual(pastedObject[field],sourceObject[field],'cross-deck appearance changed '+field);
  await action({action:'undo'});assert.deepEqual((await state()).slides,destinationBefore.slides);await action({action:'redo'});assert.deepEqual((await state()).slides,destinationAfter.slides);
  await page.screenshot({path:'/tmp/keynope-clipboard-inheritance-proof/pasted.png'});
  const pastedSaved=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Pasted saved.md'})}).then(r=>r.json()).then(r=>r.content));
  await open('Pasted saved.md',pastedSaved);const pastedReopened=await state();assert.deepEqual(pastedReopened.slides[0].elements.at(-1).futureChild,{target:'payload'});
  const reopenedObject=await page.evaluate(id=>deck.pages[0].scene.objects.find(o=>o.id===id),pasted.id);for(const field of ['bounds','paint','text'])assert.deepEqual(reopenedObject[field],sourceObject[field],'reopened cross-deck appearance changed '+field);
  await open('theme-source.md',fs.readFileSync('/tmp/keynope-clipboard-theme-proof/source.md'));
  const themeState=await state(),sourceScene=await page.evaluate(()=>deck.pages[0].scene);
  const themeBundle=await page.evaluate(async version=>{const r=await fetch('/api/editor/clipboard',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({version,ids:['text','shape']})});if(!r.ok)throw Error(await r.text());return r.json();},themeState.version);
  assert.equal(Object.keys(themeBundle.themeFallbacks).length,2);
  for(const themed of [false,true]){
   await open(themed?'theme-destination.md':'plain-destination.md',themed?fs.readFileSync('/tmp/keynope-clipboard-theme-proof/destination.md'):'<!-- keynope width=245 height=56 -->\n\nPlain destination\n');
   const existing=new Set((await state()).slides[0].elements.map(e=>e.id));
   await action({action:'paste-objects',clipboard:themeBundle});const s=await state(),scene=await page.evaluate(()=>deck.pages[0].scene),inserted=s.slides[0].elements.filter(e=>!existing.has(e.id));assert.equal(inserted.length,2);
   const authored=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Theme check.md'})}).then(r=>r.json()).then(r=>r.content));
   assert.equal(authored.includes('keynope-theme version='),themed,'destination authored theme fixture did not load');
   for(const e of inserted){
    const actual=scene.objects.find(o=>o.id===e.id),original=sourceScene.objects.find(o=>o.id===themeBundle.elements.find(source=>source.kind===e.kind).id),q=new URLSearchParams(e.query);
    if(!themed){for(const field of ['bounds','paint','text'])assert.deepEqual(actual[field],original[field],'source fallback failed '+e.kind+' '+field);assert(!q.has('theme-fg'));}
    else if(e.kind==='text'){assert.equal(q.get('modern-font'),'theme');assert.equal(q.get('theme-fg'),'accent-1');assert.notEqual(actual.paint.color,original.paint.color);}
   }
   await page.locator('#stage [data-object-id="'+inserted[0].id+'"]').first().waitFor();
   await page.evaluate(()=>document.fonts.ready.then(()=>new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)))));
   await page.screenshot({path:'/tmp/keynope-clipboard-theme-proof/'+(themed?'themed':'plain')+'-pasted.png'});
  }
  assert.deepEqual(errors,[]);
  await open('rename.md','<!-- keynope width=245 height=56 -->\n\n<!-- element-id=rename-target top=5 left=5 width=100 height=15 render=truetype modern-font=mono -->\nNamed content\n');
  await page.getByRole('tab',{name:'Objects',exact:true}).click();
  const row=page.locator('[data-object-key="rename-target"]');await row.focus();await row.press('F2');
  const nameInput=page.getByRole('textbox',{name:'Object name',exact:true});await nameInput.fill('Cancelled name');const beforeCancel=await state();await nameInput.press('Escape');assert.deepEqual((await state()).slides,beforeCancel.slides);assert(await nameInput.isHidden());
  await row.focus();await row.press('F2');await nameInput.fill('Workshop <overview> 🚀');const beforeRename=await state();await nameInput.press('Enter');await waitForAsync(page,async()=>new URLSearchParams((await fetch('/api/editor/state').then(r=>r.json())).slides[0].elements[0].query).get('object-name')==='Workshop <overview> 🚀');
  await page.locator('#stage [data-object-id="rename-target"][aria-label="Workshop <overview> 🚀"]').waitFor();assert((await row.textContent()).includes('Workshop <overview> 🚀'));
  const named=await state();assert.equal(named.slides[0].elements[0].text,'Named content');await action({action:'undo'});assert.deepEqual((await state()).slides,beforeRename.slides);await action({action:'redo'});assert.deepEqual((await state()).slides,named.slides);
  const nameSaved=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Named saved.md'})}).then(r=>r.json()).then(r=>r.content));await open('Named saved.md',nameSaved);
  assert.equal(new URLSearchParams((await state()).slides[0].elements[0].query).get('object-name'),'Workshop <overview> 🚀');
  await page.getByRole('tab',{name:'Objects',exact:true}).click();await page.screenshot({path:'/tmp/keynope-master-metadata-proof/object-named.png'});
  await row.focus();await row.press('F2');await nameInput.fill('Stale draft');await action({action:'select-element',element:-1});await nameInput.press('Enter');
  await page.locator('.keynope-object-rename [role="status"]').getByText('The document changed. Cancel and rename the object again.',{exact:true}).waitFor();await nameInput.press('Escape');assert.equal(new URLSearchParams((await state()).slides[0].elements[0].query).get('object-name'),'Workshop <overview> 🚀');
  await row.focus();await row.press('F2');await nameInput.fill('Old document draft');await open('Named saved.md',nameSaved);assert(await nameInput.isHidden(),'Old-document rename survived document replacement');
  await open('Named source.md',fs.readFileSync('/tmp/keynope-object-names-pptx-proof/source.md'));
  const expectedNames=new Map([['Diagram <node> & 中文 🚀',2],['The connection',1],['Workshop plan',1],['Our outcomes',1],['Overview',1],['The picture',1]]);
  const exportedNames=await page.evaluate(()=>fetch('/api/editor/export-pptx',{method:'POST'}).then(r=>r.json()));assert(exportedNames.content);
  const namedPackage=Buffer.from(exportedNames.content,'base64');fs.writeFileSync('/tmp/keynope-object-names-pptx-proof/wasm-named.pptx',namedPackage);
  await open('Named.pptx',namedPackage,'Named.md');const importedNames=await state(),counts=new Map();
  for(const slide of importedNames.slides)for(const e of slide.elements){const name=new URLSearchParams(e.query).get('object-name');if(expectedNames.has(name))counts.set(name,(counts.get(name)||0)+1);}
  assert.deepEqual(counts,expectedNames);assert(importedNames.slides[0].elements.some(e=>e.kind==='connector'&&new URLSearchParams(e.query).get('object-name')==='The connection'));
  await page.getByRole('tab',{name:'Objects',exact:true}).click();await page.locator('.keynope-object-navigator').getByText(/The connection/).waitFor();await page.locator('.keynope-engagement-toast').waitFor({state:'detached'});await page.screenshot({path:'/tmp/keynope-object-names-pptx-proof/named-diagram.png'});
  await action({action:'select-slide',slide:1});await page.waitForFunction(()=>pageIndex===1);await page.locator('.keynope-object-navigator').getByText(/Workshop plan/).waitFor();await page.evaluate(()=>new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r))));await page.screenshot({path:'/tmp/keynope-object-names-pptx-proof/named-primitives.png'});
  assert.deepEqual(errors,[]);
  console.log('PASS: settings/master integrity, cross-deck themes, inline naming and all-primitive PPTX name export/import with attached connectors');
 }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);server.close();process.exitCode=1;});
