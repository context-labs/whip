import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, expect, it, vi } from 'vitest';
import { UIProvider } from '@whip/ui';
import { RuntimeContext } from '../src/context';
import { SessionActionsProvider, useSessionActions } from '../src/session-actions';
import type { AppRuntime } from '../src/runtime';
import { SessionTabs, selectedSessionTab } from '../src/session-tabs';

const route = vi.hoisted(() => ({ location: { pathname: '/other' }, navigate: vi.fn(async () => {}) }));
vi.mock('@tanstack/react-router', () => ({ useLocation: () => route.location, useNavigate: () => route.navigate }));
beforeEach(() => { route.location = { pathname: '/other' }; route.navigate.mockClear(); });
function Rows({ visible = true }: { visible?: boolean }) {
  const actions = useSessionActions();
  return visible ? <><button onClick={() => actions.prepare(true)}>Refresh editors</button><button onClick={() => void actions.archive({ runtimeId: 'remote', rootId: 'same-root', title: 'Truncated…' }, true)}>Archive hover button</button>{actions.items({ runtimeId: 'remote', rootId: 'same-root', title: 'Truncated…' }).flatMap(item => item.items ?? [item]).map(item => <button key={item.id} disabled={item.disabled} onClick={item.onSelect}>{item.label}</button>)}</> : null;
}
function fixture() {
  const metadata = { root_id: 'same-root', title: 'The full title behind the truncated catalog', cwd: '/full/directory', archived: false, history_revision: '42', id: 'same-root', parent_id: null, tree_id: 'actual-tree', config_revision: '19', working_directory: '/full/directory' };
  const get = vi.fn(async () => metadata);
  const remove = vi.fn(async () => ({}));
  const tree = { id: 'actual-tree', revision: '9007199254740993', metadata: { title: metadata.title, archived: false, pinned: true } };
  const update = vi.fn(async (_id: string, _revision: string, patch: typeof tree.metadata, _options?: unknown) => ({ ...tree, metadata: patch }));
  const session = vi.fn(() => ({ get, history: { snapshot: async () => ({ session_id: 'same-root', revision: '42', through_sequence: '9007199254740995', message_count: '6' }) }, delete: remove }));
  const client = { trees: { get: vi.fn(async () => structuredClone(tree)), update }, session };
  const list = { refresh: vi.fn(async () => {}) };
  const host = { id: 'remote-profile', name: 'Remote workstation', client, list, state: 'connected', profile: { target: { kind: 'url' } } };
  const snapshot = { hosts: [host] };
  const run = vi.fn(async () => ({ root: { id: 'forked' } }));
  const workspace = { tabs: [], layout: { type: 'pane', id: 'main', tabs: [] }, focusedPaneId: 'main', closed: [] };
  const runtime = { getSnapshot: () => snapshot, subscribe: () => () => {}, connections: { host: vi.fn((id: string) => id === 'remote' ? host : undefined), signal: () => new AbortController().signal }, queries: { invalidateQueries: vi.fn(async () => {}) }, command: vi.fn((_client, method, params) => ({ method, params })),
    platform: { copy: vi.fn(async () => {}), storage: { getItem: () => null } },
    tabs: { workspace: () => workspace, open: vi.fn(() => 'fork-tab'), titles: vi.fn() },
    forgetSession: vi.fn(), run, report: vi.fn(), reportWorkspace: vi.fn() } as unknown as AppRuntime;
  const app = (visible = true) => <RuntimeContext.Provider value={runtime}><UIProvider><SessionActionsProvider><Rows visible={visible} /></SessionActionsProvider></UIProvider></RuntimeContext.Provider>;
  const view = render(app());
  return { get, metadata, tree, update, remove, session, host, list, runtime, run, workspace, rerender: (visible = true) => view.rerender(app(visible)) };
}
it('Open in new tab opens a fresh selected root chat without session work, while background still reuses', async () => {
  const f = fixture();
  const tabs = new SessionTabs();
  tabs.visit('remote', 'same-root', { view: 'repl', agent: 'child', panel: 'context' });
  const original = tabs.workspace().tabs[0]!;
  Object.assign(f.runtime, { tabs });
  fireEvent.click(screen.getByRole('button', { name: 'Open in new tab', exact: true }));
  await waitFor(() => expect(route.navigate).toHaveBeenCalledOnce());
  const tab = selectedSessionTab(tabs.workspace())!;
  expect(tab).toMatchObject({ kind: 'chat', rootId: 'same-root', runtimeId: 'remote', location: {} });
  expect(tab.id).not.toBe(original.id);
  expect(tabs.workspace().tabs[0]).toEqual(original);
  expect(route.navigate).toHaveBeenCalledWith(expect.objectContaining({ state: { whipViewId: tab.id } }));
  expect(f.get).not.toHaveBeenCalled(); expect(f.session).not.toHaveBeenCalled(); expect(f.run).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Open in background tab', exact: true }));
  expect(tabs.workspace().tabs).toHaveLength(2); expect(route.navigate).toHaveBeenCalledOnce();
});

