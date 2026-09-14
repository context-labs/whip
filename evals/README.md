# Whip evaluations

Use `whip-eval` for new Frontier evaluations. It runs the real Whip CLI and
native Harbor/Pier graders, freezes the inputs, and writes a comparable report.
Kimi K3 on Inference.net, with high reasoning effort, is the initial model route.

**Execution environments:** the native Docker workflow and its retained reports
remain authoritative. The detached Modal workflow below is a separately qualified
cloud environment, not an accepted or automatically promoted native baseline.
Cloud resource admission, native fixtures, and live-model qualification are
separate gates; a successful deployment alone does not pass them.

## Detached Modal campaigns

The narrow cloud path keeps one complete native Docker trial in each disposable
Modal VM and one detached coordinator per immutable campaign. It does **not**
replace Harbor/Pier, change the task/agent/verifier limits or networking, retry a
model attempt, or introduce a resumable agent session. The default Full cloud
selection is the existing **30 mixed tasks × 3 repeats**, QuickJS, Kimi K3/high.

```sh
# Offline expansion only: 90 planned trials, no build/network/Modal/model calls.
uv run --project evals --locked whip-eval modal submit full --dry-run

# Submit only after the exact image/resource/network/native-fixture gates pass.
# settings.json is a non-secret qualification receipt, not provider configuration.
uv run --project evals --locked whip-eval modal submit full \
  --settings /path/to/settings.json --jobs 10 --run-id <unique-run-id> \
  --allow-model-calls
uv run --project evals --locked whip-eval modal status <run-id>
uv run --project evals --locked whip-eval modal logs <run-id> --trial t0001
uv run --project evals --locked whip-eval modal fetch <run-id>
uv run --project evals --locked whip-eval modal cancel <run-id>
uv run --project evals --locked whip-eval modal reconcile <run-id>
```

`submit` freezes an explicit allowlist: selected prepared tasks, candidate binaries,
ripgrep, contracts, pinned evaluator source and dependency locks. It never uploads
a repository/home/cache tree or provider credentials. Bundle identity and every
file are verified before the native worker runs. Submission records its immutable
intent before dispatch; duplicate or ambiguous creation never launches a second
attempt. The CLI may exit after submission: the coordinator and workers continue.

`status` and bounded `logs` show lifecycle evidence; `fetch` retains complete raw
native logs and available partial evidence in a new immutable snapshot under
`artifacts/<run>/fetches/` and a JSON/Markdown/CSV report under
`reports/<run>/fetches/`. All planned trials remain in the denominator. A worker
crash retains observed cumulative known cost, but the total remains unknown and
accounting/evidence incomplete. Infra billing is separate and unknown until
reconciled with provider billing; worker uptime is not an authoritative bill.
In cloud-fetched trial rows, `cleanup_complete` means the worker VM has a proven
exit, not merely native cleanup or a termination RPC acknowledgement. It is false
when VM exit is unproven. `native_cleanup_complete` separately preserves the
native runner's cleanup status (including null when unknown); VM exit can be
proven even when native cleanup was incomplete. Fetch accepts an independent
reconciliation's exit proof only when its run, trial, bundle, owner nonce, and
known VM identity match the immutable attempt. These cleanup fields do not
upgrade evidence finality or accounting completeness.

Host `fetch` downloads at most eight logical SDK file operations concurrently,
with at most eight submitted operations (not eight workers per trial). It streams
to exclusive same-directory temporary files, fsyncs, and publishes atomically
without overwriting prior snapshots; failed downloads do not publish partial
files or a successful report. Final file hashes and integrity/finality checks
remain mandatory. Collection deliberately uses the pinned Modal 1.5.5 private
`Volume._read_file_into_fileobj(..., concurrency=1)` hook also used by its CLI;
an incompatible SDK or missing hook fails closed. The public `read_file` iterator
prefetches CPU-count blocks; the selected hook instead streams one file block at
a time. Eight logical file streams is the public limit, not a global or
cross-process HTTP/memory cap: SDK metadata, coroutines, and transport buffering
remain SDK-managed. Local disk retains the full snapshot plus up to eight
in-progress files, with no automatic pruning or new byte quota. Control
acknowledgements, cancellation, and reconciliation remain in their existing
serial ownership-checked paths.

