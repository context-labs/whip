# Canonical Whip evaluations

Branch: `feature/agent-definition` (shared checkout; unrelated work preserved).

Status: implemented September 10, 2026, with offline verification. The approved
proposal below remains the design record; [evals/README.md](../../../evals/README.md)
is the shipped operational contract.

**Execution amendment:** the user explicitly requested implementation end to end
without running any evaluations, with qualification and scored runs deferred to
another machine. Accordingly, no native evaluation/fixture containers or external
model calls were run. Baseline initialization is intentionally pending.

## Implementation record

- [x] Fixed 8/15/30 profiles, 30-task source/file/image lock, pinned uv environment,
  native configuration/resource/deadline verification, and attributed references.
- [x] Shared observer/adapter/accounting/content modules extracted with compatibility
  imports. Whole-tree evidence checks, model-catalog freeze and vision preserved.
- [x] Cross-suite resource pool, shared-binary runtime A/B, seeded schedule,
  cancellation, owned cleanup, native phase timings and peak active concurrency.
- [x] Immutable JSON/Markdown/CSV reports, explicit unknown coverage, offline
  analyses, historical and paired comparisons, and cost estimates without caps.
- [x] Automatic initial/replacement baseline policy, evidence gates, atomic pointer,
  history and concurrent-publisher compare-and-swap.
- [x] Authored future-machine qualification fixtures for engines/runners, service
  survival, numeric paging, child accounting and separate-verifier patch collection.
- [x] Adversarial review completed; findings fixed and covered by offline tests.
- [x] Documentation, feature map and roadmap updated.
- [ ] Run native qualification, establish a safe host/provider concurrency, then
  collect Smoke/Medium and initial Full baseline on the designated eval machine.

Offline evidence: 33 canonical Python tests and 81 retained adapter/controller
unit tests passed; every one of the 30 locked task bundles was prepared and
validated from cached archives with network calls forbidden. Native JobConfig
parsing was checked without execution. `task check` passed with live RLM switches disabled after
the other session's in-progress Go changes landed. SDK/web/tooling checks also
passed independently. No benchmark or native qualification trial was executed.

Small implementation choices: `request.json` and `preparation-error.json` retain
failed preparation attempts without fabricating grades. `result.json` is the final
publication marker, written after Markdown/CSV. Provider HTTP-status telemetry is
explicitly unavailable in the existing durable ledger; no production telemetry
changes were added. There is no general schema framework: schema version 1 is
specified in the README and exercised with authored records and policy tests.

## Goal

Make it easy to measure Whip as the repository changes: one command to run a
fixed evaluation, one report format, paired comparisons, and an automatically
maintained accepted baseline. Reuse the real Whip adapter, native graders, and
evidence collection already in `evals/runtime-ab/`.

The initial benchmark is **FrontierHarness v1**, using **Kimi K3 on Inference.net
with high reasoning effort**. The standard measurement is one independent attempt
per task, not best-of-N. Repetitions measure variability without changing what
counts as a pass.

## Decisions

| Area | V1 decision |
| --- | --- |
| Profiles | Smoke: 8 tasks. Medium: 15 tasks. Full: all 30 tasks in the pinned Frontier v1 manifest. |
| Subsets | Fixed, nested, versioned lists. No random task selection at launch. |
| Parallelism | One supervisor, up to 32 active trials across both suites and all candidates, subject to resource admission. |
| Limits | Benchmark task deadlines/resources; no experimental dollar, cumulative-token, round, or output-token ceilings. Preserve and record production Whip controls. |
| Runner | A small Python CLI under `evals/`, with pinned Harbor/Pier dependencies and the existing Whip instrumentation. |
| Reporting | Versioned JSON as the source of truth; generated Markdown and per-trial CSV. |
| Baseline | An accepted Full evaluation for the current Whip build/configuration, separate from external reference scores. |
| Promotion | An explicit promotion campaign runs the necessary trials, then accepts automatically if a versioned policy passes. Ordinary runs do not launch extra trials. |
| Persistence | Immutable run identities, retained evidence, atomic report/baseline publication. |
| Exclusions | No execution resume, CI, dashboard, database service, distributed scheduler, or automatic cloud provisioning in v1. |

## Research findings that determine the design

1. **There are 30 selected tasks, not an entire Terminal-Bench dataset.** The
   frozen Frontier manifest selects 21 terminal tasks and 9 DeepSWE tasks. Its
   workflow resolves terminal tasks through `terminal-bench@2.0`; the legacy
   registry currently contains 89 tasks at that version. Filter by the exact 21
   manifest IDs. Do not run all 89, substitute another similarly named dataset,
   or silently drop unavailable tasks. [S1, S2, S3]
