// Real WASM GIF export/reimport and deterministic production animation clocks.
const assert=require('node:assert/strict'),fs=require('node:fs'),http=require('node:http'),path=require('node:path');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright'),root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site'),output='/tmp/keynope-animation-export-proof';
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
 try{
  const context=await browser.newContext({viewport:{width:1500,height:1000}});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
  const open=async(name,data,type)=>{const picker=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await picker).setFiles({name,mimeType:type,buffer:data});};
  await open('animation.md',fs.readFileSync(output+'/animation.md'),'text/markdown');await page.waitForFunction(()=>deck.pages[0]?.scene?.backgroundMedia?.frames?.length===2&&deck.pages[0].scene.objects.some(o=>o.media?.frames?.length===2));
  const clock=async(name)=>{
   const result=await page.evaluate(async()=>{
    const scene=structuredClone(deck.pages[pageIndex].scene),media=[scene.backgroundMedia,...scene.objects.map(o=>o.media)].filter(m=>m?.frames?.length);if(media.length!==2||media.some(m=>m.loopCount!==2||m.frames[0].delayMs!==70||m.frames[1].delayMs!==190||!m.source))throw Error('Animation projection lost source/timing/loop');
    const host=document.createElement('div');host.style.cssText='position:fixed;left:0;top:0;width:960px;height:540px;z-index:999999;background:#222';document.body.append(host);window.animationProofHost=host;const view=KeynopeScene.mount(host,scene);view.element.style.cssText+='transform:scale(.5);transform-origin:0 0;';await view.ready;
    const results=[];for(const [time,index]of [[0,0],[100,1],[300,0],[1000,1]]){view.setTime(time,true);await Promise.all([...host.querySelectorAll('img')].map(n=>n.decode()));const sources=[...host.querySelectorAll('img')].map(n=>n.src).sort();if(JSON.stringify(sources)!==JSON.stringify(media.map(m=>m.frames[index].source).sort()))throw Error('Clock selected wrong foreground/background frame at '+time);results.push({time,index});}
    window.disposeAnimationProof=()=>{view.dispose();host.remove();};return results;
   });assert.deepEqual(result.map(r=>r.index),[0,1,0,1]);await page.screenshot({path:output+'/'+name+'.png'});await page.evaluate(()=>disposeAnimationProof());return result;
  };
  await clock('original-animation');const exported=await page.evaluate(()=>fetch('/api/editor/export-pptx',{method:'POST'}).then(r=>r.json()));assert(exported.content);assert(!(exported.report?.warnings||[]).some(w=>w.code==='image-animation-frozen'));fs.writeFileSync(output+'/wasm-export.pptx',Buffer.from(exported.content,'base64'));
  await open('Roundtrip animation.pptx',Buffer.from(exported.content,'base64'),'application/vnd.openxmlformats-officedocument.presentationml.presentation');await page.waitForFunction(()=>deck.pages[0]?.scene?.objects.filter(o=>o.media?.frames?.length===2).length===2);await clock('reimported-animation');assert.deepEqual(errors,[]);console.log('PASS: WASM foreground/background animation export/reimport, source fallback, 70/190ms delays, finite loops and production clock replay');
 }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);server.close();process.exitCode=1;});
