// Real native Go HTTP actions + generated editor, measured without launching a
// Mac application. This is not an interactive AppKit input-to-photon claim.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),cp=require('node:child_process');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=fs.mkdtempSync('/tmp/keynope-native-editing-');
const comparison=process.env.KEYNOPE_NATIVE_LATENCY_COMPARISON||'demand';
assert(['demand','scene-patches','dependencies'].includes(comparison));
(async()=>{
  let logs='',code=null,browser;
  const go=cp.spawn('go',['test','-run','^TestNativeEditingLatencyBrowserFixture$','-count=1','./internal/app'],
    {env:{...process.env,GOCACHE:process.env.GOCACHE||'/tmp/keynope-go-cache',KEYNOPE_NATIVE_EDITING_EVIDENCE:root},stdio:['ignore','pipe','pipe']});
  go.stdout.on('data',b=>logs+=b);go.stderr.on('data',b=>logs+=b);
  const exited=new Promise(resolve=>go.on('exit',value=>{code=value;resolve(value);}));
  try{
    const deadline=Date.now()+90000;
    while(!fs.existsSync(path.join(root,'connection.json'))){
      if(code!==null)throw Error('Native fixture failed: '+logs);
      if(Date.now()>deadline)throw Error('Native fixture startup timed out');
      await new Promise(r=>setTimeout(r,50));
    }
    const {url}=JSON.parse(fs.readFileSync(path.join(root,'connection.json'))),reports=[];
    for(const engine of ['chromium','webkit']){
      browser=await pw[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
      const context=await browser.newContext({viewport:{width:1440,height:1000},reducedMotion:'reduce'});
      await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
      const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
      await page.goto(url);
      await page.waitForFunction(()=>typeof editorAction==='function'&&document.querySelectorAll('#stage .keynope-modern-scene [data-object-id]').length>=49,{},{timeout:90000});
      await page.evaluate(()=>document.fonts.ready);
      const report=await page.evaluate(async comparison=>{
        const state=()=>fetch('/api/editor/state').then(r=>r.json()),frame=()=>new Promise(r=>requestAnimationFrame(r));
        const target=()=>deck.pages[pageIndex]?.scene?.objects.find(o=>o.id==='dense-text-0');
        const visible=()=>document.querySelector('#stage [data-object-id="dense-text-0"] .keynope-shaped-text');
        await editorAction({action:'select-element',elementData:{id:'dense-text-0'}});
        const scene=document.querySelector('#stage .keynope-modern-scene'),owners=new Map([...scene.querySelectorAll('[data-object-id]')].map(n=>[n.dataset.objectId,n]));
        const images=[...scene.querySelectorAll('img')].map(n=>({node:n,source:n.src}));
        const sync=syncPresenterStateOnce,nativeFetch=fetch,samples=[],dependencyChecks=[];
        let usePatches=true;
        if(comparison==='scene-patches')fetch=(input,options)=>{
          if(!usePatches&&typeof input==='string'&&input.startsWith('/slides?')){
            const url=new URL(input,location.href);url.searchParams.delete('scenePatches');input=url.pathname+url.search;
          }
          return nativeFetch(input,options);
        };
        for(const demand of [false,true]){
          // Disable only the chosen demand/scene-patch feature in the matched
          // baseline. Ordinary 60 ms polling is identical in both arms.
          usePatches=demand;
          if(comparison==='dependencies'){
            // Both browser engines share one native server. Prime a distinct
            // colour so the next engine's first baseline edit cannot be a
            // no-op that leaves the previous engine's candidate count intact.
            const current=await state();
            await editorAction({action:'set-selection-property',slide:0,sceneMaster:false,sceneRevision:current.version,objectIds:['dense-text-0'],property:'paint.color',propertyValue:'#abcdef'});
            const response=await nativeFetch('/test/native-dependencies?enabled='+(demand?'1':'0'));
            if(!response.ok)throw Error(await response.text());
          }
          syncPresenterStateOnce=request=>request&&!demand&&comparison==='demand'?Promise.resolve():sync(request);
          for(let i=0;i<12;i++){
            const s=await state(),colour=i%2?'#ff0055':'#55aaff',start=performance.now();
            await editorAction({action:'set-selection-property',slide:0,sceneMaster:false,sceneRevision:s.version,objectIds:['dense-text-0'],property:'paint.color',propertyValue:colour});
            const commandMS=performance.now()-start,immediate=target()?.paint.color===colour;
            for(let tries=0;target()?.paint.color!==colour;tries++){
              if(tries>180)throw Error('Accepted native edit never reached canvas');await frame();
            }
            // A scene object alone is insufficient: verify the retained painter.
            const expected=colour==='#ff0055'?'rgb(255, 0, 85)':'rgb(85, 170, 255)';
            if(!visible()||getComputedStyle(visible()).color!==expected)throw Error('Model updated without visible text paint');
            await frame();
            samples.push({demand,index:i,commandMS,visibleMS:performance.now()-start,immediate});
            for(const [id,node]of owners)if(scene.querySelector('[data-object-id="'+CSS.escape(id)+'"]')!==node)throw Error('Native edit replaced owner '+id);
            for(const {node,source}of images)if(!scene.contains(node)||node.src!==source)throw Error('Native paint replaced media');
            if(comparison==='dependencies'){
              const check=await nativeFetch('/test/native-dependencies').then(r=>r.json());
              if(check.slides!==100||check.projected!==1||check.considered!==(demand?1:100))throw Error('Native dependency candidates are wrong: '+JSON.stringify(check));
              dependencyChecks.push({indexed:demand,...check});
            }
          }
        }
        syncPresenterStateOnce=sync;fetch=nativeFetch;
        const s=await state();await editorAction({action:'undo'});if(target()?.paint.color!=='#55aaff')throw Error('Immediate Undo scene lagged');
        await editorAction({action:'redo'});if(target()?.paint.color!=='#ff0055')throw Error('Immediate Redo scene lagged');
        [...document.querySelectorAll('[role="tab"]')].find(n=>n.textContent.trim()==='Text')?.click();
        const metrics=[];
        for(const [property,caption,value]of [['text.size','Font size',25],['text.size','Font size',33],['text.width','TrueType width percent',80],['text.width','TrueType width percent',120]]){
          const current=await state();
          await editorAction({action:'set-selection-property',slide:0,sceneMaster:false,sceneRevision:current.version,objectIds:['dense-text-0'],property,propertyValue:value});
          const input=document.querySelector('[aria-label="'+caption+'"]');
          if(!input||input.value!==String(value))throw Error('Accepted native inspector lagged: '+caption+' '+input?.value+' != '+value);
          metrics.push({property,value});
        }
        // Miss two separately accepted publications while this client is not
        // polling. Catch up from their exact old scene in one object patch.
        syncPresenterStateOnce=()=>Promise.resolve();if(presenterStateSync)await presenterStateSync;
        const catchupBase=presenterDeckVersion;
        const catchupColours=[['dense-text-0',target()?.paint.color==='#22cc77'?'#7744dd':'#22cc77'],['dense-text-1',deck.pages[pageIndex].scene.objects.find(object=>object.id==='dense-text-1').paint.color==='#7744dd'?'#22cc77':'#7744dd']];
        for(const [id,colour]of catchupColours){
          const version=await state(),response=await nativeFetch('/api/editor/action',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'set-selection-property',slide:0,sceneMaster:false,sceneRevision:version.version,objectIds:[id],property:'paint.color',propertyValue:colour})});
          if(!response.ok)throw Error(await response.text());const accepted=await response.json();
          if(accepted.version<=version.version)throw Error('Catch-up fixture attempted a no-op instead of a publication');
          await nativeFetch('/state?editorRevision='+accepted.version+'&editorOwner='+encodeURIComponent(accepted.notesSessionId));
        }
        if(presenterDeckVersion!==catchupBase)throw Error('Slow-view fixture unexpectedly consumed a publication');
        let catchup;
        fetch=async(input,options)=>{const response=await nativeFetch(input,options);if(typeof input==='string'&&input.startsWith('/slides?'))catchup=await response.clone().json();return response;};
        syncPresenterStateOnce=sync;await sync();fetch=nativeFetch;
        const changed=catchup?.patchedPages?.flatMap(page=>page.scenePatch.changed.map(object=>object.id)).sort();
        if(JSON.stringify(changed)!==JSON.stringify(['dense-text-0','dense-text-1'])||catchup.baseVersion!==catchupBase)throw Error('Slow native view missed/coalesced unrelated objects: '+JSON.stringify(changed));
        for(const [id,colour]of catchupColours){const css=colour==='#22cc77'?'rgb(34, 204, 119)':'rgb(119, 68, 221)';if(getComputedStyle(document.querySelector('#stage [data-object-id="'+id+'"] .keynope-shaped-text')).color!==css)throw Error('Slow-view patch did not paint '+id);}
        // Stale demand must not fetch or alter a replacement document/view.
        const before=deck.pages;
        await syncPresenterStateOnce({revision:s.version,owner:'stale',current:()=>false});
        if(deck.pages!==before)throw Error('Stale demand changed the current workspace');
        // Corrupt only a wire baseline stamp, not the live document's identity:
        // editing that identity itself legitimately replaces a mounted scene.
        // The real decoder must recover without applying any corrupt object.
        const current=deck.pages[pageIndex];
        let recoveries=0,injected=false;
        fetch=async(input,options)=>{
          const isSlides=typeof input==='string'&&input.startsWith('/slides?');
          if(isSlides&&!new URL(input,location.href).searchParams.has('since'))recoveries++;
          const response=await nativeFetch(input,options);
          if(isSlides&&!injected&&response.ok){
            const data=await response.clone().json();
            if(data.patchedPages?.length){injected=true;data.patchedPages[0].scenePatch.slideId='missing-client-baseline';return new Response(JSON.stringify(data),{status:200,headers:{'Content-Type':'application/json'}});}
          }
          return response;
        };
        const latest=await state();await editorAction({action:'set-selection-property',slide:0,sceneMaster:false,sceneRevision:latest.version,objectIds:['dense-text-0'],property:'paint.color',propertyValue:'#55aaff'});
        fetch=nativeFetch;
        if(!injected||!recoveries||deck.pages[pageIndex].scene.slideId!==current.scene.slideId||target().paint.color!=='#55aaff')throw Error('Native missing-baseline recovery failed');
        for(const [id,node]of owners)if(scene.querySelector('[data-object-id="'+CSS.escape(id)+'"]')!==node)throw Error('Recovery replaced retained owner '+id);
        return {comparison,samples,dependencyChecks,owners:owners.size,images:images.length,revision:s.version,metrics,recoveries,catchup:{baseVersion:catchupBase,version:catchup.version,changed}};
      },comparison);
      const median=items=>{items.sort((a,b)=>a-b);return items[Math.floor(items.length/2)];};
      for(const demand of [false,true]){
        const values=report.samples.filter(s=>s.demand===demand&&s.index>=2);
        report[demand?'accepted':'baseline']={commandMS:median(values.map(s=>s.commandMS)),visibleMS:median(values.map(s=>s.visibleMS)),immediate:values.filter(s=>s.immediate).length,samples:values.length};
      }
      assert.equal(report.accepted.immediate,report.accepted.samples,'Accepted command completed before native scene publication');
      if(comparison!=='demand')assert.equal(report.baseline.immediate,report.baseline.samples,'Matched baseline must use the same immediate publication policy');
      assert.deepEqual(errors,[]);await page.screenshot({path:path.join(root,engine+'.png')});
      reports.push({engine,browser:browser.version(),...report});
      console.log(JSON.stringify({engine,comparison,baseline:report.baseline,accepted:report.accepted,owners:report.owners,recoveries:report.recoveries,root}));
      await browser.close();browser=null;
    }
    fs.writeFileSync(path.join(root,'report.json'),JSON.stringify({passed:true,reports},null,2));
    console.log('PASS: authoritative accepted native canvas, retained owners/media, immediate Undo/Redo. '+root);
  }finally{
    if(browser)await browser.close();fs.writeFileSync(path.join(root,'stop'),'done');
    const result=await Promise.race([exited,new Promise(r=>setTimeout(()=>r('running'),10000))]);
    if(result==='running'){go.kill('SIGTERM');await exited;throw Error('Native fixture did not stop');}
    if(result!==0)throw Error('Native fixture failed: '+logs);
  }
})().catch(error=>{console.error(error);process.exitCode=1;});
