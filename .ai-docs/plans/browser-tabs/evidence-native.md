# Phase 0 — native Electron and restricted CDP evidence

2026-09-17 UTC. Worktree `whip-browser-tabs`, baseline `e261d9b`. This is a disposable native fixture, **not production or packaged acceptance**. No production `main.ts`, preload, platform, shared bridge, or generated schema was changed by this lane.

## Reproduce

```sh
cd /Users/samheutmaker/Desktop/context-labs/src/rlm/whip-browser-tabs
# Only if npm ci left the pinned package without its executable:
node node_modules/electron/install.js
BROWSER_SPIKE_KEEP=1 \
BROWSER_SPIKE_EVIDENCE=/tmp/whip-browser-native-evidence.json \
node_modules/electron/dist/Electron.app/Contents/MacOS/Electron \
  apps/desktop/scripts/browser-spike.cjs
```

`browser-spike-preload.cjs` is an actual sandboxed Electron preload; `browser-spike.cjs` registers the handlers and runs the checks. Each run has a new temporary userData/profile, four private session partitions, a loopback fixture HTTP server, and only its own Electron process. Default run removes all temporary files; `KEEP=1` retains screenshots, not the profile. No Whip application instance is replaced. No Chromium remote-debugging port is enabled.

For the independent Go/Rod fixture, set `BROWSER_SPIKE_CONTROL` to a private temporary file and `BROWSER_SPIKE_HOLD_MS=240000`. The file contains an authenticated fixture-only WebSocket URL, one synthetic target/session ID, and fixture origin. Never publish this token. The server is a narrow shim over `guest.webContents.debugger`, not Chromium's debug server. Production should use the existing authenticated daemon connection instead of this fixture WebSocket.

## Executed result

Hardware: Apple M4 Max, macOS 26.3.1 (25D771280a), arm64. Electron **44.2.0**, Node launcher 24.14.1. The successful complete native run was shell job `job-12d8100b`, started **05:38:01Z**, finished **05:38:19.102Z**, exit 0, `NATIVE_SPIKE_OK`, evidence `/tmp/whip-browser-native-evidence-final.json`.

The successful run performed these operations against real, displayed native views:

| Check | Observed evidence |
| --- | --- |
| Real preload registration | `contextBridge` → `ipcRenderer.invoke` → `ipcMain.handle` returns `pong`. No direct test invocation of the privileged handler. |
| Sender and frame validation | A same-origin actual subframe with deliberately enabled fixture preload is rejected; a separate BrowserWindow with that same preload is rejected. Validation checks exact WebContents, exact mainFrame object, and renderer URL. Guest itself has no preload. |
| Four views | Four simultaneously visible `WebContentsView`s, each 390×260 DIP, at (20,90), (420,90), (20,370), (420,370). |
| App zoom → native bounds | CSS rectangle (10.25,20.25,301.5,220.5), app zoom 1.25 → DIP (13,25,377,276), rounding edges, no DPR multiplier. Old layout revision rejected. |
| Focus and typing | Actual native guest focus plus `sendInputEvent` inserts `x` in its visible input. Shell regains focus during overlay. This is not physical IME acceptance. |
| Native composition | `desktopCapturer` captured the exact native window with existing Screen Recording consent. Visible guest-green field **339,092 pixels**; after hide ACK and overlay, **16 pixels**, with **722,888 purple overlay pixels**. Screenshots are 1000×760. Color classification tolerates ScreenCaptureKit/display color management. |
| Screenshot API boundary | `BrowserWindow.capturePage()` captures the application renderer, **not WebContentsView composition**. Its green-field assertion failed and was not used as compositor evidence. `guest.capturePage()` returns the actual guest image: 780×520 device pixels, 22,242-byte PNG. |
| Guest privilege separation | `require`, `process`, and `browserSpike` are undefined. Identical HTTP origin in another session partition cannot see selected guest localStorage. |
| Denial before effect | Notifications denied; popup denied; download cancelled; IPC `file:`/`javascript:` navigation rejected. CDP file URL rejected. |
| Navigation lifecycle | Native back/forward history changes URL without replacing guest identity. |
| Restricted CDP | One synthetic target. Browser.close, arbitrary Target creation/attachment/getTargetInfo/sendMessage, foreign session, DOM.setFileInputFiles, Browser.setDownloadBehavior and file navigation rejected. No Network or Storage domain exposed. |
| Real page automation primitives | Debugger AX tree, DOM document, PNG screenshot, and subframe execution contexts returned. |
| JavaScript dialog | Actual `confirm()` emits `Page.javascriptDialogOpening`; scoped `Page.handleJavaScriptDialog` dismisses it and evaluation returns false. |
| DevTools contention | Guest-only detached DevTools opened; Electron44 debugger **remained attached**, no detach reason. Subsequent Runtime evaluation returned 5. This measurement does not justify implicitly sharing control in production. |
| Unsaved close | `close({waitForBeforeUnload:true})` emitted `will-prevent-unload`; fixture explicitly allowed close, then observed destroyed. |
| Crash isolation and cleanup | Force-crashed another guest; `render-process-gone` reason `killed`; application and selected guest stayed alive. All owned guest/debugger/window/server resources cleaned up in the successful run. |

