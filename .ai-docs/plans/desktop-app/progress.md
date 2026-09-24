# Desktop implementation progress

Branch: `desktop-app`; baseline `dd7aaa3a7f9b8c00bd4ec095b978def1c9231805`.

The entire phased plan is authorized and being implemented in its separate
worktree. The original checkout contains unrelated work and has not been changed.
The [desktop guide](../../../docs/desktop.md) documents current build commands and
release configuration. No release has been published or workflow triggered.

The user stopped further performance testing on 2026-09-08. Existing measurements
and partial failures are retained; unresolved performance targets remain visible.
Correctness validation passed and the package was rebuilt with the MCP fix,
Developer ID signing and Apple notarization.
The completed startup/idle measurements precede the MCP cancellation fix; their
recorded native/ASAR hashes identify that earlier artifact. The shared renderer
is unchanged, and measurements will not be repeated under the current scope.

## Implementation status

| Phase | Implemented | Acceptance still open |
| --- | --- | --- |
| 0: feasibility | Electron/Electrobun same-renderer comparison; secure scheme, storage, real local/SSH transport; signed packages | Notarized download, helper TCC and reference hardware checks |
| 1: shared renderer | Common bootstrap, platform adapters, type-only preload, saved host migration/identity, native-import guard | Passed: browser workflows and isolated production/development app/UI consumers |
| 2: package/local | Go/Swift companions, shared Go/ASAR renderer, immutable retained runtime, Finder launch, verified full ZIP/DMG contents | Downloaded install without toolchains and manual TCC checks |
| 3: connections | Attach-before-start, unowned stale socket handling, bounded SDK transport, explicit replacement identity, shared URL path | Final packaged transport parity and manual reconnect matrix |
| 4: SSH | System OpenSSH, configuration/keys/agents/forwarding, shared prompts, bounded Go supervision and cleanup | Hardware-key, real sleep/wake and separate Linux target acceptance |
| 5: native behavior | Saves, copy, external links, local folder, attention notifications, Cmd-W/Q, deep links and restoration | IME/VoiceOver, display changes, OS notification routing and helper permissions |
| 6: releases | Renderer-once CI, temporary signing keychain, signed DMG/ZIP verification, updater/close handshake, immutable conditional feed publisher | Real credentials/feed configuration, notarized N-to-N+1, interruption/signature/rollback acceptance |
| 7: final gates | Focused regressions, adversarial reviews, repository/race checks and signed startup measurements | Intermittent idle CPU bursts, cold-cache/reference-hardware, load measurements and remaining manual OS matrix |

Phase 1’s exit gate has passed. No other phase is marked complete solely because its implementation exists. Individual
proofs below are distinct from each phase's broader exit gate.

## Verified evidence

- [Shell comparison](evidence/shell-comparison/README.md): same frozen production
  renderer, real large retained-session fixture, 30 alternating launches per shell
  after warmups. Electron 44.2.0 usable median/p95 **1.001/1.289 s**; Electrobun
  2.0.1 **1.682/2.715 s**. No captured renderer errors. This supports retaining
  Electron; these are comparison figures, not final installed-app startup claims.
- [Native runtime profiling](evidence/local-runtime-profile/README.md): minimal
  Finder PATH, isolated homes, real signed companions. First install median
  **802 ms**, warm attach **110 ms**, retained restart **276 ms**. Verification
  stays enabled. This does not explain an earlier 4.28 s full staged diagnostic;
  final end-to-end measurement is separate.
- [Signed packaged startup](evidence/packaged-startup.md): the measured signed app
  completed 30 warm attachments and 30 retained-daemon starts with no failures.
  Warm shell p95 was **0.912 s** and retained-session usable p95 **1.305 s**;
  usable UI with daemon startup was **1.572 s** p95. Three fresh installations
  ranged **1.938–4.144 s**; there are too few for a p95. This was a small real
  session on an M4 Max, with warm OS caches and a bounded read-only probe. It does
  not establish cold-cache, first Gatekeeper, load or reference-Mac acceptance.
  All 65 GUI processes, 35 daemon processes and fixture directories were removed.
