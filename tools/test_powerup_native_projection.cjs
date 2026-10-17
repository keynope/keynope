// Actual Go native transport + generated presenter client; no Mac app launch.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),cp=require('node:child_process');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright'),output='/tmp/keynope-native-projection-proof';
(async()=>{
  fs.mkdirSync(output,{recursive:true});for(const file of ['connection.json','stop']){try{fs.unlinkSync(path.join(output,file));}catch(e){if(e.code!=='ENOENT')throw e;}}
  const go=cp.spawn('go',['test','-run','^TestNativeProjectionBrowserFixture$','-count=1','./internal/app'],{env:{...process.env,GOCACHE:process.env.GOCACHE||'/tmp/keynope-go-cache',KEYNOPE_NATIVE_PROJECTION_EVIDENCE:output},stdio:['ignore','pipe','pipe']});
  let logs='';go.stdout.on('data',x=>logs+=x);go.stderr.on('data',x=>logs+=x);let exit=null;const finished=new Promise(r=>go.on('exit',code=>{exit=code;r(code);}));
  let browser;
  try{
    const deadline=Date.now()+90000;while(!fs.existsSync(output+'/connection.json')){if(exit!==null)throw Error('Fixture exited: '+logs);if(Date.now()>deadline)throw Error('Fixture did not start');await new Promise(r=>setTimeout(r,100));}
    const {url}=JSON.parse(fs.readFileSync(output+'/connection.json','utf8'));
    browser=await playwright.chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});const context=await browser.newContext({viewport:{width:1440,height:900}});
    await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());const page=await context.newPage(),errors=[],patches=[];
    page.on('pageerror',e=>errors.push(e.message));page.on('response',async r=>{if(new URL(r.url()).pathname==='/slides'){try{patches.push(await r.json());}catch{}}});
    await page.goto(url);await page.waitForFunction(()=>presenterDeckVersion>=1&&document.querySelector('#stage [data-object-id="object"]'),null,{timeout:90000});
    await page.evaluate(()=>{window.KEYNOPE_PRESENTER=false;window.__keptPage2=deck.pages.find(p=>p.slide===2);window.__keptOwner=document.querySelector('#stage [data-object-id="object"]');});
    const state=()=>page.evaluate(()=>fetch('/api/editor/state').then(r=>r.json()));
    const paint=async(index,color)=>{const s=await state(),id=s.slides[index].elements[0].id;await page.evaluate(async({index,id,color,revision})=>{const r=await fetch('/api/editor/action',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'set-selection-property',slide:index,sceneRevision:revision,objectIds:[id],property:'paint.color',propertyValue:color})});if(!r.ok)throw Error(await r.text());},{index,id,color,revision:s.version});};
    const settled=async()=>{const expected=(await state()).version,deadline=Date.now()+15000;for(;;){const p=await page.evaluate(()=>fetch('/proof',{cache:'no-store'}).then(r=>r.json()));if(p.pending===false&&p.requestedRevision>=expected)return;if(Date.now()>deadline)throw Error('Native refresh did not settle: '+JSON.stringify(p));await new Promise(r=>setTimeout(r,25));}};
    const follow=async()=>{await page.evaluate(async()=>{window.KEYNOPE_PRESENTER=true;await syncPresenterStateOnce();window.KEYNOPE_PRESENTER=false;});};
    const baseline=await page.evaluate(()=>presenterDeckVersion);
    await paint(0,'#ff00aa');await paint(1,'#55aa00');await settled();let proof=await page.evaluate(()=>fetch('/proof').then(r=>r.json()));assert(proof.projected.includes(1)&&proof.projected.every(index=>index===0||index===1),JSON.stringify(proof));assert.equal(proof.full,false);
    assert.equal(await page.evaluate(()=>presenterDeckVersion),baseline);await follow();
    assert.equal(await page.evaluate(()=>deck.pages.find(p=>p.slide===0).scene.objects[0].paint.color),'#ff00aa');assert.equal(await page.evaluate(()=>deck.pages.find(p=>p.slide===1).scene.objects[0].paint.color),'#55aa00');
    assert.equal(await page.evaluate(()=>deck.pages.find(p=>p.slide===2)===window.__keptPage2),true);assert.equal(await page.evaluate(()=>document.querySelector('#stage [data-object-id="object"]')===window.__keptOwner),true);
    // Two separate publications are missed; next refresh must include both.
    await paint(0,'#ffaa00');await settled();await paint(1,'#00aaff');await settled();await follow();
    assert.equal(await page.evaluate(()=>deck.pages.find(p=>p.slide===0).scene.objects[0].paint.color),'#ffaa00');assert.equal(await page.evaluate(()=>deck.pages.find(p=>p.slide===1).scene.objects[0].paint.color),'#00aaff');
    const s=await state(),theme=structuredClone(s.theme);theme.colors.accent[0]='#aa00ff';await page.evaluate(async theme=>{const r=await fetch('/api/editor/action',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'set-deck-theme',themeData:theme})});if(!r.ok)throw Error(await r.text());},theme);await settled();await follow();
    proof=await page.evaluate(()=>fetch('/proof').then(r=>r.json()));assert.deepEqual(proof.projected,[3]);assert.equal(await page.evaluate(()=>deck.pages.find(p=>p.slide===3).scene.objects[0].paint.color),'#aa00ff');
    assert.equal(await page.evaluate(()=>deck.pages.find(p=>p.slide===2)===window.__keptPage2),true);
    await page.screenshot({path:output+'/native-transport.png'});
    assert(patches.some(p=>p.delta&&JSON.stringify(p.replaceSlides)==='[0,1]'),'Missed updates did not use coherent delta');assert(patches.some(p=>p.delta&&JSON.stringify(p.replaceSlides)==='[3]'),'Theme consumer was not a scoped delta');
    assert.deepEqual(errors,[]);fs.writeFileSync(output+'/report.json',JSON.stringify({passed:true,patches:patches.map(p=>({version:p.version,delta:!!p.delta,baseVersion:p.baseVersion,replaceSlides:p.replaceSlides,pages:p.pages.length})),proof},null,2)+'\n');
    console.log('PASS: native Go coalescing/theme scopes, actual generated presenter catch-up over multiple versions, retained page/DOM owners and coherent transport. '+output);
  }finally{
    if(browser)await browser.close();fs.writeFileSync(output+'/stop','done');
    const code=await Promise.race([finished,new Promise(r=>setTimeout(()=>r('running'),10000))]);
    if(code==='running'){go.kill('SIGTERM');await finished;throw Error('Fixture did not stop');}if(code!==0)throw Error('Fixture failed: '+logs);
  }
})().catch(e=>{console.error(e);process.exitCode=1;});
