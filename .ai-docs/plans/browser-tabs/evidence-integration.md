# Root integration evidence

## 2026-09-17, before packaged acceptance

- Full `go test ./...`: **PASS**, job `job-27f973f9`, 15:22:27–15:24:36 UTC; log `/tmp/whip-browser-go-all-final.log`. Includes corrected exact nine-tool MCP inventory/CLI doctor assertions and actual unpaired Browser open denial. Not a whole-repository race run.
- SDK/protocol suite: **349 passed, zero failed/skipped**, job `job-abf0824b`; log `/tmp/whip-browser-sdk-tests.log`. Includes revoke-before-bind-reply fencing and cancellation identity regressions. Scripted transport tests are not native integration evidence.
- Browser request UI: **22 passed**, `job-ef7a982b`. Agent lane subsequently supplied shared sanitized resource summaries and verified pending event/list/snapshot/collection projections with no remembered rule. Capability/session packages and focused Browser race tests pass (see agent evidence).
- Workspace explicit provider UI/coordinator: lane reports **12 passed**, `job-89781845`; full frontend previously **696 passed / 1 workflow inventory failure**. Root added the four missing Browser RPC classifications; final full frontend rerun is still required after human SSH entry lands.
- Native actual selected-host SSH test includes approved-port expansion and IPv6, with independent counters: denied endpoint **zero before approval, exactly one after expansion**, Mac decoys zero. Detailed native/SSH evidence remains separate from daemon→SDK→native and packaged acceptance.
- Canonical docs updated: `docs/frontend.md`, `docs/desktop.md`, `docs/browser-computer-use.md`, `packages/sdk/README.md`. No new backend permission grants are implied by these docs.

## Screenshot upload provenance regression

Independent cross-layer review found that the generic SDK upload chose HTTP for WebSocket clients. HTTP creates a different request principal, whereas Browser screenshot results must prove upload ownership on the exact selected provider connection. This defect was hidden by the previous unit test's mocked `client.upload`.

Root corrected the shared internal upload helper to accept a connection-bound transport override, used only by Browser screenshots. Both Unix/custom and WebSocket providers now use the existing `upload.begin`/`upload.chunk`/`upload.finish` flow with digest checks, transfer limits and connection-epoch fencing. Public generic uploads retain their existing behavior; no new endpoint or fallback was added.

Replaced the mocked screenshot test with actual scripted RPC transfers for both transport kinds: exact root/agent, digest, ordered three-chunk body, zero HTTP calls and no raw bytes in the Browser result envelope. SDK build and suite **350 passed / zero failed/skipped**, `job-8407e628`, 15:32:37–15:32:48 UTC; log `/tmp/whip-browser-sdk-provenance-tests.log`. A separate real-daemon WebSocket provenance regression is being added; it is not yet claimed passed.

## Native acknowledgement and observation lifecycle review

Independent review found an unbounded native selection wait, unbounded retained event Promise chain, and whole-provider teardown on a routine retired-attachment event. Root fixes reuse the SDK's existing abortable Promise helper: one bind/ACK timeout, abort/disconnect/revoke settlement, exact-epoch asynchronous cleanup and late-ACK cleanup that cannot release a replacement. The event path now snapshots payloads, waits for native ACK, caps 64 queued events / 2 MiB, bounds each acknowledgement to ten seconds, and aborts draining on provider release. Overflow remains a visible fail-closed condition; no live sequence is silently discarded.

Only server `RpcError` kind `browser_event_stale` retires that exact tab/attachment generation queue. All other event failures still fail closed. The Go lane is implementing this narrowly scoped broker error only after validating holder/root/provider epoch, plus tests and corrected proposed-versus-approved preview-port consent wording.

SDK suite **358 passed / zero failed/skipped**, `job-3c39e3b9`, 15:39:06–15:39:17 UTC; log `/tmp/whip-browser-sdk-review-tests.log`. New cases cover four missing-ACK interruption modes with a late ACK after replacement, stalled event count/byte limits, retired A with still-active B, and a non-stale error. A subsequent tightening from generic `WhipError` to server-only `RpcError` is included in the final check rerun. Desktop/frontend integrated check is in progress (`job-5d2458f4`).

## Dependency audit triage (not a clean audit or release waiver)

The inherited lockfile reports **43 findings: 3 low, 14 moderate, 25 high, 1 critical** (`/tmp/whip-browser-tabs-audit.json`). This feature has not changed the lockfile or added dependencies.

