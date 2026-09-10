# Whip evaluations

Use `whip-eval` for new Frontier evaluations. It runs the real Whip CLI and
native Harbor/Pier graders, freezes the inputs, and writes a comparable report.
Kimi K3 on Inference.net, with high reasoning effort, is the initial model route.

**Implementation status:** offline validation is complete. No benchmark trials or
machine-qualification containers were run during implementation. There is no
accepted baseline yet. Establish it on the evaluation machine after qualification.

## Setup on the evaluation machine

Use a dedicated Linux amd64 machine with local Docker and Compose v2, Git, uv,
and the Go toolchain required by the repository's `go.mod`. The CLI also works
with Linux Docker on macOS for development, but emulated execution cannot promote
a baseline. A remote Docker daemon is unsupported: bind mounts and capacity must
refer to the same machine as the checkout.

From the repository root:

```sh
uv sync --project evals --locked
uv run --project evals --locked whip-eval doctor
uv run --project evals --locked whip-eval run smoke --dry-run
```

`doctor` reads prerequisites and Docker capacity. `--dry-run` only expands the
planned workload; it does not build, contact a provider, pull images, or run tasks.
Neither needs a provider key. Python 3.12+ and exact Harbor 0.22.0/Pier 0.3.1
versions are recorded in `uv.lock`.

The explicit integration check **does start eight disposable fixture trials**:

```sh
uv run --project evals --locked whip-eval doctor --integration
```

It uses an authored fake provider, with no external model calls. Both engines and
both native runners exercise shared and separate verifiers, root/child finality,
whole-tree accounting, external content export, numeric file paging, a background
service surviving finality, and collection/application of a committed patch. It
writes private evidence under `artifacts/doctor-*/` and never contributes scores.

For scored runs, supply `INFERENCE_API_KEY` through your environment or secret
manager. The CLI never asks you to put a literal key in a command or report.
Registry access, image downloads, package installation, and provider quota must
be available. Keep enough free disk for images, all retained evidence, and builds.

## Run an evaluation

| Profile | Terminal tasks | Repository tasks | Total | Purpose |
| --- | ---: | ---: | ---: | --- |
| `smoke` | 6 | 2 | 8 | Fast regression signal, including image and service tasks |
| `medium` | 10 | 5 | 15 | Broader development comparison, including the published hard task |
| `full` | 21 | 9 | 30 | Entire pinned Frontier v1 selection |

These are fixed nested subsets in [profiles.json](frontier/profiles.json).
Full means all 30 selected Frontier tasks, not the 89-task Terminal-Bench registry.
Each ordinary run makes one independent attempt per task. Repetition results are
averaged; there is no best-of-N score or automatic failed-task retry.

```sh
uv run --project evals --locked whip-eval run smoke
uv run --project evals --locked whip-eval run medium --label parser-change
uv run --project evals --locked whip-eval run full --ref HEAD

# Same frozen binary, two execution engines: 30 trials for Medium.
uv run --project evals --locked whip-eval run medium --engines starlark,quickjs

# Candidate from working files; replay an existing Git ref as control.
uv run --project evals --locked whip-eval run medium --against main

# Replay the accepted build as the contemporary control.
uv run --project evals --locked whip-eval run medium --against baseline
```

An omitted engine reads `rlm.defaultEngine` from Whip's JSONC configuration,
falling back to `starlark` when the preference is absent. `--engines quickjs`
selects it explicitly. Personal providers, instructions, MCP servers, and sessions
are not copied into the task homes. Each task starts with a fresh Whip home and
the frozen evaluation config; production agent/runtime defaults apply.

The default candidate captures current tracked and untracked source files, excluding
ignored caches and credential files. Dirty builds are allowed for development and
ineligible for baseline promotion. `--ref HEAD` builds committed source, including
when report files or unrelated work make the checkout dirty. Builds run from
isolated source snapshots; the CLI never switches, resets, stashes, or commits
your working tree. Runtime A/B shares one binary and changes only its engine config.

## Parallelism and limits

`--jobs 32` is the default **maximum active trial count across both suites, all
arms, and repetitions**. Each admitted worker owns one native runner subprocess;
its child agents are part of that trial. More than 32 provider calls can be in
flight because Whip itself can use descendants and helper calls.

