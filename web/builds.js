const KeynopeBuilds=(()=>{
  'use strict';
  const copy=value=>JSON.parse(JSON.stringify(value)),directions=['left','right','up','down'];
  let activeEditor=null;
  function cursorKey(cursor){
    if(!cursor||typeof cursor.session!=='string'||!cursor.session||cursor.session.length>256||typeof cursor.slideId!=='string'||!cursor.slideId||cursor.slideId.length>256||!Number.isSafeInteger(cursor.generation)||cursor.generation<0||!Number.isSafeInteger(cursor.step)||cursor.step<0||cursor.step>1000||!Number.isSafeInteger(cursor.startedAtMs)||cursor.startedAtMs<0||!['enter','advance','seek'].includes(cursor.motion)||typeof cursor.paused!=='boolean')return '';
    return JSON.stringify([cursor.session,cursor.slideId,cursor.generation,cursor.step,cursor.startedAtMs,cursor.motion,cursor.paused]);
  }
  function normalize(input,ids){
    const allowed=ids?new Set(ids):null,seen=new Set();
    const duration=n=>Math.max(0,Math.min(60000,Number.isFinite(Number(n))?Number(n):0));
    const transition=input?.transition||{};
    const result={version:1,transition:{effect:['cut','fade','push','wipe'].includes(transition.effect)?transition.effect:'cut',direction:directions.includes(transition.direction)?transition.direction:'left',durationMs:duration(transition.durationMs)},steps:[]};
    if(input?.version!==1)return {version:1,transition:{effect:'cut',durationMs:0},steps:[]};
    for(const source of (Array.isArray(input?.steps)?input.steps:[]).slice(0,1000)){
      if(!source||typeof source!=='object')continue;
      const targetIds=(Array.isArray(source.targetIds)?source.targetIds:[]).filter(id=>{if(typeof id!=='string'||(allowed&&!allowed.has(id))||seen.has(id))return false;seen.add(id);return true;});
      if(!targetIds.length)continue;
      result.steps.push({targetIds,effect:['appear','fade','wipe','zoom'].includes(source.effect)?source.effect:'appear',direction:directions.includes(source.direction)?source.direction:'left',durationMs:duration(source.durationMs),advance:source.advance==='after'?'after':'click',delayMs:Math.max(0,Math.min(3600000,Number(source.delayMs)||0))});
    }
    return result;
  }
  function controller(root,input,{onChange=()=>{},transitions=true,reducedMotion=()=>matchMedia('(prefers-reduced-motion: reduce)').matches,clock=()=>performance.now()}={}){
    const build=normalize(input),targets=new Map(),saved=new Map(),animations=new Map();
    build.steps.forEach((step,index)=>step.targetIds.forEach(id=>targets.set(id,index)));
    let step=0,generation=0,disposed=false,timer=null,deadline=0,remaining=0,paused=false,remoteCursor=null;
    const nodes=()=>[...root.querySelectorAll('[data-object-id]')].filter(node=>targets.has(node.dataset.objectId));
    const cancelTimer=()=>{if(timer!==null)clearTimeout(timer);timer=null;deadline=0;};
    const cancelAnimations=()=>{for(const [animation,timing]of animations){clearTimeout(timing.timer);animation.cancel();}animations.clear();};
    function settleAnimation(animation,timing){
      clearTimeout(timing.timer);timing.deadline=clock()+timing.remaining;
      // Occluded WebKit views can freeze compositor animation time. Visibility
      // and cursor timing still advance, so settle to the authored final paint
      // on a bounded wall-clock deadline rather than leaving an object blank.
      timing.timer=setTimeout(()=>{if(!disposed&&animations.has(animation)){animations.delete(animation);animation.cancel();}},timing.remaining);
    }
    function animate(node,effect,direction,duration,transition=false,elapsed=0){
      if(elapsed>=duration)return;
      if(!duration||effect==='appear'||effect==='cut'||reducedMotion()||typeof node.animate!=='function')return;
      const base=getComputedStyle(node).transform==='none'?'':getComputedStyle(node).transform;
      let start,end;
      if(effect==='fade'){const opacity=getComputedStyle(node).opacity;start={opacity:0};end={opacity};}
      else if(effect==='wipe'){
        start={clipPath:({left:'inset(0 100% 0 0)',right:'inset(0 0 0 100%)',up:'inset(0 0 100% 0)',down:'inset(100% 0 0 0)'})[direction]||'inset(0 100% 0 0)'};
        end={clipPath:getComputedStyle(node).clipPath==='none'?'inset(0 0 0 0)':getComputedStyle(node).clipPath};
      }else if(effect==='push'&&transition){
        const x=direction==='right'?-root.offsetWidth:direction==='left'?root.offsetWidth:0,y=direction==='down'?-root.offsetHeight:direction==='up'?root.offsetHeight:0;
        start={transform:base+` translate(${x}px,${y}px)`};end={transform:base||'none'};
      }else if(effect==='zoom') {start={transform:(base||'')+' scale(.86)',opacity:0};end={transform:base||'none',opacity:getComputedStyle(node).opacity};}
      if(!start)return;
      try{const animation=node.animate([start,end],{duration,easing:'ease-out',fill:'none'}),timing={remaining:duration-elapsed+50,deadline:0,timer:null};animation.currentTime=elapsed;animations.set(animation,timing);settleAnimation(animation,timing);animation.finished.catch(()=>{}).finally(()=>{clearTimeout(timing.timer);animations.delete(animation);});}catch(_error){}
    }
    function schedule(wait=0){
      cancelTimer();remaining=0;if(disposed||paused||step>=build.steps.length||build.steps[step].advance!=='after')return;
      const ms=wait+build.steps[step].delayMs,epoch=generation;remaining=ms;deadline=clock()+ms;
      timer=setTimeout(()=>{timer=null;deadline=0;remaining=0;if(!disposed&&!paused&&generation===epoch)next();},ms);
    }
    function seek(value,{animated=false,automatic=false,wait=0,motion=animated?'advance':'seek'}={}){
      if(disposed)return false;const previous=step;cancelTimer();cancelAnimations();generation++;
      step=Math.max(0,Math.min(build.steps.length,Number.isFinite(value)?Math.floor(value):0));paused=false;remaining=0;
      for(const node of nodes()){
        if(!saved.has(node))saved.set(node,node.style.visibility);
        const index=targets.get(node.dataset.objectId),visible=index<step;
        node.style.visibility=visible?saved.get(node):'hidden';node.dataset.buildPending=visible?'false':'true';
        if(visible&&animated&&index>=previous) {const settings=build.steps[index];animate(node,settings.effect,settings.direction,settings.durationMs);}
      }
      onChange({step,total:build.steps.length,generation,paused,motion});
      if(automatic)schedule(wait);return true;
    }
    function next(){if(disposed||step>=build.steps.length)return false;const settings=build.steps[step];seek(step+1,{animated:true,automatic:true,wait:settings.durationMs});return true;}
    function previous(){if(disposed||step<=0)return false;seek(step-1);return true;}
    function start(){seek(0,{motion:'enter'});if(transitions)animate(root,build.transition.effect,build.transition.direction,build.transition.durationMs,true);schedule(build.transition.durationMs);}
    function pause(){if(disposed||paused)return;remaining=deadline?Math.max(0,deadline-clock()):0;cancelTimer();paused=true;for(const [a,timing]of animations){timing.remaining=Math.max(0,timing.deadline-clock());clearTimeout(timing.timer);a.pause();}onChange({step,total:build.steps.length,generation,paused});}
    function resume(){if(disposed||!paused)return;paused=false;for(const [a,timing]of animations){a.play();settleAnimation(a,timing);}if(step<build.steps.length&&build.steps[step].advance==='after'){const wait=remaining;cancelTimer();const epoch=generation;deadline=clock()+wait;timer=setTimeout(()=>{timer=null;if(!disposed&&!paused&&generation===epoch)next();},wait);}onChange({step,total:build.steps.length,generation,paused});}
    function dispose(){if(disposed)return;disposed=true;generation++;cancelTimer();cancelAnimations();for(const [node,visibility]of saved){node.style.visibility=visibility;delete node.dataset.buildPending;}saved.clear();}
    function applyCursor(cursor){
      const key=cursorKey(cursor);if(disposed||!key)return false;
      if(remoteCursor?.session===cursor.session&&(cursor.generation<remoteCursor.generation||key===cursorKey(remoteCursor)))return false;
      // Followers never schedule automatic reveals. Late join/hidden-tab resume
      // restores visibility immediately and animates only the latest reveal
      // when it is still inside its bounded presenter-clock interval.
      const elapsed=Math.max(0,Math.min(3600000,Date.now()-cursor.startedAtMs));
      remoteCursor={...cursor};seek(cursor.step);paused=cursor.paused;
      if(!paused&&cursor.motion==='advance'&&step>0){
        const settings=build.steps[step-1];
        for(const node of nodes())if(targets.get(node.dataset.objectId)===step-1)animate(node,settings.effect,settings.direction,settings.durationMs,false,elapsed);
      }else if(transitions&&!paused&&cursor.motion==='enter')animate(root,build.transition.effect,build.transition.direction,build.transition.durationMs,true,elapsed);
      return true;
    }
    seek(0);
    return {next,previous,seek,start,pause,resume,dispose,applyCursor,settleAnimations:cancelAnimations,snapshot:()=>({step,total:build.steps.length,generation,paused}),build};
  }
  function slideTransition(host,incoming,{reducedMotion=()=>matchMedia('(prefers-reduced-motion: reduce)').matches}={}){
    let outgoing=null,animations=[],timer=null,epoch=0,serial=0;
    function cancel(){epoch++;if(timer!==null)clearTimeout(timer);timer=null;for(const animation of animations)animation.cancel();animations=[];outgoing?.remove();outgoing=null;delete host.dataset.transitionState;}
    function snapshot(){
      // Rapid navigation first settles the old composition. Do not accumulate
      // a recursive history of snapshots or copy compositor intermediate paint.
      cancel();const clone=incoming.cloneNode(true),sources=[incoming,...incoming.querySelectorAll('*')],copies=[clone,...clone.querySelectorAll('*')],ids=new Map(),prefix='keynope-transition-'+(++serial)+'-';
      sources.forEach((source,index)=>{
        const target=copies[index];target.removeAttribute('data-object-id');target.removeAttribute('data-build-pending');target.removeAttribute('tabindex');
        if(source.id){if(source.namespaceURI==='http://www.w3.org/2000/svg'){ids.set(source.id,prefix+source.id);target.id=prefix+source.id;}else target.removeAttribute('id');}
        if(source.tagName==='CANVAS'){target.width=source.width;target.height=source.height;try{target.getContext('2d').drawImage(source,0,0);}catch(_error){}}
        if(source.getAnimations?.().length){const style=getComputedStyle(source);target.style.transform=style.transform;target.style.opacity=style.opacity;target.style.clipPath=style.clipPath;}
      });
      for(const target of copies)for(const attribute of [...target.attributes]){
        let value=attribute.value;
        value=value.replace(/url\(["']?#([^\s)"']+)["']?\)/g,(match,id)=>ids.has(id)?'url(#'+ids.get(id)+')':match);
        if((attribute.name==='href'||attribute.name==='xlink:href')&&value.startsWith('#')&&ids.has(value.slice(1)))value='#'+ids.get(value.slice(1));
        if(value!==attribute.value)target.setAttribute(attribute.name,value);
      }
      clone.setAttribute('aria-hidden','true');clone.style.pointerEvents='none';return clone;
    }
    function hold(image){cancel();if(!image)return;outgoing=image;Object.assign(outgoing.style,{position:'absolute',inset:'0',zIndex:'3',pointerEvents:'none'});host.append(outgoing);host.dataset.transitionState='holding';}
    function play(settings,{elapsed=0}={}){
      const transition=normalize({version:1,transition:settings}).transition,duration=transition.durationMs;
      elapsed=Math.max(0,Math.min(duration,Number(elapsed)||0));
      if(transition.effect==='cut'||!duration||elapsed>=duration||reducedMotion()||typeof incoming.animate!=='function'){cancel();return;}
      const generation=epoch;host.dataset.transitionState=transition.effect;if(outgoing)outgoing.style.zIndex='1';
      const dx=transition.direction==='right'?-host.clientWidth:transition.direction==='left'?host.clientWidth:0,dy=transition.direction==='down'?-host.clientHeight:transition.direction==='up'?host.clientHeight:0;
      let frames;
      if(transition.effect==='fade')frames=[{opacity:0},{opacity:1}];
      else if(transition.effect==='push')frames=[{transform:`translate(${dx}px,${dy}px)`},{transform:'translate(0,0)'}];
      else frames=[{clipPath:({left:'inset(0 100% 0 0)',right:'inset(0 0 0 100%)',up:'inset(0 0 100% 0)',down:'inset(100% 0 0 0)'})[transition.direction]},{clipPath:'inset(0 0 0 0)'}];
      try{
        const animate=(node,frames)=>{const animation=node.animate(frames,{duration,easing:'ease-in-out',fill:'both'});animation.currentTime=elapsed;animations.push(animation);};
        animate(incoming,frames);if(outgoing&&transition.effect==='push')animate(outgoing,[{transform:'translate(0,0)'},{transform:`translate(${-dx}px,${-dy}px)`}]);
        // Also settle occluded WebKit on a wall-clock deadline.
        timer=setTimeout(()=>{if(epoch===generation)cancel();},duration-elapsed+50);
        Promise.all(animations.map(animation=>animation.finished)).then(()=>{if(epoch===generation)cancel();}).catch(()=>{});
      }catch(_error){cancel();}
    }
    return {snapshot,hold,play,cancel,active:()=>!!host.dataset.transitionState};
  }
  function nextState(pages,position){
    if(!position||!Array.isArray(pages))return null;
    const index=pages.findIndex(page=>page.slide===position.slide&&page.page===(position.page||0));
    if(index<0)return {pending:true,label:'Preparing next state…'};
    const page=pages[index],cursor=position.buildCursor,build=normalize(page.scene?.build);
    if(cursor&&page.scene?.slideId&&cursor.slideId!==page.scene.slideId)return {pending:true,label:'Preparing next state…'};
    const step=cursorKey(cursor)?Math.min(cursor.step,build.steps.length):build.steps.length;
    if(step<build.steps.length)return {page,step:step+1,label:`Next build · ${step+1} of ${build.steps.length}`,kind:'build'};
    const next=pages.slice(index+1).find(candidate=>!candidate.tabOnly);
    if(!next)return {ended:true,label:'End of presentation'};
    const number=pages.filter(candidate=>!candidate.tabOnly).indexOf(next)+1;
    return {page:next,step:0,label:`Next slide · ${number}`,kind:'slide'};
  }
  function presenterPreview(host,{advance=()=>{},previous=()=>{},onClose=()=>{}}={}){
    host.classList.add('keynope-next-state');host.setAttribute('role','region');host.setAttribute('aria-label','Next presentation state');host.hidden=true;
    const header=document.createElement('div'),title=document.createElement('strong'),close=document.createElement('button'),canvas=document.createElement('div'),status=document.createElement('p'),controls=document.createElement('div');
    header.className='keynope-next-state-header';close.type='button';close.textContent='×';close.setAttribute('aria-label','Hide next state preview');close.onclick=()=>{shown=false;if(failed)clear();host.hidden=true;onClose();};header.append(title,close);
    canvas.className='keynope-next-state-canvas';canvas.setAttribute('aria-hidden','true');status.setAttribute('role','status');status.hidden=true;controls.className='keynope-next-state-controls';
    let view=null,runtime=null,source=null,epoch=0,shown=true,session=null,descriptor=null,busy=false,disposed=false,failed=false,canNavigate=true,lastPages=null,lastKey=null,lastShown=null,lastNavigation=null;
    const run=async action=>{if(busy)return;busy=true;buttons.forEach(b=>b.disabled=true);try{await action();}catch(error){status.textContent=error.message||'Could not advance presentation';status.hidden=false;}finally{busy=false;buttons.forEach((b,index)=>b.disabled=!canNavigate||(index===1&&!!descriptor?.ended)||!!descriptor?.pending);}};
    const buttons=[['Previous',previous],['Next',advance]].map(([label,action])=>{const b=document.createElement('button');b.type='button';b.textContent=label;b.setAttribute('aria-label',label+' presentation state');b.onclick=()=>run(action);controls.append(b);return b;});
    host.addEventListener('keydown',event=>{
      if(event.metaKey||event.ctrlKey||event.altKey)return;
      if(event.key==='Escape'){event.preventDefault();event.stopPropagation();close.click();return;}
      const index=['ArrowLeft','PageUp'].includes(event.key)?0:['ArrowRight','PageDown'].includes(event.key)?1:-1;
      if(index>=0){event.preventDefault();event.stopPropagation();if(!buttons[index].disabled)buttons[index].click();}
    });
    host.append(header,canvas,status,controls);
    const fit=()=>{if(!view||!source)return;const scale=Math.min(canvas.clientWidth/source.width,canvas.clientHeight/source.height);Object.assign(view.element.style,{position:'absolute',transform:`scale(${scale})`,transformOrigin:'0 0',left:(canvas.clientWidth-source.width*scale)/2+'px',top:(canvas.clientHeight-source.height*scale)/2+'px'});};
    const observer=new ResizeObserver(fit);observer.observe(canvas);
    const clear=()=>{epoch++;runtime?.dispose();view?.dispose();runtime=null;view=null;source=null;canvas.replaceChildren();};
    function update(pages,position,{navigationEnabled=true}={}){
      if(disposed)return;
      const key=position?JSON.stringify([position.slide,position.page||0,position.key||'',cursorKey(position.buildCursor)]):'';
      if(lastPages===pages&&lastKey===key&&lastShown===shown&&lastNavigation===navigationEnabled)return;
      lastPages=pages;lastKey=key;lastShown=shown;lastNavigation=navigationEnabled;
      canNavigate=navigationEnabled;
      descriptor=nextState(pages,position);
      if(!descriptor){if(view||source)clear();session=null;host.hidden=true;return;}
      const incoming=position.buildCursor?.session||'browser-presentation';if(session!==incoming){session=incoming;shown=true;}
      host.hidden=!shown;if(title.textContent!==descriptor.label)title.textContent=descriptor.label;
      buttons.forEach((b,index)=>b.disabled=busy||!navigationEnabled||(index===1&&!!descriptor.ended)||!!descriptor.pending);
      host.dataset.nextKind=descriptor.kind||(descriptor.ended?'end':'pending');
      if(!shown)return;
      if(!descriptor.page?.scene){if(view)clear();status.hidden=false;if(status.textContent!==descriptor.label)status.textContent=descriptor.label;return;}
      status.hidden=true;const scene=descriptor.page.scene;
      if(source!==scene){
        epoch++;const expected=epoch;failed=false;canvas.style.visibility='hidden';runtime?.dispose();runtime=null;
        try{const retained=view?.update(scene);if(!retained){view?.dispose();canvas.replaceChildren();view=KeynopeScene.mount(canvas,scene);}source=scene;runtime=controller(view.element,scene.build);runtime.seek(descriptor.step);fit();
          Promise.all([view.ready,KeynopeScene.ensureFonts()]).then(()=>{if(!disposed&&expected===epoch&&view){fit();canvas.style.visibility='visible';}}).catch(error=>{if(expected===epoch){failed=true;status.textContent='Could not prepare next state. '+error.message+' Hide and reopen to retry.';status.hidden=false;}});
        }catch(error){failed=true;status.textContent='Could not prepare next state. '+error.message+' Hide and reopen to retry.';status.hidden=false;}
      }else if(failed)status.hidden=false;
      else if(runtime?.snapshot().step!==descriptor.step)runtime?.seek(descriptor.step);
      host.dataset.nextStep=String(descriptor.step);
    }
    return {update,toggle(){shown=!shown;if(shown&&failed)clear();host.hidden=!shown;return shown;},visible:()=>!host.hidden,dispose(){disposed=true;clear();observer.disconnect();host.remove();}};
  }
  async function edit({scene,build,objects,selectedIds=[],apply,available=()=>true,onDirty=()=>{}}){
    if(activeEditor){activeEditor.dialog.focus();return;}
    if(!available())throw Error('The document changed; reopen Builds.');
    const previous=document.activeElement,dialog=document.createElement('dialog');dialog.className='keynope-build-dialog';dialog.dataset.keynopeSceneDialog='builds';dialog.setAttribute('aria-label','Slide builds');dialog.setAttribute('aria-modal','true');
    const title=document.createElement('h2');title.textContent='Builds & transition';
    const intro=document.createElement('p');intro.textContent='Reveal objects in order. Objects without a build are visible from the start.';
    const layout=document.createElement('div');layout.className='keynope-build-layout';const author=document.createElement('section'),preview=document.createElement('section');
    const order=document.createElement('ol');order.className='keynope-build-order';order.setAttribute('aria-label','Build order');
    const fields=document.createElement('fieldset'),fieldTitle=document.createElement('legend');fieldTitle.textContent='Selected build';fields.append(fieldTitle);
    const host=document.createElement('div');host.className='keynope-build-preview';host.setAttribute('aria-label','Build preview');
    const playback=document.createElement('div'),status=document.createElement('p');playback.className='keynope-build-playback';status.setAttribute('role','status');
    const message=document.createElement('p');message.className='keynope-theme-error';message.setAttribute('role','alert');message.hidden=true;
    const transition=document.createElement('fieldset'),transitionTitle=document.createElement('legend');transitionTitle.textContent='Slide transition';transition.append(transitionTitle);
    const actions=document.createElement('div');actions.className='keynope-build-actions';
    let draft=copy(build||{version:1,transition:{effect:'cut'},steps:[]}),active=0,history=[],future=[],view,runtime,closed=false,saving=false,drag=-1,pendingCommit=null;
    draft.transition||={effect:'cut'};draft.steps||=[];
    const baseline=JSON.stringify(draft);
    let generation=0,dirtyGeneration=-1,dirtyValue=false;
    const dirty=()=>{if(closed)return false;if(dirtyGeneration!==generation){dirtyValue=JSON.stringify(draft)!==baseline;dirtyGeneration=generation;}return dirtyValue;};
    const objectMap=new Map(objects.map(o=>[o.id,o]));
    const button=(text,callback,parent=actions)=>{const b=document.createElement('button');b.type='button';b.textContent=text;b.onclick=callback;parent.append(b);return b;};
    const mutate=(change,paint=true)=>{if(closed||saving)return;const previous=copy(draft);change();if(JSON.stringify(previous)!==JSON.stringify(draft)){history.push(previous);if(history.length>100)history.shift();future=[];generation++;}if(paint)render();else{undo.disabled=!history.length;redo.disabled=!future.length;onDirty();}};
    function select(parent,label,options,get,set){const row=document.createElement('label');row.textContent=label;const input=document.createElement('select');input.setAttribute('aria-label',label);for(const [value,title]of options)input.add(new Option(title,value));input.value=get()||options[0][0];input.onchange=()=>mutate(()=>set(input.value));row.append(input);parent.append(row);return input;}
    function number(parent,label,max,get,set){const row=document.createElement('label');row.textContent=label;const input=document.createElement('input');input.type='number';input.min=0;input.max=max;input.step=1;input.required=true;input.value=String(get()??0);input.setAttribute('aria-label',label);input.oninput=()=>mutate(()=>set(input.value===''?'':Number(input.value)),false);input.onchange=()=>{if(!input.reportValidity())return;mutate(()=>set(Number(input.value)));};row.append(input);parent.append(row);return input;}
    const undo=button('Undo edit',()=>{if(!history.length)return;future.push(copy(draft));draft=history.pop();generation++;render();});
    const redo=button('Redo edit',()=>{if(!future.length)return;history.push(copy(draft));draft=future.pop();generation++;render();});
    let observer;
    const cleanup=(restoreFocus=true)=>{if(closed)return;closed=true;if(activeEditor?.dialog===dialog)activeEditor=null;runtime?.dispose();view?.dispose();observer?.disconnect();dialog.remove();onDirty();if(restoreFocus&&previous?.isConnected)previous.focus({preventScroll:true});};
    const close=()=>{if(saving||closed)return;dialog.close();cleanup();};
    const disabledControls=new WeakMap();
    function setBusy(value){saving=value;for(const control of dialog.querySelectorAll('button,input,select')){if(value){disabledControls.set(control,control.disabled);control.disabled=true;}else control.disabled=disabledControls.get(control)||false;}dialog.setAttribute('aria-busy',String(value));}
    function commit(){
      if(pendingCommit)return pendingCommit;
      try{
        if(closed||!available())throw Error('The document changed; reopen Builds.');
        const invalid=[...dialog.querySelectorAll('input[type="number"]')].find(input=>!input.value||!input.checkValidity());
        if(invalid){invalid.reportValidity();throw Error('Enter a valid '+invalid.getAttribute('aria-label').toLowerCase()+'.');}
        // Numeric input updates the draft without replacing the focused field;
        // native Save does not necessarily deliver a blur/change event.
        const used=new Set();for(const step of draft.steps){if(!step.targetIds?.length)throw Error('Choose at least one object for every build.');for(const id of step.targetIds){if(used.has(id))throw Error('An object can appear in only one build.');used.add(id);}}
      }catch(error){if(!closed){message.textContent=error.message;message.hidden=false;}return Promise.reject(error);}
      const accepted=copy(draft);setBusy(true);message.hidden=true;runtime?.pause();
      pendingCommit=Promise.resolve().then(()=>{if(closed||!available())throw Error('The document changed; reopen Builds.');return apply(accepted);}).then(()=>{setBusy(false);close();},error=>{if(!closed){message.textContent=error.message||'Could not apply builds';message.hidden=false;setBusy(false);}throw error;}).finally(()=>{pendingCommit=null;});
      return pendingCommit;
    }
    const save=button('Apply',()=>commit().catch(()=>{}));
    const cancel=button('Cancel',close);
    const add=button('Add selected objects',()=>mutate(()=>{
      const used=new Set(draft.steps.flatMap(s=>s.targetIds)),ids=selectedIds.filter(id=>objectMap.has(id)&&!used.has(id)&&!objectMap.get(id).locked&&!objectMap.get(id).hidden);
      draft.steps.push({targetIds:ids,effect:'appear',durationMs:500,advance:'click',delayMs:0,direction:'left'});active=draft.steps.length-1;
    }),author);add.setAttribute('aria-label','Add build for selected objects');
    button('Clear builds',()=>mutate(()=>{draft.steps=[];active=0;}),author);
    author.append(order,fields,transition);preview.append(host,playback,status);layout.append(author,preview);dialog.append(title,intro,layout,message,actions);document.body.append(dialog);activeEditor={dialog,dirty,commit,pending:()=>pendingCommit,discard:()=>{dialog.close();cleanup(false);}};dialog.showModal();
    const back=button('Previous build',()=>runtime?.previous(),playback),next=button('Next build',()=>runtime?.next(),playback),replay=button('Replay',()=>runtime?.start(),playback),pause=button('Pause',()=>{runtime?.snapshot().paused?runtime.resume():runtime?.pause();},playback);
    function refreshRuntime(){if(!view||closed)return;runtime?.dispose();runtime=controller(view.element,draft,{onChange:value=>{status.textContent=`${value.step===0?'Initial state':'Build '+value.step} · ${value.step}/${value.total} revealed`;back.disabled=value.step===0;next.disabled=value.step===value.total;pause.textContent=value.paused?'Resume':'Pause';}});}
    function render(){
      runtime?.pause();order.replaceChildren();fields.replaceChildren(fieldTitle);transition.replaceChildren(transitionTitle);undo.disabled=!history.length;redo.disabled=!future.length;
      active=Math.max(0,Math.min(active,draft.steps.length-1));
      draft.steps.forEach((step,index)=>{
        const li=document.createElement('li');li.draggable=true;li.dataset.buildIndex=index;
        const choice=button(`${index+1}. ${step.targetIds.map(id=>objectMap.get(id)?.label||id).join(', ')||'Choose objects'}`,()=>{active=index;render();},li);choice.className=index===active?'active':'';choice.setAttribute('aria-label','Edit build '+(index+1));choice.setAttribute('aria-pressed',String(index===active));
        const up=button('↑',()=>mutate(()=>{[draft.steps[index-1],draft.steps[index]]=[draft.steps[index],draft.steps[index-1]];active=index-1;}),li),down=button('↓',()=>mutate(()=>{[draft.steps[index+1],draft.steps[index]]=[draft.steps[index],draft.steps[index+1]];active=index+1;}),li);
        up.disabled=index===0;down.disabled=index===draft.steps.length-1;up.setAttribute('aria-label','Move build '+(index+1)+' earlier');down.setAttribute('aria-label','Move build '+(index+1)+' later');
        button('Remove',()=>mutate(()=>draft.steps.splice(index,1)),li).setAttribute('aria-label','Remove build '+(index+1));
        li.ondragstart=event=>{drag=index;event.dataTransfer.setData('text/plain',String(index));};li.ondragover=event=>event.preventDefault();li.ondrop=event=>{event.preventDefault();if(drag<0||drag===index)return;mutate(()=>{const moved=draft.steps.splice(drag,1)[0];draft.steps.splice(index,0,moved);active=index;});drag=-1;};li.ondragend=()=>{drag=-1;};order.append(li);
      });
      const step=draft.steps[active];fields.disabled=!step;
      if(step){
        const targets=document.createElement('div');targets.className='keynope-build-targets';targets.setAttribute('aria-label','Build objects');const used=new Set(draft.steps.filter((s,i)=>i!==active).flatMap(s=>s.targetIds));
        for(const object of objects){const row=document.createElement('label'),input=document.createElement('input');input.type='checkbox';input.checked=step.targetIds.includes(object.id);input.disabled=object.locked||object.hidden||used.has(object.id);input.setAttribute('aria-label','Build object '+object.label);input.onchange=()=>mutate(()=>{step.targetIds=input.checked?[...step.targetIds,object.id]:step.targetIds.filter(id=>id!==object.id);});row.append(input,document.createTextNode(object.label));targets.append(row);}fields.append(targets);
        select(fields,'Build effect',[['appear','Appear'],['fade','Fade'],['wipe','Wipe'],['zoom','Zoom']],()=>step.effect,v=>step.effect=v);
        select(fields,'Build direction',directions.map(d=>[d,d[0].toUpperCase()+d.slice(1)]),()=>step.direction,v=>step.direction=v);
        number(fields,'Build duration (ms)',60000,()=>step.durationMs,v=>step.durationMs=v);
        select(fields,'Advance',[['click','On click'],['after','Automatically after previous']],()=>step.advance,v=>step.advance=v);
        number(fields,'Build delay (ms)',3600000,()=>step.delayMs,v=>step.delayMs=v);
      }
      select(transition,'Transition effect',[['cut','Cut'],['fade','Fade'],['push','Push'],['wipe','Wipe']],()=>draft.transition.effect,v=>draft.transition.effect=v);
      select(transition,'Transition direction',directions.map(d=>[d,d[0].toUpperCase()+d.slice(1)]),()=>draft.transition.direction,v=>draft.transition.direction=v);
      number(transition,'Transition duration (ms)',60000,()=>draft.transition.durationMs,v=>draft.transition.durationMs=v);
      refreshRuntime();onDirty();
    }
    dialog.addEventListener('cancel',event=>{event.preventDefault();close();});dialog.addEventListener('close',()=>cleanup());
    dialog.addEventListener('keydown',event=>{event.stopPropagation();if((event.metaKey||event.ctrlKey)&&['z','y'].includes(event.key.toLowerCase())){event.preventDefault();(event.key.toLowerCase()==='y'||event.shiftKey?redo:undo).click();}});
    render();
    try{
      await KeynopeScene.ensureFonts();if(closed)return;view=KeynopeScene.mount(host,scene);await view.ready;if(closed)return;
      const fit=()=>{const scale=Math.min(host.clientWidth/scene.width,host.clientHeight/scene.height);Object.assign(view.element.style,{transform:`scale(${scale})`,transformOrigin:'0 0',left:(host.clientWidth-scene.width*scale)/2+'px',top:(host.clientHeight-scene.height*scale)/2+'px'});};observer=new ResizeObserver(fit);observer.observe(host);fit();refreshRuntime();cancel.focus();
    }catch(error){if(!closed){message.textContent=error.message||'Could not prepare preview';message.hidden=false;}}
  }
  return Object.freeze({normalize,cursorKey,controller,slideTransition,nextState,presenterPreview,edit,isEditing:()=>!!activeEditor,isDirty:()=>!!activeEditor?.dirty(),commitPending:()=>activeEditor?.pending()||(activeEditor?.dirty()?activeEditor.commit():Promise.resolve()),discardPending:()=>activeEditor?.discard()});
})();
