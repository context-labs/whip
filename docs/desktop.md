# Whip desktop

The macOS desktop host uses Electron and the existing Whip web application.
It currently targets macOS 14 or newer on Apple Silicon. Distribution acceptance
is in progress: the [evidence log](../.ai-docs/plans/desktop-app/progress.md) records
which checks have passed and which still require a notarized release or hardware.
Intel, Windows and Linux desktop packages are not part of this first release.

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
- **URL:** connect to an explicitly configured reachable daemon. Its network
  configuration must allow the exact `whip-app://bundle` origin; an ordinary
  browser connection retains its existing same-origin rules. See [web setup](web-app.md).

Open **Execution hosts → This Mac** to configure the local installation. On this
Mac, select `/usr/local/bin/whipcode`; the standard home is `~/.whipcode`.
When no executable is selected, discovery checks the resolved login PATH and
known install locations. A successful choice or connection saves the absolute
path in `native-local-runtime.json` under Electron user data (normally
`~/Library/Application Support/Whip`). Finder and terminal launches then reuse
that selection even if their PATH differs. `WHIPCODE_HOME` explicitly overrides
the home; it is not a legacy `WHIP_HOME` migration or a setting in that JSON file.

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

Desktop supplies `WHIPCODE_LISTEN=127.0.0.1:8080` when starting a local daemon,
unless explicitly overridden. To make a CLI-first start use the same fixed web
endpoint, set it in that invocation as well:

```sh
WHIPCODE_LISTEN=127.0.0.1:8080 /usr/local/bin/whipcode daemon start
```

The browser can then open `http://127.0.0.1:8080`, while desktop still uses the
private Unix socket. Attaching to an already running daemon does not change its
network settings. If it was started without the desired listener, restart it
explicitly with that setting after accounting for active work. Tests and local
development use `WHIPCODE_NETWORK=0` with isolated homes.

If a URL works in a browser but the desktop app reports a WebSocket connection
failure, check the daemon's origin allowlist. The desktop renderer sends
`Origin: whip-app://bundle`; the browser sends the daemon URL as its origin.
Set `WHIPCODE_ALLOWED_ORIGINS=whip-app://bundle` for a whipcode daemon, or
`WHIP_ALLOWED_ORIGINS=whip-app://bundle` for a legacy remote whip daemon, when
starting a build that includes desktop-origin validation. Preserve other required
origins. A
separately built daemon may still reject every non-HTTP origin even when listed;
update that daemon's origin validation before restarting it. Protocol 4.1
compatibility alone does not establish that this origin is supported. Network
settings are read at startup, so editing the invoking shell does not change an
already running daemon. Schedule its restart around accepted work.

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

## Build and develop

Building requires macOS arm64, Node 24, Go from `go.mod`, and Xcode command-line
tools for Swift/signing. The resulting app includes Electron, Go and the computer
helper; users do not need those build toolchains or a first-launch runtime download.

```sh
npm ci
node node_modules/electron/install.js
npm run dev:desktop
```

Development starts the one Vite server at `http://127.0.0.1:3001`, watches
main/preload and restarts their window on changes. The development URL is ignored
by a packaged app. The fixture home and user data are in `apps/desktop/.dev`, not
the normal whipcode home. Its canonical executable is `.dev/bin/whipcode`, outside
the disposable staging tree. Closing development leaves that fixture daemon
running. Go/Swift are built once per invocation. Before restarting development
with changed native code, stop the previous fixture; the dev script refuses to
replace a live backend. After its work has finished:

```sh
WHIPCODE_HOME="$PWD/apps/desktop/.dev/home" apps/desktop/.dev/bin/whipcode daemon stop
```

| Command | Result |
| --- | --- |
| `npm run build:web` | SDK plus the single production renderer and manifest |
| `npm run pack:web` | Build and copy verified assets into Go embed |
| `npm run build:desktop` | Stage native binaries, renderer and main/preload |
| `npm run package:desktop` | Build and verify the `.app` under `apps/desktop/out` |
| `npm run make:desktop` | Package, make and reopen/verify DMG and ZIP in `out/release` |
| `npm run check:desktop` | Native TypeScript and bridge contract checks |
| `npm run test:desktop` | Native lifecycle/transport tests and packaging/publisher tests |

`--renderer-ready` on build/package/make consumes the current verified artifact
without rebuilding it. Its commit and lockfile must match the checkout. Release
mode additionally requires both producer and consumer checkouts to be clean.
Local commands never publish. Unsigned local builds use ad-hoc native signatures;
they are not distributable Developer ID releases.

## Package, signing and canonical installation

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
