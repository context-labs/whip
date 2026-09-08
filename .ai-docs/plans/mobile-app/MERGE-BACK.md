# Merge the mobile work into the main Whip checkout

Prepared and executed 2026-09-08. The approved plan below records the starting
point and procedure. See [merge evidence](MERGE-EVIDENCE.md) for the checkpoint,
resolutions, validation and promotion record.

Destination: `/Users/samheutmaker/Desktop/context-labs/src/rlm/whip`, currently on
`whip-rlm`. “Main folder” means this checkout; it does not mean switching to a
branch named `main`.

Source: `/Users/samheutmaker/Desktop/context-labs/src/rlm/whip-mobile-app`, on
`mobile-app`.

## Observed starting point

- Destination is clean at `879ad8782b63a40a31bffdff78c15c542583a1e1`.
- Source remains at base `dd7aaa3a7f9b8c00bd4ec095b978def1c9231805`; the mobile
  implementation, fixes and documentation are uncommitted.
- Destination has 10 commits beyond that base, including the public `whipcode`
  distribution, batched questions, remote clients, and the desktop integration
  with independent multiple-host connections and shared renderer packaging.
- Source has 56 modified tracked files and 372 untracked files before this plan.
  Of the latter, 281 files (about 1.5 MiB) are generated Android module build
  output under `apps/mobile/modules/whip-storage/android/build/`. They are excluded
  by EAS upload rules but not by the source Git ignore rules yet.
- A read-only, per-file three-way preview found 16 text conflicts, 15 clean text
  merges, 15 changes confined to mobile, 88 new files, two overlapping newly added
  plan documents, and 11 byte-identical files. No modify/delete conflicts found.
  Counts exclude the generated Android output and precede this new plan file.
- The identical files include several copied question/protocol changes already
  committed in the destination. The initial 24-file copied baseline is recorded
  in `worktree-baseline.json`; use it to distinguish inherited work from mobile
  additions. Preserve the resulting behavior once rather than reimplementing it.

The preview used temporary file copies with `git merge-file`; it did not create a
merge commit or modify either real index. It predicts textual conflicts, not a
complete Git rename-aware merge or semantic compatibility. Recompute against the
actual execution-time snapshots. Audit detail is in ignored
`artifacts/merge-back-audit.json`.

## Phase 1 — Make the complete mobile work recoverable

1. Recheck both HEADs, indexes, tracked modifications and untracked files. Record
   their identities. If other work arrived, include it in the inventory and keep
   it separate from the integration rather than overwriting it.
2. Create a named backup ref for the destination HEAD. Preserve the mobile tracked
   patch and a manifest/archive of authored new files before staging. Leave local
   build products and existing phone/simulator evidence in place on disk.
3. Fix the source ignore rules for nested native-module `android/build/`, plus
   any additional generated paths found in the inventory. This prevents a broad
   staging operation from committing hundreds of build products.
4. Review an explicit staging inventory. Include the Expo app, native storage
   module source, assets/licenses, shared presentation/theme exports, SDK and Go
   fixes, tests, workflows, setup/release docs, plans, and evidence narratives.
   Review generated assistant scaffolding such as `.claude/settings.json` before
   deciding whether it is intentional project configuration.
5. Exclude dependencies, generated native projects, compiled binaries, IPA/APK/app
   packages, build caches, credential files and raw device/cloud-build artifacts.
   These remain available locally; this is not a deletion/cleanup operation.
6. Commit the reviewed source snapshot on `mobile-app`, honoring repository hooks.
   Do not push, tag, or publish it as part of this step.

Exit: all authored mobile work is accounted for and recoverable from Git; the
main folder remains unchanged.

## Phase 2 — Integrate away from the main folder

Create `/Users/samheutmaker/Desktop/context-labs/src/rlm/whip-mobile-integration`
on an unused `codex/mobile-integration` branch from the destination's current HEAD.
If the name/path is already occupied, choose an unused integration name rather
than disturbing another task's worktree.

Merge the mobile checkpoint into that branch using a normal three-way merge:

```sh
git merge --no-ff --no-commit mobile-app
```

Resolve and validate there before finishing the merge commit. A directory copy,
blanket “ours/theirs” choice, or resetting the destination to the old mobile base
would lose newer desktop, publishing or multiple-host behavior.

The observed conflict groups are:

| Area | Paths | Resolution |
| --- | --- | --- |
| Workspace manifests | `package.json`, `packages/app/package.json`, `package-lock.json` | Retain desktop scripts and `desktop-bridge` export, add mobile scripts and pure `presentation` export, then reconcile the lockfile from the combined manifests. Preserve the renderer-manifest step in `build:web`. |
| SDK services | `packages/sdk/src/services.ts` | Keep desktop's host directory picker and mobile's permission decision validation/status and injected identity generation. |
| Question handling | `internal/tui/question.go`, `packages/app/src/requests.tsx`, `packages/app/test/requests.test.tsx` | Preserve current batched-question behavior, multiple-host scope and mobile's answer/recovery fixes; keep regression tests for both. |
| Integration fixtures | `internal/daemon/v2_sdk_test.go`, `packages/sdk/scripts/fixture.mjs` | Combine current distribution/runtime naming and remote/desktop acceptance with mobile fixture lifetime, networking, questions and cleanup. |
| Generated protocol | `packages/protocol/generated/request-validators.js`, `response-validators.js` | Regenerate after resolving the Go registry/schema source. Do not hand-pick generated fragments. Include all other regenerated outputs in the drift check. |
| CI | `.github/workflows/ci.yml` | Preserve current desktop, renderer and distribution checks. Add the reusable mobile workflow to the required `go` aggregate gate, including its result assertion. |
| Canonical docs | `docs/concurrency.md`, `features.md`, `frontend.md`, `roadmap.md` | Describe the combined implementation; preserve desktop/multiple-host architecture while documenting the native companion separately. |
| Historical plans | `.ai-docs/plans/mobile-app/README.md`, `IMPLEMENTATION.md` | Reconcile the two existing versions, retain useful decisions and date the latest evidence/status. They do not override canonical frontend documentation. |

## Phase 3 — Review behavior where Git merges cleanly

A clean textual merge is not enough for the shared client libraries.

1. Preserve the web/desktop `HostConnections` model: independent clients and
   session lists per host, mixed-host tabs, scoped drafts and late-result guards.
   The native app retains its deliberately smaller single-active-host runtime
   and encrypted device-local saved hosts. This merge does not add mobile
   multiple-host concurrency or transplant its detach behavior into the web app.
2. Merge main's SDK `expectedRuntimeId` guard with mobile's injected crypto,
   pause/resume, cancellation, recovery and native WebSocket error details.
   Use the SDK's initial identity guard for saved mobile connections/probes,
   retaining the clear identity-change UI and the existing retry protections.
3. Inspect `client.ts`, `state.ts`, `input-presentation.ts`, `timeline.tsx` and
   their tests even if Git accepts them. Preserve main's host scoping and mobile's
   portable row/input projection without importing DOM, Electron or Node code
   into the native bundle. Keep desktop's serialized bridge export intact.
4. Retain current `whip`/`whipcode` build identity and environment-prefix support,
   newer host RPCs and native directory selection. Adapt mobile fixtures/docs
   where they assume one distribution name; do not regress either binary variant.
5. Confirm permission decisions, batched answers, child recipients, delivery
   uncertainty and reconnect reconciliation keep their identity/durability rules.
   Retain the mobile fixes for SQLCipher file URIs, modal-local errors and the
   bounded, cancelable Test Connection probe.
6. Merge `.gitignore` and refresh `.easignore` from the combined exclusions. EAS
   replaces Git ignore rules: add `/whipcode`, desktop `.stage`, `.dev`, `out`,
   renderer output and mobile native-module caches so new desktop artifacts or
   local runtime data cannot enter a later Expo upload.
7. Preserve the existing EAS project, bundle identifier, approved phone profile
   references and native storage layout. No new project, package upgrade or local
   storage reset is needed merely to bring source into the main folder.