Expected IPC rejection errors appear on stderr; assertions require them. Earlier exploratory runs caught two real integration hazards and were not called passing: the session protocol filter initially blocked Electron's own `devtools://` frontend, and a closed WebContentsView's `webContents` getter becomes undefined. The fixture now permits DevTools resources only when they are not requests from the guest, and cleanup checks the optional getter. Production should bind an explicit known DevTools identity, not generalize that fixture exception.

## Rod seam: demonstrated boundaries and remaining failures

The browser-agent lane owns `internal/browser/desktop_native_test.go` and its authoritative helper results. Actual native integration, not mocks, found:

- Rod's default WebSocket handshake uses a non-RFC `Sec-WebSocket-Key: nil`; the standard Node `ws` server rejects it. A valid test handshake header fixed connection. This is fixture transport compatibility, not reason to expose Chromium's broad debug port.
- `NoDefaultDevice()` plus `PageFromTarget("selected-tab")` avoids viewport emulation/new-target creation. Target discovery/attach/info are synthesized; accepted page commands go only to one selected guest debugger.
- Existing helper `Fill` did not change input even though focus/activeElement were correct, while `Input.insertText` did. This is a real keyboard helper compatibility problem; **existing helper parity is not yet accepted**.
- Screenshot maximum-dimension handling currently uses CSS layout metrics while Electron returns device pixels (390 CSS px → 780 image px on this display). Native backend normalization is required; do not relax the helper contract.
- The v6 actual Go `-race` run reached every later escape assertion: foreign target/session, profile-cookie, filesystem, printToPDF and browser commands were all denied. A 100ms cancellation returned its deadline while a previously dispatched delayed page side effect still completed: post-dispatch cancellation requires `outcome_unknown`, never safe-to-retry.
- The actual existing helper-language parser and screenshot sink test passed with `-tags browser_desktop_spike ./internal/tools TestBrowserHelpersDesktopNative`: navigation/wait/JS focus/type/info/tabs and a real 780×520 JPEG (27,068 bytes). The direct Browser test remained failing only on the explicitly retained Fill and max-dimension assertions; those failures were not skipped or weakened. See `evidence-agent.md` for the exact Go commands/transcript.
- An experimental focus-emulation check was discarded after its native-shell-focus assertion failed. Background automation must not silently steal human focus or rely on unproven focus emulation.
- The fixture has no per-command grant/document token or provider-disconnect revocation implementation. It does not prove those production security guarantees. Same-origin subframe contexts are proven; out-of-process cross-origin iframe sessions are not.

The allowlist contains the current helper families only: selected Target metadata/session synthesis; selected Page navigation/history/layout/screenshot/dialog; Runtime execution/object operations; DOM query/geometry; AX tree; Input events. `Browser.getVersion` is synthetic. No Browser operation forwards to Electron, and Network/Storage/cookie/process/file/target-creation domains are denied. All direct target commands require the one synthetic session ID. Real production must use a reviewed exact method/parameter list from the helper transcript, plus outer admission and bounded response/media handling.

## Proposed exact native/platform contract (root approval required)

Keep one optional `AppPlatform.browser` and one typed `DesktopBridge.browser`, version **1**, with no Electron objects, generic IPC invocation, or renderer CDP exposure. The Web adapter has no capability (not an iframe fallback). Shared types belong in a small app browser-types module; Electron imports stay desktop-only.

```ts
type BrowserTarget = { epoch: string; tabId: string; generation: string };
type BrowserRestoreTab = {
  id: string; url: string; titleHint?: string; environmentId?: string;
};
type BrowserTabState = {
  id: string; generation: string; documentGeneration: number;
  status: 'restored' | 'ready' | 'unavailable' | 'crashed';
  url: string; title: string; loading: boolean;
  canGoBack: boolean; canGoForward: boolean; zoomFactor: number;
  environmentId?: string; error?: { code: string; message: string };
};
type BrowserInventory = {
  epoch: string; revision: number; tabs: BrowserTabState[];
};
type BrowserPresentation = {
  epoch: string; revision: number; blocked: boolean;
  slots: Array<{
    tabId: string; slotId: string;
    bounds: { x: number; y: number; width: number; height: number };
  }>;
};
type BrowserAction =
  | { kind: 'navigate'; url: string }
  | { kind: 'back' | 'forward' | 'stop' | 'focus' }
  | { kind: 'reload'; ignoreCache?: boolean }
  | { kind: 'find'; text: string; forward?: boolean; findNext?: boolean }
  | { kind: 'stop-find'; action: 'clear' | 'keep' | 'activate' }
  | { kind: 'zoom'; factor: number }
  | { kind: 'devtools'; open: boolean };
interface BrowserPlatform {
  readonly version: 1;
  snapshot(): Promise<BrowserInventory>;
  restore(input: { epoch: string; tabs: BrowserRestoreTab[] }): Promise<BrowserInventory>;
  create(input: { epoch: string; url: string; environmentId?: string }): Promise<BrowserTabState>;
  admitted(target: BrowserTarget): Promise<void>;
  present(input: BrowserPresentation): Promise<void>;
  act(input: BrowserTarget & { action: BrowserAction }): Promise<void>;
  close(target: BrowserTarget): Promise<{ status: 'closed' | 'cancelled' }>;
  onEvent(listener: (event: BrowserEvent) => void): () => void;
}
```

