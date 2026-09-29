import { assertValid } from '@whip/protocol';
import type { Admission, Operations } from '@whip/protocol';
import type { Client } from './index.js';
import { operation, RemoteError } from './wire.js';
import type { CallOptions } from './wire.js';
import { boundedInteger, bytes, delay, freeze, utf8Base64, withSignal } from './value.js';

const methods = [
  'sessions.submit', 'sessions.compact', 'sessions.spawn', 'goals.formulate', 'goals.resume', 'tool.call', 'shell.run',
  'trees.create', 'sessions.reload', 'sessions.fork', 'sessions.rewind', 'permissions.set_mode', 'permissions.set_denial',
  'workspace.capture', 'workspace.restore', 'workspace.release', 'workspace.set', 'run.configure', 'inputs.steer',
  'goals.create', 'schedules.create', 'mail.send', 'state.write', 'state.append', 'state.subscribe',
] as const;
export type DurableMethod = typeof methods[number];
type CommandRequest = { [M in DurableMethod]: { method: M; params: Operations[M]['params'] } }[DurableMethod];
export const recoveryNamespace = 'whip.v4.commands' as const;
export const maxRecoveryRecordBytes = 4 << 20;
export interface RecoveryRecord {
  readonly namespace: typeof recoveryNamespace;
  readonly version: 1;
  readonly runtimeID: string;
  readonly clientID: string;
  /** Encoded method and exact original params. Contains user input, never host credentials. */
  readonly request: string;
  readonly accepted: boolean;
}
export type RecoveryEvidence = Admission | Operations['sessions.reload_edit']['result'] | Operations['trees.creation']['result'] | Operations['permissions.mode_edit']['result'] | Operations['permissions.denial_edit']['result'] | Operations['workspace.action']['result'] | Operations['inputs.steering']['result'];
export type RecoveryCheck = { state: 'found'; evidence: RecoveryEvidence } | { state: 'identity_only'; evidence: RecoveryEvidence } | { state: 'missing' } | { state: 'unavailable' };
export class RecoveryError extends Error {
  constructor(message: string) { super(message); this.name = 'RecoveryError'; }
}

/** Local persistence failed. accepted and acknowledgement keep a received host
 * result distinct from a failure that happened before any bytes were sent. */
export class RecoveryPersistenceError extends RecoveryError {
  readonly accepted: boolean;
  constructor(readonly record: RecoveryRecord, readonly acknowledgement: unknown, cause: unknown) {
    super(record.accepted ? 'Host acceptance is known but recovery persistence failed' : 'Recovery persistence failed before sending');
    this.name = 'RecoveryPersistenceError'; this.accepted = record.accepted; this.cause = cause;
  }
}

function validateRecord(value: unknown): RecoveryRecord {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('Invalid recovery record');
  const record = value as RecoveryRecord;
  if (Object.keys(record).sort().join(',') !== 'accepted,clientID,namespace,request,runtimeID,version' || record.namespace !== recoveryNamespace || record.version !== 1 || typeof record.request !== 'string' || typeof record.accepted !== 'boolean' || bytes(record) > maxRecoveryRecordBytes) throw new TypeError('Invalid or oversized v4 recovery record');
  assertValid('RequestIdentity', { client_id: record.clientID, request_id: record.runtimeID });
  const request: unknown = JSON.parse(record.request);
  if (!request || typeof request !== 'object' || Array.isArray(request) || Object.keys(request).sort().join(',') !== 'method,params') throw new TypeError('Invalid recovery request');
  const candidate = request as CommandRequest;
  if (!methods.includes(candidate.method)) throw new TypeError('Operation cannot be journaled or replayed');
  assertValid(operation(candidate.method).params, candidate.params);
  if ('identity' in candidate.params && candidate.params.identity.client_id !== record.clientID) throw new TypeError('Recovery client identity mismatch');
  return Object.freeze({ ...record });
}
function requestID(params: CommandRequest['params']): string {
  return 'identity' in params ? params.identity.request_id : 'creation_id' in params ? params.creation_id : 'fork_id' in params ? params.fork_id : 'edit_id' in params ? params.edit_id : 'action_id' in params ? params.action_id : 'id' in params ? params.id : 'goal_id' in params ? params.goal_id : 'schedule_id' in params ? params.schedule_id : 'mail_id' in params ? params.mail_id : 'version_id' in params ? params.version_id : params.subscription_id;
}
function key(record: RecoveryRecord): string {
  const { method, params } = JSON.parse(record.request) as CommandRequest;
  return JSON.stringify([record.runtimeID, record.clientID, 'identity' in params ? 'receipt' : method, requestID(params)]);
}
/** Implementations must namespace their storage, return at most limit records,
 * and durably replace one keyed record before resolving put. The journal is the
 * single writer for this storage; unrelated app stores do not share its keys. */
