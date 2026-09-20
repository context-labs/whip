import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import userEvent from '@testing-library/user-event';
import type { SessionView, SessionViewSnapshot, TraceEvidence, TraceSpan } from '@whip/sdk/state';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { TraceView } from '../src/trace-view';
import * as traceMath from '../src/trace-math';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';

// jsdom measures nothing, so the virtualizer renders every row; scrolling and
// zoom geometry are covered by the trace-math tests and browser fixtures.
vi.mock('@tanstack/react-virtual', () => ({
  useVirtualizer: ({ count, getItemKey }: { count: number; getItemKey(index: number): string }) => ({
    getTotalSize: () => count * 28,
    getVirtualItems: () => Array.from({ length: count }, (_, index) => ({ index, key: getItemKey(index), start: index * 28, size: 28 })),
  }),
}));
beforeEach(() => {
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} });
});
afterEach(() => {
  vi.unstubAllGlobals();
  delete document.documentElement.dataset.motion;
});

const base = 1_700_000_000_000;
const span = (overrides: Partial<TraceSpan> & { id: string }): TraceSpan => ({
  traceId: 't1', parentId: '', rootId: 'root', agentId: 'root', turnId: 'turn-1', kind: 'agent', name: 'root', status: 'ok',
  startMs: base, endMs: base + 10_000, attrs: {}, links: [], updatedSeq: '1', ...overrides,
});
const spans: TraceSpan[] = [
  span({ id: 'turn', attrs: { agent_name: 'root', input: 'fix the failing test' } }),
  span({ id: 'call', parentId: 'turn', kind: 'llm', name: 'inference-net/kimi-k3-fast', startMs: base + 100, endMs: base + 2_100, attrs: { model: 'kimi-k3-fast', prompt_tokens: 1200, completion_tokens: 30, cost_micros: 4000, cost_source: 'estimated' } }),
  span({ id: 'cell', parentId: 'turn', kind: 'tool', name: 'rlm_exec', startMs: base + 2_200, endMs: base + 2_600, attrs: { summary: 'print(files.read("README.md"))', input: '{"code":"print(1)"}', output: '# Whip' } }),
  span({ id: 'read', parentId: 'cell', kind: 'host', name: 'files.read', startMs: base + 2_250, endMs: base + 2_291, attrs: { summary: 'path=README.md', operation_id: 'op-1' } }),
  span({ id: 'second', parentId: 'turn', kind: 'llm', name: 'inference-net/kimi-k3-fast', status: 'running', startMs: base + 3_000, endMs: 0, attrs: { model: 'kimi-k3-fast' } }),
];

function fixture(trace: Partial<TraceEvidence> | undefined = { loaded: true }) {
  const evidence: TraceEvidence | undefined = trace && {
    rootId: 'root', pageCursor: '9', spans: Object.fromEntries(spans.map(item => [item.id, item])), loaded: true, loading: false, hasMore: false, truncated: false, clockOffsetMs: 0, ...trace,
  };
  const holder = { state: {
    status: 'live',
    root: { root_id: 'root', history_revision: '1', agents: [{ id: 'root', name: 'root' }], presentation: [], agent_presentations: {}, active_turns: {} },
    history: {}, collections: {}, retainedBytes: 0, truncated: false, unavailable: false, trace: evidence,
  } as unknown as SessionViewSnapshot };
  // ContentRead reads the connection through the session's client; a stable
  // snapshot keeps useSyncExternalStore quiet.
  const connection = { state: 'connected', info: { connection_id: 'connection-1' } };
  const view = {
    session: { rootId: 'root', client: { subscribe: () => () => {}, getSnapshot: () => connection } },
    getSnapshot: () => holder.state,
    subscribe: () => () => {},
    loadTrace: vi.fn(async () => {}),
  } as unknown as SessionView;
  const runtime = { platform: { copy: vi.fn(async () => {}) }, report: vi.fn() } as unknown as AppRuntime;
  // Snapshots are immutable in the SDK, so each change is a new object.
  const set = (trace: Partial<TraceEvidence>) => { holder.state = { ...holder.state, trace: { ...holder.state.trace!, ...trace } as TraceEvidence }; };
  const app = (connected = true) => <RuntimeContext.Provider value={runtime}><UIProvider><ThemeProvider initialTheme="claude-code">
    <TraceView view={view} state={holder.state} agentId="root" runtimeId="host" viewId="view" connected={connected} />
  </ThemeProvider></UIProvider></RuntimeContext.Provider>;
  return { app, set, view, connection };
}