The six advisory-bearing leaves are `decode-uri-component` (moderate), `extract-zip` (high), `image-size` (high), `tar` (critical), `tmp` (high), and `uuid` (moderate). Most higher-level findings are propagation through Electron Forge/build dependencies and Expo/mobile tooling, not separate vulnerabilities.

`tar@6.2.1` is marked development-only in the lockfile and is pulled through `cacache@16.1.3`, `@electron/node-gyp@10.2.0-electron.1`, and `@electron/rebuild@3.7.2`. Advisories include archive path traversal and parse/decompression denial of service. No Browser page request is intentionally routed to these packaging dependencies. This reduces the new feature's direct exposure, **not** the risk to dependency installation, CI or packaging untrusted archives. Packaged bundle reachability still needs artifact inspection.

Npm's offered automatic fix includes downgrading Forge 7 to 6 and major Expo changes. No blanket `npm audit fix --force`, transitive major override, or untested downgrade was applied. Dependency remediation remains separate unresolved release-security work; keep packaged Browser opt-in until acceptance and security decisions are complete.

## Integration revalidation — 2026-09-17 16:05 UTC

These results supersede the earlier pending suite statuses above, not the remaining release gates.

- Full frontend `npm run test:web`: **709/709 passed**, `job-d6a3f073`, 16:04:30–16:04:49 UTC, `/tmp/whip-browser-frontend-final.log`, including the strict restore payload regressions.
- Full `go test ./...` after structured-stale-event and consent changes: **PASS**, `job-73b15b80`, 15:58:20–15:59:44 UTC, `/tmp/whip-browser-go-all-post-review.log`.
- SDK check/build: **365 passed, zero failed/skipped**, `job-ba8dd0a7`, `/tmp/whip-browser-sdk-cancel-after.log`. Two deterministic tests reproduced the late-result cancellation/retired-selection failure before the guarded catch fix; five uncancelled/mismatched-cancel negative cases remain fail-closed. No retry was added.
- Production-daemon real WebSocket screenshot regression: **1 passed**, `/tmp/whip-browser-sdk-websocket-after-cancel.log`. Actual chunk RPCs, zero HTTP calls, correct root/agent ownership and cross-root read denial; native screenshot bytes are synthetic in this specific test.
- Full **development-native** SDK → desktop transport/preload/IPC → current test-owned SSH daemon → visible-page implementation: **PASS**, `job-bad6fb5e`, `/tmp/whip-browser-preview-native-sdk-full-evidence.json`. Actual external denial/Once approval, immutable SSH scope, 54,541-byte real Go-helper/CDP JPEG, owning-connection upload/digest and foreign-root denial. Timeout reports outcome-unknown, sends exact cancellation, and dispatched delayed effect executes once without replay. Detach, explicit unbind and root deletion preserve the human guest; SDK error list and both Mac-decoy counters are empty/zero. This is not packaged acceptance.
- The first remote full-path diagnostic used an obsolete owned daemon without `browser.provider.unbind`; a separate fixture typo passed `preview.id` instead of `host_id`. Both failures were retained, not treated as production fallbacks. After explicit authorization and native SAFEIDLE, only the test-owned remote daemon was rebuilt/restarted from frozen source; actual initialize methods were checked before retry.

## Signed-package primary failures and follow-up

The first signed Beta snapshot verified Developer ID/team, shipping fuses, packaged renderer/native digests and **notarized=false**. Its ordinary native UI disabled-feature menu check passed with an explicit `WHIP_DESKTOP_BROWSER_TABS=0`; the later harness omits the variable for a genuine packaged-default check. Existing long-lived computer automation could not discover newly launched Beta; a fresh test-owned bundled signed Accessibility helper used already-granted permissions instead. No CDP, injected bridge, altered fuse or patched app was used.

Ordinary opt-in UI exposed a real restore boundary defect: `BrowserWorkspace.sync` forwarded the workspace `kind` discriminator to the strict native `BrowserRestoreTab` API. Evidence: `/tmp/whip-browser-package-ui-evidence/1789660734455-click.json` and companion native screenshot. The explicit metadata projection is fixed; two new tests failed before the fix, then targeted frontend **46/46**, app TypeScript and web production build passed (`job-3e0ab8ac`). The native validator remains strict. The first real renderer/preload/IPC seam run passed that seam but subsequently failed a focus assertion. The bounded full rerun (`job-0c5dc731`) passed with `NATIVE_MANAGER_OK` at approximately 16:08:15 UTC; changes between runs were test-only focus/window logging and a 30-second watchdog. The original timing/activation failure remains unexplained, is not attributed to Beta, and is not claimed fixed.

