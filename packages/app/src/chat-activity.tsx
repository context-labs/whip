import { useContext, useEffect, useState } from 'react';
import type {
  CellExecutionRow,
  DeepReadonly,
  SessionViewSnapshot,
} from '@whip/sdk/state';
import type { Turn } from '@whip/sdk';
import {
  ActivityIndicator,
  Button,
  IconButton,
  Tooltip,
  useTheme,
} from '@whip/ui';
import { Circle, Pause, Play, ShieldAlert } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { surface, typography } from '@whip/ui/tokens.stylex';
import { cellActivityLabel, executionActive } from './chat-activity-rows';
import { ExecutionTime } from './execution-time';
import { MotionContext } from './transcript-motion';

export function activityStatus(
  state: DeepReadonly<SessionViewSnapshot>,
  cells: readonly CellExecutionRow[],
  connected: boolean,
  lastTurn?: DeepReadonly<Turn>,
  delivery?: string,
) {
  const activity = state.activity;
  if (!connected || state.status === 'stale' || state.status === 'suspended')
    return { text: 'Updates paused', active: false, pending: !!delivery };
  if (!activity)
    return {
      text: state.error ? 'Activity unavailable' : 'Loading session…',
      active: false,
    };
  if (BigInt(activity.pending_permission_count) > 0n)
    return {
      text: 'Waiting for your approval',
      active: false,
      attention: true,
      pending: !!delivery,
    };
  if (BigInt(activity.pending_question_count) > 0n)
    return {
      text: 'Waiting for your answer',
      active: false,
      attention: true,
      pending: !!delivery,
    };
  if (activity.active_workspace_action_id)
    return { text: 'Updating workspace', active: true };
  const turn = activity.active_turn;
  if (turn) {
    if (turn.state === 'cancelling')
      return { text: 'Stopping current turn', active: true };
    const current = cells
      .filter((row) => executionActive(row) && row.cell.turn_id === turn.id)
      .at(-1);
    if (current)
      return { text: cellActivityLabel(current), active: true, cell: current };
    const preview = state.preview;
    return {
      text: !activity.execution_permit
        ? 'Waiting for work to continue'
        : !!preview?.text
          ? 'Writing a response'
          : !!preview?.reasoning
            ? 'Thinking'
            : 'Working',
      active: activity.execution_permit,
    };
  }
  if (delivery)
    return {
      text: delivery === 'Queued' ? 'Queued · waiting to start' : delivery,
      active: delivery === 'Sending…',
      pending: true,
    };
  if (BigInt(activity.queued_input_count) > 0n)
    return { text: 'Queued · waiting to start', active: false, pending: true };
  if (
    lastTurn &&
    ['failed', 'cancelled', 'interrupted'].includes(lastTurn.state)
  )
    return {
      text: `Last turn ${lastTurn.state}`,
      active: false,
      attention: lastTurn.state === 'failed',
    };
  return {
    text: activity.lifecycle === 'stopped' ? 'Stopped' : 'Idle',
    active: false,
  };
}

const workingWords = [
  'Thinking',
  'Pondering',
  'Composing',
  'Untangling',
  'Sifting',
  'Sketching',
];

