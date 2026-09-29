import { useEffect, useMemo, useState } from 'react';
import { cellExecutionRows, type CellExecutionRow, type DeepReadonly, type ExecutionView, type HistoryGap, type SessionView } from '@whip/sdk/state';
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
import { executionOutput, recordedDuration, safeJSON } from './execution-output';

const executionLabel = (engine: string) => engine === 'quickjs' ? 'JavaScript (QuickJS)' : 'Starlark';
type Row = { id: string; seq?: string; cell: CellExecutionRow; gap?: never } | { id: string; seq: string; gap: DeepReadonly<HistoryGap>; cell?: never };
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
    const values: Row[] = cellExecutionRows(evidence, state.history.messages).map(cell => ({ id: cell.cell.id, seq: cell.call?.message.sequence ?? cell.result?.message.sequence, cell }));
    values.push(...state.history.gaps.map(gap => ({ id: `gap:${gap.messageID}`, seq: gap.sequence, gap })));
    return values.sort((a, b) => a.seq && b.seq ? BigInt(a.seq) < BigInt(b.seq) ? -1 : BigInt(a.seq) > BigInt(b.seq) ? 1 : a.id.localeCompare(b.id) : a.seq ? -1 : b.seq ? 1 : a.id.localeCompare(b.id));
  }, [evidence, state.history]);
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
  const cells = rows.filter(row => row.cell);
  const ordinals = new Map(cells.map((row, index) => [row.id, index + 1]));
  const loading = evidence.status === 'idle' || evidence.status === 'loading';
  const failed = evidence.turns[0]?.state === 'failed';
  const missing = evidence.unavailable || !!evidence.error;
  const previews = connected && state.status === 'live' ? state.preview?.calls?.filter(call => call.name === 'execute') ?? [] : [];
  return <div {...stylex.props(styles.root)} data-session-view="repl">
    {(!connected || evidence.status === 'stale' || evidence.status === 'suspended') && <p role="status" {...stylex.props(styles.notice)}>Execution updates are paused. Showing the last available evidence.</p>}
    {evidence.truncated && <p role="status" {...stylex.props(styles.notice)}>This bounded execution window may omit older turns or details. Load older evidence or select an exact turn in the inspector.</p>}
    {evidence.error && <ErrorNotice type="resource" owner={`${runtimeId}:${session.id}:executions`} error={evidence.error.message} action={<Button variant="ghost" disabled={!connected} onClick={() => void execution.refresh()}>Refresh executions</Button>} />}
    <ReadingList rows={rows} label="REPL executions" earlierLabel="Load older executions"
      hasMore={evidence.olderCursor !== null || state.history.olderCursor !== null} canLoadOlder={connected} loadingHistory={loading}
      loadOlder={async () => { if (state.history.olderCursor !== null) await view.loadOlder(); else await execution.loadOlder(); await execution.refresh(); }}
      loadLatest={async () => { await view.latest(); await execution.latest(); }} latestMissing={state.history.latestMissing || evidence.latestMissing}
      historyRevision={state.history.snapshot?.revision} historyCursor={state.history.olderCursor ?? evidence.olderCursor ?? undefined}
      historyReady={!loading} bookmarkKey={`${runtimeId}:${viewId}:${session.id}:repl`} contentStyle={styles.content}
      empty={<div {...stylex.props(styles.empty)}><Code2 size={24} /><strong>{loading ? 'Loading executions…' : missing ? 'This session’s executions are unavailable' : failed ? 'The last turn failed' : 'No executions in the loaded history'}</strong><span>{loading ? 'Reading recorded execution metadata.' : missing ? 'Refresh to try again, or select another session.' : failed ? evidence.turns[0]?.failure ?? 'No cell result was recorded.' : `Cells appear here when this session runs ${executionLabel(engine)}.`}</span></div>}
      footer={previews.map(call => <article key={`${state.preview!.message_id}:${call.id}`} {...stylex.props(styles.cell)} aria-label="Provisional execution">
        <Badge>Writing · provisional</Badge><CodeBlock code={call.arguments} label="Incoming execute arguments" maxBytes={32 << 10} />
        <p {...stylex.props(styles.meta)}>No execution cell has been committed for this preview.</p>
      </article>)}
      renderRow={row => row.gap ? <HistoryGapControl gap={row.gap} connected={connected && !!loadGap} load={() => loadGap ? loadGap(row.gap.messageID) : Promise.resolve()} />
        : <ExecutionCellCard row={row.cell} number={ordinals.get(row.id)!} session={session} engine={engine} connected={connected} expanded={expanded.has(row.id)} onToggle={() => toggle(row.id)} />}
    />
  </div>;
}

