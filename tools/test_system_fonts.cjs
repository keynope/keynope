// Production font dropdown and export decisions, without activating the app.
const assert=require('node:assert/strict'),fs=require('node:fs'),http=require('node:http'),path=require('node:path');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright'),engine=process.env.KEYNOPE_BROWSER_ENGINE||'webkit';
(async()=>{
 const browser=await pw[engine].launch({headless:true});let server;
 try{
  const page=await browser.newPage({viewport:{width:1440,height:1000}}),errors=[],output=fs.mkdtempSync('/tmp/keynope-font-dropdown-');
  page.on('pageerror',e=>errors.push(e.message));page.on('dialog',dialog=>dialog.accept());
  page.on('framenavigated',frame=>{if(frame===page.mainFrame())console.log('Document navigation: '+frame.url());});
  if(process.env.KEYNOPE_FONT_SITE){
   const root=path.resolve(process.env.KEYNOPE_FONT_SITE);
   server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
   await new Promise(r=>server.listen(0,'127.0.0.1',r));await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
   const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles(process.env.KEYNOPE_FONT_DECK_FIXTURE);await page.waitForFunction(()=>deck.pages[0].scene?.objects.some(o=>o.id==='font-text'));
  }else await page.goto(process.argv[2]);
  await page.waitForFunction(()=>typeof keynopeImportTypeface==='function');
  const action=data=>page.evaluate(data=>editorAction(data),data),state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json())),select=page.getByRole('combobox',{name:'Font family',exact:true});
  await action({action:'select-element',elementData:{id:'font-text'}});await page.getByRole('tab',{name:'Text',exact:true}).click();
  await page.evaluate(async()=>{await modernPageView.ready;});assert((await select.inputValue()).startsWith('font-'));
  await select.click();let dropdown=page.locator('.keynope-font-dropdown');await dropdown.waitFor();assert.equal(await page.getByRole('dialog',{name:'Choose font'}).count(),0);
  assert.equal(await dropdown.getByRole('button',{name:'Use font',exact:true}).count(),0);assert.equal(await dropdown.getByRole('checkbox').count(),0);
  const fileChooser=page.waitForEvent('filechooser');await dropdown.getByRole('button',{name:'Load from disk…',exact:true}).click();await(await fileChooser).setFiles(process.env.KEYNOPE_FONT_FIXTURE);
  await dropdown.waitFor({state:'detached'});await page.waitForFunction(()=>document.querySelector('[aria-label="Font family"]')?.selectedOptions[0]?.textContent==='Go Regular');
  await page.evaluate(async()=>{await modernPageView.ready;});
  const regularId=await select.inputValue();await action({action:'undo'});await page.getByRole('tab',{name:'Text',exact:true}).click();
  // Over 500 entries exposes the old 160-entry truncation. Preview two actual
  // faces with distinct metrics, not CSS names that may silently fall back.
  await page.evaluate(async regularId=>{
   const mono=Object.values(deck.pages[pageIndex].scene.typefaces)[0],regular=await fetch('/api/editor/typeface?id='+regularId).then(r=>r.json());window.fontMonoSource=mono;
   const bytes=Uint8Array.from(atob(mono.data),c=>c.charCodeAt(0)),view=new DataView(bytes.buffer);
   for(let i=0;i<view.getUint16(4);i++){const p=12+i*16;if(String.fromCharCode(...bytes.subarray(p,p+4))==='OS/2'){view.setUint16(view.getUint32(p+8)+8,2);break;}}
   let data='';for(let i=0;i<bytes.length;i+=8192)data+=String.fromCharCode(...bytes.subarray(i,i+8192));
   const restricted={...mono,data:btoa(data)},entries=Array.from({length:520},(_,i)=>({name:'Font '+String(i).padStart(3,'0'),family:'Font',postScriptName:'TestFont'+i}));
   entries.push({name:'Proportional preview',family:'Test',postScriptName:'ProportionalPreview'},{name:'Monospaced preview',family:'Test',postScriptName:'MonospacedPreview'},{name:'Restricted test',family:'Test',postScriptName:'RestrictedTest'},{name:'Zzz final font',family:'Zzz',postScriptName:'LastFont'});
   window.webkit={messageHandlers:{keynopeFonts:{postMessage:async m=>m.action==='catalogue'?entries:m.postScriptName==='ProportionalPreview'?regular:m.postScriptName==='RestrictedTest'?restricted:mono}}};
  },regularId);
  const before=await state();await select.click();dropdown=page.locator('.keynope-font-dropdown');await dropdown.evaluate(n=>window.sameFontDropdown=n);
  await dropdown.getByRole('button',{name:'More fonts…',exact:true}).click();
  await dropdown.getByRole('option',{name:'Zzz final font',exact:true}).waitFor();assert(await dropdown.getByRole('option').count()>520);assert(await dropdown.evaluate(n=>n===window.sameFontDropdown),'More fonts opened a different control');
  await dropdown.getByRole('option',{name:'Zzz final font',exact:true}).scrollIntoViewIfNeeded();assert(await dropdown.getByRole('option',{name:'Zzz final font',exact:true}).isVisible());
  const search=dropdown.getByRole('searchbox',{name:'Search fonts',exact:true});await search.fill('preview');
  const preview=dropdown.getByLabel('Font preview');
  const metricCheck=async mono=>page.waitForFunction(mono=>{const node=document.querySelector('[aria-label="Font preview"]'),f=node?.style.fontFamily;if(!f?.startsWith('KeynopeFontPreview_')||getComputedStyle(node).fontFamily!==f||!document.fonts.check('32px '+f))return false;const c=document.createElement('canvas').getContext('2d');c.font='32px '+f;const a=c.measureText('iiiiiiii').width,b=c.measureText('WWWWWWWW').width;return mono?Math.abs(a-b)<.01:b>a*2;},mono);
  await dropdown.getByRole('option',{name:'Proportional preview',exact:true}).hover();await metricCheck(false);const proportional=await preview.evaluate(n=>n.style.fontFamily);
  await dropdown.getByRole('option',{name:'Monospaced preview',exact:true}).hover();await metricCheck(true);assert.notEqual(await preview.evaluate(n=>n.style.fontFamily),proportional);
  await page.screenshot({path:output+'/'+engine+'-expanded-preview.png'});assert.equal((await state()).version,before.version,'Preview edited the deck');
  await search.fill('restricted');await dropdown.getByRole('option',{name:'Restricted test',exact:true}).click();await dropdown.waitFor({state:'detached'});
  await page.waitForFunction(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[0].elements[0].query.includes(document.querySelector('[aria-label="Font family"]')?.value)&&s.version>0;});
  await page.evaluate(async()=>{await modernPageView.ready;});const restrictedId=await select.inputValue();assert.notEqual(restrictedId,before.slides[0].elements[0].query.match(/modern-font=([^&]+)/)?.[1]);
  assert.equal(await page.getByRole('dialog',{name:'Restricted fonts',exact:true}).count(),0,'Import asked for embedding rights');
  const restrictedFace=await page.evaluate(id=>fetch('/api/editor/typeface?id='+id).then(r=>r.json()),restrictedId);assert(restrictedFace.localOnly);assert(!restrictedFace.data);assert.equal(restrictedFace.embeddingFlags,2);
  const startExport=async format=>{await page.evaluate(format=>{window.fontExportResult=null;keynopeConfirmFontExport(format).then(result=>window.fontExportResult=result);},format);await page.getByRole('dialog',{name:'Restricted fonts',exact:true}).waitFor();};
  await startExport('pptx');let warning=page.getByRole('dialog',{name:'Restricted fonts',exact:true});assert(await warning.locator('li').count()>=1);assert(await warning.getByRole('button',{name:'Export',exact:true}).isEnabled());assert.equal(await warning.getByRole('checkbox').count(),0);
  await warning.getByRole('button',{name:'Cancel',exact:true}).click();await page.waitForFunction(()=>window.fontExportResult===false);
  assert.equal(await page.evaluate(()=>fetch('/api/editor/export-pptx',{method:'POST'}).then(r=>r.status)),428,'Cancelled export was approved');
  await startExport('pptx');warning=page.getByRole('dialog',{name:'Restricted fonts',exact:true});await page.screenshot({path:output+'/'+engine+'-export-references.png'});
  await warning.getByRole('button',{name:'Export',exact:true}).click();await page.waitForFunction(()=>window.fontExportResult===true);
  const exported=await page.evaluate(async()=>{const r=await fetch('/api/editor/export-pptx',{method:'POST'});if(!r.ok)throw Error(await r.text());return r.json();});assert(exported.content);assert(exported.report.warnings.some(w=>w.code==='font-not-embedded'));
  await startExport('html');warning=page.getByRole('dialog',{name:'Restricted fonts',exact:true});await warning.getByRole('button',{name:'Replace fonts…',exact:true}).click();assert(await warning.getByRole('combobox').count()>=1);
  await warning.getByRole('button',{name:'Export',exact:true}).click();await page.waitForFunction(()=>window.fontExportResult===true);
  const html=await page.evaluate(async()=>{const r=await fetch('/api/editor/export-document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Replaced.html'})});if(!r.ok)throw Error(await r.text());return (await r.json()).content;});assert(!html.includes(restrictedId),'Restricted font survived replacement');assert((await state()).slides[0].elements[0].query.includes(restrictedId),'Export replacement changed authored deck');
  await startExport('save');warning=page.getByRole('dialog',{name:'Restricted fonts',exact:true});assert(await warning.getByRole('button',{name:'Save',exact:true}).isEnabled());await warning.getByRole('button',{name:'Cancel',exact:true}).click();await page.waitForFunction(()=>window.fontExportResult===false);
  await startExport('save');warning=page.getByRole('dialog',{name:'Restricted fonts',exact:true});await warning.getByRole('button',{name:'Save',exact:true}).click();await page.waitForFunction(()=>window.fontExportResult===true);
  const saved=await page.evaluate(async()=>{const r=await fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Restricted.md'})});if(!r.ok)throw Error(await r.text());return r.json();});assert(saved.content.includes('keynope-typefaces'));
  await page.evaluate(async()=>{const font=window.fontMonoSource,bytes=Uint8Array.from(atob(font.data),c=>c.charCodeAt(0)),view=new DataView(bytes.buffer);for(let i=0;i<view.getUint16(4);i++){const p=12+i*16;if(String.fromCharCode(...bytes.subarray(p,p+4))==='OS/2'){view.setUint16(view.getUint32(p+8)+8,4);break;}}let raw='';for(let i=0;i<bytes.length;i+=8192)raw+=String.fromCharCode(...bytes.subarray(i,i+8192));const id=await keynopeImportTypeface({...font,data:btoa(raw)},keynopeFontOwner());const s=await fetch('/api/editor/state').then(r=>r.json());await editorAction({action:'set-modern-text-style',objectIds:['font-text'],sceneRevision:s.version,modernFont:id});});
  await page.getByRole('tab',{name:'Text',exact:true}).click();await page.waitForFunction(()=>deck.pages[pageIndex].scene.objects.find(o=>o.id==='font-text')?.text?.readOnly===true);assert(await page.getByRole('button',{name:'Edit text',exact:true}).isDisabled());await page.screenshot({path:output+'/'+engine+'-preview-only-text.png'});
  await select.selectOption('sans');await page.waitForFunction(()=>!deck.pages[pageIndex].scene.objects.find(o=>o.id==='font-text')?.text?.readOnly);assert(await page.getByRole('button',{name:'Edit text',exact:true}).isEnabled());
  assert.deepEqual(errors,[]);console.log('PASS '+engine+': full font list, actual previews, immediate selection, restricted references, save/export confirmation and cancellation, export-only replacement, text-local Preview & Print lock/unlock. '+output);
 }finally{await browser.close();if(server)await new Promise(resolve=>server.close(resolve));}
})().catch(error=>{console.error(error);process.exitCode=1;});
