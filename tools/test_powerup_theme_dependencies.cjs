// Actual WASM commands/cache and mounted production renderer, owned fixture.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),output='/tmp/keynope-theme-dependency-proof';
const server=http.createServer((req,res)=>{
  let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';
  const file=path.resolve(root,'.'+name);
  if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}
  res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm','.json':'application/json'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));
});
(async()=>{
  fs.mkdirSync(output,{recursive:true});await new Promise(r=>server.listen(0,'127.0.0.1',r));
  const browser=await playwright.chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
  try{
    const context=await browser.newContext({viewport:{width:1440,height:1000}});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
    const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',async d=>{assert.equal(d.type(),'confirm');await d.accept();});
    await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
    const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles({name:'Consumers.md',mimeType:'text/markdown',buffer:fs.readFileSync(output+'/consumers.md')});
    await page.waitForFunction(()=>deck.pages[0]?.scene?.objects.length===40);
    const result=await page.evaluate(async()=>{
      const state=await fetch('/api/editor/state').then(r=>r.json());let theme=structuredClone(state.theme);
      const read=async(slide,cold=false)=>{const r=await fetch('/api/editor/scene?slide='+slide+(cold?'&timing=1':''));if(!r.ok)throw Error(await r.text());return {scene:await r.json(),cache:r.headers.get('X-Keynope-Scene-Cache')};};
      const host=document.createElement('div');host.id='theme-consumer-proof';Object.assign(host.style,{position:'fixed',inset:0,zIndex:99999,background:'#eee'});document.body.append(host);
      let current=(await read(0)).scene;const view=KeynopeScene.mount(host,current);await view.ready;
      const scale=Math.min(innerWidth/current.width,innerHeight/current.height);Object.assign(view.element.style,{transformOrigin:'top left',transform:`translate(${(innerWidth-current.width*scale)/2}px,${(innerHeight-current.height*scale)/2}px) scale(${scale})`});
      const nodes=new Map(current.objects.map(o=>[o.id,view.element.querySelector('[data-object-id="'+o.id+'"]')])),records=[];
      const originalWorkspace=window.keynopeWasmWorkspace,workspaces=[];window.keynopeWasmWorkspace=(...args)=>{const raw=originalWorkspace(...args),e=JSON.parse(raw);if(e.status===200)workspaces.push(JSON.parse(e.body));return raw;};
      const command=async(next)=>{const r=await fetch('/api/editor/action',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'set-deck-theme',themeData:next})});if(!r.ok)throw Error(await r.text());};
      async function change(name,edit,expectedCache,expectedObjects,expectedSlides){
        await read(0);const workspaceStart=workspaces.length,previous=view.renderStats(),next=structuredClone(theme);edit(next);await command(next);theme=next;
        const warm=await read(0),cold=await read(0,true);
        if(warm.cache!==expectedCache||JSON.stringify(warm.scene)!==JSON.stringify(cold.scene))throw Error(name+' cache/parity '+warm.cache);
        const pages=workspaces[workspaceStart];if(!pages?.baseRevision||JSON.stringify(pages.replaceSlides||[])!==JSON.stringify(expectedSlides))throw Error(name+' workspace replaced '+JSON.stringify(pages?.replaceSlides));
        const started=performance.now();view.update(warm.scene);await view.ready;await new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)));
        const after=view.renderStats();if(JSON.stringify([...after.lastChangedObjectIDs].sort())!==JSON.stringify([...expectedObjects].sort()))throw Error(name+' changed '+JSON.stringify(after.lastChangedObjectIDs));
        for(const [id,node]of nodes)if(node!==view.element.querySelector('[data-object-id="'+id+'"]'))throw Error(name+' remounted '+id);
        const delta=Object.fromEntries(Object.keys(previous).filter(k=>k!=='lastChangedObjectIDs').map(k=>[k,after[k]-previous[k]]));
        records.push({name,cache:warm.cache,replacedSlides:pages.replaceSlides||[],changed:after.lastChangedObjectIDs,delta,renderTwoFramesMs:performance.now()-started});current=warm.scene;
      }
      await change('unused accent',t=>t.colors.accent[5]='#ff00aa','hit',[],[1]);
      await change('used accent',t=>t.colors.accent[0]='#aa0055','miss',['text-0'],[0]);
      await change('unused mono typography',t=>t.fonts.mono.size+=10,'hit',[],[1]);
      await change('used body typography',t=>t.fonts.body.width=110,'miss',current.objects.map(o=>o.id),[0]);
      const unseen=await read(1);if(unseen.scene.objects[0].paint.color!=='#ff00aa'||unseen.scene.objects[0].text.size!==theme.fonts.mono.size)throw Error('Unseen consumers stale');
      if(JSON.stringify(current).includes('theme:'))throw Error('Raw theme token leaked into scene');
      window.keynopeWasmWorkspace=originalWorkspace;window.themeConsumerProof=view;return {records,objects:current.objects.length,unseen:{color:unseen.scene.objects[0].paint.color,size:unseen.scene.objects[0].text.size}};
    });
    await page.screenshot({path:output+'/theme-consumers.png'});fs.writeFileSync(output+'/browser-report.json',JSON.stringify(result,null,2)+'\n');
    assert.deepEqual(errors,[]);console.log('PASS: actual WASM unused-role/font cache hits, used-role/font invalidation, warm/cold parity, precise retained DOM updates and unseen slide consumers. '+output);console.log(JSON.stringify(result.records));
  }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);process.exitCode=1;server.close();});
