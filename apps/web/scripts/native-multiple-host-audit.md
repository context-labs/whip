# Native multiple-host acceptance audit

Append-only retained scenario mapping and production-browser evidence for
`multiple-hosts.mjs`. Every host owns an isolated native runtime, database,
configuration, working directory and synthetic loopback HTTP provider. Remote
hosts trust only the explicit Local fixture origin. No installed runtime or
real provider account is accessed.

## 2026-09-29 — retained scenario mapping

| Retained scenario | Native proof |
| --- | --- |
| Add two verified URL profiles; rediscover in a fresh window | Actual `hosts.profiles`/CAS-backed UI, persisted only on Local; fresh browser context sees all three hosts |
| Per-host directories and model defaults | Native provider/default declarations, explicit host directory picker and first-message root creation; exact session metadata and foreign-host `NOT_FOUND` |
| Directory Go does not submit the enclosing creation form | Both `trees.create` and `sessions.submit` traffic remain unchanged until explicit Send first message |
| Three mixed-host panes and independent drafts | Ordinary sidebar/tab/split actions, exact runtime/root tuples, separate drafts and concurrent actual held provider turns |
| Search and permission attention remain scoped | Host labels/filtering, no hidden session hydration, foreign-host decision rejection and exactly one UI approval routed to Remote A |
| Remote attachment stays on its host | Original scoped bytes read from Remote B; Local/A reject the same reference; explicit attachment preview displays the original text only in B |
| Remote and Local process outages | Healthy Remote B continues accepting work; partial search labels the offline host; drafts survive same-runtime reconnect without mutation replay |
| Explicit disconnect does not stop accepted work | Held Remote A turn completes while the UI is detached; explicit reconnect reads the settled result |
| Remove/re-add a saved host | Unavailable tab and saved draft retain runtime/root identity; other hosts work; new profile ID restores the same runtime and never submits the draft |
| Reload and bounded observations | Exact mixed layout and draft restoration, separate warm-lease and steady three-pane observation ceilings, no page/CSP/global errors |

Retired protocol details are replaced explicitly:

- Native clients initialize once with pinned runtime/process identity. Restarted
  test clients reconnect explicitly; the UI reconnects observations only. There
  is no legacy `connect()` facade or root snapshot/event reducer.
- Native Disconnect includes an explicit confirmation; the probe confirms it
  and proves the Remote A sockets are gone before releasing accepted work.
  Removed hosts expose a scoped unavailable status and no composer instead of
  the retired unavailable heading.
- New-chat view IDs remain distinct from session IDs. The runner resolves the
  persisted view by the exact runtime/root tuple instead of assuming equality.
- New roots are created by explicit first-message submission. Directory browsing
  remains read-only and cannot accidentally admit either creation or input.
- Native attachment bodies are explicit scoped reads. The test opens Preview
  instead of requiring eager transcript hydration from a retired full snapshot.
- Canonical app session leases remain warm for up to 30 seconds after the last
  consumer releases them, within the documented 16-lease cap. The runner proves
  expiry of all three closed creation views before measuring steady three-pane
  traffic. Search/attention cannot introduce a hidden owner; steady observation
  remains bounded to four simultaneous requests. This replaces the retired
  immediate-unsubscribe assumption, not the bounded ownership assertion.

Traffic evidence is capped at 40,000 metadata-only frames, and page/CSP errors
are bounded across reloads. Asset hashes are checked before each browser starts.
Browser and all three owned runtimes are closed through joined cleanup even if
setup or an assertion fails. Final browser evidence is appended only after the
complete required Chromium/Firefox run passes.

The migration also exposed a separate product bug: an attachment preview opened
from a locally submitted row can close when that row's attachment subtree is
replaced by canonical metadata. The persisted-body assertion deliberately selects
the exact committed sequence. Preview continuity across same-input confirmation
is tracked for a separate product regression/fix; it is not claimed covered or
retired by this harness migration.

## 2026-09-29 — complete Chromium and Firefox gate

Both browsers passed all nine workflow groups in the final combined run, exit 0.
Each verified exact wrong-host session, permission and content rejection, both
real process crashes/restarts with unchanged runtime IDs and fresh epochs, no
provider work replay, confirmed disconnect while accepted work completes, saved
profile removal/re-add with preserved draft, and exact mixed-layout reload.
Both reported zero page/CSP errors and no global error banner. The warm and
steady counters measure simultaneous **outstanding `sessions.observe` requests**,
not retained lease count. Chromium observed a maximum of two in each phase;
Firefox observed one warm and two steady requests. The three closed
creation owners stopped polling after the documented 30-second unused expiry.

The verified production renderer digest was
`229f4aa812e464674a3e0046b057d084f55060ad52045c3c657629c03db6e14a`.

```sh
npm run pack:web
node apps/web/scripts/multiple-hosts.mjs
```

The final run used only `WHIP_WEB_MULTIPLE_HOST_RESULTS` to select
`/tmp/whip-native-multihost-final-results`; the default Chromium/Firefox matrix
was unchanged. Exact output is `/tmp/whip-native-multihost-final.log`, and the
result directory contains structured per-browser checks and screenshots. Syntax
and whitespace checks passed. All owned browsers and six disposable runtime
lifetimes joined cleanup. This checkpoint makes no Electron, Safari, signing or
notarization claim.
