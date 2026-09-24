# Whipcode browser tabs, agent attachment, and SSH previews

Branch: `desktop-browser-tabs`, worktree `../whip-browser-tabs`. Inherited local changes and this plan were committed as baseline `e261d9b5139d85ade9837eedccb8e9bdd7aaa5f1` (parent `f02c6cf920a237574ae03df6a221a376e16a98a8`).

Status: **Implemented behind the packaged opt-in flag; integration and Phase 7 acceptance are in progress, not release-complete.** The user authorized end-to-end implementation, all phases/testing, and isolated testing with the connected remote host. Track actual progress, commands, evidence and blockers in [the implementation ledger](implementation.md). Unchecked gates are not complete.

This revision incorporates the subsequent product discussion and **supersedes the initial human-only browser-tab proposal**. Agent attachment and SSH remote previews are part of the planned feature. URL/Tailscale host previews are explicitly out of scope. [OpenCode research](opencode-research.md) remains the pinned prior-art record; it is not evidence that Whipcode has implemented any of this.

## 1. Goal and release scope

A person and an explicitly authorized Whip agent can use the **same real Chromium page** in a Whipcode workspace tab. The page can browse normally on the viewing Mac or preview a development server through an approved SSH connection. The model uses Whip's existing browser helpers, permissions, capabilities, and operation lifecycle—not a new automation stack.

The complete workflow is:

1. Open Browser beside Chat/Terminal, manually or through a model request.
2. Approve an exact tab attachment and, when applicable, an SSH preview destination.
3. Inspect, interact, and verify with the existing browser helper language while the person sees the same page.
4. Expand the preview's approved ports explicitly when a frontend needs a separate API/HMR port.
5. Pause/revoke/detach agent control without destroying the person's page. Close a tab deliberately; restore its address without reviving old control authority.

### In scope

- First-class desktop Browser tabs in the existing split workspace: native navigation, forms, keyboard behavior, find/zoom, page DevTools, error/recovery states, profile/privacy controls, and bounded restoration.
- Small `browser` module lifecycle API around the existing `browser.run(code=...)` helpers, for both Starlark and QuickJS.
- Existing durable permission requests and UI extended with structured browser/preview targets, resource-scoped grants, and native enforcement.
- Reuse of the existing browser backend/Rod/CDP implementation; no unrestricted Electron debug endpoint.
- Local desktop pages and **SSH-only** remote previews. The browser renders locally; approved development-service traffic crosses SSH.
- Local and SSH-hosted agents reaching the selected desktop browser through the existing daemon/client connection with a narrow, versioned browser-control protocol.
- Whip-owned MCP exposure uses the same handlers and authority. Compatibility with a third-party Playwright MCP is a separate gated adapter, not assumed by the presence of CDP.
- Actual packaged Electron, SSH, concurrency, security, accessibility, and recovery acceptance.

### Out of scope

- URL/Tailscale-connected execution-host previews or a generic daemon TCP tunnel service.
- General VPN/SOCKS access to a remote host's LAN, automatic port scanning, arbitrary model-specified SSH hosts, starting dev servers automatically, or public preview sharing.
- Agent access to Whipcode's privileged application renderer, unselected tabs, Chrome's personal profile, SSH credentials, or a browser-wide debug port.
- Automatic computer-use fallback, changes to OS automation permissions, or agents operating Whip permission dialogs.
- Browser extensions, password-manager/sync/import, a new plugin system, mobile/web iframe emulation, full remote desktop streaming, arbitrary device emulation, and comprehensive console/network/profiling products.
- Screenshot-to-chat composer UX is a follow-up. Browser-tool screenshots are core and must work in the first agent milestone.

### Release definition

Phase 2 is a human-browser dogfood milestone; Phase 4 adds local agent control; Phase 6 completes the agreed local + SSH + permission + tool workflow. **Do not call the full feature shipped until Phase 7 acceptance passes.**

| Phase | Outcome | Depends on |
| --- | --- | --- |
| 0 | Prove native embedding, scoped automation and SSH routing; freeze contracts | Scope sign-off |
| 1 | Secure main-owned browser and typed desktop capability | 0 |
| 2 | Complete human Browser tabs in the existing workspace | 1 |
| 3 | Existing permissions gain resource scopes and a selected desktop provider | 0; integrate with 1 |
| 4 | Existing browser tools control the approved visible page | 1–3 |
| 5 | Isolated SSH preview environments and explicit port expansion | 0, 1, 3; 4 for agent flow |
| 6 | Integrated recovery, MCP reuse and product finish | 2, 4, 5 |
| 7 | Packaged acceptance, documentation and staged rollout | 6 |

Detailed checklists, owning files and exit gates are in **§8**; the acceptance matrix is in **§9**.

## 2. Existing seams and prior art

The current architectural authority is `docs/frontend.md`, with `docs/concurrency.md` and `docs/features.md` for daemon ownership and shipped behavior. The canonical docs must be updated with implementation, not rewritten now to describe proposed behavior as shipped.

