/** A device-wide ceiling shared by session and attention indexes. */
export class ReadLane {
  private active = 0;
  private queue: (() => void)[] = [];
  async run<T>(signal: AbortSignal, read: () => Promise<T>): Promise<T> {
    if (signal.aborted) throw new Error('Read cancelled');
    await new Promise<void>((resolve, reject) => {
      const start = () => { signal.removeEventListener('abort', abort); this.active++; resolve(); };
      const abort = () => { this.queue = this.queue.filter(entry => entry !== start); reject(new Error('Read cancelled')); };
      if (this.active < 2) start();
      else { this.queue.push(start); signal.addEventListener('abort', abort, { once: true }); }
    });
    try { if (signal.aborted) throw new Error('Read cancelled'); return await read(); }
    finally { this.active--; this.queue.shift()?.(); }
  }
}
