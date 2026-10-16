// One font identity from authoring through shaping and audience/export views.
globalThis.KeynopeFonts=(()=>{
 const custom=/^font-[a-f0-9]{64}$/,loaded=new Map(),faces=new Map(),fetching=new Map(),missing=new Map(),sources=new Map();
 let deckFonts={},catalogue=null,expanded=false,active=null,previewSerial=0;
 const valid=id=>['c64','sans','mono'].includes(id)||custom.test(id||'');
 const family=id=>custom.test(id||'')?'KeynopeFont_'+id.slice(5)+', KeynopeModern, sans-serif':id==='c64'?'KeynopeC64, monospace':id==='mono'?'KeynopeModernMono, monospace':'KeynopeModern, sans-serif';
 const name=id=>deckFonts[id]?.name||({c64:'Keynope C64',sans:'Go — proportional',mono:'Go Mono — monospaced'})[id]||'Missing font';
 function showMissing(){if(!globalThis.document?.body)return;let notice=document.querySelector('.keynope-missing-typeface');if(!missing.size){notice?.remove();return;}if(!notice){notice=document.createElement('div');notice.className='keynope-missing-typeface';notice.setAttribute('role','alert');document.body.append(notice);}notice.textContent='Unavailable fonts: '+[...missing.values()].join(', ')+'. Substitute fonts are being displayed.';}
 function setDeck(values){deckFonts=values||{};for(const [id,face]of faces)if(!deckFonts[id]){document.fonts.delete(face);faces.delete(id);loaded.delete(id);missing.delete(id);}showMissing();}
 function ensure(id){
  if(!custom.test(id||''))return;
  if(loaded.has(id))return loaded.get(id);
  if(deckFonts[id]){
   if(!fetching.has(id))fetching.set(id,fetch('/api/editor/typeface?id='+id).then(async response=>{if(!response.ok)throw Error('Font unavailable');const font=await response.json();if(deckFonts[id])await install({[id]:font});}).catch(()=>{if(deckFonts[id]){missing.set(id,name(id));showMissing();}}).finally(()=>fetching.delete(id)));
   return fetching.get(id);
  }
  missing.set(id,name(id));showMissing();
 }
 const readyFor=model=>Promise.all([model.fontId,...(model.runs||[]).map(run=>run.fontRole?model.fontRoles?.[run.fontRole]||run.fontId:run.fontId)].map(ensure).filter(Boolean));
 function install(values={}){
  const tasks=[];
  for(const [id,font]of Object.entries(values)){
   if(!custom.test(id))continue;
   if(loaded.has(id)){tasks.push(loaded.get(id));continue;}
   const source=font.localOnly?'local('+JSON.stringify(font.postScriptName)+')':'url(data:font/otf;base64,'+font.data+')';
   const face=new FontFace('KeynopeFont_'+id.slice(5),source);faces.set(id,face);document.fonts.add(face);
   const task=face.load().then(()=>{if(faces.get(id)!==face)return false;missing.delete(id);showMissing();return true;}).catch(()=>{document.fonts.delete(face);if(faces.get(id)===face){missing.set(id,font.name||id);showMissing();globalThis.dispatchEvent?.(new CustomEvent('keynope-typeface-missing',{detail:{id,name:font.name||id}}));}return false;});
   loaded.set(id,task);tasks.push(task);
  }
  return Promise.all(tasks);
 }
 function choices(base){return [...base,...Object.entries(deckFonts).sort((a,b)=>a[1].name.localeCompare(b[1].name)).map(([id,font])=>[id,font.name+(font.localOnly?' · Local only':'')])];}
 const nativeFonts=()=>globalThis.webkit?.messageHandlers?.keynopeFonts;
 const base64=bytes=>{let encoded='';for(let i=0;i<bytes.length;i+=8192)encoded+=String.fromCharCode(...bytes.subarray(i,i+8192));return btoa(encoded);};
 async function fontSource(entry){
  if(entry.id){const response=await fetch('/api/editor/typeface?id='+entry.id);if(!response.ok)throw Error('Font unavailable');return response.json();}
  if(entry.file||entry.blob){const blob=entry.file||await entry.blob();if(blob.size>16*1024*1024)throw Error('Font files must be no larger than 16 MB.');return {name:entry.name,postScriptName:entry.postScriptName,data:base64(new Uint8Array(await blob.arrayBuffer()))};}
  if(!sources.has(entry.postScriptName)){
   const request=nativeFonts().postMessage({action:'read',postScriptName:entry.postScriptName});sources.set(entry.postScriptName,request);
   request.catch(()=>sources.delete(entry.postScriptName));if(sources.size>12)sources.delete(sources.keys().next().value);
  }
  return sources.get(entry.postScriptName);
 }
 function enhance(select){
  if(!select||![...select.options].some(o=>o.value==='c64')||![...select.options].some(o=>o.value==='sans'))return;
  for(const [id,font]of Object.entries(deckFonts))if(![...select.options].some(o=>o.value===id))select.add(new Option(font.name+(font.localOnly?' · Local only':''),id));
  if(!globalThis.keynopeImportTypeface)return;
  if(!select.querySelector('option[value="__more_fonts__"]'))select.add(new Option('More fonts…','__more_fonts__'));
  if(!select.querySelector('option[value="__load_font__"]'))select.add(new Option('Load from disk…','__load_font__'));
  if(select.keynopeFontPicker)return;select.keynopeFontPicker=true;
  queueMicrotask(()=>{if(!select.value.startsWith('__'))select.keynopePreviousFont=select.value;});
  select.addEventListener('focus',()=>{if(!select.value.startsWith('__'))select.keynopePreviousFont=select.value;});
  select.addEventListener('pointerdown',event=>{if(event.button!==0)return;event.preventDefault();select.focus();if(active?.select===select)active.close();else open(select);});
  select.addEventListener('keydown',event=>{if(!['ArrowDown','ArrowUp','Enter',' '].includes(event.key))return;event.preventDefault();event.stopImmediatePropagation();open(select);});
  select.addEventListener('change',event=>{
   if(!select.value.startsWith('__')){select.keynopePreviousFont=select.value;return;}
   const command=select.value;event.preventDefault();event.stopImmediatePropagation();select.value=select.keynopePreviousFont||'';
   open(select,command);
  },true);
 }
 function open(select,command=''){
  active?.close(false);
  const owner=globalThis.keynopeFontOwner?.(),popup=document.createElement('div');popup.className='keynope-font-dropdown';
  const search=document.createElement('input');search.type='search';search.placeholder='Search fonts and styles';search.setAttribute('aria-label','Search fonts');
  const status=document.createElement('p');status.className='keynope-font-status';status.setAttribute('role','status');
  const list=document.createElement('div');list.className='keynope-font-list';list.setAttribute('role','listbox');list.setAttribute('aria-label','Available fonts');list.id='keynope-font-options-'+(++previewSerial);
  const previewName=document.createElement('div');previewName.className='keynope-font-preview-name';
  const preview=document.createElement('div');preview.className='keynope-font-preview';preview.textContent='The quick brown fox\n0123456789';preview.setAttribute('aria-label','Font preview');
  const error=document.createElement('p');error.setAttribute('role','alert');error.hidden=true;
  const more=document.createElement('button');more.type='button';more.textContent='More fonts…';
  const disk=document.createElement('button');disk.type='button';disk.textContent='Load from disk…';
  const file=document.createElement('input');file.type='file';file.accept='.ttf,.otf,font/ttf,font/otf';file.hidden=true;
  const actions=document.createElement('div');actions.className='keynope-font-actions';actions.append(more,disk);
  popup.append(search,status,list,previewName,preview,error,actions,file);
  (select.closest('dialog[open]')||document.body).append(popup);
  select.setAttribute('aria-expanded','true');select.setAttribute('aria-controls',list.id);
  let entries=[],closed=false,busy=false,token=0,face=null,hoverTimer;
  const current=()=>!closed&&select.isConnected&&(!globalThis.keynopeFontOwner||globalThis.keynopeFontOwner()===owner);
  const fail=problem=>{if(!closed){error.hidden=false;error.textContent=problem.message||String(problem);}};
  const position=()=>{const r=select.getBoundingClientRect(),width=Math.min(500,innerWidth-24);popup.style.width=width+'px';popup.style.left=Math.max(12,Math.min(innerWidth-width-12,r.left))+'px';const height=Math.min(popup.scrollHeight,innerHeight-24);popup.style.top=Math.max(12,r.bottom+height+8<=innerHeight?r.bottom+5:r.top-height-5)+'px';popup.style.maxHeight=(innerHeight-24)+'px';};
  const close=(focus=true)=>{if(closed)return;closed=true;token++;clearTimeout(hoverTimer);observer.disconnect();popup.remove();if(face)document.fonts.delete(face);document.removeEventListener('pointerdown',outside,true);document.removeEventListener('focusin',outsideFocus,true);globalThis.removeEventListener('resize',position);globalThis.removeEventListener('scroll',position,true);select.setAttribute('aria-expanded','false');select.removeAttribute('aria-controls');if(active?.select===select)active=null;if(focus&&select.isConnected)select.focus({preventScroll:true});};
  const outside=event=>{if(!popup.contains(event.target)&&event.target!==select)close(false);};
  const outsideFocus=event=>{if(!popup.contains(event.target)&&event.target!==select)close(false);};
  const observer=new MutationObserver(()=>{if(!select.isConnected)close(false);});observer.observe(document.body,{subtree:true,childList:true});
  active={select,close};document.addEventListener('pointerdown',outside,true);document.addEventListener('focusin',outsideFocus,true);globalThis.addEventListener('resize',position);globalThis.addEventListener('scroll',position,true);
  async function paintPreview(entry){
   const serial=++token;previewName.textContent=entry.name;
   try{
    if(entry.id!==undefined&&!custom.test(entry.id)){preview.style.setProperty('font-family',family(entry.id),'important');return;}
    const value=entry.id?(await ensure(entry.id),null):await fontSource(entry);
    if(!current()||serial!==token)return;
    if(entry.id){preview.style.setProperty('font-family',family(entry.id),'important');return;}
    const alias='KeynopeFontPreview_'+(++previewSerial),source=value.localOnly?'local('+JSON.stringify(value.postScriptName)+')':Uint8Array.from(atob(value.data),c=>c.charCodeAt(0));
    const candidate=new FontFace(alias,source);await candidate.load();
    if(!current()||serial!==token)return;
    if(face)document.fonts.delete(face);face=candidate;document.fonts.add(face);preview.style.setProperty('font-family',alias,'important');
   }catch(problem){if(current()&&serial===token){preview.style.setProperty('font-family','system-ui','important');previewName.textContent=entry.name+' — preview unavailable';}}
  }
  function highlight(button,entry){for(const option of list.children){const on=option===button;option.tabIndex=on?0:-1;option.classList.toggle('previewing',on);}clearTimeout(hoverTimer);hoverTimer=setTimeout(()=>paintPreview(entry),60);}
  async function apply(entry){
   if(busy||!current())return;busy=true;popup.setAttribute('aria-busy','true');error.hidden=true;list.inert=true;disk.disabled=more.disabled=search.disabled=true;clearTimeout(hoverTimer);
   try{
    let id=entry.id;
    if(id===undefined){const value=await fontSource(entry);if(!current())return;id=await keynopeImportTypeface(value,owner);}
    if(!current())return;
    enhance(select);close(false);select.value=id;select.keynopePreviousFont=id;select.dispatchEvent(new Event('change',{bubbles:true}));
   }catch(problem){fail(problem);}
   finally{busy=false;if(!closed){popup.removeAttribute('aria-busy');list.inert=false;disk.disabled=more.disabled=search.disabled=false;}}
  }
  function render(){
   const query=search.value.trim().toLocaleLowerCase(),base=[...select.options].filter(o=>!o.value.startsWith('__')&&o.value!=='mixed').map(o=>({id:o.value,name:o.textContent,family:'',postScriptName:''}));
   const matches=[...base,...entries].filter(entry=>(entry.name+' '+entry.family+' '+entry.postScriptName).toLocaleLowerCase().includes(query));
   list.replaceChildren();status.textContent=matches.length+' fonts';
   for(const entry of matches){const b=document.createElement('button');b.type='button';b.setAttribute('role','option');b.textContent=entry.name;b.dataset.font=entry.postScriptName||entry.id;const selected=entry.id?entry.id===select.value:deckFonts[select.value]?.postScriptName===entry.postScriptName;b.setAttribute('aria-selected',String(selected));b.tabIndex=selected?0:-1;b.onpointerenter=()=>highlight(b,entry);b.onfocus=()=>highlight(b,entry);b.onclick=()=>apply(entry);list.append(b);}
   if(list.firstChild&&!list.querySelector('[tabindex="0"]'))list.firstChild.tabIndex=0;
   more.hidden=expanded;search.hidden=!expanded;position();
  }
  async function expand(){
   expanded=true;more.hidden=true;search.hidden=false;status.textContent='Loading installed fonts…';position();
   try{
    if(nativeFonts()){catalogue ||= await nativeFonts().postMessage({action:'catalogue'});entries=catalogue;}
    else if(globalThis.queryLocalFonts)entries=(await queryLocalFonts()).map(f=>({name:f.fullName,family:f.family,postScriptName:f.postscriptName,blob:()=>f.blob()}));
    if(!current())return;render();if(!entries.length)status.textContent='Load a font from disk to add more fonts.';search.focus();
   }catch(problem){if(current()){render();fail(problem);}}
  }
  more.onclick=expand;disk.onclick=()=>file.click();
  file.onchange=()=>{const f=file.files[0];if(f)apply({name:f.name.replace(/\.(ttf|otf)$/i,''),postScriptName:f.name.replace(/\.(ttf|otf)$/i,''),file:f});};
  search.oninput=render;
  popup.addEventListener('pointerdown',event=>event.stopPropagation());
  popup.addEventListener('keydown',event=>{
   event.stopPropagation();if(event.key==='Escape'){event.preventDefault();close();return;}
   if(!['ArrowDown','ArrowUp','Home','End'].includes(event.key)||event.target===search&&['Home','End'].includes(event.key))return;
   const buttons=[...list.children];if(!buttons.length)return;event.preventDefault();let i=buttons.indexOf(document.activeElement);i=event.key==='Home'?0:event.key==='End'?buttons.length-1:Math.max(0,Math.min(buttons.length-1,i+(event.key==='ArrowUp'?-1:1)));buttons[i].focus();buttons[i].scrollIntoView({block:'nearest'});
  });
  render();const selected=[...select.options].find(o=>o.value===select.value);if(selected)paintPreview({id:selected.value,name:selected.textContent});
  if(command==='__load_font__')file.click();else if(command==='__more_fonts__'||expanded)expand();else list.querySelector('[tabindex="0"]')?.focus();
 }
 async function confirmExport(format){
  await globalThis.keynopePrepareDocumentSave?.();
  const response=await fetch('/api/editor/font-export?format='+encodeURIComponent(format));if(!response.ok)throw Error(await response.text());const context=await response.json();
  if(!context.fonts.length)return true;
  return new Promise(resolve=>{
   const dialog=document.createElement('dialog');dialog.className='keynope-font-export-dialog';dialog.setAttribute('aria-label','Restricted fonts');
   const title=document.createElement('h2');title.textContent='Restricted fonts';
   const saving=format==='save',verb=saving?'Save':'Export';
   const help=document.createElement('p');help.textContent='The following fonts do not allow embedding. Only their names will be saved, not the font files. On another computer, Keynope will use the installed fonts when available, or its default font otherwise. Substitution may change the text layout. You can also choose replacement fonts.';
   const list=document.createElement('ul');for(const f of context.fonts){const li=document.createElement('li');li.textContent=f.name;list.append(li);}
   const replacements=document.createElement('div');replacements.hidden=true;const fields=new Map();
   const error=document.createElement('p');error.setAttribute('role','alert');
   const actions=document.createElement('div');actions.className='keynope-font-actions';
   const cancel=document.createElement('button');cancel.type='button';cancel.textContent='Cancel';cancel.onclick=()=>dialog.close();
   const replace=document.createElement('button');replace.type='button';replace.textContent='Replace fonts…';
   const proceed=document.createElement('button');proceed.type='button';proceed.textContent=verb;
   replace.onclick=()=>{replacements.hidden=false;replace.hidden=true;for(const font of context.fonts){const label=document.createElement('label');label.textContent=font.name;const select=document.createElement('select');select.setAttribute('aria-label','Replace '+font.name);for(const id of ['sans','mono','c64'])select.add(new Option(name(id),id));enhance(select);fields.set(font.id,select);label.append(select);replacements.append(label);}fields.values().next().value?.focus();};
   let accepted=false,pending=false;
   proceed.onclick=async()=>{if(pending)return;pending=true;for(const select of fields.values())select.disabled=true;proceed.disabled=replace.disabled=cancel.disabled=true;try{const chosen=Object.fromEntries([...fields].map(([id,select])=>[id,select.value]));if(saving&&fields.size){await globalThis.keynopeReplaceTypefaces(chosen,context);}else{const body={owner:context.owner,version:context.version,format,confirmed:true,replacements:chosen};const r=await fetch('/api/editor/font-export',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});if(!r.ok)throw Error(await r.text());}accepted=true;dialog.close();}catch(problem){error.textContent=problem.message;proceed.disabled=false;replace.disabled=cancel.disabled=false;}finally{pending=false;for(const select of fields.values())select.disabled=false;}};
   actions.append(cancel,proceed);dialog.append(title,help,list,replace,replacements,error,actions);document.body.append(dialog);
   dialog.addEventListener('cancel',event=>{if(pending)event.preventDefault();});dialog.addEventListener('keydown',event=>event.stopPropagation());dialog.addEventListener('close',()=>{active?.close(false);dialog.remove();resolve(accepted);});dialog.showModal();cancel.focus();
  });
 }
 globalThis.keynopeConfirmFontExport=confirmExport;
 const previewOnly=id=>{const f=deckFonts[id];return !!f&&!f.localOnly&&!(f.embeddingFlags&8)&&!!(f.embeddingFlags&4);};
 return {valid,family,name,setDeck,install,readyFor,choices,enhance,previewOnly,missing:()=>[...missing.values()]};
})();
