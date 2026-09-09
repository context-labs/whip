import { useLayoutEffect, useRef } from 'react';
import type { DeepReadonly, ExecutionCell, ExecutionRow, SessionViewSnapshot } from '@whip/sdk/state';
import type { RootSnapshot } from '@whip/protocol';
import { ActivityIndicator, Button, CodeBlock, IconButton, useTheme } from '@whip/ui';
import { ArrowUpRight, Check, ChevronRight, Circle, CircleAlert, MessagesSquare, Pause, Play, ShieldAlert } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { appearance, colors, scale, surface, typography } from '@whip/ui/tokens.stylex';
import { cellActivityLabel, executionActive, type ActivityGroup } from './chat-activity-rows';
import type { TimelineRow } from './conversation-rows';
import { ExecutionTime } from './execution-time';

type Agent = DeepReadonly<NonNullable<RootSnapshot['agents']>[number]>;
export function activityStatus(state: DeepReadonly<SessionViewSnapshot>, agentId: string, cells: readonly ExecutionRow[], connected: boolean, agent?: Agent) {
  const root = state.root;
  if (!root) return { text: 'Connecting…', active: false };
  if (!connected) return { text: 'Reconnecting · activity updates paused', active: false };
  if (root.permissions?.some(item => item.status === 'pending')) return { text: 'Waiting for your approval', active: false, attention: true };
  if (root.questions?.some(item => item.question_id)) return { text: 'Waiting for your answer', active: false, attention: true };
  const turn = root.active_turns[agentId];
  if (turn) {
    const current = cells.filter((row): row is ExecutionCell => row.kind === 'cell' && executionActive(row) && (!row.turnId || row.turnId === turn)).at(-1);
    if (current) return { text: cellActivityLabel(current), active: true, cell: current };
    const presentation = agentId === root.root_id ? root.presentation : root.agent_presentations[agentId];
    const last = presentation?.at(-1);
    return { text: last?.kind === 'stream.text' ? 'Writing a response' : last?.kind === 'stream.reasoning' ? 'Thinking' : 'Working', active: true };
  }
  if (root.inbox?.some(item => item.agent_id === agentId && item.status === 'queued')) return { text: 'Queued · waiting to start', active: false };
  if (agent?.last_turn && ['failed', 'cancelled', 'interrupted'].includes(agent.last_turn.status)) {
    return { text: `Last turn ${agent.last_turn.status}`, active: false, attention: agent.last_turn.status === 'failed' };
  }
  const working = root.agents?.filter(item => item.id !== agentId && (root.active_turns[item.id] || item.status === 'running')).length ?? 0;
  if (working) return { text: `${root.omitted?.agents ? 'At least ' : ''}${working} ${working === 1 ? 'agent still working' : 'agents still working'}`, active: true };
  return { text: '', active: false };
}

function agentStatus(agent: Agent, active: boolean, connected: boolean): string {
  if (!connected) return 'Updates paused';
  if (agent.blocking_reason) return `Waiting · ${agent.blocking_reason.replaceAll('_', ' ')}`;
  if (active || agent.status === 'running') return 'Working';
  if (agent.lifecycle_phase === 'queued' || agent.status === 'queued') return 'Queued';
  const outcome = agent.last_turn?.status;
  if (outcome) return outcome === 'succeeded' ? 'Completed' : outcome.charAt(0).toUpperCase() + outcome.slice(1);
  return 'Idle';
}

