# Backend redesign development

This is the phase 0 working loop for [the redesign plan](backend-redesign-plan.md).
The new domain/runtime has not been implemented yet. These gates initially use
selected existing packages and the existing real-runtime SDK fixture. Replace
that wiring as the corresponding phases land; it is not a compatibility layer.

## Start working

Use Go from `go.mod`, Node 24, and Task 3.48.0. From this checkout:

```sh
npm ci --no-audit --no-fund
task check:fast
task check:change
```

The integration branch is `codex/backend-redesign`. Open implementation PRs
against that branch. Keep each PR focused on a behavior or invariant. Current
production gates on `main` and `development` remain unchanged.

| Command | Purpose |
| --- | --- |
| `task check:fast` | Active Go package formatting and all their tests; no npm install needed |
| `task check:change` | Fast gate, active builds/vet/race tests, generated contracts, SDK/examples, real-runtime fixture |
| `task check:phase` | Change gate plus selected admission/recovery/accounting integration regressions |
| `task check:analysis` | New lint findings since the frozen baseline, plus reachable vulnerability checks on the active packages |
| `task check:fixture` | Rebuild SDK and run the isolated process-restart scenario |
| `task dev:redesign -- --minutes=10` | Start a disposable scripted-provider daemon for manual SDK/app work |

The optional repository pre-commit hook uses `check:fast` on the integration
branch and `codex/backend-redesign-*` branches. Other branches retain the current
hook. `task hooks` installs the repository hook if desired; this is a local Git
configuration choice, not a prerequisite for CI.

## Active scope

`REDESIGN_PACKAGES` in [Taskfile.yaml](../Taskfile.yaml) is the single Go package
list for fast, build, vet, race and analysis checks:

- `internal/content`: retained immutable content and access primitives.
- `internal/agentdef`: template validation behavior to carry into the new domain.
- `internal/protocol`: current generator/interoperability baseline.
- `internal/session`: current storage invariants, until replaced in phase 1.

All tests in these packages are active. Add new domain/store/runtime packages as
they land. Remove old packages only when their retained guarantees have been
replaced or their retirement is recorded. `go test -run` cannot hide test files
that fail to compile.

The phase gate separately names four existing daemon regressions: admission
before provider construction, queued child input across restart, settlement
failure without provider replay, and accounting-lock ordering. They are temporary
reference checks, not a requirement to preserve actors or child-specific APIs.

The TypeScript protocol, SDK and SDK examples are active. The new fixture test
uses both Unix sockets and WebSocket. Full web/desktop/mobile/TUI/ACP suites
remain milestone obligations in phases 5–6. A shared contract change must expand
checks to every client already ported to that contract.

## Disposable fixture

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

This initial scenario does not establish lost-acknowledgement injection, queued
input crash behavior through the new SDK, mid-effect recovery or new-schema
semantics. Those remain explicit phase 2–3 acceptance obligations.

## CI

[backend-redesign.yml](../.github/workflows/backend-redesign.yml) runs on PRs into
the integration branch and pushes to it. Linux and macOS run the phase gate with
a race-instrumented fixture; a separate job runs pinned lint/vulnerability tools.
Their Go compiler caches have separate keys from each other and production CI,
so the first successful run can save the tools and race builds it actually uses.
The `redesign` aggregate requires every job to succeed, including after failure
or cancellation. It has no percentage-coverage gate. The existing production CI
and coverage floor remain intact.

Lint uses the repository's existing configuration and a frozen baseline revision,
so all findings introduced throughout the redesign are required to be fixed.
Five inherited findings are recorded below rather than suppressed in source.
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
| `task check:analysis` | Passed; 4.46 seconds with warm tool caches, no new lint findings or reachable vulnerabilities |
| Deliberate failing test in `internal/content` | `check:fast` failed with the expected marker in 0.71 seconds; test removed and branch hook passed |
| Manual fixture launcher, `--minutes=1` | Started, created SQLite, exited successfully on SIGTERM and removed its temporary directory; 1.67 seconds |
| Initial fixture assertion failure | Retained database, observations and daemon output; correcting the accounting check (unknown cost is independent of reported/estimated usage) produced a passing scenario |
| Workflow and patch validation | `actionlint` and `git diff --check` passed |

Logs and timings are under the ignored local `test-results/redesign/` directory.
The existing daemon restore failure remains outside the initial active gate and
is tracked above. These measurements do not include cold dependency downloads.
Remote CI and required-check enforcement are the remaining phase 0 acceptance.

The [remote canary run](https://github.com/context-labs/whip/actions/runs/36364503714)
deliberately introduced `TestRedesignGateCanary`. The test failed in both OS jobs,
analysis passed, and the required `redesign` aggregate failed. `gh pr checks 197
--required` reported that failed context. The temporary test was then removed;
the restored full phase gate must pass before phase 0 closes.

The [first restored run](https://github.com/context-labs/whip/actions/runs/36364784940)
exposed a too-small timeout: the complete storage race suite exceeded three
minutes on both hosted OS runners, while advancing through different tests in
SQLite. It had passed locally in 135 seconds. The race step now allows ten
minutes per package, still under the job's twenty-minute limit. No tests or race
instrumentation were dropped; the fast gate is unchanged. Hosted timing must be
measured separately from the local warm measurements above.
