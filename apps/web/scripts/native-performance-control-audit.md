# Isolated native keyboard control

`node apps/web/scripts/performance-input-control.mjs` runs the staged production
Electron renderer with one disposable native root. It compares the actual
composer with a plain textarea at matching geometry and typography, with 0,
32,768 and 255,543 ASCII characters. Each case receives 40 trusted keys. Native
window/content/display bounds, document visibility, focus, exact final text and
caret, event identities and bounded observer counters must all pass.

This is a causal diagnostic, **not** the unchanged `performance.mjs` acceptance
workload: it has no 10,000-message history, 32 tabs, 16 provider streams or file
transfer. Native EventTiming is a rounded browser rendering estimate, not
physical display latency. A missing event stays unknown; a finite observer drain
cannot prove it was simply below the reporting threshold. Natural memory
samples are sequential and include previous cases; they do not establish leaks,
isolated per-case allocations or a peak.

`WHIP_WEB_PERFORMANCE_INSPECT=1` pauses before cases, writing the exact owned PID
and application path to `inspection-ready.json` in the printed result directory.
Only that owned window may be observed/focused. Write `continue\n` to
`inspection-release` after recording the external evidence. The pause is bounded
to 45 seconds within the 90-second post-launch owner deadline; it does not select
or change an installed Whip window. Without this pause, the native focus guards
still apply.

## 2026-09-29 evidence

Product source: `0265ab26c` plus caret repair `a4400d3e16`; natural retention
instrumentation `ba95e1606`. Staged renderer digest:
`3e18ccd8ca5376ea08892974771b1fff9faf6ab8cc0987483fc84656206f04f2`.
Electron 44.2.0 / Chromium 152.0.7977.76. All other agents reported no local
tests/builds/browser work during the measurement.

The completed run is `/tmp/whip-performance-input-controls/run-m9JQ9i/controls.json`.
CUA bound the exact worktree's `node_modules/electron/dist/Electron.app`, observed
the disposable session/path, raised its window and clicked its composer. The
native window screenshot showed that owned surface. All six before/after native
focus and geometry guards passed; 240/240 keydown entries were reported with no
missing entries, counter overflow or dropped entries. This does not prove absence
of later OS occlusion or physical display timing.

| Initial characters | Composer p95 | Plain textarea p95 |
| ---: | ---: | ---: |
| 0 | 40 ms (36–44) | 40 ms (36–44) |
| 32,768 | 40 ms (36–44) | 40 ms (36–44) |
| 255,543 | 40 ms (36–44) | 32 ms (28–36) |

Parenthesized bounds account for native 8 ms duration rounding. Draft length
alone did not reproduce the full workload's previous 72 ms p95. That workload
and natural memory retention still require independent measurement.

Earlier incomplete trials remain invalid: the first exposed a harness-only
nonexistent unary `Client.close()` call (removed; the existing fixture/launcher
own cleanup), and subsequent trials stopped at native focus loss despite the
textarea retaining DOM focus. Exact owned PIDs were verified gone. The retained
failure reports `run-O2RQDi` and `run-zn2tfD` show that boundary; their partial
timings are not acceptance evidence. No focus assertion was weakened.

## Full workload, natural retention

The subsequent `/tmp/whip-performance-native-natural/run-VFyWn2/performance.json`
run completed all seven existing workload groups with the same renderer. CUA
observed the exact disposable window/path, raised the window, clicked the
composer and restored its end-of-draft caret. Native content was 1360×960, inside
the display work area; native and DOM focus passed before and after all 40 keys.
All 40 native entries were reported without drops. Input p95 remained **72 ms
(68–76 ms)**, exceeding the 50 ms target. This is separate from the successful
isolated controls above and confirms that focus alone does not remove the full
workload gap.

The run's explicit inspection pause lasted 42.43 seconds **after** the finite
provider delta streams began. All quantitative workloads were retained, but
later keys consequently observed held active requests after their finite delta
work had completed. This is not identical temporal overlap to the earlier run.
The harness now pauses before launching any of the 16 streams, so subsequent
measurements retain the original stream timing relationship. The pause itself
is never included in keyboard latency.

