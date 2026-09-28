# Backend domain and persistence

This is the implemented domain contract for the
[backend redesign](backend-redesign-plan.md). The new runtime, runner, RPC and
SDK use these records directly. Retained applications still use the explicitly
separate legacy runtime and SDK until their client cutover.

## Ownership

| Fact | Authority |
| --- | --- |
| Runtime identity and schema version | Fresh SQLite database |
| Definition defaults and declarations | Immutable definition revision document |
| Tree metadata and common engine | Tree row |
| Reusable subtree capacity limits | Revisioned resource-limit rows; usage derived from live owning records |
| Scoped execution permission | One SQL permit per executing turn; released at a settled wait boundary or terminal settlement |
| Permanent model spending limits | Revisioned budget-limit rows; usage derived from immutable attempts |
| Session identity, immutable parent/tree, definition origin, lifecycle | Session row |
| Effective configuration | Immutable configuration revision selected by the session |
| Configuration used by execution | Turn's pinned configuration revision |
| Accepted payload | Input row |
| Request identity and payload digest | Receipt row |
| Execution outcome | Turn row; input and receipt outcomes are derived |
| User transcript payload | Reference to the accepted input; no second body copy |
| Assistant/tool transcript payload | Message row |
| Provider dispatch, usage, price snapshot and cost | Model-attempt row, linked to its completed message |
| Code dispatch, outcome and exact REPL boundary | Cell row, linked to its assistant call and tool-result message |
| Checkpoint compatibility metadata and body reference | Immutable terminal cell checkpoint |
| Host-operation intent, dispatch and outcome | Operation row, owned through its cell and turn |
| Exact session/capability/resource authority | Immutable grant row; revocation is a terminal fact |
| One-use consent decision | Permission row, linked to its exact operation |
| Checkpoint image bytes | Durable immutable blob file, verified before restore |
| Content digest and size | Immutable content-body metadata row |
| Session access and declared media type | Immutable content-reference row |
| Content bytes | Durable immutable blob file, verified when read |
| Provider endpoint and credential reference | Explicit host configuration file |
| Incomplete provider text and tool-call previews | Bounded runtime memory; never transcript rows |
| Resolved credential, worker, interpreter, process or client | Execution memory; never session rows |

Root and child sessions have the same records and store methods. Root lookup is
derived from the sole null parent in a tree. Tree creation inserts tree and root
in one transaction. Foreign keys enforce same-tree parents, a partial unique
index allows only one root, and immutable topology plus existing-parent insertion
prevents cycles. SQL guards only reject invalid writes; they never orchestrate
work. No public operation reparents a session.

Definitions are content-addressed immutable revisions, including built-ins.
Registering a changed document produces another revision, leaving existing
references intact. Available tools, hooks and child templates are declarations;
they grant no authority and contain no executable bindings.
Opening storage idempotently registers the built-ins shipped with that binary.
An edited built-in adds its content-addressed revision; existing sessions keep
their exact original references. Definition lookup always requires a revision.
Schemas are validated and resolved locally; remote references cannot fetch
network data or change a pinned declaration later.

Configuration resolution is host/parent defaults, pinned definition defaults,
then explicit overrides. Patches replace whole fields. Nil inherits; an empty
collection clears. An explicit output policy with a null schema clears a
structured output contract. Resolution owns deep copies of all collections and
schemas. Dynamic project files and skill discovery are policies, not frozen
contents. Refreshing those declared sources is part of the retained context
behavior being ported; current execution uses the captured instruction text.

Configuration updates compare the expected revision and append a new immutable
revision. A running turn retains its captured revision; the next claim captures
the current one. Changing model selection never rewrites history, topology or
checkpoints. Host route changes affect future dispatch through the named route.
Each prepared request records its actual route/pricing snapshot on the model attempt.
When spawning without a new definition, the child copies the parent's current
effective configuration before applying overrides. It does not reapply the
original template and accidentally undo explicit parent updates. Selecting a
different pinned definition applies that template between parent defaults and
overrides. Working directories are absolute and fixed for a session's lifetime.

## Atomic transitions

1. **Admission:** identity is (client_id, request_id) within one runtime.
   The digest covers operation kind and typed submission, including recipient and source.
   A receipt and input commit together. Equal retries return the same receipt;
   changed payload reuse conflicts. Queues are bounded. No worker starts here.
2. **Claim:** an eligible queued input or due mail starts a new turn; the
   current config revision is captured. An input-backed user transcript entry
   references its input, and mail entries reference immutable mail revisions. One transaction and a partial unique index enforce one active turn
   per session. Independent store connections must obey the same invariant.
   Each turn claims at most one input; mail-only turns claim none. Input batching
   is not implicit. FIFO input order comes from the insertion ordinal.
