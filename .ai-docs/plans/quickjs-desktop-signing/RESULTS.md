# QuickJS desktop signing validation

Implementation and signed execution validated on 2026-09-10. Final distribution notarization and installation are still in progress; see the completion record below.

## Change

The existing `whipcode` backend now receives the single `com.apple.security.cs.allow-unsigned-executable-memory` entitlement when signed with Developer ID. QuickJS remains embedded in the same executable and runs in its existing separate worker process. Hardened runtime stays enabled.

Unexpected worker exits now retain their exit status or signal, engine, startup/execution phase, bounded stderr, and underlying errors. Cancellation remains distinguishable, and `SIGKILL` is not automatically attributed to code signing.

Release checks inspect effective signing permissions and require actual Starlark and QuickJS execution from the final ZIP and installed backend. Candidate verification rejects missing, failed, or mismatched execution evidence.

## Signing experiment

All three modes used the same compiled backend input, with signing as the variable.

| Signing mode | Starlark | QuickJS |
| --- | --- | --- |
| Development/ad-hoc | Passed | Passed |
| Developer ID + hardened runtime, no entitlement | Passed | Killed during startup |
| Developer ID + hardened runtime + intended entitlement | Passed | Passed |

The failing control returned `quickjs worker exited during startup (signal: killed): EOF`. The working build passed JavaScript execution, output, host calls, persistent globals, checkpoint restoration, cancellation, and recovery.

Three observations per working mode produced the following QuickJS results. These are small local samples on a shared machine, not a new benchmark score or a statistical equivalence claim.

| Observation | Development median (range) | Entitled median (range) |
| --- | --- | --- |
| Startup | 213.28 ms (210.19–441.31) | 215.20 ms (209.88–243.05) |
| First cell, including host call/checkpoint work | 9.53 ms (9.41–30.10) | 12.25 ms (10.33–14.62) |
| Resident memory | 77.69 MiB | 77.34 MiB |

The observed timing ranges overlap; these samples do not establish a performance regression. The execution engine, WASM, prompts, and product limits were not changed by this fix.

## Real desktop validation

The signed desktop app used an isolated profile and runtime home. Its backend was upgraded through `_desktop-runtime-sync` from `local-20260910221522675-d6dbc5ef5` to `local-quickjs-signing-20260910`, with the expected installed hash and a new daemon PID.

- QuickJS session `2uxelgevwdpnck4keilq`: Kimi K3 Fast on inference.net executed JavaScript, set `globalThis.signingProof = 37`, read `proof.txt` through `files.read`, and printed `42`. After restarting the daemon, a second desktop-submitted turn restored the checkpoint and printed `43` from `signingProof + 6` without redefining the global. Both turns succeeded. The desktop REPL visibly showed the two completed cells and the restore event.
- Starlark session `iqdnn5tigrdlrzedp4fq`: the same model/provider executed Starlark and returned `42`. The turn succeeded.
- Six model calls across the three successful turns recorded $0.069198 in the local usage ledger. This is the recorded cost estimate, not an independently reconciled invoice.

The validated backend SHA-256 is `dfc63cb8e56a651f6c0c25b60486d9508853eba4ee006d60aa74f40f48c3181c`.

Machine-readable observations, exact cell messages, signing controls, update identity, and model usage are in [validation.json](evidence/validation.json). The report excludes provider configuration and credentials.

## Tests

- `go test -race ./internal/rlm`: passed.
- Desktop suite against the signed native backend: 89/89 tests passed, zero skipped.
- Distribution/release and related script suite: 92/92 tests passed, zero skipped.
- Startup probe self-test: passed.
- Effective entitlement parsing verified against the real signed binary.
- Formatting/whitespace checks: passed.

## Notarization and completion

The package build successfully signed the app and created Apple submission `1e4c7486-c300-4b5c-89e7-e29e993fc71f`. The local Apple `notarytool` process then crashed with excessive recursion / `SIGBUS`. Although the submission continued to report `In Progress`, subsequent inspection found a network disconnect immediately before the crash and no confirmed upload completion. The submission ID alone was insufficient evidence that the archive had arrived. The same signed application was preserved and a recovery upload started with `--no-wait --no-progress --no-s3-acceleration` so upload success could be observed separately from processing.

- [x] Signing control reproduces the issue and confirms the entitlement fix.
- [x] Runtime diagnostics, tests, and release gates implemented.
- [x] Live QuickJS execution, file host call, and resume after restart verified through the signed desktop app.
- [x] Live Starlark regression check and isolated backend upgrade passed.
- [ ] Apple accepts the app and DMG; final package is stapled and verified.
- [ ] Final-archive and fresh-install runtime acceptance passes.
- [ ] Verified build installed into the normal desktop installation.
- [x] Test processes stopped and temporary provider credentials removed.
