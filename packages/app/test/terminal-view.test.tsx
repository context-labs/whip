import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { ThemeProvider, UIProvider } from '@whip/ui';
import type { WhipClient } from '@whip/sdk';
import { RpcError } from '@whip/sdk';
import { AppRuntime } from '../src/runtime';
import { RuntimeContext } from '../src/context';
import { TerminalView, createWriteQueue, passesToApp, terminalFontFamily, wheelReports } from '../src/terminal-view';
import { localProfile, resolveURLConnection } from '../src/platform';

const routing = vi.hoisted(() => ({ navigate: vi.fn(async () => {}) }));
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => routing.navigate }));
const ghostty = vi.hoisted(() => {
  type Listener = (value: unknown) => void;
  class Terminal {
    static instances: Terminal[] = [];
    cols = 80; rows = 24;
    selection = '';
    handlers = new Map<string, Listener[]>();
    keyHandler?: (event: KeyboardEvent) => boolean;
    wheelHandler?: (event: WheelEvent) => boolean;
    mouseTracking = false;
    element = { getBoundingClientRect: () => ({ left: 0, top: 0, width: 800, height: 480 }) } as unknown as HTMLElement;
    write = vi.fn(); reset = vi.fn(); focus = vi.fn(); dispose = vi.fn(); loadAddon = vi.fn(); open = vi.fn();
    constructor(readonly options: Record<string, unknown>) { Terminal.instances.push(this); }
    attachCustomKeyEventHandler(handler: (event: KeyboardEvent) => boolean) { this.keyHandler = handler; }
    attachCustomWheelEventHandler(handler: (event: WheelEvent) => boolean) { this.wheelHandler = handler; }
    hasMouseTracking() { return this.mouseTracking; }
    getMode() { return true; }
    private on(name: string) {
      return (listener: Listener) => { this.handlers.set(name, [...(this.handlers.get(name) ?? []), listener]); return { dispose() {} }; };
    }
    onData = this.on('data'); onResize = this.on('resize'); onTitleChange = this.on('title');
    hasSelection() { return this.selection !== ''; }
    getSelection() { return this.selection; }
    emit(name: string, value: unknown) { for (const listener of this.handlers.get(name) ?? []) listener(value); }
  }
  class FitAddon { fit = vi.fn(); }
  const Ghostty = { load: vi.fn(async () => ({ fake: true })) };
  return { Terminal, FitAddon, Ghostty };
});
vi.mock('ghostty-web', () => ({ Terminal: ghostty.Terminal, FitAddon: ghostty.FitAddon, Ghostty: ghostty.Ghostty }));
vi.mock('ghostty-web/ghostty-vt.wasm?url', () => ({ default: '/assets/ghostty-vt.wasm' }));

beforeEach(() => {
  ghostty.Terminal.instances = [];
  routing.navigate.mockClear();
  vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} });
});
afterEach(() => vi.unstubAllGlobals());

function fakeClient() {
  const output = new Set<(value: { id: string; cursor: number; bytes: Uint8Array }) => void>();
  const exited = new Set<(value: { id: string; exitCode: number; signal?: string }) => void>();
  const detached = new Set<(id: string) => void>();
  const connectionListeners = new Set<() => void>();
  const connection = { state: 'connected' as string, info: { runtime_id: 'mac', capabilities: ['terminals'] } };
  const terminals = {
    attach: vi.fn(async () => ({ cursor: 0, cwd: '/work', cols: 80, rows: 24, exited: false })),
    write: vi.fn(async () => {}), resize: vi.fn(async () => {}), close: vi.fn(async () => {}),
    open: vi.fn(async () => ({ id: 'term-2', shell: '/bin/zsh', cwd: '/work' })),
    onOutput: (listener: (value: { id: string; cursor: number; bytes: Uint8Array }) => void) => { output.add(listener); return () => output.delete(listener); },
    onExited: (listener: (value: { id: string; exitCode: number; signal?: string }) => void) => { exited.add(listener); return () => exited.delete(listener); },
    onDetached: (listener: (id: string) => void) => { detached.add(listener); return () => detached.delete(listener); },
  };
  const client = { getSnapshot: () => connection, subscribe: (fn: () => void) => { connectionListeners.add(fn); return () => connectionListeners.delete(fn); }, terminals };
  return {
    client: client as unknown as WhipClient, terminals,
    emitOutput: (id: string, cursor: number, text: string) => act(() => output.forEach(fn => fn({ id, cursor, bytes: new TextEncoder().encode(text) }))),
    emitExited: (value: { id: string; exitCode: number; signal?: string }) => act(() => exited.forEach(fn => fn(value))),
    emitDetached: (id: string) => act(() => detached.forEach(fn => fn(id))),
    setState: (state: string) => act(() => { connection.state = state; connectionListeners.forEach(fn => fn()); }),
  };
}

