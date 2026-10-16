// Shared payload/default formatting proof, not the unfinished full Detach UI.
// Generate: KEYNOPE_SHARED_DETACH_FIXTURE=/tmp/keynope-shared-detach go test . -run TestConcreteThemeSharedMasterPayloads
const fs=require('node:fs'),path=require('node:path'),http=require('node:http'),assert=require('node:assert/strict');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright'),engine=process.env.TEST_BROWSER||'chromium';
const site=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),fixtures=process.env.TEST_DECK_DIR||'/tmp/keynope-shared-detach',output=process.env.TEST_OUTPUT||'/tmp/keynope-shared-detach-proof';
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(site,'.'+name);if(!file.startsWith(site+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
const normalize=scene=>{
 const copy=structuredClone(scene);delete copy.revision;
 const text=t=>{if(!t)return;delete t.fontRoles;delete t.colorTokens;for(const runs of [t.runs,...(t.paragraphs||[]).map(p=>p.runs)])for(const r of runs||[]){delete r.fontRole;delete r.colorToken;}};
 for(const o of copy.objects){for(const c of o.table?.cells||[])text(c.text);if(o.chart)delete o.chart.data;}
 return copy;
};
(async()=>{
 fs.mkdirSync(output,{recursive:true});await new Promise(r=>server.listen(0,'127.0.0.1',r));
 const browser=await playwright[engine].launch({headless:true,...(engine==='webkit'?{}:{executablePath:process.env.CHROME_PATH})});
 try {
  const context=await browser.newContext({viewport:{width:1440,height:1000},reducedMotion:'reduce'});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
  const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const scenes=[],captures=[];
  for(const name of ['source','concrete']) {
   await page.evaluate(({content,name})=>{const result=JSON.parse(keynopeWasmInit(content,name+'.md',false));if(result.status!==200)throw Error(result.body);},{name,content:fs.readFileSync(path.join(fixtures,name+'.md'),'utf8')});
   const state=await page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));assert.equal(state.slides.length,3);assert.equal(state.masters.base.slide.elements.find(e=>e.kind==='table').objectId,'shared-table');assert.equal(state.masters.base.slide.elements.find(e=>e.kind==='chart').objectId,'shared-chart');for(const slide of state.slides)assert.equal(slide.elements[0].objectId,'shared-chart');
   for(let i=0;i<3;i++) {
    const scene=await page.evaluate(async i=>{const r=await fetch('/api/editor/scene?slide='+i);if(!r.ok)throw Error(await r.text());return r.json();},i);
    assert.equal(scene.objects.length,3);if(name==='source')scenes.push(normalize(scene));else assert.deepEqual(normalize(scene),scenes[i]);
    await page.evaluate(async scene=>{const host=document.createElement('div');host.id='theme-proof';host.style.cssText='position:fixed;inset:0;background:#111;z-index:100000';document.body.append(host);const view=KeynopeScene.mount(host,scene);window.themeProof=view;const scale=Math.min(innerWidth/scene.width,innerHeight/scene.height);Object.assign(view.element.style,{transformOrigin:'0 0',transform:'scale('+scale+')',left:(innerWidth-scene.width*scale)/2+'px',top:(innerHeight-scene.height*scale)/2+'px'});await view.ready;await view.snapshotAt(0);},scene);
    const png=await page.locator('#theme-proof').screenshot({path:path.join(output,name+'-'+i+'.png'),animations:'disabled'});
    if(name==='source')captures.push(png);else assert.deepEqual(png,captures[i],'Rendered shared-owner pixels changed on slide '+i);
    await page.evaluate(()=>{themeProof.dispose();document.querySelector('#theme-proof').remove();delete window.themeProof;});
   }
  }
  assert.deepEqual(errors,[]);console.log('PASS: shared master/payload inheritance, concrete defaults, resolved scenes and exact rendered pixels');
 } finally {await browser.close();server.close();}
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
