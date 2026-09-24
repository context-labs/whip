# Desktop first-launch setup

Branch: `whip-rlm` (research on the current working tree; no implementation branch created)
Status: Proposed — research and plan only, pending product approval.
Date: 2026-09-23

## Goal

A fresh macOS DMG installation should open to a calm, useful setup screen, not a connection error. The common path requires one explicit action, no configuration decisions, and no terminal. All optional local-runtime configuration lives in one **Advanced** disclosure, initially closed.

An unconfigured computer is an expected product state. An installed computer that cannot connect is a different state and must retain truthful recovery UI.

## Non-goals

- No new multi-step onboarding wizard, installer subsystem, dependencies, account requirement, or persistent “onboarding complete” flag.
- No silent installation before consent, automatic replacement of external CLI installations, automatic destructive restart, provider login, or first-message submission.
- No new state-directory, network, launch-at-login, update-channel, or permissions settings. Preserve existing defaults and user choices.
- No redesign of remote setup, the provider chooser, or the whole desktop shell.
- No application code changes in this research phase.

## Research and diagnosis

Sources below refer to the current checkout, which already has substantial unrelated uncommitted changes. The released DMG's exact version/commit was not supplied; this is a source trace consistent with the screenshots, not a reproduction of that binary.

### Why the first screen looks broken

1. `packages/app/src/hosts.ts:184–190` automatically connects Local at launch.
2. `apps/desktop/src/runtime.ts:227–234,276–279` already returns a structured `missing` result when there is no executable. However, `prepare()` turns that expected condition into an ordinary exception (`:445–467`).
3. `packages/app/src/hosts.ts:142–181` stores preparation failures as generic host errors. `packages/app/src/connection-notice.tsx:11–36` renders a connection-error notice, and `shell.tsx:117` renders those notices above the workspace.
4. The initial `/` route renders `EmptyWorkspace`, which does not inspect local setup readiness (`packages/app/src/routes/index.tsx:1–4`, `empty-workspace.tsx:17–55`). Its first-run hint depends on catalogs from hosts that have already answered; an absent runtime cannot satisfy that condition.
5. Only after New session does `welcome.tsx:53–65` render `LocalRuntimeSetup` for a disconnected local host. This creates the two-stage experience in the screenshots.
6. `host-dialog.tsx:318–367` treats any connection error as `unavailable`, independently of its separately probed `missing` status. Thus Set up this Mac and Retry connection can appear together. Advanced options also contains another Runtime diagnostics disclosure.

This is not primarily a missing-defaults problem. `LocalRuntimePanel` already calls `installDefault()` and then connects when installation returns stopped/running (`host-dialog.tsx:295–309`). The fix is to make setup readiness a shared, truthful input to startup, connection notices, and the setup panel.

### Existing defaults to keep

| Concern | Current implementation / recommendation |
| --- | --- |
| Runtime payload | Use the verified executable bundled in the app, not a separate download or Homebrew requirement. `apps/desktop/src/main.ts:84–93`; `runtime.ts:310–340`. |
| Install location | Fresh default is `~/.local/bin/whipcode`; no path chooser or sudo in the normal path. `runtime.ts:349–358`. |
| State/configuration | Default `~/.whipcode`; respect an inherited `WHIPCODE_HOME`. This is not currently an editable GUI setting. `runtime.ts:152–160`; `docs/desktop.md:90–97`. |
| Existing installation | Discover/reuse the selected or available compatible CLI; preserve its home, configuration, provider credentials, and ownership. Saved missing selections do not silently fall back. `runtime.ts:178–206`. |
| Daemon lifecycle | Install and Test are read-only with respect to starting the daemon; normal connection starts a stopped daemon or attaches to the running one. Closing the GUI does not stop its work. `runtime.ts:445–467`; `docs/desktop.md:194–198`. |
| Network | Current working-tree default is local socket only, with explicit gateway/listener settings preserved. Do not reintroduce the old implicit TCP listener. `runtime.ts:152–160`. |
| Safety | Preserve payload verification, refusal to overwrite different bytes, native mutation bounds, restart confirmation, and managed/external update distinctions. |

Do not describe the daemon lifecycle as a login service. Installing the helper at a conventional CLI path also does not prove that path is on every shell's PATH.

### Prior art and constraints in this repository

