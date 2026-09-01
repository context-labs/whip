# Features

whip is a minimal coding-agent harness: an interactive bubbletea TUI driving an
LLM tool-use loop (bash / read / write / edit / subagent) with provider-routable
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

### Waiting without polling (`wait` tool)

`internal/agent/wait.go` — the `wait` tool replaces `sleep N && check` loops
(which spend a full LLM turn per poll) with a **harness-owned poller**. The
model names a shell command, an optional `until` regex, an interval (min 2s,
default 10s) and a timeout (default 10m, max 1h); a goroutine re-runs the
command via `bashrun` on the interval with zero model involvement.

Exactly one message re-enters the loop when the wait resolves — never a poll
per check. Delivery routes on whether a turn is in flight (`Agent.TurnRunning`
— an `atomic.Bool` set at `turn()` entry/exit): **busy** → `Steer`, drained at
the next loop boundary like any steered message; **idle** → the registry's
`OnWake` hook, which the TUI installs (`wireWaits`, called from `wireTasks` so
every agent swap re-installs it) to submit a machine-authored turn
(`submitTurn(text, false)`), the opencode/exo wake pattern. A turn that starts
between the `TurnRunning` check and the wake message is caught by the
`waitWakeMsg` handler re-checking `m.busy` and steering instead of
double-submitting. Headless idle (`whip run`, tests) has no loop boundary and
no wake hook, so the message is dropped by design; the busy path is the one
that matters there.

Resolution states (`WaitStatus`): `condition met` (exit 0 and `until` regex
matches, checked immediately on registration and then per interval), `timed
out`, and `command failing` — 3 consecutive non-zero exits strike out the wait
early (hermes' 3-strike lesson) so a broken command doesn't poll for the full
timeout. Delivery is once-only (`atomic.Bool` CAS in `deliver`), `Done` closes
on settle like `BackgroundTask`, and `CancelWait` suppresses a pending
delivery. Waits are live-only: a dead process's waits die with it, and the
registry's `Close` cancels every poller on agent teardown.

The system prompt (`cmd/whip/main.go`) tells the model to prefer `wait` over
`sleep` loops, and the tool's return message restates the no-poll contract.

Tests: `internal/agent/wait_test.go` — `TestWaitConditionMetImmediately`,
`TestWaitUntilRegex`, `TestWaitStrikesOut`, `TestWaitTimeout`,
`TestWaitBusySteersInsteadOfWaking` (busy → Steer, not OnWake),
`TestWaitCancel`, `TestWaitToolRegisters` (def parsing + settle).
`go test -race ./internal/agent` green.

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

Every compaction is visible in the transcript as a pair of notes. The moment
folding begins, `OnCompactStart` renders `◎ compacting N msgs (est. X tok) with
<model>…` so the UI never looks hung during the summary call. When it
completes, `OnCompacted` renders `◎ compacted — summarized N msgs, M kept ·
<model> · $cost (in/out tok) · raw history preserved` — the counts come from
`OnCompact`, the model and spend from `CompactInfo` (a dedicated compaction
route is labeled `<id> @ <host>`), and the cost is priced off the provider
catalog (hidden when the model has no advertised price). The result note
renders even with no session store; the `raw history preserved` suffix appears
only once the event is actually recorded.

### Provider prompt-prefix caching

To cut time-to-first-token on the many sequential turns of an agent loop,
whip stamps `prompt_cache_key` on every request (OpenAI `prompt_cache_key`;
openrouter/xai/azure/mistral honor the same field; providers that don't
recognize it ignore the unknown top-level field). The key is the **session
id**: `Agent.SetSessionID` sets `Client.CacheKey`, so a stable session lets
the provider reuse the cached conversation prefix across turns. Subagents get
a scoped key (`<sessionID>/<taskID>` in `StartBackground`) so their shorter,
churning contexts never disturb the parent's cached prefix and two concurrent
subagents don't collide on the session key. An explicit
`Request.PromptCacheKey` overrides the client's (that's how the subagent
scoping is applied); empty omits the field entirely so providers that would
reject it never see it. The prefix-stability preconditions are already
maintained elsewhere: the system prompt's per-turn memory block sits at the
END of the system message (`prepareTurn`), MCP tools are name-sorted
(`mcp.Manager.Tools`), and the context-decay pass keeps the recent hot window
byte-stable. Anthropic-style `cache_control: ephemeral` breakpoints are out of
scope — whip speaks OpenAI chat-completions uniformly and has no
Anthropic-native consumer for them.

