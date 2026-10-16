// Real WASM edit/history and shared HTML renderer, owned offline diagram.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=process.env.TEST_SITE||'/tmp/keynope-powerup-site',output=process.env.KEYNOPE_ROUTE_EVIDENCE||'/tmp/keynope-stroke-route-proof';
const server=http.createServer((req,res)=>{
  let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';
  const fixture=['/routes.html','/routes-blocks.html'].includes(name);
  const file=fixture?output+name:path.resolve(root,'.'+name);
  if(!fs.existsSync(file)||(!fixture&&!file.startsWith(root+'/'))){res.writeHead(404);return res.end();}
  res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm','.json':'application/json'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));
});
(async()=>{
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const browser=await playwright.chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
  try{
    const context=await browser.newContext({viewport:{width:1500,height:1000}});
    await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
    const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
    page.on('dialog',async d=>{assert.equal(d.type(),'confirm');await d.accept();});
    const origin='http://127.0.0.1:'+server.address().port;
    await page.goto(origin+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
    const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();
    await(await chooser).setFiles({name:'Routes.md',mimeType:'text/markdown',buffer:fs.readFileSync(output+'/routes.md')});
    await page.waitForFunction(()=>document.querySelector('#stage polyline[data-object-id="line"]')?.getAttribute('points'));
    const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
    const action=data=>page.evaluate(data=>editorAction(data),data);
    const read=()=>page.evaluate(()=>{
      const p=deck.pages[pageIndex],s=p.scene,line=s.objects.find(o=>o.id==='line'),source=s.objects.find(o=>o.id==='source');
      const port=p.shapePorts.find(p=>p.id==='source'&&p.side==='right');
      return {points:line.points,arrows:line.arrows,arrowWidth:line.arrowWidth,dash:document.querySelector('#stage polyline[data-object-id="line"]').getAttribute('stroke-dasharray'),x:source.bounds.x+source.bounds.width,stroke:source.paint.strokeWidth,port:port.x*1920/deck.cols,path:document.querySelector('#stage polyline[data-object-id="line"]').getAttribute('points')};
    });
    const shaft=r=>{let length=0;for(let i=1;i<r.points.length;i++)length+=Math.hypot(r.points[i].x-r.points[i-1].x,r.points[i].y-r.points[i-1].y);const start=['start','both'].includes(r.arrows)?r.arrowWidth:0,end=['end','both'].includes(r.arrows)?r.arrowWidth:0;assert.deepEqual(r.dash.split(' ').map(Number),length<=start+end?[0,length+1]:[0,start,length-start-end,end],'Shaft hides arrow tip or retains stale route-length trim');};
    const before=await read();shaft(before);assert(Math.abs(before.points[0].x-before.x-24)<1e-6);assert(Math.abs(before.port-before.points[0].x)<1e-6);
    await page.evaluate(()=>{window.__strokeRouteOwner=document.querySelector('#stage polyline[data-object-id="line"]');});
    const s=await state(),i=s.slides[0].elements.findIndex(e=>e.id==='source');
    await action({action:'select-element',element:i,elementData:s.slides[0].elements[i]});await page.getByRole('tab',{name:'Style',exact:true}).click();
    const width=page.getByRole('spinbutton',{name:'Selection stroke width',exact:true});await width.fill('0');await width.press('Tab');
    await page.waitForFunction(()=>{const o=deck.pages[pageIndex].scene.objects.find(o=>o.id==='source');return o&&(o.paint.strokeWidth||0)===0;});
    const after=await read();shaft(after);assert(Math.abs(after.points[0].x-after.x)<1e-6);assert.notEqual(after.path,before.path);
    assert.equal(await page.locator('#stage [data-object-id="source"]>svg>path').first().getAttribute('stroke-width'),'0','Width zero repainted a fallback hairline');
    assert(await page.evaluate(()=>window.__strokeRouteOwner===document.querySelector('#stage polyline[data-object-id="line"]')),'stroke edit remounted connector');
    await page.getByRole('button',{name:'Undo',exact:true}).click();await page.waitForFunction(()=>deck.pages[pageIndex].scene.objects.find(o=>o.id==='source')?.paint.strokeWidth===48);
    assert.deepEqual((await read()).points,before.points);
    await page.screenshot({path:output+'/editor-routes.png'});
    const standalone=await context.newPage();standalone.on('pageerror',e=>errors.push(e.message));await standalone.goto(origin+'/routes.html');
    await standalone.waitForFunction(()=>document.querySelector('.keynope-modern-presentation')?.style.visibility==='visible');
    await standalone.screenshot({path:output+'/routes-vector.png'});
    await standalone.goto(origin+'/routes-blocks.html');
    await standalone.waitForFunction(()=>document.querySelector('#stage [data-object-id="source"] canvas'));
    await standalone.screenshot({path:output+'/routes-blocks.png'});
    assert.deepEqual(errors,[]);console.log('PASS: outer stroke ports, live width/Off/Undo routes, retained connector owner, vector/block presentation screenshots: '+output);
  }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);process.exitCode=1;server.close();});
