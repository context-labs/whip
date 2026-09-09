# Conversation row actions

Branch: `codex/desktop-release` (shared checkout; no additional branch created)

Status: implemented and verified, 2026-09-08. Remaining native acceptance limits
are recorded below; release publication is separate.

## Goal and scope

Bring the useful actions from the supplied sidebar screenshot to Whip's saved
conversation rows. The overflow button and right-click menu should offer the
same actions for the row being acted on, independently of the selected chat.
Use the shared web/desktop renderer, existing Base UI controls, and StyleX themes.

Confirmed decisions:

- Include **Open in → Cursor / VS Code / Zed / Finder**, **Rename**, **Fork**,
  **Archive**, and **Delete**.
- Fork into the **same working directory**. No Git branch or worktree creation.
- Include local folders and remote SSH editor opening in the first complete version.
- Exclude cloud execution, pinning controls, mark as unread, custom groups, and
  opening a conversation in a separate Whip window.
- Retain the existing **Open in background tab** action. It opens within Whip's
  current window. Existing directory grouping and split-pane controls stay as they are.
- Apply the shared conversation actions to sidebar and search-result rows.
  Reuse the same implementation for overlapping Session details actions; tab
  movement and closing remain tab-specific controls.
- Native Expo menu parity, bulk selection, new global keyboard shortcuts,
  arbitrary editor command templates, and transcript export are outside this work.

The user approved the one-way upgrade from the current beta database. Preserve
existing sessions, runtime identity, configuration, and command state. Older
binaries do not support the new schema; no destructive reset is part of this work.

## Proposed interaction

```text
Open in                  ›  Cursor
                            VS Code
                            Zed
                            ─────────
                            Finder
Open in background tab
────────────────────────
Rename…
Fork
────────────────────────
Archive
Delete…
```

For an archived row, **Restore** replaces **Archive**. Delete is the only red
action. Use the existing compact menu spacing, theme surfaces, hover treatment,
focus behavior, and submenu chevron. Do not display shortcut labels unless the
corresponding shortcut actually exists; Base UI's menu navigation/typeahead is enough.

Opening a menu must not select the conversation or mount its transcript. Keep
the overflow button visible while its menu is open, including when the pointer
moves into the submenu. Keyboard and touch users can use the overflow button.
Submenus must flip at viewport edges and work within the narrow sidebar Sheet.

### Action behavior

| Action | Behavior |
| --- | --- |
| Open in | Open the conversation's exact working directory in the chosen local editor, or connect that editor to the corresponding SSH folder. This does not transfer Whip's chat into another agent. |
| Finder | Open the working directory on This Mac. Not available for a remote directory; do not interpret a remote path as a local folder. |
| Rename | Small dialog with the full existing title selected; Enter saves, Escape cancels. Apply the persisted result to every view of that runtime/root, without switching the active chat. |
| Fork | Create a separate root from the committed root conversation and inherited session settings in the same directory. Use a recognizable fork title and open the result in a new Whip tab if the invocation is still current. |
| Archive | Hide from the main saved-session list, retain history and configuration, and offer Undo/Restore. Open tabs, drafts, schedules, and running work are preserved. |
| Delete | Confirm the conversation title and execution host. Delete the root and its owned runtime tree using the existing daemon command. This can stop running work; it does not delete the project's files. |

Fork copies committed history, not an unfinished response, unsent draft,
live child processes, pending decisions, or current execution state. Source and
fork remain independent conversations but share the same files. A busy source
can continue; label the history boundary accurately. Reuse the existing history
revision check and report a conflict rather than silently choosing a new boundary.
Check the 32-tab limit before creating the fork. If creation succeeds but opening
the tab fails or the user navigates elsewhere, retain a link to the created chat
in the existing command result notice; do not submit another fork.

Archive is a presentation state, not a runtime status. It does not stop work or
suppress pending questions/permissions in Attention. Receiving output or opening
an archived tab does not silently restore it. A fork of an archived session starts
unarchived. Add **Archived sessions** access from the sidebar using the existing
search dialog with Active / Archived / All filtering and per-host pagination.
The archived view offers Open, Restore, Rename, Fork, Open in, and Delete.
There is no automatic retention-based deletion introduced by archive.

Delete confirmation includes any known unsent draft or active-work warning.
Keep the row and tabs until the daemon confirms completion. After confirmation,
remove every matching runtime/root view from local panes and reopen history,
release its drafts/attachments/reading state, and choose a neighboring tab or
New session. Other hosts and roots must remain intact. Other attached clients
must reconcile authoritative deletion without trying to recreate the session.
Absence from the active catalog is not proof of deletion: it may mean archived,
filtered, paged out, or temporarily unavailable.

