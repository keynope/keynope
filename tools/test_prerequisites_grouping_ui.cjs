// Local-only optional-grouping checks: no participant infrastructure is contacted.
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
(async()=>{
  const browser=await chromium.launch({headless:true,...(process.env.CHROME_PATH?{executablePath:process.env.CHROME_PATH}:{})});
  try{
    const page=await browser.newPage({viewport:{width:1000,height:800}}),errors=[];
    page.on('pageerror',e=>errors.push(e.message));
    await page.setContent('<body style="background:#111c26;color:white;font:16px monospace"><main id="root"></main></body>');
    for(const file of ['web/activity-games.js','web/activity-design.js'])await page.addScriptTag({path:path.resolve(file)});
    const source=fs.readFileSync('main.go','utf8'),start=source.indexOf('  let activeEngagementEditor = null;');
    const end=source.indexOf("  const topbar = document.createElement('div');",start);
    assert(start>=0&&end>start);
    await page.addScriptTag({content:'var editorState={current:0,slides:[{}]},saved=null;async function editorAction(action){saved=action.engagementData;}function engagementButton(label,cls,action){const b=document.createElement("button");b.type="button";b.textContent=label;b.onclick=action;return b;}\n'+source.slice(start,end)});
    await page.evaluate(()=>openEngagementEditor());
    await page.getByRole('button',{name:'Prerequisites',exact:true}).click();
    const grouping=page.getByLabel('Group participants when finished');
    assert(await grouping.isChecked());
    await grouping.uncheck();
    assert((await page.locator('body').textContent()).includes('No groups or new pairing channel are created.'));
    await page.getByRole('button',{name:'Save activity',exact:true}).click();
    assert.equal(await page.evaluate(()=>saved.disableGrouping),true);
    await page.evaluate(()=>{editorState.slides[0].engagement=saved;openEngagementEditor();});
    assert(!(await grouping.isChecked()));
    await grouping.check();await page.getByRole('button',{name:'Save activity',exact:true}).click();
    assert.equal(await page.evaluate(()=>saved.disableGrouping),false);
    for(const presenter of [false,true]){
      for(const named of [false,true]){
        await page.evaluate(({presenter,named})=>{
          root.replaceChildren();
          window.r={definition:{kind:'prerequisites',disableGrouping:true,named},phase:3,game:{finishedCount:1,unfinishedCount:2,finishedNames:['Alex'],unfinishedNames:['Bea','Chris'],ranking:[{name:'Should not be ranked'}]}};
          KeynopeActivityDesign.mount(root,r,{presenter});KeynopeGames.renderPrerequisites(root,r,{presenter});
        },{presenter,named});
        assert.equal(await page.locator('.kn-prerequisite-tally').textContent(),'1 finished · 2 not finished');
        const text=await page.locator('#root').textContent();
        assert(!/Pairing channel|balanced group|ranked/.test(text));
        for(const name of ['Alex','Bea','Chris'])assert.equal(text.includes(name),named);
        assert.equal(await page.getByLabel('Elapsed time').count(),0);
      }
    }
    await page.screenshot({path:'/tmp/keynope-prerequisites-no-groups.png',fullPage:true});
    await page.setViewportSize({width:390,height:844});
    assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
    assert.deepEqual(errors,[]);
    console.log('PASS Prerequisites: grouping toggle saved/restored, counts, named/anonymous reveal, no ranking and mobile fit.');
  }finally{await browser.close();}
})().catch(error=>{console.error(error);process.exitCode=1;});
