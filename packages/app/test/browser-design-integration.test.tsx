import { webcrypto } from 'node:crypto';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { manifest } from '@whip/protocol';
import type { TransportFactory, TransportHandlers } from '@whip/sdk';
import { AppRuntime } from '../src/runtime';
import { RuntimeContext } from '../src/context';
import { BrowserDesignControl, designRecipients } from '../src/browser-design';
import { compositionKey } from '../src/compositions';
import type { BrowserPlatform } from '../src/browser-types';
import type { BrowserAgentBridge } from '../src/browser-agent-types';
import type { BrowserDesignDraft, BrowserDesignEvent, BrowserDesignIntent, BrowserDesignState } from '../src/browser-design-types';

// Real AppRuntime, SDK session/command handles, recovery, and CompositionStore.
// The only domain fakes are native IPC and the daemon's JSON-RPC transport.
type Request = { id: string; method: string; params: Record<string, any> };
const runtimes: AppRuntime[] = [];
beforeEach(() => {
  vi.stubGlobal('crypto', webcrypto);
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} });
  // jsdom's File lacks this browser API; preserve the real bytes via FileReader.
  vi.spyOn(Blob.prototype, 'arrayBuffer').mockImplementation(function (this: Blob) {
    return new Promise((resolve, reject) => { const reader = new FileReader(); reader.onload = () => resolve(reader.result as ArrayBuffer); reader.onerror = reject; reader.readAsArrayBuffer(this); });
  });
});
afterEach(() => { runtimes.splice(0).forEach(runtime => runtime.dispose()); vi.unstubAllGlobals(); });

function daemon() {
  const requests: Request[] = [];
  const uploads = new Map<string, { params: Request['params']; bytes: number[] }>();
  let handlers: TransportHandlers;
  let holdUpload = false;
  let held: Request | undefined;
  let rejection: string | undefined;
  let holdCommand = false;
  let uncertain = false;
  let recovered = false;
  let absent = false;
  const reply = (request: Request, result: unknown) => handlers.message(JSON.stringify({ jsonrpc: '2.0', id: request.id, result }));
  const error = (request: Request, message: string, kind = 'invalid_arguments') => handlers.message(JSON.stringify({ jsonrpc: '2.0', id: request.id, error: { code: -32000, message, data: { kind } } }));
  const finish = (request: Request) => {
    const upload = uploads.get(request.params.upload_id)!;
    reply(request, { reference_id: request.params.upload_id, digest: upload.params.expected_digest, size: upload.params.size, media_type: upload.params.media_type });
  };
  const factory: TransportFactory = async current => {
    handlers = current;
    return { kind: 'unix', bufferedAmount: 0, close() {}, send(text) {
      const request = JSON.parse(text) as Request;
      requests.push(request);
      const p = request.params;
      switch (request.method) {
        case 'initialize': reply(request, {
          protocol_major: manifest.major, protocol_minor: manifest.minor, runtime_id: 'host', connection_id: 'connection', generation: '1',
          host_platform: 'darwin', host_architecture: 'arm64', build_id: 'test', capabilities: [], negotiated_capabilities: [],
          execution_engines: [{ id: 'starlark', language: 'starlark', label: 'Starlark' }], default_execution_engine: 'starlark',
          operations: manifest.operations.map(operation => ({ ...operation })),
          limits: { frame_bytes: 1 << 20, connections: 64, in_flight_requests: 32, outbound_messages: 1024, outbound_bytes: String(8 << 20), root_subscriptions: 16, content_chunk_bytes: 256 << 10, upload_bytes: String(64 << 20) },
        }); break;
        case 'config.get': reply(request, { revision: '1', remote_hosts: [], default_execution_engine: 'starlark', import_claude: false, import_codex: false, mcp_import_offered: false, brand_icons: false, default_model: '', default_provider: '', default_effort: '', compact_model: '', compact_provider: '', compact_percent: 70, goal_max_rounds: 1, max_retries: 1 }); break;
        case 'provider.list': reply(request, { revision: '1', providers: [] }); break;
        case 'sessions.revision': reply(request, { revision: '1' }); break;
        case 'sessions.list': reply(request, { revision: '1', items: [], has_more: false }); break;
        case 'root.snapshot': reply(request, { root_id: p.root_id, cursor: '0', history_revision: '1', meta: { execution_engine: 'starlark', definition: '', definition_revision: '', id: p.root_id, kind: 'root', title: '', model: '', provider: '', cwd: '/', goal: '', forked_from: '', fork_seq: 0, tags: [], archived: false, pinned: false, effort: '', usage_in: 0, usage_cached: 0, usage_out: 0, updated_at: '' }, active_turns: { child: 'child-turn' }, agents: [{ execution_engine: 'starlark', id: 'child', root_id: p.root_id, parent_id: p.root_id, name: 'child', model: '', provider: '', effort: '', cwd: '/', report: 'notice', status: 'running', pending_mail: 0, lifecycle_phase: '', blocking_reason: '', terminal_cause: '', allowed_controls: [] }], message_seqs: [], messages: [], agent_presentations: {}, blackboard: [], budgets: [], capabilities: [], schedules: [], inbox: [], presentation: [], questions: [], permissions: [] }); break;
        case 'upload.begin': uploads.set(p.upload_id, { params: p, bytes: [] }); reply(request, { accepted: true }); break;
        case 'upload.chunk': uploads.get(p.upload_id)!.bytes.push(...Uint8Array.from(atob(p.data), value => value.charCodeAt(0))); reply(request, { accepted: true }); break;
        case 'upload.finish': if (holdUpload) held = request; else finish(request); break;
        case 'command.submit':
          if (rejection) error(request, rejection);
          else if (uncertain) { handlers.close(new Error('Acknowledgement lost')); }
          else if (!holdCommand) reply(request, { command_id: p.command_id, operation: p.operation, status: 'succeeded', ingress_seq: '1', result: p.operation === 'agent.submit' ? { agent_id: 'child', inbox_seq: '1', status: 'queued' } : { text: 'Done' } });
          break;
        case 'command.status':
          if (recovered) reply(request, { command_id: p.command_id, operation: 'agent.submit', status: 'succeeded', ingress_seq: '1', result: { agent_id: 'child', inbox_seq: '1', status: 'queued' } });
          else error(request, absent ? 'Not accepted' : 'Status temporarily unavailable', absent ? 'command_not_found' : 'unavailable');
          break;
        case 'browser.provider.bind': reply(request, { version: 1, provider_id: 'provider-id', provider_epoch: 'provider' }); break;
        default: reply(request, {});
      }
    } };
  };
  return { factory, requests, uploads, commands: () => requests.filter(item => item.method === 'command.submit'),
    holdUpload: () => { holdUpload = true; }, releaseUpload: () => { holdUpload = false; if (held) finish(held); },
    reject: (message: string) => { rejection = message; }, holdCommand: () => { holdCommand = true; },
    uncertain: () => { uncertain = true; }, recover: () => { recovered = !absent; uncertain = false; }, absent: () => { absent = true; }, disconnect: () => handlers.close(new Error('Disconnected')),
  };
}

