// Actual master sidebar, production WASM commands and asynchronous preview guards.
'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright'),engine=process.env.TEST_BROWSER||'chromium';
const {waitForAsync}=require('./wait_for_async.cjs');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),output=path.resolve(process.env.TEST_OUTPUT||'/tmp/keynope-master-thumbnail-proof');
const fixture=process.env.TEST_DECK||path.join(output,'masters.md');
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 fs.mkdirSync(output,{recursive:true});assert(fs.statSync(fixture).size>0);await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 const browser=await pw[engine].launch({headless:true,...(engine==='webkit'?{}:{executablePath:process.env.CHROME_PATH})});
 try{
  const context=await browser.newContext({viewport:{width:1440,height:1000},reducedMotion:'reduce'});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());
  const page=await context.newPage(),errors=[];page.on('pageerror',error=>errors.push(error.message));page.on('dialog',dialog=>dialog.accept());
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles(fixture);await waitForAsync(page,async()=>{const state=await fetch('/api/editor/state').then(r=>r.json());return state.masters?.layouts?.[0]?.id==='alpha';});
  const report=await page.evaluate(async()=>{
   const check=(ok,message)=>{if(!ok)throw Error(message);},frame=()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)));
   const originalFetch=window.fetch,requests=[];let pending=0;
   window.fetch=async function(...args){const url=new URL(typeof args[0]==='string'?args[0]:args[0].url,location.href);if(url.pathname!=='/api/editor/workspace'||!url.searchParams.has('master'))return originalFetch.apply(this,args);const record={index:Number(url.searchParams.get('master')),stamp:url.searchParams.get('previewRevision'),status:null};requests.push(record);pending++;try{const response=await originalFetch.apply(this,args);record.status=response.status;return response;}finally{pending--;}};
   const state=()=>originalFetch('/api/editor/state').then(r=>r.json());
   async function settle(){const end=performance.now()+10000;await frame();while(pending||document.querySelector('.keynope-slide-thumbnail[aria-busy="true"]')){if(performance.now()>end)throw Error('Master previews did not settle: '+JSON.stringify(requests));await new Promise(r=>setTimeout(r,20));}await frame();check(![...document.querySelectorAll('.keynope-slide-thumbnail')].some(node=>node.textContent.includes('Preview unavailable')),'Preview failed');}
   await editorAction({action:'toggle-master-mode'});await settle();
   const thumbnails=()=>new Map([...document.querySelectorAll('.keynope-slide-item[data-master-index]')].map(button=>[Number(button.dataset.masterIndex),button.querySelector('.keynope-modern-scene')]));
   let owners=thumbnails();check(owners.size===4&&[...owners.values()].every(Boolean),'Four visible masters did not render');check(requests.length===4&&requests.every(r=>r.status===200&&r.stamp!==null),'Initial thumbnails lack accepted dependency stamps');
   const objectOwners=new Map([...owners].map(([index,node])=>[index,new Map([...node.querySelectorAll('[data-object-id]')].map(object=>[object.dataset.objectId,object]))]));
   const records=[];
   async function command(name,action,want){
    const start=requests.length;await editorAction(action);await settle();const got=requests.slice(start).map(r=>r.index).sort((a,b)=>a-b);
    check(JSON.stringify(got)===JSON.stringify(want),name+' fetched '+JSON.stringify(got)+' want '+JSON.stringify(want));check(requests.slice(start).every(r=>r.status===200),name+' had a stale request');
    const current=thumbnails();for(const [index,node]of owners){check(current.get(index)===node,name+' remounted master '+index);for(const [id,owner]of objectOwners.get(index))check(node.querySelector('[data-object-id="'+CSS.escape(id)+'"]')===owner,name+' remounted object '+id);}
    records.push({name,requests:got,retainedMasters:owners.size});
   }
   await command('selection', {action:'select-slide',slide:1},[]);
   let s=await state(),element=structuredClone(s.masters.layouts[0].slide.elements.find(e=>e.id==='alpha-text'));element.text='Alpha edited in place';
   await command('one layout text',{action:'update-element',element:0,elementData:element},[1]);check(owners.get(1).textContent.includes('Alpha edited in place'),'Changed thumbnail text stayed stale');
   await command('undo',{action:'undo'},[1]);check(owners.get(1).textContent.includes('Alpha content'),'Undo thumbnail stayed stale');
   await command('redo',{action:'redo'},[1]);
   s=await state();let theme=structuredClone(s.theme);theme.colors.accent[5]='#123456';await command('unused theme accent',{action:'set-deck-theme',themeData:theme},[]);
   s=await state();theme=structuredClone(s.theme);theme.fonts.body.width=83;await command('inherited font width',{action:'set-deck-theme',themeData:theme},[0,1,2,3]);
   await command('select Base',{action:'select-slide',slide:0},[]);s=await state();element=structuredClone(s.masters.base.slide.elements[0]);element.text='New inherited Base';
   await command('Base text',{action:'update-element',element:0,elementData:element},[0,1,2,3]);check([...owners.values()].every(node=>node.textContent.includes('New inherited Base')),'Base consumers stayed stale');
   // Confirm an old visual token is still valid after another selection-only
   // revision, but cannot cross document ownership. Use the actual endpoint.
   s=await state();const stamp=s.masterPreviewRevisions[1];await editorAction({action:'select-slide',slide:2});await settle();
   let r=await originalFetch('/api/editor/workspace?master=1&previewRevision='+stamp+'&owner='+encodeURIComponent(s.notesSessionId)+'&cols=245&rows=56');check(r.status===200,'View-only revision rejected a current preview');
   r=await originalFetch('/api/editor/workspace?master=1&previewRevision='+stamp+'&owner=other-document&cols=245&rows=56');check(r.status===409,'Wrong document owner accepted');
   window.fetch=originalFetch;
   // Exercise the same production asynchronous thumbnail controller with
   // deliberately delayed updates, not a mock replacement implementation.
   const panel=document.createElement('div');panel.style.cssText='position:fixed;left:300px;top:100px;width:200px;height:200px;overflow:auto;z-index:99999';const button=document.createElement('button');button.style.cssText='width:180px;height:150px';panel.append(button);document.body.append(panel);
   let generation=0,mounts=0,disposals=0;const painted=[];
   const paint=async host=>{mounts++;const mark=document.createElement('span');mark.textContent='fresh-'+generation;host.append(mark);const cleanup=()=>{disposals++;mark.remove();};cleanup.update=value=>{painted.push(value);mark.textContent=value;return true;};return cleanup;};
   const controller=KeynopeWorkspace.thumbnails(panel,paint,()=>generation);controller.add(button,0);
   const wait=async predicate=>{const end=performance.now()+5000;while(!predicate()){if(performance.now()>end)throw Error('Thumbnail guard check timed out');await frame();}};
   await wait(()=>mounts===1&&!panel.querySelector('[aria-busy="true"]'));let late;
   generation++;controller.update(paint,()=>generation,update=>new Promise(resolve=>{late=()=>resolve(update('STALE'));}));
   generation++;controller.update(paint,()=>generation);await wait(()=>mounts===2&&!panel.querySelector('[aria-busy="true"]'));late();await frame();check(!painted.includes('STALE')&&!panel.textContent.includes('STALE'),'Superseded callback painted stale content');
   generation++;controller.update(paint,()=>generation,()=>Promise.reject(Error('Owned rejected preview')));await wait(()=>mounts===3&&!panel.querySelector('[aria-busy="true"]'));check(panel.textContent.includes('fresh-3'),'Rejected update did not recover through fresh paint');
   let afterClose;generation++;controller.update(paint,()=>generation,update=>new Promise(resolve=>{afterClose=()=>resolve(update('DISPOSED'));}));controller.dispose();afterClose();await frame();check(!painted.includes('DISPOSED')&&disposals===mounts,'Disposed callback retained or repainted resources');panel.remove();
   return {passed:true,initialRequests:4,records,staleAndDisposedGuards:true,syntheticMounts:mounts,syntheticDisposals:disposals};
  });
  assert.deepEqual(errors,[]);await page.screenshot({path:path.join(output,'master-thumbnails-'+engine+'.png')});fs.writeFileSync(path.join(output,'report.json'),JSON.stringify({engine,browser:browser.version(),...report},null,2));console.log('PASS '+engine+': '+JSON.stringify(report));
 }finally{await browser.close();server.close();}
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
