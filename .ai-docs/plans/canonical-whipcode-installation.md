# Canonical whipcode installation on this Mac

Research and execution date: September 8, 2026. Status: completed on this Mac.
The approved plan below preserves the original inventory and rationale. See the
[execution record](canonical-whipcode/README.md) for installed versions, cleanup
results, validation and operating instructions.

The user wants all historical local Whip state discarded and one canonical
whipcode installation. They selected a build from the freshly merged checkout,
rather than published releases. The source checkout is
`/Users/samheutmaker/Desktop/context-labs/src/rlm/whip`, currently on `whip-rlm`
at `f3fc97435294913aae7b3f62adf30f90c28f2dcb`.

Confirmed follow-up: retain the desktop app and update both its packaging and
connection workflow to use the canonical `whipcode` executable. Desktop removal
is no longer an alternative in this plan.

## Intended result

| Concern | Canonical choice |
| --- | --- |
| Installed CLI/backend | `/usr/local/bin/whipcode`, built from this checkout |
| Build identity | Explicit local version containing the source commit; record binary SHA-256 |
| Application state | Fresh `/Users/samheutmaker/.whipcode` |
| Config | Fresh `~/.whipcode/config.json`; no old config or `.bak` migration |
| Sessions and daemon ownership | One `~/.whipcode/runtime-v2` database, socket and owner lock |
| Native computer helper | Matching embedded build, extracted under `~/.whipcode/bin` |
| Local web access | One fixed loopback endpoint, proposed `http://127.0.0.1:8080` after the old listener is stopped |
| Desktop | Rebuilt `/Applications/Whip.app`, using `/usr/local/bin/whipcode` for This Mac |
| Lifecycle | CLI and desktop start/attach through the same canonical executable and home; replacement/restart remains explicit |
| Updates | Rebuild from the checkout, stop, replace, restart; do not run the published-release updater for this source-managed install |

One daemon may legitimately have several workers, MCP subprocesses and connected
clients. Success means one backend installation and one persistent runtime home,
not literally one process or no compiled test artifacts anywhere on disk.

No historical-state backup is required. Keep source repositories, Git history,
uncommitted work and worktrees. Do not touch the GPU host, the working phone
installation, Tailscale membership, Codex/Claude state, unrelated browser
profiles, provider account credentials outside Whip, or Apple signing credentials.

## Observed inventory

These are snapshots, not PIDs safe to reuse later. Re-identify every process
immediately before stopping it. Discovery used executable/build metadata,
`daemon status --json`, open files/sockets, startup configuration and source
inspection; it did not read session messages or print credential values.

| Daemon PID | Executable | State home | Network/status |
| --- | --- | --- | --- |
| 67401 | `/usr/local/bin/whip` | `~/.whip` | Running, Unix socket only; build `whip-rlm-9b2aa6d` |
| 83974 | `/usr/local/bin/whipcode` | `~/.whipcode` | Running, Unix socket only; build `whipcode-v0.0.4` |
| 85232 | `~/.whip-web-v4/bin/whip` | `~/.whip-web-v4` | Running at `127.0.0.1:8080`; permission-fix build based on `04b4c062` |
| 97086 | Main checkout's `./whip` | `~/.whip-v2-test.0k8SYz` | Process and owner lock present; status reports unhealthy; listener at `127.0.0.1:60843` |
| 59869 | `/tmp/whipcode-multihost-local-desktop-origin` | `/tmp/whipcode-multihost-local-home` | Running at `127.0.0.1:43110`; build `multihost-dev` |

Also running: `/Applications/Whip.app` 0.1.1 (PID 93896 and Electron helpers),
and a `whipcode` terminal client (PID 96220). Multiple child processes belong to
the daemon groups above. Whip-launched browser/MCP/tool processes must be checked
as descendants, not assumed safe to kill just because their name is familiar.

The two commands on PATH are distinct regular files, both owned by this user:

- `/usr/local/bin/whip`: source `9b2aa6d`, SHA-256
  `be96aceaba781c47383f65c30b3e07d5240bc5e2cff925701a4a8b5c11eb12d7`.
