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

## Built-in capabilities

- `files`: list/search/read/write/patch with canonical path authorization and
  same-path mutation ordering.
- `shell`: managed process groups, bounded output, interactive PTY support,
  and workspace-wide effect authority.
- `browser`: live/dedicated/headless/extension backends behind daemon policy.
- `computer`: macOS accessibility and screenshots with per-app policy.
- LSP diagnostics can be attached after file changes.
- Human permission requests are durable, signed at the client boundary, and
  revalidated before an exact operation resumes.

## Provider loop and models

- OpenAI-compatible streaming with retry events and usage accounting.
- Model-to-provider routing, live catalog discovery, context/output limits,
  reasoning effort, vision flags, sampling parameters, and pricing.
- `models.call` and `models.batch` provide stateless analysis without creating
  durable child identities; batch results retain input order.
- Prompt-cache keys are stable per retained session.

## Daemon and clients

- The daemon is the only runtime/store owner.
- TUI, `whip run`, sessions commands, ACP, and MCP stdio are protocol clients.
- Stable command IDs make retries idempotent.
- Ordered replay and behavioral snapshots restore client presentation state.
- Slow clients lose their bounded connection instead of blocking a root.
- Schedules and blackboard subscriptions create durable wakeups.
- Process shutdown is root-owned and waits for supervised workers.

## Terminal UI and adapters

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
  column's place; from 150 the two share the screen and the REPL grows with
  the terminal (42–64 columns). The wheel over the panel scrolls it
  independently of the chat (it follows the newest cell until you scroll up,
  then a "↓ N more lines" chip and a scrollbar mark the position). The panel
  keeps every cell seen during the TUI session, even after snapshots drop
  idle children.
- The footer's right side lists the global chords; its left side follows the
  keyboard's owner: the running turn (spinner, `esc interrupt`), an armed
  `ctrl+x` leader (every chord), the focused Agents panel, or the working
  directory.

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
assets with `internal/tui/theme/themes/convert_opencode.py`: aura, ayu,
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
