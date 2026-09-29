import { assertValid } from '@whip/protocol';
import type { ExecutorEvent, ExecutorLease, Operations, Request, Response } from '@whip/protocol';
import { decodeResponse, operation, RemoteError } from './wire.js';
import type { CallOptions } from './wire.js';

/** A single persistent connection. Close/abort revokes its executor leases.
 * Implementations must bound frames, outstanding requests and unread events,
 * and must never reconnect, rebind or replay automatically. */
export interface DuplexTransport {
  request(request: Request, options: CallOptions): Promise<Response>;
  events: AsyncIterable<ExecutorEvent>;
  close(): Promise<void>;
}

type ExecutorMethod = 'executor.bind' | 'executor.pending' | 'tool.result' | 'hook.result' | 'tool.progress';

/** Explicit connection-owned execution. JSON payloads remain base64 bytes;
 * decoding them into JavaScript numbers is an application decision. Reading
 * pending calls never executes or acknowledges them. */
export class ExecutorClient {
  private sequence = 0;
  private consumed = false;
  private constructor(private readonly transport: DuplexTransport, readonly runtimeID: string) {}

  static async connect(transport: DuplexTransport, options: { expectedRuntimeID: string } & CallOptions): Promise<ExecutorClient> {
    try {
      options.signal?.throwIfAborted();
      const params = { major: 4, expected_runtime_id: options.expectedRuntimeID };
      assertValid('InitializeParams', params);
      const response = await transport.request({ jsonrpc: '2.0', id: 'initialize', method: 'initialize', params }, options);
      const result = decodeResponse('initialize', 'initialize', response);
      if (result.runtime_id !== options.expectedRuntimeID) throw new TypeError('Runtime identity mismatch');
      return new ExecutorClient(transport, result.runtime_id);
    } catch (error) {
      await transport.close();
      throw error;
    }
  }

  private async call<M extends ExecutorMethod>(method: M, params: Operations[M]['params'], options: CallOptions = {}): Promise<Operations[M]['result']> {
    options.signal?.throwIfAborted();
    assertValid(operation(method).params, params);
    const id = 'executor-' + ++this.sequence;
    try {
      const response = await this.transport.request({ jsonrpc: '2.0', id, method, params }, options);
      return decodeResponse(method, id, response);
    } catch (error) {
      if (!(error instanceof RemoteError)) await this.transport.close();
      throw error;
    }
  }

  bind(params: Operations['executor.bind']['params'], options: CallOptions = {}): Promise<ExecutorLease> {
    return this.call('executor.bind', params, options);
  }

  pending(params: Operations['executor.pending']['params'], options: CallOptions = {}): Promise<Operations['executor.pending']['result']> {
    return this.call('executor.pending', params, options);
  }

  result(params: Operations['tool.result']['params'], options: CallOptions = {}): Promise<Operations['tool.result']['result']> {
    return this.call('tool.result', params, options);
  }

  hookResult(params: Operations['hook.result']['params'], options: CallOptions = {}): Promise<Operations['hook.result']['result']> {
    return this.call('hook.result', params, options);
  }

  progress(params: Operations['tool.progress']['params'], options: CallOptions = {}): Promise<Operations['tool.progress']['result']> {
    return this.call('tool.progress', params, options);
  }

  /** One consumer owns handler dispatch and cancellation. No pending-list replay
   * or retry is hidden here. Closing this iterator closes the borrowed peer. */
  async *events(): AsyncGenerator<ExecutorEvent> {
    if (this.consumed) throw new TypeError('Executor events already have an owner');
    this.consumed = true;
    try {
      for await (const event of this.transport.events) {
        assertValid('ExecutorEvent', event);
        if (event.method === 'executor.invoke') {
          const call = event.invocation;
          if (!call || call.invocation_id !== event.invocation_id || call.lease.epoch !== event.epoch || call.lease.generation !== event.generation) throw new TypeError('Executor invocation identity mismatch');
        } else if (event.invocation !== null) {
          throw new TypeError('Executor cancellation contains an invocation');
        }
        yield structuredClone(event);
      }
    } finally {
      await this.transport.close();
    }
  }

  close(): Promise<void> { return this.transport.close(); }
}