First signed fixture cleanup exceeded its initial SIGTERM grace; a second fixture also exceeded 30 seconds and had no accessible window. Its exact marker/PID was verified before forced cleanup. These runs **do not establish graceful quit/restart**. Primary evidence is retained at `/tmp/whip-browser-package-{eTp8Bz,BSSBFk}-evidence.json`, with separate forced-cleanup record. The harness now explicitly records forced cleanup instead of calling it a graceful restart.

The replacement signed Beta containing the SDK cancellation fix and workspace projection completed successfully at 16:06:44 UTC (`job-5aa9f8ca`, `/tmp/whip-browser-package-fixed.log`). Root checks, desktop TypeScript and desktop tests also passed (`job-254844fc`, **92 tests**, `/tmp/whip-browser-desktop-final.log`). These results predate the further confirmation fix below.

Its isolated opt-in fixture (`job-8f189f1f`, `/private/tmp/whip-browser-package-K2t3Hh`) showed a realized Browser guest. Early address/focus observations were not isolated: the user confirmed accidental simultaneous interaction at approximately 16:26 UTC, including navigation outside the test sequence. Those observations do not establish a product focus defect or a clean packaged navigation pass. A fresh signed helper, exact executable-path selection and delayed fresh Accessibility snapshots were used; no application source or shipping fuse was changed for automation.

After exclusive desktop access was confirmed, ordinary UI saved and connected the exact test SSH profile on `sam@kuzco-4090`, with the test-owned executable/home and no SSH configuration changes. The human preview picker selected that saved host, the test project and literal `http://127.0.0.1:40129/`. At 16:33:01 UTC, continuing showed “Waiting for SSH preview confirmation…” but no usable approval prompt; at 16:35:12 it showed “SSH preview was not opened.” The remote counters remained **approved 3 / unapproved 0** before and after (`job-16d16ca6`, `job-12b17c01`): no new remote HTTP request was observed. Primary Accessibility evidence: `/tmp/whip-browser-package-ui-evidence/{1789662781175-click,1789662912714-state}.json` and companion screenshots.

Source review confirmed a mismatched confirmation path: Browser preview reused the SSH authentication prompt with an already-connected connection ID, while the host-prompt UI treats the transaction as connection authentication/completion. A targeted dedicated main-owned native confirmation fix and regression are in progress. A fresh signed rebuild and isolated acceptance are required; this timeout is not a successful denial/approval test. The old Beta was stopped before further native UI work. Neither its ineffective keyboard quit attempt nor watchdog cleanup establishes graceful restart. Retained `/private/tmp/whip-browser-package-K2t3Hh-evidence.json` records marker-verified forced stop at 16:38:04 UTC and complete fixture cleanup at 16:41:35 with no cleanup errors.

For the next signed retest, only the owned remote HTTP fixture was restarted after SAFEIDLE: epoch 2 final counters 3/0 were archived, epoch 3 starts at 0/0 with the same ports/marker, a recorded 0.903-second gap, and a 17:10 UTC deadline. The owned daemon PID/runtime were verified unchanged. This is test-fixture maintenance, not a production fallback or replay.

## Dedicated native confirmation and fresh default-OFF quit regression

The dedicated Electron-main confirmation adapter is implemented and re-frozen.
The native lane reported 127 desktop tests (117 pass, 10 opt-in SSH skips), clean
typecheck, and a full real Electron run (`job-369ff53f`, `NATIVE_MANAGER_OK`).
The new test observed a real parented native sheet and AbortSignal cancellation;
it is not evidence of packaged human approval. SSH authentication is unchanged.

Root rebuilt the signed Beta with this fix (`job-8ec39399`, 16:45:57–16:48:19 UTC)
and ran a fresh isolated shipping-fuse fixture (`job-de6dbf79`,
`/private/tmp/whip-browser-package-FdrKzl`). With the opt-in environment variable
**absent**, native Accessibility showed the normal workspace and no Browser-tab
entry point. Root then activated the exact owned Beta PID 63344 and issued one
Cmd+Q at 16:51:43 UTC. Unlike earlier uncertain keyboard observations, this reached
quit and produced a real native critical alert: `TypeError: Object has been
destroyed`, from `BrowserManager.dispose`. Offline inspection of the signed
`main.cjs:5:10129` mapped the error to a cleanup closure rereading
`window.webContents` after the BrowserWindow closed event. This is a confirmed
acceptance blocker, including when the Browser feature is disabled.

