const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright'),output=process.env.KEYNOPE_VIDEO_EVIDENCE||'/tmp/keynope-video-proof';
fs.mkdirSync(output,{recursive:true});
(async()=>{
 for(const engine of ['chromium','webkit']){
  const browser=await pw[engine].launch({headless:true});
  try{
   const context=await browser.newContext({viewport:{width:1440,height:1000}}),editor=await context.newPage(),screen=await context.newPage(),errors=[];
   for(const page of [editor,screen]){page.on('pageerror',error=>errors.push(error.message));await page.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());}
   const base=new URL(process.argv[2]);base.search='';
   for(const mode of ['none','external'])await context.request.post(new URL('/presenter-status',base).href,{data:{mode,paused:false}});
   await Promise.all([editor.goto(base+'?keynopeSurface=app'),screen.goto(base+'?keynopeSurface=external')]);
   await editor.waitForFunction(()=>typeof keynopeSetPresentationState==='function'&&presenterPresenting);
   await editor.evaluate(()=>keynopeSetPresentationState('external',false));
   const monitor=editor.locator('#stage video.keynope-video'),display=screen.locator('#stage video.keynope-video');
   await Promise.all([editor.getByRole('button',{name:'Play presentation video',exact:true}).waitFor(),screen.waitForFunction(()=>document.querySelector('#stage video')?.controls)]);
   assert.equal(await monitor.evaluate(v=>{const r=v.getBoundingClientRect();return v.contains(document.elementFromPoint(r.x+r.width/2,r.y+r.height/2));}),true,'Canvas selection handles must not intercept video controls while presenting');
   assert.equal(await editor.locator('#keynope-slide-navigator').isVisible(),true);
   const revision=await editor.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()).then(s=>s.version));
   await editor.getByRole('button',{name:'Play presentation video',exact:true}).click();
   await screen.waitForFunction(()=>{const v=document.querySelector('#stage video');return v&&!v.paused&&v.currentTime>.2});
   assert.equal(await monitor.evaluate(v=>v.muted&&v.volume===0),true,'Editor monitor must be silent');
   assert.equal(await display.evaluate(v=>v.volume===1),true,'Presentation display must retain audio output');
   await editor.getByRole('button',{name:'Pause presentation video',exact:true}).click();
   await screen.waitForFunction(()=>document.querySelector('#stage video').paused);
   await monitor.evaluate(v=>{v.currentTime=2;});
   await screen.waitForFunction(()=>Math.abs(document.querySelector('#stage video').currentTime-2)<.15);
   await editor.waitForFunction(()=>document.querySelector('.keynope-video-controls output').textContent.startsWith('0:02'));
   await editor.getByRole('button',{name:'Sound off',exact:true}).click();
   await screen.waitForFunction(()=>!document.querySelector('#stage video').muted);
   assert.equal(await monitor.evaluate(v=>v.muted&&v.volume===0),true,'Enabling audience audio must not unmute the monitor');
   await editor.getByRole('button',{name:'Sound on',exact:true}).click();
   await screen.waitForFunction(()=>document.querySelector('#stage video').muted);
   await monitor.evaluate(v=>{v.playbackRate=1.5;return v.play();});
   await screen.waitForFunction(()=>{const v=document.querySelector('#stage video');return !v.paused&&v.playbackRate===1.5&&v.currentTime>=2;});
   await editor.screenshot({path:path.join(output,engine+'-presenter-controls.png')});
   await display.evaluate(v=>v.pause());
   await editor.waitForFunction(()=>document.querySelector('#stage video').paused);
   assert.equal(await editor.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()).then(s=>s.version)),revision,'Playback must not dirty the deck or create undo entries');
   await editor.locator('#stage').focus();await editor.keyboard.press('Delete');
   assert.equal(await editor.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()).then(s=>s.version)),revision,'Presentation keyboard input must not delete the selected video');
   await editor.evaluate(()=>keynopeSetPresentationState('none',false));
   assert.equal(await editor.locator('.keynope-canvas-overlay').isVisible(),true,'Stop must restore editing handles');
   assert.deepEqual(errors,[]);
   console.log('PASS '+engine+': two real views, primary controls reachable, play/pause/seek/rate synchronized, silent monitor, no deck changes');
  }finally{await browser.close();}
 }
})().catch(error=>{console.error(error);process.exitCode=1;});
