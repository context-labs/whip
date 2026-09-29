import { useEffect, useRef, useState, useSyncExternalStore } from 'react';
import { useNavigate } from '@tanstack/react-router';
import { matchesKeyboardEvent, type Hotkey } from '@tanstack/react-hotkeys';
import * as stylex from '@stylexjs/stylex';
import { Button, ContextMenu } from '@whip/ui';
import { colors, surface, typography } from '@whip/ui/tokens.stylex';
import { useTheme } from '@whip/ui/themes';
import { RemoteError, type Client, type TerminalInfo } from '@whip/sdk';
import { readTerminalOutput } from './terminal-output';
const MAX_TERMINAL_WRITE_BYTES = 16384;
const MAX_PENDING_INPUT_BYTES = 256 << 10;
import { FitAddon, Ghostty, Terminal } from 'ghostty-web';
import wasmUrl from 'ghostty-web/ghostty-vt.wasm?url';
import { useAppState, useRuntime, useSessionTabs } from './context';
import { ErrorNotice } from './error-feedback';
import { inspectTerminals, sendTerminalOpen, subscribeTerminalOpens, terminalOpenInFlight } from './terminal-open';
import { selectedSessionTab } from './session-tabs';
import { tabDestination } from './session-tab-routing';
import type { TerminalTab } from './session-tabs';
import { layout } from './styles';

/** One WASM compile per window; a failed load is retried on the next mount. */
let ghosttyLoad: Promise<Ghostty> | undefined;
export function loadGhostty(): Promise<Ghostty> {
  ghosttyLoad ??= Ghostty.load(wasmUrl).catch(error => { ghosttyLoad = undefined; throw error; });
  return ghosttyLoad;
}

type TerminalStatus =
  | { kind: 'loading' }
  | { kind: 'live' }
  | { kind: 'reconnecting' }
  | { kind: 'exited'; exitCode: number; signal?: string }
  | { kind: 'ended' }
  | { kind: 'error'; message: string };

/**
 * The terminal draws with the UI's mono stack, then Nerd Font families for the
 * Private Use Area glyphs powerline prompts use. Chromium never falls back to
 * system fonts for PUA code points, so a family must be named to be tried;
 * families that are not installed are skipped per glyph.
 */
export function terminalFontFamily(uiFamily: string): string {
  const base = uiFamily.trim() || "'JetBrains Mono Variable', ui-monospace, SFMono-Regular, Menlo, monospace";
  return `${base}, 'Symbols Nerd Font Mono', 'JetBrainsMono Nerd Font Mono', 'MesloLGS NF', 'Hack Nerd Font Mono', 'FiraCode Nerd Font Mono'`;
}

/**
 * Keystrokes leave as one write at a time. Each key is its own RPC, so fast typing
 * or key repeat would otherwise exceed the connection's in-flight request cap and
 * drop characters; coalescing what arrives while a write is outstanding keeps
 * order and bounds the outstanding requests to one per terminal.
 */
export function createWriteQueue(send: (bytes: Uint8Array) => Promise<void>, onError: (error: unknown) => void = () => {}) {
  const encoder = new TextEncoder();
  let pending: Uint8Array[] = [];
  let inFlight = false;
  let stopped = false;
  const flush = async () => {
    if (inFlight) return;
    inFlight = true;
    try {
      while (!stopped && pending.length) {
        let chunk: Uint8Array;
        if (pending[0]!.byteLength > MAX_TERMINAL_WRITE_BYTES) {
          // A paste larger than one write goes out in order, head first.
          chunk = pending[0]!.slice(0, MAX_TERMINAL_WRITE_BYTES);
          pending[0] = pending[0]!.slice(MAX_TERMINAL_WRITE_BYTES);
        } else {
          let size = 0;
          let count = 0;
          while (count < pending.length && size + pending[count]!.byteLength <= MAX_TERMINAL_WRITE_BYTES) size += pending[count++]!.byteLength;
          chunk = new Uint8Array(size);
          let offset = 0;
          for (const part of pending.splice(0, count)) { chunk.set(part, offset); offset += part.byteLength; }
        }
        await send(chunk);
      }
    } catch (error) {
      // A failed write means the connection is gone; stale keystrokes are not replayed.
      pending = []; stopped = true;
      onError(error);
    } finally { inFlight = false; }
  };
  return {
    push(data: string | Uint8Array) {
      const bytes = typeof data === 'string' ? encoder.encode(data) : data;
      if (stopped || !bytes.byteLength) return;
      if (pending.reduce((total, item) => total + item.byteLength, 0) + bytes.length > MAX_PENDING_INPUT_BYTES) {
        stopped = true; pending = []; onError(new Error('Terminal input queue is full. Pending input was discarded.')); return;
      }
      pending.push(bytes.slice());
      void flush();
    },
    dispose() { stopped = true; pending = []; },
    get pendingBytes() { return pending.reduce((total, part) => total + part.byteLength, 0); },
  };
}

