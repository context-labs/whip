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
