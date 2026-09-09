# Server management simplification

Status: implemented and validated; native interactive and manual accessibility checks remain noted below.
Date: 2026-09-09.

## Goal and confirmed decisions

Use the supplied reference's simple header, server rows, overflow actions, and
focused Add server dialog, composed from WHIP's component library. Match the
information hierarchy, not the reference application's palette, oversized
controls, authentication fields, or sidebar structure.

Confirmed with the user:
- Settings → Servers is the main management surface. Rename the visible
  Connections category; management entry points navigate here.
- Each row shows name, quiet connection status, a muted address, and an overflow
  menu. Retain visible errors/progress when relevant.
- Add defaults to URL, with SSH available on desktop. Name is optional in the UI;
  advanced settings are collapsed. Add verifies, saves, and connects.
- Keep existing capabilities and safeguards; hide complexity rather than delete
  functionality.

## Research findings (pre-implementation)

- `packages/app/src/settings/connections.tsx` currently shows a read-only host
  list and a Manage execution hosts button.
- `packages/app/src/host-dialog.tsx` duplicates the list in a dialog and combines
  connection actions, an always-visible add/edit form, local runtime tools,
  identity acceptance, imports, and previous-tab recovery.
- The same HostDialog is called by Settings, the workspace shell, and welcome/
  new-session onboarding. Simplification must cover those callers, not just
  change the Settings screenshot.
- `packages/app/src/hosts.ts` owns host connections and save/remove behavior.
  URL profiles are revision-checked configuration on Local; native SSH profiles
  stay on the device. Saving a URL needs Local connected and profiles loaded.
- URL save verifies runtime identity, rejects duplicate daemon aliases, and
  preserves identity boundaries. Disconnecting a client is not stopping work.
- `packages/app/src/connections.ts` accepts HTTP(S)/WS(S), normalizes the daemon
  address, and rejects URL credentials, query strings, fragments, and proxy path
  prefixes. It requires a nonempty stored profile label/name.
- `packages/app/src/connection-dialog.tsx` already provides SSH fields, with
  username, port, identity file, remote executable, and remote home collapsed.
  SSH uses configuration/keys, not a password form.
- The UI library already supplies Button, IconButton, Menu, Dialog, AlertDialog,
  Field, Input, Switch, ToggleGroup, Collapsible, and StatusIndicator.
- `docs/frontend.md` is the current architecture/design authority. Existing
  historical plans are not implementation requirements.

## Proposed UI

### Servers page

```text
Servers                                      + Add server

┌───────────────────────────────────────────────────────┐
│ ● This Mac                          Connected      ⋯  │
│   This device                                         │
└───────────────────────────────────────────────────────┘
┌───────────────────────────────────────────────────────┐
│ ● Build server                      Disconnected   ⋯  │
│   https://build.example.com                           │
└───────────────────────────────────────────────────────┘
```

Use the real host name and connection state. The example is illustrative, not
new default server data. Native Local shows This device rather than its internal
transport endpoint; URL rows show their normalized address and SSH rows show
SSH · host/alias. Allow long addresses to wrap or truncate with accessible full
text without pushing the menu off-screen.

- One header and Add server action; no introductory paragraphs or duplicate
  Manage button on the happy path.
- Subtle bordered rows using shared panel/element tokens and existing Settings
  width/density. No statistics, filters, separate selected-server badges, or
  whole-row connect behavior.
- Status uses existing StatusIndicator with text mapped from the SDK's connected,
  connecting, reconnecting, incompatible, paused, and closed states (display
  closed as Disconnected); show host errors separately. Never signal state by
  color alone or confuse disconnected with daemon stopped.
- Preserve current host ordering and platform-specific Local naming.
- Contextual errors, profile-storage problems, and connection progress remain
  visible beside the affected server, not buried exclusively in an overflow menu.

### Row actions

Use the shared Menu with a labeled IconButton trigger (`Actions for <name>`).
Expose only applicable actions:
- Connect / Cancel connection / Disconnect, according to state.
- Edit server for supported profiles.
- Remove server for non-Local profiles. Explain that this removes the saved
  connection, not daemon sessions; preserve host-scoped tabs/drafts.
- Local server settings on platforms with local-runtime capabilities.

Keep Local non-removable. Keep asynchronous actions pending/error-aware and
prevent conflicting operations. Preserve existing connection-selection behavior
without converting rows into a global single-active-server switch.

### Add/Edit server dialog

Use shared Dialog, Field, Input, Switch, buttons, and a URL/SSH control only when
supported by the platform. Default to URL. Reuse one form for add and edit.

URL fields:
1. Server address — required; validate with the existing endpoint helper.
2. Server name (optional) — derive a stable display label from the normalized
   address when blank, retaining the port for disambiguation; persist a valid
   nonempty name through the existing save API.
3. Advanced → Connect when the app opens — preserve existing saved values and
   the current default of enabled for new URL profiles.

