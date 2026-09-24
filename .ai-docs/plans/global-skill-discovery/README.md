# Global skill discovery before choosing a project

Branch: `whip-rlm` (existing worktree; no implementation branch created)
Status: implemented; packaged web and staged native acceptance passed
Date: 2026-09-23

## Goal

In New Chat, a connected execution host should offer its user-global skills when
the composer is focused and the user types `/`, even when no project folder,
provider, model, or session has been selected. Selecting a result inserts the
existing `$name` reference; it does not send or execute anything.

When a folder is selected, use the existing authorized project-plus-global
catalog. Existing-session and child-agent discovery keep their current scopes.
Preserve instant local prefix filtering and the existing compact dropdown.

This follows the user's fresh Desktop launch report and request to research and
plan global discovery. The proposal below records the approved design; completed implementation and
validation are documented at the end. Earlier slash implementation/acceptance remains documented in
[the original plan](../desktop-slash-skills/README.md) and
[instant filtering](../desktop-slash-skills/instant-filtering.md).

## Proposed product decisions

These are deliberately narrow defaults inferred from the requested behavior:

- **Global means the selected execution host's existing user skill roots**, not
  the browser/Electron UI machine's roots when connected remotely.
- **No project is required for discovery or insertion.** A connected host and
  resolved definition/permission context are still required. No model/provider
  readiness dependency is added.
- **A project is still required to send the first message.** This does not add
  folderless sessions or automatically choose the launch directory/home folder.
- Before choosing a folder, show globals only; afterward show the existing
  project-plus-global results. Any transition back to an empty cwd returns to
  globals. There is no dedicated clear-folder action today; do not add one for
  this feature. Host changes already clear cwd, and a new folderless draft can
  exercise the same state; direct same-host clear is a component lifecycle test.
- Use one list, existing ordering and duplicate-name rules; no new tabs, sections,
  badges, scope switch, helper footer, result count, or insertion instructions.
- Keep name-prefix matching (case-sensitive, no normalization), 1,024-entry
  negotiated preload, filtering before the 32-row display cap, and the existing
  bounded fallback for older/incomplete catalogs.
- Do not automatically scan/import Claude or Codex skill folders. Those remain
  explicit import sources. Existing Whip-native and `.agents` roots are enough
  for this change.

No blocking product question was found. Broader cross-harness discovery or
folderless sending would be separate decisions, not hidden additions here.

## Non-goals

- No new skill installation/import UI, settings screen, filesystem watcher,
  persistent/global renderer cache, command palette, or slash command registry.
- No changes to invocation syntax, definition policy, file grants, permission
  prompts, session creation, model requests, or skill-body execution.
- No duplicate-precedence change, project ancestry expansion, new source paths,
  source-path exposure in results, or automatic revalidation of inserted tokens.
- No changes to the parser, multiline editor, popup primitives, typography,
  colors, native overlay lifecycle, or keyboard behavior unless a regression
  test demonstrates a necessary adjustment.
- No TUI/mobile UX changes, protocol major bump, or new dependencies.

## Research findings

### 1. The folder restriction exists in both renderer and daemon

`packages/app/src/use-skill-completion.tsx:9-36` models a New Chat scope with
`cwd`, then requires a nonempty folder before any request. Its query at lines
63-68 always sends `cwd`, and the status at lines 121-127 tells the person to
choose a folder. `packages/app/src/welcome.tsx:126-127` supplies that scope; the
normal send path separately checks cwd at line 142.

`internal/protocol/completion_types.go:11-18` requires cwd in today's request
shape. `internal/daemon/skill_completion.go:25-65` rejects empty cwd, resolves a
workspace, then previews its canonical root under the selected definition and
permission mode. `host.skills.complete` is already a host-runtime Query
(`internal/protocol/registry.go:99-100`), so a second RPC is unnecessary.

**Do not simply remove the guard.** Both `Workspaces.Open("")`
(`internal/capability/workspace.go:37-54`) and
`LoadPromptSkillsContext` (`internal/rlm/environment.go:233-252`) resolve an empty
path against the daemon's process directory. That would silently discover a
project the person never chose. `skills.DefaultDirs` also uses `os.Getwd`
(`internal/skills/skills.go:39-44`). Substituting `HOME`, `.`, a last-used folder,
or a nonexistent dummy folder is not a safe implementation.

