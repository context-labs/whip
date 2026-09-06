// Optional real-browser acceptance: see docs/protocol-v2.md.
import { readFile, writeFile, rm } from 'node:fs/promises';
import { pathToFileURL } from 'node:url';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
const info = JSON.parse(await readFile(process.argv[2], 'utf8'));
const signing = JSON.parse(await readFile(new URL('../schema/signing-fixture.json', import.meta.url), 'utf8'));
const playwright = await import(process.env.WHIP_PLAYWRIGHT_MODULE ? pathToFileURL(process.env.WHIP_PLAYWRIGHT_MODULE).href : 'playwright');

async function smoke(info, signing) {
  const socket = new WebSocket(info.endpoint);
  await new Promise((resolve, reject) => {
    const timeout = setTimeout(() => reject(new Error('WebSocket open timeout')), 10000);
    socket.onopen = () => { clearTimeout(timeout); resolve(); };
    socket.onerror = () => { clearTimeout(timeout); reject(new Error('WebSocket failed')); };
  });
  let id = 0;
  const pending = new Map();
  socket.onmessage = event => {
    const envelope = JSON.parse(event.data);
    if (!envelope.id) return;
    const continuation = pending.get(envelope.id);
    if (!continuation) return;
    pending.delete(envelope.id);
    if (envelope.error) continuation.reject(new Error(JSON.stringify(envelope.error)));
    else continuation.resolve(envelope.result);
  };
  const rpc = (method, params) => new Promise((resolve, reject) => {
    const requestID = String(++id);
    const timeout = setTimeout(() => { pending.delete(requestID); reject(new Error('RPC timeout: ' + method)); }, 15000);
    pending.set(requestID, { resolve: value => { clearTimeout(timeout); resolve(value); }, reject: error => { clearTimeout(timeout); reject(error); } });
    socket.send(JSON.stringify({ jsonrpc: '2.0', id: requestID, method, params }));
  });
  try {
    const initialized = await rpc('initialize', { protocol_major: 2, build_id: 'browser-smoke', client_id: crypto.randomUUID(), client_kind: 'human' });
    if (initialized.protocol_major !== 2 || typeof initialized.generation !== 'string') throw new Error('invalid initialization');
    const commandID = crypto.randomUUID();
    let result = await rpc('command.submit', { command_id: commandID, scope: 'root', root_id: info.root_id, operation: 'submit', payload: { text: 'browser round trip' } });
    const deadline = Date.now() + 15000;
    while (['queued', 'running', 'waiting'].includes(result.status) && Date.now() < deadline) {
      await new Promise(resolve => setTimeout(resolve, 30));
      result = await rpc('command.status', { command_id: commandID });
    }
    if (result.status !== 'succeeded' || result.result.text !== 'browser round trip') throw new Error('command did not converge');
    const snapshot = await rpc('root.snapshot', { root_id: info.root_id });
    if (typeof snapshot.cursor !== 'string') throw new Error('snapshot cursor lost precision');
    await rpc('events.subscribe', { root_id: info.root_id, subscription_id: 'browser-view', cursor: snapshot.cursor });
    await rpc('events.unsubscribe', { subscription_id: 'browser-view' });
    const encoder = new TextEncoder();
    const bytes = value => Uint8Array.from(atob(value), char => char.charCodeAt(0));
    const prefix = encoder.encode('whip privileged request v2\0' + signing.method + signing.generation + '\0');
    const nonce = bytes(signing.nonce);
    const payload = encoder.encode(signing.payload);
    const input = new Uint8Array(prefix.length + nonce.length + payload.length);
    input.set(prefix); input.set(nonce, prefix.length); input.set(payload, prefix.length + nonce.length);
    const digest = await crypto.subtle.digest('SHA-256', input);
    const key = await crypto.subtle.importKey('raw', bytes(signing.public_key), { name: 'Ed25519' }, false, ['verify']);
    if (!await crypto.subtle.verify('Ed25519', key, bytes(signing.signature), digest)) throw new Error('signature mismatch');
    const content = encoder.encode('browser HTTP upload/download');
    const contentHash = Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256', content)), value => value.toString(16).padStart(2, '0')).join('');
    const httpBase = info.endpoint.replace(/^ws/, 'http').replace(/\/api\/v2\/ws$/, '');
    const uploaded = await fetch(httpBase + '/api/v2/content/upload?root_id=' + encodeURIComponent(info.root_id), { method: 'POST', headers: { 'Content-Type': 'text/plain', 'X-Content-SHA256': contentHash }, body: content });
    if (!uploaded.ok) throw new Error('upload failed: ' + uploaded.status);
    const handle = await uploaded.json();
    const downloaded = await fetch(httpBase + '/api/v2/content/' + handle.reference_id + '?root_id=' + encodeURIComponent(info.root_id));
    if (!downloaded.ok || await downloaded.text() !== 'browser HTTP upload/download') throw new Error('download mismatch');
    return { user_agent: navigator.userAgent, protocol_major: initialized.protocol_major, command_status: result.status, event_cursor: snapshot.cursor, ed25519: true, http_transfer: true };
  } finally { socket.close(); }
}