`BrowserEvent` is a discriminated union: complete inventory snapshot; native `focused` target; find result with request ID/matches/active ordinal/final; and fixed shell shortcut enum (never arbitrary keystrokes). Inventory revision is main-owned. Every mutation targets current epoch and native generation. There is no authority in a saved descriptor. A model open result is complete only after `admitted` confirms the descriptor was actually placed in the still-valid originating workspace; late or overflow admission closes the unadmitted entry without stealing focus. `admitted` is bounded/idempotent and can be shared by ordinary human creation. Root may collapse this ACK into the selected-provider create protocol if that removes a redundant state owner.

### Ownership and ordering

- **Workspace:** descriptor/order/split/selection, address draft, title hint, bounded saved metadata. Saved `environmentId` is an opaque restoration reference, never a session partition, SSH master, or route grant. Missing SSH environment stays unavailable, never falls back to local networking.
- **Restore bounds:** retain the plan's cap of 8 Browser descriptors within 32 total workspace tabs; reject overflow visibly and non-destructively. The workspace lane withdrew its 32-Browser-descriptor counterproposal after checking the pinned baseline. Restore inserts absent entries only and never overwrites a live URL from stale persistence.
- **Native manager:** inventory and native-instance generation, WebContentsView/session/debugger/history/document generation, URL/security policy, native events, all teardown. Register/restore is idempotent and lazy; hiding/unmounting never closes a page. Main generates IDs for new tabs; restoration preserves the saved route ID but always creates fresh generations and no controller.
- **Geometry:** renderer submits complete max-four visible-slot snapshots, CSS client coordinates. Main uses **app** `getZoomFactor()`, rounds edges, clips to content bounds, and rejects stale epochs/revisions/unknown IDs/nonfinite geometry. Do not multiply by display DPR. A new renderer epoch or renderer crash/navigation hides everything before reconciliation.
- **Overlays:** shared renderer overlay owner keeps bounded balanced tokens. Requested overlay interactivity waits for `present(blocked:true)` ACK; release only after close-complete/unmount, remeasure before showing. Menus/submenus/select/combobox/dialog/tooltips/toasts/drag portal must obey the chosen overlap policy. One DOM z-index is not sufficient.
- **Focus:** native focus event updates focused pane. Only a fixed configured shortcut set crosses to application chrome. OS IME and hidden/background page-input behavior remain acceptance work.
- **Close:** workspace removes a descriptor only after native `closed`; `cancelled` preserves it. `beforeunload`, dialogs, downloads and native close/crash are explicit state/events, not unconditional destruction during React cleanup.
- **Automation:** separate main-only CDP adapter realizes the daemon-approved target/provider/attachment/document identity and policy. No CDP on BrowserPlatform. Explicitly pause/revoke agent control for DevTools even when the installed Electron happens to support coexistence. Detach never closes the person's page.

## Not yet proved here

Packaged/signed application and actual production bridge registration; physical IME/VoiceOver; tab drag/split gestures and all real application overlay surfaces; stable two-frame resize latency/p95/memory-cycle ceilings; cancel-vs-close human dialog choice; downloads/uploads with explicit human grants; out-of-process subframes; file chooser escape matrix; persistent-profile restart; grant/document revocation and cancellation linearization; helper screenshot/keyboard parity; remote SSH networking. These must not be marked complete from this fixture or a skipped opt-in test.

## Production native lane — 2026-09-17 follow-up

The root-approved `contracts.md` supersedes the historical eight-descriptor restore proposal above: production restores **32 metadata-only descriptors**, refuses new creation beyond eight, realizes at most eight pages and presents four. Metadata restore never overwrites a live URL or recreates any grant.

Implemented native human bridge types, production main/preload IPC registration, `BrowserManager`, strict URL/action/layout validation, isolated website session policy and `ScopedBrowserDebugger`. The debugger is main-only, bound to one guest and exposes no debug HTTP/WebSocket listener; target discovery/attachment is virtualized to an opaque selected target/session. `browser-preview.ts` adapts the separately implemented environment lease to actual Electron sessions, fixed proxies and exact WC/session/proxy-challenge authentication. Full provider protocol and actual main preview-owner wiring remain integration work, not completed by these helpers.

Reproduce the real native test (pinned Electron installation required):

```sh
node apps/desktop/scripts/browser-native.mjs
```

The runner bundles **production** BrowserManager, full production preload, production IPC registration and production scoped debugger into a private temporary profile. It uses actual Electron WebContentsViews and real IPC; its HTTP fixture serves deterministic website/application documents. No mocks or public debug endpoint stand in for these production paths.

Actual successful runs: `job-21ddaea3` (manager/IPC), `job-dfdb9054` (adds scoped debugger) and `job-31e7f041` (adds crash/recreation recovery), all `NATIVE_MANAGER_OK` on Electron 44.2.0 / darwin arm64. Assertions cover:

- actual hostile app subframe and foreign renderer denied; website lacks Node and Whip bridge;
- metadata-only lazy creation/admission, eight-create refusal, four native pages, isolated session;
- closed action/URL/generation validation, full-layout ACK hiding, stale-layout rejection, zoom-correct geometry, native focus/input;
- selected-only Rod target/session discovery, DOM/AX, trusted native `Input.insertText`, guest JPEG, lifecycle events, forbidden browser/profile-cookie/storage/file/print/foreign-target/foreign-session probes;
- delivered evaluation continuing after cancellation returns `outcome_unknown`, not a retry instruction; revoke detaches debugger;
- real beforeunload cancellation preserving the page and confirmation destroying it;
- 32-entry metadata restoration, 24 excess entries visibly unavailable, no stale URL overwrite;
- loaded guest crash isolation, generation invalidation, fresh guest recovery, old renderer epoch denial, guest destruction and IPC unregister.

