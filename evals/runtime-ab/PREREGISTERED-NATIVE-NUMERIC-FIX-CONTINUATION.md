# Accounting continuation for the corrected Anko pair

Recorded 2026-09-10 at 18:47 UTC, while the first QuickJS attempt is still
running and before either corrected-pair grade is known. This changes only
the external accounting policy for dispatching the already scheduled second
attempt. The task inputs, binary, native deadlines, runtime settings, and
original QuickJS-then-Starlark order remain as preregistered.

One dispatched model call failed with HTTP 502 and no usage in Whip:
`23d8e141df5f1b412a8ed7572a98d67d`, at 18:41:12–13 UTC. Whip retried normally.
Its ledger conservatively retains $20.859667 of uncertain cost. The original
controller will consequently preserve an all-remaining-authorization
placeholder and halt after recording the first result. Preserve this behavior
and all raw files; do not change the running agent, replay the failed request,
or present its zero local cost field as a settled charge.

The authenticated inference.net observability record
`b051c156-e049-4b42-b5eb-87b90baaee8c` confirms upstream HTTP 502 and records
`totalCost: 0`. Its stored request has the exact last tool-result bytes from
local event 1054 (`rlm_exec_31`): SHA-256
`c3815076a3b072737a4639e6e2a5eec90b823b6baa9d321f2e688f1413bf7bb5`.
Times, model, maximum output, and the subsequent successful retry also match.
The sanitized evidence is retained in
`results/native-numeric-fix/provider-failure-evidence.json`. A billing lookup
by that inference ID returned no records; this is not an invoice or proof of
zero final billing. Raw Whip usage remains incomplete.

After the first attempt's final evidence is exported, audit all calls, final
state, process cleanup, and the sole unknown call. If this remains the only
unknown call, carry **$25** as an explicit conservative reserve in addition to
the known model cost. At the frozen maximum input/output sizes of 1,048,576
tokens each and uncached rates $0.00000395/input token and
$0.0000198/output token, a single maximum-sized request costs $24.90368.
The $25 reserve exceeds both that bound and the local uncertain reservation.
Do not describe the reserve as measured spending or completed accounting.

Continue only the original second scheduled Starlark attempt in a separate
`results/native-numeric-fix-v2/` directory, using the same frozen harness with
`--start-index 1`. Set external prior exposure to $64.621382 plus the first
attempt's final known cost plus $25. Retain the user's original $300 total
authorization. No per-trial dollar, cumulative-token, round, or output cap
is passed to Whip. If additional unknown calls or active operations remain,
reassess the evidence before dispatch rather than assuming this bound covers
them. Report the two attempts together with explicit split-directory
provenance and incomplete accounting for the first.

## Additional failure observed at 19:02 UTC, before grading

A second request, `ba95467e857a1604ab5c2a696aaa8143`, ran from 18:54:52
until 19:00:56 UTC (364.571 seconds) and failed with HTTP 520. Whip's retained
notice records the error and a retry after 1.060 seconds; subsequent calls
succeeded and execution resumed. The scoped provider observability query for
18:54:51–54 returned no completed record even after the local failure. Do not
infer a zero charge or a specific upstream cause from that absence.

The final-state audit must now verify exactly these **two** unknown calls,
no pending work, the same frozen prices and token maxima for each, and no
additional accounting exceptions. Increase the external conservative reserve
to **$50 ($25 per unknown call)**, in addition to known ledger cost. The local
uncertain reservations total $41.911651. The Starlark continuation's prior
exposure becomes $64.621382 plus the final QuickJS known cost plus $50.
This amendment does not alter the running task or any benchmark limit.

## Final provider timeout, recorded 19:16 UTC

The third failed request, `ef4955b48ce60e30255e4a87110fd8d0`, ran from
19:04:51 until 19:14:51 UTC and hit Whip's normal 600-second provider-call
timeout. The root turn failed with `context deadline exceeded`; preserve this
outcome and its native grade. This is a product/provider-request timeout, not
the native 5,400-second task deadline or a REPL cell timeout. The scoped
provider query returned no completed record at lookup time.

The accounting audit must verify exactly the three retained failed-call IDs
and carry **$75 ($25 per unknown call)** plus known ledger cost. Local uncertain
reservations total $63.014322. The original Starlark continuation remains
unchanged in task input, settings, binary, order, and production provider
timeout. No failed result is replaced; report this failed provider attempt
separately from solution correctness and runtime execution errors.
