# Native error ownership acceptance fixture

Run from the repository root:

```sh
node apps/web/scripts/error-ownership.mjs
node apps/web/scripts/error-ownership.mjs --desktop
```

The default runs the shared application in Chromium and Firefox and saves
screenshots and `report.json` beneath `/tmp/whip-error-ownership-results`.
`WHIP_WEB_BROWSERS=chromium` or `firefox` narrows a diagnostic.
`--desktop` keeps a disposable sandboxed stock Electron window open for manual
inspection; close the window to stop. Override `WHIP_ERROR_PORT` (default 4177)
and `WHIP_ERROR_RESULTS` for separate instances. This manual host is not the
packaged native bridge or signed release acceptance.

The fixture builds the real `mountApplication` sources with a separate, visibly
labeled toolbar. Fixture CSS reserves the toolbar’s 64 px and gives the startup
containers and shell the remaining height; it does not alter error controls or
their content. Its actual native runtime has a private home, fake local HTTP
provider and real admitted root/child turns and engine cells. No legacy SDK,
RootSnapshot, seeded execution ledger, or old event reducer is used. No installed
runtime or personal accounts are accessed.

Faults enter at storage and WebSocket request boundaries. The test-only socket
subclass permits only this origin’s exact `/api/v4/ws` route; the production SDK
still performs the normal runtime, process-epoch and network-client handshake.
It retains at most 128 live sockets, validates bounded native frames, preserves
response identity, and retires connections on app disposal. Rejected requests
never reach the host. Nothing injects error DOM or mutates application error
state directly.

Select a scenario in the toolbar. **Restore** removes the fault; then use the
app’s recovery action. Close modal dialogs before using the toolbar. Selecting
another scenario resets only this fixture origin’s saved state. Restore in the
Turn scenario explicitly submits one real successful follow-up in that same
child; it does not alter an existing failure record.

| Scenario | Native boundary and retained assertion |
| --- | --- |
| Application | Actual draft storage write throws; the draft survives and saving clears the application error. |
| Host | Close owned sockets and reject sends until Restore; one host notice, no session/global duplicate, retained draft. |
| Session | Reject `sessions.get` while the host stays connected; scoped unavailable state, no misleading new-chat/connecting copy, explicit Refresh. |
| Turn | Actual provider HTTP rejection persists in its exact child; explicit successful follow-up clears current outcome without changing history. |
| Execution | Actual Starlark division by zero beside a separate committed successful cell in the native REPL. |
| Submission | Reject native `sessions.submit` as invalid; composer error and original draft remain. |
| Uncertain | Drop that native frame, close the socket, fail `receipts.match` until Restore; exact original identity then proves absence, with no provider replay. |
| Resource | Reject `providers.catalog`; the model picker owns the failure and explicit Retry. |
| Action | Reject `trees.update`; the rename dialog retains its name and owns the error until explicit retry. |
| Validation | Invalid `file:///not-a-server` stays inside the Add server form. |
| Combined | Real failed turn plus a disconnected host; reconnect clears only the host problem. |

These are representative paths for all nine error owners, not exhaustive coverage
of every resource or OS dialog. Device exhaustion and provider outages are
injected, bounded failures; execution and turn records themselves are genuine.
Raw provider rejection bodies are deliberately not displayed by native adapters.
Setup and cleanup own the browser/Electron, preview server and runtime; one close
failure cannot skip runtime shutdown. Error/CSP evidence survives reloads with
64-entry limits and failure text is capped at 16 KiB.

## Native validation checkpoint (2026-09-29)

`WHIP_ERROR_RESULTS=/tmp/whip-native-error-final node apps/web/scripts/error-ownership.mjs`
passed all 11 workflows in Chromium 153.0.8010.12 and Firefox 155.0. Both reports
have no page errors or CSP violations; every owned browser, preview and native
runtime joined on exit. The report and before/after screenshots are in that
directory. The isolated fixture TypeScript check and runner syntax check pass.

The automated checkpoint exercises the production shared renderer and native
gateway through the dedicated toolbar build. It does not claim packaged Electron,
Finder launch, Developer ID signing or notarization coverage. The optional manual
Electron mode was preserved, but was not rerun for this migration checkpoint.
