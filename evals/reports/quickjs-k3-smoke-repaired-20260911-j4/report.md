# Whip evaluation: quickjs-k3-smoke-repaired-20260911-j4

Status: **partial**. Profile: **smoke**.

Local Frontier adaptation; no published leaderboard ranking.

| Candidate | Verified successes / planned | Graded | Known cost (USD) | Complete cost (USD) | Agent median (s) |
| --- | --- | --- | --- | --- | --- |
| candidate | 5/8 | 6 | 16.110672 | unknown | 85.35362253547646 |

Incomplete runs show confirmed successes against the entire planned denominator, not an exclusion-adjusted score.
All-attempt timing includes failures; use common-success paired latency for speed comparisons.

## Coverage and failure attribution

- candidate: evidence 5/8; accounting 5/8; ungraded 2; termination causes {'user_cancelled': 2, 'none': 6}.
- candidate / datacurve: 0/2 verified successes.
- candidate / terminal-bench: 5/6 verified successes.

## Trial outcomes

| Task | Candidate | Repetition | Grade | Execution | Cause |
| --- | --- | --- | --- | --- | --- |
| terminal-bench/code-from-image | candidate | 1 | error | cancelled | user_cancelled |
| terminal-bench/build-cython-ext | candidate | 1 | passed | completed | None |
| datacurve/httpx-multipart-response-parsing | candidate | 1 | failed | completed | None |
| terminal-bench/openssl-selfsigned-cert | candidate | 1 | passed | completed | None |
| terminal-bench/db-wal-recovery | candidate | 1 | passed | completed | None |
| terminal-bench/regex-log | candidate | 1 | passed | completed | None |
| datacurve/anko-typed-variable-bindings | candidate | 1 | error | cancelled | user_cancelled |
| terminal-bench/kv-store-grpc | candidate | 1 | passed | completed | None |
