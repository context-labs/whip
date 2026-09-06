# WHIP web application: OpenCode research and proposed delivery plan

Branch: `whip-rlm`

Status: Research and proposal, 2026-09-06. No application implementation in this change.

## Goal

Build a React web application for doing real coding work with WHIP: direct an
agent, understand concurrent work, answer requests, inspect evidence, review
changes, and continue confidently after reconnecting. Match OpenCode's visual
discipline and everyday interaction quality, while making WHIP's recursive
runtime understandable.

The existing TypeScript SDK is the connection and synchronization foundation.
The daemon remains the sole execution authority. An application screen must
not recreate its command journal, event reducer, scheduler, context assembly,
provider login, permission policy, or agent lifecycle.

## Decisions already established

- React and TypeScript; web first, Electron later. macOS first for desktop.
- Execute on the user's machine or a connected trusted-network host.
- Any connected client may approve or deny tool permission requests. No
  connection authentication, signer, enrollment, pairing, accounts, or relay.
- Permissions, internal agent capabilities, content grants, and delegated MCP
  checks remain daemon responsibilities.
- Multiple clients may observe and operate the same root concurrently.
- Keep Node 24, npm workspaces, the root lockfile, and the generated Go contract.
- The current working tree uses protocol **v3**, including `/api/v3/ws`, after
  removal of signed approvals. `runtime-v2` remains the data-directory name.
- Hosting infrastructure, Electron packaging, custom-agent SDKs, and the
  separately assigned MCP bug are outside this web milestone.

## Product questions still open

Questions were sent during research. These are proposed defaults, not answers
or approved scope:

1. **Editing/terminal scope:** deliver excellent review first, then lightweight
   file editing and user terminal tabs. A full IDE, debugger, extensions,
   language-server editing client, and collaborative editor are separate work.
2. **Devices:** desktop/laptop first; mobile supports monitoring, conversation,
   requests, and simple review. Do not squeeze every desktop pane onto a phone.
3. **Visual direction:** closely follow OpenCode's quiet, compact workbench
   principles; give WHIP its own agent/activity structure and restrained accent.

Assume one active host initially, with saved endpoints and an explicit host
switcher. Simultaneously monitoring many hosts can be added after one-host
workflows are excellent. Namespace all state correctly from the outset.

## Research baseline and confidence