- `/usr/local/bin/whipcode`: source `5164e041`, SHA-256
  `56411ab22a6b3ea9d67929bb5e927f6fe4873967af88b667f60c1aae9e5b75ad`.

The app bundles another `whip` 0.1.1 from source `879ad878`, predating the mobile
merge. Electron user data also retains a 0.1.0 runtime installation.
The main checkout has an older generated `./whip`; the publishing worktree has
a generated `./whipcode`; the integration worktree has staged desktop binaries.

Approximate allocated sizes from `du -sk`, excluding temp trees and most build
artifacts: `~/.whip` 1.36 GiB, `~/.whip-web-v4` 324 MiB,
`~/.whip-v2-test.0k8SYz` 52 MiB, `~/.whipcode` 8 MiB,
Electron data 41 MiB, `/Applications/Whip.app` 350 MiB, and the Whip research
cache 70 MiB. These are about 2.2 GiB together; this is not a guaranteed net
recovery estimate because the replacement will consume some space.

There is substantial temporary test debris:

- `/tmp`: 12,069 `whip-daemon-test-home-*` and 1,892
  `whip-cli-test-home-*` entries, plus 840 other Whip-named entries.
- This user's macOS temp directory: 49,163 `whip-501-<hash>` runtime directories
  (42,737 empty), plus test homes and other artifacts. Count by ownership and
  purpose before deletion; a matching name alone is insufficient.
- Short socket-directory names are intentional: long runtime-home paths exceed
  Unix socket limits, so `internal/daemon/socket_unix.go` hashes the home into
  the temp directory. These paths can hold live locks/sockets and must not be
  deleted while their owners or tests are running.

No Whip-related plist was found in the user's LaunchAgents or the third-party
system LaunchAgents/LaunchDaemons directories. `launchctl list` showed the
running GUI application but no separate Whip service label. The background-item
database showed no Whip item. Checked shell startup files contained no Whip
entries; no relevant current-shell or launchd environment overrides were found.
No matching Homebrew package/cask, current Node global package, or native-messaging
manifest was found in the checked locations. Local Tailscale Serve configuration
is currently `{}`. These are bounded inventory findings, not a guarantee that
every custom script or interactive shell function on the disk has been examined.

## Why deleting the old binary is insufficient

Distribution identity is compiled in. `internal/buildinfo/buildinfo.go` selects
`WHIP_HOME` / `~/.whip` or `WHIPCODE_HOME` / `~/.whipcode` from the link-time name.
Renaming a `whip` executable to `whipcode` does not change that identity.
Daemon ownership is per state home, so different homes allow simultaneous daemons.
Replacing a file also does not replace an already-running process.

The desktop's `prepareLocal` executes its verified bundled **whip** to discover
the local socket. If that home has no healthy daemon, it installs and starts a
retained bundled runtime. It does not currently select `/usr/local/bin/whipcode`
as the local backend. Quitting the GUI intentionally leaves accepted work running.
Consequently, reopening the existing app could recreate `~/.whip` after cleanup.
See [desktop runtime](../../apps/desktop/src/runtime.ts),
[desktop packaging](../../apps/desktop/scripts/build.mjs), and
[desktop lifecycle documentation](../../docs/desktop.md).

## Phased execution plan

### 1. Update desktop packaging and connections, then prepare the replacement

This is a required implementation phase before retiring the old installation.
The desktop remains installed and uses `/usr/local/bin/whipcode` as its local
backend. Runtime-selection and installer behavior below are proposed changes,
not switches supported by the existing app.

**Packaging and installation**

1. Build a real whipcode distribution with
   `github.com/context-labs/whip/internal/buildinfo.Name=whipcode`. Update this
   desktop build's native artifact name from `whip` to `whipcode`, including
   Helpers paths, runtime manifests, verification, staging, archive checks,
   signing identifiers where appropriate, fixtures and packaging tests. Keep
   wire identifiers and `whip-computer` names unchanged where they are protocol
   or helper contracts; do not perform a repository-wide text replacement.
