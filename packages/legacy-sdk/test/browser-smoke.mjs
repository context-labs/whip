import { createWhipClient } from '../dist/index.js';
import { createSessionView } from '../dist/state.js';
import { useSessionView } from '../dist/react.js';
import { createElement, StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { flushSync } from 'react-dom';

const check = (condition, message) => { if (!condition) throw new Error(message); };
async function until(condition) {
  const deadline = Date.now() + 15_000;
  while (!await condition()) {
    if (Date.now() > deadline) throw new Error('React view update timed out');
    await new Promise(resolve => setTimeout(resolve, 20));
  }
}

async function reactSmoke(view, client, rootId) {
  let active = 0;
  const subscribe = view.subscribe;
  view.subscribe = listener => {
    active++;
    const dispose = subscribe(listener);
    return () => { active--; dispose(); };
  };
  function Consumer() {
    const snapshot = useSessionView(view);
    return createElement('p', null, snapshot.root?.meta.title ?? '');
  }
  const container = document.createElement('div');
  document.body.append(container);
  const root = createRoot(container);
  try {
    flushSync(() => root.render(createElement(StrictMode, null, createElement(Consumer), createElement(Consumer))));
    await until(() => active === 2);
    const renamed = await client.submit('session.rename', { title: 'React SDK update' }, { rootId }).result();
    check(renamed.status === 'succeeded', 'React smoke rename failed');
    await until(() => container.textContent === 'React SDK updateReact SDK update');
  } finally {
    flushSync(() => root.unmount());
    check(active === 0, 'React external-store subscriptions leaked');
    check(view.getSnapshot().status !== 'closed', 'Unmount disposed the application-owned session view');
    container.remove();
    view.subscribe = subscribe;
  }
}

async function smoke() {
  const info = await (await fetch('/bridge.json')).json();
  const client = createWhipClient({ endpoint: info.endpoint, clientId: crypto.randomUUID(), clientKind: 'human' });
  await client.connect();
  try {
    const initialized = await client.call('daemon.ping', {});
    check(typeof initialized.generation === 'string', 'generation lost precision');
    const created = await client.submit('session.create', { kind: 'agent', cwd: info.directory, model: 'model', provider: 'provider' }).result({ signal: AbortSignal.timeout(15_000) });
    check(created.status === 'succeeded', 'session creation failed');
    const rootId = created.result.root_id;
    const command = client.session(rootId).submit({ text: 'browser SDK round trip' });
    const accepted = await command.accepted();
    check(typeof accepted.ingress_seq === 'string', 'ingress sequence lost precision');
    const result = await command.result({ signal: AbortSignal.timeout(15_000) });
    check(result.status === 'succeeded' && result.result.text === 'browser SDK round trip', 'command did not converge');
    const snapshot = await client.call('root.snapshot', { root_id: rootId });
    check(typeof snapshot.cursor === 'string' && snapshot.messages.length === 2, 'invalid snapshot');
    const view = createSessionView(client.session(rootId));
    try {
      await view.start();
      check(view.getSnapshot().status === 'live' && view.getSnapshot().history[rootId].messages.length === 2, 'session view did not bootstrap');
      await reactSmoke(view, client, rootId);
    } finally { await view.dispose(); }
    check((await client.permissions.setMode(rootId, false).result()).status === 'succeeded', 'Permission mode update failed');
    check((await client.permissions.setMode(rootId, true).result()).status === 'succeeded', 'Permission mode reset failed');
    for (const allow of [true, false]) {
      const command = client.session(rootId).submit({ text: 'permission:' + crypto.randomUUID() });
      await command.accepted();
      let permission;
      await until(async () => {
        permission = (await client.session(rootId).snapshot()).permissions?.find(item => item.status === 'pending');
        return permission;
      });
      await client.permissions.decide({ root_id: rootId, permission_id: permission.id, allow });
      const outcome = await command.result({ signal: AbortSignal.timeout(15_000) });
      check(outcome.status === (allow ? 'succeeded' : 'failed'), 'Permission decision did not control execution');
    }
    const encoder = new TextEncoder();
    const data = encoder.encode('browser HTTP content through SDK contracts');
    const uploaded = await client.upload(data, { rootId, mediaType: 'text/plain' });
    check(await uploaded.readText({ maxBytes: 1024 }) === new TextDecoder().decode(data), 'SDK content read differs from uploaded bytes');
    return { passed: true, user_agent: navigator.userAgent, command_status: result.status, event_cursor: snapshot.cursor, strict_csp: true, react_strict_mode: true, unsigned_permissions: true, content: true };
  } finally { client.close(); }
}

const browser = new URL(location.href).searchParams.get('browser');
const result = await smoke().catch(error => ({ passed: false, error: String(error), stack: error.stack }));
document.getElementById('status').textContent = result.passed ? 'WHIP SDK checks passed. This tab can be closed.' : JSON.stringify(result, null, 2);
await fetch('/result/' + browser, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(result) });
