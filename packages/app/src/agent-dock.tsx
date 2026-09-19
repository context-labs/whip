import { useLayoutEffect, useRef, useState } from 'react';
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

function AgentDockRoster({ state, agentId, connected, onAgent, onAllAgents, openAgentId }: AgentDockProps) {
  const children = (state.root?.agents ?? []).filter(agent => agent.parent_id === agentId && agent.id !== agentId && agent.status !== 'deleted');
  const ids = children.map(agent => agent.id);
  const [admission, setAdmission] = useState(ids);
  const admitted = admission.filter(id => ids.includes(id));
  admitted.push(...ids.filter(id => !admission.includes(id)));
  if (admitted.length !== admission.length || admitted.some((id, index) => id !== admission[index])) setAdmission(admitted);
  const [expanded, setExpanded] = useState(false);
  const [focused, setFocused] = useState<string>();
  const [hovered, setHovered] = useState<string>();
  const [focusFallback, setFocusFallback] = useState(false);
  const slots = useRef<string[]>([]);
  const heading = useRef<HTMLSpanElement>(null);
  const rows = children.map(agent => projectAgent(state.root!, agent));
  const byId = new Map(rows.map(row => [row.agent.id, row]));
  const interacting = !!focused || !!hovered;
  // Keep the entire compact set still while it contains a pointer/focus target.
  const retained = interacting ? slots.current.filter(id => byId.has(id)) : [];
  const candidates = rows.filter(row => !row.finished)
    .filter(row => !(interacting && [focused, hovered].includes(row.agent.id) && !retained.includes(row.agent.id)))
    .sort((a, b) => Number(b.attention) - Number(a.attention) || admitted.indexOf(a.agent.id) - admitted.indexOf(b.agent.id));
  const compactIds = [...retained, ...candidates.map(row => row.agent.id).filter(id => !retained.includes(id))].slice(0, 3);
  const compact = compactIds.map(id => byId.get(id)!);
  const finished = rows.filter(row => !compactIds.includes(row.agent.id) && (row.finished || [focused, hovered].includes(row.agent.id)))
    .sort((a, b) => admitted.indexOf(a.agent.id) - admitted.indexOf(b.agent.id));
  const overflow = rows.length - compact.length - finished.length;
  const partial = !!state.root?.omitted?.agents;
  const workingCount = rows.filter(row => row.text === 'Working' && !row.attention).length;
  const queuedCount = rows.filter(row => row.text === 'Queued' && !row.attention).length;
  const attentionCount = rows.filter(row => row.attention).length;
  const summary = [workingCount && `${workingCount} working`, queuedCount && `${queuedCount} queued`, attentionCount && `${attentionCount} needs attention`].filter(Boolean).join(' · ');
  useLayoutEffect(() => { slots.current = compactIds; });
  useLayoutEffect(() => {
    if (focused && !byId.has(focused)) {
      setFocusFallback(true);
      setFocused(undefined);
      heading.current?.focus();
    }
    if (hovered && !byId.has(hovered)) setHovered(undefined);
  });
  if (!rows.length && !partial && !focused && !focusFallback) return null;

  const rowButton = (row: Row, settled = false) => {
    const { agent, text, attention, running } = row;
    const open = agent.id === openAgentId;
    const calls = agent.last_turn?.model_calls;
    const activity = calls === undefined ? '' : `${calls} model ${calls === 1 ? 'call' : 'calls'} in latest turn`;
    const label = `${agent.name || agent.id} · ${text}${activity ? ` · ${activity}` : ''}${!connected ? ' · Updates paused' : ''}${open ? ' · Open' : ''} · Open chat in right split`;
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
      <span data-agent-dock-status {...stylex.props(styles.status)}>{(!settled || !['Completed', 'Idle'].includes(text)) ? text : ''}</span>
      <span data-agent-dock-calls {...stylex.props(styles.calls)} title={activity || undefined}>
        {calls !== undefined && `${calls} ${calls === 1 ? 'call' : 'calls'}`}
      </span>
    </Button>;
  };
  return <section aria-label="Session agents" data-agent-dock {...stylex.props(styles.dock)}>
    <div {...stylex.props(styles.header)}>
      <span ref={heading} tabIndex={-1} onBlur={() => setFocusFallback(false)}>Agents</span>
      {connected && !partial && summary && <span {...stylex.props(styles.note)}>· {summary}</span>}
      {!connected && <span {...stylex.props(styles.note)}>Updates paused</span>}
    </div>
    {partial && <Button variant="ghost" size="sm" xstyle={styles.disclosure} onClick={onAllAgents}>Partial agent list · See all agents</Button>}
    <div {...stylex.props(styles.compact)}>{compact.map(row => rowButton(row))}</div>
    {(overflow > 0 || finished.length > 0) && <div data-agent-dock-actions {...stylex.props(styles.actions)}>
      {overflow > 0 && <Button variant="ghost" size="sm" xstyle={[styles.disclosure, styles.footerButton]} title="See all agents" onClick={onAllAgents}>
        More ({overflow}{partial ? '+' : ''})
      </Button>}
      {finished.length > 0 && <Button variant="ghost" size="sm" xstyle={[styles.disclosure, styles.footerButton]} aria-expanded={expanded} onClick={() => setExpanded(!expanded)}>
        {expanded ? <ChevronDown size={12} aria-hidden="true" /> : <ChevronRight size={12} aria-hidden="true" />}
        Finished ({finished.length}{partial ? '+' : ''})
      </Button>}
    </div>}
    {finished.length > 0 && expanded && <div aria-label="Finished agents" {...stylex.props(styles.finished)}>{finished.map(row => rowButton(row, true))}</div>}
  </section>;
}

