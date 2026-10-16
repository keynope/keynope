// Native package styles through the actual Open chooser, renderer and history.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http'),{execFileSync}=require('node:child_process');
const {waitForAsync}=require('./wait_for_async.cjs'),pw=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),proof=process.env.KEYNOPE_TABLE_STYLE_IMPORT_EVIDENCE||'/tmp/keynope-table-style-proof',engine=process.env.TEST_BROWSER||'chromium';
execFileSync('go',['test','./internal/pptx','-run','^TestTableStylePackageRelationshipAndRecovery$','-count=1'],{cwd:path.resolve(__dirname,'..'),env:{...process.env,GOCACHE:process.env.GOCACHE||'/tmp/keynope-go-cache',KEYNOPE_TABLE_STYLE_IMPORT_EVIDENCE:proof},stdio:'pipe'});
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const browser=await pw[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
 try{
  const page=await browser.newPage({viewport:{width:1440,height:1000}}),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());await page.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles(path.join(proof,'table-style.pptx'));
  await waitForAsync(page,async()=>(await fetch('/api/editor/state').then(r=>r.json())).path.endsWith('table-style.md')&&deck.pages[0]?.scene?.objects.some(o=>o.kind==='table'));
  const report=page.getByRole('dialog').filter({hasText:'PowerPoint imported'});if(await report.isVisible())await report.getByRole('button',{name:'Close',exact:true}).click();
  const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json())),table=()=>page.evaluate(()=>deck.pages[0].scene.objects.find(o=>o.kind==='table').table);
  const initial=await table(),before=await state();assert.equal(initial.cells.length,9);
  const expected=[['#777777','#666666','#666666'],['#555555','#444444','#444444'],['#555555','#333333','#333333']];for(const cell of initial.cells)assert.equal(cell.fill.toLowerCase(),expected[cell.row][cell.column]);
  await page.locator('.keynope-engagement-toast').waitFor({state:'detached'});
  await page.locator('#stage').screenshot({path:proof+'/styled-'+engine+'.png'});
  const theme=structuredClone(before.theme);theme.colors.accent[1]='#ff33aa';await page.evaluate(theme=>editorAction({action:'set-deck-theme',themeData:theme}),theme);
  await page.waitForFunction(()=>deck.pages[0].scene.objects.find(o=>o.kind==='table').table.cells.find(c=>c.row===0&&c.column===1).paint.color==='#ff33aa');
  const changed=await table();assert.equal(changed.cells.find(c=>c.row===0&&c.column===1).fill,'#666666');
  await page.evaluate(()=>editorAction({action:'undo'}));await waitForAsync(page,async initial=>JSON.stringify(deck.pages[0].scene.objects.find(o=>o.kind==='table').table)===JSON.stringify(initial),initial);
  await page.evaluate(()=>editorAction({action:'redo'}));await waitForAsync(page,async changed=>JSON.stringify(deck.pages[0].scene.objects.find(o=>o.kind==='table').table)===JSON.stringify(changed),changed);
  const content=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Styles.md'})}).then(r=>r.json()).then(r=>r.content));
  const reopen=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await reopen).setFiles({name:'Styles.md',mimeType:'text/markdown',buffer:Buffer.from(content)});
  await waitForAsync(page,async changed=>(await fetch('/api/editor/state').then(r=>r.json())).path.endsWith('Styles.md')&&JSON.stringify(deck.pages[0].scene.objects.find(o=>o.kind==='table').table)===JSON.stringify(changed),changed);
  assert.deepEqual(errors,[]);console.log('PASS '+engine+': package table styles, conditional paints, live source text colour, literal fills, theme undo/redo and Markdown reopen.');
 }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);server.close();process.exitCode=1;});
