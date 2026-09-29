import { assertValid } from '@whip/protocol';
import type { ExecutorLease, Operations } from '@whip/protocol';
import { ExecutorClient } from './executors.js';
import { AgentSession, decodeJSON } from './agents.js';
import type { AgentDefinition, AgentRuntime, Agents, HookContext, HookName, HookResult, Invocation, ServeOptions, ToolContext } from './agents.js';
import { formatIssues, validateWith } from './schema.js';
import { boundedInteger, utf8Base64 } from './value.js';
import { RemoteError } from './wire.js';
import type { CallOptions } from './wire.js';
const settled = (error: unknown) => error instanceof RemoteError && error.kind === 'CONFLICT';
const failureText = (error: unknown) => (error instanceof Error ? error.message : String(error)).replaceAll('\0', '').slice(0, 256) || 'Handler failed';
const object = (value: unknown): value is Record<string, unknown> => value !== null && typeof value === 'object' && !Array.isArray(value);
function encode(value: unknown): string {
  const text = JSON.stringify(value, (_key, item: unknown) => {
    if (typeof item === 'number' && (!Number.isFinite(item) || Number.isInteger(item) && !Number.isSafeInteger(item))) throw new TypeError('Typed JSON contains an unsafe number');
    return item;
  });
  if (text === undefined || new TextEncoder().encode(text).length > (512 << 10)) throw new RangeError('Handler result exceeds the JSON byte bound');
  return utf8Base64(text);
}
function matches(left: { id: string; revision: string } | null, right: { id: string; revision: string }) { return left?.id === right.id && left.revision === right.revision; }

/** One peer owns one immutable definition lease. No callback queue, reconnect,
 * rebinding or pending replay; seen IDs are never evicted within this lease. */
