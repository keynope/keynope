const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const {waitForAsync}=require('./wait_for_async.cjs');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),output=path.resolve(process.env.TEST_OUTPUT||'/tmp/keynope-concrete-font-styles');
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));fs.mkdirSync(output,{recursive:true});
 for(const engine of ['chromium','webkit']){
  const browser=await playwright[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
  try{
   const context=await browser.newContext({viewport:{width:1440,height:1000}});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
   const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
   await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
   const content=`<!-- keynope width=245 height=56 -->

<!-- top=8 left=10 width=100 height=12 render=truetype default-font=mono default-size=80 default-width=75 default-line-height=1.4 -->
Frozen Mono source

<!-- top=30 left=120 width=100 height=12 render=truetype modern-font=theme modern-size=70 modern-width=55 -->
Keep recipient content
`;
   const picker=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await picker).setFiles({name:'Fixed font styles.md',mimeType:'text/markdown',buffer:Buffer.from(content)});
   const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
   await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[0].elements.some(e=>e.text==='Frozen Mono source');});
   await page.evaluate(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json()),theme=structuredClone(s.theme);theme.fonts.body.fontId='sans';theme.fonts.body.width=120;await editorAction({action:'set-deck-theme',themeData:theme});});
   const initial=await state(),source=initial.slides[0].elements.findIndex(e=>e.text==='Frozen Mono source'),target=initial.slides[0].elements.findIndex(e=>e.text==='Keep recipient content');
   const select=async index=>{await page.locator('.keynope-canvas-element[data-element="'+index+'"]').click();const tab=page.getByRole('tab').filter({hasText:/^(Text|Edit)$/}).first();if(await tab.count())await tab.click();};
   const font=page.getByRole('combobox',{name:'Font family',exact:true}),width=page.getByRole('spinbutton',{name:'TrueType width percent',exact:true});
   await select(source);assert.equal(await font.inputValue(),'mono');assert.equal(Number(await width.inputValue()),75);assert.equal((await state()).selectionCapabilities.properties['text.font'].fontBinding,'concrete');
   const size=page.getByRole('spinbutton',{name:'Font size',exact:true});
   for(const [button,property,input,expected,original]of [
    ['Increase font width','text.width',width,76,75],['Decrease font width','text.width',width,74,75],
    ['Increase font size','text.size',size,81,80],['Decrease font size','text.size',size,79,80],
   ]){
    await page.getByRole('button',{name:button,exact:true}).click();
    await waitForAsync(page,async({property,expected})=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities.properties[property].value===expected;},{property,expected});
    assert.equal(Number(await input.inputValue()),expected,'Control lags accepted '+property);
    await page.getByRole('button',{name:'Undo',exact:true}).click();assert.equal(Number(await input.inputValue()),original);
   }
   await page.getByRole('tab',{name:'Style',exact:true}).click();await page.getByRole('button',{name:'Copy style',exact:true}).click();
   const snapshot=(await state()).styleClipboard;assert.equal(snapshot.fields['text.font'].value,'mono');assert.equal(snapshot.fields['text.width'].value,'75');
   await select(target);assert.equal(await font.inputValue(),'theme');
   await page.getByRole('tab',{name:'Style',exact:true}).click();await page.getByRole('button',{name:'Paste style',exact:true}).click();
   await select(target);assert.equal(await font.inputValue(),'mono');assert.equal(Number(await width.inputValue()),75);assert.equal((await state()).slides[0].elements[target].text,'Keep recipient content');
   await select(source);await font.selectOption('c64');
   await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities.properties['text.font'].value==='c64';});
   assert(Math.abs(Number(await width.inputValue())-75/.5556)<1e-8);
   await page.getByRole('button',{name:'Undo',exact:true}).click();await select(source);assert.equal(await font.inputValue(),'mono');assert.equal(Number(await width.inputValue()),75);
   await page.screenshot({path:path.join(output,engine+'.png')});
   await page.evaluate(async()=>{const r=await fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'fixed-fonts.md'})});if(!r.ok)throw Error(await r.text());const d=await r.json(),opened=JSON.parse(keynopeWasmInit(d.content,'fixed-fonts.md',false));if(opened.status!==200)throw Error(opened.body);await keynopeReloadWebDocument(JSON.parse(JSON.parse(keynopeWasmWorkspace()).body));});
   await select(source);assert.equal(await font.inputValue(),'mono');assert.equal(Number(await width.inputValue()),75);assert.deepEqual(errors,[]);
   console.log('PASS '+engine+': concrete/theme font choices, fixed width, Copy/Paste, normalized switch, Undo and save/reopen');
  }finally{await browser.close();}
 }
 server.close();
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
