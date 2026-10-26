// Exercise the real text editor and save preparation, not just the Go codecs.
const assert=require('node:assert/strict'),fs=require('node:fs');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright');
(async()=>{
 const engine=process.env.KEYNOPE_BROWSER_ENGINE||'chromium';
 const executablePath=engine==='chromium'?process.env.CHROME_PATH:process.env.WEBKIT_PATH;
 const browser=await pw[engine].launch({headless:true,...(executablePath?{executablePath}:{})});
 try{
  const page=await browser.newPage({viewport:{width:1500,height:1000}}),errors=[];
  page.on('pageerror',e=>errors.push(e.message));
  await page.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
  await page.goto(process.argv[2]);
  const hit=page.locator('.keynope-canvas-element[data-element-id="fractional-title"]');
  await hit.click();await page.getByRole('tab',{name:'Text',exact:true}).click();
  const inspectorWidth=page.getByRole('spinbutton',{name:'TrueType width percent',exact:true});
  assert.equal(await inspectorWidth.inputValue(),'98.625');assert(await inspectorWidth.evaluate(n=>n.checkValidity()),'Inspector rejects inherited fractional width');
  await hit.dblclick();
  const dialog=page.locator('dialog[data-keynope-scene-dialog="text"]');await dialog.waitFor();
  const fields=[['Font size','96.25',1,1024],['Font width percent','98.625',1,200],['Line spacing','1.02',.5,4],['Paragraph before','0.02',0,4],['Paragraph after','0.07',0,4]];
  for(const [label,value,min,max]of fields){
   const input=dialog.getByRole('spinbutton',{name:label,exact:true});
   assert.equal(await input.inputValue(),value,label+' must preserve its inherited fractional value');
   assert(await input.evaluate(n=>n.checkValidity()),label+' rejects a valid fraction');
   for(const invalid of [min-.01,max+.01]){
    await input.fill(String(invalid));assert.equal(await input.evaluate(n=>n.checkValidity()),false,label+' lost its range validation');
   }
   await input.fill(value);
  }
  // Save while the caret is still in the text, as with native Command-S.
  const text=dialog.getByRole('textbox',{name:'Edit slide text',exact:true});
  await text.fill('Agentic Delivery Platformd');
  const spacing=dialog.getByRole('spinbutton',{name:'Line spacing',exact:true});
  await spacing.fill('0.49');
  const blocked=await page.evaluate(()=>window.keynopePrepareDocumentSave().then(()=>'',e=>e.message));
  assert.match(blocked,/Correct the text settings before saving/,'Out-of-range spacing should still block save');
  assert.equal(await text.innerText(),'Agentic Delivery Platformd','Rejected save discarded the draft');
  await spacing.fill('1.02');
  await page.evaluate(()=>window.keynopePrepareDocumentSave());
  await dialog.waitFor({state:'detached'});
  const document=await page.evaluate(async()=>{
   const r=await fetch('/api/editor/document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Saved.md'})});
   if(!r.ok)throw Error(await r.text());return r.json();
  });
  fs.writeFileSync(process.env.KEYNOPE_FRACTIONAL_SAVED_PATH,document.content);
  // Theme editing must also accept the same metrics instead of invalidating
  // untouched inherited defaults on the form's submit validation.
  await page.getByRole('button',{name:'Slide',exact:true}).click();
  await page.getByRole('button',{name:'Edit theme…',exact:true}).click();
  const theme=page.getByRole('dialog',{name:'Edit deck theme',exact:true});await theme.waitFor();
  for(const [label,value]of [['Heading theme size','96.25'],['Heading theme width','98.625'],['Heading theme line height','1.02']]){
   const input=theme.getByRole('spinbutton',{name:label,exact:true});assert.equal(await input.inputValue(),value);assert(await input.evaluate(n=>n.checkValidity()),label+' rejects a valid fraction');
  }
  assert(await theme.locator('form').evaluate(n=>n.checkValidity()),'Unchanged theme cannot be applied');
  await theme.getByRole('button',{name:'Cancel',exact:true}).click();
  assert.deepEqual(errors,[]);
  console.log('PASS '+engine+': fractional inherited typography, range checks, in-progress text save and valid theme form');
 }finally{await browser.close();}
})().catch(error=>{console.error(error);process.exitCode=1;});
