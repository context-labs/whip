import { Client, DurableCommand, RemoteError, type Operations, type RecoveryRecord } from '@whip/sdk';
import { mobileDurableMethods, metadataKey, projectRecovery, type MetadataStorage, type MobileDurableMethod, type RecoveryIntent, type RecoveryMetadata, type StoredMetadata } from './recovery-metadata';

export type DeliveryStatus = 'sending' | 'unknown' | 'missing' | 'accepted' | 'identity_only' | 'failed' | 'unavailable';
export interface DeliveryState extends StoredMetadata {
  status: DeliveryStatus;
  retryable: boolean;
  message?: string;
  inputId?: string;
  destination?: { rootId: string; treeId: string; deleted: boolean };
}
const terminalTransfer = new Set(['TRANSFER_FAILED', 'TRANSFER_UNCERTAIN', 'TRANSFER_INTERRUPTED', 'TRANSFER_CANCELLED', 'TRANSFER_DELETED']);
const detail = (error: unknown) => (error instanceof Error ? error.message : 'Delivery could not be verified').slice(0, 512);
const sameScope = (record: RecoveryMetadata, client: Client) => record.runtimeId === client.runtimeID && record.clientId === client.clientID;

/** Only local delivery state lives here. Native SessionView/ExecutionView own
 * accepted work and outcomes. Original requests are bounded, process-local and
 * never reconstructed from drafts or persisted metadata after restart. */
