import { act, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { Profiler } from 'react';
import type { RootSnapshot } from '@whip/protocol';
import type { SessionViewSnapshot } from '@whip/sdk/state';
import { AgentDock, type AgentDockProps } from '../src/agent-dock';

type Agent = NonNullable<RootSnapshot['agents']>[number];
const agent = (id: string, overrides: Partial<Agent> = {}): Agent => ({
  id, name: id, parent_id: 'root', status: 'running', ...overrides,
} as Agent);
const state = (agents: Agent[], overrides: Record<string, unknown> = {}): SessionViewSnapshot => ({
  status: 'live', root: { root_id: 'root', agents, active_turns: {}, permissions: [], questions: [], ...overrides },
  history: {}, collections: {}, retainedBytes: 0, unavailable: false, truncated: false,
} as unknown as SessionViewSnapshot);
const props = (agents: Agent[], overrides: Partial<AgentDockProps> = {}): AgentDockProps => ({
  state: state(agents), agentId: 'root', connected: true, onAgent: vi.fn(), onAllAgents: vi.fn(), ...overrides,
});
const visibleIds = (container: HTMLElement) => [...container.querySelectorAll('[data-agent-dock-row]')].map(row => row.getAttribute('data-agent-dock-row'));
const heading = () => screen.getByRole('button', { name: /^Agents/ });
const expand = () => fireEvent.click(heading());
const duration = (id: string) => document.querySelector(`[data-agent-dock-row="${id}"] [data-agent-dock-duration]`)?.textContent;
const started = '2026-09-19T12:00:00Z';
const running = (id = 'worker', overrides: Partial<NonNullable<Agent['last_turn']>> = {}) => agent(id, {
  last_turn: { status: 'running', event_seq: '1', turn_id: 'turn', started_at: started, ...overrides },
});
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks(); });

it('starts as one summary disclosure, scopes to direct children, and resets for another recipient', () => {
  const view = render(<AgentDock {...props([])} />);
  expect(view.container.textContent).toBe('');
  const agents = [agent('root', { parent_id: '' }), agent('child'), agent('grandchild', { parent_id: 'child' }), agent('deleted', { status: 'deleted' })];
  view.rerender(<AgentDock {...props(agents)} />);
  expect(heading().textContent).toBe('Agents · 1 working');
  expect(heading().getAttribute('aria-expanded')).toBe('false');
  expect(visibleIds(view.container)).toEqual([]);
  expect(document.getElementById(heading().getAttribute('aria-controls')!)?.hidden).toBe(true);
  expand();
  expect(visibleIds(view.container)).toEqual(['child']);
  view.rerender(<AgentDock {...props(agents, { agentId: 'child' })} />);
  expect(heading().getAttribute('aria-expanded')).toBe('false');
  expand();
  expect(visibleIds(view.container)).toEqual(['grandchild']);
});

it('shows all agents on expansion with attention and active work first, finished last', () => {
  const agents = [agent('done', { status: 'succeeded' }), agent('a'), agent('b'), agent('c'), agent('d'), agent('new', { status: 'idle' }), agent('failed', { status: 'failed' })];
  const view = render(<AgentDock {...props(agents)} />);
  expect(heading().textContent).toBe('Agents · 1 needs attention · 4 working · 1 not started · 1 finished');
  expand();
  expect(visibleIds(view.container)).toEqual(['failed', 'a', 'b', 'c', 'd', 'new', 'done']);
  expect(screen.queryByRole('button', { name: /^More/ })).toBeNull();
  expect(screen.getByRole('button', { name: /done · Completed/ })).toBeTruthy();
  expand();
  expect(visibleIds(view.container)).toEqual([]);
});

it('keeps lifecycle state separate from model-call counts, including zero and unknown', () => {
  const agents = [running('worker', { model_calls: 8, compactions: 2 }), running('single', { model_calls: 1 }), agent('unknown'), running('zero', { model_calls: 0 })];
  const p = props(agents);
  render(<AgentDock {...p} />); expand();
  const worker = screen.getByRole('button', { name: /worker · Working · 8 model calls in latest turn/ });
  expect(within(worker).getByText('8 calls').title).toBe('8 model calls in latest turn');
  expect(screen.getByText('1 call').title).toBe('1 model call in latest turn');
  expect(screen.getByText('0 calls')).toBeTruthy();
  expect(worker.textContent).not.toContain('compaction');
  expect(within(screen.getByRole('button', { name: /unknown · Working/ })).queryByText(/calls?/)).toBeNull();
  fireEvent.click(worker);
  expect(p.onAgent).toHaveBeenCalledWith('worker');
});

