// Local UI regression test; does not connect to participant infrastructure.
const fs=require('node:fs');
const path=require('node:path');
const assert=require('node:assert/strict');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
(async()=>{
  const browser=await chromium.launch({headless:true,...(process.env.CHROME_PATH?{executablePath:process.env.CHROME_PATH}:{})});
  try{
    const page=await browser.newPage({viewport:{width:1280,height:900}});
    const errors=[];page.on('pageerror',error=>errors.push(error.message));
    await page.setContent('<body style="margin:16px;background:#111c26;color:white;font:16px monospace"><main id="root"></main></body>');
    for(const file of ['web/activity-games.js','web/activity-design.js'])await page.addScriptTag({path:path.resolve(file)});
    const source=fs.readFileSync('main.go','utf8');
    const start=source.indexOf('  let activeEngagementEditor = null;');
    const end=source.indexOf("  const topbar = document.createElement('div');",start);
    assert(start>=0&&end>start);
    await page.addScriptTag({content:'var editorState={current:0,slides:[{}]};var saved=null;async function editorAction(action){saved=action.engagementData;}function engagementButton(label,cls,action){const b=document.createElement("button");b.type="button";b.textContent=label;b.onclick=action;return b;}\n'+source.slice(start,end)});
    await page.evaluate(()=>openEngagementEditor());
    await page.getByRole('button',{name:'The Chosen',exact:true}).click();
    // Let the form's scheduled initial prompt focus finish before filling a
    // different field; otherwise the browser can redirect typed input.
    await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));
    const count=page.getByLabel('Number of people to choose');
    assert.equal(await count.inputValue(),'1');
    assert.equal(await page.getByLabel('Anonymous',{exact:true}).count(),0);
    await count.fill('0');await page.getByRole('button',{name:'Save activity',exact:true}).click();
    assert.equal(await page.evaluate(()=>saved),null);
    await count.fill('2.5');await page.getByRole('button',{name:'Save activity',exact:true}).click();
    assert.equal(await page.evaluate(()=>saved),null);
    await count.fill('3');await page.getByRole('button',{name:'Save activity',exact:true}).click();
    assert.equal(await page.evaluate(()=>saved.chosenCount),3);
    assert.equal(await page.evaluate(()=>saved.named),true);
    await page.evaluate(()=>{editorState.slides[0].engagement=saved;openEngagementEditor();});
    assert.equal(await count.inputValue(),'3');
    await page.getByRole('button',{name:'Cancel',exact:true}).click();
    await page.evaluate(()=>{
      window.r={definition:{kind:'chosen',chosenCount:3,named:true},phase:1,memberNames:{a:'Dennis',b:'Charlie',c:'Kevin',d:'Alex'}};
      window.renderChosen=()=>{root.replaceChildren();KeynopeActivityDesign.mount(root,r);KeynopeGames.render(root,r,{identity:'a'});};
      renderChosen();
    });
    await page.getByText('You are in the draw. Stay here for the reveal.',{exact:true}).waitFor();
    assert.equal(await page.locator('.kn-activity-intro h2').textContent(),'Waiting for the draw');
    assert(!(await page.locator('#root').textContent()).includes('YOUR TURN'));
    await page.evaluate(()=>{KeynopeGames.next(r);renderChosen();});
    assert.equal(await page.locator('.kn-chosen-name').count(),3);
    const names=await page.locator('.kn-chosen-name strong').allTextContents();
    assert.equal(new Set(names).size,3);
    assert.equal(await page.locator('.kn-activity-intro h2').textContent(),'The Chosen');
    assert(!(await page.locator('#root').textContent()).includes('See what we made together'));
    await page.screenshot({path:'/tmp/keynope-chosen-desktop.png',fullPage:true});
    await page.setViewportSize({width:390,height:844});
    await page.screenshot({path:'/tmp/keynope-chosen-mobile.png',fullPage:true});
    assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
    // A fresh participant render receives the frozen public result, without redrawing.
    await page.evaluate(()=>{r={definition:r.definition,phase:3,game:JSON.parse(JSON.stringify(KeynopeGames.publicState(r)))};renderChosen();});
    assert.deepEqual(await page.locator('.kn-chosen-name strong').allTextContents(),names);
    const syncStart=source.indexOf('function syncEngagementRuntime(runtime) {');
    const syncEnd=source.indexOf("\tdocument.addEventListener('keydown',event => {",syncStart);
    assert(syncStart>=0&&syncEnd>syncStart);
    await page.addScriptTag({content:'var keynopeEngagementControllerSurface=false,keynopeEngagementRuntime=null,keynopeEngagementOverlay=root;function renderEngagementRuntime(){root.replaceChildren();KeynopeActivityDesign.mount(root,keynopeEngagementRuntime,{presenter:true});KeynopeGames.render(root,keynopeEngagementRuntime,{presenter:true});}function runEngagementCountdown(){}\n'+source.slice(syncStart,syncEnd)});
    await page.evaluate(()=>syncEngagementRuntime({...r,slide:0}));
    assert.deepEqual(await page.locator('.kn-chosen-name strong').allTextContents(),names,'Presentation window receives the same selected names');
    assert.deepEqual(errors,[]);
    console.log('PASS The Chosen: editor validation/save/reopen, participant joining, reveal, mobile layout and refreshed result.');
  }finally{await browser.close();}
})().catch(error=>{console.error(error);process.exitCode=1;});
