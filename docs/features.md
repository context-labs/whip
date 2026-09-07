# Feature map

whip is a local recursive coding-agent runtime. This page describes the
current architecture; package and test names are included where they make a
contract easier to locate.

## Recursive agent runtime

- One `AgentSession` implementation is used for roots and descendants
  (`internal/daemon/recursive_runtime.go`).
- Every provider request exposes exactly `rlm_exec`; MCP discovery cannot
  widen the model-facing catalog (`internal/agent/rlm_test.go`).
- `agents.spawn` creates a retained asynchronous child with the same interface
  as its parent. `inspect`, `list`, `stop`, and `delete` provide lifecycle
  control.
- Omitted capabilities inherit; explicit capabilities narrow. Budgets roll up
  through ancestry and enforce tokens, cost, elapsed time, depth, active
  children, and concurrent turns.
- Worker capacity is reserved before child persistence. Rejection leaves no
  child record (`TestRecursiveSpawnRejectsCapacityBeforeAdmission`).
- The default recursion limit is root → child → grandchild.
- Restart reconstructs retained nodes, transcripts, authority, route, and
  kernels (`TestRecursiveRuntimeRestoresRetainedAgentAndTranscript`).

## Starlark execution

`internal/rlm` owns the framed worker protocol and module registry.

- Each kernel serializes its cells so small globals persist within a worker.
- Cells are bounded by steps, host requests, wall time, memory, output bytes,
  and frame bytes.
- The worker has no ambient daemon/provider credentials or host I/O API.
- A crash discards globals, not durable host state.
- Large cell results and host outputs become content handles.

Available modules are summarized in [tools.md](tools.md).

## Focused context

- Full history and oversized inputs are stored behind content handles.
- A request carries at most four recent user/assistant exchanges and one
  bounded summary.
- `context.inspect/search/read` returns source metadata and byte spans.
- Proactive and reactive compaction protect the provider context window.
- Large values are immutable, content-addressed, and separately authorized.

The deterministic evaluation expands a corpus above 500 KB and proves the
answer can be found through bounded reads without copying the corpus into the
root prompt (`evals/rlm`).

## Messages and collaboration

- A child’s ordinary assistant response is local to its transcript.
- `messages.send(delivery="steer"|"queued"|"next_turn")` stores a durable
  body; readiness is derived from pending mail, not a separate wake row.
- `messages.list/read/complete/defer` make body admission and lifecycle
  (`pending → delivered → done`) explicit; delivery is committed with the turn.
- Bursts retain every message and derive one ready signal
  (`TestMailboxBurstDerivesOneReadySignal`); a turn receives one bounded digest.
- Evidence handles can be granted to a direct relative with the message.
- Child turn results arrive as `agent.completed|failed|cancelled` messages
  with a short preview and evidence handle; they do not inject child output.
- Private state is agent-scoped; blackboard state is shared and supports
  append, compare-and-swap, history, and subscriptions.

## MCP

- Stdio and streamable HTTP servers are discovered from project, Codex, and
  WHIP configuration (`internal/mcp`).
- Root and child kernels use `mcp.list_servers/list_tools/call`.
- Provider tool catalogs remain stable at one tool while MCP servers change.
- Connections have startup/call deadlines, per-server serialization,
  reconnect generation guards, and bounded structured/media flattening.
- `whip mcp serve` is a daemon protocol tool host, not a model agent.
- Secrets stay references: `$VAR`/`${VAR}`/`!cmd` in env and headers resolve
  at connect time (`config.ResolveSecret`/`ResolveEnvMap`/`ExpandTemplate`)
  inside the daemon's environment — run `whip daemon restart` after exporting
  new vars. Codex `bearer_token_env_var` imports as `Authorization: Bearer
  $VAR`; `http_headers`/`env_http_headers` import as headers.

## Built-in capabilities

- `files`: list/search/read/write/patch with canonical path authorization and
  same-path mutation ordering.
- `shell`: managed process groups, bounded output, interactive PTY support,
  and workspace-wide effect authority.
- `browser`: live/dedicated/headless/extension backends behind daemon policy.
- `computer`: macOS accessibility and screenshots with per-app policy.
- LSP diagnostics can be attached after file changes.
- Permission requests are durable and any connected client can approve or deny.
  The daemon revalidates authority before the exact operation resumes; there is
  no client pairing, signing key or first-run approver enrollment prompt.

## Provider loop and models