it('does not mistake never-started or unknown agents for finished', () => {
  render(<AgentDock {...props([agent('new', { status: 'idle' }), agent('ready', { status: 'ready' }), agent('unknown', { status: '' })])} />);
  expect(heading().textContent).not.toContain('finished');
  expand();
  expect(screen.getByRole('button', { name: /new · Not started/ })).toBeTruthy();
  expect(screen.getByRole('button', { name: /ready · Not started/ })).toBeTruthy();
  expect(screen.getByRole('button', { name: /unknown · Status unavailable/ })).toBeTruthy();
  expect(screen.queryByRole('img', { name: 'Agent is busy' })).toBeNull();
});

it('uses queued inbox work before the previous turn outcome', () => {
  const agents = [agent('new', { status: 'idle' }), agent('done', { status: 'idle', last_turn: { status: 'succeeded', event_seq: '1' } }), agent('retry', { status: 'idle', last_turn: { status: 'failed', event_seq: '2' } })];
  const inbox = agents.map(item => ({ agent_id: item.id, status: 'queued' }));
  render(<AgentDock {...props(agents, { state: state(agents, { inbox }) })} />);
  expect(heading().textContent).toBe('Agents · 3 queued');
  expand();
  for (const item of agents) expect(screen.getByRole('button', { name: new RegExp(`${item.id} · Queued`) })).toBeTruthy();
  expect(screen.getAllByRole('img', { name: 'Agent is busy' })).toHaveLength(3);
});

it('transitions from not started through queued and running to recorded completion', () => {
  const idle = agent('child', { status: 'idle' });
  const view = render(<AgentDock {...props([idle])} />); expand();
  expect(screen.getByRole('button', { name: /child · Not started/ })).toBeTruthy();
  const inbox = [{ agent_id: 'child', status: 'queued' }];
  view.rerender(<AgentDock {...props([idle], { state: state([idle], { inbox }) })} />);
  expect(screen.getByRole('button', { name: /child · Queued/ })).toBeTruthy();
  view.rerender(<AgentDock {...props([idle], { state: state([idle], { inbox, active_turns: { child: 'turn' } }) })} />);
  expect(screen.getByRole('button', { name: /child · Working/ })).toBeTruthy();
  view.rerender(<AgentDock {...props([agent('child', { status: 'idle', last_turn: { status: 'succeeded', event_seq: '1' } })])} />);
  expect(screen.getByRole('button', { name: /child · Completed/ })).toBeTruthy();
  expect(heading().textContent).toBe('Agents · 1 finished');
});

it('keeps stopped outcomes distinct and ignores obsolete pending work on stopped children', () => {
  const agents = ['stopped', 'cancelled', 'interrupted'].map(status => agent(status, { status }));
  render(<AgentDock {...props(agents, { state: state(agents, { inbox: agents.map(item => ({ agent_id: item.id, status: 'queued' })) }) })} />);
  expect(heading().textContent).toBe('Agents · 3 finished'); expand();
  for (const item of agents) expect(document.querySelector(`[data-agent-dock-row="${item.id}"] [data-agent-dock-status]`)?.textContent.toLowerCase()).toBe(item.status);
});

it('opening an agent highlights it without reordering or expanding the roster', () => {
  const agents = [agent('done', { status: 'succeeded' }), agent('a')];
  const view = render(<AgentDock {...props(agents, { openAgentId: 'done' })} />);
  expect(heading().getAttribute('aria-expanded')).toBe('false'); expand();
  expect(visibleIds(view.container)).toEqual(['a', 'done']);
  expect(screen.getByRole('button', { name: /done · Completed/ }).getAttribute('aria-current')).toBe('true');
  view.rerender(<AgentDock {...props(agents, { openAgentId: 'a' })} />);
  expect(visibleIds(view.container)).toEqual(['a', 'done']);
});