export interface RecoveryStorage {
  list(namespace: typeof recoveryNamespace, limit: number): Promise<readonly unknown[]>;
  put(namespace: typeof recoveryNamespace, key: string, record: RecoveryRecord): Promise<void>;
  delete(namespace: typeof recoveryNamespace, key: string): Promise<void>;
}
/** Opt-in persistence of exact user requests. Overflow rejects before sending;
 * unresolved records are never evicted automatically. No records are cached. */
export class RecoveryJournal {
  private serial: Promise<unknown> = Promise.resolve();
  private pending = 0;
  private pendingBytes = 0;
  private readonly maxRecords: number;
  private readonly maxBytes: number;
  constructor(private readonly storage: RecoveryStorage, limits: { maxRecords?: number; maxBytes?: number } = {}) {
    this.maxRecords = boundedInteger(limits.maxRecords ?? 64, 'maxRecords', 1024);
    this.maxBytes = boundedInteger(limits.maxBytes ?? 8 << 20, 'maxBytes', 64 << 20);
  }
  private exclusive<T>(run: () => Promise<T>, weight = 0): Promise<T> {
    if (this.pending >= this.maxRecords || weight > this.maxBytes - this.pendingBytes) return Promise.reject(new RecoveryError('Recovery journal pending work limit exceeded'));
    this.pending++; this.pendingBytes += weight;
    const next = this.serial.then(run).finally(() => { this.pending--; this.pendingBytes -= weight; });
    this.serial = next.catch(() => {}); return next;
  }
  private async read(): Promise<readonly RecoveryRecord[]> {
    const values = await this.storage.list(recoveryNamespace, this.maxRecords + 1);
    if (!Array.isArray(values) || values.length > this.maxRecords) throw new RecoveryError('Recovery journal count limit exceeded');
    const records: RecoveryRecord[] = []; const identities = new Set<string>(); let total = 0;
    for (const value of values) {
      const record = validateRecord(value); const identity = key(record); total += bytes(record);
      if (identities.has(identity) || total > this.maxBytes) throw new RecoveryError('Recovery journal identity or byte limit exceeded');
      identities.add(identity); records.push(record);
    }
    return Object.freeze(records);
  }
  list(): Promise<readonly RecoveryRecord[]> { return this.exclusive(() => this.read()); }
  put(value: RecoveryRecord): Promise<void> {
    const record = validateRecord(value);
    return this.exclusive(async () => {
      const records = await this.read(); const identity = key(record);
      const previous = records.find(item => key(item) === identity);
      if (previous && previous.request !== record.request) throw new RecoveryError('Recovery identity already has a different request');
      if (!previous && records.length >= this.maxRecords || records.reduce((total, item) => total + (key(item) === identity ? 0 : bytes(item)), bytes(record)) > this.maxBytes) throw new RecoveryError('Recovery journal is full; resolve or explicitly forget a record');
      await this.storage.put(recoveryNamespace, identity, previous?.accepted ? { ...record, accepted: true } : record);
    }, bytes(record));
  }
  forget(value: RecoveryRecord): Promise<void> {
    const record = validateRecord(value);
    return this.exclusive(() => this.storage.delete(recoveryNamespace, key(record)));
  }
}

