import { useEffect, useMemo, useState } from 'react';
import { executionPresentationRows, type ExecutionPresentationRow, type CellExecutionRow, type DeepReadonly, type ExecutionView, type HistoryGap, type SessionView } from '@whip/sdk/state';
import { useExecutionView, useSessionView } from '@whip/sdk/react';
import type { Session } from '@whip/sdk';
import { Badge, Button, CodeBlock } from '@whip/ui';
import { Code2, RotateCcw } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { ReadingList } from './reading-list';
import { HistoryGapControl } from './history-gap';
import { ContentRead } from './details/content-read';
import { styles } from './repl-view.stylex';
import { ErrorNotice } from './error-feedback';
import { cellOutput, executionOutput, recordedDuration, safeJSON } from './execution-output';
import { ExecutionTime } from './execution-time';

const executionLabel = (engine?: string) => engine === 'quickjs' ? 'JavaScript (QuickJS)' : engine === 'starlark' ? 'Starlark' : 'Execution';
type Row = { id: string; seq?: string; execution: ExecutionPresentationRow; gap?: never } | { id: string; seq: string; gap: DeepReadonly<HistoryGap>; execution?: never };
export interface ReplViewProps {
  session: Session;
  view: SessionView;
  execution: ExecutionView;
  engine: 'starlark' | 'quickjs';
  runtimeId: string;
  viewId: string;
  connected: boolean;
  loadGap?(messageID: string): Promise<void>;
}

export function ReplView({ session, view, execution, engine, runtimeId, viewId, connected, loadGap }: ReplViewProps) {
  const state = useSessionView(view), evidence = useExecutionView(execution);
  const rows = useMemo(() => {
    const cells = executionPresentationRows(evidence, state);
    const hydrated = new Set(evidence.messages?.map(message => message.id));
    const gaps = state.history.gaps.filter(gap => !hydrated.has(gap.messageID));
    const values: Row[] = [];
    let index = 0;
    for (const cell of cells) {
      const seq = cell.call?.message.sequence ?? cell.result?.message.sequence;
      while (gaps[index] && seq && BigInt(gaps[index]!.sequence) < BigInt(seq)) {
        const gap = gaps[index++]!; values.push({ id: `gap:${gap.messageID}`, seq: gap.sequence, gap });
      }
      values.push({ id: cell.id, seq, execution: cell });
    }
    for (const gap of gaps.slice(index)) values.push({ id: `gap:${gap.messageID}`, seq: gap.sequence, gap });
    return values;
  }, [evidence, state]);
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(new Set());
  useEffect(() => {
    const ids = new Set(rows.map(row => row.id));
    setExpanded(previous => [...previous].every(id => ids.has(id)) ? previous : new Set([...previous].filter(id => ids.has(id))));
  }, [rows]);
  const toggle = (id: string) => setExpanded(previous => {
    const next = new Set(previous);
    if (next.has(id)) next.delete(id); else { next.add(id); if (next.size > 128) next.delete(next.values().next().value!); }
    return next;
  });
  const cells = rows.filter(row => row.execution);
  const ordinals = new Map(cells.map((row, index) => [row.id, index + 1]));
  const loading = evidence.status === 'idle' || evidence.status === 'loading';
  const failed = evidence.turns[0]?.state === 'failed';
  const missing = evidence.unavailable || !!evidence.error;
  const omitted = !!evidence.unavailableMessageIDs?.length || state.attemptPresentationsTruncated || evidence.truncated && !evidence.olderCursor && !state.history.olderCursor;
  return <div {...stylex.props(styles.root)} data-session-view="repl">
    {(!connected || evidence.status === 'stale' || evidence.status === 'suspended') && <p role="status" {...stylex.props(styles.notice)}>Execution updates are paused. Showing the last available evidence.</p>}
    {omitted && <p role="status" {...stylex.props(styles.notice)}>Some execution details are unavailable or truncated.</p>}
    {evidence.error && <ErrorNotice type="resource" owner={`${runtimeId}:${session.id}:executions`} error={evidence.error.message} action={<Button variant="ghost" disabled={!connected} onClick={() => void execution.refresh()}>Refresh executions</Button>} />}
    <ReadingList rows={rows} label="REPL executions" earlierLabel="Load older executions"
      hasMore={evidence.olderCursor !== null || state.history.olderCursor !== null} canLoadOlder={connected} loadingHistory={loading}
      loadOlder={() => execution.loadOlder()}
      loadLatest={async () => { await view.latest(); await execution.latest(); }} latestMissing={state.history.latestMissing || evidence.latestMissing}
      historyRevision={state.history.snapshot?.revision} historyCursor={evidence.olderCellCursor?.before ?? evidence.olderCursor ?? state.history.olderCursor ?? undefined}
      historyReady={!loading} bookmarkKey={`${runtimeId}:${viewId}:${session.id}:repl`} contentStyle={styles.content}
      empty={<div {...stylex.props(styles.empty)}><Code2 size={24} /><strong>{loading ? 'Loading executions…' : missing ? 'This session’s executions are unavailable' : failed ? 'The last turn failed' : 'No executions in the loaded history'}</strong><span>{loading ? 'Reading the session’s recorded work.' : missing ? 'Refresh to try again, or select another session.' : failed ? evidence.turns[0]?.failure ?? 'No executions are present in the loaded history.' : `Cells appear here when this session runs ${executionLabel(engine)}.`}</span></div>}
      renderRow={row => row.gap ? <HistoryGapControl gap={row.gap} connected={connected && !!loadGap} load={() => loadGap ? loadGap(row.gap.messageID) : Promise.resolve()} />
        : <PresentationCell row={row.execution} number={ordinals.get(row.id)!} session={session} engine={engine} connected={connected} expanded={expanded.has(row.id)} onToggle={() => toggle(row.id)} />}
    />
  </div>;
}

