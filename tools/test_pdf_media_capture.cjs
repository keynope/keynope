// Private print-view protocol only: the document and ordinary browser printer
// must not be mutated by native PDF picture capture preparation.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=process.env.KEYNOPE_MEDIA_PDF_EVIDENCE||'/tmp/keynope-pdf-production-portrait';
(async()=>{
  for(const engine of ['chromium','webkit']){
    const browser=await playwright[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
    try{
      const context=await browser.newContext({viewport:{width:1080,height:1920}});
      await context.route(/https?:\/\/.*/,route=>route.abort());
      const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
      await page.goto('file://'+path.resolve(root,'quality.html'));
      const result=await page.evaluate(async()=>{
        const original=JSON.stringify(keynopePDFManifest);
        await keynopePrepareSnapshot(0);
        const plan=keynopePDFMediaPlan(),scene=document.querySelector('.keynope-modern-scene');
        const before=[...scene.children].map(node=>({node,visible:node.style.visibility,z:node.style.zIndex}));
        const ids=plan.map(item=>item.id),bounds=scene.getBoundingClientRect();
        const bounded=plan.every(b=>b.x>=0&&b.y>=0&&b.width>0&&b.height>0&&b.x+b.width<=bounds.width&&b.y+b.height<=bounds.height);
        keynopePDFMediaMatte(plan[0].id,'#000000');
        const isolated=[...scene.children].filter(node=>getComputedStyle(node).visibility==='visible').length;
        let rejected=0;
        for(const [id,matte]of [['absent','#ffffff'],[plan[0].id,'transparent']])try{keynopePDFMediaMatte(id,matte);}catch{rejected++;}
        try{await keynopePDFMediaInstall(plan[0].id,'https://invalid.test/a.png');}catch{rejected++;}
        const pixel='data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLttAAAAABJRU5ErkJggg==';
        await keynopePDFMediaInstall(plan[0].id,pixel);
        const after=[...scene.children],replaced=before.filter(({node})=>!node.isConnected);
        const order=after.every((node,i)=>node===before[i].node||(node.dataset.keynopePdfMedia==='true'&&node.style.zIndex===before[i].z));
        const restored=before.filter(({node})=>node.isConnected).every(({node,visible})=>node.style.visibility===visible);
        const baked=after.find(node=>node.dataset.keynopePdfMedia==='true');
        if(baked?.tagName!=='svg'||baked.querySelector('image')?.getAttribute('href')!==pixel||Number(baked.getAttribute('width'))!==plan[0].width||Number(baked.getAttribute('height'))!==plan[0].height)throw Error('Sampled PDF image lost its SVG source/frame');
        keynopePDFMediaMatte(plan[1].id,'#ffffff');
        await keynopePDFMediaInstall(plan[1].id,pixel);
        const retained=baked.isConnected&&getComputedStyle(baked).visibility==='visible'&&baked.style.opacity==='';
        await keynopePrepareSnapshot(4);const vectorPlan=keynopePDFMediaPlan().length;
        // High-resolution or anisotropically downsampled images keep the
        // original vector-PDF image resource, not a lower-density screenshot.
        const data=keynopePDFManifest.pages[0].page.scene,objects=data.objects.filter(o=>o.kind==='image');
        const saved=JSON.stringify(data);
        objects[0].media.width=100000;objects[0].media.height=100000;
        objects[1].media.fit='stretch';objects[1].media.width=100000;
        objects[2].media.source='data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciLz4=';
        await keynopePrepareSnapshot(0);const highResolutionPlan=keynopePDFMediaPlan().length;
        Object.assign(data,JSON.parse(saved));
        // Source IDs can legally resemble a background marker. Leases cannot.
        data.objects.find(o=>o.kind==='image').id='@background';
        data.backgroundMedia={...data.objects.find(o=>o.kind==='image').media,fit:'cover',opacity:.6};
        await keynopePrepareSnapshot(0);const collisionPlan=keynopePDFMediaPlan();
        Object.assign(data,JSON.parse(saved));delete data.backgroundMedia;
        await keynopePrepareSnapshot(0);const delayed=keynopePDFMediaPlan();
        const decode=Image.prototype.decode;Image.prototype.decode=function(){window.decodeEntered=true;return new Promise(()=>{});};
        const installing=keynopePDFMediaInstall(delayed[0].id,pixel).then(()=>false,e=>e.name==='AbortError');
        const next=keynopePrepareSnapshot(4);Image.prototype.decode=decode;await next;
        const cancelled=await installing;
        return {count:plan.length,ids,bounded,isolated,rejected,replaced:replaced.length,order,restored,retained,vectorPlan,highResolutionPlan,
          collision:collisionPlan.length===4&&new Set(collisionPlan.map(o=>o.id)).size===4,cancelled,
          oldImages:document.querySelectorAll('[data-keynope-pdf-media]').length,unchanged:JSON.stringify(keynopePDFManifest)===original};
      });
      assert.equal(result.count,3);assert.equal(new Set(result.ids).size,3);
      assert.equal(result.bounded,true);assert.equal(result.isolated,1);assert.equal(result.rejected,3);
      assert.equal(result.replaced,1);assert.equal(result.order,true);assert.equal(result.restored,true);assert.equal(result.retained,true);
      assert.equal(result.vectorPlan,0);assert.equal(result.highResolutionPlan,0);assert.equal(result.collision,true);
      assert.equal(result.cancelled,true);assert.equal(result.oldImages,0);assert.equal(result.unchanged,true);
      assert.deepEqual(errors,[]);
      console.log('PASS: '+engine+' PDF media leases, isolation, z-order, native-source preservation and stale-decode cancellation');
    }finally{await browser.close();}
  }
})().catch(error=>{console.error(error);process.exitCode=1;});
