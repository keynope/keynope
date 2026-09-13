// Standard text. Sizes are authored pixels on a 1920x1080 reference slide.
// Code preserves literal text; other text supports the deck's inline styles.
const KeynopeTrueType = (() => {
  const is = element => !!element && new URLSearchParams(element.query||'').get('render')==='truetype';
  const defaults = () => typeof deck!=='undefined'&&typeof pageIndex!=='undefined'?deck.pages?.[pageIndex]||{}:{};
  const presetSize = level => Math.max(1,Math.min(512,Math.round((level===1?386:level===2?193:97)*(defaults().ttfSize||97)/97)));
  const size = element => Math.max(1,Math.min(512,Number(new URLSearchParams(element.query||'').get('ttf-size'))||presetSize(element.kind==='heading'?element.level:0)));
  const widthPercent = element => {
    const value=Number(new URLSearchParams(element.query||'').get('ttf-width')||defaults().ttfWidth||100);
    return Number.isFinite(value)?Math.max(1,Math.min(200,value)):100;
  };
  const face=new FontFace('KeynopeC64','url(data:font/ttf;base64,'+keynopeTTFFontData+')');
  document.fonts.add(face);
  const ready=face.load().then(()=>{if(typeof drawFrame==='function')requestAnimationFrame(()=>drawFrame());return true;}).catch(()=>false);
  const emojiFaces=new Map();
  function loadEmoji(key,data){
    if(emojiFaces.has(key)){const record=emojiFaces.get(key);emojiFaces.delete(key);emojiFaces.set(key,record);return record;}
    const family='KeynopeEmoji_'+key.replace(/[^a-z0-9_]/gi,'');
    const font=new FontFace(family,'url(data:font/woff2;base64,'+data+')');
    document.fonts.add(font);
    const record={family,font,loaded:false,promise:null};emojiFaces.set(key,record);
    record.promise=font.load().then(()=>{
      record.loaded=true;painted.clear();paintedBytes=0;
      // Browsing the whole picker must not leave thousands of font faces live.
      for(const [oldKey,old] of emojiFaces){if(emojiFaces.size<=256)break;if(old!==record&&old.loaded){document.fonts.delete(old.font);emojiFaces.delete(oldKey);}}
      if(typeof drawFrame==='function')requestAnimationFrame(()=>drawFrame());
      return true;
    }).catch(()=>false);
    return record;
  }
  function readyFor(data){return Promise.all([ready,...Object.entries(data.emojiFonts||{}).map(([key,value])=>loadEmoji(key,value).promise)]);}
  const emojiRasters=new Map();let emojiRasterBytes=0;
  function emojiRaster(key,font,size){
    // Adjacent COLR contours are antialiased independently. Align their original
    // 160x160 subpixel grid, then scale the combined colour image once; otherwise
    // fractional font sizes produce dark hairline seams between colour layers.
    const pixels=Math.max(160,Math.ceil(size/160)*160),cacheKey=key+':'+pixels;
    if(emojiRasters.has(cacheKey)){const raster=emojiRasters.get(cacheKey);emojiRasters.delete(cacheKey);emojiRasters.set(cacheKey,raster);return raster;}
    const raster=document.createElement('canvas');raster.width=raster.height=pixels;
    const ctx=raster.getContext('2d');ctx.font=pixels+'px '+font.family;ctx.fillText('\ue000',0,pixels);
    const bytes=pixels*pixels*4;
    while(emojiRasters.size&&emojiRasterBytes+bytes>32*1024*1024){const first=emojiRasters.keys().next().value,old=emojiRasters.get(first);emojiRasterBytes-=old.width*old.height*4;emojiRasters.delete(first);}
    emojiRasters.set(cacheKey,raster);emojiRasterBytes+=bytes;return raster;
  }
  function styledCharacters(text,kind,runs=[]){
    const chars=Array.from(text),result=chars.map(char=>({char,hidden:false,bold:false,color:null}));
    for(const run of runs){
      if(!result[run.start]||(run.text&&chars.slice(run.start,run.start+run.length).join('')!==run.text))continue;
      result[run.start].emoji=run;
      for(let i=1;i<run.length;i++)if(result[run.start+i]){result[run.start+i].hidden=true;result[run.start+i].emojiContinuation=true;}
    }
    if(kind==='code')return result;
    const colors=[];
    for(let i=0;i<chars.length;){
      const tail=chars.slice(i).join(''),tag=tail.match(/^\[color=(#[0-9a-f]{6})\]/i)||tail.match(/^\[\/color\]/i);
      if(tag){const length=tag[0].length;for(let j=0;j<length;j++)result[i+j].hidden=true;if(tag[1])colors.push(tag[1]);else colors.pop();i+=length;continue;}
      // Asterisks are content. The B tool stores bold in ttf-weight metadata;
      // typing Markdown-looking punctuation must not alter the font or caret.
      result[i].color=colors.at(-1)||null;i++;
    }
    return result;
  }
  function layout(text,capacity,styles=null,offset=0){
    const chars=Array.from(text),rows=[];let start=0;
    capacity=Math.max(1,Math.floor(capacity));
    while(start<chars.length){
      let end=start;
      let used=0;
      while(end<chars.length&&chars[end]!=='\n'){
        const visible=!styles?.[offset+end]?.hidden;
        const units=styles?.[offset+end]?.units||1;
        if(visible&&used>0&&used+units>capacity)break;
        if(visible)used+=units;end++;
      }
      // Prefer word boundaries, retaining every source character for caret mapping.
      if(end<chars.length&&chars[end]!=='\n'){
        for(let i=end-1;i>start;i--)if(chars[i]===' '){end=i+1;break;}
      }
      rows.push({start,end,text:chars.slice(start,end).join('')});
      start=end+(chars[end]==='\n'?1:0);
    }
    if(!rows.length||chars.at(-1)==='\n')rows.push({start:chars.length,end:chars.length,text:''});
    return rows;
  }
  function metrics(line,cols,rows){
    const data=line.trueType,scaleX=1920/cols,scaleY=1080/rows;
    const styles=styledCharacters(data.text,data.kind,data.emojis);
    const orientation=new URLSearchParams(data.query||'').get('orientation')||'';
    const boxWidth=data.width*scaleX,boxHeight=data.height*scaleY;
    const sideways=orientation==='cw'||orientation==='ccw';
    const width=sideways?boxHeight:boxWidth,height=sideways?boxWidth:boxHeight;
    const widthScale=.4167*widthPercent(data)/100;
    const advance=data.size*.8*widthScale,lineHeight=data.size*1.2;
    const emojiSize=data.size*.8,emojiWidth=emojiSize*widthPercent(data)/100,emojiGap=Math.max(1,data.size/40);
    for(const style of styles)if(style.emoji)style.units=(emojiWidth+2*emojiGap)/advance;
    let lines;
    if(data.kind==='bullet'){
      lines=[];let offset=0;
      for(const raw of data.text.split('\n')){
        const continuation=raw.startsWith('  '),skip=continuation?2:0;
        const wrapped=layout(raw.slice(skip),width/advance-2,styles,offset+skip);
        wrapped.forEach((r,i)=>lines.push({...r,start:r.start+offset+skip,end:r.end+offset+skip,indent:advance*2,bullet:i===0&&!continuation&&!!raw.trim()}));
        offset+=Array.from(raw).length+1;
      }
    }else lines=layout(data.text,width/advance,styles);
    for(const row of lines)row.styles=styles.slice(row.start,row.end);
    const visibleLength=row=>{let end=row.styles.length;while(end>0&&(row.styles[end-1].hidden||row.styles[end-1].char===' '))end--;return row.styles.slice(0,end).reduce((n,s)=>n+(s.hidden?0:s.units||1),0);};
    const longest=Math.max(0,...lines.map(row=>visibleLength(row)*advance+(row.indent||0)));
    const query=new URLSearchParams(data.query||'');
    const alignment=query.get('text-align')||(query.get('align')==='justify'?'justify':'left');
    const justify=alignment==='justify';
    for(const row of lines){
      const chars=Array.from(row.text),end=Array.from(row.text.trimEnd()).length,first=chars.findIndex(ch=>ch!==' ');
      const gaps=chars.reduce((n,ch,i)=>n+(ch===' '&&!row.styles[i]?.hidden&&i>first&&i<end?1:0),0),used=visibleLength(row)*advance+(row.indent||0);
      const extra=justify&&gaps>0&&used>=longest*.7?Math.max(0,width-used)/gaps:0;
      const remaining=Math.max(0,width-used);
      row.offset=alignment==='center'?remaining/2:alignment==='right'?remaining:0;
      row.positions=[(row.indent||0)+row.offset];
      chars.forEach((ch,i)=>row.positions.push(row.positions[i]+(row.styles[i]?.hidden?0:advance*(row.styles[i]?.units||1))+(ch===' '&&!row.styles[i]?.hidden&&i>first&&i<end?extra:0)));
      row.inkWidth=row.positions[end];
    }
    const remainingHeight=Math.max(0,height-lines.length*lineHeight);
    let offsetY=query.get('text-valign')==='middle'?remainingHeight/2:query.get('text-valign')==='bottom'?remainingHeight:0;
    if(line.role==='shape-label'){
      // Centre visible ink, not the line-height box (which has extra space
      // below its baseline). This also accounts for capitals and descenders.
      const measure=shapeLabelMeasureContext;
      let top=Infinity,bottom=-Infinity;
      lines.forEach((row,i)=>{
        const text=row.styles.filter(s=>!s.hidden&&!s.emoji).map(s=>s.char).join('');
        if(text.trim()){
          measure.font=(query.get('ttf-weight')==='bold'||row.styles.some(s=>s.bold)?'bold ':'')+data.size+'px KeynopeC64,monospace';
          const ink=measure.measureText(text),baseline=i*lineHeight+data.size*.8;
          top=Math.min(top,baseline-ink.actualBoundingBoxAscent);bottom=Math.max(bottom,baseline+ink.actualBoundingBoxDescent);
        }
        if(row.styles.some(s=>s.emoji)){top=Math.min(top,i*lineHeight);bottom=Math.max(bottom,i*lineHeight+emojiSize);}
      });
      if(Number.isFinite(top))offsetY=(height-(bottom-top))/2-top;
    }
    return {width,height,boxWidth,boxHeight,orientation,widthScale,advance,lineHeight,emojiSize,emojiWidth,emojiGap,offsetY,rows:lines};
  }
  const shapeLabelMeasureContext=document.createElement('canvas').getContext('2d');
  function initialBounds(text,fontSize,cols,rows,percent=widthPercent({})){
    const ctx=document.createElement('canvas').getContext('2d');ctx.font=fontSize+'px KeynopeC64,monospace';
    const lines=String(text).split('\n');let width=1,height=1;
    lines.forEach((line,i)=>{const ink=ctx.measureText(line);width=Math.max(width,Array.from(line).length*fontSize*.8,ink.actualBoundingBoxRight||0);height=Math.max(height,i*fontSize*1.2+fontSize*.8+(ink.actualBoundingBoxDescent||0));});
    return {width:Math.max(1,Math.min(cols,Math.ceil(width*.4167*percent/100*cols/1920))),height:Math.max(1,Math.min(rows,Math.ceil(height*rows/1080)))};
  }
  const painted=new Map();let paintedBytes=0;
  // Sample the font's coloured ink, not a substitute bitmap alphabet. All
  // treatments keep the same layout/caret/box, and can be changed losslessly.
  function rasterTreatment(layer,mode,size,widthScale){
    if(!['blocks','braille','ascii','dense'].includes(mode))return layer;
    const source=layer.getContext('2d').getImageData(0,0,layer.width,layer.height);
    const result=document.createElement('canvas');result.width=layer.width;result.height=layer.height;result.keynopePadding=layer.keynopePadding;
    const ctx=result.getContext('2d'),step=Math.max(1,Math.min(8,size/12)),cw=Math.max(1,step*widthScale/.4167),ch=step*2;
    const sample=(x,y,w,h)=>{
      let r=0,g=0,b=0,a=0,n=0;
      for(let yy=Math.floor(y);yy<Math.min(source.height,Math.ceil(y+h));yy++)for(let xx=Math.floor(x);xx<Math.min(source.width,Math.ceil(x+w));xx++){
        if(xx<0||yy<0)continue;const i=(yy*source.width+xx)*4,alpha=source.data[i+3]/255;
        r+=source.data[i]*alpha;g+=source.data[i+1]*alpha;b+=source.data[i+2]*alpha;a+=alpha;n++;
      }
      return {coverage:n?a/n:0,color:a?'rgb('+Math.round(r/a)+','+Math.round(g/a)+','+Math.round(b/a)+')':'#fff'};
    };
    ctx.textBaseline='top';ctx.font=ch+'px monospace';
    for(let y=0;y<layer.height;y+=ch)for(let x=0;x<layer.width;x+=cw){
      const pixel=sample(x,y,cw,ch);if(pixel.coverage<.035)continue;
      ctx.fillStyle=pixel.color;
      if(mode==='blocks'){
        for(let dy=0;dy<2;dy++){const half=sample(x,y+dy*ch/2,cw,ch/2);if(half.coverage>=.25){ctx.fillStyle=half.color;const left=Math.floor(x),top=Math.floor(y+dy*ch/2);ctx.fillRect(left,top,Math.ceil(x+cw)-left,Math.ceil(y+(dy+1)*ch/2)-top);}}
        continue;
      }
      let glyph;
      if(mode==='braille'){
        let bits=0;const dots=[[1,8],[2,16],[4,32],[64,128]];
        for(let dy=0;dy<4;dy++)for(let dx=0;dx<2;dx++)if(sample(x+dx*cw/2,y+dy*ch/4,cw/2,ch/4).coverage>=.25)bits|=dots[dy][dx];
        if(!bits)continue;glyph=String.fromCharCode(0x2800+bits);
      }else{
        const ramp=mode==='dense'?' .,:;irsXA253hMHGS#9B&@':' .:-=+*#%@';
        glyph=ramp[Math.max(1,Math.min(ramp.length-1,Math.round(pixel.coverage*(ramp.length-1))))];
      }
      const advance=ctx.measureText(glyph).width||cw;ctx.save();ctx.translate(x,y);ctx.scale(cw/advance,1);ctx.fillText(glyph,0,0);ctx.restore();
    }
    return result;
  }
  function paint(line,m,q,color){
    const data=line.trueType,key=JSON.stringify([{...data,emojiFonts:undefined},m.width,m.height,m.offsetY,color,line.link]);
    if(painted.has(key))return painted.get(key);
    let fontsReady=true;
    for(const [key,value] of Object.entries(data.emojiFonts||{}))if(!loadEmoji(key,value).loaded)fontsReady=false;
    const padding=q.has('outline')?Math.max(1,Math.round(data.size*.035)):0;
    const mask=document.createElement('canvas');mask.width=Math.ceil(m.width)+padding*2;mask.height=Math.ceil(m.height)+padding*2;
    const ink=mask.getContext('2d');ink.font=(q.get('ttf-weight')==='bold'?'bold ':'')+data.size+'px KeynopeC64,monospace';ink.fillStyle='#fff';
    ink.translate(padding,padding);ink.save();ink.beginPath();ink.rect(0,0,m.width,m.height);ink.clip();
    // Scale glyph ink only. Layout, clipping, effects and caret coordinates use
    // the final (compressed) positions, leaving the authored text box unchanged.
    ink.scale(m.widthScale,1);
    for(const [i,row] of m.rows.entries()){
      const baseline=m.offsetY+i*m.lineHeight+data.size*.8;
      if(row.bullet){ink.beginPath();ink.arc((row.offset+m.advance*.65)/m.widthScale,baseline-data.size*.3,data.size*.085,0,Math.PI*2);ink.fill();}
      Array.from(row.text).forEach((ch,j)=>{if(row.styles[j]?.hidden||row.styles[j]?.emoji)return;ink.font=(row.styles[j]?.bold||q.get('ttf-weight')==='bold'?'bold ':'')+data.size+'px KeynopeC64,monospace';ink.fillText(ch,row.positions[j]/m.widthScale,baseline);});
    }
    ink.restore();
    const emojiLayer=document.createElement('canvas');emojiLayer.width=mask.width;emojiLayer.height=mask.height;
    const emojiInk=emojiLayer.getContext('2d');emojiInk.translate(padding,padding);emojiInk.beginPath();emojiInk.rect(0,0,m.width,m.height);emojiInk.clip();
    for(const [i,row] of m.rows.entries())row.styles.forEach((style,j)=>{
      if(!style.emoji)return;const font=emojiFaces.get(style.emoji.key);if(!font?.loaded)return;
      const raster=emojiRaster(style.emoji.key,font,Math.max(m.emojiSize,m.emojiWidth));
      emojiInk.drawImage(raster,row.positions[j]+m.emojiGap,m.offsetY+i*m.lineHeight,m.emojiWidth,m.emojiSize);
    });
    const layer=document.createElement('canvas');layer.width=mask.width;layer.height=mask.height;
    layer.keynopePadding=padding;
    const ctx=layer.getContext('2d');
    // Outline the union of the glyph pixels, not each rectangular TTF contour.
    if(q.has('outline')){
      const r=Math.max(1,Math.round(data.size*.035));
      const horizontal=document.createElement('canvas');horizontal.width=mask.width;horizontal.height=mask.height;
      const spread=horizontal.getContext('2d');
      for(let x=-r;x<=r;x++){spread.drawImage(mask,x,0);spread.drawImage(emojiLayer,x,0);}
      for(let y=-r;y<=r;y++)ctx.drawImage(horizontal,0,y);
      ctx.globalCompositeOperation='source-in';ctx.fillStyle=q.get('outline')==='dark'?'#15191d':'#fff';ctx.fillRect(0,0,layer.width,layer.height);ctx.globalCompositeOperation='source-over';
    }
    ink.globalCompositeOperation='source-in';let fill=color;
    if(q.has('gradient-start')&&q.has('gradient-end')){
      const direction=q.get('gradient-dir')||'horizontal';
      fill=ink.createLinearGradient(0,0,direction==='vertical'?0:m.width,direction==='horizontal'?0:m.height);
      try{fill.addColorStop(0,q.get('gradient-start'));fill.addColorStop(1,q.get('gradient-end'));}catch(_error){fill=color;}
    }
    ink.fillStyle=fill;ink.fillRect(0,0,m.width,m.height);
    // Default text colour preserves the artwork. Explicit gradients are a
    // deliberate treatment and apply to emoji ink as well.
    if(q.has('gradient-start')&&q.has('gradient-end')){
      emojiInk.globalCompositeOperation='source-in';emojiInk.fillStyle=fill;emojiInk.fillRect(0,0,m.width,m.height);emojiInk.globalCompositeOperation='source-over';
    }
    ink.globalCompositeOperation='source-atop';ink.save();ink.scale(m.widthScale,1);
    for(const [i,row] of m.rows.entries())Array.from(row.text).forEach((ch,j)=>{const style=row.styles[j];if(!style?.color||style.hidden||style.emoji)return;ink.fillStyle=style.color;ink.font=(style.bold||q.get('ttf-weight')==='bold'?'bold ':'')+data.size+'px KeynopeC64,monospace';ink.fillText(ch,row.positions[j]/m.widthScale,m.offsetY+i*m.lineHeight+data.size*.8);});
    ink.restore();ctx.drawImage(mask,0,0);ctx.drawImage(emojiLayer,0,0);
    if(data.kind==='code'){
      ctx.globalCompositeOperation='destination-over';ctx.fillStyle=q.get('bg')||'#333333';ctx.beginPath();ctx.roundRect(padding,padding,m.width,m.height,Math.min(8,data.size*.08));ctx.fill();ctx.globalCompositeOperation='source-over';
    }
    if(line.link){ctx.fillStyle=color;for(const [i,row] of m.rows.entries())ctx.fillRect(padding+row.offset,padding+m.offsetY+i*m.lineHeight+data.size*.84,row.inkWidth-row.offset,Math.max(1,data.size*.025));}
    const output=rasterTreatment(layer,q.get('glyph'),data.size,m.widthScale);
    // Tint after glyph conversion; change RGB only, never coverage or layout.
    // The emoji-only mask excludes surrounding text, backdrops and outlines.
    const tint=/^#([0-9a-f]{6})$/i.exec(q.get('tint')||'');
    if(tint&&data.emojis?.length){
      const mask=rasterTreatment(emojiLayer,q.get('glyph'),data.size,m.widthScale);
      const coverage=mask.getContext('2d').getImageData(0,0,mask.width,mask.height).data;
      const target=output.getContext('2d'),pixels=target.getImageData(0,0,output.width,output.height),rgb=[0,2,4].map(i=>parseInt(tint[1].slice(i,i+2),16));
      for(let i=0;i<pixels.data.length;i+=4){
        if(!coverage[i+3]||!pixels.data[i+3])continue;
        const luma=299*pixels.data[i]+587*pixels.data[i+1]+114*pixels.data[i+2];
        for(let c=0;c<3;c++)pixels.data[i+c]=Math.floor((rgb[c]*luma+127500)/255000);
      }
      target.putImageData(pixels,0,0);
    }
    const bytes=output.width*output.height*4;
    while(painted.size&&(painted.size>=32||paintedBytes+bytes>16*1024*1024)){
      const first=painted.keys().next().value,old=painted.get(first);paintedBytes-=old.width*old.height*4;painted.delete(first);
    }
    if(fontsReady){painted.set(key,output);paintedBytes+=bytes;}return output;
  }
  function draw(ctx,line,cellWidth,cellHeight,cols,rows,caret){
    const data=line.trueType;if(!data)return;
    const q=new URLSearchParams(data.query||''),m=metrics(line,cols,rows);
    const sx=cellWidth*cols/1920,sy=cellHeight*rows/1080;
    const labelDY=line.role==='shape-label'?(Number(q.get('shape-label-dy'))||0):0;
    ctx.save();ctx.translate(line.col*cellWidth,(line.row+labelDY)*cellHeight);ctx.scale(sx,sy);
    if(m.orientation==='cw'){ctx.translate(m.boxWidth,0);ctx.rotate(Math.PI/2);}
    else if(m.orientation==='down'){ctx.translate(m.boxWidth,m.boxHeight);ctx.rotate(Math.PI);}
    else if(m.orientation==='ccw'){ctx.translate(0,m.boxHeight);ctx.rotate(-Math.PI/2);}
    ctx.save();ctx.beginPath();ctx.rect(0,0,m.width,m.height);ctx.clip();
    const color=line.parts?.[0]?.color||'#f3efe0';
    const selected=caret?.trueType&&caret.element===line.element;
    for(const [i,row] of m.rows.entries()){
      const y=m.offsetY+i*m.lineHeight;if(y>=m.height)break;
      if(selected){
        const a=Math.max(row.start,caret.selectionStart),b=Math.min(row.end,caret.selectionEnd);
        if(b>a){ctx.fillStyle='rgba(85,170,255,.58)';ctx.fillRect(row.positions[a-row.start],y,row.positions[b-row.start]-row.positions[a-row.start],m.lineHeight);}
      }
      const baseline=y+data.size*.8;
      const next=m.rows[i+1];
      if(selected&&caret.cursor>=row.start&&caret.cursor<=row.end&&(!next||caret.cursor<next.start)&&(performance.now()-caret.started)%900<650){
        const offset=caret.cursor-row.start,x=row.positions[offset],cell=(row.positions[offset+1]??(x+m.advance))-x;
        ctx.fillStyle=color;ctx.fillRect(x,baseline+2,Math.max(2,cell),Math.max(2,data.size*.04));
      }
    }
    ctx.restore();
    ctx.globalAlpha=q.get('transparent')==='1'?.5:1;
    if(q.get('shadow')==='soft'||q.get('shadow')==='solid'){
      ctx.shadowColor=q.get('shadow-color')||'#ffffff';ctx.shadowOffsetX=(Number(q.get('shadow-x'))||1)*1920/cols*sx;
      ctx.shadowOffsetY=(Number(q.get('shadow-y'))||1)*1080/rows*sy;ctx.shadowBlur=q.get('shadow')==='soft'?Math.max(1,data.size*.09*sy):0;
    }
    const layer=paint(line,m,q,color);
    ctx.drawImage(layer,-layer.keynopePadding,-layer.keynopePadding);
    ctx.restore();
  }
  ready.then(()=>{painted.clear();paintedBytes=0;});
  return {is,size,presetSize,widthPercent,layout,metrics,initialBounds,draw,ready,readyFor};
})();
