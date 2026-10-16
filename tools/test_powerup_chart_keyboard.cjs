// Local headless checks of the production shared chart dialog; no app activation.
const assert=require('node:assert/strict'),path=require('node:path');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
(async()=>{
  const browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
  try{
    const page=await browser.newPage({viewport:{width:1400,height:1000}}),errors=[];
    page.on('pageerror',e=>errors.push(e.message));
    await page.setContent('<button id="origin">Edit</button>');
    await page.addScriptTag({path:path.resolve('web/charts.js')});
    await page.locator('#origin').focus();
    await page.evaluate(()=>{
      window.source={version:1,type:'column',categories:Array.from({length:55},(_,i)=>'Category '+(i+1)),series:[{name:'A',values:Array(55).fill(1)},{name:'B',values:Array(55).fill(2)}],options:{}};
      KeynopeCharts.edit(source,value=>window.accepted=value);
    });
    const cell=(row,column)=>page.locator(`[data-chart-row="${row}"][data-chart-column="${column}"]`);
    const active=()=>page.evaluate(()=>[Number(document.activeElement.dataset.chartRow),Number(document.activeElement.dataset.chartColumn)]);
    await cell(0,1).fill('1.000');
    assert.equal(await page.evaluate(()=>KeynopeCharts.isDirty()),false);
    await cell(0,1).fill('invalid');
    assert.equal(await page.evaluate(()=>KeynopeCharts.isDirty()),true);
    await cell(0,1).fill('1e0');
    assert.equal(await page.evaluate(()=>KeynopeCharts.isDirty()),false);
    await page.getByLabel('Axis minimum',{exact:true}).fill('3');
    assert.equal(await page.evaluate(()=>KeynopeCharts.isDirty()),true);
    await page.getByLabel('Axis minimum',{exact:true}).fill('');
    assert.equal(await page.evaluate(()=>KeynopeCharts.isDirty()),false);
    const pollSerializations=await page.evaluate(()=>{
      KeynopeCharts.isDirty();const original=JSON.stringify;let count=0;
      JSON.stringify=function(...args){count++;return original.apply(this,args);};
      try{for(let i=0;i<1000;i++)KeynopeCharts.isDirty();}finally{JSON.stringify=original;}
      return count;
    });
    assert.equal(pollSerializations,0);
    await cell(49,1).focus();await page.keyboard.press('Enter');assert.deepEqual(await active(),[50,1]);
    await page.keyboard.press('Shift+Enter');assert.deepEqual(await active(),[49,1]);
    await cell(49,2).focus();await page.keyboard.press('Tab');assert.deepEqual(await active(),[50,0]);
    await page.keyboard.press('Shift+Tab');assert.deepEqual(await active(),[49,2]);
    await page.keyboard.press('ArrowDown');assert.deepEqual(await active(),[50,2]);
    await page.keyboard.press('ArrowUp');assert.deepEqual(await active(),[49,2]);
    await cell(49,2).fill('34');await page.keyboard.press('Control+z');assert.deepEqual(await active(),[49,2]);assert.equal(await cell(49,2).inputValue(),'2');
    await page.keyboard.press('Control+y');assert.deepEqual(await active(),[49,2]);assert.equal(await cell(49,2).inputValue(),'34');
    await cell(49,2).evaluate(el=>el.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',isComposing:true,bubbles:true,cancelable:true})));assert.deepEqual(await active(),[49,2]);
    await cell(49,2).press('ArrowLeft');assert.deepEqual(await active(),[49,2]);
    await page.getByRole('button',{name:'Apply',exact:true}).click();
    assert.equal(await page.evaluate(()=>accepted.series[1].values[49]),34);
    assert.equal(await page.evaluate(()=>source.series[1].values[49]),2);
    assert.equal(await page.evaluate(()=>document.activeElement.id),'origin');
    await page.evaluate(()=>{
      window.wide={version:1,type:'column',categories:Array.from({length:55},(_,i)=>'Category '+(i+1)),series:Array.from({length:64},(_,i)=>({name:'Series '+(i+1),values:Array(55).fill(i+1)})),options:{}};
      KeynopeCharts.edit(wide,value=>window.wideAccepted=value);
    });
    assert.equal(await page.locator('[data-chart-row]').count(),450);
    await cell(0,8).focus();await page.keyboard.press('Tab');assert.deepEqual(await active(),[0,9]);
    await page.keyboard.press('Shift+Tab');assert.deepEqual(await active(),[0,8]);
    assert.equal(await page.evaluate(()=>KeynopeCharts.isDirty()),false);
    await page.getByRole('button',{name:'Next series',exact:true}).click();
    await cell(0,9).fill('900');
    await page.getByRole('button',{name:'Previous series',exact:true}).click();
    await page.getByRole('button',{name:'Apply',exact:true}).click();
    assert.equal(await page.evaluate(()=>wideAccepted.series[8].values[0]),900);
    assert.equal(await page.evaluate(()=>wideAccepted.series[63].values[54]),64);
    assert.equal(await page.evaluate(()=>wide.series[8].values[0]),9);
    await page.evaluate(()=>KeynopeCharts.edit({...wide,series:wide.series.slice(0,8)},()=>{}));
    await page.getByRole('button',{name:'Add series',exact:true}).click();
    assert.equal(await cell(0,9).count(),1);
    await page.getByRole('button',{name:'Remove series 9',exact:true}).click();
    assert.equal(await cell(0,1).count(),1);
    await page.getByRole('button',{name:'Add series',exact:true}).click();
    await page.getByLabel('Paste chart data').fill('Category\tOnly\nFirst\t7');
    await page.getByRole('button',{name:'Paste data',exact:true}).click();
    assert.equal(await cell(0,1).inputValue(),'7');
    await page.getByRole('button',{name:'Cancel',exact:true}).click();
    await page.evaluate(()=>{
      const draft=JSON.parse(JSON.stringify(wide));draft.series[63].values[54]='broken';
      window.validationCommits=0;KeynopeCharts.edit(draft,()=>validationCommits++);
    });
    await page.getByRole('button',{name:'Apply',exact:true}).click();
    assert.deepEqual(await active(),[54,64]);
    assert.equal(await cell(54,64).getAttribute('aria-invalid'),'true');
    assert.equal(await cell(54,64).inputValue(),'broken');
    assert.equal(await page.evaluate(()=>validationCommits),0);
    await cell(54,64).fill('64');
    assert.equal(await cell(54,64).getAttribute('aria-invalid'),null);
    await page.getByLabel('Axis minimum',{exact:true}).fill('oops');
    await page.getByRole('button',{name:'Apply',exact:true}).click();
    assert.equal(await page.evaluate(()=>document.activeElement.getAttribute('aria-label')),'Axis minimum');
    await page.getByLabel('Axis minimum',{exact:true}).fill('100');
    await page.getByLabel('Axis maximum',{exact:true}).fill('10');
    await page.getByRole('button',{name:'Apply',exact:true}).click();
    assert.equal(await page.evaluate(()=>document.activeElement.getAttribute('aria-label')),'Axis maximum');
    await page.getByLabel('Axis maximum',{exact:true}).fill('200');
    await page.getByRole('button',{name:'Apply',exact:true}).click();
    assert.equal(await page.evaluate(()=>validationCommits),1);
    const boundedHistory=await page.evaluate(()=>{
      const large={version:1,type:'column',categories:Array.from({length:2000},(_,i)=>'Row '+i),series:Array.from({length:64},(_,i)=>({name:'Series '+i,values:Array(2000).fill(1.123456789012345)})),options:{}};
      KeynopeCharts.edit(large,()=>{});
      const dialog=document.querySelector('dialog');
      for(let i=1;i<=24;i++){
        const input=dialog.querySelector('[aria-label="Chart title"]');
        input.dispatchEvent(new Event('focus'));input.value='Edit '+i;input.dispatchEvent(new Event('input'));
      }
      let undoCount=0;
      while(!dialog.querySelector('[data-chart-undo]').disabled&&undoCount<100){dialog.querySelector('[data-chart-undo]').click();undoCount++;}
      const oldest=dialog.querySelector('[aria-label="Chart title"]').value;
      let redoCount=0;
      while(!dialog.querySelector('[data-chart-redo]').disabled&&redoCount<100){dialog.querySelector('[data-chart-redo]').click();redoCount++;}
      const newest=dialog.querySelector('[aria-label="Chart title"]').value;
      dialog.querySelector('[data-chart-undo]').click();
      const input=dialog.querySelector('[aria-label="Chart title"]');input.dispatchEvent(new Event('focus'));input.value='Branched';input.dispatchEvent(new Event('input'));
      return {undoCount,redoCount,oldest,newest,redoDisabled:dialog.querySelector('[data-chart-redo]').disabled,sourceUntouched:large.title===undefined};
    });
    assert.ok(boundedHistory.undoCount>0&&boundedHistory.undoCount<24,JSON.stringify(boundedHistory));
    assert.equal(boundedHistory.redoCount,boundedHistory.undoCount);
    assert.equal(boundedHistory.newest,'Edit 24');
    assert.ok(boundedHistory.oldest.startsWith('Edit '));
    assert.ok(boundedHistory.redoDisabled&&boundedHistory.sourceUntouched);
    await page.getByRole('button',{name:'Cancel',exact:true}).click();
    assert.deepEqual(errors,[]);
    console.log('PASS: chart grid cross-page keyboard navigation, local history/focus, IME, commit ownership and focus restoration.');
    console.log('PASS: wide chart paging, bounded inputs, cross-series keyboard navigation, hidden data preservation, add/remove/paste page selection.');
    console.log('PASS: invalid hidden data is revealed and focused; invalid/range axis errors preserve draft and block commit until corrected.');
    console.log('PASS: maximum-size chart history evicts oldest states, retains Undo/Redo order and clears branched Redo without mutating input.');
  }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
