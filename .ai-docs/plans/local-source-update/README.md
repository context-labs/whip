# Local source update

Branch: `codex/desktop-release`

## Goal

One repeatable command builds the current working tree and updates the installed
macOS desktop app and its shared whipcode daemon, retaining sessions and settings.
The user requested both the command and README instructions, and authorized using
it to upgrade this Mac.

## Design and prior art

Compose the existing desktop build/package/verify scripts and
`cmd/whip/desktop_runtime_sync.go`. The latter already provides verified staging,
compare-before-replace, maintenance locking, graceful shutdown and readiness
checks, including cross-schema updates. `TestDesktopCompiledUpdate` exercises the
real two-build handoff and state preservation. Do not duplicate daemon lifecycle
or add dependencies. This is source tooling, not another release updater.

`scripts/update-local.mjs` builds first, stages and verifies the signed app on the
destination filesystem, retains previous binaries, requests normal GUI quit,
replaces the app, runs the existing backend handoff and reopens after readiness.
Use the installed app's channel, version and signing team by default; local builds
receive unique build IDs and no release feed. Preserve the selected executable
and runtime home. A repository lock excludes simultaneous local-update builds.

Non-goals: pulling Git changes, publishing releases, managing remote hosts,
changing app preferences, deleting state, or automatically downgrading migrated
databases. Initially supports existing macOS Apple Silicon desktop installations.

## Work and validation

- [x] Confirm existing commands and local install paths/signing identity.
- [x] Add script and `task update:local` / `npm run update:local` aliases.
- [x] Test failed verification/quit/handoff and successful installation using
  disposable filesystem fixtures; reuse existing backend integration coverage.
- [x] Document defaults, restart semantics, overrides and recovery in README;
  link from desktop and features docs. No matching roadmap item exists.
- [x] Run `task check`, focused script tests and independent review.
- [x] Run the command on this Mac and verify app/backend hashes, build ID,
  schema migration, retained sessions and the live web endpoint.

Validation so far: full `task check` passed; all 11 disposable update tests
passed; `go test -tags=integration ./cmd/whip -run '^TestDesktopCompiledUpdate$'
-count=1 -v` passed with real old/new daemon binaries and retained session/config.
Independent review found a beta version/channel mismatch, now rejected before
building (also checked against the installed Beta app without changing it).
Failed app replacement now retains recovery files even when restoration fails.

Before activation: stable Whip 0.1.3, backend
`local-provider-connections-20260909`, schema 12, 20 retained sessions, canonical
`/usr/local/bin/whipcode`, local endpoint `http://127.0.0.1:8080`. A private
temporary baseline records session IDs and a configuration digest for comparison;
no credentials or transcript content is copied into this plan.

The first real run stopped safely during packaging, with the old daemon PID and
build intact: provider-logo assets had moved in the working tree but the notice
collector still read the old directory. Updated `apps/desktop/scripts/notices.mjs`
to include the relocated, renamed `provider-logos-NOTICE.txt` directly. No license
content or provider assets were changed. Retrying the same public command.

## Activation complete (2026-09-09)

`task update:local` completed successfully in approximately three minutes:

- Stable app version 0.1.3; backend `local-20260909185243320-d570a1610`.
- Installed `/Applications/Whip.app` and `/usr/local/bin/whipcode` have the exact
  verified signed payload hash
  `08eee0d3b743c1ea78ba8e53aa50df46587fd1c9da249c12334064be99357300`.
- New daemon PID 88166, generation 14, matching build, same
  `http://127.0.0.1:8080` endpoint and `~/.whipcode` database.
- Schema migrated 12 → 13; all 20 prior session IDs retained; configuration hash
  unchanged. Existing sessions initialize to Ask as specified by that migration.
- All 25 live HTTP assets match renderer manifest
  `8adcbc508d0c248227731b19c97bf293e894f9154ce6d64c6ca49162791a4a37`.
- Installed signature verified; relocated full provider-logo notice is included;
  local release feed disabled. This source build is signed, not notarized.
- Actual desktop window reopened with retained session navigation and displayed
  the authoritative Ask permission mode. Beta app was not replaced.
- Previous app/backend retained in `/Applications/.whip-local-update-ixwZgg`.
- Validation logs: `/tmp/whip-local-update-{check,script-tests,handoff,install}.log`.