Harness corrections were explicit failures, not passes: initial native child count assumed the application view appeared in the child list; a JavaScript assignment returned an uncloneable function; close destruction needed an asynchronous acknowledgment; a renderer cannot be reliably force-crashed before its first document/process is loaded. Crash recovery prompted an implementation fix: release the failed guest, advance native generation and recover explicitly rather than silently during presentation. A new human navigation skips redundant initial navigation when realizing a fresh page. No acceptance claim is based on the failed attempts.

`npm run check:desktop` passed (`job-97736ddc`). In `job-b7909878`, the primary desktop suite (including four new pure browser policy tests) had 102 passed/10 opt-in SSH tests skipped; its separate distribution/release suite had 92 passed/zero skipped. The later combined `npm run check:desktop && npm run test:desktop` also succeeded (`job-05adfb9b`): primary suite 104 passed/10 skipped; distribution/release suite 92 passed/zero skipped. **Count correction:** earlier summaries incorrectly treated the final 92-test sub-suite as the complete desktop command. None of the ten skipped SSH cases are acceptance evidence. Context menus and new fixed guest shortcut tokens were added afterward and need the final UI/native integration pass.

Still unproven: packaged/signed application, full actual app overlay/focus/accessibility matrix and physical IME/VoiceOver, configured custom guest shortcuts, p95/memory budgets, OOPIF helper parity, actual SSH-connected preview owner integration, provider/daemon/SDK authority path, screenshot content-upload path and actual Go Fill/HiDPI parity after its targeted fix. This evidence is development-native proof, not packaged acceptance.

### Native focus crossing follow-up

`job-d32d057c` passed the same production-native fixture after adding explicit trusted-renderer focus transfer. With the actual guest focused, `present(blocked:true)` resolves only after guest views are hidden and the application WebContents is focused. Real `sendInputEvent` Mod+L transfers focus to application chrome and emits the closed `address` token; Mod+R preserves guest focus. Address/find/commands/new-browser/tab cycling/close use this fixed shell-focus allowlist. This does not establish full application overlay focus-return or physical accessibility acceptance.

## Phase 4 production native control path — 2026-09-17

Implemented the private typed `BrowserAgentBridge` (identity, metadata-only preview offer, explicit selection, dispatch, cancellation, release and closed events) in the real application preload, main-process IPC and desktop platform adapter. `BrowserControl` validates the selected root/provider/window epoch, exact scope/generations/holder and expected document before effects; it does not bind any HTTP/WebSocket debugging listener. The only listener used for cross-language Phase 0/Go helper evidence remains the opt-in isolated fixture.

Executed `node apps/desktop/scripts/browser-native.mjs && npm run check:desktop` on Electron 44.2.0/darwin-arm64. Final run `job-8430b930` passed, including the original manager suite and new production `BrowserControl` via actual sandboxed preload/IPC:

- actual subframe provider IPC denial and incorrect selected desktop identity denial;
- approved reserved tab ID/generation preserved, renderer admission ACK required, unadmitted tab discarded;
- restricted selected-page evaluation, forbidden Target/Network probes, JPEG bytes returned across the private bridge;
- duplicate command returns `outcome_unknown` rather than replay; in-flight cancellation returns `outcome_unknown` while the delivered evaluation subsequently mutates the real guest;
- explicit in-flight Page navigation attributed to its operation; unrelated native/page navigation cancels the active operation; expected-document mismatch blocks evaluation;
- failed multi-attachment transfer leaves authority intact; successful transfer revokes parent holder and admits child holder atomically;
- detach preserves the human page; provider release prevents further effects; event sequences start at one and remain contiguous per attachment;
- injected lease-bind failure destroys the partially created native guest, closes the lease exactly once and leaves no native child;
- injected two-controller preview expansion interleaving rejects the second expansion, exempts only the exact initiating controller, blocks attach/transfer during expansion and blocks stale-scope reattachment afterward. This last test uses real native guests with a controlled environment callback, **not** a real remote SSH network.

Actual execution found a zero-size initial hidden viewport causing screenshot timeout. New background guests now receive an 800x600 viewport without becoming visible; measured presentation overrides it. The updated actual fixture successfully captured the never-presented guest. The cancellation assertion waits for the hidden Chromium timer's eventual effect instead of assuming an unthrottled 150ms timer. Expected-document verification was extended to every attached native command. SSH seam review found and fixed partial-resource leakage and overlapping/stale preview scope authority; their regressions executed in the final run above.

Preview offers are metadata-only and exact native prepared-connection/runtime/project identities. The initial remote HTTP/S literal-loopback URL port is included in the daemon's immutable consent scope before effects; native compares granted ports to the offered-port set plus that exact initial URL port. Additional ports remain explicit expansion. Current bindings/scopes are rechecked after asynchronous boundaries, and incompatible controllers cannot survive session-wide expansion. No connection, route, proxy credential or runtime grant is persisted by native tab restoration.

The production Go `NewDesktopBackend` and helper/parser sink tests also executed against the isolated v7 native fixture under `-race` (agent-owned `job-39aea1c6`, 06:46:47–53Z): Fill + End/Type, DOM/AX/box, target escapes, physically bounded 640x426 JPEG (22,101 bytes), helper 780x520 JPEG (27,068 bytes), and late cancellation effect passed. The earlier invocation with the wrong opt-in environment variable skipped and is explicitly not evidence. v7 exited normally at 06:51:54Z; owned profile and token file were removed, no fixture process remained.

