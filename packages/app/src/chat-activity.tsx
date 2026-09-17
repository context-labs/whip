import type { DeepReadonly, ExecutionCell, ExecutionRow, SessionViewSnapshot } from '@whip/sdk/state';
import type { RootSnapshot } from '@whip/protocol';
import { ActivityIndicator, Button, IconButton, Tooltip, useTheme } from '@whip/ui';
import { Circle, Pause, Play, ShieldAlert } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { typography } from '@whip/ui/tokens.stylex';
import { cellActivityLabel, executionActive } from './chat-activity-rows';
import { ExecutionTime } from './execution-time';

type Agent = DeepReadonly<NonNullable<RootSnapshot['agents']>[number]>;
export function activityStatus(state: DeepReadonly<SessionViewSnapshot>, agentId: string, cells: readonly ExecutionRow[], connected: boolean, agent?: Agent) {
  const root = state.root;
  if (!root) return { text: state.error ? '' : 'Loading session…', active: false };
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

// A running turn's counters, so a long turn reads as steady work or as a
// stalled loop (many compactions, few calls) instead of a bare "Working".
function turnActivity(agent: Agent): string {
  const turn = agent.last_turn;
  if (!turn?.model_calls) return '';
  const parts = [`${turn.model_calls} ${turn.model_calls === 1 ? 'call' : 'calls'}`];
  if (turn.compactions) parts.push(`${turn.compactions} ${turn.compactions === 1 ? 'compaction' : 'compactions'}`);
  return ` · ${parts.join(' · ')}`;
}

export function agentStatus(agent: Agent, active: boolean, connected: boolean): string {
  if (!connected) return 'Updates paused';
  if (agent.blocking_reason) return `Waiting · ${agent.blocking_reason.replaceAll('_', ' ')}`;
  if (active || agent.status === 'running') return `Working${turnActivity(agent)}`;
  if (agent.lifecycle_phase === 'queued' || agent.status === 'queued') return 'Queued';
  const outcome = agent.last_turn?.status;
  if (outcome) return outcome === 'succeeded' ? 'Completed' : outcome.charAt(0).toUpperCase() + outcome.slice(1);
  return 'Idle';
}

export function CurrentActivity({ status, connected, onDetails }: {
  status: ReturnType<typeof activityStatus>; connected: boolean; onDetails(): void;
}) {
  const { display, setDisplay } = useTheme();
  return <div aria-label="Current activity" data-current-activity {...stylex.props(styles.statusRow)}>
    <Tooltip label={status.text}>
      <Button variant="ghost" size="sm" xstyle={styles.statusButton} aria-label={`Activity: ${status.text}`} onClick={onDetails}>
        {status.attention ? <ShieldAlert size={14} aria-hidden="true" /> : status.active ? <ActivityIndicator active reduceMotion={display.motion === 'reduce'} /> : <Circle size={10} aria-hidden="true" />}
        <span role="status" aria-live="polite" aria-atomic="true" {...stylex.props(styles.statusText)}>{status.text}</span>
      </Button>
    </Tooltip>
    {status.cell && <span {...stylex.props(styles.statusTime)}><ExecutionTime cell={status.cell} connected={connected} /></span>}
    {status.active && <IconButton variant="ghost" size="sm" label={display.motion === 'reduce' ? 'Use system motion setting' : 'Pause activity animation'}
      onClick={() => setDisplay({ motion: display.motion === 'reduce' ? 'system' : 'reduce' })}>
      {display.motion === 'reduce' ? <Play size={12} /> : <Pause size={12} />}
    </IconButton>}
  </div>;
}

const styles = stylex.create({
  statusRow: { display: 'flex', alignItems: 'center', gap: 4, minWidth: 0, maxWidth: '100%', fontSize: typography.size12 },
  statusButton: { minWidth: 28, paddingInline: 4, fontSize: typography.size12 },
  statusText: { minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', position: { '@container (max-width: 600px)': 'absolute' }, width: { '@container (max-width: 600px)': 1 }, height: { '@container (max-width: 600px)': 1 }, clipPath: { '@container (max-width: 600px)': 'inset(50%)' } },
  statusTime: { flexShrink: 0, display: { default: 'inline', '@container (max-width: 600px)': 'none' } },
});
