import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import type { ComponentProps, ReactNode } from 'react';
import { afterEach, expect, it, vi } from 'vitest';
import { UIProvider } from '@whip/ui';
import type { SessionView } from '@whip/sdk/state';
import { AppRuntime } from '../src/runtime';
import { RuntimeContext } from '../src/context';
import { SessionContent } from '../src/conversation';

vi.mock('@tanstack/react-router', async importOriginal => ({
  ...await importOriginal<typeof import('@tanstack/react-router')>(),
  useNavigate: () => vi.fn(async () => {}),
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
}));
// Keep the real action menu and dialogs; inspector and transcript rendering are
// unrelated to the ownership of a pending history operation.
vi.mock('@whip/ui', async importOriginal => ({
  ...await importOriginal<typeof import('@whip/ui')>(),
  Sheet: ({ children }: { children: ReactNode }) => <section>{children}</section>,
}));
vi.mock('../src/timeline', async importOriginal => ({
  ...await importOriginal<typeof import('../src/timeline')>(),
  Timeline: ({ historyAction, responseHistoryAction }: ComponentProps<typeof import('../src/timeline').Timeline>) => <>
    {(['rewind', 'fork'] as const).map(action => <button key={action} onClick={() => historyAction?.({ id: 'h:1', seq: 1, role: 'user', text: 'Hello' }, action)}>{action} message</button>)}
    {(['rewind', 'fork'] as const).map(action => <button key={action + 'response'} disabled={!responseHistoryAction} onClick={() => responseHistoryAction?.(6, action)}>{action} response</button>)}
  </>,
}));
vi.mock('../src/composer', () => ({ Composer: () => null }));
vi.mock('../src/chat-activity', async importOriginal => ({
  ...await importOriginal<typeof import('../src/chat-activity')>(), ChatActivity: () => null, CurrentActivity: () => null,
}));
vi.mock('../src/requests', () => ({ PendingRequests: () => null }));
vi.mock('../src/inspector', () => ({ SessionInspector: () => null }));
vi.mock('../src/agent-turn-notice', () => ({ AgentTurnNotice: () => null, useSelectedAgent: () => undefined }));
vi.mock('../src/session-actions', () => ({ useSessionActions: () => ({ items: () => [], prepare: () => {} }) }));

const runtimes: AppRuntime[] = [];
afterEach(() => { for (const runtime of runtimes.splice(0)) runtime.dispose(); });

function fixture(agentId = 'root') {
  const values = new Map<string, string>();
  const storage = { keys: () => [...values.keys()], getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => { values.set(key, value); }, removeItem: (key: string) => { values.delete(key); } };
  const runtime = new AppRuntime({ defaultEndpoint: 'http://127.0.0.1:8080', storage, copy: async () => {}, openExternal: async () => {}, download: async () => {} });
  runtimes.push(runtime);
  vi.spyOn(runtime.connections, 'isAttached').mockReturnValue(true);
  const connection = { state: 'connected', info: { runtime_id: 'host' } };
  let snapshot = {
    status: 'live',
    collections: {},
    root: { root_id: 'root', meta: {}, history_revision: '1', active_turns: {} as Record<string, string>, presentation: [], agent_presentations: {}, inbox: [] },
    history: { [agentId]: { revision: '1', throughSeq: 8, nextSeq: 1, hasMore: false, loading: false, truncated: false, messages: [{ seq: 1, message: { role: 'user', content: 'Hello' } }] } },
  };
  const view = {
    subscribe: () => () => {}, getSnapshot: () => snapshot, openAgent: vi.fn(async () => {}), closeAgent: vi.fn(),
    session: { rootId: 'root', client: { subscribe: () => () => {}, getSnapshot: () => connection, supports: () => false }, history: { clear: vi.fn(), rewind: vi.fn() }, fork: vi.fn() },
  } as unknown as SessionView;
  const run = vi.spyOn(runtime, 'run');
  const app = () => <RuntimeContext.Provider value={runtime}><UIProvider><SessionContent kind="chat" view={view} expectedRuntimeId="host" agentId={agentId} panel="agents" /></UIProvider></RuntimeContext.Provider>;
  const mounted = render(app());
  return { run, view, runtime, connection, snapshot, update: () => { snapshot = { ...snapshot }; mounted.rerender(app()); } };
}

it.each(['rewind', 'fork'] as const)('does not leak a closed clear-history failure into a new %s dialog', async action => {
  const { run } = fixture();
  let rejectClear!: (error: Error) => void;
  run.mockReturnValueOnce(new Promise((_resolve, reject) => { rejectClear = reject; }));
  fireEvent.click(screen.getAllByRole('button', { name: 'Session actions' }).at(-1)!);
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Clear history…' }));
  const clearDialog = await screen.findByRole('dialog', { name: 'Clear conversation history?' });
  fireEvent.click(within(clearDialog).getByRole('button', { name: 'Confirm clear' }));
  fireEvent.click(within(clearDialog).getByRole('button', { name: 'Close' }));
  fireEvent.click(screen.getByRole('button', { name: `${action} message` }));
  const dialog = await screen.findByRole('dialog', { name: action === 'rewind' ? 'Rewind before this message?' : 'Fork before this message?' });
  await act(async () => rejectClear(new Error('Old clear failure')));
  expect(within(dialog).queryByRole('alert')).toBeNull();
  expect(screen.queryByText('Old clear failure')).toBeNull();

  // A failure of the currently open action must still be displayed.
  run.mockRejectedValueOnce(new Error('Current history changed'));
  fireEvent.click(within(dialog).getByRole('button', { name: `Confirm ${action}` }));
  await waitFor(() => expect(within(dialog).getByRole('alert')).toBeTruthy());
  expect(dialog.querySelector('[data-error-type="action"]')?.textContent).toContain('Current history changed');
});

