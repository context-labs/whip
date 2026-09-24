# Whip runtime A/B on the FrontierHarness task subset, adapted methodology

The subsequent [native-limits study](NATIVE-LIMITS-RESULTS.md) removes the
experimental task budgets and round/output caps: Starlark 6/8, QuickJS 7/8
across 16 additional attempts. It also exposed a numeric host-argument defect
in QuickJS. The correction and separately recorded Anko follow-up are documented
there: both follow-up attempts failed, Starlark on one feature group and
QuickJS on a provider-call timeout before committing. All 18 additional
outcomes are retained. The earlier results below are preserved unchanged.

Study date: 2026-09-10. The primary study is complete: **Starlark 4/8,
QuickJS 3/8** using real Kimi K3 calls. Starlark has lower measured runtime
overhead; QuickJS retains the complete settled guest heap. This small selected
task sample does not establish an overall effectiveness winner or justify
changing the default. The separately recorded repository follow-up also finished: neither engine
passed either repository task.

## What was implemented
s
Whip now supports Starlark and JavaScript through bundled QuickJS/WASI in its
real daemon, worker, tool, policy, accounting, and storage paths. Execution
language is selected when a root session is created and remains immutable
through children, retries, forks, eviction, and restart. Starlark remains the
default. CLI, SDK, web/mobile creation and history, and TUI rendering support
both engines. The public protocol deliberately advances to major 6; clients
and daemon must be upgraded together.

QuickJS uses a bounded asynchronous host-call pump with one VM owner and
captures complete guest images only after a cell has settled. Starlark keeps
its documented partial checkpoint semantics. Exact host integers and decimal
values, common authorization/accounting, corrupt-image rejection, and
interruption behavior are covered by conformance tests. Pending host calls
are not durable continuations across daemon restart.

See [runtime contracts](../../docs/rlm-runtime.md),
[protocol upgrade](../../docs/protocol-v2.md), and
[the implementation record](../../.ai-docs/plans/multi-runtime-benchmark/README.md).

## Primary task study

The fixed v2 study ran four selected tasks, both engines, and two repetitions:
16 attempts. It uses the actual `kimi-k3` route on inference.net, high default
reasoning effort, 32,768 maximum output tokens per call, 500,000 cumulative
tree tokens, a $2.50 cost admission budget, and 900 seconds of agent time.
Both arms have four worker slots and one active host operation at a time.
Temperature and top-p are omitted and use provider defaults. Task order is
seeded; engine order is balanced and reversed in the second repetition.

Native Harbor/Pier verifiers determine task success. Only `reward == 1`
passes; partial scores and test-count diagnostics do not. Every attempted
trial remains in the denominator, including missing verification and harness
failures. Agent time and whole-trial time are separate because setup and
grading have their own budgets.

Agent duration means time until termination, including unsuccessful work.
The final observer outcome supplies complete duration; intermediate telemetry
is retained as a partial observation when final timing is absent. Cost/time
subset summaries state their coverage. Paired cost or timing intervals are
omitted when a pair lacks complete evidence. Pass-only comparisons condition
on each engine's successful subset and therefore have selection bias.

Primary evidence: [frozen recipe](results/formal-v2/recipe.json),
[raw trial index](results/formal-v2/trials.json),
[derived analysis](results/formal-v2/analysis.json), and
[per-trial CSV](results/formal-v2/trials.csv).

**Starlark passed 4/8 attempts; QuickJS passed 3/8.** Both engines passed
the certificate task twice. Starlark passed regex twice; QuickJS passed it
once. Every graded repository attempt stopped at token admission with an
empty committed patch. The other repository attempt was interrupted by an
optional telemetry timeout and was never graded.

| Engine | All attempts | Terminal tasks | Repository tasks | Verifier coverage | Complete accounting |
| --- | ---: | ---: | ---: | ---: | ---: |
| Starlark | 4/8 | 4/4 | 0/4 | 7/8 | 7/8 |
| QuickJS | 3/8 | 3/4 | 0/4 | 8/8 | 8/8 |

