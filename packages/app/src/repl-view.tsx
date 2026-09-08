import { useEffect, useMemo, useState } from 'react';
import { executionRows, type DeepReadonly, type ExecutionCell, type SessionView, type SessionViewSnapshot } from '@whip/sdk/state';
import { Badge, Button, CodeBlock, CopyButton, Select } from '@whip/ui';
import { Code2, RotateCcw } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { useRuntime } from './context';
import { ReadingList } from './reading-list';
import { CollectionMore, ContentRead, mergeBy, useCollection } from './details/shared';
import { styles } from './repl-view.stylex';

export function ReplView({ view, state, agentId, runtimeId, viewId, connected, onAgentChange }: {
  view: SessionView;
  state: DeepReadonly<SessionViewSnapshot>;
  agentId: string;
  runtimeId: string;
  viewId: string;
  connected: boolean;
  onAgentChange(agentId: string): void;
}) {
  const root = state.root;
  const history = state.history[agentId];
  const rows = useMemo(() => executionRows(state, agentId), [state, agentId]);
  const cells = rows.filter(row => row.kind === 'cell');
  const ordinals = new Map(cells.map((row, index) => [row.id, index + 1]));
  const collection = useCollection(view, 'agents');
  const agents = mergeBy(root?.agents ?? [], collection.page?.items?.flatMap(item => item.agent ? [item.agent] : []) ?? [], item => item.id);
  const options = [{ value: view.session.rootId, label: 'Root agent' }, ...agents.filter(agent => agent.id !== view.session.rootId).map(agent => ({ value: agent.id, label: agent.name || agent.id }))];
  if (!options.some(option => option.value === agentId)) options.push({ value: agentId, label: agentId });
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(new Set());
  useEffect(() => {
    const ids = new Set(rows.map(row => row.id));
    setExpanded(previous => [...previous].every(id => ids.has(id)) ? previous : new Set([...previous].filter(id => ids.has(id))));
  }, [rows]);
  const toggle = (id: string) => setExpanded(previous => {
    const next = new Set(previous);
    if (next.has(id)) next.delete(id);
    else { next.add(id); if (next.size > 128) next.delete(next.values().next().value!); }
    return next;
  });
  const loading = state.status === 'loading' || history?.loading;
  const missing = !!history?.error || (!root && state.status === 'error');
  return <div {...stylex.props(styles.root)} data-session-view="repl">
    <div {...stylex.props(styles.toolbar)}>
      <span {...stylex.props(styles.title)}><Code2 size={16} /> REPL</span>
      <Select label="REPL agent" value={agentId} onValueChange={onAgentChange} options={options} xstyle={styles.agent} />
      <span {...stylex.props(styles.count)}>{cells.length} loaded {cells.length === 1 ? 'cell' : 'cells'}</span>
      {(collection.page?.has_more || root?.omitted?.agents || collection.error) && <CollectionMore collection={collection} omitted={root?.omitted?.agents} connected={connected} />}
    </div>
    {!connected && <p role="status" {...stylex.props(styles.notice)}>Execution updates are paused. Showing the last available evidence.</p>}
    <p {...stylex.props(styles.notice)}>Earlier host-call traces and restart details may be unavailable.</p>
    {state.executions?.truncated && <p role="status" {...stylex.props(styles.notice)}>Some observed execution details were omitted to keep this view within its memory limit.</p>}
    <ReadingList rows={rows} label="REPL executions" earlierLabel="Load older executions"
      hasMore={history?.hasMore ?? false} canLoadOlder={connected} loadingHistory={history?.loading}
      loadOlder={() => view.loadOlder(agentId)} historyRevision={history?.revision ?? root?.history_revision}
      historyReady={!!history && !history.loading} bookmarkKey={`${runtimeId}:${viewId}:${agentId}:repl`}
      contentStyle={styles.content}
      empty={<div {...stylex.props(styles.empty)}><Code2 size={24} /><strong>{loading ? 'Loading executions…' : missing ? 'This agent’s executions are unavailable' : 'No executions in the loaded history'}</strong><span>{loading ? 'Reading the session’s recorded work.' : missing ? 'Use Refresh above to try again, or select another agent.' : history?.hasMore ? 'Load an older page to look for earlier cells.' : 'Cells appear here when this agent runs Starlark.'}</span></div>}
      renderRow={row => row.kind === 'restart'
        ? <div {...stylex.props(styles.restart)} data-repl-restart><RotateCcw size={14} /><span>{row.text}{row.historyUnmatched ? ' · observed; historical match unavailable' : ''}</span></div>
        : <Cell row={row} number={ordinals.get(row.id)!} view={view} connected={connected} expanded={expanded.has(row.id)} onToggle={() => toggle(row.id)} />}
    />
  </div>;
}

