# Missing-history recovery implementation

September 18, 2026. All six phases of [the plan](HISTORY-GAP-PLAN.md) are implemented.

The SDK previously joined a retained older window to a bounded recent snapshot
without detecting the intervening missing records. Those saved records contained
the identities needed to place the observed operations. Recovery now reads those
records through the existing history API and reconciles the existing executions.
No storage migration, protocol endpoint, provider request, or additional
subscription was introduced.

## Implementation

- `packages/sdk/src/state.ts` derives inclusive gaps from raw retained records,
  preserves older-prefix exhaustion, and distinguishes an evicted newer suffix.
  Recovery resumes after stream consumption starts. Requests are shared per
  agent, revision/epoch scoped, abortable, validated for ordered forward progress,
  and capped at four sequential 128-record / 256-KiB pages per automatic pass.
  Failures stay local; explicit retry/continuation reads one page. Paused ranges
  do not restart on an ordinary refresh. New ranges and reconnects get bounded
  recovery. All-omitted snapshots use a bounded recent read.
- Chat, REPL and native mobile expose the same history boundary. Groups and
  response-copy completeness stop at gaps; copying uses only available original
  prose. Historical recovery does not replay arrival motion. The existing guard
  against placing old unmatched activity under later replies remains in place.
- `ReadingList` preserves a surviving visible row when recovery replaces a gap.
  This fixes the additional 791px displacement found by the browser regression.
  Its existing virtualizer performs the correction; the bounded restoration
  stops on user scrolling. A temporary anchor pins only its row, not the entire
  interval to a focused control. Recovery preserves selection and moves keyboard
  focus only when the focused control disappears.
- Latest loads a newer window when needed; scrolling cancels its pending jump.
  Empty but pageable root history retains an accessible recovery control.
  Explicit navigation retains the requested page, including correct availability
  of an evicted zero-based root prefix. A child window evicted during a request
  retains its known end without resurrecting old message bodies.
- Frontend ownership/budget guidance and the SDK API documentation now describe
  `HistoryView.gaps`, `latestMissing`, `loadHistoryGap`, and `loadLatest`.

## Verification

The initial SDK regression failed before the implementation. It reproduces the
1588 → 1601 snapshot split, includes the missing authored input and five executions,
and now restores the full interval while keeping all five live execution IDs.
Additional tests cover opened children, shared readers, active streaming during
repair, stale success/error responses, revision changes, close/reopen, reconnect,
failed/empty/nonadvancing pages, manual navigation, offloaded bodies, independent
gaps, exhaustion, and retention limits.

The browser fixture uses the normal daemon worker completion path to commit real
synthetic turns, then reads the production bounded snapshot. It does not substitute
an app-owned transcript or write to the user's daemon.

| Final race-enabled fixture | Chromium | Firefox | Electron |
| --- | ---: | ---: | ---: |
| Count-limited snapshot records | 64 | 64 | 64 |
| Byte-limited snapshot records | 37 | 37 | 37 |
| Restored operation identities per case | 5/5 | 5/5 | 5/5 |
| Reading-anchor drift after insertion | 0px | 0px | 0px |
| Retained records after over-capacity turn | 512 | 512 | 512 |
| Automatic pages before continuation | 4 | 4 | 4 |
| Pages per explicit continuation | 1 | 1 | 1 |
| Peak mounted rows during continuation | 35 | 35 | 33 |

All three surfaces also pass a failed history read followed by keyboard retry,
shared chat/REPL error state, unchanged selection, bounded retained bytes, and a
later reply without old activity appended beneath it. The final runs compile the
isolated Go fixture with `-race`; shutdown checks reject any race report.
Screenshots, videos and assertions are in [the evidence directory](evidence/history-recovery/).

The broader existing transcript fixture passed Chromium, Firefox and Electron:
streaming Markdown, Unicode/highlighting, manual disclosures, selection through
settlement, scroll interruption, composer typing, large trees, narrow panes,
light/dark themes, type sizes, reduced motion, accessibility assertions, and staged
Electron main/preload behavior. This is browser automation and accessibility-tree
coverage, not a claim of manual screen-reader or native-device testing.

- SDK: **384 tests passed**.
- App: **728 tests / 70 suites passed**, including gap, copy, REPL, focus and
  Latest regressions. The captured logs retain the focused run boundaries too.
- Mobile: type checking and the existing **160 tests / 25 suites** passed.
- Go: affected daemon history/presentation and session storage/history/fork/rewind
  tests passed with the race detector; the real committed-turn browser fixture
  also passed with the race detector.
- App, mobile and desktop TypeScript checks passed. Production web packaging and
  desktop staging succeeded. The final renderer digest is
  `a84954a88a8936966b360d31c479052543a74625ab75e5562965d6fd95a67385`.

## Performance comparison

The existing 10,000-message / 100-child performance fixture passed. Comparison
against the previous composer-scroll fixture is descriptive, not a controlled
claim of a speed improvement. These measurements preceded the final zero-prefix
and temporary-anchor refinements; final race-enabled browser runs verify their
behavior and rendered-row bounds.

| Metric | Prior fixture | Recovery implementation |
| --- | ---: | ---: |
| Maximum normal rendered rows after paging | 22 | 22 |
| Final retained root records | 512 | 512 |
| Maximum retained SDK payload | 407,539 B | 407,630 B |
| Input handler → animation-frame median / p95 | 32.7 / 39.0ms | 29.2 / 33.7ms |
| Trusted keyboard → next paint median / p95 (rounded) | 48 / 64ms | 48 / 64ms |
| Stream received → DOM median / p95 | 57.5 / 82.9ms | 57.3 / 72.9ms |
| History page requests in the fixture | 24 | 24 |
| Subscription requests in the fixture | 3 | 3 |

All four prepend anchors and all composer typing scenarios showed zero measured
scroll drift. The maximum record total during child inspection is 612 across the
root and opened child, within the per-agent bound; final root retention is 512.
The original 8-MiB view and 1-MiB execution-evidence limits remain unchanged.

## Delivery scope

The desktop artifact is staged in `apps/desktop/.stage/app`. It was not installed,
and the user's daemon and saved conversations were not modified. Unrelated local
changes and the earlier composer/activity fixes were preserved. No commit was
created. The separate unreproduced disappearing-final-reply report remains outside
this plan's scope.
