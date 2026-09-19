import { useEffect, useState } from 'react';
import type { ExecutionCell } from '@whip/sdk/state';
import * as stylex from '@stylexjs/stylex';
import { surface, typography } from '@whip/ui/tokens.stylex';

/** Format the daemon's Go duration without changing the retained measurement. */
export function formatHostDuration(value: string): string {
  const units: Record<string, number> = { ns: 0.000001, 'µs': 0.001, 'μs': 0.001, us: 0.001, ms: 1, s: 1000, m: 60_000, h: 3_600_000 };
  const parts = [...value.matchAll(/(\d+(?:\.\d+)?)(ns|[µμu]s|ms|s|m|h)/g)];
  if (!parts.length || parts.map(part => part[0]).join('') !== value) return value;
  const ms = parts.reduce((sum, part) => sum + Number(part[1]) * units[part[2]!]!, 0);
  if (!Number.isFinite(ms)) return value;
  const rounded = Math.round(ms * 100) / 100;
  if (rounded < 1) return `${rounded.toFixed(2)}ms`;
  if (Math.round(ms) < 1000) return `${Math.round(ms)}ms`;
  const seconds = Math.round(ms / 1000);
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  return `${hours ? `${hours}h` : ''}${minutes ? `${minutes % 60}m` : ''}${seconds % 60}s`;
}

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
