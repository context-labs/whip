import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import userEvent from '@testing-library/user-event';
import { webcrypto } from 'node:crypto';
import { Client, DeliveryError, type ContentReference } from '@whip/sdk';
import type { TracePageParams } from '@whip/protocol';
import { createTraceView, type TraceRow, type TraceView as TraceObserver } from '@whip/sdk/state';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { TraceView } from '../src/trace-view';
import * as traceMath from '../src/trace-math';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';

vi.mock('@tanstack/react-virtual', () => ({
  useVirtualizer: ({
    count,
    getItemKey,
  }: {
    count: number;
    getItemKey(index: number): string;
  }) => ({
    getTotalSize: () => count * 28,
    getVirtualItems: () =>
      Array.from({ length: count }, (_, index) => ({
        index,
        key: getItemKey(index),
        start: index * 28,
        size: 28,
      })),
  }),
}));
const observers = new Set<TraceObserver>();
beforeEach(() => {
  vi.stubGlobal('matchMedia', () => ({
    matches: false,
    addEventListener() {},
    removeEventListener() {},
  }));
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  vi.stubGlobal('crypto', webcrypto);
});
afterEach(async () => {
  await Promise.all([...observers].map((view) => view.dispose()));
  observers.clear();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  delete document.documentElement.dataset.motion;
});
const base = 1_790_600_000_000_000_001n;
const sequence = 9007199254740993n;
const ns = (ms: number) => String(base + BigInt(ms) * 1000000n);
const hex = (value: number, length = 16) => value.toString(16).padStart(length, '0');
const attribute = (key: string, value: string | bigint | boolean) =>
  typeof value === 'string'
    ? { key, text: value, count: null, flag: null }
    : typeof value === 'bigint'
      ? { key, text: null, count: String(value), flag: null }
      : { key, text: null, count: null, flag: value };
