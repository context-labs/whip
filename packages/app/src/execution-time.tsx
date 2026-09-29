import { useEffect, useState } from 'react';
import type { Cell } from '@whip/sdk';
import type { DeepReadonly } from '@whip/sdk/state';
import { recordedDuration } from './execution-output';
import * as stylex from '@stylexjs/stylex';
import { surface, typography } from '@whip/ui/tokens.stylex';

/** Format the daemon's Go duration without changing the retained measurement. */
export function formatHostDuration(value: string): string {
  const units: Record<string, number> = {
    ns: 0.000001,
    µs: 0.001,
    μs: 0.001,
    us: 0.001,
    ms: 1,
    s: 1000,
    m: 60_000,
    h: 3_600_000,
  };
  const parts = [...value.matchAll(/(\d+(?:\.\d+)?)(ns|[µμu]s|ms|s|m|h)/g)];
  if (!parts.length || parts.map((part) => part[0]).join('') !== value)
    return value;
  const ms = parts.reduce(
    (sum, part) => sum + Number(part[1]) * units[part[2]!]!,
    0,
  );
  if (!Number.isFinite(ms)) return value;
  const rounded = Math.round(ms * 100) / 100;
  if (rounded < 1) return `${rounded.toFixed(2)}ms`;
  if (Math.round(ms) < 1000) return `${Math.round(ms)}ms`;
  const seconds = Math.round(ms / 1000);
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  return `${hours ? `${hours}h` : ''}${minutes ? `${minutes % 60}m` : ''}${seconds % 60}s`;
}

/** Completed duration uses recorded host endpoints; live elapsed is explicitly an estimate. */
export function ExecutionTime({
  cell,
  connected,
}: {
  cell: DeepReadonly<Cell>;
  connected: boolean;
}) {
  const [now, setNow] = useState(Date.now);
  const running =
    connected &&
    cell.state === 'running' &&
    Number.isFinite(Date.parse(cell.created_at));
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
    return () => {
      clearInterval(timer);
      document.removeEventListener('visibilitychange', changed);
    };
  }, [running]);
  const duration = recordedDuration(cell.created_at, cell.finished_at);
  const milliseconds = now - Date.parse(cell.created_at);
  if (
    !duration &&
    (!running || !Number.isFinite(milliseconds) || milliseconds < 0)
  )
    return null;
  return (
    <span
      {...stylex.props(styles.time)}
      title={
        duration
          ? 'Recorded execution duration'
          : 'Elapsed from host start time; client and host clocks may differ'
      }
    >
      {duration ?? `${Math.floor(milliseconds / 1000)}s elapsed`}
    </span>
  );
}

const styles = stylex.create({
  time: {
    color: surface.secondaryText,
    fontSize: typography.size12,
    fontVariantNumeric: 'tabular-nums',
  },
});
