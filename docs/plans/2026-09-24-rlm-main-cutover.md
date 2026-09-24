# RLM → main: clean-project cutover

Status: execution authorized and in progress. Revised 2026-09-24 after Sam clarified
that all installations are internal and will be wiped and reinstalled.
See [execution log](2026-09-24-rlm-main-execution.md) for completed actions and gates.
Repository: `context-labs/whip`. Actual branch names: `whip-rlm`, `whip-v1`.

## 1. Decisions and scope

- Keep `main` as the default branch and make it the canonical RLM source.
- Preserve both Git histories; do not automatically import old-main-only code.
- Main requires **PR + passing CI, zero approving reviewers, no routine bypass**.
- **Treat this as a fresh project without users.** No compatibility with either
  old `whip` or pre-reset `whipcode` installations, saved state, or updater URLs.
- **`whipcode` is the sole CLI.** Use one canonical application identity and
  `~/.whipcode` / `WHIPCODE_HOME`; no separate supported `whip` distribution.
- **`whip-v1` is a protected reference-only archive**, not a maintenance branch.
  No fixes, dependency maintenance, CI support, or releases from that branch.
- Start the new CLI release track at **`v1.0.0-alpha.N`**, followed by deliberately
  approved `v1.0.0`. Keep automatic main prereleases after publication enablement;
  stable and Desktop publication remain intentional, separately gated actions.
- Cut over the branch first. Clean up CI/build/release plumbing before enabling
  the new publisher. Do not release the untouched transitional RLM tree.

These decisions supersede the earlier legacy-maintenance/opt-in plan. We do
**not** need to preserve old install paths, reserve GitHub latest or Go semver
for legacy users, keep the old RLM branch for updaters, or maintain two products.
Preserving history is an archival task, not a runtime compatibility requirement.

Sam subsequently authorized end-to-end execution. Source, CI, GitHub rules and
automatic prerelease rollout are in scope. Wiping team machines and stable/Desktop
publication still require explicit confirmation; historical release refs stay intact.

## 2. Audit baseline: existing behavior, not the target design

The original live audit was 2026-09-24 around 17:08–17:18 UTC. Refresh it before
execution; statements below are snapshots, not continuously verified status.

| Ref | Audited commit |
| --- | --- |
| `main`, `whip-v1` | `3c7ff8f76a37fe2c014477c0a0e23d0aa09b80d2` |
| `whip-rlm` | `9edec15d90b6d1dd7ec839acb5bea17f6bf3f634` |
| Merge base | `37188abf3c0d77fc3c40073cd340e6d92d967ed9` |

There were **100 main-only commits** (71 nonmerge) and **324 RLM-only commits**;
980 reachable commits in the union. The diff spanned 2,759 files. No exact patch
equivalents were found for the 71 nonmerge commits; behavioral parity cannot be
inferred from that. `whip-v1` was unprotected. GitHub latest was old `v0.6.5`.

| Current workflow | Behavior / finding |
| --- | --- |
| `ci.yml` | PRs and main pushes: format, vet/custom analyzer, lint/tidy, portable Go race/shuffle tests with 90% coverage floor, four cross-builds, runtime, Swift driver, SDK/web, Desktop, mobile. `go` aggregates results. |
| `security.yml` | PRs, main pushes, weekly scan: govulncheck and CodeQL. Successful CodeQL execution is not itself a zero-findings policy. |
| `ci-whipcode.yml`, `security-whipcode.yml` | Separate reusable release validation. Adds installer/distribution checks and production npm audit, but omits mobile. These are not equivalent copies of main CI. |
| `release-whipcode.yml` | RLM pushes automatically run checks, build four CLI assets, publish `whipcode-v0.0.N` prereleases. No approval environment. |
| `release.yml` | `v*` tags build the `whip` identity and can claim GitHub latest; narrower release gates and no source-branch guard. **Must be removed/replaced before new v1 tags exist.** |
| `release-desktop.yml` | `desktop-v*` tags; validates ancestry in RLM; builds signed/notarized macOS arm64 app and matching Linux x64 backend, then stages/promotes. |
| `desktop-release.yml`, `desktop-publish.yml` | Reusable signing/publishing. R2 staging exposes public bytes **before** promotion approval. |
| `mobile.yml` | Validation and production exports; no store build/submission or OTA publishing in this workflow. |
| Loupe review/chat | Advisory review and a trusted-comment contents-write workflow with external Infisical/action dependencies; not a required gate. |

