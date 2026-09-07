# WHIP web application

The browser attaches to WHIP's daemon. The daemon owns execution, credentials,
permissions and durable sessions; closing a browser does not cancel its work.
Multiple clients can use the same daemon concurrently. This first release is
focused on conversations and directing agents. Editors, code review and
standalone terminals remain later work.

Draft text is application-owned and saved separately from command recovery
metadata. Draft admission allows 32 non-empty drafts, 256 KiB each and 1 MiB
total, scoped by runtime, session and recipient. Changes are saved after 150 ms and
flushed synchronously when leaving the page. Each recipient has its own storage
entry, so saving one tab never overwrites another recipient's draft. Competing
edits to the same recipient use the last explicit write. Reconnecting never
submits a draft automatically. Aggregate admission is checked against the current
storage contents; simultaneous additions in separate tabs can temporarily exceed
the total. Further writes then require clearing or explicitly discarding drafts.
Attachments and provider secrets are not saved with drafts. If browser storage
is denied or full, the app reports that its memory fallback lasts only until
the page closes or reloads.

A missing command acknowledgement locks the matching draft against a new-ID
resend while the app checks authoritative status. Accepted commands continue to
completion; a definitive missing record offers an explicit retry with the original
identity and payload. Failed lookups remain unresolved. Settings → Recovery can
inspect and forget saved identities after reload; those records contain no prompts.
Its separate, confirmed discard action clears unsent drafts, including drafts for
removed sessions, without deleting command identities or device preferences.

## Run the packaged application locally

Release binaries contain the web application. Start a daemon with its optional
network listener enabled, then open the discovered endpoint:

```sh
WHIP_NETWORK=1 whip daemon start
whip web
```

The listener defaults to an ephemeral loopback port. `whip web` discovers it,
checks the packaged application and opens your default browser. To print the URL:

```sh
whip web --no-open
```

Networking remains disabled for ordinary daemon startup. `whip web` never starts,
replaces or reconfigures a daemon. If one is already running without networking,
enable it with an explicit restart:

```sh
WHIP_NETWORK=1 whip daemon restart
whip web
```

Restarting interrupts active runtime work. Building a newer client alone does
not replace an existing daemon or its embedded application.

## Build from source

Use Node 24 and the Go version declared in `go.mod`:

```sh
npm ci
task build
WHIP_NETWORK=1 ./whip daemon start
./whip web
```

`task build` builds the web workspace, copies its output into `internal/webassets/dist`,
and embeds it in `./whip`. `task install` packages the same application before
installing the binary. Generated assets are ignored; the tracked placeholder lets
plain `go build ./cmd/whip` work without Node. Such a build reports an actionable
missing-assets error when no browser bundle has been packaged.

For individual packaging steps:

```sh
npm run build:web
node scripts/pack-web.mjs
go build -o whip ./cmd/whip
```

When replacing a running source-built daemon, use your rebuilt binary explicitly:

```sh
WHIP_NETWORK=1 ./whip daemon restart
./whip web
```

## Develop against an existing daemon

The Vite workspace reads `WHIP_WEB_DAEMON` for its `/api` proxy. Start or explicitly
restart your development daemon with a known loopback port and allow the exact
browser development origin. Example values:

```sh
WHIP_NETWORK=1 WHIP_LISTEN=127.0.0.1:8080 \
  WHIP_ALLOWED_ORIGINS=http://localhost:3000 \
  WHIP_ALLOWED_HOSTS=127.0.0.1:8080 \
  ./whip daemon start
WHIP_WEB_DAEMON=http://127.0.0.1:8080 npm run dev:web
```

Open `http://localhost:3000`. An existing daemon retains the configuration it was
started with; substitute `daemon restart` explicitly when changing listener
settings. The release application uses its own origin and needs no copied port
or separately configured browser origin.

## Trusted-network and phone access