### 2. The authoritative loader already knows the global roots

`internal/rlm/environment.go:364-389` builds the authorized skill directories:
project `.agents/skills` directories, then `config.Dir()/skills`, then
`os.UserHomeDir()/.agents/skills`. Passing **no project chain** to this directory
builder gives exactly the desired global roots.

Application-owned paths depend on the compiled distribution, not executable
spelling (`internal/buildinfo/buildinfo.go:10-27`):

| Distribution | App-global skills | Shared user-global skills |
| --- | --- | --- |
| `whip` | `$WHIP_HOME/skills`, otherwise `~/.whip/skills` | `~/.agents/skills` |
| `whipcode` / Desktop payload | `$WHIPCODE_HOME/skills`, otherwise `~/.whipcode/skills` | `~/.agents/skills` |

`~` means the daemon's OS-user home. `WHIPCODE_HOME` alone does not relocate
`.agents/skills`. `~/.claude/skills` and `~/.codex/skills` are explicit import
sources, not automatic discovery roots (`internal/skills/skills.go:60-71`).

The existing loader preserves root order; completion and invocation both make
the **last occurrence of a name win** (`internal/daemon/skill_completion.go:136-169`,
`internal/daemon/input.go:80-89`). Thus `.agents` user-global wins over app-global,
which wins over project duplicates today. Keep this behavior; do not promise
that selecting a project makes its copy override a global skill.

### 3. Definitions and authorization do not require a workspace

`latestDefinition` resolves built-ins or a latest registered revision from the
store without cwd (`internal/daemon/definition.go:55-78`). Its
`instructions.skill_discovery` flag is the relevant discovery policy
(`internal/agentdef/definition.go:134-143`, `prompt.go:28-37`). Disabled discovery
must still yield no automatic catalog. Existing-session pinned definitions and
child grants continue through their existing path unchanged.

User skill roots are already globally trusted context, including for a
skill-enabled definition without project-read capability. Permission mode must
still be validated, but prompt/automatic modes should produce the same global
preview. Preserve the existing canonical-target and user-root symlink policy
(`internal/rlm/environment.go:302-327`); do not redesign it in this feature.

The read-only promise is **no session, runtime command, grant, model call,
import, or instruction execution**. Be precise: the existing metadata parser
reads a bounded file prefix that may contain body bytes, but never returns or
executes the body (`internal/skills/prompt_catalog.go:124-171`); `config.Dir()`
can create its configuration directory (`internal/config/config.go:308-320`).
This plan does not claim literally zero filesystem syscalls/effects or change
those existing helpers as incidental cleanup.

### 4. Existing interaction/cache machinery should be extended, not replaced

The shared hook already keys metadata by runtime, client lifetime, draft owner,
scope, prefix and limit; cancels obsolete reads; preloads on focus; filters
locally; refreshes at stale focus/open boundaries; and falls back when the
catalog is incomplete (`use-skill-completion.tsx:18-90`). Add the global scope to
this same ownership model. Never merge cached global candidates client-side with
project results; the backend remains the authority for ordering and deduplication.

The existing UI is a focused native textarea plus a non-modal anchored listbox
and native-surface coordination. This is a behavior extension within Whip's
established design system, not a redesign. Follow `docs/frontend.md` and retain
the user's helper-text/spacing removal. Current source still contains a separate
keyboard footer (`packages/ui/src/textarea-suggestions.tsx:98,114`); this research
does not claim all helper text is absent or change that shared primitive. Do not
add new global instructions or restore previously removed items.

### 5. Fresh Desktop environment interpretation

The user's command isolates `WHIPCODE_HOME` and Electron user data, not `HOME`.
For that launch, the future global query should read:

- `$TEST_DIR/runtime/skills` (empty unless seeded), and
- the daemon user's normal `~/.agents/skills` (if populated).

It should **not** scan the repository merely because Electron was launched
there, or fall back to the user's ordinary `~/.whipcode/skills` despite the
explicit override. `WHIPCODE_NETWORK=0` does not block local metadata discovery.
A fully isolated test that also replaces `HOME` needs to seed a skill fixture;
empty installations correctly produce an empty list.

