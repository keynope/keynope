/* Shared chart authoring. Apply publishes one owned payload transaction. */
globalThis.KeynopeCharts=(()=>{
  const clone=value=>JSON.parse(JSON.stringify(value)),types=['column','bar','line','area','pie','doughnut'];let active=null;
  const roles=[['text','Text'],['heading','Heading'],['background','Background'],['surface','Surface'],['muted','Muted'],['line','Line'],...Array.from({length:6},(_,i)=>['accent-'+(i+1),'Accent '+(i+1)])];
  function validateColour(style){if(style.labelColor&&!/^#[\da-f]{6}$/i.test(style.labelColor)&&style.labelColor!=='none'&&!roles.some(([role])=>'theme:'+role===style.labelColor))throw Error('Choose an RGB label colour or theme role.');if(style.color!=null&&style.color!==''&&style.color!=='none'&&!/^#[\da-f]{6}$/i.test(style.color))throw Error('Choose a six-digit RGB colour.');if(style.colorToken&&(!roles.some(([role])=>role===style.colorToken)||!/^#[\da-f]{6}$/i.test(style.color||'')))throw Error('A theme colour needs a valid role and RGB fallback.');}
  function number(value){const s=String(value).trim();if(!/^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:e[+-]?\d+)?$/i.test(s)||!Number.isFinite(Number(s)))throw Error('Enter a finite number, such as 4.5 or -2.');return Number(s);}
  function utf8Bytes(text,limit=1<<20){let bytes=0;for(const char of text){const code=char.codePointAt(0);bytes+=code<0x80?1:code<0x800?2:code<0x10000?3:4;if(bytes>limit)break;}return bytes;}
  function validateTextBudget(data){
    const limit=1<<20;let bytes=0;
    const add=text=>{if(text==null)return;if(typeof text!=='string')throw Error('Chart labels must be text.');bytes+=utf8Bytes(text,limit-bytes);if(bytes>limit)throw Error('Chart text is limited to 1 MiB. Shorten titles or labels.');};
    add(data.title);add(data.options?.xTitle);add(data.options?.yTitle);for(const category of data.categories)add(category);for(const series of data.series)add(series.name);
  }
  function validate(data){
    const next=clone(data);if(!types.includes(next.type))throw Error('Choose a supported chart type.');
    if(next.categories.length<1||next.categories.length>2000||next.series.length<1||next.series.length>64)throw Error('Keep 1–2000 categories and 1–64 series.');
    validateTextBudget(next);
    const circular=['pie','doughnut'].includes(next.type);if(circular&&next.series.length!==1)throw Error('Pie and doughnut charts need one series. Remove the other series first.');
    if(next.options.stacked&&!['column','bar'].includes(next.type))throw Error('Stacking is available for column and bar charts.');
    for(const [si,series] of next.series.entries()){if(series.values.length!==next.categories.length)throw Error('Every category needs a value in every series.');series.values=series.values.map((v,i)=>{try{const n=number(v);if(circular&&n<0)throw Error('Pie and doughnut values cannot be negative.');return n;}catch(e){const error=Error(`${series.name||'Series'}, ${next.categories[i]||'category '+(i+1)}: ${e.message}`);error.chartCell=[i,si+1];throw error;}});}
    next.series.forEach(validateColour);if(next.categoryColors){if(!Array.isArray(next.categoryColors)||next.categoryColors.length>next.categories.length)throw Error('Slice colours must match the categories.');next.categoryColors.forEach(validateColour);}
    if(next.options.fontId&&!['c64','sans','mono'].includes(next.options.fontId))throw Error('Choose a bundled chart font.');
    if(next.options.fontRole&&(!['heading','body','mono'].includes(next.options.fontRole)||!['c64','sans','mono'].includes(next.options.fontId)))throw Error('Choose a valid chart font role and bundled fallback.');
    if(next.options.fontWidth!=null&&next.options.fontWidth!==''){next.options.fontWidth=number(next.options.fontWidth);if(next.options.fontWidth<1||next.options.fontWidth>200){const error=Error('Font width must be between 1 and 200.');error.chartField='Font width';throw error;}}else delete next.options.fontWidth;
    if(next.options.gridColor&&!/^#[\da-f]{6}$/i.test(next.options.gridColor)&&next.options.gridColor!=='none'&&!roles.some(([role])=>'theme:'+role===next.options.gridColor))throw Error('Choose a valid grid colour.');
    for(const key of ['minimum','maximum']){const raw=next.options[key];if(raw==null||String(raw).trim()==='')delete next.options[key];else try{next.options[key]=number(raw);}catch(error){error.chartField='Axis '+key;throw error;}}
    if(next.options.minimum!=null&&next.options.maximum!=null&&next.options.minimum>=next.options.maximum){const error=Error('Axis minimum must be less than maximum.');error.chartField='Axis maximum';throw error;}return next;
  }
  function parsePaste(text){
    if(typeof text!=='string')throw Error('Paste chart text.');
    if(text.length>1<<20||utf8Bytes(text)>1<<20)throw Error('Chart paste is limited to 1 MiB.');const rows=[[]];let field='',quoted=false;
    for(let i=0;i<text.length;i++){const c=text[i];if(c==='"'&&(quoted||field==='')){if(quoted&&text[i+1]==='"'){field+='"';i++;}else quoted=!quoted;}else if(!quoted&&['\t','\r','\n'].includes(c)){rows.at(-1).push(field);field='';if(c!=='\t'){if(c==='\r'&&text[i+1]==='\n')i++;rows.push([]);}if(rows.length>2002||rows.at(-1).length>65)throw Error('Paste exceeds 2000 categories or 64 series.');}else field+=c;}
    if(quoted)throw Error('Unclosed quote in pasted data.');rows.at(-1).push(field);if(rows.length>1&&rows.at(-1).length===1&&rows.at(-1)[0]==='')rows.pop();
    if(rows.length<2||rows.length>2001||rows[0].length<2||rows[0].length>65||rows.some(r=>r.length!==rows[0].length))throw Error('Paste a rectangular tab-separated table: category and series names on the first row, then labels and numeric values.');
    return {categories:rows.slice(1).map(r=>r[0]),series:rows[0].slice(1).map((name,i)=>({name,values:rows.slice(1).map(r=>number(r[i+1]))}))};
  }
  function edit(source,commit,options={}){
    if(active){active.dialog.focus();return;}
    const previous=document.activeElement,d=document.createElement('dialog');d.className='keynope-table-dialog keynope-chart-dialog';d.setAttribute('aria-label','Edit chart');d.dataset.keynopeSceneDialog='chart';
    let data=clone(source),history=[],future=[],page=0,seriesPage=0,busy=false,generation=0,pendingCommit=null;data.options||={};
    let invalidBaseline=false;
    try{data=validate(data);}catch{invalidBaseline=true;}
    let baseline=JSON.stringify(data),dirtyGeneration=-1,dirtyValue=false;
    // Canonicalize only the edited number. Full structural validation belongs
    // to Apply, not every dirty-indicator poll or keystroke in a large chart.
    const draftNumber=value=>{try{return number(value);}catch{return value;}};
    function setAxis(key,value){if(String(value).trim()==='')delete data.options[key];else data.options[key]=draftNumber(value);}
    const seriesPageSize=8;
    function visibleSeries(fn){for(let i=seriesPage*seriesPageSize;i<Math.min(data.series.length,(seriesPage+1)*seriesPageSize);i++)fn(data.series[i],i);}
    const dirty=()=>{if(dirtyGeneration!==generation){dirtyValue=invalidBaseline||JSON.stringify(data)!==baseline;dirtyGeneration=generation;}return dirtyValue;},notify=()=>{const u=d.querySelector('[data-chart-undo]'),r=d.querySelector('[data-chart-redo]');if(u)u.disabled=!history.length;if(r)r.disabled=!future.length;options.onDirty?.();};
    function close(){if(busy)return;d.close();d.remove();active=null;notify();if(previous?.isConnected)previous.focus({preventScroll:true});}
    const historyByteLimit=32*1024*1024;
    function snapshot(){const json=JSON.stringify(data);return {json,bytes:json.length*2};}
    function trimHistory(){
      let bytes=[...history,...future].reduce((n,entry)=>n+entry.bytes,0);
      while(history.length>100){bytes-=history.shift().bytes;}
      while(future.length>100){bytes-=future.shift().bytes;}
      while(bytes>historyByteLimit&&(history.length||future.length)){
        // Keep the nearest Undo/Redo states; discard the oldest Undo first.
        bytes-=(history.length?history.shift():future.shift()).bytes;
      }
    }
    function checkpoint(entry=snapshot()){generation++;future=[];history.push(entry);trimHistory();}
    function mutate(fn){
      const before=snapshot(),oldPage=page,oldSeriesPage=seriesPage;
      try{fn();}catch(error){data=JSON.parse(before.json);page=oldPage;seriesPage=oldSeriesPage;draw();notify();throw error;}
      if(JSON.stringify(data)!==before.json)checkpoint(before);
      draw();notify();
    }
    function activeCell(){const el=document.activeElement;return host.contains(el)&&el?.dataset.chartRow!=null?[Number(el.dataset.chartRow),Number(el.dataset.chartColumn)]:null;}
    function restoreCell(cell){if(cell)focusCell(Math.min(cell[0],data.categories.length-1),Math.min(cell[1],data.series.length));}
    function undo(){if(!busy&&history.length){const cell=activeCell(),entry=history.pop();generation++;future.push(snapshot());data=JSON.parse(entry.json);trimHistory();draw();restoreCell(cell);notify();}}
    function redo(){if(!busy&&future.length){const cell=activeCell(),entry=future.pop();generation++;history.push(snapshot());data=JSON.parse(entry.json);trimHistory();draw();restoreCell(cell);notify();}}
    function button(label,fn){const b=document.createElement('button');b.type='button';b.textContent=label;b.onclick=fn;return b;}
    function input(label,value,set){const l=document.createElement('label'),i=document.createElement('input');l.textContent=label;i.value=value??'';i.setAttribute('aria-label',label);l.append(i);let recorded=false;i.onfocus=()=>recorded=false;i.oninput=()=>{const before=recorded?null:snapshot();set(i.value);if(before){if(JSON.stringify(data)!==before.json){checkpoint(before);recorded=true;}}else generation++;i.removeAttribute('aria-invalid');error.textContent='';notify();};i.onblur=()=>recorded=false;return {label:l,input:i};}
    function select(label,choices,value,set){const l=document.createElement('label'),s=document.createElement('select');l.textContent=label;s.setAttribute('aria-label',label);for(const [v,name]of choices){const o=document.createElement('option');o.value=v;o.textContent=name;s.append(o);}globalThis.KeynopeFonts?.enhance(s);s.value=value;s.onchange=()=>mutate(()=>set(s.value));l.append(s);return l;}
    function check(label,value,set){const l=document.createElement('label'),i=document.createElement('input');i.type='checkbox';i.checked=value;i.onchange=()=>mutate(()=>set(i.checked));l.append(i,document.createTextNode(label));return l;}
    const heading=document.createElement('h2');heading.textContent='Edit chart';
    const toolbar=document.createElement('div'),fields=document.createElement('div'),styles=document.createElement('div'),pager=document.createElement('div');for(const el of [toolbar,fields,styles,pager])el.className='keynope-table-toolbar';
    const host=document.createElement('div');host.className='keynope-table-grid-host keynope-chart-grid';const error=document.createElement('p');error.setAttribute('role','alert');error.id='keynope-chart-error';
    function showValidationError(problem){
      let field;
      if(problem.chartCell){focusCell(...problem.chartCell);field=document.activeElement;}
      else if(problem.chartField){field=Array.from(fields.querySelectorAll('input')).find(input=>input.getAttribute('aria-label')===problem.chartField);field?.focus();field?.select();}
      if(field&&d.contains(field)){field.setAttribute('aria-invalid','true');field.setAttribute('aria-describedby',error.id);}
      error.textContent=problem.message;
    }
    function focusCell(row,column){
      if(row<0||row>=data.categories.length||column<0||column>data.series.length)return false;
      const targetPage=Math.floor(row/50),targetSeries=column===0?seriesPage:Math.floor((column-1)/seriesPageSize);
      if(page!==targetPage||seriesPage!==targetSeries){document.activeElement?.blur();page=targetPage;seriesPage=targetSeries;draw();}
      const field=host.querySelector(`[data-chart-row="${row}"][data-chart-column="${column}"]`);
      if(!field)return false;field.focus({preventScroll:true});field.select();field.scrollIntoView({block:'nearest',inline:'nearest'});return true;
    }
    function gridCell(field,row,column){
      field.dataset.chartRow=row;field.dataset.chartColumn=column;
      field.onkeydown=event=>{
        if(event.isComposing||event.keyCode===229||event.altKey||event.ctrlKey||event.metaKey||busy)return;
        let nextRow=row,nextColumn=column;
        if(event.key==='Enter')nextRow+=event.shiftKey?-1:1;
        else if(event.key==='ArrowUp')nextRow--;
        else if(event.key==='ArrowDown')nextRow++;
        else if(event.key==='Tab'){
          const index=row*(data.series.length+1)+column+(event.shiftKey?-1:1);
          if(index<0||index>=data.categories.length*(data.series.length+1))return;
          nextRow=Math.floor(index/(data.series.length+1));nextColumn=index%(data.series.length+1);
        }else return;
        event.preventDefault();focusCell(nextRow,nextColumn);
      };
      return field;
    }
    function available(){return !busy&&d.isConnected&&(options.available?.()??true);}
    function roleColour(role,fallback){const colors=options.theme?.()?.colors;return (role.startsWith('accent-')?colors?.accent?.[Number(role.slice(7))-1]:colors?.[role])||fallback||'#55aaff';}
    function colourControls(label,style,index,set,fallbackColor){
      const fallback=fallbackColor||options.theme?.()?.colors?.accent?.[index%6]||['#55aaff','#ffaa55','#55ffaa','#ff55aa'][index%4];
      const effective=style.colorToken?roleColour(style.colorToken,style.color):style.color||fallback;
      const write=value=>{const next={...style};delete next.color;delete next.colorToken;Object.assign(next,value);set(next);};
      const mode=style.colorToken?'theme:'+style.colorToken:style.color?'custom':'';
      const choice=select(label+' source',[['','Inherit'],['custom','Custom'],...roles.map(([role,name])=>['theme:'+role,name])],mode,value=>{
        if(!value)write({});else if(value==='custom')write({color:effective});else{const token=value.slice(6);write({color:roleColour(token,effective),colorToken:token});}
      });
      const b=button(label,()=>{const captured=generation;options.pickColor(b,effective,value=>{
        if(!available()||captured!==generation){if(d.isConnected)error.textContent='The chart changed; choose the colour again.';return;}
        mutate(()=>write(value?{color:value}:{}));
      });});b.setAttribute('aria-label',label);b.style.borderLeft='12px solid '+effective;
      const inherit=button('Inherit '+label.toLowerCase(),()=>mutate(()=>write({})));
      return [choice,options.pickColor?b:input(label,style.color,value=>write(value?{color:value}:{})).label,inherit];
    }
    function labelColourControls(label,style,set){
      const write=value=>{const next={...style};if(value)next.labelColor=value;else delete next.labelColor;set(next);};
      const value=style.labelColor||'',token=value.startsWith('theme:')?value.slice(6):'',effective=token?roleColour(token,'#ffffff'):value||'#ffffff';
      const choice=select(label+' source',[['','Automatic contrast'],['custom','Custom'],...roles.map(([role,name])=>['theme:'+role,name])],token?value:value?'custom':'',next=>write(next==='custom'?effective:next));
      choice.firstChild.textContent=label+' ';choice.style.cssText='display:inline-flex;align-items:center;gap:.35em;white-space:nowrap';
      const colour=button(label,()=>{const captured=generation;options.pickColor(colour,effective,value=>{if(!available()||captured!==generation){if(d.isConnected)error.textContent='The chart changed; choose the colour again.';return;}mutate(()=>write(value));});});
      colour.setAttribute('aria-label',label);colour.textContent='Pick colour';
      colour.style.borderLeft='12px solid '+(value?effective:'transparent');colour.title=value||'Automatic contrast';
      const auto=button('Auto '+label.toLowerCase(),()=>mutate(()=>write('')));auto.setAttribute('aria-label','Auto '+label.toLowerCase());auto.textContent='Auto';
      const group=document.createElement('div');group.style.cssText='display:flex;align-items:center;flex-wrap:wrap;gap:.35em;margin:.35em 0';group.append(choice,options.pickColor?colour:input(label,value,write).label,auto);
      return [group];
    }
    function draw(){
      for(const el of [toolbar,fields,styles,host,pager])el.replaceChildren();page=Math.min(page,Math.floor((data.categories.length-1)/50));seriesPage=Math.min(seriesPage,Math.floor((data.series.length-1)/seriesPageSize));
      const u=button('Undo',undo),r=button('Redo',redo);u.dataset.chartUndo='';r.dataset.chartRedo='';u.disabled=!history.length;r.disabled=!future.length;toolbar.append(u,r,select('Chart type',types.map(t=>[t,t[0].toUpperCase()+t.slice(1)]),data.type,v=>{if(data.type===v)return;data.type=v;if(!['column','bar'].includes(v))delete data.options.stacked;}),input('Chart title',data.title,v=>data.title=v).label);
      fields.append(check('Legend',!!data.options.legend,v=>data.options.legend=v),check('Data labels',!!data.options.dataLabels,v=>data.options.dataLabels=v));
      fields.append(select('Label font',[['','Inherit'],['theme:heading','Theme heading'],['theme:body','Theme body'],['theme:mono','Theme mono'],['c64','Keynope C64'],['sans','Sans'],['mono','Mono']],data.options.fontRole?'theme:'+data.options.fontRole:data.options.fontId||'',v=>{delete data.options.fontRole;if(v.startsWith('theme:')){const role=v.slice(6);data.options.fontRole=role;data.options.fontId=options.theme?.()?.fonts?.[role]?.fontId||data.options.fontId||'sans';}else if(v)data.options.fontId=v;else delete data.options.fontId;}));
      const fontWidth=input('Font width',data.options.fontWidth,v=>{if(v==='')delete data.options.fontWidth;else data.options.fontWidth=draftNumber(v);});fontWidth.input.type='number';fontWidth.input.min=1;fontWidth.input.max=200;fontWidth.input.step='any';fontWidth.input.placeholder='Inherit';fields.append(fontWidth.label);
      if(!['pie','doughnut'].includes(data.type)){const value=data.options.gridColor||'',token=value.startsWith('theme:')?value.slice(6):'',style=token?{color:roleColour(token,'#64748b'),colorToken:token}:{color:value};fields.append(...colourControls('Grid colour',style,0,next=>{const value=next.colorToken?'theme:'+next.colorToken:next.color;if(value)data.options.gridColor=value;else delete data.options.gridColor;},options.theme?.()?.colors?.line||'#64748b'));}
      if(!['pie','doughnut'].includes(data.type))fields.append(check('Axes',!data.options.hideAxes,v=>data.options.hideAxes=!v),input('Category axis title',data.options.xTitle,v=>data.options.xTitle=v).label,input('Value axis title',data.options.yTitle,v=>data.options.yTitle=v).label,input('Axis minimum',data.options.minimum,v=>setAxis('minimum',v)).label,input('Axis maximum',data.options.maximum,v=>setAxis('maximum',v)).label);
      fields.append(select('Number format',[['number','Number'],['percent','Percent']],data.options.valueFormat||'number',v=>data.options.valueFormat=v));if(['column','bar'].includes(data.type))fields.append(check('Stack series',!!data.options.stacked,v=>data.options.stacked=v));else if(data.options.stacked)fields.append(button('Use unstacked chart',()=>mutate(()=>data.options.stacked=false)));
      if(!['pie','doughnut'].includes(data.type))visibleSeries((series,index)=>{
        styles.append(...colourControls('Series '+(index+1)+' colour',series,index,value=>data.series[index]=value));
        if(data.options.dataLabels)styles.append(...labelColourControls('Series '+(index+1)+' label colour',series,value=>data.series[index]=value));
      });
      const table=document.createElement('table'),thead=document.createElement('thead'),tr=document.createElement('tr'),category=document.createElement('th');category.textContent='Category';tr.append(category);
      visibleSeries((series,index)=>{const th=document.createElement('th');th.scope='col';th.append(input('Series '+(index+1)+' name',series.name,v=>series.name=v).label,button('Remove series '+(index+1),()=>{if(data.series.length===1){error.textContent='Keep at least one series.';return;}mutate(()=>data.series.splice(index,1));}));tr.append(th);});const actions=document.createElement('th');actions.textContent='Rows';tr.append(actions);thead.append(tr);table.append(thead);
      const body=document.createElement('tbody');for(let index=page*50;index<Math.min(data.categories.length,(page+1)*50);index++){
        const row=document.createElement('tr'),th=document.createElement('th');th.scope='row';th.append(gridCell(input('Category '+(index+1),data.categories[index],v=>data.categories[index]=v).input,index,0));row.append(th);
        visibleSeries((series,si)=>{const td=document.createElement('td'),field=input('Value '+(index+1)+', '+(si+1),series.values[index],v=>series.values[index]=draftNumber(v));field.input.inputMode='decimal';field.input.onblur=()=>{try{const n=number(field.input.value);if(['pie','doughnut'].includes(data.type)&&n<0)throw Error('Use a non-negative value.');}catch(e){field.input.setAttribute('aria-invalid','true');error.textContent=e.message;}};td.append(gridCell(field.input,index,si+1));row.append(td);});
        const td=document.createElement('td');if(['pie','doughnut'].includes(data.type))td.append(...colourControls('Slice '+(index+1)+' colour',data.categoryColors?.[index]||{},index,value=>{data.categoryColors||=[];while(data.categoryColors.length<=index)data.categoryColors.push({});data.categoryColors[index]=value;while(data.categoryColors.length&&!Object.keys(data.categoryColors.at(-1)).length)data.categoryColors.pop();if(!data.categoryColors.length)delete data.categoryColors;}));
        if(['pie','doughnut'].includes(data.type)&&data.options.dataLabels)td.append(...labelColourControls('Slice '+(index+1)+' label colour',data.categoryColors?.[index]||{},value=>{data.categoryColors||=[];while(data.categoryColors.length<=index)data.categoryColors.push({});data.categoryColors[index]=value;}));
        td.append(button('Remove category '+(index+1),()=>{if(data.categories.length===1){error.textContent='Keep at least one category.';return;}mutate(()=>{data.categories.splice(index,1);data.categoryColors?.splice(index,1);for(const s of data.series)s.values.splice(index,1);});}));row.append(td);body.append(row);
      }table.append(body);host.append(table);
      const prev=button('Previous categories',()=>{page--;draw();}),next=button('Next categories',()=>{page++;draw();});prev.disabled=page===0;next.disabled=(page+1)*50>=data.categories.length;const status=document.createElement('span');status.textContent=`${page*50+1}–${Math.min((page+1)*50,data.categories.length)} of ${data.categories.length} categories`;status.setAttribute('role','status');
      if(data.series.length>seriesPageSize){
        const previous=button('Previous series',()=>{seriesPage--;draw();}),next=button('Next series',()=>{seriesPage++;draw();});previous.disabled=seriesPage===0;next.disabled=(seriesPage+1)*seriesPageSize>=data.series.length;
        const status=document.createElement('span');status.setAttribute('role','status');status.textContent=`Series ${seriesPage*seriesPageSize+1}–${Math.min((seriesPage+1)*seriesPageSize,data.series.length)} of ${data.series.length}`;
        pager.append(previous,status,next);
      }
      pager.append(prev,status,next,button('Add category',()=>{if(data.categories.length>=2000){error.textContent='Maximum 2000 categories.';return;}mutate(()=>{data.categories.push('Category '+(data.categories.length+1));for(const s of data.series)s.values.push(0);page=Math.floor((data.categories.length-1)/50);});}),button('Add series',()=>{if(data.series.length>=64){error.textContent='Maximum 64 series.';return;}mutate(()=>{data.series.push({name:'Series '+(data.series.length+1),values:data.categories.map(()=>0)});seriesPage=Math.floor((data.series.length-1)/seriesPageSize);});}));
    }
    const paste=document.createElement('textarea');paste.setAttribute('aria-label','Paste chart data');paste.placeholder='Category\tSeries 1\tSeries 2\nFirst\t4\t7\nSecond\t3\t5';paste.rows=3;paste.style.width='100%';
    const pasteButton=button('Paste data',()=>{try{const parsed=parsePaste(paste.value),next={...data,categories:parsed.categories,categoryColors:data.categoryColors?.slice(0,parsed.categories.length),series:parsed.series.map((s,i)=>({...data.series[i],...s}))};validateTextBudget(next);mutate(()=>{data=next;page=0;seriesPage=0;});error.textContent='';}catch(e){error.textContent=e.message;}});
    const disabledControls=new WeakMap();
    function setBusy(value){busy=value;for(const control of d.querySelectorAll('button,input,select,textarea')){if(value){disabledControls.set(control,control.disabled);control.disabled=true;}else control.disabled=disabledControls.get(control)||false;}d.setAttribute('aria-busy',String(value));}
    function apply(){
      if(pendingCommit)return pendingCommit;
      let accepted;try{if(!available())throw Error('The document changed; reopen the chart editor.');accepted=validate(data);}catch(e){showValidationError(e);return Promise.reject(e);}
      setBusy(true);
      pendingCommit=Promise.resolve().then(()=>commit(accepted)).then(()=>{baseline=JSON.stringify(data);setBusy(false);close();},e=>{error.textContent=e.message;setBusy(false);throw e;}).finally(()=>{pendingCommit=null;});
      return pendingCommit;
    }
    const applyButton=button('Apply',()=>apply().catch(()=>{})),footer=document.createElement('div');footer.className='keynope-table-actions';footer.append(button('Cancel',close),applyButton);
    d.append(heading,toolbar,fields,styles,host,pager,paste,pasteButton,error,footer);d.addEventListener('cancel',e=>{e.preventDefault();close();});d.addEventListener('keydown',e=>{e.stopPropagation();if((e.metaKey||e.ctrlKey)&&['z','y'].includes(e.key.toLowerCase())){e.preventDefault();e.key.toLowerCase()==='y'||e.shiftKey?redo():undo();}});
    active={dialog:d,dirty,apply,pending:()=>pendingCommit};document.body.append(d);draw();d.showModal();d.querySelector('select')?.focus();
  }
  return {edit,validate,parsePaste,isDirty:()=>!!active?.dirty(),commitPending:()=>active?.pending()||(active?.dirty()?active.apply():Promise.resolve())};
})();
