'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),os=require('node:os'),path=require('node:path');
const {options,fingerprint,runStep,cases,artworkProof,requireArtifacts}=require('./test_editor_integration.cjs');
test('integration selections are explicit and reject unknown/repeated cases',()=>{
 assert.equal(options([]).selected.length,cases.length);assert.deepEqual(options([]).engines,['chromium','webkit']);
 assert.deepEqual(options(['--browser=webkit','--case=themes,tables','--timeout=3']).selected,['themes','tables']);
 for(const args of [['--case=missing'],['--case=themes,themes'],['--browser=missing'],['--browser='],['--timeout=0'],['--timeout=NaN'],['--skip-build'],['--output='],['--schemas=']])assert.throws(()=>options(args));
});
test('artwork cases require fixtures and fresh per-engine package outputs',()=>{
 assert(cases.some(c=>c.id==='font-pptx'));assert(cases.some(c=>c.id==='image-pptx'));
 assert.equal(artworkProof('tables','webkit'),null);
 assert.deepEqual(artworkProof('font-pptx','webkit').outputs,['browser-export.json','legacy-browser-export.json']);
 for(const engine of ['chromium','webkit']){
  const proof=artworkProof('image-pptx',engine);assert.deepEqual(proof.outputs,['export-'+engine+'.json']);
  assert.equal(proof.readback,'^TestPPTXModernPaintBrowserPackage$/^'+engine+'$');
 }
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'keynope-artifact-unit-'));
 try{
  assert.throws(()=>requireArtifacts(root,['absent.json']),/Missing required/);
  fs.writeFileSync(path.join(root,'empty.json'),'');assert.throws(()=>requireArtifacts(root,['empty.json']),/nonempty/);
  fs.mkdirSync(path.join(root,'directory.json'));assert.throws(()=>requireArtifacts(root,['directory.json']),/regular file/);
  fs.writeFileSync(path.join(root,'valid.json'),'{}');assert.doesNotThrow(()=>requireArtifacts(root,['valid.json']));
  fs.symlinkSync(path.join(root,'valid.json'),path.join(root,'link.json'));assert.throws(()=>requireArtifacts(root,['link.json']),/regular file/);
  assert.throws(()=>requireArtifacts(root,['../escape.json']),/leaf filename/);
 }finally{fs.rmSync(root,{recursive:true,force:true});}
});
test('fingerprints bind code, fixtures and tested harnesses but exclude output/private infrastructure',()=>{
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'keynope-fingerprint-unit-'));
 try{
  fs.mkdirSync(path.join(root,'web'));fs.writeFileSync(path.join(root,'main.go'),'package main');fs.writeFileSync(path.join(root,'web','scene.js'),'first');
  const initial=fingerprint(root);fs.mkdirSync(path.join(root,'terraform'));fs.writeFileSync(path.join(root,'terraform','private.tf'),'not part of the build');assert.equal(fingerprint(root).sha256,initial.sha256);
  fs.writeFileSync(path.join(root,'web','scene.js'),'second');assert.notEqual(fingerprint(root).sha256,initial.sha256);
  fs.mkdirSync(path.join(root,'tools'));fs.writeFileSync(path.join(root,'tools','test_pptx_font_capture.cjs'),'first capture check');
  const beforeCapture=fingerprint(root).sha256;fs.writeFileSync(path.join(root,'tools','test_pptx_font_capture.cjs'),'changed capture check');assert.notEqual(fingerprint(root).sha256,beforeCapture);
  const beforeLicense=fingerprint(root).sha256;fs.writeFileSync(path.join(root,'LICENSE.txt'),'Bundled notice');assert.notEqual(fingerprint(root).sha256,beforeLicense);
  fs.symlinkSync(path.join(root,'main.go'),path.join(root,'web','unexpected-link'));assert.throws(()=>fingerprint(root),/symlink/);
 }finally{fs.rmSync(root,{recursive:true,force:true});}
});
test('step results use child exit status, preserve logs and settle timeouts',async()=>{
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'keynope-step-unit-'));
 try{
  const run=(id,code,extra={})=>runStep({id,command:process.execPath,args:['-e',code],cwd:root,env:process.env,log:path.join(root,id+'.log'),...extra});
  const pass=await run('pass','console.log("owned log")');assert(pass.passed);assert.match(fs.readFileSync(pass.log,'utf8'),/owned log/);
  const fail=await run('fail','console.log("PASS is only text");process.exit(3)');assert(!fail.passed);assert.equal(fail.code,3);
  const timed=await run('timeout','setInterval(()=>{},1000)',{timeoutMS:60});assert(!timed.passed);assert.equal(timed.status,'timed-out');assert(timed.signal||timed.code!==0);
  const spawn=await runStep({id:'missing',command:path.join(root,'absent'),cwd:root,env:process.env,log:path.join(root,'missing.log')});assert.equal(spawn.status,'spawn-error');assert(!spawn.passed);
 }finally{fs.rmSync(root,{recursive:true,force:true});}
});
