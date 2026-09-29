# Native protocol v4

The native Go runtime owns accepted work, configuration, credentials and durable
session facts. The CLI, terminal, ACP, MCP bridge, web, desktop and mobile clients
use its generated protocol directly. JSON-RPC envelopes use `"2.0"`; the current
Whip protocol is major **4**, minor **0**. Retired majors fail initialization.
There is no translation of old command IDs, snapshots, scratch or migrations.

This describes the redesign source. Draft integration, final deletion and
unverified product/platform acceptance remain tracked in the
[development record](backend-redesign-development.md).

## One executable contract

[Go DTOs](../internal/protocol/types.go) and the
[operation registry](../internal/protocol/schema.go) own wire types, constraints
and parameter/result mappings. `@whip/protocol` contains generated schemas,
TypeScript declarations and standalone validators. The handwritten
[native SDK](../packages/sdk/README.md) adds services, explicit request recovery
and bounded views; it does not define another protocol.

After changing Go DTOs, run from the repository root:

```sh
npm run generate -w @whip/protocol
npm run check -w @whip/protocol
```

Generation is the only writer of generated declarations. The check verifies Go/
TypeScript interchange, consumer types, drift and validation without runtime code
generation under the browser CSP. Do not edit generated files or retain a second
legacy schema to make a client compile.

Counters are canonical nonnegative signed-64-bit decimal **strings**, including
values above JavaScript's safe integer range. Preserve them exactly and use server
cursors rather than subtracting sequence numbers. Nullable values remain distinct
from known zero. Nil result collections may arrive as null; SDK readers normalize
them. Patch omission/null inherits, empty maps clear, and an output policy whose
schema is null explicitly clears structured output. Follow the DTO for each
operation instead of assuming one patch rule for arbitrary objects.

## Initialization and transport

[Initialization](../internal/protocol/types.go) names major 4 and may pin an
expected runtime ID and process epoch. The response includes the durable runtime
identity, current process epoch, negotiated major/minor, network marker and
immutable builtin definition references. A restarted process keeps its runtime
identity and changes epoch. Client replacement must verify those identities
before restoring observations or accepting an ephemeral handle.

The private Unix socket uses bounded newline-framed JSON-RPC. Ordinary connections
initialize before serial calls; browser-provider and custom-executor bindings use
explicit persistent duplex lifetimes. Frames are bounded at 8 MiB. The server admits
at most 64 peers, with 30-second idle reads, 5-second writes and 10-second ordinary
request handlers; it closes and joins connections on shutdown. See the
[RPC owner](../internal/rpc/server.go) for exact rules.

The gateway exposes `/api/v4/web` discovery and `/api/v4/ws`. Discovery reports
renderer availability; a build without packed assets does not claim a usable
application. Every upstream socket acknowledges `network_client: true` and the
pinned runtime/epoch. That restriction cannot be cleared by another handshake.
Human terminal methods and interactive shell input require the operator's explicit
network-terminal opt-in. Host stop remains network-restricted.

Exact Host/Origin checks are not user authentication. Non-loopback access requires
an explicitly trusted network or authenticated proxy. The gateway owns bounded
WebSockets and content transfers, closes hijacked connections on shutdown, and
does not follow a new host generation or replay effects. Foreground and managed
gateway lifetimes are described in [web setup](web-app.md).

## Identities, admission and outcomes

A tree identifies its root; every root and child has the same session shape and
service boundary. Roots have `parent_id: null`. Definitions are immutable
ID/revision pairs. Session creation resolves defaults; turns capture configuration
and history revisions. Provider credentials never appear in session configuration.

Use the stable identity required by each mutation: ordinary input request, tree
creation, history edit, grant, configuration revision or other typed operation.
Persist the exact original request before sending if the client needs durable
recovery. An identical retry resolves its recorded result; a changed payload under
the same identity conflicts. Not every mutation has an independent receipt query:
the SDK explicitly distinguishes verified payload, identity-only evidence and an
unavailable outcome.

