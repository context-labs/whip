# WHIP protocol v5 contracts

This package contains generated TypeScript declarations, Draft-07 JSON Schema,
and Ajv validators for the Go protocol registry. It has no UI or transport state.
Use Node 24 and `npm ci`; regenerate with `npm run generate` and verify types,
Go-produced interoperability fixtures, and drift with `npm run check`.

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
The wire major is 5. Required archive metadata and catalog cursor status are
incompatible with earlier majors; clients and daemons reject them at initialization.

Do not coerce decimal strings into JavaScript numbers. Binary fields use base64;
JSON payloads remain JSON. Permission decisions use ordinary unsigned payloads.
Never persist provider credentials or terminal input in a client
retry queue. A dropped connection does not cancel an accepted command.

Generated files are checked in for consumers. Edit `internal/protocol` rather
than these files, then regenerate. The daemon validates against the same Go
schema builder before admitting operations. Snapshot and command result
payloads must also be validated against the result type named in the registry.
