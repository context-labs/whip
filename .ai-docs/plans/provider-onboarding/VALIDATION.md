# Provider onboarding validation

Date: September 9, 2026. Branch: `codex/provider-onboarding`.
Implementation base: `7058e007fec0a4f2e926f3b26dcb9e06ed75be5b`.

## Automated checks

| Check | Result and scope |
| --- | --- |
| `task check` | Passed: formatting, Go vet, Whip's event-loop analyzer, all Go tests, deterministic protocol generation, SDK/type checks, production renderer build, web/UI tests, packaging and local-update-script tests. At this run: SDK 276 tests; web 416 tests across 46 files. Subsequent review fixes receive focused checks below. |
| Final `go test ./...` | Passed after all host and TUI review fixes; final formatting and diff checks were clean. |
| `go test -race ./internal/daemon ./internal/session ./internal/config ./internal/protocol ./cmd/whip` | Passed; covers host/service lifetime, configuration, durable commands, protocol and routing. |
| `go test -race ./internal/daemon -run 'Test(Provider\|ConcurrentInference\|OpenAI)' -count=1` | Passed after review fixes: catalog-only model precedence matches runtime resolution; concurrent Inference.net login begins reuse one active host flow; retained terminal outcomes remain bounded. |
| `go test -race ./internal/tui -count=1` | Passed after TUI review fixes. See [TUI report](evidence/tui.md). |
| Final shared-app checks | `npx tsc -p packages/app/tsconfig.json --noEmit` and `npm run test:web` passed: **420 tests across 46 files**, including cross-window recovery, old-host messaging and auth-flow regressions. |
| Desktop | Native tests: 79 passed, 9 existing environment-dependent SSH skips. Focused bridge/UI tests: 24 passed. Final staged production first-message smoke and signed-package acceptance passed. The final package completed 1 first launch, 30 warm attaches and 30 retained-daemon starts without failures; fixture cleanup confirmed the daemon stopped. |

## Behavior and regression coverage

| Behavior | Evidence |
| --- | --- |
| Readiness is local, does not execute secret commands or probe providers | `internal/daemon/provider_connections_test.go`, `provider_selection_test.go` |
| Valid defaults and explicit routes remain selected; ambiguous catalog-only routes require a choice | `provider_selection_test.go`, `internal/tui/setup_test.go` |
| Inference.net first within comparable groups, label exceptions, direct alternatives | `provider_selection_test.go`, `setup_test.go`, `packages/app/test/provider-connections.test.tsx` |
| Pair updates persist together with revision conflict handling | `provider_selection_test.go`, provider service tests, shared provider-setup tests |
| Singleton login choices advance; concurrent begins do not create competing flows | `provider_selection_test.go`; existing multi-choice/cancel/reconnect service tests |
| Keys masked/cleared; cancel, paste and stale replies retain the correct draft | TUI setup tests and actual PTY report; shared auth interaction tests |
| Create and submit retain identities through uncertain delivery; later edits survive | `packages/app/test/welcome-submission.test.ts`, `runtime.test.ts`, `sidebar-creation.test.tsx` |
| Selected permission stored atomically; replay never resets later changes | `internal/session/command_test.go`; isolated native restart scenario |
| Auxiliary models do not send a built-in provider ID to another endpoint | `cmd/whip/daemon_test.go`; actual-daemon native fixture logs |
| Fresh MCP imports stay off while legacy config omission retains semantics | `internal/config/config_test.go` |
| Native install uses verified bytes and existing connection ownership | `apps/desktop/tests/runtime.test.ts`, `packages/app/test/local-runtime.test.tsx`, staged native smoke |

## Real terminal and native UI

Reproduce the staged UI checks on macOS arm64 with the repository's Node 24,
Go and Xcode toolchains (dependencies already installed):

```sh
npm run build:desktop
node apps/desktop/scripts/onboarding-smoke.mjs
```

The script creates its own temporary home, backend and desktop profile, uses
loopback fixtures, and cleans up its own processes. It does not install into
Applications or attach to the normal daemon. For the signed gate, package with
the existing Developer ID configuration, then run:

