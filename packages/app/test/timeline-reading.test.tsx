import { StrictMode } from 'react';
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { UIProvider } from '@whip/ui';
import { Timeline, type TimelineRow } from '../src/timeline';
import { ReadingPositions } from '../src/reading-positions';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';

const harness = vi.hoisted(() => ({ scrolls: [] as number[], virtual: undefined as undefined | {
  elementsCache: Map<string, HTMLElement>;
  shouldAdjustScrollPositionOnItemSizeChange?: (item: { index: number }) => boolean;
} }));
vi.mock('@tanstack/react-virtual', () => {
  let options: {
    count: number;
    getScrollElement(): HTMLElement | null;
    getItemKey(index: number): string | number;
  };
  const virtual = {
    elementsCache: new Map<string, HTMLElement>(),
    options: {} as Record<string, unknown>,
    // Row zero is the separately measured history control; this layout mock
    // leaves it at zero height and gives each message a fixed 100px height.
    getTotalSize: () => (options.count - 1) * 100,
    getOffsetForIndex: (index: number) => [Math.max(0, index - 1) * 100, 'start'],
    scrollToOffset: (offset: number) => {
      harness.scrolls.push(offset);
      const root = options.getScrollElement();
      if (root)
        root.scrollTop = Math.max(
          0,
          Math.min((options.count - 1) * 100 - 200, offset),
        );
    },
    getVirtualItems: () =>
      Array.from({ length: options.count }, (_, index) => ({
        key: options.getItemKey(index),
        index,
        start: Math.max(0, index - 1) * 100,
      })),
    measureElement: (element: HTMLElement | null) => {
      if (element?.dataset.readingId) virtual.elementsCache.set(element.dataset.readingId, element);
    },
  };
  return {
    defaultRangeExtractor: () => [],
    elementScroll: vi.fn(),
    useVirtualizer: (value: typeof options) => {
      options = value;
      virtual.options = value;
      harness.virtual = virtual;
      return virtual;
    },
  };
});
const rows: TimelineRow[] = Array.from({ length: 6 }, (_, seq) => ({
  id: `row-${seq}`,
  seq,
  role: 'user',
  text: `Message ${seq}`,
}));
beforeEach(() => {
  vi.useFakeTimers();
  harness.scrolls.length = 0;
  harness.virtual?.elementsCache.clear();
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) =>
    setTimeout(() => callback(0), 16),
  );
  vi.stubGlobal('cancelAnimationFrame', (id: number) => clearTimeout(id));
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(
    function (this: HTMLElement) {
      const root = this.closest<HTMLElement>('[aria-label="Conversation"]');
      const index = Number(this.dataset.index ?? 0) - 1;
      const top = this.dataset.readingId
        ? 50 + index * 100 - (root?.scrollTop ?? 0)
        : 50;
      const height = this.dataset.readingId ? 100 : 200;
      return {
        top,
        bottom: top + height,
        left: 0,
        right: 500,
        width: 500,
        height,
        x: 0,
        y: top,
        toJSON() {},
      };
    },
  );
  vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockReturnValue(600);
  vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockReturnValue(200);
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});
function fixture() {
  const readingPositions = new ReadingPositions();
  const loadOlder = vi.fn(async () => {});
  const paging = { hasMore: true, canLoadOlder: true, loadingHistory: false };
  const report = vi.fn();
  const runtime = {
    readingPositions,
    report,
    platform: { copy: vi.fn() },
  } as unknown as AppRuntime;
  const app = (
    key = 'host:root:root',
    revision = '1',
    ready = true,
    visibleRows = rows,
    footer?: React.ReactNode,
  ) => (
    <StrictMode>
      <RuntimeContext.Provider value={runtime}>
        <UIProvider>
          <Timeline
            key={key}
            rows={visibleRows}
            footer={footer}
            bookmarkKey={key}
            historyRevision={revision}
            historyReady={ready}
            {...paging}
            loadOlder={loadOlder}
            readBody={() => {}}
          />
        </UIProvider>
      </RuntimeContext.Provider>
    </StrictMode>
  );
  return { readingPositions, loadOlder, paging, report, app, runtime };
}
it('focusing a closed activity header does not open it; explicit disclosure still works', () => {
  const f = fixture();
  const group = { id: 'group', role: 'activity', text: '', cells: [], items: [{ id: 'thought', kind: 'reasoning', row: { id: 'thought', role: 'reasoning', text: 'Saved reasoning' } }], memberIds: ['thought'], memberSeqs: [] };
  render(f.app(undefined, undefined, true, [group]));
  const button = screen.getByRole('button', { name: 'Thought' });
  act(() => button.focus());
  expect(button.getAttribute('aria-expanded')).toBe('false');
  fireEvent.click(button);
  expect(button.getAttribute('aria-expanded')).toBe('true');
  fireEvent.click(button);
  act(() => vi.advanceTimersByTime(1000));
  expect(button.getAttribute('aria-expanded')).toBe('false');
});
it('preserves an explicit disclosure when regrouping retains the old group as an alias', () => {
  const f = fixture();
  const group = { id: 'activity:old', role: 'activity', text: '', cells: [], items: [{ id: 'thought', kind: 'reasoning', row: { id: 'thought', role: 'reasoning', text: 'Saved reasoning' } }], memberIds: ['thought'], memberSeqs: [] };
  const mounted = render(f.app(undefined, undefined, true, [group]));
  fireEvent.click(screen.getByRole('button', { name: 'Thought' }));
  const merged = { ...group, id: 'activity:merged', memberIds: ['thought', group.id] };
  mounted.rerender(f.app(undefined, undefined, true, [merged]));
  const header = mounted.container.querySelector('[data-activity-group] button')!;
  expect(header.getAttribute('aria-expanded')).toBe('true');
  fireEvent.click(header);
  act(() => vi.advanceTimersByTime(1000));
  mounted.rerender(f.app(undefined, undefined, true, [merged]));
  expect(screen.getByRole('button', { name: 'Thought' }).getAttribute('aria-expanded')).toBe('false');
});
it('ordinary clicks, Tab and Enter preserve following; an upward gesture stays detached near the bottom', () => {
  const f = fixture();
  const mounted = render(f.app());
  const root = screen.getByRole('region', { name: 'Conversation' });
  const message = screen.getByText('Message 5');
  fireEvent.pointerDown(message);
  fireEvent.pointerUp(message);
  act(() => vi.advanceTimersByTime(16));
  fireEvent.keyDown(root, { key: 'Tab' });
  fireEvent.keyDown(root, { key: 'Enter' });
  expect(screen.queryByRole('button', { name: 'Latest' })).toBeNull();
  fireEvent.wheel(root, { deltaY: -1 });
  root.scrollTop = 399;
  fireEvent.scroll(root);
  expect(screen.getByRole('button', { name: 'Latest' })).toBeTruthy();
  mounted.rerender(f.app(undefined, undefined, true, [...rows]));
  expect(root.scrollTop).toBe(399);
  expect(f.readingPositions.get('host:root:root')?.follow).toBe(false);
  fireEvent.wheel(root, { deltaY: 1 });
  root.scrollTop = 400;
  fireEvent.scroll(root);
  expect(screen.queryByRole('button', { name: 'Latest' })).toBeNull();
});
it('content movement cannot re-enable following or load history without a user scroll', () => {
  const f = fixture();
  render(f.app());
  const root = screen.getByRole('region', { name: 'Conversation' });
  fireEvent.wheel(root, { deltaY: -100 });
  root.scrollTop = 300;
  fireEvent.scroll(root);
  fireEvent(root, new Event('scrollend'));
  root.scrollTop = 0;
  fireEvent.scroll(root);
  expect(f.loadOlder).not.toHaveBeenCalled();
  root.scrollTop = 400;
  fireEvent.scroll(root);
  expect(screen.getByRole('button', { name: 'Latest' })).toBeTruthy();
  expect(f.readingPositions.get('host:root:root')?.follow).toBe(false);
});
it('keeps a pressed target stationary through growth, then resumes after the click', () => {
  const f = fixture();
  const mounted = render(f.app());
  const root = screen.getByRole('region', { name: 'Conversation' });
  const message = screen.getByText('Message 5');
  fireEvent.pointerDown(message);
  vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockReturnValue(800);
  mounted.rerender(f.app(undefined, undefined, true, [...rows]));
  expect(root.scrollTop).toBe(400);
  fireEvent.pointerUp(message);
  fireEvent.click(message);
  expect(root.scrollTop).toBe(400);
  act(() => vi.advanceTimersByTime(16));
  expect(root.scrollTop).toBe(600);
  expect(screen.queryByRole('button', { name: 'Latest' })).toBeNull();
});
it('waits for ready history and restores a saved anchor under StrictMode without following the tail', () => {
  const f = fixture();
  f.readingPositions.set('host:root:root', {
    messageId: 'row-1',
    seq: 1,
    revision: '1',
    offset: 25,
    follow: false,
  });
  const mounted = render(f.app(undefined, undefined, false));
  expect(harness.scrolls).toEqual([]);
  mounted.rerender(f.app());
  act(() => vi.advanceTimersByTime(160));
  expect(screen.getByRole('region', { name: 'Conversation' }).scrollTop).toBe(
    125,
  );
  expect(harness.scrolls).not.toContain(600);
  expect(f.loadOlder).not.toHaveBeenCalled();
  expect(screen.queryByText(/saved place/)).toBeNull();
});
it('captures only visible identity and restores separate root and child positions after remount', () => {
  const f = fixture();
  const mounted = render(f.app());
  const root = screen.getByRole('region', { name: 'Conversation' });
  root.scrollTop = 230;
  fireEvent.wheel(root, { deltaY: -100 });
  fireEvent.scroll(root);
  expect(f.readingPositions.get('host:root:root')).toMatchObject({
    messageId: 'row-2',
    offset: 30,
    follow: false,
  });
  mounted.rerender(f.app('host:root:child'));
  const child = screen.getByRole('region', { name: 'Conversation' });
  child.scrollTop = 110;
  fireEvent.wheel(child, { deltaY: -100 });
  fireEvent.scroll(child);
  mounted.rerender(f.app());
  act(() => vi.advanceTimersByTime(160));
  expect(screen.getByRole('region', { name: 'Conversation' }).scrollTop).toBe(
    230,
  );
  mounted.rerender(f.app('host:root:child'));
  act(() => vi.advanceTimersByTime(160));
  expect(screen.getByRole('region', { name: 'Conversation' }).scrollTop).toBe(
    110,
  );
});
it('shows explicit revision/eviction fallback without fetching unbounded older pages', () => {
  const f = fixture();
  f.readingPositions.set('host:root:root', {
    messageId: 'evicted',
    seq: 3,
    revision: '1',
    offset: 25,
    follow: false,
  });
  render(f.app(undefined, '2'));
  act(() => vi.advanceTimersByTime(160));
  expect(screen.getByRole('status').textContent).toContain('nearest loaded');
  expect(screen.getByRole('region', { name: 'Conversation' }).scrollTop).toBe(
    300,
  );
  expect(f.loadOlder).not.toHaveBeenCalled();
});
it('hands control back immediately when the reader scrolls during restoration', () => {
  const f = fixture();
  f.readingPositions.set('host:root:root', {
    messageId: 'row-1',
    seq: 1,
    revision: '1',
    offset: 25,
    follow: false,
  });
  render(f.app());
  const root = screen.getByRole('region', { name: 'Conversation' });
  fireEvent.wheel(root, { deltaY: -100 });
  root.scrollTop = 220;
  fireEvent.wheel(root, { deltaY: -100 });
  fireEvent.scroll(root);
  const calls = harness.scrolls.length;
  act(() => vi.advanceTimersByTime(160));
  expect(root.scrollTop).toBe(220);
  expect(harness.scrolls).toHaveLength(calls);
  expect(f.readingPositions.get('host:root:root')?.messageId).toBe('row-2');
});

