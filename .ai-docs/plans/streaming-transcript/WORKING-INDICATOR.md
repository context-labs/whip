# Immediate inline activity — 2026-09-17

The shared desktop/web transcript now shows Sending immediately from the existing
local submission preview. It hands over to the SDK's active turn before any
response event is required, then uses actual tool/response phases throughout work.
Queued and uncertain delivery, reconnecting, and human-input waits stay distinct.
Completed turns remove the trailer; existing outcome notices own failures.

The UI activity indicator has a matrix variant: nine theme-colored dots with a
750ms upward opacity wave, adapted from Zeron's MIT-licensed loader. Generic
thinking captions rotate every 7 seconds. The elapsed counter uses the current
turn's recorded start when available and otherwise client observation time.
Sending has no counter. Hidden windows stop the clock, and shared reduced motion
stops the wave and caption rotation. The information bar remains the sole activity
live region; the new line lives inside the transcript's existing reading scroller.

No additional SDK subscription, request, event reducer, dependency, or history
record was introduced. Native mobile presentation is unchanged.

Validation:

- 660 web tests across 66 suites passed; 62 focused composer/activity/reading
  tests passed, with the final delivery-wait guard covered by 17 activity tests.
- Shared app typecheck, production web build, and staged Electron build passed.
- Chromium and Firefox held the outgoing request to verify Sending before host
  admission, then held the first token to verify Thinking, 7-second rotation,
  and an elapsed timer with no assistant content. Response streaming, queued
  input, reload, completion removal, narrow layout, themes and CSP checks passed.
- Electron's production activity fixture verified the inline matrix and actual
  operation label, reduced motion, 400% zoom, narrow panes, streaming, and user
  interruption of bottom following.

The tested renderer digest is
`8ebec9d392f9ce8a722defea49586dbc4a0a7c5698d88afa87b6b97f2f543c4a`.
Screenshots and logs are retained in `evidence/working-indicator/`, including
[Sending](evidence/working-indicator/chromium-sending.png),
[Thinking before the first token](evidence/working-indicator/chromium-thinking.png),
and [Electron](evidence/working-indicator/electron-running.png).
The installed user app was not replaced; validation used an isolated staged app.

The maintained architecture is in [the frontend guide](../../../docs/frontend.md#conversation-and-navigation-patterns).
