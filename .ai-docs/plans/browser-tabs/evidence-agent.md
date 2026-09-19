# Go automation / SDK transport evidence

## Final SDK and real WebSocket validation — 2026-09-17

This section records the later production integration work. The Phase 0 proposal
below is historical investigation, not current authority or an outstanding-decision
list; [contracts.md](contracts.md) is authoritative. Worktree
`whip-browser-tabs`, branch `desktop-browser-tabs`, baseline `e261d9b`.

### Screenshot upload provenance on a real WebSocket

`packages/sdk/scripts/browser-acceptance.mjs` uses the actual SDK and a race-built
production daemon over a real authenticated WebSocket. Its native bridge is
**synthetic**. The fixture creates a fresh public `tool_host` root, resolves the
real durable Once prompt, performs Browser open/run/detach, uploads a 597-byte
synthetic JPEG through `upload.begin` / `upload.chunk` / `upload.finish` on the
selected holder connection, verifies content bytes/SHA, and proves foreign-root
content access is denied. It asserts zero HTTP fallback, three upload RPCs, one
screenshot settlement with no replay, and explicit teardown. The daemon fixture's
factory uses the production tool runner for `SessionKindToolHost`.

Command after rebuilding SDK:

```sh
WHIP_SDK_RACE=1 node --test packages/sdk/scripts/browser-acceptance.mjs
```

- Initial final regression: `job-651874e8`, 15:44:57–15:45:02 UTC,
  **1 passed, 0 failed/skipped**. Log `/tmp/whip-browser-sdk-websocket.log`.
  Artifact `30c75d9872c34d88e50792c0c66bb705`, bytes `[0,608)`, SHA-256
  `0224669fcb929fbbca95a67a5d79ffc9852350427cb48fcb04536feae14e45e6`.
- Repeated against the corrected SDK below in `job-ba8dd0a7`: **1 passed,
  0 failed/skipped**, 5.375 seconds. Same 597-byte JPEG, three upload RPCs,
  HTTP calls zero, foreign-root read denied, no screenshot replay.
  Log `/tmp/whip-browser-sdk-websocket-after-cancel.log`.

This establishes the real daemon/SDK/WebSocket/broker/content path, **not** native
screenshot capture, native SSH preview routing, the packaged app, compositor,
IME, VoiceOver, or native resource acceptance. It does not substitute for the
separate native evidence below.

### Exact cancellation / late result rejection: reproduced, then corrected

The actual native integration uncovered a distinct SDK race: a result RPC was
already pending when its exact cancellation arrived, and the later rejected
result incorrectly tore down the whole selected provider. Root authorized a
narrow thaw of only `packages/sdk/src/browser.ts` and
`packages/sdk/test/browser.test.ts`. The receive-path catch now discards failure
only for that cancelled work or a retired selection; uncancelled work and
mismatched root, epoch, command ID or attachment generation still fail closed.
There is no replay and no new reconnect/rebind behavior.

Seven deterministic held-result-RPC regressions cover exact cancellation with
another attachment surviving, explicit retirement/replacement, and the five
active/mismatched negative cases.

- Before correction: `job-438ed0e2`, 15:58:34–15:58:39 UTC, the focused seven-test
  reproduction had **5 passed, 2 failed, 0 skipped**. Both intended reproductions
  failed: exact cancelled late rejection, and late rejection after retirement.
  This was a focused reproduction, not a before-fix run of the full 365 tests.
  Log `/tmp/whip-browser-sdk-cancel-before.log`.
- After correction: `job-ba8dd0a7`, `npm run check --workspace=@whip/sdk`, rebuilt
  SDK dist and passed **365/365 tests, 0 failed/skipped**, 5.220 seconds test time.
  The real WebSocket regression above then passed. Completed at 15:59:23 UTC,
  after which this source was re-frozen. Log
  `/tmp/whip-browser-sdk-cancel-after.log`.
- Combined before/after/WebSocket artifact:
  `5fca765683251cd9e3d85fb21a4b0f74`, bytes `[0,1741)`, SHA-256
  `0afad9a8241e0e4286fa064ac6974d15a8dd6770dd9286432ec079481f5eeb00`.

### Actual native/SSH follow-up and limits

