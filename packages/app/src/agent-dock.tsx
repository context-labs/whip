import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import type { DeepReadonly, SessionViewSnapshot } from '@whip/sdk/state';
import type { RootSnapshot } from '@whip/protocol';
import { Button, Spinner } from '@whip/ui';
import { ChevronDown, ChevronRight, Circle, ShieldAlert } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface, typography } from '@whip/ui/tokens.stylex';
import { agentStatus } from './chat-activity';

type Agent = DeepReadonly<NonNullable<RootSnapshot['agents']>[number]>;
type Row = { agent: Agent; text: string; attention: boolean; running: boolean; finished: boolean };
export interface AgentDockProps {
  state: DeepReadonly<SessionViewSnapshot>;
  agentId: string;
  connected: boolean;
  onAgent(id: string): void;
  onAllAgents(): void;
  openAgentId?: string;
}

function projectAgent(root: DeepReadonly<RootSnapshot>, agent: Agent): Row {
  const stopped = ['stopped', 'cancelled', 'interrupted', 'succeeded'].includes(agent.status);
  const approval = !stopped && root.permissions?.some(item => item.agent_id === agent.id && item.status === 'pending');
  const question = !stopped && root.questions?.some(item => item.agent_id === agent.id && item.question_id);
  const active = !stopped && (!!root.active_turns[agent.id] || agent.status === 'running');
  const queued = !stopped && agent.status !== 'failed' && (agent.status === 'queued' || agent.lifecycle_phase === 'queued'
    || !!root.inbox?.some(item => item.agent_id === agent.id && item.status === 'queued'));
  const waiting = !stopped && (!!approval || !!question || !!agent.blocking_reason);
  const notStarted = !active && !queued && !waiting && !agent.last_turn && ['idle', 'ready'].includes(agent.status);
  const text = stopped ? (agent.status === 'succeeded' ? 'Completed' : agent.status.charAt(0).toUpperCase() + agent.status.slice(1))
    : approval ? 'Waiting for your approval' : question ? 'Waiting for your answer'
    : !active && agent.status === 'failed' ? 'Failed'
    : queued && !active && !waiting ? 'Queued'
    : notStarted ? 'Not started' : agentStatus(agent, active, true, { includeActivity: false });
  const running = active || queued;
  const failed = !stopped && !running && (agent.status === 'failed' || agent.last_turn?.status === 'failed');
  // Idle is not an outcome: a newly created child may still be awaiting admission.
  const settled = stopped || ['succeeded', 'cancelled', 'interrupted'].includes(agent.last_turn?.status ?? '');
  return { agent, text, running, attention: waiting || failed, finished: settled && !waiting && !running && !failed };
}

/** Snapshot metadata only: opening a child, not mounting this roster, acquires its history. */
export function AgentDock(props: AgentDockProps) {
  return <AgentDockRoster key={`${props.state.root?.root_id}:${props.agentId}`} {...props} />;
}

/** Latest-turn wall time, never agent lifetime or a queued turn's predecessor. */
function turnTiming(row: Row, activeTurn?: string) {
  const turn = row.agent.last_turn;
  if (!turn || (activeTurn && turn.turn_id !== activeTurn) || row.text === 'Queued' || row.text === 'Not started') return;
  const start = Date.parse(turn.started_at ?? '');
  const end = Date.parse(turn.finished_at ?? '');
  if (!Number.isFinite(start)) return;
  if (Number.isFinite(end)) {
    if (row.running || ['running', 'waiting'].includes(turn.status)) return;
    return end >= start ? { start, end } : undefined;
  }
  if (!turn.finished_at && ['running', 'waiting'].includes(turn.status) && !row.finished && row.text !== 'Failed') return { start, end: undefined };
}

function formatElapsed(ms: number) {
  const seconds = Math.max(0, Math.floor(ms / 1000));
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  return hours ? `${hours}h ${minutes % 60}m` : minutes ? `${minutes}m ${seconds % 60}s` : `${seconds}s`;
}

