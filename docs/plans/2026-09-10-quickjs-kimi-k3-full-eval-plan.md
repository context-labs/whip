# QuickJS + Kimi K3 Full evaluation plan

Date: 2026-09-10 (Pacific). Status: execution stopped; healthy Smoke achieved, Full partial.

## Execution outcome (2026-09-11 UTC)

The sections below preserve the original plan. Subsequent user authorization
allowed autonomous progression from healthy Smoke to one Full, with parallel
execution. Qualified Smoke and Full used `--jobs 4`, not the original serial
recommendation. The sole Full authorization has been consumed; further paid
runs are paused pending a new decision.

- Qualified Smoke `quickjs-k3-smoke-qualified-20260911-j4`: 8/8 fully valid
  outcomes, 6 passes, complete cost **$12.206513**.
- Full `quickjs-k3-full-qualified-20260911-j4`: partial, 14/30 started and 16
  unstarted. Nine healthy grades (7 passes, 2 native failures), one additional
  httpx native failed grade with invalid accounting, four cancelled/ungraded.
  This is **not a complete Full score**.
- Full stopped on an incomplete model stream and HTTP 520 with unknown
  failed-attempt billing. Known Full cost **$20.766658**; complete cost unknown.
- Earlier infrastructure-aborted Smokes cost **$2.507466** and **$16.110672**
  in known usage. Cumulative known cost across all four runs: **$51.591309**;
  complete cumulative cost remains unknown. Unknown charges are not zero.
- Fixed catalog User-Agent blocking, proxy-only inherited descriptor-limit
  failure, and serial evidence-export timeout. Added pinned-ref fixture
  qualification, native bulk export validation and regression tests. Changes
  remain unstaged and uncommitted; runtime stayed pinned to the original SHA.
- Validation: 41 eval + 81 runtime-ab tests; normal native fixtures 8/8;
  real-runtime stress exported 4,483 valid bodies on each runner in 2.478 s
  and 4.244 s, under the unchanged 240-second cleanup deadline. Fake-provider
  qualification incurred no external model-call cost.
- All owned processes/jobs/containers exited; original results and evidence
  remain preserved. No baseline promotion, extra Full or execution resume.

Detailed final report (private, ignored evidence):
`evals/artifacts/setup-20260910/QUALIFIED-FULL-STOP-REPORT.md`.
Updated diagnosis after reviewing our own Whip session: the supervisor's
immediate stop on unknown failed-attempt billing was too strict for an ordinary
quality run. The scheduler already distinguishes grading/evidence from complete
billing. Let bounded transient retries and unrelated trials continue, preserve
unknown costs honestly, and reserve strict billing completeness for cost claims
and baseline promotion. Add fake-provider recovery/non-cancellation regressions
before another run; do not silently resume or alter preserved results.
See `evals/artifacts/setup-20260910/PROVIDER-FAILURE-DIAGNOSIS.md`.
Further paid execution remains paused pending a new execution decision.

## Decisions confirmed with Sam

- Use the existing `whip-eval` harness, QuickJS, and Kimi K3 on inference.net.
- Run on this Linux machine.
- First deliver an ordinary Full run: 30 tasks, one independent attempt each.
- Separately approve a paid Smoke qualification run; use its measured cost to
  request approval before Full. No paid execution is authorized by this plan.
- Do not promote a baseline or add a Starlark comparison in this campaign.

No new evaluator, runtime, model integration, or dependency changes are needed
on current evidence. Fix blockers only if qualification reproduces them.

## Research findings

### Harness and experimental contract

`evals/README.md:3–9,49–88` and `evals/frontier/profiles.json` define Full as
21 terminal tasks plus 9 repository tasks, not the complete 89-task
Terminal-Bench registry. Harbor 0.22.0 and Pier 0.3.1 run the native graders.
The harness documentation records offline validation only, with no established
accepted baseline or prior machine qualification.

`evals/frontier/protocol.json:7–24` pins `kimi-k3`, provider `inference-net`,
`https://api.inference.net/v1`, and high reasoning effort. The evaluation config
also uses K3 for compaction and fresh task homes; personal instructions,
providers, MCP servers, and sessions are not inherited. Production RLM defaults
apply (`evals/whip_evals/observe.py:322–337`). Explicit `--engines quickjs` is
necessary to avoid falling back to personal config or Starlark.

There are no evaluator cost, token, round, or output-token caps. Native deadlines
and production request/runtime safeguards remain. Trials are not automatically
retried. Qualification and Full are distinct experiments; never pool their scores
or replace Full failures with successful Smoke attempts.

An initial `--promote` campaign would cost 90 trials; later promotion with an
accepted control costs 180. Neither is part of this plan.

### Readiness checked without changing the machine

- Linux x86_64, Go 1.27.0, Python 3.14.4, and Git available.
- Local Docker 28.4.0 and Compose v2.39.2 accessible.
- Docker reports 128 CPUs and about 247 GiB RAM; approximately 418 GiB disk free
  at inspection time. Recheck immediately before execution.
