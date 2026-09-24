import { render } from '@testing-library/react';
import { afterEach, beforeEach, vi } from 'vitest';
import { QueryClientProvider } from '@tanstack/react-query';
import { createMemoryHistory, createRootRoute, createRouter, RouterProvider } from '@tanstack/react-router';
import { ThemeProvider, UIProvider } from '@whip/ui';
import type { WhipClient } from '@whip/sdk';
import { AppRuntime } from '../src/runtime';
import { RuntimeContext, useSessionTabs } from '../src/context';
import { Welcome } from '../src/welcome';
import type { HostConnection } from '../src/hosts';

const runtimes: AppRuntime[] = [];
beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => { for (const runtime of runtimes.splice(0)) runtime.dispose(); vi.unstubAllGlobals(); });

export function fixture(remoteHost = false, providerReady = true, focused = true) {
  const values = new Map<string, string>();
  const storage = { keys: () => [...values.keys()], getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => { values.set(key, value); }, removeItem: (key: string) => { values.delete(key); } };
  const pickDirectory = vi.fn(async () => '/selected/project');
  const runtime = new AppRuntime({ storage, pickDirectory, defaultEndpoint: 'http://localhost:8080', copy: async () => {}, download: async () => {}, openExternal: async () => {} });
  runtimes.push(runtime);
  runtime.queries.setDefaultOptions({ queries: { retry: false, staleTime: Infinity } });
  const snapshot = { state: 'connected', info: { runtime_id: 'host', default_execution_engine: 'starlark', execution_engines: [{ id: 'starlark', label: 'Starlark' }, { id: 'quickjs', label: 'JavaScript' }] } };
  const inventory = { revision: '1', selection: { ready: providerReady, model: 'gpt-6-astra', provider: 'openai' }, providers: [{ id: 'openai', name: 'OpenAI', methods: ['api_key'], suggested_model: 'gpt-6-astra', status: { available: providerReady, key_source: providerReady ? 'literal' : 'none' } }] };
  const catalog = { result: { models: {}, providers: { openai: { available: true } }, catalogs: { openai: { models: [{ id: 'gpt-6-astra', reasoning_efforts: ['low', 'high'] }, { id: 'gpt-5.5', reasoning_efforts: ['low', 'high'] }] } } } };
  const configuration = { revision: '1', default_effort: 'high', default_execution_engine: 'starlark' };
  const raw = {
    getSnapshot: () => snapshot, subscribe: () => () => {}, supports: () => false,
    configuration: { get: vi.fn(async () => configuration), update: vi.fn() },
    providers: { setKey: vi.fn(async () => ({})), catalogs: vi.fn(async () => catalog), list: vi.fn(async () => inventory), login: { list: vi.fn(async () => ({ flows: [] })) } },
    host: { directories: vi.fn(async ({ path }: { path?: string }) => ({ path: path || '/project/whip', entries: [] })), pickDirectory: vi.fn() },
    sessions: { create: vi.fn((params: unknown) => ({ params })) },
    upload: vi.fn(async () => ({ asAttachment: (kind: string, name: string) => ({ kind, name, ref: 'uploaded' }) })),
    session: vi.fn((rootId: string) => ({ rootId, client: raw, submit: vi.fn((payload: unknown) => ({ rootId, payload })), command: vi.fn((operation: string, payload: unknown) => ({ rootId, operation, payload })) })),
  };
  const client = raw as unknown as WhipClient;
  const recent = { page: { items: [{ cwd: '/remote/recent', updated_at: '2026-09-13' }] } };
  const host = { id: 'local', runtimeId: 'host', name: remoteHost ? 'Kuzco' : 'Local', local: !remoteHost, state: 'connected', client,
    profile: { target: remoteHost ? { kind: 'ssh', user: 'sam', host: 'kuzco' } : { kind: 'local' } },
    list: { subscribe: () => () => {}, getSnapshot: () => recent },
  } as unknown as HostConnection;
  const remote = { id: 'remote', runtimeId: 'remote-host', name: 'Mac mini', local: false, state: 'closed', endpoint: 'http://mm.local:8080', profile: { target: { kind: 'url', endpoint: 'http://mm.local:8080' } } } as HostConnection;
  const appState = { ...runtime.getSnapshot(), hosts: [host, remote] };
  vi.spyOn(runtime, 'getSnapshot').mockReturnValue(appState);
  const connect = vi.spyOn(runtime.connections, 'connect').mockResolvedValue();
  // The command runner is exercised elsewhere; here it accepts immediately and returns the created root.
  const run = vi.spyOn(runtime, 'run').mockImplementation((async (_handle: unknown, label: string, onAccepted?: () => void) => { onAccepted?.(); return label === 'Create session' ? { status: 'succeeded', result: { root_id: 'created' } } : { status: 'succeeded' }; }) as never);
  const tab = runtime.tabs.openNew({ hostProfileId: 'local', runtimeId: 'host', cwd: '/project/whip' });
  for (const [key, data] of [['provider-list', inventory], ['provider-catalogs', catalog], ['runtime-configuration', configuration], ['provider-login-flows', { flows: [] }]] as const)
    runtime.queries.setQueryData([key, 'host'], data);
  function Content() {
    useSessionTabs();
    const current = runtime.tabs.workspace().tabs.find(item => item.id === tab.id);
    return current?.kind === 'new' ? <Welcome tab={current} focused={focused} /> : <p>Promoted to {current?.kind === 'chat' ? current.rootId : 'nothing'}</p>;
  }
  const route = createRootRoute({ component: () => <RuntimeContext.Provider value={runtime}><QueryClientProvider client={runtime.queries}><ThemeProvider initialTheme="dark"><UIProvider><Content /></UIProvider></ThemeProvider></QueryClientProvider></RuntimeContext.Provider> });
  const router = createRouter({ routeTree: route, history: createMemoryHistory({ initialEntries: ['/'] }) });
  return { runtime, tab, raw, inventory, run, connect, pickDirectory, render: () => render(<RouterProvider router={router} />) };
}
