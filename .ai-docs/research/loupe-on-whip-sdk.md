# Loupe on the Whip TypeScript SDK

Research only. No Whip or Loupe code changes. Source: a fresh clone of
`context-labs/loupe` at `8fd1cc7` and this branch's SDK (`@whip/sdk` 6.4,
`@whip/sdk/agents`).

## Why

Loupe drives Whip as a subprocess: `whip run --format json -system <prompt>
-m <model> -max-turns N -cache-key <key>` with the user prompt on stdin, then
parses the NDJSON event stream and scrapes the last `{...}` out of the reply.
Everything Loupe cares about in review mode (structured output, a panel of
models, no writes, bounded exploration) is enforced by prompt text alone.

The SDK primitives this branch added turn each of those hopes into a
mechanism the daemon enforces:

| Loupe today (prompt-enforced) | Whip primitive that enforces it |
|---|---|
| "Respond with ONE JSON object" + `jsonrepair` + per-finding `safeParse` | a `submit_review` custom tool whose JSON Schema the daemon validates before the handler runs |
| "line must be a changed or context line" + post-hoc snapping | the same tool validates `path:line` against the diff and tells the model the nearest commentable lines |
| "Use SUBAGENTS heavily… a mix of glm-5.3-flash, glm-5.2-fast, deepseek" | named children with per-child `model` defaults and budgets |
| "Do NOT call tools" (headless) / "Do NOT run git" (fix) | definition `capabilities` plus a `before_tool` hook |
| temp diff file + "grep/sed by the `### <path>` header" | `pr_files` / `pr_hunks` / `pr_context` custom tools with bounded results |
| `fetchConventions` over the API, `loadSkills` from disk | `instructions.project_files` and `skill_discovery` on the definition |
| NDJSON parsing of `whip run` output | typed `stream.*`, `agent.turn.*`, `stream.usage` / `stream.accounting` events |
| `-cache-key` retry hack for old binaries | `session.configure({ cache_key })` over the wire, feature-detected by `client.supports` |

## What Loupe is (facts from the clone)

Bun workspace, five packages. Dependency direction: `action → core →
(harness, logger)`, `credentials` and `logger` are leaves.

- `packages/harness/src/index.ts`: `Harness = { name, credentialKeys,
  available(), review(ctx) → Promise<string> }`. Adapters for `claude`
  (`-p --append-system-prompt`), `codex` (`exec -`), and `whip`
  (`runWhipStreaming`). `materializeWhipHome(cfg)` writes a throwaway
  `WHIP_HOME/config.json` declaring the provider (`apiKeyEnv`) and model panel.
- `packages/core/src/index.ts` `runReview()`: fetch PR + conventions
  (Octokit) → scope by subdir and globs → incremental delta from SHA markers →
  build the three-layer system prompt → run the harness (agentic first, one-shot
  fallback on failure) → `parseReviewOutput` → `validateFindings` (snap within
  10 lines) → verification pass (second headless call, fail-open) or ensemble
  (sequential runs per model, majority clusters) → `postReview` (delete prior
  inline comments, `createReview` with empty body, upsert one summary comment
  per reviewer). `REQUEST_CHANGES` on any blocker, never `APPROVE`.
- `packages/core/src/prompt.ts`: system prompt = guidance ∥ skills ∥
  conventions ∥ reasoning note ∥ profile directive ∥ headless or agentic
  directive ∥ output contract. User prompt = environment line, PR title and
  body, path instructions, and either the inline diff (headless) or a file tree
  plus a pointer to the diff temp file (agentic).
- `packages/action/src/respond.ts`: `@loupe help | review | fix <what> |
  <question>`. Fix runs the harness agentically in the checkout, then Loupe
  itself does `git add/commit/push`. Chat answers are prose from a headless run.
- `.loupe.json`: top-level review defaults (`harness`, `model`, `reasoning`,
  `profile`, `timezone`, `dir`, `maxTurns`, `whip { provider, models }`) and
  `reviewers[]` (`name`, `prompt|promptFile`, `include/exclude`, `model`,
  `reasoning`, `profile`, `agentic`, `verify`, `ensemble`, `skills`,
  `pathInstructions`, `maxTurns`).
- CI: composite `action.yml`; the workflow downloads `whip-linux-x64` from
  the latest release, loads `INFERENCE_API_KEY` from Infisical, runs
  `bun run packages/action/src/main.ts`. Fork PRs are gated out.

