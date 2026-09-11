# Loupe on the Whip TypeScript SDK

Companion to `.ai-docs/research/loupe-on-whip-sdk.md`. This plan changes the
Loupe repository. Whip needs no code for it; the one Whip item (publishing the
SDK to npm) is deferred by decision.

## Why

Loupe enforces its review contract with prose: the JSON output shape, the
"panel of models", read-only behavior, and bounded diff access are all
sentences in a system prompt handed to `whip run -system`. Two facts from the
code make the move worth doing now rather than polishing the prose:

1. **The `-system` override replaces Whip's whole composed prompt.**
   `refreshPrompt` in `internal/daemon/prompt.go` treats a root override as
   complete and verbatim. That includes the runtime guide, the part that tells
   the model which host modules exist and how to call them. Loupe's agentic
   reviews therefore run with a model that was never shown the module catalog.
   The definition route (`instructions.persona` and `rules`) keeps the guide
   and puts Loupe's guidance beside it, which is the only correct way to add
   instructions to a Whip agent.
2. **Every prompt-enforced rule has a daemon-enforced equivalent now.** A
   `submit_review` tool validates the output shape before Loupe sees it and
   can reject a bad line while the model can still fix it. Named children fix
   the panel's models and budgets. Capabilities and `before_tool` make
   read-only a property of the session, not a request.

## Decisions

Settled with Sam on 2026-09-11:

- **Whip only.** The `claude` and `codex` adapters and the generic
  `runCli` go. Core can assume structured tool results.
- **GitHub Actions per run, as today.** Definitions are registered by each
  run; the Loupe process is the executor for the duration of the review. No
  long-lived service.
- **Vendor the SDK now, publish later.** `@whip/sdk` and `@whip/protocol`
  built `dist` trees are committed under `vendor/` as Bun workspace packages.
- **Fix mode stays as it is.** The fixer agent edits files; Loupe commits and
  pushes.
- **No cost or token caps yet.**

Recommended and applied below unless Sam objects (see "Open" at the end):

- **Orchestration split.** The confirmation panel moves into the reviewer as
  named children. Verification and ensemble stay in Loupe as deterministic
  pipeline stages.
- **Keep the checkout, drop shell.** Reviewers get `files` read operations on
  the `actions/checkout` tree plus the `pr_*` tools. No `shell` module, no
  GitHub-API-backed file reads.
- **Keep `WHIP_HOME` materialization.** Loupe starts the daemon with a
  throwaway home containing the provider and panel config. Provider creation
  over the wire exists (`provider.create`) but adds choreography for no gain
  in CI.
- **Unix socket transport.** `@whip/sdk/node` already exports
  `unixSocket(path)`; `whip daemon status --json` reports `socket`. No
  network listener, no origin or host allow-lists.

## Target shape

```
packages/
├─ credentials/  unchanged
├─ logger/       unchanged
├─ whip/         NEW: daemon lifecycle, SDK client, reviewer/verifier/fixer
│                definitions, tools, hooks; replaces harness/
├─ core/         review pipeline; consumes structured reviews from whip/
└─ action/       CLI + Action entry; loses the harness input
vendor/
├─ @whip/protocol  built dist, refreshed by `task vendor:whip`
└─ @whip/sdk       built dist (exports ., ./node, ./agents)
```

`@loupe/whip` exposes:

- `ensureDaemon(opts) → { socket, stop? }`: `whip daemon status --json`; if
  stopped, `whip daemon start` with `WHIP_HOME` set to the materialized
  throwaway home (CI) or the user's real home (local, `whipConfig` absent).
  Stops only a daemon it started.
- `connect(socket) → WhipClient` via `createWhipClient({ endpoint:
  unixSocket(socket), clientId, clientKind: 'automation' })`.
- `reviewerAgent(spec) → AgentDefinition`, `verifierAgent()`,
  `fixerAgent()`, `chatAgent()`.
- `runAgent({ client, agent, cwd, model?, cacheKey?, maxTurns?, prompt,
  onEvent }) → { text, tool results }`: `agents.serve`, `sessions.create({
  cwd, definition, model })`, `setPermissionMode(false)`,
  `configure({ max_turns, headless: true, cache_key })`, subscribe events,
  `submit({ text }).result()`, `executor.close()`.

## What must keep working

`--dry-run`; incremental review by SHA marker; noise profiles as a hard
filter; ensemble with majority clustering; the verification pass and its
fail-open; skills; conventions; path instructions; `--dir` scoping with
subdir-relative convention paths; `@loupe review | fix | <question> | help`
with visible failure comments; the agentic → one-shot fallback; the
`whip { provider, models }` block in `.loupe.json`; `maxTurns`; prompt caching
per repo and reviewer; `LOG_LEVEL=debug` visibility of what the model did.

## Steps

Each step lands green on `task check` and is independently shippable.

### 1. Vendor the SDK

- Build `packages/protocol` and `packages/sdk` in the whip checkout; copy
  `dist` and `package.json` (with `private` removed and `dependencies`
  pointing at `workspace:*`) into `vendor/@whip/{protocol,sdk}`.
- Add `vendor/*` to the root `workspaces`; `bun install`; commit the lockfile.
- `Taskfile.yml`: `vendor:whip WHIP=../whip` that repeats the copy.
- Record the whip release tag the dist came from in `vendor/@whip/VERSION`
  and pin the action's "Install whip" step to that tag instead of the latest
  release. The SDK refuses an incompatible daemon; the pin makes that a
  non-event.
- Smoke test: a Bun script imports `@whip/sdk/node`, connects to a local
  daemon socket, calls `client.agents.list()`.

### 2. `@loupe/whip` with a behavior-identical runner

