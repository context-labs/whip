# Whipcode branch publishing

Branch: `whip-rlm`

Status: local implementation and validation complete; first public release verification pending.

Research date: 2026-09-07. Local HEAD and the remote `whip-rlm` head were
`dd7aaa3a7f9b8c00bd4ec095b978def1c9231805` when checked. There are unrelated
uncommitted runtime and frontend changes in the shared checkout. Releases will
contain committed, pushed source, not the working directory's uncommitted edits.

## Goal

Add a second distribution of the existing application, built from `whip-rlm`,
installed as `whipcode`, with its own configuration, local runtime state,
installer, release assets, and update channel. Keep one Go implementation and
copy the existing publishing/validation workflows as requested.

The current configuration location is **`~/.whip/config.json`**, not an XDG
directory. Its counterpart will be **`~/.whipcode/config.json`**. Preserve the
JSONC schema, defaults, atomic writes, backups, and clobber protection.

## Decisions and assumptions

Confirmed by the request:

- Publish the current branch, `whip-rlm`.
- Produce an installed executable named `whipcode`.
- Use a separate application configuration directory named for `whipcode`.
- Provide a new installer and copied/configured CI scripts.
- The follow-up authorizes end-to-end implementation, publication, and verification of the public curl installer.

The user approved executing this plan end to end, including these recommended defaults:

1. Publish automatically after successful validation of pushes to `whip-rlm`.
2. Separate all application-owned local state, including stored credentials,
   sessions, daemon ownership, browser profiles, and extracted helpers.

Other proposed implementation choices:

- Use GitHub Releases in the existing public `context-labs/whip` repository.
- Publish versioned prereleases with a `whipcode-` tag prefix.
- Keep the same four release targets and embedded web application.
- Keep Go module paths, npm package names, and protocol identifiers unchanged.
- Give CLI commands/help/update notices the correct executable name. A complete
  visual rebrand of the web app and browser extension is a separate decision.

## Research findings

Repository paths below are relative to the repository root. Line references
describe the source at research time.

| Area | Current behavior and evidence | Implication |
| --- | --- | --- |
| Release trigger | `.github/workflows/release.yml:4` runs on `v*` tag pushes | New tags must not match `v*`; the new workflow must explicitly use `whip-rlm` |
| Build | `release.yml:31` builds Linux x64/arm64 and macOS x64/arm64; `:103` injects `main.version` | Copy the matrix and introduce an explicit build identity |
| Bundled assets | `release.yml:70-97` builds/packs the web app and embeds the Swift helper on macOS arm64 | Copy these steps; renaming only the Go output is insufficient |
| Smoke checks | `release.yml:106-125` checks version and serves embedded HTML from an isolated daemon | New checks must use the new home variable and verify the new name |
| Publishing | `release.yml:133` uploads binaries, `SHA256SUMS`, and `install.sh` | New workflow needs distinct assets and a new installer asset |
| CI | `.github/workflows/ci.yml:12` runs for PRs and main pushes; lint, race/coverage, four cross-builds, Linux/macOS runtime, SDK/web, Swift jobs feed the `go` gate | Branch publishing should explicitly depend on copied validation, not assume an unrelated CI run passed |
| Security | `.github/workflows/security.yml:13` covers PRs/main plus a main-branch schedule | Copy the checks into a callable branch gate; do not accidentally retain a default-branch schedule |
| Config | `internal/config/config.go:303` uses `WHIP_HOME`, otherwise `~/.whip`; `:315` appends `config.json` | Centralize the distribution identity and resolve `WHIPCODE_HOME` independently |
| Runtime | `internal/daemon/socket_unix.go:36` derives runtime-v2, socket, and lock paths from the config home | A separate home isolates the daemon and database without another runtime implementation |
| Child processes | `internal/daemon/autostart_unix.go:21,52` and `internal/rlm/kernel.go:404` use the current executable | Build identity survives daemon launch/restart and kernel launch; no PATH-based `whip` launcher needs replacing |
| Path bypasses | `internal/skills/skills.go:46`, `internal/browser/browser.go:848`, `internal/browser/extrelay/embed.go:17`, `internal/computer/embed_darwin.go:27` construct `.whip` directly | Move application-owned paths through the selected home resolver |
| Environment filtering | `internal/capability/process.go:367` explicitly admits `WHIP_HOME` | Preserve the active distribution's home override in managed shell children |
| Installer | Root `install.sh` selects `whip-<os>-<arch>`, calls `/releases/latest`, verifies SHA-256, installs `whip`; `scripts/install.sh` is a deprecated forwarding shim | Copy the root installer; the deprecated shim is not a second implementation |
| Update command | `cmd/whip/update.go:19` fetches the installer from `main`; it restarts the daemon selected by `config.Dir()` | Route the new command to its installer and ensure it replaces the same installed executable |
| Update notices | `internal/update/update.go:35,134,203` uses `/releases/latest` and a small semver comparator | Both release selection and version comparison need explicit channel handling |

