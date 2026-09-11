# Whip mobile

The Expo/React Native companion connects directly to an existing Whip host over
its private Tailscale HTTPS address. Execution, provider credentials, sessions and
permissions stay on the host. The phone has no application login, pairing service,
QR scanning or push notifications in this release.

This workspace is under implementation. The
[acceptance record](../.ai-docs/plans/mobile-app/EVIDENCE.md) distinguishes passing
checks from device/release work that remains. A successful Metro export is not a
native build or TestFlight release.

## Connect a host

1. Install and connect Tailscale on the host and phone. Tailnet access rules must
   restrict the served endpoint to the intended people/devices: reaching this
   listener permits directing host work. Use private
   [Tailscale Serve](https://tailscale.com/docs/features/tailscale-serve).
2. Configure Whip's existing listener on a fixed loopback port. For example,
   substitute the host's actual Tailscale DNS name for `whip.example.ts.net`:

   ```sh
   WHIP_LISTEN=127.0.0.1:9876 \
   WHIP_ALLOWED_HOSTS=127.0.0.1:9876,whip.example.ts.net \
   WHIP_ALLOWED_ORIGINS=https://whip.example.ts.net \
   whip daemon start
   ```

   These are existing environment settings, read when the daemon starts.
   `whip daemon start` does not change a running daemon's listener. Inspect
   `whip daemon status` before scheduling any intentional restart; restarting a
   live development daemon is not part of building or installing the phone app.
3. Inspect existing Serve mappings with `tailscale serve status`. On an unused
   mapping, forward HTTPS to the loopback listener:

   ```sh
   tailscale serve --bg http://127.0.0.1:9876
   ```

   Serve reports the HTTPS address. Keep exact Host and browser Origin values in
   the allowlists. Use an explicit additional HTTPS port if the default mapping
   already serves another app, and include that port in the external Host/Origin.
   The [Serve CLI reference](https://tailscale.com/docs/reference/tailscale-cli/serve)
   documents inspecting and removing individual mappings.
4. Open Whip mobile → Connect a server. Enter the HTTPS **base address**, with an
   optional friendly name. Do not append `/api/v3/ws`. The SDK owns socket/content
   paths, initializes the protocol and verifies persistent runtime identity.
   **Test Connection** checks HTTPS discovery, the live WebSocket handshake and
   a one-item session read before saving anything. Each step has a 15-second
   deadline. An empty session list is a successful result. Cancel or leave the
   screen to stop the test; backgrounding also cancels it.

A bare hostname is normalized to HTTPS. Production accepts HTTPS origins only;
credentials, paths, query strings and fragments are rejected. Development builds
also accept HTTP loopback for a simulator fixture. Disabling certificate checks
or allowing arbitrary cleartext hosts is not part of this setup.

The network boundary replaces app-level authentication for this private first
release. Client IDs identify durable command namespaces; they are not credentials.
Tailscale Serve remains tailnet-private; no public Funnel or unauthenticated public
reverse proxy is needed.

### Configured development host: gpu-4090-sam

The SSH alias `gpu-4090-sam` reaches `sam@kuzco-gpu-2`. Enter this base address
in the phone app while Tailscale is connected:

```text
https://kuzco-gpu-2.tail7524e6.ts.net
```

The `whip-sam.service` system service starts at boot and runs
`/home/sam/.local/bin/whip _daemon` as `sam`, with data in `/home/sam/.whip`.
It listens only on `127.0.0.1:9876`; persistent Tailscale Serve proxies private
HTTPS port 443 to that listener. The service unit holds the exact Host/Origin
allowlists. Manage this installation through systemd so those settings persist:

```sh
ssh gpu-4090-sam 'systemctl status whip-sam.service --no-pager'
ssh -t gpu-4090-sam 'sudo systemctl restart whip-sam.service'
```

Restart only when interrupting host work is intended. This is a fresh runtime,
with no sessions migrated from other users or machines. Its default Inference.net
provider entry exists, but credentials have not been configured. To enable model
execution, complete the provider's browser login using the remote CLI:

```sh
ssh -t gpu-4090-sam '/home/sam/.local/bin/whip auth inference-net login'
```

Open the printed verification address locally and follow the account/project
selection prompts. Provider credentials stay on the execution host. This is
provider setup, separate from the app's Tailscale connection.

HTTPS certificate verification, SDK WebSocket initialization, and session,
attention, directory, configuration and model catalog reads passed from the Mac
on 2026-09-08. Physical-phone workflows and model execution remain to be verified.
`kuzco-4090` is a different machine; the earlier preparation there was rolled back.

## Use the companion

- **Sessions** is home after a host is added. It combines connected hosts, with
  search, host filters, archived sessions and local pins. The top-right plus and
  empty-state New session button open the same host → folder → review flow.
  Model, reasoning, execution language and advertised agent definitions are in
  Session options; the first message is optional. Creation,
  effort selection and first input are independently journaled; partial failure
  retains the created root and requires an explicit next action.
- A **conversation** names the host, root and current recipient. Select Root agent
  or a child, load older history, queue or steer input, or stop the exact displayed
  active turn. Drafts belong to that runtime/root/recipient. Losing connectivity
  preserves drafts and leaves accepted host work running.
- **Needs you** queries pending questions and permissions while foregrounded.
  Open a session to see authoritative requests, review batched answers, skip a
  question or choose Allow once/Deny. Another client may resolve a request first;
  the app refreshes rather than guessing its outcome.
- **Settings** manages up to four independently connected hosts, Appearance,
  retained drafts and command activity. Renaming changes only the local label.
  Appearance includes every generated web theme, paired light/dark choices,
  live preview/cancel, JSON or host-theme import, text/code fonts and sizes,
  wrapping, tool density, contrast and reduced motion. Custom imports need a
  connected host for normalization and then work offline. Preferences stay on
  this phone. Checking delivery uses original identity. Retrying requires confirmed
  missing status and the original in-memory body; restored records never send
  old text automatically. Resolve retained records before clearing them.

There is no background delivery queue. Foreground resume reconnects and reconciles;
there are no notifications while the app is closed. Host directory paths and model
catalogs refer to the execution computer, not the phone filesystem.

## Development

Use the repository's Node 24/npm workspace and root lockfile:

```sh
npm ci
npm run build
npm run check:mobile
npm run test:mobile
npm run export:mobile
npm run dev:mobile
```

Use a development client, not Expo Go: SQLCipher, Expo UI and the private storage
module require a native build. From `apps/mobile`, `npx expo run:ios` or
`npx expo run:android` generates and builds native projects. `ios/`, `android/`,
`.expo/` and exported bundles are generated and ignored; edit Expo config and the
local module under `modules/whip-storage`, not generated projects.

Expo 57 documents Xcode 26.4+ and iOS 16.4+; use a current CocoaPods/Ruby toolchain.
Android builds need Java 17 or later plus the Expo-selected SDK/NDK. Check the
[versioned SDK compatibility table](https://docs.expo.dev/versions/v57.0.0/).
Mobile uses TypeScript 6; the existing web/tool workspaces retain TypeScript 5.9.

`eas.json` provides development, simulator, preview and production profiles.
Account ownership, production bundle identifiers, signing credentials and store
records must be configured before distributing builds. The default development
bundle ID is `dev.contextlabs.whip.mobile`; set `WHIP_MOBILE_BUNDLE_ID` for an
approved production identifier. No EAS project, release submission, encryption
export declaration or store privacy claim is fabricated by the scaffold.

For pinned EAS commands, CI behavior, ownership fields and beta acceptance, use
the [mobile release runbook](mobile-release.md).

## Isolated fixture over Tailscale

Use the [manual fixture runner](../apps/mobile/scripts/fixture.mjs) for native/web
acceptance without attaching to normal host work. It uses the same integration
fake provider as SDK acceptance, with a temporary home and a loopback listener.
The runner creates no Tailscale mapping; `--origin` adds exactly one permitted
HTTPS request origin and its exact Host (including a non-default port), alongside
the fixture's loopback Host. The listener still binds only to loopback.

1. Inspect `tailscale serve status` and choose an unused HTTPS port. The examples
   use 8443; replace `whip.example.ts.net` with this host's actual Tailscale DNS
   name and keep the chosen port identical in both commands. From the repository
   root, build the shared web fixture and start the isolated host:

   ```sh
   npm run pack:web
   node apps/mobile/scripts/fixture.mjs --minutes=30 --origin=https://whip.example.ts.net:8443
   ```

   Use an exact HTTPS origin with no trailing slash, credentials, query, fragment
   or wildcard. Invalid origins fail before compilation/startup. The runner
   prints the mobile server URL, loopback proxy target, fixture cwd/root and web
   conversation URL. It stops within 30 minutes; SDK acceptance retains its
   four-minute default.
2. In another terminal, substitute the printed loopback port for `PORT` and
   configure only the previously unused Serve mapping:

   ```sh
   tailscale serve --bg --https=8443 http://127.0.0.1:PORT
   ```

   Enter the printed HTTPS server URL in the mobile app and open the printed web
   URL on another client in the same tailnet. Serve terminates HTTPS and forwards
   to loopback with the external Host preserved; the fixture validates both that
   Host and Origin. An HTTPS-origin flag alone does
   not make the fixture reachable or provide TLS. The
   [Serve CLI reference](https://tailscale.com/docs/reference/tailscale-cli/serve)
   describes the mapping and scoped removal commands.
3. Type test text for streamed echo, `hold:manual` for cancellable work,
   `permission:manual` for Allow once/Deny, or `question:single` / `question:batch`
   for questions. Batch answers cover a choice, multiple choices/custom text and
   a skipped page; completion returns JSON for comparison across clients. Use
   only synthetic content: the fixture records history/effects in its temporary
   directory and does not contact a model provider.
4. Remove this mapping when finished, including after an expired/crashed fixture,
   then Ctrl-C the fixture runner to remove its temporary data:

   ```sh
   tailscale serve --bg --https=8443 off
   tailscale serve status
   ```

   Do not use `tailscale serve reset`, which would remove unrelated mappings.
   If restarting the fixture gives a new loopback port, update only its mapping.

Without `--origin`, the runner retains the original loopback-only setup. See
[the mobile workspace README](../apps/mobile/README.md#manual-acceptance-fixture)
for simulator access and explicit Android `adb reverse`. Production builds can use
the real HTTPS proxy URL; HTTP loopback acceptance needs a development build.

An Android emulator on macOS may not inherit the host's MagicDNS resolver. If the
host can resolve the Serve name but the emulator reports an unknown host, launch
the AVD with `-dns-server 100.100.100.100` while the host is connected to Tailscale.
This was verified with the API 36 acceptance AVD; it routes through the host and
does not establish phone VPN/cellular coverage. The Android documentation explains
[manual emulator DNS settings](https://developer.android.com/studio/run/emulator-networking-dns).

## Local data and troubleshooting

SQLCipher encrypts a private SQLite database, including its WAL. Its random key
uses SecureStore device-only accessibility. The native module places the database
outside backups. Missing keys/databases, unreadable schema, quota or write failure
preserve existing files and block unsafe sends; there is no plaintext fallback.
Unsubmitted drafts are separate from metadata-only command recovery.

A changed server runtime cannot inherit saved command state automatically. Verify
the replacement host before removing and re-adding its saved entry. Removing a
server detaches local observation and retains its drafts/recovery metadata.
Disconnect does not stop agents; Stop is the explicit host action.

Connection failures are shown inside the connection sheet, with retry guidance
and bounded native error details. Testing a saved server also checks its pinned
runtime identity. The same base address can serve the web app and mobile API;
a working web page by itself does not establish WebSocket access.

If TLS fails, verify the HTTPS address and Tailscale connectivity. If the app reports
an incompatible protocol, update the host using its normal maintenance workflow.
If secure storage fails, retain the device/app data while investigating. Do not
uninstall merely to clear an error: iOS may retain a device key after removing the
app database, which requires an explicit local recovery/reset decision.

Native content inspection is one explicit sheet, limited to 256 KiB with scope and
SHA-256 checks. Images/uploads are deferred. Embedded image/HTML markdown is shown
as selectable source, links require a tap and previews are disabled. Transcripts,
query caches and provider secrets are not copied into the device database.

Native conversation text renders at most 8,192 UTF-16 units per page, preserving
surrogate pairs at boundaries. Short messages use native Markdown; longer messages
use clearly labeled selectable source pages, including combined tool arguments and
output and explicitly inspected bodies. Collapsed details use at most 512 units.
Changing pages does not fetch host data. Live appends retain the selected page;
recycled rows reset to their first page. Copy uses the full retained text (tool
arguments and output together), not just the visible page. A body-only completed
row says the message is retained on the host and requires an explicit open action.

Settings includes bounded draft copy/discard, resolved command/permission cleanup,
and an explicit **Reset local data** action. Reset erases this phone’s saved
servers, drafts and delivery records after confirmation; host work continues.
An interrupted reset preserves a marker and must be explicitly completed before
the app can reopen. Storage errors never silently reset or downgrade encryption.

Diagnostics exports use a metadata allowlist. They include runtime identity and
connection/reconciliation status, and exclude server URLs, raw errors, transcript
content, drafts and keys. Body text uses bundled Inter and code uses JetBrains Mono,
with system fallback if font assets fail to load.
