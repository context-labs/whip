# Native-limits Anko follow-up after numeric host-argument correction

Recorded 2026-09-10 before dispatch. This is an exploratory follow-up after
observing all 16 native-limits outcomes: Starlark 6/8, QuickJS 7/8. Preserve
all 16 attempts and their scores. No result is replaced or regraded.

The final audit found a real integration defect. QuickJS's exact frame decoder
keeps numbers as `json.Number`, but host integer controls accepted only
`float64`. Content read offset/length therefore fell back to defaults. In
Anko repetition 2, execution cell 35 repeatedly read the first 8,192 bytes,
reaching 1,024 host calls. A second defect checked for a stalled VM before
draining the local quota-rejection Promise. The worker was discarded, then
the agent resumed with shell-based reads and eventually submitted a patch.
Its native failure was a typed-array-initialization defect in that patch;
the runtime incident's indirect effect on the trajectory is unknown.

The corrected product accepts exact integer host arguments for offsets,
lengths, counts, waits, budgets, and history validation without converting
wide values through floating point. It drains local quota rejections before
testing for a stalled guest. The normal quota stays in place. Deterministic
regressions failed on the prior code and pass after these changes.

Run exactly one fresh Anko attempt per engine, using the original task input
and native verifier. The selected task is where the runtime incident was
observed; this is not a new holdout sample. Do not feed hidden test findings
to the model or repair prior submitted patches. Both engines use the same
new binary, built from the original source archive plus exactly three
production-file corrections and three test-file changes. The source delta
is recorded in `results/build-numeric-fix/`, along with a byte-matching rebuild.

Binary SHA-256:
`932da343bd0e14661575745373f8f8b0a5876cbeaeaf17fa076e6f4e05c1b28f`.
Freeze the harness in `results/build-numeric-fix/harness/` and write results
to `results/native-numeric-fix/`. Generate the original seeded full schedule
(seed 20260910, one repetition), then retain only
`anko-typed-variable-bindings`. This produces **QuickJS followed by Starlark**,
serially, with no task retries.

Execution settings match the native-limits condition: Kimi K3 on inference.net,
high ordinary-turn effort, provider-default sampling, no experimental cost,
cumulative-token, round, or output override, production RLM defaults, native
5,400-second agent deadline and native verifier configuration. Normal product
execution bounds remain. The only harness change is the named-task filter,
applied after generating the seeded schedule.

Prior-inclusive testing exposure is **$64.621382**, including the native
batch's $40.684717 settled ledger estimate and the earlier studies' conservative
exposure. The user's total authorization remains $300. No per-trial dollar
limit is passed to Whip. Free fixture runs do not count as model benchmarks.

Before paid dispatch, require the deterministic regressions, Linux pagination
regressions for both engines, the broader daemon/RLM race suite, the 81 offline
harness tests, and all four native runner/engine fixture smoke tests to pass.
Report this pair separately from the original 16, including every outcome,
cost, final accounting, native grading, and runtime/evidence incident.
