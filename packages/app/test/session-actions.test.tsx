import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, expect, it, vi } from 'vitest';
import { UIProvider } from '@whip/ui';
import { RuntimeContext } from '../src/context';
import { SessionActionsProvider, useSessionActions } from '../src/session-actions';
import type { AppRuntime } from '../src/runtime';

const route = vi.hoisted(() => ({ location: { pathname: '/other' }, navigate: vi.fn() }));
vi.mock('@tanstack/react-router', () => ({ useLocation: () => route.location, useNavigate: () => route.navigate }));
beforeEach(() => { route.location = { pathname: '/other' }; route.navigate.mockClear(); });
function Rows({ visible = true }: { visible?: boolean }) {
  const actions = useSessionActions();
  return visible ? <><button onClick={() => actions.prepare(true)}>Refresh editors</button>{actions.items({ runtimeId: 'remote', rootId: 'same-root', title: 'Truncated…' }).flatMap(item => item.items ?? [item]).map(item => <button key={item.id} disabled={item.disabled} onClick={item.onSelect}>{item.label}</button>)}</> : null;
}
function fixture() {
  const metadata = { root_id: 'same-root', title: 'The full title behind the truncated catalog', cwd: '/full/directory', archived: false, history_revision: '42' };
  const get = vi.fn(async () => metadata);
  const rename = vi.fn(() => 'rename-command'), fork = vi.fn(() => 'fork-command'), archive = vi.fn(() => 'archive-command'), remove = vi.fn(() => 'delete-command');
  const session = vi.fn(() => ({ rename, fork, archive, delete: remove }));
  const client = { sessions: { get }, session };
  const host = { id: 'remote-profile', name: 'Remote workstation', client, state: 'connected', profile: { target: { kind: 'url' } } };
  const snapshot = { hosts: [host] };
  const run = vi.fn(async () => ({ result: { root_id: 'forked' } }));
  const workspace = { tabs: [], layout: { type: 'pane', id: 'main', tabs: [] }, focusedPaneId: 'main', closed: [] };
  const runtime = { getSnapshot: () => snapshot, subscribe: () => () => {}, connections: { host: vi.fn((id: string) => id === 'remote' ? host : undefined) },
    platform: { copy: vi.fn(async () => {}), storage: { getItem: () => null } },
    tabs: { workspace: () => workspace, open: vi.fn(() => 'fork-tab'), titles: vi.fn() },
    forgetSession: vi.fn(), run, report: vi.fn() } as unknown as AppRuntime;
  const tree = (visible = true) => <RuntimeContext.Provider value={runtime}><UIProvider><SessionActionsProvider><Rows visible={visible} /></SessionActionsProvider></UIProvider></RuntimeContext.Provider>;
  const view = render(tree());
  return { get, metadata, rename, fork, archive, remove, session, host, runtime, run, workspace, rerender: (visible = true) => view.rerender(tree(visible)) };
}
it('loads the full title only on action and keeps rename alive after the clicked row unmounts', async () => {
  const f = fixture();
  expect(f.get).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Rename', exact: true }));
  expect((await screen.findByRole('textbox')).getAttribute('value')).toBe(f.metadata.title);
  f.rerender(false);
  fireEvent.change(screen.getByRole('textbox'), { target: { value: 'New title' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save', exact: true }));
  await waitFor(() => expect(f.rename).toHaveBeenCalledExactlyOnceWith('New title'));
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
  let finish!: (value: { result: { root_id: string } }) => void;
  f.run.mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
  fireEvent.click(screen.getByRole('button', { name: 'Fork', exact: true }));
  await waitFor(() => expect(f.fork).toHaveBeenCalledExactlyOnceWith({ expected_revision: '42' }));
  route.location = { pathname: '/moved-away' };
  f.rerender();
  await act(async () => finish({ result: { root_id: 'forked' } }));
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
it('archive keeps tabs and drafts, with an Undo that restores the same root', async () => {
  const f = fixture();
  fireEvent.click(screen.getByRole('button', { name: 'Archive', exact: true }));
  await waitFor(() => expect(f.archive).toHaveBeenCalledWith(true));
  expect(f.runtime.forgetSession).not.toHaveBeenCalled();
  expect(f.runtime.tabs.open).not.toHaveBeenCalled();
  fireEvent.click(await screen.findByRole('button', { name: 'Undo archive' }));
  await waitFor(() => expect(f.archive).toHaveBeenLastCalledWith(false));
  expect(f.session.mock.calls.every(args => args[0] === 'same-root')).toBe(true);
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
  f.run.mockRejectedValueOnce(new Error('Rename rejected'));
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
  f.run.mockRejectedValueOnce(new Error('Archive rejected'));
  fireEvent.click(screen.getByRole('button', { name: 'Archive', exact: true }));
  const dialog = await screen.findByRole('dialog', { name: 'Could not archive session' });
  expect(dialog.textContent).toContain('Truncated… · Remote workstation');
  expect(dialog.querySelector('[data-error-owner]')?.getAttribute('data-error-owner')).toBe('remote:same-root');
  expect(f.runtime.report).not.toHaveBeenCalled();
  expect(screen.getAllByRole('alert')).toHaveLength(1);
});
