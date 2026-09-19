const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('main.go','utf8'),noop=()=>{};
let now=100000,conflicts=0,connections=0;
const sessions=new Map();
const c=vm.createContext({Date:{now:()=>now},window:{},keynopeEngagementRuntime:null,keynopeRunningActivity:null,
  keynopePairingRoom:{sessionCode:undefined,groups:[{members:['a']}]},keynopeEngagementSessions:sessions,deck:{pages:[]},pageIndex:0,
  engagementSessionFor:d=>{if(!sessions.has(d.id))sessions.set(d.id,{});return sessions.get(d.id)},
  onboardingSessionDefinition:()=>null,confirmRunningActivity:()=>conflicts++,detachEngagementRuntime:noop,
  engagementPresentationActive:()=>false,renderEngagementRuntime:r=>c.rememberEngagementResume(r),
  publishEngagementRuntime:noop,runEngagementCountdown:noop,startHostedEngagement:()=>connections++,
  publishHostedEngagementState:()=>Promise.resolve()});
for(const name of ['engagementHasCompleted','engagementIsRunning','engagementDisplayState','rememberEngagementResume','reopenEngagementRuntime']){
 const start=source.indexOf('function '+name+'(');assert(start>=0,name);vm.runInContext(source.slice(start,source.indexOf('\n}',start)+2),c);
}
function activity(kind,timerSeconds=120){return {definition:{id:kind,kind,timerSeconds,questions:[{},{}]},phase:1,
  deadlineMs:now+90000,memberNames:{a:'Alex'},ideas:['Keep me'],game:{stage:'join',entries:[{text:'Keep me too'}]},
  finishedAt:{a:123},questionIndex:1,groups:[],attributions:[],questionVotes:{a:['q1']}};}
const kinds=require('./activity_test_catalog.cjs');
for(const kind of kinds){
 const r=activity(kind);c.keynopeEngagementRuntime=c.keynopeRunningActivity=r;
 if(kind==='questions')r.phase=2;
 if(kind==='pair')r.phase=3;
 if(kind==='ball')r.game.stage='play';
 if(kind==='teach')r.game.stage='teach';
 c.rememberEngagementResume(r);now+=15000;
 r.completedReview=true;r.phase=kind==='pair'?4:3;r.deadlineMs=0;r.game.stage='done';
 c.rememberEngagementResume(r);
 assert.equal(r.resumeState.remainingMs,75000,kind);
 assert.equal(c.engagementDisplayState(r.definition),'completed',kind);
 const entries=r.game.entries,ideas=r.ideas,finished=r.finishedAt,votes=r.questionVotes;
 now+=1000000; // Reviewing results must not consume the frozen remaining time.
 c.reopenEngagementRuntime();
 assert.equal(c.engagementDisplayState(r.definition),'running',kind);
 assert.equal(r.deadlineMs-now,135000,kind+' remaining time + one minute');
 assert.equal(r.phase,kind==='pair'?3:kind==='questions'?2:1,kind);
 assert.equal(r.game.entries,entries);assert.equal(r.ideas,ideas);assert.equal(r.finishedAt,finished);assert.equal(r.questionVotes,votes);
 assert.equal(r.game.stage,kind==='ball'?'play':kind==='teach'?'teach':'join',kind);
 assert.equal(r.questionIndex,1);assert.equal(r.completedReview,false);assert.equal(r.questionRevealed,false);
}
for(const mode of ['expired','paused','untimed','legacy']){
 const r=activity('storm',mode==='untimed'?0:120);c.keynopeEngagementRuntime=c.keynopeRunningActivity=r;
 r.deadlineMs=mode==='expired'?now-1:0;r.pausedRemainingMs=mode==='paused'?30000:0;
 if(mode!=='legacy')c.rememberEngagementResume(r);
 r.phase=3;r.completedReview=true;r.deadlineMs=0;c.rememberEngagementResume(r);
 // Resume metadata survives serializing a result and reloading the deck.
 const restored=JSON.parse(JSON.stringify(r));c.keynopeEngagementRuntime=c.keynopeRunningActivity=restored;
 c.reopenEngagementRuntime();
 assert.equal(restored.deadlineMs,mode==='untimed'?0:now+(mode==='paused'?90000:60000),mode);
 assert.equal(restored.pausedRemainingMs,0);
}
const done=activity('storm');done.phase=3;done.completedReview=true;
c.keynopeEngagementRuntime=done;c.keynopeRunningActivity=activity('pulse');
c.reopenEngagementRuntime();assert.equal(conflicts,1);assert.equal(done.phase,3,'conflict reopened activity');
assert.equal(c.engagementDisplayState(done.definition),'completed');
assert.equal(c.engagementDisplayState({id:'unopened'}),'clean','other runtime coloured unrelated activity');
c.keynopeRunningActivity=null;c.keynopeEngagementRuntime=null;
sessions.set('saved',{result:{state:{phase:3}}});assert.equal(c.engagementDisplayState({id:'saved'}),'completed');
sessions.set('saved',{result:null,runtime:{definition:{id:'saved',kind:'storm'},phase:0}});
assert.equal(c.engagementDisplayState({id:'saved'}),'clean');
console.log(`PASS ${kinds.length} activity types: Reopen preserves entries/answers/groups, working phase and remaining time + one minute; paused/expired/untimed/archived states, conflicts and activity colours.`);
