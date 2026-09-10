/* Keynope workshop activities. No participant data is persisted into decks. */
globalThis.KeynopeGames = (() => {
  const names = {ball:'Ball Toss',gallery:'Gallery Walk',hunt:'Data Hunt',teach:'Teach-Back',fame:'Wall of Fame',agreements:'Working Agreements',three:'Three Before Me'};
  const has = kind => Object.hasOwn(names,kind);
  const shuffle = values => {
    const result=values.slice();
    for(let i=result.length-1;i>0;i--){const bytes=new Uint32Array(1),limit=4294967296-4294967296%(i+1);do{crypto.getRandomValues(bytes);}while(bytes[0]>=limit);const j=bytes[0]%(i+1);[result[i],result[j]]=[result[j],result[i]];}
    return result;
  };
  function init(r) { return r.game ||= {entries:[],votes:{},volunteers:[],spoken:[],speaker:'',stage:'join',groups:[],turn:0}; }
  const text = value => String(value||'').trim().slice(0,500);
  function receive(r,event) {
    if(!has(r.definition.kind))return false;
    const g=init(r),p=event.payload.response||{},id=event.identity,kind=r.definition.kind;
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
    // Identities are needed only for the explicitly named turn/group activities.
    const hidden=['gallery','fame','hunt'].includes(r.definition.kind)&&r.phase<3;
    const galleryCounts=(r.definition.options||[]).map((_,i)=>g.entries.filter(e=>e.item===i).length);
    return {...g,galleryCounts,members:Object.entries(r.memberNames||{}).map(([identity,displayName])=>({identity,displayName})),expected:r.phase>=3?(r.definition.options||[]).flatMap((item,i)=>item.startsWith('*')?[i]:[]):[],entries:hidden?[]:g.entries.map(({event,id,name,...entry})=>({...entry,name:named?name:''})),votes:Object.values(g.votes)};
  }
  function next(r) {
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
  function render(root,r,{presenter=false,identity='',draft={},send=async()=>{},changed=()=>{}}={}) {
    const kind=r.definition.kind;if(!has(kind))return false;
    if(!document.getElementById('keynope-workshop-style')){
      const style=document.createElement('style');style.id='keynope-workshop-style';style.textContent='.keynope-workshop{display:flex;flex-direction:column;gap:12px;min-width:0}.keynope-workshop section{border:1px solid #59616a;padding:14px;display:flex;flex-direction:column;gap:10px;min-width:0}.keynope-workshop p,.keynope-workshop h3,.keynope-workshop h4{margin:0;overflow-wrap:anywhere;white-space:pre-wrap}.keynope-workshop textarea,.keynope-workshop select,.keynope-workshop input:not([type=checkbox]){box-sizing:border-box;max-width:100%;width:100%;background:#101419;color:#eee;border:1px solid #59616a;padding:10px;font:inherit}.keynope-workshop textarea{min-height:76px}.keynope-workshop button{padding:10px;border:1px solid #59616a;background:#1d242b;color:#eee;font:inherit;cursor:pointer}.keynope-workshop button:hover{border-color:#ffcf55}.keynope-workshop button:disabled{opacity:.5}.keynope-workshop label{display:block}';document.head.appendChild(style);
    }
    root.classList.add('keynope-workshop');
    if(presenter)init(r);
    const g=r.game||{entries:[],votes:[],volunteers:[],spoken:[],groups:[],stage:'join'},members=r.members||g.members||Object.entries(r.memberNames||{}).map(([identity,displayName])=>({identity,displayName}));
    const reveal=r.phase>=3,open=r.phase===1&&(!r.deadlineMs||Date.now()<r.deadlineMs);
    const element=(tag,value,parent=root)=>{const el=document.createElement(tag);if(value!==undefined)el.textContent=value;parent.appendChild(el);return el;};
    const button=(label,action,parent=root)=>{const b=element('button',label,parent);b.type='button';if(/^(SUBMIT|SEND|VOLUNTEER)/.test(label))b.classList.add('kn-primary');b.onclick=async()=>{b.disabled=true;try{await action();}catch(e){element('p',e.message);}finally{b.disabled=false;}};return b;};
    const name=id=>members.find(m=>m.identity===id)?.displayName||'Participant';
    const field=(label,key,parent=root)=>{element('label',label,parent);const input=element('textarea',undefined,parent);input.setAttribute('aria-label',label);input.maxLength=500;input.dataset.gameField=key;input.value=draft[key]||'';input.oninput=()=>draft[key]=input.value;return input;};
    const addText=(label,payload={})=>{const input=field(label,'gameText');button('SUBMIT',async()=>{if(!input.value.trim())return;await send({...payload,text:input.value});draft.gameText='';input.value='';element('p','Received.');});};
    const entries=()=>{for(const e of g.entries||[])element('p',(e.name?e.name+': ':'')+(e.symbol?e.symbol+' ':'')+(e.target?e.target+' — ':'')+e.text).className='kn-contribution';};
    if(kind==='ball') {
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
    const g=r.game||{},items=r.definition.prerequisites||[],ended=r.phase>=3;
    const el=(tag,text,parent=root)=>{const n=document.createElement(tag);if(text!==undefined)n.textContent=text;parent.appendChild(n);return n;};
    const fmt=ms=>{const cent=Math.floor(Math.max(0,ms)/10);return String(Math.floor(cent/6000)).padStart(2,'0')+':'+String(Math.floor(cent/100)%60).padStart(2,'0')+'.'+String(cent%100).padStart(2,'0');};
    const clock=el('div');clock.setAttribute('aria-label','Elapsed time');clock.style.cssText='font:700 32px monospace;font-variant-numeric:tabular-nums;margin:12px 0';
    const tally=el('p',(g.finishedCount||0)+' finished');
    if(!ended&&r.definition.named&&(g.finishedNames||[]).length)el('p',g.finishedNames.join(', '));
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
  return {names,has,init,receive,publicState,next,render,definition,renderPrerequisites};
})();
