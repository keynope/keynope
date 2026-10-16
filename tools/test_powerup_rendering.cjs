// Production renderer consumer/reconciliation proof, not a synthetic no-op cache test.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-powerup-site');
const server=http.createServer((req,res)=>{
  let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';
  const file=path.resolve(root,'.'+name);if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}
  res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm','.json':'application/json'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));
});
(async()=>{
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const browser=await playwright.chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
  try{
    const context=await browser.newContext({viewport:{width:1440,height:1000}});
    await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,route=>route.abort());
    const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
    await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');
    await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
    const result=await page.evaluate(async()=>{
      const host=document.createElement('div');host.id='render-proof';
      Object.assign(host.style,{position:'fixed',inset:'0',zIndex:99999,background:'#111827',overflow:'hidden'});document.body.append(host);
      const text={fontId:'c64',family:'KeynopeC64, monospace',size:28,widthScale:.4167,lineHeight:1.2,align:'start',vertical:'top',paragraphBefore:0,paragraphAfter:0,role:'text'};
      const scene={version:1,width:1920,height:1080,background:'#111827',objects:Array.from({length:80},(_,i)=>({id:'text-'+i,kind:'text',bounds:{x:40+i%8*230,y:40+Math.floor(i/8)*75,width:215,height:60},paint:{color:'#eef2f6'},text:{...text,runs:[{text:'Object '+i}]}}))};
      scene.objects.push({id:'shape',kind:'shape',shape:'square',bounds:{x:300,y:830,width:180,height:100},paint:{color:'#55aaff'}});
      const canvas=document.createElement('canvas');canvas.width=canvas.height=1;canvas.getContext('2d').fillRect(0,0,1,1);
      const source=canvas.toDataURL();
      scene.objects.push({id:'image',kind:'image',bounds:{x:620,y:830,width:180,height:100},paint:{color:'#ffffff'},media:{source,width:1,height:1,frames:[{source,delayMs:80},{source,delayMs:80}]}});
      scene.objects.push({id:'connector',kind:'connector',bounds:{x:480,y:880,width:140,height:1},points:[{x:480,y:880},{x:620,y:880}],paint:{color:'#ffffff',strokeWidth:2},arrows:'none'});
      const view=KeynopeScene.mount(host,scene);host.firstChild.style.transform='scale('+Math.min(innerWidth/1920,innerHeight/1080)+')';host.firstChild.style.transformOrigin='0 0';await view.ready;
      const root=view.element,originalNodes=new Map(scene.objects.map(o=>[o.id,root.querySelector('[data-object-id="'+o.id+'"]')]));
      const records=[],mutate=async(name,expected,update)=>{
        const before=view.renderStats(),next=structuredClone(scene);update(next);
        const start=performance.now();if(!view.update(next))throw Error(name+' took a whole-scene fallback');await view.ready;
        await new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)));
        const after=view.renderStats();
        if(root!==view.element||after.objectMounts!==before.objectMounts)throw Error(name+' remounted objects');
        for(const [id,node]of originalNodes)if(node!==root.querySelector('[data-object-id="'+id+'"]'))throw Error(name+' replaced '+id);
        if(JSON.stringify([...after.lastChangedObjectIDs].sort())!==JSON.stringify([...expected].sort()))throw Error(name+' touched '+JSON.stringify(after.lastChangedObjectIDs));
        const delta=Object.fromEntries(Object.keys(before).filter(key=>key!=='lastChangedObjectIDs').map(key=>[key,after[key]-before[key]]));
        records.push({name,delta,changed:after.lastChangedObjectIDs,inputToPaintMs:performance.now()-start});scene.objects=next.objects;scene.background=next.background;
        return delta;
      };
      const paint=await mutate('paint',['text-0'],s=>{s.objects[0].paint.color='#ff55aa';s.objects[0].paint.shadowColor='#5555ff';s.objects[0].paint.shadowX=2;s.objects[0].paint.shadowY=2;});
      if(paint.textRebuilds||paint.textContentUpdates||paint.textWidthUpdates)throw Error('paint reshaped text');
      const move=await mutate('move/rotate',['text-0'],s=>{s.objects[0].bounds.x+=12;s.objects[0].rotation=15;});
      if(move.textRebuilds||move.textContentUpdates||move.textWidthUpdates||move.textPaintUpdates)throw Error('transform reshaped/repainted text');
      const width=await mutate('box resize',['text-0'],s=>{s.objects[0].bounds.width+=25;});
      if(width.textWidthUpdates!==1||width.textRebuilds||width.textContentUpdates)throw Error('box resize rebuilt unrelated typography');
      const content=await mutate('content',['text-27'],s=>{s.objects[27].text.runs[0].text='Changed text';});
      if(content.textContentUpdates!==1||content.textRebuilds||content.textWidthUpdates)throw Error('content updated more than one surface');
      await mutate('shape and attached line',['shape','connector'],s=>{s.objects.find(o=>o.id==='shape').bounds.x+=20;s.objects.find(o=>o.id==='connector').points[0].x+=20;});
      const none=await mutate('selection/revision',[],s=>{s.revision=41;});
      if(none.textRebuilds||none.textContentUpdates||none.textWidthUpdates||none.textPaintUpdates)throw Error('selection/revision invalidated painting');
      const background=await mutate('background',[],s=>{s.background='#172554';});
      if(background.textRebuilds||background.textContentUpdates||background.textWidthUpdates)throw Error('background invalidated text');
      const owned=view.renderStats();owned.lastChangedObjectIDs.push('injected');if(view.renderStats().lastChangedObjectIDs.includes('injected'))throw Error('metrics alias');
      return {objects:scene.objects.length,stats:view.renderStats(),records,resources:view.resourceCounts(),userAgent:navigator.userAgent};
    });
    assert.equal(result.objects,83);assert.deepEqual(errors,[]);
    fs.mkdirSync('output/powerup',{recursive:true});fs.writeFileSync('output/powerup/render-invalidation.json',JSON.stringify(result,null,2)+'\n');
    await page.screenshot({path:'output/powerup/render-invalidation.png'});
    console.log('PASS: 83-object production scene retains nodes; paint, transform, text, width and route updates touch only expected objects');
    console.log(JSON.stringify(result.records.map(({name,inputToPaintMs})=>({name,inputToPaintMs}))));
  }finally{await browser.close();await new Promise(resolve=>server.close(resolve));}
})().catch(error=>{console.error(error);process.exitCode=1;server.close();});