/**
 * Wheel input for a program that turned on mouse tracking: SGR mouse reports
 * (button 64 up, 65 down) at the cell under the pointer, one per line of travel.
 * Pixel deltas from trackpads accumulate across events so a slow drag still
 * scrolls; the remainder carries to the next call and resets on direction change.
 */
export interface WheelGeometry { left: number; top: number; width: number; height: number; cols: number; rows: number }
export function wheelReports(event: Pick<WheelEvent, 'deltaY' | 'deltaMode' | 'clientX' | 'clientY' | 'shiftKey' | 'altKey' | 'ctrlKey'>, geometry: WheelGeometry, remainder = 0): { sequences: string[]; remainder: number } {
  const lineHeight = geometry.height / Math.max(1, geometry.rows);
  const lines = event.deltaMode === 1 ? event.deltaY : event.deltaMode === 2 ? event.deltaY * geometry.rows : event.deltaY / Math.max(1, lineHeight);
  const total = (Math.sign(lines) === Math.sign(remainder) || remainder === 0 ? remainder : 0) + lines;
  const count = Math.min(20, Math.trunc(Math.abs(total)));
  const col = Math.min(geometry.cols, Math.max(1, Math.floor((event.clientX - geometry.left) / (geometry.width / Math.max(1, geometry.cols))) + 1));
  const row = Math.min(geometry.rows, Math.max(1, Math.floor((event.clientY - geometry.top) / lineHeight) + 1));
  const button = (total < 0 ? 64 : 65) + (event.shiftKey ? 4 : 0) + (event.altKey ? 8 : 0) + (event.ctrlKey ? 16 : 0);
  return { sequences: Array.from({ length: count }, () => `\x1b[<${button};${col};${row}M`), remainder: total - Math.sign(total) * count };
}

/**
 * Cells covered by a drag, as ghostty-web's select() takes them: the first cell
 * in stream order and a length that wraps across rows.
 */
export interface DragGeometry { left: number; top: number; width: number; height: number; cols: number; rows: number }
export function dragSelection(from: { x: number; y: number }, to: { x: number; y: number }, geometry: DragGeometry): { column: number; row: number; length: number } {
  const cell = (point: { x: number; y: number }) => ({
    column: Math.max(0, Math.min(geometry.cols - 1, Math.floor((point.x - geometry.left) / (geometry.width / Math.max(1, geometry.cols))))),
    row: Math.max(0, Math.min(geometry.rows - 1, Math.floor((point.y - geometry.top) / (geometry.height / Math.max(1, geometry.rows))))),
  });
  let [start, end] = [cell(from), cell(to)];
  if (start.row > end.row || (start.row === end.row && start.column > end.column)) [start, end] = [end, start];
  return { ...start, length: (end.row - start.row) * geometry.cols + end.column - start.column + 1 };
}

/** App shortcuts the shell must not swallow; everything else reaches the PTY. */
export function passesToApp(event: KeyboardEvent, shortcuts: readonly string[]): boolean {
  return shortcuts.some(shortcut => matchesKeyboardEvent(event, shortcut as Hotkey));
}

