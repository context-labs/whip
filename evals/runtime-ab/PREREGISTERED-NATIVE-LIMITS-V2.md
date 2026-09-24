# Native-limits collector correction and continuation

Date: 2026-09-10, recorded before dispatching attempt 4. Continue the exact
16-attempt schedule in [the native-limits freeze](PREREGISTERED-NATIVE-LIMITS.md)
at zero-based index 3. Preserve attempts 1–3 in `results/native-limits/`.
The remaining 13 attempts go in `results/native-limits-v2/`, retaining their
original attempt numbers. No agent/task retries or replacement attempts.

At this amendment, both regex attempts passed. Starlark's HTTPX attempt also
passed: 122/122 feature tests and 1272/1272 existing tests, with a 34,280-byte
committed patch after 92 model calls. Its post-completion content export failed.
The observer incorrectly read `WHIP_HOME/artifacts/sha256`; `session.Open`
constructs the content store beside the database, at
`WHIP_HOME/runtime-v2/artifacts/sha256`. All 2,493 referenced body copies failed
with FileNotFoundError. The deleted native container cannot supply these bodies
afterward; any reconstruction must independently verify retained identities.

The old observer also set its final-snapshot flag only after copying content,
so it reported incomplete accounting even though the database backup and call
ledger were complete. The controller conservatively reserved all remaining
authorization and halted. This was not $300 of actual spending and did not
interrupt Whip or grading.

`results/native-limits/accounting-reconciliation.json` preserves raw-file
hashes and verifies all 92 succeeded calls against the immutable database,
root budget totals, zero pending/reserved/uncertain cost, final event cursor,
succeeded root turn, verified frozen daemon, and matching metrics. Settled
ledger cost is $2.456717; independent normalized catalog cost differs only by
per-call microdollar rounding. Provider invoice data is unavailable. The raw
outcome and controller records remain unchanged. Body-export coverage remains
incomplete and must be disclosed independently of reconciled call accounting.

Changes affect observation only:

- Read immutable content bodies from the actual runtime-v2 content directory.
- Preserve the verified accounting snapshot when later body copying fails;
  report content-copy completeness and errors separately.
- Record agent duration before post-run cleanup/content copying; keep total
  observer duration as a separate raw field. Earlier attempts retain their
  original timing semantics and are identified in analysis.
- Allow a separately frozen controller to start at the next original schedule
  index. The Whip binary, prompts, model settings, zero experimental caps,
  runtime configuration, benchmark clocks, native grading and order are unchanged.

Free smoke fixtures now force large content into the real runtime store, so
the collector test has nonempty bodies. Run all four runner/engine combinations
and offline regressions before paid continuation. Archive the corrected harness
in `results/build/native-limits-v2-harness/`.

Prior testing exposure for continuation is $27.042690: $23.936665 from earlier
studies plus $3.106025 for these first three native-limits attempts. The external
total authorization remains $300; Whip receives no per-trial cost cap. The next
attempt is QuickJS on HTTPX, repetition 1. The completed Starlark HTTPX pass is
retained in the paired comparison with its collector-version caveat.