function AgentDockRoster({ state, agentId, connected, onAgent, onAllAgents, openAgentId }: AgentDockProps) {
  const [expanded, setExpanded] = useState(false);
  const [focused, setFocused] = useState<string>();
  const [hovered, setHovered] = useState<string>();
  const [focusFallback, setFocusFallback] = useState(false);
  const heading = useRef<HTMLButtonElement>(null);
  const order = useRef<string[]>([]);
  const contentId = useId();
  const [now, setNow] = useState(Date.now);
  const children = (state.root?.agents ?? []).filter(agent => agent.parent_id === agentId && agent.id !== agentId && agent.status !== 'deleted');
  const rows = children.map(agent => projectAgent(state.root!, agent));
  const priority = (row: Row) => row.attention ? 0 : row.finished ? 3 : row.running ? 1 : 2;
  const ids = new Set(children.map(agent => agent.id));
  const admission = [...order.current.filter(id => ids.has(id)), ...children.map(agent => agent.id).filter(id => !order.current.includes(id))];
  rows.sort((a, b) => ((!focused && !hovered) ? priority(a) - priority(b) : 0) || admission.indexOf(a.agent.id) - admission.indexOf(b.agent.id));
  useLayoutEffect(() => { order.current = rows.map(row => row.agent.id); });
  useLayoutEffect(() => {
    if (focused && !ids.has(focused)) {
      setFocusFallback(true);
      setFocused(undefined);
      heading.current?.focus();
    }
    if (hovered && !ids.has(hovered)) setHovered(undefined);
  });
  const timings = rows.map(row => turnTiming(row, state.root?.active_turns[row.agent.id]));
  const ticking = expanded && connected && timings.some(timing => timing && timing.end === undefined);
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
    return () => { clearInterval(timer); document.removeEventListener('visibilitychange', update); };
  }, [ticking]);
  const partial = !!state.root?.omitted?.agents;
  if (!rows.length && !partial && !focused && !focusFallback) return null;
  const counts = [
    [rows.filter(row => row.attention).length, 'needs attention'],
    [rows.filter(row => row.text === 'Working' && !row.attention).length, 'working'],
    [rows.filter(row => row.text === 'Queued' && !row.attention).length, 'queued'],
    [rows.filter(row => row.text === 'Not started' && !row.attention).length, 'not started'],
    [rows.filter(row => row.finished).length, 'finished'],
    [rows.filter(row => !row.finished && !row.attention && !['Working', 'Queued', 'Not started'].includes(row.text)).length, 'other'],
  ] as const;
  const summary = ['Agents', ...(!connected ? ['Updates paused'] : partial ? ['Partial agent list'] : counts.filter(([count]) => count).map(([count, label]) => `${count} ${label}`))].join(' · ');
  return <section aria-label="Session agents" data-agent-dock {...stylex.props(styles.dock)}>
    <div data-agent-dock-content {...stylex.props(styles.content)}>
      <button ref={heading} type="button" {...stylex.props(styles.header)} aria-expanded={expanded} aria-controls={contentId}
        title={summary} onBlur={() => setFocusFallback(false)} onClick={() => { setExpanded(!expanded); setHovered(undefined); }}>
        {expanded ? <ChevronDown size={14} aria-hidden="true" /> : <ChevronRight size={14} aria-hidden="true" />}
        <span {...stylex.props(styles.summary)}>{summary}</span>
      </button>
      <div id={contentId} hidden={!expanded}>
        {expanded && <div data-agent-dock-rows {...stylex.props(styles.rows)}>{rows.map((row, index) => {
          const { agent, text, attention, running } = row;
          const open = agent.id === openAgentId;
          const calls = agent.last_turn?.model_calls;
          const activity = calls === undefined ? '' : `${calls} model ${calls === 1 ? 'call' : 'calls'} in latest turn`;
          const timing = timings[index];
          const duration = timing && (timing.end !== undefined || connected) ? formatElapsed((timing.end ?? now) - timing.start) : undefined;
          const durationLabel = duration ? `${duration} ${timing?.end === undefined ? 'elapsed in current turn' : 'in latest completed turn'}` : '';
          const label = `${agent.name || agent.id} · ${text}${activity ? ` · ${activity}` : ''}${durationLabel ? ` · ${durationLabel}` : ''}${!connected ? ' · Updates paused' : ''}${open ? ' · Open' : ''} · Open chat in right split`;
          return <Button key={agent.id} variant="ghost" size="sm" xstyle={[styles.row, open && styles.selected]}
            data-agent-dock-row={agent.id} aria-label={label} aria-current={open ? 'true' : undefined}
            title={label} onClick={() => onAgent(agent.id)}
            onFocus={() => setFocused(agent.id)} onBlur={() => setFocused(undefined)}
            onPointerEnter={() => setHovered(agent.id)} onPointerLeave={() => setHovered(undefined)}>
            <span {...stylex.props(styles.marker, connected && attention && styles.attention)}>
              {attention ? <ShieldAlert size={12} aria-hidden="true" />
                : connected && running ? <Spinner size={10} label="Agent is busy" />
                : <Circle size={5} aria-hidden="true" />}
            </span>
            <span {...stylex.props(styles.name)}>{agent.name || agent.id}</span>
            <span data-agent-dock-status {...stylex.props(styles.status)}>{text}</span>
            <span data-agent-dock-calls {...stylex.props(styles.calls)} title={activity || undefined}>
              {calls !== undefined && `${calls} ${calls === 1 ? 'call' : 'calls'}`}
            </span>
            <span data-agent-dock-duration {...stylex.props(styles.time)} title={durationLabel || 'Turn duration unavailable'}>{duration ?? '—'}</span>
          </Button>;
        })}</div>}
        {expanded && partial && <Button variant="ghost" size="sm" xstyle={styles.disclosure} onClick={onAllAgents}>Partial agent list · See all agents</Button>}
      </div>
    </div>
  </section>;
}

