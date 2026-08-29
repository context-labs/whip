# Concurrency: where Go channels earn their keep

whip's agent runs concurrent work — parallel tool calls, background
subagents, a streaming TUI — and the design leans on channels for the parts
that are awkward in the reference harnesses (pi and opencode are TypeScript).
This doc explains the two channel patterns and why they're idiomatic in Go.

References that motivated them:

- pi `packages/agent/src/harness/tools/file-mutation-queue.ts` — per-path
  promise-chain serialization.
- pi `packages/agent/src/agent-loop.ts` `executeToolCallsParallel` — `Promise.all`
  over a tool-call batch.
- opencode `packages/core/src/background-job.ts` — a registry of `Deferred` /
  `Scope` / token for background subagents.

## 1. Per-path file-mutation lock = a 1-capacity channel

The problem: the model can emit several tool calls in one turn (e.g. write
`a.go`, edit `a.go`, write `b.go`). The writes to `a.go` must not interleave;
the write to `b.go` should run in parallel.

pi solves it with a `Map<path, Promise>` where each new call chains onto the
previous promise (`currentQueue.then(next)`). It's correct but subtle — the
registration step itself needs a promise chain to avoid a check-then-set race.

In Go the whole thing is a buffered channel per path
(`internal/agent/filelocks.go`):

```go
ch := make(chan struct{}, 1) // one per canonical path
ch <- struct{}{}             // acquire — blocks while the buffer is full
defer func() { <-ch }()      // release — drain the buffer
```

The buffer of 1 is the lock. First acquirer fills it and proceeds; later
acquirers block on send until the holder receives. No explicit unlock, no
registration race, no promise plumbing — the channel *is* the mutex, and the
compiler checks direction. Two spellings of the same file share a lock via
`filepath.Abs` + `filepath.Clean`. `bash` (side effects not attributable to a
path) takes a single global channel.

The batch itself is fanned out with a goroutine per call and a buffered results
channel (`runTools` in `internal/agent/agent.go`); results land back in call
order because the chat API matches tool results to call IDs. A `sync.WaitGroup`
+ `close(outCh)` terminates the collector — the fan-out/fan-in idiom.

## 2. Background subagents = one channel close, many waiters

the `subagent` tool with `background: true` launches a subagent that runs concurrently with
the parent and reports back later. The registry (`internal/agent/background.go`)
is opencode's `BackgroundJob` translated to channels.

opencode tracks each job with a `Deferred<Info>` per waiter plus a closeable
`Scope` and a token for settle-once. Go collapses the broadcast primitive to a
single channel close:

```go
type BackgroundTask struct {
    // ...
    Done chan struct{} // closed once on settle; <-Done() wakes every waiter
}

func (r *taskRegistry) settle(id string, s TaskStatus, report string) {
    // record final state, then:
    close(t.Done) // one close; all <-Done() receivers proceed together
}
```

Closing a channel is the one operation that wakes **all** receivers at once, so
the tool caller, the TUI's `OnChange` redraw, and `/subagents` all observe
completion for free — there's no per-waiter state to manage. Cancellation is
`context.WithCancel` on the subagent's turn; the result is delivered back into
the parent as a **steered message** (channel close → `Steer`), so the model
sees the report on the next loop boundary without polling.

Persistence rides the same events: the registry's `OnRecord` hook runs on the
worker goroutine at start and settle, and the TUI uses it to upsert the task
into the session store. The trap is that a worker-goroutine callback must
**never read UI-goroutine state** — an early version read `m.sessionID`
directly and `-race` caught it. The fix is the one piece of shared state
published atomically: the registry holds an `atomic.Pointer[string]` session
id (`SetSessionID`, written by the UI goroutine at persist/resume/clear/fork),
and `OnRecord` receives the id as an argument. No lock, no closure over `m`.

The second trap is a worker-goroutine callback that **blocks while touching
the registry mutex**: `broadcast` used to walk subscribers under `r.mu`, and
the TUI's task-view subscriber funnels events through `prog.Send`, which
parks when the UI queue is backed up. The UI goroutine itself takes `r.mu`
via `List`/`Get` to render the dock — worker holds `mu` waiting on the UI
queue, UI waits on `mu`: an ABBA deadlock that froze the whole TUI (caught in
the wild from a goroutine dump: UI parked in `List` ← `tasksDock` ← `Update`,
worker parked in `prog.Send` ← `openTask`'s subscriber ← `broadcast`). Two
rules now keep the cycle impossible:

1. `broadcast` snapshots the subscriber slice under `r.mu`, then runs
   callbacks **after** unlocking — a parked subscriber can hold its own worker
   goroutine, but never the registry mutex.
2. The TUI's subscriber callbacks never block the worker: `sendTaskMsg` (and
   the `OnChange` redraw) detach `prog.Send` into its own goroutine. The task
   pane resyncs from the stored `Report` on the next paint, so a reordered
   interim frame is cosmetic; stalling the subagent on the UI is not.