- [Signed idle CPU/memory](evidence/packaged-idle.md): three preserved windows
  recorded desktop CPU **6.04%, at least 7.94%, and 0.23%** of one core. Mean
  aggregate desktop RSS was **549, 529 and 352 MiB**; daemon/worker costs and
  de-duplicated macOS footprints are reported separately. The second run's GPU
  allocations explain part of its memory footprint. The intermittent CPU burst
  remains unattributed, so the idle gate has not passed. All 28 tracked fixture
  processes were cleaned up.
- [Partial staged load run](evidence/staged-performance-partial.json): passed
  large-history retention, selection/scroll preservation, cached child/root and
  tab switches, 1/8/32 tabs with at most four subscriptions, and 40 stream probes
  with 16 concurrent synthetic agents. An 8 MiB image upload reached Ready with
  matching digest/size. The stored-message download fixture could not locate its
  control, so the run did not complete. The browser performance follow-up was
  cancelled when the user stopped performance work. All six GUI/daemon PIDs from
  the three attempts were verified gone. These are partial staged diagnostics,
  not a passed signed-app load gate.
- [Scheme probe](evidence/electron-scheme.json): secure context, styles/fonts,
  Web Locks/localStorage, SPA reload and actual HTTP/WS origins. Renderer has no
  Node. This used a fixture bridge, not a complete installed app.
- [Installed app](evidence/installed-app.json): a real Developer ID signed app
  launched through macOS LaunchServices with an isolated Whip home and minimal PATH,
  started a retained local daemon without TCP, and preserved an unsent text draft
  through tab close/reopen. Cmd-W hide and the Quit Whip menu were exercised; process exit was checked and
  the daemon PID survived. Earlier keyboard-only quit attempts were inconclusive.
  It was not a downloaded notarized app and no model turn was submitted in that UI.
  Later explicit daemon cleanup exposed a preexisting MCP shutdown cancellation
  stall. The owned test daemon was stack-dumped and terminated after its graceful
  stop timed out; its fixture was removed. Three deterministic local HTTP cases
  reproduce the hangs on the original implementation. The fix passes six new
  regressions and the full MCP race suite, preserves healthy SSE notifications,
  and gives remote session DELETE a one-second cleanup budget. Final repository
  checks and the full race suite passed after the fix, which is included in the
  rebuilt signed package. The development user's
  daemon and data were preserved.
- [Real worker continuity](evidence/runtime-continuity.json): actual signed Go,
  loopback fake provider, three Starlark worker turns with verified durable output.
  An accepted turn survived client detach and copied-source removal; the same
  daemon then started a new worker from its retained executable. All fixture
  processes were cleaned up. This models replacement, not an actual Squirrel update.
  The signed artifact rerun before notarization passed without a legacy-manifest flag; the
  packaged manifest and negotiated protocol both record 4.1 (schema 10).
  The later notarization build changes signed bytes but not source code; this
  evidence retains the hashes of the executable it actually exercised.
- The final signed ZIP and DMG passed full app-tree comparison and repeated
  ASAR/native/signature/fuse verification after extraction/read-only mounting.
  **47 packaging/publisher/provenance tests** and **28 GitHub publication retry tests** pass, including real mounted-DMG
  tampering/cleanup, unsafe ZIP paths, stale evidence and conditional-write races.
  Final renderer digest is `fb6474fdaed31a31ef48de7115708a0af2003f251a28daf2196724eb63f0108b`;
  [package evidence](evidence/package.json) records both final archive hashes,
  matching application-tree digests, protocol metadata and signatures. The DMG
  is 144,576,572 bytes; ZIP is 145,114,134 bytes. Apple accepted the app and DMG;
  stapling and local Gatekeeper assessments passed, including archived copies.
  This is a notarized local build with dirty source provenance and no update
  feed, so publication is refused.
