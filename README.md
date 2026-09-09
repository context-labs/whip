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

Then run `whip`. It defaults to inference.net models, and any
OpenAI-compatible endpoint can be configured as a provider.

```sh
whip auth openrouter
whip run "inspect this repository and explain its architecture"
```

Drop a `.mcp.json` in a repository to make its servers available through the
Starlark `mcp` module. Use `/mcp` for connection status and `/agents` for the
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
WHIPCODE_NETWORK=1 whipcode daemon start
whipcode web
```

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

In **Execution hosts → This Mac**, choose an existing executable (for example,
`/usr/local/bin/whipcode`), or use **Install whipcode** for a fresh installation
(default: `~/.local/bin/whipcode`). Desktop and terminal share that executable.
**Test Connection** reports installation and daemon status without starting it;
**Connect** starts or attaches to that installation under `~/.whipcode`.

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

Once the build passes verification, the command quits Whip normally, retains the
previous app and executable, installs the new app and its exact bundled backend,
restarts the shared daemon, checks its build ID and reopens Whip. **Running this
command interrupts active agent work across connected clients.** Sessions,
configuration, credentials and app settings stay in place. If Whip has unsaved
attachments, resolve its normal quit dialog; the script never force-quits it.
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