No forced collection was requested after the drafts/streams/transfer stages.
The earlier 1/8/32-tab baselines still contained a forced collection in that
run; it must **not** be described as wholly GC-free. That remaining tab helper
call is now removed: each tab baseline uses the same bounded passive sampler,
with optional forced collection only in the separately labeled final diagnostic.
Natural sampled aggregate Electron RSS
peaked at 1,597,504 KiB and ended at 1,596,864 KiB. The final sequential browser
sample reported 71,580,960 bytes used JS heap, 243,909,792 bytes embedder heap,
20,011,411 bytes backing storage, 19,163 DOM nodes, 2,323 listeners and nine
virtualized transcript rows. RSS sums may double-count shared pages and periodic
sampling can miss peaks. These are investigation evidence, not a new memory
acceptance threshold or a leak diagnosis. The earlier post-forced-GC final
values must not be compared as natural retention.

The full harness now reuses the tested keyboard classifier: absent entries
remain unknown/unbounded after finite drainage. The complete 40/40 historical
run retains exactly its 72 ms estimate and 68–76 ms quantization bounds.

## Corrected trace and bounded parser candidate

The diagnostic `/tmp/whip-performance-native-trace/run-b5I6bf` used harness
`704f4c760`, with inspection before provider streams and no default forced
collection anywhere in the performance call graph. Its bounded first-key CPU
trace contained 1,740 events (945,595 bytes, no data loss). All seven workload
groups and 40 keys completed, but CPU tracing excludes that run's latency and
memory values from acceptance. The trace showed about 2.97 ms sampled self time
in `skillTrigger` and about 1.78 ms in its caller. Autosize layout was below 1 ms;
this did not justify rewriting autosize or establish the complete 72 ms cause.

Candidate `4a18de8f1` only rejects a completion search early when no slash exists
before the caret. The original whitespace, fence, token and UTF-16 offset logic
remains unchanged. The parser/catalog/runtime suites passed 101 tests, and shared
app types passed. Independent source review found no semantic blocker. A pure
Node 24 comparison of exact `704f4c760` and candidate sources returned identical
results for 14,884 deterministic differential cases. Seven alternating rounds of
100 calls, after 20 warmup calls, gave these median per-call values:

| Input | Before | Candidate |
| --- | ---: | ---: |
| 255,543 ordinary characters, no slash | 2.188 ms | 0.072 ms |
| 64,000 repeated Unicode characters, no slash | 0.690 ms | 0.019 ms |
| Valid trigger after long prose | 12.959 µs | 12.935 µs |
| Short middle-token trigger | 0.265 µs | 0.332 µs |

The script and raw results are `/tmp/whip-skill-trigger-benchmark.mjs` and
`/tmp/whip-skill-trigger-benchmark.json`. This measures only the parser; it does
not prove a 50 ms full-workload result.

## Incomplete paired baseline

A comparable untraced baseline used `704f4c760` plus passive memory instrumentation
`e9aa2b69e` (local exact-equivalent head `48011e766`), renderer digest
`3e18ccd8ca5376ea08892974771b1fff9faf6ab8cc0987483fc84656206f04f2` and native
binary digest `0136f40073f6ffc0ad192a853bec533a4627e4961d9dbd04c073891c6cb81efe`.
Other agents reported no local heavy work during the run. It failed before
streams, typing and transfer, at the cached child anchor check on switch index 7.
The prior seven child restorations matched message 006 at −5 px; the failed
restoration showed message 084 at −29 px. The root anchor matched on every
switch, though its final restoration took 1,156 ms rather than the prior roughly
50 ms. There were no page errors and no new child history-page read after its
initial load. This is a jump toward the tail, not evidence of the separate prior
28 px intermittent reading issue.

The preserved report and screenshot are
`/tmp/whip-performance-native-paired-before/run-GN5GYV/performance-failure.json`
and `performance-failure.png`; the log is
`/tmp/whip-performance-native-paired-before.log`. Owning cleanup completed with
an empty error list, and recorded Electron PID 4521 and runtime PID 4517 were
verified gone. No successful performance result was emitted. The reading-anchor
failure remains open for diagnosis; no rerun or candidate improvement is claimed.

## Completed comparison and accepted typing result

On 2026-09-29 the user explicitly accepted the 72 ms typing result and asked to
wrap up that work. The 50 ms optimization target is therefore closed as
user-accepted; no additional latency runs or speculative typing changes are
planned. This does not waive the separate memory-retention question.

The optional anchor diagnostic completed first at
`/tmp/whip-performance-native-anchor-full/run-t2ur2v/performance.json`: all seven
workload groups, 40/40 key entries and cleanup passed. Its capped anchor ring
retained 256 samples and reported 179 discarded samples. No jump reproduced.
All timing and memory values from that run remain diagnostic and excluded from
acceptance. The earlier `run-GN5GYV` child-tail jump remains unresolved.

