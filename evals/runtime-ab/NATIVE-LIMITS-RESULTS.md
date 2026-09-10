# Whip runtime comparison with native task limits

Study date: 2026-09-10. All **18 additional attempts** are complete. The main
16-attempt native-limits study scored:
**Starlark 6/8, QuickJS 7/8**, at an estimated model cost of **$40.684717**.
A separately recorded Anko pair after fixing a QuickJS integration bug scored
**0/1 for each engine**: Starlark submitted a patch with one missed feature
group; QuickJS stopped at the production provider-call timeout before committing.
That follow-up has **$8.040252 known cost plus three unknown-usage requests**.
All outcomes are retained, with the two binary conditions reported separately.

## Original 16-attempt native-limits results

| Task | Starlark | QuickJS |
| --- | ---: | ---: |
| Regex extraction | 1/2 | 2/2 |
| Self-signed certificate | 2/2 | 2/2 |
| HTTPX multipart parsing | 2/2 | 2/2 |
| Anko typed bindings | 1/2 | 1/2 |
| **Total** | **6/8 (75%)** | **7/8 (87.5%)** |

Both engines passed 3/4 repository attempts. Terminal tasks were 3/4 for
Starlark and 4/4 for QuickJS. Compared with the earlier primary condition,
repository outcomes improved from 0/8 to 6/8 across the two engines; the
terminal aggregate remained 7/8. These are fresh trajectories under changed
settings, not recovered or replaced earlier attempts.

All 16 native verifiers ran. No attempt ended at a task deadline, model-output
cap, cumulative-token cap, or cost cap. One QuickJS execution cell hit a
production host-request guard because of the numeric-argument defect; the
agent recovered. The three failed submissions were graded code/answer errors.

| Measurement, all eight attempts per engine | Starlark | QuickJS |
| --- | ---: | ---: |
| Median agent duration | 406.9 s | 623.2 s |
| Median estimated model cost | $1.396688 | $2.439872 |
| Total estimated model cost | $13.907129 | $26.777588 |
| Model calls | 437 | 752 |
| Completed execution cells | 464 | 755 |
| Execution-cell errors | 42 | 45 |
| Worker-discard incidents | 0 | 1 |
| Checkpoint warnings / omissions / restore failures | 0 / 0 / 0 | 0 / 0 / 0 |

These duration/cost summaries include failures and measure work until the
agent stopped. Pass-only subsets have different task mixes: their median
agent times are 406.9 s (Starlark, n=6) and 343.6 s (QuickJS, n=7), and median
costs are $1.396688 and $0.651627. They should not be read as matched efficiency
comparisons. Both HTTPX pairs passed and provide the clearer paired comparison
discussed below.

The paired pass-rate difference is +12.5 percentage points for QuickJS. The
descriptive task-cluster bootstrap interval is 0 to +37.5 points (four task
clusters, 256 resamples). The paired mean differences are +282.1 seconds and
+$1.608807 for QuickJS; corresponding descriptive intervals are 48.8–515.5
seconds and $0.104394–$3.113221. This tiny selected sample does not establish
a general runtime winner, especially with the implementation defect below.