OpenCode's latest published release observed during this review is
[v1.18.29](https://github.com/anomalyco/opencode/releases/tag/v1.18.29), published
2026-09-04, tag commit `16747470f976aca3d362ad730bcd3fe82ecc2c9a`.
The installed OpenCode app reports the same version. Its **new tabs/home layout**
and **OC-2 theme** were inspected directly, including an existing conversation,
the review pane, home/session navigation, and settings. The hosted
`app.opencode.ai` client was also opened; it displayed a development build and
had no execution server connected in that browser. This is not an end-to-end
OpenCode execution or performance benchmark.

Source was checked out separately under `/tmp/whip-opencode-v1.18.29-research`.
Only source was read; its dependencies were not installed and its runtime was
not started. Private session content from the installed app is not reproduced
in this plan. Repository references below pin the release instead of `dev`.

The coding app is `packages/app`. The marketing/docs site and hosted console
are different applications. Older third-party descriptions of a Tauri desktop,
xterm-based terminal, or only the old sidebar layout are not reliable descriptions
of this release.

## How OpenCode is built

| Layer | Observed implementation | Lesson for WHIP |
| --- | --- | --- |
| Shared application | SolidJS 1.9.10 and TypeScript, Solid Router 0.15.4; `@opencode-ai/app` shared by browser and desktop | Share the product renderer; keep our React decision |
| Build/workspace | Bun 1.3.14 workspaces, Turborepo 2.10.2, Vite 7.1.4, Solid Vite plugin | Vite is useful; changing our package manager or adding Turbo is unnecessary |
| Styling | Tailwind CSS 4.1.11 plus substantial component CSS, semantic custom properties, explicit state selectors | Own a coherent token system; utility classes alone do not create the appearance |
| Design-system primitives | Internal `@opencode-ai/ui`, Kobalte 0.13.11, custom icons/sprites, dialogs, menus, fields, tabs, toasts | Use accessible headless React primitives with our own styles |
| Coding-specific presentation | Internal `@opencode-ai/session-ui`; message/tool/review/prompt components | Keep domain components distinct from generic controls, initially inside one app |
| Client state | Solid contexts/stores, TanStack Solid Query, generated API clients and app-side event projections | WHIP already has synchronized SDK views; consume them instead of copying reducers |
| Long lists and file trees | TanStack Solid Virtual, Pierre Trees, application-specific tree adapters | Explicit bounds and virtualization matter more than framework slogans |
| Code/diffs | Pierre Diffs 1.2.10, Shiki; lazy diff loading and virtualized rendering | Reuse a specialist diff renderer, with daemon-owned diff semantics |
| Streaming Markdown | Marked, Remend, Shiki streaming support/workers, DOMPurify, Morphdom | Incomplete Markdown, sanitization, selection stability, and streaming cost deserve dedicated handling |
| Prompt composer | Custom contenteditable implementation with mention/attachment handling | Do not casually copy a DOM editor and inherit its selection/IME complexity |
| Terminal | Ghostty Web from a pinned OpenCode fork; backend PTY and WebSocket transport | A terminal renderer does not supply a PTY service or recovery semantics |
| Interaction | Drag-and-drop libraries, Motion, Solid Presence, fuzzy search, configurable commands/keybindings, i18n | Adopt individual behaviors as needed, not their entire dependency set |
| Desktop shell | Electron 42.3.3, electron-vite, electron-builder, updater and native integration | Their current architecture also shares a web renderer with Electron |
| Validation | Bun/Happy DOM unit tests, Playwright workflows and visual-stability/performance suites; Storybook with accessibility tooling | Test real interaction states, not just rendered screenshots |

Sources: [root manifest](https://github.com/anomalyco/opencode/blob/16747470f976aca3d362ad730bcd3fe82ecc2c9a/package.json),
[app manifest](https://github.com/anomalyco/opencode/blob/16747470f976aca3d362ad730bcd3fe82ecc2c9a/packages/app/package.json),
[UI manifest](https://github.com/anomalyco/opencode/blob/16747470f976aca3d362ad730bcd3fe82ecc2c9a/packages/ui/package.json),
[session UI manifest](https://github.com/anomalyco/opencode/blob/16747470f976aca3d362ad730bcd3fe82ecc2c9a/packages/session-ui/package.json),
[desktop manifest](https://github.com/anomalyco/opencode/blob/16747470f976aca3d362ad730bcd3fe82ecc2c9a/packages/desktop/package.json),
[Storybook manifest](https://github.com/anomalyco/opencode/blob/16747470f976aca3d362ad730bcd3fe82ecc2c9a/packages/storybook/package.json).

OpenCode uses HTTP requests and **SSE for general runtime events**, with
WebSockets for PTYs. Its current app bridges both older generated SDK methods
and a newer Effect-based protocol/client. The server includes older Hono routes
and newer Effect HTTP handlers. The public SDK uses Hey API generation. These
are layers of an ongoing migration, not one template WHIP should reproduce.
WHIP should keep its one generated JSON-RPC contract and existing WebSocket
engine. [Event handler](https://github.com/anomalyco/opencode/blob/16747470f976aca3d362ad730bcd3fe82ecc2c9a/packages/server/src/handlers/event.ts),
[server SDK and compatibility routing](https://github.com/anomalyco/opencode/blob/16747470f976aca3d362ad730bcd3fe82ecc2c9a/packages/app/src/context/server-sdk.tsx),
[public SDK](https://github.com/anomalyco/opencode/blob/16747470f976aca3d362ad730bcd3fe82ecc2c9a/packages/sdk/js/package.json).

## Why its design works

These are design interpretations of the inspected UI, supported by its source;
they are not measured usability findings.

### Surfaces and color

The new design uses a genuinely neutral gray scale. Examples include deep
`#080808`, base `#161616`, raised `#242424`, and `#2e2e2e`; light equivalents
include white, `#fafafa`, `#f2f2f2`, and `#eeeeee`. Border, hover, and pressed
states use controlled alpha layers. Semantic tokens separate background,
text, icons, borders, state, and elevation. Color is concentrated in status,
diffs, selection, focus, and small project markers.
[Palette](https://github.com/anomalyco/opencode/blob/16747470f976aca3d362ad730bcd3fe82ecc2c9a/packages/ui/src/v2/styles/colors.css),
[semantic theme](https://github.com/anomalyco/opencode/blob/16747470f976aca3d362ad730bcd3fe82ecc2c9a/packages/ui/src/v2/styles/theme.css).

### Typography and dimensions

It bundles variable Inter and JetBrains Mono Nerd Font; font preferences can
override defaults. Chrome commonly uses 12–13px type. A normal v2 button is
28px tall, with 24px and 32px alternatives, a 6px radius, 13px text/20px line
height, and carefully chosen weight/letter spacing. Tiny optical adjustments,
consistent baselines, and restrained radii produce much of the finish.
[Fonts](https://github.com/anomalyco/opencode/blob/16747470f976aca3d362ad730bcd3fe82ecc2c9a/packages/app/src/index.css),
[button metrics](https://github.com/anomalyco/opencode/blob/16747470f976aca3d362ad730bcd3fe82ecc2c9a/packages/ui/src/v2/components/button-v2.css).

### Layout and hierarchy

The latest layout emphasizes a home/project/session index and persistent
session tabs. Within a session, conversation and review are primary working
surfaces. The composer stays nearby; model/agent controls occupy its lower
edge. Tools compress into activity rows with details on demand. The review
pane supports a file tree/filter, unified or split diffs, context expansion,
line comments, and file navigation. Settings use short label/description rows
with aligned controls and clear application/server groupings.
[New layout](https://github.com/anomalyco/opencode/blob/16747470f976aca3d362ad730bcd3fe82ecc2c9a/packages/app/src/pages/layout-new.tsx),
[review panel](https://github.com/anomalyco/opencode/blob/16747470f976aca3d362ad730bcd3fe82ecc2c9a/packages/app/src/pages/session/v2/review-panel-v2.tsx),
[settings](https://github.com/anomalyco/opencode/tree/16747470f976aca3d362ad730bcd3fe82ecc2c9a/packages/app/src/components/settings-v2).

### Behavior is part of the appearance

Stable scroll position, low visual churn, incremental loading, keyboard focus,
predictable panel persistence, and a composer that preserves its contents are
essential. OpenCode even has dedicated timeline visual-stability tests. We
should judge WHIP while it is streaming, reconnecting, resizing, and awaiting
permissions, not only while idle.
[Performance tests](https://github.com/anomalyco/opencode/tree/16747470f976aca3d362ad730bcd3fe82ecc2c9a/packages/app/e2e/performance),
[regression tests](https://github.com/anomalyco/opencode/tree/16747470f976aca3d362ad730bcd3fe82ecc2c9a/packages/app/e2e/regression).

## WHIP's proposed design primitives

Design vocabulary: workspace, session, agent lineage, evidence, cell, host call,
pending request, budget, and change. The user is a developer directing several
pieces of work while reading code; the desired feel is focused and composed.

Color world: paper, graphite, charcoal, muted blue for active work, amber for
attention, restrained red for failures/removals, and green for success/additions.
Start with complete light/dark/system modes, not a large theme marketplace.

| Primitive | Starting specification and purpose |
| --- | --- |
| Typography | Self-host Inter for UI, JetBrains Mono for code; 13px chrome, 15px conversation, 12–13px code. Respect browser zoom and offer density/font-size preferences |
| Spacing | 4px base rhythm, with 8/12/16/24px group spacing; dense navigation and more generous reading space |
| Controls | 28–32px desktop controls, 6px small-control radius, 10px panel radius; larger effective targets on touch devices |
| Depth | Base, inset, raised, overlay; subtle borders and shadows for floating controls, not a card around every message |
| Status | Text + icon + optional color for queued/running/waiting/interrupted/failed. Root and descendant activity remain separately visible |
| Motion | Approximately 120–180ms for small state transitions; reduced-motion support. Streaming updates change content without moving the whole layout |
| Reading | Comfortable conversation measure, stable message anchors, accessible code selection, copy actions, and a jump-to-live control |
| Attention | Persistent request dock linked to the correct agent/tool/path; a collapsed summary remains visible while inspecting other work |
| WHIP signature | A compact agent lineage rail with current activity, pending requests, and budget pressure; selecting an agent opens its conversation or execution details |

Avoid three tempting defaults: a chat-only screen with invisible child work;
a dashboard grid of statistics above the task; and a permanently expanded
dump of Starlark, tool JSON, and mailbox digests. Replace them with conversation
plus review, a concise activity summary, and inspectable detail.

Proposed desktop arrangement (visibility is adaptive, not every pane always open):

```text
Host / workspace       Session tabs                     Search / commands
----------------------------------------------------------------------------
Sessions / agents      Conversation               Review | Files | Execution
  Root                 User request               Changed files + diff
    Explorer           Assistant response         or selected source file
    Implementer        Compact work summaries     or Starlark cells/host calls
    Reviewer           Pending permission/question
                       Composer + target + model
----------------------------------------------------------------------------
                       Optional terminal drawer
```

At ordinary laptop widths the navigation rail collapses before reading widths
become poor. On narrow screens, conversation, review, and agents become explicit
views. Preserve an obvious pending-request indicator and composer target.

## Capability comparison and full WHIP UI coverage

The example proves SDK attachment and recovery. It is not yet the product app.
The current registry contains 65 runtime operations and 36 RPC methods. Most
runtime operations already have typed access; transport and chunking RPCs
should remain invisible infrastructure rather than get their own buttons.

| Workflow | What WHIP already exposes | UI or extension needed |
| --- | --- | --- |
| Connect/select host | Initialization, capabilities, endpoint, connection state | First-run connection page, saved endpoints, actionable compatibility/offline states; same-origin default |
| Workspace/session navigation | Create/list/open/preview/rename/delete/fork, cwd | Project/session browser, search, recent items, tabs, menus; scalable search/project identity extensions below |
| Conversation | Root/child history, live text/reasoning/tool events, content refs | Rich stable timeline, collapsed activities, copy, text/code/image rendering, pageable history |
| Compose and steer | `submit`, `steer`, child input, workspace completion, multimodal parts | Drafts, attachments, mentions, model/effort, explicit recipient, send/steer distinction, queue feedback |
| Cancellation | Exact root/child turn cancellation; subtree stop | Stop the displayed authoritative turn, including one started by another client; distinguish stop-turn from stop-subtree |
| History | Clear, revision-checked rewind/fork, compaction/log/retry/config | Message actions, preview of the history cut, compaction inspector; history rewind must not imply filesystem undo |
| Session/run settings | Model/effort/defaults, reload, autotitle, run configuration | Scoped session settings with idle-only constraints and clear host-default actions |
| Goals | Set/run/from-context | Goal panel with current objective, explicit run action, and truthful activity status |
| Schedules | List/create/delete | Human-readable schedule editor, timezone/next-run detail where supplied, task association and delete control |
| Recursive agents | Tree, status, transcript, input, stop/delete, pending-mail counts | Agent rail, breadcrumbs, direct child conversation, descendant activity and failure explanation |
| Budgets/authority | Budget cap, used/reserved amounts, capabilities/revocation | Per-agent/inherited budgets and capability inspector; totals must avoid double-counting ancestry |
| Starlark execution | Stream/presentation events and raw transcript | Cells, code, prints, host calls, timing/results/errors, worker restarts; verify historical reconstruction and expose unavailable detail honestly |
| Pending requests | Questions/answers/dismissal; permissions/decisions/mode/rules/forget | Accessible question cards, allow/deny/remember scopes, rules management, multi-client resolution, attention index extension |
| Shell/tool operation | `shell.run`, `tool.schema`, `tool.call`, tool configuration | Shell output/interactive tool terminal and a secondary tool inspector; keep arbitrary tool JSON out of the primary workflow |
| Interactive terminal | Active tool terminal output and ephemeral input | Terminal renderer; resize/reconnect semantics need audit. User-created terminal tabs need a dedicated daemon lifecycle |
| Context | `context.audit` summary, agent runtime state | Applied-context inspector; richer source/skill browsing needs dedicated read APIs, not automatic model-context admission |
| Command inputs | Root `inbox` snapshot/collection | Queued input and delivery status; do not label this the inter-agent mailbox |
| Agent mail/shared state | Agent pending counts; blackboard collection; internal durable mail store | Read-only mailbox metadata/body API; blackboard/evidence inspector; inspecting mail never marks it read/delivered/done for the agent |
| MCP | Status/reconnect/enable/disable/import status/configure; delegated attach | Integration status/settings, import sources, errors and recovery; preserve host secret references and delegated authority |
| LSP | Server status | Health and workspace status now; per-file diagnostics/editor assistance needs more API support |
| Browser/computer | Driver/status, app allow/deny, tool output | Host capability-aware controls, screenshots/content viewer; distinguish execution-host apps from browser-client apps |
| Providers | Catalogs/status, API keys, host login progress/team/project/create/cancel/logout/rotation | Complete onboarding/settings, validation, progress and reconnect; fix catalog bootstrap root dependency |
| Shared configuration | Revision-checked get/update | Scoped forms and conflict UI, secret-free client storage; appearance/drafts remain local |
| Content | Upload/download/read, grants, hashes, byte limits | Attachment lifecycle, image preview, binary downloads, progress/cancel/retry and explicit large-content reads |
| Skills | Host completion includes skill names/descriptions/warnings; CLI list/import and context assembly | Completion UI now; complete source catalog and import preview/apply need daemon services if included in full CLI parity |
| Daemon operations | Ping/status information, checkpoint/restart/stop | Host diagnostics and deliberate runtime-management actions in advanced settings; stopping a daemon cannot be undone by the browser |

Local evidence: [feature map](../../../docs/features.md),
[runtime registry](../../../internal/protocol/runtime_registry.go),
[RPC registry](../../../internal/protocol/registry.go),
[SDK session helpers](../../../packages/sdk/src/session.ts),
[SDK state](../../../packages/sdk/src/state.ts),
[example](../../../examples/client/app.tsx).

OpenCode has mature product surfaces WHIP's example lacks: project/session
navigation, tab persistence, model/provider selection, prompt context chips,
change review, file browsing, line comments, terminal tabs, shortcuts,
notifications, themes, and multi-server management. Its source also supports
working-tree/branch/turn review modes, session archive/share/fork/revert, and
workspace/worktree flows. These have different backend costs; a UI cannot add
them simply by installing the same components.
[Session/review modes](https://github.com/anomalyco/opencode/blob/16747470f976aca3d362ad730bcd3fe82ecc2c9a/packages/app/src/pages/session.tsx#L660),
[session actions](https://github.com/anomalyco/opencode/blob/16747470f976aca3d362ad730bcd3fe82ecc2c9a/packages/app/src/pages/session/use-session-commands.tsx).

WHIP's opportunity is deeper visibility into its own recursive architecture:
retained agent relationships, precise turn states, bounded evidence, explicit
mail delivery, Starlark execution, and budgets. Do not replace those semantics
with an OpenCode-style fixed Build/Plan agent selector without a separate runtime
design. Likewise, a command finishing is not a declaration that descendants,
mail, goals, and schedules have all finished.

## Backend gaps to close deliberately

### 1. Host bootstrap and navigation metadata

`Providers.catalogs()` defaults to a rootless query, but `Server.query` opens a
root for every runtime query except `session.list`. This prevents a natural
model picker before session creation. Move the catalog service to a genuinely
host-scoped read path. Test a fresh host with no sessions and no credentials.
Sources: `packages/sdk/src/services.ts:24`, `internal/daemon/query.go:54`.

Workspace completion also requires an existing root. A first-session folder
picker needs a bounded host-side directory read, without creating a throwaway
agent session. A browser file picker sees the browser machine, which may be a
different machine from the execution host. Source: `internal/daemon/completion.go:30`.

The lightweight session catalog has title/cwd/model/provider metadata, but no
running/attention counts or full search. Cwd is truncated to 128 characters.
Do not group projects or key caches using the displayed truncated path. Add
stable workspace identity/full canonical metadata and bounded server-side
search/filtering as navigation needs it. Add archive/pin mutations only if
adopted as product actions; the existing `pinned` field is not an exposed pin API.

### 2. A host-wide attention query

An unopened root must not become invisible when it needs a decision. Add a
bounded host-level summary/list of pending questions, permissions, and active
root/child counts, with a revision and explicit invalidation or observed polling.
It should read lightweight state without opening every root or consuming a root
subscription per session. Do not bypass the 16-root subscription cap by silently
opening more connections.

Keep this separate from agent mail. Questions currently include in-memory
pending state in root snapshots; their interruption on daemon restart must be
represented rather than advertised as persistent unanswered forms.
Sources: `internal/session/catalog_page.go`, `internal/session/event.go`.

### 3. Workspace files and change review

WHIP has agent file tools and generic tool invocation, but no dedicated typed
workspace tree/file/git review service. Reuse the existing canonical path,
content, permission, and mutation-ordering machinery behind focused APIs:

- Bounded directory listing, search, file metadata/content, and change revision.
- Git status and per-file diff against an explicit baseline: working tree or
  branch base first, including untracked/binary/deleted/renamed files.
- Lazy file/diff bodies as scoped references; distinguish errors from an empty diff.
- External edits and agent edits both invalidate the viewed revision.
- Human edits, if selected, submit expected content hashes/revisions and fail
  with a conflict rather than overwrite another client or agent's changes.

A repository's working-tree diff is not necessarily this session's work.
Several roots and humans can edit the same workspace. Label the first release's
diff **Workspace changes**. Per-turn/session attribution and filesystem rollback
require recorded baselines or isolated workspaces and are separate extensions.
WHIP's existing history rewind and transcript fork do not supply either feature.

The first review release should inspect and comment on changes, not silently
stage, commit, push, discard, or reset files. Git mutation UI and worktree creation
should be separately specified with concrete conflict/dirty-workspace behavior.

### 4. Mail, execution inspection, and context

Add bounded, read-only `agent_messages` metadata/body access. Reuse existing
storage and content grants. `root.collection("inbox")` currently reads the
execution-input inbox, so it cannot be repurposed as an agent-mail browser.

For execution inspection, first reconstruct cells/results from raw transcripts
and existing operation records. Investigate which timing/print/restart details
survive the rolling event window. Add a paged read projection only for necessary
durable information; do not create a second transcript store or pretend omitted
live detail is recoverable.

Context audit presently returns label/byte/note rows. Rich context/skill source
inspection needs structured references and scoped reads. Reading them in the UI
must not expand the model's context or execute skill instructions.

### 5. Rich attachments and terminals

The example app appends an uploaded reference ID to prompt text. That is not
finished multimodal attachment UX. Existing `SubmitPayload.parts` supports
text/image data URLs; large uploaded images and context excerpts need a typed,
host-resolved attachment/reference path with explicit root/agent association,
limits and provider capability checks. Do not copy large image bodies back into
WebSocket frames to work around an absent attachment contract.

Existing terminal input targets an active tool terminal. Free-standing user
terminal tabs need create/list/attach/resize/close, process ownership, bounded
scrollback, exit status, and reconnect behavior. Input remains ephemeral. For
multiple viewers, choose a clear input/resize owner with explicit takeover;
viewer resize must not make the terminal oscillate. This ownership is resource
coordination, not a new user-authentication scheme.

## Recommended application stack and ownership

| Choice | Decision and reason |
| --- | --- |
| App | React 19 + TypeScript; one new private `packages/app` workspace (`@whip/app`) |
| Build | Vite with React plugin; retain npm/Node 24. No SSR, Next.js server, Bun migration, Turbo, or second backend |
| Routing | React Router in client-side declarative mode. Deep links identify host/root/agent and panel; normal browser back/forward works |
| Styling | Tailwind v4 for layout plus small component CSS and semantic variables; self-host fonts and assets |
| Accessible controls | Radix Primitives, styled for WHIP. Use cmdk for the command menu and a small shared searchable-picker surface; do not install a complete pre-styled template |
| Icons/panes | Lucide React and react-resizable-panels; one icon scale and keyboard-operable splitters |
| Live runtime data | `@whip/sdk/state` + `/react`; no Redux/Zustand copy of transcripts, command handles, subscriptions, or agent trees |
| Other host reads | TanStack Query only for bounded host/file/settings reads not owned by SDK views; cache keys include runtime/root/workspace identity and revision. Never put provider keys, terminal input, or auto-retried command submissions in it |
| Streaming Markdown | Evaluate Streamdown with its code plugin behind one renderer boundary; standalone rendering only, no AI SDK conversation/transport hook. Gate on strict CSP, sanitization, streaming selection, and bounded work |
| Code review | Pierre Diffs React bindings, lazy loaded, with worker support and strict content bounds. Test actual Safari before adopting the rendering configuration |
| Composer | Native textarea plus attachment/context chips for the first complete workflow. Add Lexical only if inline atomic mentions and richer editing demonstrably require it; explicitly test IME, undo, paste and caret behavior |
| File editing | If selected, try the chosen diff/file renderer's editing support in a small spike; choose CodeMirror 6 if that is insufficient. Never ship two full editors by default |
| Terminal rendering | Default to xterm.js for a first compatibility spike; compare Ghostty Web against the exact PTY/browser needs before adding it. OpenCode's pinned fork is a reason to evaluate, not an automatic dependency choice |
| Tests/design development | Existing Playwright and daemon fake-provider acceptance; add React Testing Library/Vitest for interaction units and a small Storybook for reusable component states. Keep all tooling in the existing workspace |

Library evidence: [Vite](https://vite.dev/guide/),
[React Router](https://reactrouter.com/start/declarative/installation),
[Tailwind tokens](https://tailwindcss.com/docs/theme),
[Radix](https://www.radix-ui.com/primitives/docs/overview/introduction),
[cmdk](https://github.com/dip/cmdk), [Lucide](https://lucide.dev/guide/react),
[resizable panels](https://github.com/bvaughn/react-resizable-panels),
[external stores](https://react.dev/reference/react/useSyncExternalStore),
[TanStack Query](https://tanstack.com/query/latest/docs/framework/react/overview),
[Streamdown](https://github.com/vercel/streamdown),
[Diffs](https://diffs.com/docs), [Lexical](https://lexical.dev/docs/intro),
[CodeMirror](https://codemirror.net/examples/basic/), [xterm.js](https://xtermjs.org/).

Versions for new WHIP dependencies must be pinned and validated at implementation;
the OpenCode versions above document the reference snapshot, not upgrade targets.

The application owns only navigation, pane sizes, selections, transient filters,
drafts, attachment progress, and local preferences. One app-level lifetime owns
each client and opened SDK view; React components borrow them. Closing a tab
disposes its observation, not the daemon session. StrictMode mounting cannot
dispose a singleton and then reuse the closed object.

Use one UI action registry for labels, shortcuts, palette entries, and local
availability. It references generated protocol names; it is not another wire
operation registry. Contextual Escape should close a popover/selection before
considering execution cancellation; terminal and composer keys retain their
normal meaning.

Start with `packages/app/src/{components,features,styles}` and small browser
platform helpers for downloads, links, clipboard, and notifications. Extract a
separate UI package only when a second consumer needs it. Later Electron imports
the same app and supplies its platform boundary; it does not move credentials or
runtime execution into the renderer.

## Delivery phases

### Phase 0 — Product contract and design proof

- [ ] Settle the three open scope questions and choose the first daily workflow.
- [ ] Capture an operation-to-workflow inventory from the current generated
  registry, including CLI-only gaps, unavailable host features and internal RPCs.
- [ ] Make realistic design fixtures: a completed conversation, concurrent root
  and children, blocked permissions, an interrupted operation, a large diff, and
  a reconnecting view. Use synthetic content, never private session screenshots.
- [ ] Specify light/dark tokens and interaction states. Validate the proposed
  layout at laptop, wide desktop and mobile widths before building every screen.
- [ ] Run narrow dependency spikes for Markdown/diff CSP and Safari behavior.

**Gate:** agreed visual direction, complete workflow map, and a short list of
necessary API additions. No generic dashboard scaffold is considered a product.

### Phase 1 — Application shell and effortless local attachment

- [ ] Add the React/Vite app, routes, tokens, common controls and component stories.
- [ ] Implement client/view ownership, recovery storage and host-scoped local
  preference/draft keys. Keep terminal input and secrets out of browser persistence.
- [ ] Add host connection/setup, workspace/session browsing and durable tabs.
- [ ] Fix rootless provider catalog lookup. Provide working first-run provider
  onboarding, model defaults, and a clear unavailable-provider state.
- [ ] Package web assets with WHIP releases and provide a `whip web` entrypoint.
  Serve assets from the daemon's enabled HTTP listener and use same-origin API
  discovery by default. Stop requiring users to guess port 8080 or paste a port
  from an ephemeral endpoint into a demo form.
- [ ] Development uses Vite against an explicitly configured existing daemon.
  A running daemon with networking disabled requires an explicit enable/restart
  path; opening the web UI never silently replaces a live runtime.

**Gate:** fresh installation → browser → provider setup → create/reopen session;
deep-link reload and offline/reconnect states work. No secret or build dependency
escapes into the renderer; no remote CDN assets are needed.

### Phase 2 — Daily conversation and human interaction

- [ ] Build the rich timeline with stable identities, grouped tools, reasoning
  disclosure, copy actions, history pagination and anchored scrolling.
- [ ] Implement drafts, text/image/context attachments, host-side completion,
  model/effort selection, clear recipient, send/steer, and durable receipt status.
- [ ] Implement questions, dismissal, allow/deny/remember permission decisions,
  and the initial request dock. Responses converge across browser and TUI.
- [ ] Offer exact-turn cancellation from snapshot state, including turns
  initiated by another client. Preserve unresolved submission identities.
- [ ] Add session rename/delete/fork and revision-checked clear/rewind controls.

**Gate:** two clients share one session with no duplicated messages or work;
reload/reconnect preserves drafts and converges; permission answers resolve once;
scrolling up, selecting text, copying and IME entry remain stable during streaming.

### Phase 3 — WHIP's recursive work and attention surfaces

- [ ] Implement agent tree/breadcrumbs, lazy child transcript inspection, explicit
  child input, turn stop, subtree stop/delete, status and budget/capability detail.
- [ ] Implement the Starlark/execution inspector using existing durable sources,
  with explicit missing/truncated state for non-recoverable live detail.
- [ ] Add the host-wide attention/index read service and an attention view that
  discovers requests without opening every root.
- [ ] Add the read-only agent mailbox API and mailbox/blackboard/evidence views.
- [ ] Add goals, schedules, usage/budgets, compaction settings/history, and applied
  context inspection. Handle history revisions and lazy collection pagination.

**Gate:** a user can tell which agent needs help, what it is doing, what it has
read, why it is waiting, and what budget remains. Observing children or mail
does not alter agent context or delivery state. Root completion never masks
active descendants or pending requests.

### Phase 4 — Files and code review

- [ ] Add scoped workspace tree/search/file/git-read APIs and SDK helpers, with
  bounded responses, content references, revisions and external-change refresh.
- [ ] Build file tree, quick open, file tabs, syntax highlighting, image/binary
  previews and the Workspace changes pane.
- [ ] Support working-tree and explicit branch-base comparisons; unified/split
  diffs, lazy hunks/context, filtered navigation and large-file placeholders.
- [ ] Add line-selection comments/context chips. A comment records path, side,
  range and content revision; stale lines require reanchoring or explicit context.
- [ ] Keep review comments as local draft context until explicitly submitted to
  the selected agent. This is not a multi-user code-review database.

**Gate:** real repository changes, untracked files, deletes/renames, binaries,
large diffs, external edits and simultaneous agent writes are understandable.
An error cannot be rendered as “No changes.” Review does not claim authorship
or provide unsafe file rollback based on transcript rewind.

### Phase 5 — Complete host controls and selected editing/terminal scope

- [ ] Finish providers, settings revisions/conflicts, MCP lifecycle/import,
  LSP status, browser drivers, computer app policy, tools and daemon diagnostics.
- [ ] Add skill/source catalog UI and daemon-backed import preview/apply if
  complete CLI parity is included. Do not use a browser-local filesystem scan.
- [ ] Complete interactive tool-terminal rendering and input parity.
- [ ] If selected: ship revision-checked lightweight editing and standalone PTY
  tabs with resize/input ownership, bounded scrollback and explicit exit behavior.
- [ ] Add keyboard customization, complete command palette/help, appearance,
  notification preferences, and accessible mobile attention/conversation views.

**Gate:** every inventoried existing TUI workflow has an intentional web path;
every browser limitation has a clear host-aware alternative. Newly selected
editor/terminal behaviors have their own daemon-backed concurrency tests.

### Phase 6 — Release quality and simplification

- [ ] Run full protocol/SDK/Go acceptance plus product browser suites, packed
  package checks, generated-contract drift, `task check`, and affected race suites.
- [ ] Test Chromium, Firefox and actual Safari on macOS; automate WebKit as
  additional coverage. Test keyboard-only navigation, VoiceOver, reduced motion,
  contrast, zoom, touch targets, IME, drag/drop and clipboard permissions.
- [ ] Add visual regression fixtures for dark/light, empty/loading/error/stale,
  pending/resolved requests, agent activity, diffs, and responsive layouts.
- [ ] Test two simultaneous clients, lost acknowledgements, restart, runtime
  replacement, stale cancellation, conflicting settings/file writes, slow
  consumers, closed tabs, browser sleep, history rewinds and subscription limits.
- [ ] Measure the performance targets below; profile before optimizing.
- [ ] Remove duplicated caches, operation lists, unused packages, demo-only
  behavior, and transport calls outside the SDK. Keep the minimal SDK example
  as an integration sample, separate from product development.
- [ ] Update feature map, roadmap, protocol reference, SDK examples, architecture,
  ownership/concurrency docs, and local/trusted-network setup instructions.

**Gate:** daily WHIP coding work can be completed in the web application with
the agreed review/edit/terminal scope, all existing runtime workflows covered,
and no loss of recovery or permission semantics.

## Acceptance metrics and scenarios

Targets below are proposed product budgets, not measurements already achieved.
Establish a repeatable reference Mac/browser, dataset and daemon fake-provider
load, recording median and p95/p99 as appropriate.

| Measure | Initial target or invariant |
| --- | --- |
| Local typing/pane interaction | p95 input-to-paint below 50ms under representative concurrent-agent streaming |
| Received event to visible state | p95 below 50ms; measure separately from daemon event polling/network latency |
| Commit to visible state | Instrument end-to-end and set the budget from the existing event-delivery baseline; preserve durability settings |
| Warm session switch | p95 below 150ms for retained views, without waiting for unrelated host queries |
| Reconnect | Local representative snapshot/replay convergence below 1s after connection availability; no duplicate submissions |
| Retained runtime state | Preserve SDK's 8MiB/view and 512-message/agent defaults; lazy content is bounded separately |
| App-wide memory | Explicit budget for cached views/file bodies; open tabs do not all retain maximum-sized live views |
| Large presentation | 10,000-message stored history, 100 retained agents, large tool bodies and 1,000-file diff metadata; fetch/render only bounded active pages |
| Subscriptions | Maximum 16 watched roots/connection; clearly distinguish an open tab from a live watched root |
| Accessibility | Zero critical automated violations; manual keyboard and VoiceOver workflows pass |
| Correctness | No scroll-anchor loss on prepend; no stale root/agent mixing; no silent empty-on-error; no old-history revision mixing |

Inactive tab eviction must be explicit and reconstructible; do not silently
evict a user-designated watched root. A lightweight attention index allows
awareness without maintaining a full transcript view for every session.

Bound Markdown, syntax tokenization, images, terminal scrollback and diff caches
in addition to SDK JSON payloads. Move demonstrated expensive work to workers;
virtualizing DOM does not itself bound downloaded/parsed data.

## Mistakes to avoid

1. **Copying an old OpenCode teardown.** This release uses the new tabs/home
   design, Electron, Ghostty Web and a protocol transition; pin evidence.
2. **Copying the migration.** OpenCode still carries old/new layouts, clients,
   adapters and stores. WHIP has deliberately removed legacy protocol paths;
   keep one product design and one SDK synchronization model.
3. **Treating every event as a message.** Keep stable message/tool/cell identity
   and obey delta versus cumulative updates; collapse detail only in presentation.
4. **Equating an empty screen with no data.** Loading, failed, truncated,
   unavailable and stale are distinct. OpenCode's reviewed VCS query currently
   catches a read error and returns an empty array; WHIP should expose the error.
5. **Hiding background decisions.** A selected-session-only request list misses
   unopened roots. Build the attention query rather than opening every root.
6. **Misleading “done,” “cancel,” “fork,” or “undo.”** Root turn, subtree,
   local wait, transcript history and filesystem effects are different things.
7. **Making the product another harness.** No renderer-owned provider loop,
   scheduler, tool executor, automatic retry of uncertain work, or secret storage.
8. **Choosing complexity before evidence.** No CRDT editor, full IDE, custom
   terminal emulator, second history store, plugin framework, or broad design
   package extraction is needed to deliver the first excellent workflow.

## Files and surfaces expected during implementation

- New `packages/app/`: React renderer, routes, features, styles, browser helpers,
  stories and product tests. Root `package.json`, lockfile and `Taskfile.yaml`.
- Existing `packages/sdk/src/{services,session,state,react}.ts`: narrowly scoped
  missing helpers/projections only; synchronization remains framework independent.
- Existing `internal/protocol/{registry,runtime_registry,types}.go` and related
  contract files: new typed reads/mutations; regenerate `packages/protocol`.
- Existing `internal/daemon/{query,client_control,network,provider_service}.go`
  plus focused workspace/attention/terminal services as needed. Query execution
  stays outside serialized actor transitions; mutations use existing admission.
- Existing `internal/session/{catalog_page,collection_page,event}.go` and mail/
  content/operation readers: bounded projections and revisions, not parallel stores.
- Existing `internal/tools`, workspace coordination, and host services: reuse
  file authorization, process management, content handling and mutation safeguards.
- New web-asset embedding/build integration and CLI entrypoint under `cmd/whip`.
- Existing docs above; add `docs/web-app.md` for setup and everyday workflows.

The next concrete step is Phase 0: agree on the workspace/review layout and
editor/terminal scope, then implement one complete connect → prompt → approve →
inspect child → review changes workflow before expanding the rest of the UI.