Evidence: `/tmp/whip-browser-package-final-default-{loaded,quit}.log`, native
Accessibility/screenshot `/tmp/whip-browser-package-ui-evidence/1789663905698-press.{json,jpg}`.
One native OK action dismissed the alert; its subsequent AXWindows read failed
after the process exited. PID absence was checked before the 16:53:18 UTC
all-clear. The fixture evidence records cleanup at 16:53:07 UTC with no errors,
no forced stop, and zero fixture page/click requests. This is **not** a clean
graceful-quit pass. The native lane has a narrow lifecycle-fix thaw plus
no-guest/live-guest real-window-close regressions; another signed rebuild and
packaged retest are required. The remote daemon and HTTP epoch 3 remain unchanged.

## Lifecycle fix and remaining packaged keychain blocker

The lifecycle fix is now re-frozen: `BrowserManager` captures the registered
window WebContents once and removes listeners from that object instead of
rereading a destroyed BrowserWindow. No exception suppression was added.
Desktop typecheck and the full real Electron native suite passed (`job-72fa7781`),
including deterministic destroyed-getter cleanup and actual disabled/empty and
enabled/live-guest window-close regressions without pre-dispose. The complete
non-GUI rerun (`job-0544c218`) passed 117 desktop tests with 10 opt-in SSH skips,
92 distribution/release tests, and the startup self-test.

The replacement signed Beta build (`job-bd97f3b1`, started 16:55:49 UTC) has
bundle digest `63428b55885dd8fd533785c84f01bc35c81f0d6af607ad28771e4bf08f18079f`
and renderer digest `07176617243c3fc8ad283b90d4352d7c8e49f9b94f2654ddaf071bfa33989bb6`.
Root ran the unchanged shipping-fuse bundle in isolated fixture
`/private/tmp/whip-browser-package-ONYGcu` (`job-1e79b5ab`). With the opt-in
environment variable absent, Accessibility showed New session tab and no New
Browser tab. One Cmd+Q reached process exit: PID 95994 was absent at 16:59:26,
without the previous destroyed-object alert or forced cleanup. The helper's
post-key AXWindows read failed after exit; that helper error is not an app error.
This establishes only the observed default-OFF exit, not enabled restart.

The enabled launch (PID 96133) connected the saved exact test SSH profile using
the owned remote executable/home. The dedicated native confirmation sheet
appeared. Explicit Cancel was clicked at 17:06:22 UTC; remote epoch-3 counters
were 0 approved / 0 unapproved at 17:06:42. A later explicit Open preview at
17:08:02 created an SSH preview tab, but it stayed Loading and counters remained
0/0 at 17:08:09 and 17:08:54, before the HTTP fixture's 17:10 deadline. This is
**not successful packaged remote loading**. The Return/default-button probe is
inconclusive; no default-Cancel acceptance claim is made.

Cmd+Q at 17:10:40 removed the accessible window but did not end PID 96133. An OS
sample at 17:13:03 observed a foreground worker in all 1,734 samples inside
`SecItemAdd -> SecKeychainItemCreateFromContent -> defaultKeychainUI ->
makeLoginAuthUI -> AuthorizationCopyRights`. This is a genuine macOS keychain
creation/authorization wait. Stripped Electron frames do not prove the cookie
callsite or that the fixture's isolated HOME is the sole cause. No encryption
bypass, mock keychain, real-user keychain access, CDP, injected bridge, or package
patch was used. A faithful repeat under a separately authorized disposable
logged-in macOS account/VM with its own initialized keychain remains outstanding;
changing HOME alone is not proof of keychain isolation.

The owned harness recorded a forced stop after SIGTERM grace at 17:14:55 UTC;
this is **not graceful enabled-app quit/restart evidence**. Final cleanup at
17:16:16 had no errors. Beta PID 96133, fixture daemon PID 95990 and harness
PID 95968 were verified absent; the isolated fixture directory was removed.
Local fixture counters remained zero. Primary retained evidence:
`/private/tmp/whip-browser-package-ONYGcu-evidence.json`,
`/tmp/whip-browser-package-lifecycle-hang.sample.txt`, and
`/tmp/whip-browser-package-lifecycle-{default-loaded,default-quit,native-consent,
after-denial,preview-approved,after-approval,preview-loaded,approval-wait,
enabled-quit}.log` plus their native Accessibility snapshots/screenshots.

