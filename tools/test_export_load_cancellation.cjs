// Own the stalled resource and native accessory subprocess as one test.
const http = require('node:http');
const { spawn } = require('node:child_process');
if (!process.argv[2]) throw Error('Pass the compiled native load-check executable');
let started = false;
const server = http.createServer((req, res) => {
  if (req.url === '/stall.png') { started = true; return; }
  if (req.url === '/started') { res.writeHead(started ? 200 : 404); return res.end(); }
  res.writeHead(404); res.end();
});
server.listen(0, '127.0.0.1', () => {
  const child = spawn(process.argv[2], [`http://127.0.0.1:${server.address().port}/`], { stdio: 'inherit' });
  const timeout = setTimeout(() => { console.error('Native load test timed out'); child.kill('SIGTERM'); }, 60000);
  const finish = code => { clearTimeout(timeout); server.closeAllConnections(); server.close(); process.exitCode = code; };
  child.on('error', error => { console.error(error); finish(1); });
  child.on('exit', code => finish(code === 0 ? 0 : 1));
});
