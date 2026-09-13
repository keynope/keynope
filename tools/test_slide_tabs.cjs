// Build first: make web-editor. Tests the shared editor UI in Chromium/WebKit.
const fs=require('node:fs'),path=require('node:path'),http=require('node:http'),assert=require('node:assert/strict');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'terraform/site');
const server=http.createServer((req,res)=>{
 let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';
 const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);res.end();return;}
 res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm','.json':'application/json'})[path.extname(file)]||'application/octet-stream');fs.createReadStream(file).pipe(res);
});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));
 for(const engine of ['chromium','webkit']){
  const browser=await pw[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
  try{
   const page=await browser.newPage({viewport:{width:1600,height:1000}}),errors=[];
   page.on('pageerror',e=>errors.push(e.message));
   await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');
   await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:120000});
   const fixture='<!-- keynope width=245 height=56 -->\n\n'+['First','Reference','Third','Resources','Last'].map(s=>'# '+s).join('\n\n---\n\n');
   await page.evaluate(async markdown=>{
    const result=JSON.parse(window.keynopeWasmInit(markdown,'Tabs.md',false));if(result.status!==200)throw Error(result.body);
    const workspace=JSON.parse(JSON.parse(window.keynopeWasmWorkspace()).body);workspace.current=0;
    await window.keynopeReloadWebDocument(workspace);
   },fixture);
   const b=name=>page.getByRole('button',{name,exact:true});
   const item=i=>page.locator('.keynope-slide-item[data-master-index="'+i+'"]');
   const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
   for(const i of [1,3]){
    await item(i).click();await b('Tab').click();
    await page.waitForFunction(async i=>(await fetch('/api/editor/state').then(r=>r.json())).slides[i].tabId,i);
   }
   assert.equal(await page.locator('[data-slide-section="tabs"]').count(),2);
   assert.equal(await page.locator('[data-slide-section="slides"]').count(),3);
   assert.equal(await b('Tab').getAttribute('aria-pressed'),'true');
   assert(await b('Present on this computer').isDisabled());
   // Native HTML drag events allow deterministic coverage in both engines.
   const drag=async(source,target,bottom)=>{
    await page.evaluate(({source,target,bottom})=>{
     const from=document.querySelector('.keynope-slide-item[data-master-index="'+source+'"]');
     const to=document.querySelector('.keynope-slide-item[data-master-index="'+target+'"]');
     const dataTransfer=new DataTransfer(),r=to.getBoundingClientRect(),clientY=bottom?r.bottom-1:r.top+1;
     from.dispatchEvent(new DragEvent('dragstart',{bubbles:true,dataTransfer}));
     to.dispatchEvent(new DragEvent('dragover',{bubbles:true,dataTransfer,clientY}));
     to.dispatchEvent(new DragEvent('drop',{bubbles:true,dataTransfer,clientY}));
     from.dispatchEvent(new DragEvent('dragend',{bubbles:true,dataTransfer}));
    },{source,target,bottom});
   };
   await drag(3,1,false);
   await page.waitForFunction(async()=>(await fetch('/api/editor/state').then(r=>r.json())).tabs[0].page===4);
   await page.waitForFunction(()=>document.querySelector('[data-slide-section="tabs"]').dataset.masterIndex==='3');
   await drag(3,1,true);
   await page.waitForFunction(async()=>(await fetch('/api/editor/state').then(r=>r.json())).tabs[0].page===2);
   await item(0).click();await b('Next').click();
   await page.waitForFunction(async()=>(await fetch('/api/editor/state').then(r=>r.json())).current===2);
   await b('Previous').click();
   await page.waitForFunction(async()=>(await fetch('/api/editor/state').then(r=>r.json())).current===0);
   // Exported presentation skips the tab slides with arrows, Home and End.
   const html=await page.evaluate(async()=>{
    const response=await fetch('/api/editor/export-document',{method:'POST',body:JSON.stringify({path:'Tabs.html'})});return (await response.json()).content;
   });
   const show=await browser.newPage({viewport:{width:1200,height:800}});
   await show.setContent(html,{waitUntil:'load'});
   await show.waitForFunction(()=>!!window.keynopePresentationPosition);
   assert.equal(await show.evaluate(()=>window.keynopePresentationPosition().slide),0);
   await show.keyboard.press('ArrowRight');assert.equal(await show.evaluate(()=>window.keynopePresentationPosition().slide),2);
   await show.keyboard.press('ArrowRight');assert.equal(await show.evaluate(()=>window.keynopePresentationPosition().slide),4);
   await show.keyboard.press('Home');assert.equal(await show.evaluate(()=>window.keynopePresentationPosition().slide),0);
   await show.keyboard.press('End');assert.equal(await show.evaluate(()=>window.keynopePresentationPosition().slide),4);
   await show.close();
   await item(1).click();await b('Tab').click();
   await page.waitForFunction(async()=>(await fetch('/api/editor/state').then(r=>r.json())).tabs.length===1);
   assert.deepEqual((await state()).slides.map(s=>s.elements[0].text),['First','Reference','Third','Resources','Last']);
   await b('Undo').click();await page.waitForFunction(async()=>(await fetch('/api/editor/state').then(r=>r.json())).tabs.length===2);
   await b('Redo').click();await page.waitForFunction(async()=>(await fetch('/api/editor/state').then(r=>r.json())).tabs.length===1);
   await page.getByRole('button',{name:'Master slides',exact:true}).click();await b('Tab').waitFor({state:'hidden'});
   await page.getByRole('button',{name:'Exit master slides',exact:true}).click();
   await b('Tab').waitFor({state:'visible'});
   fs.mkdirSync('output/slide-tab-tests',{recursive:true});await page.screenshot({path:'output/slide-tab-tests/'+engine+'.png'});
   // Reorder regular slides across a parked tab: the sections stay separate,
   // and its participant destination follows the same underlying slide.
   await drag(4,0,false);
   await page.waitForFunction(async()=>(await fetch('/api/editor/state').then(r=>r.json())).slides[0].elements[0].text==='Last');
   assert.deepEqual((await state()).slides.map(s=>s.elements[0].text),['Last','First','Reference','Third','Resources']);
   assert.equal((await state()).tabs[0].page,5);
   await drag(0,3,true);
   await page.waitForFunction(async()=>(await fetch('/api/editor/state').then(r=>r.json())).slides[3].elements[0].text==='Last');
   assert.equal((await state()).tabs[0].page,5);
   assert.deepEqual(errors,[]);console.log('PASS '+engine+': Tab toggle, sidebar sections, drag both directions, navigation/export skip, restore, undo/redo, master exclusion');
  }finally{await browser.close();}
 }
})().catch(e=>{console.error(e);process.exitCode=1;}).finally(()=>server.close());