Bind to an explicit host address with `WHIP_LISTEN`, and configure the exact Host
and Origin values clients will use. These are request-boundary checks, not an
authentication scheme. Connected clients can approve or deny human permission
requests; internal agent capabilities and content grants still apply.

Phone access needs HTTPS for browser cryptographic APIs and Web Locks. Command
recovery writes use an origin-scoped lock so concurrent tabs cannot drop one
another's accepted command identities. Missing or denied locks produce an
actionable error before command delivery. Supply TLS through your
trusted local reverse proxy, forwarding `/`, assets and `/api` (including WebSocket
upgrades) to the daemon. The certificate must be trusted by the phone. Example
configuration when the proxy forwards the original Host:

```sh
WHIP_NETWORK=1 WHIP_LISTEN=127.0.0.1:8080 \
  WHIP_ALLOWED_HOSTS=whip.example,127.0.0.1:8080 \
  WHIP_ALLOWED_ORIGINS=https://whip.example \
  whip daemon start
whip web --url https://whip.example
```

Replace the example host with your configured origin. Open that HTTPS URL on the
phone. A proxy that rewrites Host must forward a value in `WHIP_ALLOWED_HOSTS`;
its browser's HTTPS Origin still needs an exact `WHIP_ALLOWED_ORIGINS` entry.
Wildcard binds do not identify a usable browser URL: pass the explicit origin
with `--url`, or bind the intended host address directly.

## Appearance

Open the palette button or **Settings → Appearance** to search and preview all
65 named TUI themes. **System appearance** follows the operating system's light
or dark setting. Selection is saved on the viewing device and restored before
the app renders. Controls, portaled dialogs, Markdown and code share the theme.

While connected, **Custom themes** lists JSON themes from the execution host's
`WHIP_HOME/themes` directory. **Import theme JSON** accepts the existing TUI format
up to 64 KiB. The host resolves the colors and Chroma styles; the selected result
is cached on the viewing device. Importing does not write a theme file or change
the TUI's selected theme. Browser contrast adjustments preserve the source palette.

## Asset and protocol boundaries

`GET /api/v3/web` reports whether assets are available, the protocol major and the
relative WebSocket/content paths. It uses the same host/origin checks as runtime
traffic. Browser history routes fall back to the embedded application; unknown API
and asset paths return 404. HTML is revalidated, hashed Vite assets are immutable,
and production responses apply the policy in `internal/webassets/assets.go`.
No inline scripts, runtime stylesheet compiler or authentication tickets are used.

Release CI builds and packages the application before compiling each binary and
smoke-tests discovery and HTML serving from an isolated temporary runtime.
`task web` checks generated theme drift, frontend types/build and component tests.
Go tests cover route fallback, missing assets, HTTP boundaries and the explicit
runtime-management behavior of `whip web`.

## Validation and measured behavior

Run the application gates against the actual production bundle and a temporary
fake-provider daemon. The fixture owns its home, sessions, listener and process;
it does not connect to a developer's running daemon or use provider credentials.
Build and package before starting browser fixtures, because the test daemon
embeds those assets when it is compiled.

```sh
npm run check:web
npm run test:web
npm run check:themes
node --test scripts/pack-web.test.mjs
node scripts/pack-web.mjs
npx playwright install chromium firefox
node apps/web/scripts/browser.mjs
# macOS release smoke: launches actual Safari without changing its preferences.
node apps/web/scripts/safari.mjs
```

The September 6, 2026 local run passed the app unit suite and the following
production application scenarios in both Chromium and Firefox:

- Same-origin deep links attach under the production CSP, without inline scripts,
  runtime code generation, font-policy violations or browser protocol errors.
- Text deltas and interleaved cumulative tool calls update stable conversation
  rows; switching themes and reloading preserve the active presentation.
- Host completion inserts a file reference into an unsent draft. Real HTTP text
  and image uploads appear in history; explicitly opened PNG previews decode.
- Two clients observe the same permission request and its single resolution.
  Sixteen concurrent submissions retain every command identity in recovery storage.
