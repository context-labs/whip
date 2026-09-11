import { useSyncExternalStore } from 'react';
import { SessionTabs, type NewChatTab } from '../src/session-tabs';
import { welcomeDraftKey } from '../src/welcome-submission';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { beforeEach, afterEach, expect, it, vi } from 'vitest';
import { ThemeProvider, UIProvider } from '@whip/ui';
import type { ProviderList } from '@whip/protocol';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';
import { Welcome } from '../src/welcome';

const route = vi.hoisted(() => ({ search: {} as { cwd?: string; runtimeId?: string }, location: { state: { __TSR_key: 'initial' } }, navigate: vi.fn() }));
vi.mock('@tanstack/react-router', () => ({
  Link: ({ children }: { children: React.ReactNode }) => <a>{children}</a>,
  useSearch: () => route.search,
  useLocation: ({ select }: { select(value: typeof route.location): unknown }) => select(route.location),
  useNavigate: () => route.navigate,
  useRouter: () => ({ state: { location: route.location } }),
}));
vi.mock('../src/directory-picker', () => ({ DirectoryPicker: ({ onSelect, disabled }: { onSelect(value: string): void; disabled: boolean }) => <button type="button" disabled={disabled} onClick={() => onSelect('/edited')}>Choose folder</button> }));
beforeEach(() => { vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })); route.search = { cwd: '/repo', runtimeId: 'host' }; route.location = { state: { __TSR_key: 'initial' } }; route.navigate.mockClear(); });
afterEach(() => vi.unstubAllGlobals());
const provider = (id: string, available: boolean) => ({ id, name: id === 'inference-net' ? 'Inference.net' : 'OpenRouter', custom: false, recommended: id === 'inference-net', suggested_model: available ? 'coding-model' : '', methods: ['api_key'], status: { provider: id, available, configured: available, key_source: available ? 'environment' : 'none', auth_state: available ? 'connected' : 'key_required', warnings: [] } });
function fixture(ready = true, entries = [provider('inference-net', ready), provider('openrouter', false)], selectionMissing = false) {
  const execution_engines = [{ id: 'starlark', language: 'starlark', label: 'Starlark' }, { id: 'quickjs', language: 'javascript', label: 'JavaScript (QuickJS)' }];
  const snapshot = { state: 'connected', info: { runtime_id: 'host', default_execution_engine: 'starlark', execution_engines } };
  const inventory: ProviderList = { revision: '1', default_provider: 'inference-net', selection: selectionMissing ? undefined : { ready, model: 'coding-model', provider: 'inference-net', reason: ready ? 'ready' : 'provider_required' }, providers: entries };
  const client = { subscribe: () => () => {}, getSnapshot: () => snapshot, supports: () => true,
    agents: { list: vi.fn(async () => ({ items: [{ id: 'coding', revision: '', built_in: true, registered_by: '', created_at: '' }, { id: 'junior-developer', revision: '', built_in: true, registered_by: '', created_at: '' }, { id: 'support-triage', revision: 'b'.repeat(64), built_in: false, registered_by: 'app', created_at: '' }] })) },
    configuration: { get: vi.fn(async () => ({ default_execution_engine: 'starlark' })), update: vi.fn(async (patch: { default_model: string; default_provider: string }) => { inventory.selection = { ready: true, model: patch.default_model, provider: patch.default_provider, reason: 'ready' }; return {}; }) },
    providers: {
      list: vi.fn(async () => ({ ...inventory })), catalogs: vi.fn(async () => ({ result: { models: {}, providers: {}, catalogs: {} } })),
      login: { list: vi.fn(async () => ({ flows: [] })) },
      setKey: vi.fn(async (params: { provider: string }) => { const entry = inventory.providers!.find(entry => entry.id === params.provider)!; entry.status.available = true; entry.status.key_source = 'literal'; entry.suggested_model = 'coding-model'; }),
    } };
  const otherConnection = { state: 'connected', info: { runtime_id: 'another', default_execution_engine: 'starlark', execution_engines } };
  const other = { ...client, getSnapshot: () => otherConnection };
  const state = { commands: [], hosts: [
    { id: 'local', name: 'Local', local: true, runtimeId: 'host', state: 'connected', client, profile: { target: { kind: 'url' } } },
    { id: 'remote', name: 'Kuzco', local: false, runtimeId: 'another', state: 'connected', client: other, profile: { target: { kind: 'url' } } },
  ] };
  const drafts = new Map<string, string>(); const subscribers = new Set<() => void>();
  const start = vi.fn(async () => 'created');
  const query = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  const tabs = new SessionTabs();
  const first = tabs.openNew({ runtimeId: 'host', hostProfileId: 'local', cwd: '/repo' });
  let active = first.id;
  let focused = true;
  function Panel() {
    useSyncExternalStore(tabs.subscribe, tabs.getSnapshot);
    return <Welcome key={active} focused={focused} tab={tabs.workspace().tabs.find(tab => tab.id === active) as NewChatTab} />;
  }
  const runtime = {
    tabs, orphanWelcomeDrafts: () => [],
    queries: query, platform: { copy: vi.fn(async () => {}), openExternal: vi.fn(async () => {}) },
    connections: { isAttached: () => true, select: vi.fn() }, subscribe: () => () => {}, getSnapshot: () => state,
    lastSession: () => undefined, welcome: { list: () => [], get: () => undefined, start, finish: vi.fn(), subscribe: () => () => {}, getSnapshot: () => 0 },
    draft: (key: string) => drafts.get(key) ?? '', setDraft: (key: string, value: string) => { drafts.set(key, value); subscribers.forEach(listener => listener()); },
    subscribeDraft: (_key: string, listener: () => void) => { subscribers.add(listener); return () => subscribers.delete(listener); }, report: vi.fn(),
  } as unknown as AppRuntime;
  const tree = () => <RuntimeContext.Provider value={runtime}><ThemeProvider initialTheme="light"><UIProvider><QueryClientProvider client={query}><Panel /></QueryClientProvider></UIProvider></ThemeProvider></RuntimeContext.Provider>;
  const view = render(tree());
  return { runtime, client, inventory, start, drafts, query, tabs, first, focus: (value: boolean) => { focused = value; view.rerender(tree()); }, select: (id: string) => { active = id; view.rerender(tree()); }, ...view, rerender: () => view.rerender(tree()) };
}
it('keeps independent drafts and setup through tab and host switches without sending', async () => {
  const f = fixture();
  expect(screen.getByTitle('/repo')).toBeTruthy(); expect(f.start).not.toHaveBeenCalled();
  fireEvent.change(screen.getByLabelText('Your first message'), { target: { value: 'Local draft' } });
  fireEvent.click(screen.getByRole('button', { name: 'Choose folder' }));
  expect(screen.getByTitle('/edited')).toBeTruthy();
  let second!: NewChatTab;
  act(() => { second = f.tabs.openNew({ runtimeId: 'host', hostProfileId: 'local', cwd: '/second' }); });
  f.select(second.id);
  expect((screen.getByLabelText('Your first message') as HTMLTextAreaElement).value).toBe('');
  fireEvent.change(screen.getByLabelText('Your first message'), { target: { value: 'Second draft' } });
  act(() => { f.tabs.updateNew(second.id, { hostProfileId: 'remote', runtimeId: 'another' }); });
  expect((screen.getByLabelText('Your first message') as HTMLTextAreaElement).value).toBe('Second draft');
  expect(screen.getByTitle('/second')).toBeTruthy();
  f.select(f.first.id);
  expect((screen.getByLabelText('Your first message') as HTMLTextAreaElement).value).toBe('Local draft');
  expect(screen.getByTitle('/edited')).toBeTruthy(); expect(f.start).not.toHaveBeenCalled();
});
it('sends the edited folder, explicit ready pair, prompt and default Ask only after Send', async () => {
  const f = fixture();
  await screen.findByRole('button', { name: 'Send first message' });
  expect(screen.queryByRole('region', { name: 'Provider setup' })).toBeNull();
  expect(f.client.providers.catalogs).not.toHaveBeenCalled();
  fireEvent.change(screen.getByLabelText('Your first message'), { target: { value: 'Explain auth' } });
  fireEvent.click(screen.getByRole('button', { name: 'Choose folder' }));
  fireEvent.click(screen.getByRole('button', { name: 'Send first message' }));
  await waitFor(() => expect(f.start).toHaveBeenCalledExactlyOnceWith(f.first.id, f.client, { cwd: '/edited', model: 'coding-model', provider: 'inference-net', permission_mode: 'prompt', execution_engine: 'starlark' }));
  expect(f.drafts.get(welcomeDraftKey(f.first.id))).toBe('Explain auth');
  expect(route.navigate).not.toHaveBeenCalled(); // App-owned completion promotes; a panel never navigates on acceptance.
});

