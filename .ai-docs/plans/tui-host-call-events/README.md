# TUI host-call lifecycle support

Status: superseded September 9, 2026 by [PLAN.md](PLAN.md). Kept as history.

Source review: [September 9, 2026 findings and open question](REVIEW.md).

## Outcome

The TUI consumes `stream.cell.host.started` without an unsupported-event notice.
The REPL panel shows the host operation while it runs and updates that same row
when it completes, fails, or is cancelled. Reconnecting and switching agents
preserve correct attribution and avoid duplicate rows or stale running states.

## Confirmed findings

- The Docker entrypoint starts the daemon, web service, and TUI inside the same
  container using the same Whip binary. The inspected running container reported
  matching daemon/client builds.
- `internal/protocol/events.go` registers 15 stream kinds. The TUI handles every
  kind except `stream.cell.host.started`.
- `internal/daemon/recursive_runtime.go` emits start and completion events with
  the cell's tool-call ID, turn ID, and host invocation ID. The completion kind
  remains `stream.cell.host`; it includes duration and outcome.
- `internal/tui/client.go` prints the warning for the start event, while
  `internal/tui/repl_panel.go` only appends completed host operations.
- `packages/sdk/src/executions.ts` already handles both events for web/desktop.
  Its lifecycle and identity rules provide the reference behavior.
- An isolated Go overlay test reproduced the warning and verified that completion
  still records the operation. The daemon host-call presentation test passed.
  Existing TUI tests omit host-start events.

## Implementation sequence

### 1. Cover the missing event in the TUI client

Add a regression test that delivers a real-shaped start/completion sequence
through the TUI event path. Assert that neither event adds an unsupported-event
message, both reach REPL state, and normal output continues to render.

Handle `stream.cell.host.started` alongside `stream.cell.host` in
`applyClientStream`. The REPL reducer owns their presentation. Add a contract test
that exercises every registered stream kind with a valid minimal fixture and
checks for unsupported-event fallthrough. Keep diagnostics for genuinely unknown
event kinds.

Primary files: `internal/tui/client.go`, `internal/tui/client_test.go`.

### 2. Track one row per host invocation

Extend the existing REPL state with host invocation identity and explicit status.
Scope identity to its agent, turn, and cell occurrence; operation names are display
text and must never be used to match calls. Retain turn identity in cell state and
distinguish new cells when providers reuse tool-call IDs, including within a turn.

- Start creates a running row using the existing name and argument summary.
- Completion updates the matching row with duration and completed/failed/cancelled
  status. An error result takes precedence over a successful status.
- Duplicate start or completion delivery is idempotent. A late start cannot
  reopen a completed invocation or closed cell.
- Preserve completion-only events from older history when their owning cell can
  be identified. Never combine calls merely because their names match.
- For missing or truncated prefixes, attach an event only when its ownership is
  unambiguous. Incomplete evidence must not update a similarly named older call
  or fabricate a successful result.

Reuse the existing event stream and sequence deduplication. No new subscription,
dependency, wire event, or database migration is needed.

Primary file: `internal/tui/repl_panel.go`.

### 3. Render compact progress and settle incomplete operations

Keep host operations in their existing location beneath the cell's code. Show a
short textual running indicator using the theme's activity color. Replace it
with the daemon-provided duration on success; show failures with the existing
error styling and cancellations with a distinct text label. Text carries status
even when terminal colors are unavailable.

Use existing row clipping and layout helpers. Avoid adding transcript notices or
changing the panel's scrolling/focus behavior. Operations continue to update while
the REPL panel is hidden and are visible when it is reopened.

On cell/turn termination, cancellation, interruption, or worker restart, reconcile
any outstanding running rows using the available lifecycle evidence. Explicit
host outcomes take precedence; missing outcomes become interrupted or unknown
as appropriate, never assumed successful. A connection loss alone does not mean
the daemon stopped the operation. Reconcile on snapshot/resume without inventing
historical start times. Use explicit completion state when rendering replayed
cells, whose timestamps can legitimately be absent.

Primary files: `internal/tui/repl_panel.go`, existing lifecycle handlers in
`internal/tui/client.go`.

### 4. Validate lifecycle and replay behavior

Add focused tests covering:

- Running to success, failure, and cancellation; missing host completion followed
  by a terminal cell/turn event; worker restart.
- Multiple calls to the same operation, reused tool-call IDs across and within
  turns, and interleaved root/child events.
- Duplicate delivery, snapshot overlap, fresh attachment during an active call,
  reconnect after completion, and switching the visible agent.
- Legacy completion-only events and incomplete/truncated event history.
- Hidden/open REPL panel, narrow/wide terminal layouts, long summaries/errors,
  and finished replayed cells with no local timestamps.

Use one deterministic daemon-to-TUI integration scenario with a mock provider
whose cell lists files, runs a controlled slow shell command, then completes.
Include failure and interruption variants. Verify actual emitted events rather
than substituting a text-only inference response.

Run focused TUI/daemon tests, the affected packages under the race detector, and
the repository's normal `task check` gate.

### 5. Check the rebuilt Docker experience

Build the current working tree with `task onboarding:docker` in a fresh isolated
test container, preserving any container the user is currently using. The launcher
currently fixes the published port at 4000. If that port is occupied, use its
existing Dockerfile/build arguments and launch the freshly built image separately
with an available loopback port for acceptance testing.

Use Computer to exercise the TUI in a terminal with deterministic provider
fixtures and capture screenshots of running, completed, failed, and cancelled
operations at narrow and wide terminal sizes. Check chat with the panel hidden,
then open the REPL panel. Reattach during a controlled slow call and confirm that
completion updates the correct row without duplicate notices. Inspect the web
view of the same test session as a parity check.

Record commands, results, and screenshots in an acceptance note beside this plan.
Update the current TUI/REPL documentation with the supported lifecycle behavior.

## Completion criteria

1. Real host calls never produce the reported unsupported-event warning.
2. Each identifiable invocation has one correctly scoped row that visibly changes
   from running to its known outcome.
3. Replay, reconnect, and agent switching do not duplicate or misattribute calls,
   and terminal lifecycle events do not leave false running indicators.
4. Legacy and incomplete history remain usable without invented timing or status.
5. Automated checks and screenshots from the freshly built Docker TUI confirm the
   behavior, including a real tool-executing turn.
