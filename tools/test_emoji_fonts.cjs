// Full-batch colour-font fidelity plus shared-renderer layout/effects tests.
// Needs Playwright and Python 3 (stdlib only). No system emoji font is used.
const fs=require('node:fs'),os=require('node:os'),path=require('node:path'),assert=require('node:assert/strict');
const {execFileSync}=require('node:child_process'),{gunzipSync}=require('node:zlib');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const source=gunzipSync(fs.readFileSync('assets/emoji/keynope-emoji-glyphs.bin.gz'));
const artworks=[];let pos=13;
for(let i=0;i<source.readUInt32BE(9);i++){
 const length=source.readUInt16BE(pos);pos+=2;const key=source.subarray(pos,pos+length).toString();pos+=length;
 artworks.push({key,cells:source.subarray(pos,pos+12800).toString('base64')});pos+=12800;
}
const temp=fs.mkdtempSync(path.join(os.tmpdir(),'keynope-emoji-font-test-'));
execFileSync('python3',['-m','zipfile','-e','assets/emoji/keynope-emoji-fonts.zip',temp]);
const font=key=>fs.readFileSync(path.join(temp,key+'.woff2')).toString('base64');
(async()=>{
 for(const engine of ['chromium','webkit']){
  const browser=await playwright[engine].launch({headless:true,...(engine==='chromium'?{executablePath:process.env.CHROME_PATH}:{})});
  try{
   const page=await browser.newPage({viewport:{width:1000,height:850}});
   await page.setContent('<style>body{background:#161a1d;color:white;font:14px monospace}canvas{margin:4px;vertical-align:top}</style>');
   // At 160px, every canonical subpixel maps exactly to a 1x2px rectangle.
   // Compare all RGBA pixels, not merely a screenshot or a nonempty glyph.
   let maximum=0,total=0;
   for(let index=0;index<artworks.length;index+=24){
    const batch=artworks.slice(index,index+24).map(a=>({...a,font:font(a.key)}));
    const results=await page.evaluate(async batch=>{
     const out=[];
     for(const a of batch){
      const face=new FontFace('EmojiBatch','url(data:font/woff2;base64,'+a.font+')');await face.load();document.fonts.add(face);
      const canvas=document.createElement('canvas');canvas.width=160;canvas.height=160;
      const ctx=canvas.getContext('2d');ctx.font='160px EmojiBatch';ctx.fillText('\ue000',0,160);
      const pixels=ctx.getImageData(0,0,160,160).data,cells=Uint8Array.from(atob(a.cells),c=>c.charCodeAt(0));let bad=0,ink=0;const examples=[];
      for(let y=0;y<160;y++)for(let x=0;x<160;x++){
       const subY=Math.floor(y/2),at=(Math.floor(subY/2)*80+Math.floor(x/2))*4,p=(y*160+x)*4;
       const on=!!(cells[at]&(1<<((subY%2)*2+x%2)));
       if(pixels[p+3])ink++;
       // Browser font rasterizers can leave one or two alpha levels on an
       // integer-aligned edge; allow only that rounding, not displaced pixels.
       if(on?(pixels[p+3]<253||[0,1,2].some(c=>Math.abs(pixels[p+c]-cells[at+c+1])>2)):pixels[p+3]>2){bad++;if(examples.length<5)examples.push({x,y,on,actual:Array.from(pixels.slice(p,p+4)),expected:Array.from(cells.slice(at+1,at+4))});}
      }
      if(bad>=10){
       const reference=document.createElement('canvas');reference.width=160;reference.height=160;const r=reference.getContext('2d');
       for(let y=0;y<160;y++)for(let x=0;x<160;x++){const sy=Math.floor(y/2),at=(Math.floor(sy/2)*80+Math.floor(x/2))*4;if(cells[at]&(1<<((sy%2)*2+x%2))){r.fillStyle=`rgb(${cells[at+1]},${cells[at+2]},${cells[at+3]})`;r.fillRect(x,y,1,1);}}
       document.body.append(a.key+' actual / expected',canvas,reference,document.createElement('br'));
      }
      out.push({key:a.key,bad,ink,examples});document.fonts.delete(face);
     }
     return out;
    },batch);
    if(results.some(r=>r.bad>=10)){console.log(JSON.stringify(results[0]));fs.mkdirSync('output/emoji-font-tests',{recursive:true});await page.screenshot({path:'output/emoji-font-tests/'+engine+'-failure.png',fullPage:true});}
    for(const r of results){assert(r.ink>0,'empty emoji '+r.key);maximum=Math.max(maximum,r.bad);total+=r.bad;assert(r.bad<10,`${engine} ${r.key}: ${r.bad} pixels differ from canonical artwork`);}
    if(index%480===0)console.log(engine+': checked '+Math.min(index+24,artworks.length)+'/'+artworks.length);
   }
   console.log(`PASS ${engine}: all ${artworks.length} colour fonts, maximum ${maximum} mismatched pixels, total ${total}`);
   await page.addScriptTag({content:'const keynopeTTFFontData='+JSON.stringify(fs.readFileSync('assets/keynope-c64.ttf.base64','utf8').trim())+';\n'+fs.readFileSync('web/truetype.js','utf8')});
   const samples=['1f60d','1f3c1','1f469_1f3fd_200d_1f680','1f1f3_1f1f1','2728'].filter(key=>fs.existsSync(path.join(temp,key+'.woff2')));
   const fonts=Object.fromEntries(samples.map(key=>[key,font(key)]));
   const result=await page.evaluate(async fonts=>{
    await KeynopeTrueType.readyFor({emojiFonts:fonts});
    const hashes=[];let standardPositions;
    for(const mode of ['','blocks','braille','ascii','dense','']){
     const canvas=document.createElement('canvas');canvas.width=480;canvas.height=200;document.body.append(canvas);
     const data={kind:'text',text:'D😍D',size:180,width:460,height:190,query:'render=truetype&glyph='+mode,emojis:[{start:1,length:1,key:'1f60d'}],emojiFonts:{'1f60d':fonts['1f60d']}};
     const line={col:10,row:5,parts:[{color:'#fff'}],trueType:data};
     const m=KeynopeTrueType.metrics(line,1920,1080);standardPositions=m.rows[0].positions;
     KeynopeTrueType.draw(canvas.getContext('2d'),line,1,1,1920,1080);
     const p=canvas.getContext('2d').getImageData(0,0,480,200).data;let hash=2166136261,ink=0;for(let i=0;i<p.length;i++){hash=Math.imul(hash^p[i],16777619);if(i%4===3&&p[i])ink++;}hashes.push({hash,ink});
    }
    // ZWJ and skin-tone sequence occupy one square, not four squeezed letters.
    const seq={trueType:{kind:'bullet',text:'D👩🏽‍🚀D',size:100,width:240,height:300,query:'render=truetype',emojis:[{start:1,length:4,key:'1f469_1f3fd_200d_1f680'}]}};
    const m=KeynopeTrueType.metrics(seq,1920,1080),positions=m.rows[0].positions;
    const effects={};
    for(const [name,query] of Object.entries({plain:'',transparent:'transparent=1',outline:'outline=light',shadow:'shadow=soft',gradient:'gradient-start=%23ff0055&gradient-end=%2355aaff'})){
     const canvas=document.createElement('canvas');canvas.width=220;canvas.height=220;document.body.append(canvas);
     const data={kind:'text',text:'😍',size:137,width:180,height:180,query:'render=truetype&'+query,emojis:[{start:0,length:1,key:'1f60d'}],emojiFonts:{'1f60d':fonts['1f60d']}};
     KeynopeTrueType.draw(canvas.getContext('2d'),{col:20,row:20,parts:[{color:'#ffffff'}],trueType:data},1,1,1920,1080);
     const p=canvas.getContext('2d').getImageData(0,0,220,220).data;let hash=2166136261,ink=0,alpha=0;
     for(let i=0;i<p.length;i++){hash=Math.imul(hash^p[i],16777619);if(i%4===3){alpha=Math.max(alpha,p[i]);if(p[i]>2)ink++;}}effects[name]={hash,ink,alpha};
    }
    // No dark grid through the yellow face at non-grid-aligned font sizes.
    const face=await new FontFace('SeamReference','url(data:font/woff2;base64,'+fonts['1f60d']+')').load();document.fonts.add(face);
    const base=document.createElement('canvas');base.width=base.height=160;const b=base.getContext('2d');b.font='160px SeamReference';b.fillText('\ue000',0,160);
    const actual=document.createElement('canvas');actual.width=actual.height=200;
    const d={kind:'text',text:'😍',size:137,width:200,height:200,query:'render=truetype',emojis:[{start:0,length:1,key:'1f60d'}],emojiFonts:{'1f60d':fonts['1f60d']}};
    KeynopeTrueType.draw(actual.getContext('2d'),{col:0,row:0,parts:[],trueType:d},1,1,1920,1080);
    const ref=document.createElement('canvas');ref.width=ref.height=200;ref.getContext('2d').drawImage(base,137/40,0,137*.8,137*.8);
    const a=actual.getContext('2d').getImageData(0,0,200,200).data,r=ref.getContext('2d').getImageData(0,0,200,200).data;
    let seams=0;for(let i=0;i<a.length;i++)if(Math.abs(a[i]-r[i])>2)seams++;
    // Every emoji is rendered in the real font, with reference artwork beside it.
    for(const [key,data] of Object.entries(fonts)){
     const canvas=document.createElement('canvas');canvas.width=170;canvas.height=170;document.body.append(canvas);
     const face=new FontFace('Inspect'+key,'url(data:font/woff2;base64,'+data+')');await face.load();document.fonts.add(face);
     const ctx=canvas.getContext('2d');ctx.font='160px Inspect'+key;ctx.fillText('\ue000',5,165);
    }
    const tintChecks=[];
    for(const mode of ['','blocks','braille','ascii','dense']){
     const snapshots=[];
     for(const tint of ['', '#55aaff', '#ffffff', '']){
      const canvas=document.createElement('canvas');canvas.width=300;canvas.height=160;
      const data={kind:'text',text:'D😍',size:137,width:300,height:160,query:'render=truetype&glyph='+mode+(tint?'&tint='+encodeURIComponent(tint):''),emojis:[{start:1,length:1,key:'1f60d'}],emojiFonts:{'1f60d':fonts['1f60d']}};
      KeynopeTrueType.draw(canvas.getContext('2d'),{col:0,row:0,parts:[{color:'#ffffff'}],trueType:data},1,1,1920,1080);
      snapshots.push(canvas.getContext('2d').getImageData(0,0,300,160).data);
     }
     let alphaChanged=0,textChanged=0,changed=0,grayErrors=0,restoreErrors=0;const shades=new Set();
     for(let i=0;i<snapshots[0].length;i+=4){
      const x=(i/4)%300;
      if(snapshots.some(p=>p[i+3]!==snapshots[0][i+3]))alphaChanged++;
      for(let c=0;c<3;c++){
       if(x<40&&snapshots[0][i+c]!==snapshots[1][i+c])textChanged++;
       if(snapshots[0][i+c]!==snapshots[1][i+c])changed++;
       if(snapshots[0][i+c]!==snapshots[3][i+c])restoreErrors++;
      }
      if(snapshots[2][i+3]>250&&(snapshots[2][i]!==snapshots[2][i+1]||snapshots[2][i]!==snapshots[2][i+2]))grayErrors++;
      if(x>55&&snapshots[1][i+3]>250)shades.add(snapshots[1][i+2]);
     }
     tintChecks.push({mode,alphaChanged,textChanged,changed,grayErrors,restoreErrors,shades:shades.size});
    }
    return {hashes,standardPositions,positions,advance:m.advance,effects,seams,tintChecks};
   },fonts);
   assert.equal(result.hashes[0].hash,result.hashes[5].hash,'return to Default is lossless');assert.equal(new Set(result.hashes.slice(0,5).map(h=>h.hash)).size,5);
   for(const h of result.hashes)assert(h.ink>1000,'all glyph treatments paint');
   assert(Math.abs(result.standardPositions[2]-result.standardPositions[1]-153)<.001,'square emoji plus spacers');
   assert(Math.abs(result.positions[2]-result.positions[1]-85)<.001,'one advance for whole sequence');
   assert.equal(result.positions[2],result.positions[5],'ZWJ/skin continuation takes no extra width');
   assert.equal(result.seams,0,'fractional sizes have no colour-layer seams');
   assert.equal(new Set(Object.values(result.effects).map(e=>e.hash)).size,5,'all emoji effects apply');
   assert(result.effects.transparent.alpha>=126&&result.effects.transparent.alpha<=128,'see-through halves opacity');
   assert(result.effects.outline.ink>result.effects.plain.ink,'outline surrounds emoji');
   assert(result.effects.shadow.ink>result.effects.plain.ink,'shadow surrounds emoji');
   for(const t of result.tintChecks){
    assert.equal(t.alphaChanged,0,t.mode+' tint preserves glyph geometry and alpha');assert.equal(t.textChanged,0,t.mode+' surrounding text unchanged');
    assert(t.changed>100,t.mode+' tint changes colour');assert(t.shades>5,t.mode+' tint retains shading');assert.equal(t.grayErrors,0,t.mode+' white tint is grayscale');assert.equal(t.restoreErrors,0,t.mode+' disabling tint restores original colours');
   }
   fs.mkdirSync('output/emoji-font-tests',{recursive:true});await page.screenshot({path:'output/emoji-font-tests/'+engine+'.png',fullPage:true});
   console.log('PASS '+engine+': shared text renderer, five treatments, lossless return, emoji spacing and ZWJ caret metrics');
  }finally{await browser.close();}
 }
})().catch(e=>{console.error(e);process.exitCode=1;}).finally(()=>fs.rmSync(temp,{recursive:true,force:true}));
