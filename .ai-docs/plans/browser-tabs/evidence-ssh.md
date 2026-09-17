# Phase 0 — SSH preview networking evidence

Date: 2026-09-17 UTC. Worktree: `whip-browser-tabs`, branch `desktop-browser-tabs`, baseline `e261d9b5139d85ade9837eedccb8e9bdd7aaa5f1`.

**Phase 0 status: real Electron networking spike passes using a deterministic private Unix endpoint. Actual selected-host SSH and packaged acceptance remain unproven.** The Phase 0 spike was initially unwired and did not modify existing SSH/native/main. The later authorized implementation milestone below now adds production SSH route/environment code; no dependencies, host configurations, keys, services or firewall rules were changed.

## Files and runnable checks

- `apps/desktop/src/preview-proxy.ts`: bounded HTTP/CONNECT/Upgrade proof using Node core only. Random proxy authentication, loopback-only listener, exact approved ports, pinned literal remote route callback, public-address validation before numeric-address connect, per-port revocation.
- `apps/desktop/scripts/preview-fixture.mjs`: private Unix listener representing an SSH forward, plus real TCP decoys on the viewing Mac. This is explicitly **not** SSH evidence.
- `apps/desktop/scripts/preview-proxy.test.mjs`: Node boundary/transport check.
- `apps/desktop/scripts/preview-electron{,-entry}.mjs`: launched Electron network check with isolated temporary userData, generated temporary self-signed TLS fixture, no app preload and no TLS-validation override. Temp directory, server sockets and child process are owned and cleaned by the fixture.

Run from the worktree:

```sh
node --test apps/desktop/scripts/preview-proxy.test.mjs
node apps/desktop/scripts/preview-electron.mjs
node_modules/.bin/tsc -p apps/desktop/tsconfig.json --noEmit
```

Observed Node proof: 2 tests, 2 passed, 0 skipped; desktop TypeScript check exit 0. Observed Electron: **44.2.0**, Chromium **152.0.7977.76**, exit 0. Runtime job IDs: `job-dfabc473` (Node + tsc), `job-6403f32b` (initial Chromium proof), `job-75b9503f` (TLS/UDP extended proof).

### Observed, not inferred

| Check | Evidence |
| --- | --- |
| Authentication | Unauthenticated Node request returns 407 with zero endpoint hits; Chromium handles an exact matching proxy challenge in main. No proxy credential is forwarded to the endpoint. |
| Logical localhost origin | Chromium location remains `http://localhost:<approved-port>`; server receives corresponding Host; fetch is same-origin. Unix route receives traffic, not the Mac TCP decoy on the same port. |
| IPv4/IPv6 aliases | Chromium successfully loads `127.0.0.1` and `[::1]` logical URLs through the approved route. These map deliberately to the approved literal target; this is not evidence of an actual remote IPv6 listener. |
| CONNECT | Node CONNECT to `[::1]:port` carries an HTTP/SSE stream. Chromium CONNECT carries TLS and rejects the temporary self-signed certificate with `ERR_CERT_AUTHORITY_INVALID`; TLS validation was not disabled. |
| WebSocket / SSE | Real Chromium receives `remote-websocket` and `remote-sse` from the private endpoint. |
| Destination enforcement | Unapproved literal/alias loopback ports, private IPv4, cloud metadata, IPv4-mapped IPv6, and injected private/mixed DNS answers are denied before endpoint connection. Chromium denied fetch and redirect paths do not hit either local TCP decoy. |
| Service worker | A real registered/activated worker attempts an unapproved loopback fetch, which is blocked; local decoy hits remain zero. |
| Revocation | Existing Chromium WebSocket closes on per-port revocation; subsequent request receives 403. Node CONNECT/SSE socket also closes on revoke. |
| No implicit bypass | Explicit `proxyBypassRules: '<-loopback>'`; localhost, IPv4 and IPv6 logical addresses reach the private route rather than the Mac. |
| No direct fallback | With the proxy stopped and session connections closed, Chromium fails with `ERR_PROXY_CONNECTION_FAILED`; local decoy hits remain zero. |
| UDP/WebRTC | With `disable_non_proxied_udp`, a loopback STUN fixture receives zero packets and Chromium produces zero ICE candidates during the bounded check. |
| QUIC/WebTransport | With `disable-quic`, loopback WebTransport rejects as `blocked`; UDP fixture receives zero packets. This does not prove every possible WebRTC/TURN/TCP, worker, or future browser API is covered. |

