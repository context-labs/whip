```
        ▄ ▄   ▄ ▄ ▄ ▄   ▄
        █ █   █   █ █▀▀▀█▀▀▄
        ▀█▀ ▄▄█   █ █   █
        whip — a recursive coding-agent runtime in Go
```

whip gives every model session one interface: `rlm_exec`. Short, bounded
Starlark cells use host modules for files, shell, MCP, state, artifacts,
messages, and recursive agents. Root and child sessions have the same model
loop and tool surface; identity, capabilities, budgets, and ancestry are the
only differences.

The local daemon owns model turns, side effects, child processes, and durable
state. The TUI, `whip run`, ACP, and MCP bridge are clients, so disconnecting a
UI does not abandon or duplicate admitted work.

## Why whip

- **Large context stays addressable.** History, corpora, and large outputs are
  retrieved through paged raw history and immutable content handles, in bounded, cited slices.
- **Delegation is recursive, not a second agent type.** A child can inspect,
  act, use MCP, create children, and receive later turns through the same
  interface as the root.
- **Coordination is explicit.** A child’s ordinary response finishes its local
  turn. Durable `messages.send/list/read/ack` is the communication contract.
- **Authority is centralized.** File, shell, browser, computer, and state
  operations cross daemon-owned capability, budget, and permission checks.
- **Recovery is conservative.** Stable command IDs deduplicate client retry;
  committed state survives restart and uncertain effects are not replayed.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/context-labs/whip/main/install.sh | sh
```

Or build from source with Go 1.27 or newer:

```sh
go install github.com/context-labs/whip/cmd/whip@latest
```

Then run `whip` in your project folder. The TUI opens directly, without a
folder-trust prompt. Tool approvals follow the session's saved permission level.
Whip detects supported credentials on the execution host and uses your selected
model when it is ready. Otherwise, a provider dialog opens over the composer:
choose Inference.net (recommended), OpenRouter, OpenAI (API key or ChatGPT
subscription) to connect and automatically use the recommended model. Other
providers open a model picker; selecting a model returns directly to the composer.
First-time onboarding saves your choice for new sessions.

Press **Esc** to close the dialog and draft in the normal composer. `/connect`,
`/auth`, or submitting an unconfigured draft reopens it. Type in its focused
search field to filter providers; arrow keys select and Enter connects. Keys
stay in a separate masked field. Connecting preserves your draft; press **Enter** to send when ready.
Known providers already have their API URLs: choose one and paste its key.
A **✓** marks available credentials; it does not certify inference access.
OpenAI groups API billing and ChatGPT subscription choices without combining their
credentials. Whip recognizes supported environment keys and explicitly configured
local key files. Daemon startup and opening setup save missing provider entries
that reference those keys; secret values stay in their original source.
See [local key discovery](docs/models-providers.md#local-key-discovery) for file configuration.
Choose **Custom endpoint** to configure an OpenAI-compatible endpoint in
the TUI through compact steps for its name, API root URL, and API key, host
environment-variable name, or explicit **No authentication**. Whip discovers models; **Enter model
manually…** covers endpoints without model discovery. **ctrl+e** on a provider
opens connection management. Changes persist in the execution host's existing
configuration files, including across restarts.
Legacy `/auth provider key` also opens masked confirmation. Existing session
choices remain intact. Fresh installations leave external Claude/Codex MCP
imports off; enable them explicitly later if wanted.

The web and desktop welcome screen lets you draft first, connect a provider,
choose a project folder and send. **Ask** is the initial tool permission level;
changing it applies to the session you create. Credentials and defaults belong
to the selected execution host. Custom OpenAI-compatible endpoints are supported through
[provider configuration](docs/models-providers.md#supported-provider-types-and-custom-endpoints);
create them in the TUI, then use them from either application.

To reuse a secrets file, add its path to the execution host's `~/.whip/config.json`
(use `~/.whipcode/config.json` for whipcode):

```json
{
  "providerKeySources": {
    "envFiles": ["~/.secrets/providers.env"],
    "keyFiles": {"CEREBRAS_API_KEY": "~/.secrets/cerebras.key"}
  }
}
```

For example, `providers.env` can contain `OPENROUTER_API_KEY=your-key`; the Cerebras
file contains only its key. Open `/connect` or refresh **Providers & models** to
discover them. Whip saves `apiKeyEnv` references, never copies these file values
into its configuration, and does not search arbitrary folders or read OpenCode
credentials. File changes are read on discovery and new client creation; reload
an existing session after rotating a key. Newly exported environment variables
require `whip daemon restart` (or `whipcode daemon restart`).

The CLI can also validate a named file reference with `whip auth openrouter --env`.

Known-provider metadata is bundled from Models.dev. Maintainers can run
`task models:update` to refresh the reviewed subset and `task models:check` to
check generated files offline. Live provider model lists remain authoritative.

For a noninteractive API-key setup:

```sh
whip auth openrouter
whip run -p openrouter -m moonshotai/kimi-k3 "inspect this repository and explain its architecture"
```

Select an execution language once when creating a session:

```sh
whip --rlm-engine quickjs
whip run --rlm-engine quickjs --permission-mode automatic --max-cost 2 --max-tokens 50000 --effort high "inspect this repository"
```

Select an agent definition the same way. `coding` is the default; the
deliberately limited `junior-developer` edits and runs tests but cannot
delegate, reach MCP servers, or drive a browser:

```sh
whip --agent junior-developer
whip run --agent junior-developer --permission-mode automatic "add a unit test for the parser"
```

`--resume ID --agent NAME` asserts the session's definition and rejects a
conflict. `starlark` remains the default language; configure `rlm.defaultEngine`
to change the preference for new sessions. JavaScript runs in bundled QuickJS/WASM, without
Node.js or npm. Children inherit their root's language. Forks preserve it;
`--resume ID --rlm-engine NAME` asserts the existing selection and rejects a
conflict. Headless `--max-cost` (USD) and `--max-tokens` cap the entire session
model ledger, including descendants; `--permission-mode automatic` explicitly
selects the existing Full Access policy for a new session.

Drop a `.mcp.json` in a repository to make its servers available through the
selected execution language’s `mcp` module. Use `/mcp` for connection status and `/agents` for the
durable recursive tree.

Manage the local runtime daemon directly when testing or upgrading a checkout:

```sh
whip daemon status [--json]
whip daemon start
whip daemon stop [--timeout 10s] [--force]
whip daemon restart [--timeout 10s] [--force]
whip daemon logs [-f] [-n 200]
```

`restart` replaces the running daemon with the currently invoked `whip`
binary. Normal stop and restart checkpoint durable state first; `--force` is
only a fallback for an unresponsive daemon.

## Whipcode branch builds

Install the latest validated `whip-rlm` build alongside whip:

```sh
curl -fsSL https://raw.githubusercontent.com/context-labs/whip/whip-rlm/install-whipcode.sh | sh
```

The installer supports Linux and macOS on x64 and arm64. It requires `curl`,
Python 3, and `sha256sum` or `shasum`; downloads are verified against the release
checksums. Run `whipcode` to complete its independent setup.

Whipcode uses `~/.whipcode/config.json` and keeps its sessions, credentials,
browser profiles, and daemon under `~/.whipcode`. Set `WHIPCODE_HOME` to choose
another home. It does not read `WHIP_HOME` or copy your whip configuration.
Project/shared skills and explicitly configured external credentials remain
available through the existing integration mechanisms.

```sh
whipcode --version
whipcode update
whipcode daemon start
whipcode web
```

The daemon enables its localhost HTTP/WebSocket listener by default. Set
`WHIPCODE_NETWORK=0` to disable it (`WHIP_NETWORK=0` for source-built `whip`).
See [web access](docs/web-app.md#run-the-packaged-application-locally) for fixed
ports and trusted proxy configuration.

`whipcode update` installs into the invoked executable's directory and restarts
only its daemon. To choose a destination or pin/roll back to an exact build,
replace the example tag below with a published whipcode tag:

```sh
curl -fsSL https://raw.githubusercontent.com/context-labs/whip/whip-rlm/install-whipcode.sh \
  | WHIPCODE_BIN_DIR="$HOME/.local/bin" WHIPCODE_VERSION=whipcode-v0.0.4 sh
