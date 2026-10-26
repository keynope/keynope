const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright');
(async()=>{
 const browser=await pw.chromium.launch({headless:true,...(process.env.CHROME_PATH?{executablePath:process.env.CHROME_PATH}:{})});
 try{
  const page=await browser.newPage({viewport:{width:1000,height:1000}}),errors=[];
  page.on('pageerror',error=>errors.push(error.message));
  await page.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());
  await page.goto(process.argv[2]);await page.waitForFunction(()=>typeof openEngagementRuntime==='function'&&typeof modernPageView!=='undefined'&&modernPageView);
  await page.evaluate(()=>openEngagementRuntime(true));
  const qr=page.getByRole('img',{name:'Activity QR code',exact:true});
  // The old text element has only an aria-label, so check its geometry before
  // requiring the font-independent image role.
  const previous=page.locator('.keynope-engagement-qr');await previous.waitFor();
  const output=process.env.KEYNOPE_QR_EVIDENCE;
  if(output){fs.mkdirSync(output,{recursive:true});await previous.screenshot({path:path.join(output,'initial.png')});}
  const bounds=await previous.boundingBox();assert(Math.abs(bounds.width-bounds.height)<1,'Activity QR is stretched instead of square');
  await qr.waitFor();
  const encoded=await page.evaluate(()=>keynopeEngagementRuntime.qrCode);
  const rows=encoded.split('\n'),size=rows.length+6;
  for(const width of [1000,360])for(const mode of ['modern','retro']){
   await page.setViewportSize({width,height:1000});
   await page.evaluate(mode=>{
    KeynopeActivityDesign.appearance(keynopeEngagementOverlay,mode);
    KeynopeActivityDesign.appearance(keynopeEngagementOverlay.querySelector('.keynope-engagement-content'),mode);
    // A slide font or a UI font must not affect the QR module geometry.
    keynopeEngagementOverlay.style.setProperty('font-family',mode==='modern'?'Arial':'serif','important');
   },mode);
   const box=await qr.boundingBox(),content=await qr.locator('..').boundingBox();
   assert(Math.abs(box.width-box.height)<1,'QR aspect ratio changed');
   assert(Math.abs(box.x+box.width/2-content.x-content.width/2)<1,'QR is not horizontally centred');
   assert(box.x>=0&&box.x+box.width<=width,'QR overflows the viewport');
   const screenshot=await qr.screenshot();
   if(output)fs.writeFileSync(path.join(output,mode+'-'+width+'.png'),screenshot);
   const pixels=await page.evaluate(async({png,size})=>{
    const image=new Image();image.src='data:image/png;base64,'+png;await image.decode();
    const canvas=document.createElement('canvas');canvas.width=image.width;canvas.height=image.height;
    const context=canvas.getContext('2d');context.drawImage(image,0,0);
    const samples=[];
    for(let y=0;y<size;y++)for(let x=0;x<size;x++){
     const pixel=context.getImageData(Math.floor((x+.5)*image.width/size),Math.floor((y+.5)*image.height/size),1,1).data;
     samples.push(pixel[0]<128);
    }
    return samples;
   },{png:screenshot.toString('base64'),size});
   for(let y=0;y<size;y++)for(let x=0;x<size;x++){
    const dark=y>=3&&y<size-3&&x>=3&&x<size-3&&rows[y-3][(x-3)*2]==='█';
    assert.equal(pixels[y*size+x],dark,'Wrong painted QR module at '+x+','+y+' in '+mode+' '+width);
   }
  }
  await page.evaluate(()=>{keynopeHideActivityQR=true;renderEngagementRuntime();});
  assert.equal(await page.locator('.keynope-engagement-qr').count(),0,'QR visibility preference is ignored');
  assert(await page.locator('.keynope-engagement-join-text').isVisible(),'Hidden QR must retain the join URL and code');
  assert.deepEqual(errors,[]);
  console.log('PASS: onboarding QR module pixels, square geometry, centring, fonts, responsive sizing and QR visibility preference');
 }finally{await browser.close();}
})().catch(error=>{console.error(error);process.exitCode=1;});