The subsequent untraced runs both completed the unchanged seven workload groups:
10,000 root messages and 100 child transcripts, four prepends and selection,
20 cached switches, 32 tabs/drafts, 16 provider streams, 40 accepted inputs/keys,
and the bounded upload/download workload. CPU tracing, anchor tracing and forced
collection were absent. CUA observed only the exact owned worktree Electron app
and disposable path, raised its window, clicked its current composer and placed
the caret at the end before releasing the pre-stream inspection pause. Each run
reported 40/40 native key entries, zero missing/dropped entries, and passed native
focus/visibility plus 1360×960 native/content/document bounds before and after
keys. All five recorded Electron/runtime PIDs in each run were verified gone.

| Evidence | Baseline | Parser candidate |
| --- | --- | --- |
| Harness/source head | `af95dfc15f9ba7e2f7322e3843debeb1ed127fa1` | `473d065d01b38a56ef562f1731920e6b4ca3648a` |
| Staged source | `48011e766d5b069c1b47d53be396ee6fbdcabaec` | `473d065d01b38a56ef562f1731920e6b4ca3648a` |
| Renderer SHA-256 | `3e18ccd8ca5376ea08892974771b1fff9faf6ab8cc0987483fc84656206f04f2` | `0af75b87cce53a45cc1fb60227912395b09dfe601c899c4abfeb0e0d79cdc39d` |
| Native binary SHA-256 | `0136f40073f6ffc0ad192a853bec533a4627e4961d9dbd04c073891c6cb81efe` | `9503860103811d08f0e41e9fadfdd2d862e6a03686ea1897f6be3df309e01c00` |
| Typing p95, native rounding bounds | 72 ms (68–76) | 72 ms (68–76) |
| Keypress processing median / p95 | 9.9 / 10.6 ms | 8.1 / 9.0 ms |
| Keyup processing median / p95 | 2.0 / 3.6 ms | 0.2 / 0.4 ms |
| Natural application RSS after typing | 1,218,688 KiB | 1,222,960 KiB |
| Sampled transfer peak RSS | 1,629,712 KiB | 1,693,136 KiB |
| Natural end-of-work RSS | 1,614,288 KiB | 1,653,824 KiB |

The heads differ only in the two parser lines and their 15 test lines; fixture
instrumentation is identical. The baseline stage predates only fixture/docs
changes at its harness head. Both use Node 24.14.1, Electron 44.2.0 and Chromium
152.0.7977.76. The full-workload p95 did not improve. This single ordered pair is
not statistical evidence of a memory regression or physical display latency.

One additional comparison limit is explicit in the recorded geometry: the
baseline remained on the primary display at x=600/y=143. The candidate's native
window was inside the left display at the before-key check (x=−1400/y=543), then
inside the primary display at the after-key check (x=745/y=326). Both checks
passed the same content size, visibility and focus constraints, but the physical
display was not pinned. No window movement was issued during the key interval;
the evidence does not establish why its position changed. Do not use this pair
to claim a causal improvement in physical presentation timing.

Raw results:

- `/tmp/whip-performance-native-baseline-comparable/run-niNF8k/performance.json`,
  SHA-256 `359a7e344f99c5b49bb39d523030b2098a36fc135c83e08313af7c4cb2eddd2c`.
- `/tmp/whip-performance-native-candidate-comparable/run-iGeccx/performance.json`,
  SHA-256 `65319fe42cb1864851300d7711e7334b3fd0d7f32121f6fc3442bf60c8c25785`.

### Natural retention attribution

Both runs localize their largest late step to the full document navigation into
the scheduled-prompt inspector, after all upload previews had been removed:

| Sequential boundary | Baseline application / renderer RSS | Candidate application / renderer RSS |
| --- | ---: | ---: |
| Before uploads | 1,284.8 / 717.6 MiB | 1,267.5 / 706.3 MiB |
| Uploads and transfer typing complete | 1,393.2 / 787.0 MiB | 1,388.3 / 771.2 MiB |
| All previews removed, before navigation | 1,385.7 / 780.0 MiB | 1,382.4 / 765.0 MiB |
| New document and scheduled prompt ready | 1,575.7 / 975.8 MiB | 1,570.9 / 963.9 MiB |