2. **Public metadata and executable task copies differ.** The published
   `regex-log` and `sqlite-db-truncate` agent deadlines are 1,200 seconds; both
   resolve to 900 seconds in the legacy executable task source. DeepSWE's
   reproduction images also differ from the original published environment.
   Store both sources and every applied difference. The old four-task studies
   are historical evidence, not the initial canonical baseline. [S4, S5, L1]
3. **A local result does not establish a published leaderboard ranking.** The
   original reference used Fireworks and Runta; its applied network allowlist
   was not recorded. Our initial protocol uses Inference.net and local Docker
   on a dedicated Linux host. Reports must identify this adapted methodology.
   An external harness replayed with our protocol is a matched control; a table
   copied from the published results is contextual reference only. [S2]
4. **32 jobs require capacity, not just a flag.** The published agent resource
   declarations for all 30 tasks sum to 39 vCPUs and 118 GiB RAM. DeepSWE can
   additionally need a separate verifier container. CPU quotas limit consumption
   but do not reserve cores against competing work. Provider quota and nested
   model calls can also become bottlenecks. [S6, S7, task-research.json]
5. **The existing implementation is a useful base but a serial study controller.**
   `study.py` executes one blocking runner job at a time and implements historical
   spending reservations. Reuse the adapter, finality checks, accounting, content
   export, and integrity checks; replace the study-specific controller. [L1–L4]
6. **Product failures need attribution, not exclusion.** The previous work found
   both a numeric host-argument defect and a 600-second provider-call timeout.
   The current OpenAI-compatible client still has a 10-minute HTTP timeout.
   Record that as a production control, distinct from the task's deadline.
   A harness defect or local provider timeout must not disappear as an excluded
   infrastructure failure. [L5, L6]

The accompanying [task research](task-research.json) records all 30 task IDs,
metadata URLs/hashes, published resources/deadlines, executable source revisions,
and the selected profiles. It is research evidence, **not a finished execution
lock**: image digests, instruction hashes, and grader hashes still need resolution.

## Fixed profiles

Profile version 1 uses the following nested selections. Selection is based on
workload coverage, language coverage, prior workflow continuity, and published
difficulty metadata—not choosing tasks Whip already passes. Frontier labels 29
tasks medium and one hard, so an easy/medium/hard balancing rule would be misleading.
The medium subset includes the sole hard task, Meriyah. [S8]

| Profile membership | Suite | Task | Coverage |
| --- | --- | --- | --- |
| Smoke, Medium, Full | Terminal | `regex-log` | Parsing and edge cases; continuity with earlier runs |
| Smoke, Medium, Full | Terminal | `openssl-selfsigned-cert` | CLI tools, files, permissions |
| Smoke, Medium, Full | Terminal | `build-cython-ext` | Native build and Python compatibility |
| Smoke, Medium, Full | Terminal | `code-from-image` | Image interpretation and implementation |
| Smoke, Medium, Full | Terminal | `kv-store-grpc` | Service implementation and process lifecycle |
| Smoke, Medium, Full | Terminal | `db-wal-recovery` | Binary data and database recovery |
| Smoke, Medium, Full | DeepSWE | `anko-typed-variable-bindings` | Go repository change; continuity with earlier runs |
| Smoke, Medium, Full | DeepSWE | `httpx-multipart-response-parsing` | Python repository change; continuity with earlier runs |
| Medium, Full | Terminal | `constraints-scheduling` | Constraint reasoning |
| Medium, Full | Terminal | `largest-eigenval` | Numerical correctness and performance |
| Medium, Full | Terminal | `merge-diff-arc-agi-task` | Git merging and pattern reasoning |
| Medium, Full | Terminal | `dna-insert` | Scientific domain work |
| Medium, Full | DeepSWE | `fastapi-deprecation-response-headers` | Framework/API behavior |
| Medium, Full | DeepSWE | `arktype-json-schema-refs-dependencies` | TypeScript and schema behavior |
| Medium, Full | DeepSWE | `meriyah-explicit-resource-declarations` | JavaScript parsing; published hard task |

Full adds the remaining 15 IDs from `task-research.json`. Smoke is 6 terminal + 2
repository tasks; Medium is 10 + 5; Full is 21 + 9. Subset scores are scores on those
subsets, not estimates presented as a Full score. Display suite breakdowns because
the proportions differ. Any membership change creates a new profile version.

All profiles use identical per-task conditions. “Smoke” means fewer real-model
tasks, not cheaper reasoning or a shortened clock. With 90-minute repository
deadlines, even Smoke is not guaranteed to finish in a few minutes.