it('pages the durable spans once when connected and renders the tree with server-measured durations', async () => {
  const f = fixture();
  render(f.app());
  await waitFor(() => expect(f.view.loadTrace).toHaveBeenCalledTimes(1));
  const tree = screen.getByRole('tree', { name: 'Trace spans' });
  const items = await screen.findAllByRole('treeitem');
  expect(items.map(item => item.getAttribute('aria-label'))).toEqual([
    'root · 10.0s', 'kimi-k3-fast · 2.0s', 'rlm_exec: print(files.read("README.md")) · 400ms', 'files.read: path=README.md · 41ms', 'kimi-k3-fast · running',
  ]);
  expect(items[3]!.getAttribute('aria-level')).toBe('3');
  expect(tree).toBeDefined();
  // Header totals come from the loaded spans: one priced call, both calls' tokens.
  const totals = screen.getByLabelText('Trace totals').textContent!;
  expect(totals).toContain('spans5');
  expect(totals).toContain('cost$0.0040');
  expect(totals).toContain('tokens1.2K');
  // The header badge and the open span's row both say running.
  expect(screen.getAllByText('running').length).toBeGreaterThan(1);
});

it('keeps pane choices independent with the shared keyboard-accessible toggle group', async () => {
  render(fixture().app());
  const user = userEvent.setup();
  const panes = within(screen.getByRole('group', { name: 'Panes' }));
  expect(panes.getAllByRole('button', { pressed: true })).toHaveLength(2);
  panes.getByRole('button', { name: 'Tree' }).focus();
  await user.keyboard('{ArrowRight}{ArrowRight}{Enter}');
  expect(panes.getAllByRole('button', { pressed: true })).toHaveLength(3);
  for (const name of ['Tree', 'Timeline', 'Details']) await user.click(panes.getByRole('button', { name }));
  expect(panes.queryAllByRole('button', { pressed: true })).toHaveLength(0);
  await user.click(panes.getByRole('button', { name: 'Tree' }));
  expect(panes.getAllByRole('button', { pressed: true })).toHaveLength(1);
});

it('falls back to the newest retained trace after the picked trace is evicted', async () => {
  const user = userEvent.setup();
  const older = span({ id: 'older', traceId: 'old', name: 'older' });
  const newer = span({ id: 'newer', traceId: 'new', name: 'newer', startMs: base + 20_000, endMs: base + 21_000 });
  const f = fixture({ spans: { older, newer } });
  const mounted = render(f.app());
  await user.click(screen.getByRole('combobox', { name: 'Trace' }));
  await user.click(await screen.findByRole('option', { name: '1. older' }));
  expect(screen.getByRole('treeitem', { name: 'older · 10.0s' })).toBeDefined();
  f.set({ spans: { newer }, truncated: true });
  mounted.rerender(f.app());
  expect(screen.getByRole('treeitem', { name: 'newer · 1.0s' })).toBeDefined();
  expect(screen.getByRole('combobox', { name: 'Trace' }).textContent).toContain('newer');
  expect(screen.queryByText('No recorded spans')).toBeNull();
});

it('preserves Whole session selection when old trace evidence is evicted', async () => {
  const user = userEvent.setup();
  const older = span({ id: 'older', traceId: 'old', name: 'older' });
  const newer = span({ id: 'newer', traceId: 'new', name: 'newer', startMs: base + 20_000, endMs: base + 21_000 });
  const newest = span({ id: 'newest', traceId: 'newest', name: 'newest', startMs: base + 30_000, endMs: base + 31_000 });
  const f = fixture({ spans: { older, newer } });
  const mounted = render(f.app());
  await user.click(screen.getByRole('combobox', { name: 'Trace' }));
  await user.click(await screen.findByRole('option', { name: 'Whole session' }));
  f.set({ spans: { newer, newest }, truncated: true });
  mounted.rerender(f.app());
  expect(screen.getAllByRole('treeitem')).toHaveLength(2);
  expect(screen.getByRole('combobox', { name: 'Trace' }).textContent).toContain('Whole session');
});

