const fs=require('node:fs'),path=require('node:path'),http=require('node:http'),assert=require('node:assert/strict');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright'),root=path.resolve('terraform/site');
const server=http.createServer((req,res)=>{
 let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';
 const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);res.end();return;}
 res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm'})[path.extname(file)]||'application/octet-stream');fs.createReadStream(file).pipe(res);
});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));
 for(const engine of ['chromium','webkit']){
  const browser=await pw[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
  try{
   const page=await browser.newPage({viewport:{width:1600,height:1000}}),errors=[];page.on('pageerror',e=>errors.push(e.message));
   await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:120000});
   const inkCentres=await page.evaluate(async()=>{
    await KeynopeTrueType.ready;
    return ['Hello shape','gy','ABC\nxyz'].map(text=>{
     const canvas=document.createElement('canvas');canvas.width=800;canvas.height=650;const ctx=canvas.getContext('2d');
     const line={role:'shape-label',col:20,row:40,parts:[{color:'#ffffff'}],trueType:{kind:'text',text,size:97,width:700,height:500,query:'render=truetype&text-align=center&text-valign=middle&shape-label-dy=0.25'}};
     KeynopeTrueType.draw(ctx,line,1,1,1920,1080,null);
     const pixels=ctx.getImageData(0,0,800,650).data;let top=650,bottom=-1;
     for(let y=0;y<650;y++)for(let x=0;x<800;x++)if(pixels[(y*800+x)*4+3]>127){top=Math.min(top,y);bottom=Math.max(bottom,y);}
     return {text,actual:(top+bottom+1)/2,expected:290.25};
    });
   });
   for(const result of inkCentres)assert(Math.abs(result.actual-result.expected)<=1,'visible shape text must be vertically centred: '+JSON.stringify(result));
   const headingPresets=await page.evaluate(()=>[KeynopeTrueType.presetSize(1),KeynopeTrueType.presetSize(2)]);
   await page.getByLabel('Default text size',{exact:true}).fill('126');await page.getByLabel('Default text size',{exact:true}).press('Tab');
   await page.getByLabel('Default font width (%)',{exact:true}).fill('75');await page.getByLabel('Default font width (%)',{exact:true}).press('Tab');
   await page.getByRole('button',{name:'Increase default text size',exact:true}).click();
   await page.getByRole('button',{name:'Add text',exact:true}).click();
   await page.getByLabel('TrueType font size',{exact:true}).waitFor();
   assert.equal(await page.getByLabel('TrueType font size',{exact:true}).inputValue(),'127');assert.equal(await page.getByLabel('TrueType width percent',{exact:true}).inputValue(),'75');
   for(const [i,title] of ['Add title','Add subtitle'].entries()){
    await page.getByRole('button',{name:'Done',exact:true}).click();
    await page.getByRole('button',{name:title,exact:true}).click();
    await page.getByLabel('TrueType font size',{exact:true}).waitFor();
    assert.equal(Number(await page.getByLabel('TrueType font size',{exact:true}).inputValue()),headingPresets[i],'H1/H2 must ignore Insert text defaults');
   }
   await page.getByRole('button',{name:'Done',exact:true}).click();
   assert.equal(await page.getByLabel('Default text size',{exact:true}).inputValue(),'127');
   assert.equal(await page.getByLabel('Default font width (%)',{exact:true}).inputValue(),'75');
   assert.deepEqual(await page.evaluate(()=>JSON.parse(localStorage.getItem('keynope-insert-text-defaults-v1'))),{size:127,width:75});
   const fixture='<!-- keynope width=245 height=56 -->\n\n<!-- top=20 left=80 width=60 height=10 shape=circle fg=#0055aa -->\n[shape:circle]';
   await page.evaluate(async md=>{const r=JSON.parse(keynopeWasmInit(md,'Shape.md',false));if(r.status!==200)throw Error(r.body);const w=JSON.parse(JSON.parse(keynopeWasmWorkspace()).body);w.current=0;await keynopeReloadWebDocument(w);},fixture);
   const hit=()=>page.locator('.keynope-canvas-element'),state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
   const getLabel=s=>JSON.parse(Buffer.from(new URLSearchParams(s.slides[0].elements[0].query).get('shape-label'),'base64').toString());
   await page.waitForFunction(()=>document.querySelectorAll('.keynope-canvas-element').length===1);
   await hit().dblclick();await page.getByRole('textbox',{name:'Edit element text'}).fill('Hello shape');
   await page.getByRole('button',{name:'Commit',exact:true}).click();
   await page.waitForFunction(async()=>new URLSearchParams((await fetch('/api/editor/state').then(r=>r.json())).slides[0].elements[0].query).has('shape-label'));
   let s=await state(),q=new URLSearchParams(s.slides[0].elements[0].query);assert.equal(getLabel(s).text,'Hello shape');assert(Number(q.get('width'))>60);assert.equal(q.get('fg'),'#0055aa');assert.equal(s.slides[0].elements.length,1);
   assert.equal(Number(q.get('height')),10,'typing across the curved boundary must not increase height');
   assert.equal(Number(q.get('left')),80,'growth must preserve left edge');assert.equal(Number(q.get('top')),20,'growth must preserve top edge');
   await hit().click();await page.getByLabel('Shape text size',{exact:true}).fill('130');await page.getByLabel('Shape text size',{exact:true}).press('Tab');
   await page.waitForFunction(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());const d=JSON.parse(atob(new URLSearchParams(s.slides[0].elements[0].query).get('shape-label')));return new URLSearchParams(d.query).get('ttf-size')==='130';});
   const resized=new URLSearchParams((await state()).slides[0].elements[0].query);assert(Number(resized.get('width'))>Number(q.get('width'))||Number(resized.get('height'))>Number(q.get('height')));
   await page.getByRole('tab',{name:'Style',exact:true}).click();
   await page.getByLabel('Text gradient',{exact:true}).selectOption('horizontal');
   await page.waitForFunction(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return new URLSearchParams(JSON.parse(atob(new URLSearchParams(s.slides[0].elements[0].query).get('shape-label'))).query).has('gradient-start');});
   await page.getByLabel('Text shadow',{exact:true}).selectOption('soft');
   await page.getByLabel('Text outline',{exact:true}).selectOption('light');
   await page.screenshot({path:'/tmp/keynope-shape-label-'+engine+'.png'});
   assert.equal(new URLSearchParams((await state()).slides[0].elements[0].query).get('fg'),'#0055aa');
   await hit().dblclick();await page.getByRole('textbox',{name:'Edit element text'}).fill('');await page.getByRole('button',{name:'Commit',exact:true}).click();
   await page.waitForFunction(async()=>!new URLSearchParams((await fetch('/api/editor/state').then(r=>r.json())).slides[0].elements[0].query).has('shape-label'));
   assert.equal((await state()).slides[0].elements.length,1,'removing text must keep shape');
   await page.getByRole('button',{name:'Undo',exact:true}).click();await page.waitForFunction(async()=>new URLSearchParams((await fetch('/api/editor/state').then(r=>r.json())).slides[0].elements[0].query).has('shape-label'));
   // Incremental editing of a small shape must reuse each expanded preview,
   // rather than fitting every keystroke against the original tiny box.
   await page.evaluate(async md=>{keynopeWasmInit(md,'Typing.md',false);const w=JSON.parse(JSON.parse(keynopeWasmWorkspace()).body);w.current=0;await keynopeReloadWebDocument(w);},fixture.replace('width=60 height=10','width=12 height=6'));
   await page.waitForFunction(()=>document.querySelectorAll('.keynope-canvas-element').length===1);
   await page.evaluate(()=>{window.__shapePreviews=[];const fetchPreview=window.fetch;window.fetch=async(...args)=>{const result=await fetchPreview(...args);if(String(args[0]).endsWith('/api/editor/preview')&&args[1]?.body){const sent=JSON.parse(args[1].body);const value=new URLSearchParams(sent.elementData?.query).get('shape-label');if(value){const text=JSON.parse(atob(value)).text;window.__shapePreviews.push({text,sent,payload:await result.clone().json()});}}return result;};});
   await hit().dblclick();const input=page.getByRole('textbox',{name:'Edit element text'});
   const preview=async text=>{await page.waitForFunction(text=>window.__shapePreviews.some(p=>p.text===text),text);return page.evaluate(text=>window.__shapePreviews.findLast(p=>p.text===text),text);};
   let typed='Hello';await input.fill(typed);let payload=(await preview(typed)).payload;
   let fitted=new URLSearchParams(payload.elementData.query),initialHeight=Number(fitted.get('height'));
   for(const char of ' shape!'){
    await page.waitForTimeout(100);
    typed+=char;await input.pressSequentially(char);
    const received=await preview(typed),sent=new URLSearchParams(received.sent.elementData.query);
    assert(Number(sent.get('width'))>=Number(fitted.get('width')),'next keystroke must use expanded preview width');
    assert.equal(Number(sent.get('height')),Number(fitted.get('height')),'next keystroke must use expanded preview height');
    payload=received.payload;
    const next=new URLSearchParams(payload.elementData.query);
    assert.equal(Number(next.get('height')),initialHeight,'typing must keep the first fitted height');
    assert(Number(next.get('width'))>=Number(fitted.get('width')),'preview must not shrink');fitted=next;
   }
   await page.evaluate(()=>{window.__shapeCommits=[];const f=window.fetch;window.fetch=(...args)=>{if(String(args[0]).endsWith('/api/editor/action')&&args[1]?.body)window.__shapeCommits.push(JSON.parse(args[1].body));return f(...args);};});
   await input.press('Enter');
   const newlinePreview=(await preview(typed+'\n')).payload;
   assert(Number(new URLSearchParams(newlinePreview.elementData.query).get('height'))>initialHeight,'newline preview should grow');
   await input.press('Enter');
   await page.waitForFunction(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());const value=new URLSearchParams(s.slides[0].elements[0].query).get('shape-label');return value&&JSON.parse(atob(value)).text==='Hello shape!';});
   assert.equal(Number(new URLSearchParams((await state()).slides[0].elements[0].query).get('height')),initialHeight,'commit must preserve preview height: '+JSON.stringify(await page.evaluate(()=>window.__shapeCommits)));
   assert.equal(Number(new URLSearchParams((await state()).slides[0].elements[0].query).get('width')),Number(fitted.get('width')),'discarded newline must not retain temporary width growth');
   assert.deepEqual(errors,[]);console.log('PASS '+engine+': shape label editing, growth, separate typography, removal, undo and incremental typing');
  }finally{await browser.close();}
 }
})().catch(e=>{console.error(e);process.exitCode=1}).finally(()=>server.close());