```sh
node apps/desktop/scripts/startup.mjs apps/desktop/out/Whip-darwin-arm64/Whip.app \
  --samples 30 --first-samples 1 \
  --output .ai-docs/plans/provider-onboarding/evidence/packaged-startup.json
```

Run builds serially against frozen source; `.stage` is shared by packaging and
the staged smoke. Check the renderer/native hashes before combining evidence.

[TUI acceptance](evidence/tui.md) used a newly built binary in actual PTYs with isolated homes. It verified clean and already-configured startup, all presets, safe exploration before session creation, key masking, cancellation, retained drafts, and focus restoration. The configured fixture saw catalog GETs only, proving setup did not send a hidden inference request.

[Desktop acceptance](evidence/desktop.json) is generated by `apps/desktop/scripts/onboarding-smoke.mjs` against the production renderer in the real staged Electron host. One click installs verified bundled backend bytes and connects; relaunch reuses the same fixture daemon. A second scenario uses the actual daemon, SQLite store, streaming adapter and Starlark kernel with a loopback provider. UI actions draft before authentication, enter a masked key, confirm an OpenRouter model, choose a native folder and explicit permission, and send once. The partial response is observed before releasing the final chunk; `rlm_exec` really calculates 42. Reload and daemon restart retain the session, default pair, permission and later draft.

Title generation is explicitly enabled through the existing SDK while the first turn streams, and compaction is explicitly requested through the SDK. These auxiliary controls are route-acceptance checks, not claims that welcome automatically enables title generation. The fixture rejects unexpected model IDs for first turn, tool-result continuation, title and compaction.

The staged smoke uses stock Electron. The separate [signed-package verification](evidence/packaged-verification.json) and [LaunchServices acceptance](evidence/packaged-startup.json) passed against the final Developer ID signed bundle with shipping fuses. The package is not notarized; no publication or production installation was performed. All 61 launches succeeded, covering first launch, warm attachment and retained-daemon startup, with fixture cleanup verified. Any timing values in the staged report are a single scripted warm local observation, including test synchronization. They are not provider latency, usability-study, or release performance claims.

The final staged renderer digest is `0ecc28a917703b58f8b987f91b606fdbb9d4d93c8caefd1343e30c9c445256be`; backend SHA-256 is `d4a7eaf7d9b0eb37db34d5a2021eb5a90268cd19d955484fe1f2262728860dd2`. The staged assets were inspected for the final journal identity checks, older-host update message and flow-ID/URL login-opening guard before the smoke was rerun. An earlier package had overlapped review edits; it was rejected as final evidence and rebuilt from frozen source.

The final signed archive was inspected for the same fixes, and its renderer and
backend hashes exactly match the passing staged smoke. Bundle digest:
`6ac0858dc90b4b568ad5c9f9294c19c9664835c7dfbc7197ed4eca28934ac7d4`.

## Review fixes

Cross-review caught and corrected: catalog-only model routing precedence, concurrent account-login duplication, stale client login inventory, delayed verification URL opening, terminal paste/clipboard draft loss, an underlying TUI palette retaining focus, an unbounded startup readiness query, and cross-window welcome submission races. Tests exercise the failures at their owners rather than adding a second routing/authentication system.

Final TUI cases also verify that an unchecked credential command remains selectable without a recommendation and does not run during initial inventory, and that older hosts missing readiness metadata receive an update instruction without model/default mutations. The focused final TUI race suite and focused final provider race suite passed. Final `go vet` and `whipvet` checks passed after host review fixes.

## Scope and limits

All acceptance state is disposable and isolated; the installed production app, shared daemon, production credentials and sessions were not changed. No public-provider paid inference calls were made. The fixture verifies Whip's wiring, streaming, tool execution, routing and recovery; it does not evaluate every suggested public model's current quality or account entitlement. Prior ChatGPT live acceptance is recorded in the [subscription plan](../openai-subscriptions/README.md).

The user explicitly skipped the proposed 6–8 person usability study. No human-study results or measured conversion claims are implied. Current behavior and setup instructions are documented in README, `docs/features.md`, `docs/frontend.md` and `docs/roadmap.md`.