Expected certificate and proxy-failure messages appear on stderr; the fixture asserts these are failures, not successful page loads. No mocks/skips are reported as native acceptance.

## Selected SSH host investigation and blocker

The running application is `/Applications/Whip.app`. No current desktop `_desktop-ssh` process or `/tmp/whip-ssh-*/control` socket was found. Read-only extraction of the exact `whip.selectedHost.v2` storage key from Whip/Whip Beta found only a Beta candidate `local`, not an identifiable connected SSH profile. Unrelated SSH jobs are **not** authority to choose a host. No remote command was run and no remote file/listener was created.

A current selected saved profile and authenticated desktop master must be identified unambiguously before the authorized connected-host test. Once available, create one temporary fixture directory/listener on that host, acquire/cancel only its owned preview forward, and remove only its owned processes/files. Do not treat a standalone new SSH connection as evidence of current-master reuse. This remains a Phase 0 gate, not a skipped passing test.

## Contract proposed for root approval

### SSHConnection seam

Keep SSHConnection as the sole owner of authentication and master lifetime. Add the minimum route acquisition API after contract freeze:

```ts
acquirePreviewRoute(
  request: { remoteHost: '127.0.0.1' | '::1'; port: number; expectedGeneration: string },
  signal: AbortSignal,
): Promise<{
  generation: string;
  open(signal: AbortSignal): Promise<Socket>;
  close(): Promise<void>;
}>;
```

- Existing authenticated master only; acquisition must not call an establishment path or ask for a different host. Master absence, generation mismatch or auth-required state fails closed.
- Create a unique short Unix socket in the existing mode-0700 attempt directory using existing follow-up command path: `-F /dev/null -S <control> -o ProxyCommand=false -O forward -L <owned-unix-path>:<literal-loopback>:<port> <selected-host>`. IPv6 formatting must use brackets and be exercised against the actual OpenSSH implementation.
- Route API does not accept arbitrary DNS names, remote paths, SSH options, host IDs selected by the model, or listener addresses.
- A route tracks and destroys its open sockets on revocation. Close cancels exactly its own forwarding descriptor (`-O cancel -L ...`), unlinks only its own endpoint, and is idempotent. Master disconnect invalidates the transport generation and all existing route sockets before any new work. Reconnect never revives an attachment or replays browser effects.
- A private Unix endpoint is preferable to a TCP forward: the only browser-facing TCP listener is the authenticated proxy. No generic tunnel/RPC framework is needed.

### Scope and identity

Use native-offered `savedHostId`, verified remote runtime identity, `connectionGeneration` (fresh per authenticated master), stable project/environment ID, and exact literal remote loopback/port set. Existing SSHConnection does not expose an SSH key fingerprint: do not freeze a cryptographic fingerprint field that the implementation cannot verify. If true host-key identity is required, explicitly add and test that retrieval rather than labeling a saved alias as verified identity.

Canonical logical aliases `localhost`, `127.0.0.1`, `[::1]` route to the explicitly approved literal target and port. No DNS lookup chooses the remote target. User-facing approval must explain alias grouping and environment-wide port scope. Port 0/wildcards, arbitrary loopback blocks, LAN, metadata, URL/Tailscale hosts and unsolicited port expansion are rejected.

### Electron environment owner

- One main-owned isolated partition per verified host + stable project/environment identity. Persistent production identity; temporary partitions in the fixture. Never share the ordinary Browser profile's routing or accept a renderer-supplied partition.
- Freeze session proxy config (`fixed_servers`, one private HTTP proxy, `proxyBypassRules: '<-loopback>'`, no DIRECT fallback). Route policy changes happen at proxy boundary, not global session proxy churn.
- Supply random proxy credentials only when webContents/session and `isProxy`, host, port, Basic scheme, and unique realm all match. Never answer arbitrary website auth with proxy credentials. A worker with no attributable webContents fails closed rather than widening the credential handler.
- `webContents.setWebRTCIPHandlingPolicy('disable_non_proxied_udp')`; `app.commandLine.appendSwitch('disable-quic')` before ready. Deny local-network/device permissions. Note that QUIC switch is process-wide and its ordinary-browser impact needs explicit acceptance.
- Deny private app schemes in all guest paths, retain normal TLS/CORS/CSP, no Node/preload, and no external protocol launch. Native lane owns these guards; they must also cover worker/background navigation.
- Separate human route lease from agent control. Agent detach keeps the human-approved route. Port revocation closes sockets. Closing the last environment tab releases all routes and terminates/stops worker reach; the proxy must close even if background workers remain in its persisted profile.
- `setProxy` and cookies/workers are **session-wide**: environment-sharing tabs intentionally share the approved destination set. An attachment with a narrower route grant must be refused or isolated, never silently broadened by another tab's grant.