it('preserves a bookmark while the reader has no rows and restores when evidence arrives', () => {
  const f = fixture();
  f.readingPositions.set('host:view:root:repl', {
    messageId: 'row-2', seq: 2, revision: '1', offset: 15, follow: false,
  });
  const mounted = render(f.app('host:view:root:repl', '1', true, []));
  mounted.rerender(f.app('host:view:root:repl'));
  act(() => vi.advanceTimersByTime(160));
  expect(screen.getByRole('region', { name: 'Conversation' }).scrollTop).toBe(215);
  expect(f.loadOlder).not.toHaveBeenCalled();
});

it('loads on a near-top scroll, shares a pending read, and permits another after completion', async () => {
  const f = fixture();
  let resolve!: () => void;
  f.loadOlder.mockImplementationOnce(() => new Promise(done => { resolve = done; }));
  render(f.app());
  const root = screen.getByRole('region', { name: 'Conversation' });
  expect(f.loadOlder).not.toHaveBeenCalled();
  root.scrollTop = 300;
  fireEvent.wheel(root, { deltaY: -100 });
  fireEvent.scroll(root);
  expect(f.loadOlder).not.toHaveBeenCalled();
  root.scrollTop = 200;
  fireEvent.wheel(root, { deltaY: -100 });
  fireEvent.scroll(root);
  fireEvent.wheel(root, { deltaY: -100 });
  fireEvent.scroll(root);
  expect(f.loadOlder).toHaveBeenCalledTimes(1);
  expect(screen.getByRole('button', { name: /Load earlier messages$/ }).hasAttribute('disabled')).toBe(true);
  await act(async () => { resolve(); });
  expect(f.loadOlder).toHaveBeenCalledTimes(1);
  fireEvent.wheel(root, { deltaY: -100 });
  fireEvent.scroll(root);
  expect(f.loadOlder).toHaveBeenCalledTimes(2);
  await act(async () => {});
});

