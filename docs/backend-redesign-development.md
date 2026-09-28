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

## Next Phase 4 increment: completion reports

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
