# Signed packaged startup measurement

Measured on 2026-09-08 UTC, Apple M4 Max, 128 GiB RAM, Darwin 25.3.0. The actual Developer ID signed `Whip.app` ran through LaunchServices with Electron 44.2.0, Node 24.20.0 and libuv 1.52.1. All 63 measured launches and two setup launches completed successfully through the normal application quit path.

| Scenario | Samples | Shell mark median / p95 | Usable UI median / p95 | Usable range |
| --- | ---: | ---: | ---: | ---: |
| First installation, fresh app and daemon state | 3 | 0.980 s / insufficient samples | 2.570 s / insufficient samples | 1.938–4.144 s |
| Restored session, daemon already running | 30 | 0.750 s / 0.912 s | 1.053 s / 1.305 s | 0.856–1.341 s |
| Restored session, daemon stopped before launch | 30 | 0.660 s / 0.875 s | 1.129 s / 1.572 s | 1.023–1.773 s |

These are conservative observation upper bounds. The existing `whip-shell-ready` mark represents the shared renderer's two-frame shell milestone. Session usability additionally requires the expected restored route, visible host selector, connected composer with enabled Add context control, the seeded retained answer in the conversation, no connection/error notice, loaded fonts, and two animation frames. The editable textarea alone cannot prove connection.

The warm-attach observations fall below the proposed 1.0 s shell and 1.5 s retained-session p95 targets; retained daemon startup falls below the proposed 3.0 s target on this machine and small fixture. This does **not** establish the controlled cold-cache budget, lower-powered Mac performance, large retained-session performance, typing/streaming latency, or the full release acceptance matrix. The three fresh installations are reported individually in the raw evidence and are not a cold-cache or first Gatekeeper/quarantine benchmark. Their 4.144 s maximum remains visible.

## Artifact and fixture

- Renderer digest: `fb6474fdaed31a31ef48de7115708a0af2003f251a28daf2196724eb63f0108b`.
- Sealed `app.asar` SHA-256: `3e1122b39186e854fb48a23fa064cdba2c35df1914aad9d75b821f34fb9d8430`.
- Runtime protocol 4.1, schema 10, signed team `JAPWPV5JY2`. Exact native hashes, dirty source/lockfile provenance, and fuse values are in the raw report.
- `verifyDesktop` checked signatures, sealed assets, native helpers and shipping fuses before measurement. No inspector, CDP, Node option escape, alternate preload or fuse changes were used. This run did not assert notarization.
- App and daemon had a dedicated empty `HOME`, private `WHIP_HOME`, `userData` and `TMPDIR`. Finder-style `PATH=/usr/bin:/bin:/usr/sbin:/sbin` and `SHELL=/bin/zsh` were explicit. Only the `/usr/bin/open` launcher retained the user's home for LaunchServices. This differs from the earlier diagnostic profile that recovered PATH through the real login home.
- The signed Go daemon handled a real SDK submission against a private loopback fake provider. Its first response requested `rlm_exec`; the second verified the actual tool's marker and result `42`, then emitted the assistant answer. The snapshot confirmed the answer was durable. Exactly two provider requests occurred; the provider was closed before timing sessions.
- Preparation opened the session once through the product deep link and quit normally. All measured retained launches used ordinary saved tab/route restoration without a deep link. The native private Unix socket was verified against the exact short-path daemon contract and owner-only directory. No daemon TCP listener or user provider credentials were used.

## Timing limits

The parent captures `process.hrtime.bigint()` immediately before spawning `open -n -W -a Whip.app`. The opt-in main-process probe uses the same Darwin monotonic clock. The matching vendored libuv implementations in [parent Node 24.14.1](https://github.com/nodejs/node/blob/v24.14.1/deps/uv/src/unix/darwin.c#L56) and [Electron's Node 24.20.0](https://github.com/nodejs/node/blob/v24.20.0/deps/uv/src/unix/darwin.c#L56) both use `mach_continuous_time`. The renderer clock has a different origin, so its shell mark is correlated through the main process's executeJavaScript call/return bracket. The runner checks that main timestamps fall inside the parent's launch-to-report interval.

The `connected` and `usable` lower/upper fields bracket the **observation call**, not the unknown instant when the UI first became ready. Reported figures use only the conservative upper field. Polling runs every 25 ms; a qualifying DOM observation waits for fonts and two animation frames, with a 250 ms cap. Total probe wall time was median 93.9 ms / p95 128.8 ms for warm attachment and median 78.6 ms / p95 120.1 ms for retained startup. These wall totals include frame waits and IPC, not just CPU work; the initial private result-file write and sync add further unseparated overhead. No observer-disabled A/B was run, so do not subtract the wall totals to invent an uninstrumented startup time.

The scenarios ran sequentially with OS caches warm and no concurrent builds or validation suites. Cache drift and ordinary background scheduling remain; the slightly lower shell median in the later daemon-start scenario is not evidence that restarting a daemon accelerates the shell. Thirty samples support this exploratory p95, not a broad hardware or release population claim. This uses a newer renderer than the frozen Electron/Electrobun comparison; the two experiments must not be combined into a direct framework performance delta.

## Reproduction and cleanup

From the desktop worktree, after rebuilding and signing the reviewed sources:

```sh
node apps/desktop/scripts/startup.mjs --samples 30 --first-samples 3
```

The probe is inert unless the explicit fixture and startup-probe flags are both enabled. It writes only fixed `startup.json` inside the private fixture `userData`, validates a per-launch nonce, observes static bounded DOM checks, and exits through the normal main-process quit path. Cleanup signals require both the exact executable path and that launch's unique argv nonce; failed attempts are retained in the evidence.

See [raw samples and verification](packaged-startup.json), [independent cleanup audit](packaged-startup-cleanup.json), [runner](../../../../apps/desktop/scripts/startup.mjs), and [probe](../../../../apps/desktop/src/startup-probe.ts). The audit found none of the 65 recorded GUI PIDs, none of the 35 daemon PIDs, no process belonging to the bundle, and no remaining startup fixture directory.
