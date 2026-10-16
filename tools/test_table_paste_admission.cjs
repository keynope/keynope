// Real table draft surfaces: rejected clipboard input must preserve history.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright'),{waitForAsync}=require('./wait_for_async.cjs');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),engine=process.env.TEST_BROWSER||'webkit';
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const browser=await pw[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
 try{
  const page=await browser.newPage({viewport:{width:1600,height:1000}}),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());await page.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json())),action=data=>page.evaluate(data=>editorAction(data),data);
  const picker=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await picker).setFiles({name:'Paste.md',mimeType:'text/markdown',buffer:Buffer.from('<!-- keynope width=245 height=56 -->\n\nPaste tests\n')});await waitForAsync(page,async()=>(await fetch('/api/editor/state').then(r=>r.json())).path.endsWith('Paste.md'));
  let s=await state();await action({action:'insert-table',rows:3,cols:3,slide:s.current,sceneMaster:false,sceneRevision:s.version});s=await state();const table=s.slides[0].elements.find(e=>e.kind==='table');await action({action:'select-element',element:s.slides[0].elements.findIndex(e=>e.id===table.id)});
  await page.locator('.keynope-canvas-element[data-element-id="'+table.id+'"]').dblclick();let dialog=page.getByRole('dialog',{name:'Edit table cells',exact:true});await dialog.waitFor();await dialog.locator('[contenteditable=true]').press('Escape');
  const cell=dialog.getByRole('gridcell',{name:'Cell A1',exact:true});await cell.click();
  const paste=async(text)=>cell.evaluate((node,text)=>{const clipboardData=new DataTransfer();clipboardData.setData('text/plain',text);node.dispatchEvent(new ClipboardEvent('paste',{bubbles:true,cancelable:true,clipboardData}));},text);
  const initial=await state();await paste('Alpha\tBeta\nGamma\tDelta');const snapshot=await dialog.locator('.keynope-scene-table').textContent();assert.match(snapshot,/Alpha/);
  for(const bad of ['x'.repeat((1<<20)+1),'x'.repeat(1<<20),'😀'.repeat((1<<18)+1),Array(101).fill('x').join('\t')]){
   await cell.click();await paste(bad);await dialog.getByRole('alert').filter({hasText:/1 MiB|100 rows and columns/}).waitFor();assert.equal(await dialog.locator('.keynope-scene-table').textContent(),snapshot);assert.deepEqual((await state()).slides,initial.slides);
  }
  await dialog.getByRole('button',{name:'Undo cell change',exact:true}).click();assert.doesNotMatch(await dialog.locator('.keynope-scene-table').textContent(),/Alpha/);await dialog.getByRole('button',{name:'Redo cell change',exact:true}).click();assert.equal(await dialog.locator('.keynope-scene-table').textContent(),snapshot);
  await dialog.getByRole('button',{name:'Done',exact:true}).click();await dialog.waitFor({state:'detached'});s=await state();assert.equal(s.mutationRevision,initial.mutationRevision+1);assert.equal(s.selectedTable.data.cells[0][0].text,'Alpha');
  await page.evaluate(()=>{window.fullPasteResult=null;KeynopeTables.edit({version:1,rows:[{height:1}],columns:[{width:1},{width:1}],cells:[[{text:'Replace me'},{runs:[{text:'😀',bold:true}]}]],style:{}},data=>{fullPasteResult=data;});});dialog=page.getByRole('dialog',{name:'Edit table',exact:true});await dialog.waitFor();
  const values=()=>dialog.locator('[aria-label^="Cell "]').evaluateAll(nodes=>nodes.map(n=>n.value));const baseline=await values();const input=dialog.getByRole('textbox',{name:'Paste table data',exact:true});
  await input.fill('One,Two\nThree,Four');await dialog.getByRole('button',{name:'Paste data',exact:true}).click();const valid=await values();assert.notDeepEqual(valid,baseline);
  await dialog.getByRole('button',{name:'Undo',exact:true}).click();assert.deepEqual(await values(),baseline);
  for(const bad of ['x'.repeat((1<<20)+1),'x'.repeat(1<<20),Array(101).fill('x').join('\n')]){await input.fill(bad);await dialog.getByRole('button',{name:'Paste data',exact:true}).click();await dialog.getByRole('alert').filter({hasText:/1 MiB|100 rows and columns/}).waitFor();assert.deepEqual(await values(),baseline);}
  await dialog.getByRole('button',{name:'Redo',exact:true}).click();assert.deepEqual(await values(),valid,'Rejected paste destroyed redo');await dialog.getByRole('button',{name:'Apply',exact:true}).click();await dialog.waitFor({state:'detached'});assert.equal(await page.evaluate(()=>fullPasteResult.cells[0][0].text),'One');assert.deepEqual(errors,[]);
  console.log('PASS '+engine+': bounded table paste on canvas and full dialogs, unchanged draft/document after rejection, retained Undo/Redo and one final commit.');
 }finally{await browser.close();server.close();}
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
