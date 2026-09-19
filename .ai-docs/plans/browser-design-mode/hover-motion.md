# Hover-outline motion

Status: implemented; focused app, renderer, native and adversarial gates passed.
Repository-wide check remains blocked by unrelated formatting (details below).
Approved 2026-09-18 as a narrow follow-up to [Design Mode](README.md).
The 100ms timing is WHIP's chosen motion token, not a verified Cursor timing.

## Approved behavior

- Only the hover outline animates: one stable DOM node, 100ms ease-out using
  StyleX `appearance.motionFast` and CSS transitions of left/top/width/height.
- Mid-flight moves retarget immediately from the current displayed rectangle;
  no animation queue, dependency, scale transform, or changing border thickness.
- First appearance snaps. Same-element geometry changes and scroll/resize/zoom
  snap. Invalidation, navigation, exit and security hiding clear immediately.
- Native hit-testing remains authoritative: clicks select the actual pointer
  target immediately, not an interpolated box. Selected outlines remain exact;
  tooltip, composer, labels and cursor behavior are unchanged.
- App Reduce Motion and OS reduced motion both suppress interpolation.

## Implementation contract

Native `hover.id` is an opaque consecutive-target identity, stable only for the
same live backend node and independent of selected element IDs. Native increments
`state.hoverGeometryRevision` on geometry/security invalidation and rejects old
in-flight observations. Renderer compares this counter, lease, document/selection
revision and viewport before animating between different hover IDs. Missing
counter means snap; no reliance on delivery of an intermediate cleared snapshot.
Identical snapshots preserve an in-flight transition, while same-ID geometry
updates disable it. CSS owns timing and retargeting, not React timers. OS reduced
motion overrides `transition-property: none`, not only duration: the production
renderer caught that setting duration to zero alone preserves an existing
transition. App reduced motion likewise removes the motion class.

Owned files: `packages/app/src/browser-design-types.ts`,
`browser-design-geometry.ts`, `browser-design-overlay.tsx`;
`packages/app/test/browser-design.test.ts`, `browser-design-overlay.test.tsx`;
native controller/page and fixtures, production renderer fixture,
`docs/frontend.md`, `docs/features.md`.

## Validation gates

- [x] App typecheck and focused motion/interaction/controller/integration tests:
  `npx tsc -p packages/app/tsconfig.json --noEmit` and three browser-design Vitest
  files passed (41/41, 2026-09-18).
- [x] Production renderer: 228 checks, zero failures across Chromium/Firefox ×
  light/dark × 1280/480/320 widths. Measures intermediate geometry, 100ms ease-out,
  stable DOM, mid-flight retargeting, constant stroke, unchanged selection,
  clear/snap and initial/live app and OS reduced motion. Six source hashes
  reverified unchanged; exact artifact/manifest handed to native validation.
- [x] Native production fixture: 22/22 gate groups and
  `DESIGN_PRODUCTION_NATIVE_OK` (2026-09-18 23:38:58Z). Verified stable hover
  identity; layout/scroll/resize/zoom/detach generation changes; stale in-flight
  rejection; true-target native mouse selection while a real CSS transition
  is paused at 25ms; offscreen/nested-overflow keyboard reveal and Enter; and
  reveal-await hide/show ABA rejection. Existing privacy/capture/preload/IPC,
  dropdown, lease and security-hide gates passed. Desktop typecheck passed.
  Exact renderer JS: `design-CsLHLOSJ.js`; artifact manifest SHA-256:
  `330f661965f6070514a098205c72e2112cb2989a52b7bd15a18359bbd74f4cbe`.
  All 17 artifact files, six renderer sources and native source hashes were
  independently verified unchanged before/after the run.
  Log: `/tmp/whip-browser-design-motion-results/native-final.log`.
- [x] Final adversarial review: no remaining source findings after fixes for
  keyboard reveal-scroll self-invalidation, failed-refresh geometry revision,
  live OS cancellation and a later reveal-await hide/show ABA race. Native
  visibility/sequence guards and a deterministic RAF-response barrier regression
  cover the latter; exact-artifact native execution remains separate above.

Additional focused gates: desktop suite passed 136 tests, skipped 10, failed 0
(146 total; `/tmp/whip-hover-motion-desktop-tests.log`, 2026-09-18 23:39Z).
`git diff --check` was clean.

Broad gate: root's `task check` exited 201 at `gofmt -s -l .`, blocked by
unrelated nested-worktree formatting in
`.claude/worktrees/session-trace/internal/daemon/session.go` and
`.claude/worktrees/session-trace/internal/session/otlp_export.go`.
Those files were not changed. Log: `/tmp/whip-hover-motion-task-check.log`.
This is not a passing repository-wide check; focused results above are separate.

No installed app/daemon changes, staging or commits are part of this follow-up.
