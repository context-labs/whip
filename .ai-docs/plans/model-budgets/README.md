# Unlimited default model budgets

Research and implementation plan — 2026-09-07. Status: implemented; validation results below.

## Outcome and decisions

New root sessions should run without a cumulative model cost, token, or elapsed
time ceiling. Children inherit that policy unless an agent explicitly narrows
their allowance. Keep execution capacity and storage limits: unlimited model
usage does not mean unlimited simultaneous work or retained data.

Confirmed by the user:

- Do not expose budget controls to the user.
- Do not migrate, repair, or maintain compatibility with existing sessions.
- Root sessions should be effectively unlimited; subagents should have a high
  or unlimited default.

Defaults accepted with the instruction to execute this plan:

- Children default to unlimited model usage, with optional finite caps through
  the existing `agents.spawn(..., budgets=...)` interface.
- Preserve configured/provider response-length limits. Do not introduce a
  blanket 64k output ceiling to work around the lifetime-budget problem.

These assumptions are separable: removing optional model caps would simplify
the finite-limit cases below; choosing a smaller default response ceiling would
change model configuration, not cumulative accounting.

## What the investigation established

The reported error was:

```text
cost budget needs 23883863, remaining 23857772
(limit 25000000, used 1142228, reserved 0)
```

Cost values are micro-USD. WHIP was requesting a **$23.883863 reservation** from
a **$25 lifetime allowance**, with **$1.142228 already recorded**. This was an
admission failure, not evidence that the conversation had already cost $25.

Read-only inspection of the affected session and its cached provider catalog
found `kimi-k3-fast` advertising 1,048,576 maximum completion tokens and an
output price of $0.0000225/token. The possible output alone accounts for
$23.59296. The remaining estimate includes prompt/tool-schema tokens. These are
the locally cached values used by this session, not a claim about current public
model specifications or invoiced spend.

The root's three children had no separate budget rows. They inherited the same
root cap, so their reservations and usage competed for that $25 as well.

Relevant implementation findings:

| Area | Behavior before this change | Consequence |
| --- | --- | --- |
| [Root defaults](../../../internal/session/budget.go) | $25, 100 million tokens, four hours of cumulative elapsed usage | Ordinary long-running roots eventually stop; expensive reservations stop them much sooner. |
| [Child admission](../../../internal/session/swarm.go) | Omitted budgets inherit; explicit budgets add subtree caps | No need to invent a second default budget mechanism for children. |
| [Model reservation](../../../internal/daemon/budget.go) | Combined estimated tokens are priced at the highest root-model rate; 30 minutes is reserved per call | Input is overestimated at output prices; elapsed is a reservation, not an enforced request deadline. |
| [Agent calls](../../../internal/agent/agent.go) | Prompt estimate plus maximum output; character-based estimates are approximate | Estimates can also be exceeded. Current settlement rejects actual usage above the reservation. |
| [Model construction](../../../cmd/whip/daemon.go) | Root pricing is shared with children and compaction; route output resolution differs | Alternate models can use the wrong prices or inherited output limits. |
| [LLM transport](../../../internal/llm/openai.go) | Absent usage collapses to zero; retry/error paths can discard received usage | Accounting cannot distinguish known zero, unknown usage, and billable failed attempts. |
| [Settlement/recovery](../../../internal/session/runtime.go) | Missing actual usage and orphan reservations become consumed budget | A maximum reservation can be presented as actual usage after an interrupted call. |
| [Web controls](../../../packages/app/src/details/session-controls.tsx) | Existing budget editing form calls `budget.cap` | Remove this form; adding another budget control is unnecessary. |

