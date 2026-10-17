// Exercise production presenter refresh code without a window, network or app.
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('internal/app/main.go','utf8');
const start=source.indexOf('async function refreshPresenterDeck('),end=source.indexOf('let presenterStateSync = null;',start);
assert.ok(start>=0&&end>start);
const editorRefresh=source.match(/window\.keynopeRefreshEditorCanvasState=\(\)=>\{[^\n]+\};/)[0];
const context=vm.createContext({AbortSignal,URLSearchParams,console});
vm.runInContext(fs.readFileSync('web/workspace.js','utf8'),context);
vm.runInContext(fs.readFileSync('web/builds.js','utf8'),context);
vm.runInContext(fs.readFileSync('web/presentation-tools.js','utf8'),context);
vm.runInContext(`
  let deck={pages:[{slide:0,page:0,text:'old 0'},{slide:1,page:0,text:'old 1'}]},pageIndex=1;
  let presenterVersion=1,presenterDeckVersion=1,presenterPresenting=false;
  let remoteBuildCursor=null;
  let remotePresentationOverlay=null,overlaySyncCount=0,activitySyncCount=0;
  const presentationOverlayKey=value=>value?[KeynopePresentationTools.contextKey(value),value.revision].join(':'):'';
  function syncPresentationTools(){overlaySyncCount++;}
  let keynopeAppSurface=true,keynopeEditorMasterMode=false,keynopeEditorVisualResizeActive=false;
  let presenterMainSurface=false,presenterTimerMode='',presenterTimerInput='',presenterTimerEndMS=0,frame=12;
  let editorState={},paintCount=0,toolbarCount=0,animationClears=0,effectClears=0;
  let window={KEYNOPE_PRESENTER:true},state={version:2,deckVersion:2,deckSlide:-1,slide:1,page:0,presenting:false};
  let snapshot={version:2,pages:[{slide:0,page:0,text:'new 0'},{slide:1,page:0,text:'new 1'}]};
  let requests=[];
  const location={reload(){throw Error('Presenter refresh navigated away from the editor');}};
  const effectState={clear(){effectClears++;}};
  function clearContentAnimationCache(){animationClears++;}
  let animationInvalidations=[],effectInvalidations=[];
  function invalidateContentAnimationSlides(indices){animationInvalidations.push([...indices]);}
  function invalidateEffectSlides(indices){effectInvalidations.push([...indices]);}
  function syncEngagementRuntime(){activitySyncCount++;}
  function startPageTransition(){}
  function render(){paintCount++;}
  function renderEditorTopbar(){toolbarCount++;}
  ${editorRefresh}
  function refreshEditorPresenterControls(){}
  async function publishLobbyPresentation(){}
  async function fetch(url){requests.push(url);return {ok:true,json:async()=>JSON.parse(JSON.stringify(url.startsWith('/state')?state:snapshot))};}
  ${source.slice(start,end)}
`,context);
const run=code=>vm.runInContext(code,context);
(async()=>{
  await run('syncPresenterStateOnce()');
  assert.equal(run('deck.pages[1].text'),'new 1');
  assert.equal(run('pageIndex'),1);
  assert.equal(run('paintCount'),1);
  assert.equal(run('toolbarCount'),1);
  assert.equal(run('presenterDeckVersion'),2);
  assert.equal(run('requests.join(",")'),'/state,/slides?scenePatches=1&since=1');
  // Reject stale whole-deck data without replacing pages or advancing versions.
  run('state={...state,version:3,deckVersion:3};snapshot={version:2,pages:[{slide:0,page:0,text:"stale"}]};');
  await run('syncPresenterStateOnce()');
  assert.equal(run('deck.pages[1].text'),'new 1');assert.equal(run('presenterVersion'),2);assert.equal(run('paintCount'),1);
  // Reorder/remove pages with a current complete snapshot, keeping selected identity.
  run('snapshot={version:3,pages:[{slide:1,page:0,text:"retained"},{slide:0,page:0,text:"reordered"}]};');
  await run('syncPresenterStateOnce()');
  assert.equal(run('pageIndex'),0);assert.equal(run('deck.pages[0].text'),'retained');assert.equal(run('paintCount'),2);
  // A newly inserted slide not in the local deck also refreshes in place.
  run('snapshot={version:4,pages:[{slide:0,page:0},{slide:1,page:0},{slide:2,page:0,text:"inserted"}]};');
  assert.equal(await run('refreshPresenterSlide(2,4)'),true);
  assert.equal(run('deck.pages[2].text'),'inserted');assert.equal(run('pageIndex'),1);
  // Initial synchronization uses the same path and includes every page.
  run('presenterVersion=-1;state={...state,version:4,deckVersion:4,slide:2};');
  await run('syncPresenterStateOnce()');assert.equal(run('pageIndex'),2);assert.equal(run('paintCount'),3);
  // Same-version polls do not repaint or clear preparation caches.
  const before=run('animationClears');await run('syncPresenterStateOnce()');
  assert.equal(run('paintCount'),3);assert.equal(run('animationClears'),before);
  run('state={...state,version:5,buildCursor:{session:"proof",slideId:"slide-proof",step:1,generation:1,startedAtMs:Date.now(),motion:"advance",paused:false}};requests=[];');
  await run('syncPresenterStateOnce()');assert.equal(run('paintCount'),4);assert.equal(run('remoteBuildCursor.step'),1);assert.equal(run('requests.join(",")'),'/state');
  assert.equal(run('window.keynopeParticipantPosition.buildCursor.step'),1);
  await run('syncPresenterStateOnce()');assert.equal(run('paintCount'),4,'Same cursor restarted rendering');
  run('state={...state,version:6,buildCursor:null};');await run('syncPresenterStateOnce()');assert.equal(run('paintCount'),5);assert.equal(run('remoteBuildCursor'),null);
  const previewKey=run('window.keynopeParticipantPosition.previewKey');
  run(`state={...state,version:7,overlay:{session:'tools',slideId:'slide-proof',slide:2,page:0,revision:1,mode:'laser',color:'#ff3355',width:6,pointer:[.4,.5],strokes:[]}};requests=[];`);
  await run('syncPresenterStateOnce()');assert.equal(run('paintCount'),5,'Pointer update redrew the scene');assert.equal(run('requests.join(",")'),'/state');assert.equal(run('overlaySyncCount'),1);assert.equal(run('window.keynopeParticipantPosition.previewKey'),previewKey,'Pointer update invalidated preview');
  const publicationKey=run('window.keynopeParticipantPosition.key');run('state={...state,version:8,overlay:{...state.overlay,revision:2,pointer:[.6,.5]}};');await run('syncPresenterStateOnce()');assert.notEqual(run('window.keynopeParticipantPosition.key'),publicationKey);assert.equal(run('paintCount'),5);
  run(`state={...state,version:9,engagement:{phase:1,definition:{id:'activity-one',kind:'pulse'}}};requests=[];`);await run('syncPresenterStateOnce()');assert.equal(run('paintCount'),5,'Activity visibility redrew presentation');assert.equal(run('requests.join(",")'),'/state');
  run(`let previewPages=[{slide:0,page:0,scene:{slideId:'a',build:{version:1,steps:[{targetIds:['x']},{targetIds:['y']}]}}},{slide:1,page:0,tabOnly:true,scene:{slideId:'tab'}},{slide:2,page:0,scene:{slideId:'b'}}];let previewPosition={slide:0,page:0,buildCursor:{session:'proof',slideId:'a',step:0,generation:1,startedAtMs:0,motion:'seek',paused:false}};`);
  assert.equal(run('KeynopeBuilds.nextState(previewPages,previewPosition).step'),1);
  run('previewPosition.buildCursor.step=1;');assert.equal(run('KeynopeBuilds.nextState(previewPages,previewPosition).step'),2);
  run('previewPosition.buildCursor.step=2;');assert.equal(run('KeynopeBuilds.nextState(previewPages,previewPosition).page.slide'),2);assert.equal(run('KeynopeBuilds.nextState(previewPages,previewPosition).label'),'Next slide · 2');
  run('previewPosition={slide:2,page:0};');assert.equal(run('KeynopeBuilds.nextState(previewPages,previewPosition).ended'),true);
  assert.equal(run('KeynopeBuilds.nextState(previewPages,null)'),null);
  run('previewPosition={slide:0,page:0,buildCursor:{slideId:"stale"}};');assert.equal(run('KeynopeBuilds.nextState(previewPages,previewPosition).pending'),true);
  run('window.untouchedPage=deck.pages[0];snapshot={version:6,delta:true,baseVersion:4,replaceSlides:[1,2],pages:[{slide:1,page:0,text:"missed first update"},{slide:2,page:0,text:"latest update"}]};');
  const clearsBefore=run('animationClears');assert.equal(await run('refreshPresenterDeck(6)'),true);
  assert.equal(run('presenterDeckVersion'),6);assert.equal(run('deck.pages[0]===window.untouchedPage'),true);assert.equal(run('animationClears'),clearsBefore);
  assert.equal(run('JSON.stringify(animationInvalidations.at(-1))'),'[1,2]');assert.equal(run('JSON.stringify(effectInvalidations.at(-1))'),'[1,2]');
  assert.equal(run('deck.pages[1].text'),'missed first update');assert.equal(run('deck.pages[2].text'),'latest update');
  run('snapshot={...snapshot,version:7,baseVersion:5};');assert.equal(await run('refreshPresenterDeck(7)'),false);assert.equal(run('presenterDeckVersion'),6);
  run('snapshot={version:6,delta:true,baseVersion:6,pages:[],replaceSlides:[]};');assert.equal(await run('refreshPresenterDeck(6)'),true);assert.equal(run('deck.pages.length'),3);
  run('snapshot={version:7,delta:true,baseVersion:6,pages:[],replaceSlides:[1]};');assert.equal(await run('refreshPresenterDeck(7)'),false,'Partial deletion requires full recovery');
  run('snapshot={version:7,delta:true,baseVersion:6,pages:[{slide:1,page:0}],replaceSlides:[1,1]};');assert.equal(await run('refreshPresenterDeck(7)'),false,'Duplicate patch targets accepted');
  run('requests=[];window.keptPages=deck.pages;');
  await run(`syncPresenterStateOnce({revision:10,owner:'stale',current:()=>false})`);
  assert.equal(run('requests.length'),0);assert.equal(run('deck.pages===window.keptPages'),true);
  run(`let demandCurrent=true;const realFetch=fetch;fetch=async url=>{const result=await realFetch(url);demandCurrent=false;return result;};requests=[];`);
  await run(`syncPresenterStateOnce({revision:10,owner:'owner & one',current:()=>demandCurrent})`);
  assert.equal(run('requests.join(",")'),'/state?editorRevision=10&editorOwner=owner%20%26%20one');
  assert.equal(run('deck.pages===window.keptPages'),true,'Superseded state demand changed document');
  run('fetch=realFetch;snapshot={version:7,pages:[{slide:0,page:0,text:"stale document"}]};');
  assert.equal(await run('refreshPresenterDeck(7,()=>false)'),false);assert.equal(run('deck.pages===window.keptPages'),true,'Superseded page demand changed document');
  // Native object patches reuse the same validated decoder as WASM. Retain
  // unchanged object identity and apply all page replacements atomically.
  run(`
    deck.pages=[{slide:0,page:0,scene:{version:1,slideId:'objects',width:1920,height:1080,background:'#fff',objects:[{id:'keep',kind:'shape',paint:{color:'#fff'}},{id:'edit',kind:'text',text:{runs:[{text:'Before'}]}}]}}];
    pageIndex=0;presenterDeckVersion=10;window.keptObject=deck.pages[0].scene.objects[0];window.oldScene=deck.pages[0].scene;
    snapshot={version:11,delta:true,baseVersion:10,replaceSlides:[0],pages:[],patchedPages:[{slide:0,page:0,scenePatch:{version:1,slideId:'objects',order:['edit','keep','new'],changed:[{id:'edit',kind:'text',text:{runs:[{text:'After'}]}},{id:'new',kind:'shape'}],removed:[],metadata:{version:1,slideId:'objects',width:1920,height:1080,background:'#222'}}}]};
  `);
  assert.equal(await run('refreshPresenterDeck(11)'),true);
  assert.equal(run('deck.pages[0].scene.objects[1]===window.keptObject'),true);
  assert.equal(run('window.oldScene.objects[1].text.runs[0].text'),'Before');
  assert.equal(run('deck.pages[0].scene.objects[0].text.runs[0].text'),'After');
  assert.equal(run('deck.pages[0].scene.background'),'#222');
  assert.equal(run('deck.pages[0].scenePatch'),undefined);
  run(`
    window.keptPages=deck.pages;requests=[];
    const fetchBeforeRecovery=fetch;
    let replies=[{version:12,delta:true,baseVersion:11,replaceSlides:[0],pages:[],patchedPages:[{slide:0,page:0,scenePatch:{version:1,slideId:'wrong',order:[],changed:[],removed:[]}}]},{version:12,pages:[{slide:0,page:0,scene:{version:1,slideId:'recovered',width:1920,height:1080,objects:[]}}]}];
    fetch=async url=>{requests.push(url);return {ok:true,json:async()=>replies.shift()};};
  `);
  assert.equal(await run('refreshPresenterDeck(12)'),true);
  assert.equal(run('deck.pages[0].scene.slideId'),'recovered');
  assert.equal(run('requests.join(",")'),'/slides?scenePatches=1&since=11,/slides?scenePatches=1');
  run(`
    window.keptPages=deck.pages;requests=[];
    replies=[{version:13,delta:true,baseVersion:12,replaceSlides:[0],pages:[],patchedPages:[{slide:0,page:0,scenePatch:{version:1,slideId:'wrong',order:[],changed:[],removed:[]}}]},{version:12,delta:true,baseVersion:12,pages:[],replaceSlides:[]}];
  `);
  assert.equal(await run('refreshPresenterDeck(13)'),false,'Bad full recovery must not loop or advance local version');
  assert.equal(run('deck.pages===window.keptPages'),true);assert.equal(run('presenterDeckVersion'),12);assert.equal(run('requests.length'),2);
  run('fetch=fetchBeforeRecovery;requests=[];');
  assert.equal(await run('refreshPresenterDeck(13,()=>false)'),false);assert.equal(run('requests.length'),0,'Superseded owner fetched a recovery snapshot');
  console.log('PASS: full recovery, stale snapshots, coherent missed-version patches, retained pages/caches, insertion/reorder and initial sync without navigation');
})().catch(error=>{console.error(error);process.exitCode=1;});
