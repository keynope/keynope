const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const src=fs.readFileSync('main.go','utf8');
const start=src.indexOf('function receiveHostedEngagement('),end=src.indexOf('\n}',start)+2;
let broadcasts=[],timers=[],renders=0;
const runtime={definition:{kind:'ball',id:'ball'},memberNames:{},rolePublicKeys:{},activityChannel:{}};
const context=vm.createContext({keynopeLobbyPresentation:null,keynopeEngagementRuntime:runtime,keynopeEngagementSessionEpoch:1,
  setTimeout:fn=>{timers.push(fn);return timers.length},renderEngagementRuntime:()=>renders++,
  publishHostedEngagementState:async definition=>broadcasts.push(definition)});
vm.runInContext(src.slice(start,end),context);
const receive=(i,type)=>context.receiveHostedEngagement({identity:'user'+i,payload:{type,displayName:'User '+i}},runtime,1);
(async()=>{
  for(let i=0;i<20;i++)receive(i,'hello');
  assert.equal(timers.length,1);
  await timers.shift()();
  assert.deepEqual(broadcasts,[true,false]);
  assert.equal(renders,1);
  // Every definition used to elicit 20 replies, and each reply two states.
  for(let repeat=0;repeat<20;repeat++)for(let i=0;i<20;i++)receive(i,'presence');
  assert.equal(timers.length,0);assert.equal(broadcasts.length,2);
  receive(21,'hello');await timers.shift()();
  assert.deepEqual(broadcasts,[true,false,true,false],'late join must recover definition and results');
  console.log('PASS: 20 joins coalesce into one definition/state pair; 400 duplicate presences produce no broadcasts; late join recovers.');
})().catch(e=>{console.error(e);process.exitCode=1});
