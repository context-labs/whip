import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { Client, type Cell, type HostOperation, type Message, type Operations, type Turn } from '@whip/sdk';
import { createExecutionView, createSessionView } from '@whip/sdk/state';
import { ThemeProvider, UIProvider } from '@whip/ui';
import fixtures from '../../protocol/schema/fixtures.json';
import { ReplView, outputPreview } from '../src/repl-view';
import { executionOutput } from '../src/execution-output';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';

vi.mock('../src/reading-list', () => ({
  ReadingList: ({ rows, renderRow, empty, footer }: { rows: { id: string }[]; renderRow(row: unknown, index: number): ReactNode; empty: ReactNode; footer: ReactNode }) =>
    <div>{rows.length ? rows.map((row, index) => <div key={row.id}>{renderRow(row, index)}</div>) : empty}{footer}</div>,
}));
beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());
const sample = (type: string) => structuredClone(fixtures.find(value => value.type === type && value.valid)!.value);
const output = Array.from({ length: 9 }, (_, index) => `line ${index + 1}`).join('\n') + '\n';
const result = (value = output, engine: 'starlark' | 'quickjs' = 'starlark') => JSON.stringify({ result: { format_version: 2, execution_engine: engine, language: engine === 'quickjs' ? 'javascript' : 'starlark', output: value, has_value: true, value: 42, steps: 7, metrics: { quickjs_jobs: 3 } } });

async function fixture(raw = result(), engine: 'starlark' | 'quickjs' = 'starlark', empty = false) {
  const state: { attempts: NonNullable<Operations['sessions.observe']['result']['attempt_presentations']>; hasCell: boolean; turn: Turn; cell: Cell; messages: Message[]; operations: HostOperation[]; preview: Operations['sessions.observe']['result']['preview']; output: Operations['cells.output']['result']['preview'] } = {
    attempts: [],
    hasCell: !empty,
    output: null,
    turn: { ...sample('Turn'), id: 'turn', session_id: 'session_child', state: 'succeeded', kind: 'prompt', finished_at: '2026-09-27T12:00:01Z' } as Turn,
    cell: { ...sample('Cell'), id: 'cell', turn_id: 'turn', state: 'succeeded', checkpoint: null } as Cell,
    messages: [] as Message[],
    operations: [] as HostOperation[],
    preview: null as Operations['sessions.observe']['result']['preview'],
  };
  state.messages = empty ? [] : [
    { ...sample('Message'), id: 'message_call', session_id: 'session_child', turn_id: 'turn', group_id: 'turn', source: null, retired_by: null, retired_revision: null, sequence: '9007199254740993', role: 'assistant', parts: [{ type: 'tool_call', call: { id: 'call_fixture', name: 'execute', arguments: { code: 'print(42)' } } }] } as Message,
    { ...sample('Message'), id: 'message_result', session_id: 'session_child', turn_id: 'turn', group_id: 'turn', source: null, retired_by: null, retired_revision: null, sequence: '9007199254740994', role: 'tool', parts: [{ type: 'tool_result', result: { call_id: 'call_fixture', output: raw, is_error: false } }] } as Message,
  ];
  const calls: string[] = [];
  const client = await Client.connect(async request => {
    calls.push(request.method);
    const after = !!request.params && typeof request.params === 'object' && 'after' in request.params && request.params.after;
    const history = { session_id: 'session_child', revision: '1', through_sequence: state.messages.at(-1)?.sequence ?? '0', message_count: String(state.messages.length) };
    let value: unknown;
    if (request.method === 'initialize') value = { major: 4, minor: 0, runtime_id: 'host', process_epoch: 'boot', network_client: false, builtins: [] };
    else if (request.method === 'sessions.activity') value = { ...sample('SessionActivity'), active_turn: state.turn.state === 'running' ? state.turn : null, active_input_id: state.turn.state === 'running' ? 'input_fixture' : null };
    else if (request.method === 'sessions.history_page') value = { snapshot: history, messages: state.messages, next_cursor: null, attempt_presentations: state.attempts };
    else if (request.method === 'sessions.observe') value = { snapshot: history, epoch: 'boot', messages: [], preview: state.preview, attempt_presentations: state.attempts };
    else if (request.method === 'cells.output') value = { epoch: 'boot', preview: state.output };
    else if (request.method === 'sessions.turns') value = { items: [state.turn], next_cursor: null };
    else if (request.method === 'turns.cells_page') value = { items: state.hasCell ? [state.cell] : [], next_cursor: null };
    else if (request.method === 'context.read') return { jsonrpc: '2.0', id: request.id, error: { code: -32004, kind: 'NOT_FOUND', message: 'Recorded message unavailable' } };
    else if (request.method === 'turns.operations') value = { items: after ? [] : state.operations };
    else throw new Error(request.method);
    return { jsonrpc: '2.0', id: request.id, result: structuredClone(value) };
  }, { clientID: 'test' });
  const session = client.session('session_child'), view = createSessionView(session, { pollIntervalMs: 60_000 });
  await view.start();
  const execution = createExecutionView(session, view, { pollIntervalMs: 60_000 }); await execution.start();
  afterEach(async () => { await execution.dispose(); await view.dispose(); });
  const copy = vi.fn(async () => {}), runtime = { platform: { copy }, report: vi.fn() } as unknown as AppRuntime;
  const app = (connected = true) => <RuntimeContext.Provider value={runtime}><UIProvider copy={copy}><ThemeProvider initialTheme="claude-code">
    <ReplView session={session} view={view} execution={execution} engine={engine} runtimeId="host" viewId="view" connected={connected} />
  </ThemeProvider></UIProvider></RuntimeContext.Provider>;
  return { app, state, view, execution, copy, calls };
}

