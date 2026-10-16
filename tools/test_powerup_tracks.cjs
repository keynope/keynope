// Pure, deterministic tests for the exact shared table gesture algorithms.
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
const context=vm.createContext({});
vm.runInContext(fs.readFileSync(path.join(__dirname,'../web/tables.js'),'utf8'),context);
const api=context.KeynopeTables;
const fixture=weights=>({columns:weights.map(width=>({width})),rows:weights.map(height=>({height}))});
const values=(data,axis)=>data[axis].map(t=>t[axis==='rows'?'height':'width']);
const near=(a,b)=>assert.ok(Math.abs(a-b)<=1e-9*Math.max(1,Math.abs(a),Math.abs(b)),`${a} != ${b}`);
const sum=a=>a.reduce((n,v)=>n+v,0);
for(const axis of ['rows','columns']){
  const d=fixture([2,4,3,6]);api.resizeProportionalTracks(d,axis,1,3,[.2,.4,.3,.6]);
  const actual=values(d,axis);near(actual[0]/actual[1],.5);near(actual[2]/actual[3],.5);near(sum(actual),15);near(actual[0]+actual[1],9);
  const tiny=fixture([.01,4,3,6]);api.resizeTracks(tiny,axis,0,-100,[1,1]);near(values(tiny,axis)[0],.01);
  api.resizeTracks(tiny,axis,0,1,[1,1]);near(values(tiny,axis)[0],1.01);
  for(let n=0;n<400;n++){
    const weights=Array.from({length:2+n%9},(_,i)=>.5+Math.abs(Math.sin(n*1.7+i*.8))*20),index=n%(weights.length-1),minimum=weights.map((v,i)=>v*(.05+(i%4)*.1)),delta=(n%2?-1:1)*(n%3?1:1000),data=fixture(weights);
    api.resizeProportionalTracks(data,axis,index,delta,minimum);const next=values(data,axis);
    near(sum(next),sum(weights));next.forEach((v,i)=>{assert.ok(v>=minimum[i]-1e-9&&Number.isFinite(v));if(i>0&&i!==index+1)near(v/next[i-1],weights[i]/weights[i-1]);});
    const ordinary=fixture(weights);api.resizeTracks(ordinary,axis,index,delta,minimum.slice(index,index+2));const resized=values(ordinary,axis);near(sum(resized),sum(weights));resized.forEach((v,i)=>{assert.ok(v>=minimum[i]-1e-9);if(i!==index&&i!==index+1)near(v,weights[i]);});
  }
}
for(const [axis,index,delta,minimum]of [['bad',0,1,[1,1]],['columns',.5,1,[1,1]],['columns',0,NaN,[1,1]],['columns',0,1,[0,1]],['columns',0,1,[1]]]){
  const d=fixture([2,4]),before=JSON.stringify(d);assert.throws(()=>api.resizeProportionalTracks(d,axis,index,delta,minimum));assert.equal(JSON.stringify(d),before);
}
const huge=fixture([Number.MAX_VALUE,Number.MAX_VALUE]);assert.throws(()=>api.resizeProportionalTracks(huge,'columns',0,1,[1,1]));
const cell={text:'keep',style:{fill:'#223344',borderWidth:2,borders:{left:{color:'#ff0055',width:4},right:{color:'none',width:0},top:{color:'#00ffaa'}}}};
assert.equal(new Set(api.cellStyleValues(cell.style,'borderWidth')).size,3,'Different imported edges must be shown as mixed');
api.setCellStyle(cell,'borderWidth',3);
assert.equal(cell.style.borderWidth,3);assert.equal(cell.style.borders.left.width,undefined);assert.equal(cell.style.borders.right.width,undefined);
assert.equal(cell.style.borders.left.color,'#ff0055');assert.equal(cell.style.fill,'#223344');assert.equal(cell.text,'keep');
api.setCellStyle(cell,'borderColor','#ffffff');assert.equal(cell.style.borders,undefined);assert.equal(cell.style.borderWidth,3);
api.setCellStyle(cell,'borderWidth',null);api.setCellStyle(cell,'borderColor',null);assert.equal(JSON.stringify(cell.style),JSON.stringify({fill:'#223344'}),'Reset all borders should restore inheritance without clearing fill');
console.log('Table adjacent/proportional track conservation, ratios, readable clamps, legacy undersized tracks and atomic rejection passed.');
