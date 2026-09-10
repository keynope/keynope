const fs=require('node:fs'),assert=require('node:assert/strict');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
(async()=>{
 const browser=await chromium.launch({headless:true,...(process.env.CHROME_PATH?{executablePath:process.env.CHROME_PATH}:{})});
 try{
  const page=await browser.newPage();
  await page.setContent('<style>aside{width:240px}button{display:block;width:240px;height:50px}.master-dragging{display:none}</style><aside id="slidesPanel"></aside>');
  const src=fs.readFileSync('main.go','utf8');
  const start=src.indexOf('  let draggedMasterIndex = -1;'),end=src.indexOf('  const masterModeButton =',start);
  const bindStart=src.indexOf('      {\n        button.dataset.masterIndex = String(index);'),bindEnd=src.indexOf('      slidesPanel.appendChild(button);',bindStart);
  assert(start>0&&bindStart>0);
  await page.addScriptTag({content:`const slidesPanel=document.getElementById('slidesPanel');let editorState={masterMode:false};window.actions=[];const editorAction=async a=>actions.push(a);function renderEditorPanels(){}\n${src.slice(start,end)}\nwindow.populate=master=>{finishMasterDrag();editorState.masterMode=master;slidesPanel.replaceChildren();for(let index=0;index<4;index++){const button=document.createElement('button');button.className='keynope-slide-item';button.textContent='Slide '+index;${src.slice(bindStart,bindEnd)}slidesPanel.appendChild(button);}};populate(false);`});
  async function move(source,toBottom){
   await page.evaluate(({source,toBottom})=>{
    const button=slidesPanel.querySelectorAll('button')[source],dataTransfer=new DataTransfer();
    button.dispatchEvent(new DragEvent('dragstart',{bubbles:true,dataTransfer,clientX:20,clientY:button.getBoundingClientRect().top+10}));
    const y=toBottom?1000:0;
    slidesPanel.dispatchEvent(new DragEvent('dragover',{bubbles:true,cancelable:true,dataTransfer,clientY:y}));
    if(!slidesPanel.querySelector('.keynope-master-drop-placeholder'))throw Error('Missing placeholder');
    slidesPanel.dispatchEvent(new DragEvent('drop',{bubbles:true,cancelable:true,dataTransfer,clientY:y}));
   },{source,toBottom});
  }
  assert.equal(await page.locator('button[draggable=true]').count(),4);
  await move(0,true);assert.deepEqual(await page.evaluate(()=>actions.pop()),{action:'reorder-slide',slide:0,value:3});
  await move(3,false);assert.deepEqual(await page.evaluate(()=>actions.pop()),{action:'reorder-slide',slide:3,value:0});
  await page.evaluate(()=>populate(true));
  assert.equal(await page.locator('button[draggable=true]').count(),3);
  await move(3,false);assert.deepEqual(await page.evaluate(()=>actions.pop()),{action:'reorder-master',slide:3,value:1});
  console.log('Slide drag UI: first-to-last, last-to-first, insertion placeholder and protected Base Master passed.');
 }finally{await browser.close();}
})().catch(error=>{console.error(error);process.exitCode=1;});
