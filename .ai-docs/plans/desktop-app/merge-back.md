# Desktop integration into the main checkout

Prepared 2026-09-08. This is a plan, not an executed merge. The target is the
existing `whip-rlm` branch in `/Users/samheutmaker/Desktop/context-labs/src/rlm/whip`.
There is no proposal to rename it to `main`.

## Verified starting point

- Main checkout: clean at `f548ca59dcecb869934165b1f4e332ffc0f639a9` (`remote clients`).
- Desktop worktree: `/Users/samheutmaker/Desktop/context-labs/src/rlm/whip-desktop-app`,
  branch `desktop-app`, based on `dd7aaa3a7f9b8c00bd4ec095b978def1c9231805`.
- Main has seven subsequent commits, changing 293 paths. Desktop implementation
  remains uncommitted: 51 tracked modifications and 119 new source/documentation
  files, excluding this newly added plan.
- An isolated Git merge preview of that complete desktop snapshot found 31
  overlapping paths and 15 conflicted paths. It used a temporary index and object
  store; neither checkout, branch, nor real index was modified.
- A clean textual merge does not establish compatibility. Main's multi-host
  runtime and desktop's single-selected-connection runtime have different state
  and lifecycle contracts, including in files Git merges automatically.

## 1. Preserve and checkpoint the complete desktop work

1. Recheck both HEADs and working trees immediately before execution. Inventory
   any work added since this preview, including untracked files.
2. Create a backup ref for main's current commit and a recovery copy of desktop's
   tracked diff plus new files. Preserve ignored local data separately.
3. Review the desktop source, tests, workflows, docs and evidence, then commit
   them on `desktop-app`. Include the model-picker fix, daemon-origin support,
   MCP shutdown fix, packaging and signing tooling. Review evidence for accidental
   credentials or private runtime content before staging it.
4. Leave `node_modules`, generated renderer output, native build products, `.stage`,
   signed `.app`/DMG/ZIP files, `.dev` session data and credentials out of Git.
   Retain those existing files on disk; they are not cleanup targets.

Exit: the desktop implementation is recoverable as a commit, main is untouched,
and the source inventory accounts for all requested work.

## 2. Merge in an isolated integration worktree

Create a temporary worktree on `codex/desktop-integration` from main's current
commit, choosing an unused branch name if necessary. Merge the desktop checkpoint
there with a merge commit, resolving the conflicts below. Do not replace the
main folder with a copy of the older desktop tree, squash away main's intervening
work, or blanket-resolve files with one side.

The observed conflicts are:

| Area | Conflicted paths | Resolution |
| --- | --- | --- |
| Connection ownership and UI | `packages/app/src/runtime.ts`, `attention.tsx`, `shell.tsx`, `session-sidebar.tsx`, `session-tab-strip.tsx` | Preserve main's independent host connections and mixed-host workspace. Add desktop effects and transports to that architecture. |
| Model picker | `packages/app/src/model-selection.tsx` | Keep the new collision-aware detail card and main's disconnected/busy model-selection guards. |
| MCP lifecycle | `internal/mcp/manager.go` | Retain manager-owned connection context, transport cancellation/cleanup and main's context/lint fixes. |
| Test infrastructure | `apps/web/scripts/browser.mjs`, `apps/web/scripts/performance.mjs`, `cmd/whip/mcp_import_test.go`, `internal/browser/e2e_test.go`, `packages/app/test/sidebar-creation.test.tsx`, `packages/sdk/scripts/fixture.mjs` | Keep current behavior/fixtures and distribution support, plus desktop cleanup/isolation fixes. Preserve existing performance tooling without running more performance experiments. |
| Documentation | `docs/frontend.md`, `.ai-docs/plans/desktop-app/README.md` | Describe the combined implementation in the canonical frontend guide; preserve useful historical research and clearly dated evidence. |

Audit the other 16 overlapping paths and newly introduced consumers as well.
In particular, settings, welcome/new session, connection notices, route handling,
bootstrap and desktop tests can retain obsolete `state.client`, `state.connection`
or tab-method assumptions despite a textual merge succeeding.

## 3. Integrate the two connection models deliberately

Use main's `HostConnections` as the single owner of live SDK clients and session
lists. Extend its per-host connection setup to use the platform's asynchronous
connection resolver for URL, managed local and SSH targets. Each record must own
its setup cancellation, progress, resolved transport and disposal independently.
Do not restore desktop's global detach-and-clear behavior when selecting a host.

Recommended ownership decisions:

- Keep daemon-owned, revision-checked URL profiles in `remote_hosts` for the web
  workflow. Keep machine-specific SSH profiles and identity-file paths on the
  desktop device. These profile sources feed one live connection manager; do not
  maintain duplicate authoritative copies of the same saved URL profile.
- Preserve desktop's saved profiles and selection. Adapt its existing migration
  to offer older URL profiles for verified import and restore SSH profiles locally.
  Keep original records until successful migration; reject duplicate runtime
  aliases and require explicit acceptance of replacement daemon identities.
- Keep the managed local daemon as desktop's local host. Selecting a URL/SSH
  host changes the focused work, while other attached hosts remain available.
