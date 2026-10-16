/* Shared table authoring. A dialog owns a draft; Apply is one document command. */
globalThis.KeynopeTables=(()=>{
  const clone=value=>JSON.parse(JSON.stringify(value));
  const tableMetricLimits=key=>key==='fontSize'?[1,1024]:key==='fontWidth'?[1,200]:key==='lineHeight'?[.5,4]:[0,100];
  const tableMetricStep=key=>key==='lineHeight'?.1:1;
  // Bound retained drafts, not just their count: imported cell/run payloads
  // can be large. Serialized entries also cannot alias the live draft.
  function draftHistory(){
    const past=[],future=[],limit=32*1024*1024;
    const trim=()=>{
      while(past.length>100)past.shift();while(future.length>100)future.shift();
      let bytes=[...past,...future].reduce((n,s)=>n+s.length*2,0);
      while(bytes>limit&&(past.length||future.length))bytes-=(past.length?past.shift():future.shift()).length*2;
    };
    return {
      has:redo=>(redo?future:past).length>0,
      peek:redo=>{const entry=(redo?future:past).at(-1);return entry===undefined?null:JSON.parse(entry);},
      record:(before,after)=>{const entry=JSON.stringify(before);if(entry===JSON.stringify(after))return false;past.push(entry);future.length=0;trim();return true;},
      advance:(current,redo)=>{const from=redo?future:past,to=redo?past:future;if(!from.length)return;from.pop();to.push(JSON.stringify(current));trim();}
    };
  }
  const themeRoles=[['text','Text'],['heading','Heading'],['background','Background'],['surface','Surface'],['muted','Muted'],['line','Line'],...Array.from({length:6},(_,i)=>['accent-'+(i+1),'Accent '+(i+1)])];
  const themeChoices=[['','Inherit'],['none','None'],['custom','Custom'],...themeRoles.map(([role,label])=>['theme:'+role,label]),['mixed','Mixed']];
  function themeChoice(value){return !value?'':value==='none'?'none':String(value).startsWith('theme:')?value:'custom';}
  function tableColour(value,options){
    if(!String(value||'').startsWith('theme:'))return value;
    const role=value.slice(6),colors=options.theme?.()?.colors;
    return (role.startsWith('accent-')?colors?.accent?.[Number(role.slice(7))-1]:colors?.[role])||'#ffffff';
  }
  // Styling a word must not discard opaque future run metadata. The shared
  // layout editor deliberately normalises only its supported properties;
  // overlay untouched extensions at their original text boundaries on commit.
  function retainRunExtensions(runs,previous){
    if(!previous?.length||runs.map(r=>r.text).join('')!==previous.map(r=>r.text).join(''))return runs.map(r=>({...r}));
    const known=new Set(['text','bold','boldOff','italic','underline','color','colorToken','fontId','fontRole','fontSize']),result=[];
    let index=0,offset=0;
    for(const run of runs){let start=0;while(start<run.text.length){
      while(index<previous.length&&offset===previous[index].text.length){index++;offset=0;}
      const source=previous[index];if(!source)throw Error('The styled cell has inconsistent text.');
      const length=Math.min(run.text.length-start,source.text.length-offset),extra=Object.fromEntries(Object.entries(source).filter(([key])=>!known.has(key)));
      result.push({...extra,...run,text:run.text.slice(start,start+length)});start+=length;offset+=length;
    }}return result;
  }
  function cellStyleTransferTools(host,options,getData,getRange,apply,{before=()=>{},available=()=>true,root=host.closest('dialog')}={}){
    if(!options.styleTransfer)return ()=>{};
    const bar=document.createElement('div');bar.className='keynope-table-toolbar';bar.setAttribute('aria-label','Cell style transfer');
    const message=document.createElement('span');message.setAttribute('role','status');let busy=false,painter=null,generation=0,pending=null,capabilities=null,capabilityKey='',capabilityPending=false,capabilityAbort=null,operationAbort=null;
    const actions=[];
    const capabilityCache=new Map();let capabilityCacheBytes=0;
    const remember=(key,value)=>{if(key.length>65536)return;if(capabilityCache.has(key)){capabilityCacheBytes-=key.length;capabilityCache.delete(key);}capabilityCache.set(key,value);capabilityCacheBytes+=key.length;while(capabilityCache.size>32||capabilityCacheBytes>262144){const oldest=capabilityCache.keys().next().value;capabilityCache.delete(oldest);capabilityCacheBytes-=oldest.length;}};
    const commandFor=action=>({'copy-object-style':'style.copy','paste-object-style':'style.paste','reset-object-style':'style.reset'})[action];
    const signature=()=>{const data=getData();return JSON.stringify([getRange(),data.style,data.cells.map(row=>row.map(cell=>[cell.hidden||false,cell.rowSpan||1,cell.colSpan||1,cell.style||null,cell.borderFragments||null]))]);};
    const refresh=()=>{
      // Text/caret and track-geometry changes do not alter cell properties.
      // Accepted property commands return the new contract with their draft.
      if(!busy&&available()&&root?.isConnected){const key=signature();if(key!==capabilityKey){capabilityKey=key;capabilities=capabilityCache.get(key)||null;capabilityPending=!capabilities;capabilityAbort?.abort();if(!capabilities){const abort=new AbortController();capabilityAbort=abort;options.styleTransfer('cell-style-capabilities',clone(getData()),{...getRange()},{signal:abort.signal}).then(result=>{if(abort.signal.aborted||!bar.isConnected||key!==capabilityKey)return;capabilityPending=false;capabilities=result.selectionCapabilities;remember(key,capabilities);refresh();refresh.onCapabilities?.();}).catch(error=>{if(!abort.signal.aborted&&bar.isConnected&&key===capabilityKey){capabilityPending=false;message.textContent=error.message;root.removeAttribute('aria-busy');}});}}}
      for(const [action,control]of actions){const capability=capabilities?.commands?.[commandFor(action)];control.disabled=busy||!available()||!capability?.enabled||(action==='paste-object-style'&&options.styleClipboard?.()?.context!=='table-cells');control.title=capability?.reason||'';}
      painterButton.disabled=busy||!available()||!painter&&!capabilities?.commands?.['style.copy']?.enabled;painterButton.setAttribute('aria-pressed',String(!!painter));painterButton.classList.toggle('active',!!painter);keep.hidden=!painter;root?.classList.toggle('keynope-table-style-painter',!!painter);
      if(root){if(busy||capabilityPending||!available())root.setAttribute('aria-busy','true');else root.removeAttribute('aria-busy');}
      document.dispatchEvent(new Event('keynope-style-capabilities'));
    };
    const cancel=()=>{generation++;operationAbort?.abort();painter=null;refresh();};
    const perform=(action,{arm=false,paint=false,property,propertyValue,propertyDelta,targetRange}={})=>{
      if(busy||!available())return Promise.resolve();
      const source=painter,token=generation;
      pending=(async()=>{
        try{
          if(arm||['copy-object-style','paste-object-style','reset-object-style'].includes(action))globalThis.KeynopeTextStyles?.clearPainter();
          before();const data=clone(getData()),stamp=JSON.stringify(data),range={...(targetRange||getRange())};operationAbort=new AbortController();
          busy=true;refresh();refresh.onCapabilities?.();message.textContent='Updating cell style…';
          const result=await options.styleTransfer(action,data,range,{property,propertyValue,propertyDelta,styleData:paint?source?.snapshot:undefined,signal:operationAbort.signal});
          if(!bar.isConnected||token!==generation)return;
          if(JSON.stringify(getData())!==stamp)throw Error('Table changed; try the style operation again.');
          if(result.tableData&&JSON.stringify(result.tableData)!==stamp)await apply(result.tableData);
          if(result.selectionCapabilities&&JSON.stringify(range)===JSON.stringify(getRange())){capabilityAbort?.abort();capabilityPending=false;capabilityKey=signature();capabilities=result.selectionCapabilities;remember(capabilityKey,capabilities);}
          if(arm){painter={snapshot:clone(result.styleClipboard),sticky:sticky.checked};message.textContent='Choose a cell or Shift-click a range to paint. Escape cancels.';}
          else{if(paint&&!source.sticky)cancel();message.textContent=action==='copy-object-style'?'Cell style copied.':action==='reset-object-style'?'Cell formatting reset.':'Cell style applied.';}
        }catch(error){if(error.name!=='AbortError')message.textContent=error.message||'Could not change cell style';}
        finally{busy=false;pending=null;operationAbort=null;refresh();refresh.onCapabilities?.();}
      })();return pending;
    };
    for(const [label,action]of [['Copy cell style','copy-object-style'],['Paste cell style','paste-object-style'],['Reset cell style','reset-object-style']]){
      const control=button(label,()=>perform(action));
      control.setAttribute('aria-label',label);actions.push([action,control]);bar.append(control);
    }
    const painterButton=button('Cell format painter',()=>{if(painter){cancel();message.textContent='Cell format painter cancelled.';}else perform('copy-object-style',{arm:true});});painterButton.setAttribute('aria-label','Cell format painter');
    const keep=document.createElement('label'),sticky=document.createElement('input');sticky.type='checkbox';sticky.setAttribute('aria-label','Keep cell format painter active');sticky.onchange=()=>{if(painter)painter.sticky=sticky.checked;};keep.append(sticky,' Keep painting');
    // A second Escape exits cell/caret editing as before; the first only
    // disarms the painter. Do not let it commit/close an unrelated draft.
    root?.addEventListener('keydown',event=>{if(event.key==='Escape'&&(painter||busy)){event.preventDefault();event.stopImmediatePropagation();cancel();message.textContent='Cell format painter cancelled.';}},true);
    root?.addEventListener('close',()=>{capabilityAbort?.abort();cancel();},{once:true});
    refresh.paint=()=>painter?perform('paste-object-style',{paint:true}):Promise.resolve();
    refresh.isPainting=()=>!!painter;
    refresh.cancelPainter=cancel;
    refresh.pending=()=>pending;
    refresh.capabilities=()=>capabilities;
    refresh.isBusy=()=>busy||!available();
    refresh.setProperty=(key,value,theme=false,targetRange)=>perform('set-selection-property',{property:'cell.'+key+(theme?'Token':''),propertyValue:theme?String(value).replace(/^theme:/,''):value,targetRange});
    refresh.stepProperty=(key,delta)=>perform('set-selection-property',{property:'cell.'+key,propertyDelta:delta});
    refresh.setDefaultProperty=(key,value,theme=false)=>perform('set-selection-property',{property:'table.'+key+(theme?'Token':''),propertyValue:theme?String(value).replace(/^theme:/,''):value});
    refresh.stepDefaultProperty=(key,delta)=>perform('set-selection-property',{property:'table.'+key,propertyDelta:delta});
    if(root){
      root.keynopeTableStyleCommandState=()=>{
        const ready=!busy&&!capabilityPending&&available(),commands=capabilities?.commands||{},clipboard=options.styleClipboard?.();
        return {copy:ready&&!!commands['style.copy']?.enabled,paste:ready&&!!commands['style.paste']?.enabled&&clipboard?.context==='table-cells',reset:ready&&!!commands['style.reset']?.enabled,painter:ready&&(!!painter||!!commands['style.copy']?.enabled)};
      };
      root.keynopeTableStyleCommand=async command=>{
        if(!['copy','paste','reset','painter'].includes(command))return false;
        if(!root.keynopeTableStyleCommandState()[command])return false;
        if(command==='painter'){if(painter)cancel();else await perform('copy-object-style',{arm:true});}
        else await perform({'copy':'copy-object-style','paste':'paste-object-style','reset':'reset-object-style'}[command]);
        return true;
      };
      root.addEventListener('close',()=>{delete root.keynopeTableStyleCommandState;delete root.keynopeTableStyleCommand;},{once:true});
    }
    bar.append(painterButton,keep,message);host.append(bar);refresh();return refresh;
  }
  function showCellProperty(field,transfer,preserveInput=false){
    if(!transfer.capabilities)return false;
    const capability=transfer.capabilities()?.properties?.[(field.scope||'cell')+'.'+field.key+(field.themeRole?'Token':'')],input=field.input;
    input.disabled=!capability?.editable||transfer.isBusy();if(field.reset)field.reset.disabled=input.disabled;for(const step of field.steppers||[])step.disabled=input.disabled;
    // Do not flash the previously accepted number over a just-entered value
    // while its command/projection is pending. Controls are disabled above;
    // the accepted result replaces their values before they are re-enabled.
    if(!capability||transfer.isBusy())return true;
    const mixed=capability.state==='mixed',inherited=capability.provenance==='inherited';
    if(input.type==='checkbox'){input.checked=!!capability.value;input.title=capability.reason||'';return true;}
    input.dataset.scope='cells';input.dataset.mixed=String(mixed);input.dataset.inherited=capability.provenance==='mixed'?'mixed':String(inherited);
    let value=mixed?'':capability.value;
    if(field.themeRole)value=mixed?'mixed':value&&value!=='custom'&&value!=='none'?'theme:'+value:value||'';
    else if(field.choices)value=mixed?'mixed':inherited?'':value;
    if(!preserveInput||document.activeElement!==input)input.value=value??'';
    input.title=mixed?'Mixed values':inherited?'Inherited ('+capability.value+')':capability.provenance==='mixed'?'Mixed inheritance ('+capability.value+')':'Explicit ('+capability.value+')';
    if(field.colour){input.style.setProperty('--keynope-field-colour',mixed||value==='none'?'transparent':value||'transparent');input.setAttribute('aria-description',mixed?'Mixed values':String(capability.value||'Inherited'));}
    else if(input.tagName==='INPUT')input.placeholder=mixed?'Mixed':inherited?'Inherited':'';
    return true;
  }
  function showWordProperty(field,controller,cell,preserveInput=false){
    const key=field.key==='fontId'?'text.font':field.key==='fontSize'?'text.size':field.themeRole?'paint.colorToken':'paint.color';
    const p=controller.propertyState(key),input=field.input,ready=controller.isReady()&&!!p?.editable;
    input.disabled=!ready;field.reset.disabled=!ready;for(const step of field.steppers)step.disabled=!ready;
    input.dataset.scope='text';input.dataset.state=p?.state||'unavailable';
    if(controller.isBusy())return;
    if(!p){input.title='Select text to format.';return;}
    const mixed=p.state==='mixed',inherited=p.provenance==='inherited';
    input.dataset.mixed=String(mixed);input.dataset.inherited=String(inherited);input.dataset.provenance=p.provenance;
    let value=mixed?(field.choices?'mixed':''):inherited?(field.choices?'':field.effective(cell)):p.value;
    if(field.themeRole&&value&&value!=='mixed'&&value!=='custom')value='theme:'+value;
    if(!preserveInput||document.activeElement!==input)input.value=value??'';
    input.title='Selected text · '+(mixed?'Mixed formatting':inherited?'Inherited':p.provenance==='theme'?'Follows the deck theme':'Explicit');
    if(field.colour){input.style.setProperty('--keynope-field-colour',mixed?'transparent':value||'transparent');input.setAttribute('aria-description',input.title);}
    else if(input.tagName==='INPUT')input.placeholder=mixed?'Mixed':inherited?'Inherited':'';
  }
  function tableAppearanceTools(host,options,transfer){
    if(!transfer.setDefaultProperty)return ()=>{};
    const section=document.createElement('details');section.className='keynope-table-appearance';const summary=document.createElement('summary');summary.textContent='Table defaults';section.append(summary);
    const note=document.createElement('p');note.textContent='Defaults apply to inheriting cells. Cell overrides stay intact.';section.append(note);
    const fields=[],grid=document.createElement('div');grid.className='keynope-table-default-fields';section.append(grid);host.append(section);
    function property(label,key,type,choices,targetHost=grid){
      const l=document.createElement('label');l.textContent=label;
      const colour=type==='color'&&!!options.pickColor,input=document.createElement(colour?'button':choices?'select':'input');input.setAttribute('aria-label',label);
      const change=value=>transfer.setDefaultProperty(key,type==='checkbox'?!!value:value===''?'':type==='number'?Number(value):value,type==='theme');
      if(colour){input.type='button';input.className='keynope-colour-field-button';input.innerHTML='<span class="keynope-colour-field-swatch"></span><span>Pick color</span>';input.onclick=()=>options.pickColor(input,input.value||'#ffffff',value=>change(value));}
      else if(choices){for(const[value,title]of choices){const option=document.createElement('option');option.value=value;option.textContent=title;option.disabled=value==='custom'||value==='mixed';input.append(option);}if(!choices.some(([value])=>value==='mixed')){const option=document.createElement('option');option.value='mixed';option.textContent='Mixed';option.disabled=true;input.append(option);}}
      else{input.type=type;if(type==='number'){[input.min,input.max]=tableMetricLimits(key);input.step='any';}}
      if(choices)globalThis.KeynopeFonts?.enhance(input);
      input.onchange=()=>{if(type==='number'&&input.value!==''&&!input.checkValidity()){input.reportValidity();return;}change(type==='checkbox'?input.checked:input.value);};
      const row=document.createElement('div');row.append(input);const steppers=[];
      if(type==='number'){const stepper=document.createElement('div');stepper.className='keynope-table-canvas-number-stepper';for(const [symbol,delta,verb]of [['+',1,'Increase'],['−',-1,'Decrease']]){const step=button(symbol,()=>transfer.stepDefaultProperty(key,delta*tableMetricStep(key)));step.setAttribute('aria-label',verb+' '+label.toLowerCase());steppers.push(step);stepper.append(step);}row.append(stepper);}
      let reset;if(type!=='checkbox'){reset=button('↶',()=>transfer.setDefaultProperty(key,'',type==='theme'));reset.setAttribute('aria-label','Inherit '+label.toLowerCase());row.append(reset);}l.append(row);targetHost.append(l);fields.push({input,reset,steppers,key,choices,colour,themeRole:type==='theme',scope:'table'});
    }
    property('Header row','header','checkbox');property('Banded rows','banded','checkbox');
    property('Header fill','headerFill','color');property('Header fill theme','headerFill','theme',themeChoices);property('Band fill','bandFill','color');property('Band fill theme','bandFill','theme',themeChoices);
    property('Default font','fontId','text',[['','Inherit'],['c64','Keynope C64'],['sans','Sans'],['mono','Mono']]);property('Default font size','fontSize','number');property('Default font width','fontWidth','number');property('Default line spacing','lineHeight','number');
    property('Default text colour','color','color');property('Default text theme','color','theme',themeChoices);property('Default fill','fill','color');property('Default fill theme','fill','theme',themeChoices);
    property('Default alignment','align','text',[['','Inherit'],['left','Left'],['center','Centre'],['right','Right'],['justify','Justify']]);property('Default vertical','vertical','text',[['','Inherit'],['top','Top'],['middle','Middle'],['bottom','Bottom']]);property('Default padding','padding','number');
    property('Default border colour','borderColor','color');property('Default border theme','borderColor','theme',themeChoices);property('Default border width','borderWidth','number');
    const sides=document.createElement('details');sides.className='keynope-table-border-sides';const title=document.createElement('summary');title.textContent='Default border sides';sides.append(title);section.append(sides);
    for(const side of ['left','right','top','bottom']){const group=document.createElement('fieldset'),legend=document.createElement('legend');legend.textContent=side[0].toUpperCase()+side.slice(1);group.className='keynope-table-default-fields';group.append(legend);sides.append(group);property('Default '+side+' border colour','borders.'+side+'.color','color',undefined,group);property('Default '+side+' border theme','borders.'+side+'.color','theme',themeChoices,group);property('Default '+side+' border width','borders.'+side+'.width','number',undefined,group);}
    return ()=>{for(const field of fields)showCellProperty(field,transfer);};
  }
  let active=null,canvasSession=null,editSession=null;
  function button(label,action){const b=document.createElement('button');b.type='button';b.textContent=label;b.onclick=action;return b;}
  function dialog(title){
    if(active){active.focus();return null;}
    const previous=document.activeElement,d=document.createElement('dialog');d.className='keynope-table-dialog';d.setAttribute('aria-label',title);d.setAttribute('aria-modal','true');
    const h=document.createElement('h2');h.textContent=title;d.append(h);document.body.append(d);active=d;
    let pendingCommit=null,onClose=null;d.keynopePendingCommit=()=>pendingCommit;
    const close=()=>{if(pendingCommit)return;d.close();d.remove();active=null;onClose?.();if(previous?.isConnected)previous.focus({preventScroll:true});};
    const runCommit=task=>{
      if(pendingCommit)return pendingCommit;
      const focus=document.activeElement;d.inert=true;d.setAttribute('aria-busy','true');
      pendingCommit=Promise.resolve().then(task).then(()=>{pendingCommit=null;close();},error=>{pendingCommit=null;d.inert=false;d.removeAttribute('aria-busy');if(focus?.isConnected&&d.contains(focus))focus.focus({preventScroll:true});throw error;});
      return pendingCommit;
    };
    d.addEventListener('cancel',event=>{event.preventDefault();if(d.dataset.keynopeSceneDialog!=='table')close();});d.addEventListener('keydown',event=>event.stopPropagation());
    return {d,close,runCommit,setOnClose:callback=>{onClose=callback;}};
  }
  function insert(commit){
    const modal=dialog('Insert table');if(!modal)return;const {d,close}=modal;
    const dimensions=document.createElement('div');dimensions.className='keynope-table-toolbar';
    const inputs=[];
    for(const label of ['Rows','Columns']){const l=document.createElement('label');l.textContent=label;const input=document.createElement('input');input.type='number';input.min=1;input.max=100;input.value=3;input.setAttribute('aria-label',label);l.append(input);dimensions.append(l);inputs.push(input);}
    const grid=document.createElement('div');grid.className='keynope-table-insert-grid';grid.setAttribute('aria-label','Quick table sizes');
    for(let r=1;r<=6;r++)for(let c=1;c<=8;c++){const b=button('',()=>{inputs[0].value=r;inputs[1].value=c;highlight(r,c);});b.setAttribute('aria-label',r+' rows by '+c+' columns');b.dataset.row=r;b.dataset.column=c;grid.append(b);}
    function highlight(r,c){for(const b of grid.children)b.classList.toggle('active',Number(b.dataset.row)<=r&&Number(b.dataset.column)<=c);}highlight(3,3);
    inputs.forEach(input=>input.oninput=()=>highlight(Number(inputs[0].value),Number(inputs[1].value)));
    const error=document.createElement('p');error.setAttribute('role','alert');
    const apply=button('Insert',async()=>{if(inputs.some(i=>!i.checkValidity()||!i.value)){inputs.find(i=>!i.checkValidity()||!i.value).reportValidity();return;}const rows=Number(inputs[0].value),columns=Number(inputs[1].value);try{await modal.runCommit(()=>commit(rows,columns));}catch(e){error.textContent=e.message;}});
    const actions=document.createElement('div');actions.className='keynope-table-actions';actions.append(button('Cancel',close),apply);d.append(dimensions,grid,error,actions);d.showModal();inputs[0].focus();
  }
  // RFC-style quoted fields also handle spreadsheet tabs and multiline cells.
  function tableUTF8Bytes(text,limit=1<<20){
    let bytes=0;for(const char of text){const code=char.codePointAt(0);bytes+=code<0x80?1:code<0x800?2:code<0x10000?3:4;if(bytes>limit)break;}return bytes;
  }
  function parseDelimited(input,separator){
	if(typeof input!=='string')throw Error('Paste table text.');
	if(separator!==','&&separator!=='\t')throw Error('Choose a comma or tab separator.');
	const limit=1<<20,sizeError=()=>Error('Table paste is limited to 1 MiB.');
	if(input.length>limit)throw sizeError();
	// Count UTF-8 before allocating fields; the Go payload budget uses bytes,
	// not UTF-16 length. Lone surrogates count as the replacement character.
	if(tableUTF8Bytes(input,limit)>limit)throw sizeError();
    const rows=[[]];let value='',quoted=false;
	const dimensionsError=()=>Error('Paste is limited to 100 rows and columns.');
	const pushField=()=>{if(rows.at(-1).length>=100)throw dimensionsError();rows.at(-1).push(value);value='';};
    for(let i=0;i<input.length;i++){
      const ch=input[i];
      if(ch==='"'&&(quoted||value==='')){if(quoted&&input[i+1]==='"'){value+='"';i++;}else quoted=!quoted;}
      else if(!quoted&&(ch===separator||ch==='\n'||ch==='\r')){pushField();if(ch!==separator){if(ch==='\r'&&input[i+1]==='\n')i++;if(rows.length>=100&&i+1<input.length)throw dimensionsError();rows.push([]);}}
      else value+=ch;
    }
    if(quoted)throw Error('Unclosed quote in pasted table.');pushField();
    if(rows.length>1&&rows.at(-1).length===1&&rows.at(-1)[0]==='')rows.pop();
    if(rows.length>100)throw dimensionsError();return rows;
  }
  function anchors(data){const a=[];data.cells.forEach((row,r)=>row.forEach((cell,c)=>{if(!cell.hidden)a.push({r,c,rs:cell.rowSpan||1,cs:cell.colSpan||1,cell:clone(cell)});}));return a;}
  // Whole-cell border controls deliberately replace that property on all four
  // sides. Imported edge-specific values must not silently defeat the edit.
  function setCellStyle(cell,key,value){
    const match=key.match(/^borders\.(left|right|top|bottom)\.(color|width)$/),part=key==='borderColor'?'color':key==='borderWidth'?'width':match?.[2];
    if(part)for(const side of match?[match[1]]:['left','right','top','bottom']){
      for(const edge of cell.borderFragments?.[side]||[])delete edge[part];
      if(cell.borderFragments?.[side]?.every(edge=>!Object.keys(edge).length))delete cell.borderFragments[side];
    }
    if(cell.borderFragments&&!Object.keys(cell.borderFragments).length)delete cell.borderFragments;
    cell.style||={};
    const path=key.split('.');
    if(path.length===3&&path[0]==='borders'){
      cell.style.borders||={};const edge=cell.style.borders[path[1]]||={};
      if(value==null||value==='')delete edge[path[2]];else edge[path[2]]=value;
      if(!Object.keys(edge).length)delete cell.style.borders[path[1]];
      if(!Object.keys(cell.style.borders).length)delete cell.style.borders;
      if(!Object.keys(cell.style).length)delete cell.style;return;
    }
    if(key==='borderColor'||key==='borderWidth'){
      const property=key==='borderColor'?'color':'width';
      for(const [side,border]of Object.entries(cell.style.borders||{})){
        delete border[property];if(!Object.keys(border).length)delete cell.style.borders[side];
      }
      if(cell.style.borders&&!Object.keys(cell.style.borders).length)delete cell.style.borders;
    }
    if(value==null||value==='')delete cell.style[key];else cell.style[key]=value;
    if(!Object.keys(cell.style).length)delete cell.style;
  }
  function cellStyleValues(style,key,fragments={}){
    const sideValues=(side,part)=>{const base=style?.borders?.[side]?.[part]??style?.[part==='color'?'borderColor':'borderWidth'];return fragments[side]?.map(edge=>edge[part]??base)||[base];};
    const path=key.split('.');if(path.length===3&&path[0]==='borders')return sideValues(path[1],path[2]);
    if(key==='borderColor'||key==='borderWidth'){const property=key==='borderColor'?'color':'width';return ['left','right','top','bottom'].flatMap(side=>sideValues(side,property));}
    return [style?.[key]];
  }
  function rebuild(data,list){
    data.cells=data.rows.map(()=>data.columns.map(()=>({})));
    for(const a of list){const cell={...a.cell};delete cell.hidden;delete cell.rowSpan;delete cell.colSpan;if(a.rs>1)cell.rowSpan=a.rs;if(a.cs>1)cell.colSpan=a.cs;data.cells[a.r][a.c]=cell;
      for(let r=a.r;r<a.r+a.rs;r++)for(let c=a.c;c<a.c+a.cs;c++)if(r!==a.r||c!==a.c)data.cells[r][c]={hidden:true};}
  }
  function track(data,axis,index,remove){
    const rows=axis==='row',items=rows?data.rows:data.columns,start=rows?'r':'c',span=rows?'rs':'cs';
    if(!['row','column'].includes(axis)||!Number.isInteger(index)||index<0||index>items.length-(remove?1:0))throw Error('Invalid table track.');
    if(remove&&items.length===1)throw Error('Keep at least one '+axis+'.');if(!remove&&items.length===100)throw Error('Maximum 100 '+axis+'s.');
    const list=anchors(data);
    // Owner-relative perimeter arrays follow track edits, including deletion
    // of the original anchor. New interior segments inherit that side's style.
    for(const a of list)if(index>=a[start]&&index<a[start]+a[span]&&(remove||index>a[start])){
      for(const side of rows?['left','right']:['top','bottom']){
        const fragments=a.cell.borderFragments?.[side];if(!fragments)continue;
        fragments.splice(index-a[start],remove?1:0,...(remove?[]:[{}]));
        if(!fragments.length)delete a.cell.borderFragments[side];
      }
      if(a.cell.borderFragments&&!Object.keys(a.cell.borderFragments).length)delete a.cell.borderFragments;
    }
    if(remove){items.splice(index,1);for(const a of list){if(a[start]>index)a[start]--;else if(a[start]+a[span]>index)a[span]--;}}
    // Track weights may be normalized ratios or physical measurements from
    // PPTX/Fit. A literal weight of 1 can create an almost invisible new track.
    else{const dimension=rows?'height':'width',weight=items[Math.max(0,index-1)]?.[dimension]||1;items.splice(index,0,{[dimension]:weight});for(const a of list){if(a[start]>=index)a[start]++;else if(a[start]+a[span]>index)a[span]++;}}
    rebuild(data,list.filter(a=>a[span]>0));
  }
  function mergeCells(data,b){
    if(b.r0===b.r1&&b.c0===b.c1)throw Error('Shift-click to select a rectangular range first.');
    const list=anchors(data),inside=[];
    for(const a of list){if(!(a.r<=b.r1&&a.r+a.rs>b.r0&&a.c<=b.c1&&a.c+a.cs>b.c0))continue;if(a.r<b.r0||a.c<b.c0||a.r+a.rs-1>b.r1||a.c+a.cs-1>b.c1)throw Error('Select the entire merged cell.');inside.push(a);}
    const owner=inside.find(a=>a.r===b.r0&&a.c===b.c0),runs=[];
    if(!owner)throw Error('Select the entire merged cell.');
    for(const a of inside){const value=a.cell.runs?.length?a.cell.runs:[{text:a.cell.text||''}];if(value.some(run=>run.text)){if(runs.length)runs.push({text:'\n'});runs.push(...value);}}
    const fragments={};
    for(const side of ['left','right','top','bottom']){
      const vertical=side==='left'||side==='right',length=vertical?b.r1-b.r0+1:b.c1-b.c0+1,segments=Array.from({length},()=>({}));
      for(const a of inside){
        const exterior=side==='left'?a.c===b.c0:side==='right'?a.c+a.cs-1===b.c1:side==='top'?a.r===b.r0:a.r+a.rs-1===b.r1;
        if(!exterior)continue;
        for(let i=0;i<(vertical?a.rs:a.cs);i++)segments[(vertical?a.r-b.r0:a.c-b.c0)+i]=authoredEdge(a.cell,side,i);
      }
      if(segments.some(edge=>Object.keys(edge).length))fragments[side]=segments;
    }
    clearAuthoredBorders(owner.cell);owner.cell.borderFragments=fragments;
    if(!Object.keys(fragments).length)delete owner.cell.borderFragments;
    owner.cell.runs=runs;delete owner.cell.text;owner.rs=b.r1-b.r0+1;owner.cs=b.c1-b.c0+1;rebuild(data,list.filter(a=>!inside.includes(a)).concat(owner));
  }
  function authoredEdge(cell,side,index){
    const style=cell.style||{},edge=clone(style.borders?.[side]||{}),fragment=cell.borderFragments?.[side]?.[index]||{};
    if(edge.color==null&&style.borderColor!=null)edge.color=style.borderColor;
    if(edge.width==null&&style.borderWidth!=null)edge.width=style.borderWidth;
    return {...edge,...clone(fragment)};
  }
  function clearAuthoredBorders(cell){
    if(cell.style){delete cell.style.borderColor;delete cell.style.borderWidth;delete cell.style.borders;if(!Object.keys(cell.style).length)delete cell.style;}
    delete cell.borderFragments;
  }
  function splitCells(data,b){
    const result=[];
    for(const a of anchors(data)){
      if(!(a.r<=b.r1&&a.r+a.rs>b.r0&&a.c<=b.c1&&a.c+a.cs>b.c0)||(a.rs===1&&a.cs===1)){result.push(a);continue;}
      const original=clone(a.cell);
      for(let r=0;r<a.rs;r++)for(let c=0;c<a.cs;c++){
        const cell=r===0&&c===0?clone(original):{};clearAuthoredBorders(cell);
        const sides={};for(const side of ['left','right','top','bottom']){
          const exterior=side==='left'?c===0:side==='right'?c===a.cs-1:side==='top'?r===0:r===a.rs-1;
          if(!exterior)continue;const edge=authoredEdge(original,side,side==='left'||side==='right'?r:c);if(Object.keys(edge).length)sides[side]=edge;
        }
        if(Object.keys(sides).length){cell.style||={};cell.style.borders=sides;}
        result.push({r:a.r+r,c:a.c+c,rs:1,cs:1,cell});
      }
    }
    rebuild(data,result);
  }
  function pasteData(data,r,c,matrix){
    const height=matrix.length,width=Math.max(...matrix.map(row=>row.length));if(r+height>100||c+width>100)throw Error('Paste extends beyond 100 rows or columns.');
    // Validate the entire occupied region before changing any cells or tracks.
    for(let y=0;y<height;y++)for(let x=0;x<matrix[y].length;x++){const cell=data.cells[r+y]?.[c+x];if(cell&&(cell.hidden||cell.rowSpan>1||cell.colSpan>1))throw Error('Split merged cells before pasting over them.');}
    // Count the resulting payload before allocating tracks or overwriting runs.
    // Replaced content does not count, but untouched rich runs still do.
    let bytes=0;const limit=1<<20,add=text=>{bytes+=tableUTF8Bytes(text||'',limit-bytes);if(bytes>limit)throw Error('Table text is limited to 1 MiB. Paste less text or replace more cells.');};
    data.cells.forEach((row,y)=>row.forEach((cell,x)=>{if(y>=r&&y<r+height&&x>=c&&x<c+matrix[y-r].length)return;add(cell.text);for(const run of cell.runs||[])add(run.text);}));
    for(const row of matrix)for(const text of row)add(text);
    while(data.rows.length<r+height)track(data,'row',data.rows.length,false);while(data.columns.length<c+width)track(data,'column',data.columns.length,false);
    matrix.forEach((row,y)=>row.forEach((text,x)=>{const cell=data.cells[r+y][c+x];cell.text=text;delete cell.runs;}));
    return {r:r+height-1,c:c+width-1};
  }
  const topology=data=>JSON.stringify([data.rows.length,data.columns.length,data.cells.map(row=>row.map(cell=>[!!cell.hidden,cell.rowSpan||1,cell.colSpan||1]))]);
  function columnName(index){let name='';for(index++;index;index=Math.floor((index-1)/26))name=String.fromCharCode(65+(index-1)%26)+name;return name;}
  function resizeTracks(data,axis,index,delta,minimum){
    const key=axis==='rows'?'height':'width',items=data[axis];
    const limits=Array.isArray(minimum)?minimum:[minimum,minimum];
    if(!['rows','columns'].includes(axis)||!items||!Number.isInteger(index)||index<0||index>=items.length-1||!Number.isFinite(delta)||limits.length!==2||limits.some(v=>!Number.isFinite(v)||v<=0))throw Error('Invalid track resize.');
    const a=items[index][key],b=items[index+1][key],sum=a+b;if(!(a>0)||!(b>0)||!Number.isFinite(sum))throw Error('Track weights are too large to resize. Set smaller weights in Table formatting.');
    // Existing undersized imported tracks may grow, but must never get smaller.
    const minA=Math.min(limits[0],a),minB=Math.min(limits[1],b);
    const value=Math.max(minA,Math.min(sum-minB,a+delta));
    items[index][key]=value;items[index+1][key]=sum-value;
  }
  function resizeProportionalTracks(data,axis,index,delta,minimum){
    const key=axis==='rows'?'height':'width',items=data[axis];
    if(!['rows','columns'].includes(axis)||!items||!Number.isInteger(index)||index<0||index>=items.length-1||!Number.isFinite(delta)||!Array.isArray(minimum)||minimum.length!==items.length||minimum.some(v=>!Number.isFinite(v)||v<=0))throw Error('Invalid proportional track resize.');
    const weights=items.map(item=>item[key]),left=weights.slice(0,index+1),right=weights.slice(index+1),sum=list=>list.reduce((n,v)=>n+v,0),a=sum(left),b=sum(right);
    if(weights.some(v=>!Number.isFinite(v)||v<=0)||!Number.isFinite(a+b))throw Error('Track weights are too large to resize. Set smaller weights in Table formatting.');
    // Move this separator, scaling all tracks on each side as a set. Their
    // internal ratios and the table's total extent remain unchanged.
    const minLeft=Math.max(...left.map((v,i)=>Math.min(1,minimum[i]/v))),minRight=Math.max(...right.map((v,i)=>Math.min(1,minimum[index+1+i]/v)));
    const shift=Math.max(a*(minLeft-1),Math.min(b*(1-minRight),delta));
    weights.forEach((v,i)=>items[i][key]=v*(i<=index?(a+shift)/a:(b-shift)/b));
  }
  function trackMinimums(data,axis,pixels,readable){
    const key=axis==='rows'?'height':'width';
    return data[axis].map((item,i)=>item[key]*Math.min(1,(Array.isArray(readable)?readable[i]:readable)/Math.max(1,pixels[i])));
  }
  function edit(source,commit,options={}){
    const modal=dialog('Edit table');if(!modal)return;const {d,close}=modal;
    let data=clone(source),selected={...(options.selection?.start||{r:0,c:0})},end={...(options.selection?.end||selected)},trackGesture=null;
    const history=draftHistory();
    const baseline=JSON.stringify(data);let cachedDirty=null;
    const dirty=()=>cachedDirty??(cachedDirty=JSON.stringify(data)!==baseline);
    const tools=document.createElement('div');tools.className='keynope-table-toolbar';
    const gridHost=document.createElement('div');gridHost.className='keynope-table-grid-host';
    const style=document.createElement('div');style.className='keynope-table-toolbar';
    const status=document.createElement('p');status.setAttribute('role','status');
    const error=document.createElement('p');error.setAttribute('role','alert');
    const rect=()=>({r0:Math.min(selected.r,end.r),r1:Math.max(selected.r,end.r),c0:Math.min(selected.c,end.c),c1:Math.max(selected.c,end.c)});
    const intersects=(r,c,cell,b)=>r<=b.r1&&r+(cell.rowSpan||1)>b.r0&&c<=b.c1&&c+(cell.colSpan||1)>b.c0;
    const cells=()=>{const b=rect(),a=[];data.cells.forEach((row,r)=>row.forEach((cell,c)=>{if(!cell.hidden&&intersects(r,c,cell,b))a.push(cell);}));return a;};
    const cellReadOnly=cell=>{
      const theme=options.theme?.(),resolve=id=>id?.startsWith('theme:')?theme?.fonts?.[id.slice(6)]?.fontId||'sans':!id||id==='theme'?theme?.fonts?.body?.fontId||'sans':id;
      const base=resolve(cell.style?.fontId||data.style?.cell?.fontId);
      return [base,...(cell.runs||[]).map(run=>resolve(run.fontRole?'theme:'+run.fontRole:run.fontId||base))].some(id=>globalThis.KeynopeFonts?.previewOnly(id));
    };
    const change=action=>{trackGesture?.cancel();const before=clone(data);try{action();}catch(e){data=before;error.textContent=e.message;return;}history.record(before,data);render();error.textContent='';};
    const restore=redo=>{if(trackGesture){trackGesture.cancel();return;}const next=history.peek(redo);if(!next)return;history.advance(data,redo);data=next;render();};
    const undo=()=>restore(false),redo=()=>restore(true);
    const refreshStyleTransfer=cellStyleTransferTools(tools,options,()=>data,rect,next=>change(()=>{data=next;}),{root:d});
    tools.append(button('Undo',undo),button('Redo',redo));
    for(const axis of ['row','column']){
      tools.append(button('Add '+axis,()=>change(()=>track(data,axis,(axis==='row'?selected.r:selected.c)+1,false))),button('Remove '+axis,()=>change(()=>{track(data,axis,axis==='row'?selected.r:selected.c,true);selected.r=Math.min(selected.r,data.rows.length-1);selected.c=Math.min(selected.c,data.columns.length-1);end={...selected};})));
    }
    tools.append(button('Merge cells',()=>change(()=>{const b=rect();mergeCells(data,b);selected={r:b.r0,c:b.c0};end={...selected};})),button('Split cells',()=>change(()=>splitCells(data,rect()))));
    const appearanceHost=document.createElement('div'),refreshAppearance=tableAppearanceTools(appearanceHost,options,refreshStyleTransfer);
    function property(label,key,type,choices,targetHost=style){
      const l=document.createElement('label');l.textContent=label;const colour=type==='color'&&typeof options.pickColor==='function',input=document.createElement(colour?'button':choices?'select':'input');input.setAttribute('aria-label','Cell '+label.toLowerCase());
      if(colour){input.type='button';input.className='keynope-colour-field-button';input.innerHTML='<span class="keynope-colour-field-swatch"></span><span>Pick color</span>';input.onclick=()=>{const range={...rect()};options.pickColor(input,tableColour(input.value,options)||'#ffffff',value=>{if(refreshStyleTransfer.setProperty){refreshStyleTransfer.setProperty(key,value,false,range);return;}input.value=value;input.onchange();});};}
      else if(choices){for(const [value,text]of choices){const o=document.createElement('option');o.value=value;o.textContent=text;input.append(o);}if(!choices.some(([value])=>value==='mixed')){const option=document.createElement('option');option.value='mixed';option.textContent='Mixed';option.disabled=true;input.append(option);}}else{input.type=type;input.value=type==='color'?'#ffffff':'';if(type==='number'){[input.min,input.max]=tableMetricLimits(key);input.step='any';}}
      if(choices)globalThis.KeynopeFonts?.enhance(input);
      input.onchange=()=>{if(type==='number'&&input.value&&!input.checkValidity()){input.reportValidity();return;}if(refreshStyleTransfer.setProperty){refreshStyleTransfer.setProperty(key,input.value===''?'':type==='number'?Number(input.value):input.value,type==='theme');return;}change(()=>{for(const cell of cells())setCellStyle(cell,key,input.value===''?null:type==='number'?Number(input.value):input.value);});};
      const reset=button('↶',()=>{if(refreshStyleTransfer.setProperty){refreshStyleTransfer.setProperty(key,'',type==='theme');return;}change(()=>{for(const cell of cells())setCellStyle(cell,key,null);});});reset.setAttribute('aria-label','Inherit cell '+label.toLowerCase());l.append(input,reset);targetHost.append(l);
      if(type==='theme')for(const option of input.options)if(['custom','mixed'].includes(option.value))option.disabled=true;
      return {input,reset,key,colour,choices,themeRole:type==='theme'};
    }
    const fields=[property('Text colour','color','color'),property('Text theme','color','theme',themeChoices),property('Fill','fill','color'),property('Fill theme','fill','theme',themeChoices),property('Border','borderColor','color'),property('Border theme','borderColor','theme',themeChoices),property('Border width','borderWidth','number'),property('Font size','fontSize','number'),property('Font width','fontWidth','number'),property('Line spacing','lineHeight','number'),property('Padding','padding','number'),property('Font','fontId','text',[['','Inherit'],['c64','Keynope C64'],['sans','Sans'],['mono','Mono']]),property('Alignment','align','text',[['','Inherit'],['left','Left'],['center','Centre'],['right','Right'],['justify','Justify']]),property('Vertical','vertical','text',[['','Inherit'],['top','Top'],['middle','Middle'],['bottom','Bottom']])];
    const borderSides=document.createElement('details');borderSides.className='keynope-table-border-sides';const borderSummary=document.createElement('summary');borderSummary.textContent='Cell border sides';borderSides.append(borderSummary);
    for(const side of ['left','right','top','bottom']){const group=document.createElement('fieldset'),legend=document.createElement('legend');legend.textContent=side[0].toUpperCase()+side.slice(1);group.append(legend);borderSides.append(group);for(const [label,key,type,choices]of [[side+' border colour','color','color'],[side+' border theme','color','theme',themeChoices],[side+' border width','width','number']])fields.push(property(label,'borders.'+side+'.'+key,type,choices,group));}
    const widths=document.createElement('div');widths.className='keynope-table-toolbar';
    for(const [axis,label]of [['columns','Column width'],['rows','Row height']]){const l=document.createElement('label');l.textContent=label+' (weight)';const input=document.createElement('input');input.type='number';input.min=.1;input.step=.1;input.setAttribute('aria-label',label+' weight');input.onchange=()=>{if(!input.value||!input.checkValidity()){input.reportValidity();return;}change(()=>{const index=axis==='rows'?selected.r:selected.c;data[axis][index][axis==='rows'?'height':'width']=Number(input.value);});};input.dataset.track=axis;l.append(input);widths.append(l);}
    function select(r,c,extend){if(!extend)selected={r,c};end={r,c};refreshSelection();}
    function resizeHandle(axis,index){
      const handle=document.createElement('span'),key=axis==='rows'?'height':'width';
      handle.dataset.resizeAxis=axis;handle.dataset.resizeIndex=index;
      handle.className='keynope-table-track-handle '+(axis==='rows'?'row':'column');handle.tabIndex=0;handle.setAttribute('role','separator');handle.setAttribute('aria-orientation',axis==='rows'?'horizontal':'vertical');handle.setAttribute('aria-label','Resize '+(axis==='rows'?'row '+(index+1):'column '+columnName(index)));
      handle.title='Drag to resize adjacent tracks. Option/Alt scales all tracks on each side.';
      const pixelSizes=()=>{const table=gridHost.querySelector('table');return axis==='rows'?[...table.querySelectorAll('tr[data-row]')].map(row=>row.getBoundingClientRect().height):[...table.querySelectorAll('th[data-column]')].map(col=>col.getBoundingClientRect().width);};
      handle.onkeydown=event=>{if(refreshStyleTransfer.isBusy?.())return;const negative=axis==='rows'?'ArrowUp':'ArrowLeft',positive=axis==='rows'?'ArrowDown':'ArrowRight';if(event.key!==negative&&event.key!==positive)return;event.preventDefault();const pixels=pixelSizes(),minimum=trackMinimums(data,axis,pixels,axis==='rows'?24:40),pair=data[axis][index][key]+data[axis][index+1][key];change(()=>event.altKey?resizeProportionalTracks(data,axis,index,pair*.025*(event.key===negative?-1:1),minimum):resizeTracks(data,axis,index,pair*.025*(event.key===negative?-1:1),minimum.slice(index,index+2)));gridHost.querySelector('[aria-label="'+handle.getAttribute('aria-label')+'"]')?.focus();};
      handle.onpointerdown=event=>{
        if(event.button!==0||trackGesture||refreshStyleTransfer.isBusy?.())return;event.preventDefault();event.stopPropagation();
        const before=clone(data),total=data[axis].reduce((n,item)=>n+item[key],0),origin=axis==='rows'?event.clientY:event.clientX,proportional=event.altKey;
        const table=gridHost.querySelector('table');
        const sizes=pixelSizes(),pixels=sizes.reduce((n,v)=>n+v,0),minimum=trackMinimums(data,axis,sizes,axis==='rows'?24:40);let moved=false;
        if(!Number.isFinite(total)||!(pixels>0)){error.textContent='Track weights are too large to resize. Set smaller weights in Table formatting.';return;}
        handle.setPointerCapture(event.pointerId);
        const move=e=>{const delta=(axis==='rows'?e.clientY:e.clientX)-origin,weightDelta=total*Math.max(-1,Math.min(1,delta/pixels));data[axis]=clone(before[axis]);if(proportional)resizeProportionalTracks(data,axis,index,weightDelta,minimum);else resizeTracks(data,axis,index,weightDelta,minimum.slice(index,index+2));cachedDirty=null;moved=true;applyTrackSizes(table);refreshSelection();};
        const finish=()=>{if(trackGesture!==gesture)return;trackGesture=null;handle.removeEventListener('pointermove',move);handle.removeEventListener('pointerup',finish);handle.removeEventListener('pointercancel',cancel);handle.removeEventListener('lostpointercapture',cancel);window.removeEventListener('blur',cancel);if(handle.hasPointerCapture(event.pointerId))handle.releasePointerCapture(event.pointerId);if(moved)history.record(before,data);render();};
        const cancel=()=>{data=before;moved=false;finish();};
        const gesture={finish,cancel};trackGesture=gesture;
        handle.addEventListener('pointermove',move);handle.addEventListener('pointerup',finish);handle.addEventListener('pointercancel',cancel);handle.addEventListener('lostpointercapture',cancel);window.addEventListener('blur',cancel);
      };return handle;
    }
    function applyTrackSizes(table){
      const maximum=Math.max(...data.columns.map(c=>c.width)),total=data.columns.reduce((n,c)=>n+c.width/maximum,0);
      [...table.querySelectorAll('col[data-track]')].forEach((col,index)=>col.style.width=(100*(data.columns[index].width/maximum)/total)+'%');
      const sum=data.rows.reduce((n,row)=>n+row.height,0),average=sum/data.rows.length;
      [...table.querySelectorAll('tr[data-row]')].forEach((row,index)=>row.style.height=Math.max(30,Math.min(540,data.rows[index].height/average*72))+'px');
    }
    function refreshSelection(){
      refreshStyleTransfer();
      refreshAppearance();
      const busy=!!refreshStyleTransfer.isBusy?.();for(const control of tools.querySelectorAll(':scope>button,:scope>label>input'))control.disabled=busy;for(const input of widths.querySelectorAll('input'))input.disabled=busy;
      const b=rect();for(const td of gridHost.querySelectorAll('[data-cell]')){const [r,c]=td.dataset.cell.split(':').map(Number);td.classList.toggle('selected',intersects(r,c,data.cells[r][c],b));}
      status.textContent='Cell '+columnName(selected.c)+(selected.r+1)+' · '+data.rows.length+' rows × '+data.columns.length+' columns. Shift-click selects a range. Drag headers’ separators to resize.';
      const selectedCells=cells();for(const field of fields){if(showCellProperty(field,refreshStyleTransfer))continue;const {input,key,colour,themeRole}=field,values=selectedCells.flatMap(cell=>cellStyleValues(cell.style,key,cell.borderFragments));const mixed=values.some(v=>v!==values[0]);input.dataset.mixed=mixed?'true':'false';if(themeRole)input.value=mixed?'mixed':themeChoice(values[0]);else if(input.type==='color')input.value=!mixed&&/^#[0-9a-f]{6}$/i.test(tableColour(values[0],options)||'')?tableColour(values[0],options):'#ffffff';else input.value=mixed?'':values[0]??'';input.title=mixed?'Mixed values':values[0]??'Inherited';if(colour){const color=tableColour(input.value,options);input.style.setProperty('--keynope-field-colour',color&&color!=='none'?color:'transparent');input.setAttribute('aria-description',mixed?'Mixed values':input.value||'Inherited');}}
      widths.querySelector('[data-track="rows"]').value=data.rows[selected.r].height;widths.querySelector('[data-track="columns"]').value=data.columns[selected.c].width;
      for(const handle of gridHost.querySelectorAll('[data-resize-axis]')){const axis=handle.dataset.resizeAxis,index=Number(handle.dataset.resizeIndex),key=axis==='rows'?'height':'width';handle.setAttribute('aria-valuemin','0');handle.setAttribute('aria-valuemax',String(data[axis][index][key]+data[axis][index+1][key]));handle.setAttribute('aria-valuenow',String(data[axis][index][key]));}
    }
    function render(){
      cachedDirty=null;
      selected.r=Math.min(selected.r,data.rows.length-1);selected.c=Math.min(selected.c,data.columns.length-1);end.r=Math.min(end.r,data.rows.length-1);end.c=Math.min(end.c,data.columns.length-1);
      const table=document.createElement('table'),cols=document.createElement('colgroup');table.setAttribute('aria-label','Editable table cells');table.style.minWidth=Math.max(420,data.columns.length*88+40)+'px';
      const cornerCol=document.createElement('col');cornerCol.style.width='40px';cols.append(cornerCol);data.columns.forEach(()=>{const col=document.createElement('col');col.dataset.track='';cols.append(col);});table.append(cols);
      const head=document.createElement('thead'),headRow=document.createElement('tr'),corner=document.createElement('th');corner.append(button('All',()=>{selected={r:0,c:0};end={r:data.rows.length-1,c:data.columns.length-1};refreshSelection();}));headRow.append(corner);
      data.columns.forEach((_,c)=>{const th=document.createElement('th');th.dataset.column=c;const b=button(columnName(c),()=>{selected={r:0,c};end={r:data.rows.length-1,c};refreshSelection();});b.setAttribute('aria-label','Select column '+columnName(c));th.append(b);if(c<data.columns.length-1)th.append(resizeHandle('columns',c));headRow.append(th);});head.append(headRow);table.append(head);
      data.cells.forEach((row,r)=>{const tr=document.createElement('tr');tr.dataset.row=r;const th=document.createElement('th');th.scope='row';const b=button(String(r+1),()=>{selected={r,c:0};end={r,c:data.columns.length-1};refreshSelection();});b.setAttribute('aria-label','Select row '+(r+1));th.append(b);if(r<data.rows.length-1)th.append(resizeHandle('rows',r));tr.append(th);row.forEach((cell,c)=>{
        if(cell.hidden)return;const td=document.createElement('td');td.rowSpan=cell.rowSpan||1;td.colSpan=cell.colSpan||1;td.dataset.cell=r+':'+c;
        const input=document.createElement('textarea');input.setAttribute('aria-label','Cell '+(r+1)+', '+(c+1));input.value=cell.runs?.length?cell.runs.map(r=>r.text).join(''):cell.text||'';
        input.readOnly=cellReadOnly(cell);if(input.readOnly)input.title='Preview & Print font — choose another font to edit this text';
        input.onpointerdown=e=>{select(r,c,e.shiftKey);if(e.shiftKey||refreshStyleTransfer.isPainting?.())e.preventDefault();refreshStyleTransfer.paint?.();};input.onfocus=()=>{if(selected.r!==r||selected.c!==c)select(r,c,false);};
        input.oninput=()=>{const before=clone(data);cell.text=input.value;delete cell.runs;history.record(before,data);cachedDirty=null;options.onDirty?.();};
        input.onkeydown=event=>{
          if(event.key==='Tab'&&!event.metaKey&&!event.ctrlKey&&!event.altKey){event.preventDefault();const list=[...gridHost.querySelectorAll('textarea')],index=list.indexOf(input),next=list[(index+(event.shiftKey?-1:1)+list.length)%list.length];next.focus();next.select();}
        };
        input.onpaste=event=>{if(input.readOnly){event.preventDefault();return;}const value=event.clipboardData?.getData('text/plain')||'';if(!value.includes('\t')&&!value.includes('\n'))return;event.preventDefault();change(()=>{
          const matrix=parseDelimited(value,value.includes('\t')?'\t':',');end=pasteData(data,r,c,matrix);selected={r,c};
        });};
        td.append(input);tr.append(td);
      });table.append(tr);});applyTrackSizes(table);gridHost.replaceChildren(table);refreshSelection();options.onDirty?.();
    }
    d.addEventListener('keydown',event=>{if(!(event.metaKey||event.ctrlKey))return;const key=event.key.toLowerCase();if(key==='z'||key==='y'){event.preventDefault();key==='y'||event.shiftKey?redo():undo();}});
    const paste=document.createElement('textarea');paste.placeholder='Paste TSV or CSV data here';paste.setAttribute('aria-label','Paste table data');
    const pasteButton=button('Paste data',()=>change(()=>{const matrix=parseDelimited(paste.value,paste.value.includes('\t')?'\t':',');end=pasteData(data,selected.r,selected.c,matrix);}));
    const copy=button('Copy range',async()=>{const b=rect(),rows=[];for(let r=b.r0;r<=b.r1;r++){const row=[];for(let c=b.c0;c<=b.c1;c++){const cell=data.cells[r][c],value=cell.runs?.length?cell.runs.map(run=>run.text).join(''):cell.text||'';row.push(/[\t\n\r"]/.test(value)?'"'+value.replaceAll('"','""')+'"':value);}rows.push(row.join('\t'));}try{await (options.copyText?options.copyText(rows.join('\n')):navigator.clipboard.writeText(rows.join('\n')));status.textContent='Table range copied.';}catch(e){error.textContent='Could not copy: '+e.message;}});
    const finish=(force=false)=>{trackGesture?.finish();return modal.runCommit(async()=>{await refreshStyleTransfer.pending?.();if(force||dirty())await commit(clone(data));});};
    const session={finish,isDirty:()=>dirty()||!!refreshStyleTransfer.isBusy?.()};editSession=session;
    modal.setOnClose(()=>{trackGesture?.cancel();if(editSession===session)editSession=null;options.onDirty?.();});
    d.addEventListener('keydown',event=>{if(event.key==='Escape'&&trackGesture){event.preventDefault();event.stopImmediatePropagation();trackGesture.cancel();}},true);
    const apply=button('Apply',async()=>{try{await finish(true);}catch(e){error.textContent=e.message;}});
    const actions=document.createElement('div');actions.className='keynope-table-actions';actions.append(copy,button('Cancel',close),apply);const pasteRow=document.createElement('div');pasteRow.className='keynope-table-paste';pasteRow.append(paste,pasteButton);
    refreshStyleTransfer.onCapabilities=()=>{refreshSelection();options.onDirty?.();};d.append(tools,style,borderSides,appearanceHost,widths,gridHost,status,pasteRow,error,actions);render();d.showModal();gridHost.querySelector('textarea')?.focus();
  }
  // Canvas editing owns a portable table draft. Text/cell selection never
  // becomes authored deck selection, and only the final commit enters history.
  async function canvas(object,source,commit,options={}){
    if(!options.canvasScene?.isConnected||!object.table?.cells?.length)throw Error('This table is no longer available on the canvas.');
    object={...object,table:clone(object.table)};
    const modal=dialog('Edit table cells');if(!modal)return;
    const {d,close:closeDialog}=modal;
    d.className='keynope-table-canvas-dialog';d.dataset.keynopeSceneDialog='table';
    const viewport=document.createElement('div'),frame=document.createElement('div'),preview=document.createElement('div'),hits=document.createElement('div'),headers=document.createElement('div'),tools=document.createElement('div'),status=document.createElement('p'),error=document.createElement('p');
    viewport.className='keynope-table-canvas-viewport';frame.className='keynope-table-canvas-frame';tools.className='keynope-table-canvas-tools';hits.className='keynope-table-canvas-hits';
    tools.setAttribute('role','region');tools.setAttribute('aria-label','Table cell editing tools');status.setAttribute('role','status');error.setAttribute('role','alert');
    Object.assign(viewport.style,{width:object.bounds.width+'px',height:object.bounds.height+'px'});frame.style.transform=`rotate(${object.rotation||0}deg)`;
    headers.className='keynope-table-canvas-headers';frame.append(preview,hits,headers);viewport.append(frame);d.replaceChildren(viewport,tools);
    let data=clone(source),selected={r:0,c:0},end={r:0,c:0},caret=null,pending=null,projectionPending=null,projectionAbort=null,closed=false,cancelTrack=null,undoButton,redoButton;
    const history=draftHistory();
    const styleFields=[],structureButtons=[],emphasisButtons=new Map();let stagedView=null,refreshStyleTransfer=()=>{},refreshAppearance=()=>{};
    const wordTools=document.createElement('div'),cellStyleHost=document.createElement('div');wordTools.hidden=true;
    const overflow=document.createElement('div');overflow.className='keynope-table-overflow';overflow.setAttribute('role','status');overflow.setAttribute('aria-label','Table content warnings');
    const overflowCells=new Map(),overflowDirty=new Set();let overflowFrame=0,overflowTimer=0;
    const views=new Map(),cellNodes=new Map(),surfaces=new Map(),emojiFonts=Object.assign({},...object.table.cells.map(cell=>cell.text.emojiFonts||{}));
    const padding=new Map(object.table.cells.map(cell=>[cell.row+':'+cell.column,{x:cell.contentBounds.x,y:cell.contentBounds.y}])),trackHeaders={rows:[],columns:[]};
    const localObject={...object,rotation:0,bounds:{x:0,y:0,width:object.bounds.width,height:object.bounds.height}};
    let view;try{view=KeynopeScene.mount(preview,{version:1,width:object.bounds.width,height:object.bounds.height,objects:[localObject]});}catch(e){closeDialog();throw e;}
    const sourceNode=[...options.canvasScene.children].find(node=>node.dataset.objectId===object.id),sourceVisibility=sourceNode?.style.visibility;
    if(sourceNode)sourceNode.style.visibility='hidden';
    const key=(r,c)=>r+':'+c,plain=cell=>cell.runs?.length?cell.runs.map(run=>run.text).join(''):cell.text||'';
    const cellsByID=new Map(object.table.cells.map(cell=>[key(cell.row,cell.column),cell]));
    function checkOverflow(){cancelAnimationFrame(overflowFrame);clearTimeout(overflowTimer);overflowFrame=overflowTimer=0;if(closed)return;for(const id of overflowDirty){const cell=cellsByID.get(id),node=cellNodes.get(id);if(!cell||!node)continue;const extent=KeynopeScene.tableCellExtent(node,modelFor(cell),cell.contentBounds);views.get(id)?.classList.toggle('overflow',extent.overflow);views.get(id)?.setAttribute('aria-description',extent.overflow?'Text is clipped. Fit table to content or enlarge this cell.':extent.cramped?'Less than one text line is available.':'');if(extent.overflow||extent.cramped)overflowCells.set(id,{cell,...extent});else overflowCells.delete(id);}overflowDirty.clear();overflow.replaceChildren();if(!overflowCells.size)return;const label=document.createElement('p');label.textContent=overflowCells.size+' '+(overflowCells.size===1?'cell needs':'cells need')+' more space. Font sizes are unchanged.';overflow.append(label);for(const {cell,overflow:clipped}of [...overflowCells.values()].slice(0,12)){const b=button('Cell '+columnName(cell.column)+(cell.row+1)+': '+(clipped?'text clipped':'less than one text line'),()=>selectCell(cell));overflow.append(b);}if(overflowCells.size>12){const rest=document.createElement('p');rest.textContent='And '+(overflowCells.size-12)+' more cells.';overflow.append(rest);}}
    // Coalesce per-cell edits to one paint, but don't depend on animation
    // frames being delivered in a background/native WebView. This fallback
    // runs once per edit batch, never as a polling loop.
    function queueOverflow(cell){if(cell)overflowDirty.add(key(cell.row,cell.column));else for(const c of object.table.cells)overflowDirty.add(key(c.row,c.column));if(!overflowFrame&&!closed){overflowFrame=requestAnimationFrame(checkOverflow);overflowTimer=setTimeout(checkOverflow,80);}}
    const rect=()=>({r0:Math.min(selected.r,end.r),r1:Math.max(selected.r,end.r),c0:Math.min(selected.c,end.c),c1:Math.max(selected.c,end.c)});
    const chosen=()=>{const b=rect();return object.table.cells.filter(cell=>cell.row<=b.r1&&cell.row+cell.rowSpan>b.r0&&cell.column<=b.c1&&cell.column+cell.colSpan>b.c0);};
    function rememberCaretRange(){if(!caret)return;const selection=document.getSelection();if(caret.editor.element.contains(selection?.anchorNode)&&caret.editor.element.contains(selection?.focusNode))caret.range=caret.editor.selection();}
    function caretRuns(){rememberCaretRange();const range=caret.range,start=Math.min(range.start,range.end),end=Math.max(range.start,range.end);let offset=0;return caret.editor.value().runs.filter(run=>{const a=offset;offset+=run.text.length;return start===end?a<=start&&offset>=start:a<end&&offset>start;});}
    function formatCaret(style){if(!caret||pending||projectionPending)return;rememberCaretRange();if(caret.editor.isComposing()){error.textContent='Finish typing before formatting.';return;}const range=caret.range;if(range.start===range.end){error.textContent='Select text to format.';return;}caret.editor.select(range.start,range.end);caret.editor.format(style);error.textContent='';refresh();}
    const selectionChanged=()=>{if(caret){rememberCaretRange();refresh(true);}};document.addEventListener('selectionchange',selectionChanged);
    const record=before=>{if(history.record(before,data))options.onDirty?.();};
    function modelFor(cell){const authored=data.cells[cell.row][cell.column],runs=authored.runs?.length?authored.runs:[{text:authored.text||''}];return {...cell.text,emojiFonts,width:cell.contentBounds.width,color:cell.paint.color,colorTokens:Object.fromEntries(themeRoles.map(([role])=>[role,tableColour('theme:'+role,options)])),runs:runs.map(run=>cell.header&&!run.boldOff?{...run,bold:true}:{...run})};}
    function contentHost(cell){const host=document.createElement('div'),b=cell.contentBounds;
      Object.assign(host.style,{position:'absolute',left:b.x+'px',top:b.y+'px',width:b.width+'px',height:b.height+'px',display:'flex',flexDirection:'column',justifyContent:cell.text.vertical==='middle'?'center':cell.text.vertical==='bottom'?'flex-end':'flex-start'});return host;
    }
    function paint(surface,cell){const p=cell.paint,angle=p.gradientDirection==='vertical'?'180deg':p.gradientDirection==='diagonal'?'135deg':'90deg';
      Object.assign(surface.element.style,{color:p.gradientStart?'transparent':p.color||'#ffffff',backgroundImage:p.gradientStart?`linear-gradient(${angle},${p.gradientStart},${p.gradientEnd})`:'',backgroundClip:p.gradientStart?'text':'',webkitBackgroundClip:p.gradientStart?'text':'',webkitTextStroke:p.stroke?`${p.strokeWidth||1}px ${p.stroke}`:'',paintOrder:p.stroke?'stroke fill':'',tabSize:'4'});
    }
    function redrawCell(cell){const id=key(cell.row,cell.column),node=cellNodes.get(id);if(!node)return;
      surfaces.get(id)?.dispose();surfaces.delete(id);node.querySelector('[data-cell-draft]')?.remove();
      for(const child of [...node.children])child.style.visibility='hidden';
      const host=contentHost(cell);host.dataset.cellDraft='true';node.append(host);const surface=KeynopeTextLayout.create(host,modelFor(cell));paint(surface,cell);surfaces.set(id,surface);queueOverflow(cell);surface.ready().then(()=>{if(!closed&&surfaces.get(id)===surface)queueOverflow(cell);}).catch(()=>{});
    }
    function redrawChanges(before){if(JSON.stringify(before.rows)!==JSON.stringify(data.rows)||JSON.stringify(before.columns)!==JSON.stringify(data.columns))layoutTracks();for(const cell of object.table.cells)if(JSON.stringify(before.cells[cell.row][cell.column])!==JSON.stringify(data.cells[cell.row][cell.column]))redrawCell(cell);}
    const needsProjection=(before,next)=>topology(before)!==topology(next)||JSON.stringify(before.style)!==JSON.stringify(next.style)||before.cells.some((row,r)=>row.some((cell,c)=>JSON.stringify(cell.style)!==JSON.stringify(next.cells[r]?.[c]?.style)));
    function validateProjection(table,next){
      if(!table||table.rows?.length!==next.rows.length||table.columns?.length!==next.columns.length||!Array.isArray(table.cells))throw Error('The table preview has invalid tracks.');
      const expected=new Map(anchors(next).map(a=>[key(a.r,a.c),a]));
      if(table.cells.length!==expected.size)throw Error('The table preview has invalid cells.');
      for(const cell of table.cells){const id=key(cell.row,cell.column),a=expected.get(id);if(!a||cell.rowSpan!==a.rs||cell.colSpan!==a.cs||!cell.bounds||!cell.contentBounds||!cell.text||!cell.paint)throw Error('The table preview has invalid cells.');expected.delete(id);}
    }
    async function prepareStructure(table,next){
      validateProjection(table,next);
      const host=document.createElement('div');Object.assign(host.style,{position:'absolute',inset:'0',visibility:'hidden',pointerEvents:'none'});preview.append(host);
      const prepared={...localObject,table:clone(table)};let candidate;
      try{candidate=KeynopeScene.mount(host,{version:1,width:object.bounds.width,height:object.bounds.height,objects:[prepared]});stagedView=candidate;await candidate.ready;
        if(closed)throw new DOMException('Table editing was cancelled.','AbortError');
        for(const cell of prepared.table.cells)if(!candidate.element.querySelector('[data-table-cell="'+key(cell.row,cell.column)+'"]'))throw Error('The table preview is missing a cell.');
        return {candidate,prepared,host};
      }catch(e){candidate?.dispose();host.remove();if(stagedView===candidate)stagedView=null;throw e;}
    }
    function installStructure(prepared){
      for(const surface of surfaces.values())surface.dispose();surfaces.clear();view.dispose();
      object.table=prepared.prepared.table;view=prepared.candidate;preview.append(view.element);prepared.host.remove();stagedView=null;
      padding.clear();cellsByID.clear();Object.keys(emojiFonts).forEach(id=>delete emojiFonts[id]);overflowCells.clear();overflowDirty.clear();
      for(const cell of object.table.cells){const id=key(cell.row,cell.column);cellsByID.set(id,cell);padding.set(id,{x:cell.contentBounds.x,y:cell.contentBounds.y});Object.assign(emojiFonts,cell.text.emojiFonts);}
      buildInteractions();layoutTracks();
    }
    function applyProjection(table){
      if(!table||table.rows.length!==data.rows.length||table.columns.length!==data.columns.length||table.cells.length!==object.table.cells.length)throw Error('The draft changed table structure unexpectedly.');
      const next=new Map(table.cells.map(cell=>[key(cell.row,cell.column),cell]));if(object.table.cells.some(cell=>!next.has(key(cell.row,cell.column))))throw Error('The draft changed table structure unexpectedly.');
      for(const cell of object.table.cells){const id=key(cell.row,cell.column),projected=next.get(id),changed=JSON.stringify(cell)!==JSON.stringify(projected);Object.assign(cell,projected);padding.set(id,{x:cell.contentBounds.x,y:cell.contentBounds.y});Object.assign(emojiFonts,cell.text.emojiFonts);const node=cellNodes.get(id);Object.assign(node.style,{background:cell.fill==='none'?'transparent':cell.fill||'transparent',border:cell.borderColor==='none'?'none':(Array.isArray(table.borders)?0:cell.borderWidth)+'px solid '+cell.borderColor});if(changed)redrawCell(cell);}
      object.table.header=table.header;object.table.banded=table.banded;object.table.borders=Array.isArray(table.borders)?clone(table.borders):undefined;layoutTracks();
    }
    function projectDraft(next,accept){
      if(!options.renderDraft)return Promise.reject(Error('Table style previews are unavailable. Reopen the editor.'));
      projectionAbort=new AbortController();d.setAttribute('aria-busy','true');error.textContent='';status.textContent='Updating table style…';
      const job=(async()=>{try{const projected=await options.renderDraft(next,projectionAbort.signal);if(closed)return;validateProjection(projected,next);const structural=topology(data)!==topology(next),prepared=structural?await prepareStructure(projected,next):null;if(closed)return;const previous=data;data=next;try{if(prepared)installStructure(prepared);else applyProjection(projected);}catch(e){data=previous;throw e;}accept();refresh();}catch(e){if(!closed&&e.name!=='AbortError'){error.textContent=e.message;refresh();}throw e;}finally{projectionPending=null;projectionAbort=null;d.removeAttribute('aria-busy');if(!closed){refresh();options.onDirty?.();}}})();
      projectionPending=job;refresh();options.onDirty?.();return job;
    }
    function layoutTracks(){
      const sizes=(items,key,total)=>{const max=Math.max(...items.map(item=>item[key])),sum=items.reduce((n,item)=>n+item[key]/max,0);let used=0;return items.map((item,i)=>{const size=i===items.length-1?total-used:total*(item[key]/max)/sum;used+=size;return size;});};
      object.table.rows=sizes(data.rows,'height',object.bounds.height);object.table.columns=sizes(data.columns,'width',object.bounds.width);
      const offsets=values=>{let n=0;return values.map(value=>{const offset=n;n+=value;return offset;});},xs=offsets(object.table.columns),ys=offsets(object.table.rows);
      const rows=[...view.element.querySelectorAll('.keynope-scene-table tr')];rows.forEach((node,r)=>Object.assign(node.style,{top:ys[r]+'px',height:object.table.rows[r]+'px'}));
      for(const cell of object.table.cells){const id=key(cell.row,cell.column),p=padding.get(id),w=object.table.columns.slice(cell.column,cell.column+cell.colSpan).reduce((n,v)=>n+v,0),h=object.table.rows.slice(cell.row,cell.row+cell.rowSpan).reduce((n,v)=>n+v,0),changed=w!==cell.bounds.width||h!==cell.bounds.height;
        cell.bounds={x:xs[cell.column],y:ys[cell.row],width:w,height:h};cell.contentBounds={x:p.x,y:p.y,width:Math.max(1,w-2*p.x),height:Math.max(1,h-2*p.y)};
        const node=cellNodes.get(id),hit=views.get(id);Object.assign(node.style,{left:cell.bounds.x+'px',width:w+'px',height:h+'px'});Object.assign(hit.style,{left:cell.bounds.x+'px',top:cell.bounds.y+'px',width:w+'px',height:h+'px'});
        if(changed){let surface=surfaces.get(id);if(!surface)redrawCell(cell);else{Object.assign(surface.element.parentElement.style,{width:cell.contentBounds.width+'px',height:cell.contentBounds.height+'px'});surface.setWidth(cell.contentBounds.width);}}
      }
      KeynopeScene.paintTableBorders(view.element.querySelector('.keynope-scene-table'),object.table);
      for(const axis of ['rows','columns']){const minimum=canvasMinimums(axis);trackHeaders[axis].forEach((node,index)=>{const row=axis==='rows',values=object.table[axis],key=row?'height':'width';Object.assign(node.style,row?{left:'-44px',top:ys[index]+'px',width:'44px',height:values[index]+'px'}:{left:xs[index]+'px',top:'-44px',width:values[index]+'px',height:'44px'});const handle=node.querySelector('[role=separator]');if(handle){handle.setAttribute('aria-valuemin',String(minimum[index]));handle.setAttribute('aria-valuenow',String(data[axis][index][key]));handle.setAttribute('aria-valuemax',String(data[axis][index][key]+data[axis][index+1][key]-minimum[index+1]));}});}
      queueOverflow();
    }
    function refresh(preserveInput=false){selected.r=Math.max(0,Math.min(selected.r,data.rows.length-1));selected.c=Math.max(0,Math.min(selected.c,data.columns.length-1));end.r=Math.max(0,Math.min(end.r,data.rows.length-1));end.c=Math.max(0,Math.min(end.c,data.columns.length-1));if(data.cells[selected.r][selected.c].hidden){const anchor=object.table.cells.find(cell=>selected.r>=cell.row&&selected.r<cell.row+cell.rowSpan&&selected.c>=cell.column&&selected.c<cell.column+cell.colSpan);if(anchor)selected={r:anchor.row,c:anchor.column};}const set=new Set(chosen().map(cell=>key(cell.row,cell.column)));for(const [id,node]of views){const on=set.has(id);node.classList.toggle('selected',on);node.setAttribute('aria-selected',String(on));node.tabIndex=id===key(selected.r,selected.c)?0:-1;}
      if(undoButton)undoButton.disabled=!history.has(false)||!!projectionPending||!!refreshStyleTransfer.isBusy?.();if(redoButton)redoButton.disabled=!history.has(true)||!!projectionPending||!!refreshStyleTransfer.isBusy?.();
      for(const b of structureButtons)b.disabled=!!projectionPending||!!refreshStyleTransfer.isBusy?.();
      cellStyleHost.hidden=!!caret;
      const wordPainter=options.wordStyleTransfer&&(options.styleAvailable?.()??true)?KeynopeTextStyles.painterState(options.painterScope):null;
      wordTools.hidden=!caret&&!wordPainter;
      if(!caret){wordTools.replaceChildren();if(wordPainter){const hint=document.createElement('p');hint.setAttribute('role','status');hint.textContent='Word painter active. Double-click a cell, then select words.';wordTools.append(hint,button('Cancel word painter',()=>KeynopeTextStyles.clearPainter()));}}
      refreshStyleTransfer();
      refreshAppearance();
      for(const [property,b]of emphasisButtons){
        if(caret?.wordStyles){caret.wordStyles.refreshToggle(b,'text.'+property);continue;}
        b.disabled=!!pending||!!projectionPending||!!cancelTrack||!!refreshStyleTransfer.isBusy?.()||caret?.editor.isComposing()||options.styleAvailable?.()===false;
        const runs=caret?caretRuns():chosen().flatMap(cell=>modelFor(cell).runs),on=runs.some(run=>run[property]),off=runs.some(run=>!run[property]);
        b.setAttribute('aria-pressed',on?(off?'mixed':'true'):'false');
      }
      for(const [id,node]of views)node.style.pointerEvents=caret&&id===key(caret.cell.row,caret.cell.column)?'none':'';
      status.textContent=projectionPending?'Updating table style…':'Cell '+columnName(selected.c)+(selected.r+1)+(caret?' · Editing text':' · Shift-click selects a range; Enter edits text; Escape returns to the table.');
      for(const field of styleFields){
        const text=!!caret&&['fontId','fontSize','color'].includes(field.key),cells=chosen(),runs=text?caretRuns():[];
        if(field.key==='fontId')for(const option of field.input.options)if(option.value.startsWith('theme:'))option.disabled=!text||!caret.wordStyles;
        if(field.themeRole&&field.key==='color'){
          field.label.textContent=text?'Word colour role':'Text theme';field.input.setAttribute('aria-label',text?'Word colour role':'Canvas cell text theme');
          for(const option of field.input.options)if(option.value==='none')option.disabled=text;
        }
        if(text&&caret.wordStyles){showWordProperty(field,caret.wordStyles,caret.cell,preserveInput);continue;}
        if(!text&&showCellProperty(field,refreshStyleTransfer,preserveInput))continue;
        const authored=text?runs.map(run=>field.key==='color'?(run.colorToken?'theme:'+run.colorToken:run.color||null):run[field.key]??null):cells.flatMap(cell=>{const source=data.cells[cell.row][cell.column];return cellStyleValues(source.style,field.key,source.borderFragments);});
        const mixed=authored.some(value=>value!==authored[0]),inherited=!mixed&&authored[0]==null;
        const effective=text?runs.map(run=>field.key==='color'?tableColour(run.colorToken?'theme:'+run.colorToken:run.color,options)||field.effective(caret.cell):run[field.key]??field.effective(caret.cell)):cells.map(field.effective),effectiveMixed=effective.some(value=>value!==effective[0]);
        field.input.dataset.scope=text?'text':'cells';field.input.dataset.mixed=String(mixed);field.input.dataset.inherited=String(inherited);
        field.input.disabled=!!projectionPending||!!refreshStyleTransfer.isBusy?.();field.reset.disabled=field.input.disabled;for(const step of field.steppers)step.disabled=field.input.disabled;
        const value=field.themeRole?(mixed?'mixed':themeChoice(authored[0])):mixed?(field.choices?'mixed':''):inherited?(field.choices?'':effectiveMixed?'':effective[0]):authored[0];
        if(!preserveInput||document.activeElement!==field.input)field.input.value=value??'';
        field.input.title=(text?'Selected text · ':field.themeRole?'Cell colour role · ':'')+(mixed?'Mixed values':inherited?'Inherited'+(!effectiveMixed?' ('+effective[0]+')':''):'Explicit');
        if(field.colour){field.input.style.setProperty('--keynope-field-colour',mixed?'transparent':effective[0]);field.input.setAttribute('aria-description',field.input.title);}
        else if(field.input.tagName==='INPUT')field.input.placeholder=mixed?'Mixed':inherited?'Inherited':'';
      }
    }
    function stopCaret(){if(!caret)return;const current=caret,model=current.editor.value();
      if(current.wordStyles?.pending())throw Error('Wait for the word style operation before leaving this cell.');
      if(current.editor.isComposing())throw Error('Finish typing before leaving this cell.');
      const before=clone(data),cell=data.cells[current.cell.row][current.cell.column];Object.assign(emojiFonts,model.emojiFonts);
      if(JSON.stringify(model.runs)!==JSON.stringify(current.initialRuns)){cell.runs=retainRunExtensions(model.runs,cell.runs);delete cell.text;}
      current.wordStyles?.dispose({keepPainter:true});wordTools.replaceChildren();current.editor.dispose();current.host.remove();caret=null;redrawCell(current.cell);record(before);refresh();
    }
    function selectCell(cell,extend=false){if(pending||projectionPending||cancelTrack)return;try{stopCaret();if(!extend)selected={r:cell.row,c:cell.column};end={r:cell.row,c:cell.column};refresh();views.get(key(cell.row,cell.column)).focus({preventScroll:true});}catch(e){error.textContent=e.message;}}
    function editCell(cell){if(pending||projectionPending||cancelTrack||options.styleAvailable?.()===false)return;selectCell(cell);if(caret)return;
      if(cell.text?.readOnly){error.textContent='This cell uses a Preview & Print font. Choose another font to edit its text.';return;}
      refreshStyleTransfer.cancelPainter?.();
      const id=key(cell.row,cell.column),node=cellNodes.get(id),host=contentHost(cell);surfaces.get(id)?.dispose();surfaces.delete(id);node.querySelector('[data-cell-draft]')?.remove();for(const child of [...node.children])child.style.visibility='hidden';node.append(host);
      const resolveEmojiFonts=options.resolveEmojiFonts?async text=>{const fonts=await options.resolveEmojiFonts(text);Object.assign(emojiFonts,fonts);return fonts;}:null;
      const initial=modelFor(cell),editor=KeynopeTextLayout.createEditor(host,initial,{resolveEmojiFonts,onChange:()=>{options.onDirty?.();queueOverflow(cell);refresh();caret?.wordStyles?.refresh();},onError:e=>{error.textContent=e.message;}});
      editor.element.setAttribute('aria-label','Edit cell '+columnName(cell.column)+(cell.row+1));paint(editor,cell);editor.element.addEventListener('keydown',event=>{
        if(event.key==='Escape'){event.preventDefault();event.stopImmediatePropagation();try{stopCaret();views.get(id).focus();}catch(e){error.textContent=e.message;}}
        else if(event.key==='Tab'&&!event.metaKey&&!event.ctrlKey&&!event.altKey){event.preventDefault();event.stopImmediatePropagation();const anchors=object.table.cells,index=anchors.indexOf(cell);try{stopCaret();editCell(anchors[(index+(event.shiftKey?-1:1)+anchors.length)%anchors.length]);}catch(e){error.textContent=e.message;}}
      },true);
      const current={cell,editor,host,initialRuns:clone(editor.value().runs),range:{start:0,end:0}};caret=current;wordTools.replaceChildren();
      if(options.wordStyleTransfer)current.wordStyles=KeynopeTextStyles.attach(wordTools,d,editor,{
        styleClipboard:options.styleClipboard,painterScope:options.painterScope,propertyControls:false,onStateChange:accepted=>refresh(!accepted),
        available:()=>!closed&&!pending&&!projectionPending&&!cancelTrack&&caret===current&&(options.styleAvailable?.()??true),
        styleTransfer:(action,runs,range,request)=>options.wordStyleTransfer(action,runs,range,{row:cell.row,column:cell.column,rows:data.rows.length,columns:data.columns.length,header:!!data.style?.header&&cell.row===0,baseSize:editor.value().size},request)
      });
      refresh();editor.element.focus();editor.select(plain(data.cells[cell.row][cell.column]).length);
    }
    function mutate(action,project=false){if(pending||projectionPending||cancelTrack)return;try{stopCaret();const before=clone(data),oldSelection={...selected},oldEnd={...end};try{action();if((project||needsProjection(before,data))&&JSON.stringify(before)!==JSON.stringify(data)){const next=data,nextSelection={...selected},nextEnd={...end};data=before;selected=oldSelection;end=oldEnd;projectDraft(next,()=>{selected=nextSelection;end=nextEnd;record(before);}).catch(()=>{});}else{record(before);redrawChanges(before);refresh();error.textContent='';}}catch(e){data=before;selected=oldSelection;end=oldEnd;throw e;}}catch(e){error.textContent=e.message;}}
    function restore(redo=false){if(pending||projectionPending||cancelTrack)return;try{stopCaret();const next=history.peek(redo);if(!next)return;const before=clone(data);if(needsProjection(before,next)){projectDraft(next,()=>history.advance(before,redo)).catch(()=>{});}else{history.advance(before,redo);data=next;redrawChanges(before);refresh();options.onDirty?.();}}catch(e){error.textContent=e.message;}}
    const dirty=()=>!!projectionPending||JSON.stringify(data)!==JSON.stringify(source)||!!caret&&JSON.stringify(caret.editor.value().runs)!==JSON.stringify(caret.initialRuns);
    function fit(){if(closed)return;const scene=options.canvasScene,r=scene.getBoundingClientRect(),scale=r.width/parseFloat(scene.style.width);if(!(scale>0))return;
      Object.assign(viewport.style,{left:r.left+object.bounds.x*scale+'px',top:r.top+object.bounds.y*scale+'px',transform:`scale(${scale})`});
      const panel=options.inspectorHost?.getBoundingClientRect();if(panel&&panel.width>=240&&panel.height>=300&&panel.left>=0&&panel.right<=innerWidth)Object.assign(tools.style,{left:panel.left+'px',top:panel.top+'px',width:panel.width+'px',height:panel.height+'px'});
      else Object.assign(tools.style,{left:'auto',top:'auto',right:'8px',bottom:'8px',width:'min(560px,calc(100vw - 16px))',height:'auto',maxHeight:'40vh'});
    }
    const wordPainterChanged=()=>{if(!closed)refresh();};document.addEventListener('keynope-word-painter-change',wordPainterChanged);document.addEventListener('keynope-style-scope-change',wordPainterChanged);
    function close(keepPainter=false){if(closed||pending)return;cancelTrack?.();closed=true;document.removeEventListener('keynope-word-painter-change',wordPainterChanged);document.removeEventListener('keynope-style-scope-change',wordPainterChanged);document.removeEventListener('selectionchange',selectionChanged);cancelAnimationFrame(overflowFrame);clearTimeout(overflowTimer);projectionAbort?.abort();stagedView?.dispose();caret?.wordStyles?.dispose({keepPainter});if(!keepPainter)globalThis.KeynopeTextStyles?.clearPainter();caret?.editor.dispose();for(const surface of surfaces.values())surface.dispose();view.dispose();observer.disconnect();window.removeEventListener('resize',fit);window.removeEventListener('scroll',fit,true);if(sourceNode?.isConnected)sourceNode.style.visibility=sourceVisibility;canvasSession=null;options.onClose?.();closeDialog();}
    async function finish(){if(closed)return;if(pending)return pending;await caret?.wordStyles?.pending();await refreshStyleTransfer.pending?.();if(projectionPending)await projectionPending;if(closed)return;cancelTrack?.();stopCaret();if(!dirty()){close(true);return;}
      // Assign the pending handle before invoking the committer. A synchronous
      // stale-scope rejection must not overwrite finally's cleanup with an
      // already-rejected Promise (which would permanently block Cancel).
      d.inert=true;pending=Promise.resolve().then(async()=>{try{await commit(clone(data));pending=null;close(true);}catch(e){error.textContent=e.message;throw e;}finally{pending=null;d.inert=false;}});return pending;
    }
    canvasSession={finish,isDirty:dirty};
    const title=document.createElement('h2');title.textContent='Table cells';tools.append(title);
    tools.append(cellStyleHost);
    refreshStyleTransfer=cellStyleTransferTools(cellStyleHost,options,()=>data,rect,next=>{mutate(()=>{data=next;},true);return projectionPending;},{before:stopCaret,available:()=>!closed&&!pending&&!projectionPending&&!cancelTrack&&(options.styleAvailable?.()??true)});
    const actions=document.createElement('div');actions.className='keynope-table-toolbar';
    undoButton=button('Undo cell change',()=>restore());redoButton=button('Redo cell change',()=>restore(true));actions.append(undoButton,redoButton,button('Done',()=>finish().catch(()=>{})),button('Cancel',()=>close()));
    if(options.renderDraft){const structure=document.createElement('div');structure.className='keynope-table-toolbar';structure.setAttribute('aria-label','Table structure');
      for(const axis of ['row','column']){structureButtons.push(button('Add '+axis,()=>mutate(()=>track(data,axis,(axis==='row'?selected.r:selected.c)+1,false))),button('Remove '+axis,()=>mutate(()=>track(data,axis,axis==='row'?selected.r:selected.c,true))));}
      structureButtons.push(button('Merge cells',()=>mutate(()=>{const b=rect();mergeCells(data,b);selected={r:b.r0,c:b.c0};end={...selected};})),button('Split cells',()=>mutate(()=>splitCells(data,rect()))));structure.append(...structureButtons);tools.append(structure);
    }
    for(const [label,property]of [['B','bold'],['I','italic'],['U','underline']]){const b=button(label,()=>{
      if(caret){if(caret.wordStyles)return caret.wordStyles.toggleProperty('text.'+property);const on=caretRuns().every(run=>run[property]);formatCaret({[property]:!on,...(property==='bold'?{boldOff:on&&(caret.cell.header||caretRuns().some(run=>run.boldOff))}: {})});}
      else mutate(()=>{const cells=chosen(),on=cells.every(c=>{const source=data.cells[c.row][c.column];return (source.runs?.length?source.runs:[{text:source.text||''}]).every(r=>r[property]||property==='bold'&&c.header&&!r.boldOff);});for(const selectedCell of cells){const cell=data.cells[selectedCell.row][selectedCell.column];cell.runs=(cell.runs?.length?cell.runs:[{text:cell.text||''}]).map(r=>({...r,[property]:!on,...(property==='bold'?{boldOff:on&&(selectedCell.header||r.boldOff)}:{})}));delete cell.text;}});
    });b.setAttribute('aria-label',property[0].toUpperCase()+property.slice(1)+' cells');b.onpointerdown=e=>{rememberCaretRange();e.preventDefault();};emphasisButtons.set(property,b);actions.append(b);}
    function selectedText(){const b=rect(),rows=[];for(let r=b.r0;r<=b.r1;r++){const row=[];for(let c=b.c0;c<=b.c1;c++){const value=plain(data.cells[r][c]);row.push(/[\t\n\r"]/.test(value)?'"'+value.replaceAll('"','""')+'"':value);}rows.push(row.join('\t'));}return rows.join('\n');}
    const clearCells=()=>{for(const c of chosen()){data.cells[c.row][c.column].text='';delete data.cells[c.row][c.column].runs;}};
    const copy=button('Copy cells',async()=>{try{stopCaret();const value=selectedText();await(options.copyText?options.copyText(value):navigator.clipboard.writeText(value));status.textContent='Cells copied.';}catch(e){error.textContent=e.message;}});actions.append(copy);
    for(const name of ['copy','cut'])d.addEventListener(name,event=>{if(caret||pending||!event.target.closest('.keynope-table-canvas-hits'))return;event.preventDefault();event.stopPropagation();event.clipboardData.setData('text/plain',selectedText());if(name==='cut')mutate(clearCells);});
    if(options.onEditTable)actions.append(button('Table formatting…',()=>{const selection={start:{...selected},end:{...end}};finish().then(()=>{if(!active)options.onEditTable(selection);}).catch(()=>{});}));
    if(options.onFitTable)actions.append(button('Fit table to content',()=>finish().then(()=>{if(!active)options.onFitTable();}).catch(()=>{})));tools.append(actions,status,wordTools);
    if(options.renderDraft){const style=document.createElement('div');style.className='keynope-table-canvas-style';tools.append(style);
      function property(label,key,type,effective,choices,targetHost=style){const l=document.createElement('label'),heading=document.createElement('span');heading.textContent=label;l.append(heading);const colour=type==='color'&&typeof options.pickColor==='function',input=document.createElement(colour?'button':choices?'select':'input');input.setAttribute('aria-label','Canvas cell '+label.toLowerCase());
        if(colour){input.type='button';input.className='keynope-colour-field-button';input.innerHTML='<span class="keynope-colour-field-swatch"></span><span>Pick color</span>';input.onclick=()=>{if(projectionPending)return;try{rememberCaretRange();if(caret&&key!=='color')stopCaret();const range={...rect()},text=caret&&key==='color',run=text?caretRuns()[0]:null,value=text?(run?.colorToken?'theme:'+run.colorToken:run?.color||effective(caret.cell)):(input.value||effective(chosen()[0]));options.pickColor(input,tableColour(value,options)||'#ffffff',value=>{if(!text&&refreshStyleTransfer.setProperty){refreshStyleTransfer.setProperty(key,value,false,range);return;}apply(value);});}catch(e){error.textContent=e.message;}};}
        else if(choices){for(const[value,text]of choices){const option=document.createElement('option');option.value=value;option.textContent=text;input.append(option);}if(!choices.some(([value])=>value==='mixed')){const option=document.createElement('option');option.value='mixed';option.textContent='Mixed';option.disabled=true;input.append(option);}}else{input.type=type;if(type==='number'){[input.min,input.max]=tableMetricLimits(key);input.step='any';}}
        if(choices)globalThis.KeynopeFonts?.enhance(input);
        const apply=value=>{if(type==='number'&&value!==''&&!input.checkValidity()){input.reportValidity();refresh();return;}if(caret&&['fontId','fontSize','color'].includes(key)){
          if(caret.wordStyles){const property=key==='fontId'?'text.font':key==='fontSize'?'text.size':type==='theme'?'paint.colorToken':'paint.color';caret.wordStyles.setProperty(property,value===''?'':type==='number'?Number(value):type==='theme'?String(value).replace(/^theme:/,''):value);return;}
          if(type==='theme'){if(value&&(!value.startsWith('theme:')||!themeRoles.some(([role])=>'theme:'+role===value)))return;formatCaret({color:value?tableColour(value,options):null,colorToken:value?value.slice(6):null});}
          else formatCaret({[key]:value===''?null:type==='number'?Number(value):value});return;
        }if(refreshStyleTransfer.setProperty){refreshStyleTransfer.setProperty(key,value===''?'':type==='number'?Number(value):value,type==='theme');return;}mutate(()=>{for(const cell of chosen())setCellStyle(data.cells[cell.row][cell.column],key,value===''?null:type==='number'?Number(value):value);},true);};input.onchange=()=>apply(input.value);input.addEventListener('pointerdown',rememberCaretRange);input.addEventListener('focus',rememberCaretRange);
        const reset=button('↶',()=>apply(''));reset.onpointerdown=event=>{rememberCaretRange();event.preventDefault();};reset.setAttribute('aria-label','Inherit canvas cell '+label.toLowerCase());const row=document.createElement('div');row.append(input);
        const steppers=[];if(type==='number'){const stepper=document.createElement('div');stepper.className='keynope-table-canvas-number-stepper';for(const [symbol,delta,verb]of [['+',1,'Increase'],['−',-1,'Decrease']]){const step=button(symbol,()=>{if(caret&&key==='fontSize'){if(caret.wordStyles){caret.wordStyles.stepProperty('text.size',delta);return;}formatCaret(run=>({fontSize:Math.max(1,Math.min(1024,(run.fontSize||effective(caret.cell))+delta))}));return;}if(refreshStyleTransfer.stepProperty){refreshStyleTransfer.stepProperty(key,delta*tableMetricStep(key));return;}mutate(()=>{for(const cell of chosen()){const value=Math.max(Number(input.min),Math.min(Number(input.max),Number(effective(cell))+delta*tableMetricStep(key)));setCellStyle(data.cells[cell.row][cell.column],key,value);}},true);});step.onpointerdown=event=>{rememberCaretRange();event.preventDefault();};step.setAttribute('aria-label',verb+' canvas cell '+label.toLowerCase());steppers.push(step);stepper.append(step);}row.append(stepper);}row.append(reset);l.append(row);targetHost.append(l);if(type==='theme')for(const option of input.options)if(['custom','mixed'].includes(option.value))option.disabled=true;styleFields.push({input,reset,steppers,key,effective,choices,colour,themeRole:type==='theme',label:heading});
      }
      property('Font','fontId','text',cell=>cell.text.fontId,[['','Inherit'],['c64','Keynope C64'],['sans','Sans'],['mono','Mono'],['theme:heading','Theme Heading'],['theme:body','Theme Body'],['theme:mono','Theme Mono']]);property('Font size','fontSize','number',cell=>cell.text.size);property('Font width','fontWidth','number',cell=>cell.text.widthScale*100/(cell.text.fontId==='c64'?.4167:1));property('Line spacing','lineHeight','number',cell=>cell.text.lineHeight);property('Text colour','color','color',cell=>cell.paint.color);property('Text theme','color','theme',cell=>cell.paint.color,themeChoices);property('Fill','fill','color',cell=>cell.fill||'none');property('Fill theme','fill','theme',cell=>cell.fill||'none',themeChoices);property('Alignment','align','text',cell=>({start:'left',end:'right'}[cell.text.align]||cell.text.align),[['','Inherit'],['left','Left'],['center','Centre'],['right','Right'],['justify','Justify']]);property('Vertical','vertical','text',cell=>cell.text.vertical,[['','Inherit'],['top','Top'],['middle','Middle'],['bottom','Bottom']]);property('Padding','padding','number',cell=>cell.contentBounds.x);property('Border width','borderWidth','number',cell=>cell.borderWidth);property('Border colour','borderColor','color',cell=>cell.borderColor);property('Border theme','borderColor','theme',cell=>cell.borderColor,themeChoices);
      const sides=document.createElement('details');sides.className='keynope-table-border-sides';const summary=document.createElement('summary');summary.textContent='Cell border sides';sides.append(summary);tools.append(sides);
      for(const side of ['left','right','top','bottom']){const group=document.createElement('fieldset'),legend=document.createElement('legend');legend.textContent=side[0].toUpperCase()+side.slice(1);group.className='keynope-table-canvas-style';group.append(legend);sides.append(group);const key='borders.'+side+'.';property(side+' border colour',key+'color','color',cell=>cell.borderColor,undefined,group);property(side+' border theme',key+'color','theme',cell=>cell.borderColor,themeChoices,group);property(side+' border width',key+'width','number',cell=>cell.borderWidth,undefined,group);}
      refreshAppearance=tableAppearanceTools(tools,options,refreshStyleTransfer);
    }refreshStyleTransfer.onCapabilities=()=>refresh();tools.append(overflow,error);
    d.addEventListener('cancel',event=>{event.preventDefault();if(pending)return;if(caret){try{stopCaret();views.get(key(selected.r,selected.c)).focus();}catch(e){error.textContent=e.message;}}else finish().catch(()=>{});});
    d.addEventListener('keydown',event=>{if(caret||pending||projectionPending||event.target.closest('.keynope-table-canvas-tools,.keynope-table-canvas-headers'))return;event.stopPropagation();const cell=object.table.cells.find(c=>c.row===selected.r&&c.column===selected.c);if(!cell)return;
      if((event.metaKey||event.ctrlKey)&&['z','y'].includes(event.key.toLowerCase())){event.preventDefault();restore(event.key.toLowerCase()==='y'||event.shiftKey);}
      else if(event.key==='Enter'){event.preventDefault();editCell(cell);}
      else if(event.key==='Tab'){event.preventDefault();const i=object.table.cells.indexOf(cell);selectCell(object.table.cells[(i+(event.shiftKey?-1:1)+object.table.cells.length)%object.table.cells.length]);}
      else if(['ArrowLeft','ArrowRight','ArrowUp','ArrowDown'].includes(event.key)&&!event.metaKey&&!event.ctrlKey&&!event.altKey){event.preventDefault();const base=event.shiftKey?end:selected,current=object.table.cells.find(c=>c.row===base.r&&c.column===base.c),r=base.r+(event.key==='ArrowDown'?current.rowSpan:event.key==='ArrowUp'?-1:0),c=base.c+(event.key==='ArrowRight'?current.colSpan:event.key==='ArrowLeft'?-1:0);const next=object.table.cells.find(cell=>r>=cell.row&&r<cell.row+cell.rowSpan&&c>=cell.column&&c<cell.column+cell.colSpan);if(next)selectCell(next,event.shiftKey);}
      else if(event.key==='Delete'||event.key==='Backspace'){event.preventDefault();mutate(clearCells);}
    });
    d.addEventListener('paste',event=>{const value=event.clipboardData?.getData('text/plain')||'';if(caret){if(!event.target.closest('[contenteditable=true]')||!/[\t\n]/.test(value))return;}else if(!event.target.closest('.keynope-table-canvas-hits'))return;event.preventDefault();event.stopImmediatePropagation();mutate(()=>{const matrix=parseDelimited(value,value.includes('\t')?'\t':',');end=pasteData(data,selected.r,selected.c,matrix);});},true);
    d.addEventListener('pointerdown',event=>{if(event.target!==d)return;event.preventDefault();finish().then(()=>options.onOutsideCommit?.({x:event.clientX,y:event.clientY})).catch(()=>{});});
    function canvasMinimums(axis){
      const row=axis==='rows',floors=data[axis].map(()=>row?24:40);
      for(const cell of object.table.cells){const authored=data.cells[cell.row][cell.column],size=Math.max(cell.text.size,...(authored.runs||[]).map(run=>run.fontSize||cell.text.size)),span=row?cell.rowSpan:cell.colSpan,start=row?cell.row:cell.column;
        // Reserve one line vertically and a small readable fragment (three
        // ems) horizontally, plus real cell padding. This is not content Fit:
        // long prose may wrap/overflow and its font is never silently shrunk.
        const required=(row?size*cell.text.lineHeight+2*cell.contentBounds.y:3*size*cell.text.widthScale+2*cell.contentBounds.x)/span;
        for(let i=start;i<start+span;i++)floors[i]=Math.max(floors[i],required);
      }
      return trackMinimums(data,axis,object.table[axis],floors);
    }
    function buildInteractions(){views.clear();cellNodes.clear();hits.replaceChildren();headers.replaceChildren();trackHeaders.rows=[];trackHeaders.columns=[];
    for(const cell of object.table.cells){const id=key(cell.row,cell.column),node=view.element.querySelector('[data-table-cell="'+id+'"]'),hit=document.createElement('div');cellNodes.set(id,node);
      hit.className='keynope-table-canvas-cell';hit.setAttribute('role','gridcell');hit.setAttribute('aria-label','Cell '+columnName(cell.column)+(cell.row+1));Object.assign(hit.style,{left:cell.bounds.x+'px',top:cell.bounds.y+'px',width:cell.bounds.width+'px',height:cell.bounds.height+'px'});
      hit.onpointerdown=event=>{if(event.button!==0)return;event.preventDefault();selectCell(cell,event.shiftKey);refreshStyleTransfer.paint?.();};hit.ondblclick=event=>{event.preventDefault();if(!refreshStyleTransfer.isPainting?.())editCell(cell);};views.set(id,hit);hits.append(hit);
    }
    hits.setAttribute('role','grid');hits.setAttribute('aria-label','Table cells');
    for(const axis of ['columns','rows'])data[axis].forEach((_,index)=>{const row=axis==='rows',key=row?'height':'width',node=document.createElement('div'),select=button(row?String(index+1):columnName(index),()=>{if(pending||projectionPending||cancelTrack)return;try{stopCaret();selected=row?{r:index,c:0}:{r:0,c:index};end=row?{r:index,c:data.columns.length-1}:{r:data.rows.length-1,c:index};refresh();}catch(e){error.textContent=e.message;}});node.className='keynope-table-canvas-track';select.setAttribute('aria-label',row?'Select row '+(index+1):'Select column '+columnName(index));node.append(select);headers.append(node);trackHeaders[axis].push(node);
      if(index===data[axis].length-1)return;const handle=document.createElement('span');handle.className='keynope-table-canvas-separator '+(row?'row':'column');handle.tabIndex=0;handle.setAttribute('role','separator');handle.setAttribute('aria-orientation',row?'horizontal':'vertical');handle.setAttribute('aria-label','Resize canvas '+(row?'row '+(index+1):'column '+columnName(index)));handle.setAttribute('aria-valuemin','0');node.append(handle);
      handle.title='Drag to resize adjacent tracks. Option/Alt scales all tracks on each side.';
      handle.onkeydown=event=>{const negative=row?'ArrowUp':'ArrowLeft',positive=row?'ArrowDown':'ArrowRight';if(event.key!==negative&&event.key!==positive)return;event.preventDefault();event.stopPropagation();const pair=data[axis][index][key]+data[axis][index+1][key],minimum=canvasMinimums(axis);mutate(()=>event.altKey?resizeProportionalTracks(data,axis,index,pair*.025*(event.key===negative?-1:1),minimum):resizeTracks(data,axis,index,pair*.025*(event.key===negative?-1:1),minimum.slice(index,index+2)));};
      handle.onpointerdown=event=>{if(event.button!==0||pending||projectionPending||cancelTrack)return;event.preventDefault();event.stopPropagation();try{stopCaret();}catch(e){error.textContent=e.message;return;}const before=clone(data),total=data[axis].reduce((n,item)=>n+item[key],0),pixels=object.table[axis].reduce((n,v)=>n+v,0),minimum=canvasMinimums(axis),proportional=event.altKey,scene=options.canvasScene.getBoundingClientRect(),scale=scene.width/parseFloat(options.canvasScene.style.width),angle=-(object.rotation||0)*Math.PI/180;if(!Number.isFinite(total)||!(scale>0)||!(pixels>0)){error.textContent='Track weights are too large to resize. Set smaller weights in Table formatting.';return;}handle.setPointerCapture(event.pointerId);
        const move=e=>{const dx=(e.clientX-event.clientX)/scale,dy=(e.clientY-event.clientY)/scale,delta=row?dx*Math.sin(angle)+dy*Math.cos(angle):dx*Math.cos(angle)-dy*Math.sin(angle),weightDelta=total*Math.max(-1,Math.min(1,delta/pixels));data[axis]=clone(before[axis]);if(proportional)resizeProportionalTracks(data,axis,index,weightDelta,minimum);else resizeTracks(data,axis,index,weightDelta,minimum.slice(index,index+2));layoutTracks();};
        const cleanup=()=>{handle.removeEventListener('pointermove',move);handle.removeEventListener('pointerup',finishTrack);handle.removeEventListener('pointercancel',cancel);handle.removeEventListener('lostpointercapture',cancel);window.removeEventListener('blur',cancel);cancelTrack=null;if(handle.hasPointerCapture(event.pointerId))handle.releasePointerCapture(event.pointerId);};
        const finishTrack=()=>{cleanup();record(before);refresh();},cancel=()=>{data=before;cleanup();layoutTracks();refresh();options.onDirty?.();};cancelTrack=cancel;handle.addEventListener('pointermove',move);handle.addEventListener('pointerup',finishTrack);handle.addEventListener('pointercancel',cancel);handle.addEventListener('lostpointercapture',cancel);window.addEventListener('blur',cancel);
      };
    });
    }
    buildInteractions();
    layoutTracks();
    const observer=new ResizeObserver(fit);observer.observe(options.canvasScene);if(options.inspectorHost)observer.observe(options.inspectorHost);window.addEventListener('resize',fit);window.addEventListener('scroll',fit,true);
    // Replace the generic dialog Escape callback; this surface owns nested
    // caret -> cell range -> whole-object state rather than discarding on Esc.
    d.addEventListener('keydown',event=>{if(event.key==='Escape'){if(caret?.wordStyles&&(KeynopeTextStyles.painterState()||caret.wordStyles.pending()))return;event.preventDefault();event.stopImmediatePropagation();if(pending)return;if(cancelTrack){cancelTrack();return;}if(caret){try{stopCaret();views.get(key(selected.r,selected.c)).focus();}catch(e){error.textContent=e.message;}}else finish().catch(()=>{});}},true);
    try{d.showModal();fit();await view.ready;if(closed)return;queueOverflow();
    let first=object.table.cells[0];if(options.point){const r=options.canvasScene.getBoundingClientRect(),scale=r.width/parseFloat(options.canvasScene.style.width),x=(options.point.x-r.left)/scale-object.bounds.x-object.bounds.width/2,y=(options.point.y-r.top)/scale-object.bounds.y-object.bounds.height/2,a=-(object.rotation||0)*Math.PI/180,px=x*Math.cos(a)-y*Math.sin(a)+object.bounds.width/2,py=x*Math.sin(a)+y*Math.cos(a)+object.bounds.height/2;first=object.table.cells.find(c=>px>=c.bounds.x&&px<c.bounds.x+c.bounds.width&&py>=c.bounds.y&&py<c.bounds.y+c.bounds.height)||first;}
    selectCell(first);if(options.caret!==false)editCell(first);
    }catch(e){close();throw e;}
  }
  return {insert,edit,canvas,parseDelimited,track,mergeCells,splitCells,resizeTracks,resizeProportionalTracks,setCellStyle,cellStyleValues,commitPending:()=>active?.keynopePendingCommit?.()||editSession?.finish()||canvasSession?.finish(),isDirty:()=>!!(editSession?.isDirty()||canvasSession?.isDirty())};
})();
