// Visual behaviour regression checks, independent of the live channel transport.
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const assert=require('node:assert/strict');
const path=require('node:path');
const fs=require('node:fs');
(async()=>{
  const browser=await chromium.launch({headless:true,...(process.env.CHROME_PATH?{executablePath:process.env.CHROME_PATH}:{})});
  try{
    const page=await browser.newPage({viewport:{width:390,height:844},reducedMotion:'reduce'});
    await page.setContent('<body style="background:#111c26;color:white;font:16px monospace"><main id="root"></main></body>');
    await page.addScriptTag({path:path.resolve('web/activity-design.js')});
    await page.evaluate(()=>{
      window.r={definition:{kind:'storm'},phase:3};
      KeynopeActivityDesign.mount(root,r);
      const results=document.createElement('div');results.className='results';
      for(let i=0;i<40;i++){const idea=document.createElement('article');idea.className='idea';idea.textContent='Idea '+(i+1);results.append(idea);}
      root.append(results);KeynopeActivityDesign.results(results,r);
    });
    assert.equal(await page.locator('.idea:visible').count(),12);
    await page.getByRole('button',{name:'Next results',exact:true}).click();
    assert.equal(await page.locator('.idea:visible').first().textContent(),'Idea 13');
    assert.equal(await page.evaluate(()=>r.visualReviewPage),1);
    await page.getByRole('button',{name:'Next results',exact:true}).click();
    await page.getByRole('button',{name:'Next results',exact:true}).click();
    assert.equal(await page.locator('.idea:visible').count(),4);
    assert(await page.getByRole('button',{name:'Next results',exact:true}).isDisabled());
    assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
    await page.evaluate(()=>{
      root.replaceChildren();r={definition:{kind:'pulse',options:['One','Two','Three']},phase:3,attributions:[{choice:1},{choice:1},{choice:2}]};
      KeynopeActivityDesign.mount(root,r);
      const results=document.createElement('div');for(const text of r.definition.options){const row=document.createElement('div');row.className='result';row.textContent=text;results.append(row);}
      root.append(results);KeynopeActivityDesign.results(results,r);
    });
    assert.deepEqual(await page.locator('.kn-result-fill').evaluateAll(rows=>rows.map(r=>r.style.width)),['0%','100%','50%']);
    assert.equal(await page.locator('.kn-activity-intro.is-reveal').count(),1);
    // Cover every registered activity so new additions cannot silently inherit
    // generic reveal prose. Both surfaces share this module.
    const source=fs.readFileSync('web/activity-design.js','utf8');
    const catalog=source.slice(source.indexOf('const activities = {'),source.indexOf('const revealCopy = {'));
    const kinds=[...catalog.matchAll(/^    (\w+):\[/gm)].map(match=>match[1]);
    assert.equal(kinds.length,32);
    const headings=new Set();
    for(const kind of kinds){
      for(const presenter of [false,true]){
        const copy=await page.evaluate(({kind,presenter})=>{
          root.replaceChildren();KeynopeActivityDesign.mount(root,{definition:{kind},phase:3},{presenter});
          return {title:root.querySelector('h2').textContent,help:root.querySelector('p').textContent,label:root.querySelector('.kn-activity-eyebrow').textContent};
        },{kind,presenter});
        assert(copy.title&&copy.help,kind+' needs reveal copy');
        assert(!/See what we made together|Explore the results and listen|Review the responses below/.test(copy.title+' '+copy.help),kind+' has generic reveal text');
        assert(!copy.label.includes('TOGETHER'));
        if(!presenter)headings.add(copy.title);
        assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),kind+' must fit on mobile');
      }
    }
    assert.equal(headings.size,kinds.length,'Each activity has its own reveal heading');
    for(const [kind,phase,questionRevealed,label,title] of [
      ['storm',3,false,'REVEAL','Ideas from the room'],
      ['questions',2,false,'VOTE','Choose what matters most'],
      ['truefalse',1,true,'REVEAL','Fact or Fiction: the answer'],
      ['pair',3,false,'DISCUSS','Meet your discussion group'],
      ['pair',4,false,'DONE','Bring your discussion back'],
      ['chosen',1,false,'WAITING FOR DRAW','Waiting for the draw']
    ]){
      await page.evaluate(({kind,phase,questionRevealed})=>{root.replaceChildren();KeynopeActivityDesign.mount(root,{definition:{kind},phase,questionRevealed});},{kind,phase,questionRevealed});
      assert((await page.locator('.kn-activity-eyebrow').textContent()).endsWith(label));
      assert.equal(await page.locator('h2').textContent(),title);
    }
    // A compact contact sheet makes the new copy easy to visually review.
    await page.setViewportSize({width:1440,height:1000});
    await page.evaluate(kinds=>{root.replaceChildren();root.style.cssText='display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:12px';for(const kind of kinds){const card=document.createElement('section');root.append(card);KeynopeActivityDesign.mount(card,{definition:{kind},phase:3});}},kinds);
    await page.screenshot({path:'/tmp/keynope-activity-reveal-copy.png',fullPage:true});
    console.log('PASS activity design: pagination, vote bars, all 32 custom reveal headings, host/participant copy, phase labels and mobile width');
  }finally{await browser.close();}
})().catch(error=>{console.error(error);process.exitCode=1;});
