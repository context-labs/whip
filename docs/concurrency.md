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
`rlm.maxWorkers` semaphore. Worker capacity is reserved before durable child
admission.

Children are retained identities, not goroutines treated as records. A live
node owns its cancellation context, provider loop, services, and kernel. The
recursive runtime owns the tree and closes a whole subtree exactly once.

The budget ledger limits active children, concurrent child turns, recursion
depth, tokens, cost, elapsed time, durable bytes, record count, operations,
and schedules/subscriptions. Child limits clamp inherited authority.

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

Human signing helpers serialize nonce use and preserve exact payload bytes. A
connection change while signing invalidates that attempt. Ambiguous signed
responses refresh the nonce and require explicit reconciliation; provider secrets
and terminal input never enter a replay queue. HTTP transfer lifetimes are bound
to the connection as well as the caller's abort signal.
