# Native task limits follow-up — frozen before paid dispatch

Date: 2026-09-10. This exploratory condition responds to the user's request
to remove experimental limits. It preserves all earlier attempts separately.
It is a local Docker adaptation, not an official leaderboard submission.

Run the same four pinned tasks twice per engine: 16 new attempts. Use the
original seed 20260910, balanced engine order with reversal in repetition two,
fresh isolated environments, and serial execution. The task revisions, images,
instructions, grading, and commit reminder are unchanged from the earlier study.
Do not edit solutions, commit on the agent's behalf, or automatically retry tasks.
Retain every dispatched attempt, including infrastructure failures.

Use the exact archived production binary, SHA-256
`d36c04ce815a30d5a4f659fef4672cd263fd62407f3d896db8e15b6309c8f524`,
for both engines. Kimi K3 runs through inference.net with high reasoning effort;
temperature and top-p remain omitted. Helper/compaction routes also use Kimi K3.
There are no experimental cost, cumulative-token, round, or output-token caps:
`--max-cost 0 --max-tokens 0 --max-turns 0` and model `maxOut: 0`.
Zero output override resolves to the provider-advertised natural maximum;
the fresh catalog advertises 1,048,576 context and completion tokens.

| Task | Agent seconds | Native verifier seconds per attempt |
| --- | ---: | ---: |
| regex-log | 900 | 900 |
| openssl-selfsigned-cert | 900 | 900 |
| anko-typed-variable-bindings | 5400 | 1800 |
| httpx-multipart-response-parsing | 5400 | 1800 |

Read agent deadlines from each upstream task.toml. Preserve the native CPU,
memory, network, collection and grader rules, including Pier's one native
verifier infrastructure retry. Setup, export and cleanup receive separate
slack; none of their watchdogs intentionally shortens the native task window.

RLM configuration selects only the engine. The previous forced host
concurrency of one is removed; normal production defaults apply. Production
runtime safeguards, root-tree structural/storage limits, context compaction,
and the normal ten-minute per-provider-call timeout remain part of the frozen
Whip implementation. They must be reported if they bind. This condition
therefore tests ordinary Whip under native benchmark windows; comparison to
earlier conditions is not a pure one-variable ablation.

The user's existing $300 total testing authorization remains external to the
agent. Prior charged/reserved exposure is $23.936665, leaving $276.063335.
There is no per-trial financial limit. Monitor aggregate exposure during the
run; stop additional spending if the overall authorization would be exhausted,
and label any resulting interruption separately from benchmark/runtime failure.
Unknown uncapped usage reserves the entire remaining authorization for
accounting purposes and halts dispatch until reconciled; this reservation is
not an agent budget, an observed charge, or a provider invoice.

The frozen harness is in `results/build/native-limits-harness/`; its manifest
records hashes. Paid output goes only to `results/native-limits/`. The recipe
freezes ordered task hashes, settings, native timing envelopes, binary hash
and harness hashes before the first request. Free deterministic smoke tests
are recorded separately in `results/native-limits-smoke/`.

Report native reward by task and engine, all-attempt pass counts, paired
outcomes, elapsed agent time, calls, tokens, cost and accounting coverage.
Distinguish actual runtime errors, model mistakes, native deadline stops,
product safeguards, and observation/grading failures. Inspect committed patch
collection for repository tasks. Four distinct tasks provide descriptive
evidence only; repeated attempts are not additional independent tasks.