The native owner independently reran the full production SDK / real remote
SSH daemon / native guest path after the SDK correction:
**`job-bad6fb5e`, exit 0, 16:00:44 UTC**. See
[evidence-native.md — Full production SDK / real daemon / native guest path](evidence-native.md#full-production-sdk--real-daemon--native-guest-path),
especially **Cancellation race corrected and actual full path revalidated**.
That owner records the exact durable permissions, own-connection JPEG/SHA,
foreign-root content denial, real timeout with `outcome_unknown`, one dispatched
CDP evaluation and actual late side-effect counter exactly one, surviving other
attachment/provider, detach/unbind/root deletion preserving the human tab,
`SDKerrors: []`, and both Mac decoys zero. Machine proof:
`/tmp/whip-browser-preview-native-sdk-full-evidence.json`.

These native results are cross-linked owner evidence, not a second independent
execution by this lane. The hidden native fixture makes no compositor/focus
claim. The old signed Beta predates the SDK correction; only root's subsequent
rebuild and packaged acceptance can establish the corrected packaged artifact.
No whole-repository race run, IME/VoiceOver acceptance, complete native resource
acceptance, or clean dependency/security audit is claimed here. Current release
and dependency gate decisions remain in [evidence-integration.md](evidence-integration.md).

---

# Historical Phase 0: Go automation / authority / provider contract proposal

Status: **proposal pending root freeze; native and remote-daemon evidence pending**. No existing production registry, generated schema, or daemon switch is changed by this spike. Baseline: `e261d9b5139d85ade9837eedccb8e9bdd7aaa5f1`; worktree `whip-browser-tabs`. Source inspection is not native/package acceptance.

## Existing reuse seams (reconfirmed 2026-09-17)

- `internal/browser/browser.go:70-135,152-164,739-837`: `Backend`, concrete Rod browser, JPEG viewport capture, AX tree and target methods. `rod.CDPClient` already has `Call(context.Context, sessionID, method string, params any) ([]byte,error)` plus `Event() <-chan *cdp.Event`. An injected client avoids a debug WebSocket/listener entirely. Call `NoDefaultDevice()` so Rod cannot resize/emulate the human guest. Existing `Browser.Close`/`detach.go` cannot own this custom transport: desktop must override `Close` to cancel the attachment only.
- `internal/browser/ext.go:64-96` and `extrelay/relay.go`: one-target CDP synthesis is precedent, not authority policy to copy. `Target.setDiscoverTargets`, `getTargets`, `getTargetInfo`, `attachToTarget` can be synthesized for a single granted target. Never forward Browser.*, Target.createTarget, arbitrary target/session IDs, profile APIs, file paths, or generic method prefixes.
- `internal/tools/browser.go:98-135`: helper parser already takes `browser.Backend`; screenshot sink already injects images. Keep its language and JPEG sink. The parser is **not JavaScript**; `js(...)` alone evaluates page JS. Current run post-check is not sufficient network/native enforcement.
- `internal/rlm/modules.go:27`, `internal/daemon/recursive_runtime.go:826-835,1100-1112`: shared module catalog and recursive host dispatch serve both Starlark and QuickJS. New calls belong there, not provider tool schemas replacing `rlm_exec`. Attachment envelopes need JSON return handling rather than current text-only `host.invoke` result.
- `internal/tools/tools.go:517-525`: `browser_exec` has shell operation admission but no generic permission requirement. Keep legacy browser behavior; add exact desktop lifecycle registrations.
- `internal/tools/mcp.go:147-189,193-242`: resolve immutable resource identity **before** `s.run`/digest, then revalidate authority and live provider immediately before effects. Reuse this pattern for browser calls; no generic dispatcher rewrite or new prepare hook is needed.
- `internal/capability/dispatcher.go:45-101,119-183,320-380`: existing `Grant`, `Reference`, operation admission/digest and `PermissionPrompt`. `internal/session/capability.go:18-28,452-503,538-612,669-721,925-953` owns persisted scopes, permission requests and generation checks. `Grant.Scopes []string` is canonicalized as filesystem paths; do **not** overload it with browser selectors.
- `internal/capability/rule.go:89-125`, `internal/session/permission_rules.go:100-139`: rules are exact `operation + canonical rule string`, so JSON resource selectors reuse existing tree rules/config without a new permission DB.
- `internal/daemon/executor.go:24-29,42-59,96-120,151-204`: connection pointer + lease generation, bounded pending invocation state, notify/cancel and fail-on-disconnect are the right transport precedent. Do not reuse `executor.bind` itself: its definition-wide newest-wins lease does not implement an explicitly selected per-root desktop.
- `internal/daemon/server.go:330-353,355-379`: initialized connection metadata exists; unregister hooks own disconnection. `InitializeParams.ClientKind`/`Capabilities` are declarations, **not cryptographic proof of Electron**. Current permission decisions intentionally trust connected clients; do not claim otherwise or add enrollment/signers.
- `cmd/whip/mcp.go:145-183,210-225`: standalone MCP creates a separate tool-host root and sets `deny_permissions: true`. It cannot silently inherit another root's page. Browser lifecycle calls must fail until an explicit user-paired desktop/root association and scope exist; paired headless calls still need explicit preauthorization. This is required new product wiring, not already solved by daemon-backed MCP.

## Frozen-shape proposal (root must approve)

### Model operations

```text
browser.open(url: string, preview_host_id?: string)
browser.attach(tab_id: string)
browser.run(attachment_id: string, code: string,
            expected_document?: string, timeout?: number)
browser.allow_preview_port(attachment_id: string, port: integer)
browser.detach(attachment_id: string)
```

`browser.run(session?: string, code: string, timeout?: number)` remains byte-for-byte legacy mode; `session` and `attachment_id` are mutually exclusive. Explicit desktop failures **never** consult `Manager.Session` or launch/fall back to Chrome. `expected_document` is an opaque revision token/string, avoiding JS 53-bit integer limits. `timeout` is execution-only, default 60 seconds, max 120; consent wait is separately cancellable. No `provider`, `host fingerprint`, `scope`, `local-human`, `grant`, or target IDs accepted from the model beyond offered opaque IDs.

```go
// New capability-domain structs; NOT protocol-generated in the spike.
type BrowserScope struct {
    ProviderID string `json:"provider_id"`
    ProviderEpoch string `json:"provider_epoch"`
    TabID string `json:"tab_id"`
    TabGeneration string `json:"tab_generation"`
    ProfileID string `json:"profile_id"`
    AttachmentID string `json:"attachment_id,omitempty"`
    AttachmentGeneration string `json:"attachment_generation,omitempty"`
    Rights []string `json:"rights"` // exact subset: create, control, route
    Preview *BrowserPreviewScope `json:"preview,omitempty"`
}
type BrowserPreviewScope struct {
    HostID string `json:"host_id"` // offered saved SSH ID, not DNS input
    HostIdentity string `json:"host_identity"` // existing verified remote runtime ID; see identity caveat below
    ConnectionGeneration string `json:"connection_generation"`
    EnvironmentID string `json:"environment_id"` // host+project isolation
    Loopback string `json:"loopback"` // exact canonical endpoint family
    Ports []int `json:"ports"` // sorted, unique, 1..65535
}
type BrowserCall struct {
    Scope BrowserScope `json:"scope"` // resolved only by trusted broker
    Arguments json.RawMessage `json:"arguments"` // validated original model args
}
```

SSH identity caveat from the SSH lane: current code verifies remote daemon runtime identity and SSH enforces known_hosts, but does not expose a verified key fingerprint. Use offered saved host ID + expected verified remote runtime ID + fresh authenticated SSH connection generation initially; do not label a hostname or runtime ID as a cryptographic host-key fingerprint. Host/profile retargeting must require renewed approval. If the plan requires persisted key-fingerprint matching across reconnects, extracting the actually verified known_hosts key is new work, not an existing structure.

Add `Browser *BrowserScope` to `capability.Grant` and the serialized `storedCapabilityScopes` (existing scopes JSON column; no new DB), plus browser issuer reference/generation like file/MCP delegation. Include resolved scope in `BrowserCall` before dispatcher admission/digest. Permission UI receives canonical scope, not a second request database. Existing operation identity and durable payload remain the audit authority. Open allocates inert tab/attachment IDs before approval (no native resource/network side effect) so approved identity cannot be retargeted.

Operation admission keeps module/definition capability **and** resource authority separate. Exact internal operation names: `browser.open`, `browser.attach`, `browser.run`, `browser.allow_preview_port`, `browser.detach`; legacy is `browser_exec`. `browser` capability permits these operation kinds but grants no personal tab automatically. Open/attach/port require consent and immutable offered scope; run/detach require a current attachment grant and do not prompt per click. One attachment-specific capability row can carry `browser.run`, `browser.detach`, `browser.allow_preview_port` and the exact `BrowserScope`; use existing `Reference` generation in dispatcher requests rather than adding one `Authority` field per tab. Port expansion is additionally consented, not covered merely by this operation name.

Resolve/check browser capability before any request, then reuse `IssueCapability`/delegation/`RevokeCapabilityFor` storage with browser-specific exact-match validation. The existing generic issuer string is not enough: ancestor generations must be followed and checked as MCP/file grant code does. Child copying of attachment ID fails agent ownership; delegation creates a child-bound attachment and atomically transfers the single native controller (revoking/pausing parent control), with scope subset and ancestor reference preserved. No implicit multi-controller use. Initially no model-facing new delegation verb: existing child narrowing machinery must receive explicit attachment scope, or user attaches the offered page directly to the child. Root must settle the exact child grant argument before Phase 3.

Saved rule proposal: tree-only `browser.attach` selector `{version:1,provider_id,tab_id,tab_generation,profile_id,preview:{host_id,host_identity,environment_id,loopback,ports}}`; new open rule cannot cover arbitrary newly allocated tab IDs, so first release should expose **Once only for open**. No global browser wildcard, no all-localhost, no host-retargeting. Provider epoch remains in operation scope and live grant, but excluding it from a remembered *policy* is safe only after fresh explicit association and fresh offered-resource revalidation. Root can initially disable Remember UI for all browser operations until exact matching/revocation tests land. Automatic mode can bypass an eligible prompt, not association/offers/hard restrictions. Reuse current permission mode; never nil-gate auto-approve browser calls.

### Result envelope (attachment operations only)

```json
{
  "attachment_id": "opaque", "tab_id": "opaque",
  "document_revision": "opaque", "url": "https://...", "title": "...",
  "network": {"kind": "mac|ssh", "host_id": "optional", "ports": []},
  "supported_operations": ["run", "detach", "allow_preview_port"],
  "output": "bounded text, run only", "media": [],
  "error": {"kind": "optional known kind", "message": "bounded text"}
}
```

Routine errors are values, not fatal kernel exceptions: `permission_denied`, `desktop_unavailable`, `host_not_connected`, `browser_busy`, `stale_document`, `attachment_revoked`, `tab_closed`, `preview_disconnected`, `unsupported_operation`, `outcome_unknown`. Open returns an admitted attachment even when page load fails, to prevent duplicate creation. Screenshot bytes cross only selected provider connection, capped to 8 MiB (and existing negotiated frame/content chunk bounds). Reuse `upload.begin` / `upload.chunk` / `upload.finish` RPC on that holder connection with root+agent scope, JPEG media type, expected digest and exact size, then quote its content reference in command.result. This avoids a multi-MiB base64 CDP frame. The daemon-side CDPClient special-cases captureScreenshot to read the authorized bounded bytes and reconstruct the Rod result; daemon then uses the existing screenshot sink/content grants. `packages/sdk/src/content.ts:82-126` already implements these primitives; its normal WebSocket shortcut uses HTTP, so the provider should deliberately select same-connection chunk RPC when connection binding is required. Model sees image parts/authorized handles, not base64. URL/title metadata can be sensitive; never put full arguments/CDP transcript in diagnostics.

### Wire over existing authenticated SDK connection

All operations below are typed protocol registry additions after freeze; none is a public debug endpoint. Reuse SDK notification registration and serverConn bounded outbound queues.

```text
browser.provider.bind RPC:
 {root_id, version:1, desktop_id, window_id, offer_revision,
  offered_tabs:[{tab_id,tab_generation,profile_id}],
  offered_preview_hosts:[canonical verified host+environment descriptors]}
 -> {provider_id, provider_epoch, version:1}
```

Bind is an explicit trusted UI action associating this root with this connection/window, not a model call, page event, current focus heuristic, or first-client selection. Register willingness during initialize capability negotiation `desktop-browser-v1`; bind must match that capability. A second provider cannot steal an existing association by registration. Use an explicit replace association action requiring the old epoch/current root selection, cancel old lease, and generate a fresh epoch. Reconnect never restores executable authority.

```go
type BrowserCommand struct {
    CommandID string `json:"command_id"` // operation ID + bounded CDP sequence
    OperationID string `json:"operation_id"` // existing durable operation
    RootID string `json:"root_id"`
    AgentID string `json:"agent_id"`
    ProviderEpoch string `json:"provider_epoch"`
    Scope BrowserScope `json:"scope"`
    ExpectedDocument string `json:"expected_document,omitempty"`
    DeadlineMillis int64 `json:"deadline_millis,string"`
    Kind string `json:"kind"` // open, attach, cdp, detach, allow_preview_port
    Arguments json.RawMessage `json:"arguments"`
}
```

Daemon emits `browser.command` **only** to pointer-identical bound `serverConn`; native main independently checks current grant and tab owner, then executes. `browser.command.result` RPC quotes `command_id`, `provider_epoch`, `attachment_generation`, `document_revision`, `result` or known `error`; broker matches pending command + connection + epoch and rejects late/duplicate/foreign results. `browser.command.cancel` notification identifies same command/epoch and reason. No replay API. `browser.provider.event` RPC is bounded metadata/CDP events from the holder; it can update known tab state, never create a grant or execute work. CDP event sequence gaps fail the attachment closed rather than silently desynchronizing.

A browser.run has one admitted operation but many CDP requests, so `command_id` **must not equal only operation_id**; use operation_id plus monotonic request sequence (or unique opaque request ID). The enclosing admitted operation establishes native grant once; each per-tab command carries current epoch+generation+operation ownership. Native serialization must cover the whole run batch, not interleave two runs one CDP call at a time. One running batch per tab; bounded queue (8), deadline/cancel admission, no registry lock across waiting. CDP commands and events are a transport implementation detail, not callable model protocol.

For an SSH daemon: selected Mac SDK client already reaches that daemon through the SSH-forwarded authenticated daemon transport. Its `serverConn` is the browser provider connection. Rod runs remotely; `Call` emits browser.command on **that same connection**, Mac executes guest-only debugger command, and result returns as ordinary RPC. Page traffic uses a separate approved SSH preview route. No tunnel/debug listener on Mac; no new auth or permission DB. Actual SSH-daemon -> selected native guest test is still required after protocol registration, not established by a transport fake.

## Lifecycle and semantic constraints

- Open/attach returns only after native created/attached and renderer inventory acknowledgment admits the descriptor into the originating still-valid pane. Late focus must not steal another task; if destination closed, clean up unowned resources or return honest partial outcome.
- `expected_document` compares native top-level document token before first effect; every later helper command is tied to a batch token and current generation. Intentional goto/back may advance its own batch; unrelated human navigation cancels stale work. Frame/object IDs must be revalidated; document revision does not freeze DOM.
- Default-deny exact CDP method list and sensitive parameter validation is authoritative in Electron main. Runtime JS is confined to the granted guest. No filesystem paths, cookies/profile-wide APIs, arbitrary target/session IDs, new targets, debugging app/main, or Browser process calls. `tabs()` sees exactly one grant; `useTab` rejects all others. Upload/download helpers return explicit unsupported until authorized byte transfer exists.
- Cancellation cannot undo JavaScript/click/network effects already initiated. Cancel stops future dispatch/queued calls and invalidates the batch generation. Do not claim a context deadline cancels JS already running; report `outcome_unknown` for potentially delivered mutations. Native may terminate the guest execution, but that affects the human page and still cannot undo network effects.
- Pause/detach/agent stop revokes control and cancels batch; page and independent human preview lease remain. Tab close destroys guest and route. Preview port revoke closes active destination sockets and cancels affected actions. Provider/SSH disconnect fails closed. Reconnect/restart restores descriptors, not grants. Native DevTools contention must pause/detach visibly, not auto-reconnect/replay.

## Proof ledger

### Commands actually run

1. `go test ./internal/browser -run '^TestDesktopNativeRod$' -count=1 -v`: **compiled, SKIPPED** without explicit native fixture. Not native evidence.
2. `BROWSER_SPIKE_CONTROL=/tmp/whip-browser-native-control-v3.json go test -race ./internal/browser -run '^TestDesktopNativeRod$' -count=1 -v`: initial connect failed because Rod uses invalid literal WebSocket key `nil`, which the fixture's standards-compliant `ws` server rejects. Supplying valid key in test fixed it; no auth weakening/native server workaround.
3. Same native race command at 2026-09-17 05:37:41 and 05:38:15 UTC reached **actual Electron44 guest**. Navigation twice produced distinct main-frame loader identities; Info, WaitElement, DOM, same-origin iframe DOM, AX -> BoxModel -> Click worked. Test **failed**, correctly finding:
   - Existing `Browser.Fill` returns nil but leaves empty input despite `activeElement=name` and `document.hasFocus()=true`; existing `TypeText` (Input.insertText) succeeds. `PressKey`'s ASCII native key-code behavior is under investigation. Do not claim Fill parity.
   - Existing `Screenshot(ctx,640)` returns valid 780x518 JPEG on 390x259 CSS-pixel HiDPI guest. Max dimension contract is not preserved by plain Rod reuse. Need explicit desktop backend/native normalization; do not loosen assertion.
4. Added actual helper-language + screenshot sink proof under opt-in `-tags browser_desktop_spike`: `internal/tools/browser_desktop_native_test.go`. The tiny constructor in `internal/browser/desktop_spike.go` is build-tag-excluded from normal builds; no production registry/constructor is changed.

Native fixture owned/launched/cleaned by browser-native lane. This lane closes each CDP transport and all Go test jobs have finite process lifetimes. Secrets/control token are not included in evidence. `internal/browser/desktop_native_test.go` intentionally captures incompatibilities rather than changing production or calling the default Browser.Close (which would send Browser.close).

5. At 2026-09-17 05:42:25–05:42:39 UTC against live v6 native guest (`/tmp/whip-browser-native-control-v6.json`), both commands ran fully with `-race` (job `job-3d84a574`):
   - `go test -race ./internal/browser -run '^TestDesktopNativeRod$' -count=1 -v`: **FAIL only known Fill/PressKey and 640-pixel screenshot limit mismatch**. Other assertions passed: native DOM, same-origin iframe, AX/BoxModel/Click, one-target tabs, rejecting foreign UseTab/target/session, Browser.close/download behavior, Target create/exposeDevToolsProtocol, Storage/Network cookie APIs, DOM.setFileInputFiles, Page.printToPDF. A 100ms context timeout cancelled the caller wait; deliberately delivered page Promise still set `window.lateEffect=true`. This is direct native evidence that cancellation must report potentially executed work, not pretend to undo it. Native JPEG 780x518, 13951 bytes.
   - `go test -race -tags browser_desktop_spike ./internal/tools -run '^TestBrowserHelpersDesktopNative$' -count=1 -v`: **PASS** (0.27s test; 1.321s process including race runtime). Actual existing helper parser ran goto/waitFor/page JS focus/type/info/tabs/screenshot; existing screenshot sink received valid native JPEG 780x520, 27068 bytes. Foreign useTab was rejected. This proves real helper-language/image integration, not Fill or HiDPI normalization.
   - No race reports. Both test processes exited and scoped transports closed; native lane was notified it could run final fixture teardown. The two commands ran sequentially; outer shell success corresponds only to second command and must not hide the first failure.

Observed CDP transcript method names, including negative probes: Accessibility.getFullAXTree, Browser.close, Browser.setDownloadBehavior, DOM.getBoxModel, DOM.setFileInputFiles, Input.dispatchKeyEvent, Input.dispatchMouseEvent, Input.insertText, Network.getAllCookies, Network.getCookies, Page.captureScreenshot, Page.disable, Page.enable, Page.getLayoutMetrics, Page.navigate, Page.printToPDF, Page.stopLoading, Runtime.callFunctionOn, Runtime.evaluate, Storage.getCookies, Target.attachToTarget, Target.createTarget, Target.exposeDevToolsProtocol, Target.getTargetInfo, Target.getTargets, Target.setDiscoverTargets. Full arguments/page data are deliberately not logged.

Still pending: desktop-adapted Fill/key and bounded screenshot implementations; expected-document stale work enforcement; out-of-process/cross-origin iframe policy; selected remote SSH daemon -> Mac provider routing. These are **not** established by loader events or an opt-in compile/skip. Packaged desktop, real app bridge, user interaction, compositor, IME/VoiceOver and native resource acceptance are not claimed by this lane's Go test.

### Freeze decisions still requiring root choice

- Current `agents.spawn` has no browser resource-narrowing field. Proposed `browser_attachments: [attachment_id]` explicitly transfers controller(s) to child-bound grants and records issuer generation; parent's active control is invalidated while its delegation authority remains revocable. This requires careful distinction between control revocation versus retaining an issuer-only ancestor row. Reject a busy tab, no implicit share. The alternative explicit human re-offer/attach is not equivalent to the plan's promised delegated resource scope.
- Genuine SSH host-key fingerprint is not exposed by current SSHConnection; saved host+verified runtime ID+fresh authenticated master generation is available. UI must accurately label the identity being approved, and either extract the verified key for persisted host rules or disable remembering across identity reconnects.
- Initialize clientKind/capabilities cannot attest that a malicious authorized daemon client is Electron. Binding provider commands/results to the selected connection prevents generic event impersonation, **not** hostile-client attacks outside current daemon trust assumptions. If the plan intends cryptographic native provenance, that is incompatible with its no-new-enrollment/trust-model direction and needs an explicit policy change.
