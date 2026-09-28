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

These are initial Phase 1 declarations. Runtime handlers and SDK adoption arrive
in Phase 2. Current applications still use @whip/legacy-protocol; v4 does not
translate or accept that contract. Operation names in the manifest describe the
new surface and do not claim that handlers are already serving it.
