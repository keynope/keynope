// Actual native-PPTX import, attached routes, palette ownership and history.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http'),{execFileSync}=require('node:child_process');
const {waitForAsync}=require('./wait_for_async.cjs'),pw=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),proof=process.env.KEYNOPE_CONNECTOR_THEME_EVIDENCE||'/tmp/keynope-connector-theme-proof',engine=process.env.TEST_BROWSER||'chromium';
execFileSync('go',['test','.', '-run','^TestAttachedConnectorThemeNativePackageAndHistory$','-count=1'],{cwd:path.resolve(__dirname,'..'),env:{...process.env,GOCACHE:process.env.GOCACHE||'/tmp/keynope-go-cache',KEYNOPE_CONNECTOR_THEME_EVIDENCE:proof},stdio:'pipe'});
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const browser=await pw[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
 try{
  const page=await browser.newPage({viewport:{width:1440,height:1000}}),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());await page.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles(path.join(proof,'source.pptx'));
  await waitForAsync(page,async()=>(await fetch('/api/editor/state').then(r=>r.json())).path.endsWith('source.md')&&deck.pages.length===4);
  const report=page.getByRole('dialog').filter({hasText:'PowerPoint imported'});if(await report.isVisible())await report.getByRole('button',{name:'Close',exact:true}).click();
  const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json())),snapshot=()=>page.evaluate(()=>deck.pages.map(p=>{const s=structuredClone(p.scene);delete s.revision;return s;}));
  const initial=await snapshot(),s=await state(),connector=s.slides[0].elements.find(e=>e.kind==='connector');assert(connector);assert.equal(new URLSearchParams(connector.query).get('theme-fg'),'accent-1');
  const theme=structuredClone(s.theme);theme.colors.accent[0]='#00ff88';await page.evaluate(theme=>editorAction({action:'set-deck-theme',themeData:theme}),theme);
  await page.waitForFunction(()=>deck.pages[0].scene.objects.find(o=>o.kind==='connector').paint.color==='#00ff88');
  const changed=await snapshot();for(let i=1;i<4;i++)assert.deepEqual(changed[i],initial[i],'Theme edit changed foreign/literal/transformed route '+i);
  await page.locator('.keynope-engagement-toast').waitFor({state:'detached'});await page.locator('#stage').screenshot({path:proof+'/primary-themed-'+engine+'.png'});
  const editedState=await state(),from=new URLSearchParams(connector.query).get('connector-from'),index=editedState.slides[0].elements.findIndex(e=>e.id===from),body=structuredClone(editedState.slides[0].elements[index]);assert(index>=0);
  const q=new URLSearchParams(body.query);q.set('object-offset-x',String(Number(q.get('object-offset-x')||0)+2));body.query=q.toString();
  const first=changed[0].objects.find(o=>o.kind==='connector').points[0];await page.evaluate(({body,index,version})=>editorAction({action:'update-elements',slide:0,sceneMaster:false,sceneRevision:version,elementIndices:[index],elementsData:[body]}),{body,index,version:editedState.version});
  await page.waitForFunction(first=>{const line=deck.pages[0].scene.objects.find(o=>o.kind==='connector');return Math.hypot(line.points[0].x-first.x,line.points[0].y-first.y)>1;},first);
  const moved=await snapshot();assert.equal(moved[0].objects.find(o=>o.kind==='connector').paint.color,'#00ff88');
  await page.evaluate(()=>editorAction({action:'undo'}));assert.deepEqual(await snapshot(),changed,'Moving a themed endpoint did not undo exactly');await page.evaluate(()=>editorAction({action:'undo'}));assert.deepEqual(await snapshot(),initial,'Theme Undo did not restore route scenes');
  await page.evaluate(()=>editorAction({action:'redo'}));await page.evaluate(()=>editorAction({action:'redo'}));assert.deepEqual(await snapshot(),moved,'Theme/endpoint Redo drifted');
  const content=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Routes.md'})}).then(r=>r.json()).then(r=>r.content));
  const picker=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await picker).setFiles({name:'Routes.md',mimeType:'text/markdown',buffer:Buffer.from(content)});
  await waitForAsync(page,async()=>(await fetch('/api/editor/state').then(r=>r.json())).path.endsWith('Routes.md')&&deck.pages.length===4);assert.deepEqual(await snapshot(),moved,'Markdown reopen lost themed attached routes');
  assert.deepEqual(errors,[]);console.log('PASS '+engine+': native themed connector import, three scope controls, moved attachment, full history and Markdown reopen.');
 }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);server.close();process.exitCode=1;});
