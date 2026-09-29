# `/goal-from-context`

Use `/goal-from-context` to formulate a goal from the selected session's recent
conversation. The default window is eight raw messages; an explicit window can
contain 2–100 messages:

```text
/goal-from-context
/goal-from-context 20
```

The native host captures the source history revision and window at admission.
It queues a maintenance input using an exact recoverable request identity. The
captured main model proposes a goal without running conversation tools or
starting a second agent loop. Acceptance of the request does not mean the goal
has been formulated or activated.

A valid candidate becomes the current goal only if the captured goal and history
conditions still match. A later goal change or history edit cannot be overwritten
by an older candidate. Candidate text, rejection and billed model evidence remain
inspectable. Uncertain or interrupted provider work is not automatically repeated.

The terminal requests activation when formulation succeeds. Additional goal
continuations use the configured bound captured at admission; stopping the
terminal observer does not cancel accepted host work. Inspect or control the
current goal with:

```text
/goal status
/goal resume
/goal clear
```

`/goal clear` cancels the goal. Explicit `/stop` stops the selected session's work.
A model completes a native goal through `goals.complete`; the host does not scan
ordinary prose for `GOAL_MET`. Resuming names the current goal revision rather
than silently adopting a replacement.

Empty history, disabled goals, an oversized source window or an invalid model
response fails visibly. Pending and uncertain command delivery stays in the
native recovery journal; inspect it before deliberately retrying the original
request. Closing the client is not an execution-cancellation command.

The SDK exposes the same session goal services and durable formulation command.
See [the SDK guide](../packages/sdk/README.md),
[goal formulation values](../internal/session/goal_formulation.go),
[atomic admission and settlement](../internal/store/goal_formulation.go),
[native runtime](../internal/runtime/goal_formulation.go), and
[terminal controls](../internal/tui/native_goals.go).