Fresh startup currently shows the empty workspace, not an automatic New Chat
(`apps/desktop/renderer-tests/startup-frontdoor.test.tsx:149-171`). Acceptance
must use the real **New session** action before focusing the composer; this
feature must not alter the startup landing screen. The current frontdoor fake
handshake does not advertise skill capabilities, so extend it deliberately for
the new case rather than treating an unchanged startup pass as skill coverage.

Desktop normalizes `WHIPCODE_HOME` and removes legacy `WHIP_HOME`
(`apps/desktop/src/runtime.ts:152-160`). The startup fixture already separates
OS home, runtime home, user data and temporary paths
(`apps/desktop/scripts/startup.mjs:370-393`). Reuse that isolation pattern.

### Prior art and project guidance

- `docs/roadmap.md:155-158` already records shipped slash discovery. Extend its
  availability; do not introduce a competing catalog.
- `docs/features.md:981-1036` describes existing sources and invocation;
  `docs/frontend.md:2072-2130` owns renderer/package/cache architecture.
- `docs/learnings/other-harnesses/live-ux-probe.md:22-32` and
  `opencode/opencode-ux.md:175-189` document live-filtered composer discovery.
  Reuse the interaction lesson, not OpenCode's broader command/template system.
- Applied `new-feature-development`, `ponytail`, frontend-design and
  interface-design guidance. The minimal solution is one explicit scope in the
  existing query plus removal of the inappropriate renderer readiness gate.

## Proposed design

### A. Add an explicitly negotiated global request variant

Extend `HostSkillCompletionParams` with optional `scope: '' | 'global'` and
make cwd optional in the serialized/generated shape. Empty scope is a legacy
alias, matching Go's zero value; clients should omit it rather than send `''`.
The source schema and generated validators must accept both literals. Do not
add a `project` enum: the existing no-scope request already means the
selected-project preview.

```ts
// Only when host_skill_completion AND host_global_skill_completion are negotiated.
client.call('host.skills.complete', {
  scope: 'global',
  definition: 'coding',
  permission_mode: 'prompt',
  prefix: '',
  limit: 1024,
});

// Once a folder is selected, preserve the old request shape (including old hosts).
client.call('host.skills.complete', {
  cwd: selectedFolder,
  definition,
  permission_mode: permissionMode,
  prefix: '',
  limit: 1024,
});
```

The examples assume `skill_catalog_completion`; without it, retain the existing
32-result server-prefix fallback in the **same** scope.

Validation matrix:

| Scope | cwd | Behavior |
| --- | --- | --- |
| Omitted/empty | Nonempty | Existing project-plus-global preview; validate real directory |
| Omitted/empty | Omitted/empty | Reject, as today |
| `global` | Omitted/empty | User-global roots only |
| `global` | Nonempty | Reject ambiguous request |
| Unknown | Any | Reject |

Keep definition, permission, prefix, limit, byte and warning bounds. Invalid
project cwd remains an error, never a global fallback. Candidate/result shapes
stay unchanged. Update the source schema constraints in
`internal/protocol/schema.go` so Go request admission and generated JavaScript
validators accept/reject this matrix consistently; retain defensive handler
validation for direct callers. Daemon `ValidateRPC` runs before dispatch, so
changing only the handler cannot enable a missing-cwd global request. Use the
existing schema union/intersection conventions rather than handwritten generated
types or a new validation framework. Test generated types for the shapes they
actually express; do not claim TypeScript enforces every runtime constraint.

Advertise/request `host_global_skill_completion` via the existing server/SDK
capability lists. It adds semantics independently of `skill_catalog_completion`;
no changes to old capabilities' meaning. An old client retains identical cwd
behavior. A new client does not call the global variant on an old host or send a
new `scope` key with ordinary project requests. A failed request is an error,
not permission to probe fallback folders.

### B. Add a narrowly scoped global metadata entrypoint

Resolve/validate the effective definition without opening a workspace. Add a
small context-aware RLM helper that reaches the existing catalog loader with no
project chain, honoring `SkillDiscovery`, without resolving a WorkingDirectory
or accepting caller-supplied project roots/skill directories. Prefer a narrow
signature accepting the discovery flag over a general options object that
could accidentally forward `SkillDirs` overrides.