async function fixture(associated: boolean | 'ambiguous' = false) {
  const server = daemon();
  const listeners = new Set<(event: BrowserDesignEvent) => void>();
  let state: BrowserDesignState = { epoch: 'epoch', tabId: 'page', generation: '1', designId: 'design', documentRevision: 1, selectionRevision: 0,
    status: 'active', viewport: { width: 1000, height: 800 }, elements: [] };
  let draft: BrowserDesignDraft | undefined;
  const capture = vi.fn(async (input: { screenshot: boolean }) => ({ ...state, capturedAt: '2026-09-18T00:00:00Z',
    text: JSON.stringify({ schemaVersion: 1, untrusted: true, title: '页面', url: 'https://example.com/settings', elements: state.elements.map(element => ({ id: element.id, tag: 'button', name: element.label, selector: '#target' })) }),
    ...(input.screenshot ? { image: 'data:image/png;base64,iVBORw0KGgo=' } : {}),
  }));
  const design = {
    start: vi.fn(async () => state), stop: vi.fn(async () => {}), capture,
    update: vi.fn(async (input: { draft: BrowserDesignDraft }) => { draft = input.draft; }),
    onEvent: (listener: (event: BrowserDesignEvent) => void) => { listeners.add(listener); return () => { listeners.delete(listener); }; },
  };
  const page = { id: 'page', generation: '1', documentGeneration: 1, status: 'ready' as const, url: 'https://example.test', title: 'Page', loading: false, canGoBack: false, canGoForward: false, zoomFactor: 1 };
  const inventory = { epoch: 'epoch', revision: 1, tabs: [page] };
  const browser: BrowserPlatform = { version: 1, design, snapshot: async () => inventory, restore: async () => inventory,
    create: async () => page, admitted: async () => {}, present: async () => {}, act: async () => {}, close: async () => ({ status: 'closed' }), onEvent: () => () => {} };
  const bridge: BrowserAgentBridge = { identity: async () => ({ desktopId: 'desktop', windowId: 'window', createProfileId: 'profile', tabs: [{ tab_id: 'page', tab_generation: '1', profile_id: 'profile' }] }),
    select: async () => {}, release: async () => {}, preview: vi.fn(), dispatch: vi.fn(), cancel: vi.fn(), onEvent: () => () => {} };
  const values = new Map<string, string>();
  const storage = { keys: () => [...values.keys()], getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => { values.set(key, value); }, removeItem: (key: string) => { values.delete(key); },
    transaction: async <T,>(_key: string, work: () => T) => work() };
  const runtime = new AppRuntime({ storage, browser, ...(associated ? { browserAgent: bridge } : {}), defaultConnection: { id: 'local', label: 'Test host', target: { kind: 'local' } },
    connectionKinds: ['local'], resolveConnection: async () => ({ endpoint: server.factory, dispose() {} }), copy: async () => {}, download: async () => {}, openExternal: async () => {} });
  runtimes.push(runtime);
  await runtime.connections.connect();
  const rootView = runtime.tabs.open('host', 'root', 'Target');
  const childView = runtime.tabs.split(rootView, 'right');
  runtime.tabs.updateLocation(childView, { agent: 'child' });
  runtime.tabs.openBrowser({ id: 'page', url: page.url, titleHint: 'Design page' });
  runtime.tabs.open('host', 'focused', 'Unrelated focused chat');
  await runtime.browser.start();
  if (associated) await runtime.browserAssociations.select({ hostId: 'local', rootId: 'root', title: 'Target', tabId: 'page' });
  if (associated === 'ambiguous') await runtime.browserAssociations.select({ hostId: 'local', rootId: 'closed-root', title: 'Closed conversation', tabId: 'page' });
  const session = runtime.connections.getSnapshot().hosts[0]!.client!.session('root');
  expect(session.client.getSnapshot(), JSON.stringify(server.requests.map(item => item.method)) + String(session.client.getSnapshot().error?.cause)).toMatchObject({ state: 'connected', info: { runtime_id: 'host' } });
  runtime.setDraft('host:root:root', 'Normal root draft');
  runtime.setDraft('host:root:child', 'Normal child draft');
  await runtime.compositions.add('host:root:child', session, 'host', 'child', [new File(['ordinary file'], 'normal.txt', { type: 'text/plain' })]);
  const normal = runtime.compositions.get('host:root:child');
  render(<RuntimeContext.Provider value={runtime}><ThemeProvider><UIProvider><BrowserDesignControl tabId="page" available /></UIProvider></ThemeProvider></RuntimeContext.Provider>);
  fireEvent.click(screen.getByRole('button', { name: 'Enter Design Mode' }));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Exit Design Mode' })).toBeTruthy());
  const emit = async (intent: BrowserDesignIntent) => { await act(async () => { for (const listener of listeners) listener({ kind: 'intent', revision: state, intent }); }); };
  const select = async (revision = 1) => { await act(async () => {
    state = { ...state, selectionRevision: revision, elements: [
      { id: 'one', label: 'Submit button', number: 1, color: 'blue', bounds: { x: 20, y: 30, width: 80, height: 30 } },
      { id: 'two', label: 'Form label', number: 2, color: 'purple', bounds: { x: 20, y: 80, width: 120, height: 20 } },
    ] };
    for (const listener of listeners) listener({ kind: 'state', state });
  }); };
  const recipient = (agent: string) => designRecipients(runtime).find(item => item.rootId === 'root' && item.agentId === agent)!.id;
  const prepare = async (agent = 'child') => { await select(); await emit({ kind: 'prompt', value: 'Align these two controls — 改变' }); await emit({ kind: 'recipient', id: recipient(agent) }); };
  const preserved = () => { expect(runtime.draft('host:root:root')).toBe('Normal root draft'); expect(runtime.draft('host:root:child')).toBe('Normal child draft'); expect(runtime.compositions.get('host:root:child')).toBe(normal); };
  return { runtime, server, capture, design, emit, select, prepare, recipient, preserved, childView, get draft() { return draft!; } };
}

