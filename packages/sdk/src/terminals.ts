import type { TerminalOpenResult } from '@whip/protocol';
import type { CallOptions, WhipClient } from './client.js';
import { WhipError } from './errors.js';
import { decodeBase64, encodeBase64 } from './util.js';

/** Keystroke batches match the daemon's per-write bound. */
export const MAX_TERMINAL_WRITE_BYTES = 16 << 10;

export interface TerminalAttachment {
  /** Absolute cursor of the first replayed byte; live output follows from there. */
  cursor: number;
  cwd: string;
  cols: number;
  rows: number;
  exited: boolean;
  exitCode?: number;
  signal?: string;
}
export interface TerminalOutput { id: string; cursor: number; bytes: Uint8Array<ArrayBuffer> }
export interface TerminalExit { id: string; exitCode: number; signal?: string }

const encoder = new TextEncoder();
const cursorOf = (value: string): number => {
  const cursor = Number(value);
  if (!Number.isSafeInteger(cursor)) throw new WhipError('invalid_response', 'Terminal cursor is not a safe integer');
  return cursor;
};

/**
 * Workspace terminals: one login shell per tab, running where the daemon runs.
 * Output reaches only the connection that attached last; the daemon replays
 * retained output from the cursor an attach names, so a reload or reconnect
 * resumes without loss. Closing ends the shell; disconnecting does not.
 */
export class Terminals {
  constructor(private readonly client: WhipClient) {}
  /** Start a login shell. Omitted cwd resolves to the named session's directory, then the daemon home. */
  open(params: { cwd?: string; rootId?: string; cols: number; rows: number }, options: CallOptions = {}): Promise<TerminalOpenResult> {
    return this.client.call('terminal.open', {
      ...(params.cwd ? { cwd: params.cwd } : {}), ...(params.rootId ? { root_id: params.rootId } : {}),
      cols: params.cols, rows: params.rows,
    }, options);
  }
  /** cursor -1 asks for live output only; a cursor older than the retained ring is clamped to its start. */
  async attach(id: string, cursor: number, options: CallOptions = {}): Promise<TerminalAttachment> {
    const result = await this.client.call('terminal.attach', { id, cursor: String(cursor) }, options);
    return {
      cursor: cursorOf(result.cursor), cwd: result.cwd, cols: result.cols, rows: result.rows, exited: result.exited,
      ...(result.exit_code === undefined ? {} : { exitCode: result.exit_code }),
      ...(result.signal ? { signal: result.signal } : {}),
    };
  }
  async write(id: string, data: Uint8Array | string, options: CallOptions = {}): Promise<void> {
    const bytes = typeof data === 'string' ? encoder.encode(data) : data;
    if (!bytes.byteLength || bytes.byteLength > MAX_TERMINAL_WRITE_BYTES) throw new WhipError('invalid_request', `Terminal writes must be 1..${MAX_TERMINAL_WRITE_BYTES} bytes`);
    await this.client.call('terminal.write', { id, bytes: encodeBase64(bytes) }, options);
  }
  async resize(id: string, cols: number, rows: number, options: CallOptions = {}): Promise<void> {
    await this.client.call('terminal.resize', { id, cols, rows }, options);
  }
  /** Ends the shell. Use it when the tab closes, never on unmount or disconnect. */
  async close(id: string, options: CallOptions = {}): Promise<void> {
    await this.client.call('terminal.close', { id }, options);
  }
  onOutput(listener: (output: TerminalOutput) => void): () => void {
    return this.client.onNotification('terminal.output', params => {
      listener({ id: params.id, cursor: cursorOf(params.cursor), bytes: decodeBase64(params.bytes ?? '') });
    });
  }
  onExited(listener: (exit: TerminalExit) => void): () => void {
    return this.client.onNotification('terminal.exited', params => {
      listener({ id: params.id, exitCode: params.exit_code, ...(params.signal ? { signal: params.signal } : {}) });
    });
  }
  onDetached(listener: (id: string) => void): () => void {
    return this.client.onNotification('terminal.detached', params => listener(params.id));
  }
}
