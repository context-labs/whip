import assert from 'node:assert/strict';
import { createServer as createTLS } from 'node:https';
import { createServer as createTCP, connect } from 'node:net';
import { createSocket } from 'node:dgram';
import { readFile } from 'node:fs/promises';
import path from 'node:path';
import { app, BrowserWindow, session } from 'electron';
import { createPreviewProxy } from '../src/preview-proxy.ts';
import { previewFixture } from './preview-fixture.mjs';

app.setPath('userData', process.env.PREVIEW_PROOF_USER_DATA);
app.commandLine.appendSwitch('disable-quic');
app.on('window-all-closed', () => {});
async function main() {
  await app.whenReady();
  const fixture = await previewFixture();
  const tlsPath = path.join(path.dirname(process.env.PREVIEW_PROOF_KEY), 'tls-route');
  const tls = createTLS({ key: await readFile(process.env.PREVIEW_PROOF_KEY), cert: await readFile(process.env.PREVIEW_PROOF_CERT) }, (_req, res) => res.end('tls fixture'));
  await new Promise(resolve => tls.listen(tlsPath, resolve));
  const decoy = createTCP(socket => { socket.destroy(); });
  await new Promise(resolve => decoy.listen(0, '127.0.0.1', resolve)); const tlsPort = decoy.address().port;
  const tlsRoute = { port: tlsPort, remoteHost: '127.0.0.1', open: signal => new Promise((resolve, reject) => {
    const socket = connect({ path: tlsPath, signal }); socket.once('error', reject);
    socket.once('connect', () => { socket.removeListener('error', reject); resolve(socket); });
  }) };
  const udp = createSocket('udp4'); let udpPackets = 0; udp.on('message', () => udpPackets++);
  await new Promise(resolve => udp.bind(0, '127.0.0.1', resolve)); const udpPort = udp.address().port;
  const proxy = await createPreviewProxy({ routes: [fixture.route, tlsRoute] });
  const partition = session.fromPartition(`preview-proof-${process.pid}`);
  partition.setPermissionRequestHandler((_wc, _permission, answer) => answer(false));
  partition.setPermissionCheckHandler(() => false); partition.setDevicePermissionHandler(() => false);
  await partition.setProxy({ mode: 'fixed_servers', proxyRules: proxy.proxyRules, proxyBypassRules: proxy.proxyBypassRules });
  const window = new BrowserWindow({ show: false, webPreferences: { session: partition, sandbox: true,
    contextIsolation: true, nodeIntegration: false, webviewTag: false, webSecurity: true } });
  const wc = window.webContents; wc.setWebRTCIPHandlingPolicy('disable_non_proxied_udp');
  wc.setWindowOpenHandler(() => ({ action: 'deny' }));
  let challenges = 0;
  const login = (event, contents, _details, info, answer) => {
    event.preventDefault();
    if (contents !== wc || contents.session !== partition || !info.isProxy || info.host !== proxy.host ||
      info.port !== proxy.port || info.scheme !== 'basic' || info.realm !== proxy.realm) { answer(); return; }
    challenges++; answer(proxy.username, proxy.password);
  };
  app.on('login', login);
  const evaluate = code => wc.executeJavaScript(code);
  try {
    await wc.loadURL(`http://localhost:${fixture.port}/page`);
    assert.equal(await evaluate('location.origin'), `http://localhost:${fixture.port}`);
    const json = await evaluate('fetch("/json", {headers:{"X-Proof":"yes"}}).then(r=>r.json())');
    assert.equal(json.source, 'private-route'); assert.equal(json.host, `localhost:${fixture.port}`); assert.equal(json.proxyAuthLeaked, false);
    const protocols = await evaluate(`Promise.all([
      new Promise((resolve,reject)=>{const ws=new WebSocket('ws://localhost:${fixture.port}/ws');window.proofWS=ws;ws.onmessage=e=>resolve(e.data);ws.onerror=()=>reject(new Error('websocket'));}),
      new Promise((resolve,reject)=>{const es=new EventSource('/sse');window.proofSSE=es;es.onmessage=e=>resolve(e.data);es.onerror=()=>reject(new Error('sse'));})
    ])`);
    assert.deepEqual(protocols, ['remote-websocket', 'remote-sse']);
    const blocked = await evaluate(`Promise.all([
      fetch('http://localhost:${fixture.deniedPort}/fetch'),fetch('http://127.0.0.1:${fixture.deniedPort}/fetch'),
      fetch('http://[::1]:${fixture.deniedPort}/fetch'),fetch('http://[::1]:${fixture.port}/same-port-cross-family'),fetch('/redirect'),fetch('http://169.254.169.254/')
    ].map(p=>p.then(r=>r.status===403,()=>true)))`);
    assert.ok(blocked.every(Boolean));
    const sw = await evaluate(`(async()=>{await navigator.serviceWorker.register('/worker.js');await navigator.serviceWorker.ready;
      if(!navigator.serviceWorker.controller)await new Promise(r=>navigator.serviceWorker.addEventListener('controllerchange',r,{once:true}));
      return await new Promise(r=>{navigator.serviceWorker.addEventListener('message',e=>r(e.data),{once:true});navigator.serviceWorker.controller.postMessage('test');});})()`);
    assert.ok(sw.blocked || sw.status === 403);
    const rtc = await evaluate(`(async()=>{const pc=new RTCPeerConnection({iceServers:[{urls:'stun:127.0.0.1:${udpPort}'}]});const candidates=[];pc.onicecandidate=e=>{if(e.candidate)candidates.push(e.candidate.candidate);};pc.createDataChannel('probe');await pc.setLocalDescription(await pc.createOffer());await new Promise(r=>setTimeout(r,1200));pc.close();return candidates;})()`);
    assert.deepEqual(rtc, []); assert.equal(udpPackets, 0);
    const transport = await evaluate(`(async()=>{if(typeof WebTransport!=='function')return 'unavailable';const wt=new WebTransport('https://127.0.0.1:${udpPort}/');const result=await Promise.race([wt.ready.then(()=>'CONNECTED',()=>'blocked'),new Promise(r=>setTimeout(()=>r('timeout'),1200))]);wt.close();return result;})()`);
    assert.notEqual(transport, 'CONNECTED'); assert.equal(udpPackets, 0);
    await assert.rejects(wc.loadURL(`https://localhost:${tlsPort}/`), /ERR_CERT_AUTHORITY_INVALID/);
    await wc.loadURL(`http://localhost:${fixture.port}/page`);
    assert.equal(fixture.localHits, 0);
    assert.ok(challenges > 0); assert.ok(fixture.hits.every(hit => !hit.auth));
    // Same-family aliases retain their logical URL; an explicit other-family literal is denied.
    for (const host of ['127.0.0.1']) {
      await wc.loadURL(`http://${host}:${fixture.port}/page`);
      assert.equal(await evaluate('location.hostname'), host);
      assert.equal(await evaluate('fetch("/json").then(r=>r.json()).then(r=>r.source)'), 'private-route');
    }
    await wc.loadURL(`http://localhost:${fixture.port}/page`);
    await evaluate(`window.proofClosed = new Promise(r=>{const ws=new WebSocket('ws://localhost:${fixture.port}/ws');window.proofWS=ws;ws.onclose=()=>r(true);}); new Promise(r=>{window.proofWS.onmessage=()=>r(true);})`);
    proxy.revoke(fixture.port);
    assert.equal(await evaluate('window.proofClosed'), true);
    assert.equal(await evaluate('fetch("/revoked").then(r=>r.status,()=>"blocked")'), 403);
    await proxy.close(); await partition.closeAllConnections(); await partition.clearCache();
    await assert.rejects(wc.loadURL(`http://localhost:${fixture.port}/no-direct-fallback`), /ERR_PROXY_CONNECTION_FAILED|ERR_TUNNEL_CONNECTION_FAILED/);
    assert.equal(fixture.localHits, 0);
    console.log(JSON.stringify({ proof: 'electron-preview-network', electron: process.versions.electron, chromium: process.versions.chrome,
      http: true, logicalOrigin: true, ipv4: true, samePortCrossFamilyDenied: true, websocket: true, sse: true,
      deniedFetchAndRedirect: true, deniedServiceWorker: true, authenticatedProxy: true,
      socketRevocation: true, noLoopbackBypass: true, noDirectFallback: true, localDecoyHits: fixture.localHits,
      connectTLSValidation: true, webRTCLoopbackUDP: udpPackets, webRTCICECandidates: rtc.length, webTransport: transport,
      scope: 'real Electron; deterministic Unix endpoint, not connected SSH or packaged acceptance' }));
  } finally {
    app.removeListener('login', login); window.destroy(); await proxy.close(); await fixture.close();
    await partition.clearStorageData(); await partition.closeAllConnections();
    udp.close(); tls.closeAllConnections(); await Promise.all([tls, decoy].map(server => new Promise(resolve => server.close(resolve))));
  }
}
main().then(() => app.exit(0), error => { console.error(error); app.exit(1); });
