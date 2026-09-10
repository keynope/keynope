// Opt-in vector text. Sizes are authored pixels on a 1920x1080 reference slide.
// Raw text is intentional: Markdown delimiters stay literal in this object type.
const KeynopeTrueType = (() => {
  const is = element => !!element && new URLSearchParams(element.query||'').get('render')==='truetype';
  const size = element => Math.max(1,Math.min(512,Number(new URLSearchParams(element.query||'').get('ttf-size'))||72));
  const face=new FontFace('KeynopeC64','url(data:font/ttf;base64,'+keynopeTTFFontData+')');
  document.fonts.add(face);
  const ready=face.load().then(()=>{if(typeof drawFrame==='function')requestAnimationFrame(()=>drawFrame());return true;}).catch(()=>false);
  function layout(text,capacity){
    const chars=Array.from(text),rows=[];let start=0;
    capacity=Math.max(1,Math.floor(capacity));
    while(start<chars.length){
      let end=start;
      while(end<chars.length&&chars[end]!=='\n'&&end-start<capacity)end++;
      // Prefer word boundaries, retaining every source character for caret mapping.
      if(end<chars.length&&chars[end]!=='\n'&&end-start===capacity){
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
    const orientation=new URLSearchParams(data.query||'').get('orientation')||'';
    const boxWidth=data.width*scaleX,boxHeight=data.height*scaleY;
    const sideways=orientation==='cw'||orientation==='ccw';
    const width=sideways?boxHeight:boxWidth,height=sideways?boxWidth:boxHeight;
    const advance=data.size*.8,lineHeight=data.size*1.2;
    let lines;
    if(data.kind==='bullet'){
      lines=[];let offset=0;
      for(const raw of data.text.split('\n')){
        const continuation=raw.startsWith('  '),skip=continuation?2:0;
        const wrapped=layout(raw.slice(skip),width/advance-2);
        wrapped.forEach((r,i)=>lines.push({...r,start:r.start+offset+skip,end:r.end+offset+skip,indent:advance*2,bullet:i===0&&!continuation&&!!raw.trim()}));
        offset+=Array.from(raw).length+1;
      }
    }else lines=layout(data.text,width/advance);
    const longest=Math.max(0,...lines.map(row=>Array.from(row.text.trimEnd()).length*advance+(row.indent||0)));
    const justify=new URLSearchParams(data.query||'').get('align')==='justify';
    for(const row of lines){
      const chars=Array.from(row.text),end=Array.from(row.text.trimEnd()).length,first=chars.findIndex(ch=>ch!==' ');
      const gaps=chars.reduce((n,ch,i)=>n+(ch===' '&&i>first&&i<end?1:0),0),used=end*advance+(row.indent||0);
      const extra=justify&&gaps>0&&used>=longest*.7?Math.max(0,width-used)/gaps:0;
      row.positions=[row.indent||0];
      chars.forEach((ch,i)=>row.positions.push(row.positions[i]+advance+(ch===' '&&i>first&&i<end?extra:0)));
      row.inkWidth=row.positions[end];
    }
    return {width,height,boxWidth,boxHeight,orientation,advance,lineHeight,rows:lines};
  }
  function initialBounds(text,fontSize,cols,rows){
    const ctx=document.createElement('canvas').getContext('2d');ctx.font=fontSize+'px KeynopeC64,monospace';
    const lines=String(text).split('\n');let width=1,height=1;
    lines.forEach((line,i)=>{const ink=ctx.measureText(line);width=Math.max(width,Array.from(line).length*fontSize*.8,ink.actualBoundingBoxRight||0);height=Math.max(height,i*fontSize*1.2+fontSize*.8+(ink.actualBoundingBoxDescent||0));});
    return {width:Math.max(1,Math.min(cols,Math.ceil(width*cols/1920))),height:Math.max(1,Math.min(rows,Math.ceil(height*rows/1080)))};
  }
  const painted=new Map();let paintedBytes=0;
  function paint(line,m,q,color){
    const data=line.trueType,key=JSON.stringify([data,m.width,m.height,color,line.link]);
    if(painted.has(key))return painted.get(key);
    const padding=q.has('outline')?Math.max(1,Math.round(data.size*.035)):0;
    const mask=document.createElement('canvas');mask.width=Math.ceil(m.width)+padding*2;mask.height=Math.ceil(m.height)+padding*2;
    const ink=mask.getContext('2d');ink.font=(q.get('ttf-weight')==='bold'?'bold ':'')+data.size+'px KeynopeC64,monospace';ink.fillStyle='#fff';
    ink.translate(padding,padding);ink.save();ink.beginPath();ink.rect(0,0,m.width,m.height);ink.clip();
    for(const [i,row] of m.rows.entries()){
      const baseline=i*m.lineHeight+data.size*.8;
      if(row.bullet){ink.beginPath();ink.arc(m.advance*.65,baseline-data.size*.3,data.size*.085,0,Math.PI*2);ink.fill();}
      Array.from(row.text).forEach((ch,j)=>ink.fillText(ch,row.positions[j],baseline));
    }
    ink.restore();
    const layer=document.createElement('canvas');layer.width=mask.width;layer.height=mask.height;
    layer.keynopePadding=padding;
    const ctx=layer.getContext('2d');
    // Outline the union of the glyph pixels, not each rectangular TTF contour.
    if(q.has('outline')){
      const r=Math.max(1,Math.round(data.size*.035));
      const horizontal=document.createElement('canvas');horizontal.width=mask.width;horizontal.height=mask.height;
      const spread=horizontal.getContext('2d');
      for(let x=-r;x<=r;x++)spread.drawImage(mask,x,0);
      for(let y=-r;y<=r;y++)ctx.drawImage(horizontal,0,y);
      ctx.globalCompositeOperation='source-in';ctx.fillStyle=q.get('outline')==='dark'?'#15191d':'#fff';ctx.fillRect(0,0,layer.width,layer.height);ctx.globalCompositeOperation='source-over';
    }
    ink.globalCompositeOperation='source-in';let fill=color;
    if(q.has('gradient-start')&&q.has('gradient-end')){
      const direction=q.get('gradient-dir')||'horizontal';
      fill=ink.createLinearGradient(0,0,direction==='vertical'?0:m.width,direction==='horizontal'?0:m.height);
      try{fill.addColorStop(0,q.get('gradient-start'));fill.addColorStop(1,q.get('gradient-end'));}catch(_error){fill=color;}
    }
    ink.fillStyle=fill;ink.fillRect(0,0,m.width,m.height);ctx.drawImage(mask,0,0);
    if(data.kind==='code'){
      ctx.globalCompositeOperation='destination-over';ctx.fillStyle=q.get('bg')||'#333333';ctx.beginPath();ctx.roundRect(padding,padding,m.width,m.height,Math.min(8,data.size*.08));ctx.fill();ctx.globalCompositeOperation='source-over';
    }
    if(line.link){ctx.fillStyle=color;for(const [i,row] of m.rows.entries())ctx.fillRect(padding,padding+i*m.lineHeight+data.size*.84,row.inkWidth,Math.max(1,data.size*.025));}
    const bytes=layer.width*layer.height*4;
    while(painted.size&&(painted.size>=32||paintedBytes+bytes>16*1024*1024)){
      const first=painted.keys().next().value,old=painted.get(first);paintedBytes-=old.width*old.height*4;painted.delete(first);
    }
    painted.set(key,layer);paintedBytes+=bytes;return layer;
  }
  function draw(ctx,line,cellWidth,cellHeight,cols,rows,caret){
    const data=line.trueType;if(!data)return;
    const q=new URLSearchParams(data.query||''),m=metrics(line,cols,rows);
    const sx=cellWidth*cols/1920,sy=cellHeight*rows/1080;
    ctx.save();ctx.translate(line.col*cellWidth,line.row*cellHeight);ctx.scale(sx,sy);
    if(m.orientation==='cw'){ctx.translate(m.boxWidth,0);ctx.rotate(Math.PI/2);}
    else if(m.orientation==='down'){ctx.translate(m.boxWidth,m.boxHeight);ctx.rotate(Math.PI);}
    else if(m.orientation==='ccw'){ctx.translate(0,m.boxHeight);ctx.rotate(-Math.PI/2);}
    ctx.save();ctx.beginPath();ctx.rect(0,0,m.width,m.height);ctx.clip();
    const color=line.parts?.[0]?.color||'#f3efe0';
    const selected=caret?.trueType&&caret.element===line.element;
    for(const [i,row] of m.rows.entries()){
      const y=i*m.lineHeight;if(y>=m.height)break;
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
  return {is,size,layout,metrics,initialBounds,draw,ready};
})();
