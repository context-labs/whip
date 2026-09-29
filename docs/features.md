# Feature map

This map describes native WHIP behavior and its current source owners. Exact
protocol/domain rules live in [the domain contract](backend-domain.md); shared
frontend ownership lives in [the frontend guide](frontend.md). Migration,
performance and platform acceptance remain separately recorded in
[the development log](backend-redesign-development.md). A listed surface or a
passing unit test is not a claim that every release/manual check has passed.

## Recursive agent runtime

Roots and children share one session, input, turn, history, operation and
accounting model. Definitions capture immutable module/tool/hook bindings and
configuration; they do not own a second agent loop. Creating or reading a session
does not allocate a worker or execute a model. Durable queued input and due mail
make work eligible for bounded scheduling.

Spawn commits the child, exact original input, captured configuration, delegated
standing grants and scoped content together. Children may recurse within
ancestor resource and authority limits. Completion does not delete the child.
`agents.wait_after_cell` registers exact descendant input targets and releases
the parent's cell/kernel before waiting; failed child outcomes are evidence,
not automatic retry. Pending completion publication cannot rerun child work.

Sources: [runtime](../internal/runtime), [session](../internal/session),
[store](../internal/store), [runner](../internal/runner),
[recursive runtime guide](rlm-runtime.md).

## Selectable execution engines

A tree chooses Starlark or QuickJS when created; children and forks retain that
language. Both expose the model-facing `execute` tool and captured host modules.
Workers have bounded process, memory, wall-time, output and host-call lifetimes.
The interpreter has no ambient filesystem/network/process authority.

