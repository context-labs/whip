import { useState } from 'react';
import * as Clipboard from 'expo-clipboard';
import { useQuery } from '@tanstack/react-query';
import type { Session as SessionRecord } from '@whip/protocol';
import { cellExecutionRows, type CellExecutionRow, type ExecutionView, type SessionView } from '@whip/sdk/state';
import { useExecutionView, useSessionView } from '@whip/sdk/react';
import { cellOutput, contextUsageLines, turnUsageLines, recordedDuration } from '@whip/app/presentation';
import { useRuntime, useRuntimeState } from '../runtime/context';
import { Actions, Label, Loading, Notice, RowButton, Stack } from './primitives';
import { PagedText, textPreview } from './paged-text';

/** Borrow the existing paired observers. One turn and one cell are disclosed at a time. */
export function ExecutionDetails({ session, view, execution }: { session: SessionRecord; view: SessionView; execution: ExecutionView }) {
  const runtime = useRuntime(), state = useRuntimeState();
  const observed = useSessionView(view), evidence = useExecutionView(execution);
  const online = state.active && state.ready && observed.status === 'live';
  const [chosenTurn, setTurn] = useState(''), [chosenCell, setCell] = useState(''), [choose, setChoose] = useState(false), [after, setAfter] = useState(0);
  const turnID = chosenTurn || evidence.turns[0]?.id;
  const rows = cellExecutionRows(evidence, observed.history.messages).filter(row => row.cell.turn_id === turnID);
  const selected = rows.find(row => row.cell.id === chosenCell) ?? rows.at(-1);
  const context = useQuery({
    queryKey: [state.client?.runtimeID, state.client?.processEpoch, session.id, 'context-usage', session.config_revision, observed.history.snapshot?.revision],
    enabled: online, gcTime: 0, retry: false, refetchInterval: online ? 3000 : false,
    queryFn: ({ signal }) => {
      const client = runtime.requireReady(); if (client !== state.client) throw new Error('Host changed. Read context again.');
      return client.session(session.id).context.usage({ signal });
    },
  });
  const usage = useQuery({
    queryKey: [state.client?.runtimeID, state.client?.processEpoch, session.id, 'turn-usage', turnID],
    enabled: online && !!turnID, gcTime: 0, retry: false, refetchInterval: online ? 3000 : false,
    queryFn: ({ signal }) => {
      const client = runtime.requireReady(); if (client !== state.client || !turnID) throw new Error('Turn is unavailable.');
      return client.session(session.id).turns.usage(turnID, { signal });
    },
  });
  return <Stack>
    <Label>Latest context prefill</Label>
    {!online ? <Notice>Context and execution updates are paused.</Notice> : context.error ? <Notice danger>{textPreview(context.error.message)}</Notice> : context.data ? contextUsageLines(context.data, session.id, session.config_revision, observed.history.snapshot).map(line => <Label key={line}>{line}</Label>) : <Loading label="Reading context usage…" />}
    <Actions items={[{ label: 'Refresh context reading', secondary: true, disabled: !online || context.isFetching, onPress: () => { void context.refetch(); } }]} />
    <Label>Selected turn usage</Label>
    <Actions items={[{ label: choose ? 'Close turn selection' : 'Choose turn', secondary: true, onPress: () => setChoose(!choose) }]} />
    {choose && <Stack>{evidence.turns.map(turn => <RowButton key={turn.id} title={`${turn.kind} · ${turn.state}`} detail={turn.id} selected={turn.id === turnID} disabled={!online} onPress={() => { setTurn(turn.id); setCell(''); setAfter(0); setChoose(false); void execution.focus(turn.id).catch(runtime.report); }} />)}
      <Actions items={[{ label: 'Latest turns', secondary: true, disabled: !online, onPress: () => { setTurn(''); setCell(''); setAfter(0); void execution.latest().catch(runtime.report); } }, ...(evidence.olderCursor ? [{ label: 'Older turn page', secondary: true, disabled: !online, onPress: () => { setTurn(''); setCell(''); setAfter(0); void execution.loadOlder().catch(runtime.report); } }] : [])]} /></Stack>}
    {turnID ? <><Label selectable>{turnID}</Label><Label muted>Canonical attempts for this turn, including helpers and compaction retries. Child and other turns are separate.</Label>
      {online && (usage.error ? <Notice danger>{textPreview(usage.error.message)}</Notice> : usage.data ? turnUsageLines(usage.data).map(line => <Label key={line}>{line}</Label>) : <Loading label="Reading turn usage…" />)}
      <Actions items={[{ label: 'Refresh turn usage', secondary: true, disabled: !online || usage.isFetching, onPress: () => { void usage.refetch(); } }]} /></> : <Notice>No turn is available in this window.</Notice>}
    <Label>REPL executions</Label>
    <Label muted>{evidence.turns.length} turns · {evidence.cells.length} cells · {evidence.operations.length} operations retained in this view. These counts are not accounting totals.</Label>
    {evidence.error && <Notice danger>{textPreview(evidence.error.message)}</Notice>}
    {evidence.truncated && <Notice>This execution window is bounded; older turns or details may be omitted.</Notice>}
    {rows.slice(after, after + 16).map(row => <RowButton key={row.cell.id} title={`Cell · ${row.cell.state}`} detail={row.cell.id} selected={selected?.cell.id === row.cell.id} onPress={() => setCell(row.cell.id)} />)}
    <Actions items={[...(after ? [{ label: 'Previous cells', secondary: true, onPress: () => setAfter(Math.max(0, after - 16)) }] : []), ...(after + 16 < rows.length ? [{ label: 'Next cells', secondary: true, onPress: () => setAfter(after + 16) }] : [])]} />
    {selected ? <MobileCell key={selected.cell.id} row={selected} connected={online && evidence.status === 'live'} /> : <Notice>No cells in the selected turn. Direct human work does not create a code cell.</Notice>}
  </Stack>;
}