Tests: `internal/llm/cache_test.go` — `TestPromptCacheKeyStampedFromClient`,
`TestPromptCacheKeyRequestOverridesClient`, `TestConsecutiveRequestsSharePrefix`
(the prefix-cache contract: turn N's messages are a byte-identical prefix of
turn N+1's, same key on both).

Commands: `/compact` (compact now), `/compact <model> [provider]` (pick the
summarizer), `/compact off` (restore the built-in default). The palette's
"Compaction model" panel lists every configured model behind a
"default (…)" row that restores the default; "Compaction level" steps the
threshold ←/→.

Tests: `agent_test.go` — `TestTurnAutoCompactsOnContextLimit`,
`TestCompactDoesNotLoopOnRepeatedContextLimit`, `TestCompactKeepsToolCallPair`,
`TestProactiveCompactAtFiftyPercent`, `TestCompactThresholdExplicitOverride`,
`TestCompactionEventsCarryModelAndUsage` (start fires before the summary call;
done carries the model + usage, proven with a tiny context limit),
`TestCompactionInfoLabelsDedicatedRoute`, `TestUsageAccumulates`;
`compact_cmd_test.go` —
`TestCompactModelEmptyResolvesDefault`, `TestCompactModelDefaultFallsBack`,
`TestCompactThresholdFor`, `TestSetCompactPct`; `compact_vis_test.go` —
`TestCompactionVisibleInTranscript` (the start+result notes render through the
Update loop with a small compaction limit),
`TestCompactionNotesRenderInOrder`, `TestCompactionResultShowsRealCounts`;
`palette_test.go` —
`TestPaletteCompactPanelAppliesInPlace`,
`TestPaletteCompactPanelDefaultRowRestores`, `TestPaletteCompactionLevelSteps`.

### Background subagents

`internal/agent/background.go` — the `subagent` tool with `background: true` launches a
subagent that runs **concurrently with the parent** instead of blocking the
turn. This is the channel-native port of opencode's `background-job.ts`
registry.

Each task is a `BackgroundTask` with a `Done chan struct{}`. When the subagent
settles, the registry `settle()`s and **closes `Done` once** — closing a
channel broadcasts to every waiter at once, so the tool caller, the TUI, and
`/subagents` (alias `/tasks`) all wake together with no per-waiter state (opencode needs a per-job
`Deferred` for the same thing). On settle the report fans back into the parent
as a **steered message**, so the model sees it on the next loop boundary.

- `Tasks().List()` / `Get(id)` / `Cancel(id)` — registry snapshot + cancel.
- `Tasks().OnChange` — the TUI installs a callback that sends a message to
  redraw live. `Tasks().OnRecord` — a second hook the TUI uses to upsert the
  task into the session store on start and settle.
- `/subagents` (alias `/tasks`) lists running/done subagents with report previews; a `⚙ N sub`
  header badge shows the running count. The persistent dock strip renders
  **below the input** (above the status line), so focus follows the cursor's
  geometry: ↓ on an empty input (or ctrl+t) moves focus into the list, ↑ past
  its top row — or simply typing — hands focus back, and esc is never
  consumed by the dock (it stays the interrupt/rewind key). The strip is
  mouse-clickable: `dockTop()` maps screen rows to task rows, skipping the
  focused hint row (`dockSkip`) so a click opens the row actually clicked.
- **Persisted across resume.** The session store's `tasks` table records
  every start/settle; `resume()` seeds the registry via `RestoreTask`
  (settled, `Done` pre-closed, marked `Restored`). A row still `running` on
  disk means the subagent died with the last process exit, so it comes back
  as `error` — "interrupted — whip exited". Restored tasks are history:
  `/subagents` (alias `/tasks`) lists them with a `(restored)` marker; the dock never shows them.
  The dock itself shows running tasks plus ones settled within a one-minute
  grace window (`dockSettledGrace`) — long enough to notice the ✓, then the
  strip cleans itself.
- **Full transcript on open.** The registry journals every emitted event per
  task (`taskJournal`, byte-capped at 128KB, drop-oldest with a "[earlier
  output dropped]" marker). `openTask` replays the journal and subscribes to
  the live stream as ONE atomic call (`SubscribeWithJournal`), so a detail
  view opened mid-run or after settle shows the complete transcript — tool
  calls, steers, and all — instead of only what streams in after attach.
  Replay and live rendering share `renderTaskEvent` (internal/tui/tasks.go)
  so the two paths can't drift in format. Tests: `journal_test.go`
  (recording, delta coalescing, overflow truncation, atomicity under
  concurrent emit, survive-settle/clear lifecycle),
  `TestTaskViewReplaysJournal`, `TestRunningTaskViewReplaysThenStreams`.

- **Full transcript persisted.** When a background subagent settles, its whole
  conversation is saved as its own attributed session
  (`Store.SaveSubagentTranscript`, id `task-<parentID>-<taskID>` — the
  `task-` prefix avoids a prefix-collision with the parent id in `Load`),
  with `forked_from` = the parent session and `task_id` = the task. A
  follow-up turn on the settled subagent re-saves the transcript
  (`refreshTranscript` after `FollowupTask`). On resume, opening a restored
  task replays the persisted transcript read-only (`renderTranscript`) instead
  of showing only the bare report — a crashed process no longer loses the
  completed work. Tests: `session_test.go`
  `TestSubagentTranscriptRoundTrip` (attribution + follow-up re-save + no-op
  without a parent), `tasks_test.go`
  `TestRestoredTaskReplaysPersistedTranscript` (kill → resume → open shows the
  full transcript).

Background tasks use a context **not** tied to the current turn — they outlive
it by design. Cancelling a task cancels its subagent's turn. `settle()`
notifies/persists **before** closing `Done`, so a waiter woken by the close
always sees the recorded final state.

**Subagent model routing** (`internal/agent/subagent.go` `SubModel`,
`internal/tui/taskmodel.go`): subagents default to the cheap fast
`deepseek-v4-flash-0731` route (`config.DefaultTaskModel`, same default as
compaction); config `taskModel`/`taskProvider` pins a different one —
ctrl+p › Subagent model sets it and persists to the global config (the
picker's "default" row restores the built-in default); the main
model overrides per call via the `subagent` tool's optional `model`/`provider`
params. Resolution chain: taskModel → built-in default → catalog id ending in
`/<default>` (openrouter-style vendor prefixes) → silently fall back to the
session model. The agent stays config-free: the TUI/`whip run` inject a
`ResolveModel` closure over a `cfg.Snapshot()` (the resolver runs on tool
worker goroutines while `/auth` mutates live config on the UI goroutine).

**User-spawned subagents**: `/subagent [-m model[@provider]] <prompt>` starts a
background task by hand — it runs mid-turn too (listed with the
works-while-busy commands), so the LLM isn't the only driver.

**Foreground fan-out and naming.** A `subagent` call without `background`
blocks the turn on the report; emitting several in one assistant message runs
them concurrently and returns every report together (`runTools` already
parallelizes a tool batch). The tool description tells the model this is how to
explore in parallel, reserving `background:true` for fire-and-forget. Two
guardrails keep delegation legible and cheap:

- A foreground report is capped at `subagentReportCap` bytes before it lands in
  the parent's context (`subagent.go` `capReport`), so one long investigation
  can't swamp the parent window. The subagent's own context is uncapped — only
  what the parent ingests is bounded.
- Transcript rows surface the task's `description` (queued + running rows via
  `queuedSubject`/`toolSubject`, not the raw JSON args), number a parallel
  batch `1/N` (`batchSuffix`), and background task ids are description slugs —
  `survey-context-in-pi-3`, not `sub-1` (`taskSlug`) — so `/subagents`, the ⚙
  badge, and steer messages name the work.

Tests: `TestForegroundReportCapped`, `TestForegroundReportUnderCapPassesThrough`,
`TestTaskSlug`, `TestStartBackgroundSlugID` (agent); `TestSubagentBatchNumbered`,
`TestSubagentSingletonNotNumbered`, `TestBatchSuffixPerToolName` (tui).

**Chat with a subagent** — a task IS a session. The retained subagent lives on
its `BackgroundTask`; the detail view (enter from the dock) has a chat input:

- while the task **runs**, enter **steers** it (`Agent.SteerTask` → the
  child's own `pendingSteer` queue — the parent→child pipe reuses the existing
  steer primitive, no new synchronization); the model gets the same power via
  the `subagent_steer` tool (`{id, message}`).
- once it **settles**, enter runs **follow-up turns** on its preserved context
  (`Agent.FollowupTask`) — status/report/`Done` stay as they settled, usage
  rolls into the parent session. ctrl+x cancels the running task or the
  in-flight follow-up. Follow-up chats are live-only by design (not
  persisted); restored tasks are read-only — their process died.
  `ClearSettled(keep…)` protects a task whose pane is open from the new-turn
  dock sweep.

Tests: `TestBackgroundTaskDeliversReport`, `TestBackgroundTaskBroadcastsToManyWaiters`
(8 waiters all woken by one channel close), `TestBackgroundTaskCancel`;
persistence: `session.TestTaskRoundTrip`, `TestRestoreTaskSettledAndVisible`,
`TestResumeRestoresTasks`, `TestTaskPersistsOnStartAndSettle`;
spawn feedback: `TestBackgroundWorktreeRegistersBeforeProvisioning` (the task
registers — dock row + ⚙ badge — before the synchronous worktree provision
runs, and the worktree path is baked into the subagent's initial prompt so
it's delivered deterministically with the turn, never as a post-spawn steer a
fast-settling task would lose);
dock click hit-testing: `TestDockClickOpensClickedRow`,
`TestDockClickIgnoredWhilePaletteOpen`; routing: `submodel_test.go` —
`TestTaskModelOverride`, `TestTaskDefaultRoutesSubagents`,
`TestTaskModelOverrideErrors`; chat: `TestSteerTaskReachesRunningSubagent`,
`TestFollowupTaskChatsOnRetainedContext`, `TestClearSettledKeep`;
TUI: `taskmodel_test.go` — `TestTaskDefaultForResolvesDefault`,
`TestTaskDefaultForFallbacks`, `TestTaskDefaultForCatalogSuffix`,
`TestTaskCommandSpawns`, `TestTaskViewChat`, `TestTaskViewRestoredReadOnly`,
`TestTaskViewCtrlXCancels`.

## Models & providers

`internal/config/config.go`, `internal/config/catalog.go` — models route to
providers; the provider's `GET /models` is the source of truth for
capabilities. Two distinct limits, both honored:

- **Context window (input)** — `Model.Context` (legacy `maxTokens` still
  parses via `ContextWindow()`), overridden by the provider's
  `context_length`. Drives the header's `% ctx` and proactive compaction.
- **Output cap** — `Model.MaxOut`, else the provider's `max_completion_tokens`,
  else the context window. Sent as the request's `max_tokens`.

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
`Client.OnRetry` hook. Tests: `llm/retry_test.go`.

`internal/tui/setup.go` — the first-run setup wizard. When `whip` starts with
no `~/.whip/config.json` AND no `~/.whip/setup.done` marker
(`cmd/whip/main.go` checks `config.Exists()`/`config.SetupDone()` before
`config.Load()` creates the config, and threads the flag into `tui.Run`), a
short plain-terminal wizard runs after the trust gate and before bubbletea
takes the terminal — the `checkTrust` pattern, one question per line. The
marker matters: a subcommand's `config.Load` (`whip auth`/`run`/`mcp`) writes
the default config without running the wizard, and without the marker that
would permanently consume the first run. `tui.Run` shares one `bufio.Reader`
between the trust gate and the wizard — a fresh reader per prompt would lose
the other's buffered read-ahead when a paste answers several prompts at once.

1. **Provider** — `[1] Inference.net (browser sign-in) · [2] OpenRouter
   (paste key) · [3] skip`, Enter = skip. `1` runs the device login through
   `inferencenet.CompleteLogin` with a numbered-list `ChooseFunc` standing in
   for the TUI picker, then mints the machine key and upserts the provider.
   `2` validates the pasted key against OpenRouter's `/models` (the
   `openRouterValidate` seam — an httptest stub in tests) before
   `UpsertOpenRouter` writes anything. A failed sign-in or bad paste never
   wedges install: the wizard prints the error and continues.
