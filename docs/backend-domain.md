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
| Tree metadata, engine and admission limits | Tree row |
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
   The digest covers the typed submission, including recipient and source.
   A receipt and input commit together. Equal retries return the same receipt;
   changed payload reuse conflicts. Queues are bounded. No worker starts here.
2. **Claim:** one eligible queued input becomes associated with a new turn, the
   current config revision is captured, and a user transcript entry references
   the input. One transaction and a partial unique index enforce one active turn
   per session. Independent store connections must obey the same invariant.
   Each turn claims exactly one input in this initial contract; batching is not
   implicit. FIFO order comes from the input insertion ordinal.
3. **Transcript append:** stable message identity and per-session sequence make
   retries idempotent. Completed entries survive restart even before turn finish.
   Tool calls are assistant parts; tool results reference their call ID and
   occupy their own tool message. Input cannot inject either. Another assistant
   message cannot pass unanswered calls. Root and child use the same table.
   Provider responses currently arrive as completed messages; provisional stream
   delivery is still being ported.
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

Deletion is explicit and atomic for a subtree (or the entire tree when targeting
its root). Active turns make deletion conflict until cancellation/cleanup has
completed. Descendant transcripts, inputs, turns and config revisions are removed.
Receipts retain identity/digest with a deletion marker, so retrying a deleted
submission cannot recreate work. Receipt retention or runtime identity reset
must be an explicit future retention policy. Definition documents belong to the
catalog, so deleting a session does not delete its template.
Queued inputs in the deleted subtree are discarded, with the same receipt
deletion markers. Stopping one session affects that session; it does not silently
stop descendants. Phase 4 adds authorized subtree controls and child admission.

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
wait cancellation. Streaming and product-facing synchronized views come later.


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
