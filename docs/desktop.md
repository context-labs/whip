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

- **This Mac:** verify bundled binaries, resolve Finder's limited PATH, then
  attach to a healthy daemon. If none is running, install the bundled runtime
  outside the `.app` and start it there. A proven unowned stale socket goes through
  normal owner-locked daemon startup. An unhealthy live owner requires attention.
- **SSH:** use macOS `/usr/bin/ssh`, including configured aliases, keys, agents and
  jump hosts. The remote machine must already have a compatible Whip installed;
  optional executable/home overrides support other layouts. Whip may start a
  stopped remote daemon but never installs or upgrades one remotely. A private
  control connection forwards its Unix socket without exposing a remote web port.
- **URL:** connect to an explicitly configured reachable daemon. Its network
  configuration must allow the exact `whip-app://bundle` origin; an ordinary
  browser connection retains its existing same-origin rules. See [web setup](web-app.md).

If a URL works in a browser but the desktop app reports a WebSocket connection
failure, check the daemon's origin allowlist. The desktop renderer sends
`Origin: whip-app://bundle`; the browser sends the daemon URL as its origin.
Set `WHIP_ALLOWED_ORIGINS=whip-app://bundle` when starting a daemon that includes
the desktop-origin validation change, preserving other required origins. A
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
A replaced daemon requires explicit confirmation before adopting its identity;
old drafts, tabs and command recovery remain scoped to the old runtime. Unknown
deep-link hosts are never created automatically.

SSH host verification and authentication use shared in-app prompts. Secrets are
ephemeral, bounded and never saved in profiles or passed as process arguments.
The bundled Go supervisor owns the SSH process group and closes it when the main
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
the normal Whip home. Closing development leaves that fixture daemon running.
Restart the command after Go/Swift changes; they are built once per invocation.
To stop that fixture explicitly, after its work has finished:

```sh
WHIP_HOME="$PWD/apps/desktop/.dev/home" apps/desktop/.stage/native/whip daemon stop
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

## Package, signing and retained runtime

```text
Whip.app/Contents/
  MacOS/Whip
  Frameworks/                 Electron and its helpers
  Helpers/whip                signed Go executable
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
The runtime manifest records build version, arm64 architecture, signing team,
renderer digest, source/lockfile provenance and protocol/schema compatibility.

Before daemon startup, native files are copied into an owner-only immutable
`runtimes/<version>-<manifest-hash>` directory under Electron user data. Both the
source and installed files are verified. The daemon launches from that retained
absolute path with its matching computer helper. This is necessary because new
RLM workers use `os.Executable()` after the original `.app` may have been replaced.
Healthy compatible CLI daemons are reused even if their build differs.

The GUI updater never calls `whip update` or restarts a daemon. Runtime versions
are retained conservatively: automatic garbage collection is not implemented.
Do not remove retained directories while a daemon or worker might use them.
Restarting/upgrading a daemon is an explicit separate operation; GUI rollback
does not downgrade its database. Stable uses the existing Whip home. Beta has a
separate bundle ID/user data and a separate default runtime home.
Stable registers `whip://` session links; beta registers `whip-beta://`, so
installing a beta does not take over stable session links.

`make:desktop` produces a DMG and an updater ZIP. It hashes the full app tree before
Forge make, safely extracts the ZIP and mounts the DMG read-only, then verifies
the same full tree, ASAR, native files, signatures and fuses inside each. Temporary
mounts are detached on failure. Final hashes are computed after DMG notarization
and stapling. `out/release/evidence.json` and `SHA256SUMS` describe those bytes.

## Releases and updates

The tag-release workflow builds one renderer artifact. CLI target jobs and the
macOS desktop job download it; neither regenerates the renderer. Publication
waits for both products. The desktop job uses a temporary keychain and removes
its credentials afterward. A required desktop gate cannot silently be skipped.
The macOS arm64 job uses GitHub's standard `macos-14` runner
([runner reference](https://docs.github.com/en/actions/reference/runners/github-hosted-runners)).

Configure GitHub environments `desktop-stable` and `desktop-beta` separately.
All names in the table use the `WHIP_DESKTOP_` prefix:

| Kind | Names |
| --- | --- |
| Variables | `SIGN_IDENTITY`, `TEAM_ID`, `UPDATE_URL`, `NOTARY_KEY_ID`, `NOTARY_ISSUER`, `BUCKET`, `AWS_REGION`, `AWS_ROLE_ARN` |
| Secrets | `CERTIFICATE_P12_BASE64`, `CERTIFICATE_PASSWORD`, `NOTARY_PRIVATE_KEY` (raw API private key PEM) |

`UPDATE_URL` is an owned HTTPS `/RELEASES.json` URL without credentials, query or
fragment. Use separate stable/beta and macOS/arm64 paths, with the same path in
the configured S3 bucket. The AWS role uses GitHub OIDC with permission to read
and conditionally write only that release prefix. Local signing may instead use
`WHIP_DESKTOP_NOTARY_PROFILE`, an existing notarytool keychain profile, alongside
`SIGN_IDENTITY`, `TEAM_ID`, `NOTARIZE=1` and the intended feed. Credentials are
not stored in the app or repository.

HALO's [desktop release workflow](https://github.com/context-labs/HALO/blob/main/.github/workflows/app--release.yml)
is an existing source of Apple signing and notarization credentials. Its
`APPLE_DEVELOPER_ID_CERTIFICATE_BASE64` and certificate password map to Whip's
certificate secrets. Decode `APPLE_API_KEY_P8_BASE64` to the raw PEM expected by
`WHIP_DESKTOP_NOTARY_PRIVATE_KEY`; its API key ID and issuer map to Whip's notary
variables. Local Apple ID app-specific credentials can instead be stored with
`notarytool store-credentials` and used through `WHIP_DESKTOP_NOTARY_PROFILE`.
Keep actual values in the Keychain or GitHub secret store.

HALO publishes to Cloudflare R2, using an S3-compatible endpoint and static access
keys rather than the AWS role configured in Whip's current workflow. Reusing
that infrastructure requires endpoint/authentication configuration in CI and a
separate Whip route, artifact prefix and Electron update feed. HALO's existing
Electrobun feed must remain separate. The
[credential discovery record](../.ai-docs/plans/desktop-app/evidence/release-credentials.json)
tracks which local credentials and repository permissions were verified.

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
`scripts/continuity.mjs` verifies real signed retained workers using a loopback
fake provider. The staged smoke does not establish production-fuse acceptance.
`node apps/desktop/scripts/startup.mjs` measures the verified signed app through
LaunchServices with 30 warm attachments and 30 retained daemon starts. It uses
private fixture homes, real retained messages, a fixed read-only native DOM probe
and the production fuses. It records polling/paint overhead and failed attempts.
Its three first-install samples are reported separately; they do not establish
a cold-cache p95. Use `--self-test` to check probe isolation without launching.
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