it('Open in new tab permits known offline hosts but disables removed hosts', () => {
  const f = fixture();
  const tabs = new SessionTabs(); Object.assign(f.runtime, { tabs });
  f.host.state = 'disconnected'; f.rerender();
  expect(screen.getByRole('button', { name: 'Open in new tab', exact: true }).hasAttribute('disabled')).toBe(false);
  fireEvent.click(screen.getByRole('button', { name: 'Open in new tab', exact: true }));
  expect(tabs.workspace().tabs).toHaveLength(1); expect(route.navigate).toHaveBeenCalledOnce();
  expect(f.get).not.toHaveBeenCalled(); expect(f.session).not.toHaveBeenCalled(); expect(f.run).not.toHaveBeenCalled();
  vi.spyOn(f.runtime.connections, 'host').mockReturnValue(undefined); f.rerender();
  expect(screen.getByRole('button', { name: 'Open in new tab', exact: true }).hasAttribute('disabled')).toBe(true);
});

it('Copy session ID copies the root id, works offline, and reports failures', async () => {
  const f = fixture();
  fireEvent.click(screen.getByRole('button', { name: 'Copy session ID', exact: true }));
  await waitFor(() => expect(f.runtime.platform.copy).toHaveBeenCalledExactlyOnceWith('same-root'));
  await screen.findByText('Session ID copied');
  expect(f.get).not.toHaveBeenCalled(); expect(f.run).not.toHaveBeenCalled();
  f.host.state = 'disconnected'; f.rerender();
  expect(screen.getByRole('button', { name: 'Copy session ID', exact: true }).hasAttribute('disabled')).toBe(false);
  vi.spyOn(f.runtime.platform, 'copy').mockRejectedValueOnce(new Error('no clipboard'));
  fireEvent.click(screen.getByRole('button', { name: 'Copy session ID', exact: true }));
  expect(await screen.findByText('Could not copy session ID')).toBeTruthy();
});

it('Open in new tab reports full capacity without navigating', async () => {
  const f = fixture();
  const tabs = new SessionTabs();
  for (let i = 0; i < 32; i++) tabs.openChatView('remote', 'same-root');
  Object.assign(f.runtime, { tabs });
  fireEvent.click(screen.getByRole('button', { name: 'Open in new tab', exact: true }));
  await waitFor(() => expect(f.runtime.reportWorkspace).toHaveBeenCalledOnce());
  expect(tabs.workspace().tabs).toHaveLength(32); expect(route.navigate).not.toHaveBeenCalled();
});