const hostLabels = { ready: 'Queued', waiting: 'Waiting', dispatched: 'Running', succeeded: 'Done', failed: 'Failed', denied: 'Denied', cancelled: 'Cancelled', uncertain: 'Outcome unavailable' };
const statusLabels = { writing: 'Writing', pending: 'Writing', running: 'Running', succeeded: 'Completed', failed: 'Failed', uncertain: 'Outcome uncertain', cancelled: 'Cancelled', recorded: 'Recorded' };
/** Preview the tail of running output and the beginning of a recorded result. */
export function outputPreview(output: string, running: boolean, expanded: boolean) {
  const lines = output.replace(/\n$/, '').split('\n');
  const hidden = expanded ? 0 : Math.max(0, lines.length - 6);
  return { text: hidden ? (running ? lines.slice(-6) : lines.slice(0, 6)).join('\n') : output, hidden };
}
export function formatJsonOutput(output: string): string {
  const trimmed = output.trim();
  if (!trimmed.startsWith('{') && !trimmed.startsWith('[')) return output;
  try { return JSON.stringify(safeJSON(trimmed), null, 2); } catch { return output; }
}
type CardProps = { number: number; session: Session; engine: 'starlark' | 'quickjs'; connected: boolean; expanded: boolean; onToggle(): void };
/** The inspector uses the same card with authoritative native evidence. */
export function ExecutionCellCard({ row, ...props }: CardProps & { row: CellExecutionRow }) {
  const code = typeof row.call?.value.arguments.code === 'string' ? row.call.value.arguments.code : '';
  return <PresentationCell {...props} row={{ id: row.displayID, kind: 'cell', state: row.cell.state, code, cell: row, call: row.call, result: row.result, preview: null, attempt: null, part: null, messageID: row.cell.call_message_id, turnID: row.cell.turn_id, startedAt: row.cell.created_at, truncated: false }} />;
}
function PresentationCell({ row, number, session, engine, connected, expanded, onToggle }: CardProps & { row: ExecutionPresentationRow }) {
  const cell = row.cell?.cell;
  const result = row.cell ? cellOutput(row.cell, connected) : { ...executionOutput(row.result?.value.output ?? ''), provisional: false, truncated: false };
  const actualEngine = result.engine ?? cell?.checkpoint?.engine ?? (row.state === 'writing' || row.state === 'pending' ? engine : undefined);
  const label = executionLabel(actualEngine);
  const state = row.state === 'recorded' && row.result ? row.result.value.is_error || result.error ? 'failed' : 'succeeded' : row.state;
  const running = state === 'running' || state === 'writing' || state === 'pending', uncertain = state === 'uncertain';
  const code = row.code;
  const copyCode = typeof row.call?.value.arguments.code === 'string' ? row.call.value.arguments.code : code;
  const formatted = !running ? formatJsonOutput(result.output) : result.output;
  const output = outputPreview(formatted, running, expanded);
  return <article aria-label={`Execution ${number}`} data-repl-cell={row.id} {...stylex.props(styles.cell, running && connected && styles.running, state === 'failed' && styles.failed, (uncertain || state === 'cancelled') && styles.interrupted)}>
    <header {...stylex.props(styles.header)}><span {...stylex.props(styles.ordinal)}>In [{number}]</span>
      <Badge tone={state === 'failed' ? 'error' : uncertain || state === 'cancelled' ? 'warning' : running && connected ? 'info' : 'neutral'}>{!connected && running ? 'Observation paused' : statusLabels[state]}</Badge>
      {cell && <ExecutionTime cell={cell} connected={connected} />}
      {result.steps !== undefined && <span {...stylex.props(styles.meta)}>{result.steps.toLocaleString()} steps</span>}
      {result.jobs !== undefined && <span {...stylex.props(styles.meta)}>{result.jobs.toLocaleString()} jobs</span>}
      <span {...stylex.props(styles.grow)} />
    </header>
    {result.restored && <div {...stylex.props(styles.restart)} data-repl-restart><RotateCcw size={14} /><span>{result.restored}</span></div>}
    {code ? <CodeBlock code={code} copyText={copyCode} language={actualEngine === 'quickjs' ? 'javascript' : actualEngine === 'starlark' ? 'python' : undefined} label={`Cell ${number} · ${label}`} copyLabel={`Copy ${label} code`} xstyle={styles.code} />
      : <p {...stylex.props(styles.meta)}>{state === 'writing' || state === 'pending' ? 'Waiting for code…' : 'Code is unavailable in this record.'}</p>}
    {!!row.cell?.operations.length && <div {...stylex.props(styles.hosts)} aria-label="Host calls">{row.cell.operations.map(operation => <div key={operation.id}>
      <div {...stylex.props(styles.host)}><span aria-hidden="true">→</span><span {...stylex.props(styles.hostName)}>{operation.capability}</span><span {...stylex.props(styles.duration)}>{operation.state === 'dispatched' ? connected ? 'Running' : 'Updates paused' : operation.state === 'succeeded' ? recordedDuration(operation.created_at, operation.finished_at) ?? 'Done' : hostLabels[operation.state]}</span></div>
      {operation.result?.failure && <ErrorNotice type="execution" owner={operation.id} title={`${operation.capability} failed`} error={operation.result.failure} />}
      {operation.result?.content_references?.map(reference => <ContentRead key={reference} session={session} connected={connected} reference={reference} label="Host result" />)}
    </div>)}</div>}
    {result.output && <div {...stylex.props(styles.section)}><CodeBlock code={output.text} language={formatted !== result.output ? 'json' : undefined} label={output.hidden && running ? `Output · last 6 lines (${output.hidden} earlier)` : 'Output'} xstyle={styles.code} copyText={result.output} copyLabel="Copy output" />
      {(output.hidden > 0 || expanded) && <Button variant="ghost" size="sm" onClick={onToggle} aria-expanded={expanded}>{expanded ? 'Collapse output' : `Show ${output.hidden} more ${output.hidden === 1 ? 'line' : 'lines'}`}</Button>}
    </div>}
    {result.value !== undefined && <div {...stylex.props(styles.result)}><CodeBlock code={result.value} language="json" label="Return value" xstyle={styles.code} /></div>}
    {result.warning && <p aria-label="Scratch checkpoint" {...stylex.props(styles.meta)}>{result.warning}</p>}
    {result.error && <ErrorNotice type="execution" owner={row.id} error={result.error} />}
    {row.truncated || result.truncated ? <p role="status" {...stylex.props(styles.meta)}>Some details of this execution are unavailable or truncated.</p> : null}
    {cell && !row.result && !running && <p {...stylex.props(styles.meta)}>The recorded output is unavailable.</p>}
    {cell && uncertain && <p {...stylex.props(styles.meta)}>Effects may already have happened. The recorded outcome is uncertain.</p>}
  </article>;
}