- OpenAI-compatible streaming with retry events and usage accounting.
- Model-to-provider routing, live catalog discovery, context/output limits,
  reasoning effort, vision flags, sampling parameters, and pricing.
- `models.call` and `models.batch` provide stateless analysis without creating
  durable child identities; batch results retain input order.
- Prompt-cache keys are stable per retained session: the daemon stamps
  `prompt_cache_key` with the session id. Headless `whip run -cache-key <key>`
  pins a stable key (e.g. `repo/reviewer`) so one-off runs reuse the cached
  system prefix.

## Daemon and clients

- The daemon is the only runtime/store owner.
- TUI, `whip run`, sessions commands, ACP, and MCP stdio are protocol clients.
- WHIP v3 is one typed JSON-RPC 2.0 contract over Unix sockets and optional
  WebSockets; compatible builds attach without replacing the daemon. The operation
  and event registry generates TypeScript declarations and Ajv validators.
- Command submission returns committed acceptance. Stable client/command IDs
  deduplicate retries; changed payloads conflict. Status exposes queued, running,
  waiting and terminal outcomes. Disconnecting does not cancel accepted execution.
- Dynamic subscriptions have explicit unsubscribe, durable replay and a 16-root
  connection cap. Consistent bounded snapshots, history/collection revisions and
  replaced-subscription filtering let reconnects converge.
- Recent transcripts bootstrap quickly; older root/child messages are pageable.
  Large values use granted content references. HTTP transfers reuse the content
  store and limits. Raw human transcript inspection never changes model context.
- Omitted session model/provider routes resolve from host defaults after command
  deduplication. First-run setup remains pending after daemon startup creates its
  configuration (`session_defaults_test.go`, setup marker tests).
- TUI startup, initial host queries, the first prompt and automatic title delivery
  run against both transports in release acceptance (`client_integration_test.go`).
- Provider setup/login, versioned configuration updates and completion execute on
  the daemon host. TUI themes/keybindings remain local. Secret credentials and
  ephemeral terminal input are excluded from command journals.
- Implementation: `internal/protocol`, `internal/daemon/{server,subscription,
  transport,network,provider_service,completion}.go`, `internal/session`, and
  `packages/protocol`. Coverage: `v2_acceptance_test.go`, `runtime_parity_test.go`,
  `transport_test.go`, `client_admission_test.go`, provider/config tests and the
  generated contract/browser interoperability checks. See [protocol-v2.md](protocol-v2.md).
- Slow clients lose their bounded connection instead of blocking a root.
- Schedules and blackboard subscriptions create durable wakeups.
- Process shutdown is root-owned and waits for supervised workers.

## TypeScript client SDK

- Private Node 24 ESM workspace: `@whip/protocol` generates typed RPC/runtime
  maps and standalone CSP-safe validators; `@whip/sdk` attaches over native
  WebSockets or Node Unix sockets without owning daemon processes.
- Stable session handles, committed command acceptance, typed terminal outcomes,
  metadata-only application recovery storage, explicit identical-request retries,
  runtime identity checks and targeted cancellation share one connection engine.
- Optional `/state` views reconstruct bounded snapshots/history/live output,
  questions, permissions and recursive agent state. History revisions prevent
  mixing rewind epochs; catalog polling is observed and never opens every root.
  Live and snapshot presentation share delta/cumulative update rules and stable
  row keys; interleaved tool updates do not create duplicate transcript rows.
- `/react` hooks subscribe to those immutable views. The working example owns
  drafts and storage; no React dependency is loaded by core/state consumers.
  Consecutive identical internal mailbox digests share one expandable row with
  a delivery count; raw transcript entries and authored messages remain intact.
- Content reads verify root/agent grants, size and SHA-256. Permission helpers
  send typed decisions from trusted clients without a signer; the example always
  exposes Allow once and Deny. Provider configuration and terminal input are
  ephemeral and never enter SDK recovery storage.
- Implementation: `packages/sdk`, `examples/client`. Coverage: SDK TypeScript
  unit tests, `daemon.acceptance.mjs`, isolated `TestV2SDKBridge`, actual SDK
  strict-CSP Chromium/Firefox/Safari and React StrictMode smoke tests, plus packed
  package installation. See [SDK usage](../packages/sdk/README.md).

## React web application

The private web application is implemented as a thin consumer of the same SDK.
The release gate remains open for the manual device/accessibility checks recorded
in the [web plan](../.ai-docs/plans/web-app/README.md); this table maps implemented
behavior to its owning code and repeatable validation.

