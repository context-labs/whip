# Runtime measurements

Measured 2026-09-10 from the implemented Whip worker path. Each condition has
10 serial Go benchmark samples of at least one second. Host: Apple M4 Max,
macOS 26.3.1, Go 1.27.0, Darwin/arm64, GOMAXPROCS 16. The shared desktop had
unrelated active applications; our builds, tests, and task containers were idle.

| Operation | Starlark median | QuickJS median |
| --- | ---: | ---: |
| Warm 1,000-iteration arithmetic cell | 0.0549 ms | 1.1755 ms |
| Warm single host call | 0.0538 ms | 0.8863 ms |
| Eight 1 ms host calls, matched concurrency 1 | 11.82 ms | 14.42 ms |
| Changed counter + checkpoint publication to memory | 0.0563 ms | 8.7297 ms |
| Evict, restore checkpoint, evaluate next cell | 34.99 ms | 221.06 ms |
| New worker + first cell | 48.07 ms | 214.39 ms |
| Counter checkpoint bytes | 95 B | 1,507,802 B |

QuickJS's separate native-async condition (up to 16 active calls) takes
**6.13 ms** for those eight 1 ms calls. The matched condition deliberately
allows only one active host call in either engine. This identifies a benefit
from overlapping host waits; it is not a claim of faster guest computation.

The lifecycle and checkpoint costs are materially higher for this QuickJS
integration. Its checkpoint retains the complete settled heap, while Starlark
stores a small supported subset and may omit bindings. A 95-byte partial image
and a 1.44 MiB whole image provide different durability guarantees.

The repeated-run differences in the matched latency rows are distinguishable
in benchstat (all p < 0.001). Median confidence ranges are about ±0–5% except
the Starlark single-host case (±11%). These statistics describe these repeated
microbenchmarks, not task success or model latency. No aggregate geomean is used
as a product performance score.

Warm arithmetic/host cases omit checkpoint storage. The checkpoint case uses
an in-memory store, so it excludes SQLite persistence. Go allocation counts
cover the benchmark parent, not the worker heap. Additional engine timing
counters in raw output describe the last cell; the repeated `ns/op` values are
the latency measurements summarized above. Timer-based host waits also include
scheduler delay and are not exact 1 ms sleeps.

Reproduce with the command in [the workflow](README.md#runtime-measurements).
Evidence: [raw samples](results/microbench.txt),
[benchstat comparison](results/benchstat.txt),
[machine-readable medians](results/microbench-summary.json),
[environment](results/validation/environment.json), and
[host load](results/validation/host-load-before.txt).
The comparison files only remove `/starlark` and `/quickjs` from corresponding
benchmark names; values are unchanged. Native-async rows remain in the raw file.
