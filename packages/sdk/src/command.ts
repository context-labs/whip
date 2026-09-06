import { assertValid, runtimeOperations, type CommandOperation, type CommandResult, type RuntimeOperations } from '@whip/protocol';
import type { WhipClient, CallOptions } from './client.js';
import { DeliveryUncertainError, RpcError, WhipError } from './errors.js';
import { frozen, uuid, withSignal } from './util.js';

export type CommandStatus = 'queued' | 'running' | 'waiting' | 'succeeded' | 'failed' | 'cancelled' | 'interrupted';
export type CommandOutcome<O extends CommandOperation = CommandOperation> = Omit<CommandResult, 'result' | 'status'> & {
  status: CommandStatus;
  result?: RuntimeOperations[O]['result'];
};
export interface CommandOptions { rootId?: string; commandId?: string }
export interface RecoveryRecord<O extends CommandOperation = CommandOperation> {
  readonly version: 1;
  readonly runtimeId: string;
  readonly clientId: string;
  readonly commandId: string;
  readonly operation: O;
  readonly rootId?: string;
}
/** Application-owned storage. These records deliberately contain no request body. */
export interface RecoveryStorage {
  list(): Promise<readonly RecoveryRecord[]>;
  put(record: RecoveryRecord): Promise<void>;
  delete(record: RecoveryRecord): Promise<void>;
}
export function isTerminal(status: string): boolean {
  return ['succeeded', 'failed', 'cancelled', 'interrupted'].includes(status);
}

export class CommandHandle<O extends CommandOperation = CommandOperation> {
  private initial?: Promise<CommandOutcome<O>>;
  private last?: CommandOutcome<O>;
  private knownAccepted = false;
  private encoded?: string;
  private retrying?: Promise<CommandOutcome<O>>;
  private constructor(readonly client: WhipClient, readonly record: RecoveryRecord<O>, payload?: RuntimeOperations[O]['params'], private readonly beforeSend?: () => Promise<unknown>) {
    if (payload !== undefined) {
      assertValid(runtimeOperations[record.operation].params_type, payload);
      this.encoded = JSON.stringify({
        command_id: record.commandId, operation: record.operation,
        scope: record.rootId ? 'root' : 'daemon',
        ...(record.rootId ? { root_id: record.rootId } : {}), payload,
      });
    }
  }
  static submit<O extends CommandOperation>(client: WhipClient, runtimeId: string, operation: O, payload: RuntimeOperations[O]['params'], options: CommandOptions, beforeSend?: () => Promise<unknown>): CommandHandle<O> {
    const record = frozen({ version: 1 as const, runtimeId, clientId: client.clientId, commandId: options.commandId ?? uuid(), operation, ...(options.rootId ? { rootId: options.rootId } : {}) });
    if (!record.commandId) throw new TypeError('commandId must not be empty');
    const handle = new CommandHandle(client, record, payload, beforeSend);
    handle.initial = handle.send();
    // A UI may render the handle before attaching a waiter; retain rejection for accepted().
    void handle.initial.catch(() => {});
    return handle;
  }
  static recover<O extends CommandOperation>(client: WhipClient, record: RecoveryRecord<O>, payload?: RuntimeOperations[O]['params']): CommandHandle<O> {
    return new CommandHandle(client, frozen({ ...record }), payload);
  }
  get commandId(): string { return this.record.commandId; }
  get operation(): O { return this.record.operation; }
  private observe(result: CommandOutcome<O>): CommandOutcome<O> {
    this.knownAccepted = true;
    this.last = result;
    if (isTerminal(result.status)) this.encoded = undefined;
    return result;
  }
  private async send(): Promise<CommandOutcome<O>> {
    await this.beforeSend?.();
    if (!this.encoded) throw new WhipError('recovery_required', 'Retry requires the original request payload');
    const info = this.client.requireConnected();
    if (info.runtime_id !== this.record.runtimeId) throw new WhipError('runtime_changed', 'Command belongs to another runtime');
    await this.client.recoveryStorage?.put(this.record);
    // Storage may be asynchronous. Recheck before the first byte can be sent.
    if (this.client.requireConnected().runtime_id !== this.record.runtimeId) throw new WhipError('runtime_changed', 'Runtime changed before submission');
    try {
      const result = await this.client.callEncoded('command.submit', this.encoded);
      return this.observe(this.client.commandOutcome(this.record, result));
    } catch (error) {
      if (error instanceof RpcError || error instanceof TypeError || error instanceof WhipError && ['resource_limit', 'unsupported_operation', 'invalid_arguments'].includes(error.kind)) throw error;
      throw new DeliveryUncertainError(this.commandId, error);
    }
  }
  /** Acceptance is committed input, not completion. Aborting only stops this waiter. */
  accepted(options: Pick<CallOptions, 'signal'> = {}): Promise<CommandOutcome<O>> {
    if (this.last) return withSignal(Promise.resolve(this.last), options.signal);
    return withSignal(this.initial ?? this.status(options), options.signal);
  }
  async status(options: CallOptions = {}): Promise<CommandOutcome<O>> {
    return this.observe(await this.client.commandStatus(this.record, options));
  }
  /** Resolves a structured terminal outcome; execution failures remain daemon outcomes. */
  async result(options: Pick<CallOptions, 'signal'> = {}): Promise<CommandOutcome<O>> {
    if (this.initial) {
      try { await withSignal(this.initial, options.signal); }
      catch (error) { if (!(error instanceof DeliveryUncertainError)) throw error; }
    }
    for (;;) {
      options.signal?.throwIfAborted();
      if (this.last && isTerminal(this.last.status)) return this.last;
      try {
        const result = await this.status(options);
        if (isTerminal(result.status)) return result;
      } catch (error) {
        if (!options.signal?.aborted && error instanceof WhipError && error.kind === 'resource_limit') {
          await this.client.waitForCommandTick(options.signal); continue;
        }
        if (options.signal?.aborted || error instanceof RpcError || this.client.getSnapshot().state !== 'reconnecting') throw error;
        await this.client.whenConnected(options.signal);
        continue;
      }
      await this.client.waitForCommandTick(options.signal);
    }
  }
  /** Explicit retry: identical identity and bytes, only after a definitive missing lookup. */
  retry(options: Pick<CallOptions, 'signal'> = {}): Promise<CommandOutcome<O>> {
    if (options.signal?.aborted) return Promise.reject(options.signal.reason);
    if (!this.retrying) {
      this.retrying = this.retryOnce().finally(() => { this.retrying = undefined; });
    }
    return withSignal(this.retrying, options.signal);
  }
  private async retryOnce(): Promise<CommandOutcome<O>> {
    if (this.initial) await this.initial.catch(error => { if (error instanceof RpcError) throw error; });
    try { return await this.status(); }
    catch (error) {
      if (!(error instanceof RpcError) || error.kind !== 'command_not_found') throw error;
      if (this.knownAccepted) throw new WhipError('recovery_required', 'Previously accepted command is missing; it must not be replayed');
      this.initial = this.send();
      return this.initial;
    }
  }
  cancel(): CommandHandle<'cancel'> {
    if (!this.record.rootId || !['submit', 'steer'].includes(this.operation)) throw new WhipError('unsupported_operation', 'This command has no command-targeted cancellation; use an explicit agent turn ID where supported');
    return CommandHandle.submit(this.client, this.record.runtimeId, 'cancel', { target_command_id: this.commandId }, { rootId: this.record.rootId }, async () => {
      try { await this.accepted(); }
      catch (error) { if (!(error instanceof DeliveryUncertainError)) throw error; await this.status(); }
    });
  }
}
