// Cold native-editor image insertion/treatment, checked by visible pixels.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const {PNG}=require(path.join(process.env.PLAYWRIGHT_MODULE||path.dirname(require.resolve('playwright/package.json')),'../playwright-core/lib/utilsBundle.js'));
const output=fs.mkdtempSync('/tmp/keynope-image-first-paint-');
(async()=>{
 for(const engine of ['chromium','webkit']){
  const browser=await pw[engine].launch({headless:true});
  try{
   const context=await browser.newContext({viewport:{width:1440,height:1000},reducedMotion:'reduce'}),page=await context.newPage(),errors=[];
   await page.addInitScript(()=>{window.__imageSourceMounts=[];const set=SVGElement.prototype.setAttribute;SVGElement.prototype.setAttribute=function(name,value){if(this instanceof SVGImageElement&&name==='href'&&String(value).startsWith('data:image/'))window.__imageSourceMounts.push({connected:this.isConnected,width:this.getAttribute('width'),height:this.getAttribute('height')});return set.call(this,name,value);};});
   await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());page.on('pageerror',error=>errors.push(error.message));
   await page.goto(process.argv[2]);await page.waitForFunction(()=>typeof KeynopeScene!=='undefined'&&!!document.querySelector('.keynope-canvas-element'));
   const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
   const select=async id=>{const s=await state(),index=s.slides[s.current].elements.findIndex(e=>e.id===id);await page.evaluate(index=>editorAction({action:'select-element',element:index}),index);await page.locator('[data-ribbon-tab="content"]').click();};
   const visible=async(id,label)=>{
    await page.waitForFunction(id=>modernPageView&&deck.pages[pageIndex]?.scene?.objects.some(o=>o.id===id),id);
    await page.evaluate(async()=>{for(let n=0;n<20;n++){const view=modernPageView;await view.ready;await new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)));if(view===modernPageView&&view.element.isConnected&&modernPageSource===deck.pages[pageIndex].scene)return;}throw Error('Native scene readiness never settled');});
    const node=page.locator('#stage .keynope-modern-scene [data-object-id="'+id+'"]');await node.waitFor();
    const clip=await page.evaluate(id=>{const rect=document.querySelector('#stage .keynope-modern-scene [data-object-id="'+id+'"]')?.getBoundingClientRect();return rect?{x:rect.x,y:rect.y,width:rect.width,height:rect.height}:null;},id);assert(clip&&clip.width>0&&clip.height>0,'Image has no visible bounds');
    const bytes=await page.screenshot({clip,path:path.join(output,engine+'-'+label+'.png')}),png=PNG.sync.read(bytes);let coloured=0;
    for(let i=0;i<png.data.length;i+=4){const [r,g,b]=png.data.subarray(i,i+3);if(r>160&&g<100&&b>60||b>160&&g>70&&r<100)coloured++;}
    if(coloured<=100){
     console.log('BLANK '+JSON.stringify(await page.evaluate(id=>{const node=document.querySelector('#stage .keynope-modern-scene [data-object-id="'+id+'"]');return {scene:deck.pages[pageIndex]?.scene?.objects.find(o=>o.id===id)?.media?.fit,layer:modernPageLayer.getAttribute('style'),parents:node&&[node,node.parentElement,node.parentElement.parentElement].map(n=>({class:n.className,style:n.getAttribute('style')})),html:node?.outerHTML.replace(/data:image\/[^"\s]+/g,'DATA'),img:node?.querySelector('img')&&{complete:node.querySelector('img').complete,width:node.querySelector('img').naturalWidth}};},id)));
     await page.waitForTimeout(1000);await page.screenshot({clip,path:path.join(output,engine+'-'+label+'-late.png')});
    }
    assert(coloured>100,engine+' '+label+' painted no image pixels ('+coloured+'). '+output);
   };
   // Upload must be the first SVG-image operation in a fresh browser. A style
   // round-trip beforehand warms the paint resources and misses cold insertion.
   const png=new PNG({width:1200,height:900});for(let i=0;i<png.data.length;i+=4){png.data[i]=25+(i/4%79);png.data[i+1]=130;png.data[i+2]=245;png.data[i+3]=255;}
   await page.getByRole('button',{name:'Insert',exact:true}).click();const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Import image',exact:true}).click();await(await chooser).setFiles({name:engine+'-cold.png',mimeType:'image/png',buffer:PNG.sync.write(png)});
   await page.waitForFunction(()=>document.querySelectorAll('.keynope-canvas-element').length===2);const s=await state(),inserted=s.slides[0].elements.find(e=>e.id!=='cold-image');await visible(inserted.id,'cold-inserted');
   await page.evaluate(async id=>{const s=await fetch('/api/editor/state').then(r=>r.json());await editorAction({action:'delete-element',element:s.slides[0].elements.findIndex(e=>e.id===id)});},inserted.id);
   await select('cold-image');await visible('cold-image','initial-retro');
   await page.getByRole('combobox',{name:'Image style',exact:true}).selectOption('modern');await page.waitForFunction(()=>{const o=deck.pages[pageIndex]?.scene?.objects.find(o=>o.id==='cold-image');return o?.media&&!o.retroLines&&o.style==='modern';});await visible('cold-image','first-modern');
   await page.getByRole('combobox',{name:'Image style',exact:true}).selectOption('retro');await page.waitForFunction(()=>deck.pages[pageIndex]?.scene?.objects.find(o=>o.id==='cold-image')?.retroLines);await visible('cold-image','back-to-retro');
   const mounts=await page.evaluate(()=>window.__imageSourceMounts);assert(mounts.length>0);assert(mounts.every(m=>m.connected&&Number(m.width)>0&&Number(m.height)>0),'Image source assigned before its visible SVG/frame was mounted');
   assert.deepEqual(errors,[]);console.log('PASS '+engine+': cold insertion, mounted source/frame, Retro/Modern swap visible without moving or crop interaction. '+output);
  }finally{await browser.close();}
 }
})().catch(error=>{console.error(error);process.exitCode=1;});
