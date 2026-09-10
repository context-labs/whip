# TUI host-call lifecycle

Status: planned, September 9, 2026. Implementation has not started.

Supersedes [the first plan](README.md) and resolves [its review](REVIEW.md).
Both stay as history. This plan was written against commit `e92ff15bf`
(`provider work`) on `codex/provider-onboarding`.

## Decisions

Recorded September 9, 2026 with the user.

- **No TUI reconnect.** The TUI holds one live subscription per process. On
  connection loss it prints "Use /quit and relaunch" and stops
  (`internal/tui/thin_update.go`, `msg.closed`). Every attach starts from a
  fresh snapshot. The SDK's replay-gap recovery (missing prefixes, uncertain
  prior cells, `active_turns` confirmation) has no input in the TUI and is out
  of scope. Rows keyed by invocation ID are the foothold reconnect would need
  if it is ever added.
- **No Docker screenshot matrix**, web parity check, or reattach procedure.
- **Fix reused tool-call IDs** in the REPL reducer as part of this change.

## Why the first plan shrank

Facts from the code that the first plan and review did not weigh:

- The warning is the `default:` branch of `applyClientStream`
  (`internal/tui/client.go`). `stream.cell.host` already returns early from
  the same switch; the started kind was simply never added.
- Live events reach the REPL reducer through `recordClientStream` for every
  agent, before the visible-agent gate. Hidden children are already tracked.
- Host calls run synchronously inside the kernel eval loop
  (`internal/rlm/kernel.go`, `host_request`). A completion always follows a
  start unless the daemon dies, and the cell's `stream.tool.completed` always
  arrives after the last host completion. Reconciliation therefore reduces to
  "a host still running when its cell completes is unknown."
- The kernel always sets `HostStatus` to `completed`, `failed`, or `cancelled`.
  Cancelled calls carry `context canceled` in `Result`. The current renderer
  keys on `Result`, so it would paint cancellations as failures.
- The daemon's truncation path (`recordStreamEvent`, `internal/daemon/session.go`)
  keeps `HostStatus` and `InvocationID` and clears only oversized `Result`,
  `Args`, `Text`. `ContentEventPayload` embeds `StreamEvent`, so the TUI's
  existing decode already sees those fields.
- `InvocationID` is `<eval id>:<counter>` per kernel. Scoped to the agent it is
  unique. No new identity model is needed.
- `find()` in the reducer matches by tool-call ID across the agent's whole
  history. A provider that reuses `call_0` every turn overwrites the old cell's
  code today. This is a live bug independent of host events.

## Change

### 1. Transcript accepts both host kinds

File: `internal/tui/client.go`, `applyClientStream`.

- Add `"stream.cell.host.started"` to the existing
  `case "stream.cell.host": return true, nil`.
- Skip the "[Live output omitted ...]" notice when `kind` has the
  `stream.cell.host` prefix. The panel shows the outcome, and the untruncated
  event prints nothing in the transcript either. The truncated payload still
  decodes as `StreamEvent`, so the owner check and switch handle it.

### 2. Reducer keeps one row per invocation

File: `internal/tui/repl_panel.go`, `replApply` and `replHost`.

```go
type replHost struct {
	id, name, summary, duration, err string
	status string // running | completed | failed | cancelled | unknown
}
```

- `find()` walks cells from newest to oldest.
- `stream.tool.call` and `stream.tool.started` for `rlm_exec`: when the found
  cell is `finished`, append a new cell instead of reusing it. This is the
  reused-ID fix. Streaming partial `tool.call` updates still hit the open cell.
- `stream.cell.host.started`: if the cell exists and has no host with this
  `InvocationID`, append `{id, name, summary, status: "running"}`. A duplicate
  start, or a start after completion, is a no-op.
- `stream.cell.host`: find the host by `InvocationID` from the end and update it
  in place. If none matches (legacy completion-only event, or empty
  `InvocationID`), append. Status is `event.HostStatus`; when empty, use
  `failed` if `Result` is nonempty, else `completed`. Never derive status from
  `Result` when `HostStatus` is set.
- `stream.tool.completed` for `rlm_exec`: before the existing handling, set
  every host still `running` to `unknown`.
- No change to `replApplySeq`, `replRebuild`, or `replRestart`. Seq dedup already
  makes snapshot overlap idempotent. The restart marker stays a marker.

### 3. Renderer keeps status visible

File: `internal/tui/repl_panel.go`, `replCellRows`.

Build `prefix = "→ " + name + "(" + summary + ")"` and a status suffix, then clip
the prefix to `content - width(suffix)` and append the suffix. A long summary
must not push the status off the row.

| status    | suffix                | style                  |
|-----------|-----------------------|------------------------|
| running   | ` …`                  | running gutter color   |
| completed | ` <duration>`         | `st.dim`               |
| failed    | ` ✗ <err>` or ` ✗ failed` when err is empty | `st.fail` |
| cancelled | ` cancelled`          | `st.warn`              |
| unknown   | ` unknown`            | `st.dim`               |

Text carries the status on every row, so it reads without color.

Skipped: per-host elapsed time. The cell header already shows elapsed. Add a
`startedAt` field if a slow host call needs its own clock.

### 4. Tests

`internal/tui/repl_panel_test.go`, new `TestReplHostLifecycle`, fixtures shaped
like the `activity-*` cases in `internal/daemon/v2_sdk_repl_test.go`:

1. Started renders `→ files.read(path=README.md) …`. Completion with the same
   `InvocationID` keeps one row and shows `8ms` with no `…`.
2. `HostStatus: failed` with `Result` renders `✗`.
3. `HostStatus: cancelled` with `Result: "context canceled"` renders `cancelled`
   and no `✗`.
4. Duplicate started keeps one row. Started after completion changes nothing.
5. Completion with no prior start appends a row.
6. `stream.tool.completed` with a running host renders `unknown`.
7. `c1` finishes, then `tool.call` and `tool.started` for `c1` again produce
   `In [2]` and leave the first cell's code untouched.
8. At a narrow panel width, the ANSI-stripped row for a long summary still ends
   with the status text.

`internal/tui/client_test.go`:

- `applyClientStream` with `stream.cell.host.started`, and with a truncated
  `stream.cell.host` (`protocol.ContentEventPayload{Truncated: true}`), appends
  no transcript block.
- Contract: every `stream.*` kind in `protocol.EventPayloads()` fed a minimal
  `StreamEvent{ID: "x"}` appends no block containing "unsupported". This is the
  regression guard for the reported bug class.

Run:

```bash
go test -race ./internal/tui
```

```bash
task check
```

### 5. Docs

- Header comment of `internal/tui/repl_panel.go`: "each host call as it starts
  and its outcome" in place of "each host call with its duration".
- `docs/features.md`, REPL panel bullet under the TUI section: same wording.

## Out of scope

- Reconnect and replay-gap recovery. See Decisions.
- Docker acceptance beyond an optional manual run: `task onboarding:docker`,
  send a prompt that calls a host operation, confirm no warning and a row that
  settles.
- The omission notice printing for hidden agents on non-host kinds. Pre-existing
  and unrelated; fix separately by checking the owner before the notice.
- An unattributed row for a completion whose cell is unknown. Today it is
  dropped; that stays.

## Completion criteria

1. Real host calls produce no unsupported-event warning.
2. Each invocation is one row that moves from running to completed, failed,
   cancelled, or unknown, with the status text visible at narrow widths.
3. Reused tool-call IDs open new cells.
4. `go test -race ./internal/tui` and `task check` pass.
