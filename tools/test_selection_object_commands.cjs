// Production shared inspector: typed editors, mixed subsets and group/lock
// availability. Owned generated fixture; no customer data or external requests.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http'),cp=require('node:child_process');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright'),root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site');
const {waitForAsync}=require('./wait_for_async.cjs');
const output=fs.mkdtempSync('/tmp/keynope-object-commands-'),fixture=path.join(output,'commands.md');
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 cp.execFileSync('go',['test','-run','^TestPowerupDenseEditingFixture$','./internal/app'],{env:{...process.env,GOCACHE:process.env.GOCACHE||'/tmp/keynope-go-cache',KEYNOPE_DENSE_EDITING_FIXTURE:fixture},stdio:'inherit'});
 console.log('Object-command artifacts: '+output);
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 for(const engine of ['chromium','webkit']){
  const browser=await pw[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
  try{
   const context=await browser.newContext({viewport:{width:1440,height:1000},reducedMotion:'reduce'});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());
   const page=await context.newPage(),errors=[];page.on('pageerror',error=>errors.push(error.message));page.on('dialog',dialog=>dialog.accept());
   await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
   const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles(fixture);
   await page.waitForFunction(()=>deck.pages[pageIndex]?.scene?.objects.some(object=>object.id==='dense-table'));
   const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
   const action=data=>page.evaluate(data=>editorAction(data),data);
   const select=async ids=>{const s=await state(),elements=s.slides[s.current].elements,indices=ids.map(id=>elements.findIndex(element=>element.id===id));assert(indices.every(i=>i>=0));await action({action:'select-elements',element:indices[0],elementIndices:indices,elementsData:indices.map(i=>elements[i])});};
   const content=()=>page.locator('[data-ribbon-tab="content"]').click();
   const command=name=>page.locator('[data-selection-command="'+name+'"]');
   const lock=async(id,value)=>{await select([id]);await action({action:'set-object-lock',value:value?1:0});};
   for(const kind of ['table','chart']){
    await select(['dense-'+kind]);await content();assert(!(await command(kind+'.edit').isDisabled()));
    await command(kind+'.edit').click();const dialog=page.getByRole('dialog',{name:'Edit '+kind,exact:true});await dialog.waitFor();await dialog.getByRole('button',{name:'Cancel',exact:true}).click();
    await lock('dense-'+kind,true);await content();assert(await command(kind+'.edit').isDisabled());assert(await command(kind+'.edit').getAttribute('title'));
    if(kind==='table')assert(await command('table.fit').isDisabled());
    await page.screenshot({path:path.join(output,engine+'-'+kind+'-locked.png')});
    const before=(await state()).version;await page.evaluate(kind=>kind==='table'?editSelectedTable():editSelectedChart(),kind);
    assert.equal(await page.getByRole('dialog',{name:'Edit '+kind,exact:true}).count(),0);assert.equal((await state()).version,before,'Disabled editor launcher changed history');
    await lock('dense-'+kind,false);
   }
   await select(['alpha','dense-text-0']);await content();let s=await state();const subset=s.selectionCapabilities.commands['image.adjust'];assert(subset.enabled);assert.deepEqual(subset.eligibleIds,['alpha']);assert.equal(subset.totalCount,2);
   const panel=page.getByRole('group',{name:'Image adjustments',exact:true});await panel.waitFor();assert.match(await panel.textContent(),/1 of 2 objects/);
   assert.equal(await page.getByRole('button',{name:'Adjust image',exact:true}).count(),0,'Image controls must not hide behind a popup');
   assert(await panel.evaluate(panel=>{const p=panel.getBoundingClientRect(),scope=panel.querySelector('.keynope-command-scope').getBoundingClientRect(),label=panel.querySelector('label').getBoundingClientRect(),host=panel.closest('.keynope-workspace-inspector').getBoundingClientRect();return scope.width>=p.width-3&&scope.bottom<=label.top+1&&p.left>=host.left&&p.right<=host.right;}),'Image controls overflowed the sidebar grid');
   const textBefore=s.slides[0].elements.find(element=>element.id==='dense-text-0');
   await panel.getByLabel('Brightness',{exact:true}).evaluate(input=>{input.value='1.3';input.dispatchEvent(new Event('change',{bubbles:true}));});
   await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return new URLSearchParams(s.slides[0].elements.find(element=>element.id==='alpha').query).get('brightness')==='1.3';});
   assert.deepEqual((await state()).slides[0].elements.find(element=>element.id==='dense-text-0'),textBefore);
   await page.screenshot({path:path.join(output,engine+'-image-subset.png')});
   await lock('dense-text-0',true);await select(['alpha','dense-text-0']);await content();assert(await panel.getByLabel('Brightness',{exact:true}).isDisabled());await lock('dense-text-0',false);
   // Right-click selects/reveals the inspector, never a second set of actions.
   await select(['dense-text-0']);s=await state();const imageIndex=s.slides[0].elements.findIndex(element=>element.id==='alpha');
   await page.locator('.keynope-canvas-element[data-element="'+imageIndex+'"]').dispatchEvent('contextmenu',{clientX:650,clientY:350});
   await waitForAsync(page,async index=>(await fetch('/api/editor/state').then(r=>r.json())).selected===index,imageIndex);
   await panel.waitFor();assert.equal(await page.locator('.keynope-slide-context.open').count(),0);
   await panel.getByLabel('Image style',{exact:true}).selectOption('retro');await panel.getByLabel('Glyph',{exact:true}).waitFor();
   await panel.getByLabel('Glyph',{exact:true}).selectOption('braille');
   await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return new URLSearchParams(s.slides[0].elements.find(element=>element.id==='alpha').query).get('glyph')==='braille';});
   await panel.getByLabel('Image style',{exact:true}).selectOption('modern');await panel.getByLabel('Glyph',{exact:true}).waitFor({state:'detached'});
   assert(await command('image.background').isVisible(),'Background promotion belongs in the Image sidebar');
   await page.screenshot({path:path.join(output,engine+'-image-sidebar.png')});
   await page.locator('[data-ribbon-tab="style"]').click();const transfer=page.getByRole('group',{name:'Style transfer',exact:true});
   for(const name of ['Copy style','Paste style','Format painter','Reset style'])assert(await transfer.getByRole('button',{name,exact:true}).isVisible(),name+' missing from sidebar');
   await transfer.getByRole('button',{name:'Copy style',exact:true}).click();assert(!(await transfer.getByRole('button',{name:'Paste style',exact:true}).isDisabled()));
   await select(['from']);assert(!(await state()).selectionCapabilities.commands['shape.adjust'].enabled,'Collapsed diagram group exposed one child adjustment');
   await action({action:'enter-group',elementData:{id:'from'}});await select(['from']);await content();assert((await state()).selectionCapabilities.commands['shape.adjust'].enabled);await page.locator('.keynope-shape-adjustment').first().waitFor();
   await lock('from',true);await page.locator('.keynope-shape-adjustment').first().waitFor({state:'detached'});assert(!(await state()).selectionCapabilities.commands['shape.adjust'].enabled);await lock('from',false);await action({action:'exit-group'});
   await select(['dense-table','dense-chart']);await action({action:'group-elements',elementData:{id:'dense-table'}});s=await state();assert(!s.selectionCapabilities.commands['table.edit'].enabled&&!s.selectionCapabilities.commands['chart.edit'].enabled);assert(s.selectionCapabilities.commands['build.add'].enabled);
   // Whole selected groups prefill every member of the timeline, not only the
   // primary selected object. A locked group pre-fills no unsafe partial build.
   const builds=async()=>{await page.locator('[data-ribbon-tab="slide"]').click();await page.getByRole('button',{name:'Edit slide builds',exact:true}).click();const dialog=page.getByRole('dialog',{name:'Slide builds',exact:true});await dialog.waitFor();await dialog.getByRole('button',{name:'Add build for selected objects',exact:true}).click();return dialog;};
   let dialog=await builds();assert.equal(await dialog.locator('input[type="checkbox"]:checked').count(),2);await dialog.getByRole('button',{name:'Cancel',exact:true}).click();
   await action({action:'set-object-lock',value:1});s=await state();assert(!s.selectionCapabilities.commands['build.add'].enabled);dialog=await builds();assert.equal(await dialog.locator('input[type="checkbox"]:checked').count(),0);await dialog.getByRole('button',{name:'Cancel',exact:true}).click();await action({action:'set-object-lock',value:0});
   await action({action:'enter-group',elementData:{id:'dense-table'}});await select(['dense-table']);await content();assert(!(await command('table.edit').isDisabled()));await command('table.edit').click();dialog=page.getByRole('dialog',{name:'Edit table',exact:true});await dialog.waitFor();await dialog.getByRole('button',{name:'Cancel',exact:true}).click();
   await page.screenshot({path:path.join(output,engine+'.png')});assert.deepEqual(errors,[]);
   console.log('PASS '+engine+': typed editor/fit availability, locked launchers, mixed image subset, shape handles and group-safe build preselection. '+output);
  }finally{await browser.close();}
 }
})().catch(error=>{console.error(error);process.exitCode=1;}).finally(()=>server.close());