| Behavior | Implementation | Validation |
| --- | --- | --- |
| Attach to an existing host, discover its directory tree, and route to retained sessions | `apps/web/src/main.tsx`, `packages/app/src/runtime.ts`, `packages/app/src/{shell,directory-picker}.tsx`, `internal/daemon/host.go` | `packages/app/test/runtime.test.ts`, `internal/daemon/host_test.go`, `apps/web/scripts/browser.mjs` |
| Window-local session tabs, overflow/search/reorder/close/reopen, preserved attachments and reading anchors, bounded background activity | `packages/app/src/{session-tabs,session-tab-routing,session-tab-strip,compositions,reading-positions}.ts*`, `packages/ui/src/workspace-tabs.tsx`, `internal/daemon/session_summaries.go`, `internal/session/navigation.go` | App tab/routing/composition tests, `apps/web/scripts/session-tabs.mjs`, UI all-theme/CSP tab tests, `TestSessionSummariesAcrossTransports` and navigation bounds tests |
| Root/child conversations, grouped tool calls, read-only Starlark, bounded history and recipient-scoped drafts | `packages/app/src/{conversation,timeline,composer}.tsx`, SDK session views | `packages/app/test/{timeline,composer}.test.tsx`, production browser fixture; `apps/web/scripts/performance.mjs` exercises 10,000 root messages, 100 retained children, stable selection/scroll and 32 drafts under 16 concurrent streams |
| Questions, permission decisions, remembered rules and exact-turn cancellation | `packages/app/src/{requests,conversation}.tsx`, SDK permission/command helpers | `packages/app/test/requests.test.tsx`, two-client production browser fixture, existing daemon permission tests |
| Recursive work, mailbox/evidence inspection, goals, schedules, budgets, context and integrations | `packages/app/src/inspector.tsx`, `packages/app/src/details/`, host read services | `packages/app/test/inspector.test.tsx`, `internal/daemon/host_test.go`, generated SDK operation coverage |
| Host-owned provider login/configuration and local recovery/appearance settings | `packages/app/src/settings.tsx`, SDK services, daemon provider/configuration services | App runtime tests, existing provider/configuration acceptance, browser workflows |
| Accessible controls, all TUI themes, custom-theme resolution, auto appearance and portaled overlays | `packages/ui`, `internal/theme`, `cmd/themegen`, `internal/daemon/host.go` | Theme parity/drift tests, 65-theme Axe fixtures, ten component interaction scenarios, Chromium/Firefox/actual Safari CSP smoke |
| Packaged same-origin web assets and explicit `whip web` launch | `internal/webassets`, `cmd/whip/web.go`, `scripts/pack-web.mjs` | `internal/webassets/assets_test.go`, `cmd/whip/web_test.go`, `scripts/pack-web.test.mjs`, isolated packed-source consumer builds |

React 19 and TanStack Router/Query/Form/Virtual compose the product. Base UI owns
accessible component interactions; StyleX extracts authored CSS. Source UI/app
packages expose explicit public entry points and are tested as real installed
archives in both production and Vite development builds. No editor, code-review
surface, standalone terminal, account, pairing or signer UI is included. Tool
requests that require terminal input direct the user to the TUI.

Closing a page detaches the client. It neither cancels accepted work nor sends
unsent drafts. A command outcome is separate from completion of descendant agents,
mailboxes or schedules. See [web-app.md](web-app.md) for exact startup commands,
trusted-network setup and current browser evidence.

## Terminal UI behavior

- Streaming text, reasoning, tool, plan, permission, usage, and terminal
  events are rendered from daemon events.
- `/agents` inspects or controls the recursive tree; `/mcp` manages server
  lifecycle.
- `/model`, `/effort`, `/goal`, `/compact`, `/rewind`, `/fork`, `/schedule`,
  `/browser`, and `/computer-use` are daemon commands.
- ACP maps editor sessions and permission decisions onto the same root
  protocol. It does not own a second agent loop.
- The TUI is a single full-screen (alternate-screen) interface laid out like
  opendocker: a left column of panels, the transcript with its input box, and
  a key-hint footer across the whole last row. On exit it prints a resume line
  to the scrollback. The former inline mode and the `uiMode` config key are
  gone.
