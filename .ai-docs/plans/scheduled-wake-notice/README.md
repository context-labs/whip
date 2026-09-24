# Upcoming scheduled-wake notice

Branch: `compaction-loop-and-ui-cleanup` (implementation and review complete; validation caveats below)

## Goal

Replace the schedule portion of the generic full-width accepted-input panel with
a compact, collapsed-by-default upcoming-wake disclosure in the conversation's
composer-width column. Show when the session is scheduled to wake and let the
user expand the exact prompt. A fired occurrence must never appear in this notice,
even if its admitted input is queued or running.

## Research and diagnosis

- `packages/app/src/conversation.tsx:237,379-395` selects non-chat inbox entries
  and renders their raw status/payload in a generic details panel outside the
  constrained composer. `input-presentation.ts:123-155` excludes only submit/steer
  variants from that panel. This is an execution-inbox view, not a schedule view.
- `packages/app/src/styles.ts:168-174,201-210` gives that panel no conversation
  width constraint and uses code-oriented preformatted text.
- Read-only local inspection on 2026-09-20 distinguished two occurrences:
  schedule 10 fired at 23:16Z; inbox 41 was consumed. The NEW screenshot matches
  schedule 11, fired at 23:22:30Z; inbox 44 was genuinely running when inspected.
  Thus the second screenshot is not evidence of a stuck client. The first reply's
  stale-state explanation does not explain the new screenshot. No session data
  was changed.
- `internal/daemon/scheduler.go:28-62` claims due occurrences.
  `internal/session/runtime.go:765-845` atomically advances last_fire and admits
  the schedule prompt with kind=schedule, emitting schedule.fired. This claim,
  not turn completion or a browser timer, is the disappearance boundary.
- `internal/session/session.go:773-839` retains fired one-shots in the schedule
  collection. Merely checking whether a schedule exists is insufficient.
- `internal/schedule/schedule.go:1-83` owns @at / @every and anchored recurrence.
  Do not introduce a second scheduler/parser in React or skip overdue slots by
  calculating the next future time from the browser clock.
- `internal/session/event.go:508-532` currently reads schedules in ascending ID
  order with a limit. `snapshot_view.go:154-199` adds count/byte bounds and can
  omit schedules. Filtering only root.schedules in the app can miss the next wake
  behind many historical fired schedules; partial absence is not proof of none.
- `packages/sdk/src/state.ts:517-550` refreshes authoritative snapshots after
  lifecycle events (100ms coalescing at :310). It currently has no dedicated
  schedule occurrence reducer. Keep recovery and freshness in the SDK.

## Existing design / prior art

- `docs/frontend.md:51-72`: reading first, quiet chrome, progressive disclosure,
  stable scroll and focus. This guide is authoritative over historical plans.
- `packages/app/src/composer.tsx:25-34,290-292`: shared 864px outer maximum and
  responsive gutters; agent and queue slots already share that container.
- `docs/frontend.md:1494-1512` and `packages/app/src/agent-dock.tsx`: plain,
  keyboard-accessible disclosure, muted text, bounded expansion. Reuse those
  treatments; no new design system primitive or dependency.
- `packages/app/src/details/session-controls.tsx:25-34,74-124`: existing schedule
  inspector and SDK collection paging remain the place to manage schedules.
- `docs/features.md:562,716` documents durable wakeups and session inspection.
  `docs/roadmap.md` has no existing upcoming-wake UI item.
- `docs/learnings/other-harnesses/exo.md:281-305` records anchored scheduling and
  record-before-deliver semantics. Preserve these; this is not a scheduler port.

## Proposed UX

Place one quiet disclosure immediately above the agent/queue/composer stack,
INSIDE the existing composer-width region. Do not create a viewport-width banner
or another prominent filled card. Root chat only: schedules wake the root, not
the selected child. REPL/trace and native mobile redesign are out of scope.

Collapsed:

    > [clock] This session is scheduled to wake up at 5:32 PM MDT

Expanded:

    v [clock] This session is scheduled to wake up at 5:32 PM MDT
        Message to be sent
        Continue user-authorized official Desktop beta release...

- Use viewer-local time with timezone; include the date for non-today occurrences.
  Use semantic time markup and an accessible full timestamp. No ticking countdown.
- Prompt is plain selectable text, preserving newlines but wrapping long tokens;
  no rendered Markdown/HTML, execution, or automatic opening of links. Bound the
  expanded scroller to roughly the existing dock size (20dvh / 200px).
