# Concurrency and ownership

The native Go runtime separates durable ordering from independent execution.
SQLite transactions admit and settle work; a bounded scheduler owns live turns.
Roots and children use the same session path. There is no root actor or client
stream reducer that owns a second copy of durable session state.

This guide describes the redesign source, not an installed release. The
[domain contract](backend-domain.md) owns detailed transition rules and limits;
[frontend architecture](frontend.md) owns renderer state and retention budgets.
The [development record](backend-redesign-development.md) records validation and
remaining cutover work.

## Runtime and turn lifetimes

[`runtime.Open`](../internal/runtime/runtime.go) acquires exclusive ownership of
its private directory and opens configuration/storage. It does not start work.
`Start` performs crash recovery under that ownership and launches the scheduler.
One claimed session has at most one active turn, enforced by SQL as well as the
live worker registry. Provider calls, filesystem work and other external effects
run outside database transactions and the scheduler lock.

A turn captures its configuration and history revision. A later model or policy
edit affects later admission; it does not rewrite an active provider request.
Each session kernel has one serialized owner. Independent sessions can progress
within worker, active-turn and ancestor resource limits. A boundary wait releases
its execution permit only after owned attempts, cells and operations settle;
resumption reacquires permission before guest execution continues. Durable queued
work remains authoritative while a session waits for capacity.

