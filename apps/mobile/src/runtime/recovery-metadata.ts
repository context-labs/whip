import { assertValid, manifest } from '@whip/protocol';
import type { RecoveryRecord } from '@whip/sdk';

export const mobileDurableMethods = ['trees.create', 'sessions.submit', 'sessions.compact', 'sessions.spawn', 'sessions.fork', 'sessions.rewind', 'permissions.set_mode', 'inputs.steer'] as const;
export type MobileDurableMethod = typeof mobileDurableMethods[number];
export interface RecoveryMetadata {
  version: 4;
  runtimeId: string;
  clientId: string;
  commandId: string;
  operation: MobileDurableMethod;
  requestHash: string;
  rootId?: string;
  sessionId?: string;
  inputId?: string;
  turnId?: string;
}
export interface RecoveryIntent {
  draftKey?: string;
  draftRevision?: string;
  workflowId?: string;
  step?: string;
}
export interface StoredMetadata {
  record: RecoveryMetadata;
  intent: RecoveryIntent;
  knownAccepted: boolean;
}
export interface MetadataStorage {
  list(): Promise<StoredMetadata[]>;
  put(value: StoredMetadata): Promise<void>;
  accept(record: RecoveryMetadata): Promise<void>;
  delete(record: RecoveryMetadata): Promise<void>;
}
export function metadataKey(record: RecoveryMetadata): string {
  const scope = ['sessions.submit', 'sessions.compact', 'sessions.spawn'].includes(record.operation) ? 'receipt' : record.operation;
  return JSON.stringify([record.runtimeId, record.clientId, scope, record.commandId]);
}
const fields = ['version', 'runtimeId', 'clientId', 'commandId', 'operation', 'requestHash', 'rootId', 'sessionId', 'inputId', 'turnId'] as const;
const intentFields = ['draftKey', 'draftRevision', 'workflowId', 'step'] as const;
function id(value: unknown): asserts value is string {
  if (typeof value !== 'string' || !value || new TextEncoder().encode(value).byteLength > 1024) throw new TypeError('Invalid mobile recovery identity');
}
/** This is a metadata contract, deliberately distinct from the SDK's optional
 * full-request journal. Unknown properties are rejected, never persisted. */
export function validateMetadata(value: StoredMetadata): StoredMetadata {
  if (!value || typeof value !== 'object' || Object.keys(value).sort().join() !== 'intent,knownAccepted,record' || typeof value.knownAccepted !== 'boolean') throw new TypeError('Invalid mobile recovery envelope');
  const { record, intent } = value;
  if (!record || record.version !== 4 || Object.keys(record).some(field => !(fields as readonly string[]).includes(field)) || !mobileDurableMethods.includes(record.operation)) throw new TypeError('Unsupported mobile recovery metadata');
  for (const field of ['runtimeId', 'clientId', 'commandId'] as const) id(record[field]);
  for (const field of ['rootId', 'sessionId', 'inputId', 'turnId'] as const) if (record[field] !== undefined) id(record[field]);
  if (typeof record.requestHash !== 'string' || !/^[a-f0-9]{64}$/.test(record.requestHash)) throw new TypeError('Invalid mobile recovery fingerprint');
  if (!intent || typeof intent !== 'object' || Array.isArray(intent) || Object.keys(intent).some(field => !(intentFields as readonly string[]).includes(field))) throw new TypeError('Invalid mobile recovery intent');
  for (const field of intentFields) if (intent[field] !== undefined) id(intent[field]);
  if (record.operation !== 'trees.create' && !record.sessionId) throw new TypeError('Missing mobile recovery recipient');
  if (record.operation === 'inputs.steer' && (!record.inputId || !record.turnId)) throw new TypeError('Missing mobile steering identity');
  const canonical = Object.fromEntries(fields.filter(field => record[field] !== undefined).map(field => [field, record[field]])) as unknown as RecoveryMetadata;
  const correlation = Object.fromEntries(intentFields.filter(field => intent[field] !== undefined).map(field => [field, intent[field]]));
  return { record: canonical, intent: correlation, knownAccepted: value.knownAccepted };
}

/** Hash is local correlation only; it must never be compared to a host receipt
 * digest or interpreted as proof that a similarly named request was accepted. */
export async function projectRecovery(record: RecoveryRecord, hash: (request: string) => Promise<string>, options: { rootId?: string; intent?: RecoveryIntent } = {}): Promise<StoredMetadata> {
  record = structuredClone(record);
  options = structuredClone(options);
  const request = JSON.parse(record.request) as { method: MobileDurableMethod; params: Record<string, unknown> };
  if (record.namespace !== 'whip.v4.commands' || record.version !== 1 || !mobileDurableMethods.includes(request.method)) throw new TypeError('Unsupported mobile durable command');
  const operation = manifest.operations.find(item => item.name === request.method)!;
  assertValid(operation.params, request.params);
  const params = request.params as Record<string, unknown>;
  const identity = params.identity as { client_id: string; request_id: string } | undefined;
  if (identity && identity.client_id !== record.clientID) throw new TypeError('Recovery client identity mismatch');
  const commandId = identity?.request_id ?? params.creation_id ?? params.fork_id ?? params.edit_id;
  const sessionId = params.session_id ?? params.parent_id;
  return validateMetadata({
    record: { version: 4, runtimeId: record.runtimeID, clientId: record.clientID, commandId: commandId as string, operation: request.method,
      requestHash: await hash(record.request), ...(options.rootId ? { rootId: options.rootId } : {}), ...(sessionId ? { sessionId: sessionId as string } : {}),
      ...(request.method === 'inputs.steer' ? { inputId: params.input_id as string, turnId: params.turn_id as string } : {}),
    }, intent: options.intent ?? {}, knownAccepted: record.accepted,
  });
}
