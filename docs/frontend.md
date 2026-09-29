# Frontend architecture and design guide

This is the canonical starting point for coding agents working on WHIP's frontend.
It explains the current design, why it exists, and how to extend it. Updated on
2026-09-29 for native protocol-v4 SDK, shared renderer, desktop and mobile ownership.

This is a maintained engineering guide, not a delivery checklist. Historical
plans preserve research and past alternatives; they are not instructions to
reintroduce superseded decisions. In particular, session tabs now ship, and
permission approvals no longer use a signer or enrollment. Current user
instructions take precedence. When changing an architectural decision, update
this guide and the affected reference in the same change.

The [native web workflow inventory](native-web-workflows.md) classifies every
registered v4 operation by its current shared-renderer or SDK/service owner.
Its manifest check uses the native protocol; SDK-only rows expose current UI
boundaries and do not waive retained-feature parity.

The [frontend UX restoration plan](frontend-ux-restoration-plan.md) records the
approved latest-development reference, proposed minimal compatibility work and
comparative acceptance criteria. It is a proposal, not implemented behavior;
this guide continues to describe the current application until each change lands.

## Start here

Before implementation, identify the owner of the state you are changing and read
the nearest working example. These principles apply throughout:

1. **The daemon owns work.** Clients observe and direct it. A browser disconnect,
   component unmount, tab closure, or aborted local wait does not cancel execution.
2. **Each kind of state has one owner.** Do not replicate SDK session state in
   Query, React context, or a second event reducer.
3. **The UI stays thin, but complete.** Put shared protocol/recovery behavior in
   the SDK, generic interaction and visuals in UI, and product behavior in app.
4. **Preserve the person's work and place.** Drafts, recipient selection, reading
   anchors, and unresolved submissions must survive ordinary navigation.
5. **Bound what you retain.** Virtualizing DOM nodes does not bound cached data.
   Every new collection needs a count/byte limit and a visible overflow policy.
6. **Use the design system.** Base UI, semantic HTML, shared StyleX tokens, and the
   full TUI theme catalog are foundations for the product application. The public
   docs site has an explicit app-owned CSS/design boundary described below.
7. **Keep outcomes truthful.** Accepted, running, waiting, failed, interrupted,
   stale, unavailable, and delivery-uncertain describe different conditions.
8. **Prefer deletion, reuse, and direct composition.** Introduce abstractions or
   dependencies only when an actual workflow needs them. This follows the
   repository's [ponytail principles](../.agents/skills/ponytail/SKILL.md).

## Product intent and visual philosophy

WHIP is a workspace for directing recursive coding work: sessions, child agents,
mailboxes, Starlark or JavaScript execution, budgets, goals, schedules, and human decisions.
The person should be able to read calmly, understand what is happening, and
intervene precisely. Conversation is the main working surface; inspection reveals
the evidence and recursive structure behind it.

OpenCode informed the restrained color, component finish, conversation layout,
and interaction quality. We adopted those qualities within WHIP's own architecture.
Its framework choices, runtime migration layers, and IDE feature scope are not
our template. The [original research](../.ai-docs/plans/web-app/README.md) and
[session-tab design study](../.ai-docs/plans/session-tabs/design.html) record the
reference work.

Design from these principles:

- **Reading first.** Quiet chrome, comfortable conversation width, compact tool
  summaries, selectable code, and details on demand. Avoid a card around every
  message, a wall of tool JSON, or a dashboard of metrics above the conversation.
- **Color conveys meaning.** Neutral surfaces establish structure; status,
  attention, selection, and focus justify color. The selected theme supplies the
  palette. Do not assign random colors to agents or hardcode mockup swatches.
- **Subtle depth.** Use canvas/panel differences and fine borders for ordinary
  structure. Reserve shadows for floating overlays. Selected tabs sit on the
  canvas above a recessed navigation strip.
- **Identity stays visible.** Keep runtime, root, and selected child unambiguous
  across navigation, composer, requests, and inspectors. A child's transcript
  inspection does not admit that transcript into an agent's model context.
- **Behavior is part of the design.** Stable scroll, grouped streaming updates,
  focus restoration, retained drafts, and coherent reconnect states matter as
  much as an idle screenshot. Background updates must not steal the reading
  position or keyboard focus.
- **Progressive disclosure preserves control.** Put common actions near their
  subject; move secondary actions into menus and inspectors without making them
  hover-only or hiding pending human requests.

The React application shares its renderer with the macOS Electron host in
`apps/desktop`; Expo mobile uses a separate native renderer and the same SDK.
The feature map, rather than this architecture guide, records delivery scope. Terminal tabs
are in scope as a human-only shell beside the conversation. Data-only agent definitions (persona, rules, discovery, modules,
capabilities, surface flags) are authored in Settings and selected on the
welcome page; agents with custom tools or hooks are authored with the SDK,
because their handlers are code the renderer cannot host. Read-only code/tool
output is in scope. The session REPL viewer and Executions inspector show retained and live
execution evidence; neither executes user-entered code or inspects raw VM globals.

## Packages and dependency direction

All JavaScript packages are private ESM npm workspaces with one root lockfile.
Use Node 24 and the versions in the manifests. The supported renderer, mobile,
agent examples and client examples consume native `@whip/sdk` and generated
`@whip/protocol` v4 directly. Retired SDK/protocol packages have been removed.
Remaining compatibility cleanup and client acceptance are tracked in
[the redesign plan](backend-redesign-plan.md), not inferred from this guide. No source change upgrades an installed application or runtime.

| Package | Owns | May depend on |
| --- | --- | --- |
| `@whip/protocol` | Generated Go-derived operations, DTOs, schemas and standalone validators | Portable contract utilities; no app, transport or execution owner |
| `@whip/sdk` | Transports, verified identities, typed services, explicit durable recovery and bounded views | Protocol; Node-specific code only through explicit Node entry points |
| `@whip/ui` | Accessible controlled primitives, semantic styles, themes and portable theme data | UI libraries; no SDK client or app behavior |
| `@whip/app` | Product workflows, composition, routing, view leases and device presentation state | SDK, UI and app platform interfaces |
| `apps/web` | Vite assets, bootstrap and browser/desktop platform adapters | App and UI; no second product state layer |
| `apps/desktop` | Electron main/preload, native resources, local/SSH transport, packaging | Versioned, bounded bridge contracts; renderer gets no Electron objects |
| `apps/mobile` | Expo native UI, lifecycle and encrypted device storage | SDK, `@whip/app/presentation`, `@whip/ui/theme-data` |
| `apps/docs` | Public static documentation | Docs-local design system; no application SDK/runtime |

The main SDK entry point is transport independent. Browser discovery and content
adapters live in `@whip/sdk/browser`; Unix sockets in `@whip/sdk/node`; framed
native bridges in the framed transport entry point. `@whip/sdk/state` owns
SessionView, TreeCatalogView, ExecutionView and TraceView, and `@whip/sdk/react`
only subscribes to their immutable snapshots. Do not create a compatibility
facade around a retired snapshot or event API.

## Stack decisions and reasons