| Reuse point | Inspected source | Implication |
| --- | --- | --- |
| Typed tabs, 4-pane / 32-tab workspace, move/split and v3 restoration | `packages/app/src/session-tabs.ts:5–86,157–205,558–568` | Add a Browser descriptor, not a second workspace manager. |
| Selected content and root-view leases | `packages/app/src/session-tab-strip.tsx:259–275`; `packages/app/src/workspace-views.ts` | Human browsing requires no daemon root lease or terminal. Agent attachment is a separate association. |
| Measured pane slots and stable mounted siblings | `packages/ui/src/workspace-layout.tsx:72–97,165–178` | Use the existing geometry owner and stable tab IDs. Native bounds/visibility need an explicit adapter. |
| Platform/preload/native boundary | `packages/app/src/{platform,desktop-bridge}.ts`; `apps/web/src/platform/desktop.ts`; `apps/desktop/src/{main,preload}.ts` | Keep Electron imports in desktop; extend a typed optional capability and preserve exact sender/main-frame validation. |
| Native guest engine | `apps/desktop/package.json` (Electron 44.2.0); installed Electron `WebContentsView` / `Debugger` APIs | No new browser engine dependency. Runtime compatibility still needs a spike. |
| Existing browser helpers and backend | `internal/tools/browser.go:22–45,58–101`; `internal/tools/browser_lang.go`; `internal/browser/browser.go:69–128` | Reuse `goto`, `fill`, `press`, `ax`, `js`, screenshot and other existing operations. |
| Single-tab CDP adapter precedent | `internal/browser/ext.go:52–70`; `internal/browser/extrelay/relay.go:1–15,338–365` | A target-restricted CDP shim can reuse Rod; a general Electron debug listener is unnecessary and unsafe. |
| Existing model dispatch/capabilities | `internal/rlm/modules.go`; `internal/daemon/recursive_runtime.go` browser/permissions cases; `internal/agentdef/operations.go` | Today browser exposes only `run`. Add lifecycle methods to the registry, both engines, capability mapping and docs together. |
| Durable permissions and revalidation | `internal/capability/{dispatcher,rule}.go`; `internal/session/{capability,permission,permission_rules}.go`; `internal/tools/permission.go` | Exact operation admission and approval already exist. Add structured resource scopes, not a competing permission database/UI. |
| Important permission gap | `internal/tools/tools.go:517–524`; `internal/capability/rule.go:89–115` | Current `browser_exec`/`computer_exec` do not set generic `permission: true`; existing rules cover other operation kinds. Tab/host/port authorization is new work. |
| Existing request UI and SDK | `packages/app/src/requests.tsx:68–112`; `packages/sdk/src/services.ts` Permissions | Extend the same request card/decision flow and uncertain-outcome handling. |
| Authenticated SSH master and forwarding | `apps/desktop/src/ssh.ts:150–164,213–230` | Reuse the selected verified connection. Its current forward is for the daemon socket, not an existing preview feature. |
| MCP export | `cmd/whip/mcp.go:145–220` (`mcpServe`, `daemonMCPTools`) | Existing daemon-backed export is the reuse seam, but it currently creates a separate tool-host root and sets `deny_permissions: true`. Interactive desktop pairing/approval is not already wired. |

Sources refer to the inspected working tree and can move during implementation. Reconfirm surrounding code before editing; unrelated work is already present in this branch.

### OpenCode: adopt mechanisms, not all policies

The real implementation is on upstream `anomalyco/opencode` **v2**, pinned to `fca4a8fa3be1be0a60e2ff1f859f09408dd537dd` (2026-09-17 03:50:28 UTC). It is absent from the local dev checkout and its experimental setting defaults false. See [research §§1–8](opencode-research.md) for exact source spans and caveats.

Adopt: main-owned `WebContentsView` pages, explicit tab IDs, separate inventory/page instantiation, lazy restoration, zoom-aware bounds, native visibility handling around DOM overlays, document generations, state acknowledgment before returning new IDs, and no automatic mutation replay.

Deliberate differences:

- OpenCode's browser is session-attached; our **human tab lifetime and agent attachment lifetime are independent**.
- OpenCode's Chromium partition is private/in-memory; our ordinary browser profile can persist, with **separate persistent SSH-preview environment profiles**.
- OpenCode proxies page traffic through the server with documented broad network reach. Our SSH preview routing is destination-scoped and fails closed; there is no arbitrary remote LAN access.
- The inspected OpenCode snapshot has a source-level BrowserPane RPC registration concern, unmerged visibility/polish fixes, and native smoke tests that bypass production IPC and skip without a display. Our acceptance must cross the actual desktop bridge and compositor.

## 3. Product and model-facing contract

### Human experience

Use the existing contoured workspace tabs and one compact toolbar: Back, Forward, Reload/Stop, editable address, and overflow actions. No nested browser tab strip or alternate visual theme. Preserve native page behavior, current StyleX/theme tokens, keyboard focus, reduced motion, and narrow-pane usability.

Show **This Mac** for normal browsing and **SSH preview · dev-box** for a preview environment. Agent-controlled tabs also show the controlling agent/conversation and a direct Pause/Detach affordance. Network location and controller are different facts; neither is inferred from the focused conversation or pane.

A model-created tab is admitted to the originating pane/window if still valid; a late result must not steal focus from a different task. Permission UI should identify whether a new tab will be opened in the background. Human-created tabs never require a model permission merely to exist.

### Proposed API, not currently implemented

Keep the model-facing surface small. Exact schemas are frozen in Phase 0/3 before generated bindings are changed.

| Operation | Contract |
| --- | --- |
| `browser.open(url, preview_host_id?)` | Request creation **and attachment** of a desktop tab; optional SSH preview resource is included in the same permission request. No host argument means normal local-device browsing, not “whatever host is focused.” |
| `browser.attach(tab_id)` | Request control of an existing tab explicitly offered to this root/agent. No enumeration or attachment of arbitrary private tabs. |
| `browser.run(attachment_id, code, expected_document?, timeout?)` | Run existing helpers against exactly this attachment; validate ownership, generation and authority. Attachment-mode results include bounded output and state metadata. |
| `browser.allow_preview_port(attachment_id, port)` | Request adding one loopback destination to this attachment's existing SSH environment. It cannot change hosts or destinations to LAN addresses. |
| `browser.detach(attachment_id)` | Release agent control and cancel its pending commands. Leave the human tab and its independently approved preview route intact. |

Tab/host discovery is bounded metadata from the desktop broker in session context: current authorized attachments, user-offered tab IDs and offered saved SSH host IDs. No global personal browsing inventory. If discoverability needs a callable listing, add one bounded metadata-only operation—not a second inventory store or unrestricted `tabs()` endpoint.

Keep legacy `browser.run(session=..., code=...)` and its output contract unchanged for live/dedicated/headless/extension sessions. `session` and `attachment_id` are mutually exclusive. An explicit attachment that is unavailable must **never** fall back to a new Chrome session or whichever tab is focused.

Example Starlark workflow:

```python
page = browser.open(
    url="http://localhost:5173",
    preview_host_id="host_devbox",
)
# Existing permission machinery waits, then returns the approved attachment.
observed = browser.run(
    attachment_id=page["attachment_id"],
    code="print(info()); screenshot()",
)
browser.run(
    attachment_id=page["attachment_id"],
    expected_document=observed["document_revision"],
    code='waitFor("#email", true); fill("#email", "test@example.com"); screenshot()',
)
browser.detach(attachment_id=page["attachment_id"])
```

