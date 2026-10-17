// Deterministic queue ownership regression using the actual editor functions
// and workspace geometry. No UI test hooks or browser timing assumptions.
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('internal/app/main.go','utf8');
const names=['canvasGeometryContext','canvasGeometryContextCurrent','canvasGeometryGestureCurrent','canvasArrangeTargets','canvasArrangeAction','canvasMoveSnapshot','canvasArrangeUnits','rotateCanvasSelection','resizeCanvasSelection','distributeCanvasSelection','matchCanvasSelectionSize','alignCanvasSceneObjects','setCanvasObjectOrigin','preserveCanvasPositionOverride','showEditorCommandError'];
const functions=names.map(name=>{
 const match=source.match(new RegExp('^  (?:async )?function '+name+'\\([^]*?^  }','m'));
 assert(match,'Missing production function '+name);return match[0];
}).join('\n');
require('../web/workspace.js');const workspace=globalThis.KeynopeWorkspace;
function fixture(master){
 const elements=[{id:'a',kind:'text',text:'A',query:'left=20&top=10&width=20&height=5'},{id:'b',kind:'text',text:'B',query:'left=70&top=20&width=30&height=8'},{id:'c',kind:'text',text:'C',query:'left=140&top=30&width=15&height=6'}];
 const bounds=new Map(elements.map((e,index)=>{const q=new URLSearchParams(e.query),x=+q.get('left'),y=+q.get('top');return[index,{minX:x,minY:y,maxX:x+(+q.get('width')),maxY:y+(+q.get('height')),rotation:0}];}));
 const calls=[],pending=[],status={textContent:''};
 const context={URLSearchParams,KeynopeWorkspace:workspace,canvasPhysicalGeometry:()=>({width:1920,height:1080}),editorDocumentEpoch:1,editorState:{version:10,mutationRevision:3,current:0,masterMode:master,editingGroup:'',slides:[{elements}]},deck:{cols:245,rows:56,pages:[{scene:{objects:elements.map(e=>({id:e.id,kind:'text'}))}}]},pageIndex:0,canvasAlignmentReference:'selection',editorStatus:{},editorCommandErrorText:{},editorCommandErrorNotice:{hidden:true},status,
  canvasElementAt:index=>context.editorState.slides[0].elements[index],canvasSelectedIndices:()=>[0,1,2],canvasGroupMembers:index=>[index],canvasCurrentBounds:()=>bounds,canvasUnionBounds:indices=>{const boxes=indices.map(i=>bounds.get(i));return {minX:Math.min(...boxes.map(b=>b.minX)),minY:Math.min(...boxes.map(b=>b.minY)),maxX:Math.max(...boxes.map(b=>b.maxX)),maxY:Math.max(...boxes.map(b=>b.maxY))};},editorElementIndexByID:id=>context.editorState.slides[0].elements.findIndex(e=>e.id===id),clearCanvasPlacementAnchors:q=>{for(const key of ['left','top','right','bottom','align','valign'])q.delete(key);},
  queueEditorOperation:fn=>new Promise((resolve,reject)=>pending.push(()=>Promise.resolve().then(fn).then(resolve,reject))),performEditorAction:async action=>calls.push(action)};
 vm.createContext(context);vm.runInContext(functions,context);
 return {context,calls,pending,status,bounds,drain:async()=>{for(const run of pending.splice(0))await run();}};
}
const operations={align:c=>c.alignCanvasSceneObjects(0,'top',true),distribute:c=>c.distributeCanvasSelection(0,'horizontal'),match:c=>c.matchCanvasSelectionSize(0,'width',c.status),rotate:c=>c.rotateCanvasSelection(0,5),resize:c=>c.resizeCanvasSelection(0,1.05)};
(async()=>{
 for(const master of [false,true])for(const [name,run]of Object.entries(operations)){
  const f=fixture(master),promise=Promise.resolve(run(f.context));
  assert.equal(f.pending.length,1,name+' did not enqueue');
  // Model a selection/timer/private result response while the action is held.
  f.context.editorState.version+=4;f.context.editorState.selected=2;
  await f.drain();await promise;assert.equal(f.calls.length,1,name+' silently dropped after non-mutating traffic');
  const action=f.calls[0];assert.equal(action.sceneRevision,14);assert.equal(action.mutationRevision,3);assert.equal(action.sceneMaster,master);assert.equal(action.slide,0);
  assert(action.elementsData.length>0);for(let i=0;i<action.elementsData.length;i++)assert.equal(f.context.canvasElementAt(action.elementIndices[i]).id,action.elementsData[i].id);
  assert.equal(f.context.editorState.selected,2,'Arrange stole selection');
 }
 for(const master of [false,true])for(const [name,run]of Object.entries(operations))for(const change of ['mutation','epoch','slide','mode','group','object','bounds']){
  const f=fixture(master),promise=Promise.resolve(run(f.context)).then(()=>null,error=>error);
  const c=f.context;
  if(change==='mutation')c.editorState.mutationRevision++;
  if(change==='epoch')c.editorDocumentEpoch++;
  if(change==='slide')c.editorState.current++;
  if(change==='mode')c.editorState.masterMode=!master;
  if(change==='group')c.editorState.editingGroup='new-group';
  if(change==='object')c.editorState.slides[0].elements[0]={...c.editorState.slides[0].elements[0],text:'Changed'};
  if(change==='bounds')f.bounds.set(0,{...f.bounds.get(0),minX:21});
  await f.drain();const error=await promise;assert.equal(f.calls.length,0,name+' applied stale '+change);
  if(name==='align'||name==='distribute')assert.equal(c.editorCommandErrorNotice.hidden,change==='epoch',name+' wrong error visibility for '+change);
  else if(name==='match')assert.match(f.status.textContent,/changed/);
  else assert.match(error?.message||'',/changed/);
 }
 console.log('PASS: five production Arrange transforms, normal/master, non-mutating queue traffic, seven stale ownership cases and visible rejection.');
})().catch(error=>{console.error(error);process.exitCode=1;});
