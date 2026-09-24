# Desktop/web chat history prefetch

Status: implemented and validated end to end. See implementation results below.
The research and proposed policy below are retained as the implementation rationale.
Date: 2026-09-18

## User-confirmed scope

The problems are pauses at the loaded history boundary and too little history
available after opening a session. Scope is desktop/web, not native mobile.
Scroll jumping was not identified as the primary complaint; stable reading
positions remain a requirement.

## Findings

- `internal/daemon/view.go:13-17`: `root.snapshot` requests 64 recent raw
  transcript records, 128 collection entries, and a 384 KiB snapshot budget.
- `internal/session/snapshot_view.go:36-66`: message bodies share half of that
  budget (192 KiB). Reading stops at the count or byte boundary. Consequently,
  64 is a ceiling, not a guarantee, and raw records are not visible chat turns.
- `packages/sdk/src/state.ts:375-398`: initial root history comes from this
  snapshot. It merges with retained history rather than automatically fetching
  an older page for an ordinary fresh attachment.
- `packages/sdk/src/state.ts:225-229,631-649`: older reads already use the
  revision-pinned `history.page` API, at 128 records / 256 KiB per request.
  Requests are shared per agent and cancelled across incompatible lifecycle
  transitions. Child opening already requests a recent history page.
- `packages/sdk/src/state.ts:143-145,669-671`: the existing retention limits
  are 512 messages per agent and 8 MiB per session view. Older reads favor
  retaining the requested older window; Latest can recover an evicted suffix.
- `packages/app/src/reading-list.tsx:198-236,423-448`: chat pagination requires
  upward user scroll intent and `scrollTop < 256`. Programmatic scrolls,
  restoration and tail-following do not initiate it. There is no post-page
  refill check. An upward gesture at a hard scroll boundary can also lack an
  actual scroll event, leaving the manual button as the fallback.
- `packages/app/test/timeline-reading.test.tsx:325-348`: the test explicitly
  expects no request at 300 px, one at 200 px, and another only after a new
  scroll event following completion. The behavior is intentionally conservative,
  not simply a missing request-deduplication guard.
- `packages/app/src/runtime.ts:568-610`: root views are shared across consumers,
  with four retained roots and a 30-second unused-view lifetime. Initial
  prefetch belongs to the shared view lifecycle, not each mounted Timeline.
- `packages/app/src/reading-list.tsx:85-118,303-324`: existing virtualizer and
  bounded restoration preserve visible row identity/offset on insertion.
- `docs/frontend.md:920-952,1498-1522`: the maintained contract requires bounded
  fetching, SDK-owned history, stable anchors and intent-controlled following.
  Implementation must update the paging contract, not silently violate it.

These findings explain likely contributors, but are not a measured reproduction
of a particular production session. Network and render latency still need browser
measurement. No unrelated chat/scroll rewrite is justified.

## Recommended policy

### 1. Warm one additional page on a fresh desktop/web root view

Keep rendering the snapshot immediately. After the view is live, warm one older
page through the existing SDK history reader if older history is available.
With full, small records this changes the initial available window from 64 to
up to 192 raw records (64 + 128). Byte limits can reduce that number.

- Make this an app-enabled, one-time SDK view policy; default SDK behavior for
  native mobile and other consumers remains unchanged.
- Do not await warming before publishing ready/live state or enabling chat.
- Coordinate with existing recent-history recovery and gap repair; do not
  independently launch competing history readers. Missing recent history takes
  precedence over extending the older edge.
- Consume the warm-up opportunity once per shared view lifecycle, including
  when the view first becomes live after starting disconnected. Do not repeat
  it on every snapshot refresh, remount, split view, or metadata update.
- If a user older-read has already provided the extra page, do not immediately
  add a redundant warm-up page. Existing shared-request and cursor logic should
  own this coordination.
- Bound speculative work to one page; do not traverse history to satisfy a
  visible-message or turn-count target. Do not warm unopened child agents.
- Cancel stale work on disposal/disconnect/revision invalidation using existing
  SDK mechanisms. Errors remain scoped and manually retryable; no retry loop.
- Do not enlarge global daemon snapshot defaults: that increases the cost of
  refreshes and affects clients outside this request's scope.

This is deliberately a page-budget policy rather than an exact count promise.
An exact 128-record initial target would require an additional size policy; start
with reuse of the existing bounded page and tune after measurement.

### 2. Start scroll prefetch approximately two viewports early

