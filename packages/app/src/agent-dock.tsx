import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import type { DeepReadonly } from '@whip/sdk/state';
import type { Session, SessionRecord, SessionActivity, Turn } from '@whip/sdk';
import { useQuery } from '@tanstack/react-query';
import { ErrorNotice } from './error-feedback';
import { Button, Spinner } from '@whip/ui';
import { ChevronDown, ChevronRight, Circle, ShieldAlert } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface, typography } from '@whip/ui/tokens.stylex';
import { composerPanels } from './composer-panels.stylex';

export interface DockAgent {
  agent: DeepReadonly<SessionRecord>;
  activity: DeepReadonly<SessionActivity>;
  turn?: DeepReadonly<Turn>;
}
type Row = DockAgent & {
  text: string;
  attention: boolean;
  running: boolean;
  finished: boolean;
};
export interface AgentDockProps {
  session: Session;
  treeId: string;
  connected: boolean;
  onAgent(id: string): void;
  onAllAgents(): void;
  openAgentId?: string;
}
export function projectAgent(value: DockAgent): Row {
  const { agent, activity, turn } = value;
  const stopped = agent.lifecycle === 'stopped';
  const approval = BigInt(activity.pending_permission_count) > 0n;
  const question = BigInt(activity.pending_question_count) > 0n;
  const active =
    !!activity.active_turn || !!activity.active_workspace_action_id;
  const queued = BigInt(activity.queued_input_count) > 0n;
  const failed = !active && !queued && turn?.state === 'failed';
  const text = approval
    ? 'Waiting for your approval'
    : question
      ? 'Waiting for your answer'
      : stopped
        ? 'Stopped'
        : active
          ? activity.execution_permit || activity.active_workspace_action_id
            ? 'Working'
            : 'Waiting'
          : queued
            ? 'Queued'
            : failed
              ? 'Failed'
              : turn
                ? turn.state === 'succeeded'
                  ? 'Completed'
                  : turn.state
                : 'Not started';
  return {
    ...value,
    text,
    running: active || queued,
    attention: approval || question || failed,
    finished:
      !active &&
      !queued &&
      !approval &&
      !question &&
      (stopped ||
        (!!turn &&
          ['succeeded', 'cancelled', 'interrupted'].includes(turn.state))),
  };
}

/** Bounded metadata reads; this dock never opens a child's transcript or worker. */
export function AgentDock(props: AgentDockProps) {
  const { session, treeId, connected } = props;
  const query = useQuery({
    queryKey: [
      'agent-dock',
      session.client.runtimeID,
      session.client.processEpoch,
      treeId,
      session.id,
    ],
    queryFn: async ({ signal }) => {
      const page = await session.client.call(
        'sessions.list',
        { tree_id: treeId, limit: 16 },
        { signal },
      );
      if (page.items?.some((item) => item.tree_id !== treeId))
        throw new Error('Agent list belongs to another tree');
      const items = await Promise.all(
        (page.items ?? [])
          .filter((agent) => agent.parent_id === session.id)
          .map(async (agent) => {
            const handle = session.client.session(agent.id);
            const [activity, turns] = await Promise.all([
              handle.activity({ signal }),
              handle.turns.page({ limit: 1 }, { signal }),
            ]);
            return { agent, activity, turn: turns.items[0] };
          }),
      );
      return { items, partial: (page.items?.length ?? 0) === 16 };
    },
    enabled: connected,
    gcTime: 0,
    retry: false,
    refetchInterval: connected ? 3000 : false,
  });
  return (
    <>
      <ErrorNotice
        type="resource"
        owner={`${session.id}:dock`}
        error={query.error}
        title="Agent updates unavailable"
      />
      <AgentDockRoster
        key={`${session.client.runtimeID}:${session.id}`}
        {...props}
        connected={connected && !query.error}
        agents={query.data?.items ?? []}
        partial={query.data?.partial ?? false}
      />
    </>
  );
}

/** Latest-turn wall time, never agent lifetime or a queued turn's predecessor. */
function turnTiming(row: Row, activeTurn?: string) {
  const turn = row.turn;
  if (
    !turn ||
    (activeTurn && turn.id !== activeTurn) ||
    row.text === 'Queued' ||
    row.text === 'Not started'
  )
    return;
  const start = Date.parse(turn.started_at ?? '');
  const end = Date.parse(turn.finished_at ?? '');
  if (!Number.isFinite(start)) return;
  if (Number.isFinite(end)) {
    if (row.running || ['running', 'cancelling'].includes(turn.state)) return;
    return end >= start ? { start, end } : undefined;
  }
  if (
    !turn.finished_at &&
    ['running', 'cancelling'].includes(turn.state) &&
    !row.finished &&
    row.text !== 'Failed'
  )
    return { start, end: undefined };
}

function formatElapsed(ms: number) {
  const seconds = Math.max(0, Math.floor(ms / 1000));
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  return hours
    ? `${hours}h ${minutes % 60}m`
    : minutes
      ? `${minutes}m ${seconds % 60}s`
      : `${seconds}s`;
}

