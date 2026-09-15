# SSH profile picker

Branch: current working branch

## Goal
Implement the approved Paper SSH setup flow: pick one configured alias, or expand Advanced for the existing manual fields. Preserve URL setup, SSH trust/authentication, cancellation, and verified native persistence.

## Design
- `apps/desktop/src/ssh-profiles.ts`: bounded read-only discovery from local SSH configuration and Include files. Return literal aliases and explicit metadata hints; never execute SSH, Match exec, ProxyCommand, or contact hosts during discovery.
- `desktop-bridge.ts`, preload/main and web desktop adapter: optional typed listSSHProfiles capability. Older hosts retain manual entry; browsers retain URL setup.
- `connection-dialog.tsx`, `host-dialog.tsx`: shared components and theme tokens; lazy Query read, filter/refresh/empty/error, single selection, manual draft retention, pending progress and retry.
- Shared RadioGroup gains a card-row presentation; existing uses remain unchanged.
- Existing `saveNative` owns verification and cancellation; a stable form ID prevents repeated failed attempts from creating records.

## Reference
Paper frames HD9-1, HLC-1, HS6-1, HY3-1; existing `SSHFields`, `SSHConnection`, `saveNative`. No new dependencies or daemon operations.

## Validation and delivery
- [x] Bounded parser fixtures: includes, wildcard/negated aliases, quoted entries, duplicates, cycles, missing/error, conditional directives, size limits.
- [x] App dialog: lazy load, radio keyboard/filter, Added, manual switching, errors/refresh, cancel/retry/save.
- [x] Desktop and frontend type/build checks, affected test suites, browser light/dark/narrow review.
- [x] Update frontend guide/features and review the final diff. No session persistence changes.


## Completed validation
- `task check`: passed, including 59 app test files / 625 tests and Go/contract/SDK/build checks.
- Desktop suite: 90 passed; 9 existing native SSH integration cases require an opt-in built helper and were skipped.
- Desktop, app and shared UI TypeScript checks passed.
- Chromium and Firefox: desktop, phone and short viewports; light/dark; loading height, empty/error/retry, selection, manual draft retention, submit and CSP. Evidence: `/tmp/whip-ssh-profile-results/report.json` and screenshots.
- Live Electron app: Add server → SSH loaded the real local profiles through the new bridge. No remote connection initiated; dialog left open.
- Review fixes: keep query dependency out of legacy manual/edit paths; retain drafts across method tabs; clear removed selections on refresh; reuse attempt identity; treat explicit cancellation as returning to the form rather than an error.
