import { act, fireEvent, render, screen, within } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
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

it('is absent without children and scopes to direct, non-deleted children of this pane', () => {
  const view = render(<AgentDock {...props([])} />);
  expect(view.container.textContent).toBe('');
  const agents = [agent('root', { parent_id: '' }), agent('child'), agent('grandchild', { parent_id: 'child' }), agent('deleted', { status: 'deleted' })];
  view.rerender(<AgentDock {...props(agents)} />);
  expect(visibleIds(view.container)).toEqual(['child']);
  expect(screen.queryByRole('button', { name: 'All agents', exact: true })).toBeNull();
  view.rerender(<AgentDock {...props(agents, { agentId: 'child' })} />);
  expect(visibleIds(view.container)).toEqual(['grandchild']);
});

it('separates lifecycle state from latest-turn model-call counts', () => {
  const agents = [
    agent('worker', { last_turn: { status: 'running', event_seq: '1', model_calls: 8, compactions: 2 } }),
    agent('single', { last_turn: { status: 'running', event_seq: '2', model_calls: 1 } }),
    agent('unknown'),
  ];
  const p = props(agents);
  const view = render(<AgentDock {...p} />);
  const worker = screen.getByRole('button', { name: /worker · Working · 8 model calls in latest turn/ });
  expect(within(worker).getByText('Working', { exact: true })).toBeTruthy();
  expect(within(worker).getByText('8 calls').title).toBe('8 model calls in latest turn');
  expect(screen.getByText('1 call').title).toBe('1 model call in latest turn');
  expect(worker.textContent).not.toContain('compaction');
  expect(within(screen.getByRole('button', { name: /unknown · Working/ })).queryByText(/calls?/)).toBeNull();
  fireEvent.click(worker);
  expect(p.onAgent).toHaveBeenCalledWith('worker');
  view.rerender(<AgentDock {...props([agent('worker', { last_turn: { status: 'running', event_seq: '3', model_calls: 0 } })])} />);
  expect(screen.getByText('0 calls')).toBeTruthy();
});

