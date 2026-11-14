// Live MCP against an isolated native engine and real editor. No cloud access.
'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),os=require('node:os'),path=require('node:path'),cp=require('node:child_process');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright');
(async()=>{
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'keynope-live-mcp-')),file=path.join(root,'deck.md');
 const slide='<!-- top=8 left=12 width=90 height=8 render=truetype modern-font=sans modern-size=120 -->\nFirst object\n\n<!-- top=24 left=12 width=90 height=8 render=truetype modern-font=sans modern-size=120 -->\nSecond object\n';
 const markdown='<!-- keynope width=245 height=56 -->\n\n'+slide+Array.from({length:12},(_,i)=>'\n---\n\n# Slide '+(i+2)+'\n').join('');fs.writeFileSync(file,markdown);
 let engine,browser;
 try{
  const enginePath=process.env.KEYNOPE_ENGINE||path.join(root,'engine');
  if(!process.env.KEYNOPE_ENGINE){const build=cp.spawnSync('go',['build','-o',enginePath,'./cmd/keynope'],{cwd:path.resolve(__dirname,'..'),env:{...process.env,GOCACHE:process.env.GOCACHE||'/tmp/keynope-go-cache'},encoding:'utf8'});assert.equal(build.status,0,build.stderr);}
  engine=cp.spawn(enginePath,['--app',file],{stdio:['pipe','pipe','inherit']});
  const url=await new Promise((resolve,reject)=>{let out='';const timeout=setTimeout(()=>reject(Error('Engine startup timed out')),30000);engine.once('exit',(code,signal)=>{clearTimeout(timeout);reject(Error('Engine exited '+(signal||code)));});engine.stdout.on('data',chunk=>{out+=chunk;const match=out.match(/KEYNOPE_URL=(\S+)/);if(match){clearTimeout(timeout);resolve(match[1]);}});});
  const kind=process.env.TEST_BROWSER||'chromium';browser=await pw[kind].launch({headless:true});
  const page=await browser.newPage({viewport:{width:1500,height:1000}}),errors=[];page.setDefaultTimeout(15000);page.on('pageerror',e=>errors.push(e.message));
  await page.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());
  await page.goto(url+'&keynopeSurface=app');await page.waitForFunction(()=>typeof window.keynopeEditorCommand==='function');
  const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
  const rpc=async(name,args={})=>{
   const result=await page.evaluate(async({name,args})=>{const response=await fetch('/api/editor/live-mcp',{method:'POST',headers:{'Content-Type':'application/json','Accept':'application/json, text/event-stream','MCP-Protocol-Version':'2025-06-18'},body:JSON.stringify({jsonrpc:'2.0',id:1,method:'tools/call',params:{name,arguments:args}})});let text=await response.text();if(!response.ok)throw Error(text);const line=text.split('\n').find(line=>line.startsWith('data: '));if(line)text=line.slice(6);const envelope=JSON.parse(text);if(envelope.error)throw Error(JSON.stringify(envelope.error));return envelope.result;},{name,args});
   if(result.isError)throw Error(result.content.map(c=>c.text||'').join('\n'));return result.structuredContent;
  };
  let info=await rpc('deck_inspect');const owner=info.document_id;let version=info.version;
  const command=async action=>{const result=await rpc('editor_command',{document_id:owner,expected_version:version,command:action});version=result.version;return result;};
  const initial=await state(),first=initial.slides[0].elements.find(e=>e.text==='First object'),second=initial.slides[0].elements.find(e=>e.text==='Second object');assert(first&&second);
  await page.locator('.keynope-canvas-element').first().waitFor();
  await page.evaluate(()=>{document.activeElement?.blur();const nav=document.querySelector('#keynope-slide-navigator');if(nav)nav.scrollTop=40;});
  const position=await page.evaluate(()=>({x:scrollX,y:scrollY,nav:document.querySelector('#keynope-slide-navigator')?.scrollTop}));
  await command({action:'select-slide',slide:10});await command({action:'add-slide'});
  await page.waitForFunction(()=>fetch('/api/editor/state').then(r=>r.json()).then(s=>s.slides.length===14));
  await page.waitForTimeout(400);assert.equal((await state()).current,0,'MCP creating a slide must leave the human on the first slide');
  assert.deepEqual(await page.evaluate(()=>({x:scrollX,y:scrollY,nav:document.querySelector('#keynope-slide-navigator')?.scrollTop})),position,'MCP must not scroll the viewport or navigator');
  await command({action:'select-slide',slide:0});
  const firstIndex=(await state()).slides[0].elements.findIndex(e=>e.id===first.id);
  await page.locator('.keynope-canvas-element[data-element="'+firstIndex+'"]').dblclick();
  const editor=page.locator('[contenteditable="true"]').last();await editor.waitFor();await editor.fill('Human pending text');
  const beforeOther=version;
  await command({action:'set-scene-text',objectId:second.id,textRuns:[{text:'MCP updated the other object'}]});
  await page.waitForTimeout(400);assert.equal(await editor.textContent(),'Human pending text','unrelated MCP edit must preserve the open text draft');
  await page.keyboard.press('Meta+Enter');
  await page.waitForFunction(id=>fetch('/api/editor/state').then(r=>r.json()).then(s=>s.slides[0].elements.find(e=>e.id===id)?.text==='Human pending text'),first.id);
  await page.locator('.keynope-canvas-element[data-element="'+firstIndex+'"]').dblclick();
  await editor.waitFor();await editor.fill('Conflicting unsaved draft');
  await command({action:'set-scene-text',objectId:first.id,textRuns:[{text:'MCP wins the conflicting object'}]});
  await page.waitForFunction(()=>document.querySelectorAll('[contenteditable="true"]').length===0);
  await page.waitForFunction(()=>fetch('/api/editor/state').then(r=>r.json()).then(s=>s.selected===-1));
  assert.equal((await state()).slides[0].elements.find(e=>e.id===first.id).text,'MCP wins the conflicting object');
  const delta=await rpc('deck_changes',{document_id:owner,since_version:beforeOther});assert(delta.events.some(e=>e.origin==='mcp'));assert(delta.objects.some(o=>o.element.id===first.id));
  await page.evaluate(async id=>{const s=await fetch('/api/editor/state').then(r=>r.json());const e=s.slides[0].elements.find(e=>e.id===id);await editorAction({action:'update-element',element:s.slides[0].elements.indexOf(e),elementData:{...e,text:'Human committed change'}});},second.id);
  const merged=await command({action:'set-scene-text',objectId:first.id,textRuns:[{text:'MCP final'}]});
  assert(merged.delta.events.some(e=>e.origin==='human'),'MCP response includes concurrent human edits');
  assert.equal((await state()).slides[0].elements.find(e=>e.id===second.id).text,'Human committed change');
  const hit=page.locator('.keynope-canvas-element[data-element="'+firstIndex+'"]');
  await hit.click();const box=await hit.boundingBox();
  await page.mouse.move(box.x+box.width/2,box.y+box.height/2);await page.mouse.down();await page.mouse.move(box.x+box.width/2+25,box.y+box.height/2+15);
  await command({action:'set-scene-text',objectId:first.id,textRuns:[{text:'MCP interrupted drag'}]});
  await page.waitForTimeout(350);await page.mouse.up();
  await page.waitForTimeout(200);assert.equal((await state()).slides[0].elements.find(e=>e.id===first.id).text,'MCP interrupted drag');
  assert.equal((await state()).dirty,true);assert.equal(fs.readFileSync(file,'utf8'),markdown,'MCP edits remain in memory');
  // Save delegates to the app bridge. Cancel is propagated rather than writing
  // directly to disk or claiming success. Native Save itself is existing code.
  await page.evaluate(()=>{const handler=window.webkit?.messageHandlers?.keynopePresenter||{postMessage:()=>{}};const old=handler.postMessage.bind(handler);handler.postMessage=message=>{if(message.action==='mcp-save'){window.liveMCPSaveRequest=message;fetch('/api/editor/mcp-save-result',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({ID:message.id,Owner:message.owner,Error:'Cancelled in test'})});}else old(message);};window.webkit={...window.webkit,messageHandlers:{...window.webkit?.messageHandlers,keynopePresenter:handler}};});
  await assert.rejects(rpc('deck_save',{document_id:owner,expected_version:version}),/Cancelled in test/);
  assert(await page.evaluate(()=>!!window.liveMCPSaveRequest));assert.equal(fs.readFileSync(file,'utf8'),markdown);
  assert.deepEqual(errors,[]);console.log('PASS '+kind+': live MCP shares edits/undo, preserves view and unrelated drafts, cancels conflicting text, returns human deltas, and delegates Save.');
 }finally{await browser?.close();engine?.stdin.end();engine?.kill();fs.rmSync(root,{recursive:true,force:true});}
})().catch(error=>{console.error(error);process.exitCode=1;});
