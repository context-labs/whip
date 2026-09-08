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
- Remote HTTP requests remain tied to the transport lifetime through SSE body
  reads, including startup before a session is published. Retirement cancels
  stalled streams and permits a one-second best-effort session DELETE. Healthy
  notification streams survive startup completion and catalog refresh
  (`internal/mcp/http_transport.go`, `http_transport_test.go`).
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
- WHIP v4 is one typed JSON-RPC 2.0 contract over Unix sockets and optional
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

## macOS desktop application

The Electron host packages the same production renderer as the web application.
The [desktop guide](desktop.md) documents local builds, canonical runtime installation,
signing and release configuration. [Desktop acceptance](../.ai-docs/plans/desktop-app/progress.md)
is still open for notarized distribution, actual updates and manual device checks.

Local connections use one selected installed `whipcode` executable, with
`~/.whipcode` as the default home in both normal desktop channels. The packaged
backend is an installation payload, not a privately retained daemon. The initial
canonical path on this Mac is `/usr/local/bin/whipcode`. The historical-state
cleanup and signed/notarized local installation are complete. Actual desktop
diagnostics, CLI/desktop startup, shared WebSocket sessions, reconnects and a live
provider message passed; see the
[canonical installation record](../.ai-docs/plans/canonical-whipcode/README.md).

| Behavior | Implementation | Validation |
| --- | --- | --- |
| One bootstrap and UI in browser and desktop, with independent SDK clients per host; native effects behind an adapter | `apps/web/src/{main,bootstrap}.tsx`, `apps/web/src/platform/`, `packages/app/src/{platform,desktop-bridge}.ts` | App architecture/bootstrap/desktop-adapter tests; packed app/UI consumer and production renderer native-import guard |
| Exact shared renderer in Go embed and Electron ASAR, verified native companions and full DMG/ZIP contents | `scripts/{renderer-artifact,pack-web}.mjs`, `apps/desktop/scripts/{build,package,verify,distribution}.mjs`, `apps/desktop/forge.config.cjs` | Renderer/provenance/distribution tests, actual signed archive extraction/mount and signature/fuse checks |
| Stable local/URL/SSH profiles, safe migration, explicit replacement identity and stale connection disposal | `packages/app/src/{connections,hosts,runtime}.ts`, `packages/app/src/{host-dialog,connection-dialog}.tsx`, `packages/sdk/src/client.ts` | App connections/runtime/replacement-runtime/session-navigator tests; SDK changed-runtime regression |
| Canonical installed whipcode selection, compatible attach-before-start, owner-proven stale socket recovery, no daemon shutdown on GUI exit or backend replacement during an app update | `apps/desktop/src/{main,runtime,transport}.ts`, `cmd/whip/daemon_manage.go`, `cmd/whip/desktop_runtime.go` | `apps/desktop/tests/runtime.test.ts`: saved-path precedence, missing-path refusal, compatible reuse, explicit restart, port conflicts and bounded/cancelled processes; Go owner/socket tests |
| Verified whipcode payload with source/build/distribution provenance and the matching embedded Swift helper; explicit installation refuses a different existing executable | `apps/desktop/scripts/{build,verify,distribution}.mjs`, `apps/desktop/src/runtime.ts`, `cmd/whip/desktop_runtime.go` | Native runtime manifest/integrity, explicit-install, concurrent-publication and cancelled-copy tests; distribution checks; signed/notarized installed artifact and matching canonical executable verified |
| This Mac setup before daemon availability, read-only Test Connection, native executable choice, explicit installation/restart and expandable path/build diagnostics | `packages/app/src/{platform,desktop-bridge}.ts`, `packages/app/src/host-dialog.tsx`, `apps/web/src/platform/desktop.ts`, `apps/desktop/src/{main,preload,runtime}.ts` | `packages/app/test/{local-runtime,desktop-adapter,architecture}.test.ts*`; `TestDaemonStatusDoesNotInitializeHome`, `TestDaemonStatusPreservesExistingRuntime`; Chromium missing-daemon UI check |
| System SSH configuration, private Unix forwarding, in-app prompts and owned helper cleanup on GUI death | `apps/desktop/src/ssh.ts`, `cmd/whip/desktop_{ssh,askpass,wait_darwin,wait_linux}.go`, `packages/app/src/host-prompts.tsx` | Real isolated sshd native tests, Go race/integration process-group and askpass tests, shared prompt stale/cancel tests |
| Native save/copy/folder/link effects, opt-in attention notifications, restored tabs and draft-aware close | `apps/desktop/src/{main,native,links}.ts`, `packages/app/src/{attention-notifications,session-tab-routing,session-tab-strip,settings}.ts*` | Native save/disposal tests, app attention/close-tab/settings tests, signed Finder launch and tab/draft checks |
| Deferred updater, one-action managed backend synchronization, attested candidate staging and conditional feed promotion | `apps/desktop/src/{updates,runtime}.ts`, `cmd/whip/desktop_runtime_sync.go`, `internal/daemon/maintenance_unix.go`, `apps/desktop/scripts/{ci-signing,publish,publish-github,release-candidate,notices}.mjs`, `.github/workflows/release-desktop.yml` | Updater release-name/approval/retry tests, `TestDesktopCompiledUpdate` (real two-build handoff and session/config preservation), maintenance-lock race tests, candidate/publisher identity and conditional-write tests; actual Squirrel N→N+1 remains a release gate ([runbook](desktop-releases.md)) |

