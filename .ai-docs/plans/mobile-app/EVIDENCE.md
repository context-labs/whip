# Mobile implementation evidence

Branch: `mobile-app`

Worktree: `/Users/samheutmaker/Desktop/context-labs/src/rlm/whip-mobile-app`

## Current state

Implementation is underway in this worktree; the original `whip` checkout remains
on `whip-rlm`. Native device and release gates are not complete.
The [phased plan](IMPLEMENTATION.md) is the delivery checklist.

The owner reports the signed preview running on the iPhone. The correct remote
host is `gpu-4090-sam` (`sam@kuzco-gpu-2`), now serving
`https://kuzco-gpu-2.tail7524e6.ts.net`. HTTPS certificate verification, SDK WSS
initialization and mobile-facing reads pass from the Mac. The runtime is fresh
and still needs provider credentials. Physical-phone workflows remain open.
The earlier `kuzco-4090` preparation below is historical and was rolled back.

## Baseline preparation

- Created branch `mobile-app` from `whip-rlm` commit
  `dd7aaa3a7f9b8c00bd4ec095b978def1c9231805`.
- Copied the 24 tracked, uncommitted question/protocol/web changes so the native
  work begins from the source reviewed in the plan. They remain uncommitted.
  The [baseline record](worktree-baseline.json) names those files and records the
  patch digest to distinguish inherited changes from later mobile work.
- Copied the architecture, research and phased-plan documents. Other projects'
  untracked plans and browser artifacts were not copied.
- Verified the copied tracked patch before and after dependency/build checks.
- Installed independent workspace dependencies with `npm ci --no-audit --no-fund`.
- `npm run build`: passed; shared SDK artifacts are available.
- `npm run check:web`: passed, including app type checking and production build.
  Vite reports its existing large-chunk advisory; this is not a build failure.
- `npm test`: passed all 195 SDK tests with no failures or skipped tests.

No daemon was started/restarted and no network mapping, authentication or push
service was configured. This describes preparation only; implementation progress follows below.

## Local tooling inventory

| Tool | Observed setup |
| --- | --- |
| Node / npm | 24.14.1 / 11.11.0 |
| Go | 1.27.0, darwin/arm64 |
| Task | `/opt/homebrew/bin/task` |
| Xcode | 26.3, build 17C529; selected developer path is `/Applications/Xcode.app/Contents/Developer` |
| CocoaPods | Available at `/usr/local/bin/pod` |
| Android | SDK environment configured; `adb` available under `~/Library/Android/sdk/platform-tools` |
| Watchman / EAS CLI / Android emulator command | Not on PATH during preparation |

This inventory does not establish compatibility with the eventual Expo native
build. Phase 0 checks required Xcode/SDK versions, build credentials and actual
physical-device availability before claiming native acceptance. EAS CLI need not
be installed globally; use the implementation's chosen reproducible invocation.

## Implementation progress — 2026-09-07

Implemented, with native acceptance still pending:

- Expo 57 / RN 0.86 workspace, Expo Router and Expo UI, FlashList, Enriched
  Markdown, keyboard controller, SQLite/SQLCipher and SecureStore. Mobile uses
  Expo's TypeScript 6.0.3; the existing web/tool packages retain TypeScript 5.9.3.
  One React 19.2.8 instance is shared; Expo's expected 19.2.3 is a deliberately
  excluded patch-version check, still subject to native runtime validation.
- Pure app presentation/theme subpaths; web timeline imports the shared projection.
- SDK instance crypto providers; reversible pause/resume; regression fixes for
  close-between-status-and-tick, aborted resume, reentrant pause and catalog timers.
- Encrypted async storage with native backup exclusion, key/schema checks,
  metadata-only command+intent transactions and separate permission records.
- Native shell/server setup, Sessions and Attention queries, themes, root/child
  conversation, message actions, bounded content sheet, request sheets and durable
  three-step creation journal. These need rendered/device verification.
- Permission status API and daemon compatibility fixes for both old success
  tickets and legacy plaintext failures; no new wire schema.

Observed validation:

| Check | Result |
| --- | --- |
| SDK build and test suite after permission work | Passed; 223 tests |
| Full daemon package tests after permission work | Passed |
| Targeted daemon permission/status race tests | Passed |
| Native storage tests (real SQLite plus mocked native setup) | 15 passed |
| Creation workflow tests | 8 passed |
| Expo Doctor | 21/21 passed |
| Metro production export | iOS and Android Hermes bundles exported successfully |
| Web typecheck/production build after pure extraction | Passed; existing large-chunk advisory |
| Swift module syntax / Expo autolinking | Syntax check passed; iOS and Android discover WhipStorage |
| CocoaPods install | Passed with updated Homebrew CocoaPods; old system CocoaPods/Ruby failed |
| iOS native compile attempt | Could not select destination: iOS 26.2 platform missing; simulator platform download underway |
| Android native compile attempt | Initial JVM 11 rejected; Java 17 installed and build retried |
| EAS account | Not logged in; account/team information requested |
| Physical device UI / cellular / backup / beta distribution | Not verified |

Xcode 26.3 is installed; Expo 57 documents Xcode 26.4+ support. Do not claim native
compatibility from CocoaPods or Metro alone. A paired iPhone 16 Pro is available,
but no iOS development signing identity was found during inventory. Existing
Developer ID Application credentials are not iOS signing credentials.