- Independent tabs preserve drafts for different recipients across reloads.
- An accepted turn survives disconnect as an identity, becomes interrupted after
  daemon crash/restart and does not repeat its external effect.
- A route naming a different runtime sends no snapshot, history or subscription
  request for that root.
- At 320×844 and 390×844 touch viewports, every visible header control stays
  within the viewport. The composer remains visible without horizontal page
  overflow; new output preserves the reading position and the view can jump to latest.
- Inspector selections survive reload in the route. Hovering a session link
  acquires no subscription. A simulated Chromium renderer freeze/thaw converges
  to completed host work; this does not claim physical OS sleep testing.
- A phone-sized client can submit, deny permission and stop a turn begun by
  another client. Empty, pending-request, inspector and phone states have zero
  critical or serious Axe violations in both browsers.
- Command-K accepts immediate keyboard search and can focus the composer.
  Resolving a local request restores that composer's focus without overriding a
  move to another control or recipient.

The actual Safari 26.3.1 smoke separately passed application attachment, a native
form submission and authoritative response, theme selection with a retained draft,
full-page draft/theme restoration and the same strict CSP. It uses the production
bundle served by an isolated fixture with a small external self-report driver.
WebKit automation is not reported as Safari testing. These checks do not establish
physical-phone, VoiceOver or visual-regression coverage for every host workflow.

A separate native Safari keyboard walkthrough confirmed Command-K focus,
immediate typing/selection, permission denial, composer focus restoration and
Escape dismissal. Safari's own Page Menu confirmed 200% zoom while the conversation
and composer remained readable. The native observation record is
`/tmp/whip-web-browser-results/native-keyboard-report.json`; VoiceOver speech and
rotor behavior remain a manual release check.

The browser runner writes screenshots and JSON to `/tmp/whip-web-browser-results`
(or `WHIP_WEB_BROWSER_RESULTS`). Timings from the passing local run were:

| Measurement | Chromium median / p95 | Firefox median / p95 | Samples per browser |
|---|---:|---:|---:|
| Command frame sent → acceptance receipt | 3.20 / 5.85 ms | 1.34 / 3.94 ms | 13 |
| Tool event received → corresponding DOM update | 25.60 / 25.70 ms | 24.00 / 24.00 ms | 6 |
| Crash/restart → browser reinitialization | 258 ms | 420 ms | 1 |

The primary Chromium/Firefox tabs each issued 18 command-status requests across this
scenario, including uncertain-turn recovery. These are small local fake-provider samples,
not WAN latency or a performance guarantee; the single restart observation is
not a statistically meaningful tail estimate. This small scenario includes parallel clients and interleaved tool streams.
No durability or polling settings were changed to obtain these numbers.

A separate loaded-workload run exercises the production app against 10,000 stored
root messages, 100 retained children with 100 messages each, a 1,400,000-byte tool body,
32 drafts totaling 1,046,528 bytes and 16 concurrent fake root agents:

```sh
npm run pack:web
node apps/web/scripts/performance.mjs
```

Its passing local Chromium 153 report is `performance.json` in the same results
directory. The test asserts four real history prepends preserve the visible anchor
within 2 px; selected text remains mounted after an 8,000 px scroll; and 20 cached
child/root switches reset the recipient's scroll state without mixing messages.
It records timings without making hardware-sensitive latency numbers a CI gate.

| Loaded measurement | Median / p95 | Samples |
|---|---:|---:|
| SDK child transcript opening | 6.55 / 7.17 ms | 100 |
| Real history prepend | 83.52 / 149.86 ms | 4 |
| Cached child → 512-message root switch | 62.56 / 63.44 ms | 20 |
| Near-limit draft input → next animation frame | 26.20 / 28.50 ms | 40 |
| Stream event received → corresponding DOM update | 18.90 / 49.90 ms | 74 |
| Successful store COMMIT return → DOM update | 54.09 / 85.38 ms | 40 |

