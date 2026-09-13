const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('main.go','utf8');
const start=source.indexOf('  function clearCanvasPlacementAnchors(');
const end=source.indexOf('  function canvasElementAt(',start);
const context=vm.createContext({});
vm.runInContext(source.slice(start,end),context);
for(const align of ['justify','center','right','left']) {
  const query=new URLSearchParams({align,right:'2',right_pct:'.1',bottom:'3',row_delta:'4',valign:'middle',left:'5',width:'90',height:'20','text-box':'1',fg:'#55aa00'});
  context.clearCanvasPlacementAnchors(query);
  assert.equal(query.get('align'),align==='justify'?'justify':null);
  for(const key of ['right','right_pct','bottom','row_delta','valign','left'])assert.equal(query.has(key),false);
  assert.equal(query.get('width'),'90');assert.equal(query.get('height'),'20');assert.equal(query.get('fg'),'#55aa00');
}
for(const [begin,finish] of [['const fitElementForBounds =','const pumpVisualPreview ='],['const element = {...sourceElement};\n          const query','element.query = query.toString();'],['function nudgeSelected(','function cycleCanvasSelection(']]) {
  const start=source.indexOf(begin),end=source.indexOf(finish,start);
  assert(start>=0&&end>start);
  assert(source.slice(start,end).includes('clearCanvasPlacementAnchors(query)'));
}
console.log('PASS: moving, nudging and resizing preserve justification and text formatting.');
