# Features

whip is a minimal coding-agent harness: an interactive bubbletea TUI driving an
LLM tool-use loop (bash / read / write / edit / task) with provider-routable
models. This document is the map of what's shipped and where it lives. Each
section links the behavior to the code and its tests.

## The agent loop

`internal/agent/agent.go` — `Agent.Turn` is the loop: append the user message,
stream a completion, run any tool calls, append results, repeat until the model
stops calling tools. Steered messages (`Steer`) inject at loop boundaries,
never mid-generation.

### Parallel tool calls with per-path file locks

When the model emits several tool calls in one turn, `runTools` fans them out
to goroutines and collects results on a buffered channel, laid back out in
**call order** (the API matches tool results to call IDs). `OnToolStart` /
`OnToolEnd` fire per call as they run, so the UI shows each tool live.

`internal/agent/filelocks.go` — mutations to the same file serialize through a
**per-canonical-path channel semaphore** (a 1-capacity `chan struct{}` per
path: send to acquire, receive to release). Two edits to `foo.go` can't
interleave; edits to different files run truly in parallel. `bash` takes a
global lock because a command's side effects aren't attributable to one path.
Reads don't lock.

This is the Go-native port of pi's `withFileMutationQueue` (per-path promise
chains in TypeScript). In Go the lock is a buffered channel — no explicit
unlock bookkeeping.

Tests: `parallel_test.go` — `TestToolCallsRunInParallel` (overlap measured via
a concurrency counter), `TestSamePathEditsSerialize`, `TestToolMutationPath`,
`TestCanonicalPathKey`.

### Bash output feedback: live streaming + truncation spill