2. **Thinking tokens** — `[Y/n]`, Enter = show (today's default; "n" writes
   `"thinking": false`).
3. **MCP imports** — claude (`~/.claude.json`, `.mcp.json`) and codex
   (`~/.codex/config.toml`), both `[y/N]` — Enter = **no**: a first run that
   only presses Enter imports nothing. The answers always land in the
   `mcpImport` block as explicit `enabled: bool`s so the install has a
   record, and ctrl+p → MCPs flips them later.

Non-terminal stdin (piped runs, `whip run`, ACP, tests) skips the wizard
silently and keeps the shipped defaults — headless launches never see a
prompt. All writes go through the guarded `Config.Save`. Tests:
`tui/setup_test.go` (`askYN` parsing incl. EOF/garbage fallback,
Enter-through opt-out, opt-in, thinking-off persistence, OpenRouter good/bad
key via the injected validator).

`cmd/whip/auth.go`, `internal/config/openrouter.go`,
`internal/tui/auth_cmd.go` — one-command provider onboarding, first (and
currently only) for OpenRouter. `whip auth openrouter [--env] [<key>]` takes
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
so re-authing fixes a 401 without a `/model` round-trip. Tests:
`config/openrouter_test.go` (upsert modes, idempotence, `TrimKey`),
`cmd/whip/auth_test.go` (httptest fake OpenRouter — good key wires provider
+ catalog + makes catalog models resolvable, bad key writes nothing,
re-auth keeps other providers/models), `tui/auth_cmd_test.go` (usage,
masked prompt open/cancel, good/bad result, live-session rekey).

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
- **Collapsible tool results, claude-style.** A completed call renders as a
  `● Verb(subject)` header (`internal/tui/toolrow.go` — `Update(path)`,
  `Bash(cmd)`, `Subagent(description)`; the collapse never loses what the call
  was about) over its result in a `blockTool` block: first line under a `⎿`
  marker, collapsed to 5 lines with a `… +N lines` hint, red when the result
  is an error. **Write/edit results render their diff**: the tools emit
  line-numbered fenced diffs (`editDiff` with a startLine; `write` diffs
  overwrites against the old content from line 1), and the TUI shows
  `⎿ Added N lines, removed M lines` over red/green full-width bands with
  absolute line numbers — the diff IS the collapsed view (capped at 30 rows),
  and trailing LSP diagnostics stay visible under it. Resumed sessions
  re-render call headers and diffs from the stored messages. `ctrl+e` toggles
  the most recent; clicking a block expands/collapses it (each block tracks
  its rendered line range `y0`/`y1` so the click row maps through the
  viewport offset). Blocks re-render at the current width on terminal resize.
  Tests: `tool_expand_test.go`, `resize_test.go`, `toolrow_test.go`
  (headers, extract/counts, colored diff render, preview cap, resume),
  `tools_test.go` — `TestEditDiffLineNumbers`, `TestWriteToolDiffOnOverwrite`.
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
- **Typing steers a turn that's only waiting on subagents.** When a turn is
  running but its only in-flight tool calls are subagents
  (`Agent.WaitingOnSubagents` — the agent tracks in-flight tool names in
  `runTools`), typed input routes to `Agent.Steer` instead of the busy queue:
  it reaches the model at the next loop boundary as a mid-turn correction
  rather than queueing behind the whole turn (waiting on a subagent isn't real
  work the message would interrupt). Any other in-flight tool (a bash, an
  edit) keeps the queue behavior. The input placeholder reflects the routing
  live (`syncInputPlaceholder`, consulted in `View`): "waiting on subagents —
  type to steer this turn" vs "busy — type to queue". Tests:
  `internal/agent/busysteer_test.go` (`TestInFlightToolsTracking`,
  `TestWaitingOnSubagentsGating`,
  `TestWaitingOnSubagentsDuringForegroundSubagent` — a real turn blocked on a
  live foreground subagent reports waiting, then flips false),
  `internal/tui/queue_test.go` (`TestBusyPlaceholderReflectsRouting`).
