// Offline, headless integration batch. No desktop activation, release or deploy.
'use strict';
const fs=require('node:fs'),path=require('node:path'),os=require('node:os'),crypto=require('node:crypto'),cp=require('node:child_process');
const cases=Object.freeze([
 ['themes','test_powerup_themes.cjs','Template gallery, theme draft/history/detach and exact appearance'],
 ['tables','test_powerup_tables.cjs','Cells, tracks, merges, dense editing, history and editable PPTX'],
 ['charts','test_powerup_charts.cjs','Six chart types, data editing, accessible data and history'],
 ['chart-pptx','test_powerup_chart_pptx.cjs','Six native chart/workbook imports and retained rendering'],
 ['font-pptx','test_pptx_font_capture.cjs','Retro font artwork, GIF timing, cancellation and legacy editable diagrams; real PPTX readback'],
 ['image-pptx','test_pptx_image_artwork.cjs','Image-effect pixels, crop/fit/mask and foreground/background/GIF captures; real PPTX readback'],
 ['shapes','test_powerup_shapes.cjs','Catalogue, adjustment gestures, routing and persistence'],
 ['svg','test_powerup_svg.cjs','Passive SVG import, image treatments, persistence and output'],
 ['media','test_media_draft_scopes.cjs','Normal/layout crop preview, commit, Cancel and ownership'],
 ['styles','test_powerup_styles.cjs','Copy/Paste/Reset/Painter and local/group ownership'],
 ['selection','test_powerup_selection.cjs','Mixed typography/paint, precision geometry and master history'],
 ['object-layout','test_object_layout.cjs','Dense object-scoped geometry, isolated Retro sampling, retained owners and history'],
 ['master-thumbnails','test_master_thumbnails.cjs','Dependency-stamped master thumbnails, retained owners, Base/theme consumers and stale async guards'],
 ['document-lifetime','test_document_lifetime.cjs','Real New/Open cache release, failed-open ownership, portable media/fonts and subsequent editing/history'],
 ['builds','test_powerup_builds.cjs','Timeline editing, presenter/standalone/participant cursor playback'],
 ['corpus','test_powerup_corpus.cjs','Twenty public proof pages, fonts and declared diagnostics'],
 ['mixed','test_primitive_integration.cjs','Normal/tab/Base/layout mixed groups and editable second reopen']
].map(([id,script,coverage])=>Object.freeze({id,script,coverage})));

// These readbacks must consume this engine's fresh browser output, never an
// old global /tmp proof. Missing/empty artifacts are failures, not Go opt-in
// skips. Keep package verification in the same source-bound batch as capture.
function artworkProof(id,engine){
 if(id==='font-pptx')return {fixtures:['font-art.md','legacy-diagram.md'],outputs:['browser-export.json','legacy-browser-export.json'],readback:'^TestPPTX(FontCaptureBrowserReadback|LegacyDiagramBrowserReadback)$'};
 if(id==='image-pptx')return {fixtures:['deck.md'],outputs:['export-'+engine+'.json'],readback:'^TestPPTXModernPaintBrowserPackage$/^'+engine+'$'};
 return null;
}
function requireArtifacts(folder,names){
 for(const name of names){
  if(path.basename(name)!==name)throw Error('Artifact must be a leaf filename: '+name);
  const file=path.join(folder,name);
  let info;try{info=fs.lstatSync(file);}catch(error){throw Error('Missing required integration artifact: '+file,{cause:error});}
  if(!info.isFile()||info.size===0)throw Error('Required integration artifact is not a nonempty regular file: '+file);
 }
}

