import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { join } from 'node:path';
import test from 'node:test';
import { startSafariDriver } from './native-safari-driver.mjs';

const program = `
const http=require('node:http'),fs=require('node:fs');
const [port,scenario,file]=process.argv.slice(1);
if(scenario==='hung-delete')process.on('SIGTERM',()=>{});
http.createServer((request,response)=>{
 fs.appendFileSync(file,request.method+' '+request.url+'\\n');
 request.resume();
 if(request.url==='/session'&&scenario==='denied'){
  response.writeHead(500,{'content-type':'application/json'}).end(JSON.stringify({value:{error:'session not created',message:'Allow remote automation is disabled'}}));return;
 }
 if(request.method==='DELETE'&&scenario==='hung-delete')return;
 const value=request.url==='/session'?{sessionId:'owned-session',capabilities:{browserName:'fake for lifecycle test'}}:{};
 response.writeHead(200,{'content-type':'application/json'}).end(JSON.stringify({value}));
}).listen(Number(port),'127.0.0.1');
`;
for (const scenario of ['denied', 'success', 'hung-delete']) test(`owned Safari driver cleanup: ${scenario}`, { timeout: 15000 }, async () => {
  const directory = await mkdtemp('/tmp/whip-safari-driver-test-'), file = join(directory, 'requests');
  let child, driver;
  try {
    const launch = port => { child = spawn(process.execPath, ['-e', program, String(port), scenario, file], { stdio: ['ignore', 'pipe', 'pipe'] }); return child; };
    if (scenario === 'denied') await assert.rejects(startSafariDriver({ launch }), /Allow remote automation/);
    else {
      driver = await startSafariDriver({ launch });
      if (scenario === 'hung-delete') await assert.rejects(driver.close(), /timeout|aborted/i);
      else await driver.close();
      await driver.close();
    }
    assert(child.exitCode !== null || child.signalCode !== null, 'Owned child must be joined');
    const requests = (await readFile(file, 'utf8')).trim().split('\n');
    assert.equal(requests.filter(value => value === 'POST /session').length, 1, 'Never retries uncertain session creation');
    assert.equal(requests.filter(value => value === 'DELETE /session/owned-session').length, scenario === 'denied' ? 0 : 1);
  } finally { try { await driver?.close(); } finally { if (child && child.exitCode === null && child.signalCode === null) { child.kill('SIGKILL'); await new Promise(resolve => child.once('exit', resolve)); } await rm(directory, { recursive: true, force: true }); } }
});
