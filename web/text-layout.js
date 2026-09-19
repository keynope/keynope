// Browser-shaped text boundary for the semantic renderer. Coordinates are local
// CSS/design pixels; callers apply the scene transform outside this surface.
// Source offsets are UTF-16, matching DOM Range and browser editing APIs.
globalThis.KeynopeTextLayout = (() => {
  'use strict';
  const segmenter = new Intl.Segmenter(undefined, {granularity:'grapheme'});
  // Fonts are shared while a surface owns them and removed after its last
  // owner disappears. The original Unicode remains in each text node.
  const emojiFaces=new Map();let emojiSerial=0,tintSerial=0;
  function acquireEmoji(data){
    let entry=emojiFaces.get(data);
    if(!entry){
      const family='KeynopeShapedEmoji'+(++emojiSerial),face=new FontFace(family,'url(data:font/woff2;base64,'+data+')');
      document.fonts.add(face);
      entry={family,face,owners:0,ready:face.load()};
      // Readiness consumers still receive failures; avoid an unhandled promise
      // if a short-lived surface is disposed before its readiness is requested.
      entry.ready.catch(()=>{});emojiFaces.set(data,entry);
    }
    entry.owners++;return entry;
  }
  function releaseEmoji(data){const entry=emojiFaces.get(data);if(entry&&--entry.owners===0){document.fonts.delete(entry.face);emojiFaces.delete(data);}}
  const finite = (value, fallback, min, max) => Number.isFinite(value) ? Math.max(min,Math.min(max,value)) : fallback;
  function normalize(input) {
    if (!input || !Array.isArray(input.runs)) throw new Error('Text requires an array of styled runs');
    if (input.runs.length > 10000) throw new Error('Too many text runs');
    const runs = input.runs.map(run => {
      if (typeof run.text !== 'string') throw new Error('Text run content must be a string');
      return {text:run.text, bold:!!run.bold, italic:!!run.italic, underline:!!run.underline,
        color:/^#[0-9a-f]{6}$/i.test(run.color||'') ? run.color : null};
    });
    if (runs.reduce((n,run)=>n+run.text.length,0)>100000) throw new Error('Text exceeds layout limit');
    return {runs, emojiFonts:input.emojiFonts&&typeof input.emojiFonts==='object'?input.emojiFonts:{}, bullet:!!input.bullet, width:finite(input.width,640,1,20000), size:finite(input.size,32,1,1024),
      widthScale:finite(input.widthScale,1,.004167,2),
      emojiWidthScale:finite(input.emojiWidthScale,1,.01,240),
      emojiTint:/^#[0-9a-f]{6}$/i.test(input.emojiTint||'')?input.emojiTint:'',
      lineHeight:finite(input.lineHeight,1.25,.5,4),
      paragraphBefore:finite(input.paragraphBefore,0,0,4),
      paragraphAfter:finite(input.paragraphAfter,0,0,4),
      // Assigning CSS properties (never HTML) lets browsers validate families.
      family:typeof input.family==='string'?input.family:'sans-serif',
      fontId:typeof input.fontId==='string'?input.fontId:'',
      direction:['ltr','rtl','auto'].includes(input.direction)?input.direction:'auto',
      align:['start','center','end','justify'].includes(input.align)?input.align:'start',
      locale:typeof input.locale==='string'?input.locale:'',
      color:/^#[0-9a-f]{6}$/i.test(input.color||'')?input.color:'#ffffff'};
  }
  function boundary(text,offset,forward=false) {
    offset=Math.max(0,Math.min(text.length,Number(offset)||0));
    const points=[0,...Array.from(segmenter.segment(text),part=>part.index+part.segment.length)];
    return seek(points,offset,forward);
  }
  function seek(points,offset,forward=false) {
    let low=0,high=points.length-1;
    while(low<high){const mid=(low+high)>>1;if(points[mid]<offset)low=mid+1;else high=mid;}
    return points[low]===offset||forward?points[low]:points[Math.max(0,low-1)];
  }
  function compact(runs) {
    const result=[];
    for(const run of runs) {
      if(!run.text)continue;
      const last=result.at(-1);
      if(last&&last.bold===run.bold&&last.italic===run.italic&&last.underline===run.underline&&last.color===run.color)last.text+=run.text;
      else result.push({...run});
    }
    return result;
  }
  function slice(runs,start,end) {
    let offset=0;const result=[];
    for(const run of runs){
      const a=Math.max(0,start-offset),b=Math.min(run.text.length,end-offset);
      if(b>a)result.push({...run,text:run.text.slice(a,b)});
      offset+=run.text.length;
    }
    return result;
  }
  // Pure operations: the host owns transactions/history and commits once after
  // IME composition, rather than serializing half-composed browser text.
  function replace(input,start,end,inserted) {
    const model=normalize(input),text=model.runs.map(run=>run.text).join('');
    if(typeof inserted!=='string')throw new Error('Inserted text must be a string');
    const a=boundary(text,Math.min(start,end)),b=start===end?a:boundary(text,Math.max(start,end),true);
    const before=slice(model.runs,0,a),after=slice(model.runs,b,text.length);
    // Replacing a selection retains its first character's style. Plain typing
    // at a boundary continues the preceding run; at offset zero use the next.
    const selected=a<b?slice(model.runs,a,b)[0]:null;
    const style=selected||before.at(-1)||after[0]||{bold:false,italic:false,color:null};
    return normalize({...model,runs:compact([...before,{...style,text:inserted},...after])});
  }
  function format(input,start,end,style) {
    const model=normalize(input),text=model.runs.map(run=>run.text).join('');
    if(start===end)return model;
    const a=boundary(text,Math.min(start,end)),b=boundary(text,Math.max(start,end),true);
    const patch={};
    for(const key of ['bold','italic','underline','color'])if(Object.hasOwn(style,key))patch[key]=style[key];
    return normalize({...model,runs:compact([...slice(model.runs,0,a),
      ...slice(model.runs,a,b).map(run=>({...run,...patch})),...slice(model.runs,b,text.length)])});
  }
  function create(parent,input) {
    const document=parent.ownerDocument, root=document.createElement('div');
    root.className='keynope-shaped-text';
    root.setAttribute('role','document');
    Object.assign(root.style,{position:'relative',boxSizing:'border-box',padding:'0',margin:'0',
      border:'0',whiteSpace:'pre-wrap',overflowWrap:'anywhere',wordBreak:'normal',
      fontKerning:'normal',fontVariantLigatures:'normal',letterSpacing:'normal',
      textRendering:'optimizeLegibility',minHeight:'1em'});
    parent.append(root);
    // Zero-size probes recover the actual accumulated scene transform,
    // including ancestor zoom and rotation, without depending on getBoxQuads
    // (not available in every supported browser).
    const probes=[[0,0],[100,0],[0,100]].map(([x,y])=>{
      const node=document.createElement('span');node.setAttribute('aria-hidden','true');
      Object.assign(node.style,{position:'absolute',left:x+'px',top:y+'px',width:'0',height:'0',padding:'0',margin:'0',border:'0',pointerEvents:'none'});
      return node;
    });
    let model, text='', nodes=[], segments=[], points=[0], revision=0, disposed=false,probeDistance=100,fontReadiness=null;
    const ownedEmoji=new Map();
    let tintID='';
    let emojiOutline=null,emojiGradient=null,emojiDefinitions=[];
    function tintDefinition(color,span){
      const ns='http://www.w3.org/2000/svg',svg=document.createElementNS(ns,'svg'),filter=document.createElementNS(ns,'filter'),matrix=document.createElementNS(ns,'feColorMatrix');
      svg.setAttribute('aria-hidden','true');
      Object.assign(svg.style,{position:'absolute',width:'0',height:'0',pointerEvents:'none'});
      filter.id=tintID;filter.setAttribute('color-interpolation-filters','sRGB');
      let content='SourceGraphic';
      if(emojiGradient){
        filter.setAttribute('primitiveUnits','objectBoundingBox');
        const w=Math.max(1,span.offsetWidth),h=Math.max(1,span.offsetHeight),canvas=document.createElement('canvas');
        canvas.width=Math.ceil(w);canvas.height=Math.ceil(h);
        const ctx=canvas.getContext('2d'),rw=root.clientWidth,rh=root.clientHeight,x=span.offsetLeft,y=span.offsetTop;
        // Match CSS's 90/180/135 degree gradient line, including its diagonal
        // "magic corners", in the untransformed text surface coordinates.
        let x1=0,y1=0,x2=rw,y2=0;
        if(emojiGradient.direction==='vertical'){x2=0;y2=rh;}
        else if(emojiGradient.direction==='diagonal'){const d=(rw+rh)/4;x1=rw/2-d;y1=rh/2-d;x2=rw/2+d;y2=rh/2+d;}
        const ramp=ctx.createLinearGradient(x1-x,y1-y,x2-x,y2-y);
        ramp.addColorStop(0,emojiGradient.start);ramp.addColorStop(1,emojiGradient.end);ctx.fillStyle=ramp;ctx.fillRect(0,0,w,h);
        const image=document.createElementNS(ns,'feImage');
        for(const [k,v]of Object.entries({href:canvas.toDataURL(),x:0,y:0,width:1,height:1,preserveAspectRatio:'none',result:'ramp'}))image.setAttribute(k,v);
        const mask=document.createElementNS(ns,'feComposite');
        for(const [k,v]of Object.entries({in:'ramp',in2:'SourceAlpha',operator:'in',result:'gradient'}))mask.setAttribute(k,v);
        filter.append(image,mask);content='gradient';
      }
      if(color){
        const rgb=[1,3,5].map(i=>parseInt(color.slice(i,i+2),16)/255);
        const values=rgb.flatMap(c=>[.299*c,.587*c,.114*c,0,0]);values.push(0,0,0,1,0);
        matrix.setAttribute('type','matrix');matrix.setAttribute('in',content);matrix.setAttribute('values',values.join(' '));matrix.setAttribute('result','tinted');
        filter.append(matrix);content='tinted';
      }
      if(emojiOutline){
        const add=(tag,attrs)=>{const node=document.createElementNS(ns,tag);for(const [k,v]of Object.entries(attrs))node.setAttribute(k,v);filter.append(node);return node;};
        // Compensate the text squeeze so the outline is equally thick on both
        // axes in design pixels. Expand the filter region to avoid clipping.
        filter.setAttribute('x','-100%');filter.setAttribute('y','-100%');filter.setAttribute('width','300%');filter.setAttribute('height','300%');
        const rx=emojiOutline.width/model.widthScale/(emojiGradient?Math.max(1,span.offsetWidth):1),ry=emojiOutline.width/(emojiGradient?Math.max(1,span.offsetHeight):1);
        add('feMorphology',{in:'SourceAlpha',operator:'dilate',radius:rx+' '+ry,result:'expanded'});
        add('feFlood',{'flood-color':emojiOutline.color,result:'outline-colour'});
        add('feComposite',{in:'outline-colour',in2:'expanded',operator:'in',result:'outline'});
        const merge=add('feMerge',{});
        for(const source of ['outline',content]){const node=document.createElementNS(ns,'feMergeNode');node.setAttribute('in',source);merge.append(node);}
      }
      svg.append(filter);return svg;
    }
    function refreshEmojiPaint(){
      for(const definition of emojiDefinitions)definition.remove();emojiDefinitions=[];
      // WebKit can retain the old filter graph if a replacement reuses its ID.
      // A fresh URL invalidates paint without replacing the Unicode text nodes.
      const active=!!(model.emojiTint||emojiOutline||emojiGradient);
      let commonID='';
      for(const span of root.querySelectorAll('.keynope-shaped-emoji')){
        if(active){
          if(!commonID||emojiGradient){
            tintID='keynope-shaped-emoji-tint-'+(++tintSerial);commonID=tintID;
            const definition=tintDefinition(model.emojiTint,span);root.append(definition);emojiDefinitions.push(definition);
          }
          span.style.filter='url(#'+commonID+')';
        }else span.style.filter='';
      }
    }
    function setEmojiGradient(start,end,direction){
      const valid=value=>/^#[0-9a-f]{6}$/i.test(value||'');
      const next=valid(start)&&valid(end)?{start,end,direction:['vertical','diagonal'].includes(direction)?direction:'horizontal'}:null;
      if(JSON.stringify(next)===JSON.stringify(emojiGradient))return api;
      emojiGradient=next;refreshEmojiPaint();return api;
    }
    function setEmojiOutline(color,width=1){
      const next=/^#[0-9a-f]{6}$/i.test(color||'')?{color,width:finite(width,1,.01,100)}:null;
      if(JSON.stringify(next)===JSON.stringify(emojiOutline))return api;
      emojiOutline=next;refreshEmojiPaint();return api;
    }
    function update(input) {
      fontReadiness=null;
      model=normalize(input);text=model.runs.map(run=>run.text).join('');revision++;
      segments=Array.from(segmenter.segment(text));
      const needed=new Set(segments.map(part=>model.emojiFonts[part.segment]).filter(data=>typeof data==='string'&&data));
      for(const data of ownedEmoji.keys())if(!needed.has(data)){releaseEmoji(data);ownedEmoji.delete(data);}
      for(const data of needed)if(!ownedEmoji.has(data))ownedEmoji.set(data,acquireEmoji(data));
      points=[0,...segments.map(part=>part.index+part.segment.length)];
      root.dir=model.direction;root.lang=model.locale;
      Object.assign(root.style,{width:model.width/model.widthScale+'px',transform:`scaleX(${model.widthScale})`,transformOrigin:'0 0',fontFamily:model.family,fontSize:model.size+'px',
        fontWeight:'400',fontStyle:'normal',lineHeight:String(model.lineHeight),
        textAlign:model.align==='justify'?'start':model.align,color:model.color});
      root.replaceChildren();nodes=[];let offset=0;
      // Isolate spaces without splitting words/ligatures. Their DOM text stays
      // unchanged, so source offsets, selection and editing retain one model.
      const appendRun=(run,parent,hidden=false,emoji=null)=>{
        const span=document.createElement('span');
        if(run.bold)span.style.fontWeight='700';
        if(run.italic)span.style.fontStyle='italic';
        if(run.underline)span.style.textDecorationLine='underline';
        if(run.color)span.style.color=run.color;
        if(hidden)span.style.display='none';
        if(emoji){span.className='keynope-shaped-emoji';Object.assign(span.style,{fontFamily:emoji.family,fontWeight:'400',fontStyle:'normal',fontSize:'.8em',paddingInline:'.03125em',whiteSpace:'nowrap',color:'#ffffff',webkitTextStroke:'0px',fontVariantLigatures:'normal'});}
        // Keep the same atomic inline box with and without tint: changing a
        // paint property must not alter browser rounding or caret geometry.
        if(emoji)span.style.display='inline-block';
        if(emoji&&model.emojiTint)span.style.filter='url(#'+tintID+')';
        const node=document.createTextNode(run.text);
        if(emoji&&model.emojiWidthScale!==1){
          // Reserve the expanded advance in layout, then expand only the ink.
          // The parent text squeeze still applies the user's width setting.
          Object.assign(span.style,{display:'inline-block',width:model.emojiWidthScale+'em',paddingInline:(.03125*model.emojiWidthScale)+'em'});
          const ink=document.createElement('span');
          Object.assign(ink.style,{display:'inline-block',transform:'scaleX('+model.emojiWidthScale+')',transformOrigin:'left center'});
          ink.append(node);span.append(ink);
        }else span.append(node);
        parent.append(span);
        nodes.push({node,start:offset,end:offset+run.text.length});offset+=run.text.length;
      };
      const appendRuns=(runs,parent)=>{
        const parts=model.align==='justify'?runs.flatMap(run=>run.text.split(/( )/).filter(Boolean).map(text=>({...run,text}))):runs;
        for(const run of parts)if(run.text){
          let plain='';
          for(const {segment}of segmenter.segment(run.text)){
            const emoji=ownedEmoji.get(model.emojiFonts[segment]);
            if(emoji){if(plain){appendRun({...run,text:plain},parent);plain='';}appendRun({...run,text:segment},parent,false,emoji);}
            else plain+=segment;
          }
          if(plain)appendRun({...run,text:plain},parent);
        }
      };
      if(model.bullet){
        let row,start=0;
        for(const line of text.split('\n')){
          const continuation=!!row&&line.startsWith('  ');
          if(row)appendRun({text:'\n'},row,!continuation);
          if(!continuation){
            row=document.createElement('div');row.className='keynope-edit-bullet';row.dir=model.direction;
            Object.assign(row.style,{position:'relative',boxSizing:'border-box',paddingInlineStart:model.size*1.1/model.widthScale+'px',paddingTop:model.paragraphBefore*model.size+'px',paddingBottom:model.paragraphAfter*model.size+'px',minHeight:model.size*model.lineHeight+'px'});
            const marker=document.createElement('span');marker.setAttribute('aria-hidden','true');marker.contentEditable='false';
            Object.assign(marker.style,{position:'absolute',left:model.size*.25/model.widthScale+'px',top:model.size*(model.paragraphBefore+model.lineHeight/2-.09)+'px',width:model.size*.18/model.widthScale+'px',height:model.size*.18+'px',borderRadius:'50%',background:model.color,pointerEvents:'none'});
            marker.style.left='';marker.style.insetInlineStart=model.size*.25/model.widthScale+'px';
            row.append(marker);root.append(row);
          }else appendRun({text:'  '},row,true);
          const from=start+(continuation?2:0);
          appendRuns(slice(model.runs,from,start+line.length),row);
          if(from===start+line.length){appendRun({text:''},row);row.append(document.createElement('br'));}
          start+=line.length+1;
        }
      }else{
        appendRuns(model.runs,root);
      }
      if (!nodes.length) {const node=document.createTextNode('');root.append(node);nodes.push({node,start:0,end:0});}
      root.append(...probes);
      // Probe coordinates describe the visible design-space width, not the
      // uncompressed CSS text layout. Caret/range geometry then shares the
      // exact same coordinate system as painting and selection hit targets.
      probeDistance=Math.max(1,Math.min(100,model.width/2,model.size/2));
      probes[1].style.left=probeDistance/model.widthScale+'px';probes[2].style.top=probeDistance+'px';
      justify();
      refreshEmojiPaint();
      return api;
    }
    function justify() {
      if(model.align!=='justify'||disposed)return;
      for(const record of nodes)record.node.parentElement.style.wordSpacing='';
      // Measure the natural lines first, in unrotated design coordinates.
      // A short line (<70% of the longest) must not be stretched. Whitespace
      // at either edge is never an expandable gap.
      const lines=[],records=new Map(nodes.map(record=>[record.start,record])),inverse=transform().inverse();
      for(const part of segments){
        if(part.segment==='\n'||part.segment==='\r')continue;
        const rect=rectangles(part.index,part.index+part.segment.length,inverse)[0];
        if(!rect||rect.width<=0)continue;
        let line=lines.at(-1);
        if(line&&Math.abs(line.y-rect.y)>=model.size*.2)line=null;
        if(!line){line={y:rect.y,parts:[]};lines.push(line);}
        line.parts.push({start:part.index,end:part.index+part.segment.length,text:part.segment,rect});
      }
      for(const line of lines){
        const ink=line.parts.filter(p=>!/^\s+$/.test(p.text));
        line.left=ink.length?Math.min(...ink.map(p=>p.rect.x)):0;
        line.right=ink.length?Math.max(...ink.map(p=>p.rect.x+p.rect.width)):0;
        line.width=line.right-line.left;
      }
      const longest=Math.max(0,...lines.map(line=>line.width));
      for(const line of lines){
        if(!line.width||line.width<longest*.7)continue;
        const gaps=line.parts.filter(p=>p.text===' '&&p.rect.x>line.left&&p.rect.x+p.rect.width<line.right);
        if(!gaps.length)continue;
        // Leave half a design pixel for browser subpixel rounding so the last
        // word cannot be pushed onto a new line by the added spacing.
        const extra=Math.max(0,(model.width-(model.bullet?model.size*1.1:0)-line.width-.5)/gaps.length/model.widthScale);
        for(const gap of gaps){const record=records.get(gap.start);if(record?.end===gap.end)record.node.parentElement.style.wordSpacing=extra+'px';}
      }
    }
    function snap(offset,forward=false) {
      return seek(points,Math.max(0,Math.min(text.length,Number(offset)||0)),forward);
    }
    function point(offset,end=false) {
      offset=snap(offset,end);
      let low=0,high=nodes.length-1;
      while(low<high){const mid=(low+high)>>1;if(end?nodes[mid].end<offset:nodes[mid].end<=offset)low=mid+1;else high=mid;}
      const record=nodes[low];
      return {node:record.node,offset:Math.max(0,Math.min(record.node.length,offset-record.start))};
    }
    function range(start,end=start) {
      const r=document.createRange(),a=point(Math.min(start,end)),b=point(Math.max(start,end),true);
      r.setStart(a.node,a.offset);r.setEnd(b.node,b.offset);return r;
    }
    function rectangles(start,end,inverse=transform().inverse()) {
      return Array.from(range(start,end).getClientRects(),r=>{
        const corners=[[r.left,r.top],[r.right,r.top],[r.right,r.bottom],[r.left,r.bottom]].map(([x,y])=>inverse.transformPoint({x,y}));
        const x=Math.min(...corners.map(p=>p.x)),y=Math.min(...corners.map(p=>p.y));
        return {x,y,width:Math.max(...corners.map(p=>p.x))-x,height:Math.max(...corners.map(p=>p.y))-y};
      })
        .filter(r=>r.height>0);
    }
    function transform() {
      const [o,x,y]=probes.map(node=>node.getBoundingClientRect());
      return new DOMMatrix([(x.x-o.x)/probeDistance,(x.y-o.y)/probeDistance,(y.x-o.x)/probeDistance,(y.y-o.y)/probeDistance,o.x,o.y]);
    }
    async function ready() {
      // Font loading may be asynchronous. A stale readiness callback must not
      // commit measurements for a different document revision.
      const requested=revision;
      // Width-only gestures reuse the same font/content resources. Retain the
      // in-flight/completed batch until content or typography is updated.
      // Failed loads must remain retryable rather than poisoning the surface.
      if(!fontReadiness){
        const batch=Promise.all(model.runs.map(run=>document.fonts.load((run.italic?'italic ':'')+(run.bold?'700 ':'400 ')+model.size+'px '+model.family,run.text)));
        fontReadiness=batch;
        batch.catch(()=>{if(fontReadiness===batch)fontReadiness=null;});
      }
      await fontReadiness;
      await Promise.all([...ownedEmoji.values()].map(entry=>entry.ready));
      await document.fonts.ready;
      const current=!disposed && revision===requested;
      if(current){justify();if(emojiGradient)refreshEmojiPaint();}
      return current;
    }
    function measure() {
      if(disposed)throw new Error('Text surface has been disposed');
      const clusters=segments.map(part=>({start:part.index,end:part.index+part.segment.length,
        text:part.segment,rects:rectangles(part.index,part.index+part.segment.length)}));
      return {revision,text,width:model.width,height:parseFloat(getComputedStyle(root).height),clusters,
        overflow:root.scrollWidth>root.clientWidth+1};
    }
    function hitTest(x,y) {
      const client=transform().transformPoint({x,y}),clientX=client.x,clientY=client.y;
      const hit=document.caretPositionFromPoint?.(clientX,clientY);
      const fallback=!hit&&document.caretRangeFromPoint?.(clientX,clientY);
      const node=hit?.offsetNode||fallback?.startContainer, offset=hit?.offset??fallback?.startOffset;
      const record=nodes.find(record=>record.node===node);
      if(record)return snap(record.start+offset);
      // Outside the text/browser caret API: nearest measured cluster, including
      // RTL visual order. Never derive a character offset from average width.
      let nearest=0,distance=Infinity;
      for(const cluster of measure().clusters)for(const r of cluster.rects){
        const d=Math.hypot(Math.max(r.x-x,0,x-r.x-r.width),Math.max(r.y-y,0,y-r.y-r.height));
        if(d<distance){nearest=cluster.start;distance=d;}
      }
      return nearest;
    }
    function select(start,end=start) {
      const selection=document.getSelection(),backward=start>end;
      const anchor=point(start,backward),focus=point(end,!backward&&start!==end);
      selection.setBaseAndExtent(anchor.node,anchor.offset,focus.node,focus.offset);
    }
    function setWidth(width){
      if(disposed||!Number.isFinite(width)||width<1)return api;
      width=finite(width,model.width,1,20000);
      if(width===model.width)return api;
      model={...model,width};revision++;
      root.style.width=model.width/model.widthScale+'px';
      probeDistance=Math.max(1,Math.min(100,model.width/2,model.size/2));
      probes[1].style.left=probeDistance/model.widthScale+'px';probes[2].style.top=probeDistance+'px';
      justify();
      if(emojiGradient)refreshEmojiPaint();
      return api;
    }
    const api={element:root,update,setWidth,setEmojiOutline,setEmojiGradient,ready,measure,rectangles,hitTest,select,snap,
      dispose(){disposed=true;root.remove();nodes=[];for(const data of ownedEmoji.keys())releaseEmoji(data);ownedEmoji.clear();}};
    return update(input);
  }
  // The browser owns caret movement, selection, bidi and IME. Document changes
  // use styled-run operations, never serialised contenteditable HTML. The host
  // commits value() as one document transaction when the edit session finishes.
  function createEditor(parent,input,{onChange=()=>{},onError=()=>{},onCommit=null,bullet=false,resolveEmojiFonts=null}={}) {
    let model=normalize({...input,bullet}),history=[],future=[],composing=false,compositionBase=null,disposed=false;
    const surface=create(parent,model),root=surface.element,document=root.ownerDocument;
    root.contentEditable='true';root.setAttribute('role','textbox');root.setAttribute('aria-multiline','true');
    root.setAttribute('aria-label','Edit slide text');root.spellcheck=false;
    root.style.userSelect='text';root.style.webkitUserSelect='text';
    root.style.outline='2px solid #eac54f';root.style.caretColor=model.color;
    const plain=value=>value.runs.map(run=>run.text).join('');
    const resolvedEmoji={...model.emojiFonts},emojiRequests=new Set();
    function refreshEmoji() {
      if(disposed||composing||!resolveEmojiFonts)return;
      const missing=[...new Set(Array.from(segmenter.segment(plain(model)),p=>p.segment))].filter(s=>!resolvedEmoji[s]&&!emojiRequests.has(s)&&/\p{Extended_Pictographic}|\p{Regional_Indicator}|\u20e3/u.test(s));
      if(!missing.length)return;
      missing.forEach(s=>emojiRequests.add(s));
      Promise.resolve().then(()=>resolveEmojiFonts(missing.join(' '))).then(fonts=>{
        if(disposed)return;
        for(const s of missing)if(typeof fonts?.[s]==='string'&&fonts[s])resolvedEmoji[s]=fonts[s];
        if(composing)return; // The composition commit will merge the cached fonts.
        const active=document.getSelection(),inside=root.contains(active?.anchorNode)&&root.contains(active?.focusNode),saved=inside?selection():null;
        model=normalize({...model,emojiFonts:{...model.emojiFonts,...resolvedEmoji}});
        surface.update(model);if(saved)surface.select(saved.start,saved.end);onChange(model);
      }).catch(error=>{missing.forEach(s=>emojiRequests.delete(s));if(!disposed)onError(error);});
    }
    function offset(node,index) {
      if(!root.contains(node))throw new Error('Text edit range is outside the editor');
      const range=document.createRange();range.selectNodeContents(root);range.setEnd(node,index);return range.toString().length;
    }
    function selection() {
      const selected=document.getSelection();
      if(!selected||!root.contains(selected.anchorNode)||!root.contains(selected.focusNode))return {start:0,end:0};
      return {start:offset(selected.anchorNode,selected.anchorOffset),end:offset(selected.focusNode,selected.focusOffset)};
    }
    function inputRange(event) {
      const ranges=event.getTargetRanges?.()||[];
      if(!ranges.length)return null;
      if(ranges.length!==1)throw new Error('Multiple text replacement ranges are not supported');
      const range=ranges[0];
      return {start:offset(range.startContainer,range.startOffset),end:offset(range.endContainer,range.endOffset)};
    }
    function restore(next,cursor,record=true,previousSelection=selection()) {
      next=normalize({...next,emojiFonts:{...next.emojiFonts,...resolvedEmoji}});
      if(record&&JSON.stringify(next)!==JSON.stringify(model)){
        history.push({model,selection:previousSelection});if(history.length>100)history.shift();future=[];
      }
      model=next;surface.update(model);surface.select(cursor);onChange(model);refreshEmoji();
    }
    function insert(value,start,end) {
      const selected=selection();start??=selected.start;end??=selected.end;
      const a=boundary(plain(model),Math.min(start,end));
      restore(replace(model,start,end,value),a+value.length);
    }
    function undo(redo=false) {
      if(composing)return;
      const from=redo?future:history,to=redo?history:future,item=from.pop();if(!item)return;
      // Available layout width belongs to the current editing host, not the
      // text history. Undo typography/content without reviving a stale box.
      to.push({model,selection:selection()});model=normalize({...item.model,width:model.width,emojiFonts:{...item.model.emojiFonts,...resolvedEmoji}});surface.update(model);
      surface.select(item.selection.start,item.selection.end);onChange(model);refreshEmoji();
    }
    function lineDeletionRange(text,cursor,type) {
      const backward=/Backward$/.test(type);
      if(type.includes('SoftLine')){
        // Native layout knows wrapped lines and bidi; do not estimate using
        // character widths. Restore selection before recording the transaction.
        const saved=selection(),native=document.getSelection();
        if(typeof native.modify==='function'){
          try {
            native.modify('extend',backward?'backward':'forward','lineboundary');
            const extended=selection();
            return {start:Math.min(extended.start,extended.end),end:Math.max(extended.start,extended.end)};
          }finally{surface.select(saved.start,saved.end);}
        }
      }
      const start=text.slice(0,cursor).lastIndexOf('\n')+1,next=text.indexOf('\n',cursor);
      const end=next<0?text.length:next;
      return {start:backward?start:cursor,end:backward?cursor:end};
    }
    function enter(continuation=false) {
      const s=selection(),text=plain(model),cursor=s.end;
      const start=text.lastIndexOf('\n',cursor-1)+1,next=text.indexOf('\n',cursor),end=next<0?text.length:next;
      if(onCommit&&!continuation&&s.start===s.end&&!text.slice(start,end).trim()){
        // Remove only the empty logical line used to finish editing. Preserve
        // earlier deliberate blank lines and styled content on either side.
        if(start>0&&end===text.length)insert('',start-1,end);
        else if(next>=0)insert('',start,next+1);
        else if(end>start)insert('',start,end);
        Promise.resolve(onCommit()).catch(onError);
      }else insert(continuation&&bullet?'\n  ':'\n');
    }
    function beforeInput(event) {
      if(composing||event.isComposing||/Composition/.test(event.inputType))return;
      event.preventDefault();
      try {
        let target=inputRange(event);
        // Browsers canonicalise a collapsed typing target before a trailing
        // newline even when our explicit caret is after it. Ordinary typing
        // follows that caret; replacements/deletions retain their target range.
        if(event.inputType==='insertText'&&target?.start===target?.end)target=null;
        const selected=target||selection(),text=plain(model),a=Math.min(selected.start,selected.end),b=Math.max(selected.start,selected.end);
        if(['insertText','insertReplacementText'].includes(event.inputType))insert(event.data??event.dataTransfer?.getData('text/plain')??'',a,b);
        else if(['insertParagraph','insertLineBreak'].includes(event.inputType))enter(event.inputType==='insertLineBreak');
        else if(event.inputType==='historyUndo'||event.inputType==='historyRedo')undo(event.inputType==='historyRedo');
        else if(event.inputType.startsWith('delete')){
          let start=a,end=b;
          if(a===b&&!target){
            const backward=/Backward$/.test(event.inputType);
            if(/(?:Soft|Hard)Line(?:Backward|Forward)$/.test(event.inputType)){
              ({start,end}=lineDeletionRange(text,a,event.inputType));
            }else if(/Word/.test(event.inputType)){
              const words=[...new Intl.Segmenter(model.locale||undefined,{granularity:'word'}).segment(text)];
              if(backward)start=words.filter(part=>part.index<a).at(-1)?.index||0;
              else {const word=words.find(part=>part.index+part.segment.length>b);end=word?word.index+word.segment.length:text.length;}
            }else if(event.inputType==='deleteContentBackward')start=boundary(text,Math.max(0,a-1));
            else if(event.inputType==='deleteContentForward')end=boundary(text,Math.min(text.length,b+1),true);
          }
          // A continuation's newline and two source-only indent spaces are
          // one visual boundary. Browser deletion ranges can omit the hidden
          // spaces; never leave them behind as newly visible text.
          if(bullet&&start<end){
            for(const match of text.matchAll(/\n {2}/g)){
              if(start<match.index+3&&end>match.index){start=Math.min(start,match.index);end=Math.max(end,match.index+3);}
            }
          }
          insert('',start,end);
        }
      }catch(error){onError(error);}
    }
    function finishComposition() {
      if(!compositionBase||disposed)return;
      const {model:original,selection:originalSelection}=compositionBase;compositionBase=null;composing=false;
      const previous=plain(original),current=root.textContent,selected=selection();
      // Keep unchanged style runs on both sides of the composed replacement.
      let start=0,tail=0;
      while(start<previous.length&&start<current.length&&previous[start]===current[start])start++;
      start=boundary(previous,start);
      while(tail<previous.length-start&&tail<current.length-start&&previous[previous.length-tail-1]===current[current.length-tail-1])tail++;
      const end=boundary(previous,previous.length-tail,true);tail=previous.length-end;
      model=original;
      try {restore(replace(original,start,end,current.slice(start,current.length-tail)),selected.end,true,originalSelection);}
      catch(error){surface.update(model);surface.select(originalSelection.start,originalSelection.end);onError(error);}
    }
    const listeners={
      beforeinput:beforeInput,
      compositionstart(){if(composing)return;composing=true;compositionBase={model,selection:selection()};},
      compositionend(){queueMicrotask(finishComposition);},
      keydown(event){
        event.stopPropagation();
        if(composing||event.isComposing)return;
        if((event.metaKey||event.ctrlKey)&&['z','y'].includes(event.key.toLowerCase())){
          event.preventDefault();undo(event.key.toLowerCase()==='y'||event.shiftKey);
        }else if(event.key==='Enter'){event.preventDefault();enter(event.shiftKey);}
        // Tab inserts content; Shift-Tab leaves the editing surface so a
        // keyboard user can reach formatting and commit controls.
        else if(event.key==='Tab'&&!event.shiftKey&&!event.metaKey&&!event.ctrlKey&&!event.altKey){event.preventDefault();insert('\t');}
      },
      paste(event){event.preventDefault();if(composing)return;try{insert(event.clipboardData.getData('text/plain'));}catch(error){onError(error);}},
      copy(event){const s=selection();event.preventDefault();event.clipboardData.setData('text/plain',plain(model).slice(Math.min(s.start,s.end),Math.max(s.start,s.end)));},
      cut(event){event.preventDefault();if(composing)return;const s=selection();event.clipboardData.setData('text/plain',plain(model).slice(Math.min(s.start,s.end),Math.max(s.start,s.end)));insert('');},
      drop(event){event.preventDefault();},
    };
    for(const [name,handler]of Object.entries(listeners))root.addEventListener(name,handler);
    return {...surface,value:()=>model,selection,undo:()=>undo(),redo:()=>undo(true),
      isComposing:()=>composing,
      setLayoutWidth(width){
        if(disposed||composing||!Number.isFinite(width)||width<1)return;
        width=finite(width,model.width,1,20000);
        if(width===model.width)return;
        model={...model,width};surface.setWidth(width);
      },
      setFont(fontId,family){if(composing||disposed)return;const s=selection();restore({...model,fontId,family},s.end);surface.select(s.start,s.end);},
      setLineHeight(lineHeight){if(composing||disposed)return;const s=selection();restore({...model,lineHeight},s.end);surface.select(s.start,s.end);},
      setParagraphSpacing(paragraphBefore,paragraphAfter){if(composing||disposed)return;const s=selection();restore({...model,paragraphBefore,paragraphAfter},s.end);surface.select(s.start,s.end);},
      setSize(size){if(composing||disposed||!Number.isFinite(size)||size<1||size>1024)return;const s=selection();restore({...model,size},s.end);surface.select(s.start,s.end);},
      setWidthScale(widthScale){if(composing||disposed||!Number.isFinite(widthScale)||widthScale<.004167||widthScale>2)return;const s=selection();restore({...model,widthScale},s.end);surface.select(s.start,s.end);},
      format(style){if(composing||disposed)return;const s=selection();restore(format(model,s.start,s.end,style),Math.max(s.start,s.end));surface.select(s.start,s.end);},
      dispose(){disposed=true;for(const [name,handler]of Object.entries(listeners))root.removeEventListener(name,handler);surface.dispose();}};
  }
  return Object.freeze({create,createEditor,normalize,replace,format});
})();