Checkpoints are immutable evidence committed with cell outcomes. Eviction and
restart restore validated state without evaluating old cells. Partial or unusable
checkpoints remain explicit; uncertainty cannot silently load an older image.
Sources: [engine](../internal/engine), [checkpoint contract](backend-domain.md#code-execution-and-checkpoint-boundary).

## Focused context

Raw history, selected model context, summaries and scoped content are distinct.
Compaction preserves raw messages and exact assistant/tool boundaries while
recording forward source coverage. Context inspection/search/read remains bounded
and owner-scoped. Captured instructions and explicit helper selection determine
the request; an error is not permission to replay completed cells.
See [context and compaction](agent-loop.md#context-and-compaction).

## Messages and collaboration

Authored mail, completion evidence and state notifications have separate
provenance. Senders can address permitted relatives in the same tree. Delivery
classes distinguish queued, steering and next-turn work. Human inspection does
not acknowledge agent delivery. Only successful presentation advances the
observed revisions; a failed mail-only turn remains blocked until explicit input
succeeds. Recipient-owned evidence survives deletion of its original sender.

Private session state and shared tree state use exact revisions, immutable
bodies, CAS and bounded subscriptions. See [mail](backend-domain.md#mail-and-presentation)
and [state](backend-domain.md#explicit-state-and-immutable-values).

## MCP

The host owns bounded server connections, catalogs and checked calls. Native
configuration, import provenance, explicit session selection and delegated
fingerprints determine availability and trust. Catalog reads do not connect a
server or authorize its tools. Refresh/reconnect is explicit; an uncertain
transmitted mutation is never replayed automatically.

Typed text, structured content and attachments retain scoped operation evidence.
The external `whipcode mcp serve` endpoint uses a model-free native owner and
restricted tool aliases, with workspace reads preauthorized and interactive
consent denied. It does not expose interpreter execution.
See [MCP contracts](tools.md#mcp), [connection manager](../internal/mcp) and
[native MCP operations](../internal/runtime/mcp_operations.go).

## Built-in capabilities

| Capability | Native owner and contract |
| --- | --- |
| Workspace list/search/read/write/patch and diagnostics | [File services](../internal/tool/workspace.go); captured cwd, canonical path validation and operation authorization. |
| Foreground and background shell work | [Shell runtime](../internal/runtime/shell.go), bounded [process owners](../internal/capability); separate output, input and cancellation authority. |
| Browser automation | [Scoped Browser host](../internal/browserhost) and [runtime](../internal/runtime/browser.go); exact offered provider/tab identity and explicit attachment grants. |
| Computer use | [Computer runtime](../internal/runtime/computer.go), host application policy and scoped images; supported macOS driver remains separate from browser control. |
| Questions | [Native questions](../internal/runtime/questions.go); root-only, bounded pending state, answer/dismissal/expiry and joined cancellation. |
| Stateless model analysis | [Model helpers](../internal/runtime/model_helpers.go); captured route, ordered batch results and retained accounting. |
| Immutable artifacts | [Artifacts](../internal/runtime/artifacts.go); owner-scoped bounded content, never ambient filesystem handles. |
| Goals and schedules | [Goals](../internal/runtime/goals.go), [schedules](../internal/runtime/schedules.go); explicit completion, bounded continuations and durable due input. |

External Chrome live/dedicated/headless/extension modes are a retained capability
whose native owner/configuration port is still open in the development record.
The old `browser.mode` configuration does not activate them in the native host.
This is an unresolved migration obligation, not an approved feature retirement.
[Browser and computer use](browser-computer-use.md) tracks their integration.

## Provider loop and models

Provider requests capture their route, account generation, instructions, sampling
and limits. Reservation, dispatch, outcome and settlement remain separate. Missing
usage/cost stays unknown. Database settlement retries cannot redispatch a provider
request, and uncertain partial streams never authorize automatic regeneration.

### Models.dev metadata and named local keys

The reviewed [model catalog](../internal/modelcatalog) supplies offline metadata;
live account-scoped catalogs determine current membership. Explicit environment,
file, command, managed-account and no-auth sources replace guessed credential
fallback chains. See [models and providers](models-providers.md).

### Provider connections and environment discovery

Settings and terminal setup distinguish templates, configured routes, credential
readiness, model membership and defaults. Revisioned updates preserve concurrent
edits. Metadata inspection never runs a credential command or makes inference.

### Provider onboarding and the first message

Setup retains the unsent draft and original scope. Choosing a provider/model or
saving a default does not submit a prompt. Exact verified suggestions can simplify
selection; unavailable choices remain explicit. Existing sessions keep captured
configuration rather than inheriting every subsequent host edit.

### Known provider picker and local credentials

The [provider presets](../internal/providerhost/presets.go) define current supported
setup templates and endpoint/key names. Secret input is masked and never read back
into forms, stored in a transcript or replicated into ordinary client state.

### File-backed custom provider configuration

Native `runtime-v4/host.json` declares explicit routes. Interactive editors use
host revision checks; custom endpoints and model limits retain their identity
instead of inheriting a coincidentally named preset. See
[custom endpoints](models-providers.md#supported-provider-types-and-custom-endpoints).

### ChatGPT subscription provider

Device login, refresh, logout and inference share one host-owned account manager.
The fixed subscription route requires a reviewed natural output ceiling. Public
status is redacted; private continuation evidence and credential generations
cannot cross account/route/session boundaries. See
[subscription behavior](models-providers.md#openai-chatgpt-subscription).

## Daemon and clients

The native host owns execution, SQL facts and live resources. CLI/TUI, ACP, SDK,
shared web/Desktop and mobile are clients of the same versioned services.
Disconnect, unmount or cancellation of a local wait leaves accepted host work
running. Explicit cancellation identifies the exact turn/operation.

Native Unix transport pins runtime identity; ephemeral resource writes also pin
the process epoch. The gateway validates its host/origin and network policy and
serves scoped bounded content. Human terminals require explicit network enablement.
Root and child observation use the same bounded services and synchronized views.
See [protocol](protocol-v2.md), [gateway](../internal/gateway),
[Go client](../internal/client) and [ACP](../internal/acp).

## Session naming

Authored fallback titles are immediate. Automatic title work uses captured
maintenance requests and billed evidence, then applies exact tree/title revision
checks; it never becomes an ordinary assistant message. Explicit rename wins
against stale candidates. Catalog revision updates refresh unopened/off-page
conversations without hydrating their histories.
Sources: [title runtime](../internal/runtime/title.go),
[title browser acceptance](../apps/web/scripts/session-title-notifications.mjs).

## TypeScript client SDK

`@whip/protocol` generates exact native DTOs and CSP-safe validators from Go.
`@whip/sdk` owns transport validation, precise decimal counters, inert root/child
handles, bounded history/activity/input services, immutable synchronized views and
exact-request recovery. Receipt identity alone is not proof of an original payload;
recovery matches the submitted request before inferring acceptance.

React subscriptions consume SDK snapshots. Applications own drafts, selection,
tabs and reading positions instead of creating a second daemon reducer.
Fresh bounded recovery journals never replay retired protocol records. Node and
browser package entry points, installable package smoke tests and client/agent
examples use the native contract. See [SDK usage](../packages/sdk/README.md) and
[the native web inventory](native-web-workflows.md).

## macOS desktop application

Desktop uses the shared production renderer with thin native connection, clipboard,
file, drag/window and browser/terminal capabilities. Local, URL and SSH connections
retain explicit host identity and independent lifetimes. Replacing a backend is an
explicit managed action with source/hash/ownership checks; a renderer does not
silently install or restart the runtime. Packaging reuses the tested renderer.

### Experimental desktop Browser tabs

Human pages use isolated profiles. Exact conversation offers and Once-only resource
consent determine agent attachment. Design Mode captures bounded selected-element
and optional screenshot evidence into an ordinary explicitly addressed draft;
it does not grant browser control or automatically send work. Disconnect/release
ends agent control without closing the human page. Preview forwarding remains
bound to the selected SSH host. See [desktop](desktop.md) and
[frontend Browser ownership](frontend.md#native-browser-workspace-boundary).

## React web application

The web app shares presentation and product behavior with Desktop. It includes
session/sidebar search, archived filters, root/child navigation, independent tabs
and panes, draft text/images/design context, host workspace selection, model and
permission controls, scoped attachments and history actions. Settings uses the
same native services as execution. A stale reply cannot replace a newly selected
owner or draft.

Production probes exercise actual native runtime inputs and content; fixtures do
not fabricate retired protocol event reducers. Exact method-to-UI ownership and
current SDK-only boundaries are listed in [the workflow inventory](native-web-workflows.md).

## Public documentation site

The [documentation app](../apps/docs) owns the public static site and its own
visual/browser checks. It consumes maintained product documentation; historical
plans are research and delivery records, not a second implementation contract.
See [frontend package boundaries](frontend.md).

## Session information bar and contoured tabs

Shared [workspace tabs](../packages/ui/src/workspace-tabs.tsx) retain keyboard,
overflow, close/reopen and accessible focus behavior across chat, REPL, browser and
terminal panes. Session metadata and activity are bounded catalog reads; background
tabs do not open every transcript. Reading positions and local drafts remain
scoped to their host/owner/view.

## Chat activity

Canonical messages, cells and host operations retain distinct provenance.
Provisional reasoning/code/output stays visibly live until settlement. Structured
file/shell/mail/child activity uses the correct owner and exact execution evidence;
large results expose bounded reads instead of claiming a clipped value is complete.
Copy, selection, expansion, reader anchors and follow behavior survive ordinary
stream updates and virtual remounting. Sources are in
[shared app](../packages/app/src) and
[native activity acceptance](../apps/web/scripts/native-chat-activity.mjs).

## Mermaid diagrams in chat

Complete settled Mermaid fences render through a worker. Streaming or explicitly
incomplete source remains source; oversized diagrams retain visible bounds and
exact source copying. Source/diagram preference is per occurrence, including
identical fences. Expansion, focus return, reload, late image/theme replacement
and older history preserve the reader's place. See
[the native acceptance audit](../apps/web/scripts/native-mermaid-audit.md).

## Conversation row actions

Rename, archive/restore, copy, export, fork, rewind and delete use exact displayed
identity and revision. Catalog mutations do not require transcript hydration.
Fork creates a fresh root/REPL with authorized selected history; rewind invalidates
the old REPL boundary and requires the stopped owner's exact history snapshot.
Workspace effects have separate capture/restore outcomes and are never implied by
history editing. See [native session actions](../apps/web/scripts/session-actions.mjs).

## Terminal UI behavior

The native terminal mounts provider/account setup, command/file/skill completion,
root/child navigation, REPL and passive context/agent/LSP panes, history actions,
permissions/questions, model/budget controls, export/report, notes and host standing
instructions. Unicode selection, safe links, bounded markdown and theme contrast
share pure presentation code; execution and recovery stay in native host services.

Input recall keeps original local typed/pasted/image/design parts and lazily reads
bounded cross-session human text without copying foreign authority. Escape can
retain/clear the local draft or leave a child; double Ctrl+C targets the observed
turn. Detach never cancels accepted work. Interactive shell input requires explicit
focus, exact owner/operation/epoch/sequence and a single bounded in-flight write;
unknown acknowledgement discards buffered intent instead of replaying it.

Automatic theme detection uses the terminal framework's input ownership. Explicit
theme choices remain authoritative. tmux/mosh hints use bounded read-only inspection,
and unsupported background detection uses neutral colors. Sources and actual host
race tests live in [native TUI](../internal/tui).

## Storage and recovery

The fresh `runtime-v4` namespace contains native host declarations and canonical
SQL/content state. Old directories remain untouched; there are no compatibility
migrations or old identity reuse. Admission survives lost acknowledgement; queued
unclaimed work survives restart. Claimed work is interrupted and never replayed
speculatively. Failed/uncertain effects remain inspectable evidence.
See [storage/domain ownership](backend-domain.md) and [setup](setup.md).

## Transcript navigation

History windows pin owner, revision and upper boundary. Pagination follows exact
returned cursors; inserts, rewind, deletion and empty bounded pages cannot invent
continuation arithmetic. Open previews and copy/export actions retain captured
source identity while new messages arrive. Apps preserve reader anchors and
explicit jump-to-latest intent independently from SDK history state.

## Skills

Skills are discovered from declared host/project roots and read within captured
instruction authority. A skill's text cannot widen filesystem, tool or browser
grants. Completion is bounded metadata; choosing a suggestion preserves the draft
until explicit submission. Context audit distinguishes the applied instruction
manifest from a later filesystem preview. Sources:
[instruction capture](../internal/instruction), [skills](../internal/skills),
[host services](../internal/hostview).

## Themes

One reviewed theme catalog supplies native terminal and shared UI colors.
Explicit appearance, automatic detection, custom host resolution and portaled
controls share their respective state owner. Theme generation/drift and contrast,
ANSI width, CSP and accessibility checks remain required. See
[theme package](../internal/theme), [UI package](../packages/ui) and
[frontend styling](frontend.md).

## Web directory navigation

Directory choices refer to the selected execution host. The native host's bounded
list/pick services and app workspace picker preserve exact cwd identity and reject
stale selection. A remote path is never interpreted as a local renderer path.
Changing a session workspace requires the native idle-state/configuration checks.

## Multiple execution hosts in the web workspace

Stable local/URL/SSH profiles keep separate clients, native identities, drafts and
session destinations. Disconnecting one host does not retire another host's state.
Fresh client namespaces prevent old IDs or recovery records from targeting a new
runtime; replacement identity requires explicit selection. See
[connections](../packages/app/src/connections.ts), [host ownership](../packages/app/src/hosts.ts)
and [remote configuration](../internal/config/remote_hosts.go).

## Model usage budgets

Every ordinary and helper model attempt retains reported/unknown usage and cost
source. Exact decimal counters avoid JavaScript rounding. Resource and model-budget
limits apply through ancestor scopes without resetting incurred spending when a
model changes. Read-only usage/budget inspection is distinct from administrative
control APIs; see [accounting](backend-domain.md#model-budgets-and-retained-accounting)
and [the web workflow inventory](native-web-workflows.md).

## WhipCode distribution

CLI/runtime/Desktop artifacts use explicit build ownership, canonical names and
matching source/renderer provenance. Native startup and compiled replacement are
tested with disposable directories and verified process identity. Installer,
signing, updater and publisher checks retain their own release gates; a local
unsigned fixture is not signed-release acceptance. See
[desktop packaging](desktop.md#package-signing-and-canonical-installation) and
[release runbook](desktop-releases.md).

## Native mobile companion (development)

The mobile client consumes the same native SDK contract and immutable views.
Suspension/resume, network identity, explicit cancellation, images and pending
permissions/questions preserve host ownership. Its bounded recovery metadata does
not persist secret captures or replay an uncertain prompt. Mobile build/tests and
physical-device acceptance remain distinct. See [mobile app](../apps/mobile).

## Canonical Frontier evaluations

The native evaluator builds scenarios through current Go/SDK boundaries and records
ordinary attempts, tokens, cost source and task outcomes. Deterministic scripted
qualification spans both isolated engines; live model quality/cost work is an
explicit separate run. Historical evaluation files remain read-only evidence and
do not authorize executing a retired runtime. See [evaluation guide](../evals/README.md).
