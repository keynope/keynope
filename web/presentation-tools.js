(function(global){
  'use strict';
  const ns='http://www.w3.org/2000/svg',modes=['off','laser','spotlight','ink'],color=/^#[0-9a-f]{6}$/i,id=/^[A-Za-z0-9_-]{1,64}$/;
  const clone=value=>JSON.parse(JSON.stringify(value)),point=p=>Array.isArray(p)&&p.length===2&&p.every(v=>Number.isFinite(v)&&v>=0&&v<=1);
  const contextKey=o=>o?[o.session,o.slideId,o.slide,o.page].join(':'):'';
  function normalize(input){
    if(!input||typeof input.session!=='string'||!input.session||input.session.length>256||typeof input.slideId!=='string'||!input.slideId||input.slideId.length>256||!Number.isInteger(input.slide)||input.slide<0||!Number.isInteger(input.page)||input.page<0||!Number.isSafeInteger(input.revision)||input.revision<0||!modes.includes(input.mode)||!color.test(input.color)||!Number.isFinite(input.width)||input.width<1||input.width>24||!Array.isArray(input.strokes)||input.strokes.length>32)return null;
    if(input.pointer!=null&&(!point(input.pointer)||!['laser','spotlight'].includes(input.mode)))return null;
    const seen=new Set();let count=0;
    for(const s of input.strokes){if(!s||!id.test(s.id)||seen.has(s.id)||!color.test(s.color)||!Number.isFinite(s.width)||s.width<1||s.width>24||!Array.isArray(s.points)||!s.points.length||s.points.length>512||s.points.some(p=>!point(p)))return null;seen.add(s.id);count+=s.points.length;if(count>2048)return null;}
    return {session:input.session,slideId:input.slideId,slide:input.slide,page:input.page,revision:input.revision,mode:input.mode,color:input.color,width:input.width,pointer:input.pointer?input.pointer.slice():null,strokes:input.strokes.map(s=>({id:s.id,color:s.color,width:s.width,points:s.points.map(p=>p.slice())}))};
  }
  function create(host,{readOnly=false,send=null,onError=()=>{},onChange=()=>{}}={}){
    const doc=host.ownerDocument,svg=doc.createElementNS(ns,'svg'),ink=doc.createElementNS(ns,'g'),shade=doc.createElementNS(ns,'path'),halo=doc.createElementNS(ns,'circle'),dot=doc.createElementNS(ns,'circle');
    svg.classList.add('keynope-presentation-overlay');svg.setAttribute('viewBox','0 0 1920 1080');svg.setAttribute('preserveAspectRatio','none');svg.setAttribute('aria-hidden','true');
    svg.style.cssText='position:absolute;inset:0;width:100%;height:100%;overflow:hidden;pointer-events:none;touch-action:none;z-index:80';
    shade.setAttribute('fill','#000');shade.setAttribute('fill-opacity','.68');shade.setAttribute('fill-rule','evenodd');
    halo.setAttribute('r','18');halo.setAttribute('fill-opacity','.24');dot.setAttribute('r','7');dot.setAttribute('stroke','#fff');dot.setAttribute('stroke-width','2');svg.append(shade,ink,halo,dot);host.append(svg);
    const clientId='tools-'+Array.from(crypto.getRandomValues(new Uint8Array(12)),b=>b.toString(16).padStart(2,'0')).join(''),paths=new Map();
    let state=null,visible=false,interactive=!readOnly,active=null,captured=null,epoch=0,sequence=0,pending=null,sending=false,timer=null,lastSend=0,serverRevision=-1,disposed=false,limitNotified=false,history=[],future=[];
    function checkpoint(){history.push(clone(state.strokes));if(history.length>32)history.shift();future=[];}
    function paint(){
      svg.style.display=visible&&state?'block':'none';svg.style.pointerEvents=visible&&interactive&&state?.mode!=='off'?'auto':'none';svg.style.cursor=state?.mode==='ink'?'crosshair':'none';
      const present=new Set();
      for(const stroke of state?.strokes||[]){
        present.add(stroke.id);let path=paths.get(stroke.id);if(!path){path=doc.createElementNS(ns,'path');path.setAttribute('fill','none');path.setAttribute('stroke-linecap','round');path.setAttribute('stroke-linejoin','round');ink.append(path);paths.set(stroke.id,path);}
        const coordinates=stroke.points.map(p=>[p[0]*1920,p[1]*1080]);
        const d=coordinates.map((p,i)=>(i?'L':'M')+p.map(v=>v.toFixed(2)).join(' ')).join(' ')+(coordinates.length===1?' l .01 0':'');
        if(path.getAttribute('d')!==d)path.setAttribute('d',d);if(path.getAttribute('stroke')!==stroke.color)path.setAttribute('stroke',stroke.color);if(path.getAttribute('stroke-width')!==String(stroke.width))path.setAttribute('stroke-width',stroke.width);
      }
      for(const [key,path]of paths)if(!present.has(key)){path.remove();paths.delete(key);}
      const p=state?.pointer,x=(p?.[0]||0)*1920,y=(p?.[1]||0)*1080;
      shade.style.display=p&&state.mode==='spotlight'?'':'none';halo.style.display=dot.style.display=p&&state.mode==='laser'?'':'none';
      if(p&&state.mode==='spotlight')shade.setAttribute('d',`M0 0H1920V1080H0Z M${x-110} ${y}a110 110 0 1 0 220 0a110 110 0 1 0 -220 0Z`);
      if(p&&state.mode==='laser')for(const node of [halo,dot]){node.setAttribute('cx',x);node.setAttribute('cy',y);node.setAttribute('fill',state.color);}
      onChange(state);
    }
    function cancelGesture(){if(captured!=null&&svg.hasPointerCapture?.(captured))svg.releasePointerCapture(captured);captured=null;active=null;}
    function apply(input,{force=false}={}){
      const next=normalize(input);if(!next)return false;
      if(contextKey(next)!==contextKey(state)){epoch++;pending=null;cancelGesture();serverRevision=-1;limitNotified=false;history=[];future=[];}
      else if(!force&&(sending||pending||next.revision<=serverRevision))return false;
      else if(JSON.stringify(next.strokes)!==JSON.stringify(state.strokes)){history=[];future=[];}
      serverRevision=next.revision;state=next;paint();return true;
    }
    function context(input){
      if(!input){if(state){epoch++;pending=null;cancelGesture();state=null;serverRevision=-1;history=[];future=[];paint();}return;}
      if(contextKey(input)===contextKey(state))return;
      const style=state?.session===input.session?{mode:state.mode,color:state.color,width:state.width}:{mode:'off',color:'#ff3355',width:6};
      apply({...input,revision:0,...style,pointer:null,strokes:[]},{force:true});
    }
    async function flush(){
      if(disposed||sending||!pending)return;
      clearTimeout(timer);timer=null;const request=pending,generation=epoch;pending=null;sending=true;lastSend=performance.now();
      try{const result=send?await send(request):request.overlay;if(disposed||generation!==epoch)return;if(!pending&&result)apply(result,{force:true});}
      catch(error){if(generation===epoch){pending=null;onError(error);}}
      finally{sending=false;if(pending&&!disposed)schedule();}
    }
    function schedule(urgent=false){clearTimeout(timer);timer=null;if(sending)return;const delay=urgent?0:Math.max(0,200-(performance.now()-lastSend));timer=setTimeout(flush,delay);}
    function changed(urgent=false){if(!state)return;state.revision++;pending={clientId,sequence:++sequence,overlay:clone(state)};paint();schedule(urgent);}
    function setMode(mode){if(!state||readOnly||!modes.includes(mode))return;cancelGesture();state.mode=state.mode===mode?'off':mode;state.pointer=null;changed(true);}
    function position(event){const r=svg.getBoundingClientRect();if(r.width<1||r.height<1)return null;const x=(event.clientX-r.left)/r.width,y=(event.clientY-r.top)/r.height;if(x<0||x>1||y<0||y>1)return null;return [x,y].map(v=>Math.round(v*1000000)/1000000);}
    function reportLimit(){if(!limitNotified){limitNotified=true;onError(new Error('Temporary ink limit reached. Clear ink to keep drawing.'));}}
    function move(event){
      if(!state||!visible||!interactive)return;const p=position(event);
      if(['laser','spotlight'].includes(state.mode)){if(JSON.stringify(p)!==JSON.stringify(state.pointer)){state.pointer=p;changed();}return;}
      if(state.mode!=='ink'||!active||!p||event.pointerId!==captured)return;
      if(!(event.buttons&1)&&event.pointerType==='mouse'){cancelGesture();changed(true);return;}
      const stroke=state.strokes.find(s=>s.id===active);if(!stroke)return;
      const previous=stroke.points.at(-1);if(Math.hypot((p[0]-previous[0])*1920,(p[1]-previous[1])*1080)<2)return;
      if(stroke.points.length>=512||state.strokes.reduce((n,s)=>n+s.points.length,0)>=2048){reportLimit();return;}
      stroke.points.push(p);changed();
    }
    if(!readOnly){
      svg.addEventListener('pointerdown',event=>{if(!state||!visible||!interactive||state.mode==='off'||event.button!==0)return;event.preventDefault();event.stopImmediatePropagation();if(state.mode!=='ink'){move(event);return;}const p=position(event);if(!p)return;if(state.strokes.length>=32||state.strokes.reduce((n,s)=>n+s.points.length,0)>=2048){reportLimit();return;}checkpoint();active='ink-'+(++sequence);captured=event.pointerId;svg.setPointerCapture(event.pointerId);state.strokes.push({id:active,color:state.color,width:state.width,points:[p]});changed(true);});
      svg.addEventListener('pointermove',event=>{if(state?.mode!=='off'){event.stopImmediatePropagation();move(event);}});
      const end=event=>{if(captured!==event.pointerId)return;event.stopImmediatePropagation();cancelGesture();changed(true);};
      svg.addEventListener('pointerup',end);svg.addEventListener('pointercancel',end);svg.addEventListener('lostpointercapture',end);
      svg.addEventListener('pointerleave',()=>{if(state?.pointer){state.pointer=null;changed(true);}});
      svg.addEventListener('click',event=>{if(state?.mode!=='off'){event.preventDefault();event.stopImmediatePropagation();}});
    }
    return {
      element:svg,apply,context,setMode,snapshot:()=>state?clone(state):null,
      canUndo:()=>history.length>0||!!state?.strokes.length,canRedo:()=>future.length>0,
      setVisible(value,allowInput=true){const was=visible;visible=!!value;interactive=!readOnly&&allowInput;if(was&&!visible)cancelGesture();if(was!==visible||svg.style.pointerEvents!==(visible&&interactive&&state?.mode!=='off'?'auto':'none'))paint();},
      setStyle({color:c,width}){if(!state||readOnly)return;if(c!=null&&color.test(c))state.color=c;if(Number.isFinite(width)&&width>=1&&width<=24)state.width=width;changed(true);},
      undo(){if(state&&(history.length||state.strokes.length)){cancelGesture();future.push(clone(state.strokes));if(history.length)state.strokes=history.pop();else state.strokes.pop();limitNotified=false;changed(true);}},
      redo(){if(state&&future.length){cancelGesture();history.push(clone(state.strokes));state.strokes=future.pop();limitNotified=false;changed(true);}},
      clear(){if(state&&state.strokes.length){checkpoint();cancelGesture();state.strokes=[];state.pointer=null;limitNotified=false;changed(true);}},
      exit(){if(state&&state.mode!=='off'){cancelGesture();state.mode='off';state.pointer=null;changed(true);return true;}return false;},
      dispose(){disposed=true;epoch++;clearTimeout(timer);pending=null;cancelGesture();paths.clear();svg.remove();}
    };
  }
  function toolbar(container,tools){
    const doc=container.ownerDocument,launch=doc.createElement('button'),panel=doc.createElement('div');
    launch.type='button';launch.className='keynope-app-icon-button';launch.title='Presenter tools';launch.setAttribute('aria-label','Presenter tools');launch.setAttribute('aria-expanded','false');
    launch.innerHTML='<svg viewBox="0 0 64 64" aria-hidden="true"><path d="m15 49 7-18L45 8l11 11-23 23Z" fill="none" stroke="#fff" stroke-width="3"/><path d="m22 31 11 11M43 10l11 11" stroke="#9b9b9b" stroke-width="3"/><circle cx="12" cy="15" r="7" fill="none" stroke="#ff3355" stroke-width="3"/></svg>';
    panel.className='keynope-presenter-tools-panel';panel.setAttribute('role','toolbar');panel.setAttribute('aria-label','Presentation tools');panel.hidden=true;
    panel.style.cssText='position:fixed;right:20px;bottom:72px;z-index:2200;max-width:calc(100vw - 40px);display:flex;flex-wrap:wrap;gap:8px;align-items:center;padding:12px;border:1px solid #596674;background:#182028;color:#edf1f3;box-shadow:0 8px 30px #0008';
    const buttons=new Map();
    const button=(label,action)=>{const b=doc.createElement('button');b.type='button';b.textContent=label;b.setAttribute('aria-label',label);b.style.cssText='font:inherit;color:inherit;background:#26323e;border:1px solid #65717a;padding:6px 10px;cursor:pointer';b.onclick=action;panel.append(b);return b;};
    for(const [mode,label]of [['laser','Laser pointer'],['spotlight','Spotlight'],['ink','Temporary ink']]){const b=button(label,()=>tools.setMode(mode));b.setAttribute('aria-pressed','false');buttons.set(mode,b);}
    const paint=doc.createElement('input');paint.type='color';paint.value='#ff3355';paint.title='Annotation colour';paint.setAttribute('aria-label',paint.title);paint.style.cssText='width:38px;height:32px;padding:2px;background:#26323e;border:1px solid #65717a';paint.oninput=()=>tools.setStyle({color:paint.value});panel.append(paint);
    const width=doc.createElement('select');width.setAttribute('aria-label','Ink width');for(const value of [3,6,12,24]){const option=doc.createElement('option');option.value=value;option.textContent=value+' px';width.append(option);}width.value='6';width.onchange=()=>tools.setStyle({width:Number(width.value)});panel.append(width);
    const undo=button('Undo ink',()=>tools.undo()),redo=button('Redo ink',()=>tools.redo()),clear=button('Clear ink',()=>tools.clear());button('Close tools',()=>{panel.hidden=true;panel.style.display='none';launch.setAttribute('aria-expanded','false');launch.focus();});
    panel.style.display='none';container.append(launch);doc.body.append(panel);
    launch.onclick=()=>{panel.hidden=!panel.hidden;panel.style.display=panel.hidden?'none':'flex';launch.setAttribute('aria-expanded',String(!panel.hidden));};
    panel.addEventListener('keydown',event=>{if(event.key==='Escape'){event.preventDefault();event.stopPropagation();tools.exit();panel.hidden=true;panel.style.display='none';launch.setAttribute('aria-expanded','false');launch.focus();}});
    return {button:launch,update(state,enabled){launch.hidden=!enabled;launch.disabled=!enabled;if(!enabled){panel.hidden=true;panel.style.display='none';launch.setAttribute('aria-expanded','false');}for(const [mode,b]of buttons){const active=state?.mode===mode;b.setAttribute('aria-pressed',String(active));b.style.background=active?'#075985':'#26323e';}launch.classList.toggle('active',!!state&&state.mode!=='off');undo.disabled=!tools.canUndo();redo.disabled=!tools.canRedo();clear.disabled=!state?.strokes.length;if(state){if(doc.activeElement!==paint)paint.value=state.color;width.value=String(state.width);}},dispose(){launch.remove();panel.remove();}};
  }
  global.KeynopePresentationTools={normalize,contextKey,create,toolbar};
})(typeof globalThis==='object'?globalThis:window);