## React web application

The private web application is implemented as a thin consumer of the same SDK.
The release gate remains open for the manual device/accessibility checks recorded
in the [web plan](../.ai-docs/plans/web-app/README.md); this table maps implemented
behavior to its owning code and repeatable validation.

| Behavior | Implementation | Validation |
| --- | --- | --- |
| Attach to existing hosts, discover each directory tree, and route to retained sessions | `apps/web/src/main.tsx`, `packages/app/src/runtime.ts`, `packages/app/src/{shell,directory-picker}.tsx`, `internal/daemon/host.go` | `packages/app/test/runtime.test.ts`, `internal/daemon/host_test.go`, `apps/web/scripts/browser.mjs` |
| Choose a Local working directory in the OS-native folder dialog (osascript/zenity/kdialog/PowerShell), falling back to the web directory browser; Remote uses its daemon directory browser | `host.directory.pick` in `internal/{protocol,daemon}/host.go`, `packages/sdk/src/services.ts`, `packages/app/src/directory-picker.tsx` | `TestDirectoryPickCommand`/`TestHostDirectoryPickValidation` in `internal/daemon/host_test.go` |
| Multiple daemon connections, Local-owned saved profiles, verified identities, isolated disconnects and guided Local/Remote session creation | `packages/app/src/{hosts,runtime}.ts`, `{host-dialog,welcome,settings}.tsx`, `internal/config/remote_hosts.go`, daemon configuration service | `packages/app/test/hosts.test.ts`, `runtime.test.ts`, `sidebar-creation.test.tsx`; `internal/config/remote_hosts_test.go`; `TestProviderClientRemoteHostsPreserveConfigurationAndRejectConflicts` |
| Search and advisory attention across hosts, source labels/filter, independent bounded pagination and partial failures without root hydration | `packages/app/src/{session-search-dialog,attention}.tsx` | `packages/app/test/multi-host-discovery.test.tsx` |
| Window-local session tabs across hosts, v3 layout and retained v1/v2 recovery, overflow/search/reorder/close/reopen, preserved attachments and reading anchors, bounded background activity | `packages/app/src/{session-tabs,session-tab-routing,session-tab-strip,compositions,reading-positions}.ts*`, `packages/ui/src/workspace-tabs.tsx`, `internal/daemon/session_summaries.go`, `internal/session/navigation.go` | App tab/routing/composition tests, `apps/web/scripts/session-tabs.mjs`, UI all-theme/CSP tab tests, `TestSessionSummariesAcrossTransports` and navigation bounds tests |
| Nested split views, draggable tabs between panes, duplicate chats with independent agents/scroll, shared drafts, and responsive layout restoration | `packages/app/src/{session-tabs,session-tab-strip,session-tab-routing,workspace-views,runtime,conversation,composer}.ts*`, `packages/ui/src/workspace-layout.tsx` | App model/routing/runtime/workspace/composer tests; `apps/web/scripts/workspace-layout.mjs`; UI layout Chromium/Firefox, Axe and strict-CSP fixture |
| Read-only session REPL, in-place Open REPL/Open chat, independent split modes/agents, live cells and bounded history | `packages/app/src/{repl-view,reading-list,conversation,session-tab-strip}.tsx`, `packages/sdk/src/{executions,state}.ts`, mode-aware tab routing | SDK execution/state tests; app REPL, reader and routing tests; `apps/web/scripts/repl-viewer.mjs` with opt-in `v2_sdk_repl_test.go` fixtures |
| Root/child conversations, grouped tool calls, read-only Starlark, bounded history and recipient-scoped drafts | `packages/app/src/{conversation,timeline,composer}.tsx`, SDK session views | `packages/app/test/{timeline,composer}.test.tsx`, production browser fixture; `apps/web/scripts/performance.mjs` exercises 10,000 root messages, 100 retained children, stable selection/scroll and 32 drafts under 16 concurrent streams |
| Compact growing composer, shared model/reasoning picker for idle root sessions, and neutral input focus borders | `packages/app/src/{composer,model-selection}.tsx`, shared UI form styles | Composer tests; `apps/web/scripts/browser.mjs` (growth/shrink, explicit model/effort changes, busy state, draft/reload preservation); `apps/web/scripts/model-picker.mjs` (detail-card bounds, side flipping, scrolling, keyboard and resize in Chromium/Firefox); split workspace browser fixture |
| Right-aligned user bubbles, hover/focus timestamps and controls, immediate submission previews, queued/running inbox messages | `packages/app/src/{input-presentation,runtime}.ts`, `packages/app/src/{conversation,timeline,composer}.tsx` | `packages/app/test/input-presentation.test.tsx`, composer/runtime tests, `apps/web/scripts/user-messages.mjs` (Chromium/Firefox delayed request, running turn, reload, duplicate text, hover/focus and responsive themes) |
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
  session: the question, the numbered options with their descriptions (a ★
  marks the agent's recommended option), and key hints. ↑↓ (j/k) move,
  1–6 jump, space toggles when several answers are allowed, enter answers,
  esc dismisses. A batched ask (`user.ask(questions=[...])`) pages: the title
  reads "Question 2/4", enter answers and advances, tab/→ next,
  shift+tab/← back, s skips the page, / types a written response, and the
  last page's enter submits the batch; the transcript line notes
  "answered 3/4". The dialog stays up until the daemon
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
handling in `internal/tui/theme`. The browser generates all 66 named palettes with
`cmd/themegen`, follows `prefers-color-scheme` for `auto`, and stores selection on
the viewing device. Its accessible surface/text derivation leaves source palettes
unchanged and retains full Chroma code styling. Host custom discovery and pasted
JSON import share the Go resolver; the browser never compiles arbitrary CSS.

**Claude Code** (`claude-code`) is a built-in dark theme based on the supplied
Paper desktop screens: `#141414` canvas, `#111110` sidebar, warm gray text,
`#222221` composer/hover fills, and `#343434` selected rows. Select it under
**Settings → Appearance → Color theme**, or use `/theme claude-code` in the TUI.
The source is `internal/theme/themes/claude-code.json`; research and the role
mapping are in `.ai-docs/plans/claude-code-theme/README.md`. Optional `displayName`
and optional `web` fields (`navigation`, `quietBorder`, `codeBackground`,
`inlineCodeBackground`) preserve the stable theme ID and pin browser surfaces; themes without overrides retain the existing derivation.
The browser still adjusts insufficient text contrast, including inline code.
Tests: `internal/theme/resolve_test.go`, `packages/ui/tests/themes.test.ts`,
and `apps/web/scripts/claude-code-theme.mjs` cover catalog parity, validation,
source colors, rendered surfaces, selection, reload and switching away.

## Web directory navigation

The saved-session sidebar follows the compact Claude/Paper hierarchy while using
WHIP's themes. It groups the loaded SDK catalog by exact directory, keeps
worktrees distinct, preserves pin/recency order, and offers New session, Search
sessions and Settings. Hosts have separate headings and connection status within
one sidebar. Directory + preselects its source host and folder in the Local/Remote
creation form; it does not create work until submitted. Search opens a centered
dialog with host labels/filter, recent sessions, debounced host search, independent
bounded paging, partial errors and arrow/Enter navigation (`session-search-dialog.tsx`; `apps/web/scripts/session-search.mjs`).
Native session links and
background-tab menus preserve remembered child/inspector locations.

The sidebar has a 320 px default, keyboard/pointer resizing (256–420 px), a
hide/show toggle, window-local layout and bounded host-specific collapse state.
Mobile uses a contained Sheet with touch targets and a fixed footer. Virtualized
catalog updates preserve the visible reading anchor and never hydrate roots to
obtain labels. See [frontend navigation](frontend.md#saved-session-navigation).

Code: `packages/app/src/session-sidebar.tsx`, `sidebar-state.ts`,
`sidebar-layout.tsx`, `welcome.tsx`, and `session-tab-routing.ts`.
Tests: `sidebar-state.test.ts`, `sidebar-layout.test.tsx`,
`sidebar-creation.test.tsx`, `session-tab-routing.test.ts`, and the isolated
production-browser workflow `apps/web/scripts/sidebar.mjs`.


## Multiple execution hosts in the web workspace

Local owns a `remote_hosts` registry in its existing distribution-specific
configuration file. Saved IDs, names, URLs, verified runtime IDs and startup
connection preferences are shared across browsers through revision-checked config
updates. Browser-only addresses remain explicitly importable. Separate SDK clients
connect directly to existing LAN/Tailscale daemons, verify identity, and reject
runtime aliases or unaccepted replacements. Losing one host preserves the others;
losing Local blocks profile edits while attached remote sessions remain usable.

One v3 window layout carries runtime identity on every tab and allows mixed-host
panes. The existing four-pane, 32-tab and four-root-view budgets apply to the whole
window. Migration adopts the last-used legacy host layout and keeps original
v1/v2 data, with remaining layouts available under **Restore previous host tabs**.
If a complete layout cannot fit, open individual previous tabs, including closed
entries, without consuming the original layout.
Runtime/root/recipient and runtime/client/command identities prevent collisions
in drafts, views, content and command observations.

New sessions explicitly choose Local or Remote, host, folder and that host's
model/default. Settings identify the target execution host; saved-host edits
always target Local and viewing preferences stay local to the browser. Search
and attention show source hosts, filters, separate bounded pages and partial
failures. Responses open their owning sessions; neither index hydrates roots.
Listener setup and exact browser Origin allowlists remain explicit trusted-network
configuration; automatic stable-Origin handling is deferred.
The desktop origin `whip-app://bundle` can be explicitly allowed through
`WHIP_ALLOWED_ORIGINS` (`WHIPCODE_ALLOWED_ORIGINS` for the whipcode distribution).
Other custom origins, wildcards, suffixes, ports and paths remain rejected.
`internal/daemon/network_test.go:TestNetworkDesktopOriginIsExplicitAndExact`
checks validation, explicit opt-in and CORS response headers.

Code: `internal/config/remote_hosts.go`, `internal/daemon/provider_service.go`,
`packages/app/src/{hosts,runtime,session-tabs,workspace-views}.ts`,
`{host-dialog,welcome,settings,session-search-dialog,attention}.tsx`.
Tests: `internal/config/remote_hosts_test.go`, the remote-host configuration test
in `internal/daemon/provider_client_behavior_test.go`, and
`packages/app/test/{hosts,runtime,session-tabs,session-tab-routing,workspace-views}.test.ts`,
`sidebar-creation.test.tsx`, `multi-host-discovery.test.tsx`.
See [architecture](frontend.md#runtime-construction-and-lifetimes) and
[operation](web-app.md#multiple-execution-hosts). Validation evidence is tracked in
the [phased plan](../.ai-docs/plans/web-multiple-hosts/README.md).


## Model usage budgets

New root sessions and uncapped descendants have unlimited cumulative cost,
tokens, and elapsed usage. Optional `agents.spawn(..., budgets=...)` limits still
constrain a subtree; zero is a zero allowance. Worker/concurrency, recursion,
storage, and provider response limits remain independent and bounded.

Web and TUI budget inspection is read-only. Per-attempt accounting uses each
route's model prices, including child overrides, compaction, and helper calls.
Unknown provider usage is marked incomplete and retained separately from known
usage; finite caps account for that uncertainty conservatively. Catalog-derived
costs are estimates, not provider invoices. Protocol 4 carries nullable limits;
the fresh runtime schema is version 8. Older stores are rejected without being
modified; this change includes no session migration or automatic data deletion.


## Whipcode distribution

The `whip-rlm` branch publishes a separate `whipcode` executable through copied
CI, security, and release workflows. Both distributions use one Go runtime and
the same embedded web application. A compiled `internal/buildinfo` identity
selects CLI instructions, `.whipcode` home paths, `WHIPCODE_HOME`, and isolated
network controls. Application-owned config, auth, sessions, locks, notices,
skills, browser profiles/extension state, and macOS helper extraction follow
that home; renaming a binary does not switch its identity.

`install-whipcode.sh` verifies complete, versioned prerelease assets and SHA-256
checksums before atomic replacement. `whipcode update` stays in its channel,
replaces the invoked installation, and requests only its daemon's restart.
Stable `whip` release discovery remains unchanged. See [installation](../README.md#whipcode-branch-builds).

Code: `internal/buildinfo`, `internal/config`, `internal/update/whipcode.go`,
`cmd/whip/update.go`, `install-whipcode.sh`, `scripts/publish-whipcode.sh`, and
`.github/workflows/{ci,security,release}-whipcode.yml`.
Tests: `TestDistribution*` in the affected Go packages, `TestFetchWhipcodePages`,
`TestWhipcodeVersionComparison`, `scripts/test-install-whipcode.py`, and
`scripts/test-distributions.py` (both compiled binaries, independent sockets,
restart, and self-update).
## Native mobile companion (development)

The Expo workspace in `apps/mobile` provides manual private-host setup, themed
Sessions/Attention/Settings, root and child conversations, queued/steering input,
turn-specific Stop, question forms and one-shot permission decisions. It consumes
the existing SDK WebSocket protocol; execution stays on the host. Application auth,
QR pairing and push notifications remain outside this release.

- Connection diagnostics: `apps/mobile/src/runtime/connection-test.ts` and
  `app/server.tsx`; transient HTTPS/WSS/session probes, per-step deadlines,
  identity checks and modal-local actionable errors. `connection-test.test.ts`
  and `server-screen.test.tsx` cover HTTP/protocol failures, headless hosts,
  response bounds, cancellation and late results. SDK transport tests preserve
  React Native close reasons without leaking callbacks after disposal.
- Native runtime and lifecycle: `apps/mobile/src/runtime/runtime.ts`, SDK
  `client.pause/resume` and synchronized views; covered by SDK client/state tests
  and mobile runtime tests.
- Durable local identity, revisioned drafts and atomic correlation:
  `apps/mobile/src/runtime/storage.ts`, local `WhipStorage` native module;
  `storage.test.ts` and `storage.native.test.ts` cover SQLite atomicity, quotas,
  key/database mismatch and native setup boundaries.
- Partial creation recovery: `apps/mobile/src/features/creation.ts` and
  `creation.test.ts`; separate create/effort/input identities preserve the created
  root without automatic continuation after restart.
- Foreground Attention and its qualified tab badge:
  `apps/mobile/src/features/attention.tsx` and `attention.test.tsx`; one observer
  handles polling, bounded pagination, focus refresh and stale/partial counts.
- Bounded native text: `apps/mobile/src/components/paged-text.tsx` and
  `conversation.tsx`; paging, recycling, full-copy and explicit body-read tests
  cover the rendering boundary. `markdown.test.tsx` covers source fallback for
  images/HTML and the external-link allowlist.
- Permission recovery: SDK `permissions.status`, daemon permission outcome
  normalization/legacy decoding; SDK services and daemon server tests cover both
  transports, failed outcomes and original decision identity.
- Pure reuse: `@whip/app/presentation` and `@whip/ui/theme-data`; web retains its
  own renderer. See [frontend.md](frontend.md) for package boundaries.

Native device validation and distribution are not implied by this entry.
[Mobile setup](mobile.md) and the
[implementation evidence](../.ai-docs/plans/mobile-app/EVIDENCE.md) track the actual
build/device/release state.