| Task · repetition | Engine | Native grade | Agent seconds | Estimated cost | Calls |
| --- | --- | --- | ---: | ---: | ---: |
| [Regex extraction · 1](results/native-limits/jobs/formal-01-regex-log-starlark-r1/regex-log__Mm93jSJ/result.json) | starlark | Pass | 176.9 | $0.369655 | 9 |
| [Regex extraction · 1](results/native-limits/jobs/formal-02-regex-log-quickjs-r1/regex-log__xrFvB2i/result.json) | quickjs | Pass | 141.4 | $0.279653 | 4 |
| [HTTPX multipart parsing · 1](results/native-limits/jobs/formal-03-httpx-multipart-response-parsing-starlark-r1/httpx-multipart-response-parsing__2XXnVyT/result.json) | starlark | Pass | 636.8 | $2.456717 | 92 |
| [HTTPX multipart parsing · 1](results/native-limits-v2/jobs/formal-04-httpx-multipart-response-parsing-quickjs-r1/httpx-multipart-response-parsing__iUuN3dc/result.json) | quickjs | Pass | 1648.9 | $8.460261 | 229 |
| [Anko typed bindings · 1](results/native-limits-v2/jobs/formal-05-anko-typed-variable-bindings-quickjs-r1/anko-typed-variable-bindings__uMdpJjD/result.json) | quickjs | Pass | 1514.8 | $6.101658 | 167 |
| [Anko typed bindings · 1](results/native-limits-v2/jobs/formal-06-anko-typed-variable-bindings-starlark-r1/anko-typed-variable-bindings__NGgAVuK/result.json) | starlark | Fail | 798.5 | $2.959071 | 82 |
| [Self-signed certificate · 1](results/native-limits-v2/jobs/formal-07-openssl-selfsigned-cert-quickjs-r1/openssl-selfsigned-cert__cAS3Qox/result.json) | quickjs | Pass | 47.0 | $0.096456 | 9 |
| [Self-signed certificate · 1](results/native-limits-v2/jobs/formal-08-openssl-selfsigned-cert-starlark-r1/openssl-selfsigned-cert__GMUcj2q/result.json) | starlark | Pass | 60.0 | $0.090382 | 8 |
| [Regex extraction · 2](results/native-limits-v2/jobs/formal-09-regex-log-quickjs-r2/regex-log__MuKppUB/result.json) | quickjs | Pass | 343.6 | $0.651627 | 9 |
| [Regex extraction · 2](results/native-limits-v2/jobs/formal-10-regex-log-starlark-r2/regex-log__Fu4YLCV/result.json) | starlark | Fail | 108.4 | $0.167046 | 3 |
| [Self-signed certificate · 2](results/native-limits-v2/jobs/formal-11-openssl-selfsigned-cert-starlark-r2/openssl-selfsigned-cert__vXGHjGK/result.json) | starlark | Pass | 32.5 | $0.064590 | 6 |
| [Self-signed certificate · 2](results/native-limits-v2/jobs/formal-12-openssl-selfsigned-cert-quickjs-r2/openssl-selfsigned-cert__mA3k8Kt/result.json) | quickjs | Pass | 41.2 | $0.081511 | 7 |
| [HTTPX multipart parsing · 2](results/native-limits-v2/jobs/formal-13-httpx-multipart-response-parsing-quickjs-r2/httpx-multipart-response-parsing__gLZRvPR/result.json) | quickjs | Pass | 902.9 | $4.228117 | 143 |
| [HTTPX multipart parsing · 2](results/native-limits-v2/jobs/formal-14-httpx-multipart-response-parsing-starlark-r2/httpx-multipart-response-parsing__5rG2Uk7/result.json) | starlark | Pass | 664.4 | $2.423720 | 96 |
| [Anko typed bindings · 2](results/native-limits-v2/jobs/formal-15-anko-typed-variable-bindings-starlark-r2/anko-typed-variable-bindings__HwkAkw8/result.json) | starlark | Pass | 1233.4 | $5.375948 | 141 |
| [Anko typed bindings · 2](results/native-limits-v2/jobs/formal-16-anko-typed-variable-bindings-quickjs-r2/anko-typed-variable-bindings__zqEHt9Y/result.json) | quickjs | Fail | 1328.4 | $6.878305 | 184 |

Evidence: [combined analysis](results/native-limits-combined/analysis.json),
[per-trial CSV](results/native-limits-combined/trials.csv),
[source provenance and reconciliation](results/native-limits-combined/provenance.json),
[raw audit](results/native-limits-combined/raw-audit.json), and
[execution diagnostics](results/native-limits-combined/diagnostics.json).

The audit verified 1,189 model calls, all 16 checkpoints, and all 1,219 completed
execution-cell results, including 23 referenced results. No helper or child
model activity occurred. The original post-export finality flag is the only
raw audit exception and is reconciled separately below. The audit was saved
before the subsequent product fix changed the source checkout.

## What changed from the earlier experiments

This condition removes the experiment's cumulative token budget, per-trial
dollar budget, round cap, and maximum-output override. It restores production
RLM configuration, selecting only the execution engine. Repository tasks get
their native 5,400-second agent deadline; terminal tasks retain their native
900-second deadline. The native verifiers, task inputs, pinned source revisions,
and scoring remain unchanged. Only native reward 1 counts as a pass.