Shutdown cancels live work, closes owned process/service resources and joins
workers before releasing storage and the execution lock. Resources that can
block a worker are closed as part of shutdown, not left for that worker to finish
indefinitely. A successful subtree deletion also retires its kernels. Exact turn,
history and process identities keep late completions from reviving retired work.
See [runtime lifecycle](../internal/runtime/runtime.go),
[capacity](../internal/runtime/capacity.go) and
[resource rules](backend-domain.md#reusable-resource-capacity).

## Admission, settlement and recovery

A durable request carries a stable client/request identity and exact payload.
Admission records its receipt and required input atomically. An identical retry
resolves that request; changed bytes under its identity conflict. Losing the
transport acknowledgement does not establish rejection. A request context owns
admission or observation, while the runtime owns accepted execution.

After restart, unclaimed inputs remain queued. Claimed work and uncertain live
effects are interrupted, not automatically replayed. Completed messages, cells,
operations and accounting evidence remain readable. Provider attempts are
recorded independently from transcript settlement: retrying a persistence write
must never dispatch the provider again. Bounded settlement failure faults the
runtime and leaves recovery evidence intact.

Cancelling a local read or wait stops that observation only. Input cancellation,
turn cancellation, session stop, subtree deletion and host shutdown are distinct
operations with explicit scopes. Session stop retains queued input for possible
resume. Rewind requires a stopped owner with no active turn or uncancelled queue,
compares revision/tail and retires whole history groups. Fork creates a new tree
from a captured prefix. Neither action restores external files, spending, browser
ownership or an uncertain effect. See
[atomic transitions](backend-domain.md#atomic-transitions) and
[history editing](backend-domain.md#raw-history-and-selected-model-context).

## Canonical history and live observations

Immutable message bodies, groups and exact sequence identities belong to the
store. History pages carry bounded revision-aware evidence; a stale cursor after
rewind fails instead of combining incompatible histories. A retained transcript
is not the model's selected context, and imported provenance grants no source
session access.

Text, reasoning and live cell output are bounded provisional observations. Their
loss or replacement cannot execute work or change canonical history. Committed
records replace matching provisional evidence by identity. Usage is derived from
the complete attempt ledger, never by adding only the messages a client retained.

Instructions are assembled once for an ordinary turn under its captured policy.
The manifest records applied source identity, scope, digest and byte counts.
Inspection reads captured evidence; it does not run instruction hooks or rescan
files and claim that new bytes were already applied. See
[instruction capture](backend-domain.md#instruction-capture).

## Host operations and resource owners

Authorization and resource capacity are checked at the operation boundary.
Roots and children cannot widen an issuer's grant or ancestor allowance.
Permission waiting, approval, revocation and an operation's final outcome remain
separate facts. A standing grant is explicit authority for future admissions;
it does not silently answer an already pending approval.

Workspace operations use confined paths and their mutation coordinator. MCP
connections, shell jobs, language servers, model helpers and custom executors
have explicit owners, queues and cancellation. A transmitted effect with an
unknown outcome is never retried automatically. Connection-bound executor loss
revokes that binding; reconnect does not recreate it. Detailed rules are in
[host operations](backend-domain.md#host-operations-and-permission-decisions),
[MCP](backend-domain.md#mcp-ownership-imports-and-delegated-discovery) and
[language services](backend-domain.md#workspace-discovery-and-language-diagnostics).

Kernel workers receive a restricted environment and use typed host operations.
A checkpoint covers settled execution; it is not a replay script. Process groups
provide cleanup of owned processes, not a security sandbox against other code
running as the same OS user or ownership of arbitrary detached descendants.

Human terminals are command-owned PTYs independent of session/agent lifetime.
Their exact process epoch, bounded byte ring and decimal offsets make restart
and truncation explicit. Reader detach does not close the terminal. Uncertain
open is inspected through the current terminal list; input has no durable replay
receipt. Explicit close and host shutdown join the owned readers and processes.
Network use requires the operator's explicit terminal opt-in.

Desktop Browser tabs remain human-owned. Agent attachment binds an exact native
provider/connection epoch; delegation transfers scoped control. Releasing agent
authority does not close the human tab. Disconnect/revocation retires dependent
work, and a lost mutation reply remains unknown rather than eligible for replay.
See [Browser ownership](browser-computer-use.md) for the native bridge contract.

## Socket and gateway lifetimes

The [RPC server](../internal/rpc/server.go) bounds active peers before accepting
connections and joins them during shutdown. Each ordinary connection initializes
protocol major 4 and owns serial framing, read/write deadlines and bounded
requests. Browser-provider and executor bindings have their own explicit duplex
lifetimes. No socket context owns already accepted work.

[`internal/gateway`](../internal/gateway) serves the production renderer and
forwards restricted native traffic. Each WebSocket owns one upstream socket,
pinned to runtime identity, process epoch and the acknowledged network marker.
Shutdown explicitly closes and joins hijacked WebSockets and content transfers.
The gateway neither reconnects to a replacement host nor replays a mutation.
Host/Origin checks are not user authentication.

`whipcode web` runs an independent foreground gateway attached to an existing
native host. `WHIPCODE_NETWORK=1` opts a host launch into an in-process managed
gateway. Its readiness/error/endpoint is live host status. Managed gateway
failure leaves core execution available; host shutdown closes the gateway.
There is no managed gateway child or automatic restart loop. See
[web setup](web-app.md) and [gateway ownership](backend-domain.md#browser-gateway-ownership-and-network-controls).

## SDK and terminal clients

The [native SDK](../packages/sdk/README.md) verifies runtime identity and owns
bounded SessionView, TreeCatalogView, ExecutionView and TraceView snapshots.
Views reconcile native reads; there is no retired event replay log. Suspending,
disposing or replacing a view joins observation without cancelling execution.
Callers must explicitly reconnect views to the same runtime's new client.

DurableCommand and RecoveryJournal retain exact recoverable requests before
transmission. Recovery checks original acceptance; explicit retry retains the
same identity and payload. Capacity refuses new sends rather than evicting
unresolved requests. Provider credentials, terminal input and other ephemeral
effects do not enter the journal. Browser persistence contains exact request
text; mobile's persistent recovery retains receipt metadata only.

The native terminal borrows a verified Go client and owns its Bubble Tea loop,
bounded reads, dialogs and local drafts/recovery. Dialog operations capture the
selected owner and request identity. Closing or replacing a dialog cancels its
observation; stale replies cannot overwrite a new owner's draft or submit work.
The local CLI composition owns host readiness and client attachment. Retained
terminal implementation files still awaiting deletion are not native execution
owners; see [CLI disposition](backend-native-cli-disposition.md).

## React application lifetimes

AppRuntime owns a window's host attachments, Query client, view leases, command
observations, drafts and presentation state. HostConnections has a separate
client, abort scope and bounded catalog per host. Detachment retires that host's
reads and listeners; it does not detach other hosts or cancel accepted work.
Recovery rediscovers the pinned host and replaces observation only.

Visible panes lease the exact selected root or child. A transcript/ExecutionView
pair is shared per session; trace filters and reading anchors have their own
view identities. Lease release is idempotent. The current count/byte/expiry
budgets live in [the frontend guide](frontend.md#current-retention-budgets), not a
second set of limits here. TanStack Query owns bounded detail reads, not session
truth. Automatic mutation retries remain disabled.

Tree catalog revision changes invalidate native title, search, attention and tab
summary reads. Visible tab summaries use `trees.summaries` without acquiring a
transcript lease. There is no `session_title_notifications` capability or
`sessions.title.changed` event in the native client path.

CompositionStore owns uploads and scoped attachments across composer mounts.
Removing a file or explicitly retiring its owner aborts the transfer; changing
a visible tab does not. Acceptance clears only the matching submission and draft
revision. ReadingPositions retains bounded hints while the timeline remains the
scrolling authority. Tabs and appearance are navigation/device state, never
execution authority. See [frontend lifetimes](frontend.md#runtime-construction-and-lifetimes).

## Native companion lifetimes

[MobileRuntime](../apps/mobile/src/runtime/runtime.ts) owns one selected host,
Query client and selected session/ExecutionView pair. Host replacement increments
its epoch and aborts the previous local lifetime before asynchronous cleanup.
Old continuations cannot publish into a new host. Backgrounding suspends views,
cancels reads and requests a storage flush; foregrounding reconnects and verifies
the same runtime before enabling actions.

Recovery metadata and its draft correlation are saved before admission. Restored
metadata can inspect an outcome but cannot reconstruct or replay the request.
The original payload is available for explicit retry only while retained in
memory. Failed durable storage blocks sending; unresolved records are not evicted
to make room. Device tests and physical-device evidence remain distinct; see
[mobile setup](mobile.md).

## Desktop backend replacement

Desktop preparation and update own local installation/maintenance separately
from renderer observations. A replacement verifies staged bytes, coordinates the
maintenance and runtime ownership locks, and waits for matching native readiness.
An existing owner requires explicit interruption approval; cancellation or an
unconfirmed shutdown cannot authorize replacing a still-running backend.
Release-specific approval survives a failed handoff for explicit recovery.

This implementation is validated in disposable compiled fixtures; it is not
permission to update an installed runtime. Remote hosts and signed release
acceptance have separate scope. See [native CLI disposition](backend-native-cli-disposition.md)
and [desktop setup](desktop.md).
