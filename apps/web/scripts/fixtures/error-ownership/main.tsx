import { createRoot } from 'react-dom/client';
import { useState } from 'react';
import { assertValid } from '@whip/protocol';
import { urlProfile, type AppStorage } from '@whip/app/platform';
import { mountApplication } from '../../../src/bootstrap';
import './fixture.css';

// Only this fixture replaces the browser transport constructor. Production
// browserSocket still performs its normal v4 runtime/epoch/network handshake.
// Faults occur on exact native requests, never by changing app error state.
declare const __FIXTURE__: { root_id: string; runtime_id: string; turn_agent: string; execution_agent: string };
const scenarios = {
  clean: 'Healthy session',
  application: 'Application — type a draft to fail its storage write',
  host: 'Host — press Disconnect, then Restore',
  session: 'Session — metadata read fails; Restore, then Refresh',
  turn: 'Turn — recorded model failure; Restore submits one explicit successful follow-up',
  execution: 'Execution — actual recorded failed cell in REPL',
  submission: 'Submission rejected — type and send a message',
  uncertain: 'Submission uncertain — type and send; Restore, then Check status',
  resource: 'Resource — open the composer Model picker',
  action: 'Action — use sidebar session actions → Rename, then Save',
  validation: 'Validation — Settings → Servers → Add server; enter an invalid address',
  combined: 'Combined — recorded turn failure; press Disconnect',
} as const;
type Scenario = keyof typeof scenarios;
const saved = sessionStorage.getItem('error-ownership-scenario');
const scenario: Scenario = saved && Object.hasOwn(scenarios, saved) ? saved as Scenario : 'clean';
let faults = true, offline = false;
const transports = new Set<WebSocket>();
const NativeWebSocket = window.WebSocket;
const controlsLifetime = new AbortController();
class FaultSocket extends NativeWebSocket {
  constructor(url: string | URL, protocols?: string | string[]) {
    const endpoint = new URL(url, location.href);
    if (endpoint.host !== location.host || endpoint.pathname !== '/api/v4/ws') throw new Error('Fixture only permits its exact owned gateway');
    if (transports.size >= 128) throw new Error('Fixture transport capacity exceeded');
    super(url, protocols);
    transports.add(this); this.addEventListener('close', () => transports.delete(this), { once: true });
  }
  override send(data: string | ArrayBufferLike | Blob | ArrayBufferView) {
    if (offline) { this.close(); return; }
    if (typeof data !== 'string' || data.length > 8 << 20) throw new Error('Fixture only accepts bounded SDK frames');
    const request: unknown = JSON.parse(data); assertValid('Request', request);
    const reject = (message: string, kind: 'INVALID' | 'INTERNAL' = 'INTERNAL') => queueMicrotask(() => {
      if (this.readyState === NativeWebSocket.OPEN) this.dispatchEvent(new MessageEvent('message', { data: JSON.stringify({
        jsonrpc: '2.0', id: request.id, error: { code: -32000, kind, message },
      }) }));
    });
    if (faults) {
      if (scenario === 'session' && request.method === 'sessions.get') return reject('The session metadata could not be loaded.');
      if (scenario === 'resource' && request.method === 'providers.catalog') return reject('The model catalog could not be loaded.');
      if (scenario === 'action' && request.method === 'trees.update') return reject('The session could not be renamed.');
      if (scenario === 'submission' && request.method === 'sessions.submit') return reject('The message was rejected. Your draft is retained.', 'INVALID');
      if (scenario === 'uncertain' && request.method === 'sessions.submit') {
        // No mutation reaches the host. The client still must prove absence via
        // its exact original receipt identity; delivery is initially unknown.
        queueMicrotask(() => this.close()); return;
      }
      if (scenario === 'uncertain' && request.method === 'receipts.match') return reject('Receipt inspection is temporarily unavailable.');
    }
    super.send(data);
  }
}
window.WebSocket = FaultSocket;
const route = `/h/${__FIXTURE__.runtime_id}/s/${__FIXTURE__.root_id}`;
const storage: AppStorage = {
  keys: () => Object.keys(localStorage), getItem: key => localStorage.getItem(key),
  setItem(key, value) {
    if (faults && scenario === 'application' && key.startsWith('whip.web.draft')) throw new Error('Acceptance fixture: device storage is full');
    localStorage.setItem(key, value);
  },
  removeItem: key => localStorage.removeItem(key),
  transaction: (key, update) => navigator.locks.request(key, update),
};
localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'dark' }));
if (location.pathname === '/') history.replaceState(null, '', route);
const application = mountApplication({
  storage,
  windowStorage: { keys: () => Object.keys(sessionStorage), getItem: key => sessionStorage.getItem(key),
    setItem: (key, value) => sessionStorage.setItem(key, value), removeItem: key => sessionStorage.removeItem(key) },
  defaultConnection: { ...urlProfile(location.origin), runtimeId: __FIXTURE__.runtime_id, label: 'Acceptance Mac' },
  connectionKinds: ['url'],
  copy: text => navigator.clipboard.writeText(text), openExternal: async () => {}, download: async () => 'cancelled',
  dispose() { controlsLifetime.abort(); for (const socket of transports) socket.close(); transports.clear(); window.WebSocket = NativeWebSocket; },
});
function Controls() {
  const [restored, setRestored] = useState(false), [disconnected, setDisconnected] = useState(false);
  const [pending, setPending] = useState(false), [error, setError] = useState('');
  return <><label>Isolated acceptance fixture<select aria-label="Error scenario" value={scenario} onChange={event => {
    const next = event.target.value as Scenario;
    sessionStorage.clear(); localStorage.clear(); sessionStorage.setItem('error-ownership-scenario', next);
    location.href = route + (['turn', 'combined'].includes(next) ? `?agent=${__FIXTURE__.turn_agent}` : next === 'execution' ? `?agent=${__FIXTURE__.execution_agent}&view=repl` : '');
  }}>{Object.entries(scenarios).map(([id]) => <option key={id} value={id}>{id}</option>)}</select></label>
    <button onClick={() => { offline = true; setDisconnected(true); for (const socket of transports) socket.close(); }}>Disconnect</button>
    <button disabled={pending || restored} onClick={() => {
      faults = false; offline = false; setRestored(true); setDisconnected(false); application.runtime.flushDrafts();
      if (scenario === 'turn') {
        const client = application.runtime.connections.host(__FIXTURE__.runtime_id)?.client;
        if (!client) { setError('The fixture host is not connected'); return; }
        setPending(true);
        const id = crypto.randomUUID(), signal = AbortSignal.any([controlsLifetime.signal, AbortSignal.timeout(15000)]);
        void client.session(__FIXTURE__.turn_agent).submit([{ type: 'text', text: 'Explicit successful error-ownership follow-up.' }], id, { signal })
          .then(() => client.wait(id, { signal })).catch(value => setError(String(value))).finally(() => setPending(false));
      }
    }}>Restore</button>
    <p>{scenarios[scenario]}{restored ? ' · Fault removed; use the app’s recovery control.' : disconnected ? ' · Test transport disconnected.' : ''}{error && ` · Fixture control failed: ${error}`}</p>
  </>;
}
createRoot(document.getElementById('fixture-controls')!).render(<Controls />);