- Native tests include actual isolated sshd with generated host/client keys,
  known/unknown-host decisions, encrypted-key prompts, UTF-8 fragmentation,
  denied/inherited forwarding, reconnect and cleanup. No user SSH credentials or
  system Remote Login are changed. The final **65 native tests** and **75 packaging/publication tests** pass
  with no skips.
- Final `task check` passed: **257 web tests / 30 files**, **196 SDK tests**,
  protocol drift checks, UI checks and **18 pack-web tests**, plus Go format/vet
  and full tests. The complete `go test -race ./...` rerun passed after the MCP
  change. A recurring browser-fixture cleanup race was fixed by joining its owned
  Chrome process before deleting profiles; all 12 focused race repetitions and
  the full suite passed. CLI stdout capture now uses a temporary file after a
  pipe-capacity deadlock in `TestBrowserInstall`; its review and full/race suites
  passed. Desktop Go helper integration/race tests also passed.
- SDK packed protocol/browser/Node/state entry points import outside the repository.
  Packed UI/app source packages pass isolated installation plus production and
  Vite development rendering. [Browser workflow evidence](evidence/browser.json) passes Chromium (16 checks)
  and Firefox (15 checks), including upload/preview, multi-client permissions,
  draft/recovery, exact-turn cancellation and responsive accessibility. Stale
  model/effort/pause selectors were updated in the fixture; shipping UI was unchanged.
- [Staged local lifecycle](evidence/local-smoke.json) passed after strengthening
  relaunch to require an enabled connected-host control. A saved runtime ID alone
  would not prove that the new renderer reattached. Settings reload, no local TCP,
  retained runtime installation and daemon survival after forced GUI exit passed.
  The final rerun uses private HOME/TMPDIR/Whip/user-data directories, records no
  startup measurements, and requires successful daemon cleanup before deletion.
  Its fixture path was shortened after the nested macOS temporary path exceeded
  the Unix socket length limit. The final signed runtime continuity rerun also
  passed all three worker turns and cleanup with the new MCP build.
- `npm run dev:desktop` started the shared Vite server and isolated native daemon;
  actual UI and renderer HMR were observed. Its window/watcher was stopped before
  packaging; the `.dev` daemon/data were preserved because a user session exists.

## Release limitations and external gates

A local Developer ID Application identity and working Apple notarization access
are available. The HALO credential investigation validated the existing Apple ID
environment and stored a dedicated macOS Keychain profile. Apple accepted the
local app and DMG. [Credential discovery](evidence/release-credentials.json) records
names and checks only; it contains no credential values.

HALO's GitHub release secrets and Cloudflare R2 hosting were identified. The local
R2 credentials were denied access to HALO's release object, and the Infisical
lookup requires authentication. CI credentials, an owned Whip HTTPS updater feed
and release environments still need configuration; the AWS-OIDC workflow must
be adapted if R2 is reused. HALO's files, secrets and release storage were unchanged.
Read-only GitHub inspection confirmed that `context-labs/whip` has neither the
`desktop-stable` nor the `desktop-beta` environment configured. The desktop guide
lists the required variables and secrets; no environment or credential was changed.
No published feed, downloaded Gatekeeper install, signed N-to-N+1 Squirrel update,
TCC continuity, or minimum-OS/reference-hardware gate has passed. Local
notarization, stapling and Gatekeeper assessment have passed.

Native runtime versions are retained conservatively; automatic garbage collection
is not implemented. The GUI never calls `whip update` or stops a daemon on exit.
Beta uses a separate default runtime home, user data, bundle ID and link scheme.
Notifications are opt-in attention checks while the app runs; durable completion
notifications and same-count permission replacement detection are not claimed.

Final release acceptance must preserve browser behavior and prove packaging/update
requirements. Further performance work is deferred at the user's request; its
unresolved findings remain recorded. Development smoke and framework benchmarks
do not establish downloaded release acceptance.