SSH fields:
1. SSH host or alias — required.
2. Server name (optional) — default to the host/alias.
3. Advanced → existing username, port, identity file, remote executable, and
   remote home overrides. Reuse current validation and SSH setup/host-key prompts.

Do not add URL username/password fields, invent a token field, or expose internal
profile IDs/runtime IDs as inputs. Do not offer unsupported SSH auto-connect
preferences or make native Local an arbitrary user-created remote profile.

- Cancel and Add server / Save changes in the footer. Show a truthful pending
  label while verification/save is underway; do not announce connected before
  the actual connection succeeds.
- Preserve save → select → connect semantics. A post-save connection failure
  remains visible on the server row; a validation/save failure leaves the form
  open with its entered values and error.
- Focus address/host first. Keep errors associated with their controls where
  possible; restore focus to the originating Add/Edit control on close.
- Explain the Local dependency contextually when URL saving is unavailable,
  without implying SSH saving has the same dependency.
- Retain explicit new-daemon-identity acceptance for applicable edits/imports,
  behind Advanced or a focused identity warning. Never automatically accept a
  replacement identity. Existing tabs retain their original runtime ownership.

### Local runtime and recovery

- Move the existing LocalRuntimePanel into a focused Local server settings dialog
  launched from the Local row menu. Reuse its test, choose executable, install,
  restart, diagnostics, and error behavior rather than rebuilding native effects.
- Test Connection still must not start the daemon. Connect retains its current
  start behavior. Restart retains its interruption warning and confirmation.
- Show a quiet contextual recovery/import disclosure only when legacy addresses,
  device profiles, or previous host tabs actually exist. Preserve import,
  restore/dismiss, individual-tab recovery, and identity checks.
- Do not expand this into a broader Recovery-page redesign.

## Implementation sequence

1. **Make Settings the single management surface.** Replace the Connections
   summary with the server list and actions. Change visible category/search/
   management copy to Servers. Keep the internal `connections` section key and
   `hosts` anchor initially so existing URLs and stored navigation continue to
   work; this is a copy/interaction change, not a route migration.
2. **Separate Add/Edit from management.** Refactor the existing HostDialog into
   the focused reusable form, and reuse SSHFields and controller methods.
   Implement optional-name fallback before calling the existing save API.
3. **Move secondary tools.** Relocate LocalRuntimePanel into its focused dialog;
   retain conditional migration/recovery access and all safety confirmations.
   Delete the redundant manager list and always-open form rather than keep two
   parallel implementations.
4. **Update every entry point.** Sidebar, disconnected-session management links,
   and Settings manage links navigate to Settings → Servers through existing
   navigation/unsaved-edit guards. Welcome/onboarding Add server opens the same
   focused Add dialog directly and preserves its selected-host callback.
5. **Validate and document.** Update targeted tests/selectors for labels and
   changed navigation, run the checks below, and update `docs/frontend.md` to
   describe the shipped management flow. Keep this proposal distinct from the
   canonical description until implementation exists.

Primary files: `settings/connections.tsx`, `host-dialog.tsx`,
`connection-dialog.tsx`, `settings/navigation.ts`, `settings.tsx`, `shell.tsx`,
`session-sidebar.tsx`, `session-tab-strip.tsx`, `welcome.tsx`, and related tests,
all under `packages/app/src` or `packages/app/test` as appropriate. Extract a
small product component only where needed for the separate local-settings dialog.
No new dependency, protocol schema, connection controller, global store, or
native bridge API is expected.

## Validation and acceptance

- Focused React tests: list/header, menu availability by state/platform, add/edit,
  URL and SSH field sets, optional-name fallback, Advanced persistence, error and
  pending behavior, focus return, and onboarding selection after add.
- Existing `hosts.test.ts`, `connections.test.ts`, `local-runtime.test.tsx`,
  `host-prompts.test.tsx`, settings navigation/host-selection/unsaved tests, and
  relevant bootstrap/multi-host tests remain passing.
- Exercise invalid/duplicate URL, Local offline, revision conflict, changed daemon
  identity, failed connect after save, SSH cancellation/host-key prompts, and
  reconnect without moving or deleting another host's tabs/drafts.
- Local actions: missing/stopped/outdated runtime states, failed install/test,
  non-starting test, and explicitly confirmed restart.
- Browser validation using existing settings and multiple-hosts scripts, adjusted
  for the new flow; desktop checks for native runtime and SSH paths.
- Run targeted `npm run test:web -- <test paths>`, then relevant broader web tests
  and `npm run check:web` during implementation.
- Inspect light/dark themes, narrow layouts, long names/addresses, keyboard-only
  interaction, screen-reader labels/status, contrast, zoom, and 44px coarse-pointer
  targets. Report which browser/device/accessibility checks were actually run.
- Happy path matches the reference's simplicity: a server list and Add action;
  no inline add form, diagnostics, migration explanation, or wall of buttons.

## Scope boundaries and assumptions

