# Installation and local development

Detailed setup instructions for WhipCode: the `whipcode` CLI and WHIP Desktop.
This checkout implements the native v4 backend in an unmerged redesign stack.
Published releases and installed applications are separate; the source checks
below do not authorize upgrading an existing installation.
`main` is the default/stable source; `development` is the alpha integration branch.
There is one supported CLI distribution, not a separate alpha product.
For a pre-reset internal installation, use the [manual reset checklist](team-reset.md);
these instructions describe fresh installations, not a migration.
For the recommended Desktop beta quickstart, see the [project README](../README.md#quickstart).

## Install

The standalone installer requires `curl`, Python 3, and `sha256sum` or `shasum`.
Choose a release from [GitHub Releases](https://github.com/context-labs/whip/releases)
and use its pinned installation command. For releases with the two-script split,
substitute its tag below; no version environment variable is needed:

```sh
curl -fsSL https://github.com/context-labs/whip/releases/download/<tag>/install.sh | sh
```

That release's `install.sh` installs its exact version. Its `latest.sh` always
selects the newest complete stable v1+ release, even when downloaded from an
alpha release. It fails clearly until a stable release exists, never falling back
to an alpha or legacy version. These policies ignore inherited version/channel
selection variables; destination and authentication controls are unchanged.

Both install `whipcode` into `~/.local/bin` by default; add that directory to your
`PATH` if needed. `WHIPCODE_BIN_DIR` selects another destination. Older immutable
release installers keep their original behavior. For current source-installer
behavior and explicit prerelease discovery, this command remains available:

```sh
curl -fsSL https://raw.githubusercontent.com/context-labs/whip/main/install.sh | WHIPCODE_CHANNEL=prerelease sh
```

Or build the packaged CLI from source with Go 1.27+, Node 24, and Task:

```sh
git clone --branch main https://github.com/context-labs/whip.git
cd whip
npm ci
task build       # ./whipcode, including the embedded renderer/native helper
task install     # install into GOBIN or GOPATH/bin
```

On macOS, the native helper also requires Xcode command-line tools. Bare
`go install .../cmd/whip@latest` is not the supported product build: it names the
wrong executable and omits required packaged assets. Local builds use `dev`
unless `WHIPCODE_VERSION` is explicitly supplied.

Day-to-day integration uses `development` once provisioned; trusted operators may
push directly, with PRs optional. Follow the [branch, promotion and backmerge policy](releases.md#one-source-one-candidate-one-publisher).

Then run `whipcode` in your project folder. The native terminal opens its
composer and creates or resumes a session on the selected host. An unconfigured
model opens **Providers and accounts**. Press **Esc** to keep drafting and use
`/setup` to return. Opening the menu does not start account login or import
retired credentials. Model/provider readiness distinguishes configured routes,
credential availability, catalog discovery and actual inference; a saved key or
successful catalog request does not prove model access.

Choose a provider preset or **Add custom provider…**. Presets offer their declared
host environment variables, a masked API-key field and supported account login.
Custom endpoints offer an explicit provider kind, URL and credential source:
private key, host environment, private host file or no authentication. Pasted
keys are published to bounded private files on the execution host. File sources
require a clean absolute path to an owned private regular file. Secrets never
belong in browser profiles or session configuration.

After saving a route, choose a suggested or catalog model explicitly. `/model`
offers session-only selection or saving the host default as well;
`/model-for-session` offers only the session scope. Existing admitted turns retain their captured
configuration. Menus preserve the draft and explicit confirmation controls
changes. Account login, uncertain provisioning, logout and recovery remain
separate actions; OpenAI subscription credentials are separate from API billing.

The web and desktop welcome screen also lets you draft first, connect a provider,
choose a project folder and send. Credentials and model defaults belong to the
selected execution host; display preferences belong to the viewing device.

Native host declarations live at `$WHIPCODE_HOME/runtime-v4/host.json` (default
`~/.whipcode/runtime-v4/host.json`). The native backend does not read or migrate
retired `config.json`, `providerKeySources` or `apiKeyEnv` declarations. Use the
provider controls to select an explicit environment or file reference. There is
no implicit search of secret files or another application's credentials.
A running host sees the environment it inherited at launch; changing exported
variables requires an intentional host restart. File credentials are resolved
from their explicit source when a new provider call is prepared.

`whipcode auth openrouter --env` selects the execution host's
`OPENROUTER_API_KEY`; it does not validate a named secrets file. Without `--env`,
`whipcode auth openrouter` accepts a masked prompt or the invoking environment
and publishes a private key on the host. Native provider setup and account
contracts are documented in [the backend domain](backend-domain.md).

Known-provider metadata is bundled from Models.dev. Maintainers can run
`task models:update` to refresh the reviewed subset and `task models:check` to
check generated files offline. Live provider catalog and inference results are
separate evidence from bundled metadata.

For a noninteractive API-key setup:

```sh
whipcode auth openrouter
whipcode run -p openrouter -m moonshotai/kimi-k3 "inspect this repository and explain its architecture"
```

Select an execution language once when creating a session:

```sh
whipcode --rlm-engine quickjs
whipcode run --rlm-engine quickjs --permission-mode automatic --max-cost 2 --max-tokens 50000 --effort high "inspect this repository"
```

Select an agent definition the same way. `coding` is the default; the
deliberately limited `junior-developer` edits and runs tests but cannot
delegate, reach MCP servers, or drive a browser:

```sh
whipcode --agent junior-developer
whipcode run --agent junior-developer --permission-mode automatic "add a unit test for the parser"
```

`--resume ID --agent NAME` asserts the session's definition and rejects a
conflict. `starlark` remains the default language; use native execution settings to
change the host preference for new sessions. JavaScript runs in bundled QuickJS/WASM, without
Node.js or npm. Children inherit their root's language. Forks preserve it;
`--resume ID --rlm-engine NAME` asserts the existing selection and rejects a
conflict. Headless `--max-cost` (USD) and `--max-tokens` cap the entire session
model ledger, including descendants; `--permission-mode automatic` explicitly
selects the existing Full Access policy for a new session.

Declare MCP servers and explicitly enable any repository `.mcp.json` source
through native host integration settings. Publication alone grants no session
authority. Use `/mcp` for declarations and connection controls, and `/agents` for
the durable recursive tree. Host/server capability checks still apply to every
agent invocation.

Manage the local runtime daemon directly when testing or upgrading a checkout:

```sh
whipcode daemon status [--json]
whipcode daemon start
whipcode daemon stop [--timeout 10s] [--force]
whipcode daemon restart [--timeout 10s] [--force]
whipcode daemon logs [-f] [-n 200]
```

`restart` replaces the running daemon with the currently invoked `whipcode`
binary. Normal stop and restart checkpoint durable state first; `--force` is
only a fallback for an unresponsive daemon.

## Standalone releases and updates

The next train is `v1.0.1-alpha.N` (`N` is the existing workflow run number), then
approved stable `v1.0.1`. Development pushes can publish alpha after enablement;
main pushes run CI only, with stable manually approved. See [release operations](releases.md)
for dispatch rules and rollout acceptance requirements. Raw CLI assets are
`whipcode-<linux|darwin>-<x64|arm64>`; Desktop downloads are
`whipcode-desktop-darwin-arm64.dmg` and `.zip`. Versions live in the release tag
and CDN directory, not these basenames.

```sh
whipcode update
whipcode daemon start
whipcode web
```

The daemon opens only its Unix socket by default. `whipcode web` requires that
daemon to be running, starts a separate foreground gateway, opens the browser,
and waits; `--no-open` skips the browser but still stays running. Ctrl+C stops
the gateway without stopping daemon work. `WHIPCODE_NETWORK=1` explicitly opts
into managed gateway startup; `WHIPCODE_LISTEN` alone does not. See
[web access](web-app.md#run-the-packaged-application-locally) for fixed ports,
open-existing `--url` mode, compatibility, and trusted proxy configuration.

`whipcode update` replaces the invoked standalone installation and requests only
its daemon's restart. An alpha build follows CLI channel `prerelease`; a stable
build follows `stable`. `WHIPCODE_CHANNEL` can explicitly select either.
Prerelease discovery chooses the highest eligible SemVer and permits graduation
to stable: `1.0.1-alpha.N` sorts above `1.0.0`, but below stable `1.0.1`.
Desktop-owned backends refuse independent CLI updates.

To choose a destination for an exact release, replace `<tag>` with its published
tag. A pinned installer can roll back executable bytes; it does **not** make newer
saved state compatible with an older backend:

```sh
curl -fsSL https://github.com/context-labs/whip/releases/download/<tag>/install.sh \
  | WHIPCODE_BIN_DIR="$HOME/.local/bin" sh
```

To request the newest stable instead, change `install.sh` to `latest.sh`. An old
snapshot of `latest.sh` still resolves the stable version dynamically; it does not
update its own installer implementation.

The installer verifies a complete platform asset set and SHA-256 checksums
before atomic replacement. Old CLI tags and Desktop tags are not candidates.

### Desktop installation and upgrades

Alpha still installs **Whip Beta** / Desktop channel `beta`. New Beta builds use
`https://whipcode-alpha-releases.inference.net`; stable downloads/updates do not change.
Track release/update verification separately in the [acceptance checklist](roadmap.md).

**Existing Beta testers:** after the first new alpha is published, quit Whip Beta
and manually install that release's signed
`whipcode-desktop-darwin-arm64.dmg` from GitHub Releases once. Keep local data;
do not run the pre-reset cleanup just to switch feeds. Old Beta apps retain their
embedded old feed, which stays readable but stops advancing after cutover; there
is no transparent redirect. The newly signed app embeds the isolated feed for
subsequent updates. This does not migrate incompatible saved state or silently
replace a standalone/remote daemon. Follow the ownership and restart rules below.

Every macOS desktop release, including betas, bundles its matching **whipcode**
backend. It uses that bundled build for installation and managed upgrades;
it does not fetch an independently updated standalone CLI binary. Desktop and CLI
artifacts share a release version, but the app owns its bundled backend updates.
Copying the app into Applications alone does not replace an existing binary.

On a clean Mac, choose **Set up this Mac** on the welcome screen. It installs the
verified bundled backend (default: `~/.local/bin/whipcode`), connects, and opens
provider setup. An existing compatible installation is reused. Desktop and
terminal share that executable and its daemon under `~/.whipcode`.

For manual setup, open **Settings → Servers → This Mac → Local server settings**
to choose an existing executable (for example, `/usr/local/bin/whipcode`) or use
**Install whipcode**. **Test Connection** reports status without starting work.
Diagnostics, explicit restart, and remote host controls remain available there.

Backend upgrades depend on how the installation is managed:

- **Installed through Desktop:** **Install whipcode** records the executable as
  desktop-managed. After an app upgrade, Desktop updates that same executable
  to the bundled build and brings its daemon to the matching version. Use
  Desktop updates to upgrade this installation.
- **Existing or manually selected executable:** Desktop uses it if compatible,
  but does not automatically replace or update it. Continue using its standalone
  installer/update command or source-build workflow. Installing a beta does not
  automatically enroll an existing binary in desktop-managed updates.
- **Remote SSH or URL backend:** Update the backend explicitly on the remote
  machine; upgrading Desktop does not upgrade remote hosts.

Downloading an app update leaves running work alone. **Restart and update**
approves the backend restart as well; after a manual app upgrade, Desktop asks
before interrupting a running daemon. A restart can interrupt active work from
Desktop, terminal, web, or mobile, while sessions and configuration remain on
disk. Stable and beta installations cannot silently take over each other's
managed backend.

Desktop uses the private Unix socket and needs no web listener. To add a fixed
web endpoint to its running daemon without a restart, run
`WHIPCODE_LISTEN=127.0.0.1:8080 whipcode web`. Without an explicit bind the
gateway tries `127.0.0.1:4444`, falling back to an ephemeral loopback port only
if occupied. See the [desktop guide](desktop.md) for packaging, setup, and
backend upgrade details.

### Update your local installation from source

For an already-installed **post-reset** app on an Apple Silicon Mac, use the
commands below. This is not the clean-project reset or a migration from old builds:

```sh
task update:local
# Equivalent without Task:
npm run update:local
```

This updates the existing `/Applications/Whip.app` and its saved `whipcode`
executable (falling back to `/usr/local/bin/whipcode`). It runs `npm ci`, builds
the web UI, Swift helper, Go backend and desktop app from the **current working
tree, including uncommitted changes**, then signs and verifies the package.
It does not pull Git changes. Run `git pull` yourself first if desired.
Signing failures stop packaging immediately and report the signer error, before
the installed app or daemon is changed. Vite's large-chunk warning is nonfatal.

Once the build passes verification, the command quits Whip normally, retains the
previous app and executable, installs the new app and its exact bundled backend,
restarts the shared daemon, checks its build ID and reopens Whip. **Running this
command interrupts active agent work across connected clients.** Sessions,
configuration, credentials and app settings stay in place. If Whip has unsaved
attachments, resolve its normal quit dialog; the script never force-quits it.
macOS may report “User canceled (-128)” while Whip saves drafts asynchronously;
the updater waits up to 30 seconds for the app to exit before replacing it.
If you cancel the quit dialog or Whip stays open, the update stops with the
installed app and backend unchanged.
An already open browser tab may need a reload to load the new web UI.

Requirements: Node 24, Go 1.27+, Xcode/Swift and a Developer ID Application signing
identity in your keychain. The script automatically selects the sole identity
matching the installed app's signing team. If there are multiple identities, set
`WHIP_DESKTOP_SIGN_IDENTITY` to the certificate name or SHA-1; use
`WHIP_DESKTOP_TEAM_ID` when explicitly choosing another team. There is no `sudo`
step: the existing app and executable directories must be writable by your user.

The installed app's version and channel are retained by default. Each run gets a
unique `local-<timestamp>-<commit>` backend build ID. `WHIP_DESKTOP_VERSION` and
`WHIPCODE_VERSION` can override these values. Local builds have release updates
disabled. Notarization is optional: set `WHIP_DESKTOP_NOTARIZE=1` and
`WHIP_DESKTOP_NOTARY_PROFILE` to your saved `notarytool` keychain profile, or use
the API credentials supported by [desktop packaging](../apps/desktop/forge.config.cjs).

```sh
# Choose another installed app; its saved executable and channel are respected.
task update:local -- --app "/Applications/Whip Beta.app"
# Explicit backend path must match Desktop's saved choice, if one exists.
task update:local -- --executable "$HOME/.local/bin/whipcode"
task update:local -- --help
```

The normal runtime home is `~/.whipcode`; `WHIPCODE_HOME` remains supported.
The daemon inherits your shell's runtime environment. Ordinary startup is
socket-only; `WHIPCODE_NETWORK=1` explicitly opts into an in-process managed gateway,
and `WHIPCODE_LISTEN` alone does not. Export any custom runtime environment
before invoking the command. A foreground `whipcode web` can serve a compatible
running daemon without another restart.

The command prints a `.whip-local-update-*` directory beside the installed app
containing `previous.app` and `whipcode.previous`. These copies are retained until
you remove them. Build or staging failures leave the installed app and daemon
untouched. If a later step fails, fix the reported error and rerun the command;
do not automatically restore an older backend after a database schema upgrade.
Concurrent runs from the same checkout are refused. If an interrupted run leaves
`apps/desktop/.update-local.lock`, inspect its `pid` file and remove the lock
directory only after that process has exited. Test the workflow without touching
your installation with `task test:update-local`.

## Test fresh onboarding in Docker

From the checkout you want to test, run:

```sh
task onboarding:docker
# Equivalent without Task:
node scripts/onboarding-docker.mjs
```

This builds your **current working files, including uncommitted and untracked
source**, opens a clean TUI in the current terminal, and serves the production web
app at **http://localhost:4000**. Both clients share one daemon inside the container.
Provider setup in either client becomes available to the other. Ordinary checkouts
and linked Git worktrees both work; no commit, local Go installation, or host
`npm ci` is required.

Requirements: Node 24, Git, an interactive terminal, a running local Linux Docker
engine (such as OrbStack or Docker Desktop), and an available port 4000. Dependencies
and compilers are installed during the image build. The first run downloads them;
later runs reuse Docker's dependency and compilation caches while rebuilding
changed source. Each invocation checks the build before starting the container.

The container starts with no saved providers or sessions and no inherited host
credentials. Its test project is a disposable Git repository at `/workspace`.
The TUI opens directly to its normal composer with the provider connection
dialog. Esc closes the dialog; `/setup` reopens it.
Your installed Whip, normal daemon, and source files are separate from this test
environment. **Quitting the TUI removes the container, its credentials, sessions,
and test files.** Run the command again for another clean start; cached builds
remain available. The web app runs for the lifetime of that TUI.

For a clean **web** onboarding test, use a new private browser session and close
the previous private session between runs. Resetting the container does not clear
your browser's saved state. To compare initial TUI and web onboarding independently,
start a fresh container for each; configuring one client also configures the other.

For Inference.net or OpenAI device login, open the displayed URL on your Mac and
approve the code. No additional callback port is needed. Automatic host browser
opening, native clipboard integration, and macOS computer tools are unavailable
inside this Linux environment.

The launcher prints its unique container name. While it is running, inspect it
from another terminal:

```sh
docker exec <container-name> whipcode daemon logs -n 60
docker exec <container-name> whipcode daemon status --json
# Stop this test environment from another terminal:
docker stop <container-name>
```

Run `task test:onboarding-docker` for the launcher and renderer provenance tests.
Container source metadata is explicitly marked local; release builds retain their
normal Git provenance checks and reject artifacts using that local override.

