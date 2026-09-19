// Modern scene prototype. This renders the typed, audience-safe projection;
// authoring commands and persisted document data stay outside the renderer.
globalThis.KeynopeScene = (() => {
  'use strict';
  const NS='http://www.w3.org/2000/svg';
  let serial=0;
  const pendingTextDrafts=new Set();
  async function commitPendingText(){for(const commit of [...pendingTextDrafts].reverse())await commit();}
  let fonts;
  let textEditors=0,pauseStarted=0,pausedDuration=0;
  const pausedTime=()=>pausedDuration+(textEditors?performance.now()-pauseStarted:0);
  const beginTextPause=()=>{if(textEditors++===0)pauseStarted=performance.now();};
  const endTextPause=()=>{if(textEditors>0&&--textEditors===0)pausedDuration+=performance.now()-pauseStarted;};
  const prefersReducedMotion=()=>typeof matchMedia==='function'&&matchMedia('(prefers-reduced-motion: reduce)').matches;
  const reportFontReadiness=(ready,error)=>{
    if(typeof globalThis.dispatchEvent!=='function'||typeof CustomEvent!=='function')return;
    globalThis.dispatchEvent(new CustomEvent('keynope-font-readiness',{detail:{source:'scene',ready,error:error?.message||''}}));
  };
  const dialogControls=dialog=>[...dialog.querySelectorAll('button,input,select,textarea,a[href],summary,[tabindex],[contenteditable="true"]')]
    .filter(node=>!node.disabled&&node.tabIndex>=0&&node.getClientRects().length&&getComputedStyle(node).visibility!=='hidden'&&!node.closest('[inert]'));
  const containDialogFocus=(event,dialog)=>{
    event.stopPropagation();
    if(event.defaultPrevented||event.key!=='Tab')return false;
    const controls=dialogControls(dialog);
    if(!controls.length){event.preventDefault();return true;}
    const first=controls[0],last=controls[controls.length-1];
    if(!dialog.contains(document.activeElement)){event.preventDefault();(event.shiftKey?last:first).focus();}
    else if(event.shiftKey&&document.activeElement===first){event.preventDefault();last.focus();}
    else if(!event.shiftKey&&document.activeElement===last){event.preventDefault();first.focus();}
    return true;
  };
  // Both media treatments use the same elapsed-time lookup. Binary search
  // bounds the work even after a long pause or with thousands of GIF frames.
  function animationFrameAt(ends,duration,loopCount,elapsed) {
    const loops=loopCount<0?1:loopCount===0?Infinity:loopCount+1;
    if(elapsed>=duration*loops)return ends.length-1;
    const time=elapsed%duration;
    let low=0,high=ends.length-1;
    while(low<high){const mid=(low+high)>>1;if(ends[mid]<=time)low=mid+1;else high=mid;}
    return low;
  }
  function ensureFonts(url='/api/editor/scene-fonts') {
    if(fonts)return fonts;
    let ownedStyle;
    const loaded=document.querySelector('[data-keynope-modern-fonts]')?Promise.resolve():fetch(url).then(async response=>{
      if(!response.ok)throw new Error('Bundled Modern fonts could not be loaded');
      const css=await response.text();
      ownedStyle=document.createElement('style');ownedStyle.dataset.keynopeModernFonts='';ownedStyle.textContent=css;document.head.append(ownedStyle);
    });
    const required=[
      ['KeynopeC64','normal','400'],
      ...['KeynopeModern','KeynopeModernMono'].flatMap(family=>[['normal','400'],['normal','700'],['italic','400'],['italic','700']].map(([style,weight])=>[family,style,weight]))
    ];
    fonts=loaded.then(async()=>{
      const results=await Promise.all(required.map(([family,style,weight])=>document.fonts.load(`${style} ${weight} 32px "${family}"`)));
      const missing=required.filter((_,index)=>!results[index]?.length).map(([family,style,weight])=>`${family} ${style} ${weight}`);
      if(missing.length)throw new Error('Bundled presentation font faces unavailable: '+missing.join(', '));
      reportFontReadiness(true);
      return results;
    }).catch(error=>{
      // A fetched stylesheet whose font data failed must not suppress the next
      // fetch. Never remove an embedded stylesheet owned by the document.
      ownedStyle?.remove();fonts=null;reportFontReadiness(false,error);throw error;
    });
    return fonts;
  }
  function svg(tag,attrs={}) {
    const node=document.createElementNS(NS,tag);
    for(const [key,value]of Object.entries(attrs))node.setAttribute(key,String(value));
    return node;
  }
  function shadow(p) {
    return p.shadowColor?`${p.shadowX||0}px ${p.shadowY||0}px ${p.shadowBlur||0}px ${p.shadowColor}`:'';
  }
  // Centred rectangles inscribed in each convex silhouette. Keep this shared
  // by painting, edit previews and Fit Text (which measures the painted scene).
  function shapeTextBox(object) {
    const {width,height}=object.bounds;
    let x=0,y=0,w=width,h=height;
    if(object.shape==='circle'){w=width/Math.SQRT2;h=height/Math.SQRT2;x=(width-w)/2;y=(height-h)/2;}
    else if(object.shape==='diamond'){w=width/2;h=height/2;x=width/4;y=height/4;}
    else if(object.shape==='triangle'){w=width/3;h=height/3;x=width/3;y=height/3;}
    const pad=Math.min(8,w/4,h/4);
    return {x:x+pad,y:y+pad,width:Math.max(1,w-2*pad),height:Math.max(1,h-2*pad)};
  }
  // Measure unwrapped authored lines: soft wrapping must not turn horizontal
  // typing into vertical growth. Always derive from the edit's original box,
  // so deleting a temporary newline can undo only the draft's extra growth.
  async function grownShapeTextBounds(parent,object,text) {
    if(object.kind!=='shape'||!hasModernText(object))throw Error('Modern shape text is required.');
    const host=document.createElement('div');host.setAttribute('aria-hidden','true');
    Object.assign(host.style,{position:'fixed',left:'-100000px',top:'0',visibility:'hidden',pointerEvents:'none'});parent.append(host);
    let surface;
    try{
      const runs=[...text.runs];
      // A final newline alone has no painted CSS line box; the edit caret does.
      if(runs.map(run=>run.text).join('').endsWith('\n'))runs.push({text:'\u200b'});
      surface=KeynopeTextLayout.create(host,{...text,runs,align:'start',bullet:false,width:20000});
      await surface.ready();
      Object.assign(surface.element.style,{width:'max-content',whiteSpace:'pre',overflowWrap:'normal'});
      const model=KeynopeTextLayout.normalize({...text,width:20000});
      const rect=surface.element.getBoundingClientRect();
      const required={width:rect.width,height:surface.element.scrollHeight+(model.paragraphBefore+model.paragraphAfter)*model.size};
      const bounds={...object.bounds};
      const factor=object.shape==='circle'?1/Math.SQRT2:object.shape==='diamond'?.5:object.shape==='triangle'?1/3:1;
      // Padding depends on both dimensions for very small shapes. Re-evaluate
      // after each expansion until both independent constraints are satisfied.
      for(let n=0;n<4;n++){
        const interior=shapeTextBox({...object,bounds});
        bounds.width+=Math.max(0,required.width-interior.width)/factor;
        bounds.height+=Math.max(0,required.height-interior.height)/factor;
      }
      return bounds;
    }finally{surface?.dispose();host.remove();}
  }
  function blockGlyphMask(ch) {
    return {' ':0,'▘':1,'▝':2,'▀':3,'▖':4,'▌':5,'▞':6,'▛':7,'▗':8,'▚':9,'▐':10,'▜':11,'▄':12,'▙':13,'▟':14,'█':15}[ch]??-1;
  }
  function drawBlockGlyph(ctx,col,row,mask,cw,ch) {
    if(mask<=0)return;
    // Object-local canvases translate by a fractional scene origin. Snap in
    // backing-store coordinates, not before that translation: otherwise each
    // adjacent cell gets two antialiased edges and a visible translucent seam.
    const t=ctx.getTransform(),aligned=t.b===0&&t.c===0&&t.a!==0&&t.d!==0;
    const snapX=value=>aligned?(Math.round(value*t.a+t.e)-t.e)/t.a:Math.round(value);
    const snapY=value=>aligned?(Math.round(value*t.d+t.f)-t.f)/t.d:Math.round(value);
    const xs=[snapX(col*cw),snapX((col+.5)*cw),snapX((col+1)*cw)],ys=[snapY(row*ch),snapY((row+.5)*ch),snapY((row+1)*ch)];
    if(mask===15){ctx.fillRect(xs[0],ys[0],Math.max(1,xs[2]-xs[0]),Math.max(1,ys[2]-ys[0]));return;}
    for(let y=0;y<2;y++)for(let x=0;x<2;x++)if(mask&(1<<(y*2+x)))ctx.fillRect(xs[x],ys[y],Math.max(1,xs[x+1]-xs[x]),Math.max(1,ys[y+1]-ys[y]));
  }
  function mount(parent,scene) {
    if(scene.version!==1||!(scene.width>0)||!(scene.height>0)||!Array.isArray(scene.objects))throw new Error('Unsupported slide scene');
    const root=document.createElement('div'),surfaces=[],textBoxes=[],media=[],readingNodes=[],retroReady=[],retroTracks=[],diagnostics=[...(scene.diagnostics||[])];
    const activeIDs=new Set(scene.objects.map(object=>object.id));
    const initialPausedTime=pausedTime();
    root.className='keynope-modern-scene';root.setAttribute('role','region');root.setAttribute('aria-label','Slide preview');
    Object.assign(root.style,{position:'relative',width:scene.width+'px',height:scene.height+'px',
      background:scene.background,overflow:'hidden',fontFamily:'sans-serif',isolation:'isolate'});
    parent.append(root);
    const position=(node,b)=>Object.assign(node.style,{position:'absolute',left:b.x+'px',top:b.y+'px',width:b.width+'px',height:b.height+'px'});
    function text(parent,data,b,paint,id,ownerID=id,isLabel=false) {
      const wrapper=document.createElement('div');position(wrapper,b);
      if(data.role==='heading'){wrapper.setAttribute('role','heading');wrapper.setAttribute('aria-level',String(Number.isInteger(data.level)&&data.level>=1&&data.level<=6?data.level:1));}
      else if(data.role==='bullet')wrapper.setAttribute('role','list');
      else if(data.role==='code')wrapper.setAttribute('role','code');
      wrapper.style.display='flex';wrapper.style.flexDirection='column';
      wrapper.style.justifyContent=data.vertical==='middle'?'center':data.vertical==='bottom'?'flex-end':'flex-start';
      if(data.role==='code'){
        const color=paint.color||'#ffffff',channels=color.match(/[0-9a-f]{2}/gi)||['ff','ff','ff'];
        const bright=channels.slice(0,3).reduce((n,v)=>n+parseInt(v,16),0)>384;
        wrapper.style.background=bright?'#202833':'#e4eaf2';wrapper.style.borderRadius='8px';
      }
      parent.append(wrapper);
      const paragraphs=data.paragraphs||[{runs:data.runs}];
      const local=[];
      textBoxes.push({wrapper,b,id,local,data,paint,ownerID,isLabel,...textSnapshot(data,paint)});
      for(const paragraph of paragraphs){
        const row=document.createElement('div');row.style.position='relative';row.style.flexShrink='0';
        row.className='keynope-scene-paragraph';row.dir=data.direction||'auto';
        row.style.paddingTop=((data.paragraphBefore||0)*data.size)+'px';
        row.style.paddingBottom=((data.paragraphAfter||0)*data.size)+'px';
        if(data.role==='bullet')row.setAttribute('role','listitem');
        const indent=paragraph.bullet?data.size*1.1:0;
        row.style.paddingInlineStart=indent+'px';wrapper.append(row);
        if(paragraph.bullet){
          const marker=document.createElement('span');marker.setAttribute('aria-hidden','true');
          marker.className='keynope-scene-bullet';
          Object.assign(marker.style,{position:'absolute',left:data.size*.25+'px',top:data.size*((data.paragraphBefore||0)+(data.lineHeight||1.25)/2-.09)+'px',width:data.size*.18+'px',height:data.size*.18+'px',borderRadius:'50%',background:paint.gradientStart||paint.color,pointerEvents:'none'});row.append(marker);
        }
        const marker=row.querySelector('.keynope-scene-bullet');
        if(marker){marker.style.left='';marker.style.insetInlineStart=data.size*.25+'px';}
        const surface=KeynopeTextLayout.create(row,{...data,runs:paragraph.runs,width:Math.max(1,b.width-indent),color:paint.color});
        // dir=auto excludes descendants with their own dir attribute. Resolve
        // from the shaped surface so the outer indent/marker matches its text.
        row.style.direction=getComputedStyle(surface.element).direction;
        // Scene structure supplies semantics; individual shaped fragments are
        // not separate documents and must not interrupt heading/list reading.
        surface.element.removeAttribute('role');
        surface.element.style.tabSize='4';
        surfaces.push({surface,wrapper,b,id});local.push(surface);
      }
      applyTextPaint({wrapper,local,data},paint);
      return local[0];
    }
    function applyTextPaint(box,paint){
      box.wrapper.style.opacity=box.data.seeThrough&&box.data.role!=='code'?'0.5':'';
      if(box.data.role==='code'){
        const channels=(paint.color||'#ffffff').match(/[0-9a-f]{2}/gi)||['ff','ff','ff'];
        const background=channels.slice(0,3).reduce((n,v)=>n+parseInt(v,16),0)>384?'#202833':'#e4eaf2';
        box.wrapper.style.background=background+(box.data.seeThrough?'80':'');
      }
      for(const marker of box.wrapper.querySelectorAll('.keynope-scene-bullet'))marker.style.background=paint.gradientStart||paint.color||'#ffffff';
      for(const surface of box.local){
        surface.setEmojiOutline(paint.stroke,paint.strokeWidth||1);
        surface.setEmojiGradient(paint.gradientStart,paint.gradientEnd,paint.gradientDirection);
        const angle=paint.gradientDirection==='vertical'?'180deg':paint.gradientDirection==='diagonal'?'135deg':'90deg';
        Object.assign(surface.element.style,{
          backgroundImage:paint.gradientStart?`linear-gradient(${angle},${paint.gradientStart},${paint.gradientEnd})`:'',
          backgroundClip:paint.gradientStart?'text':'',webkitBackgroundClip:paint.gradientStart?'text':'',
          color:paint.gradientStart?'transparent':paint.color||'#ffffff',
          webkitTextStroke:paint.stroke?`${paint.strokeWidth||1}px ${paint.stroke}`:'',
          paintOrder:paint.stroke?'stroke fill':'',filter:paint.shadowColor?`drop-shadow(${shadow(paint)})`:''
        });
      }
    }
    function fill(defs,p) {
      if(!p.gradientStart)return p.color;
      const id='keynope-scene-gradient-'+(++serial);
      const gradient=svg('linearGradient',{id,x1:0,y1:0,x2:p.gradientDirection==='vertical'?0:1,y2:p.gradientDirection==='horizontal'?0:1});
      gradient.append(svg('stop',{offset:'0%', 'stop-color':p.gradientStart}),svg('stop',{offset:'100%','stop-color':p.gradientEnd}));defs.append(gradient);
      return `url(#${id})`;
    }
    const shapeViews=new Map();
    const imageViews=new Map();
    function sampledSemantics(entry,object){
      const retro=object.retroText||object.retroLines,lines=object.retroText?[retro.line]:retro.lines;
      const label=retro.alt||(object.kind==='image'?'Image':lines.filter(l=>l.trueType).map(l=>l.trueType.text).join(' ')||object.text?.runs?.map(r=>r.text).join('')||((object.shape||'Retro')+' shape'));
      const key=JSON.stringify([!!retro.decorative,label]);
      if(entry.semanticKey===key)return;
      entry.semanticKey=key;
      if(retro.decorative){entry.node.setAttribute('aria-hidden','true');entry.node.removeAttribute('role');entry.node.removeAttribute('aria-label');}
      else {entry.node.removeAttribute('aria-hidden');entry.node.setAttribute('role','img');entry.node.setAttribute('aria-label',label);}
    }
    function imageSemantics(entry,media){
      const key=JSON.stringify([!!media.decorative,media.alt||'']);
      if(entry.semanticKey===key)return;
      entry.semanticKey=key;
      entry.image.alt=media.decorative?'':media.alt||'Slide image';
      if(media.decorative){
        entry.fallback.setAttribute('aria-hidden','true');
        entry.fallback.removeAttribute('role');entry.fallback.removeAttribute('aria-label');
      }else{
        entry.fallback.removeAttribute('aria-hidden');entry.fallback.setAttribute('role','img');
        entry.fallback.setAttribute('aria-label','Image unavailable'+(media.alt?': '+media.alt:''));
      }
    }
    const sampledViews=new Map();
    function frameImage(entry,object){
      const {image,frame,outlineFilter}=entry,box=object.bounds,crop=object.media.crop;
      Object.assign(image.style,{position:'',width:'100%',height:'100%',maxWidth:'',left:'',top:'',objectFit:'fill'});
      frame.style.clipPath=object.media.mask==='ellipse'?'ellipse(50% 50% at 50% 50%)':object.media.mask==='rounded'?'inset(0 round '+Math.min(box.width,box.height)*.12+'px)':'';
      if(crop){
        const w=object.media.width,h=object.media.height,cw=1-crop.left-crop.right,ch=1-crop.top-crop.bottom;
        if(w>0&&h>0&&cw>0&&ch>0){
          const scale=Math.max(box.width/(w*cw),box.height/(h*ch));
          Object.assign(image.style,{position:'absolute',width:w*scale+'px',height:h*scale+'px',maxWidth:'none',left:-(w*crop.left*scale+(w*cw*scale-box.width)/2)+'px',top:-(h*crop.top*scale+(h*ch*scale-box.height)/2)+'px'});
        }
      }
      if(outlineFilter){
        const radius=Math.max(.1,Math.min(64,Number(object.paint.strokeWidth)||1)),rx=radius/box.width,ry=radius/box.height;
        for(const [key,value]of Object.entries({x:-rx,y:-ry,width:1+2*rx,height:1+2*ry}))outlineFilter.setAttribute(key,value);
        outlineFilter.querySelector('feMorphology').setAttribute('radius',rx+' '+ry);
      }
    }
    function paintShape(drawing,object){
      const box=object.bounds,paint=object.paint||{color:'#ffffff'};
      drawing.setAttribute('width',box.width);drawing.setAttribute('height',box.height);
      drawing.setAttribute('viewBox',`0 0 ${box.width} ${box.height}`);
      drawing.style.overflow='visible';
      if(object.label)drawing.setAttribute('aria-hidden','true');
      else {drawing.setAttribute('role','img');drawing.setAttribute('aria-label',(object.shape||'rectangle')+' shape');}
      const defs=svg('defs');
      let shape;
      if(object.shape==='circle')shape=svg('ellipse',{cx:box.width/2,cy:box.height/2,rx:box.width/2,ry:box.height/2});
      else if(object.shape==='triangle')shape=svg('polygon',{points:`${box.width/2},0 ${box.width},${box.height} 0,${box.height}`});
      else if(object.shape==='diamond')shape=svg('polygon',{points:`${box.width/2},0 ${box.width},${box.height/2} ${box.width/2},${box.height} 0,${box.height/2}`});
      else shape=svg('rect',{width:box.width,height:box.height});
      shape.setAttribute('fill',fill(defs,paint));
      if(paint.stroke){shape.setAttribute('stroke',paint.stroke);shape.setAttribute('stroke-width',paint.strokeWidth||1);}
      drawing.style.filter=paint.shadowColor?`drop-shadow(${shadow(paint)})`:'';
      drawing.replaceChildren(defs,shape);
    }
    function mountObject(object){
      if(object.kind==='connector'&&!object.retroLines)return;
      const box=object.bounds,paint=object.paint||{color:'#ffffff'},node=document.createElement('div');
      node.dataset.objectId=object.id;position(node,box);
      // Keep authored paint order explicit so accessible DOM order can follow
      // the slide spatially without changing overlap, alpha or pointer hits.
      node.style.zIndex=String(readingNodes.length+1);
      readingNodes.push({node,object:objectSnapshot(object)});
      if(Number.isFinite(paint.opacity))node.style.opacity=String(Math.max(0,Math.min(1,paint.opacity)));
      if(object.rotation)node.style.transform=`rotate(${object.rotation}deg)`;
      root.append(node);
      if(object.kind==='shape'&&object.label){
        const labelHost=document.createElement('div');labelHost.className='keynope-scene-label';Object.assign(labelHost.style,{position:'absolute',inset:'0',zIndex:'2',pointerEvents:'none'});node.append(labelHost);
        if(!object.retroLabel&&Number.isFinite(object.labelPaint?.opacity))labelHost.style.opacity=String(Math.max(0,Math.min(1,object.labelPaint.opacity)));
        if(object.retroLabel){
          const retro=object.retroLabel,canvas=document.createElement('canvas'),padding=64;
          canvas.className='keynope-scene-retro-label';canvas.setAttribute('role','img');canvas.setAttribute('aria-label',retro.line.trueType.text||'Shape label');
          canvas.width=Math.ceil(box.width+padding*2);canvas.height=Math.ceil(box.height+padding*2);
          Object.assign(canvas.style,{position:'absolute',left:-padding+'px',top:-padding+'px'});labelHost.append(canvas);
          retroReady.push(KeynopeTrueType.readyFor(retro.line.trueType).then(()=>{if(disposed||!root.contains(node))return;const ctx=canvas.getContext('2d');ctx.translate(padding-box.x+(object.sampleOffset?.x||0),padding-box.y+(object.sampleOffset?.y||0));KeynopeTrueType.draw(ctx,retro.line,scene.width/retro.cols,scene.height/retro.rows,retro.cols,retro.rows);}));
        }else text(labelHost,object.label,shapeTextBox(object),object.labelPaint||paint,object.id+':label',object.id,true);
      }
      if(object.retroText||object.retroLines){
        const retro=object.retroText||object.retroLines,lines=object.retroText?[retro.line]:retro.lines,canvas=document.createElement('canvas'),padding=64;
        canvas.className='keynope-scene-retro-text';canvas.setAttribute('aria-hidden','true');
        canvas.width=Math.ceil(box.width+padding*2);canvas.height=Math.ceil(box.height+padding*2);
        Object.assign(canvas.style,{position:'absolute',left:-padding+'px',top:-padding+'px',pointerEvents:'none'});node.append(canvas);
        const sampledEntry={node,canvas,padding,scaleX:object.sampleScale?.x||1,scaleY:object.sampleScale?.y||1};
        sampledViews.set(object.id,sampledEntry);sampledSemantics(sampledEntry,object);
        retroReady.push((async()=>{
          const textLines=lines.filter(l=>l.trueType);
          if(textLines.length&&typeof KeynopeTrueType==='undefined')throw new Error('Retro text renderer is unavailable');
          await Promise.all(textLines.map(l=>KeynopeTrueType.readyFor(l.trueType)));
          if(disposed||!root.contains(node))return;
          const ctx=canvas.getContext('2d');ctx.translate(padding,padding);
          ctx.scale(object.sampleScale?.x||1,object.sampleScale?.y||1);
          ctx.translate(-box.x+(object.sampleOffset?.x||0),-box.y+(object.sampleOffset?.y||0));
          const cw=scene.width/retro.cols,ch=scene.height/retro.rows;
          ctx.textBaseline='top';
          const family='ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace';
          ctx.font=ch+'px '+family;
          const measured=Math.max(1,ctx.measureText('M').width),safeWidth=Math.max(1,cw*.98);
          if(measured>safeWidth)ctx.font=(ch*safeWidth/measured)+'px '+family;
          const paintLines=lines=>{
          ctx.save();ctx.setTransform(1,0,0,1,0,0);ctx.clearRect(0,0,canvas.width,canvas.height);ctx.restore();
          for(const line of lines){
			const opacity=line.role==='shape'?retro.shapeOpacity:line.role==='outline'?1:retro.contentOpacity;
			ctx.globalAlpha=Number.isFinite(opacity)?Math.max(0,Math.min(1,opacity)):1;
            if(line.trueType){KeynopeTrueType.draw(ctx,line,cw,ch,retro.cols,retro.rows);continue;}
            for(const part of line.parts||[]){
              let col=part.col;
              for(const glyph of part.text||''){
                if(part.background){const alpha=ctx.globalAlpha;if(Number.isFinite(retro.backdropOpacity))ctx.globalAlpha=Math.max(0,Math.min(1,retro.backdropOpacity));ctx.fillStyle=part.background;drawBlockGlyph(ctx,col,line.row,15,cw,ch);ctx.globalAlpha=alpha;}
                ctx.fillStyle=part.color||'#f3efe0';
                const mask=line.role==='outline'?(glyph===' '||glyph==='\u00a0'?0:15):blockGlyphMask(glyph);
                if(mask>=0)drawBlockGlyph(ctx,col,line.row,mask,cw,ch);
                else ctx.fillText(glyph,col*cw,line.row*ch);
                col++;
              }
            }
          }
          };
          paintLines(lines);
          if(retro.frames?.length){
            const map=new Map(),frames=[],ends=[];let duration=0;
            const key=l=>`${l.row}:${l.parts?.[0]?.col??l.col}:${l.element}:${l.role}`;
            for(const frame of retro.frames){
              if(frame.full){map.clear();for(const line of frame.lines||[])map.set(key(line),line);}
              else {for(const k of frame.clear||[])map.delete(k);for(const line of frame.update||[])map.set(key(line),line);}
              frames.push([...map.values()]);ends.push(duration+=Math.max(1,Number(frame.delayMs)||100));
            }
            retroTracks.push({id:object.id,frames,ends,duration,loopCount:Number(retro.loopCount)||0,current:0,paintLines});
          }
        })());
      }else if(object.kind==='text'&&object.text){text(node,object.text,{x:0,y:0,width:box.width,height:box.height},paint,object.id);}
      else if(object.kind==='shape'){
        const drawing=svg('svg');paintShape(drawing,object);node.append(drawing);
        shapeViews.set(object.id,drawing);
      }else if(object.kind==='image'&&object.media){
        const image=document.createElement('img');image.draggable=false;
        const fallback=document.createElement('div');fallback.className='keynope-scene-image-unavailable';fallback.textContent='[IMG]';
        Object.assign(fallback.style,{position:'absolute',inset:'0',display:'none',placeItems:'center',boxSizing:'border-box',border:'1px dashed #64748b',background:'#1e293b',color:'#f1f5f9',font:'20px system-ui'});
        const failed=()=>{
          if(disposed||!root.contains(node))return;
          image.style.visibility='hidden';fallback.style.display='grid';
          if(!diagnostics.some(d=>d.objectId===object.id&&d.code==='image-decode'))diagnostics.push({objectId:object.id,code:'image-decode',message:'The embedded image could not be decoded. Replace the image or restore its source.'});
        };
        image.addEventListener('error',failed);
        image.addEventListener('load',()=>{image.style.visibility='';fallback.style.display='none';});
        const frame=document.createElement('div');Object.assign(frame.style,{position:'absolute',inset:'0',overflow:'hidden'});
        const imageEntry={image,frame,fallback,outlineFilter:null};imageViews.set(object.id,imageEntry);
        imageSemantics(imageEntry,object.media);
        frameImage(imageEntry,object);
        const source=object.media.source||object.media.frames?.[0]?.source;
        // Scenes only contain embedded media. Never let a document initiate
        // an arbitrary remote request through this renderer.
        if(source&&/^data:image\/(png|jpeg|gif|webp);base64,/.test(source))image.src=source;
        const filters=[];
        const sharpness=Number(object.media.sharpness),sharpen=Number.isFinite(sharpness)&&sharpness>=.2&&sharpness<=2&&sharpness!==1;
        if(object.media.colourMatrices?.length||sharpen){
          const definitions=svg('svg',{width:0,height:0,'aria-hidden':'true'}),defs=svg('defs'),id='keynope-scene-colour-'+(++serial);
          definitions.style.position='absolute';
          const filter=svg('filter',{id,'color-interpolation-filters':'sRGB',x:0,y:0,width:'100%',height:'100%'});
          const matrices=object.media.colourMatrices||[],sharpnessAfter=Math.max(0,Math.min(matrices.length,Number(object.media.sharpnessAfter)||0));
          const addSharpness=()=>{
            const kernel=Array(9).fill((1-sharpness)/9);kernel[4]+=sharpness;
            // Match Retro's box-blur interpolation, duplicate edge samples and
            // preserve original alpha. Apply to each displayed animation frame.
            filter.append(svg('feConvolveMatrix',{order:3,kernelMatrix:kernel.join(' '),divisor:1,bias:0,edgeMode:'duplicate',preserveAlpha:'true'}));
          };
          for(let index=0;index<=matrices.length;index++){
            if(sharpen&&index===sharpnessAfter)addSharpness();
            const matrix=matrices[index];
            if(Array.isArray(matrix)&&matrix.length===20&&matrix.every(Number.isFinite))filter.append(svg('feColorMatrix',{type:'matrix',values:matrix.join(' ')}));
          }
          defs.append(filter);definitions.append(defs);node.append(definitions);filters.push(`url(#${id})`);
        }
        const compositing=[];
        if(paint.stroke||(paint.gradientStart&&paint.gradientEnd)){
          const id='keynope-image-paint-'+(++serial),definitions=svg('svg',{width:0,height:0}),defs=svg('defs');
          const radius=paint.stroke?Math.max(.1,Math.min(64,Number(paint.strokeWidth)||1)):0;
          const rx=radius/box.width,ry=radius/box.height;
          const filter=svg('filter',{id,'color-interpolation-filters':'sRGB',filterUnits:'objectBoundingBox',primitiveUnits:'objectBoundingBox',x:-rx,y:-ry,width:1+2*rx,height:1+2*ry});
          let content='SourceGraphic';
          if(paint.gradientStart&&paint.gradientEnd){
          // The gradient replaces colour but retains the post-crop alpha, as
          // Retro gradients replace sampled ink without altering its shape.
          const ramp=document.createElement('canvas');ramp.width=ramp.height=256;
          const ctx=ramp.getContext('2d'),vertical=paint.gradientDirection==='vertical',diagonal=paint.gradientDirection==='diagonal';
          const gradient=ctx.createLinearGradient(0,0,vertical?0:256,vertical||diagonal?256:0);
          gradient.addColorStop(0,paint.gradientStart);gradient.addColorStop(1,paint.gradientEnd);ctx.fillStyle=gradient;ctx.fillRect(0,0,256,256);
          filter.append(svg('feImage',{href:ramp.toDataURL(),x:0,y:0,width:1,height:1,preserveAspectRatio:'none',result:'gradient'}));
          filter.append(svg('feComposite',{in:'gradient',in2:'SourceAlpha',operator:'in',result:'coloured'}));content='coloured';
          }
          if(paint.stroke){
          imageEntry.outlineFilter=filter;
          // Outline the cropped/masked alpha, not the image's rectangular box.
          // This outer pass runs after source adjustments, and before shadow.
          filter.append(svg('feMorphology',{in:'SourceAlpha',operator:'dilate',radius:rx+' '+ry,result:'expanded'}));
          filter.append(svg('feComposite',{in:'expanded',in2:'SourceAlpha',operator:'arithmetic',k2:1,k3:-1,result:'edge'}));
          filter.append(svg('feFlood',{'flood-color':paint.stroke,result:'outline-colour'}));
          filter.append(svg('feComposite',{in:'outline-colour',in2:'edge',operator:'in',result:'outline'}));
          const merge=svg('feMerge');merge.append(svg('feMergeNode',{in:'outline'}),svg('feMergeNode',{in:content}));filter.append(merge);
          }
          defs.append(filter);definitions.append(defs);Object.assign(definitions.style,{position:'absolute',pointerEvents:'none'});node.append(definitions);
          compositing.push(`url(#${id})`);
        }
        if(paint.shadowColor)compositing.push(`drop-shadow(${shadow(paint)})`);
        node.style.filter=compositing.join(' ');
        image.style.filter=filters.join(' ');
        frame.append(image,fallback);node.append(frame);media.push({image,media:object.media,current:source,id:object.id,failed});
      }
    }
    for(const object of scene.objects)mountObject(object);
    // Routes are above shape bodies, as are terminating heads. Geometry comes
    // from the shared route solver rather than a separate modern routing pass.
    const connectorNodes=new Map();
    const connectors=svg('svg',{width:scene.width,height:scene.height,viewBox:`0 0 ${scene.width} ${scene.height}`});
    Object.assign(connectors.style,{position:'absolute',inset:'0',pointerEvents:'none',overflow:'visible'});
    function mountConnector(object){
      if(!object.points?.length)return;
      const paint=object.paint,defs=svg('defs');connectors.append(defs);
      const path=svg('polyline',{points:object.points.map(p=>`${p.x},${p.y}`).join(' '),fill:'none',stroke:fill(defs,paint),'stroke-width':paint.strokeWidth||2,'stroke-linejoin':'round'});
      path.setAttribute('role','img');path.setAttribute('aria-label','Connector'+(object.arrows==='both'?', arrows at both ends':object.arrows==='start'?', arrow at start':object.arrows==='end'?', arrow at end':''));
      const id='keynope-scene-arrow-'+(++serial),w=object.arrowWidth||16;
      const marker=svg('marker',{id,viewBox:'0 0 10 10',refX:10,refY:5,markerWidth:w,markerHeight:w,orient:'auto-start-reverse',markerUnits:'userSpaceOnUse'});
      marker.append(svg('path',{d:'M0 0 L10 5 L0 10 Z',fill:paint.color}));defs.append(marker);
      if(['end','both'].includes(object.arrows))path.setAttribute('marker-end',`url(#${id})`);
      if(['start','both'].includes(object.arrows))path.setAttribute('marker-start',`url(#${id})`);
      path.dataset.objectId=object.id;connectors.append(path);
      connectorNodes.set(object.id,{path,defs,points:path.getAttribute('points')});
      if(Number.isFinite(paint.opacity))path.style.opacity=String(Math.max(0,Math.min(1,paint.opacity)));
    }
    for(const object of scene.objects.filter(o=>o.kind==='connector'&&!o.retroLines))mountConnector(object);
    readingNodes.sort((a,b)=>a.object.bounds.y-b.object.bounds.y||a.object.bounds.x-b.object.bounds.x||String(a.object.id).localeCompare(String(b.object.id)));
    for(const {node}of readingNodes)root.append(node);
    // Connectors have always been a foreground overlay in this renderer.
    // Retain that policy while giving ordinary objects independent stacking.
    connectors.style.zIndex=String(readingNodes.length+1);
    for(const {node,object}of readingNodes)if(object.kind==='connector')node.style.zIndex=String(readingNodes.length+1);
    root.append(connectors);
    let disposed=false;
    let readinessRevision=0;
    const dirtyTextIDs=new Set();
    function checkOverflow(affected=null){
      if(disposed)return diagnostics;
      for(let i=diagnostics.length-1;i>=0;i--)if(diagnostics[i].code==='text-overflow'&&(!affected||affected.has(diagnostics[i].objectId)))diagnostics.splice(i,1);
      for(const {wrapper,b,id,local}of textBoxes)if((!affected||affected.has(id))&&(wrapper.scrollHeight>b.height+1||local.some(s=>s.element.scrollWidth>s.element.clientWidth+1))){
        diagnostics.push({objectId:id,code:'text-overflow',message:'Text exceeds its original box. Fit Text, Grow Box or layout editing is required.'});
      }
      return diagnostics;
    }
    let ready=Promise.all([...retroReady.splice(0),...surfaces.map(s=>s.surface.ready()),...media.map(item=>item.image.decode().catch(item.failed))]).then(()=>checkOverflow());
    const animationTrack=item=>{
      let duration=0;const ends=item.media.frames.map(frame=>(duration+=Math.max(1,Number(frame.delayMs)||100)));
      return {item,ends,duration};
    };
    const animationTracks=media.filter(item=>item.media.frames?.length).map(animationTrack);
    let lastTime=0,lastSnapshot=false;
    function setTime(ms,snapshot=false){
      lastTime=ms;lastSnapshot=snapshot;
      if(disposed||(!animationTracks.length&&!retroTracks.length))return;
      // Use the same elapsed-time clock as the presenter, excluding actual
      // editor pause duration. This also handles hidden tabs and sparse ticks.
      const elapsed=snapshot?Math.max(0,Number(ms)||0):prefersReducedMotion()?0:Math.max(0,ms-(pausedTime()-initialPausedTime));
      for(const track of retroTracks){
        const index=animationFrameAt(track.ends,track.duration,track.loopCount,elapsed);
        if(index!==track.current){track.current=index;track.paintLines(track.frames[index]);}
      }
      for(const {item,ends,duration} of animationTracks){
        const frames=item.media.frames,index=animationFrameAt(ends,duration,Number(item.media.loopCount)||0,elapsed);
        const source=frames[index].source;
        if(item.current!==source&&/^data:image\/png;base64,/.test(source)){item.image.src=source;item.current=source;}
      }
    }
    // Export callers own an isolated mount with no live playback clock. Await
    // the chosen image frame, not merely the initially mounted image/font set.
    // An explicit snapshot time is independent of accessibility/editor clocks.
    async function snapshotAt(ms=0){
      if(!Number.isFinite(ms)||ms<0)throw new Error('Snapshot time must be a finite, non-negative number');
      await ready;
      if(disposed)throw new Error('Cannot snapshot a disposed scene');
      setTime(ms,true);
      await Promise.all(media.map(item=>item.image.decode().catch(item.failed)));
      if(disposed)throw new Error('Cannot snapshot a disposed scene');
      return diagnostics;
    }
    function objectSnapshot(object){
      // Retain only the values consumed by geometry/paint reconciliation.
      // Never retain caller-owned bounds or copy embedded media payloads.
      return {id:object.id,kind:object.kind,shape:object.shape,bounds:{...object.bounds},rotation:object.rotation,
        paint:{opacity:object.paint?.opacity},labelPaint:{opacity:object.labelPaint?.opacity},
        paintKey:JSON.stringify(object.paint),media:{mask:object.media?.mask,cropKey:JSON.stringify(object.media?.crop)}};
    }
    function textContent(data){const {emojiFonts,...content}=data;return JSON.stringify(content);}
    // Direction and language are shaped-surface updates, not paragraph-tree
    // changes. Preserve owners and markers when switching these properties.
    function textStructure(data){const {runs,paragraphs,emojiFonts,direction,locale,...format}=data;return JSON.stringify({...format,...(paragraphs?{paragraphs:paragraphs.map(({runs,...paragraph})=>paragraph)}:{})});}
    function textSnapshot(data,paint){
      // Snapshot the last painted values, not caller-owned references. Besides
      // avoiding repeated serialization of the old paragraph on every move,
      // this keeps in-place input mutations observable.
      return {contentKey:textContent(data),structureKey:textStructure(data),paintKey:JSON.stringify(paint),fontSources:{...data.emojiFonts}};
    }
    // A conservative retained fast path: validate the complete update before
    // touching the DOM. Text and unlabelled vector shapes reconcile inside
    // their object; unsupported changes use the full renderer. Eligible static
    // raster coverage is compared in local coordinates; changed pixels invalidate.
    function retainedObjectProjection(object){
        // These describe editor commands/selection and link hit targets, not
        // painted content. Their consumers read the current scene separately.
        // Keep text/label payloads and accessibility/media metadata below.
        const {editable,textCapabilities,editRuns,group,link,...renderObject}=object;
        object=renderObject;
        if(object.kind==='connector'&&!object.retroLines){
          const {points,bounds,...rest}=object;
          return {...rest,hasRoute:!!points?.length};
        }
        if(object.kind==='connector')return object;
        const {rotation,sampleOffset,...rest}=object;
        if(object.retroText){
          const retro=object.retroText,line=retro.line;
          if(retro.cols>0&&retro.rows>0&&line.trueType){
            const {element,...coverage}=line;
            const x=(object.bounds.x-(sampleOffset?.x||0))*retro.cols/scene.width,y=(object.bounds.y-(sampleOffset?.y||0))*retro.rows/scene.height;
            const local=(value,origin)=>Math.round((value-origin)*1e8)/1e8;
            const query=new URLSearchParams(line.trueType.query||'');
            // These position the object in the legacy slide layout; the text
            // painter consumes row/col and local typography, not these keys.
            for(const key of ['top','bottom','left','right','left_pct','right_pct','row_delta','object-offset-x','object-offset-y'])query.delete(key);
            query.sort();
            rest.retroText={...retro,line:{...coverage,row:local(line.row,y),col:local(line.col,x),parts:line.parts?.map(part=>({...part,col:local(part.col,x)})),trueType:{...line.trueType,query:query.toString()}}};
          }
        }
        // Sampled artwork is positioned in legacy slide cells. Compare
        // its local coverage, not its world coordinates, so translation can
        // retain the existing canvas. Changed/clipped pixels still invalidate.
        const retro=object.retroLines;
        if(retro&&!object.retroLabel&&!object.label&&!retro.lines.some(line=>line.trueType)&&!(retro.frames||[]).some(frame=>frame.lines?.some(line=>line.trueType)||frame.update?.some(line=>line.trueType))&&retro.cols>0&&retro.rows>0){
          const x=(object.bounds.x-(sampleOffset?.x||0))*retro.cols/scene.width,y=(object.bounds.y-(sampleOffset?.y||0))*retro.rows/scene.height;
          const local=(value,origin)=>Math.round((value-origin)*1e8)/1e8;
          const normalizeLine=line=>{
            const {element,...coverage}=line;
            return {...coverage,row:local(line.row,y),...(Number.isFinite(line.col)?{col:local(line.col,x)}:{}),parts:line.parts?.map(part=>({...part,col:local(part.col,x)}))};
          };
          const normalizeClear=key=>{
            const match=/^(-?\d+):(-?\d+):(-?\d+):(.*)$/.exec(key);
            return match?[local(Number(match[1]),y),local(Number(match[2]),x),match[4]]:key;
          };
          rest.retroLines={...retro,lines:retro.lines.map(normalizeLine),...(retro.frames?{frames:retro.frames.map(frame=>({...frame,...(frame.lines?{lines:frame.lines.map(normalizeLine)}:{}),...(frame.update?{update:frame.update.map(normalizeLine)}:{}),...(frame.clear?{clear:frame.clear.map(normalizeClear)}:{})}))}:{})};
          delete rest.retroLines.alt;delete rest.retroLines.decorative;
          // A continuous resize can reuse identical raster coverage. Compare
          // its unscaled extent, then scale the retained canvas at composition
          // time rather than remounting it and restarting animated playback.
          return {...rest,sampleScale:undefined,bounds:{...object.bounds,x:0,y:0,width:Math.round(object.bounds.width/(object.sampleScale?.x||1)*1e8)/1e8,height:Math.round(object.bounds.height/(object.sampleScale?.y||1)*1e8)/1e8}};
        }
        if(object.kind==='image'&&object.media&&!object.retroText&&!object.retroLines){
          const {crop,mask,source,frames,alt,decorative,...media}=object.media;
          if(frames)media.frames=frames.map(({source,...frame})=>frame);
          return {...rest,media,bounds:{...object.bounds,x:0,y:0,width:0,height:0}};
        }
        if(object.kind==='shape'&&!object.retroLabel&&!object.retroText&&!object.retroLines){
          delete rest.paint;delete rest.shape;
          delete rest.labelPaint;delete rest.editRuns;
          if(object.label)rest.label=true;
          return {...rest,bounds:{...object.bounds,x:0,y:0,width:0,height:0}};
        }
        if(object.kind==='text'&&object.text&&!object.retroText&&!object.retroLines){
          rest.text=true;
          delete rest.paint;
          delete rest.editRuns;
          return {...rest,bounds:{...object.bounds,x:0,y:0,width:0,height:0}};
        }
        return {...rest,bounds:{...object.bounds,x:0,y:0}};
    }
    // Revision is command-concurrency metadata, not paint. Background colour
    // is a root property and need not invalidate every object on the slide.
    const sceneEnvelope=value=>{const {objects,background,revision,defaultStyle,diagnostics,...envelope}=value;return JSON.stringify(envelope);};
    let retainedBackground=scene.background;
    let sourceDiagnostics=new Set(scene.diagnostics||[]);
    const retainedEnvelope=sceneEnvelope(scene);
    const retainedObjects=new Map(scene.objects.map(object=>[object.id,JSON.stringify(retainedObjectProjection(object))]));
    const retainedSources=new Map(scene.objects.filter(object=>imageViews.has(object.id)).map(object=>[object.id,{source:object.media.source,frames:(object.media.frames||[]).map(frame=>frame.source)}]));
    function updateTransforms(next,replaceIncompatible=false){
      if(disposed||next.version!==1||!Array.isArray(next.objects)||sceneEnvelope(next)!==retainedEnvelope)return false;
      const byID=new Map(),added=[],replaced=new Set();
      // Compare one stable-ID projection at a time; avoid sorting and building
      // a second serialized copy of the complete scene on every interaction.
      // Do not cache by JS reference: callers may mutate an input object.
      for(const object of next.objects){
        if(!object||!object.id||byID.has(object.id))return false;
        const sources=retainedSources.get(object.id);
        const incompatible=retainedObjects.has(object.id)&&(JSON.stringify(retainedObjectProjection(object))!==retainedObjects.get(object.id)||sources&&(object.media?.source!==sources.source||(object.media?.frames||[]).length!==sources.frames.length||(object.media?.frames||[]).some((frame,index)=>frame.source!==sources.frames[index])));
        if(incompatible&&!replaceIncompatible)return false;
        if(!retainedObjects.has(object.id)||incompatible){
          if(!['text','shape','image','connector'].includes(object.kind))return false;
          try{
            for(const data of [!object.retroText&&!object.retroLines?object.text:null,!object.retroLabel?object.label:null].filter(Boolean)){
              for(const paragraph of data.paragraphs||[{runs:data.runs}])KeynopeTextLayout.normalize({...data,runs:paragraph.runs});
            }
          }catch(_error){return false;}
          added.push(object);
          if(incompatible)replaced.add(object.id);
        }
        byID.set(object.id,object);
      }
      if(next.objects.some(object=>!object.bounds||!['x','y','width','height'].every(key=>Number.isFinite(object.bounds[key]))||object.bounds.width<0||object.bounds.height<0))return false;
      for(const object of next.objects)if(object.kind==='image'&&object.media&&!object.retroLines&&!object.retroText){
        if(object.bounds.width<=0||object.bounds.height<=0)return false;
        const crop=object.media.crop;
        if(crop&&(!['left','right','top','bottom'].every(key=>Number.isFinite(crop[key])&&crop[key]>=0)||crop.left+crop.right>=1||crop.top+crop.bottom>=1))return false;
      }
      // Preflight every path before modifying anything. Style, markers and
      // route presence are included in the signature and cannot change here.
      const routes=[];
      for(const object of added)if(object.kind==='connector'&&!object.retroLines&&(!object.points?.length||object.points.some(p=>!Number.isFinite(p.x)||!Number.isFinite(p.y))))return false;
      for(const [id,entry]of connectorNodes){
        if(!byID.has(id)||replaced.has(id))continue;
        const points=byID.get(id)?.points;
        if(!Array.isArray(points)||!points.length||points.some(p=>!Number.isFinite(p.x)||!Number.isFinite(p.y)))return false;
        routes.push({entry,value:points.map(p=>`${p.x},${p.y}`).join(' ')});
      }
      const changedText=[];
      // Embedded colour fonts are immutable binary resources, not paragraph
      // structure. Compare their strings directly: stringifying them on every
      // pointer update copies large base64 payloads for all untouched objects.
      const sameFonts=(a={},b={})=>{
        const keys=Object.keys(a);
        return keys.length===Object.keys(b).length&&keys.every(key=>a[key]===b[key]);
      };
      for(const box of textBoxes){
        const object=byID.get(box.ownerID);
        if(!object||replaced.has(object.id)||object.retroText||object.retroLines||object.retroLabel)continue;
        const data=box.isLabel?object.label:object.text;
        if(!data)continue;
        const bounds=box.isLabel?shapeTextBox(object):{...box.b,width:object.bounds.width,height:object.bounds.height};
        const paragraphs=data.paragraphs||[{runs:data.runs}];
        const resized=['x','y','width','height'].some(key=>bounds[key]!==box.b[key]);
        const paint=(box.isLabel?object.labelPaint:null)||object.paint||{color:'#ffffff'},repaint=JSON.stringify(paint)!==box.paintKey;
        const contentChanged=!sameFonts(data.emojiFonts,box.fontSources)||textContent(data)!==box.contentKey;
        if(resized||repaint||contentChanged)changedText.push({box,data,paragraphs,paint,contentChanged,bounds,resized,rebuild:textStructure(data)!==box.structureKey});
      }
      // All surviving objects have passed preflight. Remove only deleted
      // owners and their resources; unrelated playback/layout stays intact.
      const removed=new Set([...retainedObjects.keys()].filter(id=>!byID.has(id)||replaced.has(id)));
      if(next.background!==retainedBackground){root.style.background=next.background||'';retainedBackground=next.background;}
      const removedDiagnostics=new Set([...removed,...textBoxes.filter(box=>removed.has(box.ownerID)).map(box=>box.id)]);
      for(const id of removed){activeIDs.delete(id);retainedObjects.delete(id);retainedSources.delete(id);shapeViews.delete(id);imageViews.delete(id);sampledViews.delete(id);connectorNodes.get(id)?.path.remove();connectorNodes.get(id)?.defs.remove();connectorNodes.delete(id);}
      for(let i=textBoxes.length-1;i>=0;i--){const box=textBoxes[i];if(!removed.has(box.ownerID))continue;for(const surface of box.local)surface.dispose();for(let j=surfaces.length-1;j>=0;j--)if(box.local.includes(surfaces[j].surface))surfaces.splice(j,1);dirtyTextIDs.delete(box.id);textBoxes.splice(i,1);}
      for(let i=readingNodes.length-1;i>=0;i--)if(removed.has(readingNodes[i].object.id)){readingNodes[i].node.remove();readingNodes.splice(i,1);}
      for(let i=retroTracks.length-1;i>=0;i--)if(removed.has(retroTracks[i].id))retroTracks.splice(i,1);
      for(let i=animationTracks.length-1;i>=0;i--)if(removed.has(animationTracks[i].item.id))animationTracks.splice(i,1);
      for(let i=media.length-1;i>=0;i--)if(removed.has(media[i].id))media.splice(i,1);
      for(let i=diagnostics.length-1;i>=0;i--)if(removedDiagnostics.has(diagnostics[i].objectId))diagnostics.splice(i,1);
      // Keep surviving runtime findings, but refresh backend diagnostics after
      // owner cleanup so warnings for replacement objects are not discarded.
      for(let i=diagnostics.length-1;i>=0;i--)if(sourceDiagnostics.has(diagnostics[i]))diagnostics.splice(i,1);
      sourceDiagnostics=new Set(next.diagnostics||[]);diagnostics.push(...sourceDiagnostics);
      const pendingText=[];
      for(const object of added){
        activeIDs.add(object.id);
        const firstRetro=retroReady.length,firstSurface=surfaces.length,firstMedia=media.length,firstBox=textBoxes.length;
        if(object.kind==='connector'&&!object.retroLines)mountConnector(object);else mountObject(object);
        retainedObjects.set(object.id,JSON.stringify(retainedObjectProjection(object)));
        if(imageViews.has(object.id))retainedSources.set(object.id,{source:object.media.source,frames:(object.media.frames||[]).map(frame=>frame.source)});
        for(const box of textBoxes.slice(firstBox))dirtyTextIDs.add(box.id);
        pendingText.push(...retroReady.splice(firstRetro),...surfaces.slice(firstSurface).map(s=>s.surface.ready()));
        for(const item of media.slice(firstMedia)){pendingText.push(item.image.decode().catch(item.failed));if(item.media.frames?.length)animationTracks.push(animationTrack(item));}
      }
      for(const {box,data,paragraphs,paint,contentChanged,bounds,resized,rebuild}of changedText){
        // Paint effects do not change the shaped text or wrapping box. Keep
        // existing overflow findings and avoid forced layout/font readiness
        // for colour/gradient/outline/shadow-only updates.
        if(contentChanged||resized||rebuild)dirtyTextIDs.add(box.id);
        if(rebuild){
          const parent=box.wrapper.parentNode;
          for(const surface of box.local)surface.dispose();
          box.wrapper.remove();
          for(let i=surfaces.length-1;i>=0;i--)if(box.local.includes(surfaces[i].surface))surfaces.splice(i,1);
          textBoxes.splice(textBoxes.indexOf(box),1);
          text(parent,data,bounds,paint,box.id,box.ownerID,box.isLabel);
          pendingText.push(...textBoxes[textBoxes.length-1].local.map(surface=>surface.ready()));
          continue;
        }
        if(resized){
          position(box.wrapper,bounds);box.b=bounds;
          for(const entry of surfaces)if(entry.id===box.id)entry.b=bounds;
          if(!contentChanged)paragraphs.forEach((paragraph,index)=>box.local[index].setWidth(Math.max(1,bounds.width-(paragraph.bullet?data.size*1.1:0))));
        }
        if(contentChanged)paragraphs.forEach((paragraph,index)=>{
          const indent=paragraph.bullet?data.size*1.1:0;
          box.local[index].element.parentElement.dir=data.direction||'auto';
          box.local[index].update({...data,runs:paragraph.runs,width:Math.max(1,box.b.width-indent),color:paint.color});
          box.local[index].element.parentElement.style.direction=getComputedStyle(box.local[index].element).direction;
        });
        box.data=data;
        box.paint=paint;
        Object.assign(box,textSnapshot(data,paint));
        applyTextPaint(box,paint);
        if(contentChanged||resized)pendingText.push(...box.local.map(surface=>surface.ready()));
      }
      if(pendingText.length||added.length){
        const revision=++readinessRevision,previous=ready;
        ready=Promise.all([previous,...pendingText]).then(()=>{
          if(revision!==readinessRevision)return diagnostics;
          setTime(lastTime,lastSnapshot);
          const result=checkOverflow(dirtyTextIDs);dirtyTextIDs.clear();return result;
        });
      }
      let readingOrderDirty=removed.size>0||added.length>0;
      const paintOrder=new Map();let layer=0;
      const connectorLayer=String(readingNodes.length+1);if(connectors.style.zIndex!==connectorLayer)connectors.style.zIndex=connectorLayer;
      for(const object of next.objects)if(!connectorNodes.has(object.id))paintOrder.set(object.id,++layer);
      for(const entry of readingNodes){
        const object=byID.get(entry.object.id);
        const snapshot=objectSnapshot(object);
        const z=String(object.kind==='connector'?readingNodes.length+1:paintOrder.get(object.id));
        if(entry.node.style.zIndex!==z)entry.node.style.zIndex=z;
        const image=imageViews.get(object.id);
        if(image)imageSemantics(image,object.media);
        const sampled=sampledViews.get(object.id);
        if(sampled){
          sampledSemantics(sampled,object);
          const x=(object.sampleScale?.x||1)/sampled.scaleX,y=(object.sampleScale?.y||1)/sampled.scaleY;
          const transform=x===1&&y===1?'':`scale(${x},${y})`;
          if(sampled.canvas.style.transform!==transform)Object.assign(sampled.canvas.style,{transform,transformOrigin:'0 0',left:-sampled.padding*x+'px',top:-sampled.padding*y+'px'});
        }
        if(image&&(object.bounds.width!==entry.object.bounds.width||object.bounds.height!==entry.object.bounds.height||object.media.mask!==entry.object.media.mask||snapshot.media.cropKey!==entry.object.media.cropKey))frameImage(image,object);
        if(object.label&&!object.retroLabel){
          const opacity=object.labelPaint?.opacity,oldOpacity=entry.object.labelPaint?.opacity;
          if(opacity!==oldOpacity)entry.node.querySelector('.keynope-scene-label').style.opacity=Number.isFinite(opacity)?String(Math.max(0,Math.min(1,opacity))):'';
        }
        const drawing=shapeViews.get(object.id);
        if(drawing&&(object.shape!==entry.object.shape||object.bounds.width!==entry.object.bounds.width||object.bounds.height!==entry.object.bounds.height||snapshot.paintKey!==entry.object.paintKey))paintShape(drawing,object);
        if(object.bounds.x!==entry.object.bounds.x){entry.node.style.left=object.bounds.x+'px';readingOrderDirty=true;}
        if(object.bounds.y!==entry.object.bounds.y){entry.node.style.top=object.bounds.y+'px';readingOrderDirty=true;}
        if(object.bounds.width!==entry.object.bounds.width)entry.node.style.width=object.bounds.width+'px';
        if(object.bounds.height!==entry.object.bounds.height)entry.node.style.height=object.bounds.height+'px';
        if((object.rotation||0)!==(entry.object.rotation||0))entry.node.style.transform=object.rotation?`rotate(${object.rotation}deg)`:'';
        if(object.paint?.opacity!==entry.object.paint?.opacity)entry.node.style.opacity=Number.isFinite(object.paint?.opacity)?String(Math.max(0,Math.min(1,object.paint.opacity))):'';
        entry.object=snapshot;
      }
      for(const {entry,value}of routes)if(entry.points!==value){entry.path.setAttribute('points',value);entry.points=value;}
      // SVG paths share a foreground overlay; their sibling order is their
      // relative stacking order. Definitions stay reusable throughout reorders.
      const paths=next.objects.filter(object=>connectorNodes.has(object.id)).map(object=>connectorNodes.get(object.id).path);
      const currentPaths=[...connectors.children].filter(node=>node.hasAttribute('data-object-id'));
      if(paths.some((node,index)=>node!==currentPaths[index]))for(const node of paths)connectors.append(node);
      if(readingOrderDirty){
        const compare=(a,b)=>a.object.bounds.y-b.object.bounds.y||a.object.bounds.x-b.object.bounds.x||String(a.object.id).localeCompare(String(b.object.id));
        // Most moves do not cross another object's reading position. Avoid
        // sorting and walking DOM siblings until the spatial order changes.
        const outOfOrder=readingNodes.some((entry,index)=>index>0&&compare(readingNodes[index-1],entry)>0);
        if(outOfOrder)readingNodes.sort(compare);
        if(outOfOrder||added.length||removed.size){
          let previous=null;
          for(const {node}of readingNodes){
            const expected=previous?previous.nextSibling:root.firstChild;
            if(expected!==node)root.insertBefore(node,expected);
            previous=node;
          }
        }
      }
      return true;
    }
    function resourceCounts(){return {objects:readingNodes.length+connectorNodes.size,textSurfaces:surfaces.length,media:media.length,animationTracks:animationTracks.length+retroTracks.length,pendingRasterQueue:retroReady.length};}
    function dispose(){
      if(disposed)return;disposed=true;
      for(const s of surfaces)s.surface.dispose();
      for(const list of [surfaces,textBoxes,media,readingNodes,retroReady,retroTracks,animationTracks])list.length=0;
      for(const map of [shapeViews,imageViews,sampledViews,connectorNodes,retainedObjects,retainedSources])map.clear();
      activeIDs.clear();dirtyTextIDs.clear();sourceDiagnostics.clear();
      root.replaceChildren();root.remove();
    }
    return {element:root,get ready(){return ready;},setTime,snapshotAt,updateTransforms,update:next=>updateTransforms(next,true),diagnostics,resourceCounts,dispose};
  }
  function editImage(object,commit,{renderDraft=null}={}) {
    const previous=document.activeElement;
    const dialog=document.createElement('dialog'),title=document.createElement('h2'),preview=document.createElement('div'),controls=document.createElement('div'),status=document.createElement('p'),actions=document.createElement('div');
    dialog.dataset.keynopeSceneDialog='crop';dialog.setAttribute('aria-label','Crop image');dialog.setAttribute('aria-modal','true');title.textContent='Crop image';
    Object.assign(dialog.style,{boxSizing:'border-box',width:'min(760px,calc(100vw - 16px))',maxWidth:'calc(100vw - 16px)',maxHeight:'90vh',overflow:'auto',padding:'20px',background:'#182028',color:'#fff',colorScheme:'dark',border:'1px solid #65717a',font:'14px system-ui'});
    Object.assign(preview.style,{position:'relative',width:'100%',height:'min(360px,40vh)',overflow:'hidden',background:'#0c1117'});
    Object.assign(controls.style,{display:'grid',gap:'12px',marginTop:'16px'});Object.assign(actions.style,{display:'flex',justifyContent:'flex-end',gap:'10px',marginTop:'16px'});status.setAttribute('role','alert');
    const blank={left:0,top:0,right:0,bottom:0},initial={...blank,...object.media.crop},initialMask=object.media.mask||'';let crop={...initial},mask=initialMask,view,closed=false,pending=false;
    let draftGeneration=0,draftTimer,draftController,previewOrigin={x:0,y:0},previewBounds=object.bounds;
    const animated=!!(object.media.frames?.length>1||object.retroLines?.frames?.length>1);
    let previewPlaying=!prefersReducedMotion(),previewTime=0,previewFrame=0,previousPreviewTick=performance.now();
    let playback;
    const motionPreference=typeof matchMedia==='function'?matchMedia('(prefers-reduced-motion: reduce)'):null;
    const resetPreviewClock=()=>{
      previousPreviewTick=performance.now();
      cancelAnimationFrame(previewFrame);previewFrame=0;
      if(animated&&!closed&&previewPlaying&&!document.hidden)previewFrame=requestAnimationFrame(tickPreview);
    };
    const motionChanged=event=>{
      // Enabling reduced motion stops existing playback too. Disabling the
      // preference must not override a deliberate Pause by the user.
      if(!event.matches)return;
      previewPlaying=false;if(playback)playback.textContent='Play preview';resetPreviewClock();
    };
    if(animated)document.addEventListener('visibilitychange',resetPreviewClock);
    if(animated)motionPreference?.addEventListener('change',motionChanged);
    const tickPreview=now=>{
      previewFrame=0;
      if(closed||!previewPlaying||document.hidden)return;
      const delta=Math.max(0,now-previousPreviewTick);previousPreviewTick=now;
      // This preview owns its clock. The surrounding presentation stays
      // paused, and dragging freezes the preview rather than changing frames
      // underneath the crop handles. Resume without a hidden-tab time jump.
      if(previewPlaying&&!document.hidden&&!drag&&!rangeDrag&&!pending)previewTime+=delta;
      view?.setTime(previewTime,true);
      previewFrame=requestAnimationFrame(tickPreview);
    };
    const inputs={},outputs={},numbers={},handles=[];let drag=null,rangeDrag=false,restoring=false,historyIndex=0;
    const history=[{crop:{...initial},mask:initialMask,preset:''}];
    const sourceStage=document.createElement('div'),sourceFrame=document.createElement('div'),sourceImage=document.createElement('img'),selection=document.createElement('div');
    Object.assign(sourceStage.style,{position:'relative',height:'260px',overflow:'hidden',background:'#0c1117'});Object.assign(sourceFrame.style,{position:'absolute',touchAction:'none'});
    sourceFrame.dataset.cropSource='';sourceImage.alt='Original image';sourceImage.draggable=false;Object.assign(sourceImage.style,{width:'100%',height:'100%',pointerEvents:'none',display:'block'});
    const original=object.media.source||object.media.frames?.[0]?.source;if(original&&/^data:image\/(png|jpeg|gif|webp);base64,/.test(original))sourceImage.src=original;
    selection.tabIndex=0;selection.setAttribute('role','button');selection.setAttribute('aria-label','Move crop selection');Object.assign(selection.style,{position:'absolute',boxSizing:'border-box',border:'2px solid #ffdf66',boxShadow:'0 0 0 1000px #0008',cursor:'move',touchAction:'none'});sourceFrame.append(sourceImage,selection);sourceStage.append(sourceFrame);
    for(const corner of ['top-left','top-right','bottom-left','bottom-right']){const handle=document.createElement('button');handle.type='button';handle.dataset.cropCorner=corner;handle.setAttribute('aria-label','Crop '+corner+' corner');Object.assign(handle.style,{position:'absolute',width:'14px',height:'14px',padding:0,border:'2px solid #182028',background:'#ffdf66',borderRadius:'2px',touchAction:'none',cursor:corner==='top-left'||corner==='bottom-right'?'nwse-resize':'nesw-resize',[corner.includes('left')?'left':'right']:'-8px',[corner.includes('top')?'top':'bottom']:'-8px'});selection.append(handle);handles.push(handle);}
    const sync=()=>{Object.assign(selection.style,{left:crop.left*100+'%',top:crop.top*100+'%',width:(1-crop.left-crop.right)*100+'%',height:(1-crop.top-crop.bottom)*100+'%'});for(const side of Object.keys(inputs)){inputs[side].value=crop[side]*100;numbers[side].value=Number((crop[side]*100).toFixed(6));outputs[side].textContent='%';}};
    const fit=()=>{const sw=object.media.width||object.bounds.width,sh=object.media.height||object.bounds.height,sourceScale=Math.min((sourceStage.clientWidth-28)/sw,(sourceStage.clientHeight-28)/sh);Object.assign(sourceFrame.style,{width:sw*sourceScale+'px',height:sh*sourceScale+'px',left:(sourceStage.clientWidth-sw*sourceScale)/2+'px',top:(sourceStage.clientHeight-sh*sourceScale)/2+'px'});sync();if(!view)return;const scale=Math.min(preview.clientWidth/previewBounds.width,preview.clientHeight/previewBounds.height);Object.assign(view.element.style,{transformOrigin:'0 0',transform:`scale(${scale})`,left:((preview.clientWidth-previewBounds.width*scale)/2-previewOrigin.x*scale)+'px',top:((preview.clientHeight-previewBounds.height*scale)/2-previewOrigin.y*scale)+'px'});};
    const updatePreview=scene=>{
      if(!view?.update(scene)){view?.dispose();view=mount(preview,scene);}
      const current=view;
      fit();current.setTime(previewTime,true);
      // Replacement Retro artwork can finish decoding asynchronously. Restore
      // the preview's clock, including its frozen frame, once it is ready.
      return current.ready.then(()=>{if(!closed&&view===current)current.setTime(previewTime,true);});
    };
    // Crop against the retained source in both styles, never a stale sampled
    // derivative. The committed scene applies the selected Retro treatment.
    const draw=()=>{
      if(object.retroLines&&renderDraft){
        const generation=++draftGeneration,currentCrop={...crop},currentMask=mask;
        clearTimeout(draftTimer);draftController?.abort();status.textContent='Updating Retro preview…';fit();record();
        draftTimer=setTimeout(async()=>{
          const controller=new AbortController();draftController=controller;
          try{
            const draft=await renderDraft(currentCrop,currentMask,controller.signal);
            if(closed||generation!==draftGeneration)return;
            const image=draft.objects.find(o=>o.id===object.id);if(!image)throw Error('Image preview is unavailable');
            previewOrigin=image.bounds;previewBounds=image.bounds;
            // Cropping edits source-space coordinates in either treatment.
            // Keep the slide rotation on the authored object, not this local
            // preview; otherwise a rotated Retro result is clipped by a pane
            // fitted to its unrotated bounds. Do not mutate the draft owner.
            const previewScene={...draft,background:'#0c1117',objects:[{...image,rotation:0}]};
            await updatePreview(previewScene);
            if(!closed&&generation===draftGeneration)status.textContent='';
          }catch(error){if(error?.name!=='AbortError'&&!closed&&generation===draftGeneration)status.textContent=error.message||'Could not render image preview';}
          finally{if(draftController===controller)draftController=null;}
        },50);
        return;
      }
      updatePreview({version:1,width:object.bounds.width,height:object.bounds.height,background:'#0c1117',objects:[{...object,retroLines:undefined,retroText:undefined,rotation:0,bounds:{x:0,y:0,width:object.bounds.width,height:object.bounds.height},media:{...object.media,mask,crop:Object.values(crop).some(Boolean)?crop:null}}]});record();
    };
    const maskLabel=document.createElement('label'),maskSelect=document.createElement('select');maskLabel.textContent='Mask ';maskSelect.setAttribute('aria-label','Image mask');Object.assign(maskSelect.style,{font:'inherit',padding:'6px'});
    for(const [value,title]of [['','Rectangle'],['rounded','Rounded rectangle'],['ellipse','Ellipse']]){const option=document.createElement('option');option.value=value;option.textContent=title;maskSelect.append(option);}maskSelect.value=mask;maskSelect.onchange=()=>{mask=maskSelect.value;draw();};maskLabel.append(maskSelect);controls.append(maskLabel);
    const clamp=(v,min,max)=>Math.min(max,Math.max(min,v));
    const presetLabel=document.createElement('label'),preset=document.createElement('select');presetLabel.textContent='Crop preset ';preset.setAttribute('aria-label','Crop preset');Object.assign(preset.style,{font:'inherit',padding:'6px'});
    for(const [value,title]of [['','Custom'],['source','Original proportions'],['frame','Match image box'],['1','Square · 1:1'],[String(4/3),'Landscape · 4:3'],[String(16/9),'Widescreen · 16:9'],[String(3/4),'Portrait · 3:4'],[String(9/16),'Portrait · 9:16']]){const option=document.createElement('option');option.value=value;option.textContent=title;preset.append(option);}
    preset.onchange=()=>{
      if(!preset.value)return;
      const sourceRatio=(object.media.width||object.bounds.width)/(object.media.height||object.bounds.height);
      const ratio=preset.value==='source'?sourceRatio:preset.value==='frame'?object.bounds.width/object.bounds.height:Number(preset.value);
      const relative=ratio/sourceRatio,width=Math.min(1,relative),height=Math.min(1,1/relative);
      if(width<.01||height<.01){preset.value='';status.textContent='This image is too narrow for that crop preset. Choose a less extreme ratio.';return;}status.textContent='';
      const cx=(crop.left+1-crop.right)/2,cy=(crop.top+1-crop.bottom)/2;
      const left=clamp(cx-width/2,0,1-width),top=clamp(cy-height/2,0,1-height);
      crop={left,top,right:Math.max(0,1-left-width),bottom:Math.max(0,1-top-height)};draw();
    };presetLabel.append(preset);controls.prepend(presetLabel);
    const move=(base,corner,dx,dy)=>{
      if(!corner){const left=clamp(base.left+dx,0,base.left+base.right),top=clamp(base.top+dy,0,base.top+base.bottom);crop={left,top,right:base.left+base.right-left,bottom:base.top+base.bottom-top};}
      else{preset.value='';crop={...base};const x=corner.includes('left')?'left':'right',y=corner.includes('top')?'top':'bottom';crop[x]=clamp(base[x]+(x==='left'?dx:-dx),0,.99-base[x==='left'?'right':'left']);crop[y]=clamp(base[y]+(y==='top'?dy:-dy),0,.99-base[y==='top'?'bottom':'top']);}
      draw();
    };
    const finishGesture=(gesture,cancelled)=>{
      if(!cancelled){record();return;}
      crop={...gesture.base};preset.value=gesture.preset;
      restoring=true;draw();restoring=false;
    };
    sourceFrame.addEventListener('pointerdown',event=>{if(pending||drag||rangeDrag||event.button!==0||!selection.contains(event.target))return;event.preventDefault();drag={id:event.pointerId,x:event.clientX,y:event.clientY,base:{...crop},preset:preset.value,corner:event.target.dataset.cropCorner||'',rect:sourceFrame.getBoundingClientRect()};sourceFrame.setPointerCapture(event.pointerId);});
    sourceFrame.addEventListener('pointermove',event=>{if(!drag||event.pointerId!==drag.id||pending)return;move(drag.base,drag.corner,(event.clientX-drag.x)/drag.rect.width,(event.clientY-drag.y)/drag.rect.height);});
    for(const type of ['pointerup','pointercancel','lostpointercapture'])sourceFrame.addEventListener(type,event=>{if(drag&&event.pointerId===drag.id){const gesture=drag;drag=null;finishGesture(gesture,type!=='pointerup');}});
    selection.addEventListener('keydown',event=>{if(pending||!['ArrowLeft','ArrowRight','ArrowUp','ArrowDown'].includes(event.key))return;event.preventDefault();event.stopPropagation();const step=event.shiftKey ? .1 : .01;move(crop,event.target.dataset.cropCorner||'',event.key==='ArrowLeft'?-step:event.key==='ArrowRight'?step:0,event.key==='ArrowUp'?-step:event.key==='ArrowDown'?step:0);});
    for(const [side,opposite]of [['left','right'],['top','bottom'],['right','left'],['bottom','top']]){
      const label=document.createElement('div'),caption=document.createElement('span'),input=document.createElement('input'),number=document.createElement('input'),output=document.createElement('span');caption.textContent=side[0].toUpperCase()+side.slice(1);input.type='range';input.min=0;input.max=99;input.step=1;input.value=crop[side]*100;input.setAttribute('aria-label','Crop '+side);Object.assign(label.style,{display:'grid',gridTemplateColumns:'64px minmax(0,1fr) 76px 12px',alignItems:'center',gap:'8px'});number.type='number';number.min=0;number.max=99;number.step='any';number.setAttribute('aria-label','Crop '+side+' percent');Object.assign(number.style,{width:'100%',minWidth:0,boxSizing:'border-box',font:'inherit',padding:'5px'});output.textContent='%';label.append(caption,input,number,output);controls.append(label);inputs[side]=input;numbers[side]=number;outputs[side]=output;
      number.onchange=()=>{if(pending||drag||rangeDrag||number.value===''||!number.checkValidity())return;const value=Number(number.value)/100;if(!Number.isFinite(value))return;preset.value='';crop={...crop,[side]:Math.min(value,Math.max(0,.99-crop[opposite]))};draw();};
      input.oninput=()=>{preset.value='';crop={...crop,[side]:Math.min(Number(input.value)/100,.99-crop[opposite])};input.value=crop[side]*100;output.textContent=Math.round(crop[side]*100)+'%';draw();};
      input.addEventListener('pointerdown',event=>{if(pending||drag||rangeDrag||event.button!==0)return;rangeDrag={id:event.pointerId,base:{...crop},preset:preset.value};input.setPointerCapture(event.pointerId);});
      for(const type of ['pointerup','pointercancel','lostpointercapture'])input.addEventListener(type,event=>{if(rangeDrag&&event.pointerId===rangeDrag.id){const gesture=rangeDrag;rangeDrag=false;finishGesture(gesture,type!=='pointerup');}});
    }
    const button=(text,callback)=>{const b=document.createElement('button');b.type='button';b.textContent=text;b.setAttribute('aria-label',text);b.onclick=callback;Object.assign(b.style,{font:'inherit',padding:'8px 14px',background:'#29323b',color:'#fff',border:'1px solid #65717a',borderRadius:'6px'});return b;};
    const updateHistoryButtons=()=>{undo.disabled=pending||historyIndex===0;redo.disabled=pending||historyIndex===history.length-1;};
    const record=()=>{
      if(!restoring&&!drag&&!rangeDrag){const value={crop:{...crop},mask,preset:preset.value};if(JSON.stringify(value)!==JSON.stringify(history[historyIndex])){history.splice(historyIndex+1);history.push(value);if(history.length>101)history.shift();historyIndex=history.length-1;}}
      updateHistoryButtons();
    };
    const restore=delta=>{if(pending||drag||rangeDrag)return;const next=historyIndex+delta;if(next<0||next>=history.length)return;historyIndex=next;const value=history[next];crop={...value.crop};mask=value.mask;maskSelect.value=mask;preset.value=value.preset;status.textContent='';restoring=true;draw();restoring=false;};
    const undo=button('Undo crop',()=>restore(-1)),redo=button('Redo crop',()=>restore(1));
    const close=()=>{if(pending||closed)return;closed=true;++draftGeneration;clearTimeout(draftTimer);draftController?.abort();draftController=null;cancelAnimationFrame(previewFrame);document.removeEventListener('visibilitychange',resetPreviewClock);motionPreference?.removeEventListener('change',motionChanged);observer.disconnect();view?.dispose();endTextPause();dialog.close();dialog.remove();if(previous?.isConnected)previous.focus({preventScroll:true});};
    const reset=button('Reset crop',()=>{preset.value='source';crop={...blank};for(const side of Object.keys(inputs)){inputs[side].value=0;outputs[side].textContent='0%';}draw();}),cancel=button('Cancel',close),apply=button('Apply crop',async()=>{
      if(pending)return;
      for(const number of Object.values(numbers)){number.required=true;if(!number.reportValidity())return;}
      if(JSON.stringify(crop)===JSON.stringify(initial)&&mask===initialMask){close();return;}
      pending=true;updateHistoryButtons();for(const input of [...Object.values(inputs),...Object.values(numbers),preset,maskSelect,reset,cancel,apply])input.disabled=true;status.textContent='';
      try{await commit(crop,mask);pending=false;close();}catch(error){status.textContent=error.message||'Could not apply crop';}finally{pending=false;updateHistoryButtons();for(const input of [...Object.values(inputs),...Object.values(numbers),preset,maskSelect,reset,cancel,apply])input.disabled=false;}
    });apply.style.background='#245c91';
    actions.style.flexWrap='wrap';actions.append(undo,redo,reset,cancel,apply);const hint=document.createElement('p');hint.textContent='Drag corners to crop; drag inside to move. Arrow keys adjust the focused handle (Shift for larger steps). Cropping is non-destructive and retained when changing style.'+(object.retroLines&&!renderDraft?' This preview shows the source; the slide applies your Retro glyph treatment.':'');
    if(animated){playback=button(previewPlaying?'Pause preview':'Play preview',()=>{previewPlaying=!previewPlaying;resetPreviewClock();playback.textContent=previewPlaying?'Pause preview':'Play preview';});actions.prepend(playback);}
    const previews=document.createElement('div');Object.assign(previews.style,{display:'grid',gridTemplateColumns:'repeat(auto-fit,minmax(min(260px,100%),1fr))',gap:'16px'});
    for(const [name,content]of [['Source',sourceStage],['Result',preview]]){const section=document.createElement('section'),label=document.createElement('h3');label.textContent=name;label.style.margin='0 0 8px';content.style.height='260px';section.append(label,content);previews.append(section);}
    dialog.append(title,previews,hint,controls,status,actions);dialog.addEventListener('cancel',event=>{event.preventDefault();close();});dialog.addEventListener('keydown',event=>{containDialogFocus(event,dialog);if((event.metaKey||event.ctrlKey)&&['z','y'].includes(event.key.toLowerCase())){event.preventDefault();restore(event.key.toLowerCase()==='y'||event.shiftKey?1:-1);}});
    const observer=new ResizeObserver(fit);document.body.append(dialog);dialog.showModal();beginTextPause();draw();observer.observe(preview);observer.observe(sourceStage);handles[0].focus();
    resetPreviewClock();
  }
  async function editText(object,commit,{canvasScene=null,inspectorHost=null,onOutsideCommit=null,renderShapeDraft=null}={}) {
    const previous=document.activeElement;
    // Text uses the shared shaped surface and object transform; list markers
    // decorate editable paragraphs without becoming authored characters.
    const shapeOwner=object,shapeLabel=object.kind==='shape'&&object.label,outerBounds={...object.bounds};
    const growingShape=shapeLabel&&(!object.retroLines||!!renderShapeDraft);
    if(shapeLabel)object={...object,bounds:shapeTextBox(object),text:object.label,paint:object.labelPaint||object.paint};
    const retroText=(shapeLabel?shapeOwner.labelStyle:object.style)==='retro',widthBaseline=retroText ? .4167 : 1;
    const inline=canvasScene?.isConnected&&['text','heading','code','bullet'].includes(object.text?.role)&&(object.kind==='text'||shapeLabel);
    const dialog=document.createElement('dialog'),heading=document.createElement('h2'),area=document.createElement('div'),actions=document.createElement('div'),error=document.createElement('p');
    dialog.dataset.keynopeSceneDialog='text';
    dialog.setAttribute('aria-modal','true');
    dialog.setAttribute('aria-label','Edit Modern text');heading.textContent='Edit text';
    if(object.text.role==='bullet')heading.textContent='Edit list — Enter: new bullet · Shift-Enter: continuation';
    Object.assign(dialog.style,{boxSizing:'border-box',width:'min(1000px,calc(100vw - 16px))',maxWidth:'calc(100vw - 16px)',maxHeight:'90vh',overflow:'auto',background:'#182028',color:'#fff',padding:'20px',border:'1px solid #65717a',font:'14px system-ui'});
    const darkText=(object.paint.color.match(/[0-9a-f]{2}/gi)||[]).slice(0,3).reduce((sum,c)=>sum+parseInt(c,16),0)<384;
    Object.assign(area.style,{overflow:'auto',maxHeight:'65vh',padding:'12px',background:darkText?'#f5f7fa':'#29323b'});
    error.setAttribute('role','alert');
    const cancel=document.createElement('button'),apply=document.createElement('button');cancel.type=apply.type='button';cancel.textContent='Cancel edit';apply.textContent='Apply text';cancel.setAttribute('aria-label','Cancel edit');apply.setAttribute('aria-label','Apply text');
    Object.assign(actions.style,{display:'flex',flexWrap:'wrap',justifyContent:'flex-end',gap:'10px',marginTop:'16px'});
    for(const button of [cancel,apply])Object.assign(button.style,{font:'inherit',padding:'8px 16px',border:'1px solid #65717a',borderRadius:'6px',background:'#29323b',color:'#fff',cursor:'pointer'});
    apply.style.background='#245c91';
    const fontLabel=document.createElement('label'),font=document.createElement('select');fontLabel.textContent='Font ';font.setAttribute('aria-label','Modern font');
    for(const [value,title]of [['','Default for text type'],['sans','Go — proportional'],['mono','Go Mono — monospaced'],['c64','Keynope C64']]){const option=document.createElement('option');option.value=value;option.textContent=title;font.append(option);}
    Object.assign(font.style,{font:'inherit',padding:'6px',marginBottom:'12px'});font.value=object.text.fontId||'';fontLabel.append(font);
    const spacingLabel=document.createElement('label'),spacing=document.createElement('input');spacingLabel.textContent='Line spacing ';spacing.type='number';spacing.min='.5';spacing.max='4';spacing.step='.05';spacing.value=object.text.lineHeight||1.25;spacing.setAttribute('aria-label','Line spacing');spacing.title='Multiple of font size; default 1.25';Object.assign(spacing.style,{width:'80px',font:'inherit',padding:'6px'});spacingLabel.append(spacing);
    const sizeLabel=document.createElement('label'),size=document.createElement('input');sizeLabel.textContent='Font size ';size.type='number';size.min='1';size.max='1024';size.step='any';size.required=true;size.value=object.text.size;size.setAttribute('aria-label','Font size');size.title='Scene pixels; changes font size, not its text box';Object.assign(size.style,{width:'90px',font:'inherit',padding:'6px'});sizeLabel.append(size);
    const typography=document.createElement('div');Object.assign(typography.style,{display:'flex',alignItems:'baseline',flexWrap:'wrap',gap:'16px'});typography.append(fontLabel,sizeLabel,spacingLabel);
    const widthLabel=document.createElement('label'),width=document.createElement('input');widthLabel.textContent='Font width (%) ';width.type='number';width.min='1';width.max='200';width.step='any';width.required=true;width.value=Number(((object.text.widthScale||1)*100).toFixed(6));width.setAttribute('aria-label','Font width percent');Object.assign(width.style,{width:'90px',font:'inherit',padding:'6px'});widthLabel.append(width);typography.insertBefore(widthLabel,spacingLabel);
    const paragraphInputs=['before','after'].map(side=>{
      const label=document.createElement('label'),input=document.createElement('input');label.textContent='Paragraph '+side+' ';
      input.type='number';input.min='0';input.max='4';input.step='.05';input.value=object.text[side==='before'?'paragraphBefore':'paragraphAfter']||0;
      input.setAttribute('aria-label','Paragraph '+side);input.title='Multiple of font size; applies to each paragraph or bullet, not wrapped lines';Object.assign(input.style,{width:'80px',font:'inherit',padding:'6px'});
      label.append(input);typography.append(label);return input;
    });
    if(retroText){
      fontLabel.hidden=spacingLabel.hidden=true;
      for(const input of paragraphInputs)input.parentElement.hidden=true;
      size.max='512';size.step='1';
      width.value=Number(((object.text.widthScale||widthBaseline)*100/widthBaseline).toFixed(6));
      dialog.setAttribute('aria-label','Edit Retro text');
    }
    const inheritSize=document.createElement('button');inheritSize.type='button';inheritSize.textContent='Apply with inherited size';inheritSize.title='Apply text changes and remove the local font-size override';
    const previewPanel=document.createElement('details'),previewTitle=document.createElement('summary'),previewArea=document.createElement('div'),previewStatus=document.createElement('div');
    previewTitle.textContent='Live layout preview';previewPanel.open=true;
    Object.assign(previewPanel.style,{margin:'12px 0',border:'1px solid #65717a',padding:'8px'});
    Object.assign(previewArea.style,{position:'relative',overflow:'hidden',marginTop:'8px',background:darkText?'#f5f7fa':'#29323b',pointerEvents:'none'});
    previewArea.setAttribute('aria-hidden','true');previewArea.className='keynope-text-edit-preview';
    previewStatus.setAttribute('role','status');previewPanel.append(previewTitle,previewArea,previewStatus);
    actions.append(cancel,inheritSize,apply);dialog.append(heading,typography,area,previewPanel,error,actions);document.body.append(dialog);dialog.showModal();
    let inlineObserver,inlineSource,inlineSourceVisibility,inlinePalette,inlineCorner=0,shapeView,shapeHost,growthGeneration=0,growthTask=Promise.resolve();
    const fitInline=()=>{
      if(!inline)return;
      const rect=canvasScene.getBoundingClientRect(),scale=rect.width/parseFloat(canvasScene.style.width);
      if(!(scale>0))return;
      Object.assign(area.style,{left:rect.left+outerBounds.x*scale+'px',top:rect.top+outerBounds.y*scale+'px',width:outerBounds.width+'px',height:outerBounds.height+'px'});
      // Rotate about the same box centre as the authored object.
      area.style.transformOrigin='0 0';
      area.style.transform=`scale(${scale})`;
      inlineFrame.style.transform=`rotate(${object.rotation||0}deg)`;
      const inspector=inspectorHost?.isConnected&&!inspectorHost.hidden?inspectorHost.getBoundingClientRect():null;
      const docked=!!(inspector&&inspector.width>=240&&inspector.height>=300&&inspector.left>=0&&inspector.right<=innerWidth&&getComputedStyle(inspectorHost).display!=='none');
      inlinePalette.dataset.docked=String(docked);
      heading.style.display=docked?'block':'none';
      if(docked){
        Object.assign(inlinePalette.style,{left:inspector.left+'px',top:inspector.top+'px',right:'auto',bottom:'auto',width:inspector.width+'px',height:inspector.height+'px',maxHeight:inspector.height+'px',borderRadius:'0'});
        return;
      }
      Object.assign(inlinePalette.style,{width:'min(960px,calc(100vw - 16px))',height:'auto',maxHeight:'40vh',borderRadius:'8px'});
      const target=(inlineContent.querySelector('.keynope-shaped-text')||inlineContent).getBoundingClientRect();
      const panel=inlinePalette.getBoundingClientRect(),margin=8,gap=12;
      const right=Math.max(margin,innerWidth-panel.width-margin),bottom=Math.max(margin,innerHeight-panel.height-margin);
      const corners=[[right,bottom],[right,margin],[margin,bottom],[margin,margin]];
      const overlap=([x,y])=>Math.max(0,Math.min(x+panel.width,target.right+gap)-Math.max(x,target.left-gap))*Math.max(0,Math.min(y+panel.height,target.bottom+gap)-Math.max(y,target.top-gap));
      // Retain the current corner on a tie; typing should not make controls
      // wander when several equally clear positions are available.
      let best=overlap(corners[inlineCorner]);
      corners.forEach((corner,index)=>{const score=overlap(corner);if(score<best){best=score;inlineCorner=index;}});
      Object.assign(inlinePalette.style,{left:corners[inlineCorner][0]+'px',top:corners[inlineCorner][1]+'px',right:'auto',bottom:'auto'});
    };
    const inlineFrame=document.createElement('div');
    const inlineContent=shapeLabel?document.createElement('div'):inlineFrame;
    if(inline){
      dialog.dataset.keynopeInlineText='true';
      const backdrop=document.createElement('style');backdrop.textContent='dialog[data-keynope-inline-text]::backdrop{background:transparent}.keynope-inline-text-tools[data-docked="true"] label{display:flex;align-items:center;justify-content:space-between;width:100%;gap:8px}.keynope-inline-text-tools[data-docked="true"] select{min-width:0;max-width:70%;margin-bottom:0!important}';dialog.append(backdrop);
      Object.assign(dialog.style,{position:'fixed',inset:'0',margin:'0',width:'100vw',maxWidth:'none',height:'100vh',maxHeight:'none',padding:'0',border:'0',background:'transparent',overflow:'hidden'});
      inlinePalette=document.createElement('div');inlinePalette.className='keynope-inline-text-tools';
      inlinePalette.setAttribute('role','region');inlinePalette.setAttribute('aria-label','Text editing tools');
      Object.assign(inlinePalette.style,{position:'absolute',bottom:'8px',right:'8px',width:'min(960px,calc(100vw - 16px))',maxHeight:'40vh',display:'flex',flexDirection:'column',overflow:'auto',background:'#182028',colorScheme:'dark',border:'1px solid #65717a',borderRadius:'8px',padding:'12px',boxSizing:'border-box'});
      Object.assign(typography.style,{overflow:'auto',minHeight:'0',flex:'1 1 auto',alignContent:'flex-start'});
      Object.assign(actions.style,{flex:'0 0 auto',flexWrap:'wrap',marginTop:'8px'});
      Object.assign(error.style,{flex:'0 0 auto',margin:'4px 0'});
      heading.style.display='none';previewPanel.hidden=true;
      // Keep the scene measurable without painting a second copy. display:none
      // (including a hidden ancestor) makes overflow geometry read as zero.
      Object.assign(previewArea.style,{position:'absolute',left:'0',top:'0',width:object.bounds.width+'px',visibility:'hidden',margin:'0'});
      dialog.append(previewArea);
      previewStatus.className='keynope-text-overflow-status';
      Object.assign(previewStatus.style,{flex:'0 0 auto',color:'#ffd479',font:'13px/1.4 system-ui'});
      inlinePalette.append(heading,typography,previewStatus,error,actions);dialog.append(inlinePalette);
      Object.assign(area.style,{position:'absolute',padding:'0',background:'transparent',overflow:'visible',maxHeight:'none'});
      Object.assign(inlineFrame.style,{width:'100%',height:'100%',transformOrigin:'50% 50%'});area.append(inlineFrame);
      if(shapeLabel){Object.assign(inlineContent.style,{position:'absolute',left:object.bounds.x+'px',top:object.bounds.y+'px',width:object.bounds.width+'px',height:object.bounds.height+'px'});inlineFrame.append(inlineContent);}
      Object.assign(inlineContent.style,{display:'flex',flexDirection:'column',justifyContent:object.text.vertical==='middle'?'center':object.text.vertical==='bottom'?'flex-end':'flex-start'});
      if(object.text.role==='code')Object.assign(inlineContent.style,{background:darkText?'#e4eaf2':'#202833',borderRadius:'8px'});
      inlineSource=Array.from(canvasScene.children).find(node=>node.dataset.objectId===object.id);
      if(shapeLabel&&!growingShape)inlineSource=inlineSource?.querySelector('.keynope-scene-label');
      if(inlineSource){inlineSourceVisibility=inlineSource.style.visibility;inlineSource.style.visibility='hidden';}
      if(growingShape){shapeHost=document.createElement('div');Object.assign(shapeHost.style,{position:'absolute',inset:'0',pointerEvents:'none'});inlineFrame.prepend(shapeHost);}
      inlineObserver=new ResizeObserver(fitInline);inlineObserver.observe(canvasScene);inlineObserver.observe(inlinePalette);if(inspectorHost)inlineObserver.observe(inspectorHost);window.addEventListener('resize',fitInline);fitInline();
    }
    let previewView,previewObject,previewGeneration=0;
    const fitPreview=()=>{
      if(!previewView)return;
      const scale=Math.min(1,previewArea.clientWidth/object.bounds.width,220/object.bounds.height);
      previewArea.style.height=object.bounds.height*scale+'px';
      Object.assign(previewView.element.style,{transformOrigin:'0 0',transform:`scale(${scale})`});
    };
    const updatePreview=model=>{
      const generation=++previewGeneration;previewView?.dispose();
      const data={...object.text,...model};
      delete data.bullet; // Scene paragraphs already supply their own markers.
      if(data.role==='bullet'){
        // Match the persisted bullet convention: two leading spaces make an
        // explicit continuation, not another item. Preserve the styled runs.
        const lines=[[]];
        for(const run of model.runs)run.text.split('\n').forEach((text,index)=>{if(index)lines.push([]);if(text)lines.at(-1).push({...run,text});});
        data.paragraphs=[];
        for(const line of lines){
          if(data.paragraphs.length&&line.map(run=>run.text).join('').startsWith('  ')){
            let trim=2;const runs=line.map(run=>{const count=Math.min(trim,run.text.length);trim-=count;return {...run,text:run.text.slice(count)};});
            data.paragraphs.at(-1).runs.push({text:'\n'},...runs);
          }else data.paragraphs.push({bullet:true,runs:line});
        }
        delete data.runs;
      }else delete data.paragraphs;
      previewObject={id:'editing-preview',kind:'text',bounds:{x:0,y:0,width:object.bounds.width,height:object.bounds.height},text:data,paint:object.paint};
      previewView=mount(previewArea,{version:1,width:object.bounds.width,height:object.bounds.height,background:'transparent',objects:[previewObject]});
      fitPreview();
      previewView.ready.then(warnings=>{if(generation===previewGeneration)previewStatus.textContent=warnings.some(w=>w.code==='text-overflow')?'Text exceeds its box. Increase the box or reduce spacing or font size.':'';});
    };
    const previewObserver=new ResizeObserver(fitPreview);previewObserver.observe(previewArea);
    let updateFormattingState=()=>{};
    const styleInline=model=>{
      if(!inline)return;
      const node=inlineFrame.querySelector('.keynope-shaped-text'),paint=object.paint;
      if(!node)return;
      node.style.opacity=object.text.seeThrough&&object.text.role!=='code'?'0.5':'';
      if(object.text.role==='code'){
        const channels=(paint.color||'#ffffff').match(/[0-9a-f]{2}/gi)||['ff','ff','ff'];
        const background=channels.slice(0,3).reduce((n,v)=>n+parseInt(v,16),0)>384?'#202833':'#e4eaf2';
        node.style.backgroundColor=background+(object.text.seeThrough?'80':'');
      }
      Object.assign(node.style,{flexShrink:'0',tabSize:'4',paddingTop:model.bullet?'0':(model.paragraphBefore||0)*model.size+'px',paddingBottom:model.bullet?'0':(model.paragraphAfter||0)*model.size+'px'});
      if(paint.gradientStart){const angle=paint.gradientDirection==='vertical'?'180deg':paint.gradientDirection==='diagonal'?'135deg':'90deg';Object.assign(node.style,{backgroundImage:`linear-gradient(${angle},${paint.gradientStart},${paint.gradientEnd})`,backgroundClip:'text',webkitBackgroundClip:'text',color:'transparent'});}
      if(paint.stroke){node.style.webkitTextStroke=`${paint.strokeWidth||1}px ${paint.stroke}`;node.style.paintOrder='stroke fill';}
      editor.setEmojiOutline(paint.stroke,paint.strokeWidth||1);
      editor.setEmojiGradient(paint.gradientStart,paint.gradientEnd,paint.gradientDirection);
      if(paint.shadowColor)node.style.filter=`drop-shadow(${shadow(paint)})`;
    };
    let updateGrowth=()=>{};
    const commitFromKeyboard=()=>{if(!pending&&!confirmation&&!closed)return submit(false);};
    const resolveEmojiFonts=async text=>{
      const response=await fetch('/api/editor/emoji-text-fonts?'+new URLSearchParams({text}));
      if(!response.ok)throw new Error('Could not load emoji font');
      return response.json();
    };
    const editor=KeynopeTextLayout.createEditor(inline?inlineContent:area,{...object.text,runs:object.editRuns||object.text.runs,width:object.bounds.width,color:object.paint.color},{resolveEmojiFonts,bullet:object.text.role==='bullet',onCommit:commitFromKeyboard,onChange:model=>{font.value=model.fontId;spacing.value=model.lineHeight;size.value=model.size;width.value=Number((model.widthScale*100/widthBaseline).toFixed(6));paragraphInputs[0].value=model.paragraphBefore;paragraphInputs[1].value=model.paragraphAfter;updateFormattingState();updatePreview(model);styleInline(model);updateGrowth(model);},onError:e=>{error.textContent=e.message;}});
    const keyboardHelp=document.createElement('p');keyboardHelp.id='keynope-text-keys-'+(++serial);
    keyboardHelp.textContent=(object.text.role==='bullet'?'Enter: new bullet; empty bullet: apply. Shift-Enter: continuation. ':'Enter: new line; empty line: apply. Shift-Enter: keep a new line. ')+'Tab: indent. Shift-Tab: formatting controls. ⌘/Ctrl-Enter: apply. Escape: close.';
    Object.assign(keyboardHelp.style,{font:'12px/1.5 system-ui',color:'#c4cdd5',margin:'8px 0'});
    (inline?inlinePalette:dialog).insertBefore(keyboardHelp,actions);
    editor.element.setAttribute('aria-describedby',keyboardHelp.id);
    apply.setAttribute('aria-keyshortcuts','Meta+Enter Control+Enter');
    styleInline(editor.value());
    if(inline){inlineObserver.observe(editor.element);fitInline();}
    updatePreview(editor.value());
    const formatButtons=[],emphasisButtons=new Map();
    formatButtons.push(...paragraphInputs);
    for(const input of paragraphInputs)input.onchange=()=>{
      if(input.value==='')input.value='0';
      if(!input.checkValidity()){input.reportValidity();return;}
      editor.setParagraphSpacing(Number(paragraphInputs[0].value),Number(paragraphInputs[1].value));
    };
    let savedSelection={start:0,end:0};
    const rememberSelection=()=>{const selection=document.getSelection();if(selection&&editor.element.contains(selection.anchorNode)&&editor.element.contains(selection.focusNode))savedSelection=editor.selection();updateFormattingState();};
    document.addEventListener('selectionchange',rememberSelection);
    const selectedRange=()=>{rememberSelection();return savedSelection;};
    const applyRunStyle=style=>{if(pending||editor.isComposing())return;const s=selectedRange();if(s.start===s.end){error.textContent='Select text to format.';return;}editor.select(s.start,s.end);error.textContent='';editor.format(style);};
    const toggleRunStyle=key=>{
      if(pending||editor.isComposing())return;
      const s=selectedRange(),start=Math.min(s.start,s.end),end=Math.max(s.start,s.end);
      if(start===end){error.textContent='Select text to format.';return;}
      let offset=0,all=true;for(const run of editor.value().runs){if(offset<end&&offset+run.text.length>start)all=all&&!!run[key];offset+=run.text.length;}
      applyRunStyle({[key]:!all});
    };
    const emphasisGroup=document.createElement('span');emphasisGroup.style.cssText='display:flex;gap:6px';typography.append(emphasisGroup);
    for(const [key,label]of [['bold','Bold selection'],['italic','Italic selection'],['underline','Underline selection']]){const button=document.createElement('button');button.type='button';button.textContent=key==='bold'?'B':key==='italic'?'I':'U';button.setAttribute('aria-label',label);Object.assign(button.style,{font:'inherit',fontWeight:key==='bold'?'700':'400',fontStyle:key==='italic'?'italic':'normal',padding:'6px 12px'});button.onmousedown=e=>e.preventDefault();button.onclick=()=>toggleRunStyle(key);button.setAttribute('aria-pressed','false');emphasisButtons.set(key,button);emphasisGroup.append(button);formatButtons.push(button);}
    const colour=document.createElement('input');colour.type='color';colour.value=/^#[0-9a-f]{6}$/i.test(object.paint.color)?object.paint.color:'#ffffff';colour.setAttribute('aria-label','Selection colour');colour.addEventListener('pointerdown',rememberSelection);typography.append(colour);formatButtons.push(colour);
    for(const [label,value]of [['Apply colour',()=>colour.value],['Inherit colour',()=>null]]){const button=document.createElement('button');button.type='button';button.textContent=label;Object.assign(button.style,{font:'inherit',padding:'6px 10px'});button.onmousedown=event=>{rememberSelection();event.preventDefault();};button.onclick=()=>applyRunStyle({color:value()});typography.append(button);formatButtons.push(button);}
    font.onchange=()=>{const id=font.value;editor.setFont(id,id==='c64'?'KeynopeC64, monospace':id==='mono'||(!id&&object.text.role==='code')?'KeynopeModernMono, monospace':'KeynopeModern, sans-serif');font.value=editor.value().fontId;};
    spacing.onchange=()=>{if(spacing.value==='')spacing.value='1.25';if(!spacing.checkValidity()){spacing.reportValidity();return;}editor.setLineHeight(Number(spacing.value));spacing.value=editor.value().lineHeight;};
    size.onchange=()=>{if(!size.checkValidity()){size.reportValidity();return;}editor.setSize(Number(size.value));};
    width.onchange=()=>{if(!width.checkValidity()){width.reportValidity();return;}editor.setWidthScale(Number(width.value)*widthBaseline/100);};
    const snapshot=()=>JSON.stringify({runs:editor.value().runs,fontId:editor.value().fontId,lineHeight:editor.value().lineHeight,size:editor.value().size,widthScale:editor.value().widthScale,paragraphBefore:editor.value().paragraphBefore,paragraphAfter:editor.value().paragraphAfter});
    updateFormattingState=()=>{
      const start=Math.min(savedSelection.start,savedSelection.end),end=Math.max(savedSelection.start,savedSelection.end);
      for(const [key,button]of emphasisButtons){let offset=0,on=false,off=false;
        for(const run of editor.value().runs){if(offset<end&&offset+run.text.length>start){if(run[key])on=true;else off=true;}offset+=run.text.length;}
        const state=on?(off?'mixed':'true'):'false';button.setAttribute('aria-pressed',state);
        button.style.background=state==='true'?'#245c91':state==='mixed'?'#504622':'#29323b';button.style.color='#fff';button.style.border='1px '+(state==='mixed'?'dashed #ffdf66':'solid #65717a');
        button.title=button.getAttribute('aria-label')+(state==='mixed'?' — mixed formatting':state==='true'?' — on':' — off');
      }
    };updateFormattingState();
    const initial=snapshot();
    beginTextPause();
    let pending=false,closed=false,confirmation=null,commitForSave;
    const initialText=editor.value().runs.map(run=>run.text).join('');
    const shapeDrafts=new Map();let paintedShapeDraft='';
    const paintShapeDraft=async(generation=growthGeneration)=>{
      if(!shapeHost)return;
      if(shapeOwner.retroLines){
        const key=outerBounds.width+':'+outerBounds.height;
        if(key===paintedShapeDraft)return;
        for(const [oldKey,record]of shapeDrafts)if(oldKey!==key&&!record.done){record.controller.abort();shapeDrafts.delete(oldKey);}
        let record=shapeDrafts.get(key);
        if(!record){
          const bounds={...outerBounds};
          const controller=new AbortController();record={controller,done:false,task:null};
          record.task=Promise.resolve().then(()=>renderShapeDraft(bounds,controller.signal)).finally(()=>{record.done=true;});
          shapeDrafts.set(key,record);
          if(shapeDrafts.size>8)shapeDrafts.delete(shapeDrafts.keys().next().value);
          record.task.catch(()=>{if(shapeDrafts.get(key)===record)shapeDrafts.delete(key);});
        }
        const draft=await record.task;
        if(closed||generation!==growthGeneration)return;
        draft.background='transparent';
        shapeView?.dispose();shapeView=mount(shapeHost,draft);paintedShapeDraft=key;
        return;
      }
      shapeView?.dispose();
      const body={...shapeOwner,id:'editing-shape-body',bounds:{x:0,y:0,width:outerBounds.width,height:outerBounds.height},rotation:0};
      delete body.label;delete body.retroLabel;
      shapeView=mount(shapeHost,{version:1,width:outerBounds.width,height:outerBounds.height,objects:[body]});
    };
    paintShapeDraft().catch(e=>{if(!closed)error.textContent=e.message;});
    updateGrowth=model=>{
      if(!growingShape||editor.isComposing())return;
      const generation=++growthGeneration;
      growthTask=(async()=>{
        const bounds=model.runs.map(run=>run.text).join('')===initialText?shapeOwner.bounds:await grownShapeTextBounds(dialog,shapeOwner,model);
        if(closed||generation!==growthGeneration)return;
        Object.assign(outerBounds,bounds);
        object.bounds=shapeTextBox({...shapeOwner,bounds});
        editor.setLayoutWidth(object.bounds.width);
        if(inline){Object.assign(inlineContent.style,{left:object.bounds.x+'px',top:object.bounds.y+'px',width:object.bounds.width+'px',height:object.bounds.height+'px'});fitInline();}
        if(inline)previewArea.style.width=object.bounds.width+'px';
        updatePreview(editor.value());styleInline(editor.value());
        await paintShapeDraft(generation);
      })();
      growthTask.catch(e=>{if(!closed&&generation===growthGeneration)error.textContent=e.message||'Could not resize shape';});
    };
    const fit=document.createElement('button');fit.type='button';fit.textContent='Fit text to box';fit.title='Reduce font size to fit this box; keep its position and dimensions';typography.append(fit);formatButtons.push(fit);
    Object.assign(fit.style,{font:'inherit',padding:'6px 10px'});
    const setPending=value=>{
      pending=value;
      for(const control of [apply,inheritSize,cancel,font,spacing,size,width,...paragraphInputs,...formatButtons])control.disabled=value;
      area.inert=value;editor.element.contentEditable=String(!value);
      if(value)dialog.setAttribute('aria-busy','true');else dialog.removeAttribute('aria-busy');
    };
    fit.onclick=async()=>{
      if(pending||closed||confirmation||editor.isComposing())return;
      const selection=editor.selection();
      setPending(true);error.textContent='';
      try{
        const fitted=await fittedTextSize(dialog,{version:1,width:object.bounds.width,height:object.bounds.height,background:'transparent'},previewObject);
        editor.setSize(fitted);
      }catch(e){error.textContent=e.message||'Could not fit text';}
      finally{setPending(false);editor.element.focus();editor.select(selection.start,selection.end);}
    };
    const close=()=>{if(pending||closed)return;closed=true;++growthGeneration;for(const record of shapeDrafts.values())if(!record.done)record.controller.abort();shapeDrafts.clear();shapeView?.dispose();pendingTextDrafts.delete(commitForSave);++previewGeneration;previewObserver.disconnect();inlineObserver?.disconnect();window.removeEventListener('resize',fitInline);if(inlineSource)inlineSource.style.visibility=inlineSourceVisibility;previewView?.dispose();document.removeEventListener('selectionchange',rememberSelection);endTextPause();editor.dispose();dialog.close();dialog.remove();if(previous?.isConnected)previous.focus({preventScroll:true});};
    const requestClose=()=>{
      if(pending||closed||confirmation||editor.isComposing())return;
      if(initial===snapshot()){close();return;}
      rememberSelection();
      confirmation=document.createElement('dialog');confirmation.setAttribute('aria-label','Keep text changes?');confirmation.setAttribute('aria-modal','true');
      Object.assign(confirmation.style,{width:'min(440px,calc(100vw - 40px))',boxSizing:'border-box',padding:'20px',background:'#182028',color:'#ffffff',border:'1px solid #65717a',borderRadius:'8px',font:'16px system-ui',colorScheme:'dark'});
      const title=document.createElement('h2');title.textContent='Keep text changes?';title.style.marginTop='0';
      const explanation=document.createElement('p');explanation.textContent='Keep your edits, or revert to the text before you started editing.';
      const choices=document.createElement('div');Object.assign(choices.style,{display:'flex',flexWrap:'wrap',gap:'8px',justifyContent:'flex-end'});
      const dismiss=()=>{confirmation.close();confirmation.remove();confirmation=null;dialog.inert=false;editor.element.contentEditable='true';editor.element.focus();editor.select(savedSelection.start,savedSelection.end);};
      for(const [label,action]of [['Keep editing',()=>{}],['Revert',close],['Keep changes',()=>submit(false)]]){
        const button=document.createElement('button');button.type='button';button.textContent=label;
        Object.assign(button.style,{font:'inherit',padding:'8px 12px',background:'#29323b',color:'#ffffff',border:'1px solid #65717a',borderRadius:'5px'});
        button.onclick=()=>{dismiss();action();};choices.append(button);
      }
      confirmation.append(title,explanation,choices);document.body.append(confirmation);
      confirmation.addEventListener('keydown',event=>containDialogFocus(event,confirmation));
      confirmation.addEventListener('cancel',event=>{event.preventDefault();dismiss();});
      dialog.inert=true;editor.element.contentEditable='false';
      confirmation.showModal();choices.firstElementChild.focus();
    };
    cancel.onclick=requestClose;dialog.addEventListener('cancel',event=>{event.preventDefault();requestClose();});
    dialog.addEventListener('keydown',event=>containDialogFocus(event,dialog));
    dialog.addEventListener('keydown',event=>{
      if(event.isComposing||editor.isComposing())return;
      if((event.metaKey||event.ctrlKey)&&!event.shiftKey&&!event.altKey&&event.key==='Enter'){
        event.preventDefault();event.stopPropagation();submit(false);return;
      }
      if((event.metaKey||event.ctrlKey)&&!event.shiftKey&&event.key.toLowerCase()==='s'){
        event.preventDefault();event.stopPropagation();globalThis.webkit?.messageHandlers?.keynopePresenter?.postMessage({action:'save-presentation'});
      }
    },true);
    dialog.addEventListener('keydown',event=>{if(event.isComposing||editor.isComposing())return;if((event.metaKey||event.ctrlKey)&&['b','i','u'].includes(event.key.toLowerCase())){event.preventDefault();event.stopPropagation();toggleRunStyle(event.key.toLowerCase()==='b'?'bold':event.key.toLowerCase()==='i'?'italic':'underline');}},true);
    const submit=async resetSize=>{
      if(pending||editor.isComposing())return;
      if(!spacing.checkValidity()){spacing.reportValidity();return;}
      if(!size.checkValidity()){size.reportValidity();return;}
      if(!width.checkValidity()){width.reportValidity();return;}
      for(const input of paragraphInputs)if(!input.checkValidity()){input.reportValidity();return;}
      if(initial===snapshot()&&!resetSize){close();return;}
      setPending(true);error.textContent='';
      try{await growthTask;await commit(editor.value().runs,editor.value().fontId,editor.value().lineHeight,resetSize?0:editor.value().size===object.text.size?undefined:editor.value().size,editor.value().paragraphBefore,editor.value().paragraphAfter,editor.value().widthScale===(object.text.widthScale||1)?undefined:editor.value().widthScale*100,growingShape&&(outerBounds.width!==shapeOwner.bounds.width||outerBounds.height!==shapeOwner.bounds.height)?{...outerBounds}:undefined);pending=false;close();}
      catch(e){error.textContent=e.message||'Could not apply text';}
      finally{setPending(false);if(!closed)editor.element.focus();}
    };
    apply.onclick=()=>submit(false);inheritSize.onclick=()=>submit(true);
    commitForSave=async()=>{
      if(confirmation)throw Error('Finish the text changes confirmation before saving.');
      if(pending||editor.isComposing())throw Error('Finish the current text input before saving.');
      await submit(false);
      if(!closed)throw Error(error.textContent||'Correct the text settings before saving.');
    };
    pendingTextDrafts.add(commitForSave);
    if(inline){
      let outsidePress=null;
      dialog.addEventListener('contextmenu',event=>{if(event.target===dialog)event.preventDefault();});
      dialog.addEventListener('pointerdown',event=>{
        outsidePress=null;
        if(event.target!==dialog||event.button!==0||pending||confirmation||editor.isComposing())return;
        event.preventDefault();
        outsidePress={id:event.pointerId,x:event.clientX,y:event.clientY};
      });
      dialog.addEventListener('pointermove',event=>{
        if(outsidePress?.id===event.pointerId&&Math.hypot(event.clientX-outsidePress.x,event.clientY-outsidePress.y)>4)outsidePress=null;
      });
      dialog.addEventListener('pointercancel',()=>{outsidePress=null;});
      dialog.addEventListener('pointerup',async event=>{
        const press=outsidePress;outsidePress=null;
        if(!press||press.id!==event.pointerId||event.target!==dialog||Math.hypot(event.clientX-press.x,event.clientY-press.y)>4)return;
        event.preventDefault();event.stopPropagation();
        await submit(false);
        if(closed&&onOutsideCommit)await onOutsideCommit({x:press.x,y:press.y});
      });
    }
    await editor.ready();if(!closed){editor.element.focus();editor.select(editor.value().runs.reduce((n,r)=>n+r.text.length,0));}
  }
  function hasModernText(object) {
    if(!object)return false;
    if(object.kind==='shape')return !!object.label && !object.retroLabel;
    return !!(object.text||object.editRuns) && !object.retroText && !object.retroLines;
  }
  async function fittedTextSize(parent,scene,object) {
    if(!hasModernText(object))throw new Error('Retro text uses its own font sizing; Modern Fit Text does not apply.');
    const data=object.text||object.label;
    if(!data)throw Error('This object has no text to fit.');
    const host=document.createElement('div');host.setAttribute('aria-hidden','true');Object.assign(host.style,{position:'fixed',left:'-100000px',top:'0',visibility:'hidden'});parent.append(host);
    const fits=async size=>{
      const candidate=structuredClone(object);(candidate.text||candidate.label).size=size;
      const view=mount(host,{...scene,objects:[candidate],diagnostics:[]});
      try{return !(await view.ready).some(warning=>warning.code==='text-overflow');}finally{view.dispose();}
    };
    try{
      if(await fits(data.size))return data.size;
      if(!await fits(1))throw Error('Text cannot fit at the minimum size. Enlarge the box or shorten the text.');
      let low=1,high=data.size;
      for(let n=0;n<12;n++){const mid=(low+high)/2;if(await fits(mid))low=mid;else high=mid;}
      const precision=(object.kind==='shape'?object.labelStyle:object.style)==='retro'?1:100;
      return Math.max(1,Math.floor(low*precision)/precision);
    }finally{host.remove();}
  }
  async function preview(load,commitText) {
    const previous=document.activeElement,dialog=document.createElement('dialog');
    dialog.dataset.keynopeSceneDialog='preview';
    dialog.setAttribute('aria-label','Slide preview');
    dialog.setAttribute('aria-modal','true');
    Object.assign(dialog.style,{width:'min(1200px,94vw)',maxWidth:'94vw',height:'90vh',maxHeight:'90vh',padding:'16px',boxSizing:'border-box',background:'#182028',color:'#fff',border:'1px solid #65717a',font:'14px/1.4 system-ui',overflow:'hidden'});
    const header=document.createElement('div'),title=document.createElement('strong'),close=document.createElement('button');
    title.textContent=commitText?'Slide preview — double-click content to edit':'Slide preview';close.textContent='Close preview';close.type='button';
    Object.assign(header.style,{display:'flex',justifyContent:'space-between',alignItems:'center',height:'36px'});header.append(title,close);
    const area=document.createElement('div');Object.assign(area.style,{position:'relative',height:'calc(100% - 200px)',overflow:'hidden',margin:'12px 0'});
    const status=document.createElement('div');status.setAttribute('role','status');status.textContent='Preparing scene…';Object.assign(status.style,{height:'130px',overflow:'auto',font:'13px/1.4 system-ui'});
    dialog.append(header,area,status);document.body.append(dialog);dialog.showModal();
    let view,observer,frame,closed=false;
    const started=performance.now(),motionPreference=matchMedia('(prefers-reduced-motion: reduce)');
    const hasPlayback=()=>{const counts=view?.resourceCounts();return counts&&(counts.animationTracks||counts.retroTracks);};
    const tick=now=>{frame=0;if(closed||document.hidden||motionPreference.matches||!hasPlayback())return;view.setTime(now-started);frame=requestAnimationFrame(tick);};
    const refreshPlayback=()=>{
      cancelAnimationFrame(frame);frame=0;
      if(closed||!view||document.hidden)return;
      view.setTime(performance.now()-started);
      if(!motionPreference.matches&&hasPlayback())frame=requestAnimationFrame(tick);
    };
    document.addEventListener('visibilitychange',refreshPlayback);motionPreference.addEventListener('change',refreshPlayback);
    const dismiss=()=>{if(closed)return;closed=true;observer?.disconnect();cancelAnimationFrame(frame);document.removeEventListener('visibilitychange',refreshPlayback);motionPreference.removeEventListener('change',refreshPlayback);view?.dispose();dialog.close();dialog.remove();previous?.focus();};
    close.onclick=dismiss;dialog.addEventListener('cancel',event=>{event.preventDefault();dismiss();});
    dialog.addEventListener('keydown',event=>containDialogFocus(event,dialog));
    close.focus();
    try {
      let [scene]=await Promise.all([load(),ensureFonts()]);if(closed)return;
      const redraw=()=>{view?.dispose();view=mount(area,scene);refreshPlayback();};redraw();
      area.addEventListener('dblclick',event=>{
        const object=scene.objects.find(o=>o.id===event.target.closest('[data-object-id]')?.dataset.objectId);
        if(commitText&&object?.editable&&object.kind==='image'&&object.media){
          editImage(object,async (crop,modernMask)=>{await commitText({action:'set-scene-crop',objectId:object.id,slide:scene.slideIndex,sceneRevision:scene.revision,crop,modernMask});scene=await load();if(!closed){redraw();fit();await view.ready;}});return;
        }
        if(!commitText||!object?.editable||!hasModernText(object))return;
        editText(object,async (runs,modernFont,modernLineHeight,modernSize,modernParagraphBefore,modernParagraphAfter,modernWidth,shapeTextBounds)=>{await commitText({objectId:object.id,slide:scene.slideIndex,sceneRevision:scene.revision,textRuns:runs,modernFont,modernLineHeight,modernSize,modernParagraphBefore,modernParagraphAfter,modernWidth,shapeTextBounds});scene=await load();if(!closed){redraw();fit();await view.ready;}});
      });
      const fit=()=>{
        const scale=Math.min(area.clientWidth/scene.width,area.clientHeight/scene.height);
        Object.assign(view.element.style,{transformOrigin:'0 0',transform:`scale(${scale})`,left:(area.clientWidth-scene.width*scale)/2+'px',top:(area.clientHeight-scene.height*scale)/2+'px'});
      };
      observer=new ResizeObserver(fit);observer.observe(area);fit();
      const warnings=await view.ready;if(closed)return;
      status.replaceChildren();const intro=document.createElement('p');intro.textContent=commitText?'Double-click authored text or shapes to edit text, or images to crop. Applying commits one undoable change. Inherited objects are not editable here yet.':'Preview only.';status.append(intro);
      const list=document.createElement('ul');for(const warning of warnings){const item=document.createElement('li');item.textContent=(warning.objectId?warning.objectId+': ':'')+warning.message;list.append(item);}status.append(list);
      if(commitText){
        for(const warning of warnings.filter(warning=>warning.code==='text-overflow')){
          const object=scene.objects.find(object=>object.id===warning.objectId);if(!object?.editable||!hasModernText(object))continue;
          const fitButton=document.createElement('button');fitButton.type='button';fitButton.textContent='Fit text · '+object.id;
          fitButton.onclick=async()=>{
            fitButton.disabled=true;
            try{
              const current=scene.objects.find(candidate=>candidate.id===object.id);if(!current?.editable)throw Error('This text is no longer editable.');
              const revision=scene.revision,modernSize=await fittedTextSize(dialog,scene,current);if(closed)return;
              await commitText({objectId:current.id,slide:scene.slideIndex,sceneRevision:revision,textRuns:current.editRuns||(current.text||current.label).runs,modernSize});
              scene=await load();if(closed)return;redraw();fit();await view.ready;
              fitButton.textContent='Fitted · '+modernSize+' px (Modern only)';
            }catch(error){fitButton.textContent=error.message||'Could not fit text';fitButton.disabled=false;}
          };status.append(fitButton);
        }
      }
      refreshPlayback();
    }catch(error){if(!closed)status.textContent='Could not prepare preview: '+error.message;}
  }
  return Object.freeze({mount,preview,editText,editImage,ensureFonts,fittedTextSize,grownShapeTextBounds,blockGlyphMask,drawBlockGlyph,commitPendingText,isEditing:()=>textEditors>0});
})();