export function ChatActivity({ state, cells, agentId, agent, connected, onAgent, onAllAgents }: {
  state: DeepReadonly<SessionViewSnapshot>; cells: readonly ExecutionRow[]; agentId: string; agent?: Agent; connected: boolean;
  onAgent(id: string): void; onAllAgents(): void;
}) {
  const { display, setDisplay } = useTheme();
  const status = activityStatus(state, agentId, cells, connected, agent);
  const root = state.root;
  const admitted = useRef<string[]>([]);
  const region = useRef<HTMLElement>(null);
  const focused = typeof document !== 'undefined' && region.current?.contains(document.activeElement)
    ? document.activeElement?.closest('[data-activity-agent]')?.getAttribute('data-activity-agent') : null;
  const relevant = (item: Agent) => !!root?.active_turns[item.id] || ['running', 'queued'].includes(item.status)
    || item.lifecycle_phase === 'queued' || !!item.blocking_reason || item.last_turn?.status === 'failed' || item.id === focused;
  const priority = (item: Agent) => item.blocking_reason || (!root?.active_turns[item.id] && item.last_turn?.status === 'failed') ? 0 : 1;
  const previousIndex = (item: Agent) => { const index = admitted.current.indexOf(item.id); return index < 0 ? Infinity : index; };
  const children = [...(root?.agents?.filter(item => item.parent_id === agentId && !['deleted', 'stopped'].includes(item.status) && relevant(item)) ?? [])]
    .sort((a, b) => (focused ? 0 : priority(a) - priority(b)) || previousIndex(a) - previousIndex(b));
  useLayoutEffect(() => { admitted.current = children.slice(0, 3).map(item => item.id); }, [children]);
  if (!status.text && !children.length) return null;
  return <section ref={region} aria-label="Current activity" data-chat-activity {...stylex.props(styles.dock)}>
    <div {...stylex.props(styles.statusRow)}>
      {status.attention ? <ShieldAlert size={16} aria-hidden="true" /> : status.active ? <ActivityIndicator active reduceMotion={display.motion === 'reduce'} /> : <Circle size={12} aria-hidden="true" />}
      <span role="status" aria-live="polite" aria-atomic="true">{status.text || 'Session agents'}</span>
      {status.cell && <ExecutionTime cell={status.cell} connected={connected} />}
      {status.active && <IconButton variant="ghost" size="sm" label={display.motion === 'reduce' ? 'Use system motion setting' : 'Pause activity animation'}
        onClick={() => setDisplay({ motion: display.motion === 'reduce' ? 'system' : 'reduce' })}>
        {display.motion === 'reduce' ? <Play size={12} /> : <Pause size={12} />}
      </IconButton>}
    </div>
    {!!children.length && <div aria-label="Session agents" {...stylex.props(styles.agents)}>
      {status.text && <span {...stylex.props(styles.caption)}>Session agents</span>}
      {children.slice(0, 3).map(child => <button key={child.id} data-activity-agent={child.id} type="button" {...stylex.props(styles.agent)} onClick={() => onAgent(child.id)}>
        <Circle size={6} fill="currentColor" aria-hidden="true" />
        <span {...stylex.props(styles.agentName)}>{child.name || 'Unnamed agent'}</span>
        <span {...stylex.props(styles.caption, styles.agentModel)}>{[child.model, child.effort].filter(Boolean).join(' · ')}</span>
        <span {...stylex.props(styles.caption)}>{agentStatus(child, !!root?.active_turns[child.id], connected)}</span>
        <ArrowUpRight size={12} aria-hidden="true" />
      </button>)}
      {(children.length > 3 || root?.omitted?.agents) && <Button size="sm" variant="ghost" onClick={onAllAgents}>View all session agents</Button>}
    </div>}
  </section>;
}

