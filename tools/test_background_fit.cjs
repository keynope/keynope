// Inspect engine-generated background framing across sources and treatments.
// Local synthetic fixtures only; never activates the native application.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),output=process.env.KEYNOPE_BACKGROUND_FIT_EVIDENCE||'/tmp/keynope-background-fit-proof';
const fixtures=JSON.parse(fs.readFileSync(path.join(output,'scenes.json')));
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 const browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
 try{
  const context=await browser.newContext({viewport:{width:720,height:450},reducedMotion:'reduce'}),page=await context.newPage(),errors=[],records=[];
  await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());page.on('pageerror',error=>errors.push(error.message));
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  for(const fixture of fixtures){
   const result=await page.evaluate(async fixture=>{
    const host=document.createElement('div');host.id='background-fit-proof';Object.assign(host.style,{position:'fixed',left:'0',top:'0',width:'720px',height:'405px',zIndex:1000000,background:'#111'});document.body.append(host);
    const view=KeynopeScene.mount(host,fixture.scene);window.backgroundFitView=view;
    Object.assign(view.element.style,{transformOrigin:'0 0',transform:'scale('+720/fixture.scene.width+')'});
    await view.snapshotAt(150);await new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)));
    const bg=host.querySelector('.keynope-scene-background-media');if(!bg)throw Error(fixture.name+' missing background');
    const result={name:fixture.name,backgroundChildren:bg.childElementCount,images:bg.querySelectorAll('img').length,canvas:bg.querySelectorAll('canvas').length,diagnostics:view.diagnostics};
    if(!result.images&&!result.canvas)throw Error(fixture.name+' has no rendered media');
    const sampled=bg.querySelector('canvas.keynope-scene-retro-text');
    if(sampled){
     const pixels=sampled.getContext('2d').getImageData(0,0,sampled.width,sampled.height).data;let left=sampled.width,right=-1,top=sampled.height,bottom=-1;
     for(let y=0;y<sampled.height;y++)for(let x=0;x<sampled.width;x++)if(pixels[(y*sampled.width+x)*4+3]>0){left=Math.min(left,x);right=Math.max(right,x);top=Math.min(top,y);bottom=Math.max(bottom,y);}
     result.sampledPixels={width:right-left+1,height:bottom-top+1};
     if(result.sampledPixels.width<fixture.scene.width*.4||result.sampledPixels.height<fixture.scene.height*.85)throw Error(fixture.name+' shrank sampled artwork: '+JSON.stringify(result.sampledPixels));
    }
    return result;
   },fixture);
   await page.locator('#background-fit-proof').screenshot({path:path.join(output,fixture.name+'.png')});records.push(result);
   await page.evaluate(()=>{backgroundFitView.dispose();document.querySelector('#background-fit-proof').remove();});
  }
  assert.equal(records.length,18);assert.deepEqual(errors,[]);
  const sheet=await context.newPage();await sheet.setViewportSize({width:1500,height:1800});
  await sheet.route('**/proof-images/*',route=>route.fulfill({contentType:'image/png',body:fs.readFileSync(path.join(output,path.basename(new URL(route.request().url()).pathname)))}));
  await sheet.setContent('<style>body{margin:0;background:#111;color:white;font:13px monospace;display:grid;grid-template-columns:repeat(3,1fr);gap:8px}figure{margin:0}img{width:100%}</style>'+records.map(record=>'<figure><img src="http://127.0.0.1:'+server.address().port+'/proof-images/'+record.name+'.png"><figcaption>'+record.name+'</figcaption></figure>').join(''));
  await sheet.locator('img').evaluateAll(images=>Promise.all(images.map(image=>image.decode())));await sheet.screenshot({path:path.join(output,'contact-sheet.png'),fullPage:true});
  fs.writeFileSync(path.join(output,'report.json'),JSON.stringify({browser:browser.version(),records,errors},null,2));
  console.log('PASS: 18 engine-generated still/GIF/SVG × Modern/Retro × contain/cover/stretch background views captured; visual inspection is separate.');
 }finally{await browser.close();server.close();}
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
