const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const output=process.env.KEYNOPE_QR_EVIDENCE||fs.mkdtempSync('/tmp/keynope-qr-creator-');
(async()=>{
 fs.mkdirSync(output,{recursive:true});
 const engine=process.env.QR_BROWSER||'chromium',browser=await pw[engine].launch({headless:true,...(process.env.QR_BROWSER_PATH?{executablePath:process.env.QR_BROWSER_PATH}:engine==='chromium'&&process.env.CHROME_PATH?{executablePath:process.env.CHROME_PATH}:{})});
 try{
  const page=await browser.newPage({viewport:{width:1440,height:1000},reducedMotion:'reduce',colorScheme:'dark'}),errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());
  await page.goto(process.argv[2]);
  try{await page.waitForFunction(()=>typeof KeynopeQRCode!=='undefined'&&document.querySelector('.keynope-editor-topbar'));}
  catch(error){await page.screenshot({path:path.join(output,engine+'-startup-error.png')});console.log('Startup diagnostics',errors,await page.evaluate(()=>({qr:typeof KeynopeQRCode,text:document.body.innerText.slice(0,1200)})));throw error;}
  const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
  await page.evaluate(()=>keynopeSetInterfaceAppearance('dark',false));
  const before=await state(),initialCount=(before.slides[0].elements||[]).length;
  await page.getByRole('button',{name:'Insert',exact:true}).click();await page.getByRole('button',{name:'Add QR Code',exact:true}).click();
  const dialog=page.getByRole('dialog',{name:'QR Code',exact:true}),input=dialog.getByRole('textbox',{name:'URL or text'}),insert=dialog.getByRole('button',{name:'Insert QR Code'});
  await dialog.waitFor();assert(await insert.isDisabled());
  const payload='https://keynope.sh/join/QRTest01';await input.fill(payload);await insert.waitFor({state:'visible'});await page.waitForFunction(()=>!document.querySelector('.keynope-qr-dialog [name=insert]').disabled);
  const image=dialog.getByRole('img',{name:'Preview of your QR code'});await image.waitFor();
  const dark=await dialog.evaluate(d=>getComputedStyle(d).backgroundColor);
  await page.screenshot({path:path.join(output,engine+'-dialog.png')});await image.screenshot({path:path.join(output,engine+'-preview.png')});
  for(const width of [420,1440]){
   await page.setViewportSize({width,height:900});
   const fitting=await dialog.evaluate(d=>({fits:d.scrollWidth<=d.clientWidth,rect:d.getBoundingClientRect().toJSON()}));assert(fitting.fits&&fitting.rect.right<=width+1&&fitting.rect.left>=-1,JSON.stringify(fitting));
   if(width===420)await page.screenshot({path:path.join(output,engine+'-compact-dialog.png')});
  }
  await page.emulateMedia({colorScheme:'light'});await page.evaluate(()=>keynopeSetInterfaceAppearance('light',false));
  assert.notEqual(await dialog.evaluate(d=>getComputedStyle(d).backgroundColor),dark,'Appearance did not change');
  await page.screenshot({path:path.join(output,engine+'-light-dialog.png')});
  await dialog.getByLabel('Code colour',{exact:true}).fill('#eeeeee');await page.waitForFunction(()=>document.querySelector('.keynope-qr-status').textContent.includes('contrast'));assert(await insert.isDisabled());
  assert.equal((await state()).version,before.version,'Preview should not mutate the deck');
  await dialog.getByLabel('Code colour',{exact:true}).fill('#102030');await dialog.getByLabel('Error correction',{exact:true}).selectOption('H');await page.waitForFunction(()=>!document.querySelector('.keynope-qr-dialog [name=insert]').disabled);
  await insert.click();await dialog.waitFor({state:'detached'});
  const after=await state(),element=after.slides[0].elements.at(-1);assert.equal(after.slides[0].elements.length,initialCount+1);assert.equal(element.kind,'image');assert.equal(new URLSearchParams(element.query).get('link'),payload);
  await page.waitForFunction(id=>modernPageView&&deck.pages[pageIndex]?.scene?.objects.some(o=>o.id===id),element.id);
  await page.evaluate(async()=>{await modernPageView.ready;await new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)));});
  const node=page.locator('#stage .keynope-modern-scene [data-object-id="'+element.id+'"]');await node.waitFor();const b=await node.boundingBox();assert(Math.abs(b.width-b.height)<1,JSON.stringify(b));await node.screenshot({path:path.join(output,engine+'-inserted.png')});
  await page.getByRole('button',{name:'Undo',exact:true}).click();
  await page.waitForFunction(async count=>{const s=await fetch('/api/editor/state').then(r=>r.json());return (s.slides[0].elements||[]).length===count;},initialCount);
  await page.getByRole('button',{name:'Redo',exact:true}).click();
  await page.waitForFunction(async count=>{const s=await fetch('/api/editor/state').then(r=>r.json());return (s.slides[0].elements||[]).length===count;},after.slides[0].elements.length);
  // Cancelling discards a local draft without another undo entry or mutation.
  const prior=(await state()).version;await page.evaluate(()=>KeynopeQRCode.insert(()=>{throw Error('Must not be called')}));await page.getByRole('dialog',{name:'QR Code',exact:true}).getByRole('textbox',{name:'URL or text',exact:true}).fill('Discard me');await page.keyboard.press('Escape');await page.getByRole('dialog',{name:'QR Code',exact:true}).waitFor({state:'detached'});assert.equal((await state()).version,prior);
  assert.deepEqual(errors,[]);console.log('PASS '+engine+': Insert entry, local preview, responsive/light dialog, contrast validation, square visible QR, single insertion, undo/redo and cancel. '+output);
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