Keep a separate free `doctor` command backed by our authored deterministic
fixtures. Keep `evals/rlm/` and the Go runtime microbenchmarks; they measure different
properties. Run microbenchmarks serially on an otherwise idle host.

## CLI and layout

Use Python 3.12+ and `uv`; initially pin `harbor==0.22.0` and
`datacurve-pier==0.3.1`, then freeze all transitive dependencies in `uv.lock`.
Use stdlib orchestration, JSON, CSV, Decimal, hashing, and statistics. Reuse the
runner dependencies rather than adding an evaluation framework or Go service.

Proposed commands, from the repository root:

```sh
uv run --project evals whip-eval doctor
uv run --project evals whip-eval run smoke
uv run --project evals whip-eval run medium
uv run --project evals whip-eval run full

# One build, two execution engines; eight tasks per arm, sixteen trials total.
uv run --project evals whip-eval run smoke --engines starlark,quickjs

# Re-test the accepted build alongside the candidate on the same tasks.
uv run --project evals whip-eval run medium --against baseline

# Full, repeated comparison and automatic policy evaluation.
uv run --project evals whip-eval run full --promote

# Analysis only; these do not execute tasks or contact the model.
uv run --project evals whip-eval compare RUN_A RUN_B
uv run --project evals whip-eval report RUN_ID
uv run --project evals whip-eval baseline show
```

`run` defaults to one repetition, the current source tree, the configured default
execution engine captured at launch, Kimi K3 / Inference.net / high, and a maximum
of 32 trials. Support `--ref`, `--repetitions`, `--jobs`, `--seed`, and `--label`.
`--against REF` compares another pinned build; `--against baseline` uses its
archived build/configuration, never today's defaults. Restrict v1 to at most two
arms. Engine A/B uses one binary and changes only the engine field. Freeze the
declared treatment and validate every other difference.

`--promote` is a predefined Full campaign: three repetitions, current accepted
build versus candidate. With a baseline, that is 30 × 3 × 2 = **180 trials**. For
initialization, it is 30 × 3 = **90 trials**. Print the exact workload and historical
cost estimate before dispatch; do not enforce the estimate as a cap. Promotion
never silently appends trials to a normal 30-trial Full run.

Ordinary runs are also automatically compared to the stored baseline's matching
task subset, without paying to replay it. Label this as a historical comparison;
`--against baseline` is the stronger contemporary comparison.

```text
evals/
  README.md
  pyproject.toml
  uv.lock
  whip_evals/
    cli.py                 # commands and output
    run.py                 # freeze, prepare, admit, supervise, finalize
    adapter.py             # Harbor/Pier launch integration
    observe.py             # existing Whip evidence collection
    report.py              # normalization, metrics, comparisons
    baseline.py            # pure acceptance policy + atomic publication
    integrity.py           # existing artifact/evidence validation
  frontier/
    tasks.lock.json        # exact 30 executable tasks and resolved protocol
    profiles.json          # fixed 8/15/30 membership and versions
    protocol.json          # model, environment, controls, scoring, promotion policy
    references.json        # attributed external scores; separate comparability
  tests/                   # tests and small authored fixtures
  reports/<run-id>/        # small sanitized manifest, result, Markdown, CSV
  baselines/<track>/       # current pointer + immutable acceptance history
  artifacts/<run-id>/      # ignored bulk evidence
  cache/                   # ignored immutable builds, task bundles, images metadata
  runtime-ab/              # retained historical studies
  rlm/                     # retained deterministic Go evaluations
```

Package names can remain this small; split a module only when the implementation
needs it. Preserve historical snapshots/results. Move reusable instrumenting code
with its tests, or extract common helpers and leave thin compatibility imports;
do not maintain two evolving copies of the same observer.

## Freeze and prepare before model calls

Each run receives a UTC timestamp plus random suffix. Create its directory
exclusively and refuse an existing ID. Write an immutable launch manifest before
execution. Resolve refs once and build in isolated temporary directories; never
switch, reset, stash, or commit the user's working tree.

Capture:

- Git commit, dirty status, an exact source snapshot hash, build recipe/toolchain,
  executable/embedded-asset hashes, adapter/observer/lockfile hashes.
- Candidate engine and effective agent definition, prompts, skills, capabilities,
  recursion/worker settings, provider route, model ID, effort, sampling parameters,
  provider catalog/pricing snapshot, context and output settings.
- Task IDs and content hashes, source refs, image digests, native grader/reward
  mapping, instruction hash, adapter-added instruction hash, all resolved controls.
