import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { defaultRangeExtractor, useVirtualizer } from '@tanstack/react-virtual';
import { Button } from '@whip/ui';
import { ArrowDown } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { scale } from '@whip/ui/tokens.stylex';
import { useRuntime } from './context';
import { readingTarget } from './reading-positions';
import { layout } from './styles';

const styles = stylex.create({
  viewport: { flex: 1, overflowY: 'auto', minHeight: 0, overflowAnchor: 'none' },
  inner: { maxWidth: 840, marginInline: 'auto', paddingInline: { default: 32, [scale.phone]: 16 }, paddingBlock: 24 },
  jump: { position: 'absolute', bottom: 16, left: '50%', transform: 'translateX(-50%)', boxShadow: '0 2px 8px rgb(0 0 0 / 0.08)' },
  region: { flex: 1, position: 'relative', display: 'flex', flexDirection: 'column', minHeight: 0 },
  exhausted: { visibility: 'hidden' },
});

/** Shared virtual reading/selection anchors; session data stays with the SDK. */
export function ReadingList<Row extends { id: string; seq?: number }>({
  rows, hasMore, loadOlder, bookmarkKey, historyRevision, historyReady = true,
  label, earlierLabel, renderRow, contentStyle, empty, canLoadOlder = true, loadingHistory = false,
}: {
  rows: readonly Row[];
  hasMore: boolean;
  loadOlder(): Promise<void>;
  bookmarkKey?: string;
  historyRevision?: string;
  historyReady?: boolean;
  label: string;
  earlierLabel: string;
  renderRow(row: Row, index: number): ReactNode;
  contentStyle?: stylex.StyleXStyles;
  empty?: ReactNode;
  canLoadOlder?: boolean;
  loadingHistory?: boolean;
}) {
  const runtime = useRuntime();
  const viewport = useRef<HTMLDivElement>(null);
  const saved = useRef(
    bookmarkKey ? runtime.readingPositions.get(bookmarkKey) : undefined,
  );
  const follow = useRef(saved.current?.follow ?? true);
  const [atEnd, setAtEnd] = useState(follow.current);
  const [loading, setLoading] = useState(false);
  const loadingRef = useRef(false);
  const [pinned, setPinned] = useState<string[]>([]);
  const [positionNotice, setPositionNotice] = useState('');
  const appliedRevision = useRef<string | undefined>(undefined);
  const restoring = useRef(false);
  const restoreFrame = useRef(0);
  const virtual = useVirtualizer({
    count: rows.length + 1,
    getScrollElement: () => viewport.current,
    estimateSize: (index) => (index === 0 ? 0 : 140),
    overscan: 8,
    // Keep the control's measured space after exhaustion. The virtualizer skips
    // resize compensation while scrolling backward, so removing it shifts rows.
    getItemKey: (index) => (index === 0 ? 0 : rows[index - 1]!.id),
    anchorTo: 'end',
    scrollEndThreshold: 64,
    rangeExtractor: (range) => {
      const indices = [0, ...defaultRangeExtractor(range)];
      const selected = pinned
        .map((id) => rows.findIndex((row) => row.id === id))
        .filter((index) => index >= 0)
        .map((index) => index + 1);
      if (selected.length)
        for (
          let index = Math.min(...selected);
          index <= Math.max(...selected);
          index++
        )
          indices.push(index);
      return [...new Set(indices)].sort((left, right) => left - right);
    },
  });
  const total = virtual.getTotalSize();
  const savePosition = useRef(() => {});
  savePosition.current = () => {
    const root = viewport.current;
    if (
      !root ||
      !bookmarkKey ||
      !historyRevision ||
      !historyReady ||
      restoring.current ||
      appliedRevision.current !== historyRevision
    )
      return;
    const top = root.getBoundingClientRect().top;
    const element = [
      ...root.querySelectorAll<HTMLElement>('[data-reading-id]'),
    ].find((row) => row.getBoundingClientRect().bottom > top);
    const row = rows.find((row) => row.id === element?.dataset.readingId);
    if (!row || !element) return;
    runtime.readingPositions.set(bookmarkKey, {
      messageId: row.id,
      revision: historyRevision,
      seq: row.seq,
      offset: top - element.getBoundingClientRect().top,
      follow: follow.current,
    });
  };
  const stopRestore = () => {
    cancelAnimationFrame(restoreFrame.current);
    restoring.current = false;
  };
  const canReadHistory = hasMore && canLoadOlder && historyReady && !loadingHistory;
  const loadEarlier = async () => {
    if (!canReadHistory || loadingRef.current || restoring.current) return;
    loadingRef.current = true;
    setLoading(true);
    try {
      await loadOlder();
    } catch (error) {
      runtime.report(error);
    } finally {
      loadingRef.current = false;
      setLoading(false);
    }
  };
  const nearTop = (root: HTMLDivElement) => root.scrollTop < 256;
  // Auto-loading is user-driven only: explicit scrolls near the top fetch the
  // next page. Bookmark restoration and follow-the-tail layout passes never
  // page on their own; prepended rows cannot retrigger while the user holds
  // position because scrollTop stays beyond the threshold.
  const maybeAutoLoad = (root: HTMLDivElement) => {
    if (nearTop(root)) void loadEarlier();
  };
  useLayoutEffect(() => {
    if (
      !bookmarkKey ||
      !historyRevision ||
      !historyReady ||
      !rows.length ||
      appliedRevision.current === historyRevision
    )
      return;
    stopRestore();
    const bookmark = runtime.readingPositions.get(bookmarkKey);
    appliedRevision.current = historyRevision;
    if (!bookmark || bookmark.follow) {
      follow.current = true;
      setAtEnd(true);
      setPositionNotice('');
      return;
    }
    const target = readingTarget(rows, historyRevision, bookmark);
    setPositionNotice(
      target.fallback
        ? 'Your saved place is no longer in this history. Showing the nearest loaded item.'
        : '',
    );
    follow.current = false;
    setAtEnd(false);
    if (target.index < 0) return;
    const id = rows[target.index]!.id;
    restoring.current = true;
    virtual.scrollToOffset(
      (virtual.getOffsetForIndex(target.index + 1, 'start')?.[0] ?? 0) +
        target.offset,
    );
    let attempts = 0;
    const align = () => {
      const root = viewport.current;
      if (!root) {
        restoring.current = false;
        return;
      }
      const element = [
        ...root.querySelectorAll<HTMLElement>('[data-reading-id]'),
      ].find((row) => row.dataset.readingId === id);
      if (element) {
        const delta =
          element.getBoundingClientRect().top -
          root.getBoundingClientRect().top +
          target.offset;
        virtual.scrollToOffset(root.scrollTop + delta);
      } else
        virtual.scrollToOffset(
          (virtual.getOffsetForIndex(target.index + 1, 'start')?.[0] ?? 0) +
            target.offset,
        );
      // Measurements can settle over several frames. This bounded one-time
      // restore hands scrolling back to TanStack's existing prepend/follow logic.
      if (++attempts < 8) restoreFrame.current = requestAnimationFrame(align);
      else {
        restoring.current = false;
        if (!element)
          setPositionNotice(
            'Your saved place could not be restored. Showing loaded content.',
          );
        savePosition.current();
      }
    };
    restoreFrame.current = requestAnimationFrame(align);
  }, [bookmarkKey, historyRevision, historyReady, rows, runtime, virtual]);
  useLayoutEffect(
    () => () => {
      savePosition.current();
      cancelAnimationFrame(restoreFrame.current);
      restoring.current = false;
      appliedRevision.current = undefined;
    },
    [bookmarkKey],
  );
  useLayoutEffect(() => {
    // A fixed offset lets the next user scroll take over. An indexed scroll can
    // keep reconciling toward the last row after the user starts reading history.
    if (follow.current && historyReady && !restoring.current)
      virtual.scrollToOffset(viewport.current?.scrollHeight ?? 0);
  }, [rows, total, virtual, historyReady]);
  useEffect(() => {
    const update = () => {
      const root = viewport.current;
      if (!root) return;
      const selection = document.getSelection();
      const nodes = [
        selection?.anchorNode,
        selection?.focusNode,
        document.activeElement,
      ];
      const ids = nodes.flatMap((node) => {
        const element = node instanceof Element ? node : node?.parentElement;
        const row = element?.closest<HTMLElement>('[data-reading-id]');
        return row && root.contains(row) ? [row.dataset.readingId!] : [];
      });
      setPinned((previous) =>
        previous.join('\n') === ids.join('\n') ? previous : ids,
      );
    };
    document.addEventListener('selectionchange', update);
    document.addEventListener('focusin', update);
    return () => {
      document.removeEventListener('selectionchange', update);
      document.removeEventListener('focusin', update);
    };
  }, []);
  return (
    <div {...stylex.props(styles.region)}>
      {positionNotice && (
        <div role="status" {...stylex.props(layout.notice)}>
          {positionNotice}
        </div>
      )}
      <div
        ref={viewport}
        {...stylex.props(styles.viewport)}
        role="region"
        tabIndex={0}
        aria-label={label}
        onWheel={stopRestore}
        onTouchStart={stopRestore}
        onKeyDown={(event) => {
          if (
            [
              'ArrowUp',
              'ArrowDown',
              'PageUp',
              'PageDown',
              'Home',
              'End',
              ' ',
            ].includes(event.key)
          )
            stopRestore();
        }}
        onScroll={(event) => {
          if (restoring.current) return;
          const target = event.currentTarget;
          const end =
            target.scrollHeight - target.scrollTop - target.clientHeight < 64;
          follow.current = end;
          setAtEnd(end);
          savePosition.current();
          maybeAutoLoad(target);
        }}
      >
        <div {...stylex.props(styles.inner, contentStyle)}>
          {!rows.length && empty}
          <div style={{ height: total, position: 'relative' }}>
            {virtual.getVirtualItems().map((item) => (
              <div
                key={item.key}
                data-index={item.index}
                data-reading-id={
                  item.index === 0 ? undefined : rows[item.index - 1]!.id
                }
                ref={virtual.measureElement}
                style={{
                  position: 'absolute',
                  top: 0,
                  width: '100%',
                  transform: `translateY(${item.start}px)`,
                }}
              >
                {item.index === 0 ? (
                  <Button
                    variant="ghost"
                    xstyle={!hasMore && styles.exhausted}
                    aria-hidden={!hasMore || undefined}
                    loading={loading || loadingHistory}
                    disabled={!canReadHistory || loading}
                    onClick={() => {
                      stopRestore();
                      follow.current = false;
                      void loadEarlier();
                    }}
                  >
                    {earlierLabel}
                  </Button>
                ) : (
                  renderRow(rows[item.index - 1]!, item.index - 1)
                )}
              </div>
            ))}
          </div>
        </div>
      </div>
      {!atEnd && (
        <Button
          variant="secondary"
          xstyle={styles.jump}
          onClick={() => {
            stopRestore();
            follow.current = true;
            setAtEnd(true);
            virtual.scrollToOffset(viewport.current?.scrollHeight ?? 0);
          }}
        >
          <ArrowDown size={14} /> Latest
        </Button>
      )}
    </div>
  );
}
