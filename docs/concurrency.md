# Concurrency and ownership

whip separates durable ordering from independent execution. One root actor
serializes state transitions for a session; model calls, kernels, MCP calls,
and unrelated roots may run concurrently under explicit limits.

## Root actors

Every client command receives a stable ID and durable ingress sequence before
execution. Root actors order inbox admission, turn commits, schedules,
permissions, child lifecycle, messages, and events. They do not hold registry
locks while provider or tool work blocks.

Clients move through:

```text
disconnected -> reconnecting -> snapshotting -> live
```

A retry with the same command ID retrieves the stored status or outcome. It
does not execute the operation twice.

## Recursive agents

Each live agent has one serialized kernel because its Starlark globals belong
to that worker. Different agents can progress concurrently up to the shared
`rlm.maxWorkers` pool. Children are admitted durably and queue for a worker;
idle workers can be evicted, while a running turn pins its worker.

Scratch restoration is part of acquiring a worker. A failed load or restore
stops the new subprocess before releasing its pool reservation and remains
retryable. Post-cell checkpoint failure preserves the cell result and the
previous checkpoint; it never replays a cell. Restore audit work belongs to
the root supervisor and ends with that owner.

Children are retained identities, not goroutines treated as records. A live
node owns its cancellation context, provider loop, services, and kernel. The
recursive runtime owns the tree and closes a whole subtree exactly once.

The budget ledger limits active children, concurrent child turns, recursion
depth, durable bytes, record count, operations, and schedules/subscriptions.
Cumulative model tokens, cost, and elapsed usage are unlimited by default;
explicit child caps narrow inherited authority. Every model transport attempt,
including retries, reserves and settles against the same ancestor rows using
its own model prices. Settlement runs outside the root actor and survives caller
cancellation. Missing usage becomes uncertain exposure, not known spend;
restart moves orphan model reservations to uncertainty and reconstructs live
capacity. Generic durable-operation consumption remains unchanged.

## Message flow

There is no separate notification queue. `agent_messages` is canonical; a
node is runnable when it has a `queued` inbox row or `pending` mail whose
`available_at` has passed, and the actor re-derives that from SQLite after
every commit, wake, restart, permission decision, and budget change. Explicit
work (`submit`, `steer`, `goal`, `schedule`) starts a turn by claiming one row.
Additional human steers are claimed against that exact running turn before
a loop boundary exposes them. A commit may acknowledge only claimed input.

Steer-class mail and human steers are injected at the running turn's next
loop boundary by the one delivery engine in `AgentSession.RunTurn`; there is
no stream interruption. Queued mail starts a mailbox-triggered turn whose
input is a bounded digest (excerpts, never bodies). Ten messages to a busy
node produce one digest, not ten turns. Messages become `delivered` only when
the turn that showed them commits successfully. After a failed, cancelled, or
interrupted root turn, pending mail waits for explicit inbox input instead of
immediately launching another failing mailbox turn. This retry barrier is
derived from the latest durable root turn, so opening a view, receiving another
wake, or restarting the daemon cannot bypass it. A successful explicit turn
restores automatic mailbox delivery. Delivery records include a message
revision: a replacement or deferral
creates a newer revision that an older turn cannot acknowledge. Listing
metadata establishes a revision for explicit controls without marking it
delivered. A stale explicit completion or deferral fails with a reread request.

Agent completion posts an `agent.completed|failed|cancelled` message to the
parent according to the child's durable report mode: a 160-byte preview and
evidence handle by default, up to 4 KiB for `inline`, or explicit child
messages for successful `message` turns. Failures still notify the parent.
The child's transcript is never copied into a parent turn.

## Recovery and request ownership

Restart and nonterminal daemon shutdown preserve unclaimed queued input and
its correlated queued root command. Claimed input, including injected steers,
and uncertain running operations are interrupted instead of replayed. A
terminal root stop or failure also interrupts queued work. Every transition
is scoped to that root; retained children become idle without changing other
roots' work or reservations.

Invalid input fails that input and settles its receipt or child turn without
terminating the session or retrying the bad payload. Ordinary failed child
execution keeps its existing bounded retry policy. A turn journal starts
empty before kernel acquisition or input preparation.

