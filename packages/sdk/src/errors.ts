import type { RPCError } from '@whip/protocol';

/** Errors here describe client/protocol failures, never a guessed execution outcome. */
export class WhipError extends Error {
  constructor(readonly kind: string, message: string, options?: ErrorOptions) {
    super(message, options);
    this.name = 'WhipError';
  }
}

export class RpcError extends WhipError {
  readonly code: number;
  constructor(readonly rpc: RPCError) {
    super(rpc.data?.kind ?? 'execution_failed', rpc.message);
    this.name = 'RpcError';
    this.code = rpc.code;
  }
}

export class DeliveryUncertainError extends WhipError {
  constructor(readonly commandId: string, cause?: unknown) {
    super('delivery_uncertain', 'Command acceptance is unknown; reconcile its original identity before retrying.', { cause });
    this.name = 'DeliveryUncertainError';
  }
}

export function asError(value: unknown): Error {
  return value instanceof Error ? value : new WhipError('client_error', String(value));
}

export function abortError(signal?: AbortSignal): Error {
  return signal?.reason instanceof Error ? signal.reason : new DOMException('Operation aborted', 'AbortError');
}
