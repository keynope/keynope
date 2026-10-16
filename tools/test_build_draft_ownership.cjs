// Real shared editor Save barrier, draft history and document replacement.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http'),cp=require('node:child_process');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright'),root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),output=fs.mkdtempSync('/tmp/keynope-build-draft-');
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 cp.execFileSync('go',['test','-run','^TestBuildDraftBrowserFixture$','.'],{env:{...process.env,GOCACHE:process.env.GOCACHE||'/tmp/keynope-go-cache',KEYNOPE_BUILD_DRAFT_EVIDENCE:output},stdio:'inherit'});
 const markdown=fs.readFileSync(path.join(output,'build-drafts.md'),'utf8'),records=[];
 await new Promise(r=>server.listen(0,'127.0.0.1',r));
 for(const engine of ['chromium','webkit']){
  const browser=await pw[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
  try{
   const context=await browser.newContext({viewport:{width:1440,height:1000},reducedMotion:'reduce'});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
   const page=await context.newPage(),errors=[];page.setDefaultTimeout(20000);page.on('pageerror',e=>errors.push(e.message));
   await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
   const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json())),action=data=>page.evaluate(data=>editorAction(data),data);
   // Same production model-init and workspace-replacement entry points as Open.
   const replace=(text=markdown)=>page.evaluate(async markdown=>{const init=JSON.parse(keynopeWasmInit(markdown,'Build drafts.md',false));if(init.status!==200)throw Error(init.body);const reply=JSON.parse(keynopeWasmWorkspace());if(reply.status!==200)throw Error(reply.body);await keynopeReloadWebDocument(JSON.parse(reply.body));},text);
   const navigate=async mode=>{if(mode==='tab')await action({action:'select-slide',slide:1});if(mode==='base'||mode==='layout'){await action({action:'toggle-master-mode'});await action({action:'select-slide',slide:mode==='base'?0:1});}};
   const dialog=page.getByRole('dialog',{name:'Slide builds',exact:true});
   const launch=async()=>{await page.locator('[data-ribbon-tab="slide"]').click();await page.getByRole('button',{name:'Edit slide builds',exact:true}).click();await dialog.waitFor();};
   for(const mode of ['slide','tab','base','layout']){
    await replace();await navigate(mode);let selected=(await state()).slides[(await state()).current].elements[0];await action({action:'select-element',elementData:{id:selected.id}});
    await launch();const before=await state();
    await dialog.getByRole('button',{name:'Add build for selected objects',exact:true}).click();
    await dialog.getByRole('combobox',{name:'Transition effect',exact:true}).selectOption('fade');
    await dialog.getByRole('spinbutton',{name:'Build duration (ms)',exact:true}).fill('1250');
    assert.equal(await page.evaluate(()=>{KeynopeBuilds.isDirty();const original=JSON.stringify;let calls=0;JSON.stringify=(...args)=>{calls++;return original(...args);};try{for(let i=0;i<100;i++)if(!KeynopeBuilds.isDirty())throw Error('Dirty state was lost');return calls;}finally{JSON.stringify=original;}}),0,'Unchanged dirty polling serialized the build draft');
    // Native Save can arrive without a blur; simultaneous saves share one commit.
    await page.evaluate(()=>Promise.all([window.keynopePrepareDocumentSave(),window.keynopePrepareDocumentSave()]));
    let after=await state();assert.equal(after.version,before.version+1,'Save omitted or duplicated the pending '+mode+' draft');assert.equal(after.slideBuild.transition.effect,'fade');assert.equal(after.slideBuild.steps[0].durationMs,1250);assert.deepEqual(after.slideBuild.steps[0].targetIds,[selected.id]);
    await dialog.waitFor({state:'detached'});await action({action:'undo'});assert.equal((await state()).slideBuild.transition.effect,'cut');await action({action:'redo'});assert.equal((await state()).slideBuild.transition.effect,'fade');
    await launch();await dialog.getByRole('combobox',{name:'Transition effect',exact:true}).selectOption('wipe');
    assert(await page.evaluate(()=>KeynopeBuilds.isDirty()),'Draft was not dirty');
    await dialog.getByRole('button',{name:'Undo edit',exact:true}).click();assert(!await page.evaluate(()=>KeynopeBuilds.isDirty()),'Local Undo failed to clear dirty state');
    await dialog.getByRole('combobox',{name:'Transition effect',exact:true}).selectOption('fade');assert(await dialog.getByRole('button',{name:'Redo edit',exact:true}).isEnabled(),'Equivalent input discarded local Redo');
    await dialog.getByRole('button',{name:'Redo edit',exact:true}).click();assert(await page.evaluate(()=>KeynopeBuilds.isDirty()));
    const cancelVersion=(await state()).version;await dialog.getByRole('button',{name:'Cancel',exact:true}).click();await dialog.waitFor({state:'detached'});assert.equal((await state()).version,cancelVersion);assert(!await page.evaluate(()=>KeynopeBuilds.isDirty()));
    const saved=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Build drafts.md'})}).then(r=>r.json()).then(r=>r.content));await replace(saved);await navigate(mode);assert.deepEqual((await state()).slideBuild,after.slideBuild,'Saved build changed on reopen');
    records.push({engine,mode,saveBarrier:true,focusedNumericInput:true,singleConcurrentCommit:true,localAndDocumentHistory:true,cancel:true,reopen:true});
   }
   // Invalid input must stop Save without losing the draft or disabling Cancel.
   await replace();await launch();const invalidBefore=await state();await dialog.getByRole('spinbutton',{name:'Transition duration (ms)',exact:true}).fill('-50');assert(await page.evaluate(()=>KeynopeBuilds.isDirty()));
   const invalid=await page.evaluate(()=>keynopePrepareDocumentSave().then(()=>'',e=>e.message));assert.match(invalid,/valid transition duration/i);assert.equal((await state()).version,invalidBefore.version);assert(await dialog.getByRole('button',{name:'Cancel',exact:true}).isEnabled());
   await dialog.getByRole('spinbutton',{name:'Transition duration (ms)',exact:true}).fill('537');await dialog.getByRole('combobox',{name:'Transition effect',exact:true}).selectOption('fade');
   await page.evaluate(()=>{const original=window.fetch;window.fetch=function(input,init){if(String(input)==='/api/editor/action'&&JSON.parse(init?.body||'{}').action==='set-slide-build'){window.fetch=original;return Promise.resolve(new Response('Owned test rejection',{status:400}));}return original.call(window,input,init);};});
   const rejected=await page.evaluate(()=>keynopePrepareDocumentSave().then(()=>'',e=>e.message));assert.match(rejected,/Owned test rejection/);assert.equal((await state()).version,invalidBefore.version);assert(await page.evaluate(()=>KeynopeBuilds.isDirty()));assert(await dialog.getByRole('button',{name:'Apply',exact:true}).isEnabled());
   await page.screenshot({path:path.join(output,engine+'-recoverable.png')});await page.evaluate(()=>keynopePrepareDocumentSave());await dialog.waitFor({state:'detached'});assert.equal((await state()).version,invalidBefore.version+1);assert.equal((await state()).slideBuild.transition.durationMs,537);
   records.push({engine,invalidSave:true,rejectedCommitRetry:true});
   // Equal path, IDs and revision must not admit an old dialog callback.
   await replace();await launch();await dialog.getByRole('combobox',{name:'Transition effect',exact:true}).selectOption('push');
   await page.evaluate(()=>window.oldBuildApply=[...document.querySelector('[aria-label="Slide builds"]').querySelectorAll('button')].find(b=>b.textContent==='Apply').onclick);
   const originalOwner=(await state()).notesSessionId;await replace();const replacement=await state();assert.notEqual(replacement.notesSessionId,originalOwner);await page.evaluate(()=>oldBuildApply());
   assert.deepEqual((await state()).slides,replacement.slides);assert.equal((await state()).version,replacement.version,'Detached old Apply mutated replacement');assert.equal(await dialog.count(),0,'Old dialog survived document replacement');
   // The scene fetch can finish after Open even when the path/revision match.
   await page.evaluate(()=>{const original=window.fetch;window.releaseBuildScene=null;window.fetch=async function(input,...rest){const response=await original.call(window,input,...rest);if(String(input).startsWith('/api/editor/scene?')&&!window.releaseBuildScene){await new Promise(resolve=>window.releaseBuildScene=resolve);window.fetch=original;}return response;};});
   await page.locator('[data-ribbon-tab="slide"]').click();await page.getByRole('button',{name:'Edit slide builds',exact:true}).click();await page.waitForFunction(()=>!!window.releaseBuildScene);await replace();await page.evaluate(()=>releaseBuildScene());await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));
   assert.equal(await dialog.count(),0,'Late old scene opened a dialog for the replacement');
   // The table-size dialog also retains its insertion destination owner.
   await replace();await page.getByRole('button',{name:'Insert',exact:true}).click();await page.getByRole('button',{name:'Add table',exact:true}).click();const insert=page.getByRole('dialog',{name:'Insert table',exact:true});await insert.waitFor();
   await page.evaluate(()=>window.oldTableInsert=[...document.querySelector('[aria-label="Insert table"]').querySelectorAll('button')].find(b=>b.textContent==='Insert').onclick);
   await replace();const insertBefore=await state();await page.evaluate(()=>oldTableInsert());assert.equal((await state()).version,insertBefore.version);assert.deepEqual((await state()).slides,insertBefore.slides);await insert.getByRole('button',{name:'Cancel',exact:true}).click();
   assert.deepEqual(errors,[]);records.push({engine,staleApply:true,lateLauncher:true});
   await page.screenshot({path:path.join(output,engine+'-clean.png')});console.log('PASS '+engine+': build draft Save/history/Cancel in four scopes, replacement and late launch guards. '+output);
  }finally{await browser.close();}
 }
 fs.writeFileSync(path.join(output,'report.json'),JSON.stringify(records,null,2));
})().catch(error=>{console.error(error);process.exitCode=1;}).finally(()=>server.close());