## Remaining gates / known limits

1. Actual selected-host authenticated-master forward acquisition, remote HTTP/WS/SSE/TLS, remote IPv6, exact forward cancel, disconnect/reconnect generation, and two hosts serving the same logical origin.
2. Actual packaged production integration; current fixture runs an isolated un-packaged Electron entry, not trusted renderer IPC or production SSHConnection.
3. Full adversarial request matrix: real public DNS resolution/pinned public HTTPS, DNS answer changes between successive connections, cross-origin subframes, background-worker lifecycle after last-tab close, WebRTC TCP/TURN, proxy auth challenge confusion, cancellation during DNS/dial, and aggregate stream-pressure limits. Address classification tests do not substitute for these.
4. At initial Phase 0, shared per-window accounting was absent. The authorized implementation below now shares a 32-stream/4 MiB queued-write budget across all window environments, with cancellation/backpressure tests. This is an application queued-write bound, not a claim that Chromium/kernel/Node total process memory is limited to 4 MiB.
5. Public-IP classification is intentionally conservative and must be maintained against special-purpose address changes. The proxy never does a second hostname lookup after validation; it connects to a pinned numeric address. Network route changes outside this process are not prevented by this design.
6. Fresh public-path request coverage and allowed TLS success, production persistent-profile cleanup, permissions/broker enforcement, and restore/reconnect are not claimed by the spike.

An important implementation trap found by the runnable fixture: Node `http.request({agent:false, createConnection})` ignored the injected connection and tried the default network path. The proof uses an explicit single-request `Agent` whose `createConnection` returns the already-policy-validated socket. The local decoy catches future regressions to direct routing.

## Authorized Phase 5 implementation milestone (2026-09-17, 06:01 UTC)

Root accepted the route direction and froze `contracts.md`; this lane subsequently implemented:

- `preview-ssh-route.ts` plus focused `ssh.ts` additions: `previewConnection` is available only for the live authenticated master; `acquirePreviewRoute` requires its exact generation and never establishes a connection. Unique mode-0600 Unix forwards in the existing private attempt directory, literal IPv4/IPv6 formatting, exact `-O cancel`, per-route tracked sockets, abort propagation and idempotent close.
- `preview-environments.ts`: one owner/window; stable isolated profile per saved host + verified runtime ID + project. Atomic synced mode-0600 metadata, bounded read/file/profile limits, corruption refusal without overwrite; stored records contain no ports, live generation or grants. `ensure(approvedScope)` prepares an approved live environment; `acquire(environmentId,tabId)` leases it, while an inactive restored ID fails unavailable pending renewed approval. Thirty-second setup reservations clean up abandoned realization. Four active environments/four ports each; exact scope checks; expansion invalidates old attachments before route publication, racing revocation prevents publication; last-tab close and master loss close proxies/routes even if background workers survive.
- Strict lease-scoped proxy authentication: bound webContents ID, pointer-identical session, live lease, proxy flag, host, port, Basic scheme and random realm must all match. Null worker identity fails closed instead of gaining a generic credential handler.
- `preview-budget.ts`: shared window limit of 32 streams; 64 KiB write chunks; up to 4 MiB aggregate pending application writes with backpressure and cancellation. Pending uncancellable DNS lookup retains its stream reservation until completion, preventing repeated canceled lookups from escaping the bound. This does not purport to bound total Chromium, Node or kernel memory to 4 MiB.
- `preview-environments.test.ts`: identity isolation/inert restore, atomic private metadata/corruption refusal, expansion/revoke/master loss, four-environment limit, revoke-during-expansion race, exact authentication denial matrix, 4 MiB stalled-write cancellation, literal forward/cancel/socket disposal, shared stream budget.
- Additional `ssh.test.ts` checks: no implicit establishment without a live master, plus a conditional real isolated sshd test for IPv4 and IPv6 same-master forwards, exact cancellation preserving the daemon/listeners, and master-disposal stream invalidation.

