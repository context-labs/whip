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
work (`submit`, `steer`, `goal`, `schedule`) is claimed one row per turn.

Steer-class mail and human steers are injected at the running turn's next
loop boundary by the one delivery engine in `AgentSession.RunTurn`; there is
no stream interruption. Queued mail starts a mailbox-triggered turn whose
input is a bounded digest (excerpts, never bodies). Ten messages to a busy
node produce one digest, not ten turns. Messages become `delivered` only when
the turn that showed them commits, so a failed turn redelivers them.

Agent completion posts an `agent.completed|failed|cancelled` message to the
parent with a 160-byte preview and an evidence handle; it never copies the
child's transcript into a parent turn.

## Host operations

- Same-path file mutations serialize through the workspace coordinator.
- Unrelated paths can proceed concurrently.
- Shell and unknown mutations take broader workspace authority because their
  effects cannot be proven path-local.
- `models.batch` fans out stateless calls and returns results in input order.
- MCP calls serialize per server and obey connection/tool deadlines.
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
