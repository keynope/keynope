// Exact shared structural algorithms. Owned data, no network or app launch.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const ctx=vm.createContext({});vm.runInContext(fs.readFileSync(path.join(__dirname,'../web/tables.js'),'utf8'),ctx);
const api=ctx.KeynopeTables,copy=x=>JSON.parse(JSON.stringify(x));
const fixture=()=>({version:1,rows:[{height:2},{height:3}],columns:[{width:4},{width:5},{width:6}],cells:[[
  {text:'Merged',rowSpan:2,colSpan:2,style:{fill:'theme:surface',borderColor:'theme:line',borderWidth:2},borderFragments:{top:[{}, {color:'#00aa00',width:3}],bottom:[{color:'theme:accent-6',width:1.25},{color:'#aa00aa',width:5.5}],left:[{}, {color:'#0000aa',width:4}],right:[{color:'#aa0000',width:2},{color:'#00aaaa',width:6}]}},
  {hidden:true},{text:'Neighbour'}],[{hidden:true},{hidden:true},{text:'Other'}]]});
const edge=(cell,side,i)=>{const b=cell.style?.borders?.[side]||{},f=cell.borderFragments?.[side]?.[i]||{};return {color:f.color??b.color??cell.style?.borderColor,width:f.width??b.width??cell.style?.borderWidth};};
const sides=['left','right','top','bottom'];
// Delete original anchor: the new first segment keeps independent paint,
// including the authored role; unrelated bottom/side segments never flatten.
let d=fixture();api.track(d,'row',0,true);assert.equal(d.cells[0][0].text,'Merged');assert.equal(d.cells[0][0].rowSpan,undefined);
assert.equal(edge(d.cells[0][0],'left',0).color,'#0000aa');assert.equal(edge(d.cells[0][0],'right',0).width,6);assert.equal(edge(d.cells[0][0],'bottom',1).width,5.5);
d=fixture();api.track(d,'column',0,true);assert.equal(edge(d.cells[0][0],'top',0).color,'#00aa00');assert.equal(edge(d.cells[0][0],'bottom',0).color,'#aa00aa');assert.equal(edge(d.cells[0][0],'left',1).width,4);
// Insert inside a span: inserted segment inherits sparse owner paint; old
// fragments retain their relative order rather than copying previous RGB.
d=fixture();api.track(d,'row',1,false);assert.equal(d.cells[0][0].rowSpan,3);assert.deepEqual(copy(d.cells[0][0].borderFragments.left),[{}, {}, {color:'#0000aa',width:4}]);assert.equal(edge(d.cells[0][0],'left',1).color,'theme:line');
api.track(d,'row',1,true);assert.deepEqual(copy(d),fixture());
d=fixture();api.track(d,'column',1,false);assert.equal(d.cells[0][0].colSpan,3);assert.equal(edge(d.cells[0][0],'bottom',1).color,'theme:line');assert.equal(edge(d.cells[0][0],'bottom',2).width,5.5);api.track(d,'column',1,true);assert.deepEqual(copy(d),fixture());
// Split transfers only perimeter paint to child sides; content remains at
// anchor. Re-merge reconstructs exactly the exterior, not the internal seams.
d=fixture();const before=copy(d),rect={r0:0,c0:0,r1:1,c1:1};api.splitCells(d,rect);assert.equal(d.cells[1][1].hidden,undefined);assert.equal(d.cells[0][0].text,'Merged');assert.equal(d.cells[1][1].text,undefined);assert.equal(d.cells[0][0].style.borders.bottom,undefined);
assert.equal(edge(d.cells[1][0],'bottom',0).color,'theme:accent-6');assert.equal(edge(d.cells[1][1],'right',0).width,6);
api.mergeCells(d,rect);for(const side of sides)for(let i=0;i<2;i++)assert.deepEqual(edge(d.cells[0][0],side,i),edge(before.cells[0][0],side,i));assert.equal(d.cells[0][0].style.fill,'theme:surface');
// Merging explicit and inherited neighbours must NOT let anchor paint leak
// across the new perimeter. Empty source segment retains theme inheritance.
d={version:1,rows:[{height:1}],columns:[{width:1},{width:1}],cells:[[{text:'A',style:{borderColor:'#ff0000',borderWidth:4}},{text:'B'}]]};api.mergeCells(d,{r0:0,c0:0,r1:0,c1:1});assert.equal(edge(d.cells[0][0],'top',0).color,'#ff0000');assert.equal(edge(d.cells[0][0],'top',1).color,undefined);
// Whole-side edits clear only the selected component and expose mixed values.
d=fixture();let cell=d.cells[0][0];assert.deepEqual(copy(api.cellStyleValues(cell.style,'borders.bottom.width',cell.borderFragments)),[1.25,5.5]);
api.setCellStyle(cell,'borders.bottom.color','#ffffff');assert.equal(edge(cell,'bottom',0).color,'#ffffff');assert.equal(edge(cell,'bottom',1).width,5.5);
api.setCellStyle(cell,'borderWidth',3);assert.equal(edge(cell,'bottom',1).width,3);assert.equal(edge(cell,'top',1).color,'#00aa00');
// Optional structural artifacts are consumed by Go to validate production
// scene borders/persistence for these actual JavaScript-generated payloads.
if(process.env.KEYNOPE_FRAGMENT_EVIDENCE){fs.mkdirSync(process.env.KEYNOPE_FRAGMENT_EVIDENCE,{recursive:true});const results={};for(const [name,modify]of Object.entries({rowDelete:x=>api.track(x,'row',0,true),columnDelete:x=>api.track(x,'column',0,true),rowInsert:x=>api.track(x,'row',1,false),columnInsert:x=>api.track(x,'column',1,false),split:x=>api.splitCells(x,rect),splitMerge:x=>{api.splitCells(x,rect);api.mergeCells(x,rect);}})){const x=fixture();modify(x);results[name]=x;}fs.writeFileSync(path.join(process.env.KEYNOPE_FRAGMENT_EVIDENCE,'structures.json'),JSON.stringify(results));}
console.log('PASS: merged perimeter fragments survive track promotion/insertion, split/re-merge, sparse inheritance and component-only edits.');