Reuse `promptSkillDirs(nil)`, the metadata walker and current user-root trust
rules. Build fresh options internally with a **nonnil deny-all project callback**
and no `SkillDirs` override: a nil callback skips `ResolvePromptSkill` entirely
(`environment.go:289-299`). The deny-all callback keeps canonicalization and the
existing original-path global trust check in use; it does not revoke trusted
user-root symlinks or grant a project alias global authority. Do not change
zero-value semantics of `LoadPromptSkillsContext` for all
its other callers. No `Workspaces.Open`, `ComposePrompt`, session construction,
permission grant, filesystem watch, or daemon catalog cache in this branch.

Factor the existing roster-to-`CompletionResult` logic once, so global, scoped
host and workspace completions share last-name-wins, sort/prefix, Unicode,
warning, byte and truncation behavior. Keep cancellation checks, including an
empty catalog. This is a small branch plus shared formatting, not a second
scanner or discovery framework.

### C. Make renderer scope explicit and keep lifecycle ownership

Represent three cases in the shared hook: existing root/agent, New Chat selected
project, and New Chat global. Derive the last two from the existing New Chat cwd
state without saving new state. Include the scope kind and definition/permission
in the query/selection identity. Preserve runtime/client/owner separation.

For the global case, readiness requires a connected host, resolved authority,
unblocked active composer and both host completion capabilities, not a folder.
Leave model/provider and Send readiness in their existing owners. Preserve
`welcome.tsx:99-100` permission resolution: an explicit tab mode is ready
immediately; otherwise await runtime configuration before applying its default
(or legacy `prompt`). Do not guess prompt while configuration is pending/failed.
The definition name remains `tab.definition ?? 'coding'`; the daemon resolves
and validates it, so no provider or definition-list fetch is needed just to
discover skills. Query routing uses the global request above; project/session
routing stays unchanged.

Scope changes (folder selection/clear, host/client/runtime, definition,
permission), disconnect, hiding and disposal must cancel obsolete requests and
prevent old rows or pending selections from being accepted. A focused composer
preloads the new scope as it becomes ready. Retain the actual
`document.activeElement === input.current` guard: a merely selected pane is not
a focused textarea. Preserve the warmed observer across ordinary blur/Escape;
do not blanket-reset an in-scope preload on dismissal. Do not automatically reopen a
previously dismissed popup. Draft contents and selection remain owned by the
existing composer; inserted `$name` remains ordinary text through scope changes.
Same-mounted folder changes and insertion must preserve caret/focus. Host
switches already remount Welcome (`welcome.tsx:58`): the stable draft survives,
but cross-host caret restoration is not currently wired there. Do not claim or
expand that guarantee as part of this feature; preserve the draft and reject
stale selections. If a discovered regression requires restoration, reuse the
existing bounded `runtime.compositions` selection map, never a second owner.
Actual send re-resolves the reference under final session scope and authorizes it.

Keep 1,024-entry focus preload, local `startsWith` before the 32-row cap,
10-second boundary freshness, zero per-key warm RPCs, delayed cold loading label,
same-scope stale matches plus Retry, and fallback/truncation behavior unchanged.
A global fallback request must never acquire cwd from stale project state.

### D. Small state-copy changes only

- Supported host + no folder: normal skills list; no choose-folder prerequisite.
- Empty global catalog: `No global skills available.`
- Nonempty prefix with no match: existing `No matching skills.`
- Old host supporting only project discovery: `Update this host or choose a
  project folder to browse skills.` Do not send a speculative request.
- Disconnected, loading, errors and host warnings retain their existing priority
  and treatment. Disconnection should not be hidden behind a choose-folder tip.
- Successful result rows retain names/descriptions and the current layout; do
  not add scope instructions or restore the two removed helper items/whitespace.
  This does not redesign the separate existing shared keyboard footer.

## Ordered implementation plan

1. **Protocol and negotiation:** add the explicit variant/capability in
   `internal/protocol/completion_types.go`, `internal/protocol/schema.go`,
   `internal/daemon/server.go`, and `packages/sdk/src/client.ts`; regenerate
   protocol schema/validators/types through the existing generator. Cover the
   matrix in `internal/protocol/schema_test.go`,
   `packages/protocol/scripts/interop.test.mjs` and generated type tests, alongside
   daemon handler/capability compatibility checks.
2. **Daemon/RLM:** implement the global-only loader seam and shared formatting in
   `internal/rlm/environment.go` and `internal/daemon/skill_completion.go`;
   extend focused loader/RPC tests before changing renderer gating.