const styles = stylex.create({
  dock: { width: '100%', maxWidth: 864, alignSelf: 'center', minWidth: 0, maxHeight: '30dvh', overflowY: 'auto', overscrollBehavior: 'contain', flexShrink: 0, paddingInline: { default: 24, [scale.phone]: 12 }, paddingBlock: 4, borderTopWidth: 1, borderTopStyle: 'solid', borderTopColor: surface.quietBorder, fontSize: typography.size12, color: surface.secondaryText },
  header: { display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: 8, minWidth: 0, minHeight: 28, paddingInlineStart: 8 },
  note: { margin: 0, fontSize: typography.size12, color: surface.secondaryText, overflowWrap: 'anywhere' },
  compact: { display: 'flex', flexDirection: 'column', minWidth: 0 },
  row: { display: 'grid', gridTemplateColumns: '12px minmax(0, 1fr) minmax(0, 1.4fr) 8ch', alignItems: 'center', width: '100%', minWidth: 0, height: 'auto', minHeight: { default: 32, '@media (pointer: coarse)': 44 }, gap: 8, justifyContent: 'flex-start', paddingBlock: 4, paddingInline: 8, borderRadius: 6, textAlign: 'start', fontSize: typography.size13, fontWeight: 400, backgroundColor: { default: 'transparent', ':hover': colors.element } },
  selected: { backgroundColor: { default: colors.hover, ':hover': colors.hover } },
  name: { minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: colors.foreground },
  status: { minWidth: 0, fontSize: typography.size12, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: surface.secondaryText },
  calls: { minWidth: 0, fontSize: typography.size12, color: surface.secondaryText, textAlign: 'end', fontVariantNumeric: 'tabular-nums', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' },
  marker: { display: 'inline-flex', alignItems: 'center', justifyContent: 'center', width: 12, flexShrink: 0, color: surface.secondaryText },
  attention: { color: colors.warning },
  disclosure: { paddingInline: 8, gap: 8, fontWeight: 400, justifyContent: 'flex-start', fontSize: typography.size12, color: surface.secondaryText, whiteSpace: 'normal', textAlign: 'start', height: 'auto', minHeight: { default: 28, '@media (pointer: coarse)': 44 } },
  actions: { display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 },
  footerButton: { height: { default: 28, '@media (pointer: coarse)': 44 }, alignItems: 'center', whiteSpace: 'nowrap', flexShrink: 0 },
  finished: { display: 'flex', flexDirection: 'column', minWidth: 0, maxHeight: 112, overflowY: 'auto', overscrollBehavior: 'contain' },
});