Everything Loupe uses from `whip run` already exists over the wire for SDK
clients: `run.configure { system, max_turns, headless, cache_key }`,
`session.create { cwd, definition, model, provider, permission_mode }`,
`permission.mode`, `budget.cap { kind: cost | tokens }`, and the event stream.
`Sessions.create(...).result()` returns the root id; `session.submit({ text
}).result()` resolves with the turn's final text.

## Three shapes, from lazy to complete

### A. SDK as the transport, same contract

Replace `runWhipStreaming` with an SDK-backed `whipHarness`: connect, create a
session on the `coding` definition (or none), `configure({ system, max_turns,
headless, cache_key })`, `setPermissionMode(false)`, `submit(userPrompt)`, log
`stream.*` events, return the final text. `Harness.review` still returns a
string; core is untouched.

Gains: typed events and per-run accounting, no NDJSON parsing, no
`-cache-key` retry, no reliance on the reply text for `done`. Costs: Loupe must
start the daemon itself in CI (the SDK is attach-only), and the SDK's only
exported transport is `webSocket(endpoint)`, so either the daemon's loopback
listener is enabled (`WHIP_NETWORK=1`, `WHIP_LISTEN=127.0.0.1:<port>`, read
`network_endpoint` from `whip daemon status --json`) or the SDK grows a unix
socket transport for Node. Roughly a day of work; it is the foundation for B.

### B. Reviewer = agent definition (recommended target)

Each `.loupe.json` reviewer becomes a `defineAgent` document built at run time
and served by the Loupe process for the duration of the review.

```ts
const reviewer = defineAgent({
  id: `loupe-${reviewer.name}`,            // ^[a-z][a-z0-9-]{1,63}$
  instructions: {
    persona: reviewer.guidance ?? DEFAULT_REVIEW_GUIDANCE,
    rules: [REASONING_NOTE[effort], PROFILE_DIRECTIVE[profile], TOOL_DIRECTIVE, conventions].join('\n\n'),
    project_files: ['CLAUDE.md', 'AGENTS.md', '.loupe.md', 'CONTRIBUTING.md'],
    skill_discovery: true,
  },
  modules: agentic ? ['context', 'files', 'agents'] : ['context'],
  capabilities: ['read'],
  model: { model: reviewer.model, effort },
  tools: [prFiles, prHunks, prContext, submitReview],
  children: {
    'confirm-a': { model: { model: 'glm-5.3-flash' }, tools: ['pr_hunks', 'pr_context'], budgets: { tokens: 200_000 }, report: 'inline' },
    'confirm-b': { model: { model: 'glm-5.2-fast' }, /* … */ },
    'confirm-c': { model: { model: 'deepseek-v4-pro-0813' }, /* … */ },
  },
  hooks: {
    beforeTool: { operations: ['shell.run', 'files.write'], handler: denyWritesAndGit },
    beforeSpawn: ({ resolved }) => resolved.capabilities?.some(c => c !== 'read') ? { decision: 'deny', reason: 'reviewers are read-only' } : undefined,
  },
});
const executor = await client.agents.serve(reviewer);
const created = await client.sessions.create({ cwd: harnessCwd, definition: executor.definition }).result();
```

What moves where:

- **Output contract → `submit_review` tool.** The tool's schema is the
  `reviewOutputSchema` with `findings[].line` validated against
  `commentableLines(files)` inside the handler. A bad line returns an error the
  model reads ("`src/x.ts:42` is not in the diff; nearest commentable lines are
  40 and 45"), so the retry happens in the model's turn instead of Loupe
  snapping after the fact. The handler stores the validated review; the turn's
  final text becomes irrelevant. `parseReviewOutput` and `jsonrepair` stay only
  for the `claude` and `codex` harnesses.
- **Diff access → tools.** `pr_files()` returns the changed-file tree,
  `pr_hunks(path)` the hunks for one file, `pr_context(path, line, radius)`
  surrounding lines from the checkout or the GitHub contents API. Large results
  already become context handles the cell reads in slices. The temp diff file
  and the "grep by `### <path>`" instructions go away. With a contents-API
  backend, agentic review no longer needs a checkout or a `shell` module at all,
  which removes today's warned fallback.
- **Panel of models → named children.** `agents.spawn(definition="confirm-a",
  prompt=…)` fixes the model, tools, budget, and report mode structurally;
  `before_spawn` caps fan-out and rejects anything that widens capabilities.
  Children inherit the parent's hooks and custom tools (a `tools` list on the
  child narrows them).
