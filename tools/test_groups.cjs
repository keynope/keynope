const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const context=vm.createContext({crypto:require('node:crypto').webcrypto,console,Date,
 keynopePairingRoom:null,keynopeEngagementRuntime:null,keynopeRunningActivity:null,keynopePairingPublished:new WeakMap(),keynopeTrainingDepartures:new Map(),
 randomActivityCode:()=>require('node:crypto').randomBytes(6).toString('hex'),
 renderEngagementRuntime(){},publishEngagementRuntime(){},publishHostedEngagementState:async()=>{}});
vm.runInContext(fs.readFileSync('web/activity-games.js','utf8'),context);
const src=fs.readFileSync('main.go','utf8');
for(const name of ['receivePairingRoom','publishPairingRoom','setPairingGroups','replacePairingRoom','syncDeducerRuntimeGroups','shuffleEngagementGroups']){
 const i=src.search(new RegExp('(?:async )?function '+name+'\\('));vm.runInContext(src.slice(i,src.indexOf('\n}',i)+2),context);
}
const groups=context.KeynopeGroups,plain=v=>JSON.parse(JSON.stringify(v));
(async()=>{
 const original=[{id:'a',label:'Blue',members:['a','b','c']},{id:'b',label:'Gold',members:['d','e']}],names={a:'Alex',b:'Bea',c:'Chris',d:'Dana',e:'Eli'};
 for(let i=0;i<100;i++){
  const next=groups.shuffled(original);assert.deepEqual(plain(next.map(g=>g.members.length)),[3,2]);assert.deepEqual(plain(next.flatMap(g=>g.members).sort()),['a','b','c','d','e']);
 }
 assert.deepEqual(original[0].members,['a','b','c'],'shuffle mutated its baseline');
 const dedup=groups.normalize([{members:['a','a','b']},{members:['b','c']}],new Set(['a','c']));assert.deepEqual(plain(dedup.map(g=>g.members)),[['a'],['c']]);
 const sends=[],channel={send:async p=>sends.push(plain(p))};
 await context.setPairingGroups('Test1234',original,names,channel);const first=context.keynopePairingRoom;
 await context.publishPairingRoom(channel,'Test1234');assert.equal(sends.length,1,'unchanged chats republished');
 context.receivePairingRoom({payload:{type:'pairing',pairing:plain(first)}},'Test1234');assert.equal(context.keynopePairingRoom,first,'echo changed room reference');
 await context.setPairingGroups('Test1234',original,names,channel);assert.notEqual(context.keynopePairingRoom.epoch,first.epoch);
 const r=context.keynopeEngagementRuntime={definition:{kind:'shuffle'},phase:1,sessionCode:'Test1234',activityChannel:channel,roomReady:true,memberNames:names};
 await context.shuffleEngagementGroups();const baseline=plain(r.game.originalGroups),epoch=context.keynopePairingRoom.epoch;
 assert.equal(r.phase,3);assert.equal(sends.at(-1).type,'pairing');assert(!sends.at(-1).pairing.assignments,'wire groups should not duplicate every member list');
 await context.shuffleEngagementGroups();assert.deepEqual(plain(r.game.originalGroups),baseline,'reshuffle must retain initial baseline');
 await context.shuffleEngagementGroups(true);assert.deepEqual(plain(groups.fromRoom(context.keynopePairingRoom)),baseline);assert.notEqual(context.keynopePairingRoom.epoch,epoch);
 await context.shuffleEngagementGroups();context.keynopeTrainingDepartures.set('Test1234',new Map([['a',Date.now()]]));await context.shuffleEngagementGroups(true);assert(!context.keynopePairingRoom.groups.flatMap(g=>g.members).includes('a'),'revert resurrected departed member');
 await context.shuffleEngagementGroups();await context.setPairingGroups('Test1234',original,names,channel);await assert.rejects(()=>context.shuffleEngagementGroups(true),/Groups changed/);
 const before=context.keynopePairingRoom;r.localPreview=true;await context.shuffleEngagementGroups();assert.equal(context.keynopePairingRoom,before,'preview changed live groups');
 await context.setPairingGroups('Test1234',[],names,channel);assert.equal(sends.at(-1).pairing.groups.length,0,'last removal must close old channels');
 // Deducer manual membership survives subsequent rendering; entries stay with
 // group IDs and removing a member leaves them explicitly unassigned.
 const d=context.keynopeEngagementRuntime={definition:{kind:'deducer',groupCount:2},phase:1,sessionCode:'Test1234',memberNames:names,activityChannel:channel};
 context.syncDeducerRuntimeGroups(d);const current=groups.fromRoom(context.keynopePairingRoom);current[0].members=current[0].members.filter(id=>id!=='a');current[1].members=current[1].members.filter(id=>id!=='a');
 await context.setPairingGroups('Test1234',current,names,channel);const manual=context.keynopePairingRoom.epoch;context.syncDeducerRuntimeGroups(d);assert(!d.pairAssignments.a);assert.equal(context.keynopePairingRoom.epoch,manual,'Deducer redraw replaced manual groups');
 d.definition.kind='impostor';context.replacePairingRoom(d);assert.equal(context.keynopePairingRoom.epoch,manual);
 console.log('PASS groups: manual edits, epoch replacement, linear-size wire data, shuffle sizes, original/revert, departure filtering, conflict handling, preview isolation and Deducer stability.');
})().catch(error=>{console.error(error);process.exitCode=1});
