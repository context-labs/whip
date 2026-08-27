# whip roadmap

UX niceties worth adopting, learned from [pi](file:///home/abe/code/pi) and
[opencode](file:///home/abe/code/coding-harnesses/opencode). Check things off as they land.
Full exploration reports: [learnings/other-harnesses/opencode/](learnings/other-harnesses/opencode/),
[learnings/other-harnesses/exo.md](learnings/other-harnesses/exo.md) (durable state, self-modification, scheduler/adapters).

**Reference docs:** [features.md](features.md) (what's shipped, where it lives,
its tests) and [concurrency.md](concurrency.md) (the channel patterns behind
parallel tool calls and background subagents).

## Table of contents

- [Input & editing](#input--editing)
- [Transcript & rendering](#transcript--rendering)
- [Sessions](#sessions)
- [Agent loop](#agent-loop)
- [Skills & subagents](#skills--subagents)
- [Models & providers](#models--providers)
- [MCP](#mcp)
- [LSP](#lsp)
- [Safety & permissions](#safety--permissions)
- [Theming & config](#theming--config)
- [CLI surface](#cli-surface)
- [Autonomy & durability](#autonomy--durability) (exo)

## Input & editing

- [x] Queue messages while busy (enter, codex-style multiple), force-steer queue into the running turn (empty enter, grok-style), auto-send queued as follow-up turns
- [x] Explicit interruption: double ctrl+c while busy (cf. opencode's triple-escape with 5s reset — `packages/tui/src/routes/session/index.tsx:1388`)
- [ ] Queue management: edit/remove queued messages before they send (opencode `<leader>q`, `runtime.queue.ts`)
- [x] Multiline input (grow textarea; opencode binds newline to `shift+enter,ctrl+enter,alt+enter,ctrl+j` because terminals disagree — `keybind.ts:161`)
- [x] `!` prefix shell escape: output lands in transcript (tool-style block) and in the conversation as a non-authored `$ <cmd>` user message the model sees next turn (opencode `prompt/index.tsx:815`, `:1059`). Shipped as a submit-time prefix, not a mode — remaining delta: mode chrome (border/placeholder swap, cursor-at-0-only trigger, backspace-at-0 exits), and a real tool-role result instead of a user message
- [x] `@` file mentions, pointer-style: tag any file, any path (relative/absolute/`~`), `@file#10-40` line ranges, tab-completion — a pointer note is appended to the user message, contents never inlined; the model probes with its own tools (Abe's design; alternative documented in [learnings/other-harnesses/opencode/at-mentions.md](learnings/other-harnesses/opencode/at-mentions.md))
- [ ] `@` mention fuzzy picker + frecency ranking (opencode `prompt/frecency.tsx`, `prompt/autocomplete.tsx`)
- [ ] External editor for long prompts: `$VISUAL || $EDITOR`, suspend renderer → edit temp .md → resume (opencode `editor.ts:26-53`; pi setting `externalEditor`)
- [x] Paste handling: collapse big pastes (≥3 lines) into a `[Pasted ~N lines]` placeholder expanded on submit (opencode `prompt/index.tsx:1149`) — opt-in via config `collapsePaste`, OFF by default (a paste you can't see is a paste you can't trust)
- [x] Persist prompt input history to disk, restore across sessions; up/down only navigate history when cursor is at offset 0 (opencode `prompt/history.tsx`)

## Transcript & rendering

- [x] Markdown rendering for assistant messages (glamour, hardcoded dark style — no OSC background query; finalized segments + resumed transcripts render rich, in-flight streaming stays plain text; right-padding stripped, body aligned under the "● " marker)
- [x] Diff view for `edit` tool results (pi edit tool returns `details: {diff, patch, firstChangedLine}` — `packages/agent/src/harness/tools/edit.ts`; opencode picks split vs unified by terminal width >120)
- [x] Tool rows: icon + present-participle verb while running ("Reading file…"), collapse to one line on completion, red + expandable on failure (opencode `routes/session/index.tsx:1836`, `util/collapse-tool-output.ts` — 19 lines)
- [ ] Render tool calls as they stream, before execution starts (pi: `message_update` spawns `ToolExecutionComponent` keyed by tool-call id)
- [x] Spinner with elapsed time + token count (% of context window) in status line (opencode `routes/session/footer.tsx`) — cost part done (status line shows session spend when the provider advertises pricing)
- [ ] Toast-style transient notifications for command success/failure (opencode `ui/toast.tsx` — 102 lines)
- [ ] Desktop notification/sound when a turn finishes and the terminal is blurred (opencode `attention.ts` — "when: blurred" is the detail that makes it not-annoying)

## Sessions

- [x] SQLite session store with `--resume` / `/resume` picker
- [x] Session titles: auto-generate a short title from the first exchange
- [x] `/rename` a session (opencode: ctrl+r prompt dialog) — `/rename [title]`, bare opens an inline prompt prefilled with the current title, draft preserved
- [x] `/fork` a session (pi: tree-structured JSONL entries with `parentId` — `docs/session-format.md`; opencode forks from any message via a per-message action menu) — `/fork [name]` copies the conversation to a new session with an auto-suggested `(fork #N)` name; `f` in the rewind picker forks from any message
- [x] Timeline: jump-to-message picker that live-scrolls the transcript as you browse (opencode `dialog-timeline.tsx`) — the rewind picker (idle esc esc) does this and rewinds/forwards too
- [x] Undo last message (conversation half): rewind restores the prompt text into the input for editing (opencode `routes/session/index.tsx:615`); file-change revert (opencode `revert.ts` git snapshots) is NOT done — conversation-only by design
- [x] Compaction: summarize old turns when context fills (pi settings: `compaction: {reserveTokens, keepRecentTokens}`; opencode `/compact`) — `/compact` manually; auto-compacts proactively at a configurable % of the provider-advertised context_length (GET /models, cached in ~/.whip/models.json; default 50%, `compactPct`, slidable ←/→ in the ctrl+p palette) plus retries once when the provider errors with context_length_exceeded; `/compact <model> [provider]` picks the summarizer (default `deepseek-v4-flash-0731`, else the current model when the default isn't configured); kept tail never orphans a tool_call from its result
- [x] Token/cost tracking per session (pi models.json carries `cost: {input, output, cacheRead, cacheWrite}`) — session usage totals in the status line; cost computed from provider-advertised `pricing` in GET /models (cached in ~/.whip/models.json), cached input billed at the cache-read rate; hidden when the provider doesn't advertise prices
- [ ] Export transcript to markdown with include-options dialog (opencode `/export`, `ui/dialog-export-options.tsx`)

## Agent loop

- [x] `/goal <text>` (codex-style): keep driving turns until the model verifies and explicitly declares `GOAL_MET` — continuing is the default, so it can't terminate early like claude's; `/goal resume` re-engages (also after `/resume` of a session — goals persist), `/goal clear` drops, 20-round cap pauses with a resume hint

- [x] Parallel tool-call execution with per-path file mutation lock (pi: `withFileMutationQueue`, `executeToolCallsParallel`) — `agent.runTools` fans a tool-call batch out to goroutines; write/edit serialize through a per-canonical-path channel semaphore, bash takes a global lock; results land in call order, OnToolStart/End fire per call
- [x] Retry with backoff on provider errors (pi settings: `retry: {maxRetries, baseDelayMs}`) — transient failures (429/5xx/transport) retry with exponential backoff (1s→2s→4s… capped 20s, jittered), configurable via `maxRetries` (default 8, 1 disables); streaming retries stop once visible text has been emitted so the transcript never double-prints, and context-limit errors pass straight through to the compaction retry
- [x] Streamed partial tool output (bash `onUpdate` throttled at 100ms in pi) — `bashrun.Options.OnUpdate` fires accumulated-output snapshots at most every 100ms from the run's own goroutine; the bash tool picks it up via a per-call ctx value (`tools.WithOnUpdate` — parallel calls can't cross wires), `agent.Events.OnToolOutput` carries it with the tool-call id, and the TUI renders the last-3-lines tail under the running tool row until `toolEndMsg` collapses it
- [x] Spill truncated bash output to a temp file and mention the path (pi bash tool) — when combined output exceeds `maxOutput` and gets tail-truncated, `bashrun.Spill` writes the full bytes to `$TMPDIR/whip-bash-<pid>/*.log` (0600) and the tool result appends `[full output (N bytes): <path>]` so the model can read/grep the rest; spill failure degrades silently, never breaks the result
- [x] Inject `WHIP_SESSION_ID` / `WHIP_MODEL` env into bash children (pi injects `PI_*`) — already shipped: `bashrun.SetMarkers` stamps `WHIP=1`, `WHIP_SESSION_ID`, `WHIP_MODEL`, `WHIP_PID` on every child env (wired from `tui.go` on session create/resume); checkbox was stale

## Skills & subagents

- [x] Skills: scan `.agents/skills/*/SKILL.md` (project) and `~/.whip/skills/` (user), inject name+description into the system prompt as an `<available_skills>` block; the model reads a SKILL.md with its own read tool when relevant (pi's approach — no skill tool needed, `packages/coding-agent/src/core/skills.ts`)
- [x] Subagents: a `task` tool that runs a self-contained prompt in a fresh agent with the same tools (minus `task` — no recursion) and returns its final report
- [x] `$skill-name` invocation (codex-style) with live completion dropdown; skills re-indexed every turn and every `$` keystroke, so new skills load without restarting the harness
- [ ] Custom agent definitions (`.agents/*.md` with model/tools/prompt frontmatter; opencode agents config `packages/core/src/config/agent.ts`)
- [x] Parallel/background subagents (pi streams tool `onUpdate`; opencode `background-job.ts`) — `task` with `background:true` runs concurrently and reports back via a steered message; a `taskRegistry` keyed by id holds a `Done` channel whose single close broadcasts completion to every waiter; `/tasks` lists them, a `⚙ N sub` header badge shows running count, `/tasks` updates live via `OnChange`; tasks persist in the session store and are restored on `--resume` (a stale "running" row comes back as interrupted-error)
- [ ] `@agent` mentions to target a named subagent (opencode autocomplete)

## Models & providers

- [x] Model → provider routing in config (switch providers without touching models)
- [x] Codex subscription provider: `whip auth codex` runs Codex's device-code OAuth, writes local state, and immediately fetches the signed-in account's `/codex/models` catalog for `/model`; `/auth codex` does the same in-session. Expiring credentials refresh, and the ChatGPT Codex Responses SSE endpoint maps into the existing tool loop without the unsupported `max_output_tokens` parameter; the account-scoped catalog supplies context, vision, and reasoning capabilities
- [ ] `anthropic-messages` API style alongside `openai-completions` (pi: `packages/ai/src/api/`)
- [x] `"$VAR"` / `"!cmd"` resolution for apiKey/header values in config (pi models.json value resolution) — shipped with secrets-by-reference (internal/config/secret.go), resolved at point of use
- [x] Reasoning effort: `/effort [off|low|medium|high]` (bare opens the selector), tab-completes, clickable `⚡` control in the header top-right; sent as `reasoning_effort`, inherited by subagents, survives model switches
- [ ] Per-model sampling params in config (`samplingParams: {temperature, top_p}`)

## MCP

Improvement plan with per-item checkboxes: [`.ai-docs/plans/mcp-polish/`](../.ai-docs/plans/mcp-polish/README.md).

- [x] MCP client: stdio + streamable HTTP servers; config merges claude-style `.mcp.json` and codex-style `~/.codex/config.toml [mcp_servers]` under whip's own `"mcp"` block (opencode's status model `mcp/index.ts:83-106`, name sanitization + tool bridging `mcp/catalog.ts:47-90,117-119` — with the sanitize-collision fixed via hashed server keys; claude-code's `mcp__server__tool` naming kept). Lazy-with-kickoff connects (close-to-broadcast `ready` chan), per-server call serialization, 30s startup / 60s call timeouts, errors as tool output, `/mcp` status + reconnect/enable/disable, `whip mcp add|list|remove|serve`
- [ ] MCP resources/prompts (opencode: synthetic `read_mcp_resource` tools + prompts-as-slash-commands)
- [ ] MCP OAuth for remote servers (opencode `oauth-provider.ts` — buffer creds in memory, commit on success; ~800 lines, a `needs_auth` status covers most of the value first)
- [ ] `ToolListChanged` notification → live re-list (opencode `mcp/index.ts:462-471`; needs the standalone SSE stream on remote transports)
- [x] Fail-fast MCP calls (connecting server can't park a turn) + did-you-mean on unknown mcp__ tools + first-settle transcript note — the "never stuck, always know why" pass
- [x] Auto-reconnect with backoff on dropped sessions (gen-guard makes it safe; manual `/mcp reconnect` stays as override)
- [x] MCP server instructions injected into the system prompt (opencode `session/system.ts:119-135`)
- [x] `whip mcp test <name>` (the doctor: connect + list + timing + stderr tail, non-zero exit on failure — CI-checkable `.mcp.json`)
- [x] `whip mcp import [--dry-run]` (materialize claude/codex imports into whip's config)
- [x] MCP import source gating: `"mcpImport"` block (`enabled`/`only`/`exclude` per claude/codex source); blocked imports stay visible in `/mcp` and `whip mcp list` instead of vanishing — stops third-party codex-config entries (e.g. ChatGPT app's `node_repl`) from being picked up wholesale
- [ ] Overlay config entries (`"overlay": true` patches `enabled` over imports instead of copying definitions)

## LSP

- [x] LSP diagnostics in `write`/`edit` tool output — stdlib-only client (`internal/lsp/`), gopls built-in + user servers via the `"lsp"` config block, capped 1.5s wait, sibling-file errors included (opencode `src/lsp/` diagnostics flow, research in `docs/learnings/other-harnesses/opencode/lsp.md`); plan: [`.ai-docs/plans/lsp-diagnostics/`](../.ai-docs/plans/lsp-diagnostics/README.md) (Linear INF-4989)
- [ ] `@file.go#N` symbol-range expansion via `documentSymbol` (Linear INF-4991; deferred from the at-mentions port — see `docs/learnings/other-harnesses/opencode/at-mentions.md`)
- [ ] Read warm-up (forked `touchFile` on read so first-edit diagnostics are instant — opencode `tool/read.ts:119`)
- [ ] Pull diagnostics (`textDocument/diagnostic`) for servers without push
- [ ] Navigation tool (definition/references/symbols) if cross-file diagnostics prove insufficient

## Safety & permissions

- [x] Permission prompt: Allow once / Allow always / Reject, where "always" previews the exact rule it installs and "reject" takes a free-text redirect message back to the model (opencode `routes/session/permission.tsx`)
- [x] Command-prefix arity for useful "allow always" rules: `git checkout branch` → rule for `git checkout`, not the whole string (opencode `permission/arity.ts`)
- [x] Secrets as references, never values: `"$VAR"`/`"!cmd"` (or `${ENV_VAR}`-style) indirection in config and MCP/tool init, resolved host-side at point of use so raw keys never enter the event log or model context (exo `crates/exoharness/src/secrets.rs` — AES-GCM at rest with keychain/file master key is the full version; the indirection alone is most of the safety)

## Theming & config

- [x] ctrl+p command palette (opencode-style): modal dialog (own filter line, esc pops one level, ↑/↓ wraps), category headers, "Suggested" group pinned when the filter is empty, dimmed keybind/slash hints teach shortcuts, cheap subsequence fuzzy filter; fully interactive — rows show live state badges, ←/→ step reversible settings (effort, thinking, mouse) in place, and enter drills into sub-panels (model browser with live preview-switch, effort levels, compaction model, inline goal editor) that apply real changes without leaving the palette
- [x] Single keybind+command registry: palette, slash commands, help, and footer hints all derived from one table (opencode `config/keybind.ts` — the highest value-per-line idea in that repo)
- [ ] One generic fuzzy-select widget reused by every picker: model, session, theme, timeline (opencode `ui/dialog-select.tsx`)
- [ ] KV table in sessions.db for palette-toggleable UI prefs — no config ceremony per toggle (opencode `context/kv.tsx` pattern)
- [ ] Theme support: JSON themes with named defs + `{dark, light}` variant pairs; a "system" theme built from the terminal's real palette (opencode `theme/index.ts`)
- [x] `"mouse": false` config escape hatch so native terminal selection works (opencode `app.tsx:196`) — also a runtime `/mouse` toggle; with capture on, hold shift to select text in the transcript

## CLI surface

- [x] Non-interactive one-shot mode: `whip run "prompt"` — reads piped stdin too, `--format json` emits the raw event stream for scripting (opencode `cli/cmd/run.ts`)
- [x] `whip sessions` list subcommand
- [x] Env markers in child processes (`WHIP=1`, `WHIP_SESSION_ID`) so scripts can detect they run under the agent (opencode sets `AGENT=1`, `OPENCODE_PID`)

## Autonomy & durability

From [exo](learnings/other-harnesses/exo.md). Triaged against whip's actual code:
compaction today is destructive (`session.go` `DELETE FROM messages`), resume of a
crashed turn can orphan a tool_call, and there is no plan-tracking tool at all.
Ordered by value-per-line for a single-binary TUI — the first four are the ones
worth doing now.

**Do now:**

- [x] `todowrite` planning tool (the biggest gap): conversation-scoped store, full-list rewrite each call, exactly one item in_progress, injected back each round so the plan survives long tool loops and compactions; caps ~50 items × 300 chars (exo `exo/tools/todo-tools.ts` is ~100 lines; the claude/opencode pattern) — `internal/agent/todo.go`, persisted on the sessions row, restored on resume
- [x] Synthesize error tool-results for dangling tool calls when materializing a crashed/interrupted turn on resume — correctness fix, not a feature: one interrupted turn can otherwise produce an API-rejected history (exo `flushDanglingToolResults`, `exoharness/typescript/harness/index.ts:786-804`) — `answerDanglingToolCalls` at the `session.Load` boundary, synthetic result appended right after its assistant message
- [x] Compaction as a recorded event, not `DELETE FROM messages`: store summary + cutoff seq and derive the prompt view; the raw log stays queryable so a bad compaction is inspectable and retryable. The thin end of the event-sourcing wedge without a store rewrite (exo spec.md: "the durable conversation does not have to equal the prompt") — `compactions` table (append-only summary+cutoff), `Load` derives the view, `/compact log` inspects, `/compact retry` undoes the latest and recompacts from the raw log
- [x] Workspace rewind: git-snapshot the working tree per turn (or on demand) so file changes can be rolled back, and record the rollback in the session — "rewind does not erase history": rolling back the world must not delete the memory of what was tried (exo `rewind_sandbox` appends `SandboxStarted{snapshot_id}`; opencode `revert.ts` is the same idea) — pre-turn snapshot pinned under `refs/whip/snapshots/`, keyed by turn index in a `snapshots` table that `DeleteFrom` trims with the messages; `applyRewind` restores via `checkout <ref> -- .` and notes "⟲ workspace rewound — N file(s) restored"; untracked files never touched

**High value, cheap:**

- [x] `remember`/`forget` memory tools: plain markdown files (`~/.whip/memory.md` installation scope + `~/.whip/sessions/<id>.memory.md` session scope), checkbox bullets the user can edit by hand; `forget` strikes rather than deletes; always-inject with a hard cap (50 × 300 chars — the cap is the retrieval strategy, no embeddings); `/memory` lists both scopes numbered and marks entries done from the TUI (exo `exo/tools/memory-tools.ts`, redesigned to markdown after the opencode finding: opencode has no memory tool, its answer is AGENTS.md — files you own and diff)
- [x] Stealable `me.md` operating rules for the system prompt: "the tool set changes turn to turn — never assume a tool exists because it did earlier"; "after ~3 failed attempts on the same blocker, escalate plainly instead of looping"; git hygiene ("never `git add .`, review staged diff for secrets, never force-push") (exo `exo/prompts/me.md`) — shipped in `cmd/whip/main.go`'s system prompt, plus a remember/forget pointer

**Later (needs `/goal` usage to justify the always-on turn):**

- [x] Minimal scheduler + generic wakeup channel: `@every 10m` / `@at <rfc3339>` tasks firing machine-authored user-message turns; grid-anchored fires (slow runs don't drift), one-shot completion stays listed as (fired), fires defer while busy without drifting the grid. Cron syntax deliberately cut (two forms cover the use); the record-then-deliver outbox and `reportPrompt` routing remain future work if external channels land (exo `scheduler_runtime.rs`, `conversation_wakeup.rs`) — `internal/schedule` (parser, ~70 lines), `schedules` table in sessions.db, 5s ticker in the TUI, `/schedule @every|@at <prompt> | list | cancel <n>`, ⏰ transcript marker

**Deliberately cut** (exo needs them because it's long-running and edits itself in production; a coding TUI doesn't):

- ~~Full event-sourced store rewrite~~ — too big once compaction-as-event lands; keep custom-kind discipline inside the messages-table world instead
- ~~`/events` introspection tool~~ — pays off with adapters/restarts whip doesn't have; cost already lives in the status line
- ~~`rebuild_and_restart_whip` + SELF.md self-map~~ — a harness rebuilt by hand between sessions doesn't need to restart itself mid-conversation
