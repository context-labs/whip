# WHIP v4 contract

Go DTOs in internal/protocol own these generated schemas, TypeScript declarations,
and standalone validators. Run npm run generate in this workspace; npm run check
checks types, Go/TypeScript interchange, CSP-safe validation and generation drift.

Counters are nonnegative int64 decimal strings. Root and child sessions use the
same shape; a root has parent_id: null. Patches replace whole fields: omission or
null inherits, empty maps clear, and output: { schema: null } clears structured
output. Configuration contains logical model names, never provider credentials.
Result collections can be null when the Go value is nil; SDK readers normalize
them to empty collections. Patch null remains distinct from an empty map.

The native runtime, Go client, SDK and supported product clients use this
contract directly. The runtime validates requests and responses against the
same registry; browser validators contain no runtime code generation and work
under the production CSP. Major 4 peers reject retired contracts. Fresh native
storage and client recovery namespaces do not translate retired identities.

From the repository root, use `npm run generate -w @whip/protocol` after changing
Go DTOs, then `npm run check -w @whip/protocol`. Do not edit generated declarations
or add a parallel schema. See [the native domain](../../docs/backend-domain.md)
for ownership and [the SDK](../sdk/README.md) for services, bounded views and
recovery. The development record tracks remaining client acceptance and final
retired-core deletion; protocol adoption alone does not complete those gates.