2. Produce one verified native backend artifact, with its matching embedded
   Swift helper and the shared renderer. Use those same backend bytes for the
   installed CLI and the desktop's installation payload. Record distribution,
   source commit, build ID, hashes and protocol compatibility in the manifest.
   Desktop package version and backend build ID may have different formats;
   their shared provenance must be verifiable rather than inferred from labels.
3. Treat the app's packaged backend as an installation payload. Normal local
   connections execute the canonical installed path, not a private daemon in
   Electron `runtimes/` or directly inside the `.app`. On this machine, install
   the verified payload at `/usr/local/bin/whipcode`; the existing directory
   and files are user-owned. Remove old retained runtime copies during cleanup.
4. Support a saved absolute executable path in native desktop settings. For
   first setup, discover `whipcode` through the resolved login PATH/known install
   locations and validate its distribution and compatibility before selecting it.
   Do not choose the legacy `whip` executable. Persist the resolved path so Finder
   launch and terminal launch select the same backend. This installation is pinned
   to `/usr/local/bin/whipcode`; runtime home defaults to `~/.whipcode`.
5. If no installation exists, present an actionable setup state with **Install
   whipcode**, **Choose executable**, and **Retry**, using the existing component
   library. Installation validates the packaged payload and destination, then
   writes atomically to the selected canonical path. A non-writable destination
   needs a clear remedy or a user-selected writable path, not a second hidden
   installation. A missing backend during ordinary reconnect must not trigger
   an unsolicited installation or silently select another executable.
6. Preserve signed helper embedding, ASAR integrity, Electron fuses and existing
   code-signing verification. Do not patch the installed signed app in place.
   Keep this source-managed desktop build's release update feed disabled.
   App replacement must preserve the configured executable path and must not
   overwrite/restart a backend with active work. Backend upgrades are a separate,
   explicit stop/install/start operation; an app update alone cannot migrate state.

**This Mac connection workflow**

1. Resolve and validate the canonical executable in Electron main. Pass it and
   its validated home through one shared local-runtime path for discovery,
   status, startup, diagnostics and any local SSH-supervisor invocation.
   Use `WHIPCODE_HOME` for this distribution; never redirect legacy `WHIP_HOME`
   into the new state directory. Keep native execution out of the renderer.
2. Probe `whipcode daemon status --json`. If healthy, attach to its returned
   Unix socket and verify the protocol/runtime identity through the existing
   SDK. Do not derive socket paths in React or create a second state owner.
3. If stopped, start that same executable with the canonical home and loopback
   settings, wait for bounded readiness, and attach. Concurrent desktop/CLI
   starts must converge on the existing owner lock and the winning daemon.
   Keep the fixed web endpoint available whether CLI or desktop starts first.
4. If unhealthy, incompatible, or owned by an unexpected build, show the actual
   path, daemon/client builds and a concise recovery action. Offer an explicit
   restart/update when needed; do not restart accepted work during discovery.
   A compatible but different build can be diagnosed without assuming it must
   be killed. The cleanup's final acceptance requires the selected exact build.
5. Extend the local connection UI with **Test Connection** and readable stages:
   locating executable, checking installation, contacting daemon, and attaching.
   The test is read-only: a stopped daemon reports that state and offers a
   separate Start/Connect action. Distinguish missing/non-executable paths,
   wrong distribution, incompatible protocol, timeout, unhealthy owner, startup
   failure, port conflict and runtime-identity change. Put detailed paths/builds
   in expandable diagnostics; never display environment secrets or raw logs.
   Account for the current CLI status path calling directory-creating helpers:
   provide read-only discovery for diagnostics, and report an absent home as
   uninitialized/stopped without creating it. Verify this with filesystem checks.
6. Preserve the SDK's existing reconnect, command-recovery and identity checks.
   Use the existing host UI's explicit identity-reset flow after the intentional
   state wipe. Quitting or reloading desktop must leave accepted work running.
   Test Connection, reconnect and switching hosts must not mutate daemon identity.
7. Keep URL and SSH hosts working through their existing transports. A local
   SSH supervisor should use the canonical whipcode executable where supported,
   not cause installation/startup of a legacy local runtime. Remote executable
   and home overrides must honor the selected remote distribution; an existing
   remote `whip` host remains supported. Do not implicitly migrate or deploy to
   `gpu-4090-sam`, rewrite its home, or assume its binary is named `whipcode`.

