const {waitForAsync}=require("./wait_for_async.cjs");
// Actual shared editor selection, owned 50-label fixture; no cloud traffic.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright'),root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-selection-prep-site'),output='/tmp/keynope-selection-prep-proof',label=process.env.BENCH_LABEL||'optimized';
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
  fs.mkdirSync(output,{recursive:true});await new Promise(r=>server.listen(0,'127.0.0.1',r));const browser=await playwright.chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
  try{
    const context=await browser.newContext({viewport:{width:1500,height:1000}});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
    await context.addInitScript(()=>{let api;Object.defineProperty(window,'KeynopeScene',{configurable:true,get:()=>api,set:value=>{api=Object.freeze({...value,mount(host,...args){const view=value.mount(host,...args);if(host.closest?.('#stage'))window.__selectionView=view;return view;}});}});});
    const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',async d=>{assert.equal(d.type(),'confirm');await d.accept();});
    await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
    const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles({name:'Dense labels.md',mimeType:'text/markdown',buffer:fs.readFileSync(output+'/dense-labels.md')});
    await page.waitForFunction(()=>deck.pages[0]?.scene?.objects.filter(o=>o.id?.startsWith('label-')).length===50&&window.__selectionView?.element.isConnected);
    await page.evaluate(async()=>{await window.__selectionView.ready;window.__selectionNodes=new Map([...window.__selectionView.element.querySelectorAll('[data-object-id]')].map(n=>[n.dataset.objectId,n]));window.__selectionStats=window.__selectionView.renderStats();const original=window.fetch;window.fetch=(...args)=>{const pending=original(...args);if(String(args[0]).includes('/api/editor/action'))pending.then(r=>r.clone().json()).then(s=>{window.__selectionAccepted=s.selectionCapabilities?.targetIds.length||0;}).catch(()=>{});return pending;};});
    const records=[];
    for(let iteration=0;iteration<5;iteration++){
      await page.locator('.keynope-canvas-element[data-element="0"]').click();
      await waitForAsync(page,async()=>(await fetch('/api/editor/state').then(r=>r.json())).selectionCapabilities?.targetIds.length===1);
      const box=await page.locator('.keynope-canvas-overlay').boundingBox();assert(box);
      // Leave the navigator/inspector resize gutters outside the gesture.
      await page.keyboard.down('Shift');await page.mouse.move(box.x+8,box.y+8);await page.mouse.down();await page.mouse.move(box.x+box.width-8,box.y+box.height-8,{steps:4});
      await page.evaluate(()=>{window.__selectionAccepted=0;window.__selectionRelease=performance.now();window.__selectionPaintPromise=new Promise((resolve,reject)=>{const started=performance.now(),check=()=>{const state=document.querySelectorAll('.keynope-canvas-element.active');if(state.length===50&&window.__selectionAccepted===50){requestAnimationFrame(()=>requestAnimationFrame(()=>resolve(performance.now()-window.__selectionRelease)));}else if(performance.now()-started>10000)reject(Error('Selection did not reach 50 objects'));else requestAnimationFrame(check);};check();});});
      await page.mouse.up();await page.keyboard.up('Shift');
      let releaseToPaintMs;
      try{releaseToPaintMs=await page.evaluate(()=>window.__selectionPaintPromise);}catch(error){console.error(await page.evaluate(async()=>({accepted:window.__selectionAccepted,active:document.querySelectorAll('.keynope-canvas-element.active').length,candidates:document.querySelectorAll('.marquee-candidate').length,targets:(await fetch('/api/editor/state').then(r=>r.json())).selectionCapabilities?.targetIds,status:document.querySelector('.keynope-editor-status')?.textContent})));await page.screenshot({path:output+'/'+label+'-failure.png'});throw error;}
      const record=await page.evaluate(async elapsed=>{const state=await fetch('/api/editor/state').then(r=>r.json()),view=window.__selectionView;await view.ready;const stats=view.renderStats(),before=window.__selectionStats;for(const [id,node]of window.__selectionNodes)if(node!==view.element.querySelector('[data-object-id="'+id+'"]'))throw Error('Selection remounted '+id);return {releaseToPaintMs:elapsed,count:state.selectionCapabilities.targetIds.length,labelCount:state.selectionCapabilities.contexts['shape-label'].length,size:state.selectionCapabilities.properties['label.size'].value,objectMountDelta:stats.objectMounts-before.objectMounts,textRebuildDelta:stats.textRebuilds-before.textRebuilds};},releaseToPaintMs);
      assert.equal(record.count,50);assert.equal(record.labelCount,50);assert.equal(record.size,24);assert.equal(record.objectMountDelta,0);assert.equal(record.textRebuildDelta,0);records.push(record);
    }
    await page.getByRole('tab',{name:'Style',exact:true}).click();await page.locator('[data-selection-context="shape-label"] summary').click();await page.screenshot({path:output+'/'+label+'.png'});
    assert.deepEqual(errors,[]);fs.writeFileSync(output+'/'+label+'.json',JSON.stringify({label,records},null,2));console.log('PASS: real shift-marquee selection, 50 accepted label capabilities, retained object/text owners. '+JSON.stringify(records));
  }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);process.exitCode=1;server.close();});
