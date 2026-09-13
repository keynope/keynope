const fs=require('node:fs'),path=require('node:path'),http=require('node:http'),assert=require('node:assert/strict');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright'),root=path.resolve('terraform/site');
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);res.end();return;}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');fs.createReadStream(file).pipe(res);});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));
 for(const engine of ['chromium','webkit']){
  const browser=await pw[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
  try{
   const page=await browser.newPage({viewport:{width:1600,height:1000}}),errors=[];page.on('pageerror',e=>errors.push(e.message));
   await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:120000});
   const timer=page.locator('button[data-timer-audience]');
   // No activities: binary, both while editing and presenting.
   for(const mode of ['none','main']){
    await page.evaluate(mode=>keynopeSetPresentationState(mode,false),mode);
    await page.getByRole('button',{name:'Timer',exact:true}).click();
    assert.equal(await timer.getAttribute('aria-label'),'Stop timer');
    await timer.click();assert.equal(await timer.getAttribute('data-timer-audience'),'off');
   }
   await page.evaluate(async()=>{
    const r=await fetch('/api/editor/action',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'set-engagement',engagementData:{id:'timer-test',kind:'pulse',prompt:'Ready?'}})});
    if(!r.ok)throw Error(await r.text());
    const workspace=JSON.parse(JSON.parse(keynopeWasmWorkspace()).body);await keynopeReloadWebDocument(workspace);
   });
   // Activities alone do not enable broadcasting outside presentation mode.
   await page.evaluate(()=>keynopeSetPresentationState('none',false));
   await page.getByRole('button',{name:'Timer',exact:true}).click();
   await page.keyboard.type('100');await page.keyboard.press('Enter');
   await page.waitForFunction(async()=>(await fetch('/api/editor/state').then(r=>r.json())).timerMode==='running');
   await timer.click();assert.equal(await timer.getAttribute('data-timer-audience'),'off');
   await page.waitForFunction(async()=>!(await fetch('/api/editor/state').then(r=>r.json())).timerMode);
   await page.evaluate(()=>keynopeSetPresentationState('main',false));
   await page.getByRole('button',{name:'Timer',exact:true}).click();assert.equal(await timer.getAttribute('data-timer-audience'),'local');assert.match(await timer.textContent(),/LOCAL/);
   await page.keyboard.type('130');await page.keyboard.press('Enter');
   await page.waitForFunction(async()=>(await fetch('/api/editor/state').then(r=>r.json())).timerMode==='running');
   const deadline=await page.evaluate(async()=>(await fetch('/api/editor/state').then(r=>r.json())).timerEndMs);
   assert.equal(await page.evaluate(()=>keynopePresentationTimerEnd()),0,'first press shared the timer');
   await timer.click();assert.equal(await timer.getAttribute('data-timer-audience'),'broadcast');assert.match(await timer.textContent(),/BROADCAST/);
   await page.waitForFunction(()=>getComputedStyle(document.querySelector('[data-timer-audience="broadcast"]')).backgroundColor==='rgb(168, 223, 175)');
   assert(await timer.evaluate(el=>{const b=el.getBoundingClientRect(),t=el.querySelector('span').getBoundingClientRect();return t.left>=b.left&&t.right<=b.right;}),'broadcast tag overflows button');
   await timer.screenshot({path:'/tmp/keynope-timer-broadcast-'+engine+'.png'});
   assert.equal(await page.evaluate(()=>keynopePresentationTimerEnd()),deadline,'second press reset the timer');
   await timer.click();await page.waitForFunction(async()=>!(await fetch('/api/editor/state').then(r=>r.json())).timerMode);
   assert.equal(await timer.getAttribute('data-timer-audience'),'off');assert.equal(await page.evaluate(()=>keynopePresentationTimerEnd()),0);
   assert.equal(await page.locator('html').getAttribute('data-keynope-timer-active'),'false');
   // The cycle also works before Enter commits the duration.
   await timer.click();await timer.click();assert.equal(await timer.getAttribute('data-timer-audience'),'broadcast');assert.equal(await page.evaluate(()=>keynopePresentationTimerEnd()),0);
   await page.keyboard.type('200');await page.keyboard.press('Enter');await page.waitForFunction(()=>keynopePresentationTimerEnd()>0);
   await page.keyboard.press('Escape');await page.waitForFunction(()=>document.querySelector('[data-timer-audience="off"]'));
   await timer.click();assert.equal(await timer.getAttribute('data-timer-audience'),'local');await page.keyboard.press('q');
   assert.equal(await timer.getAttribute('data-timer-audience'),'off');
   await timer.click();await timer.click();await page.evaluate(()=>keynopeSetPresentationState('none',false));
   assert.equal(await timer.getAttribute('data-timer-audience'),'local');assert.equal(await page.evaluate(()=>keynopePresentationTimerEnd()),0);
   assert.equal(await timer.getAttribute('aria-label'),'Stop timer');await timer.click();assert.equal(await timer.getAttribute('data-timer-audience'),'off');
   assert.deepEqual(errors,[]);console.log('PASS '+engine+': local → broadcast → off, unchanged deadline, configuration cycle, Escape/q and clean restart');
  }finally{await browser.close();}
 }
})().catch(e=>{console.error(e);process.exitCode=1}).finally(()=>server.close());
