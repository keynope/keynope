const fs=require('node:fs'),assert=require('node:assert/strict');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const main=fs.readFileSync('main.go','utf8');
const css=main.slice(main.indexOf('.keynope-tabs-overlay {'),main.indexOf('.keynope-tabs-dialog button:disabled {'))+'.keynope-tabs-dialog button:disabled { opacity:.4; }';
const start=main.indexOf('  function openDeckTabsDialog(launcher) {');
const script=main.slice(start,main.indexOf('\n  exportButton.classList.add',start));
(async()=>{
 for(const engine of ['chromium','webkit']){
  const browser=await playwright[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
  try{
   const page=await browser.newPage();
   await page.setContent('<style>'+css+'</style><div id="stage" tabindex="0"></div>');
   await page.evaluate(()=>{
    window.editorState={hasActivities:true,tabs:[{id:'long',name:'N'.repeat(80),url:'https://example.com/'+ 'long-path/'.repeat(60)}],slides:[{}]};
    window.closeCanvasLinkDialog=()=>{};window.activeCanvasLinkDialog=null;window.keynopeHandleDialogKey=()=>false;
    window.slideTitle=()=> 'Very long slide title '.repeat(40);
    window.canvasTool=(label,icon,action)=>{const b=document.createElement('button');b.type='button';b.textContent=label;b.onclick=action;return b;};
    window.settingsTitle=document.createElement('summary');window.stage=document.querySelector('#stage');
   });
   await page.addScriptTag({content:script+'\nopenDeckTabsDialog();'});
   for(const width of [1440,800,390,320]){
    await page.setViewportSize({width,height:850});
    for(const mode of ['url','page']){
     await page.getByLabel('Tab destination',{exact:true}).selectOption(mode);
     const bounds=await page.locator('.keynope-tabs-dialog').evaluate(dialog=>{
      const r=dialog.getBoundingClientRect();return {fits:dialog.scrollWidth<=dialog.clientWidth+1,scroll:dialog.scrollWidth,client:dialog.clientWidth,controls:[...dialog.querySelectorAll('input,select,button')].filter(e=>!e.hidden).every(e=>{const b=e.getBoundingClientRect();return b.left>=r.left&&b.right<=r.right;}),width:r.width};
     });
     assert(bounds.fits&&bounds.controls,`${engine} ${width}px ${mode}: ${JSON.stringify(bounds)}`);
     assert(bounds.width<=width,'overlay exceeds viewport');
    }
   }
   fs.mkdirSync('output/tabs-audit',{recursive:true});await page.screenshot({path:`output/tabs-audit/dialog-${engine}.png`});
   console.log('PASS: '+engine+' tab fields and selector fit at desktop/mobile widths, even with long values.');
  }finally{await browser.close();}
 }
})().catch(e=>{console.error(e);process.exitCode=1});
