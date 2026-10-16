const assert=require('node:assert/strict'),path=require('node:path');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
(async()=>{
  const browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
  try{
    const page=await browser.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
    await page.setContent('<button id="origin">Origin</button>');
    await page.addScriptTag({path:path.resolve('web/tables.js')});
    await page.addScriptTag({path:path.resolve('web/charts.js')});
    await page.evaluate(()=>{
      KeynopeCharts.edit({version:1,type:'column',categories:['One'],series:[{name:'A',values:[3]},{name:'B',values:[2]}],options:{}},data=>window.stackedResult=data);
    });
    await page.getByLabel('Stack series',{exact:true}).check();
    await page.getByLabel('Chart type',{exact:true}).selectOption('area');
    assert.equal(await page.getByLabel('Stack series',{exact:true}).count(),0);
    await page.getByRole('button',{name:'Undo',exact:true}).click();
    assert.equal(await page.getByLabel('Chart type',{exact:true}).inputValue(),'column');
    assert.equal(await page.getByLabel('Stack series',{exact:true}).isChecked(),true);
    await page.getByRole('button',{name:'Redo',exact:true}).click();
    await page.getByRole('button',{name:'Apply',exact:true}).click();
    await page.waitForFunction(()=>window.stackedResult?.type==='area'&&!window.stackedResult.options.stacked);
    await page.evaluate(()=>KeynopeCharts.edit({version:1,type:'column',categories:['One'],series:[{name:'A',values:[3]},{name:'B',values:[2]}],options:{stacked:true}},data=>window.stackedResult=data));
    await page.getByRole('button',{name:'Apply',exact:true}).click();
    await page.waitForFunction(()=>window.stackedResult?.options.stacked===true);
    for(const kind of ['insert','edit']){
      await page.locator('#origin').focus();
      await page.evaluate(kind=>{
        window.calls=0;window.source={version:1,rows:[{height:1}],columns:[{width:1}],cells:[[{text:'Original'}]],style:{}};
        const commit=(...args)=>{calls++;window.args=args;return new Promise((resolve,reject)=>{window.accept=resolve;window.fail=reject;});};
        if(kind==='insert')KeynopeTables.insert(commit);else KeynopeTables.edit(source,commit);
      },kind);
      const label=kind==='insert'?'Insert':'Apply';
      await page.getByRole('button',{name:label,exact:true}).click();
      await page.waitForFunction(()=>calls===1);
      assert.equal(await page.locator('dialog').getAttribute('aria-busy'),'true');
      assert.equal(await page.locator('dialog').evaluate(d=>d.inert),true);
      await page.evaluate(label=>{
        const d=document.querySelector('dialog');
        [...d.querySelectorAll('button')].find(b=>b.textContent===label).click();
        [...d.querySelectorAll('button')].find(b=>b.textContent==='Cancel').click();
        d.dispatchEvent(new Event('cancel',{cancelable:true}));
      },label);
      assert.equal(await page.evaluate(()=>calls),1);
      assert.equal(await page.locator('dialog').count(),1);
      await page.evaluate(()=>fail(Error('Retryable failure')));
      await page.waitForFunction(()=>!document.querySelector('dialog').inert);
      assert.equal(await page.getByRole('alert').textContent(),'Retryable failure');
      if(kind==='edit'){
        await page.evaluate(()=>{args[0].cells[0][0].text='External mutation';});
        assert.equal(await page.getByRole('textbox',{name:'Cell 1, 1',exact:true}).inputValue(),'Original');
        assert.equal(await page.evaluate(()=>source.cells[0][0].text),'Original');
      }
      await page.getByRole('button',{name:label,exact:true}).click();
      await page.waitForFunction(()=>calls===2);await page.evaluate(()=>accept());
      await page.waitForFunction(()=>!document.querySelector('dialog'));
      assert.equal(await page.evaluate(()=>document.activeElement.id),'origin');
    }
    await page.evaluate(()=>{
      window.dirtyEvents=[];window.saved=[];
      window.openDraft=()=>KeynopeTables.edit(source,data=>{saved.push(data);},{onDirty:()=>dirtyEvents.push(KeynopeTables.isDirty())});
      openDraft();
    });
    assert.equal(await page.evaluate(()=>KeynopeTables.isDirty()),false);
    await page.getByRole('textbox',{name:'Cell 1, 1',exact:true}).fill('Unsaved');
    assert.equal(await page.evaluate(()=>KeynopeTables.isDirty()),true);
    assert.equal(await page.evaluate(()=>{
      const original=JSON.stringify;let calls=0;JSON.stringify=(...args)=>{calls++;return original(...args);};
      try{for(let i=0;i<1000;i++)KeynopeTables.isDirty();return calls;}finally{JSON.stringify=original;}
    }),0,'unchanged dirty polling must not serialize the table repeatedly');
    await page.getByRole('button',{name:'Undo',exact:true}).click();
    assert.equal(await page.evaluate(()=>KeynopeTables.isDirty()),false);
    await page.getByRole('button',{name:'Redo',exact:true}).click();
    await page.evaluate(()=>KeynopeTables.commitPending());
    assert.deepEqual(await page.evaluate(()=>saved.map(d=>d.cells[0][0].text)),['Unsaved']);
    assert.equal(await page.evaluate(()=>KeynopeTables.isDirty()),false);
    assert.equal(await page.locator('dialog').count(),0);
    await page.evaluate(()=>{openDraft();return KeynopeTables.commitPending();});
    assert.equal(await page.evaluate(()=>saved.length),1,'clean dialog must not create a deck edit');
    await page.evaluate(()=>openDraft());
    await page.getByRole('textbox',{name:'Cell 1, 1',exact:true}).fill('Discard');
    await page.getByRole('button',{name:'Cancel',exact:true}).click();
    assert.equal(await page.evaluate(()=>KeynopeTables.isDirty()),false);
    assert.equal(await page.evaluate(()=>dirtyEvents.at(-1)),false);
    // Save must join an already accepted operation, even for a clean chart.
    // A rejected Apply must reject Save as well, never silently save old data.
    for(const kind of ['insert','chart'])for(const reject of [false,true]){
      await page.evaluate(kind=>{
        window.calls=0;window.saveState='not started';
        const commit=()=>{calls++;return new Promise((resolve,reject)=>{window.accept=resolve;window.fail=reject;});};
        if(kind==='insert')KeynopeTables.insert(commit);
        else KeynopeCharts.edit({version:1,type:'column',categories:['First'],series:[{name:'Series',values:[1]}],options:{}},commit);
      },kind);
      await page.getByRole('button',{name:kind==='insert'?'Insert':'Apply',exact:true}).click();
      await page.waitForFunction(()=>calls===1);
      await page.evaluate(kind=>{
        window.saveState='waiting';
        (kind==='insert'?KeynopeTables:KeynopeCharts).commitPending().then(()=>saveState='saved',()=>saveState='failed');
      },kind);
      assert.equal(await page.evaluate(()=>saveState),'waiting');
      assert.equal(await page.evaluate(()=>calls),1);
      await page.evaluate(reject=>reject?fail(Error('Commit failed')):accept(),reject);
      await page.waitForFunction(expected=>saveState===expected,reject?'failed':'saved');
      if(reject){
        assert.equal(await page.locator('dialog').count(),1);
        await page.getByRole('button',{name:'Cancel',exact:true}).click();
      }else assert.equal(await page.locator('dialog').count(),0);
    }
    for(const action of ['escape','save','cancel']){
      await page.evaluate(()=>{
        window.resized=null;
        KeynopeTables.edit({version:1,rows:[{height:1}],columns:[{width:1},{width:1}],cells:[[{text:'A'},{text:'B'}]],style:{}},data=>{window.resized=data;});
        const handle=document.querySelector('[data-resize-axis="columns"]');
        // Synthetic pointer events retain deterministic geometry without OS input.
        handle.setPointerCapture=()=>{};handle.hasPointerCapture=()=>false;
        window.oldHandle=handle;
        handle.dispatchEvent(new PointerEvent('pointerdown',{bubbles:true,button:0,pointerId:1,clientX:100}));
        handle.dispatchEvent(new PointerEvent('pointermove',{bubbles:true,pointerId:1,clientX:160}));
      });
      assert.equal(await page.evaluate(()=>KeynopeTables.isDirty()),true);
      if(action==='escape'){
        await page.keyboard.press('Escape');
        assert.equal(await page.locator('dialog').count(),1);
        assert.equal(await page.evaluate(()=>KeynopeTables.isDirty()),false);
        await page.getByRole('button',{name:'Cancel',exact:true}).click();
      }else if(action==='save'){
        await page.evaluate(()=>KeynopeTables.commitPending());
        assert.ok(await page.evaluate(()=>resized.columns[0].width>1));
      }else await page.getByRole('button',{name:'Cancel',exact:true}).click();
      await page.evaluate(()=>{
        oldHandle.dispatchEvent(new PointerEvent('pointermove',{pointerId:1,clientX:220}));
        oldHandle.dispatchEvent(new PointerEvent('pointerup',{pointerId:1}));
        window.dispatchEvent(new Event('blur'));
      });
      assert.equal(await page.locator('dialog').count(),0);
      assert.equal(await page.evaluate(()=>KeynopeTables.isDirty()),false);
    }
    await page.evaluate(()=>{
      window.largeSource={version:1,rows:[{height:1}],columns:[{width:1}],cells:[[{text:'0'}]],style:{},extension:'x'.repeat(1024*1024)};
      KeynopeTables.edit(largeSource,()=>{});
      for(let i=1;i<=24;i++){
        const input=document.querySelector('textarea[aria-label="Cell 1, 1"]');input.value=String(i);input.dispatchEvent(new Event('input'));
      }
    });
    const count=await page.evaluate(()=>{
      const value=()=>document.querySelector('textarea[aria-label="Cell 1, 1"]').value;
      const press=name=>[...document.querySelectorAll('button')].find(b=>b.textContent===name).click();
      let count=0;
      for(let i=0;i<30;i++){const before=value();press('Undo');if(before===value())break;count++;}
      window.oldestValue=value();
      // Same-value input is not a mutation and must preserve the Redo branch.
      document.querySelector('textarea[aria-label="Cell 1, 1"]').dispatchEvent(new Event('input'));
      for(let i=0;i<count;i++)press('Redo');
      if(value()!=='24')throw Error('Redo order lost');
      press('Undo');
      const input=document.querySelector('textarea[aria-label="Cell 1, 1"]');input.value='branch';input.dispatchEvent(new Event('input'));press('Redo');
      if(value()!=='branch')throw Error('Branch retained obsolete redo');
      if(largeSource.cells[0][0].text!=='0')throw Error('Source mutated');
      return count;
    });
    assert.ok(count>0&&count<24,'large table history must evict oldest snapshots');
    await page.getByRole('button',{name:'Cancel',exact:true}).click();
    assert.deepEqual(errors,[]);
    console.log('PASS: table/chart pending saves, failure recovery, dirty history/cache, resize cancellation/save/cleanup, owned payloads and focus restoration.');
  }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
