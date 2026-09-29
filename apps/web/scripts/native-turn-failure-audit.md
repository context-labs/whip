# Native turn-failure acceptance

The maintained `turn-failures.mjs` entry point uses the production native runtime,
packaged renderer, HTTP provider adapter and SDK v4. It creates actual children
from immutable named definitions, submits real work, and inspects persisted turn
outcomes. It does not seed a legacy snapshot, reducer or synthetic turn ledger.

## Retained scenario mapping

| Previous assertion | Native equivalent |
| --- | --- |
| Failed child name | Exact immutable definition display name and runtime/session/turn error-owner identity |
| Empty failed execution view | Actual rejected first provider attempt; no execution cell is invented |
| Failed turn after an existing cell | Real completed Starlark cell followed by a separate failed provider turn; original cell remains visible |
| Large recorded raw provider error and truncation label | Native HTTP errors intentionally exclude raw provider response bodies. A 50,000-byte private synthetic body yields the exact short canonical HTTP 400 failure, and its marker is absent from both the persisted turn and UI. The former raw-body truncation label is not part of this contract. |
| Claude Code/light/dark and desktop/phone | All three variants at 1280 and 390 px in each theme; horizontal overflow, full notice bounds and original 321 px maximum retained |
| Keyboard error details | Focus the actual disclosure and press Enter; verify visible exact canonical failure text |
| Chat/reload/restart | Same failed turn identity persists through document reload and an actual killed/restarted runtime; old process-pinned client is rejected |
| Subsequent success clears failure | Real successful follow-up in the failed child clears its notice; another failed child's notice remains |
| No incidental work from inspection | Provider effect list stays exact through inspection and restart; renderer never dispatches creation/submission/spawn/lifecycle/cancel |
| Error and CSP checks | Bounded Node-owned error/CSP evidence survives document reloads |

The healthy parent is explicitly inspected to ensure a child's failure is not
presented as the parent's failure. Three rejected inputs each produce exactly one
provider attempt. Setup waits for each exact durable child-completion report and
the parent to become idle before freezing the effects baseline. The later
success admits exactly its authored input and the expected parent report;
normal child reporting is not mislabeled as observation replay. All production asset bytes are checked against the packed
renderer manifest before UI assertions.

Resource ownership starts before browser creation. Cleanup independently joins
the browser and fixture, including setup failures. Evidence retains at most 128
method counters, 64 page errors, 64 CSP violations and 16 KiB of a failed page's
text; no provider response body is copied into these records.

## Invocation

```sh
npm run pack:web
node --test apps/web/scripts/native-fixture.test.mjs
node apps/web/scripts/turn-failures.mjs
```

`WHIP_WEB_BROWSERS=chromium` or `firefox` narrows a diagnostic run.
`WHIP_TURN_FAILURE_RESULTS` selects an owned results directory. No installed
runtime, live account, host settings or system trust configuration is used.

## Validated checkpoint

The full native matrix passed on 2026-09-29: Chromium 153.0.8010.12 and
Firefox 155.0, eight workflow groups each, including 18 layout pages per browser.
Both reported zero page errors and zero CSP violations. Each runtime recorded
exactly 11 provider attempts after the successful follow-up and its parent report.
The packed renderer digest was
`c06ea03a00b18e848547bcad4d0ea1446cc37adea7e6cbab3ff4c1f12ed87ff7`.

The invocation used `WHIP_TURN_FAILURE_RESULTS=/tmp/whip-native-turn-failures-final`;
its `report.json` and screenshots retain the exact owner/turn identities and
request counters. The three native fixture process tests passed separately,
including the 50,000-byte response sanitization and rejection-option bounds.
