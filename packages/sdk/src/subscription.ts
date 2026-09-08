import { assertValid } from '@whip/protocol';
import type { CallOptions, SdkEvent, WhipClient } from './client.js';
import { asError, WhipError, abortError } from './errors.js';
import { byteLength, withSignal } from './util.js';

export interface SubscriptionOptions extends Pick<CallOptions, 'signal'> { maxMessages?: number; maxBytes?: number }

/** One ordered, bounded consumer. Fan out UI components through a SessionView. */
export class Subscription implements AsyncIterableIterator<SdkEvent> {
  readonly id: string;
  private queue: { event: SdkEvent; bytes: number }[] = [];
  private bytes = 0;
  private waiter?: { resolve(value: IteratorResult<SdkEvent>): void; reject(error: Error): void };
  private error?: Error;
  private closed = false;
  private lastReceived: bigint;
  private abortListener?: () => void;
  constructor(private readonly client: WhipClient, readonly rootId: string, public cursor: string, private readonly options: SubscriptionOptions) {
    this.id = client.createId();
    assertValid('SubscribeParams', { root_id: rootId, subscription_id: this.id, cursor });
    this.lastReceived = BigInt(cursor);
    if (cursor.startsWith('-')) throw new TypeError('Subscription cursor cannot be negative');
    for (const value of [options.maxMessages, options.maxBytes]) if (value !== undefined && (!Number.isSafeInteger(value) || value < 1)) throw new TypeError('Subscription limits must be positive integers');
  }
  async start(signal?: AbortSignal): Promise<void> {
    signal?.throwIfAborted();
    // Don't cancel admission itself: unsubscribe must happen after registration.
    const admission = this.client.call('events.subscribe', { root_id: this.rootId, subscription_id: this.id, cursor: this.cursor });
    let result;
    try { result = await withSignal(admission, signal); }
    catch (error) {
      void admission.then(() => this.unsubscribe(), failure => {
        if (failure instanceof WhipError && ['timeout', 'invalid_response'].includes(failure.kind) && this.client.getSnapshot().state === 'connected') this.client.reconnect();
      }).catch(() => {});
      throw error;
    }
    if (result.subscription_id !== this.id) throw new WhipError('invalid_response', 'Subscription acknowledgement changed identity');
    if (signal) {
      this.abortListener = () => { this.fail(abortError(signal)); };
      signal.addEventListener('abort', this.abortListener, { once: true });
      if (signal.aborted) this.abortListener();
    }
    if (this.error) throw this.error;
  }
  push(event: SdkEvent): void {
    if (this.closed || event.subscription_id !== this.id) return;
    if (event.root_id !== this.rootId) { this.fail(new WhipError('invalid_response', 'Subscription root changed')); return; }
    const seq = BigInt(event.seq);
    if (seq <= this.lastReceived) return;
    if (seq !== this.lastReceived + 1n) { this.fail(new WhipError('resynchronization_required', 'Event sequence gap; replay or obtain a fresh snapshot')); return; }
    this.lastReceived = seq;
    if (this.waiter) {
      const waiter = this.waiter; this.waiter = undefined; this.cursor = event.seq;
      waiter.resolve({ value: event, done: false }); return;
    }
    const bytes = byteLength(JSON.stringify(event));
    if (this.queue.length >= (this.options.maxMessages ?? 1024) || this.bytes + bytes > (this.options.maxBytes ?? 8 << 20)) {
      this.fail(new WhipError('resynchronization_required', 'Event consumer exceeded its buffer; obtain a fresh snapshot')); return;
    }
    this.queue.push({ event, bytes }); this.bytes += bytes;
  }
  next(): Promise<IteratorResult<SdkEvent>> {
    if (this.error) return Promise.reject(this.error);
    const next = this.queue.shift();
    if (next) { this.bytes -= next.bytes; this.cursor = next.event.seq; return Promise.resolve({ done: false, value: next.event }); }
    if (this.closed) return Promise.resolve({ done: true, value: undefined });
    if (this.waiter) return Promise.reject(new WhipError('invalid_arguments', 'Subscription supports one iterator consumer'));
    return new Promise((resolve, reject) => { this.waiter = { resolve, reject }; });
  }
  [Symbol.asyncIterator](): AsyncIterableIterator<SdkEvent> { return this; }
  async return(): Promise<IteratorResult<SdkEvent>> { await this.dispose(); return { done: true, value: undefined }; }
  fail(error: Error): void {
    if (this.closed) return;
    this.error = error;
    this.finish();
    // The connection may still be healthy after an individual stream expires.
    void this.unsubscribe().catch(() => {});
  }
  private finish(): void {
    this.closed = true;
    this.queue = []; this.bytes = 0;
    this.client.releaseSubscription(this.id);
    if (this.abortListener) this.options.signal?.removeEventListener('abort', this.abortListener);
    if (this.error) this.waiter?.reject(this.error);
    else this.waiter?.resolve({ done: true, value: undefined });
    this.waiter = undefined;
  }
  private async unsubscribe(): Promise<void> {
    if (this.client.getSnapshot().state === 'connected') await this.client.call('events.unsubscribe', { subscription_id: this.id });
  }
  async dispose(): Promise<void> {
    if (this.closed) return;
    this.finish();
    try { await this.unsubscribe(); }
    catch (error) { if (this.client.getSnapshot().state === 'connected') throw asError(error); }
  }
}
