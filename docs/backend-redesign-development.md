# Backend redesign development

This is the working loop for [the redesign plan](backend-redesign-plan.md).
Phases 1–2 add the new domain, SQLite store, host configuration, v4 contract,
runtime, runner and SDK described in [backend-domain.md](backend-domain.md).
The new v4 process fixture is required. The retained legacy product fixture
remains a separate reference while later capabilities are ported.

## Start working

Use Go from `go.mod`, Node 24, and Task 3.48.0. From this checkout:

```sh
npm ci --no-audit --no-fund
task check:fast
task check:change
```

The integration branch is `codex/backend-redesign`. Open implementation PRs
against that branch, or stack a phase PR on its pending predecessor. The redesign
workflow also runs for PRs into phase branches. Keep each PR focused on a behavior
or invariant. Current
production gates on `main` and `development` remain unchanged.

| Command | Purpose |
| --- | --- |
| `task check:fast` | Active Go formatting, core tests and process-package build; no npm install needed |
| `task check:change` | Fast gate, active builds/vet/race tests, generated contracts, SDK/examples, real-runtime fixture |
| `task check:phase` | Change gate plus selected admission/recovery/accounting integration regressions |
| `task check:analysis` | New lint findings since the frozen baseline, plus reachable vulnerability checks on the active packages |
| `task check:fixture` | Rebuild SDK and run the isolated process-restart scenario |
| `task dev:redesign -- --minutes=10` | Start a disposable scripted-provider daemon for manual SDK/app work |
| `task dev:v4 -- -directory /tmp/whip-example/state` | Start the new scripted runtime with explicit private storage |

The optional repository pre-commit hook uses `check:fast` on the integration
branch and `codex/backend-redesign-*` branches. Other branches retain the current
hook. `task hooks` installs the repository hook if desired; this is a local Git
configuration choice, not a prerequisite for CI.

## Active scope

`REDESIGN_PACKAGES` in [Taskfile.yaml](../Taskfile.yaml) lists the fast Go checks.
`REDESIGN_INTEGRATION_PACKAGES` adds the extracted subprocess implementation;
their union, `REDESIGN_ALL_PACKAGES`, is used by build, vet, race and analysis:

- `internal/content`: retained immutable content and access primitives.
- `internal/session`: pure durable values, pinned definitions and configuration resolution.
- `internal/store`: fresh schema, uniform root/child records and atomic transitions.
- `internal/config`: explicit fresh host files and credential references.
- `internal/protocol`: independent v4 DTOs, schemas and interchange fixtures.
- `internal/model`, `internal/runner`: injected provider adapter and ordinary execution loop.
- `internal/runtime`: exclusive ownership, scheduling, cancellation and cleanup.
- `internal/engine`, `internal/engine/quickjs`: guest execution contract and bundled QuickJS implementation.
- `internal/engine/process`: isolated workers, process limits and checkpoint transport; full tests run at the change/CI boundary.
- `internal/rpc`, `internal/client`: v4 transport mapping and initial Go client.
- `cmd/whip-contract`: deterministic v4 generation and fixture validation.
- `cmd/whip-runtime`: explicit-directory new runtime entry point.

All tests in these packages are active. Add new domain/store/runtime packages as
they land. Remove old packages only when their retained guarantees have been
replaced or their retirement is recorded. `go test -run` cannot hide test files
that fail to compile.

The process package's full race suite takes about 85 seconds locally, exercising
resource exhaustion, cancellation and worker lifecycle. It remains required by
`check:change`, `check:phase` and CI. `check:fast` checks its formatting and build
without running this subprocess stress suite on each local edit.

The phase gate separately names four existing daemon regressions: admission
before provider construction, queued child input across restart, settlement
failure without provider replay, and accounting-lock ordering. They are temporary
reference checks, not a requirement to preserve actors or child-specific APIs.

Both TypeScript contract packages, both SDKs and their examples are active.
The v4 fixture uses Unix sockets; the retained legacy fixture uses Unix sockets
and WebSocket. Full web/desktop/mobile/TUI/ACP suites
remain milestone obligations in phases 5–6. A shared contract change must expand
checks to every client already ported to that contract.

## New v4 disposable fixture