`cancel` records cancellation and independently signals proven-owned workers,
even if the coordinator died. `reconcile` attaches without redispatch, reports
worker liveness and durable completion, and delivers pending cancellation to late
creates. Ownership requires the exact durable intent plus full tags or an
unpredictable VM-local launch receipt—not a name prefix or shared Volume file.
If ownership cannot be proved, the worker is not destructively touched; inspect
again for a late receipt. Native cancellation/cleanup and the bounded VM lifetime
remain the safety backstop. Reconciliation never resumes a model session.

### Cloud administration and qualification

**Qualification checkpoint (2026-09-11):**
`modal-native-fixture-20260911-90b` passed an independent audit of 90 complete
native fixtures (45 Harbor + 45 Pier), using the canonical `e9c97bea…` candidate
binary (`75b26ce4…`), the pinned image, and 6 CPU / 24 GiB VMs. All 90 native
grades, evidence exports, and fixture accounting passed: 5,130 file hashes across
5,220 retained files, 450 fake model calls, and zero external provider calls.
Paired fresh platform polls proved overlap of the same 90 distinct admitted,
VM-local ownership-verified VMs—not 90 simultaneous native CPU or model calls.
All 90 VM exits were observed, and a full post-terminal planned-attempt scan
left no unknown workers. Infrastructure billing remains unreconciled.

The immutable local closure is
`artifacts/modal-qualification-20260911/90b-qualification-closure.json`
(SHA-256 `bc62599846bf7f08aeae2314187e25d3d6c55cf580943a7b0ca924b2751a894b`).
Its inventory/security audit found no persisted proxy configuration, but did not
perform an exact secret-value scan; this is not a universal secret-absence claim.
This closes the credential-free capacity/native-lifecycle gate only. Real-model
smoke, the scored 30 × 3 campaign (63 Harbor + 27 Pier attempts), and baseline
promotion remain separate, explicitly authorized gates.

All resources are explicitly in workspace `inference-net`, environment `whipcode`,
app `whip-eval`. Authenticate Modal outside the repository; do not include Modal
tokens in VM secrets. Using the locked SDK, deploy and create the named resources:

```sh
cd evals
uv run --locked modal deploy -m whip_evals.modal_cloud --env whipcode
uv run --locked modal volume create whip-eval-inputs --env whipcode
uv run --locked modal volume create whip-eval-evidence --env whipcode
uv run --locked modal dict create whip-eval-state --env whipcode
```

Provision the named Modal secret `whip-eval-inference` with **only**
`INFERENCE_API_KEY` through approved secret management, never a shell argument,
image, bundle, tracked JSON, or agent-accessible Modal account token. Fake-provider
fixture workers receive no provider secret. The coordinator alone has Modal
control-plane authority; native agent/verifier containers receive neither its
Docker socket nor Modal credentials.

`python -m whip_evals.modal_image --output <new-ignored-receipt.json>` explicitly
builds the dependency-only Debian/Python 3.12 base with frozen `uv.lock`, uv
0.12.13, checksum-pinned Docker 28.3.3, Compose 2.39.2, and Buildx 0.27.0. Clear
entrypoint and UNIX-only dockerd are required: the official DinD entrypoint opens
a Docker TCP listener reachable from inner containers. Any newly built image
must be qualified again; submit uses its immutable Modal image ID. Candidate code
is SHA-verified and extracted from the bundle, not baked from a moving checkout.

A non-secret settings file has this shape (replace the image/qualification IDs):

```json
{
  "environment": "whipcode",
  "app": "whip-eval",
  "worker_image_id": "im-QUALIFIED",
  "qualification_status": "passed",
  "qualification_receipt": "qualification-id",
  "fixture": false,
  "jobs": 10,
  "shapes": {
    "harbor": {"physical_cpus": 6, "memory_mb": 24576, "min_free_disk_mb": 100000},
    "pier": {"physical_cpus": 6, "memory_mb": 24576, "min_free_disk_mb": 100000}
  }
}
```

Outer VM resources are **not** inner task caps. The conservative initial 6 CPU /
24 GiB shape must fit each unchanged native whole-trial reservation and headroom.
Measure guest CPU/cgroups and usable RAM; do not assume a theoretical physical to
logical CPU multiplier. `--jobs` is the campaign admission ceiling, not a promise
of account capacity. Qualify the desired simultaneous peak separately.

