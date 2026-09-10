# Amended study freeze — 2026-09-10

This amendment precedes every request in `results/formal-v2`. The original
[freeze](PREREGISTERED.md), four attempted trials, raw native runner results,
and [abort audit](results/formal/ABORTED.json) remain available. Twelve scheduled
v1 trials were never dispatched. V1 results are reported separately, including
both verifier failures and both infrastructure failures; none enter v2 scores.

## Why a new version is necessary

Both v1 regex attempts consumed exactly 8,192 output tokens in reasoning and
made zero execution calls. Their verifiers failed because the required file
did not exist. Both v1 HTTPX attempts failed provider discovery before Whip
started: Pier requires the adapter to apply `environment.agent_process_env`
explicitly to receive its authenticated inference proxy. An ordinary `exec`
does not apply that proxy. Neither failure is evidence against an interpreter.

The native Pier reward map also includes test counts and partial scores.
Comparing every map value with one was incorrect; v2 uses the authoritative
`reward` field and preserves the remaining diagnostics. This correction would
not turn either attempted v1 repository trial into a pass: both had reward zero.

## Frozen changes

- Apply Pier's public `agent_process_env` to the observer/Whip process. Keep the
  original task's restricted network and the sole `api.inference.net` allowlist.
  A fresh probe in each original repository image checks provider discovery,
  denial of an unlisted domain, and a paid Kimi call through the real daemon.
  Probes are plumbing checks, not task scores.
- Raise the common per-response maximum output to **32,768 tokens**. Keep
  Kimi K3, high effort, 500,000 tree tokens, 60 root rounds, $2.50 per trial,
  900 seconds of agent execution, four workers, and host concurrency one.
- Use native verifier `reward == 1` for success; diagnostic counts and partial
  scores never determine full credit. Missing reward remains unobserved.
- Freeze the same four tasks, original source revisions and image digests,
  binary, balanced seed-20260910 schedule, and two repetitions: 16 new trials.
  The v2 recipe records the updated harness hashes before its first request.

V2 has a $40 ceiling. Pilot exposure is $1.852592; aborted v1 exposure is
$5.352576, conservatively retaining full reservations for the two discovery
failures. Two connectivity probes have a combined $0.20 ceiling. Thus the
maximum allocated exposure is **$47.405168**, within the assumed $50 total.
Known estimates can release reservations; unknown usage is never set to zero.

## Interpretation

All analysis commitments in the original freeze remain: authoritative verifier
truth, all-attempt denominators, paired outcomes, all-trial and pass-only time
and cost, full tree accounting, explicit missing evidence and no automatic
task retries. The small, selected four-task sample supports descriptive
comparisons. Report 95% percentile intervals by resampling whole task clusters,
preserving both engines and both repetitions; these are unstable with four
clusters and do not overcome selection bias.

This amendment follows inspection of failed v1 attempts and therefore is a
documented tuning decision. V2 is not an untouched holdout. Publish v1 beside
v2 and the off-manifest pilots. Starlark remains default; neither a broad
superiority claim nor an official Frontier score follows from this study.