3. **Shared app:** extend scope/readiness/request/status in
   `packages/app/src/use-skill-completion.tsx` and New Chat integration in
   `welcome.tsx`. No `packages/ui` rewrite or Desktop-only filesystem bridge.
4. **Regression coverage:** extend existing app catalogs, welcome/composer and
   Desktop startup fixtures; exercise real daemon/browser and native Desktop
   acceptance separately, retaining warm-filter performance assertions.
5. **Docs/review:** update canonical feature/frontend docs and the existing
   roadmap entry after implementation; least-code/adversarial review, final
   packaging and evidence record before claiming delivery.

## Validation and acceptance

### Backend/protocol

- Global request finds both isolated user roots without cwd; project fixtures in
  process cwd, its ancestors and unrelated folders cannot enter the result.
  Use deliberately malformed project metadata as a scan tripwire.
- All scope/cwd combinations above, unknown definition, disabled discovery,
  no project-read grant, both permission modes, invalid bounds and cancellation.
- Whip vs Whipcode environment isolation, missing global roots, malformed global
  metadata, existing symlink trust, duplicate winners and explicit-only skills.
- Project preview still contains authorized project plus globals; existing root
  and child pinned-definition/authority behavior unchanged.
- Preserve prefix/Unicode, warning/truncation and response-size limits; no bodies
  returned, no new session/agent/command/event/grant/model effects from discovery.
- Capability intersection and old/new client-host matrix; no major version bump.

### Renderer/integration

- Fresh connected New Chat, **no folder and no provider**, focuses and preloads
  exactly one global catalog. `/` shows seeded globals; selection inserts `$name`
  with caret/focus preserved, zero sends and zero session creation.
- Cold pending Enter, repeat Enter, IME, Escape, click, Shift+Enter and URL/path
  trigger regressions retain current behavior.
- Warm typing beyond the first 64 entries, before/after freshness expiration,
  produces correct local prefix results next frame, zero per-key RPCs/loading
  flashes and at most 32 visible rows. More than 32 globals remains searchable.
- Global-to-project, project-to-global, project-to-project, host, definition and
  permission transitions: obsolete request aborted, late response/selection
  ignored, no cross-scope cache leak, draft preserved, new focus preload.
- Old host makes **zero global calls**; choosing a folder resumes the legacy
  request without `scope`. No catalog capability/truncated catalog keeps global
  server-prefix fallback; arbitrary errors stay visible rather than falling
  back to an implicit directory.
- No global skills, disconnected host, cold error, stale refresh error/Retry,
  hidden New Chat pane and reconnect covered in existing suites.

### Actual product check

Build the final Desktop payload and launch a fresh isolated fixture with separate
`HOME`, `WHIPCODE_HOME`, Electron user-data and executable paths. From the empty
workspace choose **New session**, without selecting a project. Seed one skill
in each global root and one project-only skill outside them. Before selecting a
folder: globals only; no session/provider needed. Select the project: include its
skill. Switch folder/host or open a new folderless draft: no stale scope.
Exercise direct same-host cwd clearing in the component fixture, not an invented
UI control. Select a reference then send
through the ordinary first-message path to verify invocation parity.

Extend `apps/web/scripts/slash-skills.mjs` for the new pre-folder case using a
real isolated daemon; retain genuine delayed replies and next-frame warm typing
checks. Extend the relevant Desktop startup test/probe rather than treating web
Chromium as native Desktop acceptance. Use fixture-owned resources only and
stop spawned processes. Record the exact final artifact, functional assertions,
visual inspection and native-surface acceptance separately; screenshots alone
are not proof of inspected UI.

Suggested checks after implementation:

- Focused `go test -race` for daemon/RLM/skills/protocol; then normal repo Go gates.
- `npm run generate -w @whip/protocol` and protocol generated-drift/check suite.
- SDK check, app/UI type checks, focused skill tests, full `npm run test:web`.
- `npm run build:desktop`, actual shared-web and native Desktop fixtures, then
  `task check` as the repository-wide gate. Preserve/report unrelated failures;
  never reformat or repair unrelated work to manufacture a green result.

## Documentation and delivery checklist