[v4-fixture.test.mjs](../scripts/redesign/v4-fixture.test.mjs) builds
`cmd/whip-runtime`, uses a private `/tmp/whip-v4-*` directory, and runs the real
scheduler/runner/store/RPC through `@whip/sdk`. `WHIP_SDK_RACE=1` instruments the
Go binary; CI sets it. It does not need provider credentials or touch an installed
daemon. The runnable SDK example is also executed against this process.

The fixture drops a committed submission acknowledgement through a socket proxy,
checks identity reuse/conflict, concurrent submissions, aborted observation,
uniform root/child execution, and then kills an active process. Restart preserves
completed history, interrupts its claimed turn, retains cancelled input, and
executes queued input exactly once. A mismatched runtime identity fails attachment.
Failures retain database/config, bounded process output and observations in
`test-results/redesign/`; successful runs remove their temporary directory.

For manual v4 work, run `task dev:v4 -- -directory /tmp/whip-example/state`, then
the [SDK example](../packages/sdk/examples/session.mjs) using its printed socket.
The chosen directory is retained for inspection and later reopening. Stop with
SIGINT/SIGTERM. Phase 2's provider acknowledges text; engine/tool execution is
added in Phase 3.

## Retained legacy disposable fixture

The existing SDK fixture starts a real daemon and SQLite database in a temporary
home. `WHIP_SDK_AGENTS_FIXTURE=1` selects its actual agent loop and Starlark runtime
behind a scripted local HTTP provider. It needs no provider credentials and never
attaches to the installed daemon.

`dev:redesign` reuses the existing manual fixture launcher. It prints the isolated
host URL and working directory, stops on SIGINT/SIGTERM, and has a bounded
lifetime of 1–30 minutes. The fixture's frontend directory starts empty. For app
work, run `WHIP_WEB_DAEMON=<printed server URL> npm run dev:web` in another terminal;
do not point the development app at the normal installed daemon for this work.

Ordinary text receives `ack: <text>`. A prompt with a fenced `cell` block executes
that Starlark cell; a fenced `final` block selects the scripted final answer.
Permission and external-effect rules still apply. This is test input, not a
production provider API. The existing fixture's `/control/release` controls its
fake-runner mode; it is not a provider barrier in real-runtime mode.

The [acceptance scenario](../scripts/redesign/fixture.test.mjs) verifies:

1. Fresh isolated storage and protocol attachment over both transports.
2. Accepted input completes after the submitting client detaches.
3. Reusing a command identity returns the same admission and transcript.
4. The provider accounting proves the actual model loop ran once.
5. SIGKILL/restart preserves runtime identity, outcome, history and deduplication.
6. Successful shutdown removes the temporary fixture.

Failures retain database/WAL, bridge metadata, observations and daemon output
under `test-results/redesign/`, with the original temporary path printed too.
CI uploads that directory on failure. Fixture-startup failures also retain their
original temporary directory through the existing SDK helper.

This retained scenario is not evidence for the new core. The v4 fixture above
now establishes lost-acknowledgement and queued-input crash behavior; mid-effect
recovery remains a Phase 3 obligation.

## CI

[backend-redesign.yml](../.github/workflows/backend-redesign.yml) runs on PRs into
the integration branch and pushes to it. Linux and macOS run the phase gate with
a race-instrumented fixture; a separate job runs pinned lint/vulnerability tools.
Their Go compiler caches have separate keys from each other and production CI,
so the first successful run can save the tools and race builds it actually uses.
The `redesign` aggregate requires every job to succeed, including after failure
or cancellation. It has no percentage-coverage gate. The existing production CI
and coverage floor remain intact.

Lint uses the repository's existing configuration and reports findings on code
changed since a frozen baseline revision. That baseline does not advance as
phases land. Five inherited findings are recorded below rather than suppressed
in source.
Those files now live in internal/legacy/session, outside the new active core.
Vulnerability checks have no baseline exclusion. Newly added packages have no
baseline code to exclude.

