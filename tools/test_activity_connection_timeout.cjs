// A stalled browser/network request must reach the existing retry path.
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('internal/app/main.go','utf8');
const start=source.indexOf('function withActivityConnectionTimeout('),end=source.indexOf('\n}',start)+2;
const context=vm.createContext({setTimeout,clearTimeout,Promise,Error});
vm.runInContext(source.slice(start,end),context);
(async()=>{
 const run=context.withActivityConnectionTimeout;
 const channel={close(){throw Error('An on-time channel must remain open');}};
 assert.equal(await run(Promise.resolve(channel),1000),channel);
 await assert.rejects(run(Promise.reject(Error('offline')),1000),/offline/);
 await assert.rejects(run(new Promise(()=>{}),1),/timed out/);
 let complete,closed=0;
 const late=new Promise(resolve=>complete=resolve);
 await assert.rejects(run(late,1),/timed out/);
 complete({close(){closed++;return Promise.resolve();}});
 await new Promise(resolve=>setImmediate(resolve));
 assert.equal(closed,1,'A late connection must not leave an orphan presenter in the room');
 console.log('PASS: activity connections resolve, fail or time out; late connections close safely.');
})().catch(error=>{console.error(error);process.exitCode=1});
