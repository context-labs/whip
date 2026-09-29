# Focused-context runtime fixtures

`go test ./evals/rlm` runs the same semantic fixtures through Starlark and
JavaScript (QuickJS), using the native runner, provider adapter and SQLite
accounting ledger. The corpus host remains deliberately restricted evaluation
instrumentation. It is not the product's history-oriented `context` service,
a whole-daemon benchmark, model-proficiency evidence or a real-cost measurement.
There are no retired agent, LLM, configuration or RLM orchestration imports.

The comparison performs two stateless reviewer calls, searches the corpus, and
reads the exact target fragment. All four actual HTTP calls use native attempt
admission and settlement; the helpers share the root's model-call budget. The
scripted provider validates the actual engine, exact text, handle and byte span
before returning a final answer. Incorrect evidence, tool failure or exhausted
shared budget cannot produce success. Substring matches do not count. No corpus
body is included in the root prompt.

Reports label scripted usage and prices `synthetic_fixture`. Totals are cumulative
across root and helper calls; the maximum reported single-call input count is
`peak_call_prompt_tokens`. Missing usage/cost increments explicit unknown counters;
known zero remains zero. `peak_call_prompt_tokens_estimate` is now null because
native admission does not provide that retired heuristic. The separately named
`peak_declared_input_token_bound` is host policy, not measured prompt occupancy.
`fixture_context_target_tokens` records the fixture's target; live route bounds
come from its explicit native declaration. None is peak context occupancy.

Live runs are opt-in: `WHIP_RLM_LIVE_SMOKE=1` or `WHIP_RLM_LIVE_EVAL=1`.
They additionally require `WHIP_RLM_EVAL_DIRECTORY` containing an explicitly
prepared native `host.json`. The harness only reads that declaration and uses
fresh disposable ledger/workspace files; it never discovers or starts an installed
runtime. Select `WHIP_RLM_EVAL_ENGINE=starlark` (default) or `quickjs`; optional
`WHIP_RLM_EVAL_MODEL`, `WHIP_RLM_EVAL_PROVIDER` and `WHIP_RLM_EVAL_REPORT` remain.
The restricted harness uses declared API routes and credential sources; managed
subscription/account product flows belong to host acceptance. No live-provider
run is implied by deterministic success.
