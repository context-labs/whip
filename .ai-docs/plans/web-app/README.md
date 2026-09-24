# WHIP web application: OpenCode research and proposed delivery plan

Historical research and delivery record. The maintained source for frontend
architecture and design decisions is [docs/frontend.md](../../../docs/frontend.md).
Later decisions, including session tabs, supersede the original proposals below.
Use this plan for rationale and dated evidence, not as a fresh implementation brief.

Branch: `whip-rlm`

Status: Implementation in progress, 2026-09-06. Scope and stack decisions below
are accepted; the phase gates remain open until their validation is complete.

Current evidence: implementation and the full local `task check` and release
acceptance gates pass. Chromium/Firefox product tests, actual macOS Safari,
all 65 theme fixtures, packed consumers, and the full-size performance/correctness
workload pass. The workflow inventory identifies every registered operation and
its product surface or explicit deferral. See [acceptance evidence](ACCEPTANCE.md)
for exact coverage and outstanding gates. Physical iOS/Android and VoiceOver
validation remain outstanding; automated viewport tests do not establish those
results. An implementation checkbox below does not close a phase's release gate.

## Goal

Build a React web application for doing real coding work with WHIP: direct an
agent, understand concurrent work, answer requests, inspect execution evidence,
and continue confidently after reconnecting. Match OpenCode's visual
discipline and everyday interaction quality, while making WHIP's recursive
runtime understandable.

The existing TypeScript SDK is the connection and synchronization foundation.
The daemon remains the sole execution authority. An application screen must
not recreate its command journal, event reducer, scheduler, context assembly,
provider login, permission policy, or agent lifecycle.

## Decisions already established

- React and TypeScript; web first, Electron later. macOS first for desktop.
- Conversation and agent operation first. **No file editing, code review, or
  terminals in the first release.** Those are later product milestones.
- Basic mobile support: readable conversations, session/agent navigation, prompt
  submission, questions, permissions and stopping work, with responsive settings.
- OpenCode is the reference for restrained color, component finish, layout and
  conversation behavior. Its IDE-like feature scope is not our initial scope.
- TanStack Router, Query and Virtual; use other TanStack tools where they serve
  an actual workflow. Base UI supplies accessible interaction primitives.
- StyleX is the application and component styling system. Own a complete WHIP
  component library as a separate package from the start.
- Theme support begins with the foundation: every theme supported by the TUI,
  including shipped variants, automatic appearance and custom JSON themes.
  The web app must not launch with only a light/dark subset.
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

## Scope and remaining design choices

The three research questions are answered. Basic mobile details above are the
proposed operational interpretation, not a promise of a separate mobile product.
Ship one responsive app; no mobile-specific runtime, background push service,
offline execution queue, or PWA installation work in this milestone.

Runtime controls remain in scope: sessions, root/child conversations, requests,
goals/schedules, budgets, context/execution inspection, providers and host settings.
Read-only code fences and tool output inside conversations remain useful;
repository browsing, diffs, editing and interactive terminal surfaces are deferred.
The TUI parity inventory must explicitly mark these deferrals rather than claim
complete TUI parity. Existing daemon/SDK functionality remains available.

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
| Coding-specific presentation | Internal `@opencode-ai/session-ui`; message/tool/review/prompt components | Keep domain components inside the shared app, separate from our generic `@whip/ui` library |
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
pending request, budget, and attention. The user is a developer directing several
pieces of work and deciding when to intervene; the desired feel is calm enough
for sustained reading, with precise and visible control over execution.

Color world: paper, graphite, charcoal, muted blue for active work, amber for
attention, restrained red for failures, and green for completed operations.
Use the complete existing TUI theme catalog from the start, including custom
JSON themes and automatic appearance. The palette above describes the intended
restrained use of color; each selected theme supplies the actual color values.

| Primitive | Starting specification and purpose |
| --- | --- |
| Typography | Self-host Inter for UI, JetBrains Mono for code; 13px chrome, 15px conversation, 12–13px code. Respect browser zoom and offer density/font-size preferences |
| Spacing | 4px base rhythm, with 8/12/16/24px group spacing; dense navigation and more generous reading space |
| Controls | 28–32px desktop controls, 6px small-control radius, 10px panel radius; at least 44px effective touch targets and 16px mobile input text |
| Depth | Base, inset, raised, overlay; subtle borders and shadows for floating controls, not a card around every message |
| Status | Text + icon + optional color for queued/running/waiting/interrupted/failed. Root and descendant activity remain separately visible |
| Motion | Approximately 120–180ms for small state transitions; reduced-motion support. Streaming updates change content without moving the whole layout |
| Reading | Comfortable conversation measure, stable message anchors, accessible code selection, copy actions, and a jump-to-live control |
| Attention | Persistent request dock linked to the correct agent/tool/path; a collapsed summary remains visible while inspecting other work |
| WHIP signature | A compact agent lineage rail with current activity, pending requests, and budget pressure; the same agent identity appears in breadcrumbs, tool summaries, approval cards, composer recipient and execution detail |

Avoid three tempting defaults: a chat-only screen with invisible child work;
a dashboard grid of statistics above the task; and a permanently expanded
dump of Starlark, tool JSON, and mailbox digests. Replace them with conversation
plus an optional agent inspector, a concise activity summary, and inspectable detail.

Proposed desktop arrangement (visibility is adaptive, not every pane always open):

```text
Host / workspace       Session title + activity         Search / commands
----------------------------------------------------------------------------
Sessions               Conversation               Agents | Details
  Recent work          User request               Root → children + status
  Needs attention      Assistant response         Selected agent / Starlark
                       Compact work summaries     Budget / context / goal
                       Pending permission/question
                       Composer + target + model
----------------------------------------------------------------------------
                       Connection / recovery status when relevant
```

