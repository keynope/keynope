// Owned PPTX file chooser import in real WASM; compare with native Go-reopened
// Markdown paint. No network traffic except the loopback test server.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-table-fragments-site'),output='/tmp/keynope-table-fragments-proof';
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm','.json':'application/json'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));const browser=await playwright.chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
  const errors=[];
  try{
    async function load(filename){
      const context=await browser.newContext({viewport:{width:1500,height:1000}});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());const page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
      await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
      const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles(path.join(output,filename));
      await page.waitForFunction(()=>deck.pages[pageIndex]?.scene?.objects.some(o=>o.kind==='table')&&document.querySelector('#stage .keynope-scene-table'));
      await page.waitForFunction(()=>document.querySelector('.keynope-modern-presentation')?.style.visibility==='visible');
      const projection=await page.evaluate(()=>{const o=deck.pages[pageIndex].scene.objects.find(o=>o.kind==='table');return {borders:o.table.borders,cells:o.table.cells.map(c=>({row:c.row,column:c.column,rowSpan:c.rowSpan,colSpan:c.colSpan})),lines:[...document.querySelectorAll('#stage .keynope-table-borders line')].map(n=>({color:n.getAttribute('stroke'),width:Number(n.getAttribute('stroke-width')),coordinates:['x1','y1','x2','y2'].map(a=>Number(n.getAttribute(a)))}))};});
      return {page,context,projection};
    }
    for(const kind of ['colours','off','theme','sparse']){
      const native=await load('perimeter-'+kind+'.pptx'),md=await load('perimeter-'+kind+'-imported.md');
      assert.deepEqual(native.projection.borders,md.projection.borders);assert.deepEqual(native.projection.cells,md.projection.cells);assert.equal(native.projection.lines.length,md.projection.lines.length);
      native.projection.lines.forEach((line,i)=>{const other=md.projection.lines[i];assert.equal(line.color,other.color);assert.equal(line.width,other.width);line.coordinates.forEach((n,j)=>assert(Math.abs(n-other.coordinates[j])<.01));});
      assert(native.projection.cells.some(c=>c.rowSpan===2&&c.colSpan===2));await native.page.screenshot({path:output+'/pptx-'+kind+'-import.png'});
      // Imported merged cells remain directly editable, not a raster surrogate.
      const state=await native.page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json())),i=state.slides[0].elements.findIndex(e=>e.kind==='table');await native.page.evaluate(({i,e})=>editorAction({action:'select-element',element:i,elementData:e}),{i,e:state.slides[0].elements[i]});
      await native.page.locator('.keynope-canvas-element.active').dblclick();const dialog=native.page.getByRole('dialog',{name:'Edit table cells',exact:true});await dialog.waitFor();assert.equal(await dialog.getByRole('gridcell').count(),3);await dialog.getByRole('button',{name:'Cancel',exact:true}).click();
      await native.context.close();await md.context.close();
    }
    assert.deepEqual(errors,[]);console.log('PASS: four editable segmented-perimeter PPTX imports match native Go-reopened Markdown seams/cell spans and browser SVG geometry; captures: '+output);
  }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);process.exitCode=1;server.close();});
