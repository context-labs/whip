# Recover missing transcript ranges

Status: implemented and validated, September 18, 2026. See [implementation results](HISTORY-GAP-RESULTS.md).

This plan addresses the confirmed SDK history gap that left five older activity
groups beneath later responses. It does not cover the separate, unreproduced
report of a disappearing final reply. The attached image is outside this scope.
Current ownership and budgets remain defined by [the frontend guide](../../../docs/frontend.md).

## Outcome and evidence

A completed turn must remain chronologically readable when its transcript is
larger than the recent snapshot. Every retained section must either connect to
its neighbors or expose the missing range and a working recovery action.
Operations whose records are recovered must settle into their original position,
without duplicate rows, repeated animations, or a forced jump to the bottom.

The [research record](TRANSCRIPT-RESEARCH.md) establishes a reproducible case:

- Previously loaded history ended at record 1588.
- The next turn committed through 1658. The snapshot's 64-message limit and
  192 KiB message budget admitted only records 1601–1658.
- The SDK merged those records with the earlier window, leaving 1589–1600 absent.
- Five observed executions remained unmatched. Their counts and partial flags
  reproduce all five misplaced screenshot groups.
- Supplying the missing stored records to the existing reconciler reduces the
  unmatched count from five to zero. The saved identities and bodies are intact.

The existing local activity-tail guard is a containment measure. It must not be
treated as completion of this work.

## Decisions and constraints

- The SDK `SessionView` owns gap detection, requests, merging, and execution
  reconciliation. App components consume that state; they do not fetch traces,
  inspect the database, or maintain another transcript cache.
- Reuse `history.page`, including its revision, `after_seq`, `before_seq`, and
  `through_seq` bounds. No new database, migration, or endpoint is expected.
- Recover ordinary recent gaps automatically. Expose quiet loading and retry
  controls for gaps that remain after a bounded pass or a failed request.
- Preserve 512 retained messages per opened agent, the 8 MiB session budget,
  the 1 MiB execution-evidence budget, existing root/child leases, and one root
  subscription. Gap metadata counts against the session budget.
- Keep page requests at 128 records / 256 KiB. An automatic recovery pass issues
  at most four sequential pages across the view (at most 1 MiB of page payloads),
  stopping earlier when the target is filled or retention cannot make progress.
  This is a work limit, not an increase to retained memory.
- Use a synthetic reproduction in committed tests. Actual user messages and
  tool output do not become fixtures. The user's daemon remains read-only.
- Preserve unrelated working-tree changes and earlier composer fixes. Installing
  a desktop build, restarting the user's daemon, and committing are separate from
  implementation validation.

## Phase 1 — Pin the failure and the history contract

Start with existing SDK state/execution tests and the daemon's bounded-history
tests. Capture the failure before changing production behavior.

1. Build a synthetic long turn with the same old-window/new-window split and five
   host-bearing executions in the missing interval. Include prose and authored
   input in the interval so the test does not cover only operation rows.
2. Reproduce count-limited and byte-limited snapshots separately. Verify the
   canonical records exist and retain their turn/part/invocation identities.
3. Establish that raw transcript sequence numbers are ordered and contiguous
   within one agent and history revision, before filtering them into chat or
   execution rows. Test root and child transcript sources. Sequence comparison
   must never cross agent or revision boundaries.
4. Define the distinction between an unloaded older prefix, an interior gap,
   an evicted newer suffix, and an offloaded message body. A content handle is
   a present record, not a missing record.

Exit check: the SDK test demonstrates the silent interval and five unmatched
executions; supplying the missing records repairs all five with existing
reconciliation. Existing pagination exhaustion tests remain part of the gate.

## Phase 2 — Represent coverage truthfully in SDK state

Primary owner: `packages/sdk/src/state.ts`.

1. Add small, additive history-gap descriptors to `HistoryView`: inclusive
   missing sequence bounds and recovery status. Derive ranges from retained raw
   records after every merge and trim; do not build a separate record store.
   Interior descriptors are bounded by adjacent retained records, at most
   `maxMessages - 1` per agent. Keep request bookkeeping bounded to those ranges.
2. Apply the same coverage calculation to root snapshot merges, opened-child
   recent reads, explicit paging, and budget eviction. This also discovers holes
   already present in a running client's cache on its next refresh.
3. Keep `throughSeq` as the known read boundary, not proof that every earlier
   record is loaded. Keep `nextSeq`/`hasMore` describing older-prefix pagination;
   do not overload them to represent an interior gap.
4. Drop incompatible coverage and requests on revision changes, runtime changes,
   child closure, and view disposal. Recompute after eviction so a gap outside
   the retained window becomes ordinary pageable history rather than permanent
   recovery work.
5. Keep the current view usable while coverage is incomplete. A failed gap read
   is a history error, not proof that the daemon disconnected.

Exit check: every discontinuity has a bounded descriptor, contiguous histories
have none, and repeated refreshes preserve valid exhaustion knowledge.

## Phase 3 — Recover gaps through bounded, shared reads

Extend the existing history-request coordination, rather than creating a second
request scheduler or subscription.

1. After publishing a snapshot and resuming the root stream, repair recent gaps
   for the root and already opened agents. Do not delay event consumption while
   fetching pages or open unseen child histories to fill their activity cards.
2. Pin each read to the current revision and a fixed missing interval. For the
   reproduced case, read after 1588 through 1600. Advance by returned raw record
   sequences; a page containing no chat prose or REPL cells still makes progress.
3. Share in-flight reads per agent across chat, REPL, refresh, and explicit
   actions. A caller waiting for another read must re-evaluate its own target
   afterward; joining an unrelated older-page request is not gap recovery.
