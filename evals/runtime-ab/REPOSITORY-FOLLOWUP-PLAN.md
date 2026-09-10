# Exploratory repository follow-up

Decision recorded while v2 continues, after its first repository attempts.
The fixed v2 study still runs all 16 scheduled attempts with its original
harness and budgets. Its results remain primary and are never replaced.

The first QuickJS attempts on HTTPX and Anko failed after cumulative token
admission stopped further model calls, at estimated costs of $0.713994 and
$0.419384. Starlark's first Anko attempt also stopped on token accounting;
the first Starlark HTTPX attempt was interrupted by an optional telemetry
probe timeout. This motivates more token headroom for **both** repository
tasks and **both** engines, without selecting a task based on which engine won.

After v2 finishes, run one new paired repetition of each repository task:
four attempts, all results retained. Keep the same frozen Whip binary, original
tasks/images, Kimi K3 route, high default reasoning effort, 32,768 output limit,
60 root rounds, four workers, host concurrency one, and 900-second agent limit.
Use the same **$2.50 cost admission budget** with no additional cumulative
token cap (`--max-tokens 0`). Report this separately as exploratory evidence;
it answers a different budget question and must not be pooled into v2 scores.

Before dispatch, harden only the observer adapter: an optional intermediate
telemetry timeout must record a missed sample and preserve the last known
metrics, while the actual observer and runner deadlines remain authoritative.
Do not swallow observer errors, cancellation, malformed final evidence, or
missing usage. Bound final evidence/daemon cleanup separately. Regression-test
these paths and freeze the changed harness before any follow-up request.

The four attempts have a combined $10 ceiling. Recheck all prior observed
cost/reserved exposure before dispatch and retain the overall $50 ceiling.
Model admission uses input estimates; report actual provider usage and any
overage that causes accounting to pause. Prices are catalog-based estimates,
not provider-reported bills. Unknown calls retain their full trial reservation.

This decision follows observed failures and is not a new untouched holdout.
The final report must show the pilots, aborted v1, fixed v2, and exploratory
follow-up separately, with exact recipes and every verifier/infrastructure
outcome available.
