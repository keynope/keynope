/* Keynope workshop activities. Completed results are archived by the editor. */
globalThis.KeynopeGames = (() => {
const timerGlyphs = {
  '0': ['  ████  ', ' ██  ██ ', ' ██ ███ ', ' ██████ ', ' ███ ██ ', ' ██  ██ ', '  ████  ', '        '],
  '1': ['   ██   ', '  ███   ', '   ██   ', '   ██   ', '   ██   ', '   ██   ', ' ██████ ', '        '],
  '2': ['  ████  ', ' ██  ██ ', '     ██ ', '   ███  ', '  ██    ', ' ██  ██ ', ' ██████ ', '        '],
  '3': ['  ████  ', ' ██  ██ ', '     ██ ', '   ███  ', '     ██ ', ' ██  ██ ', '  ████  ', '        '],
  '4': ['    ███ ', '   ████ ', '  ██ ██ ', ' ██  ██ ', ' ███████', '     ██ ', '     ██ ', '        '],
  '5': [' ██████ ', ' ██     ', ' █████  ', '     ██ ', '     ██ ', ' ██  ██ ', '  ████  ', '        '],
  '6': ['   ███  ', '  ██    ', ' ██     ', ' █████  ', ' ██  ██ ', ' ██  ██ ', '  ████  ', '        '],
  '7': [' ██████ ', ' ██  ██ ', '     ██ ', '    ██  ', '   ██   ', '   ██   ', '   ██   ', '        '],
  '8': ['  ████  ', ' ██  ██ ', ' ██  ██ ', '  ████  ', ' ██  ██ ', ' ██  ██ ', '  ████  ', '        '],
  '9': ['  ████  ', ' ██  ██ ', ' ██  ██ ', '  █████ ', '     ██ ', '    ██  ', '  ███   ', '        '],
  ':': ['        ', '        ', '   ██   ', '        ', '        ', '   ██   ', '        ', '        ']
};
  const names = {shuffle:'Shuffle',deducer:'Deducer',finalanswer:'Final Answer',pressure:'Pressure Cooker',chosen:'The Chosen',ball:'Ball Toss',gallery:'Gallery Walk',hunt:'Data Hunt',teach:'Teach-Back',fame:'Wall of Fame',agreements:'Working Agreements',three:'Three Before Me'};
  names.nominate='Nominate';
  const has = kind => Object.hasOwn(names,kind);
  const shuffle = values => {
    const result=values.slice();
    for(let i=result.length-1;i>0;i--){const bytes=new Uint32Array(1),limit=4294967296-4294967296%(i+1);do{crypto.getRandomValues(bytes);}while(bytes[0]>=limit);const j=bytes[0]%(i+1);[result[i],result[j]]=[result[j],result[i]];}
    return result;
  };
  function init(r) { return r.game ||= {entries:[],votes:{},volunteers:[],spoken:[],speaker:'',stage:'join',groups:[],turn:0}; }
  function syncDeducerGroups(r) {
    if(r.definition.kind!=='deducer'||r.phase!==1||r.completedReview)return false;
    const g=init(r),before=JSON.stringify(g.groups),names=r.memberNames||{};
    const count=Math.max(1,Math.min(10,Number(r.definition.groupCount)||4));
    if(!g.groups.length&&!g.manualGroups)g.groups=Array.from({length:count},(_,i)=>({id:'deducer-'+i,label:'Group '+(i+1),ids:[],names:[]}));
    for(const group of g.groups)group.ids=group.ids.filter(id=>Object.hasOwn(names,id));
    const assigned=new Set(g.groups.flatMap(group=>group.ids));
    const newcomers=shuffle(Object.keys(names).filter(id=>names[id]&&!assigned.has(id)&&!g.unassigned?.includes(id)));
    for(const id of newcomers){
      if(!g.groups.length)break;
      const least=Math.min(...g.groups.map(group=>group.ids.length));
      shuffle(g.groups.filter(group=>group.ids.length===least))[0].ids.push(id);
    }
    for(const group of g.groups)group.names=group.ids.map(id=>names[id]);
    r.pairAssignments={};
    g.groups.forEach((group,i)=>group.ids.forEach(id=>r.pairAssignments[id]={group:group.id||'deducer-'+i,label:group.label,members:group.ids.slice()}));
    r.groups=g.groups.map(group=>({name:group.label,members:group.names.slice()}));
    r.participants=Object.keys(names).length;
    return before!==JSON.stringify(g.groups);
  }
  function receiveDeducer(r,event) {
    const g=init(r),p=event.payload.response||{},id=event.identity;
    if(g.pairingEpoch&&p.groupEpoch!==g.pairingEpoch)return;
    if(r.phase!==1||r.completedReview||!r.memberNames?.[id]||(r.deadlineMs&&Date.now()>=r.deadlineMs))return;
    syncDeducerGroups(r);
    const group=g.groups.findIndex(group=>group.ids.includes(id));
    if(group<0||!event.id)return;
    r.seenResponseEvents||={};
    if(r.seenResponseEvents[event.id])return;
    r.seenResponseEvents[event.id]=true;
    const entries=g.entries.filter(entry=>entry.item===group),limit=r.definition.kind==='finalanswer'?1:Number(r.definition.maxEntries)||5;
    if(p.action==='add'&&text(p.text)&&entries.length<limit) {
      if(p.requestId&&g.entries.some(entry=>entry.requestId===p.requestId))return;
      g.entries.push({entryId:event.id,event:event.id,id,name:r.memberNames[id],item:group,text:text(p.text),requestId:String(p.requestId||'').slice(0,100)});
    } else if(p.action==='remove') {
      g.entries=g.entries.filter(entry=>entry.item!==group||entry.entryId!==p.entryId);
    } else if(p.action==='move'&&r.definition.kind!=='finalanswer') {
      const from=entries.findIndex(entry=>entry.entryId===p.entryId);
      if(from<0||p.beforeId===p.entryId)return;
      // Resolve stable entry IDs against the latest list, not a stale UI index.
      if(p.beforeId&&!entries.some(entry=>entry.entryId===p.beforeId))return;
      const [entry]=entries.splice(from,1),to=p.beforeId?entries.findIndex(entry=>entry.entryId===p.beforeId):entries.length;
      entries.splice(to,0,entry);
      g.entries=g.entries.filter(entry=>entry.item!==group).concat(entries);
    }
  }
  const text = value => String(value||'').trim().slice(0,500);
  function receive(r,event) {
    if(!has(r.definition.kind))return false;
    const g=init(r),p=event.payload.response||{},id=event.identity,kind=r.definition.kind;
    if(kind==='nominate'){
      if(r.phase!==1||r.completedReview||(r.deadlineMs&&Date.now()>=r.deadlineMs)||!r.memberNames?.[id]||r.memberNames[id]==='Presenter'||!event.id)return true;
      const skip=p.skip===true,target=typeof p.nominee==='string'?p.nominee:'';
      if(!skip&&(!target||target===id||!r.memberNames[target]||r.memberNames[target]==='Presenter'))return true;
      r.seenResponseEvents||={};if(r.seenResponseEvents[event.id])return true;r.seenResponseEvents[event.id]=true;
      const createdAt=new Date(event.createdAt).getTime()||0,previous=g.entries.find(entry=>entry.id===id);
      if(previous?.event===event.id||(createdAt&&previous?.createdAt>createdAt))return true;
      g.entries=g.entries.filter(entry=>entry.id!==id);
      g.entries.push({id,event:event.id,createdAt,name:r.memberNames[id],target:skip?'':target,skip});
      r.participants=g.entries.length;return true;
    }
    if(kind==='deducer'||kind==='finalanswer'){receiveDeducer(r,event);return true;}
    // Joining is enough to enter the draw. Responses cannot change its outcome.
    if(kind==='chosen'||kind==='pressure'||kind==='shuffle')return true;
    if(!r.memberNames[id] || r.phase!==1 || (r.deadlineMs&&Date.now()>=r.deadlineMs))return true;
    if(g.entries.some(e=>e.event===event.id))return true;
    const entry={event:event.id,id,name:r.memberNames[id],text:text(p.text),target:text(p.target),symbol:text(p.symbol).slice(0,16)};
    if(kind==='ball') {
      if(g.stage==='done')return true;
      if(p.volunteer===true&&!g.volunteers.includes(id)&&g.speaker!==id)g.volunteers.push(id);
      if(g.stage==='play'&&p.target&&g.speaker===id&&g.volunteers.includes(p.target)){g.speaker=p.target;g.volunteers=g.volunteers.filter(v=>v!==p.target);}
      if(g.stage==='play'&&p.end===true&&g.speaker===id&&!g.volunteers.length){g.stage='done';r.phase=3;r.deadlineMs=0;}
      r.participants=Object.keys(r.memberNames).length;
      return true;
    } else if(kind==='hunt') {
      if(!Array.isArray(p.choices)||p.choices.some(i=>!Number.isInteger(i)||i<0||i>=(r.definition.options||[]).length))return true;
      entry.choices=[...new Set(p.choices)];g.entries=g.entries.filter(e=>e.id!==id);g.entries.push(entry);
    } else if(kind==='agreements') {
      const items=(r.definition.options||[]).concat(g.entries.map(e=>e.text));
      if(Number.isInteger(p.item)&&p.item>=0&&p.item<items.length&&['agree','concern'].includes(p.vote)){g.votes[id]||={};g.votes[id][p.item]=p.vote;}
      else if(entry.text&&g.entries.length<100)g.entries.push(entry);
    } else if(kind==='three') {
      if(entry.text&&g.entries.length<3&&!g.entries.some(e=>e.id===id))g.entries.push(entry);
      if(g.entries.length===3){r.phase=2;r.deadlineMs=0;}
    } else if(kind==='gallery') {
      if(!Number.isInteger(p.item)||p.item<0||p.item>=(r.definition.options||[]).length)return true;
      entry.item=p.item;entry.symbol=['★','?','♥'].includes(p.symbol)?p.symbol:'★';
      if(entry.text&&g.entries.length<500)g.entries.push(entry);
    } else if(kind==='teach') {
      if(g.stage!=='prepare'||!entry.text)return true;
      const group=g.groups.findIndex(group=>group.ids.includes(id));if(group<0)return true;
      entry.item=group;g.entries=g.entries.filter(e=>e.id!==id);g.entries.push(entry);
    } else if(entry.text&&g.entries.length<500) {
      if(kind==='fame'&&entry.target!=='Everyone'&&!Object.values(r.memberNames).includes(entry.target))return true;
      g.entries.push(entry);
    }
    r.participants=new Set(g.entries.map(e=>e.id)).size;
    return true;
  }
  function publicState(r) {
    const g=init(r),named=!!r.definition.named;
    if(r.definition.kind==='nominate'){
      const members=Object.entries(r.memberNames||{}).filter(([,name])=>name&&name!=='Presenter').map(([identity,displayName])=>({identity,displayName}));
      const eligible=new Set(members.map(member=>member.identity));
      const ballots=g.entries.filter(entry=>eligible.has(entry.id)&&(entry.skip||eligible.has(entry.target)));
      const result={members,submitted:ballots.length,total:members.length};
      if(r.phase>=3){
        result.nominations=members.map(member=>{
          const votes=ballots.filter(entry=>!entry.skip&&entry.target===member.identity);
          return {...member,count:votes.length,...(named?{voters:votes.map(entry=>r.memberNames[entry.id])}:{})};
        }).filter(item=>item.count).sort((a,b)=>b.count-a.count||a.displayName.localeCompare(b.displayName));
        const skipped=ballots.filter(entry=>entry.skip);result.skipped=skipped.length;
        if(named)result.skipVoters=skipped.map(entry=>r.memberNames[entry.id]);
      }
      return result;
    }
    // Identities are needed only for the explicitly named turn/group activities.
    const hidden=['gallery','fame','hunt'].includes(r.definition.kind)&&r.phase<3;
    const galleryCounts=(r.definition.options||[]).map((_,i)=>g.entries.filter(e=>e.item===i).length);
    return {...g,galleryCounts,members:Object.entries(r.memberNames||{}).map(([identity,displayName])=>({identity,displayName})),expected:r.phase>=3?(r.definition.options||[]).flatMap((item,i)=>item.startsWith('*')?[i]:[]):[],entries:hidden?[]:g.entries.map(({event,id,name,...entry})=>({...entry,name:named?name:''})),votes:Object.values(g.votes)};
  }
  function next(r) {
    if(r.definition.kind==='deducer'||r.definition.kind==='finalanswer'){
      syncDeducerGroups(r);init(r).stage='done';r.phase=3;r.deadlineMs=0;r.pausedRemainingMs=0;return true;
    }
    if(r.definition.kind==='pressure'){
      if(r.phase===1){r.phase=3;r.deadlineMs=0;r.pausedRemainingMs=0;}
      return true;
    }
    if(r.definition.kind==='chosen') {
      const g=init(r);
      if(r.phase!==1||g.stage==='done')return true;
      const ids=Object.keys(r.memberNames||{}).filter(id=>r.memberNames[id]);
      r.deadlineMs=0;r.pausedRemainingMs=0;
      if(!ids.length)return true;
      const requested=Math.max(1,Math.min(1000,Math.floor(Number(r.definition.chosenCount)||1)));
      g.chosen=shuffle(ids).slice(0,requested).map(id=>({identity:id,displayName:r.memberNames[id]}));
      g.poolSize=ids.length;g.stage='done';r.phase=3;r.participants=ids.length;
      return true;
    }
    if(r.definition.kind==='ball') {
      const g=init(r);
      if(g.stage==='join') {
        if(!g.volunteers.length){r.deadlineMs=0;return true;}
        g.speaker=shuffle(g.volunteers)[0];g.volunteers=g.volunteers.filter(id=>id!==g.speaker);g.stage='play';r.phase=1;
      } else {g.stage='done';r.phase=3;}
      r.deadlineMs=0;r.pausedRemainingMs=0;return true;
    }
    if(r.definition.kind!=='teach')return false;
    const g=init(r);
    if(g.stage==='join') {
      const ids=shuffle(Object.keys(r.memberNames||{}));if(!ids.length)return true;
      const count=Math.max(1,Math.floor(ids.length/3));g.groups=Array.from({length:count},(_,i)=>({ids:[],names:[],topic:(r.definition.options||[])[i%(r.definition.options||[]).length]||r.definition.prompt}));
      ids.forEach((id,i)=>{g.groups[i%count].ids.push(id);g.groups[i%count].names.push(r.memberNames[id]);});g.stage='prepare';r.deadlineMs=Date.now()+(r.definition.joinSeconds||180)*1000;
    } else if(g.stage==='prepare') {g.stage='teach';g.turn=0;r.deadlineMs=Date.now()+(r.definition.discussionSeconds||60)*1000;}
    else if(g.stage==='teach'&&g.turn+1<g.groups.length){g.turn++;r.deadlineMs=Date.now()+(r.definition.discussionSeconds||60)*1000;}
    else {g.stage='done';r.phase=3;r.deadlineMs=0;}
    r.pausedRemainingMs=0;return true;
  }
  function renderDeducer(root,r,{presenter,identity,draft,send}) {
    if(!document.getElementById('kn-deducer-style')){
      const style=document.createElement('style');style.id='kn-deducer-style';style.textContent=`
        .kn-deducer-boards{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,280px),1fr));gap:18px}
        .kn-deducer-group{min-width:0;border-top:4px solid var(--activity-accent,#8de5bb)!important;border-radius:12px;background:#15222d}
        .kn-deducer-members{font-size:12px;color:#bdcbd7;overflow-wrap:anywhere}
        .kn-deducer-list{list-style:none;display:grid;gap:10px;padding:0;margin:0}
        .kn-deducer-entry{display:grid;grid-template-columns:28px minmax(0,1fr);gap:10px;align-items:start;padding:14px;border:1px solid #526779;border-radius:8px;background:#101923}
        .kn-deducer-entry>span:first-child{color:#ffd166}.kn-deducer-entry strong{white-space:pre-wrap;overflow-wrap:anywhere}
        .kn-deducer-entry small{display:block;color:#b1becb;margin-top:6px}
        .kn-deducer-tools{grid-column:2;display:flex;gap:6px;flex-wrap:wrap}.kn-deducer-tools button{padding:6px 10px!important;min-height:36px}
        .kn-deducer-entry[draggable=true]{cursor:grab}.kn-deducer-entry.is-dragging{border-color:#ffe49b;box-shadow:0 0 0 2px #ffd166}
        .kn-deducer-entry.is-drop-target{border-top:4px solid #ffd166}.kn-deducer-empty{padding:22px;border:1px dashed #607588;border-radius:8px;color:#becdd8}
        .kn-deducer-form{display:grid;gap:10px}.kn-deducer-count{color:#8de5bb;font-size:13px}.kn-deducer-status{font-size:12px;color:#ffd166}
      `;document.head.appendChild(style);
    }
    const single=r.definition.kind==='finalanswer';
    const g=r.game||{},groups=g.groups||[],entries=g.entries||[],open=r.phase===1&&(!r.deadlineMs||Date.now()<r.deadlineMs),reveal=r.phase>=3;
    const own=groups.findIndex(group=>(group.ids||[]).includes(identity)),limit=r.definition.kind==='finalanswer'?1:Number(r.definition.maxEntries)||5;
    const el=(tag,text,parent)=>{const node=document.createElement(tag);if(text!==undefined)node.textContent=text;parent.appendChild(node);return node;};
    const boards=el('div',undefined,root);boards.className='kn-deducer-boards';
    if(!presenter&&!reveal&&own<0){el('p',single?'Ask the presenter to add you to a group. Your group’s answer will appear here.':'Finding your group… Your shared list will appear here.',boards);return;}
    for(const [index,group] of groups.entries()){
      if(!presenter&&!reveal&&index!==own)continue;
      const editable=!presenter&&open&&index===own,items=entries.filter(entry=>entry.item===index);
      const section=el('section',undefined,boards);section.className='kn-deducer-group';section.dataset.group=String(index);
      el('h3',group.label+(index===own?' · Your group':''),section);
      el('p',(group.names||[]).join(' · ')||'Waiting for members',section).className='kn-deducer-members';
      el('p',single?(items.length?'Answer submitted':'One shared answer per group'):items.length+' / '+limit+' entries'+(items.length>=limit?' · List full':''),section).className='kn-deducer-count';
      const status=el('p','',section);status.className='kn-deducer-status';status.setAttribute('role','status');
      const submit=async payload=>{try{await send(payload);}catch(error){status.textContent=error.message;throw error;}};
      const list=el('ol',undefined,section);list.className='kn-deducer-list';
      for(const [position,entry] of items.entries()){
        const row=el('li',undefined,list);row.className='kn-deducer-entry';row.dataset.entryId=entry.entryId;
        el('span',single?'✓':String(position+1).padStart(2,'0'),row);const text=el('div',undefined,row);el('strong',entry.text,text);el('small',entry.name||'',text);
        if(!editable)continue;
        const tools=el('div',undefined,row);tools.className='kn-deducer-tools';
        const action=(label,aria,payload,disabled=false)=>{const button=el('button',label,tools);button.type='button';button.setAttribute('aria-label',aria);button.disabled=disabled;button.onclick=async()=>{button.disabled=true;try{await submit(payload);}catch(_error){}finally{button.disabled=disabled;}};};
        if(single){action('Remove answer','Remove answer',{action:'remove',entryId:entry.entryId});continue;}
        action('↑','Move entry '+(position+1)+' up',{action:'move',entryId:entry.entryId,beforeId:items[position-1]?.entryId||''},position===0);
        action('↓','Move entry '+(position+1)+' down',{action:'move',entryId:entry.entryId,beforeId:items[position+2]?.entryId||''},position===items.length-1);
        action('Remove','Remove entry '+(position+1),{action:'remove',entryId:entry.entryId});
        row.draggable=true;
        row.ondragstart=event=>{event.dataTransfer.setData('text/plain',entry.entryId);event.dataTransfer.effectAllowed='move';row.classList.add('is-dragging');};
        row.ondragend=()=>{row.classList.remove('is-dragging');list.querySelectorAll('.is-drop-target').forEach(node=>node.classList.remove('is-drop-target'));};
        row.ondragover=event=>{event.preventDefault();row.classList.add('is-drop-target');};row.ondragleave=()=>row.classList.remove('is-drop-target');
        row.ondrop=event=>{event.preventDefault();event.stopPropagation();row.classList.remove('is-drop-target');submit({action:'move',entryId:event.dataTransfer.getData('text/plain'),beforeId:entry.entryId}).catch(()=>{});};
      }
      if(!items.length)el('p',single?(open?'Discuss together, then submit the idea you agree on.':'No answer submitted.'):(open?'Start with one idea. Build your deductions together.':'No entries submitted.'),section).className='kn-deducer-empty';
      if(!editable)continue;
      if(!single){
      const bottom=el('div','Drop here to move an entry to the end',section);bottom.className='kn-deducer-empty';
      bottom.ondragover=event=>event.preventDefault();bottom.ondrop=event=>{event.preventDefault();submit({action:'move',entryId:event.dataTransfer.getData('text/plain'),beforeId:''}).catch(()=>{});};
      }
      if(draft.deducerPending){
        const pending=draft.deducerPending,accepted=items.some(entry=>entry.requestId===pending.id);
        if(accepted){if(draft.deducerText===pending.text)draft.deducerText='';delete draft.deducerPending;}
        else if(items.length>=limit){status.textContent=single?'A teammate already submitted your group’s answer. Your draft is kept below.':'The list filled up. Your draft is kept below; remove an entry to make room.';delete draft.deducerPending;}
      }
      if(single&&items.length&&!draft.deducerText){
        el('p','Your group’s answer is in. To change it, remove the answer and submit a replacement before time runs out.',section).className='kn-deducer-members';
        continue;
      }
      const form=el('form',undefined,section);form.className='kn-deducer-form';
      const label=el('label',single?'Your group’s final answer':'Add to your group’s list',form),input=el('textarea',undefined,label);input.setAttribute('aria-label',single?'Group answer':'Group entry');input.maxLength=500;input.rows=3;
      input.value=draft.deducerText||'';input.placeholder=single?'The one idea your group agrees on…':'What can your group deduce?';input.oninput=()=>draft.deducerText=input.value;
      const add=el('button',single?'Submit final answer':'Add entry',form);add.type='submit';add.className='kn-primary';add.disabled=items.length>=limit;
      form.onsubmit=async event=>{event.preventDefault();const value=input.value.trim();if(!value)return;
        add.disabled=true;const pending=draft.deducerPending?.text===value?draft.deducerPending:{id:crypto.randomUUID(),text:value};draft.deducerPending=pending;
        try{await submit({action:'add',text:value,requestId:pending.id});status.textContent=single?'Sent — waiting for your group’s answer to update.':'Sent — waiting for your group’s list to update.';}catch(_error){}finally{add.disabled=items.length>=limit;}
      };
      el('p',single?'One answer for the whole group. Any teammate can remove it and submit a replacement before time runs out.':'Everyone in your group can add, remove and reorder entries. Drag to reorder, or use ↑ and ↓.',section).className='kn-deducer-members';
    }
  }
  function renderNominate(root,r,{presenter,identity,draft,send}) {
    if(!document.getElementById('kn-nominate-style')){
      const style=document.createElement('style');style.id='kn-nominate-style';style.textContent=`
        .kn-nominate-list{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,210px),1fr));gap:8px;max-height:340px;max-height:min(42vh,340px);overflow:auto;padding:4px;overscroll-behavior:contain}
        .keynope-workshop .kn-nominate-choice{display:flex;align-items:center;gap:10px;padding:14px;border:1px solid #59616a;border-radius:8px;background:#17212b;cursor:pointer;overflow-wrap:anywhere;min-width:0}
        .keynope-workshop .kn-nominate-choice:has(:checked){border-color:#ffd166;background:#39321e}
        .keynope-workshop .kn-nominate-choice:focus-within{outline:2px solid #ffd166;outline-offset:1px}
        .keynope-workshop .kn-nominate-choice input{width:18px!important;flex:none;accent-color:#ffd166}
        .kn-nominate-actions{display:flex;gap:10px}.kn-nominate-actions button{flex:1}.kn-nominate-status{color:#8de5bb;min-height:1.5em}
        .kn-nominate-results{display:grid;gap:12px}.kn-nominate-result{background:#17212b;border-radius:8px}
        .kn-nominate-bar{height:12px;background:#313b46;border-radius:3px;overflow:hidden}.kn-nominate-bar span{display:block;height:100%;background:#ffd166}
      `;document.head.append(style);
    }
    const g=presenter&&!r.readOnly?publicState(r):r.game||{},members=g.members||[],open=r.phase===1&&!r.completedReview&&(!r.deadlineMs||Date.now()<r.deadlineMs);
    const el=(tag,value,parent=root)=>{const node=document.createElement(tag);if(value!==undefined)node.textContent=value;parent.append(node);return node;};
    el('p',(g.submitted||0)+' / '+(g.total||members.length)+' ballots submitted');
    el('p',r.definition.named?'Voter names shown on reveal.':'Voter names hidden.');
    if(r.phase>=3){
      const results=g.nominations||[],max=Math.max(1,...results.map(item=>item.count));
      const list=el('div');list.className='kn-nominate-results';
      for(const item of results){
        const row=el('section',undefined,list);row.className='kn-nominate-result';
        el('h3',item.displayName+' · '+item.count+' '+(item.count===1?'vote':'votes'),row);
        const bar=el('div',undefined,row);bar.className='kn-nominate-bar';el('span',undefined,bar).style.width=(100*item.count/max)+'%';
        if(r.definition.named)el('p','Voted by: '+(item.voters||[]).join(', '),row);
      }
      if(!results.length)el('p','No nominations were submitted.',list);
      el('p','Skipped: '+(g.skipped||0),list);
      if(r.definition.named&&g.skipVoters?.length)el('p','Skipped by: '+g.skipVoters.join(', '),list);
      return;
    }
    if(presenter||r.readOnly||!identity||!open){el('p',open?'Voting is open. The distribution stays hidden until reveal.':'Voting is closed. Waiting for reveal.');return;}
    const candidates=members.filter(member=>member.identity!==identity&&member.displayName!=='Presenter').sort((a,b)=>a.displayName.localeCompare(b.displayName));
    if(!candidates.some(member=>member.identity===draft.nominee))draft.nominee='';
    const list=el('div');list.className='kn-nominate-list';list.setAttribute('role','radiogroup');list.setAttribute('aria-label','Choose a participant');
    const status=el('p');status.className='kn-nominate-status';status.setAttribute('role','status');
    const actions=el('div');actions.className='kn-nominate-actions';
    const vote=el('button','Vote',actions),skip=el('button','Skip',actions);vote.type=skip.type='button';vote.className='kn-primary';
    const update=()=>{vote.disabled=!draft.nominee||!!draft.nominatePending;skip.disabled=!!draft.nominatePending;};
    for(const member of candidates){
      const label=el('label',undefined,list);label.className='kn-nominate-choice';
      const radio=el('input',undefined,label);radio.type='radio';radio.name='nominate-person';radio.value=member.identity;radio.checked=draft.nominee===member.identity;
      el('span',member.displayName,label);radio.onchange=()=>{draft.nominee=member.identity;update();};
    }
    if(!candidates.length)el('p','Waiting for other participants. You can still skip.',list);
    const feedback=()=>{
      update();const submitted=draft.nominateSubmitted;
      list.querySelectorAll('input').forEach(input=>input.checked=input.value===draft.nominee);
      if(submitted==='skip')status.textContent='You skipped. You may still change your vote.';
      else if(submitted){const name=candidates.find(member=>member.identity===submitted)?.displayName;status.textContent=name?'Your vote: '+name+'. You may change it until voting closes.':'Your previous nominee has left. Please vote again or skip.';}
    };
    draft.nominateNotify=feedback;feedback();
    const submit=async abstain=>{
      if(draft.nominatePending||(!abstain&&!draft.nominee))return;
      const target=draft.nominee;draft.nominatePending=true;update();
      try{await send(abstain?{skip:true}:{nominee:target});draft.nominateSubmitted=abstain?'skip':target;if(abstain)draft.nominee='';
      }catch(error){status.textContent='Could not send your vote: '+error.message;}
      finally{draft.nominatePending=false;draft.nominateNotify?.();}
    };
    vote.onclick=()=>submit(false);skip.onclick=()=>submit(true);update();
  }
  function render(root,r,{presenter=false,identity='',draft={},send=async()=>{},changed=()=>{}}={}) {
    const kind=r.definition.kind;if(!has(kind))return false;
    if(!document.getElementById('keynope-workshop-style')){
      const style=document.createElement('style');style.id='keynope-workshop-style';style.textContent='.keynope-workshop{display:flex;flex-direction:column;gap:12px;min-width:0}.keynope-workshop section{border:1px solid #59616a;padding:14px;display:flex;flex-direction:column;gap:10px;min-width:0}.keynope-workshop p,.keynope-workshop h3,.keynope-workshop h4{margin:0;overflow-wrap:anywhere;white-space:pre-wrap}.keynope-workshop textarea,.keynope-workshop select,.keynope-workshop input:not([type=checkbox]){box-sizing:border-box;max-width:100%;width:100%;background:#101419;color:#eee;border:1px solid #59616a;padding:10px;font:inherit}.keynope-workshop textarea{min-height:76px}.keynope-workshop button{padding:10px;border:1px solid #59616a;background:#1d242b;color:#eee;font:inherit;cursor:pointer}.keynope-workshop button:hover{border-color:#ffcf55}.keynope-workshop button:disabled{opacity:.5}.keynope-workshop label{display:block}';document.head.appendChild(style);
    }
    root.classList.add('keynope-workshop');
    if(presenter)init(r);
    if(kind==='nominate'){renderNominate(root,r,{presenter,identity,draft,send});return true;}
    if(kind==='deducer'||kind==='finalanswer'){renderDeducer(root,r,{presenter,identity,draft,send});return true;}
    const g=r.game||{entries:[],votes:[],volunteers:[],spoken:[],groups:[],stage:'join'},members=r.members||g.members||Object.entries(r.memberNames||{}).map(([identity,displayName])=>({identity,displayName}));
    const reveal=r.phase>=3,open=r.phase===1&&(!r.deadlineMs||Date.now()<r.deadlineMs);
    const element=(tag,value,parent=root)=>{const el=document.createElement(tag);if(value!==undefined)el.textContent=value;parent.appendChild(el);return el;};
    const button=(label,action,parent=root)=>{const b=element('button',label,parent);b.type='button';if(/^(SUBMIT|SEND|VOLUNTEER)/.test(label))b.classList.add('kn-primary');b.onclick=async()=>{b.disabled=true;try{await action();}catch(e){element('p',e.message);}finally{b.disabled=false;}};return b;};
    const name=id=>members.find(m=>m.identity===id)?.displayName||'Participant';
    if(kind==='shuffle'){
      if(!reveal){element('p','The presenter will mix the existing groups while keeping each group’s size.');return true;}
      if(g.stage==='reverted')element('h3','Original groups restored');
      const grid=element('div');grid.className='kn-shuffle-groups';grid.style.cssText='display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,230px),1fr));gap:16px';
      for(const group of g.groups||[]){
        const card=element('section',undefined,grid),own=(group.ids||group.members||[]).includes(identity);
        card.style.cssText='border-radius:12px;border-top:4px solid '+(own?'#ffcf55':'#70b7ff')+';background:#15222d;padding:20px';
        element('h3',group.label,card);if(own)element('strong','Your group',card);
        const list=element('ul',undefined,card);list.style.cssText='padding-left:20px;line-height:1.7';
        for(const person of group.names||[])element('li',person,list);
        if(!(group.names||[]).length)element('p','No members',card);
      }
      if(!presenter)element('p','Open Pairing channel to talk with your group.');
      return true;
    }
    const field=(label,key,parent=root)=>{element('label',label,parent);const input=element('textarea',undefined,parent);input.setAttribute('aria-label',label);input.maxLength=500;input.dataset.gameField=key;input.value=draft[key]||'';input.oninput=()=>draft[key]=input.value;return input;};
    const addText=(label,payload={})=>{const input=field(label,'gameText');button('SUBMIT',async()=>{if(!input.value.trim())return;await send({...payload,text:input.value});draft.gameText='';input.value='';element('p','Received.');});};
    const entries=()=>{for(const e of g.entries||[])element('p',(e.name?e.name+': ':'')+(e.symbol?e.symbol+' ':'')+(e.target?e.target+' — ':'')+e.text).className='kn-contribution';};
    if(kind==='chosen') {
      const requested=Math.max(1,Number(r.definition.chosenCount)||1);
      const stage=element('section');stage.className='kn-chosen-stage';
      if(reveal) {
        const winners=g.chosen||[];
        element('h3','The Chosen',stage);
        if(!presenter&&identity)element('p',winners.some(person=>person.identity===identity)?'You have been chosen!':'The draw is complete. Meet the chosen participants.',stage).className='kn-chosen-status';
        const list=element('div',undefined,stage);list.className='kn-chosen-names';
        for(const person of winners){const card=element('div',undefined,list);card.className='kn-chosen-name';element('span','★',card).setAttribute('aria-hidden','true');element('strong',person.displayName,card);}
        element('p',winners.length+' selected from '+(g.poolSize||0)+' participants.',stage);
        if(winners.length<requested)element('p','Requested '+requested+'; everyone available was selected.',stage);
      } else {
        element('h3','Who will be chosen?',stage);
        element('p',requested+' '+(requested===1?'person':'people')+' will be selected at random. No duplicates.',stage);
        element('p',members.length+' joined',stage);
        element('p',presenter?'Press Draw now when everyone is here, or let the timer finish.':members.some(person=>person.identity===identity)?'You are in the draw. Stay here for the reveal.':'Joining the draw…',stage);
      }
    } else if(kind==='ball') {
      const stage=element('div');stage.className='kn-ball-stage';const ball=element('div','●',stage);ball.style.cssText='font-size:64px;color:#ffcf55';element('h3',g.stage==='done'?'Ball Toss ended':g.speaker?name(g.speaker)+' has the ball':'Who would like the ball?',stage);
      if(open&&!presenter&&g.speaker!==identity){const b=button((g.volunteers||[]).includes(identity)?'Volunteered':'VOLUNTEER',()=>send({volunteer:true}));b.disabled=(g.volunteers||[]).includes(identity);}
      if(g.stage!=='done') {
        element('p','Volunteer pool: '+((g.volunteers||[]).map(name).join(', ')||'No volunteers yet'));
        if(open&&!presenter&&g.speaker===identity){
          element('p','You caught the ball. Speak, then choose who catches it next.');
          for(const id of g.volunteers||[])button('Toss to '+name(id),()=>send({target:id}));
          if(!(g.volunteers||[]).length)button('End the Ball toss',()=>send({end:true}));
        }
      }
    } else if(kind==='teach') {
      element('h3',({join:'Waiting for participants',prepare:'Prepare your teach-back',teach:'Teaching turn '+(g.turn+1),done:'Teach-backs complete'})[g.stage]);
      (g.groups||[]).forEach((group,i)=>{const area=element('section');if(g.stage==='teach'&&i===g.turn)area.style.border='2px solid #ffcf55';element('h4','Group '+(i+1)+': '+group.topic,area);element('p',group.names.join(', '),area);for(const e of g.entries.filter(e=>e.item===i))element('p',e.text,area);});
      if(!presenter&&g.stage==='prepare')addText('Share preparation notes with your group');
      if(presenter&&!reveal)button(g.stage==='join'?'Create groups & prepare':g.stage==='prepare'?'Start teach-backs':'Next group',()=>{next(r);changed();});
    } else if(kind==='gallery') {
      renderGallery(root,r,{presenter,draft,send});
    } else if(kind==='agreements') {
      const items=(r.definition.options||[]).concat((g.entries||[]).map(e=>e.text)),votes=Array.isArray(g.votes)?g.votes:Object.values(g.votes||{});
      items.forEach((item,i)=>{const area=element('section');element('h3',item,area);element('p',votes.filter(v=>v[i]==='agree').length+' agree · '+votes.filter(v=>v[i]==='concern').length+' concerns',area);
        if(open&&!presenter){button('AGREE',()=>send({item:i,vote:'agree'}),area);button('CONCERN',()=>send({item:i,vote:'concern'}),area);}
        if(open&&presenter){const input=element('input',undefined,area);input.value=item;button('Revise',()=>{const offset=(r.definition.options||[]).length;if(i<offset)r.definition.options[i]=input.value.trim();else g.entries[i-offset].text=input.value.trim();for(const vote of Object.values(g.votes))delete vote[i];changed();},area);}});
      if(open&&!presenter)addText('Propose another agreement');
    } else if(kind==='hunt') {
      const choices=new Set(draft.hunt||[]);element('h3',reveal?'Compare the findings':'Spot the relevant evidence');
      element('p',reveal?'Expected answers are marked below. Participant selections are listed separately.':'Read the challenge above. Select all findings that answer it, then press Submit findings. You can change your selection and submit again while collection is open.');
      (r.definition.options||[]).forEach((item,i)=>{const label=element('label');const box=element('input',undefined,label);box.type='checkbox';box.checked=choices.has(i);box.disabled=!open||presenter;box.onchange=()=>{box.checked?choices.add(i):choices.delete(i);draft.hunt=[...choices];};label.appendChild(document.createTextNode(item.replace(/^\*\s*/,'')));if(reveal)element('strong',((g.expected||[]).includes(i)||(presenter&&item.startsWith('*')))?' ✓ Expected':'',label);});
      if(open&&!presenter)button('SUBMIT FINDINGS',async()=>{await send({choices:[...choices]});element('p','Findings submitted. You can update them until collection closes.');});
      if(reveal)for(const e of g.entries||[])element('p',(e.name||'Participant')+': '+(e.choices||[]).map(i=>(r.definition.options[i]||'').replace(/^\*\s*/,'')).join(', '));
    } else if(kind==='three') {
      element('p',reveal?'Compare the three participant answers with the presenter’s explanation.':'Three different people answer the question before the presenter shares their explanation. One answer per person; collection closes after the third response.');
      element('h3',(g.entries||[]).length+' / 3 participant answers');entries();
      if(open&&!presenter)addText('How would you answer?');
      if(reveal)element('p','Presenter: '+((r.definition.options||[])[0]||'Discuss the answers together.'));
    } else if(kind==='fame') {
      if(reveal)entries();else if(open&&!presenter){const select=element('select');for(const n of ['Everyone',...members.map(m=>m.displayName)]){const option=element('option',n,select);option.value=n;}select.value=draft.fameTarget||'Everyone';select.onchange=()=>draft.fameTarget=select.value;const input=field('Your appreciation','fameText');button('SEND APPRECIATION',async()=>{if(!input.value.trim())return;await send({target:select.value,text:input.value});input.value='';draft.fameText='';element('p','Appreciation received.');});}else element('p','Appreciation will appear on reveal.');
    }
    return true;
  }
  const galleryViews=new WeakMap();
  function renderGallery(root,r,{presenter,draft,send}) {
    // Keep the presenter's review position across incoming responses and heartbeat redraws.
    if(presenter){if(!galleryViews.has(r))galleryViews.set(r,{});draft=galleryViews.get(r);}
    const review=r.phase>=3,open=r.phase===1&&(!r.deadlineMs||Date.now()<r.deadlineMs);
    const items=r.definition.options||[],g=r.game||{},entries=g.entries||[];
    const counts=presenter?items.map((_,i)=>entries.filter(e=>e.item===i).length):(g.galleryCounts||[]);
    const types=[{symbol:'★',label:'Strength',hint:'What works well? Be specific.',path:'m5 12 4 4L19 6'},
      {symbol:'?',label:'Question',hint:'What would you like the group to explain?',path:'M4 3h16v13H9l-5 5ZM10 7a2 2 0 0 1 4 0c0 2-2 1-2 3M12 13h.01'},
      {symbol:'♥',label:'Suggestion',hint:'What could make this better? Suggest a next step.',path:'M9 18h6M10 21h4M8 13a6 6 0 1 1 8 0l-1 3H9Z'}];
    const el=(tag,text,parent=root)=>{const node=document.createElement(tag);if(text!==undefined)node.textContent=text;parent.appendChild(node);return node;};
    const redraw=()=>{const intro=root.querySelector(':scope > .kn-activity-intro');root.replaceChildren();if(intro)root.appendChild(intro);renderGallery(root,r,{presenter,draft,send});};
    const btn=(label,action,parent=root)=>{const b=el('button',label,parent);b.type='button';b.onclick=action;return b;};
    const row=parent=>{const n=el('div',undefined,parent);n.style.cssText='display:flex;flex-wrap:wrap;gap:8px;align-items:center';return n;};
    const select=(label,values,value,change,parent=root)=>{const wrap=el('label',label,parent),s=el('select',undefined,wrap);s.setAttribute('aria-label',label);for(const [v,t] of values){const o=el('option',t,s);o.value=v;}s.value=String(value);s.onchange=()=>change(s.value);return s;};
    const badge=(type,parent)=>{
      const svg=document.createElementNS('http://www.w3.org/2000/svg','svg');svg.setAttribute('viewBox','0 0 24 24');svg.setAttribute('width','24');svg.setAttribute('height','24');svg.setAttribute('aria-hidden','true');
      const path=document.createElementNS(svg.namespaceURI,'path');path.setAttribute('d',type.path);path.setAttribute('fill','none');path.setAttribute('stroke','currentColor');path.setAttribute('stroke-width','2');path.setAttribute('stroke-linecap','round');path.setAttribute('stroke-linejoin','round');svg.appendChild(path);parent.prepend(svg);
    };
    el('h3',review?'Gallery review':'Explore the exhibits');
    el('p',review?'Browse by exhibit and feedback type. Open a card to read and discuss it together.':'Choose a project or idea below. Share a specific strength, question or suggestion. Contributions stay hidden until review.');
    el('p',counts.reduce((a,b)=>a+b,0)+' contributions collected');
    const controls=row(root);
    select('Exhibit',[[review?'-1':'',review?'All exhibits':'Choose an exhibit'],...items.map((item,i)=>[String(i),item+' ('+(counts[i]||0)+')'])],draft.galleryItem??(review?'-1':''),value=>{draft.galleryItem=value;draft.galleryPage=0;delete draft.galleryFocus;redraw();},controls);
    if(!review){
      if(presenter){const board=el('div');board.style.cssText='display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:12px';items.forEach((item,i)=>{const card=el('section',undefined,board);el('h4',item,card);el('p',(counts[i]||0)+' contributions',card);});return;}
      if(!open){el('p','Collection is closed. Feedback will appear when the presenter opens review.');return;}
      const index=Number(draft.galleryItem);if(draft.galleryItem===undefined||draft.galleryItem===''||!items[index]){
        const exhibits=el('div');exhibits.className='kn-gallery-exhibits';
        items.forEach((item,i)=>{const choice=btn('',()=>{draft.galleryItem=String(i);redraw();},exhibits);choice.setAttribute('aria-label','Explore '+item);el('span',String(i+1).padStart(2,'0'),choice);el('strong',item,choice);el('small','Explore exhibit →',choice);});return;
      }
      const card=el('section');el('h4',items[index],card);
      const choices=row(card),chosen=draft.galleryType||'★';
      for(const type of types){const b=btn(type.label,()=>{draft.galleryType=type.symbol;redraw();},choices);badge(type,b);b.style.cssText='display:flex;gap:8px;align-items:center';b.setAttribute('aria-pressed',String(chosen===type.symbol));if(chosen===type.symbol)b.style.borderColor='#ffcf55';}
      const label=el('label',types.find(t=>t.symbol===chosen).hint,card),input=el('textarea',undefined,label);input.maxLength=500;input.dataset.gameField='gallery'+index;input.value=draft['gallery'+index]||'';input.oninput=()=>{draft['gallery'+index]=input.value;submit.disabled=!input.value.trim();};
      const status=el('p','',card);status.setAttribute('role','status');
      const submit=btn('Submit feedback',async()=>{if(!input.value.trim())return;submit.disabled=true;try{await send({item:index,symbol:chosen,text:input.value});draft['gallery'+index]='';input.value='';status.textContent='Feedback received. Add another contribution or choose another exhibit.';}catch(error){status.textContent=error.message;}finally{submit.disabled=!input.value.trim();}},card);submit.disabled=!input.value.trim();return;
    }
    select('Feedback type',[['','All feedback'],...types.map(t=>[t.symbol,t.label])],draft.galleryFilter||'',value=>{draft.galleryFilter=value;draft.galleryPage=0;delete draft.galleryFocus;redraw();},controls);
    const filtered=entries.map((entry,i)=>({...entry,number:i+1})).filter(e=>(!draft.galleryItem||draft.galleryItem==='-1'||e.item===Number(draft.galleryItem))&&(!draft.galleryFilter||e.symbol===draft.galleryFilter));
    const pages=Math.max(1,Math.ceil(filtered.length/6)),page=Math.min(draft.galleryPage||0,pages-1);
    const nav=row(root);btn('Previous',()=>{draft.galleryPage=page-1;redraw();},nav).disabled=page===0;
    el('span','Page '+(page+1)+' of '+pages+' · '+filtered.length+' contributions',nav);
    btn('Next',()=>{draft.galleryPage=page+1;redraw();},nav).disabled=page+1>=pages;
    if(!filtered.length){el('p','No contributions match this view.');return;}
    const focused=filtered.find(e=>e.number===draft.galleryFocus);
    const showCard=(entry,parent,large)=>{const card=el('section',undefined,parent),type=types.find(t=>t.symbol===entry.symbol)||types[0];
      const heading=el('h4','#'+entry.number+' · '+type.label,card);badge(type,heading);el('p',items[entry.item],card);
      const comment=el('p',entry.text,card);comment.style.cssText=large?'font-size:clamp(22px,3vw,42px);line-height:1.4;max-height:45vh;overflow:auto':'max-height:8em;overflow:auto;line-height:1.5';
      if(r.definition.named&&entry.name)el('p',entry.name,card);
      return card;
    };
    if(focused){const card=showCard(focused,root,true),n=filtered.indexOf(focused),actions=row(card);btn('Back to cards',()=>{delete draft.galleryFocus;redraw();},actions);btn('Previous contribution',()=>{draft.galleryFocus=filtered[n-1].number;redraw();},actions).disabled=n===0;btn('Next contribution',()=>{draft.galleryFocus=filtered[n+1].number;redraw();},actions).disabled=n===filtered.length-1;}
    else {const board=el('div');board.style.cssText='display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,260px),1fr));gap:12px';for(const entry of filtered.slice(page*6,page*6+6)){const card=showCard(entry,board,false);btn('Read & discuss',()=>{draft.galleryFocus=entry.number;redraw();},card);}}
  }
  function definition(r) {return r.definition.kind==='hunt'?{...r.definition,options:(r.definition.options||[]).map(item=>item.replace(/^\*\s*/,''))}:r.definition;}
  const prerequisiteTimers=new WeakMap();
  function renderPrerequisites(root,r,{presenter=false,draft={},send=async()=>{}}={}){
    if(prerequisiteTimers.has(root))clearInterval(prerequisiteTimers.get(root));
    const g=r.game||{},items=r.definition.prerequisites||[],ended=r.phase>=3,grouping=!r.definition.disableGrouping;
    const el=(tag,text,parent=root)=>{const n=document.createElement(tag);if(text!==undefined)n.textContent=text;parent.appendChild(n);return n;};
    const fmt=ms=>{const cent=Math.floor(Math.max(0,ms)/10);return String(Math.floor(cent/6000)).padStart(2,'0')+':'+String(Math.floor(cent/100)%60).padStart(2,'0')+'.'+String(cent%100).padStart(2,'0');};
    const completionSummary=()=>{
      const finished=Number(g.finishedCount)||0,unfinished=Number(g.unfinishedCount)||0;
      el('p',finished+' finished · '+unfinished+' not finished').className='kn-prerequisite-tally';
      if(r.definition.named){
        const columns=el('div');columns.style.cssText='display:grid;grid-template-columns:repeat(auto-fit,minmax(min(200px,100%),1fr));gap:16px';
        for(const [label,names,count] of [['Finished',g.finishedNames||[],finished],['Not finished',g.unfinishedNames||[],unfinished]]){
          const section=el('section',undefined,columns);el('h3',label+' ('+count+')',section);
          if(names.length){const list=el('ul',undefined,section);for(const name of names)el('li',name,list);}else el('p','None',section);
        }
      }
    };
    if(ended&&!grouping){completionSummary();return;}
    const clock=el('div');clock.setAttribute('aria-label','Elapsed time');clock.style.cssText='font:700 32px monospace;font-variant-numeric:tabular-nums;margin:12px 0';
    const tally=el('p',(g.finishedCount||0)+' finished');
    if(!grouping){tally.hidden=true;completionSummary();}
    else if(!ended&&r.definition.named&&(g.finishedNames||[]).length)el('p',g.finishedNames.join(', '));
    let ranking=null,manualPage=null,lastPage=-1;
    if(ended){
      el('h3','Your balanced groups are ready');el('p','Completion order was used to pair earlier finishers with later finishers. Open Pairing channel to meet your group.');
      if(r.definition.named){
        const records=g.ranking||[],pages=Math.max(1,Math.ceil(records.length/10));
        const nav=el('div'),prev=el('button','Previous',nav),next=el('button','Next',nav),auto=el('button','Pause rotation',nav);ranking=el('ol');
        prev.onclick=()=>{manualPage=(lastPage-1+pages)%pages;lastPage=-1;tick();};next.onclick=()=>{manualPage=(lastPage+1)%pages;lastPage=-1;tick();};auto.onclick=()=>{manualPage=manualPage===null?Math.max(0,lastPage):null;auto.textContent=manualPage===null?'Pause rotation':'Resume rotation';tick();};
        if(!records.length)el('p','No completed checklists. Participants were grouped in random order.');
      }
    }else if(!presenter){
      el('p','Select a prerequisite to read its instructions. Check it off when done.');
      draft.checked=items.map((_,i)=>!!draft.checked?.[i]);draft.prerequisiteIndex=Math.min(items.length-1,Math.max(0,draft.prerequisiteIndex||0));
      const progress=el('progress');progress.className='kn-prerequisite-progress';progress.max=items.length;progress.setAttribute('aria-label','Checklist completion');
      const progressLabel=el('p');progressLabel.setAttribute('role','status');
      const list=el('div'),detail=el('section'),title=el('h3','',detail),instructions=el('p','',detail);instructions.style.whiteSpace='pre-wrap';
      list.className='kn-prerequisite-list';detail.className='kn-prerequisite-detail';
      const status=el('p','');status.setAttribute('role','status');
      const done=el('button','I AM FINISHED!');done.type='button';
      const closed=r.phase!==1||!!draft.finished||(r.deadlineMs&&Date.now()>=r.deadlineMs);
      const buttons=[];
      const refresh=()=>{const i=draft.prerequisiteIndex;title.textContent=items[i]?.title||'';instructions.textContent=items[i]?.instructions||'No additional instructions.';progress.value=draft.checked.filter(Boolean).length;progressLabel.textContent=progress.value+' of '+items.length+' steps completed';buttons.forEach((b,n)=>{b.setAttribute('aria-pressed',String(n===i));b.style.borderColor=n===i?'#ffcf55':'';});done.hidden=!draft.checked.every(Boolean)||!items.length;done.disabled=closed;};
      items.forEach((item,i)=>{const row=el('div',undefined,list);row.style.cssText='display:flex;gap:10px;align-items:center;margin:8px 0';const check=el('input',undefined,row);check.type='checkbox';check.checked=draft.checked[i];check.disabled=closed;check.setAttribute('aria-label','Completed: '+item.title);const button=el('button',item.title,row);button.type='button';button.style.cssText='padding:10px;border:1px solid #59616a;background:#1d242b;color:inherit';buttons.push(button);button.onclick=()=>{draft.prerequisiteIndex=i;refresh();};check.onchange=()=>{draft.checked[i]=check.checked;refresh();send({checked:draft.checked.slice()}).catch(error=>status.textContent=error.message);};});
      done.onclick=async()=>{done.disabled=true;try{const finishedClock=Date.now();await send({finished:true,checked:draft.checked.slice(),finishedClock});draft.finished=true;draft.finishedClock=finishedClock;status.textContent='Finished — waiting for everyone else.';for(const input of list.querySelectorAll('input'))input.disabled=true;}catch(error){status.textContent=error.message;done.disabled=false;}};
      if(draft.finished)status.textContent='Finished — waiting for everyone else.';
      refresh();
    }
    function tick(){
      clock.textContent=fmt((ended?g.stoppedAt||Date.now():draft.finishedClock||Date.now())-(g.startedAt||Date.now()));
      if(ranking){const records=g.ranking||[],pages=Math.max(1,Math.ceil(records.length/10));const page=manualPage??Math.floor(Math.max(0,Date.now()-(g.stoppedAt||Date.now()))/5000)%pages;if(page!==lastPage){lastPage=page;ranking.replaceChildren();ranking.start=page*10+1;for(const item of records.slice(page*10,page*10+10))el('li',item.name+' — '+fmt(item.elapsedMs),ranking);tally.textContent=(g.finishedCount||0)+' finished · Ranking page '+(page+1)+' / '+pages;}}
    }
    tick();const timer=setInterval(()=>{if(!root.isConnected){clearInterval(timer);return;}if(!document.hidden)tick();},100);prerequisiteTimers.set(root,timer);
  }
  // The same 8x8 digits as the presentation timer, without a test card.
  const pressureClocks=new WeakMap();
  function renderPressureClock(root,r){
    let entry=pressureClocks.get(root);
    if(!entry){
      const svg=document.createElementNS('http://www.w3.org/2000/svg','svg');svg.setAttribute('viewBox','0 0 48 12');svg.setAttribute('role','img');
      svg.style.cssText='display:block;width:100%;height:auto;opacity:.78';root.appendChild(svg);
      entry={svg,r};pressureClocks.set(root,entry);
      entry.timer=setInterval(()=>{if(!root.isConnected){clearInterval(entry.timer);pressureClocks.delete(root);return;}if(!document.hidden)paint();},250);
      const paint=()=>{
        const state=entry.r,paused=Number(state.pausedRemainingMs)||0;
        const remaining=state.phase===0?Number(state.definition.timerSeconds)||0:state.phase===1?Math.max(0,Math.ceil((paused||Number(state.deadlineMs)-Date.now()||0)/1000)):0;
        const value=String(Math.min(99,Math.floor(remaining/60))).padStart(2,'0')+':'+String(remaining%60).padStart(2,'0');
        const label=state.phase===0?'Ready':state.phase!==1||remaining===0?'Time is up':paused?'Paused':'Time remaining';
        const key=value+label;if(entry.key===key)return;entry.key=key;
        svg.setAttribute('aria-label',label+' '+value);svg.replaceChildren();
        const rect=(x,y,w,h,fill)=>{const el=document.createElementNS(svg.namespaceURI,'rect');for(const [k,v] of Object.entries({x,y,width:w,height:h,fill}))el.setAttribute(k,v);svg.appendChild(el);};
        rect(0,0,48,12,'rgba(0,0,0,.48)');
        [...value].forEach((ch,i)=>timerGlyphs[ch].forEach((row,y)=>[...row].forEach((pixel,x)=>{if(pixel==='█')rect(2+i*9+x,2+y,1,1,paused?'#ffd166':remaining?'#ff5555':'#ffffff');})));
      };
      entry.paint=paint;
    }
    entry.r=r;entry.paint();
  }
  return {names,has,init,receive,publicState,next,render,definition,renderPrerequisites,timerGlyphs,renderPressureClock,syncDeducerGroups};
})();

