/* Shared workshop presentation language. No remote fonts, trackers or animation loops. */
globalThis.KeynopeActivityDesign = (() => {
  const activities = {
    nominate:['Nominate','Who gets your vote?','Choose another participant and press Vote, or Skip to abstain. You can change your choice until voting closes. Results appear on reveal.','gold','M4 11h16v10H4ZM8 3h8v11H8Zm2 5 2 2 3-4'],
    shuffle:['Shuffle','Meet a new mix of people','The presenter will shuffle the existing groups. Your new group will appear here, with its own Pairing channel.','blue','M3 6h4l10 12h4M3 18h4L17 6h4m-4-3 4 3-4 3m0 6 4 3-4 3'],
    finalanswer:['Final Answer','One group. One answer.','Discuss with your existing group and agree on one shared idea. Anyone in your group can submit it or remove it to make a replacement before time runs out.','mint','M4 4h16v16H4ZM7 12l3 3 7-7'],
    deducer:['Deducer','Think together. Narrow it down.','Work with your group to build a shared, ordered list of deductions before time runs out. Everyone can add, remove and reorder entries.','mint','M4 4h16v16H4ZM8 8h8M8 12h8M8 16h5'],
    pressure:['Pressure Cooker','Against the clock','Work on the task on the slide before the countdown reaches zero.','coral','M12 4a8 8 0 1 0 0 16 8 8 0 0 0 0-16ZM12 7v5l4 2M9 1h6'],
    chosen:['The Chosen','Waiting for the draw','You are in the draw. The presenter will randomly choose people from everyone who has joined.','gold','m12 3 3 6 7 1-5 5 1 7-6-3-6 3 1-7-5-5 7-1Z'],
    onboarding:['Onboarding','Make yourself at home','Join the conversation in Lobby. Your next activity will open here automatically.','mint','M4 6h16v11H8l-4 4ZM8 10h8M8 13h5'],
    pulse:['Pulse','Go with your gut','Choose the response that feels right. You can change it until responses close.','mint','M3 12h4l3-7 4 14 3-7h4'],
    storm:['Storm','One spark can start something','Send a short idea. Then another. There is room for more than one good thought.','gold','m13 2-9 12h7l-1 8 10-13h-8Z'],
    dots:['Dot Voting','Make your dots count','Explore the ideas, distribute your dots, then submit your votes.','violet','M8 7h.01M16 7h.01M12 16h.01'],
    sort:['Sort','Put ideas in their place','Drag cards into a box, or tap a card to move it to the next box. Submit when ready.','blue','M3 4h7v6H3ZM14 4h7v6h-7ZM3 14h7v6H3ZM14 14h7v6h-7'],
    dual:['Dual response','Look at both sides','Take a moment to reflect. Add your thoughts to either field, or both.','violet','M4 5h6v14H4ZM14 5h6v14h-6'],
    quiz:['Quiz','Put your thinking to the test','Choose an answer and move through the questions. Submit at the end.','blue','M5 3h14v18H5ZM8 7h8M8 11h8m-8 5 2 2 5-4'],
    truefalse:['Fact or Fiction','Trust your judgement','Choose Fact or Fiction and submit. The presenter reveals each answer before moving on.','gold','m3 7 3 3 5-6M14 5l7 7m0-7-7 7M4 17h16'],
    match:['Mix & Match','Connect the ideas','Select every category that fits each card. A card can belong to more than one.','blue','M3 5h5v5H3ZM16 14h5v5h-5ZM8 7h7v9h1'],
    questions:['Questions','Curiosity belongs here','Ask the questions you want discussed. After collection, vote for your priorities.','violet','M4 3h16v13H9l-5 5ZM9 7a3 3 0 0 1 6 0c0 2-3 2-3 4m0 2h.01'],
    wall:['Feedback Wall','Leave something useful','What worked? What could improve? What will you take away? Each field is optional.','mint','M3 4h8v7H3ZM14 4h7v7h-7ZM3 14h8v7H3ZM14 14h7v7h-7'],
    draw:['Quick Draw','A little pixel personality','Draw yourself with blocks. Choose a brush, then paint. Paint the same block again to erase.','gold','m4 17 12-12 3 3L7 20H4Zm10-10 3 3'],
    introduction:['Introduction','Build a face. Make it yours.','Choose features and colours, then share your portrait with the room.','mint','M4 4h16v16H4ZM8 9h.01M16 9h.01m-8 5h8'],
    pair:['Pair Share','A conversation starts with two','Joining is open. When pairing begins, you will see your partner and a space to chat.','blue','M3 4h8v10H7l-4 3ZM14 7h7v10h-3l-4 3Z'],
    expertise:['Expertise Map','Bring what you know','Type a skill or topic and press Enter. Each tag is shared as you add it.','violet','M12 3v5m0 8v5M3 12h5m8 0h5M8 8h8v8H8'],
    cards:['Playing Cards','Find your people','Join now. Cards are dealt when joining closes, then you will meet your group.','gold','M5 3h14v18H5Zm7 4-4 5 4 5 4-5Z'],
    impostor:['Impostor','Keep your screen to yourself','Join the mission. Your role is delivered privately when joining closes.','coral','M5 20V9a7 7 0 0 1 14 0v11h-5v-4h-4v4ZM8 7h8v5H8'],
    finishpair:['The Race','Ready. Set. Get it done.','Complete the task above. Press I am finished when you are ready.','gold','M6 22V3m0 0h13l-3 4 3 4H6'],
    prerequisites:['Prerequisites','Get ready, one step at a time','Select a step to see its instructions. Check it off when done, then press I AM FINISHED!','mint','M9 5h12M9 12h12M9 19h12M3 4l1 1 2-2M3 11l1 1 2-2M3 18l1 1 2-2'],
    ball:['Ball Toss','Pass the spotlight','Volunteer to speak. When you catch the ball, share your thought and choose the next speaker.','gold','M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18ZM5 5l14 14M5 19 19 5'],
    gallery:['Gallery Walk','Look closely. Leave a thought.','Choose an exhibit and add a strength, question or suggestion. Comments open on review.','violet','M3 4h8v7H3ZM14 4h7v16h-7ZM3 14h8v6H3'],
    hunt:['Data Hunt','Follow the evidence','Read the challenge, select the relevant findings, then submit your selection.','blue','M10 3a7 7 0 1 0 0 14 7 7 0 0 0 0-14Zm5 12 6 6'],
    teach:['Teach-Back','Learn it. Make it click.','Prepare with your group, then explain the idea in your own words when it is your turn.','gold','M3 4h18v13H3ZM12 17v5m-5 0 5-5 5 5M7 8h10M7 12h6'],
    fame:['Wall of Fame','Give someone their flowers','Choose a person and name something specific they brought to the workshop.','coral','m12 3 3 6 7 1-5 5 1 7-6-3-6 3 1-7-5-5 7-1Z'],
    agreements:['Working Agreements','Decide how you work together','Support an agreement or raise a concern. You can also propose one of your own.','mint','m3 12 5 5L21 4M4 4h7M3 21h18'],
    three:['Three Before Me','Let the room think first','Three different people answer before the presenter explains. One response per person.','violet','M5 5h14l-6 7 6 7H5']
  };
  // Every activity has its own conclusion. Keep wording independent of names
  // being visible: anonymous activities must make sense with counts alone.
  const revealCopy = {
    shuffle:['Meet your new group','Find your name and open Pairing channel. The presenter can revert to the original groups after this round.'],
    nominate:['The nominations are in','See how the votes were distributed. Skips are counted separately; nobody is automatically removed or assigned a role.'],
    finalanswer:['The final answers','One shared idea from each group. Compare your conclusions and explain the thinking behind them.'],
    deducer:['Your group deductions','Compare the groups’ ordered lists. What did you agree on, and where did your reasoning take different paths?'],
    pressure:['Time is up','The countdown has finished. Bring your task to a stopping point and follow the presenter’s next instructions.'],
    chosen:['The Chosen','The draw is complete. These people were selected.'],
    onboarding:['Your workshop room','Use the room tabs to follow the presentation, chat, and take part in activities.'],
    pulse:['The pulse of the room','Compare the responses to see where the room stands and where opinions differ.'],
    storm:['Ideas from the room','Review everyone’s ideas, spot common themes, and discuss which ones to explore further.'],
    dots:['Where the votes landed','Compare the vote totals and discuss which ideas deserve attention next.'],
    sort:['How the room sorted it','Compare the placements in each category. Discuss cards that landed in different places.'],
    dual:['Two sides of the reflection','Read the responses to both prompts. Look for connections, contrasts, and useful next steps.'],
    quiz:['Check your answers','Compare the responses with the correct answers and discuss the reasoning behind each one.'],
    truefalse:['Fact or Fiction: the answer','Check the revealed answer to this statement and discuss what makes it fact or fiction.'],
    match:['The connections you made','Compare which categories were linked to each card and discuss the different connections.'],
    questions:['Your questions, prioritised','Work through the questions in vote order, starting with the ones the room most wants answered.'],
    wall:['Feedback to take forward','Review the feedback under each heading. Discuss what to keep, what to improve, and what to take away.'],
    draw:['Meet the pixel people','Explore the self-portraits and the names beneath them. Find yours and introduce yourself.'],
    introduction:['A room full of characters','Put a name to each face and get to know the people in your workshop.'],
    pair:['Meet your discussion group','Find your partner or group, open Pairing channel, and discuss the prompt before time runs out.'],
    expertise:['The expertise in the room','Explore the skills and topics people shared. Look for knowledge you can exchange or build on.'],
    cards:['Find your card group','Find your name under a card rank, then open Pairing channel to meet your group.'],
    impostor:['Your secret role','Your role is shown only on your screen. Keep it hidden from the people around you.'],
    finishpair:['Your balanced groups','Earlier finishers have been paired with later finishers. Open Pairing channel to meet your group.'],
    prerequisites:['Ready to work together','The checklist round is complete. Find your balanced group and meet in Pairing channel.'],
    ball:['Ball Toss is complete','The round has ended. Take a moment to reflect on what people shared.'],
    gallery:['Explore the gallery feedback','Browse by exhibit and feedback type. Open a strength, question, or suggestion to discuss it with the room.'],
    hunt:['What the evidence tells us','Compare the selected findings with the expected answers. Discuss why each finding is relevant—or not.'],
    teach:['What we taught each other','Review the groups’ topics and notes. Share what you learned and clarify any remaining questions.'],
    fame:['Recognition from the room','Read the messages of appreciation and celebrate the contributions they recognise.'],
    agreements:['Agreements to work by','Review the support and concerns for each proposal. Discuss what needs to change before the group commits.'],
    three:['Three answers, then the explanation','Compare the three participant answers with the presenter’s explanation. What did each perspective add?']
  };
  const colours={mint:'#8de5bb',gold:'#ffd479',blue:'#91c9ff',violet:'#c3adff',coral:'#ffa595'};
  function mount(root,r,{presenter=false}={}) {
    if(!r?.definition)return;
    const [name,title,help,tone,path]=activities[r.definition.kind]||['Activity','Your turn','Follow the presenter’s instructions.','mint','M4 4h16v16H4Z'];
    root.classList.add('kn-activity');root.dataset.activityKind=r.definition.kind;root.dataset.activityId=r.definition.id||'';
    root.style.setProperty('--activity-accent',colours[tone]);
    const reveal=r.phase>=3||r.questionRevealed,locked=r.phase===2,voting=locked&&r.definition.kind==='questions';
    const intro=document.createElement('header');intro.className='kn-activity-intro';
    const icon=document.createElementNS('http://www.w3.org/2000/svg','svg');icon.setAttribute('viewBox','0 0 24 24');icon.setAttribute('aria-hidden','true');
    const line=document.createElementNS(icon.namespaceURI,'path');line.setAttribute('d',path);icon.append(line);
    const text=document.createElement('div'),eyebrow=document.createElement('div');eyebrow.className='kn-activity-eyebrow';eyebrow.textContent=name+' / '+(voting?'VOTE':reveal?'REVEAL':presenter?'HOST':locked?'PAUSED':'YOUR TURN');
    const heading=document.createElement('h2');heading.textContent=voting?'Choose what matters most':locked?'A moment to reflect':title;
    const description=document.createElement('p');description.textContent=voting?'Three dots. One per question. Submit the questions you want to explore first.':locked?'The presenter has closed responses. Stay here for the next step.':help;
    if(reveal){
      intro.classList.add('is-reveal');
      const copy=revealCopy[r.definition.kind]||[name+' results','Review the responses below.'];
      heading.textContent=copy[0];description.textContent=copy[1];
      if(r.definition.kind==='prerequisites'&&r.definition.disableGrouping){heading.textContent='Checklist completion';description.textContent='See how many participants finished and how many still have prerequisites to complete.';}
      if(r.definition.kind==='impostor'&&presenter){heading.textContent='Roles have been assigned';description.textContent='Participants can see their own role privately. Ask everyone to keep their screen hidden.';}
      if(r.definition.kind==='pair'){
        eyebrow.textContent=name+' / '+(r.phase>=4?'DONE':'DISCUSS');
        if(r.phase>=4){heading.textContent='Bring your discussion back';description.textContent='The discussion round has ended. Be ready to share a takeaway or question from your group.';}
      }
      if(['ball','teach','pressure'].includes(r.definition.kind))eyebrow.textContent=name+' / DONE';
    }
    if(r.definition.kind==='onboarding')eyebrow.textContent=name+' / LOBBY';
    if(r.definition.kind==='chosen')eyebrow.textContent='The Chosen / '+(reveal?'REVEAL':'WAITING FOR DRAW');
    text.append(eyebrow,heading,description);intro.append(icon,text);root.prepend(intro);
    return intro;
  }
  function install() {
    if(typeof document==='undefined'||document.getElementById('kn-activity-design'))return;
    const style=document.createElement('style');style.id='kn-activity-design';style.textContent=css;document.head.append(style);
  }
  const css=`
  .kn-activity{--activity-accent:#8de5bb;--activity-ink:#f3f5ee;--activity-muted:#b1becb;min-width:0;color:var(--activity-ink);line-height:1.55}
  .kn-activity-intro{display:flex;gap:18px;align-items:flex-start;margin:0 0 28px;padding:24px;border:1px solid #3d4d5b;border-left:4px solid var(--activity-accent);background:linear-gradient(125deg,#213039,#151d27 70%);border-radius:4px 18px 18px 4px}
  .kn-activity-intro>svg{flex:none;width:44px;height:44px;padding:10px;box-sizing:content-box;color:var(--activity-accent);background:#0f1822;border:1px solid #405361;border-radius:14px;fill:none;stroke:currentColor;stroke-width:1.6;stroke-linecap:round;stroke-linejoin:round}
  .kn-activity-intro>div{min-width:0}.kn-activity-eyebrow{font-size:11px;letter-spacing:.12em;font-weight:750;color:var(--activity-accent);margin-bottom:8px}
  .kn-activity .kn-activity-intro h2{margin:0 0 8px;color:#f5f7ef;font-size:clamp(21px,2.2vw,30px);line-height:1.22;letter-spacing:-.035em}
  .kn-activity .kn-activity-intro p{margin:0;color:var(--activity-muted);font-size:13px;max-width:64ch;line-height:1.7}
  .kn-activity-intro.is-reveal{padding:16px 20px;margin-bottom:20px}
  .kn-activity-intro.is-reveal>svg{width:30px;height:30px;padding:8px}
  .kn-activity :is(button,input,textarea,select):focus-visible{outline:3px solid var(--activity-accent);outline-offset:3px}
  .kn-activity :is(input,textarea,select){accent-color:var(--activity-accent)}
  .kn-activity :is(textarea,input:not([type=checkbox]):not([type=radio]),select){border:1px solid #536372;border-radius:9px;background:#101923;color:#f3f5ee;padding:13px;font:inherit;min-width:0;box-sizing:border-box}
  .kn-activity textarea{min-height:110px;resize:vertical;line-height:1.6}
  .kn-activity :is(button,.result,.idea,.column,.sort-zone,.dot-voting-card,.keynope-engagement-card){border-radius:10px}
  .kn-activity button{transition:border-color .12s,background .12s,box-shadow .12s;touch-action:manipulation}
  .kn-activity button:not(:disabled):hover{border-color:var(--activity-accent);box-shadow:0 0 0 1px var(--activity-accent)}
  .kn-activity button:not(:disabled):active{transform:translateY(1px)}
  .kn-activity button:disabled{cursor:default}
  .kn-activity :is(button.selected,button[aria-pressed=true],.has-votes){border-color:var(--activity-accent);background:#2a3d43;box-shadow:inset 0 0 0 1px var(--activity-accent)}
  .kn-activity :is(.response-fields>label,.quiz-question,.match-grid){padding:18px;background:#18232e;border:1px solid #3d4d5b;border-radius:12px;color:#e1e8ee}
  .kn-activity .response-fields>label>textarea{margin-top:12px}
  .kn-activity :is(.quiz-options,.match-grid){gap:12px}
  .kn-activity :is(.quiz-options,.match-grid)>label{display:flex;align-items:center;gap:12px;padding:16px;border:1px solid #4c5d6c;border-radius:8px;background:#101923;cursor:pointer}
  .kn-activity :is(.quiz-options,.match-grid)>label:has(input:checked){border-color:var(--activity-accent);background:#293a40}
  .kn-activity :is(input[type=radio],input[type=checkbox]){flex:none;width:20px;height:20px;min-height:20px;margin:0}
  .kn-activity :is(.choices,.keynope-engagement-pulse){display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:12px}
  .kn-activity :is(.choices,.keynope-engagement-pulse)>button{min-height:118px;display:flex;flex-direction:column;justify-content:center;align-items:center;gap:12px;background:#172531;color:#edf4f5;border:1px solid #536372;font-size:15px;font-weight:700;padding:16px}
  .kn-pulse-meter{display:flex;gap:4px;height:30px;align-items:end;pointer-events:none}
  .kn-pulse-meter i{width:8px;background:#4a5c68;display:block;border-radius:2px}
  .kn-pulse-meter i.on{background:var(--activity-accent)}
  .kn-activity :is(.idea,.keynope-engagement-card){padding:20px;background:#202b38;border:1px solid #526170;border-top:3px solid var(--activity-accent);white-space:pre-wrap}
  .kn-activity :is(.idea,.keynope-engagement-card)>strong{color:var(--activity-accent);font-size:12px;margin-bottom:9px}
  .kn-activity :is(.result,.keynope-engagement-attribution){position:relative;isolation:isolate;padding:18px;border:1px solid #435565;background:#17222e;overflow-wrap:anywhere;gap:16px}
  .kn-result-fill{position:absolute;z-index:-1;left:0;top:0;height:100%;background:#304951;border-right:2px solid var(--activity-accent);pointer-events:none}
  .kn-activity .result>strong{color:var(--activity-accent)}
  .kn-activity :is(.keynope-engagement-result,.keynope-engagement-result-idea){position:relative;isolation:isolate;overflow:hidden;border:1px solid #526170;border-radius:10px;padding:18px;background:#18232e;gap:16px;line-height:1.6;overflow-wrap:anywhere}
  .kn-activity .keynope-engagement-result-idea{border-top:3px solid var(--activity-accent);background:#202b38}
  .kn-activity .keynope-engagement-result>strong{color:var(--activity-accent)}
  .kn-activity :is(.results,.keynope-engagement-results).kn-idea-board{grid-template-columns:repeat(auto-fit,minmax(min(100%,260px),1fr));gap:16px}
  .kn-idea-board>:is(.idea,.keynope-engagement-result-idea){min-height:110px}
  .kn-review-nav{display:flex;flex-wrap:wrap;gap:10px;align-items:center;margin:18px 0 8px;padding:12px;background:#18232e;border:1px solid #536372;border-radius:10px}
  .kn-review-nav>span{flex:1;color:#cbd9e3;font-size:13px}.kn-review-nav button{min-height:44px;padding:8px 14px}
  .kn-activity [hidden]{display:none!important}
  .kn-activity .sort-zones{grid-template-columns:repeat(auto-fit,minmax(170px,1fr))}
  .kn-activity .sort-zone{background:#141f29;border:1px dashed #667481;min-height:160px;padding:14px}
  .kn-activity .sort-zone h3{font-size:13px;text-transform:uppercase;letter-spacing:.06em;color:var(--activity-accent)}
  .kn-activity .sort-item{width:100%;background:#263544;border:1px solid #617487;padding:15px;white-space:normal;text-align:left}
  .kn-activity .dot-voting-card{border:1px solid #536372;background:#18232e;padding:18px}
  .kn-activity .dot-voting-card p{color:#f0f4ef;font-size:15px;line-height:1.55}
  .kn-activity .dot-voting-toolbar{padding:16px;background:#101923;border:1px solid #435565;border-radius:12px}
  .kn-activity :is(.dot-voting-submit,button[type=submit],.submit-sort,.kn-primary){background:var(--activity-accent);color:#10212a;border:1px solid var(--activity-accent);font-weight:800;padding:14px 20px;min-height:48px}
  .kn-activity .waiting{position:relative;padding:40px 24px;border:1px dashed #647889;border-radius:16px;background:radial-gradient(ellipse at top,#223646,#141f29 75%);line-height:1.7;color:#d4e2ee;text-align:center}
  .kn-activity .waiting::before{content:'◆';display:block;color:var(--activity-accent);font-size:32px;margin-bottom:14px}
  .kn-activity .playing-card{border-radius:16px;border:2px solid #cdd7e2;box-shadow:5px 6px 0 #35424f;background:#f3f0e5;color:#192735}
  .kn-activity .playing-card.red{color:#b63245}
  .kn-activity .group-members,.kn-activity .card-group-members{line-height:1.8;color:#d9e7ed}
  .kn-activity .draw-grid{border:2px solid #657887;box-shadow:0 10px 30px #0004;margin:20px auto;background:#101923}
  .kn-activity .draw-grid>button{border-radius:0;padding:0;transition:none;touch-action:none}
  .kn-activity .draw-grid>button:hover{box-shadow:inset 0 0 0 1px var(--activity-accent)}
  .kn-activity .avatar-option-preview{pointer-events:none}
  .kn-activity .avatar-categories{gap:8px;flex-wrap:wrap}
  .kn-activity .avatar-options{max-height:360px;overflow-y:auto;overscroll-behavior:contain;padding:5px;scrollbar-color:#718794 #18232e}
  .kn-activity .avatar-builder-preview{border-radius:14px;background:radial-gradient(ellipse at center,#26333f,#111c26 70%);border-color:#596c7b}
  .kn-activity .avatar-colors button{border-radius:50%;box-shadow:none}
  .kn-activity .portrait{padding:18px;background:#18232e;border:1px solid #536372;border-radius:12px}
  .kn-activity .portrait figcaption{color:var(--activity-accent);margin-top:14px}
  .kn-activity .expertise-tags{gap:10px}.kn-activity .expertise-tags button{background:#293544;border-color:#637689;padding:9px 14px}
  .kn-activity.keynope-workshop section{border-radius:12px;border-color:#4b6070;background:#182530;padding:20px;gap:14px}
  .kn-activity.keynope-workshop button.kn-primary{background:var(--activity-accent);color:#10212a;border-color:var(--activity-accent);font-weight:800;min-height:48px}
  .kn-activity[data-activity-kind=hunt] label:has(input[type=checkbox]){display:flex;align-items:center;gap:14px;padding:18px;border:1px solid #4b6070;border-radius:10px;background:#182530;cursor:pointer}
  .kn-activity[data-activity-kind=hunt] label:has(input:checked){border-color:var(--activity-accent);background:#233943}
  .kn-chosen-stage{text-align:center;padding:24px!important;border:1px solid #b89750!important;border-radius:16px;background:radial-gradient(ellipse at top,#403620,#111c26 80%)}
  .kn-chosen-names{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,200px),1fr));gap:14px;width:100%}
  .kn-chosen-name{display:flex;align-items:center;justify-content:center;gap:12px;padding:20px;border:1px solid #ffd479;border-radius:12px;background:#302b20;overflow-wrap:anywhere;min-width:0;font-size:clamp(18px,3vw,30px)}
  .kn-chosen-name strong{min-width:0}.kn-chosen-name span,.kn-chosen-status{color:#ffd479}
  .kn-ball-stage{text-align:center;padding:24px;border:1px solid #5b6464;border-radius:16px;background:radial-gradient(ellipse at top,#3d382d,#17222e 75%)}
  .kn-ball-stage>div{line-height:1.2;text-shadow:3px 4px #655536;margin-bottom:12px}
  .kn-activity.keynope-workshop .kn-contribution{padding:22px;background:#202b38;border:1px solid #526170;border-top:3px solid var(--activity-accent);border-radius:12px;color:#eff4ed;line-height:1.7}
  .kn-activity .draw-brushes button{width:38px;min-height:38px}
  .kn-activity[data-activity-kind=finishpair] .finish-button{display:block;width:100%;min-height:72px;background:var(--activity-accent);color:#10212a;font-size:19px;font-weight:800;border-color:var(--activity-accent)}
  .kn-activity .impostor-reveal{border-radius:18px}
  .kn-gallery-exhibits{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,220px),1fr));gap:12px}
  .kn-gallery-exhibits>button{display:grid;gap:16px;text-align:left;padding:20px;min-height:155px;background:#202f3b!important}
  .kn-gallery-exhibits span{color:var(--activity-accent);font-size:28px;font-weight:800}.kn-gallery-exhibits strong{font-size:16px}.kn-gallery-exhibits small{color:#bfccd7;font-size:12px}
  .kn-activity .kn-prerequisite-list{display:grid;gap:8px;margin:18px 0}
  .kn-prerequisite-list>div{padding:10px;border:1px solid #4b5d6c;border-radius:10px;background:#18232e}
  .kn-prerequisite-list button{flex:1;text-align:left}
  .kn-prerequisite-detail{padding:20px;border:1px solid #617886;border-left:4px solid var(--activity-accent);border-radius:10px;background:#202f3b;margin-bottom:16px}
  .kn-prerequisite-progress{width:100%;height:8px;accent-color:var(--activity-accent);margin:10px 0}
  .kn-activity[data-activity-kind=prerequisites] ol{list-style-position:inside;padding:0;display:grid;gap:8px}
  .kn-activity[data-activity-kind=prerequisites] li{padding:14px;border:1px solid #4b5d6c;border-radius:8px;background:#18232e;font-variant-numeric:tabular-nums}
  .keynope-engagement-board{border:1px solid #596f7e;border-top:3px solid #8de5bb;border-radius:16px;background:#111c26;box-shadow:0 30px 100px #0009;padding:28px;gap:20px}
  .keynope-engagement-head{align-items:center;flex-wrap:wrap}.keynope-engagement-head h1{line-height:1.2;letter-spacing:-.035em}
  .keynope-engagement-phase{padding:8px 12px;border-radius:30px;background:#203242;color:#b9deff}
  .keynope-engagement-controls{position:sticky;bottom:-28px;background:#111c26;border-top:1px solid #405261;padding:16px 0;z-index:2;gap:8px;flex-wrap:wrap}
  .keynope-engagement-controls button{min-height:44px;border-radius:8px;padding:10px 16px}
  .keynope-engagement-controls button.primary{background:#8de5bb;border-color:#8de5bb;color:#10212a;font-weight:800}
  .keynope-engagement-join{border-radius:10px;background:#182632;padding:14px}
  .keynope-engagement-submitted{font-size:13px;color:#b1becb;border-top:1px solid #405261;padding-top:16px}
  .keynope-engagement-qr{margin:0 auto 24px;box-shadow:0 0 0 8px white;max-width:100%}
  @media(max-width:600px){.kn-activity-intro{padding:18px 14px;gap:12px;margin-bottom:20px}.kn-activity-intro>svg{width:24px;height:24px;padding:7px;border-radius:8px}.kn-activity .kn-activity-intro h2{font-size:21px}.kn-activity .kn-activity-intro p{font-size:12px}.kn-activity :is(.choices,.keynope-engagement-pulse){grid-template-columns:repeat(2,minmax(0,1fr))}.kn-activity .sort-zones{grid-template-columns:repeat(2,minmax(0,1fr))}.kn-activity .sort-zone{padding:10px}.kn-activity .sort-item{font-size:12px}.kn-activity .result{flex-wrap:wrap}.keynope-engagement-board{padding:18px}.keynope-engagement-controls{bottom:-18px}.kn-activity-intro .kn-activity-eyebrow{font-size:10px}.kn-activity :is(.quiz-options,.match-grid)>label{min-height:48px;padding:12px}}
  @media(prefers-reduced-motion:reduce){.kn-activity *{animation:none!important;transition:none!important}.kn-activity .marquee-track{transform:none!important}.kn-activity .marquee{overflow-x:auto}}
  `;
  function pulseMeter(button,index,total) {
    const meter=document.createElement('span');meter.className='kn-pulse-meter';meter.setAttribute('aria-hidden','true');
    for(let i=0;i<total;i++){const bar=document.createElement('i');bar.style.height=(8+i*4)+'px';bar.className=i<=index?'on':'';meter.append(bar);}button.prepend(meter);
  }
  function results(container,r){
    const kind=r.definition.kind,rows=Array.from(container.children);
    if(kind==='storm')container.classList.add('kn-idea-board');
    let counts=null;
    if(kind==='pulse')counts=(r.definition.options||[]).map((_,i)=>(r.attributions||[]).filter(v=>Number(v.choice)===i).length);
    if(kind==='dots')counts=(r.definition.options||[]).map((_,i)=>Number(r.counts?.[i])||0).sort((a,b)=>b-a);
    if(kind==='questions')counts=(r.attributions||[]).map(q=>Number(q.votes)||0).sort((a,b)=>b-a);
    if(counts){const max=Math.max(1,...counts);rows.forEach((row,i)=>{const fill=document.createElement('span');fill.className='kn-result-fill';fill.setAttribute('aria-hidden','true');fill.style.width=(100*(counts[i]||0)/max)+'%';row.prepend(fill);});}
    // Readable at workshop scale. Never move pages while someone is reading.
    if(rows.length<=12)return;
    const nav=document.createElement('nav');nav.className='kn-review-nav';nav.setAttribute('aria-label','Result pages');
    const label=document.createElement('span');label.setAttribute('role','status');
    const prev=document.createElement('button'),next=document.createElement('button');prev.type=next.type='button';prev.textContent='Previous results';next.textContent='Next results';
    const pages=Math.ceil(rows.length/12);
    const update=()=>{const page=Math.max(0,Math.min(pages-1,Number(r.visualReviewPage)||0));r.visualReviewPage=page;rows.forEach((row,i)=>row.hidden=Math.floor(i/12)!==page);label.textContent=(page*12+1)+'–'+Math.min(rows.length,(page+1)*12)+' of '+rows.length;prev.disabled=!page;next.disabled=page===pages-1;};
    prev.onclick=()=>{r.visualReviewPage--;update();};next.onclick=()=>{r.visualReviewPage++;update();};nav.append(label,prev,next);container.before(nav);update();
  }
  install();
  return {mount,install,pulseMeter,results,activities};
})();