function row(
  id: number,
  parent: number | null,
  kind: NonNullable<TraceRow['span']>['kind'],
  name: string,
  start: number,
  end: number | null,
  attrs: Record<string, string | bigint | boolean> = {},
): TraceRow {
  return {
    sequence: String(sequence + BigInt(id)),
    root_id: 'root',
    session_id: 'root',
    turn_id: 'turn_1',
    source_kind:
      kind === 'agent'
        ? 'turn'
        : kind === 'llm'
          ? 'attempt'
          : kind === 'host'
            ? 'operation'
            : 'cell',
    source_id: 'source_' + id,
    span_id: hex(id),
    span: {
      trace_id: hex(1, 32),
      parent_span_id: parent === null ? null : hex(parent),
      kind,
      name,
      state: end === null ? 'running' : 'succeeded',
      start_ns: ns(start),
      end_ns: end === null ? null : ns(end),
      attributes: Object.entries(attrs).map(([key, value]) => attribute(key, value)),
    },
  };
}
function records() {
  return [
    row(1, null, 'agent', 'turn.prompt', 0, 10000),
    row(2, 1, 'llm', 'turn kimi-k3-fast', 100, 2100, {
      'gen_ai.request.model': 'kimi-k3-fast',
      'gen_ai.operation.name': 'turn',
      'gen_ai.usage.input_tokens': 1200n,
      'gen_ai.usage.output_tokens': 30n,
      'whip.cost.nano_usd': 4000000n,
      'whip.cost.source': 'estimated',
      'whip.input.body_available': false,
      'whip.request.digest': 'captured-digest',
    }),
    row(3, 1, 'tool', 'execute', 2200, 2600),
    row(4, 3, 'host', 'files.read', 2250, 2291, {
      'whip.arguments.preview': '{"path":"README.md"}',
      'whip.arguments.truncated': false,
      'whip.result.preview': '# Whip',
    }),
    row(5, 1, 'llm', 'turn kimi-k3-fast', 3000, null, { 'gen_ai.request.model': 'kimi-k3-fast' }),
  ];
}
async function fixture(
  options: { rows?: TraceRow[]; maxRows?: number; maxRoots?: number; start?: boolean } = {},
) {
  const state = {
    rows: options.rows ?? records(),
    fail: false,
    lostExport: false,
    foreignExport: false,
    epoch: 'boot',
    now: ns(3100),
  };
  const calls: { method: string; params: unknown }[] = [];
  const data = new TextEncoder().encode('{"resourceSpans":[]}');
  const digest = Buffer.from(await webcrypto.subtle.digest('SHA-256', data)).toString('hex');
  const reference = (): ContentReference => ({
    id: 'trace_export',
    session_id: state.foreignExport ? 'foreign' : 'root',
    size: String(data.length),
    digest,
    media_type: 'application/json',
    created_at: '2026-09-28T12:00:00Z',
  });
  const client = await Client.connect(
    async (request) => {
      if (request.method === 'initialize')
        return {
          jsonrpc: '2.0',
          id: request.id,
          result: {
            major: 4,
            minor: 0,
            runtime_id: 'runtime',
            process_epoch: state.epoch,
            network_client: false,
            builtins: [],
          },
        };
      calls.push({ method: request.method, params: request.params });
      const revision = state.rows
        .reduce((head, item) => (BigInt(item.sequence) > head ? BigInt(item.sequence) : head), 0n)
        .toString();
      let result: unknown;
      if (request.method === 'trace.page') {
        if (state.fail)
          return {
            jsonrpc: '2.0',
            id: request.id,
            error: { code: -32001, kind: 'CONFLICT', message: 'Trace changed during read' },
          };
        const p = request.params as TracePageParams;
        if (!("before" in p)) throw new Error("Trace observer must use backward paging");
        const all = state.rows
          .filter(
            (item) =>
              (p.before === null || BigInt(item.sequence) < BigInt(p.before!)) &&
              (!item.span ||
                ((!p.trace_id || p.trace_id === item.span.trace_id) &&
                  (!p.roots_only || item.span.parent_span_id === null))),
          )
          .sort((a, b) => (BigInt(a.sequence) > BigInt(b.sequence) ? -1 : 1));
        const items = all.slice(0, p.limit),
          has_more = all.length > items.length;
        result = {
          items: structuredClone(items),
          observed_at_ns: state.now,
          revision,
          next: has_more ? items.at(-1)!.sequence : '0',
          has_more,
        };
      } else if (request.method === 'trace.export') {
        if (state.lostExport) throw new DeliveryError('Trace export acknowledgement was lost');
        result = { reference: reference(), revision, spans: state.rows.length, traces: 1 };
      } else if (request.method === 'content.read')
        result = { reference: reference(), data_base64: Buffer.from(data).toString('base64') };
      else throw new Error(request.method);
      return { jsonrpc: '2.0', id: request.id, result };
    },
    { clientID: 'test' },
  );
  const view = createTraceView(client, 'root', {
    pollIntervalMs: 60000,
    maxRows: options.maxRows,
    maxRoots: options.maxRoots,
  });
  observers.add(view);
  if (options.start !== false) await view.start();
  const download = vi.fn(async (_data: Uint8Array, _name: string, _media: string) => {});
  const runtime = {
    platform: { copy: vi.fn(async () => {}), download },
    report: vi.fn(),
  } as unknown as AppRuntime;
  const app = (connected = true) => (
    <RuntimeContext.Provider value={runtime}>
      <UIProvider>
        <ThemeProvider initialTheme="claude-code">
          <TraceView view={view} client={client} viewId="view" connected={connected} />
        </ThemeProvider>
      </UIProvider>
    </RuntimeContext.Provider>
  );
  return { app, state, view, calls, download, data };
}

it('renders native hierarchy and exact measured durations without opening another observer', async () => {
  const f = await fixture();
  const reads = f.calls.length;
  render(f.app());
  const items = screen.getAllByRole('treeitem');
  expect(items.map((item) => item.getAttribute('aria-label'))).toEqual([
    'turn.prompt · 10.0s',
    'kimi-k3-fast · 2.0s',
    'execute · 400ms',
    'files.read · 41ms',
    'kimi-k3-fast · running',
  ]);
  expect(items[3]!.getAttribute('aria-level')).toBe('3');
  expect(f.calls.length).toBe(reads);
  const totals = screen.getByLabelText('Loaded span totals').textContent!;
  expect(totals).toContain('spans5');
  expect(totals).toContain('$0.0040 + unknown');
  expect(totals).toContain('1,230 + unknown');
  fireEvent.click(screen.getByRole('treeitem', { name: 'kimi-k3-fast · 2.0s' }));
  expect(screen.getByText(/Historical model request bodies were not retained/)).toBeDefined();
  fireEvent.click(screen.getByRole('button', { name: 'Raw' }));
  const raw = screen.getByRole('region', { name: 'Raw span' }).textContent!;
  expect(raw).toContain('1790600000100000001');
  expect(raw).toContain('9007199254740995');
  expect(raw).toContain('captured-digest');
});

