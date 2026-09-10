# Selectable execution runtimes and real model benchmark

Branch: codex/provider-onboarding (shared working checkout; unrelated TUI change preserved)

## Goal and authority
Implement the approved [research plan](../../../../quickjs-wasi-research/WHIP-MULTI-RUNTIME-PLAN.md) and run real matched Starlark/QuickJS tasks with `kimi-k3` on `https://api.inference.net/v1`. The user's 2026-09-10 implementation request supersedes the historical research-only restrictions. Starlark remains default; each root and all descendants share an immutable execution engine. Pending-operation durable continuation is explicitly a later milestone.

## Frozen implementation contracts
- Stable IDs `starlark` / `quickjs`; languages `starlark` / `javascript`.
- `rlm.KernelOptions.Engine`, trusted bundled engine registry, same daemon host authority.
- Public protocol major 6 explicitly requires updated consumers; private worker protocol 2 negotiates engine identity.
- Engine-neutral result version 2 has explicit value presence and engine metrics; no synthetic JS Starlark steps.
- New root authoritative `execution_engine`, config `rlm.defaultEngine`, CLI `--rlm-engine`. Children derive root engine and cannot override.
- Engine-qualified, integrity/ownership-checked checkpoint envelopes and BLOB data; legacy Starlark scratch retained. QuickJS images only at settled boundaries.
- Guest host payloads preserve exact integers; unsafe Number values are rejected instead of silently rounded. Native guest values and display previews are separate contracts.

## Ownership and work
- [x] Runtime/worker/driver: internal/rlm, bundled WASM, wazero, async pump, conformance and microbenchmark definitions.
- [x] Selection/storage/daemon: internal/session, config, protocol generation, daemon and CLI creation/resume.
- [x] Clients: SDK result decoding, immutable retry payloads, app picker/defaults, language views, TUI.
- [x] Whole-task observer and incremental evidence export for headless CLI.
- [x] Deterministic conformance and adapter smoke, Linux packaging/signals/cleanup.
- [x] Live pilot and completed frozen matched formal study; all v1 failures retained. Separately frozen repository follow-up completed.
- [x] Results with all attempts, verifier truth, per-call usage/cost, limitations and reproducibility.
- [x] Documentation, task check, relevant race checks and independent adversarial review.

## Benchmark decisions
Use local Docker Harbor/Pier adapters written for Whip. Do not copy Frontier's unlicensed workflow code; fetch task sources from their Apache-2.0 upstream repositories. Label any selected Frontier tasks adapted methodology, with no leaderboard comparability claim. Fresh daemon/home/workspace per arm, whole tree accounting, bounded total/per-trial spending, serial paired order, same model/generation and host controls. Pilot and formal data kept separate. Raw task verifiers/solutions stay outside agent-accessible mounts. Working tree source archives and resulting binary/image digests identify the exact build without committing this shared checkout.

Authenticated model discovery verified `kimi-k3` with high effort support and prices $3.95/M input, $19.80/M output, $0.40/M cached input at 2026-09-10. Catalog evidence and route metadata are archived with trials. The assumed $50 allocation was retained; final observed catalog cost is $15.490313 and charged/reserved exposure is $23.936665. These are estimates/reservations, not provider bills.

## Validation
Preserve existing Starlark tests; exercise engine/descendant identity through create/retry/fork/restart, exact host values, JS REPL errors, Promise settlement, image corruption, process death/cancellation and quota failure. Test old/new consumer compatibility deliberately. Run task check and affected race suites, then controlled microbenchmarks serially and real Kimi tasks with retained raw evidence.

## Progress
2026-09-10: audited current seams, runtime/session/client lanes started. Docker 29.4.0 and Go 1.27.0 available; authenticated model discovery succeeded. Existing unrelated change: internal/tui/thin_update.go.

### Runtime implementation record

2026-09-10: runtime production source is ready for frozen-binary testing. `internal/rlm/engine/quickjs` embeds the exact `quickjs-wasi@3.6.0` binary (WASM SHA-256 `b006d95d9475edf7c6648cc3eb391d3b780efdd99022fbfbb470f2359da460ff`) inside the existing stripped-environment worker process; `wazero v1.12.0` is the only new Go dependency. The package archive SHA-512 and bundled WASM equality were verified, with source provenance and QuickJS-NG MIT notices retained beside the binary.

