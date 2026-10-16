// Production editor, isolated deck and fake participant transport. Never joins
// a live room, launches the desktop app or touches the presenter's document.
const assert=require('node:assert/strict'),fs=require('node:fs');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright'),engine=process.env.KEYNOPE_BROWSER_ENGINE||'webkit';
(async()=>{
 const browser=await pw[engine].launch({headless:true}),output=fs.mkdtempSync('/tmp/keynope-activity-interface-');
 try{
  const page=await browser.newPage({viewport:{width:1440,height:1000}}),errors=[];
  page.on('pageerror',e=>errors.push(e.message));await page.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
  await page.goto(process.argv[2]);await page.waitForFunction(()=>typeof window.keynopeOpenActivityActions==='function');
  const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
  const action=data=>page.evaluate(data=>editorAction(data),data);
  const chooser=()=>page.getByRole('dialog',{name:'Activity',exact:true});
  const openChooser=()=>page.locator('.keynope-app-toolbar').getByRole('button',{name:'Activities',exact:true}).click();
  // Appearance overrides the operating system, persists, and never edits a deck.
  const version=(await state()).version;
  await page.emulateMedia({colorScheme:'dark'});await page.evaluate(()=>keynopeSetInterfaceAppearance('light'));
  assert.equal(await page.locator('.keynope-workspace-inspector').evaluate(n=>getComputedStyle(n).backgroundColor),'rgb(250, 250, 250)');
  await page.emulateMedia({colorScheme:'light'});await page.evaluate(()=>keynopeSetInterfaceAppearance('dark'));
  assert.equal(await page.locator('.keynope-workspace-inspector').evaluate(n=>getComputedStyle(n).backgroundColor),'rgb(32, 33, 36)');
  assert.equal((await state()).version,version);await page.reload();await page.waitForFunction(()=>typeof editorAction==='function');
  assert.equal(await page.locator('html').getAttribute('data-keynope-interface-appearance'),'dark');
  await page.evaluate(()=>keynopeSetInterfaceAppearance('system'));assert.equal(await page.evaluate(()=>localStorage.getItem('keynope.interface.appearance')),'system');
  await page.emulateMedia({colorScheme:'dark'});
  // Insert gallery and named commands stay inside the popover, not alongside it.
  await page.getByRole('button',{name:'Insert',exact:true}).click();
  const insert=page.getByRole('region',{name:'Insert content'});await insert.waitFor({state:'visible'});
  const layout=await insert.evaluate(n=>{const p=n.getBoundingClientRect();return [...n.querySelectorAll('button,input')].filter(c=>c.getClientRects().length).map(c=>{const b=c.getBoundingClientRect();return {label:c.getAttribute('aria-label')||c.title,x:b.x-p.x,right:b.right-p.right,top:b.top-p.top};});});
  assert(layout.every(b=>b.x>=0&&b.right<=1),'Insert control escapes panel: '+JSON.stringify(layout));
  assert.equal(await insert.locator('.keynope-insert-command .keynope-insert-label').count(),11);
  await page.screenshot({path:output+'/'+engine+'-insert.png'});await page.keyboard.press('Escape');
  // Navigator X shares the native View menu's real hide/show behaviour.
  await page.getByRole('button',{name:'Hide slide navigator',exact:true}).click();
  assert.equal(await page.evaluate(()=>keynopeWorkspaceState().navigator),false);await page.evaluate(()=>keynopeWorkspaceCommand('navigator'));
  await openChooser();await chooser().waitFor({state:'visible'});
  assert(await chooser().getByRole('button',{name:'Add Activity',exact:true}).isVisible());
  const search=chooser().getByRole('searchbox',{name:'Search activities'});
  await search.fill('evidence');assert.equal(await chooser().locator('.keynope-engagement-kind button').count(),1);assert(await chooser().getByRole('button',{name:'Data Hunt',exact:true}).isVisible());
  await search.fill('troduct');assert(await chooser().getByRole('button',{name:'Introduction',exact:true}).isVisible());
  await chooser().getByRole('button',{name:'Introduction',exact:true}).click();
  const names=chooser().getByRole('checkbox',{name:'Names shown with introductions',exact:true});assert(await names.isEnabled());assert(await names.isChecked());
  await names.uncheck();await search.fill('');
  await page.screenshot({path:output+'/'+engine+'-activities-dark.png'});
  await page.evaluate(()=>keynopeSetInterfaceAppearance('light'));
  await page.waitForFunction(()=>getComputedStyle(document.querySelector('.keynope-engagement-kind button:not(.active)')).backgroundColor==='rgb(255, 255, 255)');
  await page.screenshot({path:output+'/'+engine+'-activities-light.png'});
  await chooser().getByRole('button',{name:'Add Activity',exact:true}).click();await chooser().waitFor({state:'detached'});
  assert.equal(!!(await state()).slides[0].engagement.named,false);
  // Badge offers management actions, not an implicit start or runtime open.
  await page.locator('#activity-marker').click();const badge=page.getByRole('dialog',{name:'Activity actions',exact:true});
  assert.deepEqual(await badge.getByRole('button').allTextContents(),['Reset','Remove','Edit','Cancel','Open']);
  assert.equal(await page.locator('.keynope-engagement-overlay').count(),0);
  await badge.getByRole('button',{name:'Edit',exact:true}).click();await chooser().waitFor({state:'visible'});
  assert.equal(await chooser().getByRole('button',{name:'Update Activity',exact:true}).isVisible(),false);
  await chooser().getByRole('checkbox',{name:'Names shown with introductions',exact:true}).check();
  assert(await chooser().getByRole('button',{name:'Update Activity',exact:true}).isVisible());
  await chooser().getByRole('button',{name:'Update Activity',exact:true}).click();await chooser().waitFor({state:'detached'});
  assert.equal((await state()).slides[0].engagement.named,true);
  await openChooser();await chooser().getByRole('button',{name:'Pulse',exact:true}).click();
  assert.equal(await chooser().getByRole('button',{name:'Update Activity',exact:true}).isVisible(),false);
  assert.equal(await chooser().getByRole('button',{name:'Add Activity',exact:true}).count(),0);
  await chooser().getByRole('button',{name:'Cancel',exact:true}).click();assert.equal((await state()).slides[0].engagement.kind,'introduction');
  await page.locator('#activity-marker').click();await badge.getByRole('button',{name:'Open',exact:true}).click();
  const runtime=page.locator('.keynope-engagement-overlay');await runtime.waitFor({state:'visible'});assert(await runtime.getByRole('button',{name:'Start',exact:true}).isVisible());
  assert.equal(await page.evaluate(()=>keynopeEngagementRuntime.phase),0,'Opening an activity started it');
  await runtime.getByRole('button',{name:'Close',exact:true}).click();await runtime.waitFor({state:'detached'});
  await page.locator('#activity-marker').click();await badge.getByRole('button',{name:'Reset',exact:true}).click();
  assert.equal(await runtime.count(),0,'Reset from badge implicitly opened the runtime');
  // Seed a standalone activity code in memory, and provide an owned transport
  // that exercises confirmation/batch removal without affecting real people.
  await page.evaluate(()=>{
   const definition=currentEngagementDefinition(),session=engagementSessionFor(definition);session.code='TEST1234';
   globalThis.removedPeople=[];globalThis.keynopeConnectActivity=async options=>{
    options.onRoster?.([{memberID:'member-a',identity:'owned-a',displayName:'Alice'},{memberID:'member-b',identity:'owned-b',displayName:'Bob'}]);
    return {close:async()=>{},send:async payload=>{if(payload.type!=='participant-remove')throw Error('Unexpected participant command');removedPeople.push(...payload.members.map(member=>member.identity));options.onRoster?.([]);}};
   };
  });
  await openChooser();await chooser().getByRole('button',{name:'Participants',exact:true}).click();
  const people=page.getByRole('dialog',{name:'Training participants',exact:true});await people.getByRole('button',{name:'Remove Alice',exact:true}).waitFor();
  await people.getByRole('button',{name:'Remove everyone',exact:true}).first().click();await people.getByRole('button',{name:'Cancel',exact:true}).click();assert.deepEqual(await page.evaluate(()=>removedPeople),[]);
  await people.getByRole('button',{name:'Remove everyone',exact:true}).first().click();await people.locator('.keynope-participants-confirmation').getByRole('button',{name:'Remove everyone',exact:true}).click();
  assert.deepEqual(await page.evaluate(()=>removedPeople),['owned-a','owned-b']);
  await people.getByRole('button',{name:'Close',exact:true}).click();await chooser().getByRole('button',{name:'Cancel',exact:true}).click();
  // Dock next-state preview in exactly the inspector column, with and without
  // Format open. A fake local presentation cursor avoids opening any window.
  await page.evaluate(()=>{window.KEYNOPE_WEB_EDITOR=true;window.keynopeLivePresentationWindow={closed:false,keynopePresentationPosition:()=>({slide:0,page:0,key:'owned-preview',buildCursor:null})};});
  const preview=page.getByRole('region',{name:'Next presentation state',exact:true});await preview.waitFor({state:'visible'});
  await preview.locator('[data-object-id="next-heading"]').waitFor({state:'visible'});
  for(const format of [true,false]){
   if(await page.evaluate(()=>keynopeWorkspaceState().format)!==format)await page.evaluate(()=>keynopeWorkspaceCommand('format'));
   await page.waitForFunction(()=>document.documentElement.dataset.keynopePreviewOpen==='true');
   const boxes=await page.evaluate(()=>{const p=document.querySelector('.keynope-next-state').getBoundingClientRect(),s=document.querySelector('#stage').getBoundingClientRect(),inspector=document.querySelector('.keynope-workspace-inspector'),i=inspector.getBoundingClientRect();return {px:p.x,pw:p.width,pr:p.right,sr:s.right,iw:parseFloat(getComputedStyle(inspector).width),ir:innerWidth,ib:i.bottom,py:p.y};});
   assert(boxes.sr<=boxes.px+1,'Preview overlaps slide canvas');assert.equal(boxes.pw,boxes.iw);assert.equal(boxes.pr,boxes.ir);if(format)assert(boxes.ib<=boxes.py+1,'Inspector content overlaps preview');
  }
  await page.screenshot({path:output+'/'+engine+'-preview.png'});
  await page.evaluate(()=>{window.keynopeLivePresentationWindow=null;window.KEYNOPE_WEB_EDITOR=false;});
  await preview.waitFor({state:'hidden'});
  // Catalogue is usable on smaller windows too, with no horizontal clipping.
  for(const width of [900,390]){
   await page.setViewportSize({width,height:844});await openChooser();const box=await chooser().boundingBox();assert(box.x>=0&&box.x+box.width<=width+1);
   const overflow=await chooser().evaluate(n=>n.scrollWidth-n.clientWidth);assert(overflow<=1);
   await page.screenshot({path:output+'/'+engine+'-activities-'+width+'.png'});await chooser().getByRole('button',{name:'Cancel',exact:true}).click();
  }
  // The browser's Settings mirrors native appearance choices, independently
  // of activities and without writing anything into presentation metadata.
  const web=await browser.newPage({viewport:{width:1440,height:1000}});web.on('pageerror',e=>errors.push(e.message));
  await web.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());await web.addInitScript(()=>window.KEYNOPE_WEB_EDITOR=true);await web.goto(process.argv[2]);
  await web.waitForFunction(()=>typeof keynopeWorkspaceCommand==='function');await web.getByRole('button',{name:'Slide',exact:true}).click();
  await web.locator('.keynope-settings-menu summary').click();
  const appearance=web.getByRole('combobox',{name:'Interface appearance',exact:true});
  assert.deepEqual(await appearance.locator('option').allTextContents(),['System','Light','Dark']);
  await appearance.selectOption('light');assert.equal(await web.locator('.keynope-settings-panel label').evaluate(n=>getComputedStyle(n).color),'rgb(32, 33, 36)');
  for(const mode of ['light','dark','system']){await appearance.selectOption(mode);assert.equal(await web.locator('html').getAttribute('data-keynope-interface-appearance'),mode);}
  await web.screenshot({path:output+'/'+engine+'-web-settings.png'});await web.close();
  assert.deepEqual(errors,[]);console.log('PASS '+engine+': appearance persistence, Insert layout, navigator close, activity search/actions/save semantics, Introduction names, confirmed participant reset, preview docking. '+output);
 }finally{await browser.close();}
})().catch(error=>{console.error(error);process.exitCode=1;});