it('keeps pane controls keyboard accessible and preserves collapse, exact source details, and raw copying', async () => {
  render((await fixture()).app());
  const user = userEvent.setup();
  const panes = within(screen.getByRole('group', { name: 'Panes' }));
  panes.getByRole('button', { name: 'Tree' }).focus();
  await user.keyboard('{ArrowRight}{ArrowRight}{Enter}');
  expect(panes.getAllByRole('button', { pressed: true })).toHaveLength(3);
  fireEvent.click(screen.getAllByRole('button', { name: 'Collapse', hidden: true })[1]!);
  expect(screen.queryByRole('treeitem', { name: 'files.read · 41ms' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Expand', hidden: true }));
  fireEvent.click(screen.getByRole('treeitem', { name: 'files.read · 41ms' }));
  const details = screen.getByRole('complementary', { name: 'Span details' });
  expect(details.textContent).toContain('operation · source_4');
  expect(details.textContent).toContain('41ms');
  expect(screen.getByRole('region', { name: 'Arguments preview' }).textContent).toContain(
    'README.md',
  );
  expect(screen.getByRole('region', { name: 'Result preview' }).textContent).toContain('# Whip');
  fireEvent.click(screen.getByRole('button', { name: 'Raw' }));
  expect(screen.getByRole('region', { name: 'Raw span' }).textContent).toContain(
    '"source_id": "source_4"',
  );
  expect(screen.getAllByRole('button', { name: /Copy/ }).length).toBeGreaterThan(0);
});

it('keeps selected trace identity when it moves outside the root picker and clears it only explicitly', async () => {
  const older = row(1, null, 'agent', 'older', 0, 10000);
  const newer = row(10, null, 'agent', 'newer', 20000, 21000);
  newer.span!.trace_id = hex(2, 32);
  const newest = row(20, null, 'agent', 'newest', 30000, 31000);
  newest.span!.trace_id = hex(3, 32);
  const f = await fixture({ rows: [older, newer], maxRoots: 2 });
  render(f.app());
  const user = userEvent.setup();
  await user.click(screen.getByRole('combobox', { name: 'Trace' }));
  await user.click(await screen.findByRole('option', { name: 'older · turn_1' }));
  await waitFor(() => expect(f.view.getSnapshot().traceID).toBe(hex(1, 32)));
  f.state.rows.push(newest);
  await act(() => f.view.refresh());
  expect(screen.getByRole('combobox', { name: 'Trace' }).textContent).toContain('Selected trace');
  expect(screen.getByRole('treeitem', { name: 'older · 10.0s' })).toBeDefined();
  await user.click(screen.getByRole('combobox', { name: 'Trace' }));
  await user.click(await screen.findByRole('option', { name: 'All traces · loaded window' }));
  await waitFor(() => expect(screen.getAllByRole('treeitem')).toHaveLength(3));
});

it('uses explicit replacement pages, preserves an older window during refresh, and disables disconnected actions', async () => {
  const f = await fixture({ maxRows: 2 });
  const mounted = render(f.app());
  fireEvent.click(screen.getByRole('button', { name: 'Older spans' }));
  await waitFor(() => expect(f.view.getSnapshot().latestMissing).toBe(true));
  const cursor = f.view.getSnapshot().windowBefore;
  fireEvent.click(screen.getByRole('button', { name: 'Refresh trace' }));
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Refresh trace' }).hasAttribute('disabled')).toBe(
      false,
    ),
  );
  expect(f.view.getSnapshot().windowBefore).toBe(cursor);
  expect(screen.getByText(/Viewing an older window/)).toBeDefined();
  mounted.rerender(f.app(false));
  expect(screen.getByRole('button', { name: 'Latest spans' }).hasAttribute('disabled')).toBe(true);
  expect(screen.getByRole('button', { name: 'Export trace' }).hasAttribute('disabled')).toBe(true);
  mounted.rerender(f.app());
  fireEvent.click(screen.getByRole('button', { name: 'Latest spans' }));
  await waitFor(() => expect(f.view.getSnapshot().latestMissing).toBe(false));
});

it('renders loading, unavailable, empty and stale snapshots without inventing evidence', async () => {
  const f = await fixture({ rows: [], start: false });
  render(f.app());
  expect(screen.getByText('Loading trace…')).toBeDefined();
  await act(() => f.view.start());
  expect(screen.getByText('No recorded spans')).toBeDefined();
  f.state.fail = true;
  await act(() => f.view.refresh());
  expect(screen.getByText('Trace unavailable')).toBeDefined();
  expect(screen.getByRole('button', { name: 'Retry' })).toBeDefined();
  f.state.fail = false;
  f.state.rows = records();
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
  await waitFor(() => expect(screen.getAllByRole('treeitem')).toHaveLength(5));
  f.state.fail = true;
  await act(() => f.view.refresh());
  expect(screen.getAllByRole('treeitem')).toHaveLength(5);
});

it('exports only by explicit action, pins the visible revision and reads owned verified bytes', async () => {
  const f = await fixture();
  render(f.app());
  expect(f.calls.every((call) => call.method === 'trace.page')).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: 'Export trace' }));
  await waitFor(() => expect(f.download).toHaveBeenCalledOnce());
  expect(f.calls.find((call) => call.method === 'trace.export')!.params).toEqual({
    root_id: 'root',
    trace_id: '',
    expected_revision: f.view.getSnapshot().revision,
  });
  expect([...f.download.mock.calls[0]![0]]).toEqual([...f.data]);
  expect(f.download.mock.calls[0]![1]).toBe('whip-trace.json');
});

