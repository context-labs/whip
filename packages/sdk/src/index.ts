import { assertValid } from '@whip/protocol';
import type { Admission, InitializeResult, Operations, RequestIdentity, SessionObservation } from '@whip/protocol';
import { decodeResponse, operation } from './wire.js';
import type { CallOptions, Method, Transport } from './wire.js';

export { DeliveryError, RemoteError } from './wire.js';
export type { CallOptions, Transport } from './wire.js';
export type * from '@whip/protocol';

/** No conversation or execution state lives here. Previews are disposable runtime projections. */
export class Client {
  private sequence = 0;
  private constructor(private readonly transport: Transport, private readonly initial: InitializeResult, readonly clientID: string) {}

  static async connect(transport: Transport, options: { clientID: string; expectedRuntimeID?: string } & CallOptions): Promise<Client> {
    assertValid('RequestIdentity', { client_id: options.clientID, request_id: 'validate' });
    const params = { major: 4, ...(options.expectedRuntimeID ? { expected_runtime_id: options.expectedRuntimeID } : {}) };
    assertValid('InitializeParams', params);
    const response = await transport({ jsonrpc: '2.0', id: 'initialize', method: 'initialize', params }, options.expectedRuntimeID, options);
    const initial = decodeResponse('initialize', 'initialize', response);
    if (options.expectedRuntimeID && initial.runtime_id !== options.expectedRuntimeID) throw new TypeError('Runtime identity mismatch');
    return new Client(transport, structuredClone(initial), options.clientID);
  }

  get runtimeID(): string { return this.initial.runtime_id; }
  get builtins(): NonNullable<InitializeResult['builtins']> { return structuredClone(this.initial.builtins ?? []); }

  async call<M extends Exclude<Method, 'initialize'>>(method: M, params: Operations[M]['params'], options: CallOptions = {}): Promise<Operations[M]['result']> {
    options.signal?.throwIfAborted();
    assertValid(operation(method).params, params);
    const id = 'rpc-' + ++this.sequence;
    const response = await this.transport({ jsonrpc: '2.0', id, method, params }, this.runtimeID, options);
    return decodeResponse(method, id, response);
  }

  /** Keep this requestID and exact payload until admission is known, including after a lost acknowledgement. */
  submit(sessionID: string, parts: Operations['sessions.submit']['params']['parts'], requestID: string, options: CallOptions = {}): Promise<Admission> {
    return this.call('sessions.submit', { session_id: sessionID, source: 'user', parts, identity: this.identity(requestID) }, options);
  }

  /** Child identity, initial input and delegated authority share one recoverable admission. */
  spawn(params: Omit<Operations['sessions.spawn']['params'], 'identity'>, requestID: string, options: CallOptions = {}): Promise<Operations['sessions.spawn']['result']> {
    return this.call('sessions.spawn', { ...params, identity: this.identity(requestID) }, options);
  }

  /** Keep a globally unique mailID and the same payload when retrying an uncertain send. */
  sendMail(params: Omit<Operations['mail.send']['params'], 'mail_id'>, mailID: string, options: CallOptions = {}): Promise<Operations['mail.send']['result']> {
    return this.call('mail.send', { ...params, mail_id: mailID }, options);
  }

  /** Retain versionID and the exact encoded JSON payload when a write acknowledgement is lost. */
  writeState(params: Omit<Operations['state.write']['params'], 'version_id'>, versionID: string, options: CallOptions = {}): Promise<Operations['state.write']['result']> {
    return this.call('state.write', { ...params, version_id: versionID }, options);
  }

  /** Appends strings or arrays against an explicit revision; conflicts never overwrite another writer. */
  appendState(params: Omit<Operations['state.append']['params'], 'version_id'>, versionID: string, options: CallOptions = {}): Promise<Operations['state.append']['result']> {
    return this.call('state.append', { ...params, version_id: versionID }, options);
  }

  /** Subscribes from an observed revision; creation atomically catches up with the current head. */
  subscribeState(params: Omit<Operations['state.subscribe']['params'], 'subscription_id'>, subscriptionID: string, options: CallOptions = {}): Promise<Operations['state.subscribe']['result']> {
    return this.call('state.subscribe', { ...params, subscription_id: subscriptionID }, options);
  }

  recover(requestID: string, options: CallOptions = {}): Promise<Admission> {
    return this.call('receipts.get', this.identity(requestID), options);
  }

  /** Aborting this wait affects only the observer. Use inputs.cancel/turns.cancel to cancel work. */
  async wait(requestID: string, options: CallOptions = {}): Promise<Admission> {
    for (;;) {
      const result = await this.recover(requestID, options);
      if (result.receipt.deleted_at || result.input?.state === 'cancelled' || result.turn?.finished_at) return result;
      await delay(25, options.signal);
    }
  }

  /**
   * Read committed pages and disposable previews. Replace a preview by message_id
   * when its committed message arrives; a null preview or changed epoch clears it.
   * This iterator retains only a cursor and preview revision, never a transcript.
   * Aborting observation does not cancel the session's work.
   */
  async *observe(sessionID: string, options: CallOptions & { after?: string } = {}): AsyncGenerator<SessionObservation> {
    let after = options.after ?? '0';
    let previous: string | undefined;
    for (;;) {
      const snapshot = await this.call('sessions.observe', { session_id: sessionID, after, limit: 100 }, { signal: options.signal });
      const messages = snapshot.messages ?? [];
      const preview = snapshot.preview;
      const revision = snapshot.epoch + ':' + (preview ? preview.attempt_id + ':' + preview.revision : 'none');
      for (const message of messages) {
        if (BigInt(message.sequence) <= BigInt(after)) throw new TypeError('Observation history cursor did not advance');
        after = message.sequence;
      }
      if (messages.length || previous !== revision) {
        previous = revision;
        yield snapshot;
      }
      if (messages.length) continue; // Drain bounded history pages before polling.
      await delay(100, options.signal);
    }
  }

  private identity(requestID: string): RequestIdentity { return { client_id: this.clientID, request_id: requestID }; }
}

function delay(milliseconds: number, signal?: AbortSignal): Promise<void> {
  signal?.throwIfAborted();
  return new Promise((resolve, reject) => {
    const cleanup = () => { clearTimeout(timer); signal?.removeEventListener('abort', aborted); };
    const aborted = () => { cleanup(); reject(signal?.reason); };
    const timer = setTimeout(() => { cleanup(); resolve(); }, milliseconds);
    signal?.addEventListener('abort', aborted, { once: true });
  });
}