- **Settings commands run mid-turn.** `/theme`, `/mouse`, `/effort`, `/subagents` (alias `/tasks`),
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
the redo stack up to the picked point into the copy. **Mid-turn** (`busyFork`)
the copy of the stored rows lands immediately — the confirmation prints the
`whip --resume <id>` line so the clone can be opened in another whip process
right away — and the switch defers to `turnDoneMsg` (`pendingForkID` →
`switchToForked`), since the turn goroutine owns `Agent.Messages` and the
session id until then; the original keeps the finished turn, queued messages
are dropped (they named the old conversation), and the goal carries over via
the copy's stamped row. **`/rename [title]`** retitles the current session
(`Store.SetTitle`); bare opens the same inline prompt prefilled with the
current title. Both prompts stash and restore any in-progress draft. /rename
refuses to run mid-turn; /fork never queues. Palette entries:
"Rewind conversation", "Fork session", "Rename session" under Session.

Tests: `rewind_test.go` — double-esc opens/cancels, busy esc still
interrupts, truncation + input restore + DB rows deleted, forward travel,
partial-rewind DB prefix, tool-call-pair safety, stale esc-arm across modal
dismiss, draft preservation, resume-after-rewind. `fork_test.go` (session) —
prefix/full copy, fork-title numbering, rename, DeleteFrom. `fork_test.go`
(tui) — fork with arg, bare prompt suggestion + cancel, fork from the picker,
fork while rewound into the redo stack, busy fork (immediate copy + deferred
switch + double-fork refusal + nothing-persisted case), rename both paths.

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
- **Palette panel** — ctrl+p → "MCPs" drills into a sub-panel (the `panelMCP`
  kind) with two source-toggle rows (`Import Claude MCPs`, `Import Codex
  MCPs`) then one row per live or policy-blocked server. enter/←/→ toggles:
  source rows go through `mcpSetImport`, server rows through the same
  `mcpSetEnabled` as `/mcp`. Toggling rebuilds the rows in place so the
  checkbox flips visibly. A source with `only`/`exclude` filters notes
  "(name filters set — edit config)" instead of pretending the toggle is
  complete.
