import { createRoot } from 'react-dom/client';
import { useState } from 'react';
import { webSocket, WhipError, type Transport, type TransportFactory } from '@whip/sdk';
import { urlProfile, type AppStorage } from '@whip/app/platform';
import { mountApplication } from '../../../src/bootstrap';
import './fixture.css';

// Faults enter at real platform/transport boundaries. All error displays and
// interactions below the clearly separate test toolbar are the shared app.
declare const __FIXTURE__: { root_id: string; runtime_id: string; turn_agent?: string };
const turnAgent = __FIXTURE__.turn_agent ?? 'turn-failed-empty';
const scenarios = {
  clean: 'Healthy session',
  application: 'Application — type a draft to fail its storage write',
  host: 'Host — press Disconnect, then Restore',
  session: 'Session — snapshot read fails; Restore, then Refresh',
  turn: turnAgent === 'turn-failed-empty' ? 'Turn — recorded model failure; Restore completes a follow-up' : 'Turn — recorded model failure; Restore only removes connection faults; history is retained',
  execution: 'Execution — recorded failed cell in REPL',
  submission: 'Submission rejected — type and send a message',
  uncertain: 'Submission uncertain — type and send; Restore, then Check status',
  resource: 'Resource — open the composer Model picker',
  action: 'Action — use sidebar session actions → Rename, then Save',
  validation: 'Validation — Settings → Servers → Add server; enter an invalid address',
  combined: 'Combined — recorded turn failure; press Disconnect',
} as const;
type Scenario = keyof typeof scenarios;
const scenario = (sessionStorage.getItem('error-ownership-scenario') ?? 'clean') as Scenario;
let faults = true;
let offline = false;
const transports = new Set<Transport>();
const route = `/h/${__FIXTURE__.runtime_id}/s/${__FIXTURE__.root_id}`;
const storage: AppStorage = {
  keys: () => Object.keys(localStorage),
  getItem: key => localStorage.getItem(key),
  setItem(key, value) {
    if (faults && scenario === 'application' && key.startsWith('whip.web.draft'))
      throw new Error('Acceptance fixture: device storage is full');
    localStorage.setItem(key, value);
  },
  removeItem: key => localStorage.removeItem(key),
  transaction: (_key, update) => Promise.resolve().then(update),
};
const transport: TransportFactory = async (handlers, signal) => {
  if (offline) throw new WhipError('disconnected', 'Acceptance fixture: test daemon is unavailable');
  const socket = await webSocket(`${location.origin}/api/v3/ws`)(handlers, signal);
  const wrapped: Transport = {
    kind: socket.kind,
    httpEndpoint: socket.httpEndpoint,
    get bufferedAmount() { return socket.bufferedAmount; },
    close() { transports.delete(wrapped); socket.close(); },
    send(message) {
      const request = JSON.parse(message);
      const operation = request.params?.operation;
      const reject = (message: string, kind = 'execution_failed') => queueMicrotask(() => handlers.message(JSON.stringify({
        jsonrpc: '2.0', id: request.id, error: { code: -32000, message, data: { kind } },
      })));
      if (faults) {
        if (scenario === 'session' && request.method === 'root.snapshot') return reject('The session snapshot could not be loaded.');
        if (scenario === 'resource' && operation === 'provider.catalogs') return reject('The model catalog could not be loaded.');
        if (scenario === 'action' && operation === 'session.rename') return reject('The session could not be renamed.');
        if (scenario === 'submission' && ['submit', 'agent.submit'].includes(operation)) return reject('The message was rejected. Your draft is retained.', 'invalid_arguments');
        if (scenario === 'uncertain' && ['submit', 'agent.submit'].includes(operation)) {
          // No frame reaches the daemon, but the client cannot know this until
          // it can query the original command identity after Restore.
          queueMicrotask(() => wrapped.close());
          return;
        }
        if (scenario === 'uncertain' && request.method === 'command.status') return reject('Command status is temporarily unavailable.');
      }
      socket.send(message);
    },
  };
  transports.add(wrapped);
  return wrapped;
};
localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'dark' }));
if (location.pathname === '/') history.replaceState(null, '', route);
const application = mountApplication({
  storage,
  windowStorage: { keys: () => Object.keys(sessionStorage), getItem: key => sessionStorage.getItem(key),
    setItem: (key, value) => sessionStorage.setItem(key, value), removeItem: key => sessionStorage.removeItem(key) },
  defaultConnection: { ...urlProfile(location.origin), label: 'Acceptance Mac' },
  connectionKinds: ['url'],
  resolveConnection: async () => ({ endpoint: transport, dispose() {} }),
  copy: async text => navigator.clipboard.writeText(text),
  openExternal: async () => {},
  download: async () => 'cancelled',
});

function Controls() {
  const [restored, setRestored] = useState(false);
  const [disconnected, setDisconnected] = useState(false);
  return <><label>Isolated acceptance fixture<select aria-label="Error scenario" value={scenario} onChange={event => {
    const next = event.target.value as Scenario;
    sessionStorage.clear(); localStorage.clear();
    sessionStorage.setItem('error-ownership-scenario', next);
    const search = ['turn', 'combined'].includes(next) ? `?agent=${turnAgent}`
      : next === 'execution' ? '?agent=repl-child&view=repl' : '';
    location.href = route + search;
  }}>{Object.entries(scenarios).map(([id]) => <option key={id} value={id}>{id}</option>)}</select></label>
    <button onClick={() => { offline = true; setDisconnected(true); for (const value of [...transports]) value.close(); }}>Disconnect</button>
    <button onClick={() => {
      faults = false; offline = false; setRestored(true); setDisconnected(false);
      application.runtime.flushDrafts();
      if (scenario === 'turn' && turnAgent === 'turn-failed-empty') void fetch('/fixture/control/turn-outcome/succeed', { method: 'POST' });
    }}>Restore</button>
    <p>{scenarios[scenario]}{restored ? ' · Fault removed; use the app’s recovery control.' : disconnected ? ' · Test transport disconnected.' : ''}</p>
  </>;
}
createRoot(document.getElementById('fixture-controls')!).render(<Controls />);