Latest general validation after scope hardening: `job-ae6f2a4b`, `npm run check:web && npm run test:desktop`, succeeded. App TypeScript, SDK build, production Vite build and renderer artifact generation passed; primary desktop suite had **105 passed, 10 opt-in SSH tests skipped**, distribution/release suite had **92 passed, zero skipped**, and startup probe self-test passed. Prior `job-e6ffc02f` (`check:desktop && test:desktop`) had the same two test counts. The final native execution and desktop TypeScript passed in `job-8430b930` as above. Skipped SSH cases are not acceptance evidence. Raw count evidence: `5948610340aa036f301cdb7b48f41b75` spans `[16345,16514)` and `[25031,25200)`; prior `1eec383071d0c4f263b5c450bd07cb12` spans `[12380,12549)` and `[21055,21224)`. Earlier Phase 1 count correction is supported by `bae1f35a0f2c874fce38bbad17bed0d3` spans `[12261,12430)`/`[20951,21120)` and `fd9df38ec3e11c1e9d67f26b0be70cb9` spans `[11900,12069)`/`[20588,20757)`.

At that checkpoint, still unproven: full daemon/SDK/permission/provider path and screenshot content upload, actual native remote-preview/fail-closed network integration, packaged/signed renderer/native overlay focus-return and performance/accessibility matrix. Actual native SSH preview evidence follows below. A page-initiated navigation from arbitrary evaluation/input is conservatively treated as an unrelated document transition and cancels its active batch; only explicit in-flight Page navigation is attributed. This evidence is production-code development-native proof, not packaged acceptance.


## Actual selected-host production native SSH preview — 2026-09-17

User selected `sam@kuzco-4090`; the SSH lane prepared a test-owned real Unix-socket Whip daemon built from this worktree and isolated HTTP fixtures without modifying credentials, SSH configuration, or known_hosts. Native runner `apps/desktop/scripts/browser-preview-native.mjs` consumes an explicit owned manifest; it never chooses a host or installs a fake daemon. It bundles production `SSHConnection`, `BrowserPreviewAuthority`, `BrowserControl`, `BrowserManager`, preview/session policy, IPC and preload into actual Electron 44.2.0 / darwin arm64. The typed approved commands are synthesized by this native integration harness; this is **not** the complete SDK/daemon permission/broker path or packaged acceptance.

First complete actual-host run `job-b8cddd43` passed (`NATIVE_SSH_PREVIEW_OK`), with machine-readable evidence `/tmp/whip-browser-preview-native-basic-evidence.json`. An earlier run stopped at a harness assertion mistakenly expecting `close().closed`; replacing it with the actual `close().status === 'closed'` contract fixed the test, not production code.

Proven on the selected real remote:

- Native verified the remote runtime identity through its real forwarded daemon socket; metadata-only preview offer created no usable environment.
- Approved native create/admission loaded the remote marker on the exact logical IPv4 loopback origin while a **same-port Mac TCP decoy received zero connections**. Website had no Whip preload bridge.
- The remote observed the logical Host header, persistent HttpOnly site cookie, and no proxy credentials in site headers; a site 401 challenge received no proxy authorization. Actual remote WebSocket upgrade and SSE stream worked.
- Unapproved direct fetch, redirect and service-worker fetch were rejected; the matching Mac denied-port decoy also received zero connections. Independent remote denied-port counters are recorded by the SSH lane before the expansion follow-up.
- The actual preview guest produced a scoped CDP screenshot. Model detach left its human tab and SSH route lease usable.
- Disposing the native-owned SSH master destroyed/inactivated its guest and rejected stale control. A fresh webContents using the retained session failed with `ERR_PROXY_CONNECTION_FAILED`, never falling back to the Mac decoy.
- Explicit reconnect changed connection/provider authority, retained stable environment/session/localStorage, and rejected stale commands. No grant automatically revived.
- Closing the last preview tab released its routes/proxy; another retained-session navigation failed closed and both Mac decoys stayed untouched.

Native-owned Electron process, two SSH masters, Mac HTTP decoys and temporary profile were cleaned after this run. The SSH lane still owns/holds its remote fixtures for the explicit port-expansion and IPv6 follow-up; the final cleanup/counter receipt belongs to that lane. Full SDK/daemon/provider/content-upload integration and packaged renderer/native acceptance remain separate gates.

Expanded actual-host follow-up `job-9451ae57` also passed (`NATIVE_SSH_PREVIEW_OK`). Before expansion the SSH lane independently read remote counters at 15:06:38 UTC: `approved: 15, unapproved: 0` (`job-d65a50ea`), proving the direct/redirect/service-worker denial did not merely hide a successful remote request behind CORS. The expanded run adds real production `allow_preview_port`: while a run is active expansion returns `browser_busy`; after explicit end it enables the formerly denied remote port, rejects the stale exact port scope, and reaches the remote marker without touching its matching Mac decoy. A third explicit SSH connection switches to a separately approved literal IPv6 loopback scope; the guest reaches the remote IPv6 marker with `[::1]:port` Host, rejects the IPv4 origin, and never touches the matching Mac IPv6 decoy. All three decoy TCP counters are zero. Evidence: `/tmp/whip-browser-preview-native-evidence.json`. All three native-owned SSH masters, Electron, decoys and temporary profile were disposed. Remote fixture cleanup remains held briefly by the SSH lane for root-requested human-preview entry validation; no claim of final remote cleanup yet.

