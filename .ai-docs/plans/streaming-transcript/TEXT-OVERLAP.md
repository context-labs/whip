# Streamed text overlap — 2026-09-17

Reproduced the reported jumbling in the production browser fixture by inserting
a usage event between text deltas with the same presentation part ID. The SDK
only joined adjacent deltas; the app gave the resulting fragments identical row
IDs. React keys, Markdown cache entries and virtualizer measurements collided.
Repeated renders accumulated orphaned copies of paragraphs at overlapping offsets.
The pre-fix browser assertion failed and [its screenshot](evidence/text-overlap/before.png)
shows the repeated text.

The SDK now joins continuous identified text/reasoning across usage and host
updates. Notices, discarded streams, different identities and missing data still
separate parts; telemetry cannot clear an unresolved gap. The app gives separate
retained fragments distinct keys and keeps their durable part as a reading alias.
No CSS workaround, new dependency or scroll behavior change was needed.

Validation:

- The SDK regression failed before the fix. All 336 SDK tests passed; the final
  eight focused cases also cover reconnect, refresh, boundaries and partial root
  and child presentation.
- All 661 UI tests in 66 suites passed, including a regression for fragment IDs
  and separate Markdown cache entries. Shared app typecheck and production web
  and staged desktop builds passed.
- The real daemon fixture passed in Chromium, Firefox and Electron. It verifies
  continuous Markdown around interleaved usage/host events, three numbered list
  entries, unique mounted row IDs and non-overlapping measured row bounds.
  Narrow panes, reflow, streaming scroll interruption, selection, reduced motion,
  themes, large type and Electron 400% zoom remain covered.
- The changed Go fixture's bounded-control test passed with the race detector
  and integration build tag. Diff whitespace checks passed.

Screenshots: [Electron](evidence/text-overlap/electron-numbered-list.png),
[Chromium](evidence/text-overlap/chromium-numbered-list.png),
[Firefox narrow pane](evidence/text-overlap/firefox-numbered-list-narrow.png).
Browser reports are in the same evidence directory. Videos remain in
`/tmp/whip-chat-overlap-fixed` and `/tmp/whip-chat-overlap-electron`.

Tested renderer: `ded7d860b43daa30e6558c701b48b085a4da5077e01ba5624176046a4ecf03cb`.
Validation used the isolated staged desktop app; it did not replace the installed
user app. The maintained architecture is in `docs/frontend.md`.
