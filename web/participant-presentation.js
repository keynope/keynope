import '../participant-transfer.js';
import {createSlideRenderer} from './slide-renderer.js';

export function createPresentationViewer({frame,status,request}) {
  let manifest=null,cache=new Map(),transfers=new Map(),cacheBytes=0,epoch=0,renderID=0,rendered='',received=0,lastRequest=0,lastSentAt=0;
  let renderer=null,pendingPage=null;
  const startRenderer=()=>{if(!renderer)renderer=createSlideRenderer(frame);};
  // Standby must work immediately, without downloading or booting WASM.
  const snow=frame.ownerDocument.createElement('canvas');
  snow.width=245;snow.height=56;snow.setAttribute('aria-label','Presentation standby static');
  snow.style.cssText='display:block;width:100%;aspect-ratio:16/9;image-rendering:pixelated;background:#050505';
  frame.before(snow);
  const snowContext=snow.getContext('2d'),snowPixels=snowContext.createImageData(245,56);
  let visible=false,snowFrame=0;
  function drawSnow(){
    const palette=[16,48,112,168,240],pixels=snowPixels.data;
    for(let i=0;i<pixels.length;i+=4){const shade=palette[Math.floor(Math.random()*palette.length)];pixels[i]=pixels[i+1]=pixels[i+2]=shade;pixels[i+3]=255;}
    snowContext.putImageData(snowPixels,0,0);
    if(snowFrame++%9<2){snowContext.fillStyle='#fff';snowContext.fillRect(0,Math.floor(Math.random()*56),245,1);}
  }
  function standby(){snow.style.display='block';if(visible&&!document.hidden)drawSnow();status.hidden=true;}
  standby();
  setInterval(()=>{if(visible&&!manifest&&!document.hidden)drawSnow();},1000/15);
  const showStatus=text=>{status.hidden=false;status.textContent=text;};
  const stop=text=>{manifest=null;transfers.clear();epoch++;rendered='';renderID++;pendingPage=null;renderer?.setVisible(false);frame.hidden=true;standby();if(text&&!text.startsWith('Waiting for'))showStatus(text);};
  function render() {
    if(!visible||document.hidden)return;
    if(!manifest||!cache.has(manifest.transfer))return;
    const id=[manifest.transfer,manifest.slide,manifest.page,manifest.slideCount,manifest.timerEndMs||0].join(':');
    if(rendered===id){renderer?.setCursor(manifest.buildCursor||null);renderer?.setOverlay?.(manifest.overlay||null);return;}
    try {
      const data=JSON.parse(cache.get(manifest.transfer));
      if(!Number.isInteger(data.cols)||!Number.isInteger(data.rows)||data.cols<1||data.cols>1024||data.rows<1||data.rows>512||!Array.isArray(data.pages)||!data.pages[manifest.page])throw Error('Invalid rendered slide');
      for(const page of data.pages){page.engagement=null;page.hideChromePageNumber=true;}
      startRenderer();pendingPage=data;
      snow.style.display='none';frame.hidden=false;
      renderer.setPage(data,manifest.page,{timerEndMs:manifest.timerEndMs,buildCursor:manifest.buildCursor,overlay:manifest.overlay});renderer.setVisible(visible);
      rendered=id;status.hidden=true;
    }catch(error){showStatus(error.message);}
  }
  async function finish(current) {
    const transfer=transfers.get(current),generation=epoch;
    if(!transfer||transfer.decoding||cache.has(current)||transfer.parts.filter(value=>typeof value==='string').length!==transfer.count)return;
    transfer.decoding=true;
    try {
      const markdown=await KeynopePresentationTransfer.unpack(transfer.parts,current);
      if(epoch!==generation)return;
      cache.set(current,markdown);cacheBytes+=markdown.length*2;
      while(cacheBytes>64*1024*1024||cache.size>256){
        const oldest=[...cache.keys()].find(key=>key!==manifest?.transfer&&key!==current);
        if(!oldest)break;cacheBytes-=cache.get(oldest).length*2;cache.delete(oldest);
      }
      transfers.delete(current);
      if(manifest?.transfer===current)render();
    }catch(error){transfers.delete(current);if(manifest?.transfer===current)showStatus(error.message);}
  }
  function register(payload){
    if(!/^[a-f0-9]{64}$/.test(payload.transfer)||!Number.isInteger(payload.parts)||payload.parts<1||payload.parts>KeynopePresentationTransfer.maxParts)return false;
    if(!cache.has(payload.transfer)&&!transfers.has(payload.transfer)){
      transfers.set(payload.transfer,{count:payload.parts,parts:[],decoding:false});
      while(transfers.size>4){const oldest=[...transfers.keys()].find(key=>key!==manifest?.transfer&&key!==payload.transfer);if(!oldest)break;transfers.delete(oldest);}
    }
    return true;
  }
  function receive(payload) {
    if(payload.type==='presentation-md') {
      const sentAt=Number(payload.sentAt)||0;
      if(Date.now()-sentAt>45000||sentAt>Date.now()+30000||sentAt<lastSentAt)return;
      if(!payload.presenting){lastSentAt=sentAt;received=Date.now();stop(payload.error||'Waiting for the presenter to start presenting.');return;}
      if(payload.format!=='rendered-v1'){stop('Restart the updated Keynope app to share this presentation.');return;}
      if(payload.timerEndMs!=null && (!Number.isSafeInteger(payload.timerEndMs)||payload.timerEndMs<0))return;
      if(!/^[a-f0-9]{64}$/.test(payload.transfer)||!Number.isInteger(payload.parts)||payload.parts<1||payload.parts>KeynopePresentationTransfer.maxParts||!Number.isInteger(payload.page)||payload.page<0||!Number.isInteger(payload.slide)||payload.slide<0||!Number.isInteger(payload.slideCount)||payload.slideCount<1)return;
      const c=payload.buildCursor;
      if(c!=null&&(!c||typeof c.session!=='string'||!c.session||c.session.length>256||typeof c.slideId!=='string'||!c.slideId||c.slideId.length>256||!Number.isSafeInteger(c.generation)||c.generation<0||!Number.isSafeInteger(c.step)||c.step<0||c.step>1000||!Number.isSafeInteger(c.startedAtMs)||c.startedAtMs<0||!['enter','advance','seek'].includes(c.motion)||typeof c.paused!=='boolean'))return;
      if(c&&manifest?.buildCursor?.session===c.session&&c.generation<manifest.buildCursor.generation)return;
      lastSentAt=sentAt;received=Date.now();
      if(manifest?.transfer!==payload.transfer){renderID++;rendered='';lastRequest=0;}
      manifest=payload;
      register(payload);
      if(cache.has(payload.transfer))render();else if(!pendingPage)showStatus('Receiving presentation page…');
    }else if(payload.type==='presentation-preload'){
      const age=Date.now()-Number(payload.sentAt);
      if(payload.format==='rendered-v1'&&manifest&&Number.isFinite(age)&&age>=-30000&&age<=45000)register(payload);
    }else if(payload.type==='presentation-part'){
      const transfer=transfers.get(payload.transfer);
      if(!transfer||!Number.isInteger(payload.index)||payload.index<0||payload.index>=transfer.count||typeof payload.data!=='string'||payload.data.length>KeynopePresentationTransfer.chunkSize||!/^[A-Za-z0-9+/=]+$/.test(payload.data))return;
      transfer.parts[payload.index]=payload.data;finish(payload.transfer);
    }
  }
  setInterval(()=>{
    if(received&&Date.now()-received>45000){stop('Presentation connection interrupted — waiting for the presenter.');received=0;}
    if(manifest&&!cache.has(manifest.transfer)&&Date.now()-lastRequest>8000){lastRequest=Date.now();request({type:'presentation-request',transfer:manifest.transfer});}
  },2000);
  return {receive,preload:startRenderer,setVisible(value){visible=value;if(value){if(!manifest)standby();render();}renderer?.setVisible(value&&!!manifest&&!!pendingPage);},reset(){stop('Waiting for the presenter to start presenting.');cache.clear();cacheBytes=0;received=0;lastSentAt=0;lastRequest=0;}};
}