export async function serve<Output>(agents: Agents, agent: AgentDefinition<Output>, options: ServeOptions): Promise<AgentRuntime<Output>> {
  let maxConcurrent: number, maxInvocations: number;
  const tools = Object.keys(agent.handlers), hooks = Object.keys(agent.hooks) as HookName[];
  let peer: ExecutorClient | undefined;
  let lease: ExecutorLease;
  try {
    maxConcurrent = boundedInteger(options.maxConcurrent ?? 16, 'maxConcurrent', 128);
    maxInvocations = boundedInteger(options.maxInvocations ?? 4096, 'maxInvocations', 65_536);
    if (!tools.length && !hooks.length) throw new TypeError('Agent declares no handlers to serve');
    options.signal?.throwIfAborted();
    const callOptions = { signal: options.signal };
    const registered = await agents.register(agent, callOptions);
    peer = await ExecutorClient.connect(options.transport, { expectedRuntimeID: agents.client.runtimeID, signal: options.signal });
    lease = await peer.bind({ definition: registered.ref, tools, hooks: hooks as Operations['executor.bind']['params']['hooks'] }, callOptions);
    if (!matches(lease.definition, registered.ref) || tools.some(name => !lease.tools.includes(name)) || hooks.some(name => !(lease.hooks as string[]).includes(name))) throw new TypeError('Executor binding does not match the authored definition');
    options.signal?.throwIfAborted();
  } catch (error) { await options.transport.close(); throw error; }
  const executor = peer;
  const seen = new Set<string>();
  const running = new Map<string, { controller: AbortController; done: Promise<void> }>();
  let closed = false, failure: unknown;
  let closing: Promise<void> | undefined;
  const requestClose = (reason?: unknown) => {
    if (reason !== undefined && failure === undefined) failure = reason;
    if (closed) return;
    closed = true;
    for (const job of running.values()) job.controller.abort(reason ?? new Error('Executor closed'));
    closing = Promise.resolve().then(() => executor.close());
    void closing.catch(error => { failure ??= error; });
  };
  const abort = () => requestClose(options.signal?.reason ?? new Error('Executor aborted'));
  options.signal?.addEventListener('abort', abort, { once: true });
  if (options.signal?.aborted) abort();
  const identity = (invocation: Invocation) => ({ epoch: lease.epoch, generation: lease.generation, invocation_id: invocation.invocation_id });
  const run = async (invocation: Invocation, controller: AbortController) => {
    const deadline = Number(invocation.deadline_millis);
    if (!Number.isSafeInteger(deadline)) throw new TypeError('Unsafe handler deadline');
    const timer = setTimeout(() => controller.abort(new Error('Invocation deadline passed')), Math.min(2_147_483_647, Math.max(0, deadline - Date.now())));
    let progress: Promise<void> | undefined;
    const context: ToolContext & HookContext = Object.freeze({ invocationID: invocation.invocation_id, sessionID: invocation.session_id, turnID: invocation.turn_id, operationID: invocation.operation_id,
      origin: invocation.origin, deadline, signal: controller.signal, permissionMode: invocation.permission_mode,
      progress: (text: string) => {
        controller.signal.throwIfAborted();
        if (progress) return Promise.reject(new RangeError('Await the previous progress acknowledgement'));
        if (typeof text !== 'string' || new TextEncoder().encode(text).length > 2048 || text.includes('\0')) return Promise.reject(new RangeError('Progress text exceeds bounds'));
        progress = executor.progress({ ...identity(invocation), text }).then(() => {}).catch(error => { if (settled(error)) controller.abort(error); else requestClose(error); throw error; }).finally(() => { progress = undefined; });
        void progress.catch(() => {});
        return progress;
      },
    });
    const toolResult = (output: unknown, error = ''): Operations['tool.result']['params'] => ({ ...identity(invocation), output_base64: error ? null : encode(output === undefined ? null : output), failure: error });
    const hookResult = (value: HookResult = {}, error = ''): Operations['hook.result']['params'] => ({ ...identity(invocation), decision: value.decision ?? '', reason: value.reason ?? '', context: value.context ?? '',
      arguments_base64: value.arguments === undefined ? null : encode(value.arguments), spawn_base64: value.spawn === undefined ? null : encode(value.spawn), failure: error });
    let toolReply: Operations['tool.result']['params'] | undefined;
    let hookReply: Operations['hook.result']['params'] | undefined;
    try {
      try {
        controller.signal.throwIfAborted();
        if (deadline <= Date.now()) throw new Error('Invocation deadline passed');
        if (invocation.kind === 'tool') {
          const tool = agent.handlers[invocation.name];
          if (!tool || !invocation.arguments_base64) throw new TypeError('Missing tool handler or input');
          const input = await validateWith(tool.input, decodeJSON(invocation.arguments_base64));
          if (input.issues) throw new TypeError('Tool input rejected: ' + formatIssues(input.issues));
          controller.signal.throwIfAborted();
          const output = await tool.execute(input.value, context);
          controller.signal.throwIfAborted();
          const checked = await validateWith(tool.output, output);
          if (checked.issues) throw new TypeError('Tool output rejected after handler ran: ' + formatIssues(checked.issues));
          toolReply = toolResult(checked.value);
          assertValid('ExecutorToolResultParams', toolReply);
        } else {
          const hook = agent.hooks[invocation.name as HookName];
          if (!hook) throw new TypeError('Missing hook handler');
          let event: unknown;
          if (invocation.name === 'before_tool') {
            const args = invocation.arguments_base64 && decodeJSON(invocation.arguments_base64);
            if (!object(args)) throw new TypeError('Hook arguments must be an object');
            event = { ...context, operation: invocation.operation, arguments: args };
          } else if (invocation.name === 'before_spawn') {
            const spawn = invocation.spawn_base64 && decodeJSON(invocation.spawn_base64);
            if (!object(spawn) || !object(spawn.request) || !object(spawn.resolved)) throw new TypeError('Invalid spawn preview');
            event = { ...context, spawn };
          } else if (invocation.name === 'turn_start') event = { ...context, input: invocation.input_preview };
          else throw new TypeError('Unknown hook');
          const result = await hook(event as never);
          controller.signal.throwIfAborted();
          hookReply = hookResult(result || {});
          assertValid('ExecutorHookResultParams', hookReply);
        }
      } catch (error) {
        if (!controller.signal.aborted) {
          if (invocation.kind === 'tool') toolReply = toolResult(undefined, failureText(error));
          else hookReply = hookResult({}, failureText(error));
        }
      }
      await progress;
      // Delivery is outside the callback-error catch: an uncertain result must
      // never trigger another result or another invocation of the handler.
      if (!controller.signal.aborted) {
        try {
          if (toolReply) await executor.result(toolReply);
          else if (hookReply) await executor.hookResult(hookReply);
        } catch (error) {
          // Cancellation/timeout may win at the host before its notification
          // reaches this peer. This call is closed; other handlers keep running.
          if (!settled(error)) throw error;
          controller.abort(error);
        }
      }
    } catch (error) {
      if (!settled(error) || !controller.signal.aborted) throw error;
    } finally { clearTimeout(timer); }
  };
  const done = (async () => {
    try {
      for await (const event of executor.events()) {
        if (closed) break;
        if (event.epoch !== lease.epoch || event.generation !== lease.generation) throw new TypeError('Foreign executor event');
        if (event.method === 'executor.cancel') { running.get(event.invocation_id)?.controller.abort(new Error('Invocation cancelled')); continue; }
        const invocation = event.invocation!;
        if (!matches(invocation.lease.definition, lease.definition)) throw new TypeError('Foreign definition invocation');
        if (seen.has(invocation.invocation_id)) throw new TypeError('Repeated invocation; callback will not run again');
        if (seen.size >= maxInvocations) throw new RangeError('Executor lifetime invocation limit reached; start a new explicit lease');
        if (running.size >= maxConcurrent) throw new RangeError('Executor handler capacity reached');
        seen.add(invocation.invocation_id);
        const controller = new AbortController();
        const job = Promise.resolve().then(() => run(invocation, controller)).catch(error => requestClose(error)).finally(() => { running.delete(invocation.invocation_id); });
        running.set(invocation.invocation_id, { controller, done: job });
      }
    } catch (error) { if (!closed) requestClose(error); }
    finally {
      requestClose();
      options.signal?.removeEventListener('abort', abort);
      await closing?.catch(() => {});
      await Promise.allSettled([...running.values()].map(job => job.done));
      seen.clear();
    }
    if (failure !== undefined) throw failure;
  })();
  void done.catch(() => {}); // done remains observable without an unhandled rejection.
  const pinned = async (id: string, callOptions: CallOptions = {}) => {
    const session = new AgentSession<Output>(agents.client, id, agent.output);
    const current = await session.get(callOptions);
    if (!matches(current.definition, lease.definition) || tools.length && !matches(current.configuration.tools_definition, lease.definition) || hooks.length && !matches(current.configuration.hooks_definition, lease.definition)) throw new TypeError('Session is not pinned to this served definition');
    return session;
  };
  return { definition: Object.freeze({ ...lease.definition }), generation: lease.generation, get active() { return running.size; }, done,
    close: async () => { requestClose(); await done; },
    sessions: { open: pinned, create: async (params, callOptions = {}) => {
      if (closed) throw new Error('Executor is closed; a new session needs a live explicit lease');
      const created = await agents.client.call('trees.create', { ...params, definition: lease.definition }, callOptions);
      if (!created.root || created.deleted) throw new TypeError('Creation receipt refers to a deleted session');
      return pinned(created.root.id, callOptions);
    } },
  };
}
