# Default permission level for new sessions

Branch: `whip-rlm`

Status: implemented and validated end to end.

## Goal

Add **Default permission level** beneath Reasoning effort in Settings → Providers & models → Defaults for new work. Persist it on the selected execution host alongside model and effort defaults, and use it for fresh sessions unless the user explicitly chooses another mode.

Reuse the current choices and behavior:
- **Ask for approval** (`prompt`): the safe default for existing installations.
- **Full Access** (`automatic`): retain the existing warning styling and explanation.

Suggested row description: “Choose how permissions are approved for new sessions.” Keep the existing host-ownership footer.

## Non-goals and scope

No new permission modes, approval rules, dependencies, settings category, browser-local default, or permission enforcement mechanism. Do not change running/resumed sessions, child inheritance, headless policy, or remembered allow rules. Keep the existing fork-specific Ask behavior in this change rather than silently broadening fork permissions. Explicit launch/session choices always win.

## Existing implementation and design

- `packages/app/src/settings/configuration.tsx` owns the host-scoped query, TanStack form, category-scoped patches, revision conflicts, dirty state, and Save host defaults action. Extend this path rather than adding another settings store.
- `packages/app/src/settings/section-layout.tsx` provides `SettingsGroup` and `SettingRow`.
- `packages/app/src/permission-mode.tsx` already provides `PermissionModeControl`, shared by the welcome composer and active sessions, using `@whip/ui` Button and Popover. Reuse its choices, descriptions, warning treatment, pending/error behavior, and styling tokens. Add only a small settings trigger presentation/accessible-label option so it aligns with the existing settings controls; do not build a second permission picker.
- `packages/app/src/settings/navigation.ts` owns search entries and focus targets.
- `packages/app/src/session-tabs.ts` currently requires a permission mode and hardcodes `prompt` in `openNew`. `welcome.tsx` sends that value explicitly, so a backend-only default would never affect these sessions.
- `internal/daemon/control.go` resolves omitted session defaults after command deduplication. Its `sessionDefaults` early return currently considers only routing and execution engine; it must also consider an omitted permission mode.
- `internal/config/config.go`, `internal/protocol/types.go`, and `internal/daemon/provider_service.go` own persisted configuration, the wire contract, and configuration reads/updates respectively.

Current architectural references: `docs/frontend.md` (state ownership, Settings workspace, host-scoped guarded forms); `docs/features.md` (Built-in capabilities and omitted host defaults); `docs/roadmap.md` (shipped Settings workspace). This extends existing WHIP behavior, not another harness's permission model.

## Ordered implementation

### 1. Persist and expose the host default

- [x] Add `DefaultPermissionMode` / `defaultPermissionMode` to host config and `default_permission_mode` to RuntimeConfiguration and ConfigurationUpdate. Use existing permission vocabulary and validation helpers where available.
- [x] Missing legacy configuration resolves to `prompt`. Reject invalid explicit update values; never interpret unknown input as Full Access.
- [x] Extend the existing revision-checked, atomic configuration read/update path in `provider_service.go`; preserve unrelated configuration fields.
- [x] Regenerate protocol schemas, TypeScript declarations, and validators with the existing generator rather than editing generated files. Check older-host compatibility: a missing field must leave Ask available without offering an unsupported save.

### 2. Apply the default to new sessions

- [x] Resolve omitted permission mode from host config in the daemon's shared session-default path, including when model/provider/engine are already explicit. Keep tool-host behavior unchanged.
- [x] Resolve only after deduplication and persist the selected mode through the existing session store. A retry after a host-default change must return the original session unchanged.
- [x] Allow an unset permission override in new-chat tab state, analogous to model/effort. Remove the unconditional `prompt` assignment in `openNew`; validate and persist optional overrides. Existing saved tabs with explicit modes retain those values.
- [x] In `welcome.tsx`, resolve displayed mode as explicit tab choice → selected host default → Ask for a legacy host. Use the same effective value for the picker, skill-completion scope, and create payload.
- [x] Avoid a loading race: until the host default is known, do not submit an unchosen fallback or display Ask while creating a Full Access session. Preserve the explicit choice across refetches and navigation. An unoverridden draft follows its selected host; switching hosts must not reuse another host's cached value.
- [x] Audit session-create callers for forced defaults. Preserve explicit CLI flags and choices; callers that intend to inherit should omit the field. Do not change existing-session or fork semantics as a side effect.

