import { useLayoutEffect, useRef } from 'react';
import type { DeepReadonly, HistoryGap } from '@whip/sdk/state';
import { Button } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { layout } from './styles';

/** History errors belong to the missing range, not the connection or composer. */
export function HistoryGapControl({ gap, connected, load }: { gap: DeepReadonly<HistoryGap>; connected: boolean; load(): Promise<void> }) {
  const element = useRef<HTMLDivElement>(null);
  const fromSeq = useRef(gap.fromSeq); fromSeq.current = gap.fromSeq;
  useLayoutEffect(() => () => {
    const node = element.current;
    if (!node?.contains(document.activeElement)) return;
    const region = node.closest('[role="region"]') as HTMLElement | null;
    // Keep keyboard navigation at the recovered content when its control goes away.
    requestAnimationFrame(() => {
      if (!region?.isConnected || (document.activeElement !== document.body && document.activeElement?.isConnected)) return;
      const next = [...region.querySelectorAll<HTMLElement>('[data-reading-seq]')].find(row => Number(row.dataset.readingSeq) >= fromSeq.current);
      (next ?? region).focus({ preventScroll: true });
    });
  }, []);
  const loading = gap.status === 'loading' || gap.status === 'pending';
  return <div ref={element} tabIndex={-1} aria-label="Missing messages" data-history-gap={gap.toSeq} {...stylex.props(layout.notice)}>
    <span role="status">{loading ? 'Loading missing messages…' : gap.status === 'error' ? "Couldn't load messages." : 'Some messages are not loaded.'}</span>{' '}
    <Button variant="ghost" size="sm" loading={loading} disabled={!connected || loading} onClick={() => { element.current?.focus({ preventScroll: true }); void load().catch(() => {}); }}>
      {gap.status === 'error' ? 'Retry' : 'Load missing messages'}
    </Button>
  </div>;
}