## Human entry and release-gate contracts approved — 2026-09-17

Root approved optional `BrowserPlatform.createPreview({epoch, connectionId, runtimeId, projectId, url}): Promise<BrowserTabState | undefined>`. At the app seam `connectionId` is the selected saved SSH profile; only its exact prepared native connection is eligible (URL hosts rejected). `projectId` is inert `cwd:<absolute selected-runtime catalog cwd>` isolation metadata. Main must verify/recheck runtime/master generation/window epoch, confirm the literal loopback URL/effective port through the existing native-owned HostPrompt (native hide plus renderer hide ACK), and mint human tab/network consent only—never agent grants. An expansion confirmation must name the complete port union and all-tabs-in-project network effect; active/concurrent operations cannot silently widen. Admission failure rolls back new resources while preserving independently approved existing human routes. Implementation and full SDK integration are pending at this entry.

The packaged broad-release gate is implemented: main enables Browser tabs in unpackaged development, but packaged launches default **off** unless `WHIP_DESKTOP_BROWSER_TABS=1`. Only trusted main `additionalArguments` enables the preload Browser bridges. Disabled main never installs Browser IPC handlers; renderer storage/URL parameters cannot opt in. `job-454fe0bf` passed desktop TypeScript and actual production-preload off/on Electron regression execution (`NATIVE_MANAGER_OK`), including absent off-mode bridges and rejected fixed Browser IPC probes. Native fixture launchers explicitly opt in. This proves the development-native feature gate implementation, not actual packaged acceptance; root owns the packaged harness.

### Transactional human preview implementation and actual-host proof

The approved human entry is now implemented. Confirmation creates only a bounded inert descriptor; no proxy/route/guest is admitted until the workspace acknowledges its exact target. A 15-second pending-admission lifetime, exact epoch/generation checks and cleanup cover cancellation, close-before-admission and stale capacity. Main re-verifies the real forwarded runtime after confirmation and again before admission; saved profile, live SSH master generation, project, literal family, effective port and complete prior scope must still match. The human transaction holds a native-view/command barrier through commit or rollback. Expansion holds an independent old-scope lease, adds only the confirmed delta, revokes all prior agent controllers, and commits prepared binding metadata only after realization succeeds. Failure destroys/releases the new guest first, revokes only the new route delta, and finally releases its old-scope hold. Independently approved human routes survive. Human consent never grants agent control.

`job-3c8faff1` passed desktop TypeScript and the primary desktop suite: **110 passed, 10 opt-in SSH skipped, zero failed** (120 total). `job-b6941341` passed human/release-gate unit cases and the actual Electron native manager/control off/on regression harness (`NATIVE_MANAGER_OK`). An initial unit assertion caught port zero being accepted after URL parsing; the production validator now explicitly requires effective ports 1..65535, and the corrected test passed.

Actual selected-host human integration passed twice: `job-089c8dd5`, then final runtime-revalidation build `job-d80065b2` (`NATIVE_SSH_PREVIEW_OK`). It proves prompt cancellation and a confirmed descriptor closed before admission create no route/guest; successful human admission loads the actual remote marker; injected realization failure **after expansion** removes the new descriptor and new-port access while the old human guest/route remains usable; subsequent explicit union confirmation successfully opens the new remote port; final tab close tears down the environment. Prompts contain both union ports, warn that all project-environment tabs gain access and expressly deny agent authority. Both exact same-port IPv4→IPv6 and IPv6→IPv4 negatives pass, with all three Mac TCP decoys still zero. Machine-readable final evidence: `/tmp/whip-browser-preview-native-human-evidence.json`. Native-owned masters, Electron, profiles and decoys are cleaned after each run. SSH lane retains the independent remote fixture for full SDK and root-owned packaged acceptance; remote cleanup is not yet claimed.

## Full production SDK / real daemon / native guest path

The test-only `browser-sdk-native-renderer.ts` is bundled into the trusted native SSH fixture shell when `BROWSER_PREVIEW_NATIVE_SDK=1`; `BROWSER_PREVIEW_NATIVE_SDK_ONLY=1` runs only this additional path. It uses the **production SDK, web desktop transport adapter, preload, DesktopTransports framing, real SSH-forwarded daemon socket, public tool_host/permission/tool.call APIs, broker, native BrowserControl/BrowserManager guest, and actual Go browser helpers**. No model call, synthetic broker response, fake daemon, or alternate browser is used.

Initial diagnostics exposed two fixture issues: the remote daemon was an older 14:56 build lacking `browser.provider.unbind`; the new renderer driver accidentally passed absent `preview.id` instead of `preview.host_id`, producing an ordinary Mac-scope open. The owned Mac decoy/remote-marker assertion correctly rejected that run. This was not an SSH fallback result, and it is not counted as remote acceptance. The driver now uses a typed native bridge, explicit host/environment validation, and an immediate `network.kind === 'ssh'` plus exact host assertion. With root approval, SSH rebuilt only the isolated daemon from frozen current Go, preserving evidence of its old epoch. New Linux amd64 CGO-disabled binary SHA256: `b0e7d9bb896d4d3785bc0fc3c0d3ef13bfad31869b7812c7c180f1b56877e19f`; daemon generation 2, runtime identity unchanged. HTTP epoch 2 retained the same ports/marker with an independently measured zero-counter baseline.