## Phase 4 — Regenerate and validate the integrated source

First reconcile package manifests, then update the root lockfile with the
repository's Node 24/npm toolchain. Preserve unrelated pins and verify with a
fresh `npm ci` in the integration checkout. Generate the protocol through
`npm run generate` from the resolved source and run its drift check.

Run the current repository gates, avoiding duplicate suites already included by
`task check`, plus these affected surfaces:

- `task check` for Go, contract/SDK and web checks; relevant daemon/ACP/TUI race
  cases and `WHIP_SDK_RACE=1 npm run acceptance` for changed command/permission
  contracts and fixture behavior.
- `npm run check:mobile`, `npm run test:mobile`, pinned Expo Doctor from the
  mobile workflow, and production `npm run export:mobile` for iOS and Android.
- `npm run check:desktop` and `npm run test:desktop`, then the existing desktop
  staging/package verification and lifecycle smoke for the combined renderer.
  Preserve byte equality between the Go embedded web assets and Electron's
  renderer input. Use the established package workflow; no publication required.
- Existing browser multiple-host and question/permission acceptance: two hosts
  remain attached when selecting work, drafts stay with their host/root/recipient,
  questions route to the originating host, and disconnects do not stop host work.
- Native smoke on an isolated runtime: Test Connection success/failure/cancel,
  connect, send/stream, answer a question, create a session, force-quit/relaunch
  and recover saved host plus unsent draft. Verify identity-change protection.
  Use a native artifact matching the integrated fingerprint; an existing native
  binary may be repacked only when its native inputs still match.
- Both distribution variants must retain their contract/packaging checks. Review
  CI YAML permissions, aggregate dependencies and EAS archive contents after the
  union, not only TypeScript compilation.

Use isolated homes and synthetic providers for mutation tests. The running GPU
daemon, its sessions and credentials are not test fixtures. Existing passing
mobile/desktop builds are baseline evidence; record new results against the exact
integrated commit. Public releases, signing/notarization publication, a new EAS
phone rollout, and a GPU daemon upgrade are separate from this local merge.

Exit: a committed integration result with recorded checks, no conflict markers,
no unexpected generated/credential files and no unresolved behavioral regression.

## Phase 5 — Advance the requested main folder

1. Recheck the destination HEAD and clean status immediately before promotion.
   If it advanced, merge its new commits into the integration branch and revalidate
   affected areas before proceeding. If it has local edits, preserve them before
   advancing; do not reset, clean or overwrite them.
2. Fast-forward the existing `whip-rlm` checkout to the verified integration commit:

   ```sh
   git -C /Users/samheutmaker/Desktop/context-labs/src/rlm/whip merge --ff-only codex/mobile-integration
   ```

3. Refresh the destination's installed workspace dependencies and generated local
   build outputs as needed, then verify the destination HEAD/tree matches the
   accepted integration commit and the working tree is clean. Run a short build
   smoke there to catch checkout-local assumptions; do not repeat the complete
   unchanged test matrix without a new failure or source change.
4. Confirm mobile source/docs, web/desktop functionality and all existing commits
   are present. Record the checkpoint and integration commit IDs and the actual
   commands/check results in the evidence log.
5. Retain the original mobile worktree and backup ref until the destination is
   verified. Removing worktrees or build artifacts can be a later explicit cleanup.

A source merge does not update the installed iPhone app or the running daemon on
`gpu-4090-sam`. Both continue using their existing binaries and data. Any later
build/deployment should use the final integrated source and its matching evidence.

## Recovery and completion criteria

Before promotion, abort an unsuccessful merge in the integration worktree; the
main folder and mobile checkpoint remain intact. After promotion, use a normal
revert of the integration merge if a rollback is needed, preserving later commits
and user work. Do not use a destructive reset as the default rollback.

Done means the requested main folder contains the reviewed integration commit,
all authored mobile changes are accounted for, current web/desktop/distribution
behavior is preserved, the combined validation gates pass, and the main working
tree is clean. No push, release tag, running-daemon restart or worktree removal is
required to satisfy this merge request.
