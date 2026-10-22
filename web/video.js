(function(global){
  'use strict';
  const sourcePattern=/^data:video\/(mp4|quicktime);base64,[A-Za-z0-9+/=]+$/;
  // Same aggregate asset-archive safety boundary as Go. Movies have no lower,
  // independent ceiling; the server checks remaining deck capacity atomically.
  const archiveBytes=100*1024*1024,maxMovieBytes=Math.floor(archiveBytes/4)*3;
  function mount(node,object,{control=true}={}){
    const doc=node.ownerDocument,poster=doc.createElement('img'),video=doc.createElement('video'),message=doc.createElement('p');
    let value=object.video,requested=false,active=false,disposed=false,url=null,monitor=false,canonical=null,appliedRevision=0,playbackContext='',pending=false,commandGeneration=0,editing=false,localStarted=false,localMuted;
    if(!value||typeof value.source!=='string'||!sourcePattern.test(value.source)||value.source.length>archiveBytes+64)throw Error('Invalid embedded video source');
    poster.src=object.media?.source||'';poster.alt=object.media?.alt||'Video';poster.draggable=false;
    video.className='keynope-video';video.playsInline=true;video.preload='none';video.setAttribute('playsinline','');video.setAttribute('aria-label',poster.alt);video.disablePictureInPicture=true;video.setAttribute('disableRemotePlayback','');
    message.setAttribute('role','status');message.hidden=true;message.textContent='This video cannot be played here. Use an MP4 encoded with H.264 video and AAC audio.';
    if(!doc.getElementById('keynope-video-monitor-style')){const style=doc.createElement('style');style.id='keynope-video-monitor-style';style.textContent='video[data-presenter-monitor="true"]::-webkit-media-controls-mute-button,video[data-presenter-monitor="true"]::-webkit-media-controls-volume-slider,video[data-presenter-monitor="true"]::-webkit-media-controls-volume-control-container{display:none!important}';doc.head.append(style);}
    for(const item of [poster,video])Object.assign(item.style,{position:'absolute',inset:'0',width:'100%',height:'100%',objectFit:'contain',background:'#10151e'});
    Object.assign(message.style,{position:'absolute',inset:'0',margin:'0',padding:'20px',boxSizing:'border-box',background:'#10151e',color:'#fff',font:'20px system-ui'});
    video.hidden=true;node.append(poster,video,message);
    const refresh=()=>{
      if(disposed)return;
      const style=getComputedStyle(node),next=requested&&!doc.hidden&&node.getClientRects().length>0&&style.visibility!=='hidden'&&style.display!=='none'&&Number(style.opacity)!==0&&!node.closest('[data-build-pending="true"]');
      if(next===active)return;active=next;
      node.style.pointerEvents=active?'auto':'';
      video.controls=active&&!monitor&&!editing;video.hidden=!active;poster.hidden=active;
      if(!active){appliedRevision=0;video.pause();return;}
      if(!url){
        const raw=atob(value.source.slice(value.source.indexOf(',')+1)),bytes=new Uint8Array(raw.length);for(let i=0;i<raw.length;i++)bytes[i]=raw.charCodeAt(i);
        url=URL.createObjectURL(new Blob([bytes],{type:value.source.slice(5,value.source.indexOf(';'))}));video.preload='metadata';video.src=url;video.poster=poster.src;
      }
      if(value.autoplay&&!editing&&!monitor&&!canonical)video.play().catch(()=>{/* Keep user-operated controls when autoplay is blocked. */});
    };
    const targetTime=state=>{
      let target=state.time+(state.playing?Math.max(0,Date.now()-state.updatedMs)/1000*state.rate:0);
      if(Number.isFinite(video.duration)&&video.duration>0)target=video.loop?target%video.duration:Math.min(video.duration,target);
      return target;
    };
    function sync(state){
      if(editing){canonical=null;return;}
      if(pending||state&&canonical&&state.revision<canonical.revision)return;
      canonical=state||null;
      if(!canonical||!active||disposed||appliedRevision===canonical.revision||video.readyState<1)return;
      const target=targetTime(canonical);video.playbackRate=canonical.rate;
      video.muted=monitor||canonical.muted;video.volume=monitor?0:canonical.volume;
      if(Math.abs(video.currentTime-target)>.25)video.currentTime=target;
      appliedRevision=canonical.revision;
      if(canonical.playing)video.play().catch(()=>{});else video.pause();
    }
    video.addEventListener('loadedmetadata',()=>sync(canonical));
    function changed(event){
      if(!control||editing||disposed||!active||!requested||monitor&&event.type==='volumechange')return;
      if(canonical){
        if(['play','pause','ended'].includes(event.type)&&!video.paused===canonical.playing)return;
        if(event.type==='seeked'&&Math.abs(video.currentTime-targetTime(canonical))<.35)return;
        if(event.type==='ratechange'&&video.playbackRate===canonical.rate)return;
        if(event.type==='volumechange'&&video.muted===canonical.muted&&video.volume===canonical.volume)return;
      }
      send(snapshot());
    }
    const snapshot=()=>({playing:!video.paused&&!video.ended,time:video.currentTime,rate:video.playbackRate,muted:monitor?(canonical?.muted??!!value.muted):video.muted,volume:monitor?(canonical?.volume??1):video.volume});
    function send(value){const generation=++commandGeneration;pending=true;Promise.resolve(global.keynopeVideoPlaybackChanged?.(object.id,value)).then(state=>{if(disposed||generation!==commandGeneration)return;pending=false;appliedRevision=0;sync(state||canonical);}).catch(()=>{if(generation===commandGeneration)pending=false;});}
    for(const event of ['play','pause','seeked','ratechange','volumechange','ended'])video.addEventListener(event,changed);
    video.addEventListener('error',()=>{if(!disposed){video.pause();message.hidden=false;}});
    for(const event of ['pointerdown','pointerup','click','dblclick','keydown'])video.addEventListener(event,e=>e.stopPropagation());
    doc.addEventListener('visibilitychange',refresh);
    const observer=new ResizeObserver(refresh);observer.observe(node);
    function update(object){value=object.video;video.loop=!!value.loop;video.muted=monitor||!!(canonical?.muted??localMuted??value.muted);video.defaultMuted=!!value.muted;}
    update(object);
    return {
      video,update,sync,
      play(){if(editing){localStarted=true;requested=true;refresh();}return video.play();},
      muted:()=>monitor?(canonical?.muted??!!value.muted):video.muted,
      setMuted(muted){if(editing){localMuted=muted;video.muted=muted;}else send({...snapshot(),muted});},
      setActive(enabled,options){
        if(options?.context&&playbackContext!==options.context){playbackContext=options.context;commandGeneration++;pending=false;canonical=null;localStarted=false;localMuted=undefined;appliedRevision=0;if(video.readyState>=1)video.currentTime=0;}
        if(options){editing=!!options.editing;if(monitor!==!!options.monitor){monitor=!!options.monitor;video.dataset.presenterMonitor=String(monitor);video.muted=monitor||!!(canonical?.muted??localMuted??value.muted);video.volume=monitor?0:(canonical?.volume??1);video.setAttribute('controlsList',monitor?'nofullscreen noremoteplayback nodownload':'noremoteplayback nodownload');video.title=monitor?'Silent presenter preview — audio plays on the presentation display.':'';}}
        requested=!!enabled&&(!editing||localStarted);refresh();
      },
      dispose(){disposed=true;commandGeneration++;pending=false;observer.disconnect();video.pause();video.removeAttribute('src');video.load();doc.removeEventListener('visibilitychange',refresh);if(url)URL.revokeObjectURL(url);poster.remove();video.remove();message.remove();}
    };
  }
  async function prepareImport(file){
    if(!file||!file.size)throw Error('Choose a non-empty MP4/MOV video.');
    if(file.size>maxMovieBytes)throw Error('This video exceeds the deck’s embedded-media capacity. Remove embedded media or choose a smaller clip.');
    const video=document.createElement('video'),url=URL.createObjectURL(file);video.muted=true;video.playsInline=true;video.preload='auto';
    try{
      await new Promise((resolve,reject)=>{
        const timer=setTimeout(()=>reject(Error('Video could not be decoded. Use MP4 with H.264 video and AAC audio.')),15000);
        video.onloadeddata=()=>{clearTimeout(timer);resolve();};video.onerror=()=>{clearTimeout(timer);reject(Error('Video could not be decoded. Use MP4 with H.264 video and AAC audio.'));};video.src=url;video.load();
      });
      if(!(video.videoWidth>0&&video.videoHeight>0))throw Error('The file has no video track.');
      const scale=Math.min(1,1280/video.videoWidth,720/video.videoHeight),canvas=document.createElement('canvas');canvas.width=Math.max(1,Math.round(video.videoWidth*scale));canvas.height=Math.max(1,Math.round(video.videoHeight*scale));canvas.getContext('2d').drawImage(video,0,0,canvas.width,canvas.height);
      const poster=await new Promise(resolve=>canvas.toBlob(resolve,'image/png'));const body=new FormData();body.append('video',file);if(poster)body.append('poster',poster,'poster.png');return body;
    }finally{video.pause();video.removeAttribute('src');video.load();URL.revokeObjectURL(url);}
  }
  function preview(object){
    const dialog=document.createElement('dialog'),host=document.createElement('div'),close=document.createElement('button');
    dialog.setAttribute('aria-label','Video preview');Object.assign(dialog.style,{width:'min(900px,90vw)',maxHeight:'90vh',padding:'16px',background:'#182028',color:'#fff',border:'1px solid #65717a'});
    Object.assign(host.style,{position:'relative',width:'100%',aspectRatio:String(object.media?.width||16)+' / '+String(object.media?.height||9)});close.textContent='Close';close.type='button';dialog.append(host,close);document.body.append(dialog);
    const player=mount(host,object,{control:false});const finish=()=>{player.dispose();dialog.close();dialog.remove();};close.onclick=finish;dialog.addEventListener('cancel',e=>{e.preventDefault();finish();});dialog.showModal();player.setActive(true);close.focus();
  }
  // Keep primary-screen controls in UI pixels, not scaled slide coordinates.
  function controller(host){
    const doc=host.ownerDocument,bar=doc.createElement('aside'),choose=doc.createElement('select'),play=doc.createElement('button'),restart=doc.createElement('button'),seek=doc.createElement('input'),time=doc.createElement('output'),sound=doc.createElement('button'),speed=doc.createElement('select');
    let entries=[],key='',dragging=false,enabled=false,editingMode=false,selection='';
    const watched=new Map(),events=['timeupdate','play','pause','seeked','ratechange','volumechange','loadedmetadata'];
    bar.className='keynope-video-controls';bar.setAttribute('aria-label','Presentation video controls');bar.hidden=true;
    bar.style.cssText='position:absolute;z-index:25;display:flex;align-items:center;gap:8px;box-sizing:border-box;padding:8px 10px;border:1px solid #68737f;border-radius:10px;background:#182028ed;color:#fff;font:13px system-ui;pointer-events:auto';
    bar.style.display='none';
    for(const button of [play,restart,sound]){button.type='button';button.style.cssText='font:inherit;min-height:30px;white-space:nowrap;padding:4px 8px;background:#28343f;color:#fff;border:1px solid #71808e;border-radius:5px';}
    choose.setAttribute('aria-label','Video to control');choose.style.maxWidth='130px';choose.onchange=()=>refresh(entries);
    seek.type='range';seek.min='0';seek.step='.01';seek.setAttribute('aria-label','Video position');seek.style.cssText='flex:1;min-width:40px;width:80px';
    time.style.cssText='white-space:nowrap;font-variant-numeric:tabular-nums';
    speed.setAttribute('aria-label','Video playback speed');for(const value of [.5,1,1.5,2])speed.add(new Option(value+'×',String(value)));
    for(const select of [choose,speed])Object.assign(select.style,{font:'inherit',color:'#fff',background:'#28343f',colorScheme:'dark',minHeight:'30px',border:'1px solid #71808e',borderRadius:'5px'});
    seek.style.accentColor='#70b7ff';
    const selected=()=>entries.find(([id])=>id===choose.value)?.[1]||entries[0]?.[1];
    const error=problem=>global.showEngagementToast?.('Could not play video. '+problem.message);
    play.onclick=()=>{const p=selected();if(p)p.video.paused?p.play().catch(error):p.video.pause();};
    restart.textContent='Restart';restart.onclick=()=>{const p=selected();if(p){p.video.currentTime=0;p.play().catch(error);}};
    seek.oninput=()=>{dragging=true;const p=selected();if(p)p.video.currentTime=Number(seek.value);};seek.onchange=()=>{dragging=false;};seek.onblur=()=>{dragging=false;};
    sound.onclick=()=>{const p=selected();if(p)p.setMuted(!p.muted());};speed.onchange=()=>{const p=selected();if(p)p.video.playbackRate=Number(speed.value);};
    for(const event of ['pointerdown','pointerup','click','dblclick','keydown'])bar.addEventListener(event,e=>e.stopPropagation());
    bar.append(choose,play,restart,seek,time,sound,speed);host.append(bar);
    const stamp=seconds=>Math.floor(seconds/60)+':'+String(Math.floor(seconds%60)).padStart(2,'0');
    function refresh(next,{active=enabled,editing=editingMode,selectedID=selection}={}){
      enabled=active;editingMode=editing;entries=next;
      const current=new Set(entries.map(([,player])=>player.video));
      for(const [video,listener]of watched)if(!current.has(video)){for(const event of events)video.removeEventListener(event,listener);watched.delete(video);}
      for(const video of current)if(!watched.has(video)){const listener=()=>refresh(entries);watched.set(video,listener);for(const event of events)video.addEventListener(event,listener);}
      bar.hidden=!active||!entries.length;bar.style.display=bar.hidden?'none':'flex';if(bar.hidden)return;
      const ids=entries.map(([id])=>id).join(':');if(ids!==key){const previous=choose.value;key=ids;choose.replaceChildren();entries.forEach(([id],i)=>choose.add(new Option('Video '+(i+1),id)));if(entries.some(([id])=>id===previous))choose.value=previous;}
      if(selection!==selectedID){selection=selectedID;if(editing&&entries.some(([id])=>id===selection))choose.value=selection;}
      choose.hidden=entries.length<2;const p=selected(),v=p.video,duration=Number.isFinite(v.duration)?v.duration:0;
      const label=v.paused?'Play':'Pause';if(play.textContent!==label)play.textContent=label;const name=label+(editing?' video':' presentation video');if(play.getAttribute('aria-label')!==name)play.setAttribute('aria-label',name);
      seek.disabled=!duration;if(seek.max!==String(duration))seek.max=String(duration);if(!dragging&&doc.activeElement!==seek)seek.value=String(v.currentTime);
      const clock=stamp(v.currentTime)+' / '+stamp(duration);if(time.textContent!==clock)time.textContent=clock;
      const audio=p.muted()?'Sound off':'Sound on';if(sound.textContent!==audio){sound.textContent=audio;sound.setAttribute('aria-pressed',String(!p.muted()));}if(doc.activeElement!==speed)speed.value=String(v.playbackRate);
    }
    return {element:bar,update:refresh};
  }
  global.KeynopeVideo={mount,prepareImport,preview,controller};
})(typeof globalThis==='object'?globalThis:window);
