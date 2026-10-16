// Actual WASM navigation-reference lifecycle, not a JavaScript model mock.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),output=process.env.KEYNOPE_SLIDE_LINK_EVIDENCE||'/tmp/keynope-slide-links-proof';
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));const browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
 try{
  const context=await browser.newContext({viewport:{width:1440,height:1000},reducedMotion:'reduce'});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());
  const page=await context.newPage(),errors=[];page.on('pageerror',error=>errors.push(error.message));page.on('dialog',dialog=>dialog.accept());
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const open=async(name,content)=>{const picker=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await picker).setFiles({name,mimeType:'text/markdown',buffer:Buffer.from(content)});await page.waitForFunction(name=>document.title==='Keynope — '+name||document.title==='Keynope — '+name+' *',name);await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));};
  const records=[];
  for(const operation of ['add-slide','clone-slide','reorder-slide','delete-before','delete-target']){
   await open(operation+'.md',fs.readFileSync(path.join(output,'links.md')));
   const record=await page.evaluate(async operation=>{
    const state=()=>fetch('/api/editor/state').then(response=>response.json()),before=await state();
    if(operation==='delete-target')await editorAction({action:'select-slide',slide:2});
    const action={action:operation.startsWith('delete-')?'delete-slide':operation};if(operation==='reorder-slide')Object.assign(action,{slide:2,value:0});
    await editorAction(action);const after=await state(),target=after.slides.findIndex(slide=>slide.id==='target');
    const check=slide=>{for(const element of slide.elements||[])if(['Jump','Jump from master','Jump from layout'].includes(element.text)){const q=new URLSearchParams(element.query),value=q.get('slide')||(q.get('link')||'').replace(/^#/,'');if(target<0?value!=='':Number(value)!==target+1)throw Error(operation+' wrong authored link '+value);}};
    after.slides.forEach(check);check(after.masters.base.slide);after.masters.layouts.forEach(layout=>check(layout.slide));
    const scene=await fetch('/api/editor/scene?slide=0').then(response=>response.json());
    for(const object of scene.objects)if(object.link?.startsWith('#')&&(target<0||object.link!=='#'+(target+1)))throw Error(operation+' stale scene link '+object.link);
    const live=deck.pages[pageIndex].scene;for(const object of live.objects)if(object.link?.startsWith('#')&&(target<0||object.link!=='#'+(target+1)))throw Error(operation+' stale live workspace link');
    await editorAction({action:'undo'});if(JSON.stringify((await state()).slides)!==JSON.stringify(before.slides))throw Error(operation+' undo did not restore slides');
    await editorAction({action:'redo'});if(JSON.stringify((await state()).slides)!==JSON.stringify(after.slides))throw Error(operation+' redo did not restore links');
    const saved=await fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Links saved.md'})}).then(response=>response.json());
    return {operation,target,slides:after.slides,masters:after.masters,content:saved.content};
   },operation);
   assert.equal(typeof record.content,'string');await open(operation+'-reopened.md',record.content);
   const reopened=await page.evaluate(()=>fetch('/api/editor/state').then(response=>response.json()));assert.deepEqual(reopened.slides,record.slides);assert.deepEqual(reopened.masters,record.masters);
   delete record.content;delete record.slides;delete record.masters;records.push(record);
  }
  await open('clipboard-source.md',fs.readFileSync(path.join(output,'links.md')));
  const bundle=await page.evaluate(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());const r=await fetch('/api/editor/clipboard',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({version:s.version,ids:['jump']})});if(!r.ok)throw Error(await r.text());return r.json();});
  assert.deepEqual(bundle.slideLinks,{jump:'target'});
  const clipboardRecords=[];
  for(const scenario of ['reordered','missing','legacy-bundle']){
   await open('clipboard-'+scenario+'.md',fs.readFileSync(path.join(output,'links.md')));
   const data=structuredClone(bundle);if(scenario==='legacy-bundle')delete data.slideLinks;
   const result=await page.evaluate(async({scenario,data})=>{
    if(scenario==='reordered')await editorAction({action:'reorder-slide',slide:2,value:0});
    if(scenario==='missing'){await editorAction({action:'select-slide',slide:2});await editorAction({action:'delete-slide'});}
    const state=()=>fetch('/api/editor/state').then(r=>r.json()),before=await state(),ids=new Set(before.slides[before.current].elements.map(e=>e.id));
    await editorAction({action:'paste-objects',clipboard:data});const after=await state(),added=after.slides[after.current].elements.filter(e=>!ids.has(e.id));
    if(added.length!==1)throw Error('Clipboard did not add exactly one selected object');const q=new URLSearchParams(added[0].query),link=q.get('slide')||q.get('link')||'',expected=scenario==='reordered'?'1':'';
    if(link!==expected)throw Error(scenario+' redirected paste to '+link);
    const scene=deck.pages[pageIndex].scene,object=scene.objects.find(o=>o.id===added[0].id);if(!object||(object.link||'')!==(expected?'#'+expected:''))throw Error('Stale pasted scene link');
    const warning=[...document.querySelectorAll('.keynope-engagement-toast')].some(n=>n.textContent.includes('original destination could not be identified'));
    if((scenario!=='reordered')!==warning)throw Error('Missing or unexpected visible paste warning');
    document.querySelectorAll('.keynope-engagement-toast').forEach(n=>n.remove());
    await editorAction({action:'undo'});if(JSON.stringify((await state()).slides)!==JSON.stringify(before.slides))throw Error('Clipboard undo failed');
    await editorAction({action:'redo'});if(JSON.stringify((await state()).slides)!==JSON.stringify(after.slides))throw Error('Clipboard redo failed');
    const saved=await fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Pasted.md'})}).then(r=>r.json());
    return {scenario,link,warning,slides:after.slides,content:saved.content};
   },{scenario,data});
   await open('clipboard-'+scenario+'-reopened.md',result.content);const reopened=await page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));assert.deepEqual(reopened.slides,result.slides);
   delete result.slides;delete result.content;clipboardRecords.push(result);
  }
  assert.deepEqual(errors,[]);fs.writeFileSync(path.join(output,'report.json'),JSON.stringify({browser:browser.version(),records,clipboardRecords,errors},null,2));
  console.log('PASS: five structural edits plus clipboard identity remapping/missing/legacy warnings, live scene links, Undo/Redo and save/reopen.');
 }finally{await browser.close();server.close();}
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
