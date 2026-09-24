# Shared chat and REPL history pagination

Status: implemented and validated, 2026-09-07. User approved execution of this plan.
Research date: 2026-09-07 (America/Denver).

## Outcome

Scrolling upward through a REPL should retrieve older recorded cells with the
same loading, reading-position, and exhaustion behavior as chat. Both readers
should stop requesting older history when the retained history reaches its
beginning, including after metadata refreshes.

Chat and REPL already share the scrolling component and SDK pagination API. The
work is a correction to that shared path, with direct browser coverage for REPL
scrolling. The canonical architecture remains [docs/frontend.md](../../../docs/frontend.md).

## Research findings

| Source | Current behavior and implication |
| --- | --- |
| [Timeline](../../../packages/app/src/timeline.tsx), [ReplView](../../../packages/app/src/repl-view.tsx) | Both render `ReadingList` and ultimately call `view.loadOlder(agentId)`. REPL does not have a separate paginator to replace. |
| [ReadingList](../../../packages/app/src/reading-list.tsx) | A scroll within 256 px of the top requests an older page. Both modes share TanStack Virtual, stable row keys, end anchoring, selection pinning, Latest, and bookmark restoration. An older-history button provides an explicit alternative. |
| [SessionView](../../../packages/sdk/src/state.ts) | The SDK owns revision-bound history, merges pages, deduplicates in-flight reads per agent, and enforces retention limits. Requests use at most 128 messages and 256 KiB. Defaults retain 512 messages per opened agent within an 8 MiB session payload budget. |
| [Execution projection](../../../packages/sdk/src/executions.ts) | REPL cells are derived from recorded `rlm_exec` calls/results and bounded observed evidence. Pagination counts transcript messages, not cells. A page may add no cells, or complete a cell whose call/result crosses a page boundary. |
| [Child refresh](../../../packages/sdk/src/state.ts) | `fetchHistory()` merges cached messages with the recent page but copies that page's `has_more` into the merged view. A suffix can advertise older messages that are already retained. |
| [Root refresh](../../../packages/sdk/src/state.ts) | `synchronize()` ORs `root.omitted.messages` into `hasMore`, even when merged root history already starts at its beginning. Snapshot omission and merged-history exhaustion describe different things. |
| [Loading guards](../../../packages/app/src/reading-list.tsx), [chat integration](../../../packages/app/src/conversation.tsx) | The automatic path checks local loading but not `loadingHistory` or `historyReady`. REPL supplies connection/loading props; Timeline does not currently forward those props. The SDK still deduplicates actual requests, but UI eligibility is inconsistent. |
| [REPL unit tests](../../../packages/app/test/repl-view.test.tsx) | These mock out ReadingList, so they cannot catch scroll-pagination failures. |
| [Reader tests](../../../packages/app/test/timeline-reading.test.tsx) | Cover bookmarks, revision fallback, and restoration; they do not directly establish the REPL automatic-pagination contract. |
| [REPL browser suite](../../../apps/web/scripts/repl-viewer.mjs) | Exercises older-page loading by programmatically clicking the button. It deliberately avoids scrolling to the button because scrolling can trigger a separate read. Add a dedicated scroll-driven case. |

TanStack's [official chat guide](https://tanstack.com/virtual/latest/docs/chat)
recommends stable item keys and end anchoring for prepends, with network loading
state managed outside the virtualizer. This matches the existing architecture.
The installed packages are react-virtual 3.14.10 and virtual-core 3.17.8; inspection
of the installed core confirms that it retains a keyed visible item across
prepends. No dependency upgrade or new scroll library is needed.

### Reproduction evidence

The reported child session, `b4732d98979dcac406676723ba344ef8:0ed1e763eccadb6a`,
has 137 stored messages reconstructing 71 cells. In the preceding investigation,
a read-only SDK client against the actual daemon produced:

| Action | Retained messages | First sequence | `hasMore` |
| --- | ---: | ---: | --- |
| Open child history | 128 | 10 | true |
| Load older history | 137 | 1 | false |
| Refresh metadata | 137 | 1 | true — incorrect |
| Load older again | 137 | 1 | false; empty response |

During this planning pass, an isolated root SDK probe reproduced the same
128 → 137 → 137 message counts and true → false → true flag transition with an
omitted recent root snapshot. The root problem therefore also affects main chat.

Two settled browser runs in the preceding investigation preserved the same
visible cell and its pixel offset after prepending the final child page. No
stored-message loss or failed history response was reproduced. A scroll-anchor
rewrite is not supported by that evidence.

## Implementation plan

### 1. Correct SDK pagination metadata for the retained history

Start in `packages/sdk/src/state.ts`, with regressions in its existing state test
file. Apply the same invariant to the root snapshot path and `fetchHistory()`:

- `nextSeq` identifies the oldest retained message used for backward paging.
- `hasMore` answers whether older transcript messages exist before that retained
  boundary. It does not describe omitted bodies or just the latest fetched page.
- On a same-revision refresh, preserve a previously established beginning when
  that boundary remains retained. A recent suffix must not undo that knowledge.
- When an older page extends the retained beginning, use its boundary and
  exhaustion result. A legitimate empty terminal page exhausts older history.
- When trimming removes the retained beginning, older history becomes available
  again. Preserve the existing policy that explicit backward paging retains the
  requested older page while refresh favors recent messages.
- A new history revision invalidates the old boundary. Empty initial histories
  and byte-budget eviction must retain a valid recovery path.
- Keep `truncated`/snapshot omission separate from whether older messages can be
  fetched. An abbreviated body alone must not create a pagination loop.

Use existing history fields; no new cache, cursor registry, or protocol field is
required. Keep the current epoch/revision checks and in-flight request sharing.
If a small private calculation avoids duplicating the boundary rule between the
two paths, keep it local to this file.