QuickJS exposes equivalent async calls with object arguments. These are not top-level provider tools replacing `rlm_exec`; they are additions to the existing host module. Whip-owned MCP tools call the same operation handlers with the same scopes.

Successful open/attach returns a small JSON-compatible envelope: `attachment_id`, `tab_id`, committed URL/title, document revision, network location/approved ports, and supported operations. Attachment-mode `run` adds current revision and bounded output/media handles; legacy run retains its current return shape. Screenshots use existing authorized content/image handling, not giant base64 JSON in model context.

Expected outcomes are explicit: `permission_denied`, `desktop_unavailable`, `host_not_connected`, `browser_busy`, `stale_document`, `attachment_revoked`, `tab_closed`, `preview_disconnected`, `unsupported_operation`, and `outcome_unknown`. Map these to existing tool-error/result conventions; a routine browser failure must not terminate the agent turn. A failed page load is distinct from failed tab creation: return the admitted attachment with its error state instead of inviting a duplicate open.

`expected_document` prevents actions based on a different navigated document; it does not freeze the DOM. Revalidate element references, wait for conditions and verify effects. A batch can intentionally navigate and continue with an internally advanced document generation; a competing user navigation invalidates stale action assumptions.

## 4. Architecture, ownership, and trust boundaries

```text
Agent (local daemon or SSH-hosted daemon)
  browser lifecycle / existing browser.run helpers
                │
  existing capability + durable permission/operation ledger
                │
  daemon browser broker + target-restricted backend transport
                │ existing daemon/client connection
  selected desktop provider: versioned commands/results/cancellation
                │
  Electron main: browser manager + per-tab CDP + preview route enforcement
                │
  untrusted WebContentsView ── scoped preview proxy ── existing SSH master
                                                      └ remote loopback:port
```

The browser-control protocol is not a generic remote networking service. For SSH-hosted agents, the existing authenticated forwarded daemon connection carries browser commands/results back to the viewing desktop. Website traffic travels separately through approved SSH forwards. No URL/Tailscale transport is added.

| Resource/state | Single owner | Lifetime |
| --- | --- | --- |
| Tab descriptor, selected pane, last committed address/title, closed history | `SessionTabs` in app | Bounded local layout persistence |
| Native page, navigation history, document generation, debugger, bounds, listeners | Electron browser manager | Tab lifetime; hiding/unmounting is not destruction |
| Website cookies/storage | Dedicated Chromium partition | Persistent normal profile or host+project preview profile; clearable |
| Permission requests, decisions, grant scopes, operation identity/audit | Existing daemon capability/session ledger | Existing durable semantics; no parallel approval store |
| Browser provider identity and live attachment routing | Daemon broker, bound to an authenticated client connection/root | Connection epoch; one selected provider per root |
| Native realization of approved control/route grants | Electron manager | Checks current provider/attachment generation on every use |
| SSH transport | Existing native `SSHConnection` | Selected host connection lifetime |
| Preview policy and route leases | Native preview manager, reflecting approved resources | Preview tab/environment leases; revoke/disconnect invalidates routes |
| Address edit draft, menus, find query, transient UI | App React/controller | View/controller memory |

### Native embedding

- Use `WebContentsView`, not an iframe, deprecated BrowserView or renderer-owned `<webview>`. Keep `webviewTag: false` in the trusted app.
- Guests are sandboxed, context-isolated, Node-disabled, webSecurity-enabled, and have **no Whip preload or app bridge**. The privileged renderer's CSP/navigation policy stays intact.
- Guest profiles never use `defaultSession` or register the app's private asset scheme. Deny private app schemes in guest navigation, redirects, popups and resource requests. Preserve ordinary site subresources; do not indiscriminately break all data/blob resources.
- React presentation mounts/unmounts only show/hide resources. Electron owns close/destruction and beforeunload confirmation. Split/Move preserves a page; Duplicate is a new page, not another owner of the same WebContents.
- Measure the viewport below the toolbar and map CSS pixels to content-view DIPs using renderer zoom, not a blind devicePixelRatio multiplication. Main bounds-checks every update and rejects stale layout generations.
- DOM overlays cannot out-z-index native views. Extend shared UI primitives with a minimal generic overlay/drag lifecycle signal. Balanced holds hide affected native surfaces before an overlay becomes interactive and restore after all relevant overlays close. A capped, transient still image may preserve visual continuity; do not continuously capture or retain it.
- Native focus events update pane focus. Route a fixed set of configured app shortcuts through main; preserve IME/editing input and avoid double handling. Test Cmd+L/R/F/W, tab cycling, palettes, modal focus return and VoiceOver.

### Browser control and CDP

- Prefer reuse of the current Rod-backed `Browser` through a one-attachment CDP transport/shim, following the extension relay precedent. Phase 0 proves the smallest viable integration against the installed Electron version.
- Do not blindly copy extension relay code or introduce a raw debug port. Restrict target discovery/attachment to the granted guest and needed descendant frames; deny creation/selection of other app targets and browser/process-wide operations.
- Browser filesystem/download paths and profile-wide credential APIs are not safe merely because a CDP connection is attached to one WebContents. Explicitly enumerate required commands, intercept/reject escape surfaces, and test hostile commands. Page JS is permitted only inside the authorized page; it never gains application-main authority.
- Existing `tabs()` / `useTab()` in attachment mode see only authorized targets and cannot escape the attachment. New tabs/popups go through tab admission and page policy; they do not inherit agent control implicitly.
- BrowserTool screenshots capture only the granted guest. An agent on an SSH host cannot pass a remote path to a native file API on the Mac: uploads/downloads need authorized byte transfer or a clear unsupported result, never shared-filesystem assumptions.
- One controlling agent per tab initially. Serialize bounded operations per tab; separate tabs can proceed independently. Human Pause prevents new agent actions and cancels queued ones. Human navigation invalidates stale document work.
- Page DevTools may compete with debugger ownership. The spike must define pause/detach or a supported sharing behavior; show this explicitly, do not silently reconnect and replay mutations.

### Desktop provider channel