**`job-a5dfffd3` passed at 15:52:18 UTC**, actual Electron 44.2.0 darwin/arm64. A fresh real tool_host root received an external permission denial with no native tab, then a new explicit Once approval. The SDK/provider/broker/native admission path opened the actual selected SSH remote marker. The real Go helper evaluated the guest and captured a **54,541-byte JPEG** through scoped CDP; the production SDK uploaded it over the owning framed connection; root/agent-scoped content reading verified its digest and JPEG bytes. Nine unique native commands retained the exact root/agent. Real `browser_detach`, provider unbind and root deletion preserved the human native guest until explicit native close. Both Mac TCP decoys remained zero. Evidence: `/tmp/whip-browser-preview-native-sdk-evidence.json`. Native-owned processes, transport, master, guests and profile were cleaned. This closes the basic daemon/SDK/permission/provider/screenshot-upload integration gap, but is development-native rather than packaged acceptance.

An expanded run `job-a4f94f5f` additionally passed exact immutable scope/digest in the durable consent summary, foreign-root screenshot-content denial, and a real delayed-effect timeout producing `outcome_unknown`, a matching SDK/native cancel and exactly one native dispatch. It then exposed an SDK cancellation/result race: a result submitted just before the cancellation notification was rejected by the broker after cancellation, and the SDK's outer command-result rejection handler released the entire provider despite the now-cancelled command. The subsequent detach failed; its first harness assertion misleadingly described this as tab closure, so diagnostics were corrected to distinguish detach error from human-tab lifetime. Root owns the SDK correction under the source freeze; this expanded run is **not** a passing cancellation/provider-isolation acceptance claim yet. No native production source was changed for it.

### Cancellation race corrected and actual full path revalidated

The SDK owner fixed only the exact cancelled/retired command-result rejection path, retaining fail-closed treatment of active/mismatched result failures. SDK build/check passed **365 tests**, including deterministic before/after reproduction of result submission → matched cancel notification → late RPC rejection, an independent surviving attachment, mismatched cancellation negatives and retired-provider replacement safety. No native or daemon source changed for this correction.

**`job-bad6fb5e` passed at 16:00:44 UTC** against the same frozen generation-2 real remote daemon, using a fresh tool_host root and explicit provider selection. In addition to the basic full path above, the actual durable permission summary contained the exact native provider/epoch/tab/generation and verified preview host/runtime/master-generation/environment identities plus its request digest; foreign-root screenshot content access was rejected. A real delayed JavaScript effect timed out after dispatch: the broker returned `outcome_unknown`, a matching cancellation crossed the production SDK/native bridge, exactly one native CDP evaluation was dispatched, and after late completion the **actual guest counter equalled exactly one**. The SDK reported no provider errors; detach succeeded; provider unbind and root deletion preserved the human guest until explicit close. Both Mac decoys remained zero. Complete machine evidence: `/tmp/whip-browser-preview-native-sdk-full-evidence.json`. The trusted fixture shell was hidden in this run to avoid stealing focus from simultaneous packaged acceptance; this run makes no compositor/focus claim. Original failing race evidence remains `job-a4f94f5f` (output handle `98ce9ded264cd3ab374f4251b18d1ed7`).

### Real workspace → preload → strict native restore regression

The packaged first-New-Browser failure exposed a real serialization seam missed by permissive frontend mocks: `BrowserWorkspace.sync` sent `BrowserTab.kind`, while `restoreTabs` correctly allows only `id`, `url`, `titleHint`, and `environmentId`. The workspace owner corrected the explicit metadata projection; **native production validation stayed unchanged and strict**. `browser-workspace-native-renderer.ts`, bundled by `browser-native.mjs`, now executes the production `SessionTabs` and `BrowserWorkspace` in an actual sandboxed Electron renderer through the production preload and registered native IPC. It checks rejection of the original raw descriptor, address/title/environment preservation without restoring authority, first New Browser admission and post-admission sync/action, and absence of workspace errors.

`job-650daf81` passed that new seam but failed the later existing `guest.isFocused()` assertion, then hit its watchdog. Its cause is **unexplained**: the root confirmed the packaged Beta had been terminated and all desktop UI activity was paused before this run. An initial suggestion of a Beta foreground collision was unsupported and withdrawn. No product fix or activation-precondition change was made.

With only test-owned window/focus-state logging added and a 30-second watchdog, the single bounded rerun **`job-0c5dc731` passed the entire native manager/control suite in 7 seconds (16:08:05–16:08:12 UTC), ending `NATIVE_MANAGER_OK`**. Recorded state: window1 visible/focused, guest5 focused, shell unfocused, all four guest views visible, and exactly one open/focused native window. Focus/shortcut assertions were unchanged, as were the strict IPC and guest security/authority assertions. Both observations are preserved; the rerun does not establish why the first focus assertion failed. Output handle `069c68d9e7a7d492b1a9f5ebf51161ba`, focus-state bytes6319–6724. No owned native fixture process remained after this pass; exclusive desktop UI was released to root for the rebuilt signed-package test. This development fixture is **not** packaged UI/accessibility/IME/overlay acceptance.

### Packaged human consent seam: dedicated native confirmation (16:42 UTC)

