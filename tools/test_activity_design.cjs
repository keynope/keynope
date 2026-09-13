// Visual behaviour regression checks, independent of the live channel transport.
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const assert=require('node:assert/strict');
const path=require('node:path');
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
    console.log('PASS activity design: 40-result pagination, mobile width, proportional vote bars, reveal state');
  }finally{await browser.close();}
})().catch(error=>{console.error(error);process.exitCode=1;});
