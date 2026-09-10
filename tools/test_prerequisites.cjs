const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const src=fs.readFileSync('main.go','utf8');
const context=vm.createContext({Date,randomActivityCode:()=>String(Math.random()),engagementRandomIndex:()=>0,renderEngagementRuntime(){},publishEngagementRuntime(){},publishHostedEngagementState(){}});
vm.runInContext('let keynopePairingRoom=null;',context);
for(const name of ['validPrerequisiteCompletion','prerequisitePublicState','replacePairingRoom','balancedCompletionGroups','finishBalancedPairs']){
 const start=src.indexOf('function '+name+'('),end=src.indexOf('\n}',start)+2;vm.runInContext(src.slice(start,end),context);
}
const r={phase:1,sessionCode:'Test1234',definition:{kind:'prerequisites',named:true,prerequisites:[{title:'One'},{title:'Two'}]},startedAt:1000,finishedAt:{a:1100,b:1200,c:1300,d:1400},memberNames:{a:'A',b:'B',c:'C',d:'D'}};
assert(!context.validPrerequisiteCompletion(r.definition,[true,false]));assert(!context.validPrerequisiteCompletion(r.definition,[true]));assert(!context.validPrerequisiteCompletion(r.definition,[1,1]));assert(context.validPrerequisiteCompletion(r.definition,[true,true]));
assert.equal(context.prerequisitePublicState(r).ranking.length,0);
context.finishBalancedPairs(r);assert.equal(r.phase,3);assert.deepEqual(Array.from(r.pairAssignments.a.members),['a','d']);assert.deepEqual(Array.from(r.pairAssignments.b.members),['b','c']);
assert.equal(context.prerequisitePublicState(r).ranking[0].elapsedMs,100);
r.definition.named=false;assert.equal(context.prerequisitePublicState(r).ranking.length,0);assert.equal(context.prerequisitePublicState(r).finishedNames.length,0);
const before=vm.runInContext('keynopePairingRoom.epoch',context);r.definition.kind='impostor';context.replacePairingRoom(r);assert.equal(vm.runInContext('keynopePairingRoom.epoch',context),before);
r.definition.kind='cards';context.replacePairingRoom(r);assert.notEqual(vm.runInContext('keynopePairingRoom.epoch',context),before);
const groups=context.balancedCompletionGroups(['a','b','c','d','e'],{a:1,b:2},()=>0);assert.equal(groups.flat().length,5);assert(groups.every(g=>g.length>=2));
const live={definition:{id:'check',kind:'prerequisites',named:true,prerequisites:[{title:'One'},{title:'Two'}]},phase:1,sessionCode:'Test1234',startedAt:Date.now(),finishedAt:{},memberNames:Object.fromEntries(Array.from({length:40},(_,i)=>['p'+i,'Person '+i])),responseByIdentity:{}};
Object.assign(context,{keynopeLobbyPresentation:null,keynopeEngagementRuntime:live,keynopeEngagementSessionEpoch:1,KeynopeGames:{receive:()=>false},foldHostedEngagementResponses(){}});
const receiveStart=src.indexOf('function receiveHostedEngagement(');vm.runInContext(src.slice(receiveStart,src.indexOf('\n}',receiveStart)+2),context);
const finish=(i,checked)=>context.receiveHostedEngagement({id:'finish'+i,identity:'p'+i,displayName:'Person '+i,payload:{type:'response',activityId:'check',response:{finished:true,checked}}},live,1);
finish(0,[true,false]);assert.equal(Object.keys(live.finishedAt).length,0);
for(let i=0;i<39;i++)finish(i,[true,true]);assert.equal(live.phase,1);
finish(39,[true,true]);assert.equal(live.phase,3);assert.equal(live.groups.length,20);assert.equal(Object.keys(live.pairAssignments).length,40);
console.log('Prerequisites: completion validation, balanced groups, ranking privacy, new group replacement and Impostor exclusion passed.');
