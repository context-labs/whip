import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { RemoteError, type Client } from '@whip/sdk';
import { validate } from '@whip/protocol';
import { providerFixture } from './provider-fixture';
import { AppRuntime } from '../src/runtime';
import { RuntimeContext } from '../src/context';
import { TerminalView, createWriteQueue, dragSelection, passesToApp, terminalFontFamily, wheelReports } from '../src/terminal-view';
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
    element = { getBoundingClientRect: () => ({ left: 0, top: 0, width: 800, height: 480 }), querySelector: () => ({ getBoundingClientRect: () => ({ left: 0, top: 0, width: 800, height: 480 }) }) } as unknown as HTMLElement;
    write = vi.fn(); reset = vi.fn(); focus = vi.fn(); dispose = vi.fn(); loadAddon = vi.fn(); open = vi.fn(); select = vi.fn();
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
    onSelectionChange = this.on('selection');
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

async function fakeClient() {
  const f = await providerFixture({ runtimeID: 'mac' });
  const state = { start: 0n, text: '', exited: false, exitCode: 0, signal: '' };
  const info = (id = 'term-1') => ({ process_epoch: 'boot', id, cwd: '/work', shell: '/bin/zsh', cols: 80, rows: 24, closing: false, exited: state.exited, exit_code: state.exitCode, signal: state.signal, start: String(state.start), end: String(state.start + BigInt(state.text.length)), created_at: '2026-09-28T00:00:00Z' });
  f.data.handlers['terminal.read'] = request => {
    if (request.method !== 'terminal.read' || !validate('TerminalReadParams', request.params)) throw new Error('Wrong request');
    const requested = BigInt(request.params.cursor), from = requested < state.start ? state.start : requested;
    const text = state.text.slice(Number(from - state.start), Number(from - state.start) + request.params.limit);
    return { terminal: info(), from: String(from), next: String(from + BigInt(text.length)), end: info().end, truncated: from !== requested, data_base64: btoa(text) };
  };
  f.data.handlers['terminal.write'] = f.data.handlers['terminal.resize'] = f.data.handlers['terminal.close'] = () => ({ accepted: true });
  f.data.handlers['terminal.open'] = () => info('term-2');
  f.data.handlers['terminal.list'] = () => ({ process_epoch: 'boot', items: [] });
  const terminals = { read: vi.spyOn(f.client, 'readTerminal'), write: vi.spyOn(f.client, 'writeTerminal'), resize: vi.spyOn(f.client, 'resizeTerminal'), close: vi.spyOn(f.client, 'closeTerminal'), open: vi.spyOn(f.client, 'openTerminal') };
  return { ...f, terminals, info,
    emitOutput: async (id: string, cursor: number, text: string) => {
      if (id !== 'term-1') return;
      if (BigInt(cursor) !== state.start + BigInt(state.text.length)) { state.start = BigInt(cursor); state.text = ''; }
      state.text += text;
      await waitFor(() => expect(terminal().write.mock.calls.some(([bytes]) => new TextDecoder().decode(bytes as Uint8Array).includes(text))).toBe(true));
    },
    emitExited: async (value: { id: string; exitCode: number; signal?: string }) => { state.exited = true; state.exitCode = value.exitCode; state.signal = value.signal ?? ''; await screen.findByText(/Shell exited/); },
  };
}

function mount(client: Client, focused = true, processEpoch: string | undefined = client.processEpoch) {
  const disk = new Map<string, string>();
  const runtime = new AppRuntime({ defaultConnection: localProfile, connectionKinds: ['local'], resolveConnection: resolveURLConnection,
    storage: { keys: () => [...disk.keys()], getItem: key => disk.get(key) ?? null, setItem: (key, value) => { disk.set(key, value); }, removeItem: key => { disk.delete(key); } },
    copy: vi.fn(async () => {}), openExternal: async () => {}, download: async () => 'saved' });
  const id = runtime.tabs.openTerminal('mac', 'term-1', '/work', undefined, processEpoch);
  const tab = () => runtime.tabs.workspace().tabs.find(item => item.id === id)!;
  let connected = true;
  vi.spyOn(runtime.connections, 'isAttached').mockImplementation(() => connected);
  const tree = () => <RuntimeContext.Provider value={runtime}><UIProvider><ThemeProvider>
    <TerminalView tab={tab() as Extract<ReturnType<typeof tab>, { kind: 'terminal' }>} client={client} focused={focused} connected={connected} />
  </ThemeProvider></UIProvider></RuntimeContext.Provider>;
  const view = render(tree());
  return { runtime, id, tab, view, setConnected: (value: boolean) => { connected = value; view.rerender(tree()); } };

}