- `docs/frontend.md`, especially Product intent, Runtime construction, Error ownership, and Empty workspace: calm theme-native UI, one owner per state, one canonical error display, preserve drafts and navigation.
- `docs/features.md:351–408`: provider readiness and the first message already have their own integrated flow. Reuse it after machine setup; do not combine local installation and provider credentials in Advanced.
- `docs/roadmap.md:186–209`: desktop release acceptance remains open; canonical executable integration is already shipped. This work improves its presentation, not a replacement installation architecture.
- `docs/learnings/` search did not identify a directly applicable desktop installation-onboarding study; existing canonical installation and provider-onboarding records are the relevant project prior art.
- `docs/frontend.md:2157–2180` deliberately specifies no automatically seeded draft on bare first launch. Retain that behavior until an explicit action; update the guide to document the unconfigured-Mac exception to the generic frontdoor content.
- Existing startup measurement checks a connected, preconfigured frontdoor, not an untouched DMG install. Fresh-install acceptance must be separate, not a weakening of that measurement.

No external product comparison or clean-machine runtime test was performed in this research pass.

## Proposed experience

### First launch: one setup surface

On the initial workspace, once read-only detection confirms there is no local installation:

```text
Set up Whip on this Mac

Whip needs a local service to run your coding sessions.
We’ll install it with the recommended settings.

[ Set up this Mac ]

› Advanced
```

- No red connection banner, warning icon, Retry connection, unavailable-build placeholders, or diagnostics in this state.
- One heading; do not put “What do you want to work on?” above a second setup heading.
- No fake session/tab just to host setup. Reuse the setup content in the empty workspace and in local New Chat views.
- Keep normal shell navigation available. Label the local sidebar action **Set up this Mac** when appropriate and route it to this same surface rather than retrying a connection that cannot exist yet.
- Remote setup remains accessible through Servers/Settings (and existing host selection in New Chat); it must not require local installation. Do not add an equally prominent second choice to the fresh local setup screen.
- Gate only local setup-dependent surfaces. A restored remote session, Settings, explicit deep link, or an already connected remote workspace must not be replaced by a global onboarding modal.

### One action, then continue

Clicking **Set up this Mac**:

1. Installs the verified bundled executable at the default location.
2. Connects through the existing host connection owner, starting the daemon only if needed.
3. Continues to a usable New Chat once the SDK connection is verified, not merely once the file exists.

Show factual phase text: **Setting up this Mac…**, then **Connecting…**. Disable duplicate setup actions while busy. Do not invent percentage progress or duration promises.

For setup started from `/`, use the existing `openNewChat` path after success, provided the user is still on that setup surface. For setup started from a draft, keep that draft, folder, and host selection. Navigation away must prevent late completion from creating a tab or redirecting another host. Re-observe completed native effects when returning; do not claim an installation was rolled back because a component unmounted.

Then reuse existing provider/project readiness: prompt for a provider only when required; preserve detected credentials and saved defaults. Machine setup alone does not mean a user can send a model request.

### Exactly one Advanced configuration disclosure

Initially closed whenever a fresh setup surface is entered; no global persisted “expanded” preference needed. Opening it reveals ordinary labels and rows, not nested configuration accordions:

- **Use an existing installation** — existing native executable chooser.
- **Install location** — show the recommended/effective destination and allow the existing custom-location installation flow.
- **Check installation** — read-only native test (make clear it does not start work).
- **Diagnostics** — plain labeled values for installation path, data folder, checked status, and meaningful build information. Use “Not installed yet” rather than a wall of “Unavailable” on a new Mac; omit fields that do not exist yet.
- **Restart local service** — only for relevant installed states, retaining the explicit interruption confirmation. Not visible on a genuinely fresh machine.

Clarify that the existing custom-location action performs installation through a native chooser; do not turn it into a fake editable preference without implementation. The main action always reflects the actual selected/default target.

Technical details for a real failure may use the standard ErrorNotice disclosure. That is an error-detail affordance, not a second Advanced configuration section. Never hide the primary failure summary or its retry action inside Advanced.

### State and recovery behavior

| Observed condition | User experience |
| --- | --- |
| Initial detection unresolved | Neutral “Checking this Mac…”; no transient red banner, no premature install CTA. |
| Missing executable, no selected path | The simple setup surface. This is not an error. |
| Missing previously selected path | Explain that the selected installation is no longer available; offer repair at that path or selection of an existing executable. Never silently replace it with the default path. |
| Compatible runtime stopped/running | Connect/attach automatically as today; do not show first-install onboarding or reinstall. |
| Installing / connecting | Phase text and one disabled busy action. |
| Verified connected | Continue intended flow; clear obsolete setup/error state. |
| Native inspection fails | An actual check failure with Retry check; do not assume “missing.” |
| Installation fails | Inline action error next to Set up/Retry setup; retain choices. Advanced can provide an alternate path, but is not necessary to discover why setup failed. |
| Connection fails after installation | Existing canonical host error plus one relevant Retry connection action. Do not install again or duplicate the full error in the setup panel. |
| Incompatible/unhealthy runtime | Truthful recovery, not first-run copy. Preserve existing executable and restart/replace safeguards. |
| Packaged installer unavailable (`canInstall: false`) | Do not show a dead default-install CTA; explain the limitation and offer the supported existing-installation path. |

