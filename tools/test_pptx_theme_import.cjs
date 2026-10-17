// Owned native-PPTX theme import, old/new appearance, and actual WASM history.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http'),{execFileSync}=require('node:child_process');
const {waitForAsync}=require('./wait_for_async.cjs'),pw=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),proof=process.env.KEYNOPE_PPTX_THEME_IMPORT_EVIDENCE||'/tmp/keynope-pptx-theme-import',engine=process.env.TEST_BROWSER||'chromium';
execFileSync('go',['test','./internal/app', '-run','^TestPPTXImportedThemeIsEditableAndScopeOwned$','-count=1'],{cwd:path.resolve(__dirname,'..'),env:{...process.env,GOCACHE:process.env.GOCACHE||'/tmp/keynope-go-cache',KEYNOPE_PPTX_THEME_IMPORT_EVIDENCE:proof},stdio:'pipe'});
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const browser=await pw[engine].launch({headless:true,...(engine==='webkit'?{}:{executablePath:process.env.CHROME_PATH})});
 try{
  const context=await browser.newContext({viewport:{width:1280,height:800}});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles(path.join(proof,'source.pptx'));
  await waitForAsync(page,async()=>(await fetch('/api/editor/state').then(r=>r.json())).theme?.name==='Editable import');
  const report=page.getByRole('dialog').filter({hasText:'PowerPoint imported'});
  if(await report.isVisible())await report.getByRole('button',{name:'Close',exact:true}).click();
  const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));const before=await state();assert.equal(before.slides.length,2);
  const capture=async(scene,name)=>{
   await page.evaluate(async scene=>{const host=document.createElement('div');host.id='pptx-theme-proof';host.style.cssText='position:fixed;inset:0;background:#222;z-index:100000';document.body.append(host);const view=KeynopeScene.mount(host,scene);window.pptxThemeProof=view;const scale=Math.min(innerWidth/scene.width,innerHeight/scene.height);Object.assign(view.element.style,{transformOrigin:'0 0',transform:'scale('+scale+')',left:(innerWidth-scene.width*scale)/2+'px',top:(innerHeight-scene.height*scale)/2+'px'});await view.ready;await view.snapshotAt(0);},scene);
   const png=await page.locator('#pptx-theme-proof').screenshot({path:path.join(proof,name+'-'+engine+'.png'),animations:'disabled'});await page.evaluate(()=>{pptxThemeProof.dispose();document.querySelector('#pptx-theme-proof').remove();delete window.pptxThemeProof;});return png;
  };
  const compare=(a,b)=>page.evaluate(async({a,b})=>{const decode=async s=>{const i=new Image();i.src='data:image/png;base64,'+s;await i.decode();const c=document.createElement('canvas');c.width=i.width;c.height=i.height;const x=c.getContext('2d');x.drawImage(i,0,0);return x.getImageData(0,0,c.width,c.height).data;};const [x,y]=await Promise.all([decode(a),decode(b)]);let max=0,total=0,changed=0;for(let i=0;i<x.length;i++){const d=Math.abs(x[i]-y[i]);max=Math.max(max,d);total+=d;if(d)changed++;}return {max,mean:total/x.length,changed,channels:x.length};},{a:a.toString('base64'),b:b.toString('base64')});
  const baseline=JSON.parse(fs.readFileSync(proof+'/baseline.json')),attached=JSON.parse(fs.readFileSync(proof+'/attached.json')),changed=JSON.parse(fs.readFileSync(proof+'/changed.json')),results=[];
  for(let i=0;i<2;i++){
   const a=await capture(baseline[i],'baseline-'+i),b=await capture(attached[i],'attached-'+i),diff=await compare(a,b);results.push({slide:i,initial:diff});assert(diff.max<=1,'Theme attachment changed initial pixels: '+JSON.stringify(diff));
   const c=await capture(changed[i],'changed-'+i),edit=await compare(b,c);if(i===0)assert(edit.changed>100,'Primary theme edit has no visible consumers');else assert.equal(edit.max,0,'Primary edit changed foreign-master slide');
  }
  const editorBefore=await page.evaluate(async scene=>{
    const o=scene.objects.find(o=>o.kind==='text'),host=document.createElement('div');host.id='theme-caret-proof';host.style.cssText='position:fixed;inset:20px;background:white;z-index:100000';document.body.append(host);
    window.themeCaret=KeynopeTextLayout.createEditor(host,{...o.text,width:o.bounds.width,color:o.paint.color});await themeCaret.ready();const value=structuredClone(themeCaret.value());themeCaret.element.focus();themeCaret.select(8,8);return value;
  },attached[0]);
  await page.keyboard.type('X');
  const editorAfter=await page.evaluate(()=>({value:themeCaret.value(),selection:themeCaret.selection()}));assert.equal(editorAfter.value.runs.map(r=>r.text).join(''),'Linked LXiteral');assert.equal(editorAfter.value.runs[0].colorToken,'accent-1');assert.equal(editorAfter.value.runs[0].fontRole,'heading');assert(editorAfter.value.runs.filter(r=>!r.colorToken).some(r=>r.text.includes('X')),'Typing lost literal style at coalesced paint boundary');
  await page.evaluate(()=>themeCaret.undo());assert.deepEqual(await page.evaluate(()=>themeCaret.value()),editorBefore,'Local text undo lost source role boundaries');await page.evaluate(()=>{themeCaret.dispose();document.querySelector('#theme-caret-proof').remove();delete window.themeCaret;});
  await page.evaluate(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json()),theme=structuredClone(s.theme);theme.colors.accent=theme.colors.accent.map(()=>'#00ff88');theme.fonts.heading.fontId='c64';await editorAction({action:'set-deck-theme',themeData:theme});});
  const edited=await state();assert.notDeepEqual(edited.theme,before.theme);await page.evaluate(()=>editorAction({action:'undo'}));const undone=await state();assert.deepEqual(undone.theme,before.theme);assert.deepEqual(undone.slides,before.slides);await page.evaluate(()=>editorAction({action:'redo'}));assert.deepEqual((await state()).theme,edited.theme);
  assert.deepEqual(errors,[]);fs.writeFileSync(proof+'/report-'+engine+'.json',JSON.stringify(results,null,2));console.log('PASS '+engine+': real PPTX chooser/imported theme, old/new pixel parity, scoped palette/font changes and WASM undo/redo.');
 }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);server.close();process.exitCode=1;});
