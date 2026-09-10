import {
  assertValid, manifest, rpcOperations, runtimeOperations, validate,
  type CommandOperation, type CommandResult, type EphemeralOperation,
  type InitializeResult, type QueryOperation, type QueryResult,
  type RootEvent, type RpcMethod, type RpcMethods, type RuntimeOperation, type RuntimeOperations,
} from '@whip/protocol';
import { CommandHandle, type CommandOptions, type RecoveryRecord, type RecoveryStorage, type CommandOutcome } from './command.js';
import { ContentReference, upload, type ContentScope, type UploadOptions } from './content.js';
import { Host, Permissions, Providers, Configuration } from './services.js';
import { Agents } from './agents.js';
import { Session, Sessions } from './session.js';
import { Subscription, type SubscriptionOptions } from './subscription.js';
import { WhipError, RpcError, abortError, asError } from './errors.js';
import { webSocket, type Transport, type TransportFactory } from './transport.js';
import { byteLength, frozen, notify, object, withSignal, uuid, digestHex } from './util.js';

export type SdkEvent = RootEvent;
export type ConnectionState = 'connecting' | 'connected' | 'reconnecting' | 'incompatible' | 'paused' | 'closed';
export interface ConnectionSnapshot {
  readonly state: ConnectionState;
  readonly info?: Readonly<InitializeResult>;
  readonly error?: Error;
}
export interface CallOptions { signal?: AbortSignal; timeoutMs?: number }
export interface ClientOptions {
  endpoint: string | TransportFactory;
  clientId: string;
  clientKind?: 'human' | 'automation';
  /** Refuse attachment to another installation before exposing connected state. */
  expectedRuntimeId?: string;
  buildId?: string;
  reconnect?: boolean;
  queryTimeoutMs?: number;
  connectTimeoutMs?: number;
  heartbeatIntervalMs?: number;
  heartbeatTimeoutMs?: number;
  commandPollMs?: number;
  recoveryStorage?: RecoveryStorage;
  /** Native runtimes can supply OS-backed primitives without global polyfills. */
  randomUUID?: () => string;
  sha256?: (bytes: Uint8Array<ArrayBuffer>) => Promise<Uint8Array<ArrayBuffer>>;
}
export type QueryOutcome<O extends RuntimeOperation> = Omit<QueryResult, 'result'> & { result?: RuntimeOperations[O]['result'] };
interface Pending {
  method: RpcMethod;
  resolve(value: unknown): void;
  reject(error: Error): void;
}

/** Owns one connection. It never starts, stops, or implicitly replaces a daemon. */
export class WhipClient {
  readonly clientId: string;
  readonly clientKind: 'human' | 'automation';
  readonly recoveryStorage?: RecoveryStorage;
  readonly sessions: Sessions;
  readonly providers: Providers;
  readonly configuration: Configuration;
  readonly permissions: Permissions;
  readonly host: Host;
  readonly agents: Agents;
  readonly events = {
    subscribe: async (rootId: string, cursor: string, options: SubscriptionOptions = {}): Promise<Subscription> => {
      this.requireConnected();
      const limit = this.snapshot.info!.limits.root_subscriptions;
      if (this.streams.size >= limit) throw new WhipError('resource_limit', 'Root subscription limit reached');
      const stream = new Subscription(this, rootId, cursor, options);
      // Register before sending: notifications may arrive before the acknowledgement.
      this.streams.set(stream.id, stream);
      try { await stream.start(options.signal); return stream; }
      catch (error) { stream.fail(asError(error)); throw error; }
    },
    replay: async (rootId: string, cursor: string, options: CallOptions & { limit?: number } = {}) => {
      const page = await this.call('events.replay', { root_id: rootId, cursor, limit: options.limit ?? 1000 }, options);
      return { ...page, events: (page.events ?? []).map(event => this.decodeEvent(event)) };
    },
  };
  private readonly options: ClientOptions;
  private readonly factory: TransportFactory;
  private snapshot: ConnectionSnapshot = Object.freeze({ state: 'closed' });
  private readonly listeners = new Set<() => void>();
  private readonly eventListeners = new Set<(event: SdkEvent) => void>();
  private readonly commandListeners = new Set<(outcome: CommandResult) => void>();
  private readonly pending = new Map<string, Pending>();
  private readonly streams = new Map<string, Subscription>();
  private readonly lookups = new Map<string, Promise<CommandResult>>();
  private readonly ticks = new Set<() => void>();
  private ticker?: ReturnType<typeof setTimeout>;
  private connection?: Transport;
  private controller?: AbortController;
  private opening?: Promise<void>;
  private heartbeat?: ReturnType<typeof setTimeout>;
  private retryTimer?: ReturnType<typeof setTimeout>;
  private retries = 0;
  private epoch = 0;
  private nextId = 0;
  private closed = false;
  private paused = false;
  private runtimeId?: string;

