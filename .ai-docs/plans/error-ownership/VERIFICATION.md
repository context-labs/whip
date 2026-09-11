# Error ownership verification

The nine approved owners now have canonical UI locations. Errors keep their existing state owners; a shared presentation component provides readable titles, expandable diagnostics, and local recovery. Commands no longer publish duplicate application-wide error banners.

| Owner | Canonical location | Computer evidence |
| --- | --- | --- |
| Application | Application banner | [Storage failure](screenshots/01-application.png), [recovered with draft retained](screenshots/01-application-recovered.png) |
| Host | One host notice in application chrome | [Disconnected](screenshots/02-host.png), [reconnected](screenshots/02-host-recovered.png) |
| Session | Top of the affected session pane | [Snapshot failure](screenshots/03-session.png), [Refresh recovered](screenshots/03-session-recovered.png) |
| Turn | Selected agent’s conversation / latest recorded turn | [Turn failure](screenshots/04-turn.png), [expanded long details](screenshots/04-turn-long-details.png) |
| Execution | Failed tool or REPL cell | [Failed cell](screenshots/05-execution.png) |
| Submission | Composer | [Rejected draft](screenshots/06-submission.png), [uncertain delivery](screenshots/06-submission-uncertain.png), [status checked without resending](screenshots/06-submission-reconciled.png) |
| Resource | Component loading the resource | [Model picker](screenshots/07-resource.png), [Retry recovered](screenshots/07-resource-recovered.png) |
| Action | Initiating control or dialog | [Rename failure](screenshots/08-action.png) |
| Validation | Input or form | [Invalid address](screenshots/09-validation.png), [corrected field](screenshots/09-validation-corrected.png) |

All screenshots above were captured using Computer while interacting with the running shared application in an isolated Electron window. The fixture uses a private real daemon, persisted turn/execution records, and controlled failures at transport or platform boundaries. It does not modify the user's runtime or credentials. The fixture toolbar is test-only.

Additional Computer checks: [host plus turn](screenshots/10-combined.png), [different agents in narrow split panes](screenshots/11-split-scope.png), [one host notice across both panes](screenshots/11-split-host.png). Long diagnostics remain bounded and expandable. Reconnect clears the host failure while retaining the recorded failed turn.

The actual native desktop main/preload and normal renderer also ran with a private local daemon and isolated device data: [native launch](screenshots/12-native-desktop.png). This separately exercises the real native bridge, while the nine-type fixture provides repeatable controlled failures.

## Automated coverage

- 530 frontend tests pass, including focused regressions for local ownership, stale asynchronous completions, cancellation/interruption, accepted commands that fail before a turn starts, uncertain delivery, and recovery.
- Eleven end-to-end fixture workflows, all passing with zero page errors. Includes recovery, no global duplicates, combined host/turn failures, and a later successful turn replacing the latest failed outcome.
- Repository `task check`, desktop type/build check, and frontend type/build check passed. The final frontend checks also cover fixes found during Computer verification.
- Adversarial review fixes: no duplicate submission notice for a newly recorded matching turn outcome; no stale history-action failure leaking into a different dialog; automatic local runtime probes defer to an existing host failure.

## Limits

The current protocol exposes `agent.last_turn`, not a complete indexed turn history with transcript boundaries. The UI places the latest known outcome at the end of that agent's conversation (or in its empty conversation). A later outcome replaces it. This change does not invent historical placement or add a new daemon history API. Execution failures remain attached to their persisted cells.

Native launch and local transport failure are checked separately; this is not a full platform matrix of SSH provisioning, updater, installer, or every OS dialog. Component tests cover their ownership behavior where applicable.

The native transport check reproduced the original `whip:openTransport` / `ENOENT daemon.sock` failure using only the isolated daemon. [Native host failure](screenshots/12-native-host-failure.png) shows that diagnostic once, under the host notice. The dependent local runtime setup displays availability and Retry connection without a second error alert.

After restoring the private service, the native bridge reconnected, cleared the host notice, and restored the new-chat view: [native recovery](screenshots/12-native-recovered.png). Retry while the service remained stopped kept the error with its host. Daemon restart policy was not changed.