const statusLabels: Record<ExecutionCell['status'], string> = { writing: 'Writing', running: 'Running', completed: 'Completed', failed: 'Failed', interrupted: 'Interrupted', cancelled: 'Cancelled', unknown: 'Outcome unavailable' };
function ObservedTime({ cell, connected }: { cell: ExecutionCell; connected: boolean }) {
  const [now, setNow] = useState(Date.now);
  const running = connected && cell.status === 'running' && cell.observedStartedAt !== undefined;
  useEffect(() => {
    if (!running) return;
    setNow(Date.now());
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [running]);
  if (cell.observedStartedAt === undefined || (!running && cell.observedEndedAt === undefined)) return null;
  const seconds = Math.max(0, ((cell.observedEndedAt ?? now) - cell.observedStartedAt) / 1000);
  return <span {...stylex.props(styles.meta)} title="Time observed in this client; not a historical execution duration">Observed {seconds < 10 ? seconds.toFixed(1) : Math.round(seconds)}s</span>;
}

/** Preview the tail of a running cell and the beginning of a recorded result. */
export function outputPreview(output: string, running: boolean, expanded: boolean) {
  const lines = output.replace(/\n$/, '').split('\n');
  const hidden = expanded ? 0 : Math.max(0, lines.length - 6);
  return { text: hidden ? (running ? lines.slice(-6) : lines.slice(0, 6)).join('\n') : output, hidden };
}

/** Pretty-print a whole JSON payload; mixed prose output stays as-is. */
export function formatJsonOutput(output: string): string {
  const trimmed = output.trim();
  if (!trimmed.startsWith('{') && !trimmed.startsWith('[')) return output;
  try {
    return JSON.stringify(JSON.parse(trimmed), null, 2);
  } catch {
    return output;
  }
}

function Cell({ row, number, view, connected, expanded, onToggle }: {
  row: ExecutionCell; number: number; view: SessionView; connected: boolean; expanded: boolean; onToggle(): void;
}) {
  const runtime = useRuntime();
  const running = row.status === 'running' || row.status === 'writing';
  const interrupted = row.status === 'interrupted' || row.status === 'cancelled';
  const isJson = !running && formatJsonOutput(row.output) !== row.output;
  const output = outputPreview(isJson ? formatJsonOutput(row.output) : row.output, running, expanded);
  return <article aria-label={`Execution ${number}`} data-repl-cell={row.id}
    {...stylex.props(styles.cell, running && connected && styles.running, row.status === 'failed' && styles.failed, interrupted && styles.interrupted)}>
    <header {...stylex.props(styles.header)}>
      <span {...stylex.props(styles.ordinal)}>In [{number}]</span>
      <Badge tone={row.status === 'failed' ? 'error' : interrupted ? 'warning' : running && connected ? 'info' : 'neutral'}>{!connected && running ? 'Observation paused' : statusLabels[row.status]}</Badge>
      <ObservedTime cell={row} connected={connected} />
      {row.steps !== undefined && <span {...stylex.props(styles.meta)}>{row.steps.toLocaleString()} steps</span>}
      <span {...stylex.props(styles.grow)} />
      {row.code && <CopyButton label="Copy Starlark code" text={row.code} copy={runtime.platform.copy} onError={runtime.report} />}
    </header>
    {row.historyUnmatched && <p {...stylex.props(styles.meta)}>Observed execution · historical match unavailable</p>}
    {row.code ? <CodeBlock code={row.code} language="starlark" label={`Cell ${number} · Starlark`} xstyle={styles.code} /> : !row.body && <p {...stylex.props(styles.meta)}>{row.status === 'writing' ? 'Waiting for code…' : 'Code is unavailable in this record.'}</p>}
    {!!row.hosts.length && <div {...stylex.props(styles.hosts)} aria-label="Host calls">
      {row.hosts.map(host => <div key={host.id}>
        <div {...stylex.props(styles.host)}><span aria-hidden="true">→</span><span {...stylex.props(styles.hostName)}>{host.name}{host.summary && <span {...stylex.props(styles.meta)}>({host.summary})</span>}</span><span {...stylex.props(styles.duration)}>{host.duration}</span></div>
        {host.error && <p {...stylex.props(styles.error)}>{host.error}</p>}
      </div>)}
    </div>}
    {row.output && <div {...stylex.props(styles.section)}>
      <CodeBlock code={output.text} language={isJson ? 'json' : undefined} label={output.hidden && running ? `Output · last 6 lines (${output.hidden} earlier)` : 'Output'} xstyle={styles.code}
        downloadAction={<CopyButton label="Copy output" text={row.output} copy={runtime.platform.copy} onError={runtime.report} />} />
      {(output.hidden > 0 || expanded) && <Button variant="ghost" size="sm" onClick={onToggle} aria-expanded={expanded}>{expanded ? 'Collapse output' : `Show ${output.hidden} more ${output.hidden === 1 ? 'line' : 'lines'}`}</Button>}
    </div>}
    {row.value !== undefined && <div {...stylex.props(styles.result)}><span {...stylex.props(styles.resultIcon)} aria-hidden="true">⇒</span><div {...stylex.props(styles.resultCode)}><CodeBlock code={row.value} language="json" label="Return value" xstyle={styles.code} /></div></div>}
    {row.scratch && <p aria-label="Scratch checkpoint" {...stylex.props(styles.meta)}>{row.scratch}</p>}
    {row.error && <p {...stylex.props(styles.error)}>{row.error}</p>}
    {row.truncated && <p {...stylex.props(styles.meta)}>Some details of this execution are unavailable or truncated.</p>}
    {row.body && <ContentRead key={row.body.reference_id} view={view} agentId={row.agentId} value={row.body} label="Execution record" />}
  </article>;
}