- **Conventions and skills → the definition.** `project_files` reads the
  authorized project chain from `cwd`, broad to specific, which is exactly
  Loupe's `--dir` behavior (`AGENTS.md` then `inference/AGENTS.md`). Keep the
  API fetch for the headless no-checkout path. Whether `skill_discovery`
  matches Loupe's `.agents/skills/<name>` layout needs verifying.
- **Verification and ensemble stay in Loupe.** They are deterministic control
  flow around model calls. The verifier is a second definition
  (`loupe-verifier`, one tool `submit_verdicts`); ensemble is N sessions on the
  same reviewer definition created with a `model` override
  (`CreateSessionParams.model`), merged by the existing `mergeEnsemble`.
- **Fix mode → `loupe-fixer` definition** with `capabilities: ['read',
  'write', 'shell']` and a `before_tool` hook denying `git commit`, `git push`,
  and anything outside the checkout. Loupe keeps doing the commit and push.
- **Prompt caching.** Definition instructions are the stable prefix; per-PR
  content stays in the user prompt; `cache_key` stays per repo and reviewer.

Core changes this implies: `Harness.review` returns `{ text } | { review:
ReviewOutput }` so the structured path and the parse path coexist; `runReview`
skips parse and snap when it gets a structured review.

### C. Loupe as a long-lived executor service

Persistent daemon, definitions registered once, GitHub webhooks create
sessions, Loupe stays bound as the executor. Only worth it if Loupe leaves
GitHub Actions. Nothing in B blocks it; skip until there is a reason.

## What Loupe must keep working through any of this

Harness-agnostic `claude` and `codex` adapters; `--dry-run`; incremental review
by SHA marker; noise profiles as a hard filter; ensemble; the verification
pass and its fail-open; skills; conventions; path instructions; `--dir`
scoping; `@loupe review | fix | <question> | help`; the agentic → one-shot
fallback; `WHIP_HOME` materialization of the provider and panel; the
`-max-turns` cap (now `run.configure.max_turns` or a child budget).

## Whip-side gaps this surfaces (not built)

- A unix socket transport for the Node SDK, or documented loopback listener
  setup for CI. The `Transport` type already has `kind: 'unix'`; only
  `webSocket` is exported.
- `@whip/sdk` and `@whip/protocol` are not published; Loupe is a public repo
  and installs from npm (`bun install --frozen-lockfile` in the action).
- Semantics of `run.configure.system` on a definition-backed session: full
  system replacement or persona layer only. This decides whether conventions
  go in `instructions.rules` or in `system`.
- Whether `skill_discovery` finds `.agents/skills/<name>/SKILL.md`.
- Version pinning: Loupe's workflow installs the latest whip release; the SDK
  refuses an incompatible daemon (`incompatible` connection state), so the
  action must pin a compatible whip version alongside the SDK version.

## Recommended order

1. A: SDK transport behind the existing `whipHarness`; daemon startup in the
   action; keep everything else.
2. `submit_review` tool on a minimal definition; core accepts a structured
   review. This alone removes the parse and snap heuristics for whip runs.
3. Capabilities and `before_tool` for review and fix modes.
4. `pr_*` tools; drop the diff temp file.
5. Panel children and `before_spawn`.
6. `project_files` and `skill_discovery` replacing the API fetch and
   `loadSkills` when a checkout exists.

## Open questions for Sam

1. Does Loupe stay harness-agnostic (keep `claude` and `codex`), or is it
   becoming Whip-native? This decides whether the structured `submit_review`
   path can be the only path in core.
2. GitHub Actions only, or a long-lived Loupe service later? Decides B vs C
   and whether definitions are registered per run or persist.
3. Where should orchestration live: keep verify and ensemble in Loupe as
   deterministic TypeScript, or push confirmation into children? My
   recommendation is panel in children, verify and ensemble in Loupe.
4. Should agentic review keep requiring a checkout, or should `pr_context` be
   backed by the GitHub contents API so a reviewer runs with no `files` or
   `shell` module at all? That is the strongest sandbox and removes a CI step.
5. Keep `WHIP_HOME` materialization (Loupe starts the daemon with that home)
   or configure the provider over the wire? Materialization is simpler and the
   daemon needs the API key in its environment either way.
6. Publish `@whip/sdk` to npm publicly, or vendor it into Loupe?
7. Daemon transport in CI: enable the loopback listener, or add a unix
   transport to the SDK?
8. `@loupe fix`: keep Loupe doing commit and push (recommended), or let the
   fixer definition hold `git` under a hook?
9. `whip run` supports `-max-cost` and `-max-tokens`; Loupe does not use them.
   Want per-reviewer cost caps? Cheap to add as `budget.cap`.
