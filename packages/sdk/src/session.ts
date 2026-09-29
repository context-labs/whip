import { assertValid } from '@whip/protocol';
import type { Input, Operations } from '@whip/protocol';
import type { Client } from './index.js';
import type { RecoveryJournal } from './command.js';
import type { CallOptions } from './wire.js';

type Params<M extends keyof Operations> = Operations[M]['params'];
type Scoped<M extends keyof Operations> = Omit<Params<M>, 'session_id'>;
type Page<M extends keyof Operations> = Omit<Scoped<M>, 'limit'> & { limit?: number };

/** One inert identity for a root or child. Constructing a handle performs no I/O
 * and owns no transcript, worker, configuration cache or execution lifetime. */
export class Session {
  constructor(readonly client: Client, readonly id: string) { assertValid('SessionParams', { session_id: id }); }
  async get(options: CallOptions = {}) {
    const value = await this.client.call('sessions.get', { session_id: this.id }, options);
    if (value.id !== this.id) throw new TypeError('Session identity mismatch');
    return value;
  }
  configure(expectedRevision: string, patch: Params<'sessions.configure'>['patch'], options: CallOptions = {}) {
    return this.client.call('sessions.configure', { session_id: this.id, expected_revision: expectedRevision, patch }, options);
  }
  lifecycle(lifecycle: Params<'sessions.lifecycle'>['lifecycle'], options: CallOptions = {}) { return this.client.call('sessions.lifecycle', { session_id: this.id, lifecycle }, options); }
  delete(options: CallOptions = {}) { return this.client.call('sessions.delete', { session_id: this.id }, options); }
  submit(parts: Params<'sessions.submit'>['parts'], requestID: string, options: CallOptions = {}) { return this.client.submit(this.id, parts, requestID, options); }
  /** Prepare a recoverable submission. Call send explicitly after preserving the handle/record. */
  submission(parts: Params<'sessions.submit'>['parts'], requestID: string, options: { journal?: RecoveryJournal } = {}) {
    return this.client.command('sessions.submit', { session_id: this.id, parts, source: 'user', identity: { client_id: this.client.clientID, request_id: requestID } }, options);
  }
  observe(options: Parameters<Client['observe']>[1] = {}) { return this.client.observe(this.id, options); }
  spawn(params: Omit<Params<'sessions.spawn'>, 'parent_id' | 'identity'>, requestID: string, options: CallOptions = {}) { return this.client.spawn({ ...params, parent_id: this.id }, requestID, options); }
  async cancelTurn(turnID: string, options: CallOptions = {}) {
    await this.turns.get(turnID, options);
    return this.client.call('turns.cancel', { turn_id: turnID }, options);
  }
  cancelInput(input: Input, options: CallOptions = {}) {
    assertValid('Input', input);
    if (input.session_id !== this.id) throw new TypeError('Input belongs to another session');
    return this.client.call('inputs.cancel', { input_id: input.id }, options);
  }
  readonly history = {
    page: (params: Page<'sessions.history'> = { after: '0' }, options: CallOptions = {}) => this.client.call('sessions.history', { limit: 100, ...params, session_id: this.id }, options),
    snapshot: (options: CallOptions = {}) => this.client.call('context.snapshot', { session_id: this.id }, options),
    rewind: (params: Omit<Params<'sessions.rewind'>, 'session_id' | 'edit_id'>, editID: string, options: CallOptions = {}) => this.client.rewind({ ...params, session_id: this.id }, editID, options),
    fork: (params: Omit<Params<'sessions.fork'>, 'session_id' | 'fork_id'>, forkID: string, options: CallOptions = {}) => this.client.fork({ ...params, session_id: this.id }, forkID, options),
    compact: (requestID: string, options: CallOptions = {}) => this.client.compact(this.id, requestID, options),
  };
  readonly questions = {
    list: (params: Page<'questions.list'> = {}, options: CallOptions = {}) => this.client.listQuestions({ limit: 50, ...params, session_id: this.id }, options),
    get: (operationID: string, options: CallOptions = {}) => this.client.getQuestion(this.id, operationID, options),
    answer: (operationID: string, answers: Params<'questions.answer'>['answers'], options: CallOptions = {}) => this.client.answerQuestion(this.id, operationID, answers, options),
  };
  readonly permissions = {
    list: (params: Page<'permissions.list'> = {}, options: CallOptions = {}) => this.client.call('permissions.list', { limit: 50, ...params, session_id: this.id }, options),
    policy: (options: CallOptions = {}) => this.client.getPermissionPolicy(this.id, options),
    setMode: (params: Omit<Params<'permissions.set_mode'>, 'session_id' | 'edit_id'>, editID: string, options: CallOptions = {}) => this.client.setPermissionMode({ ...params, session_id: this.id }, editID, options),
    resolve: async (operationID: string, approved: boolean, options: CallOptions = {}) => {
      await this.operations.get(operationID, options);
      return this.client.call('permissions.resolve', { operation_id: operationID, approved }, options);
    },
  };
  readonly operations = {
    get: async (operationID: string, options: CallOptions = {}) => {
      const result = await this.client.call('operations.get', { operation_id: operationID }, options);
      if (result.session_id !== this.id) throw new TypeError('Operation belongs to another session');
      return result;
    },
  };
  readonly turns = {
    get: async (turnID: string, options: CallOptions = {}) => {
      const result = await this.client.call('turns.get', { turn_id: turnID }, options);
      if (result.session_id !== this.id) throw new TypeError('Turn belongs to another session');
      return result;
    },
    output: async (turnID: string, options: CallOptions = {}) => {
      await this.turns.get(turnID, options);
      return this.client.call('turns.output', { turn_id: turnID }, options);
    },
  };
  readonly mail = {
    list: (params: Page<'mail.list'> = {}, options: CallOptions = {}) => this.client.call('mail.list', { limit: 50, ...params, session_id: this.id }, options),
    read: (mailID: string, options: CallOptions = {}) => this.client.call('mail.read', { session_id: this.id, mail_id: mailID }, options),
    send: (params: Omit<Params<'mail.send'>, 'sender_id' | 'mail_id'>, mailID: string, options: CallOptions = {}) => this.client.sendMail({ ...params, sender_id: this.id }, mailID, options),
  };
  readonly goals = {
    current: (options: CallOptions = {}) => this.client.call('goals.current', { session_id: this.id }, options),
    get: (goalID: string, options: CallOptions = {}) => this.client.call('goals.get', { session_id: this.id, goal_id: goalID }, options),
    create: (params: Omit<Params<'goals.create'>, 'session_id' | 'goal_id'>, goalID: string, options: CallOptions = {}) => this.client.createGoal({ ...params, session_id: this.id }, goalID, options),
    resume: (goal: Params<'goals.resume'>['goal'], requestID: string, options: CallOptions = {}) => this.client.resumeGoal(this.id, goal, requestID, options),
    cancel: (goalID: string, options: CallOptions = {}) => this.client.cancelGoal(this.id, goalID, options),
  };
  readonly schedules = {
    list: (params: Page<'schedules.list'> = {}, options: CallOptions = {}) => this.client.call('schedules.list', { limit: 50, ...params, session_id: this.id }, options),
    get: (scheduleID: string, options: CallOptions = {}) => this.client.call('schedules.get', { session_id: this.id, schedule_id: scheduleID }, options),
    create: (params: Omit<Params<'schedules.create'>, 'session_id' | 'schedule_id'>, scheduleID: string, options: CallOptions = {}) => this.client.createSchedule({ ...params, session_id: this.id }, scheduleID, options),
    cancel: (scheduleID: string, options: CallOptions = {}) => this.client.call('schedules.cancel', { session_id: this.id, schedule_id: scheduleID }, options),
  };
  readonly content = {
    read: (referenceID: string, options: CallOptions = {}) => this.client.call('content.read', { session_id: this.id, reference_id: referenceID }, options),
    put: (params: Scoped<'content.put'>, options: CallOptions = {}) => this.client.call('content.put', { ...params, session_id: this.id }, options),
  };
  readonly budgets = {
    list: (options: CallOptions = {}) => this.client.call('budgets.list', { session_id: this.id }, options),
    set: (params: Scoped<'budgets.set'>, options: CallOptions = {}) => this.client.call('budgets.set', { ...params, session_id: this.id }, options),
  };
  readonly resources = {
    list: (options: CallOptions = {}) => this.client.call('resources.list', { session_id: this.id }, options),
    set: (params: Scoped<'resources.set'>, options: CallOptions = {}) => this.client.call('resources.set', { ...params, session_id: this.id }, options),
  };
}