```

Successful pushes to `whip-rlm` publish `whipcode-v0.0.N` GitHub prereleases after
CI and security checks. Stable whip continues to use `v*` releases. Source builds
use `npm ci && task build:whipcode`; `task install:whipcode` installs into GOBIN
or GOPATH/bin. These local builds report `dev` unless `WHIPCODE_VERSION` is set.

### Desktop installation and upgrades

Every macOS desktop release, including betas, bundles its matching **whipcode**
backend. It uses that bundled build for installation and managed upgrades;
it does not fetch the latest independently published standalone CLI release.
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

For a CLI-first start with the desktop's fixed local web endpoint, use
`WHIPCODE_LISTEN=127.0.0.1:8080 whipcode daemon start`. See the
[desktop guide](docs/desktop.md) for packaging, setup, and backend upgrade details.

### Update your local installation from source

From this checkout on an Apple Silicon Mac, run:

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
the API credentials supported by [desktop packaging](apps/desktop/forge.config.cjs).

```sh
# Choose another installed app; its saved executable and channel are respected.
task update:local -- --app "/Applications/Whip Beta.app"
# Explicit backend path must match Desktop's saved choice, if one exists.
task update:local -- --executable "$HOME/.local/bin/whipcode"
task update:local -- --help
```

The normal runtime home is `~/.whipcode`; `WHIPCODE_HOME` remains supported.
The daemon inherits your shell's runtime environment and defaults to
`WHIPCODE_LISTEN=127.0.0.1:8080`, as Desktop does. Export any custom runtime
environment before invoking the command.

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
dialog. Esc closes the dialog; `/connect` reopens it.
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
docker exec <container-name> whip daemon logs -n 60
docker exec <container-name> whip daemon status --json
# Stop this test environment from another terminal:
docker stop <container-name>
```

Run `task test:onboarding-docker` for the launcher and renderer provenance tests.
Container source metadata is explicitly marked local; release builds retain their
normal Git provenance checks and reject artifacts using that local override.

## Documentation

- [Manual](docs/README.md)
- [Architecture](docs/architecture.md)
- [Frontend architecture and design guide](docs/frontend.md) — start here for frontend work
- [macOS desktop build and packaging](docs/desktop.md) — shared renderer, daemon lifetime and release setup
- [Recursive runtime](docs/rlm-runtime.md)
- [Tools and modules](docs/tools.md)
- [Agent loop](docs/agent-loop.md)
- [Concurrency and ownership](docs/concurrency.md)
- [Feature map](docs/features.md)
- [Evaluations](evals/README.md) — fixed Smoke/Medium/Full Frontier profiles, paired runs, reports and accepted baselines
