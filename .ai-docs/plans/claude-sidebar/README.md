# Claude-style navigation

Branch: current working checkout (user's existing documentation edits preserved).

## Goal and reference

Implement the user-approved sidebar plan: Paper's compact hierarchy adapted to
WHIP's theme system, directory-grouped saved sessions, top New/Search/Settings,
resizing and hiding, and explicit directory-prefilled creation.
Reference: https://app.paper.design/file/01M1YJSY1KZZMV9R11WBK1WBAN/1-0/2-0
Canonical architecture: `docs/frontend.md`.

No SDK/protocol changes, new dependencies, repo/worktree merging, invented live
status, or conversation/tab redesign. Reuse SessionListView, TanStack Virtual,
existing UI menus, and the current creation form.

## Implementation

- [x] App-owned sidebar projection and bounded window layout preferences.
- [x] Grouped virtual rows, search, directory creation links, keyboard resize.
- [x] Preserve route/tab semantics; creation URLs beat startup tab restoration.
- [x] Unit/component and isolated production-browser validation.
- [x] Paper comparison at 320/420 px; light/dark/mobile/all-theme checks.
- [x] Adversarial review and fixes.
- [x] Update canonical frontend guide and feature map.

State limits: four hosts, 64 collapsed paths per host, 64 KiB serialized layout.
Group only loaded catalog pages. Keep normal links and no root subscriptions for
navigation labels. Width 256–420 px, default 320; leave 480 px for main pane.
Mobile uses existing Sheet and >=44 px controls.

Validation: `npm run check:web`, `npm run test:web`, packed-app sidebar and existing
browser/session-tab scenarios, and repository `task check`. Use isolated fake
provider daemons, never restart the user's daemon.

## Validation completed

- `task check`: passed on the final source, including Go vet/tests, protocol/SDK
  checks, production frontend build, 117 frontend tests and packaging checks.
- `apps/web/scripts/sidebar.mjs`: passed Chromium and Firefox with 140 synthetic
  sessions, exact-directory/worktree labels, pagination, refresh anchoring,
  native middle-click, remembered inspector URLs, separate window layouts,
  creation prefills, keyboard/pointer resize and mobile list/footer containment.
- All 65 themes checked in Chromium; desktop 320/420 px and light/dark/mobile
  screenshots inspected against Paper's hierarchy and density.
- Existing `browser.mjs` and `session-tabs.mjs`: passed Chromium and Firefox;
  tab suite retained at most four root subscriptions.
- Adversarial review fixed replayed search commands and mobile containment;
  follow-up found no blocking issues. Root-level duplicate directory labels
  also have a regression test.
- `git diff --check`: clean. No dependencies or daemon contracts changed.

Browser artifacts: `/tmp/whip-sidebar-results/sidebar.json` and adjacent PNGs.
Actual Safari, physical mobile devices and VoiceOver were not exercised.

## Hover-detail refinement

Applied the user's follow-up reference: desktop session menus reveal on row hover,
directory carets trail the label and reveal on the heading/children hover, folder
headings have no hover fill, and session hover/selection palette roles are swapped.
The existing tab-control StyleX marker convention preserves keyboard-focus,
open-menu and touch visibility. Virtual rows retain directory hover association
when their positions change beneath the pointer. No theme palette was changed.
Validation: production build, 117 frontend tests, and added Chromium/Firefox
browser assertions for visibility, caret placement, transparent headings,
hover/selected fills and open-menu focus behavior; all-theme/mobile coverage stays
in the sidebar browser suite.

## Search dialog refinement

Replaced inline sidebar search with the user's referenced centered dialog.
The shell owns opening/closing; the dialog alone mounts its list/query observers.
Recent results are capped at 64, filtered results use existing bounded SDK pages,
and native links preserve inspector state and background opening. Arrow keys and
Enter select results; Escape/close returns focus. Mobile closes the navigation
Sheet before opening search. Relative dates and quiet action menus retain theme
and touch conventions.

The shared Dialog gained optional header content (with its accessible title
preserved) and a final-focus destination. Removed obsolete inline-search code.
Validation: app production build, 117 app tests, UI typecheck and 9 UI tests;
Chromium/Firefox search workflows including an Axe serious/critical check,
existing application regressions, and updated sidebar/all-theme workflows.
Search screenshots: `/tmp/whip-session-search-results/`.
