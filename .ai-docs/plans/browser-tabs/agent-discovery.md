# Agent-initiated Browser tabs and tool-based discovery

Branch: `compaction-loop-and-ui-cleanup`
Status: approved by the user and implemented in the working tree. The research below records the pre-change state; current behavior is documented in `docs/browser-computer-use.md` and `docs/frontend.md`.
Research date: 2026-09-18 UTC.

## Goal

An agent should be able to request a new visible Whip Browser tab without the
human first creating or offering a tab. Ordinary Ask-mode permission approval
should be the human interaction needed. An agent should discover the tabs it can
request/control through a tool, not dynamic system/user-message inventories.

Keep the current native-page execution model, durable permission dispatcher,
exact selected-client transport, isolated profiles, and SSH-only previews.
Do not add a second browser, Playwright connector, public debugging port, or
arbitrary remote browser endpoint.

## Findings in pre-change source

- The model already has `browser.open`, `attach`, `run`, `detach`, and
  `allow_preview_port`; there is no module-level listing operation
  (`internal/rlm/modules.go:27`, `internal/rlm/guide_fragments.go:40`).
- `RunDesktopBrowser` checks module authority, resolves exact resources, then
  dispatches through the durable permission system
  (`internal/tools/browser_desktop.go:96-151`). `browser.open` already creates
  a tab and returns control in one operation; it should not require a second
  attach approval for that new page.
- The blocker before that workflow is provider selection:
  `browserProviders.Resolve` rejects requests without a selected root provider
  (`internal/daemon/browser_provider.go:247-264`). The app selection path insists
  on an existing Browser tab, obtains its identity, and sends a one-tab offer
  (`packages/app/src/browser-provider.ts:64-100`). This is a UI/bootstrap
  prerequisite, not a fundamental requirement of creating a page.
- Native main already has the authoritative inventory. `BrowserControl.identity`
  projects it with IDs, generations, title, URL and preview identity
  (`apps/desktop/src/browser-control.ts:35-44`). It is a trusted-app bridge,
  not permission to expose the entire inventory to an agent or remote daemon.
- Model context currently includes only live, owned attachments
  (`internal/daemon/recursive_runtime.go:904-913` and
  `internal/daemon/browser_provider.go:393-405`). The root's offered but unattached
  tabs are not discoverable through a model tool. A bind-time offer is also not
  a fresh native inventory; unowned tabs can navigate or close afterward.
- Ask/automatic policy already exists. `decideDesktopBrowser` waits for approval
  in Ask mode and can permit automatically under the existing automatic policy.
  Headless/deny execution remains a distinct fail-closed path. Its explicit
  comment is important: automatic permission is not a resource selector
  (`internal/tools/browser_desktop.go:279-299`). Preserve that distinction.
- A separate diagnosed defect must be fixed first: native dispatch validates
  `operation_id` with the tab-ID validator, which rejects the colon-separated
  IDs of real model invocations. The earlier live-call diagnosis reproduced
  `Invalid browser identity` for the failed operation ID; direct native fixtures
  using UUID operation IDs missed it
  (`apps/desktop/src/browser-control.ts:168,293-296`,
  `apps/desktop/src/browser-policy.ts:12-15`,
  `apps/desktop/scripts/browser-control-native.ts:35-37`).

## Recommended model-facing API

Keep the current API and add one operation:

```python
browser.list_tabs()
browser.open(url="https://example.com")
browser.attach(tab_id="<tab from list_tabs>")
browser.run(attachment_id="<returned attachment>",
            expected_document="<returned revision>",
            code="<existing Browser helper commands>")
browser.detach(attachment_id="<attachment>")
```

`list_tabs` is a proposed name. It is a module operation, not the legacy `tabs()`
helper inside a `browser.run` code batch. Mirror it as `browser_list_tabs` for
Whip's own MCP tool host, through the same permission/resource checks; this does
not reintroduce third-party Playwright MCP.

A listing returns a bounded structured result with availability state and tab
records: opaque `tab_id`, title, URL, document revision, state, whether control
may be requested, and an `attachment_id` only if owned by the calling agent.
Distinguish empty inventory, no eligible desktop, stale provider, missing module
capability, and a tab controlled elsewhere. Never return another agent's control
handle, profile paths, socket addresses, browser-wide CDP targets, DOM, or cookies.
URLs and titles are untrusted data, not instructions; bound them and redact URL
credentials. No page execution or navigation is needed to list metadata.