function options(args){
 const result={engines:['chromium','webkit'],selected:cases.map(c=>c.id),timeoutMS:600000,native:false,list:false};
 for(const arg of args){
  if(arg==='--list')result.list=true;
  else if(arg==='--native')result.native=true;
  else if(arg.startsWith('--browser='))result.engines=arg.slice(10).split(',');
  else if(arg.startsWith('--case='))result.selected=arg.slice(7).split(',');
  else if(arg.startsWith('--output=')){if(!arg.slice(9))throw Error('Output directory must not be empty.');result.output=path.resolve(arg.slice(9));}
  else if(arg.startsWith('--schemas=')){if(!arg.slice(10))throw Error('Schema directory must not be empty.');result.schemas=path.resolve(arg.slice(10));}
  else if(arg.startsWith('--timeout='))result.timeoutMS=Number(arg.slice(10))*1000;
  else throw Error('Unknown integration option: '+arg);
 }
 for(const [values,allowed,name]of [[result.engines,['chromium','webkit'],'browser'],[result.selected,cases.map(c=>c.id),'case']]){
  if(!values.length||new Set(values).size!==values.length||values.some(v=>!allowed.includes(v)))throw Error('Invalid or repeated '+name+' selection.');
 }
 if(!Number.isSafeInteger(result.timeoutMS)||result.timeoutMS<1000||result.timeoutMS>3600000)throw Error('Timeout must be 1–3600 seconds.');
 return result;
}

function fingerprint(root){
 const files=[];
 function walk(relative){
  const full=path.join(root,relative);if(!fs.existsSync(full))return;
  const stat=fs.lstatSync(full);
  if(stat.isSymbolicLink())throw Error('Cannot fingerprint source symlink: '+relative);
  if(stat.isDirectory()){for(const name of fs.readdirSync(full).sort())walk(path.join(relative,name));}
  else if(stat.isFile())files.push(relative);
 }
 for(const name of fs.readdirSync(root).sort())if(/\.go$/.test(name)||['go.mod','go.sum','Makefile','README.md','CHANGELOG.md','LICENSE.txt','sandbox_bridge_darwin.m'].includes(name))walk(name);
 for(const dir of ['internal','web','presenter','app','assets','testdata/powerup','testdata/pptx'])walk(dir);
 for(const name of [...cases.map(c=>c.script),'test_editor_integration.cjs','test_editor_integration_unit.cjs','test_native_acceptance.sh','test_native_acceptance_unit.cjs','wait_for_async.cjs','build_web_editor.sh','build_participant_renderer.cjs','test_pptx_text_schema.py','test_pptx_table_schema.py','test_pptx_timing_schema.py','test_native_style_commands.sh','test_native_pdf_export.sh','test_sandbox_pdf_readback.swift','test_native_appkit_gestures.sh'])walk('tools/'+name);
 const hashes={},digest=crypto.createHash('sha256');
 for(const name of [...new Set(files)].sort()){const hash=crypto.createHash('sha256').update(fs.readFileSync(path.join(root,name))).digest('hex');hashes[name]=hash;digest.update(name+'\0'+hash+'\0');}
 return {sha256:digest.digest('hex'),files:hashes};
}

function runStep({id,command,args=[],cwd,env,log,timeoutMS=600000,signal}){
 return new Promise(resolve=>{
  fs.mkdirSync(path.dirname(log),{recursive:true});const fd=fs.openSync(log,'w'),started=Date.now();
  let timedOut=false,cancelled=!!signal?.aborted,spawnError='',killTimer;
  const child=cp.spawn(command,args,{cwd,env,stdio:['ignore',fd,fd]});
  const stop=()=>{child.kill('SIGTERM');killTimer=setTimeout(()=>child.kill('SIGKILL'),5000);killTimer.unref();};
  const abort=()=>{cancelled=true;stop();};
  const timer=setTimeout(()=>{timedOut=true;stop();},timeoutMS);timer.unref();
  signal?.addEventListener('abort',abort,{once:true});if(cancelled)stop();
  child.once('error',error=>{spawnError=error.message;});
  child.once('close',(code,termination)=>{
   clearTimeout(timer);clearTimeout(killTimer);signal?.removeEventListener('abort',abort);fs.closeSync(fd);
   const passed=code===0&&!timedOut&&!cancelled&&!spawnError;
   resolve({id,command,args,startedAt:new Date(started).toISOString(),elapsedMS:Date.now()-started,code,signal:termination,log,passed,status:passed?'passed':timedOut?'timed-out':cancelled?'cancelled':spawnError?'spawn-error':'failed',...(spawnError?{error:spawnError}:{})});
  });
 });
}

