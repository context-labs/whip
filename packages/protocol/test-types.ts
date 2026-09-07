import {
  assertValid, rpcOperations, runtimeOperations,
  type InitializeParams, type SubscribeParams, type RpcMethods, type RpcMethod,
  type RuntimeOperations, type RuntimeOperation, type QueryOperation,
  type CommandOperation, type EphemeralOperation, type RootEvent,
} from './generated/index.js';

const initialize: InitializeParams = { protocol_major: 3, build_id: 'fixture', client_kind: 'human', client_id: 'browser' };
const subscription: SubscribeParams = { root_id: 'root', subscription_id: 'view', cursor: '9007199254740993' };
assertValid('InitializeParams', initialize);
assertValid('SubscribeParams', subscription);

// Instantiate the generated public metadata contract for every registry entry.
// RPCs and runtime operations retain separate classifications.
type Executions<T extends Record<keyof T, { execution: string }>> = { [K in keyof T]: T[K]['execution'] };
function executionOf<T extends Record<keyof T, { execution: string }>>(operations: T): Executions<T> {
  return Object.fromEntries(Object.entries(operations).map(([name, metadata]) => [name, (metadata as { execution: string }).execution])) as Executions<T>;
}
const rpcClassifications: Executions<RpcMethods> = executionOf(rpcOperations);
const runtimeClassifications: Executions<RuntimeOperations> = executionOf(runtimeOperations);
const decision: 'ephemeral' = rpcClassifications['permission.decide'];
const inspectMode: 'command' = runtimeClassifications['permission.mode'];
void [decision, inspectMode];

declare function rpc<K extends RpcMethod>(name: K, params: RpcMethods[K]['params']): Promise<RpcMethods[K]['result']>;
declare function query<K extends QueryOperation>(name: K, params: RuntimeOperations[K]['params']): Promise<RuntimeOperations[K]['result']>;
declare function submit<K extends CommandOperation>(name: K, params: RuntimeOperations[K]['params']): void;
declare function invoke<K extends EphemeralOperation>(name: K, params: RuntimeOperations[K]['params']): void;

rpc('command.status', { command_id: 'command' });
rpc('permission.decide', { decision: { command_id: 'decision', root_id: 'root', permission_id: 'permission', allow: true } });
submit('permission.mode', { external_permissions: true });
// @ts-expect-error Permission decisions no longer accept signing credentials.
rpc('permission.decide', { decision: { command_id: 'decision', root_id: 'root', permission_id: 'permission', allow: true }, signature: 'old-signature' });
// @ts-expect-error Client enrollment is not part of the trusted-client protocol.
rpc('identity.enroll', {});
// @ts-expect-error Permission mode is a runtime command, not a second RPC.
rpc('permission.mode', {});

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

// Bounded collections retain their concrete wire types, not unknown records.
function inspectCollection(page: RpcMethods['root.collection']['result']) {
  for (const entry of page.items ?? []) {
    if (entry.agent) { const id: string = entry.agent.id; void id; }
    if (entry.budget) { const limit: string = entry.budget.state.limit; void limit; }
    if (entry.blackboard) { const revision: string = entry.blackboard.version; void revision; }
    if (entry.body) { const ref: string = entry.body.reference_id; void ref; }
    // @ts-expect-error typed agent IDs are strings
    const invalid: number | undefined | null = entry.agent?.id;
    void invalid;
  }
}
void inspectCollection;