export class MobileCommands {
  private client?: Client;
  private revision = 0;
  private binding = 0;
  private scope?: string;
  private state: readonly DeliveryState[] = [];
  private listeners = new Set<() => void>();
  private originals = new Map<string, RecoveryRecord>();
  private busy = new Set<string>();
  private disposed = false;
  constructor(private readonly storage: MetadataStorage, private readonly hash: (request: string) => Promise<string>) {}
  getSnapshot = () => this.state;
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  private publish(state: readonly DeliveryState[]) {
    this.state = Object.freeze(state.map(value => Object.freeze({ ...value, record: Object.freeze({ ...value.record }), intent: Object.freeze({ ...value.intent }), ...(value.destination ? { destination: Object.freeze({ ...value.destination }) } : {}) })));
    this.revision++; for (const listener of this.listeners) listener();
  }
  private put(value: DeliveryState) {
    if (!this.client || !sameScope(value.record, this.client)) return;
    const key = metadataKey(value.record);
    this.publish([...this.state.filter(item => metadataKey(item.record) !== key), Object.freeze(value)]);
  }
  private get(record: RecoveryMetadata) { return this.state.find(item => metadataKey(item.record) === metadataKey(record)); }
  async bind(client: Client): Promise<void> {
    if (this.disposed) throw new Error('Mobile command owner is closed');
    const scope = JSON.stringify([client.runtimeID, client.clientID]);
    const same = this.scope === scope;
    const binding = ++this.binding; this.client = client; this.scope = scope;
    if (!same) { this.originals.clear(); this.publish([]); }
    let values: StoredMetadata[];
    for (;;) {
      const revision = this.revision; values = await this.storage.list();
      if (this.binding !== binding) return;
      if (revision === this.revision) break;
    }
    const previous = new Map(this.state.map(item => [metadataKey(item.record), item]));
    this.publish(values.filter(value => sameScope(value.record, client) && (mobileDurableMethods as readonly string[]).includes(value.record.operation)).map(value => {
      const current = previous.get(metadataKey(value.record));
      const knownAccepted = value.knownAccepted || current?.knownAccepted || this.originals.get(metadataKey(value.record))?.accepted || false;
      return { ...value, ...current, status: value.knownAccepted ? 'accepted' : current?.status ?? 'unknown', retryable: !knownAccepted && (current?.retryable ?? false), knownAccepted };
    }));
  }
  /** Detach cancels no work and keeps originals for an explicit same-host
   * reconnect. A new owner (app restart) has no originals and cannot retry. */
  detach() { this.binding++; this.client = undefined; }
  private requireClient() { if (!this.client || this.disposed) throw new Error('Connect this host before inspecting delivery'); return this.client; }
  isBlocked(sessionId: string) {
    return this.state.some(item => item.record.sessionId === sessionId && ['sending', 'unknown', 'identity_only', 'unavailable'].includes(item.status));
  }
  private retain(key: string, record: RecoveryRecord) {
    const size = new TextEncoder().encode(record.request).byteLength;
    let total = size;
    for (const [existingKey, original] of this.originals) if (existingKey !== key) total += new TextEncoder().encode(original.request).byteLength;
    if (!this.originals.has(key) && this.originals.size >= 64 || total > 8 << 20) throw new Error('Original request memory is full; resolve or forget an existing delivery first');
    this.originals.set(key, record);
  }
  async run<M extends MobileDurableMethod>(command: DurableCommand<M>, options: { rootId?: string; intent?: RecoveryIntent } = {}): Promise<Operations[M]['result']> {
    const client = this.requireClient();
    if (command.record.runtimeID !== client.runtimeID || command.record.clientID !== client.clientID) throw new Error('This command belongs to another host');
    const value = await projectRecovery(command.record, this.hash, options);
    if (this.client !== client) throw new Error('Host changed before submission');
    const key = metadataKey(value.record);
    if (this.busy.has(key) || this.get(value.record)) throw new Error('Check the existing delivery before sending again');
    if (value.record.sessionId && this.isBlocked(value.record.sessionId)) throw new Error('Check the previous delivery to this recipient before sending again');
    this.retain(key, command.record); this.busy.add(key);
    this.put({ ...value, status: 'sending', retryable: false });
    let persisted = false;
    try {
      await this.storage.put(value); persisted = true;
      if (this.client !== client) throw new Error('Host changed before submission');
      this.put({ ...value, status: 'sending', retryable: false });
      const result = await command.send();
      await this.accept(value, result);
      return result;
    } catch (error) {
      if (persisted) this.failed(value, error);
      else { this.originals.delete(key); this.put({ ...value, status: 'failed', retryable: false, message: 'Request was not sent: ' + detail(error) }); }
      throw error;
    } finally { this.busy.delete(key); }
  }
  private destination(result: unknown): DeliveryState['destination'] {
    if (result && typeof result === 'object' && 'creation' in result && 'deleted' in result) {
      const value = result as Operations['trees.create']['result'];
      return { rootId: value.creation.root_id, treeId: value.creation.tree_id, deleted: value.deleted };
    }
    if (result && typeof result === 'object' && 'fork' in result && 'deleted' in result) {
      const value = result as Operations['sessions.fork']['result'];
      return { rootId: value.fork.root_id, treeId: value.fork.tree_id, deleted: value.deleted };
    }
    return undefined;
  }
  private async accept(value: StoredMetadata, result?: unknown) {
    const key = metadataKey(value.record);
    const original = this.originals.get(key);
    if (original) this.originals.set(key, { ...original, accepted: true });
    const inputId = value.record.operation === 'sessions.submit' && result && typeof result === 'object' && 'input' in result ? (result as Operations['sessions.submit']['result']).input?.id : undefined;
    this.put({ ...value, knownAccepted: true, status: 'accepted', retryable: false, destination: this.destination(result), inputId });
    await this.storage.accept(value.record);
    this.originals.delete(key);
  }
  private failed(value: StoredMetadata, error: unknown) {
    const current = this.get(value.record);
    const knownAccepted = value.knownAccepted || current?.knownAccepted || false;
    const terminal = error instanceof RemoteError && (terminalTransfer.has(error.kind) || error.kind === 'CONFLICT');
    if (terminal) this.originals.delete(metadataKey(value.record));
    this.put({ ...value, ...current, knownAccepted, status: terminal ? 'failed' : 'unknown', retryable: false, message: detail(error) });
  }
  async check(record: RecoveryMetadata, signal?: AbortSignal): Promise<DeliveryState> {
    const client = this.requireClient(); const previous = this.get(record);
    if (!previous || !sameScope(record, client)) throw new Error('This delivery belongs to another host or was forgotten');
    const key = metadataKey(record);
    if (this.busy.has(key)) return previous;
    this.busy.add(key);
    try {
      const original = this.originals.get(key);
      if (original) {
        const check = await DurableCommand.recover(client, original).check({ signal });
        if (this.client !== client) throw new Error('Host changed while inspecting delivery');
        if (check.state === 'found') await this.accept(previous, check.evidence);
        else this.put({ ...previous, status: check.state === 'missing' ? 'missing' : check.state, retryable: check.state === 'missing' && !previous.knownAccepted,
          message: check.state === 'identity_only' ? 'This identity exists. Review the destination; the host read does not prove the original payload.' : undefined,
          destination: check.state === 'identity_only' ? this.destination(check.evidence) : previous.destination });
      } else {
        const evidence = await this.inspectMetadata(client, previous, signal);
        if (this.client !== client) throw new Error('Host changed while inspecting delivery');
        if (evidence === undefined) this.put({ ...previous, status: 'unavailable', retryable: false, message: 'This action has no metadata-only receipt lookup. The original request is no longer available; it cannot be retried.' });
        else if (previous.knownAccepted) await this.accept(previous, evidence);
        else this.put({ ...previous, status: 'identity_only', retryable: false, destination: this.destination(evidence), message: 'The host has this identity. The original request is no longer available, so its exact payload cannot be verified or retried.' });
      }
    } catch (error) {
      if (this.client !== client) throw error;
      if (error instanceof RemoteError && error.kind === 'NOT_FOUND' && !previous.knownAccepted)
        this.put({ ...previous, status: 'missing', retryable: this.originals.has(key), message: this.originals.has(key) ? undefined : 'No receipt was found. The original request is no longer available; review your retained draft before sending a new request.' });
      else this.failed(previous, error);
    } finally { this.busy.delete(key); }
    return this.get(record) ?? previous;
  }
  private async inspectMetadata(client: Client, value: StoredMetadata, signal?: AbortSignal): Promise<unknown> {
    const record = value.record;
    switch (record.operation) {
      case 'sessions.submit': case 'sessions.compact': case 'sessions.spawn': {
        const result = await client.call('receipts.get', { client_id: record.clientId, request_id: record.commandId }, { signal });
        if (result.receipt.identity.client_id !== record.clientId || result.receipt.identity.request_id !== record.commandId || result.input && record.operation !== 'sessions.spawn' && result.input.session_id !== record.sessionId) throw new Error('Receipt identity or recipient mismatch');
        return result;
      }
      case 'trees.create': {
        const result = await client.getTreeCreation(record.commandId, { signal });
        if (result.creation.id !== record.commandId) throw new Error('Creation receipt identity mismatch');
        return result;
      }
      case 'permissions.set_mode': case 'permissions.set_denial': {
        const result = record.operation === 'permissions.set_mode'
          ? await client.getPermissionModeEdit(record.sessionId!, record.commandId, { signal })
          : await client.getPermissionDenialEdit(record.sessionId!, record.commandId, { signal });
        if (result.id !== record.commandId || result.session_id !== record.sessionId) throw new Error('Permission edit identity or recipient mismatch');
        return result;
      }
      case 'sessions.reload': return client.getReloadEdit(record.sessionId!, record.commandId, { signal });
      case 'inputs.steer': return client.session(record.sessionId!).inputs.steering(record.commandId, { signal });
      default: return undefined;
    }
  }
  async retry(record: RecoveryMetadata): Promise<unknown> {
    const client = this.requireClient(); const previous = this.get(record); const key = metadataKey(record);
    const original = this.originals.get(key);
    if (!previous?.retryable || previous.status !== 'missing' || previous.knownAccepted || !original || this.busy.has(key)) throw new Error('Only a confirmed missing delivery with its original request can be retried');
    this.busy.add(key); this.put({ ...previous, status: 'sending', retryable: false });
    try {
      const result = await DurableCommand.recover(client, original).retry();
      await this.accept(previous, result); return result;
    } catch (error) { this.failed(previous, error); throw error; }
    finally { this.busy.delete(key); }
  }
  /** Forgetting is an explicit local action, including an unverifiable restored
   * identity. It never resends work and never deletes the retained draft. */
  async forget(record: RecoveryMetadata) {
    const key = metadataKey(record);
    if (this.busy.has(key)) throw new Error('Wait for the current delivery inspection to finish');
    await this.storage.delete(record); this.originals.delete(key);
    this.publish(this.state.filter(item => metadataKey(item.record) !== key));
  }
  dispose() { this.disposed = true; this.client = undefined; this.originals.clear(); this.listeners.clear(); this.state = []; }
}
