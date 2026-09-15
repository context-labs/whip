import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { QueryClientProvider } from '@tanstack/react-query';
import { createMemoryHistory, createRootRoute, createRouter, RouterProvider } from '@tanstack/react-router';
import { ThemeProvider, UIProvider } from '@whip/ui';
import type { WhipClient } from '@whip/sdk';
import { AppRuntime } from '../src/runtime';
import { RuntimeContext, useSessionTabs } from '../src/context';
import { Welcome } from '../src/welcome';
import type { HostConnection } from '../src/hosts';
import { welcomeDraftKey, type NewChatTab } from '../src/session-tabs';
import { fakeMCPImport, supportsImport, twoServers } from './mcp-import-fake';

const runtimes: AppRuntime[] = [];
beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => { for (const runtime of runtimes.splice(0)) runtime.dispose(); vi.unstubAllGlobals(); });

function fixture(remoteHost = false, providerReady = true) {
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
    session: vi.fn((rootId: string) => ({ submit: vi.fn((payload: unknown) => ({ rootId, payload })), command: vi.fn((operation: string, payload: unknown) => ({ rootId, operation, payload })) })),
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
    return current?.kind === 'new' ? <Welcome tab={current} /> : <p>Promoted to {current?.kind === 'chat' ? current.rootId : 'nothing'}</p>;
  }
  const route = createRootRoute({ component: () => <RuntimeContext.Provider value={runtime}><QueryClientProvider client={runtime.queries}><ThemeProvider initialTheme="dark"><UIProvider><Content /></UIProvider></ThemeProvider></QueryClientProvider></RuntimeContext.Provider> });
  const router = createRouter({ routeTree: route, history: createMemoryHistory({ initialEntries: ['/'] }) });
  return { runtime, tab, raw, inventory, run, connect, pickDirectory, render: () => render(<RouterProvider router={router} />) };
}

it('keeps the ready composer focused and saves model/effort choices to this draft only', async () => {
  const f = fixture(); f.render();
  const input = await screen.findByRole('textbox', { name: 'Your first message' });
  expect(screen.queryByText('Change host')).toBeNull();
  expect(screen.queryByLabelText('Execution language')).toBeNull();
  expect(screen.queryByRole('region', { name: 'Provider setup' })).toBeNull();
  fireEvent.change(input, { target: { value: 'Explain the code' } });
  fireEvent.click(screen.getByRole('button', { name: 'Model', exact: true }));
  fireEvent.click(await screen.findByRole('option', { name: /gpt-5.5/ }));
  fireEvent.click(screen.getByRole('button', { name: 'Reasoning effort' }));
  fireEvent.click(await screen.findByRole('menuitem', { name: 'High' }));
  expect(f.runtime.tabs.workspace().tabs.find(item => item.id === f.tab.id)).toMatchObject({ model: 'gpt-5.5', provider: 'openai', effort: 'high' });
  expect(f.raw.configuration.update).not.toHaveBeenCalled();
  expect(f.runtime.draft(welcomeDraftKey(f.tab.id))).toBe('Explain the code');
  fireEvent.click(screen.getByRole('button', { name: 'Send first message' }));
  await waitFor(() => expect(f.raw.sessions.create).toHaveBeenCalledWith(expect.objectContaining({ cwd: '/project/whip', model: 'gpt-5.5', provider: 'openai', permission_mode: 'prompt', execution_engine: 'starlark' })));
  await screen.findByText('Promoted to created');
  expect(f.raw.session).toHaveBeenCalledWith('created');
  expect(f.raw.session.mock.results[0]!.value.command).toHaveBeenCalledWith('session.effort', { effort: 'high', persist_default: false });
  expect(f.raw.session.mock.results[1]!.value.submit).toHaveBeenCalledWith({ text: 'Explain the code' });
  expect(f.run.mock.calls.map(call => call[1])).toEqual(['Create session', 'Set initial reasoning effort', 'Send first message']);
  expect(f.runtime.draft(welcomeDraftKey(f.tab.id))).toBe('');
});

