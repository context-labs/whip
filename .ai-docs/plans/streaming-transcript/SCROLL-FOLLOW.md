# Transcript following correction — 2026-09-17

The transcript now stays at the actual bottom during streaming until the reader
scrolls upward or selects text. Following resumes only after a downward user
scroll reaches the bottom (2px rounding tolerance), or through **Latest**.

The continuous spring and its custom row-resize predicate were removed. TanStack
handles end anchoring and reading-position compensation. An exact bottom
correction on append and layout also covers the surrounding padding, turn footer
and viewport/composer resizing. Indexed `followOnAppend` remains disabled so a
pending scroll target cannot resume after the reader interrupts following.
`overflow-anchor: none` remains on the virtualized
scroller so native browser anchoring does not compete with virtual measurements.

The previous handlers detached on ordinary clicks, Tab and Enter, then could
reattach during a small upward scroll inside the old 70px proximity threshold.
Direction and explicit following intent now determine attachment. Programmatic
movement cannot resume following or trigger history loading. The default
TanStack resize predicate also avoids dragging the viewport when a paragraph
spanning its top edge grows below the visible text.

Only **Latest** uses native smooth scrolling; new scroll input cancels it.
Reduced motion jumps immediately. Pointer presses temporarily pause automatic
movement through the click so live layout changes cannot move an action away
between pointer-down and pointer-up. This pause does not discard following intent.
REPL retains its existing following behavior.

## Verification

- Shared app typecheck, production renderer build and staged Electron build.
- 658 web tests across 66 suites; 31 focused transcript/reading tests.
- Chromium and Firefox history/prepend/Latest reading-position fixtures.
- Electron, Chromium and Firefox production activity fixtures passed. All three
  finished with a 0px bottom gap and unchanged settled reading offsets through
  subsequent output after both ordinary and overlapping upward gestures.
- The production activity fixture now streams a tall Markdown paragraph and adds
  new blocks while testing pinned growth, a 12px upward wheel movement, stationary
  reading through more output, downward return, ordinary clicks and interruption
  of Latest. The small upward gesture overlaps an arriving block to exercise
  interruption of pending append work. Assertions require a bottom gap of at most
  2px, and unchanged reading
  offsets while detached. Preceding activity folds settle before the absolute
  offset assertions, since folding content above correctly changes scrollTop.
  Wheel movement must remain settled for 300ms before the next chunk, accounting
  for Firefox's asynchronous compositor scrolling without relaxing the assertions.

The tested renderer digest is
`0d5bddab437a17bbcbe475242525747a6036f306d4fe4cad909ffb7d2fefe166`.
[Validation data](evidence/scroll-follow/validation.json),
[surface results](evidence/scroll-follow/results.json), screenshots, build/test
logs and new [Electron](evidence/scroll-follow/electron-streaming.webm),
[Chromium](evidence/scroll-follow/chromium-streaming.webm) and
[Firefox](evidence/scroll-follow/firefox-streaming.webm) recordings are retained
in `evidence/scroll-follow/`. The staged Electron app uses the same renderer as
the isolated daemon's embedded web app; the installed user app was not replaced.

The updated architecture is maintained in
[the frontend guide](../../../docs/frontend.md#conversation-and-navigation-patterns).
This correction supersedes the original spring-following behavior described in
the September 16 implementation report. Its historical benchmark numbers have
not been reused as measurements of this correction.