function mount(client: WhipClient, focused = true) {
  const disk = new Map<string, string>();
  const runtime = new AppRuntime({ defaultConnection: localProfile, connectionKinds: ['local'], resolveConnection: resolveURLConnection,
    storage: { keys: () => [...disk.keys()], getItem: key => disk.get(key) ?? null, setItem: (key, value) => { disk.set(key, value); }, removeItem: key => { disk.delete(key); } },
    copy: vi.fn(async () => {}), openExternal: async () => {}, download: async () => 'saved' });
  const id = runtime.tabs.openTerminal('mac', 'term-1', '/work');
  const tab = () => runtime.tabs.workspace().tabs.find(item => item.id === id)!;
  const view = render(<RuntimeContext.Provider value={runtime}><UIProvider><ThemeProvider>
    <TerminalView tab={tab() as Extract<ReturnType<typeof tab>, { kind: 'terminal' }>} client={client} focused={focused} />
  </ThemeProvider></UIProvider></RuntimeContext.Provider>);
  return { runtime, id, tab, view };
}

const terminal = () => ghostty.Terminal.instances[0]!;
const ready = async () => { await waitFor(() => expect(ghostty.Terminal.instances).toHaveLength(1)); return terminal(); };

it('attaches from cursor zero, writes output in order, forwards keystrokes, titles and debounced resizes', async () => {
  const fake = fakeClient();
  const { runtime, tab } = mount(fake.client);
  await waitFor(() => expect(fake.terminals.attach).toHaveBeenCalledWith('term-1', 0));
  const instance = await ready();
  expect(instance.open).toHaveBeenCalledTimes(1);
  expect(instance.focus).toHaveBeenCalled();
  // Powerline prompts need PUA glyphs; Chromium only tries fonts that are named.
  expect(String(instance.options.fontFamily)).toContain("'Symbols Nerd Font Mono'");
  expect(String(instance.options.fontFamily)).toContain("'MesloLGS NF'");
  expect(terminalFontFamily("Menlo, monospace").startsWith('Menlo, monospace, ')).toBe(true);
  expect(terminalFontFamily('')).toContain("'JetBrains Mono Variable'");
  fake.emitOutput('term-1', 0, '$ ');
  fake.emitOutput('other', 0, 'ignored');
  fake.emitOutput('term-1', 2, 'ls\r\n');
  expect(instance.write.mock.calls.map(([bytes]) => new TextDecoder().decode(bytes as Uint8Array))).toEqual(['$ ', 'ls\r\n']);
  expect(instance.reset).not.toHaveBeenCalled();
  instance.emit('data', 'echo hi\r');
  await waitFor(() => expect(fake.terminals.write).toHaveBeenCalledTimes(1));
  expect(new TextDecoder().decode(fake.terminals.write.mock.calls[0]![1] as Uint8Array)).toBe('echo hi\r');
  // Wheel travel only becomes mouse reports once the program asks for them.
  const wheel = { deltaY: 40, deltaMode: 0, clientX: 15, clientY: 25, shiftKey: false, altKey: false, ctrlKey: false } as WheelEvent;
  expect(instance.wheelHandler!(wheel)).toBe(false);
  instance.mouseTracking = true;
  expect(instance.wheelHandler!(wheel)).toBe(true);
  // Two lines of travel: the queue sends the first report at once and the second when it settles.
  await waitFor(() => expect(fake.terminals.write).toHaveBeenCalledTimes(3));
  expect(fake.terminals.write.mock.calls.slice(1).map(([, bytes]) => new TextDecoder().decode(bytes as Uint8Array)).join('')).toBe('\x1b[<65;2;2M'.repeat(2));
  instance.emit('title', 'zsh — project');
  expect(tab()).toMatchObject({ kind: 'terminal', titleHint: 'zsh — project' });
  instance.emit('resize', { cols: 100, rows: 40 });
  instance.emit('resize', { cols: 120, rows: 40 });
  await waitFor(() => expect(fake.terminals.resize).toHaveBeenCalledWith('term-1', 120, 40));
  expect(fake.terminals.resize).toHaveBeenCalledTimes(1);
  expect(fake.terminals.close).not.toHaveBeenCalled();
  expect(runtime.getSnapshot().workspaceError).toBeUndefined();
});

