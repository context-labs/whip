# Starlark / QuickJS evaluation

This runs the actual Whip daemon, recursive agents, worker subprocesses, host
tools, accounting, and checkpoint storage against `kimi-k3` on
`https://api.inference.net/v1`, using Harbor 0.22.0 and Pier 0.3.1. It is a local
Docker adaptation of selected upstream tasks, **not an official Frontier run
or a leaderboard-comparable score**. Frontier workflow code is not included.

The primary study uses [the v2 freeze](PREREGISTERED-V2.md). A separately
reported [repository follow-up](PREREGISTERED-REPOSITORY-FOLLOWUP.md) removes
the cumulative token cap while keeping the same dollar and agent-time limits.
The [native-limits condition](PREREGISTERED-NATIVE-LIMITS.md) removes all
experimental cost, cumulative-token, round and output-token caps, restores
production RLM defaults, and uses each task's native agent deadline.
Its first three attempts are recorded in `results/native-limits/`.
After attempt 3, a [collector correction](PREREGISTERED-NATIVE-LIMITS-V2.md)
continues the remaining 13 in `results/native-limits-v2/`. The original HTTPX
grade remains valid; its missing content bodies and independently reconciled
call accounting are disclosed in the amendment. See the
[native-limits report](NATIVE-LIMITS-RESULTS.md) and
[earlier results and evidence](RESULTS.md). A separate
[corrected-runtime Anko pair](PREREGISTERED-NATIVE-NUMERIC-FIX.md) follows the
numeric host-argument bug discovered in the native study. It uses the frozen
`results/build-numeric-fix/` binary and harness. Its
[accounting continuation](PREREGISTERED-NATIVE-NUMERIC-FIX-CONTINUATION.md)
retains failed provider calls and an explicit reserve without changing agent
limits or replacing any attempt. The original
[v1 freeze](PREREGISTERED.md) and its four attempts remain available with an
[abort audit](results/formal/ABORTED.json); v2 is a documented amendment after
those failures, not an untouched holdout. Local interpreter microbenchmarks are in
[Runtime measurements](RUNTIME-MEASUREMENTS.md).

## Reproduce

Requirements: Go from `go.mod`, Docker, Python 3.12, and an exported
`INFERENCE_API_KEY`. Never put a literal key in a task, manifest, or command.

