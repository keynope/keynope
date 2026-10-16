// Run the production frame/selection functions with a deliberately suspended
// animation-frame queue. Model IDs can reorder before native paint arrives.
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('main.go','utf8');
const between=(start,end)=>{const a=source.indexOf(start),b=source.indexOf(end,a);assert(a>=0&&b>a);return source.slice(a,b);};
const context=vm.createContext({console});
vm.runInContext('window=globalThis;',context);
vm.runInContext(fs.readFileSync('web/workspace.js','utf8'),context);
vm.runInContext(`
let keynopeAppSurface=true,editorState={current:0,selected:0,selection:[0],slides:[{elements:[{id:'a'},{id:'b'}]}]},activeCanvasDrag=null;
let scene={width:1920,height:1080,objects:[{id:'a',kind:'shape',bounds:{x:100,y:120,width:200,height:100}},{id:'b',kind:'shape',bounds:{x:900,y:120,width:100,height:100}}]};
let deck={cols:245,rows:56,pages:[{scene,fg:'#ffffff',bg:'#000000'}]},pageIndex=0;
let hits=[],frames=[],events=[],overlayCount=0,editorCanvasOverlayFrame=0;
let window=globalThis;
const fakeHit=(id,index,b)=>({dataset:{elementId:id,element:String(index)},bounds:b,active:false,classList:{toggle(_class,on){this.owner.active=on;},owner:null},querySelectorAll(){return []},appendChild(){}});
const canvasOverlay={querySelectorAll(){return hits;}};
function renderEditorCanvasOverlay(){overlayCount++;events.push('overlay');const boxes=KeynopeWorkspace.sceneSelectionBounds(deck.pages[pageIndex].scene,editorState.slides[0].elements,deck.cols,deck.rows);hits=[...boxes].map(([index,b])=>{const hit=fakeHit(editorState.slides[0].elements[index].id,index,b);hit.classList.owner=hit;hit.active=editorState.selection.includes(index);return hit;});}
function canvasGroupMembers(index){return [index];}
function canvasHitWantsHandles(){return false;}
function refreshCanvasSelectionBox(){}
function refreshCanvasSelectionResize(){}
function refreshCanvasShapeAdjustments(){}
function appendCanvasResizeHandles(){}
function scheduleEditorCanvasOverlay(){frames.push(()=>renderEditorCanvasOverlay());}
function requestAnimationFrame(fn){frames.push(fn);return frames.length;}
const stage={style:{}},effectLayer={style:{}},contentLayer={style:{}},chromeLayer={};
let keynopeEngagementOverlay=null,keynopeEngagementRuntime=null;
function updateAccessibleSlide(){}
function closeEngagementRuntime(){}
function label(){return '1';}
function esc(x){return x;}
function renderActivityMarker(){}
function drawFrame(){events.push('paint');}
${between('function render() {','function lineKey(')}
${between('  function refreshCanvasSelectionInPlace() {','  let shapeConnecting =')}
`,context);
const run=code=>vm.runInContext(code,context);
run('renderEditorCanvasOverlay();events=[];overlayCount=0;');
// A move paints fresh geometry while RAF remains suspended: the hit box must
// already match on return from the production render function.
run('deck.pages[0].scene.objects[0].bounds.x=350;render();');
assert.equal(run('hits[0].bounds.minX'),350/1920*245);
assert.equal(run('frames.length'),0,'Hit boxes depend on a later animation frame');
assert.equal(run('events.join(",")'),'paint,overlay');
assert.equal(run('stage.style.background'),'','Editor slide colour leaked onto pasteboard');
run('keynopeAppSurface=false;render();');
assert.equal(run('stage.style.background'),'#000000','Presentation background changed');
run('keynopeAppSurface=true;render();');
assert.equal(run('stage.style.background'),'','Switching back to editor retained slide background');
// Authored order changes independently of the scene order. Numeric selection
// now points to a different index for the same logical object.
run('editorState.slides[0].elements.reverse();editorState.selected=1;editorState.selection=[1];overlayCount=0;refreshCanvasSelectionInPlace();');
assert.equal(run('overlayCount'),1,'Stale index bindings were not recomputed');
assert.equal(run('hits.find(h=>h.active).dataset.elementId'),'a');
assert.equal(run('hits.find(h=>h.active).dataset.element'),'1');
assert.equal(run('hits.find(h=>h.active).bounds.minX'),350/1920*245);
// Pure selection on unchanged ordering takes the retained path.
run('overlayCount=0;editorState.selected=0;editorState.selection=[0];refreshCanvasSelectionInPlace();');
assert.equal(run('overlayCount'),0);assert.equal(run('hits.find(h=>h.active).dataset.elementId'),'b');
// A gesture retains its owned starting indices until release; no DOM rebuild
// may drop pointer capture while an unrelated asynchronous update arrives.
run('overlayCount=0;activeCanvasDrag={indices:[1]};refreshCanvasSelectionInPlace();');
assert.equal(run('overlayCount'),0);assert.equal(run('hits.find(h=>h.active).dataset.elementId'),'a');
console.log('PASS: production paint synchronises selection geometry without RAF; authored reorder rebinds IDs; pure selection/active gestures retain owners');