Final regression run at this milestone: `job-631afe24`, exit 0. Desktop TypeScript clean; desktop suite **114 tests: 104 passed, 10 explicitly skipped SSH integration cases requiring a built helper**; standalone proxy **2/2 passed**; real Electron fixture full HTTP/CONNECT/TLS/WS/SSE/worker/revocation/bypass/UDP matrix passed. The skipped SSH cases are not evidence of working SSH production routing.

An attempted owned temporary helper build (`job-12cdacc8`) was blocked by another lane's then-in-progress Go type references (`capability.BrowserCall` undefined in `internal/browser/desktop_types.go`). The helper temp directory was cleaned and no cross-lane source was changed. Root was asked for a prebuilt baseline helper or build-readiness. Actual selected remote-host identity also remains pending root investigation.

Native owner accepted the exact interfaces and is integrating actual Electron session creation/proxy setup, bound auth, sole session resource filter and lifecycle callbacks. That integration and the packaged acceptance matrix are not claimed by the standalone fixture. No commits or staging were performed by this lane.

### Metadata-only offers and real isolated SSH validation (06:44 UTC)

Native provider integration needed a stable environment ID before model consent. Added `describe(identity): Promise<string>`: bounded atomic metadata only, zero connection lookup/SSH forward/proxy/session activity. Concurrent descriptions reuse the same identity-specific ID; inactive `acquire(id)` still rejects until approval. `ensure(approvedScope)` returns that same ID after consent. Tests explicitly assert zero connection resolver calls and absence of destinations/generation in stored offer metadata.

After the shared Go type blocker was resolved, `job-42cd8029` built the current Whip helper into an owned temporary directory and ran the complete desktop suite with `WHIP_DESKTOP_SSH_TEST_EXECUTABLE` set: **115 tests passed, 0 skipped, exit 0**. This includes the real isolated generated-key sshd test for current-master IPv4 and IPv6 preview forwards, echo through each private Unix route, per-route close preserving the daemon and remote listeners, and master disposal aborting active streams and generation authority. All existing real SSH authentication/reconnect fixture tests also ran. The helper and generated fixture resources were cleaned. Full suite evidence: shell output `28125eadb7d7756228d1fab9b544f4b7`, bytes 6000–9000 includes same-master preview test.

This removes the local real-SSH helper/test blocker; it does **not** identify or test the user's actual selected remote host, nor constitute packaged acceptance. The preceding full desktop TypeScript attempt (`job-10e0d707`) was blocked only by another lane's then-in-progress `browser-control.ts` narrowing errors; this lane did not modify that owner file or claim the current full check green.



## User-selected remote acceptance setup — 2026-09-17 15:00 UTC

The user explicitly selected `sam@kuzco-4090`; root authorized isolated test-owned SSH/native-master acceptance. Existing-config SSH with `BatchMode=yes`, `StrictHostKeyChecking=yes`, no ControlMaster reuse succeeded. No credentials, host keys, SSH config, firewall, or user daemon settings were changed.

- Built current worktree `cmd/whip` for Linux/amd64 (`CGO_ENABLED=0`) and native macOS; build job `job-db94ea0a` succeeded.
- Remote setup `job-a15d9efa` uploaded the Linux helper and stdlib-only `scripts/preview-remote-fixture.py` into an owned temporary directory. Started a real daemon with isolated `WHIP_HOME`/`WHIPCODE_HOME`, `WHIP_NETWORK=0`, plus ephemeral literal IPv4/IPv6 HTTP endpoints and a distinct unapproved-port endpoint. This is not the earlier fake echo daemon.
- Read runtime identity `5a71387ffd0e47c7a15ce14bc51aae22` using protocol-major 6 `initialize` on that remote daemon's Unix socket (`job-ad3b15e7`). An initial major-3 probe was correctly rejected as unsupported before retrying with the repository's actual manifest version; no acceptance pass was inferred from that failed probe.
- Handed native a manifest containing the test-owned selected saved-profile ID, stable owned project directory, actual runtime identity, exact remote helper/home target, local SSH-supervisor executable, remote endpoint ports and unpredictable marker. Native owns the one production `SSHConnection`/master, local same-port decoy, and Electron `BrowserPreviewAuthority` → `BrowserControl` → lease-backed `BrowserManager` acceptance.
- SSH lane retains cleanup ownership of the remote daemon/HTTP fixture and helper directories, and will clean them after native explicitly releases them. At this point setup is verified; integrated actual-remote Electron acceptance is **pending**, not claimed passed.