Across child inspection, the SDK retained at most 346,436 payload bytes and 612
messages: 512 root messages plus one opened 100-message child. Each child was
released, leaving 512 messages. History paging rendered at most 19 conversation
rows. After garbage collection, the browser reported 10,643,252 JavaScript heap
bytes, 546 DOM nodes and 9 current transcript rows. JavaScript heap is not process
RSS or the full browser footprint; payload counters do not include object overhead.
Input-to-animation-frame is an input-response proxy, not a measured presentation
completion. These local fake-provider measurements establish a reproducible load
case, not real-model throughput or a guarantee for other machines.


The COMMIT measurement starts immediately after `Store.AppendRootEvent` returns
successfully and ends when the actual app's MutationObserver sees the committed
marker. Forty before/after clock brackets relate Go's monotonic clock to the
browser's, with ±0.376 ms uncertainty in this run. This includes unchanged 50 ms
subscription polling, SDK projection and DOM rendering; it excludes physical
paint. The initial reference budget is p95 below 125 ms: the 50 ms polling period,
50 ms received-event UI budget and 25 ms for local transport/scheduling overhead.
Latency remains a reported reference budget, not a hardware-sensitive CI threshold.
The same workload and the bounded concurrent probe test passed separately under
Go's race detector; race-instrumented timings are not used as the baseline.

A separate native EventTiming run records keyboard input through Chromium's next
render completion. All 40 trusted keydowns were reported: median/p95 32 ms,
with conservative 28–36 ms rounding bounds and no missing or dropped entries.
That satisfies the 50 ms reference target without equating an animation-frame
callback with paint. The browser reports durations rounded to 8 ms; the script
also accounts for threshold-censored fast entries instead of omitting them from
percentiles. This is a browser rendering estimate, not physical display timing.
The independently retained result is
`/tmp/whip-web-browser-results-event-timing/performance.json`; its rAF proxy remains
separately labeled (median 22.90 / p95 25.10 ms).

Component and package checks are separate from application protocol tests:

```sh
npm run check -w @whip/ui
npm run test -w @whip/ui
npm run build:storybook -w @whip/ui
npm run test:browser -w @whip/ui
npm run test:csp -w @whip/ui
npm run test:packed -w @whip/ui
# Reviewed macOS reference; see packages/ui/tests/visual-baselines/README.md.
npm run test:visual -w @whip/ui
npm run test:visual:proof -w @whip/ui
```

The UI suite covers all 65 generated palettes, source-palette immutability,
readable browser foreground derivation, custom-theme validation and complete
Chroma token attributes. Its 65 Axe fixtures check text contrast, labels and ARIA;
ten interaction scenarios cover keyboard/focus behavior, theme selection and
storage, narrow touch layouts and the normal Storybook accessibility panel.
Chromium, Firefox and actual Safari passed
self-only script/style/font CSP tests with lazy code rendering, retained selection,
custom Chroma backgrounds and bold/italic/underline, inert HTML, dialogs and menus.
Nine pure theme/code tests cover UTF-8 and token bounds. Packed protocol, SDK, UI
and application packages are installed into a separate consumer and exercised in
both Vite production and development builds. Reports live in the ignored
`packages/ui/ui-test-results` directory.

Ten reviewed light/dark screenshot baselines compare actual content, forms,
portaled overlays, narrow layouts, requests and recursive activity. The explicit
macOS reference gate fails on image differences. Its negative proof deliberately
changes component geometry, requires a real diff and checks that the baseline
hash did not change. Updates are explicit and require visual review; Linux CI
does not compare its rasterization to macOS images or silently regenerate them.

CI runs theme drift, package/type checks, app and UI unit tests, packing tests,
Storybook browser checks, Chromium/Firefox application tests, loaded-history
correctness/measurement and packed-consumer checks. Actual Safari remains an explicit macOS release gate. Keep physical-device
and assistive-technology testing on the release checklist; automated checks are
not a substitute for those remaining manual checks.