/**
 * One shell tab: ghostty-web draws the daemon-owned PTY. The view owns only its
 * cursor and presentation; the shell lives on the host and survives unmounts,
 * reloads and reconnects. Restart replaces the shell behind the same tab.
 */
type TerminalViewProps = { tab: TerminalTab; client: Client; focused: boolean; connected: boolean };
export function TerminalView(props: TerminalViewProps) {
  const tabs = useSessionTabs();
  const current = tabs.workspace.tabs.find(tab => tab.id === props.tab.id);
  const tab = current?.kind === 'terminal' ? current : props.tab;
  if (tab.opening || tab.terminalId === null) return <TerminalOpenReview {...props} tab={tab} />;
  return <AttachedTerminalView {...props} tab={{ ...tab, terminalId: tab.terminalId }} />;
}

/** The host allocates shell IDs, so a lost open reply cannot be matched from cwd
 * or timestamps. Even a singleton candidate requires an explicit selection. */
function TerminalOpenReview({ tab, client, connected }: TerminalViewProps) {
  const runtime = useRuntime(), navigate = useNavigate();
  const [items, setItems] = useState<TerminalInfo[]>();
  const [error, setError] = useState<unknown>();
  const [busy, setBusy] = useState(false);
  const busyRef = useRef(false);
  const lifetime = useRef<AbortController | null>(null);
  const openingID = tab.opening?.id;
  const sending = useSyncExternalStore(listener => subscribeTerminalOpens(runtime, listener), () => terminalOpenInFlight(runtime, tab.id));
  const current = () => connected && runtime.connections.isAttached(client) && client.runtimeID === tab.runtimeId
    && runtime.tabs.pendingTerminal(tab.runtimeId)?.opening?.id === openingID;
  const refresh = async (signal: AbortSignal) => {
    if (!current()) throw new Error('Reconnect this host before inspecting existing shells.');
    const result = await inspectTerminals(client, signal);
    if (!current()) throw new Error('This terminal review belongs to an older connection or open attempt.');
    setItems(result); return result;
  };
  useEffect(() => {
    const controller = new AbortController(); lifetime.current = controller; setItems(undefined); setError(undefined);
    if (connected && !sending) void refresh(controller.signal).catch(failure => { if (!controller.signal.aborted) setError(failure); });
    return () => { controller.abort(); if (lifetime.current === controller) lifetime.current = null; };
  }, [client, connected, openingID, sending]);
  const navigateCurrent = async () => {
    const updated = runtime.tabs.workspace().tabs.find(item => item.id === tab.id);
    if (updated && selectedSessionTab(runtime.tabs.workspace())?.id === tab.id) await navigate({ ...tabDestination(updated), replace: true });
  };
  const act = async (choose?: string, create = false) => {
    if (busyRef.current || terminalOpenInFlight(runtime, tab.id)) return;
    const controller = lifetime.current;
    let attempt = openingID;
    if (!controller || controller.signal.aborted || !current()) return;
    busyRef.current = true; setBusy(true); setError(undefined);
    try {
      const reviewed = items;
      const listed = await refresh(controller.signal);
      if (choose) {
        const selected = listed.find(item => item.id === choose);
        if (!selected || selected.closing) throw new Error('That shell is no longer available. Review the updated list.');
        if (!runtime.tabs.updateTerminal(tab.id, { terminalId: selected.id, processEpoch: selected.process_epoch, cwd: selected.cwd, opening: undefined }, openingID)) return;
        await navigateCurrent();
      } else if (create) {
        const identities = (values: TerminalInfo[]) => JSON.stringify(values.map(item => [item.id, item.created_at]).sort());
        if (!reviewed || identities(reviewed) !== identities(listed)) throw new Error('The host’s shells changed. Review this list before opening another.');
        const captured = runtime.tabs.beginTerminalOpen(tab.id, client.processEpoch);
        attempt = captured.opening?.id;
        await sendTerminalOpen(runtime, client, captured);
        await navigateCurrent();
      }
    } catch (failure) {
      if (lifetime.current && runtime.connections.isAttached(client) && runtime.tabs.pendingTerminal(tab.runtimeId)?.opening?.id === attempt) setError(failure);
      else runtime.reportWorkspace(failure);
    } finally { busyRef.current = false; if (lifetime.current) setBusy(false); }
  };
  return <section aria-label="Review terminal open" {...stylex.props(styles.review)}>
    <p>The shell may have opened, but its acknowledgement is unconfirmed. Inspect this host’s existing shells before creating another.</p>
    {tab.opening?.processEpoch !== client.processEpoch && <p>The previous host process has ended. The list below belongs to the current process.</p>}
    {!connected && <p>Reconnect this host to inspect existing shells.</p>}
    {sending && <p>Waiting for the host’s open reply…</p>}
    <ErrorNotice type="resource" owner={`terminal-open:${tab.id}`} error={error} />
    <Button disabled={!connected || busy || sending} onClick={() => void act()}>Check existing shells</Button>
    {items && <>
      {items.length === 0 && <p>No shells are currently retained by this host process.</p>}
      {items.map(item => <div key={item.id}>
        <span>{item.cwd} · {item.shell} · {item.exited ? 'Exited' : item.closing ? 'Closing' : 'Running'} · {item.id}</span>{' '}
        <Button disabled={!connected || busy || sending || item.closing} onClick={() => void act(item.id)}>{item.id === tab.terminalId && item.process_epoch === tab.processEpoch ? 'Keep existing shell' : 'Use this shell'}</Button>
      </div>)}
      <Button disabled={!connected || busy || sending} onClick={() => void act(undefined, true)}>Open another shell</Button>
    </>}
  </section>;
}