Cancelling a child interrupts the active input; separately queued follow-ups
remain runnable. A subtree stop/delete settles the durable turn before its
worker exits, so a late completion cannot revive it. An unexpected child
commit failure fails its root and interrupts outstanding claims.

Actor calls own one reply. Queries and unadmitted actor calls can be skipped on
cancellation. Accepted protocol commands use daemon/root supervision even after
the requesting connection disappears; callers may stop waiting independently. Results and mutable
arguments transfer ownership across that boundary. Bounded database claims
must resolve once started so the caller knows whether it owns settlement.
Cancellation after a mutation starts does not prove that it had no effect.
Shutdown settles pending replies before closing resources whose workers may
be awaiting those replies, and continues draining while workers exit.

## Transcript and prompt ownership

The turn worker journals each original message before focusing, decay, or
compaction can alter the model view. Every journal message receives an agent-local
raw sequence; derived summaries retain the highest covered sequence. Root and
child commits append these raw deltas and compactions atomically with their
existing lifecycle transitions. Explicit idle compaction uses the same raw
coordinate mapping. Clearing or rewinding also resets the live journal.

History reads freeze an upper sequence and copy current-turn message headers
under the node mutex, then read committed rows one at a time. Bodies and nested
slices are immutable to readers. Final tool timing/status replaces its metadata
slice after the batch settles, before compaction or commit. Provisional messages
are marked with their turn identity and are never counted twice after commit.

Environment assembly happens once at the start of each root or child turn.
Applied source metadata and the explicit skill catalog share that snapshot;
context inspection does not rescan files and claim they were already applied.
No filesystem watcher or mid-turn prompt mutation is involved.

## Host operations

- Same-path file mutations serialize through the workspace coordinator;
  unrelated paths proceed concurrently.
- Shell commands take no lock. They run concurrently with each other and with
  edits, as in Prime; their authority (a writer capability scoped to the root)
  is checked at admission. Keeping parallel editors off the same files is the
  parent's decomposition job, not the coordinator's.
- A cell's 30 s wall clock charges Starlark compute only. Time inside host
  calls (shell, permission prompts, `agents.wait`, MCP) is not counted; each
  host call is bounded by its own limit and by turn cancellation.
- `models.batch` fans out stateless calls and returns results in input order.
- MCP calls reserve existing operation capacity and serialize per server without
  workspace writer locks. Tool deadlines include the server queue. Immediately
  before transmission they recheck the live manager/catalog generation, exact
  definition, permission policy, and every issuer grant in the delegation chain.
  Disable, reconnect, replacement, and root shutdown cancel queued and active
  work. An interrupted transmitted effect is never automatically replayed.
- A root session owns the MCP manager behind a mutex. RLM hosts and protocol
  adapters resolve that owner at use time; runtimes keep no duplicate pointer.
- MCP tool errors, RPC failures, cancellation, and output-storage failures
  settle the same durable operation and release its reservation. If content
  storage fails, a small inline failed result preserves settlement.
- Every managed process belongs to a root and is cancelled on root shutdown.

Callbacks copy state under a mutex, release the mutex, then invoke external
code. The repository’s analyzer and race tests enforce this ownership rule.

## Kernel containment

Kernel cells have limits for Starlark steps, host requests, wall time, memory,
captured output, and frame size. Workers receive an allowlisted environment,
closed unintended descriptors, and no daemon or provider credentials. Useful
work crosses the typed host boundary.

Shell and kernel subprocesses run in managed process groups. This is
operational containment, not a security sandbox against another hostile
process already running as the same OS user.

## V2 connections and views

Adapters own framing, deadlines and connection cancellation. Every WebSocket
control/data write holds the same write lock; fragmented messages have a total
size cap. Each connection has bounded requests, output bytes and subscriptions.
A slow consumer loses its connection and must replay or resynchronize.

Command acceptance commits the operation and required input before replying.
Blocking provider preparation, shell/integration work, compaction and root
shutdown execute in supervised workers; actors admit and apply completions.
Network acceptance allocates no per-retry receipt. The Go wait convenience uses
lifecycle wakeups plus authoritative status polling without consuming UI events.

