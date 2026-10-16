// Independent DOM measurements against alignment semantics, not pixel equality
// between two instances of the same renderer. Uses owned exported fixtures.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright'),engine=process.env.TEST_BROWSER||'chromium';
const root=process.env.KEYNOPE_ALIGNMENT_OUTPUT_PROOF||'/tmp/keynope-alignment-output-proof';
const cases=JSON.parse(fs.readFileSync(path.join(root,'cases.json')));assert.equal(cases.length,15);
(async()=>{
 const browser=await pw[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
 try{
  const context=await browser.newContext({viewport:{width:1920,height:1080},deviceScaleFactor:1,reducedMotion:'reduce'});
  await context.route(/https?:\/\/.*/,route=>route.abort());const page=await context.newPage(),errors=[],records=[];page.on('pageerror',e=>errors.push(e.message));
  for(const item of cases){
   await page.goto('file://'+path.join(root,item.file));await page.evaluate(()=>keynopePrepareSnapshot(0));
   const measured=await page.evaluate(()=>{
    const records=[];
    for(const object of deck.pages[0].scene.objects){
     const owner=modernPageView.element.querySelector('[data-object-id="'+object.id+'"]');
     const shaped=owner?.querySelector('.keynope-shaped-text'),row=shaped?.closest('.keynope-scene-paragraph'),box=row?.parentElement;
     if(!shaped||!row||!box)throw Error('Missing text geometry for '+object.id);
     // The first explicit line is common to all kinds, including the first
     // list paragraph. Measure the actual shaped glyph ranges independently
     // of authored alignment values and resolved scene flags.
     const walker=document.createTreeWalker(shaped,NodeFilter.SHOW_TEXT),nodes=[];let node;
     while(node=walker.nextNode())nodes.push(node);
     let count=0,start,end;
     for(const node of nodes){
      if(!start&&node.length)start={node,offset:0};
      if(count+node.length>=16){end={node,offset:16-count};break;}count+=node.length;
     }
     if(!start||!end)throw Error('Missing first text line for '+object.id);
     const range=document.createRange();range.setStart(start.node,start.offset);range.setEnd(end.node,end.offset);
     const parts=[];let offset=0;
     for(const node of nodes){for(let i=0;i<node.length&&offset<16;i++,offset++){
      const glyph=document.createRange();glyph.setStart(node,i);glyph.setEnd(node,i+1);const rect=glyph.getBoundingClientRect();
      if(rect.width>0)parts.push({text:node.textContent[i],left:rect.left,right:rect.right,top:rect.top});
     }if(offset>=16)break;}
     const first=parts.find(p=>p.text.trim());if(!first)throw Error('No first painted line');
     const line=parts.filter(p=>Math.abs(p.top-first.top)<1),inkParts=line.filter(p=>p.text.trim());
     const ink={left:Math.min(...inkParts.map(p=>p.left)),right:Math.max(...inkParts.map(p=>p.right))};ink.width=ink.right-ink.left;
     const hasGap=line.some(p=>p.text===' '&&p.left>ink.left&&p.right<ink.right);
     const surface=shaped.getBoundingClientRect(),bounds=box.getBoundingClientRect();
     const rows=[...box.children].filter(n=>n.classList.contains('keynope-scene-paragraph')).map(n=>n.getBoundingClientRect());
     const top=Math.min(...rows.map(r=>r.top)),bottom=Math.max(...rows.map(r=>r.bottom));
     const [vertical,horizontal]=object.id.split('-');
     const horizontalError=horizontal==='center'?Math.abs((ink.left+ink.right-surface.left-surface.right)/2):horizontal==='right'?Math.abs(ink.right-surface.right):horizontal==='justify'&&hasGap?Math.abs(ink.right-(surface.right-.5)):Math.abs(ink.left-surface.left);
     const verticalError=vertical==='middle'?Math.abs((top+bottom-bounds.top-bounds.bottom)/2):vertical==='bottom'?Math.abs(bottom-bounds.bottom):Math.abs(top-bounds.top);
     records.push({id:object.id,horizontalError,verticalError,inkWidth:ink.width,surfaceWidth:surface.width,text:range.toString(),firstLine:line.map(p=>p.text).join(''),hasGap});
    }
    return records;
   });
   assert.equal(measured.length,12,item.file);
   for(const result of measured){
    assert.equal(result.text,'Alignment sample',item.file+'/'+result.id);
    assert(result.inkWidth>20&&result.surfaceWidth>300,'Vacuous geometry measurement');
    assert(result.horizontalError<1,item.file+'/'+result.id+' horizontal '+result.horizontalError);
    assert(result.verticalError<1,item.file+'/'+result.id+' vertical '+result.verticalError);
   }
   records.push({file:item.file,measured});
  }
  assert.deepEqual(errors,[]);assert.equal(records.reduce((n,r)=>n+r.measured.length,0),180);
  fs.writeFileSync(path.join(root,engine+'-geometry.json'),JSON.stringify({engine,records,errors},null,2));
  console.log('PASS '+engine+': 180 shaped text ranges and paragraph boxes satisfy independently measured horizontal/vertical alignment.');
 }finally{await browser.close();}
})().catch(error=>{console.error(error);process.exitCode=1;});
