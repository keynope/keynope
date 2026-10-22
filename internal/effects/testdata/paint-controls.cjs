// Production paint command queue: no browser, timers or build required.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
vm.runInThisContext(fs.readFileSync(path.resolve(__dirname,'../../../web/workspace.js'),'utf8'));
const tick=()=>new Promise(resolve=>setImmediate(resolve));
(async()=>{
 let current={epoch:1,owner:'deck',slide:0,master:false,group:'',targetIds:['image'],revision:1},release;
 const calls=[],errors=[];
 const writer=KeynopeWorkspace.selectionPaintWriter({target:()=>({...current}),write:async(property,value,target)=>{
  calls.push({property,value,revision:target.revision});
  if(calls.length===1)await new Promise(resolve=>release=resolve);
  current={...current,revision:current.revision+1};
 },error:error=>errors.push(error.message)});
 const complete=writer.enqueue('paint.blur',10);await tick();
 writer.enqueue('paint.blur',20);writer.enqueue('paint.blur',0);writer.enqueue('paint.blur',0);
 assert.equal(writer.busy('paint.blur'),true);release();await complete;
 assert.deepEqual(calls,[{property:'paint.blur',value:10,revision:1},{property:'paint.blur',value:0,revision:2}]);
 assert.equal(writer.busy('paint.blur'),false);assert.deepEqual(errors,[]);
 const stale={...current,revision:1};await writer.enqueue('paint.blur',30,stale);
 assert.match(errors.pop(),/document changed/);assert.equal(calls.length,2);
 let unblock;const applied=[];
 const scoped=KeynopeWorkspace.selectionPaintWriter({target:()=>current,write:async(property,value)=>{applied.push(value);await new Promise(resolve=>unblock=resolve);}});
 const pending=scoped.enqueue('paint.blur',10);await tick();scoped.enqueue('paint.blur',20);
 current={...current,targetIds:['another-image']};unblock();await pending;assert.deepEqual(applied,[10]);
 const recoverErrors=[],recovered=[];let reject;
 const recovering=KeynopeWorkspace.selectionPaintWriter({target:()=>current,write:async(property,value)=>{recovered.push(value);if(value===10)await new Promise((resolve,fail)=>reject=fail);},error:error=>recoverErrors.push(error.message)});
 const recovery=recovering.enqueue('paint.blur',10);await tick();recovering.enqueue('paint.blur',0);reject(Error('Test rejection'));await recovery;
 assert.deepEqual(recovered,[10,0]);assert.deepEqual(recoverErrors,['Test rejection']);
 current=null;await recovering.enqueue('paint.blur',1);assert.deepEqual(recovered,[10,0]);
 console.log('PASS: latest paint wins, revision rebasing, duplicate suppression, scoped cancellation and error recovery');
})().catch(error=>{console.error(error);process.exitCode=1;});