### Independent remote denied-port counter check — 15:06:38 UTC

At native's request, after its definitive actual selected-host production Electron run and **before** any explicit port expansion, SSH lane independently read the remote fixture counters over existing-key strict-known-host SSH (`job-d65a50ea`, exit 0):

```json
{"approved":15,"unapproved":0}
```

The still-running owned daemon and fixture PIDs were confirmed against their exact executable paths. Thus the separately listening remote denied endpoint received **zero requests** during the initial deny/redirect/service-worker coverage. Native owns the detailed integrated run evidence (`job-b8cddd43`, `/tmp/whip-browser-preview-native-evidence.json`) and its assertions, including local decoy counters. Native was explicitly released to test `allow_preview_port` only after this counter capture; subsequent formerly-denied endpoint hits are expected only with explicit approval. Remote resources remain intentionally retained for expansion/IPv6 follow-up, not forgotten cleanup.

### Post-expansion remote counters and owned-resource audit — 15:17:49 UTC

After native's explicit port-expansion and actual IPv6 follow-up, independent SSH read `job-5512abf5` (exit 0) returned:

```json
{"approved":29,"unapproved":1}
```

The formerly denied remote endpoint received exactly **one** request after native explicitly approved that port; it had zero requests before approval. The isolated remote daemon remains the same owned PID and generation, with no network endpoint, and the only retained fixture process matches its exact owned script/directory. Local process inspection found no owned helper `_desktop-ssh` or native harness process. Native owns its integrated expanded-run evidence (`job-9451ae57`). The remote daemon/HTTP fixture and local helper directories are deliberately held for root-requested standalone-human-preview follow-up, pending native's explicit final release. HTTP fixture lifetime is bounded to one hour (about 15:59 UTC).

### Exact literal family scope strengthening — 15:26 UTC

Native review correctly identified that the original Phase-0 alias test mapped explicit IPv4 and IPv6 logical URLs at a given approved port to the **same pinned approved remote literal target**. It never contacted an unapproved target, but silently remapped a literal URL's address family. Tightened this policy in both the proxy and `PreviewEnvironments.assertURL`: `localhost` follows the approved remote family; explicit `127.0.0.1` and `::1` must match the approved literal family, including at the exact same port. Updated old alias-positive tests and proof labels accordingly rather than claiming the earlier cross-family alias behavior remains current.

`job-d6288fcd` passed desktop TypeScript, **3/3 proxy tests**, **116/116 desktop tests with no skipped real-SSH tests**, and the real Electron deterministic-endpoint proof. New proxy regression checks both mismatch directions for HTTP, CONNECT, and WebSocket and asserts the denied request never calls even the authorized route opener. Environment scope assertions cover both directions and preserve `localhost`. Electron checks same-port cross-family fetch denial with zero local-decoy hits. Native was asked to include both exact-same-port mismatch directions in its next actual selected-host run; that integrated recheck is distinct from this deterministic regression.

### Root-authorized HTTP-only lifetime extension — 15:44 UTC

After native explicitly confirmed every acceptance consumer/master idle, root-authorized `job-66521beb` (exit 0) gracefully stopped only the owned Python HTTP fixture after checking its exact `/proc` command line, then restarted it with the **same three ports and marker**. The daemon, runtime home, credentials and configuration were untouched. The replacement has a bounded 3,600-second lifetime, ending approximately **16:44 UTC**.

