import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import type { SessionView, SessionViewSnapshot } from '@whip/sdk/state';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { ReplView, outputPreview } from '../src/repl-view';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';

// Layout/anchor behavior is covered by timeline-reading and browser fixtures.
vi.mock('../src/reading-list', () => ({
  ReadingList: ({ rows, renderRow, empty }: { rows: { id: string }[]; renderRow(row: unknown, index: number): ReactNode; empty: ReactNode }) =>
    <div>{rows.length ? rows.map((row, index) => <div key={row.id}>{renderRow(row, index)}</div>) : empty}</div>,
}));
beforeEach(() => {
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
});
afterEach(() => vi.unstubAllGlobals());

const output = Array.from({ length: 9 }, (_, index) => `line ${index + 1}`).join('\n') + '\n';
it('previews the newest running output and first recorded lines, without changing full copy text', () => {
  expect(outputPreview(output, true, false)).toEqual({ text: 'line 4\nline 5\nline 6\nline 7\nline 8\nline 9', hidden: 3 });
  expect(outputPreview(output, false, false)).toEqual({ text: 'line 1\nline 2\nline 3\nline 4\nline 5\nline 6', hidden: 3 });
  expect(outputPreview(output, true, true)).toEqual({ text: output, hidden: 0 });
  expect(outputPreview('one\n', false, false)).toEqual({ text: 'one\n', hidden: 0 });
});

function fixture(messages: NonNullable<SessionViewSnapshot['history'][string]>['messages'] = []) {
  const state = {
    status: 'live',
    root: { root_id: 'root', history_revision: '1', agents: [], presentation: [], agent_presentations: {}, active_turns: {} },
    history: { root: { revision: '1', throughSeq: 2, nextSeq: -1, hasMore: false, messages, loading: false, truncated: false } },
    collections: {}, retainedBytes: 0, truncated: false, unavailable: false,
  } as unknown as SessionViewSnapshot;
  const view = {
    session: { rootId: 'root' },
    getSnapshot: () => state,
    subscribe: () => () => {},
    loadOlder: vi.fn(async () => {}), loadCollection: vi.fn(async () => {}),
  } as unknown as SessionView;
  const copy = vi.fn(async () => {});
  const runtime = { platform: { copy }, report: vi.fn() } as unknown as AppRuntime;
  const app = (connected = true) => <RuntimeContext.Provider value={runtime}><UIProvider><ThemeProvider initialTheme="claude-code">
    <ReplView view={view} state={state} agentId="root" runtimeId="host" viewId="view" connected={connected} onAgentChange={vi.fn()} />
  </ThemeProvider></UIProvider></RuntimeContext.Provider>;
  return { app, state, view, copy };
}

it('explains a saved failure with no execution cells instead of the ordinary empty state', () => {
  const f = fixture();
  f.state.root!.agents = [{ id: 'root', name: 'Research', last_turn: { status: 'failed', event_seq: '14', error: 'Invalid prompt_cache_key' } }] as NonNullable<SessionViewSnapshot['root']>['agents'];
  render(f.app());
  expect(screen.getByText('The last turn failed')).toBeDefined();
  expect(screen.queryByText('No executions in the loaded history')).toBeNull();
  expect(screen.queryAllByRole('article')).toHaveLength(0);
});
it('renders a read-only cell, expands output, copies exact text and does not invent historical timing', async () => {
  const f = fixture([
    { seq: 1, message: { role: 'assistant', content: '', tool_calls: [{ id: 'call', type: 'function', function: { name: 'rlm_exec', arguments: '{"code":"print(42)"}' } }] } },
    { seq: 2, message: { role: 'tool', name: 'rlm_exec', tool_call_id: 'call', content: JSON.stringify({ output, value: 42, steps: 7 }) } },
  ]);
  render(f.app());
  expect(screen.getByRole('article', { name: 'Execution 1' })).toBeDefined();
  expect(screen.getByText('Completed')).toBeDefined();
  expect(screen.getByRole('region', { name: 'Output' }).textContent).not.toContain('line 9');
  fireEvent.click(screen.getByRole('button', { name: 'Show 3 more lines' }));
  expect(screen.getByRole('region', { name: 'Output' }).textContent).toBe(output);
  fireEvent.click(screen.getByRole('button', { name: 'Copy output' }));
  await waitFor(() => expect(f.copy).toHaveBeenCalledWith(output));
  expect(screen.getByRole('region', { name: 'Return value' }).textContent).toBe('42');
  expect(screen.queryByText('⇒')).toBeNull();
  expect(screen.queryByText(/Observed \d/)).toBeNull();
  expect(screen.queryByRole('textbox')).toBeNull();
  expect(f.view.loadOlder).not.toHaveBeenCalled();
  expect(f.view.loadCollection).not.toHaveBeenCalled();
});
it('distinguishes empty, loading and unavailable agent history, and keeps stale evidence explicit', () => {
  const f = fixture();
  const mounted = render(f.app());
  expect(screen.getByText('No executions in the loaded history')).toBeDefined();
  f.state.history.root!.loading = true;
  mounted.rerender(f.app());
  expect(screen.getByText('Loading executions…')).toBeDefined();
  f.state.history.root!.loading = false;
  f.state.history.root!.error = new Error('missing');
  mounted.rerender(f.app(false));
  expect(screen.getByText('This agent’s executions are unavailable')).toBeDefined();
  expect(screen.getByRole('status').textContent).toContain('paused');
});

