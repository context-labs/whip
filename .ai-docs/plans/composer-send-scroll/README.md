# Scroll the sending chat to latest

Branch: current working branch

## Goal
After composer admission (including queued and child messages), jump only the author's chat view to latest and resume following. Rejected/uncertain sends preserve reading position. Upward input cancels following, including while latest history is loading. The user approved this behavior in conversation.

## Non-goals
No global session event listener, SDK state, REPL change, native-mobile change, or automatic scroll on background activity. New-session chat already opens at the bottom.

## Design and prior art
Reuse ReadingList's existing Latest action and cancellation/loading path, without smooth animation. A view-local imperative handle passes through Timeline to SessionContent; Composer notifies it only on admission while still mounted for the same recipient. Existing sources: docs/frontend.md (ReadingList and preserve-place principle), docs/features.md (chat follow behavior), packages/app/src/chat-submission.ts (admission callback). No roadmap item or external harness port is needed.

## Tasks
- [x] Wire admission to the existing latest path in composer.tsx, conversation.tsx, timeline.tsx, reading-list.tsx.
- [x] Regression tests for admission/failure/unmount, follow interruption, suffix recovery, and view isolation.
- [x] Update frontend.md and features.md.
- [x] Run final focused tests and adversarial review. Independent read-only review found no correctness issues. Final validation: 127 composer/reading/submission/welcome tests and 1 view-isolation test passed (2 unrelated dock tests skipped by filter). No real-browser scroll verification performed. App typecheck is blocked by pre-existing missing AgentDockRoster in agent-dock.tsx (left untouched). task check is blocked by formatting in .claude/worktrees/session-trace/internal/{daemon/session.go,session/otlp_export.go} (left untouched).
