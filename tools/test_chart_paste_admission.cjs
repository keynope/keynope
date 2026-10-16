// Production dialog tests, isolated from the user's app and external services.
const assert=require('node:assert/strict'),path=require('node:path');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright'),engine=process.env.TEST_BROWSER||'webkit';
(async()=>{
 const browser=await pw[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
 try{
  const page=await browser.newPage({viewport:{width:1400,height:1000}}),errors=[];page.on('pageerror',e=>errors.push(e.message));await page.route('https://**',r=>r.abort());
  await page.setContent('<button id="origin">Edit</button>');await page.addScriptTag({path:path.resolve('web/charts.js')});
  async function open(type,title='Workshop'){
   await page.evaluate(({type,title})=>{window.chartSource={version:1,type,title,categories:['First'],series:[{name:'One',values:[1]}],options:{}};window.chartAccepted=null;window.chartCommits=0;KeynopeCharts.edit(chartSource,data=>{chartAccepted=data;chartCommits++;});},{type,title});
  }
  const dialog=page.getByRole('dialog',{name:'Edit chart',exact:true}),category=page.getByRole('textbox',{name:'Category 1',exact:true});
  async function paste(text){await dialog.getByRole('textbox',{name:'Paste chart data',exact:true}).fill(text);await dialog.getByRole('button',{name:'Paste data',exact:true}).click();}
  for(const type of ['column','bar','line','area','pie','doughnut']){
   await open(type);await paste('Category\tOne\nNew\t7');assert.equal(await category.inputValue(),'New');await dialog.getByRole('button',{name:'Undo',exact:true}).click();assert.equal(await category.inputValue(),'First');
   await dialog.getByRole('textbox',{name:'Value 1, 1',exact:true}).fill('1.000');assert(!(await dialog.getByRole('button',{name:'Redo',exact:true}).isDisabled()),'Equivalent numeric input destroyed redo');await category.fill('First');assert(!(await dialog.getByRole('button',{name:'Redo',exact:true}).isDisabled()),'Unchanged text input destroyed redo');
   await dialog.getByRole('combobox',{name:'Chart type',exact:true}).selectOption(type);assert(!(await dialog.getByRole('button',{name:'Redo',exact:true}).isDisabled()),'Unchanged chart type destroyed redo');
   await paste('Category\tOne\nFirst\t1');assert(!(await dialog.getByRole('button',{name:'Redo',exact:true}).isDisabled()),'Identical pasted data destroyed redo');assert.equal(await page.evaluate(()=>KeynopeCharts.isDirty()),false);
   const bad=['Category\tOne\nBroken\tnot-a-number','Category\tOne\nIncomplete'];if(type==='column')bad.push('Category\tOne\n'+'😀'.repeat(1<<18)+'\t1');
   for(const text of bad){await paste(text);await dialog.getByRole('alert').filter({hasText:/finite number|rectangular|1 MiB/}).waitFor();assert.equal(await category.inputValue(),'First');assert(!(await dialog.getByRole('button',{name:'Redo',exact:true}).isDisabled()));assert.equal(await page.evaluate(()=>chartCommits),0);}
   await dialog.getByRole('button',{name:'Redo',exact:true}).click();assert.equal(await category.inputValue(),'New');await dialog.getByRole('button',{name:'Apply',exact:true}).click();await dialog.waitFor({state:'detached'});assert.equal(await page.evaluate(()=>chartCommits),1);assert.equal(await page.evaluate(()=>chartAccepted.series[0].values[0]),7);assert.equal(await page.evaluate(()=>chartSource.categories[0]),'First');
  }
  await open('column','x'.repeat((1<<20)-10));await paste('Category\tOne\nLong category label\t5');await dialog.getByRole('alert').filter({hasText:'1 MiB'}).waitFor();assert.equal(await category.inputValue(),'First');assert(await dialog.getByRole('button',{name:'Undo',exact:true}).isDisabled(),'Rejected combined-budget paste created history');
  await dialog.getByRole('textbox',{name:'Chart title',exact:true}).fill('Short');await paste('Category\tOne\nLong category label\t5');assert.equal(await category.inputValue(),'Long category label');await dialog.getByRole('button',{name:'Undo',exact:true}).click();assert.equal(await category.inputValue(),'First');await dialog.getByRole('button',{name:'Redo',exact:true}).click();await dialog.getByRole('button',{name:'Apply',exact:true}).click();await dialog.waitFor({state:'detached'});assert.equal(await page.evaluate(()=>chartAccepted.title),'Short');
  await open('column');await dialog.getByRole('textbox',{name:'Chart title',exact:true}).fill('😀'.repeat(1<<18));await dialog.getByRole('button',{name:'Apply',exact:true}).click();await dialog.getByRole('alert').filter({hasText:'1 MiB'}).waitFor();assert.equal(await page.evaluate(()=>chartCommits),0);await dialog.getByRole('button',{name:'Cancel',exact:true}).click();
  assert.deepEqual(errors,[]);console.log('PASS '+engine+': six chart types retain redo for no-op/rejected paste, UTF-8 and combined-label budgets, source isolation, local history, typed-budget validation and one Apply.');
 }finally{await browser.close();}
})().catch(error=>{console.error(error);process.exitCode=1;});