| Measurement | Starlark | QuickJS |
| --- | ---: | ---: |
| Median agent time, complete observations | 240.3 s (n=7) | 131.4 s (n=8) |
| Median estimated cost, complete accounting | $0.472038 (n=7) | $0.388056 (n=8) |
| Pass-only median agent time | 146.8 s (n=4) | 45.5 s (n=3) |
| Pass-only median estimated cost | $0.284939 (n=4) | $0.091176 (n=3) |
| Median whole-trial time, all attempts | 285.5 s (n=8) | 209.2 s (n=8) |
| Observed model cost estimate | $3.112885 + one unknown call | $3.596684 |

The QuickJS-minus-Starlark difference in all-attempt pass rate is −12.5
percentage points. The preregistered whole-task bootstrap gives a descriptive
95% interval of **−37.5 to 0 percentage points** (four task clusters, 256
enumerated resamples). Seven of eight pairs have comparable final cost and
agent timing. Their differences are retained, but overall paired cost/time
intervals are omitted because the remaining pair lacks complete evidence.

All primary attempts, in dispatch order:

| Task · repetition | Engine | Verifier outcome | Agent seconds | Estimated cost | Calls |
| --- | --- | --- | ---: | ---: | ---: |
| [Regex · 1](results/formal-v2/jobs/formal-01-regex-log-starlark-r1/regex-log__Jm8TMn5/result.json) | starlark | Pass | 317.6 | $0.584075 | 14 |
| [Regex · 1](results/formal-v2/jobs/formal-02-regex-log-quickjs-r1/regex-log__un9wSAp/result.json) | quickjs | Pass | 154.1 | $0.292704 | 8 |
| [HTTPX · 1](results/formal-v2/jobs/formal-03-httpx-multipart-response-parsing-starlark-r1/httpx-multipart-response-parsing__SuMRKY4/result.json) | starlark | Ungraded: telemetry failure | 116.7† | $0.366717 + ? | 21 |
| [HTTPX · 1](results/formal-v2/jobs/formal-04-httpx-multipart-response-parsing-quickjs-r1/httpx-multipart-response-parsing__6S2iUqr/result.json) | quickjs | Fail: token admission | 322.0 | $0.713994 | 27 |
| [Anko · 1](results/formal-v2/jobs/formal-05-anko-typed-variable-bindings-quickjs-r1/anko-typed-variable-bindings__EesSqdW/result.json) | quickjs | Fail: token admission | 80.1 | $0.419384 | 20 |
| [Anko · 1](results/formal-v2/jobs/formal-06-anko-typed-variable-bindings-starlark-r1/anko-typed-variable-bindings__zYKdM5J/result.json) | starlark | Fail: token admission | 151.2 | $0.422023 | 23 |
| [Certificate · 1](results/formal-v2/jobs/formal-07-openssl-selfsigned-cert-quickjs-r1/openssl-selfsigned-cert__skzZVjE/result.json) | quickjs | Pass | 45.5 | $0.091176 | 8 |
| [Certificate · 1](results/formal-v2/jobs/formal-08-openssl-selfsigned-cert-starlark-r1/openssl-selfsigned-cert__tDmZNiv/result.json) | starlark | Pass | 38.0 | $0.076297 | 7 |
| [Regex · 2](results/formal-v2/jobs/formal-09-regex-log-quickjs-r2/regex-log__DvnvD6y/result.json) | quickjs | Fail: token admission | 514.1 | $1.157074 | 20 |
| [Regex · 2](results/formal-v2/jobs/formal-10-regex-log-starlark-r2/regex-log__KTqeW2P/result.json) | starlark | Pass | 240.3 | $0.476262 | 8 |
| [Certificate · 2](results/formal-v2/jobs/formal-11-openssl-selfsigned-cert-starlark-r2/openssl-selfsigned-cert__bVUpCpN/result.json) | starlark | Pass | 53.2 | $0.093616 | 9 |
| [Certificate · 2](results/formal-v2/jobs/formal-12-openssl-selfsigned-cert-quickjs-r2/openssl-selfsigned-cert__iScqZgJ/result.json) | quickjs | Pass | 41.0 | $0.084250 | 7 |
| [HTTPX · 2](results/formal-v2/jobs/formal-13-httpx-multipart-response-parsing-quickjs-r2/httpx-multipart-response-parsing__G4YkwXF/result.json) | quickjs | Fail: token admission | 147.6 | $0.481375 | 28 |
| [HTTPX · 2](results/formal-v2/jobs/formal-14-httpx-multipart-response-parsing-starlark-r2/httpx-multipart-response-parsing__rASAPBH/result.json) | starlark | Fail: token admission | 265.6 | $0.472038 | 27 |
| [Anko · 2](results/formal-v2/jobs/formal-15-anko-typed-variable-bindings-starlark-r2/anko-typed-variable-bindings__AoByhty/result.json) | starlark | Fail: token admission | 247.7 | $0.621857 | 22 |
| [Anko · 2](results/formal-v2/jobs/formal-16-anko-typed-variable-bindings-quickjs-r2/anko-typed-variable-bindings__8iFD3LQ/result.json) | quickjs | Fail: token admission | 115.1 | $0.356727 | 28 |