For chat, replace the fixed 256 px threshold with the proposed starting value:

`prefetchDistance = clamp(2 * viewportHeight, 800, 2400)` pixels.

This is a tuning hypothesis, not an empirically optimal constant. It scales with
split panes while providing substantially more lead time. Keep the current page
size and byte cap. Virtualizer overscan only changes DOM rendering and is not a
substitute for fetching earlier.

Preserve intentional upward wheel/touch/keyboard/scrollbar gating. An upward
input while already at the top should be able to initiate a request even if the
browser emits no scroll event. Downward scrolling, Latest, tail-following,
ordinary row clicks, selection, layout growth and bookmark restoration must not
start older-history fetching.

### 3. Refill the buffer after a page, without unbounded paging

A user-triggered fetch starts one bounded prefetch episode. After each successful
page, wait for layout and prepend-anchor restoration to settle; then re-evaluate
actual viewport geometry. If the reader remains inside the prefetch zone, allow
another page without requiring another wheel event.

- One in-flight history request per agent, reusing SDK sharing.
- At most three pages in an episode (including its first page): up to 384
  records / 768 KiB of history responses. This is a starting safety cap.
- Stop when outside the zone, exhausted, failed, disconnected, hidden/unmounted,
  invalidated, no longer progressing through history, or after the page cap.
- Stop a pending continuation when the reader moves down, chooses Latest, or
  otherwise changes reading intent. A stopped gesture at the top does not by
  itself prevent the already authorized bounded episode from completing.
- Programmatic prepend compensation may complete an existing episode, but must
  never authorize a new one. Use current geometry/state after restoration,
  not stale values captured before the request.
- Coalesce additional upward events during an episode; they must not continually
  reset its page budget. A subsequent explicit upward gesture or manual button
  can start another bounded attempt after the cap is reached.
- Permit pages that advance the raw cursor but add little/no visible height
  (tool-heavy/filtered history) to continue within the cap. Stop on cursor
  nonprogress; never refetch evicted history in a background fill loop.
- Keep the explicit Load earlier control, loading indicator and error/retry path.

Do not add a permanent IntersectionObserver-driven auto-fill loop, velocity
estimator, dependency, second history cache, or unbounded mount-time fill.

### 4. Preserve current reading semantics and bounds

Reuse stable row IDs, visible-anchor restoration and TanStack Virtual. Do not
replace these with total-scroll-height arithmetic. Assert that appending older
history preserves the visible row/offset when detached and preserves bottom
following when still following. Existing Latest behavior must recover suffixes
removed by bounded retention. Keep 512 messages per agent / 8 MiB per view.

Limit the new scroll policy to chat via the existing chat behavior distinction;
REPL shares ReadingList but should not accidentally inherit different input or
pagination semantics. The shared root cache can naturally benefit from warm-up.

## Implementation sequence and likely files

1. Extend focused regression tests with the desired earlier threshold, stalled
   edge gesture, post-page refill and shared initial warm-up behavior.
2. Add the small, opt-in warm-up policy in `packages/sdk/src/state.ts`; enable
   it for shared desktop/web views in `packages/app/src/runtime.ts`. Reuse
   existing history reads, cancellation, revisions and error ownership.
3. Change chat prefetch gating and bounded continuation in
   `packages/app/src/reading-list.tsx`. Pass any necessary cursor/progress
   information through the existing Timeline/Conversation props rather than
   infer progress solely from rendered row count.
4. Update app and SDK tests, and extend
   `apps/web/scripts/reading-position.mjs` for real browser geometry and RPC
   timing. Reuse the isolated daemon fixture and existing history-gap coverage.
5. Update `docs/frontend.md` to document warm-up, viewport-based distance,
   per-episode caps and programmatic-scroll exclusions.

No daemon/protocol change should be needed. No user-facing preference is proposed
for the first iteration.

## Validation and acceptance

### Automated regression cases

- Snapshot renders before warm-up completes; one warm-up per shared root view.
- No duplicate warm-up across StrictMode, split panes, remounts or refreshes.
- Exhausted histories issue no speculative request; byte-limited, empty and
  tool-heavy pages terminate correctly. Unopened children are untouched.
- Offline-to-live start, reconnect, revision reset, disposal, and delayed replies
  cannot apply stale data or cause retries without a budget.
- Upward scroll triggers at the new threshold on short/tall viewports, not only
  at the top. Upward input at `scrollTop = 0` works without an offset change.
