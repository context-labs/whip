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