`TestBroadcastBlockingSubscriberCannotDeadlock` reproduces the original shape
(a subscriber parked on an unbuffered channel stands in for the wedged
`prog.Send`) and fails against the pre-fix `broadcast`.

### What this buys over the TS versions

- **No leak bookkeeping.** The channel semaphore and the Done-close both have
  obvious owners and exits; there are no dangling promises or un-awaited
  deferreds.
- **Backpressure is the buffer size.** The results channel is sized to the
  batch; the per-path lock's buffer is 1. The capacity is the contract.
- **Race-checked.** `go test -race ./...` covers the fan-out, the lock, the
  broadcast, and cancel. The equivalent TS relies on convention.

## 3. MCP server readiness = the same close-to-broadcast, with a generation guard

`internal/mcp/manager.go` reuses the pattern for server connections: each
server has a `ready chan struct{}` closed **once** when its first connect
settles (success or failure), so a tool call blocks only on its own server
and `/mcp` never blocks at all. Two twists the task registry doesn't need:

- **Reconnects reuse the channel.** `ready` means "first attempt settled,"
  not "connected"; after a reconnect, callers check the session under the
  mutex instead of the channel. This keeps the close-once invariant
  unbreakable (the first implementation re-closed on reconnect and panicked).
- **Watchers carry a generation.** When a session drops, its watcher only
  flips the server to failed if `s.gen` still matches the connect that
  spawned it — opencode does the same check by client identity
  (`mcp/index.ts:443`), and it's what makes `/mcp <name> reconnect` safe
  against a stale close event arriving after the new session is up.

Calls into a server serialize through a 1-capacity `calling` channel (many
stdio servers are single-request-at-a-time), so "capacity is the contract"
applies twice per server: one channel for readiness, one for in-flight calls.

## Process safety (not channels, but the same "don't leak" instinct)

`internal/tools/bashrun/bashrun.go` tracks every spawned child in a registry
and `KillAll()` SIGKILLs the whole process group on exit, so an agent-started
server never outlives whip. The non-interactive path closes its output pipes
on process exit so a detached grandchild (`sleep 30 &`, nohup) can't hang the
agent waiting on pipe EOF.

## 4. LSP diagnostic waiters = per-file channel closes

`internal/lsp/manager.go` reuses close-to-broadcast for LSP push diagnostics:
`write`/`edit` send `didOpen`/`didChange` with a document version, then wait
on a channel registered under the file's path; the reader goroutine's
`publishDiagnostics` handler closes all of that file's waiters (a stale push
is harmless — the waiter re-checks the diagnostics cache and re-registers).
This replaces opencode's poll-with-timeout `waitForDiagnostics`
(`packages/opencode/src/lsp/client.ts`): no per-waiter goroutine, no polling
interval, and the wait is bounded by the tool's ctx plus a 1.5s cap. Two
twists the task registry doesn't need: the waiter list is keyed so a push
for file A never wakes file B, and the loop breaks on the edited file's push
plus one 50ms trailing wake (still deadline-bounded) for sibling frames —
gopls fans pushes out across the whole package, so sibling errors land a
tick after the edited file's.

## 5. Lifecycle hooks = immutable snapshots outside mutation locks

`internal/hooks.Manager` is fully built before publication and never mutated.
The TUI's `/cd` path discovers a replacement, then `Agent.SetHookScope` swaps
the runner and explicit working directory under a short `RWMutex` critical
section. Each lifecycle event snapshots those two values and releases the
lock before starting a subprocess. Existing events finish on the old manager;
new events use the new one. No command ever observes a half-reloaded config.

Parallel tool calls share the immutable manager safely. Commands for one event
run serially in declaration order, while different tool workers may run their
hook chains concurrently. `PreToolUse` runs before acquiring the per-path or
global mutation semaphore. The tool releases that semaphore before
`PostToolUse`/`PostToolUseFailure`, so a slow hook cannot hold a file lock or
form a lock/I/O cycle with another tool. Subagents inherit the runner snapshot
but carry their own working directory (including an isolated worktree).

Hook callbacks therefore follow the same rule as task callbacks: they may run
on tool-worker goroutines and consumers must be concurrency-safe. The TUI only
sends a message, headless mode serializes through its turn-owned encoder, and
ACP writes actionable outcomes to its synchronized event log.

`bashrun` also snapshots the process-wide `WHIP_*` child markers under an
`RWMutex` before merging each command environment. Publishing the first
session id therefore cannot race a background tool or hook subprocess spawn.