it('previews running tails and recorded beginnings while keeping the full copy text', () => {
  expect(outputPreview(output, true, false)).toEqual({ text: 'line 4\nline 5\nline 6\nline 7\nline 8\nline 9', hidden: 3 });
  expect(outputPreview(output, false, false)).toEqual({ text: 'line 1\nline 2\nline 3\nline 4\nline 5\nline 6', hidden: 3 });
  expect(outputPreview(output, true, true)).toEqual({ text: output, hidden: 0 });
});
it('renders native committed evidence, expands output and copies exact code, text and value', async () => {
  const f = await fixture(); render(f.app());
  expect(screen.getByRole('article', { name: 'Execution 1' })).toBeDefined(); expect(screen.getByText('Completed')).toBeDefined();
  expect(screen.getByRole('region', { name: 'Output' }).textContent).not.toContain('line 9');
  for (const block of document.querySelectorAll('figure')) expect(within(block).getAllByRole('button')).toHaveLength(1);
  fireEvent.click(screen.getByRole('button', { name: 'Copy Starlark code' })); await waitFor(() => expect(f.copy).toHaveBeenLastCalledWith('print(42)'));
  fireEvent.click(screen.getByRole('button', { name: 'Copy Return value' })); await waitFor(() => expect(f.copy).toHaveBeenLastCalledWith('42'));
  fireEvent.click(screen.getByRole('button', { name: 'Copy output' })); await waitFor(() => expect(f.copy).toHaveBeenLastCalledWith(output));
  fireEvent.click(screen.getByRole('button', { name: 'Show 3 more lines' })); expect(screen.getByRole('region', { name: 'Output' }).textContent).toBe(output);
  expect(screen.queryByRole('textbox')).toBeNull(); expect(screen.queryByText(/Observed \d/)).toBeNull();
  expect(f.calls.every(call => call === 'initialize' || call.startsWith('sessions.') || call.startsWith('turns.'))).toBe(true);
});
it('copies raw JSON output and preserves unsafe integer payloads verbatim', async () => {
  const raw = '{"greeting":"🌍","items":[1,2]}', f = await fixture(result(raw)); render(f.app());
  expect(screen.getByRole('region', { name: 'Output' }).textContent).not.toBe(raw);
  fireEvent.click(screen.getByRole('button', { name: 'Copy output' })); await waitFor(() => expect(f.copy).toHaveBeenCalledExactlyOnceWith(raw));
  const exact = '{"result":{"execution_engine":"starlark","has_value":true,"value":9007199254740993}}';
  expect(executionOutput(exact).output).toBe(exact); expect(executionOutput(exact).value).toBeUndefined();
});
it('shows native QuickJS explicit null and job count, while absent values stay absent', async () => {
  const raw = JSON.stringify({ result: { execution_engine: 'quickjs', has_value: true, value: null, metrics: { quickjs_jobs: 3 } } });
  const f = await fixture(raw, 'quickjs'); render(f.app());
  expect(screen.getByText('3 jobs')).toBeDefined(); expect(screen.getByRole('region', { name: 'Return value' }).textContent).toBe('null');
  expect(screen.getByRole('button', { name: 'Copy JavaScript (QuickJS) code' })).toBeDefined(); expect(screen.queryByText(/steps/)).toBeNull();
  expect(executionOutput(JSON.stringify({ result: { execution_engine: 'starlark', has_value: false, value: null } })).value).toBeUndefined();
});
it('retains failed output, restart and checkpoint warnings without inventing a success or replay', async () => {
  const raw = JSON.stringify({ error: 'cell failed', result: { execution_engine: 'starlark', output: 'before failure', has_value: false, value: null, scratch: { warning: 'Checkpoint failed. Do not replay effects.' }, restored: { restored: ['answer'], failed: [{ name: 'stream', reason: 'unavailable' }] } } });
  const f = await fixture(raw); f.state.cell.state = 'failed'; await f.execution.refresh(); render(f.app());
  expect(screen.getByText('Failed')).toBeDefined(); expect(screen.getByRole('region', { name: 'Output' }).textContent).toBe('before failure');
  expect(screen.getByText('cell failed')).toBeDefined(); expect(screen.getByLabelText('Scratch checkpoint').textContent).toContain('Do not replay');
  expect(screen.getByText('Worker restarted; restored 1 saved names, 1 unavailable.')).toBeDefined();
  expect(screen.queryByRole('button', { name: /replay|run/i })).toBeNull();
});
it('distinguishes failed empty turns and paused retained evidence', async () => {
  const f = await fixture('', 'starlark', true); f.state.turn.state = 'failed'; f.state.turn.failure = 'Invalid prompt_cache_key'; await f.execution.refresh();
  const mounted = render(f.app()); expect(screen.getByText('The last turn failed')).toBeDefined(); expect(screen.getByText('Invalid prompt_cache_key')).toBeDefined();
  mounted.rerender(f.app(false)); expect(screen.getByRole('status').textContent).toContain('paused'); expect(screen.queryAllByRole('article')).toHaveLength(0);
});
it('shows writing code in the ordinary card flow and exact host-operation state beside the committed cell', async () => {
  const f = await fixture();
  f.state.operations = [{ ...sample('HostOperation'), id: 'operation', session_id: 'session_child', turn_id: 'turn', cell_id: 'cell', state: 'waiting', result: null, dispatched_at: null, finished_at: null } as HostOperation];
  f.state.preview = { attempt_id: 'attempt', turn_id: 'turn', message_id: 'preview', text: '', reasoning: '', calls: [{ index: 0, id: 'next_call', name: 'execute', arguments: '{"code":"print(7)' }], truncated: false, revision: '1' };
  await act(async () => { await f.view.refresh(); await f.execution.refresh(); }); render(f.app());
  expect(screen.getByRole('article', { name: 'Execution 2' })).toBeDefined(); expect(screen.getByText('Waiting')).toBeDefined();
  expect(document.querySelectorAll('[data-repl-cell]')).toHaveLength(2); expect(screen.getByText('Writing')).toBeDefined();
  expect(screen.getByRole('region', { name: 'Cell 2 · Starlark' }).textContent).toBe('print(7)');
  expect(screen.queryByText(/provisional|No execution cell|Incoming execute arguments/i)).toBeNull();
});

