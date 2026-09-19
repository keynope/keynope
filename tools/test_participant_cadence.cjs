// Exercise the generated participant loop with the native wake-delay function.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync('main.go', 'utf8');
const builder = fs.readFileSync('tools/build_participant_renderer.cjs', 'utf8');
const cadence = source.slice(source.indexOf('function contentAnimationWakeDelayMS()'), source.indexOf('function tick()'));
const suffix = builder.split('const suffix=`')[1].split('\n`;')[0];
let now = 0, pending, frames = null, page = {}, draws = 0, width = 800, disconnected = false;
const context = vm.createContext({
  performance: {now: () => now},
  host: {getBoundingClientRect: () => ({width, height: 450})},
  realDocument: {hidden: false},
  ResizeObserver: class {observe() {} disconnect() {disconnected = true;}},
  KeynopeTrueType: {ready: {then() {}}},
  setTimeout(fn, delay) {pending = {fn, delay}; return 1;},
  clearTimeout() {pending = null;},
  keynopeEditorSelectionActive: false,
  keynopeAppSurface: false,
  presenterTimerMode: '', presenterTransitionUntil: 0,
  editorExportConfirmation: null, modernPageView: null,
  presenterPageAt: () => page, decodedContentFrames: () => frames,
  pageIndex: 0, frame: 0, contentAnimationElapsedMS: 0,
  drawFrame() {draws++;}, resize() {}, render() {}, clearModernPage() {},
});
const renderer = vm.runInContext(cadence + '\n(function(){' + suffix.slice(0, suffix.lastIndexOf('}')) + '})()', context);
const step = () => {const task = pending; now += task.delay; task.fn();};
assert.equal(pending.delay, 70, 'same initial cadence as native');
renderer.setVisible(true);
for (let i = 0; i < 10; i++) {assert.equal(pending.delay, 70); step();}
assert.equal(draws, 10);
assert.equal(context.frame, 10, 'effects advance ten frames in 700ms, not 23');
assert.equal(context.contentAnimationElapsedMS, 700);
frames = [{delayMs: 12}, {delayMs: 12}];
step();
assert.equal(pending.delay, 10, 'honor remaining GIF frame deadline');
step();
assert.equal(pending.delay, 12);
renderer.setVisible(false);
const pausedFrame = context.frame;
step();
assert.equal(context.frame, pausedFrame);
assert.equal(pending.delay, 70, 'hidden views do not spin at GIF cadence');
renderer.setVisible(true);
context.realDocument.hidden = true;
step();
assert.equal(context.frame, pausedFrame);
context.realDocument.hidden = false;
width = 0;
step();
assert.equal(context.frame, pausedFrame);
width = 800;
frames = null;
context.keynopeAppSurface = true;
renderer.setVisible(true);
step();
assert.equal(pending.delay, 1000, 'static native editor surfaces use a sparse safety wake-up');
context.presenterTimerMode = 'running';
step();
assert.equal(pending.delay, 70, 'native timers retain the live cadence');
context.presenterTimerMode = '';
page = {scene:{}};
context.modernPageView = {resourceCounts: () => ({animationTracks: 1})};
step();
assert.equal(pending.delay, 70, 'native animated media retain the live cadence');
renderer.destroy();
assert.equal(pending, null);
assert(disconnected);
console.log('PASS: native effect cadence, GIF deadlines, hidden-view suspension, cleanup');