## What is already implemented

| Existing foundation | Consequence for this work |
| --- | --- |
| [Sidebar rows](../../../packages/app/src/session-sidebar.tsx) and [search results](../../../packages/app/src/session-search-dialog.tsx) already share menu items between overflow and right-click | Extend these action definitions; do not build another row interaction system. |
| [Session details](../../../packages/app/src/conversation.tsx) already implements rename, fork, and delete | Extract the overlapping action/dialog behavior instead of copying it into every row. Keep history-specific controls in the conversation. |
| [SDK Session](../../../packages/sdk/src/session.ts) has `rename`, `fork`, and `delete` | Use command handles and `runtime.run`; no custom HTTP or WebSocket code. |
| [Daemon commands](../../../internal/daemon/client_control.go) and [store fork](../../../internal/session/session.go) already preserve fork history/settings | Keep their ownership and semantics. Use the existing fork-title helper where applicable. |
| [Catalog pages](../../../internal/session/catalog_page.go) truncate titles and paths for display | Never rename from a truncated title or launch an editor using the displayed path. Read authoritative metadata on demand. |
| [SessionListView](../../../packages/sdk/src/state.ts) observes catalog revisions and completed commands | Archive/restore must advance the catalog revision, and existing views should refresh through this path. Do not introduce another list poller. |
| [Menu primitives](../../../packages/ui/src/overlays.tsx) currently support flat actions | Extend this small wrapper with Base UI submenus for both Menu and ContextMenu. |
| [Native effects](../../../apps/desktop/src/native.ts) only allow HTTP(S)/mailto external links | Add a dedicated, typed project-opening capability instead of weakening the generic URL allowlist. |
| [Connection profiles](../../../packages/app/src/connections.ts) distinguish local, URL, and SSH hosts | Resolve editor targets from the row's verified host, never the currently selected host. |

No archive field or archive operation exists in the current session metadata,
catalog, or SDK. This is the main new backend feature. The current store is schema
10 and [its opening policy](../../../internal/session/migrations.go) rejects older
schemas rather than migrating them. Archive needs an explicit storage decision.

## Architecture

### Shared action controller and metadata

Add a focused `session-actions.tsx` controller/dialog host in `packages/app`.
Mount it outside virtualized rows so scrolling, rename-induced reordering, and
archive removal cannot discard an open dialog or its pending result. Rows supply
`runtimeId`, `rootId`, their source client/profile, and a focus-return target.
Keep one active dialog target, not a separate dialog and subscription per row.

Reuse shared command notices for progress, failure, and uncertain delivery.
Capture the source client/runtime identity and invocation generation. A host
replacement, newer action, or navigation change must not redirect a late result
into another chat. A disconnected host disables daemon mutations with a useful
reason. A local editor can open from previously verified full metadata when safe;
otherwise report that the host must reconnect to resolve its folder.

Add one bounded metadata query, tentatively `sessions.get({root_id})`, returning
the exact title, cwd, archive state, and history revision. It reads only the
session bookkeeping row, not transcripts or active roots. Keep the result within
a fixed byte limit and return an explicit error for oversized fields rather
than silently truncating actionable data. Fetch only for an invoked action or
opened submenu/dialog, with cancellation and host-scoped query keys. Do not
hydrate every row or use `root.snapshot` merely to rename a background chat.

The Go registry owns the query and archive command definitions. Regenerate
`packages/protocol`; add typed SDK helpers. Existing command admission,
deduplication, scope validation, and recovery remain authoritative.

### Native project opening

Add an optional `AppPlatform` capability for listing fixed editor targets and
opening a project. Carry it through `desktop-bridge.ts`, preload, the web desktop
adapter, and Electron main. Keep discovery and native launch code in a small
`apps/desktop/src/project-open.ts` module. No new npm dependency is expected.

The renderer supplies a fixed application ID, verified connection reference, and
bounded full cwd. Native code revalidates them and derives executable/arguments
from its fixed app registry. It must not accept arbitrary commands, executable
paths, shell fragments, or unchecked custom URL schemes.

- Discover Cursor, VS Code, and Zed from their installed application bundles;
  Finder is the local built-in target. Do not rely solely on shell PATH: Finder
  launches may have a different environment. Missing apps stay disabled with an
  installation explanation; do not install editors or extensions automatically.
- Local: validate an absolute existing directory, then use the editor's launcher
  with an argument array. Finder can use Electron's `shell.openPath` on the
  validated directory; handle its nonempty error string as failure.