export function MobileCell({ row, connected }: { row: CellExecutionRow; connected: boolean }) {
  const runtime = useRuntime(), result = cellOutput(row, connected);
  const code = typeof row.call?.value.arguments.code === 'string' ? row.call.value.arguments.code : undefined;
  const duration = recordedDuration(row.cell.created_at, row.cell.finished_at);
  return <Stack>
    <Label>{row.cell.state === 'running' && !connected ? 'Observation paused' : row.cell.state === 'uncertain' ? 'Outcome uncertain' : row.cell.state}</Label>
    {duration && <Label muted>{duration} recorded duration</Label>}
    {result.restored && <Notice>{result.restored}</Notice>}
    {code === undefined ? <Notice>The exact call message is outside the loaded transcript window.</Notice> : <><Label>Executed code</Label><PagedText source text={code} identity={`${row.cell.id}:code`} /></>}
    {result.output ? <><Label>{result.provisional ? 'Live output · provisional' : 'Output'}</Label><PagedText source text={result.output} identity={`${row.cell.id}:output:${result.provisional ? 'live' : 'committed'}`} /><Actions items={[{ label: 'Copy output', secondary: true, onPress: () => { void Clipboard.setStringAsync(result.output).catch(runtime.report); } }]} /></> : row.cell.state === 'running' ? <Notice>No live stdout is currently available.</Notice> : !row.result ? <Notice>The exact result is outside the loaded transcript window.</Notice> : null}
    {result.provisional && <Notice>Provisional stdout; the committed cell result will replace it.</Notice>}
    {result.truncated && <Notice>Live output is truncated to the first 64 KiB.</Notice>}
    {result.value !== undefined && <><Label>Return value</Label><PagedText source text={result.value} identity={`${row.cell.id}:value`} /></>}
    {result.steps !== undefined && <Label>{result.steps} Starlark steps</Label>}{result.jobs !== undefined && <Label>{result.jobs} QuickJS jobs</Label>}
    {result.error && <Notice danger>{result.error}</Notice>}{result.warning && <Notice>{result.warning}</Notice>}
    {row.cell.state === 'uncertain' && <Notice>Effects may already have happened. Reloading this view does not replay them.</Notice>}
    {row.operations.slice(0, 32).map(operation => <Label key={operation.id}>{textPreview(operation.capability)} · {operation.state}{operation.result?.failure && ` · ${textPreview(operation.result.failure)}`}</Label>)}
    {row.operations.length > 32 && <Notice>Showing 32 host-operation summaries for this cell.</Notice>}
  </Stack>;
}
