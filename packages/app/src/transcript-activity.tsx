import type { DeepReadonly, ExecutionCell } from '@whip/sdk/state';
import type { RootSnapshot } from '@whip/protocol';
import { useState } from 'react';
import { Button, CodeBlock } from '@whip/ui';
import { ArrowUpRight, Bot, Brain, ChevronRight, Circle, CircleAlert, FilePenLine, FileSearch, FileText, Globe, Terminal } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface, typography } from '@whip/ui/tokens.stylex';
import { activityItems, activitySummary, type ActivityGroup, type ActivityItem, type AgentActivityRow } from './chat-activity-rows';
import type { TimelineRow } from './conversation-rows';
import { agentStatus } from './chat-activity';
import { Shimmer } from './transcript-motion';

export type TranscriptAgent = DeepReadonly<NonNullable<RootSnapshot['agents']>[number]>;
export type Density = 'compact' | 'comfortable' | 'detailed';

export function ActivityHeader({ group, open, toggle, connected, density }: { group: ActivityGroup; open: boolean; toggle(): void; connected: boolean; density: Density }) {
  const preview = activityItems(group).at(-1);
  const snippet = (preview?.host?.summary || preview?.row?.text || preview?.cell?.error || preview?.cell?.output || '').slice(0, 512).split('\n').slice(0, 3).join('\n');
  return <article data-activity-group={group.id} data-activity-owner={group.id} data-message-role="activity" {...stylex.props(styles.header)}>
    <button type="button" data-activity-content aria-expanded={open} onClick={event => {
      const selection = window.getSelection();
      if (event.detail && selection && !selection.isCollapsed && event.currentTarget.contains(selection.anchorNode)) return;
      toggle();
    }} {...stylex.props(styles.button)}>
      <ChevronRight size={14} aria-hidden="true" {...stylex.props(styles.chevron, open && styles.rotated)} />
      <Shimmer active={!!group.live && connected}>{activitySummary(group)}</Shimmer>
      {group.live && <span {...stylex.props(styles.muted)}>{connected ? 'Working' : 'Updates paused'}</span>}
    </button>
    {!open && density === 'comfortable' && snippet && <div data-tool-preview {...stylex.props(styles.preview)}>{snippet}</div>}
  </article>;
}

function operation(item: ActivityItem) {
  const name = item.host?.name ?? '';
  if (item.kind === 'reasoning') return { label: 'Thought', icon: Brain, detail: '' };
  if (item.kind === 'mailbox') return { label: 'Agent messages', icon: Bot, detail: '' };
  if (item.kind === 'execution') return { label: item.cell?.status === 'writing' ? 'Preparing execution' : `${item.cell?.language === 'javascript' ? 'JavaScript' : 'Starlark'} execution`, icon: Terminal, detail: '' };
  const display = item.host?.display;
  const label = name === 'files.read' ? 'Read' : ['files.search', 'files.list'].includes(name) ? 'Search' : name === 'files.write' ? 'Write' : name === 'files.patch' ? 'Edit'
    : ['shell.run', 'shell.start'].includes(name) ? 'Run' : name === 'shell.wait' ? 'Wait' : name.startsWith('browser.') ? 'Browser' : name;
  const icon = label === 'Read' ? FileText : label === 'Search' ? FileSearch : ['Write', 'Edit'].includes(label) ? FilePenLine : label === 'Browser' ? Globe : Terminal;
  return { label, icon, detail: display?.target || display?.command || display?.query || item.host?.summary || '' };
}

export function ActivityStep({ item, groupId, open, toggle, connected, last }: { item: ActivityItem; groupId: string; open: boolean; toggle(): void; connected: boolean; last: boolean }) {
  const { label, icon: Icon, detail } = operation(item);
  const status = item.host?.status ?? item.cell?.status;
  return <div data-activity-owner={groupId} data-activity-step={item.id} {...stylex.props(styles.step)}>
    <svg width="28" height="32" viewBox="0 0 28 32" aria-hidden="true" {...stylex.props(styles.connector)}><path data-connector d={`M9 0V12Q9 19 16 19H27${last ? '' : 'M9 12V32'}`} fill="none" stroke="currentColor" strokeWidth="1" /></svg>
    <button type="button" data-activity-content aria-expanded={open} onClick={toggle} {...stylex.props(styles.button, styles.stepButton)}>
      <Icon size={14} aria-hidden="true" {...stylex.props(styles.fixed)} /><span {...stylex.props(styles.fixed)}>{label}</span>
      {detail && <span title={detail} {...stylex.props(styles.subject)}>{detail}</span>}
      {status && <span {...stylex.props(styles.muted, styles.fixed, status === 'failed' && styles.error)}>{status === 'running' || status === 'writing' ? connected ? 'Running' : 'Paused' : status === 'unknown' ? 'Unknown' : status === 'completed' ? 'Done' : status}</span>}
      {item.host?.duration && <span {...stylex.props(styles.muted, styles.fixed, styles.duration)}>{item.host.duration}</span>}
      <ChevronRight size={12} aria-hidden="true" {...stylex.props(styles.chevron, open && styles.rotated)} />
    </button>
  </div>;
}