/** A visual trailer in the reading flow. CurrentActivity owns live announcements. */
export function TranscriptWorking({
  status,
  turnId,
  startedAt,
}: {
  status: ReturnType<typeof activityStatus>;
  turnId?: string;
  startedAt?: string;
}) {
  const motion = useContext(MotionContext);
  const [observedStart] = useState(Date.now);
  const [now, setNow] = useState(Date.now);
  const running = !!turnId && status.active && !status.pending;
  useEffect(() => {
    if (!running) return;
    let timer: ReturnType<typeof setInterval> | undefined;
    const update = () => {
      clearInterval(timer);
      if (!document.hidden) {
        setNow(Date.now());
        timer = setInterval(() => setNow(Date.now()), 1000);
      }
    };
    update();
    document.addEventListener('visibilitychange', update);
    return () => {
      clearInterval(timer);
      document.removeEventListener('visibilitychange', update);
    };
  }, [running]);
  if (!status.active && !status.pending && !turnId) return null;
  const recorded = startedAt ? Date.parse(startedAt) : NaN;
  const seconds = Math.max(
    0,
    Math.floor(
      (now - (Number.isFinite(recorded) ? recorded : observedStart)) / 1000,
    ),
  );
  const label =
    running && ['Working', 'Thinking'].includes(status.text)
      ? workingWords[
          motion ? Math.floor(seconds / 7) % workingWords.length : 0
        ]!
      : status.text;
  return (
    <div data-transcript-working {...stylex.props(styles.trailer)}>
      {status.attention ? (
        <ShieldAlert size={14} aria-hidden="true" />
      ) : (
        <ActivityIndicator
          variant="matrix"
          active={status.active}
          reduceMotion={!motion}
        />
      )}
      <span>{status.active && !label.endsWith('…') ? `${label}…` : label}</span>
      {running && (
        <span
          aria-hidden="true"
          data-turn-elapsed
          {...stylex.props(styles.elapsed)}
          title={
            Number.isFinite(recorded)
              ? 'Elapsed turn time'
              : 'Time since this client observed the turn'
          }
        >
          {seconds < 60
            ? `${seconds}s`
            : `${Math.floor(seconds / 60)}m ${seconds % 60}s`}
        </span>
      )}
    </div>
  );
}

export function CurrentActivity({
  status,
  connected,
  onDetails,
}: {
  status: ReturnType<typeof activityStatus>;
  connected: boolean;
  onDetails(): void;
}) {
  const { display, setDisplay } = useTheme();
  return (
    <div
      aria-label="Current activity"
      data-current-activity
      {...stylex.props(styles.statusRow)}
    >
      <Tooltip label={status.text}>
        <Button
          variant="ghost"
          size="sm"
          xstyle={styles.statusButton}
          aria-label={`Activity: ${status.text}`}
          onClick={onDetails}
        >
          {status.attention ? (
            <ShieldAlert size={14} aria-hidden="true" />
          ) : status.active ? (
            <ActivityIndicator
              active
              reduceMotion={display.motion === 'reduce'}
            />
          ) : (
            <Circle size={10} aria-hidden="true" />
          )}
          <span
            role="status"
            aria-live="polite"
            aria-atomic="true"
            {...stylex.props(styles.statusText)}
          >
            {status.text}
          </span>
        </Button>
      </Tooltip>
      {status.cell && (
        <span {...stylex.props(styles.statusTime)}>
          <ExecutionTime cell={status.cell.cell} connected={connected} />
        </span>
      )}
      {status.active && (
        <IconButton
          variant="ghost"
          size="sm"
          label={
            display.motion === 'reduce'
              ? 'Use system motion setting'
              : 'Pause activity animation'
          }
          onClick={() =>
            setDisplay({
              motion: display.motion === 'reduce' ? 'system' : 'reduce',
            })
          }
        >
          {display.motion === 'reduce' ? (
            <Play size={12} />
          ) : (
            <Pause size={12} />
          )}
        </IconButton>
      )}
    </div>
  );
}

const styles = stylex.create({
  trailer: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    minHeight: 32,
    paddingBlock: 12,
    color: surface.secondaryText,
    fontSize: typography.size12,
  },
  elapsed: { fontVariantNumeric: 'tabular-nums', whiteSpace: 'nowrap' },
  statusRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 4,
    minWidth: 0,
    maxWidth: '100%',
    fontSize: typography.size12,
  },
  statusButton: { minWidth: 28, paddingInline: 4, fontSize: typography.size12 },
  statusText: {
    minWidth: 0,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
    position: { '@container (max-width: 600px)': 'absolute' },
    width: { '@container (max-width: 600px)': 1 },
    height: { '@container (max-width: 600px)': 1 },
    clipPath: { '@container (max-width: 600px)': 'inset(50%)' },
  },
  statusTime: {
    flexShrink: 0,
    display: { default: 'inline', '@container (max-width: 600px)': 'none' },
  },
});