it('does not replay uncertain export or read a foreign content reference', async () => {
  const f = await fixture();
  f.state.lostExport = true;
  render(f.app());
  fireEvent.click(screen.getByRole('button', { name: 'Export trace' }));
  await screen.findByText('Trace export acknowledgement was lost');
  expect(f.calls.filter((call) => call.method === 'trace.export')).toHaveLength(1);
  expect(f.download).not.toHaveBeenCalled();
  f.state.lostExport = false;
  f.state.foreignExport = true;
  fireEvent.click(screen.getByRole('button', { name: 'Export trace' }));
  await screen.findByText('Content belongs to another session');
  expect(f.calls.some((call) => call.method === 'content.read')).toBe(false);
  expect(f.download).not.toHaveBeenCalled();
});

it('distinguishes zero-duration and skewed closed spans from open work', async () => {
  const zero = row(1, null, 'agent', 'zero', 0, 0),
    skew = row(2, null, 'agent', 'skew', 100, 99),
    future = row(3, null, 'agent', 'future', 5000, null);
  const f = await fixture({ rows: [zero, skew, future] });
  render(f.app());
  expect(screen.getByRole('treeitem', { name: 'zero · <1ms' })).toBeDefined();
  expect(screen.getByRole('treeitem', { name: 'skew · Timing unavailable' })).toBeDefined();
  fireEvent.click(screen.getByRole('treeitem', { name: 'future · running' }));
  expect(screen.getByText('running · clock mismatch')).toBeDefined();
});