At ordinary laptop widths the navigation rail collapses before reading widths
become poor. The agent/details rail can be collapsed; nothing replaces it with
an empty review pane. The initial proposal used the sidebar and browser history
without app tabs. The subsequently accepted [session-tab design](../session-tabs/README.md)
supersedes that choice; tabs now represent the window's working set.

On narrow screens, the conversation fills the viewport. Sessions open in a
navigation drawer; agents and details open in a sheet or dedicated route. Keep
the composer above the software keyboard, honor safe areas and dynamic viewport
height, preserve the reading anchor on rotation, and expose actions without hover.
Basic mobile approval means showing the full scope of a request, not shortening
away paths or arguments. Show disconnected/stale state after browser suspension;
do not promise background monitoring while the browser is asleep.

## Capability comparison and full WHIP UI coverage

The example proves SDK attachment and recovery. It is not yet the product app.
The current registry contains 65 runtime operations and 36 RPC methods. Most
runtime operations already have typed access; transport and chunking RPCs
should remain invisible infrastructure rather than get their own buttons.

| Workflow | What WHIP already exposes | UI or extension needed |
| --- | --- | --- |
| Connect/select host | Initialization, capabilities, endpoint, connection state | First-run connection page, saved endpoints, actionable compatibility/offline states; same-origin default |
| Workspace/session navigation | Create/list/open/preview/rename/delete/fork, cwd | Project/session browser, search, recent items and menus; scalable search/project identity extensions below |
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
| Shell/tool operation | `shell.run`, `tool.schema`, `tool.call`, tool configuration | Read-only tool output and tool settings/inspection. A direct shell runner and arbitrary tool console are deferred with terminal surfaces |
| Interactive terminal | Active tool terminal output and ephemeral input | Deferred. Show that interactive input is required and identify the TUI path; preserve available stop/deny controls |
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

### Deferred: workspace files and change review

This research is retained for a later milestone. None of these services or
renderers is needed to complete the initial conversation application.

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

### 3. Mail, execution inspection, and context

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

### 4. Rich attachments

The example app appends an uploaded reference ID to prompt text. That is not
finished multimodal attachment UX. Existing `SubmitPayload.parts` supports
text/image data URLs; large uploaded images and context excerpts need a typed,
host-resolved attachment/reference path with explicit root/agent association,
limits and provider capability checks. Do not copy large image bodies back into
WebSocket frames to work around an absent attachment contract.

### Deferred: interactive terminals

Existing terminal input targets an active tool terminal. Free-standing user
terminal tabs need create/list/attach/resize/close, process ownership, bounded
scrollback, exit status, and reconnect behavior. Input remains ephemeral. For
multiple viewers, choose a clear input/resize owner with explicit takeover;
viewer resize must not make the terminal oscillate. This ownership is resource
coordination, not a new user-authentication scheme.

### 5. Reachable mobile origin and browser capabilities

The current SDK uses `crypto.randomUUID` for command identities and
`crypto.subtle` for content integrity (`packages/sdk/src/util.ts:40–48`). Those
browser capabilities need a secure context. A phone opening a LAN IP over plain
HTTP does not inherit the desktop localhost exception. A responsive layout
alone therefore does not make the existing SDK work on that origin.

