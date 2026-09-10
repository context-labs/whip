# QuickJS desktop signing fix

Date: 2026-09-10

Status: implementation, signing controls, automated tests, and live desktop execution are complete. Final notarization and installation are in progress. See [RESULTS.md](RESULTS.md) for evidence and the completion record.

## Decision

Keep the existing single-backend distribution. Embed QuickJS's WASM in `whipcode` and continue launching `whipcode _kernel -engine quickjs` as a separate worker process. Add `com.apple.security.cs.allow-unsigned-executable-memory = true` to the macOS signing policy for `whipcode`.

Keep hardened runtime, Developer ID signing, notarization, signature verification, and artifact hashing. Preserve both execution engines and wazero compiler mode. The change requires no separate worker executable, runtime-selection migration, or new installation layout.

The accepted tradeoff is executable-wide permission: daemon, CLI, Starlark worker, and QuickJS worker processes launched from this signed executable receive the same executable-memory exception. Existing process separation, WASM isolation, host-tool permissions, and resource controls remain in place. This exception relaxes a macOS defense; it does not grant new application-level tool permissions.

## Evidence and remaining uncertainty

- QuickJS's `quickjs.wasm` is embedded in `internal/rlm/engine/quickjs/bridge.go`; the factory explicitly selects `wazero.NewRuntimeConfigCompiler()`.
- `internal/rlm/kernel.go` launches the current executable with `_kernel`; QuickJS already runs outside the daemon process.
- `apps/desktop/scripts/build.mjs` signs `whipcode` with hardened runtime but supplies no entitlements. `forge.config.cjs` preserves that native signature. Electron's own entitlement file therefore does not fix the backend.
- Five observed QuickJS startup failures matched macOS code-signing crashes. The latest session, `5royo3drlrhsbp2gechq`, failed at 16:51:53 MDT with `EOF` and zero model calls. Its crash report recorded `SIGKILL (Code Signature Invalid)` and `CODESIGNING: Invalid Page` in dynamically allocated executable memory.
- The installed executable passed on-disk signature verification. The problem is permission to execute generated code, rather than a demonstrated corrupt executable.
- The pinned wazero v1.12.0 allocator maps writable memory and changes it to executable memory without `MAP_JIT`. Apple's documented executable-memory exception matches this mechanism. A signed positive/negative execution test must confirm that it is sufficient for Whip.
- The earlier task benchmarks ran in Linux containers. Existing macOS release checks verify signatures, application startup, and other behavior, but do not require an actual QuickJS cell to execute.

