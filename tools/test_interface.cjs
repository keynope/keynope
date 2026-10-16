// Isolated production editor HTML; no desktop activation or external services.
const assert=require('node:assert/strict'),fs=require('node:fs');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright'),engine=process.env.KEYNOPE_BROWSER_ENGINE||'webkit';
(async()=>{
 const browser=await pw[engine].launch({headless:true});
 try{
  const page=await browser.newPage({viewport:{width:1440,height:1000}}),errors=[],output=fs.mkdtempSync('/tmp/keynope-interface-');
  page.on('pageerror',e=>errors.push(e.message));await page.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
  await page.goto(process.argv[2]);await page.waitForFunction(()=>typeof window.keynopeWorkspaceCommand==='function');
  const action=data=>page.evaluate(data=>editorAction(data),data);
  const settle=()=>page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));
  await action({action:'select-element',elementData:{id:'box'}});await page.getByRole('tab',{name:'Arrange',exact:true}).click();
  const contrast=async(selector)=>page.locator(selector).evaluate(node=>{
   const style=getComputedStyle(node),rgb=value=>value.match(/[\d.]+/g).slice(0,3).map(Number),lum=value=>rgb(value).map(v=>{v/=255;return v<=.04045?v/12.92:((v+.055)/1.055)**2.4;}).reduce((sum,v,i)=>sum+v*[.2126,.7152,.0722][i],0);
   const a=lum(style.color),b=lum(style.backgroundColor);return (Math.max(a,b)+.05)/(Math.min(a,b)+.05);
  });
  let scenePaint;
  await page.getByRole('button',{name:'Speaker notes',exact:true}).click();
  const notes=page.getByRole('textbox',{name:'Speaker notes',exact:true});await notes.waitFor({state:'visible'});
  const notesVersion=await page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()).then(s=>s.version));
  for(const [mode,system,resolved]of [['light','dark','light'],['dark','light','dark'],['system','light','light'],['system','dark','dark']]){
   await page.evaluate(mode=>keynopeSetInterfaceAppearance(mode),mode);await page.emulateMedia({colorScheme:system});await settle();
   const appearance=await notes.evaluate(node=>{const field=getComputedStyle(node),panel=node.closest('.keynope-speaker-notes'),style=getComputedStyle(panel),label=getComputedStyle(panel.querySelector('label')),placeholder=getComputedStyle(node,'::placeholder'),retry=getComputedStyle(panel.querySelector('button'));return {panel:style.backgroundColor,background:field.backgroundColor,color:field.color,label:label.color,placeholder:placeholder.color,retry:retry.backgroundColor};});
   assert.equal(appearance.panel,resolved==='light'?'rgb(250, 250, 250)':'rgb(32, 33, 36)','Notes panel ignores '+mode+'/'+system);
   assert.equal(appearance.background,resolved==='light'?'rgb(255, 255, 255)':'rgb(24, 25, 27)','Notes field ignores '+mode+'/'+system);
   assert.equal(appearance.label,appearance.placeholder);assert(await contrast('.keynope-speaker-notes textarea')>=4.5,'Notes contrast '+mode+'/'+system);
   assert(await contrast('.keynope-speaker-notes button:first-of-type')>=4.5,'Notes retry/discard contrast '+mode+'/'+system);
   await notes.focus();assert.equal(await notes.evaluate(n=>getComputedStyle(n).outlineStyle),'solid');
   await page.screenshot({path:output+'/'+engine+'-notes-'+mode+'-'+system+'.png'});
  }
  assert.equal(await page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()).then(s=>s.version)),notesVersion,'Appearance change edited speaker notes');
  await page.getByRole('button',{name:'Speaker notes',exact:true}).click();
  for(const appearance of ['light','dark']){
   await page.emulateMedia({colorScheme:appearance});await settle();
   assert(await contrast('.keynope-workspace-inspector')>=4.5,'Inspector contrast '+appearance);
   assert(await contrast('[aria-label="Object x"]')>=4.5,'Numeric field contrast '+appearance);
   const paint=await page.locator('#stage .keynope-modern-scene').evaluate(node=>({background:getComputedStyle(node).backgroundColor,markup:node.innerHTML}));
   if(scenePaint)assert.deepEqual(paint,scenePaint,'System appearance altered authored slide paint');else scenePaint=paint;
   await page.screenshot({path:output+'/'+engine+'-'+appearance+'-arrange.png'});
  }
  const x=page.getByRole('spinbutton',{name:'Object x',exact:true});await x.focus();await x.press('Tab');assert.equal(await page.getByRole('spinbutton',{name:'Object y',exact:true}).evaluate(n=>n===document.activeElement),true);
  await page.locator('#stage').focus();await page.keyboard.press('F6');assert(await page.locator('.keynope-workspace-inspector').evaluate(n=>n.contains(document.activeElement)));await page.keyboard.press('Shift+F6');assert.equal(await page.locator('#stage').evaluate(n=>n===document.activeElement),true);
  const before=await page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
  // The native View menu and toolbar restore button share a real visibility
  // toggle, not a focus-only command. Hiding gives the canvas its space back.
  const navigator=page.getByRole('complementary',{name:'Slide navigator',exact:true});
  const rect=await page.locator('#stage').boundingBox();assert(await navigator.isVisible());assert.equal(await page.evaluate(()=>keynopeWorkspaceState().navigator),true);
  await page.evaluate(()=>keynopeWorkspaceCommand('navigator'));assert.equal(await navigator.isVisible(),false);assert.equal(await page.evaluate(()=>keynopeWorkspaceState().navigator),false);
  assert(await page.getByRole('separator',{name:'Resize slide navigator',exact:true}).isHidden());
  const expanded=await page.locator('#stage').boundingBox();assert.equal(expanded.x,0);assert(expanded.width>rect.width+150,'Hidden navigator did not reclaim canvas width');
  await page.screenshot({path:output+'/'+engine+'-navigator-hidden.png'});
  await page.reload();await page.waitForFunction(()=>typeof window.keynopeWorkspaceCommand==='function');assert.equal(await navigator.isVisible(),false,'Navigator preference was lost on reload');
  await page.getByRole('button',{name:'Slide navigator',exact:true}).click();assert(await navigator.isVisible());assert.equal(await page.evaluate(()=>keynopeWorkspaceState().navigator),true);assert.equal((await page.locator('#stage').boundingBox()).x,rect.x);
  await page.getByRole('tab',{name:'Arrange',exact:true}).click();
  // Menu activation (no keyboard event) must still produce visible pane focus.
  await page.locator('#stage').focus();
  for(const selector of ['.keynope-workspace-inspector','.keynope-app-toolbar','.keynope-ribbon-header','#keynope-slide-navigator','#stage']){
   assert.equal(await page.evaluate(()=>keynopeWorkspaceCommand('focus-next')),true);
   assert(await page.locator(selector).evaluate(n=>n.contains(document.activeElement)&&n.classList.contains('keynope-pane-focus')&&parseFloat(getComputedStyle(n).outlineWidth)>=2),'Move Focus did not visibly focus '+selector);
  }
  await page.evaluate(()=>keynopeWorkspaceCommand('zoom-in'));assert(Math.abs(await page.evaluate(()=>KEYNOPE_EDITOR_VIEWPORT.zoom)-1.1)<.001);await page.evaluate(()=>keynopeWorkspaceCommand('fit'));assert.equal(await page.evaluate(()=>KEYNOPE_EDITOR_VIEWPORT.zoom),1);
  assert.equal((await page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()))).version,before.version,'View commands modified the deck');
  await page.getByRole('tab',{name:'Style',exact:true}).click();await page.locator('[data-selection-property="paint.color"]').click();
  const picker=page.getByRole('dialog',{name:'Pick color',exact:true}),hex=picker.getByRole('textbox',{name:'HTML color',exact:true});
  await hex.fill('zzzzzz');await picker.getByRole('button',{name:'Apply',exact:true}).click();assert.equal(await hex.getAttribute('aria-invalid'),'true');assert(await picker.getByRole('alert').isVisible());
  assert.equal(await page.evaluate(()=>keynopeWorkspaceCommand('zoom-in')),false,'View command escaped a modal task');
  const palette=picker.getByRole('group',{name:'Color palette'});assert.equal(await palette.locator('button[tabindex="0"]').count(),1);await palette.locator('button[tabindex="0"]').focus();await page.keyboard.press('ArrowRight');assert(await palette.locator('button[tabindex="0"]').evaluate(n=>n===document.activeElement));
  await page.screenshot({path:output+'/'+engine+'-colour.png'});await page.keyboard.press('Escape');assert.equal(await picker.count(),0);assert(await page.locator('[data-selection-property="paint.color"]').evaluate(n=>n===document.activeElement));
  // Exact-value alternative to dragging image sliders, using real insertion.
  await page.getByRole('button',{name:'Insert',exact:true}).click();const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Import image',exact:true}).click();
  const png=await page.evaluate(()=>{const c=document.createElement('canvas');c.width=120;c.height=80;const p=c.getContext('2d');p.fillStyle='#ff0055';p.fillRect(0,0,120,80);return c.toDataURL().split(',')[1];});await(await chooser).setFiles({name:'Owned interface.png',mimeType:'image/png',buffer:Buffer.from(png,'base64')});
  await page.waitForFunction(()=>deck.pages[pageIndex].scene?.objects.some(o=>o.kind==='image'));const image=(await page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()))).slides[0].elements.find(e=>e.kind==='image');
  await action({action:'select-element',elementData:{id:image.id}});await page.getByRole('tab',{name:'Image',exact:true}).click();
  const brightness=page.getByRole('spinbutton',{name:'Brightness value',exact:true});await brightness.fill('1.2');await brightness.press('Tab');
  await page.waitForFunction(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities.properties['image.brightness']?.value===1.2;});assert.equal(await page.getByRole('slider',{name:'Brightness',exact:true}).inputValue(),'1.2');
  await settle();await page.screenshot({path:output+'/'+engine+'-image.png'});await action({action:'undo'});
  await action({action:'select-element',elementData:{id:'heading'}});await page.getByRole('tab',{name:'Text',exact:true}).click();
  await page.screenshot({path:output+'/'+engine+'-text.png'});
  const stack=await page.locator('.keynope-text-metric-control').first().evaluate(node=>{const input=node.querySelector('input').getBoundingClientRect(),buttons=[...node.querySelectorAll('button')].map(n=>{const s=getComputedStyle(n);return {height:n.getBoundingClientRect().height,min:s.minHeight,padding:s.padding,line:s.lineHeight,box:s.boxSizing};});return {height:input.height,buttons};});assert(stack.buttons.every(b=>b.height<=stack.height/2+1),'Font steppers exceed their shared field height: '+JSON.stringify(stack));
  await page.emulateMedia({reducedMotion:'reduce',contrast:'more'});await page.getByRole('button',{name:'Insert',exact:true}).focus();assert.equal(await page.getByRole('button',{name:'Insert',exact:true}).evaluate(n=>getComputedStyle(n).transitionDuration),'0s');await page.screenshot({path:output+'/'+engine+'-contrast.png'});
  for(const width of [1100,900,390]){
   await page.setViewportSize({width,height:width<700?844:768});await settle();
   await page.evaluate(()=>keynopeWorkspaceCommand('format'));await page.evaluate(()=>keynopeWorkspaceCommand('format'));await settle();
   const overflow=await page.locator('.keynope-workspace-inspector').evaluate(n=>({width:n.clientWidth,scroll:n.scrollWidth}));assert(overflow.scroll<=overflow.width+1,'Inspector horizontal overflow at '+width);
   await page.screenshot({path:output+'/'+engine+'-'+width+'.png'});
  }
  await page.evaluate(()=>keynopeWorkspaceCommand('navigator'));assert(await navigator.isVisible(),'Narrow navigator did not open');assert.equal((await page.locator('#stage').boundingBox()).x,0);
  await page.evaluate(()=>keynopeWorkspaceCommand('navigator'));assert.equal(await navigator.isVisible(),false,'Narrow navigator did not toggle closed');
  await page.setViewportSize({width:1440,height:1000});await settle();assert(await navigator.isVisible(),'Drawer behaviour overwrote the desktop visibility preference');
  assert.deepEqual(errors,[]);console.log('PASS '+engine+': light/dark contrast and paint isolation, pane focus, view commands, colour validation/keyboard/focus recovery, steppers, accessibility preferences and responsive widths. '+output);
 }finally{await browser.close();}
})().catch(error=>{console.error(error);process.exitCode=1;});
