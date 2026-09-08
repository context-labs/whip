# Local runtime preparation diagnostic — September 8, 2026

`prepareLocal` took a median **802 ms** for a first installation into a fresh isolated home, **110 ms** to attach to a running fixture daemon, and **276 ms** to restart from a retained installation. This small warm-OS diagnostic does not reproduce the earlier 4.28-second staged launch-to-persisted-runtime result and does not establish a whole-app startup budget.

The measured binary files came unchanged from the signed app's `Contents/Helpers`. Both SHA-256 and Developer ID requirement verification remained enabled. The source `apps/desktop/src/runtime.ts` was copied to a temporary module and instrumented with `performance.now()` spans in `try/finally` wrappers, then transpiled using the existing TypeScript dependency. No shipping source, native executable, signature, or dependency was changed.

The historical signed package has renderer digest `6a400e7240ceb060b2813bad32ae2d0b80716a7bdf5508a8f38410e93f2f04f2`; its manifest lacks the recently added `protocolMinor`. The diagnostic copied that manifest and supplied **protocolMinor=1**, matching the current protocol source, solely to exercise the current source validator. Original and diagnostic manifests are both preserved in `raw-spans.json`. This hybrid is not packaged-artifact acceptance.

The child profiler received only the existing HOME/USER/LOGNAME/TMPDIR, `SHELL=/bin/zsh`, and minimal Finder-style `PATH=/usr/bin:/bin:/usr/sbin:/sbin`. HOME was preserved. The actual login shell resolved PATH through the current user's login configuration; its environment/output was not persisted. Each of four rounds used a fresh `WHIP_HOME`, private retained-runtime directory, and fake provider configuration pointing at an unused loopback port. No session was submitted and no provider credential was loaded. Each round performed one first install, two warm attaches, a clean daemon stop, and one retained restart. All daemons reported no network listener.

| Median elapsed stage | First install, n=4 | Warm attach, n=8 | Retained restart, n=4 |
| --- | ---: | ---: | ---: |
| Entire `prepareLocal` | **802 ms** | **110 ms** | **276 ms** |
| Hashing, all verifications combined | 70 ms | 21 ms | 44 ms |
| `codesign`, all verifications combined | 141 ms | 46 ms | 94 ms |
| Login shell and PATH normalization | 26 ms | 24 ms | 28 ms |
| Daemon status subprocesses combined | 37 ms | 18 ms | 36 ms |
| `installRuntime`, including its verification work | 154 ms | — | 69 ms |
| `daemon start` subprocess through readiness | 513 ms | — | 72 ms |

The rows overlap: installation includes its own hashing/signature checks. First install verifies the bundled source before executing status, verifies it again inside installation, and verifies the copied retained files. Total ranges were 788–855 ms, 105–129 ms, and 267–280 ms respectively. Hash/signature data and disk caches were warm. These are diagnostic samples, not the plan's 30-run distribution gate.

The largest measured first-install stage was `daemon start`. Its 513 ms versus 72 ms for restart includes new runtime process execution, empty-database initialization, and readiness polling. This profile does not separate those internal costs; assigning the difference entirely to migrations, Gatekeeper, or executable mapping would be speculation. Verification is the largest warm-attach stage, but removing or caching trust checks is not justified by this result.

The smallest safe optimization to consider is to **defer login-shell PATH recovery until the initial daemon status says a start is needed**. Status executes the already verified Whip binary by absolute path and probes the isolated socket; attaching to an existing daemon does not change that daemon's environment. Running that status with the inherited Finder environment preserves verification and avoids a shell on the attach path. The observed saving would be about **24 ms per warm attach** on this machine, and it would also avoid the shell's existing three-second timeout in a slow-shell case. This is an estimate from the measured stage, not a tested implementation change. Fresh-start PATH recovery must still finish before starting the daemon.

To explain the 4.28-second whole-app observation, the next useful step is one correlated trace from process launch through `app.whenReady`, renderer load/boot, `prepareConnection` entry/exit, and SDK readiness/profile persistence under the same launch conditions. Native preparation here accounts for less than one second; the earlier result may include cold caches, different environment/load, framework startup, or the renderer/SDK path. Do not claim this profile has identified the remaining delay.

Run the read-only diagnostic with the signed bundle present:

```sh
node .ai-docs/plans/desktop-app/evidence/local-runtime-profile/run-profile.mjs
```

It writes `/tmp/whip-runtime-profile-results.json` by default. `WHIP_COMPARISON_REPOSITORY` and `WHIP_RUNTIME_PROFILE_OUTPUT` can select another checkout/output. The diagnostic operates on fresh private homes and stops each owned daemon in cleanup. Eight unique daemon PIDs were confirmed absent after the run; all profiler homes and retained copies were removed. `raw-spans.json` retains every nested span, the source digest, exact helper hashes, progress boundaries, and daemon IDs. No code optimization was implemented.

## Smallest useful packaged measurement next

The current 110 ms attach path does not warrant an optimization detour. After the final signed rebuild, use a narrowly scoped, opt-in fixture timing collector in the native host and its existing preload, without enabling debugging fuses or changing renderer dependencies. A parent process should timestamp bounded stage notifications on one monotonic clock, as in the shell comparison, so main-process and renderer `performance.now()` origins are not accidentally compared.

Collect these boundaries in one launch: launcher request, `app.whenReady`, native `loadURL`, existing desktop `ready` notification, entry/exit of `prepareConnection`, and a retained-session DOM criterion after font readiness and two animation frames. The last criterion should require the expected fixture transcript and enabled composer; neither `ready-to-show` nor persisted runtime identity alone proves usable session rendering. The common frozen renderer should remain identical between web and packaged app.

Run separate scenarios for (a) a running fixture daemon with retained app state, (b) stopped daemon with installed runtime, and (c) first retained installation with a fresh fixture database. Use the final signed/fused `.app` launched through LaunchServices with explicit private fixture directories and minimal Finder environment. Record at least 30 warm process launches per accepted scenario, verify GUI/process shutdown between samples, and report Gatekeeper/quarantine first-open checks separately. Preserve the renderer and native manifests with the raw events. The instrumentation should report timings and readiness booleans only, with no transcript, prompt, credential, or environment output.
