# Benchmark results and methodology

## Verified WhipCode result

The retained [modal-full-20260917-starlark-b report](../evals/reports/modal-full-20260917-starlark-b/fetches/20260917T163952Z-2ed26b2998/report.md) records **20 successes out of 30 planned tasks (66.7%)**, one attempt per task: 17/21 Terminal-Bench tasks and 3/9 code-repository tasks. All 30 were graded. This is a historical evaluation, not a fresh measurement of the current Desktop release.

WhipCode used the local Frontier adaptation on Modal with the Inference.net Kimi K3 Fast route. The published reference results used Kimi K3 through Fireworks on Runta. These are different environments, not a controlled paired comparison. See the [evaluation guide](../evals/README.md) and [reference metadata](../evals/frontier/references.json).

## Supplied comparison graphic

The README graphic was supplied by the maintainer. Its WhipCode point reads 66.7% at $2.60; the pass rate agrees with the retained report, but the $2.60 figure has not been traced to a reproducible accounting definition or source run.

The report records $132.945764 in known aggregate cost, with accounting complete for 29/30 trials and one unknown-usage call. Neither known aggregate cost nor a median over only fully accounted trials establishes the chart’s figure. Missing usage must not be treated as zero.

The graphic labels the horizontal axis “Median cost per task.” The [pinned upstream README](https://github.com/frontier-harness-eval/eval/blob/e837a70bd6beb4e72eeeda62dd06e3bd34f6cb63/README.md) labels the corresponding competitor figures “Median cost per pass,” also discusses effective cost per pass, and describes cache normalization. Those definitions must be reconciled with the underlying data before making relative-cost claims.

Accordingly, the graphic is a supplied comparison, **not evidence of a matched cost advantage, statistical superiority, or an official WhipCode leaderboard rank**. Do not infer general performance from this small task set.

Before presenting the cost frontier as a verified result, retain the plotting source and run identifiers, establish a common cost definition and pricing/cache treatment, account for missing usage, and label the model, provider, environment, task count, and attempts. Preserve failed trials in the denominator. The original asset’s values and axes have not been silently changed.