- **Live source toggles, no restart** — `mcpSetImport` persists the gate then
  applies it in place: off calls `Manager.RemoveServers` (sessions close, the
  gen bump stops auto-reconnect and stale watchers, tools leave
  `Agent.SetMCPTools` on the next `fireOnChange`); on calls
  `Manager.AddServers` (lazy-with-kickoff connects like startup, skipped for
  names already live so whip-owned shadow entries win). Both refresh
  `SetBlocked` so `/mcp` stays accurate. With no manager yet (nothing
  configured), enabling builds one on the spot so imports can be switched on
  from zero. Every `Manager.servers` map read (Tools/Statuses/Config/Disable/
  Enable/Reconnect/InstructionsBlock/Close) takes `onChangeMu` — the same
  lock that guards AddServers/RemoveServers mutations — so a mid-turn toggle
  never races the slice a running request reads. Source attribution matches
  BOTH shapes: `Filtered.Sources` uses short labels (`"codex"`,
  `".mcp.json"`) while the live manager's `Statuses()` carry the absolute
  discovery path — `isSource` in `tui/mcp.go` normalizes both, and a
  remove-mid-connect is guarded by `connect`'s `startGen`/`stillOurs` check
  so a toggled-off server's in-flight connect can't resurrect it.
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
`tui/mcp_panel_test.go` (row assembly, palette-driven source toggle off →
server gone + gate persisted + whip-owned entries untouched, enable → live
re-discovery, only/exclude filters survive a toggle, panel row replaces the
old run-row), `manager_live_test.go` (`AddServers` connects a late server,
duplicate add no-ops, `RemoveServers` drops tools immediately, remove-
while-connecting + concurrent readers race-clean, stale tool closure fails
as an error string, remove-then-add reconnects),
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