it.each(['fork', 'rewind'] as const)('keeps the selected response with the correct %s command cut', async action => {
  const f = fixture();
  f.run.mockResolvedValue({ status: 'completed' } as never);
  fireEvent.click(screen.getByRole('button', { name: action + ' response' }));
  const dialog = screen.getByRole('dialog', { name: action === 'fork' ? 'Fork from here?' : 'Rewind to here?' });
  expect(dialog.textContent).toContain(action === 'rewind' ? 'Keep this response' : 'including this response');
  fireEvent.click(within(dialog).getByRole('button', { name: 'Confirm ' + action }));
  await waitFor(() => expect(f.run).toHaveBeenCalledTimes(1));
  if (action === 'fork') expect(f.view.session.fork).toHaveBeenCalledWith({ cut: 6, expected_revision: '1' });
  else expect(f.view.session.history.rewind).toHaveBeenCalledWith(7, '1');
});

it.each(['fork', 'rewind'] as const)('does not change the existing user %s cut', async action => {
  const f = fixture();
  f.run.mockResolvedValue({ status: 'completed' } as never);
  fireEvent.click(screen.getByRole('button', { name: action + ' message' }));
  fireEvent.click(screen.getByRole('button', { name: 'Confirm ' + action }));
  await waitFor(() => expect(f.run).toHaveBeenCalledTimes(1));
  if (action === 'fork') expect(f.view.session.fork).toHaveBeenCalledWith({ cut: 1, expected_revision: '1' });
  else expect(f.view.session.history.rewind).toHaveBeenCalledWith(1, '1');
});

it.each(['fork', 'rewind'] as const)('rejects stale response %s confirmation locally without replacing the selected revision', async action => {
  const f = fixture();
  fireEvent.click(screen.getByRole('button', { name: action + ' response' }));
  f.snapshot.root.history_revision = '2';
  f.snapshot.history.root.revision = '2';
  f.update();
  fireEvent.click(screen.getByRole('button', { name: 'Confirm ' + action }));
  expect(screen.getByRole('dialog').textContent).toContain('History has changed');
  expect(f.run).not.toHaveBeenCalled();
});

it('blocks response rewind when the root starts working or there is no later history', () => {
  const f = fixture();
  f.snapshot.root.active_turns.root = 'running'; f.update();
  fireEvent.click(screen.getByRole('button', { name: 'rewind response' }));
  expect(screen.queryByRole('dialog')).toBeNull();
  delete f.snapshot.root.active_turns.root; f.snapshot.history.root.throughSeq = 6; f.update();
  fireEvent.click(screen.getByRole('button', { name: 'rewind response' }));
  expect(screen.queryByRole('dialog')).toBeNull();
  f.snapshot.history.root.throughSeq = 8; f.update();
  fireEvent.click(screen.getByRole('button', { name: 'rewind response' }));
  f.snapshot.root.active_turns.root = 'running'; f.update();
  expect((screen.getByRole('button', { name: 'Confirm rewind' }) as HTMLButtonElement).disabled).toBe(true);
  expect(f.run).not.toHaveBeenCalled();
});

it('disables response actions in child transcripts', () => {
  const child = fixture('child');
  expect((screen.getByRole('button', { name: 'fork response' }) as HTMLButtonElement).disabled).toBe(true);
  expect(child.run).not.toHaveBeenCalled();
});

it.each(['disconnected', 'wrong-runtime'] as const)('cannot submit a response action after %s', mode => {
  const f = fixture();
  fireEvent.click(screen.getByRole('button', { name: 'fork response' }));
  if (mode === 'disconnected') f.connection.state = 'disconnected';
  else f.connection.info.runtime_id = 'another-host';
  f.update();
  if (mode === 'wrong-runtime') expect(screen.queryByRole('button', { name: 'Confirm fork' })).toBeNull();
  else {
    expect((screen.getByRole('button', { name: 'Confirm fork' }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole('button', { name: 'Confirm fork' }));
  }
  expect(f.run).not.toHaveBeenCalled();
});

it('guards fork tab limits', async () => {
  const f = fixture();
  for (let i = 0; i < 32; i++) f.runtime.tabs.open('host', 'root-' + i);
  fireEvent.click(screen.getByRole('button', { name: 'fork response' }));
  fireEvent.click(screen.getByRole('button', { name: 'Confirm fork' }));
  expect(screen.getByRole('dialog').textContent).toContain('32 open session tabs');
  expect(f.run).not.toHaveBeenCalled();
});

it('does not submit a response history command twice while confirmation is pending', async () => {
  const f = fixture();
  let resolve!: (outcome: never) => void;
  f.run.mockReturnValueOnce(new Promise(done => { resolve = done; }));
  fireEvent.click(screen.getByRole('button', { name: 'rewind response' }));
  const confirm = screen.getByRole('button', { name: 'Confirm rewind' });
  fireEvent.click(confirm); fireEvent.click(confirm);
  expect(f.view.session.history.rewind).toHaveBeenCalledTimes(1);
  expect((confirm as HTMLButtonElement).disabled).toBe(true);
  await act(async () => resolve({ status: 'completed' } as never));
});