Two ways a bash command stops being a black box (pi's bash tool has both):

- **Streamed partial output.** `bashrun.Options.OnUpdate` reports the
  accumulated combined stdout/stderr at most every 100ms
  (`bashrun.updateInterval`) from a snapshot ticker goroutine owned by the
  run — it snapshots the shared buffer under the drains' mutex and exits on a
  done-channel close when `runPiped` returns (one trailing tick may land after
  return; the TUI's `toolRunning` check makes it a no-op). The TUI callback
  uses a detached `go p.Send(...)`, so a wedged UI queue can never park the
  ticker goroutine (docs/concurrency.md's ABBA rule). The bash
  tool receives the callback through a **per-call context value**
  (`tools.WithOnUpdate`), not a package var, so parallel tool calls can't
  cross wires; `agent.runTools` attaches it when `Events.OnToolOutput` is set
  and the call is bash. The TUI (`toolOutputMsg`) renders the last three
  non-empty lines under the running tool row's verb line (`block.live`);
  `toolEndMsg` clears it and collapses the row as before. The final output
  still arrives via the tool result — snapshots are progress, never state.
- **Truncation spill.** When combined output exceeds `maxOutput` (50KB) and
  `TruncateTail` fires, `bashrun.Spill` writes the **full** bytes to
  `$TMPDIR/whip-bash-<pid>/*.log` (0600, OS-reaped) and the tool result
  appends `[full output (N bytes): <path>]` so the model can read/grep the
  head it never saw. Spill failure degrades silently — a broken temp dir must
  not cost the tool result.

Tests: `internal/tools/bashrun/feedback_test.go` — `TestOnUpdateThrottle`
(≥95ms between fires, prefix-growing snapshots), `TestOnUpdateNil`,
`TestOnUpdateFastCommand`, `TestSpill` (content round-trip + 0600 perms);
`internal/tools/bash_feedback_test.go` — `TestBashToolSpillOnTruncation`
(notice + file holds the truncated-away head), `TestBashToolNoSpillUnderCap`,
`TestBashToolOnUpdateCtx`; `internal/agent/tool_output_test.go` —
`TestOnToolOutputStreamsBash` (event carries the tool-call id, fires mid-run);
`internal/tui/tool_output_test.go` — `TestToolOutputMsgUpdatesRunningRow`
(unknown id ignored, tail replaces, end clears), `TestLastLines`.

### Compaction

When the conversation fills the context window, old turns fold into an
LLM-generated summary. Two triggers:

- **Proactive**: `maybeCompact` runs before each request once the estimated
  token count crosses the compaction threshold — a percent of the advertised
  context window, default 50% (`compactPct` in config, clamped 10–90;
  `Agent.CompactThreshold` holds the fraction). Slide it in the palette's
  "Compaction level" row (←/→ steps ±10%).
- **Reactive**: if the provider still rejects a request with a context-limit
  error (`context_length_exceeded`, `prompt_too_long`, HTTP 413), `Turn`
  compacts once and retries. A `compacted` guard prevents retry loops.

`compact()` keeps the system prompt and a recent tail, and is **orphan-safe**:
a kept tail that begins with a `tool`-role message walks back to its owning
assistant message so no tool result references an erased call ID. The summary
runs as a non-streaming `Complete` on the compaction model — the built-in
default `deepseek-v4-flash-0731` (`config.DefaultCompactModel`, resolved from
the user's config when `compactModel` is empty), a configured
`compactModel` / `compactProvider`, or the conversation's own model when the
default isn't in the config.

Token bookkeeping: `llm.Usage` (prompt/completion/cached) is read off the
terminal stream chunk (`stream_options: include_usage`) and folded into session
totals via `AddUsage`. Compaction and subagent calls count too.

Commands: `/compact` (compact now), `/compact <model> [provider]` (pick the
summarizer), `/compact off` (restore the built-in default). The palette's
"Compaction model" panel lists every configured model behind a
"default (…)" row that restores the default; "Compaction level" steps the
threshold ←/→.

Tests: `agent_test.go` — `TestTurnAutoCompactsOnContextLimit`,
`TestCompactDoesNotLoopOnRepeatedContextLimit`, `TestCompactKeepsToolCallPair`,
`TestProactiveCompactAtFiftyPercent`, `TestCompactThresholdExplicitOverride`,
`TestUsageAccumulates`; `compact_cmd_test.go` —
`TestCompactModelEmptyResolvesDefault`, `TestCompactModelDefaultFallsBack`,
`TestCompactThresholdFor`, `TestSetCompactPct`; `palette_test.go` —
`TestPaletteCompactPanelAppliesInPlace`,
`TestPaletteCompactPanelDefaultRowRestores`, `TestPaletteCompactionLevelSteps`.

### Background subagents

`internal/agent/background.go` — `task` with `background: true` launches a
subagent that runs **concurrently with the parent** instead of blocking the
turn. This is the channel-native port of opencode's `background-job.ts`
registry.

Each task is a `BackgroundTask` with a `Done chan struct{}`. When the subagent
settles, the registry `settle()`s and **closes `Done` once** — closing a
channel broadcasts to every waiter at once, so the tool caller, the TUI, and
`/tasks` all wake together with no per-waiter state (opencode needs a per-job
`Deferred` for the same thing). On settle the report fans back into the parent
as a **steered message**, so the model sees it on the next loop boundary.

- `Tasks().List()` / `Get(id)` / `Cancel(id)` — registry snapshot + cancel.
- `Tasks().OnChange` — the TUI installs a callback that sends a message to
  redraw live. `Tasks().OnRecord` — a second hook the TUI uses to upsert the
  task into the session store on start and settle.
- `/tasks` lists running/done subagents with report previews; a `⚙ N sub`
  header badge shows the running count. The persistent dock strip above the
  input is mouse-clickable: `dockTop()` maps screen rows to task rows,
  skipping the focused hint row (`dockSkip`) so a click opens the row
  actually clicked.
- **Persisted across resume.** The session store's `tasks` table records
  every start/settle; `resume()` seeds the registry via `RestoreTask`
  (settled, `Done` pre-closed, marked `Restored`). A row still `running` on
  disk means the subagent died with the last process exit, so it comes back
  as `error` — "interrupted — whip exited". Restored tasks are history:
  `/tasks` lists them with a `(restored)` marker; the dock never shows them.
  The dock itself shows running tasks plus ones settled within a one-minute
  grace window (`dockSettledGrace`) — long enough to notice the ✓, then the
  strip cleans itself.

Background tasks use a context **not** tied to the current turn — they outlive
it by design. Cancelling a task cancels its subagent's turn.

Tests: `TestBackgroundTaskDeliversReport`, `TestBackgroundTaskBroadcastsToManyWaiters`
(8 waiters all woken by one channel close), `TestBackgroundTaskCancel`;
persistence: `session.TestTaskRoundTrip`, `TestRestoreTaskSettledAndVisible`,
`TestResumeRestoresTasks`, `TestTaskPersistsOnStartAndSettle`;
dock click hit-testing: `TestDockClickOpensClickedRow`,
`TestDockClickIgnoredWhilePaletteOpen`.

## Models & providers

`internal/config/config.go`, `internal/config/catalog.go` — models route to
providers; OpenAI-compatible providers use `GET /models` as the source of
truth for capabilities. Two distinct limits, both honored:

- **Context window (input)** — `Model.Context` (legacy `maxTokens` still
  parses via `ContextWindow()`), overridden by the provider's
  `context_length`. Drives the header's `% ctx` and proactive compaction.
- **Output cap** — `Model.MaxOut`, else the provider's `max_completion_tokens`,
  else the context window. Sent as the request's `max_tokens` for
  OpenAI-compatible chat-completions providers; Codex subscriptions omit the
  rejected `max_output_tokens` field and let the backend enforce its cap.

The catalog (`~/.whip/models.json`) caches each provider's model list with a
24h TTL and refreshes in the background. When the provider advertises
per-token `pricing` (inference.net / OpenRouter shape — `prompt`,
`completion`, `input_cache_read` decimal strings), the catalog caches the
parsed rates and the status line appends the session's cumulative cost to the
token spend (`31.1k(20.7k)/360 tok · $0.0134`): fresh input at the prompt
rate, cached input at the cache-read rate (full prompt rate when none is
advertised), output at the completion rate — `llm.SessionCost`. Providers
without pricing hide the segment entirely. Tests: `llm/openai_test.go`
(`TestSessionCost`, pricing unmarshal), `config/catalog_test.go`,
`tui/status_test.go` (`TestStatusLineShowsCost`, `TestStatusLineHidesCostWithoutPricing`).

`internal/llm/openai.go` — the streaming client. Typed `HTTPError` (keeps the
`<status>: <body>` shape), `IsContextLimit()` classifies context-overflow
errors for the compaction retry, `Stream` returns the message + usage, and
`Complete` is the non-streaming round-trip used by compaction.

Transient request failures — 429, any 5xx (e.g. a gateway's 524), and
transport errors — retry with exponential backoff (1s→2s→4s… capped 20s,
+25% jitter, ctx-cancellable). Budget: `maxRetries` in config (default
`llm.DefaultMaxAttempts` = 8, `1` disables). A streaming attempt is only
retried before the first visible delta reaches the UI — after that a retry
would replay rendered text, so the error surfaces instead. Mid-stream
provider `error` chunks and 4xxs (including context-limit, which the
compaction path must see immediately) are never retried. Each retry posts a
`⚠ request failed (…) — retrying in Ns (attempt N/M)` line via the
`OpenAI.OnRetry` hook. Tests: `llm/retry_test.go`.

### Codex subscription provider

`"api": "openai-codex-responses", "auth": "codex"` routes a configured
model through the ChatGPT Codex Responses SSE endpoint without an API key.
`whip auth codex` implements Codex's device-code sign-in: it shows the
verification URL and one-time code,
polls until approval (or ctrl+c), exchanges the server-provided PKCE verifier,
atomically stores the result in Codex-compatible `~/.codex/auth.json`, and
then upserts the `codex` provider plus `gpt-5.4` fallback route **and fetches
the signed-in account's `/codex/models` catalog**. `/auth codex` does the same
within an active TUI session, so `/model` shows every available subscription
model immediately. Neither flow changes the user's default model. The backend
catalog is authoritative for plan and rollout availability and refreshes on
the normal 24-hour TTL or `/model refresh`. `internal/codexauth/auth.go`
derives missing account and expiry
data from JWT claims, refreshes within five minutes of expiry, and preserves
unrelated auth-file fields. Tokens are never logged or sent to the conversation.

`internal/llm/codex.go` maps messages, tool calls, tool results, text/thinking
deltas, usage, and the account-scoped `/codex/models` response to Whip's
existing provider contract. Codex subscription requests omit
`max_output_tokens`, which that endpoint rejects; its backend owns the output
limit. Catalog context, vision, and supported reasoning efforts flow through
the same picker and resolver as OpenRouter. OAuth credentials are accepted
only for `https://chatgpt.com/backend-api`. Tests: `codexauth/auth_test.go`,
`cmd/whip/auth_codex_test.go`, `llm/codex_test.go`, and `tui/model_cmd_test.go`
(`TestBuildAgentCodexAuth*`).

`cmd/whip/auth.go`, `internal/config/openrouter.go`, `internal/config/codex.go`,
`internal/tui/auth_cmd.go` — one-command provider onboarding. `whip auth openrouter [--env] [<key>]` takes
the key from arg / `OPENROUTER_API_KEY` / a masked prompt, **validates it
against the live API before writing anything** (a rejected key leaves no
trace), upserts the `openrouter` provider into config (literal `apiKey` by
default — config is 0600; `--env` stores `apiKeyEnv: OPENROUTER_API_KEY` and
offers to append the export to the shell rc), and pre-fetches the model
catalog so `/model` lists the entire OpenRouter catalog immediately — every
model usable with zero per-model config via catalog resolution. In-session,
`/auth openrouter` (bare) repurposes the input box as a **masked** one-shot
prompt (the `namePrompt` machinery with a `mask` flag — the key never
echoes, and the inline-key form is kept out of ↑-recallable input history);
a session already routed through openrouter is hot-rebuilt with the new key
so re-authing fixes a 401 without a `/model` round-trip. `whip auth codex` and
`/auth codex` use the subscription device flow above and pre-fetch the
account-scoped Codex catalog, so its models use the same zero-config picker
path. Tests:
`config/openrouter_test.go` (upsert modes, idempotence, `TrimKey`),
`cmd/whip/auth_test.go` (httptest fake OpenRouter — good key wires provider
+ catalog + makes catalog models resolvable, bad key writes nothing,
re-auth keeps other providers/models), `config/codex_test.go` (fallback route,
preservation, idempotence), `cmd/whip/auth_codex_test.go` (auth configures Codex
and caches its catalog), and `tui/auth_cmd_test.go` (usage, masked prompt
open/cancel, good/bad result, live-session rekey, Codex account catalog picker).

`cmd/whip/auth_inferencenet.go`, `internal/inferencenet/`,
`internal/config/inferencenet.go`, `internal/tui/auth_inferencenet_cmd.go` —
first-class Inference.net auth. `whip auth inference-net login` runs the
**OAuth device-authorization flow** against the relay
(`internal/inferencenet/device.go`): request a code, open the dashboard's
approval page in the browser, poll until approved — then select the account's
primary project and **mint a machine API key** (`whip-<host>-<timestamp>`)
via the relay's tRPC (`apiKey.create`), all without the user touching a key.
State lands in `~/.whip/inference-net.json` (0600 — session token + machine
key, kept out of config.json); the provider entry carries no key material and
resolves the machine key from that file at request time (`Provider.ResolveKey`
fallback, ahead of the legacy `~/.inf/config.json` read). BYOK is supported
too: `login --key <k>` / `--env` validates against the gateway (`GET /models`)
before writing. Subcommands: `status`, `logout` (archives the machine key +
closes the remote session), `key rotate`. The tRPC client
(`internal/inferencenet/trpc.go`) is stdlib-only — the superjson transport is
plain JSON plus a `{"json": …}` envelope for the shapes whip touches. The
provider key was renamed `inference` → `inference-net`; `Config.normalize`
migrates old configs (provider, model routes, default/compact provider)
transparently on load. Tests: `inferencenet/inferencenet_test.go` (stubbed
relay: full device login + key mint, store round-trip, key validation),
`config/inferencenet_test.go` (rename migration, upsert modes, key fallback),
`cmd/whip/auth_inferencenet_test.go`, `tui/auth_inferencenet_cmd_test.go`.

## The TUI

`internal/tui/tui.go` — bubbletea fullscreen alt-screen. Highlights:

- **ctrl+c is a two-stage key.** While busy it interrupts the turn (first press
  arms, second cancels). While idle it quits — but only on a **second press
  within a 2-second window**, so a stray ctrl+c can't nuke the session. The
  hint `press ctrl+c again to quit` shows while armed.
  Tests: `quit_confirm_test.go`.
- **Collapsible tool results.** Tool results store raw output in a `blockTool`
  transcript block and render collapsed to 5 lines with a `… +N lines` hint.
  `ctrl+e` toggles the most recent; clicking a block expands/collapses it
  (each block tracks its rendered line range `y0`/`y1` so the click row maps
  through the viewport offset). Blocks re-render at the current width on
  terminal resize. Tests: `tool_expand_test.go`, `resize_test.go`.
- **Markdown.** Assistant messages render through glamour; streamed in-flight
  text stays plain and renders on flush. `markdown.go`.
- **Clickable links (OSC 8).** URLs and existing local file paths in the
  transcript are terminal hyperlinks — cmd/ctrl-click opens them, no mouse
  plumbing in whip (the terminal owns the click). `links.go` runs two passes
  over glamour's output: `hyperlinkGlamourLinks` rewires rendered
  `[label](url)` links so the href atom becomes the OSC 8 target on the
  label instead of a second visible copy (bare autolinks become clickable in
  place), and `linkifyRenderedFilePaths` wraps bare `path/to/file[:N]` tokens
  in `file://` links — gated on the file existing on disk, resolved against
  the process CWD. User-input echoes (submit, resume replay, steer) get the
  same file linkification on the raw text. Unsupported terminals ignore
  OSC 8 and show the underlined text as before; copy/selection strips the
  sequences. Tests: `links_test.go` (ref regex, target gating, glamour
  rewiring incl. wrap-split links, end-to-end renderMarkdown, user echo).
- **Command palette** (ctrl+p) with sub-panels for model/effort/goal/compaction
  and ←/→ steppers for the compaction level — `palette.go`.
- **Mouse**: `/mouse` toggles capture; with capture off the terminal's native
  selection works, with it on shift-drag selects. `"mouse": false` in config
  disables capture at startup.
- Queueing (enter while busy), steering (empty enter), history recall (↑/↓),
  `@file` mentions, `$skill` invocation, `/goal` loop, `/resume` session
  picker, `/effort` reasoning levels — see the roadmap for the full list.
- **Settings commands run mid-turn.** `/theme`, `/mouse`, `/effort`, `/tasks`,
  `/help`, `/cd`, `/pwd`, and the non-submitting `/goal` forms (bare, `clear`,
  `rounds`) execute immediately while busy instead of queueing — queued text
  is sent to the model verbatim after the turn, which is nonsense for a
  settings change. The `busyCmd` allow-list gates this; everything else
  (`/model`, `/goal <text>`, plain messages) still queues. These commands only
  touch TUI-local state or fields read at the *next* request, and their
  confirmation notes append as transcript blocks safely behind the streaming
  one. Tests: `queue_test.go` (`TestBusyCmdAllowList`, `TestEnterWhileBusy*`).
- **`!` shell escape, `/cd`, `/pwd`** — `shell.go`. An input starting with
  `!` runs locally via the same `bashrun` runner as the agent's bash tool
  (120s cap, `tools.TruncateTail`, `(exit …)` markers) — no model turn, no
  busy state, runs immediately even mid-turn, and queued `!` lines execute
  when the queue drains instead of being submitted to the model. The command
  runs on a goroutine and lands via `shellDoneMsg` (the UI never blocks), the
  output lands in the transcript as a collapsed tool block **and** in the
  conversation so the model sees it at the next request: idle via
  `Agent.AppendUser` (non-authored `$ <cmd>` user message), mid-turn via
  `Agent.Steer` (mutex-guarded, injected at the next loop boundary with the
  usual `(steered)` echo) — the turn goroutine owns `Agent.Messages` while
  busy. Esc stays bound to the turn; a running escape isn't cancellable (the
  120s cap bounds it). `/cd [dir]` changes whip's process cwd (an in-flight
  command keeps its already-resolved cwd, POSIX; the next spawns, relative
  tool paths, and the `@` index follow); bare prints it, `~` expands. `/pwd`
  prints it. Port of opencode's `session.shell` minus the shell-mode chrome —
  see the `ponytail` note in `shell.go`.
  Tests: `shell_test.go` (output/message routing idle+busy, queue-drain,
  truncation, echo rules, cd/pwd incl. `~` and bad dirs).
- **`/goal-from-context [n]`** distills the last *n* conversation messages
  (default 8, clamped to the available history) into a detailed goal — a
  concrete outcome line plus a bullet list of checkable completion criteria —
  with one non-streaming call on the current model (the compact-model override
  is deliberately ignored), then sets it exactly like `/goal <text>` and starts
  the goal loop. The transcript note states the exact window used. Prompt
  building is pure (`agent.BuildGoalFromContextPrompt` over the window from
  `agent.GoalFromContextMessages`); the TUI command mirrors `/compact`'s
  goroutine + `goalFromContextMsg` pattern, refusing while busy and running
  inline when headless. Tests: `goal_test.go` (`TestGoalFromContext*`).
  User-facing walkthrough: [goal-from-context.md](goal-from-context.md).

## Conversation time travel

`internal/tui/rewind.go` — **double-esc while idle** opens the rewind picker:
the conversation's authored user messages, newest first, with the transcript
**live-scrolling** to the selected message as you browse (opencode's
`dialog-timeline.tsx` `onMove`, and `msgBlock` maps conversation index →
transcript block so the jump is direct). enter rewinds to just before the
selected message: `Agent.Messages` is truncated, the clipped tail becomes an
in-memory **redo stack** (`m.future`, oldest first), the DB rows are deleted
(`Store.DeleteFrom`), the transcript is rebuilt via `seedTranscript`, and the
rewound message's text lands back in the input for editing (opencode's undo:
"the input restore is what makes it feel good"). Cuts sit at user-message
indices, so a tool_call is never orphaned from its results.

**Forward travel:** reopening the picker while rewound lists the clipped
messages dimmed, marked `(rewound)`; enter on one pulls the tail back in and
re-saves it. Submitting new input while rewound discards the redo stack.
Compaction also drops it (a stale redo would resurrect summarized history).
esc cancels and restores the scroll position. The redo stack is in-memory
only by design: quitting while rewound leaves the DB at the rewound point.

`internal/tui/fork.go` — **`/fork [name]`** copies the conversation into a
**new** session (one `INSERT…SELECT` in `Store.Fork`; the original is
untouched and stays under `/resume`) and switches to it. Bare `/fork` opens an
inline name prompt prefilled with `<title> (fork #N)` (`Store.ForkTitle`
increments past existing forks and unwraps nested suffixes, opencode's
`getForkedTitle`). **`f` in the rewind picker** forks from the selected
message instead — one picker, two destinations. Forking while rewound pulls
the redo stack up to the picked point into the copy. **`/rename [title]`**
retitles the current session (`Store.SetTitle`); bare opens the same inline
prompt prefilled with the current title. Both prompts stash and restore any
in-progress draft. All three refuse to run mid-turn. Palette entries:
"Rewind conversation", "Fork session", "Rename session" under Session.

Tests: `rewind_test.go` — double-esc opens/cancels, busy esc still
interrupts, truncation + input restore + DB rows deleted, forward travel,
partial-rewind DB prefix, tool-call-pair safety, stale esc-arm across modal
dismiss, draft preservation, resume-after-rewind. `fork_test.go` (session) —
prefix/full copy, fork-title numbering, rename, DeleteFrom. `fork_test.go`
(tui) — fork with arg, bare prompt suggestion + cancel, fork from the picker,
fork while rewound into the redo stack, rename both paths.

## MCP

`internal/mcp/` — whip is an MCP client (stdio + streamable HTTP) and, via
`whip mcp serve`, an MCP server. Three sources of server config merge with
whip's own on top (per-name, whole entry): a project `.mcp.json`
(claude-style: `{"mcpServers": {name: {type, command, args, env, url,
headers}}}`), `~/.codex/config.toml` `[mcp_servers.*]` (codex-style), and the
`"mcp"` block in `~/.whip/config.json`. Claude `type: sse` imports as
disabled-with-note (legacy transport); `${VAR}` references in env/headers
expand from whip's environment.

- **Manager** (`manager.go`) — one lifecycle goroutine per server; a
  `ready chan struct{}` closes once on first settle (the BackgroundTask
  close-to-broadcast pattern), so tool calls block only on *their* server and
  startup never waits. Statuses: connecting → ready/failed (plus disabled);
  a dropped session flips to failed via a generation-guarded watcher
  (opencode's client-identity check, `mcp/index.ts:443`). Connect/list bounded
  by `startupTimeout` (default 30s — opencode's DEFAULT_TIMEOUT).
- **Tool bridge** — listed tools become agent tools named
  `mcp__<server>__<tool>` (claude-code convention; double underscores keep
  the split unambiguous since tool names contain `_`). Unsafe server-name
  chars get an fnv hash suffix so sanitized names can't collide (an opencode
  weakness). Calls serialize per server (1-cap channel — many stdio servers
  are single-request), run under `toolTimeout` (default 60s), and respect
  ctrl+c via ctx. Results flatten to text: images/audio/binary resources →
  placeholders, `structuredContent` → JSON when there's no text, `IsError` →
  `"Error: …"` fed back to the model — a broken MCP tool never kills a turn.
  Output capped at the shared 50KB truncation. MCP tools take no file locks
  and run in parallel with everything.
- **Late arrivals** — `Manager.SetOnChange` pushes refreshed tool sets into
  `Agent.SetMCPTools` (mutex-guarded; a settle mid-turn can't race the slice
  a request reads), so a server connecting after turn 1 appears without a
  restart.
- **TUI** — `/mcp` shows the status table (`● N tools` / `✗ err` /
  `○ disabled` / `◌ connecting…`); `/mcp <name> reconnect|enable|disable`
  reconnects live or persists a toggle through the guarded `Config.Save`.
- **CLI** — `whip mcp list` (merged view with per-name source labels —
  `whip config` / `.mcp.json` / `codex config` — and a `blocked` state),
  `whip mcp add <name> -- <cmd...>` / `--url`, `whip mcp remove`,
  `whip mcp import [--dry-run]` (materializes imported servers into whip's
  config; `--dry-run` prints the JSONC fragment without writing; blocked
  servers are never imported). `whip mcp serve` (`serve.go`) exposes whip's
  read/bash/edit/write as an MCP stdio server for other harnesses.
- **Import gating** — the `"mcpImport"` block in `~/.whip/config.json`
  (`{"claude": …, "codex": …}` per source: `enabled`, `only` allowlist,
  `exclude` denylist — exclude wins over only; absent block imports both
  sources, the pre-gating behavior). Filtered-out imports land in the
  discovery result's `Blocked` map as disabled+noted copies
  (`LoadMergedFiltered`), stay visible in `/mcp` and `whip mcp list`
  (`○ disabled — blocked by mcpImport config`), and `/mcp <name> enable` on a
  blocked name refuses with a pointer at the config instead of silently
  shadowing. This is the fix for third-party apps writing MCP entries into
  `~/.codex/config.toml` (e.g. the ChatGPT desktop app's `node_repl`) that
  whip would otherwise pick up wholesale.
- **Shutdown** — `Manager.Close()` runs before `bashrun.KillAll()`; stdio
  children spawn in their own process group, and the SDK terminates them
  (stdin close → SIGTERM → SIGKILL after 3s).

Polish (the "never stuck, always know why" pass):

- **Fail-fast calls** — a call to a failed/disabled server returns instantly
  with an actionable message (`/mcp <name> reconnect|enable`); a
  still-connecting server caps the wait at a 5s grace then returns "retry in
  a moment". No turn parks on a 30s startup timeout.
- **Did-you-mean** — `tools.Suggester` (installed by `Agent.SetMCPTools`)
  runs an early-exit Levenshtein over live tool names, so a stale/typo'd
  `mcp__` call gets `did you mean mcp__docs__greet?` instead of a dead end.
- **First-settle notes** — each server's first settle lands one transcript
  line (`⚡ mcp: docs ready (4 tools)` / `✗ mcp: x failed: …`); later
  transitions stay quiet.
- **Auto-reconnect** — a dropped session retries in the background with
  backoff (1s/2s/4s, cap 3), guarded against close/disable/dupes; manual
  `/mcp reconnect` stays unlimited.
- **Server instructions** — initialize-result instructions render into an
  `<mcp_instructions>` block appended to the system prompt every turn
  (alongside skills), tracking live sessions.
- **`whip mcp test <name>`** — the doctor: connect + list + timing + tool
  names, stderr tail on failure, non-zero exit — CI-checkable `.mcp.json`.

Tests: `config_test.go` (claude/codex parsing incl. a real-world codex
config, merge precedence, discovery errors, tool-name round-trips, import
policy filtering — blocked-in-`Blocked`, exclude-beats-only, whip-name
shadow protection — and the blocked node_repl scenario at the manager
level), `manager_test.go` (connect/call, error-as-output, structured+media
flattening, dead-server degradation, reconnect, parallel calls under `-race`,
ctx cancel mid-connect), `loop_test.go` (model→MCP→model round trip against
a fake provider; stale def on a dead server returns `"Error: …"` and the turn
completes), `selfhost_test.go` (`whip mcp serve` end-to-end, gated on
`WHIP_TEST_SELFHOST=1`), `tui/mcp_test.go` (status view incl. blocked rows,
toggle persistence round-trip, enable-on-blocked refusal),
`config/config_test.go` (mcpImport JSONC round-trip, clobber recovery
preserving the block), `cmd/whip/mcp_import_test.go` (import dry-run vs
apply, idempotence, blocked servers never imported).

## Process safety

`internal/tools/bashrun/bashrun.go` — every command the agent runs is tracked
in a process registry (`track`/`untrack`). On exit (`tui.Run` returning — quit,
`/quit`, or a signal), `KillAll()` SIGKILLs every tracked **process group** and
waits briefly for reaping, so an agent-started server or watcher never outlives
whip.

The non-interactive path captures via explicit `StdoutPipe`/`StderrPipe` and
closes the read ends the moment the process exits, so a detached grandchild
(`nohup`, `sleep 30 &`, a daemonized server) holding the write end can't hang
the agent on pipe EOF. The interactive path runs in a PTY for sudo/ssh-style
prompts, killed after 15s of no input.

Tests: `killall_test.go` — `TestKillAllReapsChildren` (kills a live `sleep 60`),
`TestBackgroundGrandchildDoesNotHang`.

## Update check

`internal/update/update.go` — on interactive startup `main` fires
`update.Check(version)` in a goroutine, concurrent with the trust prompt and
agent setup, so its ~1 RTT is usually free: when the check wins, the notice
shows in that very startup report; when startup wins, the recorded notice is
durable and shows next launch.

The check reads `~/.whip/update.json` first and skips the network when a
notice is pending for a release not yet installed (never nags twice about the
same version) or the last check is under 24h old. Otherwise it GETs
`api.github.com/.../releases/latest` (2s timeout, `gh` token / `GH_TOKEN`
auth mirroring `install.sh` while the repo is private), and a strictly newer
semver (prereleases sort before their release) is written to the notice file
atomically (tmp+rename). The startup report shows `update available: vX.Y.Z
(run: whip update)`; a successful `whip update` calls `update.Acknowledge()`
so an installed release stops nagging — and a user who updates out of band
(curl|sh) has the now-stale notice cleared on next launch, so checks resume.
`dev` builds never check. Every failure — offline, rate-limited, corrupt
notice — is silent by design: a version check must never break startup.

Tests: `internal/update/update_test.go` (`TestCheckNewerRelease`,
`TestCheckSkips` — pending notice / fresh TTL / dev build never fetch,
`TestCheckStaleTTLRefetches`, `TestCheckOutOfBandUpdate` clears the stale
notice of a curl|sh updater, `TestCheckFetchFailure` still records the
attempt, `TestCheckCorruptNotice`, `TestNewer`, `TestPendingAndAcknowledge`);
`tui/startup_report_test.go` (`TestStartupReportUpdateNotice`).

## LSP diagnostics

`internal/lsp/` — a stdlib-only LSP client over stdio (JSON-RPC +
`Content-Length` framing; no new dependencies) that feeds language-server
diagnostics back into the model's `write`/`edit` tool results, so the model
sees and fixes breakage in the same turn instead of spending a `go build`
round-trip. Ported from opencode's diagnostics flow
(`packages/opencode/src/lsp/`, research in
`docs/learnings/other-harnesses/opencode/lsp.md`) with two widenings:
sibling-file errors (opencode renders only the touched file) and wait-free
wakeup (a per-file channel close instead of polling timeouts).

- **Tool output** — after a successful `write`/`edit`, the tool result gains
  a `<diagnostics file="…">ERROR [l:c] msg</diagnostics>` block (format
  ported verbatim from opencode's `lsp/diagnostic.ts`): errors+warnings for
  the edited file (max 20), errors-only for up to 5 sibling files in the
  same directory, with a "this edit introduced errors in other files" note.
  Injection is via the package hook `tools.LSP` (same pattern as
  `tools.InteractiveBash`); nil hook = unchanged output.
- **Manager** (`manager.go`) — the registry is data: `gopls` built-in (root =
  nearest `go.work`/`go.mod`/`go.sum`, found by walking up from the file);
  the `"lsp"` block in `~/.whip/config.json` (same shape as the `mcp`
  block: `command`, `extensions`, `rootMarkers`, `env`, `enabled`) adds
  servers or disables the built-in. Servers spawn lazily on first covered
  file touch; concurrent touches dedup through a close-to-broadcast channel,
  failed spawns (binary not on PATH, initialize error) are remembered per
  (server, root) so a broken server is a permanent no-op, never a retry
  storm. The wait for diagnostics is capped at 1.5s and honors the tool
  call's ctx (ctrl+c cancels); timeout = no block appended, the tool result
  is never delayed further or failed.
- **Client** (`client.go`) — one reader goroutine parses frames and routes
  responses by id into cap-1 pending channels; writes funnel through a
  buffered channel drained by one writer goroutine (no locks). Server→client
  requests (`window/workDoneProgress/create`, `workspace/configuration`,
  `client/registerCapability`) get a null-result ack, same as opencode.
  Shutdown is polite `shutdown`/`exit` then SIGKILL of the process group;
  `Manager.Close()` runs next to `mcpMgr.Close()` on exit.
- **TUI** — `/lsp` prints per-server rows (`● connected (root: …)` /
  `○ not started` / `✗ err`); the manager is built in the same startup block
  as MCP and installed on `tools.LSP`.

Tests: `internal/lsp/client_test.go` (frame parsing incl. split/garbage,
request routing, ctx-cancel on unanswered requests, server-request acks),
`manager_test.go` (in-process fake LSP server over pipes — no real gopls:
edited-file blocks, sibling blocks, didOpen→didChange versioning, timeout,
cancel, broken-spawn caching, config merge, root walk),
`concurrency_test.go` (spawn dedup across 8 concurrent touches, parallel
waiter wake with goroutine-leak check, publish-before-wait interleaving),
`internal/tools/lsp_test.go` (block appended to write/edit output, nil hook,
failure never fails the tool), `internal/agent/lsp_test.go`
(`TestLSPDiagnosticsReachModel`: fake provider receives the diagnostics
block in the tool result on the next call), `internal/tui/lsp_test.go`
(`/lsp` status view).

Out of scope (breadcrumbs in `.ai-docs/plans/lsp-diagnostics/README.md`):
@-mention symbol-range expansion (Linear INF-4991), read warm-up
(opencode forks `touchFile` on read — cut; revisit if first-edit latency
annoys), pull diagnostics, navigation tools (definition/references/hover),
auto-installing servers.

## Skills

`internal/skills/skills.go` — scans `.agents/skills/*/SKILL.md` (project) and
`~/.whip/skills/` (user) for a name+description frontmatter block, injected
into the system prompt as an `<available_skills>` catalog in the Agent Skills
spec format (`<skill><name>/<description>/<location>`, XML-escaped). The model
reads a SKILL.md with its own read tool when relevant. Skills re-index every
turn, so new ones load without restarting.

**Spec compliance** (agentskills.io, matching pi's `core/skills.ts`): name
validated (≤64 chars, lowercase a-z/0-9/hyphens, no leading/trailing/double
hyphens), description ≤1024 chars (a *validity* ceiling, not a prompt budget),
`disable-model-invocation: true` skills excluded from the catalog but still
invocable via `$name`. Violations load with a `Warning` (surfaced in the
startup report), never silently disappear. Tests: `skills/spec_test.go`.

**`/context-doctor` (alias `/context-doctor`)** — fresh-session context audit: every
automatic injection source with its estimated token cost (base system prompt,
skills block with the 5 biggest offenders, per-server MCP tool schemas, server
instructions, built-in tool schemas, conversation history, and actual session
spend once requests have run), a TOTAL line, and trim pointers. Built for
users arriving from heavier harnesses whose first call silently carries tens
of thousands of tokens of skill/MCP bloat. Tests: `tui/context_doctor_test.go`.

**`/report`** — bug-report bundle for terminal/rendering issues: one
transcript block pairing a clickable OSC 8 link (opens a prefilled
`context-labs/whip` issue with a What-happened/Expected skeleton + the
environment bundle in a fenced block) with the same bundle as a
copy-pastable fenced snippet. Strict env whitelist (whip version/model/
provider, theme + *how it was detected* — captured at startup, never
re-queried, mouse, session id; TERM/TERM_PROGRAM/COLORTERM/COLORFGBG, tmux +
`tmux -V`, SHELL, locale, window size, ssh flag; OS/arch, uname, sw_vers, Go
version) — no secrets, no conversation content. Nothing is submitted or
persisted: the user clicks or pastes. Version is plumbed from `main.version`
via `tui.Version`. Tests: `tui/report_cmd_test.go` (whitelist, no-secret
leak, issue URL round-trip, fenced snippet, busy-safe).

**Startup resource report** — first paint names what whip loaded: `skills: N
loaded`, one `⚠` line per degraded skill (description over maxDesc → truncated
in the prompt) or unparseable SKILL.md (pi's [Skill conflicts] lesson — a
broken skill is never silent), and one `mcp:` line with per-server status
glyphs (`✓ N tools` / `✗` / `○ disabled` / `◌ connecting`). Skipped on resume.
Tests: `tui/startup_report_test.go` (warnings, MCP glyphs, silence when empty).

Installed: the `golang-*` skill set plus `i-have-adhd` (output-shaping for ADHD
readers; invoke with `/i-have-adhd`, off with "stop adhd mode").

## Browser automation

`internal/browser/` + `internal/tools/browser*.go` — a native Go browser
subsystem (go-rod/rod; no Python/Node) exposed to the model as one
code-shaped tool, `browser_exec`. Design: docs/learnings/browser-use-integration.md §5b.

- **Three modes** (`config.Browser.Mode`): `live` attaches to the user's
  running Chromium-family browser (their real cookies/sessions) via
  DevToolsActivePort profile scan + SingletonLock liveness + `/json/version`
  → WS resolution (Chrome 147+ 404 falls back to the file's WS path after
  the path proves it answers a WebSocket upgrade; Chrome 144+ 403 surfaces
  as `ErrPermissionBlocked` with user-actionable text). `dedicated`
  launches a separate Chrome with a whip-owned
  `~/.whip/browser/dedicated-profile` (no popups); `headless` is the same
  without a window. Explicit endpoints win: `WHIP_CDP_WS`/`WHIP_CDP_URL`
  env or `browser.cdpUrl` config.
- **Auto-launch fallback** (hermes `/browser connect` model, ported in
  `.ai-docs/plans/browser-auto-launch`): when live discovery finds no
  debuggable browser (`ErrNoLiveBrowser` — including a non-Chrome process
  squatting the debug port), `Open`/`openRod` silently launch the dedicated
  Chrome for that session instead of dead-ending the tool call. Discovery
  probes both loopbacks (127.0.0.1 + [::1]) and verifies `/json/version`'s
  `Browser` field, so a squatter on 9222 no longer resolves to a bogus WS
  URL — it triggers the fallback. A still-running whip Chrome is reattached
  via `DiscoverWSForProfile` (its profile's DevToolsActivePort) rather than
  re-launched; `Browser.Obtained()` reports live/launched/reattached, and
  `Session.Do` prepends a one-line notice to the first tool output when a
  live session fell back (the model relays which browser it's driving).
  `Close` detaches (severs the CDP socket via `detach.go`, no Browser.close)
  for live/reattached/dedicated so a reattach target survives; headless
  still kills its process.
- **Extension relay** (`config.Browser.Mode: "extension"`,
  `internal/browser/extrelay/`): drives the user's real, logged-in Chrome
  tab — the only way onto the default profile on Chrome ≥ 136, where direct
  CDP is blocked. The unpacked MV3 extension (`extension/manifest.json` +
  `background.js`, go:embed'd) holds an outbound WebSocket to a loopback
  relay (`relay.go`, gobwas/ws — already vendored via rod, no new deps) and
  pipes raw CDP through `chrome.debugger` on the pinned tab. The relay
  synthesizes the few browser-level `Target.*` responses rod's attach needs
  (one attached page target) and tunnels everything else verbatim, so the
  existing rod Backend is reused unchanged (navigate/click/type/screenshot/
  AX tree). Security: loopback only, per-process bearer token in
  `~/.whip/browser/extension/relay.json` (0600), and only a tab the user
  pinned by clicking the extension icon is drivable. Accepted trade-off:
  Chrome shows a "whip is debugging this browser" infobar while pinned.
  Setup: `whip browser install` writes the extension + relay.json, mints
  the token, and opens `chrome://extensions` + the folder (the 3 manual
  clicks — Developer mode → Load unpacked → select folder — are on the user;
  Chrome forbids programmatic install).
- **One tool, per hermes's benchmark** (36/36 task success at ~60% fewer
  schema tokens vs a 12-tool granular set): the `code` argument is a line/
  semicolon-separated helper-call program (`goto`, `js`, `click`, `type`,
  `press`, `fill`, `scroll`, `waitLoad`, `waitFor`, `ax`+`box` for
  AX-tree→coordinate workflows, `tabs`/`useTab`, `upload`, `dialog`,
  `screenshot`, `info`, `print`) — parsed by a ~200-line quote-aware
  mini-interpreter (`browser_lang.go`), no eval surface in whip.
- **Named sessions** (`session: "<name>"`, prefix `<mode>:` to override the
  default mode per session): one lazily-opened browser per (mode, name),
  calls serialized through a 1-capacity channel semaphore (the filelocks
  idiom), dead connections reopened once (stale-tab recovery).
- **Safety floor** (`safety.go`, ported from hermes's url_safety.py):
  cloud-metadata endpoints (169.254.169.254, metadata.google.internal, ECS)
  blocked unconditionally on every `goto` in every mode, all DNS answers
  checked, fail-closed on resolution errors; private/LAN addresses blocked
  on dedicated/headless unless `browser.allowPrivateUrls`; post-action URL
  recheck neutralizes the page to about:blank when JS navigation laundered
  the target.
- **Vision loop**: `screenshot()` returns a JPEG (≤1568px, quality 80, via
  CDP clip-scale); when the model has vision, the TUI's `ScreenshotSink`
  steers the image into the conversation as a multimodal user message
  (`Agent.SteerImages` → `pendingSteer.parts`), so the model inspects it
  natively on the next request — no temp-file dance.
- **UX**: the TUI tool row shows the code's first `# comment` as a
  plain-language step label (`tui/browser.go browserStepLabel`) instead of
  raw JSON; `browser.enabled: false` removes the tool.
- **Screencast hook (follow-up, not shipped)**: because the driver is
  in-process, `Page.startScreencast` frames could stream to a TUI pane for
  a live view of the agent's page — impossible through the CLI-subprocess
  design. The seam is `internal/browser.Backend` (add a
  `Screencast(ctx, func(frame []byte))` method) feeding a new transcript
  block type; no agent-loop changes needed.

Tests: `internal/browser/browser_test.go` (DevToolsActivePort parsing,
profile scan with fake dirs, /json/version + 404-upgrade-fallback +
403-permission discovery, dual-stack portLive, squatter rejection,
per-profile reattach discovery, fallback notice once-per-session, SSRF
floor, session/mode selection), `e2e_test.go` (real Chrome × all three
modes — cookie round-trip, AX-tree→click, screenshot JPEG,
dedicated-profile isolation, live-attach survival after Close, live→launched
fallback, dedicated reattach-no-duplicate),
`internal/browser/extrelay/relay_test.go` (token auth, CDP tunnel
round-trip, Target.* synthesis, no-tab error, disconnect-detach) +
`rod_e2e_test.go` (a real rod.Browser drives attach + Eval through the relay
against a fake extension — proves the tunnel + Target synth end-to-end),
`internal/tools/browser_lang_test.go` (parser), `schema_test.go` (all
built-in tool schemas parse — ratchet for the request-corrupting malformed
schema class), `browser_e2e_test.go` (tool-level E2E),
`internal/agent/browser_test.go` (fake-provider loop: model calls
browser_exec, page title reaches the model).

Environment note (this dev box): Playwright's Chromium + unpacked Ubuntu
debs under `/tmp/chromelibs` (LD_LIBRARY_PATH) drive the E2E tests; tests
skip cleanly without a Chromium binary. Form-control text input
(`<input>`/textarea) wedges the renderer in that sandboxed build — an
environment quirk, not a rod/whip bug; verified on real Chrome.

The browser-use CLI-over-MCP escape hatch remains available via config for
anyone wanting the Python ecosystem (§4 option B).

## Computer-use (macOS)

`internal/computer/` + `internal/tools/computer.go` — `computer_exec` drives
the user's Mac via AppleScript (osascript), with the already-open Chrome as
the flagship path: their real tabs and logins, zero CDP setup. Design and the
codex/mack borrow rationale: .ai-docs/plans/computer-use/README.md and
docs/learnings/other-harnesses/codex-computer-use-plugin.md (dissected from
the on-disk driver).

- **Chrome via AppleScript** (`internal/computer/chrome.go`) — the user's
  running Chrome answers AppleScript: active-tab URL/title, every tab of
  every window, goto/new-tab/activate/close/back/reload, and
  `execute javascript` (needs Chrome's View→Developer→"Allow JavaScript
  from Apple Events" toggle; the error surfaces it). Our osascript helper
  fixes mack's flaws: newlines preserved (tab lists stay readable), quotes
  escaped (no injection).
- **Per-app policy** (`policy.go`, ported from codex's computer_use.rs):
  every action targets an app. **Default is allow-all** — users build
  blocklists (`computer.deny` config, or `/computer-use deny <app>`
  in-session), not allowlists. `computer.deny` always wins (config and
  session). `computer.allow` and `computer.defaultDeny: true` restore the
  gated posture for anyone who wants it.
- **Tool shape** — the same helper-call mini-language as browser_exec
  (`internal/tools/browser_lang.go`, now shared): `chrome_state`,
  `chrome_tabs`, `chrome_goto`, `chrome_new_tab`, `chrome_activate`,
  `chrome_close`, `chrome_back`, `chrome_reload`, `chrome_js`,
  `chrome_find`, and the `tell(app, script)` escape hatch. Step-label
  `# comment` convention carried over (the TUI row shows it).
- **Safety**: URLs pass the browser SSRF floor (`browser.CheckURL`) before
  any navigation; login walls → stop and ask (in the tool description).
- macOS-only for now (`computer.Available()` gates on darwin); Linux/Windows
  backends are follow-ups. The AX/CGEvent/ScreenCaptureKit tier (full
  desktop control) is v2 — a signed embedded helper binary extracted to a
  stable path so TCC grants stick.

Tests: `internal/computer/computer_test.go` (quote escaping, policy
allow/deny/session-approval, tab-list parse), `internal/tools/computer_test.go`
(tool gating, policy enforcement, approver flow); the schema ratchet
(`schema_test.go`) now covers computer_exec.