it('sends two native selections through actual uploads and runtime command admission to the explicit child, not the focused chat', async () => {
  const f = await fixture();
  await f.prepare();
  await f.emit({ kind: 'delivery', value: 'steer' });
  await f.emit({ kind: 'send' });
  await waitFor(() => expect(f.server.commands()).toHaveLength(1));
  await waitFor(() => expect(f.draft.prompt).toBe(''));
  const command = f.server.commands()[0]!.params;
  expect(command).toMatchObject({ root_id: 'root', operation: 'agent.submit', payload: { id: 'child', text: 'Align these two controls — 改变', delivery: 'steer' } });
  expect(command.payload.attachments.map((item: any) => item.kind)).toEqual(['text', 'image']);
  expect(command.payload.design_context).toMatchObject({
    context_attachment_id: command.payload.attachments[0].content.reference_id,
    screenshot_attachment_id: command.payload.attachments[1].content.reference_id,
    element_count: 2, page_url: 'https://example.com/settings', page_title: '页面',
  });
  expect(f.capture).toHaveBeenCalledOnce();
  const uploads = [...f.server.uploads.values()].slice(1);
  expect(uploads.map(item => [item.params.root_id, item.params.agent_id, item.params.media_type])).toEqual([['root', 'child', 'text/plain'], ['root', 'child', 'image/png']]);
  expect(JSON.parse(new TextDecoder().decode(new Uint8Array(uploads[0]!.bytes))).elements.map((item: any) => item.id)).toEqual(['one', 'two']);
  expect(uploads[1]!.bytes).toEqual([137, 80, 78, 71, 13, 10, 26, 10]);
  expect(f.runtime.getSnapshot().commands.some(item => item.commandId === command.command_id && item.status === 'succeeded')).toBe(true);
  expect(f.runtime.compositions.get(compositionKey('host', 'root', 'child', 'design:page')).attachments).toEqual([]);
  f.preserved();
});

