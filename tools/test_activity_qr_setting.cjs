const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
(async()=>{
 const browser=await chromium.launch({headless:true,...(process.env.CHROME_PATH?{executablePath:process.env.CHROME_PATH}:{})});
 try{
  const page=await browser.newPage({viewport:{width:900,height:700}}),source=fs.readFileSync('main.go','utf8');
  await page.setContent('<body style="background:#111c26;color:white"><main id="root"></main></body>');
  await page.addScriptTag({path:path.resolve('web/activity-design.js')});
  const css=source.split('\n').filter(line=>line.startsWith('.keynope-engagement-join-text')).join('\n');
  await page.addStyleTag({content:css});
  await page.evaluate(async data=>{const face=new FontFace('KeynopeC64','url(data:font/ttf;base64,'+data+')');document.fonts.add(face);await face.load();},fs.readFileSync('assets/keynope-c64.ttf.base64','utf8').trim());
  const helperStart=source.indexOf('function activityJoinText(runtime) {'),helperEnd=source.indexOf('function renderEngagementRuntime(',helperStart);
  assert(helperStart>=0&&helperEnd>helperStart);
  await page.addScriptTag({content:source.slice(helperStart,helperEnd)});
  const start=source.indexOf("  const revealed = phase === 'REVEAL' || phase === 'DISCUSS';",helperEnd);
  const end=source.indexOf("\tconst accepting = phase === 'OPEN'",start);
  assert(start>helperEnd&&end>start);
  const snippet=source.slice(start,end);
  for(const readOnly of [false,true])for(const hidden of [false,true])for(const phase of ['OPEN','REVEAL']){
   await page.evaluate(({snippet,readOnly,hidden,phase})=>{
    root.replaceChildren();window.keynopeHideActivityQR=readOnly?!hidden:hidden;
    const runtime={definition:{kind:'onboarding'},phase:phase==='OPEN'?1:3,roomReady:true,readOnly,hideActivityQR:hidden,sessionCode:'AbCd1234',joinUrl:'https://keynope.sh/join/AbCd1234',qrCode:'██ ▀▀'};
    const run=new Function('runtime','phase','definition',`const keynopeHostedEngagementControllerSurface=false;function engagementButton(label){const b=document.createElement('button');b.textContent=label;return b;}function copyEngagementJoinURL(){};${snippet};root.append(content);if(join)root.append(join);`);
    run(runtime,phase,runtime.definition);
   },{snippet,readOnly,hidden,phase});
   const show=phase==='OPEN';
   assert.equal(await page.locator('.keynope-engagement-qr').count(),show&&!hidden?1:0);
   assert.equal(await page.locator('.keynope-engagement-join-text').count(),show&&hidden?1:0);
   if(show&&hidden){assert.equal(await page.locator('.keynope-engagement-join-text').innerText(),'https://keynope.sh/join/\nCode: AbCd1234');assert.equal(await page.locator('.keynope-engagement-join-text').evaluate(el=>getComputedStyle(el).textAlign),'center');await page.screenshot({path:'/tmp/keynope-activity-join-text.png'});}
  }
  console.log('PASS activity QR setting: controller/presentation on/off, centered retro join URL and case-sensitive code; no join area on reveal.');
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