The scheduler reserves native CPU, RAM, and disk declarations for the entire
trial, including a simultaneous separate verifier. It subtracts 2 CPUs, 4 GiB
RAM, and 10 GiB disk for host headroom. It reduces actual concurrency to fit;
a task that cannot fit alone fails preflight. Reservations release only after
owned-container cleanup succeeds. Requested and peak active trial counts are
reported. CPU quotas do not isolate your machine from unrelated workloads.

A historical cost estimate is printed when complete accepted-baseline costs are
available. It is a forecast, not a spending limit.

There are no evaluator spending, cumulative-token, round, or output-token caps.
`--max-cost`, `--max-tokens`, and `--max-turns` are passed as zero; the model's
advertised output capacity applies. Native task agent/verifier deadlines remain.
Preparation, image pulls and queueing occur outside agent task clocks.

Production Whip safeguards remain, including its provider request timeout and
runtime resource limits. They are agent behavior being measured. Longer outer
watchdogs cover native setup, grading, observer export, and cleanup hangs; they
do not shorten native agent deadlines. Reports distinguish these causes. Pier's
native verifier retry remains; it is not a second agent attempt.

For quality-preserving speed, qualify the target host and provider before adopting
32-way execution. Start with the fake-provider check, then inspect real Smoke runs
for provider errors, incomplete evidence, resource pressure and paired successful
latency. This implementation does not claim that 32-way quality has been measured.

## Reports and evidence

Every launch gets a unique UTC/random ID. `--run-id` is optional and an existing
ID is refused. Final reports live in `evals/reports/<run-id>/`:

- `manifest.json`: source/build identity, model/catalog/config, task hashes,
  resolved native controls, host/resource policy, schedule and launch baseline.
- `result.json`: versioned canonical scores, coverage, task records, comparisons,
  concurrency, timing and artifact references. Its publication marks finalization.
- `report.md` and `trials.csv`: projections of the same normalized result.
- `acceptance.json`: the decision for an explicit promotion campaign.

`request.json` records the planned workload; `progress.json` is temporary. A failure during preparation records
`preparation-error.json`; no task score is asserted without launching a campaign.
An interrupted execution finalizes a partial report where possible. A hard host
crash may leave only the manifest and private evidence.

Bulk evidence lives in ignored `evals/artifacts/<run-id>/`: native runner output,
verifier logs and patches, transcripts/events, SQLite snapshots, call accounting,
content bodies, contracts, and an integrity inventory. Ignored `evals/cache/`
retains exact source archives, binaries, ripgrep and prepared task bundles.
Small reports and baseline pointers are eligible for Git; nothing is auto-committed.
Retain accepted evidence **and its build cache** indefinitely. There is no automatic
pruning, remote storage, or distributed baseline lock in v1.

Useful offline operations:

```sh
uv run --project evals --locked whip-eval compare RUN_A RUN_B
uv run --project evals --locked whip-eval report RUN_ID
uv run --project evals --locked whip-eval baseline show
```

`report` writes `reports/<run-id>/analyses/<analysis-id>/` with an analysis-code hash;
it never changes the original accepted report. Historical comparisons project the
Full baseline onto the requested subset. Compatibility is explicit; different
protocols/hosts/catalogs/concurrency are descriptive comparisons. An arm can be
selected with `compare --control-arm ... --candidate-arm ...`.

Ctrl+C stops dispatch, cancels owned runner work, and keeps available evidence.
After a crashed runner has exited, `whip-eval cleanup RUN_ID` removes only containers
whose native identity and exact log mount prove ownership. Cleanup cannot resume
execution. Use a new run ID for a new experiment. Exit status 2 means a command
failed or the resulting run is partial; a complete run with valid task failures
still exits 0.

## Metric contract (schema version 1)

| Metric | Meaning |
| --- | --- |
| `verified_success_rate` | Valid native passes / all planned trials. Partial runs expose a confirmed floor, not a complete score. |
| `graded_pass_rate` | Passes / authoritative grades; always shown with missing coverage. |
| `success` | `true` for native reward 1, `false` for 0, `null` for missing/invalid grade. |
| `cost_usd` | Complete ledger USD as a six-decimal string; null if accounting/usage is incomplete. |
| `known_cost_usd` | Known cost subtotal including failed work; never silently substitutes for complete cost. |
| `effective_cost_per_pass_usd` | All-trial complete cost / passes; null for incomplete cost or zero passes. |
| `agent_seconds` | Observer-measured whole-tree agent time; excludes queue/setup/verifier. |
| `phase_seconds` | Queue, native environment/setup/agent/verifier spans, cleanup; unavailable spans are null. Native agent span includes adapter overhead. |
| `common_success_latency_ratio` | Geometric mean candidate/control time on pairs both passed; coverage shown. Early failures cannot improve this metric. |
| `delta_interval_95` | Seeded 10,000-resample task-cluster bootstrap; retains both arms and repetitions within each sampled task. |