it('retains call counts and exceptional outcomes inside Finished', () => {
  render(<AgentDock {...props([
    agent('done', { status: 'succeeded', last_turn: { status: 'succeeded', event_seq: '1', model_calls: 12 } }),
    agent('stopped', { status: 'stopped', last_turn: { status: 'cancelled', event_seq: '2', model_calls: 3 } }),
  ])} />);
  expect(screen.queryByText('12 calls')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Finished (2)' }));
  const done = screen.getByRole('button', { name: /done · Completed · 12 model calls in latest turn/ });
  expect(within(done).queryByText('Completed')).toBeNull();
  expect(within(done).getByText('12 calls')).toBeTruthy();
  const stopped = screen.getByRole('button', { name: /stopped · Stopped · 3 model calls in latest turn/ });
  expect(within(stopped).getByText('Stopped')).toBeTruthy();
});

it('summarizes all known children, not just compact rows, without double-counting waiting work', () => {
  const agents = [agent('a'), agent('b'), agent('c'), agent('waiting', { blocking_reason: 'approval' }), agent('queued', { status: 'queued' }), agent('done', { status: 'succeeded' })];
  const p = props(agents);
  const view = render(<AgentDock {...p} />);
  expect(screen.getByText('· 3 working · 1 queued · 1 needs attention')).toBeTruthy();
  view.rerender(<AgentDock {...p} connected={false} />);
  expect(screen.queryByText(/3 working/)).toBeNull();
  expect(screen.getByText('Updates paused')).toBeTruthy();
  view.rerender(<AgentDock {...p} state={state(agents, { omitted: { agents: true } })} />);
  expect(screen.queryByText(/3 working/)).toBeNull();
  expect(screen.getByText('Partial agent list · See all agents')).toBeTruthy();
});

it('prioritizes waiting and failed work, keeps admission order and exposes compact overflow', () => {
  const agents = [agent('working'), agent('waiting', { blocking_reason: 'approval' }), agent('failed', { status: 'idle', last_turn: { status: 'failed', event_seq: '1' } }), agent('queued', { status: 'queued' })];
  const p = props(agents);
  const view = render(<AgentDock {...p} />);
  expect(visibleIds(view.container)).toEqual(['waiting', 'failed', 'working']);
  fireEvent.click(screen.getByRole('button', { name: 'More (1)' }));
  expect(p.onAllAgents).toHaveBeenCalledOnce();
  fireEvent.click(screen.getByRole('button', { name: /waiting · Waiting/ }));
  expect(p.onAgent).toHaveBeenCalledWith('waiting');
  view.rerender(<AgentDock {...p} state={state([...agents].reverse())} />);
  expect(visibleIds(view.container)).toEqual(['waiting', 'failed', 'working']);
});

it('groups More and Finished in one footer with expanded rows below it', () => {
  const p = props([agent('a'), agent('b'), agent('c'), agent('d'), agent('done', { status: 'succeeded' })]);
  const view = render(<AgentDock {...p} />);
  const more = screen.getByRole('button', { name: 'More (1)' });
  const finished = screen.getByRole('button', { name: 'Finished (1)' });
  const footer = view.container.querySelector('[data-agent-dock-actions]');
  expect(more.parentElement).toBe(footer);
  expect(finished.parentElement).toBe(footer);
  expect(footer?.children[0]).toBe(more);
  expect(footer?.children[1]).toBe(finished);
  fireEvent.click(more);
  expect(p.onAllAgents).toHaveBeenCalledOnce();
  fireEvent.click(finished);
  expect(footer?.nextElementSibling).toBe(screen.getByLabelText('Finished agents'));
  expect(finished.getAttribute('aria-expanded')).toBe('true');
  view.rerender(<AgentDock {...p} state={state([agent('a'), agent('b'), agent('c'), agent('d'), agent('done', { status: 'succeeded' })], { omitted: { agents: true } })} />);
  expect(screen.getByRole('button', { name: 'More (1+)' })).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Finished (1+)' })).toBeTruthy();
});

it('does not classify new idle, ready, or unknown agents as finished', () => {
  const agents = [agent('new', { status: 'idle' }), agent('ready', { status: 'ready', last_turn: null }), agent('unknown', { status: 'unknown' })];
  const view = render(<AgentDock {...props(agents)} />);
  expect(visibleIds(view.container)).toEqual(['new', 'ready', 'unknown']);
  expect(screen.getByRole('button', { name: /new · Not started/ })).toBeTruthy();
  expect(screen.getByRole('button', { name: /ready · Not started/ })).toBeTruthy();
  expect(screen.getByRole('button', { name: /unknown · Status unavailable/ })).toBeTruthy();
  expect(screen.queryByRole('button', { name: /^Finished/ })).toBeNull();
  expect(screen.queryByRole('img', { name: 'Agent is busy' })).toBeNull();
});

it('uses child queued inbox work before any previous turn outcome', () => {
  const agents = [
    agent('new', { status: 'idle' }),
    agent('done', { status: 'idle', last_turn: { status: 'succeeded', event_seq: '1' } }),
    agent('retry', { status: 'idle', last_turn: { status: 'failed', event_seq: '2' } }),
  ];
  const inbox = agents.map(item => ({ agent_id: item.id, status: 'queued' }));
  render(<AgentDock {...props(agents, { state: state(agents, { inbox }) })} />);
  for (const item of agents) expect(screen.getByRole('button', { name: new RegExp(`${item.id} · Queued`) })).toBeTruthy();
  expect(screen.getByText('· 3 queued')).toBeTruthy();
  expect(screen.getAllByRole('img', { name: 'Agent is busy' })).toHaveLength(3);
  expect(screen.queryByRole('button', { name: /^Finished/ })).toBeNull();
});

it('keeps pending children in More rather than Finished when compact rows are full', () => {
  const agents = [agent('a'), agent('b'), agent('c'), agent('pending', { status: 'idle' }), agent('queued', { status: 'idle' })];
  render(<AgentDock {...props(agents, { state: state(agents, { inbox: [{ agent_id: 'queued', status: 'queued' }] }) })} />);
  expect(screen.getByRole('button', { name: 'More (2)' })).toBeTruthy();
  expect(screen.queryByRole('button', { name: /^Finished/ })).toBeNull();
});

it('transitions from not started through queued and running to a recorded completion', () => {
  const idle = agent('child', { status: 'idle' });
  const view = render(<AgentDock {...props([idle])} />);
  expect(screen.getByRole('button', { name: /child · Not started/ })).toBeTruthy();
  const inbox = [{ agent_id: 'child', status: 'queued' }];
  view.rerender(<AgentDock {...props([idle], { state: state([idle], { inbox }) })} />);
  expect(screen.getByRole('button', { name: /child · Queued/ })).toBeTruthy();
  view.rerender(<AgentDock {...props([idle], { state: state([idle], { inbox, active_turns: { child: 'turn' } }) })} />);
  expect(screen.getByRole('button', { name: /child · Working/ })).toBeTruthy();
  const done = agent('child', { status: 'idle', last_turn: { status: 'succeeded', event_seq: '1' } });
  view.rerender(<AgentDock {...props([done])} />);
  fireEvent.click(screen.getByRole('button', { name: 'Finished (1)' }));
  expect(within(screen.getByLabelText('Finished agents')).getByRole('button', { name: /child · Completed/ })).toBeTruthy();
});

it('ignores other recipients and consumed queue rows and preserves explicit stopped outcomes', () => {
  const agents = [agent('new', { status: 'idle' }), agent('done', { status: 'idle', last_turn: { status: 'succeeded', event_seq: '1' } }), agent('stopped', { status: 'stopped' })];
  const inbox = [{ agent_id: 'root', status: 'queued' }, { agent_id: 'new', status: 'completed' }, { agent_id: 'done', status: 'cancelled' }, { agent_id: 'stopped', status: 'queued' }];
  render(<AgentDock {...props(agents, { state: state(agents, { inbox }) })} />);
  expect(screen.getByRole('button', { name: /new · Not started/ })).toBeTruthy();
  expect(screen.queryByText('Queued')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Finished (2)' }));
  expect(within(screen.getByLabelText('Finished agents')).getAllByRole('button')).toHaveLength(2);
});

it('shows a failed lifecycle even without a last-turn record', () => {
  render(<AgentDock {...props([agent('failed', { status: 'failed' })])} />);
  expect(screen.getByRole('button', { name: /failed · Failed/ })).toBeTruthy();
  expect(screen.queryByText('Idle')).toBeNull();
});

it('collapses finished and stopped work by default, expands bounded rows, and returns resumed work', () => {
  const done = agent('done', { status: 'idle', last_turn: { status: 'succeeded', event_seq: '2' } });
  const stopped = agent('stopped', { status: 'stopped', blocking_reason: 'old_reason' });
  const view = render(<AgentDock {...props([done, stopped])} />);
  expect(visibleIds(view.container)).toEqual([]);
  const disclosure = screen.getByRole('button', { name: 'Finished (2)' });
  expect(disclosure.getAttribute('aria-expanded')).toBe('false');
  fireEvent.click(disclosure);
  expect(visibleIds(view.container)).toEqual(['done', 'stopped']);
  expect(within(screen.getByLabelText('Finished agents')).getByRole('button', { name: /stopped · Stopped/ })).toBeTruthy();
  fireEvent.click(disclosure);
  view.rerender(<AgentDock {...props([agent('done'), stopped])} />);
  expect(visibleIds(view.container)).toEqual(['done']);
  expect(screen.getByRole('button', { name: 'Finished (1)' }).getAttribute('aria-expanded')).toBe('false');
});

it('keeps open children in lifecycle order and finished children inside their disclosure', () => {
  const agents = [agent('a'), agent('b'), agent('c'), agent('done', { status: 'succeeded' })];
  const view = render(<AgentDock {...props(agents, { openAgentId: 'done' })} />);
  expect(visibleIds(view.container)).toEqual(['a', 'b', 'c']);
  const disclosure = screen.getByRole('button', { name: 'Finished (1)' });
  expect(disclosure.getAttribute('aria-expanded')).toBe('false');
  fireEvent.click(disclosure);
  const row = within(screen.getByLabelText('Finished agents')).getByRole('button', { name: /done · Completed · Open/ });
  expect(row.getAttribute('aria-current')).toBe('true');
  expect(row.textContent).toBe('done');
  view.rerender(<AgentDock {...props(agents, { openAgentId: 'c' })} />);
  expect(visibleIds(view.container)).toEqual(['a', 'b', 'c', 'done']);
  expect(row.hasAttribute('aria-current')).toBe(false);
  expect(screen.getByRole('button', { name: /c · Working · Open/ }).getAttribute('aria-current')).toBe('true');
  fireEvent.click(disclosure);
  view.rerender(<AgentDock {...props(agents, { openAgentId: 'done' })} />);
  expect(visibleIds(view.container)).toEqual(['a', 'b', 'c']);
  expect(disclosure.getAttribute('aria-expanded')).toBe('false');
});

it('uses busy indicators for running and queued work, but not attention or paused updates', () => {
  const agents = [agent('working'), agent('queued', { status: 'queued' }), agent('waiting', { blocking_reason: 'approval' })];
  const p = props(agents);
  const view = render(<AgentDock {...p} />);
  expect(screen.getAllByRole('img', { name: 'Agent is busy' })).toHaveLength(2);
  expect(within(screen.getByRole('button', { name: /waiting · Waiting/ })).queryByRole('img')).toBeNull();
  expect(view.container.querySelector('.lucide-columns2')).toBeNull();
  view.rerender(<AgentDock {...p} connected={false} />);
  expect(screen.queryByRole('img', { name: 'Agent is busy' })).toBeNull();
});

it('preserves the focused row and pointer targets during status updates, with deletion fallback', () => {
  const agents = [agent('a'), agent('b'), agent('c')];
  const view = render(<AgentDock {...props(agents)} />);
  const b = screen.getByRole('button', { name: /b · Working/ });
  act(() => b.focus());
  const updated = [agent('urgent', { status: 'failed' }), agent('a'), agent('b', { status: 'succeeded' }), agent('c')];
  view.rerender(<AgentDock {...props(updated)} />);
  expect(visibleIds(view.container)).toEqual(['a', 'b', 'c']);
  expect(document.activeElement).toBe(b);
  expect(b.textContent).toContain('Completed');
  view.rerender(<AgentDock {...props(updated.filter(item => item.id !== 'b'))} />);
  expect(document.activeElement).toBe(screen.getByText('Agents', { exact: true }));
  const a = screen.getByRole('button', { name: /a · Working/ });
  fireEvent.pointerEnter(a);
  const before = visibleIds(view.container);
  view.rerender(<AgentDock {...props([agent('new', { blocking_reason: 'approval' }), ...updated.filter(item => item.id !== 'b')])} />);
  expect(visibleIds(view.container)).toEqual(before);
  fireEvent.pointerLeave(a);
  expect(visibleIds(view.container)).toEqual(['urgent', 'new', 'a']);
});

it('preserves focused finished rows when their work resumes', () => {
  const view = render(<AgentDock {...props([agent('done', { status: 'succeeded' })])} />);
  fireEvent.click(screen.getByRole('button', { name: 'Finished (1)' }));
  const button = screen.getByRole('button', { name: /done · Completed/ });
  act(() => button.focus());
  view.rerender(<AgentDock {...props([agent('done')])} />);
  expect(document.activeElement).toBe(button);
  expect(within(screen.getByLabelText('Finished agents')).getByRole('button')).toBe(button);
});

it('retains truthful offline statuses and shows partial metadata without claiming a total', () => {
  const agents = [agent('working'), agent('waiting')];
  const snapshot = state(agents, { omitted: { agents: 7 }, permissions: [{ agent_id: 'waiting', status: 'pending' }] });
  const p = props(agents, { state: snapshot });
  const view = render(<AgentDock {...p} />);
  expect(visibleIds(view.container)).toEqual(['waiting', 'working']);
  expect(screen.getByText('Partial agent list · See all agents')).toBeTruthy();
  view.rerender(<AgentDock {...p} connected={false} />);
  expect(visibleIds(view.container)).toEqual(['waiting', 'working']);
  expect(screen.getByText('Updates paused')).toBeTruthy();
  expect(screen.getByRole('button', { name: /working · Working · Updates paused/ })).toBeTruthy();
  expect(screen.getByRole('button', { name: /waiting · Waiting for your approval · Updates paused/ })).toBeTruthy();
  expect(view.container.querySelector('[aria-live]')).toBeNull();
  view.rerender(<AgentDock {...props([], { state: state([], { omitted: { agents: 7 } }) })} />);
  expect(screen.getByRole('button', { name: 'Partial agent list · See all agents' })).toBeTruthy();
  expect(screen.getByText('Partial agent list · See all agents')).toBeTruthy();
});

it('does not apply another agent’s requests to a child or retain old failure when it resumes', () => {
  const agents = [agent('resumed', { status: 'idle', last_turn: { status: 'failed', event_seq: '1' } }), agent('child')];
  const snapshot = state(agents, {
    active_turns: { resumed: 'new-turn' },
    permissions: [{ agent_id: 'root', status: 'pending' }],
    questions: [{ agent_id: 'child', question_id: 'question' }],
  });
  const view = render(<AgentDock {...props(agents, { state: snapshot })} />);
  expect(visibleIds(view.container)).toEqual(['child', 'resumed']);
  expect(screen.getByRole('button', { name: /child · Waiting for your answer/ })).toBeTruthy();
  expect(screen.getByRole('button', { name: /resumed · Working/ })).toBeTruthy();
  expect(screen.queryByText('Waiting for your approval')).toBeNull();
});

it('moves focus to the dock heading even when the final child is deleted', () => {
  const view = render(<AgentDock {...props([agent('only')])} />);
  act(() => screen.getByRole('button', { name: /only · Working/ }).focus());
  view.rerender(<AgentDock {...props([])} />);
  expect(document.activeElement).toBe(screen.getByText('Agents', { exact: true }));
  fireEvent.blur(document.activeElement!);
  expect(view.container.textContent).toBe('');
});
