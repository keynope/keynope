const assert=require('node:assert/strict'),fs=require('node:fs');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright'),engine=process.env.KEYNOPE_BROWSER_ENGINE||'webkit';
(async()=>{
 const browser=await pw[engine].launch({headless:true});
 try{
  const page=await browser.newPage({viewport:{width:1440,height:1000}}),errors=[],commands=[];
  page.on('request',r=>{if(r.url().endsWith('/api/editor/action'))commands.push(JSON.parse(r.postData()||'{}'));});
  page.on('pageerror',e=>errors.push(e.message));await page.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
  await page.goto(process.argv[2]);await page.waitForFunction(()=>typeof editorAction==='function');
  const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
  await page.evaluate(()=>editorAction({action:'select-element',elementData:{id:'box'}}));
  await page.getByRole('tab',{name:'Arrange',exact:true}).click();
  const form=page.getByRole('form',{name:'Object geometry',exact:true}),field=k=>form.getByRole('spinbutton',{name:'Object '+k,exact:true});
  const waitGeometry=async(key,value)=>{
   await page.waitForFunction(async({key,value})=>{const s=await fetch('/api/editor/state').then(r=>r.json());return Math.abs(s.selectionCapabilities.geometry?.[key]-value)<.001;},{key,value});
   try{await page.waitForFunction(({key,value})=>Math.abs(Number(document.querySelector('[aria-label="Object '+key+'"]')?.value)-value)<.001,{key,value});}
   catch(error){console.error(commands);console.error(await page.evaluate(async()=>({events:window.__stepEvents,geometry:(await fetch('/api/editor/state').then(r=>r.json())).selectionCapabilities.geometry,fields:[...document.querySelectorAll('form[aria-label="Object geometry"] input')].map(n=>({label:n.getAttribute('aria-label'),value:n.value})),focus:document.activeElement?.outerHTML})));await page.screenshot({path:'/tmp/keynope-geometry-failure-'+engine+'.png'});throw error;}
  };
  for(const key of ['x','y','width','height']){
   const before=(await state()).selectionCapabilities.geometry[key];await field(key).press('ArrowUp');await waitGeometry(key,before+1);
   assert(await field(key).evaluate(n=>document.activeElement===n),'Arrow stepping lost input focus');
   await field(key).press('ArrowDown');await waitGeometry(key,before);
  }
  // Actual native spinner hit, not dispatchEvent or clicking Apply.
  await page.evaluate(()=>{window.__stepEvents=[];for(const event of ['pointerdown','pointerup','input','change','focusin','focusout'])document.addEventListener(event,e=>{if(e.target?.getAttribute('aria-label')?.startsWith('Object '))__stepEvents.push({event,label:e.target.getAttribute('aria-label'),value:e.target.value,time:performance.now()});},true);});
  const beforeMouse=(await state()).selectionCapabilities.geometry.x,box=await field('x').boundingBox(),padding=await field('x').evaluate(n=>parseFloat(getComputedStyle(n).paddingRight));
  await field('x').hover();await page.mouse.click(box.x+box.width-padding-5,box.y+box.height/2-4);await waitGeometry('x',beforeMouse+1);
  // Multiple key repeats while a command response is deliberately delayed.
  await page.evaluate(()=>{window.__geometryFetch=window.fetch;window.fetch=async(...args)=>{const r=await window.__geometryFetch(...args);if(String(args[0])==='/api/editor/action'&&JSON.parse(args[1]?.body||'{}').action==='set-selection-geometry')await new Promise(resolve=>setTimeout(resolve,100));return r;};});
  const beforeFast=(await state()).selectionCapabilities.geometry.x;await field('x').focus();
  for(let i=0;i<8;i++)await page.keyboard.down('ArrowUp');await page.keyboard.up('ArrowUp');await waitGeometry('x',beforeFast+8);
  await page.evaluate(()=>{window.fetch=window.__geometryFetch;});
  await form.getByRole('checkbox',{name:'Lock object aspect ratio',exact:true}).check();
  const beforeAspect=(await state()).selectionCapabilities.geometry,ratio=beforeAspect.width/beforeAspect.height;
  await field('width').press('ArrowUp');await waitGeometry('width',beforeAspect.width+1);await waitGeometry('height',(beforeAspect.width+1)/ratio);
  assert(await form.getByRole('checkbox',{name:'Lock object aspect ratio',exact:true}).isChecked());
  await form.getByRole('checkbox',{name:'Lock object aspect ratio',exact:true}).uncheck();
  // Typing remains a multi-field draft until Apply, including fractions.
  const beforeDraft=await state();await field('x').fill('30.125');await field('y').fill('12.25');
  assert.equal((await state()).version,beforeDraft.version,'Typing committed a partial draft');
  await form.getByRole('button',{name:'Apply geometry',exact:true}).click();await waitGeometry('x',30.125);await waitGeometry('y',12.25);
  await field('x').press('ArrowUp');await waitGeometry('x',31.125);
  const previous=(await state()).slides[0].elements.find(e=>e.id==='box');
  await page.evaluate(()=>editorAction({action:'set-object-lock',value:1}));assert.equal(await form.count(),1);assert(await field('x').isDisabled(),'Focused stepper ignored a lock change');
  await page.evaluate(async()=>{await editorAction({action:'set-object-lock',value:0});await editorAction({action:'select-element',elementData:{id:'other'}});});await waitGeometry('x',130);
  await field('x').press('ArrowUp');await waitGeometry('x',131);
  assert.deepEqual((await state()).slides[0].elements.find(e=>e.id==='box'),previous,'New selection changed the previous object');
  const output=fs.mkdtempSync('/tmp/keynope-geometry-steppers-');await page.screenshot({path:output+'/'+engine+'.png'});
  assert.deepEqual(errors,[]);console.log('PASS '+engine+': immediate X/Y/width/height arrows, native spinner, rapid repeats, focus, aspect lock and fractional Apply draft. '+output);
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