Across that navigation, connected DOM nodes increased from 7,544 to 11,641 in
the baseline and 7,679 to 11,774 in the candidate. Total reported DOM nodes rose
from 7,657 to 19,406 and 7,788 to 20,144 respectively; document count rose from
one to two. The largest Markdown block grew from about 6,035 nodes to 10,000.
No mounted upload attachments or images remained before navigation. This rules
out retained upload-preview elements as the sole explanation of that late step;
it does not prove a leaked document, identify retaining objects, or attribute
all RSS to DOM. Sampling is sequential/non-atomic, can miss brief peaks, and
summed process RSS can count shared pages more than once. No 350 MiB pass bar is
invented and no forced collection authorizes a memory pass.

The next step is a bounded source/lifetime audit of that navigation boundary:
old preload/IPC subscriptions, SDK/React owners and expected Markdown subtree
growth. If ownership remains ambiguous, a focused retention-only check can take
passive samples after effects settle and the existing view-retention interval
expires, with exact document/context ownership and connected-node counts. It
needs no timed typing, repeated latency benchmark, forced collection, owner
deletion or speculative product repair. Ordinary browser/document retention and
an app-owned leak remain distinct possibilities; the current immediate samples
do not decide between them.


## Bounded retention source audit (2026-09-29)

This audit uses measured candidate `473d065d01b38a56ef562f1731920e6b4ca3648a`
plus the preceding documentation-only record `0fb3c63e3`. It adds no product
change and does not reopen the user-accepted 72 ms typing result. The immediate
post-navigation samples in both full runs remain natural observations, not a
leak diagnosis or a memory acceptance threshold.

### Document and native ownership

The reviewed normal, nonpersisted navigation path has explicit retirement:

| Owner | Source and concrete disposal behavior |
| --- | --- |
| Shared application bootstrap | [`bootstrap.tsx`](../src/bootstrap.tsx) `pagehide` flushes drafts; `persisted === false` runs idempotent disposal. It removes runtime/composition/desktop subscriptions and DOM lifecycle listeners, disposes the application, unmounts React, then disposes the platform. `persisted === true` deliberately preserves the application for back-forward caching. The measured runs did **not** record this flag. |
| App runtime and query cache | [`runtime.ts`](../../../packages/app/src/runtime.ts) `dispose` closes host connections, drops all session/execution/trace leases including suspended ones, clears pending command closures and submitted inputs, clears Query, browser state, tabs, compositions, reading positions and subscribers. Draft flushing clears the 150 ms save timer. Host detachment removes title subscriptions. Query defaults are immediate inactive disposal except the enumerated five-minute host-metadata caches; `queries.clear()` removes both kinds. The pinned Query implementation destroys/cancels each removed query. |
| SDK observation state | [`state.ts`](../../../packages/sdk/src/state.ts), [`execution-state.ts`](../../../packages/sdk/src/execution-state.ts) and [`trace-state.ts`](../../../packages/sdk/src/trace-state.ts) abort requests, advance generations, clear polling timers, join pending observation/navigation promises and empty their retained rows/listeners on disposal. Ordinary unused app views have a 30-second warm lease; complete app disposal drops them immediately instead of waiting for that lease. |
| Renderer native transport | [`desktop.ts`](../src/platform/desktop.ts) aborts each prepared connection, removes its exact bridge subscription and sends `releaseConnection`; each framed call removes its transport subscription and sends `closeTransport`. [`framed.ts`](../../../packages/sdk/src/framed.ts) closes its connection in `finally`, and closes a connector that resolves after cancellation. |
| Preload listeners | [`preload.ts`](../../desktop/src/preload.ts) and [`browser-preload.ts`](../../desktop/src/browser-preload.ts) register explicit listener wrappers and return exact `removeListener` callbacks. App browser association/workspace disposal invokes those callbacks and removes DOM/viewport listeners and measured slot elements. They do not register a second cross-document app registry. |
| Main process | [`main.ts`](../../desktop/src/main.ts) owns the window and bounded native resource maps. Releasing one connection deletes its record, aborts its controller and releases its transports/SSH. [`transport.ts`](../../desktop/src/transport.ts) closes queued/active work, clears timers and pending byte-count maps. Outstanding frame accounting retains sequence/byte numbers, not renderer DOM or cached conversation values. [`unix-frames.ts`](../../desktop/src/unix-frames.ts) destroys its socket and clears the decode buffer on close. Main's event sink closes over the native BrowserWindow, not the renderer's AppRuntime. |
| Attachment bytes | [`compositions.ts`](../../../packages/app/src/compositions.ts) aborts removed upload jobs, clears file/session references and revokes removed object URLs. [`input-attachment.tsx`](../../../packages/app/src/input-attachment.tsx) uses inactive `gcTime: 0`, aborts downloads and revokes image URLs on cleanup. [`content-read.tsx`](../../../packages/app/src/details/content-read.tsx) aborts its scoped read and revokes its image URL on unmount. The native save owner deletes completed save entries and retains no completed content byte cache. |