it('keeps uncertain cell metadata when exact transcript bodies are unavailable', async () => {
  const f = await fixture('', 'starlark', true);
  f.state.hasCell = true; f.state.cell.state = 'uncertain'; f.state.cell.result_message_id = null;
  f.state.turn.state = 'interrupted';
  await f.view.latest(); await f.execution.refresh(); render(f.app());
  expect(screen.getByText('Outcome uncertain')).toBeDefined();
  expect(screen.getByText('Code is unavailable in this record.')).toBeDefined();
  expect(screen.getByText('The recorded output is unavailable.')).toBeDefined();
  expect(screen.getByText('Effects may already have happened. The recorded outcome is uncertain.')).toBeDefined();
  expect(screen.queryByRole('region', { name: 'Output' })).toBeNull();
});

it('renders native provisional stdout verbatim, then replaces it with the exact committed result and clears on detach', async () => {
  const f = await fixture();
  f.state.turn.state = 'running'; f.state.turn.finished_at = null;
  f.state.cell.state = 'running'; f.state.cell.finished_at = null; f.state.cell.result_message_id = null;
  f.state.messages = f.state.messages.slice(0, 1);
  f.state.output = { session_id: 'session_child', turn_id: 'turn', cell_id: 'cell', call_message_id: 'message_call', call_id: 'call_fixture', history_revision: '1', revision: '9007199254740993', text: '{"result":{"output":"must stay raw"}}', truncated: true };
  await f.view.latest(); await f.view.refresh(); await f.execution.refresh();
  expect(f.execution.getSnapshot().output).not.toBeNull();
  const mounted = render(f.app());
  expect(screen.getByRole('region', { name: 'Output' }).textContent).toContain('must stay raw');
  expect(screen.getByText('Some details of this execution are unavailable or truncated.')).toBeDefined();
  expect(screen.queryByText(/provisional/i)).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Copy output' })); await waitFor(() => expect(f.copy).toHaveBeenCalledWith(f.state.output!.text));
  mounted.rerender(f.app(false)); expect(screen.queryByRole('region', { name: 'Output' })).toBeNull();
  mounted.rerender(f.app());
  f.state.cell.state = 'succeeded'; f.state.cell.result_message_id = 'message_result'; f.state.cell.finished_at = '2026-09-27T12:00:01Z';
  f.state.turn.state = 'succeeded'; f.state.turn.finished_at = '2026-09-27T12:00:01Z';
  f.state.messages.push({ ...f.state.messages[0], id: 'message_result', sequence: '9007199254740994', role: 'tool', parts: [{ type: 'tool_result', result: { call_id: 'call_fixture', output: result('committed stdout'), is_error: false } }] });
  await act(async () => { await f.view.latest(); await f.execution.refresh(); });
  expect(screen.getByRole('region', { name: 'Output' }).textContent).toBe('committed stdout');
});