- The left column shows on terminals of 120 columns or more and holds three
  panels: `[1] Agents`, `[2] Context` (tokens, share of the window, spend) and
  `[3] LSP`. One is expanded and the others collapse to their header row;
  `ctrl+x 1/2/3` pick the expanded one, `ctrl+x b` hides the column. The
  `sidebar` and `panel` config keys set the startup state.
- Agent rows are structured: a lifecycle badge (running, blocked, idle, done,
  failed, queued…), the name indented by depth, and what the agent is doing
  (its running REPL cell or tool with the elapsed time, or pending mail). The
  root heads the tree. `ctrl+t` or ↓ on an empty input focuses the panel
  (its bar lights up), ↑/↓ select, enter opens an agent, `ctrl+x s` stops
  the selected one, esc leaves; enter on the root, or esc with an empty
  input, returns from a child to the main transcript. On narrow terminals the
  same rows sit under the input.
- `ctrl+x r` (or `/repl`, config key `repl`) opens the REPL panel on the
  right: the open agent's live Starlark cells, code as the model writes it,
  print output as it happens, each host call with its duration, results,
  errors, and worker restarts. Below 150 columns the panel takes the left
  column's place; from 150 the two share the screen. The panel takes half of
  the width right of the left column (half the terminal when the column is
  hidden). The wheel over the panel scrolls it
  independently of the chat (it follows the newest cell until you scroll up,
  then a "↓ N more lines" chip and a scrollbar mark the position). The panel
  keeps every cell seen during the TUI session, even after snapshots drop
  idle children.
- `user.ask` from the root agent opens a floating dialog over the dimmed
  session: the question, the numbered options with their descriptions, and
  key hints. ↑↓ (j/k) move, 1–6 jump, space toggles when several answers are
  allowed, enter answers, esc dismisses. The dialog stays up until the daemon
  records the answer (it may come from another client), then a dim transcript
  line notes what was chosen.
- The frame has a one-row margin above the columns and a two-row footer band
  at the bottom (a blank row, then the key hints on the last row) under the
  prompt or, on narrow terminals, under the agents dock. The hints' right side
  lists the global chords; the left side follows the
  keyboard's owner: the running turn (spinner, `esc interrupt`), an armed
  `ctrl+x` leader (every chord), the focused Agents panel, or the working
  directory.
- `shift+enter`, `ctrl+j` and `alt+enter` insert a newline. Bubble Tea v2
  requests kitty key disambiguation and modifyOtherKeys at startup; inside
  tmux a modified key only reaches the pane when the server option
  `extended-keys` is on. whip never changes your tmux server: when the option
  is off it warns and suggests `set -s extended-keys on` in `~/.tmux.conf`.
  mosh collapses shift+enter before tmux or whip see it — use ctrl+j there.
- Pasted images show as chips. A clipboard image (`ctrl+v`) lands in the
  input as `[Image N]`; a pasted or dropped image path, or a macOS screenshot
  preview, as `[Image N: name.png]` (long names shortened to 24 columns), with
  the bytes copied to `~/.whip/pastes/`. The live transcript echoes the chip;
  only the text sent to the daemon expands it to the real `@path` mention,
  which is what a resumed or rebuilt transcript shows. The registry resets on
  `/clear` and when the TUI switches root session, so a recalled chip from
  before stays literal text.

## Storage and recovery

Runtime-v2 stores command/event journals, agents, per-agent transcripts,
messages, state, capabilities, budgets, permissions, schedules, and content
references in SQLite WAL plus immutable content files.

On restart:

- committed command outcomes remain final;
- running descendant sessions become retained idle sessions;
- queued recursive notifications remain actionable;
- uncertain operations are interrupted and reservations are reconciled;
- external side effects are never guessed or replayed automatically.

See [architecture.md](architecture.md), [rlm-runtime.md](rlm-runtime.md), and
[concurrency.md](concurrency.md) for the contracts behind these features.

## Transcript navigation

The transcript scrolls with the wheel and PgUp/PgDn. When it is longer than
the window a scrollbar sits in the column right of the text; scrolled away from
the newest rows, a "↓ N more lines" chip marks how far, and clicking it (or a
new turn, when following) jumps back to the bottom. Drag to select and copy
(OSC 52, with a clipboard tool fallback); double-click selects a word,
triple-click a row, both copying immediately. Clicking a user or assistant
message opens Message Actions (revert, copy, fork); clicking a tool result
expands it. Failed local commands report in the top-right toast rather than in
the conversation.

