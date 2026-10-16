const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright'),root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),proof='/tmp/keynope-build-proof';
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const base=name.startsWith('/proof/')?proof:root;const file=name==='/join/presentation.js'?path.resolve(root,'join/presentation.js'):path.resolve(base,'.'+(name.startsWith('/proof/')?name.slice(6):name));if((name!=='/join/presentation.js'&&!file.startsWith(base+'/'))||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.html':'text/html','.css':'text/css','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));const browser=await playwright.chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
 try{
  const context=await browser.newContext({viewport:{width:1440,height:900}});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());const page=await context.newPage(),errors=[];page.setDefaultTimeout(20000);page.on('pageerror',e=>errors.push(e.message));
  const origin='http://127.0.0.1:'+server.address().port;await page.goto(origin+'/proof/transitions.html');await page.waitForFunction(()=>!!document.querySelector('[data-object-id="outgoing-title"]'));
  const source=await page.evaluate(()=>JSON.stringify(deck));await page.getByRole('button',{name:'Presenter tools',exact:true}).click();
  await page.getByRole('button',{name:'Laser pointer',exact:true}).click();await page.getByRole('button',{name:'Close tools',exact:true}).click();
  const overlay=page.locator('.keynope-presentation-overlay'),box=await overlay.boundingBox();assert.ok(box);
  await page.mouse.move(box.x+box.width*.4,box.y+box.height*.45);await page.waitForTimeout(250);
  let snapshot=await page.evaluate(()=>keynopePresentationOverlaySnapshot());assert.equal(snapshot.mode,'laser');assert.ok(Math.abs(snapshot.pointer[0]-.4)<.01);assert.ok(Math.abs(snapshot.pointer[1]-.45)<.01);
  const circle=overlay.locator('circle').last();assert.equal(await circle.evaluate(n=>getComputedStyle(n).display),'inline');
  await page.screenshot({path:path.join(proof,'laser-pointer.png')});
  await page.getByRole('button',{name:'Presenter tools',exact:true}).click();await page.getByRole('button',{name:'Spotlight',exact:true}).click();await page.getByRole('button',{name:'Close tools',exact:true}).click();await page.mouse.move(box.x+box.width*.6,box.y+box.height*.5);
  assert.equal(await overlay.locator('path').first().evaluate(n=>getComputedStyle(n).display),'inline');await page.screenshot({path:path.join(proof,'spotlight.png')});
  await page.keyboard.press('Escape');assert.equal((await page.evaluate(()=>keynopePresentationOverlaySnapshot())).mode,'off');assert.equal(await overlay.evaluate(n=>getComputedStyle(n).pointerEvents),'none');
  await page.getByRole('button',{name:'Presenter tools',exact:true}).click();await page.getByRole('button',{name:'Temporary ink',exact:true}).click();await page.getByRole('combobox',{name:'Ink width',exact:true}).selectOption('12');await page.getByRole('button',{name:'Close tools',exact:true}).click();
  await page.mouse.move(box.x+box.width*.2,box.y+box.height*.7);await page.mouse.down();await page.mouse.move(box.x+box.width*.7,box.y+box.height*.7,{steps:25});await page.mouse.up();
  snapshot=await page.evaluate(()=>keynopePresentationOverlaySnapshot());assert.equal(snapshot.strokes.length,1);assert.ok(snapshot.strokes[0].points.length>10);assert.equal(snapshot.strokes[0].width,12);
  const retained=await overlay.locator('g path').elementHandle();await page.screenshot({path:path.join(proof,'temporary-ink.png')});
  assert.equal(await page.evaluate(()=>JSON.stringify(deck)),source,'Ink changed authored deck');
  await page.keyboard.press('Control+z');assert.equal((await page.evaluate(()=>keynopePresentationOverlaySnapshot())).strokes.length,0);
  await page.keyboard.press('Control+y');assert.equal((await page.evaluate(()=>keynopePresentationOverlaySnapshot())).strokes.length,1);
  // A slow writer must see the latest frame, with at most one in flight and
  // one coalesced pending snapshot. Unchanged polling must not touch its DOM.
  const coalesced=await page.evaluate(async()=>{
    const host=document.createElement('div');host.style.cssText='position:fixed;width:500px;height:280px';document.body.append(host);let release,requests=[];
    const tools=KeynopePresentationTools.create(host,{send:r=>{requests.push(r);return new Promise(resolve=>release=()=>resolve(r.overlay));}});
    tools.context({session:'bounded',slideId:'one',slide:0,page:0});tools.setVisible(true);tools.setMode('laser');await new Promise(r=>setTimeout(r,30));
    for(let i=0;i<100;i++)tools.setStyle({width:3+i%20});await new Promise(r=>setTimeout(r,30));if(requests.length!==1)throw Error('Unbounded concurrent tool uploads');release();await new Promise(r=>setTimeout(r,230));if(requests.length!==2||requests[1].overlay.width!==22)throw Error('Latest pending snapshot was lost');release();await new Promise(r=>setTimeout(r,10));
    const observer=new MutationObserver(()=>{});observer.observe(host,{subtree:true,attributes:true,childList:true});const current=tools.snapshot();for(let i=0;i<100;i++)tools.apply(current);if(observer.takeRecords().length)throw Error('Unchanged tool poll rewrote overlay DOM');observer.disconnect();tools.dispose();host.remove();return true;
  });assert.ok(coalesced);
  // Production participant renderer: same cached scene, pointer-only update,
  // ink retained across cursor steps, stale rejection, hidden resume and Stop.
  const audience=await page.evaluate(async()=>{
    const {createPresentationViewer}=await import('/join/presentation.js'),host=document.createElement('div'),status=document.createElement('p');host.id='tool-audience';host.style.cssText='position:fixed;left:20px;top:20px;width:800px;height:450px';document.body.append(host,status);
    const viewer=createPresentationViewer({frame:host,status,request:()=>{}}),data=JSON.parse(document.querySelector('#keynope-data').textContent),packed=await KeynopePresentationTransfer.pack(JSON.stringify(data));let sentAt=Date.now();
    const cursor={session:'audience-tools',slideId:data.pages[0].scene.slideId,step:0,generation:1,startedAtMs:Date.now(),motion:'seek',paused:false};
    const overlay={...keynopePresentationOverlaySnapshot(),session:cursor.session,slideId:cursor.slideId,revision:10};
    const announce=o=>viewer.receive({type:'presentation-md',format:'rendered-v1',presenting:true,transfer:packed.id,parts:packed.parts.length,slide:0,page:0,slideCount:data.pages.length,buildCursor:cursor,overlay:o,sentAt:++sentAt});
    viewer.setVisible(true);announce(overlay);packed.parts.forEach((data,index)=>viewer.receive({type:'presentation-part',transfer:packed.id,index,data}));
    const wait=ms=>new Promise(r=>setTimeout(r,ms));for(let i=0;i<100&&!host.shadowRoot?.querySelector('.keynope-presentation-overlay g path');i++)await wait(30);
    const object=host.shadowRoot?.querySelector('[data-object-id="outgoing-title"]'),path=host.shadowRoot?.querySelector('.keynope-presentation-overlay g path');if(!object||!path)throw Error('Audience tools failed: '+status.textContent);
    const pointed={...overlay,revision:11,mode:'laser',pointer:[.3,.4]};announce(pointed);await wait(80);if(host.shadowRoot.querySelector('[data-object-id="outgoing-title"]')!==object||host.shadowRoot.querySelector('.keynope-presentation-overlay g path')!==path)throw Error('Pointer remounted content or ink');
    const dot=host.shadowRoot.querySelector('.keynope-presentation-overlay circle:last-child');if(Math.abs(Number(dot.getAttribute('cx'))-576)>1)throw Error('Audience pointer position wrong');
    announce({...pointed,revision:9,pointer:[.8,.8]});await wait(50);if(Math.abs(Number(dot.getAttribute('cx'))-576)>1)throw Error('Stale pointer rewound audience');
    viewer.setVisible(false);announce({...pointed,revision:12,pointer:[.6,.6]});viewer.setVisible(true);await wait(80);if(Math.abs(Number(dot.getAttribute('cx'))-1152)>1)throw Error('Hidden audience lost latest pointer');
    window.__stopToolAudience=()=>{viewer.receive({type:'presentation-md',presenting:false,sentAt:++sentAt});if(!host.hidden)throw Error('Stop kept audience content visible');viewer.reset();host.remove();status.remove();};return true;
  });assert.ok(audience);await page.locator('#tool-audience').screenshot({path:path.join(proof,'participant-ink.png')});await page.evaluate(()=>__stopToolAudience());
  await page.keyboard.press('ArrowRight');await page.waitForFunction(()=>!!document.querySelector('[data-object-id="incoming-title"]'));assert.equal((await page.evaluate(()=>keynopePresentationOverlaySnapshot())).strokes.length,0,'Slide change retained old ink');assert.equal((await page.evaluate(()=>keynopePresentationOverlaySnapshot())).mode,'ink','Same-session slide change lost tool selection');assert.equal(await retained.evaluate(n=>n.isConnected),false);
  await page.evaluate(()=>keynopePrepareSnapshot(1));assert.equal(await overlay.evaluate(n=>getComputedStyle(n).display),'none','Static export included tools');assert.equal(await page.getByRole('button',{name:'Presenter tools',exact:true}).isVisible(),false);
  // Web editor -> real popup (including a slide with no authored builds).
  // Capture the actual publisher locally; no real workshop/network writes.
  const editor=await context.newPage();editor.on('pageerror',e=>errors.push(e.message));editor.on('dialog',d=>d.accept());await editor.goto(origin+'/editor/');await editor.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const chooser=editor.waitForEvent('filechooser');await editor.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles(path.join(proof,'transitions.md'));await editor.waitForFunction(()=>!!document.querySelector('#stage [data-object-id="outgoing-title"]'));
  const editorVersion=await editor.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()).then(s=>s.version)),popupEvent=editor.waitForEvent('popup');await editor.getByRole('button',{name:'Present on this computer',exact:true}).click();const popup=await popupEvent;popup.on('pageerror',e=>errors.push(e.message));await popup.waitForFunction(()=>!!keynopePresentationOverlaySnapshot());
  await editor.evaluate(async()=>{onboardingSessionDefinition=()=>({code:'owned001',id:'owned-onboarding'});window.__toolPublications=[];keynopeLobbyPresentation={code:'owned001',channel:{usesState:true,close(){},async send(payload){window.__toolPublications.push(payload);}},key:'',sentAt:0,sending:false,documents:new Map(),preloaded:new Set(),sentTransfers:new Set()};await publishLobbyPresentation(true);});
  await editor.waitForFunction(()=>window.__toolPublications.some(p=>p.type==='presentation-part'));await editor.evaluate(()=>{window.__toolCurrentTransfer=window.__toolPublications.find(p=>p.type==='presentation-md'&&p.presenting).transfer;window.__toolPublications=[];});
  await editor.getByRole('button',{name:'Presenter tools',exact:true}).waitFor({state:'visible'});await editor.getByRole('button',{name:'Presenter tools',exact:true}).click();await editor.getByRole('button',{name:'Laser pointer',exact:true}).click();await editor.getByRole('button',{name:'Close tools',exact:true}).click();
  const editorBox=await editor.locator('.keynope-presentation-overlay').boundingBox();await editor.mouse.move(editorBox.x+editorBox.width*.6,editorBox.y+editorBox.height*.4);
  await popup.waitForFunction(()=>keynopePresentationOverlaySnapshot()?.mode==='laser'&&Math.abs(keynopePresentationOverlaySnapshot().pointer?.[0]-.6)<.01);
  await editor.waitForFunction(()=>window.__toolPublications.some(p=>p.type==='presentation-md'&&p.overlay?.mode==='laser'&&p.overlay.pointer));
  assert.equal(await editor.evaluate(()=>window.__toolPublications.filter(p=>p.type==='presentation-part'&&p.transfer===window.__toolCurrentTransfer).length),0,'Pointer re-uploaded current slide content');
  assert.equal(await editor.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()).then(s=>s.version)),editorVersion,'Web editor tools dirtied the deck');
  await editor.screenshot({path:path.join(proof,'web-editor-presenter-tools.png')});await popup.close();await editor.getByRole('button',{name:'Presenter tools',exact:true}).waitFor({state:'hidden'});
  assert.deepEqual(errors,[]);fs.writeFileSync(path.join(proof,'presentation-tools-report.json'),JSON.stringify({passed:true,checks:['laser/spotlight controls and normalized placement','Escape restores navigation','captured ink/stroke width','no authored mutation','coalesced slow writer/no idle DOM mutations','real participant late join/retained scene and ink','stale/hidden updates and Stop','page-change clear','snapshot exclusion']},null,2)+'\n');console.log('PASS: shared presenter tools, bounded transport, participant overlays and static exclusion');
 }finally{await browser.close();await new Promise(resolve=>server.close(resolve));}
})().catch(error=>{console.error(error);process.exitCode=1;server.close();});
