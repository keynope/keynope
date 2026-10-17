// Generate owned source/detached standalone HTML using:
// KEYNOPE_DETACH_EXPORT_EVIDENCE=/tmp/keynope-detach-exports go test ./internal/app -run '^TestThemeDetachExportCorpus$'
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.KEYNOPE_DETACH_EXPORT_EVIDENCE||'/tmp/keynope-detach-exports');
const engine=process.env.TEST_BROWSER||'chromium';
(async()=>{
 const browser=await playwright[engine].launch({headless:true,...(engine==='webkit'?{}:{executablePath:process.env.CHROME_PATH})});
 try{
  const context=await browser.newContext({viewport:{width:1280,height:720},reducedMotion:'reduce'});
  await context.route(/https?:\/\/.*/,r=>r.abort());
  const errors=[],labels=JSON.parse(fs.readFileSync(path.join(root,'pages.json'),'utf8'));
  const output=path.join(root,engine);fs.mkdirSync(output,{recursive:true});
  const pages=[];
  for(const name of ['source','detached']){
   const page=await context.newPage();page.on('pageerror',error=>errors.push(name+': '+error.message));
   await page.goto('file://'+path.join(root,name+'.html'));
   await page.waitForFunction(()=>typeof keynopePrepareSnapshot==='function');
   assert.equal(await page.evaluate(()=>deck.pages.length),labels.length);
   pages.push(page);
  }
  for(let i=0;i<labels.length;i++){
   const images=[];
   for(let j=0;j<pages.length;j++){
    const page=pages[j];await page.evaluate(i=>keynopePrepareSnapshot(i),i);
    images.push(await page.screenshot({path:path.join(output,(j?'detached':'source')+'-'+String(i+1).padStart(2,'0')+'.png'),animations:'disabled'}));
   }
   assert.deepEqual(images[0],images[1],'Standalone HTML pixels changed: '+labels[i]);
  }
  assert.deepEqual(errors,[]);
  fs.writeFileSync(path.join(output,'report.json'),JSON.stringify({passed:true,engine,pages:labels},null,2));
  console.log('PASS: '+labels.length+' standalone HTML pages have exact source/detached pixels in '+engine);
 }finally{await browser.close();}
})().catch(error=>{console.error(error);process.exitCode=1;});