Primary references: [Apple executable-memory entitlement](https://developer.apple.com/documentation/BundleResources/Entitlements/com.apple.security.cs.allow-unsigned-executable-memory), [Apple JIT entitlement](https://developer.apple.com/documentation/BundleResources/Entitlements/com.apple.security.cs.allow-jit). Local incident evidence: `/Users/samheutmaker/Library/Logs/DiagnosticReports/whipcode-2026-09-10-165154.ips` and the session database inspected read-only during diagnosis.

## Implementation sequence

### 1. Prove the exact signing change

Use isolated temporary copies of the same macOS arm64 backend build. Sign one with the current hardened policy and another with hardened runtime plus the single proposed entitlement, using the production Developer ID identity when available. Do not alter the installed executable or the user's active daemon for this experiment.

Exercise the real worker with the existing `rlm.Kernel` interface and `KernelOptions.Command`, pointing explicitly at each copied executable. Require a real JavaScript result and one deterministic host-call round trip. A version query, engine descriptor, or successful process launch is insufficient.

Record build identity, unsigned build inputs, signed artifact hashes, signature flags, entitlements, worker outcome, and relevant crash evidence. Keep credentials and personal session data out of the report.

Acceptance: the current policy reproduces the startup failure and the proposed policy completes the same operations. If the proposed entitlement is insufficient, investigate that result before expanding permissions or changing runtimes.

### 2. Apply the backend signing policy

Add `apps/desktop/resources/runtime.entitlements.plist` containing the single executable-memory entitlement. Pass it to the existing `whipcode` signing invocation in `apps/desktop/scripts/build.mjs` for Developer ID builds.

Preserve the existing build order: sign the computer helper, embed its exact bytes, build the backend, sign the backend, then calculate manifest hashes and package the application. Preserve Forge's exclusion of already-signed native helpers from its Electron signing pass.

Keep Electron and `whip-computer` signing policies separate. Do not add `disable-executable-page-protection`, `disable-library-validation`, debugging entitlements, or a blanket entitlement bundle. Development builds may retain their existing ad-hoc signing behavior; they do not count as release-signing evidence.

The installed backend must be the exact signed payload from the package. Rebuild through the existing distribution pipeline instead of re-signing a user's installed executable, which would invalidate its recorded identity.

### 3. Make worker failures diagnosable

In `internal/rlm/kernel.go`, preserve the result of `command.Wait()` and expose it safely after worker completion. Use it in the shared response reader and the QuickJS response loop in `internal/rlm/quickjs_kernel.go` when a worker exits unexpectedly.

Include the selected engine, lifecycle phase, exit status or signal, and bounded stderr. For example: `QuickJS worker exited during startup (signal: killed)`.

Preserve the underlying error for callers. Cancellation, explicit resource-limit termination, protocol errors, and unexpected worker exits must remain distinguishable. A `SIGKILL` alone must not be labeled a code-signing failure; that attribution requires additional evidence. Normal worker shutdown must remain successful. Preserve prompt cleanup, process-group reaping, and checkpoint behavior.

Extend the existing subprocess tests with unexpected nonzero exit, killed worker, clean but premature EOF, cancellation, and successful recovery. Exercise both readers and use the race detector for the shared exit-state change.

### 4. Require execution evidence for release artifacts

Extend `apps/desktop/scripts/verify.mjs` to inspect the signed backend's effective entitlements and hardened-runtime flag, alongside its existing identity and signature checks. Require the intended permission and reject unintended additions to the backend policy. Parse the entitlement payload structurally.

Add a small external-binary acceptance test, preferably `internal/rlm/packaged_runtime_test.go`, using `KernelOptions.Command` rather than implementing the worker wire protocol again. An explicit test executable path must identify the packaged `whipcode`; the signed release lane must fail if it is missing instead of falling back to the Go test executable.

Require both engines to execute code. For QuickJS, cover a computed result, output, a host call, persistent state across cells, checkpoint restoration in a replacement worker, cancellation, and subsequent successful execution. Use deterministic local fixtures with zero paid provider requests.

Integrate this test into the shared `.github/workflows/desktop-release.yml` workflow before release artifacts can advance to publication. Bind the acceptance evidence to the backend hash, build ID, engine descriptors, architecture, and signing policy. Update `apps/desktop/scripts/release-candidate.mjs` and its tests to reject missing, failed, or mismatched runtime evidence. Keep the ordinary desktop check lane useful for unsigned development, while retaining an explicit signed macOS release gate.

Verify the final backend extracted from the distributable ZIP, and the payload installed through the existing installation path, against those hashes. Avoid unnecessary repeated notarization or adding QuickJS execution to every interactive daemon status request.

### 5. Validate the installed desktop experience

Build the signed and notarized candidate through the normal pipeline. Use an isolated application profile, runtime home, workspace, and installation destination for automated checks. Preserve normal production fuses and the normal backend-selection path.

Cover fresh installation and an update from the affected backend to the fixed build. Confirm that the installed executable retains the expected entitlement and hash and that the active daemon is running the new build before checking execution. Exercise a new QuickJS session, additional turns, resume after daemon restart, and a Starlark session. Reuse existing desktop fixtures where possible; the current staged smoke and LaunchServices startup tests are useful foundations but are not themselves proof of QuickJS execution.

Finish with a real desktop session using Kimi K3 on inference.net that demonstrably executes JavaScript and uses a host tool, then resume that session after restart. Record session IDs and outcomes separately from the deterministic release tests. Keep live-provider validation outside the release gate so provider outages cannot obscure packaging correctness.

Compare startup, execution time, and memory against the same runtime build in a working development signing configuration. Report observations without claiming equivalence from identical source alone. A full paid benchmark rerun is unnecessary unless these checks reveal a behavior or performance change. Benchmark settings, model prompts, and normal runtime limits are outside this fix.

## Expected files

| Area | Files |
| --- | --- |
| Backend signing | New `apps/desktop/resources/runtime.entitlements.plist`; `apps/desktop/scripts/build.mjs` |
| Signature and execution acceptance | `apps/desktop/scripts/verify.mjs`; new external-binary runtime test; relevant desktop test scripts |
| Worker diagnostics | `internal/rlm/kernel.go`, `internal/rlm/quickjs_kernel.go`, `internal/rlm/kernel_test.go`, `internal/rlm/quickjs_test.go` |
| Release enforcement | `.github/workflows/desktop-release.yml`, `apps/desktop/scripts/release-candidate.mjs`, `scripts/release-candidate.test.mjs`; distribution evidence wiring where required |
| Installed application validation | Existing desktop smoke/startup fixtures and installation/update tests, extended only where needed |

At initial inspection, `apps/desktop/src/runtime.ts`, `apps/desktop/tsconfig.json`, and `scripts/update-local.test.mjs` already contained unrelated changes. Additional concurrent runtime edits appeared while planning, including changes to `internal/rlm/kernel.go`, `internal/rlm/quickjs_kernel.go`, and worker tests. Preserve all existing changes and reconcile implementation with their latest state before editing overlapping files. The initial source baseline was commit `8f9b130c8` plus its working tree; this plan is not a frozen source snapshot.

## Validation and completion

Run the focused Go runtime tests, the race checks covering worker lifecycle changes, and `npm run test:desktop`. Run `npm run check:desktop` if TypeScript changes are required. Then complete the signed artifact and installed-app checks; ordinary unit tests cannot substitute for them.

- [x] Hardened backend without the entitlement reproduces the failure; the same build with the intended entitlement executes QuickJS.
- [ ] Production backend has the intended entitlement and retains hardened runtime, valid Developer ID signing, and notarization.
- [ ] Both engines execute successfully from the final distribution and installed backend.
- [x] Host calls, persistent state, checkpoint restore, cancellation, and worker recovery pass.
- [x] Unexpected worker exits produce actionable errors without masking cancellation or inventing a cause.
- [x] Release verification rejects a missing entitlement and missing, failed, or mismatched runtime execution evidence.
- [ ] Fresh install and update exercise the fixed backend through the normal desktop path.
- [x] A real Kimi K3 desktop session completes QuickJS work and resumes successfully.
- [ ] Results record the tested artifact identities and any remaining limitations; no unexplained performance regression remains.

If acceptance fails, retain the failure evidence and keep the candidate out of release promotion. Do not silently select Starlark or interpreter mode, or broaden the signing exceptions. The delivered result should be the same QuickJS runtime, packaged as today, with the required permission and proof that the shipped application can execute it.