| Setting | Earlier primary study | Earlier repository follow-up | This condition |
| --- | --- | --- | --- |
| Cumulative tree tokens | 500,000 | Unlimited | Unlimited |
| Per-trial cost admission | $2.50 | $2.50 | Unlimited |
| Ordinary root rounds | 60 | 60 | Unlimited |
| Maximum output per call | 32,768 | 32,768 | Provider-advertised maximum, 1,048,576 |
| Repository agent deadline | 900 s | 900 s | Native 5,400 s |
| Terminal agent deadline | 900 s | Not run | Native 900 s |
| Host-operation concurrency | 1 | 1 | Production default, 16 |

All calls use `inference-net/kimi-k3`, with high effort on ordinary turns and
provider-default temperature/top-p. The exact production Linux binary is
unchanged between the older and newer experiments:
`d36c04ce815a30d5a4f659fef4672cd263fd62407f3d896db8e15b6309c8f524`.
There are four selected tasks, two engines, and two repetitions, dispatched
serially in the original seeded order. Repetition 2 reverses each engine pair's
order. Hidden test findings are not fed back into subsequent attempts.

The user's $300 authorization remains an external limit on total testing
exposure across experiments, rather than a smaller Whip per-trial budget.
Normal production execution bounds remain: for example, per-cell wall/memory
bounds and individual provider-call timeouts. This tests Whip with its normal
runtime safeguards; it does not disable every safety bound in the product.

## Completed solution failures

Starlark finished at 6/8. Its two misses were completed but incorrect solutions,
with no budget, deadline, interpreter crash, or observer interruption:

- **Regex, repetition 2:** the submitted expression was valid but accepted the
  invalid IP `172.16.0.256`, returning an extra date (`2000-05-10`). The native
  test expected nine matches and received ten.
- **Anko, repetition 1:** the generated Go patch passed 8/9 feature groups and
  all 94 existing tests. It missed multi-variable typed initialization such as
  `var left, right: int64 = [1,2]`, incorrectly assigning the whole array to
  the first typed variable and reporting the wrong variable in a related error.
  This is a defect in the generated Anko implementation, not in Whip's Starlark
  interpreter. Native reward remained zero despite the high partial test score.

QuickJS's final Anko attempt also passed 8/9 feature groups and all 94 existing
tests, but failed the same multi-variable initialization case with an
`interface {}` versus `int64` mismatch. Its submitted Go patch attempted array
unpacking but rejected a valid element. The attempt completed normally after
184 model calls and $6.88. A recovered runtime incident earlier in this attempt
is described below; its possible indirect effect on the later solution cannot
be ruled out.

Starlark's fresh second Anko attempt passed all 9/9 feature groups and 94/94
existing tests. It independently exercised multi-variable typed initialization
in its own tests. It used 141 calls and about $5.38, compared with 82 calls and
$2.96 for its first, unsuccessful attempt. More work can improve coverage, but
removing caps does not guarantee that the model will find every edge case.

## Why the earlier scores understated completed-task performance

The earlier primary condition scored Starlark 4/8 and QuickJS 3/8. Eight of
its nine non-passes hit cumulative token admission; the other was interrupted
by an optional telemetry probe timeout and was ungraded. The four repository
follow-up attempts removed that token cap but all stopped after 60 ordinary
calls plus a final call, submitting empty committed patches. None of these
earlier primary/follow-up attempts actually reached the 900-second agent
deadline. The experimental limits stopped work earlier.

Cumulative input tokens count repeatedly supplied history, including cached
tokens. They do not measure a single prompt's context-window size. A long
successful repair can therefore account for millions of input tokens at a
modest dollar cost. A 500,000-token cumulative admission cap was especially
restrictive for these repository tasks.

The first native-limits HTTPX pair demonstrates the difference concretely.
Starlark's first commit appeared at execution cell 88 and the solution passed
all 122 feature tests and 1,272 existing tests. QuickJS also passed all those
tests, taking 27.5 minutes and about $8.46. QuickJS's first Anko attempt passed
all nine feature groups and 94 existing tests, taking 25.2 minutes and about
$6.10; its first commit appeared at cell 163. These observed trajectories
could not have completed under the earlier round limits, and the QuickJS
trajectories also exceeded the old time and dollar allowances.

