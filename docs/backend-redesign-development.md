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
- `internal/instruction`, `internal/skills`: confined instruction-source capture and retained skill metadata parsing; full suites run in the active gate.
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
scheduler/runner/store/RPC through `@whip/sdk`. It explicitly builds the production
binary with `-race=false`, including its memory-limited engine subprocesses.
Required Go package suites separately exercise backend and engine code with
`-race`; the runtime integration suite combines a race-instrumented host with
production workers. `WHIP_SDK_RACE` affects only the retained legacy fixture.
No test contacts a real provider or touches an installed daemon. Synthetic
private subscription credentials are confined to its disposable directory, with
a rejecting proxy guarding the no-dispatch scenarios. The runnable SDK example
is also executed against this process.

The fixture drops a committed submission acknowledgement through a socket proxy,
checks identity reuse/conflict, concurrent submissions, aborted observation,
uniform root/child execution, and then kills an active process. Restart preserves
completed history, interrupts its claimed turn, retains cancelled input, and
executes queued input exactly once. A mismatched runtime identity fails attachment.
Compilation, startup and each acceptance scenario have separate named deadlines;
adding an independent scenario does not consume another scenario's time budget.
Progress records identify the active scenario and elapsed times. A timeout aborts
socket requests/observers, prevents another runtime start and performs bounded
shutdown. Failures retain database/config, progress, bounded process output and
observations in `test-results/redesign/`; successful runs remove their temporary
directory. The workflow still has an overall job deadline.

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
required Go race suites and production-binary SDK acceptance; a separate job
runs pinned lint/vulnerability tools.
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
        "context_window_tokens": 128000,
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
up once. No price is inferred from a model name. `context_window_tokens` is
optional: configure the actual provider maximum for that model rather than
copying this illustrative value. It provides a conservative input reservation
bound for finite budgets; absent limits/prices remain unknown. Actual provider
overages are recorded, never clamped to the host declaration.

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


### Durable code loop increment

