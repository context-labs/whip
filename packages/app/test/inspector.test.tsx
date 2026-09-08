import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { Session, type WhipClient } from '@whip/sdk';
import type { SessionView, SessionViewSnapshot } from '@whip/sdk/state';
import type { RootSnapshot } from '@whip/protocol';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';
import { Agents, Mailbox } from '../src/details/observation';
import { MCP } from '../src/details/integrations';
import {
  Compaction,
  ContextSettings,
  Goals,
  Permissions,
  Limits,
  formatBudgetAmount,
} from '../src/details/session-controls';
import { valueText, type InspectorProps } from '../src/details/shared';

vi.mock('@tanstack/react-router', () => ({
  Link: ({ children }: { children: ReactNode }) => <a href="#agent">{children}</a>,
}));

beforeEach(() => {
  vi.stubGlobal('matchMedia', () => ({
    matches: false,
    addEventListener() {},
    removeEventListener() {},
  }));
});
afterEach(() => vi.unstubAllGlobals());

function fixture() {
  const root = {
    root_id: 'root',
    cursor: '1',
    history_revision: '1',
    meta: {
      id: 'root',
      kind: 'agent',
      title: 'Test',
      goal: '',
      model: 'model',
      provider: 'provider',
      cwd: '/',
      effort: '',
      usage_in: 0,
      usage_out: 0,
      usage_cached: 0,
    },
    active_turns: {},
    agents: [],
    messages: [],
    message_seqs: [],
    inbox: [],
    blackboard: [],
    budgets: [],
    capabilities: [],
    schedules: [],
    permissions: [],
    questions: [],
    presentation: [],
    agent_presentations: {},
  } as unknown as RootSnapshot;
  let state = {
    status: 'live',
    root,
    history: {},
    collections: {},
    retainedBytes: 0,
    truncated: false,
    unavailable: false,
  } as SessionViewSnapshot;
  const listeners = new Set<() => void>();
  const client = {
    getSnapshot: () => ({
      state: 'connected',
      info: {
        runtime_id: 'runtime',
        host_platform: 'darwin',
        host_architecture: 'arm64',
        limits: {},
      },
    }),
    supports: vi.fn(() => true),
    query: vi.fn(async (operation: string) => ({
      result:
        operation === 'mcp.import.status'
          ? { claude: true, codex: false }
          : operation === 'permission.rules'
            ? { rules: [], global: [] }
            : [],
    })),
    submit: vi.fn((operation: string, payload: unknown) => ({ operation, payload })),
    invoke: vi.fn(async () => ({ result: {} })),
    call: vi.fn(async () => ({})),
    configuration: {
      get: vi.fn(async () => ({
        revision: '1',
        compact_model: 'original',
        compact_provider: 'provider',
      })),
      update: vi.fn(async () => ({})),
    },
  };
  const session = new Session(client as unknown as WhipClient, 'root');
  const view = {
    session,
    getSnapshot: () => state,
    subscribe: (listener: () => void) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    loadCollection: vi.fn(async () => {}),
  } as unknown as SessionView;
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  const run = vi.fn(async () => ({ status: 'succeeded', result: {} }));
  const runtime = {
    queries,
    run,
    report: vi.fn(),
    platform: { download: vi.fn() },
  } as unknown as AppRuntime;
  const props: InspectorProps = { view, root, connected: true, agentId: 'root' };
  return {
    client,
    root,
    view,
    queries,
    run,
    props,
    update(next: Partial<SessionViewSnapshot>) {
      state = { ...state, ...next };
      listeners.forEach((listener) => listener());
    },
    render(node: ReactNode) {
      return render(
        <RuntimeContext.Provider value={runtime}>
          <ThemeProvider initialTheme="light">
            <UIProvider>
              <QueryClientProvider client={queries}>{node}</QueryClientProvider>
            </UIProvider>
          </ThemeProvider>
        </RuntimeContext.Provider>,
      );
    },
  };
}
const agent = (id: string, controls: string[]) => ({
  id,
  root_id: 'root',
  parent_id: 'root',
  name: id,
  model: 'model',
  provider: 'provider',
  effort: '',
  cwd: '/',
  report: '',
  status: 'idle',
  pending_mail: 0,
  lifecycle_phase: 'idle',
  blocking_reason: '',
  terminal_cause: '',
  allowed_controls: controls,
});