Live read-only GitHub checks confirmed that the repository is public, its default
branch is `main`, and the most recent listed release was `v0.5.14`, containing
four `whip-*` binaries, `SHA256SUMS`, and `install.sh`. No `whipcode` workflow
was registered. The roadmap and learning notes contain no existing branch
distribution design to reuse. `docs/features.md` and the frontend guide's build
contract confirm that the release includes the existing web application.

GitHub-specific constraints, verified against primary documentation:

- The latest-release endpoint excludes prereleases and drafts. Use a separate
  selector for `whipcode`; prereleases preserve the stable latest feed.
  [GitHub release API](https://docs.github.com/en/rest/releases/releases#get-the-latest-release).
- Release creation without an existing tag defaults to the default branch.
  Supply the exact tested commit with `--target`; mark the release
  `--prerelease --latest=false`.
  [GitHub CLI release creation](https://cli.github.com/manual/gh_release_create).
- Manual dispatch requires the workflow on the default branch. A workflow
  added only to `whip-rlm` can start from branch pushes; manual dispatch needs a
  separate small default-branch bootstrap change.
  [Workflow events](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#workflow_dispatch).
- Local reusable workflows referenced with `./.github/workflows/...` use the
  caller's commit. Permissions cannot increase down the call chain.
  [Reusable workflows](https://docs.github.com/en/actions/how-tos/reuse-automations/reuse-workflows).
- Concurrency does not guarantee execution order. Re-check the branch head
  before publishing and compare version counters when selecting updates.
  [Workflow concurrency](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-workflow-concurrency).

## Design

### One codebase, two build identities

Introduce a small leaf package, provisionally `internal/buildinfo`, with a
linker-set string `Name` defaulting to `whip`. Derive the home directory basename,
home environment variable, command label, and release-channel identity from the
two supported values. This is a two-distribution switch, not a plugin registry.
Retain `main.version`; no Go module rename or copy of `cmd/whip` is required.

Illustrative release build:

```sh
go build -trimpath \
  -ldflags "-s -w -X main.version=$RELEASE_TAG -X github.com/context-labs/whip/internal/buildinfo.Name=whipcode" \
  -o "dist/release/$ASSET" ./cmd/whip
```

The identity is compiled into the binary. Renaming an executable or invoking it
through a symlink must not silently switch its configuration/update channel.
`--version` should print `whipcode v0.0.N`; internally retain the complete release
tag for exact update/install matching. Record the full source SHA in release
notes. Preserve ordinary `whip --version` output.

Add explicit `task build:whipcode` and `task install:whipcode` targets. The latter
must build/install a file named `whipcode`; `go install ./cmd/whip` alone always
uses the package directory name. Both targets must include the existing web
asset prerequisite. Local development builds use `dev` unless supplied an actual
release version. Do not derive whipcode versions from unrelated `v*` tags.

### Configuration and state isolation

| Surface | whip | whipcode |
| --- | --- | --- |
| Executable | `whip` | `whipcode` |
| Default home | `~/.whip` | `~/.whipcode` |
| Config | `~/.whip/config.json` | `~/.whipcode/config.json` |
| Explicit home override | `WHIP_HOME` | `WHIPCODE_HOME` |
| Runtime | `~/.whip/runtime-v2/` | `~/.whipcode/runtime-v2/` |
| Stored auth / update notice | Under whip home | Under whipcode home |
| Skills / themes / browser files / helper extraction | Under whip home | Under whipcode home |
| Installer overrides | `WHIP_VERSION`, `WHIP_BIN_DIR` | `WHIPCODE_VERSION`, `WHIPCODE_BIN_DIR` |

`WHIPCODE_HOME` takes precedence over the default home. Do not fall back to
`WHIP_HOME`: it may already be exported for the other application. Do not
automatically copy credentials/configuration or migrate an existing database.
Each distribution runs its existing first-run setup independently.

Preserve generic external integration sources, including project `.agents`
skills, `~/.agents/skills`, explicit secret environment references, imported
Codex/Claude MCP configuration, and the external `~/.inf` credential fallback.
Those are shared inputs by existing design; application-owned saved state is
separate. Users may explicitly point overrides at the same directory, but that
does not provide side-by-side isolation and is not the default.

Route the four direct `.whip` path constructors through the selected application
home. Update their callers and tests where the current argument means user home
rather than application home. Preserve owner-only permissions and extraction
behavior. Keep the internal Swift executable name `whip-computer`; its containing
directory isolates it. No Swift source/package rename is required.

The Unix socket fallback hashes the absolute home already, so long-path homes
remain separate even if the temporary-directory prefix still says `whip`.

Give the four daemon networking controls parallel names:
`WHIPCODE_NETWORK`, `WHIPCODE_LISTEN`, `WHIPCODE_ALLOWED_ORIGINS`, and
`WHIPCODE_ALLOWED_HOSTS`. This prevents an exported stable listener address from
binding the new daemon to the same port. Preserve network-off defaults and
ephemeral loopback ports when enabled without an address. Other existing
integration/debug environment variable names can remain compatible in this
first change; do not mechanically rename protocol or third-party identifiers.

### Copied CI and publishing workflows

Create these three files from their existing counterparts:

1. `.github/workflows/ci-whipcode.yml`: copy `ci.yml`, use `workflow_call`, keep
   the existing quality checks, and add release-identity/isolation checks.
2. `.github/workflows/security-whipcode.yml`: copy `security.yml`, use
   `workflow_call`, preserve vulnerability/CodeQL checks, and give CodeQL a
   distinct analysis category. Remove the copied schedule.
3. `.github/workflows/release-whipcode.yml`: copy `release.yml`, change the
   trigger to pushes to `whip-rlm`, and call both copied validation workflows.
   Build/publish jobs must depend on their success in this same workflow run.

Preserve the existing stable workflows and required `go` status check. Loupe
chat/review and Dependabot are not publishing/build scripts and need no copies.
Use the repository's existing pinned actions and Go/Node versions; avoid
unrelated toolchain upgrades.

Important wiring details:

- Require `refs/heads/whip-rlm` and `context-labs/whip` for publication.
- Check out the triggering SHA in every build; local workflow calls use that
  same source version. Resolve version metadata once and pass it to every job.
- Keep default permissions read-only. Grant `contents: write` only to the
  publishing job. The security caller must explicitly allow
  `security-events: write`, with the called workflow narrowing it to CodeQL.
- Remove copied child concurrency blocks, or use distinct literal names.
  Reusing `github.workflow` in caller and called workflow groups can make them
  interfere. Use a separate `whipcode-publishing` group with
  `cancel-in-progress: false`; stale runs must check the live branch SHA and
  skip publication once superseded.
- Preserve Linux x64/arm64 and macOS x64/arm64 runners. macOS arm64 embeds the
  Swift helper; macOS x64 remains cross-built without it or a native smoke test.
- Preserve Node 24 web build/pack steps and unsetting `GOOS`/`GOARCH` for
  build-time generators. Build the web app from the same branch commit.
- Asset names: `whipcode-linux-x64`, `whipcode-linux-arm64`,
  `whipcode-darwin-x64`, `whipcode-darwin-arm64`.
- Name Actions artifacts `whipcode-bin-<platform>` and download only that
  pattern. The reusable CI workflow also uploads browser evidence; it must not
  be mixed into release artifacts. Require exactly four expected binaries.
- Generate a release-local `SHA256SUMS` for those four binaries and attach
  `install-whipcode.sh`. Verify nonempty assets and native binary identity.
- Start smoke daemons with an isolated `WHIPCODE_HOME`, use
  `WHIPCODE_NETWORK=1`, fetch HTML plus a referenced bundled asset, and stop
  only the fixture daemon in cleanup.
- Existing suites can keep testing the default identity. Add explicit compiled
  whipcode subprocess acceptance. Do not globally inject whipcode linker flags
  into all tests while their fixtures still set only `WHIP_HOME`.

### Version and release selection

Use unique tags `whipcode-v0.0.<github.run_number>` for this workflow, independent
of stable versions. The run number is monotonic for a fixed workflow and stable
across reruns. Keep the workflow identity fixed after launch; a future workflow
replacement requires carrying forward the numbering sequence or an explicit
version policy change.

Create a draft prerelease targeting the exact tested SHA, upload and verify all
assets, then publish it with `latest=false`. For reruns, verify that an existing
tag resolves to the intended SHA. Resume incomplete drafts; treat an already
published complete matching release as success. Do not clobber published
versioned assets. A failed build/upload must leave the previous published
release installable.

Both installer and update checker must select only published, non-draft
`whipcode-v0.0.N` prereleases with the required asset set. Page through GitHub's
release list and select the greatest numeric N; do not assume the first response
item, tag alphabetic order, or the first page is the new channel. Keep requests
bounded; a partial/failed lookup must not silently downgrade or select stable.
`WHIPCODE_VERSION` accepts an exact whipcode tag for reproducible installs and
explicit rollback, rejecting stable tags.

Extend the existing comparator narrowly: ordinary `v*` semantics stay intact;
whipcode counters compare numerically only against other whipcode tags. A stable
release is never a newer whipcode release. Preserve the best-effort startup
check, daily TTL, acknowledgement behavior, and distribution-local notice file.

### New installer and self-update

Copy the root installer to `install-whipcode.sh`. Use the new asset prefix,
destination filename, release selector, messages, and environment overrides.
Keep architecture detection, authenticated/anonymous downloads, SHA-256
verification, executable permissions, PATH setup, cleanup, and explicit errors.

Proposed bootstrap URL, available after the file is pushed:

```sh
curl -fsSL https://raw.githubusercontent.com/context-labs/whip/whip-rlm/install-whipcode.sh | sh
```

For release-list JSON, require Python 3 with a clear diagnostic. Keeping one
parser avoids two divergent implementations of paginated channel selection.
This is an installer prerequisite documented in the README; there is no new
Go/npm application dependency. GitHub CLI authentication remains optional.

Stage the verified executable in the destination directory, then rename it over
`whipcode`. This keeps replacement atomic when the download temp directory is on
a different filesystem. Never remove/replace a sibling `whip` executable.

`whipcode update` must use the new installer and force its destination to the
directory of the resolved running executable, rather than letting the installer
choose another writable directory. Preserve explicit version pinning. Download
the script successfully before executing it so a failed curl cannot be reported
as a successful update by a shell pipeline. Acknowledge update notices and
request restart only after successful installation. The existing restart path
uses the selected home and current executable, so it should restart whipcode
only; prove that with the two-daemon acceptance test.

Update actionable CLI/TUI text: version, usage, config location, setup, auth,
resume command, update notices, and relevant daemon/browser messages. Preserve
MCP provenance labels, wire method names, npm names, and generated identifiers.
Review the web app's hardcoded CLI instructions separately if they are changed;
read and follow `docs/frontend.md` for any app-layer edits.

## Ordered implementation work

- [x] Trace local release, config, installer, updater, and state paths.
- [x] Verify branch/repository metadata and GitHub workflow/release semantics.
- [x] Write this proposal and surface trigger/isolation questions.
- [x] Resolve the pending preferences before treating the design as accepted.
- [x] Add build identity and home resolution, preserving default whip behavior.
- [x] Fix direct application-home paths and managed-child home propagation.
- [x] Add whipcode CLI identity and explicit local build/install targets.
- [x] Implement whipcode release selection, comparison, and update routing.
- [x] Add and test `install-whipcode.sh` and atomic installation.
- [x] Copy and configure CI/security/release workflows and exact-SHA gates.
- [x] Update documentation and pass the local acceptance matrix below.
- [x] Review the implementation for correctness and unnecessary abstraction.
- [ ] When implementation is authorized, push the completed changes to
  `whip-rlm` and observe the first branch release through install verification.

Likely touched sources, beyond the new workflow/installer/buildinfo files:
`internal/config/config.go`, `cmd/whip/{main,update,daemon,daemon_manage,browser}.go`,
`internal/update/update.go`, `internal/capability/process.go`,
`internal/skills/skills.go`, `internal/browser/{browser,chromedp_backend,ext}.go`,
`internal/browser/extrelay/embed.go`, `internal/computer/embed_darwin.go`,
affected `internal/tui` command/setup/notice text, and their existing tests.
Use targeted searches to finish the actionable-text audit during implementation.

## Validation and acceptance

1. **Default behavior:** ordinary builds retain `whip`, `.whip`, `WHIP_HOME`,
   stable installer URL, and stable version comparisons. No stable workflow
   trigger or existing release asset is changed.
2. **Isolation:** build both distributions and run them under the same temporary
   user home. Set conflicting `WHIP_HOME`/`WHIPCODE_HOME` values deliberately.
   Assert config/auth/setup/trust/notice files and runtime database/socket/locks
   stay separate; renaming the binary does not change its identity.
3. **Path bypasses:** prove skills, browser profiles, relay files, helper
   extraction, and managed-shell child home overrides use the intended home.
   Check macOS helper extraction on macOS. Test long socket paths too.
4. **Daemon lifecycle:** start both daemons, compare runtime IDs/PIDs/paths,
   stop/restart/update whipcode, and prove whip remains responsive with its
   original state. Use only fixture homes and fake providers.
5. **Release selection:** fixture API tests cover mixed stable/whipcode releases,
   numeric 9 versus 10, drafts, missing assets, pagination, reordering, reruns,
   pinned rollback, malformed data, authentication failure, timeout, and no
   whipcode release. No fallback to stable and no automatic downgrade.
6. **Installer:** mocked network/platform tests cover four architectures,
   anonymous/authenticated requests, exact binary/checksum pairing, tampering,
   missing asset, unsupported platform, destination paths with spaces,
   existing sibling whip, missing JSON parser, and failed download cleanup.
   Verify a failed download/checksum never changes the installed executable.
7. **Updater:** verify installer URL/destination, download and child-process
   failure propagation, correct restart target, and no acknowledgement on
   installation failure. Preserve existing update notice tests.
8. **Workflow:** run an Actions-aware YAML validator and shell syntax/lint
   checks. Check needs/permissions/artifact patterns, branch guard, copied
   concurrency, exact target SHA, prerelease/latest flags, and draft recovery.
9. **Repository checks:** run `task check`, relevant Go race suites, `task lint`,
   `task vuln`, and release/runtime acceptance. Preserve the existing CI's
   portable exclusions and coverage threshold. Inspect generated assets for
   unintended changes; do not overwrite unrelated work to get a clean result.
10. **First published release:** confirm four binaries, installer and checksums,
    source tag SHA, prerelease status, and unchanged stable `/releases/latest`.
    Install into a temporary bin directory, inspect identity/config isolation,
    and smoke-test the embedded web app. Run the macOS Intel build on the supported `macos-15-intel` runner so
    all four release binaries receive native runtime/web smoke coverage.

## Documentation and rollout

- `README.md`: separate install, local build, config, and update instructions;
  explain branch builds and the JSON-parser prerequisite.
- `docs/features.md`: add a distribution/publishing section mapping behavior to
  code and named tests once implemented.
- `docs/roadmap.md`: add the second-distribution item, checked only when shipped.
- `docs/web-app.md`: document whipcode network variables and startup commands.
- `docs/frontend.md`: update build/launch guidance if that contract changes;
  the shared frontend package/state architecture remains the same.

Initial rollout needs no separate repository or new publishing secret: the
publish job can use its existing repository `GITHUB_TOKEN` with contents-write
permission. Verify applicable repository/organization rules during rollout.

If the user chooses manual-only publishing, first land a minimal disabled-unless-
dispatched workflow on `main`, then dispatch it with `--ref whip-rlm`, guarded to
that branch and targeting its event SHA. That default-branch bootstrap is an
additional change; merely adding `workflow_dispatch` to this branch is not a
complete manual-only delivery plan.

## Non-goals

- Forking the Go runtime, renaming the repository/module/npm packages, or
  introducing GoReleaser and another build system.
- Copying live user data, automatic database migrations, or shared daemon state.
- Adding Windows, Homebrew, npm distribution, new signing/notarization, or
  completing unrelated web accessibility/device release milestones.
- Rebranding every UI surface, changing third-party protocol identifiers, or
  broad CI modernization.

The main ongoing cost is keeping the copied workflows in sync. Add comments
pointing to their counterparts and review both paths when shared build steps
change. Avoid a general release framework until these two concrete workflows
demonstrate a need for one.


## Implementation evidence and deviations

- Implemented in an isolated worktree from `whip-rlm` to preserve concurrent edits
  in the original checkout. Publication will target `whip-rlm` explicitly.
- The installer uses one Python 3 JSON parser instead of implementing parallel
  Python and `gh --jq` selectors; this prerequisite is documented.
- Installer acceptance: 12 mocked-network tests passed on macOS, covering all
  four asset mappings, pagination, auth, rollback, missing assets/parser,
  tampering, and preservation of existing installations.
- Compiled acceptance passed: default/override homes, renamed executable,
  long-path socket separation, both daemons running, independent restart, and
  actual whipcode binary replacement followed by its daemon's self-restart.
- Focused Go race tests passed. Local vulnerability scan reports no reachable
  vulnerabilities (run with `GOTOOLCHAIN=go1.27.0`, matching CI's toolchain).
- Adversarial review found and fixed tagless-draft recovery and existing-tag
  target verification. Publisher tests exercise both cases and published reruns.
- The inherited branch had full-lint failures and 85.8% portable coverage,
  below its existing 90% gate. Minimal behavior-preserving lint fixes and
  regression tests for command validation, failed storage writes, replay,
  authentication, and daemon lifecycle repair these prerequisites. Wire formats
  and validation thresholds remain intact. A dedicated-browser E2E fixture now
  stops its own persistent browser before deleting its temporary profile.

- Runner verification: GitHub currently supports `macos-15` (arm64) and
  `macos-15-intel` (x64). The new workflows use these supported labels and smoke
  every release binary natively. The original stable workflows remain intact.
  Source: https://docs.github.com/en/actions/reference/runners/github-hosted-runners

- Final local `task check` passed (Go, generated contracts, SDK, web, packaging).
  Full golangci-lint reported zero issues; actionlint and shellcheck passed.
  Both compiled identities passed their path tests. The 12 installer and 11
  publisher cases passed, as did the two-daemon binary replacement acceptance.

- Final portable `go test -race -shuffle=on` completed successfully at **90.2%**
  total statement coverage, preserving the inherited 90% floor and exclusions.

- First hosted run passed lint, security, all cross-builds, both runtime and
  distribution platforms, and the Swift driver. Its portable race suite exposed
  a five-second fixture wait on the three-megabyte shell-output test. The fixture
  now allows instrumentation overhead and kills the job if its wait expires;
  all output, timeout, and process-lifecycle assertions remain in place.

- The same hosted run exposed stale browser selectors for the branch’s separate
  model/effort controls and renamed pause action. Browser acceptance now follows
  those controls, seeds a fixture catalog, and retains draft, host persistence,
  busy-state, and exact-turn cancellation assertions. The model picker now
  disables changes while a turn is active, matching the existing root policy.
  SDK fixture config/catalog files share its persistent temporary home.

- Repaired browser acceptance passed all Chromium and Firefox checks, including
  mobile cancellation and daemon restart recovery. Web types and 166 unit tests,
  28 race-enabled SDK acceptance tests, and the web performance suite passed.
  The shell-job suite passed five instrumented repetitions with GOMAXPROCS=2.

- Second hosted run passed all Go/security gates at 90.1% Linux coverage and
  Chromium browser acceptance. Firefox exposed concurrent test edits to one
  intentionally shared recipient draft. Concurrent recovery acceptance now uses
  distinct recipients and asserts that all sixteen authored messages persist
  exactly once, while the preceding shared-session permission test remains.

- Revised concurrent-send acceptance passed Chromium and Firefox, plus an
  additional complete Firefox run with GOMAXPROCS=2. No application behavior
  changed for this correction.

- Third hosted run passed Go/security and Chromium again. Firefox reached the
  next control before model replay/focus restoration settled. The browser test
  now waits for the rendered model and returned popup focus before clicking
  reasoning, preserving the existing timeout and assertions.

- The browser-render/focus synchronization passed the full Chromium and Firefox
  suites locally with GOMAXPROCS=2.
