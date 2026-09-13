const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const source = fs.readFileSync('main.go','utf8');
const noop = () => {};
let now = 1000;
const context = vm.createContext({Date:{now:()=>now},clearInterval:noop,crypto:require('node:crypto').webcrypto,engagementBase64URL:bytes=>Buffer.from(bytes).toString('base64url'),
  document:{querySelectorAll:()=>[]},keynopeEngagementSessions:new Map(),
  deck:{pages:[{slide:0,engagement:{id:'a',kind:'storm',timerSeconds:60}}]},pageIndex:0,
  keynopeEngagementRuntime:null,keynopeEngagementOverlay:null,keynopeEngagementSessionEpoch:0,
  keynopeEngagementCountdownTick:0,keynopeRecentActivityItems:[],keynopeLobbyPresentation:null,
  renderActivityMarker:noop,renderEngagementRuntime:noop,publishEngagementRuntime:noop,
  runEngagementCountdown:noop,startHostedEngagement:noop,randomActivityCode:()=> 'abcdefgh',
  publishHostedEngagementState:()=>Promise.resolve()});
for(const name of ['onboardingSessionDefinition','engagementSessionFor','currentEngagementDefinition','closeEngagementRuntime','openEngagementRuntime','resetEngagementRuntime']) {
  const start=source.indexOf('function '+name+'(');
  const end=source.indexOf('\n}',start)+2;
  vm.runInContext(source.slice(start,end),context);
}
context.openEngagementRuntime(false);
let r=context.keynopeEngagementRuntime;
r.phase=3;r.ideas=['Saved idea'];r.responseByIdentity={alice:{response:{idea:'Saved idea'}}};
r.questionIndex=2;r.questionRevealed=true;r.game={entries:['entry']};r.activation='original';
now=11000;
context.closeEngagementRuntime();
assert.equal(context.keynopeEngagementRuntime,null);
now=91000;
context.openEngagementRuntime(false);
r=context.keynopeEngagementRuntime;
assert.equal(r.phase,3);assert.equal(r.ideas[0],'Saved idea');assert.equal(r.activation,'original');
assert.equal(r.questionIndex,2);assert.equal(r.questionRevealed,true);assert.equal(r.game.entries[0],'entry');
assert.equal(r.deadlineMs,141000);
// Reconnection must not erase the restored state a second time.
context.keynopeHostedEngagementControllerSurface=true;
const connectionStart=source.indexOf('function startHostedEngagement()');
const connectionEnd=source.indexOf('  keynopeActivityConnector()',connectionStart);
vm.runInContext(source.slice(connectionStart,connectionEnd)+'\n}',context);
context.startHostedEngagement();
assert.equal(r.responseByIdentity.alice.response.idea,'Saved idea');
assert.equal(r.ideas[0],'Saved idea');
context.startHostedEngagement=noop;
r.deadlineMs=0;r.pausedRemainingMs=12345;
context.closeEngagementRuntime();context.openEngagementRuntime(false);
assert.equal(context.keynopeEngagementRuntime.pausedRemainingMs,12345);
assert.equal(context.keynopeEngagementRuntime.deadlineMs,0);
context.resetEngagementRuntime();context.closeEngagementRuntime();context.openEngagementRuntime(false);
r=context.keynopeEngagementRuntime;
assert.equal(r.phase,1);assert.equal(r.ideas.length,0);assert.equal(r.questionIndex,0);assert.equal(r.game,undefined);
context.closeEngagementRuntime();context.keynopeEngagementSessions.clear();context.openEngagementRuntime(false);
assert.equal(context.keynopeEngagementRuntime.phase,1);
console.log('PASS: close/reopen preserves answers, reveal, game state and timers; reset clears progress.');