† The ungraded attempt has only its last telemetry duration, not final agent
timing. Its observed $0.366717 is incomplete; the accounting ledger retains
the full $2.50 trial reservation. No request was repeated to recover usage.

The seven graded repository failures submitted zero-byte patches because
the native runner collects `git diff BASE HEAD`. Finishing and committing
the repair before the budget stops the agent is part of this task protocol.
Reported input plus output exceeded the 500,000-token admission cap slightly in
some attempts; the largest observed primary total was 507,220 tokens.
Input estimates admit calls; reported usage settles afterward and can pause
subsequent work.

Primary execution diagnostics:

| Observation | Starlark | QuickJS |
| --- | ---: | ---: |
| Recorded model calls | 131 | 146 |
| Completed execution tool calls | 146 | 174 |
| Decoded result-v2 cells | 143 | 174 |
| Errors in decoded cells | 8 | 14 |
| Unavailable referenced results | 3 | 0 |
| Checkpoint warnings / omission notices / restore failures | 0 / 0 / 0 | 0 / 0 / 0 |
| Median verified final checkpoint size | 2,556 B (n=7) | 1,573,335 B (n=8) |
| Median sampled process-tree peak RSS | 312.6 MiB | 367.7 MiB |

Fourteen large results were reconstructed from retained tool transcripts only
after their exact serialized size and SHA-256 matched the canonical event,
database reference/object, and root grant. Each event counts once. The three
unavailable bodies belong to the telemetry-interrupted Starlark attempt,
which lacks a database export. Raw records remain unchanged; recovery and
coverage are recorded in [the audit](results/formal-v2/audit.json). Error and
checkpoint-notice counts cover decoded cells only.

All 277 recorded primary calls used `inference-net/kimi-k3`; all observed
result engines and all 15 available checkpoint BLOBs matched their assigned
engine and integrity metadata. No descendant or helper-model activity was
observed. The model used both runtimes mainly for shell and file operations.
The live tasks do not test forced eviction/restart or establish child-runtime
inheritance; those behaviors are covered separately by conformance tests.
The small, incomplete cell-error sample does not establish a JavaScript
familiarity advantage.

High effort is inferred from the frozen ordinary-turn request construction
and pinned configuration. Per-call wire effort is not stored. Auxiliary
helper/compaction request constructors omit effort in this build, so their
provider default would be unknown; none occurred in the primary study.

The JavaScript guide added 521–528 reported tokens to the first
root prompt across these pairs. Aggregate RSS includes generated shell/file
work and shared pages; one Starlark peak is only a partial observation.
Retained checkpoint bytes measure the final generation, with different
fidelity between the engines. These diagnostics do not isolate interpreter
speed; use the controlled runtime measurements below for that question.

## Exploratory repository follow-up

The first primary repository attempts reached token admission limits well
before the dollar cap. One Starlark attempt also lost its optional telemetry
connection and was interrupted by the adapter. The separately recorded
[follow-up decision](REPOSITORY-FOLLOWUP-PLAN.md) and
[separate freeze](PREREGISTERED-REPOSITORY-FOLLOWUP.md) therefore schedule one
additional attempt per repository task and engine, with the same $2.50 and
900-second limits and no cumulative token cap. Its adapter tolerates missed
intermediate telemetry samples while preserving observer failures, final
evidence checks, and bounded cleanup.

