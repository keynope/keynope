// Pure reconstruction/failure tests of the production shared workspace helper.
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
vm.runInThisContext(fs.readFileSync('web/workspace.js','utf8'));
const previous=[{slide:0,page:0,scene:{version:1,slideId:'s',width:1920,height:1080,background:'#fff',objects:[{id:'keep',kind:'shape'},{id:'edit',kind:'text',text:{runs:[{text:'Before'}]}},{id:'remove',kind:'shape'}]}}];
const old=JSON.stringify(previous),patch={version:1,slideId:'s',order:['edit','new','keep'],changed:[{id:'edit',kind:'text',text:{runs:[{text:'After'}]}},{id:'new',kind:'shape'}],removed:['remove'],metadata:{version:1,slideId:'s',width:1920,height:1080,background:'#111',objects:null}};
const apply=p=>KeynopeWorkspace.applyScenePagePatches([{slide:0,page:0,scenePatch:p}],previous)[0];
const result=apply(patch);assert.equal(result.scenePatch,undefined);assert.equal(result.scene.background,'#111');assert.deepEqual(result.scene.objects.map(o=>o.id),patch.order);assert.equal(result.scene.objects[0].text.runs[0].text,'After');assert.equal(result.scene.objects[2],previous[0].scene.objects[0]);assert.equal(JSON.stringify(previous),old);
for(const alter of [p=>p.slideId='other',p=>p.version=2,p=>p.order=['edit','edit','keep'],p=>p.order=['missing','new','keep'],p=>p.removed=['bad'],p=>p.changed.push(p.changed[0]),p=>p.metadata.width=NaN]){const bad=structuredClone(patch);alter(bad);assert.throws(()=>apply(bad));assert.equal(JSON.stringify(previous),old);}
console.log('PASS: production workspace scene reconstruction/order/ownership and invalid-patch rejection');
