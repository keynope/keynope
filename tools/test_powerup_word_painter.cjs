const {waitForAsync}=require("./wait_for_async.cjs");
// Owned document, actual WASM/DOM pointer selections. No cloud traffic, native
// activation, filesystem preferences or system clipboard.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-word-painter-site'),output=process.env.KEYNOPE_WORD_STYLE_EVIDENCE||'/tmp/keynope-word-style-proof';
let exported='';
const server=http.createServer((req,res)=>{
  let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';
  if(name==='/painted.html'){res.setHeader('Content-Type','text/html');return res.end(exported);}
  const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}
  res.setHeader('Content-Type',({'.html':'text/html','.js':'text/javascript','.css':'text/css','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));
});
(async()=>{
  await new Promise(r=>server.listen(0,'127.0.0.1',r));const browser=await playwright.chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
  try{
    const context=await browser.newContext({viewport:{width:1600,height:1150}});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
    const page=await context.newPage(),errors=[];page.setDefaultTimeout(25000);page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
    await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
    const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
    const open=async(content=null,name='painters.md')=>{
      const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();const input=await chooser;
      if(content===null)await input.setFiles(path.join(output,name));else await input.setFiles({name,mimeType:'text/markdown',buffer:Buffer.from(content)});
      await waitForAsync(page,async name=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.path.endsWith(name)&&s.version===1&&window.keynopeWorkspaceAppliedRevision===1&&deck.pages[0]?.scene?.objects.some(o=>o.id==='destination');},name);
    };
    const dialog=page.locator('dialog[data-keynope-scene-dialog="text"]'),notice=page.getByRole('complementary',{name:'Word painter status',exact:true});
    const edit=async id=>{await page.locator('.keynope-canvas-element[data-element-id="'+id+'"]').dblclick();await dialog.waitFor();};
    const select=async(start,end)=>{
      await page.evaluate(([a,b])=>{const root=document.querySelector('dialog[data-keynope-scene-dialog="text"] [contenteditable=true]');root.focus();const walker=document.createTreeWalker(root,NodeFilter.SHOW_TEXT),nodes=[];let node,offset=0;while(node=walker.nextNode()){nodes.push({node,start:offset,end:offset+node.length});offset+=node.length;}const point=x=>{const n=nodes.find(n=>x<=n.end)||nodes.at(-1);return [n.node,x-n.start];};document.getSelection().setBaseAndExtent(...point(a),...point(b));},[start,end]);
      await page.waitForFunction(([start,end])=>{const section=document.querySelector('fieldset[aria-label="Word formatting"]');return section?.getAttribute('aria-busy')==='false'&&section.dataset.rangeStart===String(start)&&section.dataset.rangeEnd===String(end)&&window.keynopeStyleCommandState()['style.copy'];},[start,end]);
    };
    const mouseWord=async(start,end,waitForApply=true)=>{
      const point=await page.evaluate(([start,end])=>{
        const root=document.querySelector('dialog[data-keynope-scene-dialog="text"] [contenteditable=true]'),walker=document.createTreeWalker(root,NodeFilter.SHOW_TEXT);let node,offset=0;
        while(node=walker.nextNode()){if(offset<=start&&offset+node.length>=end){const r=document.createRange();r.setStart(node,start-offset);r.setEnd(node,end-offset);const b=r.getBoundingClientRect();return {x:b.x+b.width/2,y:b.y+b.height/2};}offset+=node.length;}throw Error('Missing word range');
      },[start,end]);
      await page.mouse.dblclick(point.x,point.y);
      if(waitForApply)await page.waitForFunction(([start,end])=>{const section=document.querySelector('fieldset[aria-label="Word formatting"]');return document.querySelector('input[aria-label="Word font size"]')?.value==='80'&&section?.getAttribute('aria-busy')==='false'&&section.dataset.rangeStart===String(start)&&section.dataset.rangeEnd===String(end);},[start,end]);
    };
    const arm=async()=>{await dialog.getByRole('button',{name:'Word format painter',exact:true}).click();await page.waitForFunction(()=>document.querySelector('button[aria-label="Word format painter"]')?.getAttribute('aria-pressed')==='true');};
    const apply=async()=>{await dialog.getByRole('button',{name:'Apply text',exact:true}).click();await dialog.waitFor({state:'detached'});};
    const model=async id=>page.evaluate(id=>{const o=deck.pages[0].scene.objects.find(o=>o.id===id);return o.text||o.label;},id);
    const compareOwner=(before,after)=>{
      for(const key of ['id','kind','objectId','path','assetId'])assert.equal(after[key],before[key]);
      const a=new URLSearchParams(before.query),b=new URLSearchParams(after.query);
      for(const key of ['left','top','width','height','link','object-rotation','modern-font','modern-size','fg','shape'])assert.equal(b.get(key),a.get(key),key+' changed');
    };
    await open();const original=await state();
    await edit('words');await select(0,5);const captured=await state();await arm();assert.equal((await state()).version,captured.version);
    // Painting the source itself is a legitimate no-op, and still consumes
    // a one-shot painter. Never key eligibility solely by an old range.
    await mouseWord(0,5);await page.waitForFunction(()=>document.querySelector('button[aria-label="Word format painter"]')?.getAttribute('aria-pressed')==='false');assert.equal((await state()).version,captured.version);await page.keyboard.press('Meta+z');assert.equal((await state()).version,captured.version);
    await select(0,5);await arm();await apply();await notice.waitFor({state:'visible'});
    assert.equal((await state()).version,captured.version,'arming/no-op Apply made a document edit');
    assert.equal(await notice.getByRole('checkbox',{name:'Keep word format painter active',exact:true}).isChecked(),false);
    const destination=page.locator('.keynope-canvas-element[data-element-id="destination"]');await destination.hover();assert((await destination.getAttribute('class')).includes('style-painter-target'));
    const lockedPoint=await page.evaluate(()=>{const o=deck.pages[0].scene.objects.find(o=>o.id==='locked'),root=document.querySelector('#stage .keynope-modern-scene'),b=root.getBoundingClientRect(),scene=deck.pages[0].scene;return {x:b.x+(o.bounds.x+o.bounds.width/2)*b.width/scene.width,y:b.y+(o.bounds.y+o.bounds.height/2)*b.height/scene.height};});
    await page.mouse.move(lockedPoint.x,lockedPoint.y);assert.equal(await page.locator('.keynope-canvas-element.style-painter-target').count(),0);
    await notice.screenshot({path:output+'/word-painter-between-boxes.png'});
    await edit('destination');await notice.waitFor({state:'hidden'});assert.equal(await dialog.getByRole('button',{name:'Word format painter',exact:true}).getAttribute('aria-pressed'),'true');
    const selectedDestination=await state();await mouseWord(0,5);assert.equal(await dialog.getByRole('button',{name:'Word format painter',exact:true}).getAttribute('aria-pressed'),'false');
    assert.equal(await dialog.getByRole('combobox',{name:'Word font',exact:true}).inputValue(),'mono');assert.equal(await dialog.getByRole('spinbutton',{name:'Font size',exact:true}).inputValue(),'72');
    assert.equal((await state()).version,selectedDestination.version,'painting committed before Apply');
    await page.keyboard.press('Meta+z');await page.waitForFunction(()=>document.querySelector('input[aria-label="Word font size"]')?.value==='');
    await page.keyboard.press('Meta+y');await page.waitForFunction(()=>document.querySelector('input[aria-label="Word font size"]')?.value==='80');
    await apply();const firstCommit=await state();assert.equal(firstCommit.version,selectedDestination.version+1);compareOwner(original.slides[0].elements[1],firstCommit.slides[0].elements[1]);assert.equal((await model('destination')).runs.map(r=>r.text).join(''),'Bravo Gamma');
    await page.getByRole('button',{name:'Undo',exact:true}).click();await page.waitForFunction(()=>deck.pages[0].scene.objects.find(o=>o.id==='destination').text.runs[0].fontSize!==80);
    await page.getByRole('button',{name:'Redo',exact:true}).click();await page.waitForFunction(()=>deck.pages[0].scene.objects.find(o=>o.id==='destination').text.runs[0].fontSize===80);
    // The word clipboard survives closing its source; Paste changes only the
    // selected words, not the destination box's parent style or content.
    await edit('words');await select(0,5);await dialog.getByRole('button',{name:'Copy word style',exact:true}).click();await dialog.getByText('Word style copied.',{exact:true}).waitFor();await apply();
    await edit('destination');await select(6,11);await page.keyboard.press('Meta+Alt+v');await dialog.getByText('Word style applied.',{exact:true}).waitFor();const pasteScope=await state();await apply();assert.equal((await state()).version,pasteScope.version+1);const pastedModel=await model('destination');assert.equal(pastedModel.runs.map(r=>r.text).join(''),'Bravo Gamma');assert.equal(pastedModel.runs.find(r=>r.text.includes('Gamma')).fontSize,80);assert.equal(pastedModel.runs.find(r=>r.text===' ').fontSize,undefined,'unselected space lost inheritance');
    // Explicit sticky mode is carried to a separate shape-label editor. Two
    // applications are separate local undo steps and one document Apply.
    await edit('words');await select(0,5);await arm();await dialog.getByRole('checkbox',{name:'Keep word format painter active',exact:true}).check();await apply();await notice.waitFor({state:'visible'});assert.equal(await notice.getByRole('checkbox',{name:'Keep word format painter active',exact:true}).isChecked(),true);
    await edit('label-owner');const labelScope=await state();await mouseWord(0,5);await mouseWord(6,11);assert.equal(await dialog.getByRole('button',{name:'Word format painter',exact:true}).getAttribute('aria-pressed'),'true');assert.equal((await state()).version,labelScope.version);
    await dialog.screenshot({path:output+'/word-painter-sticky-label.png'});
    await page.keyboard.press('Meta+z');await select(6,11);assert.equal(await dialog.getByRole('spinbutton',{name:'Word font size',exact:true}).inputValue(),'');await select(0,5);assert.equal(await dialog.getByRole('spinbutton',{name:'Word font size',exact:true}).inputValue(),'80');
    await page.keyboard.press('Meta+z');await select(0,5);assert.equal(await dialog.getByRole('spinbutton',{name:'Word font size',exact:true}).inputValue(),'');await page.keyboard.press('Meta+y');await page.keyboard.press('Meta+y');
    await page.keyboard.press('Escape');assert.equal(await dialog.count(),1);assert.equal(await dialog.getByRole('button',{name:'Word format painter',exact:true}).getAttribute('aria-pressed'),'false');await apply();
    const labelCommit=await state();assert.equal(labelCommit.version,labelScope.version+1);compareOwner(original.slides[0].elements[2],labelCommit.slides[0].elements[2]);const paintedLabel=await model('label-owner');assert.equal(paintedLabel.runs.map(r=>r.text).join(''),'Delta Zebra');assert.equal(paintedLabel.runs.find(r=>r.text.includes('Delta')).fontSize,80);assert.equal(paintedLabel.runs.find(r=>r.text.includes('Zebra')).fontSize,80);
    const destinationModel=await model('destination');
    const saved=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Painted.md'})}).then(r=>r.json()).then(r=>r.content));
    exported=await page.evaluate(()=>fetch('/api/editor/export-document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Painted.html'})}).then(r=>r.json()).then(r=>r.content));
    const presentation=await context.newPage();presentation.on('pageerror',e=>errors.push(e.message));await presentation.goto('http://127.0.0.1:'+server.address().port+'/painted.html');await presentation.waitForFunction(()=>document.querySelector('.keynope-modern-presentation')?.style.visibility==='visible');
    assert.deepEqual(await presentation.evaluate(()=>deck.pages[0].scene.objects.find(o=>o.id==='label-owner').label),paintedLabel);assert.deepEqual(await presentation.evaluate(()=>deck.pages[0].scene.objects.find(o=>o.id==='destination').text),destinationModel);await presentation.screenshot({path:output+'/word-painter-export.png'});await presentation.close();
    await open(saved,'Painted.md');assert.deepEqual(await model('label-owner'),paintedLabel);assert.deepEqual(await model('destination'),destinationModel);
    // Canvas Escape, explicit Cancel, master/group scope and document
    // replacement all exit the mode, without another formatting edit.
    const armOutside=async()=>{await edit('words');await select(0,5);await arm();const s=await state();await apply();await notice.waitFor({state:'visible'});assert.equal((await state()).version,s.version);return s;};
    let outside=await armOutside();await page.keyboard.press('Escape');await notice.waitFor({state:'hidden'});assert.equal(await page.evaluate(()=>KeynopeTextStyles.painterState()),null);assert.equal((await state()).version,outside.version);
    outside=await armOutside();await notice.getByRole('button',{name:'Cancel painter',exact:true}).click();await notice.waitFor({state:'hidden'});assert.equal((await state()).version,outside.version);
    await armOutside();await page.evaluate(()=>editorAction({action:'toggle-master-mode'}));await notice.waitFor({state:'hidden'});assert.equal(await page.evaluate(()=>KeynopeTextStyles.painterState()),null);await page.evaluate(()=>editorAction({action:'toggle-master-mode'}));
    await armOutside();await open();await notice.waitFor({state:'hidden'});assert.equal(await page.evaluate(()=>KeynopeTextStyles.painterState()),null);
    await edit('words');await select(0,5);await arm();const cancelScope=await state();await dialog.getByRole('button',{name:'Cancel edit',exact:true}).click();await dialog.waitFor({state:'detached'});assert.equal(await page.evaluate(()=>KeynopeTextStyles.painterState()),null);assert.equal((await state()).version,cancelScope.version);
    // Hold a prepared paint response, then replace the deck with the same
    // path/IDs/revision. Epoch cancellation must beat this ABA, even though
    // a numeric-revision-only guard would accept the old response.
    await armOutside();await edit('destination');const heldScope=await state();
    await page.evaluate(()=>{window.__paintFetch=window.fetch;window.fetch=async(...args)=>{const response=await window.__paintFetch(...args);if(String(args[0]).endsWith('/api/editor/text-style-draft')&&JSON.parse(args[1]?.body||'{}').action==='paste-object-style'){window.__paintHeld=true;await new Promise(resolve=>window.__paintRelease=resolve);}return response;};});
    await mouseWord(0,5,false);await page.waitForFunction(()=>window.__paintHeld);
    const replacement=await page.evaluate(async({content,path,version})=>{
      const init=JSON.parse(keynopeWasmInit(content,path,false));if(init.status!==200)throw Error(init.body);let current=JSON.parse(init.body).version;
      while(current<version){const response=JSON.parse(keynopeWasmRequest('POST','/api/editor/action',JSON.stringify({action:'set-selection-property',sceneRevision:current,slide:0,objectIds:['destination'],property:'paint.opacity',propertyValue:current%2?.75:.5})));if(response.status!==200)throw Error(response.body);const next=JSON.parse(response.body).version;if(next<=current)throw Error('Fixture mutation did not advance');current=next;}
      await window.keynopeReloadWebDocument(JSON.parse(JSON.parse(keynopeWasmWorkspace()).body));return fetch('/api/editor/state').then(r=>r.json());
    },{content:fs.readFileSync(path.join(output,'painters.md'),'utf8'),path:heldScope.path,version:heldScope.version});
    assert.equal(replacement.version,heldScope.version);assert.equal(await page.evaluate(()=>KeynopeTextStyles.painterState()),null);assert.equal(await dialog.getByRole('button',{name:'Copy word style',exact:true}).isDisabled(),true);assert.equal(await page.evaluate(()=>window.keynopeStyleCommandState()['style.copy']),false);
    await page.evaluate(()=>window.__paintRelease());await page.waitForFunction(()=>document.querySelector('fieldset[aria-label="Word formatting"]')?.getAttribute('aria-busy')==='false');assert.deepEqual((await state()).slides,replacement.slides);
    await dialog.getByRole('textbox',{name:'Edit slide text',exact:true}).focus();await page.keyboard.type('Old draft');
    await dialog.getByRole('button',{name:'Apply text',exact:true}).click();await dialog.getByText('The document changed; reopen the text editor.',{exact:true}).first().waitFor();assert.deepEqual((await state()).slides,replacement.slides);
    await dialog.getByRole('button',{name:'Cancel edit',exact:true}).click();await page.getByRole('dialog',{name:'Keep text changes?',exact:true}).getByRole('button',{name:'Revert',exact:true}).click();await dialog.waitFor({state:'detached'});await page.evaluate(()=>window.fetch=window.__paintFetch);assert.deepEqual((await state()).slides,replacement.slides);
    assert.deepEqual(errors,[]);console.log('PASS: cross-box clipboard and one-shot/sticky word painter, same-range different owner, hover/lock feedback, body/shape-label local and document history, box/content isolation, Markdown/HTML parity, scope/replacement/Escape/Cancel and delayed ABA lifetimes; captures '+output);
  }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);process.exitCode=1;server.close();});