The change is not a randomized single-variable intervention: several limits
and host concurrency changed together, and model outputs vary between runs.
It shows that the previous experiment was strongly budget-constrained; it
does not isolate the causal contribution of each setting or predict exactly
how every earlier attempt would have continued.

## Runtime defect exposed by the benchmark

The final QuickJS Anko attempt exposed a real host-integration defect. Numeric
arguments were preserved as `json.Number` by the QuickJS bridge, but several
daemon argument readers accepted only `float64`. Consequently, content-read
offset and length arguments silently used their defaults. A valid paging loop
in cell 35 requested successive ranges but repeatedly received the first
8,192 bytes. It made 1,024 host calls in about seven seconds.

At the normal per-cell host-request limit, the worker queued a Promise
rejection but checked for a stalled guest before draining that rejection.
It incorrectly reported an unresolved Promise and discarded the worker. The
next cell resumed via shell reads, and the overall attempt completed normally.
The [retained incident](results/native-limits-combined/runtime-stall.json)
identifies the exact cell and surrounding events.

This was not an experimental dollar/round/task-time cap, and it did not itself
terminate the benchmark attempt. It is nevertheless a genuine implementation
problem. Other numeric controls could also have fallen back to defaults.
Consequently, the original QuickJS timings and trajectories cannot be treated
as measurements of an implementation with correct numeric host arguments.
The later Anko grade pinpoints an invalid generated patch, but does not prove
that the earlier runtime defect had no indirect effect on that solution.

The correction accepts exact integer arguments without rounding wide values
through floating point and drains queued local quota rejections before checking
for a stall. Both reproduced bugs now have passing regressions, including real
Linux pagination with both engines. The daemon and RLM race suites, 81 harness
tests, and all four runner/engine smoke tests passed. A separately archived
binary rebuilt identically from the original source plus these corrections.

The [post-fix Anko pair](PREREGISTERED-NATIVE-NUMERIC-FIX.md) uses the same native
limits, model settings, task input, and grader. It is a separate exploratory
comparison; the original 16 scores remain intact.

### Corrected-runtime follow-up

| Engine | Native result | Agent time | Known model cost | Calls | Cells / errors |
| --- | --- | ---: | ---: | ---: | ---: |
| [QuickJS](results/native-numeric-fix/jobs/formal-01-anko-typed-variable-bindings-quickjs-r1/anko-typed-variable-bindings__PyrULsw/result.json) | Fail: provider timeout; empty committed patch | 35.3 min | $4.642475 + unknown usage | 142 | 139 / 7 |
| [Starlark](results/native-numeric-fix-v2/jobs/formal-02-anko-typed-variable-bindings-starlark-r1/anko-typed-variable-bindings__ET8q6m8/result.json) | Fail: 8/9 feature groups, 94/94 existing tests | 12.3 min | $3.397777 | 86 | 111 / 9 |

QuickJS's corrected Anko attempt failed after **35.3 minutes**, because a model
request hit Whip's normal **600-second provider-call timeout**. This was not
the native 5,400-second task deadline, a dollar/token/round cap, or a REPL cell
timeout. The timeout ended the root turn before the model committed its work.
The native collector grades the committed diff, which was empty: 0/9 feature
groups, 94/94 existing tests, reward 0. Its unfinished working-tree changes
were not repaired, committed, or substituted into grading afterward.

This is a service/LLM-client failure outcome, not evidence that the corrected
QuickJS interpreter could not execute the solution. There were 139 completed
cells, seven recoverable cell errors, zero worker discards, zero checkpoint
warnings or restore failures, and complete content export. A real content
paging loop in cell 99 completed across offsets 0, 8,192, and 16,384; the
earlier repeated-first-page incident did not recur. The deterministic
regressions establish the fix; this single failed task does not establish an
accuracy improvement.

The run recorded 142 model calls and $4.642475 in known ledger cost. Three
failed requests consumed **965.758 seconds (16.1 minutes)**, about 46% of its
wall time. Total model-call time was 1,919.242 seconds. Keep these waits in
the end-to-end duration; subtracting them would not turn the unfinished
attempt into a successful solution.

