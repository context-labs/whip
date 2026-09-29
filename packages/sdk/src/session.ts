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
  async activity(options: CallOptions = {}) {
    const value = await this.client.call('sessions.activity', { session_id: this.id }, options);
    if (value.session_id !== this.id || value.active_turn && value.active_turn.session_id !== this.id) throw new TypeError('Activity belongs to another session');
    return value;
  }
  readonly inputs = {
    page: async (params: Page<'inputs.page'> = { state: 'queued' }, options: CallOptions = {}) => {
      const value = await this.client.call('inputs.page', { limit: 50, ...params, session_id: this.id }, options);
      const items = value.items ?? [];
      let previous = params.after ?? '0';
      for (const item of items) {
        if (item.session_id !== this.id || BigInt(item.ordinal) <= BigInt(previous)) throw new TypeError('Input page scope or ordering mismatch');
        previous = item.ordinal;
      }
      if (value.next_cursor !== null && (!items.length || value.next_cursor !== previous)) throw new TypeError('Input page continuation mismatch');
      return { ...value, items };
    },
    get: async (inputID: string, options: CallOptions = {}) => {
      const value = await this.client.call('inputs.get', { session_id: this.id, input_id: inputID }, options);
      if (value.id !== inputID || value.session_id !== this.id) throw new TypeError('Input belongs to another session');
      return value;
    },
    cancel: async (inputID: string, options: CallOptions = {}) => this.cancelInput(await this.inputs.get(inputID, options), options),
    /** Explicitly promote a queued input to this exact active turn. */
    steer: (inputID: string, turnID: string, editID: string, options: CallOptions = {}) => this.client.call('inputs.steer', { session_id: this.id, input_id: inputID, turn_id: turnID, edit_id: editID }, options),
    steering: async (editID: string, options: CallOptions = {}) => {
      const value = await this.client.call('inputs.steering', { session_id: this.id, edit_id: editID }, options);
      if (value.id !== editID || value.session_id !== this.id || value.input && (value.input.session_id !== this.id || value.input.id !== value.input_id)) throw new TypeError('Steering receipt belongs to another input');
      return value;
    },
    promotion: (inputID: string, turnID: string, editID: string, options: { journal?: RecoveryJournal } = {}) => this.client.command('inputs.steer', { session_id: this.id, input_id: inputID, turn_id: turnID, edit_id: editID }, options),
  };
  configure(expectedRevision: string, patch: Params<'sessions.configure'>['patch'], options: CallOptions = {}) {
    return this.client.call('sessions.configure', { session_id: this.id, expected_revision: expectedRevision, patch }, options);
  }
  lifecycle(lifecycle: Params<'sessions.lifecycle'>['lifecycle'], options: CallOptions = {}) { return this.client.call('sessions.lifecycle', { session_id: this.id, lifecycle }, options); }
  delete(options: CallOptions = {}) { return this.client.call('sessions.delete', { session_id: this.id }, options); }
  submit(parts: Params<'sessions.submit'>['parts'], requestID: string, options: Parameters<Client['submit']>[3] = {}) { return this.client.submit(this.id, parts, requestID, options); }
  /** Prepare a recoverable submission. Call send explicitly after preserving the handle/record. */
  submission(parts: Params<'sessions.submit'>['parts'], requestID: string, options: { journal?: RecoveryJournal; designContext?: Params<'sessions.submit'>['design_context']; delivery?: 'queued' | 'steer'; targetTurnID?: string } = {}) {
    const { designContext, delivery, targetTurnID, ...commandOptions } = options;
    return this.client.command('sessions.submit', { session_id: this.id, parts, source: 'user', identity: { client_id: this.client.clientID, request_id: requestID }, ...(designContext === undefined ? {} : { design_context: designContext }), ...(delivery === undefined ? {} : { delivery }), ...(targetTurnID === undefined ? {} : { target_turn_id: targetTurnID }) }, commandOptions);
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
    page: (params: Page<'sessions.history_page'> = { direction: 'backward' }, options: CallOptions = {}) => this.client.call('sessions.history_page', { limit: 100, ...params, session_id: this.id }, options),
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