it('cancels native wheel scrolling only for timeline zoom/pan and removes the listener', () => {
  const f = fixture({ spans: {} });
  const mounted = render(f.app());
  f.set({ spans: { turn: span({ id: 'turn' }) } });
  mounted.rerender(f.app());
  const tree = screen.getByRole('tree', { name: 'Trace spans' });
  const wheel = (init: WheelEventInit) => fireEvent(tree, new WheelEvent('wheel', { bubbles: true, cancelable: true, ...init }));
  expect(wheel({ deltaY: 40 })).toBe(true);
  expect(wheel({ ctrlKey: true, deltaY: -40, clientX: 500 })).toBe(false);
  expect(wheel({ metaKey: true, deltaY: -40, clientX: 500 })).toBe(false);
  expect(wheel({ deltaX: 40, deltaY: 1 })).toBe(false);
  fireEvent.click(screen.getByRole('button', { name: 'Timeline' }));
  expect(wheel({ ctrlKey: true, deltaY: -40 })).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: 'Timeline' }));
  expect(wheel({ ctrlKey: true, deltaY: -40 })).toBe(false);
  mounted.unmount();
  expect(wheel({ ctrlKey: true, deltaY: -40 })).toBe(true);
});

it('starts with span details hidden and allows toggling them open and closed', () => {
  const f = fixture();
  render(f.app());
  const toggle = screen.getByRole('button', { name: 'Details' });
  expect(toggle.getAttribute('aria-pressed')).toBe('false');
  expect(screen.queryByRole('complementary', { name: 'Span details' })).toBeNull();
  fireEvent.click(toggle);
  expect(toggle.getAttribute('aria-pressed')).toBe('true');
  expect(screen.getByRole('complementary', { name: 'Span details' })).toBeDefined();
  fireEvent.click(toggle);
  expect(toggle.getAttribute('aria-pressed')).toBe('false');
  expect(screen.queryByRole('complementary', { name: 'Span details' })).toBeNull();
});

it('collapses a subtree, selects a span for the detail pane, and rolls cost up on the turn', async () => {
  const f = fixture();
  render(f.app());
  await screen.findAllByRole('treeitem');
  fireEvent.click(screen.getAllByRole('button', { name: 'Collapse', hidden: true })[1]!);
  expect(screen.getAllByRole('treeitem').map(item => item.getAttribute('aria-label'))).not.toContain('files.read: path=README.md · 41ms');
  fireEvent.click(screen.getByRole('treeitem', { name: 'root · 10.0s' }));
  const details = screen.getByRole('complementary', { name: 'Span details' });
  expect(details.textContent).toContain('cost (rolled up)');
  expect(details.textContent).toContain('$0.0040');
  expect(details.textContent).toContain('children');
  expect(details.textContent).toContain('fix the failing test');
  fireEvent.click(screen.getByRole('button', { name: 'Expand', hidden: true }));
  fireEvent.click(screen.getByRole('treeitem', { name: 'files.read: path=README.md · 41ms' }));
  expect(screen.getByRole('complementary', { name: 'Span details' }).textContent).toContain('41ms');
  const modes = within(screen.getByRole('group', { name: 'Detail mode' }));
  fireEvent.click(modes.getByRole('button', { name: 'Raw' }));
  fireEvent.click(modes.getByRole('button', { name: 'Raw' }));
  expect(modes.getAllByRole('button', { pressed: true })).toHaveLength(1);
  expect(modes.getByRole('button', { name: 'Raw', pressed: true })).toBeDefined();
  expect(screen.getByRole('region', { name: 'Raw span' }).textContent).toContain('"operation_id": "op-1"');
});