## Final owned-resource cleanup and source review

The SSH lane completed final cleanup after an independent SAFEIDLE audit. Its
exact isolated daemon stopped gracefully, without force; daemon/socket/HTTP
process/listeners were verified gone before removing only the owned remote
fixture at 17:21:32 UTC. The owned local SSH helper directory was removed at
17:22:09 after checking for consumers. See [SSH evidence](evidence-ssh.md).
Root verified the retained archive at `/tmp/whip-preview-ssh-evidence.7iFpmT/remote-records.tar.gz`
has SHA-256 `783cfae2cd97f513b4fa6549de91be5e227f9d6458f3be3d20f8e8f00aac98ca`.
No acceptance services remain; evidence archives are intentionally retained.

Final read-only source reconciliation identified the same 184 intentional paths
(91 modified, 93 new), no suspected live credentials in the heuristic scan, and
54 valid added relative links in current guides. Root corrected the one extra
EOF blank line. Exploratory focus scripts, generated build outputs, runtime
profiles, screenshots and machine-specific credentials are excluded. This is
review for an **experimental default-OFF source commit**, not a release waiver;
source review and cleanup do not complete the missing packaged gates.

## Consented normal-HOME packaged local acceptance — 2026-09-17 21:58–22:04 UTC

The user explicitly authorized the signed Beta to use the current macOS account's
normal HOME/keychain, while keeping WHIP data, the browser profile, daemon home,
and temporary files disposable. `browser-packaged.mjs --enabled --user-home`
selects this mode explicitly; without `--user-home`, the isolated-HOME default is
unchanged. The user handles any OS credential prompt. No credential contents were
inspected, keychain configuration changed, or cookie-encryption fuse disabled.
The same signed shipping-fuse lifecycle bundle above was reused, not rebuilt,
patched, or instrumented with CDP/an injected bridge.

The earlier normal-HOME fixture `wv176m` reached a local 404, but extra input had
changed the address. It is not a clean navigation pass. Its 45-minute fixture
lifetime expired while waiting for exclusive desktop use; cleanup completed at
21:11:51 with no errors. An attempted activation after that expiry sent no UI
input. A fresh fixture was started only after the user granted exclusive keyboard
and mouse use.

Fresh fixture `JFuhwB` (`job-f3978db9`) used local URL
`http://127.0.0.1:55836/` and marker
`2a2a7975-da54-4735-9c7d-30aa275e32d8`. Ordinary native Accessibility interactions
and exact-address assertions produced these observations:

- PID **40970** rendered **Native Browser acceptance**, the exact fixture marker,
  and its input/button/link controls. At first render the server counted **1 page,
  0 clicks**. Typing left `native-input-check` in the visible fixture input; one
  Increment click produced exactly **1 server-side click**.
- Opening **Second fixture page** changed the Browser address to `/second`; Back
  returned to `/`. The local check (`job-41e58f66`) completed at **21:59:52** with
  `LOCAL_RENDER_PASS`, `ONE_CLICK_PASS`, and `NAVIGATION_PASS`. The visit counter
  was **3** before quit (including the back navigation).
- Cmd+Q at **22:00:01** exited PID 40970, independently confirmed absent before
  relaunch. No stop command, SIGTERM, or forced kill was needed for that exit.
- The harness relaunched the same disposable profile as PID **41660** at
  **22:00:11**. The saved Browser tab restored at the same URL and rendered the
  same marker. The visible localStorage visit counter became **4**, demonstrating
  profile continuity. Server totals were **3 pages, 1 click**: back navigation
  need not cause another HTTP request, and the click was not replayed.
- The first post-relaunch Accessibility snapshot was not yet hydrated; a later
  read-only snapshot showed the restored controls. An assertion on the initial
  snapshot failed before sending any further input. This sampling interval is
  **not** a measured startup latency or a product restore failure.
- Cmd+Q at **22:02:54** also exited PID 41660; both app PIDs were confirmed absent.
  Both post-quit helper AXWindows reads failed because the app had exited; process
  absence, not the helper's exit status, establishes quit. The harness's final
  cleanup completed at **22:04:04** with **no errors and no forced-stop record**.
  The owned directory and local HTTP listener were absent. No remote fixture was
  recreated during these local checks.