### 2. Make the shared reader's loading eligibility consistent

Keep automatic near-top loading in `ReadingList` for both modes, with the existing
256 px threshold. Use the existing readiness, connection, and loading inputs
consistently for the button and automatic path. Neither path should initiate an
older read while initial history or an SDK history-page refresh is loading,
during restoration, or during an already pending page request.

Forward `canLoadOlder` and `loadingHistory` through Timeline from SessionContent,
matching ReplView. Continue relying on SDK request sharing when chat and REPL
display the same agent in separate panes. Preserve the current error reporting
and explicit retry behavior.

Retain the existing virtualizer, row IDs, measurement, and reading bookmarks.
Appending live output should follow only while the reader is at Latest; older
pages should preserve the visible retained cell. Ordinary refreshes and bookmark
restoration should not themselves trigger an older-page scan.

Keep the older-history button as an accessible fallback when a REPL has too few
cells to scroll. Each user request retrieves a bounded transcript page. A page
with no cells still advances the SDK cursor; it is not proof of exhaustion. This
iteration does not automatically drain pages to find a target number of cells.

### 3. Add focused regressions at the existing boundaries

| Layer | Required cases |
| --- | --- |
| SDK state | Root and child: recent page → load to beginning → refresh repeatedly; retain all messages and keep `hasMore=false`. Further `loadOlder()` calls produce no RPC. |
| SDK state | Partial history stays pageable; older pages advance the cursor; a terminal empty page stops paging. |
| SDK state | Refresh/count eviction makes dropped older history reachable again; byte eviction can recover; revision changes discard old exhaustion knowledge. Extend existing retention/revision cases instead of creating another harness. |
| Shared reader | A near-top scroll requests a page; repeated scrolls while it is pending do not duplicate it. Exhausted, disconnected, not-ready, SDK-loading, and restoring states do not request older history. Completion/error clears the local loading guard. |
| Browser | Exercise actual upward scrolling, without clicking the fallback, for root and child REPL. Assert scoped `history.page` requests, decreasing `before_seq`, and arrival of known older cells. Include a main-chat parity case using the same fixture. |
| Browser | Record a visible cell ID and offset after layout settles; prepend a page containing variable-height output and assert the same retained cell stays within the existing 4 px tolerance. Check the final page as the older button disappears. |
| Browser | Exhaust history, trigger a fixture metadata refresh, and verify no older control or redundant backward read returns. Distinguish expected recent refresh reads from redundant backward requests. |
| REPL edge cases | A sparse/no-cell page leaves a usable fallback and advances the transcript cursor. Calls/results split across pages reconcile without duplicate cells. Continue existing coverage of live output, separate mode bookmarks, shared child leases, and bounded DOM rendering. |

Use a deliberately long history fixture to test automatic scrolling and a short
fully exhaustible fixture for the refresh regression. Avoid tests that pass only
because they clicked the button, started inside the wrong page range, or sampled
an unsettled virtual layout. The user's running daemon remains read-only; test
mutations belong to the existing isolated fixture.

### 4. Validate and document the resulting contract

Extend the existing SDK/app tests and `apps/web/scripts/repl-viewer.mjs`. Update
the short ReadingList description in `docs/frontend.md` to mention automatic
near-top loading and its fallback/readiness contract once implemented.

Run the affected checks with Node 24:

```sh
npm test
npm run test:web -- packages/app/test/timeline-reading.test.tsx packages/app/test/repl-view.test.tsx
npm run check:web
npm run pack:web
node apps/web/scripts/repl-viewer.mjs
node apps/web/scripts/snapshot-refresh.mjs
```

The REPL suite defaults to Chromium and Firefox. Record browser versions and
results; its browser checks do not establish physical-mobile or Safari coverage.
### Implementation evidence

- All 217 SDK tests pass, including new root/child refresh, exhaustion, eviction,
  empty-page recovery, and shared-request regressions. The refresh regressions
  failed before the SDK correction.
- All 175 web tests across 21 files pass; the focused reader/REPL subset contains
  17 tests. Type checking, production compilation, and asset packaging pass.
- The isolated Go REPL probe test passes with the race detector.
- All 11 REPL browser workflows pass in Chromium 153.0.8010.12 and Firefox 155.0.
  Both keep one root subscription. New cases cover child and root scroll-driven
  pagination, main-chat parity, sparse pages, and exhaustion across two refreshes.
- The snapshot-refresh browser regression passes in both browsers with the final
  packaged application.
- A read-only check using the updated SDK against the reported session retains
  all 137 messages / 71 cells and keeps `hasMore=false` after refresh.

The final-page browser case exposed an additional 32 px shift when removing the
older control during backward scrolling. The installed virtualizer deliberately
skips resize compensation in that direction. ReadingList now keeps the control's
natural measured space, hiding and disabling it when exhausted. This also adapts
to the design system's touch sizing without another fixed-height rule. The new
browser assertion now preserves the same cell within the existing 4 px tolerance.
No custom prepend compensation or additional cache was introduced.

## Scope and completion

The expected production changes are `state.ts`, `reading-list.tsx`, and the two
chat prop-forwarding sites, plus existing tests and the canonical guide. ReplView
already uses the shared reader and should require no independent pagination code.

This work is complete when scrolling retrieves older root/child REPL cells,
the reading position remains stable for retained cells, shared readers avoid
duplicate requests, and refresh cannot resurrect exhausted pagination. Retention
limits and revision recovery must still work.

Event-log aggregation, the 10k event replay boundary, durable host-call archives,
cell numbering changes, bidirectional cache-window navigation, and scroll-library
replacement are separate work. The existing 512-message cache can still evict
content in long sessions; this plan corrects its pagination metadata without
promising that every message remains loaded at once.
