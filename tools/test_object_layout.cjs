// Real WASM commands and production diagnostics, not a parallel layout mock.
'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright'),engine=process.env.TEST_BROWSER||'chromium';
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),output=path.resolve(process.env.TEST_OUTPUT||'/tmp/keynope-object-layout-proof'),fixture=process.env.TEST_DECK||path.join(output,'dense.md');
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 fs.mkdirSync(output,{recursive:true});assert(fs.statSync(fixture).size>0);await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 const browser=await pw[engine].launch({headless:true,...(engine==='webkit'?{}:{executablePath:process.env.CHROME_PATH})});
 try{
  const context=await browser.newContext({viewport:{width:1440,height:1000},reducedMotion:'reduce'});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());
  const page=await context.newPage(),errors=[];page.on('pageerror',error=>errors.push(error.message));page.on('dialog',dialog=>dialog.accept());await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const picker=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await picker).setFiles(fixture);await page.waitForFunction(()=>deck.pages[pageIndex]?.scene?.objects.length===49);await page.evaluate(()=>document.fonts.ready);
  const report=await page.evaluate(async()=>{
   const state=()=>fetch('/api/editor/state').then(r=>r.json()),frame=()=>new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r))),normalize=value=>{value=structuredClone(value);delete value.revision;delete value.slideIndex;return value;};
   const difference=(a,b,at='scene')=>{if(Object.is(a,b))return null;if(!a||!b||typeof a!=='object'||typeof b!=='object')return {at,actual:String(a).slice(0,100),expected:String(b).slice(0,100)};const ak=Object.keys(a).sort(),bk=Object.keys(b).sort();if(JSON.stringify(ak)!==JSON.stringify(bk))return {at,actualKeys:ak,expectedKeys:bk};for(const key of ak){const changed=difference(a[key],b[key],at+'.'+key);if(changed)return changed;}return null;};
   const stage=document.querySelector('#stage .keynope-modern-scene'),owners=new Map([...stage.querySelectorAll('[data-object-id]')].map(node=>[node.dataset.objectId,node])),results=[];
   async function mutate(name,id,command,allowTargetReplacement=false){
    await editorAction({action:'select-element',elementData:{id}});const before=await state(),start=performance.now();
    // The hosted fetch bridge eagerly applies workspace before returning an
    // action response. Call its same production Go handler directly so the
    // diagnostic below measures the first projection, not an already warm one.
    const response=JSON.parse(window.keynopeWasmRequest('POST','/api/editor/action',JSON.stringify({...command,slide:0,sceneMaster:false,sceneRevision:before.version}),'application/problem+json'));if(response.status!==200)throw Error(name+': '+response.body);const accepted=JSON.parse(response.body);if(accepted.version!==before.version+1)throw Error(name+' did not commit once');
    const measured=await fetch('/api/editor/scene?slide=0&timing=1');if(!measured.ok)throw Error(await measured.text());const count=key=>{const value=measured.headers.get(key);if(value===null)throw Error('Missing diagnostic '+key);return Number(value);};
    const metrics={full:count('X-Keynope-Layout-Builds'),built:count('X-Keynope-Object-Layout-Builds'),reused:count('X-Keynope-Object-Layout-Cache-Hits'),whole:count('X-Keynope-Layout-Cache-Hits'),artwork:count('X-Keynope-Object-Artwork-Layouts')},projected=await measured.json();
    if(metrics.full!==0||metrics.built!==1||metrics.reused<40)throw Error(name+' rebuilt unrelated geometry: '+JSON.stringify(metrics));
    // Replay the now-equal command through the normal hosted bridge. It must
    // remain a no-op but paint the accepted workspace, whose eager refresh was
    // deliberately held back only for the first-projection measurement above.
    await editorAction({...command,slide:0,sceneMaster:false,sceneRevision:accepted.version});if((await state()).version!==accepted.version)throw Error(name+' equal replay created another edit');await frame();const changed=difference(normalize(deck.pages[pageIndex].scene),normalize(projected));if(changed)throw Error(name+' canvas differs from accepted scene: '+JSON.stringify(changed));
    for(const [owner,node]of owners){const current=stage.querySelector('[data-object-id="'+CSS.escape(owner)+'"]');if(current!==node&&!(allowTargetReplacement&&owner===id))throw Error(name+' replaced owner '+owner);if(owner===id)owners.set(owner,current);}if(document.querySelector('#stage .keynope-modern-scene')!==stage)throw Error(name+' remounted the scene');
    results.push({name,id,...metrics,totalMS:performance.now()-start});
   }
   for(const size of [29,34])await mutate('text-size-'+size,'dense-text-0',{action:'set-selection-property',objectIds:['dense-text-0'],property:'text.size',propertyValue:size});
   let s=await state(),index=s.slides[0].elements.findIndex(e=>e.id==='dense-text-0'),element=structuredClone(s.slides[0].elements[index]);element.text='Edited content';await mutate('text-content',element.id,{action:'update-element',element:index,elementData:element});
   s=await state();index=s.slides[0].elements.findIndex(e=>e.kind==='image');element=structuredClone(s.slides[0].elements[index]);let query=new URLSearchParams(element.query);query.set('width','32.5');element.query=query.toString();await mutate('image-size',element.id,{action:'update-element',element:index,elementData:element});
   s=await state();element=structuredClone(s.slides[0].elements.find(e=>e.id===element.id));query=new URLSearchParams(element.query);query.set('image-style','retro');element.query=query.toString();await mutate('image-retro',element.id,{action:'update-element',element:index,elementData:element},true);
   await mutate('text-size-with-retro','dense-text-0',{action:'set-selection-property',objectIds:['dense-text-0'],property:'text.size',propertyValue:39});if(results.at(-1).artwork!==1)throw Error('Retro artwork did not use the isolated object path');
   const committed=normalize(deck.pages[pageIndex].scene);await editorAction({action:'undo'});await editorAction({action:'redo'});await frame();const restored=difference(normalize(deck.pages[pageIndex].scene),committed);if(restored)throw Error('Incremental edit lost exact Undo/Redo: '+JSON.stringify(restored));return {passed:true,owners:owners.size,results};
  });
  assert.equal(report.owners,49);assert.deepEqual(errors,[]);await page.screenshot({path:path.join(output,'object-layout-'+engine+'.png')});fs.writeFileSync(path.join(output,'report.json'),JSON.stringify({engine,browser:browser.version(),...report},null,2));console.log('PASS '+engine+': '+JSON.stringify(report));
 }finally{await browser.close();server.close();}
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
