const fs=require('node:fs'),assert=require('node:assert/strict');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
(async()=>{
 for(const engine of ['chromium','webkit']){
  const browser=await playwright[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
  try{
   const page=await browser.newPage({viewport:{width:1100,height:1100}});
   await page.setContent('<style>body{background:#161a1d;color:#fff;font:14px monospace}.sample{display:inline-block;margin:8px}canvas{display:block;border:1px solid #555}</style>');
   await page.addScriptTag({content:'const keynopeTTFFontData='+JSON.stringify(fs.readFileSync('assets/keynope-c64.ttf.base64','utf8').trim())+';\n'+fs.readFileSync('web/truetype.js','utf8')});
   await page.evaluate(()=>KeynopeTrueType.ready);
   const results=await page.evaluate(()=>{
    const samples=[];
    for(const mode of ['','blocks','braille','ascii','dense','']){
     const box=document.createElement('div');box.className='sample';box.append(mode||'Default');
     const canvas=document.createElement('canvas');canvas.width=510;canvas.height=200;box.append(canvas);document.body.append(box);
     const query=new URLSearchParams({'render':'truetype','glyph':mode,'ttf-width':'100','outline':'dark','gradient-start':'#ff0055','gradient-end':'#ffffaa','shadow':'soft','shadow-color':'#ffffff'});
     const line={element:1,col:20,row:20,parts:[{color:'#fff'}],trueType:{text:'Keynope\nText 123',kind:'text',size:73,width:470,height:170,query:query.toString()}};
     const m=KeynopeTrueType.metrics(line,1920,1080);KeynopeTrueType.draw(canvas.getContext('2d'),line,1,1,1920,1080);
     const pixels=canvas.getContext('2d').getImageData(0,0,510,200).data;let hash=2166136261,ink=0;
     for(let i=0;i<pixels.length;i++){hash=Math.imul(hash^pixels[i],16777619);if(i%4===3&&pixels[i]>20)ink++;}
     samples.push({mode,hash,ink,positions:m.rows.map(r=>r.positions),height:m.lineHeight});
    }
    const styled={trueType:{text:'A*B* [color=#ff5500]C[/color]',kind:'text',size:73,width:470,height:170,query:'render=truetype'}};
    const m=KeynopeTrueType.metrics(styled,1920,1080);
    const literals=['*text*','**text**','***text***','**** some text ****','*****','2 * 3 * 4'].map(text=>{
     const line={trueType:{text,kind:'text',size:30,width:1000,height:100,query:'render=truetype'}};
     const metrics=KeynopeTrueType.metrics(line,1920,1080);
     return {text,width:metrics.rows[0].inkWidth,advance:metrics.advance,hidden:metrics.rows[0].styles.some(s=>s.hidden),bold:metrics.rows[0].styles.some(s=>s.bold)};
    });
    return {samples,literals,styledWidth:m.rows[0].inkWidth,advance:m.advance,hidden:m.rows[0].styles.filter(s=>s.hidden).length};
   });
   for(const s of results.samples){assert(s.ink>500,'visible ink '+s.mode);assert.deepEqual(s.positions,results.samples[0].positions,'unchanged layout '+s.mode);assert.equal(s.height,87.6);}
   assert.equal(results.samples[0].hash,results.samples[5].hash,'Default is a lossless return to the font');
   assert.equal(new Set(results.samples.slice(0,5).map(s=>s.hash)).size,5,'all five render treatments differ');
   assert(Math.abs(results.styledWidth-results.advance*6)<.001,'asterisks occupy space; colour tags do not');assert(results.hidden>2);
   for(const literal of results.literals){assert(!literal.hidden&&!literal.bold,'typed stars stay literal: '+literal.text);assert(Math.abs(literal.width-literal.advance*literal.text.length)<.001,'caret includes every star');}
   fs.mkdirSync('output/standard-text',{recursive:true});await page.screenshot({path:'output/standard-text/'+engine+'.png',fullPage:true});
   console.log('PASS '+engine+': precise font size, five styles, unchanged caret/layout, lossless return to Default, inline formatting');
  }finally{await browser.close();}
 }
})().catch(e=>{console.error(e);process.exitCode=1;});