it('names a fold "compaction" and offers the interned prompt and summary for reading', async () => {
  const fold = span({ id: 'fold', parentId: 'turn', kind: 'llm', name: 'compaction', startMs: base + 50, endMs: base + 90, attrs: { model: 'compact-model', purpose: 'compaction', output_ref: 'ref-summary', output_bytes: 2048, raw_cutoff: 12 } });
  const call = span({ id: 'call', parentId: 'turn', kind: 'llm', name: 'inference-net/kimi-k3-fast', startMs: base + 100, endMs: base + 2_100, attrs: { model: 'kimi-k3-fast', purpose: 'turn', system_prompt_ref: 'ref-prompt', system_prompt_bytes: 35_328, ephemeral_ref: 'ref-notice', ephemeral_bytes: 300 } });
  const f = fixture({ spans: { turn: spans[0]!, fold, call } });
  render(f.app());
  const items = await screen.findAllByRole('treeitem');
  expect(items.map(item => item.getAttribute('aria-label'))).toEqual(['root · 10.0s', 'compaction · 40ms', 'kimi-k3-fast · 2.0s']);
  fireEvent.click(screen.getByRole('treeitem', { name: 'kimi-k3-fast · 2.0s' }));
  expect(screen.getByRole('complementary', { name: 'Span details' }).textContent).toContain('35 KB');
  expect(screen.getByRole('complementary', { name: 'Span details' }).textContent).toContain('300 B');
  expect(screen.getByRole('button', { name: 'Read system prompt' })).toBeDefined();
  expect(screen.getByRole('button', { name: 'Read ephemeral notice' })).toBeDefined();
  fireEvent.click(screen.getByRole('treeitem', { name: 'compaction · 40ms' }));
  const details = screen.getByRole('complementary', { name: 'Span details' }).textContent!;
  expect(details).toContain('2.0 KB');
  expect(details).toContain('folded through seq');
  expect(screen.getByRole('button', { name: 'Read compaction summary' })).toBeDefined();
  expect(screen.queryByText(/recorded no excerpt/)).toBeNull();
});

it('explains loading, disconnected, truncated and failed evidence without inventing spans', async () => {
  const f = fixture({ loaded: false, loading: true, spans: {} });
  const mounted = render(f.app());
  expect(screen.getByText('Loading trace…')).toBeDefined();
  f.set({ loading: false, loaded: false });
  mounted.rerender(f.app(false));
  expect(screen.getByText('Trace unavailable')).toBeDefined();
  expect(screen.getByRole('status').textContent).toContain('paused');
  f.set({ loaded: true, truncated: true, error: new Error('page failed'), spans: { turn: spans[0]! } });
  mounted.rerender(f.app());
  expect(screen.getByText(/Older traces were dropped/)).toBeDefined();
  expect(screen.getByRole('button', { name: 'Retry' })).toBeDefined();
  expect(await screen.findAllByRole('treeitem')).toHaveLength(1);
});

// Drive display frames separately from wall time: duration must use the daemon
// offset, not the rAF timestamp, and skipped frames must not replay on resume.
function animationClock() {
  let time = 0;
  let id = 0;
  const pending = new Map<number, FrameRequestCallback>();
  vi.spyOn(performance, 'now').mockImplementation(() => time);
  vi.spyOn(Date, 'now').mockImplementation(() => base + time);
  vi.stubGlobal('requestAnimationFrame', vi.fn((callback: FrameRequestCallback) => {
    pending.set(++id, callback);
    return id;
  }));
  vi.stubGlobal('cancelAnimationFrame', vi.fn((id: number) => pending.delete(id)));
  return {
    pending,
    advance(ms: number) {
      act(() => {
        time += ms;
        const callbacks = [...pending.values()];
        pending.clear();
        callbacks.forEach(callback => callback(time));
      });
    },
  };
}
const live = () => span({ id: 'turn', status: 'running', endMs: 0 });

