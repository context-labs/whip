import * as Crypto from 'expo-crypto';
import { RemoteError, type Question, type AnswerQuestionParams } from '@whip/sdk';
import type { MobileRuntime } from './runtime';
import { metadataKey, type RecoveryIntent, type StoredMetadata } from './recovery-metadata';

type QuestionRequest = Question['request'];
type QuestionAnswers = AnswerQuestionParams['answers'];
export type Decision = StoredMetadata & { status: 'sending' | 'unknown' | 'delivered' | 'resolved' | 'pending' | 'unavailable'; message?: string };
/** A local decision journal, not permission authority. A completed host operation
 * can prove the request is resolved, but cannot prove which client answered it. */
export class DecisionStore {
  private state: { ready: boolean; items: readonly Decision[] } = { ready: false, items: [] };
  private listeners = new Set<() => void>();
  private generation = 0;
  private lifetime = new AbortController();
  private busy = new Set<string>();
  private known = new Set<string>();
  private revision = 0;
  constructor(private readonly runtime: MobileRuntime) {}
  getSnapshot = () => this.state;
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  private update(patch: Partial<typeof this.state>) {
    this.revision++; this.state = Object.freeze({ ...this.state, ...patch }); for (const listener of this.listeners) listener();
  }
  private put(value: Decision) {
    const client = this.runtime.getSnapshot().client;
    if (!client || value.record.runtimeId !== client.runtimeID || value.record.clientId !== client.clientID) return;
    this.update({ items: [...this.state.items.filter(item => metadataKey(item.record) !== metadataKey(value.record)), Object.freeze({ ...value, record: Object.freeze({ ...value.record }), intent: Object.freeze({ ...value.intent }) })] });
  }
  reset() { this.generation++; this.lifetime.abort(); this.lifetime = new AbortController(); this.update({ ready: false, items: [] }); }
  forRequest(requestId: string, rootId?: string) { return this.state.items.find(item => item.record.commandId === requestId && (!rootId || item.record.rootId === rootId)); }
  isBlocked(requestId: string, rootId?: string) { return !this.state.ready || !this.runtime.getSnapshot().ready || !this.runtime.getSnapshot().active || this.busy.has(requestId) || !!this.forRequest(requestId, rootId); }
  async reconcile() {
    const client = this.runtime.getSnapshot().client; if (!client || !this.runtime.getSnapshot().active) return;
    const generation = this.generation; let saved: StoredMetadata[];
    for (;;) {
      const revision = this.revision; saved = await this.runtime.storage.nativeRecovery.list();
      if (this.generation !== generation || this.runtime.getSnapshot().client !== client) return;
      if (revision === this.revision) break;
    }
    const current = new Map(this.state.items.map(value => [metadataKey(value.record), value]));
    this.update({ ready: true, items: saved.filter(value => value.record.runtimeId === client.runtimeID && value.record.clientId === client.clientID && ['permissions.resolve', 'questions.answer'].includes(value.record.operation)).map(value => {
      const previous = current.get(metadataKey(value.record)); const knownAccepted = value.knownAccepted || previous?.knownAccepted || this.known.has(metadataKey(value.record));
      return { ...value, ...previous, knownAccepted, status: knownAccepted ? 'delivered' : previous?.status ?? 'unknown' };
    }) });
  }
  decide(rootId: string, operationId: string, sessionId: string, approved: boolean, validate: () => void = () => {}) {
    return this.send('permissions.resolve', rootId, sessionId, operationId, { operation_id: operationId, approved }, {}, validate);
  }
  answer(rootId: string, sessionId: string, operationId: string, answers: QuestionAnswers, intent: RecoveryIntent, expected: QuestionRequest) {
    return this.send('questions.answer', rootId, sessionId, operationId, { session_id: sessionId, operation_id: operationId, answers }, intent, () => {}, expected);
  }
  private async send(method: 'permissions.resolve' | 'questions.answer', rootId: string, sessionId: string, operationId: string,
    params: { operation_id: string; approved: boolean } | { session_id: string; operation_id: string; answers: QuestionAnswers }, intent: RecoveryIntent, validate: () => void, expected?: QuestionRequest) {
    const client = this.runtime.requireReady(); if (this.isBlocked(operationId, rootId)) throw new Error('Inspect the previous decision before answering again');
    this.busy.add(operationId); const generation = this.generation; const signal = this.lifetime.signal;
    let value: StoredMetadata | undefined; let persisted = false;
    try {
      validate(); params = structuredClone(params); intent = structuredClone(intent); expected = expected && structuredClone(expected);
      const record = { version: 4 as const, runtimeId: client.runtimeID, clientId: client.clientID, commandId: operationId, operation: method,
        requestHash: await Crypto.digestStringAsync(Crypto.CryptoDigestAlgorithm.SHA256, JSON.stringify(params)), rootId, sessionId };
      value = { record, intent, knownAccepted: false };
      if (method === 'permissions.resolve') {
        const operation = await client.session(sessionId).operations.get(operationId, { signal });
        if (operation.state !== 'waiting') throw new Error('This permission request was already resolved; refresh it');
      } else {
        const question = await client.session(sessionId).questions.get(operationId, { signal });
        if (question.state !== 'pending' || JSON.stringify(question.request) !== JSON.stringify(expected)) throw new Error('This question changed or was answered elsewhere; refresh it');
      }
      await this.runtime.storage.nativeRecovery.put(value); persisted = true;
      signal.throwIfAborted(); if (this.runtime.requireReady() !== client) throw new Error('Host changed before the decision was sent'); validate();
      this.put({ ...value, status: 'sending' });
      if ('approved' in params) await client.call('permissions.resolve', params, { signal });
      else await client.call('questions.answer', params, { signal });
      this.known.add(metadataKey(record)); this.put({ ...value, knownAccepted: true, status: 'delivered' });
      await this.runtime.storage.nativeRecovery.accept(record);
      this.known.delete(metadataKey(record));
      if (this.generation === generation) await this.runtime.query.invalidateQueries();
    } catch (error) {
      if (persisted && value && this.generation === generation) this.put({ ...value, knownAccepted: this.known.has(metadataKey(value.record)), status: 'unknown', message: (error instanceof Error ? error.message : 'Decision could not be verified').slice(0, 512) });
      throw error;
    } finally { this.busy.delete(operationId); }
  }
  async check(operationId: string) {
    const client = this.runtime.requireReady(); const value = this.forRequest(operationId);
    if (!value || this.busy.has(operationId)) return;
    const generation = this.generation; this.busy.add(operationId);
    try {
      const session = client.session(value.record.sessionId!);
      const result = value.record.operation === 'questions.answer'
        ? await session.questions.get(operationId, { signal: this.lifetime.signal })
        : await session.operations.get(operationId, { signal: this.lifetime.signal });
      if (this.generation !== generation || this.runtime.getSnapshot().client !== client) return;
      const pending = result.state === 'waiting' || result.state === 'pending';
      this.put({ ...value, status: pending ? 'pending' : value.knownAccepted ? 'delivered' : 'resolved', message: pending ? 'The host still needs a decision. Review it before making a new attempt.' : value.knownAccepted ? undefined : 'The host resolved this request. Another client may have answered it; this does not prove your original decision was delivered.' });
    } catch (error) {
      if (this.generation === generation) this.put({ ...value, status: 'unavailable', message: error instanceof RemoteError && error.kind === 'NOT_FOUND' ? 'The request is unavailable. Its original decision will not be replayed.' : 'Decision status could not be read; no decision was resent.' });
    } finally { this.busy.delete(operationId); }
  }
  async clear(operationId: string) {
    const value = this.forRequest(operationId); if (!value) return;
    if (this.busy.has(operationId)) throw new Error('Wait for decision delivery to finish');
    const generation = this.generation;
    await this.runtime.storage.nativeRecovery.delete(value.record); this.known.delete(metadataKey(value.record));
    if (generation !== this.generation) return;
    this.update({ items: this.state.items.filter(item => metadataKey(item.record) !== metadataKey(value.record)) });
  }
}
