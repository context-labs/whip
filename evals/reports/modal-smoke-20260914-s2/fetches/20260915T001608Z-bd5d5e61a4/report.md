# Whip evaluation: modal-smoke-20260914-s2

Status: **complete** (0 ungraded, 0 evidence-incomplete, 0 provider errors, 0 cancelled). Profile: **smoke**.

Local Frontier adaptation; no published leaderboard ranking.

| Candidate | Verified successes / planned | Graded | Known cost (USD) | Unknown-usage calls | Agent median (s) |
| --- | --- | --- | --- | --- | --- |
| candidate | 7/8 | 8 | 17.970639 | 2 | 139.53663393750003 |

Incomplete runs show confirmed successes against the entire planned denominator, not an exclusion-adjusted score.
All-attempt timing includes failures; use common-success paired latency for speed comparisons.

## Coverage and failure attribution

- candidate: evidence 8/8; accounting 6/8; ungraded 0; provider errors 0; termination causes {'none': 8}.
- candidate / datacurve: 1/2 verified successes.
- candidate / terminal-bench: 6/6 verified successes.

## Trial outcomes

| Task | Candidate | Repetition | Grade | Execution | Cause |
| --- | --- | --- | --- | --- | --- |
| terminal-bench/code-from-image | candidate | 1 | passed | completed | None |
| terminal-bench/build-cython-ext | candidate | 1 | passed | completed | None |
| datacurve/httpx-multipart-response-parsing | candidate | 1 | passed | completed | None |
| terminal-bench/openssl-selfsigned-cert | candidate | 1 | passed | completed | None |
| terminal-bench/db-wal-recovery | candidate | 1 | passed | completed | None |
| terminal-bench/regex-log | candidate | 1 | passed | completed | None |
| datacurve/anko-typed-variable-bindings | candidate | 1 | failed | completed | None |
| terminal-bench/kv-store-grpc | candidate | 1 | passed | completed | None |