Root's controlled signed-package test connected the selected SSH host, then submitted the human preview picker. No usable confirmation appeared; the operation eventually returned cancellation without authority. Source inspection found that production `main.ts` incorrectly reused the SSH **authentication-attempt** prompt callback for human preview network consent. `resolveConnection` retains the attempt-to-host mapping until disposal; `HostPrompts` routes a recognized host prompt through `HostConnectionDialog`, whose connection-completion path closes it for an already-connected host. The earlier native policy fixtures injected confirmation and therefore missed this main/frontend composition seam. The exact UI effect loop was not instrumented inside the packaged app.

Root authorized a narrow thaw. Production changes are only new `apps/desktop/src/browser-human-confirmation.ts` and the `main.ts` import/callback wiring. Human consent now uses a parented, trusted-main Electron `dialog.showMessageBox`: exact title/detail from the existing main-owned offer, explicit Cancel and Open preview buttons, **Cancel as both default and cancellation response**, the original combined AbortSignal, and post-response cancellation/window-lifetime checks. Credential-field prompts are rejected; at most one human sheet is pending per window; errors release that bound. SSH authentication prompting, preview authority, strict IPC, provider grants, and all agent consent paths are unchanged. Approval still produces only inert metadata until exact workspace admission.

Validation:
- **`job-dd4d83a0` PASS**, desktop **127 tests:117 passed,10 opt-in SSH skipped,0 failures**, plus desktop TypeScript check. Seven added tests cover adapter approval/denial, exact parent/detail/signal, pre-abort, late approval after abort or owner destruction, concurrency/error cleanup, explicit production-main wiring, and `BrowserHumanPreview` denial/abort/approval authority invariants.
- **`job-369ff53f` PASS, 16:42:18–16:42:24 UTC**, full actual Electron44.2.0 darwin/arm64 native suite. The production adapter opened a **real native sheet** for an already-connected fixture identity (`sheet-begin` observed), and the actual Electron AbortSignal path closed it and returned cancellation. The existing workspace/preload/strict-IPC, guest isolation, focus, CDP, control, crash, teardown and expansion regressions also passed; final marker `NATIVE_MANAGER_OK`. This automatic cancellation test does not claim a human approved the packaged remote offer.
- **`job-a7bc4ef9` PASS**, complete `npm run test:desktop`: the same127 desktop tests, **92/92 distribution/release/publication/provenance tests**, and startup-probe self-test.

Native production re-frozen16:42:58 UTC; all owned native GUI processes stopped, root given exclusive UI and fresh signed rebuild responsibility. Freeze SHA256: `main.ts` `5861c2762f002bbd73f28e359aa7d39cbd83305b4430c8c76afba0593b77de6a`; `browser-human-confirmation.ts` `b66473646a710876304df887e13733a3120a339ce9d09fbe6b2ba9f5a6a7069d`. The three exploratory physical-focus fixture scripts were removed without ever launching; contaminated prior packaged focus observations are not product-defect evidence. Signed-package human-preview acceptance remains root-owned and pending the rebuild.
### Real closed-window teardown regression (16:55 UTC)

Root's fresh signed default-OFF package failed ordinary Cmd+Q with a native critical alert, `TypeError: Object has been destroyed`. The bundled callback mapped to `BrowserManager`'s listener cleanup, which re-read `window.webContents` after the native BrowserWindow was already destroyed. Earlier tests explicitly disposed the manager before destroying their window and therefore masked this seam. This packaged quit remains a recorded **failure**, not a successful quit/restart.

The narrowly authorized production fix captures the shell WebContents when registering its listeners and removes those listeners through the same retained object. No exception suppression, early-dispose workaround, authority/IPC/feature change or unrelated focus change was made. The native harness now checks a deterministic throwing destroyed-window getter and two **actual BrowserWindow.close()** paths: feature-disabled/no guest, and enabled with a visible live guest. Neither pre-disposes its manager. Both assert closed-window state and idempotent later disposal; the enabled case additionally verifies that the owned guest is destroyed.

- **`job-72fa7781` PASS, 16:54:50–16:54:59 UTC**: `npx tsc -p apps/desktop/tsconfig.json --noEmit && node apps/desktop/scripts/browser-native.mjs`. Full actual Electron44.2.0 darwin/arm64 suite ended `NATIVE_MANAGER_OK`, including both new actual-close regressions. Output handle `f8f6ab044f89e5db567c626ba6935c6e`, final close markers bytes11740–12337.
- **`job-0544c218` PASS, 16:55:41–16:55:59 UTC**: complete `npm run test:desktop`; **127 desktop tests:117 passed/10 opt-in SSH skipped/0 failures**, **92/92 distribution/release/publication/provenance tests**, startup-probe self-test. Output handle `d44d9250ae3fac93d2a0d58004c9453a`, desktop totals bytes13424–13593.

Re-frozen16:55:41 UTC, GUI all clear and no owned fixture process remaining. Production SHA256 `browser-manager.ts`: `c706d6e2d1b37a4f80461c700c92e5c4d85cd68ec6384e37f505678f72f8964c`. Test fixture SHA256 `browser-native-main.ts`: `f0a7d577a0006e3387fabe7b3a1ff23140ac97620173786b363a1eaf92e76128`. `main.ts` and `browser-human-confirmation.ts` retain their previous freeze hashes. Root owns the fresh signed rebuild and ordinary packaged Cmd+Q/remote-preview acceptance; these development lifecycle tests do not claim that pending packaged validation.
