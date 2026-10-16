// Test every activity exposed by the shared participant UI, not a stale
// handwritten subset. Loading without document avoids installing browser CSS.
const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const context=vm.createContext({});
vm.runInContext(fs.readFileSync(path.resolve(__dirname,'../web/activity-design.js'),'utf8'),context);
module.exports=Object.freeze(Object.keys(context.KeynopeActivityDesign.activities));
