import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { createServer } from 'node:net';
import { eventually } from './native-fixture.mjs';

const launchSafari = port => spawn('/usr/bin/safaridriver', ['--port', String(port)], { stdio: ['ignore', 'pipe', 'pipe'] });

/** Actual Safari's disposable WebDriver session. Never enables Remote Automation.
 * Tests inject only the owned process boundary to prove failed-start cleanup. */
export async function startSafariDriver({ launch = launchSafari } = {}) {
  if (launch === launchSafari) assert.equal(process.platform, 'darwin', 'Actual Safari requires macOS; WebKit is different coverage');
  const reservation = createServer();
  await new Promise((resolve, reject) => { reservation.once('error', reject); reservation.listen(0, '127.0.0.1', resolve); });
  const port = reservation.address().port;
  await new Promise((resolve, reject) => reservation.close(error => error ? reject(error) : resolve()));
  const driverProcess = launch(port);
  let log = '', session, stopped = false;
  const exited = new Promise(resolve => { driverProcess.once('exit', (code, signal) => { stopped = true; resolve({ code, signal }); }); driverProcess.once('error', error => { stopped = true; resolve({ error: error.message }); }); });
  const record = value => { log = (log + value.toString()).slice(-65536); };
  driverProcess.stdout.on('data', record); driverProcess.stderr.on('data', record);
  const request = async (method, path, body, timeout = 15000) => {
    assert(path.startsWith('/') && !path.includes('..'));
    const response = await fetch(`http://127.0.0.1:${port}${path}`, { method, signal: AbortSignal.timeout(timeout),
      headers: body === undefined ? {} : { 'Content-Type': 'application/json' }, ...(body === undefined ? {} : { body: JSON.stringify(body) }) });
    const reader = response.body.getReader(); let bytes = 0; const parts = [];
    try {
      for (;;) { const { value, done } = await reader.read(); if (done) break; bytes += value.byteLength; if (bytes > 8 << 20) throw new Error('Safari reply exceeds 8MiB'); parts.push(value); }
    } finally { await reader.cancel(); }
    const result = JSON.parse(Buffer.concat(parts).toString());
    if (!response.ok || result.value?.error) throw new Error(`Safari WebDriver ${result.value?.error ?? response.status}: ${String(result.value?.message ?? '').slice(0, 4096)}`);
    return result.value;
  };
  const close = async () => {
    let failure;
    try { if (session && !stopped) await request('DELETE', `/session/${session}`, undefined, 5000); }
    catch (error) { failure = error; }
    finally {
      session = undefined;
      if (!stopped) driverProcess.kill('SIGTERM');
      let timer; const result = await Promise.race([exited, new Promise(resolve => { timer = setTimeout(() => resolve(null), 1000); })]); clearTimeout(timer);
      if (!result && !stopped) { driverProcess.kill('SIGKILL'); await exited; }
    }
    if (failure) throw failure;
  };
  try {
    await eventually(async () => { if (stopped) throw new Error(`Safari driver exited: ${log}`); return await request('GET', '/status', undefined, 1000); }, { timeout: 10000, description: 'owned Safari driver startup' });
    const value = await request('POST', '/session', { capabilities: { alwaysMatch: { browserName: 'safari' } } });
    assert(typeof value.sessionId === 'string' && /^[A-Za-z0-9-]{1,128}$/.test(value.sessionId)); session = value.sessionId;
    await request('POST', `/session/${session}/timeouts`, { script: 15000, pageLoad: 15000, implicit: 0 });
    return { capabilities: value.capabilities,
      call: (method, path, body) => request(method, `/session/${session}${path}`, body),
      evaluate: (callback, ...args) => request('POST', `/session/${session}/execute/sync`, { script: `return (${callback.toString()})(...arguments)`, args }),
      async click(selector, text) {
        const element = await request('POST', `/session/${session}/execute/sync`, {
          script: 'return [...document.querySelectorAll(arguments[0])].find(element => !element.disabled && (arguments[1] === null || (element.getAttribute("aria-label") ?? element.textContent.trim()) === arguments[1]))', args: [selector, text ?? null],
        });
        const id = element?.['element-6066-11e4-a52e-4f735466cecf'];
        assert(typeof id === 'string' && id.length <= 256, 'Safari element identity missing');
        await request('POST', `/session/${session}/element/${encodeURIComponent(id)}/click`, {});
      },
      close, get log() { return log; },
    };
  } catch (error) { await close(); throw error; }
}
