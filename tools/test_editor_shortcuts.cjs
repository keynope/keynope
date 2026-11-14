// Build first: tools/build_web_editor.sh /tmp/keynope-shortcuts-site/editor
// Adapted from Bruno Schaatsbergen's keys/shortcut-help (ae6420a).
// Native editor keyboard chords against the actual WASM editor. Owned
// fixture, no external traffic, no system clipboard.
const {waitForAsync}=require('./wait_for_async.cjs');
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright'),engine=process.env.TEST_BROWSER||'chromium',root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-shortcuts-site');
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm','.json':'application/json'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
const deck=`<!-- keynope width=245 height=56 -->

<!-- top=4 left=10 width=85 height=8 render=truetype modern-font=c64 modern-size=120 -->
First text

<!-- top=16 left=10 width=85 height=8 render=truetype modern-font=mono modern-size=100 -->
Second text

<!-- top=28 left=10 width=30 height=10 fg=#55aaff shape=square object-locked=1 -->
[shape:square]

<!-- top=40 left=10 width=30 height=10 fg=#ffaa55 shape=circle object-hidden=1 -->
[shape:circle]
`;
(async()=>{
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const browser=await playwright[engine].launch({headless:true,...(engine==='webkit'?{}:{executablePath:process.env.CHROME_PATH})});
  try{
    const context=await browser.newContext({viewport:{width:1500,height:1000}});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());
    const page=await context.newPage(),errors=[];page.setDefaultTimeout(20000);page.on('pageerror',error=>errors.push(error.message));
    page.on('dialog',async dialog=>{assert.equal(dialog.type(),'confirm');await dialog.accept();});
    await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
    const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
    const chooser=page.waitForEvent('filechooser');
    await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();
    await (await chooser).setFiles({name:'shortcuts.md',mimeType:'text/markdown',buffer:Buffer.from(deck)});
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides?.[0]?.elements?.length===4;});
    await page.locator('.keynope-canvas-element').first().waitFor();
    const blur=()=>page.evaluate(()=>{document.activeElement?.blur();document.getElementById('stage')?.focus();});
    const selection=async()=>{const s=await state();return {selected:s.selected,selection:[...(s.selection||[])].sort((a,b)=>a-b),canUndo:!!s.canUndo};};
    const waitSelection=expected=>waitForAsync(page,async expected=>{const s=await fetch('/api/editor/state').then(r=>r.json());return JSON.stringify([...(s.selection||[])].sort((a,b)=>a-b))===JSON.stringify(expected)&&(expected.length?s.selected>=0:s.selected<0);},expected);

    // The shortcut table is the single description shared with menus/help.
    const bindings=await page.evaluate(()=>window.keynopeEditorShortcuts());
    const chord=name=>bindings.find(binding=>binding.name===name)?.chord;
    assert.equal(chord('select-all'),'Meta+A');assert.equal(chord('deselect-all'),'Meta+Shift+A');
    assert(bindings.every(binding=>binding.name&&binding.chord&&binding.label),'every binding is described for menus and help');
    const command=name=>page.evaluate(name=>window.keynopeEditorCommand(name),name);
    const commandState=()=>page.evaluate(()=>window.keynopeEditorCommandState());

    // Select All picks the visible, unlocked objects like the marquee does.
    await blur();const before=await selection();assert.equal(before.selected,-1);
    let flags=await commandState();assert.equal(flags['select-all'],true);assert.equal(flags['deselect-all'],false,'nothing to deselect yet');
    await page.keyboard.press('Meta+a');await waitSelection([0,1]);
    let current=await selection();assert.equal(current.selected,0);assert.equal(current.canUndo,before.canUndo,'selection must not create history');
    flags=await commandState();assert.equal(flags['deselect-all'],true);

    // Deselect All clears both the primary and the multi-selection.
    await page.keyboard.press('Meta+Shift+a');await waitSelection([]);
    current=await selection();assert.equal(current.selected,-1);

    // Control is the browser-editor stand-in for Command.
    await page.keyboard.press('Control+a');await waitSelection([0,1]);
    await page.keyboard.press('Escape');await waitSelection([]);

    // Text fields keep their own Select All; the editor must not react.
    await page.evaluate(()=>{const input=document.createElement('input');input.id='shortcut-caret';document.body.append(input);input.value='caret';input.focus();});
    await page.keyboard.press('Meta+a');
    assert.equal(await command('select-all'),false,'menu route declines while a text field has focus');assert.equal((await commandState())['select-all'],false);
    await page.waitForTimeout(300);current=await selection();assert.equal(current.selected,-1);assert.deepEqual(current.selection,[]);
    await page.evaluate(()=>document.getElementById('shortcut-caret').remove());

    // Unrelated modal dialogs own the keyboard; the menu route declines too.
    await page.evaluate(()=>{const dialog=document.createElement('dialog');dialog.id='unrelated-modal';dialog.innerHTML='<button>Unrelated modal</button>';document.body.append(dialog);dialog.showModal();dialog.querySelector('button').focus();});
    await page.keyboard.press('Meta+a');
    assert.equal(await command('select-all'),false);
    await page.waitForTimeout(300);current=await selection();assert.equal(current.selected,-1);
    await page.evaluate(()=>{const dialog=document.getElementById('unrelated-modal');dialog.close();dialog.remove();});

    // The native menu item uses the same guarded route as the chord.
    await blur();assert.equal(await command('select-all'),true);await waitSelection([0,1]);
    assert.equal(await command('deselect-all'),true);await waitSelection([]);
    assert.equal(await command('constructor'),false);assert.equal(await command('unknown-command'),false);

    // Slide commands share the toolbar actions. New slide lands after the
    // current one and becomes current; Duplicate clones the slide only when
    // nothing is selected.
    const slides=async()=>{const s=await state();return {current:s.current,ids:s.slides.map(slide=>slide.id),count:s.slides.length};};
    const waitSlides=(count,current)=>waitForAsync(page,async([count,current])=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides.length===count&&(current==null||s.current===current);},[count,current]);
    const waitSlideAt=(index,id)=>waitForAsync(page,async([index,id])=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[index]?.id===id&&s.current===index;},[index,id]);
    await blur();let deckState=await slides();assert.equal(deckState.count,1);const firstId=deckState.ids[0];
    flags=await commandState();assert.equal(flags['move-slide-up'],false,'a single slide cannot move');assert.equal(flags['delete-slide'],true);
    await page.keyboard.press('Meta+Shift+n');await waitSlides(2,1);
    deckState=await slides();assert.equal(deckState.ids[0],firstId);
    await page.keyboard.press('Meta+d');await waitSlides(3,2);
    await page.keyboard.press('Home');await waitSlides(3,0);
    await page.keyboard.press('Meta+a');await waitSelection([0,1]);
    await page.keyboard.press('Meta+d');await waitForAsync(page,async()=>(await fetch('/api/editor/state').then(r=>r.json())).slides[0].elements.length===5);
    deckState=await slides();assert.equal(deckState.count,3,'duplicating an object must not clone the slide');
    await page.keyboard.press('Escape');await waitSelection([]);
    // Moving follows the slide: down one, to the end, back to the beginning.
    await page.keyboard.press('Meta+ArrowDown');await waitSlideAt(1,firstId);
    await page.keyboard.press('Meta+Shift+ArrowDown');await waitSlideAt(2,firstId);
    await page.keyboard.press('Meta+Shift+ArrowUp');await waitSlideAt(0,firstId);
    await page.keyboard.press('Meta+ArrowUp');await page.waitForTimeout(300);deckState=await slides();assert.equal(deckState.current,0,'already first stays put');
    // Page navigation keys.
    await page.keyboard.press('End');await waitSlides(3,2);
    await page.keyboard.press('PageUp');await waitSlides(3,1);
    await page.keyboard.press('PageDown');await waitSlides(3,2);
    // Delete the current slide; history restores it.
    await page.keyboard.press('Meta+Backspace');await waitSlides(2,1);
    await page.getByRole('button',{name:'Undo',exact:true}).click();await waitSlides(3);
    await page.keyboard.press('End');await waitSlides(3,2);await page.keyboard.press('Meta+Delete');await waitSlides(2,1);
    await page.getByRole('button',{name:'Undo',exact:true}).click();await waitSlides(3);
    // Slide chords stay out of text fields.
    await page.evaluate(()=>{const input=document.createElement('input');input.id='shortcut-caret';document.body.append(input);input.focus();});
    await page.keyboard.press('Meta+Shift+n');await page.keyboard.press('Meta+Backspace');await page.keyboard.press('Home');
    await page.waitForTimeout(300);deckState=await slides();assert.equal(deckState.count,3);
    await page.evaluate(()=>document.getElementById('shortcut-caret').remove());

    // Arrange: layer moves take one stack unit and rewrite z-index ranks;
    // lock/unlock follow the selection and report their availability. The
    // earlier object duplicate shifted indices, so locate objects by content.
    await blur();await page.keyboard.press('Home');await waitSlides(3,0);
    const indexOf=async predicate=>(await state()).slides[0].elements.findIndex(predicate);
    const textIndex=await indexOf(e=>e.text==='First text'),secondIndex=await indexOf(e=>e.text==='Second text'),lockedIndex=await indexOf(e=>new URLSearchParams(e.query||'').get('object-locked')==='1');
    assert(textIndex>=0&&secondIndex>=0&&lockedIndex>=0,'fixture objects located');
    // The page mirrors editor state on a timer; wait for the published command
    // flags to reflect the new selection before reading them.
    const select=async(index,expected)=>{const s=await state();await page.evaluate(({i,e})=>editorAction({action:'select-element',element:i,elementData:e}),{i:index,e:s.slides[0].elements[index]});await page.waitForFunction(expected=>{const f=window.keynopeEditorCommandState();return Object.entries(expected).every(([k,v])=>f[k]===v);},expected);await blur();};
    const rank=async index=>new URLSearchParams((await state()).slides[0].elements[index].query).get('z-index');
    const waitRank=(index,value)=>waitForAsync(page,async([index,value])=>new URLSearchParams((await fetch('/api/editor/state').then(r=>r.json())).slides[0].elements[index].query).get('z-index')===value,[index,value]);
    const waitLocked=(index,locked)=>waitForAsync(page,async([index,locked])=>(new URLSearchParams((await fetch('/api/editor/state').then(r=>r.json())).slides[0].elements[index].query).get('object-locked')==='1')===locked,[index,locked]);
    await select(textIndex,{'bring-to-front':true,lock:true,unlock:false});
    await page.keyboard.press('Meta+Shift+f');await waitRank(textIndex,'4');
    await page.keyboard.press('Meta+Shift+b');await waitRank(textIndex,'0');
    // macOS Option transforms event.key; the physical key must still match.
    await page.evaluate(()=>document.dispatchEvent(new KeyboardEvent('keydown',{key:'ƒ',code:'KeyF',metaKey:true,altKey:true,shiftKey:true,bubbles:true,cancelable:true})));
    await waitRank(textIndex,'1');
    await page.keyboard.press('Meta+Alt+Shift+b');await waitRank(textIndex,'0');
    // A locked object cannot be reordered, but it can be unlocked.
    await select(lockedIndex,{'bring-to-front':false,lock:false,unlock:true});
    const lockedRank=await rank(lockedIndex);await page.keyboard.press('Meta+Shift+f');await page.waitForTimeout(300);assert.equal(await rank(lockedIndex),lockedRank);
    await page.keyboard.press('Meta+Alt+l');await waitLocked(lockedIndex,false);
    await page.keyboard.press('Meta+l');await waitLocked(lockedIndex,true);
    // Group and ungroup keep their Option-Command-G chords.
    await page.evaluate(async indices=>{const s=await fetch('/api/editor/state').then(r=>r.json());await editorAction({action:'select-elements',elementIndices:indices,elementsData:indices.map(i=>({id:s.slides[0].elements[i].id}))});},[textIndex,secondIndex]);
    await waitSelection([textIndex,secondIndex].sort((a,b)=>a-b));await blur();
    const groupOf=async index=>new URLSearchParams((await state()).slides[0].elements[index].query).get('group')||'';
    await page.keyboard.press('Meta+Alt+g');await waitForAsync(page,async([a,b])=>{const s=await fetch('/api/editor/state').then(r=>r.json());const g=i=>new URLSearchParams(s.slides[0].elements[i].query).get('group');return !!g(a)&&g(a)===g(b);},[textIndex,secondIndex]);
    await page.keyboard.press('Meta+Alt+Shift+g');await waitForAsync(page,async index=>!new URLSearchParams((await fetch('/api/editor/state').then(r=>r.json())).slides[0].elements[index].query).get('group'),textIndex);
    assert.equal(await groupOf(secondIndex),'');
    await page.keyboard.press('Escape');await waitSelection([]);

    // Text: emphasis, size and alignment chords resolve through the selection
    // properties, so a shape without text reports them unavailable.
    const query=async index=>new URLSearchParams((await state()).slides[0].elements[index].query);
    const waitQuery=(index,key,value)=>waitForAsync(page,async([index,key,value])=>(new URLSearchParams((await fetch('/api/editor/state').then(r=>r.json())).slides[0].elements[index].query).get(key)||'')===value,[index,key,value]);
    await select(textIndex,{bold:true,italic:true,underline:true,'bigger-text':true,'align-left':true});
    // Emphasis is read back through the selection property, because the
    // engine stores it per font family (ttf-weight for retro faces).
    const waitProperty=(property,value)=>waitForAsync(page,async([property,value])=>{const s=await fetch('/api/editor/state').then(r=>r.json());const p=s.selectionCapabilities?.properties?.[property];return s.selectionCapabilities?.revision===s.version&&p?.state==='value'&&p.value===value;},[property,value]);
    await page.keyboard.press('Meta+b');await waitProperty('text.bold',true);
    await page.keyboard.press('Meta+i');await waitProperty('text.italic',true);
    await page.keyboard.press('Meta+u');await waitProperty('text.underline',true);
    await page.keyboard.press('Meta+b');await waitProperty('text.bold',false);
    await page.keyboard.press('Meta+i');await waitProperty('text.italic',false);
    await page.keyboard.press('Meta+u');await waitProperty('text.underline',false);
    // Each edit is sent with the page's state revision, so wait for the ribbon
    // (which renders from the page's mirror) before the next chord.
    const settled=value=>page.waitForFunction(value=>document.querySelector('[data-selection-property="text.size"]')?.value===String(value),value);
    assert.equal(Number((await query(textIndex)).get('modern-size')),120);await settled(120);
    await page.keyboard.press('Meta+Shift+Period');await waitQuery(textIndex,'modern-size','121');await settled(121);
    await page.keyboard.press('Meta+Shift+Comma');await waitQuery(textIndex,'modern-size','120');await settled(120);
    await page.keyboard.press('Meta+Shift+BracketRight');await waitProperty('text.align','right');await page.waitForTimeout(250);
    await page.keyboard.press('Meta+Shift+Backslash');await waitProperty('text.align','center');await page.waitForTimeout(250);
    await page.keyboard.press('Meta+Shift+BracketLeft');await waitProperty('text.align','left');
    assert.equal((await query(textIndex)).get('left'),'10','text alignment must not move the object');
    await select(lockedIndex,{bold:false,'bigger-text':false});
    await page.keyboard.press('Escape');await waitSelection([]);

    // Presentation: Shift-Command-P toggles speaker notes; Option-Command-P
    // drives the Play button, which in the browser editor opens the live
    // presentation window.
    await blur();
    const notesShown=()=>page.evaluate(()=>document.documentElement.getAttribute('data-keynope-notes'));
    const notesBefore=await notesShown();
    await page.keyboard.press('Meta+Shift+p');await page.waitForFunction(before=>document.documentElement.getAttribute('data-keynope-notes')!==before,notesBefore);
    await page.keyboard.press('Meta+Shift+p');await page.waitForFunction(before=>document.documentElement.getAttribute('data-keynope-notes')===before,notesBefore);
    flags=await commandState();assert.equal(flags.play,true);assert.equal(flags['play-external'],false,'no external display in the browser editor');
    const popup=context.waitForEvent('page');await page.keyboard.press('Meta+Alt+p');
    const presentation=await popup;await presentation.close();
    await blur();

    // Command-/ opens the shortcut sheet; it lists every bound chord, owns the
    // keyboard while open, and Escape closes it.
    await page.keyboard.press('Meta+Slash');const sheet=page.locator('dialog[aria-label="Keyboard shortcuts"]');await sheet.waitFor();
    const listed=await sheet.locator('dd').allTextContents(),keys=await sheet.locator('kbd').allTextContents();
    const published=await page.evaluate(()=>window.keynopeEditorShortcuts());
    for(const binding of published)assert(listed.includes(binding.label),'sheet lists '+binding.name);
    assert.equal(keys.length,listed.length);assert(keys.every(Boolean),'every row shows its chord');
    assert(keys.length>published.flatMap(b=>b.chords).length,'static rows for older handlers are listed too');
    await page.keyboard.press('Meta+a');await page.waitForTimeout(300);current=await selection();assert.equal(current.selected,-1,'the sheet owns the keyboard while open');
    await page.keyboard.press('Escape');await sheet.waitFor({state:'detached'});

    // Preserve the dedicated slide shortcut even with an object selected.
    await select(textIndex,{'bold':true});
    const countBeforeDuplicate=(await slides()).count;
    await page.keyboard.press('Meta+Shift+d');await waitSlides(countBeforeDuplicate+1);
    await page.keyboard.press('Meta+Backspace');await waitSlides(countBeforeDuplicate);

    // Selecting part of a protected group would let the engine expand it to
    // include its locked member. Select All must skip that whole group.
    const protectedDeck=deck.replace('modern-size=120','modern-size=120 group=protected').replace('modern-size=100','modern-size=100 group=protected object-locked=1');
    const protectedChooser=page.waitForEvent('filechooser');
    await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();
    await (await protectedChooser).setFiles({name:'protected.md',mimeType:'text/markdown',buffer:Buffer.from(protectedDeck)});
    await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides.length===1&&s.slides[0].elements[0].query.includes('group=protected');});
    await blur();
    await page.waitForFunction(()=>window.keynopeEditorCommandState()['select-all']===false);
    assert.equal(await command('select-all'),false);
    await waitSelection([]);

    // Native menus receive focus changes without waiting for a menu to open.
    await page.evaluate(()=>{
      window.shortcutMenuMessages=[];
      const presenter=window.webkit.messageHandlers.keynopePresenter;
      const post=presenter.postMessage.bind(presenter);
      presenter.postMessage=message=>{if(message.action==='editor-command-state')window.shortcutMenuMessages.push(message.flags);post(message);};
    });
    await page.evaluate(()=>{const input=document.createElement('input');input.id='native-menu-focus';document.body.append(input);input.focus();});
    await page.waitForFunction(()=>window.shortcutMenuMessages.at(-1)?.['new-slide']===false);
    await page.evaluate(()=>document.getElementById('native-menu-focus').remove());await blur();
    await page.waitForFunction(()=>window.shortcutMenuMessages.at(-1)?.['new-slide']===true);

    // The shortcut reference remains usable in a narrow window.
    await page.setViewportSize({width:390,height:700});
    assert.equal(await command('shortcuts'),true);await sheet.waitFor();
    assert(await sheet.evaluate(node=>node.scrollWidth<=node.clientWidth+1),'shortcut sheet must not scroll horizontally');
    await page.keyboard.press('Escape');await sheet.waitFor({state:'detached'});

    assert.deepEqual(errors,[]);
    console.log('editor shortcuts ok ('+engine+')');
  }finally{await browser.close();server.close();}
})().catch(error=>{console.error(error);process.exit(1);});
