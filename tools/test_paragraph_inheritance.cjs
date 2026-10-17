// Shared inspector/WASM paragraph values and inheritance, without opening the Mac app.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http'),{execFileSync}=require('node:child_process');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright'),{waitForAsync}=require('./wait_for_async.cjs');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),engine=process.env.TEST_BROWSER||'chromium',proof='/tmp/keynope-paragraph-'+engine;
execFileSync('go',['test','./internal/app','-run','^TestParagraphBrowserFixture$','-count=1'],{env:{...process.env,GOCACHE:process.env.GOCACHE||'/tmp/keynope-go-cache',KEYNOPE_PARAGRAPH_PROOF:proof},stdio:'pipe'});
const server=http.createServer((req,res)=>{
 let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';
 const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}
 res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));
});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const browser=await pw[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
 try{
  const page=await browser.newPage({viewport:{width:1600,height:1100}}),errors=[];page.setDefaultTimeout(20000);page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
  await page.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
  const open=async(name,content=fs.readFileSync(path.join(proof,name)))=>{
   const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();
   await(await chooser).setFiles({name,mimeType:'text/markdown',buffer:Buffer.from(content)});
   await waitForAsync(page,async name=>(await fetch('/api/editor/state').then(r=>r.json())).path.endsWith(name),name);
  };
  const select=async(id,tab='Text')=>{await page.locator('.keynope-canvas-element[data-element-id="'+id+'"]').click();await page.getByRole('tab',{name:tab,exact:true}).click();};
  const check=async(prefix,before,after,line)=>{
   await waitForAsync(page,async({prefix,before,after,line})=>{
    const s=await fetch('/api/editor/state').then(r=>r.json()),p=s.selectionCapabilities?.properties;
    return p?.[prefix+'.paragraphBefore']?.value===before&&p?.[prefix+'.paragraphAfter']?.value===after&&(line===undefined||p?.[prefix+'.lineHeight']?.value===line);
   },{prefix,before,after,line});
   const id=prefix==='label'?'label-a':'child-text';
   await waitForAsync(page,({id,before,after})=>{
    const rows=[...document.querySelectorAll('#stage [data-object-id="'+id+'"] .keynope-scene-paragraph')];
    return rows.length&&rows.every(row=>{
     const top=parseFloat(row.style.paddingTop),bottom=parseFloat(row.style.paddingBottom);
     return before===0?top===0:top>0;
    })&&rows.every(row=>after===0?parseFloat(row.style.paddingBottom)===0:parseFloat(row.style.paddingBottom)>0);
   },{id,before,after});
  };
  await open('paragraphs.md');await select('child-text');await check('text',.4,.7,1.8);
  const masters=(await state()).masters;
  for(const caption of ['Before (em)','After (em)']){
   const old=await state();await page.getByRole('spinbutton',{name:caption,exact:true}).fill('0');await page.getByRole('spinbutton',{name:caption,exact:true}).press('Tab');
   await waitForAsync(page,async version=>(await fetch('/api/editor/state').then(r=>r.json())).version===version+1,old.version);
  }
  await check('text',0,0,1.8);assert.deepEqual((await state()).masters,masters);
  await page.locator('#stage').screenshot({path:path.join(proof,'explicit-zero.png')});
  const save=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Saved.md'})}).then(r=>r.json()).then(r=>r.content));
  await open('Saved.md',save);await select('child-text');await check('text',0,0,1.8);
  const old=await state();await page.getByRole('spinbutton',{name:'Before (em)',exact:true}).fill('');await page.getByRole('spinbutton',{name:'Before (em)',exact:true}).press('Tab');await check('text',.4,0,1.8);
  assert.equal((await state()).version,old.version+1);
  await page.getByRole('button',{name:'Undo',exact:true}).click();await check('text',0,0,1.8);
  await page.getByRole('button',{name:'Redo',exact:true}).click();await check('text',.4,0,1.8);
  await page.getByRole('spinbutton',{name:'After (em)',exact:true}).fill('');await page.getByRole('spinbutton',{name:'After (em)',exact:true}).press('Tab');await check('text',.4,.7,1.8);
  await page.getByRole('spinbutton',{name:'Line spacing (×)',exact:true}).fill('1.25');await page.getByRole('spinbutton',{name:'Line spacing (×)',exact:true}).press('Tab');await check('text',.4,.7,1.25);
  await page.getByRole('spinbutton',{name:'Line spacing (×)',exact:true}).fill('');await page.getByRole('spinbutton',{name:'Line spacing (×)',exact:true}).press('Tab');await check('text',.4,.7,1.8);
  await page.locator('#stage').screenshot({path:path.join(proof,'inherited.png')});
  await open('labels.md');await select('label-a','Style');await check('label',.4,.7);
  await page.locator('details[data-selection-context="shape-label"] > summary').click();
  for(const name of ['Selection label space before (em)','Selection label space after (em)']){
   await page.getByRole('spinbutton',{name,exact:true}).fill('0');await page.getByRole('spinbutton',{name,exact:true}).press('Tab');
  }
  await check('label',0,0);
  await page.getByRole('button',{name:'Inherit label space before (em)',exact:true}).click();await check('label',.4,0);
  await page.getByRole('button',{name:'Inherit label space after (em)',exact:true}).click();await check('label',.4,.7);
  assert.deepEqual(errors,[]);
  console.log('PASS '+engine+': numeric zero, blank inheritance, retained master, saved Markdown, Undo/Redo, line-height and label controls/CSS. '+proof);
 }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);server.close();process.exitCode=1;});
