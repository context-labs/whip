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
}));
// Keep the real action menu and dialogs; inspector and transcript rendering are
// unrelated to the ownership of a pending history operation.
vi.mock('@whip/ui', async importOriginal => ({
  ...await importOriginal<typeof import('@whip/ui')>(),
  Sheet: ({ children }: { children: ReactNode }) => <section>{children}</section>,
}));
vi.mock('../src/timeline', async importOriginal => ({
  ...await importOriginal<typeof import('../src/timeline')>(),
  Timeline: ({ historyAction }: ComponentProps<typeof import('../src/timeline').Timeline>) => <>
    {(['rewind', 'fork'] as const).map(action => <button key={action} onClick={() => historyAction?.({ id: 'h:1', seq: 1, role: 'user', text: 'Hello' }, action)}>{action} message</button>)}
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

function fixture() {
  const values = new Map<string, string>();
  const storage = { keys: () => [...values.keys()], getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => { values.set(key, value); }, removeItem: (key: string) => { values.delete(key); } };
  const runtime = new AppRuntime({ defaultEndpoint: 'http://127.0.0.1:8080', storage, copy: async () => {}, openExternal: async () => {}, download: async () => {} });
  runtimes.push(runtime);
  vi.spyOn(runtime.connections, 'isAttached').mockReturnValue(true);
  const connection = { state: 'connected', info: { runtime_id: 'host' } };
  const snapshot = {
    status: 'live',
    root: { root_id: 'root', meta: {}, history_revision: '1', active_turns: {}, presentation: [], inbox: [] },
    history: { root: { revision: '1', throughSeq: 1, nextSeq: 1, hasMore: false, loading: false, truncated: false, messages: [{ seq: 1, message: { role: 'user', content: 'Hello' } }] } },
  };
  const view = {
    subscribe: () => () => {}, getSnapshot: () => snapshot,
    session: { rootId: 'root', client: { subscribe: () => () => {}, getSnapshot: () => connection }, history: { clear: vi.fn(), rewind: vi.fn() }, fork: vi.fn() },
  } as unknown as SessionView;
  const run = vi.spyOn(runtime, 'run');
  render(<RuntimeContext.Provider value={runtime}><UIProvider><SessionContent kind="chat" view={view} expectedRuntimeId="host" agentId="root" panel="agents" /></UIProvider></RuntimeContext.Provider>);
  return { run };
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
