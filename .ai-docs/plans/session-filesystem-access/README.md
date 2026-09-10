# Session filesystem access

Status: **Implemented and verified locally.** All six phases are complete.
Date: September 9, 2026.

See [validation and installation evidence](VALIDATION.md).

## Outcome

Full Access lets a session use files and working directories outside its project,
subject to the operating-system permissions of the user running the execution
host. The current directory remains the base for relative paths. The choice
continues to belong to the root session and survive reconnection and restart.

The motivating case is a session in `prime-agent` inspecting its sibling `whip`
repository through `files.list`. An isolated reproduction with saved permission
mode `automatic` rejected that file operation while a shell command successfully
read a sibling file. This is a mismatch between approval policy and file scope.

## Agreed behavior

| Concern | Intended behavior |
| --- | --- |
| Full Access | Ordinary root and inherited child file grants allow any host path accessible to the host's OS user; existing automatic approval behavior continues. |
| Ask for approval | Preserve the existing project file boundary and approval behavior. This change does not add prompts for expanding filesystem scope. |
| Explicit child restrictions | Preserve restricted operations and explicit path ceilings in both modes. Full Access never supplies a missing capability or bypasses revocation, expiry, budgets, or a narrower issuer. |
| Working directory | Navigation and relative-path context, separate from allowed paths and project-instruction boundaries. Navigating never grants access. |
| Persistence | Reuse `sessions.permission_mode`, the root snapshot field, and the existing durable mode command/event. No client-owned access setting or second permission preference. |
| Host | Paths refer to the host executing the session, including remote hosts; Full Access does not grant access to a different host or elevate the OS user. |
| Shell | Shell already executes with ordinary OS-user authority. Ask is not a filesystem sandbox. Preserve the separate shell capability, writer requirement, consent, and process lifecycle. |

New sessions and forks keep their current Ask default. Existing saved automatic
sessions receive the corrected Full Access behavior after upgrading. Headless
denial and separate MCP/browser/computer consent rules retain their existing
semantics; a nil consent callback is not evidence of Full Access.

## Source map

- [Workspace](../../../internal/capability/workspace.go) separates unrestricted
  canonicalization from the confined compatibility helper. Mutation locks are
  shared across projects and recheck their canonical target after waiting.
- [Dispatcher admission](../../../internal/capability/dispatcher.go) resolves
  both working directories and tool paths before the permission decision.
- [Session filesystem policy](../../../internal/session/filesystem.go) derives
  effective authority from the saved mode and exact issuer chain. The existing
  [capability ledger](../../../internal/session/capability.go) uses it for
  admissions, direct authorization, delegation, and shell writer requirements.
- [Delegation](../../../internal/session/permission.go) canonicalizes child
  scopes and checks issuer limits. New file delegations record the exact issuer
  ID and generation, with explicit inheritance distinct from path ceilings.
- [Recursive runtime](../../../internal/daemon/recursive_runtime.go) adapts
  `files.list` and `files.search` to `read`, and constructs default child grants
  that inherit their issuer's scope rather than copying cwd.
- [Tool services](../../../internal/tools/tools.go) bind the dispatcher and
  separately resolve working-directory changes.
- [Input preparation](../../../internal/daemon/input.go) authorizes canonical
  file mentions and explicitly invoked skills; [prompt composition](../../../internal/daemon/prompt.go)
  keeps project roots bounded and filters project context by current authority.
- [Mode command](../../../internal/daemon/client_control.go),
  [store](../../../internal/session/permission_mode.go), and
  [daemon opening](../../../internal/daemon/daemon.go) already save the mode
  atomically and restore it before resumed root/child work.

The current architecture is documented in [docs/concurrency.md](../../../docs/concurrency.md)
and [docs/frontend.md](../../../docs/frontend.md). The earlier
[persistence plan](../session-permission-mode/README.md) records history; it does
not override this approved expansion of Full Access semantics.

## Design boundaries

Keep this in the existing capability/session layers. No new permission engine,
dependency, OS sandbox, extra user-facing mode, or per-tool Full Access flags.

Represent these concepts separately:

1. **Path resolution:** canonical absolute path, including existing symlinks and
   missing leaves for writes.
2. **Session policy:** filesystem scope and approval behavior derived from the
   saved mode. The restricted project boundary remains stable when cwd changes.
3. **Grant limits:** operations plus either inherited session/issuer scope or
   explicit paths. Child effective authority is always bounded by its issuer.
4. **Project context:** the directories used to discover instructions and skills.
   Unrestricted access must not cause discovery from the entire filesystem.