3. **Transcript append:** stable message identity and per-session sequence make
   retries idempotent. Completed entries survive restart even before turn finish.
   Tool calls are assistant parts; tool results reference their call ID and
   occupy their own tool message. Input cannot inject either. Another assistant
   message cannot pass unanswered calls. Root and child use the same table.
   Provider deltas are disposable previews keyed to the eventual message ID.
   Only validated completed responses become transcript messages.
4. **Finish:** optional completed messages and the terminal turn outcome commit
   together. Input and receipt observations join the turn rather than copying
   terminal states. A failed persistence attempt can retry this transaction; it
   must not call a provider or repeat an effect.
5. **Model dispatch and settlement:** reservation precedes exclusive dispatch.
   One attempt identifies one actual request, with a logical-call identity for
   retries. Terminal outcome, usage, calculated/provider cost and its completed
   assistant message commit together. Retrying settlement cannot redispatch.
   Missing usage or prices remain unknown; explicitly free prices can prove zero.
   Arithmetic overflow preserves output and evidence with unknown cost.
6. **Recovery:** opening a store never starts or interrupts execution. After
   acquiring exclusive runtime ownership, the runtime explicitly interrupts
   nonterminal turns. Unclaimed queued inputs remain queued; claimed inputs stay
   linked to the interrupted turn and are never automatically requeued.
   In the same transaction, reserved attempts become cancelled with known zero
   cost and dispatched attempts become uncertain. A turn cannot finish while
   an attempt, operation or code cell remains unsettled. Waiting/ready operations
   become cancelled; dispatched operations become uncertain. Pending permissions
   are cancelled before cells settle, so a lost live waiter cannot resume.
   Recovery also records an uncertain
   result for an admitted unfinished cell, and a not-dispatched result for an
   unanswered assistant call that never acquired a cell record.

Turn transitions are running → cancelling → cancelled/interrupted or
running → succeeded/failed/cancelled/interrupted. Terminal outcomes cannot
change. Cancellation requests persist intent; they do not claim that an external
effect has stopped. Operation dispatch and settlement record that evidence separately.

Queued input cancellation removes its eligibility. Claimed input cancellation
targets its turn. Stopping a session pauses admission/claims and requests
cancellation of active execution while retaining queued input. Resuming permits
claims after active execution has settled. Disconnecting a client changes none
of these records.
The stop transaction returns the exact cancelling turn identity as transient
cleanup data. Runtime cancellation uses that identity; a delayed stop response
cannot target a newer turn after reactivation. No duplicate lifecycle row is needed.

Deletion is explicit and atomic for a subtree (or the entire tree when targeting
its root). Active turns make deletion conflict until cancellation/cleanup has
completed. Descendant transcripts, inputs, turns and config revisions are removed.
Receipts retain identity/digest with a deletion marker, so retrying a deleted
submission cannot recreate work. Receipt retention or runtime identity reset
must be an explicit future retention policy. Definition documents belong to the
catalog, so deleting a session does not delete its template.
Queued inputs in the deleted subtree are discarded, with the same receipt
deletion markers. Stopping one session affects that session; it does not silently
stop descendants. Child admission is atomic; authorized subtree controls remain
Phase 4 work.

Collection queries accept 1–100 rows and also stop at a 4 MiB payload budget.
Resume after the last returned session ID, definition ID/revision, or transcript
sequence. An empty page means the end; a short page can reflect the byte budget.
Messages and configuration documents are bounded at admission. SQL timestamps
use integer UTC microseconds, avoiding mixed-precision textual ordering.

## Content boundary

`content.put` publishes bytes durably before registering body metadata and a
session reference. A caller-supplied reference ID makes upload retries idempotent;
changing its owner, bytes or media type conflicts. The body table owns digest and
size once, while references own session access and media interpretation. A digest
alone never authorizes reading. The trusted client can select a session; runner
hydration always uses the actual executing session's identity.

Input admission and completed-message insertion validate every reference in the
same SQL transaction as their write. Missing and foreign references produce the
same not-found outcome, without leaving an input, receipt or output. Existing
admission/message retries preserve their original idempotency behavior.

Bodies and reads are limited to 4 MiB. Each session may retain 1,024 references
and 64 MiB of referenced bytes (including repeated references to one body).
Provider context hydration is bounded in aggregate and checks file size/digest;
encoding also counts repeated occurrences. Text and supported images become
temporary provider payloads, while durable messages retain only reference IDs.
Special files and symlinks cannot substitute for a body during verified reads.

Session deletion removes references transactionally. Shared bytes remain available
to surviving owners; checkpoint references also retain their image files. Physical orphan collection runs only during exclusive
startup, before uploads or requests can run, in bounded directory batches. This
also collects files left by successful publication followed by failed SQL.
No live deletion can race the gap between publication and registration.