Suites and repetitions have their own summaries. Unknown usage/timings remain
null; HTTP-status telemetry is explicitly unavailable in the current durable call
ledger. Diagnostic cost repricing and sampled RSS/CPU are not complete billing or
exclusive-process resource measurements. Native grades remain authoritative even
when execution ends abnormally; evidence and accounting coverage are separate.
SQLite calls, exported state, metrics, finality, identity and content digests are
cross-checked before declaring complete accounting/evidence.

## Accepted baseline

The initial track is `frontier-v1-local-v1/kimi-k3-inference-high`. Ordinary runs
report comparisons without changing it or launching additional trials.

After committing the implementation and qualifying the evaluation machine:

```sh
# Initial baseline: 30 tasks × 3 repetitions = 90 paid trials.
uv run --project evals --locked whip-eval run full --ref HEAD --promote

# Later: accepted build and candidate each get 90 trials = 180 total.
uv run --project evals --locked whip-eval run full --ref HEAD --promote
```

The initial complete, clean campaign is accepted automatically. Replacement also
requires a positive lower endpoint of the paired 95% quality interval, no suite
regression, cost per pass and common-success latency each at most 1.10× control,
at least five shared successful tasks, and no decrease from the stored accepted
score. Full native grade/evidence/accounting coverage and matching protocol/build
identity are mandatory. Ties and cost-only improvements are reported but held.
These thresholds govern acceptance, **not spending or early termination**.

`acceptance.json` lists every gate. A locked compare-and-swap updates
`baselines/<track>/current.json` and immutable history only if the launch baseline
is still current. A simultaneous stale campaign retains its report and cannot
clobber the winner. Use one authoritative checkout/host for publication, and move
its private artifacts/builds together with the small Git records.

Changing pinned tasks, graders, runner versions, model catalog, protocol,
measurement code, concurrency or host class may invalidate compatibility. Establish
a new explicitly named protocol/track in `frontier/protocol.json` before collecting
its baseline; never edit an existing accepted result to make it compatible.
Repeated tests on a small public suite are development evidence, not proof of
performance on unseen tasks.

## Pinned methodology and maintenance

[tasks.lock.json](frontier/tasks.lock.json) freezes the 30 IDs, source/archive and
file hashes/modes, Linux amd64 OCI image digests, native resources, graders and
declared deadline overlays. `regex-log` and `sqlite-db-truncate` explicitly use
the published 1,200-second agent deadline instead of the executable copies' 900
seconds. Preparation verifies every selected file and never mounts solution files.
Native network declarations/defaults apply, with Pier's agent provider exception
for `api.inference.net`; the resolved task configuration is recorded.

The [Frontier source](https://github.com/frontier-harness-eval/eval/blob/e837a70bd6beb4e72eeeda62dd06e3bd34f6cb63/README.md)
used Fireworks/Runta and did not record its original applied network allowlist.
Our local adaptation therefore has no published leaderboard rank.
[references.json](frontier/references.json) keeps a few attributed external scores
as context, separate from Whip's accepted baseline. Cost and timing definitions
from external reports are not silently equated with ours.

Pin updates are deliberate: resolve new source/archive hashes and OCI digests,
review task inventories and native parsing, increment the relevant profile/protocol,
and collect a new baseline. Do not auto-refresh moving tags. Upstream workflow
scripts/results corpora are not vendored; source archives retain their notices.

## Offline development checks

```sh
uv run --project evals --locked python -m unittest discover -s evals/tests -p 'test_*.py'
uv run --project evals --locked python -m unittest discover -s evals/runtime-ab -p 'test_*.py'
```

These use authored records, temporary SQLite databases, Python worker threads,
and mocks. They do not launch benchmark trials or containers or call real models.
Legacy mocked-controller tests print historical study labels and budgets; those
are test fixtures, not runs. The historical study remains under [runtime-ab](runtime-ab/README.md),
and deterministic Go evaluation tests remain under [rlm](rlm/README.md).
There is no eval CI, execution resume, or dashboard in this initial implementation.