`internal/skills/skills.go` — scans `.agents/skills/*/SKILL.md` (project),
`~/.whip/skills/`, and `~/.agents/skills/` (user) for a name+description
frontmatter block, injected into the system prompt as an `<available_skills>`
catalog in the Agent Skills spec format (`<skill><name>/<description>/<location>`,
XML-escaped). The model reads a SKILL.md with its own read tool when relevant.
Skills re-index every turn, so new ones load without restarting.

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

## `whip up <prompt>` — start the TUI with a first-turn prompt from argv

`whip up <words...>` (`cmd/whip/main.go`) joins every argv token after `up`
with spaces and opens the interactive TUI with that text submitted as the
first user turn — the exact typed-submission path (`submitTurn`,
`Authored: true`), so it lands in up-arrow input history and the transcript.
Flags still work because Go's `flag` package stops parsing at `up`
(`whip -m kimi up do the thing`), and the prompt itself may start with `-`
untouched — the `up` handler never re-parses its args.

The prompt rides the `model.initialPrompt` field into the session; `Init()`
emits a one-shot `initialPromptMsg` (batched with the textarea blink) and
`Update` submits it. Kicking off from `Init` — not from `Run` before
`tea.NewProgram` — is the load-bearing choice: the turn goroutine's event
callbacks `p.Send` through `m.prog`, which only exists once the program is
constructed. Combined with `--resume` the replayed history renders first and
the prompt fires as the next turn, matching `whip run`'s
prompt-after-resume order.