function animationClock() {
  let time = 0,
    id = 0;
  const pending = new Map<number, FrameRequestCallback>();
  vi.spyOn(performance, 'now').mockImplementation(() => time);
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
    pending.set(++id, callback);
    return id;
  });
  vi.stubGlobal('cancelAnimationFrame', (key: number) => pending.delete(key));
  return {
    pending,
    advance(ms: number) {
      act(() => {
        time += ms;
        const callbacks = [...pending.values()];
        pending.clear();
        callbacks.forEach((callback) => callback(time));
      });
    },
  };
}
it('animates from host time with monotonic elapsed, reduced motion, hidden panes and joined unmount', async () => {
  const clock = animationClock();
  const f = await fixture({ rows: [row(1, null, 'agent', 'live', 3000, null)] });
  const mounted = render(f.app());
  fireEvent.click(screen.getByRole('treeitem', { name: 'live · running' }));
  expect(screen.getByText('running · 100ms')).toBeDefined();
  const reads = f.calls.length;
  const totals = vi.spyOn(traceMath, 'traceTotals');
  for (let i = 0; i < 100; i++) clock.advance(10);
  expect(screen.getByText('running · 1.1s')).toBeDefined();
  expect(f.calls.length).toBe(reads);
  expect(totals).not.toHaveBeenCalled();
  await act(async () => {
    document.documentElement.dataset.motion = 'reduce';
  });
  expect(clock.pending.size).toBe(0);
  await act(async () => {
    delete document.documentElement.dataset.motion;
  });
  expect(clock.pending.size).toBe(1);
  let hidden = true;
  vi.spyOn(document, 'hidden', 'get').mockImplementation(() => hidden);
  act(() => document.dispatchEvent(new Event('visibilitychange')));
  expect(clock.pending.size).toBe(0);
  clock.advance(5000);
  hidden = false;
  act(() => document.dispatchEvent(new Event('visibilitychange')));
  expect(clock.pending.size).toBe(1);
  expect(screen.getByText('running · 6.1s')).toBeDefined();
  mounted.rerender(f.app(false));
  expect(clock.pending.size).toBe(0);
  mounted.unmount();
  expect(clock.pending.size).toBe(0);
});

it('preserves wheel zoom, divider keyboard bounds, pane sizes and drag cancellation', async () => {
  vi.stubGlobal('PointerEvent', MouseEvent);
  const mounted = render((await fixture()).app());
  const list = screen.getByRole('tree', { name: 'Trace spans' });
  const wheel = (init: WheelEventInit) =>
    fireEvent(list, new WheelEvent('wheel', { bubbles: true, cancelable: true, ...init }));
  expect(wheel({ deltaY: 40 })).toBe(true);
  expect(wheel({ ctrlKey: true, deltaY: -40, clientX: 500 })).toBe(false);
  expect(wheel({ deltaX: 40, deltaY: 1 })).toBe(false);
  const tree = screen.getByRole('separator', { name: 'Resize tree and timeline' });
  fireEvent.keyDown(tree, { key: 'ArrowRight' });
  expect(tree.getAttribute('aria-valuenow')).toBe('328');
  fireEvent.keyDown(tree, { key: 'Home' });
  expect(tree.getAttribute('aria-valuenow')).toBe('160');
  fireEvent.doubleClick(tree);
  expect(tree.getAttribute('aria-valuenow')).toBe('320');
  tree.setPointerCapture = vi.fn();
  tree.hasPointerCapture = () => true;
  tree.releasePointerCapture = vi.fn();
  fireEvent.pointerDown(tree, { button: 0, clientX: 320 });
  fireEvent.pointerMove(tree, { clientX: 420 });
  expect(tree.getAttribute('aria-valuenow')).toBe('420');
  fireEvent.pointerCancel(tree);
  expect(tree.getAttribute('aria-valuenow')).toBe('320');
  fireEvent.click(screen.getByRole('button', { name: 'Details' }));
  const details = screen.getByRole('separator', { name: 'Resize details' });
  fireEvent.keyDown(details, { key: 'ArrowLeft' });
  expect(details.getAttribute('aria-valuenow')).toBe('368');
  fireEvent.click(screen.getByRole('button', { name: 'Details' }));
  fireEvent.click(screen.getByRole('button', { name: 'Details' }));
  expect(
    screen.getByRole('separator', { name: 'Resize details' }).getAttribute('aria-valuenow'),
  ).toBe('368');
  fireEvent.click(screen.getByRole('button', { name: 'Timeline' }));
  expect(wheel({ ctrlKey: true, deltaY: -40 })).toBe(true);
  expect(screen.queryByRole('separator', { name: 'Resize tree and timeline' })).toBeNull();
  mounted.unmount();
  expect(wheel({ ctrlKey: true, deltaY: -40 })).toBe(true);
});
