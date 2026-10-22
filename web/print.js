// Deterministic print output using the production scene painter. No workshop
// runtime, authoring archive, external resources or speaker notes live here.
(() => {
  'use strict';
  const payload=JSON.parse(document.getElementById('keynope-print-data').textContent);
  const manifest=payload.manifest,root=document.getElementById('keynope-print-pages'),status=document.getElementById('keynope-print-status');
  if(manifest.version!==1||!Array.isArray(manifest.pages)||!manifest.pages.length)throw Error('Invalid PDF manifest');
  window.keynopePDFManifest=manifest;
  const views=[],frames=[],pending=new Set();let generation=0,closed=false,prepared=null,isolation=null;
  const restoreMedia=()=>{if(!isolation)return;for(const [node,value]of isolation.visibility)node.style.visibility=value;isolation.scene.style.background=isolation.background;isolation=null;};
  const dispose=()=>{restoreMedia();prepared=null;generation++;window.keynopePrintReady=false;for(const cancel of Array.from(pending))cancel();for(const view of views.splice(0))view.dispose();for(const frame of frames.splice(0))frame.remove();root.replaceChildren();};
  const check=token=>{if(closed||generation!==token)throw new DOMException('Export cancelled','AbortError');};
  // Every readiness wait belongs to one export generation. A frame that has
  // loaded can still have stalled image/font promises; cancellation must settle
  // those waits too, without waiting for the timeout or an unrelated tab event.
  const ownedWait=(task,token,label,cleanup=()=>{})=>new Promise((resolve,reject)=>{
    check(token);let settled=false;
    const finish=(error,value)=>{if(settled)return;settled=true;clearTimeout(timeout);pending.delete(cancel);cleanup();error?reject(error):resolve(value);};
    const cancel=()=>finish(new DOMException('Export cancelled','AbortError'));
    const timeout=setTimeout(()=>finish(Error(label+' took too long to prepare')),30000);
    pending.add(cancel);
    Promise.resolve(task).then(value=>{try{check(token);finish(null,value);}catch(error){finish(error);}},error=>finish(error));
  });
  const pageHost=info=>{const host=document.createElement('section');host.className='keynope-print-page';host.dataset.page=info.index;host.style.width=info.width+'px';host.style.height=info.height+'px';host.style.page='keynope-'+info.index;root.append(host);return host;};
  // Native WKWebView PDF capture uses one PDF point per scene design pixel.
  // CSS print normally converts 96px to 72pt. Match native paper/font sizes
  // without changing layout, snapshots or the semantic scene: scale the print
  // artwork by 4/3 and declare the named page in points rather than CSS pixels.
  // Do not zoom an iframe ancestor: Chromium changes the embedded viewport's
  // print scale independently. Keep its viewport in scene pixels, transform
  // only the painted child and reserve the enlarged page extent separately.
  const css=document.createElement('style');css.textContent=manifest.pages.map(p=>`@page keynope-${p.index}{size:${p.width}pt ${p.height}pt;margin:0}`).join('\n')+'\n@media print{'+manifest.pages.map(p=>`.keynope-print-page[data-page="${p.index}"]{width:${p.width*4/3}px!important;height:${p.height*4/3}px!important}`).join('\n')+'\n.keynope-print-page>.keynope-modern-scene,.keynope-print-page>iframe{transform:scale(1.3333333333333333)!important;transform-origin:0 0}}';document.head.append(css);
  async function qualityPage(info,token){
    check(token);if(!info.page.scene)throw Error('This page requires exact-appearance output');
    await ownedWait(KeynopeScene.ensureFonts(),token,'Fonts');check(token);
    const host=pageHost(info),view=KeynopeScene.mount(host,info.page.scene);views.push(view);
    await ownedWait(view.snapshotAt(0),token,'Slide '+info.label);await ownedWait(document.fonts.ready,token,'Fonts');check(token);
    // Layout queries are deliberate: hidden native windows can throttle RAF.
    host.getBoundingClientRect();return host;
  }
  async function fallbackPage(info,token){
    check(token);const host=pageHost(info),frame=document.createElement('iframe');frames.push(frame);
    frame.title='Slide '+info.label;frame.setAttribute('sandbox','allow-scripts allow-same-origin');
    Object.assign(frame.style,{display:'block',border:'0',width:info.width+'px',height:info.height+'px'});
    const source={cols:manifest.cols,rows:manifest.rows,pages:[info.page],source:manifest.title};
    // Retain the legacy renderer for backgrounds/sample art, but give each
    // frame only its own public page rather than another full deck copy.
    const json=JSON.stringify(source).replace(/</g,'\\u003c');
    frame.srcdoc=payload.fallback.replace(/(<script id="keynope-data" type="application\/json">)[\s\S]*?(<\/script>)/,(_match,before,after)=>before+json+after);
    const loaded=new Promise((resolve,reject)=>{frame.onload=resolve;frame.onerror=()=>reject(Error('Could not load slide '+info.label));});
    host.append(frame);
    await ownedWait(loaded,token,'Slide '+info.label,()=>{frame.onload=null;frame.onerror=null;});check(token);
    await ownedWait(frame.contentWindow.keynopePrepareSnapshot(0),token,'Slide '+info.label);check(token);
    const style=frame.contentDocument.createElement('style');style.textContent='.keynope-chart-data-access,dialog{display:none!important}';frame.contentDocument.head.append(style);
    return host;
  }
  // Native quality capture can contain selectable text at stale coordinates
  // even after DOM/font readiness. Give PDFKit bounded, audience-safe checks
  // for the visible chart captions, not private authored data or diagnostics.
  function pdfTextChecks(info){
    const checks=[],scene=info.page.scene;
    for(const object of scene?.objects||[]){
      if(!object.chart||object.paint?.opacity===0)continue;
      const b=object.bounds,a=(object.rotation||0)*Math.PI/180,cos=Math.cos(a),sin=Math.sin(a);
      for(const label of object.chart.labels||[]){
        const text=(label.text?.runs||[]).map(run=>run.text||'').join('').trim(),r=label.bounds;
        if(!/[A-Za-z]/.test(text)||text.length>4096||label.paint?.color==='none')continue;
        const points=[[r.x,r.y],[r.x+r.width,r.y],[r.x,r.y+r.height],[r.x+r.width,r.y+r.height]].map(([x,y])=>{x-=b.width/2;y-=b.height/2;return [b.x+b.width/2+x*cos-y*sin,b.y+b.height/2+x*sin+y*cos];});
        const x=Math.min(...points.map(p=>p[0])),y=Math.min(...points.map(p=>p[1])),right=Math.max(...points.map(p=>p[0])),bottom=Math.max(...points.map(p=>p[1]));
        // Intentionally clipped/off-slide captions need not remain selectable.
        if(x<0||y<0||right>scene.width||bottom>scene.height)continue;
        checks.push({text,x,y,width:right-x,height:bottom-y});if(checks.length===4096)return checks;
      }
    }
    return checks;
  }
  window.keynopePrepareSnapshot=async index=>{
    if(!Number.isInteger(index)||index<0||index>=manifest.pages.length)throw Error('Invalid PDF page');
    dispose();const token=generation,info=manifest.pages[index];status.hidden=true;
    const host=await qualityPage(info,token);window.scrollTo(0,0);const b=host.getBoundingClientRect();
    prepared={info,host,token,media:null};
    return {x:b.x,y:b.y,width:info.width,height:info.height,page:index,pageCount:manifest.pages.length,pdfTextChecks:pdfTextChecks(info)};
  };
  // Native quality PDF readers interpolate heavily magnified raster sources
  // differently. Capture only those pictures at native display density, with
  // alpha recovered by the native exporter. Text, vectors, high-resolution
  // images and browser printing remain on their normal paths.
  window.keynopePDFMediaPlan=()=>{
    if(!prepared)throw Error('Prepare the PDF page first');check(prepared.token);
    const scene=prepared.host.querySelector('.keynope-modern-scene'),data=prepared.info.page.scene,media=new Map();
    const add=(object,node)=>{
      const m=object.media,b=object.bounds;
      if(!node||!m||!/^data:image\/(png|jpeg);base64,/.test(m.source||'')||m.frames?.length||object.paint?.opacity===0||getComputedStyle(node).visibility==='hidden')return;
      const cw=m.width*(1-(m.crop?.left||0)-(m.crop?.right||0)),ch=m.height*(1-(m.crop?.top||0)-(m.crop?.bottom||0));
      if(!(cw>0&&ch>0))return;
      const sx=b.width/cw,sy=b.height/ch,scale=m.fit==='contain'||m.fit==='stretch'?Math.min(sx,sy):Math.max(sx,sy);
      if(!Number.isFinite(scale)||scale<2)return;
      const box=node.getBoundingClientRect(),origin=scene.getBoundingClientRect(),paint=object.paint||{};
      const padding=2+(Number(paint.strokeWidth)||0)+3*(Number(paint.shadowBlur)||0)+Math.max(Math.abs(Number(paint.shadowX)||0),Math.abs(Number(paint.shadowY)||0))+Math.max(0,Number(paint.effects?.padding)||0);
      const x=Math.max(0,Math.floor(box.left-origin.left-padding)),y=Math.max(0,Math.floor(box.top-origin.top-padding));
      const right=Math.min(data.width,Math.ceil(box.right-origin.left+padding)),bottom=Math.min(data.height,Math.ceil(box.bottom-origin.top+padding));
      if(right<=x||bottom<=y)return;
      // Capture identities are private leases, not document IDs. A document
      // image named @background must never collide with the background lease.
      const lease='media-'+media.size;
      media.set(lease,{id:lease,node,x,y,width:right-x,height:bottom-y});
    };
    for(const object of data.objects||[])if(object.kind==='image')add(object,Array.from(scene.children).find(node=>node.dataset.objectId===object.id));
    if(data.backgroundMedia){const m=data.backgroundMedia;add({media:m,bounds:{x:0,y:0,width:data.width,height:data.height},paint:m.paint||{opacity:m.opacity}},scene.querySelector('.keynope-scene-background-media'));}
    prepared.media=media;
    return Array.from(media.values(),({node,...item})=>item);
  };
  window.keynopePDFMediaMatte=(id,matte)=>{
    if(!prepared?.media||!['#000000','#ffffff'].includes(matte))throw Error('Invalid PDF media capture');check(prepared.token);
    const item=prepared.media.get(id);if(!item)throw Error('Unknown PDF media target');
    restoreMedia();const scene=prepared.host.querySelector('.keynope-modern-scene');
    isolation={scene,background:scene.style.background,visibility:Array.from(scene.children,node=>[node,node.style.visibility])};
    for(const [node]of isolation.visibility)node.style.visibility=node===item.node?'visible':'hidden';
    scene.style.background=matte;scene.getBoundingClientRect();
    const origin=scene.getBoundingClientRect();return {x:origin.x+item.x,y:origin.y+item.y,width:item.width,height:item.height};
  };
  window.keynopePDFMediaInstall=async(id,source)=>{
    if(!prepared?.media||typeof source!=='string'||source.length>90*1024*1024||!source.startsWith('data:image/png;base64,'))throw Error('Invalid captured PDF media');
    const owner=prepared;check(owner.token);const item=owner.media.get(id);if(!item)throw Error('Unknown PDF media target');
    restoreMedia();const image=new Image();image.alt=item.node.getAttribute('aria-label')||'';image.src=source;
    Object.assign(image.style,{position:'absolute',left:item.x+'px',top:item.y+'px',width:item.width+'px',height:item.height+'px',maxWidth:'none',zIndex:item.node.style.zIndex});
    await ownedWait(image.decode(),owner.token,'PDF image');check(owner.token);
    if(prepared!==owner||!item.node.isConnected)throw new DOMException('Export cancelled','AbortError');
    // OS WebKit can omit a newly installed HTML <img> from createPDF even
    // when decode() and a native snapshot succeed. Use the same SVG image
    // primitive as the scene painter; only this sampled picture is replaced.
    const ns='http://www.w3.org/2000/svg',layer=document.createElementNS(ns,'svg'),picture=document.createElementNS(ns,'image');
    layer.dataset.keynopePdfMedia='true';layer.style.cssText=image.style.cssText;
    layer.setAttribute('width',item.width);layer.setAttribute('height',item.height);layer.setAttribute('viewBox',`0 0 ${item.width} ${item.height}`);
    if(image.alt){layer.setAttribute('role','img');layer.setAttribute('aria-label',image.alt);}else layer.setAttribute('aria-hidden','true');
    for(const [name,value]of Object.entries({href:source,x:0,y:0,width:item.width,height:item.height,preserveAspectRatio:'none'}))picture.setAttribute(name,value);
    layer.append(picture);item.node.replaceWith(layer);owner.media.delete(id);layer.getBoundingClientRect();
  };
  window.keynopePrepareBrowserPrint=async()=>{
    dispose();const token=generation;status.hidden=false;status.textContent='Preparing PDF…';
    try {
    for(const info of manifest.pages){check(token);status.textContent=`Preparing slide ${info.index+1} of ${manifest.pages.length}…`;if(info.quality)await qualityPage(info,token);else await fallbackPage(info,token);}
    check(token);status.replaceChildren();const message=document.createElement('p');message.textContent='Ready to print. Choose Save as PDF in your browser’s print dialog.';
    const print=document.createElement('button');print.textContent='Print / Save as PDF';print.onclick=()=>window.print();const close=document.createElement('button');close.textContent='Close';close.onclick=()=>window.close();status.append(message,print,close);
    document.title=manifest.title;window.keynopePrintReady=true;window.print();return manifest.pages.length;
    } catch(error) {
      // A superseded generation must not tear down the new generation's views.
      if(generation===token)dispose();
      throw error;
    }
  };
  window.keynopeCancelPrint=()=>{closed=true;dispose();};
  window.addEventListener('afterprint',()=>{status.hidden=false;});
  window.addEventListener('pagehide',()=>{closed=true;dispose();},{once:true});
  if(location.hash==='#print')window.keynopePrepareBrowserPrint().catch(error=>{status.hidden=false;status.textContent=error.name==='AbortError'?'Export cancelled.':'Could not prepare PDF: '+error.message;});
})();