// Shared group model. A fresh epoch replaces the entire chat membership,
// including an empty assignment list when the last group is removed.
globalThis.KeynopeGroups = (() => {
  function normalize(groups, eligible) {
    const seen=new Set(),ids=new Set();
    return (groups||[]).map((group,index)=>{
      let id=String(group.id??index);while(ids.has(id))id+='-';ids.add(id);
      return {id,label:String(group.label||'Group '+(index+1)).slice(0,80),members:(group.members||[]).filter(member=>{
        if(typeof member!=='string'||seen.has(member)||(eligible&&!eligible.has(member)))return false;
        seen.add(member);return true;
      })};
    });
  }
  function fromRoom(room) {
    if(room?.groups)return normalize(room.groups);
    const groups=new Map();
    for(const item of room?.assignments||[]){const id=String(item.group);if(!groups.has(id))groups.set(id,{id,label:item.label||'Group '+(groups.size+1),members:[]});groups.get(id).members.push(item.identity);}
    return normalize([...groups.values()]);
  }
  function room(code,groups,names={},epoch=crypto.randomUUID()) {
    groups=normalize(groups);
    return {sessionCode:code,epoch,groups,names:{...names},assignments:groups.flatMap(group=>group.members.map(identity=>({identity,group:group.id,label:group.label,members:group.members.slice()})))};
  }
  function shuffled(groups) {
    groups=normalize(groups);const pool=groups.flatMap(g=>g.members);
    const occupied=groups.filter(g=>g.members.length);if(occupied.length<2)return groups;
    for(let attempt=0;attempt<16;attempt++){
      for(let i=pool.length-1;i>0;i--){const bytes=new Uint32Array(1),limit=4294967296-4294967296%(i+1);do{crypto.getRandomValues(bytes);}while(bytes[0]>=limit);const j=bytes[0]%(i+1);[pool[i],pool[j]]=[pool[j],pool[i]];}
      let offset=0;const result=groups.map(g=>({...g,members:pool.slice(offset,offset+=g.members.length)}));
      if(result.some((g,i)=>g.members.some(id=>!groups[i].members.includes(id))))return result;
    }
    // An extremely unlucky draw must still make a visible change.
    [occupied[0].members[0],occupied[1].members[0]]=[occupied[1].members[0],occupied[0].members[0]];
    return groups;
  }
  function publicRoom(value){if(!value)return value;const {assignments,...publicValue}=value;return {...publicValue,groups:fromRoom(value)};}
  return {normalize,fromRoom,room,shuffled,publicRoom};
})();
