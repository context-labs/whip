# OpenCode browser-tab implementation research

Research date: 2026-09-17 UTC. Read-only source investigation; no OpenCode builds, tests, UI runs, checkouts, or source edits were performed.

## 1. Provenance and status — distinguish the two branches

| Source | Observed state |
|---|---|
| Local reference | `/Users/samheutmaker/Desktop/context-labs/src/rlm/reference/opencode`, branch `dev`, HEAD `e03db9bc6908f75c9334d8aa997deeaac81c0298`; commit date `2026-09-14T17:41:36-04:00`; subject `Merge branch 'dev' of github.com:anomalyco/opencode into dev`. |
| Remote configured locally | `https://github.com/sst/opencode.git` for fetch/push. |
| Local working tree | `git status --porcelain=v1` returned no entries: clean. Searches of this checkout did **not** find the embedded-browser implementation described below. The repository does contain Electron desktop code; do not conclude that OpenCode's browser is Tauri-based from older material. |
| Actual implementation researched | Public upstream `anomalyco/opencode`, branch **`v2`**, pinned to **`fca4a8fa3be1be0a60e2ff1f859f09408dd537dd`**, committed **`2026-09-17T03:50:28Z`** (`feat(tui): show explicit subagent model (#49388)`). Files retrieved from GitHub raw URLs at this immutable SHA; no local git fetch/checkout. |
| Experimental | `packages/app/src/settings/model.tsx:97,254` defines `experimentalBrowser` and defaults it to **false**. `packages/app/src/session/browser/attachments.ts:46-48` requires both the setting and `platform.browserPane`; `model.ts:22-29` further requires a session, a compatible server, and desktop mode. |

All file:line references below are **upstream v2 at the pinned SHA**, not paths present at the local dev HEAD. Source URL template:

`https://github.com/anomalyco/opencode/blob/fca4a8fa3be1be0a60e2ff1f859f09408dd537dd/<path>#L<start>-L<end>`

Root, desktop, and app AGENTS.md guidance was inspected in the local reference and pinned upstream. Ancestor directories were checked. There were no additional browser-subdirectory instruction files in the upstream tree inspected. This report is the only project file created by this research.

### Merge/release distinction and an important integration caveat