function ExecutionDetails({ cell, readBody, onOpenRepl }: { cell: ExecutionCell; readBody(row: TimelineRow): void; onOpenRepl?(): void }) {
  const [full, setFull] = useState(false);
  return <div {...stylex.props(styles.stack)}>
    {cell.code && <CodeBlock code={cell.code} language={cell.language} label={cell.language === 'javascript' ? 'JavaScript' : 'Starlark'} />}
    {cell.output && <CodeBlock code={cell.output} language="text" label="Output" />}
    {cell.value !== undefined && <CodeBlock code={cell.value} language="json" label="Value" />}
    {[cell.code, cell.output, cell.value ?? ''].some(text => new TextEncoder().encode(text).length > 16384) && <details open={full} onToggle={event => setFull(event.currentTarget.open)}>
      <summary>Show all retained code and output</summary>
      {full && <pre {...stylex.props(styles.output)}>{[cell.code, cell.output, cell.value].filter(Boolean).join('\n\n')}</pre>}
    </details>}
    {cell.error && <p {...stylex.props(styles.error)}>{cell.error}</p>}
    {(cell.truncated || cell.historyUnmatched) && <p {...stylex.props(styles.muted)}>Showing retained evidence. Additional content may be available in the stored execution.</p>}
    {cell.codeBody && <Button size="sm" variant="ghost" onClick={() => readBody({ id: cell.id, role: 'tool', text: '', body: { ...cell.codeBody!, media_type: cell.codeBody!.media_type ?? '', source: cell.codeBody!.source ?? '' } })}>Read stored code</Button>}
    {cell.body && <Button size="sm" variant="ghost" onClick={() => readBody({ id: cell.id, role: 'tool', text: '', body: { ...cell.body!, media_type: cell.body!.media_type ?? '', source: cell.body!.source ?? '' } })}>Read stored execution</Button>}
    {onOpenRepl && <Button size="sm" variant="ghost" onClick={onOpenRepl}>Open in REPL <ArrowUpRight size={13} /></Button>}
  </div>;
}

export function ActivityDetail({ item, groupId, readBody, onOpenRepl }: { item: ActivityItem; groupId: string; readBody(row: TimelineRow): void; onOpenRepl?(): void }) {
  const row = item.row;
  const text = row?.text ?? '';
  const lines = text.split('\n');
  const preview = (row?.live ? lines.slice(-24) : lines.slice(0, 24)).join('\n');
  return <div data-activity-owner={groupId} data-activity-detail={item.id} {...stylex.props(styles.detail)}>
    {row && <>
      <pre {...stylex.props(styles.reasoning)}>{preview}</pre>
      {lines.length > 24 && <details><summary>Show all retained {item.kind === 'reasoning' ? 'reasoning' : 'agent messages'}</summary><pre {...stylex.props(styles.output)}>{text}</pre></details>}
      {row.truncated && <p {...stylex.props(styles.muted)}>Some earlier activity was omitted.</p>}
      {row.body && <Button size="sm" variant="ghost" onClick={() => readBody(row)}>Read stored details</Button>}
    </>}
    {item.host && <>
      {item.host.summary && <pre {...stylex.props(styles.output)}>{item.host.summary}</pre>}
      {item.host.error && <p {...stylex.props(styles.error)}><CircleAlert size={13} /> {item.host.error}</p>}
    </>}
    {item.cell && <ExecutionDetails cell={item.cell} readBody={readBody} onOpenRepl={onOpenRepl} />}
  </div>;
}

