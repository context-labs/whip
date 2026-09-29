# Browser and computer use

Native browser control has two explicit targets: an offered Desktop Browser tab,
or a named external Chrome session. Neither discovers authority from old
configuration, saved checkpoints, tab IDs, or a previous connection. The
`browser` and `computer` modules use the same durable operation and permission
ledger as other native host effects.

## External Chrome modes

The native host's `external_browser` declaration selects one mode. Its default
is `disabled`. Configuration and status reads do not launch Chrome, discover a
profile, install an extension, open a relay, or grant control.

| Mode | Explicit configuration | Ownership |
| --- | --- | --- |
| `live` | One literal loopback `live_endpoint`, or one absolute `live_profile` containing `DevToolsActivePort` | Attaches to that browser; disconnect never closes human Chrome. |
| `dedicated` | Absolute `executable` | Launches an owned visible Chrome with an isolated profile under the native runtime directory. |
| `headless` | Absolute `executable` | Uses the same owned profile and process rules, with headless Chrome. |
| `extension` | Explicitly installed native extension | Opens an authenticated loopback relay after operation approval, then waits for the person to pin a tab. |

Live endpoints accept `http://127.0.0.1:PORT`, an equivalent literal IPv6 loopback
address, or an exact `ws://…/devtools/browser/…` URL. HTTP discovery cannot redirect
or change endpoint authority. Profile discovery uses only the named profile and
its published loopback port. There is no well-known-profile scan, conventional
port probe, downloaded executable, silent live-to-dedicated fallback, or browser
process takeover. A locked dedicated profile fails explicitly; Whip does not
kill another Chrome or move its profile to make the launch succeed.

In Web and Desktop, open **Settings → Execution → External Chrome** to edit
these values on the selected execution host. The same form appears under
**Session details → Host integrations → Browser automation**. Saving requires
the revision that was displayed; a conflict or interrupted reply preserves the
draft and requires **Discard edits and read external Chrome settings** before
another change. Reading and saving do not open Chrome.

The root agent's browser inspector shows each prepared connection's mode,
driver, state, exact generation and permission resource. **Reconnect** creates a
fresh prepared generation; **Disconnect** retires the selected generation.
Children have read-only connection metadata and still need delegated grants.
After an unconfirmed action, **Read current external connections** observes the
host without retrying the action.

Mobile exposes the same settings under **Settings → Hosts → Browser automation**.
Its conversation controls show exact named generations, with reconnect and
disconnect available only on the root recipient. Leaving the foreground aborts
local waits; an unconfirmed change requires an explicit read before another
write. Mobile never opens a browser on the phone.

The native SDK exposes the same configuration CAS:

```ts
const current = await client.hosts.externalBrowser();
await client.hosts.setExternalBrowser(current.revision, {
  mode: 'headless',
  executable: '/absolute/path/to/chrome',
  live_endpoint: '',
  live_profile: '',
  allow_private_urls: false,
});
```

After a lost configuration acknowledgment, read the host declaration and
reconcile it. Do not automatically resend the edit. The driver (`rod` or
`chromedp`) is a separate host setting; a process-level driver pin is reported
explicitly. Changing effective external-browser configuration or driver retires
old external connections and prepared captures.

An agent explicitly names its external session:

```python
browser.run(session="default", code='goto("https://example.com"); info()')
```

JavaScript uses the same fields in an object. Names contain 1–64 letters, digits,
dashes or underscores. A retained `mode:name` spelling is accepted only when its
mode matches the host declaration; it cannot override host policy. A missing
`session` does not select external Chrome. Never combine it with `attachment_id`
or another Desktop lifecycle argument.

`browser.external` authorizes the exact root, name and process generation. The
permission intent identifies the mode, driver and whole-browser scope: live and
owned Chrome may enumerate or select their tabs. Extension control is limited to
the currently pinned tab. Children need an explicitly delegated grant for the
same resource; sharing a root does not give them control.

Named external sessions are bounded to four per root and sixteen per host.
Live and extension modes each permit one selected resource on the host. Each
resource allows one active batch plus four queued batches; at most four batches
run across the host. The execution deadline defaults to 60 seconds and can be
set explicitly up to 120 seconds. Permission waiting precedes that deadline.
Cancellation removes queued work, and every CDP primitive rechecks its captured
configuration and dispatched operation authority.

A failed or canceled delivered batch retires the connection without replay.
`client.hosts.externalBrowserSessions(sessionID)` lists bounded metadata.
Root-only `reconnectExternalBrowser(rootID, name, generation)` explicitly creates
a new prepared generation; it does not launch a browser or repeat the failed
operation. `disconnectExternalBrowser` and permission revocation retire the exact
generation. Old grants and captures cannot restore control after restart.
Owned Chrome is stopped and joined; its profile cookies remain. Human Chrome
and human pages remain open.

### Helpers, uploads and screenshots