- [x] Trace current discovery, environment, definition, authorization and cache.
- [x] Specify global scope, compatibility, non-goals and validation plan.
- [x] User approved execution; implementation started after approval.
- [x] Implement protocol/SDK/daemon/RLM with focused tests.
- [x] Implement shared New Chat scope transitions and regression tests.
- [x] Validate final packaged shared-web and native Desktop behavior (scope below).
- [x] Update `docs/features.md` Skills and `docs/frontend.md` skill suggestions;
      revise the folder prerequisite, global source semantics and cache identity.
- [x] Amend `docs/roadmap.md` existing entry; update workflow
      inventory/fixture metadata if its contract assertions require it.
- [x] Record review, exact check results and any unverified/blocked coverage here.

## Worktree safety and research evidence

Research ran against the existing dirty `whip-rlm` worktree and initially added
only this plan. After approval, implementation began with focused source/diff
baselines saved outside the repository. User configuration and skill installations
remain untouched. Nothing is staged or committed. Preserve unrelated ongoing
work, especially mixed protocol and daemon files. The validation section above defines the acceptance scope. Implementation checks
are now in progress; final results and any unverified coverage will be recorded
below. Research itself ran no tests/builds.

Backend research artifact: `af05bd8554df5baba33a24759bb728ea`
(`global-skills-backend-research`, bytes 0-17518); exact source-file spans are
listed above. Backend follow-up clarified deny-all callback and schema admission;
final research-only review found no blockers. Frontend final research report
`5bf3449efb1a3f713d2952b5ee8bf04a` approved the narrow integration with corrections
for folder-clear state, live permission defaults, host-remount caret limits,
actual startup navigation and existing keyboard-footer truth. Those corrections
are incorporated. Those research reviews preceded implementation and runtime
validation; current execution evidence follows.

## Implementation and validation record

Approved implementation completed on 2026-09-23. The frontend change is localized
to the shared skill-completion hook; `welcome.tsx`, editor/parser and UI primitive
sources did not need changes. The new backend helper and request branch reuse
existing discovery/formatting. The only extra implementation path beyond the
initial list was `cmd/whip-contract/main.go`: the generated example needs a valid
global request now that its former zero-value cwd fixture is invalid.

### Source and review

- Backend/protocol/SDK: 20 incremental paths; snapshot, `changes.json`,
  `incremental.patch` and logs in `/tmp/whip-global-backend.nA4ZHI`. Final report
  artifact `b81b5bffca4fb1ea1a3c036fae525878`.
- Frontend: four incremental paths (hook, two app test suites, one Desktop
  bootstrap test). Baseline and `incremental.diff` in
  `/tmp/whip-global-frontend-baseline.ynxZwt`.
- Docs baseline: `/tmp/whip-global-docs.AG96pm`; product-script baseline:
  `/tmp/whip-slash-skills-baseline.Q1Dmg4`. Existing dirty/untracked files were
  treated as prior work, not replaced as newly authored files.
- Independent incremental core/protocol/lifecycle and least-code review found no
  concrete blockers. It also inspected the real-web and temporary native probes
  for result substitution/assertion weakening; none found. This is source review,
  not a substitute for runtime acceptance.

### Automated checks

- Protocol generation, type checking, 16 interop tests and Go/JavaScript drift:
  passed. SDK build/type checks and **458 tests**: passed.
- App TypeScript and **111 focused / 1,259 full frontend tests**: passed. Logs:
  `/tmp/whip-global-frontend-{types,focused,full}.log`.
- Focused Go race tests for daemon/RLM/global discovery, full protocol/skills
  race suites and both Whip/Whipcode distribution-global tests: passed.
- `go vet ./...`, `go run ./cmd/whipvet ./...`, `go test ./...`: passed together
  (`job-b3749b42`, ended 22:51:40Z).
- Desktop and UI TypeScript: passed. `npm run test:desktop`: passed (138 passed
  / 10 skipped in the first suite, 99 passed in the second, startup self-test
  passed). Output `21f40d239891c696ab47623bfe4406f8`, `job-1d67f035`.
- `task check`: offline model metadata check passed, then stopped at existing
  formatting in `.claude/worktrees/session-trace/internal/daemon/session.go` and
  `internal/session/otlp_export.go` under that same nested worktree. Those files
  were left untouched. Log: `/tmp/whip-global-task-check.log`. This gate is not
  claimed green; broad Go checks were run separately as noted above.
