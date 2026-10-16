const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright'),output='/tmp/keynope-build-proof',site='/tmp/keynope-powerup-site';
const server=http.createServer((req,res)=>{const name=new URL(req.url,'http://localhost').pathname,base=name.startsWith('/join/')?site:output,file=path.resolve(base,'.'+name);if(!file.startsWith(base+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',path.extname(file)==='.js'?'text/javascript':path.extname(file)==='.html'?'text/html':'text/css');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));const browser=await playwright.chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
 try{
  const context=await browser.newContext({viewport:{width:1500,height:900}});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
  const page=await context.newPage(),errors=[],checks=[];page.on('pageerror',error=>errors.push(error.message));
  const origin='http://127.0.0.1:'+server.address().port;await page.goto(origin+'/transitions.html');
  await page.locator('[data-object-id="outgoing-title"]').waitFor();await page.waitForFunction(()=>getComputedStyle(document.querySelector('.keynope-modern-presentation')).visibility==='visible');
  for(const effect of ['push','fade','wipe']){
   await page.evaluate(effect=>{deck.pages[1].scene.build.transition.effect=effect;},effect);
   await page.keyboard.press('ArrowRight');await page.waitForFunction(effect=>document.querySelector('.keynope-slide-viewport')?.dataset.transitionState===effect,effect);
   const state=await page.evaluate(()=>{
    const host=document.querySelector('.keynope-slide-viewport');for(const animation of host.getAnimations({subtree:true})){animation.pause();animation.currentTime=750;}
    const incoming=host.querySelector('.keynope-slide-group'),outgoing=[...host.children].find(node=>node!==incoming),backdrop=incoming.querySelector('.keynope-scene-backdrop');
    window.__transitionIncoming=incoming;window.__transitionOutgoing=outgoing;
    return {snapshots:host.children.length,liveIDs:host.querySelectorAll('[data-object-id]').length,incomingColour:Array.from(backdrop.getContext('2d').getImageData(1,1,1,1).data),outgoingColour:Array.from(outgoing.querySelector('canvas').getContext('2d').getImageData(1,1,1,1).data)};
   });
   assert.equal(state.snapshots,2);assert.equal(state.liveIDs,1,'Outgoing snapshot retained interactive identities');assert.deepEqual(state.incomingColour,[36,92,200,255]);assert.deepEqual(state.outgoingColour,[192,64,48,255]);
   await page.waitForTimeout(70);
   const aligned=await page.evaluate(()=>{const group=window.__transitionIncoming,a=group.getBoundingClientRect(),b=group.querySelector('.keynope-scene-backdrop').getBoundingClientRect();return Math.abs(a.left-b.left)<.5&&Math.abs(a.top-b.top)<.5&&Math.abs(a.width-b.width)<.5&&Math.abs(a.height-b.height)<.5;});assert.ok(aligned,'Backdrop and incoming content did not move as one');
   await page.screenshot({path:path.join(output,'transition-'+effect+'.png')});checks.push(effect+' moves/composites full background and scene with a frozen outgoing page');
   await page.waitForFunction(()=>!document.querySelector('.keynope-slide-viewport')?.dataset.transitionState);assert.equal(await page.locator('.keynope-slide-viewport').evaluate(n=>n.children.length),1,'Completed transition retained its snapshot');
   await page.keyboard.press('ArrowLeft');await page.locator('[data-object-id="outgoing-title"]').waitFor();assert.equal(await page.locator('.keynope-slide-viewport').getAttribute('data-transition-state'),null,'Backward final-state entry replayed transition');
  }
  await page.keyboard.press('ArrowRight');await page.waitForFunction(()=>document.querySelector('.keynope-slide-viewport')?.dataset.transitionState==='wipe');await page.keyboard.press('ArrowLeft');
  await page.waitForTimeout(1700);assert.ok(await page.locator('[data-object-id="outgoing-title"]').isVisible());assert.equal(await page.locator('.keynope-slide-viewport').getAttribute('data-transition-state'),null);checks.push('rapid navigation cancels old animations and cleanup generations');
  await page.keyboard.press('ArrowRight');await page.waitForFunction(()=>document.querySelector('.keynope-slide-viewport')?.dataset.transitionState==='wipe');await page.setViewportSize({width:1200,height:800});
  await page.waitForFunction(()=>!document.querySelector('.keynope-slide-viewport')?.dataset.transitionState);assert.equal(await page.locator('.keynope-slide-viewport').evaluate(n=>n.children.length),1);checks.push('resize settles composition without old snapshots');
  await page.keyboard.press('ArrowLeft');await page.keyboard.press('ArrowRight');await page.waitForFunction(()=>!!document.querySelector('.keynope-slide-viewport')?.dataset.transitionState);await page.evaluate(()=>keynopePrepareSnapshot(1));assert.equal(await page.locator('.keynope-slide-viewport').evaluate(n=>n.children.length),1);checks.push('static snapshot removes transitions and shows final authored scene');
  const references=await page.evaluate(()=>{
    const host=document.createElement('div'),incoming=document.createElement('div');host.append(incoming);document.body.append(host);incoming.innerHTML='<svg xmlns="http://www.w3.org/2000/svg"><defs><linearGradient id="owned-gradient"><stop stop-color="red"/></linearGradient></defs><rect data-object-id="private-copy" width="20" height="20" fill="url(#owned-gradient)"/></svg>';
    const c=KeynopeBuilds.slideTransition(host,incoming),copy=c.snapshot(),id=copy.querySelector('linearGradient').id;
    const valid=id!=='owned-gradient'&&copy.querySelector('rect').getAttribute('fill')==='url(#'+id+')'&&!copy.querySelector('[data-object-id]');c.cancel();host.remove();return valid;
  });assert.ok(references);checks.push('frozen SVG snapshots remap local references and remove interactive identities');
  const reduced=await context.newPage();await reduced.emulateMedia({reducedMotion:'reduce'});await reduced.goto(origin+'/transitions.html');await reduced.locator('[data-object-id="outgoing-title"]').waitFor();await reduced.keyboard.press('ArrowRight');await reduced.locator('[data-object-id="incoming-title"]').waitFor();assert.equal(await reduced.locator('.keynope-slide-viewport').getAttribute('data-transition-state'),null);checks.push('reduced motion uses a clean cut');
  await reduced.emulateMedia({reducedMotion:'no-preference'});await reduced.keyboard.press('ArrowLeft');await reduced.keyboard.press('ArrowRight');await reduced.waitForFunction(()=>!!document.querySelector('.keynope-slide-viewport')?.dataset.transitionState);await reduced.emulateMedia({reducedMotion:'reduce'});await reduced.waitForFunction(()=>!document.querySelector('.keynope-slide-viewport')?.dataset.transitionState);checks.push('changing reduced motion settles an in-flight transition');
  // Actual generated participant renderer uses the same composition, cancels
  // on hiding/stopping, and never replays an expired late-join transition.
  const participant=await page.evaluate(async()=>{
   const {createSlideRenderer}=await import('/join/slide-renderer.js'),host=document.createElement('div');host.style.cssText='width:1000px;height:560px';document.body.append(host);const renderer=createSlideRenderer(host),data=JSON.parse(document.querySelector('#keynope-data').textContent);
   data.pages[1].scene.build.transition={effect:'push',direction:'left',durationMs:1500};
   const cursor=(slide,generation,startedAtMs=Date.now())=>({session:'transition-proof',slideId:data.pages[slide].scene.slideId,step:0,generation,startedAtMs,motion:'enter',paused:false});
   const wait=ms=>new Promise(r=>setTimeout(r,ms));renderer.setPage(data,0,{buildCursor:cursor(0,1)});await wait(150);renderer.setPage(data,1,{buildCursor:cursor(1,2)});await wait(150);
   const viewport=host.shadowRoot.querySelector('.keynope-slide-viewport');if(viewport.dataset.transitionState!=='push'||viewport.children.length!==2)throw Error('Participant did not compose transition');
   renderer.setVisible(false);if(viewport.dataset.transitionState||viewport.children.length!==1)throw Error('Hidden participant retained transition');renderer.setVisible(true);await wait(80);if(viewport.dataset.transitionState)throw Error('Hidden participant replayed old transition');
   renderer.setPage(data,0,{buildCursor:cursor(0,3)});await wait(100);renderer.setPage(data,1,{buildCursor:cursor(1,4,Date.now()-10000)});await wait(100);if(viewport.dataset.transitionState||viewport.children.length!==1)throw Error('Late participant replayed expired transition');
   renderer.destroy();host.remove();return true;
  });assert.ok(participant);checks.push('participant composition, hide/resume cancellation and expired late-join cut');
  assert.deepEqual(errors,[]);fs.writeFileSync(path.join(output,'transition-report.json'),JSON.stringify({passed:true,checks},null,2)+'\n');console.log('PASS: full-slide transitions, outgoing backgrounds, cancellation, snapshots and participant parity');
 }finally{await browser.close();await new Promise(resolve=>server.close(resolve));}
})().catch(error=>{console.error(error);process.exitCode=1;server.close();});
