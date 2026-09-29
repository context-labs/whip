# Native agent examples

These programs use `@whip/sdk` v4. `junior-developer.ts` is a data-only definition;
`support-triage.ts` and `incident-commander.ts` add typed tools and hooks. Register
and serve each child first, then pass its exact `{id, revision}` to the parent
factory. Each served definition owns one explicit `executorSocket` peer.

From the repository root:

```sh
npm ci --ignore-scripts
npm run build -w @whip/sdk
npm run check -w @whip/agents-example
npm run pack:web
npm run acceptance -w @whip/agents-example
```

Acceptance starts disposable production runtimes with a local synthetic provider.
It never starts the installed runtime, calls a public provider, or loads account
credentials. Both acceptance files use real native execution, durable operations,
scoped grants, typed results, hook rewrites/denials, and joined executor shutdown.

Definitions declare available tools; they do not grant authority. The tests
explicitly choose Full Access for their disposable roots and delegate exact
custom-tool grants to children. An inherited displayed permission mode never
broadens child authority. Model-call budgets work with unknown model token
bounds; finite token budgets require a known conservative token reservation.

Children preserve required hook contracts and explicitly clear a parent's
structured output with `output: null`. Omitting `output` inherits it. Large
runbooks are returned as bounded JSON and explicitly retained with
`artifacts.put`; `artifacts.read` returns bounded base64 byte slices. There is no
automatic legacy content-handle conversion. State writes use explicit scope and
revision, and mailbox reads use `mail.list`.

Live progress and hook decisions are bounded observations, not an event log.
Observe them while the turn/callback is active. After an executor closes, the
native five-second bind grace can apply to each missing hook; nothing rebinds or
replays automatically. A failed cell is canonical evidence independent of the
provider's later turn outcome.
