# Native Safari application acceptance

The original `safari.mjs` entrypoint now drives the actual installed Safari with
its WebDriver and the disposable native runtime/provider fixture. It does not
select an existing browser window, enable Remote Automation, modify Safari or
system settings, or use a user's runtime/profile. `native-safari-rehearsal.mjs`
runs the same actions with Chromium and explicitly reports **NOT Safari**.

## Retained guarantees

| Original assertion | Native evidence |
| --- | --- |
| Actual Safari user agent | Require Safari and reject Chrome after one owned WebDriver session creation. No WebKit/Chromium fallback. |
| Production renderer and strict CSP | Untouched embedded production assets and gateway CSP; no injected application bootstrap or alternate client. |
| Native form submission and authoritative response | Actual Send action, one provider effect, successful exact-root turn, both canonical user/assistant rows. |
| Independent root tab drafts | Create two real roots, open through the native sidebar, keep independent draft text and exact view IDs. |
| Close/reopen | Actual close and Reopen preserve the original view identity and draft. |
| Theme before attachment and settings while drafting | Owned-origin saved Nord theme before first app navigation, then actual Appearance selection of Dark without submitting. |
| Full reload | Both tab IDs, independent drafts and Dark theme survive actual refresh; provider effects remain unchanged. |
| Browser failures | Strict window error, unhandled rejection and CSP checks from the first ready composer through each document's interactions. Startup must reach the real visible composer within the fixed deadline; WebDriver does not provide a pre-document event injection API, so this does not claim startup console capture. |

## Ownership and bounds

The runner starts `/usr/bin/safaridriver` on a fresh loopback port and attempts
session creation exactly once. A missing machine opt-in fails before starting a
runtime. Every request has a deadline and an 8MiB response bound; driver output
retains at most 64KiB. Teardown deletes only the exact returned session, then
joins the owned driver with TERM/KILL fallback. Runtime cleanup remains
independent even if driver cleanup fails. Cleanup failures fail the report.
The only page state written belongs to the fixture's unique loopback origin.

The driver lifecycle tests use owned disposable HTTP subprocesses, covering
session refusal, one exact deletion, repeated close and a hung deletion plus
TERM-ignoring process. They do not claim Safari acceptance.

## Current evidence

- Three owned-driver lifecycle tests pass, including the five-second deletion
  deadline and joined forced shutdown.
- The actual entrypoint on this machine fails truthfully with Safari's
  `Allow remote automation` prerequisite, before any fixture runtime is created.
  No setting was changed. **Actual Safari acceptance remains pending explicit
  machine opt-in.**
- The shared Chromium rehearsal exercises all five groups above; this is
  workflow validation only, not Safari/WebKit or signed desktop evidence.
- The strict rehearsal exposed a real synchronous transcript row measurement
  ResizeObserver error. The separate ReadingList change uses TanStack Virtual's
  existing animation-frame measurement option; no browser-error suppression is
  introduced.

Run after `npm run pack:web`:

```sh
node --test apps/web/scripts/native-safari-driver.test.mjs
node apps/web/scripts/safari.mjs
# Deliberately separate, never invoked as an automatic fallback:
node apps/web/scripts/native-safari-rehearsal.mjs
```

Reports are written under `WHIP_WEB_BROWSER_RESULTS` (actual Safari) or
`WHIP_SAFARI_REHEARSAL_RESULTS` (Chromium rehearsal). Neither entrypoint enables
machine settings or treats a failed prerequisite as a pass.