Register the willing desktop provider through the existing SDK/client connection with version/capability negotiation. Availability is not permission to use a tab. Select one provider explicitly for a root; if two windows/devices offer service, require a deterministic user-selected association rather than the model's “first client.” Browser-less clients cannot impersonate that provider through generic event payloads.

Carry command ID, root/agent, provider epoch, attachment ID/generation, tab ID, document revision, deadline/cancel and bounded arguments/results. Reuse existing authenticated connections and command/content facilities. The app/SDK only bridges validated messages; Electron main applies authority and native ownership checks. Do not add signing/enrollment merely because older historical plans used it; current Whip permissions accept decisions from connected clients.

Metadata notification is not authority: native work must correspond to an admitted operation on the bound connection and a current grant. Define readiness/inventory acknowledgment before an open/attach result becomes usable. Provider disconnect immediately blocks new work and invalidates in-flight command delivery; resource metadata can survive, executable authority cannot.

## 5. Permission and lifetime contract

### Same permission system, new structured resources

1. Model invokes the **exact** `open`, `attach` or port-expansion operation.
2. Existing capability admission checks root/agent/delegation and canonicalizes the requested tab/profile or saved SSH identity + literal loopback endpoint.
3. Existing saved policy is checked. If approval is needed, create the ordinary durable request and show it in the existing request UI.
4. Approval revalidates the operation digest, target identity/generation, provider availability and current policy. No tab, tunnel or destination request is created before authorization.
5. Realize the approved resources in Electron, acknowledge the inventory, then return the attachment. Partial failure cleans up unowned resources and reports exactly what remains.

Create, control and route are separate scopes even when one combined request approves them. The card must show agent/conversation, desktop, new vs existing tab, profile/login implications, actual SSH host, exact endpoint/ports, and each lifetime. A saved hostname string is not a verified host identity. Reject retargeted profiles or changed host identities after approval.

Extend the existing operation/rule descriptors and capability scope enforcement, including native checks; do not reduce a request to “allow browser_exec” or “allow all SSH.” Reuse `permission.decide`, decision uncertainty/recovery, durable audit and the existing request queue. A model cannot approve its own request by calling another model-facing module.

### Mode, saved rules and delegation

- Prompt/automatic modes use the current policy machinery. Automatic mode may skip an eligible prompt within the desktop's user-offered resource ceiling; it cannot create a missing desktop association, choose an unoffered personal tab, import credentials, select arbitrary SSH hosts or bypass hard destination restrictions.
- First release supports an exact operation approval and bounded grants it establishes. Tree-scoped remembered rules may cover the same canonical tab/environment/ports, not “every localhost” or wildcard hosts. Do not offer an Always option until its exact matching/revocation behavior is implemented and tested.
- Existing `browser` capability permits asking/using the module; a live attachment is an additional resource grant. `mcp` tool consent likewise does not imply tab/host authority.
- Child agents do not gain control by copying an attachment ID. Phase 3 defines reuse of existing delegation/narrowing to issue a child-bound attachment, and tests ancestor revocation. No implicit multi-controller sharing.
- Direct human UI actions use the trusted native path, not model-supplied flags claiming `local-human` status. User “Attach to conversation” realizes the same grant and result association as an approved model request.

### Separate lifetimes (do not collapse them)

| Event | Agent control | Human page / preview route |
| --- | --- | --- |
| View hidden, moved, Settings opened | Retained unless separately paused; native surface hidden as needed | Page and approved route retained |
| Agent detaches or is stopped | Revoke its attachment, cancel queued commands; never replay started effects | Page remains; a tab-lifetime preview route stays available to the person |
| User pauses control | No new agent work; cancel queued work, report uncertain started effects | Human browsing remains usable |
| User revokes a preview destination | Invalidate affected route scope and pending actions requiring it | Close its active sockets; show blocked/disconnected state, no local fallback |
| Tab closes | Invalidate attachments/generation | Confirm unsaved work/downloads; destroy guest and release its route leases |
| Provider/SSH disconnect | Reject new work, interrupt uncertain in-flight actions | Hide orphan/recovery surfaces as needed; remote requests fail closed; human tab metadata survives |
| Desktop/daemon restarts | Old attachment IDs/grants are not live authority | Restore descriptors lazily; explicit reattachment/revalidation is required |

The combined permission UI must state these different lifetimes. A one-time approval can create a continuing scoped resource; it is not a request to prompt before every click or packet.

Admission, cancellation and idempotency follow the current durable operation model. Distinguish waiting for consent from page-load/execution timeouts. Reconnect/status lookup retrieves a known operation outcome; a new invocation is not an idempotent retry. If a click/POST may have occurred, return `outcome_unknown`, inspect, and never automatically replay it. Restore never resubmits POSTs or resurrects a revoked controller.

## 6. SSH preview networking

### Routing policy

Use a **private authenticated local preview proxy** installed on an isolated Chromium session. It preserves the logical URL, Host/Origin behavior and browser TLS validation while directing approved development services through the existing SSH master. Do not parse/rewrite HTML, weaken CORS/CSP, or disable TLS validation.

- Canonicalize approved loopback aliases/IPv4/IPv6 and port to explicit remote loopback targets. No arbitrary hostnames, DNS rebinding route, LAN targets, cloud metadata endpoints or unrestricted SOCKS egress.
- In a preview environment, approved loopback ports mean **that SSH host**, unapproved loopback destinations are blocked, and they never mean the Mac by fallback. Handle Chromium's implicit loopback proxy bypass explicitly.
- Ordinary public-web requests use the Mac's normal network path; enforce the local/private-network boundary for redirects, subframes, fetch, WebSockets, service workers and other browser paths too. Do not let direct-network/WebRTC/QUIC bypasses open unapproved local/private destinations. Phase 0 proves the supported transport policy; unsupported paths fail visibly rather than claiming universal browser networking.
- Per-destination enforcement is at the proxy/tunnel boundary for every connection, not just `goto()` or the model tool call. Remote mappings use pinned literal loopback addresses; public-name routing must resist DNS rebinding to local/private addresses.
- HTTP, HTTPS CONNECT, WebSockets/HMR, SSE and streaming bodies must work with bounded buffering/backpressure. End-to-end TLS is unchanged: local dev certificates may need explicit trust setup; never bypass certificate errors globally.
- Reuse the **already authenticated, currently connected saved SSH profile**. Preserve host-key verification, no agent forwarding, and follow-up operations pinned to that master. If disconnected, offer native Connect; the model does not obtain SSH credentials or establish an arbitrary new host connection.
- Use private per-route forwarding endpoints where practical, or loopback-only listeners with strict ownership. The browser-facing proxy requires a per-provider secret supplied only by native main, never to the model, guest scripts, logs or unrelated proxy-auth challenges.
- Route failure does not fall back to a new SSH connection, another saved host or a direct Mac connection. On verified reconnection, recreate only authorized routes with a fresh transport generation; uncertain browser mutations remain interrupted.

