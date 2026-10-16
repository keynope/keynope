const assert=require('node:assert/strict');
const {waitForAsync}=require('./wait_for_async.cjs');
(async()=>{
 let calls=0;
 const page={evaluate:async(fn,arg)=>fn(arg)};
 const result=await waitForAsync(page,async count=>{await Promise.resolve();return ++calls>=count;},3,{timeout:1000,polling:1});
 assert.equal(result,true);assert.equal(calls,3,'Stopped at a Promise resolving false');
 await assert.rejects(waitForAsync(page,async()=>false,null,{timeout:5,polling:1}),/Timed out/);
 const failure=new Error('Fixture failure');await assert.rejects(waitForAsync(page,async()=>{throw failure;}),error=>error===failure);
 console.log('PASS: async polling retries false values, times out and propagates evaluation failures.');
})().catch(error=>{console.error(error);process.exitCode=1;});