it('requires an intentional destination without a page association, regardless of focused chat', async () => {
  const f = await fixture();
  await f.select();
  await f.emit({ kind: 'prompt', value: 'Change the spacing' });
  expect(f.draft.recipientId).toBe('');
  await f.emit({ kind: 'send' });
  expect(f.draft.error).toMatch(/choose.*conversation/i);
  expect(f.capture).not.toHaveBeenCalled();
  expect(f.server.commands()).toEqual([]);
  expect(f.draft.prompt).toBe('Change the spacing');
  f.preserved();
});

it('defaults only to the page association and sends to that root, not the focused chat', async () => {
  const f = await fixture(true);
  expect(f.draft.recipientId).toBe(f.recipient('root'));
  await f.select();
  await f.emit({ kind: 'prompt', value: 'Use consistent padding' });
  await f.emit({ kind: 'send' });
  await waitFor(() => expect(f.server.commands()).toHaveLength(1));
  await waitFor(() => expect(f.draft.prompt).toBe(''));
  expect(f.server.commands()[0]!.params).toMatchObject({ root_id: 'root', operation: 'submit', payload: { text: 'Use consistent padding' } });
  expect([...f.server.uploads.values()].slice(1).every(item => item.params.agent_id === 'root')).toBe(true);
  f.preserved();
});

it('does not silently choose the only open recipient when page associations are ambiguous', async () => {
  const f = await fixture('ambiguous');
  await f.select();
  expect(f.draft.recipientId).toBe('');
  await f.emit({ kind: 'prompt', value: 'Keep the associations explicit' });
  await f.emit({ kind: 'send' });
  expect(f.server.commands()).toEqual([]);
  expect(f.draft.error).toMatch(/available conversation/);
  f.preserved();
});

it.each(['The selected model does not support image input', 'Admission rejected'])('preserves prompt/evidence on daemon rejection: %s', async rejection => {
  const f = await fixture();
  await f.prepare();
  f.server.reject(rejection);
  await f.emit({ kind: 'capture' });
  await waitFor(() => expect(f.draft.evidence?.text).toContain('one'));
  await f.emit({ kind: 'send' });
  await waitFor(() => expect(f.draft.error).toContain(rejection));
  expect(f.draft.prompt).toBe('Align these two controls — 改变');
  expect(f.draft.evidence?.text).toContain('one');
  expect(f.draft.evidence?.image).toContain('data:image/png');
  expect(JSON.parse(new TextDecoder().decode(new Uint8Array([...f.server.uploads.values()][1]!.bytes))).elements).toHaveLength(2);
  expect(f.runtime.compositions.get(compositionKey('host', 'root', 'child', 'design:page')).attachments).toHaveLength(0);
  expect(f.server.commands()).toHaveLength(1);
  f.preserved();
});