A plain local-port forward is an acceptable **spike fixture**, not the final integrated routing contract: it changes the visible origin, breaks some redirects/multi-port apps and is not cookie isolation. An external-browser forward can be a separate opt-in future feature.

### Profile/environment identity

`session.setProxy` is session-wide, not per-tab. Keep a normal persistent Whipcode Browser partition and separate persistent preview partitions keyed to a native-owned **verified host identity + stable project/environment ID**. Do not mutate the shared normal profile's routing or derive authority from model-provided partition names.

Two machines serving `http://localhost:3000` must not share cookies, cache, service workers or storage. Different ports alone do not isolate cookies. Tabs deliberately sharing an environment share its routing scope/profile; the approval UI makes that scope explicit. A port expansion therefore authorizes the environment, not only one incidental request. Every active agent attachment is still checked against its own approved scope; reject incompatible grants or allocate an isolated environment rather than broadening another controller silently.

Freeze proxy configuration for an environment where possible; grant changes update the authenticated proxy's route policy, not a global mutable Electron proxy setting. Revocation closes existing matching connections as well as blocking new ones. Track service workers and background page requests so closing the last tab or revoking a route actually removes network reach.

Remote loopback means the SSH server's network namespace. Docker/container-only services need a published host port or a later explicit container route; never scan networks to guess one.

## 7. Bounds, persistence, and safety floors

Initial bounds are proposals to verify in Phase 0 and encode centrally, not scattered literals:

| Resource | Initial ceiling / behavior |
| --- | --- |
| Workspace | Existing 4 panes, 32 total tabs, 20 closed entries, 64 KiB layout metadata |
| Browser tabs / native guests | 8 browser descriptors within the total cap; lazy creation; at most 4 presented. Refuse overflow rather than silently discard forms. |
| Active controller | 1 per tab; explicit handoff before another agent controls it |
| Per-tab commands | 1 active + 4 queued; queue rejection/cancellation is visible; separate tabs run independently |
| URL / title | 8 KiB URL and existing 128-codepoint title; aggregate metadata limit still applies |
| Attachments / offered inventory | Bounded by open tabs and selected provider; no unbounded historical page inventory |
| Preview environments / ports | 4 active environments per window; 4 approved ports per environment initially; visible limit errors |
| Preview streams | 32 active sockets per window; 64 KiB chunks; 4 MiB aggregate queued bytes; backpressure before allocation growth |
| Control metadata | 64 KiB command arguments by default; larger bounded results/media use existing content/blob transfer, not event payloads |
| Screenshots | 8 MiB encoded image cap, dimension limit, authorized content storage; no recurring screenshot polling |
| Diagnostics | Bounded operation/status metadata; no raw full URL/query tokens, page contents, cookies, tunnel secrets or CDP transcript logs |

Use existing root actor/operation ordering; do not hold actor/registry locks across native calls, approval waits or tunnel I/O. Every worker/stream has an owner, cancellation path, bounded queue and shutdown join. Review Go concurrency/context skills when implementing and run the race detector. Native cleanup must not terminate the daemon, dev-server process or unrelated terminal.

Persist descriptors and last committed addresses/title/network-environment references via a tested v4 workspace migration, preserving v1/v2/v3 recovery and a non-destructive downgrade path. Do not persist live native IDs, control handles, debugger sessions, form contents, DOM or complete navigation stacks. Restored browser tabs without platform capability remain visible as unavailable, not silently dropped or replaced with iframes.

Saved URLs and website profiles are sensitive local browsing data. Explain that addresses may contain query/fragment tokens; do not pretend userinfo rejection removes all secrets. Provide Clear site data, Forget saved browser addresses/closed tabs and preview-profile removal without deleting app credentials or host configuration. Profile disk growth is not bounded by the 64 KiB layout limit; expose clearing and handle disk-full/quota errors. Do not silently evict stored logins.

Security floor for all pages: no Whip preload/Node, no app schemes/assets, strict IPC sender/frame/window checks, denied unsolicited device/screen/notification/clipboard permissions, no automatic external application launches, no uncontrolled popups, and normal TLS/CORS enforcement. Main owns safe downloads and beforeunload; page dialog spam cannot trap the entire app. Normal file-input selection remains a deliberate human action. Favicons are bounded inert images resolved through guest/native policy, not arbitrary privileged-renderer fetches or injected SVG/HTML.

Downloads need bounded concurrency, user-selected destinations, cancellation and no auto-open; keep at most two active initially and finalize a streamed per-file ceiling in Phase 0. Closing an active download asks whether to keep the tab or cancel it; no invisible background download manager. Agent uploads/downloads must separately respect both host filesystem permissions and browser attachment authority.

## 8. Phased implementation

All tasks below are unchecked. File names for new implementation/tests are suggested seams, not a mandate to create every file. Reuse current helpers first; no new Go/JS dependency is expected unless a spike demonstrates a concrete gap.

### Phase 0 — Freeze contracts and prove the risky boundaries

**Depends on:** this scope. **Deliverable:** focused disposable/native fixtures and recorded decisions, not production feature flags enabled by default.

