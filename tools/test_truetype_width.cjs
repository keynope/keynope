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
   const retiredTreatments=await page.evaluate(()=>{
    const render=tag=>{
     const line={col:4,row:4,parts:[{color:'#ffffff'}],trueType:{text:'C64 text',kind:'text',size:60,width:450,height:100,query:'outline=light&gradient-start=%23ff0055&gradient-end=%2355ffff&'+tag}};
     const c=document.createElement('canvas');c.width=600;c.height=180;
     KeynopeTrueType.draw(c.getContext('2d'),line,1,1,1920,1080);
     return c.toDataURL();
    };
    const original=render('');
    return ['glyph=blocks','glyph=braille','glyph=ascii','glyph=dense','font=custom'].map(tag=>({tag,same:render(tag)===original}));
   });
   for(const result of retiredTreatments)assert(result.same,`retired ${result.tag} must not alter TrueType export pixels`);
   const defaults=await page.evaluate(()=>({width:KeynopeTrueType.widthPercent({}),explicit:KeynopeTrueType.widthPercent({query:'ttf-width=100'}),bounds:KeynopeTrueType.initialBounds('ABCD',100,1920,1080)}));
   assert.equal(defaults.width,100);assert.equal(defaults.explicit,100);assert.equal(defaults.bounds.width,134);
   const aligned=await page.evaluate(()=>{
    const line={trueType:{text:'AB\nA',kind:'text',size:60,width:500,height:300,query:'ttf-width=100&text-align=right&text-valign=bottom&align=center'}};
    const m=KeynopeTrueType.metrics(line,1920,1080);
    return {end:m.rows.map(r=>r.inkWidth),starts:m.rows.map(r=>r.positions[0]),offsetY:m.offsetY};
   });
   assert(aligned.end.every(x=>Math.abs(x-500)<.0001));assert.equal(aligned.offsetY,156);assert(aligned.starts[1]>aligned.starts[0]);
   const rich=await page.evaluate(()=>{
    const line={col:0,row:0,parts:[{color:'#ffffff'}],trueType:{text:'A[color=#ff0000]B[/color]C',kind:'text',size:60,width:500,height:100,query:'ttf-weight=bold',richRuns:[{text:'A'},{text:'B',bold:true,color:'#ff0000'},{text:'C',italic:true}]}};
    const m=KeynopeTrueType.metrics(line,1920,1080),calls=[],original=CanvasRenderingContext2D.prototype.fillText;
    CanvasRenderingContext2D.prototype.fillText=function(text,...args){calls.push({text,font:this.font});return original.call(this,text,...args);};
    try{const c=document.createElement('canvas');c.width=500;c.height=100;KeynopeTrueType.draw(c.getContext('2d'),line,1,1,1920,1080);}finally{CanvasRenderingContext2D.prototype.fillText=original;}
    line.trueType.richRuns=[{text:'stale text',bold:true}];
    return {styles:m.rows[0].styles.filter(s=>!s.hidden).map(s=>({char:s.char,bold:s.bold,italic:s.italic,color:s.color})),calls,stale:KeynopeTrueType.metrics(line,1920,1080).rows[0].styles.some(s=>s.explicitWeight)};
   });
   assert.deepEqual(rich.styles,[{char:'A',bold:false,italic:false,color:null},{char:'B',bold:true,italic:false,color:'#ff0000'},{char:'C',bold:false,italic:true,color:null}]);
   assert(rich.calls.some(c=>c.text==='A'&&!c.font.includes('bold')),'explicit regular overrides whole-element bold');
   assert(rich.calls.some(c=>c.text==='B'&&c.font.includes('bold')),'Retro paints bold run');
   assert(rich.calls.some(c=>c.text==='C'&&c.font.includes('italic')),'Retro paints italic run');
   assert.equal(rich.stale,false,'mismatched source styles are ignored');
   const underline=await page.evaluate(()=>{
    const line={col:0,row:0,parts:[{color:'#ffffff'}],trueType:{text:'A',kind:'text',size:60,width:200,height:100,query:'',richRuns:[{text:'A',underline:true,color:'#ff0000'}]}};
    const c=document.createElement('canvas');c.width=200;c.height=100;const ctx=c.getContext('2d');
    KeynopeTrueType.draw(ctx,line,1,1,1920,1080);const on=Array.from(ctx.getImageData(5,53,1,1).data);
    ctx.clearRect(0,0,200,100);line.trueType.richRuns[0].underline=false;KeynopeTrueType.draw(ctx,line,1,1,1920,1080);
    return {on,off:Array.from(ctx.getImageData(5,53,1,1).data)};
   });
   assert(underline.on[0]>240&&underline.on[1]<10&&underline.on[3]>240,'Retro underline uses the run colour');
   assert.equal(underline.off[3],0,'removing underline clears its pixels');
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
