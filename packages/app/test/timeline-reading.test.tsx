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

const harness = vi.hoisted(() => ({ scrolls: [] as number[] }));
vi.mock('@tanstack/react-virtual', () => {
  let options: {
    count: number;
    getScrollElement(): HTMLElement | null;
    getItemKey(index: number): string | number;
  };
  const virtual = {
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
    measureElement: () => {},
  };
  return {
    defaultRangeExtractor: () => [],
    useVirtualizer: (value: typeof options) => {
      options = value;
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
  ) => (
    <StrictMode>
      <RuntimeContext.Provider value={runtime}>
        <UIProvider>
          <Timeline
            key={key}
            rows={visibleRows}
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
  return { readingPositions, loadOlder, paging, report, app };
}
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
  fireEvent.scroll(root);
  expect(f.readingPositions.get('host:root:root')).toMatchObject({
    messageId: 'row-2',
    offset: 30,
    follow: false,
  });
  mounted.rerender(f.app('host:root:child'));
  const child = screen.getByRole('region', { name: 'Conversation' });
  child.scrollTop = 110;
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
  fireEvent.wheel(root);
  root.scrollTop = 220;
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
  fireEvent.scroll(root);
  expect(f.loadOlder).not.toHaveBeenCalled();
  root.scrollTop = 200;
  fireEvent.scroll(root);
  fireEvent.scroll(root);
  expect(f.loadOlder).toHaveBeenCalledTimes(1);
  expect(screen.getByRole('button', { name: /Load earlier messages$/ }).hasAttribute('disabled')).toBe(true);
  await act(async () => { resolve(); });
  expect(f.loadOlder).toHaveBeenCalledTimes(1);
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
  expect(f.report).toHaveBeenCalledWith(error);
  expect(older.hasAttribute('disabled')).toBe(false);
  await act(async () => { fireEvent.click(older); });
  expect(f.loadOlder).toHaveBeenCalledTimes(2);
  expect(older.hasAttribute('disabled')).toBe(false);
});