Implement shared connection UI in the existing app/platform boundaries and use
the existing SDK for protocol and recovery. Update `docs/desktop.md` and
`docs/frontend.md` with the resulting architecture in the implementation change.

**Required tests before the cleanup cutover**

- Packaging proves that the payload is actually whipcode, its signed bytes and
  renderer match the manifest, and the installed canonical executable matches it.
- Cover clean install, existing compatible install, missing/wrong-distribution
  executable, non-writable destination, startup timeout and port conflict.
- Test Finder launch with limited PATH, launching outside the checkout, desktop
  first, CLI first, simultaneous starts, GUI quit/reopen, daemon stop/restart,
  runtime-identity reset and reconnect after replacement.
- Confirm Test Connection does not start/restart a daemon or alter configuration.
- Confirm CLI, web and desktop observe the same runtime ID and sessions; no flow
  creates `~/.whip`, starts a legacy retained runtime, or installs another PATH copy.
- Exercise local and remote host switching, existing SSH overrides, explicit URL
  origin errors, and both active and idle daemon behavior during app replacement.

Simply setting `WHIP_HOME=~/.whipcode`, symlinking the two state directories, or
patching a signed installed `.app` is not the proposed solution: those approaches
leave independent runtime selection/update behavior or invalidate packaging.

Prepare before deleting old installations:

- Confirm current Git status and source revision. Build from the merged checkout,
  plus any reviewed desktop change. Record actual revision and dirty status.
- Use the existing web asset pipeline and the whipcode link-time identity:
  `task build:whipcode WHIPCODE_VERSION=local-<commit>` after dependencies are ready.
- Include the Swift computer helper, using the desktop build-overlay pattern to
  embed verified helper bytes without modifying the tracked empty placeholder.
  A bare CLI build with that placeholder can fall back to a relative developer
  build tree, which is unsuitable for a self-contained installation.
- Stage the resulting executable outside live state, record its version/hash,
  and validate it in a short, private disposable home. Stop that fixture and
  remove it when done. Do not start this build against any historical home.
- Ensure the installed executable can run from an unrelated working directory.
  Do not use `task install:whipcode` unqualified: it installs to GOBIN/GOPATH/bin,
  which would add another copy instead of replacing the existing PATH location.

### 2. Stop the old runtime groups

Refresh the process tree, listeners, owner locks and exact executable paths.
Check for ongoing Whip test/build tasks so cleanup does not race new fixtures.
Stop accepting work and close the Whip GUI, Whip CLI client and relevant web
clients. This interrupts/discards local Whip work as part of the requested reset;
it must not terminate the user's entire terminal or unrelated browser processes.