export function ActivityGroupRow({ group, open, onToggle, onOpenRepl, connected, density, readBody }: {
  group: ActivityGroup; open: boolean; onToggle(): void; onOpenRepl?(): void; connected: boolean;
  density: 'compact' | 'comfortable' | 'detailed'; readBody(row: TimelineRow): void;
}) {
  const active = group.cells.filter(executionActive).at(-1);
  const failed = group.cells.filter(cell => cell.status === 'failed').length;
  const interrupted = group.cells.some(cell => cell.status === 'cancelled' || cell.status === 'interrupted');
  const uncertain = group.cells.some(cell => cell.status === 'unknown' || cell.truncated || cell.historyUnmatched);
  const updates = group.updates ?? [];
  const title = !group.cells.length ? 'Agent updates' : active ? connected ? cellActivityLabel(active) : 'Activity updates paused'
    : failed ? 'Execution failed' : interrupted ? 'Execution stopped' : uncertain ? 'Execution activity' : 'Completed work';
  const preview = active ?? group.cells.at(-1);
  const windowAnchor = useRef(group.cells.slice(-6)[0]?.id);
  useLayoutEffect(() => { if (!open) windowAnchor.current = group.cells.slice(-6)[0]?.id; }, [open, group.cells]);
  const windowStart = Math.max(0, group.cells.findIndex(cell => cell.id === windowAnchor.current));
  const host = preview?.hosts.filter(item => item.status === 'running').at(-1) ?? preview?.hosts.at(-1);
  const previewElement = useRef<HTMLSpanElement>(null);
  const previousSnippet = useRef('');
  const selection = typeof window === 'undefined' ? null : window.getSelection();
  const pinned = !!previewElement.current && (previewElement.current === document.activeElement
    || (!!selection && !selection.isCollapsed && (previewElement.current.contains(selection.anchorNode) || previewElement.current.contains(selection.focusNode))));
  const snippet = pinned ? previousSnippet.current : (preview?.error || host?.summary || preview?.output || preview?.code || '').slice(0, 512).split('\n').slice(0, 3).join('\n');
  useLayoutEffect(() => { previousSnippet.current = snippet; }, [snippet]);
  const hostErrors = group.cells.reduce((count, cell) => count + cell.hosts.filter(item => item.status === 'failed').length, 0);
  return <article data-activity-group={group.id} data-message-role="activity" {...stylex.props(styles.group)}>
    <details open={open}>
      <summary {...stylex.props(styles.summary)} onClick={event => {
        event.preventDefault();
        const selected = window.getSelection();
        if (event.detail > 0 && selected && !selected.isCollapsed && event.currentTarget.contains(selected.anchorNode)) return;
        onToggle();
      }}>
        <ChevronRight size={14} aria-hidden="true" {...stylex.props(styles.chevron, open && styles.openChevron)} />
        {failed ? <CircleAlert size={14} aria-hidden="true" /> : !group.cells.length ? <MessagesSquare size={14} aria-hidden="true" /> : !active && !interrupted && !uncertain ? <Check size={14} aria-hidden="true" /> : null}
        <span>{title}</span>
        {!!group.cells.length && <span {...stylex.props(styles.caption)}>{group.cells.length} {group.cells.length === 1 ? 'execution' : 'executions'}{failed ? ` · ${failed} failed` : ''}{updates.length ? ' · agent updates' : ''}</span>}
        {!!hostErrors && <span {...stylex.props(styles.caption)}>{hostErrors} host {hostErrors === 1 ? 'operation failed' : 'operations failed'}</span>}
        {!open && (active || pinned || density !== 'compact') && snippet && <span ref={previewElement} tabIndex={0} data-tool-preview {...stylex.props(styles.preview)}>{snippet}</span>}
      </summary>
      {open && <div {...stylex.props(styles.expanded)}>
        {!!updates.length && <details data-agent-updates {...stylex.props(styles.updates)}>
          <summary {...stylex.props(styles.summary, styles.updateSummary)}>Agent messages <span {...stylex.props(styles.caption)}>{updates.reduce((count, row) => count + (row.deliveries ?? 1), 0)} {updates.length === 1 && !updates[0]?.deliveries ? 'delivery' : 'deliveries'}</span></summary>
          <p {...stylex.props(styles.caption, styles.updateDescription)}>Context delivered to the agent. This may include reports, questions, or completion notices.</p>
          {updates.map(row => <div key={row.id} data-agent-update={row.id} {...stylex.props(styles.execution)}>
            {row.text && <pre {...stylex.props(styles.output)}>{row.text}</pre>}
            {row.body && <Button size="sm" variant="ghost" xstyle={styles.detailAction} onClick={() => readBody(row)}>Read stored agent messages</Button>}
          </div>)}
        </details>}
        {group.cells.length > 6 && <p {...stylex.props(styles.caption)}>Showing 6 of {group.cells.length} executions. Open the REPL for all loaded details.</p>}
        {group.cells.slice(windowStart, windowStart + 6).map(cell => <div key={cell.id} data-activity-cell={cell.id} {...stylex.props(styles.execution)}>
          {(group.cells.length > 1 || cell.status !== 'completed' || cell.hosts.length > 0) && <div {...stylex.props(styles.executionHeader)}>
            <span>{cell.status === 'running' ? connected ? cellActivityLabel(cell) : 'Updates paused' : cell.status === 'unknown' ? 'Outcome unavailable' : cell.status.charAt(0).toUpperCase() + cell.status.slice(1)}</span>
            {cell.hosts.length > 0 && <span {...stylex.props(styles.caption)}>{cell.hosts.length > 3 ? '3 of ' : ''}{cell.hosts.length} {cell.truncated ? 'retained ' : ''}host {cell.hosts.length === 1 ? 'operation' : 'operations'}</span>}
          </div>}
          {cell.hosts.slice(0, 3).map(item => <div key={item.id} {...stylex.props(styles.host)}>
            <span>{item.name}</span><span {...stylex.props(styles.caption)}>{item.status === 'running' ? connected ? 'Running' : 'Updates paused' : item.status === 'unknown' ? 'Outcome unavailable' : item.status}{item.duration ? ` · ${item.duration}` : ''}</span>
            {item.summary && <span {...stylex.props(styles.hostSummary)}>{item.summary.slice(0, 160)}</span>}
            {item.error && <span {...stylex.props(styles.error)}>{item.error.slice(0, 256)}</span>}
          </div>)}
          {cell.code && <CodeBlock code={cell.code.slice(0, 4096)} language="starlark" label="Starlark" />}
          {cell.output && <pre {...stylex.props(styles.output)}>{cell.output.slice(-2048)}</pre>}
          {cell.error && <p {...stylex.props(styles.error)}>{cell.error.slice(0, 1024)}</p>}
          {(cell.truncated || cell.code.length > 4096 || cell.output.length > 2048 || cell.historyUnmatched) && <span {...stylex.props(styles.caption)}>Showing available execution previews. More detail may be available in the REPL.</span>}
          {cell.body && <Button size="sm" variant="ghost" onClick={() => readBody({ id: cell.id, role: 'tool', text: '', body: { ...cell.body!, media_type: cell.body!.media_type ?? '', source: cell.body!.source ?? '' } })}>Read stored execution</Button>}
        </div>)}
        {onOpenRepl && !!group.cells.length && <Button variant="ghost" size="sm" xstyle={styles.detailAction} onClick={onOpenRepl}>Open in REPL <ArrowUpRight size={13} /></Button>}
      </div>}
    </details>
  </article>;
}