- Initial implementation checks caught a formatter variable-scope error, an
  invalid auto-generated request fixture and a test parenthesis typo. All were
  corrected before the passing results above; none remain product blockers.

### Exact packaged artifact

`npm run build:desktop` passed (`job-038b9bcd`, 22:50:42–22:51:04Z);
log `/tmp/whip-global-build-desktop.log`. This includes production web build,
Go-embedded asset packing, Swift helper and staged Whipcode runtime.

- Renderer: `d9a3a0623ee150d09f50b7c74ef970807ce88eb69ea62d5250ef23d7e12acd88`
- Whipcode: `0fe4ba9a2c9cd820f9a479aebd5ec2373cd599b87cfca20a471f317a952617bf`

### Actual native Desktop

The temporary `/tmp/whip-global-native-probe.mjs` passed **all five checks on the
first run**, using stock Electron with the staged production app and a fresh
fixture-only runtime/home/user-data directory. It exercised the real empty
workspace → New session → physical composer focus path, one real global IPC
preload, both app-global and OS-user-global candidates with project-only skill
excluded, warm filtering/Enter insertion with focus/caret, disabled Send, and
zero command submissions/session creations. No provider configuration or model
request was needed. Runtime hash matches the artifact above.

Report: `/tmp/whip-global-native-results/report.json`. Fixture daemon stopped and
fixture removed. Screenshots were captured, **not visually inspected**. This is
native staged Electron acceptance, not signed installed/notarized app acceptance.
The macOS native folder chooser was not automated; project transitions belong to
the shared-web and component tests rather than a faked native dialog.

### Actual shared-web acceptance

Final corrected-fixture run **passed all 14 acceptance groups in both Chromium
153.0.8010.12 and Firefox 155.0**, on the exact unchanged renderer above
(`job-9c1e6100`). Evidence: `/tmp/whip-global-web-results-fixed/report.json`,
per-browser transport frames and screenshots. Final script SHA256:
`3afa8aaf7289988394b38f00328e8b01bda4c5a65b9eb2554a757f86f85a9c19`.

Verified exact 100 globals across both isolated user roots, 200 combined names
after real project-picker selection, no project/cwd leakage, folderless no-provider
preload/insertion with zero create/send, fresh folderless draft isolation, and
existing-session keyboard/caret/send/scroll behavior. Genuine metadata responses
were delayed by 1.5 seconds; no candidate results were mocked. All six warm
typing windows (three composer scopes × two browsers) exceeded 11 seconds with
**zero extra completion RPCs, zero loading flashes, and correct next-frame
results**, including names beyond entry 64 before the 32-row display cap.

| Browser/scope | Cold response | Warm samples | Warm duration |
| --- | ---: | ---: | ---: |
| Chromium global New Chat | 1,523 ms | 69 | 11.004 s |
| Chromium project New Chat | 1,523 ms | 69 | 11.016 s |
| Chromium existing session | 1,526 ms | 68 | 11.023 s |
| Firefox global New Chat | 1,529 ms | 67 | 11.102 s |
| Firefox project New Chat | 1,519 ms | 67 | 11.033 s |
| Firefox existing session | 1,535 ms | 66 | 11.093 s |

Both browsers reported zero page errors and only `GET /v1/models`, no inference
requests. Turns use the existing synthetic fixture runner, not a model. Layout
assertions cover light/dark/narrow overflow and independent list scrolling;
screenshots were captured, not visually inspected.

The first attempt failed before browser launch because daemon test `TestMain`
overrides HOME: 99 app-global candidates were correct but the OS-global fixture
was seeded in the wrong isolated directory. That failed report remains in
`/tmp/whip-global-web-results/report.json`. The reviewed, script-only correction
sets TMPDIR to the owned fixture root, queries the host's actual home, and checks
canonical parent equality plus the test-directory prefix before any writes. It
also fails fast rather than repeating a failed scenario in another browser.
Exact catalog assertions stayed unchanged. The corrected run needed no product
changes or repack; native acceptance was not rerun because it already passed.

Product owner confirmed fixture project paths removed and no owned test processes
left; all parent/backend/frontend/reviewer jobs ended. Final review independently
checked native/web reports against the scope/count/hash claims and closed with no
concrete discrepancy. Final intentional-file whitespace checks passed. Nothing
was staged or committed; unrelated worktree edits remain preserved.