  constructor(options: ClientOptions) {
    if (!options.clientId.trim()) throw new TypeError('clientId is required; persist it to recover command identities');
    for (const value of [options.queryTimeoutMs, options.connectTimeoutMs, options.heartbeatIntervalMs, options.heartbeatTimeoutMs, options.commandPollMs]) {
      if (value !== undefined && (!Number.isSafeInteger(value) || value < 1)) throw new TypeError('Timeouts and polling intervals must be positive integer milliseconds');
    }
    this.options = { ...options };
    this.clientId = options.clientId;
    this.clientKind = options.clientKind ?? 'automation';
    this.recoveryStorage = options.recoveryStorage;
    this.factory = typeof options.endpoint === 'string' ? webSocket(options.endpoint) : options.endpoint;
    this.sessions = new Sessions(this);
    this.providers = new Providers(this);
    this.configuration = new Configuration(this);
    this.permissions = new Permissions(this);
    this.host = new Host(this);
    this.agents = new Agents(this);
  }
  getSnapshot = (): ConnectionSnapshot => this.snapshot;
  subscribe = (listener: () => void): (() => void) => { this.listeners.add(listener); return () => this.listeners.delete(listener); };
  onEvent(listener: (event: SdkEvent) => void): () => void { this.eventListeners.add(listener); return () => this.eventListeners.delete(listener); }
  onCommand(listener: (outcome: CommandResult) => void): () => void { this.commandListeners.add(listener); return () => this.commandListeners.delete(listener); }
  session(rootId: string): Session { return new Session(this, rootId); }
  get transportKind(): Transport['kind'] | undefined { return this.connection?.kind; }
  get httpEndpoint(): string | undefined { return this.connection?.httpEndpoint; }
  get lifetimeSignal(): AbortSignal { this.requireConnected(); return this.controller!.signal; }

