const fs=require('node:fs'),assert=require('node:assert/strict');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
(async()=>{
 for(const engine of ['chromium','webkit']){
  const browser=await playwright[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
  try{
   const page=await browser.newPage({viewport:{width:1000,height:720}});
   await page.setContent('<style>body{background:#171717;color:white;font:16px monospace}canvas{display:block;border:1px solid #555;margin:8px}</style><h2>TrueType horizontal width</h2>');
   await page.addScriptTag({content:'const keynopeTTFFontData='+JSON.stringify(fs.readFileSync('assets/keynope-c64.ttf.base64','utf8').trim())+';\n'+fs.readFileSync('web/truetype.js','utf8')});
   await page.evaluate(()=>KeynopeTrueType.ready);
   const defaults=await page.evaluate(()=>({width:KeynopeTrueType.widthPercent({}),explicit:KeynopeTrueType.widthPercent({query:'ttf-width=100'}),bounds:KeynopeTrueType.initialBounds('ABCD',100,1920,1080)}));
   assert.equal(defaults.width,100);assert.equal(defaults.explicit,100);assert.equal(defaults.bounds.width,134);
   const aligned=await page.evaluate(()=>{
    const line={trueType:{text:'AB\nA',kind:'text',size:60,width:500,height:300,query:'ttf-width=100&text-align=right&text-valign=bottom&align=center'}};
    const m=KeynopeTrueType.metrics(line,1920,1080);
    return {end:m.rows.map(r=>r.inkWidth),starts:m.rows.map(r=>r.positions[0]),offsetY:m.offsetY};
   });
   assert(aligned.end.every(x=>Math.abs(x-500)<.0001));assert.equal(aligned.offsetY,156);assert(aligned.starts[1]>aligned.starts[0]);
   const result=await page.evaluate(()=>{
    const make=(percent,text='C64 TEXT',kind='text',extra='')=>({col:20,row:20,element:1,parts:[{color:'#ffffff'}],trueType:{text,kind,size:60,width:500,height:160,query:'ttf-width='+percent+'&'+extra}});
    function paint(line,label){
     const caption=document.createElement('div');caption.textContent=label;document.body.append(caption);
     const c=document.createElement('canvas');c.width=560;c.height=210;document.body.append(c);
     KeynopeTrueType.draw(c.getContext('2d'),line,1,1,1920,1080);
     const p=c.getContext('2d').getImageData(0,0,c.width,c.height).data;
     let left=c.width,right=-1,top=c.height,bottom=-1;
     for(let y=0;y<c.height;y++)for(let x=0;x<c.width;x++)if(p[(y*c.width+x)*4+3]>10){left=Math.min(left,x);right=Math.max(right,x);top=Math.min(top,y);bottom=Math.max(bottom,y);}
     return {left,right,top,bottom,width:right-left+1,height:bottom-top+1};
    }
    const normal=paint(make(100),'100%'),narrow=paint(make(50),'50%'),wide=paint(make(150,'C64'),'150%');
    const effect=paint(make(50,'C64 TEXT','text','outline=1&gradient-start=%23ff0055&gradient-end=%23ffffaa&shadow=soft&shadow-color=%23ffffff'),'50% + gradient, outline and shadow');
    const a=KeynopeTrueType.metrics(make(100),1920,1080),b=KeynopeTrueType.metrics(make(50),1920,1080);
    const wrap=percent=>KeynopeTrueType.metrics(make(percent,'ONE TWO THREE FOUR FIVE SIX SEVEN EIGHT NINE'),1920,1080).rows.length;
    const bullet=KeynopeTrueType.metrics(make(50,'ONE TWO THREE\nFOUR FIVE SIX','bullet'),1920,1080);
    const code=KeynopeTrueType.metrics(make(50,'code\nline 2','code'),1920,1080);
    const justified=KeynopeTrueType.metrics(make(50,'ONE TWO THREE\nFOUR FIVE SIX\nHI','text','align=justify'),1920,1080);
    const rotated=KeynopeTrueType.metrics(make(50,'C64','text','orientation=cw'),1920,1080);
    return {normal,narrow,wide,effect,advance:[a.advance,b.advance],lineHeight:[a.lineHeight,b.lineHeight],positions:[a.rows[0].positions,b.rows[0].positions],wrap:[wrap(100),wrap(50)],bullet:bullet.rows.map(r=>({indent:r.indent,positions:r.positions})),codeAdvance:code.advance,justified:justified.rows.map(r=>r.inkWidth),rotated:{advance:rotated.advance,width:rotated.width,height:rotated.height}};
   });
   assert(Math.abs(result.narrow.width-result.normal.width*.5)<=2,JSON.stringify(result));
   assert.equal(result.narrow.height,result.normal.height,'glyph height must not shrink');
   assert(Math.abs((result.narrow.left-20)-(result.normal.left-20)*.5)<=1,'left glyph bearing scales around the unchanged box anchor');
   assert.equal(result.advance[1],result.advance[0]/2);
   assert.equal(result.lineHeight[1],result.lineHeight[0]);
   assert.deepEqual(result.positions[1],result.positions[0].map(x=>x/2),'caret and selections track compressed advances');
   assert(result.wrap[1]<result.wrap[0],'more text fits in the same box');
   assert(result.bullet.every(r=>r.indent===48*.4167&&r.positions[0]===48*.4167),'bullet continuation indentation scales');
   assert.equal(result.codeAdvance,24*.4167);
   assert(result.justified.every((w,i)=>Math.abs(w-[500,500,48*.4167][i])<.00001),'justification respects compressed text and short-line exception');
   assert.deepEqual(result.rotated,{advance:24*.4167,width:160,height:500});
   assert(result.effect.top<result.narrow.top,'outline extends above squeezed ink');
   fs.mkdirSync('output/ttf-width',{recursive:true});await page.screenshot({path:'output/ttf-width/'+engine+'.png',fullPage:true});
   console.log('PASS '+engine+': glyph width, height, anchors, wrapping, caret mapping, bullets, code, justification, rotation and effects');
  }finally{await browser.close();}
 }
})().catch(e=>{console.error(e);process.exitCode=1;});
