const {waitForAsync}=require("./wait_for_async.cjs");
// Actual WASM imports, owned fixture, source/Go-reopened geometry and word
// rendering. No Office dependency, system clipboard or cloud traffic.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-rich-pptx-site'),output=process.env.KEYNOPE_RICH_PPTX_EVIDENCE||'/tmp/keynope-rich-pptx-proof';
const server=http.createServer((req,res)=>{let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));});
(async()=>{
  await new Promise(r=>server.listen(0,'127.0.0.1',r));const browser=await playwright.chromium.launch({headless:true,executablePath:process.env.CHROME_PATH}),errors=[];
  try{
    const load=async file=>{
      const context=await browser.newContext({viewport:{width:1500,height:1050}});await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
      const page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
      await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
      const chooser=page.waitForEvent('filechooser');await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();await(await chooser).setFiles(path.join(output,file));
      await waitForAsync(page,async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides?.length===4&&deck.pages.length===4;});return {context,page};
    };
    const source=await load('source.md'),pptx=await load('words.pptx'),md=await load('imported.md');
    for(let index=0;index<4;index++){
      const samples=[];
      for(const [label,{page}]of [['source',source],['pptx',pptx],['md',md]]){
        await page.evaluate(async i=>{await editorAction({action:'select-slide',slide:i});await KeynopeScene.ensureFonts();await modernPageView.ready;await new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)));},index);
        await page.waitForFunction(()=>document.querySelector('.keynope-modern-presentation')?.style.visibility==='visible');
        samples.push(await page.evaluate(()=>{const o=deck.pages[pageIndex].scene.objects.find(o=>{const t=o.text||o.label;return t&&(t.runs?.some(r=>r.text)||t.paragraphs?.some(p=>p.runs?.some(r=>r.text)));}),t=o.text||o.label,p=o.labelPaint||o.paint;
          const effective=r=>({...r,fontId:r.fontId||t.fontId,fontSize:r.fontSize||t.size,color:(r.color||p.color).toLowerCase()});
          const runs=t.paragraphs?t.paragraphs.flatMap((p,i)=>[...(i?[{text:'\n'}]:[]),...p.runs]):t.runs;
          return {plain:runs.map(r=>r.text).join(''),runes:runs.flatMap(r=>[...r.text].map(ch=>ch==='\n'?{text:ch}:{...effective(r),text:ch})),align:t.align,vertical:t.vertical,size:t.size,font:t.fontId,lineHeight:t.lineHeight,before:t.paragraphBefore,after:t.paragraphAfter};}));
        await page.locator('#stage').screenshot({path:output+'/'+label+'-slide-'+(index+1)+'.png'});
      }
      assert.deepEqual(samples[1],samples[2],'WASM/native Go imports disagree on slide '+index);
      // Native and Markdown may omit false/zero fields; compare effective
      // rune styles rather than serialization spelling or span coalescing.
      const canonical=s=>JSON.parse(JSON.stringify(s,(key,value)=>value===false||value===0?undefined:value));
      assert.deepEqual(canonical(samples[1]),canonical(samples[0]),'word formatting lost on slide '+index);
    }
    // Imported text is editable, not a screenshot/image fallback.
    await pptx.page.evaluate(()=>editorAction({action:'select-slide',slide:0}));
    const textID=await pptx.page.evaluate(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[0].elements.find(e=>e.kind==='text').id;});
    await pptx.page.locator('.keynope-canvas-element[data-element-id="'+textID+'"]').dblclick();
    const dialog=pptx.page.locator('dialog[data-keynope-scene-dialog="text"]');await dialog.waitFor();
    assert((await dialog.getByRole('textbox',{name:'Edit slide text',exact:true}).innerText()).includes('Underlined'));
    await dialog.getByRole('button',{name:'Cancel edit',exact:true}).click();
    for(const item of [source,pptx,md])await item.context.close();assert.deepEqual(errors,[]);
    console.log('PASS: four source/editable-PPTX/Go-Markdown slides preserve effective word fonts/sizes/colours/emphasis, paragraph settings and editable text; captures '+output);
  }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);process.exitCode=1;server.close();});