- SSH: launch the local editor into the exact remote folder using its SSH
  integration. The editor owns authentication prompts, host-key confirmation,
  and its own server/extension setup. Whip does not send transcripts or provider
  credentials to the editor.
- Prefer the user's existing OpenSSH alias so IdentityFile, ProxyJump, user, and
  port remain consistent. If a Whip profile has overrides that an editor cannot
  represent, explain the required SSH alias setup; never silently drop options.
- A URL/Tailscale backend has no implied SSH identity. Provide **Configure SSH
  for editors…** when needed and store the explicit alias as a viewing-device
  preference bound to the verified runtime ID. This must not change the daemon's
  transport or create a duplicate Whip connection. For example, a user may map
  their URL-connected 4090 host to their existing `gpu-4090-sam` alias.
- In a normal web browser, omit unsupported native launch targets and offer
  **Copy working directory** as a useful fallback. The browser's daemon host is
  not necessarily the user's computer. No localhost inference or custom-scheme
  bypass is needed.

Launch success means the editor accepted the open request; it does not prove
that SSH authentication finished. Errors should distinguish a missing editor,
missing local directory, unavailable source host, unconfigured SSH alias, and
failed launcher. Paths containing spaces, quotes, Unicode, `#`, `%`, and shell
metacharacters must remain literal arguments or correctly encoded URL components.

### Durable archive state

Add a first-class `archived` boolean to session metadata and catalog records,
default false. Do not encode it as a tag or localStorage blacklist. Add a durable
`session.archive({archived: boolean})` command for archive and restore, with a
typed result and normal command-ID recovery. Persist the value before reporting
success and emit the metadata event used to update any open session view.

Extend `sessions.list` with `status: active | archived | all`, default active.
Apply the predicate in SQL before pagination and include status in cursor
validation. Archive/restore must bump the catalog revision. Preserve the current
list/page byte limits and reset paging when the filter changes. Include archive
state in targeted tab summaries so an archived open tab is not treated as missing.

The main SDK catalog remains the active list. Archived/all search uses the
existing bounded, on-demand Query flow; there is no second always-on catalog.
Attention and explicit root reads include archived sessions. Update affected
SDK, web, and mobile consumers of catalog defaults so they do not misinterpret
hidden archived sessions as deleted. Native mobile menu expansion remains out
of scope; existing tabs and session access must continue to work.

Add the approved transactional one-way migration from the immediately preceding
supported schema, preserving runtime identity, sessions, history, configuration,
and command state. Update the catalog trigger and schema identity together.
Test failure rollback and restart. Do not add downgrade support or legacy-client
fallbacks. Never reset the user's live database automatically. Desktop/backend
release compatibility metadata must reflect the chosen schema change.

## Phased implementation

### 1. Shared action and metadata foundation

- [x] Introduce the bounded session metadata query and generated SDK method.
- [x] Add one reusable submenu representation to the existing Menu/ContextMenu
  wrappers, using Base UI's native submenu/focus primitives.
- [x] Add the shared app action target and dialog host outside row virtualization.
- [x] Wire identical action definitions into sidebar and search rows.

Exit: menus work on a background conversation without changing selection,
opening a transcript subscription, or losing their target when the list reorders.

### 2. Rename, fork, and delete

- [x] Move the existing overlapping Session details actions into the shared flow.
- [x] Implement full-title rename, revision-bound same-directory fork, and explicit
  delete confirmation using existing daemon commands.
- [x] Reconcile successful changes across tabs, sidebar, search, and all matching
  local state; retain actionable command outcomes when delivery is uncertain.
- [x] Preserve current conversation-only history and inspection controls.

Exit: these actions work from inactive rows on local and remote hosts, including
duplicate views, active work, tab-cap limits, and host disconnect/replacement.

### 3. Open in local and remote editors

- [x] Add the typed native capability, installed-app discovery, and fixed launchers.
- [x] Implement local Cursor/VS Code/Zed/Finder opening with full cwd validation.
- [x] Implement editor SSH opening and explicit URL-host-to-SSH-alias preferences.
- [x] Provide missing-app/setup errors, appropriate disabled items, and web fallback.
- [x] Verify actual local folder opening in installed Cursor, VS Code, and Zed;
  verify Cursor and VS Code remote handoff and authentication independently.
- [ ] Complete remote server setup/folder browsing in all three editors and
  actual Zed SSH acceptance. See the native QA record for the isolation limit.

Exit: each supported editor opens the correct folder/host from the row that was
clicked, including when another host is currently selected.

### 4. Archive and restore