- `uv` is missing from PATH; no eval virtualenv or installed Harbor/Pier was found.
- `INFERENCE_API_KEY` was absent from the research process environment. Only
  presence was checked; no credential files or values were inspected.
- QuickJS WASM is embedded and its documented checksum matched. No native
  `qjs`, clang, or separate QuickJS build is needed. Eval binaries build with
  `CGO_ENABLED=0` (`evals/whip_evals/prepare.py:74–99`; QuickJS bridge and
  `internal/rlm/engine/quickjs/THIRD_PARTY_NOTICES.md`).

This is promising hardware, not successful task/container qualification. All
30 tasks together declare roughly 570 GiB disk reservations, excluding host
headroom, images and retained evidence, so do not expect all tasks to fit at once.
The scheduler admits work by native CPU/RAM/disk reservations and leaves 2 CPUs,
4 GiB RAM and 10 GiB disk headroom. Images/builds/evidence still need monitoring.

### Provider checks and uncertainty

An unauthenticated public `GET https://api.inference.net/v1/models` via curl
returned K3 with high effort, text/image input, 1,048,576 context tokens, and
1,048,576 advertised maximum completion tokens. Its published per-token fields
converted to $3.95/M input, $19.80/M output and $0.40/M cached input at inspection.
These are catalog observations, not a run estimate or billing guarantee; use
the actual run's frozen catalog and recorded call costs.

The same unauthenticated request through Python urllib returned HTTP 403.
The harness uses urllib with bearer authentication (`prepare.py:122–137`), so
its authenticated path must be tested before preparing a scored campaign.
Do not infer that auth will fix it or silently switch provider/model.

Public rate-limit docs list tier-dependent RPM limits (30 free, 200 with granted
credits, a 250 purchased-credit floor, 1,000 Growth/Enterprise), but do not establish
this account's K3 quota. Confirm account access, balance, concurrent requests,
TPM/RPM, burst policy, and any provider-side spend control. Sources:
- https://docs.inference.net/api/rate-limits
- https://docs.inference.net/reference/rate-limits

## Execution sequence

Commands below run from the repository root. Use a persistent supervised shell
or background job; preserve logs and the generated run ID. Do not place secret
values in commands, transcripts, reports, or Git.

### 1. Freeze source and install locked tooling — no paid inference

Inspect the intended commit and working tree. Research observed clean commit
`e9c97beabf82f1d6923039b8f6a4ca6959ebcb00`. Confirm it is still the intended
candidate, then record an immutable SHA for every stage:

```sh
EVAL_REF=$(git rev-parse HEAD)
```

Install uv using the host's approved installation method, then:

```sh
uv sync --project evals --locked
uv run --project evals --locked whip-eval doctor
uv run --project evals --locked whip-eval baseline show
uv run --project evals --locked whip-eval run full \
  --engines quickjs --ref "$EVAL_REF" --jobs 1 --dry-run
uv run --project evals --locked python -m unittest discover \
  -s evals/tests -p 'test_*.py'
uv run --project evals --locked python -m unittest discover \
  -s evals/runtime-ab -p 'test_*.py'
```

Gate: locked dependencies installed, tests pass, doctor passes, dry-run expands
exactly 30 QuickJS trials with no control or promotion. Doctor does not establish
provider, registry, network, or task setup health. Keep dependencies/protocol
pinned; do not update versions to bypass a failing check without review.

### 2. End-to-end fixture qualification — containers, no paid inference

After approval for downloads/builds/disposable containers:

```sh
uv run --project evals --locked whip-eval doctor --integration
```

This intentionally runs eight fake-provider fixtures across both engines,
both native runners, and shared/separate verifiers. It is infrastructure
qualification, not a paid Starlark benchmark arm. Check finality, child accounting,
content export, service survival, patch collection/application and cleanup.

Gate: all fixture checks pass with complete evidence and no orphaned owned
containers. Real task images may still need registry access or apt installation
of missing Python/curl/rg/git, which fixtures cannot fully qualify.

### 3. Provider preflight and paid-Smoke approval

Provision `INFERENCE_API_KEY` via the environment or secret manager, with no key
value sent in chat. In the locked Python environment, invoke the existing
`whip_evals.prepare.catalog` helper against `frontier/protocol.json` as a
catalog-only check. This tests the exact authenticated urllib path without an
inference request. Record only whitelisted catalog metadata; sanitize errors.
Do it before `run`, because normal preparation downloads/builds before its
catalog/key validation.

Gate: exact model/high effort and usable pricing available, authenticated catalog
works, account/quota confirmed, and Sam explicitly approves the uncapped
8-trial Smoke. Explain that even one active trial can spawn multiple model calls.
If the authenticated request still gets 403, diagnose the actual response/proxy
behavior and make only a reproduced, tested fix; do not repeatedly start runs.

### 4. Paid Smoke at conservative concurrency

```sh
uv run --project evals --locked whip-eval run smoke \
  --engines quickjs --ref "$EVAL_REF" --jobs 1 \
  --label quickjs-k3-qualification-j1
```

