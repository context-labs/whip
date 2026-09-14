# Whip evaluation: quickjs-k3-smoke-20260911-j1

Status: **partial**. Profile: **smoke**.

Local Frontier adaptation; no published leaderboard ranking.

| Candidate | Verified successes / planned | Graded | Known cost (USD) | Complete cost (USD) | Agent median (s) |
| --- | --- | --- | --- | --- | --- |
| candidate | 2/8 | 2 | 2.507466 | unknown | 392.63401514291763 |

Incomplete runs show confirmed successes against the entire planned denominator, not an exclusion-adjusted score.
All-attempt timing includes failures; use common-success paired latency for speed comparisons.

## Coverage and failure attribution

- candidate: evidence 3/8; accounting 3/8; ungraded 6; termination causes {'none': 2, 'verifier_error': 1, 'user_cancelled': 5}.
- candidate / datacurve: 0/2 verified successes.
- candidate / terminal-bench: 2/6 verified successes.

## Trial outcomes

| Task | Candidate | Repetition | Grade | Execution | Cause |
| --- | --- | --- | --- | --- | --- |
| terminal-bench/code-from-image | candidate | 1 | passed | completed | None |
| terminal-bench/build-cython-ext | candidate | 1 | passed | completed | None |
| datacurve/httpx-multipart-response-parsing | candidate | 1 | error | runner_error | verifier_error |
| terminal-bench/openssl-selfsigned-cert | candidate | 1 | error | completed | user_cancelled |
| terminal-bench/db-wal-recovery | candidate | 1 | missing | cancelled | user_cancelled |
| terminal-bench/regex-log | candidate | 1 | missing | cancelled | user_cancelled |
| datacurve/anko-typed-variable-bindings | candidate | 1 | missing | cancelled | user_cancelled |
| terminal-bench/kv-store-grpc | candidate | 1 | missing | cancelled | user_cancelled |