No live Whip daemon or Tailscale mapping was changed. Native app icons remain
scaffold assets pending the visual/release pass. Local logs are ignored beside
this document; retained evidence must summarize results rather than rely on logs
being committed. Full `task check`, native fixture parity, adversarial runtime/UI
review and all phase acceptance gates remain in progress.

## Runtime and native build pass — 2026-09-07

- `task check`: passed in this worktree after fixing the inherited web question
  focus/final-skip defects and the new portable theme export's Node resolution.
  Includes Go formatting/vet/tests, SDK 223 tests, web 168 tests, UI 11 tests,
  frontend builds and existing packing checks.
- Mobile TypeScript and Jest: 81 tests across 11 suites passed. Coverage includes
  24 storage cases, actual SDK transport with SQLite runtime fixtures, question
  interaction, permission recovery, creation journals, revision-aware reading,
  stale model/effort actions, URL/link policy and diagnostic redaction.
- Added explicit reset confirmation on startup failure and Settings, saved draft
  copy/discard, decision cleanup, server editing/cancellation, draft save status,
  bundled Inter/JetBrains Mono, a source SVG Whip mark and native splash assets.
- Production Metro exports after the additions: both platforms passed (iOS about
  8 MB, Android 8.1 MB Hermes bundles). Exports are not native acceptance.
- Mobile CI is integrated into the required aggregate check. EAS profiles and the
  owner-facing release runbook were validated against current tooling schemas;
  Expo Doctor remains 21/21. The EAS SDK build hook was exercised successfully.
- Installed current CocoaPods, Java 17, Android SDK/NDK/emulator tools and an
  isolated API 36 ARM64 AVD. These are local tools, not repository dependencies.
- Installed iOS 26.2 simulator runtime and checked Xcode setup. The iOS native
  compiler then failed in ExpoModulesJSI's Swift/C++ annotations under Xcode 26.3.
  Expo 57 requires Xcode 26.4+. The temporary simulator matching override used
  during diagnosis was restored to its default. No dependency compiler patches
  or unsupported-platform success claims were introduced.
- Android native build found and fixed missing splash image configuration and an
  unsupported `O_DIRECTORY` reference in the local module. The corrected module's
  Kotlin compile passed; full app build and emulator verification continue.

EAS is still unlinked/logged out. Apple ownership, signing and physical release
verification remain pending. All edits stay in `whip-mobile-app`; original
checkout changes from other work have not been folded into this baseline.

## Android runtime verification — 2026-09-07

Device: isolated `Whip_API_36` ARM64 emulator, Android API 36. App:
`dev.contextlabs.whip.mobile`, local debug native build with Metro/Hermes.
Java 17, SDK/build tools 36 and Expo-selected NDKs were used. Native
`:app:assembleDebug` completed successfully and the APK installed and launched.
This is emulator evidence, not a physical phone or Tailscale/cellular result.

Observed against the isolated fake-provider fixture through `adb reverse`:

- Manual saved-host connection, protocol initialization, session list and root
  history loaded through the existing SDK WebSocket transport.
- A submitted question streamed, appeared in the native request sheet, accepted
  its recommended answer and resumed the daemon turn. The three-page batch
  accepted two selections plus custom text and a skipped final page; committed
  output contained those exact answers and the dismissal.
- Native Compose sheets, `RNHostView`, text input, selection rows and action
  groups rendered. The question form scrolled to its actions with the keyboard
  open. The tool permission sheet displayed the exact test path; Allow once
  completed the fixture write and the request disappeared.
- Host folder browsing and new-session creation with an optional first prompt
  worked. This exposed a retained queued-preview defect when creation finished
  before opening the view; the follow-up fix/tests are recorded below.
- A revisioned draft survived app force-stop/relaunch. Pressing Home closed the
  native test socket after backgrounding; reopening restored the saved host and
  refreshed its sessions. The host's accepted work continued independently.
- A native Hermes probe used the shipped SDK and Expo Crypto adapter to read a
  scoped content reference through streaming HTTP and verify its SHA-256. The
  checked JSON message body was 124,262 bytes. This proves the native transport /
  hashing path; it is separate from full content-sheet UI acceptance.
- SQLCipher created the database in `no_backup/Whip` with private permissions.
  Confirmed reset produced a new encrypted database. Removing only the test
  database while retaining its key failed closed with `database_missing`, kept
  the remaining state and required explicit reset; no automatic regeneration.

Native portability defects fixed during these checks:

- Native WebSocket requires a string URL; React Native declares but does not
  supply `bufferedAmount`. The adapter preserves browser measurements and treats
  an absent native measurement as zero. Strict native queue-byte accounting is
  unavailable; per-frame/in-flight limits remain. The SDK README states this.
- React Native lacks `throwIfAborted` and drops ordinary controller reasons.
  The feature-detected mobile shim publishes the first reason before listeners
  run and installs the missing checkpoint. Tests use the actual native
  `abort-controller` and Expo composition/timeout implementations.
- Universal Expo UI numeric control dimensions replaced percentage button
  widths that crashed Compose. The outer React Native layout still measures
  available width normally.
- Fast Refresh now serializes the prior runtime/storage cleanup before opening
  its replacement. Metro's dispose callback does not provide Webpack-style hot
  data; the pending cleanup is held in one development-only global slot.

