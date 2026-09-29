import type { FramedConnection } from '@whip/sdk';
import { nativeFrameBytes, unixFrames } from './unix-frames';
import type { DesktopEvent } from '@whip/app/desktop-bridge';

const frameLimit = nativeFrameBytes;
const queueLimit = 8 << 20;
export function validHandle(value: unknown): asserts value is string {
  if (typeof value !== 'string' || !/^[a-zA-Z0-9-]{1,128}$/.test(value)) throw new Error('Invalid desktop handle');
}

/** Bounded raw transport only; product SDK state remains in the renderer. */
export class DesktopTransports {
  private entries = new Map<string, {
    connectionId: string; controller: AbortController; transport?: FramedConnection; sent: number; received: number;
    outstanding: Map<number, number>; bytes: number; timer?: ReturnType<typeof setInterval>;
  }>();
  private waiting = new Map<string, { connectionId: string; start(): void; cancel(error: Error): void }>();
  private disposed = false;
  constructor(private emit: (event: DesktopEvent) => void) {}

  async open(id: string, connectionId: string, socket: string, signal: AbortSignal) {
    validHandle(id); validHandle(connectionId);
    signal.throwIfAborted();
    if (this.disposed) throw new Error('Desktop transports are closed');
    if (this.entries.has(id) || this.waiting.has(id)) throw new Error('Desktop transport handle is already in use');
    if (this.entries.size < 32) return this.openNow(id, connectionId, socket, signal);
    if (this.waiting.size >= 64) throw new Error('Desktop transport wait queue is full. Close a view or wait for current requests to finish.');
    // Native unary metadata reads can exceed the active socket bound briefly.
    // Admission alone waits here; no frame, operation or retry is retained.
    return new Promise<void>((resolve, reject) => {
      const cleanup = () => {
        this.waiting.delete(id); clearTimeout(timer); signal.removeEventListener('abort', abort);
      };
      const pending = {
        connectionId,
        start: () => { cleanup(); this.openNow(id, connectionId, socket, signal).then(resolve, reject); },
        cancel: (error: Error) => { cleanup(); reject(error); },
      };
      const abort = () => { if (this.waiting.get(id) === pending) this.close(id, 'Desktop transport wait cancelled'); };
      const timer = setTimeout(() => {
        if (this.waiting.get(id) === pending) this.close(id, 'Desktop transport wait timed out. Close a view and try again.');
      }, 15_000);
      timer.unref();
      this.waiting.set(id, pending);
      signal.addEventListener('abort', abort, { once: true });
      if (signal.aborted) abort();
    });
  }

  private async openNow(id: string, connectionId: string, socket: string, signal: AbortSignal) {
    signal.throwIfAborted();
    const entry = { connectionId, controller: new AbortController(), sent: 0, received: 0,
      outstanding: new Map<number, number>(), bytes: 0 } as NonNullable<ReturnType<typeof this.entries.get>>;
    this.entries.set(id, entry);
    try {
      entry.transport = await unixFrames(socket)({
        message: frame => {
          if (this.entries.get(id) !== entry) return;
          const bytes = Buffer.byteLength(frame);
          if (entry.bytes + bytes > queueLimit || entry.outstanding.size >= 1024) {
            this.close(id, 'Desktop receive queue exceeded its limit'); return;
          }
          entry.outstanding.set(++entry.received, bytes); entry.bytes += bytes;
          this.emit({ kind: 'frame', id, sequence: entry.received, frame });
        },
        close: error => { if (this.entries.get(id) === entry) this.close(id, error.message); },
      }, AbortSignal.any([signal, entry.controller.signal]));
      if (this.entries.get(id) !== entry) { entry.transport.close(); throw new Error('Connection was closed'); }
      // The native connector exposes bufferedAmount, not a drain callback.
      // Poll only while a write remains buffered, and release this timer at close.
    } catch (error) { if (this.entries.get(id) === entry) this.close(id); throw error; }
  }

  send(id: string, sequence: number, frame: string) {
    validHandle(id);
    const entry = this.entries.get(id);
    if (!entry?.transport) throw new Error('Desktop transport is closed');
    const bytes = typeof frame === 'string' ? Buffer.byteLength(frame) + 1 : Infinity;
    if (!Number.isSafeInteger(sequence) || sequence !== entry.sent + 1 || bytes > frameLimit ||
        frame.includes('\n') || entry.transport.bufferedAmount + bytes > queueLimit) {
      this.close(id, 'Invalid desktop transport frame or queue limit'); return;
    }
    try {
      entry.transport.send(frame); entry.sent = sequence;
      this.emit({ kind: 'sent', id, sequence, buffered: entry.transport.bufferedAmount });
      if (entry.transport.bufferedAmount && !entry.timer) {
        entry.timer = setInterval(() => {
          const bytes = entry.transport?.bufferedAmount ?? 0;
          this.emit({ kind: 'buffered', id, bytes });
          if (!bytes) { clearInterval(entry.timer); entry.timer = undefined; }
        }, 20);
        entry.timer.unref();
      }
    } catch (error) { this.close(id, error instanceof Error ? error.message : 'Transport send failed'); }
  }

  acknowledge(id: string, sequence: number) {
    validHandle(id);
    const entry = this.entries.get(id);
    if (!entry) return;
    const first = entry.outstanding.entries().next().value;
    if (!first || first[0] !== sequence) { this.close(id, 'Invalid desktop receive acknowledgement'); return; }
    entry.outstanding.delete(sequence); entry.bytes -= first[1];
  }
  close(id: string, error = 'Connection closed') {
    const pending = this.waiting.get(id);
    if (pending) {
      pending.cancel(new Error(error));
      this.emit({ kind: 'closed', id, error: error.slice(0, 2048) });
      return;
    }
    const entry = this.entries.get(id);
    if (!entry) return;
    this.entries.delete(id);
    clearInterval(entry.timer); entry.controller.abort(); entry.transport?.close(); entry.outstanding.clear();
    this.emit({ kind: 'closed', id, error: error.slice(0, 2048) });
    while (!this.disposed && this.entries.size < 32 && this.waiting.size) this.waiting.values().next().value!.start();
  }
  release(connectionId: string) {
    for (const [id, entry] of this.waiting) if (entry.connectionId === connectionId) this.close(id);
    for (const [id, entry] of this.entries) if (entry.connectionId === connectionId) this.close(id);
  }
  dispose() {
    this.disposed = true;
    for (const id of this.waiting.keys()) this.close(id);
    for (const id of this.entries.keys()) this.close(id);
  }
}