No public npm publisher or separate hosted-web deploy was found; workspaces are
private and web assets ship in the gateway/Desktop. Mobile store readiness is a
separate task, not a reason to retain a legacy release pipeline. `task ci` does
not reproduce every hosted platform/frontend/release gate; document the distinction.

Release run [36030785142](https://github.com/context-labs/whip/actions/runs/36030785142)
failed at the audited RLM tip. Linux/macOS distribution jobs and `ci / go` failed;
build/publish were skipped. Linux failed starting a daemon from an executable
without embedded web assets (`scripts/test-distributions.py:59`). That fixture
builds current source as two identities, not two historical versions.

**Revised implication:** retire the two-product fixture instead of repairing a
legacy coexistence requirement we no longer have. Replace it with packaged
`whipcode` fresh-install/daemon/web acceptance, building the actual embedded
renderer. Missing assets remain a real failure; deleting the fixture must not
hide a broken canonical package. Keep a green pre-cutover candidate; until this
replacement lands, fix the existing job's asset setup rather than bypassing it.

## 3. Target architecture: one product, shared validation, explicit publication

### CLI identity and installation

- One supported executable: `whipcode`; one home/env namespace. Default local,
  CI, standalone release, and Desktop-managed builds must agree without relying
  on renaming an executable or a release-only identity override.
- Make normal `task build` / `task install` build/install `whipcode` with embedded
  web assets. Remove duplicate identity-specific tasks once callers are migrated.
- Keep **one installer implementation**, recommended public entrypoint
  `main/install.sh`, now installing whipcode. Remove redundant `install-whipcode.sh`
  and `scripts/install.sh` copies after updating their in-repo callers. No redirect
  or compatibility wrapper for old URLs is needed.
- Point new updater code at that one installer/channel. Remove branches that
  choose between old whip/whipcode releases or fetch from `whip-rlm`.
- Retire pre-reset identity adapters, old release-prefix parsing, compatibility
  aliases and migrations that exist only to read pre-reset installations.
  Establish a fresh schema baseline where practical; preserve schema validation,
  permissions, corruption detection and new-product data integrity. Review the
  callers before deleting anything called “legacy” or “migration.”
- Keep standalone-vs-Desktop **update ownership**: a Desktop-managed backend must
  update with its app, not independently through the CLI installer. That is a
  current product invariant, not support for old installations.
- No wholesale repository/package/wire-identifier rename is required. Internal
  `whip` names are not automatically obsolete compatibility code. If an entrypoint
  directory is renamed to `cmd/whipcode`, move it rather than keep a shim. Do not
  advertise bare `go install .../cmd/whip@latest`: it names the wrong executable
  and does not reproduce the required web/native build. Document the supported
  packaged installer and source build instead.

### One CLI release contract

| Release | Source / gate | Discovery |
| --- | --- | --- |
| Automatic CLI prerelease | Fully validated main SHA, `v1.0.0-alpha.N` | New prerelease channel; never repository latest |
| CLI stable | Explicitly approved validated main candidate, `v1.0.0`, then normal semver | New stable channel and GitHub latest |
| Desktop | Deliberately approved main candidate, matching app/backend/renderer | Existing artifact-specific `desktop-v*` tags and beta/stable feeds only where needed; never CLI latest |

There is **no legacy channel**. Historical tags/releases may stay as inert
records, but new installers must select the new version range and required
whipcode asset set. They must not fall back to old `v0.6.5`, `whipcode-v0.0.N`,
or Desktop assets. Before v1 stable exists, document an explicit prerelease
install; a stable request must fail clearly, not silently install the old project.
After v1 stable, normal installation selects stable; prereleases remain explicit.

Use proper semver ordering, including numeric alpha counters (`alpha.10` after
`alpha.9`); do not preserve the old integer-only `whipcode-v0.0.N` parser. Allocate
unique N values, e.g. from a single workflow run number (gaps are fine), and
verify no collision with any existing tag. A rerun must not overwrite a release;
retry only a matching unpublished draft. Do not require alpha numbering to start
at 1. Advance the base version/channel deliberately after stable publication.

Plain `v1.*` tags may now belong to RLM; GitHub latest and Go-module discovery no
longer need legacy protection. Keep old tags immutable: “fresh project” is not
permission to rewrite published Git identities. No need to delete old assets to
simplify active code; any later artifact cleanup is a separate approved action.

### Shared CI, not mirrored pipelines

- Consolidate `ci.yml` / `ci-whipcode.yml` and `security.yml` /
  `security-whipcode.yml` into one maintained definition per concern. Use the
  existing checks/reusable-workflow machinery, not another CI framework.
- PR/main/release callers use the same validation definitions. Include the union
  of useful Go, security, dependency audit, renderer/SDK, native, Desktop/mobile,
  and **single-product** package/install acceptance checks. Remove the legacy
  identity matrix, not useful coverage or native-platform checks.
- Preserve a fail-closed aggregate and security statuses. Prefer keeping required
  names `go`, `govulncheck`, `codeql`; if reusable calls change their prefixes,
  verify real PR Check Runs and change the ruleset atomically. No missing jobs
  treated as success, and no separate release-only weaker gate.
- One CLI release orchestrator, with main-push prerelease and explicit stable
  modes, sharing build/publish steps. Remove old `release.yml` vs
  `release-whipcode.yml` product duplication rather than running both on v1 tags.
  Desktop reuses validation/renderer infrastructure but retains necessary signing,
  package, attestation and feed-promotion steps.
- Build a release renderer once and reuse its verified artifact across the CLI
  matrix and matching Desktop consumers. Publish only the exact validated source
  and complete checksum-verified asset set. PR jobs have no publication secrets.
- Avoid duplicate/re-entrant release triggers: a publisher-created tag must not
  start another publishing graph. Do not rely on `GITHUB_TOKEN`-created tag pushes
  triggering another workflow; invoke reusable jobs explicitly. Restrict publishing
  to canonical repo/event/ref; no PR artifact or untrusted cache can substitute
  for a validated release build.

## 4. GitHub policy changes

These were rulesets, not classic branch protection. Refresh effective rules and
actor permissions immediately before editing.

| Scope | Audited state | Target |
| --- | --- | --- |
| Main `21579831` | `~DEFAULT_BRANCH`; PR + 1 approval, strict `lint/test/driver`, update restriction; user `atbe` and devs team always bypass; force/delete blocked | Keep main default. PR + 0 reviews, full shared validation; remove update restriction **and bypass actors together**. Keep force/delete protection. Disable extra approval for unattributed changes. |
| `whip-v1` | Unprotected | Exact archive rules: prohibit update, deletion and force-push; no routine bypass. No required maintenance checks or supported PR/release route. |
| RLM `22588466` | Strict checks, force/delete blocked, no PR requirement or bypass | Protect during cutover; after snapshots/ancestry verification retire the branch and its rule. No installer compatibility reason to retain it. Branch deletion requires explicit execution approval. |
| `v*` | No matching tag rules found | New-tag creation restricted to chosen release actor(s); updates/deletions blocked. Source/version/identity enforcement in publisher, not just tag-name patterns. |
| `whipcode-v*` | No matching tag rules found | Retired namespace: block new creation and protect existing refs from changes. |
| `desktop-v*` | Admin-only creation, separate no-bypass update/delete rule | Retain needed immutability/authority, update source guards to canonical main. |

Do not remove main bypass while leaving its update restriction: that locks out
normal merges. Current `lint/test/driver` do exist on RLM PRs; they are too narrow,
not inherently missing. No current signed-commit/linear-history policy forbids
the proposed two-parent reconciliation. Use merge-commit integration, not squash.

Archived source still contains old workflows. No pushes or new tags on archived
commits; prevent new publication through tag authority, disabled obsolete
workflow entrypoints, source validation and removal of obsolete credentials.
A branch lock alone does not prevent tags, manual dispatch, or privileged reruns.
Do not add legacy CI/publisher maintenance to solve this—close the execution paths.
For new releases require main membership **and descent from the recorded clean
release baseline** (or an explicitly approved exact candidate). Old source is
also an ancestor of main after reconciliation, so membership alone is inadequate.

Desktop's five environments currently allow `desktop-v*`; promote requires Sam,
permits self-review and admin bypass, while signing/stage have no reviewers.
Recommend deliberate approval **before public staging**, then retain feed-last
promotion. Confirm environment reviewers/bypass separately from main's PR policy.

Actions currently default to read, but jobs can elevate; Actions may approve PRs,
all actions are allowed, and SHA pinning is not enforced globally. Audit writable
Loupe/release jobs and prefer minimal permissions and pinned release actions.
There is no active local pre-push branch guard; optional pre-commit hooks are not
enforcement. Native release immutability is disabled; evaluate enabling it against
draft/retry behavior. Do not add a PAT just to evade tag or branch rules. A broad
GitHub Actions App exemption is not specific to the intended publisher workflow.

## 5. Ordered execution plan

### A. Preserve source and stop accidental old publication

1. Refresh refs, open PRs, rules, runs, releases, external integrations. Record
   old-main M and approved RLM R and their tree IDs; freeze the merge window.
2. Protect `whip-v1` as reference-only. Snapshot both M/R outside release-tag
   patterns and create/verify a Git bundle. Existing tags remain untouched.
3. Prepare on a disposable topic branch/worktree, not by pushing to RLM (currently
   auto-publishing). No new release tags during preparation or cutover.
4. With explicit execution approval, pause obsolete publishers and drain/cancel
   active runs as appropriate. Record workflow IDs/settings: renaming a file later
   must not accidentally leave the new pipeline disabled or old one enabled.
   Restrict tag creation before any v1 candidate is tagged. Retire obsolete write
   credentials/dispatch access without breaking signing credentials still needed.
5. Resolve missing-assets failure enough for a green cutover candidate. If R
   changes, record the fix separately and approve the new R. Never bypass CI.

### B. Branch-only cutover, preserving history

1. Create a reconciliation commit whose tree is **exactly approved R** and which
   preserves both M/R as ancestry. One approach: branch from R, then merge M using
   the `ours` strategy. “Ours” must be R, never old main.
2. Do not use an ordinary merge or `-X theirs`; they can retain nonconflicting old
   code. Do not force-push/reset main or squash away R's ancestry.
3. Verify exact tree equality, both ancestor relationships and original commit
   reachability. Open a PR from the disposable branch to unchanged main.
4. Verify actual CI/security results, apply the agreed zero-review/no-bypass main
   rules atomically, and merge with a merge commit. GitHub auto-deletes merged head
   branches: use neither v1 nor RLM as the PR head.
5. Verify final main tree equals R, both tips remain ancestors, archive intact,
   main still default, and no publication occurred. Record cutover SHA.

### C. Clean the new main before enabling any release

Use small PRs with build/check coverage, not a temporary compatibility framework:

1. **Canonical identity/build:** default to whipcode; unify normal build/install
   tasks, help text, state/env selection, app/backend identity and update ownership.
2. **Delete superseded paths:** replace dual-product installers/updaters with one
   implementation; remove old prefix parsing/identity rewriting/legacy-only tests
   and pre-reset data upgrade machinery that has no new-product responsibility.
3. **Consolidate CI:** shared checks/security, single-product packaged acceptance,
   fail-closed gate. Ensure real renderer/native release assets are tested.
4. **Consolidate releases:** one v1 CLI publisher, one semver/channel policy and
   shared build steps. Retire old v* publisher before enabling the new one. Adapt
   publisher scripts, Desktop source/versions, installer discovery and their tests
   together; do not merely replace branch strings. Keep publishing disabled.
5. **Docs/automation:** new README/setup/source install, one download flow,
   CONTRIBUTING CI mapping, Desktop runbook, examples and agent instructions.
   Search active code/docs for old branch/URL/tag assumptions; do not rewrite
   history files or blindly rename module/wire identifiers. Follow docs/frontend.md
   and update it if implementation changes frontend architectural contracts.
6. Retarget useful open PRs to main; close/archive obsolete ones. One Dependabot
   and scheduled-security policy on main. No v1 maintenance automation. Preserve
   a selective old-commit port ledger rather than future main↔v1 merges.
7. Record the **clean release baseline** SHA. Re-run the full candidate validation
   before opening publication. Verify old workflow IDs/dispatch routes are retired.

### D. Fresh-install rehearsal and publication enablement

1. Write a short team reset checklist: close apps, stop old daemons/services,
   inventory binaries/PATH links, old app state and desktop/remote runtime paths,
   save anything desired, explicitly confirm deletion, then reinstall. No automatic
   migration, broad `rm -rf`, or silent installer deletion of a user's old data.
   Apply the same reset expectation to previously provisioned remote backends.
2. Test candidate artifacts in disposable clean homes first: all CLI platforms,
   embedded web startup, native helper where applicable, config/session creation,
   daemon lifecycle, installer checksums/atomic replacement, and Desktop packaging.
3. Test new-channel selection including no-v1-stable-yet, semver ordering, ignored
   historical releases, failed/cancelled/superseded builds, reruns and tag collisions.
   No legacy-vs-RLM coexistence or old-updater regression suite is required.
4. Retain tests for **new-to-new** normal updates, safe failure, daemon reconnect
   and Desktop-owned updates; the clean reset removes pre-reset upgrade promises,
   not the new product's basic update mechanism. Use new-schema fixtures only.
5. Explicitly approve the publication-enablement PR/config change. Its own merge
   can publish the first `v1.0.0-alpha.N`. Ensure source checks, complete validation,
   tag authority, concurrency and draft/asset verification are operational first.
6. Verify one automatic main prerelease end to end; then the team wipes/reinstalls
   using the documented flow. Retire RLM branch/rules after preservation and caller
   checks; no compatibility branch or redirects. This retirement is a planned,
   separately approved ref deletion, not an action performed by this research.
7. Promote a validated main candidate to `v1.0.0` only on explicit approval; make
   it the new CLI latest. Desktop beta/stable publication and public staging follow
   their own approval. Mobile store/npm publishing remain out of scope.

## 6. Acceptance, recovery, and remaining checks

Done means:

- Main is canonical, requires PR + complete passing CI, needs no review, has no
  routine bypass, and blocks force/deletion. Test effective rules/mergeability,
  not destructive pushes to production refs.
- Both histories remain recoverable; v1 is reference-only with no publishing path.
- One documented CLI, build/install flow, CI definition per concern, and release
  implementation. No active runtime dependency on the old branches or tags.
- Fresh standalone/Desktop installs work; failed builds cannot publish; no old
  release is selected by the new installer. Tag creation does not double-publish.
- Old-only behavior is considered by an intent/SHA port ledger, not imported by
  bulk merges. Old code can be studied without being maintained in active paths.

Recovery: pause the new publisher under the incident procedure; fix/revert source
with a reviewed commit, repair rules from saved settings, and issue a new release.
Do not force-reset main, move tags or overwrite released binaries. If a prelaunch
candidate needs another team reset, do it explicitly; do not build a cross-era
migration system as rollback insurance. Keep normal new-product data safety.

Remaining execution details (not additional compatibility work):

- Select tag-creation/release actors and approval environments, including whether
  Desktop pre-staging approval permits self-approval or admin bypass.
- Confirm first-candidate version allocation, installer prerelease interface, and
  any retained artifact-specific Desktop version alignment during implementation.
- Verify external Infisical, R2, signing/Apple, Apps/org policies and hosting with
  owners. Org-wide admin APIs were denied; App-installation inspection needed a
  different credential. No repo-visible inherited rules/webhooks/Pages deployment
  were found, but external integration absence is not established. Secret names
  alone do not prove valid credentials or least-privilege access. EAS/store state
  was not checked. No credential values need be exposed to close these gaps.

## Evidence index

Source pointers refer to the audited RLM tree, not the proposed implementation:

- [CI](../../.github/workflows/ci.yml),
  [duplicated CLI CI](../../.github/workflows/ci-whipcode.yml),
  [security](../../.github/workflows/security.yml).
- [old v-tag publisher](../../.github/workflows/release.yml),
  [RLM publisher](../../.github/workflows/release-whipcode.yml),
  [publisher script](../../scripts/publish-whipcode.sh).
- [Desktop entrypoint](../../.github/workflows/release-desktop.yml),
  [Desktop staging/promotion](../../.github/workflows/desktop-publish.yml).
- [identity adapter](../../internal/buildinfo/buildinfo.go),
  [updater branching](../../cmd/whip/update.go),
  [dual-identity fixture](../../scripts/test-distributions.py),
  [Taskfile](../../Taskfile.yaml).
- Read-only GitHub audit: `repos/context-labs/whip/{branches,rulesets}`;
  `rulesets/{21579831,22588466,22586583,22586584}`;
  `rules/branches/{main,whip-rlm,whip-v1}`; environments/deployment policies;
  Actions permissions; immutable-releases; releases/latest;
  `actions/runs/36030785142/jobs`.
- Prior live-settings artifact `7e5e503906b9cefbb885821c590ea57b`, bytes 0–13725;
  history report `9c14b4c9abe234d6928ab01dc0c0becc`, bytes 0–10363;
  final run/latest `95cf1ebe3f69b6f9e1b98152bb347046`;
  failure log `cfdb622106b049f321d216597f183980`.