The preparation script fetches the pinned static
[ripgrep 14.1.1 release](https://github.com/BurntSushi/ripgrep/releases/tag/14.1.1),
checks its archive SHA-256, and retains its licenses. Place `rg-linux-amd64`
beside the Whip binary; the adapter uploads it for tasks whose network policy
prevents package installation. The recipe pins its binary digest.

```sh
uv venv /tmp/whip-runtime-ab-venv --python 3.12
uv pip install --python /tmp/whip-runtime-ab-venv/bin/python harbor==0.22.0 datacurve-pier==0.3.1
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o /tmp/whip-runtime-ab-linux ./cmd/whip
/tmp/whip-runtime-ab-venv/bin/python evals/runtime-ab/prepare_ripgrep.py /tmp
PYTHONPATH=evals/runtime-ab /tmp/whip-runtime-ab-venv/bin/python -m unittest discover -s evals/runtime-ab -p 'test_*.py'
```

First run `smoke/` using each actual runner/engine with the deterministic
provider (`--ak fixture=true`). This exercises Linux startup, a root finishing
before its child, whole-tree finality, usage export, actual `AgentContext`, and
the hidden verifier. Fixture results never count as model proficiency.

```sh
PYTHONPATH=evals/runtime-ab /tmp/whip-runtime-ab-venv/bin/harbor run \
  -p evals/runtime-ab/smoke -a whip_adapter:HarborWhip \
  -m inference-net/kimi-k3 --ak binary=/tmp/whip-runtime-ab-linux \
  --ak engine=starlark --ak fixture=true --ak timeout=100 \
  --job-name offline-starlark --jobs-dir /tmp/whip-runtime-ab-smoke \
  -n 1 --max-retries 0
```

Replace the engine with `quickjs`; for Pier use `pier run` and
`--agent-import-path whip_adapter:PierWhip` in place of Harbor's `-a`.

The two pilot tasks are authored here and excluded from the formal sample:
one terminal data task, one repository repair task. The pilot uses one paired
repetition, a $1.50 cap per trial, a $6 total cap, and 300 seconds of agent time.

```sh
/tmp/whip-runtime-ab-venv/bin/python evals/runtime-ab/study.py \
  --phase pilot --binary /tmp/whip-runtime-ab-linux \
  --output evals/runtime-ab/results/pilot --trial-cap 1.5 --total-cap 6 --timeout 300
```

For the formal sample, obtain these Apache-2.0 task repositories at the pinned
revisions. The runner mounts instructions and environment only into the agent;
reference solutions are neither read into prompts nor mounted there.

| Source | Revision | Selected tasks |
| --- | --- | --- |
| [Terminal tasks](https://github.com/harbor-framework/terminal-bench-2) | `2fd12b88aafdd04a52c298e3940bcb189f9766d6` | `regex-log`, `openssl-selfsigned-cert` |
| [DeepSWE tasks](https://github.com/datacurve-ai/deep-swe) | `435ee89ec2f2e2289f33b0da4f992f0b7b7266b9` | `anko-typed-variable-bindings`, `httpx-multipart-response-parsing` |

Exact historical reproduction requires the archived binary (or the verified
source/embedded-asset reconstruction in `results/build/build.json`) and each
study's frozen harness. A fresh build from a changed checkout is a new build
condition. The commands below use separate output directories and preserve
the recorded studies. Archived artifacts are local and ignored by Git.

Verify restricted inference egress in both original repository images before
the formal run. These two small paid probes have a combined $0.20 ceiling and
are separate from task scores:

```sh
DOCKER_DEFAULT_PLATFORM=linux/amd64 /tmp/whip-runtime-ab-venv/bin/python evals/runtime-ab/preflight.py \
  --binary /tmp/whip-runtime-ab-linux --output /tmp/whip-runtime-ab-proxy-preflight
```

```sh
/tmp/whip-runtime-ab-venv/bin/python evals/runtime-ab/results/build/formal-v2-harness/study.py \
  --phase formal --binary "$PWD/evals/runtime-ab/results/build/whip-linux-amd64" \
  --terminal /tmp/whip-runtime-ab-terminal --deepswe /tmp/whip-runtime-ab-deepswe \
  --output /tmp/whip-runtime-ab-formal-v2-reproduction --repetitions 2 \
  --trial-cap 2.5 --total-cap 40 --timeout 900 --max-tokens 500000 --max-turns 60 \
  --max-output 32768
/tmp/whip-runtime-ab-venv/bin/python evals/runtime-ab/summarize.py /tmp/whip-runtime-ab-formal-v2-reproduction
```

The exploratory repository condition uses its separately frozen harness:

```sh
/tmp/whip-runtime-ab-venv/bin/python evals/runtime-ab/results/build/repository-followup-harness/study.py \
  --phase formal --suite repository \
  --binary "$PWD/evals/runtime-ab/results/build/whip-linux-amd64" \
  --terminal /tmp/whip-runtime-ab-terminal --deepswe /tmp/whip-runtime-ab-deepswe \
  --output /tmp/whip-runtime-ab-repository-followup-reproduction --repetitions 1 \
  --trial-cap 2.5 --total-cap 10 --timeout 900 --max-tokens 0 --max-turns 60 \
  --max-output 32768
```

## Frozen comparison and analysis

The native-limits condition is reproduced with the frozen harness below.
`--native-limits` reads task deadlines and sets all four experimental caps to
zero regardless of their legacy defaults. `--total-cap` is the overall testing
authorization; it is never passed to Whip as a per-trial cap. Prior exposure
is included when deciding whether to dispatch additional attempts. Monitor
aggregate exposure during uncapped trials; unknown accounting halts dispatch.

```sh
/tmp/whip-runtime-ab-venv/bin/python evals/runtime-ab/results/build/native-limits-v2-harness/study.py \
  --phase formal --suite all --repetitions 2 --native-limits \
  --binary "$PWD/evals/runtime-ab/results/build/whip-linux-amd64" \
  --output /tmp/whip-runtime-ab-native-limits-reproduction \
  --total-cap 300 --prior-exposure 23.936665
```

This uses the corrected collector. The original `native-limits-harness`
archive retains its known post-completion content-path bug as historical evidence.
To reproduce the recorded continuation specifically, add `--start-index 3`
and use `--prior-exposure 27.042690` in a separate output directory. The first
three attempts are retained from the original condition, never replaced.

The exposure values above reproduce the historical dispatch conditions.
For additional paid testing, use the actual accumulated exposure against the
overall authorization.

After all 16 recorded attempts finish, derive the combined analysis without
rewriting either raw trial index:

```sh
/tmp/whip-runtime-ab-venv/bin/python evals/runtime-ab/combine_native_results.py
/tmp/whip-runtime-ab-venv/bin/python evals/runtime-ab/native_diagnostics.py \
  evals/runtime-ab/results/native-limits-combined
```

The combiner verifies the exact original schedule and the original-file hashes
in the accounting reconciliation. Its separate derived records preserve source
provenance and document the accounting/finality correction for attempt 3.

The corrected-runtime Anko pair used `results/build-numeric-fix/harness/`
with its adjacent corrected binary, `--task anko-typed-variable-bindings`,
`--repetitions 1`, and `--native-limits`. Its original prior exposure was
$64.621382. The first attempt remains in `results/native-numeric-fix/`.
After its three unknown-usage provider failures, the documented $75 reserve
allowed the original second attempt to run in `results/native-numeric-fix-v2/`
with `--start-index 1 --prior-exposure 144.263857`. The model inputs, binary,
task, and execution settings were unchanged. Both outcomes are retained.
The default ten-minute provider-call timeout remained a product limit and
ended the QuickJS attempt; it was not the native 90-minute Anko task deadline.

Derive this separate pair from the retained evidence:

```sh
/tmp/whip-runtime-ab-venv/bin/python evals/runtime-ab/combine_numeric_fix_results.py
/tmp/whip-runtime-ab-venv/bin/python evals/runtime-ab/native_diagnostics.py \
  evals/runtime-ab/results/native-numeric-fix-combined
/tmp/whip-runtime-ab-venv/bin/python evals/runtime-ab/audit_trials.py \
  evals/runtime-ab/results/native-numeric-fix evals/runtime-ab/results/native-numeric-fix-v2 \
  --build evals/runtime-ab/results/build-numeric-fix \
  --output /tmp/whip-runtime-ab-numeric-fix-audit.json
```

The external exposure adjustment preserves `accounting_complete=false` for
QuickJS and never rewrites either raw index. The native report includes both
failed follow-up results and explains their distinct causes.

The settings below describe the earlier primary study; the native-limits
freeze above records its deliberately different settings.

- Same binary, model route, high reasoning effort, 32,768 maximum output tokens,
  500,000 cumulative tree tokens, 60 CLI rounds, and four worker slots per formal arm.
  Pilots used 120,000 tokens and 30 rounds; the terminal pilot approached that
  token ceiling, so the formal study increases headroom within the same $2.50 cap.
  The v1 8,192-output-token cap stopped both regex attempts before execution;
  v2 raises that shared limit and retains the original failures separately.
  Temperature and top-p use provider defaults (the request omits those fields).
  All helper/compaction routes remain Kimi K3 and enter the same call ledger.
- Primary host concurrency is one for both engines. QuickJS may queue bounded
  Promise requests, but only one host operation executes at a time. Native
  asynchronous fan-out is a separate microbenchmark condition.
- Every trial has a fresh container, home, session tree, and workspace. Each
  task's native CPU, memory, network, and verifier requirements are retained.
  Agent execution is serial across trials; setup/grading time is recorded
  separately from the observer's agent duration.
- Seed 20260910 shuffles task order; engine order alternates within paired
  tasks and repetitions. `recipe.json` freezes ordered tasks, source hashes,
  binary hash, settings, and harness hashes before the first call. A changed
  recipe refuses to reuse the same output directory.
- Zero automatic runner retries. Retain every attempt, including startup,
  runtime, accounting, timeout, budget, and verifier failures. A failed verifier
  is a failed task; a missing verifier is unknown/infrastructure failure and is
  never converted to success. Report both all-attempt success and exclusions,
  if any, with causes. Pilot tuning attempts stay separate.
  Only the native `reward` field determines success; Pier's other fields
  include test counts and partial scores. Missing reward remains unknown.
  Pier retains its native verifier infrastructure retry; this is separate from
  rerunning the agent/task. The follow-up recipe records the distinct timing
  envelopes and preserves the native grader limits.
- Compare raw verifier pass counts, paired outcomes, all-trial cost and agent
  duration, and cost/time among passes. With only four distinct formal tasks,
  this is descriptive evidence with high task-selection uncertainty. Repeated
  attempts are not independent new tasks. Any bootstrap interval must resample
  task clusters (both engines and repetitions together); no broad superiority
  or default-switch conclusion is warranted from this sample.
  Final agent timing comes from the observer outcome; stale intermediate timing
  is retained as partial evidence. Paired cost, token, or timing comparisons
  require complete evidence for that quantity. Complete-evidence and pass-only
  summaries include counts; early failure can shorten time until termination.

## Observation and accounting

`observe.py` launches `whip run` and reads consistent, read-only SQLite
transactions. A root final response is insufficient: CLI exit plus no running
turn, queued/running input, runnable mail, or pending provider call must remain
true with no new events for two seconds. The observer then verifies the
daemon's locked owner PID, executable and home, freezes only that process, and
reads the store again. If new work raced with the freeze, it resumes planning
and waits. Future schedules/deferred mail are recorded and cannot wake during
grading. The container ends after grading.

The adapter updates the real Harbor/Pier `AgentContext` throughout execution
and exports per-call route, token, cost, and status evidence. Unknown usage and
cost remain unknown. Billed cost, Whip's settled accounting ledger, and
independent Decimal normalization using each call's pricing are separate.
The older capped studies reserve a full trial cap when evidence is uncertain.
The native-limits controller reserves all remaining authorization and halts
further dispatch; an evidence-backed, separately recorded accounting amendment
is required to continue. The corrected Anko pair retains a $75 conservative
reserve for three failed model requests with incomplete local usage. The HTTP 502
has zero provider observability cost; the later HTTP 520 and 600-second request timeout have no completed
provider records at lookup time. It does not repeat a provider
request to repair accounting. The observer is evaluation
instrumentation and does not replace Whip's agent loop or mutate its store.

Each trial retains raw CLI/events, root and descendants, transcripts, call and
budget rows, checkpoint envelopes, a consistent SQLite backup, catalog,
configuration, instruction hash, verifier output, and one authoritative runner
result.

The current observer additionally exports root-owned external content bodies
once, after freezing/stopping the daemon and taking the final database backup.
It verifies sizes and hashes and records `content-export.json`; missing or
corrupt bodies make export failure explicit. This improvement was applied
only after the primary and follow-up studies completed. Their original frozen
observers omitted those bodies. Derived analysis can recover a retained tool
transcript only when its serialized bytes match the stored content identity;
pruned canonical event rows require explicit retention-range validation.
Reproduction of either historical study uses its frozen scripts above. DeepSWE instructions require the agent to commit within its disposable
repository; the adapter never commits or fixes a failed solution afterward.

On timeout, stop the CLI process group and daemon before final export. Setup,
agent, verifier, evidence export, and hard cleanup have distinct time budgets.
Normal completion preserves task service processes through grading; their
daemon-owned output drains are paused with the daemon. Docker removes only
the owned trial containers. The sampled process-tree RSS includes shared pages
per process; `/proc` CPU sampling may miss very short-lived processes. These
are operational observations, not exact physical-memory or complete CPU counts.


After every scheduled attempt has finished, derive the analysis and inspect
its evidence coverage before interpreting aggregate timing or cost:

```sh
/tmp/whip-runtime-ab-venv/bin/python evals/runtime-ab/summarize.py evals/runtime-ab/results/formal-v2
/tmp/whip-runtime-ab-venv/bin/python evals/runtime-ab/audit_trials.py \
  evals/runtime-ab/results/formal-v2 evals/runtime-ab/results/repository-followup \
  --output evals/runtime-ab/results/final-audit/trials.json
/tmp/whip-runtime-ab-venv/bin/python evals/runtime-ab/artifact_integrity.py evals/runtime-ab/results
```

The integrity manifest excludes itself, hashes regular files and decompressed
tar entries, and checks exact available provider-key values without printing
them. It also flags potential proxy configuration for review without exposing
values. Its scope and skipped formats are explicit; a clean exact-key check
does not establish that every possible secret is absent. Run it again after
changing derived artifacts to update their hashes. Raw results remain local
and ignored by Git.

## Runtime measurements

Run serially while builds, tests, and task containers are idle:

```sh
go test ./internal/rlm -run '^$' -bench '^BenchmarkRuntime' -benchmem -benchtime=1s -count=10 > evals/runtime-ab/results/microbench.txt
```

Checkpoint fidelity differs deliberately: Starlark's tagged partial state and
QuickJS's whole image. Compare bytes and recovery time together with that
semantic difference. Report OS/architecture and emulation separately for native
microbenchmarks and Linux task runs. Timing does not establish containment,
authority, value, or recovery correctness; those require the conformance tests.

Warm arithmetic and host-call microbenchmarks omit checkpoint storage; the
checkpoint case uses an in-memory store, so it measures image generation and
transport rather than SQLite persistence. Go `B/op` and `allocs/op` count the
benchmark parent, not the separate worker heap. Engine-specific timing counters
printed alongside Go timings are the last cell's diagnostics, not repeated-run
averages. Use the repeated `ns/op` observations for latency comparisons. These
runs use a shared desktop; retained load evidence and confidence intervals
describe the observed noise, not dedicated-machine performance guarantees.
