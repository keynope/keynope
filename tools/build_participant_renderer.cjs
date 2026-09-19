// Generate a trusted, reusable renderer from the same canvas engine as exports.
// Received presentation data is JSON only; it is never evaluated as HTML/JS.
const fs=require('node:fs');
const path=require('node:path');
const crypto=require('node:crypto');
const html=fs.readFileSync(process.argv[2],'utf8');
let modernFontFile='modern-fonts.css';
if(process.argv[4]){
  const css=fs.readFileSync(process.argv[4]);
  modernFontFile='modern-fonts.'+crypto.createHash('sha256').update(css).digest('hex').slice(0,20)+'.css';
  fs.writeFileSync(path.join(path.dirname(process.argv[3]),modernFontFile),css);
}
const fontStart=html.indexOf('const keynopeTTFFontData = ');
const rendererStart=html.indexOf("const deck = JSON.parse(document.getElementById('keynope-data').textContent);");
if(fontStart<0||fontStart>=rendererStart)throw Error('Export TrueType dependency not found');
// Share the exact embedded font and drawing implementation used by exports.
// It lives at module scope so all participant tabs share one loaded font.
const trueType=html.slice(fontStart,rendererStart);
let script=html.slice(html.indexOf("const deck = JSON.parse(document.getElementById('keynope-data').textContent);"),html.lastIndexOf('</script>'));
if(!script.startsWith('const deck ='))throw Error('Export renderer not found');
script=script.replace("const deck = JSON.parse(document.getElementById('keynope-data').textContent);",'const deck = initialDeck;');
script=script.replace(/resize\(\);\s*render\(\);\s*tick\(\);\s*$/, '');
const prefix=`export function createSlideRenderer(host) {
const realDocument=host.ownerDocument;
const root=host.attachShadow({mode:'open'});
root.innerHTML='<style>:host{display:block;position:relative;width:100%;aspect-ratio:16/9;overflow:hidden;background:#000}#stage{position:absolute;inset:0;overflow:hidden}canvas{position:absolute;left:50%;top:50%;transform:translate(-50%,-50%)}#link-layer,#activity-marker,.terminal-layer{display:none!important}</style><div id="stage"><canvas id="presenter-canvas"></canvas><div id="link-layer"></div><div id="effect-layer" class="terminal-layer"></div><div id="content-layer" class="terminal-layer"></div><div id="chrome-layer" class="terminal-layer"></div><button id="activity-marker" hidden></button></div>';
const document=new Proxy(realDocument,{get(target,key){if(key==='documentElement'||key==='body')return host;if(key==='getElementById')return id=>root.getElementById(id);if(key==='querySelector'||key==='querySelectorAll')return root[key].bind(root);if(key==='addEventListener')return ()=>{};const value=target[key];return typeof value==='function'?value.bind(target):value;}});
const window={devicePixelRatio:globalThis.devicePixelRatio||1,KEYNOPE_PRESENTER:false,KEYNOPE_MODERN_FONT_URL:new URL(${JSON.stringify('./'+modernFontFile)},import.meta.url).href};
const location={search:'',hash:''};
const addEventListener=()=>{},setInterval=()=>0;
let innerWidth=1,innerHeight=1;
const initialDeck={cols:245,rows:56,pages:[{slide:0,page:0,pageCount:1,slideCount:1,fg:'#fff',bg:'#000',effect:'none',lines:[]}]};
`;
const suffix=`
let active=false,destroyed=false,lastTick=performance.now();
function paint(){const rect=host.getBoundingClientRect();if(!active||rect.width<1||rect.height<1)return;innerWidth=rect.width;innerHeight=rect.height;resize();render();}
KeynopeTrueType.ready.then(()=>{if(!destroyed)paint();});
const observer=new ResizeObserver(paint);observer.observe(host);
// Use the native/export cadence, including GIF frame deadlines. A fixed 30 ms
// interval makes frame-driven distortion and effects run over twice as fast.
let timer;
function participantTick(){
  if(destroyed)return;
  const now=performance.now();
  const visible=active&&host.getBoundingClientRect().width>0&&!realDocument.hidden;
  if(visible){contentAnimationElapsedMS+=Math.max(0,Math.min(1000,now-lastTick));frame++;try{drawFrame();}catch(_err){presenterTransitionUntil=0;presenterContext.globalAlpha=1;drawPresenterPageFallback();}}
  lastTick=now;
  timer=globalThis.setTimeout(participantTick,visible?contentAnimationWakeDelayMS():70);
}
timer=globalThis.setTimeout(participantTick,contentAnimationWakeDelayMS());
return {setPage(data,index,{timerEndMs=0}={}){if(!data||!Array.isArray(data.pages)||!data.pages[index])throw Error('Invalid rendered presentation page');presenterTimerEndMS=Number.isSafeInteger(timerEndMs)&&timerEndMs>0?timerEndMs:0;presenterTimerMode=presenterTimerEndMS?'running':'';presenterTimerInput='';deck.cols=data.cols;deck.rows=data.rows;deck.pages=data.pages;pageIndex=index;frame=0;contentAnimationElapsedMS=0;contentAnimationPageIndex=-1;contentAnimationCache.clear();effectState.clear();active=true;lastTick=performance.now();paint();},setVisible(value){active=value;lastTick=performance.now();if(value)paint();},destroy(){active=false;destroyed=true;observer.disconnect();clearModernPage();globalThis.clearTimeout(timer);}};
}
`;
fs.writeFileSync(process.argv[3],trueType+prefix+script+suffix);
