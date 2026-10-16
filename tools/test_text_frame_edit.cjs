const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const output=fs.mkdtempSync('/tmp/keynope-text-frame-');
(async()=>{
 for(const engine of [process.env.KEYNOPE_BROWSER_ENGINE||'chromium']){
  const browser=await pw[engine].launch({headless:true});
  try{
   const page=await browser.newPage({viewport:{width:1440,height:1000}});
   await page.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
   await page.goto(process.argv[2]);await page.waitForFunction(()=>typeof modernPageView!=='undefined'&&modernPageView);
   const settled=()=>page.evaluate(async()=>{for(let n=0;n<20;n++){const v=modernPageView;await v.ready;if(v===modernPageView&&v.element.isConnected)return;}throw Error('Scene never settled');});
   await settled();
   const headingID=await page.evaluate(()=>deck.pages[pageIndex].scene.objects.find(o=>o.text?.role==='heading').id);
   const metrics=()=>page.evaluate(async id=>{const s=await fetch('/api/editor/state').then(r=>r.json()),o=deck.pages[pageIndex].scene.objects.find(o=>o.id===id),el=document.querySelector('#stage [data-object-id="'+id+'"]'),p=el.querySelector('.keynope-shaped-text'),r=document.createRange();r.selectNodeContents(p);const b=el.getBoundingClientRect(),t=r.getBoundingClientRect();return {bounds:o.bounds,text:o.text,frame:{x:b.x,y:b.y,width:b.width,height:b.height},ink:{x:t.x,y:t.y,width:t.width,height:t.height},height:p.offsetHeight,q:s.slides[s.current].elements.find(e=>e.id===id).query};},headingID);
   const before=await metrics();
   const select=async id=>{await page.evaluate(async id=>{const s=await fetch('/api/editor/state').then(r=>r.json()),index=s.slides[s.current].elements.findIndex(e=>e.id===id);await editorAction({action:'select-element',element:index});},id);};
   await select(headingID);await settled();
   await page.screenshot({path:path.join(output,engine+'-heading-initial.png')});
   const hit=page.locator('.keynope-canvas-element[data-element-id="'+headingID+'"]');
   const handle=await hit.locator('[data-corner="se"]').boundingBox();assert(handle);
   await page.mouse.move(handle.x+handle.width/2,handle.y+handle.height/2);await page.mouse.down();await page.mouse.move(handle.x+handle.width/2+25,handle.y+handle.height/2+5,{steps:4});await page.mouse.up();await settled();
   await page.waitForFunction(async id=>{const s=await fetch('/api/editor/state').then(r=>r.json());return new URLSearchParams(s.slides[s.current].elements.find(e=>e.id===id).query).has('width');},headingID);
   await settled();const resized=await metrics();
   assert(Math.abs(resized.ink.y-before.ink.y)<.75,'Resizing moved the heading baseline');
   assert.equal(resized.text.size,before.text.size,'Resizing changed font size');
   assert(resized.height===before.height,'Resizing changed line height');
   await hit.dblclick();const dialog=page.locator('dialog[data-keynope-scene-dialog="text"]');await dialog.waitFor();const edit=dialog.getByRole('textbox',{name:'Edit slide text',exact:true});await edit.fill('Your presentation\nFoo');await dialog.getByRole('button',{name:'Apply text',exact:true}).click();await dialog.waitFor({state:'detached'});await settled();
   const edited=await metrics();assert(edited.bounds.height>resized.bounds.height,'Extra line did not grow the frame');
   assert(edited.bounds.height>=edited.height-.1,'Edited text still overflows its frame');
   assert(Math.abs(edited.bounds.y-resized.bounds.y)<.01,'New line moved the top edge');
   assert.equal(edited.bounds.width,resized.bounds.width,'New line changed wrapping width');
   assert.equal(edited.text.size,before.text.size,'Editing changed font size');
   await page.screenshot({path:path.join(output,engine+'-heading-grown.png')});
   await select('flags');await settled();
   const flags=await page.evaluate(()=>{const n=document.querySelector('#stage [data-object-id="flags"]'),r=n.getBoundingClientRect();return {frame:{x:r.x,y:r.y,width:r.width,height:r.height},emojis:[...n.querySelectorAll('.keynope-shaped-emoji')].map(e=>{const b=e.getBoundingClientRect();return {x:b.x,y:b.y,width:b.width,height:b.height};})};});
   assert.equal(flags.emojis.length,4);for(const e of flags.emojis){assert(e.y>=flags.frame.y-.75,'Emoji protrudes above its selection');assert(e.y+e.height<=flags.frame.y+flags.frame.height+.75,'Emoji protrudes below its selection');}
   await page.screenshot({path:path.join(output,engine+'.png')});
  }finally{await browser.close();}
 }
 console.log('PASS: heading resize preserves baseline/size, extra lines grow frame, emoji fit inside selection. '+output);
})().catch(e=>{console.error(e);process.exitCode=1;});