### 3. Add the Settings row

- [x] Add the row below Reasoning effort using `SettingRow` and the shared `PermissionModeControl`. Include the field in the Providers form's values, dirty tracking, reset, and category-owned patch.
- [x] Preserve Save/Discard/Stay, disconnected/submitting disabled states, conflict handling, query cache updates, and per-host isolation. Selecting a value changes the draft only; Save host defaults persists it.
- [x] Register “Default permission level” in settings search with permission/approval/Ask/Full Access keywords and focus the actual control.

### 4. Tests and validation

- [x] Config/daemon tests: legacy fallback, both modes round-trip, invalid values rejected without writes, revision conflicts, explicit overrides, omitted defaults with explicit routing/engine, durable retries, and saved-session isolation. Extend `internal/daemon/session_defaults_test.go` across Unix and WebSocket transports and existing provider configuration tests.
- [x] Frontend tests: row placement, shared picker behavior, save/reset/conflicts, category patch isolation, host switching, pending/failed configuration reads, optional tab-state round-trip, legacy saved tabs, and create payload matching the visible choice. Extend `packages/app/test/permission-mode.test.tsx`, `session-tabs.test.ts`, and `provider-connections.test.tsx`; add a focused welcome-flow test if needed.
- [x] Run protocol generation/drift checks, focused Go tests with `-race`, web tests/typechecking, and the repository's `task check` before shipping. Verify keyboard focus, screen-reader labels, narrow layout, and Full Access warning styling in the real Settings page.
- [x] Update `docs/features.md` to distinguish fresh-session host defaults from existing/fork behavior, and `docs/frontend.md` for the new setting and inherited draft permission state. Update roadmap only where applicable.

## Acceptance criteria

Saving Full Access on host A makes a fresh session on host A display and start in Full Access. Selecting Ask for that session overrides the host default. Host B remains independent. Changing defaults never rewrites an existing session. Existing installations remain Ask until explicitly changed. No duplicate permission picker or parallel settings persistence is introduced.

## Working-tree note

The checkout has substantial pre-existing changes, including welcome, daemon, and generated protocol files. Implementation must preserve those changes, regenerate against the current contract, and stage only intentional changes. Implementation preserves the pre-existing edits; no changes have been staged or committed. Validation results are recorded below.

## Validation results

- `go vet ./...` and `go run ./cmd/whipvet ./...`: passed.
- `go test ./...`: passed on final rerun. An initial run compiled a new tool-host test while it was being edited; its invalid fixture was corrected and the full suite rerun.
- Full config/protocol/session and focused daemon permission/configuration/default regression tests with `-race`: passed.
- `task contract`: passed, including 15 interop tests and generator drift checks.
- `task sdk`: passed; SDK and example checks green.
- `npm run check:web`: passed (TypeScript + production build).
- `npm run test:web`: passed, 1,165 tests across 90 files.
- Focused frontend regression suite: 203 tests across 8 files passed.
- Shared UI typechecking and 14 tests, theme generation check, packaging/dev-proxy scripts, local-update and onboarding-script tests: passed.
- `git diff --check`: clean.
- `task check` stops at existing formatting issues in `.claude/worktrees/session-trace/internal/daemon/session.go` and `.claude/worktrees/session-trace/internal/session/otlp_export.go`. Those unrelated worktree files were not modified; the remaining check commands were run directly as listed above.
- Real Chromium against an isolated current-source daemon and loopback mock provider: Settings row order, draft-only edit, persistence after save/reload, keyboard Enter/Escape/focus return, and 390px layout verified. Fresh-session inheritance and explicit override creation passed. Actual desktop search correctly focuses the new button. Cold direct links leave focus on BODY for both the existing Reasoning effort control and the new permission control; same-route search also does not refocus. This pre-existing shared Settings behavior was documented rather than changed in this feature. Both permission modes were confirmed in isolated SQLite records; changing the host default back to Ask preserved the existing Full Access session. All test jobs and Chromium instances were stopped and production state was untouched. Browser evidence is retained in `/tmp/whip-permission-e2e.Stg5Iy/` (desktop/narrow popovers and both session modes). Browser coverage was Chromium only, using loopback mock responses; successful model-turn completion, VoiceOver, Safari, physical mobile, and multi-host browser coverage were not claimed.