- Preserve main's v3 mixed-host tab layout and v1/v2 recovery. Desktop's window
  storage adapter must persist that same layout across relaunch. Keep runtime,
  root, recipient and view identity on drafts, navigation and pending commands.
- Adapt native notifications and deep links to their source host. Scope attention
  observation and cleanup per runtime and retain the existing bounded behavior.
  Cmd-W closes the focused tab, and window close retains accepted daemon work.
- Native folder selection applies to This Mac. URL and SSH hosts use the remote
  daemon's directory browser. Late picker replies must match the original host.
- Keep Node, Electron, filesystem and SSH process access behind the desktop host
  and versioned bridge. Shared React components still consume `@whip/ui` and SDK
  services, with a single browser/desktop route tree.

This phase is implementation work required by the merge, not just choosing text
between conflict markers. Update relevant runtime/host tests around these contracts.

## 4. Reconcile builds, distribution variants and packaging

1. Retain main's `whip`/`whipcode` distribution support, environment prefixes,
   release/install tooling and current Go schema/protocol implementation. Desktop
   adds its CLI subcommands and schema-reporting hook to that current executable.
   The desktop-origin allowlist fix is already in main; retain it once with its
   regression coverage.
2. Merge manifests and scripts, then validate the lockfile using Node 24 and
   `npm ci`. Regenerate the lockfile only if the combined manifests require it;
   do not opportunistically upgrade unrelated dependencies.
3. Preserve the one-renderer packaging pipeline. Build Vite once from the combined
   source, generate the renderer manifest, and feed those exact bytes to both Go
   web assets and Electron ASAR. Run renderer provenance and byte-equality checks.
4. Rebuild the Swift helper and Go daemon from the combined source, sign them in
   the established order, and preserve the Go build-overlay mechanism. Derive
   runtime compatibility metadata from the newly built binary.
5. Keep current CLI CI/release lanes and add the desktop/macOS lanes without
   dropping main's newer acceptance checks. Review release triggers, permissions,
   artifact names and distribution variants even where YAML merges cleanly.
6. Produce a new signed/notarized local desktop package after integration checks
   pass. Verify ASAR integrity, native signatures, notarization/stapling and final
   artifact hashes. Old package evidence remains historical and cannot certify
   these new bytes. Do not overwrite a published version with different contents.

Local signing/notarization has previously succeeded. Automated publication still
has unresolved Whip upload credentials, a dedicated feed/route and the mismatch
between the planned AWS OIDC publisher and HALO's R2 setup. Merge the tooling and
document those gates accurately; publishing or issuing a release tag is outside
this local merge. A signed local package can be validated without an active feed.

## 5. Validate the combined result

Run the repository's `task check` and affected acceptance/race checks, plus
`npm run test:desktop`, renderer/package checks, and browser scenarios covering
the changed contracts. Use isolated homes and fake providers, not the active
daemon's database or the user's live SSH configuration.

Required outcomes:

- Existing web multi-host tests, host-scoped search/attention, mixed-host panes,
  tab restoration, draft preservation, directory selection and disconnect
  isolation continue to pass.
- Desktop can connect to its managed daemon, an existing localhost daemon such
  as the port-43110 configuration, and an isolated SSH fixture. Identity changes,
  aborted setup and one host failing do not corrupt other hosts' state.
- Notifications and session/deep links choose the correct host; Cmd-W and GUI
  close/relaunch preserve the established tab and daemon lifecycle behavior.
- Model-info panels remain bounded on narrow/short screens and during scrolling
  and resizing; main's busy-model guards remain enforced.
- MCP HTTP cancellation, daemon stop and retained worker continuity regressions
  pass against the combined Go source.
- Browser and Electron renderer bytes match; packaged native binaries and
  metadata belong to the combined source; strict CSP and production fuses remain
  enabled. Smoke-test the resulting package with isolated runtime data.

Do not restart the user's daemon merely to validate the merge. Do not run new
startup/idle/performance benchmark rounds. Preserve existing measurements and
state the exact functional checks performed on the new artifact.

Exit: conflicts and obsolete API assumptions are resolved; the combined source
and package have fresh functional evidence. Hardware-only and hosted-update
release gates remain explicitly recorded where they cannot be exercised locally.

## 6. Move the verified result into the main folder

1. Commit the integration and validation adjustments in the integration branch.
2. Recheck that main is clean and still at the recorded target commit. If it has
   advanced, incorporate that new work in integration and rerun affected checks.
3. In `/Users/samheutmaker/Desktop/context-labs/src/rlm/whip`, fast-forward
   `whip-rlm` to the verified integration commit with `git merge --ff-only`.
   This brings the merge history and all source changes into the requested folder.
4. Synchronize main's dependencies and regenerate ignored build artifacts there
   where needed. Confirm its HEAD matches the validated result and its tracked
   tree is clean. Install/relaunch a newly packaged app separately when desired;
   moving source does not update the currently installed `.app`.
5. Retain the desktop branch, old worktree, runtime data and backup ref until the
   integration is accepted. Worktree deletion and any data migration are separate
   cleanup steps; never force-remove the worktree containing `.dev` sessions.

Completion means main contains both its newer work and the full desktop feature,
the combined package has been validated, and the rollback checkpoints remain
available. No remote push, tag, publication, daemon restart or worktree deletion
is implied by preparing this plan.