- Intentional HTTP gap: **0.904 seconds**, 15:44:14–15:44:15 UTC. This was coordinated fixture maintenance, not a feature failure.
- Counter epoch 1 ended at `{"approved":77,"unapproved":7}` after native's explicitly approved expansion/human follow-up runs. Epoch 2 started at zero; counters must not be combined without retaining this boundary.
- Runtime re-initialization independently confirmed unchanged `5a71387ffd0e47c7a15ce14bc51aae22`.
- The remote helper SHA-256 exactly matches the Linux helper built at **14:56:07 UTC**: `15d9b4e895b2ac7739425c5cc934a02cddd3c1055a99f49e850db80a77b82994` (baseline `e261d9b5139d85ade9837eedccb8e9bdd7aaa5f1`, modified worktree, Linux/amd64, CGO disabled).
- Native's subsequent full-SDK fixture attempt hit unsupported `browser.provider.unbind`. Read-only inspection confirms that method text is absent from this earlier helper but present in the current daemon source/schema. Root and native were notified of the stale fixture binary. No daemon replacement/restart was performed without further coordination; the mismatch is an explicit pending integration-fixture refresh, not a claimed product pass.

### Root-authorized frozen daemon fixture refresh — 15:50 UTC

After native's explicit diagnostic `SAFEIDLE`, rebuilt the current frozen worktree for Linux/amd64 (`CGO_ENABLED=0`), retaining baseline and modified-tree provenance. Build `job-b29acac5` completed at **15:49:07 UTC**; strict known-host upload `job-00b98914` verified the identical remote SHA-256:

`b0e7d9bb896d4d3785bc0fc3c0d3ef13bfad31869b7812c7c180f1b56877e19f`

Only the isolated test daemon was gracefully stopped and started, after checking its exact process executable, isolated socket/home, old hash, and old initialize response. The previous binary and complete before/after initialize/status records were preserved. New daemon start **15:50:56 UTC**, generation **2** (previously 1), network listener still disabled. Persistent runtime ID remained **`5a71387ffd0e47c7a15ce14bc51aae22`**, so canonical manifest identity did not change.

Actual initialize now advertises `browser.provider.bind`, `browser.provider.event`, `browser.provider.unbind`, and `browser.command.result`. The HTTP epoch-2 process, ports, marker, and deadline remained unchanged; independent counters at **15:51:19 UTC** were zero/zero before the new acceptance run. Native was released only for a fresh root and explicit provider selection, never old-scope replay.

Maintenance audit detail: restart job `job-66573cfd` successfully applied the intended restart, then a postcondition incorrectly named `browser.provider.result` instead of the real `browser.command.result`. Effects were inspected, not repeated. Read-only follow-up `job-1b73a2e5` passed all corrected postconditions and wrote the after record. Full SDK and packaged acceptance were still pending when this entry was recorded; daemon readiness alone is not an acceptance pass.

### Packaged human-preview attempt: independent zero-delta counter evidence

Root reported successful saved-SSH connection through the signed packaged Beta's ordinary UI using the isolated executable/home, then a human-preview confirmation that did not display and timed out. Root's pre-attempt counters were approved **3**, unapproved **0** at **16:32:30 UTC**. SSH-lane read-only `job-12b17c01` independently read **3/0 at 16:37:04 UTC**, while verifying the same owned fixture and daemon processes. No HTTP probe was used. Thus this failed confirmation attempt caused no observed remote HTTP request; it is not a packaged-preview pass.

The owned epoch-2 fixture has `stop.wait(3600)` and no reload/control API. Root was told its approximately 16:44:15 UTC deadline cannot safely be extended in place; any further availability requires a separately authorized HTTP-only restart with a new counter epoch. No restart or daemon mutation was performed for this observation. Root/native were handling the narrowly scoped confirmation fix and signed rebuild.

### Root-authorized HTTP epoch 3 — bounded through 17:10 UTC

After root's explicit `SAFEIDLE` and authorization, restarted only the exact owned epoch-2 Python fixture after rechecking its command line. Preserved epoch-2 final counters **3 approved / 0 unapproved** and prior 16:44:15 UTC deadline. Replacement keeps the exact three ports and marker and now uses an **absolute 17:10:00 UTC shutdown deadline**. Daemon PID, generation 2, and persistent runtime identity independently verified unchanged.

