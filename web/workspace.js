/* Shared Mac/Web workspace. This module owns chrome, never document content. */
globalThis.KeynopeWorkspace = (() => {
  if(typeof window!=='undefined'&&typeof document!=='undefined'&&document.documentElement?.dataset&&window.dispatchEvent){
  const appearanceKey='keynope.interface.appearance';
  window.keynopeSetInterfaceAppearance=(mode,syncNative=true)=>{
    if(!['system','light','dark'].includes(mode))return false;
    document.documentElement.dataset.keynopeInterfaceAppearance=mode;
    window.KEYNOPE_INTERFACE_APPEARANCE=mode;
    try{localStorage.setItem(appearanceKey,mode);}catch(_error){}
    if(syncNative)window.webkit?.messageHandlers?.keynopePresenter?.postMessage({action:'interface-appearance',mode});
    window.dispatchEvent(new CustomEvent('keynope-interface-appearance',{detail:mode}));return true;
  };
  let initialAppearance=window.KEYNOPE_INTERFACE_APPEARANCE;
  try{initialAppearance ||= localStorage.getItem(appearanceKey);}catch(_error){}
  window.keynopeSetInterfaceAppearance(['system','light','dark'].includes(initialAppearance)?initialAppearance:'system',false);
  }
  // Setting an unchanged DOM property can still rewrite its attribute/text
  // nodes and notify observers. Keep retained chrome genuinely incremental.
  function setChromeProperty(node,property,value) {
    if(node[property]!==value)node[property]=value;
  }
  // Keep controls responsive while commands are in flight. Coalesce repeated
  // edits per property, but never retarget a delayed edit to a new selection.
  function selectionPaintWriter({target,write,error=()=>{},changed=()=>{}}){
    const pending=new Map();let active=null,running=null;
    const same=(a,b)=>!!a&&!!b&&['epoch','owner','slide','master','group'].every(key=>a[key]===b[key])&&Array.isArray(a.targetIds)&&Array.isArray(b.targetIds)&&a.targetIds.length===b.targetIds.length&&a.targetIds.every((id,index)=>id===b.targetIds[index]);
    const busy=property=>active?.property===property||pending.has(property);
    async function drain(){
      while(pending.size){
        const [property,job]=pending.entries().next().value;pending.delete(property);
        const current=target();if(!same(job.target,current))continue;
        active=job;changed();
        try{await write(property,job.value,current);}
        catch(reason){if(same(job.target,target()))error(reason);}
        finally{active=null;changed();}
      }
    }
    function enqueue(property,value,expected=target()){
      const current=target();if(!expected?.targetIds?.length||!same(expected,current))return Promise.resolve();
      // Explicit captures (e.g. an open colour picker) must still be current.
      if(expected.revision!==current.revision){error(Error('The document changed; pick the colour or style again.'));return Promise.resolve();}
      const queued=pending.get(property);
      if(queued&&same(queued.target,expected)&&Object.is(queued.value,value)||!queued&&active?.property===property&&same(active.target,expected)&&Object.is(active.value,value))return running;
      pending.set(property,{property,value,target:{...expected,targetIds:[...expected.targetIds]}});changed();
      if(!running)running=Promise.resolve().then(drain).finally(()=>{running=null;});
      return running;
    }
    return {enqueue,busy};
  }
  function applyScenePagePatches(pages,previous) {
    return pages.map(page=>{
      if(!page.scenePatch)return page;
      const patch=page.scenePatch,old=previous.find(p=>p.slide===page.slide&&p.page===page.page)?.scene;
      if(!old||patch.version!==1||!patch.slideId||old.slideId!==patch.slideId||!Array.isArray(patch.order)||patch.order.length>10000||!Array.isArray(patch.changed)||!Array.isArray(patch.removed))throw Error('Invalid workspace scene patch');
      const objects=new Map();for(const object of old.objects){if(!object.id||objects.has(object.id))throw Error('Ambiguous workspace object');objects.set(object.id,object);}
      const removed=new Set();for(const id of patch.removed){if(removed.has(id)||!objects.has(id))throw Error('Invalid workspace removal');removed.add(id);objects.delete(id);}
      const changed=new Set();for(const object of patch.changed){if(!object?.id||changed.has(object.id)||removed.has(object.id))throw Error('Invalid workspace replacement');changed.add(object.id);objects.set(object.id,object);}
      const order=new Set(patch.order);if(order.size!==patch.order.length||objects.size!==order.size||patch.order.some(id=>!objects.has(id)))throw Error('Incomplete workspace object order');
      const metadata=patch.metadata||old;
      if(metadata.version!==1||metadata.slideId!==patch.slideId||!Number.isFinite(metadata.width)||metadata.width<=0||!Number.isFinite(metadata.height)||metadata.height<=0)throw Error('Invalid workspace scene metadata');
      const {scenePatch,...result}=page;
      return {...result,scene:{...metadata,objects:patch.order.map(id=>objects.get(id))}};
    });
  }
  function alignmentMoves(units, alignment, vertical, reference) {
    const low=vertical?'minY':'minX',high=vertical?'maxY':'maxX';
    const leading=vertical?'top':'left',trailing=vertical?'bottom':'right';
    return units.map(unit=>{
      const b=unit.bounds;
      const delta=alignment===leading?reference[low]-b[low]:alignment===trailing?reference[high]-b[high]:(reference[low]+reference[high]-b[low]-b[high])/2;
      return {id:unit.id,dx:vertical?0:delta,dy:vertical?delta:0};
    });
  }
  function connectorIndex(ports,routes) {
    const byShape=new Map(),byRoute=new Map();
    for(const port of ports){
      let sides=byShape.get(port.id);if(!sides){sides=new Map();byShape.set(port.id,sides);}
      if(!sides.has(port.side))sides.set(port.side,port);
    }
    for(const route of routes)if(!byRoute.has(route.id))byRoute.set(route.id,route);
    return {port:(id,side)=>byShape.get(id)?.get(side),route:id=>byRoute.get(id)};
  }
  function cellAspect(cols,rows,canvas) {
    const width=canvas?.width??1920,height=canvas?.height??1080;
    if(![width,height,cols,rows].every(Number.isFinite)||width<=0||height<=0||cols<=0||rows<=0)throw Error('Invalid physical canvas geometry.');
    return (height/rows)/(width/cols);
  }
  function clampMove(boxes,dx,dy,cols,rows,step=1,canvas) {
    if(!boxes.length)return {dx:0,dy:0};
    // Include local bounds because legacy placement still requires a local
    // origin on the slide. Every member receives the same captured delta.
    const extents=boxes.flatMap(b=>[b,visualBounds(b,cols,rows,canvas)]);
    const axis=(delta,lo,hi,limit)=>{
      const start=Math.min(...extents.map(b=>b[lo])),end=Math.max(...extents.map(b=>b[hi]));
      const lower=Math.min(0,step?Math.ceil((-start-1e-9)/step)*step:-start),upper=Math.max(0,step?Math.floor((limit-end+1e-9)/step)*step:limit-end);
      return Math.max(lower,Math.min(upper,step?Math.round(delta/step)*step:delta));
    };
    return {dx:axis(dx,'minX','maxX',cols),dy:axis(dy,'minY','maxY',rows)};
  }
  function visualBounds(b,cols,rows,canvas) {
    if(!b.rotation)return {minX:b.minX,minY:b.minY,maxX:b.maxX,maxY:b.maxY};
    const angle=b.rotation*Math.PI/180,aspect=cellAspect(cols,rows,canvas);
    const width=b.maxX-b.minX,height=b.maxY-b.minY;
    const w=Math.abs(Math.cos(angle))*width+Math.abs(Math.sin(angle))*height*aspect;
    const h=Math.abs(Math.sin(angle))*width/aspect+Math.abs(Math.cos(angle))*height;
    const cx=(b.minX+b.maxX)/2,cy=(b.minY+b.maxY)/2;
    return {minX:cx-w/2,minY:cy-h/2,maxX:cx+w/2,maxY:cy+h/2};
  }
  // Rigid rotation in physical slide space, not the non-square cell grid.
  // Return local boxes plus angles; never resize contents or round positions.
  // The caller keeps the pivot throughout a gesture (and its inverse/undo).
  function rotateSelection(items,degrees,cols,rows,pivot,canvas) {
    if(!Number.isFinite(degrees)||!Number.isFinite(cols)||!Number.isFinite(rows)||cols<=0||rows<=0)throw Error('Invalid rotation geometry.');
    if(!items.length)return {pivot:pivot||null,items:[]};
    for(const item of items){
      const b=item.bounds;
      if(!b||![b.minX,b.minY,b.maxX,b.maxY,b.rotation||0].every(Number.isFinite)||b.maxX<b.minX||b.maxY<b.minY)throw Error('Selection has invalid bounds.');
    }
    if(!pivot){
      const boxes=items.map(item=>visualBounds(item.bounds,cols,rows,canvas));
      pivot={x:(Math.min(...boxes.map(b=>b.minX))+Math.max(...boxes.map(b=>b.maxX)))/2,y:(Math.min(...boxes.map(b=>b.minY))+Math.max(...boxes.map(b=>b.maxY)))/2};
    }
    if(!Number.isFinite(pivot.x)||!Number.isFinite(pivot.y))throw Error('Invalid rotation pivot.');
    const aspect=cellAspect(cols,rows,canvas),angle=degrees*Math.PI/180,c=Math.cos(angle),s=Math.sin(angle);
    return {pivot:{...pivot},items:items.map(item=>{
      const b=item.bounds,w=b.maxX-b.minX,h=b.maxY-b.minY;
      const x=(b.minX+b.maxX)/2-pivot.x,y=((b.minY+b.maxY)/2-pivot.y)*aspect;
      const cx=pivot.x+x*c-y*s,cy=pivot.y+(x*s+y*c)/aspect;
      return {...item,bounds:{...b,minX:cx-w/2,minY:cy-h/2,maxX:cx+w/2,maxY:cy+h/2,rotation:((b.rotation||0)+degrees)%360}};
    })};
  }
  // Proportional group resizing in shared scene space. Uniform scaling keeps
  // rotated rectangles rectangular (nonuniform world scaling would shear
  // them). Text metrics and crop coordinates belong to the caller's content,
  // not this geometry operation. Keep the pivot for an entire gesture.
  function resizeSelection(items,factor,cols,rows,pivot,canvas) {
    if(!Number.isFinite(factor)||factor<=0)throw Error('Invalid group scale.');
    // Reuse rotation's validation and default visual-union pivot.
    const validated=rotateSelection(items,0,cols,rows,pivot,canvas);
    if(!items.length)return validated;
    const anchor=validated.pivot;
    return {pivot:anchor,items:items.map(item=>{
      const b=item.bounds;
      const bounds={...b,
        minX:anchor.x+(b.minX-anchor.x)*factor,
        minY:anchor.y+(b.minY-anchor.y)*factor,
        maxX:anchor.x+(b.maxX-anchor.x)*factor,
        maxY:anchor.y+(b.maxY-anchor.y)*factor};
      if(![bounds.minX,bounds.minY,bounds.maxX,bounds.maxY].every(Number.isFinite))throw Error('Group scale exceeds supported geometry.');
      return {...item,bounds};
    })};
  }
  // Attached connectors derive their outer geometry from their shapes. Only
  // user-positioned elbow tracks are authored coordinates to scale; stroke
  // and arrow widths are paint and must remain unchanged.
  function resizeConnectedSelection(items,factor,cols,rows,pivot,canvas) {
    const members=items.filter(item=>item.element?.kind!=='connector');
    const ids=new Set(members.filter(item=>item.element?.kind==='shape').map(item=>item.element.id));
    const connectors=items.filter(item=>item.element?.kind==='connector');
    for(const item of connectors){
      const q=new URLSearchParams(item.element.query||'');
      if(!ids.has(q.get('connector-from'))||!ids.has(q.get('connector-to')))throw Error('Select both attached shapes to resize this connector.');
    }
    const resized=resizeSelection(members,factor,cols,rows,pivot,canvas);
    const attached=connectors.map(item=>{
      const q=new URLSearchParams(item.element.query||''),raw=q.get('connector-route');
      if(!raw)return {...item};
      let points;try{points=JSON.parse(raw);}catch{throw Error('Connector has an invalid manual route.');}
      if(!Array.isArray(points)||points.length<4||points.length>128||points.some((p,i)=>!p||!Number.isFinite(p.x)||!Number.isFinite(p.y)||(i&&p.x!==points[i-1].x&&p.y!==points[i-1].y)))throw Error('Connector has an invalid manual route.');
      points=points.map(p=>({x:resized.pivot.x+(p.x-resized.pivot.x)*factor,y:resized.pivot.y+(p.y-resized.pivot.y)*factor}));
      if(points.some(p=>!Number.isFinite(p.x)||!Number.isFinite(p.y)||Math.abs(p.x)>10000||Math.abs(p.y)>10000))throw Error('Connector route exceeds supported geometry.');
      q.set('connector-route',JSON.stringify(points));
      return {...item,element:{...item.element,query:q.toString()}};
    });
    return {...resized,connectors:attached};
  }

  // Linear-time lookup for authoring overlays. Keep the first authored ID,
  // matching legacy findIndex behaviour even for malformed duplicate IDs.
  function sceneSelectionBounds(scene,elements,cols,rows) {
    const indices=new Map(),bounds=new Map();
    elements.forEach((element,index)=>{if(!indices.has(element.id))indices.set(element.id,index);});
    const sx=cols/scene.width,sy=rows/scene.height;
    for(const object of scene.objects){
      if(object.kind==='connector')continue;
      const index=indices.get(object.id);if(index===undefined)continue;
      const b=object.bounds;
      bounds.set(index,{minX:b.x*sx,minY:b.y*sy,maxX:(b.x+b.width)*sx,maxY:(b.y+b.height)*sy,rotation:object.rotation||0,color:object.paint?.color||''});
    }
    return bounds;
  }
  function toolbarKeyboard(toolbar) {
    let remembered=null;
    const originalTabs=new Map();
    const available=()=>[...toolbar.querySelectorAll('button')].filter(button=>!button.disabled&&button.getAttribute('aria-disabled')!=='true'&&button.getClientRects().length&&getComputedStyle(button).visibility!=='hidden');
    const restore=button=>{const value=originalTabs.get(button);if(value===null)button.removeAttribute('tabindex');else button.setAttribute('tabindex',value);originalTabs.delete(button);};
    const refresh=()=>{
      const buttons=available();
      if(!buttons.includes(remembered))remembered=buttons[0]||null;
      for(const button of originalTabs.keys())if(!toolbar.contains(button))restore(button);
      for(const button of toolbar.querySelectorAll('button')){
        if(!originalTabs.has(button))originalTabs.set(button,button.getAttribute('tabindex'));
        button.tabIndex=button===remembered?0:-1;
      }
    };
    const focusin=event=>{const button=event.target.closest('button');if(available().includes(button)){remembered=button;refresh();}};
    const keydown=event=>{
      if(event.altKey||event.ctrlKey||event.metaKey||event.shiftKey||event.isComposing)return;
      const current=event.target.closest('button');
      if(!current||!toolbar.contains(current)||!['ArrowLeft','ArrowRight','Home','End'].includes(event.key))return;
      const buttons=available();
      const index=buttons.indexOf(current);if(index<0)return;
      event.preventDefault();event.stopPropagation();
      const rtl=getComputedStyle(toolbar).direction==='rtl';
      const delta=(event.key==='ArrowRight'?1:-1)*(rtl?-1:1);
      const next=event.key==='Home'?0:event.key==='End'?buttons.length-1:(index+delta+buttons.length)%buttons.length;
      buttons[next].focus();
    };
    toolbar.addEventListener('keydown',keydown);
    toolbar.addEventListener('focusin',focusin);
    const mutations=new MutationObserver(refresh);mutations.observe(toolbar,{subtree:true,childList:true,attributes:true,attributeFilter:['disabled','aria-disabled','hidden','style','class']});
    const resize=new ResizeObserver(refresh);resize.observe(toolbar);
    window.addEventListener('resize',refresh);refresh();
    return ()=>{mutations.disconnect();resize.disconnect();window.removeEventListener('resize',refresh);toolbar.removeEventListener('keydown',keydown);toolbar.removeEventListener('focusin',focusin);for(const button of originalTabs.keys())restore(button);};
  }
  function slideKeyboard(panel,{isMaster,action,status}) {
    let pending=false;
    const items=()=>[...panel.querySelectorAll('.keynope-slide-item[data-master-index]')];
    async function keydown(event){
      const button=event.target.closest('.keynope-slide-item[data-master-index]');
      if(!button||!panel.contains(button))return;
      // Navigator keystrokes must never nudge/delete canvas objects.
      event.stopPropagation();
      if(!['ArrowUp','ArrowDown','Home','End'].includes(event.key))return;
      event.preventDefault();if(pending)return;
      const all=items(),index=all.indexOf(button),delta=event.key==='ArrowUp'?-1:1;
      if(!event.altKey){
        all[event.key==='Home'?0:event.key==='End'?all.length-1:Math.max(0,Math.min(all.length-1,index+delta))]?.focus();return;
      }
      if(!event.key.startsWith('Arrow'))return;
      const master=isMaster(),source=Number(button.dataset.masterIndex),tab=button.dataset.slideSection==='tabs';
      if(master&&source===0){status('Base Master stays first.');return;}
      const section=all.filter(node=>node.dataset.slideSection===button.dataset.slideSection&&(!master||Number(node.dataset.masterIndex)>0));
      const at=section.indexOf(button),to=at+delta;
      if(to<0||to>=section.length){status(delta<0?'Already first in this section.':'Already last in this section.');return;}
      const destination=Number(section[to].dataset.masterIndex),focusIndex=tab?source:destination;
      pending=true;panel.setAttribute('aria-busy','true');
      try{
        await action({action:master?'reorder-master':tab?'reorder-slide-tab':'reorder-slide',slide:source,value:tab?to:destination});
        items().find(node=>Number(node.dataset.masterIndex)===focusIndex)?.focus();
        status('Moved '+(master?'master':tab?'tab':'slide')+' '+(delta<0?'up.':'down.'));
      }catch(error){status(error.message||'Could not reorder slide.');}
      finally{pending=false;panel.removeAttribute('aria-busy');}
    }
    panel.addEventListener('keydown',keydown);
    return ()=>panel.removeEventListener('keydown',keydown);
  }
  // Pointer movement is continuous in slide coordinates. Only the initial
  // screen-space dead zone distinguishes a click from a drag; once engaged,
  // returning near the origin must not create a discontinuity.
  function pointerMove(dx,dy,width,height,cols,rows,engaged=false) {
    if(![dx,dy,width,height,cols,rows].every(Number.isFinite)||width<=0||height<=0||cols<=0||rows<=0)return {dx:0,dy:0,engaged:false};
    engaged=engaged||Math.hypot(dx,dy)>=3;
    return {dx:engaged?dx*cols/width:0,dy:engaged?dy*rows/height:0,engaged};
  }
  // Capture group membership and editable boxes once per marquee gesture.
  // A collapsed group's empty union space is not a selectable member.
  function marqueeSelectionUnits(elements,bounds,indices,editingGroup='') {
    const queries=elements.map(element=>new URLSearchParams(element.query||'')),groups=new Map();
    queries.forEach((q,index)=>{const id=q.get('group');if(id){if(!groups.has(id))groups.set(id,[]);groups.get(id).push(index);}});
    return indices.flatMap(index=>{
      const q=queries[index];if(!q)return [];
      const group=q.get('group'),members=group&&group!==editingGroup?groups.get(group):[index];
      const boxes=members.filter(i=>queries[i].get('object-hidden')!=='1'&&queries[i].get('object-locked')!=='1'&&bounds.has(i)).map(i=>({...bounds.get(i)}));
      return boxes.length?[{index,members:[...members],boxes}]:[];
    });
  }
  // Separating-axis test in physical space. Cell height and width need not
  // match; using logical-grid rotation would distort diagonal hit geometry.
  function boxIntersectsRect(box,area,aspect=1) {
    if(!box||!area||![box.minX,box.minY,box.maxX,box.maxY,box.rotation||0,area.minX,area.minY,area.maxX,area.maxY,aspect].every(Number.isFinite)||aspect<=0)return false;
    if(box.maxX<=box.minX||box.maxY<=box.minY||area.maxX<=area.minX||area.maxY<=area.minY)return false;
    const angle=(box.rotation||0)*Math.PI/180,c=Math.cos(angle),s=Math.sin(angle);
    const w=(box.maxX-box.minX)/2,h=(box.maxY-box.minY)*aspect/2;
    const rw=(area.maxX-area.minX)/2,rh=(area.maxY-area.minY)*aspect/2;
    const dx=(box.minX+box.maxX-area.minX-area.maxX)/2,dy=(box.minY+box.maxY-area.minY-area.maxY)*aspect/2;
    for(const [x,y] of [[1,0],[0,1],[c,s],[-s,c]]){
      const radius=w*Math.abs(x*c+y*s)+h*Math.abs(-x*s+y*c)+rw*Math.abs(x)+rh*Math.abs(y);
      if(Math.abs(dx*x+dy*y)>=radius-1e-10)return false;
    }
    return true;
  }
  function marqueeCandidates(units,area,aspect,initial=[]) {
    const result=new Set(initial);
    for(const unit of units)if(unit.boxes.some(box=>boxIntersectsRect(box,area,aspect)))for(const index of unit.members)result.add(index);
    return result;
  }
  function sameLayerContext(a,b) {
    if(!a||!b)return !a&&!b;
    return ['epoch','owner','slide','master','group','mutationRevision'].every(key=>a[key]===b[key]);
  }
  function snapMove(bounds,targets,dx,dy,toleranceX,toleranceY,step=1) {
    const result={dx,dy,guides:[]};
    const quantize=value=>step>0?Math.round(value/step)*step:value;
    for(const [axis,lo,hi,delta,tolerance] of [['x','minX','maxX',dx,toleranceX],['y','minY','maxY',dy,toleranceY]]){
      const anchors=[bounds[lo],(bounds[lo]+bounds[hi])/2,bounds[hi]];
      let best=null;
      for(const target of targets)for(const at of [target[lo],(target[lo]+target[hi])/2,target[hi]])for(const anchor of anchors){
        const correction=at-anchor-delta,snapped=quantize(at-anchor);
        if(Math.abs(anchor+snapped-at)>0.00001||Math.abs(correction)>tolerance)continue;
        if(!best||Math.abs(correction)<best.distance)best={distance:Math.abs(correction),delta:snapped,at};
      }
      // Only compare neighbours in the same visual row/column. The slide
      // frame is an alignment target, not an object to space against.
      const crossLo=axis==='x'?'minY':'minX',crossHi=axis==='x'?'maxY':'maxX',crossDelta=axis==='x'?dy:dx;
      const peers=targets.filter(t=>t.spacing!==false&&Math.min(t[crossHi],bounds[crossHi]+crossDelta)>Math.max(t[crossLo],bounds[crossLo]+crossDelta));
      const before=peers.filter(t=>t[hi]<=bounds[lo]+delta+tolerance).sort((a,b)=>b[hi]-a[hi])[0];
      const after=peers.filter(t=>t[lo]>=bounds[hi]+delta-tolerance).sort((a,b)=>a[lo]-b[lo])[0];
      if(before&&after&&before!==after){
        const width=bounds[hi]-bounds[lo],gap=(after[lo]-before[hi]-width)/2;
        const desired=before[hi]+gap-bounds[lo],snapped=quantize(desired),distance=Math.abs(desired-delta);
        if(gap>0&&distance<=tolerance&&Math.abs(desired-snapped)<.00001&&(!best||distance<best.distance)){
          best={delta:snapped,distance,spacing:true,segments:[[before[hi],before[hi]+gap],[after[lo]-gap,after[lo]]],cross:(bounds[crossLo]+bounds[crossHi])/2+crossDelta};
        }
      }
      if(best){result[axis==='x'?'dx':'dy']=best.delta;result.guides.push(best.spacing?{axis,spacing:true,segments:best.segments,cross:best.cross}:{axis,at:best.at});}
    }
    return result;
  }
  // Equal gaps also work for differently sized objects. Collapsed groups are units.
  function matchSize(units,referenceID,axis,cols,rows) {
    if(!['width','height','both'].includes(axis)||units.length<2)return [];
    const reference=units.find(unit=>unit.id===referenceID);
    if(!reference)throw Error('Select a reference object first.');
    const width=reference.bounds.maxX-reference.bounds.minX,height=reference.bounds.maxY-reference.bounds.minY;
    return units.filter(unit=>unit.id!==referenceID).map(unit=>{
      const box={...unit.bounds};
      if(axis!=='height')box.maxX=box.minX+width;
      if(axis!=='width')box.maxY=box.minY+height;
      const step=unit.step||1,w=box.maxX-box.minX,h=box.maxY-box.minY;
      if(!Object.values(box).every(Number.isFinite)||w<=0||h<=0)throw Error('Selection has invalid bounds.');
      if((axis!=='height'&&Math.abs(w/step-Math.round(w/step))>.00001)||(axis!=='width'&&Math.abs(h/step-Math.round(h/step))>.00001))throw Error('These objects use different sizing grids; choose a whole-cell reference size.');
      if(box.minX<0||box.minY<0||box.maxX>cols||box.maxY>rows)throw Error('The matched size would extend beyond the slide. Move the objects inward first.');
      return {id:unit.id,bounds:box};
    });
  }
  function distribute(units,axis) {
    if(!['horizontal','vertical'].includes(axis)||units.length<3)return [];
    const lo=axis==='horizontal'?'minX':'minY',hi=axis==='horizontal'?'maxX':'maxY';
    if(units.some(unit=>!Number.isFinite(unit.bounds?.[lo])||!Number.isFinite(unit.bounds?.[hi])||unit.bounds[hi]<unit.bounds[lo]))throw Error('Selection has invalid bounds');
    const sorted=units.map((unit,index)=>({...unit,index})).sort((a,b)=>a.bounds[lo]-b.bounds[lo]||a.index-b.index);
    const start=sorted[0].bounds[lo],end=sorted.at(-1).bounds[hi];
    const total=sorted.reduce((sum,unit)=>sum+unit.bounds[hi]-unit.bounds[lo],0),gap=(end-start-total)/(sorted.length-1);
    let position=start;
    return sorted.map((unit,index)=>{const delta=index===0||index===sorted.length-1?0:position-unit.bounds[lo];position+=unit.bounds[hi]-unit.bounds[lo]+gap;return {id:unit.id,dx:axis==='horizontal'?delta:0,dy:axis==='vertical'?delta:0};});
  }
  function thumbnails(panel,paint,keyForIndex=()=>null) {
    let disposed=false;
    const entries=new Map();
    function busy(host,entry,value){entry.loading=value;host.setAttribute('aria-busy',String(value));}
    function clear(host,entry){entry.token++;busy(host,entry,false);entry.cleanup?.();entry.cleanup=null;host.replaceChildren();}
    function render(host,entry){
      if(disposed||!entry.visible||entry.loading||entry.cleanup)return;
      const token=++entry.token;busy(host,entry,true);
      // Give each asynchronous paint its own surface. A superseded paint can
      // finish, but cannot insert stale nodes into the current preview.
      const surface=document.createElement('div');surface.style.cssText='position:absolute;inset:0';host.replaceChildren(surface);
      Promise.resolve().then(()=>paint(surface,entry.index)).then(cleanup=>{
        if(disposed||token!==entry.token){cleanup?.();surface.remove();return;}
        busy(host,entry,false);entry.cleanup=()=>{cleanup?.();surface.remove();};
        entry.cleanup.update=cleanup?.update;
      }).catch(()=>{if(!disposed&&token===entry.token){busy(host,entry,false);surface.textContent='Preview unavailable';}});
    }
    const observer=new IntersectionObserver(changes=>{
      for(const change of changes){const entry=entries.get(change.target);if(!entry)continue;
        entry.visible=change.isIntersecting;
        if(!entry.visible){clear(change.target,entry);continue;}
        render(change.target,entry);
      }
    },{root:panel,rootMargin:'100px 0px'});
    return {
      add(button,index){const host=document.createElement('div');host.className='keynope-slide-thumbnail';host.setAttribute('aria-hidden','true');button.prepend(host);entries.set(host,{index,key:keyForIndex(index),visible:false,token:0,loading:false,cleanup:null});observer.observe(host);},
      update(nextPaint,nextKey,updateRetained){paint=nextPaint;keyForIndex=nextKey;for(const [host,entry] of entries){const key=keyForIndex(entry.index);if(key===entry.key)continue;entry.key=key;
        // Retain compatible visible previews while an asynchronous master
        // request is pending. The guarded updater cannot paint a stale result.
        // Hidden previews retain no scene resources; pending initial paints
        // still have their own isolated surface and cleanup.
        if(entry.visible&&!entry.loading&&entry.cleanup?.update){
          const token=++entry.token,cleanup=entry.cleanup;
          const current=()=>!disposed&&entry.visible&&entry.token===token&&entry.cleanup===cleanup;
          const update=(...args)=>current()&&cleanup.update(...args);
          try{
            const result=updateRetained?.(update,entry.index);
            if(result===true)continue;
            if(result&&typeof result.then==='function'){
              busy(host,entry,true);
              Promise.resolve(result).then(retained=>{
                if(!current())return;busy(host,entry,false);
                if(retained!==true){clear(host,entry);render(host,entry);}
              }).catch(()=>{if(current()){clear(host,entry);render(host,entry);}});
              continue;
            }
          }catch(_error){}
        }
        clear(host,entry);render(host,entry);}},
      dispose(){disposed=true;observer.disconnect();for(const [host,entry] of entries)clear(host,entry);entries.clear();}
    };
  }
  function mount({topbar, header, quick, tabs, main, selection, panels, addSlide, resize, canUseSlidePresets, appearance, selectionCapabilities, selectionPaintTarget, setSelectionPaint, objects,selectObject,setObjectOpacity,moveObjectLayer,setObjectVisibility,setObjectLock,setObjectName,groupScope,groupAction,setTheme,getTheme,themePresets,setDeckTheme,themeDraftContext,hasDeckTheme,detachDeckTheme,pickColor,addPreset,setImageDescription,editBuilds}) {
    const root = document.documentElement;
    root.dataset.keynopeWorkspace = 'inspector';
    const toolbar = document.createElement('div');
    toolbar.className = 'keynope-workspace-actions';
    toolbar.setAttribute('role', 'toolbar');
    toolbar.setAttribute('aria-label', 'Workspace');
    toolbarKeyboard(toolbar);
    const inspector = document.createElement('aside');
    inspector.className = 'keynope-workspace-inspector';
    inspector.id = 'keynope-format-inspector';
    inspector.setAttribute('aria-label', 'Format inspector');
    const inspectorScrim=document.createElement('div');inspectorScrim.className='keynope-inspector-scrim';inspectorScrim.hidden=true;inspectorScrim.setAttribute('aria-hidden','true');topbar.append(inspectorScrim);
    const title = document.createElement('h2');
    const inspectorClose=document.createElement('button');inspectorClose.type='button';inspectorClose.className='keynope-inspector-close';inspectorClose.textContent='×';inspectorClose.setAttribute('aria-label','Close Format inspector');inspectorClose.title='Close Format inspector';
    const hint = document.createElement('p');
    hint.className = 'keynope-workspace-hint';
    inspector.append(title,inspectorClose,tabs,hint,selection,panels.slide);
    const scopeBar=document.createElement('nav');scopeBar.className='keynope-group-scope';scopeBar.setAttribute('aria-label','Object editing scope');
    const scopeExit=document.createElement('button');scopeExit.type='button';scopeExit.textContent='All objects';scopeExit.title='Finish editing this group';
    const scopeLabel=document.createElement('span');scopeLabel.setAttribute('aria-current','location');scopeBar.append(scopeExit,scopeLabel);inspector.insertBefore(scopeBar,tabs);
    let changingGroup=false;
    async function performGroupAction(action){if(editing||changingGroup||!groupAction)return;changingGroup=true;refresh();try{await groupAction(action);}catch(error){hint.textContent=error.message||'Could not change group';}finally{changingGroup=false;refresh();}}
    scopeExit.onclick=()=>performGroupAction('exit-group');
    const opacityLabel=document.createElement('label'),opacityInput=document.createElement('input');opacityLabel.textContent='Opacity (%) ';opacityInput.type='number';opacityInput.min='0';opacityInput.max='100';opacityInput.step='1';opacityInput.setAttribute('aria-label','Object opacity');opacityInput.style.width='75px';opacityLabel.append(opacityInput);inspector.insertBefore(opacityLabel,selection);let savingOpacity=false;
    opacityInput.addEventListener('keydown',event=>event.stopPropagation());
    opacityInput.onchange=async()=>{if(!opacityInput.checkValidity()||opacityInput.value===''){opacityInput.reportValidity();return;}savingOpacity=true;opacityInput.disabled=true;try{await setObjectOpacity(Number(opacityInput.value)/100);}catch(error){hint.textContent=error.message||'Could not change opacity';}finally{savingOpacity=false;refresh();}};
    // Keep shared controls outside the contextual panel: renderEditorTopbar
    // replaces that panel's children when the authored selection changes.
    const paintPanel=document.createElement('section');paintPanel.className='keynope-selection-paint';paintPanel.setAttribute('aria-label','Selection paint');inspector.insertBefore(paintPanel,selection);
    const paintError=document.createElement('p');paintError.className='keynope-theme-error';paintError.setAttribute('role','alert');paintError.hidden=true;
    let paintOwnerKey='';
    const paintFields=[];
    const paintSection=(label,collapsible=false)=>{const node=document.createElement(collapsible?'details':'fieldset'),heading=document.createElement(collapsible?'summary':'legend');heading.textContent=label;node.append(heading);paintPanel.append(node);return node;};
    const basePaint=paintSection('Paint'),strokePaint=paintSection('Shape stroke'),linePaint=paintSection('Lines'),gradientPaint=paintSection('Gradient',true),shadowPaint=paintSection('Shadow',true),colourEffects=paintSection('Colour effects',true),blurEffects=paintSection('Blur',true),glowEffects=paintSection('Glow',true);
    const paintWriter=selectionPaintWriter({target:()=>selectionPaintTarget?.(),write:(property,value,target)=>{paintError.hidden=true;return setSelectionPaint(property,value,target);},error:error=>{paintError.textContent=error.message||'Could not change paint';paintError.hidden=false;},changed:()=>refresh()});
    function commitPaint(property,value,target=selectionPaintTarget?.()){
      paintError.hidden=true;return paintWriter.enqueue(property,value,target);
    }
    function paintField(parent,property,label,type,choices){
      const row=document.createElement('label'),name=document.createElement('span'),subset=document.createElement('small');row.className='keynope-paint-field';name.textContent=property.startsWith('label.')?label.replace(/^Label /,''):label;name.append(subset);row.append(name);
      if(property==='label.font')row.classList.add('keynope-label-font-field');
      if(property==='line.path'||property==='line.arrows'||property==='line.routing')row.classList.add('keynope-line-choice-field');
      if(['paint.colorToken','paint.strokeToken','label.colorToken'].includes(property))row.classList.add('keynope-paint-role-field');
      if(property==='paint.strokeColor')row.classList.add('keynope-paint-field-stroke');
      const control=document.createElement(type==='color'?'button':type==='select'?'select':'input');control.dataset.selectionProperty=property;control.setAttribute('aria-label','Selection '+label.toLowerCase());
      if(type==='color'){
        control.type='button';control.className='keynope-paint-color';
        control.onclick=()=>{const state=selectionCapabilities?.()?.properties?.[property],target=selectionPaintTarget?.();if(!state?.editable)return;pickColor?.(control,state.state==='value'&&String(state.value).startsWith('#')?state.value:'#ffffff',value=>commitPaint(property,value,target));};
      }else if(type==='select'){
        for(const [value,title]of choices)control.add(new Option(title,value));
        globalThis.KeynopeFonts?.enhance(control);
        const mixed=new Option('Mixed','mixed');mixed.disabled=true;control.add(mixed);
        control.onchange=()=>commitPaint(property,property==='paint.posterize'?Number(control.value):['label.bold','label.italic','label.underline'].includes(property)?control.value==='true':control.value);
      }else{
        control.type='number';control.min='-4';control.max='4';control.step='1';control.required=true;
        if(property==='paint.strokeWidth'){control.min='0';control.max='64';control.step='.25';}
        const ranges={'line.width':[1,8,.5],'line.arrowWidth':[.5,20,.5],'label.size':[1,512,1],'label.width':[1,200,1],'label.lineHeight':[.5,4,.05],'label.paragraphBefore':[0,4,.05],'label.paragraphAfter':[0,4,.05]};
        if(ranges[property]){const [min,max,step]=ranges[property];control.min=min;control.max=max;control.step=step;}
        const effects={'paint.blur':[0,48,.25],'paint.posterize':[0,32,1],'paint.glowRadius':[0,48,1],'paint.glowThreshold':[0,100,1],'paint.glowIntensity':[0,100,1]};
        if(effects[property]){const [min,max,step]=effects[property];control.min=min;control.max=max;control.step=step;}
        let submitted=null;
        const submit=report=>{
          if(control.value===''||!(report?control.reportValidity():control.checkValidity()))return;
          const target=selectionPaintTarget?.(),key=JSON.stringify([target?.epoch,target?.owner,target?.slide,target?.master,target?.group,target?.targetIds,control.value]);
          if(submitted===key)return;submitted=key;
          commitPaint(property,Number(control.value),target);
        };
        control.oninput=()=>submit(false);control.onchange=()=>submit(true);
        control.addEventListener('blur',()=>{submitted=null;});
      }
      control.addEventListener('keydown',event=>event.stopPropagation());row.append(control);
      let inherit=null;
      if(type==='color'||property==='paint.strokeWidth'||property.startsWith('line.')||property.startsWith('paint.glow')||['paint.blur','paint.blurAxes','paint.posterize','paint.colourEffect','label.size','label.width','label.align','label.vertical','label.lineHeight','label.paragraphBefore','label.paragraphAfter'].includes(property)){
        inherit=document.createElement('button');inherit.type='button';inherit.className='keynope-paint-inherit';inherit.textContent='↶';inherit.setAttribute('aria-label','Inherit '+label.toLowerCase());inherit.title='Use inherited '+label.toLowerCase();inherit.onclick=()=>commitPaint(property,'');row.append(inherit);
      }
      let off=null;
      if(property==='paint.strokeColor'){off=document.createElement('button');off.type='button';off.textContent='Off';off.setAttribute('aria-label','Turn shape stroke off');off.onclick=()=>commitPaint(property,'none');row.append(off);}
      parent.append(row);paintFields.push({row,name,subset,control,inherit,off,property,type});
    }
    paintField(basePaint,'paint.color','Base colour','color');
    paintField(linePaint,'line.path','Line path','select',[['','Inherit'],['straight','Straight'],['elbow','Elbow']]);
    paintField(linePaint,'line.arrows','Arrowheads','select',[['','Inherit'],['none','None'],['start','Start'],['end','End'],['both','Both ends']]);
    paintField(linePaint,'line.width','Line width','number');
    paintField(linePaint,'line.arrowWidth','Arrow width','number');
    paintField(linePaint,'line.routing','Routing','select',[['','Inherit'],['automatic','Automatic'],['manual','Manual bends']]);
    paintFields.find(field=>field.property==='line.routing').control.querySelector('[value="manual"]').disabled=true;
    paintField(basePaint,'paint.fill','Shape fill','select',[['','Inherit'],['solid','Colour / gradient'],['none','No fill']]);
    const themeColourChoices=[['','Inherit'],['custom','Custom colour'],['text','Text'],['heading','Heading'],['background','Background'],['surface','Surface'],['muted','Muted'],['line','Line'],...Array.from({length:6},(_,i)=>['accent-'+(i+1),'Accent '+(i+1)])];
    paintField(basePaint,'paint.colorToken','Theme colour','select',themeColourChoices);
    const themeColourField=paintFields.find(field=>field.property==='paint.colorToken');
    themeColourField.control.querySelector('[value="custom"]').disabled=true;
    paintField(basePaint,'paint.outline','Outline','select',[['','Inherit'],['none','Off'],['dark','Dark'],['light','Light'],['theme','Inherited stroke'],['custom','Custom stroke']]);
    paintField(strokePaint,'paint.strokeColor','Stroke colour','color');paintField(strokePaint,'paint.strokeWidth','Stroke width','number');
    paintField(strokePaint,'paint.strokePosition','Stroke position','select',[['','Inherit'],['inside','Inside'],['center','Centre'],['outside','Outside']]);
    paintField(strokePaint,'paint.strokeToken','Stroke theme colour','select',themeColourChoices);
    paintFields.find(field=>field.property==='paint.strokeToken').control.querySelector('[value="custom"]').disabled=true;
    paintField(gradientPaint,'paint.gradientDirection','Gradient geometry','select',[['','Inherit'],['none','Off'],['horizontal','Horizontal'],['vertical','Vertical'],['diagonal','Diagonal'],['radial','Radial'],['reflected','Reflected'],['diamond','Diamond'],['angle','Angular']]);
    paintField(gradientPaint,'paint.gradientStart','Gradient start','color');paintField(gradientPaint,'paint.gradientEnd','Gradient end','color');
    paintField(shadowPaint,'paint.shadow','Shadow mode','select',[['','Inherit'],['none','Off'],['soft','Soft'],['solid','Solid']]);
    paintField(shadowPaint,'paint.shadowColor','Shadow colour','color');paintField(shadowPaint,'paint.shadowX','Shadow x','number');paintField(shadowPaint,'paint.shadowY','Shadow y','number');
    paintField(colourEffects,'paint.colourEffect','Colour treatment','select',[['none','Original'],['invert','Invert'],['duotone','Duotone'],['tritone','Tritone']]);
    paintField(colourEffects,'paint.effectShadows','Shadows','color');paintField(colourEffects,'paint.effectMidtones','Midtones','color');paintField(colourEffects,'paint.effectHighlights','Highlights','color');
    paintField(colourEffects,'paint.posterize','Posterise levels','select',[['0','Off'],...Array.from({length:31},(_,i)=>[String(i+2),String(i+2)])]);
    paintField(blurEffects,'paint.blur','Blur radius (0 = off)','number');
    paintField(blurEffects,'paint.blurAxes','Blur direction','select',[['both','Both axes'],['horizontal','Horizontal'],['vertical','Vertical']]);
    paintField(glowEffects,'paint.glowRadius','Glow radius (0 = off)','number');paintField(glowEffects,'paint.glowColour','Glow colour','color');
    paintField(glowEffects,'paint.glowThreshold','Glow threshold','number');paintField(glowEffects,'paint.glowIntensity','Glow strength','number');
    for(const [panel,text]of [[colourEffects,'Non-destructive colour mapping, then posterisation. Use at least 2 levels, or 0 to turn it off.'],[blurEffects,'Softens the rendered artwork, after Retro conversion. Zero keeps edges sharp.'],[glowEffects,'A soft, coloured halo behind visible pixels. Threshold selects how opaque pixels must be to emit light.']]){const help=document.createElement('p');help.className='keynope-command-scope';help.textContent=text;panel.append(help);}
    // Resolved theme or detached defaults are inherited strokes, not an
    // authored preset. Choose Inherit to restore one, or Off to override it.
    paintFields.find(field=>field.property==='paint.outline').control.querySelector('option[value="theme"]').disabled=true;
    paintFields.find(field=>field.property==='paint.outline').control.querySelector('option[value="custom"]').disabled=true;
    paintPanel.append(paintError);
    const labelPaint=paintSection('Shape labels',true);labelPaint.dataset.selectionContext='shape-label';
    paintField(labelPaint,'label.font','Label font','select',[['theme','Inherit font'],['c64','Keynope C64'],['sans','Go — proportional'],['mono','Go Mono']]);
    paintField(labelPaint,'label.size','Label size','number');paintField(labelPaint,'label.width','Label width','number');
    for(const name of ['bold','italic','underline'])paintField(labelPaint,'label.'+name,'Label '+name,'select',[['false','Off'],['true','On']]);
    paintField(labelPaint,'label.align','Label alignment','select',[['left','Left'],['center','Centre'],['right','Right'],['justify','Justify']]);
    paintField(labelPaint,'label.vertical','Label vertical alignment','select',[['top','Top'],['middle','Middle'],['bottom','Bottom']]);
    paintField(labelPaint,'label.lineHeight','Label line height','number');
    paintField(labelPaint,'label.paragraphBefore','Label space before (em)','number');paintField(labelPaint,'label.paragraphAfter','Label space after (em)','number');
    paintField(labelPaint,'label.color','Label colour','color');paintField(labelPaint,'label.colorToken','Label theme colour','select',themeColourChoices);
    paintFields.find(field=>field.property==='label.colorToken').control.querySelector('[value="custom"]').disabled=true;
    paintField(labelPaint,'label.outline','Label outline','select',[['','Inherit'],['none','Off'],['dark','Dark'],['light','Light']]);
    paintField(labelPaint,'label.gradientDirection','Label gradient','select',[['','Inherit'],['none','Off'],['horizontal','Horizontal'],['vertical','Vertical'],['diagonal','Diagonal']]);
    paintField(labelPaint,'label.gradientStart','Label gradient start','color');paintField(labelPaint,'label.gradientEnd','Label gradient end','color');
    paintField(labelPaint,'label.shadow','Label shadow','select',[['','Inherit'],['none','Off'],['soft','Soft'],['solid','Solid']]);paintField(labelPaint,'label.shadowColor','Label shadow colour','color');
    paintField(labelPaint,'label.shadowX','Label shadow x','number');paintField(labelPaint,'label.shadowY','Label shadow y','number');
    const imageDescription=document.createElement('form'),descriptionLabel=document.createElement('label'),descriptionInput=document.createElement('textarea'),decorativeLabel=document.createElement('label'),decorativeInput=document.createElement('input'),descriptionSave=document.createElement('button');
    imageDescription.className='keynope-image-description';descriptionLabel.textContent='Image description';descriptionInput.setAttribute('aria-label','Image description');descriptionInput.maxLength=1000;descriptionInput.rows=3;descriptionLabel.append(descriptionInput);
    decorativeInput.type='checkbox';decorativeLabel.append(decorativeInput,document.createTextNode(' Decorative image'));descriptionSave.type='submit';descriptionSave.textContent='Apply description';imageDescription.append(descriptionLabel,decorativeLabel,descriptionSave);inspector.insertBefore(imageDescription,selection);
    const descriptionReset=document.createElement('button');descriptionReset.type='button';descriptionReset.textContent='Reset description';descriptionReset.setAttribute('aria-label','Reset image description draft');imageDescription.append(descriptionReset);
    const descriptionError=document.createElement('p');descriptionError.setAttribute('role','alert');descriptionError.hidden=true;imageDescription.append(descriptionError);
    let descriptionTarget='',descriptionOwner='',descriptionCapture=null,descriptionDirty=false,savingDescription=false;
    const editDescription=()=>{descriptionDirty=true;descriptionReset.disabled=savingDescription;descriptionError.hidden=true;};
    descriptionInput.addEventListener('input',editDescription);
    decorativeInput.onchange=()=>{descriptionInput.disabled=decorativeInput.checked;editDescription();};
    descriptionReset.onclick=()=>{if(savingDescription)return;descriptionDirty=false;descriptionCapture=null;descriptionError.hidden=true;refresh();};
    imageDescription.addEventListener('keydown',event=>event.stopPropagation());
    imageDescription.onsubmit=async event=>{event.preventDefault();if(!descriptionTarget||savingDescription||!imageDescription.reportValidity())return;const id=descriptionTarget,description=descriptionInput.value,decorative=decorativeInput.checked,capture=descriptionCapture,owner=descriptionOwner;savingDescription=true;descriptionError.hidden=true;refresh();try{await setImageDescription(id,description,decorative,capture);descriptionDirty=false;descriptionCapture=null;}catch(error){if(owner===descriptionOwner){descriptionError.textContent=error.message||'Could not save image description';descriptionError.hidden=false;}}finally{savingDescription=false;refresh();}};
    const objectPanel=document.createElement('div');objectPanel.id='keynope-object-navigator';objectPanel.className='keynope-object-navigator';objectPanel.setAttribute('role','tabpanel');
    panels={...panels,objects:objectPanel};inspector.append(objectPanel);
    const layerControls=document.createElement('div');inspector.insertBefore(layerControls,objectPanel);
    let movingLayer=false;
    const renameButton=document.createElement('button');renameButton.type='button';renameButton.textContent='Rename object';renameButton.hidden=!setObjectName;layerControls.append(renameButton);
    const renameForm=document.createElement('form');renameForm.hidden=true;renameForm.className='keynope-object-rename';
    const renameLabel=document.createElement('label'),renameInput=document.createElement('input'),renameSave=document.createElement('button'),renameCancel=document.createElement('button'),renameError=document.createElement('div');
    renameLabel.textContent='Object name';renameInput.type='text';renameInput.maxLength=128;renameInput.setAttribute('aria-label','Object name');renameLabel.append(renameInput);
    renameSave.type='submit';renameSave.textContent='Save name';renameCancel.type='button';renameCancel.textContent='Cancel rename';renameError.setAttribute('role','status');renameForm.append(renameLabel,renameSave,renameCancel,renameError);inspector.insertBefore(renameForm,objectPanel);
    let naming=null,savingName=false;
    const closeRename=()=>{const id=naming?.id;naming=null;renameForm.hidden=true;renameError.textContent='';if(id)objectNodes.get(id)?.focus();};
    async function beginRename(item){if(!setObjectName||!item||item.locked||editing||movingLayer||savingName)return;await selectObject(item.id,false);naming={id:item.id,capture:selectionPaintTarget?.()};renameInput.value=item.name||'';renameError.textContent='';renameForm.hidden=false;renameInput.focus();renameInput.select();}
    renameButton.onclick=()=>beginRename((objects?.()||[]).find(item=>item.selected)).catch(error=>hint.textContent=error.message);
    renameCancel.onclick=closeRename;renameForm.addEventListener('keydown',event=>{event.stopPropagation();if(event.key==='Escape'){event.preventDefault();if(!savingName)closeRename();}});
    renameForm.onsubmit=async event=>{event.preventDefault();if(!naming||savingName||!renameForm.reportValidity())return;savingName=true;renameSave.disabled=renameCancel.disabled=renameInput.disabled=true;try{await setObjectName(naming.id,renameInput.value,naming.capture);closeRename();}catch(error){renameError.textContent=error.message||'Could not rename object';}finally{savingName=false;renameSave.disabled=renameCancel.disabled=renameInput.disabled=false;}};
    const visibility=document.createElement('button');visibility.type='button';layerControls.append(visibility);
    const lock=document.createElement('button');lock.type='button';layerControls.append(lock);
    const groupButtons=[['group-elements','Group','Alt+Meta+G'],['ungroup-elements','Ungroup','Shift+Alt+Meta+G'],['enter-group','Edit group','Alt+Meta+Enter']].map(([action,label,shortcut])=>{const button=document.createElement('button');button.type='button';button.textContent=label;button.title=label+' · '+shortcut.replaceAll('Meta','⌘').replaceAll('Alt','⌥');button.setAttribute('aria-keyshortcuts',shortcut);button.onclick=()=>performGroupAction(action);layerControls.append(button);return button;});
    lock.onclick=async()=>{const selected=(objects?.()||[]).filter(o=>o.selected);if(!selected.length||movingLayer||!setObjectLock)return;movingLayer=true;refresh();try{await setObjectLock(!selected.every(o=>o.locked));}catch(error){hint.textContent=error.message||'Could not change lock';}finally{movingLayer=false;refresh();}};
    visibility.onclick=async()=>{const selected=(objects?.()||[]).filter(o=>o.selected);if(!selected.length||movingLayer||!setObjectVisibility)return;movingLayer=true;refresh();try{await setObjectVisibility(!selected.every(o=>o.hidden));}catch(error){hint.textContent=error.message||'Could not change visibility';}finally{movingLayer=false;refresh();}};
    const oneStackUnit=items=>items.length===1||(items.length>1&&!!items[0].group&&items.every(item=>item.group===items[0].group));
    const layerButtons=[[-1,'Send backward'],[1,'Bring forward']].map(([delta,label])=>{
      const button=document.createElement('button');button.type='button';button.textContent=label;button.setAttribute('aria-label',label+' in object stack');
      button.onclick=async()=>{const selected=(objects?.()||[]).filter(o=>o.selected);if(!oneStackUnit(selected)||movingLayer)return;movingLayer=true;refresh();try{await moveObjectLayer(selected[0].id,delta);}catch(error){hint.textContent=error.message||'Could not reorder object';}finally{movingLayer=false;refresh();}};
      layerControls.append(button);return button;
    });
    let objectSignature='';
    const objectNodes=new Map();
    const objectEmpty=document.createElement('p');objectEmpty.textContent='No objects on this slide.';
    const layerStatus=document.createElement('p');layerStatus.setAttribute('role','status');layerStatus.setAttribute('aria-live','polite');layerStatus.className='keynope-workspace-hint';layerControls.append(layerStatus);
    let objectDrag=null,suppressObjectClick=false;
    const clearObjectDrop=()=>{for(const button of objectPanel.children){button.classList.remove('layer-dragging','layer-drop-before','layer-drop-after');}};
    const cancelObjectDrag=message=>{
      const drag=objectDrag;if(!drag)return;objectDrag=null;
      if(drag.active)suppressObjectClick=true;
      clearObjectDrop();
      const button=objectNodes.get(drag.key);if(button?.hasPointerCapture(drag.id))button.releasePointerCapture(drag.id);
      if(message)layerStatus.textContent=message;
    };
    const cancelLayerKey=event=>{if(objectDrag&&event.key==='Escape'){event.preventDefault();event.stopImmediatePropagation();cancelObjectDrag('Layer reorder cancelled.');}};
    const blurLayerDrag=()=>cancelObjectDrag('Layer reorder cancelled.');
    // Register before the editor's canvas shortcuts. A listener installed only
    // on pointerdown can lose Escape to an older capture-phase shortcut.
    window.addEventListener('keydown',cancelLayerKey,true);window.addEventListener('blur',blurLayerDrag);
    const canReorderObject=id=>{const all=objects?.()||[],item=all.find(item=>item.id===id);return item&&!item.locked&&(!item.group||item.group===groupScope?.()||!all.some(peer=>peer.group===item.group&&peer.locked));};
    async function reorderObject(id,value,target,side,capture=selectionPaintTarget?.()){
      if(editing||movingLayer||!moveObjectLayer)return;
      if(!sameLayerContext(capture,selectionPaintTarget?.())){layerStatus.textContent='The object list changed; reorder cancelled.';return;}
      if(!canReorderObject(id)){layerStatus.textContent='Unlock the object or its group before reordering.';return;}
      movingLayer=true;refresh();
      try{await moveObjectLayer(id,value,target,side,capture);layerStatus.textContent='Object stack updated.';}
      catch(error){layerStatus.textContent=error.message||'Could not reorder object';}
      finally{movingLayer=false;refresh();}
    }
    function refreshObjects(allObjects){
      if(objectDrag&&!sameLayerContext(objectDrag.capture,selectionPaintTarget?.()))cancelObjectDrag('The object list changed; reorder cancelled.');
      const scope=groupScope?.()||'',items=(allObjects||[]).filter(item=>!scope||item.group===scope);const signature=JSON.stringify([items,editing,scope]);if(signature===objectSignature)return;objectSignature=signature;
      const focused=document.activeElement?.closest('[data-object-key]')?.dataset.objectKey;
      const retained=new Set(items.map(item=>item.id));
      for(const [id,button] of objectNodes){if(retained.has(id))continue;button.remove();objectNodes.delete(id);}
      if(!items.length){objectEmpty.hidden=false;objectPanel.append(objectEmpty);return;}
      objectEmpty.remove();
      let cursor=objectPanel.firstElementChild;
      for(const item of items){
        let button=objectNodes.get(item.id);
        if(!button){
          button=document.createElement('button');button.type='button';button.dataset.objectKey=item.id;
          button.setAttribute('aria-keyshortcuts','F2 Shift+Enter Shift+Space ArrowUp ArrowDown Home End Alt+ArrowUp Alt+ArrowDown');
          button.onclick=async event=>{const current=button.keynopeObject;if(!current)return;if(suppressObjectClick&&event.detail!==0){suppressObjectClick=false;return;}suppressObjectClick=false;try{await selectObject(current.id,!!event.shiftKey);}catch(error){hint.textContent=error.message||'Could not select object';}};
          button.onkeydown=async event=>{const current=button.keynopeObject;if(!current)return;event.stopPropagation();if(event.key==='F2'){event.preventDefault();try{await beginRename(current);}catch(error){hint.textContent=error.message;}return;}if(event.shiftKey&&(event.key==='Enter'||event.key===' ')){event.preventDefault();try{await selectObject(current.id,true);}catch(error){hint.textContent=error.message||'Could not extend selection';}return;}if(!['ArrowUp','ArrowDown','Home','End'].includes(event.key))return;event.preventDefault();if(event.altKey&&event.key.startsWith('Arrow')){reorderObject(current.id,event.key==='ArrowUp'?-1:1);return;}const buttons=[...objectPanel.querySelectorAll('button')],index=buttons.indexOf(button);buttons[event.key==='Home'?0:event.key==='End'?buttons.length-1:(index+(event.key==='ArrowUp'?-1:1)+buttons.length)%buttons.length]?.focus();};
          button.ondblclick=()=>beginRename(button.keynopeObject).catch(error=>hint.textContent=error.message);
          button.onpointerdown=event=>{const current=button.keynopeObject;if(!current||event.button!==0||editing||movingLayer||!moveObjectLayer)return;suppressObjectClick=false;if(!canReorderObject(current.id))return;cancelObjectDrag();objectDrag={id:event.pointerId,key:current.id,x:event.clientX,y:event.clientY,active:false,capture:selectionPaintTarget?.()};button.setPointerCapture(event.pointerId);};
          button.onpointermove=event=>{
            if(objectDrag?.id!==event.pointerId)return;
            if(!sameLayerContext(objectDrag.capture,selectionPaintTarget?.())){cancelObjectDrag('The object list changed; reorder cancelled.');return;}
            if(!objectDrag.active&&Math.hypot(event.clientX-objectDrag.x,event.clientY-objectDrag.y)<5)return;
            objectDrag.active=true;event.preventDefault();clearObjectDrop();button.classList.add('layer-dragging');
            const bounds=inspector.getBoundingClientRect();if(event.clientY>bounds.bottom-32)inspector.scrollTop+=16;else if(event.clientY<bounds.top+32)inspector.scrollTop-=16;
            const choices=[...objectPanel.children].filter(node=>node.dataset.objectKey);
            const target=choices.find(node=>event.clientY<node.getBoundingClientRect().bottom)||choices.at(-1);
            if(!target)return;const box=target.getBoundingClientRect(),side=event.clientY<box.y+box.height/2?'before':'after';
            objectDrag.target=target.dataset.objectKey;objectDrag.side=side;target.classList.add('layer-drop-'+side);
          };
          const finishObjectDrag=event=>{if(objectDrag?.id!==event.pointerId)return;const drag=objectDrag;cancelObjectDrag();if(event.type==='pointerup'&&drag.active&&drag.target&&drag.target!==drag.key)reorderObject(drag.key,0,drag.target,drag.side,drag.capture);};
          for(const type of ['pointerup','pointercancel','lostpointercapture'])button.addEventListener(type,finishObjectDrag);
          objectNodes.set(item.id,button);
        }
        button.keynopeObject=item;
        button.textContent=item.label+(item.hidden?' · Hidden':'')+(item.locked?' · Locked':'');button.title=button.textContent;button.setAttribute('aria-pressed',String(item.selected));button.disabled=editing;
        button.classList.toggle('active',item.selected);
        button.title+=' · F2 renames · Shift+Enter/Space extends selection · Drag to reorder · Alt+Up/Down moves one layer';
        // Keep an unchanged row attached/in place: re-appending every row on
        // selection-only refresh can invalidate its active pointer capture.
        if(button!==cursor)objectPanel.insertBefore(button,cursor);
        else cursor=cursor.nextElementSibling;
      }
      if(focused)[...objectPanel.children].find(n=>n.dataset.objectKey===focused)?.focus();
    }
    topbar.append(inspector);
    const divider=document.createElement('button');divider.type='button';divider.className='keynope-inspector-divider';
    divider.setAttribute('role','separator');divider.setAttribute('aria-label','Resize Format inspector');
    divider.setAttribute('aria-orientation','vertical');divider.setAttribute('aria-controls',inspector.id);
    divider.title='Drag to resize · Left/Right arrow: 10 px · Shift: 50 px · Double-click: reset';
    topbar.append(divider);
    let inspectorWidth=300;
    try {const saved=Number(localStorage.getItem('keynope.inspectorWidth'));if(saved>=240&&saved<=480)inspectorWidth=saved;}catch(_error){}
    let navigatorWidth=210;
    try {const saved=Number(localStorage.getItem('keynope.navigatorWidth'));if(saved>=160&&saved<=360)navigatorWidth=saved;}catch(_error){}
    const navigatorDivider=document.createElement('button');navigatorDivider.type='button';navigatorDivider.className='keynope-navigator-divider';navigatorDivider.setAttribute('role','separator');navigatorDivider.setAttribute('aria-label','Resize slide navigator');navigatorDivider.setAttribute('aria-orientation','vertical');navigatorDivider.title='Drag to resize · Left/Right arrow: 10 px · Shift: 50 px · Double-click: reset';topbar.append(navigatorDivider);
    navigatorDivider.setAttribute('aria-controls','keynope-slide-navigator');
    main.classList.add('keynope-insert-popover');
    main.id = 'keynope-insert-popover';
    main.setAttribute('role', 'region');
    main.setAttribute('aria-label', 'Insert content');
    const presets=document.createElement('section');presets.className='keynope-slide-presets';presets.setAttribute('aria-label','Slide layouts');
    const presetHeading=document.createElement('h3');presetHeading.textContent='Slide layouts';presets.append(presetHeading);
    for(const [id,label,boxes] of [['title','Title',[[8,15,84,25],[8,65,65,8]]],['section','Section',[[8,20,30,6],[8,40,84,25]]],['body','Title and body',[[8,8,70,12],[8,36,84,48]]],['columns','Two columns',[[8,8,70,12],[8,36,38,48],[54,36,38,48]]],['quote','Quote',[[12,25,76,30],[12,72,30,6]]],['comparison','Comparison',[[8,8,70,12],[8,34,38,50],[54,34,38,50]]]]){
      const card=document.createElement('button');card.type='button';card.title='Insert '+label+' slide';card.setAttribute('aria-label',card.title);
      const diagram=document.createElementNS('http://www.w3.org/2000/svg','svg');diagram.setAttribute('viewBox','0 0 100 100');diagram.setAttribute('aria-hidden','true');
      for(const [x,y,w,h] of boxes){const rect=document.createElementNS(diagram.namespaceURI,'rect');for(const [key,value] of Object.entries({x,y,width:w,height:h,fill:'currentColor',opacity:h>20?.3:.8}))rect.setAttribute(key,String(value));diagram.append(rect);}
      const text=document.createElement('span');text.textContent=label;card.append(diagram,text);card.onclick=async()=>{card.disabled=true;try{await addPreset(id);insertOpen=false;refresh();}catch(error){hint.textContent=error.message||'Could not insert slide';}finally{card.disabled=false;}};presets.append(card);
    }
    if(addPreset)main.prepend(presets);
    // Keep the actual insertion controls and their listeners. Give each a
    // visible name and a predictable section instead of an anonymous icon row.
    const insertSection=(label,className)=>{const section=document.createElement('section');section.className='keynope-insert-section '+className;const heading=document.createElement('h3');heading.textContent=label;section.append(heading);panels.insert.append(section);return section;};
    const insertControls=[...panels.insert.children];
    const textSection=insertSection('Text','keynope-insert-text'),mediaSection=insertSection('Media & objects','keynope-insert-media'),defaults=insertSection('New text defaults','keynope-insert-defaults'),other=insertSection('Document','keynope-insert-document');
    const labels={'Add title':'Title','Add subtitle':'Subtitle','Add text':'Text','Add bullet point':'Bullets','Add code':'Code','Add emoji':'Emoji','Add shape':'Shape','Connect shapes':'Connector','Import image':'Image','Import video':'Video','Add table':'Table','Add chart':'Chart'};
    for(const node of insertControls){
      if(node.classList.contains('keynope-text-metric-control')){defaults.append(node);continue;}
      if(node.tagName==='BUTTON'&&labels[node.title]){
        const label=document.createElement('span');label.className='keynope-insert-label';label.textContent=labels[node.title];node.append(label);node.classList.add('keynope-insert-command');
        (/^Add (title|subtitle|text|bullet point|code)$/.test(node.title)?textSection:mediaSection).append(node);
      }else other.append(node);
    }
    const syncDocumentSection=()=>{const hidden=![...other.children].some(node=>node.tagName!=='H3'&&!node.hidden);if(other.hidden!==hidden)other.hidden=hidden;};
    new MutationObserver(syncDocumentSection).observe(other,{attributes:true,attributeFilter:['hidden'],subtree:true});syncDocumentSection();
    quick.append(addSlide);
    header.insertBefore(toolbar, header.lastElementChild);
    let context = '', editing = false, visible = true, insertOpen = false, section = 'slide';
    // Application chrome and object model are unified. Typography is a font
    // choice; only images expose an optional Retro conversion treatment.
    const remembered = new Map();
    const themeLabel=document.createElement('label'),themeSelect=document.createElement('select');themeLabel.className='keynope-ttf-width-control';themeLabel.textContent='Deck theme';themeSelect.setAttribute('aria-label','Deck theme');
    for(const [id,name] of [['','Original'],['studio-v1','Studio'],['midnight-v1','Midnight'],['warm-v1','Warm paper']])themeSelect.add(new Option(name,id));
    themeSelect.title='Sets the deck colour defaults. Explicit slide, master and object colours are preserved.';themeLabel.append(themeSelect);if(setTheme)panels.slide.append(themeLabel);
    let savingTheme=false;
    themeSelect.addEventListener('keydown',event=>event.stopPropagation());themeSelect.onchange=async()=>{savingTheme=true;themeSelect.disabled=true;try{await setTheme(themeSelect.value);themeSelect.setCustomValidity('');}catch(error){themeSelect.setCustomValidity(error.message||'Could not apply theme');themeSelect.reportValidity();}finally{savingTheme=false;refresh();}};
    const editTheme=document.createElement('button');editTheme.type='button';editTheme.textContent='Edit theme…';editTheme.title='Edit portable deck colours and typography';
    if(setDeckTheme)panels.slide.append(editTheme);
    const editBuildsButton=document.createElement('button');editBuildsButton.type='button';editBuildsButton.textContent='Builds…';editBuildsButton.title='Edit object builds and slide transition';editBuildsButton.setAttribute('aria-label','Edit slide builds');
    if(editBuilds)panels.slide.append(editBuildsButton);
    editBuildsButton.onclick=()=>Promise.resolve(editBuilds()).catch(error=>{hint.textContent=error.message||'Could not open Builds';});
    const themeDialog=document.createElement('dialog');themeDialog.className='keynope-theme-dialog';themeDialog.setAttribute('aria-label','Edit deck theme');
    const themeForm=document.createElement('form');themeForm.className='keynope-theme-form';themeDialog.append(themeForm);
    let themeDraft=null,themeContext=null,themeFocus=null,themeApplying=false;
    const closeTheme=()=>{if(!themeApplying&&themeDialog.open)themeDialog.close();};
    const renderThemeDraft=()=>{
      themeForm.replaceChildren();
      const heading=document.createElement('h2');heading.textContent='Deck theme';themeForm.append(heading);
      const row=(label,node,parent=themeForm)=>{const field=document.createElement('label');field.append(document.createTextNode(label),node);parent.append(field);return node;};
      const name=document.createElement('input');name.type='text';name.value=themeDraft.name||'';name.maxLength=100;name.setAttribute('aria-label','Theme name');name.oninput=()=>themeDraft.name=name.value;row('Name',name);
      const presets=document.createElement('div');presets.className='keynope-theme-presets';
      for(const preset of themePresets?.()||[]){
        const choice=document.createElement('button');choice.type='button';choice.textContent=preset.name;choice.dataset.themePreview='';
        choice.style.setProperty('--theme-preview-bg',preset.colors.background);choice.style.setProperty('--theme-preview-fg',preset.colors.heading);
        choice.onclick=()=>{themeDraft=JSON.parse(JSON.stringify(preset));renderThemeDraft();};presets.append(choice);
      }
      themeForm.append(presets);
      const colorSection=document.createElement('fieldset'),colorLegend=document.createElement('legend');colorLegend.textContent='Colours';colorSection.append(colorLegend);
      const colors=document.createElement('div');colors.className='keynope-theme-colors';colorSection.append(colors);
      const colorControl=(label,value,set)=>{
        const button=document.createElement('button');button.type='button';button.className='keynope-theme-color';button.setAttribute('aria-label',label);button.title=label;
        const show=color=>{button.style.setProperty('--theme-color',color);button.setAttribute('aria-description',color);};show(value);
        button.onclick=()=>pickColor?.(button,value,color=>{value=color;set(color);show(color);});return button;
      };
      const addColor=(label,value,set)=>row(label,colorControl('Theme '+label.toLowerCase(),value,set),colors);
      for(const [key,label] of [['background','Background'],['surface','Surface'],['text','Text'],['heading','Heading'],['muted','Muted'],['line','Line']])addColor(label,themeDraft.colors[key],value=>themeDraft.colors[key]=value);
      themeDraft.colors.accent.forEach((value,index)=>addColor('Accent '+(index+1),value,color=>themeDraft.colors.accent[index]=color));themeForm.append(colorSection);
      const fonts=document.createElement('fieldset'),fontLegend=document.createElement('legend');fontLegend.textContent='Typography';fonts.append(fontLegend);
      for(const [role,label]of [['heading','Heading'],['body','Body'],['mono','Code']]){
        const group=document.createElement('div');group.className='keynope-theme-font';const title=document.createElement('h3');title.textContent=label;group.append(title);
        const select=document.createElement('select');for(const [id,name] of [['c64','Keynope C64'],['sans','Go — proportional'],['mono','Go Mono — monospaced']])select.add(new Option(name,id));globalThis.KeynopeFonts?.enhance(select);select.value=themeDraft.fonts[role].fontId;select.setAttribute('aria-label',label+' theme font');select.onchange=()=>themeDraft.fonts[role].fontId=select.value;row('Font',select,group);
        for(const [key,title,min,max,step]of [['size','Size',1,1024,1],['width','Width',1,200,1],['lineHeight','Line height',.5,4,.05]]){
          const input=document.createElement('input');input.type='number';input.min=String(min);input.max=String(max);input.step=String(step);input.required=true;input.value=String(themeDraft.fonts[role][key]);input.setAttribute('aria-label',label+' theme '+title.toLowerCase());input.oninput=()=>{if(input.value!==''&&input.checkValidity())themeDraft.fonts[role][key]=Number(input.value);};row(title,input,group);
        }
        fonts.append(group);
      }
      themeForm.append(fonts);
      const defaults=document.createElement('fieldset'),defaultLegend=document.createElement('legend');defaultLegend.textContent='Shapes and lines';defaults.append(defaultLegend);
      for(const [key,label]of [['shapeFill','Shape fill'],['shapeLine','Shape outline']]){
        const fallback=themeDraft.colors[key==='shapeFill'?'surface':'line'];
        let chosen=themeDraft.defaults[key]==='none'?fallback:themeDraft.defaults[key]||fallback;
        const colour=colorControl(label,chosen,value=>{chosen=value;themeDraft.defaults[key]=value;off.checked=false;});
        const group=document.createElement('div'),off=document.createElement('input');off.type='checkbox';off.checked=themeDraft.defaults[key]==='none';off.setAttribute('aria-label','No '+label.toLowerCase());
        off.onchange=()=>{themeDraft.defaults[key]=off.checked?'none':chosen;};
        const offLabel=document.createElement('label');offLabel.append(off,document.createTextNode('None'));group.append(colour,offLabel);row(label,group,defaults);
      }
      const width=document.createElement('input');width.type='number';width.min='0';width.max='20';width.step='any';width.required=true;width.value=String(themeDraft.defaults.lineWidth??1);width.setAttribute('aria-label','Theme line width');width.oninput=()=>{if(width.value!==''&&width.checkValidity())themeDraft.defaults.lineWidth=Number(width.value);};row('Line width',width,defaults);themeForm.append(defaults);
      const note=document.createElement('p');note.className='keynope-theme-note';note.textContent='Explicit slide, master and object styles stay unchanged. Choose “Use theme font” on text to link its font.';themeForm.append(note);
      const error=document.createElement('p');error.className='keynope-theme-error';error.setAttribute('role','alert');themeForm.append(error);
      const actions=document.createElement('div');actions.className='keynope-theme-actions';
      const cancel=document.createElement('button');cancel.type='button';cancel.textContent='Cancel';cancel.onclick=closeTheme;
      const apply=document.createElement('button');apply.type='submit';apply.textContent='Apply theme';apply.className='primary';actions.append(cancel,apply);themeForm.append(actions);
      const detach=document.createElement('button');detach.type='button';detach.textContent='Detach from theme';
      if(detachDeckTheme&&hasDeckTheme?.()){
        actions.prepend(detach);
        const explanation=document.createElement('p');explanation.className='keynope-theme-note';explanation.textContent='Detach keeps the deck’s current appearance and master links, but removes theme links. Unapplied changes above are not used. You can undo this.';themeForm.insertBefore(explanation,actions);
      }
      const pending=value=>{themeApplying=value;apply.disabled=cancel.disabled=detach.disabled=value;};
      detach.onclick=async()=>{
        if(themeApplying)return;
        pending(true);error.textContent='';
        try{await detachDeckTheme(themeContext);themeApplying=false;themeDialog.close();}
        catch(problem){error.textContent=problem.message||'Could not detach theme';}
        finally{pending(false);refresh();}
      };
      themeForm.onsubmit=async event=>{
        event.preventDefault();if(themeApplying||!themeForm.reportValidity())return;
        pending(true);error.textContent='';
        try{await setDeckTheme(JSON.parse(JSON.stringify(themeDraft)),themeContext);themeApplying=false;themeDialog.close();}
        catch(problem){error.textContent=problem.message||'Could not apply theme';}
        finally{pending(false);refresh();}
      };
    };
    editTheme.onclick=()=>{const current=getTheme?.();if(!current)return;themeDraft=JSON.parse(JSON.stringify(current));themeContext=themeDraftContext?JSON.parse(JSON.stringify(themeDraftContext())):null;themeDraft.defaults||={};themeFocus=document.activeElement;renderThemeDraft();themeDialog.showModal();themeForm.querySelector('input')?.focus();};
    themeDialog.addEventListener('keydown',event=>event.stopPropagation());themeDialog.addEventListener('cancel',event=>{if(themeApplying)event.preventDefault();});
    themeDialog.addEventListener('close',()=>{themeDraft=null;themeContext=null;if(themeFocus?.isConnected)themeFocus.focus({preventScroll:true});themeFocus=null;});document.body.append(themeDialog);
    const button = (label, callback, control) => {
      const node = document.createElement('button');
      node.type = 'button'; node.textContent = label; node.title = label;
      if (control) node.setAttribute('aria-controls', control);
      node.onclick = callback; toolbar.append(node); return node;
    };
    let navigatorOpen=false,navigatorVisible=true;
    try{navigatorVisible=localStorage.getItem('keynope.workspace.navigator')!=='off';}catch(_error){}
    const narrowNavigator=()=>innerWidth<700;
    const navigatorShowing=()=>narrowNavigator()?navigatorOpen:navigatorVisible;
    const drawerInspector=()=>innerWidth<1050;
    const slidesToggle=button('Slides',()=>{
      const navigator=document.getElementById('keynope-slide-navigator'),hadFocus=navigator?.contains(document.activeElement)||document.activeElement===navigatorDivider;
      if(narrowNavigator()){
        navigatorOpen=!navigatorOpen;
        if(navigatorOpen){visible=false;insertOpen=false;}
      }else{
        navigatorVisible=!navigatorVisible;
        try{localStorage.setItem('keynope.workspace.navigator',navigatorVisible?'on':'off');}catch(_error){}
      }
      refresh();
      if(navigatorShowing())document.querySelector('#keynope-slide-navigator .keynope-slide-item.active, #keynope-slide-navigator .keynope-slide-item')?.focus();
      else if(hadFocus)slidesToggle.focus({preventScroll:true});
    },'keynope-slide-navigator');
    slidesToggle.className='keynope-slides-toggle';
    slidesToggle.setAttribute('aria-label','Slide navigator');
    const closeNavigator=(focus=false)=>{if(!navigatorOpen)return;navigatorOpen=false;updateGeometry();if(focus)slidesToggle.focus();};
    const insert = button('Insert', () => {
      insertOpen = !insertOpen; refresh();
      if (insertOpen) main.querySelector('button:not(:disabled)')?.focus();
    }, main.id);
    const format = button('Format', () => { visible = !visible; refresh(); }, inspector.id);
    const closeInspector=(focus=false)=>{if(!visible)return;visible=false;refresh();if(focus)format.focus();};
    inspectorClose.onclick=()=>closeInspector(true);inspectorScrim.onpointerdown=event=>{event.preventDefault();closeInspector(true);};
    const slide = button('Slide', () => { section = 'slide'; visible = true; refresh(); }, inspector.id);
    const viewport=window.KEYNOPE_EDITOR_VIEWPORT={zoom:1,x:0,y:0};
    const fit = button('Fit', () => {Object.assign(viewport,{zoom:1,x:0,y:0});resize();});
    fit.title = 'Fit the whole slide to the workspace';
    const zoomDialog=document.createElement('dialog');zoomDialog.className='keynope-zoom-dialog';zoomDialog.setAttribute('aria-label','Canvas zoom');zoomDialog.setAttribute('aria-modal','true');
    Object.assign(zoomDialog.style,{color:'#edf1f3',background:'#202830',border:'1px solid #65717a',borderRadius:'8px',padding:'18px',maxWidth:'calc(100vw - 48px)'});
    const zoomLabel=document.createElement('label');zoomLabel.textContent='Canvas zoom (relative to Fit) ';zoomDialog.append(zoomLabel);
    const zoomInput=document.createElement('input');zoomInput.type='range';zoomInput.min='50';zoomInput.max='400';zoomInput.step='10';zoomInput.setAttribute('aria-label','Canvas zoom percent');zoomLabel.append(zoomInput);
    const zoomValue=document.createElement('output');zoomDialog.append(zoomValue);
    const setZoom=(value,anchor)=>{
      const canvas=document.getElementById('presenter-canvas'),before=anchor&&canvas?.getBoundingClientRect();
      viewport.zoom=Math.max(.5,Math.min(4,value));zoomInput.value=String(Math.round(viewport.zoom*100));zoomValue.textContent=Math.round(viewport.zoom*100)+'%';resize();
      if(before?.width&&before.height){
        const after=canvas.getBoundingClientRect();
        viewport.x+=anchor.x-after.left-(anchor.x-before.left)/before.width*after.width;
        viewport.y+=anchor.y-after.top-(anchor.y-before.top)/before.height*after.height;
        resize();
      }
    };
    zoomInput.oninput=()=>setZoom(Number(zoomInput.value)/100);
    const zoomHelp=document.createElement('p');zoomHelp.textContent='Scroll to pan when enlarged. Ctrl/⌘ + scroll to zoom. Fit restores the whole slide.';zoomDialog.append(zoomHelp);
    const zoomActions=document.createElement('div');Object.assign(zoomActions.style,{display:'flex',gap:'8px',flexWrap:'wrap'});zoomDialog.append(zoomActions);
    let zoomRestore=null;
    const closeZoom=()=>{if(zoomDialog.open)zoomDialog.close();};
    for(const [label,action] of [['Fit',()=>{viewport.x=viewport.y=0;setZoom(1);}],['Left',()=>{viewport.x+=80;resize();}],['Right',()=>{viewport.x-=80;resize();}],['Up',()=>{viewport.y+=80;resize();}],['Down',()=>{viewport.y-=80;resize();}],['Done',closeZoom]]){
      const node=document.createElement('button');node.type='button';node.textContent=label;node.onclick=action;zoomActions.append(node);
    }
    zoomDialog.addEventListener('keydown',event=>{
      event.stopPropagation();
      if(event.key==='Escape'){event.preventDefault();closeZoom();return;}
      if(event.key!=='Tab')return;
      const controls=[...zoomDialog.querySelectorAll('button,input,select,textarea,a[href],summary,[tabindex]')].filter(node=>!node.disabled&&node.tabIndex>=0&&node.getClientRects().length&&getComputedStyle(node).visibility!=='hidden');
      if(!controls.length){event.preventDefault();return;}
      const first=controls[0],last=controls[controls.length-1];
      if(event.shiftKey&&document.activeElement===first){event.preventDefault();last.focus();}
      else if(!event.shiftKey&&document.activeElement===last){event.preventDefault();first.focus();}
    });
    zoomDialog.addEventListener('close',()=>{const restore=zoomRestore;zoomRestore=null;if(restore?.isConnected)restore.focus({preventScroll:true});});
    document.body.append(zoomDialog);
    const zoomButton=button('Zoom',()=>{zoomRestore=zoomButton;zoomInput.value=String(Math.round(viewport.zoom*100));zoomValue.textContent=zoomInput.value+'%';zoomDialog.showModal();zoomInput.focus();});zoomButton.classList.add('keynope-workspace-extra');
    const zoomStage=document.getElementById('stage');
    zoomStage?.addEventListener('wheel',event=>{
      if(document.querySelector('dialog[open]')||event.target.closest('input,textarea,[contenteditable="true"]'))return;
      if(event.ctrlKey||event.metaKey){event.preventDefault();event.stopImmediatePropagation();setZoom(viewport.zoom*Math.exp(-event.deltaY*.002),{x:event.clientX,y:event.clientY});}
      else if(viewport.zoom>1){event.preventDefault();event.stopImmediatePropagation();const unit=event.deltaMode===1?16:event.deltaMode===2?zoomStage.clientHeight:1;viewport.x-=event.deltaX*unit;viewport.y-=event.deltaY*unit;resize();}
    },{passive:false,capture:true});
    let snapping=true;
    try{snapping=localStorage.getItem('keynope.workspace.snapping')!=='off';}catch(_){}
    const updateSnapping=()=>{document.documentElement.dataset.keynopeSnapping=snapping?'on':'off';snap.dataset.state=snapping?'ON':'OFF';snap.setAttribute('aria-pressed',String(snapping));snap.title=snapping?'Alignment snapping on — hold Alt to bypass':'Alignment snapping off';};
    const snap=button('Snap',()=>{snapping=!snapping;try{localStorage.setItem('keynope.workspace.snapping',snapping?'on':'off');}catch(_){}updateSnapping();});
    snap.setAttribute('aria-label','Snap to alignment guides');snap.classList.add('keynope-workspace-extra');updateSnapping();
    let rulersOn=false;
    try{rulersOn=localStorage.getItem('keynope.workspace.rulers')==='on';}catch(_){}
    const rulerBars=['horizontal','vertical'].map(axis=>{const node=document.createElement('div');node.className='keynope-ruler keynope-ruler-'+axis;node.setAttribute('role','img');node.setAttribute('aria-label',axis+' slide-coordinate ruler');Object.assign(node.style,{position:'fixed',pointerEvents:'none',overflow:'hidden',background:'#202830',color:'#d5dde5',font:'10px system-ui',zIndex:'3'});document.body.append(node);return node;});
    let rulerGeometry='';
    const paintRulers=()=>{
      rulerBars.forEach(node=>node.hidden=!rulersOn);if(!rulersOn)return;
      const canvas=document.getElementById('presenter-canvas');if(!canvas)return;
      const rect=canvas.getBoundingClientRect(),style=getComputedStyle(document.documentElement);
      const geometry=[rect.left,rect.top,rect.width,rect.height,style.getPropertyValue('--cols'),style.getPropertyValue('--rows')].join(':');
      if(geometry===rulerGeometry)return;
      rulerGeometry=geometry;
      rulerBars.forEach((bar,axis)=>{
        const horizontal=axis===0,length=horizontal?rect.width:rect.height,units=Number(style.getPropertyValue(horizontal?'--cols':'--rows'))||1;
        const bounds=zoomStage.getBoundingClientRect();
        const left=Math.max(bounds.left,rect.left-20),top=Math.max(bounds.top+56,rect.top-20);
        const start=horizontal?Math.max(left+20,rect.left):Math.max(top+20,rect.top);
        const end=horizontal?Math.min(bounds.right,rect.right):Math.min(bounds.bottom-52,rect.bottom);
        const offset=(horizontal?rect.left:rect.top)-start;
        Object.assign(bar.style,{left:(horizontal?start:left)+'px',top:(horizontal?top:start)+'px',width:(horizontal?Math.max(0,end-start):20)+'px',height:(horizontal?20:Math.max(0,end-start))+'px'});
        bar.replaceChildren();const step=[1,2,5,10,20,25,50,100,200,500,1000].find(n=>n*length/units>=45)||Math.ceil(units/5);
        for(let value=0;value<=units;value+=step){const tick=document.createElement('span');tick.textContent=String(value);tick.dataset.coordinate=String(value);Object.assign(tick.style,{position:'absolute',...(horizontal?{left:(offset+value/units*length)+'px',bottom:'0',borderLeft:'1px solid #7e8b98',paddingLeft:'3px',height:'15px'}:{top:(offset+value/units*length)+'px',right:'0',borderTop:'1px solid #7e8b98',paddingTop:'2px',width:'19px',textAlign:'center'})});bar.append(tick);}
      });
    };
    const rulers=button('Rulers',()=>{rulersOn=!rulersOn;try{localStorage.setItem('keynope.workspace.rulers',rulersOn?'on':'off');}catch(_){}updateRulers();resize();requestAnimationFrame(paintRulers);});
    rulers.classList.add('keynope-workspace-extra');rulers.setAttribute('aria-label','Show slide rulers');
    function updateRulers(){document.documentElement.dataset.keynopeRulers=rulersOn?'on':'off';rulers.dataset.state=rulersOn?'ON':'OFF';rulers.setAttribute('aria-pressed',String(rulersOn));paintRulers();}
    updateRulers();
    const rulerObserver=new ResizeObserver(paintRulers);for(const node of [document.getElementById('stage'),document.getElementById('presenter-canvas')])if(node)rulerObserver.observe(node);
    new MutationObserver(paintRulers).observe(document.documentElement,{attributes:true,attributeFilter:['style']});
    window.addEventListener('resize',paintRulers);
    for(const node of quick.querySelectorAll('button'))if(!node.classList.contains('keynope-save-button')&&!node.classList.contains('keynope-history-button'))node.classList.add('keynope-narrow-overflow');
    slide.classList.add('keynope-narrow-overflow');fit.classList.add('keynope-narrow-overflow');
    const morePanel=document.createElement('div');morePanel.id='keynope-workspace-more';morePanel.className='keynope-workspace-more';morePanel.hidden=true;morePanel.setAttribute('role','menu');morePanel.setAttribute('aria-label','More actions');topbar.append(morePanel);
    const closeMore=(focus=false)=>{morePanel.hidden=true;more.setAttribute('aria-expanded','false');if(focus)more.focus();};
    const more=button('•••',()=>{
      if(!morePanel.hidden){closeMore();return;}
      closeNavigator();insertOpen=false;refresh();morePanel.replaceChildren();
      const sources=[...quick.querySelectorAll('button:not(.keynope-save-button):not(.keynope-history-button)'),slide,fit,...toolbar.querySelectorAll('.keynope-workspace-extra'),...document.querySelectorAll('.keynope-app-toolbar .keynope-bottom-overflow'),...header.querySelectorAll('.keynope-ribbon-trailing button')];
      for(const source of sources){
        if(source.hidden)continue;
        const item=document.createElement('button');item.type='button';item.textContent=source.getAttribute('aria-label')||source.title||source.textContent;item.disabled=source.disabled;
        if(source.hasAttribute('aria-pressed')){item.setAttribute('role','menuitemcheckbox');item.setAttribute('aria-checked',source.getAttribute('aria-pressed'));}
        else item.setAttribute('role','menuitem');
        item.onclick=()=>{closeMore();source.click();};morePanel.append(item);
      }
      morePanel.hidden=false;more.setAttribute('aria-expanded','true');morePanel.querySelector('button:not(:disabled)')?.focus();
    },morePanel.id);
    more.className='keynope-more-toggle';more.setAttribute('aria-label','More actions');more.setAttribute('aria-haspopup','menu');more.setAttribute('aria-expanded','false');more.title='More actions';
    morePanel.addEventListener('keydown',event=>{
      event.stopPropagation();
      if(event.key==='Escape'){event.preventDefault();closeMore(true);return;}
      if(event.key==='Tab'){closeMore();return;}
      if(!['ArrowDown','ArrowUp','ArrowRight','ArrowLeft','Home','End'].includes(event.key))return;
      const items=[...morePanel.querySelectorAll('[role="menuitem"],[role="menuitemcheckbox"]')].filter(item=>!item.disabled&&item.getClientRects().length);
      if(!items.length)return;
      event.preventDefault();
      const index=Math.max(0,items.indexOf(document.activeElement));
      const target=event.key==='Home'?0:event.key==='End'?items.length-1:(index+(event.key==='ArrowDown'||event.key==='ArrowRight'?1:-1)+items.length)%items.length;
      items[target].focus();
    });
    let lastGeometry = '',previewVisible=false,previewHeight=0;
    const updateGeometry = () => {
      if(!narrowNavigator()){navigatorOpen=false;closeMore();}
      const navigatorShown=navigatorShowing();
      root.dataset.keynopeNavigatorOpen=String(navigatorShown);
      root.dataset.keynopeInspectorDrawer=String(drawerInspector());
      root.dataset.keynopeInspectorOpen=String(visible);
      slidesToggle.setAttribute('aria-expanded',String(navigatorShown));
      slidesToggle.classList.toggle('active',navigatorShown);
      slidesToggle.dataset.state=navigatorShown?'OPEN':'CLOSED';
      format.dataset.state=visible?'OPEN':'CLOSED';
      inspectorScrim.hidden=!drawerInspector()||!visible;
      inspectorClose.hidden=!drawerInspector();
      navigatorDivider.hidden=narrowNavigator()||!navigatorVisible;
      const width=Math.min(inspectorWidth,Math.max(160,innerWidth-32));
      const navigatorMaximum=Math.min(360,Math.max(160,Math.floor(innerWidth*.28))),navWidth=Math.min(navigatorWidth,navigatorMaximum);
      const navSpace=!narrowNavigator()&&navigatorVisible?navWidth:0;
      const space = (visible||previewVisible) && innerWidth >= 1050 ? width+'px' : '0px';
      const geometry = [space,width,navWidth,navSpace,previewVisible,previewHeight,innerWidth,innerHeight].join(':');
      if (geometry === lastGeometry) return;
      lastGeometry = geometry;
      root.style.setProperty('--keynope-inspector-space', space);
      root.style.setProperty('--keynope-inspector-width',width+'px');
      root.style.setProperty('--keynope-navigator-width',navWidth+'px');
      root.style.setProperty('--keynope-navigator-space',navSpace+'px');
      root.style.setProperty('--keynope-preview-height',previewVisible?previewHeight+'px':'0px');
      root.dataset.keynopePreviewOpen=String(previewVisible);
      navigatorDivider.setAttribute('aria-valuemin','160');navigatorDivider.setAttribute('aria-valuemax',String(navigatorMaximum));navigatorDivider.setAttribute('aria-valuenow',String(navWidth));navigatorDivider.setAttribute('aria-valuetext',navWidth+' pixels');
      const maximum=Math.min(480,Math.max(160,innerWidth-32));
      divider.setAttribute('aria-valuemin',String(Math.min(240,maximum)));divider.setAttribute('aria-valuemax',String(maximum));
      divider.setAttribute('aria-valuenow',String(width));divider.setAttribute('aria-valuetext',width+' pixels');
      resize();
    };
    const setInspectorWidth=value=>{inspectorWidth=Math.round(Math.max(240,Math.min(480,value)));updateGeometry();};
    const rememberWidth=()=>{try {localStorage.setItem('keynope.inspectorWidth',String(inspectorWidth));}catch(_error){}};
    let resizing=null;
    divider.addEventListener('pointerdown',event=>{
      if(event.button!==0)return;event.preventDefault();event.stopPropagation();
      resizing={id:event.pointerId,x:event.clientX,width:Number(divider.getAttribute('aria-valuenow'))};divider.setPointerCapture(event.pointerId);divider.focus();
      root.dataset.keynopeResizingInspector='true';
    });
    divider.addEventListener('pointermove',event=>{if(resizing?.id===event.pointerId)setInspectorWidth(resizing.width+resizing.x-event.clientX);});
    const finishResize=()=>{if(!resizing)return;resizing=null;delete root.dataset.keynopeResizingInspector;rememberWidth();};
    divider.addEventListener('pointerup',finishResize);divider.addEventListener('pointercancel',finishResize);divider.addEventListener('lostpointercapture',finishResize);window.addEventListener('blur',finishResize);
    divider.addEventListener('dblclick',()=>{setInspectorWidth(300);rememberWidth();});
    divider.addEventListener('keydown',event=>{
      event.stopPropagation();const step=event.shiftKey?50:10;
      if(!['ArrowLeft','ArrowRight','Home','End'].includes(event.key))return;
      event.preventDefault();setInspectorWidth(event.key==='Home'?Number(divider.getAttribute('aria-valuemin')):event.key==='End'?Number(divider.getAttribute('aria-valuemax')):Number(divider.getAttribute('aria-valuenow'))+(event.key==='ArrowLeft'?step:-step));rememberWidth();
    });
    const setNavigatorWidth=value=>{navigatorWidth=Math.round(Math.max(160,Math.min(360,value)));updateGeometry();};
    const rememberNavigator=()=>{try{localStorage.setItem('keynope.navigatorWidth',String(navigatorWidth));}catch(_error){}};
    let resizingNavigator=null;
    navigatorDivider.addEventListener('pointerdown',event=>{if(event.button!==0)return;event.preventDefault();event.stopPropagation();resizingNavigator={id:event.pointerId,x:event.clientX,width:Number(navigatorDivider.getAttribute('aria-valuenow'))};navigatorDivider.setPointerCapture(event.pointerId);navigatorDivider.focus();root.dataset.keynopeResizingNavigator='true';});
    navigatorDivider.addEventListener('pointermove',event=>{if(resizingNavigator?.id===event.pointerId)setNavigatorWidth(resizingNavigator.width+event.clientX-resizingNavigator.x);});
    const finishNavigatorResize=()=>{if(!resizingNavigator)return;resizingNavigator=null;delete root.dataset.keynopeResizingNavigator;rememberNavigator();};
    for(const type of ['pointerup','pointercancel','lostpointercapture'])navigatorDivider.addEventListener(type,finishNavigatorResize);window.addEventListener('blur',finishNavigatorResize);
    navigatorDivider.addEventListener('dblclick',()=>{setNavigatorWidth(210);rememberNavigator();});
    navigatorDivider.addEventListener('keydown',event=>{event.stopPropagation();if(!['ArrowLeft','ArrowRight','Home','End'].includes(event.key))return;event.preventDefault();const step=event.shiftKey?50:10;setNavigatorWidth(event.key==='Home'?160:event.key==='End'?Number(navigatorDivider.getAttribute('aria-valuemax')):Number(navigatorDivider.getAttribute('aria-valuenow'))+(event.key==='ArrowLeft'?-step:step));rememberNavigator();});
    function refresh() {
      const paintOwner=selectionPaintTarget?.(),paintKey=JSON.stringify([paintOwner?.epoch,paintOwner?.owner]);
      if(paintOwnerKey!==paintKey){paintOwnerKey=paintKey;paintError.hidden=true;}
      const slideControlsDisabled=editing||context==='master'||(canUseSlidePresets&&!canUseSlidePresets());
      presets.hidden=editing||(canUseSlidePresets&&!canUseSlidePresets());
      // Object projection parses every authored query and sorts the complete
      // stack. Own one immutable snapshot for this refresh instead of doing
      // that work independently for opacity, groups, locking and navigation.
      const scope=groupScope?.()||'';
      const allObjects=section!=='slide'||scope?(objects?.()||[]):[];
      const opacityItems=allObjects.filter(item=>item.selected);
      const members=allObjects.filter(item=>item.group===scope);
      scopeBar.hidden=!scope;setChromeProperty(scopeLabel,'textContent',' › Group · '+members.length+' objects');scopeExit.disabled=editing||changingGroup;
      const groups=new Set(opacityItems.map(item=>item.group).filter(Boolean));
      const groupingLocked=opacityItems.some(item=>item.locked)||allObjects.some(item=>groups.has(item.group)&&item.locked);
      groupButtons.forEach((button,index)=>{button.hidden=!groupAction;button.disabled=editing||changingGroup||movingLayer||(index===0?(opacityItems.length<2||groupingLocked):index===1?(!groups.size||groupingLocked):(!oneStackUnit(opacityItems)||!groups.size||!!scope));});
      lock.hidden=!setObjectLock;setChromeProperty(lock,'textContent',opacityItems.length&&opacityItems.every(item=>item.locked)?'Unlock objects':'Lock objects');lock.disabled=editing||movingLayer||!opacityItems.length;
      const hasLocked=opacityItems.some(item=>item.locked);
      renameButton.disabled=editing||movingLayer||savingName||opacityItems.length!==1||hasLocked;
      if(naming&&(naming.capture?.epoch!==selectionPaintTarget?.()?.epoch||!allObjects.some(item=>item.id===naming.id)))closeRename();
      renameForm.hidden=!naming||section!=='objects';
      const describedImage=opacityItems.length===1&&opacityItems[0].kind==='image'?opacityItems[0]:null;
      imageDescription.hidden=!setImageDescription||!describedImage||section==='slide';
      const descriptionContext=selectionPaintTarget?.(),descriptionKey=JSON.stringify([describedImage?.id,descriptionContext?.epoch,descriptionContext?.owner,descriptionContext?.slide,descriptionContext?.master,descriptionContext?.group]);
      if(descriptionOwner!==descriptionKey){descriptionOwner=descriptionKey;descriptionTarget=describedImage?.id||'';descriptionDirty=false;descriptionCapture=null;descriptionError.hidden=true;}
      if(!descriptionDirty&&!savingDescription){descriptionInput.value=describedImage?.alt||'';decorativeInput.checked=!!describedImage?.decorative;descriptionCapture={...descriptionContext,alt:describedImage?.alt||'',decorative:!!describedImage?.decorative};}
      decorativeInput.disabled=descriptionSave.disabled=editing||savingDescription||hasLocked;descriptionInput.disabled=decorativeInput.disabled||decorativeInput.checked;
      descriptionReset.disabled=editing||savingDescription||!descriptionDirty;
      visibility.hidden=!setObjectVisibility;setChromeProperty(visibility,'textContent',opacityItems.length&&opacityItems.every(item=>item.hidden)?'Show objects':'Hide objects');visibility.disabled=editing||movingLayer||!opacityItems.length||hasLocked;
      layerControls.hidden=section!=='objects'||!moveObjectLayer;
      for(const button of layerButtons)button.disabled=editing||movingLayer||!oneStackUnit(opacityItems)||hasLocked;
      const opacityState=selectionCapabilities?.()?.properties?.['paint.opacity'];
      const paintStates=selectionCapabilities?.()?.properties||{};
      setChromeProperty(paintPanel,'hidden',!setSelectionPaint||section!=='style'||!paintFields.some(field=>paintStates[field.property]?.eligibleIds?.length));
      for(const field of paintFields){
        const state=paintStates[field.property],count=state?.eligibleIds?.length||0;
        setChromeProperty(field.row,'hidden',!count);setChromeProperty(field.control,'disabled',editing||!state?.editable);
        if(field.inherit)setChromeProperty(field.inherit,'disabled',field.control.disabled||state?.provenance==='inherited');
        if(field.off)setChromeProperty(field.off,'disabled',field.control.disabled);
        setChromeProperty(field.subset,'textContent',count&&count<state.totalCount?' · '+count+' of '+state.totalCount:'');
        setChromeProperty(field.control,'title',state?.reason||((state?.provenance||'')+' · Applies to '+count+' of '+(state?.totalCount||0)+' selected objects'));
        setChromeProperty(field.control.dataset,'provenance',state?.provenance||'');
        if(field.property==='paint.colorToken'||field.property==='paint.strokeToken'||field.property==='label.colorToken'){
          const colors=getTheme?.()?.colors;
          for(const [role,label]of themeColourChoices){
            const option=[...field.control.options].find(option=>option.value===role);
            const color=role.startsWith('accent-')?colors?.accent?.[Number(role.slice(7))-1]:colors?.[role];
            const text=role==='custom'&&state?.provenance==='inherited'?'Inherited colour':color?label+' · '+color:label;
            if(option&&option.textContent!==text)option.textContent=text;
          }
        }
        if(field.type==='color'){
          const color=state?.state==='value'?state.value:'';
          const label=state?.state==='mixed'?'Mixed':color==='none'?'Off':color||'Not set';
          if(field.control.textContent!==label)field.control.textContent=label;
          const swatch=color&&color!=='none'?color:'transparent';
          if(field.control.style.getPropertyValue('--paint-color')!==swatch)field.control.style.setProperty('--paint-color',swatch);
          const description=state?.state==='mixed'?'Mixed colours':color==='none'?'Stroke off':color||'No explicit colour';
          if(field.control.getAttribute('aria-description')!==description)field.control.setAttribute('aria-description',description);
        }else if(!paintWriter.busy(field.property)&&(document.activeElement!==field.control||field.control.disabled)){
          const value=field.property==='label.font'&&state?.state==='value'&&state.fontBinding==='theme'?'theme':state?.state==='mixed'?(field.type==='select'?'mixed':''):state?.state==='value'?String(state.value):'';
          if(field.type==='select')globalThis.KeynopeFonts?.enhance(field.control);
          setChromeProperty(field.control,'value',value);
          if(field.type==='number')setChromeProperty(field.control,'placeholder',state?.state==='mixed'?'Mixed':'');
        }
      }
      setChromeProperty(gradientPaint,'hidden',!paintStates['paint.gradientDirection']?.eligibleIds?.length);setChromeProperty(shadowPaint,'hidden',!paintStates['paint.shadow']?.eligibleIds?.length);
      for(const panel of [colourEffects,blurEffects,glowEffects])setChromeProperty(panel,'hidden',!paintStates['paint.blur']?.eligibleIds?.length);
      const mode=paintStates['paint.colourEffect']?.value;
      for(const field of paintFields){if(['paint.effectShadows','paint.effectHighlights','paint.effectMidtones'].includes(field.property))setChromeProperty(field.row,'hidden',!['duotone','tritone'].includes(mode)||field.property==='paint.effectMidtones'&&mode!=='tritone');}
      setChromeProperty(strokePaint,'hidden',!paintStates['paint.strokeColor']?.eligibleIds?.length);
      setChromeProperty(linePaint,'hidden',!paintStates['line.width']?.eligibleIds?.length);
      setChromeProperty(labelPaint,'hidden',!paintStates['label.size']?.eligibleIds?.length);
      opacityLabel.hidden=!setObjectOpacity||section!=='style'||(opacityState?!opacityState.eligibleIds.length:!opacityItems.length);
      opacityInput.disabled=editing||savingOpacity||(opacityState?!opacityState.editable:hasLocked);
      opacityInput.dataset.selectionProperty='paint.opacity';
      opacityInput.title=opacityState?.reason||('Object opacity'+(opacityState?.provenance==='inherited'?' · Inherited':''));
      if(document.activeElement!==opacityInput){
        const values=opacityItems.map(item=>Math.round((Number.isFinite(item.opacity)?item.opacity:1)*100));
        opacityInput.value=opacityState?(opacityState.state==='value'?String(Math.round(opacityState.value*100)):''):(values.length&&values.every(value=>value===values[0])?String(values[0]):'');
        opacityInput.placeholder=opacityState?.state==='mixed'?'Mixed':opacityState?'':'Mixed';
      }
      main.hidden = !insertOpen;
      themeSelect.disabled=editing||savingTheme;themeSelect.value=appearance?.()?.theme||'';
      editTheme.disabled=editing||savingTheme||!getTheme?.();
      editBuildsButton.disabled=editing;
      if(getTheme?.()&&setDeckTheme){themeLabel.firstChild.textContent='Colour preset';editTheme.textContent='Edit theme…';}
      inspector.hidden = !visible;
      divider.hidden=!visible;
      selection.hidden = false;
      insert.setAttribute('aria-expanded', String(insertOpen));
      format.setAttribute('aria-expanded', String(visible));
      format.classList.toggle('active', visible);
      slide.classList.toggle('active', visible && section === 'slide');
      setChromeProperty(title,'textContent',section==='objects'?'Objects':section === 'slide' ? (context === 'master' ? 'Master slide' : 'Slide') : context);
      setChromeProperty(hint,'textContent',section==='objects'?'Select an object, including those behind other objects.':section === 'slide' ? 'Layout and slide appearance' : editing ? 'Editing text' : 'Format the current selection');
      if(section==='objects')refreshObjects(allObjects);
      for (const [id, panel] of Object.entries(panels)) panel.hidden = id === 'insert' ? false : id !== section;
      for (const item of tabs.children) {
        const selected = item.dataset.ribbonTab === section;
        item.setAttribute('aria-selected', String(selected)); item.tabIndex = selected ? 0 : -1;
      }
      updateGeometry();
    }
    function selectPanel(id, focus = false) {
      if (id === 'insert') { insertOpen = true; refresh(); return; }
      if (editing && id === 'arrange') return;
      section = id; visible = true; remembered.set(context, id); refresh();
      if (focus) tabs.querySelector('[data-ribbon-tab="' + id + '"]')?.focus();
    }
    function configure(nextContext, nextEditing) {
      const changed = context !== nextContext;
      if (changed || editing !== nextEditing) {
        context = nextContext; editing = nextEditing;
        const empty = context === 'main' || context === 'master';
        const choices = empty ? [['slide', 'Slide']] : context === 'Line' ? [['content', 'Line'], ['style', 'Style'], ['slide', 'Slide']] : [['content', context], ['style', 'Style'], ['arrange', 'Arrange'], ['slide', 'Slide']];
        if(objects)choices.push(['objects','Objects']);
        // Arrange/Style are workflows shared by different object kinds. Keep
        // the user's current workflow when the new selection supports it;
        // only fall back to a contextual tab when it is no longer available.
        const sharedSection=['arrange','style','objects'].includes(section)&&choices.some(([id])=>id===section);
        if (changed&&!sharedSection) section = remembered.get(context) || choices[0][0];
        if (editing) section = 'content';
        tabs.replaceChildren();
        for (const [id, label] of choices) {
          const node = document.createElement('button');
          node.type = 'button'; node.className = 'keynope-ribbon-tab'; node.textContent = label;
          node.dataset.ribbonTab = id; node.id = 'keynope-ribbon-tab-' + id;
          node.setAttribute('role', 'tab'); node.setAttribute('aria-controls', panels[id].id);
          node.disabled = editing && id === 'arrange';
          node.onclick = () => selectPanel(id);
          node.onkeydown = event => {
            event.stopPropagation();
            if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
            event.preventDefault();
            const nodes = [...tabs.children].filter(n => !n.disabled), i = nodes.indexOf(node);
            const direction=getComputedStyle(tabs).direction==='rtl'?-1:1;
            const target = event.key === 'Home' ? 0 : event.key === 'End' ? nodes.length - 1 : (i + (event.key === 'ArrowRight' ? direction : -direction) + nodes.length) % nodes.length;
            selectPanel(nodes[target].dataset.ribbonTab, true);
          };
          tabs.append(node);
        }
      }
      refresh();
    }
    main.addEventListener('click', event => {
      // Keep size/font controls usable; inserting an object closes the browser.
      const target = event.target.closest('button');
      if (target && /^(Add |Insert |Import )/.test(target.title) && !target.hasAttribute('aria-haspopup')) { insertOpen = false; refresh(); }
    });
    document.addEventListener('pointerdown', event => {
      if (insertOpen && !main.contains(event.target) && !insert.contains(event.target)) { insertOpen = false; refresh(); }
      if(narrowNavigator()&&navigatorOpen&&!event.target.closest('#keynope-slide-navigator')&&!slidesToggle.contains(event.target))closeNavigator();
      if(!morePanel.hidden&&!morePanel.contains(event.target)&&!more.contains(event.target))closeMore();
    });
    document.addEventListener('click',event=>{if(narrowNavigator()&&event.target.closest('#keynope-slide-navigator .keynope-slide-item'))closeNavigator(true);},true);
    // Own drawer Escape before the editor's document-level canvas shortcuts.
    window.addEventListener('keydown',event=>{if(event.target?.closest?.('dialog,[role="dialog"],[role="menu"]'))return;if(dismissInsert(event)){event.stopImmediatePropagation();return;}if(narrowNavigator()&&navigatorOpen&&event.key==='Escape'){event.preventDefault();event.stopImmediatePropagation();closeNavigator(true);}},true);
    window.addEventListener('keydown',event=>{if(drawerInspector()&&visible&&event.key==='Escape'&&!event.target?.closest?.('dialog,[role="dialog"],[role="menu"]')){event.preventDefault();event.stopImmediatePropagation();closeInspector(true);}},true);
    function dismissInsert(event) {
      if(event.key!=='Escape'||!insertOpen||event.target?.closest?.('dialog,[role="dialog"],[role="menu"]'))return false;
      event.preventDefault();event.stopPropagation();insertOpen=false;refresh();insert.focus();return true;
    }
    topbar.addEventListener('keydown',dismissInsert);
    window.addEventListener('resize', updateGeometry);
    configure('main', false);
    // Pane focus is navigation, not a document selection or mutation. Share
    // these commands with the native View menu instead of duplicating logic.
    const stage=document.getElementById('stage');
    if(stage){stage.tabIndex=0;stage.setAttribute('aria-label','Slide canvas');}
    const blocked=()=>[...document.querySelectorAll('dialog[open],[aria-modal="true"],[role="menu"]')].some(node=>node.getClientRects().length);
    window.keynopeWorkspaceState=()=>({enabled:!blocked(),navigator:navigatorShowing(),format:visible,rulers:rulersOn,snap:snapping});
    let focusedPane=null;
    const clearPaneFocus=()=>{focusedPane?.classList.remove('keynope-pane-focus');focusedPane=null;};
    document.addEventListener('pointerdown',clearPaneFocus,true);
    document.addEventListener('focusin',event=>{if(focusedPane&&!focusedPane.contains(event.target))clearPaneFocus();});
    const focusPane=(backwards=false)=>{
      const candidates=[header,document.getElementById('keynope-slide-navigator'),stage,visible?inspector:null,document.querySelector('.keynope-app-toolbar')].filter(node=>node?.getClientRects().length);
      if(!candidates.length)return false;
      const current=candidates.findIndex(node=>node.contains(document.activeElement));
      const next=current<0?(backwards?candidates.length-1:0):(current+(backwards?-1:1)+candidates.length)%candidates.length;
      const target=candidates[next];
      const controls=[...target.querySelectorAll('button,input,select,textarea,[tabindex]')].filter(node=>!node.disabled&&node.tabIndex>=0&&node.getClientRects().length);
      const control=target===stage?target:controls.find(node=>node.getAttribute('aria-selected')==='true'||node.getAttribute('aria-current')==='page')||controls[0];
      control?.focus({preventScroll:true});
      if(!control||document.activeElement!==control)return false;
      clearPaneFocus();focusedPane=target;target.classList.add('keynope-pane-focus');return true;
    };
    window.keynopeWorkspaceCommand=command=>{
      if(blocked())return false;
      const commands={insert:()=>insert.click(),format:()=>{format.click();if(visible)tabs.querySelector('[aria-selected="true"]')?.focus();},slide:()=>slide.click(),fit:()=>fit.click(),'zoom-in':()=>setZoom(viewport.zoom+.1),'zoom-out':()=>setZoom(viewport.zoom-.1),rulers:()=>rulers.click(),snap:()=>snap.click(),navigator:()=>slidesToggle.click(),'focus-next':()=>focusPane()};
      if(!Object.hasOwn(commands,command))return false;
      return commands[command]()!==false;
    };
    window.addEventListener('keydown',event=>{
      if(event.key!=='F6'||event.ctrlKey||event.altKey||event.metaKey||event.isComposing||blocked())return;
      event.preventDefault();event.stopImmediatePropagation();focusPane(event.shiftKey);
    },true);
    function bindCommand(source, label) {
      const mirror = button(label, () => { if (!source.hidden && !source.disabled) source.click(); });
      mirror.classList.add('keynope-workspace-extra');
      const sync = () => { mirror.disabled = source.disabled; mirror.hidden = source.hidden; };
      new MutationObserver(sync).observe(source, {attributes:true, attributeFilter:['hidden','disabled']});
      sync();
    }
    const setPreview=(shown,height)=>{const h=Math.ceil(height||0);if(previewVisible===shown&&previewHeight===h)return;previewVisible=shown;previewHeight=h;updateGeometry();};
    return {configure, selectPanel, bindCommand, dismissInsert,setPreview};
  }
  return {selectionPaintWriter,applyScenePagePatches,mount,thumbnails,distribute,snapMove,pointerMove,marqueeSelectionUnits,boxIntersectsRect,marqueeCandidates,sameLayerContext,matchSize,slideKeyboard,toolbarKeyboard,sceneSelectionBounds,cellAspect,visualBounds,clampMove,connectorIndex,rotateSelection,resizeSelection,resizeConnectedSelection,alignmentMoves};
})();