Eight tasks: six terminal and two repository, including image and service tasks.
Use generated unique IDs. Keep the model/runtime defaults unchanged.

Gate before more spending:
- 8/8 authoritative grades, with valid failures distinguished from infrastructure
  failures; passing every task is not required for a valid evaluation.
- Complete evidence, identity and whole-tree accounting, including descendants,
  helpers, compaction and failed work; no unresolved/pending usage.
- No unresolved provider saturation, timeout pathology, setup failures or cleanup
  errors. HTTP status telemetry is not available in the durable call ledger;
  combine retained runner/provider errors with account-side telemetry if available.
- Review disk/RAM pressure and per-phase timings, not just agent latency.

Stop and diagnose systemic faults rather than retrying failed tasks until they
pass. Any rerun gets a new ID and remains separately reported.

### 5. Cost review and explicit Full approval

Deliver the Smoke report, total complete cost, known-cost subtotal, trial-level
costs, suite breakdown, runtime, missingness and failure causes. Never substitute
known subtotal for complete cost when usage is missing.

A transparent first extrapolation is:

`Full forecast = 21 × mean terminal Smoke cost + 9 × mean repository Smoke cost`.

State that this is a rough forecast from six and two tasks, not a confidence
bound or cap. Show cost dispersion and the long-task risk: repository tasks
allow up to 90 minutes; all Full agent deadline ceilings sum to about 19.25
agent-hours before setup/grading. Neither is a wall-clock or spending prediction.
Report all qualification spend separately and include it in campaign totals.

Default: retain `--jobs 1` for Full, prioritizing a qualified execution path over
speed. If measured runtime is unacceptable, propose a separately approved second
8-trial Smoke at `--jobs 4`, review quality/errors/latency/accounting, then use
that qualified concurrency for Full. Do not jump to the default 32. Changed
concurrency is a descriptive comparison, not strict apples-to-apples timing.

Planned paid trial counts: 8 Smoke + 30 Full = **38**, or **46** if one additional
concurrency qualification is approved. A later baseline is additional work.

Sam approves Full only after reviewing forecast, desired concurrency and budget
policy. Monitoring is not a hard cap; provider-enforced exhaustion may terminate
trials and produce a partial run. Any new harness-side cap would change the
experimental protocol and requires a separate decision, not a hidden tweak.

### 6. Full run — only after the preceding approval

```sh
# Change only if another concurrency was explicitly qualified and approved.
EVAL_JOBS=1
uv run --project evals --locked whip-eval run full \
  --engines quickjs --ref "$EVAL_REF" --jobs "$EVAL_JOBS" \
  --label quickjs-k3-full-initial
```

No `--promote`, `--against`, or extra repetitions. Record SHA, binary hash,
protocol/task/runner pins, frozen catalog, host and requested/peak concurrency.
Recheck source/harness/catalog identity between stages. If a material change is
needed, document it and requalify rather than silently attributing results to the
original candidate. Keep the host free of unrelated heavy work.

### 7. Validate, retain and summarize

A valid complete Full evaluation requires all 30 planned native grades, complete
identity/evidence/accounting, and successful owned-resource cleanup. Exit 0 or
`status=complete` alone is insufficient to establish complete billing.

Deliver:
- Overall and per-suite verified success rate and authoritative grading coverage.
- Per-task outcomes and failure taxonomy: model/task failure, provider, runtime,
  setup/grader, deadline, missing evidence/accounting, cleanup.
- Total spend including failures, effective cost/pass, accounting coverage and
  separate campaign/qualification cost totals.
- Whole-tree agent time, setup/verifier/queue/cleanup timing where available,
  wall time and requested/peak concurrency.
- Manifest, canonical result, Markdown report and CSV paths, plus private evidence.

Small reports live in `evals/reports/<run-id>/`; bulk evidence lives in ignored
`evals/artifacts/<run-id>/`; retain the associated `evals/cache/` build inputs and
binaries. Review public artifacts for secrets before any intentional staging;
no auto-commit, upload or baseline mutation. Report this as the pinned local
Frontier adaptation, not a published leaderboard rank or evidence of QuickJS
beating Starlark.

## Stop/recovery policy

On systemic errors, unexpected spending, missing accounting, resource pressure or
cleanup failures: stop dispatch via graceful interruption, preserve available
evidence, and request a decision before another paid run. In-flight model costs
can still settle after interruption. Never stop unrelated host workloads.

There is no execution resume. Once the owning runner has exited,
`uv run --project evals --locked whip-eval cleanup RUN_ID` can clean only proven
owned containers. A new experiment uses a new run ID; do not overwrite results
or cherry-pick successful reruns. Partial scores are confirmed floors, not a
complete Full result.

## Work completed for this plan

Read-only repository/machine/provider-catalog research and this runbook only.
No package installation, build, test suite, container launch/image pull, paid
inference request, benchmark, or baseline promotion has been performed.