The large-message experiment is not a performance pass: a development build
with a 122 KB message showed approximately 683 MiB total PSS after rendering
(the retained row preview is capped at 32,768 characters). This includes Metro /
Hermes development overhead and native libraries, with no controlled baseline.
Long-history memory, expansion, repeated-navigation and physical-device profiling
remain open release gates; do not infer acceptable memory use from virtualization.

Still outstanding: supported-toolchain iOS compile/launch, iPhone and Android
physical-device matrices, real Serve HTTPS/WSS and VPN/cellular transitions,
accessibility/large text/reduced motion, backup/upgrade acceptance, long-history
performance, owner account/signing/store metadata and beta installation.
No production Whip daemon, Tailscale mapping, auth or notification service was changed.

### Preview correction and integrated checks

- Fixed the creation-before-view queued preview using command/recipient/inbox
  identity and bounded fresh-snapshot handoff. It coalesces refreshes, preserves
  queued child inbox entries, handles omitted inbox data conservatively and cannot
  let released/old views confirm new submissions. Definitive failed child sends
  remove their local preview while preserving the unsent draft and failure outcome.
- Native retest: creating a session with its first prompt produced one user and
  one assistant history row, with no extra queued preview. Sending the identical
  prompt again produced a distinct second user/assistant pair.
- Full mobile checks: TypeScript passed; 99 tests across 13 suites passed,
  including 26 runtime cases. SDK full suite: 224 tests passed.
- Final shared `task check`: passed, including daemon/protocol/SDK/web/UI/build
  checks. Updated SDK fixture acceptance with race detection previously passed
  30/30, including both transports and single/batch questions.
- Local Android ARM64 `assembleRelease`: passed (initial build 4m 52s, 949 tasks).
  This uses local development signing, not a Play distribution credential.
  The final-source rebuild/install result is recorded below.