- OS, architecture, CPU/RAM/disk capacity, Docker/runner versions, emulation,
  network policy by phase, scheduling seed, requested/effective concurrency.
- Planned task/arm/repetition matrix, retry/scoring/promotion policy versions,
  baseline pointer and its revision at launch.

Dirty-tree runs are allowed for development, built from a captured snapshot that
includes relevant untracked inputs and excludes secrets/results. Mark them
ineligible for automatic baseline promotion. Baselines need a reproducible clean
commit and a retained verified build. Never change the source mid-campaign.

Discover model metadata once, freeze it, and supply the same snapshot to every
trial. Do not refresh pricing/configuration separately inside each task. Record
server model identifiers/fingerprints when available; an alias is not proof that
the hosted weights remained unchanged.

### Task and environment protocol

Initial protocol name: `frontier-v1-local-v1`.

1. Pin Frontier's selection to `e837a70bd6beb4e72eeeda62dd06e3bd34f6cb63`.
2. Pin the selected Terminal-Bench executable tasks to the legacy registry's
   resolved source revision `69671fbaac6d67a7ef0dfec016cc38a64ef7a77c`. Pin DeepSWE
   to `435ee89ec2f2e2289f33b0da4f992f0b7b7266b9`.
3. Resolve and hash complete executable bundles and image digests. Preserve native
   graders. Do not use mutable registry lookup or image tags on each run.
4. Use published Frontier agent/verifier deadlines and CPU/RAM declarations.
   Apply an explicit protocol overlay for the two terminal agent-deadline
   differences (900 → 1,200 seconds). Normalize legacy memory/storage units and
   record source versus effective values. These are benchmark-alignment changes,
   not per-run tuning. Unexpected differences fail preparation.
5. Preserve executable-task network semantics, including the pinned runner's
   resolved legacy defaults, and the existing domain-limited Inference.net proxy
   exception for isolated Pier agents. Record differences from Frontier's public
   network metadata. Do not silently apply a blanket provider-only network rule
   to verifiers: package installation can be part of their native operation.
   Hash explicit agent/setup/verifier policies. Validate permitted and denied
   routes with authored fixtures before the first scored run.
6. Fresh writable task environment, OS home, Whip home/database, sockets, process
   groups, and output paths per trial. Pin supporting tools such as ripgrep and
   retain their notices. Hide graders/reference solutions from the agent until
   the native runner's verification phase. No mounts of the developer's home,
   whole benchmark checkout, or prior solution artifacts.
7. Keep the DeepSWE commit instruction constant and recorded: the agent must
   commit its work inside its disposable task repository. The adapter does not
   repair, commit, or improve a submitted solution after the task ends.

This protocol deliberately defines a stable internal comparison rather than
claiming reproduction of the original Runta checkpoint. It requires a new initial
baseline. Changing the task lock, network policy, graders, or execution environment
creates a new protocol version and a new baseline track.

Pre-pull images and prepare immutable dependencies/builds before task timing.
Reuse clean read-only caches only. No cached solutions, warm writable task state,
or formal-task execution in the prepared image. If an image or verifier cannot
run under the declared policy, preparation fails visibly; do not substitute a
task or loosen policy based on whether Whip passed it.

## Parallel execution without changing agent behavior

One supervisor owns a bounded pool of runner subprocesses. Each subprocess
executes exactly one task/arm/repetition with Harbor or Pier's internal task
concurrency set to one. The supervisor supplies the cross-suite concurrency.
This retains the existing adapters and avoids coordinating two independent
32-job pools or changing Whip's internal agent scheduling.

Admission reserves the complete trial's worst-case overlapping container CPU,
memory, and disk requirements, including separate verifiers and declared host
headroom. Hold the reservation until cleanup is confirmed. Reserving upfront
avoids deadlocks where every agent finishes but no verifier can obtain resources.
If a task cannot fit even alone, fail preparation with the required capacity.
Do not shrink task resources to make 32 fit. A supported smaller machine runs
fewer concurrent trials and records that fact.

One Full run has only 30 trials; 32 matters for A/B or repetitions. The initial
host recommendation is a dedicated native Linux x86_64 machine, sized from the
computed schedule. The 39-vCPU/118-GiB agent sum excludes host overhead and
additional verifiers; it is a lower planning bound, not a sufficient host spec.
The earlier desktop Docker VM is unsuitable for 32 heavy trials.

Shuffle task blocks with a saved seed; interleave A/B within blocks and balance
which arm starts first. Use one capacity policy for both arms. Separate fresh
sessions and provider cache identities; record observed cache use. Provider
caching remains production behavior rather than inventing first-turn discounts.