export function AgentDockRoster({
  agents,
  partial,
  connected,
  onAgent,
  onAllAgents,
  openAgentId,
}: Pick<
  AgentDockProps,
  'connected' | 'onAgent' | 'onAllAgents' | 'openAgentId'
> & { agents: readonly DockAgent[]; partial: boolean }) {
  const [expanded, setExpanded] = useState(false);
  const [focused, setFocused] = useState<string>();
  const [hovered, setHovered] = useState<string>();
  const [focusFallback, setFocusFallback] = useState(false);
  const heading = useRef<HTMLButtonElement>(null);
  const order = useRef<string[]>([]);
  const contentId = useId();
  const [now, setNow] = useState(Date.now);
  const children = agents.map((value) => value.agent);
  const rows = agents.map(projectAgent);
  const priority = (row: Row) =>
    row.attention ? 0 : row.finished ? 3 : row.running ? 1 : 2;
  const ids = new Set(children.map((agent) => agent.id));
  const admission = [
    ...order.current.filter((id) => ids.has(id)),
    ...children
      .map((agent) => agent.id)
      .filter((id) => !order.current.includes(id)),
  ];
  rows.sort(
    (a, b) =>
      (!focused && !hovered ? priority(a) - priority(b) : 0) ||
      admission.indexOf(a.agent.id) - admission.indexOf(b.agent.id),
  );
  useLayoutEffect(() => {
    order.current = rows.map((row) => row.agent.id);
  });
  useLayoutEffect(() => {
    if (focused && !ids.has(focused)) {
      setFocusFallback(true);
      setFocused(undefined);
      heading.current?.focus();
    }
    if (hovered && !ids.has(hovered)) setHovered(undefined);
  });
  const timings = rows.map((row) =>
    turnTiming(row, row.activity.active_turn?.id),
  );
  const ticking =
    expanded &&
    connected &&
    timings.some((timing) => timing && timing.end === undefined);
  useEffect(() => {
    if (!ticking) return;
    let timer: ReturnType<typeof setInterval> | undefined;
    const update = () => {
      clearInterval(timer);
      timer = undefined;
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
  }, [ticking]);
  if (!rows.length && !partial && !focused && !focusFallback) return null;
  const counts = [
    [rows.filter((row) => row.attention).length, 'needs attention'],
    [
      rows.filter((row) => row.text === 'Working' && !row.attention).length,
      'working',
    ],
    [
      rows.filter((row) => row.text === 'Queued' && !row.attention).length,
      'queued',
    ],
    [
      rows.filter((row) => row.text === 'Not started' && !row.attention).length,
      'not started',
    ],
    [rows.filter((row) => row.finished).length, 'finished'],
    [
      rows.filter(
        (row) =>
          !row.finished &&
          !row.attention &&
          !['Working', 'Queued', 'Not started'].includes(row.text),
      ).length,
      'other',
    ],
  ] as const;
  const summary = [
    'Agents',
    ...(!connected
      ? ['Updates paused']
      : partial
        ? ['Partial agent list']
        : counts
            .filter(([count]) => count)
            .map(([count, label]) => `${count} ${label}`)),
  ].join(' · ');
  return (
    <section
      aria-label="Session agents"
      data-agent-dock
      {...stylex.props(composerPanels.surface, styles.dock)}
    >
      <div data-agent-dock-content {...stylex.props(styles.content)}>
        <button
          ref={heading}
          type="button"
          {...stylex.props(styles.header)}
          aria-expanded={expanded}
          aria-controls={contentId}
          title={summary}
          onBlur={() => setFocusFallback(false)}
          onClick={() => {
            setNow(Date.now());
            setExpanded(!expanded);
            setHovered(undefined);
          }}
        >
          {expanded ? (
            <ChevronDown size={14} aria-hidden="true" />
          ) : (
            <ChevronRight size={14} aria-hidden="true" />
          )}
          <span {...stylex.props(styles.summary)}>{summary}</span>
        </button>
        <div id={contentId} hidden={!expanded}>
          {expanded && (
            <div data-agent-dock-rows {...stylex.props(styles.rows)}>
              {rows.map((row, index) => {
                const { agent, text, attention, running } = row;
                const open = agent.id === openAgentId;
                const activity =
                  row.activity.active_turn?.kind === 'host_operation'
                    ? 'Direct human operation'
                    : '';
                const timing = timings[index];
                const duration =
                  timing && (timing.end !== undefined || connected)
                    ? formatElapsed((timing.end ?? now) - timing.start)
                    : undefined;
                const durationLabel = duration
                  ? `${duration} ${timing?.end === undefined ? 'elapsed in current turn' : 'in latest completed turn'}`
                  : '';
                const label = `${agent.definition.id || agent.id} · ${text}${activity ? ` · ${activity}` : ''}${durationLabel ? ` · ${durationLabel}` : ''}${!connected ? ' · Updates paused' : ''}${open ? ' · Open' : ''} · Open chat in right split`;
                return (
                  <Button
                    key={agent.id}
                    variant="ghost"
                    size="sm"
                    xstyle={[styles.row, open && styles.selected]}
                    data-agent-dock-row={agent.id}
                    aria-label={label}
                    aria-current={open ? 'true' : undefined}
                    title={label}
                    onClick={() => onAgent(agent.id)}
                    onFocus={() => setFocused(agent.id)}
                    onBlur={() => setFocused(undefined)}
                    onPointerEnter={() => setHovered(agent.id)}
                    onPointerLeave={() => setHovered(undefined)}
                  >
                    <span
                      {...stylex.props(
                        styles.marker,
                        connected && attention && styles.attention,
                      )}
                    >
                      {attention ? (
                        <ShieldAlert size={12} aria-hidden="true" />
                      ) : connected && running ? (
                        <Spinner size={10} label="Agent is busy" />
                      ) : (
                        <Circle size={5} aria-hidden="true" />
                      )}
                    </span>
                    <span {...stylex.props(styles.name)}>
                      {agent.definition.id || agent.id}
                    </span>
                    <span
                      data-agent-dock-status
                      {...stylex.props(styles.status)}
                    >
                      {text}
                    </span>
                    <span
                      data-agent-dock-calls
                      {...stylex.props(styles.calls)}
                      title={activity || undefined}
                    >
                      {activity}
                    </span>
                    <span
                      data-agent-dock-duration
                      {...stylex.props(styles.time)}
                      title={durationLabel || 'Turn duration unavailable'}
                    >
                      {duration ?? '—'}
                    </span>
                  </Button>
                );
              })}
            </div>
          )}
          {expanded && partial && (
            <Button
              variant="ghost"
              size="sm"
              xstyle={styles.disclosure}
              onClick={onAllAgents}
            >
              Partial agent list · See all agents
            </Button>
          )}
        </div>
      </div>
    </section>
  );
}

const styles = stylex.create({
  dock: {
    containerType: 'inline-size',
    fontSize: typography.size12,
    color: surface.secondaryText,
  },
  content: { paddingInline: 4 },
  header: {
    backgroundColor: 'transparent',
    borderWidth: 0,
    borderRadius: scale.radiusControl,
    fontFamily: 'inherit',
    textAlign: 'start',
    cursor: 'pointer',
    outlineOffset: 2,
    paddingBlock: 4,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'flex-start',
    width: '100%',
    minWidth: 0,
    height: 'auto',
    minHeight: { default: 32, '@media (pointer: coarse)': 44 },
    gap: 8,
    paddingInline: 8,
    fontSize: typography.size13,
    fontWeight: 400,
    color: surface.secondaryText,
  },
  summary: {
    minWidth: 0,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  rows: {
    display: 'flex',
    flexDirection: 'column',
    minWidth: 0,
    maxHeight: 'min(200px, 20dvh)',
    overflowY: 'auto',
    overscrollBehavior: 'contain',
    scrollbarGutter: 'stable',
  },
  row: {
    display: 'grid',
    gridTemplateColumns: {
      default: '12px minmax(0, 1.4fr) minmax(0, 1fr) 7ch 8ch',
      '@container (max-width: 440px)': '12px minmax(0, 1fr) 8ch',
    },
    alignItems: 'center',
    width: '100%',
    minWidth: 0,
    height: 'auto',
    minHeight: { default: 32, '@media (pointer: coarse)': 44 },
    gap: 8,
    justifyContent: 'flex-start',
    paddingBlock: 4,
    paddingInline: 8,
    borderRadius: 6,
    outlineOffset: -2,
    textAlign: 'start',
    fontSize: typography.size13,
    fontWeight: 400,
    backgroundColor: { default: 'transparent', ':hover': colors.hover },
  },
  selected: {
    backgroundColor: { default: colors.hover, ':hover': colors.hover },
  },
  name: {
    minWidth: 0,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
    color: colors.foreground,
  },
  status: {
    gridColumn: { '@container (max-width: 440px)': 2 },
    gridRow: { '@container (max-width: 440px)': 2 },
    minWidth: 0,
    fontSize: typography.size12,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
    color: surface.secondaryText,
  },
  calls: {
    gridColumn: { '@container (max-width: 440px)': 3 },
    gridRow: { '@container (max-width: 440px)': 2 },
    minWidth: 0,
    fontSize: typography.size12,
    color: surface.secondaryText,
    textAlign: 'end',
    fontVariantNumeric: 'tabular-nums',
    whiteSpace: 'nowrap',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
  },
  time: {
    gridColumn: { '@container (max-width: 440px)': 3 },
    gridRow: { '@container (max-width: 440px)': 1 },
    minWidth: 0,
    fontSize: typography.size12,
    color: surface.secondaryText,
    textAlign: 'end',
    fontVariantNumeric: 'tabular-nums',
    whiteSpace: 'nowrap',
  },
  marker: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    width: 12,
    flexShrink: 0,
    color: surface.secondaryText,
  },
  attention: { color: colors.warning },
  disclosure: {
    paddingInline: 8,
    gap: 8,
    fontWeight: 400,
    justifyContent: 'flex-start',
    fontSize: typography.size12,
    color: surface.secondaryText,
    whiteSpace: 'normal',
    textAlign: 'start',
    height: 'auto',
    minHeight: { default: 28, '@media (pointer: coarse)': 44 },
  },
});