it('freezes row order while focused or hovered, and restores focus after deletion', () => {
  const view = render(<AgentDock {...props([agent('a'), agent('b'), agent('c')])} />); expand();
  const b = screen.getByRole('button', { name: /b · Working/ });
  act(() => b.focus());
  const updated = [agent('urgent', { status: 'failed' }), agent('a'), agent('b', { status: 'succeeded' }), agent('c')];
  view.rerender(<AgentDock {...props(updated)} />);
  expect(visibleIds(view.container)).toEqual(['a', 'b', 'c', 'urgent']);
  expect(document.activeElement).toBe(b);
  view.rerender(<AgentDock {...props(updated.filter(item => item.id !== 'b'))} />);
  expect(document.activeElement).toBe(heading());
  expect(visibleIds(view.container)).toEqual(['urgent', 'a', 'c']);
  fireEvent.pointerEnter(screen.getByRole('button', { name: /c · Working/ }));
  view.rerender(<AgentDock {...props([agent('new', { status: 'failed' }), ...updated.filter(item => item.id !== 'b')])} />);
  expect(visibleIds(view.container)).toEqual(['urgent', 'a', 'c', 'new']);
  fireEvent.pointerLeave(screen.getByRole('button', { name: /c · Working/ }));
  expect(visibleIds(view.container)).toEqual(['urgent', 'new', 'a', 'c']);
});

it('returns focus to the disclosure even when the final child is deleted', () => {
  const view = render(<AgentDock {...props([agent('only')])} />); expand();
  act(() => screen.getByRole('button', { name: /only · Working/ }).focus());
  view.rerender(<AgentDock {...props([])} />);
  expect(document.activeElement).toBe(heading());
  fireEvent.blur(heading());
  expect(view.container.textContent).toBe('');
});

it('keeps disconnected and partial rosters explicit without claiming live completeness', () => {
  const agents = [agent('working')];
  const view = render(<AgentDock {...props(agents)} />); expand();
  view.rerender(<AgentDock {...props(agents)} connected={false} />);
  expect(heading().textContent).toBe('Agents · Updates paused');
  expect(screen.getByRole('button', { name: /working · Working · Updates paused/ })).toBeTruthy();
  expect(screen.queryByRole('img', { name: 'Agent is busy' })).toBeNull();
  expect(view.container.querySelector('[aria-live]')).toBeNull();
  const p = props([], { state: state([], { omitted: { agents: 7 } }) });
  view.rerender(<AgentDock {...p} />);
  expect(heading().textContent).toBe('Agents · Partial agent list');
  fireEvent.click(screen.getByRole('button', { name: 'Partial agent list · See all agents' }));
  expect(p.onAllAgents).toHaveBeenCalledOnce();
});

it('scopes attention to the child and clears previous failure while resuming', () => {
  const agents = [agent('resumed', { status: 'idle', last_turn: { status: 'failed', event_seq: '1' } }), agent('child')];
  const snapshot = state(agents, { active_turns: { resumed: 'new-turn' }, permissions: [{ agent_id: 'root', status: 'pending' }], questions: [{ agent_id: 'child', question_id: 'question' }] });
  const view = render(<AgentDock {...props(agents, { state: snapshot })} />); expand();
  expect(visibleIds(view.container)).toEqual(['child', 'resumed']);
  expect(screen.getByRole('button', { name: /child · Waiting for your answer/ })).toBeTruthy();
  expect(screen.getByRole('button', { name: /resumed · Working/ })).toBeTruthy();
  expect(screen.queryByText('Waiting for your approval')).toBeNull();
});

it('commits a fresh duration immediately when expanding after time spent collapsed', () => {
  vi.useFakeTimers(); vi.setSystemTime(new Date(started));
  const committed: (string | null | undefined)[] = [];
  render(<Profiler id="dock" onRender={() => committed.push(duration('worker'))}>
    <AgentDock {...props([running()])} />
  </Profiler>);
  act(() => vi.advanceTimersByTime(65_000));
  expect(vi.getTimerCount()).toBe(0);
  committed.length = 0;
  expand();
  expect(committed.length).toBeGreaterThan(0);
  expect(committed.every(value => value === '1m 5s')).toBe(true);
  expand();
  act(() => vi.advanceTimersByTime(30_000));
  committed.length = 0;
  expand();
  expect(committed.every(value => value === '1m 35s')).toBe(true);
});

