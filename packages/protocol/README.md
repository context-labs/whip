# WHIP protocol contracts

This package contains generated TypeScript declarations, Draft-07 JSON Schema,
and Ajv validators for the Go protocol registry. It has no UI or transport state.
Use Node 24, the Go toolchain in `go.mod`, and Task. From the repository root:

```sh
npm ci
task generate
npm run check
```

`task generate` creates the schemas, manifest and Go-marshaled interoperability
fixtures in `schema/`, then JavaScript validators and TypeScript declarations in
`generated/`, and finally builds the SDK. Both output directories are ignored by
Git. `npm run check` verifies freshness, types and interoperability without
regenerating protocol output. To check freshness alone, run
`npm run check:drift -w @whip/protocol`.

```ts
import { assertValid, type SubscribeParams } from '@whip/protocol';

const params: SubscribeParams = {
  root_id: 'root-id',
  subscription_id: 'view-id',
  cursor: '9007199254740993',
};
assertValid('SubscribeParams', params);
```

The registry manifest records the request/result contract, execution ownership,
and existing permission requirements for each RPC and runtime operation.
Runtime operations travel inside `command.submit` or `query`; the latter never
journals work. A command's RPC request ID is separate from its durable command
ID. Client IDs provide retry namespaces and are not authenticated identities.
Clients and daemons must agree on the wire major; incompatible clients are
rejected at initialization.

Do not coerce decimal strings into JavaScript numbers. Binary fields use base64;
JSON payloads remain JSON. Permission decisions use ordinary unsigned payloads.
Never persist provider credentials or terminal input in a client
retry queue. A dropped connection does not cancel an accepted command.

Edit the definitions in `internal/protocol` or the generators rather than their
output. Rerun `task generate` after pulling, switching branches, or editing those
sources; rerun `npm ci` first when dependencies change. Ordinary builds use the
prepared files, and checks reject missing or stale output instead of rewriting it.
Package archives include the generated JavaScript and declarations, so consumers
do not need Go or generation tools. The daemon validates against the same Go
schema builder before admitting operations. Snapshot and command result
payloads must also be validated against the result type named in the registry.
