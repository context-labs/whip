import { useEffect, useMemo, useState } from 'react';
import type { RootSnapshot } from '@whip/protocol';
import { executionRows, type DeepReadonly, type ExecutionCell, type SessionView, type SessionViewSnapshot } from '@whip/sdk/state';
import { Badge, Button, CodeBlock, CopyButton, Tooltip } from '@whip/ui';
import { Code2, Info, RotateCcw } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { useRuntime } from './context';
import { ReadingList } from './reading-list';
import { ContentRead } from './details/shared';
import { styles } from './repl-view.stylex';
import { ErrorNotice } from './error-feedback';
import { ExecutionTime } from './execution-time';

const historyHelp = 'Saved cells include code, output, results and recorded restart information. Details of individual host calls may be unavailable for older cells.';
const executionLabel = (engine?: string) => engine === 'quickjs' ? 'JavaScript (QuickJS)' : !engine || engine === 'starlark' ? 'Starlark' : 'Unsupported execution language';

export function ReplView({ view, state, agentId, runtimeId, viewId, connected, lastTurn }: {
  view: SessionView;
  state: DeepReadonly<SessionViewSnapshot>;
  agentId: string;
  runtimeId: string;
  viewId: string;
  connected: boolean;
  lastTurn?: DeepReadonly<NonNullable<RootSnapshot['agents']>[number]['last_turn']>;
}) {
  const root = state.root;
  const languageLabel = executionLabel(root?.meta?.execution_engine);
  const history = state.history[agentId];
  const rows = useMemo(() => executionRows(state, agentId), [state, agentId]);
  const cells = rows.filter(row => row.kind === 'cell');
  const ordinals = new Map(cells.map((row, index) => [row.id, index + 1]));
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
  const failed = !root?.active_turns?.[agentId] && (lastTurn ?? root?.agents?.find(agent => agent.id === agentId)?.last_turn)?.status === 'failed';
  return <div {...stylex.props(styles.root)} data-session-view="repl">
    <div {...stylex.props(styles.toolbar)}>
      <span {...stylex.props(styles.title)}>
        <Tooltip label={historyHelp}>
          <Button variant="ghost" size="sm" aria-label="About REPL history" aria-description={historyHelp}><Info size={14} /></Button>
        </Tooltip>
      </span>
      <span {...stylex.props(styles.count)}>{languageLabel} · {cells.length} loaded {cells.length === 1 ? 'cell' : 'cells'}</span>
    </div>
    {!connected && <p role="status" {...stylex.props(styles.notice)}>Execution updates are paused. Showing the last available evidence.</p>}
    {state.executions?.truncated && <p role="status" {...stylex.props(styles.notice)}>Some observed execution details were omitted to keep this view within its memory limit.</p>}
    <ReadingList rows={rows} label="REPL executions" earlierLabel="Load older executions"
      hasMore={history?.hasMore ?? false} canLoadOlder={connected} loadingHistory={history?.loading}
      loadOlder={() => view.loadOlder(agentId)} historyRevision={history?.revision ?? root?.history_revision}
      historyReady={!!history && !history.loading} bookmarkKey={`${runtimeId}:${viewId}:${agentId}:repl`}
      contentStyle={styles.content}
      empty={<div {...stylex.props(styles.empty)}><Code2 size={24} /><strong>{loading ? 'Loading executions…' : missing ? 'This agent’s executions are unavailable' : failed ? 'The last turn failed' : 'No executions in the loaded history'}</strong><span>{loading ? 'Reading the session’s recorded work.' : missing ? 'Use Refresh above to try again, or select another agent.' : failed ? 'No executions are present in the loaded history. See the recorded turn error below.' : history?.hasMore ? 'Load an older page to look for earlier cells.' : `Cells appear here when this agent runs ${languageLabel}.`}</span></div>}
      renderRow={row => row.kind === 'restart'
        ? <div {...stylex.props(styles.restart)} data-repl-restart><RotateCcw size={14} /><span>{row.text}{row.historyUnmatched ? ' · observed; historical match unavailable' : ''}</span></div>
        : <Cell row={row} number={ordinals.get(row.id)!} view={view} connected={connected} expanded={expanded.has(row.id)} onToggle={() => toggle(row.id)} />}
    />
  </div>;
}