it('keeps one write in flight and coalesces keystrokes typed meanwhile, in order', async () => {
  const sent: string[] = [];
  const resolvers: (() => void)[] = [];
  const send = vi.fn((bytes: Uint8Array) => new Promise<void>(resolve => { sent.push(new TextDecoder().decode(bytes)); resolvers.push(resolve); }));
  const errors: unknown[] = [];
  const queue = createWriteQueue(send, error => errors.push(error));
  queue.push('a'); queue.push('b'); queue.push('c');
  expect(sent).toEqual(['a']);
  expect(queue.pendingBytes).toBe(2);
  resolvers.shift()!();
  await waitFor(() => expect(sent).toEqual(['a', 'bc']));
  queue.push('');
  resolvers.shift()!();
  await new Promise(resolve => setTimeout(resolve, 0));
  expect(send).toHaveBeenCalledTimes(2);
  // Large pastes split at the daemon's per-write bound without reordering.
  queue.push('x'.repeat(20_000)); queue.push('tail');
  await waitFor(() => expect(sent.length).toBe(3));
  expect(sent[2]!.length).toBe(16_384);
  resolvers.shift()!();
  await waitFor(() => expect(sent.length).toBe(4));
  expect(sent[3]).toBe('x'.repeat(20_000 - 16_384) + 'tail');
  resolvers.shift()!();
  // A failed write drops what was pending instead of replaying stale keystrokes later.
  const failing = createWriteQueue(() => Promise.reject(new Error('gone')), error => errors.push(error));
  failing.push('lost'); failing.push('too');
  await waitFor(() => expect(errors).toHaveLength(1));
  expect(failing.pendingBytes).toBe(0);
});

it('turns wheel travel into SGR mouse reports at the cell under the pointer', () => {
  const geometry = { left: 100, top: 50, width: 800, height: 400, cols: 80, rows: 20 };
  const wheel = (deltaY: number, deltaMode = 0, extra: Partial<WheelEvent> = {}) => ({ deltaY, deltaMode, clientX: 100 + 10 * 4.5, clientY: 50 + 20 * 2.5, shiftKey: false, altKey: false, ctrlKey: false, ...extra });
  // One mouse notch of 60px at a 20px line height is three lines down at column 5, row 3.
  expect(wheelReports(wheel(60), geometry)).toEqual({ sequences: Array(3).fill('\x1b[<65;5;3M'), remainder: 0 });
  expect(wheelReports(wheel(-20), geometry).sequences).toEqual(['\x1b[<64;5;3M']);
  // Trackpad pixels accumulate: 8px twice is nothing, the third crosses a line.
  let state = wheelReports(wheel(8), geometry, 0);
  expect(state.sequences).toEqual([]);
  state = wheelReports(wheel(8), geometry, state.remainder);
  expect(state.sequences).toEqual([]);
  state = wheelReports(wheel(8), geometry, state.remainder);
  expect(state.sequences).toEqual(['\x1b[<65;5;3M']);
  expect(state.remainder).toBeCloseTo(0.2);
  // Reversing direction drops the carried remainder instead of fighting it.
  expect(wheelReports(wheel(-20), geometry, 0.9).sequences).toEqual(['\x1b[<64;5;3M']);
  // Line mode counts lines directly; modifiers set the SGR bits; positions clamp to the grid.
  expect(wheelReports(wheel(2, 1, { shiftKey: true, ctrlKey: true }), geometry).sequences).toEqual(Array(2).fill('\x1b[<85;5;3M'));
  expect(wheelReports({ ...wheel(20), clientX: 5000, clientY: -100 }, geometry).sequences).toEqual(['\x1b[<65;80;1M']);
  expect(wheelReports(wheel(100_000), geometry).sequences).toHaveLength(20);
});

