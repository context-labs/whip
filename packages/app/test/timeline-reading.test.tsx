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
    getItemKey(index: number): string;
  };
  const virtual = {
    getTotalSize: () => options.count * 100,
    getOffsetForIndex: (index: number) => [index * 100, 'start'],
    scrollToOffset: (offset: number) => {
      harness.scrolls.push(offset);
      const root = options.getScrollElement();
      if (root)
        root.scrollTop = Math.max(
          0,
          Math.min(options.count * 100 - 200, offset),
        );
    },
    getVirtualItems: () =>
      Array.from({ length: options.count }, (_, index) => ({
        key: options.getItemKey(index),
        index,
        start: index * 100,
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
      const index = Number(this.parentElement?.dataset.index ?? 0);
      const top = this.dataset.messageId
        ? 50 + index * 100 - (root?.scrollTop ?? 0)
        : 50;
      const height = this.dataset.messageId ? 100 : 200;
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
  const runtime = {
    readingPositions,
    report: vi.fn(),
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
            hasMore
            loadOlder={loadOlder}
            readBody={() => {}}
          />
        </UIProvider>
      </RuntimeContext.Provider>
    </StrictMode>
  );
  return { readingPositions, loadOlder, app };
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
