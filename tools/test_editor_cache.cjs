const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const handlers = {}, entries = new Map(), removed = [];
let offline = false, fetched = 0;
const url = path => 'https://keynope.sh' + path;
const key = request => typeof request === 'string' ? request : request.url;
const cache = {
  match:async request=>entries.get(key(request))?.clone(),
  put:async(request,response)=>entries.set(key(request),response.clone()),
  addAll:async()=>{}
};
vm.runInNewContext(fs.readFileSync('web/editor/service-worker.js','utf8'), {
  URL, Request, self:{location:{origin:'https://keynope.sh'},skipWaiting(){},clients:{claim(){}},addEventListener:(name,handler)=>handlers[name]=handler},
  caches:{open:async()=>cache,keys:async()=>['keynope-editor-old','unrelated-app'],delete:async name=>removed.push(name)},
  fetch:async()=>{fetched++;if(offline)throw Error('offline');return new Response('new');}
});
async function request(path, options={}) {
  let response;
  handlers.fetch({request:new Request(url(path),options),respondWith:value=>response=value});
  return response ? (await response).text() : undefined;
}
(async()=>{
  entries.set(url('/editor/'),new Response('old'));
  assert.equal(await request('/editor/'),'new','HTML must update even with a cached copy');
  entries.set(url('/editor/keynope-editor.wasm?build=test'),new Response('matching build'));
  let count=fetched;
  assert.equal(await request('/editor/keynope-editor.wasm?build=test'),'matching build');
  assert.equal(fetched,count,'versioned assets avoid redundant downloads');
  assert.equal(await request('/editor/keynope-editor.wasm?build=test',{cache:'reload'}),'new');
  entries.set(url('/editor/Welcome.md'),new Response('old'));
  assert.equal(await request('/editor/Welcome.md',{cache:'no-store'}),'new');
  assert.equal(await (await cache.match(url('/editor/Welcome.md'))).text(),'old','no-store is respected');
  assert.equal(await request('/api/editor/state'),undefined);
  assert.equal(await request('/join/example'),undefined);
  offline=true;
  assert.equal(await request('/editor/'),'new','offline still works');
  let activation;
  handlers.activate({waitUntil:value=>activation=value});await activation;
  assert.deepEqual(removed,['keynope-editor-old'],'only editor caches are cleared');
  console.log('Editor cache: fresh navigation, versioned assets, reload, no-store, offline, scope, and migration passed.');
})().catch(error=>{console.error(error);process.exitCode=1;});
