// Playwright waitForFunction checks predicate truthiness before awaiting its
// value. Async predicates therefore stop on their first (truthy) Promise even
// when it resolves false. Await each local page predicate before deciding.
async function waitForAsync(page,predicate,arg,{timeout=30000,polling=40}={}) {
  const deadline=Date.now()+timeout;
  do {
    const result=await page.evaluate(predicate,arg);
    if(result)return result;
    if(Date.now()>=deadline)break;
    await new Promise(resolve=>setTimeout(resolve,polling));
  }while(Date.now()<deadline);
  throw new Error('Timed out after '+timeout+'ms awaiting asynchronous page predicate: '+String(predicate));
}
module.exports={waitForAsync};