Snapshots and their event cursor share a SQLite transaction. Question registry
updates hold the registry lock through event commit; snapshots hold it through
SQL capture and question copying. Answers wake the agent only after the answer
event commits. Replay validates retention and reads envelopes in one transaction.
Subscriptions poll every 50 ms, deliver strictly after the selected cursor, and
report replay failures. A retiring stream cannot remove a replacement stream.

History revisions change on destructive edits, while collection revisions change
with collection rows. Pagination rejects stale revisions instead of mixing views.
Configuration writes serialize revision comparison, fresh patch application and
atomic replacement. Provider flows and terminals are ephemeral; restart interrupts
login flows and terminal input is never automatically retried.

## TypeScript connection and view ownership

`WhipClient` owns one native transport, pending RPC table, reconnect generation,
heartbeat and waiter-driven command polling. Each connection attempt has an epoch;
late readers, initialization and heartbeat continuations cannot revive a closed
client or mutate a replacement connection. Request and outbound buffers use the
daemon's negotiated bounds. Closing rejects pending queries and subscriptions;
it does not cancel accepted execution.

Command handles retain frozen request bytes and runtime/client/command identities.
Application storage commits identity-only recovery metadata before submission.
Acknowledgement loss remains uncertain until status resolves it; absent commands
are retried only explicitly. Cancellation waits for original admission before
sending its exact target. Concurrent waiters share status lookups and wakeups,
without retaining a permanent poller for historical commands.

A subscription registers routing before admission and owns one bounded async
iterator. Abort during admission retains cleanup until the late acknowledgement
can be unsubscribed; uncertain admission refreshes the connection. Stream IDs,
root IDs and decimal sequence counters guard delivery. A slow consumer or gap
fails explicitly rather than dropping deltas. Views own snapshot/subscription
replacement and immutable bounded state; React only subscribes. Notification
batching does not discard events. Local UI drafts/layout are application-owned.

Permission decisions are typed requests from trusted clients. The live resolver
claims each permission once under its mutex and retains the claim until the
entire dispatcher invocation settles; consuming a channel value does not release
it. This includes early answers before waiter registration. Invocation cleanup
bounds retained state for both built-ins and MCP. The durable ledger revalidates
execution authority before resuming an operation. Decision replies acknowledge
the handoff; the revalidation and operation outcome settle asynchronously.
There is no client enrollment or shared signing nonce to serialize. An uncertain decision is reconciled through pending permission state,
not automatically replayed. Provider secrets and terminal input never enter a
replay queue. HTTP transfer lifetimes are bound to the connection as well as the
caller's abort signal.

## React application lifetimes

The [frontend guide](frontend.md) explains why these ownership boundaries exist
and how app features should use them. This section records their lifecycle rules.