- Tailscale read-only inventory: installed client 1.102.3, backend stopped, no
  Serve mappings. No VPN state or mappings were changed. The Mac app CLI requires
  `TAILSCALE_BE_CLI=1` in automation; see the
  [official CLI reference](https://tailscale.com/docs/reference/tailscale-cli?tab=macos).

Questions are pending for a supported Xcode installation and the owner's Expo /
Apple release account choices. Neither elapsed time nor local build success
supplies signing access or physical-device acceptance.

### Bundled Android installation

The final-source ARM64 release-variant rebuild passed in 1m 14s (26 executed,
923 up-to-date tasks). Installed it over the debug app on the same isolated AVD,
removed the Metro `adb reverse` mapping and launched `MainActivity`. The bundled
app opened its encrypted state without Metro or a developer-menu overlay. Package
flags did not include `DEBUGGABLE`. Its saved development HTTP endpoint was
rejected with “Use the HTTPS address provided by Tailscale Serve.” No production
HTTP exception was introduced.

Artifact: `apps/mobile/android/app/build/outputs/apk/release/app-release.apk`
(about 64 MiB; ARM64 local test artifact, development signing).
SHA-256: `6bd3413abae30d86c8876d77b402b1a50c61d5bdf5f5cddf86058f737f989b8d`.
Native turn-specific Stop also ended the held fixture turn; its cancellation
outcome was surfaced. Screenshot/log artifacts remain ignored local evidence.

After verification, the owned fixture and Metro process were stopped, the two
fixture/development `adb reverse` mappings were removed, and the isolated emulator
was shut down. The installed test app and generated APK remain available locally.
Both final Metro platform exports passed. Work remains uncommitted on `mobile-app`;
the inherited question/protocol changes are still identified by the baseline record.

## Private HTTPS and bounded native rendering — 2026-09-08

This pass used the API 36 ARM64 emulator and a bundled, locally signed Android
release variant. It did not use Metro, `adb reverse`, a public endpoint or the
production Whip daemon. The source checkout remains separate from this worktree.

### Network and fixture verification

- Added strict optional `--origin` support to the manual fixture. The integration
  daemon permits that exact HTTPS Origin and external Host alongside its exact
  loopback Host. The listener remains loopback. Serve preserves the external
  Host header; allowing only Origin initially returned HTTP 403 and was corrected.
  Production networking/allowlist behavior was not relaxed.
- Temporarily connected the Mac's existing Tailscale configuration, whose initial
  state was Stopped with no Serve mapping. Used an unused private HTTPS port 8443
  and a background Serve mapping to the isolated fixture. The foreground attempt
  printed a URL but did not retain a working mapping in this environment.
- Normal TLS verification passed; HTTPS discovery returned 200 and a second SDK
  client initialized over WSS. No certificate bypass or cleartext release
  exception was used.
- The emulator initially could not resolve the MagicDNS name. Restarting it with
  `-dns-server 100.100.100.100` resolved that test-environment issue; the installed
  release app then connected through Serve and reopened its saved HTTPS host
  after force-stop. Setup and exact cleanup commands are in `docs/mobile.md`.
- Temporarily removing only the test Serve mapping and backgrounding/foregrounding
  the app produced a truthful reconnecting state with retained catalog rows.
  Restoring the mapping reconnected automatically without another app retry loop.
  This is host-NAT emulator coverage, not a phone VPN/cellular transition.
- Full SDK acceptance with race detection passed **31/31**, including exact proxy
  Host/Origin, absent native Origin, wrong-host/wrong-port rejection and both
  existing transports' command, permission, question and recovery workflows.

### Attention and text fixes

- One foreground Attention observer now serves every route and the tab badge.
  It retains the existing four-page limit, coalesces focus/manual refreshes and
  qualifies stale, unavailable and partial counts. Native verification: a second
  WSS client created a pending question while Sessions was visible; the badge
  showed one session. Opening Attention and answering Proceed cleared the request
  and badge. The second client verified the committed answer and empty question
  list. Tests cover background cancellation, host replacement and late responses.
- Native text is bounded on every conversation disclosure path. Short messages
  keep Markdown; larger content uses selectable source pages of at most 8,192
  UTF-16 units, preserving surrogate pairs. Collapsed detail previews use 512
  units. Combined tool arguments/output and explicit body results share the bound;
  Copy retains the full loaded text. Recycled rows reset page state, while live
  appends preserve the selected page. A completed body-only tool now states that
  its message is retained on the host instead of claiming it is still waiting.
- Actual native paging exposed an interaction with following the latest message:
  Next initially left the reader at the bottom. Explicit page navigation now
  disables following and ignores old scroll events until FlashList completes its
  scroll. The final installed build showed **Page 2 of 12 at its beginning** with
  the composer accessible. The full-message sheet also resets its scroll on paging.
- Image/HTML source fallback and unsafe-link rejection now have focused component
  tests. No image parser, auto-fetch path or additional content transport was added.

### Native observations and limits

The host fixture contained 10,000 root messages, a 1.4 MB tool body and 100 child
agents. The app opened its bounded latest window, loaded earlier messages and
selected a child's separate 100-message history. Explicitly opening the oversized
tool body returned the 256 KiB limit error. A separate 94,622-character synthetic
message exercised normal loaded text and paging. Successful native scoped HTTPS
body transfer remains a separate acceptance item; the oversized refusal does not
prove that transfer. The earlier Hermes HTTP streaming/hash probe remains recorded
above.

`adb shell dumpsys meminfo` measured total PSS (KiB):

| Observation | Total PSS |
| --- | ---: |
| Release app before connection | 156,592 |
| First large-history open | 206,276 |
| Five repeated reopen cycles | 216,730; 216,420; 217,371; 220,201; 219,847 |
| Final 94,622-character message, page 2 | 183,642 |

These are preliminary samples from different workloads, not a controlled
before/after optimization claim or a 100-cycle/physical-device performance pass.
PSS includes native/JS allocations; a separate JS heap profile was not captured.
The final page sample reported 50,249 KiB native-heap PSS and 12,500 KiB Java-heap
PSS. Dark appearance and 1.5 font scaling were visually checked; font scale was
restored to 1.0. Screen-reader, rotation, reduced-motion and physical-device
acceptance remain open.

### Artifact and cleanup

- Final Android rebuild: **passed, 40 seconds**; installed over the previous local
  release build, retaining encrypted host data. This is development signing, not
  a Play distribution credential.
- APK: `apps/mobile/android/app/build/outputs/apk/release/app-release.apk`.
  SHA-256: `6337bd8c898cbdaf06854b9582cceaa0c0752868d6d2acf88e6f2b6bc68a3e12`.
- Mobile TypeScript passed; **113 tests across 17 suites passed**. Final-source
  iOS/Android Metro exports passed. Integrated **`task check` passed**, including
  formatting, Go vet/whipvet/tests, generated protocol, SDK and web/UI/build checks.
- Removed only the owned Serve mapping and verified `{}`. Gracefully stopped the
  fixture, verified its temporary directory was removed, and shut down the AVD.
  Restored Tailscale to **Stopped** and verified that state. No `adb reverse`
  mappings were present. No production host, auth, notification or public relay
  service was changed.

Xcode inventory still shows only `/Applications/Xcode.app`, version 26.3. Expo 57's
supported Xcode 26.4+ toolchain, Expo/Apple ownership/signing choices and the
physical-device/distribution gates remain outstanding. These successful Android
checks do not complete the iOS beta or the entire plan. Changes remain uncommitted
on `mobile-app` with inherited changes preserved.

## Xcode license and existing Apple access — 2026-09-08

After the owner accepted the license, `xcodebuild -checkFirstLaunchStatus` passed.
The selected installation remains `/Applications/Xcode.app/Contents/Developer`,
Xcode **26.3 (17C529)**. The versioned Expo 57 compatibility table still specifies
**Xcode 26.4+**. A fresh unsigned simulator build attempt exited 70 before
compilation: Xcode could not resolve an eligible destination and reported the
iOS 26.2 platform missing. CoreSimulator separately lists its installed iOS 26.2
runtime; that does not establish a usable Xcode build destination. The retry log
is ignored at `artifacts/ios-license-retry.log`. No simulator-matching override or
dependency patch was applied. Update Xcode and install its iOS platform before
the next native iOS attempt.

Apple account, team, API key ID and issuer variables are present in the local
environment. `APPLE_API_KEY_PATH` points to a missing file, but a single readable
matching `.p8` file was found locally. Its EC P-256 private key successfully signed
a short-lived JWT restricted to the read-only checks. Apple's API returned HTTP
200 for each of these:

- Bundle IDs filtered to `dev.contextlabs.whip.mobile`: no matching record.
- Apps filtered to that development bundle ID: no matching record.
- iOS/unified development and distribution certificates: no matching record.

The local keychain has one valid Developer ID Application identity and no valid
Apple/iPhone development or distribution identity. The existing API access is
useful for subsequent provisioning, but these GET requests do not prove write
permissions, create a signing identity or establish a signed iPhone build. No
certificate, device, bundle ID or app record was created or revoked. No key,
password or authorization token was printed or added to the repository.

Pinned EAS CLI 23.2.0 `whoami` still reports **Not logged in**, and `EXPO_TOKEN` is
absent. An owner Expo login/project choice remains necessary for EAS jobs; local
simulator compilation does not require it. The release runbook now distinguishes
verified existing Apple API access from the remaining provisioning work. No cloud
build or submission was started. Documentation-only follow-up; earlier source
validation remains applicable.

## Expo project, cloud builds and iPhone provisioning — 2026-09-08

The owner signed in to Expo, created the project and installed the official Expo
plugin. Its deployment skill was consulted. Pinned EAS CLI 23.2.0 authenticated
as `sam-inference`; read-only project discovery found the owner's existing
`@inference/whipcode` project, UUID `fa9874ce-4324-474f-86ef-a8749cf8fa91`.
`apps/mobile/app.config.ts` now contains that verified owner, slug and project ID.
The installed app name remains **Whip**, with development bundle identifier
`dev.contextlabs.whip.mobile`. `eas project:info`, resolved public Expo config and
`npm run check:mobile` passed after linking. No duplicate project was created.

The account reported the Free plan, zero builds used and no overage before the
first job. Root `.easignore` preserves Git exclusions and additionally excludes
local research/acceptance records and credential files. `eas build:inspect` on
the `preview-simulator` archive verified the root lockfile, workspace sources and
local native storage module are present. Generated native projects, node_modules,
local test data and credential-file candidates were absent. The reviewed archive
contained 1,858 files / 20,867,799 bytes; its sorted per-file SHA-256 manifest hash
was `1b50fe0c0ccb826185593b8e2ef8e95156b98c6941069d1262a8fb67101f7997`.
The lockfile SHA-256 was
`fd6a84bf41ad4c84104ff23f920ba1a54a9562fc40a9d7bd8d7a0daf4e35aff9`.
Uploads were approximately 8.1 MB compressed. These jobs include uncommitted
worktree source; their base Git revision alone does not identify all inputs.

Cloud jobs (completion and installation are recorded separately below):

- [iOS simulator](https://expo.dev/accounts/inference/projects/whipcode/builds/e32dec53-ddb5-43d0-919a-3ee46252623c):
  `preview-simulator`, app version 0.1.0, build 1, embedded bundle. EAS initialized
  remote build numbering to 1. Logs identify image
  `macos-tahoe-26.5-xcode-26.6`, macOS 26.5.2, Xcode **26.6 (17F113)**.
  Dependency installation, Prebuild and native compilation succeeded. Finished
  at 07:10:02 UTC; native iOS acceptance and the storage defect are recorded below.
- [iPhone preview](https://expo.dev/accounts/inference/projects/whipcode/builds/e47bae5e-5f8f-4f98-9430-1b495e83dc5d):
  `preview`, app version 0.1.0, build 1, embedded bundle, internal/ad hoc
  distribution. Submitted with existing credentials frozen after provisioning.
  Finished at 07:16:59 UTC. This original artifact contains the storage bug below
  and was not installed on the phone.

The owner explicitly selected the paired **iPhone (40), iPhone 16 Pro**. Its UDID
was resolved from local device metadata and registered with EAS. Existing Apple
API credentials then successfully registered the development bundle identifier,
created an Apple Distribution certificate and active ad hoc provisioning profile,
and included only that selected iPhone. Credentials are managed by EAS; no private
key, password or API token was printed or added to repository files. No existing
certificate/profile was revoked. The App Store Connect application record,
store metadata and export answers remain unconfigured.

Initial noninteractive device setup could not create the first credential set.
An interactive build was cancelled at the encryption question without answering
it; config was checked to remain unset. `eas credentials:configure-build` then
completed provisioning, and the noninteractive preview build accepted those
credentials. No export-compliance declaration, push capability or store
submission was added as a side effect.

After the owner connected the phone by USB, Xcode's Devices and Simulators window
confirmed it connected on iOS **26.6.1 (23G83)** and reported Developer Mode
disabled. The window was opened for the owner; the phone-side setting/restart is
still an installation prerequisite. Local Xcode remains 26.3; EAS supplies the
supported compiler for these cloud jobs. Simulator and device acceptance remain
open until their actual launch/workflow checks are recorded.


## Native iOS acceptance and storage-path correction — 2026-09-08

The first EAS simulator artifact compiled with Xcode 26.6 and launched on an
owned iPhone 17 Pro / iOS 26.2 simulator without Metro. Artifact SHA-256:
`3f43d84b1a162d3a660f7a51138761bcf3d88fe84971365a6543ebeac6bd9eff`.
The native UI connected over verified Tailscale HTTPS/WSS to an isolated
fake-provider daemon, sent a message, streamed its echo, and answered a question.
An independent SDK WebSocket client verified the committed answer.

**The first force-quit/relaunch failed closed.** Expo SQLite 57's iOS native
conversion treats a bare filesystem path as a URL, retaining percent escapes.
It created `Library/Application%20Support/Whip/whip.db`, whereas Whip's native
existence/reset/backup checks target `Library/Application Support/Whip`. This
also meant the actual database directory did not receive the intended backup
exclusion. Those original simulator/device artifacts must not be distributed.
Only synthetic simulator data existed; the phone artifact was not installed.

`storage.ts` now passes an encoded `file://` directory URI to Expo SQLite. The
native module still owns the real filesystem path. iOS resolves file URIs via
`standardizedFileURL.path`; Android resolves via `Uri.path`. Tests cover spaces,
literal percent signs, fragment/query characters and Unicode. Invalid native
paths fail before creating a key or database. A macOS Swift regression uses the
installed Expo `URL+FilePath.swift` implementation, reproduces the old behavior,
checks backup exclusion, and creates/reopens a database at the correct path.
No migration, automatic deletion or key regeneration was added for broken data.

The complete mobile suite passes: **120 tests / 17 suites**, including 31 native
storage tests. TypeScript passed. The native fingerprint remained
`9c60e76aed2900da20106e45aa63a859a968c1bc`, matching the cloud simulator binary.
Using official `@expo/repack-app@0.10.3` with `--js-bundle-only` and
`--embed-bundle-assets`, a separate simulator `.app` was created with the fix
and the corrected native Back label. This simulator repack is not a signed
phone artifact and does not establish an OTA update path.

On a fresh owned simulator, the corrected embedded bundle passed:

- Cold launch and manual private HTTPS/WSS attachment to the isolated daemon.
- Mobile message submission and streamed response, verified by a second client.
- Saving an unsent draft, force-quitting the process, reopening, automatic
  reattachment to the saved host, and restoring the same draft on session entry.
- Receiving a new question after restart, selecting and reviewing an answer,
  submitting it, and seeing no remaining request. An independent client verified
  committed history and an empty question snapshot.
- Creating a new session and first message through the native UI; another client
  found the exact new session and host working directory in the session index.
- Filesystem inspection confirmed the encrypted database at the native path,
  no literal `Application%20Support` directory, and Foundation confirmed backup
  exclusion on the actual database directory. No encryption key was read out.

A later archive inspection found that the first `.easignore` omitted exclusions
from nested/global ignore files: native-module Android build output had been
uploaded. No credentials or user test data were present. `.easignore` now also
excludes that output, Gradle/Kotlin caches, key/PEM files and machine-local files.
The corrected archive contains **1,577 files / 19,312,990 bytes**, with manifest
SHA-256 `a5537a2d2a36721490fda9bc17e0fa9790347bedc4c83eecdb27a6faa17769b9`.
All required workspace/native inputs were present; excluded-directory and
credential-candidate checks passed. The root lockfile remains unchanged from
this turn's previously recorded hash.

A replacement [signed iPhone preview](https://expo.dev/accounts/inference/projects/whipcode/builds/4193010f-b554-46d5-b7f6-bf17114fca17)
was submitted at 07:24:40 UTC using the existing frozen credentials and corrected
archive. It embeds the fix; no EAS Update configuration or store submission was
introduced. Completion, checksum and physical installation must be recorded
before treating that artifact as accepted.


The independent SDK/native reviewer found no actionable regressions in the URI
fix after checking installed Expo SQLite 57.0.2 on both platforms. The existing
Attention badge theme correction was also verified byte-identical to the uploaded
archive; it was not an unbundled change after the phone job started.

After the acceptance flows, the temporary Tailscale Serve 8443 mapping was
removed, restoring the initial empty Serve configuration. The fixture was
interrupted deliberately; the PTY delivered SIGINT to its child test process,
so the fixture reported a nonzero shutdown, but its finally cleanup removed the
owned temporary directory. Port 57323 has no listener. Tailscale remains Running,
matching this turn's initial state. The app reports the disconnected host and
disables sending while retaining the visible transcript. No production Whip host
was stopped, reset, exposed or used for these synthetic acceptance flows.


### Corrected signed phone artifact ready; Developer Mode required

Build `4193010f-b554-46d5-b7f6-bf17114fca17` finished successfully at
**2026-09-08 07:32:23 UTC**. Downloaded IPA: **15,420,890 bytes**, SHA-256
`92d3c864e10cca1e502e980f940deabbfb9478519299b8d2021db78a865a9043`.
`codesign --verify --deep --strict` passed. The executable is arm64; bundle ID
is `dev.contextlabs.whip.mobile`, app version 0.1.0, build 1. Its embedded ad hoc
profile includes only the approved iPhone and expires 2027-09-08. The archive
contains no `ITSAppUsesNonExemptEncryption` declaration. Executable file modes
were restored from the IPA's ZIP metadata after local inspection/extraction.

`devicectl device install app` acquired the phone connection, then returned
**CoreDeviceError 10005: Developer Mode is disabled**. The app was not installed.
This is a phone-side prerequisite, not a signing/build failure. The owner can
open the exact EAS build page in Safari on the phone, install there, tap Whip to
trigger the Developer Mode alert, and enable it in Privacy & Security with the
required restart/Turn On confirmation. This app-first path is documented by
[Expo](https://docs.expo.dev/guides/ios-developer-mode/).

Both owned iOS simulators are shut down; their synthetic app data and original
failed artifact remain available for diagnosis. No permanent phone-side security
setting was changed by the agent. Signing and build setup are complete for this
internal preview; actual iPhone launch, private-host workflows, accessibility,
network transition tests and TestFlight remain open.


The owner subsequently reports the app running on the selected iPhone and asks
to connect it to the existing daemon on `kuzco-4090`. This establishes an
owner-reported phone launch, not yet observed physical workflow acceptance.
Read-only Tailscale discovery found `kuzco-4090.tail7524e6.ts.net` online;
HTTPS port 443 refused the connection. The Mac's default SSH username was
rejected, so the current daemon listener/Serve configuration is not yet inspected.
No remote daemon restart or network configuration mutation was performed.


## Kuzco 4090 private HTTPS preparation — 2026-09-08

After the owner requested autonomous setup, SSH succeeded as `sam@kuzco-4090`.
The existing executable is
`/home/sam/.local/share/whipcode-multihost-test/bin/whipcode`, with its existing
`WHIPCODE_HOME` under that directory. It serves protocol 4 at
`http://100.75.4.112:43111`. This host uses the WHIPCODE-prefixed environment
variables from its existing build; no replacement binary or new runtime was
installed. SDK WebSocket initialization and enumeration of seven sessions passed.
The host currently runs the existing multihost test environment; these checks do
not establish real provider execution.

Tailscale 1.102.2 reported an empty Serve configuration. Both unprivileged Serve
configuration and `sudo -n tailscale serve` were rejected: `sam` is not configured
as a Tailscale operator and sudo requires interactive authentication. The available
SSH key does not permit root login. No sudo password is available to the agent.
No Tailscale ACL/operator, public Funnel, or HTTPS configuration was changed.

The daemon originally allowed only its HTTP hostnames including port 43111.
Tailscale's installed-version reverse proxy preserves the incoming Host header.
Before restarting, the SDK verified all seven sessions idle, no active turns and
no pending agent mail. The existing `control.py` was backed up to
`control.py.before-mobile-https-20260908T144509Z`, and only the allowlists were
extended with `kuzco-4090.tail7524e6.ts.net`, its explicit `:443` form, and
`https://kuzco-4090.tail7524e6.ts.net`. The existing addresses/origins, listener,
binary, data home, provider and controller behavior were preserved.

The controller stopped idle PID 671494 and started PID **800249**. The SDK
reconnected and verified exactly the same seven session IDs. Discovery requests
with the planned HTTPS Host and Origin now return 200. The changed controller
preserves these additions on future starts through that controller.

Remaining privileged command, runnable from the owner's Mac terminal:

```sh
ssh -t sam@kuzco-4090 'sudo tailscale serve --bg http://100.75.4.112:43111'
```

The owner must enter the remote Linux sudo password in the terminal. An attempt
to open a terminal password prompt through computer use was blocked because that
tool does not allow Ghostty; no alternate UI bypass was attempted. After Serve is
enabled, verify actual TLS/WSS and enter
`https://kuzco-4090.tail7524e6.ts.net` in the phone app with Tailscale connected.
The HTTPS endpoint is not yet established; the daemon preparation alone is not a
completed phone-to-host acceptance test.


## Correct gpu-4090-sam deployment — 2026-09-08

The owner corrected the target to the configured SSH alias `gpu-4090-sam`, which
resolves to `sam@kuzco-gpu-2`, Tailscale IPv4 `100.80.110.112` and DNS name
`kuzco-gpu-2.tail7524e6.ts.net`. The supplied interactive credential was used for
SSH and sudo authentication, without installing keys or changing sudo/operator
permissions. Temporary SSH multiplexing and a root shell were used for setup.

No Whip installation, runtime or service existed for `sam`. Two pre-existing
Whip processes belonged to another user and were left alone. The new installation
uses `/home/sam/.local/bin/whip`, data `/home/sam/.whip` (sam-owned, mode 0700), and
`/etc/systemd/system/whip-sam.service` (root-owned, mode 0644). The system service
runs as `sam`, starts at boot, restarts on failure, and binds only
`127.0.0.1:9876`. No user lingering setting was changed.

The mobile worktree was cross-compiled with Go 1.27.0, `GOOS=linux`, `GOARCH=amd64`,
`CGO_ENABLED=0`, `-trimpath` and stripped symbols. The embedded version is
`mobile-app-dd7aaa3a-20260908`; this is an uncommitted-worktree build, not a release
from that base commit alone. The binary is 41,504,928 bytes, SHA-256
`a53b2106afa3f346d240cf7b5478e231d2d5d14163693d75b2c399e1fda0d52f`.
The remote upload checksum matched before the file was installed atomically.
`govulncheck` v1.7.0 binary scanning reported no vulnerabilities; its log is in
`artifacts/gpu-4090-linux-govulncheck.log`. No dependencies changed for deployment.

The service unit sets HOME/WHIP_HOME for sam, WHIP_NETWORK=1, the loopback listener,
exact loopback/external Host allowlists (including explicit :443), and the exact
HTTPS browser Origin. `systemd-analyze verify` passed. After enable/start, service
state is active/running, boot state enabled, PID 166854, restart count 0.

The initially empty Tailscale Serve configuration now persistently proxies private
HTTPS 443 to `http://127.0.0.1:9876`. No Funnel, ACL or operator changes were made.
Initial TLS attempts timed out while Tailscale obtained its first certificate;
after issuance the actual Mac HTTPS request returned discovery HTTP 200 with
certificate verification result 0. The SDK connected over WSS, negotiated protocol
4, and read sessions, attention, directories, configuration and provider catalogs.
Runtime ID is `d3c5442033feb1835b7e598fe2956cce`; sessions and attention are empty.
Filtered read evidence is in `artifacts/gpu-4090-sam-connection.json`.

The default catalog contains four models and an Inference.net provider entry,
with default model `kimi-k3-fast`. The provider status API's `configured=true`
means the entry exists, not that a credential resolves. Direct presence checks
confirmed INFERENCE_API_KEY is absent from the service environment and neither
sam's Whip machine-key store nor inf CLI store exists. No credentials were copied
from another account or machine. Provider browser login remains required before
model execution; no model request or synthetic session was submitted here.

The earlier distinct `kuzco-4090` machine was restored before this deployment.
All seven sessions were verified idle, its controller matched exactly the agent's
prior edit, and the original controller backup was restored byte-for-byte. The
existing controller restarted PID 800249 as PID 803045; SDK reads verified the
same seven session IDs. Original HTTP Host/Origin allowlists are restored and
Tailscale Serve remains empty. Its retained backup is the only setup artifact;
there is no outstanding privileged command to run on that machine.

The current connection URL and systemd/provider setup commands are documented in
`docs/mobile.md`. Phone Wi-Fi/cellular workflows and actual provider execution
remain unverified. Persistent service and Serve configuration are intentionally
left running for the owner.

Setup cleanup: the privileged SSH shell and temporary multiplex master were
closed, and the owned temporary socket directory was removed. The daemon and
private Serve mapping remain active. `git diff --check` passed.


## Connection diagnostics and silent iPhone failure — 2026-09-08

The owner's screenshot has the correct base URL. The daemon serves the web UI at
`/`, discovery at `/api/v3/web`, and the shared SDK's socket at `/api/v3/ws`.
Fresh Mac HTTPS/TLS and SDK connection checks passed, including the native HTTPS
Origin. The host now has an owner-created session; no session or provider mutation
was performed for this investigation.

Two client defects explain why a failed phone connection appeared to do nothing:
`server.tsx` reported errors only to the root banner outside its native modal, and
errors before assigning an attempt ID were swallowed. In addition, RN 0.86 emits
a bare error then a close with the native failure reason; SDK `onerror` erased
`onclose` before it could preserve that reason. The sheet now owns inline errors,
and SDK transport gives the synchronous close precedence with a microtask
fallback for error-only sockets. Close details are bounded and stripped of control
and bidi formatting. The mobile initialization deadline is now 15 seconds.

Test Connection uses a transient SDK client and staged HTTPS discovery, WSS
initialization/identity and one-item session read. Every stage has a 15-second
limit; Expo fetch streams discovery into an 8 KiB buffer and aborts/releases its
request even on header rejection or excess body. Missing embedded web assets do
not reject an otherwise compatible headless daemon. Testing never saves a host,
changes the current host, submits commands, or claims provider execution works.
Cancel, background and unmount release the probe; stale results cannot navigate
or overwrite a later attempt. URL edits clear old results.

Validation after independent review fixes:
- Mobile TypeScript passes; 144 tests pass across 19 suites, including 24 focused
  probe/screen tests for HTTP/protocol/native failures, empty/headless hosts,
  response bounds, identity, cancellation, early errors and stale results.
- SDK suite: 231 tests pass, including native error/close precedence and disposal.
- `task check` passed. Production Metro exports passed on iOS and Android.
- Independent adversarial review found two issues (headless availability and
  response buffering), both corrected with regressions; final review has no
  remaining actionable findings.

The official Expo repack tool embedded the final JS into the previously verified
simulator binary, with no native dependency/config change. On the owned iOS 26.2
Whip Storage Acceptance simulator, the old inactive fixture produced a visible
inline HTTPS failure and its native reason. The correct GPU host then passed all
three stages; Test Connection left the sheet open. Connect dismissed the modal,
showed Connected, and displayed the existing remote session. This verifies the
native app/transport against the real private host from the Mac's network, not
the physical phone's VPN or per-app network access.

The owner's iPhone is still on the previous signed embedded bundle. A replacement
internal preview build was queued using the existing EAS project and approved
phone profile: `b595d18a-8b5e-4804-8211-2b3b0d9c392c`. Its native fingerprint is
unchanged (`9c60e76aed2900da20106e45aa63a859a968c1bc`). No store submission or
new signing/account setup was performed. The physical-phone root cause remains
unconfirmed until its test/native error or Safari API result is available.


### Signed diagnostics update ready; phone disconnected before installation

Build `b595d18a-8b5e-4804-8211-2b3b0d9c392c` finished at
2026-09-08 15:31:29 UTC. IPA size: 15,430,191 bytes; SHA-256
`bbb8611b2cc71afb416f81e6588039d5eb1c4bd16e8846f1b02a268fae0d8394`.
`codesign --verify --deep --strict` passed. Bundle ID remains
`dev.contextlabs.whip.mobile`, version 0.1.0/build 1. The embedded ad hoc profile
contains exactly the previously approved phone and expires 2027-09-08. The
embedded bundle contains the new Test Connection controls. This is an update to
the existing app identity, with no uninstall/reset or data migration.

The paired phone was available at initial discovery, but `devicectl device
install app` returned CoreDeviceError 1011 (device not found). A fresh device
listing confirms that iPhone 16 Pro is now unavailable. The updated app was not
installed on the physical phone. Install from the exact EAS build page in Safari,
or reconnect/unlock the phone for a direct retry:
https://expo.dev/accounts/inference/projects/whipcode/builds/b595d18a-8b5e-4804-8211-2b3b0d9c392c

The owned simulator was shut down after acceptance; the real daemon was not
restarted or modified. Final whitespace validation passed. No physical-phone
network diagnosis is claimed: its pending Safari discovery response or the new
in-app test result is still needed.