Prefer a small tagged scope representation in the existing persisted grant JSON:
root session scope, inherited issuer scope, and explicit paths. Preserve existing
path data as the restricted baseline where applicable, and record exact issuer
ID/generation for new inherited grants. Empty paths must not ambiguously mean
both unrestricted and denied. Final field names belong to implementation.

Centralize effective path authorization and reuse it for normal admission,
permission revalidation, explicit capability authorization, and delegation. Read
policy and grants consistently within the ledger transaction; keep canonical
targets and policy validity bound to pending admissions. Do not persist `/` as a
replacement workspace root or rewrite every descendant whenever the mode changes.

## Phase 1 — Capture the contract and compatibility fixtures

Tasks:

- Turn the isolated sibling-directory reproduction into regression coverage for
  root Full Access and Ask. Include list/read/write/edit, cwd, and relative paths.
- Add fixtures for fresh roots, reopened roots, retained default children,
  explicitly restricted children, and revoked/expired grants.
- Establish the stable restricted boundary from the persisted bootstrap grant,
  rather than the mutable `sessions.cwd`. Handle roots that have never bootstrapped.
- Inspect what persisted evidence distinguishes automatically inherited grants
  from explicit grants. Record the exact migration eligibility rule in the tests.
- Define mode downgrade behavior: keep the visible cwd, enforce Ask boundaries on
  subsequent operations, and allow navigation back to an authorized directory.
  An absolute file target inside the allowed scope must remain usable even when
  the session's current directory is outside it.

Acceptance: tests describe the approved behavior, reproduce the present failure,
and explicitly cover ambiguous legacy grants. No live user database is needed.

## Phase 2 — Separate canonicalization and authorization

Tasks:

- Extract or reshape path resolution so it canonicalizes without deciding scope.
  Preserve symlink handling, relative bases, missing write targets, and useful errors.
- Move confinement into the shared authorization path. Preserve current behavior
  until the session policy/grant changes in the following phases are connected.
- Keep daemon-wide mutation locks keyed by canonical path, including paths shared
  by sessions with different projects. Recheck canonical target changes after
  approval and while acquiring a mutation lock.
- Distinguish a tool's target from a process working directory: validate the
  resource that the operation actually uses. A denied cwd must not block unrelated
  MCP calls or navigation back into the authorized project.

Acceptance: existing restricted-path, symlink, stale-admission, and shared-lock
tests pass. There is one reusable path authorization implementation, and removing
the old resolver check does not accidentally authorize existing restricted grants.

## Phase 3 — Persist scope semantics and upgrade existing sessions

Tasks:

- Add explicit inherited-versus-fixed scope metadata and the issuer references
  needed to enforce inherited file authority. Reuse the stored permission mode.
- Bootstrap ordinary roots with session scope; create ordinary child grants that
  inherit their issuer's scope while retaining their requested operation subset.
- Keep explicit scopes as ceilings. Validate them against the issuer on creation
  and use; mode changes never broaden an explicit ceiling.
- Introduce an in-place schema/data migration with a version boundary. Preserve
  saved modes, identities, history, budgets, revocations, expiry, and event cursors.
  Preserve the existing supported upgrade chain and reject unsupported old readers.
- Migrate default root grants using their bootstrap identity and invariants.
  Migrate retained children only when persisted provenance proves inheritance.
  Do not infer inheritance merely because a path equals the project root.
- If legacy child provenance is ambiguous, retain its existing explicit scope,
  expose it truthfully in capability inspection, and document that a newly created
  default child inherits the corrected policy. Do not silently replace that child
  or discard its work. This conservative compatibility case is a release criterion.
- Make migration transactional and repeatable after interrupted startup. Missing
  or malformed authority must not become unrestricted access.

Acceptance: migration/reopen fixtures preserve state and saved Full Access works
for existing roots. Proven default children inherit; explicit and ambiguous grants
stay bounded. An interrupted migration either commits completely or leaves the
previous valid database intact.

## Phase 4 — Connect policy to all execution paths

Tasks:

- Apply effective session/grant scope during `Begin`, `Decide`, direct
  `AuthorizeCapability`, grant issuance/delegation, and working-directory changes.
- Cover classic tools and Starlark adapters through their common dispatcher:
  list/search/read/write/edit, foreground shell, background shell, and managed
  workspace processes. File mentions use the same authority after canonicalization.
- Replace shell's literal root-path equality check with the equivalent effective
  writer-authority check. A grant explicitly limited to a subtree must not acquire
  general shell execution as a way around that ceiling. Preserve Ask's established
  ordinary shell behavior and describe its existing host access accurately.
