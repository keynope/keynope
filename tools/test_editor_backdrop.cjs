// Owned decks, actual WASM editor and exported presentation; no cloud traffic.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=process.env.TEST_SITE||'/tmp/keynope-powerup-site',output='/tmp/keynope-backdrop-proof';
let presentation='';
const server=http.createServer((req,res)=>{
  let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';
  if(name==='/presentation.html'){res.setHeader('Content-Type','text/html');return res.end(presentation);}
  const file=path.resolve(root,'.'+name);
  if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}
  res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm','.json':'application/json'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));
});
(async()=>{
  fs.mkdirSync(output,{recursive:true});await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const browser=await playwright.chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
  try{
    const context=await browser.newContext({viewport:{width:1400,height:1000}});
    await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
    const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
    page.on('dialog',async dialog=>{assert.equal(dialog.type(),'confirm');await dialog.accept();});
    const origin='http://127.0.0.1:'+server.address().port;
    await page.goto(origin+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
    const chooser=page.waitForEvent('filechooser');
    await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();
    await(await chooser).setFiles({name:'Backdrop.md',mimeType:'text/markdown',buffer:Buffer.from('<!-- keynope width=245 height=56 -->\n\n<!-- bg=#e9dec9 -->\n\n<!-- top=15 left=20 render=truetype modern-font=sans modern-size=90 fg=#234359 -->\nSlide background stays inside the canvas\n\n---\n<!-- bg=#143c58 -->\n\n<!-- top=15 left=20 render=truetype modern-font=sans modern-size=90 -->\nDark slide\n')});
    await page.waitForFunction(()=>deck.pages[0]?.scene?.background==='#e9dec9'&&document.querySelector('.keynope-scene-backdrop')?.width>0);
    const check=async expected=>{
      const result=await page.evaluate(()=>{
        const stage=document.querySelector('#stage'),canvas=document.querySelector('.keynope-scene-backdrop'),box=canvas.getBoundingClientRect();
        const pixel=[...canvas.getContext('2d').getImageData(0,0,1,1).data];
        return {stage:getComputedStyle(stage).backgroundColor,inline:stage.style.background,pixel,top:box.top,bottom:box.bottom,height:innerHeight};
      });
      assert.equal(result.stage,'rgb(29, 32, 36)');assert.equal(result.inline,'');assert.deepEqual(result.pixel,expected);
      assert(result.top>76&&result.bottom<result.height-72,'Missing visible top/bottom pasteboard');
    };
    await check([233,222,201,255]);await page.screenshot({path:output+'/editor-light.png'});
    await page.evaluate(()=>{pageIndex=1;render();});await check([20,60,88,255]);
    await page.screenshot({path:output+'/editor-dark.png'});
    // Empty and zoomed/panned slides still have their own bounded fill.
    await page.evaluate(()=>{pageIndex=0;deck.pages[0].scene={...deck.pages[0].scene,objects:[]};window.KEYNOPE_EDITOR_VIEWPORT={zoom:.5,x:15,y:10};resize();render();});
    await check([233,222,201,255]);await page.screenshot({path:output+'/editor-empty-zoomed.png'});
    // A configured application background must survive subsequent renders.
    await page.evaluate(()=>{document.documentElement.style.setProperty('--keynope-workspace-background','#272a30');render();});
    assert.equal(await page.locator('#stage').evaluate(n=>getComputedStyle(n).backgroundColor),'rgb(39, 42, 48)');
    presentation=await page.evaluate(()=>fetch('/api/editor/export-document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Backdrop.html'})}).then(r=>r.json()).then(r=>r.content));
    const external=await context.newPage();external.on('pageerror',e=>errors.push(e.message));await external.goto(origin+'/presentation.html');
    await external.waitForFunction(()=>document.querySelector('.keynope-modern-presentation')?.style.visibility==='visible');
    assert.equal(await external.locator('#stage').evaluate(n=>getComputedStyle(n).backgroundColor),'rgb(233, 222, 201)');
    await external.screenshot({path:output+'/presentation-light.png'});
    await external.evaluate(()=>{pageIndex=1;render();});
    assert.equal(await external.locator('#stage').evaluate(n=>getComputedStyle(n).backgroundColor),'rgb(20, 60, 88)');
    assert.deepEqual(errors,[]);
    console.log('PASS: light/dark/empty slides retain bounded fill; zoom/pan and custom application backdrop; exported presentation unchanged. Screenshots: '+output);
  }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);process.exitCode=1;server.close();});