it('keeps one selected card through partial ID, call commit, running cell and settled output', async () => {
  const f = await fixture('', 'starlark', true);
  f.state.turn.state = 'running'; f.state.turn.finished_at = null;
  const presentation = { version: 1 as const, attempt_id: 'attempt', truncated: false, parts: [{ id: 'p0', type: 'tool_call' as const, call_index: 0, call_id: 'call_fixture' }] };
  f.state.preview = { attempt_id: 'attempt', turn_id: 'turn', message_id: 'message_call', text: '', reasoning: '', calls: [{ index: 0, id: '', name: 'execute', arguments: '{"code":"print(42)' }], presentation, truncated: false, revision: '1' };
  await f.view.refresh(); await f.execution.refresh(); render(f.app());
  const article = screen.getByRole('article', { name: 'Execution 1' });
  const codeRegion = screen.getByRole('region', { name: 'Cell 1 · Starlark' });
  const selection = window.getSelection()!; const range = document.createRange(); range.selectNodeContents(codeRegion); selection.removeAllRanges(); selection.addRange(range);
  expect(selection.toString()).toBe('print(42)');
  f.state.preview.calls![0]!.id = 'call_fixture';
  await act(async () => { await f.view.refresh(); });
  expect(screen.getByRole('article', { name: 'Execution 1' })).toBe(article); expect(selection.toString()).toBe('print(42)');
  f.state.messages = [{ ...sample('Message'), id: 'message_call', session_id: 'session_child', turn_id: 'turn', group_id: 'turn', source: null, sequence: '9007199254740993', role: 'assistant', presentation, parts: [{ type: 'tool_call', call: { id: 'call_fixture', name: 'execute', arguments: { code: 'print(42)' } } }] } as Message];
  f.state.preview = null;
  await act(async () => { await f.view.latest(); });
  expect(screen.getByRole('article', { name: 'Execution 1' })).toBe(article); expect(selection.toString()).toBe('print(42)'); expect(screen.getByText('Writing')).toBeDefined();
  f.state.hasCell = true; f.state.cell.state = 'running'; f.state.cell.finished_at = null; f.state.cell.result_message_id = null;
  await act(async () => { await f.execution.refresh(); });
  expect(screen.getByRole('article', { name: 'Execution 1' })).toBe(article); expect(selection.toString()).toBe('print(42)'); expect(screen.getByText('Running')).toBeDefined();
  expect(screen.getByRole('region', { name: 'Cell 1 · Starlark' })).toBe(codeRegion);
  expect(screen.getByRole('button', { name: 'Copy Starlark code' })).toBeDefined();
  f.state.cell.state = 'succeeded'; f.state.cell.result_message_id = 'message_result'; f.state.cell.finished_at = '2026-09-27T12:00:01Z';
  f.state.messages.push({ ...f.state.messages[0]!, id: 'message_result', presentation: undefined, sequence: '9007199254740994', role: 'tool', parts: [{ type: 'tool_result', result: { call_id: 'call_fixture', output: result(), is_error: false } }] });
  await act(async () => { await f.view.latest(); await f.execution.refresh(); });
  expect(screen.getByRole('article', { name: 'Execution 1' })).toBe(article); expect(selection.toString()).toBe('print(42)'); expect(screen.getByText('Completed')).toBeDefined();
  fireEvent.click(screen.getByRole('button', { name: 'Show 3 more lines' }));
  await act(async () => { await f.execution.refresh(); });
  expect(screen.getByRole('region', { name: 'Output' }).textContent).toBe(output); expect(screen.getByRole('button', { name: 'Collapse output' })).toBeDefined();
  expect(document.querySelectorAll('[data-repl-cell]')).toHaveLength(1); expect(screen.queryByText(/provisional|protocol|checkpoint was recorded/i)).toBeNull();
});

