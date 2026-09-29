# Native permission acceptance

`permission-requests.mjs` now drives the packaged production app and a disposable
native runtime. Two actual concurrent QuickJS `files.write` operations produce
canonical pending permissions; no RootSnapshot, fake Session or renderer reducer
is seeded. The former specialized permission fixture is removed because the
maintained runner exercises the real application directly.

| Retained assertion | Native evidence |
| --- | --- |
| Two queued approvals | Actual same-root `Promise.allSettled` calls, SQL pending count two, one visible card with “1 more waiting” |
| Long requester/tool/command layout | Exact canonical owner and capability stay on one line; bounded long operation arguments remain selectable in explicit disclosure without horizontal overflow |
| Four sizes × three themes × two browsers | Original 1440×900, 390×844, 320×640 and 640×320 cases; light/dark/Claude Code; original 816 px maximum, 12 px margins, exact composer edges and ≤9 px gap |
| Reachable mobile actions | Original ≥43.99 px touch target, scroll into view and viewport checks |
| Keyboard denial, next request, allow once | Actual denied operation never writes its file; distinct approved operation writes exact bytes; no standing write grant appears |
| Scope isolation | Foreign Session handle rejects the first operation before resolution; both actual requests remain pending |
| Strict rendering | Production asset hashes, bounded page errors and CSP evidence, screenshots every layout |

The retired fixture’s inline “remember on this host” / “session and children”
selector represented an old authority model. Native one-operation consent never
silently creates standing or global authority. Explicit standing-grant creation
in Session details is a separately tracked retained workflow; this leaf does not
claim that workflow complete. It also does not create a child approval dialog
where native child authority requires explicit delegation.

All owned setup runs under cleanup. Browser shutdown cannot skip fixture shutdown;
failures retain bounded diagnostics. No installed runtime, real account, or host
configuration is touched.

```sh
npm run pack:web
node apps/web/scripts/permission-requests.mjs
```

`WHIP_PERMISSION_RESULTS` selects artifacts; `WHIP_WEB_BROWSERS` can narrow a
local diagnostic to Chromium or Firefox without changing the default matrix.

Validated on 2026-09-29: all 24 native layouts passed on Chromium
153.0.8010.12 and Firefox 155.0, including the foreign-owner negative and exact
filesystem effects for every case. No page errors or CSP violations occurred.
The packed renderer digest was
`c06ea03a00b18e848547bcad4d0ea1446cc37adea7e6cbab3ff4c1f12ed87ff7`.
Artifacts and exact bounds are in `/tmp/whip-native-permission-final/report.json`
and adjacent screenshots, produced with
`WHIP_PERMISSION_RESULTS=/tmp/whip-native-permission-final node apps/web/scripts/permission-requests.mjs`.

The shared card/composer edge uses the same 0.01 px DOMRect rounding allowance
as the retained mobile touch-target check: a diagnostic measured the shared
edge at 778.0000152587891 vs 778 px. The original 9 px maximum gap, width, margins
and no-overflow assertions remain unchanged.

## Explicit standing authority follow-up

Session details → Permissions now offers explicit standing-grant creation.
Root selection requires the exact capability and resource; child selection offers
only active standing grants from its direct parent, with immutable scope and
32-record replaceable pages. It never selects a grant automatically, widens a
child scope, creates a host-global wildcard, or turns “Allow once” into remembered
authority. Existing pending requests retain their independent decision.

The form captures one stable grant ID and exact payload on the first send. An
uncertain response freezes those fields and offers only explicit same-request
retry; acknowledgement must match ID, owner, issuer, capability and resource.
The form labels an exact retry of a since-revoked grant as revoked. It retires
late results on owner/client changes and explicitly asks the person to keep an
unresolved form open; this form does not claim a persistent recovery journal.
The backend’s immutable grant receipt and live delegation check remain the sole
authority; no configuration revision or synthetic CAS is invented.

Six focused creation tests and 24 existing inspector tests passed, as did the
shared app typecheck. `standing-grants.mjs` passed Chromium and Firefox against
production renderer `4a622528f83758ed982c2685685d2392993fc67e8dc23757f7c6a023fedb0c47`:
actual root creation and effective later write, pending request not auto-approved,
foreign-root denial, host rejection of missing/foreign child issuers, exact child
delegation and effective write, then root revocation removing child authority.
All eight workflow groups reported no page errors or CSP violations.

```sh
WHIP_GRANT_RESULTS=/tmp/whip-standing-grants-final node apps/web/scripts/standing-grants.mjs
```

The former inline global “remember” selector is replaced by this explicit exact
session grant workflow, not reproduced as global wildcard authority.