Unload any newly discovered Whip-specific startup jobs before stopping daemons.
None were found in this audit, so do not create extra service-management work
unless the refreshed inventory differs. On macOS, launchd jobs are managed via
launchctl; login/background entries are separate settings to inspect.
[Apple launchd guidance](https://support.apple.com/guide/terminal/script-management-with-launchd-apdc6c1077b-5d5d-4d35-9c19-60f2397b2369/mac),
[Apple Login Items guidance](https://support.apple.com/guide/mac-help/change-login-items-extensions-settings-mtusr003/mac).

Stop each daemon with its own executable and matching home override, while those
files still exist. Use `daemon stop --timeout 10s` first. The unhealthy test
daemon may require `--force`; confirm its current process and held owner lock
before escalation. The snapshot has five homes to stop, not just the two defaults.
Wait for daemon-owned workers/helpers to exit; terminate only positively identified
remaining descendants. Never use a broad `pkill -f whip` or kill from stale PIDs.

Verify no remaining Whip process holds a database, owner lock, executable or
socket scheduled for removal. Check listeners on 8080, 43110 and the recorded
test port; refresh dynamic ports instead of assuming the snapshot is permanent.

### 3. Remove the confirmed old installation and state

Produce an explicit, owner-checked cleanup manifest immediately before deletion.
Do not follow directory symlinks into unrelated paths. Historical state is to be
deleted, including backup configs and copied runtime databases, not restored.

| Path or category | Action |
| --- | --- |
| `~/.whip` | Delete entire legacy runtime home, including browser profiles, config/backups, keys stored there, sessions, logs and trust rules |
| `~/.whipcode` | Delete existing state; recreate fresh in phase 4 |
| `~/.whip-web-v4` | Delete custom installation, config, browser state and database backups |
| `~/.whip-v2-test.0k8SYz` | Delete retired test runtime after confirming its owner stopped |
| `/tmp/whipcode-multihost-local-home` and `/tmp/whipcode-multihost-local-desktop-origin` | Remove the known test home and executable after shutdown |
| `/usr/local/bin/whip` | Remove legacy command; do not replace it with an alias to hide the distinction |
| `/usr/local/bin/whipcode` | Replace with the staged canonical build in phase 4 |
| `~/Library/Application Support/Whip` | Reset GUI storage, cached host identities/drafts and retained old runtime; recreate with the canonical executable setting |
| `~/Library/Preferences/com.contextlabs.whip.plist` | Reset app-specific preferences through the appropriate preferences mechanism while the GUI is closed |
| `~/Library/Caches/whip-research` | Remove disposable Whip research cache after confirming no process uses it |
| `/Applications/Whip.app` | Replace with the rebuilt desktop using the canonical whipcode installation |
| Generated checkout executables, desktop `.stage`, `.dev` and `out` artifacts | Enumerate across registered worktrees; remove retired generated runtime copies only, preserving tracked files and active build outputs |
| Whip temp homes and hashed socket directories | Delete only enumerated, current-user-owned, inactive Whip fixtures; inspect nonempty/ambiguous entries |
| Old desktop installers/archives and temporary Whip scripts/logs | Remove confirmed generated artifacts; inspect patches/scripts that may contain uncommitted source work before including them |

Do not run a wildcard home/temp wipe or `git clean -fdx`: those would mix runtime
debris with source changes, environments and unrelated files. Do not reset the
entire macOS background-item database, browser profile or privacy database.

Clear Whip client storage only for confirmed local Whip origins and the desktop
app. The old local ports may now serve unrelated applications, so identify the
stored Whip keys/origins before resetting. Do not clear storage for the remote
GPU host or reset the phone. Remove obsolete unpacked extension registrations
only if found; no installed Whip extension was found in the checked Chrome/Edge
extension manifests.

### 4. Install and bootstrap the fresh runtime

After old owners have exited, stage the new binary in `/usr/local/bin` and rename
it atomically to `whipcode`. Validate file ownership, executable permissions,
version and hash. This directory already contains the user-owned installed
commands; an additional PATH entry or installation location is unnecessary.

Create `~/.whipcode` with owner-only permissions and generate a valid fresh
configuration through normal startup/setup. Keep config/credentials owner-only.
Do not copy the old `config.json`, `.bak`, runtime database, permissions, standing
instructions, host pins or browser profile. Old Whip-owned login data will be
gone: authenticate the desired provider again, or use an intentional existing
environment/secret reference. Never copy credential values into this plan.
Codex/Claude/Inference tooling's own credentials remain untouched.

Review fresh defaults for external credential resolution and MCP import:
the config supports references to external credentials, and Claude/Codex MCP
imports default to enabled when their policy is unspecified. Decide those fresh
settings explicitly rather than mistaking imported external tools for old Whip
state returning. Re-grant only the desired working-directory and tool permissions.

Start the canonical daemon with a fixed loopback listener, proposed:

```sh
WHIPCODE_LISTEN=127.0.0.1:8080 /usr/local/bin/whipcode daemon start
/usr/local/bin/whipcode daemon status --json
/usr/local/bin/whipcode web --no-open
```

Check port availability immediately beforehand. Network configuration is read
when the daemon starts; a subsequent `daemon start` does not reconfigure an
already-running daemon. Preserve this setting consistently for later starts.
`WHIPCODE_NETWORK=1` alone uses an ephemeral port and does not establish the fixed
endpoint proposed here. The web command attaches to a running network-enabled
daemon; it does not restart or reconfigure it.

Local Tailscale Serve is empty today. Leave network exposure unchanged for this
cleanup. If access to this Mac from the phone is subsequently wanted, add a
private Tailscale HTTPS Serve route to this one loopback backend, with exact
host/origin allowlists. Do not create another daemon, use a wildcard bind or
enable Funnel. The GPU host's working HTTPS endpoint is separate and unchanged.

Install the rebuilt desktop and its canonical executable setting. Connect
This Mac to the same daemon. A direct URL desktop connection
would additionally require the exact `whip-app://bundle` origin allowlist; the
local Unix-socket connection does not need that network exception.

Do not add an always-on launch agent by default. If login startup is later
requested, add one user LaunchAgent with the canonical absolute path, home and
network settings. Supervise an actual foreground process, not the short-lived
`daemon start` command under KeepAlive. Establish stop/restart semantics before
turning on restart policies.

### 5. Prove the result and prevent recurrence

Acceptance checks:

1. Fresh login-shell command resolution finds one `whipcode`, at the canonical
   path, with the expected build/hash. `whip` no longer resolves. Check terminal
   command caches and any interactive aliases separately from startup files.
2. Exactly one daemon owns `~/.whipcode/runtime-v2`. Its reported build matches
   the installed client and its executable path is canonical. No old home,
   test daemon, listener or retained desktop backend is active.
3. Fresh session/config/trust state is visible. Test CLI and web against that
   same runtime: create a disposable session, send a message, answer a question,
   verify WebSocket updates/reconnects, and stop it. Remove the acceptance session
   through supported controls if an empty initial session list is desired.
4. The same session appears in desktop; desktop-first and
   CLI-first starts, GUI quit/reopen and daemon stop/start do not create a second
   home or change the chosen runtime. Test missing/incompatible executable errors.
5. Verify the embedded helper resolves outside the repository working directory.
   macOS may need fresh app/helper permissions after replacement; request only
   those needed for features actually exercised, using normal system prompts.
6. Run the appropriate existing tests for any desktop/runtime code change,
   distribution identity and packaged artifact. For cleanup alone, installation
   smoke checks are more relevant than adding tests that merely assert deleted files.
7. Rescan startup registration, PIDs, homes, PATH and ports after reopening clients.
   A logout/reboot check is useful final user validation, but never reboot this
   machine automatically during cleanup.

Record source revision, binary hash, installed path, state home, chosen desktop
mode and startup command in a small installation record. Record cleanup counts
and verification results without historical conversations or secrets.

For future development, use short disposable test homes and explicit teardown.
The current CLI/daemon TestMain implementations already attempt to delete their
test homes. The debris count alone does not prove their current cleanup is broken;
interrupted older runs and socket paths outside those homes are plausible causes.
Investigate recurrence with a bounded test run before changing cleanup code.
Never unlink an active owner-lock file as generic garbage collection: another
process could acquire a new lock inode and create competing owners.

Use one source-build install procedure. Build to staging, stop the old canonical
daemon, atomically replace its executable, then start and verify. Existing workers
can spawn using `os.Executable()`, so swapping the installed bytes while work is
running risks mixing builds. Keep temporary fixture cleanup in the runner's
`finally`/trap path. Do not globally alias normal commands to historical test homes.

## Remaining decisions and practical limits

- **Confirmed:** build from the freshly merged source; discard historical local
  Whip state; retain desktop and update its packaging and connection workflow
  to use the single canonical whipcode runtime. Complete and validate that
  integration before cleanup cutover. No desktop keep/remove decision remains.
- **At bootstrap:** choose the provider/model and authenticate or reference the
  intended credential. Do not infer that a deleted Whip login should be restored.
- **Not required for this reset:** mobile rebuild, remote daemon deployment,
  new Tailscale exposure, new login service, repository/worktree deletion,
  release publishing, or erasing unrelated developer tooling.

This plan removes local application state; it is not secure erasure of backups,
APFS snapshots or provider-side records. No such broader erasure was requested.