- [x] Resolve the database upgrade decision before modifying the store policy.
- [x] Add persisted archive state, command, metadata event, and catalog filtering.
- [x] Add the Archived sessions entry and reusable search filter/restore actions.
- [x] Implement Undo as a restore command and retain archived open tabs and work.
- [x] Verify catalog defaults, Attention, restart, and cross-client convergence.

Exit: archive survives daemon restart, can be reversed from another connected
client, and never cancels or silently deletes work.

### 5. Acceptance and documentation

- [x] Run the focused checks below and required repository checks.
- [x] Inspect actual light/dark, long-title, narrow, submenu-edge and touch output.
- [x] Validate the production renderer and real sandboxed Electron preload/main
  bridge in an isolated app, plus installed editor launches and an isolated SSH
  authentication fixture. Signed installed-app acceptance belongs to release QA;
  remote browsing and Zed SSH remain unverified as recorded below.
- [x] Update `docs/frontend.md` for action ownership, archive semantics and native
  capability; `packages/ui/README.md` for submenus; `docs/desktop.md` for editor
  setup; and `docs/features.md`/`docs/roadmap.md` for shipped behavior and evidence.
- [x] Record source/build IDs and any unmet native QA in this plan. Publish/rebuild
  through the normal release path when release execution is requested.

## Validation

| Layer | Meaningful checks |
| --- | --- |
| Store/daemon | Archive/restore persistence, catalog revision and filter-bound cursors, paging and search, archive during active work, fork history boundary, deletion cleanup, supported migration/rollback if approved. Use Go race tests for changed daemon/store concurrency. |
| Protocol/SDK | Generated contract drift, exact runtime/root targeting, command deduplication and uncertain outcome recovery, catalog refresh, snapshot/event archive consistency, archive versus authoritative deletion. |
| App | Row action never operates on the selected chat accidentally; virtualization/dialog lifetime; full title/path despite truncated catalog; duplicates across panes; late results after host replacement; no automatic fork retry; tabs/drafts preserved or removed according to the action. |
| UI | Overflow/right-click parity, submenu keyboard traversal and focus return, no unexpected navigation, disabled/destructive/pending states, touch targets, edge flipping, theme contrast and strict CSP. |
| Native | Fixed executable registry and argument encoding; missing apps/directories; Finder-launched PATH; local versus remote resolution; no password/key material in bridge logs; explicit alias setup; actual local and SSH editor opens. |
| End to end | Two clients on one daemon: rename, fork, archive, restore, delete; disconnect during mutations; restart and restoration; archived requests still actionable; URL-connected remote plus separate editor SSH alias. |

Use `npm run check:web`, `npm run test:web`, UI type/unit checks and affected
browser/CSP fixtures, `npm run check:desktop`, native project-open tests, generated
protocol/SDK checks, and `task check` before declaring implementation complete.
Use isolated fixture homes and SSH hosts; acceptance should not stop or reset
the developer's real daemon.

## Research and references

- The supplied screenshot is the menu/interaction reference. The confirmed scope
  above deliberately excludes its cloud, pin, unread, group, and new-window items.
- Existing Whip behavior is grounded in the source links above, not older plan
  requirements. The [frontend guide](../../../docs/frontend.md) remains canonical.
- The [existing OpenCode research](../../../docs/learnings/other-harnesses/opencode/opencode-ux.md)
  records rename, fork, and session-list actions. Whip keeps its own daemon command
  semantics and confirmation rules rather than copying another runtime's behavior.