A transport failure does not establish rejection. Local wait cancellation does
not cancel accepted work. Explicit `inputs.cancel`, `turns.cancel`, session stop,
subtree deletion and host shutdown have separate scopes. Accepted queued inputs
survive restart; claimed work is interrupted and uncertain effects are not replayed.
Turn failure, cancellation, interruption and unknown delivery remain distinct.
A successful admission is not proof of a successful turn.

JSON-RPC errors contain a validated code, message and typed `kind`; raw database,
credential and provider error bodies are not a public error API. Completed turn,
cell and operation failures belong to their canonical records. Applications render
errors at their owner as described in [frontend error ownership](frontend.md#error-ownership-and-canonical-displays).

## Observation and history

The native contract uses bounded authoritative reads and revision-aware
reconciliation. It has no retired root snapshot, global event log or
`sessions.title.changed` notification. `trees.catalog` exposes an invalidation
revision including off-page metadata; `trees.list`, `trees.recent` and
`trees.summaries` serve bounded catalog/navigation reads without opening workers.
SDK TreeCatalogView owns the catalog; the app invalidates its title/detail reads
when that revision changes.

SessionView observes exact root or child history/activity. ExecutionView reads
turns, cells and operations beside that transcript; TraceView retains a bounded
trace window. Live text, reasoning and stdout are provisional presentation and
cannot create durable messages. Committed records reconcile exact identities.
Imported history may have no local execution record; provenance does not grant
access to the source session.

History revision and captured boundaries prevent pages from mixing across rewind.
Gaps, truncation, stale cursors, missing evidence and retention overflow are
explicit. Loading older pages does not grant an unbounded transcript cache.
Suspension/disposal joins observers; it never cancels execution. Reconnecting to
the same runtime's new client does not resend a command or restore connection-bound
authority. See [SDK views and recovery](../packages/sdk/README.md).

## Content and native resources

A content digest names immutable bytes; a session-scoped reference grants access.
`content.put`, `content.get` and `content.read` use exact session/reference identity
and bounded values. The shared payload ceiling is 4 MiB. Gateway transfers validate
owner, length and digest and release their request resources on cancellation.
No global digest lookup or retained browser upload registry supplies authority.

Permissions refer to canonical pending operations. “Allow once” is not a standing
grant. Explicit standing grants name exact session/capability/resource; child
authority derives through a valid issuer chain. Revocation prevents future
admission, while an already pending operation keeps its separate decision and
revalidation. Questions, permission choices and saved permission mode have distinct
lifetimes and typed controls.

Human terminal handles carry the process epoch. Reads use bounded byte rings and
exact decimal offsets; writes are ephemeral and cannot be replayed after an
uncertain reply. List the current epoch to inspect an uncertain open. Browser
attachments and custom executors bind exact native connection epochs/leases;
disconnect revokes those bindings. Reconnect and checkpoint restoration do not
recreate browser, terminal, executor or permission authority.

## Service map and verification

The operation registry is the complete method reference. The
[domain guide](backend-domain.md) documents their ownership and transition rules:

- Sessions, tree creation, configuration, stop/resume/delete and history edits.
- Inputs, turns, cells, operations, attempts, context, usage and trace evidence.
- Goals, schedules, mail, explicit state, grants, budgets and shared resources.
- Workspace files/actions, language diagnostics, shell, terminal and scoped content.
- Host configuration, providers/accounts, MCP and native browser/computer services.
- Immutable definitions, captured modules/hooks and live executor bindings.

Contract drift/interchange checks and real-process SDK fixtures are required in
[the task gates](../Taskfile.yaml). Deterministic fixtures use temporary private
storage, actual native runtime/engines and controlled providers; client view tests
also cover bounded retention, stale ownership and uncertain acknowledgements.
These checks do not substitute for signed artifacts, actual remote SSH, physical
mobile, accessibility or real-provider evidence named in the redesign plan.