it('caps display updates near 30fps using daemon time, without recomputing cost/token rollups', () => {
  const clock = animationClock();
  const totals = vi.spyOn(traceMath, 'traceTotals');
  const rollup = vi.spyOn(traceMath, 'rollup');
  const f = fixture({ clockOffsetMs: 100, spans: { turn: live() } });
  const mounted = render(f.app());
  fireEvent.click(screen.getByRole('treeitem', { name: 'root · running' }));
  expect(screen.getByText('running · 100ms')).toBeDefined();
  const totalCalls = totals.mock.calls.length;
  const rollupCalls = rollup.mock.calls.length;
  expect(rollupCalls).toBeGreaterThan(0);
  for (let frame = 1; frame <= 100; frame++) clock.advance(10);
  expect(screen.getByText('running · 1.1s')).toBeDefined();
  expect(totals).toHaveBeenCalledTimes(totalCalls);
  expect(rollup).toHaveBeenCalledTimes(rollupCalls);
  expect(f.view.loadTrace).toHaveBeenCalledTimes(1);
  mounted.unmount();
  expect(clock.pending.size).toBe(0);
});

it('skips short frames and stops for completed, disconnected and unmounted traces', () => {
  const clock = animationClock();
  const f = fixture({ spans: { turn: live() } });
  const read = vi.spyOn(f.view, 'getSnapshot');
  const mounted = render(f.app());
  read.mockClear();
  for (let frame = 1; frame <= 100; frame++) clock.advance(10);
  expect(read.mock.calls.length).toBeGreaterThanOrEqual(29);
  expect(read.mock.calls.length).toBeLessThanOrEqual(30);
  clock.advance(10);
  mounted.rerender(f.app(false));
  expect(clock.pending.size).toBe(0);
  clock.advance(500);
  expect(screen.getByText('1.0s')).toBeDefined();
  mounted.rerender(f.app());
  expect(screen.getByText('duration').parentElement?.textContent).toBe('duration1.5s');
  expect(clock.pending.size).toBe(1);
  f.set({ spans: { turn: span({ id: 'turn', endMs: base + 1250 }) } });
  mounted.rerender(f.app());
  expect(clock.pending.size).toBe(0);
  expect(screen.getByRole('treeitem', { name: 'root · 1.3s' })).toBeDefined();
  f.set({ spans: { turn: live() } });
  mounted.rerender(f.app());
  expect(clock.pending.size).toBe(1);
  mounted.unmount();
  expect(clock.pending.size).toBe(0);
});

it('pauses every visible pane in hidden documents and catches up without reloading evidence', () => {
  const clock = animationClock();
  let hidden = false;
  vi.spyOn(document, 'hidden', 'get').mockImplementation(() => hidden);
  const first = fixture({ spans: { turn: live() } });
  const second = fixture({ spans: { turn: live() } });
  const one = render(first.app());
  const two = render(second.app());
  expect(clock.pending.size).toBe(2);
  act(() => { hidden = true; document.dispatchEvent(new Event('visibilitychange')); });
  expect(clock.pending.size).toBe(0);
  clock.advance(5000);
  act(() => { hidden = false; document.dispatchEvent(new Event('visibilitychange')); });
  expect(clock.pending.size).toBe(2);
  expect(screen.getAllByText('5.0s')).toHaveLength(2);
  expect(first.view.loadTrace).toHaveBeenCalledTimes(1);
  expect(second.view.loadTrace).toHaveBeenCalledTimes(1);
  one.unmount();
  expect(clock.pending.size).toBe(1);
  two.unmount();
  expect(clock.pending.size).toBe(0);
});

it('respects OS and app reduced motion while continuing to show live evidence', async () => {
  const clock = animationClock();
  let reduced = true;
  const media = new EventTarget();
  vi.stubGlobal('matchMedia', () => ({
    get matches() { return reduced; },
    addEventListener: media.addEventListener.bind(media),
    removeEventListener: media.removeEventListener.bind(media),
  }));
  const f = fixture({ spans: { turn: live() } });
  const mounted = render(f.app());
  expect(clock.pending.size).toBe(0);
  clock.advance(1000);
  f.set({ spans: { turn: live(), call: { ...spans[1]!, endMs: base + 1000 } }, clockOffsetMs: 500 });
  mounted.rerender(f.app());
  expect(screen.getByText('duration').parentElement?.textContent).toBe('duration1.5s');
  expect(screen.getByText('$0.0040')).toBeDefined();
  expect(clock.pending.size).toBe(0);
  act(() => { reduced = false; media.dispatchEvent(new Event('change')); });
  expect(clock.pending.size).toBe(1);
  await act(async () => { document.documentElement.dataset.motion = 'reduce'; });
  expect(clock.pending.size).toBe(0);
  clock.advance(1000);
  f.set({ spans: { turn: span({ id: 'turn', endMs: base + 1800 }) } });
  mounted.rerender(f.app());
  expect(screen.getByRole('treeitem', { name: 'root · 1.8s' })).toBeDefined();
  await act(async () => { delete document.documentElement.dataset.motion; });
  expect(clock.pending.size).toBe(0);
});