Queue and prep time occur before the task clock. The clock begins at agent
execution and applies to the whole Whip tree. Setup, verification, evidence
export, and cleanup have separately recorded deadlines. Any outer watchdog must
cover those envelopes and native verifier retries; it must not truncate a valid
task. After timeout, stop the full owned Whip tree and grade available work when
the native runner permits it. On normal completion, preserve task services
through verification and reuse the observer's whole-tree finality checks.

Model calls inside a task keep production concurrency and retries. Do not add a
global token budget, shorten reasoning, or throttle in-progress tasks to hit a
wall-time target. Verify actual account headroom before high concurrency; current
provider documentation lists different RPM limits by tier and custom overrides.
Shared account use and descendant calls count too. Record 429/5xx/timeouts,
backoff, time to first token when available, and achieved throughput. [S7]

One-time capacity qualification: first exercise 32 authored fixture trials with
real runner/process/resource behavior; then compare the same Whip build on Smoke
at 4 versus 32 workers, four repetitions each (64 paid trials total). Interleave
condition blocks, keep per-task settings fixed, and inspect task outcomes,
provider latency, saturation and throttling. Freeze the highest supported
concurrency after that check. The small live sample cannot prove statistical
equivalence; resource isolation and ongoing telemetry remain necessary. These
qualification results are separate from the proficiency baseline.

## Limits, failures, and cancellation

Reports must distinguish three control sources:

- **Benchmark:** task agent/grader deadlines and container resources.
- **Whip:** production request timeout, worker/recursion/cell/host-call/output
  controls, plus any limits the agent itself assigns to descendants.
- **Evaluator:** setup, export, and cleanup deadlines outside agent execution.

The canonical runner introduces no cumulative dollar/token/round budget, no
smaller model output limit, no reserve-based spending stop, and no global run
deadline. Use the CLI's documented unlimited/unset semantics; do not confuse a
configuration's literal zero allowance with an absent limit. Unknown cost is
reported as unknown and does not prevent other planned tasks from starting.

An exhausted production limit is measured Whip behavior, not an evaluator budget
failure. Changing it is a declared candidate change with a new comparison, not
an unrecorded exception inside the eval. The historical 600-second HTTP request
timeout is the concrete regression case for this distinction.

Disable automatic full-task/agent retries. Keep native verifier infrastructure
retry behavior and production provider retries, record every attempt, and never
select the best result. A deliberate rerun gets a new run ID. Repetitions are
independent planned trials, not attempts to rescue failures.

Keep separate fields for execution status, grader status/reward, termination
source, and evidence/accounting completeness. Suggested causes include
`benchmark_deadline`, `whip_request_timeout`, `whip_guard`, `agent_error`,
`provider_error`, `setup_error`, `verifier_error`, `export_error`, and
`user_cancelled`. Causes require evidence; unresolved causes stay unknown.
A valid failing grader result is a task failure. A provider 429 or harness crash
does not, by itself, justify excluding a trial as infrastructure-invalid.

Ctrl+C stops new dispatch, cancels owned active work, exports what is available,
and produces a partial report. Process groups/containers are identified by run
and trial identity; cleanup never touches unrelated work. Preserve cleanup errors
and resource ownership metadata. Provide `cleanup RUN_ID` for orphaned resources,
not execution recovery. A crashed/interrupted run is never resumed. Offline
report regeneration from existing evidence is supported and makes no model calls.

## Standard results and metric definitions

`manifest.json` records the immutable experiment; `result.json` has schema version,
run status, coverage, per-arm summaries, comparisons, per-trial records, and
artifact references. `report.md` and `trials.csv` derive from that same normalized
result. No separately calculated Markdown score.

Each trial is keyed by task ID, candidate ID, and repetition. Include native
reward/result hash, all statuses and cause evidence, phase timings, root/child
accounting, effective controls, evidence hashes, and artifact paths. Raw runner
results remain authoritative. Score a task using its documented native reward
mapping; partial test counts are diagnostics, not additional benchmark passes.

