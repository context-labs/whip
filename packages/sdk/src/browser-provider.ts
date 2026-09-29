import { assertValid } from '@whip/protocol';
import type { BrowserEvent, BrowserCommandResultParams, Operations } from '@whip/protocol';
import type { DuplexTransport } from './executors.js';
import { decodeResponse, operation, RemoteError } from './wire.js';
import type { CallOptions } from './wire.js';

type BrowserMethod = 'browser.provider.bind' | 'browser.provider.unbind' | 'browser.provider.event' | 'browser.command.result' | 'browser.screenshot.chunk' | 'browser.inventory.result';

/** One explicit provider connection. Offers describe human-owned pages; they do
 * not grant control. Only commands received on this peer authorize dispatch.
 * Disconnect revokes its controls without closing the human pages. */
export class BrowserProviderClient {
  private sequence = 0;
  private consumed = false;
  private constructor(private readonly transport: DuplexTransport<BrowserEvent>, readonly runtimeID: string, readonly processEpoch: string) {}

  static async connect(transport: DuplexTransport<BrowserEvent>, options: { expectedRuntimeID: string; expectedProcessEpoch: string } & CallOptions): Promise<BrowserProviderClient> {
    try {
      const params = { major: 4, expected_runtime_id: options.expectedRuntimeID, expected_process_epoch: options.expectedProcessEpoch };
      assertValid('InitializeParams', params);
      options.signal?.throwIfAborted();
      const response = await transport.request({ jsonrpc: '2.0', id: 'initialize', method: 'initialize', params }, options);
      const result = decodeResponse('initialize', 'initialize', response);
      if (result.runtime_id !== params.expected_runtime_id || result.process_epoch !== params.expected_process_epoch) throw new TypeError('Browser provider runtime identity or process generation mismatch');
      return new BrowserProviderClient(transport, result.runtime_id, result.process_epoch);
    } catch (error) {
      await transport.close();
      throw error;
    }
  }

  private async call<M extends BrowserMethod>(method: M, params: Operations[M]['params'], options: CallOptions): Promise<Operations[M]['result']> {
    options.signal?.throwIfAborted();
    assertValid(operation(method).params, params);
    const id = 'browser-' + ++this.sequence;
    try {
      const response = await this.transport.request({ jsonrpc: '2.0', id, method, params: structuredClone(params) }, options);
      return decodeResponse(method, id, response);
    } catch (error) {
      if (!(error instanceof RemoteError)) await this.transport.close();
      throw error;
    }
  }

  bind(params: Operations['browser.provider.bind']['params'], options: CallOptions = {}): Promise<Operations['browser.provider.bind']['result']> { return this.call('browser.provider.bind', params, options); }
  unbind(params: Operations['browser.provider.unbind']['params'], options: CallOptions = {}): Promise<Operations['browser.provider.unbind']['result']> { return this.call('browser.provider.unbind', params, options); }
  event(params: Operations['browser.provider.event']['params'], options: CallOptions = {}): Promise<Operations['browser.provider.event']['result']> { return this.call('browser.provider.event', params, options); }
  result(params: Operations['browser.command.result']['params'], options: CallOptions = {}): Promise<Operations['browser.command.result']['result']> { return this.call('browser.command.result', params, options); }
  inventory(params: Operations['browser.inventory.result']['params'], options: CallOptions = {}): Promise<Operations['browser.inventory.result']['result']> { return this.call('browser.inventory.result', params, options); }
  screenshotChunk(params: Operations['browser.screenshot.chunk']['params'], options: CallOptions = {}): Promise<Operations['browser.screenshot.chunk']['result']> { return this.call('browser.screenshot.chunk', params, options); }

  /** Upload only bytes produced by this exact pending screenshot command, then
   * acknowledge once. An uncertain upload or acknowledgement is never replayed. */
  async screenshotResult(result: Omit<BrowserCommandResultParams, 'screenshot'>, bytes: Uint8Array, options: CallOptions = {}): Promise<Operations['browser.command.result']['result']> {
    if (!(bytes instanceof Uint8Array) || bytes.length === 0 || bytes.length > (4 << 20)) throw new RangeError('Browser screenshot must contain 1..4194304 bytes');
    if (result.error !== undefined) throw new TypeError('A failed browser command cannot publish a screenshot');
    const captured = structuredClone(result);
    assertValid('BrowserCommandResultParams', captured);
    const snapshot = Uint8Array.from(bytes);
    const digest = Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256', snapshot)), value => value.toString(16).padStart(2, '0')).join('');
    const params = { ...captured, screenshot: { size: String(snapshot.length), digest, media_type: 'image/jpeg' as const } };
    assertValid('BrowserCommandResultParams', params);
    for (let offset = 0; offset < snapshot.length; offset += 64 << 10) {
      const chunk = snapshot.subarray(offset, offset + (64 << 10));
      let binary = '';
      for (const value of chunk) binary += String.fromCharCode(value);
      const ack = await this.screenshotChunk({ command_id: params.command_id, root_id: params.root_id, provider_epoch: params.provider_epoch, attachment_generation: params.attachment_generation, offset: String(offset), data_base64: btoa(binary) }, options);
      if (!ack.accepted) throw new TypeError('Browser screenshot chunk was not accepted');
    }
    return this.result(params, options);
  }

  /** Exactly one dispatcher consumes events. Returning closes the peer; there
   * is no event replay or automatic provider replacement. */
  async *events(): AsyncGenerator<BrowserEvent> {
    if (this.consumed) throw new TypeError('Browser provider events already have an owner');
    this.consumed = true;
    try {
      for await (const event of this.transport.events) {
        assertValid('BrowserEvent', event);
        yield structuredClone(event);
      }
    } finally { await this.transport.close(); }
  }
  close(): Promise<void> { return this.transport.close(); }
}
