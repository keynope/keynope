// Expanded acceptance for the contributor's documented recipes. Uses only MCP
// to author decks, then the real exported player and native PNG capture.
// Prerequisite: KEYNOPE_RECIPE_FIXTURES=/tmp/keynope-recipe-fixtures go test
// ./internal/app -run '^TestMCPRecipeFixtures$'. No production dependencies.
const fs=require('node:fs'),path=require('node:path'),os=require('node:os'),cp=require('node:child_process'),assert=require('node:assert/strict');
const pw=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const repo=path.resolve(__dirname,'../../..');
const python=process.env.KEYNOPE_PARITY_PYTHON||'python3';
const legacyScripts=process.env.KEYNOPE_LEGACY_SCRIPTS;
const output=fs.mkdtempSync(path.join(os.homedir(),'Library/Containers/sh.keynope.app/Data/tmp/keynope-recipes-'));
const fixture=process.env.KEYNOPE_RECIPE_FIXTURES||'/tmp/keynope-recipe-fixtures';
for(const name of ['transparent.png','white.png','dark-lines.png','qr.svg'])fs.copyFileSync(path.join(fixture,name),path.join(output,name));
let child,browser,server,buffer='',stderr='',sequence=0,version=1;const pending=new Map(),checks=[],slides=[],screenshots=[],measures={},oracles=[];
function check(name,pass,details){checks.push({name,pass:!!pass,details});console.log((pass?'PASS ':'FAIL ')+name);if(!pass)throw Error(name+' '+JSON.stringify(details));}
function rpc(method,params){const id=++sequence;return new Promise((resolve,reject)=>{const timer=setTimeout(()=>{pending.delete(id);reject(Error('MCP timeout '+stderr));},120000);pending.set(id,{resolve,reject,timer});child.stdin.write(JSON.stringify({jsonrpc:'2.0',id,method,params})+'\n')});}
async function tool(name,args={}){const result=await rpc('tools/call',{name,arguments:args});const text=result.content?.filter(c=>c.type==='text').map(c=>c.text).join('\n');if(result.isError)throw Error(name+': '+text);const value=result.structuredContent||JSON.parse(text);if(Number.isInteger(value.version))version=value.version;return value;}
const command=command=>tool('editor_command',{expected_version:version,command});
const batch=commands=>tool('editor_batch',{expected_version:version,commands});
const q=v=>new URLSearchParams(Object.entries(v).map(([k,v])=>[k,String(v)])).toString();
const text=(value,x,y,w,size=44,font='sans',h=6,extra={})=>({action:'add-element',kind:'text',elementData:{kind:'text',text:value,query:q({render:'truetype','modern-font':font,'modern-size':size,'modern-width':100,fg:'#ecf4ff',left:x,top:y,width:w,height:h,...extra})}});
async function addText(...args){const r=await command(text(...args));return r.created[0].id;}
async function shape(x,y,w,h,colour,extra={}){const r=await command({action:'add-element',kind:'shape',name:'square'}),id=r.created[0].id;const listed=await tool('deck_objects',{slides:[slides.length-1]});const entry=listed.objects.find(o=>o.id===id);await command({action:'update-element',element:entry.index,elementData:{kind:'shape',text:'[shape:square]',query:q({shape:'square',left:x,top:y,width:w,height:h,fg:colour,...extra})}});return id;}
async function slide(title,{effect='none',background='none'}={}){if(slides.length)await command({action:'add-slide'});const index=slides.length;slides.push({index,title,effect,background});await batch([{action:'update-slide',slideData:{bg:'#101828',bgSet:true,fg:'#ecf4ff',fgSet:true,headerFg:'#55ffff',headerFgSet:true,pageNumber:'hide',effect,effectSet:true,background,backgroundSet:true}},text(title,12,3,220,64,'sans',6)]);return index;}
async function style(id,property,propertyValue){return command({action:'set-selection-property',objectIds:[id],property,propertyValue});}
async function geometry(id,box){return command({action:'set-selection-geometry',objectIds:[id],geometry:box,geometryResize:true});}
async function image(file,box){const r=await tool('asset_import',{path:file,expected_version:version});const id=r.created[0].id;await geometry(id,box);await style(id,'image.alpha',100);return id;}
async function capture(index,name){await tool('slide_screenshot',{slide:index,width:960,path:name+'.png',overwrite:true});screenshots.push({index,title:slides[index].title,path:name+'.png'});}
function oracle(file,mode,columns,threshold,colour){if(!legacyScripts)return null;const name='oracle-'+oracles.length;const result=cp.spawnSync(python,[path.join(legacyScripts,'pixelart.py'),name,path.join(output,file),String(columns),mode,String(threshold),...(colour?[colour]:[])],{cwd:output,encoding:'utf8'});assert.equal(result.status,0,result.stderr);return fs.readFileSync(path.join(output,name+'-grid.txt'),'utf8').trim().split('\n').slice(1).map(l=>l.split(' '));}
async function main(){
 child=cp.spawn(path.join(repo,'bin/Keynope.app/Contents/MacOS/Keynope'),['--mcp','--workspace',output],{stdio:['pipe','pipe','pipe']});
 child.stderr.on('data',d=>stderr+=d);child.stdout.on('data',d=>{buffer+=d;let end;while((end=buffer.indexOf('\n'))>=0){const line=buffer.slice(0,end);buffer=buffer.slice(end+1);const msg=JSON.parse(line);const p=pending.get(msg.id);if(p){pending.delete(msg.id);clearTimeout(p.timer);msg.error?p.reject(Error(JSON.stringify(msg.error))):p.resolve(msg.result)}}});
 child.on('exit',(code,signal)=>{for(const p of pending.values()){clearTimeout(p.timer);p.reject(Error('MCP exited '+(signal||code)+' '+stderr))}pending.clear()});
 await rpc('initialize',{protocolVersion:'2025-06-18',clientInfo:{name:'recipe-acceptance',version:'1'},capabilities:{}});child.stdin.write(JSON.stringify({jsonrpc:'2.0',method:'notifications/initialized'})+'\n');await tool('deck_new',{expected_version:version,discard_unsaved:true});await tool('deck_authoring_guide',{topic:'all'});

 await slide('A command worth remembering');
 const body=await addText('Measure first. Build with confidence.\nKeep your changes small and reviewable.',20,20,205,48,'sans',9);
 const tagText=await addText('FIELD NOTES',18,16,60,28,'mono',3,{fg:'#101828'});
 const tag=await shape(16,16,63,3,'#55ffff',{'shape-offset-y':-.3});const panel=await shape(16,18,213,16,'#19283b',{'shape-stroke':'#516b8a','shape-stroke-width':2});
 await batch([{action:'move-object-layer',objectId:panel,kind:'back'},{action:'move-object-layer',objectId:tag,kind:'front'},{action:'move-object-layer',objectId:tagText,kind:'front'}]);
 measures.box=await tool('slide_measure',{slide:0,panel_id:panel});check('tagged box content is above the panel',!measures.box.behind_panel.includes(body));await capture(0,'01-tagged-box');

 await slide('Read clearly. Keep the atmosphere.',{background:'constellation'});
 const reading=await addText('Good workshops leave room to think.\n\nGive people a clear instruction, a useful example,\nand enough breathing space to get started.',20,18,205,48,'sans',19);
 const readingPanel=await shape(12,14,221,29,'#101828',{'modern-opacity':.92,'shape-stroke':'#2a3654','shape-stroke-width':2,'shape-offset-y':.5});await command({action:'move-object-layer',objectId:readingPanel,kind:'back'});
 measures.reading=await tool('slide_measure',{slide:1,panel_id:readingPanel});check('reading panel contains text and leaves heading outside',!measures.reading.panel_outside.includes(reading)&&!measures.reading.behind_panel.includes(reading),measures.reading);await capture(1,'02-reading-panel');

 await slide('One diagram. Three moments.');
 const diagram='┌────────┐    ┌────────┐    ┌────────┐\n│  PLAN  │ →  │ BUILD  │ →  │ REVIEW │\n└────────┘    └────────┘    └────────┘';
 const layers=await tool('diagram_layers',{text:diagram,layers:[{name:'plan',rectangles:[{row_start:0,row_end:2,column_start:0,column_end:12}]},{name:'build',rectangles:[{row_start:0,row_end:2,column_start:13,column_end:25}]},{name:'review',rectangles:[{row_start:0,row_end:2,column_start:26,column_end:37}]}]});const layerIDs=[];
 for(let i=0;i<layers.layers.length;i++)layerIDs.push(await addText(layers.layers[i].text,16,24,215,58,'mono',13,{fg:['#55ffff','#ffffaa','#ff88bb'][i]}));
 await command({action:'set-slide-build',buildData:{version:1,transition:{effect:'fade',durationMs:300},steps:layerIDs.map((id,i)=>({targetIds:[id],effect:['appear','wipe','zoom'][i],direction:'right',durationMs:250,advance:'click'}))}});await capture(2,'03-layered-diagram');

 for(const mode of ['alpha','dark','nonwhite']){
  await slide('Pixel artwork / '+mode);const file=mode==='alpha'?'transparent.png':mode==='dark'?'dark-lines.png':'white.png';
  for(let i=0;i<3;i++){
   const threshold=[.12,.4,.8][i],columns=30;
   const converted=await tool('image_pixelart',{path:file,columns,mode,threshold,...(i===2?{colour:'#55ffff'}:{})});
   check('native pixel grid '+mode+' '+threshold,converted.grid.length>0&&converted.grid.every(row=>row.length===columns));
   if(i===2)check('native pixel tint '+mode,converted.grid.flat().every(c=>c==='.'||c==='#55ffff'));
   const old=oracle(file,mode,columns,threshold,i===2?'#55ffff':null);
   if(old){assert.equal(converted.grid.length,old.length);assert.equal(converted.grid[0].length,old[0].length);
    let intersection=0,union=0;for(let r=0;r<old.length;r++)for(let c=0;c<columns;c++){const a=old[r][c]!=='.',b=converted.grid[r][c]!=='.';if(a&&b)intersection++;if(a||b)union++;}
    const iou=union?intersection/union:1;oracles.push({mode,threshold,iou,rows:old.length});check('pixel occupancy parity '+mode+' '+threshold,iou>=.9,{iou});
   }
   const element=converted.element,values=new URLSearchParams(element.query);for(const [key,value]of Object.entries({left:12+i*78,top:17,width:70,height:28,'modern-size':23}))values.set(key,String(value));element.query=values.toString();const r=await command({action:'add-element',kind:'text',elementData:element});await command({action:'set-scene-text',objectId:r.created[0].id,textRuns:converted.text_runs});await addText('threshold '+threshold+(i===2?' / tint':''),12+i*78,46,70,24,'mono',3);
  }await capture(slides.length-1,'04-pixels-'+mode);
 }

 await slide('Words, shapes and character');const fontIDs=[];
 for(const [i,font]of ['sans','mono','c64'].entries()){
  for(const [j,width]of [60,100,160].entries()){
   const value=font==='c64'?'TYPE WITH CHARACTER':'Type with character';const id=await addText(value,12+j*78,14+i*10,72,32,font,5,{'modern-width':width});fontIDs.push(id);await addText(font+' / '+width+'%',12+j*78,19+i*10,72,22,'mono',2);
  }
 }
 const mixed=await addText('C64 / café → ✓ “quotes”',12,47,218,38,'c64',5);
 await command({action:'set-scene-text',objectId:mixed,textRuns:[{text:'C64 / '},{text:'café → ✓ “quotes”',fontId:'mono'}]});await capture(slides.length-1,'05-fonts');

 await slide('Lists and code, with room to read.');
 for(const [kind,x,width,value]of [['bullet',14,96,'Plan the structure\nMeasure the layout\nReview the result'],['code',127,105,'#!/bin/bash\n\necho "Hello workshop"']]){
  const added=await command({action:'add-element',kind});const id=added.created[0].id;const listed=await tool('deck_objects',{slides:[slides.length-1]});const object=listed.objects.find(o=>o.id===id);await command({action:'update-element',element:object.index,elementData:{kind,text:value,query:q({render:'truetype','modern-font':kind==='code'?'mono':'sans','modern-size':42,'modern-line-height':1.4,left:x,top:17,width,height:23,fg:'#ecf4ff'})}});
 }await capture(slides.length-1,'05a-lists-code');

 await slide('Shape, colour, light.');
 for(const [i,name]of ['square','rounded','pill','circle','triangle','diamond','hexagon','arrow-right','chevron','star','burst','cloud','database','document','callout','speech-bubble','brace','label'].entries()){
  const x=12+(i%6)*38,y=13+Math.floor(i/6)*13;const inserted=await command({action:'add-element',kind:'shape',name});const id=inserted.created[0].id;const list=await tool('deck_objects',{slides:[slides.length-1]});const e=list.objects.find(o=>o.id===id);await command({action:'update-element',element:e.index,elementData:{kind:'shape',text:'[shape:'+name+']',query:q({shape:name,left:x,top:y,width:28,height:8,fg:['#55ffff','#ff88bb','#ffffaa'][i%3],'shape-stroke':'#ffffff','shape-stroke-width':1})}});await addText(name,x-2,y+9,35,17,'mono',2);
 }await capture(slides.length-1,'05b-shapes');

 await slide('Gradients belong to the words.');
 for(const [i,direction]of ['horizontal','vertical','diagonal','radial','reflected','diamond','angle'].entries()){
  const y=12+i*5;const id=await addText(direction.toUpperCase()+'  /  KEYNOPE',18,y,210,42,'c64',4);await style(id,'paint.gradientStart','#55ffff');await style(id,'paint.gradientEnd','#ff55aa');await style(id,'paint.gradientDirection',direction);if(i===0){await style(id,'paint.shadow','solid');await style(id,'paint.shadowColor','#000033');await style(id,'paint.shadowX',1);await style(id,'paint.shadowY',1)}
 }await capture(slides.length-1,'05c-gradients');

 await slide('Images, in every Retro treatment.');
 for(const [i,glyph]of ['blocks','braille','ascii','dense'].entries()){
  const x=13+(i%2)*119,y=13+Math.floor(i/2)*19;const id=await image('transparent.png',{x,y,width:100,height:14});await style(id,'image.style','retro');await style(id,'image.glyph',glyph);await style(id,'image.tint','#55ffff');await addText(glyph,x,y+15,100,22,'mono',2);
 }await capture(slides.length-1,'05d-retro-images');

 await slide('A richer table');await command({action:'insert-table',rows:3,cols:3});let objects=await tool('deck_objects',{slides:[slides.length-1]});const tid=objects.objects.find(o=>o.kind==='table').id;
 const table={version:1,rows:[{height:1},{height:1},{height:1}],columns:[{width:2},{width:3},{width:2}],cells:[[{text:'Stage'},{text:'A useful question',colSpan:2},{hidden:true}],[{text:'Explore'},{text:'What should change?',runs:[{text:'What should ',color:'#55ffff'},{text:'change?',bold:true}]},{text:'Listen'}],[{text:'Build'},{text:'Make it tangible.'},{text:'Review'}]],style:{header:true,banded:true,headerFill:'#24506a',bandFill:'#1c2d42',cell:{fill:'#131f2e',color:'#ffffff',fontId:'sans',fontSize:40,fontWidth:100,lineHeight:1.25,padding:14,borderColor:'#516b8a',borderWidth:1,vertical:'middle'}}};
 const tm=await tool('table_measure',{table,box_width:219});await command({action:'set-table',objectId:tid,tableData:{...table,rows:tm.row_heights.map(height=>({height}))}});await geometry(tid,{x:13,y:18,width:219,height:Math.max(tm.height,23)});await capture(slides.length-1,'06-table');

 await slide('Share a link. Keep it scannable.');await image('qr.svg',{x:91,y:15,width:63,height:25.61143});await addText('https://keynope.sh/join/Recipe01',25,44,195,37,'mono',4);await capture(slides.length-1,'07-qr');

 // Controlled web-page fixture: the same screenshot/import recipe without a
 // third-party page changing or requiring login during a regression test.
 browser=await pw.chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});const sourcePage=await browser.newPage({viewport:{width:1600,height:1000}});
 const http=require('node:http');server=http.createServer((req,res)=>{res.setHeader('Content-Type','text/html');res.end('<!doctype html><style>body{margin:0;background:#08283b;color:white;font:40px system-ui;padding:90px}h1{font-size:100px}strong{color:#55ffff}</style><p>KEYNOPE / LOCAL TEST PAGE</p><h1>A page worth <strong>sharing.</strong></h1><p>Browser screenshot → portable embedded slide asset</p>')});await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));await sourcePage.goto('http://127.0.0.1:'+server.address().port);await sourcePage.screenshot({path:path.join(output,'webpage.png')});await sourcePage.close();server.close();server=null;
 await slide('A webpage, captured and portable.');await image('webpage.png',{x:21,y:14,width:203,height:33.02});await capture(slides.length-1,'08-webpage');

 await slide('Motion, without distraction.');const fire=await tool('animation_create',{path:'fireworks.gif',expected_version:version,animation:{width:480,height:270,seconds:4,delay_ms:80,rockets:15,salvo_every:1.25,burst_min:150,burst_max:300,colours:['#55ffff','#ff88bb','#ffffaa'],seed:1729,avoid:{x:10,y:0,width:225,height:16}}});check('custom fireworks generated',fire.frames===50&&fire.rockets===15,fire);await image('fireworks.gif',{x:12,y:14,width:221,height:35.8});await capture(slides.length-1,'09-fireworks');

 const effectNames=['matrix','stars','plasma','glitch','digital-snow','radar','neural','circuit','data-storm','flame','warp','scanline','fireworks','explosion'];
 for(const effect of effectNames){await slide('Effect / '+effect,{effect});await addText('An atmosphere behind the story.',12,14,221,38);await capture(slides.length-1,'effect-'+effect);}
 const bgNames=['soft-plasma','aurora','topography','waves','mesh','constellation','ribbons','diagonal-flow','blueprint'];
 for(const background of bgNames){await slide('Background / '+background,{background});const p=await shape(12,16,221,30,'#101828',{'modern-opacity':.85});await command({action:'move-object-layer',objectId:p,kind:'back'});await addText('A quiet panel protects the words.\nThe background carries the atmosphere.',20,22,202,46,'sans',15);await capture(slides.length-1,'background-'+background);}
 await tool('deck_save',{path:'recipes.md',expected_version:version});await tool('deck_export',{path:'recipes.html',format:'html',expected_version:version});await tool('deck_validate');

 const page=await browser.newPage({viewport:{width:960,height:540}}),errors=[];page.on('pageerror',e=>errors.push(e.message));await page.goto('file://'+path.join(output,'recipes.html'));await page.waitForFunction(()=>typeof KeynopeScene!=='undefined'&&typeof KeynopeBuilds!=='undefined');
 const exported=await page.evaluate(()=>JSON.parse(document.getElementById('keynope-data').textContent));check('all recipe slides survive export',exported.pages.length===slides.length,{slides:slides.length,pages:exported.pages.length});
 const transitionResult=await page.evaluate(async()=>{
  const host=document.createElement('div');Object.assign(host.style,{position:'fixed',inset:0,width:'960px',height:'540px',zIndex:9999});const incoming=document.createElement('div');Object.assign(incoming.style,{position:'absolute',inset:0,background:'#205e9b',color:'white',font:'64px sans-serif'});incoming.textContent='The next idea.';host.append(incoming);document.body.append(host);const list=[];
  for(const effect of ['cut','fade','push','wipe','dip-black','dip-white','tv-switch'])for(const direction of ['left','right','up','down']){
   const transition=KeynopeBuilds.slideTransition(host,incoming,{reducedMotion:()=>false});transition.hold(transition.snapshot());transition.play({effect,direction,durationMs:120});await new Promise(r=>setTimeout(r,230));list.push({effect,direction,settled:!transition.active()&&host.children.length===1&&incoming.style.visibility!=='hidden'});transition.cancel();
  }
  for(const effect of ['fade','push','wipe','dip-black','dip-white','tv-switch']){const transition=KeynopeBuilds.slideTransition(host,incoming,{reducedMotion:()=>true});transition.hold(transition.snapshot());transition.play({effect,durationMs:800});list.push({effect,reduced:true,settled:!transition.active()&&host.children.length===1});transition.cancel();}
  host.remove();return list;
 });check('every transition and direction settles; reduced motion bypasses animation',transitionResult.every(r=>r.settled),transitionResult);
 const buildResult=await page.evaluate(async()=>{
  const host=document.createElement('div');host.style.position='fixed';host.style.inset='0';host.style.zIndex='9999';document.body.append(host);const input=JSON.parse(document.getElementById('keynope-data').textContent).pages[2].scene;const view=KeynopeScene.mount(host,input);await view.ready;const controller=KeynopeBuilds.controller(view.element,input.build,{transitions:false});const states=[];
  const state=()=>input.build.steps.map(s=>s.targetIds.map(id=>[...view.element.querySelectorAll('[data-object-id]')].filter(n=>n.dataset.objectId===id).every(n=>n.style.visibility!=='hidden')));
  controller.start();states.push(state());for(let i=0;i<3;i++){controller.next();await new Promise(r=>setTimeout(r,310));states.push(state())}controller.previous();states.push(state());controller.dispose();view.dispose();host.remove();return states;
 });check('layered reveals stay aligned and advance independently',JSON.stringify(buildResult)===JSON.stringify([[[false],[false],[false]],[[true],[false],[false]],[[true],[true],[false]],[[true],[true],[true]],[[true],[true],[false]]]),buildResult);
 const moreBuilds=await page.evaluate(async()=>{const cases=[];for(const effect of ['appear','fade','wipe','zoom'])for(const direction of ['left','right','up','down']){const root=document.createElement('div');const node=document.createElement('div');node.dataset.objectId='x';node.textContent='Reveal';root.append(node);document.body.append(root);const control=KeynopeBuilds.controller(root,{version:1,steps:[{targetIds:['x'],effect,direction,durationMs:30,advance:'after',delayMs:20}]},{transitions:false});control.start();await new Promise(r=>setTimeout(r,120));cases.push({effect,direction,visible:node.style.visibility!=='hidden',step:control.snapshot().step});control.dispose();root.remove()}return cases;});check('every build effect/direction supports automatic reveal',moreBuilds.every(r=>r.visible&&r.step===1),moreBuilds);
 await page.evaluate(()=>window.keynopePrepareSnapshot(2));await page.screenshot({path:path.join(output,'browser-layered.png')});
 const glyphs=await page.evaluate(async()=>{await document.fonts.ready;const c=document.createElement('canvas').getContext('2d');return ['KeynopeC64','KeynopeModern','KeynopeModernMono'].map(font=>{c.font='40px '+font;return {font,loaded:document.fonts.check('40px '+font),asciiWidth:c.measureText('KEYNOPE').width,unicodeWidth:c.measureText('café → ✓').width}})});check('all three bundled font families load',glyphs.every(g=>g.loaded&&g.asciiWidth>0),glyphs);
 check('exported browser reports no script errors',errors.length===0,errors);await page.close();

 const contact=await browser.newPage({viewport:{width:1440,height:Math.ceil(screenshots.length/3)*300}});await contact.setContent('<style>body{margin:0;background:#20262e;font:16px system-ui;color:white;display:grid;grid-template-columns:repeat(3,1fr);gap:6px}figure{margin:0}img{width:100%;display:block}figcaption{height:28px;padding:4px}</style>'+screenshots.map(s=>'<figure><img src="data:image/png;base64,'+fs.readFileSync(path.join(output,s.path)).toString('base64')+'"><figcaption>'+s.title+'</figcaption></figure>').join(''));await contact.screenshot({path:path.join(output,'contact-sheet.png'),fullPage:true});await contact.close();
 fs.writeFileSync(path.join(output,'results.json'),JSON.stringify({output,checks,slides,oracles,measures,transitions:transitionResult,builds:buildResult},null,2));console.log('EVIDENCE '+output);
}
main().catch(error=>{console.error(error);checks.push({name:'run completion',pass:false,error:error.stack});fs.writeFileSync(path.join(output,'failure.json'),JSON.stringify({output,checks,stderr,error:error.stack},null,2));console.log('EVIDENCE '+output);process.exitCode=1}).finally(async()=>{if(browser)await browser.close();server?.close();child?.stdin.end();setTimeout(()=>child?.kill(),1000).unref()});