4. Merge each valid page into the latest compatible state, deduplicate by raw
   sequence, and run existing execution reconciliation. Preserve live cell IDs,
   group aliases, and manual disclosure choices. Do not weaken matching to
   tool-call ID alone.
5. Validate response revision, requested bounds, ordering, and forward progress.
   Reject late results after an epoch/revision change. Empty/nonadvancing pages
   cannot silently declare a known gap filled or trigger an infinite loop.
6. Stop on failure, disconnection, the four-page work limit, or retention that
   would repeatedly evict and refetch the same records. Leave the remaining gap
   explicit. Publications alone must not restart an exhausted recovery pass.
   Retry failed work on an explicit action or a new connection; new ranges can
   initiate their own bounded pass.
7. Provide an explicit SDK gap-load/retry action. Each action retrieves one
   bounded page and retains the requested range according to the existing
   explicit-navigation policy. Automatic recovery continues to favor recent
   history. Keep evicted edges reachable; if the newest retained record is behind
   the known end, Latest must reload a recent window before scrolling to it.
8. If a snapshot omits every message, reuse bounded recent paging to recover a
   window and establish its end. Do not infer a missing numeric range from an
   absent high-water mark.

Exit check: the reproduction recovers all records and all five execution links
automatically. Multi-page gaps, simultaneous turns, reconnects, errors, and small
budgets cannot duplicate records, exceed limits, or spin requests.

## Phase 4 — Integrate recovery with readers and response copying

Primary owners: the app's conversation projection, Timeline, ReplView, and shared
reading-list integration. SDK behavior also applies to native mobile.

1. Project an unresolved gap as a stable chronological boundary between its two
   retained sections. Stop activity grouping and response completeness across
   it. Gap identity is scoped to agent/revision and a retained boundary so partial
   fills do not repeatedly remount its control.
2. Use existing notices/buttons for “Loading missing messages…”, “Load missing
   messages”, and “Couldn't load messages · Retry”. Show these only for actual
   gaps. Support keyboard and screen-reader use without per-record announcements.
3. Keep chat and REPL on the same SDK recovery state. Give native mobile a minimal
   equivalent through its existing notice/action components; retain its current
   transcript presentation and avoid introducing desktop activity trees there.
4. Keep the activity-tail guard: unplaced old evidence must not appear under an
   unrelated response. Recovered historical operations render at their proven
   positions; truly ambiguous legacy evidence remains separate in the REPL.
5. Mark response copying as incomplete where retained prose crosses a gap.
   Copy only original available assistant prose, excluding gap labels, reasoning,
   and tool output. Do not imply that a partially loaded response is complete.
6. Preserve visible row identity and pixel offset when records are inserted above
   the reader. Honor selection and keyboard focus during replacement of a gap
   control; move focus predictably only when the focused control disappears.
   Follow the bottom only when the reader was already following it.
7. Recovered history is historical content: no arrival fades or tree entrance
   replay. Programmatic anchor compensation must not request another history
   page. If budget eviction removes the anchor, use the existing documented
   bookmark fallback rather than claiming exact preservation.

Exit check: recovery restores chronological content without duplicate operations,
misleading copy footers, replayed motion, or unwanted scroll/focus movement.

## Phase 5 — Verify the real commit-to-refresh path

Extend existing suites and fixtures rather than creating a parallel harness.

| Layer | Required acceptance coverage |
| --- | --- |
| SDK | Root and opened child; count/byte limits; multiple and multi-page gaps; initially omitted history; offloaded bodies; overlapping/repeated pages; gap already in cache; ordinary exhaustion and backward paging. |
| Reconciliation | Five-operation reproduction; reused tool IDs across turns; live IDs retained; parallel hosts settle in place; failure/cancellation; record/result split across pages; legacy ambiguity remains explicit. |
| Request lifecycle | Shared readers; refresh during repair; active streaming during repair; disconnect/reconnect; revision/rewind/fork changes; child close/reopen; stale success and error replies; nonadvancing/empty page; manual retry. |
| Bounds | 512 records per agent, 8 MiB session, 1 MiB execution evidence; four-page automatic work cap; no refetch/eviction loop; large gap exceeds retained capacity; no extra subscriptions or full-output reads. |
| Chat/REPL | Correct chronological placement, full available prose, one appropriate copy footer, stable disclosures, truthful gap controls, preserved selection/focus and reading anchors, interruptible Latest. |
| Native mobile | Shared SDK updates compile and pass existing tests; unresolved gaps have a usable action without changing native transcript styling. |

The browser fixture must commit a real synthetic turn larger than the snapshot
window into the isolated daemon. Merely injecting stream events without journal
records tests the fallback guard but cannot establish recovery correctness.
Include a later reply after the long turn so stale groups cannot hide at the end.

Run affected Go/race history/fixture checks, the SDK suite, focused app/reading
tests, type checks, and production web/desktop builds. Exercise Chromium, Firefox,
and staged Electron with screenshots and recorded assertions. Confirm the existing
long-history DOM and subscription bounds; compare request counts, retained bytes,
input responsiveness, and reading-anchor drift to the baseline. No broad benchmark
rewrite is necessary.

Exit check: the full commit → bounded snapshot → gap recovery → later reply flow
passes, including an offline/error retry case and an over-budget case with an
explicit continuation action.

## Phase 6 — Document and deliver

Update `docs/frontend.md` with history coverage ownership, request/work bounds,
gap/retry behavior, and overflow policy. Update SDK API documentation for additive
history state/actions, and record fixture results alongside this plan.

Review the final diff against unrelated local work. Report the root-cause fix,
validation, and any remaining limitation separately from the earlier UI guard.
Stage the validated desktop artifact; installation is outside this plan.

No product preference is required to begin: automatic bounded repair with explicit
continuation/retry is the default. Implementation should proceed in phase order;
ship only when the SDK correction and reader behavior pass together.