This is an exploratory budget and harness change after observing outcomes.
Its four trials are reported separately and never replace primary failures.

**Starlark 0/2; QuickJS 0/2.** All four native verifiers ran, all four
submissions were zero-byte patches, and all four attempts have complete
accounting and final timing. Each made 60 ordinary calls plus one final call.

| Task | Engine | Native outcome | Agent seconds | Estimated cost | Input + output tokens |
| --- | --- | --- | ---: | ---: | ---: |
| [HTTPX](results/repository-followup/jobs/formal-01-httpx-multipart-response-parsing-starlark-r1/httpx-multipart-response-parsing__Z7Je7TJ/result.json) | starlark | Fail | 527.0 | $1.684093 | 1,841,807 |
| [HTTPX](results/repository-followup/jobs/formal-02-httpx-multipart-response-parsing-quickjs-r1/httpx-multipart-response-parsing__LfMSfGc/result.json) | quickjs | Fail | 695.7 | $1.872557 | 2,156,397 |
| [Anko](results/repository-followup/jobs/formal-03-anko-typed-variable-bindings-quickjs-r1/anko-typed-variable-bindings__ah6wcX4/result.json) | quickjs | Fail | 570.7 | $1.765832 | 2,242,783 |
| [Anko](results/repository-followup/jobs/formal-04-anko-typed-variable-bindings-starlark-r1/anko-typed-variable-bindings__zoDoGA5/result.json) | starlark | Fail | 809.4 | $2.531982 | 3,110,518 |

| Measurement, n=2 complete attempts per engine | Starlark | QuickJS |
| --- | ---: | ---: |
| Median agent time until termination | 668.2 s | 633.2 s |
| Median estimated cost | $2.108038 | $1.819195 |
| Total estimated cost | $4.216075 | $3.638389 |

There were no successful attempts, so pass-only time and cost are undefined.
Two tasks with one repetition do not support a useful population-level
interval; paired differences remain in [the derived analysis](results/repository-followup/analysis.json).
The [completion audit](results/repository-followup/completion-audit.json) confirms
that the binary and harness remained frozen through all four attempts.

The HTTPX pair reached 60 ordinary model rounds, after which Whip made one
forced final-answer call without tools. Both final messages acknowledged
that the changes had not been committed. The native task protocol collects
`git diff BASE HEAD`, so those drafts produced zero-byte submissions and
failed all 122 feature tests while preserving all 1,272 existing tests.
Neither attempt exhausted dollar or agent-time limits. No worker or harness
failure was observed; all 61 calls per attempt settled and final exports were
complete. Draft correctness is unknown because uncommitted work was not graded.

QuickJS Anko also finished at the round limit with acknowledged implementation
bugs, missing tests, and no commit. Starlark Anko reached the final-answer call
with $2.190940 already settled. Admission reduced that call's output allowance
from 32,768 to 4,297 tokens and reserved $0.309042. Reported usage settled the
call at $0.341042, taking the root to **$2.531982**. Whip then failed the turn
with `model accounting: model budget exhausted: capability denied` (CLI exit 1).
The $0.031982 overrun is an admission-estimation difference with settled usage,
not an unknown charge or a worker crash. Both Anko submissions failed all nine
feature tests and passed all 94 existing tests.

The follow-up contains 244 Kimi K3 calls, with no observed children or helper
calls. All four checkpoint BLOBs passed identity and integrity checks. Ordinary
script errors occurred, but no worker crash or checkpoint-loss notice was
observed. Eight early Starlark results required separate reconstruction after
normal event retention pruned their canonical database rows; their exact bodies
still matched retained references, grants, and transcripts. This recovery is
identified separately in [the final audit](results/final-audit/trials.json).



## Runtime overhead, without a model

These are equivalent hand-authored programs through Whip's actual worker
path, with ten serial samples per condition on native Darwin/arm64.