it('loads the full title only on action and keeps rename alive after the clicked row unmounts', async () => {
  const f = fixture();
  expect(f.get).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Rename', exact: true }));
  expect((await screen.findByRole('textbox')).getAttribute('value')).toBe(f.metadata.title);
  f.rerender(false);
  fireEvent.change(screen.getByRole('textbox'), { target: { value: 'New title' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save', exact: true }));
  await waitFor(() => expect(f.update).toHaveBeenCalledExactlyOnceWith('actual-tree', '9007199254740993', { title: 'New title', archived: false, pinned: true }, { signal: expect.any(AbortSignal) }));
  expect(f.session).toHaveBeenCalledWith('same-root');
  expect(route.navigate).not.toHaveBeenCalled();
});
it('refuses a changed host after reading metadata', async () => {
  const f = fixture();
  let finish!: (value: typeof f.metadata) => void;
  f.get.mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
  fireEvent.click(screen.getByRole('button', { name: 'Rename', exact: true }));
  f.host.state = 'closed';
  await act(async () => finish(f.metadata));
  expect(screen.getByRole('alert').textContent).toContain('disconnected or changed');
  expect(screen.getByRole('button', { name: 'Save', exact: true })).toHaveProperty('disabled', true);
  expect(f.run).not.toHaveBeenCalled();
});
it('forks the fresh revision once and does not steal navigation on a late result', async () => {
  const f = fixture();
  let finish!: (value: { root: { id: string } }) => void;
  f.run.mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
  fireEvent.click(screen.getByRole('button', { name: 'Fork', exact: true }));
  await waitFor(() => expect(f.runtime.command).toHaveBeenCalledWith(f.host.client, 'sessions.fork', { fork_id: expect.any(String), session_id: 'same-root', expected_history_revision: '42', expected_config_revision: '19', observed_through: '9007199254740995', keep_through: '9007199254740995', title: f.metadata.title }));
  route.location = { pathname: '/moved-away' };
  f.rerender();
  await act(async () => finish({ root: { id: 'forked' } }));
  expect(route.navigate).not.toHaveBeenCalled();
  expect(f.runtime.tabs.open).not.toHaveBeenCalled();
});
it('deletes only after confirmation and clears the clicked root on its original host', async () => {
  const f = fixture();
  fireEvent.click(screen.getByRole('button', { name: 'Delete…', exact: true }));
  await screen.findByText(f.metadata.title);
  expect(screen.getByText('Host: Remote workstation')).toBeTruthy();
  expect(f.remove).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Delete session', exact: true }));
  await waitFor(() => expect(f.runtime.forgetSession).toHaveBeenCalledExactlyOnceWith('remote', 'same-root'));
  expect(f.remove).toHaveBeenCalledOnce();
  expect(route.navigate).not.toHaveBeenCalled();
});
it('deleting the current and only session lands in a New Chat on that host', async () => {
  const f = fixture();
  route.location = { pathname: '/h/remote/s/same-root' };
  route.navigate.mockResolvedValue(undefined);
  const openNew = vi.fn(() => ({ id: 'draft', kind: 'new', runtimeId: 'remote', cwd: '', permissionMode: 'prompt' }));
  Object.assign(f.runtime.tabs, { openNew }); Object.assign(f.host, { runtimeId: 'remote' });
  f.rerender();
  fireEvent.click(screen.getByRole('button', { name: 'Delete…', exact: true }));
  await screen.findByText(f.metadata.title);
  fireEvent.click(screen.getByRole('button', { name: 'Delete session', exact: true }));
  await waitFor(() => expect(f.runtime.forgetSession).toHaveBeenCalledOnce());
  expect(openNew).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ runtimeId: 'remote' }));
  expect(route.navigate).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ to: '/new/$draftId', params: { draftId: 'draft' }, replace: true }));
});
it('archive keeps tabs and drafts, with an Undo that restores the same root', async () => {
  const f = fixture();
  fireEvent.click(screen.getByRole('button', { name: 'Archive', exact: true }));
  await waitFor(() => expect(f.update).toHaveBeenCalledWith('actual-tree', '9007199254740993', { ...f.tree.metadata, archived: true }, { signal: expect.any(AbortSignal) }));
  expect(f.runtime.forgetSession).not.toHaveBeenCalled();
  expect(f.runtime.tabs.open).not.toHaveBeenCalled();
  fireEvent.click(await screen.findByRole('button', { name: 'Undo archive' }));
  await waitFor(() => expect(f.update).toHaveBeenLastCalledWith('actual-tree', '9007199254740993', { ...f.tree.metadata, archived: false }, { signal: expect.any(AbortSignal) }));
  expect(f.session.mock.calls.every(args => args[0] === 'same-root')).toBe(true);
});
it('hover archive button archives without a dialog and offers Undo, keeping any open tab', async () => {
  const f = fixture();
  route.location = { pathname: '/h/remote/s/same-root' };
  fireEvent.click(screen.getByRole('button', { name: 'Archive hover button', exact: true }));
  expect(f.list.refresh).not.toHaveBeenCalled();
  expect(await screen.findByText('Session archived')).toBeTruthy();
  await waitFor(() => expect(f.update).toHaveBeenCalledExactlyOnceWith('actual-tree', '9007199254740993', { ...f.tree.metadata, archived: true }, { signal: expect.any(AbortSignal) }));
  expect(screen.queryByText('Delete this session?')).toBeNull();
  expect(screen.queryByText('Could not archive session')).toBeNull();
  expect(route.navigate).not.toHaveBeenCalled();
  expect(f.runtime.forgetSession).not.toHaveBeenCalled();
  fireEvent.click(await screen.findByRole('button', { name: 'Undo archive' }));
  await waitFor(() => expect(f.update).toHaveBeenLastCalledWith('actual-tree', '9007199254740993', { ...f.tree.metadata, archived: false }, { signal: expect.any(AbortSignal) }));
  expect(f.list.refresh).toHaveBeenCalledTimes(2);
});
it('a failed archive retains authoritative catalog state', async () => {
  const f = fixture();
  f.update.mockRejectedValueOnce(new Error('Archive rejected'));
  fireEvent.click(screen.getByRole('button', { name: 'Archive hover button', exact: true }));
  expect(f.list.refresh).not.toHaveBeenCalled();
  await screen.findByRole('dialog', { name: 'Could not archive session' });
  expect(f.list.refresh).not.toHaveBeenCalled();
  expect(screen.queryByText('Session archived')).toBeNull();
});
it('browser Copy directory uses full metadata rather than the catalog abbreviation', async () => {
  const f = fixture();
  fireEvent.click(screen.getByRole('button', { name: 'Copy directory', exact: true }));
  await waitFor(() => expect(f.runtime.platform.copy).toHaveBeenCalledExactlyOnceWith('/full/directory'));
  expect(f.run).not.toHaveBeenCalled();
});

