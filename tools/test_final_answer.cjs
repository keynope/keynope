const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const c=vm.createContext({crypto:require('node:crypto').webcrypto});
vm.runInContext(fs.readFileSync('web/activity-games.js','utf8'),c);
const source=fs.readFileSync('main.go','utf8');
for(const name of ['syncFinalAnswerGroups','syncDeducerRuntimeGroups','startEngagementRuntime','engagementSubmissionProgress']){
 const start=source.indexOf('function '+name+'(');vm.runInContext(source.slice(start,source.indexOf('\n}',start)+2),c);
}
const game=c.KeynopeGames,names={a:'Alice',b:'Bob',c:'Charlie',d:'Dennis'};
const room=c.KeynopeGroups.room('Test1234',[{id:'red',label:'Red',members:['a','b']},{id:'blue',label:'Blue',members:['c','d']}],names,'same-chat');
c.keynopePairingRoom=room;
const r={definition:{kind:'finalanswer',maxEntries:99},sessionCode:'Test1234',phase:1,memberNames:names};
c.syncDeducerRuntimeGroups(r);
const original=JSON.stringify(room),groups=JSON.stringify(r.game.groups);
assert.equal(r.game.groups.length,2);
assert.equal(r.game.groups[0].id,'red');assert.equal(r.game.pairingEpoch,'same-chat');
let seq=0;
const send=(identity,response)=>game.receive(r,{id:'event-'+(++seq),identity,payload:{response:{groupEpoch:'same-chat',...response}}});
send('a',{action:'add',text:'Our answer'});send('b',{action:'add',text:'Concurrent answer'});
assert.equal(r.game.entries.length,1,'must enforce one shared answer regardless of metadata');
const first=r.game.entries[0].entryId;
send('c',{action:'remove',entryId:first});assert.equal(r.game.entries.length,1);
send('b',{action:'remove',entryId:first});assert.equal(r.game.entries.length,0);
send('b',{action:'add',text:'Agreed replacement'});send('c',{action:'add',text:'Blue answer'});
c.syncDeducerRuntimeGroups(r);
assert.equal(r.game.entries.length,2);assert.equal(JSON.stringify(r.game.groups),groups);
assert.equal(c.engagementSubmissionProgress(r).all,true,'one answer from each group completes submissions');
assert.equal(c.engagementSubmissionProgress(r).submitted,2);
assert.equal(c.keynopePairingRoom,room);assert.equal(JSON.stringify(room),original,'chat/groups must be unchanged');
r.memberNames={...names,late:'Late joiner'};c.syncDeducerRuntimeGroups(r);
assert(!r.game.groups.some(g=>g.ids.includes('late')),'never assign newcomers automatically');
send('late',{action:'add',text:'Unassigned'});assert.equal(r.game.entries.length,2);
game.next(r);assert.equal(r.phase,3);
send('a',{action:'remove',entryId:r.game.entries[0].entryId});assert.equal(r.game.entries.length,2);
r.phase=1;r.deadlineMs=Date.now()-1;
send('a',{action:'remove',entryId:r.game.entries[0].entryId});assert.equal(r.game.entries.length,2);
// Start must leave the activity clean if groups are missing or from another session.
c.keynopeRunningActivity=null;c.showEngagementToast=message=>c.toast=message;
for(const missing of [null,{sessionCode:'Other123',groups:room.groups},{sessionCode:'Test1234',groups:[]}]){
 c.keynopePairingRoom=missing;c.keynopeEngagementRuntime={...r,phase:0};c.startEngagementRuntime();
 assert.equal(c.keynopeEngagementRuntime.phase,0);assert.match(c.toast,/CREATE GROUPS FIRST/);
}
console.log('Final Answer: existing groups/chat preserved, one shared answer, teammate replacement, group isolation, unassigned joiners, reveal, expiry, and missing-group Start guard passed.');