- Continue ordering mode changes through the root actor, committing before live
  policy changes and rejecting changes during active agent work. Restore policy
  before queued or retained children can run; refresh it across runtime reloads.
- Revalidate pending operations against current policy and issuer generations.
  A mode downgrade must not revive stale approvals or authorize new outside writes.
  Already-running background processes retain the existing lifecycle; narrowing
  policy does not retroactively sandbox or automatically terminate them.
- Cover cwd outside the restricted baseline on downgrade/restart: preserve cwd,
  provide a specific scope error for denied work, and keep recovery navigation usable.

Acceptance: the original cross-repository case succeeds in Full Access, including
after restart. Ask and explicit child ceilings still reject disallowed file work.
Mode changes are isolated to the session tree and a failed save changes no live policy.

## Phase 5 — Align project context and client explanations

Tasks:

- Decouple project roots from unrestricted capability scope in prompt composition.
  Preserve bounded instruction/skill discovery and the rule to inspect applicable
  project instructions when entering another repository. Never scan `/` or the
  home directory just because the session has Full Access.
- Update the shared web/desktop picker description to explain access beyond the
  project, for example: “Access files outside this project and approve actions
  automatically.” Keep the existing two options and session-owned state flow.
- Update relevant TUI/CLI/ACP help and scope-denial errors. Use host-aware wording;
  avoid implying administrator access or that Ask provides a shell sandbox.
- Keep capability inspection truthful about inherited policy and explicit limits.
  If that requires additive wire fields, update the Go registry, generate protocol
  artifacts, and verify SDK/client handling; do not hand-edit generated files.
- Update canonical documentation with implementation: `docs/concurrency.md`,
  `docs/frontend.md`, `docs/features.md`, and relevant runtime/CLI help.

Acceptance: both UI surfaces describe the implemented behavior; reconnects display
the saved mode; drafts remain untouched; prompt discovery stays bounded and project
instructions remain scoped correctly when accessing a sibling repository.

## Phase 6 — Validate and prepare the local build

Run focused tests as each phase lands, then one complete integration pass:

| Area | Required cases |
| --- | --- |
| Paths | Absolute sibling targets, `..`, relative paths, external cwd, symlink aliases and retargeting, missing write leaves, paths shared across sessions. |
| Authority | Fresh/resumed roots; inherited and explicit children/grandchildren; issuer revocation/expiry; missing capabilities; denied operations; budgets unchanged. |
| Mode lifecycle | Ask → Full Access → Ask, outside cwd on downgrade, reconnect, runtime reload, daemon restart, failed saves, replayed commands, independent roots. |
| Persistence | Every supported legacy migration, ambiguous grants, interrupted migration, stale pending permissions, retained children with queued work. |
| Tools | Files, search, shell and background jobs, working-directory navigation, mentions; no regression in independent MCP/browser/computer consent. |
| Clients/context | TUI, shared web/desktop renderer, ACP mode synchronization, bounded project instructions, correct host identity and UI copy. |

- Run focused Go tests and race checks for capability/session/tools/daemon changes,
  plus TUI/ACP and prompt-composition coverage affected by integration.
- Run `task check`; run generated-contract and relevant app/SDK checks when touched.
- Use an isolated daemon/database and temporary sibling projects for an actual
  Starlark cell from both TUI and web/desktop. Exercise a write, restart, and repeat;
  then switch to Ask and verify the expected denial. No paid model call is needed.
- Record commands, outcomes, and any compatibility limitations in `VALIDATION.md`
  beside this plan. Mark phases complete only after their acceptance criteria pass.
- When executing the implementation and local upgrade, use the existing
  `task update:local` workflow and verify installed build identity and signature.
  Installation is part of the approved execution of this plan.

Acceptance: all checks pass, the screenshot scenario works on the integrated build,
and the final report identifies exactly what changed and any retained legacy-child
restrictions. No failed operation is automatically replayed by the upgrade.

## Delivery order

Execute phases in order. Keep path refactoring, schema migration, and integration
reviewable as separate changes, but ship the semantic change only when the full
vertical path and compatibility tests are complete. Intermediate work must not
expose unrestricted resolution without authorization or new grant semantics to
an old reader. UI copy and documentation ship with the working behavior.

- [x] Phase 1: contract and compatibility fixtures
- [x] Phase 2: canonicalization and authorization separation
- [x] Phase 3: persistent scopes and migration
- [x] Phase 4: execution and lifecycle integration
- [x] Phase 5: project context and client explanations
- [x] Phase 6: integration validation and local-build readiness