Fresh schema version 4 adds cells bound to committed assistant calls. The runner
now supports typed tool calls/results and the same model-attempt path for every
iteration. Both engines execute through `internal/engine/process`; the runtime
stages immutable checkpoint bodies and atomically commits the result/boundary.
The exact ownership and unavailable-checkpoint policy are documented in the
[domain contract](backend-domain.md#code-execution-and-checkpoint-boundary).

Targeted race tests cover one cell dispatch across independent SQL connections,
rollback of result/checkpoint settlement, recovery of interrupted and undispatched
calls, input spoof rejection, partial Starlark restoration, corrupt/incompatible
images, failed publication, subtree kernel cleanup, and state across model
changes, eviction and restart. A correlated engine result is explicitly distinct
from an unsettled transport result, including a late successful QuickJS host call
after cancellation. Review found and fixed dropped restore reports and retained
kernel handles after deletion.

The v4 SDK fixture passed with `WHIP_SDK_RACE=1` for both engines. It uses the
packaged command and HTTP adapter, kills the runtime after a cell commit while a
subsequent provider request is pending, and verifies the result/history/checkpoint
survive with the pending request marked uncertain. Restarted execution reads the
saved variable without rerunning the completed cell.

The required live-provider/engine smoke passed at **2026-09-28 03:42:47 UTC** using
OpenRouter's `openai/gpt-4.1-mini` and Starlark through the new command/socket/SDK.
One call executed `6 * 7` in the REPL; the next consumed its result and answered
`42`. Both attempts succeeded: usage was 192/28 and 272/3 input/output tokens,
with provider-reported costs of 121600 and 113600 nano-USD. Credentials remained
environment references. A preceding OpenAI-route attempt returned HTTP 429 and
was recorded as failed with unknown usage/cost; it did not execute a cell.
The disposable successful runtime was removed after verification; the full
local evidence was saved to `/tmp/whip-redesign-live-evidence.json`.

Hosted content/extraction revision `2c46110cd` passed Linux, macOS, analysis and
the required aggregate in
[run 36374104894](https://github.com/context-labs/whip/actions/runs/36374104894).
This is preceding-revision evidence, not hosted validation of the code-loop slice.
Scoped host effects/permissions and provisional output remain Phase 3 work;
Phase 4 recursion policies and Phase 5 integrations remain incomplete.


The combined code-loop increment passed `task check:phase` and
`task check:analysis` locally, with zero lint issues and no reachable
vulnerabilities. The phase gate includes full isolated-process stress/race tests,
new runtime/store race checks, both SDK process fixtures, generated contracts and
the retained admission/accounting regressions. Targeted deletion cleanup checks
also passed after the final mechanical map-clone adjustment.


### Scoped host-operation increment

Fresh schema version 5 adds operations, grants and permissions, with ownership
and transition rules in the [domain contract](backend-domain.md#host-operations-and-permission-decisions).
The independent filesystem adapter supports confined read/write/patch through
rooted handles and shared mutation locks. The dispatcher records immutable intent,
waits for scoped consent, rechecks revocation after resource acquisition, commits
dispatch, executes once, and retries only outcome persistence. The SDK exposes
permissions, grants, operation evidence and cell boundaries through generated v4
contracts; no hand-maintained second schema was added.

Targeted race tests pass for two-connection dispatch and consent/revocation/stop/
recovery races, transactional rollback, scope isolation, quota/page bounds,
one-use cascade deletion, cancellation while awaiting consent, revocation while
waiting for a path lock, and a failed SQL settlement after a completed write.
That last test changes the file externally during settlement retries and verifies
that persistence recovery never overwrites it by repeating the effect.

The actual command/socket/SDK fixture passed with `WHIP_SDK_RACE=1` for both
engines. In one cell it approves a first write, observes its committed success,
waits on a second permission, then sends SIGKILL. Restart preserves the first
operation unchanged, cancels the second without dispatch, records the lost cell
as uncertain without a checkpoint, rejects late approval and permits text-only
history inspection. An external file change survives restart and the unapproved
file is never created. This distinguishes effect evidence from interpreter state.
`task check:phase` and `task check:analysis` pass for this increment, including
full process stress/race coverage, both SDK fixtures and generated contract checks.
Analysis reports zero issues and no reachable vulnerabilities. Hosted validation
of this increment remains pending.


The preceding code-loop revision `bb1acc960` passed Linux, macOS, analysis and the
required aggregate in [run 36375596169](https://github.com/context-labs/whip/actions/runs/36375596169).
Its initial Linux run exposed a five-second test deadline shorter than cold
QuickJS startup under concurrent race testing. The fix raises only real-engine
acceptance waits to thirty seconds; ordinary scripted waits remain unchanged.
This is preceding-revision evidence, not hosted validation of the operation slice.
Review also found a transient operation-read failure could strand an admitted
permission; the dispatcher now cancels that waiter, and a fault-injection test
proves cell and turn settlement can proceed without an effect.


### Provisional observation increment

The OpenAI-compatible adapter now requests bounded SSE with usage and supports
validated JSON fallback. Provider assembly is separate from a capped runtime
preview; only a complete validated response enters durable history. The runner
keeps previews through SQL settlement retries. `sessions.observe` and the SDK's
async iterator reconcile previews by eventual message identity and process epoch.
See the [observation contract](backend-domain.md#provisional-output-and-observation).
Generated message unions now carry their shared required fields in every variant,
fixing TypeScript's loss of typed IDs/cursors without maintaining separate types.

Model tests cover framed/fragmented streams, complete markers, usage snapshots,
known accounting on later failure, cancellation, limits and JSON fallback. Runtime
race tests cover committed replacement, cursor advancement, isolation, stale
callbacks, UTF-8 bounds, observer cancellation, failed/restarted streams and actual
SQL rollback with preview retention. The race-enabled command/socket/SDK fixture
observes partial output, verifies exactly one committed replacement, sends SIGKILL
during another stream and verifies epoch change/uncertain attempt/no partial
message, and exercises both observer abort and explicit turn cancellation.

A direct streaming smoke passed at **2026-09-28 04:13:39 UTC** through OpenRouter
`openai/gpt-4.1-mini` and Starlark: four preview snapshots, one successful cell,
answer `42`, and two succeeded attempts with 269/28 and 349/3 input/output tokens.
Provider-reported costs were 152400 and 144400 nano-USD. Evidence is saved locally
at `/tmp/whip-redesign-stream-live-evidence.json`; the successful runtime was removed.
The first streaming smoke detected OpenRouter's additional content-free choice in
its usage footer. That failed attempt preserved usage/cost as uncertain and ran
no code. A fresh ledger-recorded diagnostic through a local relay captured the
shape; a focused regression now accepts that
[documented accounting footer](https://openrouter.ai/docs/api_reference/streaming#the-final-usage-chunk-chat-completions),
while rejecting new post-completion content and still requiring `[DONE]`.
Failure evidence remains in `/tmp/whip-redesign-stream-live-failure.json` and
`/tmp/whip-redesign-stream-capture-evidence.json`; captured response data is in
`/tmp/whip-stream-capture.sse`. No credential headers were captured.

Hosted operation revision `852ee6570` failed both platforms only when the test's
independent SQL connection tried to drop its injected trigger while settlement
held the database lock. Revision `ed7fba842` gives that test connection a bounded
SQLite busy timeout. Twenty-five targeted race repetitions pass; production
transaction semantics are unchanged. The complete preview increment passes
`task check:phase` and `task check:analysis`: isolated-process stress/race tests,
active core checks, SDK fixtures, generated Go/TypeScript interchange, retained
regressions, zero lint issues and no reachable vulnerabilities. Final hosted
validation remains pending.


## Phase 3 final hosted evidence

Revision `3f0b883a8838a89098c6659258065a1d449d8a5c` passed the complete
[hosted run](https://github.com/context-labs/whip/actions/runs/36377036391):
Linux/macOS active scope, process/SDK fixtures, analysis and aggregate gate.
Phase 3 is complete in [PR #201](https://github.com/context-labs/whip/pull/201).

## Phase 4 atomic child admission and boundary waits

The first increment adds one transaction for child identity/configuration,
scoped content reference copying, grant delegation, initial ordinary input and
its stable request receipt. The production identity-only child creation API is
removed. SQL-native `agents.spawn` also includes operation success in that
transaction; injected failure of the final settlement leaves no child or input.
Grant issuer chains enforce direct-parent exact scopes at every dispatch and
propagate revocation to ready descendants without rewriting dispatched effects.

The explicit new `agents.wait_after_cell` registers descendant inputs to await
at a committed cell boundary. It replaces same-cell blocking joins; no suspended
continuation is reconstructed. The bounded runtime distinguishes active ownership
from runnable permits and alternates FIFO resumptions with fresh queued work.
The existing engine manager can evict an idle parent after its checkpoint commits.

Evidence includes store concurrent retries across two SQL handles, rollback,
restart/deletion receipts, content isolation and delegated-authority tests;
Go/TypeScript interchange and the SDK fixture exercise the new spawn contract.
`TestRecursiveWaitReleasesWorkerAndKernelAtCommittedCell` passes under `-race` for
both engines: root/child/grandchild run with one worker and one kernel slot,
parent history and checkpoint precede the wait, and parent globals survive
recursive eviction. `TestChildAdmissionSurvivesRestartWithoutLoadedWorker`
checks queued child execution after a fresh runtime opens the database.

This increment does not complete Phase 4. Model budgets follow below; mail,
private/shared state, resource limits, retry/report policies and subtree controls
remain required.
The user extended execution scope to all phases; full client cutover and final
retired-core deletion remain subsequent gates.

Local gate evidence for this increment: `task check:phase` passed, including the
full process-engine race suite (97.972 s), active packages, generated contract
checks, both SDK fixtures and retained cross-layer regressions. After final
review fixes, affected runtime/store/RPC/tool race suites passed again and
`task check:analysis` reported zero lint issues and no reachable vulnerabilities.
The separate `WHIP_SDK_RACE=1` new-runtime fixture passed in 18.181 s.


## Phase 4 model budgets increment

Model budgets share the canonical attempt ledger. Fresh schema 7 adds revisioned
local limits and immutable attempt-to-ancestor associations; request snapshots
already own reservation bounds, so no extra mutable accounting counters or
reservation-value copies are needed. Child deletion retains its attempts and
charges; whole-tree deletion explicitly clears them. Ancestors without finite
limits do not scan historical usage during each reservation.

The generated contract/SDK expose budget inspection, revision-checked cap updates,
optional child limits, input reservation bounds and measured execution duration.
The SDK subprocess fixture proves shared parent/child allowance, deletion without
replenishment, stale cap conflicts and denial before another provider attempt.
`TestAncestorModelCallBudgetDenialLeavesRuntimeAvailable` checks the same isolation
across an unrelated tree. Config/model/runner tests cover immutable host bounds,
truthful usage overages, missing evidence and execution duration excluding SQL
settlement retries. Resource-count/byte/depth limits and the remaining Phase 4
coordination policies remain distinct subsequent work.

Budget validation: `task check:fast`, active build/vet and `task check:analysis`
passed (zero lint issues and no reachable vulnerabilities). Final session/store/
runtime/RPC/protocol/command race suites passed; store took 17.684 s and runtime
39.382 s. Generated protocol and SDK checks passed, and the race-enabled SDK
fixture passed in 19.073 s. Projection regressions cover partial usage beyond
reserved bounds, dispatched-call accounting, unknown evidence and saturation.

The preceding child/wait increment at `70bce08a7` passed the complete
[Linux/macOS hosted gate](https://github.com/context-labs/whip/actions/runs/36378062536).

## Phase 4 mail increment

Fresh schema 8 stores mail identities/retry tombstones, immutable revisions and
turn presentation receipts. Mail-driven turns use the existing scheduler, runner,
accounting and transcript; no synthetic input or second work queue is added.
Client inspection remains read-only. Agent helpers atomically commit authority,
mail mutations or observations, and their operation outcomes. A read operation
retains its revision metadata and reconstructs its body from canonical mail.

New store tests exercise concurrent identical sends across two connections,
partial-admission rollback, source-backed history across replacement, atomic
successful-turn delivery, stale batch handling, operation settlement rollback,
read retry after replacement, deferral and sender/recipient deletion. The failure
barrier survives reopen at both root and child depth. Both engine integration
tests exercise actual mail helpers, metadata-only listing versus presentation,
mail-only turns, steer injection after the complete tool batch, and failure/
cancellation/interruption recovery. Future due mail wakes through ordinary
scheduler reconciliation without another notification.

The generated Go/TypeScript contract and SDK checks pass. The race-enabled SDK
process fixture passed in 19.477 s, including lost mail acknowledgement, exact
retry, read-only inspection, immutable transcript provenance and deletion
without resurrection. Focused mail/cancellation race suites pass (store 4.044 s,
runtime 16.180 s); pinned analysis reports zero lint issues and no reachable
vulnerabilities. The full `task check:phase` passed: active build/vet/race,
complete process-engine suite, generated contract/SDK checks, new and retained
process fixtures, and required legacy cross-layer regressions. Runtime race
coverage took 51.668 s; retained cross-layer tests took 2.709 s.

The budget head `a9f05dc40` passed macOS and analysis, but Linux exposed an overly
strict cancellation assertion: a cancelled host wait can return a correlated
language-error result before process cancellation wins. That settled boundary
may retain its exact checkpoint. Revision `e5f5d215d` checks both permitted
outcomes, verifies any retained checkpoint through the real restore loader,
and still rejects checkpoints on uncertain cells, late approval and filesystem
effects. Ten repeated race runs across both engines pass in 51.004 s. This changes
only the test; production execution and checkpoint semantics are unchanged.

The mail head `545bddca8` passed the complete
[Linux/macOS hosted gate](https://github.com/context-labs/whip/actions/runs/36434576654).

## Phase 4 state increment

Fresh schema 9 introduces one immutable state-version table, with session-private
or tree-shared visibility. Immutable JSON lives in content files; SQL owns version
identity, revision comparison, visibility and retention. There is no second head
or current-value cache. A shared version survives its author's deletion; private
versions follow their owner. Reads verify authorized handles before content
access, and bounded range reads hash the same bytes they return.

The runtime, both REPL engines, generated v4 contract and SDK expose revision-
checked writes/appends and bounded inspection/history. Guest operations commit
permission, mutation and settlement together; stored operation results contain
metadata, never another copy of the state body. Lost acknowledgements return the
original immutable version. Read-only client inspection does not admit turns.

`task check:phase` passed, including active build/vet/race, the complete process
suite (96.578 s), generated contracts, SDK checks, both process fixtures and the
required retained regressions (2.868 s). State runtime coverage includes a real
64 MiB value across restart and deletion/collection; the runtime race suite took
57.053 s. Separate race tests cover concurrent CAS across two database handles,
atomic rollback, exact old-version read retries, scoped access and revoked
authority, plus aggregate private/shared byte and version limits. Both engines
preserve large integers through actual write/append/get/history helpers.

The race-enabled SDK fixture passed in 20.788 s, including a dropped state-write
acknowledgement, stale revisions, immutable old reads, chunked Unicode reads and
restart. Pinned analysis reports zero lint issues and no reachable vulnerabilities.
A ten-second state JSON fuzz run passed. Subscriptions/coalesced notifications,
ancestor resource limits, report/retry policies and remaining subtree lifecycle
acceptance remain open in Phase 4.

## Phase 4 subscription increment

Fresh schema 10 gives mail explicit provenance (`session` sender or `state`
subscription) and retains owner-scoped state subscriptions. Subscription creation
atomically closes the snapshot gap. Shared writes commit values, cursors and
coalesced revisioned notifications together. Own writes only advance cursors;
recipient deferral survives coalescing. Cancellation retains earlier mail as
evidence. The ordinary scheduler discovers due notifications across restart.

Store tests cover initial catch-up, identity/cancel retry, foreign ownership,
restart, author changes, exact presented revisions, notification/operation
settlement rollback, and mailbox-limit backpressure rolling back all cursors and
the value. Both engine tests execute the real subscribe/list/unsubscribe helpers.
The race-enabled SDK process fixture passed in 22.233 s, exercising notification
coalescing across restart, typed provenance, delivery, own-write suppression and
cancellation without resurrection. Targeted store race tests passed in 4.093 s;
pinned analysis reports zero lint issues and no reachable vulnerabilities.

The full local subscription phase gate passed: active build/vet/race, complete
process suite (97.116 s), generated contracts/SDK checks, new and retained process
fixtures, and retained regressions (2.839 s). The runtime race suite took 59.216 s.
A subsequent targeted race check covers a coalesced revision from a different
author while preserving the subscription's source identity.

The preceding state head `33ca2c099` passed hosted analysis but failed both
[platform race jobs](https://github.com/context-labs/whip/actions/runs/36436781996)
at the new QuickJS state test's five-second observer deadline. Existing full-engine
helpers use a thirty-second integration deadline; these two new state helper
tests now use that same bound. Production timeouts and all exact-value, revision,
cell-result and subscription assertions are unchanged.
Five repeated race runs of both state workflows across both engines, using the
hosted shuffle seed, passed in 32.883 s with the corrected integration deadline.
Final pinned analysis again passed with zero issues and no reachable vulnerabilities.


The subscription head `f7cc98c99` passed the complete
[Linux/macOS hosted gate](https://github.com/context-labs/whip/actions/runs/36438104798),
including analysis and the required aggregate check.

## Phase 4 reusable capacity increment

Fresh schema 11 removes the tree's duplicated policy document. Session-scoped,
revisioned resource limits govern depth, retained descendants, queued inputs,
unsettled host operations and active state subscriptions through every ancestor.
Usage is derived from their canonical owning rows. Host configuration version 2
resolves finite defaults once into new root records. The generated wire contract,
SDK and both guest engines carry explicit child caps; inspection returns every
enforcing scope. Stale edits conflict, narrowing below usage fails, and admission
and policy edits serialize in the same immediate transaction.

Queues now share ancestor capacity across siblings. Stop and worker eviction
retain queued inputs and session identity; claim/cancel and deletion release their
respective capacity. Capacity reuse never erases permanent ancestor model spend.
Both budget and resource null limits consistently remove only the local cap;
ancestor enforcement remains intact. The former test that treated a null child
budget as an explicit widening was replaced by a real reservation that proves
the ancestor still rejects excess exposure.

Ten focused store scenarios cover two-handle sibling contention, limit-update
races, revision checks, multi-level narrowing, initial-input rollback, exhausted
and deleted retries, correct release boundaries, retained model spend, and the
maximum depth/authority chain. The full public deep-chain setup passed under race
instrumentation but took 71 seconds. Its ordinary fixture now builds the first
127 valid edges in one transaction, then uses public admission for the 128th and
rejected 129th edge and public grant creation to validate all 129 owners. This
preserves the boundary assertions without taxing every development iteration.
Admission now loads applicable finite caps together and skips usage aggregation
for inherited scopes. The final focused resource/budget race run passed in 4.050 s.

The race-built SDK process acceptance passed, covering cap rejection, local
inheritance, exact counters beyond JavaScript's safe integer range, restart and
deletion/reuse. Both engine recursion tests execute a leaf with a local depth-zero
cap. Broader byte/record capacity, report/retry policy and final Phase 4 lifecycle
acceptance remain outstanding; Phase 5–7 scope is unchanged.

The full local phase gate passed: active build/vet/race, the complete process
engine suite (97.701 s), generated contract/SDK checks, both process fixtures and
required retained daemon regressions (2.764 s). Store race coverage took 29.204 s
and runtime race coverage 61.783 s. The final helper-instruction/recursive-cap
check passed in 11.280 s. Pinned analysis reports zero lint issues and no reachable
vulnerabilities. An independent review found no correctness blockers; admission
queries may merit profiling with many unrelated trees before scale claims.


## Remaining Phase 4 policy audit

The resource audit distinguishes retained controls from incidental old storage
structure. Legacy `durable_bytes` and `record_count` charged selected successful
logical writes cumulatively: mail, descendant input, content, state, subscriptions
and schedules. They did not count every database row or represent current disk
occupancy; deletion did not refund them. Model accounting explicitly bypassed
these allowances (`internal/legacy/session/model_call_test.go`,
`TestModelAccountingDoesNotConsumeAgentStorageAllowances`). Preserve configurable
ancestor write allowances with that narrow meaning. Do not introduce a generic
quota over all transcript/accounting rows, or allow exhaustion to block recording
completed effects. The existing per-owner retention bounds remain separate.

Subtree concurrency is a distinct retained control: the old regression runs four
host workers with a subtree child-turn allowance of one and keeps the second
child queued. Host-wide worker counts alone do not preserve it. The scoped
permit increment below now covers this with release/reacquisition around parent
waits, rather than counting unfinished turns and deadlocking recursive waits. The new
`active_operations` resource intentionally describes the host-operation domain;
model concurrency needs explicit coverage in those scheduling permits and later
helper-call admission. Schedule/subscription combined admission is revisited
with the Phase 5 schedule implementation.

The guest recursion surface now includes ordinary submit, bounded result
inspection/listing and scoped stop/delete operations, covered below. Completion reporting must
retain idle-parent wakeup and exact result access. The old completion notifier
sent after turn settlement and ignored failures; copying that ordering would
lose reports. Conversely, checking an inbox quota while finishing a completed
effect can prevent terminal settlement. The replacement needs bounded reserved
report delivery ownership and independently recoverable delivery. These are
remaining obligations, not implemented claims.

## Phase 4 scoped execution and guest controls

Fresh schema 12 adds `turn_permits`, one authorization row per executing turn.
`runnable_descendants` counts these rows only for proper descendants, with every
ancestor enforced. Its finite root default is 64. Turn outcome remains in `turns`;
a parent can stay running while it yields execution permission. Claim acquires
before consuming input or writing history. Yield rejects unfinished model, cell
or host work. Resumption uses the same turn and input, while finish/recovery
release permission atomically. New provider, cell and host dispatch boundaries
enforce the permit. No second mutable usage counter or background repair exists.

The runtime owns physical worker slots separately. It reserves a slot before SQL
acquisition without holding its mutex, releases a parent's permit at the committed
cell boundary, and reacquires both before continuing. A bounded in-memory queue
cursor scans past blocked scopes and wraps to earlier work. It is advisory, not
another persisted queue. Resumption cancellation releases any unused acquisition.
Cap changes wake scheduling.

Guest submit, inspect, list, stop and delete use the existing operation transaction.
Submit restricts mutation to direct children and atomically shares authorized
content with the queued input. Inspect returns the exact requested input's outcome
and bounded UTF-8 text pages with stable terminal message identity. List reveals
only relative metadata. Stop retains queued work and marks active turns cancelling;
its postcommit cleanup cannot cancel a later turn after reactivation. Delete uses
ordinary subtree deletion and rejects active work. Successful retries survive
target deletion. No child-specific transcript or execution loop was introduced.

The store permit suite tests two independent handles competing for one ancestor
slot, rollback of rejected claims, proper-ancestor semantics, safe-yield guards,
admission/dispatch after yield, revision/cancellation races and recovery. Runtime
tests cover four host workers under a one-slot subtree cap, more than 100 blocked
queue entries, revisiting an earlier newly eligible scope, blocked resumptions
with cap increases/cancellation/deadlines, and recursive waits with one worker.
The legacy capacity test fake now waits during provider preparation, before any
attempt is admitted; yielding inside an unfinished provider attempt is intentionally
invalid. Actual Starlark and QuickJS tests verify the committed-cell wait boundary.

Child-control tests cover descendant authority, rejected sibling/ancestor mutation,
foreign content references, SQL rollback, concurrent retries, exact-input outcome
paging, retained queues, post-stop reactivation and both actual engines. The SDK
process fixture validates the new generated resource kind, queued retention at
zero capacity, progress after a cap increase and released usage after restart.

Focused race runs passed for store permits (3.669 s), runtime permits/capacity
(12.362 s), and RPC/child-control integration. Two independent concurrency reviews
found no correctness blockers. Pinned analysis reports zero lint issues and no
reachable vulnerabilities. The full local phase gate passed: store race coverage
35.705 s, runtime 83.255 s, process engines 97.788 s, generated contracts and SDK,
the new SDK restart fixture 5.118 s, retained fixture 2.267 s, and required daemon
regressions 2.775 s. The preceding committed resource increment also passed hosted
macOS/Linux and analysis in run `36440818797` at `fce5b5964`.
Completion reports, cumulative write allowances and final uniform lifecycle
acceptance still block Phase 4 completion; all Phase 5–7 obligations remain.

## Phase 4 uniform lifecycle acceptance

Root and child now run the same lifecycle tables. Controlled-provider tests cover
queued cancellation, stopping active work without discarding queued input,
reactivation before old cancellation settles, exact failure/history outcomes,
recovery without replay, preserved queued work and deletion receipts. Store tests
inject a cancellation-write failure and prove lifecycle/turn/permit rollback.

These checks exposed a postcommit race: session stop selected a cancellation
handle from the mutable active-session map after the transaction. A delayed result
could cancel a newly started turn. The store now returns transient
`LifecycleChange{Session, CancelTurnID}` captured in the same transaction; the
runtime cancels only that turn. Public runtime/RPC signatures are unchanged.
Deterministic root/child regressions delay both active and idle stop notifications
until newer work is executing, then confirm it completes successfully.

Focused lifecycle race/shuffle coverage, vet and scoped pinned lint passed. Full
RPC race tests also passed. The store suite will be rerun with the next write
allowance increment, since its concurrent projection changes affected the broader
run. Completion reports and write allowances remain outstanding.

## Phase 4 cumulative logical writes

Schema 13 extends the existing budget API with `logical_writes` and
`logical_write_bytes`, defaulting fresh roots to 100,000 and 1 GiB at revision one.
Action and immutable charge evidence commit in one transaction. Captured ancestor
associations outlive source deletion; deleting the whole tree removes its ledger.
Finite root caps, explicit child narrowing and revision checks reuse budget rules.
Model reservations read only model accounting and do not scan the write ledger.

Explicit content registration, mail send/replacement, child initial/follow-up
input, state write/append and subscription creation now charge the caller and
ancestors. Spawn charging deliberately closes the old uncharged-initial-prompt
gap. Ordinary human input remains exempt. Generic persistence helpers do not
charge: shared content aliases, automatic notifications, mail defer/observation,
transcript/checkpoint evidence and execution settlement retain their own bounds.
State append derives its submitted byte quantity before merging, and guest/RPC
request types cannot set that quantity. Budget limit JSON is now consistently
decimal-string encoded, including guest child-spawn arguments.

Tests cover independent-handle contention, every charge hook's rollback, late
caps and narrowing, idempotent and conflicting retries, overflow, notification
and alias exemptions, deletion/restart retention, and settlement after exhaustion.
The SDK fixture consumes exactly three writes/eleven bytes through spawn, state
write and append, then checks rejected-state absence, free retry, human/model
progress, restart, child deletion and shared evidence retention. Both actual
engines successfully execute a leaf with a zero logical-write allowance.

The full local phase gate passed: store race 41.197 s, runtime race 87.415 s,
process engines 96.505 s, generated contracts and SDK, new restart fixture 5.211 s,
retained fixture 2.241 s and required daemon regressions 2.751 s. This also verifies
the preceding lifecycle changes with the completed accounting implementation.
Focused runtime/lifecycle/engine coverage passed in 14.422 s. Pinned analysis
reports zero lint issues and no reachable vulnerabilities; independent review
found no correctness blockers. The prior permit/control commit `eedc650bd` passed
hosted Linux/macOS/analysis in run `36442791727`.

## Phase 4 completion report design

Keep completion publication separate from required terminal settlement. A bounded
parent-owned slot reserved when a child is admitted can capture the latest exact
turn outcome and full bounded text in the Finish/Recover transaction. Count slots
whose source children were deleted until their pending evidence is published;
descendant counts alone cannot bound repeated create/delete churn. A later bounded
coordinator can publish immutable parent-owned content and canonical mail, then
clear only the exact pending turn it read. Mail/content pressure leaves inspectable
pending evidence, never a repeated model turn or failed terminal commit.

Retain queued idle-parent wakeups and the existing failed-parent retry barrier.
Use completion provenance distinct from ordinary session/state mail. Coalescing
must preserve immutable published revisions and recipient deferral. Notice and
inline preview limits are 160 bytes and 4 KiB; ordinary digests remain capped at
2 KiB. Message mode suppresses successful automatic reporting, not failures.
Published and pending full evidence need a bounded guest read path as well as host
inspection; content.read currently exists only for the host. The reporting policy
must have one explicit owner and be captured for the exact turn being reported.

Do not restore full-turn automatic replay. Existing confirmed provider-attempt
retries and SQL settlement retries remain; failed/uncertain turns require explicit
new input. Report pressure is independently recoverable delivery work. Remaining
acceptance includes full inbox/content pressure, Finish/report rollback, restart,
replacement races, child deletion/GC, all modes and zero logical-write allowance.
Phase 4 remains open until this and its final cleanup checks pass. Phases 5–7 remain
fully authorized and pending.


## Phase 4 completion report implementation

Fresh schema 14 reserves at most 128 parent-owned completion slots when admitting
children. Finish and Recover capture each newly terminal child's exact input,
turn, last assistant message, outcome and policy atomically; they never require
mail space, content publication or remaining logical-write allowance. Pending
snapshots survive source-child deletion. Empty deleted-source slots release after
publication, while deleting a parent cascades its owned slots.

Reporting is an ordinary inherited `Configuration.ReportMode` value, defaulting
to notice and captured by the turn's configuration revision. Notice and inline
previews are bounded; message mode suppresses successful automatic reports.
Failures/cancellation/interruption still notify, and a suppressed success does
not erase an older pending failure. Report retries are delivery retries only.
Whole-turn automatic replay remains intentionally retired.

A four-candidate runtime pass advances past blocked parents, even when turn worker
capacity is full. Parent-owned JSON content, canonical completion-provenance mail
and exact pending-slot clearing commit together after blob publication. Mail
coalescing preserves immutable revisions and recipient deferral. Retained quota
pressure leaves a visible pending outcome; no evidence is silently dropped.
Host `completions.list/read`, guest `agents.pending_reports/read_report`, and scoped
`artifacts.read` supply metadata and exact-token, bounded-byte inspection. The new
SDK validates these generated shapes without keeping another report authority.

Store tests cover reserved capacity, atomic rollback, turn-captured configuration,
all terminal modes, replacement races, deferral, recovery, deletion retention,
mail/content pressure and escaped UTF-8 limits. Both real engines execute report
and artifact inspection through grant-backed operations; the complete evidence
exceeds the ordinary mail digest. The SDK fixture verifies automatic idle-parent
wake, immutable evidence, source deletion, restart, and a report larger than one
64 KiB page while the parent's 64 MiB content allowance is full. Existing unrelated
success fixtures explicitly choose message mode; lifecycle counters now observe
the target session so a legitimate parent failure-report turn is not mistaken for
child replay. No lifecycle assertions were removed.

The initial complete local phase gate passed (store race 57.747 s, runtime race
90.896 s, generated contracts, SDK, both process engines and retained daemon
regressions). Pinned analysis reports zero lint issues and no reachable
vulnerabilities. An independent read-only review found no correctness blockers.
The previous committed write-allowance revision also passed hosted Linux/macOS
and analysis in run `36444502836`.


Final cleanup acceptance now covers a detached child continuing after its parent
finishes in both engines. The test proves runtime worker ownership and SQL permits
transition from two to one to zero independently, then verifies exact-input
cancellation produces the retained parent's report. Subtree deletion is also
covered in both engines under pending-report pressure, checking affected kernels
close, unrelated kernel identities remain unchanged, and parent-owned evidence
survives child deletion.

The final local `task check:phase` passed with these acceptance tests: store race
68.886 s, runtime race 114.160 s, process engines 111.560 s, new SDK restart fixture
7.054 s, retained fixture 2.652 s and required daemon regressions 2.755 s. Final
`task check:analysis` again reports zero issues and no reachable vulnerabilities.
A final assertion-strengthening change in the detached-child test also passed its
targeted race/shuffle run (5.3 s), vet and scoped lint. No production files changed
after the full gate started. Phase 4 acceptance is complete. Phase 5 begins with
final-output validation, then context selection/compaction; Phases 5–7 remain
fully authorized. This does not claim the entire retained legacy suite is green.

The final Phase 4 revision `cb80f1307` also passed hosted Linux, macOS and analysis
in [run 36447220434](https://github.com/context-labs/whip/actions/runs/36447220434).

## Phase 5 output contracts

Fresh schema 15 adds an indexed last-assistant lookup. The ordinary runner now
enforces the turn-captured output schema. An invalid final candidate remains in
raw history and permits one corrective model response through ordinary attempt
reservation, budgets and settlement. A second mismatch or corrective tool call
fails explicitly; the latter records the call but executes no cell. No whole-turn
replay or separate output column was introduced.

`turns.output` derives exact JSON bytes from a successful terminal message and
its captured configuration. Base64 on the wire avoids JavaScript numeric
coercion. A non-null output containing JSON `null` is distinct from no output.
Config changes, clearing, restart and repeated reads cannot re-execute work or
change an earlier turn's contract.

Domain tests exposed both instance-number misclassification and floating-point
constraint rounding in the earlier schema validator. Validation now uses pinned
`github.com/santhosh-tekuri/jsonschema/v6` v6.0.3 with exact numbers and an explicit
rejecting external loader; the existing library remains the wire generator.
Pre-arithmetic numeric bounds and a schema-position count bound prevent huge
exponent allocation and signed-count overflow. Tests distinguish schema keywords
from similarly named keys inside ordinary const/enum/examples data and cover all
supported explicit drafts, local references and recursive schemas.

Output acceptance covers cancellation after candidate commit, uncertain usage,
SQL settlement retry without provider replay, raw invalid responses, captured
schema/clear behavior, JSON null, fences and exact integers/decimals. The real SDK
fixture completes a correction, rejects a second invalid response, distinguishes
null from clearing, preserves an integer above the JavaScript safe range, and
re-reads the same result after restart without another provider call.

The first full gate found a QuickJS child-control fixture exceeding the 256 MiB
worker RSS cap under race instrumentation. Five measured runs of the same
QuickJS control flow peaked at 189–256 MiB in test-binary workers, with one kill;
the actual production worker peaked at 64–78 MiB and passed all five. The host
runtime remains race-instrumented in integration tests, while its engine fixture
now builds the actual production worker once. Engine/process race and memory
exhaustion suites retain their original limits and assertions. No production
memory limit was raised.

The final `task check:phase` passed: store race 58.908 s, runtime race 67.953 s,
generated contract drift/interop, SDK checks, both engine suites, the v4 SDK
restart fixture 6.914 s, retained fixture 2.684 s and required daemon regressions
2.836 s. `task check:analysis` reports zero issues and no reachable vulnerabilities;
the final test-fixture changes also pass scoped pinned lint. Module tidy/verify
passed. Phase 5 remains in progress; context and compaction are next.

The output-contract revision `a4922d174` also passed hosted Linux, macOS and
analysis in [run 36449731324](https://github.com/context-labs/whip/actions/runs/36449731324).

## Phase 5 context and compaction foundation

Fresh schema 16 adds immutable compactions and one revisioned context head.
Input kind owns the distinction between prompt work and maintenance; turn kind
is a projection from the accepted input. Manual `sessions.compact` uses ordinary
receipts, queue capacity, permits, captured configuration and cancellation.
Maintenance produces no transcript message, cell, reply preview, structured
answer, mail acknowledgement or child report. Store guards preserve an earlier
failed prompt's mail retry barrier and a child's already pending completion.

The runner assembles a request from selected summary, pinned raw messages and
raw tail. Summary text remains quoted untrusted data. Helpers use the same
reserve/dispatch/accounting path as ordinary responses and atomically settle
summary evidence and conditional selection. Lost SQL acknowledgement retries
never redispatch a provider. Stale or cancelling work cannot replace the context
head, and replay cannot reselect a summary after undo. Idle revision-checked
undo retains evidence and changes no files, checkpoints or messages.

Before omissions, both guest engines and the SDK gain fixed-snapshot raw-history
metadata, literal bounded search and exact byte reads. Guests can only inspect
their own session, through normal grant-backed operations. Tests cover mail
revision fidelity without delivery acknowledgement, owner isolation, no-match
search progress, UTF-8 byte boundaries and numbers above JavaScript's safe range.
Runtime exposes only the client-facing history methods; the runner's full-message
range and recent-tail queries stay on its direct store interface.

Manual compaction keeps four recent message-bearing turns. Ordinary requests
compact older history before their 100-message/4 MiB limit, progressively keeping
four through one recent whole turns. Helper batches preserve all results of an
assistant call, have bounded bytes/messages and a shared 16-fold limit per turn.
Effectiveness compares the actual replacement summary encoding against replaced
source bytes, excluding pins that remain present. Corrections and later tool
boundaries rebuild through the same selection path without replaying effects.

The SDK process fixture compacts seven turns, checks ordinary accounting without
an assistant message, restarts, reads the same selection and raw history, undoes
selection and restores the raw prefix to model context. It also drives 51 prompt
turns: the final turn records helper and ordinary attempts, covers exactly raw
sequence 94, and retains all 102 authored messages. A receipt tombstone regression
caught and fixed null matching both input-kind schema variants; generated Go/TS
contract fixtures now include that case.

Single oversized turns and an indivisible oversized tool exchange still fail
explicitly. Exact opening-input pinning for split turns, typed provider
context-limit recovery, proactive token policy and dynamic instructions remain
required work. This increment does not complete the retained compaction family
or change the scope of Phases 5–7.

The coherent `task check:phase` passed: store race 65.417 s, runtime race 69.247 s,
runner race 2.286 s, both engine suites, generated Go/TS contract drift and
interop, SDK checks, the new process fixture 8.529 s, retained reference fixture
2.291 s and required daemon regressions 2.766 s. `task check:analysis` reports
zero lint issues and no reachable vulnerabilities.

## Phase 5 long turns and confirmed context rejection

Fresh schema 17 deliberately rejects the previous disposable database version:
summary pin semantics are now stronger even though the tables are unchanged.
A cut inside a live or partially covered prompt turn requires the exact opening
input-backed message. A mail entry with user role, another turn's input or a
missing pin cannot satisfy it. Terminal fully covered turns and mail-only turns
need no opening pin. The guard runs for every summary settlement, including
intermediate cuts in older history. Pin-set validation failure still records
completed provider billing and returns rejected summary evidence.

The runner restores pins with bounded indexed lookups and folds within a long
turn when retaining recent whole turns cannot fit. It retains the newest
assistant exchange with every tool result, plus later mail, and carries the
exact required opening input. Manual compaction invokes the same fallback after
its normal older-turn fold. New pin sets can discard obsolete opening inputs
once their whole turn is covered. Effectiveness compares the entire before/after
projection, including changed pin bytes; coverage advances and the shared
16-fold ceiling still apply. An indivisible oversized exchange fails explicitly.

OpenAI-compatible dispatch recognizes only complete bounded non-streaming HTTP
400/413 responses with exact structured context-limit error codes/types. Usage
and cost decoding is independent and diagnostics omit provider bodies. The
runner can replan once per ordinary turn, only after that certain rejected
attempt settles. Compaction must advance coverage before a new logical model
round is prepared. Preparation errors, settlement errors, arbitrary error text,
partial streams and uncertain transport never authorize replan. Helpers cannot
recursively replan. Existing output-correction state and completed effects survive
request reconstruction.

The first real-process scenario exceeded the existing 64-cell turn bound. The
fixture was corrected to create context pressure using large assistant evidence:
ten complete multi-call exchanges execute 30 cells per engine without changing
any production limit. Both Starlark and QuickJS then retain all 42 raw messages,
pin the exact accepted input, restart and recover the same count. A confirmed
context rejection records failed/helper/resumed attempts; the restored count
remains 30. A second rejection stops after one helper, and a disconnected
provider creates one uncertain attempt. The targeted SDK fixture passed in
11.480 s before the broader gate.

The full `task check:phase` passed: store race 68.540 s, runtime race 71.154 s,
runner race 2.600 s, generated contract/SDK checks, both engines, new process
acceptance 11.788 s, retained process acceptance 2.634 s and required daemon
regressions 2.806 s. The final sanitized context-rejection diagnostic also passed
focused model/runner race tests (3.059 s/1.890 s); it identifies the rejection
category in durable failure evidence without retaining provider text.

Final `task check:analysis` reports zero lint issues and no reachable vulnerabilities.

Hosted Linux passed the context-foundation revision, but macOS exposed an
inherited worker-exit diagnostic race: an early process exit could close stdin
before the read loop reported EOF, returning a raw closed-pipe error without
engine/status context. Parent revision `18d5b7fa5` fixes write-side annotation
while retaining the real I/O cause. Deterministic tests reproduce the original
failure in both engines; hosted-seed process race stress passed ten repetitions
(63.704 s), checkpoint/cancellation checks passed (21.362 s), and scoped lint was
clean. The recovery increment rebased onto that fix without conflicts; the real
SDK acceptance passed again (11.927 s) and final whole-scope analysis remained
clean. Updated hosted gates are pending; prior failed macOS evidence is not
reported as a pass.

## Phase 5 captured compaction policy

Fresh schema 18 captures one compaction policy with each session configuration
and turn: a helper model or conversation-model fallback, plus a 1–100% threshold
whose explicit zero reset resolves to 50%. Host defaults, definitions, session
patches and parent-to-child copies share whole-field resolution. Active turns
keep their old policy; reopening storage never re-resolves it. Every compaction
trigger uses that helper route, with no fallback for an invalid explicit route.

The runner checks proactive pressure before ordinary requests and after a final
reply if no fold has occurred. The current turn's validated ordinary input count
is authoritative when known, including zero; otherwise a bounded saturating
request estimate is used. Later request growth is estimated. Helper usage,
child usage and reservation bounds do not measure occupancy. Fold or route/window
changes invalidate measurements. No cross-turn usage cache is introduced, so
warm and restarted turns behave alike. Unknown windows disable only proactive
checks. An estimate cannot itself reject a request as too large.

No replaceable source, including a prefix too small to beat the smallest quoted
summary envelope, skips proactive helper dispatch. Later material is rechecked.
A useful fold whose estimated floor still exceeds the threshold stalls further
proactive helpers for the rest of that turn, while local and confirmed-rejection
paths remain available. A failed post-final helper preserves the durable answer
and records a failed turn. Explicit retry attempts can preserve an earlier
validated ordinary input observation without altering their separate ledgers.

The SDK HTTP fixture changes A/50% to B/75% while the sixth ordinary request is
blocked. Its 60,000/100,000 usage folds under the captured A/50% policy, and the
next maintenance turn uses B. Helper usage of 90,000 is billed independently
without triggering another fold. Reset/restart restores conversation-model
fallback, and a missing explicit helper route fails without a provider request.
The initial pricing assertion exposed incomplete fixture usage details; reporting
explicit zero category counts permits the expected exact cost of 270,010 nano-USD.
Production unknown-cost semantics were preserved.

`task check:phase` passed: store race 69.381 s, runtime race 71.533 s, runner race
2.485 s, both engines, generated contract/SDK checks, new SDK acceptance 11.572 s,
retained process acceptance 3.782 s and required daemon regressions 2.786 s.
`task check:analysis` reports zero lint issues and no reachable vulnerabilities.
Independent review and focused runner race/shuffle tests also passed.

The branch filter previously omitted PRs whose base was a context-named redesign
branch. Foundation commit `da6b1f807` broadens it to every `codex/backend-redesign-*`
base, preserving the exact integration branch. `actionlint` passed. This restores
hosted checks for the stack; it does not substitute for their eventual results.
Dynamic instruction refresh and the remaining Phase 5–7 obligations stay open.

## Phase 5 workspace instruction capture

Fresh schema 19 stores one immutable metadata-only instruction manifest per
ordinary turn. The captured policy resolves literal text, authorized workspace
project files and project skill metadata once before provider dispatch. Confined
file descriptors, bounded reads and source validation prevent ambient filesystem
access; standing read authority is checked before capture. Later file/config
edits and grant revocation do not rewrite a running turn. New turns and retained
children refresh sources, including after restart. Maintenance compaction skips
external instruction reads. Skill bodies remain deferred.

The generated `turns.instructions` contract exposes ordered source paths, sizes
and SHA-256 digests without reopening files or persisting instruction bodies.
Audit insertion failure and malformed applicable sources stop before any model
attempt. The real QuickJS/HTTP SDK fixture checks blocked-request edits and
revocation, frozen follow-up requests, child inheritance, restart, source hashes,
malformed-source failure and maintenance isolation. Reader tests cover confinement,
symlink retargeting, nonregular files, truncation/growth and all read bounds.

The first full gate caught an outdated architecture allowlist and an unchecked
directory close. Both were corrected; the boundary test now explicitly includes
the instruction and skill packages. Independent review found no further issues
in capture, authority or restart behavior.

Final `task check:phase` passed: store race 74.272 s, runtime race 75.343 s, runner
race 2.447 s, instruction race 4.155 s, skills race 3.071 s, both engines, generated
contract/SDK checks, SDK process acceptance 12.192 s, retained process acceptance
4.125 s and required daemon regressions 2.816 s. `task check:analysis` reports zero
lint issues and no reachable vulnerabilities. Parent foundation commit
`da6b1f807` also passed all hosted checks, including macOS and Linux. Explicit
skill bodies, shared live catalog inspection, authorized ancestor/global sources
and the remaining Phase 5–7 obligations remain open.

## Phase 5 explicit skills and live catalog

Fresh schema 20 accepts complete `invoked_skill` sources, bounded to 256 KiB,
while project files and skill frontmatter retain their 64 KiB source bounds.
One catalog resolves each exact name using the last sorted source path, and the
same winners drive model discovery, current-input invocation and `skills.list`.
Audit retains all consumed metadata, including disabled entries and duplicate
losers. Disabled winners remain explicitly invocable. Overall composition and
manifest bounds still fail explicitly rather than dropping sources.

Only the newly claimed canonical input's direct text parts supply explicit
references. Each selected body receives a new standing-grant check, confined
complete-file read and exact frontmatter identity check. Its full digest joins
the immutable turn audit before provider dispatch. Input stays literal, active
requests reuse the captured body, and later turns never re-expand old references.
This deliberately replaces legacy expansion into durable user text.

The read-only catalog API uses a current policy/authority snapshot, needs no
turn or permit, and works for idle and stopped sessions. Name cursors and
case-sensitive prefixes page at most 100 live winner records, with disabled
status and relative source metadata. Inspection neither reads bodies nor claims
input, starts a kernel, creates permissions or acknowledges mail. Tests verify
that a concurrent writer cannot mix policy and authority revisions.

Focused race/shuffle tests passed for session/store (1.217 s/4.547 s), reader
(2.701 s) and runtime (1.996 s). Independent integration review found no defects.
The first SDK run used stale generated operation metadata and rejected
`skills.list`; regeneration fixed that setup error. Static analysis found one
test-formatting issue, which was corrected. No production behavior was loosened
to make those checks pass.

Final `task check:phase` passed: store race 72.909 s, runtime race 74.839 s, runner
race 2.679 s, instruction race 3.135 s, skills race 3.375 s, both engines, generated
contract/SDK checks, SDK process acceptance 11.806 s, retained process acceptance
2.233 s and required daemon regressions 3.429 s. The process scenario covers a
70 KiB disabled skill, repeated references, exact body hash/size, file edits and
revocation during a blocked request, frozen follow-up requests, child/restart
refresh, unchanged canonical input and paged inspection with discovery disabled.
`task check:analysis` reports zero lint issues and no reachable vulnerabilities.
Hosted recovery and compaction-policy PRs also passed Linux, macOS and analysis.
Named host roots, standing user instructions, authorized ancestors and the
remaining Phase 5–7 obligations remain open.

## Phase 5 named host skill roots

Fresh host config version 3 maps at most 16 logical skill-root IDs to explicit
absolute directories. Captured instruction policy selects an ordered subset;
registration and selection create no authority. Standing `skills.read` grants
for the exact ID authorize automatic discovery and explicit body capture.
Missing grants cause no filesystem probes. Workspace entries come last in the
shared winner rules. Global catalog limits still apply across all roots before
deduplication. Fresh schema 21 adds nullable logical root IDs to audit sources;
paths remain relative and OS errors do not expose absolute host paths.

Guest `skills.read` resolves the cell's immutable turn configuration, then uses
ordinary durable permission/admission/dispatch. It reads a complete validated
file up to 256 KiB and returns at most 64 KiB of base64 bytes. Continuations
require the full-file digest, preventing mixed revisions. Workspace reads use
existing `files.read` authority; named roots use `skills.read`. A one-use approval
authorizes only that call, never automatic capture or neighboring files/scripts.
Descriptor lifetime follows the existing dispatcher sequence; review removed an
unneeded second in-memory lifecycle state machine. Guidance supplies the right
call syntax for each engine.

Focused race/shuffle passed for domain/config/store (1.285 s/1.514 s/5.331 s),
reader (3.353 s after lint), and operation/process tests (4.473 s/3.415 s). Tests
cover registry validation without probing, whole-field copying/clear, concurrent
policy/authority snapshots, issuer revocation, one-use isolation, captured old
policy, exact name lookup, Unicode pages, changed hashes, nonregular/escaping
files, cancellation and real-dispatch descriptor cleanup. Independent review of
both composition and the operation path found no defects. The first analysis
run flagged construction of an oversized-root test slice; using concatenation
preserves that boundary assertion without the lint ambiguity.

Final `task check:phase` passed: store race 75.247 s, runtime race 79.502 s, runner
race 2.738 s, instruction race 5.426 s, skills race 2.716 s, generated contracts,
SDK checks, both-engine SDK process acceptance 12.572 s, retained process
acceptance 3.698 s and required daemon regressions 2.711 s. The SDK scenario
changes active root policy and file content before dispatching two named reads,
reconstructs exact Unicode bytes, checks workspace precedence and no-grant
missing-root isolation, then checks child inheritance, restart and issuer
revocation. Final engine-specific guidance also passed focused instruction/runtime
tests (1.978 s/3.367 s) and scoped lint. `task check:analysis` reports zero lint
issues and no reachable vulnerabilities.

The workspace instruction PR passed all hosted checks: Linux 10m26s, macOS
14m31s, analysis 3m5s. Named roots deliberately reject legacy global symlinks that
escape their root. Standing user instructions, authorized ancestors and all
remaining Phase 5–7 obligations remain open.


## Phase 5 standing user instructions

Fresh host config version 4 names one optional standing instruction file; captured
session policy selects it. Exact standing `instructions.read` authority for
resource `standing` admits automatic reads. Disabled or ungranted sources cause
no filesystem probes. No HOME lookup, template creation or source-file writes
occur. Fresh schema 22 records the full raw source digest/size, while composed
text retains the trimmed-line/comment-filtering convention. Active turns freeze
their captured text; later turns and children recheck policy and authority.

The reader anchors the parent directory and uses descriptor-relative `openat`
with `O_NOFOLLOW`. Tests exposed that Go's `os.Root.OpenFile` resolves the final
symlink internally even when passed that flag; the replacement rejects it at
the actual open. Regular-file, complete-read, UTF-8/NUL, 64 KiB, cancellation,
parent-retargeting and descriptor-cleanup checks pass. Runtime tests prove no
provider attempt or source manifest on authorized-source failure, no probe on
missing authority, raw-file audit and filtered text composition. Independent
review found one host-validation/reader basename mismatch; both now reject a
backslash in the basename. Its final focused config race test passed in 1.330 s,
with zero scoped lint issues.

The SDK acceptance passed after correcting a fixture's `config` property to the
wire's `configuration`. It covers edits and revocation during a blocked request,
frozen follow-up rounds, invalid disabled/ungranted sources, child inheritance,
restart, raw hashes, pre-dispatch failure and maintenance/catalog isolation.
`task check:phase` passed: store race 76.379 s, runtime race 80.742 s, runner
race 1.985 s, instruction race 4.452 s, skills race 2.966 s, both engines,
generated contracts/SDK, SDK process acceptance 13.319 s, retained acceptance
2.372 s and required daemon regressions 2.701 s. `task check:analysis` reports
zero lint issues and no reachable vulnerabilities after replacing one `errors.As`
with the current idiom. The basename validation refinement was separately tested
after that gate started.

Hosted PR #210 passed Linux, macOS and analysis. PR #211 passed Linux and analysis,
but its macOS SDK acceptance hit the 180-second fixture deadline; diagnosis is
pending and it is not claimed green. No failure artifacts were uploaded. This is
separate from the previously green Phase 4 revision.

## Phase 4 completion audit correction

The user challenged the Phase 4 completion claim. Rechecking the original plan
(`a9e021723`, also matching the original checkout's seven Phase 4 criteria), the
retained feature map and implementation found a missing collaboration capability.
Phase 4 is reopened. Passing hosted checks at `cb80f1307` proves the implemented
test scope, not complete coverage of the original retained feature requirements.

Ordinary authored mail must carry scoped evidence. The retained feature map says
evidence handles can be granted with a message, and
`internal/legacy/session/mailbox_storage_test.go`'s
`TestSiblingDigestPreservesUnicodeAndEvidenceAccess` proves recipient access and
evidence-only mail. The new `session.MailSend` and generated `SendMailParams` have
no content-reference field, require nonempty text, and `store.sendMail` performs
no content-access transfer. Completion-generated evidence covers only automatic
reports. No explicit retirement was recorded for authored-mail evidence.

The immediate remaining Phase 4 work is an atomic authored-mail content-sharing
path through store, host/guest API and generated SDK, with owner isolation,
rejection rollback, idempotence, exact revision presentation, restart and sender
deletion coverage. Keep existing content bodies immutable and share a recipient
reference in the same mail transaction; a digest or a sender's reference ID must
not itself grant access. Evidence-only mail must remain possible.

The rest of the audit located implementation evidence rather than relying on
checked boxes:

| Original Phase 4 criterion | Evidence inspected |
| --- | --- |
| Atomic child admission and restart | `store/spawn_test.go` concurrent retry, rollback at each write, scoped reference copying and reopen; `runtime/recursion_test.go` unloaded-worker restart |
| One root/child execution and lifecycle path | `runtime.execute`, ordinary store Claim/Finish/history, `runtime/lifecycle_test.go` root/child tables for cancellation, failure, recovery and deletion |
| Ancestor reservations and narrowed authority | `store/budgets_test.go` independent-handle sibling contention and rollback; `store/delegation_test.go` exact direct-parent issuer chains and no approval widening |
| Progress under saturated workers/kernels | Both-engine `runtime/recursion_test.go`; scoped permits, blocked queue/resumption and nested one-worker cases in `runtime/turn_permits_test.go` |
| Explicit retry/report semantics | Host `Model.MaxAttempts`, common runner's confirmed/uncertain retry distinction, `runtime/provider_test.go`; captured report modes and transactional publication in store/runtime completion tests |
| Mail inspection/acknowledgement and private/shared state | `store/mail_test.go` exact revisions, failure barrier at each depth and reopen, no inspection delivery; store/runtime state tests for CAS, isolation, large values and restart. Authored-mail evidence remains missing |
| Distinct parent finish, child cancellation, deletion and eviction | Both-engine `runtime/detached_child_test.go` plus engine eviction/subtree disposal tests; parent-owned completion evidence retention |

Subscriptions additionally have atomic value/cursor/mail/operation tests,
restart, deferral, cancellation and quota rollback. These test families were
present at the Phase 4 revision; later compact-turn exclusions do not replace
their original evidence.

The original guidance does not require a session-owned retry field or automatic
whole-turn replay. Host route attempt limits and the shared runner implement an
explicit safe-retry policy; full-turn replay retirement is already documented.
Do not invent a missing configuration requirement from field absence alone.
Client migration must also account for the documented array-append change:
append now concatenates arrays, so appending one object uses a one-element array.

Two capacity semantics also differ from the legacy implementation and must be
described as design changes, not equivalent ports. `active_operations` now counts
host operations; model execution is bounded separately by runnable-descendant
permits and host worker capacity. A session's own turn is excluded from its
proper-descendant permit count. `descendants` counts retained children, including
stopped ones; stopping retains data and capacity, whereas deletion frees that
retention capacity. Legacy `active_children` excluded stopped children. The
existing resource tests deliberately assert the new behavior. These decisions
are documented implementation choices, not evidence of separate user approval;
they must remain visible in final scope reconciliation and client migration.


## Phase 4 authored-mail evidence repair

The missing retained capability identified above is now implemented. Optional
`MailSend.evidence_ref` names a sender-owned reference; each immutable mail
revision retains a recipient-owned reference to the same immutable body. Sharing,
mail admission, revision creation, logical-write charging and guest-operation
settlement commit together. The new reference has the recipient's ownership and
limits without copying bytes or charging another content write. A digest or a
foreign reference is never authority. Fresh schema 23 adds the nullable, deferred
foreign key and its partial index. Evidence-only mail accepts an empty body.

Idempotence hashes the original sender request before reference translation.
Lost acknowledgements, restart and sender deletion therefore return the original
recipient alias without creating another alias. Replacement produces another
immutable revision; deferral preserves its existing recipient alias. Presentation
uses the exact retained revision and appends a bounded reference locator after
text truncation. It does not hydrate attachment bytes into model context.
Human inspection still does not acknowledge delivery.

Replacement evidence covers direct relatives and self, ownership rejection,
quota/transaction/operation-settlement rollback, immutable old revisions,
replacement, deferral, restart, both deletion directions and stable retries.
Actual Starlark and QuickJS guests send evidence-only mail and read exact paged
UTF-8 bytes after sender deletion and restart; a sibling with the reference ID
and an artifacts capability still cannot read it. Generated protocol fixtures
and the SDK acceptance exercise lost acknowledgements, owner isolation, human
inspection, restart, lazy presentation and stable reference identity.

Local validation: `task check:phase` passed (store race/shuffle 79.915s, runtime
87.630s, process engine 101.734s, v4 SDK fixture 13.346s and selected retained
daemon regressions 2.656s). `task check:analysis` passed with zero lint issues and
no reachable vulnerabilities. The partial foreign-key index was added during
the gate and checked with SQLite query planning and focused affected tests;
the only later test edit was formatting. Phase 4 remains open pending hosted
validation of the repair. A preceding host-skills macOS job hit the SDK fixture's
aggregate 180-second timeout; that job is failed evidence, not a passing phase
gate. A separate harness increment is adding named scenario budgets and retained
timeout diagnostics without removing acceptance scenarios.


## SDK fixture feedback repair

The host-skills macOS run `36462714658` failed at the growing fixture's single
180-second Node timeout. Linux passed the same revision in 113.970s. The failed
run had neither scenario timing nor uploaded artifacts, so it does not establish
which step stalled or whether the aggregate budget alone was responsible.

The fixture now runs the same assertions and scenario functions under named
stage deadlines. Compilation, readiness, scenario execution and cleanup have
separate budgets; the existing job-wide CI bound remains. Each stage writes
start/completion/failure and elapsed time to `progress.json`. A stage failure
aborts all socket transport operations, including observers with no local
deadline, prevents new process starts and reaps the runtime before copying
failure state. No acceptance was removed or weakened.

The isolated increment passed the normal fixture in 13.878s and the race fixture
in 95.846s. A temporary fault probe replaced one resources operation with an
indefinite real SDK observation and used a 100ms scenario budget. It failed in
103ms, stopped the runtime, retained progress/log/observations/SQLite, passed
SQLite `quick_check` and never entered the next scenario. The probe is not a new
production test flag or a permanently duplicated acceptance suite.

Integration preserved all 19 acceptance function bodies byte for byte, adding
named stages for the newer standing-instruction and mail-evidence scenarios.
The integrated race fixture passed in 102.496s. In that run context recovery
took 24.022s and completion reports 17.837s; those measurements localize costs
without claiming to explain the earlier hosted timeout. Hosted results for
the harness revision remain pending.


## One mail attachment path

Automatic completion reports now put their existing parent-owned reference in
the same immutable mail revision field as authored mail. `CompletionNotice`
contains bounded outcome metadata and previews; it no longer has another
attachment field inside its JSON body. Publication creates no additional alias.
Digest truncation retains the attachment locator independently of body text,
and mail listing can discover full report evidence without parsing the body.

Store coverage checks coalesced and deferred revisions, old transcript evidence
and a preview larger than the digest limit. Both engines discover the published
reference via `mail.list` before using `artifacts.read`; SDK acceptance checks
the same field, child deletion and restart. A separate send-retry assertion
clarifies the existing admission contract: retrying an original request after
replacement returns current mail metadata without creating another reference.

The integrated `WHIP_SDK_RACE=1 task check:phase` passed: store race/shuffle
77.508s, runtime 85.124s, process engine 101.318s, v4 SDK 101.664s and selected
retained daemon regressions 2.657s. Analysis reported zero lint findings and no
reachable vulnerabilities. The later send-retry assertion passed its focused
race test in 1.696s and scoped lint; no production code changed after the gate
started. Canonical domain docs now remove stale Phase 2/4 future-work statements
and declare current schema/config versions in one place.

The preceding standing-instruction hosted run `36465298896` passed Linux and
analysis but also hit the old aggregate 180-second SDK timeout on macOS. Its
failed result remains recorded. The new named-scenario harness and these mail
changes still need hosted evidence; Phase 4 remains reopened until that passes.


## Phase 5 chat provider wire profiles

The new Chat adapter now preserves the pinned compatible-provider profiles.
Captured effort `off` omits the wire field. A cache key derives from the stable
session ID, bounded to 64 bytes with SHA-256 for longer IDs; no cache-key column
or in-memory session registry is added. Exact preset roots suppress unsupported
cache-key fields, and the exact DeepSeek root selects non-thinking mode for its
retained V4 profile. Custom paths, ports and lookalike hosts retain the generic
contract. The immutable attempt snapshot records the original selection and the
digest of the actual post-profile body; preparing freezes that body and route.

The isolated change passed model, runner, config and command race tests, scoped
vet and pinned lint. Integration passed `task check:fast`, `task check:analysis`
(zero lint findings and no reachable vulnerabilities), and the complete actual-
process v4 SDK fixture in 17.461s. Wire tests cover tool round trips, all pinned
profiles, custom endpoints, stable and distinct session keys, frozen request
preparation and credential exclusion. These are deterministic local contracts,
not claims about current remote provider availability. Hosted gates are pending.

The remaining provider execution order is Responses with private durable
continuation, subscription execution with credential-generation checks, and
stateless helper/batch calls through the ordinary attempt ledger. Sampling
parameters require explicit copied configuration. Provider onboarding/account
management UI remains a client-adoption obligation. Legacy partial-stream
regeneration will not be restored: the new accounting contract forbids automatic
replay after uncertainty. Idle-stall detection can terminate uncertain work but
cannot itself authorize another dispatch. Each confirmed authentication retry
must likewise be a separately recorded attempt.


## Phase 5 authorized ancestor instructions

Host `project_roots` publishes named boundaries. A session copies the nullable
`instructions.project_root` selection; the exact `instructions.read` grant for
`project:<id>` separately admits membership metadata and instruction sources.
Neither host publication nor copied selection grants access. This authority
includes the verified boundary-to-cwd chain without an additional workspace
`files.read` grant. Workspace, project, standing-file and host-skill authorities
remain distinct. Denied or unrelated project sources fall back only to an
independently authorized workspace source.

The runtime opens an authorized boundary, verifies canonical membership against
boundary/cwd file identities, and reads through its confined directory descriptor.
Stable symlink aliases work; escaped sources and changed identities fail. One
bounded directory chain replaces a separate cwd scan. Rules follow directory
and configured filename order; discovery, explicit invocation, human inspection
and scoped guest reads share the same skill winners. Audit metadata records
project scope and boundary-relative paths. Source bytes remain turn-local;
there is no additional persistent content cache or filesystem snapshot claim.
Fresh config 5 and schema 24 capture the new policy and provenance.

The isolated implementation passed domain, storage, reader and both-engine race
tests, vet, generated-contract/SDK checks, and the real-process SDK fixture.
Integration review found that a combined rule/skill manifest could exceed the
reader's source limit even though storage still rejected it before model dispatch.
A three-line guard now rejects that combined overflow in `Load`. Its real-filesystem
regression first reproduced the issue, then proved exact 1,152-source success and
1,153-source failure for both discovery and explicit-invocation-only paths.

Integrated `WHIP_SDK_RACE=1 task check:phase` passed: store 77.644s, runtime
90.692s, process engine 102.703s, v4 SDK fixture 109.501s, retained process fixture
4.267s and selected retained daemon regressions 2.647s. The project SDK stage
took 7.008s and covered both engines, aliases, source refresh, captured policy,
restricted/delegated children, issuer revocation and restart. Analysis passed with
zero lint issues and no reachable vulnerabilities. The later combined-source
guard passed the integrated instruction race suite in 4.577s and isolated pinned
lint; it was not credited to the earlier-started full gate. Hosted validation of
this increment remains pending.

The preceding named-scenario harness at `72d22969a` passed Linux, macOS, analysis
and the aggregate gate in [run 36466900346](https://github.com/context-labs/whip/actions/runs/36466900346).
That green result includes the authored-mail evidence repair.


## Phase 4 audit closure

The final unified mail attachment increment at `1244d7cd2` passed hosted Linux,
macOS, analysis and the aggregate redesign gate in
[run 36467683567](https://github.com/context-labs/whip/actions/runs/36467683567).
Together with the local phase/analysis evidence recorded above, this closes the
authored-mail evidence gap found by rechecking the original seven Phase 4
criteria and retained feature guidance. Phase 4 is now complete through the
stack of PRs #202, #213, #214 and #215; #202 alone did not satisfy the audited
scope. The earlier completion claim was premature and remains recorded as such.

The timeout failures at the preceding host-skills, standing-instruction and
authored-mail revisions remain failed results. They are superseded by the
combined passing repair, not reclassified as successes. No acceptance scenario
was removed. The named-stage harness has independent hosted evidence at
`72d22969a`. Phases 5–7 and their retained-feature/client/cutover criteria remain
open.

## API Responses and private continuation

The explicit `openai-responses` host route uses the existing recorded-attempt
path, including tool execution, content hydration, streaming, context rejection
and truthful usage/cost settlement. Opaque output is immutable private message
evidence, bounded and committed with the assistant message and model attempt.
The selected ordinary context can replay it only under the same route,
credential and model with matching visible parts. Compaction helpers and public
history, inspection, attempts and operation results never receive private data.
No provider conversation cache or subscription authentication was added.

Focused tests cover completed-item fallback, malformed/incomplete accounting,
invalid/oversized private output, scope mismatch, exact settlement retry,
rollback, restart, ownership and private-byte-driven compaction. Actual-process
SDK acceptance exercises both engines through initial execution, process
restart and a live route change while retaining the same REPL. Every stage
performs the two recorded model requests and checks guest/public privacy.

An initial full `WHIP_SDK_RACE=1` fixture run failed in the QuickJS route-change
cell with the existing 256 MiB worker RSS limit. Its failure evidence remains at
`test-results/redesign/whip-v4-yU74V6` and the original `/tmp/whip-v4-yU74V6`.
The saved initial/restart QuickJS images were both exactly 1,507,796 bytes;
continuation lives only in the host/store. External RSS sampling on unchanged
reruns observed roughly 210–244 MiB race workers versus 51–55 MiB shipping
workers. This supports instrumentation overhead as the source of pressure,
without proving that every allocation peak was observed. No process limit,
assertion, route-change lifetime or race setting was changed. The unchanged
isolated race scenario passed, then the complete race SDK fixture passed in
112.403s (Responses stage 9.266s). A repeated memory failure requires a separate
shared worker/harness investigation; a passing non-race run is not a substitute
for the required race fixture.

Full affected Go race suites passed (store 78.124s, runtime 83.946s), as did
protocol schema/type drift checks, SDK unit tests, pinned lint and reachable
vulnerability analysis. Later focused tests also cover whitespace-formatted
empty terminal output, malformed status with independently valid accounting,
and exact message ID/phase replay; these changes do not alter the successful
SDK wire path. No generated public contract change is needed for private state.

Integration with the Chat profiles and ancestor-instruction work passed
`WHIP_SDK_RACE=1 task check:phase`: store race 80.408s, runtime race 90.827s,
process engine race 99.359s, full v4 SDK fixture 119.183s, retained process
fixture 4.534s and selected retained daemon regressions 2.649s. The Responses
SDK stage passed in 9.107s, including the live route change without restarting
its REPL; the ancestor-instruction stage passed in 7.007s. The earlier worker
memory failure did not recur in this combined run. All acceptance scenarios and
production memory limits remain unchanged.

The first combined phase run caught an ancestor-config test asserting the old
literal version 5 after Responses advanced the fresh config to version 6. The
test now verifies the current version and rejects the immediately previous one.
That failed run is not counted as acceptance. The final full gate above includes
the correction. Full analysis passed with zero lint issues and no reachable
vulnerabilities; a later scoped config lint also passed after the test edit.
Current fresh schema is 25; the public protocol remains development major 4.
Hosted validation of this Responses increment remains pending.

The preceding Chat wire-profile revision `fc8360949` passed Linux, macOS,
analysis and the aggregate redesign gate in
[run 36468457638](https://github.com/context-labs/whip/actions/runs/36468457638).
Ancestor revision `a8952622a` has passed hosted analysis; its Linux and macOS
checks in [run 36470031266](https://github.com/context-labs/whip/actions/runs/36470031266)
were still running at this integration checkpoint. Subscription authentication,
stateless helpers, sampling and the other Phase 5–7 acceptance criteria remain
open.

## Subscription credential capture boundary

The retained independent `openaiauth.Manager` now captures credentials and login
generation atomically. `Check` rejects closed, terminal, unpersisted-rotation or
replaced logins; `RefreshCaptured` guards both entry to shared refresh work and
the moment a waiting caller receives its result. Access-token rotation preserves
the login generation. Logout and any installation, including the same account,
invalidate older captures. The manager remains the only credential owner, with
no new registry or persistent fields. Its check has a documented point-in-time
guarantee; it is not atomic with a later HTTP request.

Deterministic race tests cover capture ordered with installation, account and
generation mismatch, logout during exchange, cancelled/coalesced waiters,
replacement after refresh publication, dirty rotated-token persistence and
joined shutdown. The full auth race/shuffle suite passed ten runs in 4.417s,
retained subscription adapter tests passed in 1.742s, and retained daemon
OpenAI/recursive subscription tests passed in 3.259s. Integration passed the auth
race suite in 1.927s, `task check:fast` and `task check:analysis` with zero lint
issues and no reachable vulnerabilities. The active package gates now include
the independent auth package. No host config, schema or public contract changed.

This is a prerequisite for subscription execution. The new runner and provider
still need the captured dispatch guard, separately recorded 401 retry and fixed
subscription wire profile; command lifetime wiring and real-runtime coverage
remain required before claiming that capability complete.

Ancestor revision `a8952622a` has now passed Linux, macOS, analysis and the
aggregate redesign gate in
[run 36470031266](https://github.com/context-labs/whip/actions/runs/36470031266).

## Durable schedules through ordinary inputs

Schedules own their immutable specification and one mutable next-due cursor.
Firing atomically admits an ordinary input and its receipt, charges logical
writes, and advances the cursor. Input provenance and the unique schedule/slot
constraint provide occurrence evidence without a second execution ledger.
Create/fire retries resolve their original identity before changed lifecycle or
cursor checks. Cancellation prevents future occurrences; already accepted
inputs retain their independent lifecycle. Session deletion leaves identity
tombstones and clears payloads.

The runtime scans a bounded global due page even when a session worker is not
loaded. A keyset cursor and an input-ordinal boundary prevent a fast recurrence
from monopolizing one sweep. Recurrence catches up one outstanding occurrence
at a time on its original grid; stopping retains the cursor. Timestamp identity
preserves UTC nanoseconds without relying on UnixNano's narrower date range.
An unrepresentable successor becomes an inspectable blocked schedule rather
than repeatedly admitting or dropping work. Active future schedules consume
ancestor-enforced reusable capacity. Public input admission rejects the internal
`schedule` and `operation` client namespaces so callers cannot occupy internal
receipt identities.

Coverage includes transaction rollback, receipt replay, capacity, exact time
parsing, queued work across process kill/restart, cancellation and deletion,
both guest engines, and generated SDK create/get/list/cancel. The isolated
increment passed its phase and analysis gates. After integration at `e3765de4c`,
`WHIP_SDK_RACE=1 task check:phase` passed: store race tests 86.873s, runtime
91.084s, process engine 101.077s, the full SDK fixture 121.241s (schedules 0.594s),
retained crash fixture 10.509s, and retained daemon acceptance 2.661s.
`task check:analysis` passed with zero lint issues and no reachable
vulnerabilities. Protocol regeneration produced no drift. Logs are
`/tmp/whip-schedules-integrated-phase.log` and
`/tmp/whip-schedules-integrated-analysis.log`. Hosted validation of this
increment remains pending. Fresh config is 7, schema 26, protocol development
major 4.

The Phase 4 plan header was stale after its audit repair passed hosted checks;
it now agrees with the detailed closure evidence. Responses revision
`d39ee7ef7` has also passed Linux, macOS, analysis and the aggregate gate in
[run 36471811463](https://github.com/context-labs/whip/actions/runs/36471811463).
Subscription capture revision `c10e6de1e` has passed analysis; Linux and macOS
remain pending at this checkpoint in
[run 36473179604](https://github.com/context-labs/whip/actions/runs/36473179604).
Goals are not yet implemented, so the combined goals/schedules acceptance
criterion and the rest of Phases 5–7 remain open.

## Subscription model and runner boundary

The `openai-codex` model adapter now shares Responses encoding/decoding and the
ordinary recorded-attempt loop. One host-owned credential manager supplies
ephemeral captures. A guard after reservation rejects stale captures before
ledger dispatch and releases the unused reservation. A second guard immediately
before HTTP prevents requests after a later login change. If the ledger already
recorded dispatch, that later refusal conservatively retains failed/unknown
accounting; it never invents provider zeroes or sends the request again.

A complete 401 settles first. One credential refresh can then replace only the
credential closure, preserving body, digest, route, prices and model; the next
request receives its own attempt and reservation within the existing attempt
limit. A second 401, cancelled/failed refresh, uncertain response or settlement
failure stops dispatch. Recognized hard quota failures remain permanent even
when optional reset metadata is malformed. Safe diagnostics retain no raw
provider body or token.

The fixed subscription endpoint omits a wire output cap. Known retained model
names reserve the pinned natural 128000-token ceiling; unknown names and smaller
explicit caps fail before credential capture. This is retained wire-policy
evidence, not a claim of live model availability. Unknown subscription prices
remain unknown, so finite spend limits fail closed. Private continuation binds
to fixed route, account and model. Token rotation and same-account re-login can
retain that immutable evidence for new authorized requests; login generation
still invalidates previously prepared work. Helper calls use visible content
only.

The isolated slice `6f8625f19`, integrated as `d86bdc093`, passed full model/runner
race/shuffle suites three times, vet and pinned lint. Final tests after hard-quota
parsing refinement passed again. Integrated model/runner/auth race suites passed
in 3.284s/6.735s/2.053s, `task check:fast` passed, and `task check:analysis`
reported zero lint issues and no reachable vulnerabilities. The architecture
gate now permits model to use the independent auth leaf and checks that the auth
package imports no other internal subsystem. Logs are
`/tmp/whip-subscription-core-{race,fast,analysis}.log`.

This checkpoint does not complete subscription support: host-config resolution,
command-owned manager lifetime and real-runtime acceptance are the next slice.
Public authentication/onboarding and client model catalogs remain Phase 6 work.
Config/schema/protocol versions are unchanged by the model/runner slice.


## Subscription host integration and production SDK acceptance

The runtime command now owns one subscription credential manager for its lifetime.
Configuration rejects subscription endpoint and environment-credential overrides;
the fixed adapter owns its wire route. Host model resolution applies the pinned
natural output ceiling before generic defaults and refuses unsupported bounds
before credential capture. Credentials remain lazy: API and scripted routes do
not read the private subscription file. Runtime shutdown finishes before the
manager cancels and joins refresh work.

Both-engine runtime tests cover subscription execution, private continuation
across restart, account changes and stale captures. Command tests cover natural
bounds, lazy credential access and refresh shutdown. The SDK admission scenario
uses synthetic private credentials and a rejecting local proxy; finite unknown
spend exposure fails before any HTTPS connection. Public login, onboarding and
client account catalogs remain Phase 6 work.

The first isolated race-built SDK run failed during context-recovery checkpointing
with `RLM worker memory limit exceeded`. The completed cell output and failed
checkpoint were reported truthfully; this was not a subscription dispatch failure.
An unchanged diagnostic rerun passed, but that does not repair or reclassify the
failure. Sampling that run every 0.2 seconds found QuickJS worker RSS as high as
255.047 MiB against the unchanged 256 MiB production ceiling. The same full SDK
scenarios with the production binary passed in 15.739s; sampled QuickJS RSS peaked
at 67.359 MiB. Samples are observations, not guaranteed process maxima. The parent
enforces total RSS, including race-detector overhead, which explains why this
instrumented executable is unsuitable for production-limit acceptance.

The v4 SDK fixture now explicitly builds with `-race=false` and records that build
mode in its diagnostics. It tests the shipping command and production workers;
the black-box daemon in this fixture is no longer race-instrumented. Required Go
race/shuffle suites remain unchanged, including process-engine coverage and
runtime integration with a race-instrumented host and production workers. The
legacy SDK fixture still honors `WHIP_SDK_RACE=1`. No memory ceiling was raised,
scenario removed, or production test mode added.

After integrating host commit `16f2c2cf4` and the fixture correction,
`WHIP_SDK_RACE=1 task check:phase` passed: store race tests 86.444s, runtime
95.974s, process engine 100.544s, the full v4 SDK fixture 15.512s (subscription
admission 0.202s), retained crash fixture 4.060s and retained daemon acceptance
2.742s. `task check:analysis` passed with zero lint issues and no reachable
vulnerabilities. Logs are `/tmp/whip-subscription-runtime-phase.log` and
`/tmp/whip-subscription-runtime-analysis.log`. Diagnostic evidence remains in
`/tmp/whip-subscription-host-sdk.log`,
`/tmp/whip-subscription-host-sdk-rerun.log`,
`/tmp/whip-subscription-host-rss.jsonl`, and
`/tmp/whip-shipping-sdk-{rss.jsonl,peaks.json}`. Hosted validation of this increment
is pending. Config 7, schema 26 and protocol development major 4 are unchanged.

At this checkpoint, subscription capture revision `c10e6de1e` passed all hosted
checks in [run 36473179604](https://github.com/context-labs/whip/actions/runs/36473179604),
and schedules revision `b3dec1e3d` passed all hosted checks in
[run 36474139342](https://github.com/context-labs/whip/actions/runs/36474139342).
The subscription core run remains pending. Phase 4's audited closure is unchanged;
Phases 5–7 remain open.


## Durable goal records and atomic admission

The goal foundation is integrated as `6e0b2160c` from isolated commit
`415908388`. One row owns immutable objective/allowance and revisioned lifecycle;
current selection derives from latest creation, including terminal goals.
Creation, replacement, queued-input cancellation, logical charges and optional
first input commit together. Resume uses ordinary receipts without resetting
usage. Cancellation identifies only goal-owned work. Owner deletion retains
identity tombstones while clearing text. The generated input projection carries
exact goal ID/revision provenance, and explicit false eligibility overrides
survive configuration resolution.

Tests exercise independent database-handle CAS, exact retries after later
changes/deletion, fault injection at every write, queue/write/byte rollback,
root/child isolation, captured configuration and decimal revisions above 2^53.
The isolated slice passed focused and full scoped race tests, fast/analysis,
contract regeneration and SDK checks. After integration, the complete
`WHIP_SDK_RACE=1 task check:phase` passed: store race 101.233s, runtime 104.835s,
process engine 105.830s, v4 production SDK fixture 15.462s, retained crash fixture
4.286s, and retained daemon acceptance. `task check:analysis` reported zero lint
issues and no reachable vulnerabilities. Regeneration produced no drift. Logs
are `/tmp/whip-goal-admission-phase.log` and
`/tmp/whip-goal-admission-analysis.log`.

Fresh config is 8, schema 27 and protocol development major 4. There is no public
goal command in this foundation. Turn capture, continuation, completion,
formulation and client services remain required, and the combined goals/schedules
acceptance criterion stays open. Hosted validation of this increment is pending.
The plan now also records researched fork/rewind/workspace ownership and explicit
restore semantics; those design entries are not implementation claims.


## Captured goal execution and completion settlement

Goal execution is integrated as `014956444` from isolated commit `8a8e26dfd`.
Ordinary turns capture the eligible armed goal; maintenance compaction does not.
Stale or disabled goal-owned queued inputs are cancelled in a committed cleanup,
so they cannot block work behind them. The runner reads immutable goal context
once and freezes it across correction and compaction. Helpers receive none.

Finish atomically records success and either applies an exact authorized
completion intent or admits one continuation with its durable count/revision.
A savepoint permits semantic admission-limit rejection to pause continuation
without losing terminal turn/accounting evidence; actual SQL faults roll back
for SQL-only retry. An outstanding goal input suppresses duplicates. Failed,
interrupted, cancelled, uncertain or invalid-output execution pauses the captured
goal; resume never resets the allowance. A start=false goal already captured by
an ordinary turn cannot receive another zero-allowance initial run.

Typed completion uses the ordinary operation permission/dispatch/settlement
transaction. Goal completion waits for successful, valid final output and the
unchanged goal, and stores only links to the successful operation and turn.
One-use permission, delegated issuer revocation, exact goal binding, rollback,
replacement/cancellation timing and recovery are covered. No extra intent table
or text-completion heuristic is introduced.

The isolated slice passed full session/store/runner/protocol race/shuffle and vet
(store 111.5s); final goal/output refinements passed their targeted race suite
(17.6s), fast/analysis, generated interop/drift and SDK checks. Integrated
`task check:fast`, `task contract`, `task sdk`, and `task check:fixture` passed;
the v4 SDK fixture took 15.487s and the retained crash fixture 3.875s. Integrated
`task check:analysis` reported zero lint issues and no reachable vulnerabilities
(`/tmp/whip-goal-turns-analysis.log`). The focused and affected-package race
results plus integrated checks precede the full hosted phase gate; this entry
does not claim a second local full-phase race run for identical sources.

Fresh config remains 8, schema is 28 and protocol development major remains 4.
Runtime/guest/RPC/SDK goal controls and formulation remain pending. Hosted
validation of this increment is pending. Subscription core revision `bd5182e21`
has now passed Linux, macOS, analysis and the aggregate gate in
[run 36474934637](https://github.com/context-labs/whip/actions/runs/36474934637).


## Provider idle termination without replay

Idle termination is integrated as `27652d6bc` from isolated commit `3fa5c5e69`.
The adapter retains two-minute Chat and five-minute Responses/subscription idle
limits, separately from the absolute attempt deadline. Headers and positive body
reads advance the deadline. Preparation freezes the optional adapter-only timing
override, including across credential refresh. One owned timer goroutine cancels
stalled HTTP and joins before Execute returns; completion or caller cancellation
also joins it. A confirmed completed response wins a simultaneous idle expiry.

Interrupted I/O returns a sanitized uncertain, non-retryable failure. Already
reported usage and cost survive; absent accounting remains unknown. Provisional
text does not become completed transcript or executable tools. Local HTTP and
synthetic-time tests cover headers, SSE and JSON stalls across all three adapters,
active heartbeat streams beyond the idle period, cancellation/deadline, captured
policy, completed-response races and watchdog cleanup. Runtime tests set
MaxAttempts to three and prove exactly one actual HTTP request, one durable
uncertain attempt, a failed turn, preserved known/unknown accounting, and stable
same-input retries.

The isolated slice passed full model race/shuffle five times (15.489s), full
runner race (6.853s), runtime idle acceptance five times (5.177s), vet and pinned
lint. The integrated `WHIP_SDK_RACE=1 task check:phase` passed, including store
race 103.037s, runtime 100.366s, process engine 100.909s, v4 SDK fixture 15.594s,
retained crash fixture 4.007s and daemon acceptance 2.767s. This combined gate also
covers the preceding captured-goal execution increment. Integrated analysis
reported zero lint issues and no reachable vulnerabilities. Logs are
`/tmp/whip-idle-streams-phase.log` and
`/tmp/whip-idle-streams-analysis.log`. Config8/schema28/protocol development
major4 are unchanged. Hosted validation remains pending.

The plan's feature table now reflects subscription and goal progress without
closing the remaining capability families. Host account controls must reuse the
same command-owned manager to keep login generation and refresh ownership
coherent; their public client implementation remains pending.


## Prepared request output limits for model helpers

The adapter prerequisite is integrated as `052b26b43` from isolated commit
`451d0f54e`. `model.Request.OutputTokenLimit` optionally narrows the valid captured
host ceiling. Chat profiles and API Responses encode the effective limit and
record the same value in the immutable reservation snapshot. Preparation freezes
it even if the caller later changes request pointers or resolver values. A
subscription bound below its natural ceiling fails before credential capture;
equal/wider bounds retain the natural reservation without a wire cap. The scripted
fixture records the bound without claiming tokenizer-based truncation.

Wire/profile, digest, frozen request, private replay, invalid-route/bound and
subscription capture tests passed. Full model race/shuffle passed five times
(15.565s); the final focused tests passed again after lint cleanup (1.395s), and
vet/pinned lint passed. Integrated `task check:fast` and `task check:analysis`
passed with zero lint issues and no reachable vulnerabilities. Logs are
`/tmp/whip-helper-output-limits-fast.log` and
`/tmp/whip-helper-output-limits-analysis.log`. Hosted full-phase validation is
pending. Config8/schema28/protocol development major4 are unchanged.

This changes only the internal model request boundary. Model-helper operation
provenance, dependency settlement, shared runner execution, bounded batches and
public guest/SDK acceptance remain separate work. No new helper execution path
or provider retry loop is introduced by this prerequisite.


## Scheduler fairness acceptance without a latency assumption

The goal-admission and goal-execution revisions failed hosted Linux acceptance
in `TestTurnPermitsBlockedQueuePrefixDoesNotStarveUnrelatedWork`, at the existing
five-second observation deadline. These are failed results in
[run 36476869810](https://github.com/context-labs/whip/actions/runs/36476869810)
and [run 36477264575](https://github.com/context-labs/whip/actions/runs/36477264575);
their macOS and analysis jobs passed. The preceding subscription-host revision
`96f5daef3` passed all jobs in
[run 36476310155](https://github.com/context-labs/whip/actions/runs/36476310155).
The audited Phase 4 revision's passing evidence remains separate from these
later integration results.

A single-CPU race/profile diagnostic of the unchanged failing test passed three
times locally. That pass does not repair the hosted failure. Across the diagnostic,
real scheduler claims consumed 6.14 seconds of sampled CPU, and database connection
waits consumed 9.26 seconds of blocking time. The test repeatedly observed the
same single-connection store while scanning 106 deliberately blocked claims.
This supports removing the test's latency assumption; it does not establish a
production starvation defect or prove the exact hosted timing cause.

The revised test drives the real scheduler, SQLite claims and workers in bounded
passes. The first page leaves unrelated work queued; the second executes it and
wraps the cursor; the next sweep executes earlier work after its cap is raised.
Blocked work remains queued. Workers are joined before inspection and teardown,
with one overall cleanup deadline. Existing live-runtime concurrency, resumption,
cancellation and nested-wait scenarios remain unchanged. No production code,
resource cap or acceptance scenario was removed.

`GOMAXPROCS=1 go test -race -shuffle=on -count=3 -run '^TestTurnPermits'
./internal/runtime` passed in 29.890 seconds. Runtime vet and integrated analysis
passed with zero lint issues and no reachable vulnerabilities. Logs are
`/tmp/whip-scheduler-fairness-race.log` and
`/tmp/whip-scheduler-fairness-analysis.log`. The integrated fast gate also passed
(`/tmp/whip-scheduler-fairness-fast.log`); hosted validation of this repair is pending.


## Public goal controls and authorized completion

Integrated isolated commit `ec67684f8` as `a45cfb708`. Runtime and generated
RPC/SDK controls expose create, current, get, resume and cancel through the same
durable store transitions. Both guest engines dispatch `goals.complete` through
ordinary authority and operation settlement. Its accepted result is an intent;
only successful final turn settlement can complete the unchanged goal.

The SDK process fixture now has a named goals stage. It covers exact/default
allowances, stale creation replay without reselection, disabled eligibility,
lost initial acknowledgement followed by SIGKILL, unclaimed-input cleanup,
dispatched interruption without replay, explicit resume without allowance reset,
and root/child completion through both engines after one continuation. Runtime
tests additionally cover one-use permission consumption, ancestor revocation,
accepted intent followed by invalid output, and cancellation that distinguishes
a goal-owned turn from an independently submitted human turn. Completed goal
evidence and transcript remain stable across restart.

The SDK guide and canonical frontend/domain guides explain the receipt boundary:
initial work uses the returned reserved `goal` identity; caller-owned resume
work uses ordinary SDK wait/recovery. Clients keep neither a second goal state
machine nor a continuation loop. Current selection is not synonymous with armed.

Integrated fast, contract interchange/drift, SDK checks and the production SDK
process fixture passed (16.439s for the full fixture). Focused goal race/shuffle
tests passed across runtime, RPC and protocol. Integrated analysis reported zero
lint issues and no reachable vulnerabilities. Evidence is in
`/tmp/whip-goal-api-{fast,contract,sdk,fixture,race,analysis}.log`.
Fresh config8/schema28/protocol development major4 are unchanged. Hosted
validation is pending. Goal formulation and product-client adoption remain open.


## Stateless-helper accounting and dependency settlement

Integrated isolated commit `6a1f10de0` as `ad4bab54c`. Fresh schema29 records
immutable operation/item provenance on ordinary model attempts. Store admission
validates the dispatched same-owner/same-turn helper operation, tree scope,
strict arguments, item bounds and requested output ceiling. Helper attempts
cannot publish assistant messages or private continuation. Operations wait for
linked attempts before settlement; cells already wait for their operations.
Recovery orders attempts, operations and cells in one atomic transaction, with
known-zero undispatched cancellation and uncertain dispatched work without replay.

Dispatch rechecks current ancestor exposure, counting its reservation once.
Independent database handles exercise a sibling settling above its reservation,
unknown exposure and unchanged retention of already completed output. Provenance
is intentionally not a cascading operation foreign key: child deletion retains
permanent ancestor charges and their origin, while root deletion releases them.
Real-SQL tests also cover authorization/scope/argument rejection, exact retries,
immutable provenance, settlement joins, fault rollback and repeated recovery.

The integrated `WHIP_SDK_RACE=1 task check:phase` passed: store race112.392s,
runtime107.115s, process engine100.985s, full v4 SDK fixture17.463s, retained crash
fixture9.402s and daemon regressions2.739s. Analysis reported zero lint issues and
no reachable vulnerabilities. Logs are `/tmp/whip-helper-ledger-phase.log` and
`/tmp/whip-helper-ledger-analysis.log`. This combined gate also verifies the
preceding public goal controls and scheduler test correction. Hosted validation
of this revision is pending; public helper projections and execution are still
separate work. Config8 and protocol development major4 are unchanged.

Provider idle revision `fa8e28191` passed every hosted job in
[run 36477743920](https://github.com/context-labs/whip/actions/runs/36477743920),
and prepared output-limit revision `a680d8ac3` passed every hosted job in
[run 36477974198](https://github.com/context-labs/whip/actions/runs/36477974198).
The earlier Linux fairness-test failures remain failed evidence.


## Bounded provisional reasoning observations

Integrated adapter/public-observation commits as `6e9b6b5d0` and `a7510d180`
from isolated `022ca1220`/`28e40d63e`. Chat reasoning and Responses/subscription
reasoning-summary deltas now reach `preview.reasoning` through the existing
Runtime/RPC/SDK observation contract. Text, reasoning and tool-call fields share
the existing 128 KiB UTF-8-safe budget; reasoning-only chunks advance revision.
The SDK iterator still retains only cursor/revision and introduces no new cache.

Reasoning fragments never enter completed message parts, private-continuation
projections or later provider requests. Raw framing/event bounds remain in place
without counting discarded reasoning against final-message bytes. Tests cover
interleaving, nil callbacks, malformed/cancelled/failed streams, unknown and known
accounting, shared truncation, retry replacement, settlement retry, final commit,
cancellation and process restart. The real SDK streaming scenario verifies both
reasoning display and its absence from later request/history payloads.

The isolated slice passed observation race/shuffle three times (12.644s), full
model/protocol/RPC race tests, vet, scoped lint, generated contract checks, SDK
checks and its final full process fixture (18.548s). Integrated fast, contract
interchange/drift, all eight SDK tests and the full process fixture passed
(16.493s). Integrated analysis found zero lint issues and no reachable
vulnerabilities. Logs are `/tmp/whip-reasoning-public-{fast,contract,sdk,fixture,analysis}.log`.
Hosted validation is pending. Config8/schema29/protocol development major4 are
unchanged. Product UI adoption remains a later client-cutover obligation.


## Fatal host persistence boundary

Integrated isolated `e38c9508c` as `1e9c6f817`. Prepared external handlers receive
their committed operation ID. The dispatcher turns unresolved accepted-operation
cleanup or settlement errors into `tool.Fatal`; helper accounting can use the
same marker. The process owns interpretation of that Go error interface and
terminates the worker before sending a guest response. There is no guest-writable
fatal frame or string-based classification.

QuickJS cancels before releasing host admission/serialization, prevents queued
calls from starting, and joins all started calls. Both engines return an
unsettled cell and publish no checkpoint. Ordinary settled failures remain
catchable; pre-admission failures invoke no handler. Completed filesystem bytes
and dispatched ledger evidence survive a settlement failure without replay.
Only `models.call/batch` may opt into per-attempt deadlines instead of the normal
30-second handler deadline. The obsolete QuickJS ten-minute whole-cell timer was
removed; guest compute/time, VM job/request, RSS and cancellation limits remain.
Synthetic long host waits and live busy-guest/cancellation tests verify the
remaining boundaries without ten-minute sleeps.

The isolated full tool/process race suites passed (5.046s/111.601s), as did
affected runtime race tests (25.117s), vet, build and pinned lint. Integrated fast,
focused fatal/compute/host-wait race tests (tool 2.218s, process 8.728s), the full
SDK fixture (16.882s) and analysis passed. Analysis reported zero lint issues and
no reachable vulnerabilities. Logs are
`/tmp/whip-fatal-boundary-{fast,race,fixture,analysis}.log`. Hosted validation is
pending. Config 8, schema 29 and protocol development major 4 are unchanged.

The scheduler fairness correction `b2a61bd69` now passes Linux, macOS, analysis
and the aggregate gate in
[run 36478833850](https://github.com/context-labs/whip/actions/runs/36478833850).
The earlier failed goal-foundation/execution runs remain failed results.

## Captured sampling preferences

Integrated isolated `2369dbce3` as `5da92a882`. Nullable temperature and top-p
belong to the complete revisioned model selection, with finite bounds 0–2 and
0–1. Explicit zero survives wire encoding. Model/configuration/request copies
own their optional values; context-pressure comparisons use value equality.
Chat profiles forward the fields, while API Responses and subscription routes
reject explicit values before credential resolution or HTTP dispatch.

This deliberately replaces legacy per-route `samplingParams` fallback with
captured host/definition/override configuration. Replacing a model clears omitted
sampling preferences. Compaction and other helpers use their complete captured
selection, including effort and sampling; legacy helper calls omitted both.
No sampling setting changes continuation authorization scope or creates a second
SDK settings cache. Fresh host config is now 9; schema 29 and protocol major 4
are unchanged.

The isolated domain/config/model/runner/protocol and store/RPC/command race suites,
vet, lint, generated interchange and SDK checks passed. Integrated fast, contract,
SDK and analysis gates passed; analysis reported zero lint findings and no
reachable vulnerabilities. The new real-HTTP runtime test passed race/shuffle
three times (6.731s): changing settings during a blocked request preserves its
retry, affects the next turn, survives restart, is inherited by a child, and is
cleared by whole-model replacement. The full SDK process fixture passed (16.438s),
including explicit-zero sampling through generated validation, RPC and HTTP.
Logs are `/tmp/whip-captured-sampling-{fast,contract,sdk,fixture,analysis}.log`.
Hosted validation is pending.

The public-goal revision `bbcf9ac52` now passes Linux, macOS, analysis and the
aggregate gate in [run 36479306726](https://github.com/context-labs/whip/actions/runs/36479306726).
The provider inventory confirmed that all retained inference protocols have
adapters. Remaining provider scope is account lifecycle (including Inference.net),
credential sources, presets, account-scoped catalogs/pricing, readiness/defaults,
client adoption and a separate live-provider smoke. Phases 5–7 remain open.


## Stateless model helpers through durable operations

Integrated runner `3fa2a4828`, owner-scoped content publication `19294e6bf`,
public attempt provenance `65ee1e514` and runtime binding `07c6d2392`.
`models.call` and ordered `models.batch` use the common prepared-attempt, retry
and SQL-only settlement path. Each helper captures the active turn's complete
model selection, including effort and sampling, and receives one prompt without
conversation history, tools, instructions or private continuation. Batches admit
1–32 items with four concurrent requests. Cancellation drains started work;
unresolved accounting errors cross the fatal host boundary even if guest code
tries to catch them.

Successful large output becomes immutable session-owned content after billing
settles. Bounded UTF-8 previews and encoded-size checks keep a maximum batch
inside the operation limit. Publication or registration failure reports unavailable
output without losing its charge or sending the provider another request.
Attempt projections expose nullable operation and batch-item provenance.

Both engines have runtime coverage for captured configuration, strict arguments,
one-use permission, ordering, retries, budget refusal, escaped maximum batches,
content access/isolation/restart/deletion, cancellation and SQL/blob failures.
The SDK process scenario runs roots and inherited children through a real local
HTTP adapter. SIGKILL after one batch item settles retains its known charge,
marks dispatched unfinished work uncertain and sends no replacement requests
on restart. A runner regression also mutates the caller's sampling pointers
after the first worker starts and verifies later items keep the captured values.

Integrated `task check:phase` passed: store race116.070s, runner11.418s,
runtime139.458s, process111.221s, full v4 SDK fixture21.531s, retained crash
fixture3.735s and selected daemon regressions2.780s. Separate focused runner/runtime
race checks, generated contract interchange/drift and SDK tests passed.
`task check:analysis` reported zero lint findings and no reachable vulnerabilities.
Logs are `/tmp/whip-stateless-models-{phase,analysis,fixture,contract,sdk,runtime}.log`.
Fresh config9/schema29/protocol development major4 are unchanged. Hosted
validation of this helper increment remains pending.

The preceding helper ledger (`0aea348d9`), reasoning observations (`f837af58d`),
fatal boundary (`b6cd1728a`) and captured sampling (`3894a8fe3`) revisions now
pass all hosted jobs in runs
[36479828700](https://github.com/context-labs/whip/actions/runs/36479828700),
[36480074108](https://github.com/context-labs/whip/actions/runs/36480074108),
[36480736937](https://github.com/context-labs/whip/actions/runs/36480736937) and
[36481765530](https://github.com/context-labs/whip/actions/runs/36481765530).
Phase 5 remains in progress; phases 6–7 remain pending.


## Durable goal formulation and public candidate evidence

Integrated store `3948c6d3f`, runner `b5fe702e3` and public API `fdb8348f9`
from isolated releases `0b483350c`, `c97bf3b4c` and `6164c6d84`.
A formulation request freezes a bounded raw-history window at admission and
uses its ordinary turn's main configuration at claim. It shares the existing
helper attempt/retry/SQL-settlement path, with no conversation message, engine
cell, mail delivery, content hydration, output contract or private continuation.

Billing, a valid candidate, conditional goal activation and optional ordinary
initial input commit together. A savepoint preserves billed candidate evidence
when activation is semantically refused; SQL failures roll back all settlement.
Exact settlement replay never reactivates an earlier rejection. The goal retains
an immutable origin attempt and candidate inspection reports original acceptance,
independently of later cancellation, replacement, child deletion or an interrupted
maintenance turn. Full tree deletion still removes its accounting evidence.
Generated RPC and SDK APIs use ordinary caller-owned maintenance receipts and
bounded owner/attempt candidate reads. Fresh schema30 replaces schema29; config9
and development protocol major4 remain unchanged.

The real SDK process scenario covers roots and children under both engine
configurations using a local HTTP provider: dropped admission acknowledgements,
exact replay versus changed-payload conflict, empty maintenance history, captured
sampling, exact decimal allowances, one billed candidate, rejected activation,
independent initial goal input, owner isolation, child deletion and SIGKILL
while the provider is dispatched. Restart preserves uncertain accounting without
creating a candidate or replaying the call. The full fixture passed separately
in 23.599s and again inside the phase gate in 22.626s (formulation stage0.667s).

The integration gate initially found one constructor call in the preceding
helper-runtime tests that needed the new formulation store argument; that call
was updated before the final checks. Focused race/shuffle tests passed across
store8.164s, runner5.513s, runtime2.381s, RPC3.118s and protocol2.627s. Generated
contract interchange/drift, SDK checks and `go build ./...` passed. Independent
read-only review found no blocker in settlement replay, lifecycle, source
ownership or continuation isolation.

`task check:phase` passed: store race123.074s, runner13.868s, runtime140.880s,
process111.140s, retained crash fixture2.277s and daemon regressions2.812s.
`task check:analysis` reported zero lint findings and no reachable vulnerabilities.
Logs are `/tmp/whip-goal-formulation-{phase,analysis,race,contract,sdk,fixture,build}.log`.
Hosted validation remains pending. The backend goals/schedules acceptance item
now has replacement evidence; broader Phase5, product clients and final cutover
remain open.

The retained-title audit also identified first-authored-text fallback naming,
manual ownership, a single bounded helper request and catalog refresh obligations.
The plan now records its proposed maintenance ownership and deliberate timing,
recovery and adapter-ceiling changes. Automatic naming is still unimplemented;
explicit tree metadata alone is not counted as replacing it.


## Session-owned opaque content identities

Integrated isolated `f9c82bd86` as `0780f80c4`. Content references now have the
composite identity `(owner_session_id, reference_id)`, while content bodies still
deduplicate by digest. Registration, reads and exact retries include the owner;
permanent logical-write identities encode both owner and reference so a second
owner cannot accidentally avoid its charge. Mail revisions constrain their
recipient through the canonical mail row and their evidence through the same
recipient/reference pair. This keeps the additional FK coordinate derived and
prevents independent ownership drift.

Real-SQL tests cover identical handles with shared or different bodies, independent
retry/conflict/charge behavior, charge-refusal rollback, reopening and deletion,
forged mail recipient/evidence (including absent evidence), cleanup constraints
and automatic completion/helper references already occupied by another owner.
The SDK process scenario verifies two owners with different bytes under the same
handle, an unrelated owner's denied access, exact retry, conflict, restart and
independent deletion. Explicit child/mail sharing retains its recipient aliases;
actual fork, history editing and REPL invalidation remain separate work.

The isolated full store/session race suites passed (125.116s/5.129s), as did
focused actual runtime/RPC acceptance, vet and pinned lint. Integrated fast checks
passed; focused store/runtime race suites passed (40.210s/50.343s), followed by the
full RPC race suite. The full v4 SDK process fixture passed in 26.875s, including
the new ownership stage in 0.092s and all prior scenarios. Analysis reported zero
lint findings and no reachable vulnerabilities. Logs are
`/tmp/whip-content-owners-{fast,race,rpc,fixture,analysis}.log`.
Fresh schema31 replaces schema30; config9 and development protocol major4 stay
unchanged. Hosted validation is pending. This prerequisite does not close the
fork/rewind acceptance item or Phase5.


## Host-owned ChatGPT account controls

Integrated config authority `621b3d145`, account service `d54bf1b82` and public
RPC/SDK wiring `847500fab`. One command-owned credential manager supplies both
model authorization and a separate host account service; sessions and the runtime
do not own onboarding. Seven generated `accounts.openai` operations expose
begin/get/list/cancel/status/setup/logout with bounded safe projections and exact
nullable expiry strings. The SDK owns no account cache or automatic polling.

Login is accepted before device HTTP and survives client disconnection. One
active flow is deduplicated, retained records are bounded, old process identities
report interrupted, and cancellation/logout/shutdown join owned requests.
Credentials are stored before fixed-route setup. Setup is explicitly retryable,
preserves defaults and custom routes, and compares freshly read host-file bytes
through an anchored config authority. Status describes local credential and
routing evidence; it is not a claim of working model access or live connectivity.

Persistence review found two gaps before publication: failed logout could later
look signed out despite remaining saved credentials, and a failed token save
could look fully persisted. The real SDK fixture first reproduced the logout
bug (`signed_out` instead of `unavailable`). Repairs `09eb28c15` and `591ca1973`
track publication separately from durability, invalidate old login captures when
replacement becomes visible, preserve rotated tokens for local-only save retry,
and keep failed logout unavailable until explicit retry. First load confirms
directory durability before trusting saved credentials or absence; failure
blocks authorization and remains locally retryable. No second token exchange is
used to repair a failed local save.

The SDK process scenario uses only synthetic credentials and disposable files.
It covers signed-out/expired-saved status, exact metadata without secrets,
interrupted old flows, idempotent setup, unchanged defaults, conflicting routes,
failed removal, explicit recovery, lost logout acknowledgement and restart.
The extended fixture passed after the repair in 26.588s, including the account
stage in 0.209s. Analysis passed with zero lint findings and no reachable
vulnerabilities. Device-flow tests intercept HTTP; no real account was changed.
The final `task check:phase` passed: store race170.817s, runtime185.374s,
process147.618s, account15.597s, auth3.440s, generated contracts and SDK checks,
v4 process fixture23.920s, retained crash fixture4.376s and daemon regressions2.833s.
Logs are `/tmp/whip-host-accounts-{phase,analysis,durability-fixture}.log`;
the pre-fix reproduction is `/tmp/whip-host-accounts-before-durability.log`.
Hosted Linux, macOS, analysis and aggregate validation passed at `07641b736`
in [run 36487199877](https://github.com/context-labs/whip/actions/runs/36487199877),
verified again before this continuation began.

The preceding stateless-model and formulation increments also passed hosted
Linux, macOS, analysis and aggregate gates at their exact heads:
`b2a5be867` in [run 36483835552](https://github.com/context-labs/whip/actions/runs/36483835552)
and `c47fefe96` in [run 36484832748](https://github.com/context-labs/whip/actions/runs/36484832748).
The owner-scoped content increment `0ce307807` likewise passed all hosted gates
in [run 36485162275](https://github.com/context-labs/whip/actions/runs/36485162275).

This is a checkpoint within Phase5. The original Phase4 audit remains closed at
`1244d7cd2`, whose passing hosted checks were reconfirmed against the original
seven criteria. Phase5 is incomplete; account catalogs, other credential sources,
Inference.net onboarding, automatic titles, fork/rewind/workspace behavior and
remaining integration families still require work. Phases6–7 remain pending.
The redesign is an unmerged draft stack; the original development checkout has
not adopted it and retains the original proposed plan.

At the user's request to finish the current task, the following independent work
was saved in clean isolated commits without integrating it into this account PR:

| Checkpoint | Commit and location | Validation and remaining work |
| --- | --- | --- |
| Immutable history groups and revision foundation | `762f8451b0321644fb4c7023a4205f4155a28f66`, `/Users/samheutmaker/.codex/worktrees/history-foundation/whip` | Full store/session races, targeted runtime checks, build/vet/lint passed. Fresh schema32. Still needs integration and public projections; imported runner context remains work for fork. |
| Atomic conversation rewind and revision-scoped REPL reset | `0651d734920ad5ddcaf65bd9993d6f3b572bc65a`, `/Users/samheutmaker/.codex/worktrees/rewind/whip` | Full store/session/runtime races204.362s/7.505s/211.327s, both engines, build/vet/lint passed. Apply the history foundation first, then this commit only. RPC/SDK exposure, fork and workspace restore remain open. |
| Independent Inference.net credential manager | `3e04bfcae8e2813ed7aa5587a250913cd4f5b80d`, `/Users/samheutmaker/.codex/worktrees/inference-auth/whip` | Race/shuffle, 20-run stress, vet, Linux compile and lint passed. Management credentials and inference keys have separate lifetimes. Integration, device/team/project/key HTTP flows, managed provider binding and public API remain open. |

These are local checkpoints, not completed product features or published PRs.
No next implementation slice was started after finishing this checkpoint.


## Revision-aware history and public rewind

Continuation started from the clean verified account head `07641b736`, preserving
all unrelated changes in the original development checkout. Reused saved history
foundation `762f8451b` as `124054b07` and rewind `0651d7349` as `545983a27`;
no duplicate branch ancestors were imported. Fresh storage is now schema32,
with config9 and development protocol major4 unchanged in this slice.

Atomic history pages (`51aa86007`) read current revision, active maximum sequence,
active count and bounded message bodies from one SQLite statement. An expected
revision rejects stale history, metadata/search and observation cursors before
returning a replacement page. Appends retain the revision and advance sequences;
rewind advances the revision, including keep-all or empty edits. Provisional
previews carry their captured history revision and cannot cross into a new one.

Focused store/runtime history and observation checks passed, followed by focused
race/shuffle checks (store3.258s/runtime2.483s). Public protocol/SDK exposure (`98701fd2a`, isolated `7ce6270c7`) adds `sessions.rewind`,
revision-bearing pages/observation and explicit imported execution/provenance
fields. RPC tests exercise both roots and children; SDK observation resets its
cursor on revision conflict and emits an empty replacement when applicable.
A resumed SDK observation requires the saved revision with its nonzero cursor.
Focused protocol/RPC races passed2.862s/10.969s; 15 SDK tests and seven generated
interchange/strict-CSP tests passed, including clean generation drift and focused
build/vet/lint.

The complete real-process SDK fixture passed25.530s; its new rewind stage passed
1.802s. Both engines and roots/children exercise lost edit acknowledgements,
SIGKILL/restart after commitment, exact retry after later work, stale tail/revision,
split-group rejection, retained exact evidence, non-reused sequences and empty
REPL restoration. The integrated `task check:phase` passed: store race167.062s,
runtime173.291s, process126.962s, RPC14.874s, runner15.355s, account14.857s,
model11.990s, plus generated contracts, SDK checks and remaining package races.
Its v4 process fixture passed25.179s, retained crash fixture3.004s and selected
daemon regressions2.825s. `task check:analysis` passed with zero lint findings
and no reachable vulnerabilities after correcting a multiline literal's format.
An independent review found no blocking defect in the integrated history path.
Logs are `/tmp/whip-history-controls-{phase,analysis,fixture}.log`.
Hosted Linux, macOS, analysis and aggregate validation passed at `41bb99a5e`
in [run36494846463](https://github.com/context-labs/whip/actions/runs/36494846463).
Broader Phase5 acceptance remains open.

The retained-feature audit also makes these Phase5 obligations explicit: human
questions and permission modes, file list/search/LSP behavior, definition modules
and capability surfaces, executor tools/required versus optional hooks, MCP
configuration/import/refresh, shell jobs versus human PTYs, browser/computer/native
resources and host/gateway trust. Existing operation approvals and file read/write/
patch alone do not replace those families. Every supported client and final core
removal remain required under Phases6–7.


## Managed Inference.net credential binding

History and rewind are published in draft [PR237](https://github.com/context-labs/whip/pull/237)
at `41bb99a5e`; hosted validation subsequently passed in run36494846463. The next isolated stack increment
reuses saved credential-manager checkpoint `3e04bfcae` via clean tested leaf
`f054248c2` as `53dd639dc`, then integrates managed binding `296e5cee8` as
`797e800b1`. Fresh configuration advances to version10; schema32 and development
protocol major4 are unchanged.

The command owns the manager after acquiring the runtime directory and injects it
before starting execution. Loading is lazy, so unused malformed Inference.net
credentials cannot block scripted or other-provider startup. Explicit managed
routes accept only the pinned chat gateway and exclude environment credentials.
Prepared requests capture one machine key/generation; replacement and logout
invalidate later checks without exposing management tokens or keys in SQL.
Publication, pending durability, local-only persistence retry and failed logout
retain the checkpoint's exact ownership semantics. Onboarding/public HTTP account
flows, other credential sources and account catalogs remain open.

The isolated leaf passed race/shuffle for inferenceauth1.961s/config3.252s/
model11.720s/command4.734s, architecture checks0.524s, focused build/vet,
pinned lint with zero findings and a Linux amd64 command build. Fixtures use
synthetic credentials and intercepted HTTP, with no real account changes.
Integrated `task check:phase` passed: store race157.111s, runtime170.583s,
process114.661s, inferenceauth4.505s, config5.140s, model8.932s, command7.428s,
RPC13.725s and remaining package races, generated contracts and SDK checks.
The v4 process fixture passed26.418s, retained fixture2.866s and daemon
regressions2.771s. `task check:analysis` passed with zero lint findings and no
reachable vulnerabilities. Logs are `/tmp/whip-managed-credentials-{phase,analysis}.log`.
Hosted Linux/macOS/analysis checks subsequently passed at `f1382213c` in
run36495409570. No installed runtime was changed.


## Explicit API credential sources

Managed Inference.net binding is published in draft
[PR238](https://github.com/context-labs/whip/pull/238) at `f1382213c`, stacked on
history [PR237](https://github.com/context-labs/whip/pull/237). Both remain unmerged.
The next increment integrates clean leaf `81d22f963` as `d153095b1` and advances
fresh configuration to version11, with schema32/protocol major4 unchanged.

Generic API routes now resolve an explicit environment, canonical private file,
bounded command or no-auth source. The existing environment/no-auth shorthand is
preserved without introducing implicit discovery. Source validation rejects
ambiguous declarations, API credentials on subscription routes, and managed
Inference.net credentials at another gateway. File reads anchor every path
component; commands use a ten-second limit, 64 KiB combined output, explicit
inherited environment names and joined process-group cleanup. Both adapters
capture credentials once at preparation across retries and rotation/deletion.

The isolated leaf passed unit checks config3.028s/model1.786s/command2.079s,
focused races config3.712s/command1.551s, vet, pinned lint and diff checks. Tests
cover cancellation, deadline, surviving descendant pipes, environment isolation,
private files, safe errors and frozen request evidence. Integrated phase and
analysis gates passed: store race155.454s, runtime169.207s, process115.860s,
config4.984s, command7.125s, model10.424s, RPC18.262s and other package races,
generated contracts and SDK checks. The v4 process fixture passed25.333s,
retained fixture2.620s and daemon regressions2.743s. Analysis found zero lint
issues and no reachable vulnerabilities. Logs are
`/tmp/whip-credential-sources-{phase,analysis}.log`. Hosted Linux/macOS/analysis
checks subsequently passed at `1dd685670` in run36495956816.
Pasted/named-key publication, catalogs, readiness,
route/default setup and product controls remain open; no real credentials or
installed runtime were changed.


## Bounded conversation fork and imported context

Explicit credential sources are published in draft
[PR239](https://github.com/context-labs/whip/pull/239) at `1dd685670`, stacked on
[PR238](https://github.com/context-labs/whip/pull/238). The next main increment
integrates fork core `4742f0d5d` as `31c765e4e`, imported runner context `ab91f78c9`
as `bfab4c316`, and public integration `30bb54554` as `5cef4be84`.
Fresh schema advances to33; config11/development protocol4 stay unchanged.

Fork is an atomic bounded import with exact history/config/tail comparison,
whole terminal group boundaries, immutable receipts and destination tombstones.
It copies effective configuration, pinned definition/engine, raw groups/messages,
authorized opaque handles and compatible summaries/pins with explicit provenance.
It creates no execution identities, authority, checkpoints or spending. Shared
runner context now uses history groups and opening-input markers for imported
exchanges. Destination budgets are fresh; bounded initial import has no historical
logical-write charge, matching fresh-tree admission. Working-directory reuse and
empty REPL semantics are explicit; workspace effects remain separate work.

Core store race passed168.478s; focused store race16.277s plus build/vet/pinned
lint passed. Imported runner tests passed4.291s after first reproducing dropped
pins/split imported groups. Public integration races passed runtime7.006s,
RPC3.951s/protocol1.297s, and focused vet/lint passed. Generated interchange/CSP/
drift checks and16 SDK tests passed. Independent review found no blocking defect.

The full SDK process fixture passed25.259s, including fork0.646s across both
engines and root/child sources. It drops fork acknowledgements, kills/restarts the
runtime, checks null imported execution links, deletes source owners, verifies
opaque content after startup collection, continues local execution and forks
again before checking deletion tombstones. An initial fixture failure used a
parent handle after child admission had aliased it; authorizing the opaque handle
in the actual child scope repaired the test without changing production code.
Separate both-engine tests prove empty REPL after restart, retained source globals
and old retry preserving destination globals. Integrated phase/analysis gates
passed: store race175.402s, runtime179.862s, process118.085s, runner17.896s,
RPC20.170s, protocol7.033s and remaining package races, generated contracts and
SDK checks. The integrated v4 fixture passed25.417s, retained fixture2.893s and
daemon regressions2.725s. Analysis found zero lint issues and no reachable
vulnerabilities. Hosted Linux/macOS/analysis checks subsequently passed at
`3577b5848` in run36496511586; logs use `/tmp/whip-forks-{phase,analysis}.log` and
`/tmp/whip-fork-public-{focused,race,fixture,contract,sdk,lint}.log`.


## Host-owned Inference.net onboarding and cleanup

Forking is published in draft [PR240](https://github.com/context-labs/whip/pull/240)
at `3577b5848`, stacked on [PR239](https://github.com/context-labs/whip/pull/239).
The next increment integrates account service `c5ad86354` as `d24947dff` and public
RPC/SDK leaf `4f5c3ad6b` as `8641957d4`. Fresh schema33/config11 and development
protocol4 are unchanged. The active gate now includes `internal/inferenceaccount`.

The command owns one bounded account service borrowing the existing private
manager and configuration authority. Device approval and explicit team/project
selection use pinned control-plane HTTP with no redirects/cookie jar. Only
singleton choices advance automatically. Project creation and rotation are
explicit; remote uncertainty never authorizes automatic creation replay. Durable
key publication precedes route setup/old-key archival, and known local failures
retry their saved step. Management expiry remains independent of machine-key
use. Newly approved account credentials cannot inherit a previous account's key.

Fourteen generated RPC operations and SDK methods expose safe flow/status/cleanup
projections. HostServices carries borrowed command-owned services; no new manager
or session receipt owner was added. Setup preserves model defaults/custom routes.
Logout revokes local authority before remote cleanup and returns each outcome
separately. Cleanup retries use only prior saved authority; 64-entry/15-minute
process retention and restart never imply remote success. No real account or
installed runtime was changed.

The core checkpoint passed races2.478s, repeat checks4.438s, vet/lint and Linux
build. Public affected-package race/shuffle passed inferenceaccount2.003s,
RPC12.374s, protocol3.567s, config4.658s and command4.352s. Architecture race
passed1.520s; vet and pinned lint passed with zero findings. Generated contract
interchange/CSP/drift checks and17 SDK tests passed. SDK process acceptance
passed25.735s, including Inference account projections143ms with synthetic
machine-only credentials, route conflicts, a lost logout acknowledgement and
restart. Intercepted socket tests cover device-flow delivery loss, stable choices,
cancellation, cleanup failure/retry and secret exclusion. Initial test corrections
added bounded fixture shutdown, registered cleanup as its actual response root,
kept long metadata bounds compatible with browser standalone validators, and
included the new management-authorization error in the generated enum.
Integrated phase/analysis gates passed: store race165.251s, runtime172.081s,
process110.133s, config4.790s, inferenceaccount5.360s, RPC17.435s and all remaining
active-package races, generated contracts and SDK checks. The v4 process fixture
passed25.449s, retained fixture2.886s and daemon regressions2.745s. Analysis found
zero lint issues and no reachable vulnerabilities. Hosted Linux/macOS/analysis/
aggregate checks subsequently passed at `11443df57` in run36497852698.
Logs use `/tmp/whip-inference-accounts-{phase,analysis}.log` and
`/tmp/whip-inference-public-{race,boundaries,contract,sdk,fixture,lint}.log`.


## Durable scoped workspace snapshots and restore

Inference.net onboarding is published in draft
[PR241](https://github.com/context-labs/whip/pull/241) at `11443df57`, stacked on
[PR240](https://github.com/context-labs/whip/pull/240). The workspace increment
integrates core `3ae273938` as `bb37bc70a` and public leaf `984972272` as `37ed0004c`.
Fresh schema advances to34; config11/development protocol4 are unchanged. The
active gate adds `internal/workspace` with explicit import-boundary checks.

Human workspace actions have their own SQL ledger, with no fake turn/cell
identities. Exact request retries resolve before current lifecycle/path checks.
Atomic idle-owner claims block ordinary execution admission while the external
workflow runs. Capture records a private Git object before CAS pin publication;
restore overlays only captured directory paths, with explicit tracked/untracked/
later-file/staging limitations. Worktree/gitdir/scope identities and expected pins
are revalidated. Pins survive restore and must be released before owner deletion.
Claimed restart and partial failures stay uncertain and never automatically replay.
Process groups, cross-runtime writer lock, output bounds and joined shutdown
replace unowned Git subprocesses. Human/other-session writers remain independent.

Six RPC/SDK methods expose action/snapshot metadata and bounded pages without
private paths/objects. Production process acceptance drops capture/restore/release
acknowledgements, observes exact actions, verifies scoped overlay, restarts with
SIGKILL and retries after owner deletion. Canonical docs distinguish workspace
restore from conversation rewind and state the limits truthfully.

Core race/shuffle passed session5.310s/store174.634s/workspace4.587s. Runtime
behavior tests passed177.983s; an initial sole import-allowlist failure was corrected
and its focused race passed1.543s. Final focused runtime race10.805s and adapter
race4.827s passed, as did build/vet/Linux adapter build/pinned lint. Public focused
races passed protocol2.999s/RPC4.516s/store7.349s/runtime14.264s, with final protocol/
RPC retest1.276s/4.020s after lint-only changes. Generated interchange/drift,
16 SDK tests, build/vet/lint and full production fixture28.127s passed; the workspace
stage took2.094s. Parent reviewed core and public surfaces; the earlier path-output
fix preserves trailing-space directory names and exact Git ref matching.
Integrated phase/analysis gates passed: store race177.643s, runtime186.105s,
process112.211s, workspace10.358s, RPC21.497s and all other active-package
races, generated contracts and SDK checks. The production fixture passed26.587s
(workspace1.251s), retained fixture3.161s and daemon regressions2.771s. Analysis
reported zero lint issues and no reachable vulnerabilities. Hosted validation is
pending. Logs use `/tmp/whip-workspace-{phase,analysis}.log`. Product-client adoption, automatic titles
and the other unresolved Phase5 families remain work; Phases6–7 are not complete.


## Durable automatic root naming

Workspace controls are published in draft
[PR242](https://github.com/context-labs/whip/pull/242) at `7feeb4128`, stacked on
[PR241](https://github.com/context-labs/whip/pull/241). Automatic titles integrate
core `dbfae79cb` as `ae1cf21de` and public leaf `ef695851e` as `302e58761`.
Fresh storage/configuration advance to schema35/config12; protocol major4 remains.
Integration preserved workspace claims and tables, all account projections and
all production fixture stages; generated validators were regenerated from Go.

Initialization atomically records one decision and the immediate authored-text
fallback. Manual same-value/clear edits permanently own naming, while attachment-
only roots remain eligible. The captured helper uses an ordinary independent
maintenance input and shared accounting, one attempt/20-second deadline, and
no transcript/instruction/tool/continuation/output side effects. Human admission
still succeeds at queue capacity one. Billing, validated candidate and whole-tree
metadata CAS settle together; manual edits can supersede application without
losing charges. Pending intent survives restart; attempted naming never replays.
Generated RPC/SDK expose policy and immutable decision/result evidence, keeping
selected naming solely in tree metadata.

Core full race/shuffle passed session5.625s/store174.069s/runner16.656s/
runtime168.659s/config2.996s, plus focused checks, build/vet and pinned lint.
Public focused races passed protocol3.003s/RPC11.757s; focused domain/store title
checks passed0.239s/0.737s. Generated interchange/CSP/drift,18 SDK tests,
build/vet/lint and full production fixture26.5s passed. Its title stage453ms
uses both engines, queue capacity one, lost admission acknowledgement, a
foreground SIGKILL before naming, captured source/selection/sampling, late manual
clear, exact1200-nano billing and no history contamination or replay. Parent
reviewed both checkpoints. Integrated phase/analysis gates passed after the
repairs documented below; the first run remains recorded as failed. Logs use `/tmp/whip-title-{phase,analysis}.log`. Off-page title observation and
product-client adoption remain Phase6 work.


The first integrated title phase run failed the existing workspace shutdown
regression after30 seconds; its other active-package races passed (store228.913s,
runtime228.662s, process119.935s). A focused12-run reproduction failed twice and
showed a sleeping descendant left in the owned process group. The first group
signal can race a fork; shutdown previously waited until the command deadline
sent another signal. Shared process shutdown/cancellation now repeat the group
signal until its reaper confirms disappearance, with no signal after group
ownership ends. Confirmed disappearance wins over transient Darwin signal errors
while a child exits. Twenty workspace repetitions passed4.354s;20 root-isolation/
concurrent-fork cancellation repetitions passed1.547s. The earlier first repair's
transient EPERM failures remain recorded in `/tmp/whip-capability-race.log` and
`/tmp/whip-process-close-isolation.log`. `internal/capability` joins active gates.

Parent review also reproduced a title queue ordering defect: decision SQL time
used nanoseconds while ordinary admission uses microseconds. Persisting the
shared unit restores admission-order traversal; the regression checks paging
from pending title intent to a later independent input. Final focused title/queue
races passed store8.764s/runtime2.784s. The final phase/analysis gates passed with both repairs in `50ed5f20f`: store
race199.722s, runtime196.032s, process116.750s, capability31.639s, workspace11.796s,
RPC23.545s and all other active-package races, generated contracts and SDK checks.
Production fixture27.186s, retained fixture4.339s and daemon regressions2.706s
passed. Analysis reported zero lint issues and no reachable vulnerabilities.
Hosted validation is pending. Final logs are `/tmp/whip-title-{phase,analysis}-final.log`, and focused
before/after evidence is `/tmp/whip-title-clock-{before,after}.log` and
`/tmp/whip-workspace-close-{before,after}.log`.


## Explicit provider setup, catalogs and live new-root defaults

Automatic naming and the two integration repairs are published in draft
[PR243](https://github.com/context-labs/whip/pull/243) at `d4aeddf2f`, stacked on
[PR242](https://github.com/context-labs/whip/pull/242). This increment integrates
provider core `7889491a7` as `53bfa7a71`, shared authority/admission `8b610d806`
as `f6db6f706`, and public leaf `1e38fb2c2` as `ea1b03b5e`. Schema35/config12/
development protocol4 remain unchanged. Active gates and architecture checks now
include `providerhost`; the borrowed process capability leaf is checked explicitly.

Eleven provider operations cover retained presets/bundled metadata, safe inventory,
revisioned route/default/compaction changes, scoped catalog read/explicit refresh
and honest readiness. Stable private key publication precedes configuration CAS;
known local retries do not mint another key, and ambiguous orphans are retained.
Catalog scope includes route and credential/account generation, rejects late old
responses, retains same-scope failure evidence and clears on successful empty
responses. Exact nullable prices, efforts, modalities and limits survive public
projection. Uncatalogued explicit selection intentionally remains valid after
cache loss, replacing legacy catalog-membership validation; this is configuration,
not proof of inference readiness. No real provider/account was contacted.

The runtime now creates one authority and the command borrows it. New roots read
fresh host defaults/resources while retained session configurations remain
unchanged. Fork retry/tombstone lookup precedes invalid current host declarations;
new forks still validate current resources. Startup instruction registries retain
their documented snapshot policy. Standalone validator generation now embeds a
native Unicode code-point counter for Ajv's CommonJS string-length helper, checks
it against the pinned helper (including unpaired surrogates) and rejects unresolved
runtime imports. Strict-CSP execution remains required.

Core focused races passed providerhost2.589s/config4.679s; parser refinement,
vet/Linux build/pinned lint passed. Shared-authority focused runtime/default/fork
checks passed0.606s, store fork1.483s and command1.827s. Public focused final races
passed RPC21.930s, protocol2.606s, runtime12.502s and store14.408s; normal full
RPC5.740s/protocol0.979s/command1.704s passed. Contract TypeScript,8 strict-CSP/
Go interchange tests/drift and21 SDK tests passed; vet, Linux build, pinned lint0
and diff checks passed. Production fixture32.329s covers lost save acknowledgement,
private stable key publication, exact9007199254740993/zero/null prices, current
new-root defaults, unchanged old sessions, invalid-host exact fork retry, failed
catalog retention and successful-empty clearing. Boundary tests cover128routes
near the host cap and1024models near2MiB, with oversized escaped results rejected
without clearing cache. Parent reviewed core/public and authority boundaries.
Integrated race/shuffle passed store189.008s/runtime190.952s/process113.889s,
providerhost5.473s/config7.274s/RPC43.792s and all other active packages. Analysis
reported zero lint issues and no reachable vulnerabilities. The full phase gate passed: production fixture27.737s, retained fixture2.224s
and daemon regressions2.810s, plus generated contracts and SDK checks. Logs use
`/tmp/whip-provider-{phase,analysis}.log`. Product provider/account UI and the other
unresolved Phase5 families remain work; Phases6–7 are not complete.

Hosted Linux, macOS, analysis and aggregate checks for PR242 at
`7feeb41282777522f696fc6ad945db1ff11b0cfb` all passed in run36498599944.
PR243 hosted analysis passed; platform jobs remain in progress at this checkpoint.


## Durable human questions

Provider integration is published as draft [PR244](https://github.com/context-labs/whip/pull/244)
at `cb996cbf376ce88b8c4d25ec5723f80f1a456f7a`, stacked on PR243. This increment
reuses question core `5d9ab30e9` as `790355bc0`, public `12cc3c957` as `011e7bcdd`,
and declaration-bound `dd33f3be8` as `179ce1ad6`. Fresh schema36/config12/
development protocol4 apply. The provider Unicode validator helper is preserved;
TypeScript generation limits tuple expansion to4 while Go and standalone
validators retain exact runtime collection bounds.

Root-only single/batched questions now have one operation-owned request/result,
fixed deadline, explicit dismissal/free text, exact normalized answer retries and
atomic answer/cancellation settlement. Interrupted waits close on startup without
claiming a resumed cell; committed answers survive. The public SDK performs no
automatic answer or delivery replay. Parent read the SQL, runtime and domain
boundaries and reconciled generated contracts with the integrated provider leaf.

Isolated final core races passed session1.626s/store30.983s/runtime21.357s; focused
question runtime11.460s and build/vet/pinned lint passed. Public socket/protocol
races passed2.478s/4.413s,19 SDK tests,7 interchange/CSP checks, drift/build/vet/lint.
The both-engine production fixture passed27.968s, including a lost answer
acknowledgement followed by SIGKILL, committed-answer recovery, pending-question
interruption, batch/freeform/dismissal, cancel/late conflict and child denial.
Fresh roots after uncertain REPL interruption preserve the existing no-replay
rule. Integrated race/shuffle passed store207.915s/runtime212.252s/process123.067s,
RPC52.714s/session5.813s and all other active packages. Analysis reported zero
lint issues and no reachable vulnerabilities. The full phase gate passed: production fixture31.035s, retained fixture2.626s
and daemon regressions2.711s, with all generated contracts/SDK checks. Logs use
`/tmp/whip-question-{phase,analysis}.log`. Saved modes, product question UI and
Phases6–7 remain outstanding.


## Workspace listing/search and authorized language services

Durable questions are published as draft [PR246](https://github.com/context-labs/whip/pull/246)
at `9b1cce8574f0a2f3a370f0d60103eed485b10626`, stacked on PR244. This increment
reuses files `75e61aa2d` as `21fc57fa9`, pure LSP `97062a5e0` as `f83b601f8`,
and public integration `2623ca62a` as `d7f0ad5a2`. Shared authority was already
integrated; its duplicate leaf was not replayed. Fresh schema36/config13/protocol4
apply. Provider Unicode helper and compact question declarations remain intact.

Listing/search are bounded descriptor-confined observations. Optional post-write
diagnostics settle separately, use captured content and standing workspace
authority, and never undo a committed write. Explicit one-use diagnostics isolate
and join their process; standing authority permits bounded reuse. Root/child
scope, issuer revocation, replaced workspace identity, generation invalidation,
read-only safe status and restart/shutdown are covered by the released tests.
Active phase/analysis and import boundaries now include `internal/lsp`.

Parent review found that retained `clientState.kill` joined only the direct
process after one group signal. The shared fork-race stop routine is now public
`Process.Stop` (`101f37837` as `768fbc6a2`), and language-client retirement invokes
it. Its cancellation/explicit-stop fork-race regression passed20 repetitions each
in1.648s, with build/vet/Linux build/pinned lint0. LSP retirement additionally
asserts owned process groups have disappeared before returning. This is a concrete
resource-lifetime correction, not a new execution owner.

Released file/tool races passed6.906s and final focused tool3.570s/runtime6.858s.
Pure LSP full race passed11.213s. Integration full small-package races passed
LSP11.545s/tool7.038s/config5.034s/protocol3.393s; focused runtime11.915s/store2.996s/
RPC1.741s/process1.683s passed. Final nullable diagnostic-envelope race1.923s,
build/vet/pinned lint0,19 SDK tests,8 interchange/CSP checks and drift passed.
Production fixture27.617s includes both engines, automatic/explicit ledgers,
status, shutdown PID disappearance and restart status reset. Parent reviewed the
workspace, store and LSP lifetime boundaries. The strengthened retirement/init-close stress test passed10 repetitions each in
22.782s. The first expanded phase gate failed the established core import-boundary
test: host config and protocol imported the process-owning LSP package. This was
not waived. A side-effect-free `internal/lspconfig` leaf now owns declarations,
validation and built-in merging; wire projection lives in RPC and protocol keeps
DTOs only. Both boundary tests passed store0.440s/runtime0.807s with the new pure
leaf checked explicitly. The final expanded phase and analysis gates passed after
this repair: store race190.736s, runtime201.157s, process109.943s, LSP15.012s,
workspace8.203s and RPC41.357s. Contract interchange/CSP, generated drift, SDK and
examples passed; production fixture28.747s (files/LSP680ms), retained crash
fixture4.119s and selected daemon races2.763s passed. Pinned analysis reports
zero issues and no reachable vulnerabilities. Original failure evidence remains
in `/tmp/whip-lsp-phase.log`; final logs use `/tmp/whip-lsp-phase-final.log`
and `/tmp/whip-lsp-analysis-final2.log`, and retirement stress uses
`/tmp/whip-lsp-joined-stop.log`. Saved-mode interaction and
supported-client adoption remain separate obligations; Phases5–7 are still open.

The first analysis run after extraction reported only grouped-alias formatting;
that formatting was corrected and the final analysis passed. Hosted Linux, macOS,
analysis and aggregate checks for PR243 at
`d4aeddf2f67d1f8e9df82b7676a3c2ca70ae1cbf` passed in
[run36499959078](https://github.com/context-labs/whip/actions/runs/36499959078).
The same full hosted set passed for PR244 at
`cb996cbf376ce88b8c4d25ec5723f80f1a456f7a` in
[run36500497345](https://github.com/context-labs/whip/actions/runs/36500497345).
PR246 analysis has passed; its platform jobs remain in progress at this check.


## Saved root permission modes and public recovery

Workspace/LSP integration is published as draft [PR247](https://github.com/context-labs/whip/pull/247)
at `ee57e1a4d`, stacked on PR246. Hosted validation is pending. This increment
reuses mode core `2c4499a21` as `6c11482a9`, public controls `12eb5b4f9` as
`c14cf4027`, and LSP interaction `bc7edb149` as `f097a1002`. Main’s pure LSP
configuration boundary, joined process stop, manual title initialization,
provider Unicode validation and compact question declarations were preserved
while resolving overlaps. Fresh schema37/config14/protocol4 apply.

One SQL policy per tree controls root prompting; children still need exact live
delegation. Stable edit receipts resolve before mutable state/CAS, including
stopped roots and deleted owners. Same-value edits preserve revision and retained
resources. Actual changes invalidate pending policy authority and language-server
generations, while exact historical retries cannot retire newly started servers.
New root/fork defaults are captured from fresh host snapshots; invalid values
fail. The old blanket child/outside-workspace Full Access bypass is explicitly
retired. Public RPC/SDK preserve safe DTOs and exact counters, expose receipt
recovery and never automatically replay host publication.

Released focused core races passed config1.539s/store5.616s/runtime8.023s/tool2.521s;
compatibility store8.307s/tool3.549s and stopped-root1.745s passed. Public protocol3.252s/
RPC2.118s,21 SDK tests,7 CSP/interchange checks, drift, build/vet/pinned lint0 and
production fixture28.983s passed. LSP follow-up focused store6.481s/runtime17.749s/
tool2.414s/capability2.623s passed, with22 SDK tests,8 CSP/interchange checks, drift,
build/vet/pinned lint0 and production fixture29.604s (mode1.182s/files-LSP778ms).
Parent reviewed policy admission/dispatch, receipt precedence and newly applied
retirement semantics. Expanded integrated phase and analysis gates passed: store
race204.723s/runtime215.476s/process111.834s/config7.283s/RPC44.376s, contract
interchange/CSP and drift, SDK/examples, production fixture30.737s, retained
crash fixture2.927s and selected daemon races2.640s. Pinned analysis reports zero
issues and no reachable vulnerabilities. Logs are `/tmp/whip-modes-{phase,analysis}.log`.
Hosted Linux, macOS, analysis and aggregate checks for PR246 at
`9b1cce8574f0a2f3a370f0d60103eed485b10626` now pass in
[run36501096754](https://github.com/context-labs/whip/actions/runs/36501096754).
PR247 analysis passed; platform jobs remain in progress. Product clients and remaining
Phase5 capabilities are still outstanding; no phase-completion claim is made.


## Immutable definition bindings and captured host vocabulary

Permission controls are published as draft [PR248](https://github.com/context-labs/whip/pull/248)
at `99260052c`, stacked on PR247. Hosted validation is pending. Definition leaf
`4db6f83be` is integrated as `0e8a44167`; independently reviewed overlap resolutions
from `7320b6d43` preserve mode/title admission, LSP vocabulary and pure boundaries,
native Unicode checks and compact declarations. `internal/hostmodule` joins the
active gates. Fresh schema38/config15/protocol4 apply.

The initial binding ceiling now survives kernel restore and permits only captured
subsets in later turns and children. Explicit empty module lists remain empty
through worker CLI arguments. Separate immutable tool/hook provenance is derived
from registered declarations, never caller-supplied owner references. Model
updates retain globals/aliases, but every invocation checks the live cell’s
captured policy before admission. Required hooks cannot silently disappear.
Live custom executors/hooks remain following work; unavailable declarations do
not fabricate operations or effects.

Released focused races passed session1.361s/store2.610s/runtime10.370s/RPC2.957s/
process7.434s; model-change/eviction regressions19.483s, protocol10 CSP/interchange,
SDK29, generated drift, build/vet/Linux build and pinned lint0 passed. Parent
reviewed resolution/provenance, child and edit ceilings, captured cell ownership,
worker empty-selection semantics, wire projections and alias/restart tests.
Expanded integrated phase and analysis gates passed: store race200.492s,
runtime222.630s, process116.341s and RPC43.424s, with generated interchange/CSP,
drift and SDK/examples passing. Production fixture31.648s, retained crash
fixture4.848s and selected daemon races2.679s passed. Initial analysis found one
extra blank line in the captured instructions; formatting was corrected and the
final analysis reports zero issues and no reachable vulnerabilities. Logs are
`/tmp/whip-bindings-phase.log` and `/tmp/whip-bindings-analysis-final.log`; the
original lint result remains `/tmp/whip-bindings-analysis.log`. No full-product or Phase5 completion is
claimed; client migration and retired-core removal remain open.


## Stable root creation and global catalog invalidation

Definition bindings are published as draft [PR249](https://github.com/context-labs/whip/pull/249)
at `855e8f530`, stacked on PR248. Hosted validation is pending. Reused discovery
`9c90a212f` as `d3718cfd5`, creation core `6caf5456f` as `63d3090fc`, and public
controls `dd5c72b20` as `88d207988`. Parent retained definition-source stripping
when moving root admission into its receipt-owning transaction, preserved all
newer protocol families, and regenerated declarations/validators. Fresh
schema39/config15/protocol4 apply.

Stable caller IDs now recover lost root-creation acknowledgements before mutable
host reads, including restart and deleted-root tombstones. Catalog head/pages
observe off-page metadata/title/membership changes without hydrating roots or
history. Manual same-value metadata intent keeps the original increment/automatic
title supersession behavior. Helper counter exhaustion cannot prevent actual
billing/candidate settlement. RPC/SDK require explicit identity/revision recovery;
no automatic replay, poller or second catalog truth cache was added.

Released focused races passed store6.798s/runtime2.064s/protocol2.240s/RPC2.434s;
full protocol0.726s/RPC7.632s, client compile, vet/build/lint0,10 CSP/interchange,
35 SDK tests and drift passed. Final production fixture43 stages38.900s includes
both-engine lost creation reply → SIGKILL → changed defaults → exact recovery,
fresh-default capture, fallback title invalidation, delete/restart/tombstone,
off-page filter/title changes and stale iterator rejection (new stage396ms).
Parent reviewed caller-only digest, retry/transaction precedence, tombstone
projection, catalog atomicity and preserved binding provenance. Expanded phase
and analysis gates passed: store race207.734s/runtime219.539s/process114.360s/
RPC45.088s, generated interchange/CSP and drift, SDK/examples, production
fixture31.630s, retained crash fixture2.289s and selected daemon races2.739s.
Pinned analysis reports zero issues and no reachable vulnerabilities. The first
phase run caught one newly integrated
binding socket fixture without its now-required creation identity; it was updated
to preserve that identity, without changing production validation. Original failure
log is `/tmp/whip-root-recovery-phase.log`; final phase output is
`/tmp/whip-root-recovery-phase-final.log` and analysis is
`/tmp/whip-root-recovery-analysis.log`.
Phases5–7 remain open; all supported product clients still need migration.

Hosted Linux, macOS, analysis and aggregate checks for PR247 at
`ee57e1a4d1a37be9129db3fdbd053fde45d9880c` passed in
[run36502087075](https://github.com/context-labs/whip/actions/runs/36502087075).
PR248 at `99260052cad3c559a378a455de529be55269a1a2` has passing Linux and analysis;
macOS remains in progress. PR249 at `855e8f530b8d8aac6a20ff5a2f9eb989c34bf018`
has passing analysis with both platform jobs in progress at this check.


## Pinned browser gateway and browser SDK transport

Root recovery/catalog is published as draft [PR250](https://github.com/context-labs/whip/pull/250)
at `b04d4dfaf34e87cf876f598155ef3d3393a57a71`, stacked on PR249. Hosted validation
is pending. Reused gateway core `cc4576b16` as `6e17a06c2` and browser SDK/fixture
`6b1ce821e` as `8830bf9f7`. Parent preserved the newer definition bindings and
hostmodule boundary, regenerated combined contracts, and added the new handshake
fields to the binding SDK fixture. `internal/gateway` and its real browser/process
fixture join the active gates. No storage/config version change: schema39/config15/
protocol4 remain current.

One private socket per browser connection retains the immutable network marker,
with startup/connection identity and process-generation acknowledgement before
relay. Exact Host/Origin policy, strict compact framing, bounded connections and
transfers, joined shutdown and owner-scoped content use the new core directly.
The shared4MiB transfer cap deliberately replaces the retired64MiB gateway cap.
No application assets are yet packaged; API discovery says unavailable and the
root returns503. Ordinary browser SDK calls/content do not replay or reconnect.

Released affected races passed gateway6.896s/RPC43.302s/protocol5.368s/command4.375s,
build/vet/import boundaries/pinned lint0,10 CSP checks,40 SDK tests and drift.
Native-browser production fixture1.856s covers both engines, lost submit replies,
4MiB owner-scoped references, SIGKILL/restart generation rejection, server-side
human-terminal denial and SIGTERM teardown. Existing43-stage fixture31.489s also
passed. Parent reviewed launcher/transport lifetime, marker/identity enforcement,
content identity, frame bounds and mutation recovery. Expanded phase validation
failed on three local runtime observation deadlines under high host load; final
analysis passed zero issues/no reachable vulnerabilities. The
initial analysis invocation was blocked by another lint process and is recorded
as unsuccessful, not a code finding. Logs: `/tmp/whip-browser-gateway-phase.log`,
`/tmp/whip-browser-gateway-analysis.log`, and
`/tmp/whip-browser-gateway-analysis-final.log`.

Hosted Linux, macOS, analysis and aggregate checks for PR248 at
`99260052cad3c559a378a455de529be55269a1a2` now pass in
[run36502676866](https://github.com/context-labs/whip/actions/runs/36502676866).
PR249 analysis passes; platform jobs remain in progress at this check.
Phases5–7 remain open; no supported-product cutover, deployment or installed
runtime change has occurred.

The first gateway phase run failed runtime completion-evidence (QuickJS),
reasoning-retry observation and pending-title observation deadlines. It remains
`/tmp/whip-browser-gateway-phase.log`; focused repeats of those exact scenarios
use `/tmp/whip-browser-gateway-failed-regressions.log`. Host load measured50.61/
131.68/82.65 after the run. No installed process was changed. In hosted PR249 run
36503421921, Linux store exceeded the unchanged10-minute aggregate package bound
while executing an ordinary6-second state-pressure test; runtime completed565.401s.
The previous PR248 run completed store409.016s/runtime357.119s. No deadlock or race
report appeared in the timeout stack, which showed active SQLite query compilation.
To reduce CPU/memory contention, the active race command now limits concurrently
running package binaries to2 (`-p=2`), retaining every test, shuffle/race mode,
package deadlines, CI job deadlines and all acceptance scenarios. This is a gate
resource adjustment; it changes no product concurrency policy. A full revised
phase run and fresh hosted evidence remain required before crediting validation.

The revised full local phase gate now passes: store229.544s/runtime232.842s/
RPC48.369s/process116.012s, all11 v4 CSP/interchange checks,41 v4 SDK tests,
generation drift/examples,43-stage production fixture30.830s, native browser
fixture1.901s, retained fixture2.712s and selected daemon races2.769s. The three
initially failing runtime scenarios also passed three repetitions with the
original shuffle seed (15.829s). Final analysis reports zero new lint issues and
no reachable vulnerabilities. Logs are
`/tmp/whip-browser-gateway-phase-final.log` and
`/tmp/whip-browser-gateway-analysis-final.log`. The parent corrected an import-map
merge typo before this final gate; no test or deadline was removed.

PR249 at `855e8f530b8d8aac6a20ff5a2f9eb989c34bf018` finished with passing macOS
and analysis, failed Linux and failed aggregate in run36503421921. PR250 at
`b04d4dfaf34e87cf876f598155ef3d3393a57a71` likewise passed macOS/analysis but
failed Linux/aggregate in run36504448730: store exceeded600s during context-pin
validation and runtime passed570.284s. Neither failed hosted revision receives
passing credit. The next gateway draft carries the tested concurrency adjustment
and requires fresh Linux/macOS/analysis/aggregate evidence.

## Connection executors and session-owned shell execution

The gateway is published as draft [PR253](https://github.com/context-labs/whip/pull/253)
at `bbe0f39d19fa17613c4d5c1b479cd4bc31e48899`, stacked on PR250.
Analysis passes in run36511095353; Linux/macOS are still in progress at this check.

Reused the released pure executor registry30649358 and public executor384864290,
integrated as2744d1851/f0ef59c5c after shared timeout/lifetime leaves6a1d16280,
9839ec0ae and550b7330f. The parent first integrated them in executor-controls,
preserving the gateway/root contracts and regenerating their combined wire types.
Reused shell extraction/bounds/lifetime/owner leaves as0b656fdb9,6b30e105f,
8d56376f6 and29542b840; completed public shell/input asb9994343b and production
acceptance as2daf73965. Root creation fixture IDs and new initialization fields
were retained. All three leaf packages and both production fixtures join the
active gate. Fresh schema39/config15/protocol4 remain unchanged.

Custom executors bind immutable definitions on connection-owned generations.
Consent precedes lease acquisition, SQL dispatch precedes invocation, and handler
loss never triggers another dispatch. Required hooks gate effects; optional
failure produces bounded evidence, without suppressing cancellation. Rewritten
spawn policy is checked again by ordinary SQL admission. Executor notices and
progress belong only to the exact active turn. New runtime/store/RPC/SDK tests
cover replacement, separate provenance, full schema validation and disconnect.

Session shells now provide foreground execution, persistent background jobs,
bounded output/content and transient human PTY input. Foreground cancellation
preserves partial uncertain evidence; background jobs survive turn cancellation
until explicit job/session stop, deletion or shutdown. Every stop joins owned
groups and callbacks. Human input is scoped to the exact operation and last
accepted sequence, allowing an exact lost-ack retry without duplicate bytes.
The real fixture exercises echo-disabled secrets, lost input replies, both
engines,1MiB output tails/content and group teardown. This is separate from human
terminal tabs, which remain a following checkpoint. Shell cwd identity checks
and process groups are not an OS sandbox or filesystem freeze.

Focused integrated races passed runtime27.909s/RPC3.321s/executor2.576s and the
shell/bashrun regressions. Final full `task check:phase` passes with store217.561s,
runtime250.054s, process116.262s, executor2.141s, bashrun8.780s and shell2.553s;
all11 v4 contract checks,46 v4 SDK tests, drift/examples, existing43-stage
fixture31.418s, gateway1.911s, executor1.714s, shell2.016s, retained fixture4.149s
and selected daemon races2.705s. `task check:analysis` reports zero new lint
issues and no reachable vulnerabilities. Logs:
`/tmp/whip-execution-services-{focused,phase,analysis}.log`. The original source
checkpoints' evidence is preserved in their commit tests; no installed runtime,
real credentials or real browser/helper session was used.

Phases5–7 remain open. A renewed retained-feature audit identified direct human
model-free shell/tool admission, guest artifacts.put/inspect and permission
inspection as unfinished backend parity. Browser/computer, human terminals,
MCP integration, live-provider evidence and complete client migration/removal
also remain; passing this checkpoint does not discharge those obligations.

### Executor/shell Linux PTY correction

PR254 first hosted head0e72402d2 failed Linux's fast phase in
`TestPTYJoinsBlockedKeyWriter` (run36512124869, job109226355275). The5-second
join bound exposed a blocking master descriptor: the PTY dependency's ioctl
path can leave the original descriptor outside Go's interruptible polling.
A blocked key write could therefore prevent Close from joining. This is a
product shutdown defect, not grounds to extend the test deadline.

`capability.OpenPTY` now duplicates the private master with close-on-exec,
sets nonblocking before wrapping it in a new Go file, and documents that later
ioctls must use SyscallConn. Agent shell execution uses this primitive. Three
repetitions of the exact join/descendant/callback regressions pass3.127s;
full affected race suites pass bashrun8.512s/shell2.552s/capability29.144s,
with vet and pinned lint0. Logs are `/tmp/whip-shell-pty-repair-*.log`.
Fresh hosted Linux evidence remains required; the failed head is not credited.



The corrected PTY head a377b7683 completed every active Go race package on both
platforms in run36513021610, but both jobs reached the20-minute job ceiling during
later checks. Linux store521.554s/runtime519.320s/process192.730s and macOS
store438.992s/runtime458.536s/process189.845s all passed; Linux stopped in contract
drift, macOS during the production fixture. Analysis passed and aggregate failed.
These cancellations are not passing end-to-end evidence. Exact logs:
`/tmp/whip-execution-services-repair-ci-linux.log` and
`/tmp/whip-execution-services-repair-ci-macos.log`.

The growing gate now runs build, race and client stages as independent required
jobs on both platforms, retaining the20-minute job ceiling,10-minute race package
deadline, all package/scenario selections and the unchanged aggregate failure
policy. Local `check:phase` still executes their complete union. Client acceptance
also includes the selected retained regressions. No test deadline was extended
and no scenario was removed; hosted evidence is required for the split workflow.


## Native MCP, human terminals and browser executor peers

Gateway draftPR253 at bbe0f39d19fa17613c4d5c1b479cd4bc31e48899 now passes
Linux, macOS, analysis and aggregate in run36511095353. The earlier pending
sentence is superseded. Execution-services draftPR254 was published at0e72402d2;
its Linux PTY defect and correction a377b7683 are recorded above, with new
hosted validation required for that corrected head.

Reused the clean MCP extraction/transport/host/runtime/public checkpoints as
78e38a442,d45759e1e,136d6c4c5,71a6a8ac0,5b05ed10d,783556749 and370deb9f7.
They add native schema40/config16 and active package/import boundaries, preserving
retained compatibility aliases for the final deletion phase. Reused independent
human-terminal core/public checkpoints as79b2b5f2f/3b8535f49 and persistent
browser duplex ascded5bd62. Browser executor acceptance now exercises the same
real fixture over both Unix and native WebSocket transports.

The first combined phase run failed an import-boundary guard and two account
fixture panics: the icon resolver assumed http.DefaultTransport had a concrete
transport type, but account tests deliberately install a credential wrapper.
The repair b98c1d0f6 creates an owned transport and updates the pure mcpconfig
boundary. Focused race checks pass RPC2.887s/command3.697s/icons1.912s/store1.941s.
The failed gate remains `/tmp/whip-host-resources-phase.log`; it receives no
passing credit.

The repaired full phase gate passes store215.061s/runtime262.358s/RPC48.144s/
process117.663s, terminal8.127s/MCP21.220s,11 strict-CSP v4 contract checks,
57 SDK tests, generated drift/examples, expanded production fixture32.486s,
native gateway/terminal fixture2.775s, Unix executor1.848s/browser executor1.889s,
shell2.042s, retained crash fixture4.090s and selected daemon races2.820s.
Analysis reports zero new lint issues and no reachable vulnerabilities. Logs:
`/tmp/whip-host-resources-phase-final.log` and
`/tmp/whip-host-resources-analysis-final.log`.

After that gate, the upstream PTY fix was integrated as3020b1434 and human
terminals now reuse the same primitive instead of maintaining a second copy.
All affected race packages pass terminal7.877s/bashrun8.664s/shell2.854s in
`/tmp/whip-host-resources-shared-pty.log`. Final canonical docs distinguish MCP
metadata from model vision, human-terminal ownership from agent authority,
and persistent browser transport from product renderer adoption.

Phases5–7 remain open: native browser/computer, direct human actions, remaining
bootstrap/trace controls, SDK services/views, every supported product client,
manual/live evidence and retired-core removal are still active obligations.
No merge, deployment, installed-runtime replacement, real account mutation or
real browser/helper session was performed.

The final release gate, including the shared PTY primitive and terminal reuse,
also passes in full atca74b20e5: store210.086s/runtime258.015s/RPC47.582s/
process117.794s, terminal9.177s/MCP21.227s, production fixture33.177s,
gateway/terminal2.515s, Unix/browser executors1.713s/1.806s, shell1.966s,
retained crash4.335s and selected daemon races2.697s. Contract/SDK/drift/examples
remain green; release analysis reports zero new lint findings and no reachable
vulnerabilities. Exact logs are `/tmp/whip-host-resources-release-phase.log` and
`/tmp/whip-host-resources-release-analysis.log`. Hosted evidence for this
checkpoint will be recorded against its published head, not inferred from local
results or another PR.


## Uniform session clients, direct human actions and exact recovery

The integrated client contract now uses fresh schema42/config16/protocol4.
Guest utilities (8f57b136e) preserve scoped text artifacts and intrinsic own
permission inspection. Direct human tool/shell admission (95ed567b5) shares the
ordinary receipt/input/turn/permission/dispatcher path with exact direct-turn
provenance, without inventing model calls, interpreter cells or conversation.
The retained parity audit is `.ai-docs/audits/backend-redesign-phase5-parity-2026-09-28.md`;
its unresolved families remain implementation obligations.

History paging2c5e5c3b2 reads tail/revision/page in one statement; activity and
input discovery9293ab46f expose exact owner state including another client's
queued work. Receipt matching65e3e3932 compares original typed parameters using
shared Go admission normalization, without sending or admitting work. Missing,
changed payload and tombstoned outcomes remain distinct across restart.

SDK checkpoints5b542dba7/2b5cd949f/ef5b00662/47c5228bc/a474a180c/c1dfef7a7
provide inert uniform root/child handles, explicit bounded command journals,
immutable bounded session/catalog views, activity/input services and thin React
subscriptions. A recovered identity alone cannot prove payload acceptance;
ordinary commands use read-only receipt matching. Application drafts, selection,
reading anchors and shared observation lifetimes remain outside SDK truth.

The complete local phase gate passes: store217.764s/runtime267.254s/RPC50.819s/
process114.530s race+shuffle,11 strict-CSP v4 contracts,81 SDK tests, generated
drift/examples, production fixture31.670s, gateway/session/terminal3.620s,
Unix/browser executors2.190s/1.788s, shell1.924s, retained crash2.495s and selected
daemon races2.704s. Analysis reports zero new lint findings and no reachable
vulnerabilities. Logs: `/tmp/whip-session-clients-phase.log` and
`/tmp/whip-session-clients-analysis.log`. The gate began before the CI-only task
split; its command union and package/scenario/deadline coverage are unchanged.
The split was checked with `task --dry check:phase` as well.

PR255's base repair was inherited as7ece7203f: its earlier conflict was solely
chronological documentation because the already-tested PTY patch existed under
a cherry-picked identity. c54b64c98 preserves its exact tree; bcec1270f also
inherits the required CI stage split. No pull request was merged. Hosted results
for the split jobs remain pending and must be verified against their exact heads.

The uniform SDK handle/view acceptance item is complete. Phase6 overall remains
open because supported product clients still need cutover and their own gates.
Native computer/browser controls, host bootstrap/attention/trace, saved host
profiles and remaining CLI controls are proceeding in isolated leaves; none of
those unintegrated leaves is credited here. Phase7 deletion/release work remains.

## Native controls, host previews, typed agents and trace integration

Next integration uses fresh schema45/config18/protocol4. Reused checkpoints:
computer helper/controller43d1ab75f/342ce0b03/27b499ef5, typed image43
697e8f612, computer44/config17 547810b20, host previews456f409b8,
attention5ff13a28b, gateway discovery7145e4e1e, typed SDK agentsafa6dbcd4,
trace45 5dc507ec5, saved host profiles/config18 e63ae0891 and confined native
frame transportbf7ba0b33. Gates and import ownership include the new packages.
Generated contracts were rebuilt from Go after integration; computer permission
exclusions, existing receipt matching and host-view schemas are all retained.

Independent evidence includes typed agent SDK94+compile tests and real executor
fixture4.614s; profile config race4.645s, protocol2.742s/RPC1.916s, SDK84,
CSP/drift12, lost-ACK/SIGKILL persistence fixture4.118s; trace focused
store6.775s/runtime2.119s/RPC3.082s/protocol3.298s/trace2.567s, production
fixture37.795s. Native frame tests cover exact per-call identity, synchronous
close callbacks, lost ACK without replay, aborted setup/late connection cleanup,
malformed envelopes and queue bounds. SDK87 passed on that independent branch.
Combined phase and analysis gates are pending; these are not whole-product passes.

Hosted runs36514900483 (#254 at46af42ed7),36514923722 (#255 atbcec1270f)
and36515394834 (#256 ate9400aa4f) show passing split build/client/analysis
jobs so far, with race jobs still running when checked. No PR was merged.

Shared app adoption is underway in a separate worktree: recovery storage uses
cross-window transactions,64records/8MiB bounds and no unresolved eviction;
accepted records cannot regress under stale writers. It is not yet wired into
all product command paths. Browser control, workspace/run controls, the complete
renderer/desktop/mobile/CLI/TUI/ACP cutover, core deletion and final release gates
remain open. Fake helpers do not establish real macOS permission/signing or live
provider evidence, and the installed runtime remains untouched.

The combined native-controls gate completed successfully at032091517:
store253.852s/runtime293.846s/RPC56.711s/process115.531s race+shuffle;
12 strict-CSP v4 contracts,105 SDK tests, generation drift and examples;
production fixture33.266s, gateway/profile restart3.491s, Unix/browser
executors2.509s/2.453s, shell2.156s, fake computer helper2.081s, retained
crash3.881s and selected daemon races2.749s. Analysis passed with zero new lint
findings and no reachable vulnerabilities. Logs are
`/tmp/whip-native-controls-{phase,analysis}.log`. The subsequent SDK-only native
process-epoch pin632f779f7 passed all106 SDK tests, including rejection of a
restarted peer before a dependent request. Its log is
`/tmp/whip-native-controls-epoch-sdk.log`; no Go behavior changed afterward.

Hosted validation is now fully passing at exact published heads:
#254 46af42ed735726549b2db99ffd7538fb18ee4fcd/run36514900483,
#255 bcec1270f193e5f576231af2290031d8267dd448/run36514923722, and
#256 e9400aa4f4a1712f4e9b7c2d0e9d6e7d1622e05e/run36515394834.
Each has successful Linux/macOS build, race and client jobs, analysis and required
aggregate. These supersede the earlier pending split-run statuses, not the
historical failed/cancelled runs. No pull request was merged.

Further retained parity audit confirms human active-turn steering and queued-input
promotion need a v4 implementation; existing mail steering is not equivalent.
Browser design selections also require explicit validated input presentation
provenance, rather than inferring trusted metadata from tagged text. Both remain
open alongside browser integration and full client adoption.


## Workspace/run controls and authored design provenance

Integration branch `codex/backend-redesign-input-controls` follows draft #257.
It reuses tested controls leaf `a93b16fb4` as `929cc982b`, resolving host computer,
host profiles, receipt matching, shared process-manager ownership and regenerated
contract output. The retained run-control test executor now returns canonical
parts, matching the already-integrated typed-image execution interface.

Fresh schema 48/config 18/protocol 4 includes revisioned workspace/run controls and
bounded display-only design provenance. The separate browser leaf reserves 47;
its later merge must preserve schema 48 or a newer integrated version. No old
storage or installed runtime was touched.

Focused race/shuffle controls passed: runtime 21.091s, runner 1.983s,
model 2.490s. Design store/RPC tests passed 2.226s/2.525s, covering exact retry
conflict, invalid/foreign/duplicated/wrong-kind evidence, SQL immutability, both
history readers, fork ownership, restart and receipt-first source deletion.
The runner projection test verifies that original content bytes remain in model
input and display metadata does not enter provider requests. Generated strict-CSP
contracts and 109 v4 SDK tests pass; retained 466 SDK tests and 6 example checks
also pass. The production fixture passed 38.077s, including workspace/run controls
141ms and design provenance253ms (dropped ACK, read-only exact recovery, restart,
fork and source deletion). Analysis passed with zero new lint findings and no
reachable vulnerabilities before the final runner-only test addition. The full
phase gate for this integrated checkpoint is pending below; these focused results
do not mark Phases 5–7 complete.


## Separate CI runners for large race suites

Draft #257's hosted run 36517034403 passed macOS race, both builds, both client
gates and analysis, but Linux store/runtime each exhausted the existing 10-minute
package deadline. The tests active at timeout had run for only 1 second; both
stacks were making ordinary SQLite progress rather than demonstrating a stuck
individual test. The overall job finished 19m44s with failure. This is not a
passing release gate.

CI now gives the complete store suite and complete runtime suite separate
runners on each OS. A third race job runs every other active package, derived
from the same active-package union with only those two exact paths excluded.
The local `check:race` remains the complete union. Race/shuffle/count flags,
10-minute package deadlines, 20-minute job deadlines and the required aggregate
are unchanged. The workflow still requires all builds, all client checks and
analysis. Hosted results for the repaired head remain pending.


The combined workspace/run and design checkpoint passed `task check:phase` and
`task check:analysis` at `8012242eb`. Store/runtime/RPC/process race suites passed
in 265.377s/339.914s/57.751s/192.569s respectively. Contract generation and strict
CSP checks, 109 native SDK tests, 466 retained SDK tests and 6 example checks passed.
The production v4 fixture passed in 96.792s, including workspace/run controls
205ms and design provenance 319ms. Gateway passed 12.762s, Unix/browser executors
5.678s/3.121s, shell 2.593s, fake computer helper 2.336s, retained crash 4.195s and
selected daemon races 2.743s. Analysis found zero new lint issues and no reachable
vulnerabilities. Exact logs: `/tmp/whip-input-controls-phase-final.log` and
`/tmp/whip-input-controls-analysis-final.log`.

Earlier attempts failed on a wall-clock-dependent assertion and an invalid test
fixture literal; `038cb331e` made the timestamp deterministic and `8012242eb`
derived the actual typed design presentation. Only the final successful runs
above establish this checkpoint. The subsequent merge brings in #257's separate
race-runner CI repair (`4d3b155cb`) without changing Go runtime behavior. Hosted
checks for this new checkpoint remain pending. Phases 5–6 remain in progress and
Phase 7 remains pending; no installed runtime was modified.

## Browser peers, execution defaults and input steering

`codex/backend-redesign-browser-integration` follows draft #258. Integrated code
`fc5753961` preserves schema 49/config 19/protocol 4. It reuses execution defaults
8b652a1a9, the three browser foundation leaves 322c613cc/d1d60f7a0/bb0e75812,
browser runtime 28271aae7, public peers c89fdeec1 and steering bfcba5070. Native
transport deadlines reuse c2480748e. Browser runtime merged with existing tree
control gates, schema versions stayed monotonic, and generated contracts were
rebuilt from the combined Go registry.

The combined browser/defaults/import-boundary focused race suite passed runtime
14.563s, store 4.016s and RPC 5.705s. Independent steering evidence passed focused
store/runtime/runner/RPC/protocol races, complete runner tests, 112 SDK tests,
contract/CSP/drift checks and the production crash fixture (35.832s, steering
stage 197ms). Independent browser public peers passed actual-socket RPC races,
118 SDK tests and 13 strict-CSP contracts; these are leaf results, not a combined
release pass. Full integrated phase and analysis gates are pending below. Every
browser, browser/extrelay and browserhost test is now in the active gate union.

Browser offers/catalogs do not grant control. Captured SQL authority and live
provider epoch/attachment lineage both govern native dispatch. Preview expansion
retires narrower authority; stop/revoke/delete/disconnect retire live resources.
Screenshot chunks are confined to the exact pending command. Native success does
not pre-settle an operation, and uncertain publication never restores live control.
Atomic child transfer and the actual desktop bridge remain subsequent work.

Input steering records one immutable target while preserving the original input
and its one logical charge. Consumption appends canonical messages only at full
tool-batch or text-response boundaries, bounded to 20 messages/2 MiB of encoded
parts. An untaken input remains queued and may open its own later turn, never
retargeting a different active turn. Exact promotion receipts survive deletion.
Human submission defaults queued; guest agents.submit preserves default steering.

Host execution defaults use configuration CAS. Engine, effort, compaction,
additional goal continuations and total model attempts are captured at admission;
existing work is unchanged. Maximum attempts remains the native 1–5 bound, with
3 by default; zero additional goal continuations means none. These deliberate
semantics replace legacy overloaded reset/retry labels in product settings.

Draft #257's repaired hosted run 36518700275 at 4d3b155cb passed all Linux/macOS
build, race-store, race-runtime, race-other and client jobs, analysis and the
required aggregate. This supersedes the pending repair status above; the earlier
Linux timeout is still a recorded failed run. Draft #258 remains pending hosted
validation. Neither pull request was merged.


The first integrated browser/defaults/steering phase attempt at dc0ed126b failed
in the fast configuration suite: the computer-settings test still expected
configuration version 18 after execution defaults advanced it to 19. The
assertion now checks the current version constant; explicit rejection of the old
format remains covered. Analysis passed with zero new lint issues and no reachable
vulnerabilities. The failed phase run did not reach race/client gates. Logs:
`/tmp/whip-browser-steering-phase.log` and
`/tmp/whip-browser-steering-analysis.log`.

Browser selection checkpoint 2b532b1c0 and contract declaration fix c4fe5276e
are integrated with regenerated contracts. The browser-provider production
fixture is now required by check:fixture. Combined phase validation follows;
independent leaf results do not establish that pass.

## Input-controls hosted build deadline correction

Draft #258 at c5850730c failed Linux build in run 36519493581 because
the complete runtime package exhausted the shared 120-second package deadline.
The sole active test (turn-permit blocked resumption/deadline) had run for just
2 seconds and remained within its existing 5-second scenario wait. All six
Linux/macOS race jobs, both client jobs, macOS build and analysis passed.

The fast gate now runs the complete runtime package separately with a 5-minute
aggregate deadline. Every other fast package retains 2 minutes. No tests or
scenario assertions were removed, individual operation waits remain unchanged,
and race package/job deadlines remain unchanged. This targets the growing
package's cumulative cost rather than extending a stalled scenario. The full
local build check and new hosted result are pending below. Failed-run evidence:
`/tmp/whip-pr258-linux-build-failure.log`.

The repaired #258 build gate passed locally at 35793114b, including the complete
runtime suite in 84.853s, all other fast packages, active builds and vet. Exact
log: `/tmp/whip-pr258-build-repair.log`. The prior complete race/client passes
remain evidence for unchanged runtime code; the new hosted run is pending.


## Integrated browser checkpoint validation

At a65a3a0b2 the complete active build/vet and race/shuffle suites passed,
including store 289.550s, runtime 347.842s, RPC 70.195s and process 115.739s.
The phase command then failed contract drift because the declaration-generator
leaf had been integrated after the earlier generation. f2005eb2c regenerates
only that declaration (five BrowserEvent non-null variant constraints). Go and
wire validation behavior are unchanged. The failed command is not a phase pass.

All remaining client gates passed at f2005eb2c: native contract14 plus retained16,
native SDK133, retained SDK466, examples6, and every production fixture. Actual
scenario durations: v4 crash/admission33.461s, gateway3.651s, Unix/browser
executors2.452s/2.509s, shell2.211s, fake computer2.180s, native browser
provider1.860s, retained crash4.299s and selected daemon races2.772s. Analysis at
a65a3a0b2 passed zero new lint issues and no reachable vulnerabilities. Exact logs:
`/tmp/whip-browser-phase-integrated.log`,
`/tmp/whip-browser-clients-integrated.log`, and
`/tmp/whip-browser-analysis-integrated.log`.

This checkpoint integrates the tested #258 build-gate repair ae59e2e48. Its
local complete build gate passed; replacement hosted validation is pending.
Browser child transfer, product renderer/native-client migration, and retired
core removal remain outstanding. Phases5–6 are in progress; Phase7 is pending.
No merge, deployment, or installed-runtime change occurred.

### Native client and observation foundations (2026-09-28, in progress)

The next stack branch is `codex/backend-redesign-client-cutover`, based on the
browser integration draft #259. Integration head `22b7e5b0d` reuses the exact
released native Go client, shared command startup, local launcher, catalog search,
tree summaries, turn metadata, execution observer and built-in persona leaves:
`c457abc02`, `17a78f574`, `e7a7cc02b`, `4cab2c676`, `9b422109d`, `793228ce4`,
`8a6d98e19`, and `292cc6684`. Fresh storage is schema50; configuration remains19
and protocol development major4. Schema49 steering remains intact. The schema50
change adds the owner/start/id turn index and does not migrate old storage.

Native Go session handles perform no I/O on construction. Durable commands retain
exact request records, bounded journal recovery and explicit retries; observation
cancellation never cancels accepted work. Shared `internal/hostcmd` owns the one
native startup/shutdown path. `internal/localruntime` launches only the fresh
runtime-v4 namespace, serializes startup with private owner/lock records, and
never reads the legacy namespace or signals an unverified PID. Native
`host.status/stop` verifies runtime and process epoch; stop remains local-only,
acknowledges before normal joined shutdown, and survives acknowledgement loss.
Disposable child-process tests are separate from the installed runtime.

`trees.list` preserves bounded literal search and root working directories.
`trees.summaries` reads at most64 selected roots in one SQL snapshot, with deep
child activity and explicit missing roots. Neither hydrates transcript bodies or
starts workers. `sessions.turns` pages canonical newest-first turn metadata. The
SDK execution observer holds bounded turns/cells/operations alongside its sole
transcript view, discarding obsolete reads after epoch/history revision changes.

The real retained coding/junior-developer personas use ordinary immutable
registration and exact module/configuration constraints; no authority is minted.
An absent host standing-instruction publication means disabled discovery without
probing a fallback path. Explicitly configured missing/unsafe sources still fail
closed. Both actual engines and restart checks passed in the released leaf.

Local integrated phase validation at `22b7e5b0d` is currently running; final
results must be appended before this slice is represented as passing. Its pinned
analysis gate passed with zero new lint findings against the unchanged baseline
`e3fed9c91918d9c36766dd47d878c1b5466238d1`, and no reachable vulnerabilities.

The predecessor input-controls draft #258 at `ae59e2e48` now has all hosted
Linux/macOS build, store/runtime/other race, client, analysis and aggregate
`redesign` checks passing in run `36521478534`. This supersedes the earlier
pending note while retaining the failed pre-repair runtime aggregate-timeout
record. Draft #259 at `8d538697d` failed its Linux browser fixture; the isolated
repair `ff8796849` selects detected Chrome consistently and supplies an Xvfb
display without weakening sandbox flags or removing scenarios. Its repaired
Linux build passed in run `36522499701`; remaining jobs are still pending here.

Parallel app adoption remains isolated and incomplete. Recent tested checkpoints
include `d60f52385` exact post-disconnect lost-ACK recovery, `668d6d672` paired
session/execution lifetimes, `c6db926b5` native desktop browser bridge,
`9e370fcfa` verified native browser/design submission, `42d141f51` native REPL
and scoped content, `4c3c33a5f` complete workspace recovery/steering outcome, and
`e2e2d3454` native queue controls. Combined browser/recovery/lifetime tests passed
152 cases; REPL integration passed11; workspace/recovery passed42; queue passed8
using the real validating SDK. These do not prove a full app build. Native
inspector leaf `98cfda85d` has40 focused tests and awaits parent integration.
The current parity audit names the remaining required capabilities explicitly.

The complete `task check:phase` at `22b7e5b0d` passed: active formatting/build/vet,
all package race/shuffle tests, generated contracts, SDK/examples, production
process fixtures and retained acceptance regressions. Store race time was
286.639 seconds; runtime race time 338.128 seconds. `task check:analysis` also
passed with zero new lint findings against the frozen baseline and no reported
vulnerabilities. These are foundation integration gates, not evidence of full
client cutover or Phases 5–7 completion.

### Browser integration hosted fixture repair (2026-09-28)

Draft #259 at `8d538697d` failed Linux build in run `36521788536`:
`internal/browser` found the system Chrome through `PATH`, but only the explicit
candidate-path branch configured `ROD_BROWSER_BIN`. Production `Open` therefore
launched Rod's downloaded Chromium, which had no usable sandbox on the runner.
The fixture now consistently selects its detected executable. Linux build and
race-other jobs use an isolated Xvfb display for the retained headed dedicated
browser scenarios. Production launch flags and sandbox policy are unchanged;
no browser scenario is removed. Local browser checks and the subsequent hosted
run are recorded separately below; this failed run is not passing evidence.

Local affected browser scenarios (`TestE2EHeadless`, `TestE2EDedicated` including
reattachment, and `TestManySequentialCalls`) passed in 10.964s on macOS with an
isolated profile. This proves the fixture change locally, not the Linux Xvfb
setup; the exact repaired hosted head must pass before crediting Linux evidence.

### PR 259 store race gate partition — 2026-09-28

The repaired `ff8796849` hosted run 36522499701 passed both browser/build gates,
both runtime race gates, other races, clients and analysis. Its Linux store
package exhausted the aggregate 600-second timeout while a newly started
`TestModelHelperProvenanceIsOwnedImmutableAndRetryable` (reported 0 seconds old)
was initializing a fresh schema. This is recorded as a failed run. The same
full store suite passed locally at the later integration head in 286.639 seconds.

The hosted matrix now runs the store suite in two complementary groups: names
matching `^Test[A-M]`, and everything excluded by that exact pattern. Both retain
the race detector, shuffled order, count one and ten-minute package deadline;
no individual test, assertion, production timeout or fixture is weakened. The
local `check:race-store` target runs both groups sequentially. Examples and future
names automatically belong to the complement, preserving complete coverage.

Local complementary store validation passed in 193.133 seconds and 89.438
seconds respectively (`task check:race-store`), with every test covered exactly
once by the mutually exclusive run/skip patterns. Hosted validation is pending
for this repair; the previous hosted failure is not claimed as passed.

### Native run-client integration — 2026-09-28

The tested native one-turn CLI orchestration leaf `a2a569872` is integrated as
`ab08f01c6`, followed by the verified PR 259 CI repairs in `ea0da1ef5`. The native
run client, Go transport/client, local host launcher and shared host entrypoint
pass combined race/shuffle tests at the integrated head: 20.311, 8.511, 3.495
and 5.096 seconds respectively. Public CLI flag routing and remaining supported
client cutovers are still in progress; the new package is a tested foundation.
PR 259 repair `39c76adb6` hosted run 36523975388 is pending at this record.
### PR 259 extension relay publication repair — 2026-09-28

Hosted run 36523975388 at `39c76adb6` exposed a real intermittent handshake
race: `TestCDPTunnelRoundTrip` waited the entire 120-second package deadline.
The extension's HTTP 101 acknowledgement could reach its client before the
relay published that connection, letting the first CDP command see no extension.
Leaf `ef99a4403` holds the existing relay lock across upgrade and publication.
A deterministic gated-socket regression fails on the old code and passes with
the repair. Focused handshake/auth/roundtrip races pass 100 repetitions (4.318s);
the full fake-only relay race suite passes ten repetitions (3.958s), vet and
pinned lint pass. Test-local reads/dials now fail after five seconds instead of
hanging indefinitely. No production timeout, assertion or test is weakened.
The prior failed hosted run remains recorded as failed; fresh validation follows.

### Hosted runtime aggregate race partition — 2026-09-28

Draft #259 at `2f56f9f58` passes both platform builds, store race groups, other
race packages, clients and analysis in run `36525000977`. The Linux runtime
package alone reached its ten-minute aggregate deadline, with the current
automatic-title deletion case running for one second; macOS runtime passed.
This remains a failed run, not a passing checkpoint. Runtime race tests now use
complementary `^Test[A-M]` and `-skip ^Test[A-M]` jobs, preserving every test and
example, race instrumentation, shuffle, count and each ten-minute deadline.
The aggregate runtime task executes both jobs. No test assertion or individual
operation timeout changes. Hosted validation of the partition is pending.

### Native transfer, navigation, trace, CLI and usage integration — 2026-09-28

The coherent integration at `03d8ae4da` passes the complete `task check:phase`:
active formatting/build/vet, all package race/shuffle checks, generated contracts,
SDK/examples, native CLI, compiled production fixtures and retained regressions.
Store race passed in 298.291 seconds and runtime race in 351.469 seconds. Native
CLI race scenarios passed in 31.620 seconds and the actual compiled CLI fixture
in 18.627 seconds. Pinned analysis passes with zero findings against unchanged
`e3fed9c91918d9c36766dd47d878c1b5466238d1` and no reported vulnerabilities.

This slice integrates atomic/recoverable Browser transfer, same-owner preview
expansion, bounded recent-root/file completion reads, bounded native trace
windows with actual host observation time, host environment composition, exact
subtree usage and native CLI run/session/export workflows. The first full gate
at `0f36a153e` failed two standing-instruction tests because they assumed the
execution guide immediately followed source text. `0664c5d3e` now checks the
intended environment ordering while strengthening exclusion of the private
source directory; the passing complete rerun includes this correction.

Merge `9fb3bf8e8` then incorporates the exhaustive runtime CI partition from
foundation draft #260 without changing product code. Its previous hosted head
`c38277eaa` failed only Linux runtime's aggregate ten-minute deadline in run
`36525072107`, with the current observation test running for one second. Both
platform builds, clients, analysis, other races, store groups and macOS runtime
passed. The new partition retains every test and deadline; its fresh hosted
results remain pending. No failed run is represented as green.

App adoption, mobile, remaining desktop/distribution and TUI/ACP consumers are
still being migrated in their isolated worktrees. Native daemon/updater/desktop
startup checkpoints are tested separately and await the next integration.
Phases 5–7 and deletion of the retired core remain incomplete. No product branch
merge, deployment, installed-runtime restart or original development edit occurred.

### Native desktop, gateway and model inspection integration — 2026-09-28

The coherent integration at `397cb9785` passes complete `task check:phase`, including formatting/build/vet, all native Go race/shuffle suites, contracts, SDK/examples, public CLI, actual compiled fixtures and retained regressions. Store race passed313.939s, runtime372.290s, RPC72.644s, native CLI34.962s. Separate `task check:analysis` passes with zero findings against unchanged `e3fed9c91918d9c36766dd47d878c1b5466238d1` and no reported vulnerabilities. Logs: `/tmp/whip-desktop-native-phase-repaired.log` and `/tmp/whip-desktop-native-analysis-repaired.log`.

This checkpoint integrates native daemon/status/log/updater routing, desktop local/SSH attachment and startup fixtures, bounded renderer framing, native public web/gateway owner lifetime, explicit bundled computer-helper selection, Browser preview/control ownership, scoped content metadata, pending-permission filtering and captured model/prompt/notices/compaction inspection (schema52). The renderer and mobile cutover remains a separate in-progress increment; signed matching-artifact startup/continuity and Linux packaged renderer validation are still outstanding. Nothing was installed, restarted in a user runtime, merged into a product branch or deployed.

Earlier combined runs remain recorded as failures: the first SDK trace-state fixture used a1ns clock delta that rounded at epoch-sized floating point; `c2594080f` uses a deterministic small clock. The later runtime restart fixture assumed its scripted input was always the final message, ignoring canonical interrupted-child completion mail; `3ffd94dcc` forces and verifies that ordering while retaining ownership/restart checks. The CLI gate exposed a real socket/context deadline publication race; `397cb9785` recognizes the caller's already elapsed deadline before `Context.Err` publication and cancels only the exact accepted input. An earlier unrelated transport timeout remains uncertain and gains no cancellation authority. The final full gate includes all repairs.

Previously pending hosted checkpoints are now verified successful: draft259 head`9cfa29e63a48c16587a6498ff02f85e0b29e1468`, run36527605325; draft260 head`a51afc4f8770bebb1ffa07b5677d96b316188cdf`, run36527639551; draft261 head`50bcd028631a96d421a4baf38f87fa77dc7c718c`, run36526837824. The new desktop checkpoint's hosted validation is pending. Phases5–7 remain incomplete; the active source-scope exclusions and retired core have not been removed.

### Desktop Linux unavailable-host contract repair

Hosted desktop draft262 at `d74bcf1da` failed its Linux client gate in run36532327797.
Every other platform job and analysis passed; the aggregate correctly remained failed.
The compiled computer fixture received `HOST_UNAVAILABLE` for an unsupported bundled
helper, but that already-emitted host-service error was missing from the generated
RPC error enum. The authoritative Go DTO now includes the error, and a real wire-error
regression covers both directory-picker and bundled-helper unavailability on every
platform. The regression failed before the fix and passed under race afterward
(1.651s). Generated contract/CSP/drift checks and the compiled disposable computer
fixture passed locally. The existing Linux scenario is retained unchanged; new hosted
validation is pending. Logs: `/tmp/whip-host-unavailable-{before,after}.log` and
`/tmp/whip-desktop-ci-{contract,computer}.log`.

## Native retained controls integration — 2026-09-29

This checkpoint adds validated provider-key setup and the native account CLI,
independent durable tree-wide interactive permission denial (schema53), captured
idle-boundary session reload and exact recovery receipts (schema54), derived
admitted-input identity in queue/history, and captured Rod/ChromeDP selection
(config20). It reuses the tested leaf commits; it creates no replacement runtime
or installed application state. Pending reload acceptance remains distinct from
application, and browser settings cannot reinterpret accepted batches.

At `3cf771fae`, active build/vet/fast and the complete package race suites passed
(store323.609s, runtime393.702s). The phase command then failed the native CLI
client gate: the retained combined MCP/auth dispatch test invoked native auth
through a legacy fixture whose temporary socket exceeded macOS's100-byte bound.
A verbose repeat identified the exact `mcp_and_auth` subtest. The test now keeps
MCP in its retained fixture and uses the existing short native auth fixture,
preserving both assertions and the production path limit. The independently
reproduced missing `HOST_UNAVAILABLE` contract error is also repaired.

At repaired code head `14315f2ee`, the complete client gate passes (native CLI
race48.242s, compiled CLI and all production/retained fixtures), and analysis
reports0 new lint issues against frozen `e3fed9c91918d9c36766dd47d878c1b5466238d1`
and no vulnerabilities. Runtime/store implementation is unchanged by these two
repairs; their complete preceding race coverage is retained. Logs:
`/tmp/whip-parity-phase.log`, `/tmp/whip-parity-native-cli-verbose.log`,
`/tmp/whip-parity-clients-repaired.log`, `/tmp/whip-parity-analysis.log`.

Desktop draft262 repaired head `0e6930680` is now fully green in hosted run
36533998513, including Linux/macOS/analysis/aggregate. Its earlier run36532327797
remains a recorded failure. The new parity draft's hosted gates are pending.
Phases5–7 remain in progress: ACP/TUI, final renderer integration, acceptance
artifacts and retirement of the old core still have work outstanding.

## Bounded live native REPL stdout

Both engines now publish their existing process output callback through read-only
`cells.output`. The SDK validates owner, process and64 KiB byte bound and retains
one exact-cell preview in the existing bounded ExecutionView. It clears on
settlement, detach, history change or process replacement; slow readers neither
queue output nor block execution. Reading never starts a kernel or replays code.

Independent backend review and targeted races passed: runtime15.604s, RPC2.670s,
protocol1.193s; vet/pinned lint0. Real engine proofs cover output before an
intrinsic question, answer/settlement, cancel and restart, with committed full
stdout retained. Callback tests cover capacity, Unicode, stale callbacks and
settlement-before-retirement. All177 native SDK tests and protocol/CSP/drift
passed (`/tmp/whip-cell-output-{sdk-check,protocol-check}.log`). Product renderers
must still adopt this new preview; this checkpoint does not claim that UI work.

## Native accounting, live output, ACP and MCP integration — 2026-09-29

The parity draft #263 originally had no hosted run because its #262 base gained
`0e6930680` after branch creation. Rebased only this task's draft ancestry onto
that reviewed fix, regenerated generated-only conflicts, and verified the entire
source tree is byte-for-byte identical to `d2bd6537e`. Updated with an exact
force-with-lease. Published head `df902fe962a9d7807c44d1e2b42038c809f14873`
passes every Linux/macOS job and analysis in run `36536378729`. This is not a
merge into any product branch; no original development files were modified.

The next backend checkpoint is `95ce0ab96`. It reuses exact reviewed leaves for:

- Per-turn `usage.turn` / `session.turns.usage`, derived from actual owned attempt
  evidence, including helper and compaction attempts and committed compactions.
  Whole-tree cumulative usage remains a different read.
- `context.usage` / `session.context.usage`, captured from the latest actual
  ordinary/final prefill. Reported input including zero wins over the captured
  estimate; stale tail, changed configuration/selection and unknown capacity stay
  explicit. Schema55 adds only a derived partial index; there is no mutable
  duplicate context ledger or provider preparation during reads.
- Bounded process-generation-scoped cell stdout through `cells.output` and the
  existing SDK execution observer: at most64 owners/64KiB per owner, exact
  cell/turn/call/history joins, no read-triggered execution or persistent stream.
  Committed settlement, suspension, restart and changed ownership clear previews.
- Native ACP sessions, prompts, content, pending decisions, cancellation and
  reopen through canonical public owners. Extracted retained image normalization
  independently; ACP does not delegate into the retired runtime.
- Native MCP CLI management and model-free tool hosting. Explicit all-empty model
  selection overrides configured host defaults while ordinary model prompts fail
  closed. A configured model requires its nonempty name. The endpoint retains the
  ten supported tool aliases through a small explicit adapter, workspace read
  grant and independent interactive denial; there is no automatic-mode bypass.
  Unknown admission stops further calls and preserves the exact owned session.
  Shutdown joins accepted work before deleting only its endpoint-created root.
  MCP bounds input lines to1MiB, arguments to512KiB, batches to16, concurrent
  admissions to16, output to8MiB and blocked writes to5seconds. Shared command-owned
  stdio preserves ACP's10MiB frame limit and joins without stopping the host.

The real browser fixture exposed a suppressed second Starlark print before a
blocking host call. The regression first failed with a held real executor.
`ecd59173f` (integrated `89153b792`) flushes changed cumulative output before the
host request, with unchanged byte limits and no timer/goroutine. Both-engine held
host-call and two-print human-question settlement/cancel/restart regressions pass.

Full `task check:phase` at pre-repair `c834d935e` passed build/vet/fast and every
native Go race suite, including store321.388s and runtime406.793s, then failed in
SDK creation recovery: the new model-free contract fixture changed an implicit
first-fixture selection. The identity assertion remains; `61ce78fca` selects its
mixed-case fixture explicitly. The earlier run remains a failure, not an overall
phase pass. Log: `/tmp/whip-observation-phase.log`.

After integrating the MCP/stdout leaves and fixture repair, full affected package
race/shuffle passes: ACP22.038s, MCP21.244s, process120.899s. Log:
`/tmp/whip-observation-protocol-process.log`. The repaired complete client gate
passes, including180 SDK tests, native CLI, every production-process fixture and
the four retained regressions. Log: `/tmp/whip-observation-clients-repaired.log`.
Analysis then reported three Go1.27 embedded-literal simplifications in ACP
presentation tests. Promoted literals preserve all assertions; focused presentation
tests pass. Repaired pinned analysis passes with0 new lint issues against the
frozen baseline and no vulnerabilities. Log:
`/tmp/whip-observation-analysis-repaired.log`.
Phases5–7 remain incomplete, including native TUI, final client/packaging gates,
retired-core deletion, representative live-provider and release evidence.

### Hosted CLI gate partition — 2026-09-29

At `58ddcaf345da7905418cb9beaa13f390eb024bcf`, hosted run `36538731438`
passed analysis and all build/race jobs on Linux and macOS, plus macOS clients.
Linux clients hit the existing three-minute aggregate CLI deadline after native
ACP/MCP increased that selection from115 to140 tests. The current test had run
for nine seconds; there was no preceding assertion failure. The run and aggregate
remain failed. The CLI gate now runs the115 existing CLI tests and25 protocol
host tests separately, with the same three-minute deadline for each. Compared
the actual discovered test names: their union exactly matches the old140-test
selection and the groups are disjoint. No scenario or assertion was removed.

Repaired `task check:native-cli` passes: CLI race48.797s, ACP/MCP race18.889s,
compiled CLI integration21.284s, and vet. Log:
`/tmp/whip-observation-cli-split.log`. Exact-head hosted rerun remains pending.

### Cold Linux CLI readiness and complete family gates — 2026-09-29

Run `36540585469` at `90032ecf9` still failed Linux clients. The115-test
non-protocol group exhausted its three-minute total deadline; its next auth
fixture was still constructing a fresh schema. Separately,
`TestDesktopManagedDiagnosticsAndApprovalCLI` hit its five-second fixture startup
wait and then reported schema initialization cancelled during cleanup. This is
not credited as a pass or merely hidden behind the protocol split.

The desktop fixture now uses the same15-second readiness allowance and25ms poll
cadence as production `localruntime.Start`, instead of a shorter five-second wait
and millisecond filesystem polling. CLI groups are now auth24, lifecycle37,
run/catalog/native54, and protocol25. Actual discovered names prove the four
groups are disjoint and their union is exactly the original140 selected tests;
each retains its three-minute deadline. Local auth13.189s, lifecycle6.173s and
run32.965s pass. The subsequent protocol run exposed a separate real ACP startup
data race between `SetAgentConnection` and the first outbound update (shuffle
`1790669959083801000`). This run remains failed; the connection publication fix
and repaired protocol/full CLI validation are required before publication. Log:
`/tmp/whip-observation-cli-families.log`.

### ACP publication repair and complete CLI rerun — 2026-09-29

The ACP SDK starts receiving inside connection construction, before the bridge
setter runs. Commit `4de3980e9` replaces the unsafe nullable connection read with
one immutable publication barrier. Early updates/permissions wait instead of
being dropped; cancellation and bridge close release/join those waits. Nil,
repeated and post-close publication are rejected. The focused regression passes
20 race repetitions; both real CLI startup/EOF paths pass 20 repetitions each
under the failing shuffle seed, and the complete ACP race suite passes. This is
a production race repair, not a timeout adjustment.

The first complete CLI rerun (`/tmp/whip-observation-cli-final.log`) then exposed
an unbound test-only bridge in the retained provider-instruction fixture. It had
relied on silently discarded notifications. That fixture now binds a real ACP
SDK connection with an explicit notification sink, closes/joins it, and preserves
every actual provider prompt/source-freshness assertion. Five repetitions under
the failing seed pass8.530s. Final complete `task check:native-cli` passes:
auth13.255s, lifecycle6.176s, run/catalog32.805s, protocol19.043s,
compiled native CLI18.170s and CLI vet. Logs:
`/tmp/whip-observation-acp-context-repaired.log` and
`/tmp/whip-observation-cli-final-repaired.log`.

Hosted run `36540585469` at `90032ecf9` is complete and failed: Linux clients and
the required aggregate failed; every other Linux/macOS build/race/client and
analysis job passed. Those are historical outcomes, not repaired-head evidence.
This publication includes the complete family/readiness changes and ACP repair;
all required hosted checks must rerun at the new head. Phases5–7 remain incomplete.

Shared app adoption checkpoint (not yet a complete product build): runtime owns
uniform root/child SDK view leases,16 views with256messages/4MiB each, revision
invalidation from the SDK catalog, a locked v4 recovery journal and explicit
command acceptance/status presentation. Immutable content upload preserves exact
owner/reference/digest; retained composition tests now use v4 content references.
App draft and reading ownership remains unchanged. Focused runtime/recovery/input/
composition tests pass52; SDK binary upload/identity additions pass107. Logs:
`/tmp/whip-app-core-v4.log`, `/tmp/whip-content-sdk-tests.log`.

Runtime test disposition: draft/storage/StrictMode/deletion behavior is retained;
legacy root-vs-child reader methods are replaced by uniform session leases;
legacy connection internals move to real v4 HostConnections tests; old silent
journal eviction is deliberately replaced by cross-window capacity rejection.
Lost ACK, absence, identity-only evidence, concurrent checks, explicit retry,
accepted-but-unsaved recovery, authoritative interruption, disposal and late
navigation have v4 replacement coverage. Full renderer types/component suites
remain pending while surrounding legacy consumers are ported.

### Native app controls and trace ownership — 2026-09-28

App adoption integrates native navigation actions (`9d832c842`), inspector and
diagnostics checkpoints, and native permission/turn notices (`2348f823a`). The
combined inspector/content/REPL/navigation/permission/turn suites pass 59 React
tests after rebuilding the merged SDK (the first diagnostic run used stale
compiled SDK output and failed two tests). The full app remains mid-cutover;
conversation/composer, sidebar and terminal wiring are not claimed complete.
Trace lifetime integration adds per-pane bounded root observers and explicitly
disposes suspended session/execution/trace owners when closing during recovery.

Trace/session runtime lifetime and workspace reconciliation tests pass 40 cases
(2.90 seconds); affected source files also pass strict type checking within the
still-partially-migrated application. Remaining legacy-screen errors continue
to prevent a full app type/build gate and are not waived.

Native skill suggestions pass 26 tests across retained composer interaction
behavior and actual validating SDK metadata reads: exact immutable host scope,
selected child identity, eleven-request/1,024-record limits, disabled metadata,
prefix fallback, malformed continuation and cancellation. Owned source types
are clean; composer/welcome callers are still being migrated to these props.

The native context picker now reads exact selected-session workspace and skill
metadata through the validating SDK, bounds results to 32 candidates, preserves
truncation, and disables stale/offline selection. All three focused tests pass
(3.26 seconds), including child ownership, disabled skills, and delayed responses
after recipient changes. Its composer caller is the next cutover step.

Native composer cutover preserves shared recipient drafts, per-pane caret/focus,
IME and slash completion, queue/steer delivery, and admission recovery. It uses
the exact native selected session and explicit tree root for attachments. Pause
verifies canonical turn ownership before cancelling and prevents duplicate clicks.
Terminal notices now retain their authoritative turn ID; composer feedback yields
only to that exact recipient/turn, replacing the old error-text/sequence heuristic.
The combined composer, submission, runtime and picker suites pass 96 tests
(3.75 seconds), with affected source types clean. Conversation callers and the
remaining application still need migration before the complete frontend gate.

New-chat workspace descriptors now persist validated immutable agent references
with copied/frozen revisions. Existing mutable-name drafts are preserved as an
unresolved choice that requires explicit selection; they are never rebound to a
current definition. All 78 session-tab tests pass (2.22 seconds).

Native directory browsing retains keyboard selection, direct local picking,
remote confirmation, bounded speculative reads/cache, scroll positions, paging
and explicit truncation. Query ownership includes runtime, epoch and client;
offline/client changes cancel queued work and discard stale picker responses.
Recents use the native bounded activity-ordered roots API. All 20 directory
interaction/cache tests pass (3.40 seconds), including actual validating SDK
wire fixtures; affected source types are clean.

## Shared client reload, permission denial and terminal recovery

The app branch integrates the released mobile five-leaf native migration and terminal
recovery `823dc4d51`, followed by native root reload and independent interactive-denial
controls. Pending reload acknowledgement remains pending, survives in the existing
journal, and can be checked/cancelled by exact receipt after reopening Settings.
Terminal uncertainty retains a local pending descriptor before sending and requires
explicit current-process shell inspection/selection. No real shell or installed runtime
was touched. Native policy fixtures now include the required denial field.

At this client checkpoint, shared-app source TypeScript passed and all **109 renderer
suites / 1,314 tests passed** in25.31s (`/tmp/whip-app-parity-full.log`); the focused
reload/denial/recovery/runtime subset passed80 tests in3.77s. Mobile controls for these
new APIs and browser-driver UI are still being completed. This is not Phase6 closure.

### Shared host browser and computer controls

The browser inspector now exposes native Rod/ChromeDP selection with exact host CAS,
explicit stale-draft discard and process-environment pin display. Computer settings
expose bundled helper publication and separate enable/disable/connection controls;
selection preserves the app allow/deny rules and never starts a helper. Focused
inspector suites passed26 tests in3.94s, and shared-app TypeScript passed
(`/tmp/whip-app-hostcontrols-{tests,types}.log`). Actual browser/signed desktop
artifact coverage remains a separate pending obligation.

### Native shared-app build dependency

`build:web` now builds the native SDK before compiling the production renderer.
The shared app drops its retired SDK/protocol dependencies, and the root lockfile
also records the already-adopted native mobile dependencies. `npm run check:web`
passed, producing the38-file renderer artifact `9c5a6c6ed398da48475bb28b3058d97643038d5a10316d0795f9f88b2f84ac39`
(`/tmp/whip-native-client-web-build.log`). Existing production browser harnesses
still need their explicit native migration; this build alone is not browser acceptance.

### Standalone updater native lifecycle — 2026-09-29

`whipcode update` retains its download-before-execute/checksummed installer path
and desktop-owned update refusal, but now controls only the fresh native host.
Capture the canonical executable before installation; read bounded compiled
metadata from its replacement so the old updater cannot advertise the old build
on the new process. Inspect and select a verified runtime/epoch, hold the native
maintenance lease, recheck ownership, stop/join that exact host and start the
replacement. Verify stable runtime identity, changed epoch and replacement build.
Absent hosts remain absent; unsafe/unverified or changed owners are never signalled.
A failed restart returns an explicit installed-but-unconfirmed error instead of
promising reconnection. Client-owned update notices retain their existing path
without importing retired runtime configuration.

Disposable real native child processes prove restart identity/build, bad metadata
leaving the old owner alive, absent/unverified owner handling and old-directory
preservation. Stub installers prove download failure does not execute partial
content/acknowledge notices and restart failure does not print readiness. No real
installer, provider, installed application or user runtime was executed/modified.
Focused updater race5.453s and complete update-notice race1.616s pass; final combined
updater/ACP/MCP race22.503s, vet and Go1.27 pinned lint0 pass. Logs:
`/tmp/whip-native-update-{tests,notices,vet,cli-final,lint-final}.log`.

Required analysis now also includes the supported CLI, previously validated only
with build/vet/selected race suites. Its first expanded lint run found unchecked
intentional stdio cleanup and two unreferenced retired fixture/cleanup functions.
Those cleanup errors are now explicitly discarded, and only unused functions
were removed; retained title-fixture assertions remain. Frozen lint baseline is
unchanged. The notice package is added to active build/race/analysis gates.
