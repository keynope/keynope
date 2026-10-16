// Actual file chooser, hosted document replacement and production cache counters.
'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright'),engine=process.env.TEST_BROWSER||'chromium';
const {waitForAsync}=require('./wait_for_async.cjs');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),output=path.resolve(process.env.TEST_OUTPUT||'/tmp/keynope-document-lifetime-proof');
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 fs.mkdirSync(output,{recursive:true});const manifest=JSON.parse(fs.readFileSync(path.join(output,'manifest.json'),'utf8'));
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 const browser=await pw[engine].launch({headless:true,...(engine==='webkit'?{}:{executablePath:process.env.CHROME_PATH})});
 try{
  const context=await browser.newContext({viewport:{width:1440,height:1000},reducedMotion:'reduce'});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());
  const page=await context.newPage(),errors=[],dialogs=[];page.on('pageerror',error=>errors.push(error.message));page.on('dialog',async dialog=>{dialogs.push(dialog.message());await dialog.accept();});
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
  const stats=()=>page.evaluate(()=>{const response=JSON.parse(keynopeWasmWorkspace(0,true));if(response.status!==200)throw Error(response.body);return JSON.parse(response.body).projectionMetrics.runtimeCaches;});
  await page.evaluate(()=>{
   window.documentLifetimeProof={opens:[],clears:[]};const init=window.keynopeWasmInit;
   window.keynopeWasmInit=(...args)=>{const raw=init(...args);documentLifetimeProof.opens.push({name:args[1],status:JSON.parse(raw).status});return raw;};
   const clear=KeynopeTrueType.clearDocumentCaches;KeynopeTrueType.clearDocumentCaches=()=>{const before=KeynopeTrueType.cacheStats();clear();documentLifetimeProof.clears.push({before,after:KeynopeTrueType.cacheStats()});};
  });
  async function open(name,rejected=false){
   const before=await state(),attempts=await page.evaluate(()=>documentLifetimeProof.opens.length);
   const picker=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await picker).setFiles(path.join(output,name));
   await page.waitForFunction(n=>documentLifetimeProof.opens.length>n,attempts);
   const attempt=await page.evaluate(()=>documentLifetimeProof.opens.at(-1));assert.equal(attempt.status,rejected?400:200,name);
   if(!rejected)await waitForAsync(page,async owner=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.notesSessionId!==owner&&document.querySelector('#stage .keynope-modern-scene');},before.notesSessionId);
   return {before,after:await state()};
  }
  await open('old.md');await page.waitForFunction(()=>deck.pages[pageIndex]?.scene?.objects.length===49);
  const old=await stats();assert(old.caches.text.entries>=40);assert(old.caches.tables.entries>0);assert(old.caches.charts.entries>0);assert(old.stillAssets>0&&old.animatedAssets>0);
  const failure=await open('rejected.md',true),afterFailure=await stats();assert.equal(failure.after.notesSessionId,failure.before.notesSessionId);assert.equal(failure.after.version,failure.before.version);assert.deepEqual(afterFailure,old);assert(dialogs.some(message=>message.includes('Could not open presentation')));
  await open('new.md');await page.waitForFunction(()=>deck.pages[pageIndex]?.scene?.objects.length===5);await page.evaluate(()=>document.fonts.ready);
  const next=await stats();assert.equal(next.epoch,old.epoch+1);assert.equal(next.stillAssets,manifest.newStillAssets);assert.equal(next.animatedAssets,manifest.newAnimatedAssets);assert.equal(next.caches.tables.entries,0);assert.equal(next.caches.charts.entries,0);assert(next.caches.text.entries<10);assert(next.caches.sources.entries<old.caches.sources.entries);
  const rendered=await page.evaluate(async()=>{
   const page=deck.pages[pageIndex],scene=page.scene,stage=document.querySelector('#stage .keynope-modern-scene'),check=(ok,message)=>{if(!ok)throw Error(message);};
   for(const id of ['new-title','new-body','new-code','new-photo','new-movie'])check(stage.querySelector('[data-object-id="'+id+'"]'),'New owner missing '+id);
   check(!stage.textContent.includes('Dense text'),'Old text leaked into new document');
   await editorAction({action:'toggle-master-mode'});const response=await fetch('/api/editor/workspace?master=1&cols=245&rows=56');check(response.ok,'Hidden master preview rejected: '+await response.clone().text());const hidden=await response.json();check(JSON.stringify(hidden).includes('hidden-image'),'Hidden master asset no longer renders');await editorAction({action:'toggle-master-mode'});
   for(const [id,font]of [['new-title','c64'],['new-body','sans'],['new-code','mono']])check(scene.objects.find(o=>o.id===id).text.fontId===font,'New font lost: '+id);
   const movie=scene.objects.find(o=>o.id==='new-movie');check(movie.media.frames.map(f=>f.delayMs).join(',')==='90,130,170'&&movie.media.loopCount===2,'Animation timing lost');
   // The unified scene uses its own DOM text painter. Exercise the retained
   // legacy canvas painter directly as well; old export consumers still use it.
   const title=scene.objects.find(o=>o.id==='new-title'),line={row:2,col:10,element:0,role:'heading',parts:[{col:0,color:title.paint.color,text:'New document'}],trueType:{text:'New document',kind:'heading',size:title.text.size,width:220,height:7,query:''}};await KeynopeTrueType.ready;
   const canvas=document.createElement('canvas');canvas.width=1920;canvas.height=1080;
   KeynopeTrueType.draw(canvas.getContext('2d'),line,1920/245,1080/56,245,56);
   const js=KeynopeTrueType.cacheStats();check(js.painted.entries>0,'Actual legacy text canvas did not cache');
   // An oversized individual raster must bypass retention without flushing the
   // useful hot set. Use an ordinary text line with a 2050-square authored box.
   const oversized=structuredClone(line);oversized.trueType.width=2050*245/1920;oversized.trueType.height=2050*56/1080;
   KeynopeTrueType.draw(canvas.getContext('2d'),oversized,1920/245,1080/56,245,56);check(JSON.stringify(KeynopeTrueType.cacheStats())===JSON.stringify(js),'Oversized canvas evicted useful data or exceeded its budget');
   const s=await fetch('/api/editor/state').then(r=>r.json());await editorAction({action:'set-selection-property',slide:0,objectIds:['new-title'],property:'text.size',propertyValue:88,sceneMaster:false,sceneRevision:s.version});
   const changed=await fetch('/api/editor/state').then(r=>r.json());check(changed.version===s.version+1,'New document edit did not commit');await editorAction({action:'undo'});check(JSON.stringify((await fetch('/api/editor/state').then(r=>r.json())).slides)===JSON.stringify(s.slides),'New document Undo lost content');await editorAction({action:'redo'});
   const saved=await fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'saved.md'})}).then(r=>r.json());check(saved.content.includes('replacement-movie')&&!saved.content.includes('runtimeCaches'),'Portable sources missing or runtime counters persisted');
   return {saved,js,objects:scene.objects.length,hiddenMaster:true,oversizedCanvasBypass:true};
  });
  await page.screenshot({path:path.join(output,'new-document-'+engine+'.png')});
  fs.writeFileSync(path.join(output,'saved.md'),rendered.saved.content);await open('saved.md');
  const reopened=await stats();assert.equal(reopened.epoch,next.epoch+1);assert.equal(reopened.stillAssets,2);assert.equal(reopened.animatedAssets,1);
  const beforeBlank=(await state()).notesSessionId;await page.getByRole('button',{name:'New presentation',exact:true}).click();await page.getByRole('button',{name:'Use Blank template',exact:true}).click();
  await waitForAsync(page,async owner=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.untitled&&s.notesSessionId!==owner;},beforeBlank);
  const blank=await stats();assert.equal(blank.epoch,reopened.epoch+1);assert.equal(blank.stillAssets,0);assert.equal(blank.animatedAssets,0);for(const key of ['imageRows','imageFastRows','decodedImages','emojiRows'])assert.equal(blank.caches[key].entries,0,key);
  const js=await page.evaluate(()=>documentLifetimeProof.clears);assert(js.length>=4);assert(js.every(clear=>Object.values(clear.after).every(cache=>cache.entries===0&&cache.bytes===0)));assert(js.some(clear=>clear.before.painted.entries>0));
  assert.deepEqual(errors,[]);const report={passed:true,engine,browser:browser.version(),old,afterFailure,next,reopened,blank,js,objects:rendered.objects,hiddenMaster:rendered.hiddenMaster,oversizedCanvasBypass:rendered.oversizedCanvasBypass};fs.writeFileSync(path.join(output,'report.json'),JSON.stringify(report,null,2));console.log('PASS '+engine+': document lifetime, owned media, history and canvas budgets');
 }finally{await browser.close();server.close();}
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