const styles = stylex.create({
  dock: { containerType: 'inline-size', width: '100%', maxWidth: 864, alignSelf: 'center', minWidth: 0, flexShrink: 0, paddingInline: { default: 24, [scale.phone]: 12 }, fontSize: typography.size12, color: surface.secondaryText },
  content: { paddingTop: 4, borderTopWidth: 1, borderTopStyle: 'solid', borderTopColor: surface.quietBorder },
  header: { backgroundColor: 'transparent', borderWidth: 0, borderRadius: scale.radiusControl, fontFamily: 'inherit', textAlign: 'start', cursor: 'pointer', outlineOffset: 2, paddingBlock: 4, display: 'flex', alignItems: 'center', justifyContent: 'flex-start', width: '100%', minWidth: 0, height: 'auto', minHeight: { default: 32, '@media (pointer: coarse)': 44 }, gap: 8, paddingInline: 8, fontSize: typography.size13, fontWeight: 400, color: surface.secondaryText },
  summary: { minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
  rows: { display: 'flex', flexDirection: 'column', minWidth: 0, maxHeight: 'min(240px, 28dvh)', overflowY: 'auto', overscrollBehavior: 'contain', scrollbarGutter: 'stable' },
  row: { display: 'grid', gridTemplateColumns: { default: '12px minmax(0, 1.4fr) minmax(0, 1fr) 7ch 8ch', '@container (max-width: 440px)': '12px minmax(0, 1fr) 8ch' }, alignItems: 'center', width: '100%', minWidth: 0, height: 'auto', minHeight: { default: 32, '@media (pointer: coarse)': 44 }, gap: 8, justifyContent: 'flex-start', paddingBlock: 4, paddingInline: 8, borderRadius: 6, textAlign: 'start', fontSize: typography.size13, fontWeight: 400, backgroundColor: { default: 'transparent', ':hover': colors.element } },
  selected: { backgroundColor: { default: colors.hover, ':hover': colors.hover } },
  name: { minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: colors.foreground },
  status: { gridColumn: { '@container (max-width: 440px)': 2 }, gridRow: { '@container (max-width: 440px)': 2 }, minWidth: 0, fontSize: typography.size12, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: surface.secondaryText },
  calls: { gridColumn: { '@container (max-width: 440px)': 3 }, gridRow: { '@container (max-width: 440px)': 2 }, minWidth: 0, fontSize: typography.size12, color: surface.secondaryText, textAlign: 'end', fontVariantNumeric: 'tabular-nums', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' },
  time: { gridColumn: { '@container (max-width: 440px)': 3 }, gridRow: { '@container (max-width: 440px)': 1 }, minWidth: 0, fontSize: typography.size12, color: surface.secondaryText, textAlign: 'end', fontVariantNumeric: 'tabular-nums', whiteSpace: 'nowrap' },
  marker: { display: 'inline-flex', alignItems: 'center', justifyContent: 'center', width: 12, flexShrink: 0, color: surface.secondaryText },
  attention: { color: colors.warning },
  disclosure: { paddingInline: 8, gap: 8, fontWeight: 400, justifyContent: 'flex-start', fontSize: typography.size12, color: surface.secondaryText, whiteSpace: 'normal', textAlign: 'start', height: 'auto', minHeight: { default: 28, '@media (pointer: coarse)': 44 } },
});
