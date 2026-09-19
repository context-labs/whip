# Attachment thumbnails

Implemented September 18, 2026 in the shared desktop/web renderer.

User images appear above the text bubble in a right-aligned wrapping strip of
80 px square thumbnails, with an 8 px gap and themed borders. Each thumbnail
reserves its size while loading, shows a small spinner until ready, and handles
decode failure independently. Image-only messages omit the empty bubble.
Clicking a thumbnail opens its original image in the shared accessible dialog;
Escape closes it and restores keyboard focus. Cached images do not wait for an
already-fired load event. External images retain explicit-link behavior.

No new dependency or fetch path was added. Visible stored messages continue to
use the scoped SDK reads, cancellation, and inactive-cache disposal documented
in `docs/frontend.md`. Native mobile and assistant/tool image layouts are unchanged.

Validation:

- Full web suite: 740 tests across 71 suites passed. Final focused message,
  timeline and reading suite: 40 tests passed.
- App and desktop TypeScript checks passed; desktop production build succeeded.
- Production Chromium, Firefox and staged Electron fixtures passed with five
  attachments of varying aspect ratios. They exercise stored history handles,
  retry, virtual unmount/reload, independent loaders, fixed loading height,
  keyboard preview/dismissal, reduced motion, narrow wrapping, and light theme
  with increased contrast and large text.
- Selected reading-anchor drift after the delayed body transfer was 0 px in all
  three engines. Mounted reading rows were 17 / 17 / 16 respectively.
- The fixture holds native image completion notifications for deterministic
  loader screenshots; history-transfer delays use intercepted content requests.
- `git diff --check` passed.

Screenshots and per-engine measurements are in
[`evidence/attachment-thumbnails`](evidence/attachment-thumbnails/results.json).
Videos are retained in `/tmp/whip-attachment-final-web` and
`/tmp/whip-attachment-final-desktop`.

Staged renderer: `de36cbcfc70c72610af71df029bf3f6ac732c9a5130905a462fe4a1ea0b3984c`.
Desktop output: `apps/desktop/.stage/app`. The user's running app and daemon were
not replaced or restarted.
