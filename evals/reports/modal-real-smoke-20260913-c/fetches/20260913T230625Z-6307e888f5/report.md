# Whip evaluation: modal-real-smoke-20260913-c

Status: **complete**. Profile: **smoke**.

Local Frontier adaptation; no published leaderboard ranking.

| Candidate | Verified successes / planned | Graded | Known cost (USD) | Complete cost (USD) | Agent median (s) |
| --- | --- | --- | --- | --- | --- |
| candidate | 0/8 | 8 | 0.000000 | unknown | 3.345392015 |

Incomplete runs show confirmed successes against the entire planned denominator, not an exclusion-adjusted score.
All-attempt timing includes failures; use common-success paired latency for speed comparisons.

## Coverage and failure attribution

- candidate: evidence 8/8; accounting 0/8; ungraded 0; termination causes {'agent_error': 8}.
- candidate / datacurve: 0/2 verified successes.
- candidate / terminal-bench: 0/6 verified successes.

## Trial outcomes

| Task | Candidate | Repetition | Grade | Execution | Cause |
| --- | --- | --- | --- | --- | --- |
| terminal-bench/code-from-image | candidate | 1 | failed | agent_error | agent_error |
| terminal-bench/build-cython-ext | candidate | 1 | failed | agent_error | agent_error |
| datacurve/httpx-multipart-response-parsing | candidate | 1 | failed | agent_error | agent_error |
| terminal-bench/openssl-selfsigned-cert | candidate | 1 | failed | agent_error | agent_error |
| terminal-bench/db-wal-recovery | candidate | 1 | failed | agent_error | agent_error |
| terminal-bench/regex-log | candidate | 1 | failed | agent_error | agent_error |
| datacurve/anko-typed-variable-bindings | candidate | 1 | failed | agent_error | agent_error |
| terminal-bench/kv-store-grpc | candidate | 1 | failed | agent_error | agent_error |
