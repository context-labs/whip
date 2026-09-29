import {
  act,
  fireEvent,
  screen,
  waitFor,
  within,
} from '@testing-library/react';
import type { ComponentProps, ReactNode } from 'react';
import { beforeEach, expect, it, vi } from 'vitest';
import { SessionContent } from '../src/conversation';
import { conversationFixture } from './native-conversation-fixture';

const navigate = vi.hoisted(() => vi.fn(async () => {}));
vi.mock('@tanstack/react-router', async (original) => ({
  ...(await original<typeof import('@tanstack/react-router')>()),
  useNavigate: () => navigate,
}));
vi.mock('@whip/ui', async (original) => ({
  ...(await original<typeof import('@whip/ui')>()),
  Sheet: ({ children }: { children: ReactNode }) => (
    <section>{children}</section>
  ),
}));
vi.mock('../src/timeline', async (original) => ({
  ...(await original<typeof import('../src/timeline')>()),
  Timeline: ({
    historyAction,
    rows,
  }: ComponentProps<typeof import('../src/timeline').Timeline>) => (
    <>
      {(['rewind', 'fork'] as const).map((action) => (
        <button
          key={action}
          disabled={!historyAction}
          onClick={() => historyAction?.(rows[0]!, action)}
        >
          {action} exchange
        </button>
      ))}
    </>
  ),
}));
vi.mock('../src/composer', () => ({ Composer: () => null }));
vi.mock('../src/requests', () => ({ PendingRequests: () => null }));
vi.mock('../src/inspector', () => ({ SessionInspector: () => null }));
vi.mock('../src/agent-turn-notice', async (original) => ({
  ...(await original<typeof import('../src/agent-turn-notice')>()),
  AgentTurnNotice: () => null,
}));
vi.mock('../src/session-actions', () => ({
  useSessionActions: () => ({ items: () => [], prepare: () => {} }),
}));
beforeEach(() =>
  vi.stubGlobal('matchMedia', () => ({
    matches: false,
    addEventListener() {},
    removeEventListener() {},
  })),
);
async function fixture(owner = 'root') {
  const f = await conversationFixture(owner),
    { client, session, view, execution, runtime } = f;
  runtime.tabs.visit('host', 'root', owner === 'root' ? {} : { agent: owner });
  const app = () => (
    <SessionContent
      client={client}
      session={session}
      rootId="root"
      kind="chat"
      view={view}
      execution={execution}
      expectedRuntimeId="host"
      agentId={owner}
      panel="agents"
    />
  );
  const rendered = f.mount(app());
  await waitFor(() =>
    expect(
      screen.getByRole('button', { name: 'rewind exchange' }),
    ).not.toHaveProperty('disabled', true),
  );
  return { ...f, app, ...rendered, run: vi.spyOn(runtime, 'run') };
}
it.each(['rewind', 'fork'] as const)(
  'does not leak a closed clear-history failure into a new %s dialog',
  async (action) => {
    const f = await fixture();
    let rejectClear!: (error: Error) => void;
    f.run.mockReturnValueOnce(
      new Promise((_resolve, reject) => {
        rejectClear = reject;
      }),
    );
    fireEvent.click(
      screen.getAllByRole('button', { name: 'Session actions' }).at(-1)!,
    );
    fireEvent.click(
      await screen.findByRole('menuitem', { name: 'Clear history…' }),
    );
    const clear = await screen.findByRole('dialog', {
      name: 'Clear conversation history?',
    });
    fireEvent.click(
      within(clear).getByRole('button', { name: 'Confirm clear' }),
    );
    fireEvent.click(within(clear).getByRole('button', { name: 'Close' }));
    fireEvent.click(screen.getByRole('button', { name: `${action} exchange` }));
    const dialog = await screen.findByRole('dialog', {
      name: `${action === 'fork' ? 'Fork' : 'Rewind'} before this exchange?`,
    });
    await act(async () => rejectClear(new Error('Old clear failure')));
    expect(within(dialog).queryByRole('alert')).toBeNull();
    expect(screen.queryByText('Old clear failure')).toBeNull();
    f.run.mockRejectedValueOnce(new Error('Current history changed'));
    fireEvent.click(
      within(dialog).getByRole('button', { name: `Confirm ${action}` }),
    );
    await waitFor(() =>
      expect(within(dialog).getByRole('alert').textContent).toContain(
        'Current history changed',
      ),
    );
  },
);
it.each(['rewind', 'fork'] as const)(
  'captures exact %s scope, revision and request identity before confirmation',
  async (action) => {
    const f = await fixture('child');
    f.run.mockRejectedValueOnce(new Error('Lost acknowledgement'));
    fireEvent.click(screen.getByRole('button', { name: `${action} exchange` }));
    const dialog = await screen.findByRole('dialog', {
      name: `${action === 'fork' ? 'Fork' : 'Rewind'} before this exchange?`,
    });
    f.observed.revision = '9007199254740994';
    await act(async () => {
      await f.view.latest();
    });
    fireEvent.click(
      within(dialog).getByRole('button', { name: `Confirm ${action}` }),
    );
    await waitFor(() => expect(f.run).toHaveBeenCalledOnce());
    const command = f.run.mock.calls[0]![0];
    expect(command.method).toBe(`sessions.${action}`);
    expect(command.params).toMatchObject({
      session_id: 'child',
      observed_through: '9007199254740993',
      keep_through: '0',
      [action === 'fork' ? 'expected_history_revision' : 'expected_revision']:
        '9007199254740993',
    });
    if (action === 'fork')
      expect(command.params).toMatchObject({
        expected_config_revision: '9007199254740993',
        title: null,
      });
    expect(command.id).toBeTruthy();
    expect(
      within(dialog).getByRole('button', { name: `Confirm ${action}` }),
    ).toHaveProperty('disabled', true);
    expect(f.count('sessions.submit')).toBe(0);
  },
);
it('a changed host cannot submit an already-open history confirmation', async () => {
  const f = await fixture();
  fireEvent.click(screen.getByRole('button', { name: 'rewind exchange' }));
  const dialog = await screen.findByRole('dialog', {
    name: 'Rewind before this exchange?',
  });
  f.state.hosts = [{ ...f.host, state: 'stale', client: undefined }];
  f.rerender(f.wrap(f.app()));
  expect(
    within(dialog).getByRole('button', { name: 'Confirm rewind' }),
  ).toHaveProperty('disabled', true);
  fireEvent.click(
    within(dialog).getByRole('button', { name: 'Confirm rewind' }),
  );
  expect(f.run).not.toHaveBeenCalled();
});
