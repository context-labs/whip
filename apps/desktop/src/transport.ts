import { unixSocket } from '@whip/sdk/node';
import type { Transport } from '@whip/sdk';
import type { DesktopEvent } from '@whip/app/desktop-bridge';

const frameLimit = 1 << 20;
const queueLimit = 8 << 20;
export function validHandle(value: unknown): asserts value is string {
  if (typeof value !== 'string' || !/^[a-zA-Z0-9-]{1,128}$/.test(value)) throw new Error('Invalid desktop handle');
}

/** Bounded raw transport only; product SDK state remains in the renderer. */
export class DesktopTransports {
  private entries = new Map<string, {
    connectionId: string; controller: AbortController; transport?: Transport; sent: number; received: number;
    outstanding: Map<number, number>; bytes: number; timer?: ReturnType<typeof setInterval>;
  }>();
  constructor(private emit: (event: DesktopEvent) => void) {}

  async open(id: string, connectionId: string, socket: string, signal: AbortSignal) {
    validHandle(id); validHandle(connectionId);
    if (this.entries.has(id) || this.entries.size >= 32) throw new Error('Desktop transport limit reached');
    const entry = { connectionId, controller: new AbortController(), sent: 0, received: 0,
      outstanding: new Map<number, number>(), bytes: 0 } as NonNullable<ReturnType<typeof this.entries.get>>;
    this.entries.set(id, entry);
    try {
      entry.transport = await unixSocket(socket, frameLimit)({
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
      // The existing SDK transport exposes bufferedAmount, not a drain callback.
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
    const entry = this.entries.get(id);
    if (!entry) return;
    this.entries.delete(id);
    clearInterval(entry.timer); entry.controller.abort(); entry.transport?.close(); entry.outstanding.clear();
    this.emit({ kind: 'closed', id, error: error.slice(0, 2048) });
  }
  release(connectionId: string) {
    for (const [id, entry] of this.entries) if (entry.connectionId === connectionId) this.close(id);
  }
  dispose() { for (const id of this.entries.keys()) this.close(id); }
}
