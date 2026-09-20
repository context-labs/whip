import { readFileSync } from 'node:fs';
import { webcrypto } from 'node:crypto';
import { JSDOM } from 'jsdom';
import { act, fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { manifest } from '@whip/protocol';
import type { TransportFactory, TransportHandlers } from '@whip/sdk';
import { localProfile } from '@whip/app/platform';
import { mountApplication } from '../../web/src/bootstrap';

const script = readFileSync('apps/desktop/src/startup-probe.ts', 'utf8').match(/const snapshotScript = String.raw`([\s\S]*?)`;/)![1]!;
vi.hoisted(() => { globalThis.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} }; });
const applications: ReturnType<typeof mountApplication>[] = [];
function storage() {
  const values = new Map<string, string>();
  return { keys: () => [...values.keys()], getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value); }, removeItem: (key: string) => { values.delete(key); } };
}

// Only the daemon transport is fake. Identity verification, SDK attachment,
// queries, routing, tab state, StartupScreen and bootstrap are production code.
function daemon(options: { held?: boolean; incompatible?: boolean; fail?: boolean; configured?: boolean } = {}) {
  const methods: string[] = [], unexpected: string[] = [];
  let handlers: TransportHandlers;
  const factory: TransportFactory = async current => {
    handlers = current;
    return { kind: 'unix', bufferedAmount: 0, close() {}, send(text) {
      const request = JSON.parse(text) as { id?: string; method: string; params?: { operation?: string } };
      methods.push(request.method);
      let result: unknown;
      switch (request.method) {
        case 'initialize':
          if (options.held) return;
          if (options.fail) { handlers.message(JSON.stringify({ jsonrpc: '2.0', id: request.id, error: { code: -32000, message: 'Test handshake failure' } })); return; }
          result = {
          protocol_major: manifest.major + (options.incompatible ? 1 : 0), protocol_minor: manifest.minor, runtime_id: 'host', connection_id: 'connection', generation: '1',
          host_platform: 'darwin', host_architecture: 'arm64', build_id: 'test', capabilities: [], negotiated_capabilities: [],
          execution_engines: [{ id: 'starlark', language: 'starlark', label: 'Starlark' }], default_execution_engine: 'starlark',
          operations: manifest.operations.map(operation => ({ ...operation })),
          limits: { frame_bytes: 1 << 20, connections: 64, in_flight_requests: 32, outbound_messages: 1024, outbound_bytes: String(8 << 20), root_subscriptions: 16, content_chunk_bytes: 256 << 10, upload_bytes: String(64 << 20) },
        }; break;
        case 'config.get': result = { revision: '1', remote_hosts: [], default_execution_engine: 'starlark', import_claude: false, import_codex: false, mcp_import_offered: true, brand_icons: false, default_model: options.configured ? 'gpt-6-astra' : '', default_provider: options.configured ? 'openai' : '', default_effort: '', compact_model: '', compact_provider: '', compact_percent: 70, goal_max_rounds: 1, max_retries: 1 }; break;
        case 'provider.discover':
        case 'provider.list': result = { revision: '1', selection: { ready: !!options.configured, model: options.configured ? 'gpt-6-astra' : '', provider: options.configured ? 'openai' : '', reason: options.configured ? '' : 'not configured' }, providers: [{ id: 'openai', name: 'OpenAI', custom: false, methods: ['api_key'], suggested_model: 'gpt-6-astra', status: { provider: 'openai', configured: !!options.configured, available: !!options.configured, key_source: options.configured ? 'literal' : 'none', warnings: [] } }] }; break;
        case 'host.attention': result = { items: [], has_more: false, truncated: false }; break;
        case 'definitions.list': result = { items: [] }; break;
        case 'provider.login.list': result = { flows: [] }; break;
        case 'sessions.revision': result = { revision: '1' }; break;
        case 'sessions.list': result = { revision: '1', items: [], has_more: false }; break;
        case 'query':
          if (request.params?.operation === 'provider.catalogs') { result = { result: { models: {}, providers: { openai: { base_url: 'https://provider.invalid', available: !!options.configured } }, catalogs: { openai: { fetched_at: '2026-01-01T00:00:00Z', base_url: 'https://provider.invalid', models: [{ id: 'gpt-6-astra', reasoning_efforts: [] }] } } } }; break; }
          // Every other request is a fixture bug, never silently treated as success.
        default:
          unexpected.push(request.method + (request.params?.operation ? ':' + request.params.operation : ''));
          handlers.message(JSON.stringify({ jsonrpc: '2.0', id: request.id, error: { code: -32601, message: 'Unexpected test request' } }));
          return;
      }
      handlers.message(JSON.stringify({ jsonrpc: '2.0', id: request.id, result }));
    } };
  };
  return { factory, methods, unexpected, disconnect: () => { options.held = true; handlers.close(new Error('Test disconnect')); } };
}

