# Whip evaluation: quickjs-k3-full-qualified-20260911-j4

Status: **partial**. Profile: **full**.

Local Frontier adaptation; no published leaderboard ranking.

| Candidate | Verified successes / planned | Graded | Known cost (USD) | Complete cost (USD) | Agent median (s) |
| --- | --- | --- | --- | --- | --- |
| candidate | 7/30 | 10 | 20.766658 | unknown | 104.09116392256692 |

Incomplete runs show confirmed successes against the entire planned denominator, not an exclusion-adjusted score.
All-attempt timing includes failures; use common-success paired latency for speed comparisons.

## Coverage and failure attribution

- candidate: evidence 10/30; accounting 9/30; ungraded 20; termination causes {'none': 9, 'user_cancelled': 20, 'agent_error': 1}.
- candidate / datacurve: 0/9 verified successes.
- candidate / terminal-bench: 7/21 verified successes.

## Published context

Different provider/environment; no matched comparison or ranking.

- Codex 0.148.0: 66.7% on 30 tasks, one attempt each.
- Claude Code 2.1.237: 63.3% on 30 tasks, one attempt each.
- Pi 0.84.2: 60.0% on 30 tasks, one attempt each.

Source: [pinned Frontier results](https://github.com/frontier-harness-eval/eval/blob/e837a70bd6beb4e72eeeda62dd06e3bd34f6cb63/README.md).

## Trial outcomes

| Task | Candidate | Repetition | Grade | Execution | Cause |
| --- | --- | --- | --- | --- | --- |
| terminal-bench/log-summary-date-ranges | candidate | 1 | passed | completed | None |
| terminal-bench/multi-source-data-merger | candidate | 1 | passed | completed | None |
| terminal-bench/build-cython-ext | candidate | 1 | passed | completed | None |
| datacurve/expr-try-catch-errors | candidate | 1 | error | cancelled | user_cancelled |
| terminal-bench/constraints-scheduling | candidate | 1 | passed | completed | None |
| terminal-bench/sqlite-db-truncate | candidate | 1 | passed | completed | None |
| datacurve/arktype-json-schema-refs-dependencies | candidate | 1 | error | cancelled | user_cancelled |
| terminal-bench/vulnerable-secret | candidate | 1 | passed | completed | None |
| terminal-bench/sanitize-git-repo | candidate | 1 | failed | completed | None |
| datacurve/httpx-multipart-response-parsing | candidate | 1 | failed | agent_error | agent_error |
| terminal-bench/chess-best-move | candidate | 1 | passed | completed | None |
| terminal-bench/polyglot-c-py | candidate | 1 | failed | completed | None |
| terminal-bench/regex-log | candidate | 1 | error | cancelled | user_cancelled |
| datacurve/anko-typed-variable-bindings | candidate | 1 | error | cancelled | user_cancelled |
| terminal-bench/modernize-scientific-stack | candidate | 1 | missing | cancelled | user_cancelled |
| datacurve/scc-bounded-memory-spilling | candidate | 1 | missing | cancelled | user_cancelled |
| terminal-bench/dna-insert | candidate | 1 | missing | cancelled | user_cancelled |
| datacurve/katex-multicolumn-array-spans | candidate | 1 | missing | cancelled | user_cancelled |
| terminal-bench/kv-store-grpc | candidate | 1 | missing | cancelled | user_cancelled |
| terminal-bench/gcode-to-text | candidate | 1 | missing | cancelled | user_cancelled |
| terminal-bench/largest-eigenval | candidate | 1 | missing | cancelled | user_cancelled |
| terminal-bench/openssl-selfsigned-cert | candidate | 1 | missing | cancelled | user_cancelled |
| terminal-bench/git-leak-recovery | candidate | 1 | missing | cancelled | user_cancelled |
| terminal-bench/merge-diff-arc-agi-task | candidate | 1 | missing | cancelled | user_cancelled |
| terminal-bench/extract-elf | candidate | 1 | missing | cancelled | user_cancelled |
| datacurve/meriyah-explicit-resource-declarations | candidate | 1 | missing | cancelled | user_cancelled |
| terminal-bench/db-wal-recovery | candidate | 1 | missing | cancelled | user_cancelled |
| terminal-bench/code-from-image | candidate | 1 | missing | cancelled | user_cancelled |
| datacurve/python-statemachine-state-data-scoping | candidate | 1 | missing | cancelled | user_cancelled |
| datacurve/fastapi-deprecation-response-headers | candidate | 1 | missing | cancelled | user_cancelled |