## Skills

SKILL.md skills load from `.agents/skills` in the working directory,
`~/.whip/skills`, and `~/.agents/skills` (`skills.DirsFor`).

CLI: `whip skills list` (names, sources, warnings) and `whip skills import
[--dry-run]` — copies skills from other harnesses' user dirs
(`~/.codex/skills`, `~/.claude/skills` — `skills.ForeignDirs`) into
`~/.agents/skills`, deduped by name against what whip already loads and
across the sources (codex wins on a dup). Never overwrites an existing
skill. Tests: `cmd/whip/skills_test.go`.

**Spec compliance** (agentskills.io, matching pi's `core/skills.ts`): name
validated (≤64 chars, lowercase a-z/0-9/hyphens, no leading/trailing/double
hyphens), description ≤1024 chars (a *validity* ceiling, not a prompt budget),
`disable-model-invocation: true` skills excluded from the catalog but still
invocable via `$name`. Violations load with a `Warning` (surfaced in the
startup report), never silently disappear. Tests: `skills/spec_test.go`.

## Themes

Every color whip paints comes from one theme: text, muted, accents, the
selection fill, the raised surfaces behind cards and the prompt box, and the
syntax colors inside code blocks (markdown and tool output share them). `auto`
follows the terminal background; `light` and `dark` pin the built-ins.

`/theme` with no argument opens the switcher. `/theme <name>` pins a theme and
saves it to the config (`"theme": "<name>"`).

The whole view is painted with the theme's background and text colour, so a
light theme reads on a dark terminal and the terminal's own colours follow
the theme while whip runs (they are restored on exit). Besides whip's `light`
and `dark`, the switcher lists opencode's theme catalog, converted from its
assets with `internal/theme/themes/convert_opencode.py`: aura, ayu,
carbonfox, catppuccin (latte/frappe/macchiato), cobalt2, cursor, dracula,
everforest, flexoki, github, gruvbox, kanagawa, lucent-orng, material, matrix,
mercury, monokai, nightowl, nord, one-dark, opencode, orng, osaka-jade,
palenight, rosepine, solarized, synthwave84, tokyonight, vercel, vesper and
zenburn — each as `<name>` (dark) and `<name>-light`. Catalog themes pin their
surfaces, syntax colours and markdown accents; whip's own themes derive them.

User themes are JSON files in `~/.whip/themes/<name>.json` (or under
`$WHIP_HOME`). Any token you leave out defaults from the built-in of the same
darkness; unknown keys and malformed colors are reported with the allowed keys
when you run `/theme`. Colors are `#rrggbb` or an ANSI palette index `0`-`255`.

```json
{
  "dark": true,
  "palette": {
    "text": "#e0e0e0", "muted": "#808080", "faint": "#5a5a5a",
    "primary": "#00aaff", "accent": "#c678dd",
    "success": "#98c379", "warning": "#e5c07b", "error": "#e06c75", "info": "#61afef",
    "link": "#56b6c2", "emphasis": "#e5c07b", "onPrimary": "#0a0a0a",
    "border": "#3a3a3a", "borderFocus": "#61afef", "bg": "#1e1e1e",
    "diffAdd": "22", "diffDel": "52"
  },
  "surfaces": { "panel": "#262626", "element": "#303030", "hover": "#3a3a3a" },
  "chroma": "dracula"
}
```

`diffAdd`/`diffDel` are the background tints behind added and removed diff
lines. Optional `syntax` (`keyword`, `string`, `number`, `comment`, `function`,
`type`, `operator`, `punctuation`) and `markdown` (`heading`, `strong`, `code`,
`quote`) blocks pin those colours instead of deriving them from the palette.
`surfaces` is optional: without it the card and prompt fills are derived from
the terminal's real background so they read as raised layers on any terminal.
`chroma` is optional: without it the code colors are generated from the
palette; with it, that registered chroma style is used instead.


The renderer-independent specification, catalog and ANSI/Chroma resolver live in
`internal/theme`; the TUI retains terminal-specific rendering and background
handling in `internal/tui/theme`. The browser generates all 65 named palettes with
`cmd/themegen`, follows `prefers-color-scheme` for `auto`, and stores selection on
the viewing device. Its accessible surface/text derivation leaves source palettes
unchanged and retains full Chroma code styling. Host custom discovery and pasted
JSON import share the Go resolver; the browser never compiles arbitrary CSS.
