# Whip desktop

The macOS desktop host uses Electron and the existing Whip web application.
It currently targets macOS 14 or newer on Apple Silicon. Distribution acceptance
is in progress: the [evidence log](../.ai-docs/plans/desktop-app/progress.md) records
which checks have passed and which still require a notarized release or hardware.
Intel, Windows and Linux desktop packages are not part of this first release.

For the routine source-build/install/restart workflow, use `task update:local`;
see [local update instructions](setup.md#update-your-local-installation-from-source).
It updates the existing app and shared backend together from the current checkout.

## New session shortcut

Press **Command+T** or choose **File > New session** to open a fresh New Chat tab
in the focused pane, reopening the desktop window if it was closed. This works
from Settings and with a composer, terminal or
embedded website focused. It preserves existing drafts and running work; the
session starts through the existing first-message flow. Open dialogs block the
action, and the existing 32-tab limit still applies.

Command+T always means New session, not New Browser tab. Use the existing Browser
UI or command palette action for a Browser tab. The web app has no new shortcut;
browser-owned Command+T remains native New Tab.

## Reopen closed tab shortcut

Press **Shift+Command+T** or choose **File > Reopen closed tab** to restore and
select the most recently closed tab. Repeated presses walk backwards through
the existing history (up to 20 tabs); with no history, nothing happens. This also
works from Settings and reveals a hidden window. Open dialogs block the action,
and existing workspace/Browser tab limits still apply without consuming history.

Restoration uses the existing Reopen action for sessions, unfinished drafts and
Browser tabs. It restores the original pane when it still exists, otherwise the
focused pane. Closed terminals are excluded from history because closing them
ends their shells; Browser tabs receive a fresh native identity. Deleted sessions
stay deleted.
On Windows/Linux the equivalent is **Ctrl+Shift+T**. The web app has no added
shortcut; the browser's reopen-tab shortcut remains native.

## One UI, one renderer artifact

`apps/web/src/main.tsx` selects a platform adapter and calls the common
`bootstrap.tsx`. Both hosts mount the same `@whip/app` application, router, React
providers, SDK client, session views, tabs, settings, drafts and `@whip/ui` styles.
The [frontend guide](frontend.md) is the source of current package boundaries.

The browser adapter uses browser storage, HTTP/WebSocket transport and web APIs.
The desktop adapter consumes a type-only preload contract for native operations.
Local and SSH connections forward bounded Unix-socket frames to the renderer’s
SDK client for that host; direct URL connections use its ordinary network transport.
Electron main owns processes, sockets and OS effects. Session state, recovery,
queries and product controls stay in the shared application.

Production runs the existing Vite build **once**. Its sorted path/size/SHA-256
manifest also records the source commit, dirty state, dependency lockfile and CSP.
The exact output is copied into Go's `internal/webassets/dist` embed and Electron's
`app.asar/renderer`. Desktop does not run a separate Vite build or transform those
assets. Packaging verifies every renderer file, its complete bytes inside the Go
binary, and the corresponding ASAR entry. Native configuration lives outside the
renderer, so channel/version/feed settings do not change the web bundle.

Electron serves assets from the secure standard `whip-app://bundle` scheme with
the shared CSP, exact asset allowlist and SPA route fallback. The production
renderer cannot import Electron or Node; Vite rejects those imports. The preload
exposes only bounded operations with sender/origin and argument checks in main.
Sandbox, context isolation and web security remain enabled. Production fuses
disable RunAsNode, NODE_OPTIONS, CLI inspection and extra file-scheme privileges;
ASAR integrity, ASAR-only loading and cookie encryption are enabled.

## Connections and lifetime

- **This Mac:** resolve the saved canonical `whipcode` executable, validate its
  distribution and compatibility, and attach to its healthy daemon. Connect starts
  that same executable if stopped; concurrent CLI and desktop starts share its
  owner lock. A proven unowned stale socket goes through normal daemon startup.
  An unhealthy live owner requires explicit attention. Discovery and reconnect
  never install another runtime or fall back to legacy `whip`.
- **SSH:** use macOS `/usr/bin/ssh`, including configured aliases, keys, agents and
  jump hosts. The remote machine must already have a compatible Whip installed;
  optional executable/home overrides support other layouts. Remote `whip` and
  `whipcode` use their own home environment variables. Whip may start a
  stopped remote daemon but never installs or upgrades one remotely. A private
  control connection forwards its Unix socket without exposing a remote web port.
- **URL:** connect to a daemon through an explicitly running reachable gateway.
  The gateway must allow the exact `whip-app://bundle` origin; an ordinary
  browser connection retains its existing same-origin rules. See [web setup](web-app.md).

Open **Execution hosts → This Mac** to configure the local installation. On this
Mac, select `/usr/local/bin/whipcode`; the standard home is `~/.whipcode`.
When no executable is selected, discovery checks the resolved login PATH and
known install locations. A successful choice or connection saves the absolute
path in `native-local-runtime.json` under Electron user data (normally
`~/Library/Application Support/Whip`). Finder and terminal launches then reuse
that selection even if their PATH differs. `WHIPCODE_HOME` explicitly overrides
the home; it is not a legacy `WHIP_HOME` migration or a setting in that JSON file.

For local launches, desktop recovers the known providers' API-key variables
alongside PATH from the user's interactive login shell. The names in
`apps/desktop/src/provider-environment.ts` are generated by `task models:update`
from the same Whip/Models.dev registry as the daemon; `task models:check` catches
drift offline. It also recovers `OPENAI_BASE_URL` and `OPENAI_API_BASE` so keys
intended for a custom endpoint are not automatically identified as OpenAI
credentials. An invalid recovered endpoint override prevents automatic use of
its OpenAI key. OpenCode-specific shell markers, config-path probes and credential
imports are removed. Existing inherited XDG values remain ordinary environment
values; they are no longer recovered for OpenCode lookup.
Declared `providerKeySources` files resolve inside the local daemon using its
own configuration. Desktop never reads those values into renderer state.
The fixed allowlist, three-second timeout and output limits keep this bounded;
explicit inherited values win, including an intentionally empty value. Shell
output and recovered keys are not persisted or logged. SSH setup retains its
PATH-only probe and uses the remote daemon's credentials; URL hosts also use
their own environment. Reusing an already running local daemon does not update
its environment or restart it. After changing shell keys, use the existing
explicit daemon restart workflow when appropriate for active work.

Each local connection attempt reads the shell environment once and shares it
between backend synchronization and attachment. When the verified executable
and running daemon already match, the successful probe is reused. A backend
update or daemon start is followed by a fresh readiness check. Window readiness
does not trigger duplicate synchronization. Results are not cached across
attempts: reconnect and diagnostics reread the environment, and an explicit
restart shares a newly read environment throughout that operation. Remote host
restoration proceeds in the background without holding the startup splash.

**Choose executable** validates an existing installation. **Install whipcode**
defaults to `~/.local/bin/whipcode`, verifies the bundled payload, and copies
its exact bytes there atomically. It does not overwrite an existing backend.
Neither action starts the daemon; use **Connect** afterward. A missing saved
executable remains an actionable setup error rather than silently selecting a
replacement. **Test Connection** checks the executable, build compatibility and
daemon status without starting, restarting, installing, writing configuration or
creating an absent runtime home. Expand diagnostics to see the executable, home,
client build and daemon build. **Restart daemon** is a separate confirmed action
because it interrupts work shared with CLI and web clients.

Desktop and ordinary CLI startup open no web listener. Desktop connects over
the private Unix socket, so occupied TCP ports must not prevent local startup.
To serve the same running daemon in a browser, start a separate foreground gateway:

```sh
WHIPCODE_LISTEN=127.0.0.1:8080 /usr/local/bin/whipcode web
```

This opens `http://127.0.0.1:8080` and stays running; Ctrl+C stops only that
gateway. Omit `WHIPCODE_LISTEN` to try `127.0.0.1:4444`, with an ephemeral
loopback fallback only when that port is occupied. Explicit addresses never
silently fall back. No daemon restart is needed to enable web access.

For automatic managed web startup, opt in with `WHIPCODE_NETWORK=1` when the
daemon starts; `WHIPCODE_LISTEN` alone is not an opt-in. The same gateway runs
as an owned child after the socket is ready. Gateway failure leaves the daemon
usable; it is not a reason for Desktop to start a second daemon. An already
running daemon retains its launch settings. Tests and local development use
isolated homes; with `WHIPCODE_NETWORK` unset or `0`, no gateway is auto-started.

If a URL host works in a browser but Desktop reports a WebSocket connection
failure, check the **gateway's** origin allowlist. Desktop sends
`Origin: whip-app://bundle`; the browser sends the web app's origin. Include
`whip-app://bundle` in `WHIPCODE_ALLOWED_ORIGINS` (or `WHIP_ALLOWED_ORIGINS` for
whip) when starting the remote gateway, preserving other required origins.
These exact Host/Origin checks are not authentication. Remote URL hosts still
need a trusted network or authenticated proxy. Gateway settings are read at
startup, not live from the invoking shell. A foreground replacement can change
web settings without restarting the daemon or interrupting accepted work.

Host profiles keep stable IDs and verified runtime identities rather than socket
addresses. Local, SSH and URL hosts stay connected independently while you switch
between their sessions. This Mac owns shared URL profiles through its configuration;
SSH settings and key paths stay on this device. Previously saved desktop addresses
remain available under **Execution hosts → Import desktop addresses** until you
verify and import them. Editing a host offers **Accept a new daemon identity**;
ordinary reconnects preserve the saved identity.
A changed daemon identity requires explicit confirmation before adopting it;
old drafts, tabs and command recovery remain scoped to the old runtime. Unknown
deep-link hosts are never created automatically.

Add server → SSH lists literal aliases from this Mac’s `~/.ssh/config`, with
explicit HostName/User/Port hints. Include files are read in lexical order, with
bounds of 64 files/Include patterns, eight nested levels, 1 MiB total input and
256 profiles. Wildcard/negated aliases and recursive Include globs are not offered;
conditional Match blocks are not evaluated. Truncation is visible, and manual entry
remains available. Discovery never invokes SSH or executes Match/ProxyCommand,
and is deferred until the SSH dialog is opened. OpenSSH resolves the selected
alias normally at connection time, including defaults and conditional options.

SSH host verification and authentication use shared in-app prompts. Secrets are
ephemeral, bounded and never saved in profiles or passed as process arguments.
The canonical local whipcode executable supervises the SSH process group and closes it when the main
process dies or its lifetime pipe closes. Disconnecting never stops remote work.

The daemon owns accepted work. Closing tabs, hiding/reloading a window, quitting
the GUI and applying a GUI update do not cancel it. Cmd-W uses the shared tab
command, then hides the window when no active tab is present; Cmd-Q quits the GUI.
Quit/reload/update flush text drafts and ask before discarding in-memory files or
unsaved storage changes. Window bounds, theme and session layouts are restored.

Native operations include copy, save, external links and a local folder chooser.
Uploads still use the shared file input. Optional notifications observe bounded
question/permission attention metadata for each connected host while the app is
open; they start off, suppress the initial/reconnect baseline and show no prompt
body. Completion notifications are not implemented because the current metadata
does not provide a durable completion event. Fully quitting ends observation.
An unchanged permission count cannot identify a replacement pending request.

## Browser tabs (experimental)

Browser tabs are enabled by default in packaged and development builds. Set
`WHIP_DESKTOP_BROWSER_TABS=0` in the app's launch environment to disable them
(restart required). Disabled launches expose neither Browser bridges nor Browser
IPC handlers; saved descriptors are retained as unavailable metadata. This switch
controls availability, not agent permission or SSH preview approval.

Browser tabs embed native web pages in the existing split workspace, rather than
putting websites in the application renderer. The address bar, back/forward,
reload/stop, find, zoom and tab movement use the shared UI; Electron main owns
isolated page profiles, navigation policy and page lifetime. Moving a tab keeps
the same page. Duplicate/reopen creates a new identity without inheriting agent
control. Closing honours `beforeunload`; a cancelled close keeps the descriptor.
The web-only application retains Browser descriptors as unavailable metadata.

An open conversation advertises an inert, exact Desktop destination even with
zero Browser tabs. The agent can request `browser.open`; create/control still
passes through the existing durable permission policy before a native tab exists.
Multiple Desktop windows serving the same conversation require an explicit choice,
never a newest/focused-window guess. Approved pages enter the originating pane in
the background.

Sharing an existing human page remains an explicit **Offer to conversation**
action: choose the connected execution host and conversation. `browser.list_tabs()`
discovers only root-offered pages, the caller's created pages and its own live
attachments. It grants neither control nor preview networking. No tab inventory
is injected into agent prompts.

Open/attach and preview-port expansion use **Allow once** or **Deny** in prompt
mode, with no remembered wildcard rule; automatic permission mode is unchanged.
Approval grants scoped attachment control, not one individual click.
Detach/revoke/disconnect ends that control; human pages remain open. Reconnection
may advertise inert availability but never restores old grants or replays a
create. Explicit page offers require reselection. Older conversations retain
their original Browser grants; start a fresh conversation for new operations,
never silently upgrade stored authority.

SSH previews use **saved SSH connections only**. Their isolation identity combines
saved host, verified remote runtime, live SSH generation and project metadata.
The stable `cwd:<absolute project path>` key is an isolation label, not filesystem
permission. Localhost URLs stay localhost URLs in the page: a private authenticated
proxy routes only explicitly approved literal `127.0.0.1` or `::1` ports through
the same authenticated SSH master. There is no Mac-local or direct fallback on
failure, and IPv4 approval does not imply IPv6 approval. URL/Tailscale-style
connections do not provide preview environments.

Network access belongs to the tab's preview environment, independently of agent
control. Expanding a project's port policy requires explicit approval and
invalidates incompatible controllers. Master loss revokes control and makes the
page unavailable; restore/reconnect metadata is not renewed consent. The last
page closes the environment's routes and private proxy. Initial standalone human
preview admission uses a parented native confirmation sheet owned by Electron
main, separate from SSH authentication prompts. Cancel is the default; approval
never mints an agent grant.

This feature is still under integrated/packaged acceptance, not a broad-release
claim. See the [implementation evidence](../.ai-docs/plans/browser-tabs/implementation.md)
for tested seams and remaining gates. Browser guest screenshots alone do not
prove compositor or overlay behavior; scripted transports do not prove the full
SDK/preload/native path.

### Design Mode

Use the **Design Mode** action to the right of a Browser tab's address field, or
press **Cmd+Shift+D** while that Browser pane is active, to toggle element selection
and describe a change. The shortcut works from the page, address/toolbar, and Design
composer. It leaves other panes and security dialogs alone; held-key repeats and
extra modifiers do not toggle it. Hover shows an element outline; select
several elements with Shift/Cmd/Ctrl-click to collect numbered, color-matched chips. The floating composer
anchors near a single element and docks lower-right for multiple selections. Remove
individual chips, choose an open conversation/child recipient, write the instruction,
and send without replacing an existing chat draft. A unique explicit conversation
association supplies the default; focus alone does not choose the destination.

**Evidence** shows bounded DOM evidence and the default viewport screenshot.
Turn off Screenshot for metadata-only messages. Evidence includes role/name, visible
text, selected attributes, a selector hint, computed layout/typography styles and
geometry; it is not a complete DOM dump or a guaranteed source-file mapping. Input
values, hidden/editable text and arbitrary framework props are not collected. Page
URL credentials, query and fragment are removed. Screenshots still contain visible
page content, which can include sensitive information: inspect them before sending.
Image messages require a vision-capable model; a rejection retains the draft.

This is inspection-to-chat, **not** drawing, drag/reorder, or direct CSS editing.
It works in the desktop Browser for ordinary/local pages and existing SSH previews
without adding network or agent-control grants. Cross-origin frame interiors and
closed shadow content are not promised: frame/host selection or explicit unavailable
feedback is preferable to selecting the wrong element. DevTools or active agent
inspection can make Design Mode busy; it never steals their debugger. Navigation
invalidates live selections. Selection context uses normal text/image attachments
and survives accepted-message replay; live DOM handles do not survive restart.

The overlay is a trusted native surface with a dedicated narrow preload, not code
receiving app permissions inside the website. Native compositor and production
capture checks are separate from web renderer tests. See the
[Design Mode implementation plan and validation](../.ai-docs/plans/browser-design-mode/README.md)
for measured coverage and remaining hardware IME/accessibility/release gates.

## Terminal tabs

A terminal tab is a login shell running where the session's daemon runs: on This
Mac for local sessions, on the remote machine for SSH and URL hosts. Electron does
not host the PTY; the daemon does, over the same connection as the conversation, so
reloading or hiding the window keeps the shell and replays what was missed. Closing
the tab ends the shell. Open one from a pane's actions menu, a session tab's **Open
terminal here**, the command palette, or the terminal shortcut (Control+` by default,
in Settings → General). The daemon refuses terminals for gateway-marked network
connections unless it started with `WHIPCODE_NETWORK_TERMINALS=1`; ordinary
Desktop Unix-socket and SSH connections retain local terminal access. The
gateway's socket hop does not bypass this daemon-owned policy. Paste uses the renderer's native paste event; the window denies
clipboard-read permission, so there is no programmatic paste path.

## Open a session in an editor

Conversation row menus and Session details offer **Open in → Cursor, VS Code,
Zed, Finder**. Whip discovers installed macOS applications; missing applications
are disabled. Local folders use the installed application's bundled launcher.
Finder opens local folders only. Launch errors appear in Whip with their cause.

For a remote daemon reached through a URL (including Tailscale), choose
**Configure SSH for editors…** and enter an alias from this Mac's OpenSSH config,
for example `gpu-4090-sam`. The alias is stored for that daemon's verified runtime
identity on this device. It does not change the Whip connection or SSH config.
The alias must reach the same machine as the daemon. Editors handle SSH login and
their required Remote SSH extensions. A simple native SSH profile can reuse its
host alias; profiles with separate user, port, or identity-file overrides require
an explicit alias that contains those options. URLs never imply an SSH identity.

The main process verifies the requested runtime against the prepared connection
or URL before launching, and remote paths cannot fall through to local Finder.
Only the fixed editor registry is supported; shell command templates and
arbitrary external URI schemes are not accepted.

The [implementation record](../.ai-docs/plans/conversation-row-actions/README.md#implementation-record--2026-09-08)
tracks native acceptance: local launches and Cursor/VS Code SSH handoff were
verified, while completed remote folder browsing and Zed SSH remain release-QA
items. The IPC diagnostic defaults to no external application launches; the
installed-editor diagnostic requires explicit opt-in.

## Build and develop

Building requires macOS arm64, Node 24, Go from `go.mod`, and Xcode command-line
tools for Swift/signing. The resulting app includes Electron, Go and the computer
helper; users do not need those build toolchains or a first-launch runtime download.

```sh
npm ci
node node_modules/electron/install.js
npm run dev:desktop
```

For UI development against an already-running daemon, use attach mode:

```sh
# Use the main daemon and the executable selected by the installed Whip app.
npm run dev:desktop -- --attach

# Explicitly attach to an isolated development daemon instead.
npm run dev:desktop -- --attach --home "$PWD/apps/desktop/.dev/home" --executable "$PWD/apps/desktop/.dev/bin/whipcode"
```

Attach mode builds the SDK and watches the Electron main/preload code, while Vite
serves the shared renderer with hot reload. It does not build Go, Swift, or the
embedded production renderer. Its shell output is in `.dev/attach-app`; GUI
settings are in `.dev/attach/<target>/user-data`, separate from the installed app
and managed development. The executable and home can also be supplied together
through `WHIP_DESKTOP_EXECUTABLE` and `WHIPCODE_HOME`; flags take precedence.
With no target override, `--attach` uses `~/.whipcode` and reads the executable
selection from `~/Library/Application Support/Whip/native-local-runtime.json`.
It never modifies that file or adopts the installed app’s update ownership.
Missing or invalid selection settings require explicit target flags; there is
no fallback to the isolated development daemon.

Attachment checks the executable's distribution and uses the SDK handshake to
validate the running daemon's protocol major and required response shape. Minor
versions add optional fields/capabilities and need not match; features negotiate
normally. Different build IDs or binary hashes are allowed; an incompatible, stopped, missing, or unhealthy daemon is reported
without changing it. Attach mode never installs, synchronizes, starts, repairs,
or restarts the backend, including on reconnect or from desktop management
controls. Previously saved installation choices/update approvals cannot change
its explicit target. Update the daemon separately when backend changes are needed.
Packaged apps reject attach mode and retain their existing payload integrity and
managed-update checks.

Both development modes start one Vite server at `http://127.0.0.1:3001`; pass
`--port 3002` to use another loopback port. Main/preload edits restart only Electron.
Closing development leaves the daemon running. The development URL is ignored by
a packaged app.

Without `--attach`, development builds and manages the isolated fixture in
`apps/desktop/.dev`. Its canonical executable is `.dev/bin/whipcode`, outside the
disposable staging tree. Go/Swift are built once per invocation. The script refuses
to replace a live backend when rebuilt bytes differ (including embedded renderer
changes). Use attach mode to keep working on the UI; to replace the backend,
stop the fixture separately after its work has finished:

```sh
WHIPCODE_HOME="$PWD/apps/desktop/.dev/home" apps/desktop/.dev/bin/whipcode daemon stop
```

| Command | Result |
| --- | --- |
| `npm run build:web` | SDK plus the single production renderer and manifest |
| `npm run pack:web` | Build and copy verified assets into Go embed |
| `npm run build:desktop` | Stage native binaries, renderer and main/preload |
| `npm run package:desktop` | Build and verify the `.app` under `apps/desktop/out` |
| `task update:local` / `npm run update:local` | Build, sign, verify and install the app plus its shared backend, then restart and reopen |
| `npm run make:desktop` | Package, make and reopen/verify DMG and ZIP in `out/release` |
| `npm run check:desktop` | Build SDK declarations, then check native TypeScript and bridge contracts |
| `npm run test:desktop` | Native lifecycle/transport tests and packaging/publisher tests |

`--renderer-ready` on build/package/make consumes the current verified artifact
without rebuilding it; the SDK is still built for the native main process.
Its commit and lockfile must match the checkout. Release
mode additionally requires both producer and consumer checkouts to be clean.
Local commands never publish. Unsigned local builds use ad-hoc native signatures;
they are not distributable Developer ID releases.

## Package, signing and canonical installation

The README and Desktop share `apps/desktop/resources/Whip.png` as the logo source.
Keep the source 1024×1024, square, and fully opaque, with the background extending
to every edge; do not bake in rounded corners or transparent padding. On macOS 26,
transparency can cause the legacy icon to appear inset inside a system backing
tile rather than filling it.
After replacing it, run `node apps/desktop/scripts/icon.mjs` on macOS to regenerate
`apps/desktop/resources/Whip.icns`; commit both assets. Forge uses that icon for
both stable and beta app bundles. This does not change an already installed app.

```text
Whip.app/Contents/
  MacOS/Whip
  Frameworks/                 Electron and its helpers
  Helpers/whipcode            signed Go installation payload
  Helpers/whip-computer       signed Swift helper
  Resources/app.asar/
    main.cjs, preload.cjs
    package.json, desktop-config.json
    renderer-manifest.json, runtime-manifest.json
    renderer/                 exact web build
    licenses/                 Whip, Electron and Chromium notices
```

The Swift helper is signed first. A Go build overlay embeds those exact signed
bytes without changing the tracked helper placeholder. Go is then signed, final
native hashes are recorded, and Forge seals/signs the ASAR and outer app while
preserving the native signatures. Only Electron receives its JIT entitlement.
Forge sets `osxSign.continueOnError: false` so a signing failure rejects packaging
with the original error. Electron Packager's default can suppress that failure
and leave an unsigned app that only fails later resource-seal verification.
The runtime manifest records `distribution: "whipcode"`, the app `version`, a
separate backend `buildId`, arm64 architecture, signing team, renderer digest,
source/lockfile provenance and protocol/schema compatibility. The backend is
built with the link-time distribution name `whipcode`; renaming a legacy binary
is insufficient. `WHIPCODE_VERSION` sets its build ID independently of
`WHIP_DESKTOP_VERSION`; it defaults to the app version when omitted. Packaging
executes `_desktop-runtime-info` and verifies those fields against the manifest.

`Helpers/whipcode` is an installation payload. Explicit installation copies those
verified signed bytes to the chosen canonical path; the daemon and new RLM
workers use that path via `os.Executable()`. The exact signed computer helper is
embedded in the executable and extracted under `~/.whipcode/bin` when needed.
Normal daemon/worker execution uses the selected installed executable. Desktop invokes
the verified bundled payload only as the updater for a desktop-managed installation;
it does not create a private Electron runtime directory.

An explicit **Install whipcode** records the installed hash and owning channel.
**Choose executable** keeps an external installation under the user's control.
On launch and before local attachment, a managed installation must match both the
bundled payload and the running daemon build. A changed external file or channel
conflict requires explicit remediation rather than an automatic overwrite.

A desktop update downloads while work continues. **Restart and update** saves one
release-specific interruption approval before Squirrel relaunches the app. The new
app stages and verifies the backend beside its canonical path, takes a shared
maintenance lock against other starters/updaters, gracefully stops its owner,
atomically replaces the file, and starts/verifies the new daemon. Without an
existing approval, any running daemon requires a restart/defer choice; desktop
never guesses that it is idle. Deferring leaves work running. Retrying after a
partial update inspects actual bytes/process state and reuses the saved approval.

Sessions and configuration remain in place. A failed local update keeps diagnostics
and remote connections available. Desktop-supplied `whipcode update` directs the
user to desktop updates and suppresses unrelated standalone update notices.
Source-managed or explicitly chosen external binaries retain their own update
procedure. GUI replacement does not imply permission to delete or migrate state.

Stable and beta keep separate app bundle IDs, GUI user data and executable
selection files, but both default to the canonical `whipcode` distribution and
`~/.whipcode` home. A beta GUI does not implicitly create another daemon home.
Development and acceptance fixtures choose isolated executables and homes with
`WHIP_DESKTOP_FIXTURE=1`, `WHIP_DESKTOP_EXECUTABLE`, `WHIPCODE_HOME` and
`WHIP_DESKTOP_USER_DATA`.
Stable registers `whip://` session links; beta registers `whip-beta://`, so
installing a beta does not take over stable session links.

`make:desktop` produces a DMG and an updater ZIP. It hashes the full app tree before
Forge make, safely extracts the ZIP and mounts the DMG read-only, then verifies
the same full tree, ASAR, native files, signatures and fuses inside each. Temporary
mounts are detached on failure. Final hashes are computed after DMG notarization
and stapling. `out/release/evidence.json` and `SHA256SUMS` describe those bytes.

## Releases and updates

The protected `desktop-v<semver>` release graph runs Whipcode CI/security and
native desktop checks, consumes one renderer artifact, and produces a signed,
notarized Apple Silicon application plus the matching Linux x64 backend. Candidate
checksums, dependency notices/inventory, package/startup evidence, and attestations
are retained. Separate staging and reviewed promotion steps reuse the same bytes;
the live R2 feed advances last. Desktop releases do not replace standalone CLI
latest-release discovery. See the [release runbook](desktop-releases.md) for exact
GitHub environments, secrets, hosting, acceptance, and retry procedures.

Squirrel.Mac verifies and downloads updates from the HTTPS static JSON feed.
Checking is deferred until after the renderer is ready; the Device settings page
can also check explicitly. Restart is enabled after download and uses the same
draft/attachment close handshake. Installer failures revoke quit approval and
permit another check. Builds without a feed show that updates are unavailable.

The publisher accepts only verified, signed/notarized, clean artifacts. It writes
immutable versioned ZIP/DMG objects first and downloads them to verify hashes.
It merges authoritative S3 history, then conditionally promotes the feed using
its ETag (or create-only for the first feed). A competing writer causes failure
instead of lost history. Release versions must not be reused with different bytes.
GitHub release retries likewise verify existing assets and upload only missing
names. They preserve release metadata and refuse changed bytes without clobbering.

## Acceptance

`task check` remains the repository gate. Add the affected Go race/integration
suites, browser/packed-package consumer checks, and desktop tests. Set
`WHIP_DESKTOP_SSH_TEST_EXECUTABLE` to the built Go executable to run the real
isolated SSH server tests; no user SSH configuration or system Remote Login is
required. `scripts/smoke.mjs` under `apps/desktop` exercises staged local lifecycle;
`scripts/terminal-smoke.mjs` opens a terminal tab on This Mac from the palette, types,
pastes through the native paste event, reloads, and closes it through File > Close tab;
`scripts/continuity.mjs` explicitly installs a canonical fixture executable and
verifies real signed workers using a loopback fake provider. It removes only a
disposable copy of the source application payload, then verifies recovered work
and new workers from the unchanged canonical installation. This models payload
replacement, not an actual Squirrel update. The staged smoke checks canonical
installation, absence of legacy homes/retained copies, and client lifecycle; it
does not establish production-fuse acceptance.
`node apps/desktop/scripts/startup.mjs` measures the verified signed app through
LaunchServices with 30 warm attachments and 30 retained daemon starts. It uses
private fixture homes, real retained messages, a fixed read-only native DOM probe
and the production fuses. It records polling/paint overhead and failed attempts.
Its three first-launch samples use an explicitly preinstalled canonical fixture
executable and are reported separately. They measure neither installation time
nor a cold-cache p95. Use `--self-test` to check probe isolation without launching.
`--idle` disables that probe after fixture preparation and samples the actual
desktop and detached daemon process trees separately with macOS CPU/RSS counters.
It also records de-duplicated physical footprint after the CPU window. The
[current idle evidence](../.ai-docs/plans/desktop-app/evidence/packaged-idle.md)
contains intermittent bursts and does not yet establish the idle CPU target.

Before publishing, validate a downloaded notarized build on the minimum macOS
target without toolchains, Accessibility/Screen Recording permission continuity,
sleep/wake and SSH hardware-key behavior, VoiceOver/IME, and a real signed
N-to-N+1 Squirrel update with active work and a subsequent old-daemon worker.
The [phased plan](../.ai-docs/plans/desktop-app/implementation.md) also defines
startup, memory, streaming and input-latency budgets. Measurements must name the
actual artifact, hardware and fixture; an empty window is not session readiness.