The batch parser retains `goto`, `back`, `info`, `js`, `click`, `type`, `press`,
`fill`, `scroll`, `waitLoad`, `waitFor`, `ax`, `box`, `tabs`, `useTab`, `dialog`,
`screenshot`, `upload` and `print`. Parsing and static validation finish before
permission. All modes reject metadata destinations; non-live modes also reject
private network destinations unless `allow_private_urls` is explicitly enabled.
The final page URL is checked after helper execution, including redirects. This
is a destination policy, not a claim that arbitrary page JavaScript is a network
sandbox.

External `upload(selector, pathOrPaths)` requires the separate
`browser.external.upload` capability for the exact browser generation,
captured workspace and canonical path set. A generic browser-control grant
cannot authorize host file bytes. Paths must remain inside the invoking
session's captured working directory, including for children. The native host
rejects replaced files/workspaces and nonregular files, then reads bounded bytes
only after dispatch. Chrome receives private copies, not mutable workspace paths.
At most sixteen files and sixteen MiB are retained per browser generation, with
a four MiB limit per file. Copies remain available for a later form submission
and are removed on generation retirement; the next exclusive runtime startup
collects copies left by a crash. Capacity failure is explicit.

Screenshots use canonical owner-scoped content references and the existing
operation/cell image budgets. Accepted tool images become ordinary model vision
parts; the host does not infer image authority from JSON text. Browser output is
bounded to 64 KiB, individual CDP replies to 12 MiB, and transport overflow ends
the connection rather than starting a replacement.

### Extension installation

Run `whipcode browser install`, or pass `--directory /absolute/runtime-directory`
for a deliberately selected native runtime. The default location is
`$WHIPCODE_HOME/runtime-v4/browser/extension` (or
`~/.whipcode/runtime-v4/browser/extension`). Installation writes extension assets;
it does not mint a token or launch a relay.

In Chrome, open `chrome://extensions`, enable Developer mode, choose **Load
unpacked**, and select the printed folder. Configure the native host's mode as
`extension`. Submit and approve a named `browser.run` request, then click the
extension icon on the desired tab while the host waits for selection. Clicking
again detaches. A lost relay or controller connection requires explicit reconnect
and a new human pin; the worker never reconnects or resends an old command.

Implementation: [native connection factory](../internal/browser/native_open.go),
[bounded owner](../internal/browser/native_host.go),
[runtime admission](../internal/runtime/external_browser.go), and
[SDK controls](../packages/sdk/src/services.ts).

## Desktop Browser tabs

Desktop controls the same embedded page the person sees, through an exact
root-offered tab and a scoped provider connection. It does not borrow external
Chrome configuration or expose a production debugging port. Desktop advertises
availability for an exact conversation/window; ambiguous destinations require
explicit selection. Existing human pages must be offered before discovery or
attachment.

`browser.list_tabs()` returns bounded metadata for offered and caller-owned
pages. A listed ID is only a candidate for permission-gated `open` or `attach`.
`browser.run(attachment_id=…, code=…)`, `detach`, and `allow_preview_port` retain
exact provider/tab/profile/document authority. Child transfer requires explicit
`browser_attachments` delegation during spawn and tracks ancestor revocation.
Provider release, disconnect or revocation ends agent control without closing
the person's page. Reconnect can advertise availability, not restore a grant.

Desktop batches are serialized, bounded and never replay uncertain mutations.
Screenshots travel over the exact holder connection into scoped native content.
Filesystem-path uploads remain unavailable on this path. SSH previews can reach
only approved literal loopback ports on the selected saved SSH connection; there
is no Mac-local network fallback.

Implementation: [browser host](../internal/browserhost/host.go),
[runtime](../internal/runtime/browser.go),
[SDK provider transport](../packages/sdk/src/browser-provider.ts), and
[native Desktop control](../apps/desktop/src/browser-control.ts). See also
[Desktop behavior](desktop.md#browser-tabs-experimental) and the
[frontend boundary](frontend.md#native-browser-workspace-boundary).

## Computer use (macOS)

`computer.run(code=…)` drives native apps through bounded helper batches. The
host owns one revocable helper generation and borrows the same process manager
as other native services. Status and availability reads do not extract, start,
or enable the helper. Installing the bundled helper, enabling it, changing saved
app policy, and reconnecting are explicit human actions with configuration or
generation CAS.

`state(app)` supplies indexed accessibility evidence and scoped screenshots.
Indexed actions must match the observed app/UI generation in the current live
kernel. Kernel eviction, disconnect and restart do not restore those indices.
Saved Allow is availability policy, not a SQL grant; unlisted apps can still ask
for one-off consent, while explicit Deny rejects them. Cancellation never turns
a waiting batch into an authorized one.

Chrome AppleScript helpers retain their explicit app authority. General `tell`
is visibly broad AppleScript authority, not an app-confined script. Required
permissions and current helper/policy generations are checked before effects;
partial failures remain uncertain without resume or replay. Accessibility and
Screen Recording permissions are managed by macOS and are not bypassed.

Implementation: [runtime computer controls](../internal/runtime/computer.go),
[helper controller](../internal/computer/controller.go), and
[batch validation](../internal/computer/batch.go). Screen and page contents remain
untrusted evidence, never instructions to the host.
