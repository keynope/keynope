// Exercise selected-slide cache priority through the real WASM host.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-workspace-priority-site');
const server=http.createServer((req,res)=>{
  let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';
  const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}
  res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));
});
(async()=>{
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
  try{
    const context=await browser.newContext();await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
    const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
    await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');
    await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
    const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();
    await(await chooser).setFiles({name:'Priority.md',mimeType:'text/markdown',buffer:Buffer.from(Array.from({length:140},(_,i)=>'# Slide '+(i+1)).join('\n\n---\n\n'))});
    await page.waitForFunction(()=>deck.pages.length===140);
    const reports=await page.evaluate(async()=>{
      const select=async slide=>{const response=await fetch('/api/editor/action',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'select-slide',slide})});if(!response.ok)throw Error(await response.text());await response.json();};
      const workspace=()=>{const response=JSON.parse(keynopeWasmWorkspace(0,true));if(response.status!==200)throw Error(response.body);return JSON.parse(response.body);};
      await select(139);workspace();const late=workspace();
      if(late.projectionMetrics.normalReplacedSlides?.includes(139))throw Error('Current late slide was evicted');
      if(late.projectionMetrics.normalReplacedSlides.length!==12)throw Error('Unexpected cache eviction count');
      await select(0);workspace();const first=workspace();
      if(first.projectionMetrics.normalReplacedSlides?.includes(0))throw Error('Current first slide was evicted');
      if(first.projectionMetrics.normalReplacedSlides.length!==12)throw Error('Navigation changed retention limits');
      return {late:late.projectionMetrics.normalReplacedSlides,first:first.projectionMetrics.normalReplacedSlides};
    });
    assert.deepEqual(reports.late,Array.from({length:12},(_,i)=>i));
    assert.deepEqual(reports.first,Array.from({length:12},(_,i)=>128+i));
    assert.deepEqual(errors,[]);
    console.log('PASS: real WASM retains the active late slide and changes cache priorities on navigation within page limits.');
  }finally{await browser.close();server.close();}
})().catch(error=>{console.error(error);process.exitCode=1;server.close();});