it('does not render a panel for a null return value', () => {
  const f = fixture([
    { seq: 1, message: { role: 'assistant', content: '', tool_calls: [{ id: 'call', type: 'function', function: { name: 'rlm_exec', arguments: '{"code":"print(42)"}' } }] } },
    { seq: 2, message: { role: 'tool', name: 'rlm_exec', tool_call_id: 'call', content: JSON.stringify({ output: '42\n', value: null, steps: 7 }) } },
  ]);
  render(f.app());
  expect(screen.getByRole('region', { name: 'Output' }).textContent).toBe('42\n');
  expect(screen.queryByRole('region', { name: 'Return value' })).toBeNull();
});

it('renders JavaScript completion with explicit null and jobs, without Starlark labels', () => {
  const f = fixture([
    { seq: 1, message: { role: 'assistant', content: '', tool_calls: [{ id: 'call', type: 'function', function: { name: 'rlm_exec', arguments: '{"code":"null"}' } }] } },
    { seq: 2, message: { role: 'tool', name: 'rlm_exec', tool_call_id: 'call', content: JSON.stringify({ format_version: 2, execution_engine: 'quickjs', language: 'javascript', has_value: true, value: null, metrics: { quickjs_jobs: 3 } }) } },
  ]);
  f.state.root!.meta = { execution_engine: 'quickjs' } as NonNullable<SessionViewSnapshot['root']>['meta'];
  render(f.app());
  expect(screen.getByText('Completed')).toBeDefined();
  expect(screen.getByText('3 jobs')).toBeDefined();
  expect(screen.getByRole('region', { name: 'Return value' }).textContent).toBe('null');
  expect(screen.getByRole('region', { name: 'Cell 1 · JavaScript (QuickJS)' })).toBeDefined();
  expect(screen.getByRole('button', { name: 'Copy JavaScript (QuickJS) code' })).toBeDefined();
  expect(screen.queryByText(/steps/)).toBeNull();
});

it('shows failed-cell output and checkpoint warnings together', () => {
  const f = fixture([
    { seq: 1, message: { role: 'assistant', content: '', tool_calls: [{ id: 'call', type: 'function', function: { name: 'rlm_exec', arguments: '{"code":"fail()"}' } }] } },
    { seq: 2, message: { role: 'tool', name: 'rlm_exec', tool_call_id: 'call', content: 'Error: cell failed\n' + JSON.stringify({ output: 'before failure', value: null, steps: 7, scratch: { warning: 'Scratch checkpoint failed. Do not replay effects.' } }) } },
  ]);
  render(f.app());
  expect(screen.getByText('Failed')).toBeDefined();
  expect(screen.getByText('cell failed')).toBeDefined();
  expect(screen.getByLabelText('Scratch checkpoint').textContent).toContain('Do not replay effects.');
  expect(screen.getByRole('region', { name: 'Output' }).textContent).toBe('before failure');
  expect(screen.queryByRole('region', { name: 'Return value' })).toBeNull();
});
