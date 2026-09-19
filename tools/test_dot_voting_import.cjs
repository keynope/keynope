const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('main.go','utf8'),noop=()=>{},sessions=new Map(),saved=new Map(),messages=[];
const c=vm.createContext({window:{keynopeActivityResult:id=>saved.get(id)},deck:{pages:[]},Date,TextEncoder,
 keynopeEngagementRuntime:null,keynopeRunningActivity:null,
 engagementSessionFor:d=>{if(!sessions.has(d.id))sessions.set(d.id,{});return sessions.get(d.id);},
 showEngagementToast:message=>messages.push(message),engagementPresentationActive:()=>true,
 detachEngagementRuntime:noop,renderEngagementRuntime:noop,publishEngagementRuntime:noop,
 runEngagementCountdown:noop,startHostedEngagement:noop,confirmRunningActivity:()=>messages.push('conflict')});
for(const name of ['activityProducesVotingItems','activitySubmittedItems','previousActivityForVoting','startEngagementRuntime','engagementHasCompleted','engagementIsRunning']){
 const a=source.indexOf('function '+name+'(');vm.runInContext(source.slice(a,source.indexOf('\n}',a)+2),c);
}
const old={definition:{id:'slide-2',kind:'storm'},phase:3,startedAt:9000,ideas:['Wrong source','Do not import']};
const prior={definition:{id:'slide-13',kind:'storm'},phase:3,startedAt:1000,entryResponses:[{idea:'One'},{idea:' Two '},{idea:'One'}],ideas:['One','Two']};
const vote={definition:{id:'slide-16',kind:'dots',importPrevious:true,dotBudget:3},phase:0};
c.deck.pages=Array.from({length:16},(_,slide)=>({slide}));
c.deck.pages[1].engagement=old.definition;c.deck.pages[12].engagement=prior.definition;c.deck.pages[15].engagement=vote.definition;
sessions.set(old.definition.id,{runtime:old});sessions.set(prior.definition.id,{runtime:prior});
c.keynopeRunningActivity=old;c.keynopeEngagementRuntime=vote;
c.startEngagementRuntime();assert.deepEqual(Array.from(vote.definition.options),['One','Two']);assert.equal(vote.phase,1);assert.equal(c.keynopeRunningActivity,vote);
// Reopening doesn't import again; Start only operates on Ready activities.
prior.ideas.push('Later');c.startEngagementRuntime();assert.equal(vote.definition.options.length,2);
vote.phase=0;c.startEngagementRuntime();assert.deepEqual(Array.from(vote.definition.options),['One','Two','Later']);
// Stored results work after restart; no last-run state is required.
sessions.clear();saved.set(prior.definition.id,{definition:prior.definition,state:{phase:3,ideas:['Saved one','Saved two']}});
vote.phase=0;c.startEngagementRuntime();assert.deepEqual(Array.from(vote.definition.options),['Saved one','Saved two']);
// The nearest activity is authoritative: do not skip an empty/reset source.
sessions.set(prior.definition.id,{runtime:{definition:prior.definition,phase:0,ideas:[]}});
vote.phase=0;c.startEngagementRuntime();assert.equal(vote.phase,0);assert.match(messages.pop(),/AT LEAST TWO/);
// Reordering slides changes the source; unrelated navigation does not.
c.deck.pages[12].engagement=old.definition;c.deck.pages[1].engagement=prior.definition;sessions.set(old.definition.id,{runtime:old});
c.startEngagementRuntime();assert.deepEqual(Array.from(vote.definition.options),['Wrong source','Do not import']);
old.phase=1;vote.phase=0;c.keynopeRunningActivity=old;c.startEngagementRuntime();assert.equal(messages.pop(),'conflict');assert.equal(vote.phase,0);
assert.deepEqual(Array.from(c.activitySubmittedItems({attributions:[{question:'Q',displayName:'Private name',answers:['A'],tags:['Skill']}],game:{entries:[{text:'Deduction',name:'Private name'}]}})),['Q','A','Skill','Deduction']);
// Deducer -> Pressure Cooker -> Dot Voting: import the three deductions.
sessions.clear();saved.clear();c.keynopeRunningActivity=null;vote.phase=0;
const deducer={id:'deducer',kind:'deducer'},pressure={id:'pressure',kind:'pressure'};
c.deck.pages=[{slide:24,engagement:deducer},{slide:25,engagement:pressure},{slide:26,engagement:vote.definition}];
saved.set(deducer.id,{definition:deducer,state:{phase:3,game:{entries:[{text:'First deduction'},{text:'Second deduction'},{text:'Third deduction'}]}}});
c.startEngagementRuntime();assert.equal(vote.phase,1);assert.deepEqual(Array.from(vote.definition.options),['First deduction','Second deduction','Third deduction']);
const skipped=['onboarding','pressure','chosen','shuffle','cards','impostor','pair','finishpair','prerequisites','ball','pulse','quiz','truefalse','dots','sort','match','draw','introduction','hunt'];
for(const kind of skipped){
 assert.equal(c.activityProducesVotingItems({kind}),false,kind);
 c.deck.pages[1].engagement={id:'skip-'+kind,kind};vote.phase=0;c.startEngagementRuntime();
 assert.equal(vote.definition.options.length,3,kind+' was incorrectly selected as source');
}
for(const kind of ['storm','deducer','finalanswer','dual','questions','wall','expertise','gallery','teach','fame','agreements','three'])assert(c.activityProducesVotingItems({kind}),kind);
// A newer eligible but empty activity must still stop lookup, not use old answers.
c.deck.pages.splice(2,0,{slide:25,engagement:{id:'empty-storm',kind:'storm'}});
vote.phase=0;c.startEngagementRuntime();assert.equal(vote.phase,0);assert.match(messages.pop(),/AT LEAST TWO/);
console.log('PASS automatic voting: nearest text-producing activity in slide order, Deducer across Pressure Cooker, all non-producing types skipped, saved results, reset/empty source, deduplication and conflict guard.');
