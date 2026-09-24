import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { beforeEach, afterEach, expect, it, vi } from 'vitest';
import type { RootSnapshot } from '@whip/protocol';
import type { SessionView, SessionViewSnapshot } from '@whip/sdk/state';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { AgentTurnNotice, useSelectedAgent } from '../src/agent-turn-notice';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';

beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());
const agent: NonNullable<RootSnapshot['agents']>[number] = {
  id: 'child', root_id: 'root', parent_id: 'root', name: 'Architecture researcher', model: 'model', provider: 'provider', cwd: '/', effort: '', report: 'notice', status: 'idle',
  pending_mail: 0, lifecycle_phase: 'idle', blocking_reason: '', terminal_cause: '', allowed_controls: [],
  last_turn: { status: 'failed', turn_id: 'turn', event_seq: '14', error: '400 Bad Request: Invalid prompt_cache_key' },
};

it('shows the named agent and recorded error, copies it, and clears for a new turn', async () => {
  const copy = vi.fn(async () => {});
  const runtime = { platform: { copy }, report: vi.fn() } as unknown as AppRuntime;
  const view = { session: { rootId: 'root' } } as SessionView;
  const app = (activeTurn?: string, selected = agent) => <RuntimeContext.Provider value={runtime}><UIProvider><ThemeProvider initialTheme="claude-code">
    <AgentTurnNotice view={view} agent={selected} activeTurn={activeTurn} />
  </ThemeProvider></UIProvider></RuntimeContext.Provider>;
  const rendered = render(app());
  expect(screen.getByRole('alert').textContent).toContain('Architecture researcher · Last turn failed');
  fireEvent.click(screen.getByRole('button', { name: 'Error details' }));
  expect(screen.getByRole('alert').textContent).toContain('Invalid prompt_cache_key');
  fireEvent.click(screen.getByRole('button', { name: 'Copy error' }));
  await waitFor(() => expect(copy).toHaveBeenCalledWith(agent.last_turn!.error));
  rendered.rerender(app('next-turn'));
  expect(screen.queryByRole('alert')).toBeNull();
  rendered.rerender(app(undefined, { ...agent, last_turn: { status: 'succeeded', event_seq: '16' } }));
  expect(screen.queryByRole('alert')).toBeNull();
  rendered.rerender(app(undefined, { ...agent, last_turn: { status: 'interrupted', event_seq: '17', error: 'Daemon restarted' } }));
  expect(screen.getByRole('status').textContent).toContain('Last turn interrupted');
  expect(screen.queryByRole('alert')).toBeNull();
});

it('loads a selected agent outside the snapshot page and refreshes only with lifecycle snapshots', async () => {
  const inspect = vi.fn(async () => ({ result: { agent } }));
  const view = { session: { rootId: 'root', client: { getSnapshot: () => ({ info: { runtime_id: 'host' } }) }, agents: { inspect } } } as unknown as SessionView;
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const state = { root: { agents: [], cursor: '10' }, collections: {} } as unknown as SessionViewSnapshot;
  function Selected({ state }: { state: SessionViewSnapshot }) {
    const selected = useSelectedAgent(view, state, 'child', true);
    return <span>{selected?.name}</span>;
  }
  const app = (state: SessionViewSnapshot) => <QueryClientProvider client={queryClient}><Selected state={state} /></QueryClientProvider>;
  const rendered = render(app(state));
  await screen.findByText('Architecture researcher');
  expect(inspect).toHaveBeenCalledTimes(1);
  rendered.rerender(app({ ...state, root: { ...state.root!, cursor: '11' } }));
  expect(inspect).toHaveBeenCalledTimes(1);
  rendered.rerender(app({ ...state, root: { ...state.root!, agents: [], cursor: '12' } }));
  await waitFor(() => expect(inspect).toHaveBeenCalledTimes(2));
  queryClient.clear();
});