Evidence: `/private/tmp/whip-browser-package-JFuhwB-evidence.json` records signed
verification, `userHome: true`, both launch PIDs, final counters and cleanup.
The focused UI logs, final Accessibility state, helper sequence and 16 app-scoped
screenshots are retained in `/tmp/whip-browser-local-evidence.y4erlW.tar.gz`
(SHA-256 `2faa4d43927c772310332080eaced1362f9e1f37133ee43e67ba9c57e5052dc6`).
The bundle still reports **notarized=false**. These observations establish local
packaged loading/input/navigation and enabled quit/restart/restore for this
consented environment only. They do not prove the exact cause of the earlier
Security.framework wait, isolated-keychain behavior, SSH loading/recovery,
packaged agent-control acceptance, multi-pane/focus coverage, or performance.

## Consented packaged SSH acceptance — 2026-09-17, 22:34–23:03 UTC

The user authorized an exclusive, bounded desktop run and the existing normal-HOME
mode. Root used ordinary native Accessibility input against the **same Developer
ID-signed, shipping-fuse Beta** as the local run above; no CDP, injected bridge,
package patch, encryption bypass or credential inspection. App data and Browser
profiles remained disposable. GUI use ended at **22:56:10**, before the 23:04:50
limit; the desktop was returned to the user. This is human preview evidence, not
packaged agent-control acceptance.

Fixture `Lrqo19` used the saved entry **Browser SSH acceptance Lrqo19**, ID
`ssh:1d57918e-f1e4-44b2-af36-fcf7984d60e7`, on `sam@kuzco-4090`. The native sheet
identified runtime `86e575f45b2faa250ed875d0a4f6a45d`, project
`/tmp/whip-browser-acceptance-final.OrWYJW/project`, literal **127.0.0.1** and
approved port **40711**. Persisted native identity correctly normalizes the
project to `cwd:/tmp/whip-browser-acceptance-final.OrWYJW/project`; it is not a
second project. The one environment profile remained
`d2fef634781792c6184a2bc4741cfe091e5f479891bf68c5` across reconnection.
The SSH lane independently owned the remote daemon/HTTP fixture, detached
watchdog, counter reads and six exclusive Mac decoys (IPv4 and IPv6 on each of
40711, 33671 and 37315). The Linux fixture was built from clean `57e045bad`.

| Ordinary packaged observation | Independent request evidence |
| --- | --- |
| Cancel first native confirmation | Remote **0 approved / 0 unapproved** before and after, through 22:45:05 |
| Approve exact IPv4 scope, render the remote marker | Remote **1/0** at 22:46:15; visible marker `remote-preview-bdebb36510f87e5fc827646ee117c07b` |
| Navigate to `/cookie`, then `/inspect` | Remote **3/0**; visible JSON has logical Host `127.0.0.1:40711`, synthetic cookie, and null `authorization` / `proxyAuthorization` |
| Cancel additional-port 33671 confirmation | Remote **3/0** both before and after |
| Submit unapproved port 33671 and opposite-family `[::1]:40711` addresses | Remote still **3/0** at 22:49:12; no claim about exact error-message timing |
| Return to approved address, then close only the verified Beta-owned SSH master | Remote **4/0**; old page becomes unavailable and Retry remains blocked |
| Automatic host reconnection, followed by explicit disconnect/reconnect of the same saved entry | Old preview still unavailable; remote remains **4/0** through fresh-confirmation baseline 22:52:50 |
| Approve the new native confirmation | Remote **5/0** at 22:53:19; exact remote marker visible again |
| Inspect after renewed approval | Remote **6/0**; the same project cookie and logical Host remain, with both authorization headers null |
| Quit and relaunch the same disposable profile | Two saved SSH tab descriptors restore; the selected preview is **unavailable**, with explicit no-This-Mac-fallback text; remote stays **6/0** at 22:55:28 |

**All six Mac decoys remained TCP 0 / HTTP 0 throughout.** Their successful live
binds were independently checked before stopping them, including same-port
opposite-family listeners. Positive packaged loading was **IPv4 only**; this run
does not establish positive packaged IPv6, WebSocket/SSE, service-worker, port
expansion, agent-control or default-button acceptance. Earlier native/SDK tests
remain separate evidence. The sole seeded remote conversation was an idle inert
`tool_host`, with no model/provider, model turns or schedules.