Private protocol v2 negotiates engine/build/ABI/profile and transports opaque images in ordered bounded chunks. One parent reader and one writer/callback owner serve bounded correlated calls; one guest owner runs QuickJS outside host goroutines. Default guest queue/concurrency is 16; `rlm.maxConcurrentHostCalls=1` supplies the matched primary arm while still letting Promise.all queue. The native asynchronous arm is separately named in microbenchmarks. Each image captures only terminal evaluation with no pending owned calls/jobs. Full JavaScript lexical bindings, closures, cycles and class instances survive fresh-worker restore. Ordinary errors retain partial heap changes; cancellation/traps/stalls discard to the last committed image without replaying effects. Both engines publish new BLOB envelopes, with legacy Starlark scratch fallback. Snapshot change detection includes manifests, so newly skipped Starlark globals cannot silently lose notices.

The host codec accepts passive JSON trees, safe Number integers, exact BigInt integers and immutable decimal/exponent wrappers; it rejects accessors, cycles, unsafe integer Numbers, undefined values, unpaired Unicode surrogates and non-JSON objects. Proxy construction is disabled in this initial profile. Output/result previews tag BigInt/exact numbers. Defaults: 32 MiB guest heap, 64 MiB WASM memory, 256 MiB process RSS, 1,024 host calls/cell, 100,000 jobs/cell, 30 s guest compute, 10 min cell watchdog, 64 KiB output, 40 MiB image. Registry descriptors and hidden `_kernel -engine <id> -describe` report build, bridge/guide hashes, capabilities and unit-qualified limits. Bridge hashes are checked in both checkpoint envelopes and internal image profiles.

Validation completed before final freeze:
- `go test ./internal/rlm/... -count=1` passed (native Darwin/arm64); legacy Starlark tests retained and protocol fixtures updated for the handshake.
- `go vet ./internal/rlm/...` passed.
- `go test -race ./internal/rlm/... -count=1` passed again on the final frozen runtime source (`internal/rlm`: 48.054 s; QuickJS leaf: 3.169 s).
- Crosscompiled `GOOS=linux GOARCH=amd64 go test -c -o /tmp/whip-rlm-amd64.test ./internal/rlm`; ran the entire QuickJS, checkpoint-manifest, descriptor and concurrency conformance group inside `debian:bookworm-slim` with Docker `--platform linux/amd64`: all passed.
- Linux smoke exposed a real WASM ABI issue: amd64 host-call i32 parameters can contain nonzero upper 32 bits of their uint64 slots. A full-width clock-id comparison falsely rejected realtime clock 0, causing a QuickJS startup `unreachable`. Imports and exported i32 results are now normalized; `TestWASIClockNormalizesI32ArgumentWidth` pins the fix.

Coverage includes full image restore, BigInt/decimal/exponent/negative-zero round trips, Unicode, hostile prototype/accessor serialization, Promise.all early rejection with a live sibling, forgotten await and unhandled rejection, cancellation settlement, stalled promises, lexical errors, failed publication/corruption and exact concurrency ceilings. `BenchmarkRuntimeWarmCell` covers arithmetic, host calls, matched single-call concurrency, separately labeled native asynchronous fanout, and image capture; `BenchmarkRuntimeCheckpointRestore` and `BenchmarkRuntimeColdStart` separate lifecycle costs. Preliminary measurements under concurrent build/test load were diagnostic only. The final serial study contains 130 samples with builds, tests, and task containers idle; shared desktop applications remained active. See [runtime measurements](../../../evals/runtime-ab/RUNTIME-MEASUREMENTS.md).


### Completed benchmark

2026-09-10: implemented and validated both production engines, then completed
130 native runtime samples and the real Kimi K3 task studies. Primary v2:
Starlark 4/8, QuickJS 3/8. Separate repository follow-up: 0/2 each; all four
submitted empty committed patches, with the final Starlark call settling
slightly above its cost-admission cap. Every pilot, aborted v1, setup probe,
primary and follow-up attempt is retained. The frozen binary rebuilt to the
same hash, 1,485 production source files remained unchanged, and task images
matched across both studies. All benchmark containers were removed.

[The final report](../../../evals/runtime-ab/RESULTS.md) includes verifier truth,
coverage, cost/time estimates, native runtime measurements, limitations and
reproduction. Both engines remain supported and Starlark remains default.
Future-run content export was improved only after all recorded trials finished;
76 final harness/analysis tests passed. Durable continuation of pending effects
remains the explicitly deferred milestone from the approved plan.
