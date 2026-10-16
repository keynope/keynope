// Authored physical sizes through shared quality/exact HTML and browser print.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),{execFileSync}=require('node:child_process');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright'),root=path.resolve(process.env.KEYNOPE_PAGE_SIZE_PDF_PROOF||'output/pdf/page-sizes');
(async()=>{
 const browser=await pw.chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
 try{
  const context=await browser.newContext();await context.route(/https?:\/\/.*/,r=>r.abort());await context.addInitScript(()=>window.print=()=>{});
  for(const [width,height]of [[1440,1080],[1080,1920],[2560,1080]]){
   const dir=path.join(root,width+'x'+height),page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));await page.setViewportSize({width,height});
   await page.goto('file://'+path.join(dir,'quality.html'));await page.evaluate(()=>keynopePrepareBrowserPrint());
   assert.equal(await page.locator('.keynope-print-page').count(),2);assert.equal(await page.locator('iframe').count(),1);
   const dimensions=await page.locator('.keynope-print-page').evaluateAll(nodes=>nodes.map(n=>({width:n.clientWidth,height:n.clientHeight})));assert.deepEqual(dimensions,[{width,height},{width,height}]);
   await page.pdf({path:path.join(dir,'browser-combined.pdf'),preferCSSPageSize:true,printBackground:true});
   const info=execFileSync('pdfinfo',[path.join(dir,'browser-combined.pdf')],{encoding:'utf8'});
   assert.match(info,/Pages:\s+2\b/);const box=info.match(/Page size:\s+([\d.]+) x ([\d.]+) pts/);assert(box);assert(Math.abs(Number(box[1])-width)<1&&Math.abs(Number(box[2])-height)<1,'Browser and native physical media boxes differ: '+box[0]);
   if(process.env.KEYNOPE_COMPARE_NATIVE_PDF==='1'){
    const words=file=>execFileSync('pdftotext',['-bbox-layout',file,'-'],{encoding:'utf8'}).split('</page>')[0];
    const native=words(path.join(dir,'native-quality.pdf')),printed=words(path.join(dir,'browser-combined.pdf'));
    for(const text of ['Page','physical','Every']){
     const bounds=xml=>{const match=xml.match(new RegExp('<word ([^>]+)>'+text+'</word>'));assert(match,'Missing selectable '+text);return ['xMin','yMin','xMax','yMax'].map(key=>Number(match[1].match(new RegExp(key+'="([^"]+)"'))[1]));};
     const a=bounds(native),b=bounds(printed);a.forEach((value,i)=>assert(Math.abs(value-b[i])<1.5,text+' physical text bounds differ: '+a+' / '+b));
    }
   }
   await page.evaluate(()=>keynopePrepareSnapshot(0));await page.screenshot({path:path.join(dir,'browser-quality.png')});
   await page.goto('file://'+path.join(dir,'exact.html'));const rect=await page.evaluate(()=>keynopePrepareSnapshot(1));assert(Math.abs(rect.width-width)<1&&Math.abs(rect.height-height)<1,JSON.stringify(rect));
   await page.screenshot({path:path.join(dir,'browser-exact.png'),clip:{x:rect.x,y:rect.y,width:rect.width,height:rect.height}});
   assert.deepEqual(errors,[]);await page.close();console.log('PASS browser '+width+'x'+height+': CSS page dimensions, quality/exact preparation, two-page PDF, private-tab exclusion.');
  }
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
