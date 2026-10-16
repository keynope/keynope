// Targeted single-slide edit measurements; not participant/workshop load tests.
const fs=require('node:fs'),path=require('node:path'),http=require('node:http'),assert=require('node:assert/strict'),os=require('node:os');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),fixture=process.env.TEST_DECK||'/tmp/keynope-dense-editing.md',output=path.resolve(process.env.TEST_OUTPUT||'/tmp/keynope-dense-editing-proof');
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 fs.mkdirSync(output,{recursive:true});await new Promise(r=>server.listen(0,'127.0.0.1',r));const browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
 try {
  const context=await browser.newContext({viewport:{width:1440,height:1000},reducedMotion:'reduce'});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
  const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const picker=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await picker).setFiles({name:'dense.md',mimeType:'text/markdown',buffer:fs.readFileSync(fixture)});
  await page.waitForFunction(()=>deck.pages[pageIndex]?.scene?.objects.length===49);await page.evaluate(()=>document.fonts.ready);
  const result=await page.evaluate(async()=>{
   const state=()=>fetch('/api/editor/state').then(r=>r.json()),settle=()=>new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)));
   const phases=[],originalRequest=window.keynopeWasmRequest,originalWorkspace=window.keynopeWasmWorkspace;
   window.keynopeWasmRequest=(...args)=>{const start=performance.now(),raw=originalRequest(...args);phases.push({path:String(args[1]),ms:performance.now()-start,characters:raw.length});return raw;};
   window.keynopeWasmWorkspace=(...args)=>{args[1]=true;const start=performance.now(),raw=originalWorkspace(...args),ms=performance.now()-start,envelope=JSON.parse(raw),metrics=envelope.status===200?JSON.parse(envelope.body).projectionMetrics:null;phases.push({path:'workspace',ms,characters:raw.length,structuredComparisonHits:metrics?.structuredComparisonHits||0});return raw;};
   await editorAction({action:'select-element',elementData:{id:'dense-text-0'}});await settle();
   const host=document.querySelector('#stage .keynope-modern-scene');if(!host)throw Error('Missing live scene');
   const owners=new Map([...host.querySelectorAll('[data-object-id]')].map(n=>[n.dataset.objectId,n])),images=[...host.querySelectorAll('img')].map(n=>({node:n,source:n.src}));
   let added=0;const observer=new MutationObserver(records=>{for(const record of records)added+=record.addedNodes.length;});observer.observe(host,{childList:true,subtree:true});
   const samples=[],commands=[],records=[],paintPhases=[];
   for(let i=0;i<16;i++) {
    const s=await state(),phaseStart=phases.length,start=performance.now();await editorAction({action:'set-selection-property',slide:0,sceneMaster:false,sceneRevision:s.version,objectIds:['dense-text-0'],property:'paint.color',propertyValue:i%2?'#ff0055':'#55aaff'});commands.push(performance.now()-start);paintPhases.push(phases.slice(phaseStart));await settle();
    const elapsed=performance.now()-start;
    if(!phases.slice(phaseStart).some(p=>p.path==='workspace'&&p.structuredComparisonHits>=2))throw Error('Real workspace did not reuse table/chart comparisons');
    let replaced=0;for(const [id,node]of owners)if(host.querySelector('[data-object-id="'+CSS.escape(id)+'"]')!==node)replaced++;
    if(document.querySelector('#stage .keynope-modern-scene')!==host||replaced)throw Error('Edit replaced unaffected scene/owners: '+replaced);
    for(const image of images)if(!host.contains(image.node)||image.node.src!==image.source)throw Error('Text paint rebuilt an image');
    samples.push(elapsed);
   }
   for(const kind of ['table','chart']) {
    await editorAction({action:'select-element',elementData:{id:'dense-'+kind}});await settle();
    for(let i=0;i<6;i++) {
     const s=await state(),data=structuredClone(kind==='table'?s.selectedTable.data:s.selectedChart.data);
     if(kind==='table')data.cells[10][10].text=i%2?'A':'B';else data.series[0].values[0]=i%2?1:2;
     const beforeAdded=added,phaseStart=phases.length,start=performance.now();await editorAction({action:kind==='table'?'set-table':'set-chart-data',slide:0,sceneMaster:false,sceneRevision:s.version,objectId:'dense-'+kind,[kind+'Data']:data});const commandMS=performance.now()-start,commandPhases=phases.slice(phaseStart);await settle();
     let replaced=0;for(const [id,node]of owners)if(host.querySelector('[data-object-id="'+CSS.escape(id)+'"]')!==node)replaced++;
     if(replaced||document.querySelector('#stage .keynope-modern-scene')!==host)throw Error(kind+' edit replaced scene/owners: '+replaced);
     for(const image of images)if(!host.contains(image.node)||image.node.src!==image.source)throw Error(kind+' edit rebuilt an image');
     records.push({kind,commandMS,totalMS:performance.now()-start,addedNodes:added-beforeAdded,phases:commandPhases});
    }
   }
   const layoutCounters=[];
   const verifyProjection=async()=>{
    const response=await fetch('/api/editor/scene?slide=0&timing=1');if(!response.ok)throw Error(await response.text());const cold=await response.json(),warm=structuredClone(deck.pages[pageIndex].scene);
    const counters={builds:Number(response.headers.get('X-Keynope-Layout-Builds')),hits:Number(response.headers.get('X-Keynope-Layout-Cache-Hits'))};layoutCounters.push(counters);if(counters.builds!==0||counters.hits!==1)throw Error('Dense unchanged geometry rebuilt: '+JSON.stringify(counters));
    delete warm.revision;delete cold.revision;delete warm.slideIndex;delete cold.slideIndex;
    if(JSON.stringify(warm)!==JSON.stringify(cold))throw Error('Reconstructed workspace differs from independently requested scene endpoint');
   };
   await verifyProjection();
   const recoveries=[];
   for(const failure of ['stale-revision','corrupt-order']) {
    const workspace=window.keynopeWasmWorkspace;let corrupted=false,recovered=false;
    window.keynopeWasmWorkspace=(...args)=>{
     const raw=workspace(...args),envelope=JSON.parse(raw);if(envelope.status!==200)return raw;const body=JSON.parse(envelope.body);
     if(corrupted&&!args[0])recovered=true;
     const patch=body.pages?.find(page=>page.scenePatch)?.scenePatch;
     if(!corrupted&&body.baseRevision&&patch){corrupted=true;if(failure==='stale-revision')body.baseRevision=-1;else patch.order[0]='missing-object';envelope.body=JSON.stringify(body);return JSON.stringify(envelope);}
     return raw;
    };
    const s=await state();await editorAction({action:'set-selection-property',slide:0,sceneMaster:false,sceneRevision:s.version,objectIds:['dense-text-0'],property:'paint.color',propertyValue:failure==='stale-revision'?'#ffaa00':'#00ffaa'});await settle();window.keynopeWasmWorkspace=workspace;
    if(!corrupted||!recovered)throw Error('Missing deliberate full recovery for '+failure);await verifyProjection();recoveries.push(failure);
   }
   observer.disconnect();
   window.keynopeWasmRequest=originalRequest;window.keynopeWasmWorkspace=originalWorkspace;
   return {objects:deck.pages[pageIndex].scene.objects.length,tableCells:host.querySelectorAll('td,th').length,images:images.length,ownerNodes:owners.size,addedNodes:added,samples,commands,paintPhases,records,recoveries,sceneEndpointParity:true,layoutCounters,heap:performance.memory?{used:performance.memory.usedJSHeapSize,total:performance.memory.totalJSHeapSize}:null};
  });
  assert.deepEqual(errors,[]);const values=result.samples.slice(2).sort((a,b)=>a-b);result.medianMS=values[Math.floor(values.length/2)];result.p95MS=values[Math.min(values.length-1,Math.floor(values.length*.95))];
  const report={machine:{platform:os.platform(),arch:os.arch(),cpu:os.cpus()[0].model},browser:browser.version(),viewport:{width:1440,height:1000},measurement:'Accepted WASM text-colour command plus two animation frames; excludes initial load/state read',result};
  fs.writeFileSync(path.join(output,'report.json'),JSON.stringify(report,null,2));await page.screenshot({path:path.join(output,'dense-editor.png')});console.log(JSON.stringify({machine:report.machine,browser:report.browser,objects:result.objects,cells:result.tableCells,medianMS:result.medianMS,p95MS:result.p95MS,report:path.join(output,'report.json')}));
 }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);server.close();process.exitCode=1;});
