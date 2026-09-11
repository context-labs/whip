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
