'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const {spawnSync} = require('node:child_process');
const script = path.join(__dirname, 'test_native_acceptance.sh');
const flags = {
  'activity-builds': 'ACTIVITY_BUILD', builds: 'BUILD', 'pptx-timing': 'PPTX_TIMING',
  'presentation-tools': 'PRESENTATION_TOOLS', shapes: 'SHAPE', 'stroke-routes': 'STROKE_ROUTE',
  svg: 'SVG', 'table-tokens': 'TABLE_TOKEN', 'theme-colours': 'THEME_COLOUR', transitions: 'TRANSITION'
};
const run = args => spawnSync('zsh', [script, ...args], {encoding: 'utf8'});
test('native case listing and dry runs never launch the acceptance app', {skip: process.platform !== 'darwin'}, () => {
  const listed = run(['--list']);
  assert.equal(listed.status, 0, listed.stderr);
  assert.deepEqual(listed.stdout.trim().split('\n').slice(2), Object.keys(flags));
  for (const [name, flag] of Object.entries(flags)) {
    const result = run([name, '--dry-run']);
    assert.equal(result.status, 0, result.stderr);
    const lines = result.stdout.trim().split('\n');
    assert.equal(lines.at(-1), `KEYNOPE_NATIVE_${flag}_ACCEPTANCE=1 zsh tools/test_native_pdf_export.sh`);
    assert.equal(lines.length, name === 'svg' ? 2 : 1);
    if (name === 'svg') assert.match(lines[0], /KEYNOPE_SVG_EVIDENCE=.*go test -run TestSVG/);
  }
});
test('native dispatcher rejects missing, unknown and extra arguments before launching', {skip: process.platform !== 'darwin'}, () => {
  for (const args of [[], ['missing'], ['shapes', '--wrong'], ['shapes', '--dry-run', 'extra']]) {
    const result = run(args);
    assert.equal(result.status, 2, result.stderr);
    assert.equal(result.stdout, '');
  }
});
