import { assertValid, manifest } from '@whip/protocol';
import type { Operations, Request, Response, RPCError } from '@whip/protocol';

export class RemoteError extends Error {
  readonly code: number;
  readonly kind: RPCError['kind'];
  constructor(error: RPCError) {
    super(error.message);
    this.name = 'RemoteError';
    this.code = error.code;
    this.kind = error.kind;
  }
}

/** A transport failure does not tell the caller whether a mutation committed. */
export class DeliveryError extends Error {
  constructor(message: string, options?: ErrorOptions) {
    super(message, options);
    this.name = 'DeliveryError';
  }
}

export type CallOptions = { signal?: AbortSignal };
export type Transport = (request: Request, expectedRuntimeID: string | undefined, options: CallOptions) => Promise<Response>;
export type Method = keyof Operations;

export function operation(method: Method) {
  const operation = manifest.operations.find(value => value.name === method);
  if (!operation) throw new TypeError('Unknown operation: ' + method);
  return operation;
}

export function decodeResponse<M extends Method>(method: M, id: string, response: unknown): Operations[M]['result'] {
  assertValid('Response', response);
  if (response.id !== id) throw new TypeError('Response identity mismatch');
  if (response.error) throw new RemoteError(response.error);
  assertValid(operation(method).result, response.result);
  return response.result as Operations[M]['result'];
}