- Default collapsed. Keyboard toggle, visible focus, aria-expanded, and stable
  disclosure identity use root + schedule ID + occurrence slot.
- Multiple pending schedules: one summary for the earliest wake plus '+N more';
  expansion lists occurrences in chronological order with their own prompts.
  Never claim an exact total or globally earliest item from partial data.
- Recurring schedules: show the next unclaimed occurrence. On fire the old
  occurrence disappears; the next occurrence may immediately replace it.
- Fired one-shots, deleted schedules, and admitted schedule inbox entries are
  excluded regardless of queued/running/consumed status.
- Overdue but not claimed: say 'Scheduled wake-up was due at …' while awaiting
  host confirmation. Do not pretend firing occurred because local time passed.
- Disconnected or refreshing-after-reconnect: suppress the confident upcoming
  claim until fresh evidence, using existing connection feedback. Schedules still
  run on the host; reconnect is not necessary for execution.
- No cancel/edit/dismiss buttons in this initial notice; keep existing inspector
  management. Expanding must not alter execution or draft state.

## Data and ownership plan

1. Add a small, optional, bounded upcoming-schedule projection to root.snapshot.
   Derive it from durable schedules using the SAME Go occurrence calculation as
   the scheduler. Each row identifies schedule ID, unclaimed slot/next_fire, and
   available prompt evidence. Fired one-shots yield no row. No persisted next_fire
   column, migration, new endpoint, polling loop, or independent schedule store.
2. Select upcoming candidates independently of the old ascending-ID historical
   page. Apply limits after pending selection/order, not before it. Respect the
   snapshot byte budget and expose incomplete coverage; keep timestamp metadata
   available when large prompts need bounded preview/content handling. Reuse
   existing collection/content reads for explicit full-prompt expansion, with
   truncation/recovery visible rather than silently showing a partial prompt as
   the complete message. Preserve existing inspector history ordering.
3. Factor/reuse the daemon's next-slot calculation in internal/schedule so the
   read projection and execution cannot disagree about @every anchors, first fire,
   last_fire, or overdue occurrences. Do not change firing/catch-up semantics.
4. Carry the projection through generated protocol contracts. Optional support
   permits older daemons: hide the new notice when unsupported rather than
   guessing from incomplete schedule lists or reviving the old running panel.
5. SDK owns immediate schedule.fired reconciliation (schedule ID + slot), followed
   by the existing coalesced snapshot refresh for the next occurrence. Protect
   against stale snapshots/pages resurrecting claimed occurrences; preserve
   ordering/cursor guarantees rather than adding an app-local fired-ID cache.
   Creation/deletion/reconnect use the existing event/snapshot path.
6. App renders the SDK projection. Add a focused ScheduledWakeNotice composition
   and a composer slot. Explicitly exclude kind=schedule from the old admitted
   input fallback. Do not remove or reinterpret other internal inputs such as goal
   continuation; their separate UI is outside this change.

## Intended files

- `internal/schedule/schedule.go`, `internal/daemon/scheduler.go`: shared next-slot
  calculation, with corresponding scheduler/parse tests.
- `internal/session/event.go`, `internal/session/snapshot_view.go`: upcoming
  snapshot projection and bounds; snapshot tests. Reuse collection/content
  plumbing where needed; do not change schedule persistence.
- Generated `packages/protocol/schema/` and `packages/protocol/generated/` output
  through the existing generator; contract/fixture tests.
- `packages/sdk/src/state.ts`, `packages/sdk/test/state.test.ts`: schedule lifecycle
  freshness/reconciliation. Read current uncommitted work before editing.
- New `packages/app/src/scheduled-wake-notice.tsx` and focused tests.
- `packages/app/src/conversation.tsx`, `packages/app/src/composer.tsx` and relevant
  composition tests; a tiny pure presentation helper only if tests benefit.
- `docs/frontend.md`, `docs/features.md`, SDK docs if the public projection changes.

## Ordered implementation / validation

- [x] Trace screenshots, execution vs schedule ownership, width and existing UX.
- [x] Record proposal and assumptions before implementation.
- [x] Confirm plan before writing implementation code.
- [x] Add regressions for pending -> fired-but-queued -> running -> consumed.
- [x] Implement shared slot calculation + bounded upcoming projection, preserving
  historical schedules and working even when the pending row follows many fired
  entries beyond the original snapshot page.