| Metric | Definition / interpretation |
| --- | --- |
| Verified successes | Passed native graders / planned trials, with counts. On an incomplete run this is a confirmed success floor, prominently labeled partial. |
| Graded pass rate | Passes / trials with an authoritative valid native grade. Show denominator and missing/ungraded reasons beside it. Never hide excluded work. |
| Coverage | Planned, started, graded, missing, infrastructure-invalid, cancelled; evidence and accounting completeness separately. |
| Suite scores | Terminal and DeepSWE pass counts/rates, plus task-level outcomes. |
| Repetitions | Mean per-task success frequency, repetition scores, and variability; no best-of-N or pass@k headline. |
| Cost | Total known USD across all calls/trials, unknown-call count, accounting coverage. Complete cost is null if any relevant cost is unknown. |
| Effective cost per pass | Complete all-trial cost / verified passes, including failed work. Null if incomplete or no passes. |
| Tokens/cache | Input/output, cache read/write if supplied, root/descendant/helper/compaction attribution. Cache ratio uses documented non-overlapping provider fields. |
| Agent time | Median/p95 wall time to termination over started trials; label failures/timeouts. Also report successes separately with sample size. |
| Run time | End-to-end elapsed, queue/setup/agent/verifier/export/cleanup breakdown and throughput. Phase sums are not parallel run wall time. |
| Reliability | Deadline, product guard, provider, runtime, verifier, and export failures with rates/counts. |
| Resource/runtime diagnostics | Peak sampled RSS, CPU sampling coverage, REPL/host calls/errors, worker/child counts, peak prompt size; unavailable fields remain null. |

Money uses Decimal or integer microdollars internally. Distinguish provider-billed
values, catalog-price estimates, and Whip's ledger. Unknown is never zero. Do not
invent upstream first-turn cache-normalized costs from missing evidence. Counters
such as sampled process RSS or summed child spans must retain their measurement
limitations. No opaque combined “quality × cost × speed” score. [S2, L2]

A/B output shows paired wins/losses/ties, pass-rate delta in percentage points,
cost ratios, common-success latency ratios, and task-cluster bootstrap intervals
(10,000 resamples, stored seed). Resample each task with both arms and all its
repetitions together; 90 trials on 30 tasks are not 90 independent tasks.
Published first-attempt results remain separate from repeated-run averages.

Store immutable finalized reports and evidence inventories. During execution,
write progress separately; finalize atomically. Later report regeneration writes
a versioned analysis output and records the analysis-code hash without replacing
raw evidence or the report accepted as a baseline.

## Comparison and baseline acceptance

Maintain one default track, initially
`frontier-v1-local-v1/kimi-k3-inference-high`. A track fixes tasks, graders, model
route, effort/sampling, runner protocol, environment class and resource policy.
Its baseline also names the accepted Whip commit, engine, definition/configuration,
and build hash. The engine is part of the accepted candidate identity, so a
validated runtime change can become the next baseline in that same track.

Compare by compatibility fields, not Git SHA equality. Code/prompt/runtime changes
are intended treatments; undeclared task, model, grader, network, or environment
changes are confounders. Model/provider experiments can produce descriptive A/B
reports with declared differences, but require their own baseline track.
Record requested and actual concurrency; historical timing comparisons across
unqualified host/load conditions are descriptive only.

Smoke/Medium comparisons project the Full baseline onto exactly the selected task
IDs and show its repetition count. They cannot promote a baseline. Reports pin
the baseline revision at launch, even if another run later updates it.

### Automatic policy v1

`run full --promote` pre-registers three repetitions per task/arm. It performs no
optional stopping or early acceptance. For the first baseline, accept a complete
clean Full campaign automatically once the integrity gates below pass. Keep all
three repetitions and use their mean task success rate as the accepted score.

For replacement, require all of the following:

1. Compatible protocol, clean reproducible builds, 100% planned trial completion,
   native grader coverage, complete required evidence/accounting, and no unresolved
   infrastructure or cleanup failures. Ordinary valid task failures are allowed.
2. Candidate mean pass rate exceeds the contemporary accepted-build replay, with
   the lower endpoint of the paired task-cluster 95% bootstrap interval above zero.
3. Neither suite's mean pass rate decreases versus that replay.
4. Candidate effective cost per pass is at most 1.10× the control's. Candidate
   geometric-mean agent-time ratio on paired successful trials is at most 1.10×,
   with at least five distinct tasks represented. If a ratio cannot be computed,
   hold the baseline instead of assuming the guard passed.
5. Candidate mean pass rate is at least the stored accepted mean, preventing an
   automatic nominal decrease when the control replay itself deteriorates.

These are conservative **engineering acceptance rules**, not a guarantee against
noise or repeated-testing bias. Equal-score cost-only improvements and larger
quality/cost tradeoffs are reported but do not auto-promote under policy v1.
Changing the policy is an explicit versioned decision made before another
campaign, not tuning thresholds after seeing the current results.

Write `acceptance.json` with every gate, metric, control/candidate run reference,
policy version and reason. On acceptance, acquire a short exclusive lock, verify
the launch baseline revision is still current, and atomically publish the new
pointer and immutable history entry. If it changed, retain results and record a
stale-baseline decision; do not silently compare against a different control or
launch another campaign. Use one authoritative eval checkout/host for publication
in v1. Git transport of small baseline files is sufficient; no remote locking service.