it('rejects changed native selection during upload instead of sending stale evidence', async () => {
  const f = await fixture();
  await f.prepare();
  f.server.holdUpload();
  await f.emit({ kind: 'send' });
  await waitFor(() => expect(f.server.requests.filter(item => item.method === 'upload.finish')).toHaveLength(2));
  await f.select(2);
  await act(async () => f.server.releaseUpload());
  await waitFor(() => expect(f.draft.busy).toBe(false));
  expect(f.draft.error).toMatch(/changed during upload/);
  expect(f.draft.prompt).toBe('Align these two controls — 改变');
  expect(f.server.commands()).toEqual([]);
  expect(f.runtime.compositions.get(compositionKey('host', 'root', 'child', 'design:page')).attachments).toEqual([]);
  f.preserved();
});

it('rejects a destination change while evidence uploads without routing to the replacement agent', async () => {
  const f = await fixture();
  await f.prepare();
  f.server.holdUpload();
  await f.emit({ kind: 'send' });
  await waitFor(() => expect(f.server.requests.filter(item => item.method === 'upload.finish')).toHaveLength(2));
  await act(async () => f.runtime.tabs.updateLocation(f.childView, { agent: 'different-child' }));
  await act(async () => f.server.releaseUpload());
  await waitFor(() => expect(f.draft.busy).toBe(false));
  expect(f.draft.error).toMatch(/changed during upload/);
  expect(f.draft.prompt).toBe('Align these two controls — 改变');
  expect(f.server.commands()).toEqual([]);
  f.preserved();
});

it('coalesces repeated Send while capture/upload/admission are in flight', async () => {
  const f = await fixture();
  await f.prepare();
  f.server.holdCommand();
  await f.emit({ kind: 'send' });
  await f.emit({ kind: 'send' });
  await waitFor(() => expect(f.server.commands()).toHaveLength(1));
  await f.emit({ kind: 'send' });
  expect(f.capture).toHaveBeenCalledOnce();
  expect(f.server.commands()).toHaveLength(1);
  expect(f.draft.busy).toBe(true);
  expect(f.draft.prompt).toBe('Align these two controls — 改变');
  f.preserved();
});

it('preserves prompt when its destination disconnects before Send', async () => {
  const f = await fixture();
  await f.prepare();
  await f.emit({ kind: 'capture' });
  await waitFor(() => expect(f.draft.evidence?.text).toContain('one'));
  await act(async () => f.runtime.connections.disconnect('local'));
  await f.emit({ kind: 'send' });
  expect(f.server.commands()).toEqual([]);
  expect(f.draft.prompt).toBe('Align these two controls — 改变');
  expect(f.draft.error).toMatch(/available conversation/);
  expect(f.draft.evidence?.text).toContain('one');
  expect(f.runtime.draft('host:root:child')).toBe('Normal child draft');
});

it.each(['check', 'retry'] as const)('retains unresolved delivery after reconnect until explicit %s recovery', async mode => {
  const f = await fixture();
  await f.prepare();
  f.server.uncertain();
  if (mode === 'retry') f.server.absent();
  await f.emit({ kind: 'capture' });
  await waitFor(() => expect(f.draft.evidence?.text).toContain('one'));
  await f.emit({ kind: 'send' });
  await waitFor(() => expect(f.server.commands()).toHaveLength(1));
  await waitFor(() => expect(f.draft.uncertain, JSON.stringify({ draft: f.draft, commands: f.runtime.getSnapshot().commands, requests: f.server.requests.map(item => item.method) })).toBe(true), { timeout: 4000 });
  const command = f.server.commands()[0]!.params;
  expect(f.runtime.getSnapshot().commands).toContainEqual(expect.objectContaining({ commandId: command.command_id, delivery: mode === 'retry' ? 'absent' : 'uncertain' }));
  expect(f.draft.prompt).toBe('Align these two controls — 改变');
  expect(f.draft.evidence?.text).toContain('one');
  await f.emit({ kind: 'send' });
  expect(f.server.commands()).toHaveLength(1);
  expect(f.runtime.draft('host:root:child')).toBe('Normal child draft');
  f.server.recover();
  const pending = f.runtime.getSnapshot().commands.find(item => item.commandId === command.command_id)!;
  await act(async () => { await (mode === 'check' ? f.runtime.checkCommand(pending.id) : f.runtime.retryCommand(pending.id)); });
  await waitFor(() => expect(f.draft.prompt).toBe(''));
  expect(f.draft.uncertain).toBe(false);
  expect(f.server.commands()).toHaveLength(mode === 'retry' ? 2 : 1);
  for (const request of f.server.commands()) expect(request.params).toEqual(command);
});

