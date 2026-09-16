const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('main.go','utf8'),noop=()=>{};let now=1000,connections=0,presenting=true;
const c=vm.createContext({window:{},document:{querySelectorAll:()=>[]},Date:{now:()=>now},crypto:require('node:crypto').webcrypto,
 keynopeEngagementSessions:new Map(),keynopeEngagementRuntime:null,keynopeRunningActivity:null,keynopeEngagementOverlay:null,keynopeEngagementSessionEpoch:0,
 renderActiveActivityStatus:noop,clearTimeout:noop,confirmRunningActivity:()=>{throw Error('Unexpected conflict')},
 keynopeEngagementCountdownTick:0,keynopeRecentActivityItems:[],keynopeLobbyPresentation:null,keynopeHostedEngagementControllerSurface:true,
 deck:{pages:[]},pageIndex:0,clearInterval:noop,archiveEngagementResult:noop,renderActivityMarker:noop,renderEngagementRuntime:noop,
 publishEngagementRuntime:noop,runEngagementCountdown:noop,publishHostedEngagementState:()=>Promise.resolve(),
 engagementPresentationActive:()=>presenting,randomActivityCode:()=> 'Test1234',connected:()=>connections++});
for(const name of ['engagementHasCompleted','engagementIsRunning','engagementRuntimeAlive','detachEngagementRuntime','disposeEngagementRuntimes','onboardingSessionDefinition','engagementSessionFor','currentEngagementDefinition','openEngagementRuntime','activitySubmittedItems','closeEngagementRuntime','startEngagementRuntime','resetEngagementRuntime']){
 const start=source.indexOf('function '+name+'(');vm.runInContext(source.slice(start,source.indexOf('\n}',start)+2),c);
}
const start=source.indexOf('function startHostedEngagement('),end=source.indexOf('  keynopeActivityConnector()',start);
vm.runInContext(source.slice(start,end)+' connected();runtime.hostingConnection=false;\n}',c);
const kinds=['onboarding','shuffle','deducer','pulse','storm','sort','dual','quiz','match','questions','wall','draw','introduction','expertise','cards','impostor','dots','finishpair','prerequisites','chosen','pressure','ball','gallery','hunt','teach','fame','agreements','three','pair','truefalse'];
for(const kind of kinds){
 c.disposeEngagementRuntimes();c.keynopeEngagementSessions.clear();connections=0;now=1000;
 c.deck.pages=[{slide:0,engagement:{id:kind,kind,code:kind==='onboarding'?'Test1234':undefined,timerSeconds:60,joinSeconds:120,options:[]}}];
 c.openEngagementRuntime(false);let r=c.keynopeEngagementRuntime;
 assert.equal(r.phase,0,kind);assert.equal(r.deadlineMs,0);assert.equal(r.startedAt,0);assert.equal(connections,0,kind+' connected before Start');
 now=20000;c.closeEngagementRuntime(false);c.openEngagementRuntime(false);r=c.keynopeEngagementRuntime;
 assert.equal(r.phase,0);assert.equal(connections,0);
 c.startEngagementRuntime();assert.equal(r.phase,1);assert.equal(r.startedAt,now);
 assert.equal(r.deadlineMs,now+(['pair','cards','impostor'].includes(kind)?120000:60000));assert.equal(connections,1);
 const deadline=r.deadlineMs;c.startEngagementRuntime();assert.equal(connections,1);assert.equal(r.deadlineMs,deadline,'double Start reset timer');
 r.ideas=['Preserve me'];now+=1000;c.closeEngagementRuntime(false);now+=1000;c.openEngagementRuntime(false);r=c.keynopeEngagementRuntime;
 assert.equal(r.phase,1,kind+' did not resume');assert.equal(r.ideas[0],'Preserve me');assert.equal(r.deadlineMs,deadline,'Close paused or reset the deadline');
 if(kind!=='onboarding'){c.resetEngagementRuntime();assert.equal(r.phase,0);assert.equal(r.deadlineMs,0);assert.equal(r.startedAt,0);}
 c.closeEngagementRuntime(false);
}
c.disposeEngagementRuntimes();c.keynopeEngagementSessions.clear();presenting=false;connections=0;c.deck.pages=[{slide:0,engagement:{id:'preview',kind:'storm'}}];c.openEngagementRuntime(false);c.startEngagementRuntime();assert(c.keynopeEngagementRuntime.localPreview);assert.equal(connections,0);assert.equal(c.keynopeEngagementRuntime.deadlineMs,0);
console.log('PASS all 30 activities: Ready without timer/connection, explicit Start, double-click safety, resume, reset and local previews.');