### What does “available” mean?

Recommended first increment: list only resources already in this agent's allowed
discovery scope, not every page open on the person's machine:

- tabs explicitly shared with this conversation (for the root);
- tabs created for this agent, where their discovery association is still live;
- live attachments owned by or explicitly delegated to this agent.

Listing already-shared metadata should not ask for another control permission.
It still requires the browser module/operation grant and exact live scope checks.
A listed/requestable tab is not an authorized attachment: attaching needs its own
permission. After detach, a still-shared/agent-created human tab can remain
requestable; ending control should not erase independent tab availability.

Children must not gain all of the root's offered tab metadata simply by inheriting
the browser module. Initially list their own/delegated resources only; preserve
explicit attachment transfer and resource narrowing.

If the desired product behavior instead includes discovering *all* Whip tabs in
a window without per-tab sharing, add an explicit window-inventory read permission.
Titles/URLs can be private. Such permission is distinct from control and must be
bound to the agent, root, window and provider epoch. Do not silently implement
whole-window discovery behind the safe subset listing. This broader mode is an
optional follow-up, not a prerequisite for autonomous creation or useful listing.

## Provider availability versus authority

The key change is to stop equating “there is a desktop that can handle requests”
with “the user has already selected a tab for control.”

1. A connected desktop advertises a bounded, root-scoped **availability** record
   when it is serving that conversation. It can do so with zero Browser tabs.
   Include native desktop/window identity, exact host connection epoch, supported
   operations and an originating workspace destination; not page inventory.
2. Availability authorizes no control, navigation, cookies, or preview network.
   It must not activate a debugger, realize restored pages, or start an SSH tunnel.
3. `browser.open` resolves a concrete candidate and reserves inert identities
   before permission admission. The permission request names the actual desktop
   destination, initial URL and create-plus-control scope.
4. Approval is the explicit authorization to use that destination and create the
   page. Activate the selected-provider relationship through the existing
   admitted transport path; do not execute against a candidate merely because
   it connected or because the app has focus.
5. Revalidate the exact holder, native window/profile, provider epoch, root/agent,
   and proposed resource scope after approval. Then use the current native
   create -> renderer admission ACK -> realization/control path. Return the
   attachment and current document revision only after successful admission.

This deliberately changes the old manual-offer prerequisite. Existing explicit
“Offer Browser to conversation” remains a shortcut for sharing a human page,
not a prerequisite to create a new one.

### Destination selection

Use an existing explicit destination, or the unambiguous conversation-origin
candidate shown in the permission request. Do not use first-connected,
most-recently-focused, or newest-wins rules. An active selected provider cannot
be stolen by another window advertising availability.

If several candidates are valid and none is selected, the human must select one.
The selection can be part of the request UI, but final scope resolution/digest
must happen *after* the choice and *before* approval. Do not approve a wildcard
and substitute a destination afterward. The simple first implementation can
return `desktop_selection_required` with a clear UI action instead of adding a
compound chooser immediately.

Automatic/Full Access can bypass a permission prompt under existing policy; it
cannot pick an ambiguous desktop or broaden an existing resource scope. A
terminal/headless session with no eligible desktop gets a clear unavailable
result, never a silently launched external browser. Reconnection can advertise
availability again, but cannot restore old control/discovery grants or silently
reselect a replacement provider.

## Permissions and lifetime

| Operation | Recommended authority |
| --- | --- |
| Advertise desktop availability | Trusted connected application; no page or network authority |
| List already-available tabs | Module/operation authority + per-agent discovery filter; no extra control grant |
| Read all tabs in a window, if added | Separate explicit metadata-read permission |
| Open a new tab | Ask-mode approval for one creation and its scoped control attachment |
| Attach to a listed human tab | Ask-mode approval for that exact tab and generation |
| Run actions | Existing live attachment authority; no prompt for every click |
| Expand an SSH preview port | Existing separate preview-network permission and exact saved SSH identity |
| Detach/revoke | Ends control, not the independent human tab |

Keep v1 approvals Once-only; a Once approval can establish a live scoped
attachment, not merely allow one click. Do not introduce a remembered wildcard
browser rule as part of this work. No page creation or target-network request
before approval. Cancellation/denial leaves no realized orphan page or tunnel;
uncertain delivered mutations are reported, not replayed.