- [ ] Freeze the model operation/result schemas, identity/scope fields, tab/route/controller lifetimes, saved-rule/automatic-mode behavior, and single-controller policy.
- [ ] Prove 2–4 WebContentsViews under the current Electron build: real preload IPC, app overlays, drag, geometry/zoom, focus/IME, DevTools contention, beforeunload, crash/cleanup and permission denial.
- [ ] Connect the existing Rod/browser helpers to one guest through a restricted transport. Exercise navigation, screenshots, AX/DOM, input, subframes and uploads/download escape attempts. Decide the smallest CDP shim/backend adapter; no general debug listener.
- [ ] Prove a private session proxy over an existing SSH connection with a local test SSH server/remote fixture: HMR, frontend+API ports, CONNECT, redirects, DNS/private-IP rejection, IPv6 loopback, proxy bypass and no direct fallback.
- [ ] Verify that session/profile isolation really separates identical localhost origins on two hosts; verify service-worker and background network behavior under revocation.
- [ ] Choose the minimum desktop-provider registration/command/result transport over existing protocol. Demonstrate a command initiated on an SSH daemon reaching only the selected desktop guest.
- [ ] Record API/permission decisions, provisional bounds, hardware measurements, unsupported cases and proof gaps here. No external production credentials in fixtures.

**Exit gate:** all three seams—native UI, restricted automation, and scoped SSH networking—have runnable evidence. If safe preview routing or overlay composition is not viable as proposed, revise the design before broader implementation. Do not substitute “works in a headless mock.”

### Phase 1 — Secure native browser owner and typed platform capability

**Depends on:** Phase 0 native/CDP decisions. **Primary files:** `apps/desktop/src/{main,preload}.ts`, new focused browser-manager/policy module, `packages/app/src/{platform,desktop-bridge}.ts`, `apps/web/src/platform/{desktop,browser}.ts`, desktop tests.

- [ ] Implement manager-owned WebContentsViews, isolated normal/preview session creation, safe URL admission, native session policy and deterministic cleanup.
- [ ] Add optional capability/version negotiation and typed create/present/navigate/focus/find/zoom/request-close/events. Do not expose generic Electron objects, IPC dispatch or arbitrary main-process execution.
- [ ] Implement ordered state snapshots, document/layout/native generations, bounds validation and fail-closed hide/reconcile on renderer failure.
- [ ] Implement permission/popup/external-protocol/download foundations and guest-only DevTools behavior from the spike.
- [ ] Test forged IPC sender/frame, unknown/stale tab IDs, unsafe URLs/redirects/subresources, rapid create/close, StrictMode replay and listener/resource cleanup through the real bridge.

**Exit gate:** untrusted pages cannot reach the app bridge or profile, and every native resource has one owner and teardown path.

### Phase 2 — First-class workspace tabs and complete human browsing

**Depends on:** Phase 1. **Primary files:** `packages/app/src/{session-tabs,session-tab-strip,shell,empty-workspace,workspace-views}.*`, new Browser view/controller/styles and `/browser/$viewId` route, `packages/ui/src/{workspace-layout,overlays}.*`, app/UI tests.

- [ ] Add Browser descriptors, command/pane add actions, labels/icons, route selection, close/reopen, move/split/duplicate, caps and non-destructive v4 migration. Regenerate route output.
- [ ] Keep browser descriptors independent of daemon root/host leases; support browsing when all daemons are unavailable. Audit all tab-kind switches and runtimeId/rootId assumptions.
- [ ] Implement separate address draft/pending/committed state; normalize local URLs and domains; no silent search provider or HTTPS downgrade.
- [ ] Finish navigation/loading/stop/errors, find/zoom, page context menus, Copy URL/Open externally, beforeunload, unavailable platform/bridge, narrow panes and profile/address-clearing UX.
- [ ] Integrate the minimal shared overlay/drag visibility protocol, guest focus-to-pane updates and configured shell shortcuts. Include settings/back, compact mode, window hide/minimize and display scaling.
- [ ] Lazy restore selected pages only; no hidden popup/POST replay or form-state promise. Preserve form/scroll/history on ordinary moves and tab switches.

**Exit gate:** Browser works like a real workspace tab in a packaged app, including menus and keyboard input. This can be dogfooded as human browsing; it is not completion of the agreed feature.

### Phase 3 — Resource-scoped permissions and desktop-provider broker

**Depends on:** Phase 0 contract; integrate with Phase 1 identities. Can overlap Phase 2 using a fake native provider. **Primary files:** `internal/capability/{dispatcher,rule}.go`, `internal/session/{capability,permission,permission_rules}.go`, `internal/tools/permission.go`, `internal/daemon` browser broker/handlers, `internal/protocol`, `packages/{protocol,sdk}`, `packages/app/src/requests.tsx` and runtime/provider binding.

- [ ] Add canonical browser-create/control and SSH-preview resource scopes using the existing capability/permission ledger. Add schema/migrations only for state not already representable; no parallel approval store.
- [ ] Extend operation descriptions and existing request cards with exact desktop/tab/profile/host/port/lifetime details. Preserve decision uncertainty, saved-rule matching and multiple-client approval behavior.
- [ ] Define prompt/automatic behavior within desktop-offered ceilings; test no authority from a model-provided principal/host/partition string. A module/MCP grant alone is insufficient.
- [ ] Implement versioned selected-provider registration, bounded inventory, command/result/cancel correlation, content transfer and readiness acknowledgment using current client/daemon paths. Handle unsupported old daemon/desktop combinations explicitly.
- [ ] Bind approvals to root/agent, provider epoch, resource identity/generation and operation digest. Check again immediately before native dispatch; invalidate pending work on policy changes, target changes and disconnects.
- [ ] Define/test exact grant delegation and ancestor revocation; no implicit shared controller or cross-root access. Existing human “Attach to conversation” and model requests converge on the same grant realization.
- [ ] Test denial-before-effect, approval races, cancellation while waiting, duplicate/status retrieval, daemon restart, unavailable provider, two desktop candidates and forged/late provider replies.

**Exit gate:** fake-provider end-to-end tests prove the ordinary permissions system admits exactly the intended resource and rejects stale/broader actions. Waiting for consent does not spend page-execution time or create native resources.

### Phase 4 — Existing browser tools drive an approved embedded page

**Depends on:** Phases 1–3. **Primary files:** `internal/browser/{browser,session}.go` and a focused desktop transport/backend; `internal/tools/{browser,browser_lang,tools}.go`; `internal/rlm/{modules,guide_fragments}.go` and engine bindings/tests; `internal/daemon/recursive_runtime.go`; `internal/agentdef/operations.go`; native CDP bridge.