- [PR #44838](https://github.com/anomalyco/opencode/pull/44838), “add browser tabs and Chromium diagnostics,” was **merged 2026-09-07T10:58:02Z**. [PR #46531](https://github.com/anomalyco/opencode/pull/46531), the public-API browser plugin, was **merged 2026-09-07T07:16:02Z** into v2. This is real implementation, not just a proposal.
- Older [PR #39273](https://github.com/anomalyco/opencode/pull/39273), “add Electron browser adapter,” was closed **unmerged**. Do not use its description as the current design.
- **The inspected latest v2 snapshot appears incompletely wired at the production desktop IPC boundary.** `packages/desktop/src/renderer/api.ts:47-50` invokes `BrowserPane`; `shared/ipc-rpc/browser.ts:27-43` defines it; but `shared/ipc-rpc.ts:1-31` does not import/merge it into `DesktopRpcs`, and `main/ipc.ts:9-44` does not register a browser handler. `shared/ipc-rpc/app.ts:56-72` and `window.ts:27-38` also do not include it. The component/native test architecture described below does not establish that this production bridge works: native fixtures call `createBrowserPane` directly.
- Therefore: **source-present, merged, experimental; not verified end-to-end working in the current app or a released binary**. This is a source-level integration finding, not a runtime reproduction. The cause (e.g. an in-progress refactor/regression) is not established.

## 2. Architecture and ownership

```text
Solid app: session Review tabs + browser toolbar + empty native-surface rectangle
    -> platform.browserPane registration (binding UUID)
    -> renderer API / typed Electron MessagePort RPC [current wiring caveat above]
    -> Electron main createBrowserPane (window-bound attachment, native tabs, restore)
    -> WebContentsView per loaded tab + CDP / files / diagnostics / profiling

Agent Code Mode browser.* tools
    -> server plugin owns tools, session/attachment, pending requests
    -> authenticated RPC + control events carrying request IDs
    -> desktop retrieves command, executes on the SAME native tab
    -> desktop publishes inventory before result; bounded files cross as bytes

Page network
    -> private Chromium session HTTP/CONNECT proxy
    -> authenticated plugin tunnel RPC
    -> TCP on the connected server's machine (including localhost)
```

This is **not** an iframe, `<webview>`, screenshot streaming surface, or separate normal browser window. The app DOM renders browser chrome, while **Electron `WebContentsView` paints the page above the app DOM**. Main creates one native view per loaded tab; hidden live tabs can remain executing. `packages/desktop/src/main/browser-chromium.ts:31-59,246-275`.

Ownership is deliberately split:

- Shell-session attachment lifetime, not route-component lifetime: pages survive visiting Settings/another tab and close when their owning shell session tab closes or the setting is turned off. `packages/app/src/session/browser/attachments.ts:19-30,45-70`.
- Attachment key is `server.key + newline + sessionID`; owner shell tab is learned after asynchronous hydration. `attachments.ts:45,54-68`.
- Native main entry stores `win`, binding ID, abort controller, pending requests, separate `pages` and `tabs` maps, focused ID, private partition, and storage key. `packages/desktop/src/main/browser-pane.ts:19-34,43-81`.
- Native methods use `owned(win,bindingID)` and reject a registration belonging to another window. `browser-pane.ts:257-292`.
- Agent target is an **explicit tab ID**, never “whichever tab currently has focus.” Focus is a UI selection action. `packages/plugin-browser/README.md:13-16`; `src/connection.ts:136-168`; `test/rpc.test.ts:7-14`.

## 3. Tab model, chrome, and integration

- Browser tabs are ordinary entries in the existing session Review-tab strip, not new top-level browser windows. Keys are **`browser:<tabID>`** (not `browser://...`); legacy `browser` is recognized. `packages/app/src/shell/state/session-tabs.ts:1-5`.
- `createSessionBrowser` adapts desktop inventory to existing layout tabs, opens the Review panel on browser focus, syncs active tab to native focus, sends close when a layout browser key disappears, and adds only newly observed inventory IDs so a locally closed tab is not immediately resurrected before native confirmation. `packages/app/src/session/browser/model.ts:30-42,72-114`.
- Browser-open command is `mod+shift+b`; open creates an empty tab. `model.ts:52-65`.
- Tab UI is sortable/closable and displays a **generic globe icon**, then browser label for blank URL, otherwise title or URL; this source does not show favicon fetching. `packages/app/src/session/files/session-side-panel.tsx:388-409`.
- Toolbar: back/forward enabled from real Chromium navigation state, loading spinner toggles Stop/Reload, F5 command, address form, inline error banner, suspended-state message. `packages/app/src/session/browser/pane.tsx:40-52,153-254`.
- Address is an editable native input with an overlaid, scheme-colored text rendering; scheme detection is cosmetic. Empty submission does nothing at this SHA. `pane.tsx:203-238`. **No omnibox search provider, bookmark/history UI, certificate panel, or browser-profile management was established in the inspected implementation.**
- Localhost/127.x/IPv6-loopback input is prefixed `http`; other bare addresses use `https`; accepted top-level destinations are HTTP, HTTPS, and `about:blank`, without embedded user/password. `packages/desktop/src/main/browser/policy.ts:3-16`.

### Native geometry / overlay strategy — particularly transferable

`packages/app/src/session/browser/pane.tsx:54-150` is the key UI file:

1. Measure an actual surface div with `getBoundingClientRect()`.
2. Convert CSS coordinates by the app webview zoom factor; round **edges**, then derive width/height. Include tab, visibility, bounds, backdrop, and device pixel ratio in a deduplication key (`:68-97`).
3. Native page must hide for document invisibility, inactive pane, active dialog, or overlapping popover/menu/select. Detect popper-positioner overlap in DOM; exclude tooltips (`:54-60,74`). A CSS z-index cannot solve native-view stacking.
4. ResizeObserver measures immediately to keep native bounds in step with dragging. Resize, theme, route-registration, and floating-body-portal changes schedule short 300ms animation-frame measurement windows. Cleanup cancels frames and observers (`:105-150`).
5. On registration change, cleanup captures and hides the **outgoing** registration, not the newly routed session's registration (`:110-117`). This avoids ghost views from another session.
6. Backdrop colors are resolved by painting to a 1-pixel canvas, avoiding Electron parser incompatibility with custom CSS color formats (`:75-96`).

Main hides all other pages, lazy-loads the selected tab, sets bounds, then shows it (`browser-pane.ts:257-272`). Native rounded bottom corners are **two Electron ImageViews** above the WebContentsView, with scale-aware corner images and zero-duration animated bounds to force a composited layer (`browser-chromium.ts:249-254,282-307`). They are not CSS clipping.

**Known upstream rough edge:** `session-side-panel.tsx:598` passes browser visibility based only on whether the active Review tab is a browser. Open [PR #49425](https://github.com/anomalyco/opencode/pull/49425) specifically fixes hiding when the right panel closes and immediate hide while animation frames are paused. Open [PR #49432](https://github.com/anomalyco/opencode/pull/49432) addresses blank/error surface polish, URL selection/submission, and toolbar spacing. Both were unmerged when queried. Do not credit their proposed behavior to this pinned implementation.

## 4. IPC and trust boundaries

### Intended browser adapter

`packages/app/src/runtime/platform/browser-pane.ts:1-42` describes the app-facing platform seam (target endpoint/server/session; registration commands/layout/close; state/focus events). `packages/desktop/src/renderer/platform/index.ts:34-62` creates a UUID, filters events by it, waits for registration before commands/layout, and makes close idempotent. Events are removed on close.

`packages/desktop/src/shared/ipc-rpc/browser.ts:5-43` schema-validates binding IDs, target fields, the command union, finite bounds, 0–255 color channels, and radius 0–100. This is bounded structured IPC, not arbitrary JavaScript supplied by the UI.

### Electron transport in the inspected app

- `packages/desktop/src/main/ipc.ts:63-79`: on each BrowserWindow load, main creates a MessageChannelMain, binds one port to that window's WebContents, and posts the other port to it.
- `packages/desktop/src/preload/index.ts:5-15`: forwards that port to the page and exposes only window ID / file-path helper via contextBridge.
- `packages/desktop/src/main/ipc-transport.ts:39-82,84-126`: one current binding per sender, MessagePack parsing, sender association, disconnect cleanup on destruction/port close, and client-ID routing.
- Remote page WebContentsViews receive **no app preload** in their explicit webPreferences. They are not BrowserWindows used for this handoff (`browser-chromium.ts:45-59`).

**Limits:** this is a trace of the current general transport and intended browser seam, with the production registration omission noted in §1. It is not proof of a completed origin-security audit. In particular, a port is an authority-bearing capability; Whipcode should explicitly gate it to its own trusted application renderer/top-level document and test navigation/iframe attempts, not assume a UUID authenticates a client.

### Untrusted page defaults

`browser-chromium.ts:45-59,136-170`:

- `nodeIntegration:false`, `contextIsolation:true`, `sandbox:true`, `webSecurity:true`, `webviewTag:false`, `devTools:false`.
- `focusOnNavigation:false` so hidden/agent navigation does not steal keyboard focus.
- `backgroundThrottling:false` supports hidden agent work but is a resource-cost tradeoff.
- Permission request/check/device handlers deny; display media returns none.
- Main-frame navigation/redirect guard enforces scheme/origin parsing; popup handler denies unsafe schemes and adopts valid popups as controlled tabs with the same security preferences/partition. Cross-origin HTTP navigation is allowed, not origin-pinned.
- Download handler routes to tab-owned capture files, cancels oversized downloads, and does not open arbitrary host save dialogs (`:171-192`).

**Partition semantics matter:** `browser-pane.ts:79` creates `opencode-browser-${randomUUID()}` with **no `persist:` prefix**. Tabs in one attachment share this private **in-memory** Chromium partition; a new registration gets a new partition. This is not a persistent login/profile partition. Persistent *tab URLs* (§5) must not be confused with persistent cookies/site storage.

## 5. Persistence, lifecycle, resource release, failure recovery

- Restore metadata uses namespace `opencode.browser.dat`, key `<serverKey>\n<sessionID>`, schema-decoded `{tabs:[{id,url}],focusedTabID}`. No page DOM, navigation history, transient title, loading state, cookies, or credentials are saved in that record. Malformed data becomes empty state. `packages/desktop/src/main/browser/restore.ts:5-24`; `browser-pane.ts:49-78,337-352`.
- Backing desktop state store is SQLite/Drizzle, with a 250ms write-behind default and flush/close methods. `packages/desktop/src/main/storage/state.ts:10-44,50-87`.
- Restored inventory keeps tab IDs, resets history/loading flags, and increments generations; **pages are created lazily** on visible layout or an agent command. `browser-pane.ts:51-78,265,371-375,447-449`.
- Closing a single tab aborts requests to it, removes it, picks another focused ID if necessary, disposes the view, and publishes inventory (`:320-335`). Explicit registration close removes saved restore data (`:277-280`). App/window teardown clears live pages but preserves the saved inventory by clearing the report callback before empty teardown state (`:300-318,343-352`).
- Main registration watches owning renderer destruction and main-frame non-same-document navigation; shutdown aborts attachment/network work (`:85-95,300-318`).
- View disposal removes download/network handlers, disposes profiling/CDP, clears refs, removes corner and page child views, closes WebContents without waiting for beforeunload, and disposes temporary files (`browser-chromium.ts:369-382`). Removing a child view alone is not adequate cleanup.
- Renderer process gone or CDP detached causes page failure/close; this implementation does not automatically recover crashed page DOM/history. `browser-chromium.ts:130-135`; `browser-pane.ts:385-388`.
- Connection loss: exponential reconnect from 1s up to 30s; **suspension/idle eviction has no timer** and retains inventory, waking on deliberate route/focus/visibility/pointer/key edges or commands. `packages/app/src/session/browser/connection.ts:28-78`; `attachments.ts:72-84`.
- `unsupported` and `replaced` are terminal until explicit new ownership/setup, preventing two desktops from fighting over one session. `connection.ts:36-43`; plugin `README.md:61-67`.
- No mutating command replay on timeout/disconnect; the action may already have happened. `packages/plugin-browser/src/connection.ts:173-220` and `tools.ts:60-92` make this explicit.

## 6. Agent integration and remote networking

The pure contract/operation registry is `packages/plugin-browser/src/rpc.ts`; the entrypoint merely composes connection and tool registration (`src/index.ts:1-13`). Server and desktop do not import one another's implementations (`README.md:3-6,30-36`).

Tools cover tabs, navigation, frames, DOM/accessibility snapshots/find/evaluate/wait/screenshots, click/hover/drag/fill/form/select/check/press/scroll/dialog, uploads/drop/downloads, console/network, trace/CPU/heap, and Lighthouse snapshot audits (`README.md:18-31`). This is a full agent browser, not merely an address bar.

The server tool registration uses namespace `browser`, permission category `browser`, and Code Mode; all page-derived content is explicitly untrusted. Selected operation output is schema-validated, including file metadata (`src/tools.ts:33-58,60-113`). Images are returned as model-visible file content; bounded bytes travel across RPC and are exported to server-local paths, avoiding any shared-filesystem assumption (`README.md:69-88`).

Protocol sequencing:

- Desktop subscribes to control events **before** starting long-lived attach version 4; attached event is readiness barrier (`browser-pane.ts:156-179,222-253`).
- Server control event carries request ID/cancel, not script source, file bytes, results, or full arguments. Authenticated RPC retrieves command and returns results (`plugin README.md:44-67`; `src/connection.ts:181-220`). Connection ID is correlation, not a substitute for transport auth.
- Desktop serializes outgoing publication so **server state acknowledgment precedes result** for open/close, retrying inventory until accepted or attachment ends (`browser-pane.ts:139-155`). Otherwise immediately using a returned tab ID can race the server inventory.
- Pending requests bind to attachment and tab generation; main rejects stale document actions rather than silently executing against newly navigated content. Navigation increments generation and clears refs (`browser-chromium.ts:118-125,309-338`; plugin `connection.ts:187-189`). Inspection/approved-target mechanisms exist in the contract, but that does not mean the base tool workflow enforces per-URL permissions.

### Remote network placement is a product decision, not an implementation detail

`packages/desktop/src/main/browser/network.ts:10-47` builds a private authenticated proxy over `tunnel.open/read/write/close`, installs it in the private partition, and explicitly disables Chromium's localhost bypass (`proxyBypassRules: '<-loopback>'`). Thus **localhost means the connected server**, not the Electron laptop; Chromium/JS still execute locally. Login credentials are supplied only for matching proxy host/port/basic realm; WebRTC disables non-proxied UDP (`:49-69`). There is no direct-network fallback per `plugin README.md:84-90`.

**Security caveat to carry into Whipcode's design:** the upstream README explicitly says loaded pages' traffic has the server host's network reach, including loopback/LAN, and is **not filtered per request** (`README.md:89-90`). Per-URL and server-file permission checks are explicitly deferred to final permission work (`:121-122`). [PR #46530](https://github.com/anomalyco/opencode/pull/46530) was still **open/unmerged** when queried. `tools.ts:19-30` goes from normalization/target selection to file read/request; it does not invoke `target.inspect()`/per-URL authorization in that workflow. A coarse `permission: 'browser'` tool label is not SSRF/per-destination protection. Do not copy this security posture unquestioningly.

## 7. Existing tests and what they actually establish

No tests were run for this research. The following are inspected tests, not claimed passes:

| Source | Coverage / limitation |
|---|---|
| `packages/desktop/test/browser-native.test.ts:6-7,61-65` | Real Electron smoke harness; **skips under CI** because it needs a display. Runs physical HTTP boundary with delayed/rejected state acks (first tab inventory rejected five times, nearly 8s). |
| `packages/desktop/test/browser/native.ts:1-628` | Large integration fixture exercising real Chromium and plugin transport; proxy/CORS/WebSocket behavior, cookie-header redaction, cross-origin frames, browser tools, tab persistence/restore. Calls native pane directly, not through production renderer IPC. |
| `packages/desktop/test/browser/idle.ts:59-140` | Direct adapter fixture: idle eviction releases native views, retains IDs; wake/list does not eagerly load background pages; selecting first reloads only it; agent can load second without focus; generation advances. |
| `packages/desktop/src/main/browser-pane-policy.test.ts:4-15` | HTTP cross-origin allowed; file/javascript/data/embedded credentials rejected. |
| `packages/desktop/src/main/browser/restore.test.ts:10-58` | URL/ID/focus survive database reopen; server-key isolation, clearing/removal, malformed metadata fallback. |
| `packages/app/src/session/browser/connection.test.ts:56-110` | Suspension preserves tabs/current endpoint, no retry timer, one wake, no command replay, stale events ignored, replacement/unsupported terminal. |
| `packages/app/component-tests/browser-pane.spec.ts:16-44` | Native-layout **recorder fixture**, not actual Electron compositor: hide prior registration on session change, route with no pane, Review switch, unmount. |
| `packages/plugin-browser/test/rpc.test.ts:7-70` | Explicit tab IDs, bounded inputs/files, protocol version/network lifecycle, bounded tunnel bytes not exposed as model tools. |
| `packages/plugin-browser/test/tools.test.ts:12-54` | URL/path normalization, encoded URL bounds, capture filename/path/device safety. |

Tests/tree also include tunnel, browser-error, diagnostics/analysis, and core idle tests. Their existence is not a substitute for a launched-desktop integration test. Whipcode especially needs real native stacking/focus/resize/multi-monitor tests and production preload/IPC boundary tests; the recorder fixture will not catch native compositor behavior or a missing RPC registration.

## 8. Transferable patterns, pitfalls, and boundaries for Whipcode

**Good patterns to reuse conceptually**

1. Existing tab-strip chrome + native WebContentsView page, behind a narrow desktop capability interface; keep web-only rendering independent.
2. Stable explicit tab IDs and main-owned inventory; UI focus must not implicitly retarget agent commands.
3. Separate metadata inventory from loaded WebContents to allow lazy restoration/resource suspension.
4. Explicit owner key (server/workspace/session as Whipcode chooses) and window-bound binding; route visibility is not tab lifetime.
5. Geometry/visibility protocol with zoom-aware edge rounding, deduplication, immediate outgoing-owner cleanup, and explicit modal/popover strategy.
6. Abort/dispose every layer; close WebContents, don't just detach views. Distinguish idle suspension, transport loss, unsupported protocol, replacement, and crash.
7. Versioned schema contract, control IDs rather than sensitive payload broadcasts, state-before-result acknowledgment, per-document generations, and no blind mutation replay.
8. Typed untrusted results and bounded byte transfers; server-local vs desktop-local files must be explicit.

**Do not inherit without design work**

- The newest source snapshot's missing browser RPC registration and CI-skipped native smoke test.
- Implicit server-LAN reach and deferred destination/file permissions. A local preview tab does not require exposing an authenticated remote TCP tunnel.
- Calling all persistence one thing: metadata persistence here does **not** retain login cookies across registration/app restart. Decide Whipcode profile scope deliberately, including cleanup and secret-bearing URL storage.
- Native views outrank DOM. Their blanket hide-on-overlap is functional but can flash the page away; tooltips are deliberately excluded and may be occluded. Product-quality overlay behavior deserves an explicit policy/test matrix.
- `backgroundThrottling:false` and one live view per tab can be expensive; idle/lazy restore is not a complete active-tab memory budget.
- Popups are browser tabs, all site permissions denied, DevTools disabled, and beforeunload skipped on disposal. These are actual upstream choices, not necessarily the right Whipcode choices.
- Cosmetic status: no established favicon/history/search/profile/credential UX; empty/error panel polish and immediate panel-close hiding were still open fixes.
- Do not copy all profiling/audit/remote-network complexity into a first browser-tab milestone. The minimum useful browser tab and a comprehensive agent browser can be separate increments.

## 9. Evidence ledger (bounded reads retained in research history)

Exact source identifiers and byte spans for the major findings; line numbers above come from line-numbered raw GitHub fetches. These preserve the distinction between retrieved source and interpretation:

- `9262aca1f39b32e33a41bc86afc317b1` `[0,2000)` commit metadata; `[2200,8418)` browser/test/instruction tree.
- `906ab7d049eec2ba228136ad50cbed24` `[0,7000)` pane geometry; `[7000,11400)` toolbar; `[11400,18600)` model; `[18300,22610)` connection/IPC schema; `[22600,29980)` plugin README, including network and permission caveats.
- `0c1b6912983311f606d6564613a074b3` `[0,8192)` native pane registration/partition; `[7000,9550)` ack/event ordering; `[9500,15000)` layout/ownership; `[15000,23192)` lifecycle, lazy creation, policy, restore.
- `04bf08c1e15a111d82f64135b47d33f8` `[0,8192)` Chromium preferences/lifecycle; `[6000,13000)` popup/download/CDP/geometry; `[15000,17100)` disposal; `[50321,58513)` private proxy and attachments.
- `a077ded72d53d864d11673c0dee26329` `[0,6267)` actual tool workflow; `[12600,17503)` target binding/request lifecycle.
- `1751dba1bf40d9847884e9925449ac5c` `[0,3975)` platform registration; `[6244,12384)` IPC transport/preload; `[12382,16404)` SQLite state; `[55600,59900)` browser tab strip. Exact setting-default search text span `[26247,26426)`; panel visibility search text span `[69213,69391)`.
- `ff3280f8c47bcbfdecca24ac18cd4028` `[2000,4700)` main port handoff; renderer API browser call search spans `[9915,10212)`.
- `6c230912f5e17cd315e3b0142c27cf76` `[3600,7100)` component test; shell job preview includes exact `session-tabs.ts:1-5` and app/window RPC lists.
- `054ca0e150124f50f6fcf946b27abce3` `[40620,47920)` native idle test; `[47920,55420)` policy/restore/connection tests; `[55420,61844)` plugin tests.
- `6ce735ea3a5410fc1a4827fe5d4c0181` `[0,3800)` unmerged polish/visibility PRs; `[8216,9116)` merged browser PR; `[19920,23220)` old unmerged adapter and merged plugin PR.
- Shared DesktopRpcs omission: raw shell output `job-4db64cc9` (`packages/desktop/src/shared/ipc-rpc.ts:1-32`); the subsequent unrelated guessed lifecycle path returned 404, so no claim is based on that missing path.
- Local provenance rechecked via read-only git command in research history; no porcelain entries. PR #46530 state rechecked via GitHub API in `job-06bef8fe`: open, merged=false.