Durable permission decisions and live native capabilities are different: old
attachment IDs cannot authorize restored tabs after daemon/window restart.
Old roots/definitions lacking the new operation stay fail-closed; do not silently
widen their stored capability lists. Define the supported upgrade/new-root path
and surface it in errors.

## Inventory implementation

Reuse Electron main's inventory; do not create a second authoritative tab store.
The daemon's cached offer/attachment records only determine eligible resources.
Fetch or synchronize fresh native metadata over the exact provider connection,
filtered to those resources *before* disclosure to an agent/remote execution host.
A lightweight typed inventory exchange belongs on the existing provider transport;
listing does not require CDP, a fake attachment, or a made-up tab grant.

Closed, re-created, or navigated pages can change between list and attach/run.
Treat list output as observation only and revalidate native generations and
policy on use. Preserve generation-stable tab identity and document-revision
checks. Keep limits aligned with existing 8 live Browser / 32 workspace descriptor
bounds; report any truncation explicitly. Listing must not wake suspended or
unrealized restored pages or establish preview routes.

After this API is reliable, remove the per-turn attachment inventory injected
into input by `focusInput`. Keep static tool usage guidance. Spawn/transfer tool
results may still return explicitly delegated attachment metadata, and listing
provides recovery after context compaction without replaying open/attach effects.

## Approved implementation sequence

1. **Repair the real invocation boundary.** Use the actual operation-ID contract
   independently of the native tab/attachment-ID contract. Audit all native
   dispatch/cancel/event validators that handle that field. Improve structured
   diagnostics without logging page contents or secrets. Add a real-format
   regression before changing discovery/bootstrap.
2. **Add bounded `list_tabs` for current explicit associations.** Establish
   fresh metadata, per-agent filtering, stale/busy states and reconnect behavior.
   This immediately fixes the current offered-tab discovery gap without widening
   window visibility.
3. **Remove the existing-tab prerequisite for opening.** Add candidate
   availability and a zero-tab/create-only path to the same provider lifecycle.
   Reuse durable permissions and admission; approval is the explicit action,
   not an extra manual Offer step. Preserve exact host/window targeting.
4. **Finish model UX and optional wider discovery.** Static native-first guidance,
   useful error states, no dynamic prompt inventories. Only add whole-window read
   access if that visibility choice is approved separately.
5. **Validate through the complete path and document the behavior.** Do not call
   this done based on native fixtures or mocked SDK calls alone.

### Expected source surfaces

- Model API: `internal/rlm/modules.go`, `guide_fragments.go`, engine/module tests;
  `internal/agentdef/operations.go`; `internal/tools/browser_desktop.go` (including
  Whip MCP schemas); module admission/definition compatibility tests.
- Broker/authority: `internal/browser/desktop_types.go`,
  `internal/daemon/browser_provider.go`, `browser_rpc.go`, `browser_execute.go`,
  and the existing capability/session browser scope code where admission needs
  candidate support. Reuse the dispatcher rather than a parallel consent store.
- Wire/transport: `internal/protocol/browser.go`, registry and generated protocol
  artifacts; `packages/sdk/src/browser.ts`. Negotiate the changed protocol/API
  shape; old clients must fail clearly rather than partially support it.
- Desktop/app: `packages/app/src/browser-provider.ts`,
  `browser-provider-controls.tsx`, `browser-agent-types.ts`, permission UI as
  needed; `apps/desktop/src/browser-control.ts`, policy, trusted IPC/preload and
  `BrowserManager`'s existing inventory/admission seam. Keep generic UI components
  unaware of Browser authorization.
- Prompt cleanup: `internal/daemon/recursive_runtime.go` and attachment-context
  tests; retain explicit delegation metadata in spawn/inspect tool results.

## Acceptance plan

- Real RLM-issued open from an empty workspace, no manual Browser tab/Offer;
  real operation-ID shape reaches daemon -> SDK -> preload -> Electron.
- Ask mode: zero native realization/navigation/SSH network before permission;
  allow creates exactly one tab and usable attachment; deny/cancel creates none.
- New tab appears in captured originating pane without stealing focus; subsequent
  read/fill/click/navigation occurs on the same human-visible page.
- `list_tabs` -> attach -> run with no tab IDs injected in system/user messages;
  no cross-root/child/window inventory or control-handle leaks.
