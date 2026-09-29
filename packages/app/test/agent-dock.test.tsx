import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import {
  AgentDock,
  AgentDockRoster,
  projectAgent,
  type DockAgent,
} from '../src/agent-dock';
import { activity, at, turn, parameters } from './native-conversation-fixture';
import { providerFixture, sessionRecord } from './provider-fixture';

const child = (
  id: string,
  state: 'running' | 'succeeded' | 'failed' | 'queued' | 'idle' = 'running',
): DockAgent => {
  const agent = sessionRecord(id);
  agent.definition.id = id;
  const last =
    state === 'idle'
      ? undefined
      : turn('turn-' + id, state === 'queued' ? 'failed' : state, id);
  return {
    agent,
    activity: {
      ...activity(id, state === 'running' ? last! : null),
      queued_input_count: state === 'queued' ? '1' : '0',
    },
    turn: last,
  };
};
const props = (agents: readonly DockAgent[]) => ({
  agents,
  partial: false,
  connected: true,
  onAgent: vi.fn(),
  onAllAgents: vi.fn(),
});
const heading = () => screen.getByRole('button', { name: /^Agents/ });
const expand = () => fireEvent.click(heading());
const ids = (container: HTMLElement) =>
  [...container.querySelectorAll('[data-agent-dock-row]')].map((row) =>
    row.getAttribute('data-agent-dock-row'),
  );
const duration = (id: string) =>
  document.querySelector(
    `[data-agent-dock-row="${id}"] [data-agent-dock-duration]`,
  )?.textContent;
beforeEach(() =>
  vi.stubGlobal('matchMedia', () => ({
    matches: false,
    addEventListener() {},
    removeEventListener() {},
  })),
);
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

