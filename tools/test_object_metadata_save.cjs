// Real WASM save must retain payloads referenced only by opaque nested fields.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http'),zlib=require('node:zlib');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-object-lifetime-site');
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
    await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
    await page.evaluate(()=>{const original=keynopeWasmInit;window.documentLoads=0;window.keynopeWasmInit=(...args)=>{const raw=original(...args);if(JSON.parse(raw).status!==200)throw Error('Document initialization failed');documentLoads++;return raw;};});
    const table={version:1,rows:[{height:1}],columns:[{width:1}],cells:[[{text:'Visible'}]]};
    const used=structuredClone(table);used.cells[0][0].futureLink={target:'orphan-chart'};
    const objects={version:1,tables:{'used-table':used,'orphan-table':table},charts:{'orphan-chart':{version:1,type:'column',categories:['One'],series:[{name:'A',values:[1]}]}}};
    const data=zlib.gzipSync(Buffer.from(JSON.stringify(objects))).toString('base64');
    const future={kind:'future-widget',id:'future-object',objectId:'orphan-chart',text:'*** literal\n---\nfuture',query:'top=30&render=truetype&element-style=retro&future=x%20y',placeholder:true,placeholderRole:'future-role',futureProperty:{path:'preserve'}};
    const markdown='<!-- keynope width=245 height=56 -->\n<!-- keynope-objects version=1 base64:'+data+' -->\n<!-- keynope-slide-id=opaque-slide -->\n<!-- element-id=visible-table object-id=used-table top=1 left=1 width=80 height=20 -->\n[table]\n\n<!-- keynope-element version=1 base64:'+Buffer.from(JSON.stringify(future)).toString('base64')+' -->\n';
    const open=async content=>{const expected=await page.evaluate(()=>documentLoads+1);const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles({name:'Opaque.md',mimeType:'text/markdown',buffer:Buffer.from(content)});await page.waitForFunction(expected=>documentLoads===expected,expected);await page.waitForFunction(()=>deck.pages[0]?.scene?.objects.some(o=>o.table?.cells.some(c=>c.text?.runs?.some(r=>r.text==='Visible'))));};
    const save=()=>page.evaluate(async()=>{const r=await fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Opaque.md'})});if(!r.ok)throw Error(await r.text());return (await r.json()).content;});
    const decode=content=>JSON.parse(zlib.gunzipSync(Buffer.from(content.match(/keynope-objects version=1 base64:([^ ]+)/)[1],'base64')));
    await open(markdown);const first=await save(),stored=decode(first);
    assert.deepEqual(stored.tables['used-table'].cells[0][0].futureLink,{target:'orphan-chart'});
    assert.ok(stored.tables['orphan-table']);assert.ok(stored.charts['orphan-chart']);
    assert.deepEqual(JSON.parse(Buffer.from(first.match(/keynope-element version=1 base64:([^ ]+)/)[1],'base64')),future);
    await open(first);assert.equal(await save(),first);
    assert.deepEqual(errors,[]);
    console.log('PASS: WASM save/reopen preserves unknown element kinds, nested opaque metadata, potential dependencies and deterministic Markdown.');
  }finally{await browser.close();server.close();}
})().catch(error=>{console.error(error);process.exitCode=1;server.close();});