const statusLabels: Record<ExecutionCell['status'], string> = { writing: 'Writing', running: 'Running', completed: 'Completed', failed: 'Failed', interrupted: 'Interrupted', cancelled: 'Cancelled', unknown: 'Outcome unavailable' };
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
  const languageLabel = executionLabel(row.executionEngine);
  const [copyError, setCopyError] = useState<unknown>();
  const running = row.status === 'running' || row.status === 'writing';
  const interrupted = row.status === 'interrupted' || row.status === 'cancelled';
  const isJson = !running && formatJsonOutput(row.output) !== row.output;
  const output = outputPreview(isJson ? formatJsonOutput(row.output) : row.output, running, expanded);
  return <article aria-label={`Execution ${number}`} data-repl-cell={row.id}
    {...stylex.props(styles.cell, running && connected && styles.running, row.status === 'failed' && styles.failed, interrupted && styles.interrupted)}>
    <header {...stylex.props(styles.header)}>
      <span {...stylex.props(styles.ordinal)}>In [{number}]</span>
      <Badge tone={row.status === 'failed' ? 'error' : interrupted ? 'warning' : running && connected ? 'info' : 'neutral'}>{!connected && running ? 'Observation paused' : statusLabels[row.status]}</Badge>
      <ExecutionTime cell={row} connected={connected} />
      {row.steps !== undefined && <span {...stylex.props(styles.meta)}>{row.steps.toLocaleString()} steps</span>}
      {row.quickjsJobs !== undefined && <span {...stylex.props(styles.meta)}>{row.quickjsJobs.toLocaleString()} jobs</span>}
      <span {...stylex.props(styles.grow)} />
      {row.code && <CopyButton label={`Copy ${languageLabel} code`} text={row.code} copy={async text => { await runtime.platform.copy(text); setCopyError(undefined); }} onError={setCopyError} />}
    </header>
    {row.historyUnmatched && <p {...stylex.props(styles.meta)}>Observed execution · historical match unavailable</p>}
    {row.code ? <CodeBlock code={row.code} language={row.language} label={`Cell ${number} · ${languageLabel}`} xstyle={styles.code} /> : !row.body && <p {...stylex.props(styles.meta)}>{row.status === 'writing' ? 'Waiting for code…' : 'Code is unavailable in this record.'}</p>}
    {!!row.hosts.length && <div {...stylex.props(styles.hosts)} aria-label="Host calls">
      {row.hosts.map(host => <div key={host.id}>
        <div {...stylex.props(styles.host)}><span aria-hidden="true">→</span><span {...stylex.props(styles.hostName)}>{host.name}{host.summary && <span {...stylex.props(styles.meta)}>({host.summary})</span>}</span><span {...stylex.props(styles.duration)}>{host.status === 'running' ? connected ? 'Running' : 'Updates paused' : host.status === 'unknown' ? 'Outcome unavailable' : host.status === 'cancelled' || host.status === 'interrupted' ? host.status : host.duration}</span></div>
        {host.error && <ErrorNotice type="execution" owner={`${row.id}:${host.id}`} title={`${host.name} failed`} error={host.error} />}
      </div>)}
    </div>}
    {row.output && <div {...stylex.props(styles.section)}>
      <CodeBlock code={output.text} language={isJson ? 'json' : undefined} label={output.hidden && running ? `Output · last 6 lines (${output.hidden} earlier)` : 'Output'} xstyle={styles.code}
        downloadAction={<CopyButton label="Copy output" text={row.output} copy={async text => { await runtime.platform.copy(text); setCopyError(undefined); }} onError={setCopyError} />} />
      {(output.hidden > 0 || expanded) && <Button variant="ghost" size="sm" onClick={onToggle} aria-expanded={expanded}>{expanded ? 'Collapse output' : `Show ${output.hidden} more ${output.hidden === 1 ? 'line' : 'lines'}`}</Button>}
    </div>}
    {row.hasValue && row.value !== undefined && <div {...stylex.props(styles.result)}><CodeBlock code={row.value} language="json" label="Return value" xstyle={styles.code} /></div>}
    {row.scratch && <p aria-label="Scratch checkpoint" {...stylex.props(styles.meta)}>{row.scratch}</p>}
    {row.error && <ErrorNotice type="execution" owner={row.id} error={row.error} />}
    <ErrorNotice type="action" owner={`${row.id}:copy`} title="Could not copy" error={copyError} />
    {row.truncated && <p {...stylex.props(styles.meta)}>Some details of this execution are unavailable or truncated.</p>}
    {row.body && <ContentRead key={row.body.reference_id} view={view} agentId={row.agentId} value={row.body} label="Execution record" />}
  </article>;
}