| Operation | Starlark median | QuickJS median |
| --- | ---: | ---: |
| Warm 1,000-iteration arithmetic cell | 0.0549 ms | 1.1755 ms |
| Warm single host call | 0.0538 ms | 0.8863 ms |
| Eight 1 ms host calls, concurrency one | 11.82 ms | 14.42 ms |
| Changed counter plus memory checkpoint | 0.0563 ms | 8.7297 ms |
| Evict, restore, evaluate next cell | 34.99 ms | 221.06 ms |
| New worker plus first cell | 48.07 ms | 214.39 ms |
| Counter checkpoint bytes | 95 B | 1,507,802 B |

QuickJS's separately labeled asynchronous condition completes the eight host
waits in 6.13 ms with up to 16 active calls. That measures overlapping waits;
the primary comparison uses concurrency one. The 95-byte Starlark checkpoint
is partial, while the QuickJS image retains the complete settled heap.

Matched timing differences have benchstat p < 0.001 in these repeated samples.
This does not establish task effectiveness or population-level superiority.
Warm arithmetic/host rows omit checkpoint storage; the checkpoint row uses
memory rather than SQLite. Parent Go allocations exclude the worker heap.
See [measurement methods, confidence ranges, and raw samples](RUNTIME-MEASUREMENTS.md).

## Pilots, amendments, and accounting

The four authored pilot attempts passed their native task verifiers and are
excluded from the selected task study. The QuickJS terminal pilot also has
one interrupted child call with unknown usage and a later mailbox-triggered
root failure. Its verifier pass does not imply clean agent completion; the
raw events and accounting uncertainty are retained in [pilot evidence](results/pilot/analysis.json).

| Phase | Attempts | Native passes | Observed cost estimate | Charged or reserved exposure | Complete accounting |
| --- | ---: | ---: | ---: | ---: | ---: |
| Authored pilots | 4 | 4 | $0.539523 | $1.852592 | 3/4 |
| Aborted v1 | 4 | 0 | $0.352576 | $5.352576 | 2/4 |
| Restricted-egress probes | 2 | — | $0.034181 | $0.034181 | 2/2 |
| Primary v2 | 16 | 7 | $6.709569 | $8.842852 | 15/16 |
| Exploratory repository follow-up | 4 | 0 | $7.854464 | $7.854464 | 4/4 |

Across 28 task attempts and two paid setup probes, observed catalog cost is
**$15.490313**; charged or reserved exposure is **$23.936665**, within the
assumed $50 overall allocation. No call supplied provider-reported billing.
The [accounting audit](results/final-audit/accounting.json) retains phase totals
and recorded unknown-call counts. Reservations are an accounting policy, not
a guaranteed upper bound on later reported usage or a provider invoice.

The two restricted-egress probes each ran actual Whip/Kimi calls in original
repository images. Both reached their intended execution cell, allowed the
provider endpoint, and observed blocked unrelated egress. They are setup
checks, not graded task attempts. The two v1 provider-discovery failures lack
usage exports and retain full $2.50 reservations. The pilot and primary each
retain one recorded call with unknown usage/cost; absent v1 telemetry does
not establish zero provider usage.


All cost amounts are catalog-based estimates unless explicitly marked as
provider-reported billing. Whip's per-call ledger and independent Decimal
normalization are retained separately. Unknown calls retain their trial
reservation; missing usage is never treated as zero. Cost and token admission
use input estimates, so subsequently reported usage can exceed an admission
budget and cause further work to pause, as the final Starlark follow-up shows.

The original v1 used an 8,192-token output cap. Both regex attempts exhausted
it in reasoning before making an execution call. Both HTTPX attempts failed
provider discovery because Pier's restricted inference proxy was not applied
to the launched process. Four attempts are retained, including a separately
audited recovery of the final native result; 12 other scheduled v1 attempts
were never dispatched. The [v2 amendment](PREREGISTERED-V2.md) documents the
shared output-limit change, proxy repair, and native reward interpretation
before v2 requests. V2 is therefore not an untouched holdout.

## Reproducibility and limits