const statusLabels = { running: 'Running', succeeded: 'Completed', failed: 'Failed', uncertain: 'Outcome uncertain' };
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
export function ExecutionCellCard({ row, number, session, engine, connected, expanded, onToggle }: {
  row: CellExecutionRow; number: number; session: Session; engine: 'starlark' | 'quickjs'; connected: boolean; expanded: boolean; onToggle(): void;
}) {
  const cell = row.cell, result = executionOutput(row.result?.value.output ?? '');
  const actualEngine = cell.checkpoint?.engine ?? engine;
  const label = executionLabel(actualEngine), running = cell.state === 'running', uncertain = cell.state === 'uncertain';
  const code = typeof row.call?.value.arguments.code === 'string' ? row.call.value.arguments.code : undefined;
  const formatted = !running ? formatJsonOutput(result.output) : result.output;
  const output = outputPreview(formatted, running, expanded);
  const duration = recordedDuration(cell.created_at, cell.finished_at);
  return <article aria-label={`Execution ${number}`} data-repl-cell={cell.id} {...stylex.props(styles.cell, running && connected && styles.running, cell.state === 'failed' && styles.failed, uncertain && styles.interrupted)}>
    <header {...stylex.props(styles.header)}><span {...stylex.props(styles.ordinal)}>In [{number}]</span>
      <Badge tone={cell.state === 'failed' ? 'error' : uncertain ? 'warning' : running && connected ? 'info' : 'neutral'}>{!connected && running ? 'Observation paused' : statusLabels[cell.state]}</Badge>
      {duration && <span {...stylex.props(styles.meta)} title="Recorded host duration">{duration}</span>}
      {result.steps !== undefined && <span {...stylex.props(styles.meta)}>{result.steps.toLocaleString()} steps</span>}
      {result.jobs !== undefined && <span {...stylex.props(styles.meta)}>{result.jobs.toLocaleString()} jobs</span>}
    </header>
    {result.restored && <div {...stylex.props(styles.restart)} data-repl-restart><RotateCcw size={14} /><span>{result.restored}</span></div>}
    {code !== undefined ? <CodeBlock code={code} language={actualEngine === 'quickjs' ? 'javascript' : 'python'} label={`Cell ${number} · ${label}`} copyLabel={`Copy ${label} code`} xstyle={styles.code} />
      : <p {...stylex.props(styles.meta)}>The exact call message is outside the loaded transcript window.</p>}
    {!!row.operations.length && <div {...stylex.props(styles.hosts)} aria-label="Host calls">{row.operations.map(operation => <div key={operation.id}>
      <div {...stylex.props(styles.host)}><span aria-hidden="true">→</span><span {...stylex.props(styles.hostName)}>{operation.capability}</span><span {...stylex.props(styles.duration)}>{operation.state}{recordedDuration(operation.created_at, operation.finished_at) && ` · ${recordedDuration(operation.created_at, operation.finished_at)}`}</span></div>
      {operation.result?.failure && <ErrorNotice type="execution" owner={operation.id} title={`${operation.capability} failed`} error={operation.result.failure} />}
      {operation.result?.content_references?.map(reference => <ContentRead key={reference} session={session} connected={connected} reference={reference} label="Host result" />)}
    </div>)}</div>}
    {result.output && <div {...stylex.props(styles.section)}><CodeBlock code={output.text} language={formatted !== result.output ? 'json' : undefined} label={output.hidden && running ? `Output · last 6 lines (${output.hidden} earlier)` : 'Output'} xstyle={styles.code} copyText={result.output} copyLabel="Copy output" />
      {(output.hidden > 0 || expanded) && <Button variant="ghost" size="sm" onClick={onToggle} aria-expanded={expanded}>{expanded ? 'Collapse output' : `Show ${output.hidden} more ${output.hidden === 1 ? 'line' : 'lines'}`}</Button>}
    </div>}
    {result.value !== undefined && <div {...stylex.props(styles.result)}><CodeBlock code={result.value} language="json" label="Return value" xstyle={styles.code} /></div>}
    {result.warning && <p aria-label="Scratch checkpoint" {...stylex.props(styles.meta)}>{result.warning}</p>}
    {result.error && <ErrorNotice type="execution" owner={cell.id} error={result.error} />}
    {!row.result && !running && <p {...stylex.props(styles.meta)}>The exact result message is outside the loaded transcript window.</p>}
    {!cell.checkpoint && !running && <p {...stylex.props(styles.meta)}>No reusable checkpoint was recorded at this boundary. Reloading this view does not replay effects.</p>}
    {uncertain && <p {...stylex.props(styles.meta)}>Effects may already have happened. The recorded outcome is uncertain.</p>}
  </article>;
}