The active [integration-branch ruleset](https://github.com/context-labs/whip/rules/24090266)
requires the `redesign` context from GitHub Actions, with an up-to-date base. Its
only target is `refs/heads/codex/backend-redesign`. This is a repository setting
separate from the workflow file; its effective branch rules were read back after
creation. Remote failure/success evidence is recorded below.

## Baseline and phase 0 evidence

Baseline: `e3fed9c91918d9c36766dd47d878c1b5466238d1`, captured 2026-09-27
on macOS arm64 with Go 1.27.0, Node 24.14.1 and Task 3.48.0.

| Baseline check | Result |
| --- | --- |
| `go build ./...` | Passed, 7.90 seconds |
| `go test -count=1 -timeout=10m ./internal/session ./internal/agent ./internal/agentdef ./internal/protocol ./internal/llm ./internal/rlm ./internal/daemon` | Six package suites passed; daemon failed; 68.68 seconds total |
| `npm ci --no-audit --no-fund` | Passed; lockfile unchanged |
| `npm run check` | Passed; protocol drift/interoperability, 466 SDK tests, client example build |

The baseline daemon failure is
`TestResumeActiveIsolatesFailuresAndAllowsRetry`, at `daemon_test.go:66`:
`failed restore changed saved session: <nil>`. It occurred before production
source changes. Its restore/configuration guarantee is assigned to phase 3;
this phase does not mark the full daemon suite green or change that assertion.
No full browser, mobile, desktop, TUI, signing or release validation was performed
for this baseline.

The initial full active-package lint run found three `nilnil` findings in
`internal/session/title.go` (lines 69, 77, 86) and two `perfsprint` findings in
`internal/session/title_admission_test.go` (lines 254, 284). These belong to the
phase 1 storage/title replacement or retirement. Phase 0 changes no production
Go code and does not change their semantics merely to clear the lint baseline.

| Phase 0 check | Result |
| --- | --- |
| `task check:fast` | Passed; approximately 10 seconds initially, 0.52 seconds through the hook with warm test caches |
| `task check:phase` (includes `check:change`) | Passed; 170.04 seconds, including race checks, SDK/examples and real-runtime crash/restart |
| `WHIP_SDK_RACE=1 task check:fixture` | Passed; 10.68 seconds, including SDK build and the race-instrumented daemon |
| `task check:analysis` | Passed; 4.46 seconds with warm tool caches, no new lint findings or reachable vulnerabilities |
| Deliberate failing test in `internal/content` | `check:fast` failed with the expected marker in 0.71 seconds; test removed and branch hook passed |
| Manual fixture launcher, `--minutes=1` | Started, created SQLite, exited successfully on SIGTERM and removed its temporary directory; 1.67 seconds |
| Initial fixture assertion failure | Retained database, observations and daemon output; correcting the accounting check (unknown cost is independent of reported/estimated usage) produced a passing scenario |
| Workflow and patch validation | `actionlint` and `git diff --check` passed |

Logs and timings are under the ignored local `test-results/redesign/` directory.
The existing daemon restore failure remains outside the initial active gate and
is tracked above. These measurements do not include cold dependency downloads.
Required-check enforcement, failure propagation and the restored hosted phase
run are verified below.

The [remote canary run](https://github.com/context-labs/whip/actions/runs/36364503714)
deliberately introduced `TestRedesignGateCanary`. The test failed in both OS jobs,
analysis passed, and the required `redesign` aggregate failed. `gh pr checks 197
--required` reported that failed context. The temporary test was then removed.

The [first restored run](https://github.com/context-labs/whip/actions/runs/36364784940)
exposed a too-small timeout: the complete storage race suite exceeded three
minutes on both hosted OS runners, while advancing through different tests in
SQLite. It had passed locally in 135 seconds. The race step now allows ten
minutes per package, still under the job's twenty-minute limit. No tests or race
instrumentation were dropped; the fast gate is unchanged. Hosted timing must be
measured separately from the local warm measurements above.

The [restored hosted run](https://github.com/context-labs/whip/actions/runs/36365373490)
passed on revision `faecb7913994360f4254a924fb41a1249ff179b2`: Linux, macOS,
analysis and the required `redesign` aggregate all succeeded. The phase command
took 424 seconds on Linux and 359 seconds on macOS. Including setup and cache
upload, the jobs took 538 and 554 seconds respectively; analysis took 189 seconds.
These are first-run measurements for the separate cache keys, not claimed warm
CI timings. The hosted gate includes the race-instrumented SDK fixture.

Phase 0 is complete in [PR #197](https://github.com/context-labs/whip/pull/197).
No replacement runtime or schema was implemented by Phase 0.

## Phase 1 behavior ownership and evidence

The old session/config/protocol packages and v3 TypeScript contract were moved
to explicit legacy locations without aliases. The retained runtime and clients
still build against them. New core import tests prohibit reaching that runtime,
its managers, or legacy packages.

| Former active area | Phase 1 replacement or later obligation |
| --- | --- |
| Root and child storage, input acceptance, transcript commits | New store constraint, rollback, retry, root/child history and restart tests |
| Agent definition defaults and validation | New session canonical revisions, local schema validation, deep ownership and clear/inherit tests |
| v3 schema generator | New v4 generator and Go/TypeScript interchange; v3 checks remain for unported clients |
| Actor restore/title generation | Old actor behavior is retired; title metadata is revisioned here, model helper accounting and restore behavior belong to Phase 3 |
| Legacy child budgets, mail, grants and shared state | Phase 4 acceptance; no claim that Phase 1 replaces those guarantees |
| Checkpoints, process/workspace resources and broader client behavior | Phases 3–6; retained fixture and selected daemon regressions still run |

The new SQLite tests use disposable files and independent connections. They
inject failed receipt, transcript, configuration and deletion writes, check
rollback, then retry successfully. They cover foreign-schema rejection without
modification, concurrent initialization, topology/active-turn constraints,
configuration capture, bounded pages, stop/cancel/recovery and deletion receipts.
Host tests cover atomic concurrent initialization, strict files, environment
references and default changes that leave stored sessions intact. Contract
fixtures exercise actual Go JSON and TypeScript re-encoding with counters above
2^53, explicit clearing and invalid payloads; standalone validators run under
Node's prohibition on dynamic code generation.

Local validation on macOS arm64, Go 1.27.0 and Node 24.14.1:

| Check | Result |
| --- | --- |
| Whole retained/new backend build | go build ./... passed |
| Warm fast gate | Passed, 0.30 seconds |
| Full phase gate | Passed, 35.58 seconds; active race/shuffle tests, both contracts, 466 SDK tests, examples, real-runtime restart fixture and four named daemon regressions |
| Analysis | No lint findings or reachable vulnerabilities in the active scope |
| Concurrent fresh initialization | 100 repeated runs under the race detector passed |
| Generated v4 contract | TypeScript compilation, three CSP-safe interop tests, Go fixture validation and deterministic drift passed |
| Patch/workflow validation | git diff --check and actionlint passed |

The first complete run found intermittent SQLITE_BUSY during concurrent
initialization. WAL setup now retries only its idempotent PRAGMA within a bounded,
context-cancellable wait. A held-reader test proves timeout and later success;
admission/claim/finish transactions are never automatically replayed.

| Phase 1 acceptance | Evidence |
| --- | --- |
| Fresh identity/config, no legacy readers | TestFreshIdentityAndForeignSchema, TestConcurrentInitialization, TestExplicitFreshHostAndAtomicInitialization, TestCoreImportBoundaries |
| Root uniqueness, same-tree parents, no cycles, one active turn | TestTopologyAndRevisionConstraints, TestIndependentConnectionsSerializeClaimsAndDuplicateAdmission |
| Uniform root/child persistence and history | TestConfigurationCaptureAndUniformHistory |
| Pinned definitions and independent values | TestDefinitionRevisionAndResolutionIsolation, TestDefinitionCanonicalSchemaOrdering, TestDefinitionAndHostEditsDoNotChangeRetainedSessions |
| Single authorities and captured configuration, no stored credentials | schema.sql, TestConfigurationCaptureAndUniformHistory, TestCredentialsRemainHostReferences, backend-domain.md ownership table |
| Atomic admission/claims and rollback, database-only construction | TestAtomicAdmissionClaimFinishAndRetry, TestTreeConfigurationAndDeletionRollback, TestCoreImportBoundaries |
| Retry/interruption/cancellation/deletion semantics | TestStopCancelRecoveryAndDeletion, TestClosedStoreRecoveryRetainsQueuedWork and backend-domain.md transitions |

The new persistence contract is ready for the Phase 2 runner/RPC/SDK slice.
No claim is made that the existing SDK or applications already use this store.
The [hosted Phase 1 run](https://github.com/context-labs/whip/actions/runs/36368729721)
passed the phase gate on revision 76505c4ca7ef60ba324f4b75d3184351a8cb47de:
167 seconds on Linux and 202 seconds on macOS. Analysis passed too. These are
hosted gate measurements with restored dependency caches and newly compiled
relocated packages, not the local warm measurements above.

Phase 1 is complete in [PR #199](https://github.com/context-labs/whip/pull/199),
stacked on Phase 0's PR #197. The final documentation commit is checked by the
same workflow; the PR records the current head's result.

## Phase 2 behavior ownership and evidence

The new runtime/runner/RPC/Go client and `@whip/sdk` use v4 directly. Retained
applications import `@whip/legacy-sdk` explicitly; no compatibility adapter sends
new execution through the old daemon. Domain message drafts moved out of store
so the runner's transcript interface depends only on domain values.

| Phase 2 acceptance | Replacement evidence |
| --- | --- |
| Create, submit, observe and read a complete turn | `packages/sdk/examples/session.mjs`, executed by the v4 process fixture; Go RPC acceptance |
| Lost acknowledgement, stable admission, payload conflicts | Socket proxy drops a committed response in `v4-fixture.test.mjs`; retry returns the same input ID and changed input fails with `CONFLICT` |
| One active turn per session | Runtime barrier test permits separate sessions concurrently and keeps a second input queued; SQLite claim invariant; concurrent SDK submissions |
| Reconnect without duplicate transcript | Process fixture compares committed messages before and after SIGKILL and repeats the original identity |
| Queued input survives, history starts no work | Runtime reopen/read test and RPC pre-Start history test; process kill while a second input remains queued |
| Observer cancellation is separate from execution | Runtime request-context cancellation, Go socket disconnect and SDK aborted-wait tests |
| Generated Go/TypeScript agreement | Actual Go fixture JSON, strict standalone validators, decimal counters, initialization and exclusive result/error envelopes |
| Required gate | `check:change` includes new packages, both SDKs, generated contracts and the v4 process fixture |

Additional tests cover exclusive directory ownership, duplicate listener refusal,
context-aware shutdown, interrupted claimed work without replay, queued deletion
without unrelated runtime failure, stable completed-output write retries and
bounded external error text. Import checks keep provider/runner code outside
runtime/storage and Go clients outside all execution packages.

Local validation passed on 2026-09-27: `task check:phase`, targeted race/shuffle
checks after lint fixes, the v4 process fixture with `WHIP_SDK_RACE=1`, contract
generation/interchange, and `task check:analysis` (zero new lint issues; no
reachable vulnerabilities). The retained SDK's 466 tests and its original
process fixture still pass. Hosted Linux/macOS validation runs on [Phase 2 PR #200](https://github.com/context-labs/whip/pull/200), stacked on Phase 1 PR #199.

Phase 2 intentionally does not claim engine execution, real-provider integration,
model accounting, effect permissions, checkpointing, or product UI adoption.
The first five belong to Phase 3; uniform recursion and retained integrations
follow in Phases 4–5. The overall execution objective remains through Phase 5.


Hosted Phase 2 validation completed successfully on final revision
`0da66231c1af5a7c7e320900e28666223d148672` in
[run 36371312906](https://github.com/context-labs/whip/actions/runs/36371312906):
Linux, macOS, analysis and the required aggregate all passed. Phase 2 is complete.

## Phase 3 progress: model-attempt ledger

The first Phase 3 increment isolates the existing independent engine package at
`internal/engine` and introduces model-attempt admission, exclusive dispatch,
atomic outcome/message settlement, and restart uncertainty. The runner now uses
this ledger; the scripted provider exercises the same accounting boundary as
future real providers. `turns.attempts` exposes bounded durable inspection through
both clients and generated v4 validators.

Storage version 2 adds the ledger to fresh databases. Earlier disposable redesign
schema versions are rejected without mutation; this is not an old-data importer.
Usage count presence remains explicit. Price snapshots use nano-USD per million
tokens; resulting cost uses nano-USD with one final upward rounding. Missing usage
or rates produce unknown cost unless the evidence proves the cost (including an
explicitly free route). Overflow preserves the response and usage with unknown
cost and a diagnostic note. Credentials never enter dispatch snapshots.

Reserved attempts may become cancelled with known zero cost because they were
not dispatched. Dispatched attempts settle with success/failure evidence or
uncertainty; cancellation and crash cannot relabel them as never dispatched.
Recovery updates attempts and turns in one transaction, processing bounded
batches. A turn cannot finish while its attempts remain unsettled. A committed
model outcome names its exact transcript message; retries cannot substitute a
new message or change the saved outcome.

The engine extraction preserved the bundled WASM and JavaScript bytes and passed
engine/RLM race tests before and after, repository build, and scoped vet. New
ledger tests exercise independent SQLite connections, injected transaction
failures, cancellation races, unknown/free/overflow costs and recovery. The real
process fixture now observes a dispatched attempt before SIGKILL and checks the
resulting uncertainty through the SDK. Phase 3 remains incomplete: the real
provider, Starlark loop, effects, content authorization, checkpoints, and their
remaining acceptance checks are still being implemented.

This increment passed `task check:phase`, targeted race checks and the v4 process
fixture with `WHIP_SDK_RACE=1`. `task check:analysis` reported zero new lint issues
and no reachable vulnerabilities. These are local results, not a claim of
completed Phase 3 acceptance or hosted validation for this increment.

### OpenAI-compatible dispatch increment

`whip-runtime -directory <private-directory>` now uses configured HTTP providers.
`-scripted` remains an explicit fixture option. The command composes the provider
with the runtime; scheduling does not import provider implementations or resolve
credentials. HTTP requests, including retryable rejections, pass through the same
attempt ledger as scripted execution.

Edit the initialized `host.json` provider map with an API base URL and an optional
credential environment reference. For example, these provider entries can be
added while preserving the other initialized host fields:

```json
{
  "example": {
    "kind": "openai-chat",
    "base_url": "https://provider.example/v1",
    "credential_env": "WHIP_PROVIDER_KEY",
    "models": {
      "your-model": {
        "max_output_tokens": 4096,
        "timeout_millis": 120000,
        "max_attempts": 3
      }
    }
  }
}
```

Select `{ "provider": "example", "name": "your-model", "effort": "" }` in a
session override or host defaults. Missing model settings use the limits above;
missing prices remain unknown. Optional `prices` fields use the ledger's
nano-USD-per-million-token units, with independent input/output/reasoning/cache
rates. Provider-reported USD charges are converted exactly to nano-USD, rounding
up once. No price is inferred from a model name.

Routes and credentials refresh when preparing a logical call. Its body, endpoint,
limits and price snapshot remain fixed for its retries. The adapter performs one
request; the runner permits at most five attempts, with cancellation-aware
backoff, only following explicit retryable HTTP responses. Transport uncertainty,
invalid completion bodies and persistence failures do not cause automatic
redispatch. Redirects are rejected to preserve the recorded route and credential
scope. Responses and encoded requests have explicit size limits.

Local race tests cover HTTP retry accounting, unknown transport outcomes, slow
provider cancellation, exact decimal charges, missing/malformed accounting,
immutable prepared bodies and route refresh. The SDK process fixture also runs
the command without `-scripted` against a local HTTP provider, observes a 429 and
successful retry as distinct attempts, and verifies one committed response.
This local fixture is not the required live-provider/engine smoke; that remains
part of Phase 3 acceptance.

Hosted validation passed for provider revision
`acd97035f4fa21e1ed2efdb234def9f116ed239f` in
[run 36373024664](https://github.com/context-labs/whip/actions/runs/36373024664):
Linux, macOS, analysis and the required aggregate all succeeded. The preceding
ledger revision `952546ee7e485da94261f7c164f0bbaac7a6f74b` passed
[run 36372449220](https://github.com/context-labs/whip/actions/runs/36372449220).

### Authorized content increment

Fresh schema version 3 adds content-body metadata and session-scoped references.
The runtime publishes immutable files before committing references, validates
references inside input/message transactions, and hydrates bounded verified
bytes for provider encoding. Startup collection removes unreferenced files while
preserving shared bodies. The exact ownership, quotas and collection boundary
are documented in [the domain contract](backend-domain.md#content-boundary).

The v4 SDK fixture now uploads an image, retries its upload identity, reads it,
rejects access/admission from another session, and sends it through the actual
HTTP adapter. It checks that history retains the reference and the provider sees
the image payload. Local race checks additionally cover SQL rollback, shared
quotas across independent connections, corrupt bodies rejected before dispatch,
complete reads beyond 64 KiB, special-file rejection and startup orphan cleanup.
The real-provider/engine smoke and other Phase 3 acceptance remain pending.

The combined content and subprocess-extraction change passed `task check:phase`
and `task check:analysis` locally. The phase gate includes both SDK process
fixtures, generated contract checks, race/shuffle coverage for the new core and
isolated engine, and the four retained admission/accounting regressions. Static
analysis reported zero issues and no reachable vulnerabilities. The v4 fixture
also passed separately with `WHIP_SDK_RACE=1`.