- Base UI supports accessible nested [Menu](https://base-ui.com/react/components/menu)
  and [Context Menu](https://base-ui.com/react/components/context-menu) components.
  Use the installed library's submenu primitives; no additional menu dependency.
- VS Code documents folder launch and `--remote` SSH authorities in its
  [CLI reference](https://code.visualstudio.com/docs/configure/command-line), with
  prerequisites in [Remote SSH](https://code.visualstudio.com/docs/remote/ssh).
- Zed documents local/SSH URLs in its [CLI reference](https://zed.dev/docs/reference/cli)
  and SSH alias/config behavior in [Remote Development](https://zed.dev/docs/remote-development).
- Cursor documents remote support in [network and remote troubleshooting](https://prod.cursor.com/help/troubleshooting/network).
  Local inspection of Cursor 3.19.13 confirmed its folder launcher and parsed
  `remote` / `folder-uri` options in `Contents/Resources/app/out/cli.js`. This is
  evidence for an adapter candidate, not proof of a completed SSH launch.
- Electron's [shell API](https://www.electronjs.org/docs/latest/api/shell) documents
  directory opening and its error-return behavior. Keep editor launching separate
  from Whip's existing HTTP(S)/mailto link capability.

## Implementation record — 2026-09-08

Implemented on `codex/desktop-release` from `2cea57c23b138284974226e9175d8bf1d8c9ba97`,
preserving the pre-existing staged changes. No release was published and no live
user database or running daemon was used as a fixture.

- Archive introduces schema 11. The approved transactional v10 upgrade preserves
  sessions and runtime identity; rollback/retry and reopen are tested using the
  checked-in v10 fixture. The concurrently developed last-turn feature advances
  the combined checkout to schema 12, chaining v10→v11→v12. Existing schema 11
  stores run only the latter migration. Unsupported stores remain rejected.
- Required archive metadata and normalized catalog cursors advance the wire
  contract to protocol 5.0. Mixed-major clients fail during initialization;
  clients and the daemon must be updated together. `/api/v3/ws` remains the
  transport path. There is no older-client fallback or downgrade support.
- `sessions.get` returns exact title/cwd, history revision and archive state with
  a 64 KiB wire/allocation bound and no runtime-root hydration.
- `session.archive` uses durable command recovery, metadata events and the existing
  catalog revision. Archive and restore work during a turn. New forks use the
  existing fork title helper and start unarchived.
- A single app provider owns actions above virtual rows. It targets original
  client/runtime/root identities, preserves background navigation, and cleans up
  every local view/draft for a confirmed deletion. Both sidebar and search use
  Base UI submenus; Session details delegates its overlapping actions.
- Archived/all search reuses catalog revision observation; another device's
  mutations refresh only the affected host and discard its stale search cursor.
- Native editor launchers use fixed executables and argument arrays, full paths,
  and verified runtime connections. URL profiles retain their original renderer
  WebSocket transport and do not consume native connection slots.

Validation completed:

- `task check`: all Go format/vet/custom-vet/unit checks, generated contract drift,
  SDK checks, shared UI checks, production renderer and 321 web tests passed.
- `go test -race ./internal/session ./internal/protocol ./internal/daemon`: passed.
- SDK: 264 tests; generated protocol interoperability: 11 tests.
- Mobile: typecheck and 145 tests passed; catalog hiding does not invalidate an
  explicit mobile session route or its saved draft.
- `apps/web/scripts/session-actions.mjs`: actual Chromium and Firefox against an
  isolated daemon, full metadata without transcript hydration, inactive rename,
  keyboard/context submenus, search and Session details dialog nesting, same-directory fork,
  archive retaining an open draft, cross-client restore/search refresh, deletion
  cleanup, light/dark/narrow layouts and strict CSP all passed. Screenshots and
  results are under `/tmp/whip-session-action-results`.
- Adversarial review found and resolved background-delete navigation and stale
  cross-client search results. Final review found no remaining blockers.

The final production renderer is
`be7376fa02f71e74262ef2ffc167a7dde4a9b4d723d15bbc0f1fab065affa75d`
(23 files), packed into Go embed and used by both browser acceptance engines and
the real Electron bridge fixture on protocol 5 / schema 12. Desktop typechecking,
native project-opening tests (71 passed, 9 existing SSH-dependent skips), and
desktop adapter tests passed. The final bridge fixture verifies discovery,
runtime mismatch and missing-directory errors, URL-host SSH requirements, and
cancellation when connections are released, without launching external apps.

Native evidence is in [the editor acceptance record](native-editor-acceptance.json)
and [the current IPC record](native-ipc-smoke.json). Installed Cursor, VS Code,
and Zed opened the exact local folder containing spaces, `#`, `%`, and Unicode.
Cursor and VS Code accepted the SSH handoff and authenticated to the disposable
server; that server deliberately refused editor installation, so completed
remote browsing is not claimed. Actual Zed SSH acceptance remains unverified
because its macOS singleton prevented a separate fixture without disturbing the
user's open editor. The earlier Finder launch is recorded separately in
[the prior IPC run](native-ipc-smoke-protocol4.json).

The first editor smoke harness removed a temporary HOME before a detached Cursor
process exited, causing missing-keychain prompts. The fixture process was stopped;
the user's normal Cursor session and login keychain were preserved. The harness
now requires explicit opt-in and checks fixture-process exit before cleanup.
Its attempted Cursor keychain suppression has not been verified, and no further
editor launches were used for final acceptance.

The real installed app and daemon have not been replaced by this feature task.
Clients and the backend need a coordinated build/update for protocol 5. Signed
installed-app acceptance and release publication remain outside this change.