Tests: `internal/tui/up_test.go` — `TestInitialPromptSubmitsFirstTurn` (Init
kickoff → busy turn, authored user message, history entry, one-shot
consumption), `TestNoInitialPromptNoKickoff` (bare blink Init, empty msg is
a no-op), `TestInitialPromptMsgIgnoredWhileBusy` (a replayed msg can't
double-submit mid-turn).

## ACP agent mode

`whip acp` (`cmd/whip/acp.go`, `internal/acp/`) serves whip as an **Agent
Client Protocol** v1 agent over stdio: an editor (Zed et al.) spawns the
binary and drives the agent loop with newline-delimited JSON-RPC 2.0. Stdout
is exclusively protocol frames; diagnostics go to stderr + the event log.
Wire types, framing, and per-session cancel plumbing come from
`github.com/coder/acp-go-sdk` (schema-generated, zero transitive deps).

- **Bridge** (`internal/acp/bridge.go`) — implements the SDK's `Agent` +
  `AgentLoader`: `initialize` negotiates protocol version 1 and advertises
  `loadSession` (with a store), prompt capabilities (image only when the
  resolved model has vision; embeddedContext always), MCP-over-http, and
  `sessionCapabilities.list`/`close`. `session/new` builds a fresh
  `agent.Agent` via a `Factory` (model/key/system-prompt rooted at the
  client's `cwd`, per-session MCP manager merging client-sent servers over
  whip's config — whip wins name clashes). `session/prompt` runs
  `Agent.TurnParts` (the one-line export of the loop's parts-taking turn);
  streamed text/thought chunks, tool cards (`tool_call`/`tool_call_update`
  with kind, title, locations, raw input, and `diff` content for
  write/edit), `plan` updates from todowrite (via the new
  `Agent.SetOnTodos` hook), `usage_update` (per-request prompt tokens over
  the advertised context window), and a `session_info_update` title once the
  store auto-titles the session — all flow through `SessionUpdate`
  notifications. Stop reasons: `end_turn` normally, `cancelled` on
  `session/cancel` (never an error response, per spec), `max_tokens` when a
  context-limit error survives the compaction retry.
- **One turn at a time** — a prompt arriving mid-turn gets a JSON-RPC
  "session busy" error (ACP clients serialize turns; queueing prompts nobody
  is watching invites zombie work). The turn runs on a ctx decoupled from
  the request ctx because the SDK auto-cancels a session's in-flight prompt
  when a second prompt arrives; cancellation flows through `session/cancel`
  → `Bridge.Cancel` instead, and an idle-session cancel (which the SDK parks
  against the next request's ctx) is a no-op. `session/close` and process
  teardown (`Bridge.CloseAll` on conn EOF/signal) cancel running turns and
  close per-session MCP managers before `bashrun.KillAll()`.
- **Persistence** — turns save into the same SQLite store as the TUI
  (`storeFrom` starts at 1: the system prompt is never persisted), so an ACP
  session is resumable with `whip --resume <id>` and appears in
  `session/list`. `session/load` rejects prefix ids, verifies the request
  cwd matches the recorded one, then replays the full history (user/agent
  chunks + tool cards in terminal state, `replayUpdates` in translate.go)
  **before** responding, per spec.
- **Modes & permissions** (`internal/acp/permission.go`) — sessions
  advertise modes `auto` (default; tools ungated, `whip run` posture) and
  `ask` (gated bash/write/edit round-trip through
  `session/request_permission` with allow-once/always/reject options).
  `session/set_mode` flips live and echoes `current_mode_update`. The gate
  installs on the package-global `tools.Gate` serialized bridge-wide for the
  turn's duration (a second ask-mode turn waits rather than interleave
  mislabeled prompts); the permission request runs on the turn ctx so cancel
  unblocks it, and cancelled/errored prompts fail closed. "Allow always"
  rules are remembered per session for the session's lifetime.

Out of scope by design (recorded in `.ai-docs/plans/acp/README.md`):
terminal suite, `fs/*` client calls, elicitation, auth, config options,
session/resume+delete, ACP v2. Known gap: background subagents gated
mid-turn share the session's gate.

Tests: `internal/acp/translate_test.go` (content-block conversion, tool
kind/title/locations, diff cards, replay ordering), `bridge_test.go` +
`bridge_lifecycle_test.go` (in-memory client over pipes + scripted httptest
provider: capabilities, streaming order, cancel mid-turn → cancelled not
error, prompt-while-busy → "session busy" error + recovery, unknown session,
idle-cancel no-op, plan updates, context-limit → max_tokens),
`permission_test.go` (allow-once/reject/always-covers-repeats, auto mode
never prompts, unknown mode, cancelled outcome fails closed),
`load_test.go` (persistence incremental + system-prompt exclusion, replay
before response with tool cards, prefix-id rejection, session/list with cwd
filter, usage + title updates). All green under `-race`.

Editor setup (Zed `settings.json`):
```json
{ "agent_servers": { "whip": { "command": "/path/to/whip", "args": ["acp"] } } }
```

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
