// Actual WASM compatibility adapter and file Open, using owned privacy fixtures.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright'),engine=process.env.TEST_BROWSER||'chromium';
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),proof=process.env.KEYNOPE_AUDIENCE_PRIVACY_PROOF||'/tmp/keynope-audience-privacy-proof';
const cases=JSON.parse(fs.readFileSync(path.join(proof,'cases.json')));assert.equal(cases.length,3);
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const browser=await pw[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
 try{
  const page=await browser.newPage({viewport:{width:1600,height:1100}}),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',async dialog=>{assert.equal(dialog.type(),'confirm');await dialog.accept();});
  await page.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  for(const item of cases){
   const md=fs.readFileSync(path.join(proof,item.file.replace('.html','.md')),'utf8');
   const result=await page.evaluate(md=>{
    const response=JSON.parse(keynopeWasmRenderParticipantPage(md,0,4,12));if(response.status!==200)throw Error(response.body);
    const doc=new DOMParser().parseFromString(response.body,'text/html');return{data:JSON.parse(doc.querySelector('#keynope-data').textContent),private:response.body.includes('AUDIENCE_PRIVATE')};
   },md);
   assert.equal(result.private,false);assert.equal(result.data.pages.length,1);assert.equal(result.data.pages[0].slide,4);assert.equal(result.data.pages[0].slideCount,12);
   const publicPage=result.data.pages[0];
   for(const key of ['shapePorts','connectors','engagement'])assert.equal(publicPage[key],undefined,'Unexpected audience editor/activity field: '+key);
   const expected=item.scenes[0],actual=result.data.pages[0].scene;assert(actual);assert.equal(actual.width,expected.width);assert.equal(actual.height,expected.height);
   for(const key of ['revision','master','conversion'])assert.equal(actual[key],undefined,'Unexpected audience owner field: '+key);
   for(const object of actual.objects)for(const key of ['editable','editRuns','textCapabilities'])assert.equal(object[key],undefined,'Unexpected audience editing field: '+object.id+'/'+key);
   assert.deepEqual(actual.objects.map(o=>o.id),expected.objects.map(o=>o.id));
   for(const wanted of expected.objects){const got=actual.objects.find(o=>o.id===wanted.id);for(const key of ['bounds','text','label','paint','labelPaint','media','shapeGeometry','points'])assert.deepEqual(got[key],wanted[key],item.file+'/'+wanted.id+'/'+key);}
  }
  const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles({name:'Audience.md',mimeType:'text/markdown',buffer:fs.readFileSync(path.join(proof,'public.md'))});
  await page.waitForFunction(()=>deck.pages[0]?.scene?.objects.some(o=>o.id==='box'));
  const info=await page.evaluate(()=>({scene:deck.pages[0].scene,source:JSON.stringify(deck.pages[0].scene)}));
  assert(!info.source.includes('AUDIENCE_PRIVATE'));assert.equal(info.scene.objects.find(o=>o.id==='box').label.runs[0].color,'#ff0055');
  assert(info.scene.objects.find(o=>o.id==='title').textCapabilities.includes('size'),'Opening public Markdown lost editor typography capabilities');
  await page.screenshot({path:path.join(proof,engine+'-editor.png')});assert.deepEqual(errors,[]);
  console.log('PASS '+engine+': real WASM participant pages retain source artwork/flow and active label formatting without hidden content or private/stale metadata.');
 }finally{await browser.close();server.close();}
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
