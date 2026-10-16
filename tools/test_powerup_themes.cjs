const {waitForAsync}=require("./wait_for_async.cjs");
// Build first: tools/build_web_editor.sh /tmp/keynope-powerup-site/editor
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site');
const output=path.resolve(process.env.TEST_OUTPUT||'/tmp/keynope-themes-proof');
const server=http.createServer((req,res)=>{
  let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';
  const file=path.resolve(root,'.'+name);
  if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}
  res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm','.json':'application/json'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));
});
(async()=>{
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const browser=await playwright[process.env.TEST_BROWSER||'chromium'].launch({headless:true,...(process.env.TEST_BROWSER==='webkit'?{}:{executablePath:process.env.CHROME_PATH})});
  try{
    const context=await browser.newContext({viewport:{width:1440,height:1000}});
    await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());
    const page=await context.newPage(),errors=[];page.setDefaultTimeout(20000);page.on('pageerror',e=>errors.push(e.message));
    page.on('dialog',async dialog=>{assert.equal(dialog.type(),'confirm');await dialog.accept();});
    const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
    const bar=page.locator('.keynope-editor-topbar');
    await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');
    await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
    const before=await state();
    await bar.getByRole('button',{name:'New presentation',exact:true}).click();
    let chooser=page.getByRole('dialog',{name:'New presentation',exact:true});
    await chooser.getByRole('button',{name:'Use C64-inspired template',exact:true}).waitFor();
    assert.equal(await chooser.locator('.keynope-template-card').count(),7);
    fs.mkdirSync(output,{recursive:true});
    await chooser.screenshot({path:path.join(output,'template-gallery.png')});
    await chooser.getByRole('button',{name:'Cancel',exact:true}).click();
    const afterCancel=await state();
    assert.deepEqual(afterCancel.slides,before.slides);assert.equal(afterCancel.version,before.version);assert.equal(afterCancel.canUndo,before.canUndo);
    await bar.getByRole('button',{name:'New presentation',exact:true}).click();
    chooser=page.getByRole('dialog',{name:'New presentation',exact:true});
    await chooser.getByRole('button',{name:'Use Clean light template',exact:true}).click();
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.theme?.name==='Clean light'&&s.slides.length===2;});
    const template=await state();assert.equal(template.untitled,true);assert.equal(template.canUndo,false);assert.equal(template.theme.fonts.heading.fontId,'sans');
    await page.getByRole('button',{name:'Edit theme…',exact:true}).click();
    let dialog=page.getByRole('dialog',{name:'Edit deck theme',exact:true});
    await dialog.getByRole('textbox',{name:'Theme name',exact:true}).fill('Discarded');
    await dialog.getByRole('button',{name:'Theme background',exact:true}).click();
    let picker=dialog.getByRole('dialog',{name:'Pick color',exact:true});
    assert.equal(await picker.locator('.keynope-colour-swatch').count()>16,true);
    await picker.getByRole('textbox',{name:'HTML color',exact:true}).fill('#ff00ff');
    await picker.getByRole('button',{name:'Apply',exact:true}).click();
    assert.equal(await dialog.getByRole('button',{name:'Theme background',exact:true}).getAttribute('aria-description'),'#ff00ff');
    assert.deepEqual((await state()).theme,template.theme,'colour picker changes only the draft');
    await dialog.getByRole('button',{name:'Cancel',exact:true}).click();
    assert.deepEqual((await state()).theme,template.theme);
    await page.getByRole('button',{name:'Edit theme…',exact:true}).click();
    dialog=page.getByRole('dialog',{name:'Edit deck theme',exact:true});
    await dialog.getByRole('button',{name:'Clean dark',exact:true}).click();
    await dialog.getByRole('spinbutton',{name:'Heading theme size',exact:true}).fill('140');
    await dialog.screenshot({path:path.join(output,'theme-editor.png')});
    await dialog.getByRole('button',{name:'Apply theme',exact:true}).click();
    await dialog.waitFor({state:'hidden'});
    const changed=await state();assert.equal(changed.theme.name,'Clean dark');assert.equal(changed.theme.fonts.heading.size,140);assert.deepEqual(changed.slides,template.slides);assert.equal(changed.canUndo,true);
    await bar.getByRole('button',{name:'Undo',exact:true}).click();
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.theme.name==='Clean light';});
    await bar.getByRole('button',{name:'Redo',exact:true}).click();
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.theme.name==='Clean dark';});
    // View revisions do not invalidate a theme draft.
    await page.getByRole('button',{name:'Edit theme…',exact:true}).click();
    dialog=page.getByRole('dialog',{name:'Edit deck theme',exact:true});
    await dialog.getByRole('textbox',{name:'Theme name',exact:true}).fill('Draft after navigation');
    await page.evaluate(()=>editorAction({action:'select-slide',value:1}));
    await dialog.getByRole('button',{name:'Apply theme',exact:true}).click();await dialog.waitFor({state:'hidden'});
    assert.equal((await state()).theme.name,'Draft after navigation');
    await page.evaluate(()=>editorAction({action:'undo'}));assert.equal((await state()).theme.name,'Clean dark');
    // A second theme edit cannot be overwritten by the already-open draft.
    await page.getByRole('button',{name:'Edit theme…',exact:true}).click();
    dialog=page.getByRole('dialog',{name:'Edit deck theme',exact:true});
    await dialog.getByRole('textbox',{name:'Theme name',exact:true}).fill('Retain this draft');
    await page.evaluate(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json()),theme=structuredClone(s.theme);theme.name='Newer theme';await editorAction({action:'set-deck-theme',themeData:theme});});
    const newer=await state();await dialog.getByRole('button',{name:'Apply theme',exact:true}).click();
    await dialog.getByRole('alert').filter({hasText:'the theme changed'}).waitFor();
    assert.equal(await dialog.getByRole('textbox',{name:'Theme name',exact:true}).inputValue(),'Retain this draft');
    assert.equal(await dialog.getByRole('button',{name:'Apply theme',exact:true}).isEnabled(),true);
    assert.deepEqual((await state()).theme,newer.theme);assert.equal((await state()).version,newer.version);
    await dialog.getByRole('button',{name:'Detach from theme',exact:true}).click();
    await dialog.getByRole('alert').filter({hasText:'the theme changed'}).waitFor();
    assert.equal((await state()).version,newer.version);assert.equal((await state()).hasDeckTheme,true);
    assert.equal(await dialog.getByRole('button',{name:'Detach from theme',exact:true}).isEnabled(),true);
    await dialog.getByRole('button',{name:'Cancel',exact:true}).click();await page.evaluate(()=>editorAction({action:'undo'}));
    await page.screenshot({path:path.join(output,'themed-editor.png')});
    await page.setViewportSize({width:520,height:800});
    await page.getByRole('button',{name:'Edit theme…',exact:true}).click();
    dialog=page.getByRole('dialog',{name:'Edit deck theme',exact:true});
    assert(await dialog.evaluate(e=>e.scrollWidth<=e.clientWidth),'theme dialog fits narrow viewport');
    await dialog.screenshot({path:path.join(output,'theme-narrow.png')});
    await page.keyboard.press('Escape');
    await page.setViewportSize({width:1440,height:1000});
    await bar.getByRole('button',{name:'New presentation',exact:true}).click();
    await page.getByRole('button',{name:'Use C64-inspired template',exact:true}).click();
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.theme?.name==='C64-inspired';});
    await page.screenshot({path:path.join(output,'c64-template.png')});
    // Same path/data/revision after a document replacement still has a new owner.
    await page.getByRole('button',{name:'Edit theme…',exact:true}).click();dialog=page.getByRole('dialog',{name:'Edit deck theme',exact:true});
    await dialog.getByRole('textbox',{name:'Theme name',exact:true}).fill('Do not cross documents');
    const owner=(await state()).notesSessionId;
    await page.evaluate(async()=>{const source=await fetch('/api/editor/templates?id=c64').then(r=>r.json()),result=JSON.parse(keynopeWasmInit(source.content,'Untitled.md',true));if(result.status!==200)throw Error(result.body);});
    const replacement=await state();assert.notEqual(replacement.notesSessionId,owner);
    await dialog.getByRole('button',{name:'Apply theme',exact:true}).click();await dialog.getByRole('alert').filter({hasText:'the document changed'}).waitFor();
    const rejected=await state();assert.deepEqual(rejected.theme,replacement.theme);assert.deepEqual(rejected.slides,replacement.slides);assert.equal(rejected.version,replacement.version);assert.equal(rejected.canUndo,false);
    await dialog.getByRole('button',{name:'Cancel',exact:true}).click();
    // The preceding direct runtime replacement intentionally bypassed the
    // host, to exercise stale-owner rejection. Now perform the host's normal
    // document reload before issuing commands for the replacement document.
    await page.evaluate(async()=>{const envelope=JSON.parse(keynopeWasmWorkspace());if(envelope.status!==200)throw Error(envelope.body);await keynopeReloadWebDocument(JSON.parse(envelope.body));});
    // Detach is a real undoable command. Capture the actual shared renderer
    // before and after, rather than comparing only authored query metadata.
    await page.evaluate(()=>editorAction({action:'select-slide',value:0}));
    const capture=async label=>{
      const images=[];
      for(let i=0;i<(await state()).slides.length;i++){
        const scene=await page.evaluate(i=>fetch('/api/editor/scene?slide='+i).then(r=>r.json()),i);
        await page.evaluate(async scene=>{
          const host=document.createElement('div');host.id='theme-proof';host.style.cssText='position:fixed;inset:0;background:#111;z-index:100000';document.body.append(host);
          const view=KeynopeScene.mount(host,scene);window.themeProof=view;
          const scale=Math.min(innerWidth/scene.width,innerHeight/scene.height);
          Object.assign(view.element.style,{transformOrigin:'0 0',transform:'scale('+scale+')',left:(innerWidth-scene.width*scale)/2+'px',top:(innerHeight-scene.height*scale)/2+'px'});
          await view.ready;await view.snapshotAt(0);
        },scene);
        images.push(await page.locator('#theme-proof').screenshot({path:path.join(output,label+'-'+i+'.png'),animations:'disabled'}));
        await page.evaluate(()=>{themeProof.dispose();document.querySelector('#theme-proof').remove();delete window.themeProof;});
      }
      return images;
    };
    const linked=await state();assert.equal(linked.hasDeckTheme,true);
    const linkedPixels=await capture('before-detach');
    await page.getByRole('button',{name:'Edit theme…',exact:true}).click();dialog=page.getByRole('dialog',{name:'Edit deck theme',exact:true});
    await dialog.getByRole('textbox',{name:'Theme name',exact:true}).fill('Unapplied draft must not be used');
    await dialog.screenshot({path:path.join(output,'detach-theme-dialog.png')});
    await dialog.getByRole('button',{name:'Detach from theme',exact:true}).scrollIntoViewIfNeeded();
    await dialog.screenshot({path:path.join(output,'detach-theme-actions.png')});
    await dialog.getByRole('button',{name:'Detach from theme',exact:true}).click();
    await waitForAsync(page,()=>{const d=document.querySelector('.keynope-theme-dialog');return !d?.open||!!d.querySelector('[role="alert"]')?.textContent;});
    if(await dialog.isVisible())throw Error(await dialog.getByRole('alert').textContent());
    await dialog.waitFor({state:'hidden'});
    const detached=await state();assert.equal(detached.hasDeckTheme,false);assert.equal(detached.canUndo,true);
    assert.deepEqual(await capture('after-detach'),linkedPixels,'Detach changed rendered pixels');
    await page.evaluate(()=>editorAction({action:'undo'}));assert.equal((await state()).hasDeckTheme,true);assert.deepEqual((await state()).slides,linked.slides);
    await page.evaluate(()=>editorAction({action:'redo'}));assert.equal((await state()).hasDeckTheme,false);assert.deepEqual((await state()).slides,detached.slides);
    await page.getByRole('button',{name:'Edit theme…',exact:true}).click();dialog=page.getByRole('dialog',{name:'Edit deck theme',exact:true});
    assert.equal(await dialog.getByRole('button',{name:'Detach from theme',exact:true}).count(),0);
    await dialog.getByRole('button',{name:'Cancel',exact:true}).click();
    await page.evaluate(async()=>{
      const response=await fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'detached.md'})});
      if(!response.ok)throw Error(await response.text());
      const document=await response.json(),result=JSON.parse(keynopeWasmInit(document.content,'detached.md',false));
      if(result.status!==200)throw Error(result.body);
      const workspace=JSON.parse(keynopeWasmWorkspace());if(workspace.status!==200)throw Error(workspace.body);
      await keynopeReloadWebDocument(JSON.parse(workspace.body));
      await editorAction({action:'select-slide',value:0});
    });
    assert.equal((await state()).hasDeckTheme,false);
    assert.deepEqual(await capture('reopened-detach'),linkedPixels,'Save/reopen changed detached rendering');
    assert.deepEqual(errors,[]);
    console.log('PASS: templates, theme draft/apply/history, stale-theme/document rejection, narrow layout, Detach UI/history and exact rendered pixels');
  }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);server.close();process.exitCode=1;});