beforeEach(() => {
  vi.stubGlobal('crypto', webcrypto);
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} });
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  Object.defineProperty(window, 'whipStartupMeasurement', { configurable: true, value: true });
  window.history.replaceState(null, '', '/');
  document.body.innerHTML = '<div id="root"></div>';
});
afterEach(async () => {
  await act(async () => { applications.splice(0).forEach(application => application.dispose()); });
  delete (window as Window & { whipStartupMeasurement?: boolean }).whipStartupMeasurement;
  vi.unstubAllGlobals();
});

async function boot(options: { held?: boolean; incompatible?: boolean; fail?: boolean; configured?: boolean } = {}, ready = true) {
  const server = daemon(options);
  const platform = { storage: storage(), windowStorage: storage(), defaultConnection: localProfile,
    resolveConnection: async () => ({ endpoint: server.factory, dispose() {} }),
    copy: async () => {}, download: async () => {}, openExternal: async () => {} };
  expect(platform.storage.keys()).toEqual([]); expect(platform.windowStorage.keys()).toEqual([]);
  let app!: ReturnType<typeof mountApplication>;
  await act(async () => { app = mountApplication(platform); applications.push(app); await app.router.load(); });
  if (ready) {
    await waitFor(() => expect(app.runtime.getSnapshot().home?.state).toBe('connected'));
    await waitFor(() => expect(document.querySelector('[data-startup-phase]')?.getAttribute('data-startup-phase')).toBe('visible'));
    expect(app.runtime.queries.getQueryCache().getAll().filter(query => query.state.error).map(query => query.queryKey)).toEqual([]);
  }
  return { app, server };
}

async function observe(mutate?: (document: Document, view: JSDOM['window']) => void) {
  // JSDOM has no layout/font renderer. Only those browser primitives are supplied;
  // the observed DOM (including the real StartupScreen) comes from mountApplication.
  const dom = new JSDOM(document.body.innerHTML, { url: 'whip-app://bundle' + window.location.pathname, runScripts: 'outside-only', pretendToBeVisual: true });
  try {
    Object.defineProperty(dom.window.document, 'fonts', { value: { status: 'loaded', ready: Promise.resolve() } });
    dom.window.HTMLElement.prototype.getClientRects = function () { return (this.closest('[hidden]') ? [] : [{}]) as unknown as DOMRectList; };
    dom.window.performance.getEntriesByName = () => [{ startTime: 1 }] as unknown as PerformanceEntry[];
    Object.defineProperty(dom.window, 'whipStartupSnapshot', { value: (window as Window & { whipStartupSnapshot?: () => unknown }).whipStartupSnapshot });
    mutate?.(dom.window.document, dom.window);
    return await dom.window.eval(script);
  } finally { dom.window.close(); }
}

it('accepts the connected, visible, zero-interaction first-launch frontdoor through real bootstrap', async () => {
  const { app, server } = await boot();
  expect(app.router.state.location.pathname).toBe('/');
  expect(app.runtime.tabs.workspace().tabs).toEqual([]);
  expect(screen.getAllByRole('button', { name: 'New session', exact: true }).length).toBeGreaterThan(0);
  expect(screen.queryByRole('textbox', { name: 'Your first message' })).toBeNull();
  expect(document.querySelector('[aria-label="Provider setup"]')).toBeNull();
  expect(server.methods).toContain('initialize'); expect(server.methods).toContain('provider.list');
  expect(server.unexpected).toEqual([]); expect(server.methods).not.toContain('sessions.create');
  const observed = await observe();
  expect(observed.painted).toBe(true);
  expect(observed).toMatchObject({ host: true, noNotice: true, frontdoor: true, sdkConnected: true, appVisible: true, home: false, visible: true, fonts: true, painted: true, pathname: '/' });
});

