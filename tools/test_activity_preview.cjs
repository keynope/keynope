const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('main.go','utf8'),noop=()=>{};
let mode='none',connections=0,writes=0;
const context=vm.createContext({window:{KEYNOPE_PRESENTER:true},keynopeAppSurface:true,
  keynopeEditorMasterMode:false,presenterPresenting:false,keynopeHostedEngagementControllerSurface:true,
  keynopeEngagementControllerSurface:true,document:{documentElement:{getAttribute:()=>mode},querySelectorAll:()=>[]},
  Date,crypto:require('node:crypto').webcrypto,clearInterval:noop,
  deck:{pages:[{slide:0,engagement:{id:'a',kind:'storm',timerSeconds:0}}]},pageIndex:0,
  keynopeEngagementRuntime:null,keynopeRunningActivity:null,keynopeEngagementOverlay:null,keynopeEngagementSessionEpoch:0,renderActiveActivityStatus:noop,
  keynopeEngagementCountdownTick:0,keynopeEngagementSessions:new Map(),keynopeRecentActivityItems:[],keynopeLobbyPresentation:null,
  keynopePairingRoom:{epoch:'live-group'},keynopeEngagementPublishChain:Promise.resolve(),
  archiveEngagementResult:noop,renderActivityMarker:noop,renderEngagementRuntime:noop,runEngagementCountdown:noop,
  randomActivityCode:()=> 'AbCd1234',fetch:async()=>{writes++;return {ok:true}},
  keynopeActivityConnector:()=>{connections++;throw Error('Preview must not connect');}});
for(const name of ['engagementIsRunning','engagementPresentationActive','engagementHasCompleted','onboardingSessionDefinition','engagementSessionFor','currentEngagementDefinition','openEngagementRuntime','activitySubmittedItems','closeEngagementRuntime','startHostedEngagement','publishEngagementRuntime','publishHostedEngagementState','replacePairingRoom']){
  const start=source.search(new RegExp('(?:async )?function '+name+'\\('));
  assert(start>=0,name);vm.runInContext(source.slice(start,source.indexOf('\n}',start)+2),context);
}
(async()=>{
  context.openEngagementRuntime(false);
  assert.equal(context.keynopeEngagementRuntime.localPreview,true);
  assert.equal(context.keynopeEngagementRuntime.hostingConnection,undefined);
  // Even a stale restored connection must not publish preview changes.
  context.keynopeEngagementRuntime.activityChannel={send:async()=>writes++,close:noop};
  await context.publishHostedEngagementState(true);
  context.replacePairingRoom({...context.keynopeEngagementRuntime,definition:{kind:'deducer'},pairAssignments:{}});
  assert.equal(context.keynopePairingRoom.epoch,'live-group');
  context.closeEngagementRuntime();
  await context.keynopeEngagementPublishChain;
  assert.equal(connections,0);assert.equal(writes,0);
  context.openEngagementRuntime(false);assert.equal(context.keynopeEngagementRuntime.localPreview,true);
  context.closeEngagementRuntime();
  for(const value of ['main','external']){mode=value;assert.equal(context.engagementPresentationActive(),true);}
  mode='none';context.presenterPresenting=true;
  assert.equal(context.engagementPresentationActive(),false,'editor mode wins over stale presenter polling');
  context.window.KEYNOPE_WEB_EDITOR=true;
  assert.equal(context.engagementPresentationActive(),false);
  context.window.keynopeLivePresentationWindow={closed:false};assert.equal(context.engagementPresentationActive(),true);
  context.window.keynopeLivePresentationWindow.closed=true;assert.equal(context.engagementPresentationActive(),false);
  console.log('PASS: editing previews never connect, publish or replace pairing chat; native/web presentation gates.');
})().catch(error=>{console.error(error);process.exitCode=1});