## Visual direction

Use the existing Whip design system, not a new branded wizard. The person is a developer opening an unfamiliar app and wants to get to a coding session, not administer a server.

- Domain: this Mac, local service, project folder, coding session, installed CLI, connected host.
- Color world: existing charcoal canvas, graphite panel, fine gray separators, bright primary text, muted secondary text, theme focus accent; warning/red reserved for actual failures. These are semantic theme roles, not new fixed colors.
- Product-specific interaction: one explicit setup action prepares the same local runtime shared with the CLI, then hands off to the existing coding-session composer. Local identity stays visible through setup, progress, Advanced, sidebar action, and the resulting session.
- Replace the usual multi-step wizard with one in-workspace surface; replace the settings checklist with recommended defaults; replace a status dashboard with one phase message and one disclosure.
- A compact left-aligned column within the current workspace, existing heading/body/button tokens, comfortable spacing, no extra card or illustration. Keep the CTA and Advanced visible at the minimum supported window size.
- Keyboard-reachable disclosure and actions; proper heading structure, `aria-expanded`, live status for phase changes, visible focus, and focus continuity on successful transition. Test light/dark themes, zoom, long paths, and narrow windows.

## Implementation design

### Ownership and minimum change

Native code remains authoritative for installation facts and mutations. The existing app host connection owner should retain the latest local-runtime inspection used to decide whether connection can proceed. UI components consume that snapshot rather than independently guessing from connection-error text.

Recommended first implementation:

1. Add a small optional native-local readiness/inspection record to the app host snapshot (not the SDK `ConnectionState`). Derive needs-setup from the existing `LocalRuntimeStatus` and `executable`; distinguish checking and failed inspection explicitly.
2. In the existing local connection path, inspect before preparing the transport. A confirmed fresh `missing` result settles into setup-needed without storing a host connection error or attempting a handshake. Explicitly reject connection as setup-required where callers require a rejected promise; do not resolve a connection promise as though connected.
3. Reuse that inspection in LocalRuntimeSetup, empty workspace, and the sidebar. Publish returned native mutation results back to the owner and recheck as needed. Avoid duplicate component probes, unbounded polling, and a second persisted onboarding state.
4. Use existing attempt/cancellation/lifetime guards: late inspection must not overwrite newer connection success, a changed executable, or a retired attempt. Deduplicate probes and native mutations across visible setup entry points. Keep remote startup independent.
5. Do not suppress all local errors while unconnected, match localized error strings, or depend on whether the setup component happens to be mounted. A typed expected outcome must prevent the initial error from being created.
6. Preserve native preparation's last-minute validation. If a disappearing executable between inspection and preparation needs an IPC discriminator, add a serialized setup-required result and map it at the desktop adapter; do not depend on custom Error properties surviving Electron IPC. Prefer the existing status contract where sufficient. A genuine race failure must never become a silent success.

The SDK need not learn about desktop installation. No protocol/Go changes are anticipated. The exact app-local outcome shape should be proven with host tests before broad UI edits.

### Expected files

- `packages/app/src/hosts.ts`, `platform.ts` as needed: shared inspection/readiness and expected setup-required semantics.
- `packages/app/src/empty-workspace.tsx`, `welcome.tsx`, `host-dialog.tsx`: initial setup surface, single Advanced, contextual primary action, guarded successful continuation.
- `packages/app/src/session-sidebar.tsx`, `connection-notice.tsx`: consistent setup label/status; preserve canonical real errors. Inspect `shell.tsx` consumers without adding a global blocking gate.
- `apps/web/src/platform/desktop.ts`: only if adapter mapping/status propagation is required.
- `apps/desktop/src/runtime.ts`, `main.ts`, `preload.ts`, `packages/app/src/desktop-bridge.ts`: only if a serialized native result is actually needed; preserve installer defaults and safety behavior.
- Existing corresponding tests, `apps/desktop/scripts/onboarding-smoke.mjs`, and potentially startup probe fixtures for a distinct fresh-setup acceptance path.
- Docs: `docs/frontend.md`, `docs/desktop.md`, `docs/features.md`; link the milestone in `docs/roadmap.md` without marking broader release acceptance complete.

## Validation and release acceptance

