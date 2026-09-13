const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const assert=require('node:assert/strict');
const path=require('node:path');
(async()=>{
  const browser=await chromium.launch({headless:true,...(process.env.CHROME_PATH?{executablePath:process.env.CHROME_PATH}:{})});
  try{
    const page=await browser.newPage({viewport:{width:1280,height:800}});
    await page.setContent('<body style="background:#101419;color:white;font:18px monospace"><main id="root"></main></body>');
    await page.addScriptTag({path:path.resolve('web/activity-games.js')});
    await page.evaluate(()=>{
      window.r={definition:{kind:'gallery',options:['Claims workflow','Transaction triage'],named:true},phase:1,memberNames:{a:'Alice'},game:{votes:{},entries:Array.from({length:120},(_,i)=>({item:i%2,symbol:['★','?','♥'][i%3],text:'Specific feedback '+i,name:'Alice'}))}};
      window.draw=()=>{root.replaceChildren();KeynopeGames.render(root,r,{presenter:true});};draw();
    });
    assert.equal(await page.getByText('120 contributions collected').count(),1);
    assert.equal(await page.getByText('Specific feedback',{exact:false}).count(),0);
    await page.evaluate(()=>{r.phase=2;draw();});
    assert.equal(await page.getByText('Specific feedback',{exact:false}).count(),0);
    await page.evaluate(()=>{r.phase=3;draw();});
    assert.equal(await page.getByRole('button',{name:'Read & discuss',exact:true}).count(),6);
    await page.getByRole('button',{name:'Read & discuss',exact:true}).first().click();
    assert.equal(await page.getByRole('button',{name:'Back to cards'}).count(),1);
    await page.getByRole('button',{name:'Next contribution'}).click();
    await page.evaluate(()=>draw());
    assert.equal(await page.getByText('Specific feedback 1',{exact:true}).count(),1);
    await page.getByRole('button',{name:'Back to cards'}).click();
    await page.getByLabel('Feedback type').selectOption('?');
    await page.getByLabel('Exhibit').selectOption('1');
    assert.equal(await page.getByText('Page 1 of 4 · 20 contributions').count(),1);
    await page.screenshot({path:'/tmp/keynope-gallery-review.png',fullPage:true});
    await page.setViewportSize({width:390,height:844});
    await page.evaluate(()=>{window.draft={};window.sent=[];r.phase=1;r.game=KeynopeGames.publicState(r);root.replaceChildren();KeynopeGames.render(root,r,{draft,send:async payload=>sent.push(payload)});});
    await page.getByLabel('Exhibit').selectOption('0');
    await page.getByRole('button',{name:'Question',exact:true}).click();
    await page.getByRole('textbox').fill('How are conflicting claims handled?');
    await page.getByRole('button',{name:'Submit feedback'}).click();
    assert.equal(await page.evaluate(()=>sent[0].symbol),'?');
    assert.equal(await page.getByRole('textbox').inputValue(),'');
    assert.equal(await page.getByText('Specific feedback',{exact:false}).count(),0);
    assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
    await page.screenshot({path:'/tmp/keynope-gallery-collect.png',fullPage:true});
    console.log('Gallery UI: private collection, 120-card review, filters, focus persistence and mobile submission passed.');
  }finally{await browser.close();}
})().catch(error=>{console.error(error);process.exitCode=1;});