Starlark completed normally, committed a 127,781-byte patch, and passed all
existing tests plus eight of nine feature groups. It failed
`TestTypedBindingsAdditionalRepresentativeFlows`: the valid declaration
`var left, right: int64 = [1,2]` was rejected as assigning `interface {}` to
`int64`, with an incorrect variable name in a related error. This is the same
generated-solution edge case missed by two earlier native attempts. No time,
budget, provider, worker, or checkpoint failure ended this Starlark attempt.
Removing caps and fixing host argument parsing did not guarantee that Kimi
K3 covered this case.

The production client timeout remained because this condition restores normal
Whip runtime/client configuration. The experimental caps were removed, but
this is **not a claim that every product timeout was disabled**. Unlike the
original 16 attempts, this follow-up was materially affected by a non-benchmark
timeout. Its score therefore cannot isolate REPL implementation quality.

During the corrected QuickJS attempt, one model request failed with upstream
HTTP 502 and no local usage. Whip retried normally. Authenticated provider
observability records the request at zero cost; the stored request's last tool
result exactly matches the local event bytes. This is provider-side failure
evidence, not a final invoice. Whip correctly retains unknown usage and a
$20.859667 uncertain cost reservation. The separately recorded
[accounting continuation](PREREGISTERED-NATIVE-NUMERIC-FIX-CONTINUATION.md)
preserves the raw controller halt and carries a conservative $25-per-request reserve
when dispatching the already scheduled Starlark attempt. That reserve exceeds
the $24.90368 cost of one request at both advertised token maxima and uncached
prices. It does not impose a per-trial cap or turn missing usage into zero.
A second request later failed with HTTP 520 after 364.571 seconds, then Whip
retried and resumed. No completed provider observability record was found for
that request at lookup time; neither its exact upstream cause nor a zero
charge can be inferred. The final 600-second timeout also has no completed
provider record at lookup time. The continuation carries $75 for the three
unknown requests, above the local $63.014322 uncertain reservation. These incidents
remain visible in the corrected pair's cost coverage and elapsed time.

Evidence: [paired analysis](results/native-numeric-fix-combined/analysis.json),
[CSV](results/native-numeric-fix-combined/trials.csv),
[raw audit](results/native-numeric-fix-combined/raw-audit.json),
[diagnostics](results/native-numeric-fix-combined/diagnostics.json),
[native failure excerpts](results/native-numeric-fix-combined/native-failures.json),
and [accounting receipt](results/native-numeric-fix/continuation-accounting.json).
The audit verified all 228 model calls, both checkpoint blobs, and all 250
completed cell results. The only audit exception is QuickJS's incomplete
model accounting. Both environments met the native task requirements.
The same rebuilt binary was used in both attempts; only the scheduled engine
selection differed. This is one previously inspected task, so no useful
accuracy confidence interval or general runtime ranking follows from this pair.

## Runtime overhead versus model behavior

The first HTTPX pair took about 637 seconds in Starlark and 1,649 seconds in
QuickJS. Recorded model-call time accounts for about 549 and 1,544 seconds,
respectively. About 995 seconds of the 1,012-second difference is therefore
additional model-call time. The remaining time includes shell work, tests,
orchestration, checkpointing, and runtime execution; it is not all interpreter
time. More model calls can themselves result from runtime/API friction,
including the numeric-argument defect, so this timing decomposition does not
establish that the extra time was independent of the implementation.

QuickJS used 229 model calls and had 21 recoverable execution-cell errors in
that attempt, compared with Starlark's 92 calls and five cell errors. Observed
JavaScript friction included redeclaring persistent lexical bindings, escaping
nested code, passing an undefined handle, and coercing a host-result object to
a string. Whip's JavaScript decoder creates host-result objects without a
prototype, which explains the observed string-coercion failure. These are
possible contributors to extra iterations, not proof that one error explains
the whole time difference. QuickJS's second HTTPX pass used 143 calls, 903
seconds, and $4.23, versus 229 calls, 1,649 seconds, and $8.46 in its first pass.
The same engine also varied substantially between repetitions on the simple
regex task.

Starlark's second HTTPX pass used 96 calls, 664 seconds, and $2.42. It was
again faster and cheaper, despite having more cell errors than QuickJS in
that pair (13 versus 10). Recorded model-call time explains 235 of the
238-second difference. Error counts alone therefore do not explain efficiency.

