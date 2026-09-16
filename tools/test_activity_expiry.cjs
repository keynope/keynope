const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('main.go','utf8');
let tick,publications=0,archives=0,session={};
const noop=()=>{};
const c=vm.createContext({console,Date,crypto:require('node:crypto').webcrypto,
 keynopeEngagementRuntime:null,keynopeRunningActivity:null,keynopeEngagementCountdownTick:0,keynopeEngagementOverlay:null,renderActiveActivityStatus:noop,
 setInterval:fn=>(tick=fn,1),clearInterval:noop,engagementSessionFor:()=>session,
 renderEngagementRuntime:noop,closeEngagementRuntime:noop,publishEngagementRuntime:(r=c.keynopeEngagementRuntime)=>{publications++;if(c.engagementHasCompleted(r))archives++;},
 publishHostedEngagementState:()=>Promise.resolve(),replacePairingRoom:noop,
 engagementRandomIndex:n=>0,sendImpostorRole:async()=>{},showEngagementToast:noop});
vm.runInContext(fs.readFileSync('web/activity-games.js','utf8'),c);
for(const name of ['engagementRuntimeAlive','engagementHasCompleted','engagementAcceptsResponse','expireEngagementRuntime','runEngagementCountdown','advanceEngagementRuntime','finishBalancedPairs','balancedCompletionGroups','assignEngagementGroups','finishPairShareDiscussion','distributePlayingCards','rebuildPlayingCardGroups','dealPlayingCards','revealImpostorRoles']){
 const start=source.indexOf((name==='revealImpostorRoles'?'async ':'')+'function '+name+'(');
 assert(start>=0,name);vm.runInContext(source.slice(start,source.indexOf('\n}',start)+2),c);
}
const kinds=['shuffle','deducer','finalanswer','pulse','storm','sort','dual','quiz','match','questions','wall','draw','introduction','expertise','cards','impostor','dots','finishpair','prerequisites','chosen','pressure','ball','gallery','hunt','teach','fame','agreements','three','pair','truefalse'];
kinds.push('nominate');
function runtime(kind){return {definition:{id:kind,kind,options:['A','B'],questions:[{},{}],groupCount:1,chosenCount:1},
 phase:1,deadlineMs:Date.now()-100,pausedRemainingMs:0,memberNames:{a:'Alex',b:'Bea'},
 counts:[1,2],attributions:[{displayName:'Alex',idea:'Keep me'}],entryResponses:[{id:'response',idea:'Keep me'}],
 finishedAt:{a:10},questionIndex:0,questionRevealed:false,
 game:{stage:'join',entries:[{text:'Keep this too'}],groups:[],volunteers:['a','b'],votes:{}}};}
(async()=>{
 for(const background of [false,true])for(const kind of kinds){
  const r=runtime(kind),preview={definition:{id:'preview',kind:'storm'},phase:0};c.keynopeRunningActivity=background?r:null;c.keynopeEngagementRuntime=background?preview:r;session={deadlineMs:r.deadlineMs};publications=archives=0;
  c.runEngagementCountdown(r);tick();await new Promise(setImmediate);
  assert(r.phase>=3,kind+' did not reveal');assert.equal(r.deadlineMs,0,kind);assert.equal(session.deadlineMs,0,kind);
  assert.equal(r.pausedRemainingMs,0);assert(archives>0,kind+' not archived');
  assert.equal(r.entryResponses[0].idea,'Keep me');assert.equal(r.attributions[0].idea,'Keep me');assert.equal(r.game.entries[0].text,'Keep this too');
  assert.equal(c.engagementAcceptsResponse(r,{idea:'late'}),false,kind+' accepts late edits');
  const count=publications;c.expireEngagementRuntime(r);assert.equal(publications,count,'expiry ran twice');
  if(background)assert.equal(c.keynopeEngagementRuntime,preview,'background expiry replaced the inspected activity');
 }
 c.keynopeRunningActivity=null;
 for(const kind of ['chosen','teach','ball','cards','impostor','deducer']){
  const r=runtime(kind);r.memberNames={};r.game.volunteers=[];c.keynopeEngagementRuntime=r;c.expireEngagementRuntime(r);await new Promise(setImmediate);
  assert(r.phase>=3,kind+' with no submissions stuck');assert.equal(r.deadlineMs,0);
 }
 for(const phase of [2,3]){const r=runtime('questions');r.phase=phase;c.keynopeEngagementRuntime=r;c.expireEngagementRuntime(r);assert.equal(r.phase,3);}
 const fact=runtime('truefalse');c.keynopeEngagementRuntime=fact;c.expireEngagementRuntime(fact);c.advanceEngagementRuntime();assert.equal(fact.questionIndex,1);assert(fact.questionRevealed,'Next reopened answer collection after expiry');
 const pair=runtime('pair');pair.phase=3;c.keynopeEngagementRuntime=pair;c.expireEngagementRuntime(pair);assert.equal(pair.phase,4);
 for(const stage of ['prepare','teach']){const r=runtime('teach');r.game.stage=stage;c.keynopeEngagementRuntime=r;c.expireEngagementRuntime(r);assert.equal(r.game.stage,'done');assert.equal(r.deadlineMs,0);}
 for(const flag of ['readOnly','paused','future','stale']){
  const r=runtime('pulse');c.keynopeEngagementRuntime=r;
  if(flag==='readOnly')r.readOnly=true;if(flag==='paused'){r.deadlineMs=0;r.pausedRemainingMs=5000;}
  if(flag==='future')r.deadlineMs=Date.now()+5000;if(flag==='stale')c.keynopeEngagementRuntime=runtime('pulse');
  c.expireEngagementRuntime(r);assert.equal(r.phase,1,flag+' expired unexpectedly');
 }
 const r=runtime('storm');r.deadlineMs=Date.now()+5000;assert(c.engagementAcceptsResponse(r,{}));r.deadlineMs=Date.now()-1;assert(!c.engagementAcceptsResponse(r,{}));
 r.definition.kind='questions';r.phase=2;r.deadlineMs=0;assert(c.engagementAcceptsResponse(r,{questionDots:[]}));
 r.definition.kind='pair';r.phase=3;assert(c.engagementAcceptsResponse(r,{chat:'hello'}));
 console.log('PASS: all 31 exercises finalize and archive on expiry; empty rooms, late-response lock, voting, pauses and read-only views.');
})().catch(e=>{console.error(e);process.exitCode=1});
