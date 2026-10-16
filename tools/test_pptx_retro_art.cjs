// Compare exported Go PNG pixels against the actual shared canvas painter.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright'),engine=process.env.TEST_BROWSER||'chromium';
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),proof=process.env.KEYNOPE_RETRO_PPTX_EVIDENCE||'/tmp/keynope-retro-pptx-proof';
const fixtures=JSON.parse(fs.readFileSync(path.join(proof,'fixtures.json'),'utf8'));
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));const browser=await playwright[engine].launch({headless:true,...(engine==='webkit'?{}:{executablePath:process.env.CHROME_PATH})});
 try{
  const context=await browser.newContext({viewport:{width:1400,height:1000}});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const results=await page.evaluate(async fixtures=>{
   const results=[],sheet=document.createElement('section');sheet.id='retro-proof';Object.assign(sheet.style,{position:'fixed',inset:0,zIndex:99999,background:'#fff',color:'#111',padding:'16px',overflow:'auto',font:'14px monospace'});document.body.append(sheet);
   for(const fixture of fixtures){
    const host=document.createElement('div');document.body.append(host);const view=KeynopeScene.mount(host,fixture.scene||{version:1,width:1920,height:1080,objects:[fixture.object]});await view.ready;await view.snapshotAt(0);
    const actual=host.querySelector('.keynope-scene-retro-text');if(!actual)throw Error('Missing sampled canvas');
    for(const [index,capture]of (fixture.frames||[{png:fixture.png,time:0}]).entries()){
    await view.snapshotAt(capture.time);
    const expected=document.createElement('canvas');expected.width=actual.width;expected.height=actual.height;const image=new Image();image.src='data:image/png;base64,'+capture.png;await image.decode();if(image.width!==actual.width||image.height!==actual.height)throw Error('Raster dimensions differ');expected.getContext('2d').drawImage(image,0,0);
    const a=actual.getContext('2d').getImageData(0,0,actual.width,actual.height).data,b=expected.getContext('2d').getImageData(0,0,expected.width,expected.height).data;let max=0,different=0;for(let i=0;i<a.length;i++){const delta=Math.abs(a[i]-b[i]);max=Math.max(max,delta);if(delta>2)different++;}
    const name=fixture.object.id+(fixture.frames?'/frame-'+index:'');results.push({name,maxChannelDelta:max,channelsOverTolerance:different,pixels:actual.width*actual.height});
    const heading=document.createElement('h2');heading.textContent=name+' — shared renderer / exported pixels';sheet.append(heading);const row=document.createElement('div');row.style.cssText='display:flex;gap:24px';for(const source of [actual,expected]){const preview=document.createElement('canvas');preview.width=source.width;preview.height=source.height;preview.getContext('2d').drawImage(source,0,0);preview.style.cssText='width:500px;image-rendering:pixelated;background:repeating-conic-gradient(#eee 0% 25%,#fff 0% 50%) 0/16px 16px;border:1px solid #aaa';row.append(preview);}sheet.append(row);
    }view.dispose();host.remove();
   }return results;
  },fixtures);
  for(const result of results)assert.equal(result.channelsOverTolerance,0,JSON.stringify(result));assert.deepEqual(errors,[]);await page.screenshot({path:path.join(proof,'renderer-versus-png-'+engine+'.png')});await page.evaluate(()=>{[...document.querySelectorAll('#retro-proof h2')].find(h=>h.textContent.startsWith('animation/frame-1'))?.scrollIntoView();});await page.screenshot({path:path.join(proof,'animation-versus-gif-'+engine+'.png')});await page.evaluate(()=>{const sheet=document.querySelector('#retro-proof');sheet.scrollTop=sheet.scrollHeight;});await page.screenshot({path:path.join(proof,'background-versus-png-'+engine+'.png')});fs.writeFileSync(path.join(proof,'browser-report-'+engine+'.json'),JSON.stringify(results,null,2));console.log(engine+': '+JSON.stringify(results));
 }finally{await browser.close();server.close();}
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
