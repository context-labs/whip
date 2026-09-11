# Mobile UI delivery evidence

Branch: `codex/mobile-ui`

## Phase 1 — baseline

- User approved the implementation plan and requested a commit after each phase.
- Updated the mobile root snapshot fixture for the current required agent-definition fields. Mobile typecheck and all 27 runtime tests pass; all six previously failing runtime tests were caused by this invalid fixture.
- All 19 native-storage tests pass outside the filesystem sandbox. The earlier Swift backup-exclusion precondition failure was sandbox-related; no test assertion or storage implementation was weakened.
- Xcode 26.3 with iOS 26.2 runtime is installed; no simulator/emulator was booted initially. Expo's documented Xcode recommendation is 26.4+. Native visual/build feasibility continues with the actual component gallery in Phase 2; it is not established by this baseline commit.
- Scope defaults accepted by proceeding: iOS and Android, combined connected hosts, core UI without chat attachments/voice/pairing/push.

## Phase 2 — native library and themes

- Added the reusable `src/ui` controls, theme-aware legacy aliases, development gallery, Appearance route, full generated catalog picker, global preview/cancel, custom host/JSON imports, fonts/text size/code wrapping/tool density/contrast/motion preferences.
- Extracted the existing host-theme normalization into the portable presentation entry. Native Markdown maps heading/body/code/syntax/list/table/status roles instead of native library defaults.
- Schema v2 stores one atomic appearance/custom-theme record in a separate 256 KiB bucket (maximum 16 themes). This is simpler than separate imported rows and makes selected-theme removal atomic. Migration and failed-write preservation have regression coverage.
- Mobile typecheck passes. Full mobile run reached 148/149 passing; the remaining test depended on the former Expo UI button mock. Updated it to exercise the real button handler, retaining its duplicate-send assertion. The targeted request suite is recorded at the phase commit.
- All 14 shared UI tests pass. Native Android build and iOS build continue in the background; Homebrew CocoaPods fixed the obsolete system CocoaPods failure. Visual acceptance is still pending.

## Phase 3 — host lifecycle

- Added the device workspace coordinator, independent host runtime scopes, host management/edit/connect/remove, setup help, and dedicated settings navigation. Startup reconnects remembered hosts two at a time. Session routes resolve verified runtime identities; duplicate connections to the same runtime are rejected.
- Kept the existing tested runtime command engine rather than splitting it into another duplicated class. Workspace preferences are separate from host commands. Child runtimes cannot close shared storage.
- Three workspace lifecycle regression tests pass, nine host screen tests pass, and all eight request tests now pass, including repeated presses and retained failed-send drafts. Mobile typecheck passes.
- Android debug APK builds and runs on API 36. iOS build is blocked before compilation by Xcode destination eligibility reporting the installed iOS 26.2 platform unavailable; this is not recorded as an iOS pass.

## Phase 4 — sessions and creation

- Replaced the tabbed home with the storyboard's session-first navigation, first-host onboarding, shared New session entry points, combined host lists, search, host/status filters, and archived sessions. Healthy hosts remain usable when another host fails.
- Added a single foreground workspace attention owner. Session and attention reads share a two-request device lane. Each host retains one bounded page (128 sessions/256 KiB or 64 attention entries/128 KiB); Next replaces that host's page rather than growing memory. This explicit page-window implementation differs from append pagination in the proposal and keeps partial counts honest. Old query windows are immediately garbage-collected.
- New session is host → folder → review, with model/provider/reasoning/execution language/advertised agent definitions in a native options sheet. The existing journaled creation state machine remains responsible for creation and recovery.
- Mobile typecheck and 13 targeted creation/workspace/read-lane tests pass. Android home and the native filter sheet were visually inspected; the sheet bridge uses Expo's RNHostView on both platforms.

## Phase 5 — conversation and settings

- Added a keyboard-aware rounded composer, compact host/recipient header, working status pulse, semantic user bubbles, quieter activity previews, and reduced-motion-aware navigation. Agent selection, full-content inspection, questions and permissions use the shared native sheet.
- Session menu supports device-local pins (128 maximum), guarded rename/archive/restore, details and Copy ID. The protocol has no pin command, so pins explicitly stay on the phone; existing host pins remain visible. Local host errors are visible inside scoped conversations.
- Tool density now controls initial disclosure and preview length. Theme imports remain scoped to the explicitly selected source and abort on a source change.
- All 153 mobile tests pass, including native storage. A temporary fake-provider host connected from Android; its combined session list, empty chat, keyboard-open draft, send, automatic title and completed echo response were inspected. No real model or ordinary host was used for these mutation tests.

## Phase 6 — native fixes and final verification

- Fixed two failures found through Android use: native document picking backgrounds the app, so JSON imports now wait for the original host to resume; session identity resolution now lives in the leaf route because external-link query parameters were unavailable in the parent layout. Regression tests cover cancellation/source replacement and ambiguous or missing route identities.
- Connected host renames preserve the command engine. Startup prioritizes the selected host. Storage migration validates records before advancing the schema version. Removed the obsolete single-host attention owner and replaced its tests with combined-index lifecycle, partial failure, identity, cancellation and page-window coverage.
- Refined the folder step, sheet headings, composer capitalization, selected palette indicators and background working animation. Native session sheets expand by dragging to expose every action.

### Checks