it.each(['hasMore', 'canLoadOlder', 'loadingHistory', 'historyReady', 'restoring'] as const)(
  'does not page while %s prevents reading', condition => {
    const f = fixture();
    if (condition === 'hasMore' || condition === 'canLoadOlder') f.paging[condition] = false;
    if (condition === 'loadingHistory') f.paging.loadingHistory = true;
    if (condition === 'restoring') f.readingPositions.set('host:root:root', {
      messageId: 'row-1', seq: 1, revision: '1', offset: 25, follow: false,
    });
    render(f.app(undefined, undefined, condition !== 'historyReady'));
    const root = screen.getByRole('region', { name: 'Conversation' });
    root.scrollTop = 200;
    if (condition !== 'restoring') fireEvent.wheel(root, { deltaY: -100 });
    fireEvent.scroll(root);
    expect(f.loadOlder).not.toHaveBeenCalled();
    if (condition !== 'restoring') {
      const button = screen.queryByRole('button', { name: 'Load earlier messages' });
      if (button) fireEvent.click(button);
      expect(f.loadOlder).not.toHaveBeenCalled();
    }
  },
);

it('keeps an explicit fallback for a page with no visible rows and permits retry after failure', async () => {
  const f = fixture();
  const error = new Error('History unavailable');
  f.loadOlder.mockRejectedValueOnce(error);
  render(f.app(undefined, undefined, true, []));
  const older = screen.getByRole('button', { name: 'Load earlier messages' });
  expect(f.loadOlder).not.toHaveBeenCalled();
  await act(async () => { fireEvent.click(older); });
  expect(f.report).not.toHaveBeenCalled();
  expect(older.hasAttribute('disabled')).toBe(false);
  await act(async () => { fireEvent.click(older); });
  expect(f.loadOlder).toHaveBeenCalledTimes(2);
  expect(older.hasAttribute('disabled')).toBe(false);
});

