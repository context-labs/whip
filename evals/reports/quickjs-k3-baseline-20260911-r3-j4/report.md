# Whip evaluation: quickjs-k3-baseline-20260911-r3-j4

Status: **complete**. Profile: **full**.

Local Frontier adaptation; no published leaderboard ranking.

| Candidate | Verified successes / planned | Graded | Known cost (USD) | Complete cost (USD) | Agent median (s) |
| --- | --- | --- | --- | --- | --- |
| candidate | 19/30 | 30 | 124.581345 | unknown | 251.8018508653622 |

Incomplete runs show confirmed successes against the entire planned denominator, not an exclusion-adjusted score.
All-attempt timing includes failures; use common-success paired latency for speed comparisons.

## Coverage and failure attribution

- candidate: evidence 30/30; accounting 28/30; ungraded 0; termination causes {'none': 27, 'benchmark_deadline': 3}.
- candidate / datacurve: 5/9 verified successes.
- candidate / terminal-bench: 14/21 verified successes.

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
| datacurve/expr-try-catch-errors | candidate | 1 | passed | completed | None |
| terminal-bench/constraints-scheduling | candidate | 1 | passed | completed | None |
| terminal-bench/sqlite-db-truncate | candidate | 1 | passed | completed | None |
| datacurve/arktype-json-schema-refs-dependencies | candidate | 1 | passed | completed | None |
| terminal-bench/vulnerable-secret | candidate | 1 | passed | completed | None |
| terminal-bench/sanitize-git-repo | candidate | 1 | passed | completed | None |
| datacurve/httpx-multipart-response-parsing | candidate | 1 | passed | completed | None |
| terminal-bench/chess-best-move | candidate | 1 | failed | timeout | benchmark_deadline |
| terminal-bench/polyglot-c-py | candidate | 1 | failed | completed | None |
| terminal-bench/regex-log | candidate | 1 | passed | completed | None |
| datacurve/anko-typed-variable-bindings | candidate | 1 | failed | completed | None |
| terminal-bench/modernize-scientific-stack | candidate | 1 | passed | completed | None |
| datacurve/scc-bounded-memory-spilling | candidate | 1 | failed | completed | None |
| terminal-bench/dna-insert | candidate | 1 | passed | completed | None |
| datacurve/katex-multicolumn-array-spans | candidate | 1 | failed | completed | None |
| terminal-bench/kv-store-grpc | candidate | 1 | passed | completed | None |
| terminal-bench/gcode-to-text | candidate | 1 | failed | completed | None |
| terminal-bench/largest-eigenval | candidate | 1 | failed | timeout | benchmark_deadline |
| terminal-bench/openssl-selfsigned-cert | candidate | 1 | passed | completed | None |
| terminal-bench/git-leak-recovery | candidate | 1 | passed | completed | None |
| terminal-bench/merge-diff-arc-agi-task | candidate | 1 | failed | completed | None |
| terminal-bench/extract-elf | candidate | 1 | failed | completed | None |
| datacurve/meriyah-explicit-resource-declarations | candidate | 1 | failed | completed | None |
| terminal-bench/db-wal-recovery | candidate | 1 | passed | completed | None |
| terminal-bench/code-from-image | candidate | 1 | failed | timeout | benchmark_deadline |
| datacurve/python-statemachine-state-data-scoping | candidate | 1 | passed | completed | None |
| datacurve/fastapi-deprecation-response-headers | candidate | 1 | passed | completed | None |
