# Whip evaluation: modal-real-smoke-20260913-d

Status: **complete**. Profile: **smoke**.

Local Frontier adaptation; no published leaderboard ranking.

| Candidate | Verified successes / planned | Graded | Known cost (USD) | Complete cost (USD) | Agent median (s) |
| --- | --- | --- | --- | --- | --- |
| candidate | 6/8 | 8 | 1.732259 | unknown | 250.9325666135 |

Incomplete runs show confirmed successes against the entire planned denominator, not an exclusion-adjusted score.
All-attempt timing includes failures; use common-success paired latency for speed comparisons.

## Coverage and failure attribution

- candidate: evidence 8/8; accounting 6/8; ungraded 0; termination causes {'none': 6, 'agent_error': 2}.
- candidate / datacurve: 0/2 verified successes.
- candidate / terminal-bench: 6/6 verified successes.

## Trial outcomes

| Task | Candidate | Repetition | Grade | Execution | Cause |
| --- | --- | --- | --- | --- | --- |
| terminal-bench/code-from-image | candidate | 1 | passed | completed | None |
| terminal-bench/build-cython-ext | candidate | 1 | passed | completed | None |
| datacurve/httpx-multipart-response-parsing | candidate | 1 | failed | agent_error | agent_error |
| terminal-bench/openssl-selfsigned-cert | candidate | 1 | passed | completed | None |
| terminal-bench/db-wal-recovery | candidate | 1 | passed | completed | None |
| terminal-bench/regex-log | candidate | 1 | passed | completed | None |
| datacurve/anko-typed-variable-bindings | candidate | 1 | failed | agent_error | agent_error |
| terminal-bench/kv-store-grpc | candidate | 1 | passed | completed | None |
