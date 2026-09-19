const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const c=vm.createContext({crypto:require('node:crypto').webcrypto});
vm.runInContext(fs.readFileSync('web/activity-games.js','utf8'),c);
const games=c.KeynopeGames,r={definition:{kind:'nominate'},phase:1,memberNames:{a:'Alice',b:'Bob',c:'Charlie',p:'Presenter'}};
let sequence=0;const send=(identity,response,id='event-'+(++sequence))=>games.receive(r,{id,identity,payload:{response}});
for(const [who,ballot] of [['a',{nominee:'a'}],['a',{nominee:'unknown'}],['unknown',{nominee:'a'}],['p',{nominee:'a'}],['a',{nominee:'p'}],['a',{}]])send(who,ballot);
assert.equal(r.game.entries.length,0);
send('a',{nominee:'b'},'first');send('a',{nominee:'c'});send('a',{nominee:'b'},'first');
assert.equal(r.game.entries.length,1);assert.equal(r.game.entries[0].target,'c');
send('b',{nominee:'c'});send('c',{skip:true});assert.equal(r.participants,3);
for(const named of [false,true]){r.definition.named=named;const state=games.publicState(r);assert.equal(state.submitted,3);assert.equal(state.total,3);assert(!state.entries&&!state.nominations&&!state.skipVoters,'no ballot details before reveal');}
r.phase=2;send('a',{skip:true});assert.equal(r.game.entries[0].target,'c');
r.phase=3;r.definition.named=false;let state=games.publicState(r);
assert.equal(state.nominations.length,1);assert.equal(state.nominations[0].count,2);assert.equal(state.skipped,1);assert(!state.nominations[0].voters&&!state.skipVoters&&!state.entries);
r.definition.named=true;state=games.publicState(r);assert.equal(state.nominations[0].voters.join(','),'Alice,Bob');assert.equal(state.skipVoters.join(','),'Charlie');
r.phase=1;send('a',{skip:true});assert.equal(games.publicState(r).submitted,3);
r.deadlineMs=Date.now()-1;send('b',{skip:true});assert.equal(r.game.entries.find(e=>e.id==='b').target,'c');
const source=fs.readFileSync('main.go','utf8'),start=source.indexOf('function pruneTrainingParticipant(');
c.foldHostedEngagementResponses=()=>{};
vm.runInContext(source.slice(start,source.indexOf('\n}',start)+2),c);
c.pruneTrainingParticipant(r,'c');assert.equal(r.game.entries.length,1);assert.equal(r.participants,1);
assert.equal(r.game.groups.length,0,'no grouping side effects');
// Delayed delivery/history cannot replace a newer ballot after reopening.
const ordered={definition:{kind:'nominate'},phase:1,memberNames:{a:'Alice',b:'Bob'}};
games.receive(ordered,{id:'new',identity:'a',createdAt:'2026-09-16T12:00:02Z',payload:{response:{skip:true}}});
games.receive(ordered,{id:'old',identity:'a',createdAt:'2026-09-16T12:00:01Z',payload:{response:{nominee:'b'}}});
assert.equal(ordered.game.entries[0].skip,true);
delete ordered.seenResponseEvents;
games.receive(ordered,{id:'old',identity:'a',createdAt:'2026-09-16T12:00:01Z',payload:{response:{nominee:'b'}}});
assert.equal(ordered.game.entries[0].skip,true);
// Optional private-site check. The public checkout must not require terraform/.
if(fs.existsSync('terraform/site/join/join.js')){
 const join=fs.readFileSync('terraform/site/join/join.js','utf8'),restore=join.indexOf('function restoreOwnResponse(');
 c.state={activityId:'test',definition:{kind:'nominate'}};c.activityDraft={};c.render=()=>{};c.setStatus=()=>{};
 vm.runInContext(join.slice(restore,join.indexOf('\n}',restore)+2),c);
 c.restoreOwnResponse({activityId:'test',response:{nominee:'b'}});assert.equal(c.activityDraft.nominee,'b');assert.equal(c.activityDraft.nominateSubmitted,'b');
 c.restoreOwnResponse({activityId:'test',response:{skip:true}});assert.equal(c.activityDraft.nominee,'');assert.equal(c.activityDraft.nominateSubmitted,'skip');
}
console.log('Nominate: validation, replacement, deduplication, skip, privacy, named results, locks, expiry and participant removal passed.');
