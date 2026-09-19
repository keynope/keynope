const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('main.go','utf8'),noop=()=>{},stored=new Map();
const context=vm.createContext({window:{keynopeStoreActivityResult:async(id,result)=>{stored.set(id,result)},keynopeActivityResult:id=>stored.get(id)},
  document:{querySelectorAll:()=>[]},deck:{pages:[]},pageIndex:0,Date,crypto:require('node:crypto').webcrypto,
  keynopeEngagementSessions:new Map(),keynopeEngagementRuntime:null,keynopeRunningActivity:null,keynopeEngagementOverlay:null,keynopeEngagementSessionEpoch:0,renderActiveActivityStatus:noop,
  keynopeEngagementCountdownTick:0,keynopeRecentActivityItems:[],keynopeLobbyPresentation:null,
  clearInterval:noop,renderActivityMarker:noop,renderEngagementRuntime:noop,publishEngagementRuntime:noop,engagementPresentationActive:()=>true,
  runEngagementCountdown:noop,startHostedEngagement:noop,randomActivityCode:()=> 'AbCd1234',
  showEngagementToast:message=>{throw Error(message)},publishHostedEngagementState:()=>Promise.resolve()});
vm.runInContext(source.slice(source.indexOf('let keynopeActivityResultWrites ='),source.indexOf('function engagementHasCompleted(')),context);
for(const name of ['engagementIsRunning','engagementHasCompleted','archiveEngagementResult','queueActivityResultWrite','onboardingSessionDefinition','engagementSessionFor','currentEngagementDefinition','activitySubmittedItems','closeEngagementRuntime','openEngagementRuntime','resetEngagementRuntime']){
  const start=source.indexOf('function '+name+'(');vm.runInContext(source.slice(start,source.indexOf('\n}',start)+2),context);
}
const flush=()=>vm.runInContext('keynopeActivityResultWrites',context);
(async()=>{
  const kinds=require('./activity_test_catalog.cjs').filter(kind=>kind!=='onboarding');
  for(const kind of kinds){
    context.keynopeEngagementSessions.clear();
    const definition={id:kind,kind,questions:[{prompt:'First'},{prompt:'Last'}],options:['One','Two'],timerSeconds:60};
    context.deck.pages=[{slide:0,engagement:definition}];context.openEngagementRuntime(false);
    const runtime=context.keynopeEngagementRuntime;
    Object.assign(runtime,{phase:kind==='pair'?4:kind==='truefalse'?1:3,counts:[2,1],ideas:['A saved idea'],
      attributions:[{displayName:'Alex',idea:'A saved idea',drawing:['█▀'],choices:[0,1]}],memberNames:{alex:'Alex'},
      participants:1,questionIndex:1,questionRevealed:true,game:{stage:'done',entries:[],votes:{},volunteers:[],groups:[],chosen:[{identity:'alex',displayName:'Alex'}]},
      roleAssignments:{alex:'crew'},cardAssignments:{alex:{rank:'A'}},groups:[{name:'Pair 1',members:['Alex','Bea']}],
      privateKey:'DO NOT SAVE',rolePublicKeys:{alex:{key:'DO NOT SAVE'}},pairMessages:[{text:'Private chat'}]});
    context.archiveEngagementResult(runtime);await flush();
    const result=stored.get(kind);assert(result,kind+' not archived');
    assert.equal(result.state.phase,kind==='pair'?4:3);assert(!JSON.stringify(result).includes('DO NOT SAVE'));assert(!JSON.stringify(result).includes('Private chat'));
    context.closeEngagementRuntime();await flush();context.keynopeEngagementSessions.clear();context.keynopeEngagementRuntime=context.keynopeRunningActivity=null;
    context.openEngagementRuntime(false);
    const restored=context.keynopeEngagementRuntime;
    assert.equal(restored.completedReview,true,kind);assert(restored.phase>=3,kind);assert.equal(restored.deadlineMs,0,kind);
    assert.equal(restored.ideas[0],'A saved idea');assert.equal(restored.attributions[0].displayName,'Alex');assert.equal(restored.questionIndex,1);
    context.resetEngagementRuntime();await flush();assert.equal(stored.get(kind),null,kind+' reset retained results');
    assert.equal(restored.phase,0);assert.equal(restored.completedReview,false);assert.equal(restored.attributions.length,0);
    context.closeEngagementRuntime();context.keynopeEngagementRuntime=context.keynopeRunningActivity=null;
  }
  for(const kind of ['onboarding','pair','truefalse','pulse']){
    const runtime={definition:{id:'incomplete-'+kind,kind,questions:[{},{}]},phase:kind==='pair'?3:1,questionIndex:0,questionRevealed:true};
    context.archiveEngagementResult(runtime);await flush();assert(!stored.has(runtime.definition.id),'incomplete '+kind+' saved as final');
  }
  console.log(`All ${kinds.length} non-lobby activities: archive, final-screen restore, reset, credentials/chat exclusion; incomplete activities remain live.`);
})().catch(error=>{console.error(error);process.exitCode=1;});
