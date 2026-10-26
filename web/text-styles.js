// Word formatting owns local text-draft history, never object geometry/content.
globalThis.KeynopeTextStyles=(()=>{
  const clone=value=>JSON.parse(JSON.stringify(value));
  // Keep only a semantic snapshot between text editors, never their draft
  // contents or DOM nodes. Document replacement cancels this ephemeral mode.
  let sharedPainter=null;
  const scopeKey=scope=>scope?JSON.stringify([scope.documentEpoch,!!scope.sceneMaster,scope.group||'']):null;
  function publishPainter(painter){if(sharedPainter===painter)return;sharedPainter=painter;document.dispatchEvent(new Event('keynope-word-painter-change'));}
  function clearPainter(){publishPainter(null);}
  function painterState(scope){if(scope!==undefined&&sharedPainter&&sharedPainter.scope!==scopeKey(scope))clearPainter();return sharedPainter?{active:true,sticky:sharedPainter.sticky}:null;}
  function setPainterSticky(value){if(sharedPainter){sharedPainter.sticky=!!value;document.dispatchEvent(new Event('keynope-word-painter-change'));}}
  function attach(host,dialog,editor,{styleClipboard,styleTransfer,painterScope=null,available=()=>true,propertyControls=true,onStateChange=()=>{}}){
    painterState(painterScope);
    const section=document.createElement('fieldset'),legend=document.createElement('legend'),status=document.createElement('span');legend.textContent='Selected words';section.setAttribute('aria-label','Word formatting');section.style.cssText='display:flex;flex-wrap:wrap;align-items:center;gap:8px;width:100%;box-sizing:border-box;border:1px solid #65717a;padding:10px';status.setAttribute('role','status');
    section.append(legend);host.append(section);
    let disposed=false,busy=false,capPending=false,caps=null,capKey='',saved={start:0,end:0},capAbort=null,operationAbort=null,operation=null,painter=sharedPainter,paintCandidate=null,paintFrame=0,generation=0;
    const cache=new Map();let cacheBytes=0;
    const notify=accepted=>{document.dispatchEvent(new Event('keynope-style-capabilities'));onStateChange(!!accepted);};
    const range=()=>{
      const selection=document.getSelection();if(selection&&editor.element.contains(selection.anchorNode)&&editor.element.contains(selection.focusNode))saved=editor.selection();
      return KeynopeTextLayout.textRange(editor.value(),saved.start,saved.end);
    };
    const signature=()=>JSON.stringify([range(),editor.value().runs]);
    const modelStamp=()=>{const m=editor.value();return JSON.stringify([m.runs,m.fontId,m.fontRoles,m.colorTokens,m.size,m.widthScale,m.lineHeight,m.paragraphBefore,m.paragraphAfter]);};
    const selected=()=>{const r=range();return r.start!==r.end;};
    const ready=()=>!disposed&&!busy&&!capPending&&available()&&!editor.isComposing()&&selected()&&!!caps;
    const controls=new Map();
    function button(label,fn){const b=document.createElement('button');b.type='button';b.textContent=label;b.setAttribute('aria-label',label);b.style.cssText='font:inherit;padding:6px 10px;background:#29323b;color:#fff;border:1px solid #65717a;border-radius:4px';b.onpointerdown=e=>{range();e.preventDefault();};b.onclick=fn;section.append(b);return b;}
    for(const [name,label]of [['copy','Copy word style'],['paste','Paste word style'],['reset','Reset word style']])controls.set(name,button(label,()=>perform(name)));
    const painterButton=button('Word format painter',()=>togglePainter()),keep=document.createElement('label'),sticky=document.createElement('input');sticky.type='checkbox';sticky.checked=!!painter?.sticky;sticky.setAttribute('aria-label','Keep word format painter active');sticky.onchange=()=>setPainterSticky(sticky.checked);keep.append(sticky,' Keep painting');section.append(keep);
    const fields=[];
    const roles=[['','Inherit text-box colour'],['custom','Custom colour'],...['background','surface','text','heading','muted','line'].map(role=>[role,role[0].toUpperCase()+role.slice(1)]),...Array.from({length:6},(_,i)=>['accent-'+(i+1),'Accent '+(i+1)]),['mixed','Mixed']];
    for(const [key,label,options]of propertyControls?[['text.font','Word font',[['','Inherit text-box font'],['c64','Keynope C64'],['sans','Go — proportional'],['mono','Go Mono — monospaced'],['theme:heading','Theme Heading'],['theme:body','Theme Body'],['theme:mono','Theme Mono'],['mixed','Mixed']]],['text.size','Word font size',null],['paint.colorToken','Word colour role',roles]]:[]){
      const l=document.createElement('label'),input=document.createElement(options?'select':'input');l.textContent=label+' ';input.setAttribute('aria-label',label);input.style.cssText='font:inherit;width:150px;padding:6px;box-sizing:border-box';
      if(options){for(const [value,text]of options){const o=document.createElement('option');o.value=value;o.textContent=text;o.disabled=value==='mixed'||value==='custom';input.append(o);}globalThis.KeynopeFonts?.enhance(input);}
      else{input.type='number';input.min='1';input.max='1024';input.step='any';input.placeholder='Inherit';input.style.width='100px';}
      input.onchange=()=>{if(!options&&!input.checkValidity()){input.reportValidity();return;}perform('property',{property:key,propertyValue:options?input.value:input.value===''?'':Number(input.value)});};
      l.append(input);section.append(l);fields.push({key,input});
    }
    section.append(status);
    const remember=(key,value)=>{if(key.length>65536)return;if(cache.has(key)){cacheBytes-=key.length;cache.delete(key);}cache.set(key,value);cacheBytes+=key.length;while(cache.size>16||cacheBytes>262144){const oldest=cache.keys().next().value;cache.delete(oldest);cacheBytes-=oldest.length;}};
    function refresh(accepted=false){
      if(disposed)return;
      if(!selected()||!available()||editor.isComposing()){capAbort?.abort();capAbort=null;caps=null;capPending=false;capKey='';}
      else if(!busy){const key=signature();if(key!==capKey){capKey=key;caps=cache.get(key)||null;capPending=!caps;capAbort?.abort();
        if(!caps){const abort=new AbortController();capAbort=abort;styleTransfer('text-style-capabilities',clone(editor.value().runs),range(),{signal:abort.signal}).then(result=>{
          if(disposed||abort.signal.aborted||key!==capKey)return;caps=result.selectionCapabilities;capPending=false;remember(key,caps);refresh();maybePaint();
        }).catch(error=>{if(disposed||abort.signal.aborted||key!==capKey)return;capPending=false;status.textContent=error.message;refreshControls();});}
      }}
      refreshControls(accepted);
    }
    function refreshControls(accepted=false){
      const enabled=ready();for(const [name,b]of controls)b.disabled=!enabled||!caps.commands?.['style.'+name]?.enabled||(name==='paste'&&styleClipboard?.()?.context!=='text-range');
      section.dataset.rangeStart=enabled?String(caps.textRange?.start??''):'';section.dataset.rangeEnd=enabled?String(caps.textRange?.end??''):'';
      painterButton.disabled=busy||capPending||!available()||editor.isComposing()||(!painter&&(!enabled||!caps.commands?.['style.copy']?.enabled));painterButton.setAttribute('aria-pressed',String(!!painter));painterButton.style.background=painter?'#245c91':'#29323b';keep.hidden=!painter;keep.style.display=painter?'flex':'none';
      for(const {key,input}of fields){input.disabled=!enabled;const p=caps?.properties?.[key];if(!enabled||document.activeElement===input&&!accepted)continue;input.dataset.mixed=String(p?.state==='mixed');input.dataset.inherited=String(p?.provenance==='inherited');input.value=p?.state==='mixed'?(input.tagName==='SELECT'?'mixed':''):p?.value??'';input.title=p?.state==='mixed'?'Mixed formatting':p?.provenance==='inherited'?'Inherit text-box formatting':p?.provenance==='theme'?'Follows the deck theme':key==='paint.colorToken'&&p?.value==='custom'?'Use the colour picker above for a fixed colour':'Explicit word formatting';}
      section.setAttribute('aria-busy',String(busy||capPending));notify(accepted);
    }
    function commandState(){
      const off={copy:false,paste:false,reset:false,painter:false},active=document.activeElement;
      if(disposed||busy||!available()||editor.isComposing()||active?.matches?.('input,textarea,select')||active?.isContentEditable&&!editor.element.contains(active))return off;
      return {copy:!controls.get('copy').disabled,paste:!controls.get('paste').disabled,reset:!controls.get('reset').disabled,painter:!painterButton.disabled};
    }
    function cancelPainter(){generation++;operationAbort?.abort();clearPainter();painter=null;paintCandidate=null;if(paintFrame)cancelAnimationFrame(paintFrame);paintFrame=0;refreshControls();}
    function perform(name,extra={}){
      if(!ready()||!['copy','paste','reset','property'].includes(name))return Promise.resolve(false);
      if(name==='property'&&!caps.properties?.[extra.property]?.editable)return Promise.resolve(false);
      if(name!=='property'&&!caps.commands?.['style.'+name]?.enabled)return Promise.resolve(false);
      if(name==='paste'&&(extra.styleData||styleClipboard?.())?.context!=='text-range')return Promise.resolve(false);
      const selectedRange=range(),originalRange={...saved},stamp=modelStamp(),token=generation,abort=new AbortController();let accepted=false;operationAbort=abort;busy=true;refreshControls();status.textContent='Updating word style…';
      operation=(async()=>{
        try{
          const action=name==='property'?'set-selection-property':name+'-object-style';
          const result=await styleTransfer(action,clone(editor.value().runs),selectedRange,{...extra,modernSize:editor.value().size,signal:abort.signal});
          if(disposed||abort.signal.aborted||token!==generation)return false;
          if(stamp!==modelStamp()||JSON.stringify(selectedRange)!==JSON.stringify(range()))throw Error('Text or selection changed; try the word style operation again.');
          if(result.textRuns)editor.applyStyledRuns(result.textRuns,originalRange,result.colorTokens,result.fontRoles);
          accepted=!!result.textRuns;
          if(result.selectionCapabilities){capAbort?.abort();capPending=false;capKey=signature();caps=result.selectionCapabilities;remember(capKey,caps);}
          if(extra.arm){publishPainter({snapshot:clone(result.styleClipboard),sticky:sticky.checked,scope:scopeKey(painterScope)});status.textContent='Select words here, or Apply and open another text box. Escape cancels.';}
          else{status.textContent=name==='copy'?'Word style copied.':name==='reset'?'Word formatting reset.':'Word style applied.';if(extra.paint&&!painter?.sticky)clearPainter();}
          return true;
        }catch(error){if(!disposed&&error.name!=='AbortError')status.textContent=error.message||'Could not change word style';return false;}
        finally{busy=false;operation=null;operationAbort=null;if(!disposed)refresh(accepted);}
      })();return operation;
    }
    function togglePainter(){if(painter){cancelPainter();status.textContent='Word format painter cancelled.';return Promise.resolve(true);}return perform('copy',{arm:true});}
    function toggleProperty(property){
      if(!['text.bold','text.italic','text.underline'].includes(property)||!ready())return Promise.resolve(false);
      const p=caps.properties?.[property];if(!p?.editable)return Promise.resolve(false);
      // A mixed selection becomes uniformly on; only uniformly on becomes off.
      return perform('property',{property,propertyValue:!(p.state==='uniform'&&p.value===true)});
    }
    function refreshToggle(button,property){
      const p=caps?.properties?.[property],state=p?.state==='mixed'?'mixed':p?.value===true?'true':'false';
      button.disabled=!ready()||!p?.editable;button.setAttribute('aria-pressed',state);
      button.dataset.state=p?.state||'unavailable';button.dataset.provenance=p?.provenance||'unavailable';
      button.style.background=state==='true'?'#245c91':state==='mixed'?'#504622':'#29323b';button.style.color='#fff';button.style.border='1px '+(state==='mixed'?'dashed #ffdf66':'solid #65717a');
      button.title=button.getAttribute('aria-label')+(state==='mixed'?' — mixed formatting':state==='true'?' — on':' — off');
    }
    function maybePaint(){if(!painter||!paintCandidate||!ready())return;const key=JSON.stringify(range());if(key!==paintCandidate)return;paintCandidate=null;perform('paste',{paint:true,styleData:painter.snapshot});}
    const painterChanged=()=>{if(disposed)return;if(painter!==sharedPainter){generation++;operationAbort?.abort();paintCandidate=null;painter=sharedPainter;if(!available())status.textContent='The document changed; reopen the text editor.';}sticky.checked=!!painter?.sticky;refreshControls();};
    const selectChanged=()=>refresh();
    const compositionChanged=()=>queueMicrotask(refresh);
    const pointerUp=()=>{if(!painter)return;if(paintFrame)cancelAnimationFrame(paintFrame);paintFrame=requestAnimationFrame(()=>{paintFrame=0;if(disposed||!painter)return;refresh();paintCandidate=JSON.stringify(range());maybePaint();});};
    const keyboard=event=>{
      if(event.isComposing||editor.isComposing())return;
      if((event.metaKey||event.ctrlKey)&&!event.altKey&&!event.shiftKey&&['b','i','u'].includes(event.key.toLowerCase())&&!document.activeElement?.matches?.('input,textarea,select')){
        event.preventDefault();event.stopImmediatePropagation();toggleProperty('text.'+({b:'bold',i:'italic',u:'underline'}[event.key.toLowerCase()]));return;
      }
      if(event.key==='Escape'&&(painter||busy)){event.preventDefault();event.stopImmediatePropagation();cancelPainter();status.textContent='Word format painter cancelled.';return;}
      if((event.metaKey||event.ctrlKey)&&event.altKey&&!event.shiftKey&&['c','v'].includes(event.key.toLowerCase())){const name=event.key.toLowerCase()==='c'?'copy':'paste';if(!commandState()[name])return;event.preventDefault();event.stopImmediatePropagation();perform(name);}
    };
    dialog.keynopeTextStyleCommandState=commandState;
    dialog.keynopeTextStyleCommand=name=>{if(!['copy','paste','reset','painter'].includes(name)||!commandState()[name])return Promise.resolve(false);return name==='painter'?togglePainter():perform(name);};
    document.addEventListener('keynope-style-scope-change',selectChanged);document.addEventListener('keynope-word-painter-change',painterChanged);document.addEventListener('selectionchange',selectChanged);dialog.addEventListener('keydown',keyboard,true);editor.element.addEventListener('pointerup',pointerUp);editor.element.addEventListener('compositionstart',compositionChanged);editor.element.addEventListener('compositionend',compositionChanged);
    if(painter)status.textContent='Word painter active. Select words to apply the captured style. Escape cancels.';
    refresh();
    return {refresh,toggleProperty,refreshToggle,pending:()=>operation,isReady:ready,isBusy:()=>busy||capPending,propertyState:key=>caps?.properties?.[key]?clone(caps.properties[key]):null,
      setProperty:(property,propertyValue)=>perform('property',{property,propertyValue}),stepProperty:(property,propertyDelta)=>perform('property',{property,propertyDelta}),
      dispose({keepPainter=false}={}){disposed=true;generation++;capAbort?.abort();operationAbort?.abort();if(!keepPainter)clearPainter();if(paintFrame)cancelAnimationFrame(paintFrame);paintFrame=0;paintCandidate=null;painter=null;cache.clear();document.removeEventListener('keynope-style-scope-change',selectChanged);document.removeEventListener('keynope-word-painter-change',painterChanged);document.removeEventListener('selectionchange',selectChanged);dialog.removeEventListener('keydown',keyboard,true);editor.element.removeEventListener('pointerup',pointerUp);editor.element.removeEventListener('compositionstart',compositionChanged);editor.element.removeEventListener('compositionend',compositionChanged);delete dialog.keynopeTextStyleCommandState;delete dialog.keynopeTextStyleCommand;}};
  }
  return Object.freeze({attach,painterState,clearPainter,setPainterSticky});
})();
