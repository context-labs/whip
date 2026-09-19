import { useContext, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { defaultRangeExtractor, elementScroll, useVirtualizer } from '@tanstack/react-virtual';
import { Button } from '@whip/ui';
import { ArrowDown } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { scale } from '@whip/ui/tokens.stylex';
import { useRuntime } from './context';
import { readingTarget } from './reading-positions';
import { layout } from './styles';
import { MotionContext } from './transcript-motion';

const styles = stylex.create({
  viewport: { flex: 1, overflowY: 'auto', minHeight: 0, overflowAnchor: 'none' },
  inner: { maxWidth: 840, marginInline: 'auto', paddingInline: { default: 32, [scale.phone]: 16 }, paddingBlock: 24 },
  chatEdges: {
    // Alpha-only masks blend into every theme without covering selection or intercepting input.
    maskImage: {
      default: 'linear-gradient(to bottom, transparent, rgb(0 0 0 / 0.65) 8px, black 20px, black calc(100% - 40px), rgb(0 0 0 / 0.65) calc(100% - 16px), transparent)',
      '@media (forced-colors: active)': 'none',
      '@media print': 'none',
    },
    WebkitMaskImage: {
      default: 'linear-gradient(to bottom, transparent, rgb(0 0 0 / 0.65) 8px, black 20px, black calc(100% - 40px), rgb(0 0 0 / 0.65) calc(100% - 16px), transparent)',
      '@media (forced-colors: active)': 'none',
      '@media print': 'none',
    },
    scrollPaddingTop: 20,
    scrollPaddingBottom: 40,
  },
  chatInset: { paddingBottom: 40 },
  jump: { position: 'absolute', bottom: 16, left: '50%', transform: 'translateX(-50%)', boxShadow: '0 2px 8px rgb(0 0 0 / 0.08)' },
  region: { flex: 1, position: 'relative', display: 'flex', flexDirection: 'column', minHeight: 0 },
  exhausted: { visibility: 'hidden' },
});

/** Shared virtual reading/selection anchors; session data stays with the SDK. */
export function ReadingList<Row extends { id: string; seq?: number }>({
  rows, hasMore, loadOlder, loadLatest, latestMissing = false, bookmarkKey, historyRevision, historyCursor, historyReady = true,
  label, earlierLabel, renderRow, contentStyle, empty, footer, canLoadOlder = true, loadingHistory = false, chatFollow = false,
}: {
  rows: readonly Row[];
  hasMore: boolean;
  loadOlder(): Promise<void>;
  loadLatest?(): Promise<void>;
  latestMissing?: boolean;
  bookmarkKey?: string;
  historyRevision?: string;
  historyCursor?: number;
  historyReady?: boolean;
  label: string;
  earlierLabel: string;
  renderRow(row: Row, index: number): ReactNode;
  contentStyle?: stylex.StyleXStyles;
  empty?: ReactNode;
  footer?: ReactNode;
  canLoadOlder?: boolean;
  loadingHistory?: boolean;
  chatFollow?: boolean;
}) {
  const runtime = useRuntime();
  const motion = useContext(MotionContext);
  const jumping = useRef(false);
  const pressed = useRef(false);
  const resumeFrame = useRef(0);
  const programmatic = useRef<number | undefined>(undefined);
  const intent = useRef<'up' | 'down' | 'drag' | undefined>(undefined);
  const touchY = useRef<number | undefined>(undefined);
  const lastOffset = useRef(0);
  const content = useRef<HTMLDivElement>(null);
  const viewport = useRef<HTMLDivElement>(null);
  const protectedAnchor = useRef<string | undefined>(undefined);
  const saved = useRef(
    bookmarkKey ? runtime.readingPositions.get(bookmarkKey) : undefined,
  );
  const follow = useRef(saved.current?.follow ?? true);
  const [atEnd, setAtEnd] = useState(follow.current);
  const [loading, setLoading] = useState(false);
  const [loadingLatest, setLoadingLatest] = useState(false);
  const latestIntent = useRef(0);
  const loadingRef = useRef(false);
  const prefetch = useRef<{ pages: number; cursor?: number } | undefined>(undefined);
  const prefetchFrame = useRef(0);
  const upwardGesture = useRef(false);
  const cancelPrefetch = () => {
    prefetch.current = undefined;
    upwardGesture.current = false;
    cancelAnimationFrame(prefetchFrame.current);
  };
  const continuePrefetch = useRef(() => {});
  const [pinned, setPinned] = useState<string[]>([]);
  const [positionNotice, setPositionNotice] = useState('');
  const appliedRevision = useRef<string | undefined>(undefined);
  const restoring = useRef(false);
  const restoreFrame = useRef(0);
  const previousRows = useRef(rows);
  const insertionAnchor = useRef<{ id: string; offset: number } | undefined>(undefined);
  if (previousRows.current !== rows) {
    const changed = previousRows.current.length !== rows.length || previousRows.current.some((row, index) => row.id !== rows[index]?.id);
    if (changed && !follow.current && !restoring.current && appliedRevision.current === historyRevision && viewport.current) {
      const root = viewport.current, top = root.getBoundingClientRect().top;
      const retained = new Set(rows.map(row => row.id));
      // TanStack anchors the first visible item. That item can be the gap
      // being removed, so preserve the next surviving visible row instead.
      const anchor = [...root.querySelectorAll<HTMLElement>('[data-reading-id]')]
        .find(element => retained.has(element.dataset.readingId!) && element.getBoundingClientRect().bottom > top);
      if (anchor) insertionAnchor.current = { id: anchor.dataset.readingId!, offset: anchor.getBoundingClientRect().top - top };
    }
    previousRows.current = rows;
  }
  const virtual = useVirtualizer<HTMLDivElement, HTMLDivElement>({
    count: rows.length + 1,
    getScrollElement: () => viewport.current,
    estimateSize: (index) => (index === 0 ? 0 : 140),
    overscan: 8,
    // Keep the control's measured space after exhaustion. The virtualizer skips
    // resize compensation while scrolling backward, so removing it shifts rows.
    getItemKey: (index) => (index === 0 ? 0 : rows[index - 1]!.id),
    anchorTo: 'end',
    // Intent, not proximity after a resize, decides whether chat is pinned.
    scrollEndThreshold: chatFollow ? (follow.current && historyReady && !jumping.current && !pressed.current ? Infinity : -1) : 64,
    scrollToFn: (offset, options, instance) => {
      elementScroll(offset, options, instance);
      if (viewport.current) programmatic.current = viewport.current.scrollTop;
    },
    rangeExtractor: (range) => {
      const indices = [0, ...defaultRangeExtractor(range)];
      const anchorIndex = insertionAnchor.current ? rows.findIndex(row => row.id === insertionAnchor.current!.id) : -1;
      if (anchorIndex >= 0) indices.push(anchorIndex + 1);
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
  // A selected/focused row is the reader's anchor even when the preceding
  // loading message still spans the viewport edge. The default virtualizer
  // predicate only compensates remeasured rows entirely above that edge.
  const protectedIndex = chatFollow && protectedAnchor.current ? rows.findIndex(row => row.id === protectedAnchor.current) + 1 : 0;
  virtual.shouldAdjustScrollPositionOnItemSizeChange = protectedIndex > 0
    ? item => item.index < protectedIndex
    : undefined;
  const setFollowing = (value: boolean) => {
    follow.current = value;
    if (chatFollow) {
      // Also gate measurements arriving before React commits this input event.
      virtual.options.scrollEndThreshold = value && historyReady && !jumping.current && !pressed.current ? Infinity : -1;
    }
    setAtEnd(value);
  };
  const pinBottom = () => {
    const root = viewport.current;
    if (!root || !follow.current || !historyReady || restoring.current || document.hidden || jumping.current || pressed.current) return;
    // TanStack owns row resize/prepend compensation. This exact end correction
    // covers the surrounding padding, footer and viewport/composer resizing too.
    // Avoid indexed followOnAppend: its pending target can keep reconciling
    // after the reader interrupts it. Direct offsets have no remaining target.
    if (chatFollow) {
      root.scrollTop = Math.max(0, root.scrollHeight - root.clientHeight);
      programmatic.current = root.scrollTop;
    } else virtual.scrollToOffset(root.scrollHeight);
  };
  const cancelJump = () => {
    if (jumping.current && viewport.current) {
      viewport.current.scrollTo({ top: viewport.current.scrollTop, behavior: 'instant' });
      programmatic.current = viewport.current.scrollTop;
    }
    jumping.current = false;
  };
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
    insertionAnchor.current = undefined;
  };
  const userScroll = (direction: 'up' | 'down' | 'drag', target?: EventTarget | null) => {
    if (chatFollow && direction === 'up') {
      // A code/details scroller owns input until it can chain to the transcript.
      for (let node = target instanceof Element ? target : null; node && node !== viewport.current; node = node.parentElement) {
        const style = getComputedStyle(node);
        if (/auto|scroll/.test(style.overflowY) && (node.scrollTop > 0 || /contain|none/.test(style.overscrollBehaviorY))) return;
      }
    }
    protectedAnchor.current = undefined;
    virtual.shouldAdjustScrollPositionOnItemSizeChange = undefined;
    stopRestore();
    latestIntent.current++;
    programmatic.current = undefined;
    if (!chatFollow) return;
    if (direction === 'down') cancelPrefetch();
    // Coalesced input belongs to the current episode, not another page budget.
    if (!prefetch.current) upwardGesture.current = direction !== 'down';
    cancelJump();
    intent.current = direction;
    lastOffset.current = viewport.current?.scrollTop ?? 0;
    const root = viewport.current;
    setFollowing(direction === 'down' && !!root && root.scrollHeight - root.scrollTop - root.clientHeight <= 2);
    // At the hard boundary wheel/touch/keyboard input may produce no scroll event.
    if (direction === 'up' && root && root.scrollTop <= 0) maybeAutoLoad(root);
  };
  const canReadHistory = hasMore && canLoadOlder && historyReady && !loadingHistory;
  const nearTop = (root: HTMLDivElement) =>
    root.scrollTop < (chatFollow ? Math.min(2400, Math.max(800, 2 * root.clientHeight)) : 256);
  const loadEarlier = async (continuation = false) => {
    if (chatFollow && prefetch.current && !continuation) return;
    if (!canReadHistory || loadingRef.current || restoring.current || (chatFollow && document.hidden)) return;
    const episode = chatFollow ? (prefetch.current ??= { pages: 0 }) : undefined;
    if (episode) { episode.pages++; episode.cursor = historyCursor; upwardGesture.current = false; }
    loadingRef.current = true;
    setLoading(true);
    let succeeded = false;
    try {
      await loadOlder();
      succeeded = true;
    } catch {
      // Session history state owns the failure; this control only ends its wait.
    } finally {
      loadingRef.current = false;
      setLoading(false);
    }
    if (!episode || prefetch.current !== episode) return;
    if (!succeeded) { cancelPrefetch(); return; }
    // Let React publish the cursor/rows, then let prepend anchoring and deferred
    // measurements settle. Only an existing episode can reach this bounded wait.
    const started = performance.now();
    let frames = 0;
    let stable = 0;
    let geometry = '';
    const settle = () => {
      if (prefetch.current !== episode) return;
      const root = viewport.current;
      if (!root || document.hidden || ++frames > 32) { cancelPrefetch(); return; }
      const next = `${root.scrollTop}:${root.scrollHeight}:${root.clientHeight}`;
      stable = !restoring.current && next === geometry ? stable + 1 : 0;
      geometry = next;
      // Timeline coalesces live projections at 30Hz, independent of display Hz.
      if (performance.now() - started >= 50 && stable >= 2) continuePrefetch.current();
      else prefetchFrame.current = requestAnimationFrame(settle);
    };
    prefetchFrame.current = requestAnimationFrame(settle);
  };
  continuePrefetch.current = () => {
    const episode = prefetch.current;
    const root = viewport.current;
    if (!episode || !root) return;
    // Raw cursors, not visible rows/height, prove progress through filtered pages.
    if (episode.pages >= 3 || !canReadHistory || !nearTop(root) ||
      episode.cursor === undefined || historyCursor === undefined || historyCursor >= episode.cursor) {
      cancelPrefetch(); return;
    }
    void loadEarlier(true);
  };
  const maybeAutoLoad = (root: HTMLDivElement) => {
    if (!nearTop(root) || (chatFollow && (!upwardGesture.current || prefetch.current))) return;
    if (chatFollow) upwardGesture.current = false;
    void loadEarlier();
  };
  useLayoutEffect(() => {
    if (!canLoadOlder || (!historyReady && !loadingHistory)) cancelPrefetch();
  }, [canLoadOlder, historyReady, loadingHistory]);
  useLayoutEffect(() => () => cancelPrefetch(), [bookmarkKey, historyRevision, chatFollow]);
  useEffect(() => {
    const hide = () => { if (document.hidden) cancelPrefetch(); };
    document.addEventListener('visibilitychange', hide);
    return () => document.removeEventListener('visibilitychange', hide);
  }, []);
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
  useLayoutEffect(() => {
    const anchor = insertionAnchor.current;
    if (!anchor || restoring.current) return;
    restoring.current = true;
    let attempts = 0;
    const align = () => {
      const root = viewport.current;
      const element = root && [...root.querySelectorAll<HTMLElement>('[data-reading-id]')].find(row => row.dataset.readingId === anchor.id);
      if (!root || !element) { stopRestore(); return; }
      const delta = element.getBoundingClientRect().top - root.getBoundingClientRect().top - anchor.offset;
      if (Math.abs(delta) > 0.5) virtual.scrollToOffset(root.scrollTop + delta);
      if (++attempts < 8) restoreFrame.current = requestAnimationFrame(align);
      else { stopRestore(); savePosition.current(); }
    };
    align();
  }, [rows, virtual]);
  useLayoutEffect(
    () => () => {
      savePosition.current();
      latestIntent.current++;
      cancelJump();
      cancelAnimationFrame(resumeFrame.current);
      intent.current = undefined;
      cancelAnimationFrame(restoreFrame.current);
      restoring.current = false;
      appliedRevision.current = undefined;
    },
    [bookmarkKey],
  );
  useLayoutEffect(() => {
    if (!motion && jumping.current) { cancelJump(); setFollowing(follow.current); }
    pinBottom();
  }, [rows, total, virtual, historyReady, footer, motion]);
  const onResize = useRef(pinBottom);
  onResize.current = pinBottom;
  const resumeFollowing = useRef(() => {});
  resumeFollowing.current = () => {
    pressed.current = false;
    if (intent.current === 'drag') intent.current = undefined;
    setFollowing(follow.current); pinBottom();
  };
  useEffect(() => {
    const release = () => {
      if (!pressed.current) return;
      cancelAnimationFrame(resumeFrame.current);
      // Let pointerup/mouseup/click finish on the same, stationary target.
      resumeFrame.current = requestAnimationFrame(() => resumeFollowing.current());
    };
    window.addEventListener('pointerup', release);
    window.addEventListener('pointercancel', release);
    window.addEventListener('blur', release);
    return () => { window.removeEventListener('pointerup', release); window.removeEventListener('pointercancel', release); window.removeEventListener('blur', release); cancelAnimationFrame(resumeFrame.current); };
  }, []);
  useEffect(() => {
    if (!chatFollow || typeof ResizeObserver === 'undefined') return;
    const observer = new ResizeObserver(() => onResize.current());
    if (content.current) observer.observe(content.current);
    if (viewport.current) observer.observe(viewport.current);
    const visible = () => { if (!document.hidden) onResize.current(); };
    document.addEventListener('visibilitychange', visible);
    return () => { observer.disconnect(); document.removeEventListener('visibilitychange', visible); };
  }, [chatFollow]);
  useEffect(() => {
    const update = (event: Event) => {
      const root = viewport.current;
      if (!root) return;
      if (chatFollow && event.type === 'focusin' && root.contains(document.activeElement) && document.activeElement?.closest('[data-reading-id]')) cancelPrefetch();
      const selection = document.getSelection();
      if (chatFollow && selection && !selection.isCollapsed && (root.contains(selection.anchorNode) || root.contains(selection.focusNode))) { cancelPrefetch(); latestIntent.current++; cancelJump(); intent.current = undefined; setFollowing(false); }
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
      if (chatFollow) {
        // Capture visibility at the interaction, not during a resize render:
        // scroll compensation can precede the new row transforms in that frame.
        const bounds = root.getBoundingClientRect();
        const anchor = ids.map(id => virtual.elementsCache?.get(id)).find(element => element && element.getBoundingClientRect().bottom > bounds.top && element.getBoundingClientRect().top < bounds.bottom);
        protectedAnchor.current = anchor?.dataset.readingId;
        virtual.shouldAdjustScrollPositionOnItemSizeChange = anchor
          ? item => item.index < Number(anchor.dataset.index)
          : undefined;
      }
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
        {...stylex.props(styles.viewport, chatFollow && styles.chatEdges)}
        role="region"
        tabIndex={0}
        aria-label={label}
        onWheel={event => { if (!event.ctrlKey && !event.defaultPrevented && event.deltaY) userScroll(event.deltaY < 0 ? 'up' : 'down', event.target); }}
        onTouchStart={event => { touchY.current = !event.defaultPrevented && event.touches.length === 1 ? event.touches[0]?.clientY : undefined; }}
        onTouchMove={event => {
          if (event.defaultPrevented || event.touches.length !== 1) { touchY.current = undefined; return; }
          const y = event.touches[0]?.clientY;
          if (y !== undefined && touchY.current !== undefined && y !== touchY.current) userScroll(y > touchY.current ? 'up' : 'down', event.target);
          touchY.current = y;
        }}
        onPointerDown={event => {
          if (chatFollow) { pressed.current = true; setFollowing(follow.current); }
          // Native scrollbar drags target the viewport. Ordinary row clicks do not.
          if (event.target === event.currentTarget && event.pointerType !== 'touch') userScroll('drag');
        }}
        onKeyDown={event => {
          if (event.defaultPrevented || (event.target as Element).closest('input, textarea, select, [contenteditable="true"]')) return;
          if (event.key === ' ' && (event.target as Element).closest('button, a, [role="button"]')) return;
          if (['ArrowUp', 'PageUp', 'Home'].includes(event.key) || (event.key === ' ' && event.shiftKey)) userScroll('up', event.target);
          else if (['ArrowDown', 'PageDown', 'End', ' '].includes(event.key)) userScroll('down');
        }}
        onScrollEnd={() => {
          if (!pressed.current || intent.current !== 'drag') { intent.current = undefined; upwardGesture.current = false; }
          if (jumping.current) { jumping.current = false; setFollowing(true); pinBottom(); }
        }}
        onScroll={event => {
          if (restoring.current) return;
          const target = event.currentTarget;
          const offset = target.scrollTop;
          if (programmatic.current !== undefined && Math.abs(offset - programmatic.current) < 1) {
            programmatic.current = undefined; lastOffset.current = offset; savePosition.current(); return;
          }
          programmatic.current = undefined;
          const end = target.scrollHeight - offset - target.clientHeight <= (chatFollow ? 2 : 64);
          if (!chatFollow) { follow.current = end; setAtEnd(end); maybeAutoLoad(target); }
          else if (intent.current && !jumping.current) {
            // An upward gesture stays detached even a pixel from the bottom.
            setFollowing(end && (intent.current === 'down' || (intent.current === 'drag' && offset > lastOffset.current)));
            if (intent.current === 'drag' && offset > lastOffset.current) cancelPrefetch();
            if (intent.current === 'up' || (intent.current === 'drag' && offset < lastOffset.current)) maybeAutoLoad(target);
          }
          lastOffset.current = offset;
          savePosition.current();
        }}
      >
        <div ref={content} {...stylex.props(styles.inner, chatFollow && styles.chatInset, contentStyle)}>
          {!rows.length && empty}
          <div style={{ height: total, position: 'relative' }}>
            {virtual.getVirtualItems().map((item) => (
              <div
                key={item.key}
                data-index={item.index}
                data-reading-id={
                  item.index === 0 ? undefined : rows[item.index - 1]!.id
                }
                data-reading-seq={item.index === 0 ? undefined : rows[item.index - 1]!.seq}
                tabIndex={-1}
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
                      cancelJump(); setFollowing(false);
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
          {footer}
        </div>
      </div>
      {(!atEnd || latestMissing) && (
        <Button
          variant="secondary"
          xstyle={styles.jump}
          loading={loadingLatest}
          disabled={loadingLatest || (latestMissing && !canLoadOlder)}
          onClick={() => { void (async () => {
            cancelPrefetch();
            stopRestore();
            const request = ++latestIntent.current;
            if (latestMissing && loadLatest) {
              cancelJump(); setFollowing(false); setLoadingLatest(true);
              try { await loadLatest(); await new Promise<void>(resolve => requestAnimationFrame(() => resolve())); }
              catch { return; }
              finally { setLoadingLatest(false); }
              if (request !== latestIntent.current || !viewport.current) return;
            }
            intent.current = undefined;
            const root = viewport.current;
            jumping.current = chatFollow && motion && !!root && root.scrollHeight - root.scrollTop - root.clientHeight > 2;
            setFollowing(true);
            if (jumping.current && root) root.scrollTo({ top: root.scrollHeight, behavior: 'smooth' });
            else pinBottom();
          })(); }}
        >
          <ArrowDown size={14} /> Latest
        </Button>
      )}
    </div>
  );
}