it('uses recorded starts across remounts, ticks only expanded, and fixes completed duration', () => {
  vi.useFakeTimers(); vi.setSystemTime(new Date('2026-09-19T12:01:05Z'));
  const view = render(<AgentDock {...props([running()])} />);
  expect(vi.getTimerCount()).toBe(0); expand();
  expect(duration('worker')).toBe('1m 5s');
  act(() => vi.advanceTimersByTime(2000));
  expect(duration('worker')).toBe('1m 7s');
  const done = agent('worker', { status: 'idle', last_turn: { status: 'succeeded', event_seq: '2', started_at: started, finished_at: '2026-09-19T12:01:06Z' } });
  view.rerender(<AgentDock {...props([done])} />);
  expect(duration('worker')).toBe('1m 6s');
  expect(vi.getTimerCount()).toBe(0);
  act(() => vi.advanceTimersByTime(10000));
  expect(duration('worker')).toBe('1m 6s');
  view.unmount();
  render(<AgentDock {...props([running()])} />); expand();
  expect(duration('worker')).toBe('1m 17s');
  expand(); expect(vi.getTimerCount()).toBe(0);
});

it('stops live clocks while disconnected or hidden and catches up on reconnect or visibility', () => {
  vi.useFakeTimers(); vi.setSystemTime(new Date('2026-09-19T12:00:10Z'));
  const p = props([running()]);
  const view = render(<AgentDock {...p} />); expand();
  expect(duration('worker')).toBe('10s');
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(true);
  fireEvent(document, new Event('visibilitychange'));
  expect(vi.getTimerCount()).toBe(0);
  act(() => vi.advanceTimersByTime(10000));
  expect(duration('worker')).toBe('10s');
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(false);
  fireEvent(document, new Event('visibilitychange'));
  expect(duration('worker')).toBe('20s');
  view.rerender(<AgentDock {...p} connected={false} />);
  expect(duration('worker')).toBe('—');
  expect(vi.getTimerCount()).toBe(0);
  act(() => vi.advanceTimersByTime(5000));
  view.rerender(<AgentDock {...p} />);
  expect(duration('worker')).toBe('25s');
  view.unmount(); expect(vi.getTimerCount()).toBe(0);
});

it('does not reuse a previous turn duration for queued, resumed, or untimed agents', () => {
  vi.useFakeTimers();
  const done = { status: 'succeeded', event_seq: '1', turn_id: 'old', started_at: started, finished_at: '2026-09-19T12:00:20Z' };
  const agents = [agent('queued', { status: 'queued', last_turn: done }), agent('resumed', { last_turn: done }), agent('missing'), running('mismatch')];
  render(<AgentDock {...props(agents, { state: state(agents, { active_turns: { resumed: 'new', mismatch: 'other' } }) })} />); expand();
  for (const item of agents) expect(duration(item.id)).toBe('—');
  expect(vi.getTimerCount()).toBe(0);
});

it('handles invalid, future, missing-end, and reversed timestamps without fabricating final time', () => {
  vi.useFakeTimers(); vi.setSystemTime(new Date(started));
  const agents = [running('invalid', { started_at: 'bad' }), running('future', { started_at: '2026-09-19T13:00:00Z' }),
    agent('missing-end', { status: 'stopped', last_turn: { status: 'succeeded', event_seq: '1', started_at: started } }),
    agent('reversed', { status: 'idle', last_turn: { status: 'succeeded', event_seq: '1', started_at: started, finished_at: '2026-09-19T11:00:00Z' } }),
    running('bad-end', { finished_at: 'bad' })];
  render(<AgentDock {...props(agents)} />); expand();
  for (const id of ['invalid', 'missing-end', 'reversed', 'bad-end']) expect(duration(id)).toBe('—');
  expect(duration('future')).toBe('0s');
});

it('includes waiting time and preserves fixed failure and stopped durations', () => {
  vi.useFakeTimers(); vi.setSystemTime(new Date('2026-09-19T13:02:03Z'));
  const agents = [running('waiting', { status: 'waiting' }), ...['failed', 'stopped'].map(status => agent(status, {
    status, last_turn: { status: 'failed', event_seq: '1', started_at: started, finished_at: '2026-09-19T12:00:00Z' },
  }))];
  render(<AgentDock {...props(agents, { state: state(agents, { permissions: [{ agent_id: 'waiting', status: 'pending' }] }) })} />); expand();
  expect(duration('waiting')).toBe('1h 2m');
  expect(duration('failed')).toBe('0s');
  expect(duration('stopped')).toBe('0s');
});