| Check | Result |
| --- | --- |
| `npm run check:mobile` | Pass after final code changes |
| `npm run test:mobile` | **25 suites, 160 tests pass**, including native-storage checks with normal filesystem metadata access |
| `npm run export:mobile` | Pass; iOS and Android Hermes bundles generated after final route changes |
| `task check` | Pass; required repository checks completed, with final mobile checks repeated after subsequent mobile-only fixes |
| Shared UI tests / web checks | 14 UI tests, web typecheck/build passed during the shared-theme phase; repository checks also passed |
| `git diff --check` | Pass |
| Android native development APK | Build and launch pass; API 36 emulator, Expo SDK 57 |
| iOS native build | **Blocked before compilation** by Xcode destination eligibility; not a pass |

### Android flow evidence

An isolated, time-limited fake-provider host was used for host connection, combined session home, empty conversation, keyboard draft, send/echo/title update, a reviewed single-choice question, an Allow once permission and completed tool result. Host → folder → options → review → session creation also completed. Local pin state and archive/restore actions were exercised through the native session menu. External links with verified runtime identity open the correct host-scoped conversation.

Appearance selection, light-mode cold-start persistence and JSON import through the native file picker were exercised. The gallery rendered actual library controls, Markdown and composer for every generated theme: **66 captures, 66 matching titles**. This establishes Android rendering coverage, not exhaustive visual review of every pixel or accessibility acceptance. Claude Code, GitHub Light and Synthwave were inspected closely; semantic-role/contrast tests iterate the full generated catalog.

- [Claude Code controls and composer](screenshots/claude-code.png)
- [GitHub Light controls and composer](screenshots/github-light.png)
- [Synthwave controls and composer](screenshots/synthwave84.png)
- [Session creation review](screenshots/create-review.png) — captured before the final footer wording refinement.
- [Conversation with keyboard](screenshots/chat-keyboard.png)
- [All-theme capture manifest](screenshots/android-theme-results.json)

The complete 66-image archive is available locally at `/Users/samheutmaker/.codex/visualizations/2026/09/11/01a09161-8e80-7711-8639-6b739c83b206/whip-mobile-android-themes.zip`. Representative captures above are committed so the evidence remains useful without that local archive. Captures use a development build and may include Expo's development overlay.

### Remaining release acceptance

Xcode 26.3 (17C529), iOS SDK/runtime 26.2 and a newly created iOS simulator are present, but both explicit-simulator and generic-simulator builds report the platform unavailable before compilation. Homebrew CocoaPods installation succeeded. Expo SDK 57 recommends Xcode 26.4 or newer; a working native iOS toolchain is required to complete that matrix. JavaScript export success does not establish native iOS build or layout success.

Physical-device VoiceOver/TalkBack, small-phone/tablet/landscape and maximum system text sizing, cellular/background/resume behavior, and measured four-host/long-transcript streaming performance still require release acceptance. No frame-rate, physical-network or full accessibility pass is claimed from these emulator captures. Packaging, store distribution, pairing and push remain separate roadmap work.

### Delivery decisions and commits

The tested `MobileRuntime` engine is reused per host rather than copied into a new engine. Combined indexes use bounded replaceable pages instead of append pagination. Pins are device-local because the current protocol has no pin operation. Older component names remain thin aliases into the new library, with no competing style system.

1. `457dce4f0` — baseline fixtures and runtime validation.
2. `55310a79a` — native library, complete themes and Appearance.
3. `809a13e3c` — independent host connections and management.
4. `21a923e3c` — combined sessions and guided creation.
5. `4f29f87e2` — conversation, composer and session actions.
6. Final phase — native lifecycle/routing fixes, regression coverage, screenshots and canonical documentation (the commit containing this entry).

## iOS follow-up — 2026-09-11

The owner upgraded Xcode and requested installation on their iPhone. The source
baseline is `9bb095f74`, after merging `feature/agent-definition` into
`codex/mobile-ui`.

- Xcode **26.6 (17F113)** now builds the current native app successfully with
  iOS SDK 26.5. The earlier destination/toolchain block is resolved.
- Local Release simulator compilation passed. The first attempt explicitly
  disabled signing and reached the secure-storage error screen because the
  simulator app lacked its application entitlement. Rebuilding with normal
  simulator ad hoc signing (`CODE_SIGNING_ALLOWED=YES CODE_SIGN_IDENTITY=-`)
  fixed startup without changing product source, weakening storage, or deleting
  app data.
- The signed Release app installed and launched on iPhone 17 Pro / iOS 26.5
  simulator. SecureStore and encrypted storage initialized, and the new
  [onboarding screen](screenshots/ios-onboarding.png) rendered. The app contains
  its Hermes bundle and does not require Metro. Bundle SHA-256:
  `f8d8972dfd32437920b6875c95c40e8e45dc8165a7af3d152a24521b29cae377`.
- Full interactive iOS UI acceptance is not established by this launch. The
  computer-use service timed out while selecting Simulator; an external
  Appearance link reached iOS's Open in Whip confirmation and was not recorded
  as an Appearance pass.
- The previously selected iPhone 16 Pro is still reported unavailable by
  `devicectl`. The owner has been asked to connect and unlock it.
- Existing Expo ownership was verified (`@inference/whipcode`). The local
  keychain has a macOS Developer ID identity, not an iPhone signing identity;
  the existing phone distribution credentials are managed by EAS.
- The signed EAS preview request was rejected by automatic approval review
  before execution because uploading project source requires explicit approval.
  The owner was asked to approve uploading the current source to the existing
  Expo project and using its existing signing credentials. No new remote build
  or phone installation is claimed. Signing and physical-device installation
  remain pending that response and phone availability.

Local build logs: `/tmp/whip-ios-ui-release.log` and
`/tmp/whip-ios-ui-signed-simulator.log`. No application source changed during
this follow-up; the existing regression results above remain the code baseline.
