# Backend redesign development

This is the working loop and chronological evidence record for
[the redesign plan](backend-redesign-plan.md). The current source uses the native
Go runtime, protocol v4, SDK and supported clients; the retired execution roots,
legacy SDK/protocol and ambient browser/computer wrappers are removed.
The first-version engineering handoff is complete under the user's recorded
deferrals. Start with the [closeout and human verification checklist](backend-redesign-closeout.md)
for the exact candidate, failed/passing gates and deferred human/release work.
The [acceptance snapshot](backend-native-gate-audit.md#current-acceptance-snapshot)
retains earlier checkpoints without claiming unrestricted release readiness.

The dated/checkpoint sections below preserve what was true when recorded.
Statements there that clients were unported, wrappers remained, or a gate was
pending are historical, not the current implementation status. A later subset
pass does not retroactively make an earlier failed full gate pass.

## Start working

Use Go from `go.mod`, Node 24, and Task 3.48.0. From an isolated checkout:

```sh
npm ci --no-audit --no-fund
task check:fast
task check:change
```

Implementation PRs remain an unmerged draft stack against
`codex/backend-redesign` or their pending predecessors. The reusable normal
workflow owns the same supported-product gates for the redesign branches.
Use disposable homes, databases, profiles and packaged artifacts; do not attach
tests to an installed runtime.

| Command | Purpose |
| --- | --- |
| `task check:fast` | Whole-module formatting and fast regressions, complete non-race runtime suite, process build |
| `task check:build` | Fast gate, model metadata, all Go test compilation, vet and TUI lock analysis |
| `task check:change` | Build, complete complementary Go race groups, native contracts/SDK/clients/process fixtures and actual disposable Chrome |
| `task check:analysis` | Pinned lint against the frozen baseline, whole-module vulnerability scan and tidy drift |
| `task ci` / `task check:phase` | Complete normal host gate, including product UI/content/settings, docs, evals, distribution and applicable mobile/Desktop/driver checks |
| `task check:fixture` | Production native process, gateway/executor/shell/computer/browser, self-host MCP and packaged-worker fixtures |
| `task dev:fixture -- --minutes=10` | Disposable native web/mobile product fixture |
| `task dev:v4 -- -directory /tmp/whip-example/state` | Scripted native runtime with explicit private storage |

The optional repository pre-commit hook runs the same lightweight `check:fast`
on every branch, with pinned lint when the contributor has opted into lint
tooling. Full slow product/race gates remain explicit. `task hooks` installs
this local Git configuration only when desired.

## Active scope

[Taskfile.yaml](../Taskfile.yaml) discovers every remaining Go package with
`go list ./...`; no redesign allowlist or retired test package is excluded.
Store, runtime, TUI and CLI race partitions are complementary, including future
test names/examples in their remainder groups. The source of package ownership
is [the domain contract](backend-domain.md); old-suite replacement evidence is
[the retirement disposition](backend-native-core-retirement.md).

One native TypeScript contract and SDK serve the shared app, web, Desktop,
mobile, examples and browser executors. All supported clients are in normal CI.
`check:invariants` preserves admission-before-execution, queued child restart,
settlement without replay and nonblocking cancellation/capacity guarantees from
the former daemon regressions. The old fixture remains only in Git history.

## New v4 disposable fixture

[v4-fixture.test.mjs](../scripts/redesign/v4-fixture.test.mjs) builds
`cmd/whip-runtime`, uses a private `/tmp/whip-v4-*` directory, and runs the real
scheduler/runner/store/RPC through `@whip/sdk`. It explicitly builds the production
binary with `-race=false`, including its memory-limited engine subprocesses.
Required Go package suites separately exercise backend and engine code with
`-race`; the runtime integration suite combines a race-instrumented host with
production workers.
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
SIGINT/SIGTERM. The scripted provider is deterministic fixture input; production
workers and native authority still own any engine/tool execution.

## Historical retained fixture (removed at core cutover)

The following describes the earlier comparison fixture, not a runnable current
command. Its native replacement is above and in the retirement disposition.

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

The [historical acceptance scenario](https://github.com/context-labs/whip/blob/c9db05507e37e963d6cdf7dd8a51a1f1f49f449c/scripts/redesign/fixture.test.mjs) verifies:

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
subsequently established lost-acknowledgement and queued-input crash behavior;
mid-effect recovery was still a Phase 3 obligation at this historical checkpoint.

## CI

[ci.yml](../.github/workflows/ci.yml) owns the complete normal gate;
[backend-redesign.yml](../.github/workflows/backend-redesign.yml) calls it and
retains the branch-required `redesign` aggregate. Required Linux/macOS jobs,
platform jobs and cross-compilation targets must succeed; failed, cancelled,
skipped or missing dependencies fail the aggregate. Coverage is diagnostic,
without a rewrite-wide percentage floor.

Lint remains v2.13.1 against frozen baseline
`e3fed9c91918d9c36766dd47d878c1b5466238d1`; the baseline does not advance with
checkpoints. Vulnerability checks have no baseline exclusion. The old baseline
findings and original ruleset setup below are historical evidence, not current
package exclusions or a fresh read of remote branch settings.

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

### Native mobile manual acceptance fixture — 2026-09-29

The manual mobile runner now builds the production native host and uses the same
bounded v4 provider/executor fixture as web. It explicitly creates its sample
root and prints its real runtime/session identity; it never loads the retired
SDK fixture. Optional HTTPS proxy setup accepts one exact origin and its Host
authority alongside the owned loopback listener, with no wildcard or proxy
configuration. The fixture exposes its actual process exit and joins/removes
its own resources on SIGINT/SIGTERM. Its independent lifetime remains1–30minutes.
Single/batch questions execute actual `user.ask`, including multiple/custom text
and optional dismissal from another client. The old synthetic optional question
had bypassed the production2–6-choice validation; the real fixture supplies two
choices while preserving custom text and skip behavior.

Both manual-fixture regressions pass4.323s (origin/Host rejection, exact native
question settlement and cross-client convergence, CLI identity and SIGTERM
cleanup). Existing native fixture regression passes4.782s, including real engines,
consent, restart and structured final output. The full five-case mobile backend
acceptance passes15.556s after moving its child HOME/XDG/shell/temp environment
into disposable directories. Mobile types pass with explicit test NODE_ENV.
Logs: `/tmp/whip-mobile-{manual-native-repaired,backend-isolated,fixture-types-repaired,native-fixture-regression}.log`.
The initial local manual run failed because its isolated checkout lacked the
documented packed web asset prerequisite; no backend claim was credited to that
run. Packing the actual renderer restored the expected discoverable web surface.
No physical device, private-network proxy or real-provider evidence is claimed.

### Native product integration and required client gates — 2026-09-29

The client integration retains the released app/web, desktop, mobile, SDK,
examples, client-notes, standing-instruction and updater checkpoints. No installed
runtime or original development checkout is involved. Product gates are now
required by the redesign aggregate: web, Chromium/Firefox browser, mobile,
Apple Silicon desktop, native examples and public docs. The browser gate includes
sidebar, layout, canonical large history, bounded provider streaming and tool
output; mobile includes the native manual fixture and both Expo exports. Examples
run the actual18-case agent acceptance command, not its exported helper module.

Integrated evidence, with exact local logs:

- Complete product web:1,330 app tests/110 suites, UI38 and support/type/build
  checks pass (`/tmp/whip-product-web-final.log`). Native broad/snapshot/search/
  tabs/user-message browser runs pass in Chromium and Firefox after repairing a
  fixture's per-browser one-shot hold identities
  (`/tmp/whip-product-browser-repaired.log`). Sidebar11 Chromium cases including
  all66 themes and10 Firefox cases pass (`/tmp/whip-renderer-sidebar.log`).
- Layout first failed Firefox because it advanced pagination before the first
  page arrived. The one-line first-page wait preserves all assertions; integrated
  Chromium14 and Firefox14 workflows pass
  (`/tmp/whip-renderer-layout-repaired.log`).
- Desktop types,157 product tests,116 distribution/startup checks, native fixture,
  ad-hoc package, actual Electron Browser/onboarding/workspace/failure/terminal/
  editor IPC checks pass (`/tmp/whip-product-desktop.log`). These are disposable
  staged Apple Silicon artifacts, not signed installed-release evidence.
- Mobile219 tests/34 suites, five native backend cases and iOS+Android Expo exports
  pass (`/tmp/whip-product-mobile.log`). The integrated native/manual fixture
  checks pass4 cases (`/tmp/whip-renderer-fixtures-integrated.log`). No physical
  device or application-store acceptance is claimed.
- SDK182 tests and contract/actual interchange/generation drift pass
  (`/tmp/whip-renderer-standing-sdk.log`,
  `/tmp/whip-renderer-standing-contract.log`). Standing instruction/protocol
  races pass4.604s/7.836s; focused runtime/RPC8.950s/2.884s. Client/ACP races pass
  8.974s/29.147s. TUI remains an unrouted native controller/control foundation;
  the actual CLI TUI route and retained dialogs/rendering are still required.
- Agent examples5 unit/type tests and18 actual host acceptance cases pass
  (`/tmp/whip-renderer-agent-acceptance.log`). Browser/Node examples pass types,
  recovery-storage tests and actual native transcript/content/root-child/live-cell,
  SIGKILL/reconnect/draft, permission/question, dropped-ACK exact recovery and
  Unix/HTTP workflows (`/tmp/whip-renderer-client-example.log`).
- Public SDK guide retains10 standalone checked TypeScript examples; integrated
  docs types,79 tests and static build pass, producing4 prerendered pages/65 public
  files (`/tmp/whip-renderer-docs-final.log`). The SDK page remains draft; its
  deliberately skipped public-route browser test is not credited as a pass.
- Client notes exposed a real first-use concurrent lock-file ENOENT. The repaired
  native open/create path passes25 repetitions of16 race cases (400 total),8.487s
  (`/tmp/whip-renderer-notes-repaired.log`). Expanded analysis including native
  update, notes and CLI passes with zero new lint findings against the unchanged
  frozen baseline and no reachable vulnerabilities
  (`/tmp/whip-renderer-analysis.log`). Two dependency advisories reported as
  unreachable are not represented as an advisory-free dependency graph.
- Canonical history fixture verifies10,000 root messages,100 children×100 messages,
  128 operations and1.4MiB owner-scoped content. One explicitly synthetic canonical
  selected compaction covers9,996 messages, retaining all raw UI history and a
  bounded active model context; an actual subsequent provider stream verifies it.
  Integrated check passes19.869s (`/tmp/whip-renderer-native-history-context.log`).
  Sixteen actual concurrent provider streams plus exact queued/cancelled probes
  pass (`/tmp/whip-renderer-performance-fixture.log`).

The native web performance harness is integrated with truthful measurements:
root/child switches, anchors, near-limit32 drafts,16 actual providers and40 real
queued admissions, without event injection or a fabricated SQL commit clock.
The measured root anchor restoration median is about5.1 seconds despite content
appearing in about100ms; this remains an unresolved performance target, not a
successful old latency claim. Desktop performance and remaining specialized
production browser harnesses are in progress. Final TUI adoption, full retired-core
removal, all-target gates, exact hosted success, signed/fresh-install/remote-platform
and live-provider acceptance remain open. Phases5–7 are not marked complete.

The canonical frontend guide now describes current native state/recovery owners
and actual app retention limits, removing repeated appendices and stale legacy
APIs. Relative links and unique headings were checked against source; feature
obligations remain in the plan/audit and are not retired by this consolidation.

### Client checkpoint rebased onto repaired backend — 2026-09-29

Client commits were replayed onto backend`239f76152` with a backup at
`codex/backend-redesign-renderer-before-cli-repair`. The entire source-tree diff
against that backup contains only the intended native CLI family split, ACP
provider-context fixture binding and desktop readiness fixture, plus progress-log
ordering. Duplicate backend patches were omitted; no client implementation was
reconstructed or discarded.

The first combined CLI run then failed in the retained main update-dispatch test:
its long macOS testing directory exceeded the native Unix socket bound. That
fixture now uses an explicitly owned short temporary home (the real installer is
still replaced by its existing inert shell), matching other native CLI fixtures.
The production path bound is unchanged. Separately, updater-only scenarios are
now required instead of relying on test-name prefixes to happen to select them.
Final full native CLI gate passes: auth12.623s, lifecycle6.399s,
run/catalog36.953s, ACP/MCP20.229s, updater2.822s, actual compiled CLI20.522s,
full retained/native TUI race21.184s, and vet. Earlier failed run is retained at
`/tmp/whip-renderer-cli-integration-final.log`; repaired evidence is
`/tmp/whip-renderer-cli-integration-repaired.log`.

Hosted backend run36543645270 at exact`239f76152261484e4bc2c61dc1e8e7141fcb5f94`
is in progress. The new client checkpoint still requires all hosted native and
product jobs. Specialized Settings/provider/conversation and desktop performance
harnesses continue independently. Distribution acceptance still exposes the
retained long-home socket-fallback contract and managed-gateway failure semantics
for explicit reconciliation; they are not waived by short-path fixture success.

### Clean product-gate repair after draft 265

Hosted backend run `36543645270` at `239f76152` failed only the Linux clients
job and aggregate: the combined run/catalog CLI invocation exhausted its3-minute
aggregate timer while a retained daemon test had run3 seconds. Renderer run
`36544662706` at `7afc0d691` independently hit the same group deadline, this time
in `TestRunNoSession` after11 seconds. These are failed runs, not green evidence.
The group is now split by actual lifecycle ownership into retained daemon/model,
ordinary native run, and remaining native/catalog/browser tests. Each still uses
race detection, one count, shuffle, and the original3-minute bound. An exact-set
comparison against `go test -list` proves all58 tests at the renderer head occur
once in disjoint7/23/28 groups (four additional tests since the54-test backend
checkpoint). Under the recorded failing seed `1790671198575510378`, all three
local groups passed in3.152/20.563/16.450 seconds.

The first clean product jobs also exposed missing setup concealed by existing
local build outputs: web type checking ran before SDK emission, docs requested a
nonexistent protocol `build` script, and desktop packaging lacked Electron's
explicit binary install. Web checking now builds its declared inputs before
checking shared-app types; docs runs the real protocol check; the desktop CI job
uses the explicit Electron installer already required by the retained desktop
workflow. No product assertion was removed. In a fresh exact-PR worktree with
`npm ci`, web production build/types, protocol checks, all79 docs tests, docs
build (four pages/65 files), and all18 web-pack tests passed; the exact Electron
license and executable inputs are present. Hosted validation of this repair is
pending. No installed application or runtime was touched.

### Hosted product gates after clean setup repair

At `9176615a2`, run `36545994800` passes the repaired web and documentation jobs,
mobile, examples, browser acceptance, both clients jobs, and every completed
native build/store/runtime/analysis job. The macOS other-race job remains pending
at this record. Desktop packaging now succeeds and its native browser probe
reaches the production zoom/focus assertions; it then fails an immediate check
that a guest100ms timer fired within a fixed130ms sleep after observer cancellation.

The compiled bundle maps that failure exactly to the retained cancellation-effect
assertion. The probe now observes that original effect within its existing bounded
fixture lifetime; it retains `outcome_unknown`, checks `delivered=true`, and never
reissues the command. The real isolated Electron suite passes `NATIVE_MANAGER_OK`
with production BrowserManager, IPC, preload, debugger, transfer and close paths.
No production timeout or behavior changed. New hosted validation remains required.

Run `36545994800` has now completed: every backend/Linux/macOS/client, analysis,
web/browser/mobile/examples/docs job passed. Only the desktop job and dependent
aggregate failed at the fixed-sleep assertion described above. The next head
retains every gate and carries the exact-effect observation repair.

### Managed native gateway failure isolation and packaged browser acceptance

The retained real packaged gateway smoke is ported to native protocol/SDK4,
exact runtime/process identity, native network restrictions and owned disposable
homes. Its first run passed Chromium startup but exposed an actual parity defect:
a managed bind failure returned from hostcmd and shut down the otherwise healthy
local host. Managed gateway state now belongs to the command lifecycle;
`host.status` publishes optional bounded `web_state`/`web_error`, and a failed
listener leaves the core usable. Newly launched explicit network requests wait
for gateway readiness. Repeated start of an existing socket-only host remains
idempotent and does not change its policy; explicit restart is required.

Real detached fixtures cover ready, occupied and existing-local cases, verifying
same-epoch attachment after the gateway result and joined explicit stop. The
first schema check rejected a regex repetition beyond Go's supported bound;
using the existing JSON-schema MaxLength mechanism fixed that without changing
the4096-character public ceiling (published errors are capped at1024 runes).
Full localruntime/hostcmd/protocol race suites passed4.563/6.043/9.091s; RPC passed
78.709s; CLI launch-policy race passed1.909s. The final three-mode fixture passed
three shuffled repeats6.565s and its context-aware listener check3.370s. Protocol
types/interchange/drift, Go vet and pinned baseline lint passed (zero new issues).

The actual packaged Chromium smoke passes restricted handshake, concurrent
foreground gateways, Host/Origin refusal, Ctrl+C ownership, unchanged existing
runtime, managed bind-failure isolation, managed readiness, parent shutdown and
foreground backend-loss cleanup. Initial failed logs remain failures. This uses
only a disposable binary/home and does not install or modify the real runtime.
Long-home socket fallback and the remaining distribution update scenarios are
still pending; this does not close Phase7.


### Native long-home socket parity

The retained distribution probe requires a long explicit product home. Native
launch previously rejected it outright, even though durable storage itself fits.
The shared pure `runtimepath.Socket` leaf now keeps short addresses unchanged and
moves only an oversized socket address into a deterministic private `/tmp`
directory; the full directory digest and UID distinguish owners, while durable
state and both execution/launch locks remain in the selected home. Discovery is
read-only and independent of the caller's `TMPDIR`. Existing private-directory,
non-symlink, stale-socket and100-byte listener checks remain enforced.

Tests verify read-only deterministic selection, distinct homes, rejected public
and symlink directories, preserved regular-file occupants, exclusive durable lock
before stale-socket removal, empty-directory cleanup and real detached
start/stop/restart retaining runtime ID with a new process epoch. Three shuffled
race repeats passed runtimepath1.173s/runtime2.480s/localruntime5.932s. Existing
execution ownership/restart/shutdown and architecture regressions passed3.899s;
full localruntime/hostcmd races passed5.073/5.422s. Vet and pinned baseline lint
passed with zero new issues. The leaf is included in required package and import
boundary gates. The full installer/update probe is next; this result alone does
not claim distribution or Phase7 completion.

### Native model catalog generator and single snapshot owner

`cmd/modelgen` no longer depends on retired configuration/provider types. The
existing tested snapshot codec, all provenance/catalog bytes and license moved
unchanged into `internal/modelcatalog`; native providerhost now consumes that
leaf, removing its12,653-line duplicate catalog and second partial decoder.
Native preset declarations and the same explicit Inference.net model overrides
supply generator policy. Temporary retained config readers import the pure leaf
until their final deletion; no new native package imports retired core.

Generator, codec and providerhost race/shuffle suites pass1.772/1.617/3.207s;
the retained consumer suite passes1.597s, native import boundaries1.863s, Go vet
and pinned baseline lint pass with zero new issues. The existing offline
`modelgen -check` confirms normalized snapshot bytes and generated desktop
environment names are unchanged. No upstream download, credential resolution,
default selection or live membership inference occurred. Required package/import
gates include the new leaf and generator. Remaining retired consumers/evals and
complete CLI/TUI cutover still prevent Phase7 completion.

### Native installer and update acceptance

The retained distribution probe now speaks generated native protocol4 and verifies
session persistence through fresh public reads after each process change. It
retains every pinned-installer, fixed-product-identity, renderer manifest/digest,
private configuration, long explicit home, start/restart, update destination and
release-selection assertion. Managed readiness requires the exact build plus a
running gateway; restart/update preserves durable runtime ID and changes process
epoch. A failed update preserves both the installed fixture binary and its live
process epoch. No paid turn or direct dependency on retired SQL columns is needed.

The benchmark contract is explicit: fresh `--bench` creates no product directory;
`--bench-init` initializes only native `runtime-v4/host.json`; the next read-only
benchmark preserves its bytes, with no database or legacy configuration created.
The complete `python3 scripts/test-distributions.py --browser` passed using two
locally compiled native candidates, fixture releases and disposable homes. Real
Chromium also passed packaged UI/bootstrap, restricted handshake, socket-only and
managed network lifecycles, bind-failure isolation, parent/backend-loss cleanup,
and matching renderer digests. Both pinned installation and new-to-new update
passed, including persisted exact session configuration across restart and update.
This is local macOS arm64 evidence, not signed distribution, Linux packaging,
live-provider, remote-SSH or installed-runtime evidence. No installed app changed.

### Client parity: native Settings, TUI, desktop admission and staged workload

The next client branch integrates these reviewed leaves after the renderer CI
setup repair. Settings and provider removal now preserve active-dialog errors;
explicit attachment download verifies scoped bytes and digest before platform
saving, with cancellation on owner/reference/client/connection/dialog changes.
The production Settings probes use native hosts and protocols throughout. The
new required `check:product-settings` gate passed on the integrated source:
12 Settings, five provider, nine REPL-conversation and two stored-body workflows
in each of Chromium and Firefox (56 checks total). It builds its own renderer
inputs and records the exact manifest. Fixture cleanup also covers setup failure.
The required product matrix now separately runs Settings and full native package
installation/update on Linux and macOS; failed product jobs upload every owned
`whip-*-results` artifact directory, including session-tab recovery diagnostics.

Native TUI now has `/memory` and `/me`, owner-bound permission/question dialogs,
and bounded rich transcript/history controls. Historical paging uses exact
owner/revision/cursor identity, 64-message pages and explicit live/latest return;
late replies and rewind-invalidated pages cannot replace current history. Rich
Markdown renders bounded content without filesystem probing or terminal controls;
tool disclosure and live-only reasoning remain distinct. Rendering caps each
message and the aggregate view. Dialogs retain exact retry identity and drafts,
close after canonical external resolution, and do not invent child human approval.
ACP likewise preserves direct-parent delegation: child approval cannot mint root
authority. Full integrated ACP/TUI race runs passed 22.938/26.495s; permission TUI
passed 31.680s; final rich-history TUI passed 40.743s.

Desktop separates 32 ordinary connections from 32 persistent browser-provider
peers, with 64 pending opens and a 15-second admission bound. Exact abort/release
removes waiters before admission; purpose conveys resource accounting only.
Server admission waits before accepting the next connection and shutdown wakes
admission and active calls, avoiding early EOF and stalled close under saturation.
Focused server/shutdown races passed three repeats (11.628s); SDK transport,
adapter and desktop types passed. The integrated desktop suite passed 166 tests
(156 executed, ten platform skips). Long-home localruntime/runtimepath races
passed 5.428/1.844s after integration. The offline model generator check passed
with the single unchanged native model catalog and no retired-config dependency.

The staged real Electron performance fixture passes the complete retained
functional workload: a 10,000-message root and 100 children, bounded history and
reading anchors, 1/8/32 tabs, 16 simultaneous streams and 40 queued/cancelled
inputs, preserved drafts, rejected oversized upload, three verified uploads and
verified native saving. At most four native observations were live. Tab and
visible-sidebar summary polls are separate bounded owners. The recorded root
switch median/p95 was 188/207ms, tab switch 196/216ms. Performance remains OPEN:
one upload-time key-to-two-animation-frame probe reached 1044ms p95 and sampled
peak RSS was 1,761,200 KiB. Same-current-source web measurement reproduced a
roughly five-second anchor publication delay. Functional pass does not close
responsiveness, memory, signed release, live-provider or remote-SSH obligations.

Native `--bench`/`--bench-init` and installer/update acceptance are integrated as
documented above. Main/default TUI routing, remaining specialized browser probes,
active eval consumers, final package/entrypoint gates and retired-core deletion
remain explicit next work. Phases5–7 are not complete at this checkpoint.

### Focused-context evals use native execution and accounting

`evals/rlm` now uses the shipping native runner, provider adapter, shared attempt
budget and SQLite ledger with the existing restricted synthetic corpus host and
both actual isolated engines. It no longer imports retired agent, agentdef, LLM,
legacy configuration, RLM orchestration or tools. The exact text/handle/byte-span
checks and negative-evidence cases remain. Two stateless reviewers now make real
scripted HTTP calls through native helper accounting; all four calls appear in
the same durable ledger. A two-call budget test proves the root and reviewers
cannot bypass shared admission. Provider charges, known-free charges and unknown
prices retain distinct results. Initial fixture failures revealed incomplete SSE
headers/terminators accepted by the retired adapter; the scripted server now
produces valid complete streams. No native completion assertion was weakened.

Final shuffled race checks passed all deterministic fixtures in10.697s, including
both engines and shared-budget refusal; vet passed. Pinned Go1.27 lint against
`e3fed9c91918d9c36766dd47d878c1b5466238d1` passed with zero new issues before the
final cost-source label correction (native `prices`, not retired `estimated`).
The final narrow check is recorded on integration. A first lint invocation found
one avoidable output concatenation; it was replaced with strings.Builder. A
concurrent-lint lock refusal was infrastructure, not a passing check.

The report keeps unknown usage/cost counts and marks the retired local token
estimate null; native declared input bounds and fixture context targets are named
separately from actual single-call reported input or peak context occupancy. Live
evals remain opt-in and require an explicit native declaration directory; they
read it without discovering or starting installed runtimes. Restricted API-route
live evaluation does not claim account-managed product acceptance or actual live
provider evidence. This package now belongs to the required active gates. Python
benchmark observation and the frozen historical study adapters are a separate
remaining disposition; historical results have not been rewritten.


### Client parity checkpoint: connection admission and native TUI setup

Native TUI adds captured model/theme/settings and provider/account menus, serialized
private preference edits, owner/session navigation, and exact fork/rewind receipts.
Provider setup covers explicit route declarations, masked private keys, fixed account
approval URLs, status/discovery, and known recovery actions. First-run recommendations
require exact current canonical catalog membership and explicit host-default
confirmation; existing sessions are unchanged. Visible login polling belongs to the
joined UI lifetime. The real loopback Inference fixture proves one key mint and
read-only recovery after a lost begin acknowledgement. No real account was touched.
Menu foundation leaves passed focused race checks (final 8.004s), vet and pinned lint.
The default launch route and additional mounted command/layout behavior remain a
separate in-progress leaf; menu objects alone are not complete TUI adoption.

Browser connection establishment now admits four handshakes with 128 queued opens
per SDK realm. Cancelled/expired waiters leave before WebSocket construction, slots
release once, and established subscriptions do not monopolize the handshake pool.
The measured cause was Chromium connection throttling during concurrent metadata
reads, not a virtualizer timer. SDK 187 tests, build and the actual test TypeScript
configuration passed. A mistaken invocation of a nonexistent test-types npm script
failed after the tests; the explicit test project typecheck then passed.
Both browsers also passed all 13 session-tab workflows, including runtime restart
and lost acknowledgements. With the full staged web workload, measured root-switch
median/p95 fell from 5108/5381ms to 653/686ms; 181 preview samples were 7.6/11.1ms.
Forty queued inputs still took 1496/1874ms; polling and memory remain open work.
Performance traffic uses bounded per-method counters instead of retaining bodies.
Desktop summary polls use exact root-set classification; indistinguishable tab and
sidebar sets retain a combined 4–8 bound instead of fabricated owner attribution.

The desktop file-path remeasurement exposed automation overhead in the earlier
upload probe: constructing browser files from injected buffers caused a 404ms task.
With identical owned files (three uploads, 9,437,346 bytes), automation typing p95
was 177ms instead of 964ms. Native keyboard EventTiming still reached 144ms (browser
rounding ±4ms), and peak sampled RSS remained 1,689,424KiB. These are separate
measurements; neither the earlier automation delay nor this improvement closes
native responsiveness or memory acceptance. All full-byte/digest/size refusal,
scoped download, bounded observations and retained history assertions remain.

Hosted run 36547856726 at renderer head d9f5b854e failed three required jobs:
Firefox session-tab recovery after restart; desktop first hidden-page screenshot;
and macOS runtime import-boundary inspection (`go list` exited 1). All other jobs
passed. That head remains failed, not green. The architecture check now captures
stderr while retaining the same boundaries; the local check passed 0.583s, but the
hosted subprocess cause is unproven. The desktop fixture now waits for its actual
initial document navigation and includes safe failure diagnostics. The complete
native Electron production-browser probe passed locally, including screenshot,
control cancellation/uncertainty, IPC security, guest lifetime and native daemon
discovery. Its first local attempt had stale packaged renderer provenance; rebuilding
the exact renderer restored the prerequisite. No production deadline was extended,
mutation retried, or assertion removed. A hosted rerun remains required.

Final native eval narrow lint at the final cost-source label passed with zero new
issues using Go 1.27 and the frozen baseline. All work is in disposable checkouts;
the original development checkout, installed runtime and real data are untouched.


### Default native terminal route and integrated web gate

The default interactive command now calls `RunNative` through the public native
client. It creates a genuine root or resumes a canonical owner, preserves immutable
engine/definition identity and applies explicit model/mode edits through revision
checks. Setup/model/theme/settings menus are mounted; continued observations remain
live while a menu is open. First-run input stays a draft until configuration is
ready and never becomes a fabricated bootstrap session. Closing a menu joins its
work before navigation. Mouse/thinking preferences apply from the fresh client-v4
namespace. The CLI main path no longer imports retired config or agent definitions.

The actual PTY command test passed (3.760s), including resume, engine, definition,
initial prompt dispatch and clean observation detach. A two-turn Bubble Tea/native
socket test passed three repeats (6.244s), proving current host instructions and
environment reach the provider without client-composed prompts. Full leaf TUI races
passed (62.526s); runclient/client races passed (21.342/9.716s); focused CLI, vet and
pinned lint passed. Before this final route leaf, integrated TUI/eval races passed
54.807/10.763s and vet passed. Sidebar/REPL/paste presentation, child controls,
remaining retained commands, durable uncertain-input restart records, and richer
fork/rewind selection and attachment redrafting are still open.

The complete integrated product-web gate passed: 1,343 tests in 111 files, 38 UI
tests, all support/proxy checks, theme drift and app types. Renderer artifact was
`a00030b4ee8df51e1f213a133bda8353be18765c68bcb95db4651a98a136a34c`.
Native file-path performance harness syntax and all three scope-classifier tests
also passed. This draft is a tested increment, not completion of Phases 5–7.

### Native content, terminal input recovery and CI repairs — 2026-09-29

Hosted run 36551394891 at 4b1da3b9f completed with failures in both build jobs,
both client jobs and desktop. Every other required job passed, including both
platforms' distribution acceptance, all store/runtime race groups, native web
browser workflows, Settings, examples, mobile, docs and analysis. The earlier
Firefox restart, hidden desktop screenshot and macOS architecture check passed
on this run; the overall run remains failed.

The build gate tried `go build` on the deliberately test-only `evals/rlm` package.
It now compiles every active package with `go test -run '^$'` after the unchanged
fast checks, followed by vet. Complete local `task check:build` passed. The client
failure came from the malformed-configuration fixture writing retired config.json
and then launching the native connector with os.Executable, which was whip.test.
That recursively launched test descendants and starved later real-host deadlines.
The fixture now writes native host.json and invokes actual hostcmd.Run through its
existing injection point. It proves the native decode error and unchanged bytes
across the retained command surfaces. Both original CI shuffle seeds passed
20.935/18.668s with TERM=dumb and the unchanged three-minute deadline; vet and
pinned lint passed. No production timeout or automatic retry was added.

Desktop video recording requires Playwright FFmpeg. With an empty private browser
cache the staged workspace smoke reproduced the blank URL and missing body seen
in CI. Installing only FFmpeg in that same cache made the identical smoke pass:
relaunch, exact daemon continuity, independent hosts, settings reload, tab/sidebar
dragging, four splits and preserved window bounds. Renderer was
55f25f7a3550f673e25fb38bb993cdf2fb804aae50686ab4662c410f6f3cc6a7;
this is a dependency isolation check, not a claim that those assets contain every
later renderer leaf. The redesign workflow now installs the same FFmpeg dependency
as the existing desktop workflow. Failure capture is bounded and preserves the
original attachment error. Logs: /tmp/whip-desktop-missing-ffmpeg.log and
/tmp/whip-desktop-with-ffmpeg.log. No installed user runtime was changed.

Native terminal inputs publish their exact immutable request before dispatch into
a private, bounded client-v4 journal. Reattachment checks receipts without sending;
/retry retains the original identity. Publication failure retains the draft. Only
the matching confirmed receipt clears a record; a rejected input restores the draft
or remains separately recoverable if the user has already typed new text. File and
directory durability, nonblocking cross-terminal locking, private/no-follow paths,
aggregate bounds and joined lifetime are tested. Integrated TUI/client/runclient
race suites passed 61.750/8.300/21.568s. Non-input uncertain controls and inspection
of journals whose owner was deleted remain explicit follow-ups.

Native content acceptance now runs real v4 RPC and actual MCP image results in both
Chromium and Firefox. Composer/drop and stored-content suites pass with exact-ref
retry, held-upload cleanup, owner isolation, 16-file bounds, verified large images,
zero page/CSP errors and zero anchor drift. Collapsed tool results do not read full
bodies. Verified PNG/JPEG/WebP/GIF images display only after an explicit bounded
read; stale or unmounted owners revoke their local preview URLs. Canonical file
activity displays the captured path while preserving permission scope in details.
The scenario-by-scenario map is apps/web/scripts/native-content-audit.md. Complete
integrated product-web passed 1,352 tests/111 files, 38 UI tests, types and support
checks; renderer7d04f55eb29b71ec69b67f355105deebfe8deeddeafaa00b4e983084388b7756.

Saved-draft serialization now counts exact encoded entries once instead of
repeatedly encoding growing prefixes. The full staged workload passed with the
same 2MiB bound and three real uploads totaling9,437,346 bytes. Profiled draft
serialization fell203→17.8ms and setDraft185→47.4ms over12 upload keys. Native
keyboard EventTiming p95 fell144±4→112±4ms during uploads and104±4→88±4ms during
40 streams. Peak sampled RSS increased1,689,424→1,715,440KiB; post-work RSS fell
1,436,720→1,391,280KiB. The 50 ms target and memory acceptance remain open. Evidence:
/tmp/whip-desktop-transfer-linear-drafts/run-dVPZIA/performance.json. This is a
measured improvement, not completion of Phases5–7 or performance acceptance.

### Settings preview read attribution — 2026-09-29

At ccf8310e7, both hosted build jobs pass the corrected test-only package compile.
The Settings job failed its fetch-free density assertion (five reads versus four).
The app deliberately retains a released conversation lease for30seconds; the
probe changed density while that old lease could still poll. The probe now reloads
the saved Appearance route before measuring, joining the old page while retaining
the workspace/draft. It still requires zero transcript/content reads caused by
appearance controls. Both Chromium and Firefox pass all12 Settings workflows,
including workspace/draft restore, density, persisted appearance, accessibility,
provider-save guards and responsive theme controls. Logs/artifacts:
/tmp/whip-settings-lease-repair.log and /tmp/whip-settings-lease-repair-results.
No product polling policy, deadline or read assertion changed. The earlier hosted
run remains failed; client/desktop results and the next exact-head run remain
separate evidence.

### Native Python evaluation and public package entry points — 2026-09-29

Python trials now create a private native-v4 host/tree with explicit delegated
workspace authority and observe native turns, inputs, operations, permissions,
goals, mail and the immutable attempt ancestry ledger. Finality uses database
commit quiescence plus verified exact-process freeze and matching settled readback.
Scoped content is exported with byte/digest checks. Exact integer nanodollars
remain authoritative; missing usage, cache usage and cost remain unknown. Reports
verify state.json against the copied database. New trials use a separate baseline
track; archived results and frozen source hashes remain unchanged. Historical
analysis readers are read-only and never execute the retired runtime.

Full deterministic qualification at6497bc957 passed all eight combinations of
Harbor/Pier, Starlark/QuickJS and shared/separate verifiers, including Pier's
no-network tasks. Every trial passed hidden grading, accounting/evidence checks
and owned cleanup with zero external model calls. The Linuxamd64 candidate ran
on an arm64 Docker host; this proves emulated correctness, not native performance
or model proficiency. Evidence:
/private/tmp/whip-native-eval-observer/evals/artifacts/doctor-20260929T101726Z-dca1fc2967/doctor.json.
The integrated backend separately passed both-engine offline Linuxarm64 tests,
including native report normalization/tamper rejection, in13.001s. Canonical
Python checks pass87 tests (optional Linux acceptance skipped on macOS); retained
native/historical checks pass77. Full scenario mapping is evals/native-observer.md.
Required Linux CI now includes this native evaluation gate.

The first doctor attempt stopped before a trial because dirty source capture
rejected the repository's internal skill symlink. The preparation fix preserves
internal relative links as archive metadata, rejects absolute/escaping links and
never follows an external target; extraction and negative paths are tested. A
clean frozen-source run then supplied the eight-way qualification above.

Root npm generate/build/check/test/acceptance/browser/package commands now target
the supported native protocol and SDK. The package gate installs private4.0.0
archives outside the repository, checks all public imports, browser bundling and
positive/negative consumer types, then submits a real native input and verifies
history. Its first fixture run failed because renderer assets had not been
packaged; test:package now includes that prerequisite and passes. Eight existing
native acceptance families passed62.275s. Complete integrated product-examples,
including packed SDK, both agent examples and the actual browser/Node client,
also passes. The broader legacy reference gates remain explicitly named until
final removal; this checkpoint does not claim Phase7 completion or new Safari,
signed-distribution, real-account or remote-SSH evidence.

### Native terminal and browser workflow follow-through — 2026-09-29

The native terminal now mounts goals/formulation/resume, durable schedules,
compaction history and summary selection, and explicit MCP/browser/computer/LSP
status and controls. Mutations retain their original owner and CAS payload;
helper inputs use the existing private input journal. Unknown host lifecycle or
integration effects require status inspection, not generic replay. Read-only
status never starts a helper or acquires agent authority. Compaction off clears
the helper selection; it does not disable automatic context compaction.

The agent tree, sidebar and narrow dock use canonical session lineage and exact
activity. Metadata reads stop at eight pages/512 owners; only eight visible
owners receive activity reads. Lifecycle active is not inferred to mean running.
Per-owner drafts are bounded to16 owners/1MiB total with refusal before a switch
would lose text. Real root/child/grandchild tests cover navigation without stopping
host work, pending input on a stopped child, foreign-tree rejection, child-only
deletion and non-replay of lifecycle controls. Integrated native TUI race/shuffle
passed68.735s before the tree leaf; the integrated tree/layout scenarios then
passed7.090s. The leaf's full native suite passed72.723s. Remaining terminal REPL,
paste/attachment, copy/palette/completion and richer history navigation work is
still tracked; no retired TUI implementation has been deleted.

SDK cell operation rows now sort by exact nanosecond timestamps with opaque ID
ties, preserving API keyset pagination. Actual Firefox exposed ID ordering that
split three chronological file reads around a later spawn. Tests cover2500
timestamps beyond JavaScript's exact-integer range and equal-second fractions.
The combined SDK suite passes189 tests; focused app tests pass42 plus app types.
Actual Chromium/Firefox activity probes each pass12 workflow groups, including
real cells/operations/child wait, live-only reasoning, reading intent and bounded
rows (maximum33 mounted for128 operations). These do not claim Electron400%
zoom or native desktop drag coverage.

Queue acceptance uses real native inputs and immutable steering receipts. Both
browsers pass eight groups: exact original attachments, FIFO, reload/removal,
foreign-child rejection, ended-target fallback and composer/queue/agent geometry.
Collapsed queue attachments display metadata; explicit preview fetches scoped
bytes. Combined actual activity/queue/history fixture checks pass five tests in
32.240s against renderer434fd24db73351a53734d49c73f50060c2532a618c812d73d79aea766f248daf.

Native history prefetch passes Chromium/Firefox at100/300/800ms: one warmup,
no idle/resize/selection/downward/Latest-triggered crawl, bounded three-page
intent refill, stable canonical cursors and at-most2px anchor drift. Recovery
preserves exact DOM/selection/draft across held and failed older reads and
Chat/REPL switches; only explicit keyboard retry repeats the failed cursor.
Six100-message pages cross the512-message retention bound, and Latest then
fetches the canonical tail. A separate actual-host fixture proves count/8MiB
windows, independent execution evidence after eviction and an oversized body's
explicit exact-byte read. Native sequence holes do not imply missing events;
the previous event-gap arithmetic is intentionally replaced by revisioned pages.

Multiple-host acceptance passes nine workflow groups in each browser, including
three panes, per-host defaults/directories, wrong-host content and permission
denial, two actual crashes/restarts with stable runtime/new epoch, no provider
replay, explicit disconnect while accepted work continues, and remove/re-add
with draft/reload preservation. Evidence and the30-second released-lease policy
are recorded in apps/web/scripts/native-multiple-host-audit.md. Maximum steady
outstanding observations were two on each browser. Opening an attachment across
local-to-canonical confirmation still needs its separately tracked continuity fix.

Required CI adds product-content and product-activity jobs, retaining all prior
browser assertions and deadlines. This partition makes room for the additional
history/activity/queue checks without extending the20-minute job bounds. These
checkpoints do not complete Phases5–7: performance targets, remaining specialized
probes, canonical documentation, final active-import removal and exact final-head
platform/package validation remain open.

### Retired CLI daemon removal and native update acceptance — 2026-09-29

The supported CLI already used native lifecycle, protocol and account services.
This increment removes its old daemon factory, client connector, PID-only
management and separate gateway subprocess. The retired `_daemon` and
`_web-gateway` entry points fail explicitly before filesystem initialization or
prompt admission. `_kernel` remains the actual shared worker for both engines;
its descriptor now matches the native runtime binary without a retired prompt
guide digest. Neither default nor integration-tagged cmd/whip source directly
imports the retired packages. Remaining terminal dependencies are still open.

The old fixtures were audited against native replacements before removal; the
scenario map is docs/backend-native-cli-disposition.md. Native preflight and
diagnostics tests replace the mixed legacy desktop checks. The compiled desktop
update fixture now uses real private native binaries and protocol clients. It
preserves approval and byte checks, stable runtime/new process identities,
sessions, history, an unconfigured model selection, a future schedule, native
host configuration and untouched retired configuration. It verifies a lost
success response does not restart again. Reading an unconfigured owner's history
stays available without restoring a worker, as required by the native contract.
The initial fixture used @every24h, whose first occurrence is immediately due;
the final future-preservation case explicitly schedules 24 hours ahead.

The selected CLI/desktop baseline passed race checks in 8.132s. After deletion,
the full CLI suite passed shuffle/race in 73.225s, then vet and semantic
diagnostics. Compiled native update passed in 22.67s and both real execution
engines passed in 11.03s (36.573s combined race-enabled integration run). The
required native CLI gate adds compiled desktop replacement and removes only
empty retired test selections; desktop maintenance uses the native localruntime
suite. Final hosted checks and complete retired-core deletion remain pending.

### Clean-machine evaluator and cancellation qualification — 2026-09-29

Hosted run36555920516 atf26acf587 failed its new evaluator and package-consumer
jobs. The direct offline observer test omitted the synthetic INFERENCE_API_KEY
already supplied by the Doctor adapter. It now supplies only offline-fixture
inside the test context. The isolated npm consumer previously depended on cached
registry metadata, which npm ci need not retain. It now packs its exact installed
React/type dependencies as local tarballs and installs all archives with an
explicitly empty private cache. Public imports, browser bundling, consumer types
and actual native input/history pass with that empty cache.

The corrected evaluator passes87 canonical and77 retained/historical Python
tests. The existing Linuxamd64 fixture image, with networking disabled and no
external credential, passes six both-engine/accounting/export checks in16.608s.
Python3.13 also exposed SQLite connection ResourceWarnings: a connection context
manager ends a transaction but does not close the connection. Observer snapshots,
content export, backup and report reads now close their own connections explicitly;
the final Linux run has no ResourceWarning or ignored cleanup exception.

The Linux build job failed an ACP test that treated cancellation of the upstream
SDK's Prompt context as observation-only. That SDK explicitly sends session/cancel.
The replacement tests separately prove that ending the bridge caller context
preserves accepted host execution and that the SDK notification cancels the exact
host input/provider. Ten shuffled repetitions under race detection pass22.794s;
vet passes. No production cancellation policy or timeout changed.

All other client/race/distribution/Settings/web/docs/analysis jobs in that run
passed except macOS runtime-first, product-browser and product-desktop. The
runtime standing-edit test sometimes consumed an automatic-title helper request
instead of the ordinary turn it meant to inspect; its original exact-seed20-run
stress reproduced the failure. It now keeps title execution enabled, excludes
only that helper purpose from this instruction-evidence channel and checks each
captured request's exact turn ID. Thirty repetitions pass25.733s with the same
hosted seed; vet passes. Browser mobile-search synchronization and hidden desktop
guest first-frame screenshot failures remain separately diagnosed repair work.
The failed hosted run remains failed; none of these local checks establishes a
passing final hosted revision or completion of Phases5–7.

The browser transfer fixture also assumed the parent could have no messages
after child admission. A fast child can legitimately publish its completion
mail in that interval. The replacement assertion admits only that exact child's
completion reference and revision, rejects the direct transfer turn and ordinary
inputs, and retains lost-acknowledgement and cancellation assertions. Thirty
shuffled race repetitions pass in 27.777s. The earlier macOS failure in run
36556449033 remains recorded as a failure.

Mobile sidebar search now holds the actual filtered catalog response in its
fixture, proves an existing recent-result label is not filtered readiness, and
requires that response and settled loading state before the original selection
and dialog-close assertions. Chromium passes all 11 workflows, including 66
themes; Firefox passes all 10. Bounded diagnostics preserve click/query evidence
if the failure recurs. The original missed click remains an inference because
its hosted trace did not record those events. No product timeout or assertion
was relaxed. Failure injection also verifies fixture process/socket cleanup.

The desktop first-frame screenshot failure is still unresolved. A separate
disposable stock Electron reproduction also reaches the same five-second
capture deadline with a new hidden WebContentsView; this establishes first-frame
sensitivity outside Whip, but does not prove the cause of the hosted failure.
Failed rendering experiments were discarded. Exact final-head hosted acceptance
is still required.

### Terminal originals, REPL lineage and native reading acceptance — 2026-09-29

The integrated terminal adds bounded REPL navigation through exact message,
turn and call identities. Reused provider call IDs no longer hide later cell
output. Imported history cannot pretend to have local execution cells, and old
or oversized evidence requires explicit reads. Nine real turns exercise cell
navigation and foreign-turn rejection. Utility commands now use native scoped
controls: exact `!` shell commands, effort selection, context usage and captured
attempt inspection, bounded environment reports, help, and atomic private local
Markdown export. Export reads the captured canonical history through its initial
tail without growing the UI window; concurrent appends remain outside that
snapshot. Scoped attachment descriptors are included, not embedded bytes.
Context inspection does not claim a new-session token or injection audit.

Text paste preserves original whitespace before publishing the input journal.
Owner-local paste chips, including hidden original bytes, share the existing
draft bounds; capacity rejection preserves the original. The terminal refuses
to silently normalize tabs, carriage returns or oversized logical line counts.
Explicit shell commands receive the expanded original once. Native TUI race and
shuffle checks pass in 93.616s with REPL/utilities and in 96.102s after paste;
focused utility vet and frozen-baseline lint pass. Image attachments, clipboard,
copy/palette/completion and richer history editing are still in progress.

Reading-position acceptance now creates 128 actual native turns in each browser.
Chromium and Firefox pass incoming-output reading, Latest/follow, narrow touch
layouts and two explicit older pages through the full 260-message history. All
measured anchors move zero pixels against the existing five-pixel bound, with
zero JavaScript or CSP failures. The native initial window is 100 records; exact
canonical cursors replace retired event-page arithmetic.

The native dock fixture creates eight actual completed children and exercises
metadata-only navigation, separate drafts/attachment, pane reuse, inline routes,
keyboard focus and narrow/large-type layouts. Chromium passes; Firefox passed
two diagnostic runs with zero drift after an earlier 28-pixel split-open failure.
That intermittent failure remains unresolved. Bounded pre/post geometry is
retained on success and failure, and no speculative product fix was applied.
Reading-position and dock probes are now required in content/activity CI.

Hosted workflow run 36557957330 at d94e51b03 exposed a Firefox queue test that
scrolled before all admitted rows were observed and a macOS interactive-shell
inactivity test failure. Both remain under investigation. Its evaluator/package
failures predate the now-merged clean-machine fixes above. Desktop first-frame
capture, performance targets, specialized probes and final core retirement remain
open; these checkpoints do not complete Phases 5–7.

Terminal image attachment and explicit clipboard capture are now integrated.
Client-local files are bounded before normalization (16 MiB and 64 megapixels),
then uploaded as complete owner-scoped bodies of at most 4 MiB. An uncertain
upload retains the exact owner, reference and bytes for explicit check/retry;
switching owners cannot retarget it. At most eight image references enter the
existing input journal, and rejected multipart input restores its draft. Shell
commands reject image parts. Clipboard fixtures verify bounded output and joined
child cleanup; no real clipboard or installed runtime was accessed. The complete
integrated native terminal race/shuffle suite passes in 109.808s, followed by vet.
Unsubmitted upload drafts remain ephemeral on terminal exit; accepted input
records remain durable.

Attachment confirmation now keeps the same scoped preview subtree and uses
verified upload metadata in the existing query cache. An open preview survives
the original input's canonical confirmation; owner, runtime, client, reference,
digest or row retirement still closes it. Both browsers prove the same dialog
node, one upload, one body read and no additional metadata read at confirmation,
with zero page/CSP errors. The released leaf passes 106 tests across eight suites
and app types; the integrated focused components pass 15 tests plus app types.
The actual confirmation browser probe is now required in product-content CI.

The queue fixture now requires the observed canonical set size of 24 before its
single bottom scroll, then samples full-row geometry and hit-testing together.
Both browsers pass all eight workflows; the last row and viewport bottoms are
both 778 pixels and the hit-test succeeds, retaining the one-pixel bound. The
late-arrival explanation for the hosted failure is inferred from source and its
screenshot; the local run already had all 24 rows. No product policy, deadline
or visibility requirement changed. Run 36557957330 remains failed.

Final combined CLI retirement verification passes both binary builds, integration
vet and pinned frozen-baseline lint with zero issues. Lint identified one orphan
legacy title-response fixture; semantic references confirmed no remaining caller,
and it was deleted. The CLI has no direct retired LLM import either. This does
not yet remove retained terminal dependencies from the final executable.

Parent run 36559029153 at 98f75aa9e passes evaluator/package examples, all Linux
and macOS Go/client/race checks, distributions, Settings, web, mobile, docs and
analysis. The aggregate remains failed: Firefox's separate middle-click search
result did not open a page, and desktop failed an earlier guest navigation before
reaching the previously failing capture. Both need exact event diagnostics; a
successful local rerun is not treated as closure. Run 36560260360 is the current
workflow checkpoint at d800463fe, with hosted validation still in progress.


### Native terminal selection and client acceptance checkpoint — 2026-09-29

The next isolated branch, `codex/backend-redesign-client-completion`, builds on
CLI retirement `91ab222cdd7171476fff7527e8457b92da60487a` (#269). Native terminal
palette/completion, exact owner-local clipboard copy, rendered mouse selection
and individual tool-block expansion are integrated through `fbd2cbeff`.
Completion is bounded to 64 rows/256 KiB with joined deadlines and stale-owner
rejection. Path/skill suggestions use host services and do not grant authority.
Copy retains at most 1 MiB of captured loaded text, uses Bubble Tea OSC52, and
owns one bounded foreground clipboard helper until replacement/detachment.
Selection preserves Unicode and blank lines, rejects stale geometry/history,
and never changes canonical messages. Real clipboard policy remains untested;
fixture helpers prove lifetime, exact bytes, output bounds and cleanup.

The complete native TUI race/shuffle group passes in 109.396s after selection,
then vet passes. The earlier copy integration passes in 107.725s; the retained
non-native group passes in 9.010s and spacing-affected native checks in 11.276s.
Pinned golangci-lint v2.13.1 against frozen baseline
`e3fed9c91918d9c36766dd47d878c1b5466238d1` reports zero issues for TUI, bashrun and
CLI; both native executables build. Hosted run 36560260360 exposed a layout
literal ratchet failure on macOS and aggregate three-minute TUI timeout on
Linux. The native spacing now derives from theme/glyph widths. CI retains the
same complete test set in complementary native/retained groups, each with the
original three-minute deadline; the timeout was observed while a new test was
initializing its store, not as a proven deadlock.

The shell inactivity fixture now waits for an actual non-echoing PTY readiness
marker, records six received lines outside the PTY, cancels and joins its sender,
and checks the exact inactivity result. Terminal output cannot mask a broken
key activity reset. The original 250 ms inactivity and 550 ms lower elapsed
bounds remain. Fifteen race repetitions pass in 16.254s, the full bashrun race
package in 8.576s, and vet passes. The earlier macOS failure had no key-delivery
evidence; its precise cause is not retrospectively claimed proven.

SDK build, consumer types and all 201 tests pass. Encoding now uses the native
Uint8Array base64 API when available and preserves the portable fallback;
12 tests cover exact views, padding and UTF-8. In one unchanged staged before/
after workload, three 3 MiB uploads improve from 1321.5 to 993.7 ms. Renderer
identities are `434fd24db73351a53734d49c73f50060c2532a618c812d73d79aea766f248daf`
and `b9532f278f9ad208785fef5a40fe65ec156f7daaf4249557051b3f565ea7d096`.
Native streamed-input p95 changes from 88±4 to 72±4 ms, but encoding is absent
from that path and receives no causal credit. Peak RSS grows from 1,588,832 to
1,638,848 KiB; neither memory acceptance nor the 50 ms input target is met.
These are one measured pair, not a broad performance guarantee.

The integrated production renderer packs as
`a4f135049db656e01df42d51be116a08260cc76eca4ebd04c65f92ae3a1f9ccd`.
A fresh-worktree fixture attempt first failed because no renderer had been
packaged; after packing, production fixture/queue checks pass five tests in
9.517s. App and Desktop types pass. The native rejection fixture caps synthetic
provider bodies at 64 KiB and proves that a large private body becomes only the
sanitized canonical HTTP 400 failure. Integrated turn-failure leaf `ccedf663a`
passes eight workflows per browser in Chromium 153/Firefox 155: 18 themed/width
layouts each, exact child/turn and healthy-root negatives, committed cells,
reload, crash/restart, old-epoch rejection, no replay and later child success.
Exactly 11 provider attempts include legitimate child completion reports. Raw
provider-body truncation UI is deliberately replaced by sanitization, not claimed
as retained raw-error coverage. The audit accompanies the probe, which is now
required in product-activity CI.

The separate middle-click sidebar probe now holds and observes the exact native
filtered result before exercising the original popup/inspector assertions.
Both browsers pass (11 Chromium/10 Firefox workflows). The precise hosted
dropped-click cause remains inferred. Desktop fixtures now retain a bounded
navigation event ring, underlying rejection and guest geometry on failure.
An injected ERR_EMPTY_RESPONSE proves the artifact and joined process/profile
cleanup; the normal production fixture passes locally. This is diagnostic
coverage, not a fix for intermittent hosted navigation or hidden first capture.

Hosted run 36560578233 at #269's exact head fails both unsplit TUI client jobs and
Desktop's first hidden-guest screenshot; its macOS store-rest job failed before
tests because the Go dependency proxy timed out. Only that setup-failed job was
requested again after the run completed. All other product/build/race/analysis
jobs passed. The original failure remains recorded; the next checkpoint's
hosted results must be assessed independently.

Architecture/setup/gateway/mobile and SDK/app/protocol entry guides are reconciled
with actual native code. They distinguish the independent foreground gateway
from the in-process managed gateway, fresh host/client storage, bounded exact
browser recovery versus mobile receipt-only persistence, and historical device/
remote-host evidence. Documentation corrects draft eviction descriptions to the
existing implementation; no new draft policy was introduced. All relative file
links in the updated guides resolve.

Remaining obligations include richer terminal history/context controls,
shared-app standing-grant creation (listing/revocation alone was insufficient),
other specialized browser probes, Desktop failures, performance acceptance and
final old-core/package/test removal. Signed release, actual remote SSH, physical
mobile, VoiceOver, Safari and live-provider acceptance remain explicitly
unverified. No installed runtime or original development checkout was modified.

### Shared terminal presentation extraction — 2026-09-29

The structural checkpoint on `codex/backend-redesign-terminal-presentation`
starts at `7d1f6c06dca7a169c8063c2d7ca9749f0a8694e6` (#270). Shared input styling,
terminal background observations, transcript scroll geometry and selection
geometry now live separately from the retained client model. The retired model's
transcript projection and old-config theme loading have explicit legacy files.
Function bodies and public behavior are unchanged; this prepares deletion without
removing the native client's rendering dependencies. Link and Markdown helpers
remain until their native parity work is complete.

Semantic references were inspected before moving declarations. The complete
retained TUI race/shuffle group passes in 9.851s; affected native presentation,
completion, selection, palette and paste tests pass in 7.751s. Vet, native CLI
build and pinned lint against the unchanged baseline all pass (zero lint issues).
Logs are `/tmp/whip-terminal-presentation-{retained,native,vet,lint}.log`.
This extraction does not retire the old model or complete Phases 5–7.

The setup-only retry for #269 completed: its macOS store-rest job now passes.
The run remains failed on the already-recorded client and desktop failures.
#270's hosted run 36562851241 is still in progress; its desktop, activity,
mobile, web, Settings, evaluator and distribution jobs have passed, but the
aggregate is not yet accepted.

### Native history, permissions and draft notifications — 2026-09-29

`codex/backend-redesign-client-controls` builds on structural checkpoint
`76fb8b47d9153a567e7a6f10083d91cafa6ae77a` (#271). Existing tested leaves were
integrated: orphan MCP fixture removal (`682ff1acf`), actual permission matrix
(`b78a3b831`), standing grants (`608e241c7`), captured terminal history controls
(`b1b803d92`), bounded performance tracing (`6954ef0d1`), draft notifications
(`87bf016f1`) and applied-instruction audit/safe HTTP links (`e99dd39ca`).

Native rewind and fork now mount bounded dialogs around the original input and
captured history/configuration/tail. Rewind requires an explicitly stopped owner;
matching receipts gate original text, content-handle and design-context restoration.
An existing draft is preserved for explicit redraft controls. No UI action silently
stops, uploads or submits. Context inspection reports the applied turn manifest;
its separately labeled bytes/4 heuristic is not added to raw source bytes or host
schemas. The retained implementation also inspected applied context: the old
registry's “fresh-session” wording was not a fresh-preview contract. Native HTTP
links reject unsafe schemes, credentials and controls without probing host paths.
Known-local file links and remaining terminal panels are separate follow-ups.

Session details now creates exact standing grants. Roots name capability/resource;
children select an active standing grant from their direct parent. A stable grant
ID and immutable payload survive an explicit same-request retry while the form is
open. Scope is checked on acknowledgement; owner/client changes retire late results.
This is not a persistent recovery journal or implicit “remember” action. Pending
approvals remain independent. The real native permission and standing-grant probes
are now required in `check:product-activity`.

Integrated validation: the complete native TUI race/shuffle suite passes in
111.836s, followed by vet. Added applied-context/native-HTTP/retained-hyperlink
race tests pass in 3.096s. Pinned lint reports zero issues; its first attempt was
blocked by another worktree's active linter lock and the serialized retry passes.
The seven affected app suites pass142 tests in4.55s, shared app types pass, and
all eight bounded trace lifecycle/loss tests pass in10.045s. The last server-manager
fixture's type-only legacy SDK import now uses the native Client and v4 endpoint;
all existing host-manager workflows remain covered.

The actual packed renderer is
`187ddeaeefc2117ea5befac4e7c664296abc8fd3e2275fcd7c0e9aa8911e08d8`.
Both browsers pass the eight standing-grant workflow groups, including effective
writes, pending-request independence, foreign-root/missing-issuer negatives,
child delegation and revocation. All24 original permission layouts pass across
four viewports and three themes, with exact denied/allowed file effects, owner
isolation and no page/CSP errors. Logs and artifacts use
`/tmp/whip-client-controls-{app-tests,app-types,tui,vet,audit-links,lint-final,trace-tests,pack,standing-grants,permissions}`.

The notification fix first reproduced two failures: merely becoming dirty or
successfully saving replaced AppRuntime's snapshot and rerendered unrelated tabs.
It now notifies existing safety subscribers without changing an unchanged React
snapshot; actual draft presence, eviction and errors still publish changes. The
unchanged seven-group staged desktop workload passes. Compared with the prior
untraced encoder build, input-handler-to-rAF p95 improves30.5→11ms and native
keydown-to-paint p95 remains72ms (68–76ms bound;40 samples), with maximum104→88ms.
Peak app RSS changes1,638,848→1,589,680KiB in this single pair. The 50 ms input and
memory acceptance remain open. Fix renderer
`9263204a384a0329076691cb30f572e14353fa43baeb9ba6c98368f649b50c41` and artifact
`/tmp/whip-performance-draft-notification/run-9TMoFW/performance.json` retain the
exact measurement. Traced runs remain diagnostic, never acceptance evidence.

Hosted #270 run36562851241 fails both client jobs on the complete native TUI
three-minute aggregate limit, while the active Linux fork test had run1s and the
macOS model-menu test0s. No individual test failure or deadlock was established.
The next gate partitions native names into complementary A–L and remaining
native groups at the same3m limit, alongside the retained complement: no test or
assertion is omitted. Desktop passed on that run; this does not prove the earlier
intermittent capture/navigation issue permanently fixed.

The concurrency guide now describes current native owners, bounded views,
recovery, gateways and mobile lifetimes, with checked source links. Broader
protocol/runtime/feature documentation, specialized client probes, performance,
remaining terminal controls, and final retired-core removal are still required.
No installed runtime, real account, original checkout, merge or deployment changed.

### Archived search, draft tabs and remaining terminal controls — 2026-09-29

The next integration branch, `codex/backend-redesign-client-finalization`, starts
at #272's exact head `0f965d32dd8185d57b5e9620545a0ed9787fa925`. Its hosted run
[36564812096](https://github.com/context-labs/whip/actions/runs/36564812096)
completed successfully: all required Linux/macOS client, product, runtime/store
race, distribution and analysis jobs pass. This establishes that checkpoint's
required gates; it does not erase prior intermittent desktop or Firefox dock
failures or close the performance/platform obligations.

Integrated native controls include known-local file links (explicit local-client
eligibility and bounded no-follow path inspection), passive context/LSP panels,
exact child navigation, orphaned pending-request inspection/receipt checks and
explicit local forget. Pending controls never resend the payload or cancel host
work. Delayed message actions preserve double/triple selection and drag, capture
history/configuration boundaries for fork/rewind, and retain existing drafts for
explicit restoration. No action silently stops, uploads or submits.

The session-action browser matrix exposed archived roots still appearing in the
normal sidebar. Its projection now excludes archived rows before directory
grouping, while open tabs and canonical catalog metadata remain available. Search
has All/Active/Archived filters with exact cursor and owner guards. The complete
native session-action matrix passes 11 groups in both browsers, including remote
metadata changes, archived-only search, fork content without copied charges and
server deletion/local saved-view purge. Native model-budget inspection passes
all eight browser/theme/width combinations without mutating budgets or dispatching
a provider. The associated audit files retain exact renderers and artifacts.

The original activity entrypoint now uses native runners while preserving its
modes. Both browsers pass activity (12 groups) and history (4); real local-IPC
Electron passes activity (14, including 400% zoom and draggable regions). Electron
remote-URL history, composer/stored content and dock checks pass. Explicit manual
Finder drag remains unrun. The staged desktop helper now waits for its initial
sidebar and bounds inspector/TERM/KILL cleanup; four actual lifecycle tests pass
in 5.250s. This is fixture lifecycle coverage, not proof that every prior hosted
navigation/capture failure is permanently fixed.

Focused integrated validation passes: file links 1.811s; panels/context/LSP/dock
6.381s; orphaned pending records 10.009s; sidebar/actions 27 tests; actual native
sidebar one test; filtered multi-host discovery ten tests; shared app types and
CLI/terminal vet. The first complementary native TUI aggregate failed after
58.887s on `TestNativeCopyBoundsFailureAndConcurrentDetach`: its helper predicate
accepted empty bytes as a ready PID. The test-only repair requires nonempty data
and atomically publishes the disposable PID; the exact test passes 20 race-enabled
repetitions in 4.566s. Final aggregate results are recorded below when available.

Protocol v4, agent-loop and recursive-runtime guides now describe native
ownership, exact receipts, deferred descendant waits, atomic checkpoints,
explicit uncertainty and fresh namespaces. They remove retired notification,
scratch fallback, daemon-command and whole-turn retry instructions. Relative
file links in these guides resolve.

Still open: remaining terminal command/history/interactive-shell affordances,
specialized browser probes, the 50ms native input and RSS acceptance, final
retired-core/package/test removal and final comprehensive gates. Signed release,
actual remote SSH, physical mobile, VoiceOver, Safari, trusted Finder drag and
live-provider evidence remain explicitly unverified. No installed runtime, real
account or original development checkout changed; nothing was merged or deployed.

The repaired integrated native TUI groups pass independently in 58.659 s and 70.836 s,
followed by vet. The later direct command affordance leaf retains `/auth`,
`/connect`, `/mouse`, named theme/model choices, model refresh and captured rename
through existing menus and revisioned host/session controls; its focused real-host
race suite passes 11.956 s. The integrated affordance result is recorded with the
published checkpoint. Input recall and foreground-shell key forwarding remain
separate, unfinished follow-through.

The tightened title and draft-tab probes pass again in both browsers. Titles use
catalog revisions with normal native polling and no unopened history/turn reads;
all 12 draft-tab groups preserve local-only drafts, background creation, exact scoped
image uploads and explicit upload-failure recovery. The provider fixture supplies
a valid nonempty reply to image-only input. Existing last-session→blank-draft
behavior from 203695a03b is preserved, then closing that draft proves the empty
workspace. Final artifacts: `/tmp/whip-native-title-final` and
`/tmp/whip-native-drafts-tightened`; fixture/stream tests pass 4 in 5.188 s. The audit
records the review correction from obsolete `turns.page` to canonical
`sessions.turns` and both native history reads.

The native terminal-tab matrix passes six groups in both browsers, with explicit
network opt-in, byte-exact independent cursor reads, reload without open/write
replay and explicit close. The old output-sink takeover never conferred exclusive
write authority; independent native readers preserve the useful behavior. The
Claude Code theme matrix passes seven groups per browser, including exact palette,
persistence, hover/selection, Axe contrast, mobile navigation and reset. These
probes, metadata actions, draft/title and model-budget checks now join required
product gates. No supported assertion is hidden behind an optional environment flag.

Retained MCP tests now use native configuration and secret-reference types without
old agent/LLM/tool adapters. Native real-engine success/failure loops and actual
MCP endpoint coverage replace the retired loop/stdio test scaffolding. Full MCP
race/shuffle passes 20.884 s, both native engine families 10.407 s and native MCP/EOF
CLI races 6.751 s; vet and pinned lint pass. Exact failure evidence survives a failed
cell/operation, two accounted model steps complete the turn, and the remote effect
runs once. The opt-in real-account discovery test was not run; it now requires an
explicit native host directory. The still-retained pure handler adapter and optional
selfhost fixture remain part of final old-core cleanup.

A quiet unchanged staged performance run passes all seven workflow groups but
still misses the 50 ms input target: all 40 native keydowns give 72 ms p95 (68–76 ms
rounding bound). Native input delay is 1.3 ms p95, keydown dispatch 0.2 ms and input
handler 5.8 ms; these measurements do not establish the remaining rendering cause.
Peak RSS is 1,606,160 KiB. Exact artifact:
`/tmp/whip-performance-paint-quiet/run-kowyzI/performance.json`, renderer
`187ddeaeefc2117ea5befac4e7c664296abc8fd3e2275fcd7c0e9aa8911e08d8`.
The preceding concurrent run is diagnostic only. Bounded phase and memory evidence
was retained; no unsupported performance fix or acceptance claim was introduced.

Final combined renderer
`484c6721166bf35fde7ce8ebc9585d34f56a9ad9f2a278a3a517bd59266dd1ae`
packages successfully. All four affected app suites pass 38 tests in 5.67 s and
shared-app types pass. The merged native fixture/stream/desktop-lifecycle group
passes 9 tests in 14.320 s. Integrated command affordance races pass 4.917 s, followed
by CLI/terminal vet. These checks use the final combined source; prior specialized
browser reports retain their own exact renderer hashes rather than being relabeled.

The final combined renderer also passes all 11 native session-action groups in
both browsers (`/tmp/whip-client-finalization-session-actions`), including the
actual archived-search behavior. Combined pinned lint for terminal, CLI, MCP and
runtime reports zero issues. `task --list` parses the expanded required gates.
The input-recall API and further shell/keyboard work are separate subsequent
checkpoints; this branch makes no claim to include them.

Performance evidence correction (same day): the subsequent owned-window control
at `/tmp/whip-native-input-control-36zQu7/results.json` confirmed native Electron
content was 1200×800 while the browser page emulated 1360×960. The short composer's
y=834–891 was outside that content, and the near-limit composer was partially
clipped. Focus, hit tests and accepted text did not establish actual visible paint.
Therefore the earlier 72 ms readings, including the quiet run above, are diagnostic
only and do not establish fully visible native input acceptance. The 50 ms target
remains unverified. A following bounded fixture change must set the actual native
content size, assert viewport/composer containment and rerun the unchanged workload.
No product timing workaround or performance completion is claimed.


### Native terminal input, terminal environment and diagram acceptance — 2026-09-29

This checkpoint follows draft #273 (`05e41970a`) on
`codex/backend-redesign-terminal-input`. It preserves original local input recall
and adds bounded cross-session human-text recall through `inputs.recent_text`.
The query scans at most 500 canonical input rows per page before filtering,
returns at most 256 KiB of exact first-text-part content and uses the returned
ordinal cursor. Empty pages can still advance. It exposes no foreign attachment,
design, credential or execution authority. The SDK validates exact decimal
ordinals, counts, ordering, cursor progress and UTF-8 byte bounds. The terminal
retains original local parts first (32 entries / 1 MiB), then lazily reads at
most eight global pages into a 500-entry / 256 KiB text cache. Edits or owner
changes discard late reads; recall never submits work.

Escape preserves original drafts before clearing, restores root navigation from
a child without cancellation, and targets an exact active turn when appropriate.
Double Ctrl+C freezes the observed turn ID; quitting/detaching leaves accepted
host work running. Interactive shell output is a bounded passive tail. Explicit
`/shell focus` or Ctrl+X I captures owner, operation, process epoch and input
sequence. One write may be in flight with at most 16 KiB of unsent input; a lost
acknowledgement discards buffered intent and requires explicit refocus instead
of replay. Ctrl+] returns to the untouched composer. The Go client's new
`CallAtEpoch` rejects a replacement process before dispatch.

The default terminal route now uses Bubble Tea's owned background/color-profile
messages, preserving explicit saved themes and launch overrides. Unknown
backgrounds stay neutral with an actionable hint. Optional tmux/mosh hints use
bounded read-only inspection and joined helper processes. No competing raw TTY
reader or termios mutation was reintroduced. Semantic native production/test
closure confirms no dependency on retained test helpers; the nine shared
presentation files and mixed report helper are recorded for the deletion slice.

The Mermaid production probe now uses actual native inputs, held provider
streams, exact owner-scoped history cursors and canonical large source.
Chromium and Firefox pass all 12 groups each on the combined renderer
`a918fb0988b027f06423f1650712a578428004445340e479c3e924b592a4d690`.
The separate staged Electron checkpoint uses native window sizing before
interaction and passes worker/font/decode, live settlement, expansion, reload
and CSP. Its exact older renderer/runtime hashes and component incomplete-source
coverage remain in [the Mermaid audit](../apps/web/scripts/native-mermaid-audit.md).
Mermaid is now part of the required product-content gate.

The opt-in MCP self-host test builds a disposable native binary, supplies private
home/workspace paths, proves workspace reads plus exact policy denial for write
and shell effects, and stops only its verified fixture host. It no longer starts
an installed/default runtime or expects the retired four-tool endpoint.

The [native web workflow inventory](native-web-workflows.md) classifies all 216
current manifest operations with existing source owners (153 Web, 16 Internal,
47 SDK-only). SDK-only is an explicit current UI boundary, not permission to
retire a retained feature. Its test no longer reads the retired contract or a
historical plan. The first combined jsdom run exposed a test-environment mismatch
(`import.meta.url` became HTTP); this filesystem-only contract now explicitly
uses Vitest's Node environment and passes under the real shared test config.
[Tools](tools.md) and [providers](models-providers.md) now document native modules,
request ownership, route kinds, host configuration and credential/account rules.

Validation: combined recall/store/RPC/client/TUI race groups passed
3.367 / 2.360 / 2.538 / 7.194 seconds, with vet; exact cancellation passed
2.736 seconds. The full complementary native terminal groups after shell
integration passed 68.925 and 72.569 seconds. The later terminal-environment
checks passed 2.101 seconds and the actual default-route terminal/host fixture
passed 3.806 seconds. The isolated MCP self-host race passed 7.288 seconds.
Protocol validation and generation drift passed (17 tests), all 203 SDK tests
passed, the combined native/viewport fixture suites passed 10 tests, and the
native inventory/Mermaid component suites passed 11 tests. Combined pinned lint,
app type checking and Taskfile parsing also pass. Tests use disposable runtimes;
no installed runtime, real account or development checkout was changed.

The corrected quiet desktop workload verifies native content and document at
1360×960, a fully contained focused composer before/after all 40 keys, and zero
dropped observations. All seven workload groups pass, but Event Timing p95
remains 72 ms (rounded bounds 68–76 ms); handler-to-rAF p95 is 11.6 ms. Peak
aggregate application RSS is 1,581,968 KiB (about 1.509 GiB). Evidence is
`/tmp/whip-performance-native-visible/run-xl6auP/performance.json`. This supersedes
the clipped-window paint evidence, not the outstanding 50 ms/RSS acceptance.
Geometry checks do not prove OS non-occlusion or physical scanout latency.

The audit also found an unresolved retained capability: external Chrome
live/dedicated/headless/extension engines exist, but the current native runtime
only consumes offered Browser providers, and `browser install` still names the
retired config path. No approved retirement was found. Restoring explicit native
mode ownership/configuration is required; old implicit browser fallback and
uncertain mutation replay cannot be reused. Remaining REPL/slash/chat/Safari
probes, platform evidence, performance targets, complete retired-core removal
and final comprehensive gates still prevent Phase 5–7 completion.


### Retire the old terminal model — 2026-09-29

Based on draft #274 (`9bc05d632`), `codex/backend-redesign-core-retirement`
removes the retired terminal model, root-only client/dispatcher, transcript state,
setup flow and raw terminal-query implementation. Native terminal files, pure
presentation/theme/UI code and 32 mixed-file pure tests remain. The complete
[terminal behavior and test disposition](native-terminal-retirement.md) records
native replacements for command, input, shell, history, selection, permission,
provider, context, theme and observation families. Twenty-eight pure tests tied
to obsolete implementations and 305 old-model-dependent tests are removed with
their fixtures; this is not permission to delete separate browser engines.

`report.go` keeps the unchanged issue-URL helper and build version. TestMain
still isolates the client home and now removes it before `os.Exit`. Final lint
identified old write-only style globals and an unused selection-region flag;
those were deleted and theme callers use `rebuildTheme` directly. The literal
padding ratchet tightens from 33 to four; the style ratchet remains zero.

Validation on the isolated deletion tree: CLI/TUI compile passed; complete
terminal race/shuffle partitions passed in 67.394 and 74.444 seconds, including
all remaining pure tests in the complementary partition. Vet passed. The actual
compiled default-route terminal/native-host fixture passed in 3.698 seconds.
After the lint cleanup, focused pure/native theme/menu/render/selection races
passed in 5.642 seconds, pinned lint reported zero issues, and both supported
CLI/runtime binaries built into a disposable directory. `go list -deps` for
those entry points contains no retired core package. The old daemon and other
retired packages are still present pending the next deletion increment.

Hosted #273 at exact `05e41970a` completed with failures in the old terminal
literal-padding assertion (both OSes), Firefox history recovery while changing
to REPL, and Chromium queue-removal focus. Its other required jobs passed. This
terminal deletion removes obsolete padding sites without relaxing the ratchet;
the two shared-client failures remain under investigation. #274 hosted run
36570634436 is still in progress at this record. Neither prior failure is
reclassified as passing evidence. Phases 5–7 remain open for external Chrome
ownership, specialized client probes, performance/platform evidence, final core
removal and comprehensive final-revision gates. No installed runtime, real
account, original checkout, merge or deployment was changed.


### Native core removal and normal product gates — 2026-09-29

The isolated `codex/backend-redesign-core-removal` tree starts from draft #275
(`c9db05507`). It removes the eleven retired Go roots, old contract generator,
legacy SDK/protocol and old process fixture. The initial deletion is 825 files,
including 325 tests. Later cleanup removes the unused old capability dispatcher,
permission rule/cache and Desktop broker DTOs; the native Desktop error type and
filesystem/process/PTY/MCP leaves remain. The [permanent family disposition](backend-native-core-retirement.md)
links replacement guarantees and distinguishes obsolete implementation tests.
No package allowlist conceals surviving retired targets.

The increment reuses released leaves rather than reconstructing them: MCP
`6311e4398`, normal gates `24db285d6`, Chrome foundations `e47bfef2f`,
`ddf7ff0c5`, `617f0adee`, bounded retirement fix `eb802f3a7`, REPL fixtures/port
`945d6b44f`, `e0dd2fb82`, `002ca52b2`, native skills setup `f1e5c4998`,
welcome drafting `b80c793cf`, slash/skills acceptance `4bcfd6ef3`, client recovery
`20210f8a5`, native chat polish `b6e01774c`, image context repair `c0999eef9`,
and the native feature/goal guide leaves `1bc6ddfa1` and `b90173cec`.
The original checkout and designated observation-integration checkpoint remain
separate; no installed runtime, account, merge or deployment is changed.

The normal `ci.yml` now owns the complete required product graph. The redesign
workflow calls it and keeps the stable aggregate. Dynamic Go discovery and
complementary heavy-suite partitions replace temporary active-target lists;
normal pre-commit behavior is branch-neutral. The accepted plan's diagnostic
coverage replaces its temporary global floor. Pinned lint remains v2.13.1 with
baseline `e3fed9c91918d9c36766dd47d878c1b5466238d1`. Required UI packed-consumer,
Storybook/docs, integration CLI/SSH, native MCP self-host and packaged worker
checks are restored alongside existing clients/platform/distribution gates.

The dependency lock removes only the retired workspaces and their orphaned
entries, with no dependency version upgrades. `npm ci` passes. The isolated
installed UI/app consumer passes all four production/development cases using
only native protocol/SDK packages. Production dependency audit passes its high
severity gate (zero high/critical; fourteen existing moderate findings). Its first
request was rejected for potentially private metadata; a read-only repository
check confirmed this repository is public and the pruned lock introduced no new
private dependency names, after which the same audit was approved and passed.

Two previously hosted client failures now have concrete corrections. Queue
removal focus waits for canonical removal to commit in React; the regression
fails before and passes after the fix. The history-recovery test now loads actual
execution evidence before testing retention across an injected failure. Both
Chromium and Firefox pass the focused history/queue probes and the sidebar check
uses a 1/64 CSS-pixel bound for layout roundoff. It still rejects actual drift.
The terminal entry fixture now waits for distinct UI admission acknowledgements
and submits explicit `/quit` with completion closed; the host can settle before
its observer sees idle. One race run passed2.927s and ten repeats passed13.160s.
An intermediate `/quit` attempt without closing completion failed; that result
is not passing evidence or a reason to weaken active-turn Ctrl+C semantics.

Native skills import, immutable publication, default selection and an explicit
root/session grant are separate shipped commands. A first ungranted invocation
preserves an unavailable `$name` literally without capturing skill metadata/body;
an explicit grant and new invocation capture the real body. Existing sessions,
custom definitions and unrelated grants remain unchanged. Actual Chromium and
Firefox slash/skills probes pass fourteen workflow groups each. REPL probes pass
ten groups each using actual executions, 180 root cells, 80 child cells, exact
opaque pages, canonical content and crash recovery. The explicit no-provider
drafting action preserves setup and disables Send until a route is ready.

Chat polish now uses actual native child mail, execution and provider streams.
Its browser assertions caught a missing agent-updates label in mixed work and a
stretched Open in REPL button. Both are corrected; Chromium and Firefox pass
four workflow groups each, including 530/390/320px spacing, touch44px targets,
6000+ character reasoning, child/root separation and completion-only copy
footers. Renderer digest:
`3d17e57542f5d46b311e810f39b64aaad2407a76ba0f95c6db4137b68bfd98c8`.

The context retirement audit identified a concrete replacement gap: metadata-only
selection could exceed the image hydration budget before compaction. The new
metadata preflight and incremental whole-exchange helper bounds preserve raw
history, pins, exact attempts and already committed tool effects. The regression
failed before any provider dispatch, then passes with bounded folding; indivisible
current inputs still fail before body reads. The released leaf passes full runner
race26.717s, focused actual runtime/SQLite/blob race19.818s, store rollback2.936s,
and final image-pin race5.621s, with vet/build/pinned lint. This replaces old age
rewrites without claiming the same24k hot window or provider token costs.

MCP import discovery now preserves its explicit owning host source; the revised
provenance test reproduced the retired-path overwrite before the fix. Its full
race suite passed16.317s. Shared capability/tool races passed29.044/15.618s;
remaining skill/parser and native instruction races passed1.284/4.611s. All Go
test packages compile after extracting the Desktop error type from deleted DTOs.
The official protocol generator and validation pass after combining skills with
host config21. The assembled tree passes pinned lint with zero issues, all204
SDK tests, all1375 app/Desktop-renderer tests across113 files, and app types.
These are checkpoint results, not a claim that the final complete gate passed.

Hosted draft #274 at `9bc05d632` completed with both native terminal entry
fixtures failing detach and a tiny sidebar bounding-box roundoff failure; other
required jobs passed. Draft #275 run36572405711 completed with the same terminal
fixture failures, Firefox attachment confirmation reading its immutable preview
twice, and the 120-second canonical history seed timing out in workspace-layout.
Other required jobs passed. The terminal and sidebar have tested fixes above;
attachment retention and slow setup were investigated as recorded below.

The released native Chrome leaf `0f3e0461b` is integrated on the existing four
foundation commits. It exposes explicit host configuration and named root
generation controls through five public operations and SDK helpers. Dispatch
rechecks current configuration, owner and SQL operation authority. Uploads use a
separate exact workspace/path-set grant and immutable bounded private snapshots;
generic browser authority does not authorize local-file disclosure. Actual
disposable Chrome passes upload/screenshot/detach under both drivers (race4.252s)
and both worker-engine restart scenarios (8.527s). Existing offered Desktop
both-driver/both-engine coverage remains passing (14.454s). Real user profiles
and installed extensions were not touched. Shared/mobile/CLI/TUI controls and
unused old browser-wrapper removal remain the next increment.

Safari leaves `e0bb264f2` and `f30f0a407` move the actual Safari entrypoint onto
the native host. Three owned-driver lifecycle tests pass, including bounded
failed-start/hung-delete cleanup. The shared Chromium rehearsal passes five
workflow groups with no strict window errors or CSP violations. Its stricter
error observation exposed a real ResizeObserver loop; using the virtualizer's
existing animation-frame measurement option fixes it. Seventy-three reading and
timeline tests and both-browser native history recovery pass. Actual Safari
refuses before starting a runtime because Remote Automation is disabled. A
separate machine-setting permission request is pending; rehearsal is not Safari
acceptance and no setting has been changed.

Attachment leaf `f7f603373` makes immutable attachment queries statically fresh,
so runtime-wide completion invalidation does not reread their successful or
failed preview. New regressions first failed with duplicate reads, then pass
alongside explicit Retry and existing ownership/cleanup checks (20 tests).
The strengthened actual Chromium/Firefox probe holds canonical completion and
receipt recovery until the preview opens, then observes command retirement:
exactly one upload, one preview read, zero metadata rereads, stable dialog and
zero browser errors. Renderer digest:
`506ad7b4cb180f3f1dcf157fad80f06f66f333a5f79bf9b4a005d657d8dd3fe6`.

The unchanged full history seed passes macOS/arm64 in15.253s and a constrained
Ubuntu24.04/Linux-arm64 container in45.508s. The actual stopped-owner helper,
runtime restart and post-seed execution acceptance passes18.417s with all10000
root messages,100children×100messages and128operation assertions. Compilation
precedes the helper's120second deadline. Profiling shows SQL compilation and
FULL-synchronous durability cost, but does not reproduce the hosted Ubuntu/amd64
timeout. A temporary cache experiment offered no material win and was discarded.
No workload, durability, deadline, retry or production API changed. The next
hosted run must establish whether the slowdown recurs; this is not a claimed fix.

After integrating Chrome and the Safari/attachment leaves, official protocol
regeneration resolves their generated-validator overlap and every remaining Go
test package compiles. The complete normal gates are pending at this commit;
earlier checkpoint passes above are not relabeled as final-revision evidence.

Phases5–7 remain open: external Chrome client controls and obsolete wrapper
retirement, actual Safari automation permission and execution, hosted history
followthrough, corrected quiet Desktop50ms/RSS targets,
applicable manual platform/live-provider evidence and final comprehensive gates.
No passing local subset substitutes for those outstanding requirements.

Draft #276 publishes the native-core deletion at `d90668cc3`. Its first restored
normal build exposed a real Taskfile error: multiline package discovery was
interpolated directly into a shell `for` header. Checked command substitution
now preserves complete package discovery while producing valid shell syntax in
both fast and complementary-race groups. The repaired full `check:build` passes
formatting, all fast packages, the complete runtime suite, all test compilation,
vet and the UI-lock analyzer. Full-module pinned lint reports zero issues;
reachable-vulnerability analysis reports zero affected calls/imported packages
(two vulnerabilities occur only in uncalled required modules), and tidy is clean.
Protocol17, SDK205 and example5 tests pass at that checkpoint. The subsequent
strict external-browser inventory and discoverable host-tool schema fixes are
released as `481d05acc` and `c0e4d5b30` with focused evidence; final hosted gates
remain pending. Failed hosted run36577739713 is not passing evidence.

#### Required-gate follow-through and platform evidence (2026-09-29)

Draft #276's first exact-head run36577739713 at `d90668cc3` failed. Both build
and complementary-race jobs exposed the package-list shell interpolation fixed
above. Distribution jobs exposed stale workflow/native-readiness assertions;
the offline eval harness still wrote host configuration20 while this runtime
requires21. The conversation probe read a slash-command receipt before actual
admission acknowledgement, and the independent UI job had no Storybook assets.
The settings job completed both-provider controls and Chromium REPL history,
then timed out seeding the Chromium body-history scenario. That last failure is
not a Firefox result and is not fixed merely by the earlier local seed passes.

The eval fixture version repair has actual offline Linux/arm64 evidence: both
engines fail before the repair (30.838s), and all six isolation/accounting/export
checks pass afterward (13.788s). The container used only synthetic credentials
and no network. A separate timeout-fixture repair waits for an admitted provider
turn before exercising the unchanged500ms CLI recovery timeout, proving exact
input cancellation without counting worker startup against that assertion;
ten race/shuffle repetitions pass16.865s.

The independent UI gate now builds its own Storybook assets. That reaches and
passes all66 theme/14 interaction checks, strict-CSP/highlighting/portals under
Chromium, Firefox and Playwright WebKit, four packed consumers, workspace tabs
and theme accessibility. WebKit is not actual Safari acceptance. The subsequent
workspace-layout probe exposes drag-preview and reading-position failures under
investigation; the full UI gate is not claimed passing.

[Native mobile readiness](backend-native-mobile-readiness.md) records exact
`d90668cc3` artifacts and limitations: mobile types,219 tests, actual lifecycle
fixtures, Expo Doctor21/21 and both Hermes exports pass. Xcode26.6 builds the
Release arm64 simulator application with its normal simulator signing. An owned
then-deleted iOS26.5 simulator starts and relaunches it, preserving encrypted
SQLCipher files that reject plaintext reads. The Android arm64 Release APK
build passes with the repository's debug test signing; no Android device was
available. Desktop types,156 tests,116 distribution checks and ten actual
loopback-SSH transport checks pass. These do not claim connected-device UI,
physical-device accessibility, production signing or real-account acceptance.


External Chrome controls are now integrated across shared Web/Desktop, mobile,
CLI and TUI (`1d2cdee79`, `9c08e9dc3`, `f9aad8311`). Host changes require the
explicit displayed revision and a complete declaration; session controls capture
root/name/generation and children are read-only. Lost acknowledgements preserve
uncertainty and drafts rather than silently refreshing or replaying. The shared
browser probe passes four workflow groups in Chromium and Firefox and now runs
in the normal settings gate. Focused shared tests38, mobile226 tests/35 suites,
mobile types/both Hermes exports and actual lifecycle checks pass. CLI/TUI socket
and navigation race checks pass7.839/4.481s with build, vet and pinned lint0.
These focused checks precede final integrated validation.

Strict connection inventories remain non-null and bounded to four; external and
Desktop host-tool schemas are discoverable. The MCP SDK requires top-level
`type: object` even when `oneOf` defines the alternatives; `5caf16e2d` retains
that requirement and passes actual MCP endpoint tests. The complete local race
run before that repair passed store222.255/111.235s, runtime301.413/130.863s and
all complementary packages, then failed in MCP CLI registration as expected.
No full-race success is inferred from those package passes.

The obsolete computer helper lifecycle is removed (`f567bb709`): unused ambient
binary discovery, automatic retry/restart, extraction and private wrapper tests.
[Its disposition](backend-native-computer-retirement.md) retains screenshot
protocol values and verifies the actual owned connection's missing/rejected/wrong
handshake and joined child process. Complete computer race6.513s, architecture
boundary, compilation/vet and pinned lint0 pass. Native Controller/Connection
ownership is unchanged; the driver guide now describes the explicit native path.

Native gate repairs (`7ffcfd31c`, `449f9f08e`) restore Swift's real XCTest target
(13 safe tests plus release build), require actual disposable Chrome tests on
both CI operating systems, retain immutable checkout refs and validate the new
readiness identity. Distribution checks preserve aggregate failure/cancellation/
skip/missing-result rejection (36 workflow and five readiness tests pass).
The exact slash-command acknowledgement fix (`30a472f51`) fails before the change
and passes three focused tests plus14 groups each in Chromium and Firefox with
zero errors/CSP violations. Receipt lookup waits for admission and never resends.


Hosted runtime diagnosis is recorded in [the gate audit](native-runtime-gate-diagnosis.md).
The timed test was only one second old; the600second alarm belonged to the whole
partition. A separately proven fixture leak retained MCP server sessions after
HTTP teardown; its new regression fails before cleanup and the full MCP family
passes race14.012s afterward. This does not prove that leak caused the hosted
alarm. First/middle/rest now partition234 real test names82/70/82 with all race,
coverage, shuffle and original10minute flags preserved. Executed shell membership
also checks nine future/example/fuzz boundaries; CI includes all three on both OSes.

History diagnostics (`bff967547`) preserve the full durable workload and original
120/125second deadlines. At most64 bounded stderr records expose seed stages,
counts, elapsed/self CPU and OS block output; settings reports exact browser/mode
and joined cleanup. All four Settings browser scenarios pass locally with zero
page/CSP errors and previous runtime exit0 before the next scenario. The hosted
seed timeout remains open until the next run supplies equivalent evidence.

The integrated revision `1fa8dbfb2` passes protocol18 with generated drift checks,
SDK build, shared app types and1382 tests across114 files, plus mobile types and
226 tests across35 suites. Full CLI/TUI validation and the unchanged staged
Desktop packaging/acceptance gate are running on that source revision. The
original development checkout remains untouched and the designated handoff
worktree remains clean at `239f761522`.

#### Final compatibility cleanup increment (2026-09-29)

The published #276 head `b1c9ca965` has an exact hosted run36581778862; the previous
failed run is retained as failed evidence. Its underlying source `1fa8dbfb2`
now passes the complete normal native CLI/TUI gate, including compiled CLI,
update, SSH/askpass/prompt socket, both native terminal partitions, presentation
and complementary CLI tests. Full analysis passes pinned lint0, tidy and no
reachable/imported-package vulnerabilities. SDK205 and example5 tests pass.
The [complete staged Desktop gate](backend-native-desktop-readiness.md) passes
on that same source, including all166 staged-helper tests and116 distribution
checks, native browser controls, onboarding, normal/failure workspace flows,
terminal and editor IPC. It used disposable ad-hoc packaging and synthetic
provider transport; it is not signed release, real-account or performance evidence.

The next isolated increment starts from that published checkpoint. Two actual
workspace defects are repaired: a finished fill-forwards drag animation kept
its visual effect after ownership was discarded, and replacing a split child
reused a sizing cache with the old child IDs, briefly collapsing the existing
content to15–31px. That transient layout wrapped text and moved the browser's
scroll anchor from160 to320. The first fix releases the animation after retaining
its final geometry; the second changes sizing identity when immediate pane
membership changes while preserving the content DOM. Both new bounded browser
regressions fail before their respective repairs, then the complete existing
Chromium/Firefox suite passes exact draft, scroll, DOM, four-edge, zoom/RTL,
CSP and accessibility assertions. No timeout or geometry tolerance changed.

The new hosted eval gate exposed one remaining unit assertion expecting host
version20; its writer already uses21. Correcting the assertion passes the full
local offline gate (87 tests, one Linux-only acceptance skipped, plus77 runtime-AB
tests). The earlier actual Linux candidate's six acceptance tests remain recorded
separately. Three hosted jobs failed downloading/verifying Task dependencies with
Go proxy HTTP/2 INTERNAL_ERROR before their product checks began; these are not
code findings or passing gate evidence.

The standalone model-picker geometry fixture still supplied retired fake SDK
props, so its restored gate could not render a Model button. It now uses the
current typed CatalogModelPicker, the same visual component used by session and
settings controls, with native catalog metadata and no fake runtime. All30
Chromium/Firefox geometry scenarios pass unchanged. The existing app typecheck
now includes all four web fixture entrypoints, and passes without compatibility
casts in the repaired fixture.


#### Current acceptance reconciliation (2026-09-29)

The [current acceptance matrix](backend-native-gate-audit.md#current-acceptance-snapshot)
distinguishes implemented controls from final validation. Native provider/account
UI and external Chrome controls are mounted across supported clients; both
ambient wrapper retirements are integrated through `0265ab26c`. Earlier progress
paragraphs remain checkpoint history.

Combined browser-retirement revision `b8372f26c` passes the complete normal build
(fast packages, full runtime suite, all test compilation, vet and UI-lock checks),
full pinned analysis (lint 0, tidy, no reachable/imported-package vulnerabilities),
and expanded required real-Chrome race gate (browser 32.068s/runtime 19.944s).
The complete normal UI gate passes at equivalent isolated UI/model-fixture
checkpoint `d9545097d`: SDK/Storybook, all 66 themes and 14 interaction checks,
strict CSP, isolated production/development packed consumers, workspace tabs
and both-browser layout, and all 30 model-picker scenarios. No stage, tolerance
or assertion was weakened. Log: `/tmp/whip-product-ui-complete.log`.

Published parent `b1c9ca965` has a passing hosted Settings gate in run 36581778862,
job 109451534346. All four Chromium/Firefox REPL/body-history scenarios pass;
stopped-owner seed processes close in 49.576s and 49.057s, including the latter's
49.047s seed itself, with joined runtime exit 0 between scenarios. This passing
run does not explain the earlier timeout or establish an optimization. The
aggregate still fails: model-picker/eval assertion repairs were absent from that
head, three jobs failed Task dependency HTTP/2 setup before checks, and SSH proxy
fixtures on both OSes plus a later slash-selection scenario require follow-through.

The performance audit corrected an evidence boundary: the earlier visible
Desktop sample's final heap/DOM counters and after-work RSS followed forced GC.
Its peak/after-typing RSS remain natural observations, but the final counters
cannot establish natural retention. The harness now captures natural heap/DOM
and after-work RSS first; optional forced-GC output is explicitly separate and
excluded from acceptance. Seven bounded sampler/cleanup tests pass and are part
of the normal performance gate. No new performance result or target pass is
claimed until a quiet run of the corrected workload. Final-head comprehensive
checks and separate platform/live requirements remain mandatory; Phases 5–7
are not complete.

The hosted slash-selection failure is a product race: completion's delayed
animation-frame caret update can collapse a newer select-all range before text
replacement, appending instead of replacing. Restoring the caret in the controlled
value's layout commit preserves the existing owner/value/focus checks and later
user selection. Four regressions fail against the old source; 71 focused checks,
all 14 unchanged workflows in both browsers, and final integrated app types and
1,386 tests across 114 files pass (`a4400d3e1`, integrated as `c62394b73`).

The two hosted SSH failures have a distinct fixture cause. OpenSSH prefixes a
ProxyCommand with `exec`; the fixture also supplied it, which local zsh accepted
but Bash rejected with `exec: exec: not found`. Explicit Bash reproduction fails
in 0.14s. Removing only the fixture's extra prefix, using its own `/bin/sh`, and
joining/reporting bounded failed-child output passes all SSH race/coverage checks
(5.862s) and the exact hosted integration family and failing shuffle seed
(23.767s), plus vet and pinned lint 0. Production SSH and all deadlines/assertions
are unchanged. A fresh hosted Linux/macOS pass is still required.

#### Platform validation follow-through (2026-09-29)

Draft #277 publishes the preceding increment at `1edc9d823`. Hosted run
36584226063 passes all Linux/macOS race partitions, analysis, evals, mobile,
Settings and the full UI gate. It exposes two new failures: dedicated Chrome on
Linux exits before publishing its endpoint, and the Chromium REPL probe reports
overlapping observations. Other running jobs remain pending in this snapshot.

The Linux launch failure has a concrete cause: the shared process manager
correctly filters ambient display variables, but dedicated Chrome did not pass
them explicitly. Repair `d0769783f` supplies only `DISPLAY` and `XAUTHORITY` to
that owned visible process. A real-process regression fails before the repair
and passes on macOS and Linux afterward, retaining provider-secret exclusion and
headless/ordinary process isolation. Full local native-browser acceptance passes
(browser 15.430s/runtime 12.475s); hosted Linux headed acceptance remains pending.

The next draft branch also keeps Desktop validation running after unrelated
product failures, while requiring the same verified renderer artifact. The
required aggregate continues to reject every failed, skipped, cancelled or
missing dependency. Existing workflow contracts pass all ten cases, including
executing those aggregate refusal paths, and actionlint passes both workflows.
This change collects evidence; it does not allow publication or a green aggregate
after another required check fails.

The isolated native input diagnostic reports all 240 trusted events across six
cases, with composer p95 of 40ms at each tested draft length. Full-workload p95
remains 72ms against the 50ms target despite native and DOM focus. Natural final
RSS is 1,596,864KiB, sampled immediately after work; this is not a leak diagnosis.
That run had earlier forced-GC tab checkpoints and a 42.43s inspection pause after
streams started. Its limits and exact artifact are in
[the performance audit](../apps/web/scripts/native-performance-control-audit.md).

Subsequent instrumentation now pauses before streams, reports missing timing
entries as unknown, and samples tab retention passively. Forced GC remains only
an explicitly requested final diagnostic, excluded from acceptance. All eleven
keyboard/retention contracts pass locally (10.038s) and are included in the normal
performance gate. A new trace and final unchanged-workload measurement are still
required; functional success does not close the measured performance gap.

The same hosted run subsequently passes the full browser gate and both repaired
SSH integration blocks (Linux 54.316s/macOS 48.928s). Both client jobs fail later
at the final packaged-runtime fixture: Task's embedded shell rejects the named
`HUP` signal trap before executing either Go command. Explicit Bash now owns
that unchanged fixture block. An exact-block Task reproduction fails before the
repair and passes afterward, including cleanup on successful execution, failed
build and failed test, with no test after failed build. The actual packaged
runtime integration passes in 3.413s with unchanged race/shuffle/engine assertions
and two-minute deadline (`/tmp/whip-platform-runtime-fixture.log`). No product
runtime behavior or test requirement changed.

Hosted #277 run36584226063 finishes at `1edc9d823` with34 passed jobs, four failed
leaf jobs, two propagated aggregate failures and skipped Desktop. The four
leaves are exactly Linux dedicated Chrome, the REPL observation assertion and
the Task trap in both client jobs. Full product-browser validation passes;
Desktop supplies no hosted evidence because the parent workflow skipped it.
The final remote record is `/tmp/whip-277-final-summary.md`.

REPL diagnostics now retain bounded document/runtime/epoch/owner and raw socket
overlap evidence before asserting, and their output directory matches hosted
artifact collection. All original assertions and deadlines remain. Both local
browsers pass ten workflow groups with one observation per owner; seven focused
probe checks also cover refused sends/closes and lifecycle bounds. The hosted
failure is not reproduced or declared fixed. These checks and the existing
Desktop performance helper contracts now participate in their normal gates.
All32 diagnostic contracts pass together in10.045s; injected Safari-driver tests
required loopback access and never opened Safari or enabled Remote Automation.

The measured long-draft parser fast path is integrated as `2c38fdfaa`. It preserves
14,884 compared parsing cases and reduces the isolated 255,543-character ordinary
text scan median from2.188ms to0.072ms; valid long-prose triggers are unchanged.
Complete shared app types and1,388 tests across114 files pass in26.40s on the
combined increment. The native50ms typing target remains open: the comparable
baseline stopped before typing when a cached child jumped from its saved reading
anchor to the tail after seven successful restores. Its retained failure and
joined cleanup are recorded in the performance audit. No new UI-latency result
or leak diagnosis is inferred from the parser microbenchmark.

### Fresh-checkout performance gate ordering — 2026-09-29

Draft #278 publishes the platform increment at `e85f72203`. Its hosted run
`36588313432` exposes a setup-order failure before the performance workload:
the newly required Desktop helper contracts import the SDK through
`native-fixture.mjs`, but `packages/sdk/dist/index.js` does not exist until
`web-assets` builds it. The performance gate now builds its existing assets
before running those contracts, matching the other native product gates.
No workload, assertion or timing threshold changed. With generated SDK output
moved aside, the actual asset build and all 22 performance helper contracts
pass (10.041s for the tests); prior generated output is preserved separately.
Evidence: `/tmp/whip-performance-order-validation.log`. The complete performance
workload is not claimed by this dependency-order check. Hosted #278 remains
frozen for the other platform results.

### Accepted typing speed and final obligation reconciliation — 2026-09-29

The user explicitly accepts the measured 72 ms native typing p95 and asks to
close the speed work. The earlier 50 ms target is superseded for this redesign
acceptance; no further latency optimization is required. The full untraced
baseline completes all seven workload groups and all 40 reported native keys,
with no missing entries, native focus verified before/after, and no forced GC.
Its rounded p95 is 72 ms (68–76 ms quantization bounds), recorded at
`/tmp/whip-performance-native-baseline-comparable/run-niNF8k/performance.json`.
This decision does not waive reading correctness or natural retention evidence.

A read-only source/evidence audit reconciles the phase checklists: retained
capability implementation and both-engine contracts are satisfied; native core
removal, dynamic whole-product gate restoration, bounded SDK data/transport and
fresh-namespace setup documentation are also complete. Exact family test
replacements and earlier checkpoint limits remain linked. Final combined
client/platform/live-provider/resource acceptance stays open. Canonical docs now
state host config 21/schema 55, implemented client adoption and completed code
removal. The public SDK article remains drafted under the original publication
decision; its ten native source-checked examples and seven helper scenarios are
covered by the normal docs gate, while three draft-route presentation checks
remain explicitly pending publication. No release links or routes changed.

Hosted #279 at `ad7a4955f` passes the repaired performance helper/workload gate.
It exposes separate Firefox activity-reading and Settings assertion failures:
the first small upward wheel leaves a zero tail gap while Latest is visible;
the second sees two provisional-output hints in one two-operation group. Their
captured artifacts are being examined before changing product behavior or tests.
These are distinct from the earlier history-seed failure and from typing speed.

The complete unchanged `check:product-docs` gate passes at `73b118c1c`: protocol
checks, SDK build, docs types, 79 tests across nine files (including the ten SDK
examples), live add/edit/delete route refresh, static verification of four
prerendered documents/65 public files, 27 browser checks and Storybook build.
The three pre-existing SDK draft-route presentation checks remain intentionally
skipped under the publication disposition above. Log:
`/tmp/whip-final-docs-gate.log`; the owned gate joins with exit 0 and the source
tree stays clean. No public documentation or release was deployed.

Hosted #278 run `36588313432` finishes at exact `e85f72203` with 38 successful
jobs, the single performance setup-order leaf failure and two propagated
aggregate failures. No job is skipped; Desktop and both client/native-browser
gates pass. The setup-order repair is already exercised successfully in #279.

The #279 Settings failure is a fixture ownership error: two distinct canonical
operations share one running cell, and each mounted operation panel correctly
contains that cell's provisional stdout. Integrated `28060cf20` captures the
two exact operation IDs, their owner/turn and single running cell; checks one
hint per mounted expected operation with no duplicate or foreign hint; and
preserves disclosure, line-nine, committed replacement, REPL, draft and split
assertions. Both actual browsers pass all nine REPL Settings checks and join
their owned runtimes (48.432s Chromium/46.954s Firefox), using renderer
`0af75b87cce53a45cc1fb60227912395b09dfe601c899c4abfeb0e0d79cdc39d`.
No product code, workload, deadline or geometry changes; the untouched body
scenario is not claimed by this focused repair. Evidence:
`/tmp/whip-settings-provisional-evidence.md` and its exact ownership reports.

The first-gesture Firefox failure is not reproduced locally. The original full
activity workload passes; a controlled delayed-measurement experiment preserves
the 12 px upward movement, and bounded browser experiments reject a no-op
scroll-write explanation. No speculative product repair or settlement wait is
introduced. Integrated `0a61fdb64` adds passive bounded wheel/scroll/resize/frame
evidence around the unchanged first gesture, written on success or failure and
retired on completion, page hide, unmount or a ten-second deadline. Four
lifecycle/bounds contracts pass and now join the normal activity gate after its
asset prerequisite. The complete affected native activity scenario passes all
12 checks in each browser, with at most 30 mounted tree rows and the same
production renderer; this is passing evidence, not proof of the hosted cause.
Artifacts: `/tmp/whip-firefox-small-scroll-passive/`; investigation:
`/tmp/whip-firefox-small-scroll-investigation/findings.md`.


### Closed performance investigation and final combined follow-through — 2026-09-29

The single bounded retention follow-up completes and is integrated as `027612274`.
It reuses the measured candidate package without another typing run: three full
same-root navigations, natural 35-second samples, then explicit fixture
settlement. Every old document naturally collects, previous native frame
connections close, and the one provider request is not repeated. Settlement
removes 3,999 temporary Markdown spans. Renderer RSS finishes at 461.4 MiB,
compared with 459.0 MiB initially; no threshold was invented. All five owned
processes join, no bound overflows and no page/probe/cleanup errors occur.

The old-document retention investigation is satisfied for this bounded case,
without establishing an accumulating app-owned leak or requiring a speculative
product change. Full-workload peaks (1,629,712 KiB baseline / 1,693,136 KiB
candidate), RSS/shared-page and physical-footprint limitations remain preserved
in the [exact audit](../apps/web/scripts/native-performance-control-audit.md#retention-only-native-outcome-2026-09-29).
The 72 ms speed result remains accepted. No further timing or memory experiment
is planned for this investigation.

Hosted #279 run `36590021190` finishes at exact `ad7a4955f`: 37 successful jobs,
two known failed leaves (Firefox small-scroll activity and the Settings ownership
assertion), two dependent aggregate failures and zero skips. Desktop, both
client jobs and the repaired performance helper/workload gate pass. Final evidence:
`/tmp/whip-279-final-summary.md`. The next draft combines the locally verified
Settings repair, bounded scroll diagnostics and these reconciled records.
The diagnostic writer preserves the original reading assertion if artifact
collection also fails; its success path and workload are unchanged.

Final combined hosted acceptance and the unexplained reading/observation
failures remain open. Actual Safari automation and the bounded live-provider
smoke still await the existing approval questions. Signed/physical-device,
minimum-OS and real account/hardware evidence remain separately named limits;
none are implied by deterministic fixtures or by the user's speed acceptance.


### First-version closeout for human verification — 2026-09-29

The user changes the remaining acceptance sequence: document the Firefox scroll
and REPL incidents for later, execute the final combined/environment/closeout
steps, record other failures and continue, skip signed install/update, and stop
performance optimization. This refers to items 2–4 of the current completion
list; the original architecture Phases 2–4 were already complete. The first
human-verification version is the requested endpoint, with deferred requirements
recorded rather than falsely passed.

Product candidate: `674347705b7d3fc162146a4bfd5a7174ff57de13`, draft #280.
Branch `codex/backend-redesign-human-verification` adds documentation only.
[The closeout](backend-redesign-closeout.md) names NATIVE-01 Firefox small-scroll,
NATIVE-02 REPL observation accounting, NATIVE-03 cached child-anchor and NATIVE-04
Desktop navigation incidents, exact evidence, environment results and human steps.
The tested REPL fixture correction `fde8bd22020b42ff32977df4c985ccde4545c98e`
remains deliberately unintegrated future work; its separate eleven tests and
dual-browser workflow pass are not attributed to the candidate.

A bounded source/history diagnostic found no supported cached-anchor product
fix. All eight tiny Chromium/Firefox nested-history cases preserved the exact
semantic bookmark; their window-scroll control confirmed browser restoration
was exercised. No setting, product code or threshold changed. Evidence:
`/tmp/whip-cached-anchor-source-followthrough.md` and
`/tmp/whip-nested-history-audit/README.md`. Further investigation is deferred.

Fresh read-only protection checks find the designated handoff worktree clean at
`239f76152261484e4bc2c61dc1e8e7141fcb5f94`. The original development checkout
is at externally advanced `12f0ea0768b7d769765596c35c049fe80edfaeba` with 14
unrelated modified files. They are preserved; no reset, cleanup or source edits
were performed there. No installed runtime, account, Safari configuration or
physical device was changed, and no merge/deploy/publication occurred.

Final candidate run `36594047984` at `674347705` completes with 38 successful
jobs, one failed Desktop leaf, two dependent aggregate failures and zero skips.
Activity (including Firefox), Settings and the complete conversation workflow
pass. Desktop job `109501057523` passes types/package verification and its
166 native/unit +116 distribution checks, then fails native browser navigation:
page-3 records `ERR_ABORTED (-3)`, the same URL commits, and the act request still
throws `Browser navigation failed`. Later stages do not execute. The failure's
bounded JSON remains in the hosted log; the configured Desktop artifact path
contains no files. No complete candidate Desktop pass is claimed from earlier
checkpoints. This is NATIVE-04 in the closeout; per user direction it is recorded
and deferred without another repair/retest cycle. The aggregate remains failed.


The available environment preflight completes at 2026-09-29T16:21:19Z. Safari's
Remote Automation preference is absent (not sufficient alone to prove effective
state); its latest actual native WebDriver attempt rejected the missing opt-in.
No explicit live-eval config is selected. Paired iPhones have unavailable tunnels
and developer services; 25 simulators are available but none booted. No adb
server is listening, and Android connection availability remains unverified.
All metadata commands join. No credentials, Safari setting, device, provider or
installed runtime is touched. These are recorded follow-ups, not new blockers.
The [durable candidate inventory](backend-native-candidate-validation.md) retains
all 41 job results and 25 artifact IDs, including the missing Desktop archive.


## 2026-09-29 — fix provider default selection during human verification

The user reported `invalid provider setup operation` from the temporary Desktop
review installation. Actual bundled `kimi-k3-fast` metadata carries a 1,048,576
output ceiling, but configuration, dispatch and request-snapshot validation
still capped host output at 1,000,000. The SDK and renderer payload was correct.
Commit `f8a88d3b2bedb95bc1e42e3a28b8b25e5e1e660a` shares the supported host ceiling
across those three validators; the explicit 1,000,000-token helper/request
narrowing bound and subscription natural limits remain unchanged.

The actual Inference.net suggested-model setup regression fails on the previous
source specifically for `kimi-k3-fast`. Config and frozen Chat/Responses request
regressions also fail before the fix. Afterward, complete `internal/config`,
`internal/model`, `internal/providerhost`, `internal/session` and `internal/store`
suites pass. The wire regression checks exact limits, immutable digests and valid
saved request evidence. Independent review found no further natural-ceiling
validator or accounting mismatch.

A clean production Desktop package builds and verifies from `f8a88d3b2`, with
build ID `0.1.0-native-f8a88d3b2` and the unchanged validated renderer digest.
Only the user-authorized temporary installation at `/private/tmp/whip-review-wpUH7k`
was updated through its managed runtime path; the previous app/build evidence
is retained under `previous-build`. Its profile, runtime identity and saved data
were preserved. Native UI verification clicks **Use kimi-k3-fast**, observes the
normal first-message composer, and confirms the exact persisted selection and
1,048,576 ceiling. No message/inference request was sent. Local evidence is
`provider-fix-verification.json`, `build-evidence.json` and `launch-status.json`
in that temporary directory; the build log is `/private/tmp/whip-provider-fix-build.log`.
The normal installation and original development checkout remain untouched.
This is a targeted verified follow-up, not a new claim of full CI or release acceptance.


## 2026-09-29 — restore default sub-agent permission inheritance

The user-requested review of `session_LXF5LNK73AS5UDNLKGYY7Q4ME5` found
19 admitted children whose ordinary operations were denied with `no delegated
authority`. The root had Full Access and no standing grants. Default spawn copied
only standing grants, while automatic permission was limited to roots. This was
a backend authority-inheritance defect, not a provider or model failure.

New default children now capture eligible same-workspace automatic authority
alongside standing grants. Exact grant selections (including empty selections),
workspace boundaries, explicit computer/MCP consent, root-only questions and
one-use grant isolation remain enforced. Admission and dispatch validate every
captured ancestor hop. Permission changes retire ready policy operations across
the tree and permanently expire prior child delegation; later toggles cannot
revive it. Spawn previews expose the observed revision as an exact decimal string
and admission reevaluates it. Runtime instructions and the Full Access explanation
now describe the behavior.

Schema56 adds one child-policy table through an atomic schema55 upgrade. There
is no historical backfill or replay; existing failed children retain their old
restrictions. Migration tests preserve identity and admission receipts, verify
no backfill, and prove DDL/version rollback after a partial migration failure.
All older/foreign schemas remain rejected.

The original failure reproduces in both Starlark and QuickJS before the fix.
The both-engine regression now passes under the race detector: default child
file access, queued parent report delivery, and explicit-empty delegation refusal.
The complete store, runtime and CLI suites pass. Focused store policy/migration/
diagnostics race tests and preview race/shuffle tests pass. Permission UI tests
pass (19 tests across three suites). An independent review caught and corrected
revision serialization above JavaScript integer precision; no additional authority
blocker was found. This is a targeted follow-up, not full release acceptance.


The clean production Desktop package from `96498440ac3dd12e3dbad919ae52c94dd26b4b23`
builds and verifies as `0.1.0-native-96498440a`. The user-authorized temporary
installation at `/private/tmp/whip-review-wpUH7k` was updated through the managed
runtime synchronizer and reopened to the same root and remaining child panes.
Native UI inspection verifies Full Access and its updated inheritance explanation.
No live inference was requested. The normal installation and development checkout
remain untouched.

Runtime identity is unchanged. The actual schema55 snapshot and matching prior
app/backend are retained in `before-child-policy-fix/`, whose README records the
backup capture sequence. Comparing all54 original tables after upgrade finds
53 identical, with only18 unreferenced content bodies removed by normal restart
collection. Referenced content, history, sessions, configuration and permissions
are unchanged; no child-policy rows are backfilled. Integrity and foreign-key
checks pass. Exact local evidence is `child-policy-fix-verification.json`,
`runtime-manifest.json`, `build-evidence.json` and `launch-status.json`. The build
log is `/private/tmp/whip-subagent-debug/package-build.log`.


## 2026-09-29 — restore working REPL behavior after the native migration

The user clarifies that the old working behavior is the acceptance target unless
a deliberate change fixes a documented defect. In particular, the permanent
child-policy expiry described in the previous entry was a regression. This
follow-up preserves the new execution/persistence boundaries while restoring
live permission propagation, automatic instruction capture, MCP discovery/call
alignment, useful model guidance and friendly child identity. Performance work
remains closed. The active frontend checkout and normal installation are untouched.

The first increment keeps an ongoing policy-inheritance relationship for default
same-workspace children, including children created in Ask mode. Later policy
changes apply to new operations in existing children and grandchildren. Waiting
and ready old-policy operations remain denied; they are never replayed. Explicit
grant subsets, empty grants, different workspaces and explicit host-consent
requirements retain their restrictions. No standing grants are fabricated.

Schema57 restores missing default relationships only when immutable successful
agent-spawn arguments and matching receipts prove the original selection. Old
direct-client receipts without those arguments cannot safely distinguish default
inheritance from explicit restrictions and remain restricted. Identity, input
and operation outcomes are preserved. Existing schema56 delegation records now
follow the live policy. Fresh stores and atomic55/56 upgrades are supported.

The new off/on and Ask-to-Full-Access store regressions fail against the previous
helper. Focused store inheritance/preview/migration tests pass with the fix, as
does the actual Starlark/QuickJS scenario exercising existing default and
explicitly restricted children through Ask → Full Access → Ask → Full Access.
Broader integrated validation and the remaining increments are still in progress.

The user subsequently explicitly permits session invalidation during development.
The final implementation therefore removes the schema55/56 compatibility upgrade
and historical delegation backfill. Prior upgrade tests were useful intermediate
evidence, not a promise retained by this slice. Older databases are rejected
without modification; a fresh development runtime is required. Unsupported REPL
aliases will be removed even though this changes saved QuickJS fingerprints.
No user's existing database or running installation is modified by these edits.