describe('session inspector controls', () => {
  it('enables automatic titles through the existing one-way daemon action', () => {
    const f = fixture();
    f.render(<ContextSettings {...f.props} />);
    fireEvent.click(screen.getByRole('button', { name: 'Enable automatic titles' }));
    expect(f.client.submit).toHaveBeenCalledWith('session.autotitle', {}, { rootId: 'root' });
  });
  it('keeps retained agent controls disabled while disconnected', () => {
    const f = fixture();
    f.root.agents = [agent('child', ['agent.stop', 'agent.delete'])];
    f.root.active_turns = { child: 'turn' };
    f.render(<Agents {...f.props} connected={false} />);
    for (const name of ['Stop subtree', 'Delete agent', 'Cancel turn']) {
      const button = screen.getByRole('button', { name }) as HTMLButtonElement;
      expect(button.disabled).toBe(true);
      fireEvent.click(button);
    }
    expect(f.client.submit).not.toHaveBeenCalled();
  });
  it('keeps oversized collection entries explicit without fetching their content', () => {
    const f = fixture();
    f.update({
      collections: {
        agents: {
          root_id: 'root',
          collection: 'agents',
          revision: '1',
          event_cursor: '1',
          has_more: false,
          items: [
            {
              body: {
                reference_id: 'large-agent',
                digest: 'digest',
                size: '1048576',
                media_type: 'application/json',
              },
            },
          ],
        },
      },
    });
    f.render(<Agents {...f.props} />);
    expect(screen.getByRole('button', { name: 'Read large collection entry' })).toBeTruthy();
    expect(f.client.call).not.toHaveBeenCalled();
  });
  it('uses the configuration revision captured when compaction defaults were edited', async () => {
    const f = fixture();
    f.client.configuration.update.mockRejectedValueOnce(
      new Error('configuration revision changed'),
    );
    f.render(<Compaction {...f.props} />);
    await waitFor(() =>
      expect((screen.getByLabelText('Compaction model') as HTMLInputElement).value).toBe(
        'original',
      ),
    );
    fireEvent.change(screen.getByLabelText('Compaction model'), { target: { value: 'my-model' } });
    act(() =>
      f.queries.setQueryData(['inspector-compaction-config', 'runtime'], {
        revision: '2',
        compact_model: 'someone-elses-model',
        compact_provider: 'provider',
      }),
    );
    await screen.findByText(
      'Host configuration changed while you were editing. Refresh these fields before applying them.',
    );
    fireEvent.click(screen.getByRole('button', { name: 'Apply compaction defaults' }));
    await screen.findByText('configuration revision changed');
    expect(f.client.configuration.update).toHaveBeenCalledWith({
      revision: '1',
      compact_model: 'my-model',
      compact_provider: 'provider',
    });
    expect(f.run).not.toHaveBeenCalled();
    expect(f.client.configuration.get).toHaveBeenCalledTimes(1);
  });
  it('uses daemon-advertised agent controls and renders paged agents', async () => {
    const f = fixture();
    f.root.agents = [
      agent('running-child', ['agent.stop']),
      agent('finished-child', ['agent.delete']),
    ];
    f.root.omitted = { agents: true };
    f.render(<Agents {...f.props} />);
    fireEvent.click(screen.getByRole('button', { name: 'Stop subtree' }));
    expect(f.client.submit).toHaveBeenCalledWith(
      'agent.control',
      { id: 'running-child' },
      { rootId: 'root' },
    );
    fireEvent.click(screen.getByRole('button', { name: 'Delete agent' }));
    expect(f.client.submit).toHaveBeenCalledWith(
      'agent.delete',
      { id: 'finished-child' },
      { rootId: 'root' },
    );
    fireEvent.click(screen.getByRole('button', { name: 'Load more' }));
    await waitFor(() =>
      expect(f.view.loadCollection).toHaveBeenCalledWith('agents', { more: false }),
    );
    act(() =>
      f.update({
        collections: {
          agents: {
            root_id: 'root',
            collection: 'agents',
            revision: '1',
            event_cursor: '1',
            has_more: false,
            items: [{ agent: agent('paged-child', []) }],
          },
        },
      }),
    );
    expect(screen.getByText('paged-child')).toBeTruthy();
  });
  it('uses the exact active child turn for cancellation', () => {
    const f = fixture();
    f.root.agents = [agent('child', ['agent.stop'])];
    f.root.active_turns = { child: 'turn-before-click' };
    f.render(<Agents {...f.props} />);
    fireEvent.click(screen.getByRole('button', { name: 'Cancel turn' }));
    expect(f.client.submit).toHaveBeenCalledWith(
      'agent.turn.cancel',
      { id: 'child', turn_id: 'turn-before-click' },
      { rootId: 'root' },
    );
  });
  it('submits goals and schedules through durable SDK commands', async () => {
    const f = fixture();
    f.render(<Goals {...f.props} />);
    fireEvent.change(screen.getByLabelText('Goal'), { target: { value: 'Finish the audit' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save goal' }));
    expect(f.client.submit).toHaveBeenCalledWith(
      'goal.set',
      { text: 'Finish the audit' },
      { rootId: 'root' },
    );
    fireEvent.change(screen.getByLabelText('When'), { target: { value: 'every 30m' } });
    fireEvent.change(screen.getByLabelText('Scheduled prompt'), {
      target: { value: 'Check progress' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Create schedule' }));
    expect(f.client.submit).toHaveBeenCalledWith(
      'schedule.create',
      { schedule: 'every 30m', prompt: 'Check progress' },
      { rootId: 'root' },
    );
  });
  it('forgets only the selected permission rule', async () => {
    const f = fixture();
    f.client.query.mockImplementation(
      async () =>
        ({
          result: {
            rules: [
              {
                id: 'rule-1',
                operation: 'write',
                rule: '/project/**',
                principal_id: 'human',
                created_at: 'now',
              },
            ],
            global: [],
          },
        }) as never,
    );
    f.render(<Permissions {...f.props} />);
    fireEvent.click(await screen.findByRole('button', { name: 'Forget rule' }));
    expect(f.client.submit).toHaveBeenCalledWith(
      'permission.forget',
      { id: 'rule-1' },
      { rootId: 'root' },
    );
  });
  it('formats exact int64 usage and decodes binary presentation data without model admission', () => {
    expect(formatBudgetAmount('cost', '9007199254740993')).toBe('$9007199254.740993');
    expect(formatBudgetAmount('elapsed', '12345')).toBe('12.345 s');
    expect(valueText({ reference_id: '', digest: '', size: '5', binary: btoa('hello') })).toBe(
      'hello',
    );
  });
});

describe('read-only mailbox and ephemeral integration workflows', () => {
  it('reloads an inspected body when the same mailbox message receives a new revision', async () => {
    const f = fixture();
    let revision = '1';
    const item = () => ({
      id: 'message',
      subject: 'Report',
      kind: 'report',
      sender: 'child',
      recipient: 'root',
      revision,
      status: 'pending',
      excerpt: 'Excerpt',
    });
    vi.spyOn(f.view.session.mailbox, 'list').mockImplementation(
      async () => ({ revision, items: [item()], has_more: false }) as never,
    );
    const read = vi
      .spyOn(f.view.session.mailbox, 'read')
      .mockImplementation(
        async () =>
          ({
            ...item(),
            body: {
              text: `Body revision ${revision}`,
              reference_id: '',
              digest: '',
              size: '15',
              media_type: 'text/plain',
            },
          }) as never,
      );
    f.render(<Mailbox {...f.props} />);
    fireEvent.click(await screen.findByRole('button', { name: 'Inspect message' }));
    await screen.findByText('Body revision 1');
    revision = '2';
    await act(() =>
      f.queries.invalidateQueries({
        queryKey: ['inspector-mail', 'runtime', 'root', 'root', 'all'],
        exact: true,
      }),
    );
    await screen.findByText('Body revision 2');
    expect(screen.queryByText('Body revision 1')).toBeNull();
    expect(read).toHaveBeenCalledTimes(2);
    expect(f.client.submit).not.toHaveBeenCalled();
  });
  it('pages the mailbox and reads bodies only after selection', async () => {
    const f = fixture();
    const cursor = { root_id: 'root', agent_id: 'root', status: 'all', revision: '2', offset: '1' };
    const item = (id: string) => ({
      id,
      subject: id,
      kind: 'report',
      sender: 'child',
      recipient: 'root',
      revision: '2',
      status: 'pending',
      excerpt: 'Excerpt',
    });
    const list = vi.spyOn(f.view.session.mailbox, 'list').mockImplementation(
      async (params) =>
        ({
          revision: '2',
          items: [item(params?.cursor ? 'second-message' : 'first-message')],
          has_more: !params?.cursor,
          next_cursor: params?.cursor ? undefined : cursor,
        }) as never,
    );
    const read = vi.spyOn(f.view.session.mailbox, 'read').mockResolvedValue({
      ...item('second-message'),
      body: {
        text: 'Private body',
        reference_id: '',
        digest: '',
        size: '12',
        media_type: 'text/plain',
        source: 'mail',
      },
    } as never);
    f.render(<Mailbox {...f.props} />);
    await screen.findByText('first-message');
    expect(read).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Older messages' }));
    await screen.findByText('second-message');
    expect(list).toHaveBeenLastCalledWith(
      expect.objectContaining({ cursor, limit: 32, max_bytes: 131072 }),
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
    fireEvent.click(screen.getAllByRole('button', { name: 'Inspect message' })[1]!);
    await screen.findByText('Private body');
    expect(read).toHaveBeenCalledWith('second-message', 'root', expect.anything());
    expect(f.client.submit).not.toHaveBeenCalled();
  });
  it('restarts mailbox paging after a revision conflict', async () => {
    const f = fixture();
    const list = vi.spyOn(f.view.session.mailbox, 'list').mockImplementation(async (params) => {
      if (params?.cursor)
        throw Object.assign(new Error('expired'), { kind: 'resynchronization_required' });
      return {
        revision: '2',
        items: [],
        has_more: true,
        next_cursor: {
          root_id: 'root',
          agent_id: 'root',
          status: 'all',
          revision: '2',
          offset: '1',
        },
      } as never;
    });
    f.render(<Mailbox {...f.props} />);
    fireEvent.click(await screen.findByRole('button', { name: 'Older messages' }));
    await screen.findByText('Mailbox changed. Refreshed from the current first page.');
    await waitFor(() => expect(list).toHaveBeenCalledTimes(3));
    expect(list.mock.calls[2]?.[0]?.cursor).toBeUndefined();
  });
  it('sends private MCP configuration once, clears the editor, and keeps it out of query caches', async () => {
    const f = fixture();
    f.render(<MCP {...f.props} />);
    const configuration = {
      private: {
        url: 'https://host.example/mcp',
        headers: { Authorization: 'Bearer super-secret' },
      },
    };
    fireEvent.change(screen.getByLabelText('Private MCP server JSON'), {
      target: { value: JSON.stringify(configuration) },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Attach to this session' }));
    await waitFor(() =>
      expect(f.client.invoke).toHaveBeenCalledWith(
        'mcp.attach',
        { servers: configuration },
        expect.objectContaining({ rootId: 'root', signal: expect.any(AbortSignal) }),
      ),
    );
    expect((screen.getByLabelText('Private MCP server JSON') as HTMLTextAreaElement).value).toBe(
      '',
    );
    expect(
      JSON.stringify(
        f.queries
          .getQueryCache()
          .getAll()
          .map((query) => ({ key: query.queryKey, data: query.state.data })),
      ),
    ).not.toContain('super-secret');
    expect(f.client.submit).not.toHaveBeenCalled();
  });
});


describe('read-only budget usage', () => {
  it('shows unlimited and uncertain usage without offering budget controls', () => {
    const f = fixture();
    f.root.budgets = [{ agent_id: '', state: { kind: 'cost', limit: null, remaining: null, used: '1142228', reserved: '0', uncertain: '23883863', incomplete: true } }];
    f.render(<Limits {...f.props} />);
    expect(screen.getByText('Unlimited')).toBeTruthy();
    expect(screen.getByText('$1.142228 used · $0.000000 in flight')).toBeTruthy();
    expect(screen.getByText('Usage is incomplete · estimated $23.883863 unconfirmed.')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Set cap' })).toBeNull();
    expect(screen.queryByLabelText('Budget agent')).toBeNull();
    expect(screen.queryByLabelText('Limit')).toBeNull();
    expect(f.client.submit).not.toHaveBeenCalled();
  });
});