  async connect(options: Pick<CallOptions, 'signal'> = {}): Promise<void> {
    options.signal?.throwIfAborted();
    if (this.closed) throw new WhipError('closed', 'Client is closed; create a new client to attach again');
    if (this.snapshot.state === 'incompatible') throw this.snapshot.error;
    if (this.paused) throw new WhipError('paused', 'Client observation is paused; resume it before connecting');
    if (this.snapshot.state === 'connected') return;
    if (!this.opening) {
      clearTimeout(this.retryTimer);
      this.retryTimer = undefined;
      this.opening = this.open().finally(() => { this.opening = undefined; });
    }
    return withSignal(this.opening, options.signal);
  }
  private async open(): Promise<void> {
    const epoch = ++this.epoch;
    const controller = new AbortController();
    this.controller = controller;
    this.setState(this.runtimeId ? 'reconnecting' : 'connecting');
    const timer = setTimeout(() => controller.abort(new WhipError('timeout', 'Daemon initialization timed out')), this.options.connectTimeoutMs ?? 5000);
    try {
      const connection = await this.factory({
        message: message => { if (epoch === this.epoch) this.receive(message); },
        close: error => { if (epoch === this.epoch) this.disconnected(error); },
      }, controller.signal);
      if (epoch !== this.epoch || this.closed || controller.signal.aborted) { connection.close(); throw abortError(controller.signal); }
      this.connection = connection;
      const info = await this.dispatch('initialize', {
        protocol_major: manifest.major, client_id: this.clientId, client_kind: this.clientKind,
        build_id: this.options.buildId ?? '@whip/sdk', capabilities: ['commands', 'events', 'snapshots', 'uploads', 'history_pages', 'collections', 'host_configuration', 'workspace_completion', 'host_views', 'themes', 'mailbox_inspection', 'input_attachments', 'session_summaries', 'execution_engines'],
      }, { signal: controller.signal }, true);
      if (epoch !== this.epoch || this.closed || controller.signal.aborted) throw abortError(controller.signal);
      if (info.protocol_major !== manifest.major) throw new WhipError('unsupported_protocol', 'Daemon protocol major is incompatible');
      const expectedRuntime = this.runtimeId ?? this.options.expectedRuntimeId;
      if (expectedRuntime && expectedRuntime !== info.runtime_id) throw new WhipError('runtime_changed', 'The endpoint now serves a different runtime; create a new client explicitly');
      if (!info.runtime_id || info.limits.frame_bytes < 1 || info.limits.in_flight_requests < 1 || BigInt(info.limits.outbound_bytes) < 1n || info.limits.root_subscriptions < 1 || info.limits.content_chunk_bytes < 1 || BigInt(info.limits.upload_bytes) < 0n) {
        throw new WhipError('invalid_response', 'Daemon reported invalid identity or limits');
      }
      this.runtimeId = info.runtime_id;
      this.retries = 0;
      this.setState('connected', undefined, frozen(info));
      this.scheduleHeartbeat();
    } catch (value) {
      const error = asError(value);
      if (epoch === this.epoch) this.disconnected(error);
      throw error;
    } finally { clearTimeout(timer); }
  }
  private setState(state: ConnectionState, error?: Error, info = this.snapshot.info): void {
    this.snapshot = Object.freeze({ state, ...(info ? { info } : {}), ...(error ? { error } : {}) });
    notify(this.listeners, undefined);
    this.wakeCommands();
  }
  private disconnected(error: Error): void {
    ++this.epoch;
    const connection = this.connection;
    this.connection = undefined;
    this.controller?.abort(error);
    connection?.close();
    clearTimeout(this.heartbeat);
    for (const pending of this.pending.values()) pending.reject(error);
    this.pending.clear();
    this.lookups.clear();
    for (const stream of [...this.streams.values()]) stream.fail(error);
    if (this.closed) return;
    if (this.paused) {
      clearTimeout(this.retryTimer);
      this.retryTimer = undefined;
      this.setState('paused');
      return;
    }
    const incompatible = error instanceof WhipError && ['unsupported_protocol', 'runtime_changed'].includes(error.kind);
    this.setState(incompatible ? 'incompatible' : this.options.reconnect === false ? 'closed' : 'reconnecting', error);
    if (!incompatible && this.options.reconnect !== false) {
      clearTimeout(this.retryTimer);
      const delay = Math.min(10_000, 250 * 2 ** Math.min(this.retries++, 6)) * (0.75 + Math.random() * 0.5);
      this.retryTimer = setTimeout(() => { void this.connect().catch(() => { /* open records state and schedules recovery */ }); }, delay);
    }
  }
  /** Refreshes a connection without replaying pending requests. */
  reconnect(): void {
    this.requireConnected();
    this.disconnected(new WhipError('disconnected', 'Refreshing daemon connection'));
  }
  /** Suspend observation without cancelling accepted work or discarding command identities. */
  pause(): void {
    if (this.closed || this.paused || this.snapshot.state === 'incompatible') return;
    this.paused = true;
    this.disconnected(new WhipError('paused', 'Client observation paused'));
  }
  /** Resume the same runtime; commands are reconciled, never resubmitted. */
  async resume(options: Pick<CallOptions, 'signal'> = {}): Promise<void> {
    options.signal?.throwIfAborted();
    this.paused = false;
    const resuming = (async () => {
      // A paused initialization may still be unwinding its aborted transport.
      await this.opening?.catch(() => {});
      await this.connect();
    })();
    return withSignal(resuming, options.signal);
  }
  createId(): string { return (this.options.randomUUID ?? uuid)(); }
  async digestHex(bytes: Uint8Array<ArrayBuffer>): Promise<string> {
    if (!this.options.sha256) return digestHex(bytes);
    const digest = await this.options.sha256(bytes);
    if (digest.byteLength !== 32) throw new WhipError('invalid_response', 'SHA-256 provider returned an invalid digest');
    return Array.from(digest, byte => byte.toString(16).padStart(2, '0')).join('');
  }
  close(): void {
    if (this.closed) return;
    this.closed = true;
    clearTimeout(this.retryTimer); clearTimeout(this.heartbeat); clearTimeout(this.ticker);
    this.disconnected(new WhipError('closed', 'Client closed'));
    this.setState('closed');
    this.listeners.clear(); this.eventListeners.clear(); this.commandListeners.clear();
  }
  requireConnected(): Readonly<InitializeResult> {
    if (this.snapshot.state !== 'connected' || !this.connection) throw this.snapshot.error ?? new WhipError('disconnected', 'Connect to the daemon before sending requests');
    return this.snapshot.info!;
  }
  async whenConnected(signal?: AbortSignal): Promise<Readonly<InitializeResult>> {
    signal?.throwIfAborted();
    if (this.snapshot.state === 'connected') return this.snapshot.info!;
    if (this.closed || this.snapshot.state === 'incompatible' || this.snapshot.state === 'closed') throw this.snapshot.error ?? new WhipError('closed', 'Client is not connected');
    return new Promise((resolve, reject) => {
      const cleanup = () => { unsubscribe(); signal?.removeEventListener('abort', abort); };
      const check = () => {
        if (this.snapshot.state === 'connected') { cleanup(); resolve(this.snapshot.info!); }
        else if (['closed', 'incompatible'].includes(this.snapshot.state)) { cleanup(); reject(this.snapshot.error ?? new WhipError('closed', 'Client closed')); }
      };
      const abort = () => { cleanup(); reject(abortError(signal)); };
      const unsubscribe = this.subscribe(check);
      signal?.addEventListener('abort', abort, { once: true });
      if (signal?.aborted) abort(); else check();
    });
  }
  supports(surface: 'rpc' | 'runtime', name: string): boolean {
    return this.snapshot.info?.operations?.some(operation => operation.surface === surface && operation.name === name) ?? false;
  }
  call<M extends RpcMethod>(method: M, params: RpcMethods[M]['params'], options: CallOptions = {}): Promise<RpcMethods[M]['result']> {
    return this.dispatch(method, params, options);
  }
  /** Internal exact JSON path for command retries. */
  callEncoded<M extends RpcMethod>(method: M, paramsJSON: string, options: CallOptions = {}): Promise<RpcMethods[M]['result']> {
    return this.dispatch(method, JSON.parse(paramsJSON), options, false, paramsJSON);
  }
  private async dispatch<M extends RpcMethod>(method: M, params: RpcMethods[M]['params'], options: CallOptions = {}, initializing = false, encoded?: string): Promise<RpcMethods[M]['result']> {
    options.signal?.throwIfAborted();
    if (!initializing) this.requireConnected();
    if (!Object.hasOwn(rpcOperations, method)) throw new WhipError('unsupported_operation', 'Unknown RPC method');
    if (!initializing && !this.supports('rpc', method)) throw new WhipError('unsupported_operation', `Daemon does not support ${method}`);
    const metadata = rpcOperations[method];
    assertValid(metadata.params_type, params);
    this.validateNested(method, params);
    const limit = this.snapshot.info?.limits;
    if (this.pending.size >= (limit?.in_flight_requests ?? 32)) throw new WhipError('resource_limit', 'In-flight request limit reached');
    const id = String(++this.nextId);
    const data = `{"jsonrpc":"2.0","id":${JSON.stringify(id)},"method":${JSON.stringify(method)},"params":${encoded ?? JSON.stringify(params)}}`;
    const connection = this.connection!;
    const bytes = byteLength(data) + (connection.kind === 'unix' ? 1 : 0);
    if (bytes > (limit?.frame_bytes ?? 1 << 20)) throw new WhipError('resource_limit', 'Request exceeds the frame limit; upload large content separately');
    if (BigInt(connection.bufferedAmount + bytes) > BigInt(limit?.outbound_bytes ?? 8 << 20)) throw new WhipError('resource_limit', 'Outbound byte limit reached');
    return new Promise<RpcMethods[M]['result']>((resolve, reject) => {
      const cleanup = () => { clearTimeout(timer); options.signal?.removeEventListener('abort', abort); this.pending.delete(id); };
      const fail = (error: Error) => { cleanup(); reject(error); };
      const abort = () => fail(abortError(options.signal));
      const timer = setTimeout(() => fail(new WhipError('timeout', `Request timed out: ${method}`)), options.timeoutMs ?? this.options.queryTimeoutMs ?? 10_000);
      this.pending.set(id, { method, resolve: value => { cleanup(); resolve(value as RpcMethods[M]['result']); }, reject: fail });
      options.signal?.addEventListener('abort', abort, { once: true });
      try { connection.send(data); } catch (error) { fail(asError(error)); }
    });
  }
  private validateNested(method: RpcMethod, value: unknown): void {
    if (!['command.submit', 'query', 'operation.invoke'].includes(method) || !object(value)) return;
    const operation = value.operation as RuntimeOperation;
    if (!Object.hasOwn(runtimeOperations, operation)) throw new WhipError('unsupported_operation', 'Unknown runtime operation');
    const metadata = runtimeOperations[operation];
    const expected = method === 'command.submit' ? 'command' : method === 'query' ? 'query' : 'ephemeral';
    if (metadata.execution !== expected) throw new WhipError('invalid_arguments', `Operation must use the ${metadata.execution} interface`);
    if (!this.supports('runtime', operation)) throw new WhipError('unsupported_operation', `Daemon does not support ${operation}`);
    assertValid(metadata.params_type, value.payload ?? {});
  }
  private receive(text: string): void {
    try {
      if (byteLength(text) > (this.snapshot.info?.limits.frame_bytes ?? 1 << 20)) throw new WhipError('resource_limit', 'Received message exceeds the frame limit');
      const envelope: unknown = JSON.parse(text);
      if (!object(envelope) || envelope.jsonrpc !== '2.0') throw new WhipError('invalid_response', 'Invalid JSON-RPC envelope');
      if (Object.hasOwn(envelope, 'id')) {
        if (typeof envelope.id !== 'string') throw new WhipError('invalid_response', 'Response ID must match the string request ID');
        const pending = this.pending.get(envelope.id);
        if (!pending) return; // Late reply after cancellation/timeout.
        const hasError = Object.hasOwn(envelope, 'error');
        if (hasError === Object.hasOwn(envelope, 'result')) throw new WhipError('invalid_response', 'Expected one response result or error');
        if (hasError) {
          assertValid('RPCError', envelope.error, 'response');
          pending.reject(new RpcError(envelope.error));
        } else {
          assertValid(rpcOperations[pending.method].result_type, envelope.result, 'response');
          pending.resolve(frozen(envelope.result));
        }
        return;
      }
      if (typeof envelope.method !== 'string') throw new WhipError('invalid_response', 'Notification method is missing');
      if (envelope.method === 'event') {
        assertValid('EventNotification', envelope.params, 'response');
        const event = this.decodeEvent(envelope.params.event);
        const stream = event.subscription_id ? this.streams.get(event.subscription_id) : undefined;
        if (!stream) return;
        stream.push(event);
        notify(this.eventListeners, event);
        this.wakeCommands();
      } else if (envelope.method === 'subscription.failed') {
        assertValid('SubscriptionFailure', envelope.params, 'response');
        const failure = envelope.params;
        this.streams.get(failure.subscription_id)?.fail(failure.error ? new RpcError(failure.error) : new WhipError('resynchronization_required', 'Subscription failed'));
      }
    } catch (error) { this.disconnected(new WhipError('invalid_response', 'Malformed daemon response', { cause: error })); }
  }
  private decodeEvent(input: unknown): SdkEvent {
    const wrapper = { event: input };
    assertValid('EventNotification', wrapper, 'response');
    const value = wrapper.event;
    const schema = Object.hasOwn(manifest.event_payloads, value.kind) ? manifest.event_payloads[value.kind] : undefined;
    if (!schema) return frozen({ ...value, payload: value.payload, unknown: true });
    if (!validate(schema, value.payload, 'response') && !validate('ContentEventPayload', value.payload, 'response')) throw new WhipError('invalid_response', `Invalid ${value.kind} event payload`);
    return frozen({ ...value, unknown: false }) as SdkEvent;
  }
  private scheduleHeartbeat(): void {
    clearTimeout(this.heartbeat);
    if (this.snapshot.state !== 'connected') return;
    const epoch = this.epoch;
    this.heartbeat = setTimeout(() => {
      if (epoch !== this.epoch || this.closed) return;
      void this.call('daemon.ping', {}, { timeoutMs: this.options.heartbeatTimeoutMs ?? 10_000 })
        .then(() => { if (epoch === this.epoch && !this.closed) this.scheduleHeartbeat(); }, error => { if (epoch === this.epoch && !this.closed) this.disconnected(asError(error)); });
    }, this.options.heartbeatIntervalMs ?? 30_000);
  }
  releaseSubscription(id: string): void { this.streams.delete(id); }
  query<O extends QueryOperation>(operation: O, payload: RuntimeOperations[O]['params'], options: CallOptions & { rootId?: string } = {}): Promise<QueryOutcome<O>> {
    return this.runtimeCall('query', operation, payload, options);
  }
  invoke<O extends EphemeralOperation>(operation: O, payload: RuntimeOperations[O]['params'], options: CallOptions & { rootId?: string } = {}): Promise<QueryOutcome<O>> {
    return this.runtimeCall('operation.invoke', operation, payload, options);
  }
  private async runtimeCall<O extends RuntimeOperation>(method: 'query' | 'operation.invoke', operation: O, payload: RuntimeOperations[O]['params'], options: CallOptions & { rootId?: string }): Promise<QueryOutcome<O>> {
    const result = await this.call(method, { operation, payload, ...(options.rootId ? { root_id: options.rootId } : {}) }, options);
    if (result.result !== undefined) assertValid(runtimeOperations[operation].result_type, result.result, 'response');
    return result as QueryOutcome<O>;
  }
  submit<O extends CommandOperation>(operation: O, payload: RuntimeOperations[O]['params'], options: CommandOptions = {}): CommandHandle<O> {
    const info = this.requireConnected();
    if (!Object.hasOwn(runtimeOperations, operation) || runtimeOperations[operation].execution !== 'command') throw new WhipError('invalid_arguments', 'Only durable runtime commands can be submitted');
    if (!this.supports('runtime', operation)) throw new WhipError('unsupported_operation', `Daemon does not support ${operation}`);
    assertValid(runtimeOperations[operation].params_type, payload);
    return CommandHandle.submit(this, info.runtime_id, operation, payload, options);
  }
  recover<O extends CommandOperation>(record: RecoveryRecord<O>, payload?: RuntimeOperations[O]['params']): CommandHandle<O> {
    if (record.version !== 1 || record.clientId !== this.clientId || !record.commandId || !Object.hasOwn(runtimeOperations, record.operation) || runtimeOperations[record.operation].execution !== 'command') throw new WhipError('invalid_arguments', 'Invalid recovery record or command namespace');
    return CommandHandle.recover(this, record, payload);
  }
  async recoveryRecords(): Promise<readonly RecoveryRecord[]> { return this.recoveryStorage ? this.recoveryStorage.list() : []; }
  async forget(record: RecoveryRecord): Promise<void> {
    if (record.clientId !== this.clientId) throw new WhipError('invalid_arguments', 'Recovery record belongs to a different client namespace');
    await this.recoveryStorage?.delete(record);
  }
  async commandStatus<O extends CommandOperation>(record: RecoveryRecord<O>, options: CallOptions = {}): Promise<CommandOutcome<O>> {
    options.signal?.throwIfAborted();
    if (record.clientId !== this.clientId) throw new WhipError('invalid_arguments', 'Command belongs to another client namespace');
    const info = await this.whenConnected(options.signal);
    if (info.runtime_id !== record.runtimeId) throw new WhipError('runtime_changed', 'Command belongs to a different persistent runtime');
    let pending = this.lookups.get(record.commandId);
    if (!pending) {
      pending = this.call('command.status', { command_id: record.commandId });
      this.lookups.set(record.commandId, pending);
      void pending.finally(() => { if (this.lookups.get(record.commandId) === pending) this.lookups.delete(record.commandId); }).catch(() => {});
    }
    const result = await withSignal(pending, options.signal);
    return this.commandOutcome(record, result);
  }
  commandOutcome<O extends CommandOperation>(record: RecoveryRecord<O>, result: CommandResult): CommandOutcome<O> {
    if (result.command_id !== record.commandId || result.operation !== record.operation || !['queued', 'running', 'waiting', 'succeeded', 'failed', 'cancelled', 'interrupted'].includes(result.status)) throw new WhipError('invalid_response', 'Command response does not match its identity or lifecycle');
    if (result.status === 'succeeded' && result.result !== undefined) assertValid(runtimeOperations[record.operation].result_type, result.result, 'response');
    const outcome = result as CommandOutcome<O>;
    notify(this.commandListeners, outcome);
    return outcome;
  }
  waitForCommandTick(signal?: AbortSignal): Promise<void> {
    if (this.closed) return Promise.reject(new WhipError('closed', 'Client closed'));
    return new Promise((resolve, reject) => {
      const cleanup = () => {
        this.ticks.delete(done); signal?.removeEventListener('abort', abort);
        if (this.ticks.size === 0) { clearTimeout(this.ticker); this.ticker = undefined; }
      };
      const done = () => { cleanup(); resolve(); };
      const abort = () => { cleanup(); reject(abortError(signal)); };
      this.ticks.add(done);
      signal?.addEventListener('abort', abort, { once: true });
      if (signal?.aborted) { abort(); return; }
      if (!this.paused && !this.closed) this.ticker ??= setTimeout(() => this.wakeCommands(), this.options.commandPollMs ?? 250);
    });
  }
  private wakeCommands(): void {
    clearTimeout(this.ticker); this.ticker = undefined;
    for (const done of [...this.ticks]) done();
  }
  content(handle: ConstructorParameters<typeof ContentReference>[1], scope: ContentScope): ContentReference { return new ContentReference(this, handle, scope); }
  upload(bytes: Uint8Array<ArrayBuffer>, options: UploadOptions): Promise<ContentReference> { return upload(this, bytes, options); }
}

export function createWhipClient(options: ClientOptions): WhipClient { return new WhipClient(options); }
