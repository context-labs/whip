# Whip evaluation: modal-cancel-20260914-c1

Status: **cancelled** (8 ungraded, 8 evidence-incomplete, 0 provider errors, 1 cancelled). Profile: **smoke**.

Local Frontier adaptation; no published leaderboard ranking.

| Candidate | Verified successes / planned | Graded | Known cost (USD) | Unknown-usage calls | Agent median (s) |
| --- | --- | --- | --- | --- | --- |
| candidate | 0/8 | 0 | 0.000000 | n/a | None |

Incomplete runs show confirmed successes against the entire planned denominator, not an exclusion-adjusted score.
All-attempt timing includes failures; use common-success paired latency for speed comparisons.

## Coverage and failure attribution

- candidate: evidence 0/8; accounting 0/8; ungraded 8; provider errors 0; termination causes {'user_cancelled': 1, 'none': 7}.
- candidate / datacurve: 0/2 verified successes.
- candidate / terminal-bench: 0/6 verified successes.

## Trial outcomes

| Task | Candidate | Repetition | Grade | Execution | Cause |
| --- | --- | --- | --- | --- | --- |
| terminal-bench/code-from-image | candidate | 1 | error | cancelled | user_cancelled |
| terminal-bench/build-cython-ext | candidate | 1 | missing | not_started | None |
| datacurve/httpx-multipart-response-parsing | candidate | 1 | missing | not_started | None |
| terminal-bench/openssl-selfsigned-cert | candidate | 1 | missing | not_started | None |
| terminal-bench/db-wal-recovery | candidate | 1 | missing | not_started | None |
| terminal-bench/regex-log | candidate | 1 | missing | not_started | None |
| datacurve/anko-typed-variable-bindings | candidate | 1 | missing | not_started | None |
| terminal-bench/kv-store-grpc | candidate | 1 | missing | not_started | None |