- Post-page refill does not need another gesture; stops on buffer sufficiency,
  three-page cap, error, exhaustion, cursor nonprogress and changed user intent.
- No older reads initiated by programmatic scrolling, resize, streaming,
  restoration, downward input or Latest.
- Prepend preserves visible identity/offset after variable-height rows and
  asynchronous content settle. Selection/focus and interrupted Latest survive.
- REPL and mobile SDK defaults remain unchanged; retention budgets and Latest
  recovery remain intact.

### Real-browser checks

Run the existing isolated-fixture browser scripts in Chromium and Firefox. Seed
long histories with short messages, long Markdown/code, images, grouped tools,
large/offloaded content and an active streaming tail. Include narrow split panes,
normal desktop height, rapid wheel/trackpad input, keyboard paging, top-edge
input and switching among retained sessions. Inject history response delays of
roughly 100, 300 and 800 ms into the test fixture.

Record request start offset, request latency, time spent blocked at the loaded
boundary, requests/bytes per open and prefetch episode, retained data bounds and
anchor drift. Require earlier-than-boundary requests, bounded continuation with
no extra gesture, and no visible anchor jump (target <=2 px after layout settles
for a surviving anchor). Compare initial snapshot interactivity before/after;
background warm-up must not gate it. Tune 2 viewports / 800–2400 px and the
three-page cap from these measurements; arbitrary scroll speed or network delay
cannot have a universal no-wait guarantee.

## Research validation performed

Ran:

`npm run test:web -- packages/app/test/timeline-reading.test.tsx packages/app/test/conversation-history-errors.test.tsx`

Result: 2 files, 24 tests passed. These establish the existing regression
baseline, not validation of the proposed policy. No browser latency experiment,
SDK suite or Go suite was run during that research-only phase.

## Implementation results

Implemented the policy above, including `initialHistoryWarmup` opt-in on shared
AppRuntime views, raw `historyCursor` propagation, and bounded chat refill.
Post-page continuation waits at least 50 ms for the existing 30 Hz projection
and stable geometry; at most 32 animation frames are allowed before stopping
speculation. Review additionally caught nested-scroller ownership at the hard
top: wheel/touch/key input now respects nested surfaces, and zoom, pinch and
already-prevented input cannot authorize paging.

Final validation:

- `npm test -w @whip/sdk`: 429 passed.
- `npx tsc -p packages/app/tsconfig.json --noEmit`: passed.
- Focused reading/history-error tests: 58 passed.
- `npm run test:web`: 941 passed, 4 failed in
  `packages/app/test/multi-host-discovery.test.tsx`. All four failures reproduced
  on clean detached HEAD `ae1093bb4cbf2fb549815fe705c8246a69cd7b54` with baseline
  first-party packages; these are pre-existing search/cache expectation failures.
  No unrelated search code/tests were changed.
- `npm run build && npm run pack:web`: passed.
- `node apps/web/scripts/history-prefetch.mjs`: Chromium and Firefox at
  100/300/800 ms history latency, all six cases passed on final code.
- `node apps/web/scripts/reading-position.mjs`: both browsers passed. The seed
  now leaves older records available after warm-up, preserving manual-page checks.
- `WHIP_CHAT_HISTORY_ONLY=1 node apps/web/scripts/chat-activity.mjs`: both
  browsers passed the unchanged history-gap recovery checks. `history-gap.mjs`
  is a helper invoked by this entrypoint, not a standalone runnable test.
- `git diff --check`: passed.

The new browser fixture observed early fetching at scrollTop 1,235 px with a
1,386 px threshold, exactly one warm-up page, at most one in-flight request,
and three-page refill without another gesture. Anchor drift was 1 px for the
native 1 px wheel movement and 0 px during refill. Incoming-output/manual-prepend
and gap-recovery anchor drift were 0 px. The refill stress case uses real daemon
history pages constrained to one record by test transport, explicitly not the
production page size. Nested/zoom/prevented inputs and programmatic/layout changes
did not trigger paging. Gap recovery remained capped at four automatic pages and
512 retained records. Final renderer artifact:
`d09aa1f16a0795e1203d81a4f77590586b37d4dc5dd7c17b7deb20db8da92652`.

Browser artifacts are under `/tmp/whip-history-prefetch`,
`/tmp/whip-reading-position`, and `/tmp/whip-chat-activity-results`.
All test jobs and isolated daemon fixtures exited. No commits were created.

