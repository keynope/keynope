// Real WASM actions and opt-in normal-page projection counters, owned deck.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-workspace-consumer-site'),output='/tmp/keynope-workspace-consumer-proof';
const server=http.createServer((req,res)=>{
  let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);
  if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}
  res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm','.json':'application/json'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));
});
(async()=>{
  fs.mkdirSync(output,{recursive:true});await new Promise(r=>server.listen(0,'127.0.0.1',r));
  const browser=await playwright.chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
  try{
    const context=await browser.newContext({viewport:{width:1440,height:1000}});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
    const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',async d=>{assert.equal(d.type(),'confirm');await d.accept();});
    await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
    const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles({name:'Consumers.md',mimeType:'text/markdown',buffer:fs.readFileSync(output+'/consumers.md')});
    await page.waitForFunction(()=>deck.pages.length===5&&deck.pages[0]?.scene?.objects.some(o=>o.table));
    const result=await page.evaluate(async()=>{
      const original=window.keynopeWasmWorkspace,reports=[],records=[];
      const normal=JSON.parse(original()).body;if(JSON.parse(normal).projectionMetrics)throw Error('Metrics leaked into ordinary request');
      window.keynopeWasmWorkspace=(...args)=>{const raw=original(args[0]||0,true),e=JSON.parse(raw);if(e.status===200)reports.push(JSON.parse(e.body));return raw;};
      window.keynopeWasmWorkspace(0);
      const state=()=>fetch('/api/editor/state').then(r=>r.json());
      const act=async data=>{const r=await fetch('/api/editor/action',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(data)});if(!r.ok)throw Error(data.action+': '+await r.text());await r.json();};
      await act({action:'select-slide',slide:0});
      const metrics=()=>reports.at(-1).projectionMetrics;
      async function check(name,command,want){
        const before=metrics(),start=reports.length;await command();const after=metrics(),misses=after.normalPageMisses-before.normalPageMisses;
        const slides=[...new Set(reports.slice(start).filter(r=>!r.master).flatMap(r=>r.projectionMetrics.normalReplacedSlides||[]))].sort((a,b)=>a-b);
        if(misses!==want.length||JSON.stringify(slides)!==JSON.stringify(want))throw Error(name+' rebuilt '+JSON.stringify({misses,slides,after,want}));
        records.push({name,misses,slides,resolutions:after.normalSourceResolutions-before.normalSourceResolutions});
      }
      const editMaster=async(id,color)=>{
        await act({action:'toggle-master-mode'});let s=await state();const index=s.masters.layouts.findIndex(l=>l.id===id);
        if(index<0)throw Error('Missing master '+id);await act({action:'select-slide',slide:index+1});s=await state();const slide=structuredClone(s.masters.layouts[index].slide);slide.bg=color;slide.bgSet=true;
        await act({action:'update-slide',slideData:slide});await act({action:'toggle-master-mode'});
      };
      await check('unused master',()=>editMaster('unused-layout','#ddaa77'),[]);
      await check('used layout explicit override elsewhere',()=>editMaster('alpha','#ffeeaa'),[0]);
      const background=document.querySelector('#stage .keynope-scene-background-media');
      if(!background||background.dataset.objectId||background.style.zIndex!=='0')throw Error('Background still owns a selectable/foreground slot');
      await check('notes',()=>act({action:'update-slide-notes',slide:0,notes:'Private workshop notes'}),[]);
      if(background!==document.querySelector('#stage .keynope-scene-background-media'))throw Error('Notes replaced background media');
      const table=deck.pages[0].scene.objects.find(o=>o.table),data=table.table;
      await act({action:'select-element',elementData:{id:'table-0'}});const s=await state(),payload=structuredClone(s.selectedTable.data);payload.cells[1][1].text='Shared data changed';
      await check('shared table',()=>act({action:'set-table',slide:0,objectId:'table-0',sceneRevision:s.version,tableData:payload}),[0]);
      // Normal command copy-on-write intentionally separates the shared table.
      if(deck.pages[2].scene.objects.find(o=>o.table).table.cells.some(c=>c.text.runs?.some(r=>r.text==='Shared data changed')))throw Error('Payload fork changed other consumer');
      await check('undo',()=>act({action:'undo'}),[0]);
      await check('redo',()=>act({action:'redo'}),[0]);
      // Compare every currently assembled workspace page to a cold backend
      // scene, including pages the editor did not redraw during these changes.
      for(let i=0;i<5;i++){
        const r=await fetch('/api/editor/scene?slide='+i+'&timing=1');if(!r.ok)throw Error(await r.text());const cold=await r.json(),warm=structuredClone(deck.pages.find(p=>p.slide===i).scene);
        if((cold.slideIndex||0)!==i)throw Error('Cold request target wrong');
        // HTTP scene requests stamp concurrency/target context; exported
        // workspace pages do not. Compare all actual projection fields.
        delete warm.revision;delete cold.revision;delete warm.slideIndex;delete cold.slideIndex;const a=JSON.stringify(warm),b=JSON.stringify(cold);if(a!==b){let at=0;while(at<a.length&&at<b.length&&a[at]===b[at])at++;throw Error('Unseen workspace scene stale '+i+' '+JSON.stringify({at,warm:a.slice(Math.max(0,at-80),at+200),cold:b.slice(Math.max(0,at-80),at+200)}));}
      }
      if(JSON.stringify(deck.pages).includes('Private workshop notes'))throw Error('Private notes leaked to page payload');
      const source=structuredClone(deck.pages[0].scene),host=document.createElement('div');host.style.cssText='position:fixed;left:-20000px;top:0';document.body.append(host);
      const view=KeynopeScene.mount(host,source);await view.ready;const layer=view.element.querySelector('.keynope-scene-background-media');
      const move=structuredClone(source);move.objects[0].bounds.x+=12;if(!view.update(move))throw Error('Foreground movement with backdrop rejected');await view.ready;
      if(layer!==view.element.querySelector('.keynope-scene-background-media')||layer!==view.element.firstChild||view.resourceCounts().objects!==source.objects.length)throw Error('Backdrop entered selectable reconciliation');
      const collision=structuredClone(move);collision.objects[0].id='keynope-background-media';
      const collisionView=KeynopeScene.mount(host,move);await collisionView.ready;const retainedForeground=collision.objects.slice(1).map(object=>collisionView.element.querySelector('[data-object-id="'+object.id+'"]'));
      if(!collisionView.update(collision))throw Error('Background namespace could not rebase for an authored ID');await collisionView.ready;
      if(!collisionView.element.querySelector('[data-object-id="keynope-background-media"]')||collisionView.resourceCounts().objects!==source.objects.length||collisionView.element.querySelectorAll('.keynope-scene-background-media').length!==1||collisionView.element.firstChild.dataset.objectId)throw Error('Background namespace stole an authored ID or entered object order');
      for(const node of retainedForeground)if(node!==collisionView.element.querySelector('[data-object-id="'+node.dataset.objectId+'"]'))throw Error('Namespace rebasing remounted an unrelated foreground object');collisionView.dispose();
      const empty={...move,objects:[]};if(!view.update(empty))throw Error('Foreground deletion rejected');await view.ready;if(layer!==view.element.firstChild||view.resourceCounts().objects!==0||view.resourceCounts().media!==1)throw Error('Foreground deletion removed backdrop');
      view.dispose();if(view.resourceCounts().media!==0)throw Error('Backdrop disposal leaked media');host.remove();
      window.keynopeWasmWorkspace=original;return {records,metrics:metrics(),pages:deck.pages.length,backgroundReconciliation:true};
    });
    await page.screenshot({path:output+'/workspace-consumers.png'});fs.writeFileSync(output+'/browser-report.json',JSON.stringify(result,null,2)+'\n');assert.deepEqual(errors,[]);
    console.log('PASS: opt-in WASM counters, unused/overridden master consumers, notes, payload COW/history, precise normal-page rebuilds and unseen cold scene parity. '+output);console.log(JSON.stringify(result));
  }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);process.exitCode=1;server.close();});
