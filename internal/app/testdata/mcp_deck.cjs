// End-to-end authoring acceptance. EVERY deck read/write/render goes through
// real MCP JSON-RPC over stdio. The client only seeds a licensed image fixture
// and writes its protocol transcript; it never writes or edits deck Markdown.
const fs=require('node:fs'),os=require('node:os'),path=require('node:path'),cp=require('node:child_process'),assert=require('node:assert/strict');
const repo=path.resolve(__dirname,'../../..'),engine=process.env.KEYNOPE_MCP_ENGINE||path.join(repo,'bin/Keynope.app/Contents/MacOS/Keynope');
// The signed app can use its own temporary container without a folder panel.
const appTemp=path.join(os.homedir(),'Library/Containers/sh.keynope.app/Data/tmp');
const tempRoot=!process.env.KEYNOPE_MCP_ENGINE&&fs.existsSync(appTemp)?appTemp:os.tmpdir();
const output=process.env.KEYNOPE_MCP_EVIDENCE||fs.mkdtempSync(path.join(tempRoot,'keynope-mcp-deck-'));
fs.mkdirSync(output,{recursive:true});
fs.copyFileSync(path.join(repo,'assets/KeynopeAssets.xcassets/KeynopeApp.appiconset/icon_512x512.png'),path.join(output,'source.png'));
const child=cp.spawn(engine,['--mcp','--workspace',output],{env:{...process.env,KEYNOPE_RENDERER:path.join(repo,'bin/Keynope.app/Contents/MacOS/Keynope')},stdio:['pipe','pipe','pipe']});
let sequence=0,buffer='',stderr='',version=1;const pending=new Map(),transcript=[];
child.stdout.on('data',data=>{buffer+=data;for(;;){const newline=buffer.indexOf('\n');if(newline<0)break;const line=buffer.slice(0,newline);buffer=buffer.slice(newline+1);if(!line.trim())continue;let message;try{message=JSON.parse(line);}catch{throw Error('Non-protocol stdout: '+line)}const job=pending.get(message.id);if(job){pending.delete(message.id);clearTimeout(job.timeout);message.error?job.reject(Error(JSON.stringify(message.error))):job.resolve(message.result);}}});
child.stderr.on('data',data=>stderr+=data);child.on('exit',(code,signal)=>{for(const job of pending.values()){clearTimeout(job.timeout);job.reject(Error('MCP engine exited '+(signal||code)+': '+stderr))}pending.clear();});
function rpc(method,params={}){const id=++sequence;return new Promise((resolve,reject)=>{const timeout=setTimeout(()=>{pending.delete(id);reject(Error(method+' timed out: '+stderr));child.kill();},150000);pending.set(id,{resolve,reject,timeout});child.stdin.write(JSON.stringify({jsonrpc:'2.0',id,method,params})+'\n');});}
async function tool(name,args={}){const response=await rpc('tools/call',{name,arguments:args});const text=response.content?.filter(item=>item.type==='text').map(item=>item.text).join('\n');if(response.isError)throw Error(name+': '+text);const value=response.structuredContent||JSON.parse(text);if(Number.isInteger(value.version))version=value.version;transcript.push({tool:name,arguments:args,result:JSON.parse(JSON.stringify(value,(key,value)=>typeof value==='string'&&value.length>1000?'[large payload]':value))});return value;}
const command=command=>tool('editor_command',{expected_version:version,command});
const query=values=>new URLSearchParams(Object.entries(values).map(([k,v])=>[k,String(v)])).toString();
const inspect=slide=>tool('slide_inspect',{slide});
async function text(content,left,top,width=216,height=7,size=64,color='#e6edf8',font='sans'){
 const state=await command({action:'add-element',kind:'text',elementData:{kind:'text',text:content,query:query({left,top,width,height,'text-box':1,render:'truetype','modern-font':font,'modern-size':size,'modern-width':100,fg:color})}});
 const page=await inspect(state.current_slide);return page.authored.elements[state.selected_element];
}
async function update(element,values){return command({action:'update-element',elementData:{...element,query:query({...Object.fromEntries(new URLSearchParams(element.query||'')),...values})}});}
async function shape(name,left,top,width,height,color){const state=await command({action:'add-element',kind:'shape',name});const page=await inspect(state.current_slide),element=page.authored.elements[state.selected_element];await update(element,{left,top,width,height,fg:color});return {...element,query:query({left,top,width,height,fg:color,shape:name})};}
async function style(id,property,value){return command({action:'set-selection-property',objectIds:[id],property,propertyValue:value});}
async function slide(){await command({action:'add-slide'});await background();}
async function background(){await command({action:'update-slide',slideData:{bg:'#0b1220',bgSet:true,fg:'#e6edf8',fgSet:true,headerFg:'#78dce8',headerFgSet:true,effect:'none',effectSet:true,background:'none',backgroundSet:true,pageNumber:'hide'}});}
async function screenshot(slide){return tool('slide_screenshot',{slide,path:'slide-'+(slide+1)+'.png',width:1280,overwrite:true});}
(async()=>{try{
 await rpc('initialize',{protocolVersion:'2025-06-18',clientInfo:{name:'Keynope authoring acceptance',version:'1'},capabilities:{}});child.stdin.write(JSON.stringify({jsonrpc:'2.0',method:'notifications/initialized'})+'\n');
 const listed=await rpc('tools/list');assert.ok(listed.tools.some(tool=>tool.name==='editor_command'));assert.ok(listed.tools.some(tool=>tool.name==='slide_screenshot'));
 const resources=await rpc('resources/list');assert.equal(resources.resources.length,3);await rpc('resources/read',{uri:'keynope://commands'});
 await tool('deck_inspect');for(const topic of ['action','element','properties','table','chart','activities','build','shapes','fonts'])await tool('deck_schema',{topic});
 const emoji=await tool('emoji_catalog',{query:'smile',limit:5});assert.ok(emoji.items.length);
 const fonts=await tool('fonts_installed',{limit:5});assert.ok(fonts.fonts.length);const imported=await tool('font_use_installed',{font:fonts.fonts[0].postScriptName,expected_version:version});assert.ok(imported.font_id);
 await tool('deck_new',{expected_version:version,discard_unsaved:true});await background();
 let layoutState=await command({action:'add-layout',name:'MCP reusable layout'});const layout=layoutState.masters.layouts.find(l=>l.name==='MCP reusable layout');
 await command({action:'update-layout',kind:layout.id,slideData:{bg:'48;2;11;18;32',bgSet:true,elements:[{kind:'text',text:'Reusable, editable masters.',query:query({left:16,top:20,width:212,height:8,render:'truetype','modern-font':'sans','modern-size':100,fg:'#78dce8'})}]}});
 const masterIndex=layoutState.masters.layouts.findIndex(l=>l.id===layout.id)+1;const master=await tool('slide_inspect',{slide:masterIndex,master:true});assert.equal(master.authored.elements.length,1);await tool('slide_screenshot',{slide:masterIndex,master:true,path:'master.png',width:1280});
 await text('KEYNOPE / AUTOMATION',16,5,210,4,35,'#78dce8','mono');
 const hero=await text('Made through MCP.',16,15,216,9,130,'#ffffff');
 await style(hero.id,'paint.gradientStart','#55aaff');await style(hero.id,'paint.gradientEnd','#ff80bf');await style(hero.id,'paint.gradientDirection','horizontal');await style(hero.id,'paint.glowRadius',5);
 await text('Real editing tools. One portable deck.',17,29,212,6,58,'#b8c7e0');
 await text('Native commands • editable text • visual inspection',17,42,212,4,35,'#78dce8','mono');
 const copied=await tool('selection_copy');assert.ok(copied.clipboard.elements.length);
 await command({action:'update-slide-notes',notes:'Built entirely through MCP. No Markdown was written by the client.'});
 await screenshot(0);

 await slide();await text('Colour, light and motion.',16,6,216,7,94);
 const glow=await shape('circle',20,20,48,17,'#ff477e');await style(glow.id,'paint.glowRadius',14);await style(glow.id,'paint.glowColour','#ff477e');
 const blur=await shape('square',98,20,46,17,'#55aaff');await style(blur.id,'paint.blur',12);
 const stroke=await shape('diamond',174,20,48,17,'#78dce8');await style(stroke.id,'paint.strokeColor','#ffffff');await style(stroke.id,'paint.strokeWidth',5);await style(stroke.id,'paint.strokePosition','outside');
 await command({action:'select-elements',objectIds:[glow.id,blur.id,stroke.id]});await command({action:'group-elements'});let grouped=await inspect(1);assert.equal(new Set(grouped.authored.elements.filter(e=>[glow.id,blur.id,stroke.id].includes(e.id)).map(e=>new URLSearchParams(e.query).get('group'))).size,1);await command({action:'ungroup-elements'});
 await text('GLOW',20,41,56,4,34,'#ff80bf','mono');await text('BLUR',98,41,56,4,34,'#80baff','mono');await text('OUTLINE',172,41,56,4,34,'#78dce8','mono');
 await command({action:'set-slide-build',buildData:{version:1,transition:{effect:'dip-black',durationMs:700}}});await screenshot(1);

 await slide();await text('Data, not decoration.',16,6,216,7,96);
 await command({action:'insert-chart',kind:'bar',chartData:{version:1,type:'bar',title:'Workshop momentum',categories:['Explore','Build','Review'],series:[{name:'Confidence',values:[3,8,6],color:'#78dce8'}],options:{fontId:'sans',legend:false,dataLabels:true,gridColor:'#283650'}}});
 let page=await inspect(2),chart=page.authored.elements.find(e=>e.kind==='chart');await update(chart,{left:14,top:18,width:110,height:29});
 await command({action:'insert-table',rows:3,cols:2});page=await inspect(2);const table=page.authored.elements.find(e=>e.kind==='table');
 await command({action:'set-table',objectId:table.id,tableData:{version:1,rows:[{height:1},{height:1},{height:1}],columns:[{width:2},{width:1}],style:{header:true,banded:true,headerFill:'#17344a',bandFill:'#142034',cell:{fontId:'sans',fontSize:38,color:'#e6edf8',fill:'#101a2b',padding:14,borderColor:'#314159',borderWidth:1}},cells:[[{text:'Stage'},{text:'Minutes'}],[{text:'Build together'},{text:'20'}],[{text:'Review & reflect'},{text:'10'}]]}});await update(table,{left:139,top:21,width:90,height:22});await screenshot(2);

 await slide();await text('A workshop, ready to run.',16,6,216,8,100);
 await text('How confident are we now?',16,23,208,7,72,'#78dce8');
 await text('Join. Reflect. Share your perspective.',16,36,208,6,46,'#b8c7e0');
 await command({action:'set-engagement',engagementData:{kind:'pulse',prompt:'How confident are we now?',options:['Not at all','Hardly','Meh','Somewhat','Quite','Very'],timerSeconds:120,named:true}});
 page=await inspect(3);assert.equal(page.authored.engagement.kind,'pulse');await screenshot(3);

 await slide();await text('Same image. Different energy.',16,6,218,7,94);
 await tool('asset_import',{path:'source.png',expected_version:version});let state=await tool('editor_state');let image=state.slides[4].elements.find(e=>e.kind==='image');await update(image,{left:24,top:18,width:72,height:28,'image-fit':'contain'});
 await command({action:'select-elements',objectIds:[image.id],elementIndices:[state.slides[4].elements.findIndex(e=>e.id===image.id)]});await command({action:'duplicate-element'});page=await inspect(4);let second=page.authored.elements.filter(e=>e.kind==='image').find(e=>e.id!==image.id);await update(second,{left:146,top:18,width:72,height:28,'image-style':'retro',glyph:'blocks','image-fit':'contain'});await style(second.id,'paint.colourEffect','duotone');await style(second.id,'paint.effectShadows','#0b1220');await style(second.id,'paint.effectHighlights','#78dce8');
 await text('MODERN',29,47,75,4,32,'#b8c7e0','mono');await text('RETRO + DUOTONE',142,47,90,4,32,'#78dce8','mono');await screenshot(4);

 await tool('editor_batch',{expected_version:version,commands:[{action:'undo'},{action:'redo'}]});
 const validation=await tool('deck_validate');assert.equal(validation.valid,true);
 const review=await tool('font_export_review',{format:'save'});assert.equal(review.confirmation_required,false);
 await tool('deck_save',{path:'test.md',expected_version:version,overwrite:true});
 await tool('deck_export',{path:'test.html',format:'html',expected_version:version,overwrite:true});
 await tool('deck_export',{path:'test.pptx',format:'pptx',expected_version:version,overwrite:true});
 await tool('deck_export',{path:'test.pdf',format:'pdf',expected_version:version,overwrite:true});
 const savedVersion=version;await tool('deck_open',{path:'test.md',expected_version:version});assert.ok(version>savedVersion);const reopened=await tool('deck_inspect');assert.equal(reopened.slides.length,5);assert.equal(reopened.dirty,false);
 await screenshot(0);
 // Negative protocol calls: stale revision and path escape must be tool errors.
 for(const [name,args]of [['editor_command',{expected_version:1,command:{action:'delete-slide'}}],['deck_save',{expected_version:version,path:'../escape.md'}]]){const result=await rpc('tools/call',{name,arguments:args});assert.equal(result.isError,true);}
 console.log('PASS: '+transcript.length+' MCP calls; native screenshots, authoring, effects, charts, tables, image styles, activity, undo/redo, exports and save/reopen.\n'+output);
 }finally{fs.writeFileSync(path.join(output,'protocol.json'),JSON.stringify(transcript,null,2));fs.writeFileSync(path.join(output,'engine.log'),stderr);child.stdin.end();setTimeout(()=>child.kill(),2000).unref();}
})().catch(error=>{console.error(error);process.exitCode=1;});