export function InlineAgent({ row, agent, active, connected, onAgent, readBody, onOpenRepl }: { row: AgentActivityRow; agent?: TranscriptAgent; active: boolean; connected: boolean; onAgent?(id: string): void; readBody(row: TimelineRow): void; onOpenRepl?(): void }) {
  const [details, setDetails] = useState(false);
  const id = row.agentHost.display?.child_id;
  const title = agent?.name || row.agentHost.display?.label || 'Agent';
  return <article data-inline-agent={id || row.id} {...stylex.props(styles.agent)}>
    <div data-activity-content {...stylex.props(styles.stack)}>
      <button type="button" disabled={!id || !onAgent} onClick={() => id && onAgent?.(id)} {...stylex.props(styles.button)}>
        <Bot size={16} /><strong>{title}</strong><Circle size={6} fill="currentColor" aria-hidden="true" />
        <span {...stylex.props(styles.muted)}>{agent ? agentStatus(agent, active, connected) : !connected ? 'Updates paused' : row.agentHost.status === 'running' ? 'Starting' : row.agentHost.status === 'failed' ? 'Failed to start' : 'Status unavailable'}</span>
        {id && <ArrowUpRight size={14} />}
      </button>
      {agent && <span {...stylex.props(styles.muted)}>{[agent.model, agent.effort].filter(Boolean).join(' · ')}</span>}
      {row.agentHost.error && <p {...stylex.props(styles.error)}>{row.agentHost.error}</p>}
      <details onToggle={event => setDetails(event.currentTarget.open)}><summary {...stylex.props(styles.muted)}>Launch details</summary>{details && <ExecutionDetails cell={row.cell} readBody={readBody} onOpenRepl={onOpenRepl} />}</details>
    </div>
  </article>;
}

const styles = stylex.create({
  header: { paddingBlock: 4, color: surface.secondaryText, fontSize: typography.size13 },
  button: { display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8, borderWidth: 0, borderRadius: scale.radiusControl, backgroundColor: { default: 'transparent', ':hover': colors.hover }, color: 'inherit', fontFamily: 'inherit', fontSize: 'inherit', textAlign: 'left', cursor: 'pointer', padding: '4px 2px', minHeight: { default: 28, '@media (pointer: coarse)': 44 }, maxWidth: '100%', outlineOffset: 2 },
  chevron: { flexShrink: 0, transitionProperty: 'transform', transitionDuration: { default: '140ms', [scale.reducedMotion]: '0ms', ':where([data-motion="reduce"]) *': '0ms' }, transitionTimingFunction: 'ease-out' },
  rotated: { transform: 'rotate(90deg)' },
  preview: { marginLeft: 24, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', maxHeight: '4.8em', overflow: 'hidden', fontSize: typography.size12, lineHeight: 1.6 },
  step: { position: 'relative', minHeight: 32, paddingLeft: 32, color: surface.secondaryText, fontSize: typography.size12 },
  connector: { position: 'absolute', left: 2, top: 0, color: surface.quietBorder },
  stepButton: { minHeight: 32, width: '100%', flexWrap: 'nowrap' },
  fixed: { flexShrink: 0, whiteSpace: 'nowrap' },
  duration: { display: { default: 'inline', [scale.phone]: 'none' } },
  subject: { flex: 1, fontFamily: typography.mono, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', minWidth: 0, color: colors.foreground },
  muted: { color: surface.secondaryText, fontSize: typography.size12, overflowWrap: 'anywhere' },
  detail: { marginLeft: 11, paddingLeft: 28, paddingBottom: 12, borderLeft: `1px solid ${surface.quietBorder}`, color: surface.secondaryText, fontSize: typography.size12, minWidth: 0 },
  stack: { display: 'flex', flexDirection: 'column', gap: 8, minWidth: 0 },
  reasoning: { whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', fontFamily: typography.sans, fontSize: typography.size13, lineHeight: 1.65, margin: 0, maxWidth: '96ch', maxHeight: '39.6em', overflowY: 'auto' },
  output: { whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', fontFamily: typography.mono, margin: 0, maxHeight: 520, overflow: 'auto' },
  error: { color: colors.error, overflowWrap: 'anywhere' },
  agent: { marginBlock: 8, padding: 12, border: `1px solid ${surface.quietBorder}`, borderRadius: 8, color: colors.foreground, fontSize: typography.size13 },
});