it('reloads durable spans after reconnect and when another view replaces the same root', async () => {
  const f = fixture();
  const mounted = render(f.app());
  await waitFor(() => expect(f.view.loadTrace).toHaveBeenCalledTimes(1));
  mounted.rerender(f.app(false));
  expect(f.view.loadTrace).toHaveBeenCalledTimes(1);
  mounted.rerender(f.app());
  await waitFor(() => expect(f.view.loadTrace).toHaveBeenCalledTimes(2));
  mounted.rerender(f.app());
  expect(f.view.loadTrace).toHaveBeenCalledTimes(2);
  f.connection.info.connection_id = 'connection-2';
  mounted.rerender(f.app());
  await waitFor(() => expect(f.view.loadTrace).toHaveBeenCalledTimes(3));
  const other = fixture();
  mounted.rerender(other.app());
  await waitFor(() => expect(other.view.loadTrace).toHaveBeenCalledTimes(1));
});

it('distinguishes an empty historical trace from a failed read and offers retry', async () => {
  const f = fixture({ spans: {}, loaded: true });
  const mounted = render(f.app());
  expect(screen.getByText('No recorded spans')).toBeDefined();
  expect(screen.getByText(/Older conversations may predate tracing/)).toBeDefined();
  expect(screen.queryByText('Loading trace…')).toBeNull();
  f.set({ loaded: false, error: new Error('Request timed out: trace.page') });
  mounted.rerender(f.app());
  expect(screen.getByText('Trace unavailable')).toBeDefined();
  expect(screen.queryByText('No recorded spans')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
  await waitFor(() => expect(f.view.loadTrace).toHaveBeenCalledTimes(2));
  f.set({ loading: true, error: undefined });
  mounted.rerender(f.app());
  expect(screen.getByText('Loading trace…')).toBeDefined();
  f.set({ loading: false, loaded: true, spans: { turn: spans[0]! } });
  mounted.rerender(f.app());
  expect(screen.getByRole('treeitem', { name: 'root · 10.0s' })).toBeDefined();
});

it('offers bounded historical paging continuation without allowing duplicate or disconnected reads', () => {
  const f = fixture({ hasMore: true });
  const mounted = render(f.app());
  fireEvent.click(screen.getByRole('button', { name: 'Load more spans' }));
  expect(f.view.loadTrace).toHaveBeenCalledTimes(2);
  f.set({ loading: true });
  mounted.rerender(f.app());
  expect((screen.getByRole('button', { name: 'Loading trace…' }) as HTMLButtonElement).disabled).toBe(true);
  f.set({ loading: false });
  mounted.rerender(f.app(false));
  expect((screen.getByRole('button', { name: 'Load more spans' }) as HTMLButtonElement).disabled).toBe(true);
  f.set({ hasMore: false });
  mounted.rerender(f.app());
  expect(screen.queryByRole('button', { name: 'Load more spans' })).toBeNull();
});

it('resizes the shared tree column and details with keyboard, bounds, reset and pane toggles', () => {
  render(fixture().app());
  const tree = screen.getByRole('separator', { name: 'Resize tree and timeline' });
  fireEvent.keyDown(tree, { key: 'ArrowRight' });
  expect(tree.getAttribute('aria-valuenow')).toBe('328');
  expect(screen.getByText('Execution').style.width).toBe('328px');
  for (const row of screen.getAllByRole('treeitem')) expect((row.firstElementChild as HTMLElement).style.width).toBe('328px');
  fireEvent.keyDown(tree, { key: 'Home' });
  expect(tree.getAttribute('aria-valuenow')).toBe('160');
  fireEvent.keyDown(tree, { key: 'ArrowLeft' });
  expect(tree.getAttribute('aria-valuenow')).toBe('160');
  fireEvent.doubleClick(tree);
  expect(tree.getAttribute('aria-valuenow')).toBe('320');

  fireEvent.click(screen.getByRole('button', { name: 'Details' }));
  const details = screen.getByRole('separator', { name: 'Resize details' });
  fireEvent.keyDown(details, { key: 'ArrowLeft' });
  expect(screen.getByRole('complementary', { name: 'Span details' }).style.width).toBe('368px');
  fireEvent.keyDown(details, { key: 'End' });
  expect(details.getAttribute('aria-valuenow')).toBe(details.getAttribute('aria-valuemax'));
  expect(tree.getAttribute('aria-valuenow')).toBe('160');
  fireEvent.doubleClick(details);
  expect(details.getAttribute('aria-valuenow')).toBe('360');
  expect(tree.getAttribute('aria-valuenow')).toBe('320');
  fireEvent.keyDown(details, { key: 'ArrowLeft' });
  fireEvent.click(screen.getByRole('button', { name: 'Details' }));
  expect(screen.queryByRole('separator', { name: 'Resize details' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Details' }));
  expect(screen.getByRole('separator', { name: 'Resize details' }).getAttribute('aria-valuenow')).toBe('368');
  fireEvent.click(screen.getByRole('button', { name: 'Timeline' }));
  expect(screen.queryByRole('separator', { name: 'Resize tree and timeline' })).toBeNull();
  expect(screen.getByText('Execution').style.width).toBe('100%');
  fireEvent.click(screen.getByRole('button', { name: 'Timeline' }));
  expect(screen.getByRole('separator', { name: 'Resize tree and timeline' }).getAttribute('aria-valuenow')).toBe('320');
});

it('drags dividers with pointer capture and restores the starting width on cancellation', () => {
  vi.stubGlobal('PointerEvent', MouseEvent);
  render(fixture().app());
  const tree = screen.getByRole('separator', { name: 'Resize tree and timeline' });
  tree.setPointerCapture = vi.fn(); tree.hasPointerCapture = () => true; tree.releasePointerCapture = vi.fn();
  // Native pointer focus must remain available; forced focus shows the keyboard outline during drag.
  expect(fireEvent.pointerDown(tree, { button: 0, clientX: 320 })).toBe(true);
  fireEvent.pointerMove(tree, { clientX: 420 });
  expect(tree.getAttribute('aria-valuenow')).toBe('420');
  fireEvent.pointerCancel(tree);
  expect(tree.getAttribute('aria-valuenow')).toBe('320');
  fireEvent.pointerDown(tree, { button: 0, clientX: 320 });
  fireEvent.pointerMove(tree, { clientX: 400 });
  fireEvent.pointerUp(tree);
  fireEvent.pointerMove(tree, { clientX: 450 });
  expect(tree.getAttribute('aria-valuenow')).toBe('400');
  expect(tree.releasePointerCapture).toHaveBeenCalledOnce();
  fireEvent.click(screen.getByRole('button', { name: 'Details' }));
  const details = screen.getByRole('separator', { name: 'Resize details' });
  details.setPointerCapture = vi.fn();
  fireEvent.pointerDown(details, { button: 0, clientX: 640 });
  fireEvent.pointerMove(details, { clientX: 600 });
  expect(details.getAttribute('aria-valuenow')).toBe('400');
  fireEvent.lostPointerCapture(details);
  fireEvent.pointerMove(details, { clientX: 550 });
  expect(details.getAttribute('aria-valuenow')).toBe('400');
});

it('has no resize handles without span data', () => {
  render(fixture({ spans: {}, loaded: true }).app());
  fireEvent.click(screen.getByRole('button', { name: 'Details' }));
  expect(screen.queryByRole('separator')).toBeNull();
});