function AttachedTerminalView({ tab, client, focused, connected }: Omit<TerminalViewProps, 'tab'> & { tab: TerminalTab & { terminalId: string } }) {
  const runtime = useRuntime();
  const { preferences } = useAppState();
  const { resolvedTheme } = useTheme();
  const navigate = useNavigate();
  const container = useRef<HTMLDivElement>(null);
  const term = useRef<Terminal>(null);
  /** Absolute cursor of the next expected byte; -1 until the first chunk arrives. */
  const cursor = useRef('0');
  const [status, setStatus] = useState<TerminalStatus>({ kind: 'loading' });
  /** The renderer exists; output arriving earlier waits in pending. */
  const [ready, setReady] = useState(false);
  const [hasSelection, setHasSelection] = useState(false);
  const [attachEpoch, setAttachEpoch] = useState(0);
  const shortcuts = [preferences.commandShortcut, preferences.composerShortcut, preferences.terminalShortcut];
  const shortcutsRef = useRef(shortcuts);
  shortcutsRef.current = shortcuts;
  const { terminalId, processEpoch } = tab;
  const writable = useRef(false); writable.current = connected && status.kind === 'live' && processEpoch === client.processEpoch;
  const observation = useRef<AbortController | null>(null);
  const inputQueue = useRef<ReturnType<typeof createWriteQueue> | null>(null);
  useEffect(() => { if (!connected) inputQueue.current?.dispose(); }, [connected]);

  // Mount the renderer once per shell identity; output arriving earlier is queued.
  useEffect(() => {
    const element = container.current;
    if (!element) return;
    // A fresh renderer has an empty screen: forget the cursor so the next attach
    // replays the whole ring instead of resuming mid-history onto a blank canvas.
    cursor.current = '0';
    let disposed = false;
    const allowCopy = (event: Event) => { if (terminal?.hasSelection()) event.preventDefault(); };
    const copySelection = (event: ClipboardEvent) => {
      if (!terminal?.hasSelection()) return;
      event.preventDefault();
      event.clipboardData?.setData('text/plain', terminal.getSelection());
    };
    let observer: ResizeObserver | undefined;
    let resizeTimer: ReturnType<typeof setTimeout> | undefined;
    let terminal: Terminal | undefined;
    let mouseListeners = () => {};
    const themeColors = resolvedTheme.colors;
    const fontFamily = terminalFontFamily(getComputedStyle(element).fontFamily);
    // ghostty-web sizes every cell from one measurement at open; a web font that
    // finishes loading afterwards would leave glyphs misaligned in Menlo-sized cells.
    const fonts = typeof document.fonts?.ready === 'object' ? Promise.race([document.fonts.ready, new Promise(resolve => setTimeout(resolve, 2000))]) : Promise.resolve();
    void Promise.all([loadGhostty(), fonts]).then(([ghostty]) => {
      if (disposed) return;
      terminal = new Terminal({
        ghostty, scrollback: 10_000, fontSize: 13, fontFamily,
        theme: { background: themeColors.background, foreground: themeColors.foreground, cursor: themeColors.primary, cursorAccent: themeColors.background, selectionBackground: themeColors.element, selectionForeground: themeColors.foreground },
      });
      const fit = new FitAddon();
      terminal.loadAddon(fit);
      terminal.open(element);
      // ghostty-web semantics, the opposite of xterm.js: returning true means
      // "handled here, do not send to the shell"; false lets the key through.
      terminal.attachCustomKeyEventHandler(event => {
        if (event.type !== 'keydown') return false;
        if (passesToApp(event, shortcutsRef.current)) return true;
        // Copy a selection with the platform copy chord; the shell gets Ctrl+C otherwise.
        const copyChord = (event.metaKey && !event.ctrlKey && event.code === 'KeyC') || (event.ctrlKey && event.shiftKey && event.code === 'KeyC');
        if (copyChord && terminal?.hasSelection()) {
          void runtime.platform.copy(terminal.getSelection()).catch(() => {});
          return true;
        }
        return false;
      });
      const ref = { id: terminalId, process_epoch: processEpoch ?? '' };
      const writes = createWriteQueue(async bytes => {
        if (!writable.current || !runtime.connections.isAttached(client)) throw new Error('Reconnect and review this shell before typing.');
        await client.writeTerminal(ref, bytes);
      }, error => { observation.current?.abort(); if (!disposed) setStatus({ kind: 'error', message: error instanceof Error ? error.message : String(error) }); });
      inputQueue.current = writes;
      terminal.onData(data => writes.push(data));
      // ghostty-web's capture-phase wheel handler stops propagation before its own SGR
      // reporter runs, then falls back to arrow keys in the alternate screen. A TUI that
      // asked for mouse tracking (whip, vim, less) wants wheel reports instead.
      let wheelRemainder = 0;
      terminal.attachCustomWheelEventHandler(event => {
        if (!terminal?.hasMouseTracking() || !terminal.getMode(1006)) return false;
        const rect = (terminal.element ?? element).getBoundingClientRect();
        const report = wheelReports(event, { left: rect.left, top: rect.top, width: rect.width, height: rect.height, cols: terminal.cols, rows: terminal.rows }, wheelRemainder);
        wheelRemainder = report.remainder;
        for (const sequence of report.sequences) writes.push(sequence);
        return true;
      });
      terminal.onTitleChange(title => runtime.tabs.updateTerminal(tab.id, { titleHint: title }));
      terminal.onSelectionChange(() => setHasSelection(terminal?.hasSelection() ?? false));
      // Shift+drag selects while a program owns the mouse (whip, vim). ghostty-web
      // reports every mouse event to such a program and clears the selection on each
      // report, so a plain drag never leaves anything to copy; real terminals let Shift
      // bypass reporting. Stopping the events here keeps both of its handlers out.
      let anchor: { x: number; y: number } | undefined;
      const shiftSelect = (event: MouseEvent) => {
        if (event.type === 'mousedown') {
          if (!(event.shiftKey && event.button === 0 && terminal?.hasMouseTracking())) return;
          anchor = { x: event.clientX, y: event.clientY };
          terminal.focus();
        } else if (!anchor) return;
        else if (event.type === 'mousemove' && !(event.buttons & 1)) { anchor = undefined; return; }
        event.stopPropagation();
        event.preventDefault();
        const canvas = terminal?.element?.querySelector('canvas');
        if (terminal && canvas && event.type === 'mousemove') {
          const rect = canvas.getBoundingClientRect();
          const { column, row, length } = dragSelection(anchor, event, { left: rect.left, top: rect.top, width: rect.width, height: rect.height, cols: terminal.cols, rows: terminal.rows });
          terminal.select(column, row, length);
        }
        if (event.type === 'mouseup') anchor = undefined;
      };
      for (const type of ['mousedown', 'mousemove', 'mouseup'] as const) element.addEventListener(type, shiftSelect, true);
      mouseListeners = () => { for (const type of ['mousedown', 'mousemove', 'mouseup'] as const) element.removeEventListener(type, shiftSelect, true); };
      // The selection lives on the canvas, so the native copy command (the Edit menu's
      // Cmd+C on macOS Electron, or a browser's default) finds no DOM selection. Answer
      // the clipboard events instead: beforecopy enables the command, copy supplies the text.
      element.addEventListener('beforecopy', allowCopy);
      element.addEventListener('copy', copySelection);
      terminal.onResize(({ cols, rows }) => {
        clearTimeout(resizeTimer);
        resizeTimer = setTimeout(() => { if (writable.current && runtime.connections.isAttached(client)) void client.resizeTerminal(ref, cols, rows).catch(error => { observation.current?.abort(); inputQueue.current?.dispose(); if (!disposed) setStatus({ kind: 'error', message: String(error) }); }); }, 100);
      });
      fit.fit();
      observer = new ResizeObserver(() => fit.fit());
      observer.observe(element);
      term.current = terminal;
      setReady(true);

    }).catch((error: unknown) => {
      if (!disposed) setStatus({ kind: 'error', message: error instanceof Error ? error.message : String(error) });
    });
    return () => {
      disposed = true;
      clearTimeout(resizeTimer);
      observer?.disconnect();
      element.removeEventListener('beforecopy', allowCopy);
      element.removeEventListener('copy', copySelection);
      mouseListeners();
      inputQueue.current?.dispose(); inputQueue.current = null;
      terminal?.dispose();
      term.current = null;
      setReady(false);
      setHasSelection(false);
    };
    // Theme changes remount through the strip's key; colors are read once here.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [terminalId, processEpoch, client, connected, attachEpoch, resolvedTheme.id]);

  // Replay immediately, then wait for output instead of delaying typed echo.
  // One bounded read stays in flight; observation never owns the shell.
  useEffect(() => {
    if (!processEpoch || processEpoch !== client.processEpoch) { setStatus({ kind: 'ended' }); return; }
    if (!connected) { setStatus({ kind: 'reconnecting' }); return; }
    if (!ready) return;
    const controller = new AbortController(); observation.current = controller;
    const observe = async () => {
      try {
        let waitMs: number | undefined;
        while (!controller.signal.aborted) {
          const { page, bytes, reset } = await readTerminalOutput(client, { id: terminalId, process_epoch: processEpoch }, cursor.current, controller.signal, waitMs);
          if (controller.signal.aborted) return;
          if (reset) term.current?.reset();
          if (bytes.length) term.current?.write(bytes);
          cursor.current = page.next;
          const ended = page.terminal.exited && page.next === page.end;
          setStatus(ended ? { kind: 'exited', exitCode: page.terminal.exit_code, signal: page.terminal.signal } : { kind: 'live' });
          if (ended) return;
          waitMs = page.next === page.end ? 5000 : undefined;
        }
      } catch (error) {
        if (controller.signal.aborted) return;
        inputQueue.current?.dispose();
        if (error instanceof RemoteError && error.kind === 'NOT_FOUND') setStatus({ kind: 'ended' });
        else setStatus({ kind: 'error', message: error instanceof Error ? error.message : String(error) });
      }
    };
    void observe();
    return () => { controller.abort(); if (observation.current === controller) observation.current = null; };
  }, [client, terminalId, processEpoch, connected, attachEpoch, ready]);

  useEffect(() => {
    if (ready && focused && status.kind === 'live' && !document.querySelector('[role="dialog"], [role="alertdialog"]')) term.current?.focus();
  }, [ready, focused, status.kind]);

  const restart = async () => {
    try {
      const size = term.current ? { cols: term.current.cols, rows: term.current.rows } : { cols: 80, rows: 24 };
      if (!connected || !runtime.connections.isAttached(client)) throw new Error('Connect this host before restarting the shell.');
      const captured = runtime.tabs.beginTerminalOpen(tab.id, client.processEpoch);
      await sendTerminalOpen(runtime, client, captured, size);
      const updated = runtime.tabs.workspace().tabs.find(item => item.id === tab.id);
      if (updated && selectedSessionTab(runtime.tabs.workspace())?.id === tab.id) await navigate({ ...tabDestination(updated), replace: true });
    } catch (error) { runtime.reportWorkspace(error); }
  };
  const notice = status.kind === 'exited' ? `Shell exited${status.signal ? ` (${status.signal})` : status.exitCode ? ` (code ${status.exitCode})` : ''}.`
    : status.kind === 'ended' ? 'This shell has ended.'
    : status.kind === 'reconnecting' ? 'Reconnecting to the host…'
    : status.kind === 'loading' ? 'Starting terminal…' : undefined;
  const copySelected = () => { const text = term.current?.getSelection(); if (text) void runtime.platform.copy(text).catch(error => runtime.reportWorkspace(error)); };
  return <div {...stylex.props(styles.frame)} data-terminal-view={terminalId} data-terminal-status={status.kind} data-terminal-selection={hasSelection ? 'true' : 'false'}>
    <ContextMenu items={[{ id: 'copy', label: 'Copy', disabled: !hasSelection, onSelect: copySelected }]}>
      <div ref={container} {...stylex.props(styles.surface)} />
    </ContextMenu>
    {status.kind === 'error' && <div {...stylex.props(styles.bar)}><ErrorNotice type="resource" owner={`terminal:${terminalId}`} title="Terminal is unavailable" error={status.message} action={<Button variant="ghost" onClick={() => setAttachEpoch(value => value + 1)}>Try again</Button>} /></div>}
    {notice && <div role="status" {...stylex.props(styles.bar)}>
      <span {...stylex.props(layout.grow)}>{notice}</span>
      {(status.kind === 'exited' || status.kind === 'ended') && <Button variant="secondary" disabled={!connected} onClick={() => void restart()}>Restart</Button>}
    </div>}
  </div>;
}

const styles = stylex.create({
  review: { display: 'flex', flexDirection: 'column', gap: 12, padding: 16, overflowY: 'auto' },
  frame: { display: 'flex', flexDirection: 'column', flex: 1, minWidth: 0, minHeight: 0, backgroundColor: colors.background },
  // ghostty-web focuses a contenteditable container for keyboard input; the browser
  // would draw its own caret there beside the shell's cursor. caret-color inherits.
  // Room above and beside the first row, as OpenCode leaves; the fit addon measures
  // ghostty's element, which fills this content box, so the grid excludes the padding.
  surface: { flex: 1, minHeight: 0, minWidth: 0, padding: '14px 12px 6px', overflow: 'hidden', fontFamily: typography.mono, caretColor: 'transparent' },
  bar: { display: 'flex', alignItems: 'center', gap: 8, padding: '6px 12px', borderTopWidth: 1, borderTopStyle: 'solid', borderTopColor: surface.quietBorder, backgroundColor: surface.navigation, color: colors.foreground, fontSize: typography.size12, flexShrink: 0 },
});
