import { useEffect, useRef, useState } from 'react';
import { useNavigate } from '@tanstack/react-router';
import { matchesKeyboardEvent, type Hotkey } from '@tanstack/react-hotkeys';
import * as stylex from '@stylexjs/stylex';
import { Button, ContextMenu } from '@whip/ui';
import { colors, surface, typography } from '@whip/ui/tokens.stylex';
import { useTheme } from '@whip/ui/themes';
import type { WhipClient } from '@whip/sdk';
import { MAX_TERMINAL_WRITE_BYTES, RpcError } from '@whip/sdk';
import { FitAddon, Ghostty, Terminal } from 'ghostty-web';
import wasmUrl from 'ghostty-web/ghostty-vt.wasm?url';
import { useAppState, useRuntime } from './context';
import { ErrorNotice } from './error-feedback';
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
  | { kind: 'detached' }
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
  const flush = async () => {
    if (inFlight) return;
    inFlight = true;
    try {
      while (pending.length) {
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
      pending = [];
      onError(error);
    } finally { inFlight = false; }
  };
  return {
    push(data: string | Uint8Array) {
      const bytes = typeof data === 'string' ? encoder.encode(data) : data;
      if (!bytes.byteLength) return;
      pending.push(bytes);
      void flush();
    },
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

/** App shortcuts the shell must not swallow; everything else reaches the PTY. */
export function passesToApp(event: KeyboardEvent, shortcuts: readonly string[]): boolean {
  return shortcuts.some(shortcut => matchesKeyboardEvent(event, shortcut as Hotkey));
}

/**
 * One shell tab: ghostty-web draws the daemon-owned PTY. The view owns only its
 * cursor and presentation; the shell lives on the host and survives unmounts,
 * reloads and reconnects. Restart replaces the shell behind the same tab.
 */
export function TerminalView({ tab, client, focused }: { tab: TerminalTab; client: WhipClient; focused: boolean }) {
  const runtime = useRuntime();
  const { preferences } = useAppState();
  const { resolvedTheme } = useTheme();
  const navigate = useNavigate();
  const container = useRef<HTMLDivElement>(null);
  const term = useRef<Terminal>(null);
  /** Absolute cursor of the next expected byte; -1 until the first chunk arrives. */
  const cursor = useRef(-1);
  const pending = useRef<Uint8Array[]>([]);
  const [status, setStatus] = useState<TerminalStatus>({ kind: 'loading' });
  /** The renderer exists; output arriving earlier waits in pending. */
  const [ready, setReady] = useState(false);
  const [hasSelection, setHasSelection] = useState(false);
  const [attachEpoch, setAttachEpoch] = useState(0);
  const shortcuts = [preferences.commandShortcut, preferences.composerShortcut, preferences.terminalShortcut];
  const shortcutsRef = useRef(shortcuts);
  shortcutsRef.current = shortcuts;
  const { terminalId } = tab;

  // Mount the renderer once per shell identity; output arriving earlier is queued.
  useEffect(() => {
    const element = container.current;
    if (!element) return;
    // A fresh renderer has an empty screen: forget the cursor so the next attach
    // replays the whole ring instead of resuming mid-history onto a blank canvas.
    cursor.current = -1;
    pending.current = [];
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
      const writes = createWriteQueue(bytes => client.terminals.write(terminalId, bytes));
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
      // The selection lives on the canvas, so the native copy command (the Edit menu's
      // Cmd+C on macOS Electron, or a browser's default) finds no DOM selection. Answer
      // the clipboard events instead: beforecopy enables the command, copy supplies the text.
      element.addEventListener('beforecopy', allowCopy);
      element.addEventListener('copy', copySelection);
      terminal.onResize(({ cols, rows }) => {
        clearTimeout(resizeTimer);
        resizeTimer = setTimeout(() => { void client.terminals.resize(terminalId, cols, rows).catch(() => {}); }, 100);
      });
      fit.fit();
      observer = new ResizeObserver(() => fit.fit());
      observer.observe(element);
      term.current = terminal;
      for (const chunk of pending.current) terminal.write(chunk);
      pending.current = [];
      setReady(true);
      setStatus(current => current.kind === 'loading' ? { kind: 'live' } : current);
    }).catch((error: unknown) => {
      if (!disposed) setStatus({ kind: 'error', message: error instanceof Error ? error.message : String(error) });
    });
    return () => {
      disposed = true;
      clearTimeout(resizeTimer);
      observer?.disconnect();
      element.removeEventListener('beforecopy', allowCopy);
      element.removeEventListener('copy', copySelection);
      terminal?.dispose();
      term.current = null;
      setReady(false);
      setHasSelection(false);
    };
    // Theme changes remount through the strip's key; colors are read once here.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [terminalId, client, resolvedTheme.id]);

  // Attach on mount and after every reconnect; deliver output in cursor order.
  useEffect(() => {
    let cancelled = false;
    const write = (bytes: Uint8Array) => {
      if (term.current) term.current.write(bytes); else pending.current.push(bytes);
    };
    const offOutput = client.terminals.onOutput(output => {
      if (output.id !== terminalId) return;
      if (cursor.current >= 0 && output.cursor !== cursor.current) {
        // A gap means the daemon replayed from an older or newer point than we
        // saw; redraw from what it sent rather than interleave two histories.
        term.current?.reset();
        pending.current = [];
      }
      cursor.current = output.cursor + output.bytes.byteLength;
      write(output.bytes);
      // Output only clears a reconnect notice; 'loading' ends when the renderer mounts.
      setStatus(current => current.kind === 'reconnecting' ? { kind: 'live' } : current);
    });
    const offExited = client.terminals.onExited(exit => {
      if (exit.id === terminalId) setStatus({ kind: 'exited', exitCode: exit.exitCode, ...(exit.signal ? { signal: exit.signal } : {}) });
    });
    const offDetached = client.terminals.onDetached(id => { if (id === terminalId) setStatus({ kind: 'detached' }); });
    const attach = async () => {
      try {
        const attachment = await client.terminals.attach(terminalId, cursor.current < 0 ? 0 : cursor.current);
        if (cancelled) return;
        if (attachment.exited) setStatus({ kind: 'exited', exitCode: attachment.exitCode ?? -1, ...(attachment.signal ? { signal: attachment.signal } : {}) });
        else setStatus(current => current.kind === 'detached' || current.kind === 'reconnecting' || current.kind === 'error' ? { kind: 'live' } : current);
      } catch (error) {
        if (cancelled) return;
        if (error instanceof RpcError && error.code === -32003) setStatus({ kind: 'ended' });
        else setStatus({ kind: 'error', message: error instanceof Error ? error.message : String(error) });
      }
    };
    let wasConnected = client.getSnapshot().state === 'connected';
    if (wasConnected) void attach(); else setStatus({ kind: 'reconnecting' });
    const offConnection = client.subscribe(() => {
      const connected = client.getSnapshot().state === 'connected';
      if (connected && !wasConnected) void attach();
      else if (!connected && wasConnected) setStatus(current => current.kind === 'live' || current.kind === 'loading' ? { kind: 'reconnecting' } : current);
      wasConnected = connected;
    });
    return () => { cancelled = true; offOutput(); offExited(); offDetached(); offConnection(); };
    // ready flips whenever the renderer is recreated (theme or client change), so
    // the new canvas attaches again and receives a full redraw.
  }, [client, terminalId, attachEpoch, ready]);

  useEffect(() => {
    if (ready && focused && status.kind === 'live' && !document.querySelector('[role="dialog"], [role="alertdialog"]')) term.current?.focus();
  }, [ready, focused, status.kind]);

  const restart = async () => {
    try {
      const size = term.current ? { cols: term.current.cols, rows: term.current.rows } : { cols: 80, rows: 24 };
      const opened = await client.terminals.open({ cwd: tab.cwd, ...size });
      runtime.tabs.updateTerminal(tab.id, { terminalId: opened.id, cwd: opened.cwd });
      const updated = runtime.tabs.workspace().tabs.find(item => item.id === tab.id);
      if (updated) await navigate({ ...tabDestination(updated), replace: true });
    } catch (error) { runtime.reportWorkspace(error); }
  };
  const notice = status.kind === 'exited' ? `Shell exited${status.signal ? ` (${status.signal})` : status.exitCode ? ` (code ${status.exitCode})` : ''}.`
    : status.kind === 'ended' ? 'This shell has ended.'
    : status.kind === 'detached' ? 'Attached in another window.'
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
      {(status.kind === 'exited' || status.kind === 'ended') && <Button variant="secondary" onClick={() => void restart()}>Restart</Button>}
      {status.kind === 'detached' && <Button variant="secondary" onClick={() => setAttachEpoch(value => value + 1)}>Reattach here</Button>}
    </div>}
  </div>;
}

const styles = stylex.create({
  frame: { display: 'flex', flexDirection: 'column', flex: 1, minWidth: 0, minHeight: 0, backgroundColor: colors.background },
  // ghostty-web focuses a contenteditable container for keyboard input; the browser
  // would draw its own caret there beside the shell's cursor. caret-color inherits.
  // Room above and beside the first row, as OpenCode leaves; the fit addon measures
  // ghostty's element, which fills this content box, so the grid excludes the padding.
  surface: { flex: 1, minHeight: 0, minWidth: 0, padding: '14px 12px 6px', overflow: 'hidden', fontFamily: typography.mono, caretColor: 'transparent' },
  bar: { display: 'flex', alignItems: 'center', gap: 8, padding: '6px 12px', borderTopWidth: 1, borderTopStyle: 'solid', borderTopColor: surface.quietBorder, backgroundColor: surface.navigation, color: colors.foreground, fontSize: typography.size12, flexShrink: 0 },
});