it('keeps turn feedback after history inside the same reading viewport', () => {
  const f = fixture();
  render(f.app(undefined, undefined, true, rows, <div data-error-type="turn">This turn failed</div>));
  const notice = screen.getByText('This turn failed');
  const viewport = screen.getByRole('region', { name: 'Conversation' });
  expect(viewport.contains(notice)).toBe(true);
  const last = viewport.querySelectorAll('[data-reading-id]');
  expect(last[last.length - 1]!.compareDocumentPosition(notice) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
});

it('keeps clipboard failures on the affected message without reporting globally', async () => {
  const f = fixture();
  vi.mocked(f.runtime.platform.copy).mockRejectedValueOnce(new Error('Clipboard unavailable'));
  render(f.app(undefined, undefined, true, [{ id: 'copy-row', role: 'user', text: 'Keep this text', live: false }]));
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Copy message', exact: true })); });
  const alert = screen.getByRole('alert');
  expect(alert.closest('[data-message-id]')?.getAttribute('data-message-id')).toBe('copy-row');
  expect(alert.closest('[data-error-owner]')?.getAttribute('data-error-owner')).toBe('copy:copy-row');
  expect(f.report).not.toHaveBeenCalled();
});

it('replacing a focused gap control keeps keyboard focus at recovered content without loading older history', async () => {
  const f = fixture();
  const missing: TimelineRow = { id: 'history-gap:1:3', role: 'history-gap', text: '', seq: 2, historyGap: { fromSeq: 2, toSeq: 3, status: 'error' } };
  const mounted = render(f.app(undefined, undefined, true, [rows[0]!, rows[1]!, missing, rows[4]!, rows[5]!]));
  act(() => screen.getByRole('button', { name: 'Retry', exact: true }).focus());
  mounted.rerender(f.app(undefined, undefined, true, rows));
  await act(async () => { vi.advanceTimersByTime(32); });
  expect(document.activeElement?.getAttribute('data-reading-id')).toBe('row-2');
  expect(f.loadOlder).not.toHaveBeenCalled();
});

it('Latest loads an evicted suffix and an upward gesture cancels the pending jump', async () => {
  const f = fixture();
  let resolve!: () => void;
  const loadLatest = vi.fn(() => new Promise<void>(done => { resolve = done; }));
  const app = () => <RuntimeContext.Provider value={f.runtime}><UIProvider><Timeline rows={rows} hasMore={false} loadOlder={f.loadOlder}
    latestMissing loadLatest={loadLatest} readBody={() => {}} /></UIProvider></RuntimeContext.Provider>;
  render(app());
  const root = screen.getByRole('region', { name: 'Conversation' });
  fireEvent.click(screen.getByRole('button', { name: 'Latest' }));
  expect(loadLatest).toHaveBeenCalledTimes(1);
  fireEvent.wheel(root, { deltaY: -20 }); root.scrollTop = 240;
  await act(async () => { resolve(); });
  await act(async () => { vi.advanceTimersByTime(32); });
  expect(root.scrollTop).toBe(240);
  expect(screen.getByRole('button', { name: 'Latest' })).toBeTruthy();
  expect(f.loadOlder).not.toHaveBeenCalled();
});

it('anchors visible focused content when a preceding stored message or image grows', () => {
  const f = fixture(); render(f.app());
  const root = screen.getByRole('region', { name: 'Conversation' });
  fireEvent.wheel(root, { deltaY: -1 }); root.scrollTop = 200; fireEvent.scroll(root);
  const row = screen.getByText('Message 2').closest<HTMLElement>('[data-reading-id]')!;
  act(() => row.focus());
  expect(harness.virtual?.shouldAdjustScrollPositionOnItemSizeChange?.({ index: 2 })).toBe(true);
  expect(harness.virtual?.shouldAdjustScrollPositionOnItemSizeChange?.({ index: 3 })).toBe(false);
  expect(harness.virtual?.shouldAdjustScrollPositionOnItemSizeChange?.({ index: 4 })).toBe(false);
  expect(screen.getByRole('button', { name: 'Latest' })).toBeTruthy();
  fireEvent.wheel(root, { deltaY: -20 });
  expect(harness.virtual?.shouldAdjustScrollPositionOnItemSizeChange).toBeUndefined();
});