const results = {};
try {
  for (const name of ['chromium', 'firefox']) {
    const browser = await playwright[name].launch({ headless: true });
    try {
      const page = await browser.newPage();
      await page.goto(info.frontend);
      results[name] = { passed: true, ...await page.evaluate(({ info, signing, code }) => (0, eval)(`(${code})`)(info, signing), { info, signing, code: smoke.toString() }) };
    } catch (error) { results[name] = { passed: false, error: String(error) }; }
    finally { await browser.close(); }
  }
  if (process.env.WHIP_SAFARIDRIVER_URL) {
    const base = process.env.WHIP_SAFARIDRIVER_URL;
    let sessionID;
    const webdriver = async (path, body) => {
      const response = await fetch(base + path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
      const result = await response.json();
      if (!response.ok || result.value?.error) throw new Error(JSON.stringify(result.value));
      return result.value;
    };
    try {
      const created = await webdriver('/session', { capabilities: { alwaysMatch: { browserName: 'safari' } } });
      sessionID = created.sessionId;
      await webdriver(`/session/${sessionID}/timeouts`, { script: 30000 });
      await webdriver(`/session/${sessionID}/url`, { url: info.frontend });
      const result = await webdriver(`/session/${sessionID}/execute/async`, { script: `const done=arguments[arguments.length-1]; (${smoke.toString()})(arguments[0],arguments[1]).then(value=>done({value}),error=>done({error:String(error)}));`, args: [info, signing] });
      if (result.error) throw new Error(result.error);
      results.safari = { passed: true, ...result.value };
    } catch (error) { results.safari = { passed: false, error: String(error) }; }
    finally { if (sessionID) await fetch(base + '/session/' + sessionID, { method: 'DELETE' }); }
  } else results.safari = { passed: false, error: 'WHIP_SAFARIDRIVER_URL not configured; Safari not run' };
  if (!results.safari.passed && process.platform === 'darwin') {
    try {
      const document = '<!doctype html><meta charset="utf-8"><title>WHIP Safari protocol smoke</title><p id="status">Running isolated WHIP protocol checks…</p><script>' +
        '(' + smoke.toString() + ')(' + JSON.stringify(info) + ',' + JSON.stringify(signing).replaceAll('<', '\\u003c') + ')' +
        '.then(value=>({passed:true,...value}),error=>({passed:false,error:String(error)}))' +
        '.then(async result=>{document.getElementById("status").textContent=result.passed?"WHIP Safari protocol checks passed. This tab can be closed.":JSON.stringify(result);await fetch("/safari-result",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify(result)});});</script>';
      const pagePath = process.argv[2].replace(/bridge.json$/, 'safari-smoke.html');
      const resultPath = process.argv[2].replace(/bridge.json$/, 'safari-result.json');
      await rm(resultPath, { force: true });
      await writeFile(pagePath, document);
      await promisify(execFile)('open', ['-a', 'Safari', info.frontend + '/safari-smoke']);
      const deadline = Date.now() + 45000;
      let received = false;
      while (Date.now() < deadline) {
        try { results.safari = JSON.parse(await readFile(resultPath, 'utf8')); received = true; break; }
        catch { await new Promise(resolve => setTimeout(resolve, 250)); }
      }
      if (!received) throw new Error('Safari page did not report a result within 45 seconds');
    } catch (error) { results.safari = { passed: false, error: String(error) }; }
  }

} finally {
  await fetch(info.frontend + '/done', { method: 'POST' }).catch(() => {});
  await writeFile(process.argv[2].replace(/bridge.json$/, 'browser-results.json'), JSON.stringify(results, null, 2) + '\n');
  console.log(JSON.stringify(results, null, 2));
}
if (Object.values(results).some(result => !result.passed)) process.exitCode = 1;