async function main(args){
 const config=options(args);
 if(config.list){console.log(JSON.stringify({cases,defaultBrowsers:['chromium','webkit'],optional:['--native: background-only macOS style/geometry/build checks','--schemas=/path/to/official/transitional/xsd: independent XML checks']},null,2));return 0;}
 const root=path.resolve(__dirname,'..'),output=config.output||fs.mkdtempSync(path.join(os.tmpdir(),'keynope-integration-'));
 if(config.schemas)for(const name of ['pml.xsd','dml-main.xsd','dml-chart.xsd'])if(!fs.existsSync(path.join(config.schemas,name)))throw Error('Missing supplied schema: '+name);
 if(config.native&&process.platform!=='darwin')throw Error('--native requires macOS; no silent native skip.');
 if(fs.existsSync(output)&&fs.readdirSync(output).length)throw Error('Choose a new or empty integration output directory.');
 fs.mkdirSync(output,{recursive:true});
 const fixtures=path.join(output,'fixtures'),site=path.join(output,'site');
 fs.mkdirSync(path.join(fixtures,'object-layout'),{recursive:true});
 const env={...process.env,GOCACHE:process.env.GOCACHE||path.join(os.tmpdir(),'keynope-go-cache'),TEST_SITE:site,KEYNOPE_UPDATE_POWERUP_FIXTURES:'',
  KEYNOPE_SHAPE_EVIDENCE:path.join(fixtures,'shapes'),KEYNOPE_SVG_EVIDENCE:path.join(fixtures,'svg'),KEYNOPE_BUILD_EVIDENCE:path.join(fixtures,'builds'),KEYNOPE_DRAFT_SCOPE_FIXTURE:path.join(fixtures,'media'),KEYNOPE_PRIMITIVE_EVIDENCE:path.join(fixtures,'mixed'),KEYNOPE_CHART_PPTX_EVIDENCE:path.join(fixtures,'chart-pptx'),KEYNOPE_RICH_PPTX_EVIDENCE:path.join(fixtures,'words'),KEYNOPE_PPTX_TIMING_EVIDENCE:path.join(fixtures,'timing'),KEYNOPE_FONT_CAPTURE_EVIDENCE:path.join(fixtures,'font-pptx'),KEYNOPE_IMAGE_ARTWORK_EVIDENCE:path.join(fixtures,'image-pptx'),KEYNOPE_IMAGE_ARTWORK_VERIFY:''};
 env.KEYNOPE_DENSE_EDITING_FIXTURE=path.join(fixtures,'object-layout','dense.md');
 env.KEYNOPE_MASTER_THUMBNAIL_EVIDENCE=path.join(fixtures,'master-thumbnails');
 env.KEYNOPE_DOCUMENT_LIFETIME_EVIDENCE=path.join(fixtures,'document-lifetime');
 const report={version:1,startedAt:new Date().toISOString(),platform:{system:os.type(),release:os.release(),architecture:os.arch(),node:process.version},selectedCases:config.selected,browsers:config.engines,steps:[],passed:false,visualApproval:'not-performed',excluded:['Foreground AppKit/Save panels','Native PDF rendering and cross-reader pixels','Native input-to-photon performance','Workshop mechanics/load and cloud transport','External Office visual/editing acceptance']};
 const controller=new AbortController(),interrupt=()=>controller.abort();process.on('SIGINT',interrupt);process.on('SIGTERM',interrupt);
 const save=()=>fs.writeFileSync(path.join(output,'report.json'),JSON.stringify(report,null,2)+'\n');
 const step=async(id,command,args,extra={})=>{
  console.log('START '+id);report.running=id;save();
  const result=await runStep({id,command,args,cwd:root,env:{...env,...extra},log:path.join(output,'logs',id+'.log'),timeoutMS:config.timeoutMS,signal:controller.signal});
  report.steps.push(result);delete report.running;save();console.log(result.status.toUpperCase()+' '+id+' ('+result.elapsedMS+' ms) '+result.log);return result.passed;
 };
 let testedSource;
 try{
  console.log('Integration artifacts: '+output);
  if(!await step('runner-unit',process.execPath,['--test','tools/test_editor_integration_unit.cjs','tools/test_native_acceptance_unit.cjs']))return 1;
  if(!await step('build-web','sh',['tools/build_web_editor.sh',path.join(site,'editor')]))return 1;
  // The build intentionally refreshes deterministic template previews first.
  testedSource=fingerprint(root);report.source=testedSource;
  if(!await step('go-and-fixtures',env.GO||'go',['test','-count=1','./...']))return 1;
  const module=require.resolve(env.PLAYWRIGHT_MODULE||'playwright');
  report.playwright=JSON.parse(fs.readFileSync(path.join(path.dirname(module),'package.json'),'utf8')).version;
  for(const engine of config.engines){
   for(const test of cases.filter(c=>config.selected.includes(c.id))){
    if(controller.signal.aborted)return 1;
    const folder=path.join(output,engine,test.id);fs.mkdirSync(folder,{recursive:true});
    const proof=artworkProof(test.id,engine);
    if(['shapes','svg','media','builds','object-layout','master-thumbnails','document-lifetime'].includes(test.id)||proof)fs.cpSync(path.join(fixtures,test.id),folder,{recursive:true});
    if(proof)requireArtifacts(folder,proof.fixtures);
    const before=fingerprint(root).sha256;
    const extra={TEST_BROWSER:engine,TEST_OUTPUT:folder,PROOF_OUTPUT:folder,KEYNOPE_SHAPE_EVIDENCE:folder,SVG_PROOF:folder,KEYNOPE_BUILD_EVIDENCE:folder,KEYNOPE_DRAFT_SCOPE_FIXTURE:folder,CHART_PPTX_FILE:path.join(fixtures,'chart-pptx','editable-charts.pptx'),KEYNOPE_FONT_CAPTURE_EVIDENCE:folder,KEYNOPE_IMAGE_ARTWORK_EVIDENCE:folder,KEYNOPE_IMAGE_ARTWORK_VERIFY:folder};
    if(test.id==='object-layout')extra.TEST_DECK=path.join(folder,'dense.md');
    if(test.id==='master-thumbnails')extra.TEST_DECK=path.join(folder,'masters.md');
    const passed=await step(engine+'-'+test.id,process.execPath,['tools/'+test.script],extra);
    const after=fingerprint(root).sha256;
    report.steps.at(-1).sourceStable=before===testedSource.sha256&&after===before;
    report.steps.at(-1).coverage=test.coverage;save();
    if(proof&&passed){
     requireArtifacts(folder,proof.outputs);
     await step(engine+'-'+test.id+'-package',env.GO||'go',['test','-v','-count=1','-run',proof.readback,'.'],extra);
    }
   }
  }
  if(config.schemas){
   await step('schema-charts-and-words','python3',['tools/test_pptx_text_schema.py',config.schemas,path.join(fixtures,'chart-pptx','editable-charts.pptx'),path.join(fixtures,'words','words.pptx')]);
   await step('schema-timing','python3',['tools/test_pptx_timing_schema.py',config.schemas,path.join(fixtures,'timing','timing-native.pptx')]);
   if(config.selected.includes('tables'))for(const engine of config.engines)await step('schema-tables-'+engine,'python3',['tools/test_pptx_table_schema.py',config.schemas,path.join(output,engine,'tables','table-editable.pptx')]);
  }else report.schemaValidation='not-requested';
  if(config.native){
   if(await step('build-native','make',['app']))await step('native-background','zsh',['tools/test_native_style_commands.sh'],{SKIP_BUILD:'1',KEEP_OUTPUT:'1',KEYNOPE_NATIVE_BACKGROUND:'1',KEYNOPE_NATIVE_STYLE_MENU_FOREGROUND:'0',SANDBOX:'0'});
  }else report.nativeAcceptance='not-requested';
  const finalSource=fingerprint(root);report.sourceStable=finalSource.sha256===testedSource.sha256&&report.steps.every(s=>s.sourceStable!==false);
  report.passed=report.sourceStable&&report.steps.every(s=>s.passed)&&!controller.signal.aborted;
  return report.passed?0:1;
 }catch(error){report.error=error.message;return 1;}
 finally{
  delete report.running;report.finishedAt=new Date().toISOString();save();process.removeListener('SIGINT',interrupt);process.removeListener('SIGTERM',interrupt);
  console.log((report.passed?'PASS':'FAIL')+' integration batch: '+path.join(output,'report.json'));
 }
}
module.exports={cases,artworkProof,requireArtifacts,options,fingerprint,runStep,main};
if(require.main===module)main(process.argv.slice(2)).then(code=>{process.exitCode=code;},error=>{console.error(error.message);process.exitCode=1;});
