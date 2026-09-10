const assert = require('node:assert/strict');
require('../web/participant-transfer.js');

(async () => {
  const transport = KeynopePresentationTransfer;
  const text = 'Hello 😍\n' + Array.from({length:10000}, () => crypto.randomUUID()).join('\n');
  const packed = await transport.pack(text);
  assert.ok(packed.parts.length > 1);
  assert.equal(await transport.unpack(packed.parts, packed.id), text);
  for (const data of packed.parts) {
    const body=JSON.stringify({protocol:'keynope-activity-v1',code:'abcdefgh',sessionId:'x'.repeat(128),type:'presentation-part',transfer:packed.id,index:63,data});
    assert.ok(new TextEncoder().encode(body).length <= 32768, 'Part including channel envelope must fit signed MESSAGE limit');
  }
  await assert.rejects(transport.unpack(packed.parts, '0'.repeat(64)), /Incomplete/);
  await assert.rejects(transport.pack('x'.repeat(16*1024*1024+1)), /too large/);
  console.log('Participant transfer: Unicode, chunks, packet limits, integrity and size bounds passed.');
})().catch(error => { console.error(error); process.exitCode = 1; });
