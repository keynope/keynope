// Production WASM/inspector proof: theme width zero is Off, not legacy fallback.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const {waitForAsync}=require('./wait_for_async.cjs');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site');
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));const browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
 try{
  const context=await browser.newContext({viewport:{width:1500,height:1000}});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
  await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles({name:'Theme zero.md',mimeType:'text/markdown',buffer:Buffer.from('<!-- keynope width=245 height=56 -->\n\n<!-- element-id=shape top=10 left=30 width=30 height=10 shape=square shape-stroke=#ff0055 -->\n[shape:square]\n')});
  await waitForAsync(page,async()=>(await fetch('/api/editor/state').then(r=>r.json())).path.endsWith('Theme zero.md'));
  const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json())),action=data=>page.evaluate(data=>editorAction(data),data);
  const original=await state(),theme=structuredClone(original.theme);assert(theme);theme.defaults.lineWidth=0;theme.defaults.shapeLine='#55ffff';await action({action:'set-deck-theme',themeData:theme});
  const s=await state(),i=s.slides[0].elements.findIndex(e=>e.id==='shape');assert(i>=0);await action({action:'select-element',element:i,elementData:{id:'shape'}});await page.getByRole('tab',{name:'Style',exact:true}).click();
  const width=page.getByRole('spinbutton',{name:'Selection stroke width',exact:true});await width.waitFor();
  await page.waitForFunction(()=>document.querySelector('[data-selection-property="paint.strokeWidth"]')?.value==='0');
  const shape=page.locator('#stage [data-object-id="shape"]>svg>path').first();await shape.waitFor();assert.equal(Number(await shape.getAttribute('stroke-width')),0,'Theme Off painted an SVG outline');
  await width.fill('3.5');await width.press('Tab');await page.waitForFunction(()=>document.querySelector('#stage [data-object-id="shape"]>svg>path')?.getAttribute('stroke-width')==='3.5');
  await page.getByRole('button',{name:'Inherit stroke width',exact:true}).click();await page.waitForFunction(()=>document.querySelector('[data-selection-property="paint.strokeWidth"]')?.value==='0'&&document.querySelector('#stage [data-object-id="shape"]>svg>path')?.getAttribute('stroke-width')==='0');
  theme.defaults.lineWidth=4.25;await action({action:'set-deck-theme',themeData:theme});await page.waitForFunction(()=>document.querySelector('[data-selection-property="paint.strokeWidth"]')?.value==='4.25'&&document.querySelector('#stage [data-object-id="shape"]>svg>path')?.getAttribute('stroke-width')==='4.25');
  await page.getByRole('button',{name:'Slide',exact:true}).click();await page.getByRole('button',{name:'Edit theme…',exact:true}).click();
  const dialog=page.getByRole('dialog',{name:'Edit deck theme',exact:true});assert(await dialog.getByRole('spinbutton',{name:'Theme line width',exact:true}).evaluate(n=>n.checkValidity()),'Theme dialog rejects an accepted fractional width');await dialog.getByRole('checkbox',{name:'No shape fill',exact:true}).check();await dialog.getByRole('button',{name:'Apply theme',exact:true}).click();await dialog.waitFor({state:'detached'});
  await page.waitForFunction(()=>document.querySelector('#stage [data-object-id="shape"]>svg>path')?.getAttribute('fill')==='none');
  await page.getByRole('tab',{name:'Style',exact:true}).click();const fill=page.getByRole('combobox',{name:'Selection shape fill',exact:true});await fill.selectOption('solid');
  await page.waitForFunction(()=>document.querySelector('#stage [data-object-id="shape"]>svg>path')?.getAttribute('fill')!=='none');
  await fill.selectOption('none');await page.waitForFunction(()=>document.querySelector('#stage [data-object-id="shape"]>svg>path')?.getAttribute('fill')==='none');
  await fill.selectOption('');await page.waitForFunction(()=>document.querySelector('#stage [data-object-id="shape"]>svg>path')?.getAttribute('fill')==='none');
  await page.screenshot({path:'/tmp/keynope-no-fill-inspector.png'});
  const saved=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'No fill.md'})}).then(r=>r.json()));assert(saved.content.includes('keynope-theme'));
  await fill.selectOption('solid');const explicit=await page.evaluate(()=>fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Solid fill.md'})}).then(r=>r.json()));assert(explicit.content.includes('shape-fill=solid'),'Markdown dropped explicit fill');
  assert.deepEqual(errors,[]);console.log('PASS: theme zero width, inspector/SVG agreement, explicit width/Inherit, fractional theme validation, theme None, object fill overrides and saved Markdown.');
 }finally{await browser.close();server.close();}
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