it('redraws when the daemon replays from a different cursor than the view saw', async () => {
  const fake = fakeClient();
  mount(fake.client);
  const instance = await ready();
  fake.emitOutput('term-1', 0, 'abc');
  fake.emitOutput('term-1', 3, 'def');
  expect(instance.reset).not.toHaveBeenCalled();
  fake.emitOutput('term-1', 40, 'later');
  expect(instance.reset).toHaveBeenCalledTimes(1);
  expect(instance.write).toHaveBeenCalledTimes(3);
});

it('reports an exited shell and restarts it behind the same tab', async () => {
  const fake = fakeClient();
  const { tab, id } = mount(fake.client);
  await ready();
  fake.emitExited({ id: 'term-1', exitCode: 3 });
  expect(screen.getByRole('status').textContent).toContain('Shell exited (code 3).');
  fireEvent.click(screen.getByRole('button', { name: 'Restart' }));
  await waitFor(() => expect(fake.terminals.open).toHaveBeenCalledWith({ cwd: '/work', cols: 80, rows: 24 }));
  await waitFor(() => expect(tab()).toMatchObject({ id, kind: 'terminal', terminalId: 'term-2', cwd: '/work' }));
  expect(routing.navigate).toHaveBeenCalledWith(expect.objectContaining({ to: '/h/$runtimeId/t/$terminalId', params: { runtimeId: 'mac', terminalId: 'term-2' }, replace: true }));
});

it('shows an ended shell when the daemon no longer knows the terminal', async () => {
  const fake = fakeClient();
  fake.terminals.attach.mockRejectedValueOnce(new RpcError({ code: -32003, message: 'terminal not found' }));
  mount(fake.client);
  await waitFor(() => expect(screen.getByRole('status').textContent).toContain('This shell has ended.'));
  expect(screen.getByRole('button', { name: 'Restart' })).toBeTruthy();
});

it('reports detachment and reattaches from the last cursor on request or reconnect', async () => {
  const fake = fakeClient();
  mount(fake.client);
  await ready();
  fake.emitOutput('term-1', 0, 'hello');
  fake.emitDetached('term-1');
  expect(screen.getByRole('status').textContent).toContain('Attached in another window.');
  fireEvent.click(screen.getByRole('button', { name: 'Reattach here' }));
  await waitFor(() => expect(fake.terminals.attach).toHaveBeenLastCalledWith('term-1', 5));
  await waitFor(() => expect(screen.queryByRole('status')).toBeNull());
  fake.setState('reconnecting');
  expect(screen.getByRole('status').textContent).toContain('Reconnecting to the host…');
  const attaches = fake.terminals.attach.mock.calls.length;
  fake.setState('connected');
  await waitFor(() => expect(fake.terminals.attach.mock.calls.length).toBe(attaches + 1));
  expect(fake.terminals.attach).toHaveBeenLastCalledWith('term-1', 5);
});

it('lets app shortcuts through and copies a selection with the platform chord', async () => {
  const fake = fakeClient();
  const { runtime } = mount(fake.client);
  const instance = await ready();
  const key = (init: KeyboardEventInit) => new KeyboardEvent('keydown', init);
  expect(passesToApp(key({ key: '`', code: 'Backquote', ctrlKey: true }), ['Control+`'])).toBe(true);
  expect(passesToApp(key({ key: 'k', code: 'KeyK', ctrlKey: true }), ['Control+`'])).toBe(false);
  // ghostty-web: true = handled by the app, false = send to the shell.
  expect(instance.keyHandler!(key({ key: '`', code: 'Backquote', ctrlKey: true }))).toBe(true);
  expect(instance.keyHandler!(key({ key: 'c', code: 'KeyC', ctrlKey: true }))).toBe(false);
  instance.selection = 'selected text';
  expect(instance.keyHandler!(key({ key: 'c', code: 'KeyC', metaKey: true }))).toBe(true);
  await waitFor(() => expect(runtime.platform.copy).toHaveBeenCalledWith('selected text'));
  expect(instance.keyHandler!(key({ key: 'a', code: 'KeyA' }))).toBe(false);
});