/** A prepared mutation. Construction, recovery and inspection never send it.
 * Exact retries are explicit and rely on the host's immutable receipt first. */
export class DurableCommand<M extends DurableMethod> {
  private sent?: Promise<Operations[M]['result']>;
  private retrying?: Promise<Operations[M]['result']>;
  private current: RecoveryRecord;
  private constructor(private readonly client: Client, readonly method: M, record: RecoveryRecord, private readonly journal?: RecoveryJournal, private readonly recovered = false) {
    this.current = validateRecord(record);
    if (record.runtimeID !== client.runtimeID || record.clientID !== client.clientID) throw new RecoveryError('Recovery belongs to another runtime or client');
  }
  static prepare<M extends DurableMethod>(client: Client, method: M, params: Operations[M]['params'], options: { journal?: RecoveryJournal } = {}): DurableCommand<M> {
    const record: RecoveryRecord = { namespace: recoveryNamespace, version: 1, runtimeID: client.runtimeID, clientID: client.clientID, request: JSON.stringify({ method, params }), accepted: false };
    return new DurableCommand(client, method, record, options.journal);
  }
  static recover(client: Client, record: RecoveryRecord, options: { journal?: RecoveryJournal } = {}): DurableCommand<DurableMethod> {
    const checked = validateRecord(record);
    return new DurableCommand(client, (JSON.parse(checked.request) as CommandRequest).method, checked, options.journal, true);
  }
  get record(): RecoveryRecord { return this.current; }
  /** Stable caller identity used by notices and explicit recovery actions. */
  get id(): string { return requestID(this.params); }
  get params(): Operations[M]['params'] { return JSON.parse(this.current.request).params as Operations[M]['params']; }
  private async persistAccepted(acknowledgement: unknown): Promise<void> {
    if (this.current.accepted) return;
    this.current = Object.freeze({ ...this.current, accepted: true });
    try { await this.journal?.put(this.current); }
    catch (error) { throw new RecoveryPersistenceError(this.current, acknowledgement, error); }
  }
  private async transmit(): Promise<Operations[M]['result']> {
    try { await this.journal?.put(this.current); }
    catch (error) { throw new RecoveryPersistenceError(this.current, undefined, error); }
    const result = await this.client.call(this.method, this.params);
    await this.persistAccepted(result);
    return result;
  }
  /** Sends once. Cancelling a waiter leaves this shared submission alone. */
  send(options: CallOptions = {}): Promise<Operations[M]['result']> {
    options.signal?.throwIfAborted();
    if (this.retrying) return withSignal(this.retrying, options.signal);
    if (this.recovered && !this.sent) throw new RecoveryError('Recovered commands require an explicit check/retry');
    this.sent ??= this.transmit();
    return withSignal(this.sent, options.signal);
  }
  async check(options: CallOptions = {}): Promise<RecoveryCheck> {
    const request = JSON.parse(this.current.request) as CommandRequest;
    try {
      let evidence: RecoveryEvidence;
      let matchesRequest = this.current.accepted;
      switch (request.method) {
        case 'sessions.submit': case 'sessions.compact': case 'sessions.spawn': case 'goals.formulate': case 'goals.resume': case 'tool.call': case 'shell.run': {
          evidence = await this.client.call('receipts.match', { method: request.method, params_base64: utf8Base64(JSON.stringify(request.params)) }, options);
          if (evidence.receipt.identity.client_id !== this.current.clientID || evidence.receipt.identity.request_id !== request.params.identity.request_id) throw new TypeError('Receipt identity mismatch');
          if ('session_id' in request.params && evidence.input && evidence.input.session_id !== request.params.session_id) throw new TypeError('Receipt session mismatch');
          matchesRequest = true;
          break;
        }
        case 'trees.create': {
          evidence = await this.client.getTreeCreation(request.params.creation_id, options);
          if (evidence.creation.id !== request.params.creation_id) throw new TypeError('Creation receipt mismatch');
          break;
        }
        case 'inputs.steer': {
          evidence = await this.client.session(request.params.session_id).inputs.steering(request.params.edit_id, options);
          if (evidence.input_id !== request.params.input_id || evidence.turn_id !== request.params.turn_id) throw new TypeError('Steering receipt mismatch');
          matchesRequest = true;
          break;
        }
        case 'sessions.reload': {
          evidence = await this.client.getReloadEdit(request.params.session_id, request.params.edit_id, options);
          if (evidence.expected_revision !== request.params.expected_revision) throw new TypeError('Reload receipt request mismatch');
          matchesRequest = true;
          break;
        }
        case 'permissions.set_denial': {
          evidence = await this.client.getPermissionDenialEdit(request.params.session_id, request.params.edit_id, options);
          if (evidence.id !== request.params.edit_id || evidence.session_id !== request.params.session_id || evidence.deny_interactive !== request.params.deny_interactive || evidence.expected_revision !== request.params.expected_revision) throw new TypeError('Permission denial receipt mismatch');
          matchesRequest = true;
          break;
        }
        case 'permissions.set_mode': {
          evidence = await this.client.getPermissionModeEdit(request.params.session_id, request.params.edit_id, options);
          if (evidence.id !== request.params.edit_id || evidence.session_id !== request.params.session_id || evidence.mode !== request.params.mode || evidence.expected_revision !== request.params.expected_revision) throw new TypeError('Permission edit receipt mismatch');
          matchesRequest = true;
          break;
        }
        case 'workspace.capture': case 'workspace.restore': case 'workspace.release': {
          evidence = await this.client.getWorkspaceAction(request.params.session_id, request.params.action_id, options);
          if (evidence.id !== request.params.action_id || evidence.session_id !== request.params.session_id || evidence.snapshot_id !== request.params.snapshot_id || evidence.kind !== request.method.slice('workspace.'.length)) throw new TypeError('Workspace receipt mismatch');
          break;
        }
        default: return { state: 'unavailable' };
      }
      // Most evidence reads expose identity/outcome, not the original payload.
      // A colliding ID must not turn an unacknowledged request into acceptance.
      if (!matchesRequest) return { state: 'identity_only', evidence: freeze(evidence) as RecoveryEvidence };
      await this.persistAccepted(evidence);
      return { state: 'found', evidence: freeze(evidence) as RecoveryEvidence };
    } catch (error) {
      if (!(error instanceof RemoteError) || error.kind !== 'NOT_FOUND') throw error;
      if (this.current.accepted) throw new RecoveryError('Previously accepted receipt is missing; this command must not be replayed');
      return { state: 'missing' };
    }
  }
  /** An explicit resend keeps exact bytes and identity. An unavailable read does
   * not claim absence; the server still checks its immutable receipt before work. */
  retry(options: CallOptions = {}): Promise<Operations[M]['result']> {
    options.signal?.throwIfAborted();
    this.retrying ??= (async () => {
      await this.sent?.catch(() => {});
      await this.check();
      // A found receipt is still replayed only at this explicit caller request:
      // the server returns the original method's typed acknowledgement/tombstone.
      this.sent = this.transmit();
      return this.sent;
    })().finally(() => { this.retrying = undefined; });
    return withSignal(this.retrying, options.signal);
  }
  /** Poll only ordinary admission receipts. Aborting never cancels execution. */
  async wait(options: CallOptions = {}): Promise<Admission> {
    if (!('identity' in this.params)) throw new TypeError('This mutation has no input/turn receipt to wait for');
    for (;;) {
      const check = await this.check(options);
      if (check.state !== 'found' || !('receipt' in check.evidence)) throw new RecoveryError('Exact request acceptance is not known; verify delivery before waiting');
      const value = check.evidence;
      if (value.receipt.deleted_at || value.input?.state === 'cancelled' || value.turn?.finished_at) return value;
      await delay(100, options.signal);
    }
  }
  /** Explicitly removes local recovery only, never remote work. */
  async forget(): Promise<void> {
    await this.retrying?.catch(() => {});
    await this.sent?.catch(() => {});
    await this.journal?.forget(this.current);
  }
}