it('deleting a selected workspace tab from Settings leaves Settings open', async () => {
  const f = fixture();
  route.location = { pathname: '/settings' };
  const tab = { id: 'selected', runtimeId: 'remote', rootId: 'same-root' };
  Object.assign(f.workspace, { tabs: [tab], layout: { type: 'pane', id: 'main', tabs: [tab], selected: tab.id } });
  f.rerender();
  fireEvent.click(screen.getByRole('button', { name: 'Delete…', exact: true }));
  await screen.findByText(f.metadata.title);
  fireEvent.click(screen.getByRole('button', { name: 'Delete session', exact: true }));
  await waitFor(() => expect(f.runtime.forgetSession).toHaveBeenCalledOnce());
  expect(route.navigate).not.toHaveBeenCalled();
});

it('a local profile does not inherit an SSH alias saved for an earlier URL connection', async () => {
  const f = fixture();
  const open = vi.fn(async () => {});
  Object.assign(f.runtime.platform, { projectEditors: { list: async () => [{ id: 'vscode', label: 'VS Code', installed: true }], open }, storage: { getItem: () => JSON.stringify('earlier-remote-alias') } });
  f.host.profile.target.kind = 'local';
  f.rerender();
  fireEvent.click(screen.getByRole('button', { name: 'Refresh editors' }));
  fireEvent.click(await screen.findByRole('button', { name: 'VS Code', exact: true }));
  await waitFor(() => expect(open).toHaveBeenCalledExactlyOnceWith({ app: 'vscode', directory: '/full/directory', connectionId: 'remote-profile', runtimeId: 'remote', sshAlias: undefined }));
});

