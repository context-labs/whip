import type {
  CommandOperation, RuntimeOperations, QueryOperation, EphemeralOperation,
  CreateSessionParams, SessionCatalogParams, SubmitPayload, HistoryPageParams, MailboxPageParams,
} from '@whip/protocol';
import type { WhipClient, CallOptions } from './client.js';
import type { CommandOptions } from './command.js';
import { encodeBase64 } from './util.js';

/** Root-bound handle. Merely constructing it causes no I/O. */
export class Session {
  constructor(readonly client: WhipClient, readonly rootId: string) {
    if (!rootId) throw new TypeError('rootId is required');
  }
  command<O extends CommandOperation>(operation: O, payload: RuntimeOperations[O]['params'], options: Omit<CommandOptions, 'rootId'> = {}) {
    return this.client.submit(operation, payload, { ...options, rootId: this.rootId });
  }
  query<O extends QueryOperation>(operation: O, payload: RuntimeOperations[O]['params'], options: CallOptions = {}) {
    return this.client.query(operation, payload, { ...options, rootId: this.rootId });
  }
  invoke<O extends EphemeralOperation>(operation: O, payload: RuntimeOperations[O]['params'], options: CallOptions = {}) {
    return this.client.invoke(operation, payload, { ...options, rootId: this.rootId });
  }
  submit(payload: SubmitPayload, options: Omit<CommandOptions, 'rootId'> = {}) { return this.command('submit', payload, options); }
  steer(payload: SubmitPayload, options: Omit<CommandOptions, 'rootId'> = {}) { return this.command('steer', payload, options); }
  rename(title: string) { return this.command('session.rename', { title }); }
  fork(params: RuntimeOperations['session.fork']['params']) { return this.command('session.fork', params); }
  delete() { return this.client.sessions.delete(this.rootId); }
  snapshot(options: CallOptions = {}) { return this.client.call('root.snapshot', { root_id: this.rootId }, options); }
  configure(params: RuntimeOperations['run.configure']['params']) { return this.command('run.configure', params); }
  setModel(model: string, provider = '', persistDefault = false) { return this.command('session.model', { model, provider, persist_default: persistDefault }); }
  setEffort(effort: string, persistDefault = false) { return this.command('session.effort', { effort, persist_default: persistDefault }); }
  /** externalPermissions = prompt connected clients; false = approve automatically. */
  setPermissionMode(externalPermissions: boolean) { return this.command('permission.mode', { external_permissions: externalPermissions }); }
  cancelTurn(turnId: string) { return this.command('cancel', { turn_id: turnId }); }
  readonly history = {
    page: (params: Partial<Omit<HistoryPageParams, 'root_id'>> = {}, options: CallOptions = {}) => this.client.call('history.page', {
      root_id: this.rootId, agent_id: this.rootId, through_seq: -1, limit: 128, max_bytes: 512 << 10, recent: true, ...params,
    }, options),
    clear: (expectedRevision?: string) => this.command('history.clear', expectedRevision === undefined ? {} : { expected_revision: expectedRevision }),
    rewind: (cut: number, expectedRevision: string) => this.command('history.rewind', { cut, expected_revision: expectedRevision }),
    compact: () => this.command('history.compact', {}),
  };
  readonly agents = {
    list: (options: CallOptions = {}) => this.query('agents.list', {}, options),
    inspect: (id: string, options: CallOptions = {}) => this.query('agent.transcript', { id }, options),
    submit: (id: string, input: string | SubmitPayload, delivery = 'queued') => this.command('agent.submit', { id, ...(typeof input === 'string' ? { text: input } : input), delivery }),
    cancelTurn: (id: string, turnId: string) => this.command('agent.turn.cancel', { id, turn_id: turnId }),
    control: (id: string) => this.command('agent.control', { id }),
    delete: (id: string) => this.command('agent.delete', { id }),
  };
  /** Human inspection does not acknowledge, deliver, or complete agent mail. */
  readonly mailbox = {
    list: (params: Partial<Omit<MailboxPageParams, 'root_id'>> = {}, options: CallOptions = {}) => this.client.call('mailbox.list', {
      root_id: this.rootId, agent_id: this.rootId, limit: 64, max_bytes: 256 << 10, ...params,
    }, options),
    read: (id: string, agentId = this.rootId, options: CallOptions = {}) => this.client.call('mailbox.read', {
      root_id: this.rootId, agent_id: agentId, id,
    }, options),
  };
  answerQuestion(id: string, answer: string[], dismissed = false) { return this.command('question.answer', { id, answer, dismissed }); }
  /** Answer a batched user.ask: one entry per question, null for skipped. */
  answerQuestions(id: string, answers: ({ answer: string[]; dismissed?: boolean } | null)[]) {
    return this.command('question.answer', {
      id, answer: [], dismissed: false,
      answers: answers.map((entry) => ({ answer: entry?.answer ?? [], dismissed: entry?.dismissed ?? !entry?.answer?.length })),
    });
  }
  terminalInput(id: string, bytes: Uint8Array, options: CallOptions = {}) {
    return this.invoke('terminal.input', { id, bytes: encodeBase64(bytes) }, options);
  }
}

export class Sessions {
  constructor(private readonly client: WhipClient) {}
  create(params: Pick<CreateSessionParams, 'cwd'> & Partial<Omit<CreateSessionParams, 'cwd'>>, options: CommandOptions = {}) {
    return this.client.submit('session.create', { kind: 'agent', model: '', provider: '', ...params }, options);
  }
  list(params: Partial<SessionCatalogParams> = {}, options: CallOptions = {}) { return this.client.call('sessions.list', { limit: 128, max_bytes: 512 << 10, ...params }, options); }
  /** Advisory metadata for up to 32 roots, without opening their views. Check missing before using counts. */
  summaries(rootIds: readonly string[], options: CallOptions = {}) { return this.client.call('sessions.summaries', { root_ids: [...rootIds] }, options); }
  open(rootId: string, options: CallOptions = {}) { return this.client.query('session.open', { id: rootId }, options); }
  delete(rootId: string, options: Omit<CommandOptions, 'rootId'> = {}) { return this.client.submit('session.delete', { root_id: rootId }, options); }
}
