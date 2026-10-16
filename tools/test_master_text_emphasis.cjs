// Actual shared inspector + WASM persistence for master-bound text emphasis.
// Synthetic local fixture only; never launches the user's Mac application.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http'),{execFileSync}=require('node:child_process');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright'),{waitForAsync}=require('./wait_for_async.cjs');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),engine=process.env.TEST_BROWSER||'chromium',proof='/tmp/keynope-master-emphasis-'+engine;
execFileSync('go',['test','.','-run','^TestMasterTextEmphasisBrowserFixture$','-count=1'],{env:{...process.env,GOCACHE:process.env.GOCACHE||'/tmp/keynope-go-cache',KEYNOPE_EMPHASIS_PROOF:proof},stdio:'pipe'});
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const browser=await pw[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
 try{
  const page=await browser.newPage({viewport:{width:1600,height:1100}}),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());await page.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
  const open=async(name,content=fs.readFileSync(path.join(proof,name)))=>{const picker=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await picker).setFiles({name,mimeType:'text/markdown',buffer:Buffer.from(content)});await waitForAsync(page,async name=>(await fetch('/api/editor/state').then(r=>r.json())).path.endsWith(name)&&!!document.querySelector('#stage [data-object-id="child-text"]'),name);};
  const select=async()=>{await page.locator('.keynope-canvas-element[data-element-id="child-text"]').click();await page.getByRole('tab',{name:'Text',exact:true}).click();};
  const names=['Bold','Italic','Underline'];
  const check=async expected=>{
   await waitForAsync(page,async expected=>{const s=await fetch('/api/editor/state').then(r=>r.json());return ['text.bold','text.italic','text.underline'].every((key,i)=>s.selectionCapabilities?.properties[key]?.value===expected[i]);},expected);
   for(let i=0;i<3;i++)assert.equal(await page.getByRole('button',{name:names[i]+' text',exact:true}).getAttribute('aria-pressed'),String(expected[i]));
   const rendered=await page.locator('#stage [data-object-id="child-text"]').evaluate(node=>[...node.querySelectorAll('span')].filter(n=>[...n.childNodes].some(c=>c.nodeType===Node.TEXT_NODE&&c.textContent.trim())).map(n=>{const s=getComputedStyle(n);return {bold:Number(s.fontWeight)>=600,italic:s.fontStyle==='italic',underline:s.textDecorationLine.includes('underline'),color:s.color};}));
   assert(rendered.length,'No rendered text spans');for(const run of rendered){assert.deepEqual([run.bold,run.italic,run.underline],expected);assert.equal(run.color,'rgb(170, 51, 85)');}
  };
  await open('inherited.md');await select();await check([true,true,true]);const masters=(await state()).masters;
  for(let i=0;i<3;i++){
   const before=await state(),expected=[true,true,true];expected[i]=false;
   await page.getByRole('button',{name:names[i]+' text',exact:true}).click();await check(expected);assert.equal((await state()).version,before.version+1);assert.deepEqual((await state()).masters,masters);
   await page.getByRole('button',{name:'Undo',exact:true}).click();await check([true,true,true]);await page.getByRole('button',{name:'Redo',exact:true}).click();await check(expected);await page.getByRole('button',{name:'Undo',exact:true}).click();await check([true,true,true]);
  }
  for(let i=0;i<3;i++)await page.getByRole('button',{name:names[i]+' text',exact:true}).click();await check([false,false,false]);
  await page.locator('#stage').screenshot({path:path.join(proof,'all-off.png')});
  const saved=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Saved.md'})}).then(r=>r.json()).then(r=>r.content));await open('Saved.md',saved);await select();await check([false,false,false]);
  const reset=async()=>{await page.getByRole('tab',{name:'Style',exact:true}).click();await page.getByRole('button',{name:'Reset style',exact:true}).click();await page.getByRole('tab',{name:'Text',exact:true}).click();await check([true,true,true]);};
  await reset();
  for(let i=0;i<3;i++){
   const property=['text.bold','text.italic','text.underline'][i];await page.evaluate(async property=>{const s=await fetch('/api/editor/state').then(r=>r.json());return editorAction({action:'paste-object-style',slide:0,sceneMaster:false,sceneRevision:s.version,objectIds:['child-text'],styleData:{version:1,fields:{[property]:{present:true,value:'false'}}}});},property);const expected=[true,true,true];expected[i]=false;await check(expected);await reset();
  }
  await page.locator('#stage').screenshot({path:path.join(proof,'reset-inherited.png')});assert.deepEqual(errors,[]);
  console.log('PASS '+engine+': inherited B/I/U Off via real controls and typed style paste; other emphasis/colour retained, one command, Undo/Redo, Reset and Markdown reopen. '+proof);
 }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);server.close();process.exitCode=1;});