it('switches execution host without sending or losing the prompt and clears host-specific choices', async () => {
  const f = fixture(); f.render();
  fireEvent.change(await screen.findByRole('textbox', { name: 'Your first message' }), { target: { value: 'Keep this task' } });
  fireEvent.click(screen.getByRole('button', { name: 'Execution host' }));
  const item = await screen.findByRole('menuitem', { name: /Mac mini/ });
  expect(item.textContent).toContain('Disconnected'); expect(item.textContent).toContain('mm.local:8080');
  fireEvent.click(item);
  await waitFor(() => expect(f.connect).toHaveBeenCalledWith('remote'));
  expect(f.runtime.tabs.workspace().tabs.find(item => item.id === f.tab.id)).toMatchObject({ hostProfileId: 'remote', cwd: '' });
  expect(f.runtime.draft(welcomeDraftKey(f.tab.id))).toBe('Keep this task');
  expect(f.raw.sessions.create).not.toHaveBeenCalled();
});

it('opens the native system picker directly from the local folder control', async () => {
  const f = fixture(); f.render();
  const folder = await screen.findByRole('button', { name: 'Project folder' });
  expect(screen.queryByRole('button', { name: 'Browse host' })).toBeNull();
  fireEvent.click(folder);
  await waitFor(() => expect(f.runtime.tabs.workspace().tabs.find(item => item.id === f.tab.id)).toMatchObject({ cwd: '/selected/project' }));
  expect(f.pickDirectory).toHaveBeenCalledOnce();
  expect(screen.queryByRole('dialog')).toBeNull();
  expect(f.raw.host.pickDirectory).not.toHaveBeenCalled();
  expect(f.raw.host.directories).not.toHaveBeenCalled();
  expect(f.raw.sessions.create).not.toHaveBeenCalled();
});

it('uses remote recents and sends the confirmed remote directory with the retained draft', async () => {
  const f = fixture(true); f.render();
  fireEvent.change(await screen.findByRole('textbox', { name: 'Your first message' }), { target: { value: 'Explain the remote project' } });
  fireEvent.click(screen.getByRole('button', { name: 'Project folder' }));
  await screen.findByRole('dialog', { name: 'Choose a folder' });
  expect(screen.getByText('sam@kuzco')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'recent', exact: true }));
  await waitFor(() => expect((screen.getByRole('button', { name: 'Choose folder', exact: true }) as HTMLButtonElement).disabled).toBe(false));
  fireEvent.click(screen.getByRole('button', { name: 'Choose folder', exact: true }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(f.runtime.tabs.workspace().tabs.find(item => item.id === f.tab.id)).toMatchObject({ cwd: '/remote/recent' });
  expect(f.pickDirectory).not.toHaveBeenCalled(); expect(f.raw.host.pickDirectory).not.toHaveBeenCalled();
  expect(f.raw.sessions.create).not.toHaveBeenCalled();
  expect(f.runtime.draft(welcomeDraftKey(f.tab.id))).toBe('Explain the remote project');
  fireEvent.click(screen.getByRole('button', { name: 'Send first message' }));
  await waitFor(() => expect(f.raw.sessions.create).toHaveBeenCalledWith(expect.objectContaining({ cwd: '/remote/recent' })));
});

it('keeps an unsupported saved effort visible and requires an explicit replacement before sending', async () => {
  const f = fixture();
  f.runtime.tabs.updateNew(f.tab.id, { effort: 'high' });
  f.render();
  fireEvent.change(await screen.findByRole('textbox', { name: 'Your first message' }), { target: { value: 'Use my saved choice' } });
  act(() => { f.runtime.queries.setQueryData(['provider-catalogs', 'host'], { result: { models: {}, providers: {}, catalogs: {} } }); });
  await screen.findByText('Choose an available reasoning effort for this model before sending.');
  expect(screen.getByRole('button', { name: 'Reasoning effort' }).textContent).toContain('High');
  fireEvent.keyDown(screen.getByRole('textbox', { name: 'Your first message' }), { key: 'Enter' });
  expect(f.raw.sessions.create).not.toHaveBeenCalled();
  expect(f.runtime.tabs.workspace().tabs.find(item => item.id === f.tab.id)).toMatchObject({ effort: 'high' });
  fireEvent.click(screen.getByRole('button', { name: 'Reasoning effort' }));
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Default' }));
  fireEvent.click(screen.getByRole('button', { name: 'Send first message' }));
  await waitFor(() => expect(f.raw.sessions.create).toHaveBeenCalledOnce());
  await waitFor(() => expect(f.raw.session.mock.results[0]!.value.command).toHaveBeenCalledWith('session.effort', { effort: 'off', persist_default: false }));
});

