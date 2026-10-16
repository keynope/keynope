// Position-only preview admission and ownership. Shared production renderer;
// no native application activation and no reference-equality content shortcut.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site');
const engine=process.env.TEST_BROWSER||'chromium';
const server=http.createServer((req,res)=>{
 const name=new URL(req.url,'http://localhost').pathname;
 const file=path.resolve(root,'.'+(name.endsWith('/')?name+'index.html':name));
 if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}
 res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));
});
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 const browser=await playwright[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
 try{
  const context=await browser.newContext({viewport:{width:1440,height:1000},reducedMotion:'reduce'});
  await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());
  const page=await context.newPage(),errors=[];page.on('pageerror',error=>errors.push(error.message));
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');
  await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const result=await page.evaluate(async()=>{
   const host=document.createElement('div');document.body.append(host);
   const text={fontId:'c64',family:'KeynopeC64, monospace',size:28,widthScale:.4167,lineHeight:1.2,align:'start',vertical:'top',paragraphBefore:0,paragraphAfter:0,role:'text',runs:[{text:'Before'}]};
   const scene={version:1,width:1920,height:1080,background:'#111827',objects:[
    {id:'a',kind:'text',bounds:{x:40,y:40,width:215,height:60},paint:{color:'#eef2f6'},text},
    {id:'b',kind:'shape',shape:'square',rotation:17,bounds:{x:320,y:100,width:180,height:100},paint:{color:'#55aaff'}},
    {id:'line',kind:'connector',bounds:{x:500,y:150,width:120,height:1},points:[{x:500,y:150},{x:620,y:150}],paint:{color:'#ffffff',strokeWidth:2},arrows:'both',arrowWidth:16}
   ]};
   const before=JSON.stringify(scene),view=KeynopeScene.mount(host,scene);await view.ready;
   const node=id=>view.element.querySelector('[data-object-id="'+id+'"]');
   const a=node('a'),b=node('b'),stats=view.renderStats(),resources=view.resourceCounts();
   const originalStringify=JSON.stringify;let serialized=0;
   JSON.stringify=(...args)=>{serialized++;return originalStringify(...args);};
   let admitted;
   try{admitted=view.previewPositions([{id:'a',x:84.25,y:160.5},{id:'b',x:350.75,y:90.125}]);}
   finally{JSON.stringify=originalStringify;}
   if(!admitted||serialized)throw Error('Position preview serialized scene content');
   if(a!==node('a')||b!==node('b')||a.style.left!=='84.25px'||b.style.top!=='90.125px')throw Error('Position preview replaced owners or rounded geometry');
   if(b.style.transform!=='rotate(17deg)')throw Error('Position preview changed rotation');
   if(JSON.stringify(scene)!==before)throw Error('Position preview mutated caller-owned scene');
   if(JSON.stringify(view.resourceCounts())!==JSON.stringify(resources))throw Error('Position preview rebuilt resources');
   const changed=view.renderStats();
   for(const key of Object.keys(stats))if(!['updates','lastChangedObjectIDs'].includes(key)&&changed[key]!==stats[key])throw Error('Position preview repainted '+key);
   if(JSON.stringify([...changed.lastChangedObjectIDs].sort())!==JSON.stringify(['a','b']))throw Error('Position preview touched unrelated objects');
   const order=[...view.element.children].filter(n=>n.hasAttribute('data-object-id')).map(n=>n.dataset.objectId);
   if(order.indexOf('b')>order.indexOf('a'))throw Error('Position preview failed to update spatial reading order');
   // Reject the complete command before applying even its first valid move.
   for(const positions of [null,[{id:'a',x:1,y:2},{id:'missing',x:3,y:4}],[{id:'a',x:1,y:2},{id:'a',x:3,y:4}],[{id:'a',x:Infinity,y:2}],[{id:'a',x:1,y:NaN}],[{id:'line',x:1,y:2}]]){
    const html=view.element.innerHTML,previous=JSON.stringify(view.renderStats());
    if(view.previewPositions(positions)!==false||view.element.innerHTML!==html||JSON.stringify(view.renderStats())!==previous)throw Error('Invalid position preview changed the scene');
   }
   const line=node('line'),marker=line.getAttribute('marker-start'),defs=line.parentElement.querySelector('defs'),routePoints=[{x:600,y:100},{x:600,y:200},{x:900,y:200}],routeStats=view.renderStats();
   serialized=0;JSON.stringify=(...args)=>{serialized++;return originalStringify(...args);};
   let routed;
   try{routed=view.previewPositions([{id:'a',x:90.5,y:170.25}],[{id:'line',points:routePoints}]);}
   finally{JSON.stringify=originalStringify;}
   if(!routed||serialized)throw Error('Route preview rejected or serialized payloads');
   if(node('line')!==line||line.parentElement.querySelector('defs')!==defs||line.getAttribute('marker-start')!==marker)throw Error('Route preview replaced marker/path resources');
   if(line.getAttribute('points')!=='600,100 600,200 900,200'||line.getAttribute('stroke-dasharray')!=='0 16 368 16')throw Error('Route points or double-arrow shaft trim did not update');
   if(JSON.stringify(scene)!==before)throw Error('Route preview mutated caller scene');
   routePoints[0].x=-100;
   if(line.getAttribute('points')!=='600,100 600,200 900,200')throw Error('Route preview retains mutable caller points');
   const routedStats=view.renderStats();
   for(const key of Object.keys(routeStats))if(!['updates','lastChangedObjectIDs'].includes(key)&&routeStats[key]!==routedStats[key])throw Error('Route preview repainted '+key);
   if(JSON.stringify([...routedStats.lastChangedObjectIDs].sort())!==JSON.stringify(['a','line']))throw Error('Route preview omitted changed identities');
   for(const routes of [null,[{id:'missing',points:[{x:1,y:1},{x:2,y:2}]}],[{id:'b',points:[{x:1,y:1},{x:2,y:2}]}],[{id:'line',points:[]}],[{id:'line',points:[{x:1,y:1}]}],[{id:'line',points:[{x:1,y:1},null]}],[{id:'line',points:[{x:NaN,y:1},{x:2,y:2}]}],[{id:'line',points:[{x:1,y:1},{x:Infinity,y:2}]}],[{id:'line',points:Array.from({length:4097},()=>({x:1,y:2}))}],[{id:'line',points:routePoints},{id:'line',points:routePoints}]]){
    const html=view.element.innerHTML,previous=JSON.stringify(view.renderStats());
    if(view.previewPositions([{id:'a',x:1,y:2}],routes)!==false||view.element.innerHTML!==html||JSON.stringify(view.renderStats())!==previous)throw Error('Invalid combined route preview was not atomic');
   }
   if(!view.update(scene))throw Error('Restore after preview failed');await view.ready;
   if(a!==node('a')||a.style.left!=='40px'||b.style.top!=='100px')throw Error('Restore did not recover original positions');
   if(node('line')!==line||line.getAttribute('points')!=='500,150 620,150'||line.getAttribute('stroke-dasharray')!=='0 16 88 16')throw Error('Restore did not recover route and shaft geometry');
   // Input reference is unchanged: normal update must still detect these edits.
   scene.objects[0].text.runs[0].text='After';scene.objects[0].paint.color='#ff0055';
   if(!view.update(scene))throw Error('In-place content update failed');await view.ready;
   if(view.renderStats().textContentUpdates!==stats.textContentUpdates+1||view.renderStats().textPaintUpdates!==stats.textPaintUpdates+1)throw Error('Normal update hid in-place text/paint mutation');
   if(a!==node('a'))throw Error('In-place content update replaced owner');
   scene.objects[2].arrows='end';scene.objects[2].arrowWidth=12;scene.objects[2].paint.color='#ff0055';
   if(!view.update(scene))throw Error('Normal marker style update failed');
   const freshLine=node('line');
   if(!view.previewPositions([],[{id:'line',points:[{x:600,y:100},{x:600,y:200},{x:900,y:200}]}])||freshLine.getAttribute('stroke-dasharray')!=='0 0 388 12'||freshLine.hasAttribute('marker-start'))throw Error('Route preview used stale arrow style');
   view.dispose();if(view.previewPositions([{id:'a',x:1,y:2}])!==false)throw Error('Disposed view accepted position preview');
   host.remove();return {serialized,updated:['a','b','line'],inPlaceContent:true,atomicRejections:true,routeOwnership:true,markerTrim:true};
  });
  assert.deepEqual(errors,[]);console.log('PASS '+engine+': '+JSON.stringify(result));
 }finally{await browser.close();await new Promise(resolve=>server.close(resolve));}
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
