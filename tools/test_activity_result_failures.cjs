const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('main.go','utf8');
const stored=new Map(),sessions=new Map(),messages=[];let failing=true,attempts=0;
const context=vm.createContext({window:{keynopeStoreActivityResult:async(id,result)=>{
  attempts++;if(failing)throw Error('activity result definition does not match');stored.set(id,result);
}},console:{warn:()=>{}},showEngagementToast:message=>messages.push(message),
engagementSessionFor:def=>{if(!sessions.has(def.id))sessions.set(def.id,{});return sessions.get(def.id);}});
vm.runInContext(source.slice(source.indexOf('let keynopeActivityResultWrites ='),source.indexOf('const keynopeTrainingDepartures =')),context);
(async()=>{
  const runtime={definition:{id:'storm',kind:'storm'},phase:3,ideas:['Keep my answer']};
  context.archiveEngagementResult(runtime);
  assert.equal(await context.window.keynopeFlushActivityResults(),false,'failure must resolve, not reject Save');
  assert(context.window.keynopeHasPendingActivityResults());
  assert.equal(sessions.get('storm').result.state.ideas[0],'Keep my answer');
  const before=attempts;context.archiveEngagementResult(runtime);
  await vm.runInContext('keynopeActivityResultWrites',context);
  assert.equal(attempts,before,'render loop must not flood a failing backend');
  failing=false;assert.equal(await context.window.keynopeFlushActivityResults(),true);
  assert.equal(stored.get('storm').state.ideas[0],'Keep my answer');
  assert.equal(context.window.keynopeHasPendingActivityResults(),false);
  // Reset supersedes an unsuccessful archive; retry must not resurrect results.
  failing=true;runtime.ideas=['New answer'];context.archiveEngagementResult(runtime);
  await vm.runInContext('keynopeActivityResultWrites',context);
  context.archiveEngagementResult(runtime,true);
  assert.equal(await context.window.keynopeFlushActivityResults(),false);
  failing=false;await context.window.keynopeFlushActivityResults();assert.equal(stored.get('storm'),null);
  // Clearing a deck must cancel queued retries into the next document.
  failing=true;runtime.ideas=['Old deck'];context.archiveEngagementResult(runtime);
  await vm.runInContext('keynopeActivityResultWrites',context);
  vm.runInContext('keynopePendingActivityResults.clear()',context);
  failing=false;await context.window.keynopeFlushActivityResults();assert.equal(stored.get('storm'),null);
  assert(messages.some(m=>m.includes('NOT INCLUDED')));
  console.log('PASS: failed result writes do not poison Save; pending data retries, resets supersede, deck changes cancel.');
})().catch(error=>{console.error(error);process.exitCode=1});