const terminal = () => ghostty.Terminal.instances[0]!;
const ready = async () => { await waitFor(() => expect(ghostty.Terminal.instances).toHaveLength(1)); return terminal(); };

it('attaches from cursor zero, writes output in order, forwards keystrokes, titles and debounced resizes', async () => {
  const fake = await fakeClient();
  const { runtime, tab } = mount(fake.client);
  await waitFor(() => expect(fake.terminals.read).toHaveBeenCalledWith({ id: 'term-1', process_epoch: 'boot' }, '0', 32768, { signal: expect.any(AbortSignal) }));
  const instance = await ready();
  expect(instance.open).toHaveBeenCalledTimes(1);
  expect(instance.focus).toHaveBeenCalled();
  // Powerline prompts need PUA glyphs; Chromium only tries fonts that are named.
  expect(String(instance.options.fontFamily)).toContain("'Symbols Nerd Font Mono'");
  expect(String(instance.options.fontFamily)).toContain("'MesloLGS NF'");
  expect(terminalFontFamily("Menlo, monospace").startsWith('Menlo, monospace, ')).toBe(true);
  expect(terminalFontFamily('')).toContain("'JetBrains Mono Variable'");
  await fake.emitOutput('term-1', 0, '$ ');
  await fake.emitOutput('other', 0, 'ignored');
  await fake.emitOutput('term-1', 2, 'ls\r\n');
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
  await waitFor(() => expect(fake.terminals.resize).toHaveBeenCalledWith({ id: 'term-1', process_epoch: 'boot' }, 120, 40));
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

it('answers the native copy command with the canvas selection and offers Copy on right-click', async () => {
  const fake = await fakeClient();
  const { view } = mount(fake.client);
  const instance = await ready();
  const surface = view.container.querySelector('[data-terminal-view] > div, [data-terminal-view] div') as HTMLElement;
  const clipboard = { setData: vi.fn() };
  const copyEvent = () => Object.assign(new Event('copy', { bubbles: true, cancelable: true }), { clipboardData: clipboard });
  const beforeCopy = () => new Event('beforecopy', { bubbles: true, cancelable: true });
  // Nothing selected: the events pass through untouched.
  expect(surface.dispatchEvent(beforeCopy())).toBe(true);
  expect(surface.dispatchEvent(copyEvent())).toBe(true);
  expect(clipboard.setData).not.toHaveBeenCalled();
  instance.selection = 'whip-42';
  act(() => instance.emit('selection', undefined));
  expect(view.container.querySelector('[data-terminal-view]')!.getAttribute('data-terminal-selection')).toBe('true');
  expect(surface.dispatchEvent(beforeCopy())).toBe(false);
  expect(surface.dispatchEvent(copyEvent())).toBe(false);
  expect(clipboard.setData).toHaveBeenCalledWith('text/plain', 'whip-42');
  instance.selection = '';
  act(() => instance.emit('selection', undefined));
  expect(view.container.querySelector('[data-terminal-view]')!.getAttribute('data-terminal-selection')).toBe('false');
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
  const fake = await fakeClient();
  mount(fake.client);
  const instance = await ready();
  await fake.emitOutput('term-1', 0, 'abc');
  await fake.emitOutput('term-1', 3, 'def');
  expect(instance.reset).not.toHaveBeenCalled();
  await fake.emitOutput('term-1', 40, 'later');
  expect(instance.reset).toHaveBeenCalledTimes(1);
  expect(instance.write).toHaveBeenCalledTimes(3);
});

it('reports an exited shell and restarts it behind the same tab', async () => {
  const fake = await fakeClient();
  const { tab, id } = mount(fake.client);
  await ready();
  await fake.emitExited({ id: 'term-1', exitCode: 3 });
  expect(screen.getByRole('status').textContent).toContain('Shell exited (code 3).');
  fireEvent.click(screen.getByRole('button', { name: 'Restart' }));
  await waitFor(() => expect(fake.terminals.open).toHaveBeenCalledWith({ cwd: '/work', cols: 80, rows: 24 }));
  await waitFor(() => expect(tab()).toMatchObject({ id, kind: 'terminal', terminalId: 'term-2', cwd: '/work' }));
  expect(routing.navigate).toHaveBeenCalledWith(expect.objectContaining({ to: '/h/$runtimeId/t/$terminalId', params: { runtimeId: 'mac', terminalId: 'term-2' }, replace: true }));
});

it('shows an ended shell when the daemon no longer knows the terminal', async () => {
  const fake = await fakeClient();
  fake.terminals.read.mockRejectedValueOnce(new RemoteError({ code: -32003, kind: 'NOT_FOUND', message: 'terminal not found' }));
  mount(fake.client);
  await waitFor(() => expect(screen.getByRole('status').textContent).toContain('This shell has ended.'));
  expect(screen.getByRole('button', { name: 'Restart' })).toBeTruthy();
});

it('stops observation while disconnected and resumes exact native reads on reconnect', async () => {
  const fake = await fakeClient(); const f = mount(fake.client); await ready();
  await fake.emitOutput('term-1', 0, 'hello');
  f.setConnected(false); await screen.findByText('Reconnecting to the host…');
  const calls = fake.terminals.read.mock.calls.length;
  await new Promise(resolve => setTimeout(resolve, 300)); expect(fake.terminals.read).toHaveBeenCalledTimes(calls);
  f.setConnected(true); await waitFor(() => expect(fake.terminals.read.mock.calls.length).toBeGreaterThan(calls));
  expect(fake.terminals.close).not.toHaveBeenCalled();
});
it('never attaches a terminal retained from a different host process', async () => {
  const fake = await fakeClient(); mount(fake.client, true, 'retired-boot');
  await screen.findByText('This shell has ended.'); expect(fake.terminals.read).not.toHaveBeenCalled();
  expect(fake.terminals.write).not.toHaveBeenCalled(); expect(fake.terminals.open).not.toHaveBeenCalled();
});
it('bounds pending terminal input and discards it after overflow or disposal', async () => {
  const send = vi.fn(() => new Promise<void>(() => {})), error = vi.fn();
  const queue = createWriteQueue(send, error); queue.push('first'); queue.push('x'.repeat(256 << 10)); queue.push('overflow');
  expect(error).toHaveBeenCalledOnce(); expect(queue.pendingBytes).toBe(0); queue.push('later'); expect(send).toHaveBeenCalledOnce();
  const other = createWriteQueue(send); other.dispose(); other.push('never'); expect(send).toHaveBeenCalledOnce();
});

it('selects locally on Shift+drag while a program has mouse tracking, keeping the drag from the reporter', async () => {
  const geometry = { left: 0, top: 0, width: 800, height: 480, cols: 80, rows: 24 };
  expect(dragSelection({ x: 15, y: 5 }, { x: 55, y: 45 }, geometry)).toEqual({ column: 1, row: 0, length: 165 });
  expect(dragSelection({ x: 55, y: 45 }, { x: 15, y: 5 }, geometry)).toEqual({ column: 1, row: 0, length: 165 });
  expect(dragSelection({ x: -9, y: -9 }, { x: 9999, y: 9999 }, geometry)).toEqual({ column: 0, row: 0, length: 80 * 24 });
  const fake = await fakeClient();
  const { view } = mount(fake.client);
  const instance = await ready();
  const frame = view.container.querySelector('[data-terminal-view]') as HTMLElement;
  const surface = frame.firstElementChild as HTMLElement;
  const reachedAncestor = vi.fn();
  frame.addEventListener('mousedown', reachedAncestor);
  frame.addEventListener('mouseup', reachedAncestor);
  const drag = (shiftKey: boolean) => {
    fireEvent.mouseDown(surface, { shiftKey, button: 0, buttons: 1, clientX: 15, clientY: 5 });
    fireEvent.mouseMove(surface, { shiftKey, buttons: 1, clientX: 55, clientY: 45 });
    fireEvent.mouseUp(surface, { shiftKey, button: 0, clientX: 55, clientY: 45 });
  };
  // Without mouse tracking the drag is ghostty's own selection; with it and no Shift it belongs to the program.
  drag(true);
  instance.mouseTracking = true;
  drag(false);
  expect(instance.select).not.toHaveBeenCalled();
  expect(reachedAncestor).toHaveBeenCalledTimes(4);
  drag(true);
  expect(instance.select).toHaveBeenCalledWith(1, 0, 165);
  expect(instance.focus).toHaveBeenCalled();
  expect(reachedAncestor).toHaveBeenCalledTimes(4);
  // A drag that ended outside the view does not keep extending the selection.
  fireEvent.mouseMove(surface, { shiftKey: true, buttons: 0, clientX: 95, clientY: 45 });
  expect(instance.select).toHaveBeenCalledTimes(1);
});

it('lets app shortcuts through and copies a selection with the platform chord', async () => {
  const fake = await fakeClient();
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

it('waits for the original open reply before offering recovery or another open', async () => {
  const fake = await fakeClient(); const f = mount(fake.client); await ready();
  await fake.emitExited({ id: 'term-1', exitCode: 0 });
  let reject!: (error: Error) => void;
  fake.terminals.open.mockImplementationOnce(() => new Promise((_, fail) => { reject = fail; }));
  fireEvent.click(screen.getByRole('button', { name: 'Restart' }));
  await screen.findByText('Waiting for the host’s open reply…');
  expect((screen.getByRole('button', { name: 'Check existing shells' }) as HTMLButtonElement).disabled).toBe(true);
  expect(screen.queryByRole('button', { name: 'Open another shell' })).toBeNull();
  expect(fake.terminals.open).toHaveBeenCalledOnce();
  await act(async () => reject(new Error('reply lost')));
  await screen.findByText('No shells are currently retained by this host process.');
  expect(f.tab()).toMatchObject({ terminalId: 'term-1', opening: { processEpoch: 'boot' } });
  expect(fake.terminals.open).toHaveBeenCalledOnce();
});

it('requires an explicit choice even for one candidate and preserves the original shell', async () => {
  const fake = await fakeClient(); const f = mount(fake.client); const renderer = await ready();
  fake.data.handlers['terminal.list'] = () => ({ process_epoch: 'boot', items: [fake.info()] });
  act(() => { f.runtime.tabs.beginTerminalOpen(f.id, 'boot'); });
  const choose = await screen.findByRole('button', { name: 'Keep existing shell' });
  expect(f.tab()).toHaveProperty('opening');
  expect(fake.terminals.open).not.toHaveBeenCalled(); expect(fake.terminals.close).not.toHaveBeenCalled();
  expect(renderer.dispose).toHaveBeenCalledOnce();
  renderer.emit('data', 'stale keystroke');
  expect(fake.terminals.write).not.toHaveBeenCalled();
  fireEvent.click(choose);
  await waitFor(() => expect(f.tab()).not.toHaveProperty('opening'));
  expect(f.tab()).toMatchObject({ terminalId: 'term-1', processEpoch: 'boot' });
  expect(fake.terminals.open).not.toHaveBeenCalled(); expect(fake.terminals.write).not.toHaveBeenCalled();
});

it('shows all ambiguous candidates without choosing by their identical directory or time', async () => {
  const fake = await fakeClient(); const f = mount(fake.client); await ready();
  fake.data.handlers['terminal.list'] = () => ({ process_epoch: 'boot', items: [fake.info('a'), fake.info('b')] });
  act(() => { f.runtime.tabs.beginTerminalOpen(f.id, 'boot'); });
  await waitFor(() => expect(screen.getAllByRole('button', { name: 'Use this shell' })).toHaveLength(2));
  expect(f.tab()).toMatchObject({ terminalId: 'term-1' });
  fireEvent.click(screen.getAllByRole('button', { name: 'Use this shell' })[1]!);
  await waitFor(() => expect(f.tab()).toMatchObject({ terminalId: 'b' }));
  expect(fake.terminals.open).not.toHaveBeenCalled();
});

it('requires a fresh successful list and another review if candidates changed before replacement', async () => {
  const fake = await fakeClient(); const f = mount(fake.client); await ready();
  const list = vi.spyOn(fake.client, 'listTerminals');
  list.mockRejectedValueOnce(new Error('inspection unavailable'));
  act(() => { f.runtime.tabs.beginTerminalOpen(f.id, 'boot'); });
  await screen.findByText('inspection unavailable');
  expect(screen.queryByRole('button', { name: 'Open another shell' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Check existing shells' }));
  await screen.findByRole('button', { name: 'Open another shell' });
  fake.data.handlers['terminal.list'] = () => ({ process_epoch: 'boot', items: [fake.info('arrived')] });
  fireEvent.click(screen.getByRole('button', { name: 'Open another shell' }));
  await screen.findByText('The host’s shells changed. Review this list before opening another.');
  expect(fake.terminals.open).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Open another shell' }));
  await waitFor(() => expect(fake.terminals.open).toHaveBeenCalledOnce());
  await waitFor(() => expect(f.tab()).toMatchObject({ terminalId: 'term-2' }));
  expect(list).toHaveBeenCalledTimes(4);
});

it('retains the uncertain marker if an explicitly reviewed replacement also loses its reply', async () => {
  const fake = await fakeClient(); const f = mount(fake.client); await ready();
  act(() => { f.runtime.tabs.beginTerminalOpen(f.id, 'boot'); });
  await screen.findByRole('button', { name: 'Open another shell' });
  fake.terminals.open.mockRejectedValueOnce(new Error('second reply lost'));
  fireEvent.click(screen.getByRole('button', { name: 'Open another shell' }));
  await screen.findByText('second reply lost');
  await waitFor(() => expect((screen.getByRole('button', { name: 'Check existing shells' }) as HTMLButtonElement).disabled).toBe(false));
  expect(f.tab()).toHaveProperty('opening'); expect(fake.terminals.open).toHaveBeenCalledOnce();
});

it('inspects current process candidates without sending a marker retained from the ended process', async () => {
  const fake = await fakeClient(); const f = mount(fake.client, true, 'old-process');
  await screen.findByText('This shell has ended.');
  fake.data.handlers['terminal.list'] = () => ({ process_epoch: 'boot', items: [fake.info('current')] });
  act(() => { f.runtime.tabs.beginTerminalOpen(f.id, 'old-process'); });
  await screen.findByText('The previous host process has ended. The list below belongs to the current process.');
  fireEvent.click(await screen.findByRole('button', { name: 'Use this shell' }));
  await waitFor(() => expect(f.tab()).toMatchObject({ terminalId: 'current', processEpoch: 'boot' }));
  expect(fake.terminals.open).not.toHaveBeenCalled(); expect(fake.terminals.write).not.toHaveBeenCalled();
});

it('waits for reconnect to inspect an uncertain open and never repeats open on reconnect', async () => {
  const fake = await fakeClient(); const f = mount(fake.client); await ready();
  f.setConnected(false);
  const list = vi.spyOn(fake.client, 'listTerminals');
  act(() => { f.runtime.tabs.beginTerminalOpen(f.id, 'boot'); });
  await screen.findByText('Reconnect this host to inspect existing shells.');
  expect(list).not.toHaveBeenCalled(); expect(fake.terminals.open).not.toHaveBeenCalled();
  f.setConnected(true);
  await screen.findByRole('button', { name: 'Open another shell' });
  expect(list).toHaveBeenCalledOnce(); expect(fake.terminals.open).not.toHaveBeenCalled();
});
