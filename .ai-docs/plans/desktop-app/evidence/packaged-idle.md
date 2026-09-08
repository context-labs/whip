# Signed app idle CPU and memory

The idle CPU result is **not consistently below the proposed 1% target**. Two runs showed intermittent bursts; a third was quiet. All three exceeded the 350 MiB aggregate desktop RSS investigation threshold, with substantial variation in graphics memory and OS residency. No shipping source, fuses, renderer, native helpers or runtime verification changed during these measurements.

Same signed artifact and Apple M4 Max / 128 GiB machine as [packaged startup](packaged-startup.md). Renderer `fb6474fdaed31a31ef48de7115708a0af2003f251a28daf2196724eb63f0108b`; sealed archive SHA-256 `3e1122b39186e854fb48a23fa064cdba2c35df1914aad9d75b821f34fb9d8430`. Root builds/tests were paused during each window.

| Run | Settle / measured interval | Desktop CPU, one core | Desktop RSS mean (range) | Daemon + worker CPU / mean RSS | Complete RSS mean |
| --- | --- | ---: | ---: | ---: | ---: |
| [Initial](packaged-idle.json) | 30 s / 60.957 s | 6.037% | 549.0 MiB (501.0–594.4) | 1.099% / 80.7 MiB | 629.7 MiB |
| [Repeat](packaged-idle-repeat.json) | 90 s / 61.624 s | **≥7.935%**; incomplete transient-child accounting | 529.3 MiB (511.7–548.7) | 0.990% / 77.1 MiB | 606.3 MiB |
| [Attribution attempt](packaged-idle-attribution.json) | 90 s / 61.245 s | 0.229% | 352.2 MiB (341.8–393.6) | 0.914% / 59.4 MiB | 411.6 MiB |

The corresponding complete CPU totals are 7.136%, at least 8.925%, and 1.143% of one logical core. The table keeps daemon/worker cost separate instead of hiding it. These three windows are exploratory observations, not a population percentile or a resolved idle acceptance result.

## Method and ownership

The existing `startup.mjs --idle` runner verified the signed/fused bundle, created isolated empty `HOME`, `WHIP_HOME`, app `userData` and `TMPDIR`, seeded the same real two-request tool/answer fixture through the signed daemon, and verified the retained UI in a setup launch. The actual idle launch restored that session normally with the startup probe **disabled**. No active turn or live provider remained. The private Unix socket was checked, and the daemon had no TCP listener.

During the initial run's settle interval, a native CUA accessibility observation confirmed the exact fixture route, This Mac selector, retained answer `Verified Whip desktop startup fixture: 42`, enabled Add context/model controls and “No currently observed activity.” No UI actions or observations occurred during its CPU window. Neither later launch received a CUA call. This is not proof that macOS accessibility support was globally disabled, and focus/occlusion was not instrumented continuously.

Every five seconds, macOS `ps` supplied PID, parent PID, process start time, accumulated user+system CPU time, RSS and executable. Thirteen snapshots span at least 60 seconds. CPU percentage is the change in accumulated CPU seconds divided by the measured monotonic interval, multiplied by 100; it is not `ps`'s smoothed `%CPU` or a percentage divided by the machine's core count. RSS uses the documented 1024-byte units. Collection scan durations and exact process rows are retained. The process scans are an external observer, with their overhead and normal host scheduling still present.

The desktop tree consisted of main, GPU, network utility and renderer processes. The daemon was detached with parent PID 1 and had one child Whip worker, so its tree was measured explicitly rather than assumed to be a GUI descendant. Start-time identities guard against PID reuse. The repeat observed one additional short-lived main child, PID 10123, with executable text `(whip)`. Its final CPU counter was unavailable, so the original exact aggregate remains `null`; [derived summary](packaged-idle-summary.json) reports only the persistent four-process CPU lower bound. The worker's CPU contribution was zero in the first two runs and 0.01 CPU seconds in the third.

## CPU investigation