it('shows standalone provider setup, reuses the connection dialog and restores the draft after explicit model confirmation', async () => {
  const f = fixture(false, false);
  f.runtime.setDraft(welcomeDraftKey(f.tab.id), 'Keep my task'); f.render();
  await screen.findByRole('heading', { name: 'Connect a provider to get started', level: 1 });
  expect(screen.queryByRole('textbox', { name: 'Your first message' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'Project folder' })).toBeNull();
  expect(screen.getByRole('button', { name: 'Connect Remote' })).toBeTruthy();
  f.raw.providers.setKey.mockImplementation(async () => {
    f.raw.providers.list.mockResolvedValue({ ...f.inventory, providers: [{ ...f.inventory.providers[0], status: { available: true, key_source: 'literal' } }] });
    return {};
  });
  f.raw.configuration.update.mockImplementation(async () => {
    f.raw.providers.list.mockResolvedValue({ ...f.inventory, selection: { ...f.inventory.selection, ready: true }, providers: [{ ...f.inventory.providers[0], status: { available: true, key_source: 'literal' } }] });
  });
  fireEvent.click(await screen.findByRole('button', { name: 'Connect OpenAI' }));
  fireEvent.change(await screen.findByLabelText('API key'), { target: { value: 'fixture-api-key' } });
  fireEvent.click(screen.getByRole('button', { name: 'Connect', exact: true }));
  const confirm = await screen.findByRole('button', { name: 'Use gpt-6-astra', exact: true });
  expect(f.raw.configuration.update).not.toHaveBeenCalled(); expect(f.raw.sessions.create).not.toHaveBeenCalled();
  fireEvent.click(confirm);
  const input = await screen.findByRole('textbox', { name: 'Your first message' });
  expect((input as HTMLTextAreaElement).value).toBe('Keep my task');
  expect(screen.queryByRole('region', { name: 'Provider setup' })).toBeNull();
  expect(f.raw.providers.setKey).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ provider: 'openai', key: 'fixture-api-key' }), expect.anything());
  expect(f.raw.configuration.update).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ default_model: 'gpt-6-astra', default_provider: 'openai' }), expect.anything());
  expect(f.raw.sessions.create).not.toHaveBeenCalled();
});

it('keeps the composer footprint while the provider inventory is pending, then defers to setup', async () => {
  const f = fixture(false, false);
  f.runtime.queries.removeQueries({ queryKey: ['provider-list', 'host'] });
  let resolveList!: (value: unknown) => void;
  f.raw.providers.list.mockImplementation(() => new Promise(resolve => { resolveList = resolve; }));
  f.render();
  await screen.findByRole('textbox', { name: 'Your first message' });
  expect(screen.getByRole('heading', { name: 'What do you want to work on?', level: 1 })).toBeTruthy();
  expect(screen.queryByText('Connect a provider')).toBeNull();
  expect(screen.queryByRole('region', { name: 'Provider setup' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'Reasoning effort' })).toBeNull();
  expect(document.querySelectorAll('[data-picker-skeleton]')).toHaveLength(2);
  await act(async () => { resolveList(f.inventory); });
  await screen.findByRole('heading', { name: 'Connect a provider to get started', level: 1 });
  expect(screen.queryByRole('textbox', { name: 'Your first message' })).toBeNull();
  expect(document.querySelectorAll('[data-picker-skeleton]')).toHaveLength(0);
});

it('renders the composer disabled while the host is still connecting', async () => {
  const f = fixture();
  const connecting = { state: 'connecting' };
  f.raw.getSnapshot = () => connecting as never;
  f.render();
  await screen.findByRole('textbox', { name: 'Your first message' });
  expect(screen.getByRole('heading', { name: 'What do you want to work on?', level: 1 })).toBeTruthy();
  expect((screen.getByRole('button', { name: 'Send first message' }) as HTMLButtonElement).disabled).toBe(true);
  expect(screen.getByText(/^Connecting to Local\./)).toBeTruthy();
  expect(screen.queryByText('Review unavailable session options')).toBeNull();
  expect(document.querySelectorAll('[data-picker-skeleton]')).toHaveLength(2);
});

it('paints provider setup first when the device remembers this host has no ready provider', async () => {
  const f = fixture(false, false);
  f.runtime.queries.removeQueries({ queryKey: ['provider-list', 'host'] });
  f.runtime.platform.storage.setItem('whip.web.provider-ready.v1:host', 'false');
  f.raw.providers.list.mockImplementation(() => new Promise(() => {}));
  f.render();
  await screen.findByRole('heading', { name: 'Connect a provider to get started', level: 1 });
  expect(screen.queryByRole('textbox', { name: 'Your first message' })).toBeNull();
  expect(document.querySelectorAll('[data-picker-skeleton]')).toHaveLength(0);
});