This is a source-supported account of the existing normal path, not proof that
all asynchronous disposal completed before the immediate CDP samples. Bootstrap
does not await all SDK joins, and preserving a persisted document is intentional.
Two CDP documents alone cannot distinguish that case from a detached document
awaiting natural collection or an actual retained owner. No concrete missing
cross-document teardown was identified in these paths. No speculative teardown
or cache rewrite is justified by the current evidence.

### Exact large-paragraph node attribution

The fixture emits one paragraph containing 2,000 `**delta-NNNN** ` tokens.
[`markdownRows`](../../../packages/app/src/streaming-markdown.tsx) virtualizes
Markdown blocks; it does not split one paragraph into independently virtualized
inline nodes. Its AST optimization is bounded by 512 entries and 2 MiB of source
text; this is not a byte measurement of the resulting AST/React/DOM graph.

[`timeline.tsx`](../../../packages/app/src/timeline.tsx) can mark a newly observed
live preview as an arrival. `MarkdownBlock` starts that arrival's displayed-text
model empty and decorates its added text with `FadingText` spans. Each rendered
text leaf covered by that chunk gets a span, even though there is only one
paragraph. There are at most 32 chunk records, but that bound does not limit the
number of text leaves intersecting one chunk.

A temporary diagnostic test rendered the actual current `markdownRows` and
`MarkdownBlock` under the production React/StyleX test configuration:

| Same 2,000-token paragraph | Descendant count including block root | Fade spans | Strong elements |
| --- | ---: | ---: | ---: |
| Initial live value without fresh arrival | 6,001 | 0 | 2,000 |
| Fresh live arrival | 10,000 | 3,999 | 2,000 |
| Fresh arrival after clock passes all fade durations, unchanged text | 10,000 | 3,999 | 2,000 |
| Explicit settled row | 6,001 | 0 | 2,000 |

The animation effect cancels on cleanup, but expiration alone does not remove
its span. Chunk expiry is pruned on another text change; settlement/motion state
clears it on rendering. This reproduces **exactly** the 10,000-node largest block
in both measured documents. It is a sufficient explanation of the extra
connected block nodes, not proof that all of the approximately 190 MiB renderer
RSS jump belongs to these spans. There is no timing claim or proposed animation
change in this audit.

The focused run passed 58 tests across this diagnostic and the existing bootstrap
and runtime suites. Artifacts are
`/tmp/whip-retention-source-audit/markdown-proof.test.tsx` and
`/tmp/whip-retention-source-audit/source-lifetimes.log`. The temporary test was
removed from the worktree after verification; no product/test contract was
changed merely to encode an observed implementation detail. No browser, typing,
forced collection or heap snapshot was used for this proof.

### Remaining ambiguity and bounded follow-up

One retention-only native experiment is sufficient as the next diagnostic:
retain one finite held paragraph, capture bounded scalar
`pagehide.persisted`/execution-context/transport-close evidence, navigate the same
owned document at most three times, and read natural immediate/35-second
heap/DOM/RSS values. Then explicitly settle that fixture turn and take one final
sample. Do not type, upload, force collection, take heap snapshots, introduce a
new threshold or rewrite product ownership. Stable/released document counts
would narrow the owner question; absence of natural collection within this
finite window remains ambiguity, not proof of a leak. This experiment has been
authorized separately; its results must be recorded separately from this source
audit and from the earlier full workload.


## Retention-only native outcome (2026-09-29)

The single approved follow-up completed with exit 0. It used the same staged
candidate renderer/native build as the full pair, under Electron 44.2.0 /
Chromium 152.0.7977.76, from documentation head
`48d862e7d81824512e523717bf7fc874a4e1bca6`. The production code remains
`473d065d01b38a56ef562f1731920e6b4ca3648a`; subsequent commits are audit-only.
Renderer digest:
`0af75b87cce53a45cc1fb60227912395b09dfe601c899c4abfeb0e0d79cdc39d`.
Native SHA-256:
`9503860103811d08f0e41e9fadfdd2d862e6a03686ea1897f6be3df309e01c00`.

