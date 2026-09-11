# Canonical error ownership

Branch: current working branch

## Goal

Each error has one owner and one canonical display. Implement the user-approved nine-type plan: application, host, session, turn, execution, submission, resource, action, validation. Keep technical details expandable and recovery beside the failure. Preserve drafts, uncertain delivery, and historical execution outcomes.

## Non-goals

No new error database, notification queue, daemon recovery policy, dependencies, or automatic submission retries. Do not modify the user's running daemon.

## Design and existing sources

`packages/app/src/error-feedback.tsx` defines a small presentation contract over existing state owners. `runtime.ts` retains application persistence notices and command observations but no longer forwards command failures globally. `shell.tsx` owns one host error per host; `conversation.tsx` suppresses dependent connection failures and owns session feedback. Composer owns submissions. Turn and execution records remain local to their conversation/REPL evidence. Settings and resource components retain local state and use the common presentation.

Existing sources: `docs/frontend.md` (state ownership, mutations, desktop lifetimes); `docs/features.md` (React web app and desktop); `packages/app/src/{runtime,connection-notice,agent-turn-notice,composer}.tsx` and `details/shared.tsx`. This is consolidation of existing Whip behavior, not a port from another harness.

## Work

- [x] Shared presentation contract and canonical host/application/session displays
- [x] Submission, turn, execution ownership and duplicate suppression
- [x] Local resource/action/validation migration
- [x] Focused behavior tests and frontend/build checks
- [x] Isolated desktop acceptance with Computer and screenshots for every type
- [x] Adversarial review, documentation, and verification report

## Verification

Exercise each of nine types through actual components/application paths using isolated fixture failures. Capture visible desktop failures and recovery with Computer. Include failed turn plus disconnected host, tab/split scope, input retention, no duplicate banners/toasts, long details, narrow layout, and native shell launch. Run relevant app tests, web/desktop type/build checks, and repository check; document any environmental failures truthfully.

## Outcome

Implemented all nine canonical ownership types with existing state owners and a small shared presentation contract. Completed Computer verification for each type, split panes, long diagnostics, recovery, and the actual native socket failure from the original screenshot. See [verification and screenshots](VERIFICATION.md).

Validation: 530 frontend tests pass; eleven end-to-end error workflows pass with zero page errors; `task check`, `npm run check:web`, and `npm run check:desktop` pass. Adversarial findings and the native probe duplicate are fixed with regression tests.

Protocol limit retained: `last_turn` exposes only the latest outcome. This implementation does not add a historical-turn API or claim exact placement of all prior turn failures. A later successful outcome replaces the latest failed turn notice; persisted execution-cell failures remain attached to their cells.

The acceptance runner is `node apps/web/scripts/error-ownership.mjs`; add `--desktop` for a Computer-driven Electron window. It creates an isolated daemon and test-only fault controls. No production recovery policy, credentials, or daemon configuration was changed.