- [ ] Implement `open`, `attach`, attachment-targeted `run`, and `detach` against the broker. Keep existing browser sessions/modes backward-compatible; enforce mutual exclusion of legacy session and attachment targets.
- [ ] Reuse Rod/backend helpers through the restricted transport proved in Phase 0. Limit target discovery/selection, reject browser/app/filesystem escapes, and surface unsupported helpers truthfully.
- [ ] Register lifecycle operations in module and capability maps for both engines and descendant definitions; update generated guides/golden tests, not just one host switch.
- [ ] Return bounded structured attachment-mode results and authorized screenshot/content handles; preserve legacy run results. A remote agent's screenshots/bytes travel over the broker, not presumed shared disk paths.
- [ ] Enforce per-tab command serialization, one controller, per-statement/per-native-command revocation checks, document generations, queue limits and cancellation. Never replay a mutation to repair delivery uncertainty.
- [ ] Wire agent indicator/Pause/Detach and user-offered existing tabs. Pausing cannot undo a started side effect; report that distinction.
- [ ] Implement scoped file transfer or explicit unsupported behavior for attachment-mode uploads/downloads before shipping those helper claims. No raw remote path is interpreted as a Mac path.

**Exit gate:** a fake-provider conversation opens a tab, waits for a real permission decision, inspects/fills/screenshots the same visible page and detaches without closing it. Pass both Starlark and QuickJS and hostile cross-agent/target tests. Existing browser-mode regression suites remain green.

### Phase 5 — SSH preview environments and permission expansion

**Depends on:** Phases 0, 1 and 3; model end-to-end also needs Phase 4. **Primary files:** `apps/desktop/src/ssh.ts`, focused preview proxy/route manager, browser manager/session policy, existing platform/bridge, broker/permission scopes, Browser UI and SSH/native tests.

- [ ] Add explicit destination forwarding on the selected authenticated master without inheriting unrelated user forwards or falling back to a new connection. Maintain cancellable bounded route leases separate from the daemon socket lease.
- [ ] Implement the authenticated local session proxy and per-environment route table, loopback normalization, no bypass/direct fallback, public-vs-private destination checks and unsupported network-protocol policy.
- [ ] Create native-owned persistent host+project environment partitions. Do not change normal browser routing or let identical localhost origins on different hosts share storage.
- [ ] Support HTTP(S), WebSocket/HMR, SSE, streaming bodies, TLS errors, server stop/restart, multiple approved ports and explicit namespace limitations.
- [ ] Implement `preview_host_id` in open and `allow_preview_port`; apply saved policy and one combined permission request where appropriate. Output-discovered URLs are inert suggestions until the route is authorized.
- [ ] Test environment-wide port expansion against every attachment's scope; reject accidental privilege widening. On revoke, close live matching sockets and block service-worker/background connections too.
- [ ] Wire provenance badges, Connect/Retry/blocked-port states, disconnect generation changes and no-local-fallback errors. Do not tunnel all traffic just because a tab is associated with an SSH agent.

**Exit gate:** an agent on the SSH host can open/control its preview on the Mac; a two-port app gets an explicit expansion; two hosts on the same localhost port stay isolated; revoked/unapproved destinations cannot be reached by JS, redirects, WebSockets or stale sockets. No URL/Tailscale path exists.

### Phase 6 — Integrated lifecycle, MCP reuse, and product finish

**Depends on:** Phases 2, 4 and 5. **Primary files:** integration across existing browser/permission/UI/native owners; `cmd/whip/mcp.go` and daemon tool export; protocol/SDK tests and deterministic fixture app.

- [ ] Exercise model-driven local and SSH workflows through actual production preload + SDK/protocol paths, not direct calls into a manager fixture.
- [ ] Expose Whip-owned MCP operations through existing daemon-backed handlers, with identical resource gates, bounded output and cancellation. Current `whip mcp serve` uses a separate tool-host root with `deny_permissions: true`: require explicit desktop pairing and a valid resource grant, or fail closed with an actionable unavailable/permission-required result. Do not globally disable that protection or promise prompts on an unpaired noninteractive server. Test tool consent plus resource consent; a native/trusted MCP entry does not bypass browser scopes.
- [ ] If desired, run a separate third-party Playwright-MCP compatibility spike against the restricted transport. Record unsupported Target/Browser operations; do not widen authority to make it pass. This adapter is not a release dependency unless separately approved.
- [ ] Complete Pause/Detach/handoff, multiple clients, child delegation, profile clearing, root stop, renderer crash, SSH sleep/wake, daemon/app restart, late permission decisions and pending-operation recovery.
- [ ] Complete human browsing edges from Phase 2: native dialogs/popups/auth fallbacks, download/unsaved-work close, link-opening defaults, guest-only DevTools, all shared overlay owners, VoiceOver/IME and narrow layouts.
- [ ] Confirm computer-use remains separate; no automatic fallback or ability for the browser tool to interact with Whip's permission dialogs. Do not widen the computer app policy as part of this feature.
- [ ] Apply least-code/security review: eliminate duplicate state/RPC layers, unbounded retained objects, raw CDP logs and any bypass around the common permission gate.

**Exit gate:** the whole agreed workflow functions end-to-end with no second automation or permissions stack, and failures preserve the person's work and report uncertainty honestly.

### Phase 7 — Release acceptance, documentation, and staged rollout

**Depends on:** Phase 6. **Deliverable:** reproducible test/evidence record for the actual packaged artifact; feature flag stays off outside explicit dogfood until gates pass.

- [ ] Run the full matrix below, including targeted Go race tests and the complete required check suite; record exact artifact, hardware, versions, command output and remaining manual gaps.
- [ ] Measure 0/1/4/8-page memory, idle CPU, command/viewport latency, route buffering and 100 open/close cycles. Set hardware-specific budgets from Phase 0; reject linear leaks and hidden daemon polling.
- [ ] Dogfood with real local/SSH dev servers and authenticated apps using disposable accounts, not production credentials in tests. Validate minimum supported macOS and packaged security settings.
- [ ] Verify feature-disabled/old-client behavior, v4 migration/downgrade, capability negotiation and a rollback that stops authority without erasing saved pages/profile data.
- [ ] Update current architecture/feature/tool/security docs with implementation and evidence; mark the roadmap milestone complete only when its gates pass.
- [ ] Replace this plan's unchecked tasks with verified evidence/deviations as work lands. Do not treat a passed mock/headless run as packaged native acceptance.