it.each([false, true])('enters actual onboarding after the real New session action (configured=%s)', async configured => {
  const { app, server } = await boot({ configured });
  const action = document.querySelector<HTMLButtonElement>('[data-empty-workspace="frontdoor"] button[aria-label="New session"]')!;
  await act(async () => { fireEvent.click(action); });
  if (configured) await screen.findByRole('textbox', { name: 'Your first message' });
  else await screen.findByRole('button', { name: 'Connect OpenAI', exact: true });
  const tab = app.runtime.tabs.workspace().tabs[0]!;
  expect(tab.kind).toBe('new');
  expect(app.router.state.location.pathname).toBe(`/new/${tab.id}`);
  expect(server.unexpected).toEqual([]); expect(server.methods).not.toContain('sessions.create');
  expect(app.runtime.queries.getQueryCache().getAll().filter(query => query.state.error).map(query => query.queryKey)).toEqual([]);
  expect(await observe()).toMatchObject({ home: true, frontdoor: false, sdkConnected: true, draftPath: `/new/${tab.id}`, pathname: `/new/${tab.id}` });
});

it('does not expose fixture state without native opt-in and removes its getter on disposal', async () => {
  delete (window as Window & { whipStartupMeasurement?: boolean }).whipStartupMeasurement;
  const { app } = await boot();
  expect((window as Window & { whipStartupSnapshot?: unknown }).whipStartupSnapshot).toBeUndefined();
  expect(await observe()).toMatchObject({ frontdoor: false, sdkConnected: false, sdkState: 'unknown' });
  await act(async () => app.dispose());
});

it('projects only bounded current state, not identities/capabilities, and fails again on disconnect', async () => {
  const { app, server } = await boot();
  const read = (window as Window & { whipStartupSnapshot: () => unknown }).whipStartupSnapshot;
  expect(read()).toEqual({ sdkState: 'connected', sdkConnected: true, tabCount: 0, newDraftMatchesRoute: false });
  await act(async () => server.disconnect());
  expect(await observe()).toMatchObject({ frontdoor: false, sdkConnected: false });
  expect(read()).not.toMatchObject({ sdkConnected: true });
  await act(async () => app.dispose());
  expect((window as Window & { whipStartupSnapshot?: unknown }).whipStartupSnapshot).toBeUndefined();
});

it.each(['held', 'incompatible', 'fail'] as const)('rejects a real SDK %s handshake even after the startup screen releases its timeout', async mode => {
  const { app, server } = await boot({ [mode]: true }, false);
  expect((await observe()).frontdoor).toBe(false);
  await waitFor(() => expect(document.querySelector('[data-startup-phase]')?.getAttribute('data-startup-phase')).toBe('visible'), { timeout: 4500 });
  expect(app.runtime.tabs.workspace().tabs).toEqual([]);
  expect(server.methods).toContain('initialize');
  expect(await observe()).toMatchObject({ frontdoor: false, sdkConnected: false, appVisible: true });
});

it.each(['pending', 'exiting', 'entering'])('rejects the real startup surface during %s', async phase => {
  await boot();
  expect(await observe(d => d.querySelector('[data-startup-phase]')!.setAttribute('data-startup-phase', phase)))
    .toMatchObject({ frontdoor: false, appVisible: false });
});

it.each(['disabled', 'hidden', 'inert', 'aria-hidden', 'css-hidden', 'missing', 'unscoped'])('rejects a %s frontdoor action without accepting other New session controls', async mode => {
  await boot();
  expect(await observe(d => {
    const empty = d.querySelector<HTMLElement>('[data-empty-workspace="frontdoor"]')!;
    const action = empty.querySelector<HTMLButtonElement>('button[aria-label="New session"]')!;
    if (mode === 'disabled') action.disabled = true;
    else if (mode === 'css-hidden') empty.style.visibility = 'hidden';
    else if (mode === 'missing') empty.dataset.emptyWorkspace = 'missing';
    else if (mode === 'unscoped') d.body.append(action);
    else empty.setAttribute(mode, mode === 'aria-hidden' ? 'true' : '');
  })).toMatchObject({ frontdoor: false });
});

it('does not accept an enabled frontdoor at a different route or a missing draft', async () => {
  const { app } = await boot();
  await act(async () => { await app.router.navigate({ to: '/new/$draftId', params: { draftId: 'missing-draft' } }); });
  expect(document.querySelector('[data-empty-workspace="missing"]')).toBeTruthy();
  expect(await observe()).toMatchObject({ frontdoor: false, home: false, draftPath: '', uiVariant: 'missing' });
});

it('retains notice, font and frame-wait rejection after a real successful bootstrap', async () => {
  await boot();
  expect(await observe(d => d.body.insertAdjacentHTML('beforeend', '<p role="status">Connecting</p>')))
    .toMatchObject({ noNotice: false, painted: false });
  expect(await observe(d => Object.assign(d.fonts, { status: 'loading' }))).toMatchObject({ fonts: false, painted: false });
  expect(await observe((_d, view) => { view.requestAnimationFrame = () => 1; })).toMatchObject({ painted: false });
});
