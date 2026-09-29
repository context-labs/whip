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
  const state = {
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
    else if (request.method === 'sessions.activity') value = { ...sample('SessionActivity'), active_turn: null, active_input_id: null };
    else if (request.method === 'sessions.history_page') value = { snapshot: history, messages: state.messages, next_cursor: null };
    else if (request.method === 'sessions.observe') value = { snapshot: history, epoch: 'boot', messages: [], preview: state.preview };
    else if (request.method === 'sessions.turns') value = { items: [state.turn], next_cursor: null };
    else if (request.method === 'turns.cells') value = { items: after || empty ? [] : [state.cell] };
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
it('shows provisional code separately and exact host-operation state beside the committed cell', async () => {
  const f = await fixture();
  f.state.operations = [{ ...sample('HostOperation'), id: 'operation', session_id: 'session_child', turn_id: 'turn', cell_id: 'cell', state: 'waiting', result: null, dispatched_at: null, finished_at: null } as HostOperation];
  f.state.preview = { attempt_id: 'attempt', turn_id: 'turn', message_id: 'preview', text: '', reasoning: '', calls: [{ index: 0, id: 'next_call', name: 'execute', arguments: '{"code":' }], truncated: false, revision: '1' };
  await act(async () => { await f.view.refresh(); await f.execution.refresh(); }); render(f.app());
  expect(screen.getByRole('article', { name: 'Provisional execution' })).toBeDefined(); expect(screen.getByText('waiting')).toBeDefined();
  expect(document.querySelectorAll('[data-repl-cell]')).toHaveLength(1); expect(screen.getByText('Writing · provisional')).toBeDefined();
});

it('keeps uncertain cell metadata when exact transcript bodies are outside the window', async () => {
  const f = await fixture(); f.state.cell.state = 'uncertain'; f.state.messages = [];
  await f.view.latest(); await f.execution.refresh(); render(f.app());
  expect(screen.getByText('Outcome uncertain')).toBeDefined();
  expect(screen.getByText('The exact call message is outside the loaded transcript window.')).toBeDefined();
  expect(screen.getByText('The exact result message is outside the loaded transcript window.')).toBeDefined();
  expect(screen.getByText('Effects may already have happened. The recorded outcome is uncertain.')).toBeDefined();
  expect(screen.queryByRole('region', { name: 'Output' })).toBeNull();
});