**Exit gate:** all core acceptance passes, remaining limitations are explicit and approved, and the flag can be broadened safely.

### Dependency / parallel work map

```text
Phase 0 ── Phase 1 ── Phase 2 ───────────────────────────┐
   └─────────────── Phase 3 (fake-provider lane) ──┐      │
                         Phase 1 + 3 ── Phase 4 ─┼─ Phase 6 ─ Phase 7
                     Phase 0 + 1 + 3 ── Phase 5 ─┘      │
                                         Phase 2 ──────┘
```

After Phase 0 freezes shared contracts, UI/workspace and daemon/permission work can proceed in parallel. SSH routing can proceed against a fake broker, but it cannot bypass Phase 3 admission for integration. Keep changes reviewable by owner; avoid simultaneous edits to central protocol/generated outputs without coordination. Do not estimate a release date before the three native/automation/network spikes resolve.

## 9. Acceptance matrix and validation commands

| Area | Required evidence |
| --- | --- |
| Workspace/native UI | Chat + Terminal + 2 browsers; move/resize preserve page state; accurate bounds at renderer zoom and display scales; every shared modal/menu/tooltip/drag preview above pages; correct native focus/shortcuts/IME/VoiceOver |
| Permission boundary | Deny creates no effect; approve exact request; stale identity/policy fails; saved/automatic rules stay within ceilings; no self-approval; no permission timeout confused with execution timeout |
| Agent control | Same visible page, stable attachment despite focus changes; cross-root/child copied ID rejected; explicit delegation; ancestor revoke; one controller; stale document/DOM references; bounded batch/output; no replay after uncertain click |
| Guest isolation | No Node/preload/Whip IPC/app assets; hostile subframes/redirects/popups; target/browser-level CDP escape denied; page DevTools interaction tested; no arbitrary native path access or profile-wide credential dump |
| SSH network | HTTP/HTTPS/HMR/SSE, IPv4/IPv6 loopback, multi-port request, Host/Origin/redirect handling, container limitation, proxy auth, TLS errors, no LAN/private/DNS-rebinding escape, no loopback/UDP bypass, no direct fallback |
| Environment grants | Two hosts same origin isolated; tabs deliberately sharing environment understood; expansion cannot broaden other controllers; revocation closes active sockets and worker traffic |
| Lifecycle | Hidden vs closed distinction; detach retains human page/route; root stop, renderer crash, provider/SSH loss, restart/migration, reconnect generations, in-flight result uncertainty, rollback |
| Files/media | Page-only screenshots across SSH into authorized handles; bounded output; denied/approved uploads with correct source host; downloads no auto-open, cancel/partial cleanup; no shared-filesystem assumption |
| Compatibility | Both RLM engines, definitions/descendant narrowing, legacy browser modes, old daemon/bridge, missing desktop, MCP same gates, web/mobile unavailable state |
| Resource limits | Cap and overflow tests, queue cancellation, slow consumers, stream backpressure, idle CPU, no polling, 100-cycle listener/WebContents/socket/goroutine plateau |

Use a deterministic local fixture site and a controllable test SSH server: redirects, forms/beforeunload, SPA navigation, cross-origin frames, slow/failed loads, TLS failures, popup/JS dialog spam, file inputs/downloads, permission probes, malicious titles/favicon, loopback/private destinations and deliberate crashes. Native tests exercise production registration/preload/IPC, not just direct manager methods. Keep CI headless tests and real display/macOS gates separate and name skips honestly.

During implementation, run the affected existing Go/Node/Vitest/Playwright checks, including:

- `go test` for changed `internal/{browser,tools,capability,session,daemon,rlm,agentdef,protocol}` and relevant MCP/export packages; `go test -race` for changed concurrent paths and full race acceptance where required.
- `npm run generate` when protocol changes, followed by clean generated-contract verification and SDK/browser interoperability tests. Never hand-edit generated schemas/validators/routes.
- `npm run check`, `npm run check:web`, `npm run test:web`, `npm run check:desktop`, `npm run test:desktop`, production renderer/CSP/package guards, and new native/SSH fixtures.
- Final `task check`, required race suite and staged/signed Electron acceptance. Physical VoiceOver, display scaling and native SSH key/sleep behavior are explicit hardware gates.

Initial performance goals (confirm on named hardware): warm UI/tab actions p95 <100 ms; settled resize reflected within two frames; no extra transcript/daemon polling for human tabs; bounded command/stream buffering; no growth proportional to completed open/close cycles. Report remote command latency separately from network RTT and page work. Arbitrary websites do not have a universal fixed memory budget.

This planning-only revision requires document/source/link consistency checks, **not** runtime builds. No runtime acceptance result is claimed here.

## 10. Documentation and follow-ups

Update with implementation, in the owning phase:

- `docs/frontend.md`: Browser descriptor/state ownership, overlays/focus, selected provider, profile split, restoration and attachment UI.
- `docs/desktop.md`: native security, SSH preview reach, profile/privacy controls, limitations and packaged evidence.
- `docs/{tools,browser-computer-use,rlm-runtime}.md`: new lifecycle methods, attachment-mode result/error contract, both engines, consent and no-fallback semantics; keep browser vs computer use distinct.
- `docs/{protocol-v2,concurrency}.md` and package READMEs where wire/ownership/public APIs change; protocol/SDK regeneration and tests in the same change.
- `docs/features.md`: behavior → implementation → tests for browsing, attachment/permissions and SSH previews, separately from legacy browser modes.
- `docs/roadmap.md`: unchecked feature milestone during implementation; completion only with Phase 7 evidence.
- This plan and `opencode-research.md`: link actual phase evidence and record differences without turning historical upstream behavior into a requirement.

Later, separately approved increments: explicit screenshot/URL attachment to a chat draft using `CompositionStore`; console/network evidence capture; richer agent selector APIs; remote container routing; external-browser forwarding; optional third-party MCP compatibility. **URL/Tailscale previews remain excluded from this plan.**
