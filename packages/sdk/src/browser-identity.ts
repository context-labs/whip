import { assertValid } from '@whip/protocol';
import type { InitializeParams, Response } from '@whip/protocol';
import { decodeResponse } from './wire.js';

export type BrowserOptions = { expectedRuntimeID: string; expectedProcessEpoch?: string };

export function networkInitialize(params: InitializeParams, options: BrowserOptions): InitializeParams {
  if (params.expected_runtime_id !== undefined && params.expected_runtime_id !== options.expectedRuntimeID || params.expected_process_epoch !== undefined && params.expected_process_epoch !== options.expectedProcessEpoch) throw new TypeError('Runtime identity or process generation mismatch');
  const result = { ...params, network_client: true, expected_runtime_id: options.expectedRuntimeID, ...(options.expectedProcessEpoch ? { expected_process_epoch: options.expectedProcessEpoch } : {}) };
  assertValid('InitializeParams', result);
  return result;
}
export function checkNetworkInitialize(id: string, response: Response, options: BrowserOptions): void {
  const initial = decodeResponse('initialize', id, response);
  if (!initial.network_client || initial.runtime_id !== options.expectedRuntimeID || options.expectedProcessEpoch !== undefined && initial.process_epoch !== options.expectedProcessEpoch) throw new TypeError('Gateway runtime identity, process generation or network acknowledgement mismatch');
}