- List after navigation/close/recreate/detach/transfer/compaction; current metadata,
  useful stale/busy results, and no hidden restore/network effects.
- Full Access, Ask, headless, missing capability, no provider, disabled feature,
  old-client and multi-window ambiguous destination cases.
- Disconnect/reconnect/replacement while awaiting permission or admission ACK;
  old approvals never target a new holder/epoch, no uncertain action replay.
- SSH preview keeps exact saved-host/project/generation and independent port
  consent; no Mac-local, hostname-match, URL-host, or other-network fallback.
- Automated race/queue/cancellation checks plus an actual human desktop session
  from the shipping path. Keep existing signed-package/manual release gates
  separate; this planning exercise does not complete them.

## Documentation and prior research

Canonical ownership: `docs/frontend.md:156-195`; current Browser API/policy:
`docs/browser-computer-use.md` and `docs/desktop.md`. Update these with the
approved behavioral change, plus `docs/tools.md`, SDK documentation, feature map
and roadmap. Some current docs still describe old default/selection behavior;
reconcile those with source instead of creating a competing requirement.

Historical contract `.ai-docs/plans/browser-tabs/contracts.md:25-39` requires
manual selection, pre-resolution before permission, exact holder identity and
no fallback. This proposal changes only how an explicit approval establishes
selection; the other safety properties remain requirements.

Prior local research `opencode-research.md` (pinned upstream v2
`fca4a8fa3be1be0a60e2ff1f859f09408dd537dd`, not freshly revalidated upstream)
describes tab operations, explicit target IDs, inventory-before-result ordering,
and no replay of uncertain actions. Those are useful patterns. Its permissive
remote-network/permission caveats are explicitly not a model to copy. The older
`docs/learnings/browser-use-integration.md` describes legacy external-browser
backends, not a reason to add another driver here.

The initial planning task changed only this document. After the user's explicit
implementation approval, the approved sequence above was implemented and tested.
Whole-window discovery remains out of scope: listings contain only the caller's
scoped tabs, not an inventory of the user's browsing.

## Implementation validation (2026-09-18 UTC)

- `go test -p 2 ./...` passes; targeted Browser daemon/tools/session tests also
  pass with `-race`.
- SDK: 371 tests pass. Web/app: 724 tests across 70 files pass. Desktop: 92 tests
  and the startup probe self-test pass. SDK build and app/Desktop TypeScript
  checks pass. `npm run check` also passes, including protocol interop and
  generated-contract drift checks and the client example build.
- `BROWSER_NATIVE_DAEMON=1 node apps/desktop/scripts/browser-native.mjs` passes
  against an isolated real daemon and Electron app. The production daemon,
  SDK, app association/admission, preload/IPC and native BrowserControl path
  verifies zero-tab availability, side-effect-free discovery, prompt denial,
  approved native creation/workspace admission, fresh title metadata via real CDP, detach and
  permission-gated reattach, and release preserving the human page. The same
  run retains the existing native control, transfer, preview and close checks.
- The real daemon operation-ID regression is exercised at the native boundary;
  operation IDs are bounded independently of tab/attachment IDs.

These are source-tree and isolated-native acceptance results, not notarized or
packaged-release acceptance. No installed app or active daemon was restarted;
no MCP configuration, external browser fallback, commit or push was performed.
Unrelated in-progress UI edits were preserved.

### Live-report follow-up

The initial native harness used the tool-host API, bypassing recursive agent
operation dispatch. A subsequent live report exposed a missing `list_tabs` case
in that dispatcher and nil arguments becoming JSON `null` rather than `{}`.
Both are fixed. The native harness now uses the SDK scripted-model fixture to
execute real Starlark agent turns for discovery, denial, create, run, detach and
reattach; the isolated native run passes. Separate recursive dispatcher
regressions pass for both Starlark and QuickJS, including no-argument listing;
the targeted daemon Browser suite also passes with `-race`.

The reported page did exist as a native workspace tab. Selecting it in the live
app showed the rendered page. Its title was obscured in the crowded pane because
nonshrinking host metadata consumed the label width. Workspace tab labels now
allow metadata to shrink and give the title flexible space. The regression failed
before this fix, then passed with the full workspace-tab Chromium/Firefox suite
and accessibility checks across all 66 themes. App/Desktop TypeScript checks
also pass. These source fixes have not been installed into or restarted in the
user's active app/daemon; packaged-release acceptance remains separate.
