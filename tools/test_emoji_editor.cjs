// Test the real picker/insert/edit/export flow. A URL tests the native HTTP
// engine; otherwise serve TEST_SITE to exercise the Go/WASM editor.
const fs=require('node:fs'),path=require('node:path'),http=require('node:http'),assert=require('node:assert/strict');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'terraform/site');
const server=http.createServer((req,res)=>{
 let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';
 const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);res.end();return;}
 res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm','.json':'application/json'})[path.extname(file)]||'application/octet-stream');fs.createReadStream(file).pipe(res);
});
(async()=>{
 if(!process.argv[2])await new Promise(r=>server.listen(0,'127.0.0.1',r));
 const url=process.argv[2]||'http://127.0.0.1:'+server.address().port+'/editor/';
 for(const engine of ['chromium','webkit']){
  const browser=await playwright[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
  try{
   const page=await browser.newPage({viewport:{width:1440,height:1000}}),errors=[];
   page.on('pageerror',error=>errors.push(error.message));await page.goto(url);
   if(!process.argv[2])await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:120000});
   // The HTTP harness has no native companion to push revised workspace pages.
   const syncNative=async()=>{if(process.argv[2])await page.evaluate(async()=>keynopeLoadWebWorkspace(await fetch('/test/workspace').then(r=>r.json())));};
   if(process.argv[2]){await page.waitForFunction(()=>typeof keynopeLoadWebWorkspace==='function');await syncNative();}
   const button=name=>page.getByRole('button',{name,exact:true});
   if(await button('Done').isVisible()&&await button('Done').isEnabled())await button('Done').click();
   if(!await button('Add emoji').isVisible())await button('Insert').click();
   await button('Add emoji').click();
   const choice=button('smiling face with heart-eyes');await choice.waitFor({timeout:30000});
   await page.waitForFunction(()=>{const c=document.querySelector('[aria-label="smiling face with heart-eyes"] canvas');return c&&c.getContext('2d').getImageData(0,0,c.width,c.height).data.some((v,i)=>i%4===3&&v>100);});
   fs.mkdirSync('output/emoji-editor-tests',{recursive:true});await page.screenshot({path:'output/emoji-editor-tests/'+(process.argv[2]?'native-':'wasm-')+engine+'-picker.png'});
   await choice.click();
   const size=page.getByRole('spinbutton',{name:'TrueType font size',exact:true});await size.waitFor();assert.equal(await size.inputValue(),'512');
   const selected=()=>page.evaluate(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[s.current].elements[s.selected];});
   assert.equal((await selected()).text,'😍');assert.equal(new URLSearchParams((await selected()).query).get('render'),'truetype');
   await size.fill('137');await size.press('Tab');
   await page.waitForFunction(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json()),e=s.slides[s.current].elements[s.selected];return new URLSearchParams(e.query||'').get('ttf-size')==='137';});
   await syncNative();
   await page.getByRole('tab',{name:'Style',exact:true}).click();
   const tint=page.getByRole('checkbox',{name:'Enable emoji tint',exact:true});await tint.check();
   assert.equal(await page.getByText('Emoji tint',{exact:true}).evaluate(e=>getComputedStyle(e).color),'rgb(238, 238, 238)','tint label is readable on the dark toolbar');
   assert.equal(new URLSearchParams((await selected()).query).get('tint'),'#ffffff');
   await button('Choose emoji tint').click();
   const picker=page.getByRole('dialog',{name:'Pick color',exact:true});await picker.getByRole('textbox',{name:'HTML color',exact:true}).fill('#55aaff');await picker.getByRole('button',{name:'Apply',exact:true}).click();
   assert.equal(new URLSearchParams((await selected()).query).get('tint'),'#55aaff');
   await syncNative();await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));
   await page.screenshot({path:'output/emoji-editor-tests/'+(process.argv[2]?'native-':'wasm-')+engine+'-tint.png'});
   await tint.uncheck();assert.equal(new URLSearchParams((await selected()).query).has('tint'),false);
   await page.getByRole('tab',{name:'Text',exact:true}).click();
   await button('Edit text').click();
   const input=page.getByRole('textbox',{name:'Edit slide text',exact:true});await input.fill('D😍D 👩🏽‍🚀 🇳🇱');
   await button('Apply text').click();await input.waitFor({state:'detached'});
   const data=await page.evaluate(async()=>{
    const state=await fetch('/api/editor/state').then(r=>r.json()),scene=await fetch('/api/editor/scene?slide='+state.current).then(r=>r.json());
    const plain=text=>(text.runs||[]).map(run=>run.text||'').join('')||(text.paragraphs||[]).flatMap((paragraph,index)=>[index?'\n':'',...(paragraph.runs||[]).map(run=>run.text||'')]).join('');
    return scene.objects.find(object=>object.text&&plain(object.text)==='D😍D 👩🏽‍🚀 🇳🇱')?.text;
   });
   assert(data,'edited text exported through the shared scene');assert.equal(Object.keys(data.emojiFonts||{}).length,3);
   const shaped=page.locator('#stage .keynope-shaped-text').filter({hasText:'D😍D 👩🏽‍🚀 🇳🇱'}).last();await shaped.waitFor();
   const rendered=await shaped.locator('.keynope-shaped-emoji').evaluateAll(nodes=>nodes.map(node=>node.getBoundingClientRect().width));
   assert.equal(rendered.length,3,'shared text renderer paints every emoji sequence once');assert(rendered.every(width=>width>0),'every emoji has visible shaped width');
   await syncNative();
   await page.screenshot({path:'output/emoji-editor-tests/'+(process.argv[2]?'native-':'wasm-')+engine+'-slide.png'});
   assert.deepEqual(errors,[]);console.log('PASS '+engine+': '+(process.argv[2]?'native':'WASM')+' picker, capped insertion, resize, mixed-text edit and self-contained font payload');
  }finally{await browser.close();}
 }
})().catch(e=>{console.error(e);process.exitCode=1;}).finally(()=>server.close());