- New package with `ensureDaemon`, `connect`, `runAgent`, and a
  `reviewerAgent` whose persona is today's full `buildSystemPrompt` output and
  whose modules are `['context', 'files', 'agents']` (agentic) or `['context']`
  (one-shot), capabilities `['read']`, model from the reviewer. No tools yet.
- Map events to today's logs: `stream.reasoning` → throttled `thinking`,
  `stream.text` → `streaming reply`, `stream.cell.host.started` and
  `stream.tool.started` → `tool call`, `stream.usage` / `stream.accounting`
  → a new per-run cost line, `turn.failed` → error.
- `core/runReview` calls `runAgent` instead of `harness.review`; the returned
  text still goes through `parseReviewOutput`. Verify, chat, and fix use
  `verifierAgent`, `chatAgent`, `fixerAgent` with today's prompts as persona.
- Delete `packages/harness`, `runCli`, `runWhipStreaming`, the `-cache-key`
  retry, and the `harness` input/flag/env/config field. Keep
  `materializeWhipHome` (moves into `@loupe/whip`).
- Test: `whip-config.test.ts` moves; add a unit test for the event → log
  mapping with recorded events; dry-run a real PR and diff the rendered
  review against a run from `main`.

### 3. `submit_review` replaces JSON scraping

- Tool `submit_review` with the review schema (`summary`, `concerns`,
  `highlights`, `diagram`, `findings[] { path, line, severity, body }`).
  Handler validates each finding against `commentableLines(files)`; on a miss
  it returns an error naming the nearest commentable lines in that file; on
  success it stores the review and returns `{ accepted: true }`.
- Persona's output contract becomes "call `tools.submit_review` exactly once
  when done"; the model's final text is ignored for reviews.
- `submit_verdicts` does the same for the verification pass.
- Core: `runReview` reads the stored review; `parseReviewOutput`,
  `parseVerification`, `extractLastJsonObject`, and `jsonrepair` are removed.
  `validateFindings` stays only to split inline from dropped for files whose
  patch is missing (binary or too large).
- Severity alias normalization moves into the tool schema's `enum` plus a
  handler-side map, so the model gets an error for an unknown value instead
  of a silent `warning`.
- Test: handler unit tests for accept, snap, and reject; the acceptance dry
  run again.

### 4. Capabilities and hooks

- Reviewer: `capabilities: ['read']`; `before_tool` with `operations:
  ['files.write', 'files.patch']` denying with "reviewers are read-only"
  (belt and braces; the capability already denies). `before_spawn` denies a
  child whose resolved capabilities are wider than the parent's.
- Fixer: `modules: ['context', 'files', 'shell']`, `capabilities: ['read',
  'write', 'shell']`; `before_tool` on `shell.run` denies `git commit`, `git
  push`, `git reset --hard`, `rm -rf`, and any path outside the checkout.
- Log `stream.hook.decision` events at info.
- Test: hook handlers are pure functions; unit-test the deny table.

### 5. `pr_*` tools replace the diff temp file

- `pr_files()` → the changed-file tree with additions and deletions per file.
- `pr_hunks(path)` → that file's unified hunks (large results become
  handles automatically).
- `pr_context(path, line, radius)` → surrounding lines from the checkout.
- The agentic user prompt drops the diff path and the grep instructions; it
  says which tools to use. `writeDiffFile` is deleted. The one-shot prompt
  still inlines the diff.
- Test: tool handlers against fixture diffs; confirm a run never spawns a
  shell (no `stream.cell.host` for `shell.*`).

### 6. Panel as named children

- Children `confirm-<n>` built from `whip.models` minus the reviewer's own
  model (or an explicit per-reviewer `panel: [...]` in `.loupe.json`), each
  with `model`, `tools: ['pr_hunks', 'pr_context']`, `report: 'inline'`, and
  a token budget.
- Persona's panel paragraph becomes: "before reporting a blocker, spawn two
  confirmers with `agents.spawn(definition="confirm-1", …)` and keep it only
  if they agree". `before_spawn` caps the fan-out per turn.
- Test: an acceptance run shows `agent.admitted` events with the child
  definitions; the unit test asserts the children map from a config.

### 7. Project files and skill discovery

- `instructions.project_files` = the convention paths; `skill_discovery:
  true`. With a checkout, drop the API fetch and `loadSkills` for paths under
  `.agents/skills`; keep the API fetch for the one-shot no-checkout path and
  `loadSkills` for arbitrary skill paths.
- Test: dry run with `--dir inference` shows `inference/AGENTS.md` in the
  prompt snapshot (`session.snapshot()` exposes prompt sources).

### 8. Docs and cleanup

`README.md`, `docs/architecture.md`, `docs/configuration.md`,
`docs/credentials.md`, `docs/github-action.md`, `action.yml`,
`examples/review.example.yml` lose the harness choice and describe the daemon
step, the pinned whip version, and the tools. `.loupe.json` schema drops
`harness` with a clear error if present.

## Risks

- **Bun and `@whip/sdk/node`.** `unixSocket` uses `node:net`; Bun supports
  it, and Bun ignores the SDK's `engines` field. Step 1's smoke test proves it
  before anything depends on it.
- **Executor lifetime.** Tools and hooks run in the Loupe process; a crash
  mid-review fails the pending hook (deny) and tool call (error to the
  model). Same failure mode as a subprocess dying today, now visible in the
  session stream.
- **Parallel reviewers.** The Action runs reviewers with `Promise.all`; one
  client serves several definitions concurrently, which the executor
  registry supports (one lease per definition revision).
- **Definition ids.** `loupe-<name>` must match `^[a-z][a-z0-9-]{1,63}$`;
  reviewer names are validated to fit.

## Open

Confirm or redirect the four recommendations above (orchestration split,
keep checkout and drop shell, keep materialization, unix socket). Steps 2
through 8 assume them.