The proposed initial phone setup uses an externally supplied, browser-trusted
HTTPS endpoint for both the app and proxied daemon API/WebSocket, with the
existing host/origin checks configured. This adds no application authentication
or hosting service. Keep desktop localhost HTTP supported. Test actual phone
access, not only a desktop viewport emulator, and present capability failures
before the first submission. If plain-LAN-HTTP phone support is required, specify
the SDK identity/hash implementation separately; never skip integrity checks.
[Browser UUID requirements](https://developer.mozilla.org/en-US/docs/Web/API/Crypto/randomUUID),
[secure contexts](https://developer.mozilla.org/en-US/docs/Web/Security/Defenses/Secure_Contexts).

## Application stack

The user's stack choices replace the earlier React Router/Radix/Tailwind
proposal. Keep the existing React 19, TypeScript, npm workspaces and Node 24.
Pin compatible dependency versions during the foundation spike; the OpenCode
versions document research, not installation targets.

| Layer | Decision and purpose |
| --- | --- |
| Build | Vite + React plugin, static client application; the Go daemon serves release assets. TanStack Start/SSR and a JavaScript backend are unnecessary for this attach-only client |
| Navigation | TanStack Router, typed file routes and validated search state; lazy feature routes and normal browser back/forward |
| Host reads | TanStack Query calls typed SDK services for providers, configuration, host attention and other reads not owned by synchronized SDK views |
| Live sessions | Existing SDK SessionView/SessionListView and React external-store adapter own snapshot/replay/history; TanStack does not replace their state machine |
| Large views | TanStack Virtual for long session/agent lists and variable-height conversation rows; stable identities and scroll anchors remain product responsibilities |
| Forms | TanStack Form for multi-field settings, schedules and onboarding, composed with our fields. The composer keeps its application-owned draft and normal text-input behavior |
| Keyboard | Evaluate TanStack Hotkeys for scoped registration and shortcut customization; actions remain a small app registry shared by menus and help |
| Components | Our `@whip/ui`, built with `@base-ui/react` primitives and semantic HTML |
| Styling | StyleX for tokens, themes, variants, responsive layouts and component styles; self-host fonts and icons |
| Markdown/code | One app-owned renderer with StyleX-styled elements. Evaluate TanStack Markdown against WHIP transcript fixtures; choose it only if syntax and streaming behavior fit. Read-only code highlighting is lazy and bounded |
| Tests/catalog | Storybook with Vite for our library and app compositions; Vitest/Testing Library, existing Playwright and fake-provider acceptance |

TanStack Router explicitly supports coordinating an external cache. Its Vite
plugin provides file-route generation and code splitting. Use those features
without an SSR integration package in this client-only app.
[External loading](https://tanstack.com/router/latest/docs/guide/external-data-loading),
[Vite setup](https://tanstack.com/router/latest/docs/installation/with-vite).

TanStack's current Markdown package has a focused syntax profile and optional
accumulated-AI-stream handling. It does not claim full CommonMark/GFM support.
The renderer spike must test nested lists, tables, code fences, incomplete links,
raw HTML, links/content references and long outputs before adoption. A renderer
must not dictate a second styling framework or take over WHIP's conversation
state. If unsuitable, evaluate a React Markdown renderer behind the same boundary.
[Markdown contract](https://tanstack.com/markdown/latest/docs/overview).

Use TanStack tools deliberately: Router/Query/Virtual are core decisions; Form
serves real forms, and Hotkeys serves keyboard commands. Add Table only when a
screen actually needs tabular interaction. No TanStack DB replica, extra global
store, or TanStack AI/provider loop is required.
[Virtual chat reference](https://tanstack.com/virtual/latest/docs/framework/react/examples/chat),
[Form](https://tanstack.com/form/latest/docs/overview),
[Hotkeys](https://tanstack.com/hotkeys/latest/docs/overview).

## Package boundaries

Add three private workspaces, each with a concrete responsibility. Keep one root
lockfile; extend the existing workspace globs with `apps/*`. Do not reorganize
the Go tree or turn every feature directory into a separately released package.

| Path / package | Owns | Allowed internal dependencies |
| --- | --- | --- |
| `packages/protocol` / `@whip/protocol` | Generated wire contract and validation | None |
| `packages/sdk` / `@whip/sdk` | Connections, durable commands, services, synchronized views | Protocol; React only through the existing optional adapter |
| `packages/ui` / `@whip/ui` | Tokens, themes, fonts/icons, accessible styled controls, layout primitives, stories and component documentation | None; no SDK, protocol, router, Query, host access or product state |
| `packages/app` / `@whip/app` | Shared React renderer, routes, feature controllers, conversation/agent/request components, SDK lifetime and TanStack integration | UI, SDK, protocol types where needed |
| `apps/web` / `@whip/web` | Browser entry, HTML, Vite configuration, endpoint discovery, browser storage/download/clipboard adapters and release assets | App; UI stylesheet/theme entry only as needed for bootstrapping |
| `examples/client` | Existing small SDK integration example | Existing SDK dependencies; independent of the product app |

Future `apps/desktop` imports `@whip/app` and provides Electron platform adapters.
It is not created now. UI controls know nothing about Electron, and app features
do not import Node, Electron IPC, filesystem APIs, or `@whip/sdk/node`.

The dependency direction is `web → app → ui` and `app → sdk → protocol`.
Keep generic controls and domain UI separate without creating `session-ui` yet:
`Message`, `ToolActivity`, `AgentTree`, `PermissionRequest`, `Composer`, and
`ExecutionCell` live in app feature directories, consuming UI primitives. Present
them through props/callbacks; adjacent controllers use SDK hooks. A button never
submits a daemon command by itself. These components are shared by web and
Electron because both consume the same app.

Give packages explicit exports; no imports from another package's private `src`
paths. React stays a peer of reusable React packages. Keep CSS/font side effects
declared and heavy renderers behind separate imports. Enforce dependency rules
in lint/build checks rather than relying on a diagram.

Tokens stay under `@whip/ui/tokens` and theme/font/reset entry points stay in UI;
separate tokens/icons/theme packages have no independent consumer or lifecycle.
Storybook is a development build of the UI workspace, with app stories included,
not another deployed service or production dependency. Do not create a generic
platform plugin system: inject the small set of adapters actually used.

### Source and build contract

Initially UI and app are private ESM/TypeScript source packages compiled by their
consumer's Vite build; protocol and SDK keep their existing built artifacts.
Document that consuming the private UI source requires the StyleX compiler.
This permits one CSS extraction pass across UI and app and preserves static
resolution of shared tokens. It is not a promise of a public, compiler-free
component package.

Configure the compiler for resolved workspace paths and the explicit `@whip`
paths in packed-consumer fixtures; default node_modules exclusions must not
omit our styles. Tokens need canonical, statically resolvable `.stylex.ts`
exports rather than arbitrary computed barrel exports. Verify package exports,
theme overrides, route splitting and class/CSS consistency in a clean packed
Vite consumer, not only through workspace symlinks. If a public precompiled UI
package becomes useful later, specify its JS/CSS/theme contract separately.

## One owner for each kind of state

| State | Owner and integration |
| --- | --- |
| Execution and durable truth | Daemon |
| Session snapshot, transcript, agents, requests, history revisions | SDK synchronized views; components subscribe through `/react` |
| Lightweight session list | Existing SDK list view; do not run a parallel Query list poller |
| Host settings/catalogs/attention and bounded detail reads | TanStack Query, using SDK methods and cancellation signals |
| Selected host/root/agent and shareable panel/filter state | TanStack Router |
| Drafts, theme, sidebar width and device preferences | App-owned local state/storage through platform adapters |
| Durable command acceptance and outcome | SDK CommandHandle, including delivery uncertainty and recovery |

Route loaders may call Query's `ensureQueryData` for the small host reads a page
requires. They must not copy a SessionView into the Query cache, open root event
streams on link hover, or wait for a running command to finish before rendering.
The selected session gets an explicit view lifetime with bounded retention;
preview/preload reads remain bounded and read-only. Avoid Router and Query both
maintaining independent caches for the same query result.

Query keys include persistent runtime ID, scope and all read parameters; include
history/content revisions where they define a different page. Discard/invalidate
host data on runtime replacement and refresh live availability on generation
changes. Do not key anything by truncated cwd or use JavaScript Number for
64-bit wire counters. Configure staleness, collection page caps and cache GC;
Query's time-based GC alone is not a byte budget. Do not persist the Query cache.

WHIP connection state determines whether RPCs are available. A machine can have
no public internet yet still reach a local daemon. For SDK-backed Query work,
use `networkMode: 'always'` with explicit SDK connection gating and bounded
read retries; reconnect invalidation comes from the client. A query function
still checks current connection state when it runs. Do not let browser online
state create a paused mutation which submits later without the user's action.
This is an application of Query's documented network modes, not a daemon change.
[Network modes](https://tanstack.com/query/latest/docs/framework/react/guides/network-mode).

Mutations go through the existing SDK handles. Query may wrap non-sensitive UI
actions for local pending/error feedback, with automatic retries and offline
resumption disabled; acceptance is not terminal success. Secrets bypass Query
and mutation/devtools caches: use ephemeral SDK calls from a transient field,
clear it after submission/close, and keep it out of logs and persistence. Cache
invalidation follows authoritative outcomes/events, not an optimistic claim
that work completed. Local abort, disconnect and closing a route do not cancel
daemon execution. Cancelling work always targets the authoritative turn.

App-level ownership survives ordinary React rerenders. StrictMode setup/cleanup,
navigation, host switch and disposal have tests; no component creates its own
socket or extra root subscription. Respect 16 watched roots and never exceed
the cap to make preloading feel faster. Draft persistence remains explicit
application behavior; recovery metadata does not become an offline prompt queue.

## Our component library and StyleX contract

The library is a product deliverable: consistent public APIs, documented anatomy,
states, keyboard behavior, accessibility, density and responsive treatment.
Implement the full set needed by the in-scope screens, in groups as those screens
arrive. Completeness means those workflows never need ad hoc replacement controls;
it does not require inventing date pickers, editors or charts without a consumer.

| Group | Components and design coverage |
| --- | --- |
| Foundations | Semantic color/typography/space/radius/motion/elevation tokens; full TUI theme catalog, auto and custom themes; self-hosted fonts; consistent icons; focus treatment |
| Actions | Button, IconButton, ButtonGroup/ToggleGroup, Link, Tooltip, keyboard shortcut hint |
| Forms | Field/Fieldset, Label, Input, Textarea, NumberField, Checkbox, RadioGroup, Switch, Select, searchable Combobox, descriptions and errors |
| Navigation/overlays | Tabs, Menu/ContextMenu, Popover, Dialog, AlertDialog, Drawer/Sheet, Breadcrumbs, command menu |
| Disclosure/layout | Accordion/Collapsible, Separator, ScrollArea where needed, layout/panel primitives, aligned settings rows |
| Feedback/content | Badge/StatusIndicator, Progress/Meter, Spinner, Skeleton, Alert, Toast, empty/error states, Avatar, copy action |

Build Base UI-backed controls inside `@whip/ui`. Use its compound anatomy,
controlled/uncontrolled behavior, focus management and render composition;
retain refs, event merging and accessibility props. Keep our variants modest:
purpose, size and state. Use native elements for simple presentation components.
A command menu can compose Base UI Dialog + Combobox/Autocomplete; validate its
keyboard semantics rather than automatically adding cmdk and another primitives
dependency. App code imports WHIP controls, not new unstyled Base UI controls.
[Base UI styling](https://base-ui.com/react/handbook/styling),
[Composition](https://base-ui.com/react/handbook/composition).

StyleX supplies our authored styles, not another execution or state framework:

- Define themed semantic roles with `defineVars`, invariant scale/breakpoint
  values with `defineConsts`, and shipped themes with `createTheme`. Tokens
  have designated `.stylex.ts` modules and one source of truth.
- Compose variants with `stylex.create` and `stylex.props`; expose a constrained
  `xstyle` extension prop where useful, keeping native `style` available for
  Base UI/runtime positioning. Avoid both a styling DSL and class-string helpers.
- Use Base UI state callbacks/render composition to select StyleX variants.
  Merge both className and runtime style/position values; a spread must not
  overwrite Base UI's geometry, event handlers or ref.
- Theme the document/portal host coherently. A portal escaping a themed React
  subtree must still inherit the correct variables. Test nested dialogs,
  popovers, focus return, collision positioning and theme switching.
- Use the official StyleX Vite unplugin for build-time CSS extraction, with
  runtime injection off in production. Configure plugin order with TanStack's
  router transform and React and verify generated/lazy routes. Storybook uses
  the same StyleX configuration rather than its own theme implementation.
- Keep global CSS limited to reset, fonts and documented third-party integration
  needs. No Tailwind, Emotion or parallel CSS Modules design system. Dynamic
  geometry from virtualization and popovers is legitimate runtime data.

Sources: [user-provided StyleX resources](https://stylexjs.com/docs/llm-resources),
[Vite integration](https://stylexjs.com/docs/learn/installation/vite/),
[variable modules](https://stylexjs.com/docs/learn/theming/defining-variables/).

Production CSP is a build acceptance criterion, not an assumption that extracted
CSS solves every integration. Base UI documents optional inline style elements
and a `CSPProvider`; begin with `disableStyleElements`, external equivalents for
required scrollbar rules, and no optional inline scripts. Test popover geometry,
virtual-row transforms, theme portals and lazy code rendering under the exact
production policy in Chromium, Firefox and Safari. Its provider does not govern
every style attribute; do not claim a nonce alone fixes arbitrary inline styles.
[Base UI CSP](https://base-ui.com/react/utils/csp-provider).

Every exported control gets realistic stories: light/dark, all interactive
states, long labels/content, narrow viewports, touch, keyboard and focus return.
Add visual regression and accessible-name/role checks, plus interactions for
compositions used in the app. Test nested Menu → Dialog, asynchronous Combobox
results, and fields displaying daemon revision errors. Use synthetic data;
the catalog does not need a running daemon.

### Theme parity from the first component

The source-of-truth inventory is `theme.Builtins()` and the theme loader, not
the prose list in the feature map or a fresh download of OpenCode's catalog.
The current repository has **63 embedded catalog variants (32 dark, 31 light),
plus WHIP's `light` and `dark`: 65 named shipped themes**, as well as `auto`
and user-defined JSON themes. Preserve exact IDs, including `-light` variants;
do not invent a missing counterpart or hardcode the count in application logic.
The terminal's internal unknown-background/ANSI fallback is not another named
theme to expose in the browser.

Source: [theme specification and catalog](../../../internal/tui/theme/spec.go),
[resolution](../../../internal/tui/theme/theme.go),
[syntax and Markdown roles](../../../internal/tui/theme/syntax.go),
[documented custom format](../../../docs/features.md#themes).

**Shared definitions, platform-specific presentation.** Generate the web catalog
from the existing Go definitions/resolution, including derived built-in surfaces,
syntax and Markdown colors. Generate deterministic data and static StyleX theme
modules under `packages/ui`; never maintain hand-copied TypeScript palettes.
The build exporter can initially reuse the existing Go implementation. Where
runtime custom-theme resolution needs shared Go code, extract only the neutral
specification/catalog/color logic into `internal/theme`; retain terminal styles,
terminal detection and rendering adapters under `internal/tui/theme`. Browser
runtime code does not import Go, and the daemon must not gain a TUI dependency.

Keep the generated catalog, token mapping, themes, picker primitives and preview
fixtures inside `@whip/ui`, with an explicit themes entry point. The app owns
the appearance settings screen, persistence and SDK calls. The existing package
boundaries do not require a new npm theme package.

| TUI theme roles | Web mapping |
| --- | --- |
| `bg`, surfaces `panel`/`element`/`hover` | Canvas, panels, composer/code surfaces and interaction fills |
| `text`, `muted`, `faint` | Foreground hierarchy; reserve faint colors for suitable secondary/decorative roles |
| `primary`, `onPrimary`, `accent`, `border`, `borderFocus` | Selection, controls, accents, boundaries and focus |
| `success`, `warning`, `error`, `info` | Semantic status, with text/icons as well as color |
| `link`, `emphasis`, Markdown overrides | Links, headings, strong/emphasized text, inline code and quotes |
| Syntax overrides / optional Chroma style | Read-only code fences, Starlark and syntax-highlighted tool detail |
| `diffAdd`, `diffDel` | Preserve in the contract for later review features; no diff UI is added now |

Use a single documented derivation for web-only roles such as overlays, disabled
controls and pressed fills. Palette parity does not mean forcing every faint
terminal color onto small browser text. Check contrast of actual role/background
pairs; choose or derive accessible web roles while retaining the source palette
and recording deliberate differences. Geometry, spacing and component anatomy
remain consistent across themes.

**Custom-theme parity.** Accept the existing JSON format, same-darkness defaults,
optional surfaces/syntax/Markdown blocks, ANSI indices and registered `chroma`
overrides. Normalize to explicit browser colors using shared resolution. ANSI
indices use a documented reference palette; the browser cannot observe a user's
custom terminal ANSI palette. Resolve registered Chroma styles into portable
token/style data rather than assuming a browser highlighter shares Chroma names.
Preserve effective override precedence, including explicit Chroma selection.

Provide bounded, host-scoped theme discovery/read and pure theme-resolution
services for custom themes in the execution host's `WHIP_HOME/themes`, plus an
explicit JSON-file import in the appearance UI. Validate imports using the same
schema/resolution rules; names must not overwrite built-ins. These operations
do not create roots, write the selected theme into daemon configuration, or
allow arbitrary host file reads. Report a malformed file individually while
keeping the rest of the catalog available.

StyleX compilation remains static. Imported theme data updates only a fixed
allowlist of generated CSS variables after validation; it is never passed to a
runtime compiler, evaluated, or treated as arbitrary CSS. Test this application
path under the production CSP. Shipped themes use generated `createTheme`
outputs. See [StyleX variables](https://stylexjs.com/docs/api/javascript/defineVars/)
and [theme application](https://stylexjs.com/docs/learn/theming/creating-themes/).

**Selection and persistence.** The searchable picker offers names, color swatches
and a realistic preview. Preview is reversible; dismissal restores the committed
choice. `auto` follows browser `prefers-color-scheme`; explicit named choices keep
their declared appearance. Persist the choice per client/device, applying it
before the first app paint and to the document/portal root with the appropriate
CSS `color-scheme`. Catch unavailable storage and missing/invalid themes and fall
back to auto with an understandable notice. Cache validated custom-theme data
locally for startup/reconnect; scope host-origin custom IDs by runtime/source so
equal names on different hosts do not collide. A browser choice never changes
the TUI or another device's choice.

Theme changes must update existing messages, selected text, code, dialogs,
toasts and virtualized/offscreen rows without recreating the session view,
resetting scroll position, clearing drafts or restarting execution. Use semantic
CSS variables for highlighted tokens where possible; key unavoidable rendered
color caches by theme revision. Account for portaled content and avoid a flash
of the wrong theme during startup or route navigation.

**Acceptance:** derive the exported IDs from the Go catalog and fail generation
drift checks for missing/extra variants or changed token mappings. Test a compact
component/conversation fixture in every shipped theme, plus representative
custom themes with ANSI colors, missing values and Chroma overrides. Include
contrast checks, malformed input, system-scheme changes, storage failure,
preview cancellation, startup, live streaming, portal themes, cached code and
mobile. Theme inventory and token coverage are exhaustive; run the heavier
interaction suites on representative themes instead of multiplying every
unrelated workflow test by 65.

## Delivery phases

### Phase 0 — Design proof and package/build spike

- [x] Resolve the scope questions and record TanStack/Base UI/StyleX decisions.
- [x] Prove the proposed UI/app/web source-package boundaries and exports in
  workspace and packed-consumer Vite builds. Pin compatible tool versions.
- [x] Verify StyleX extraction, token imports, Base UI state composition, portal
  themes and production CSP with a button, searchable picker and nested dialog.
- [x] Inventory all TUI themes and custom-format behavior; prove deterministic
  Go-to-UI theme generation and runtime custom-variable application under CSP.
- [x] Capture an operation-to-workflow inventory from the current generated
  registry, marking editor/review/terminal deferrals, CLI-only gaps, unavailable
  host features and internal RPCs explicitly.
- [x] Make realistic design fixtures: a completed conversation, concurrent root
  and children, blocked permissions, an interrupted operation, a long tool body, and
  a reconnecting view. Use synthetic content, never private session screenshots.
- [x] Specify shared semantic tokens and interaction states across the full
  catalog. Validate the proposed
  layout at laptop, wide desktop and mobile widths before building every screen.
- [x] Test the Markdown candidate against real-shaped synthetic streaming output
  and a TanStack Virtual timeline against variable-height/selection/scroll cases.

**Gate:** agreed visual direction, complete workflow map, and a short list of
necessary API additions. No generic dashboard scaffold is considered a product.

### Phase 1 — Component library and conversation shell

- [x] Add UI, shared app and web workspaces with enforced dependency boundaries,
  scripts, one lockfile and explicit exports. Keep SDK/example packages independent.
- [x] Build the library foundations, controls, navigation, overlays and feedback
  used by the first workflow. Add documented states and Storybook interactions.
- [x] Deliver the full shipped theme catalog, auto selection, picker/preview,
  first-paint persistence, custom-theme import/discovery/resolution and matching
  Markdown/code styling. Establish theme drift and all-theme fixture checks.
- [x] Build the conversation shell with TanStack Router, responsive session
  navigation, optional agent/details rail and a composer. No empty future panes.
- [x] Add Query/Form integration for host reads and settings; keep SDK view
  ownership, local drafts and durable command handling distinct.

**Gate:** the component catalog and realistic shell work in every shipped theme
and representative custom themes, in laptop and phone layouts; packages work
outside symlink-only development; keyboard,
screen-reader and portal behavior pass before features multiply.

Component/package evidence (2026-09-06): `@whip/ui` source and Storybook type
checks, nine theme/code invariants, all 65 generated palette contrast/label/ARIA
fixtures, and representative keyboard/focus/storage/narrow-touch interactions
pass. The isolated strict-CSP production fixture passes Chromium, Firefox and
actual Safari 26.3.1 for lazy highlighting, retained code selection/node identity,
custom Chroma attributes and portaled controls. Real protocol/SDK/UI/app archives
install in a clean consumer and render both production and Vite development
builds with source CSS extraction. These checks do not establish physical-phone
or VoiceOver coverage; the full phase gate remains open until those and the
application workflow checks below pass.

### Phase 2 — Local attachment and complete conversation workflow

- [x] Implement client/view ownership, recovery storage and host-scoped local
  preference/draft keys. Keep secrets out of caches, devtools and persistence.
- [x] Add host connection/setup, host directory selection, session browsing and
  selection restoration. Route preloading must not consume root subscriptions.
- [x] Fix rootless provider catalog lookup. Provide working first-run provider
  onboarding, model defaults, and a clear unavailable-provider state.
- [x] Package web assets with WHIP releases and provide a `whip web` entrypoint.
  Serve assets from the daemon's enabled HTTP listener and use same-origin API
  discovery by default. Stop requiring users to guess port 8080 or paste a port
  from an ephemeral endpoint into a demo form.
- [x] Development uses Vite against an explicitly configured existing daemon.
  A running daemon with networking disabled requires an explicit enable/restart
  path; opening the web UI never silently replaces a live runtime.
- [ ] Prove actual phone attachment with the documented HTTPS/origin setup and
  early browser capability checks; retain localhost HTTP for local desktop use.
- [x] Build the rich timeline with stable identities, grouped tools, reasoning
  disclosure, copy actions, bounded history pagination, TanStack Virtual where
  necessary and anchored scrolling. Pin the active/selected row as needed so
  virtualization does not discard focus or text selection during streaming.
- [x] Implement drafts, text/image/context attachments, host-side completion,
  model/effort selection, clear recipient, send/steer, and durable receipt status.
- [x] Implement questions, dismissal, allow/deny/remember permission decisions,
  and the initial request dock. Responses converge across browser and TUI.
- [x] Offer exact-turn cancellation from snapshot state, including turns
  initiated by another client. Preserve unresolved submission identities.
- [x] Add session rename/delete/fork and revision-checked clear/rewind controls.

**Gate:** fresh host → provider setup → create/reopen → prompt → stream → approve
→ inspect child → reconnect works. Two clients share one session with no duplicate
work or messages. Local connectivity works without public internet. Reload keeps
drafts; permission answers resolve once; selection, scroll anchors and IME stay
stable. Phone users can read, send, answer and stop work without hover or a keyboard.

### Phase 3 — WHIP's recursive work and attention surfaces

- [x] Implement agent tree/breadcrumbs, lazy child transcript inspection, explicit
  child input, turn stop, subtree stop/delete, status and budget/capability detail.
- [x] Implement the Starlark/execution inspector using existing durable sources,
  with explicit missing/truncated state for non-recoverable live detail.
- [x] Add the host-wide attention/index read service and an attention view that
  discovers requests without opening every root.
- [x] Add the read-only agent mailbox API and mailbox/blackboard/evidence views.
- [x] Add goals, schedules, usage/budgets, compaction settings/history, and applied
  context inspection. Handle history revisions and lazy collection pagination.

**Gate:** a user can tell which agent needs help, what it is doing, what it has
read, why it is waiting, and what budget remains. Observing children or mail
does not alter agent context or delivery state. Root completion never masks
active descendants or pending requests.

### Phase 4 — Host controls and component-library completeness

- [x] Finish providers, settings revisions/conflicts, MCP lifecycle/import,
  LSP status, browser drivers, computer app policy, tools and daemon diagnostics.
- Not applicable to the agreed conversation scope: complete CLI skill-import
  management is not promised. Host skill/source inspection and composer completion
  are included; no browser-local filesystem scan or fabricated import API is added.
- [x] Complete the in-scope component matrix and stories as provider pickers,
  permission rules, configuration conflicts and advanced controls are built.
- [x] Show unavailable or deferred interactions explicitly, including tools
  waiting for interactive terminal input. Do not silently leave a session stuck.
- [x] Add keyboard customization, complete command palette/help, appearance,
  notification preferences, and accessible mobile attention/conversation views.

**Gate:** every in-scope workflow has an intentional web path and consistent
library components; every deferred TUI-only interaction is identified honestly.
Forms expose server validation and revision conflicts without losing user input.

### Phase 5 — Release quality and simplification

- [x] Run full protocol/SDK/Go acceptance plus product browser suites, packed
  package checks, generated-contract drift, `task check`, and affected race suites.
- [ ] Test Chromium, Firefox and actual Safari on macOS; automate WebKit as
  additional coverage, plus actual iOS Safari and Android Chrome for basic mobile.
  Test keyboard-only navigation, VoiceOver, reduced motion,
  contrast, zoom, touch targets, IME, drag/drop and clipboard permissions.
- [x] Add visual regression fixtures for dark/light, empty/loading/error/stale,
  pending/resolved requests, agent activity, complex forms and responsive layouts.
- [x] Run all-theme fixture/contrast coverage, generated-catalog drift, custom
  import parity and first-paint/live-switch/portal/syntax regressions.
- [x] Test two simultaneous clients, lost acknowledgements, restart, runtime
  replacement, stale cancellation, conflicting settings writes, slow consumers,
  route churn, browser sleep, history rewinds and subscription limits.
- [x] Test Query reconnect invalidation, no queued/retried mutations, cache
  bounds, route hover behavior, private-package exports/CSS extraction and no
  Node/React leakage into unrelated SDK entry points.
- [x] Measure the performance targets below; profile before optimizing.
- [x] Remove duplicated caches, operation lists, unused packages, demo-only
  behavior, and transport calls outside the SDK. Keep the minimal SDK example
  as an integration sample, separate from product development.
- [x] Update feature map, roadmap, protocol reference, SDK examples, architecture,
  ownership/concurrency docs, and local/trusted-network setup instructions.

**Gate:** users can direct and observe agents, answer requests and manage the
in-scope runtime from the responsive web app, using our documented component
library. Recovery and permission semantics match the SDK. Editing, code review
and terminal parity are explicitly deferred, with no placeholder implementations.

### Later milestones — not part of this implementation

Repository browsing and code review, file editing, interactive tool terminals
and user PTY tabs, worktrees/Git mutation UI, and Electron packaging have separate
product and daemon contracts. Keep the research above; install none of their
editor/diff/terminal dependencies in the initial app. Inspecting a Starlark cell
or code fence is read-only conversation detail, not an editor.

## Acceptance metrics and scenarios

Targets below remain the acceptance criteria. The measured reference Mac/browser,
synthetic dataset, sample counts and limits are recorded in `docs/web-app.md` and
[the acceptance ledger](ACCEPTANCE.md). A small sample is not a p99 estimate.

| Measure | Initial target or invariant |
| --- | --- |
| Local typing/pane interaction | p95 input-to-paint below 50ms under representative concurrent-agent streaming |
| Received event to visible state | p95 below 50ms; measure separately from daemon event polling/network latency |
| Commit to visible state | Initial reference p95 below 125ms from successful store COMMIT return to DOM update; physical paint excluded. Preserve durability settings. |
| Warm session switch | p95 below 150ms for retained views, without waiting for unrelated host queries |
| Reconnect | Local representative snapshot/replay convergence below 1s after connection availability; no duplicate submissions |
| Retained runtime state | Preserve SDK's 8MiB/view and 512-message/agent defaults; lazy content is bounded separately |
| App-wide memory | Explicit budget for retained views, Query pages, parsed Markdown, highlighted code and content bodies |
| Large presentation | 10,000-message stored history, 100 retained agents, large tool bodies and long session lists; fetch/render only bounded active pages |
| Subscriptions | Maximum 16 watched roots/connection; navigation history and hovered links do not imply live subscriptions |
| Accessibility | Zero critical automated violations; manual keyboard and VoiceOver workflows pass |
| Correctness | No scroll-anchor loss on prepend; no stale root/agent mixing; no silent empty-on-error; no old-history revision mixing |

Inactive view eviction must be explicit and reconstructible; do not silently
evict a user-designated watched root. A lightweight attention index allows
awareness without maintaining a full transcript view for every session.

Bound Markdown, syntax tokenization, images and detail-query caches
in addition to SDK JSON payloads. Move demonstrated expensive work to workers;
virtualizing DOM does not itself bound downloaded/parsed data.

The repeatable local loaded-workload check is now
`npm run pack:web && node apps/web/scripts/performance.mjs`. Its isolated store
contains 10,000 root messages, a 1.4 MB tool body and 100 retained children with
100 messages each. On an Apple M4 Max Mac with 128 GiB RAM, Node 24.14.1 and
Chromium 153, the passing run measured p95 28.50 ms from 40 input events to the
next animation frame, 49.90 ms from 74 received stream events to their DOM update,
and 63.44 ms for 20 cached recipient switches. The typing sample used 32 drafts
totalling 1,046,528 encoded bytes and 16 concurrent fake root-agent streams.
Animation-frame timing is a paint proxy, not physical-device input latency.

Four successive history prepends preserved the same 56-pixel row offset,
including the former small-to-large rendering threshold. Selection stayed mounted
across an 8,000-pixel scroll. Inspecting and closing every child retained at most
346,436 SDK payload bytes and 612 messages (512 root plus 100 selected child),
returning to one root history after each inspection. A post-GC browser sample
used 10,643,252 JavaScript heap bytes with 546 DOM nodes and nine rendered transcript
rows. This is a measured workload, not a worst-case application heap guarantee.
The detailed report is `/tmp/whip-web-browser-results/performance.json`; rerunning
the command replaces it. The physical-device and assistive-technology gates
above remain open, and screenshots alone do not establish image-diff regression
coverage.

Additional boundary measurements: 40 successful store COMMIT returns reached the
actual SDK/app DOM at median 54.09 / p95 85.38 ms with ±0.376 ms clock calibration
uncertainty. The initial local reference budget is p95 below 125 ms, allowing the
existing 50 ms event polling period, 50 ms received-event UI budget and 25 ms local
transport/scheduling overhead. A separate native Chromium EventTiming run reported
all 40 trusted keydowns, p95 32 ms (conservative 28–36 ms quantization bounds),
with no censored or dropped entries. This measures browser next-render completion;
the rAF proxy and physical display timing remain distinct. See `docs/web-app.md`
and [the acceptance ledger](ACCEPTANCE.md) for the report locations and limits.

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
8. **Choosing complexity before evidence.** No CRDT editor, custom terminal,
   second history store, plugin framework or package per UI feature. Our requested
   component library has a clear owner; token/icon subpackages and a separate
   session-components package do not yet need independent lifecycles.
9. **Using two data owners.** TanStack Query serves reads outside SDK views;
   neither the router nor Query duplicates the transcript reducer or command journal.
10. **Treating the design system as a wrapper exercise.** Base UI provides
    behavior. WHIP still owns typography, proportions, states, composition,
    responsiveness and accessibility of complete screens.

## Files and surfaces expected during implementation

- New `packages/ui/`: StyleX tokens/themes, Base UI-backed controls, stories,
  generated TUI theme catalog, library docs and tests; explicit exports and one
  supported compiler setup. Build-only Go exporter and theme drift checks.
- New `packages/app/`: shared React renderer, TanStack routes/queries/forms,
  feature controllers, domain presentation, platform contract and product tests.
- New `apps/web/`: browser bootstrap, Vite/StyleX setup, browser adapters and
  release assets. Root `package.json`, lockfile, lint rules and `Taskfile.yaml`.
- Existing `packages/sdk/src/{services,session,state,react}.ts`: narrowly scoped
  missing helpers/projections only; synchronization remains framework independent.
- Existing `internal/protocol/{registry,runtime_registry,types}.go` and related
  contract files: new typed reads/mutations; regenerate `packages/protocol`.
- Existing `internal/daemon/{query,client_control,network,provider_service}.go`
  plus focused host-directory/attention/content/theme services as needed. Query execution
  stays outside serialized actor transitions; mutations use existing admission.
- Existing `internal/session/{catalog_page,collection_page,event}.go` and mail/
  content/operation readers: bounded projections and revisions, not parallel stores.
- Existing `internal/tools`, workspace coordination, and host services: reuse
  file authorization, process management, content handling and mutation safeguards.
- Existing `internal/tui/theme/{spec,theme,syntax}.go` and catalog assets:
  reuse theme semantics; extract only renderer-independent shared logic into
  `internal/theme` as needed for host custom-theme services, preserving TUI behavior.
- New web-asset embedding/build integration and CLI entrypoint under `cmd/whip`.
- Existing docs above; add `docs/web-app.md` for setup and everyday workflows.

The next implementation slice is Phase 0: prove the UI/app/web package build,
StyleX/Base UI integration and a realistic conversation layout. Then complete
connect → prompt → approve → inspect child → reconnect before expanding the
rest of the runtime controls. This plan update does not itself implement the app.
