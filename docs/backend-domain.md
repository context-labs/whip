# Backend domain and persistence

This is the Phase 1 implementation contract for the
[backend redesign](backend-redesign-plan.md). The replacement packages are
internal/session, internal/store, internal/config, and internal/protocol.
The retained runtime still uses internal/legacy/{session,config,protocol} and
@whip/legacy-protocol until its replacement is connected in Phase 2.

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
contents; the runner refreshes them at turn start and records request evidence.

Configuration updates compare the expected revision and append a new immutable
revision. A running turn retains its captured revision; the next claim captures
the current one. Changing model selection never rewrites history, topology or
checkpoints. Host route changes affect future dispatch through the named route;
Phase 3 records each actual route/pricing snapshot on the model attempt.
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
   Streaming fragments remain provisional. Root and child use the same table.
4. **Finish:** optional completed messages and the terminal turn outcome commit
   together. Input and receipt observations join the turn rather than copying
   terminal states. A failed persistence attempt can retry this transaction; it
   must not call a provider or repeat an effect.
5. **Recovery:** opening a store never starts or interrupts execution. After
   acquiring exclusive runtime ownership, the runtime explicitly interrupts
   nonterminal turns. Unclaimed queued inputs remain queued; claimed inputs stay
   linked to the interrupted turn and are never automatically requeued.

Turn transitions are running → cancelling → cancelled/interrupted or
running → succeeded/failed/cancelled/interrupted. Terminal outcomes cannot
change. Cancellation requests persist intent; they do not claim that an external
effect has stopped. Phase 3 supplies dispatch evidence and effect uncertainty.

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

The first execution implementation in Phase 3 will use the OpenAI-compatible
chat-completions adapter and Starlark. This selects protocol/engine adapters, not
a hardcoded commercial model or credentials. QuickJS remains a declared tree
engine and is implemented in Phase 5.

## Phase boundary and verification

Phase 1 delivers persistence and initial v4 wire declarations/generation fixtures.
Runtime scheduling, RPC serving, SDK adoption and a full turn through the new
stack belong to Phase 2. Model attempts, effects, checkpoint bytes, grants,
budgets, mail and shared state are added by their owning later phases; no empty
repositories or speculative tables are created for them here.

Completion requires tests against real temporary SQLite for constraints,
transaction rollback, concurrent claims across connections, pinned configuration,
root/child history, cancellation, restart and deletion. Contract checks must
validate actual Go JSON in TypeScript, including counters above JavaScript's
safe-integer range. Import checks prevent new core packages reaching the retired
orchestration. The phase checklist remains uncompleted until this evidence exists.