- Coordinated HTTP gap: **0.903 seconds**, 16:39:56.878–16:39:57.781 UTC.
- Epoch-3 counters: initial **0/0**, still **0/0** at 16:40:42 UTC read-only verification.
- Restart job `job-fbf341bf` applied the intended HTTP restart, then its final audit compared JSON string generation `"2"` with numeric `2`. Inspected effects rather than retrying; corrected read-only verification `job-22cb12a4` passed. No second restart occurred.
- Root received readiness for the rebuilt signed-Beta ordinary-UI retest. Native owned no GUI actions; root's possible orphan-master cleanup remained separately coordinated. No daemon restart, credentials, SSH configuration, firewall, or unrelated process changes occurred.

### Epoch-3 expiry audit — 17:14 UTC

Root's signed-Beta retest explicitly opened a Loading preview tab at 17:08:02 UTC, but root observed no fixture request through 17:08:54. Root reported an OS sample showing a macOS Keychain wait in the isolated-HOME signed process; this is a reported harness blocker, not a proven SSH defect or packaged-preview pass.

SSH-lane **read-only** `job-650a6389` (exit 0) independently verified at **17:14:43.889 UTC**:

- Epoch-3 final counters **approved 0 / unapproved 0**.
- Owned HTTP process **no longer existed**, with **no listeners on any of its three ports**, following the absolute 17:10 deadline.
- Owned daemon still running as its exact isolated executable; PID/generation **1178227 / 2**, runtime ID unchanged, original 15:50:56 start time, and no network endpoint.
- No HTTP probe, restart, daemon mutation, or configuration change was performed. Root was advised to authorize evidence archival and narrow owned-resource cleanup after its signed app and SSH consumers stop. Cleanup was still pending at this entry.

### Final authorized cleanup — ALL CLEAR, 17:22 UTC

After root explicitly verified its exact signed-Beta/local-daemon/harness consumers stopped and authorized final cleanup, preserved the owned counter epochs, HTTP manifests, daemon before/after provenance, all three fixture scripts, local manifests, helper SHA-256/build metadata, and final state/audit records in retained local evidence:

**`/tmp/whip-preview-ssh-evidence.7iFpmT`**

Remote records archive SHA-256: **`783cfae2cd97f513b4fa6549de91be5e227f9d6458f3be3d20f8e8f00aac98ca`** (`remote-records.tar.gz`, 12 explicitly selected files; archival job `job-58ee1db0`, exit 0). No credentials, SSH configuration, or unrelated application data were archived or modified.

Read-only pre-cleanup audit `job-72824049` at **17:20:34 UTC** found exactly one test-owned tool-host agent, **idle**, no turns, no schedules, no connected Unix-socket clients, and only the isolated daemon process referencing the owned remote directory. Rechecked its exact executable, binary hash, owner and both isolated home environment variables before cleanup.

Remote cleanup `job-86ae66a7` (exit 0) completed at **17:21:32.028 UTC**:

- Used only the exact owned helper with `WHIP_HOME` and `WHIPCODE_HOME` set to the isolated home; graceful `daemon stop` succeeded, with no force kill.
- Verified daemon PID gone, daemon socket gone, expired HTTP PID gone, no listeners on all three fixture ports, and no remaining process arguments referencing the owned remote directory.
- Removed only **`/tmp/whip-browser-acceptance.Nynor7`** after those checks.
- Final HTTP epoch-3 counters remained **0 approved / 0 unapproved**. Earlier epochs are retained separately (epoch 1 final 77/7 after approved expansion tests; epoch 2 final 3/0).

Local cleanup completed at **17:22:09.372 UTC**, after verifying the retained archive/hash, completed remote cleanup, and no consumer process referencing the exact helper directory. Removed only **`/tmp/whip-preview-kuzco.2jL0pB`**. Retained evidence was not removed. The directory contains machine-readable `remote-idle-audit.json`, `remote-cleanup.json`, `local-cleanup.json`, and final lane state alongside the archive/provenance.

**All owned SSH-lane acceptance resources are cleared.** No HTTP restart, SSH-config/known-host/credential/firewall change, unrelated service mutation, staging, or commit occurred in this cleanup. Connected-host native/SDK acceptance remains distinct from the **environment-blocked packaged human-preview retest**; cleanup does not convert that retest into a pass or an SSH defect.
