const fs=require('node:fs'),assert=require('node:assert/strict'),path=require('node:path'),os=require('node:os'),cp=require('node:child_process');
const {chromium,webkit}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(__dirname,'../../..'),output=fs.mkdtempSync(path.join(os.tmpdir(),'keynope-effects-proof-')),fixture=path.join(output,'fixtures.json');
cp.execFileSync('go',['test','./internal/effects','-run','^TestExportBrowserFixtures$','-count=1'],{cwd:root,env:{...process.env,KEYNOPE_EFFECT_FIXTURES:fixture}});
const fixtures=JSON.parse(fs.readFileSync(fixture));
(async()=>{
 for(const [name,engine] of Object.entries({chromium,webkit})){
  const browser=await engine.launch({headless:true});
  const page=await browser.newPage({viewport:{width:1250,height:920},deviceScaleFactor:1});
  const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.setContent('<body style="margin:0;background:#101522;color:white;font-family:system-ui"><h1>Go-ported Craft effects</h1><div id="gallery" style="display:grid;grid-template-columns:repeat(4,1fr);gap:18px;padding:20px"></div><div id="motion"></div></body>');
  await page.evaluate(()=>globalThis.KEYNOPE_SCENE_SKIP_FONT_CHECK=true);
  for(const file of ['web/text-layout.js','web/scene.js','web/builds.js'])await page.addScriptTag({content:fs.readFileSync(root+'/'+file,'utf8')});
  const report=await page.evaluate(async fixtures=>{
   const gallery=document.querySelector('#gallery'),canvas=document.createElement('canvas');canvas.width=160;canvas.height=100;
   const ctx=canvas.getContext('2d'),pixels=ctx.createImageData(160,100);
   for(let y=15;y<85;y++)for(let x=20;x<140;x++){const i=(y*160+x)*4;pixels.data.set([Math.round(x/159*255),Math.round(y/99*255),102,255],i);}
   ctx.putImageData(pixels,0,0);const source=canvas.toDataURL(),views=[];
   const add=async(title,paint,kind='image')=>{
    const cell=document.createElement('section'),heading=document.createElement('h3'),host=document.createElement('div');heading.textContent=title;cell.append(heading,host);gallery.append(cell);
    const object={id:title,kind,bounds:{x:40,y:40,width:160,height:100},paint:{color:'#ffd166',...paint}};
    if(kind==='image')object.media={source,width:160,height:100,fit:'contain'};
    if(kind==='shape')object.shape='circle';
    if(kind==='retro'){object.kind='image';object.retroLines={cols:12,rows:9,lines:[2,3,4,5,6].map(row=>({row,role:'image',parts:[{col:3,text:'██████',color:'#ff0055'}]}))};}
    if(kind==='text')object.text={role:'text',family:'Arial',size:32,widthScale:1,lineHeight:1.25,align:'start',vertical:'middle',runs:[{text:'Keynope'}]};
    const scene={version:1,width:240,height:180,background:'#171d2c',objects:[object]},view=KeynopeScene.mount(host,scene);await view.ready;
    views.push({view,scene,object,host});return views.at(-1);
   };
   await add('Original',{});
   const checks=[];
   for(const [key,effects]of Object.entries(fixtures.filters)){
    const item=await add(key,{effects}),capture=await item.view.imageArtwork(key),pad=64+Math.ceil(effects.padding),data=capture.getContext('2d').getImageData(pad+80,pad+50,1,1).data;
    if(data[3]<200)throw Error(key+' did not paint: '+data);
    checks.push({key,rgba:[...data],point:{x:item.host.getBoundingClientRect().x+120,y:item.host.getBoundingClientRect().y+90}});
    if(key==='invert'&&Math.abs(data[0]-(255-pixels.data[(50*160+80)*4]))>2)throw Error('Invert not faithful');
    if(key==='posterize'&&Math.abs(data[0]-153)>2)throw Error('Posterize uses wrong thresholds/colour space: '+data);
    if(key==='blur'){
     const fringe=capture.getContext('2d').getImageData(pad+18,pad+50,1,1).data;
     if(fringe[3]<5)throw Error('Blur outside original alpha was clipped');
     const node=item.host.querySelector('[data-object-id]');
     const next=structuredClone(item.scene);next.objects[0].paint.effects=fixtures.filters.invert;
     if(!item.view.update(next)||item.host.querySelector('[data-object-id]')!==node)throw Error('Paint change replaced object');
     const counters=item.view.renderStats();if(counters.objectMounts!==1||counters.textRebuilds!==0)throw Error('Full redraw on effect change');
     for(let i=0;i<30;i++){next.objects[0].bounds.x++;if(!item.view.update(next))throw Error('Move failed');}
     if(item.host.querySelectorAll('filter[id^="keynope-object-effect"]').length!==1)throw Error('Filter definitions accumulated');
     next.objects[0].paint.effects=fixtures.filters.blur;next.objects[0].bounds.x=40;if(!item.view.update(next))throw Error('Could not restore blur after retained-update checks');
    }
   }
   for(const [key,source]of Object.entries(fixtures.gradients))await add(key,{gradientStart:'#ff0055',gradientEnd:'#55ffff',gradientDirection:key,gradientImage:source},'shape');
   await add('Text glow',{effects:fixtures.filters.glow},'text');
   await add('Shape glow',{effects:fixtures.filters.glow},'shape');
   for(const position of ['inside','center','outside'])await add('Stroke '+position,{stroke:'#ffffff',strokeWidth:8,strokePosition:position,strokeSpan:position==='center'?8:16},'shape');
   const retro=await add('Retro invert',{effects:fixtures.filters.invert},'retro'),retroCapture=await retro.view.imageArtwork('Retro invert'),retroPad=64+Math.ceil(fixtures.filters.invert.padding),retroColour=retroCapture.getContext('2d').getImageData(retroPad+60,retroPad+40,1,1).data;
   checks.push({key:'Retro invert',rgba:[...retroColour],point:{x:retro.host.getBoundingClientRect().x+100,y:retro.host.getBoundingClientRect().y+80}});
   const sample=views.find(v=>v.object.id==='glow'),capture=await sample.view.imageArtwork('glow'),pad=64+Math.ceil(fixtures.filters.glow.padding);
   if(capture.getContext('2d').getImageData(pad+17,pad+50,1,1).data[3]<2)throw Error('Glow did not extend beyond alpha');
   // A full-slide static background must stop executing live filters once its
   // exact effect pixels are ready. Foreground updates must reuse that bitmap.
   for(const retro of [false,true])for(const treatment of ['invert','glow']){
    const host=document.createElement('div');gallery.append(host);
    const scene={version:1,width:160,height:100,background:'#171d2c',objects:[],backgroundMedia:{source,width:160,height:100,fit:'contain',paint:{color:'#ffffff',effects:fixtures.filters[treatment]}}};
    if(retro)scene.backgroundMedia.retroLines={cols:8,rows:5,lines:[{row:2,role:'image',parts:[{col:2,text:'████',color:'#ff0055'}]}]};
    const view=KeynopeScene.mount(host,scene);await view.ready;
    const background=host.querySelector('.keynope-scene-background-media'),raster=background.querySelector('.keynope-scene-background-raster');
    if(!raster||view.renderStats().backgroundRasterizations!==1)throw Error('Background effects were not cached');
    const bpad=64+Math.ceil(fixtures.filters[treatment].padding),pixel=[...raster.getContext('2d').getImageData(bpad+80,bpad+50,1,1).data];
    if(retro&&pixel.some((n,i)=>Math.abs(n-(treatment==='invert'?[0,255,170,255]:[255,0,85,255])[i])>2))throw Error('Retro background effects lost: '+pixel);
    if(!retro&&Math.abs(pixel[0]-(treatment==='invert'?255-pixels.data[(50*160+80)*4]:pixels.data[(50*160+80)*4]))>2)throw Error('Background differs from original effect');
    checks.push({key:'Cached '+(retro?'Retro':'modern')+' '+treatment+' background',rgba:pixel,point:{x:host.getBoundingClientRect().x+80,y:host.getBoundingClientRect().y+50}});
    if(treatment==='glow'&&raster.getContext('2d').getImageData(bpad+(retro?37:17),bpad+50,1,1).data[3]<2)throw Error('Cached background lost glow fringe');
    if(background.style.filter.includes('keynope-object-effect'))throw Error('Cached background still has live HTML effects');
    const next=structuredClone(scene);next.objects=[{id:'foreground',kind:'shape',shape:'circle',bounds:{x:1,y:1,width:10,height:10},paint:{color:'#ffffff'}}];
    for(let i=0;i<20;i++){next.objects[0].bounds.x++;if(!view.update(next))throw Error('Background prevented foreground update');view.setTime(i*16);}
    await view.ready;
    if(background.querySelector('.keynope-scene-background-raster')!==raster||view.renderStats().backgroundRasterizations!==1)throw Error('Foreground change rerasterized background');
    next.backgroundMedia.paint.opacity=.5;if(!view.update(next))throw Error('Background alpha edit failed');await view.ready;
    if(view.renderStats().backgroundRasterizations!==1||background.style.opacity!=='0.5')throw Error('Opacity unnecessarily invalidated cached pixels: '+JSON.stringify({retro,stats:view.renderStats(),opacity:background.style.opacity}));
    next.backgroundMedia.paint.opacity=1;next.backgroundMedia.paint.effects=fixtures.filters.tritone;
    if(!view.update(next))throw Error('Background effect edit failed');await view.ready;
    if(view.renderStats().backgroundRasterizations!==2||host.querySelectorAll('.keynope-scene-background-raster').length!==1)throw Error('Background edit did not replace cached pixels');
    next.backgroundMedia=null;if(!view.update(next))throw Error('Background removal failed');await view.ready;
    if(host.querySelector('.keynope-scene-background-raster')||host.querySelector('filter'))throw Error('Removed background leaked raster/filter resources');
    // Restore the sampled view for the screenshot/live comparison below.
    if(!view.update(scene))throw Error('Background restoration failed');await view.ready;
   }
   {
    const host=document.createElement('div'),scene={version:1,width:160,height:100,objects:[],backgroundMedia:{source,width:160,height:100,frames:[{source,delayMs:100},{source,delayMs:100}],paint:{effects:fixtures.filters.blur}}};gallery.append(host);
    const view=KeynopeScene.mount(host,scene);await view.ready;view.setTime(150);
    if(host.querySelector('.keynope-scene-background-raster'))throw Error('Animated background was frozen');
    if(!host.querySelector('[data-keynope-effect-content][filter]')||host.querySelector('.keynope-scene-background-media').style.filter)throw Error('Background missed SVG-native effects path');
    view.dispose();
   }
   {
    const host=document.createElement('div');gallery.append(host);
    const scene={version:1,width:160,height:100,objects:[],backgroundMedia:{source,width:160,height:100,paint:{effects:fixtures.filters.glow}}};
    const view=KeynopeScene.mount(host,scene),next=structuredClone(scene);next.backgroundMedia.paint.effects=fixtures.filters.invert;
    if(!view.update(next))throw Error('Cold background edit failed');await view.ready;
    if(view.renderStats().backgroundRasterizations!==1||host.querySelectorAll('.keynope-scene-background-raster').length!==1)throw Error('Stale background job replaced edited pixels');
    const raster=host.querySelector('canvas.keynope-scene-background-raster'),padding=64+Math.ceil(fixtures.filters.invert.padding);
    if(Math.abs(raster.getContext('2d').getImageData(padding+80,padding+50,1,1).data[0]-(255-pixels.data[(50*160+80)*4]))>2)throw Error('Cold edit rendered stale background effect');
    view.dispose();
   }
   {
    const host=document.createElement('div');gallery.append(host);
    const scene={version:1,width:160,height:100,objects:[],backgroundMedia:{source,width:160,height:100,paint:{effects:fixtures.filters.glow}}};
    const view=KeynopeScene.mount(host,scene);await view.ready;
    const next=structuredClone(scene);next.backgroundMedia.paint.effects=fixtures.filters.blur;
    if(!view.update(next))throw Error('Live blur edit failed');
    const node=host.querySelector('.keynope-scene-background-media');
    if(node.querySelector('.keynope-scene-background-raster')||node.querySelector('svg').style.display==='none'||!node.querySelector('[data-keynope-effect-content][filter]'))throw Error('New effect waited for cached preview');
    // Let the obsolete blur bake start its quiet-period wait, then turn it off.
    await Promise.resolve();await Promise.resolve();
    next.backgroundMedia.paint.effects=fixtures.filters.horizontal;if(!view.update(next))throw Error('Rapid blur edit failed');
    next.backgroundMedia.paint.effects=null;if(!view.update(next))throw Error('Immediate blur removal failed');
    if(node.querySelector('[data-keynope-effect-content][filter]')||node.style.filter)throw Error('Blur remained live after switching off');
    await view.ready;
    if(node.querySelector('.keynope-scene-background-raster')||view.renderStats().backgroundRasterizations!==1)throw Error('An obsolete blur capture became visible');
    view.dispose();
   }
   const frozen=JSON.stringify(sample.scene),bad=structuredClone(sample.scene);bad.objects[0].paint.effects.primitives[0].tag='script';
   if(sample.view.update(bad)!==false||JSON.stringify(sample.scene)!==frozen)throw Error('Unsafe recipe accepted');
   globalThis.KeynopeTransitionCurves=fixtures.dips;
   const host=document.querySelector('#motion'),incoming=document.createElement('div');host.style.cssText='position:relative;width:200px;height:100px';incoming.style.cssText='width:200px;height:100px;background:#dd2222';host.append(incoming);
   const transition=KeynopeBuilds.slideTransition(host,incoming,{reducedMotion:()=>false});
   for(const effect of ['dip-black','dip-white']){transition.hold(transition.snapshot());incoming.style.background='#22dd66';transition.play({effect,durationMs:200},{elapsed:100});if(!transition.active())throw Error('Dip not active');transition.cancel();if(host.querySelector('filter')||incoming.style.filter||incoming.style.visibility)throw Error('Dip cleanup failed');}
   incoming.style.background='#dd2222';transition.hold(transition.snapshot());incoming.style.background='#22dd66';transition.play({effect:'dip-black',durationMs:60000},{elapsed:15000});transition.pause();
   checks.push({key:'Dip linear-light quarter',rgba:[162,22,22,255],point:{x:host.getBoundingClientRect().x+50,y:host.getBoundingClientRect().y+50}});
   return checks;
  },fixtures);
  assert.equal(errors.length,0,errors.join('\n'));
  const screenshot=await page.screenshot({path:path.join(output,name+'.png'),fullPage:true});
  const samples=await page.evaluate(async({base64,report})=>{const image=new Image();image.src='data:image/png;base64,'+base64;await image.decode();const canvas=document.createElement('canvas');canvas.width=image.width;canvas.height=image.height;const ctx=canvas.getContext('2d');ctx.drawImage(image,0,0);return report.filter(row=>row.key!=='blur').map(row=>({key:row.key,expected:row.rgba,actual:[...ctx.getImageData(row.point.x,row.point.y,1,1).data]}))},{base64:screenshot.toString('base64'),report});
  for(const sample of samples)assert.ok(sample.actual.every((v,i)=>Math.abs(v-sample.expected[i])<=4),name+' live/export mismatch: '+JSON.stringify(sample));
  console.log(name+': live/export pixels, Retro effects, linear-light dips and retained updates passed. Evidence: '+output);await browser.close();
 }
})().catch(e=>{console.error(e);process.exit(1)});
