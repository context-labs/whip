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