- Shared web/desktop renderer only; no React Native mobile redesign.
- Keep existing local server names and connection semantics; do not relabel This
  Mac globally or rename internal HostConnections/types just for visual copy.
- The user chose simplification by progressive disclosure, not feature removal.
- Runtime-backed status, identity safety, storage ownership, and recovery survive
  unchanged. Session/provider configuration is not being redesigned.
- This section records the original research-only scope. Implementation and
  validation followed approval, as recorded below.


## Implementation outcome — 2026-09-09

Implemented the approved hierarchy without new dependencies, backend changes,
protocol changes, or connection-state ownership. Settings now has one Servers
heading/Add action and ordered rows with textual status, muted address, visible
progress/errors, and applicable shared overflow-menu actions. The internal
`connections` section and `hosts` anchor remain compatible. Shell, sidebar,
command palette, disconnected-tab, and Settings recovery/configuration links
use that route and its existing unsaved-change guard. Welcome opens Add directly
and retains its save/selection callback.

The focused Add/Edit dialog defaults to URL, accepts an optional name derived
from the normalized host including non-default port, preserves connect-on-launch
and explicit replacement-identity consent in Advanced, and exposes SSH only on
capable desktop adapters. SSH uses its host/alias as the blank-name fallback and
keeps its override fields in a disclosure. Shared URL saves require Local's
loaded registry; SSH saves do not. Verification/save failures retain values;
success saves/selects/starts connection before closing, leaving post-save
connection failures on the row. In-flight save prevents double submission and
conflicting edits. Connection cancellation stays reachable during setup.

Removal now requires explicit confirmation explaining that daemon sessions,
host-owned tabs, and drafts remain intact. Local remains non-removable. Its
focused settings dialog reuses the existing runtime panel and late-result
retirement, non-starting probe, choose/install actions, restart warning, and
diagnostics. Existing conditional legacy imports and tab recovery are preserved.
Dialog focus returns to Add/the originating row menu trigger.

### Validation evidence

- `npm run test:web` — **45 files / 385 tests passed**, including 11 focused
  server-management UI tests consolidated in `server-manager.test.tsx`, scoped
  native-save cancellation tests, adapted replacement-identity/local-runtime tests,
  and existing hosts, bootstrap, connections, prompts, settings navigation,
  unsaved edits, host selection, workspace, and native-adapter regressions.
- `npm run check:web` and `npm run pack:web` — **passed** TypeScript, production
  renderer build, and embedded web packaging. Vite retains its non-fatal warning
  about chunks larger than 500 kB.
- `node apps/desktop/scripts/test.mjs` — **75 passed / 9 skipped / 0 failed**.
  The environment-gated native integration cases need a built Whip helper;
  this is not evidence of live SSH or packaged Electron UI acceptance.
- `WHIP_WEB_BROWSERS=chromium,firefox node apps/web/scripts/settings.mjs` —
  **10 workflows passed in each browser**, no runtime/CSP errors. Covers a
  single Servers heading, no inline address input, URL-first Add focus,
  keyboard Enter/Escape and return focus, browser SSH exclusion, 390px server
  and dialog reflow, plus existing Settings navigation, dirty guards, and theme
  checks. Scoped Axe WCAG 2/2.1 A/AA audits found no violations in the server
  list or Add dialog in light and Claude Code Dark themes.
- `WHIP_WEB_BROWSERS=chromium,firefox node apps/web/scripts/multiple-hosts.mjs` —
  **9 workflows passed in each browser** with three isolated real daemons.
  Verifies Add, independent simultaneous work/drafts, disconnect/reconnect,
  Local/remote outage isolation, confirmed remove/re-add with draft recovery,
  reload, and bounded root subscriptions.
- `node --check` passed for updated Settings/multi-host scripts (and the
  entrypoint subtask's other four scripts); scoped `git diff --check` passed.
- Intermediate failures were fixed: initial incomplete-file/token-name compile
  errors, old tests targeting the removed manager dialog, and one overly exact
  identity-switch accessible-name selector. Final commands above are green.

[Evidence directory](server-management-evidence/) contains current Chromium and
Firefox Settings manifests, a combined two-browser multi-host manifest, and
Chromium light/dark/narrow screenshots of both the server list and Add dialog.
The captured light, Claude Code Dark, and narrow layouts were visually reviewed.
No screenshots or logs from unrelated prior failed runs were copied. Fixtures
and browsers shut down through script `finally` blocks; no validation servers
remain running. Existing unrelated dirty work was preserved; nothing was staged
or committed.

### Remaining validation limitations

Automated browser assertions, Axe audits, and screenshot review are not a
manual screen-reader or physical-device audit. Live packaged Electron
startup/local installation and real SSH authentication/host-key acceptance were
not exercised interactively. Screen-reader speech, manual contrast assessment,
browser zoom, coarse-pointer 44px hitbox measurement, and a long-address stress
screenshot remain unverified. Shared components/tokens retain their existing
behavior; names/addresses wrap inside a min-width-zero row identity without
displacing its menu.