For credential-free authored fixtures, use `fixture: true` in settings and:

```sh
uv run --project evals --locked whip-eval modal submit smoke --fixture \
  --engines quickjs --jobs 2 --settings /path/to/fixture-settings.json
```

This runs authored Harbor shared-container and Pier clean separate-verifier
fixtures using a fake provider; they are explicitly excluded from scored results.
After representative qualification passes, `--fixture-repetitions 45 --jobs 90`
with one engine prepares 90 fake-only lifecycle trials (settings must match jobs).
This is capacity/admission qualification, not the scored 30-task × 3 workload;
the two-engine fixture maximum is 22 repeats/88 trials.
Qualify task cgroups, provider-only CONNECT/DNS policy, verifier isolation, native
setup, actual observer metrics/state/events, abrupt loss, direct cancellation,
final export, and proven cleanup before real-model smoke or a 90-trial campaign.

Modal VM Volumes have boot-snapshot reads and asynchronous outbound commits;
`reload_volumes()` and later inbound Volume ACK/cancel updates are unsupported.
Inputs must commit before VM creation. Admission and final ACK/cancel use direct
VM-local filesystem control, and the coordinator reads evidence independently
through the Volume API. Finality requires every committed file hash plus a passing
integrity audit. Unconfirmed tails can be lost on abrupt VM death; do not infer
zero cost or lossless durability. Secret/integrity failures stop further admission.

Volume does not support native hard-link exclusive publication. Keep only the
native runner's immutable wrapper receipt directory on VM-local ext4; copy its
closed files once to evidence. Native job/observer logs remain on the durable
Volume, and cloud single-writer receipts use atomic rename in their uniquely
claimed attempt directory. Existing `common.atomic_write` is not weakened.
The worker resolves only the trusted Volume mount alias before constructing native
paths; descendant symlink rejection and content size/digest checks remain intact.
Cloud-only Pier proxy configuration/build context is generated in a private,
0700 VM-local `/tmp` directory from creation, using the unchanged pinned native
generator. Native trial paths are restored synchronously and that private directory
survives through native stop, then is removed. Local Docker behavior is unchanged.
Generated proxy credentials must never be published as native evidence.

The immutable bundle binds its staged controller/common/execution source digest
and worker measurement digest. Submit passes the expected controller digest as a
required coordinator argument; older signatures or mismatched deployments fail
before worker creation. A local deploy command starting is not deployment proof:
require its successful exit, app identity, and remote revision verification before
submitting a newly frozen bundle. Do not repair mismatches by editing a frozen run.

Raw evidence retention is an operational intent, not automatic expiry. No timer
prunes runs or qualification caches; retain incomplete evidence and reconcile
owned resources deliberately. This workflow does not change baseline promotion.

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
# Qualify the current host harness/fixtures against an immutable runtime commit.
uv run --project evals --locked whip-eval doctor --integration --ref <commit-sha>
```

It uses an authored fake provider, with no external model calls. Both engines and
both native runners exercise shared and separate verifiers, root/child finality,
whole-tree accounting, external content export, numeric file paging, a background
service surviving finality, and collection/application of a committed patch. It
writes private evidence under `artifacts/doctor-*/` and never contributes scores.
`doctor --ref` selects the runtime build for integration only; without it, the
existing working-file snapshot behavior is unchanged. The host harness and
fixtures always come from the current checkout; the result records build identity.

Pier fixtures exercise `no-network` in both shared and separate verifier modes.
The supported native environment import hook selects our small Docker extension,
which caps only the inference proxy's soft/hard `nofile` limits at 65,536. This
avoids Squid allocating an enormous FD table from host-inherited Docker defaults;
it does not relax task networks, proxy authentication, or the provider allowlist.
The extension depends on Pier 0.3.1's private proxy-preparation hook and fails
visibly on an incompatible proxy shape. The protocol and measured adapter/runner
launch hashes record this adaptation; the locked dependency itself is unchanged.

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
cross-checked before declaring complete accounting/evidence. Content bodies use one
native directory download into isolated staging, then exact manifest, regular-file,
size and SHA256 validation before atomic publication. The same 240-second cleanup
deadline covers transfer and validation; incomplete copies never become complete
evidence. Metadata transfers and native log collection remain separate.

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
