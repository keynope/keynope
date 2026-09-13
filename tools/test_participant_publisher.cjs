const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const source = fs.readFileSync('main.go','utf8');
const start = source.indexOf('let keynopeEditorPresentationActive = false;');
const end = source.indexOf('const keynopeEngagementControllerSurface',start);
const sent=[];
let connections=0;
let currentSlide=2,edited=false;
const context = vm.createContext({
  window:{KEYNOPE_PRESENTER:true}, console, Date, AbortSignal,
  keynopeAppSurface:true,keynopeEditorMasterMode:false,presenterPresenting:true,
  presenterDeckVersion:1,pageIndex:0,
  presenterTimerBroadcast:false,
  presenterTimerCanBroadcast:()=>context.presenterPresenting && context.hasActivities,
  hasActivities:true,
  presenterPageAt:()=>({slide:currentSlide,page:1}),
  onboardingSessionDefinition:()=>({id:'lobby',code:'Abcd1234'}),
  keynopeActivityConnector:async()=>async()=>{connections++;return {send:async p=>{if(p.type!=='lobby-tabs')sent.push(p);},close(){}}},
  KeynopePresentationTransfer:{pack:async markdown=>({id:markdown,parts:['data']})},
  fetch:async url=>({ok:true,json:async()=>({rendered:{label:url+(edited&&url.endsWith('slide=0')?' edited':'')},slideCount:4})}),
  syncPresenterState:async()=>{},setInterval:()=>{}
});
vm.runInContext(source.slice(start,end),context);
(async()=>{
  // No activity dialog or activity channel exists. Native state alone starts
  // publishing, even if the editor-only toolbar callback was never delivered.
  await vm.runInContext('publishLobbyPresentation()',context);
  assert.equal(connections,1);
  assert.equal(sent[0].type,'presentation-md');
  assert.equal(sent[0].presenting,true);
  assert.equal(sent[0].slide,2);
  assert.equal(sent[0].page,1);
  assert.equal(sent[1].type,'presentation-part');
  await vm.runInContext('publishLobbyPresentation()',context);
  assert.equal(sent[2].type,'presentation-preload');
  assert.equal(sent[3].type,'presentation-part');
  currentSlide=0;context.pageIndex=1;
  await vm.runInContext('publishLobbyPresentation()',context);
  assert.equal(sent.length,5,'cached navigation retransmitted media');
  assert.equal(sent[4].slide,0);
  assert.equal(sent[4].type,'presentation-md');
  edited=true;context.presenterDeckVersion++;
  await vm.runInContext('publishLobbyPresentation()',context);
  assert.equal(sent.length,7,'changed slide was not retransmitted');
  for(let i=0;i<4;i++)await vm.runInContext('publishLobbyPresentation()',context);
  const afterPreload=sent.length;
  await vm.runInContext('publishLobbyPresentation()',context);
  assert.equal(sent.length,afterPreload,'completed preload keeps sending');
  await vm.runInContext('publishLobbyPresentation(true)',context);
  assert.equal(sent.length,afterPreload+2,'late join did not receive document');
  for(let i=0;i<20;i++)await vm.runInContext('publishLobbyPresentation(true)',context);
  assert.equal(sent.length,afterPreload+2,'simultaneous late joiners repeatedly retransmitted the page');
  // Advance the cooldown so stop can flush the pending replay request.
  vm.runInContext('keynopeLobbyPresentation.forceSentAt = Date.now()-2100',context);
  context.presenterPresenting=false;
  await vm.runInContext('publishLobbyPresentation()',context);
  assert.equal(sent.at(-1).presenting,false);
  assert.equal(connections,1,'stop unnecessarily reconnected');
  let attempt=0;
  context.recoveringSend=async payload=>{
    if(++attempt===1)throw Error('Simulated upload timeout');
    if(attempt===2)return new Promise(()=>{}); // error-report write also stuck
    sent.push(payload);
  };
  context.presenterPresenting=true;context.pageIndex++;
  vm.runInContext('keynopeLobbyPresentation.channel.send=recoveringSend',context);
  await Promise.race([vm.runInContext('publishLobbyPresentation()',context),new Promise((_,reject)=>setTimeout(()=>reject(Error('Failure handler held sending latch')),100))]);
  assert.equal(vm.runInContext('keynopeLobbyPresentation.sending',context),false);
  vm.runInContext('keynopeLobbyPresentation.forceSentAt=0',context);
  await vm.runInContext('publishLobbyPresentation()',context);
  assert.equal(sent.filter(p=>p.type==='presentation-md').at(-1).presenting,true,'publisher failed to recover');
  // Broadcast requires presentation mode and activities; local timers never leak.
  context.presenterTimerMode='running';
  context.presenterTimerEndMS=Date.now()+90000;
  context.presenterPresenting=false;
  vm.runInContext('keynopeLobbyPresentation.forceSentAt=0',context);
  sent.length=0;
  await vm.runInContext('publishLobbyPresentation()',context);
  assert.equal(context.window.keynopePresentationTimerEnd(),0,'local timer leaked to web');
  assert.equal(sent.find(p=>p.type==='presentation-md').presenting,false,'local timer started a web presentation');
  const localDeadline=context.presenterTimerEndMS;
  context.presenterTimerBroadcast=true;
  assert.equal(context.window.keynopePresentationTimerEnd(),0,'non-presenting timer leaked');
  context.presenterPresenting=true;context.hasActivities=false;
  assert.equal(context.window.keynopePresentationTimerEnd(),0,'activity-free timer leaked');
  context.hasActivities=true;
  sent.length=0;
  await vm.runInContext('publishLobbyPresentation()',context);
  assert.equal(context.presenterTimerEndMS,localDeadline,'broadcast restarted the countdown');
  const timer=sent.find(p=>p.type==='presentation-md');
  assert.equal(timer.presenting,true);
  assert.equal(timer.timerEndMs,context.presenterTimerEndMS);
  assert.equal(timer.slideCount,1);
  assert.equal(JSON.parse(timer.transfer).pages[0].lines.length,0,'timer leaked slide content');
  const timerCount=sent.length;
  await vm.runInContext('publishLobbyPresentation()',context);
  assert.equal(sent.length,timerCount,'timer retransmitted on unchanged tick');
  context.presenterTimerEndMS+=1000;
  await vm.runInContext('publishLobbyPresentation()',context);
  assert.equal(sent.length,timerCount+1,'timer restart re-uploaded the test card');
  assert.equal(sent.at(-1).timerEndMs,context.presenterTimerEndMS);
  await vm.runInContext('publishLobbyPresentation(true)',context);
  assert.equal(sent.filter(p=>p.type==='presentation-md').at(-1).timerEndMs,context.presenterTimerEndMS,'late join lost deadline');
  context.presenterTimerMode='';context.presenterTimerEndMS=0;context.presenterPresenting=false;
  await vm.runInContext('publishLobbyPresentation()',context);
  assert.equal(sent.at(-1).presenting,false,'stopping timer did not restore standby');
  context.presenterPresenting=true;
  await vm.runInContext('publishLobbyPresentation()',context);
  assert.equal(sent.filter(p=>p.type==='presentation-md').at(-1).timerEndMs,0,'normal slide retained timer');
  // A browser presentation can start its own timer via its keyboard controls.
  context.window.KEYNOPE_WEB_EDITOR=true;
  const popupEnd=Date.now()+60000;
  context.window.keynopeLivePresentationWindow={closed:false,keynopePresentationPosition:()=>({slide:0,page:0,key:'popup'}),keynopePresentationTimerEnd:()=>popupEnd};
  await vm.runInContext('publishLobbyPresentation()',context);
  assert(sent.filter(p=>p.type==='presentation-md').at(-1).timerEndMs>0,'popup timer not shared');
  console.log('Timer publisher: eligibility, cached restart, late join, standby/slide restoration and web popup passed.');
  console.log('Participant publisher: activity-independent start, native state, overflow, deduplication, late join and stop passed.');
})().catch(e=>{console.error(e);process.exitCode=1});
