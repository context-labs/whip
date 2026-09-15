import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import type { SessionView, SessionViewSnapshot, TraceEvidence, TraceSpan } from '@whip/sdk/state';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { TraceView } from '../src/trace-view';
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
afterEach(() => vi.unstubAllGlobals());

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
  const view = {
    session: { rootId: 'root' },
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
  return { app, set, view };
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
  fireEvent.click(screen.getByRole('button', { name: 'Raw' }));
  expect(screen.getByRole('region', { name: 'Raw span' }).textContent).toContain('"operation_id": "op-1"');
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