it('renders imported code and output without claiming a local cell or starting a clock', async () => {
  const f = await fixture();
  f.state.hasCell = false;
  f.state.messages = f.state.messages.map(message => ({ ...message, turn_id: null, group_id: 'imported', source: { session_id: 'original', message_id: message.id, sequence: message.sequence } }));
  await f.view.latest(); await f.execution.refresh(); render(f.app());
  expect(screen.getByText('Completed')).toBeDefined(); expect(screen.getByRole('region', { name: 'Output' }).textContent).toContain('line 1');
  expect(screen.getByRole('button', { name: 'Copy Starlark code' })).toBeDefined();
  expect(screen.queryByTitle('Recorded execution duration')).toBeNull(); expect(screen.queryByText(/elapsed/)).toBeNull();
});

it('retains the writing card as failed evidence and reloads it without inventing a cell', async () => {
  const f = await fixture('', 'starlark', true);
  f.state.messages = [{ ...sample('Message'), id: 'input', session_id: 'session_child', turn_id: 'turn', group_id: 'turn', source: null, role: 'user', parts: [{ type: 'text', text: 'Run this' }] } as Message];
  const call = { index: 0, id: '', name: 'execute', arguments: '{"code":"before_failure' };
  const presentation = { version: 1 as const, attempt_id: 'failed_attempt', truncated: false, parts: [{ id: 'p0', type: 'tool_call' as const, call_index: 0, call }] };
  f.state.preview = { attempt_id: 'failed_attempt', turn_id: 'turn', message_id: 'pending', text: '', reasoning: '', calls: [call], presentation, truncated: false, revision: '1' };
  await f.view.latest(); await f.view.refresh(); render(f.app());
  const article = screen.getByRole('article', { name: 'Execution 1' }); expect(screen.getByText('Writing')).toBeDefined();
  f.state.preview = null; f.state.attempts = [{ attempt_id: 'failed_attempt', turn_id: 'turn', group_id: 'turn', state: 'failed', presentation }];
  await act(async () => { await f.view.refresh(); });
  expect(screen.getByRole('article', { name: 'Execution 1' })).toBe(article); expect(screen.getByText('Failed')).toBeDefined();
  expect(screen.getByRole('region', { name: 'Cell 1 · Execution' }).textContent).toBe('before_failure');
  await act(async () => { await f.view.latest(); });
  expect(screen.getByText('Failed')).toBeDefined(); expect(f.execution.getSnapshot().cells).toHaveLength(0);
  expect(screen.queryByText(/effects may|checkpoint|provisional/i)).toBeNull();
});
