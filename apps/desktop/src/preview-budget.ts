import type { Readable, Writable } from 'node:stream';

/** One instance belongs to a native window, shared by all of its preview proxies. */
export class PreviewBudget {
  private active = 0;
  private queued = 0;
  private waiters = new Set<() => void>();
  get usage() { return { streams: this.active, queuedBytes: this.queued }; }
  acquire() {
    if (this.active >= 32) throw new Error('Preview window stream limit reached');
    this.active++;
    let released = false;
    return () => { if (!released) { released = true; this.active--; } };
  }
  private async reserve(bytes: number, signal: AbortSignal) {
    signal.throwIfAborted();
    if (bytes > 64 * 1024) throw new Error('Preview chunk limit exceeded');
    while (this.queued + bytes > 4 * 1024 * 1024) {
      await new Promise<void>((resolve, reject) => {
        const wake = () => { this.waiters.delete(wake); signal.removeEventListener('abort', abort); resolve(); };
        const abort = () => { this.waiters.delete(wake); reject(signal.reason); };
        this.waiters.add(wake); signal.addEventListener('abort', abort, { once: true });
      });
      signal.throwIfAborted();
    }
    this.queued += bytes;
    return () => { this.queued -= bytes; for (const wake of [...this.waiters]) wake(); };
  }
  /** Backpressure both directions. Count each chunk until its destination write completes. */
  async pipe(source: Readable, destination: Writable, signal: AbortSignal, end = true) {
    for await (const input of source) {
      const bytes = Buffer.isBuffer(input) ? input : Buffer.from(input);
      for (let offset = 0; offset < bytes.length; offset += 64 * 1024) {
        const chunk = bytes.subarray(offset, offset + 64 * 1024);
        const release = await this.reserve(chunk.length, signal);
        try {
          await new Promise<void>((resolve, reject) => {
            const abort = () => { destination.destroy(); reject(signal.reason); };
            signal.addEventListener('abort', abort, { once: true });
            destination.write(chunk, error => { signal.removeEventListener('abort', abort); if (error) reject(error); else resolve(); });
          });
        } finally { release(); }
      }
    }
    if (end && !destination.destroyed) destination.end();
  }
}
