import { useSyncExternalStore } from 'react';
import { render } from '@testing-library/react';
import { afterEach, beforeEach, vi } from 'vitest';
import { QueryClientProvider } from '@tanstack/react-query';
import { createMemoryHistory, createRootRoute, createRouter, RouterProvider } from '@tanstack/react-router';
import { ThemeProvider, UIProvider } from '@whip/ui';
import fixtures from '../../protocol/schema/fixtures.json';
import { AppRuntime } from '../src/runtime';
import { RuntimeContext, useSessionTabs } from '../src/context';
import { Welcome } from '../src/welcome';
import type { HostConnection } from '../src/hosts';
import { readModelCatalog } from '../src/model-options';
import { providerFixture, revision, model, preset, route, sessionRecord } from './provider-fixture';

export const wire = (name: string): any => structuredClone(fixtures.find(item => item.type === name && item.valid)!.value);
const runtimes: AppRuntime[] = [];
beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => { for (const runtime of runtimes.splice(0)) runtime.dispose(); vi.unstubAllGlobals(); });

export async function fixture(remoteHost = false, providerReady = true, focused = true) {
  const f = await providerFixture({ runtimeID: 'host', builtins: [{ id: 'coding', revision }] });
  f.data.inventory = { revision, routes: providerReady ? [route('openai')] : [], defaults: providerReady ? { provider: 'openai', name: 'gpt-6-astra', effort: 'high' } : null, compaction_model: null };
  f.data.presets.items = [{ ...preset('openai'), name: 'OpenAI', suggested_models: ['gpt-6-astra'] }, preset('inference-net'), preset('openai-codex')];
  const rpc: Record<string, ReturnType<typeof vi.fn<(...args: any[]) => any>>> = {};
  const on = (name: string, callback: (params: any, signal?: AbortSignal) => any) => {
    const mock = vi.fn(callback); rpc[name] = mock;
    f.data.handlers[name] = (request, signal) => mock(request.params, signal);
    return mock;
  };
  on('providers.list', () => f.data.inventory);
  on('providers.bundled', () => ({ items: [model('gpt-6-astra'), model('gpt-5.5')] }));
  on('providers.catalog', ({ provider }) => ({ provider, state: 'missing', scope_state: 'unverified', discovery: 'not_checked', fetched_at: null, stale: false, failure: null, models: [] }));
  on('providers.readiness', () => ({ configured: f.data.inventory.routes.length > 0, credential_state: f.data.inventory.routes.length ? 'available' : 'missing', catalog_state: 'missing', model_state: 'configured', inference_state: 'not_tested' }));
  on('providers.create', ({ provider }) => { f.data.inventory = { ...f.data.inventory, routes: [route(provider)] }; return f.data.inventory; });
  on('providers.defaults', ({ defaults }) => { f.data.inventory = { ...f.data.inventory, defaults: defaults.selection }; return f.data.inventory; });
  on('host.permission_default', () => ({ revision, mode: 'prompt' }));
  on('host.execution_defaults', () => ({ ...wire('HostExecutionDefaults'), revision, engine: 'starlark' }));
  on('definitions.list', () => ({ items: [{ ref: { id: 'coding', revision }, name: 'Coding', created_at: '2026-09-28T00:00:00Z' }], next_cursor: null }));
  on('host.skills.complete', () => ({ candidates: [], truncated: false }));
  on('mcp.configuration', () => ({ ...wire('MCPConfiguration'), revision, imports: { claude: null, codex: null, project: null, opencode: null, offered: true } }));
  on('host.directories.list', ({ path }) => ({ path, parent: '/', entries: [], has_more: false, next_after: null, truncated: false }));
  on('host.directory.pick', () => ({ path: null, cancelled: true }));
  on('trees.recent', () => ({ catalog_revision: '1', items: [{ root_id: 'root', working_directory: '/remote/recent', model: f.data.inventory.defaults, last_activity_at: '2026-09-28T00:00:00Z', tree: { id: 'tree', engine: 'starlark', metadata: { title: null, pinned: false, archived: false }, revision: '1', created_at: '2026-09-28T00:00:00Z' } }], has_more: false }));
  on('trees.create', params => ({ creation: { id: params.creation_id, root_id: 'created', tree_id: 'tree', created_at: '2026-09-28T00:00:00Z' }, root: { ...sessionRecord('created'), parent_id: null, definition: params.definition }, tree: { id: 'tree', engine: params.engine, revision: '1', metadata: params.metadata, created_at: '2026-09-28T00:00:00Z' }, deleted: false }));
  on('sessions.submit', params => ({ ...wire('Admission'), receipt: { ...wire('Admission').receipt, identity: params.identity } }));
  on('content.put', async params => {
    const data = Uint8Array.from(atob(params.data_base64), c => c.charCodeAt(0));
    const digest = [...new Uint8Array(await crypto.subtle.digest('SHA-256', data))].map(v => v.toString(16).padStart(2, '0')).join('');
    return { id: params.reference_id, session_id: params.session_id, digest, size: String(data.length), media_type: params.media_type, created_at: '2026-09-28T00:00:00Z' };
  });
  const values = new Map<string, string>();
  const storage = { persistent: false, keys: () => [...values.keys()], getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => { values.set(key, value); }, removeItem: (key: string) => { values.delete(key); } };
  const pickDirectory = vi.fn(async () => '/selected/project');
  const runtime = new AppRuntime({ storage, pickDirectory, defaultEndpoint: 'http://localhost:8080', copy: async () => {}, download: async () => {}, openExternal: async () => {} });
  runtimes.push(runtime);
  runtime.queries.setDefaultOptions({ queries: { retry: false, staleTime: Infinity } });
  const host = { id: 'local', runtimeId: 'host', name: remoteHost ? 'Kuzco' : 'Local', local: !remoteHost, state: 'connected', client: f.client,
    profile: { target: remoteHost ? { kind: 'ssh', user: 'sam', host: 'kuzco' } : { kind: 'local' } },
  } as unknown as HostConnection;
  const remote = { id: 'remote', runtimeId: 'remote-host', name: 'Mac mini', local: false, state: 'closed', endpoint: 'http://mm.local:8080', profile: { target: { kind: 'url', endpoint: 'http://mm.local:8080' } } } as HostConnection;
  vi.spyOn(runtime, 'getSnapshot').mockReturnValue({ ...runtime.getSnapshot(), hosts: [host, remote] });
  vi.spyOn(runtime.connections, 'isAttached').mockReturnValue(true);
  vi.spyOn(runtime.connections, 'host').mockImplementation(id => runtime.getSnapshot().hosts.find(host => host.runtimeId === id));
  const connect = vi.spyOn(runtime.connections, 'connect').mockResolvedValue();
  // The actual durable command and codec run here; recovery transitions have separate driver coverage.
  const run = vi.spyOn(runtime, 'run').mockImplementation((async (handle, _label, onAccepted) => { const result = await handle.send(); onAccepted?.(result); await handle.forget(); return result; }) as AppRuntime['run']);
  const tab = runtime.tabs.openNew({ hostProfileId: 'local', runtimeId: 'host', cwd: '/project/whip' });
  const catalog = await readModelCatalog(f.client, new AbortController().signal);
  runtime.queries.setQueryData(['provider-list', 'host'], f.data.inventory);
  runtime.queries.setQueryData(['provider-presets', 'host'], f.data.presets);
  runtime.queries.setQueryData(['provider-catalogs', 'host', ''], catalog);
  runtime.queries.setQueryData(['provider-login-flows', 'host'], []);
  runtime.queries.setQueryData(['host-permission-default', 'host', 'boot'], await f.client.getDefaultPermissionMode());
  runtime.queries.setQueryData(['host-execution-defaults', 'host', 'boot'], await f.client.hosts.executionDefaults());
  if (f.data.inventory.defaults) runtime.queries.setQueryData(['provider-readiness', 'host', f.data.inventory.defaults], await f.client.providerReadiness(f.data.inventory.defaults));
  for (const mock of Object.values(rpc)) mock.mockClear(); f.calls.length = 0;
  let pane = { active: tab.id, focused };
  const paneListeners = new Set<() => void>();
  const paneSnapshot = () => pane;
  const paneSubscribe = (listener: () => void) => { paneListeners.add(listener); return () => { paneListeners.delete(listener); }; };
  const updatePane = (patch: Partial<typeof pane>) => { pane = { ...pane, ...patch }; for (const listener of paneListeners) listener(); };
  function Content() {
    useSessionTabs();
    const { active, focused } = useSyncExternalStore(paneSubscribe, paneSnapshot);
    const current = runtime.tabs.workspace().tabs.find(item => item.id === active);
    return current?.kind === 'new' ? <Welcome key={active} tab={current} focused={focused} /> : <p>Promoted to {current?.kind === 'chat' ? current.rootId : 'nothing'}</p>;
  }
  const root = createRootRoute({ component: () => <RuntimeContext.Provider value={runtime}><QueryClientProvider client={runtime.queries}><ThemeProvider initialTheme="dark"><UIProvider><Content /></UIProvider></ThemeProvider></QueryClientProvider></RuntimeContext.Provider> });
  const router = createRouter({ routeTree: root, history: createMemoryHistory({ initialEntries: ['/'] }) });
  return { ...f, runtime, tab, rpc, on, catalog, inventory: f.data.inventory, run, connect, pickDirectory, router, select: (id: string) => updatePane({ active: id }), focus: (focused: boolean) => updatePane({ focused }), render: () => render(<RouterProvider router={router} />) };
}
