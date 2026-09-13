const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('main.go','utf8');
const context=vm.createContext({KeynopeGames:{has:()=>false},keynopePairingRoom:null});
for(const name of ['receiveQuestionResponse','foldHostedEngagementResponses','hostedEngagementResults']){
  const start=source.indexOf('function '+name+'('),end=source.indexOf('\n}',start)+2;
  vm.runInContext(source.slice(start,end),context);
}
const r={definition:{kind:'questions',named:true},phase:1,memberNames:{a:'Alice',b:'Bob'},entryResponses:[],questionVotes:{},seenResponseEvents:{},responseByIdentity:{}};
let n=0;
const send=(identity,response,id='e'+(++n))=>context.receiveQuestionResponse(r,{id,identity,payload:{response}},r.memberNames[identity]);
assert(send('a',{question:'First question'},'q1'));
assert(send('a',{question:'Second question'},'q2'));
assert(!send('a',{question:'First question'},'q1'));
assert(!send('unknown',{question:'No'}));
assert(!send('a',{questionDots:['q1']}));
context.foldHostedEngagementResponses(r);
assert.equal(context.hostedEngagementResults(r).attributions.length,0);
r.phase=2;
assert(!send('a',{question:'Too late'}));
for(const ballot of [['q1','q1','q1','q2'],['q1','q1'],['missing'],[1],null])assert(!send('b',{questionDots:ballot}));
assert(send('a',{questionDots:['q1','q2']}));
assert(send('b',{questionDots:['q2']}));
context.foldHostedEngagementResponses(r);
assert.equal(r.attributions[0].votes,1);assert.equal(r.attributions[1].votes,2);
assert.equal(context.hostedEngagementResults(r).attributions[0].votes,undefined);
assert.equal(context.hostedEngagementResults(r).attributions[0].question,'First question');
assert(send('a',{questionDots:['q2']})); // Replacement, not another budget.
context.foldHostedEngagementResponses(r);
assert.equal(r.attributions[0].votes,0);assert.equal(r.attributions[1].votes,2);
r.phase=3;
assert(!send('a',{questionDots:['q1','q1','q1']}));
assert(!send('a',{question:'Too late'}));
const ranked=context.hostedEngagementResults(r).attributions.slice().sort((a,b)=>b.votes-a.votes);
assert.equal(ranked[0].id,'q2');assert.equal(ranked[0].votes,2);
r.definition.named=false;assert.equal(context.hostedEngagementResults(r).attributions[0].displayName,'');
console.log('Questions: collection/voting/reveal gates, 3-dot budget, duplicate rejection, replacement and hidden totals passed.');
