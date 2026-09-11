# Error ownership acceptance fixture

Run from the repository root:

```sh
node apps/web/scripts/error-ownership.mjs
node apps/web/scripts/error-ownership.mjs --desktop
```

The first command runs browser assertions and saves screenshots and `report.json`
to `/tmp/whip-error-ownership-results`. The second keeps a visible Electron window
open for manual Computer inspection. Close its window to stop the fixture. Override
`WHIP_ERROR_PORT` (default 4177) and `WHIP_ERROR_RESULTS` to run separate instances.

The renderer mounts the real `createWhipApplication` through the normal bootstrap.
The daemon is the isolated SDK integration fixture, with its own temporary home,
fake provider, and recorded turn/execution outcomes. The toolbar is test-only;
it injects failures at storage or transport boundaries, never by creating error
DOM or changing application state directly. No personal daemon or credentials are
used. The Electron host is a minimal sandboxed window, not the packaged native
bridge; native startup/crash dialogs are outside this fixture's coverage.

Select a scenario in the separate toolbar. Its explanation identifies the app
interaction that triggers the fault. **Restore** removes the fault; then use the
app's recovery action. Close modal dialogs before using the toolbar. Selecting
another scenario resets only this fixture origin's saved state. The scenarios are:

Manual Electron uses the retained long-error child for Turn/Combined, so those
screens remain repeatable. Restore reconnects without changing that historical
outcome. The automated run uses the empty-history child and verifies the daemon's
successful-follow-up control separately.

| Scenario | Actual failure path |
| --- | --- |
| Application | Type a draft; its storage write throws. Restore allows saving the retained text. |
| Host | Disconnect closes the SDK transport and blocks reopening until Restore. |
| Session | Reject `root.snapshot` while all other requests and the host connection remain healthy. Restore, then Refresh. |
| Turn | Read a persisted failed child-agent turn. Restore commits a successful follow-up via the existing daemon test control. |
| Execution | Read a persisted failed cell in the child-agent REPL, alongside successful cells. |
| Submission | Reject the actual `command.submit` for the submitted message. |
| Uncertain | Drop that frame and close the socket; status reads fail until Restore, after which the actual daemon proves the original identity absent. |
| Resource | Reject the actual `provider.catalogs` read; open the composer Model picker. |
| Action | Reject `session.rename`; open sidebar session actions → Rename and submit. |
| Validation | Settings → Servers → Add server; submit `file:///not-a-server` as the address. |
| Combined | Persisted failed turn plus a disconnected host; Restore clears only the connection problem. |

These are representative paths for all nine owners, not exhaustive coverage of
every individual resource, action, validation rule, operating-system dialog, or
failure outcome. Boundary failures use deterministic messages rather than real
disk exhaustion or live provider outages. Successful execution history is retained
alongside the failed execution; historical records are not erased by Restore.