The separately controlled [runtime measurements](RUNTIME-MEASUREMENTS.md)
show lower Starlark execution/checkpoint overhead. QuickJS's full-heap durable
checkpoint is much larger and more costly than Starlark's supported-value
checkpoint. Those measurements explain implementation overhead, while task
pass rates also reflect model-generated code, API usability, debugging, and
testing decisions. A faster interpreter need not yield the higher task score.

## Observation amendment and evidence limitations

Attempts 1–3 retain their original evidence in `results/native-limits/`.
Attempts 4–16 use the corrected observer in `results/native-limits-v2/`.
The [original freeze](PREREGISTERED-NATIVE-LIMITS.md) and
[continuation amendment](PREREGISTERED-NATIVE-LIMITS-V2.md) record this division.
There are no replacement attempts, coached retries, or discarded failures.

After the first Starlark HTTPX attempt had completed and passed grading, the
observer tried copying 2,493 content bodies from the wrong directory. The
final database and call ledger had already been exported, but the subsequent
copy failure incorrectly left the final-snapshot flag false. The controller
halted and conservatively reserved all remaining spending authorization.
That placeholder was **not $300 spent** and did not stop the agent's work.

The [accounting reconciliation](results/native-limits/accounting-reconciliation.json)
checks all 92 completed calls against the immutable database, final state,
budget totals, and zero pending or uncertain cost. The verified ledger estimate
is $2.456717. Raw flags and raw reservations remain unchanged. Derived combined
analysis explicitly applies the hash-verified reconciliation for accounting
and finality, while retaining incomplete body-export coverage as a separate
limitation. All 91 execution-cell results were independently resolved; two
required reconstruction that matched retained content identities. The missing
bulk content bodies themselves were not recreated.

The corrected observer reads the actual content-store directory, separates
body-export errors from accounting finality, and records agent duration before
cleanup. Its four free Linux runner/engine smoke tests exercised 12 real content
bodies, and 81 offline tests passed before paid continuation. The binary,
prompt construction, task grading, and execution settings stayed frozen.
Attempts 1–3 retain their earlier duration definition, which includes early
observer cleanup; subsequent attempts exclude post-run copying explicitly.

This is a local Docker adaptation using Harbor 0.22.0 and Pier 0.3.1, with
Linux/amd64 containers on an Apple M4 Max through OrbStack emulation. It is not
an official leaderboard submission. Four selected tasks with two repetitions
are a small descriptive sample; repeated attempts are not independent new
tasks. Cost figures are ledger/catalog estimates, not provider invoices.

## Cost, validation, and retained artifacts

The 18 additional attempts have **$48.724969 in known model cost**, including
the $40.684717 main batch and $8.040252 corrected-runtime pair. The three failed
QuickJS requests remain unpriced in Whip's ledger. The $75 reserve covers
those requests conservatively; it is not measured spending. Including earlier
experiments, known model cost is **$64.215282** and conservative total testing
exposure is **$147.661634**, within the user's $300 authorization. The latter
also retains earlier studies' conservative reserves. No provider invoice was
available, and missing usage was not treated as free.

The numeric-argument and quota-rejection regressions failed before the fix and
passed afterward. Validation also passed the daemon/RLM race suites, Linux
pagination regressions for both engines, 81 offline harness tests, and all four
Harbor/Pier × engine fixture smoke tests. The corrected Linux binary's fresh
rebuild matched byte for byte:
`932da343bd0e14661575745373f8f8b0a5876cbeaeaf17fa076e6f4e05c1b28f`.
The [build record](results/build-numeric-fix/build.json),
[exact source delta](results/build-numeric-fix/runtime-fix.patch), and
[validation artifacts](results/build-numeric-fix/validation/pre-dispatch.json)
preserve that provenance. The original binary and every original result remain
unchanged. Benchmark containers and observer processes have been cleaned up;
unrelated workspace services remain running.

The [analysis/report snapshot manifest](results/native-limits-combined/analysis-snapshot/manifest.json)
freezes the final analysis sources and reports. The
[artifact integrity inventory](results/native-limits-combined/artifact-integrity.json)
hashes retained files and inspected archive members after the final artifact
writes. Its exact-key scan has a declared scope and does not imply that raw
Docker proxy configuration is suitable for public redistribution.
