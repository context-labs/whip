# Disposable Docker environment for onboarding

Status: implemented and validated September 9, 2026. Run `task onboarding:docker`.
See [validation results](VALIDATION.md) and the
[user instructions](../../../README.md#test-fresh-onboarding-in-docker).

The launcher creates the container before attaching with `docker start`, rather
than using one `docker run` call. This keeps ownership explicit during interruption
and allows cleanup even when terminal attachment fails. Auto-removal, init, and
TTY behavior are the same. Container renderer metadata is marked `source.local`
and rejected by release verification even when the checkout was clean.

Container acceptance exposed a Linux worker issue: the process could already have
more virtual memory reserved at startup than the old fixed address-space ceiling.
`internal/rlm/memory_linux.go` now measures those initial reservations and adds the
existing growth allowance and configured budget. The parent-side resident RAM
limit remains unchanged. A subprocess regression test checks both small allocation
success and continued enforcement of a finite address-space limit.

## Intended workflow

Run this from the checkout you want to test:

```sh
task onboarding:docker
```

The command builds the current files, starts a fresh container, prints
`http://localhost:4000`, and opens Whip's normal first-run TUI in the current
terminal. The browser and TUI use the same daemon and configuration. Quitting
the TUI stops the container; running the command again starts from empty state.
Build caches survive between runs, but application state does not.

The default test project is a small disposable Git repository at `/workspace`.
The checkout supplies build inputs; the agent's test work happens inside the
container. This makes it easy to try onboarding and a first file/shell request.

## Findings before implementation

- There is no Dockerfile or Compose configuration in this checkout. The existing
  `update:local` command installs the macOS desktop application and shared runtime;
  this workflow should have its own entry point.
- [Taskfile.yaml](../../../Taskfile.yaml) already defines the production build:
  build the web renderer, package it for `go:embed`, then build `./cmd/whip`.
  [package.json](../../../package.json) requires Node 24; [go.mod](../../../go.mod)
  currently requires Go 1.27.0. Use those requirements as the version source.
- [whip web](../../../cmd/whip/web.go) attaches to a running network-enabled
  daemon. It does not launch a separate web server. Its readiness check validates
  `/api/v3/web`, including asset availability and protocol paths.
- [Daemon startup](../../../cmd/whip/daemon.go) already accepts explicit listen,
  Host, and Origin settings. The TUI attaches over the Unix socket belonging to
  its configured Whip home.
- [CI](../../../.github/workflows/ci.yml) includes Linux arm64 and amd64 builds
  with `CGO_ENABLED=0`, plus Linux runtime acceptance. Linux does not include the
  macOS computer helper; that boundary is explicit in
  [embed_stub.go](../../../internal/computer/embed_stub.go).
- [Inference.net](../../../internal/inferencenet/device.go) and
  [OpenAI subscription login](../../../internal/openaiauth/device.go) use device
  authorization with outbound polling. They need no local OAuth callback port.
  The [TUI browser opener](../../../internal/tui/openbrowser.go) already permits
  manual opening of its displayed URL on headless systems.
- [Renderer artifact creation](../../../scripts/renderer-artifact.mjs) currently
  runs Git commands unconditionally. Excluding `.git` from a Docker context would
  break `npm run build:web` unless this is handled deliberately.

Docker client and engine 29.4.0 responded successfully in the `orbstack` context
during research. Implementation was subsequently tested against that local engine.

## Build design

Use one multi-stage Dockerfile and the checkout directory as the local build
context. Include staged edits, unstaged edits, deletions, and new untracked source
files. There is no Git checkout, release download, or requirement to commit.
Docker supports local directory inputs and Dockerfile-specific ignore files.
[Docker build contexts](https://docs.docker.com/build/concepts/context/)

1. **Dependency stages.** Use official Node 24 and Go images matching the repo's
   requirements. Install JavaScript dependencies with `npm ci`, preserving npm
   workspace layout and considering all workspace manifests in the dependency
   cache. Skip Electron's binary download, as existing web/desktop CI does.
   Download Go modules from `go.mod` and `go.sum`.
2. **Web stage.** Build through the existing `npm run build:web` and
   `node scripts/pack-web.mjs` paths. Build output comes entirely from this
   container build, including renderer manifest and CSP verification.
3. **Go stage.** Copy the newly packaged web assets into the Go source and build
   `./cmd/whip` with `CGO_ENABLED=0` and the normal `whip` distribution identity.
   Default to the Docker engine's native architecture, avoiding emulation on
   Apple Silicon. Pass a recognizable local build version derived from source
   metadata; report the resulting image ID and renderer digest as well.
4. **Runtime stage.** Use a Debian slim base with CA certificates, Bash, Git,
   ripgrep, and normal core utilities. Copy only the built binary and the small
   test workspace. Run as an unprivileged user with a writable, initially empty
   home. Compilers, `node_modules`, source checkout, and build-time state stay in
   the build stages.

Use BuildKit cache mounts for npm downloads, Go modules, and Go compilation.
Source changes still invalidate the affected build steps. A repeated command
always evaluates the local build; it may reuse identical cached outputs.
[Docker cache guidance](https://docs.docker.com/build/cache/optimize/)

Add `scripts/docker/onboarding.Dockerfile.dockerignore` beside the Dockerfile.
Exclude `.git`, dependency directories, generated build output, native desktop
staging, binaries, local environment files, and unrelated local artifacts.
Keep build-required source and fixtures, including untracked additions. Do not
derive this list from tracked Git files or blindly reuse `.gitignore`.

For Git worktree support, add a narrow, explicit local-build metadata input to
`scripts/renderer-artifact.mjs`. The wrapper resolves the actual commit and dirty
status on the host, then supplies those values to the builder. Validate the input
and keep existing Git-based behavior as the default. Release consumers must still
perform their current checkout/dirty-state checks, and missing Git metadata must
not silently authorize a release. This avoids copying Git internals or following
a worktree's host-specific `.git` pointer inside the image.

## Runtime and isolation

Use `docker create --rm --init -it`, then `docker start --attach --interactive`, and publish only
`127.0.0.1:4000:4000`. Docker's `--init` handles orphaned child reaping; the TTY
supports normal Bubble Tea input and terminal resize. Loopback publishing keeps
the host endpoint local.
[Docker run](https://docs.docker.com/reference/cli/docker/container/run/),
[port publishing](https://docs.docker.com/engine/network/port-publishing/)

Set these values inside the container:

```text
WHIP_HOME=/home/whip/.whip
WHIP_NETWORK=1
WHIP_LISTEN=0.0.0.0:4000
WHIP_ALLOWED_HOSTS=localhost:4000,127.0.0.1:4000
WHIP_ALLOWED_ORIGINS=http://localhost:4000,http://127.0.0.1:4000
```

The container must listen beyond its own loopback interface for Docker's published
port to reach it. Both browser origins are explicitly allowed by Whip's existing
[network contract](../../../docs/protocol-v2.md#local-and-trusted-network-setup).

The entrypoint performs this sequence:

1. Start the daemon with `whip daemon start` from `/workspace`.
2. Validate readiness with a bounded call to
   `whip web --no-open --url http://127.0.0.1:4000`. Show daemon logs on failure.
3. Print the host URL and container name, then run ordinary `whip` in the terminal.
   Supply no provider, model, permission, resume, or initial-prompt overrides.
4. Stop the daemon on normal exit or termination, preserve the meaningful exit
   status, and let Docker remove the container. Handle terminal interrupts and
   partial startup as well as successful shutdown.

Create no persistent application volume. Pass terminal presentation variables
such as `TERM` and `COLORTERM` explicitly where appropriate. Do not forward host
provider keys, home/config directories, SSH agents, browser profiles, or the
Docker socket. Configuration and credentials entered during a run remain in that
container until it is removed. Cached image layers contain no runtime login state.

The wrapper should fail clearly if Docker is unavailable, the selected engine
is remote, the terminal is noninteractive, or port 4000 is occupied. It should
never stop another service to claim that port. Use a unique container name and
clean up only the container created by this invocation. Build failures must stop
before launch, rather than falling back to an older image.

## Testing the two onboarding surfaces

A fresh container provides a clean TUI and backend. The host browser's
`localStorage` and `sessionStorage` survive container replacement, as used by the
[browser platform adapter](../../../apps/web/src/platform/browser.ts).
For a fully fresh browser test, open `http://localhost:4000` in a new private
session, closing the previous private session between resets, or use a disposable
browser profile. Document this next to the launch command.

Provider setup is shared: configuring a provider from one client makes it
available to the other. Use a new container when comparing the initial TUI flow
with the initial web flow independently. Use the same container when checking
that setup propagates between clients.

Inference.net remains recommended through the application's existing ordering;
the harness does not preselect a provider or populate credentials. Device login
can be completed in the host browser using the displayed URL/code. The container
will not automatically open a Mac browser or provide native clipboard integration.

This exercises the shared web UI and Linux TUI/runtime. Native macOS onboarding,
Keychain/clipboard behavior, computer use, and desktop installation still require
the existing native validation. Hot reload, persistent/reusable profiles, host
workspace mounts, automatic credential injection, and Compose are outside the
initial implementation.

## Implementation sequence and files

1. Add the explicit local renderer metadata input and focused regression coverage
   in `scripts/renderer-artifact.mjs` and its existing test file.
2. Add `scripts/docker/onboarding.Dockerfile`, its Dockerfile-specific ignore file,
   and `scripts/docker/onboarding-entrypoint.sh`. Create the tiny example workspace
   during the image build rather than adding a fixture framework.
3. Add `scripts/onboarding-docker.mjs`, using Node's standard library for argument
   handling, preflight checks, build/run invocation, and owned-container cleanup.
   Expose it as `task onboarding:docker` in `Taskfile.yaml`. Direct invocation with
   Node should work without installing the repository's npm dependencies on the
   host. Host prerequisites are Docker, Git, Node 24, and optionally Task.
4. Document the command in `README.md`: first versus cached build, fresh-state
   semantics, shared provider setup, private browser testing, device login,
   disposable workspace, quitting, and inspecting daemon logs with `docker exec`.
5. Verify the actual container and record measured cold/warm build times, rather
   than promising an unmeasured startup duration.

## Acceptance criteria

- Start from an empty container home; show first-run TUI onboarding and web welcome
  without discovering the host's configured providers or sessions.
- Serve the embedded production UI at `http://localhost:4000`; connect its real
  WebSocket to the same runtime the TUI reports. Test both allowed browser origins.
- Demonstrate that a tracked edit and an untracked source addition affect the
  build. Run from a linked Git worktree too, with no host build artifacts needed.
- Repeat unchanged and changed builds to verify caching without stale binaries or
  web assets. Confirm an intentional build failure cannot launch a prior image.
- Configure a provider, select a model, and complete a first request using the
  existing deterministic test-provider approach. Check file/shell execution in
  `/workspace` and visibility from both clients. Leave real account login and
  billable inference as an explicit manual smoke test.
- Confirm terminal sizing, resize, paste, normal quit, interruption, and
  `docker stop` behavior. After exit, the container is removed and port 4000 can
  be reused; a new run has no previous credentials or sessions.
- Check missing Docker, occupied port, startup failure, and cleanup paths with
  focused script tests plus an actual Docker smoke run. Verify renderer release
  provenance regressions still pass after the local metadata addition.
- Confirm the existing installed daemon, desktop app, home directories, and
  source checkout remain unaffected by the test run.

Implementation and validation results are recorded in [VALIDATION.md](VALIDATION.md).
