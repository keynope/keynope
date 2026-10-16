const {waitForAsync}=require("./wait_for_async.cjs");
// Production media renderer: retained/fresh pixels, filter ownership and GIF clock.
// Owned local fixtures only; no Keynope/Office application activation.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright'),root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-chart-token-site'),output=process.env.KEYNOPE_MEDIA_PATCH_EVIDENCE||'/tmp/keynope-media-patch-proof';
const engine=process.env.TEST_BROWSER||'chromium';
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
  fs.mkdirSync(output,{recursive:true});await new Promise(r=>server.listen(0,'127.0.0.1',r));const browser=await playwright[engine].launch({headless:true,...(engine==='webkit'?{}:{executablePath:process.env.CHROME_PATH})});
  try{
    const context=await browser.newContext({viewport:{width:960,height:640},reducedMotion:'no-preference'});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
    page.on('dialog',d=>d.accept());
    const records=[];
    for(const kind of ['png','svg','animated']){
      await page.evaluate(async kind=>{
        const png=colour=>{const c=document.createElement('canvas');c.width=160;c.height=100;const x=c.getContext('2d');x.fillStyle=colour;x.fillRect(15,15,130,70);x.clearRect(40,30,25,30);x.fillStyle='#ffffaa';x.fillRect(80,35,50,15);return c.toDataURL();};
        const first=png('#ef4444'),second=png('#2563eb'),vector='data:image/svg+xml;base64,'+btoa('<svg xmlns="http://www.w3.org/2000/svg" width="160" height="100"><path fill="#ef4444" fill-rule="evenodd" d="M15 15H145V85H15Z M40 30H65V60H40Z"/><rect fill="#ffffaa" x="80" y="35" width="50" height="15"/></svg>');
        const host=document.createElement('div');host.id='media-patch-proof';Object.assign(host.style,{position:'fixed',left:0,top:0,width:'900px',height:'560px',zIndex:999999,background:'#17202a'});document.body.append(host);
        window.__mediaScene={version:1,width:900,height:560,background:'#17202a',objects:[{id:'other',kind:'shape',shape:'circle',bounds:{x:630,y:50,width:100,height:100},paint:{color:'#55ffaa'}},{id:'media',kind:'image',bounds:{x:80,y:70,width:450,height:280},paint:{color:'#ffffff'},media:{source:kind==='svg'?vector:first,width:160,height:100,...(kind==='animated'?{frames:[{source:first,delayMs:100},{source:second,delayMs:200}],loopCount:0}:{})}}]};
        window.__mediaView=KeynopeScene.mount(host,window.__mediaScene);await window.__mediaView.snapshotAt(180);
        window.__mediaNodes={owner:host.querySelector('[data-object-id="media"]'),image:host.querySelector('img'),paint:host.querySelector('[data-object-id="media"] image'),other:host.querySelector('[data-object-id="other"]')};window.__mediaSource=window.__mediaNodes.image.src;
      },kind);
      const compare=async(a,b)=>page.evaluate(async({a,b})=>{const decode=async value=>{const img=new Image();img.src='data:image/png;base64,'+value;await img.decode();const canvas=document.createElement('canvas');canvas.width=img.width;canvas.height=img.height;const c=canvas.getContext('2d');c.drawImage(img,0,0);return c.getImageData(0,0,img.width,img.height).data;};const [x,y]=await Promise.all([decode(a),decode(b)]);let sum=0,large=0;for(let i=0;i<x.length;i++){const d=Math.abs(x[i]-y[i]);sum+=d;if(d>20)large++;}return {mean:sum/x.length,largeFraction:large/x.length};},{a:a.toString('base64'),b:b.toString('base64')});
      let previous=await page.locator('#media-patch-proof').screenshot();
      const mutations=[['opacity',s=>s.objects[1].paint.opacity=.65],['gradient',s=>Object.assign(s.objects[1].paint,{gradientStart:'#55aaff',gradientEnd:'#ff55aa',gradientDirection:'diagonal'})],['outline',s=>Object.assign(s.objects[1].paint,{stroke:'#ffffff',strokeWidth:4})],['shadow',s=>Object.assign(s.objects[1].paint,{shadowColor:'#ffdd55',shadowX:10,shadowY:8,shadowBlur:5})],['colour correction',s=>{delete s.objects[1].paint.gradientStart;delete s.objects[1].paint.gradientEnd;s.objects[1].media.colourMatrices=[[.5,0,0,0,.1,0,.8,0,0,0,0,0,.6,0,0,0,0,0,1,0]];}],['sharpness',s=>Object.assign(s.objects[1].media,{sharpness:.6,sharpnessAfter:1})],['mask',s=>s.objects[1].media.mask='ellipse'],['crop',s=>s.objects[1].media.crop={left:.12,right:.05,top:.08,bottom:.1}],['resize',s=>Object.assign(s.objects[1].bounds,{width:380,height:320})],['move/rotate',s=>{s.objects[1].bounds.x+=35;s.objects[1].rotation=12;}],['reset',s=>{s.objects[1].paint={color:'#ffffff'};delete s.objects[1].media.colourMatrices;delete s.objects[1].media.sharpness;delete s.objects[1].media.sharpnessAfter;delete s.objects[1].media.mask;delete s.objects[1].media.crop;}]];
      for(const [name,fn]of mutations){
        const result=await page.evaluate(async({name,fn})=>{const scene=structuredClone(window.__mediaScene);(0,eval)('('+fn+')')(scene);const view=window.__mediaView,before=view.renderStats(),resources=view.resourceCounts();if(!view.update(scene))throw Error(name+' refused retained update');await view.ready;await new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)));const host=document.getElementById('media-patch-proof'),nodes=window.__mediaNodes;
          if(nodes.owner!==host.querySelector('[data-object-id="media"]')||nodes.image!==host.querySelector('img')||nodes.other!==host.querySelector('[data-object-id="other"]'))throw Error(name+' remounted image/owner/unrelated object');
          if(nodes.image.src!==window.__mediaSource)throw Error(name+' reset the animation frame/source');
          if(nodes.paint!==host.querySelector('[data-object-id="media"] image')||nodes.paint.getAttribute('href')!==nodes.image.src)throw Error(name+' replaced or desynchronized the visible SVG image');
          for(const filter of nodes.owner.querySelectorAll('filter'))if(filter.querySelector('feColorMatrix,feConvolveMatrix')){
            const box=scene.objects[1].bounds;
            if(filter.getAttribute('filterUnits')!=='userSpaceOnUse'||Number(filter.getAttribute('x'))!==-1||Number(filter.getAttribute('y'))!==-1||Number(filter.getAttribute('width'))!==box.width+2||Number(filter.getAttribute('height'))!==box.height+2)throw Error(name+' treatment surface does not follow the visible frame');
          }
          if(view.renderStats().objectMounts!==before.objectMounts)throw Error(name+' mounted objects');
          if(JSON.stringify(view.resourceCounts())!==JSON.stringify(resources))throw Error(name+' changed resources');
          window.__mediaScene=scene;return {stats:view.renderStats(),definitions:host.querySelector('[data-object-id="media"]').querySelectorAll('filter').length};
        },{name,fn:String(fn)});
        assert.deepEqual(result.stats.lastChangedObjectIDs,['media'],kind+'/'+name+' omitted invalidation');assert(result.definitions<=2,'filter definitions leaked');
        const retained=await page.locator('#media-patch-proof').screenshot();
        const visibleChange=await compare(retained,previous);assert(visibleChange.mean>.005,kind+'/'+name+' did not visibly change the fixture');previous=retained;
        await page.evaluate(async()=>{const host=document.createElement('div');host.id='media-fresh-proof';Object.assign(host.style,{position:'fixed',left:0,top:0,width:'900px',height:'560px',zIndex:1000000,background:'#17202a'});document.body.append(host);window.__mediaFresh=KeynopeScene.mount(host,structuredClone(window.__mediaScene));await window.__mediaFresh.snapshotAt(180);await new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)));});
        const fresh=await page.locator('#media-fresh-proof').screenshot();
        const parity=await compare(retained,fresh);
        assert(parity.mean<.15&&parity.largeFraction<.001,kind+'/'+name+' differs from fresh pixels: '+JSON.stringify(parity));records.push({kind,name,...parity,visibleChange,...result});
        if(name==='crop')fs.writeFileSync(path.join(output,kind+'-crop.png'),retained);
        await page.evaluate(()=>{window.__mediaFresh.dispose();document.getElementById('media-fresh-proof').remove();});
      }
      const rejected=await page.evaluate(()=>{const view=window.__mediaView,host=document.getElementById('media-patch-proof'),before=host.innerHTML,stats=JSON.stringify(view.renderStats());for(const type of ['matrix','colour','number','negative-blur','crop']){const scene=structuredClone(window.__mediaScene);scene.objects[0].paint.color='#ff0000';const image=scene.objects[1];if(type==='matrix')image.media.colourMatrices=[[1]];if(type==='colour'){image.paint.gradientStart='var(--missing)';image.paint.gradientEnd='#ffffff';}if(type==='number')image.paint.shadowX=NaN;if(type==='negative-blur')image.paint.shadowBlur=-1;if(type==='crop')image.media.crop={left:.6,right:.6,top:0,bottom:0};if(view.update(scene)!==false||host.innerHTML!==before||JSON.stringify(view.renderStats())!==stats)throw Error(type+' mutated a rejected batch');}return true;});assert.equal(rejected,true);
      if(kind==='animated')await page.evaluate(async()=>{const view=window.__mediaView,img=window.__mediaNodes.image,frames=window.__mediaScene.objects[1].media.frames;await view.snapshotAt(310);if(img.src!==frames[0].source)throw Error('Retained animation did not wrap to first frame');await view.snapshotAt(430);if(img.src!==frames[1].source)throw Error('Retained animation did not continue after adjustments');});
      await page.evaluate(()=>{window.__mediaView.dispose();document.getElementById('media-patch-proof').remove();});
    }
    // Real editor import + Go/WASM command projection, not only synthetic scenes.
    // The pixel fixtures use a small fixed surface. Editor toolbar checks use
    // a normal desktop window; narrow-window inspector overlap is a separate
    // outstanding workspace-layout issue, not bypassed with forced clicks.
    await page.setViewportSize({width:1600,height:1100});
    const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles({name:'Media.md',mimeType:'text/markdown',buffer:Buffer.from('<!-- keynope width=245 height=56 -->\n\n<!-- top=1 left=10 render=truetype modern-font=sans modern-size=55 -->\nMedia controls\n')});await page.waitForFunction(()=>deck.pages[0]?.scene?.objects.some(o=>o.text?.runs?.some(r=>r.text==='Media controls')));
    await page.evaluate(()=>editorAction({action:'select-element',element:-1}));await page.getByRole('button',{name:'Insert',exact:true}).click();const imageChooser=page.waitForEvent('filechooser');imageChooser.catch(()=>{});await page.getByRole('button',{name:'Import image',exact:true}).click();const pngSource=await page.evaluate(()=>window.__mediaScene.objects[1].media.source);await(await imageChooser).setFiles({name:'fixture.png',mimeType:'image/png',buffer:Buffer.from(pngSource.split(',')[1],'base64')});
    await page.waitForFunction(()=>deck.pages[0]?.scene?.objects.some(o=>o.kind==='image'&&o.media));
    const prepare=await page.evaluate(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json()),index=s.slides[0].elements.findIndex(e=>e.kind==='image'),element=s.slides[0].elements[index],q=new URLSearchParams(element.query||'');q.set('image-style','modern');q.set('left','20');q.set('top','10');q.set('width','100');q.set('height','30');q.set('stretch','1');await editorAction({action:'update-element',element:index,elementData:{...element,query:q.toString()}});return {index,id:element.id};});
    await page.waitForFunction(id=>document.querySelector('#stage [data-object-id="'+id+'"] img')?.complete,prepare.id);
    await page.setViewportSize({width:1500,height:1000});
    const selectedBefore=await page.evaluate(async({index})=>{const s=await fetch('/api/editor/state').then(r=>r.json());await editorAction({action:'select-elements',elementIndices:s.slides[0].elements.map((_,i)=>i),elementsData:s.slides[0].elements,element:index});return s.slides[0].elements.filter(e=>e.kind!=='image');},prepare);
    await page.getByRole('tab',{name:'Edit',exact:true}).click();
    for(const imageStyle of ['retro','modern']){
      await page.getByRole('combobox',{name:'Image style',exact:true}).selectOption(imageStyle);
      await waitForAsync(page,async value=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities?.properties['image.style']?.value===value;},imageStyle);
    }
    await page.getByRole('button',{name:'Inherit image image style',exact:true}).click();
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities?.properties['image.style']?.provenance==='inherited';});
    await page.getByRole('tab',{name:'Edit',exact:true}).click();
    const imageDialog=page.getByRole('group',{name:'Image adjustments',exact:true});
    for(const fit of ['contain','cover','stretch']){
      await imageDialog.getByRole('combobox',{name:'Framing',exact:true}).selectOption(fit);
      await waitForAsync(page,async fit=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities?.properties['image.fit']?.value===fit;},fit);
      await page.screenshot({path:path.join(output,'image-fit-'+fit+'.png')});
    }
    await imageDialog.getByRole('button',{name:'Inherit image framing',exact:true}).click();
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities?.properties['image.fit']?.value==='legacy';});
    await imageDialog.getByRole('combobox',{name:'Mask',exact:true}).selectOption('ellipse');
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities?.properties['image.mask']?.value==='ellipse';});
    await imageDialog.getByRole('button',{name:'Inherit image mask',exact:true}).click();
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities?.properties['image.mask']?.value==='none';});
    await imageDialog.getByRole('slider',{name:'Brightness',exact:true}).evaluate(input=>{input.value='1.5';input.dispatchEvent(new Event('change',{bubbles:true}));});
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities?.properties['image.brightness']?.value===1.5;});
    const afterImage=await page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
    assert.deepEqual(afterImage.slides[0].elements.filter(e=>e.kind!=='image'),selectedBefore,'Image subset must not rewrite text');
    await page.screenshot({path:path.join(output,'shared-image-adjustments.png')});
    await imageDialog.getByRole('button',{name:'Inherit image brightness',exact:true}).click();
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities?.properties['image.brightness']?.value===1;});
    const tint=imageDialog.getByRole('checkbox',{name:'Enable monochrome tint',exact:true});
    await tint.check();
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities?.properties['image.tint']?.value==='#ffffff';});
    await tint.uncheck();
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities?.properties['image.tint']?.value==='none';});
    await tint.check();
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities?.properties['image.tint']?.value==='#ffffff';});
    await imageDialog.getByRole('button',{name:'Inherit image tint',exact:true}).click();
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities?.properties['image.tint']?.value==='none';});
    assert.deepEqual(await page.evaluate(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[0].elements.filter(e=>e.kind!=='image');}),selectedBefore,'Tint subset must not rewrite text');
    await page.keyboard.press('Escape');
    await page.evaluate(async({index})=>{await editorAction({action:'select-element',element:index});},prepare);
    await page.evaluate(id=>{window.__editorMediaNodes={root:modernPageView.element,image:document.querySelector('#stage [data-object-id="'+id+'"] img'),owner:document.querySelector('#stage [data-object-id="'+id+'"]'),other:document.querySelector('#stage [data-object-id]:not([data-object-id="'+id+'"])')};},prepare.id);
    for(const [key,value]of [['modern-opacity','.7'],['gradient-start','#55aaff'],['gradient-end','#ff55aa'],['shadow','soft'],['shadow-color','#ffffff'],['brightness','1.2'],['sharpness','.8'],['modern-mask','ellipse'],['modern-crop','0.1,0.1,0.1,0.1']]){
      const result=await page.evaluate(async({key,value,id})=>{const s=await fetch('/api/editor/state').then(r=>r.json()),index=s.slides[0].elements.findIndex(e=>e.id===id),element=s.slides[0].elements[index],q=new URLSearchParams(element.query||''),view=modernPageView,before=view.renderStats();q.set(key,value);await editorAction({action:'update-element',element:index,elementData:{...element,query:q.toString()}});const nodes=window.__editorMediaNodes;await modernPageView.ready;
        if(nodes.root!==modernPageView.element||nodes.owner!==document.querySelector('#stage [data-object-id="'+id+'"]')||nodes.image!==document.querySelector('#stage [data-object-id="'+id+'"] img'))throw Error(key+' remounted projected image');if(modernPageView.renderStats().objectMounts!==before.objectMounts)throw Error(key+' mounted projected objects');const expected=['gradient-start','shadow-color'].includes(key)?[]:[id];if(JSON.stringify(modernPageView.renderStats().lastChangedObjectIDs)!==JSON.stringify(expected))throw Error(key+' did not update only its consumed stable image ID: '+JSON.stringify(modernPageView.renderStats().lastChangedObjectIDs));return {key,stats:modernPageView.renderStats()};
      },{key,value,...prepare});records.push({kind:'editor',...result});
    }
    await page.screenshot({path:output+'/editor-adjusted.png'});
    const promotionBefore=await page.evaluate(async id=>{await editorAction({action:'select-element',element:-1});await editorAction({action:'select-element',element:0,elementData:{id}});const s=await fetch('/api/editor/state').then(r=>r.json());return {index:s.slides[0].elements.findIndex(e=>e.id===id),slide:s.slides[0]};},prepare.id);
    await page.locator('.keynope-canvas-element[data-element="'+promotionBefore.index+'"]').dispatchEvent('contextmenu',{clientX:650,clientY:350});
    await page.screenshot({path:output+'/promotion-menu.png'});
    await page.getByRole('button',{name:'Set as background',exact:true}).click();
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return !!s.slides[0].backgroundMedia?.assetId;});
    const promotionAfter=await page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
    const bg=promotionAfter.slides[0].backgroundMedia;
    assert.equal(bg.opacity,.7);assert.equal(bg.opacitySet,true);assert.equal(new URLSearchParams(bg.query).get('modern-mask'),'ellipse');
    assert.equal(new URLSearchParams(promotionAfter.slides[0].elements.find(e=>e.id===prepare.id).query).get('object-hidden'),'1');
    await page.screenshot({path:output+'/promoted-background.png'});
    assert.equal(bg.sourceObjectId,prepare.id,'Promotion lost its local foreground owner');
    await page.evaluate(()=>editorAction({action:'select-element',element:-1}));
    await page.locator('.keynope-slide-item').first().dispatchEvent('contextmenu',{clientX:160,clientY:180});
    await page.getByRole('button',{name:'Move background back to image',exact:true}).click();
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[0].backgroundMediaSet&&!s.slides[0].backgroundMedia;});
    const restored=await page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
    const originalImage=promotionBefore.slide.elements.find(e=>e.id===prepare.id),restoredImage=restored.slides[0].elements.find(e=>e.id===prepare.id);
    for(const key of ['top','left','left_pct','width','height','object-offset-x','object-offset-y','object-rotation','modern-crop','modern-mask','brightness','sharpness','shadow','gradient-start'])assert.equal(new URLSearchParams(restoredImage.query).get(key),new URLSearchParams(originalImage.query).get(key),'Restore changed '+key);
    assert(!new URLSearchParams(restoredImage.query).has('object-hidden'),'Restored image remained hidden');
    await page.screenshot({path:output+'/restored-foreground.png'});
    await page.evaluate(()=>editorAction({action:'undo'}));
    assert.deepEqual((await page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()))).slides[0],promotionAfter.slides[0],'Restore undo must recover background and hidden source together');
    await page.evaluate(()=>editorAction({action:'undo'}));
    const promotionUndone=await page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
    assert.deepEqual(promotionUndone.slides[0],promotionBefore.slide,'One undo must restore source and background together');
    const importedBackground=await page.evaluate(async bg=>{
      const s=await fetch('/api/editor/state').then(r=>r.json()),slide=structuredClone(s.slides[0]);
      delete bg.sourceObjectId;slide.backgroundMedia=bg;slide.backgroundMediaSet=true;
      await editorAction({action:'update-slide',slideData:slide});await editorAction({action:'select-element',element:-1});await modernPageView.ready;
      return (await fetch('/api/editor/state').then(r=>r.json())).slides[0];
    },structuredClone(bg));
    const backgroundPixels=await page.locator('#stage .keynope-modern-scene').screenshot();
    await page.locator('.keynope-slide-item').first().dispatchEvent('contextmenu',{clientX:160,clientY:180});
    await page.getByRole('button',{name:'Convert background to image',exact:true}).click();
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return !s.slides[0].backgroundMedia&&s.slides[0].elements.length>2;});
    const materialized=await page.evaluate(async()=>{await modernPageView.ready;return (await fetch('/api/editor/state').then(r=>r.json())).slides[0];});
    const newImage=materialized.elements.find(e=>!importedBackground.elements.some(old=>old.id===e.id));assert(newImage&&newImage.kind==='image','Imported background has no editable foreground');
    for(const original of importedBackground.elements)assert.deepEqual(materialized.elements.find(e=>e.id===original.id),original,'Imported background conversion changed another object');
    const foregroundPixels=await page.locator('#stage .keynope-modern-scene').screenshot();
    const visual=await page.evaluate(async({before,after})=>{
      const decode=async data=>{const img=new Image();img.src='data:image/png;base64,'+data;await img.decode();const c=document.createElement('canvas');c.width=img.width;c.height=img.height;const x=c.getContext('2d');x.drawImage(img,0,0);return x.getImageData(0,0,c.width,c.height).data;};
      const [a,b]=await Promise.all([decode(before),decode(after)]);let max=0,total=0,changed=0,large=0;const samples=[];for(let i=0;i<a.length;i++){const d=Math.abs(a[i]-b[i]);max=Math.max(max,d);total+=d;if(d)changed++;if(d>5){large++;if(samples.length<12)samples.push({offset:i,before:a[i],after:b[i]});}}return {max,mean:total/a.length,changed,large,channels:a.length,samples};
    },{before:backgroundPixels.toString('base64'),after:foregroundPixels.toString('base64')});
    fs.writeFileSync(output+'/materialize-before.png',backgroundPixels);fs.writeFileSync(output+'/materialize-after.png',foregroundPixels);
    assert(visual.mean<.05&&visual.max<=5,'Imported background changed rendered frame: '+JSON.stringify(visual));records.push({kind:'background-materialization',visual});
    await page.screenshot({path:output+'/materialized-background.png'});
    await page.evaluate(()=>editorAction({action:'undo'}));assert.deepEqual((await page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()))).slides[0],importedBackground,'Imported background conversion is not one undo');
    const inheritedOwner=await page.evaluate(async bg=>{
      await editorAction({action:'toggle-master-mode'});await editorAction({action:'select-slide',slide:0});
      let s=await fetch('/api/editor/state').then(r=>r.json()),slide=structuredClone(s.slides[0]);delete bg.sourceObjectId;slide.backgroundMedia=bg;slide.backgroundMediaSet=true;await editorAction({action:'update-slide',slideData:slide});
      await editorAction({action:'toggle-master-mode'});await editorAction({action:'select-slide',slide:0});await editorAction({action:'set-layout',kind:'blank'});
      s=await fetch('/api/editor/state').then(r=>r.json());slide=structuredClone(s.slides[0]);slide.backgroundMedia=null;slide.backgroundMediaSet=false;await editorAction({action:'update-slide',slideData:slide});await editorAction({action:'select-element',element:-1});
      return (await fetch('/api/editor/state').then(r=>r.json())).masters.base;
    },structuredClone(bg));
    await page.locator('.keynope-slide-item').first().dispatchEvent('contextmenu',{clientX:160,clientY:180});
    await page.getByRole('button',{name:'Convert inherited background to image',exact:true}).click();
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[0].backgroundMediaSet&&!s.slides[0].backgroundMedia;});
    assert.deepEqual((await page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()))).masters.base,inheritedOwner,'Inherited conversion modified the Base master');
    const originalPromotion=await page.evaluate(async id=>{
      const s=await fetch('/api/editor/state').then(r=>r.json());await editorAction({action:'promote-image-background',slide:s.current,sceneMaster:false,sceneRevision:s.version,objectId:id});return (await fetch('/api/editor/state').then(r=>r.json())).slides[0];
    },prepare.id);
    await page.evaluate(()=>editorAction({action:'clone-slide'}));
    const cloneState=await page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json())),cloneOwner=cloneState.slides[cloneState.current].backgroundMedia.sourceObjectId;
    assert(cloneOwner&&cloneOwner!==prepare.id&&cloneState.slides[cloneState.current].elements.some(e=>e.id===cloneOwner),'Slide clone did not rebind background owner');
    await page.locator('.keynope-slide-item').nth(cloneState.current).dispatchEvent('contextmenu',{clientX:160,clientY:300});
    await page.getByRole('button',{name:'Move background back to image',exact:true}).click();
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return !s.slides[s.current].backgroundMedia;});
    assert.deepEqual((await page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()))).slides[0],originalPromotion,'Restoring copied background changed source slide');
    await page.evaluate(()=>editorAction({action:'undo'}));
    await page.evaluate(async owner=>{const s=await fetch('/api/editor/state').then(r=>r.json());await editorAction({action:'delete-objects',sceneRevision:s.version,objectIds:[owner]});await editorAction({action:'select-element',element:-1});},cloneOwner);
    const deletedOwner=await page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));assert(!deletedOwner.slides[deletedOwner.current].backgroundMedia.sourceObjectId,'Deleting hidden source left dangling background owner');
    await page.locator('.keynope-slide-item').nth(deletedOwner.current).dispatchEvent('contextmenu',{clientX:160,clientY:300});
    await page.getByRole('button',{name:'Convert background to image',exact:true}).click();
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return !s.slides[s.current].backgroundMedia;});
    assert.deepEqual(errors,[]);fs.writeFileSync(path.join(output,'report.json'),JSON.stringify(records,null,2));console.log('PASS: 33 retained/fresh media mutations and real WASM import/adjustment/promotion/foreground restoration commands, owner/image/unrelated identity, correction/effect/crop/mask/geometry, independent promotion/restore undo and bounded filter resources. '+output);
  }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);process.exitCode=1;server.close();});
