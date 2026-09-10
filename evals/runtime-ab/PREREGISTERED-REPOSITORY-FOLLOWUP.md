# Exploratory repository follow-up freeze

Recorded 2026-09-10 before any follow-up requests, while fixed v2 completes.
This implements the earlier [follow-up decision](REPOSITORY-FOLLOWUP-PLAN.md).
Primary outcomes and their original harness remain unchanged.

## Question and schedule

Can the agent finish and commit the original repository repairs within the
same dollar and time limits when cumulative tree tokens are not capped?
Run exactly four new attempts, serially after all 16 v2 attempts:

1. HTTPX — Starlark.
2. HTTPX — QuickJS.
3. Anko — QuickJS.
4. Anko — Starlark.

This is the original seed-20260910, first-repetition full schedule filtered
to repository tasks, retaining its paired engine order. Keep both original
task trees, grader/base commits, Docker images and restricted network policy.
No post-agent patch repair or commit is permitted. Native `reward == 1`
determines a pass. Retain every attempt and account for missing verification.

Keep inference.net `kimi-k3`, high default turn effort, 32,768 per-call output
maximum, 60 root rounds, four workers, host concurrency one, $2.50 cost
admission per trial and 900 seconds of agent execution. Set `--max-tokens 0`,
which leaves the new root's cumulative token budget unlimited; cost admission
and all other limits remain in force. Record actual usage, output-cap
reductions by admission, and any model-requested overrides.

## Frozen implementation

Whip Linux/amd64 SHA-256 remains
`d36c04ce815a30d5a4f659fef4672cd263fd62407f3d896db8e15b6309c8f524`.
No runtime, language guide, tool policy, model loop, or task source change is
part of this follow-up. Harness source SHA-256:

| File | SHA-256 |
| --- | --- |
| `observe.py` | `63cde08e72cd9d17c55949d141dccea97cd3a97caa0792ea4aaf5334faaf3a10` |
| `study.py` | `89d87b8dd7d4ec4b5898c8350ebcf68daf0bc6e56f5919ec2bc8e6308dbad7eb` |
| `whip_adapter.py` | `7777ba5c7553c41436fa4f2a9ca22bbeeeb71b3bedeef5c897e62eaa1f9f1dd1` |

The adapter tolerates only optional metrics-probe timeouts and records missed
samples/staleness separately. Observer failure, cancellation, malformed
evidence, and incomplete final accounting remain explicit. Final evidence and
daemon cleanup have a 240-second shared deadline. The observer command has
945 seconds; the runner agent envelope has 1,245 seconds including cleanup
and guard time. Agent planning still ends at 900 seconds.

The 8,566-second outer watchdog includes the original environment build,
setup, patch collection, native verification, and transfer/teardown budgets.
Pier's 1,800-second verifier timer includes separate environment startup and
retains its native single infrastructure retry. Automatic **task** retries
are zero; Whip/provider transient retry behavior is unchanged. An outer
watchdog permits bounded signal waits and cleanup of only independently
verified trial-owned container IDs, then halts further dispatch for review.

The candidate passed 38 offline tests before freezing, including 18 new
telemetry, malformed evidence, cancellation, cleanup, schedule, and watchdog
regressions. Source, patch, tests, runner timing evidence, and exact recipes
will be archived alongside the results before execution.

## Spending and interpretation

The follow-up has a combined $10 ceiling inside the existing assumed $50
overall ceiling. At this freeze, prior pilot/v1/probe exposure is $7.239349;
13 completed primary attempts account for $7.392230. Reserving $7.50 for all
three remaining primary attempts and $10 for the follow-up yields a worst
allocated total of **$32.131579**. Recheck the actual ledger before dispatch.
Unknown usage retains full reservations; estimates are not provider bills.

These four trials are exploratory and follow observed primary failures.
Report them separately, with all-attempt outcomes, complete-evidence cost and
time, submitted patch sizes, and native verifier diagnostics. Do not pool
them with v2 or replace v2 failures. Two tasks with one paired repetition
provide very limited evidence; no default switch or population superiority
claim follows from this sample.
