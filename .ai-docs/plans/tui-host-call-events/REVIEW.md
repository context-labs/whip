# Plan review — September 9, 2026

Reviewed [the implementation plan](README.md) against commit
`e92ff15bfbc7c24a46531d42253fe7da97db87dc` (`provider work`). The working tree was
clean at the start of review.

The proposed event handling and invocation-based identity are supported by the
current code. Before implementation, make the following recovery and payload
rules explicit. These are plan findings, not claims that an implementation has
been completed or tested.

## 1. Specify uncertainty and recovery across a replay gap

**Priority: high. Plan sections 2–3.** Sequence deduplication alone cannot establish
that an observed invocation is still running. For example, a client sees call A,
disconnects, and misses A's completion and the next cell's prefix. A snapshot can
then contain a new host start with the same provider tool-call ID in the same
turn. Keeping A marked running can attach the new invocation to A's old code.

The SDK already tests this case in `reconnect cannot attach a reused tool ID with
a missing prefix to an uncertain prior cell`. Its disconnect path marks ongoing
evidence unknown; snapshot reconciliation restores running state only when it can
match the observed occurrence or invocation. Older snapshot events can provide
that confirmation even when ordinary event application would skip their sequence.

Record this distinction in the plan: connection loss makes the client's evidence
uncertain; it does not establish cancellation or interruption at the daemon.
Use exact `active_turns` plus occurrence/invocation evidence for recovery. An
active turn alone does not prove that a particular host call is still running.
The TUI currently does not retain `snapshot.ActiveTurns` in `applyClientSnapshot`,
and connection-state handling lives in `internal/tui/thin_update.go`; include
these paths explicitly in scope. Add the same-turn, missing-prefix reconnect
case and the overlapping-snapshot recovery case to the TUI tests.

Sources: [TUI connection updates](../../../internal/tui/thin_update.go),
[TUI snapshot application](../../../internal/tui/client.go),
[SDK recovery](../../../packages/sdk/src/executions.ts),
[SDK connection state](../../../packages/sdk/src/state.ts),
[SDK regression cases](../../../packages/sdk/test/executions.test.ts).

## 2. Preserve truncated payload metadata in live and snapshot reduction

**Priority: high. Plan sections 1–3.** Missing history and an omitted event body
are different cases. `ContentEventPayload` retains stream identity and available
small fields with `truncated: true` and a content handle. Currently both TUI REPL
paths decode into `StreamEvent`, losing the truncation flag. Separately,
`applyClientStream` appends a generic omission notice before checking the event
kind or visible agent. Merely adding the new kind to its switch will leave that
path unchanged, including notices for hidden child operations.

Require both host event kinds to retain envelope metadata through live reduction
and snapshot replay. Missing error text must not turn an unknown or failed result
into success. Show available lifecycle evidence beneath the cell and mark missing
detail there, without automatically fetching the content body. Test large host
errors, identity-preserving truncated events, old content-only envelopes, and
events belonging to a non-visible agent.

Also pin outcome precedence with fixtures: cancelled hosts commonly include a
nonempty `result` (`context canceled`), so generic error handling must not relabel
them failed. The SDK conservatively marks truncated completions unknown unless
failure/cancellation is known, even when `host_status: completed` is still present.
The plan's “explicit host outcomes take precedence” wording is broader than that
behavior. Recommended resolution: preserve the explicit outcome when available
(with errors overriding success), use unknown when outcome evidence is missing,
and document this deliberate difference from the SDK in the test expectations.

Sources: [content envelope](../../../internal/protocol/events.go),
[`recordStreamEvent`](../../../internal/daemon/session.go),
[TUI stream handling](../../../internal/tui/client.go),
[TUI replay decoding](../../../internal/tui/repl_panel.go),
[SDK host outcome precedence](../../../packages/sdk/src/executions.ts).

## 3. Do not treat a scratch-restore notice as a termination boundary

**Priority: high. Plan section 3.** `recordScratchRestore` publishes its audit event
asynchronously; its comment explicitly says ordering against the turn does not
matter. The persisted `scratch.restored` payload has agent identity but no turn,
cell, or invocation identity. It can arrive after a restored worker has begun a
new host call. Settling all currently running rows when this notice arrives would
incorrectly interrupt that new call.

Keep the notice as a restart marker. Settle calls using attributable cell/turn
termination or authoritative recovery evidence; the notice alone cannot establish
which invocation stopped. Add a test where a delayed restore notice arrives
after a new call starts and assert that the new call remains running. The SDK's
scratch-restore branch likewise adds a marker without settling host calls.

Sources: [`recordScratchRestore`](../../../internal/daemon/recursive_runtime.go),
[`RecordScratchRestore`](../../../internal/session/scratch.go),
[SDK restart handling](../../../packages/sdk/src/executions.ts).

## Acceptance and presentation corrections

- **Keep status visible under clipping.** The existing host renderer appends
  duration/error after the argument summary and truncates the whole line. A long
  summary can therefore hide the entire status. Reserve width for status before
  clipping the summary; assert the status remains in ANSI-stripped narrow output.
  Source: [`replCellRows`](../../../internal/tui/repl_panel.go).
- **Define a reattachment procedure that preserves execution.** Quitting the
  onboarding container's primary TUI makes its entrypoint stop the daemon and
  the launcher remove the container. For reconnect acceptance, keep that TUI
  alive and detach/reattach a second client, or use a dedicated fixture entrypoint
  with an independently managed daemon. Source:
  [onboarding entrypoint](../../../scripts/docker/onboarding-entrypoint.sh).
- **Configure alternate-port allowlists.** Publishing a different loopback port
  requires `WHIP_ALLOWED_HOSTS` to include that external address; its image
  default allows only port 4000. Update `WHIP_ALLOWED_ORIGINS` too if the fixture
  uses cross-origin browser access. Same-origin requests are already accepted
  once the host is allowed. Sources:
  [onboarding Dockerfile](../../../scripts/docker/onboarding.Dockerfile),
  [network checks](../../../internal/daemon/network.go).
- **Name the integration gate.** The existing TUI integration file uses
  `//go:build integration`; `task check` does not enable that tag, and
  `task acceptance` selects tests by name. Give the new scenario an explicit
  tagged test command and include its name in the appropriate acceptance selector.
  Sources: [TUI integration test](../../../internal/tui/client_integration_test.go),
  [task definitions](../../../Taskfile.yaml).

## Open presentation question

When a host event cannot be attributed safely after truncated history, should
the TUI use generic activity or show an explicitly unattributed operation row?
The recommended default, presented to the user during review, is generic activity
to match the SDK. This is a recommendation, not a recorded user decision.

## Evidence and documentation

This was a source-level review. No runtime tests, Docker builds, or UI acceptance
were run, and the earlier overlay-test/container observations in the plan were
not independently reproduced. Preserve their commands, fixture, build identifier,
and output if available; otherwise leave them identified as prior observations.

The current TUI behavior is documented in the REPL entry of
[the feature map](../../../docs/features.md). Update that entry during
implementation. Keep this review and the plan as historical rationale, with
acceptance results recorded separately as already proposed.