The initial 6.037% breaks down as main 2.133%, renderer 2.690%, GPU 1.214%, network 0%. Most activity occurred in a roughly 25-second middle burst, with surrounding five-second intervals much quieter.

The repeat's first roughly 31 seconds of measurement were around 0.55% desktop CPU. The sharp increase coincided with the transient main child at approximately 126 seconds after launch; the four persistent processes then remained elevated for the rest of that window. This is correlation, not identification of the child's command or proof of a reconnect. The sealed bundle has no update feed, so its optional automatic update timer was inactive. Read-only source review found session-list and tab-summary polling at two seconds, attention polling at three seconds, and SDK heartbeat at 30 seconds. Those are background work to inspect; the measurements do **not** establish any of them as the burst's cause. Main has no recurring daemon status timer.

The runner was hardened to retain a persistent-process CPU lower bound when the tree changes and to collect five-second native main/renderer stacks after a high or unaccountable window. The third attempt did not reproduce the burst, so that condition did not trigger. **No native stack attribution was obtained.** The temporary homes were cleaned automatically before a later profile/history inspection could recover a cause. We therefore cannot blame accessibility, reconnection, polling, focus, GPU activity or memory pressure, and cannot call the idle gate resolved.

The smallest next diagnostic is to capture the exact short-lived child command and owned main/renderer stacks when another burst occurs, while recording connection/profile changes and window focus/occlusion. A background `open -g` comparison could test focus sensitivity but would describe unfocused-visible behavior, not focused-idle acceptance. This needs controlled measurement, not a speculative timer or framework change.

## Memory attribution

macOS `footprint` ran against explicit owned PIDs **after** each CPU window. It succeeded with shipping security settings and de-duplicated shared mappings within each selected group. Its accounting emphasizes dirty physical footprint; it is a different metric from summed RSS, not a substitute for that threshold.

| Run | Desktop de-duplicated footprint | Daemon + worker footprint |
| --- | ---: | ---: |
| Initial | 211.0 MiB | 43.7 MiB |
| Repeat | 450.3 MiB | 46.8 MiB |
| Attribution attempt | 137.9 MiB | 42.7 MiB |

The initial desktop result also contained about 216.9 MiB of clean mappings and 124.5 MiB classified reclaimable. RSS sums can count shared pages repeatedly and include reclaimable code/data; they are neither JS heap nor unique memory cost. These categories should not be casually added into a new “true memory” number.

The repeat's 450.3 MiB footprint included approximately 233.7 MiB of **owned unmapped graphics memory**, making GPU allocation a concrete contributor to investigate. Its GPU process footprint alone was approximately 310.9 MiB. The quiet third run had approximately 39.9 MiB GPU, 50.3 MiB renderer, 41.8 MiB main and 6.8 MiB network footprints before group de-duplication. The raw opaque allocator-tag categories are preserved; they do not identify a JS heap leak or application allocation stack.

Whole-machine `vm_stat` was captured before launch, after settling and after measurement. Free-plus-speculative memory changed by −819 MiB, +1182 MiB and −945 MiB respectively from pre-launch to post-window. Compressor occupancy was already roughly 49–59 GiB; the third run ended around 58.6 GiB. These large unrelated system changes prevent treating global memory delta as Whip's unique cost. The third run also had 12 swap-ins; the first two had none. No claim of a leak, steady-state ceiling, or isolated system-memory delta follows from these short windows.

## Reproduction and cleanup

```sh
node apps/desktop/scripts/startup.mjs --idle
node apps/desktop/scripts/startup.mjs --idle --idle-settle 90 --output /tmp/whip-idle-repeat.json
```

Raw process samples, footprint JSON/text and OS memory counters are embedded in each linked report. [Derived comparison](packaged-idle-summary.json) retains the repeat's CPU-accounting limitation. [Cleanup audit](packaged-idle-cleanup.json) found all 28 tracked processes absent, no process from the signed bundle, and no remaining startup fixture directory. The idle GUI received TERM only after its exact executable and unique launch nonce were verified; daemon stop was scoped to its private home. Real user daemon/data and the earlier root-owned continuity fixture were not touched.
