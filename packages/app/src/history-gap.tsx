import { useLayoutEffect, useRef, useState } from 'react';
import type { DeepReadonly, HistoryGap } from '@whip/sdk/state';
import { Button } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { layout } from './styles';
import { ErrorNotice } from './error-feedback';

/** The SDK explicitly omitted this message to keep its transcript window bounded.
 * Read the message separately; repeated observation cannot enlarge that window. */
export function HistoryGapControl({ gap, connected, load }: { gap: DeepReadonly<HistoryGap>; connected: boolean; load(): Promise<void> }) {
  const element = useRef<HTMLDivElement>(null);
  const sequence = useRef(gap.sequence); sequence.current = gap.sequence;
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<unknown>();
  useLayoutEffect(() => () => {
    const node = element.current;
    if (!node?.contains(document.activeElement)) return;
    const region = node.closest('[role="region"]') as HTMLElement | null;
    requestAnimationFrame(() => {
      if (!region?.isConnected || (document.activeElement !== document.body && document.activeElement?.isConnected)) return;
      const next = [...region.querySelectorAll<HTMLElement>('[data-reading-seq]')].find(row => row.dataset.readingSeq && BigInt(row.dataset.readingSeq) >= BigInt(sequence.current));
      (next ?? region).focus({ preventScroll: true });
    });
  }, []);
  return <div ref={element} tabIndex={-1} aria-label="Large message" data-history-gap={gap.messageID} {...stylex.props(layout.notice)}>
    <span role="status">This message exceeds the conversation window's size limit ({gap.bytes.toLocaleString()} bytes).</span>{' '}
    <Button variant="ghost" size="sm" loading={loading} disabled={!connected || loading} onClick={async () => {
      element.current?.focus({ preventScroll: true }); setLoading(true); setError(undefined);
      try { await load(); } catch (failure) { setError(failure); } finally { setLoading(false); }
    }}>Read large message</Button>
    {error !== undefined && <ErrorNotice type="resource" owner={gap.messageID} error={error} />}
  </div>;
}