Existing useful coverage: `packages/app/test/local-runtime.test.tsx:113–168` covers default install, retry, cancellation, and reuse. `apps/desktop/tests/runtime.test.ts` covers default paths, external/incompatible installs, integrity, permissions, and daemon behavior. `apps/desktop/renderer-tests/startup-frontdoor.test.tsx` exercises real bootstrap with fake transport. Extend these rather than inventing a new harness.

Required regressions:

- Fresh bare `/`, empty storage, native runtime missing: setup visible without clicking New session; no host alert at any observed startup phase; no automatic install, handshake, or seeded draft.
- Exactly one enabled primary setup action and one closed Advanced disclosure; no Retry connection, nested Runtime diagnostics accordion, build placeholders, or visible path choices.
- Slow inspection remains neutral; inspection failure stays actionable. Missing selected path and missing unselected runtime render differently.
- Default install occurs once, then connects once; SDK verified success continues to the correct draft/provider flow. Missing/incompatible/cancelled install results do not count as success.
- Permission, payload, install-path, incompatible runtime, and post-install connection failures preserve recovery and error ownership.
- Existing stopped/running compatible CLI skips onboarding and is never reinstalled/restarted unnecessarily; selected missing or incompatible executable is not replaced.
- Settings/sidebar/New session entry points agree. Host changes, unmounts, navigation, relaunch, repeated clicks, and multiple views do not cause duplicate effects or late navigation.
- Remote-only use works without local setup; web behavior, connected startup frontdoor, restored tabs, explicit routes, and first-message safeguards remain intact.
- Keyboard, zoom, minimum window size, both theme modes, long filesystem paths, and focus/live-status behavior.

Run affected app/desktop tests, type checks and builds according to the repository's current validation scripts. This planning change itself does not claim any passing runtime tests.

### Actual DMG gate

The existing `onboarding-smoke.mjs:220–264` tests installation and relaunch, but uses staged Electron, fixture paths, and an explicit network override (`:1–2,24–28`). It does not prove untouched GitHub-DMG defaults or absence of the initial red banner. Extend it with UI assertions, and separately verify:

1. Download the release DMG and launch through Finder on a disposable clean macOS user/VM without an existing whipcode binary or developer toolchain. Do not clean the developer's real home.
2. Record release/build identity, architecture, OS, and inherited environment; exercise default behavior without fixture path or network overrides.
3. Before clicking anything, capture the neutral setup state with closed Advanced and no error banner.
4. Complete default setup without Terminal, path choices, or administrator prompts; verify real installed location, selected executable, daemon identity, state directory, and default absence of a TCP listener.
5. Reach the existing provider/project flow and verify a usable session once credentials are supplied explicitly. Quit/relaunch and confirm no repeated setup and reuse of the intended runtime.
6. Separately test existing CLI, failed install, and missing-selected-path recovery. Capture screenshots and results, including failures.

Keep connected startup timing percentiles and fresh-install functional acceptance distinct.

## Ordered work

- [x] Trace screenshot symptoms through first-launch routing, host error ownership, native inspection, defaults, and current tests.
- [x] Record proposed UX, safeguards, implementation boundaries, and release acceptance.
- [x] Approve the one-screen direction and setup-to-New-Chat transition; capture the states in Paper.
- [x] Add host/bootstrap regressions for the initial missing-runtime state and preserve actual failure tests.
- [x] Implement shared local readiness and expected setup-needed handling.
- [x] Reuse setup content across frontdoor and New Chat; collapse controls into one Advanced section and retain the existing sidebar setup action.
- [x] Update canonical frontend documentation.
- [x] Validate the production build, full web suite, and headless light/dark desktop/narrow layout and retry behavior.
- [ ] Complete staged native smoke coverage and validate an actual fresh GitHub DMG installation.

Implementation validation (2026-09-23): `npm run check:web` passed; `npm run test:web`
passed 1,211 tests across 91 files. The real bootstrap tests cover missing-runtime
setup without a host error or transport, an explicit install, and continuation to
both provider setup and the configured composer. Host tests cover retired probes,
real failures, compatible installations, and remote/web bypass. Temporary headless
Chromium checks at 1280px and 390px in light/dark themes verified Advanced controls,
no horizontal overflow, pending installation, retry, and no page errors. The temporary
preview used fake native capabilities and was removed; it is not clean-Mac native
installation evidence.

## Remaining verification

The implementation preserves the existing dirty tree and native installer policy. Before release acceptance, confirm the affected release's build identity and any in-flight host/network changes. Setup now reuses shared readiness rather than issuing a second panel probe. The runtime inspection is already bounded, but additional preflight cost for existing installations remains to be measured. Native permission prompts and installer behavior on a truly clean Mac remain acceptance evidence to collect, not assumptions to declare proven.
