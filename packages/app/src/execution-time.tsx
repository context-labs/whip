import { useEffect, useState } from 'react';
import type { ExecutionCell } from '@whip/sdk/state';
import * as stylex from '@stylexjs/stylex';
import { surface, typography } from '@whip/ui/tokens.stylex';

/** Client-observed time only; snapshot replay never starts a historical clock. */
export function ExecutionTime({ cell, connected }: { cell: ExecutionCell; connected: boolean }) {
  const [now, setNow] = useState(Date.now);
  const running = connected && cell.status === 'running' && cell.observedStartedAt !== undefined;
  useEffect(() => {
    if (!running) return;
    let timer: ReturnType<typeof setInterval> | undefined;
    const changed = () => {
      clearInterval(timer);
      timer = undefined;
      if (!document.hidden) {
        setNow(Date.now());
        timer = setInterval(() => setNow(Date.now()), 1000);
      }
    };
    changed();
    document.addEventListener('visibilitychange', changed);
    return () => { clearInterval(timer); document.removeEventListener('visibilitychange', changed); };
  }, [running]);
  if (cell.observedStartedAt === undefined || (!running && cell.observedEndedAt === undefined)) return null;
  const seconds = Math.max(0, ((cell.observedEndedAt ?? now) - cell.observedStartedAt) / 1000);
  return <span {...stylex.props(styles.time)} title="Time observed in this client; not a historical execution duration">Observed {seconds < 10 ? seconds.toFixed(1) : Math.round(seconds)}s</span>;
}

const styles = stylex.create({ time: { color: surface.secondaryText, fontSize: typography.size12, fontVariantNumeric: 'tabular-nums' } });