An accepted baseline is a reference to evidence and a reproducible build, not a
number manually pasted into a file. Keep historical scores and contemporary
replays visible so provider drift can be distinguished from agent changes.

## Artifact storage and retention

Every run automatically writes sanitized small reports under
`evals/reports/<run-id>/`. Keep private bulk artifacts in ignored
`evals/artifacts/<run-id>/`: stdout/stderr/events, SQLite snapshot, external content,
call ledger, verifier logs, submitted patch, and runner-native output. Builds and
downloaded task bundles live in an ignored content-addressed cache.

Reports contain hashes and relative paths for those artifacts. Keep accepted
baseline builds/evidence indefinitely, and keep ordinary evidence until deliberate
manual cleanup; no automatic pruning in v1. If relocating bulk files later, add
an explicit archive URI/checksum rather than breaking the baseline reference.
Remote object storage can be added when local disk becomes a real constraint.

Never put credentials, raw environment dumps, private prompts/transcripts, or
full provider headers in Git reports. Reuse current integrity scanning and add a
whitelist-based public report projection. An exact-key scan is not proof that all
secrets are absent. Existing report corrections become versioned analyses.

## Implementation sequence and validation

1. **Freeze the contract.** Add `evals/README.md`, Python/uv setup, schema types,
   task lock, protocol, and fixed profiles. Resolve all 30 task/grader/image inputs
   and document the two deadline overlays and network differences. Validate exact
   membership, units, hashes, duplicate detection, and no missing Full tasks.
2. **Extract and generalize the adapter.** Reuse observer/finality/accounting/
   content-integrity code and its tests. Parameterize candidate identity instead
   of a hardcoded engine study. Bind the current agent-definition/config contract.
   Verify unlimited CLI semantics, provider metadata freeze, DeepSWE commit
   collection, numeric paging, and root/child/helper accounting with fake calls.
3. **Implement the supervisor.** Bounded cross-suite pool, resource reservations,
   deterministic paired scheduling, unique homes/processes, independent phase
   envelopes, graceful cancellation and owned cleanup. No resume paths or cost
   reservation ledger. Test actual overlapping fixture jobs, maximum concurrency,
   verifier headroom, cancellation, child processes, and cleanup isolation.
4. **Implement one normalization/report path.** Golden small fixture records for
   passes, native failures, missing grades, request/task timeouts, partial exports,
   unknown usage, duplicate records, and corrupt content. Check denominators,
   Decimal totals, matching task projections, paired wins/losses, and interval
   reproducibility. Reports must not turn an early crash into a speed improvement.
5. **Implement baseline policy/publication.** Pure policy tests for every gate,
   invalid/partial runs, exact ties, unknown cost, subset exclusion, incompatible
   tracks, dirty builds, stale pointers and interrupted atomic writes. Two
   simultaneous publication attempts must never clobber each other. Test initial
   acceptance separately from replacement. No model calls from report/compare.
6. **Validate the real path.** Run `doctor` through both native runners and both
   engines, including service survival during grading and separate-verifier
   patch collection. Qualify host concurrency as above. Then perform a real Smoke
   run, a Medium A/B, and the clean Full initialization campaign. Publish the
   resulting reports and initial accepted baseline only when their gates pass.
7. **Document and review.** Add a canonical-evals section to `docs/features.md`
   linking behavior → code → tests; add/check its `docs/roadmap.md` item when
   shipped; link the new workflow from the repository README and historical eval
   README. Run the focused Python suite and repository-required `task check`.
   If any Go concurrency code changes become necessary, also run relevant race
   tests. Review failure attribution, limit propagation, and least-code scope.

No benchmark expenditure is part of this planning change. Implementation acceptance
has explicitly enumerated paid campaigns; running those later requires an active
authorization covering that work. Do not reinterpret the earlier study's $300 as
a silent per-run cap or perpetual authorization for future campaigns.

Definition of done: from the documented prerequisites, one command produces an
8-, 15-, or 30-task report in the standard location; A/B runs preserve matched
conditions; failed/missing work is visible; cancellation cleans owned resources;
baseline initialization/publication is implemented and can advance automatically
with an auditable decision. Actual initial baseline collection is deferred by the
user's execution amendment above. No CI or resume implementation is required.

## Deliberate boundaries and future considerations

- A small public suite becomes a development target after repeated use. Keep
  optimization on Smoke/Medium and use Full for milestones; repeated Full runs
  still do not establish generalization to unseen coding work. Add a separately
  versioned broader/held-out benchmark when that becomes important.