it('starts as a compact disclosure and prioritizes attention, active work, new children and final outcomes', () => {
  const view = render(
    <AgentDockRoster
      {...props([
        child('done', 'succeeded'),
        child('working'),
        child('new', 'idle'),
        child('failed', 'failed'),
      ])}
    />,
  );
  expect(heading().textContent).toBe(
    'Agents · 1 needs attention · 1 working · 1 not started · 1 finished',
  );
  expect(ids(view.container)).toEqual([]);
  expand();
  expect(ids(view.container)).toEqual(['failed', 'working', 'new', 'done']);
  expand();
  expect(ids(view.container)).toEqual([]);
});
it('uses queue and current activity before a previous failed outcome', () => {
  const queued = child('queued', 'queued'),
    resumed = {
      ...child('resumed', 'failed'),
      activity: activity('resumed', turn('new', 'running', 'resumed')),
    };
  expect(projectAgent(queued)).toMatchObject({
    text: 'Queued',
    finished: false,
    attention: false,
  });
  expect(projectAgent(resumed)).toMatchObject({
    text: 'Working',
    finished: false,
    attention: false,
  });
});
it('keeps explicit approval and question state separate from released execution permits', () => {
  const values = [child('approval'), child('question'), child('waiting')];
  values[0]!.activity = {
    ...values[0]!.activity,
    pending_permission_count: '1',
  };
  values[1]!.activity = { ...values[1]!.activity, pending_question_count: '1' };
  values[2]!.activity = { ...values[2]!.activity, execution_permit: false };
  render(<AgentDockRoster {...props(values)} />);
  expand();
  expect(screen.getByText('Waiting for your approval')).toBeTruthy();
  expect(screen.getByText('Waiting for your answer')).toBeTruthy();
  expect(screen.getByText('Waiting')).toBeTruthy();
});
it('opens the exact child and marks the currently open companion', () => {
  const input = props([child('a'), child('b')]);
  render(<AgentDockRoster {...input} openAgentId="b" />);
  expand();
  const button = screen.getByRole('button', { name: /b · Working/ });
  expect(button.getAttribute('aria-current')).toBe('true');
  fireEvent.click(button);
  expect(input.onAgent).toHaveBeenCalledWith('b');
});
it('shows child names while duplicate names still open the exact session', () => {
  const a = child('a'), b = child('b');
  a.agent.name = b.agent.name = 'Reviewer';
  const input = props([a, b]);
  render(<AgentDockRoster {...input} openAgentId="b" />);
  expand();
  const named = screen.getAllByRole('button', { name: /^Reviewer · Working/ });
  expect(named).toHaveLength(2);
  const selected = named.find(button => button.getAttribute('aria-current') === 'true')!;
  fireEvent.click(selected);
  expect(input.onAgent).toHaveBeenCalledWith('b');
});
it('freezes order while a row is hovered or focused and reconciles after release', () => {
  const input = props([child('a'), child('b'), child('c')]);
  const view = render(<AgentDockRoster {...input} />);
  expand();
  fireEvent.pointerEnter(screen.getByRole('button', { name: /b · Working/ }));
  view.rerender(
    <AgentDockRoster
      {...props([child('urgent', 'failed'), ...input.agents])}
    />,
  );
  expect(ids(view.container)).toEqual(['a', 'b', 'c', 'urgent']);
  fireEvent.pointerLeave(screen.getByRole('button', { name: /b · Working/ }));
  expect(ids(view.container)).toEqual(['urgent', 'a', 'b', 'c']);
  act(() => screen.getByRole('button', { name: /a · Working/ }).focus());
  view.rerender(
    <AgentDockRoster
      {...props([
        child('new', 'failed'),
        child('urgent', 'failed'),
        ...input.agents,
      ])}
    />,
  );
  expect(ids(view.container)).toEqual(['urgent', 'a', 'b', 'c', 'new']);
});
it('returns focus to the disclosure when the focused child disappears, including the final child', () => {
  const view = render(<AgentDockRoster {...props([child('only')])} />);
  expand();
  act(() => screen.getByRole('button', { name: /only · Working/ }).focus());
  view.rerender(<AgentDockRoster {...props([])} />);
  expect(document.activeElement).toBe(heading());
  fireEvent.blur(heading());
  expect(view.container.textContent).toBe('');
});
it('labels disconnected and partial evidence without pretending the roster is complete', () => {
  const input = props([child('worker')]),
    view = render(<AgentDockRoster {...input} />);
  expand();
  view.rerender(<AgentDockRoster {...input} connected={false} />);
  expect(heading().textContent).toBe('Agents · Updates paused');
  expect(screen.queryByRole('img', { name: 'Agent is busy' })).toBeNull();
  view.rerender(<AgentDockRoster {...input} partial agents={[]} />);
  expect(heading().textContent).toBe('Agents · Partial agent list');
  fireEvent.click(screen.getByRole('button', { name: /See all agents/ }));
  expect(input.onAllAgents).toHaveBeenCalledOnce();
});
it('does not invent model-call counts when native turn metadata has no accounting projection', () => {
  render(<AgentDockRoster {...props([child('worker')])} />);
  expand();
  expect(document.querySelector('[data-agent-dock-calls]')?.textContent).toBe(
    '',
  );
  expect(screen.queryByText(/0 calls/)).toBeNull();
});
it('ticks only while expanded and visible, and completed duration remains fixed', () => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date('2026-09-28T12:01:05Z'));
  const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(false);
  const input = props([child('worker')]),
    view = render(<AgentDockRoster {...input} />);
  expect(vi.getTimerCount()).toBe(0);
  expand();
  expect(duration('worker')).toBe('1m 5s');
  act(() => vi.advanceTimersByTime(2000));
  expect(duration('worker')).toBe('1m 7s');
  hidden.mockReturnValue(true);
  act(() => document.dispatchEvent(new Event('visibilitychange')));
  expect(vi.getTimerCount()).toBe(0);
  act(() => vi.advanceTimersByTime(3000));
  hidden.mockReturnValue(false);
  act(() => document.dispatchEvent(new Event('visibilitychange')));
  expect(duration('worker')).toBe('1m 10s');
  view.rerender(<AgentDockRoster {...props([child('worker', 'succeeded')])} />);
  expect(duration('worker')).toBe('2s');
  expect(vi.getTimerCount()).toBe(0);
});
it('shows a fresh elapsed estimate immediately on expansion and pauses disconnected clocks', () => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date(at));
  const input = props([child('worker')]),
    view = render(<AgentDockRoster {...input} />);
  act(() => vi.advanceTimersByTime(65_000));
  expand();
  expect(duration('worker')).toBe('1m 5s');
  view.rerender(<AgentDockRoster {...input} connected={false} />);
  expect(duration('worker')).toBe('—');
  expect(vi.getTimerCount()).toBe(0);
});
it('never substitutes a previous turn duration for queued or mismatched active work', () => {
  const queued = child('queued', 'queued'),
    resumed = {
      ...child('resumed', 'succeeded'),
      activity: activity('resumed', turn('replacement', 'running', 'resumed')),
    };
  render(
    <AgentDockRoster {...props([queued, resumed, child('new', 'idle')])} />,
  );
  expand();
  for (const id of ['queued', 'resumed', 'new']) expect(duration(id)).toBe('—');
});
it('reads a bounded native tree roster and hydrates only direct-child activity and latest turn', async () => {
  const f = await providerFixture(),
    session = f.client.session('root');
  const root = sessionRecord('root'),
    direct = sessionRecord('child'),
    descendant = { ...sessionRecord('grandchild'), parent_id: 'child' };
  f.data.handlers['sessions.list'] = () => ({
    items: [root, direct, descendant],
  });
  f.data.handlers['sessions.activity'] = (request) =>
    activity(
      parameters('SessionParams', request.params).session_id,
      turn('child-turn', 'running', 'child'),
    );
  f.data.handlers['sessions.turns'] = () => ({
    items: [turn('child-turn', 'running', 'child')],
    next_cursor: null,
  });
  const view = f.mount(
    <AgentDock
      session={session}
      treeId="tree"
      connected
      onAgent={vi.fn()}
      onAllAgents={vi.fn()}
    />,
  );
  await waitFor(() => expect(heading()).toBeTruthy());
  expand();
  expect(ids(view.container)).toEqual(['child']);
  expect(
    f.calls.find((call) => call.method === 'sessions.list')?.params,
  ).toEqual({ tree_id: 'tree', limit: 16 });
  expect(f.count('sessions.activity')).toBe(1);
  expect(f.count('sessions.history_page')).toBe(0);
  expect(
    f.calls.find((call) => call.method === 'sessions.turns')?.params,
  ).toEqual({ session_id: 'child', limit: 1 });
});