The Linux/amd64 benchmark binary is frozen with SHA-256
`d36c04ce815a30d5a4f659fef4672cd263fd62407f3d896db8e15b6309c8f524`.
Its source archive, working-tree patch, module and embedded-asset manifests,
compiler environment, engine descriptors, language guides, and reconstruction
audit are retained in [build evidence](results/build/build.json).
Rebuilding the archived source and assets produced the identical binary hash.
The checkout was shared and dirty; a commit ID alone would not identify this
build. No commit or publication of the shared checkout was performed.

QuickJS embeds quickjs-wasi 3.6.0 via wazero 1.12.0. Its WASM SHA-256 is
`b006d95d9475edf7c6648cc3eb391d3b780efdd99022fbfbb470f2359da460ff`;
the bridge hash is
`cdec212203a787c609049bb76d1fd3f4be8b16439653e1c57832c6562a4883de`.
Actual generated guides are archived: Starlark 11,408 bytes, JavaScript
13,200 bytes. Different syntax and prompt length are part of the agent
effectiveness comparison, not pure interpreter speed.

Original Apache-2.0 task trees, source revisions, base commits, instructions,
verifiers, image identities, and a pinned static ripgrep are archived. The
task archive excludes reference solutions. Per-trial Docker identity records
capture image digests, architecture, limits, and mounts without credential
environment values. The daemon and workers run inside fresh task containers;
developer homes and daemon sockets are not mounted. Repository tasks retain
restricted egress with inference.net as the sole provider allowlist. The
adapter never commits or repairs a generated solution after the agent stops.

Native microbenchmarks ran on an Apple M4 Max, macOS 26.3.1 and Go 1.27.0.
Task runs used Docker 29.4.0 through OrbStack, Linux/amd64 under Rosetta on the
same shared desktop. Other applications remained active. Runtime microbench
latencies cannot be substituted for Linux task execution time. Sampled
process RSS double-counts shared pages; sampled CPU can miss short processes.

Four purposefully selected tasks are a small, biased sample. Repetitions
share tasks and are not 16 independent problem samples. Task-cluster
bootstrap intervals preserve the pair/repetition structure but cannot remove
selection bias. Provider RNG, prefix-cache state, scheduling, and an immutable
Kimi weights revision were not controlled. The route name and actual call
usage are recorded. This is adapted local Harbor/Pier methodology, not an
official Frontier run or a leaderboard-comparable score.

## Validation

The full `task check` passed, as did the final runtime race suite, affected
daemon/session/config/protocol/CLI checks, Linux/amd64 QuickJS conformance,
SDK/web/mobile tests and type checks, and TUI tests. Browser light/dark and
desktop/mobile-width views were inspected. The deterministic `evals/rlm`
fixture now exercises both languages against exact value, handle, and citation
assertions with synthetic usage clearly labeled; its focused tests and vet
passed with live calls disabled. [Validation logs](results/validation/task-check.log)
and [fixture validation](results/validation/deterministic-fixtures.json) are retained.

After all trials finished, the observer was updated to export external content
bodies with root ownership, size, and digest checks. It does not change the
recorded studies. All 76 final harness and analysis tests passed. They cover large-result
recovery, retention gaps, finality, unknown accounting, cleanup, and artifact
integrity ([test log](results/validation/final-harness-tests.log)). Frozen primary/follow-up scripts remain archived separately from
this future-run improvement.

The [final trial audit](results/final-audit/trials.json) verifies route,
engine, checkpoint, and evidence coverage. The [environment audit](results/repository-followup/environment-audit.json)
and [completion audit](results/repository-followup/completion-audit.json)
confirm unchanged production source and identical per-task images across
both studies. [The artifact manifest](results/final-audit/artifact-integrity.json)
records file hashes and the explicitly bounded credential-scan scope.
No exact configured provider-key matches were found. Sixteen ancillary Compose
files retain random per-trial proxy tokens for removed containers; these private
raw files are not a sanitized sharing bundle. The [proxy review](results/final-audit/proxy-config-review.json)
records only paths and indicators, never token values. Model/verifier records
remain unchanged.


All benchmark artifacts referenced here are local under the ignored
`evals/runtime-ab/results/` directory. [The workflow](README.md) describes
reproduction and distinguishes frozen primary scripts from the exploratory
follow-up harness.