The disposable root had one actual provider request, emitting the existing
2,000-token finite paragraph and then holding. The runner performed exactly
three full navigations to that same root, natural immediate/35-second samples,
then explicit successful fixture settlement and another immediate/35-second
pair. It did not type, upload, force collection, take heap snapshots, modify
product code, or use installed runtime/browser state. The other agents and
parent had joined their local workloads for this interval. Passive evidence was
bounded to 16 document lifecycle records, 128 context events, 1,024 IPC lifecycle
records, eight renderer-frame summaries and 96 active transport identities.
No diagnostic bound overflowed.

| Natural phase | Documents | DOM nodes | Fade spans | App RSS MiB | Renderer RSS MiB | Used JS MiB | Embedder MiB |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| finite-preview-before-navigation | 1 | 6,760 | 36 | 858.5 | 459.0 | 30.4 | 89.4 |
| navigation-1-immediate | 2 | 16,798 | 3,999 | 1016.8 | 616.5 | 28.3 | 202.5 |
| navigation-1-natural-35s | 1 | 10,321 | 3,999 | 1032.1 | 615.5 | 33.2 | 139.0 |
| navigation-2-immediate | 2 | 20,654 | 3,999 | 1170.3 | 753.8 | 33.9 | 358.6 |
| navigation-2-natural-35s | 1 | 10,321 | 3,999 | 1142.3 | 720.4 | 26.2 | 143.1 |
| navigation-3-immediate | 2 | 20,654 | 3,999 | 1187.9 | 765.8 | 38.2 | 361.1 |
| navigation-3-natural-35s | 1 | 10,321 | 3,999 | 1074.1 | 649.4 | 23.3 | 137.0 |
| explicit-settlement-immediate | 1 | 14,331 | 0 | 1105.5 | 667.4 | 26.7 | 146.5 |
| explicit-settlement-natural-35s | 1 | 6,308 | 0 | 883.2 | 461.4 | 31.6 | 9.1 |

Every outgoing measured document reported `pagehide.persisted === false`.
Owned process arguments also show Playwright's `--disable-back-forward-cache`;
that is a property of this fixture, not a claim about every installed desktop
launch. Context-clear events preceded each replacement's default/isolated
contexts. Each old native frame stopped sending, released its prepared
connection and closed its final browser-provider peer. Subsequent samples had
no active transports from an older frame. The successful model request occurred
exactly once across all reloads.

After each reload, document count returned naturally from two to one and total
DOM nodes returned to 10,321 while the live paragraph remained held. This
reproduces the previous immediate second-document observation and shows that
these old documents do not accumulate in this bounded case. Following explicit
settlement, the paragraph's 3,999 fade spans disappeared and its largest-block
count fell from 10,000 to 6,001. Natural collection then reduced total DOM nodes
to 6,308; renderer RSS was 461.4 MiB versus 459.0 MiB before navigation, used JS
31.6 versus 30.4 MiB, and embedder heap 9.1 versus 89.4 MiB. App RSS was 883.2
versus 858.5 MiB. No threshold or tolerance was used to label those values.

The old-document retention investigation is satisfied **for this bounded
case**: natural delayed collection plus the exact live-paragraph representation
explain the document/node behavior, with no app-owned accumulating leak or
product fix established. These sequential samples do not attribute every byte
of the RSS excursion. The earlier full-workload peak observations
(1,629,712 KiB baseline / 1,693,136 KiB candidate) remain unchanged; this smaller
one-root experiment is not a replacement or a waiver. Summed process RSS can
double-count shared pages, sampled values can miss brief peaks, and this is not
physical footprint, signed-package or long-duration memory acceptance. No
further memory/timing experiment or speculative optimization is implied.

All five owned processes (37851, 37857, 37858, 37859, 37861) were verified absent
after joined cleanup. The fixture and desktop isolation directories were
removed. Page/probe/cleanup error arrays were empty. Exact artifacts:

- `/tmp/whip-retention-source-audit/document-retention.mjs` — artifact-only runner.
- `/tmp/whip-retention-source-audit/document-retention.log` — complete run output.
- `/tmp/whip-retention-source-audit/native-document-run/retention.json` — scalar phases, lifecycle/context/IPC records; SHA-256 `e1fdcebf5700203718f79f8850bb501e7e7def5091774203d4028f4727c3c993`.
- `/tmp/whip-retention-source-audit/native-document-run/owned-process-arguments.txt` — exact owned launch-flag evidence.

The 72 ms speed decision remains user-accepted. This memory investigation adds
no new latency measurement or acceptance criterion and does not declare all
Phases 5–7 complete.