it('retains the selected execution language in its draft and sends it explicitly', async () => {
  const f = fixture();
  await screen.findByRole('button', { name: 'Send first message' });
  fireEvent.click(screen.getByRole('combobox', { name: 'Execution language' }));
  const option = await screen.findByRole('option', { name: 'JavaScript (QuickJS)' });
  fireEvent.pointerDown(option); fireEvent.click(option);
  expect((f.tabs.workspace().tabs[0] as NewChatTab).executionEngine).toBe('quickjs');
  fireEvent.change(screen.getByLabelText('Your first message'), { target: { value: 'Use this language' } });
  fireEvent.click(screen.getByRole('button', { name: 'Send first message' }));
  await waitFor(() => expect(f.start).toHaveBeenCalledWith(f.first.id, f.client, expect.objectContaining({ execution_engine: 'quickjs' })));
});

it('does not offer unadvertised execution engines or send with missing discovery', async () => {
  const f = fixture();
  f.client.getSnapshot().info.execution_engines = [];
  f.rerender();
  fireEvent.change(screen.getByLabelText('Your first message'), { target: { value: 'Keep this task' } });
  expect((await screen.findByRole('button', { name: 'Send first message' }) as HTMLButtonElement).disabled).toBe(true);
  fireEvent.keyDown(screen.getByLabelText('Your first message'), { key: 'Enter' });
  expect(f.start).not.toHaveBeenCalled();
  expect(screen.getByRole('alert').textContent).toContain('does not advertise');
});
it('offers only one default confirmation for the detected OpenRouter route with no promotion detour', async () => {
  const f = fixture(false, [provider('inference-net', false), provider('openrouter', true)]);
  const panel = await screen.findByRole('region', { name: 'Provider setup' });
  const available = await within(panel).findByRole('button', { name: 'Use OpenRouter' });
  const connect = within(panel).getByRole('button', { name: 'Connect Inference.net' });
  expect(available.compareDocumentPosition(connect) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  expect(screen.getAllByText('Recommended')).toHaveLength(1);
  fireEvent.change(screen.getByLabelText('Your first message'), { target: { value: 'Retain this task' } });
  fireEvent.click(await screen.findByRole('button', { name: 'Use coding-model' }));
  await waitFor(() => expect(f.client.configuration.update).toHaveBeenCalledExactlyOnceWith({ revision: '1', default_model: 'coding-model', default_provider: 'openrouter', default_effort: '' }, { signal: expect.any(AbortSignal) }));
  await waitFor(() => expect(screen.queryByRole('region', { name: 'Provider setup' })).toBeNull());
  expect((screen.getByLabelText('Your first message') as HTMLTextAreaElement).value).toBe('Retain this task');
  expect(f.start).not.toHaveBeenCalled();
});
it('connects a key directly beside the welcome draft, masks it, then asks for the explicit model pair', async () => {
  const f = fixture(false);
  fireEvent.change(screen.getByLabelText('Your first message'), { target: { value: 'Keep me while signing in' } });
  fireEvent.click(await screen.findByRole('button', { name: 'Connect OpenRouter' }));
  const key = await screen.findByLabelText('API key') as HTMLInputElement;
  expect(key.type).toBe('password'); fireEvent.change(key, { target: { value: 'secret-key' } });
  fireEvent.click(screen.getByRole('button', { name: 'Connect', exact: true }));
  await screen.findByRole('button', { name: 'Use coding-model' });
  expect(f.client.configuration.update).not.toHaveBeenCalled();
  expect((screen.getByLabelText('Your first message') as HTMLTextAreaElement).value).toBe('Keep me while signing in');
  expect(JSON.stringify([...f.drafts])).not.toContain('secret-key');
  expect(JSON.stringify(f.query.getQueryCache().getAll().map(query => query.state.data))).not.toContain('secret-key');
  expect(f.start).not.toHaveBeenCalled();
});
it('does not navigate away from a later route when the first submission resolves', async () => {
  const f = fixture(); let finish!: (rootId: string) => void;
  f.start.mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
  fireEvent.change(screen.getByLabelText('Your first message'), { target: { value: 'Task' } });
  fireEvent.click(await screen.findByRole('button', { name: 'Send first message' }));
  await waitFor(() => expect(f.start).toHaveBeenCalledOnce());
  route.location = { state: { __TSR_key: 'another-visit' } }; f.rerender();
  await act(async () => { finish('created'); });
  expect(route.navigate).not.toHaveBeenCalled();
});

it('retains the draft and explains a compatible old host needs updating instead of looping through setup', async () => {
  const f = fixture(false, [provider('inference-net', true)], true);
  fireEvent.change(screen.getByLabelText('Your first message'), { target: { value: 'Keep this task' } });
  await screen.findByText('Update Whip on Local to use provider setup. Your draft is preserved.');
  expect(screen.queryByRole('button', { name: 'Use Inference.net' })).toBeNull();
  expect((screen.getByRole('button', { name: 'Set up provider' }) as HTMLButtonElement).disabled).toBe(true);
  fireEvent.keyDown(screen.getByLabelText('Your first message'), { key: 'Enter' });
  expect(f.start).not.toHaveBeenCalled(); expect(f.client.configuration.update).not.toHaveBeenCalled();
  expect(f.client.providers.catalogs).not.toHaveBeenCalled();
  expect((screen.getByLabelText('Your first message') as HTMLTextAreaElement).value).toBe('Keep this task');
});

it('does not focus a background pane composer when provider setup completes', async () => {
  const f = fixture(false, [provider('openrouter', true)]);
  const confirm = await screen.findByRole('button', { name: 'Use coding-model' });
  f.focus(false);
  confirm.focus();
  fireEvent.click(confirm);
  await waitFor(() => expect(screen.queryByRole('region', { name: 'Provider setup' })).toBeNull());
  await new Promise(resolve => setTimeout(resolve, 30));
  expect(document.activeElement).not.toBe(screen.getByLabelText('Your first message'));
  expect(f.start).not.toHaveBeenCalled();
});
it('locks editable text for both active admission and retained unresolved first messages', async () => {
  const f = fixture();
  let finish!: (id: string) => void;
  f.start.mockImplementation(() => new Promise<string>(resolve => { finish = resolve; }));
  fireEvent.change(screen.getByLabelText('Your first message'), { target: { value: 'Frozen task' } });
  fireEvent.click(await screen.findByRole('button', { name: 'Send first message' }));
  expect((screen.getByLabelText('Your first message') as HTMLTextAreaElement).disabled).toBe(true);
  vi.spyOn(f.runtime.welcome, 'get').mockReturnValue({ state: 'creating', create: { commandId: 'create', runtimeId: 'host' }, input: { commandId: 'input' } } as never);
  await act(async () => { finish('created'); });
  f.rerender();
  expect((screen.getByLabelText('Your first message') as HTMLTextAreaElement).disabled).toBe(true);
});
it('retries accepted journal retirement from recovery even when its tab is already a chat', async () => {
  const f = fixture();
  let promotedId!: string;
  act(() => { promotedId = f.tabs.open('host', 'accepted-root'); });
  const promoted = f.tabs.workspace().tabs.find(tab => tab.id === promotedId)!;
  const item = { draftId: promoted.id, state: 'accepted', create: { runtimeId: 'host', commandId: 'original' }, params: { cwd: '/accepted' } };
  vi.spyOn(f.runtime.welcome, 'list').mockReturnValue([item] as never);
  vi.spyOn(f.runtime.welcome, 'get').mockImplementation(id => id === promoted.id ? item as never : undefined);
  const completeAccepted = vi.fn(async () => 'accepted-root');
  Object.assign(f.runtime.welcome, { completeAccepted });
  Object.assign(f.runtime, { recoverWelcome: vi.fn(() => promoted) });
  f.rerender();
  fireEvent.click(screen.getByRole('button', { name: 'Recover first message · Local · /accepted' }));
  await waitFor(() => expect(completeAccepted).toHaveBeenCalledExactlyOnceWith(promoted.id));
  await waitFor(() => expect(route.navigate).toHaveBeenCalledWith(expect.objectContaining({ to: '/h/$runtimeId/s/$rootId', params: { runtimeId: 'host', rootId: 'accepted-root' } })));
  expect(f.start).not.toHaveBeenCalled();
});

it('offers the host’s agent definitions and sends the chosen one with the first message', async () => {
  const f = fixture();
  fireEvent.change(screen.getByLabelText('Your first message'), { target: { value: 'Triage the queue' } });
  const picker = await screen.findByRole('combobox', { name: 'Agent' });
  fireEvent.click(picker);
  const option = await screen.findByRole('option', { name: 'support-triage' });
  fireEvent.pointerDown(option); fireEvent.click(option);
  await waitFor(() => expect((f.tabs.workspace().tabs.find(tab => tab.id === f.first.id) as { definition?: string }).definition).toBe('support-triage'));
  fireEvent.click(screen.getByRole('button', { name: 'Send first message' }));
  await waitFor(() => expect(f.start).toHaveBeenCalledOnce());
  expect(f.start.mock.calls[0][2]).toMatchObject({ cwd: '/repo', definition: 'support-triage', execution_engine: 'starlark' });
});