## Host and schema boundary

Initialization takes explicit paths and never discovers an installed daemon or
reads the retired home/config. A new host config is valid but unconfigured: users
must select a model/provider before creating a runnable session. Credentials are
environment references resolved only when constructing a provider client.

SQLite has an application identifier and schema version. Existing databases of
another application/version are rejected, not imported. Reopening preserves the
runtime identity and seeded revisions; separate databases receive distinct
identities. Future versions of this fresh schema may have ordinary migrations.
The store owns database transactions only, with no resource-manager construction.

The new runtime implements the OpenAI-compatible chat-completions adapter and
both Starlark and QuickJS subprocess engines. These are protocol/engine adapters,
not a hardcoded commercial model or credentials. The remaining provider families
and product integrations are still being ported.

## Phase boundary and verification

Phase 1 delivers persistence and initial v4 wire declarations/generation fixtures.
Phase 2 adds runtime scheduling, RPC serving, SDK/Go clients and a full scripted
turn through the new stack. Phase 3 adds model attempts, authorized content, the code loop and checkpoint
boundaries. Scoped host effects and grants remain in progress; budgets, mail and
shared state follow in Phase 4. No empty repositories or speculative tables are
created for those capabilities.

Passing tests against real temporary SQLite cover constraints,
transaction rollback, concurrent claims across connections, pinned configuration,
root/child history, cancellation, restart and deletion. Contract checks
validate actual Go JSON in TypeScript, including counters above JavaScript's
safe-integer range. Import checks prevent new core packages reaching the retired
orchestration. The [development guide](backend-redesign-development.md#phase-1-behavior-ownership-and-evidence)
maps every acceptance criterion to its tests and records local/hosted results.

## Execution and transport ownership

`runtime.Open` locks a private explicit directory, opens fresh host configuration
and SQLite, and removes a dead owner's socket. It starts no work. `Start` performs
recovery under that execution lock and starts a bounded scheduler. One worker
owns one claimed session turn; SQL also enforces this invariant. Workers do not
hold the scheduler mutex across provider calls or database operations. Closing
the runtime cancels and joins workers, closes owned kernels, then releases storage
and its lock. Successful subtree deletion also closes its live kernels.

`runner` projects the durable transcript into a provider request using the turn's
captured configuration. It has injected provider/transcript interfaces and no
runtime or SQL imports. Completed output is persisted with a stable message ID;
retrying that write or turn settlement never dispatches the provider again.
These cleanup writes have a separate five-second bound. Exhausting settlement
retries faults the runtime, leaving recovery to interrupt the nonterminal turn.
Phase 3 adds durable attempt accounting and effect uncertainty.

The Phase 2 runner explicitly fails context beyond 100 messages or 4 MiB until
compaction is implemented. Its scripted provider is an injected adapter on this
same runner path and accepts only the `scripted/scripted` selection. There is no
engine execution in Phase 2, despite the tree's engine selection being retained.

`rpc` serves newline-framed JSON-RPC on the private Unix socket. Every connection
must initialize with major 4; reconnections verify the original runtime identity.
Frames are bounded at 8 MiB, connections at 64, idle reads at 30 seconds, writes at
5 seconds, and request handlers at 10 seconds. The server joins its connection
handlers on shutdown. Request contexts own admission/observation only, never the
lifetime of accepted work. Protocol errors are typed without exposing internal
database or provider error details.

The SDK and Go client initially observe by polling receipts and reading bounded
history pages. They keep no event log or duplicate transcript authority. Stable
request identities recover lost acknowledgements; ambiguous transport failures
do not imply rejection. Explicit input/turn cancellation is separate from local
wait cancellation. Bounded preview observation is described below; product-facing
synchronized views are part of the later client cutover.


## Code execution and checkpoint boundary

The runner advertises `execute` and repeats model → code → model through injected
interfaces, with at most 32 logical model calls and 64 dispatched cells per turn.
Each retry has its own attempt under the logical call. Completed assistant calls
commit before code admission. Model tool declarations describe available syntax;
they do not authorize host effects. The tool dispatcher separately admits and
authorizes supported host operations.

A cell names its turn, committed assistant message and provider call ID. An atomic
begin admits exactly one execution. The runtime serializes each session's kernel
and pins process capacity through cell settlement, releasing capacity before the
next provider call. Kernels are disposable runtime-owned resources. Loading a
session or updating its model does not recreate its REPL or start execution.

The engine stages a checkpoint after evaluation. Immutable bytes are published
first; the tool-result message, cell outcome and checkpoint reference then commit
in one SQL transaction. An unchanged image may reuse the preceding body at the
new cell boundary. A failed publication/commit cannot make the candidate image
restorable. Startup collection retains committed checkpoint bodies and removes
orphans. Engine protocol sequence numbers are not transcript or cell watermarks.

A correlated engine result can report a language error while retaining useful
partially changed globals. It is distinct from transport loss/cancellation, which
leaves the cell outcome uncertain. A completed cell can also lack a usable
checkpoint. Both facts remain visible: its result survives, while further code
execution fails explicitly instead of loading an older image. Text-only turns and
history inspection remain possible; a fresh session starts a fresh REPL. No repair
or replay is automatic. There is not yet a client operation to reset an existing
REPL boundary.

Restoration verifies the body digest/size and engine build, ABI, profile and
fidelity. Starlark checkpoints are partial; skipped globals and restoration
failures are recorded in tool results. QuickJS checkpoints preserve the whole
image within their resource contract. Restoration never evaluates past cells or
reissues host calls. Both engines preserve state across model changes, eviction
and restart through the same runtime and storage path.


## Host operations and permission decisions

`internal/tool` prepares bounded host requests and coordinates their durable
admission, consent and dispatch through consumer-owned interfaces. It owns no SQL,
interpreter or scheduler. The runtime binds each host invocation to its running
cell; the store derives the owning turn and session from that cell. The operation
keeps normalized immutable arguments. Its ID is stable for the cell/invocation
identity; a repeated identity cannot execute the effect again.

A standing grant matches one session, capability and resource exactly. Workspace
file capabilities use the session's fixed working directory as their resource;
`files.read`, `files.write` and `files.patch` are separate capabilities. Workspace
scope permits relative paths within that directory, not arbitrary host paths.
Creating standing authority is an explicit trusted-client operation. Templates,
model instructions, child relationships and an existing workspace confer none.
Resolving a permission as approved creates a grant bound to that exact operation,
including its normalized arguments. The next invocation needs its own decision.

An operation moves from waiting → ready → dispatched → succeeded/failed/uncertain.
Waiting/ready operations can instead become denied/cancelled. Consent, one-use
grant creation and readiness commit together. Filesystem locks and root handles
are acquired after consent, then SQL rechecks the live owner and unrevoked grant
at dispatch. Revocation committed before dispatch prevents the effect; revocation
after dispatch cannot claim an already admitted effect stopped. No SQL transaction
contains filesystem execution. A cell cannot settle with unfinished operations.

The filesystem adapter uses rooted handles, canonical cancellable mutation locks,
regular-file checks and bounded UTF-8 requests/results. Writes/patches publish with
an atomic same-directory rename and sync; each prepared request executes once.
Read pages default to 2,000 lines with a 256 KiB source cap and 32 KiB output cap.
A null `next_offset` means an incomplete line/source cannot resume by line number.
Mutation errors conservatively record uncertainty. Successful effects retain that
outcome even when cancellation arrives before result presentation. SQL settlement
may retry for five seconds without invoking the effect again. Dispatched calls
have a 30-second execution context; waiting for consent remains cancellable.

A permission is durable decision evidence, not a live waiter. Restart cancels
undispatched operations and pending permissions, marks dispatched unsettled
operations uncertain, then reconciles cells/turns in the same transaction.
Completed operation evidence remains unchanged even if its cell's checkpoint was
lost. Late approval of a cancelled permission conflicts. No effect is replayed to
recover an interpreter image. Grants and operation evidence are retained until
explicit owner deletion; current limits are 1,024 grants per session and 1,024
operations per turn. Lists use bounded 100-item/4 MiB pages.

The SDK exposes `grants.create/list/revoke`, `permissions.list/resolve`,
`operations.get`, `turns.operations`, `cells.get` and `turns.cells`. These views
query durable records; they do not introduce another authority cache.


## Provisional output and observation

The provider owns response assembly; the runner owns dispatch and commit. A
synchronous chunk callback publishes text and incomplete tool arguments into a
runtime-owned preview for the active attempt. The runtime retains at most 64
previews of 128 KiB each, uses UTF-8-safe truncation, and exposes truncation
explicitly. It retains no completed preview cache or replay log. Preview callbacks
cannot start effects, veto execution or inherit an observing client's lifetime.

OpenAI-compatible requests ask for SSE and usage. The adapter also accepts bounded
JSON responses. Streaming requires both a valid completion reason and the final
transport marker; malformed, truncated or cancelled streams return no executable
parts and are recorded as uncertain. Known usage/cost received before failure
remains evidence. Each retry has its own attempt and preview identity. The prepared
wire body, route and price snapshot remain frozen across retries.

`sessions.observe` returns bounded committed history after a decimal cursor, a
nullable preview and a fresh process epoch. A preview identifies its turn,
attempt and eventual committed message, with its own revision. It is not a
message and may contain incomplete JSON arguments. The runtime snapshots it before
reading attempt/history state, suppressing it when its committed message is
visible. SQL settlement retries keep the preview alive without redispatching.
Success replaces it by the committed message ID; failure discards it without
inventing an assistant message. Restart changes the epoch and drops all previews
while the ordinary durable attempt/turn recovery records uncertainty.

The SDK's `observe` async iterator drains history pages, then polls live state at
100 ms. It retains only the exact history cursor and last preview revision. A
slow or disconnected observer cannot block the provider or cancel accepted work.
Consumers upsert completed messages by ID, replace matching previews, and clear
a preview on null or epoch change. Polling reads current state, so losing a
notification cannot leave a durable history gap. Aborting observation stops only
the observer; execution cancellation remains an explicit operation.


## Child admission, delegation and waits

`sessions.spawn` requires a request identity, parent, initial text/content parts,
configuration overrides and `grant_ids`. The result contains the child session
and its ordinary input admission. Child identity, resolved configuration,
parent-scoped content references copied into new child references, delegated
grants, initial input and receipt commit together. No duplicate body bytes or
child-specific transcript path exists. `grant_ids: null` inherits currently valid
standing grants; `[]` delegates none. Retries preserve this original request and
return the same child/input even if parent defaults or grants have since changed.
A deleted receipt returns `session: null`; it cannot resurrect the child.

A child standing grant names an `issuer_id` belonging to its direct parent with
exactly the same capability/resource. One-use approvals cannot be delegated.
Dispatch validates every issuer hop and revocation. Revoking an ancestor grant
also denies ready descendant operations; it cannot undo effects already
dispatched. A child lacking delegated authority records a denial, not a prompt
that could widen its scope. The trusted client can explicitly delegate another
standing parent grant later. Stopping only a parent does not revoke its grants.

`agents.spawn(prompt=...)` normalizes the executing parent into immutable
operation arguments. Its tree-scoped authorization, child admission and operation
success commit in one transaction. The tool dispatcher distinguishes these
SQL-only coordination operations from externally dispatched effects. A failed
SQL commit cannot leave a child without its input or claim an operation succeeded.

`agents.wait_after_cell(input_ids=[...])` registers a wait and returns immediately.
Finish the cell; after its result/checkpoint commit and kernel lease release,
the runtime waits before the next model step. This replaces same-cell blocking
joins in the new runtime. Only descendant inputs can be targeted (never self,
ancestors, siblings or another tree), preventing wait cycles. Failed, interrupted
or cancelled child work resolves the wait as a durable outcome; it does not
implicitly fail or retry the parent. Deleting a target makes it unavailable and
fails that wait. A cell may register at most 128 distinct input targets. The
successful operation rows are the durable registrations; there is no duplicate
wait registry/table. Restart interrupts the parent turn rather than inventing a
suspended interpreter continuation; queued child work survives independently.

`agents.submit(session_id, parts)` admits another input to a direct child, sharing
only content references already authorized to the caller. Input, receipt and
operation success commit together. `agents.inspect(session_id, input_id, offset)`
reads that exact descendant input's outcome, never another turn's answer. Text is
paged at UTF-8 boundaries with a 16 KiB maximum, decimal-string offsets and total
bytes, message identity, truncation and omitted-part metadata. When inspecting an
unfinished turn, check the returned message identity before combining pages;
terminal output is stable. `agents.list(relation, after, limit)` returns bounded
parent, child or sibling identity/lifecycle metadata, without their configuration
or transcript. Each operation requires its own tree-scoped capability.

`agents.stop(session_id)` stops a proper descendant subtree, retaining queued
input and requesting cancellation of active turns in the same transaction. Only
after commit does the runtime cancel their workers; replay only services turns
already marked cancelling, so it cannot stop subsequently restarted work.
`agents.delete(session_id)` shares the ordinary subtree deletion transaction and
rejects active or cancelling work. Successful operation retries return their
original result even after the target disappears. Kernel cleanup follows commit.

The runtime separately owns active turn cancellation handles and physical worker
slots. SQL `turn_permits` authorize execution under every ancestor's
`runnable_descendants` limit. A running turn can wait without a permit; outcome and
execution permission have different lifetimes. Claim acquires permission before
consuming queued input. A boundary wait releases it only after all attempts, cells
and host operations settle, then frees the worker slot. Resumption reacquires
both before any new model or code execution. New attempts, cells and host-operation
dispatch require that permission. Finish and restart recovery release it atomically.

Resumptions alternate with fresh work. A blocked resumption does not hide later
waiters, and a bounded cursor pages past blocked queued sessions and wraps back to
earlier work. Physical slots are reserved before SQL acquisition without holding
the scheduler mutex; cancellation after acquisition releases unused permission.
`MaxActiveTurns` defaults to 1,024 and is limited to `Workers..1024`; at most
`MaxActiveTurns - Workers` turns may wait, preserving admission capacity for
progress. Limit exhaustion is explicit. One worker and one kernel slot can
execute a recursive chain because no waiting parent holds either resource.


## Model budgets and retained accounting

`budgets.list(session_id)` projects four local scopes: `model_calls`,
`model_tokens` (input plus output totals), `model_cost_nano_usd`, and
`model_elapsed_millis`. Each scope has a nullable limit and a compare-and-set
revision. An absent limit is unlimited locally; every ancestor's live cap still
applies. Null removes only the local cap, with the same inheritance semantics as
resource limits. `budgets.set` requires the current revision (zero for an unset scope).
Child admission may include explicit narrower `budgets`; limits are not copied
into each descendant. A child cannot explicitly widen a finite ancestor cap.
Changing a parent cap immediately constrains subsequent descendant reservations.

Reservation checks and attempt insertion commit together across every ancestor.
The request snapshot owns the input bound, output maximum, prices and timeout;
there is no separate mutable copy of these bounds or of consumed usage. Each
attempt captures immutable ancestor associations for accounting. Concurrent
siblings therefore cannot reserve the same remaining allowance. Frozen requests
are admitted or rejected as a whole; reservation never silently clamps their
output tokens after request encoding/digest calculation.

The projection distinguishes known `used`, unfinished `reserved`, and quantified
unresolved `uncertain` exposure. `incomplete` means some exposure cannot be bounded
from available evidence or represented in an int64 aggregate; finite limits
reject that uncertainty. Unknown usage/cost remains distinct from known zero.
Confirmed undispatched cancellation releases its reservation. Dispatched requests
count as calls even if their response is lost. Restart preserves unresolved
exposure, and retries are distinct actual attempts. Known overages remain visible
and prevent further admission. Setting a finite cap below already allocated
used/reserved/uncertain amounts is rejected. Overflow never wraps counters; exact
individual evidence remains in the ledger and a saturated aggregate is incomplete.

Attempt records outlive deletion of their child session/transcript, retaining
original turn/message identities as historical references. Captured ancestor
associations keep their usage charged to surviving ancestors. Deleting a whole
tree explicitly removes its accounting; deleting and recreating a child cannot
replenish allowance. Usage is stored once in the attempt ledger and budget views
are derived. Ancestors without finite limits skip historical aggregation during
reservation, while retaining associations for later inspection or limit changes.

For HTTP models, optional host `context_window_tokens` declares the provider's
maximum context, used as a conservative input bound rather than an estimated
request token count. It must be positive, at most one billion, and at least the
configured output maximum. Missing bounds/prices prevent finite token/cost
reservation when required; unlimited scopes can still execute with explicit
unknown evidence. This is a trusted host declaration, not independent proof of
a provider limit. Actual usage, charges and timeout overruns are never clamped
to it. Elapsed usage is monotonic execution time rounded up to milliseconds;
SQL settlement retries do not add execution usage. Provider timeouts are
cooperative, so an actual duration may exceed its reserved timeout.

## Reusable resource capacity

Resource limits belong to sessions, not the tree metadata or model accounting.
`resources.list(session_id)` returns each kind for the requested session and every
ancestor, nearest first. Each entry identifies its scope, revision, nullable local
limit, and current usage. A child's absent row has revision zero and no local cap;
ancestor caps still apply. The response is bounded by the absolute 128-edge ancestry
limit and the six supported kinds. It is a single database snapshot, not a cache.

| Kind | Usage within the owner's subtree | Capacity becomes available when |
| --- | --- | --- |
| `depth` | Greatest retained descendant edge distance; owner is depth zero | Deepest descendants are deleted |
| `descendants` | Retained descendant identities, excluding the owner | Descendants are deleted |
| `queued_inputs` | Unclaimed, uncancelled inputs, including the owner's | Inputs are claimed, cancelled while queued, or deleted |
| `active_operations` | Unsettled host operations, including permission waiters | Operations reach any terminal outcome |
| `subscriptions` | Active shared-state subscriptions | Subscriptions are cancelled or their owner is deleted |
| `runnable_descendants` | Execution permits held by proper descendants; excludes the owner | A settled parent wait yields, or a turn finishes/is recovered |

All applicable ancestor scopes are checked inside the same immediate transaction
that admits work. Siblings share their parent's allowance. In particular, queue
capacity is now aggregated across the subtree, replacing the old independent
per-session queue cap. Child creation acquires its identity and initial input
capacity atomically; denial leaves neither a child nor a receipt. Exact accepted
retries return their receipt before new capacity checks, including deletion markers.
No counters need repair after a crash. Stop, worker eviction, turn completion, and
model changes do not release retained session capacity. Claiming a mail-only turn
does not change queued-input usage. Recovery settles operations but retains
unclaimed inputs. Cancelling a subscription retains its historical row and committed
notification evidence; its separate physical retention bound still applies.

`resources.set` requires the scope's current revision. It rejects a limit below
current usage; stale revisions conflict. Removing a child's local cap with null
inherits ancestor enforcement. Explicit child limits cannot exceed ancestor caps;
for depth and descendants, subtract the ancestor-to-child distance because those
identities already consume ancestor capacity. A child depth cap of zero permits
that child but no descendants. Tightening a parent immediately constrains later
admission even if a child's older local cap is larger. Inspection returns all
scopes so callers can see which allowance is exhausted. A local allowance is not
reserved exclusively for that child.

Fresh host configuration is version 2. Optional finite `resources` defaults and
creation overrides resolve once into root rows at revision one: depth 8,
127 descendants, 256 queued inputs, 64 active operations, 1,000 subscriptions,
and 64 runnable descendants.
Root limits cannot be null. Partial creation/configuration lists inherit the
remaining built-in defaults; duplicate kinds are rejected. Limits use decimal
strings, including host JSON and guest child-spawn arguments. Host edits never
change existing root limits. `TreePolicy` and its duplicate JSON column no longer
exist; this disposable database uses schema 12 and rejects previous schemas.
The absolute depth ceiling remains 128 for bounded hierarchy and grant traversal.

Capacity reuse never replenishes permanent model spend. Deleting a child releases
its retained resources while its immutable model attempts remain charged to live
ancestors. Per-value, retained-history and byte safety bounds remain at their
owning boundaries. Configurable cumulative logical-write allowances are still
Phase 4 work; reusable capacities do not claim to cover them.
The legacy byte/record allowance charged selected accepted writes, including
state and mail, without refund on deletion. It was not a quota over all database
rows or retained bytes. Model accounting and required execution settlement must
remain recordable when a write allowance is exhausted.

## Mail and presentation

Mail is retained inter-session communication, separate from accepted input.
A caller supplies a stable mail ID; its original typed payload digest makes an
identical send retry idempotent. A deleted recipient leaves a mail tombstone,
so retrying an uncertain send cannot resurrect deleted work. Sender deletion
leaves surviving recipients' mail intact. For session-authored mail, senders and
recipients must be in the same tree and be self, parent, child or siblings.
Mail metadata has an explicit source: `session` plus sender ID, or `state` plus
subscription ID. Clients and agents cannot forge runtime notification provenance
through the send API. Runtime-authored notifications use their own scoped source
identity and can reach any subscriber in the same tree.

A mail identity owns its current revision and handling state. Immutable revisions
own delivery class, availability, subject and body. Transcript entries reference
the exact presented revision, so replacing pending mail cannot rewrite history.
A listing contains metadata and body size; an explicit read returns the body.
Human list/read operations do not start work, establish agent observations or
acknowledge delivery.

The scheduler derives readiness directly from due mail and queued inputs.
`queued` mail waits for an idle session, `steer` can also be presented at the next
model boundary after all preceding code calls settle, and `next_turn` waits for
another reason to start a turn. All due classes can be presented when a turn
starts. A mail-only turn has no input; execution, accounting, cancellation,
history and recovery use the ordinary turn path.

Turn observations record exact mail revisions. Listing through an agent helper
allows explicit handling but does not count as model presentation. Automatic
digests and explicit agent reads do. A successful turn marks only its presented,
still-current pending revisions delivered. Failed, cancelled and interrupted
turns leave mail pending. An unsuccessful latest turn blocks automatic mail-only
execution until an explicit input succeeds; new mail and a runtime restart do
not bypass that barrier. Delivery does not mean the agent has finished handling
the mail: `mail.complete` explicitly marks observed revisions done, while
`mail.defer` creates a new pending revision for a later availability time.

Guest helpers are scoped host operations with persisted authority and results.
Their observation/handling mutation and operation settlement share one SQL
transaction. They derive the sender/recipient from the executing session.
Client operations expose send and inspection; clients cannot impersonate a
successful agent presentation by acknowledging mail through an inspection API.

Mail bodies are limited to 16 KiB and subjects to 256 bytes. A digest presents at
most 20 items, each with at most 2 KiB and 20 body lines. Per recipient, storage
allows 1024 retained mail identities, 256 pending items and 20 unfinished items
from one source. Each source can create 30 identities in ten seconds. A mail ID
has at most 128 revisions and a turn at most 1024 observed revisions. Exhaustion
returns an explicit limit error; no body, revision or observation is silently
dropped. General resource policies and report coalescing remain subsequent
Phase 4 work.

## Explicit state and immutable values

Session state is private to its session; tree state is shared by members of that
one tree. Both use the same `state_versions` table. Each immutable row identifies
its owner, key, revision, author and content digest. The latest revision is the
head, derived from those rows; no mutable head cache or second current-value table
exists. The content store owns immutable JSON bytes. VM globals, conversation
history, ordinary content references and explicit state remain separate domains.

Every write compares an explicit expected revision: zero creates a key, a
positive value replaces that exact head. Appends concatenate two JSON strings or
two arrays against the same comparison. A failed comparison leaves metadata and
history unchanged. A caller retains a globally unique version ID and the exact
payload to recover a lost acknowledgement. Retrying an old successful write
returns its original version even when newer versions exist. Deleted private
owners cannot retry writes or read handles. State versions have no retry tombstone:
owner deletion returns not-found, and session IDs are never recreated by a write.

A version ID is an authorized handle. The digest alone grants no read authority,
and a state handle does not become an ordinary conversation-content reference.
Private versions are collected when their session is deleted. Shared versions
and history survive their author's deletion and are collected with their tree.
Startup collection under exclusive runtime ownership removes orphan publications;
normal execution never races file publication against collection.

State supports valid UTF-8 JSON to 64 MiB, nesting to 100 containers, and no
unpaired Unicode escapes. Numeric lexemes are retained without conversion to
float64. JSON storage accepts arbitrary numeric lexemes; a language adapter may
reject a number it cannot represent. Each tree retains at most 1024 versions and
1 GiB of logical version bytes, including private and shared history. Content
file deduplication does not reduce that logical budget. Limits reject writes;
no old version is silently evicted. Broader ancestor resource policies remain
Phase 4 work.

Guest `state.get` returns version metadata plus inline JSON only up to 64 KiB.
Larger values return metadata without an inline `value`; a stored JSON null is
still an explicit value. `state.read` returns at most 64 KiB of base64 bytes from
an authorized immutable version. Ranges can split UTF-8 characters and are not
partial JSON values. The file is hashed in the same read that captures the range,
with bounded memory; corruption anywhere in the file prevents returning bytes.
List and history queries page metadata only. Client reads always return encoded
bytes, preserving exact numbers across JavaScript transports. Each client write
or append carries at most 4 MiB of JSON, and a value can grow to 64 MiB through
revision-checked appends without creating an oversized protocol frame.

Guest state helpers use the ordinary scoped operation ledger. The runtime
publishes validated immutable bytes before SQL. The transaction rechecks
permission, compares the expected revision, inserts the version and settles the
operation together. The ledger retains metadata and digests, never a second copy
of the state body. Read operations retain the exact observed version and hydrate
its body after the transaction. Unreferenced files from rejected or interrupted
writes are collectible. Shared state writes notify only explicit subscribers through ordinary mail;
private state writes never create notifications.


## State subscriptions

A subscription belongs to one session and one shared key. Creation supplies a
stable ID and an `after` revision. The transaction reads the current head and
notifies the subscriber if it has advanced, closing the gap between a client's
snapshot and subscription. A future cursor conflicts. There is one active
subscription per session/key, and at most 1000 retained subscriptions per tree.
Creation retries return the same subscription; retrying a cancelled subscription
cannot reactivate it. List queries are bounded and owner-scoped.

A shared write inserts its version, advances each subscription cursor and
creates or replaces its pending mail notification in one transaction. The cursor
means enqueued or authored revision, not successful agent processing. Own writes
advance the cursor without notifying that same session. A pending notification
is coalesced by adding an immutable mail revision; the state version's ID, key,
revision and author form its small JSON body. The value itself remains in the
state content store. A recipient's deferred availability time survives coalescing.
Presented revisions remain immutable, so a successful turn that saw an old
revision cannot acknowledge its newer replacement.

Notification admission obeys ordinary mail capacity limits, including the
128-revision limit on a pending identity. If any recipient lacks capacity, the
entire write, every cursor update and all notifications roll back. This is
explicit backpressure, not silent notification loss. After a notification is
delivered, a later change creates a new mail identity. Stopped recipients retain
pending mail; normal lifecycle and failure barriers govern when it can run.

Cancellation stops future notifications and preserves already committed mail as
handling evidence. Deleting a subscriber deletes its subscriptions and applies
ordinary recipient-mail deletion. Restart needs no callback or in-memory watcher:
SQL retains subscriptions and mail, and scheduler reconciliation finds due work.
Guest subscription admission/cancellation and operation outcomes share the same
transaction as their generated notifications. No separate subscription runner
or wakeup queue exists.