The rules in this section describe the web renderer. The native companion uses
the same SDK with the narrower [mobile lifetime rules](#native-companion-lifetimes)
below.

`packages/app/src/runtime.ts` owns the current SDK client, session-list view,
TanStack Query client, root-view leases and local command waiters. Host attachment
increments an application epoch. Late connections, acceptance callbacks and
terminal outcomes from an older epoch cannot mutate the replacement host. Detach
aborts local waits, disposes views/listeners and clears query data before closing
the old client; daemon work keeps its existing supervision.

Components lease one shared SDK `SessionView` per root. Leases release
idempotently, and a zero-user view expires after 30 seconds; at most four views
are retained by the application, within the daemon's 16-subscription limit.
Admission never evicts an actively observed root. Route preloading does not open
a root subscription. React StrictMode and overlapping components share the lease
instead of creating independent event reducers. The SDK alone owns replay,
snapshot replacement, sequence checks and history revision invalidation.

TanStack Query handles explicit host reads with automatic request/mutation retries
and browser-online heuristics disabled. A new daemon connection invalidates host
reads; detach clears them. Mutation helpers use SDK command identities and
acceptance/outcomes. Uncertain delivery holds the matching draft against a new-ID
resend, and only an authoritative absence enables explicit original-request
retry. Permission acknowledgement loss is reconciled by refreshing the pending
ledger before another decision. No reconnect path submits a draft, credential or
terminal input automatically.

Drafts are keyed by runtime/root/recipient and stored separately from identity-only
command recovery. The application bounds drafts by count and bytes and batches
persistence for 150 ms, flushing at teardown. Acceptance clears only the matching
submission/recipient; a late result cannot unlock a newer submission. Browser
storage adapters serialize concurrent recovery writes, with visible memory-only
fallback when persistent storage is unavailable. The browser shell owns cross-tab
storage coordination; the SDK does not assume localStorage is transactional.

Appearance has no daemon execution lifetime. The document and portal host share
one theme, and theme changes update a fixed variable allowlist without remounting
sessions. Code highlighting is lazy and bounded to 16 KiB of text and 4,096 tokens;
theme switches restyle retained nodes rather than tokenizing again. Transcript
virtualization keeps focused/selected rows mounted, distinguishes following the
latest output from reading older rows, and uses history identities for paging.
One TanStack Virtual instance owns end anchoring and dynamic row measurements at
every history size; switching rendering modes or also applying manual prepend
offsets would lose or double-adjust the reading position. The loaded-production
regression in `apps/web/scripts/performance.mjs` checks four successive prepends,
selection across an 8,000-pixel scroll, and cached recipient switches against
10,000 stored messages and 100 retained child agents.

The corresponding ownership regressions are in `packages/app/test/runtime.test.ts`,
`composer.test.tsx`, `timeline.test.tsx`, and `requests.test.tsx`. Component/CSP and
packed-consumer checks live under `packages/ui/tests`; the production fake-daemon
browser workflow suite lives at `apps/web/scripts/browser.mjs`. Manual device and
screen-reader gates are tracked separately in the accepted web plan.

Session tabs are navigation metadata, not view leases. `SessionTabs` publishes an
immutable bounded window record; the router remains the sole active-root authority.
Tab mutations cannot submit commands. Late create/fork completions check the
originating route and client before selecting their result. Root and child load
errors stay in their own view. A single visible-window TanStack Query batches
`sessions.summaries` for all open IDs; lifecycle wakeups coalesce at 250 ms and
steady polling runs every two seconds. Background labels never acquire root views.

`CompositionStore` owns transient upload controllers and scoped attachment refs
independently of mounted composers. Its serial queue bounds source copies; unmount
is not cancellation. Removing a file, replacing the host or disposing the runtime
aborts the owned transfer. Submission tokens prevent late acceptance from clearing
newer drafts. `ReadingPositions` retains only bounded row/revision/offset/follow
hints; TanStack Virtual remains the single scrolling authority. Expired closed-tab
metadata releases associated reading hints, and view eviction never deletes drafts.

## Native companion lifetimes

`apps/mobile/src/runtime/runtime.ts` owns one SDK client, one QueryClient and one
root view with at most one selected child. Replacing the host synchronously
increments the epoch, aborts local waits, clears scoped state and closes the old
client before awaiting cleanup. Old continuations cannot publish into the new
host. Releasing navigation leases never cancels accepted daemon execution.

The bootstrap owns the native AppState listener outside React. Actual backgrounding
flushes queued draft writes where possible, cancels Query reads and calls SDK
`pause()` to stop connection, heartbeat and command-observation timers. A temporary
inactive state does not detach. Foreground resume validates the same runtime and
reconciles unresolved identities before enabling actions. `close()` remains
terminal. Critical storage writes happen before network admission; a final
background callback is not assumed to run before process death.

The native recovery adapter commits command identity and recipient/draft-revision
correlation in one SQLCipher transaction before sending. Draft text is a separate
bounded encrypted record. A failed durable write blocks admission; unresolved
records are not evicted to admit another send. Restored metadata can check the
original outcome but cannot recreate or automatically replay a request body.
Permission decisions use their typed status namespace and retain their own
decision identity through uncertain replies.

One foreground Attention query observer serves every route and the tab badge.
Screen focus and explicit refresh join an existing request; hidden screens do
not create extra polling loops. Background/host cleanup cancels the shared read.
Catalog polling follows the focused Sessions list, and history remains owned by
SDK views. See [the frontend guide](frontend.md#native-mobile-companion) for
package boundaries and [mobile setup](mobile.md) for the private network contract.
Native runtime, storage, decision and Attention tests live under
`apps/mobile/src`; device evidence is tracked separately from these unit checks.
