# Focused-context runtime fixtures

`go test ./evals/rlm` runs the same semantic fixtures through Starlark and
JavaScript (QuickJS). These are deterministic integration tests using a scripted
HTTP provider and a restricted fixture host. They are not model-proficiency,
whole-daemon, or real-cost benchmarks.

The comparison fixture performs two stateless reviewer calls, searches the corpus,
and reads the exact target fragment. The scripted provider validates the actual
result's engine, text, handle and byte span before returning its final answer.
Incorrect evidence and tool failures cannot produce fixture success. Final answers
must match both the value and exact citation; substring matches do not count.

Reports label scripted usage and prices `synthetic_fixture`. Token totals are
cumulative across root and helper model calls. `peak_call_prompt_tokens` is the
largest reported input count for one call; `peak_call_prompt_tokens_estimate` is
the largest local admission estimate. Neither cumulative input-plus-output nor
these per-call input measures should be labeled peak context occupancy.

Live runs remain explicitly opt-in. `WHIP_RLM_LIVE_SMOKE=1` enables the smoke test;
`WHIP_RLM_LIVE_EVAL=1` enables the comparison. Select their engine with
`WHIP_RLM_EVAL_ENGINE=starlark` (default) or `WHIP_RLM_EVAL_ENGINE=quickjs`.
Existing `WHIP_RLM_EVAL_MODEL`, `WHIP_RLM_EVAL_PROVIDER` and
`WHIP_RLM_EVAL_REPORT` settings remain available. Fixture task instructions are
language-neutral; the selected engine supplies its own tool schema and guide.