External protocol research supports treating missing usage explicitly:
[OpenAI's usage documentation](https://help.openai.com/en/articles/10478918)
explains that an interrupted stream may omit its final usage chunk; that does
not establish zero usage. Its
[Chat Completions reference](https://developers.openai.com/api/reference/resources/chat/subresources/completions/methods/create)
also distinguishes the per-completion output ceiling, including reasoning
tokens, from any application-level cumulative budget. WHIP's compatible
providers still require fixture tests for their actual response shapes.

## Budget policy and representation

| Kind | New root default | Child default |
| --- | --- | --- |
| `cost`, `tokens`, `elapsed` | Unlimited | Inherit, with no automatic local cap |
| `active_children` | 8 per tree | Existing inherited limit |
| `concurrent_child_turns` | 4 per tree | Existing inherited limit |
| `active_operations` | 64 | Existing inherited limit |
| `depth` | 2 | Existing inherited limit |
| `durable_bytes` | 1 GiB | Existing inherited limit |
| `record_count` | 100,000 | Existing inherited limit |
| `schedules_subscriptions` | 1,000 | Existing inherited limit |

Use an explicit unlimited value, not `math.MaxInt64`, negative numbers, or a
missing root budget row:

- Make `budgets.limit_value` nullable. `NULL` means unlimited and is allowed
  only for the three cumulative model-usage kinds.
- Expose `BudgetState.Limit` and `Remaining` as nullable integer pointers with
  the existing JSON string encoding. Finite values remain decimal strings;
  unlimited is `null`. The existing
  [schema generator](../../../internal/protocol/schema.go) already handles
  nullable string-encoded integer pointers.
- Keep root rows for accounting. Missing required rows, invalid kinds,
  negative counters, and malformed persisted values remain errors.
- Keep explicit requested limits as nonnegative integers: zero means zero;
  omission means inheritance. Reject fractional/out-of-range spawn values.
- Enforce every finite ancestor allowance atomically. Unlimited ancestors
  continue accumulating usage but do not participate in remaining-limit checks.
- Inspect the most restrictive finite ancestor when one exists. Otherwise
  return unlimited. Do not add root and child totals together: they overlap.
- Keep checked integer arithmetic independently of limit enforcement.

New forks receive the new root defaults and their own usage accounting. Reopen
and crash recovery of sessions created under the new schema remain supported.
Change the clean schema and its version/identity using the repository's existing
incompatible-schema policy. Do not add migration code or automatically erase a
user database. Test and develop with isolated fresh runtime homes.

## Model calls and accounting

Keep the existing store/daemon/agent/LLM boundaries. Add no dependency, billing
service, pricing lookup during each request, or unbounded per-call ledger.

### Model identity and estimates

Resolve an immutable model configuration for each route: provider, API model
ID, context window, maximum output, and known/unknown input/output/cache prices.
Use it for ordinary root calls, inherited and overridden child calls,
compaction, final-answer calls, and `models.call`/`models.batch` helpers.

The call reservation must receive separate prompt and output estimates and the
actual route's prices. Estimate prompt at the uncached input rate (or the higher
cache rate if advertised), and possible output at the output rate. Continue to
include tool schemas and image estimates. Preserve price presence so an
explicit zero rate is distinct from absent pricing; an absent cache discount
can fall back to the known input rate.

Keep output configuration resolution consistent: explicit `max_out`, then the
selected model's advertised completion limit, then the existing documented
fallback when no limit is available. Never inherit a different model's output
ceiling after an override. Keep compaction's deliberate small output limit.
Changing context management, provider parameter compatibility, or normal
response-length policy is outside this fix.

When a finite cost cap applies and usable pricing is unavailable, reject with a
specific explanation rather than treating the request as free. Unlimited calls
can proceed with cost marked incomplete. Provider-reported tokens remain useful
even when price data is missing.

### Known usage, unknown usage, and settlement

Carry usage presence and request outcome through the LLM client, including
error paths. Use a small request-lifecycle hook so each transport retry is
admitted and settled; a retry must not bypass a finite cap. Keep this hook
independent of daemon/session types. Preserve known usage from failed attempts
and settle each attempt once; a successful retry must not overwrite prior
attempt accounting. A locally rejected request can release its reservation;
a network error alone is not proof that no provider work occurred.

Use bounded aggregate uncertainty in the existing budget rows:

- `used` records known usage; cost calculated from catalog rates is still an
  estimate of charges, not a provider invoice.
- `reserved` records admitted work currently in flight.
- Add an `uncertain` amount and an incomplete flag for model-usage kinds.
  Missing usage transfers the applicable reservation into uncertainty, never
  into known spend. Missing prices mark cost incomplete even if there is no
  numeric cost estimate to reserve.
- Finite allowance checks include `used + reserved + uncertain`; unlimited
  rows only account. Uncertainty is conservative exposure, explicitly labelled
  as such. It does not block default sessions.
- Known zero settles to zero. Known positive usage releases the full estimate
  and records the actual amount. If actual usage exceeds the estimate, record
  it and release capacity; do not discard the response or leave reservations
  stuck. A finite cap can then be exhausted and deny subsequent admission.
  Clamp displayed finite remaining allowance at zero, while preserving the
  actual overage. Approximate token counts cannot guarantee an invoice cap.
- Keep live-operation release exactly once on success, error, cancellation,
  and shutdown. Preserve the existing settlement path outside the root actor
  to avoid its documented shutdown deadlock.
- On restart, transfer orphan model token/cost/elapsed reservations to
  uncertainty. Release/reconstruct live capacity as today. Preserve the
  existing durable-operation reconciliation; generic storage reservations must
  not silently acquire model-specific settlement semantics.

This requires updating reservation SQL, snapshot and collection reads, and
schema constraints together. Repeated recovery must not add usage or
uncertainty twice. Do not create an automatic clearing/replenishment policy for
explicit finite caps or claim provider billing reconciliation.

## User and agent surfaces

Remove the web inspector's budget agent/kind/limit fields and Set cap action.
Remove the TUI `/agents budget` command, completion/help entries, and its
budget-specific argument parsing. Add no setting, slider, session-creation
field, or unlimited toggle.

Keep read-only usage and resource information within the current inspector;
show unlimited without giant numeric limits, format USD and durations, and
distinguish known usage, in-flight reservations, and incomplete estimates.
Do not add a new dashboard or global banner. Keep bounded collection fetching
and the state ownership described in [the frontend guide](../../../docs/frontend.md).

Retain optional finite `agents.spawn` caps and their existing subtree authority
rules under the recommended assumption. Clarify in
[the agent prompt](../../../internal/rlm/prompt.go) that budgets should normally
be omitted, omitted children inherit unlimited model usage, and explicit caps
are for intentionally constrained tasks. Keep the existing low-level
`budget.cap` contract as an internal command; removing its product controls does
not require an unrelated API deletion. No public configuration for root caps
is added. Root defaults cannot become finite merely through opening a client.

## Implementation sequence

1. **Store policy and finite-limit semantics.** Update
   `internal/session/{budget,swarm,migrations,runtime,event,collection_page}.go`.
   Add explicit unlimited defaults, nullable state, uncertainty, safe ancestor
   arithmetic, and recovery. Update only tests whose old-default assumptions
   change; finite cap and resource tests should remain substantive.
2. **Request accounting and route resolution.** Update
   `internal/{llm/openai,agent/agent,daemon/budget,daemon/agent_session,daemon/recursive_runtime,config/catalog}.go`
   and `cmd/whip/daemon.go`. Carry route prices and usage presence through every
   call path, account for retries, and reconcile outcomes without phantom spend.
   Keep provider limits and existing execution deadlines separate.
3. **Contracts and clients.** Regenerate protocol schemas, TypeScript types,
   and validators from Go. Update snapshots, SDK fixtures, web/TUI inspection,
   and remove budget editing affordances. Do not hand-edit generated artifacts.
4. **Documentation and verification.** Update `docs/frontend.md`,
   `docs/features.md`, `docs/architecture.md`, `docs/concurrency.md`, and relevant
   command/agent documentation to state the new defaults and internal-only
   caps. Replace stale claims that budget caps can be raised through `budget.cap`.
   Complete the regression checks below before calling the work done.

Steps 1–3 form one coherent change: do not ship nullable wire values to clients
that still assume numeric limits. Refresh dirty shared files before editing;
preserve concurrent theme, composer, and split-view work.

## Acceptance and validation

Use fake providers, deterministic usage, fake durations, and temporary runtime
homes. Do not spend provider money or wait hours to validate unlimited budgets.

- Reproduce the screenshot's numbers and million-token advertised maximum in
  a real daemon fixture. Multiple root turns and three simultaneous child
  attempts succeed without cumulative model budget denials.
- Accumulate beyond all former defaults ($25, 100 million tokens, four hours)
  and verify new roots/uncapped children continue. Reopen and fork under the new
  schema. Check defaults across web/CLI/ACP entry points through shared bootstrap.
- Verify finite child caps, finite ancestor rollup, grandchild inheritance,
  sibling isolation, explicit zero, malformed limits, missing rows, and atomic
  denial/release under concurrent admission. Resource limits remain unchanged.
- Verify different root/child/compaction/helper models use their own prices
  and output limits; include aliases, cached input, known free prices, absent
  pricing, tools, and images. Test integer/float overflow and invalid rates.
- Exercise streamed/non-streamed success, known zero, absent usage, received
  usage followed by error, cancellation, local failure before dispatch,
  ambiguous network failure, retries, actual usage above estimate, settlement
  failure, shutdown, and repeated crash recovery. No false known spend, lost
  known usage, duplicate settlement, or leaked active-operation slots.
- Validate nullable finite/unlimited values and uncertainty through Go JSON,
  generated validators, SDK snapshot/replay and collection pagination, web,
  and TUI. Read-only usage remains available; budget mutation controls are absent.
- Verify normal explicit subagent caps still yield understandable errors with
  units and the binding scope. No recommendation to change a nonexistent UI
  budget setting, and no automatic shortening of model responses.

Run affected Go packages first, including `internal/session`, `internal/daemon`,
`internal/agent`, `internal/llm`, `internal/config`, `internal/protocol`,
`internal/tui`, and `cmd/whip`; use `-race` for reservation, retry, shutdown, and
recovery scenarios. Run `npm run generate`, `npm run check`,
`npm run check:web`, and `npm run test:web`, plus the affected real-browser
inspector scenario against an isolated daemon. Then run the repository's
`task check`, `task lint`, and `task acceptance` gates; `task ci` is required
before a push under [CONTRIBUTING.md](../../../CONTRIBUTING.md).

## Implementation results

The store now creates explicit unlimited model limits and retains known usage,
reservations, and uncertainty separately. Finite child caps and resource limits
remain enforced. Reopening validates existing accounting instead of recreating
missing budget rows. Forks initialize their own unlimited defaults. Old schemas
are rejected without alteration; the current clean schema is version 8.

Every provider attempt, including retries, uses an immutable snapshot of its
route's input/output/cache prices. Ordinary turns, child overrides, compaction,
stateless helpers, final-answer fallbacks, and title generation share that path.
HTTP failures can retain reported usage; absent usage, known zero, cancellation,
and local failures remain distinguishable. Settlement records estimate overages
and is atomic and retryable if storage fails. Recovery handles remaining orphan
reservations without double counting.

The generated wire contract is major 4, minor 0. Nullable limits and new
uncertainty fields are deliberately incompatible with older clients; existing
`/api/v3/` transport paths are unchanged. Web/TUI budget editing was removed;
read-only usage formats exact currency, durations, unlimited values, and
unconfirmed estimates. No dependency was added.

Validation on 2026-09-07:

- `task check`: passed, including format, vet, whipvet, full Go tests, contract
  generation drift, SDK checks, frontend checks, UI checks, and production builds.
- `task acceptance`: passed, including race-enabled daemon/recursive runtime,
  cancellation/recovery/authority scenarios, 24 SDK acceptance tests, and package
  smoke tests.
- `npm run generate`, `npm run check`, `npm run check:web`, and
  `npm run test:web`: passed (182 SDK unit tests and 155 frontend tests in the
  recorded standalone runs).
- Focused `-race` regressions passed for unlimited model usage, explicit caps,
  concurrent calls, known/unknown usage, HTTP retries, cancellation, model route
  pricing, overflow, settlement storage failure, repeated recovery, and forks.
  The full affected-package race run passed apart from a provider-catalog fixture
  timeout; that fixture and the protocol-compatibility scenario passed on their
  isolated rerun. The later full Go check and release acceptance also passed.
- `node apps/web/scripts/model-budgets.mjs`: passed against an isolated daemon
  with production assets, using Chromium and Firefox in light/dark appearance at
  1280 px and 390 px. Checked nullable SDK snapshots, unchanged capacity limits,
  read-only usage, absence of cap controls/mutations, reload, and browser errors.
  Screenshots are under `/tmp/whip-model-budget-browser`; desktop dark and phone
  light captures were visually reviewed.
- `task lint`, using CI-pinned golangci-lint v2.13.1 built with Go 1.27: reports
  81 findings outside the budget changes. Budget-related findings were fixed;
  unrelated shared-checkout work was preserved. This gate is not green.

The running user daemon and existing databases were not restarted, migrated, or
deleted. Running this implementation requires the updated clients/daemon and a
fresh runtime home. `task ci` was not run because no push was requested and lint
remains blocked by the unrelated findings above.