| Tool | Use it for | Why this boundary exists |
| --- | --- | --- |
| React + TypeScript | Shared application and typed component composition | Web and desktop can share product behavior |
| Vite | Static SPA build, development server, code splitting | The Go gateway already serves assets; no Node backend, SSR, or TanStack Start is needed |
| TanStack Router | File routes, validated search parameters, browser navigation | Shareable location has one authority: the URL |
| TanStack Query | Bounded host/detail reads through SDK methods | Deduplicates reads and manages invalidation without replacing SDK session synchronization |
| TanStack Virtual | Long session lists and variable-height transcript rows | Keeps rendered DOM work bounded; app still owns reading anchors and data budgets |
| TanStack Form | Structured settings forms where field state/validation warrant it | A simple field or the draft composer does not need a second form store |
| TanStack Hotkeys | Scoped, configurable application shortcuts | Browser-owned shortcuts and normal text editing remain native |
| TanStack Markdown | App-owned Markdown presentation and streaming rendering | Parsing does not own message identity, event assembly, or conversation state |
| Base UI | Focus, ARIA, keyboard, portals, overlay behavior | Accessible interaction comes from maintained primitives |
| StyleX | Authored component/app styles, variants, responsive rules, tokens | One extracted styling system across packages and themes |
| Lucide, self-hosted Inter and JetBrains Mono | Icons, chrome/reading, code | Consistent vocabulary and geometry without remote font dependencies |
| ghostty-web | Terminal tab rendering (Ghostty's VT parser in WASM, canvas renderer) | Draws a daemon-owned PTY without injecting styles, so the production CSP only gains `'wasm-unsafe-eval'`; xterm.js would need inline styles |
| Storybook, Vitest/Testing Library, Playwright | Components, app behavior, real browser integration | Each layer is tested at its actual boundary |

Do not add Redux/Zustand, TanStack DB, a second Query client per feature, another
CSS framework, or a frontend provider/agent execution loop without a concrete
architectural need. Existing tools are choices with defined jobs, not an excuse
to route every piece of state through a framework.

## Runtime construction and lifetimes

[Bootstrap](../apps/web/src/bootstrap.tsx) creates one application outside React,
applies saved appearance before first render, and wires the router, runtime,
ThemeProvider, UIProvider and QueryClientProvider. The `whip-app://bundle` origin
requires the versioned desktop bridge; it never falls back to treating that origin
as a host endpoint. Platform disposal follows shared application disposal.

[AppRuntime](../packages/app/src/runtime.ts) owns one window's connection owner,
Query client, paired session/execution leases, trace leases, command observations,
drafts, composition, reading positions and tabs. It does not own accepted work.
Unmounting, hiding, local wait cancellation and detaching stop observations;
execution cancellation always names an explicit native input or turn.

Each visible selected root or child leases its exact session. Root navigation,
tree identity and selected session identity are separate values. Two views of
one session share a transcript and ExecutionView; independent panes retain their
own reading anchors. Trace leases use `(runtimeID, rootID, viewID)`, so separate
panes can select different trace filters and older windows. StrictMode cleanup
must release only the lease it acquired. Start, suspend, reconnect and dispose
remain with the owner, not every renderer using its snapshot.

The startup splash holds native browser surfaces hidden as well as making the
router inert. Initial Local connection settlement releases it; the bounded
ceiling reveals recovery on a slow host. Saved remote hosts do not gate startup.
It never replays on ordinary navigation. Reduced-motion preferences skip motion.

### Host attachment and recovery

`HostConnections` now owns one v4 `Client` and SDK `TreeCatalogView` per attached
host. The client has no connection event reducer. The app owns `closed`,
`connecting`, `connected`, and `stale` attachment state; the SDK alone owns the
bounded catalog and its revision reconciliation. The catalog exists in the
published host record before the window receives its connected callback. Product consumers use these native objects directly.

URL attachment first uses `discoverGateway`, then `browserSocket` pinned to the
observed runtime and process epoch. Native connection resolution supplies an SDK
`FramedConnector`; `framedTransport` verifies the runtime and captured epoch on
every new unary connection. One host wait controller scopes all client calls.
Each SDK browser realm admits at most four connecting WebSockets and 128 queued
opens. FIFO admission removes cancelled or expired waits before constructing a
socket; open/error/cancellation releases its slot exactly once. Established
persistent peers do not retain a handshake slot. This bounds browser connection
throttling without replay, a second data cache, or a new runtime authority.
A caller abort stops only that read or wait; detaching aborts the whole client's
observation lifetime. Native preparation has a separate lifetime so a dropped
call cannot dispose the connection needed for read-only recovery.

After a transport failure, the stale client is hidden from consumers, the catalog
is suspended but retained, and the window receives `detached(..., {recovering:
true})`. One bounded backoff timer per host rediscovers or reinitializes only the
saved identity, reconnects the existing catalog, and publishes a fresh client.
It never repeats a mutation, terminal input, account action, native preparation,
or runtime start. Explicit Disconnect cancels recovery and disposes the prepared
connection and catalog. A changed runtime identity stops automatic recovery and
requires explicit acceptance. Explicit Connect retains the platform's normal
user-requested native preparation contract. Local failure does not detach other
hosts or cancel their accepted work.

Saved URL profiles use `client.hosts.profiles()` and `setProfiles()` through the
single host configuration revision. Profiles retain exact validated root HTTP(S)
URLs and observed runtime pins, with at most 16 records. Input normalization can
accept a WebSocket URL or either known WebSocket path before discovery; it never
stores credentials, queries, fragments, or a proxy path. A display-only edit
preserves the saved URL and live attachment. Uncertain writes and conflicts trigger
read-only reconciliation, never an automatic write retry. Device-owned SSH/local
profiles and legacy URL imports keep their existing explicit verification and
storage boundaries. Host subscriptions and concurrent probes are bounded; SDK
catalog retention remains independently bounded.

## State ownership

| State | One owner |
| --- | --- |
| Sessions, inputs, receipts, history, configuration, execution and accounts | Go host; protocol values are projections of those authorities |
| Bounded history/catalog/execution/trace reconciliation | SDK views |
| Scoped metadata and explicit content reads | Runtime Query client, keyed by runtime/client lifetime, owner and query inputs |
| Connection state and recovery timer | HostConnections |
| Authored text, staged files, target selection and upload progress | AppRuntime drafts and CompositionStore |
| Tabs, panes, selected child, REPL/chat/trace mode | SessionTabs and URL integration |
| Reading position and follow-tail intent | App ReadingPositions and each timeline |
| Pending local delivery and exact recoverable request | SDK DurableCommand/RecoveryJournal, observed by the app |
| Display preferences, theme and native resources | Device/platform owners; never session configuration by implication |

Do not copy SDK snapshots into Query or React state, reduce old stream events,
sum retained rows into whole-tree totals, or add a second transcript cache.
Snapshot absence is unavailable, never evidence of an idle session.

### Current retention budgets

These are current defaults, not permission to grow a collection without review.
[Source](../packages/app/src/runtime.ts) owns exact admission/cleanup behavior.

| Collection | Bound / lifecycle |
| --- | --- |
| App session views | 16 leases; each 256 messages / 4 MiB, with explicit gaps; unused leases expire after 30 seconds |
| SDK SessionView default outside app | 512 messages / 8 MiB |
| ExecutionView beside a session | 16 turns / 128 cells / 512 operations / 4 MiB |
| TraceView | 1,024 rows / 64 trace roots / 1 MiB; at most eight app leases, unused expiry 30 seconds |
| Workspace | 32 tabs / four panes / 64 KiB saved layout; browser tabs additionally capped at eight |
| Unsent text drafts | 32 / 1 MiB encoded total / 256 KiB each; unowned then oldest drafts are evicted at capacity, excluding the draft being edited; storage failures remain visible |
| Browser/desktop recovery journal | 64 records / 8 MiB total / 4 MiB per record; unresolved records are never evicted to send another request |
| Query defaults | 10-second freshness, zero inactive retention, no automatic retry/focus/reconnect refresh |
| New Chat host metadata | Explicit inventory/catalog/defaults/definition/import keys retain five minutes; host detach clears them |

Counts and bytes are independent bounds. DOM virtualization alone does not bound
history, attachments, drafts or model previews. APIs expose continuation,
truncation, missing evidence and explicit older-page navigation.

### Error ownership and canonical displays

[ErrorNotice](../packages/app/src/error-feedback.tsx) supplies the shared visual
contract; existing state owners retain and clear their own errors. Classify by
what failed, not its technical cause. A timeout can belong to any of these owners.

| Type | Owner and canonical location |
| --- | --- |
| Application | Application-wide startup, rendering or persistence failure; app error area or blocking recovery screen |
| Host | One server connection; named host notice in app chrome |
| Session | Session snapshot or transcript synchronization; top of that session pane |
| Turn | Agent turn outcome; conversation outcome notice or matching REPL outcome |
| Execution | One execution cell or host invocation; inside its recorded result |
| Submission | User input admission or uncertain delivery; adjacent to its composer |
| Resource | Read of a list, catalog, stored body or attachment; inside its owning component |
| Action | Explicit mutation, recovery, copy or download; beside its control or inside its dialog |
| Validation | Invalid input; at the field or immediately above form submission controls |

In forms and action dialogs, place action errors after the fields and immediately
above the submission controls, matching form-level validation errors.

Show one primary error, with recovery controls and expandable technical details.
Dependent surfaces show availability status instead of repeating a host failure.
Do not send local failures to a global banner or a second toast. `runtime.run`
records command outcomes but its caller owns presentation. Durable command
acceptance does not prove that a child submission was enqueued or a turn started.
Keep failure, cancellation, interruption, reconnecting and uncertain acceptance
distinct; never retry uncertain input automatically.

Clear current errors after successful recovery and retire late results when their
owner changes. Recorded cell/operation failures stay with their canonical records.
Turn notices use exact native session/turn evidence; do not infer historical
placement or the failed turn's model from current configuration.

## Data fetching and synchronization

Use the SDK view for a synchronized transcript/catalog/execution/trace. Use a
scoped, abortable Query for a bounded detail read. No feature starts another
socket, hidden transcript crawl, view observer or client-side execution ledger.
Query keys include the actual owner and client/epoch where evidence can expire.
Warm metadata uses Query's own deduplication and error state; fire-and-forget
prefetch must not leak cancellation as an unhandled rejection.

SessionView starts at the actual tail, retains exact decimal sequences, and
reconciles authoritative history independently from provisional preview text.
`loadOlder()` stops following the tail until `latest()`; new work is signalled as
missing latest history. Changed history revision or process epoch clears
incompatible pages/previews. Canonical message parts are already in the snapshot:
there is no legacy JSON-body fetch or stored-prose cache.

Content identity is `(owner_session_id, reference_id)`, not a digest or filename.
Metadata does not prove bytes are available. Explicit SDK `readBytes` verifies
owner, length, canonical encoding and digest within the 4 MiB transfer ceiling.
App content keys include runtime, exact owner, reference and digest; inactive
byte queries have zero retention, image object URLs are revoked on unmount, and
late owner-switched results are discarded. Attachment metadata reads inspect at
most 128 references in batches of four. Text loads on disclosure with a 1 MiB
preview ceiling; mounted image previews use the 4 MiB ceiling. Explicit Download
reads and verifies at most 4 MiB before using the platform save flow. A changed
owner, reference, client, connection, closed dialog or unmount cancels pending
attachment downloads. An oversized transcript gap is inspected
separately; ordinary paging does not enlarge the transcript budget.

Activity subjects use a small allowlisted projection of recorded operation
arguments: file path/search pattern, shell command, browser URL/query and agent
name. Excerpts retain the reference's byte bounds and Unicode boundaries;
arbitrary arguments and results stay under Operation details. Permission scope
is separate from the subject. Native denied/uncertain outcomes never inherit a
successful parent turn's status.

## Mutations, acceptance, and permissions

Prepare a typed durable command with its immutable ID and exact payload, persist
it before transmission, then observe its receipt. Transport loss is uncertain
delivery, not rejection. A local abort stops the waiter and keeps unresolved
tracking; it never submits a replacement request or cancels work implicitly.
Explicit check/retry binds the original record to the same runtime and client.
Different payload under the same identity conflicts. A changed host is not a
recovery destination. Acceptance, application and terminal success are separate.

Browser/desktop Saved commands read the existing RecoveryJournal. Opening that
page only reads local storage. Checks distinguish exact acceptance, identity-only,
missing and unavailable evidence. Retrying and forgetting are explicit; forgetting
only removes local tracking. Workspace `claimed` and `uncertain` outcomes retain
tracking and cannot complete a dependent UI action. Ephemeral terminal input,
account secrets and resource handles never enter the durable command journal.

Native permission/question decisions preserve the selected operation/question,
exact decision and original identity through an uncertain acknowledgement.
Ordinary pending replies keep their original controls disabled in place; Check
and Retry appear only after a failed/uncertain reply. The request dock reads
human approvals only for the root. Child operations retain native delegation
and denial behavior; they cannot be turned into directly approvable requests.
The root card shows the requested command/path and a readable requester; exact
resource/arguments remain available in details. Focus returns to the composer
in the originating workspace pane. Broader historical Remember scopes are not
silently mapped to exact native grants.
The composer permission picker captures root tree-policy revision when opened;
background reads never rebase an open choice. Child configuration cannot widen
authority, and child model controls remain read-only in the current UI. Ask/Full
Access, standing grants, intrinsic questions and tool-denial policy are distinct.

### Provider and model settings

Provider screens read explicit host routes and a bounded set of candidate sources
through `Client.listProviders()` and `providerCandidates()`. Candidate evidence is
read-only: it contains source labels and availability, never secret values, and
never runs credential commands or publishes routes. Opening Settings or New Chat
does not refresh a catalog, start sign-in, or change a default. Using a detected
source is an explicit CAS operation. Cached and bundled model reads do not
establish inference access. Unknown token prices remain null and display as
unknown; exact decimal prices are formatted without a JavaScript number. A
current, successful empty catalog is not filled from the offline bundle.

Model catalogs retain at most 4,096 choices and report truncation. Session model
edits use the selected root's exact configuration revision; child model controls
remain read-only. Missing activity disables edits rather than implying idle.
Model changes clear old effort, and **Default** is distinct from provider-defined
**Off**. Providers settings saves model, effort and permission in one host CAS.
Execution settings saves engine, compaction, limits and MCP source preferences in
one host CAS. Conversation Model leaves compaction selection unset. Zero attempts
means the host default; the raw goal preference distinguishes null/default from
an explicit zero-continuation policy. These defaults affect future work only.
Conflict or uncertain publication refreshes evidence without replaying the edit
or adopting a fresh revision; the form retains the complete draft.

Account flows retain their native Inference.net/OpenAI shapes behind the focused
reference login and key forms. The UI polls only actively progressing flows,
uses list/get to inspect lost acknowledgements, and exposes known persistence,
setup and cleanup recovery explicitly. Uncertain project or key creation is
never retried automatically. Pasted keys remain in the form only; an explicit
retry of an unconfirmed publication retains the same key identity. Secrets never
enter query data, saved commands, or the recovery journal.

Disable/Enable persists route state without changing its credentials or defaults.
Disconnect uses the host's source-aware cleanup: owned key files may be removed,
external credentials are preserved with an explanation, and shared credentials
are kept while the selected route is disabled. Local and remote cleanup failures
remain explicit and retryable. Advanced **Remove configured route** is a separate
CAS operation that preserves credentials and rejects dangling defaults.
Connecting an account leaves model defaults unchanged, and choosing a default
never submits an existing chat draft.

### Dedicated Settings workspace

Settings owns form drafts and validation; native host configuration remains the
single authority. Each save quotes the revision loaded into its form. Conflict
or uncertain publication performs read-only reconciliation and preserves the
draft, never an automatic rebase/retry. Explicit discard adopts current settings.
Execution, permission, model and compaction defaults affect future captures, not
existing sessions by accident.

Definitions use immutable `{id, revision}` references, explicit 100-item pages,
at most 1,000 retained revisions and visible truncation. Registering creates a
new immutable revision. Preserve omitted fields when editing; module visibility
is not an authority grant. Code handlers belong in authored SDK agents, not a
renderer-created executor.

MCP import captures host revision, fingerprint and optional root when opened.
It lists unsupported/disabled/excluded sources separately. Publication does not
start a connection or grant calls. A separate explicit refresh targets that
captured root; a refresh failure does not undo or replay publication. Tool schemas
and host diagnostics reuse native reads; inspection neither binds an executor nor
starts/restarts a host.

Session reload captures host defaults plus expected session revision at admission
and applies at an idle tree boundary, preserving explicit overrides. Pending,
conflicted, interrupted and unavailable receipts remain in the existing journal.
Browser driver edits use host CAS; a live environment pin is visible and disables
editing. Computer-helper bundle publication, enabled configuration and live
connection are separate explicit actions.

External Chrome settings use the existing host-scoped Query owner and explicit
revision CAS. The shared form retains its reviewed revision and draft; stale or
uncertain writes require an explicit read/discard before another write. It never
replays a mutation after reconnect. The root-only browser inspector shows bounded
name/generation/resource metadata and captures the exact generation for reconnect
or disconnect. Children can inspect but cannot mutate root connection lifetimes.
These controls do not launch Chrome, open an extension relay or grant operations;
Desktop offered tabs keep their separate native owner. See
[the form](../packages/app/src/settings/external-browser.tsx) and
[the inspector](../packages/app/src/details/external-browser.tsx).

## Conversation and navigation patterns

Conversation consumes canonical message parts and whole provisional preview values.
The backend additionally retains optional versioned ordered presentation on
canonical messages and failed-attempt evidence on bounded history/observation
pages. It is display-only: exact UTF-8 text ranges, streamed reasoning and tool
slots scoped by the owning session and source attempt. Imported source identities
never grant local execution authority. Old messages may lack this metadata;
canonical prose/calls remain authoritative, including when presentation truncates.
SDK/app consumption is tracked in the [restoration record](frontend-ux-restoration-progress.md).
SessionView retains failed-attempt presentation only for its selected bounded
history groups. ExecutionView shares that source and owns bounded exact call/result
bodies for retained cells. `session.history.message` validates owner, identity,
retirement, byte cursors and immutable metadata through every chunk. Turn cells
page by their real store ordinal, not opaque ID ordering. Older navigation retains
its boundary while new work continues; returning to latest is explicit.
Calls, results, cells and operations join by exact session/turn/message/call IDs;
imported groups with no local turn do not invent local execution. Local authored
previews retire only on exact admitted input identity. Mail and Design context
use recorded provenance, never tagged text or filename guesses.

Drafts, staged attachments, focus, pane selection and reading anchors belong to
the app and survive observer refreshes/mode changes. Virtual rows preserve exact
decimal bookmarks and selection. New data does not steal scroll position from
someone reading earlier history. Waiting/failed/cancelled/interrupted states remain
truthful, and running elapsed labels are display estimates that pause when hidden.

Control pages are bounded independently of the transcript: 100 queued-input
metadata rows, 16 children, 16 upcoming schedules, one pending permission with its
operation and four root-owned questions. Queue pages replace each other; older
pages and returning to the first are explicit. Authoritative activity counts never
come from the number of retained messages. Promotion retains exact input/active-turn
identity; cancellation names its actual input. A conflict never selects a replacement
turn. Full input payloads load only when explicitly opened.

Fork/rewind confirmation captures selected owner, immutable request ID, history
revision, observed tail and whole exchange boundary before dispatch. A bounded
backward lookup can locate an opening outside the current window. Completed
response footers resolve the actual whole-group end with a bounded forward
lookup; neither action performs sequence arithmetic. Timestamp and bounded
canonical-prose copy remain available offline. A partial/missing response tail
does not expose history actions. Host conflicts do not silently capture a different edit.
Workspace restoration is a separate durable effect with explicit uncertain states.

New Chat's tab UUID is its tree-creation identity. Verify accepted native root,
tree, definition and engine before moving the draft/staged files to it. Recovery
of creation alone never submits a message, navigates, or reopens a closed tab.
Saved unresolved creation blocks a new payload; explicit Saved commands recovery
preserves newer authored text and rejects destination conflicts.

### Saved-session navigation

Projects groups exact directory strings across hosts and reads actual root recency
through `trees.recent`. One client-keyed Query window retains at most 500 rows,
five 100-row reads and 2 MiB. Older/expanded windows retain their range; Show latest
is explicit. Recency cursors are advisory because new activity or pins can move
rows; deduplication does not claim a frozen snapshot. TreeCatalogView still owns
catalog revision observation and Search. Only visible roots read activity. A title
hint is not a second mutable title store.
Search retains one 64-row tree page per host with the matching revision and
cursor, invalidates only the changed host, and discards transient pages on close.
Search dates describe tree creation; Projects uses the host's actual last activity.

Remote New folder creates one child under the selected parent on that host,
without shell commands or replay. Success enters the folder; Choose folder is
still explicit. Mac native dialogs expose their create-directory control.

Open roots use bounded activity summaries without hydrating transcripts. Attention
and notifications preserve exact child recipients. The notification index retains
at most 256 sessions through four 64-row / 256 KiB pages and resets on uncertainty
or Client replacement. Equal request counts do not establish a new question.
Hidden-browser and opt-in desktop polling have distinct policies.

### Split workspace

SessionTabs owns descriptors, pane layout and explicit moves; routes identify the
selected view without creating a second pane tree. Closing a tab releases
observation, not execution. Split views share native session data while retaining
independent mode, selected child, scroll and companion tabs. Tab/window storage
holds bounded UI metadata, not runtime handles. Terminal and browser descriptors
must be revalidated against their native process epochs before any effect.

### Session REPL viewer

REPL receives the selected session plus the shared SessionView and ExecutionView.
It renders retained cells independently from whether their exact transcript
messages fit the window. Live stdout comes from bounded native `cells.output`
observation; committed results remain the canonical final authority. Provisional
execute arguments never become a fake cell. The recorded result supplies output,
explicit JSON null values, engine metrics, restart and checkpoint warnings.
Formatting/collapsing never changes copied text; unsafe numeric JSON remains
verbatim. Explicit text content reads cap at 1 MiB and downloads at 4 MiB.

### Session trace viewer

TraceView owns canonical descending rows and a separate trace-root picker at the
same revision. Paging replaces bounded windows; refresh preserves an explicit
older boundary. Empty filtered pages can still continue. Tombstones remain null
span rows. Trace-window counts are not whole-tree usage.

The renderer retains virtualized tree/waterfall, zoom/pan, pane sizes, raw evidence
and explicit OTLP export. `observed_at_ns` plus monotonic local elapsed time
advances display only; it never commits a span end. Subtract exact BigInt counters
before converting relative geometry. Completed zero-duration spans remain complete;
reversed/future clocks stay labelled. Hidden/detached/reduced-motion views pause
animation. Raw detail preserves exact IDs, timestamps and attributes.

### Prepared model instruction inspection

Trace details perform an explicit `session.models.inspection(attemptID)` read, scoped to
that span's canonical session and attempt. They show exact base instructions and appended
notices captured at preparation, plus the prepared-request digest and bounded message/tool
provenance. These descriptors do not reconstruct historical transcript bodies or provider wire
requests. Private provider continuation, cache identity, credentials, and hydrated attachment
bytes never enter the public capture. Existing attempts with no capture remain unavailable.

Captured text retains at most 16 MiB per request in content chunks of at most 4 MiB. Publication
and registration use the existing owner content quotas; unavailable oversized, quota, or storage
captures are labeled explicitly and never replaced with current instructions. Reading verifies
owner, individual chunk bytes/digests, and the complete text digest. The UI retains a preview of
at most 1 MiB and renders a bounded excerpt; explicit downloads preserve the complete captured
text. Selecting a different attempt, changing transport, disconnecting, or unmounting cancels
pending reads without cancelling model work. Trace observers never hydrate these bodies.

Compaction inspection follows the exact attempt's immutable summary metadata and reads its
canonical text, even after context selection changes. It displays the original raw cutoff as an
exact decimal counter. Trace export includes available captured instructions under the explicit
`captured_instructions_only` scope, with the existing 1 MiB body and 4 MiB export limits, and
includes committed compaction output as canonical text parts. It does not manufacture missing
prompts, notices, summaries, or private request state.

### Mermaid diagrams

Web and desktop Markdown fences marked `mermaid` use the shared
`MermaidBlock` (`packages/ui/src/mermaid-block.tsx`), backed by the exact reviewed
`beautiful-mermaid` release. This is a **conservative Mermaid subset**, not full
mermaid.js compatibility. Unknown types, unsupported statements, incomplete or
truncated source, and render/size failures remain source with an explanation;
never silently display a partial parse as a complete diagram. The supported
statement policy and limits live in `mermaid-data.ts` with real-library fixtures.
Basic flowchart, state, sequence, class, ER, and single-series XY diagrams are
supported. Styling/configuration directives, HTML, external resources, actions,
and unsupported advanced syntax are intentionally source-only.

`packages/app/src/markdown-code-block.tsx` is the Markdown-only dispatch shared by
`timeline.tsx`'s `Prose` path and `streaming-markdown.tsx`'s virtualized walker.
Generic tool/REPL `CodeBlock` instances never opt into rendering. Message readiness
is explicit: live responses remain source until settlement, even if the parser
synthetically closes a fence. Truncated retained text never renders a diagram.
Diagram/Source preferences are presentation state owned by each mounted Timeline,
bounded to 128 choices / 512 KiB of keys and discarded with the conversation view.
Static Markdown annotates TanStack's rendered tree with owner+structural-path
identity, so identical nested fences retain independent preferences on remount. Original Markdown remains
the response-copy/history authority. No SDK, protocol, or host state is added.

The shared component uses resolved theme colors, accessible text/connector roles,
and the UI font/size preference. Chrome uses StyleX tokens and existing controls.
Source uses `CodeBlock` with its header hidden only because the enclosing diagram
header supplies the same original-source copy action. The expanded Dialog offers
Fit/100% and scrolling; focus returns to Expand. Source focus/selection prevents
an automatic image swap. Theme replacement keeps the old image until its
replacement decodes; row-size changes use existing reading-anchor measurement.

The library and bundled fonts live in one lazy module worker, never React render.
The scheduler bounds source (32 KiB), statements (250), queued work (16 / 512 KiB),
SVG output (1 MiB including fonts), dimensions/pixels, and cache (32 / 8 MiB).
Active layout has a two-second termination deadline, separate from a 15-second
cold-worker startup deadline. Stale/abandoned requests are cancelled and URLs are
revoked after replacement/unmount; cache entries are strings, not live URLs.

Generated SVG is a **Blob-backed image only**, never injected markup, an object,
iframe, or navigable document. The output adapter strips library remote font
imports, embeds existing local Inter subsets when selected, and rejects active
markup/external references. Source cannot supply theme CSS or URLs. SVG image
isolation means page CSS/fonts are not implicitly inherited; the image is
self-contained. A release-specific worker bootstrap selects ELK's FakeWorker
export and warms it once before restoring worker globals; it never alters the
window. This compatibility shim is covered by real production and development
worker tests and must be revisited on dependency upgrades. Vite consumers include
`beautiful-mermaid` in `optimizeDeps.include`: the engine remains lazy, but its
transitive CommonJS dependency must be transformed even inside an installed UI
package's worker. See `packages/ui/README.md` and the packed-consumer probe. The public
`mermaid-NOTICE.txt` includes licenses and ELK source availability; desktop's
existing dependency-notice/SBOM pipeline also includes the transitive packages.
Production CSP is unchanged. Raw SVG export and interactive links
are deliberately absent because they need a different security design.

Coverage includes `packages/app/test/mermaid-markdown.test.tsx`, shared renderer
tests and `npm run test:mermaid -w @whip/ui`. Native packed-app probe migration and
exact browser evidence are tracked in the development log. Use actual production CSP, real
library output, offline/local assets, theme changes, and reading-position checks;
mocked component tests alone do not validate this rendering boundary.

Every standalone shared `CodeBlock` owns one always-visible copy button in its top-right
header, including chat, REPL source/output/return values, traces and detail views.
`UIProvider copy` receives `text => platform.copy(text)` at application bootstrap;
UI imports no app runtime or Electron APIs. `CopyButton` resolves an explicit
callback before the provider and uses browser clipboard only as the standalone
fallback. Nested UI providers retain the inherited clipboard adapter.

Copy uses `copyText ?? code`, before visual byte limits or text decoration. REPL
output supplies raw `row.output` while displaying its collapsed/formatted preview;
there are no separate REPL copy controls. Copy does not fetch offloaded bodies or
claim that unavailable text is present. Additional authorized actions use
`headerActions`. Clipboard failures have one accessible local display at the
copy control, cleared on successful retry or source change; stale asynchronous
results cannot mark newer streaming text as copied. See the shared code-block,
clipboard-provider and REPL tests, plus the UI CSP browser probe.

### Slash skill suggestions

Native `skills.list` uses the exact session. New-draft completion uses explicit
host global/project scope and immutable definition revision. Warm metadata retains
at most 1,024 records in eleven requests / 1 MiB; disabled skills are omitted.
Truncation falls back to one debounced 32-item prefix request. Preserve IME,
selection and focus, cancel obsolete scopes, and do not load skill bodies or
admit work while completing a name. File mentions use native workspace discovery,
not the skill catalog.

## Native Browser workspace boundary

Desktop Browser pages are human resources owned by Electron BrowserManager;
BrowserWorkspace owns their app descriptors, routing and observations. Shared
BrowserPlatform mutations quote native epoch/tab/generation. Restoring metadata
never navigates or admits a page. Missing native support preserves unavailable
descriptors. Browser tabs are enabled unless explicitly disabled at launch;
availability grants neither model control nor preview-network access.

Native WebContentsViews require actual hide acknowledgement before interactive
overlays mount: CSS stacking cannot hide a native page. Geometry snapshots are
monotonic and use CSS viewport coordinates. Full renderer navigation invalidates
the outgoing epoch without publishing a replacement to that document.

[BrowserAssociations](../packages/app/src/browser-provider.ts) separately offers
an exact verified host/root/window/pane and optional human pages through native
SDK provider selection. Inert availability shares no pages and grants no control.
Only unambiguous destinations may be selected automatically; focus and connection
order never choose one. Host permission/grant checks and selected executor
bindings own model effects. Disconnect releases authority and does not replay an
uncertain effect. Titles/URLs are untrusted data and do not enter prompts as an
implicit page inventory. SSH previews require exact verified host/project/connection
evidence and explicit preview authority.

Browser transport/executor contracts live in the [SDK guide](../packages/sdk/README.md)
and [backend domain](backend-domain.md). Human-tab control, model delegation,
Design selection and human terminals remain independent resource boundaries.

### Browser Design Mode

Design Mode uses a separate trusted native sibling `WebContentsView` above the
selected guest. The isolated `apps/web/design.html` entry imports only
`@whip/app/browser-design`, UI/theme code, and its dedicated design-only preload;
it never initializes an app runtime or receives the normal desktop bridge.
Native [`BrowserDesignController`](../apps/desktop/src/browser-design.ts) owns
one exclusive human debugger lease, isolated-world DOM observations, selection
identities, overlay geometry, and capture. The overlay intercepts selection
clicks/keys; only bounded wheel input is forwarded to the guest. It yields to
existing native hide holds **before** acknowledgement; this does not weaken the
ordinary portal/modal boundary. Guest pages retain no app preload or authority.
Exact Cmd+Shift+D routes from guest/overlay native key handling and the browser
section's DOM handler through existing Browser shortcut events to the same app
DesignControl toggle. BrowserWorkspace rechecks the selected pane, current
epoch/generation, and native-surface holds; no global OS shortcut or guest bridge
is introduced. Repeats, composing input, extra modifiers, and stale/hidden
native surfaces do not toggle.

The app-side [controller](../packages/app/src/browser-design-controller.ts) owns
prompt/destination/capture state; [integration](../packages/app/src/browser-design.tsx)
projects connected, open conversation targets and defaults only an unambiguous
explicit page association. The isolated overlay receives bounded serializable
models and emits validated intents quoting a lease and document/selection revision.
It has no SDK, host connection, upload, or arbitrary CDP access. Theme backgrounds
must remain transparent on this native surface.

The hover outline alone transitions `left`, `top`, `width`, and `height` together
using StyleX `appearance.motionFast` (100ms), ease-out. One stable DOM outline
retargets from its current presentation without a JavaScript animation queue or
scale transform, preserving constant border thickness. First appearance snaps;
selected outlines, tooltip, composer, and click hit-testing never interpolate.
Native hover IDs identify consecutive observations of the same live backend node,
independently of selection IDs. Native `hoverGeometryRevision` invalidates motion
on scroll/resize/zoom/security hiding even if React batches away a cleared model;
missing revisions fail closed to snapping. Same-node geometry changes also snap.
The renderer compares lease, document/selection revision and viewport before
animating and removes invalid/nonactive hover immediately. Both app Reduce Motion
and OS `prefers-reduced-motion` disable interpolation, including an in-flight move.

Captured page text is an **untrusted text evidence attachment**, never concatenated
into authored instructions or passed through mention/skill expansion. A viewport
PNG is included by default and can be excluded. Existing
[`CompositionStore`](../packages/app/src/compositions.ts) surface-scoped keys retain
normal upload quotas and cancellation; [`submitChatInput`](../packages/app/src/chat-submission.ts)
is shared with normal chat. Design drafts do not overwrite the destination composer.
Confirmed admission clears the matching draft, evidence preview, and native selection,
closing the composer while leaving Design Mode active for another selection. The
reset is document/selection-revision scoped so late admission cannot erase newer picks.

Design submissions carry a bounded `design_context` presentation descriptor with exact
uploaded attachment identities. The daemon persists it in inbox previews and authored
message presentation, adding resolved content-part indices for history. The transcript
uses those indices—not filenames or text matching—to group the screenshot and selected
elements separately from authored prose. Full model evidence is unchanged. The shared
[attachment UI](../packages/app/src/browser-design-attachment.tsx) offers captured page
details and raw context on demand; pending/queued attachments load through the existing
scoped content reader. Older messages without this provenance keep their ordinary,
inspectable presentation. Page labels remain untrusted plain text and never retarget a
live page when opened from history.

Admission uncertainty uses existing command recovery, not a new command ID/retry.
Live nodes are transient; accepted text/image attachments use normal transcript
persistence. Navigation or stale nodes require reselection, never selector-based
silent retargeting. Inspecting/sending context grants no agent browser control.

## Native mobile companion

[Expo mobile](../apps/mobile) is a separate iOS/Android renderer using native
contextual sheets and semantic Whip colors. React Native StyleSheet owns layout,
FlashList bounds rows, Enriched Markdown renders selectable text, and safe-area/
keyboard providers own insets. Only the portable `@whip/app/presentation` and
`@whip/ui/theme-data` entry points cross from shared packages; Metro must not load
web controls, routing, StyleX or browser providers.

MobileWorkspace owns four host runtimes and shared SQLCipher storage. Each host
owns one native Client, Query client, command/decision owners and selected-session
leases. Profiles pin persistent runtime and human client identities. Backgrounding
suspends views/cancels reads; foreground reconciles identity before enabling
actions. Temporary inactive transitions do not detach. One host's removal does
not close another or shared storage. The host keeps executing accepted work.

The connection sheet owns action errors and an explicit temporary probe. It
performs bounded HTTPS discovery, native identity verification and a one-item
catalog read without replacing/saving the runtime client. Leaving/backgrounding
closes it; diagnostics never export secrets or raw host errors. Workspace indexes
share a two-read lane and one foreground poll; each host retains one bounded page,
not a growing page cache. Lists never subscribe to transcripts.

Encrypted storage keeps four hosts, 16 drafts (64 KiB each / 512 KiB total),
64 bookmarks (64 KiB total), and 64 recovery/decision records sharing 64 KiB.
The device-only SecureStore key and backup-excluded database have visible failure
states; never fall back to plaintext or an unrecorded send. Appearance has its
own bounded 256 KiB bucket. Transcripts/query snapshots are not persisted.

Mobile deliberately persists recovery **metadata**, fingerprints and acceptance
markers rather than prompt-bearing request payloads. Exact requests remain only
in bounded process memory (64 / 8 MiB). An explicit same-lifetime recovery can
verify/retry those bytes. After app restart, identity-only evidence cannot prove
the original payload and cannot reconstruct/retry it from a current draft.
This differs from browser/desktop's exact-request journal and must remain visible
in the UI. Decisions use native permission/question APIs and the same explicit
uncertainty distinctions; reconnect never answers or resubmits automatically.

Full-message sheets explicitly fetch at most 256 KiB through scoped/hash-checked
SDK reads. Recycled rows never initiate reads. Large native text pages retain at
most 8,192 UTF-16 units without splitting surrogate pairs; tool previews are
512 units. Copy preserves complete loaded text. Image syntax/raw HTML stay as
source so Markdown cannot fetch embedded resources; links open only on a user
tap through the scheme allowlist.

Use [mobile setup](mobile.md) and exact evidence in the development log. Passing
Expo exports and backend lifecycle fixtures does not establish physical-device,
VoiceOver, signed release or App Store coverage. Unsupported product capabilities
remain in the feature ledger rather than being implied by this shared architecture.

### Desktop native services

[AppPlatform](../packages/app/src/platform.ts) exposes optional typed capabilities,
not filesystem handles or Electron objects. Main owns local runtime discovery,
verified executable installation/update and explicit restart. Renderer diagnostics
are bounded read-only snapshots; opening Settings never installs, starts or
restarts a runtime. Setup and connection preparation have one owner and preserve
explicit interruption approval. Disconnecting/quitting the GUI stops observation,
not accepted daemon work. Never use an installed runtime as an acceptance fixture.

Native local/SSH transports are bounded framed connections with runtime/epoch
verification. Desktop admits 32 ordinary connections and 32 persistent browser
provider peers, with at most 64 pending opens and a 15-second admission deadline.
The connector declares its purpose before opening; this controls resource
accounting and grants no browser authority. Waiting is FIFO within each pool,
and a full pool does not block the other. Abort, owner release and disposal remove
pending opens before admitting replacements; no frame or command is replayed.
SSH key/authentication prompts quote exact attempts and serials;
answers are ephemeral and never enter drafts/storage/logs. A focused connection
dialog owns progress/errors while background prompts queue. Selected binaries,
profiles and native resources retain their platform-owned records; components do
not derive socket paths or silently adopt a different executable.

Terminal descriptors quote process epoch. Missing/replaced epochs cannot read,
write, resize or close a shell; restart is a new explicit shell action. Reads use
32 KiB pages and exact decimal byte cursors, resetting only for explicit truncation.
The renderer replays buffered output immediately, then keeps one bounded
`readTerminal(..., { waitMs: 5000, signal })` in flight; output/exit wakes it
without a fixed typing delay. Unmount aborts observation without closing the
shell. Human terminals inherit the host's full shell environment and WHIP markers;
agent execution retains its separate filtered environment. A host launched with
a disposable HOME/ZDOTDIR uses that disposable configuration.
Input has a 256 KiB pending bound and one write in flight. Error/overflow/disposal
discards uncertain bytes; reconnect never replays keystrokes. Native browser,
terminal, editor and shell effects remain behind their separate bridge owners.

Source and packaging contracts are in [desktop releases](desktop-releases.md),
[LocalRuntime](../apps/desktop/src/runtime.ts),
[desktop bridge](../packages/app/src/desktop-bridge.ts) and the staged native
acceptance scripts. An ad-hoc packaged fixture is not a signed installed release.

## Component and styling contract

Start with the [UI inventory](../packages/ui/README.md#component-inventory) and
its stories. Compose existing controls before adding a primitive. A reusable UI
component has controlled props, ordinary callbacks, native refs/ARIA/events, and
an optional typed `xstyle` extension where appropriate. Base UI `render` and
`mergeProps` preserve composition. Keep interactive siblings separate; a tab link
cannot contain its own close/menu button. App-specific data fetching belongs in
app controllers/hooks, not in UI controls.

Use `stylex.create` and `stylex.props` for authored visuals and import shared
variables from `@whip/ui/tokens.stylex`. The `.stylex` suffix is required for
cross-package static resolution. Do not add Tailwind, CSS-in-JS runtime injection,
an unlayered global reset, or ad hoc per-component palettes. `reset.css` is browser
normalization inside `@layer whip-reset`, not a second component stylesheet.
Configure StyleX with `useCSSLayers: {before: ['whip-reset']}` so that reset
defaults remain below component styles in both Vite development and production.
Import order alone does not establish that precedence in development. Text inputs
use a neutral focus border without an outline; other controls and the reset
fallback use a thin neutral keyboard-focus outline.
Declare border width, style, and color explicitly; shorthand borders were lost
by the current compilation path.

| Design role | Current source of truth |
| --- | --- |
| Palette | `colors`: background, foreground, panel, element, hover, borders, semantic statuses |
| Accessible secondary text | `surface.secondaryText`; terminal faint/muted colors are not automatically suitable for small browser text |
| Quiet separation / recessed navigation | `surface.quietBorder` / `surface.navigation` |
| Reading and code typography | `typography.sans` / `typography.mono`, scalable `size10`…`size28`, and `codeSize` |
| Rhythm and geometry | `scale`: 4/8/12/16/20/24/32px spacing; 4/6/10/12px radii |
| Responsive and motion rules | `scale.phone`, `scale.touch`, `scale.reducedMotion`, `appearance.motionFast`/`motionNormal`, and `prefersReducedMotion()` for imperative animation |
| Syntax and Markdown | `syntax`, `markdown`, resolved theme code tokens |

Typical chrome is 13px, conversation and desktop composer text 14px, and small supporting text 12px;
use existing roles before inventing another size. Desktop controls are compact;
coarse-pointer/mobile targets are at least 44px and mobile text inputs use 16px.
Every control needs default, hover, active, focus-visible, disabled, and pending
states. Color alone must not communicate status. Preserve browser zoom, semantic
labels, keyboard paths, reduced motion, selection, and focus return after overlays.

Runtime inline styles are reserved for measured geometry (virtual rows, overlay
positioning), not authored colors/spacing. Validated theme data updates a fixed
CSS-variable allowlist. UIProvider disables Base UI's injected style elements;
the tab drag implementation uses native Web Animations instead of injected drag
feedback CSS. A shared, inert tab preview follows the pointer outside the
scroller; temporary sibling transforms reveal the insertion gap without changing
the saved order or remounting content. Headers use this gap without a separate
insertion marker; content-area move and split targets retain their drop previews.
Tabs use a shared contour with 12px upper
corners and outward-curving lower shoulders. The active tab's quiet border follows
the top and sides, leaving its bottom open into the content surface and covering
the strip baseline. The drag preview keeps that contour without a drop shadow.
Preserve the production CSP and existing geometry exceptions. The only script-source
relaxation is `'wasm-unsafe-eval'` for ghostty-web's terminal parser; no style-source
relaxation is acceptable, which is why xterm.js was not adopted.

### Empty workspace

The empty workspace (`empty-workspace.tsx`) is the front door: first run, storage
loss, and URLs whose tab is not open here. It reuses the New Chat heading, lists
the shell's actions as rows with the live keycaps from Settings (`formatForDisplay`
from the hotkeys package). One vocabulary across the sidebar, palette and this page:
New session · Search sessions · New terminal · Commands · Reopen closed tab.
Empty states everywhere ask "What do you want to work on?"; failure copy is
reserved for stale URLs and real errors.

A fresh bare `/` with empty device **and** window stores stays here without opening
or seeding a tab. An unconfigured native Mac shows local setup here instead of an
error or the action list; completing explicit setup opens New Chat. New session is a separate user action that creates its real
`/new/$draftId` route. When no provider is ready, provider setup replaces the
composer and project controls; completing setup reveals the preserved draft.
Explicit provider setup also temporarily replaces the composer.
Desktop startup acceptance preserves this UX: the zero-interaction measurement
requires the scoped frontdoor action, current verified local SDK connectivity,
and the actual `StartupScreen` visible phase, not just layout rectangles or a
healthy daemon. Only explicit native startup fixtures opt into a read-only
bootstrap projection of bounded connection/tab state; it exposes no runtime,
client, identities, paths, errors or native capabilities and is removed on dispose.
A separate functional onboarding launch clicks the real action and verifies the
generated draft route; its timings never enter startup percentiles. See
[Desktop release acceptance](desktop-releases.md#signed-startup-contract).

### Loading states and placeholders

Keep the interface still while a host answers. Four rules, in priority order
(background and measurements: `.ai-docs/plans/loading-states/`):

1. Never show a wrong state as a placeholder. "Unavailable", "Connect a
   provider", "not available" describe outcomes; while a read is still pending
   the copy is neutral ("Loading session…") or absent. Derive failure copy from
   an error or a terminal status, never from "no data yet".
2. Reserve the footprint instead of swapping components. Header, composer and
   toolbar exist from the first paint at their final height and their contents
   fill in; a disabled real control or an empty fixed-height slot usually
   suffices. `SessionContent` and `WelcomeComposer` do this with an `opening` /
   `isPending` flag rather than a separate loading view.
3. Use a skeleton only where a blank slot would read as incomplete: the
   composer's mode/model/effort pills (`PickerSkeletons`) and short lists that
   fill a dialog (SSH profiles, folder rows). No skeleton for one-line status
   text, sidebar sessions, or settings sections.
4. Put status text where the content will appear, inside the slot it describes,
   so removing it does not shift what follows.

Spinners may be delayed to avoid flashing on fast reads (the directory dialog
waits 180 ms); footprint reservation is never delayed, because a slot that
appears late is itself a flash. New Chat's heading and automatic provider setup
need host readiness. `AppRuntime.primeProviders` warms the provider inventory
using the connection callback’s verified client, before the host snapshot is
published (the splash also awaits Local’s warm-up). Host configuration and model
catalogs warm alongside it without blocking the splash. Query owns deduplication and
freshness, so failed or expired prefetches can be retried. The last readiness
answer is remembered per host in device storage (`provider-readiness.ts`).
Readiness is requested immediately after inventory selects a default, independently
of optional preset, catalog, execution and MCP reads. Pending readiness retains
the composer and its control footprint; it is not a setup failure. Locally
configured command credentials (`unchecked`) and refreshable accounts
(`refresh_required`) are eligible for explicit first use without running either
mechanism during a read-only readiness check.

Session content readiness comes from its owned `SessionView`. Selected/root
metadata must still prove the command target, but tree decoration and queue
reads do not gate sending or replace live activity with a loading/unavailable
state. Tree failures get a scoped details notice; the transcript, composer draft
and focus remain mounted. Suspended/stale observation still disables commands.

Provider inventory, runtime configuration, model catalogs, and definitions have
five-minute inactive retention in the runtime's query defaults: closing the last
tab must not discard the metadata needed to paint the next New Chat. Freshness
stays at ten seconds; existing data remains visible while stale reads refresh.
Login flows and other queries retain the zero-GC default. Host detach clears its
retained queries, and no query cache is persisted to disk. Route bodies for tabs
the workspace already owns render nothing, so a route commit never paints their "missing" copy.

### Themes are foundational

The Go theme catalog and resolver feed `cmd/themegen`, which produces
`packages/ui/src/generated` data and static StyleX themes. Never hand-edit those
outputs or maintain a second browser palette catalog. All 66 currently shipped
themes, system appearance, and validated custom themes are supported.

The `claude-code` theme displays as **Claude Code** and uses the supplied Paper
desktop palette. A catalog spec may provide `displayName` and optional `web`
surface overrides (`navigation`, `quietBorder`, `codeBackground`,
`inlineCodeBackground`). These pass through the shared
Go resolver and generated catalog; the host wire uses snake_case keys, normalized
by `themeFromHost`. The browser validates hex colors and assigns only these fixed
roles. Missing overrides retain the existing derivation, and switching themes
always resets them. Do not special-case theme IDs in component styles.

`initializeTheme` applies appearance before first paint. `ThemeProvider` owns
selection/preview and applies the theme at the document root so portals, Markdown,
code, and controls agree. The selected theme's declared dark/light mode controls
derived surfaces; it must not be guessed from the OS when a named theme is active.

`adaptThemeForWeb` preserves the source palette while adjusting insufficient
foreground contrast for browser surfaces; the raw TUI palette and displayed
browser palette are intentionally distinguishable. Custom TUI JSON goes through
the home connection's shared resolver, then validation and the fixed variable allowlist.
No arbitrary CSS is accepted. Theme choice is a viewing-device preference and
does not write the TUI's chosen theme or host configuration.
Custom theme import lives beside the color theme control at the top of Appearance.
It uses the home connection only, with no execution-host selector or remote theme
configuration. A shared dialog and file button import bounded JSON (64 KiB), show
validation errors inline, and cancel pending imports when dismissed. Existing
imports stay available offline.

`appearance-data.ts` validates a bounded device display record: UI size 12–20,
code size 10–24, Inter or system UI font, JetBrains Mono or system monospace,
code wrapping, contrast (system/standard/increased), and motion (system/reduce).
Typography roles scale from the 13px UI default; code uses its independent 12px
default. Use these roles instead of new fixed font sizes. Code wrapping changes
presentation across Markdown, tool bodies and REPL without modifying copied text.
The compact theme picker reuses the full catalog and preview/cancel behavior in
one Base UI combobox popup. Its empty search query is separate from the selected
theme; selection stays in the trigger and checked row. The row places its type
(Light/Dark) before a swatch on the right. Swatches are miniature workspaces:
canvas, navigation, conversation and composer use the applied theme's surface
roles, with a small primary accent and its actual foreground. `browserSurfaces`
shares derived roles between document application and swatches, including custom
navigation overrides and the resolved increased-contrast preference. Syntax colors
do not stand in for the application's palette. Search uses the neutral inset input
treatment, with a complete focus border rather than an accent underline.
Appearance includes a bounded preview composed from real MessageRow and CodeBlock
components; it owns no SDK data or simulated client.

Tool density belongs to AppRuntime: Compact shows summaries, Comfortable adds at
most three lines/512 characters of already-loaded content, and Detailed initially
opens available tool details. Manual disclosure state wins for the mounted row.
Density does not auto-open reasoning or mailbox details and never dereferences a
tool/reasoning body handle; full tool content still requires an explicit user action.
Ordinary user/assistant canonical message parts are already supplied by SessionView;
mounted image previews use explicit owner-scoped bytes.

Increased contrast adapts semantic foregrounds and control/focus boundaries while
preserving the chosen palette. System contrast uses the browser media query, with
Electron's validated `nativeTheme.shouldUseHighContrastColors` snapshot/events
through desktop bridge v2. The asynchronous native snapshot cannot overwrite a
newer event. Reduced motion controls CSS durations and imperative tab movement;
system mode continues to follow the OS. Appearance reset restores display/theme
and density defaults but keeps imported themes. Persistence failures remain visible.

## Usage and execution limits

Whole-tree usage is a separate native `usage.get` / `session.usage()` projection,
not a sum of loaded turns, trace spans or overlapping budget rows. A read of the
root includes all original descendant attempts through immutable captured budget
ancestry, including deleted children; a fork starts fresh accounting. The backend
reads only scalar fields in one read transaction, with constant memory, a five
second deadline and a one-million-attempt ceiling. It returns a limit error rather
than partial totals. No second accounting ledger or mutable cache is introduced.

The Usage inspector reads the root even while a child is selected, sharing a
metadata query keyed by runtime, process epoch and root. It refreshes every ten
seconds while connected and offers an explicit refresh. Provider-reported and
captured-price estimates remain separate, with unknown settled cost counted
explicitly. Reserved, in-flight, settled, uncertain-outcome and cancelled-before-
dispatch counts are distinct; uncertain outcomes are a subset of settled attempts.
Input/output, reasoning, cached input/output and elapsed totals each carry known
and missing attempt counts. Explicit zero remains reported zero; missing evidence
is not zero. Saturated counters carry an overflow flag and display as lower bounds.
Reasoning and cache detail fields must not be added to input/output totals. The
selected agent's budget limits and reserved/uncertain exposure remain separate.

## Protocol and build boundaries

The active development wire protocol is **major 4**, generated from Go's native
registry. This is a deliberate fresh-store/runtime cutover, not negotiation with
retired protocol majors. Explicit schema/config versions live in the backend
contracts. Compatible build strings do not establish identity; transports verify
runtime and process epoch. Feature code uses typed services/generated validators,
not handwritten envelopes, duplicate DTOs or a private WebSocket.

Decimal counters/revisions remain strings; use BigInt-aware comparisons and
arithmetic. Preserve null versus zero versus absent. Generate standalone validators
ahead of time for strict CSP; never compile schemas with runtime eval. Read
[SDK APIs](../packages/sdk/README.md) and [native ownership](backend-domain.md).

### Gateway and daemon ownership

The runtime owns durable work and its Unix socket. The native gateway owns static
assets, bounded discovery, `/api/v4/ws` and scoped content transport with exact
Host/Origin checks; those checks are not authentication. Network trust remains
explicit. Browser requests cannot become trusted local terminal clients through
the socket hop. Gateway loss detaches observation and never cancels accepted work.
A replacement runtime is not silently substituted.

The shared Vite renderer is packed into CLI/desktop release artifacts; it needs
no Node or Vite server in production. Desktop consumes its versioned native bridge,
not a browser-origin fallback. Dev proxying requires an explicitly running matching
gateway; building frontend assets never upgrades or restarts a daemon.

[Vite configuration](../apps/web/vite.config.ts) owns route generation/splitting,
StyleX extraction before React, CSS layers and the required CommonJS prebundling.
Compile app/UI source even from package archives. The dev token pre-middleware
uses Vite's transform cache so StyleX constants exist before virtual CSS requests.
Generated routes/themes/contracts are never hand edited.

## Public documentation site

`apps/docs` is an independent React/TanStack Start site in the same Node 24/npm
workspace. It follows the structural conventions of inference's `fast-web`, not
its backend or deployment. Start renders during the build; only the generated
`apps/docs/dist/client` HTML/assets and a small Cloudflare asset-routing Worker
are deployed. There is no React runtime server,
SDK, daemon connection, React Query, authentication, analytics or remote content.
The root `/` and `/docs` redirect to `/docs/quickstart`; there is no landing page.
Legacy entry/installation/CLI/permissions URLs retain redirects.
The `/docs/...` articles are separate from the application renderer and must
never be embedded by `pack:web` or included in desktop builds.

The docs component library is app-owned under `apps/docs/src/components` and
`src/features/docs/components`. It uses Base UI primitives with StyleX for
component styles, but keeps its own **docs-local** tokens
(`apps/docs/src/tokens.stylex.ts`) that reference the site's semantic custom
properties — it never imports `@whip/ui` or the application theme catalog, so
public-site styling stays independent. Global base styles, the Paper token
custom properties, and MDX prose element styling remain plain CSS under
`apps/docs/src/styles`. StyleX is configured with `runtimeInjection: false`
and no CSS layers so prerendered pages stay fully styled with JavaScript
disabled. The
[Paper board](https://app.paper.design/file/01M3A92K3HM4T5QF90SJFKQR0A/p-1-0/1O0-0)
and its Light counterpart are the visual reference; [brand-guide.md](brand-guide.md)
documents the local contract. Explicit user requirements supersede the board:
code fences use multicolour Carbonfox syntax tokens; prose links and inline code
match whip's transcript (blue underlined links, green borderless inline code).

Trusted checked-in MDX lives in `apps/docs/src/content/docs/<slug>/index.mdx`.
It is executable source compiled at build time, never accepted from remote or
untrusted input. Folder paths define URLs; frontmatter defines title,
description, section and order; optional `navTitle` preserves a shorter sidebar
label without changing the article title. Generated metadata provides navigation, heading
anchors and the complete prerender URL list. Refractor tokenizes fenced code in
the compiler; token spans reach the browser, but highlighter code and grammars
do not. Keep raw snippets page-local for copying, not in a global content index.
All articles include title-row copy/download actions. Their trusted MDX source
is imported in page-local chunks, not the global manifest; Vite raw
imports bypass the MDX compiler. Menus, theme preferences and selected code tabs
use local component state. The public content currently consists of 21 V1 pages
(Quickstart, Download and TypeScript SDK written, 18 other outlines) in five groups: Getting Started, Using whip, Configuration, Agents & RLM,
and Developers. Group labels/order are shared between compiler and sidebar in
`sections.ts`. Full-article library examples remain in Storybook, not production.

Use `npm run dev:docs`, `check:docs`, `test:docs`, `test:docs:dev`, `build:docs`,
`preview:docs`, `test:docs:browser`, `storybook:docs` and
`build:storybook:docs` from the workspace root. See the app README for the
content contract, static preview, tests and publication procedure. Storybook
exists for component/board review, not as a public production route.

Public docs deploy automatically from `main` to `https://inference.net/whipcode`
(`whipcode-docs`) and from `development` to `https://inference.cool/whipcode`
(`whipcode-docs-preview`). `DOCS_ENVIRONMENT=production|preview` selects fixed
canonical URLs and indexing policy; a nonempty `DOCS_SITE_URL` alone never
permits indexing. Preview emits noindex metadata and response headers,
disallow robots and no sitemap; production publishes an article sitemap.
Default local builds remain root-path noindex previews.

`apps/docs/wrangler.jsonc` records only exact `/whipcode` and `/whipcode/*` routes;
root websites, assets, robots and sitemaps remain untouched. The independent
`docs-deploy.yml` workflow validates/builds a target-specific static package
without credentials, then uploads/activates an exact Worker version using a
branch-restricted environment's per-Worker Editor token. CI never changes zone
routes and never deploys from PRs or called release workflows. Deploys serialize
per branch and skip stale sources. See the app README for bootstrap, recovery
and local/live `worker-smoke.mjs` checks. Vite/TanStack own the router/asset base;
plain/MDX links use `sitePath`. No runtime rendering or backend state is added.
Existing engineering
docs remain engineering references; migrate user-facing material deliberately
rather than exposing all of `docs/` or maintaining divergent installation claims.

## Working on a change

1. Read the [feature map](features.md), locate the existing state owner and nearest
   implementation, and decide what survives navigation and what must be disposed.
2. Add a native host/SDK capability only where the current contract cannot express
   the behavior. Reuse typed services, views and controlled UI primitives.
3. Preserve loading, unavailable, conflict, uncertain delivery, long content,
   concurrent changes, keyboard, selection, focus and narrow-layout behavior.
4. Run meaningful checks at the changed boundary. Keep exact passed/failed evidence
   and retained test disposition in the development log; update this guide when
   an architectural decision changes.

| Area | Starting source |
| --- | --- |
| Host and window lifetime | [hosts.ts](../packages/app/src/hosts.ts), [runtime.ts](../packages/app/src/runtime.ts) |
| Durable recovery | [SDK command.ts](../packages/sdk/src/command.ts), [app storage](../packages/app/src/recovery-storage.ts) |
| Composition and submission | [compositions.ts](../packages/app/src/compositions.ts), [chat-submission.ts](../packages/app/src/chat-submission.ts) |
| Transcript/execution/trace | [SDK state](../packages/sdk/src/state.ts), [execution](../packages/sdk/src/execution-state.ts), [trace](../packages/sdk/src/trace-state.ts), [timeline](../packages/app/src/timeline.tsx) |
| Workspace and reading | [session-tabs.ts](../packages/app/src/session-tabs.ts), [reading-positions.ts](../packages/app/src/reading-positions.ts) |
| Generic UI and themes | [UI inventory](../packages/ui/README.md), [tokens](../packages/ui/src/tokens.stylex.ts), [theme generator](../cmd/themegen) |
| Mobile ownership | [runtime](../apps/mobile/src/runtime/runtime.ts), [commands](../apps/mobile/src/runtime/commands.ts), [storage](../apps/mobile/src/runtime/storage.ts) |
| New protocol contract | [native registry](../internal/protocol/schema.go), [SDK guide](../packages/sdk/README.md) |

## Development and validation

Use Node 24, the pinned Go toolchain and the existing npm lockfile. Active required
checks are defined in [Taskfile](../Taskfile.yaml) and the
[redesign workflow](../.github/workflows/backend-redesign.yml): native contract/SDK/
process/CLI gates plus product web, Chromium/Firefox browser, mobile, Apple Silicon
desktop and examples. Read their actual commands rather than inferring coverage
from a generic test name. Build/pack assets before production browser fixtures.

`npm run generate`, `build`, `check` and `test` target the supported native-v4
protocol and SDK. `npm run acceptance` exercises actual native runtime, gateway,
executor, shell, computer, desktop discovery and browser-provider fixtures.
`npm run test:browser` builds the production renderer and runs Chromium/Firefox
acceptance. `npm run test:package` installs packed native archives into an isolated
consumer, checks public imports/types and browser bundling, then submits a real
input to its own disposable native runtime. The normal product gate discovers
all remaining Go packages; retired core and legacy SDK/protocol fixtures have
been removed with their [behavior disposition](backend-native-core-retirement.md).

`task check:product-web` checks shared app/UI and assets.
`task check:product-browser` uses disposable native hosts and production assets.
`task check:product-mobile` includes native backend fixtures and both Expo exports.
`task check:product-desktop` validates staged native bridges and ad-hoc packaging.
`task check:product-examples` runs real native agent and browser/Node examples.
Public docs use their separate `check:docs`, `test:docs` and `build:docs` commands.
No gate modifies an installed runtime or real account.

Retained specialized browser/performance probes now use native fixtures;
a historical filename or old green run does not establish final-revision coverage. The
[development log](backend-redesign-development.md) records exact source, failures,
repairs and remaining acceptance. Actual Safari, physical mobile, VoiceOver,
remote SSH and signed packaging claims require their own evidence. Measure retained
bytes/subscriptions and real latency without weakening host durability or replacing
canonical provider work with injected frontend events.

## Maintaining one canonical resource

This guide owns frontend architecture and design. The UI README owns component
APIs; the SDK README owns public client APIs; backend-domain owns native durable
contracts; feature and development records own disposition/evidence. Link to these
rather than append duplicate architecture sections. Historical plans retain research,
not competing current implementation requirements. Reconcile disagreements with
actual source and record the chosen behavior; never silently retire a feature by
removing its prose from this architecture guide.
