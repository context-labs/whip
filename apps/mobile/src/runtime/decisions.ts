import { RpcError, isTerminal, type PermissionDecisionStatus, type WhipClient } from '@whip/sdk';
import type { MobileRuntime } from './runtime';
import type { CommandIntent, PermissionRecoveryRecord, StoredRecovery } from './storage';

export type Decision = StoredRecovery<PermissionRecoveryRecord> & { status: string; message?: string; outcome?: PermissionDecisionStatus };
type Snapshot = { identity: string; ready: boolean; items: readonly Decision[] };
const identity = (runtimeId: string, clientId: string) => JSON.stringify([runtimeId, clientId]);
const message = (error: unknown) => error instanceof Error ? error.message : String(error);
/** One-shot permission decisions retain identity, never a body to auto-replay. */
export class DecisionStore {
  private state: Snapshot = Object.freeze({ identity: '', ready: false, items: [] });
  private listeners = new Set<() => void>();
  private epoch = 0;
  private revision = 0;
  private lifetime = new AbortController();
  private reconciling?: Promise<void>;
  private checks = new Map<string, Promise<void>>();
  private sending = new Set<string>();
  private clearing = new Set<string>();
  constructor(private readonly runtime: MobileRuntime) {}
  getSnapshot = () => this.state;
  subscribe = (fn: () => void) => { this.listeners.add(fn); return () => { this.listeners.delete(fn); }; };
  private update(patch: Partial<Snapshot>) { this.revision++; this.state = Object.freeze({ ...this.state, ...patch }); for (const fn of this.listeners) fn(); }
  private put(decision: Decision) { this.update({ items: [...this.state.items.filter(item => item.record.commandId !== decision.record.commandId), Object.freeze(decision)] }); }
  reset() {
    this.epoch++; this.lifetime.abort(); this.lifetime = new AbortController();
    this.reconciling = undefined; this.checks.clear(); this.sending.clear(); this.clearing.clear();
    this.update({ identity: '', ready: false, items: [] });
  }
  private currentIdentity() { const { client, host } = this.runtime.getSnapshot(); return client && host?.runtimeId ? identity(host.runtimeId, client.clientId) : ''; }
  private current(client: WhipClient, epoch: number) { return epoch === this.epoch && this.runtime.getSnapshot().client === client && this.state.identity === this.currentIdentity(); }
  forRequest(requestId: string, rootId?: string) { return this.state.items.find(item => item.intent?.requestId === requestId && (!rootId || item.record.rootId === rootId)); }
  isBlocked(requestId: string, rootId?: string) {
    const runtime = this.runtime.getSnapshot();
    return !runtime.ready || !runtime.active || !this.state.ready || this.state.identity !== this.currentIdentity() || this.state.items.length >= 64 || !!this.forRequest(requestId, rootId)
      || this.state.items.some(item => !item.intent?.requestId && (!rootId || !item.record.rootId || item.record.rootId === rootId));
  }
  reconcile(): Promise<void> {
    if (this.reconciling) return this.reconciling;
    const promise = this.restore();
    this.reconciling = promise;
    void promise.finally(() => { if (this.reconciling === promise) this.reconciling = undefined; }).catch(() => {});
    return promise;
  }
  private async restore() {
    const client = this.runtime.getSnapshot().client;
    if (!client || client.getSnapshot().state !== 'connected' || !this.runtime.getSnapshot().active) { this.update({ ready: false }); return; }
    const epoch = this.epoch;
    const runtimeId = client.requireConnected().runtime_id;
    const scope = identity(runtimeId, client.clientId);
    this.update({ identity: scope, ready: false });
    // A decision already in flight may finish while the journal read is queued.
    // Repeat a changed read so clear/acceptance cannot be overwritten by it.
    let saved: Array<StoredRecovery<PermissionRecoveryRecord>>;
    for (;;) {
      const revision = this.revision;
      try { saved = (await this.runtime.storage.listDecisions()).filter(item => item.record.clientId === client.clientId && item.record.runtimeId === runtimeId); }
      catch (error) { if (!this.current(client, epoch)) return; throw error; }
      if (!this.current(client, epoch)) return;
      if (revision === this.revision) break;
    }
    const merged = new Map(this.state.items.filter(item => item.record.runtimeId === runtimeId && item.record.clientId === client.clientId).map(item => [item.record.commandId, item]));
    for (const item of saved) {
      const previous = merged.get(item.record.commandId);
      merged.set(item.record.commandId, { ...item, ...previous, knownAccepted: item.knownAccepted || previous?.knownAccepted || false, status: previous?.status ?? 'checking' });
    }
    this.update({ items: [...merged.values()], ready: true });
    const items = [...merged.values()];
    for (let offset = 0; offset < items.length; offset += 4) {
      if (!this.current(client, epoch) || client.getSnapshot().state !== 'connected' || !this.runtime.getSnapshot().active) { if (this.current(client, epoch)) this.update({ ready: false }); return; }
      await Promise.all(items.slice(offset, offset + 4).map(item => this.check(item.record.commandId)));
    }
  }
  async decide(rootId: string, requestId: string, agentId: string, allow: boolean, validateRequest: () => void = () => {}) {
    const client = this.runtime.requireReady();
    if (this.isBlocked(requestId, rootId)) throw new Error('Check the previous permission decision before answering again.');
    if (!client.supports('rpc', 'permission.decide')) throw new Error('This Whip host does not support permission decisions.');
    validateRequest();
    const epoch = this.epoch;
    const signal = AbortSignal.any([client.lifetimeSignal, this.lifetime.signal]);
    const record: PermissionRecoveryRecord = { version: 1, runtimeId: client.requireConnected().runtime_id, clientId: client.clientId, commandId: client.createId(), operation: 'permission.decide', rootId };
    const intent: CommandIntent = { requestId, agentId };
    const decision: Decision = { record, intent, knownAccepted: false, status: 'sending' };
    this.sending.add(record.commandId); this.put(decision);
    let persisted = false;
    let accepted = false;
    try {
      await this.runtime.storage.putDecision(record, intent); persisted = true;
      if (!this.current(client, epoch) || this.runtime.requireReady() !== client) throw new Error('Host changed before the permission decision was sent.');
      validateRequest(); signal.throwIfAborted();
      await client.permissions.decide({ command_id: record.commandId, root_id: rootId, permission_id: requestId, allow }, { signal });
      accepted = true;
      if (this.current(client, epoch)) this.put({ ...decision, status: 'succeeded', knownAccepted: true });
      await this.runtime.storage.markDecisionAccepted(record);
      if (!this.current(client, epoch)) return;
      await this.runtime.query.invalidateQueries();
    } catch (error) {
      if (!this.current(client, epoch)) return;
      this.put({ ...decision, knownAccepted: accepted, status: persisted ? 'checking' : 'failed', message: `${!persisted ? 'Decision was not sent. ' : ''}${message(error)}` });
      throw error;
    } finally { if (epoch === this.epoch) this.sending.delete(record.commandId); }
  }
  check(commandId: string): Promise<void> {
    const pending = this.checks.get(commandId);
    if (pending) return pending;
    if (this.sending.has(commandId) || this.clearing.has(commandId)) return Promise.resolve();
    const promise = this.lookup(commandId);
    this.checks.set(commandId, promise);
    void promise.finally(() => { if (this.checks.get(commandId) === promise) this.checks.delete(commandId); }).catch(() => {});
    return promise;
  }
  private async lookup(commandId: string) {
    const client = this.runtime.getSnapshot().client;
    if (!client || client.getSnapshot().state !== 'connected' || !this.runtime.getSnapshot().active) return;
    const previous = this.state.items.find(item => item.record.commandId === commandId);
    if (!previous || this.state.identity !== this.currentIdentity()) return;
    const epoch = this.epoch;
    const signal = AbortSignal.any([client.lifetimeSignal, this.lifetime.signal]);
    const valid = () => this.current(client, epoch) && !this.clearing.has(commandId) && this.state.items.some(item => item.record.commandId === commandId);
    let accepted = previous.knownAccepted;
    try {
      const outcome = await client.permissions.status(commandId, { signal, timeoutMs: 5000 });
      accepted = true;
      if (!valid()) return;
      // Retain this fact before the async disk write: its failure must never make
      // a later missing lookup eligible for a fresh decision.
      this.put({ ...previous, knownAccepted: true, status: outcome.status, outcome, message: 'failure' in outcome ? outcome.failure?.message : undefined });
      await this.runtime.storage.markDecisionAccepted(previous.record);
    } catch (error) {
      if (!valid()) return;
      const latest = this.state.items.find(item => item.record.commandId === commandId)!;
      accepted ||= latest.knownAccepted;
      const missing = error instanceof RpcError && error.kind === 'command_not_found' && !accepted;
      this.put({ ...latest, knownAccepted: accepted, status: missing ? 'not_found' : 'checking', message: message(error) });
    }
  }
  async clear(commandId: string) {
    const decision = this.state.items.find(item => item.record.commandId === commandId);
    const client = this.runtime.getSnapshot().client;
    const epoch = this.epoch;
    if (!client || !decision || this.state.identity !== this.currentIdentity() || this.clearing.has(commandId)) return;
    if (!isTerminal(decision.status) && decision.status !== 'not_found') throw new Error('Check the decision status before clearing it.');
    if (decision.status === 'not_found' && decision.knownAccepted) throw new Error('This decision was accepted previously; its outcome is still unknown.');
    this.clearing.add(commandId);
    try {
      await this.runtime.storage.deleteDecision(decision.record);
      if (this.current(client, epoch)) this.update({ items: this.state.items.filter(item => item.record.commandId !== commandId) });
    } catch (error) { if (this.current(client, epoch)) throw error; } finally { if (epoch === this.epoch) this.clearing.delete(commandId); }
  }
}