it('remembers the inventory answer on the device', async () => {
  const f = fixture();
  f.render();
  await screen.findByRole('textbox', { name: 'Your first message' });
  await waitFor(() => expect(f.runtime.platform.storage.getItem('whip.web.provider-ready.v1:host')).toBe('true'));
});

it('keeps the draft and shows the delivery notice while the first send is unresolved', async () => {
  const f = fixture();
  const checkCommand = vi.spyOn(f.runtime, 'checkCommand').mockResolvedValue(undefined);
  vi.mocked(f.runtime.getSnapshot).mockReturnValue({ ...f.runtime.getSnapshot(), commands: [{ id: 'pending', commandId: 'c1', runtimeId: 'host', label: 'Create session', status: 'Acceptance unresolved', draftKey: welcomeDraftKey(f.tab.id), delivery: 'uncertain' }] });
  f.runtime.setDraft(welcomeDraftKey(f.tab.id), 'Still here');
  f.render();
  expect((await screen.findByRole('textbox', { name: 'Your first message' }) as HTMLTextAreaElement).value).toBe('Still here');
  expect(screen.getByText('Checking whether your first message was received')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Send first message' }));
  expect(f.raw.sessions.create).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Check status' }));
  expect(checkCommand).toHaveBeenCalledWith('pending');
});

it('keeps the draft and reports the error when the first send fails before acceptance', async () => {
  const f = fixture();
  f.run.mockImplementationOnce((async () => { throw new Error('Host refused the session'); }) as never);
  f.render();
  fireEvent.change(await screen.findByRole('textbox', { name: 'Your first message' }), { target: { value: 'Try this' } });
  fireEvent.click(screen.getByRole('button', { name: 'Send first message' }));
  await screen.findByText('Host refused the session');
  expect(f.runtime.draft(welcomeDraftKey(f.tab.id))).toBe('Try this');
  expect(f.runtime.tabs.workspace().tabs.find(item => item.id === f.tab.id)).toMatchObject({ kind: 'new' });
});

// A host that has MCP servers configured for other agents gets one offer
// before the composer; answering it returns the composer.
function withMCPImport(f: ReturnType<typeof fixture>, offered = false) {
  const mcpImport = fakeMCPImport(twoServers(offered));
  Object.assign(f.raw, { supports: supportsImport, mcpImport });
  f.runtime.queries.setQueryData(['runtime-configuration', 'host'], (old: object | undefined) => ({ ...old, mcp_import_offered: offered }));
  return mcpImport;
}

it('offers the MCP import once a provider is ready and returns the composer after Skip', async () => {
  const f = fixture(); const mcpImport = withMCPImport(f); f.render();
  await screen.findByRole('heading', { name: 'Bring your MCP servers into Whip' });
  expect(screen.queryByRole('textbox', { name: 'Your first message' })).toBeNull();
  expect(screen.getByRole('checkbox', { name: 'Import paper' })).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Execution host' })).toBeTruthy();
  expect(mcpImport.candidates).toHaveBeenCalledWith({ cwd: '/project/whip' }, expect.anything());
  fireEvent.click(screen.getByRole('button', { name: 'Skip for now' }));
  await screen.findByRole('textbox', { name: 'Your first message' });
  expect(mcpImport.apply).toHaveBeenCalledExactlyOnceWith({ cwd: '/project/whip', names: [] });
  expect(screen.getByRole('heading', { name: 'What do you want to work on?' })).toBeTruthy();
});

it('does not offer the import before a provider is ready, after it was answered, or on a daemon without it', async () => {
  const notReady = fixture(false, false); const pending = withMCPImport(notReady); notReady.render();
  await screen.findByRole('region', { name: 'Provider setup' });
  expect(screen.queryByRole('heading', { name: 'Bring your MCP servers into Whip' })).toBeNull();
  expect(pending.candidates).not.toHaveBeenCalled();
  notReady.runtime.dispose();
  const answered = fixture(); const done = withMCPImport(answered, true); answered.render();
  await screen.findByRole('textbox', { name: 'Your first message' });
  expect(screen.queryByRole('heading', { name: 'Bring your MCP servers into Whip' })).toBeNull();
  expect(done.candidates).not.toHaveBeenCalled();
  answered.runtime.dispose();
  const older = fixture(); const unsupported = withMCPImport(older); Object.assign(older.raw, { supports: () => false }); older.render();
  await screen.findByRole('textbox', { name: 'Your first message' });
  expect(unsupported.candidates).not.toHaveBeenCalled();
});