- Do not auto-update task lists, images, model aliases, pricing assumptions, or
  framework versions. Dependency upgrades need a protocol audit and control run.
- Keep correctness, cost, latency and reliability separately visible. Improvements
  in one dimension can hide regressions in another, especially with early failures.
- Start with one host and files. Add remote artifact storage, distributed workers,
  CI or a dashboard only when their absence creates a measured operational problem.
- Frontier's repository has no LICENSE file at the inspected revision. Do not
  vendor its workflow scripts or redistribute its result corpus as licensed code.
  Implement the workflow in Whip, use source links and factual task identifiers,
  and retain the underlying task/framework licenses. Public redistribution of
  third-party bundles requires checking their notices. [S9, L7]

## Sources and local implementation references

- **S1:** [Pinned Frontier manifest](https://github.com/frontier-harness-eval/eval/blob/e837a70bd6beb4e72eeeda62dd06e3bd34f6cb63/benchmark.json).
- **S2:** [Pinned Frontier methodology and reproduction notes](https://github.com/frontier-harness-eval/eval/blob/e837a70bd6beb4e72eeeda62dd06e3bd34f6cb63/README.md).
- **S3:** [Frontier runner task resolution](https://github.com/frontier-harness-eval/eval/blob/e837a70bd6beb4e72eeeda62dd06e3bd34f6cb63/skills/frontierharness-eval/scripts/run-trials.sh#L168) and [legacy registry](https://raw.githubusercontent.com/laude-institute/harbor/main/registry.json), retrieved September 10; SHA-256 recorded in the research JSON.
- **S4:** Published [regex metadata](https://github.com/frontier-harness-eval/eval/blob/e837a70bd6beb4e72eeeda62dd06e3bd34f6cb63/tasks/regex-log/task.toml) and [SQLite metadata](https://github.com/frontier-harness-eval/eval/blob/e837a70bd6beb4e72eeeda62dd06e3bd34f6cb63/tasks/sqlite-db-truncate/task.toml).
- **S5:** Executable [regex metadata](https://github.com/laude-institute/terminal-bench-2/blob/69671fbaac6d67a7ef0dfec016cc38a64ef7a77c/regex-log/task.toml), [SQLite metadata](https://github.com/laude-institute/terminal-bench-2/blob/69671fbaac6d67a7ef0dfec016cc38a64ef7a77c/sqlite-db-truncate/task.toml), and [DeepSWE task contract](https://github.com/datacurve-ai/deep-swe/blob/435ee89ec2f2e2289f33b0da4f992f0b7b7266b9/tasks/anko-typed-variable-bindings/task.toml).
- **S6:** [Docker CPU/memory constraints](https://docs.docker.com/engine/containers/resource_constraints/).
- **S7:** [Inference.net rate limits](https://docs.inference.net/api/rate-limits), checked September 10. The actual account's capacity has not been verified in this planning task.
- **S8:** [Frontier difficulty metadata](https://github.com/frontier-harness-eval/eval/blob/e837a70bd6beb4e72eeeda62dd06e3bd34f6cb63/metadata/difficulty.json).
- **S9:** [Frontier repository tree at the pinned revision](https://github.com/frontier-harness-eval/eval/tree/e837a70bd6beb4e72eeeda62dd06e3bd34f6cb63).
- **L1:** [Existing eval workflow](../../../evals/runtime-ab/README.md), [controller](../../../evals/runtime-ab/study.py) (`run`, approximately lines 179–330).
- **L2:** [Observer](../../../evals/runtime-ab/observe.py) (`write_config`, `discover_catalog`, `run`, approximately lines 322–524).
- **L3:** [Harbor/Pier adapter](../../../evals/runtime-ab/whip_adapter.py), including native proxy injection and observer cleanup.
- **L4:** [Artifact integrity](../../../evals/runtime-ab/artifact_integrity.py), [result evidence](../../../evals/runtime-ab/result_evidence.py), and their existing tests.
- **L5:** [Completed native-limits study](../../../evals/runtime-ab/NATIVE-LIMITS-RESULTS.md).
- **L6:** [Production provider client](../../../internal/llm/openai.go), `NewOpenAI` HTTP client timeout near line 407; [production defaults](../../../docs/features.md#model-usage-budgets).
- **L7:** [Earlier Frontier research](../../../../quickjs-wasi-research/notes/frontier-harness.md).
- Repository architecture: [feature map](../../../docs/features.md), [roadmap](../../../docs/roadmap.md), [concurrency contracts](../../../docs/concurrency.md).