const styles = stylex.create({
  dock: { width: '100%', maxWidth: 840, marginInline: 'auto', paddingBlock: '8px 12px', paddingInline: { default: 32, [scale.phone]: 16 }, flexShrink: 0, minWidth: 0, maxHeight: '30dvh', overflowY: 'auto', color: surface.secondaryText, fontSize: typography.size13, lineHeight: 1.6 },
  statusRow: { display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8, minHeight: 28, overflowWrap: 'anywhere' },
  agents: { display: 'flex', flexDirection: 'column', gap: 4, marginTop: 8, marginLeft: 24, alignItems: 'flex-start' },
  agent: { fontFamily: 'inherit', fontSize: typography.size13, color: colors.foreground, display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: 8, minWidth: 0, minHeight: { default: 28, [scale.phone]: 44 }, maxWidth: '100%', backgroundColor: { default: 'transparent', ':hover': colors.hover }, borderWidth: 0, borderRadius: 6, paddingBlock: 4, paddingInline: 4, textAlign: 'left', cursor: 'pointer', outlineOffset: 3 },
  agentName: { minWidth: 0, overflowWrap: 'anywhere' },
  agentModel: { display: { default: 'inline', [scale.phone]: 'none' } },
  caption: { color: surface.secondaryText, fontSize: typography.size12, overflowWrap: 'anywhere' },
  group: { paddingBlock: 4, minWidth: 0, fontSize: typography.size13, lineHeight: 1.65, color: surface.secondaryText },
  summary: { display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8, width: 'fit-content', maxWidth: '100%', minHeight: { default: 28, '@media (pointer: coarse)': 44 }, borderRadius: scale.radiusControl, cursor: 'pointer', listStyle: 'none', overflowWrap: 'anywhere', outlineOffset: 2 },
  chevron: { flexShrink: 0, transitionProperty: 'transform', transitionDuration: appearance.motionFast },
  openChevron: { transform: 'rotate(90deg)' },
  preview: { flexBasis: '100%', marginLeft: 22, fontFamily: typography.mono, fontSize: typography.codeSize, lineHeight: 1.6, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', maxHeight: '4.8em', overflow: 'hidden' },
  expanded: { marginTop: 8, marginBottom: 8, marginLeft: { default: 22, [scale.phone]: 0 }, paddingLeft: 12, borderLeftWidth: 1, borderLeftStyle: 'solid', borderLeftColor: surface.quietBorder, minWidth: 0, display: 'flex', flexDirection: 'column', gap: 12, alignItems: 'stretch' },
  updates: { minWidth: 0 },
  updateSummary: { display: 'list-item', listStyle: 'revert' },
  updateDescription: { marginBlock: '4px 8px' },
  detailAction: { alignSelf: 'flex-start' },
  execution: { minWidth: 0, display: 'flex', flexDirection: 'column', gap: 8 },
  executionHeader: { display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8 },
  host: { display: 'flex', flexWrap: 'wrap', gap: 8, minWidth: 0, fontSize: typography.size12 },
  hostSummary: { flexBasis: '100%', fontFamily: typography.mono, fontSize: typography.codeSize, overflowWrap: 'anywhere' },
  output: { margin: 0, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', fontFamily: typography.mono, fontSize: typography.codeSize, maxHeight: 160, overflowY: 'auto' },
  error: { color: colors.error, margin: 0, overflowWrap: 'anywhere' },
});