- [x] Regenerate contracts; implement SDK fire/reconnect reconciliation.
- [x] Build the compact disclosure and remove schedule rows from the old fallback.
- [x] Test recurring, multiple, overdue/unclaimed, cancelled, invalid/unsupported,
  no-schedule, truncated prompt, and partial/omitted collection cases.
- [x] Test live fire removes the exact occurrence before any turn starts, delayed
  refresh cannot resurrect it, old schedule events cannot remove newer slots,
  and remount/reconnect rehydrates from host truth without timers firing work.
- [x] Test timezone/day-boundary formatting, long unbroken prompt strings, plain
  text safety, disclosure keyboard/focus, and no root notice in child panes.
- [x] Test no regressions to ordinary queued chat inputs or non-schedule fallback.
- [x] Verify isolated shared-web component layout at wide, narrow and short
  viewports; component-test focus and actual composer/agent/queue slot ordering.
- [ ] Optional follow-up: installed Desktop end-to-end visual acceptance. The
  user's current app/daemon were deliberately not restarted or replaced.
- [x] Run focused Go tests with race detection, SDK/app suites and type checks,
  protocol drift checks, then project task check; documented unrelated blockers below.
- [x] Update canonical docs and record any implementation deviations here.

## Non-goals and working assumptions

No scheduler execution changes, cancellation of already-admitted work, generic
inbox redesign, database cleanup, auto-deletion of fired schedules, restart,
release/install actions, or changes to the user's current session. No broad
'stuck running' fix without a separate reproducible stale-state failure.

Assumptions for sign-off: root-chat placement above the composer stack; viewer
local time with timezone; recurring schedules show their next occurrence; multiple
schedules are grouped. These do not require blocking research on a question.

Only this planning document was written in the research pass. Implementation was
subsequently authorized by the user. Existing dirty
files (frontend docs, SDK state/tests/docs, span code and trace tests) are unrelated
work and must not be overwritten, staged, or reverted.

## Implementation decisions and validation

- Wire occurrence identity is canonical UTC with fixed nine fractional digits,
  exactly matching schedule.fired.slot. No client timestamp parser is needed.
- On fire the SDK removes the matching occurrence and invalidates the exact count
  until snapshot refresh; recurring and omitted work prevent safely decrementing it.
- A host-confirmed positive count with every row omitted renders a count-only
  recovery disclosure. Empty unknown-count state after a fire stays hidden.
- Long prompts show explicitly incomplete previews and route to the existing
  Goals/Schedules inspector's bounded collection/content reader for the full body.
- Independent review found historical interval walking in NextAfter was unsuitable
  for snapshot reads. Whole-interval arithmetic jumps now preserve grid semantics
  while bounding work even for ancient anchors and nanosecond intervals.
- Backend focused race tests passed after all follow-up fixes; protocol generation,
  14 interop tests and drift checks passed. SDK build + all 451 tests passed.
- Full Go vet, whipvet and go test ./... passed before the final small recurrence
  optimization; the affected schedule/session/daemon race suites passed afterward.
- Final full web suite passed 1045 tests, followed by a successful app type check.
  Final UI refinement also passed 59 focused tests (inspector22 and input11
  unchanged/passed). UI package checks/tests and renderer pack,
  dev-proxy, local-update and Docker-onboarding script tests passed.
- Isolated Chromium fixture at 1440x900,390x600,768x450: no horizontal overflow,
  prompt heights180/120/90 (20dvh cap), no page errors. Source component rendered
  against existing Vite, not a real user session. Screenshots in /tmp/whip-wake-*.png.
  Composer placement/focus/stack ordering are also component-tested. Native
  installed Desktop end-to-end acceptance was not run.
- Full task check is NOT green: repository-wide formatting scans an unrelated
  .claude/worktrees/session-trace checkout and flags its session.go/otlp_export.go.
  Changed Go files are formatted. Remaining checks were run separately.
- Existing cold StyleX dev CSS test stalled >2 minutes; a bounded retry reported
  timeout at30s. Both owned processes were stopped. No attempts to alter that
  unrelated test or restart the user's existing dev server.
- Independent read-only reviewer inspected both final fixes and found no remaining
  blockers. git diff --check passed; nothing is staged.
- No commit, staging, installation, release, current-session mutation or app/daemon
  restart was performed. Unrelated working-tree changes were preserved.