Both Beta launches exited after ordinary **Cmd+Q**, without a stop/force command:
PID **53694** was confirmed absent at 22:54:34, before relaunch as **57948** at
22:54:35; the latter was confirmed absent at 22:56:10, before harness cleanup.
Post-quit Accessibility reads returned an absent-window error; process absence
was checked independently rather than interpreting that helper exit as failure
to quit. Harness `job-bfe35b87` finished at 22:56:11 with no cleanup errors and
removed `Lrqo19`. Local fixture HTTP/click counters stayed zero.

After root confirmed no consumers, the SSH lane independently sampled final
remote **6/0** at **23:01:03**, no daemon TCP listener and no Unix clients.
Early exact-owned watchdog cleanup finished **23:01:51**: graceful daemon stop
code 0, `errors=[]`, `forced=[]`, `ownedAfter=[]`, socket and remote directory
removed. Independent checks confirmed watchdog/daemon/HTTP PIDs and all three
remote listeners gone. All six Mac decoys stopped gracefully at 23:01:44; their
helper directory was removed at 23:02:41. Residual remote evidence was removed
at 23:03:08 **only after** local archival and matching hashes. The watchdog and
its follow-up schedule are settled; regular Whip, unrelated SSH/services and
credential/configuration stores were not changed.

Retained local evidence (not repository payloads):

- `/tmp/whip-browser-ssh-ui-evidence.ViJI6T`: 75 UI/counter logs, 122 screenshots,
  native profile metadata, harness/cleanup records and the ordinary-input helpers.
  `node .../check-evidence.mjs` rechecks 12 recorded counter samples, visible
  markers/cookie/Host, fail-closed states, restored descriptors and harness records;
  `assertions.json` records **PASS**. This checks retained observations, not a new
  UI execution. Archive `/tmp/whip-browser-ssh-ui-evidence.ViJI6T.tar.gz`, SHA-256
  `170c144725d22d38f878e23b1a273fdd8521302ff2b51e9fa6b3e11637957beb`.
- `/tmp/whip-preview-ssh-final-evidence.Z9MSfl`: independently collected fixture,
  listener, idle-catalog and cleanup evidence; its README indexes the records.
  `remote-evidence/remote-records.tar.gz` SHA-256
  `e95e2e98caffdad851397a0ff153a9f87b1588f114a5e29c0a4247c0652be34e`.
  Final saved/native association is in `local-fixture-records/manifest.json`;
  the remote bootstrap manifest predates UI save and is not the final binding.
- `/private/tmp/whip-browser-package-Lrqo19-evidence.json`: signed bundle
  provenance, normal-HOME mode, both launches and error-free local cleanup.
  It still reports **notarized=false**.

## Release gate status

Implementation remains experimental. The user subsequently requested default-ON
Browser tabs; current source enables them in packaged and development builds,
with `WHIP_DESKTOP_BROWSER_TABS=0` as the explicit opt-out. This availability
decision does not waive the remaining release gates. The signed Beta and its
packaged observations above predate that source change; those artifacts have
not been rebuilt or relabeled as default-ON acceptance. Substantial
development/native/selected-host tests have passed.

Default-ON source validation: desktop **118 passed / 10 opt-in SSH skips**,
distribution/release tests **92 passed**, startup-probe self-test, desktop
TypeScript and packaged-harness syntax passed. Eight fail-before-effects CLI
checks covered default/explicit modes and conflicting/duplicate/unknown flags.
No GUI, SSH fixture or packaged acceptance run was started for this change.
The harness now leaves the override unset for default launches, sends `0` for
`--disabled` (or `enabled:false` commands), and records the chosen override.

Local enabled loading, input/navigation and graceful quit/restart/restore pass
in the explicitly consented normal-HOME environment. Packaged human SSH IPv4
loading, negative-network checks, fresh-approval recovery and fail-closed tab
restore now also pass, with final owned-resource cleanup verified. Phase 7
packaged acceptance is still incomplete: packaged agent control and remaining
manual/performance gates are not established.
The original development focus flake remains unexplained; contaminated earlier
packaged focus observations are not product-defect evidence. The dependency
audit still reports 43 findings (3 low, 14 moderate, 25 high, 1 critical); no
clean security audit or release approval is claimed. Notary configuration is
absent; the Developer ID signature does not establish notarization. No remote
debugging port, altered shipping fuse or injected bridge is allowed for packaged
evidence, and no disposable macOS account/VM has been created or authorized.
