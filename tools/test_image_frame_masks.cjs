// Real engine projections: fractional, rotated foreground frames in both treatments.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),output=process.env.KEYNOPE_FRAME_MASK_EVIDENCE||'/tmp/keynope-frame-mask-proof';
const fixtures=JSON.parse(fs.readFileSync(path.join(output,'scenes.json')));
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));const browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
 try{
  const context=await browser.newContext({viewport:{width:960,height:560}}),page=await context.newPage(),errors=[],records=[];
  await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());page.on('pageerror',error=>errors.push(error.message));
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  for(const fixture of fixtures.sort((a,b)=>a.name.localeCompare(b.name))){
   const record=await page.evaluate(async fixture=>{
    const host=document.createElement('div');host.id='frame-mask-proof';Object.assign(host.style,{position:'fixed',left:0,top:0,width:'960px',height:'540px',zIndex:1000000,background:'#18202a'});document.body.append(host);
    const view=KeynopeScene.mount(host,fixture.scene);window.frameMaskView=view;Object.assign(view.element.style,{transformOrigin:'0 0',transform:'scale(.5)'});await view.snapshotAt(170);
    const objects=fixture.scene.objects.map(object=>{const node=host.querySelector('[data-object-id="'+object.id+'"]');if(!node)throw Error('Missing frame '+object.id);const img=node.querySelector('img'),canvas=node.querySelector('canvas');if(img&&(!img.complete||!img.naturalWidth))throw Error('Image failed to decode');if(!img&&!canvas)throw Error('No rendered media');if(canvas){const pixels=canvas.getContext('2d').getImageData(0,0,canvas.width,canvas.height).data;let count=0;for(let i=3;i<pixels.length;i+=4)if(pixels[i])count++;if(!count)throw Error('Empty sampled frame');}return {id:object.id,rotation:object.rotation||0,bounds:object.bounds,images:node.querySelectorAll('img').length,canvases:node.querySelectorAll('canvas').length};});
    for(const object of fixture.scene.objects){const rect=host.querySelector('[data-object-id="'+object.id+'"]').getBoundingClientRect(),b=object.bounds;const cx=(b.x+b.width/2)*.5,cy=(b.y+b.height/2)*.5;if(Math.abs(rect.x+rect.width/2-cx)>6||Math.abs(rect.y+rect.height/2-cy)>6)throw Error('Rendered frame moved away from authored centre: '+object.id);}
    return {name:fixture.name,objects,diagnostics:view.diagnostics};
   },fixture);
   records.push(record);await page.locator('#frame-mask-proof').screenshot({path:path.join(output,fixture.name+'.png')});await page.evaluate(()=>{frameMaskView.dispose();document.querySelector('#frame-mask-proof').remove();});
  }
  assert.equal(records.length,18);assert.deepEqual(errors,[]);
  const sheet=await context.newPage();await sheet.setViewportSize({width:1440,height:1800});await sheet.route('**/proof-images/*',route=>route.fulfill({contentType:'image/png',body:fs.readFileSync(path.join(output,path.basename(new URL(route.request().url()).pathname)))}));
  await sheet.setContent('<style>body{margin:0;background:#111;color:#fff;font:14px monospace;display:grid;grid-template-columns:repeat(3,1fr);gap:8px}figure{margin:0}img{width:100%}</style>'+records.map(record=>'<figure><img src="http://127.0.0.1:'+server.address().port+'/proof-images/'+record.name+'.png"><figcaption>'+record.name+'</figcaption></figure>').join(''));
  await sheet.locator('img').evaluateAll(images=>Promise.all(images.map(image=>image.decode())));await sheet.screenshot({path:path.join(output,'contact-sheet.png'),fullPage:true});fs.writeFileSync(path.join(output,'report.json'),JSON.stringify({browser:browser.version(),records,errors},null,2));
  console.log('PASS: 54 fractional/rotated/cropped rounded frames across still/SVG/GIF, Modern/Retro and contain/cover/stretch; captures require visual inspection.');
 }finally{await browser.close();server.close();}
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
