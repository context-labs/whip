import {
  assertValid, rpcOperations, runtimeOperations,
  type InitializeParams, type SubscribeParams, type RpcMethods, type RpcMethod,
  type RuntimeOperations, type RuntimeOperation, type QueryOperation,
  type CommandOperation, type EphemeralOperation, type RootEvent,
} from './generated/index.js';

const initialize: InitializeParams = { protocol_major: 2, build_id: 'fixture', client_kind: 'human', client_id: 'browser' };
const subscription: SubscribeParams = { root_id: 'root', subscription_id: 'view', cursor: '9007199254740993' };
assertValid('InitializeParams', initialize);
assertValid('SubscribeParams', subscription);

// Instantiate the generated public metadata contract for every registry entry.
// Identically named methods on different surfaces retain separate classifications.
type Executions<T extends Record<keyof T, { execution: string }>> = { [K in keyof T]: T[K]['execution'] };
function executionOf<T extends Record<keyof T, { execution: string }>>(operations: T): Executions<T> {
  return Object.fromEntries(Object.entries(operations).map(([name, metadata]) => [name, (metadata as { execution: string }).execution])) as Executions<T>;
}
const rpcClassifications: Executions<RpcMethods> = executionOf(rpcOperations);
const runtimeClassifications: Executions<RuntimeOperations> = executionOf(runtimeOperations);
const signedMode: 'ephemeral' = rpcClassifications['permission.mode'];
const inspectMode: 'command' = runtimeClassifications['permission.mode'];
void [signedMode, inspectMode];

declare function rpc<K extends RpcMethod>(name: K, params: RpcMethods[K]['params']): Promise<RpcMethods[K]['result']>;
declare function query<K extends QueryOperation>(name: K, params: RuntimeOperations[K]['params']): Promise<RuntimeOperations[K]['result']>;
declare function submit<K extends CommandOperation>(name: K, params: RuntimeOperations[K]['params']): void;
declare function invoke<K extends EphemeralOperation>(name: K, params: RuntimeOperations[K]['params']): void;

rpc('command.status', { command_id: 'command' });
query('session.model.get', {});
submit('submit', { text: 'hello' });
invoke('terminal.input', { id: 'terminal', bytes: 'aGVsbG8=' });
// @ts-expect-error A query cannot enter the command journal.
submit('session.model.get', {});
// @ts-expect-error Terminal input cannot enter the command journal.
submit('terminal.input', { id: 'terminal', bytes: 'aGVsbG8=' });
// @ts-expect-error Runtime input uses named parameters.
submit('submit', 'hello');
// @ts-expect-error A durable command cannot be sent as an ephemeral operation.
invoke('submit', { text: 'hello' });

function inspectEvent(event: RootEvent): string {
  if (event.unknown) return event.kind;
  if (event.kind === 'stream.text' && !('content' in event.payload)) {
    return event.root_id + ':' + event.seq + ':' + (event.payload.text ?? '');
  }
  return event.kind;
}
void inspectEvent;