it('keeps a failed rename in its session dialog and clears it on successful retry', async () => {
  const f = fixture();
  f.update.mockRejectedValueOnce(new Error('Rename rejected'));
  fireEvent.click(screen.getByRole('button', { name: 'Rename', exact: true }));
  await screen.findByRole('textbox');
  fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Keep this name' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save', exact: true }));
  const alert = await screen.findByRole('alert');
  expect(alert.closest('[data-error-type]')?.getAttribute('data-error-type')).toBe('action');
  expect(alert.closest('[data-error-owner]')?.getAttribute('data-error-owner')).toBe('remote:same-root');
  expect((screen.getByRole('textbox') as HTMLInputElement).value).toBe('Keep this name');
  expect(f.runtime.report).not.toHaveBeenCalled();
  expect(screen.queryByText('Session action failed')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Save', exact: true }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
});
it('anchors a failed menu action to its named session after the menu closes', async () => {
  const f = fixture();
  f.update.mockRejectedValueOnce(new Error('Archive rejected'));
  fireEvent.click(screen.getByRole('button', { name: 'Archive', exact: true }));
  const dialog = await screen.findByRole('dialog', { name: 'Could not archive session' });
  expect(dialog.textContent).toContain('Truncated… · Remote workstation');
  expect(dialog.querySelector('[data-error-owner]')?.getAttribute('data-error-owner')).toBe('remote:same-root');
  expect(f.runtime.report).not.toHaveBeenCalled();
  expect(screen.getAllByRole('alert')).toHaveLength(1);
});


it('preserves a rename draft and its exact revision until current metadata is explicitly reloaded', async () => {
  const f = fixture();
  fireEvent.click(screen.getByRole('button', { name: 'Rename', exact: true }));
  fireEvent.change(await screen.findByRole('textbox'), { target: { value: 'Keep my authored name' } });
  f.tree.revision = '9007199254740995';
  f.update.mockRejectedValueOnce(new Error('Tree revision changed'));
  fireEvent.click(screen.getByRole('button', { name: 'Save', exact: true }));
  await screen.findByRole('alert');
  expect(f.update).toHaveBeenCalledTimes(1);
  expect(f.update.mock.calls[0]?.[1]).toBe('9007199254740993');
  expect((screen.getByRole('textbox') as HTMLInputElement).value).toBe('Keep my authored name');
  fireEvent.click(screen.getByRole('button', { name: 'Reload current metadata' }));
  await waitFor(() => expect(screen.queryByRole('alert')).toBeNull());
  expect((screen.getByRole('textbox') as HTMLInputElement).value).toBe('Keep my authored name');
  expect(f.update).toHaveBeenCalledTimes(1);
  fireEvent.click(screen.getByRole('button', { name: 'Save', exact: true }));
  await waitFor(() => expect(f.update).toHaveBeenCalledTimes(2));
  expect(f.update.mock.calls[1]?.[1]).toBe('9007199254740995');
});
it('rejects tree metadata returned for another owner before exposing a mutation', async () => {
  const f = fixture(); f.tree.id = 'unrelated-tree';
  fireEvent.click(screen.getByRole('button', { name: 'Rename', exact: true }));
  await screen.findByRole('alert');
  expect(screen.getByRole('button', { name: 'Save', exact: true })).toHaveProperty('disabled', true);
  expect(f.update).not.toHaveBeenCalled(); expect(f.run).not.toHaveBeenCalled();
});
