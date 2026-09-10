# Session filesystem access validation

September 9, 2026. All six phases are complete. Implementation, automated checks,
isolated TUI/desktop acceptance, and the local update passed.

## Implemented contract

| Area | Verified behavior |
| --- | --- |
| Root Full Access | List/read/write/edit sibling files, resolve relative paths from cwd, and navigate outside the original project. Saved mode survives reopen and daemon restart. |
| Ask | Retains the original project boundary even after navigation. Outside operations are denied; absolute in-project targets and navigation back remain usable. |
| Delegation | New default children and grandchildren inherit effective issuer scope. Explicit path and operation ceilings, issuer generation, expiry, and revocation remain enforced. |
| Migration | Schema 14 tags only identifiable bootstrap root grants. Saved modes, IDs, history, budgets, expiry/revocation, and event cursors are preserved. Ambiguous legacy children remain explicit. Rollback/retry and concurrent opener fixtures pass. |
| Pending work | Actual mode changes cancel pending approvals in the same transaction. Switching back cannot revive an old approval. Missing root authority is not recreated on reopen. |
| Paths and locks | Canonical paths are separate from authorization. Symlink aliases share daemon-wide locks; unresolved symlinks fail closed. A target retargeted while waiting for a mutation lock is rejected after acquisition. |
| Context | Project instruction/skill discovery remains bounded and follows current agent authority. Explicit project skill invocations recheck cached paths; configured global user context remains available. |
| Clients | Shared web/desktop picker, capability inspection, TUI/CLI and ACP explain Full Access consistently. Existing session-owned persistence and client synchronization remain in use. |

The central implementation is
[`internal/session/filesystem.go`](../../../internal/session/filesystem.go),
used by the existing capability ledger and delegation paths. Path resolution
and locking remain in
[`internal/capability/workspace.go`](../../../internal/capability/workspace.go).

## Automated verification

| Check | Result |
| --- | --- |
| `task check` | Passed: Go formatting, vet, whipvet, all Go packages, protocol checks (11 tests), SDK checks (276 tests), shared app checks (420 tests), UI/package tests, local updater tests, and Docker/build artifact tests. |
| `npm run generate -w @whip/protocol` | Regenerated additive file-scope and issuer metadata; generated contract validation passed. |
| Focused capability/session/daemon race checks | Passed, including filesystem policy, delegation, migrations, restart, and input handling. |
| `go test -race ./internal/daemon ./internal/rlm ./internal/skills -run '^Test(Prompt\|PrepareAuthoredInput\|ComposePrompt\|LoadPromptCatalog)' -count=1 -timeout 90s` | Passed after the final context integration. |
| `go test -race ./internal/capability -count=1` | Passed after the final queued-lock correction, including its permanent deterministic regression. |
| `go vet ./internal/capability` | Passed after that correction. |
| `go test ./internal/capability ./internal/session ./internal/daemon ./internal/rlm ./internal/skills` | Passed on the final runtime sources. |
| `git diff --check` | Passed. |

The queued-lock correction landed after the complete `task check` pass. The
affected Go packages, capability race suite, and vet were rerun; both actual
client acceptance fixtures were then built from the corrected runtime sources.
An independent regression first reproduced the lock-wait problem and passed
after the correction.

Local logs: `/tmp/whip-access-final-check.log`,
`/tmp/whip-access-final-go.log`, `/tmp/whip-access-race.log`,
`/tmp/whip-access-lock-race.log`, and `/tmp/whip-access-lock-vet.log`.
These temporary logs supplement the repository fixtures and recorded results.

## Actual TUI acceptance

Built `./cmd/whip` and ran it in a real PTY against an isolated loopback provider.
A fresh `--yolo` session executed Starlark sibling list/read/write operations.
After stopping its daemon, resuming the same session without `--yolo` repeated
those operations successfully. The terminal rendered Full Access and both final
responses. Database inspection verified schema 14, saved `automatic`, and the
same session/cwd. All fixture processes stopped.

Tested binary SHA-256:
`2d270e6463e7674a1c823cbafa1936009077b7218f919286fde4c2edcf0a024c`.

[Procedure and observations](evidence/tui.md),
[machine-readable results](evidence/tui-result.json),
[reproducible PTY harness](evidence/tui_acceptance.py).

## Shared renderer in desktop

Ran `npm run build:desktop`, then
`node .ai-docs/plans/session-filesystem-access/evidence/desktop-smoke.mjs`.
The actual renderer submitted a prompt whose fixture response called `rlm_exec`
to list a sibling directory and write a file there. The fixture checked the
written bytes. Reload and daemon restart preserved the session and Full Access.
SDK commands against the resumed daemon read and wrote that same sibling file.
Selecting Ask in the actual UI then rejected a read with the specific
“outside this agent's allowed filesystem scope” error. No renderer errors occurred.

Renderer digest:
`c7f3c6d5d680f56ca9a85e0a9e6f4ba573aef142e4014329cd486e2d5fd8d955`.
Staged runtime digest:
`c25d501d90462d2dbeae839a5685db7fe41ed4eca7269a01495093b8894ad4a5`.

[Machine-readable desktop results](evidence/desktop.json),
[fixture wrapper](evidence/desktop-smoke.mjs),
[Ask selected after restart](evidence/desktop-ask-denies-sibling.png).
The screenshot was visually inspected. The wrapper reuses the existing isolated
Electron harness and fails if its expected insertion points change.

## Installed local build

`task update:local` completed successfully, rebuilt and signed the current
working tree, installed the shared backend and desktop app, restarted the
daemon, and reopened Desktop. The installed app process was observed.

- Build: `local-20260909224404584-7058e007f` (Whip 0.1.3).
- App: `/Applications/Whip.app`; backend: `/usr/local/bin/whipcode`.
- Daemon status: running, generation 16, matching daemon/client build.
- Installed runtime and live database both report schema 14. The live database
  identity was checked read-only as `whip-recursive-runtime-v14`.
- Installed backend bytes match both the app's bundled backend and the verified
  package evidence. The renderer digest matches the desktop acceptance build.
- `/usr/bin/codesign --verify --deep --strict` with the Apple anchor and team
  `JAPWPV5JY2` requirement passed for the installed app.
- Previous binaries remain at `/Applications/.whip-local-update-1Maob7`.
  The database upgrade is one-way; do not restore an older backend over it.

[Installation receipt](evidence/local-update.json) records exact build, process,
schema, artifact, and hash information. The full update log is
`/tmp/whip-access-update-local.log`. The existing Vite chunk-size warning was
nonfatal; packaging and signature verification succeeded.

## Compatibility and validation limits

- Legacy child records do not prove whether their paths were inherited or
  explicitly restricted. Their recorded limits are retained; newly created
  default children inherit the corrected session policy.
- Full Access operates as the execution host's OS user. Ask preserves its
  existing shell behavior; it is not an OS filesystem sandbox. Existing
  background processes are not retroactively sandboxed or terminated by a mode
  change. This work adds no new remote-host access or OS privilege.
- Client acceptance used disposable homes/databases and local synthetic
  providers. It made no paid model requests. The desktop fixture exercised the
  shared renderer in staged Electron; installed-package verification is separate.
- No previously failed user operation is automatically replayed. The user can
  retry the original sibling-repository request with the saved Full Access mode.
