# Provider connections, logos, and environment discovery

Branch: `codex/desktop-release` (current shared working tree; no branch change made)

Status: first-release implementation complete and validated, 2026-09-09. Approved by the user. Provider expansion remains a separate increment.

Implemented: effective host routes, source-aware inventory, revision-checked disable/disconnect, shared connect/manage dialogs, bundled logos, and bounded local desktop environment recovery. Full checks and independent review passed.

Deviation: introduced `provider.disconnect` with a required configuration revision instead of changing legacy `provider.logout`, preserving older clients’ account-only logout behavior.

## Goal

Make Settings → Providers immediately answer: which providers can this execution
host use, where do their credentials come from, and how do I connect another one?
Use the supplied OpenCode screenshots as the interaction and layout reference:
recognizable logos, compact grouped rows, source badges, and per-provider actions.
Automatically make supported providers available when their keys are already in
the execution host's environment.

Approved scope: ship the list, existing connection flows, and discovery together.
Then expand the preset list in a separate increment after checking each provider's
actual inference behavior. This gives us a useful first release without turning
a Settings improvement into a general provider SDK project.

An optional scope question was sent about including direct Anthropic and Google.
This plan uses the recommended narrower scope unless the user chooses otherwise.

## Research findings

### What OpenCode actually does

The screenshots show v1.18.29. I checked that tag at
`16747470f976aca3d362ad730bcd3fe82ecc2c9a` and current `dev` at
`830d5eb5354874105cc31599635a80c1662609e8`. The provider Settings component is
identical at those two revisions. References below are pinned, not moving links.

1. **A provider directory and a connected subset are separate concepts.** Its
   Settings component removes connected IDs from the popular list, displays a
   source badge, and opens a shared connection dialog. The popular list is a
   short explicit ordering; it is not computed from usage or installed software.
   Sources: [Settings rows](https://github.com/anomalyco/opencode/blob/830d5eb5354874105cc31599635a80c1662609e8/packages/app/src/components/settings-providers.tsx#L49-L95),
   [popular ordering](https://github.com/anomalyco/opencode/blob/830d5eb5354874105cc31599635a80c1662609e8/packages/app/src/hooks/use-providers.ts#L9-L19).

2. **Environment discovery is server-side credential discovery.** In the
   screenshot version, it walks known provider definitions, checks their declared
   environment names, and enables matching providers with source `env`. Stored
   API keys are merged afterward; configuration overrides are applied later.
   This path does not validate every credential with an inference request.
   Sources: [v1.18.29 discovery](https://github.com/anomalyco/opencode/blob/16747470f976aca3d362ad730bcd3fe82ecc2c9a/packages/opencode/src/provider/provider.ts#L1578-L1606),
   [environment owner](https://github.com/anomalyco/opencode/blob/830d5eb5354874105cc31599635a80c1662609e8/packages/opencode/src/env/index.ts#L21-L43).

3. **Environment connections do not have an ordinary Disconnect button.** The
   UI describes how they are managed externally. OpenCode also has
   `disabled_providers`, which suppresses automatic loading and model selection.
   Sources: [source-aware actions](https://github.com/anomalyco/opencode/blob/830d5eb5354874105cc31599635a80c1662609e8/packages/app/src/components/settings-providers.tsx#L92-L142),
   [disable semantics](https://opencode.ai/docs/config/#disabled-providers).

4. **Logos are bundled assets.** A small component resolves a provider ID to a
   local SVG sprite with a generic fallback. We can use this pattern without
   copying its whole icon collection or introducing an icon dependency.
   Sources: [icon component](https://github.com/anomalyco/opencode/blob/830d5eb5354874105cc31599635a80c1662609e8/packages/ui/src/components/provider-icon.tsx),
   [SVG assets](https://github.com/anomalyco/opencode/blob/830d5eb5354874105cc31599635a80c1662609e8/packages/ui/src/components/provider-icons/sprite.svg),
   [upstream notice](https://github.com/anomalyco/opencode/blob/830d5eb5354874105cc31599635a80c1662609e8/LICENSE).

5. **Desktop also recovers shell environment variables.** Its bounded probe tries
   an interactive login shell, reads NUL-delimited environment output, and falls
   back when unavailable. App-provided values take precedence when merged.
   Source: [desktop shell environment](https://github.com/anomalyco/opencode/blob/830d5eb5354874105cc31599635a80c1662609e8/packages/desktop/src/main/shell-env.ts#L25-L101).

OpenCode's broader provider coverage comes from provider adapters and model
metadata, not from the environment detector itself. Its new core also derives
key/environment integration methods from Models.dev definitions:
[metadata integration](https://github.com/anomalyco/opencode/blob/830d5eb5354874105cc31599635a80c1662609e8/packages/core/src/plugin/models-dev.ts#L131-L151).
WHIP does not need to adopt this plugin architecture.

### Where WHIP is today

| Area | Current implementation | Consequence |
| --- | --- | --- |
| Provider setup | `packages/app/src/settings/providers.tsx`: provider dropdown, two sign-in buttons, shared key form | The user has to inspect one provider at a time; connection methods are scattered. |
| Supported setup | `internal/daemon/provider_service.go`: API key setup only for Inference.net/OpenRouter; account login also supports `openai-codex` | We already have the important connection machinery to reuse. |
| Key resolution | `internal/config/config.go:Provider.ResolveKey`: explicit environment reference, configured literal/secret reference, then Inference.net account/CLI fallback | Keys can come from the environment, but an arbitrary known provider is not automatically registered. |
| Status | `internal/daemon/provider_account.go:readProviderStatus`: only Inference.net/OpenRouter; `configured` means entry exists; source is inferred from fields | A missing environment variable can still look configured, and a fallback machine key can be mislabeled Environment. |
| Model discovery | `clientProviderCatalogs` in `internal/daemon/client_control.go` refreshes every configured provider sequentially | A provider list should not wait on these upstream calls just to show logos and connection state. |
| Runtime routing | `Config.ResolveRoute`, catalog resolution, daemon `ProviderService.ModelClient` | Discovery must feed these shared paths; adding rows only in React would produce unusable choices. |
| Desktop environment | `apps/desktop/src/runtime.ts:runtimeEnvironment` imports shell PATH only | Finder launches can miss keys exported in shell startup files. A running daemon retains its startup environment. |
| Model UI | `packages/app/src/model-options.ts` combines config/catalog models using explicit model/provider pairs | Reuse it and preserve API/subscription distinction when new routes appear. |

The existing ChatGPT subscription work is the implementation baseline, including
its host-owned login, refresh, account catalog, and signed-out/error states.
Research did not read private environment values or make paid model requests.

## Scope and release boundary

### First release: existing working routes

| Provider | Connect methods | Automatic detection |
| --- | --- | --- |
| Inference.net (`inference-net`) | Existing account login or API key | `INFERENCE_API_KEY`; preserve the existing account and `inf` CLI fallbacks |
| OpenRouter (`openrouter`) | API key | `OPENROUTER_API_KEY` |
| OpenAI — ChatGPT subscription (`openai-codex`) | Existing device login | Existing WHIP-managed subscription account only; an API key never connects this route |
| Already configured custom endpoints | Inspect and replace their API key when supported | Resolve their explicitly configured environment references; do not guess endpoints from arbitrary variable names |

This release makes environment detection real for the built-ins WHIP has already
validated. It also makes existing custom providers visible instead of showing an
unsupported-status error when selected.

### Next increment: more presets and custom creation

Start with **Cerebras** (`CEREBRAS_API_KEY`) and **OpenAI API** (`OPENAI_API_KEY`),
keeping OpenAI API and ChatGPT subscription as separate rows with the same logo.
These are additions to the supported preset list, not claims that every model
they advertise works through WHIP today.

- Cerebras documents an OpenAI-compatible endpoint and specific differences in
  reasoning and image input. Validate WHIP's complete streamed tool cycle and
  normalize its catalog limits before enabling the preset.
  [Cerebras compatibility](https://inference-docs.cerebras.ai/resources/openai).
- OpenAI API needs its own compatibility check: WHIP's generic request currently
  writes `max_tokens`, and its `/models` parser does not establish model endpoint
  capabilities or reliable context/output limits. Expose only models verified
  for the implemented API; exclude image/audio/embedding and Responses-only
  models from the coding model picker. Add a small provider-specific request or
  metadata adaptation if needed, rather than assuming a URL/key is sufficient.
  [OpenAI model capability reference](https://developers.openai.com/api/docs/models/all).
- Add a **Custom provider** connection form over the existing OpenAI-compatible
  route: name, stable ID, base URL, and API key or environment-variable name.
  Support an explicit model ID and context/output limits when `/models` is
  absent or incomplete. Preserve existing aliases on edits. Reserve built-in
  IDs, reject duplicates and credential-bearing URLs, and do not allow a custom
  endpoint to impersonate the subscription transport. No header editor or
  arbitrary protocol selector in this increment.
- Groq is a reasonable following preset, using `GROQ_API_KEY` and its documented
  compatibility surface. It is not necessary to complete the first release.
  [Groq compatibility](https://console.groq.com/docs/openai).

Direct Anthropic and Google support remains a separate decision. Both offer
compatibility layers, so native adapters are not automatically mandatory, but
the layers have meaningful constraints. Anthropic documents missing prompt
caching and altered system-message handling; Gemini's compatibility layer is
still beta. We should evaluate multi-turn tools, reasoning/continuation, usage,
images, and compaction before promoting either to a supported preset.
[Anthropic compatibility](https://platform.claude.com/docs/en/cli-sdks-libraries/libraries/openai-sdk),
[Gemini compatibility](https://ai.google.dev/gemini-api/docs/openai).

### Non-goals

No provider plugin framework, full Models.dev synchronization, automatic OAuth
credential import from other applications, cloud credential-chain discovery,
project `.env` scanning, local server port scanning, or new secret vault.
No automatic model/default/billing-route changes. No Settings navigation redesign
or native mobile provider editor in this work. Existing host contracts continue
to serve CLI/TUI/ACP consumers.

## Product and visual design

The person here is configuring the host that will run their next coding task.
The interface should let them recognize a provider, understand its credential
source, and connect it without interrupting the surrounding workspace.

Design exploration, grounded in the reference and WHIP's existing UI:

- **Domain:** execution hosts, provider accounts, API credentials, model routes,
  subscription access, connected/disconnected states, configuration ownership.
- **Color world:** the current theme's canvas, raised settings surface, strong
  foreground, subdued metadata, quiet separators, and semantic error color.
  Use WHIP's existing tokens and typography rather than copying screenshot hex
  colors. Provider marks supply recognition without introducing branded panels.
- **Signature:** every connection belongs to the selected execution host. This
  appears in the page host selector, connect dialog title, environment help,
  account details, and model/default destination.
- **Defaults to replace:** a grid of large vendor cards becomes a compact list;
  a permanently visible credential form becomes a per-provider dialog; a generic
  green Connected badge becomes an accurate credential-source label and an
  actionable error only when there is an actual problem.

Within the existing Providers category:

1. Keep the existing execution-host selector prominent.
2. Show **Connected providers** first: logo, display name, source badge, and an
   appropriate Manage/Disconnect action. Credentials detected from environment
   are available connections, not proof of successful inference or remaining
   quota. Do not add a verification checkmark based only on key presence.
3. Show **Needs attention** only when there are configured connections with
   missing credentials, invalid setup, or expired login. Each row offers the
   relevant repair action. This prevents missing-key entries from appearing as
   healthy connections or disappearing without explanation.
4. Show **Connect a provider** with the remaining supported presets: logo, short
   description, and Connect. No empty placeholders for unsupported vendors.
   Add “Show more” only when the list actually grows beyond the short preset set.
5. Keep the existing default model/provider/effort controls below these groups.
   Connecting a provider updates choices but does not change these defaults.

Rows use one subtle group border with internal separators, approximately 20px
logos, existing 13–14px labels and 12px supporting text, and the existing spacing
scale. Actions stay on the right at desktop widths and wrap below content on
narrow screens. Names and source labels remain readable at 200% zoom. Environment
help must be reachable by keyboard/touch, not hover-only.

The connection dialog reuses existing Base UI primitives and current account
flows. Only show a method chooser when a provider has multiple methods. A key-only
provider opens directly to a password input and Connect. Inference.net retains
its team/project selection and rotation details; OpenAI subscription retains its
device-code and account states. Manage contains secondary actions and source
details, keeping the list compact.

Use a small app-owned `ProviderLogo` component and checked-in SVGs for the initial
brands, with a neutral generic fallback for custom IDs. Alias `openai-codex` to
the OpenAI logo and `inference-net` to the Inference mark. The upstream sprite
contains those marks; extract only the required symbols, retain provenance and
applicable notices, and include those notices in packaged artifacts. No runtime
logo requests, uploaded SVGs, or new dependency. Logos are decorative next to
the provider name, with explicit dimensions and `aria-hidden`.

## Backend design: one effective provider map

### Small built-in table, shared resolution

Add a plain table in `internal/config/providers.go` with built-in ID, display
name, endpoint/API, known environment names, and supported connection methods.
Reuse existing constants and upsert helpers. This is data for supported WHIP
routes, not a new registry/plugin subsystem.

Keep saved configuration separate from runtime discovery. Add a pure
`Config.EffectiveProviders`-style helper that returns a fresh map combining saved
entries with missing built-ins whose approved environment variable is nonempty.
Keep discovered entries as environment references, never resolved key strings.
Use it in `ResolveRoute`, catalog-only model resolution, provider inventory, and
catalog loading. Audit direct `Config.Providers` reads so every execution path
sees the same routes.

Do **not** merge discovered entries into `loadUnlocked` or the saved `Providers`
map: `ReadVersioned`/`UpdateVersioned` and ordinary `Save` share these objects.
Doing that would accidentally persist discovery during unrelated settings edits
and make configuration revisions depend on ambient environment state.

Resolution rules:

1. An explicitly disabled provider is unavailable, regardless of credentials.
2. A saved provider entry wins as a whole over an automatically discovered
   built-in of the same ID. Never replace its endpoint or inject a conventional
   environment key into a custom endpoint.
3. Within that saved entry, preserve today's resolution order:
   `apiKeyEnv` → configured key/reference → existing Inference.net fallbacks.
4. Only a missing built-in entry can be synthesized from its known environment
   variable. Blank values do not create connections; arbitrary `_API_KEY`
   variables do not create guessed providers.
5. `openai-codex` uses its existing credential manager and account-scoped catalog.
   It does not participate in API-key discovery.

### Source-aware inventory

Add one cheap host RPC, `provider.list`, composed in the existing ProviderService.
Return all supported presets plus explicitly configured custom providers, with
safe display metadata, supported actions, and the existing status shape extended
only where necessary for credential availability/source and disabled state.
Keep `configured` as “saved route exists” for compatibility; do not silently
change its meaning. Reuse this status projection for `provider.status` too.

The source must match the credential actually chosen. A missing `apiKeyEnv` that
falls through to an Inference.net machine key is an account source, not
Environment. Distinguish available credentials, missing credentials, and a
configured command reference that has not been checked. Do not execute `!cmd`
secret helpers simply to paint the list. Subscription states remain owned by the
existing auth manager.

The inventory read performs no upstream calls and exposes no key/token value,
secret command, or raw upstream error body. Known environment variable names are
safe to display. One TanStack Query per selected host reads this inventory; rows
do not each create a status request or poller. The screen requires the new RPC;
the user explicitly removed older-daemon UI compatibility on 2026-09-09.

### Connection, disabling, and disconnect

Reuse revision-checked `provider.key.set` and the existing login RPCs. Generalize
key replacement to supported configured custom routes without replacing their
endpoint or model definitions. Keep secrets ephemeral in the renderer/SDK and
keep the current host storage; no migration into a second auth store.

Add one `disabledProviders` config list, default empty, so automatic discovery
has a durable opt-out. Update it through the existing revision-checked config
service. Manage can offer **Disable on this host** for environment/config-owned
connections and **Enable** for disabled ones. Do not display Disconnect as though
WHIP can unset a parent process's environment or remove another CLI's credentials.

For WHIP-owned credentials, extend the existing logout path to remove the
appropriate saved key/account and disable the route, preventing an environment
or external-key fallback from immediately reconnecting it. Preserve model aliases
and saved defaults; display an unavailable-default repair action if necessary.
Do not promise remote API-key revocation when only a local key was removed.
Retain existing Inference.net best-effort remote logout warnings and subscription
refresh/logout race protection. New credential/config mutations must use current
revision checks; preserve existing RPC compatibility where signatures expand.

Implemented boundary: every model-request admission checks the current host connection, including retries, children, helpers, title generation and compaction. An admitted request may finish; subsequent requests to a disabled or unavailable provider fail permanently with repair guidance. Enabling a route with no current credentials cannot reactivate a retained session key. Existing session clients otherwise retain their route/key snapshot until session reload, model change or reconstruction; replacing/rotating a key requires reloading existing sessions. No billing route is selected as a fallback.

### Catalogs and model selection

Keep the existing catalog store and model-options builder. Make catalog reads
use the effective provider map and filter cached catalogs for providers that are
now absent/disabled; stale caches must not keep disconnected models selectable.
Keep configured aliases intact, with unavailable routes clearly indicated.

The cheap provider inventory lets Settings render before model fetching finishes.
Reuse the existing catalog freshness metadata for missing/stale fetches, refresh
after connection changes, retain last-good data on transient network failures,
and bound upstream work with cancellation and a per-provider timeout under the
existing total deadline. Do not introduce background health polling or another
cache. Do not treat a public `/models` success as proof that a key can run models.

For newly added presets, normalize actual input/output limits and capabilities
from an authoritative source. Missing metadata stays unknown. A successful
`/models` response alone is insufficient to promote a provider or every returned
model into the supported coding catalog.

## Desktop environment behavior

Include a narrow improvement to the existing `runtimeEnvironment` helper for
local daemon launches: recover the supported provider environment names alongside
PATH from the user's shell. Use a fixed allowlist, bounded output, timeout and
cancellation, NUL-delimited parsing, and no shell-output logging. Preserve
explicitly inherited values. Support ordinary macOS zsh/bash login/interactive
startup; fall back to inherited environment if the probe fails.

Do not import the whole shell environment or discover project `.env` files. The
small desktop allowlist mirrors the enabled built-in presets and is checked for
parity in tests; a separate generation system is unnecessary at this size.

Attach to an existing compatible daemon as today. A newly imported variable
cannot change that running process. Explain in source details that environment
changes take effect when the execution host is restarted through the existing
explicit restart workflow. Never restart active work just to discover a key.
Reopening Settings only rereads the daemon's current environment.

Browser clients use the selected daemon's environment. Remote hosts discover
their own credentials; desktop shell recovery must not transmit local provider
keys to an SSH/URL execution host.

## Implementation sequence and files

1. **Effective routes and inventory.** Add the small built-in table, source-aware
   projection, disabled list, and read RPC. Touch `internal/config/{config,
   providers,revision}.go`, `internal/daemon/{provider_account,provider_service,
   provider_model,provider_rpc,client_control}.go`, and protocol source types/
   registry. Update shared resolution callers. Generate protocol outputs and add
   `providers.list` in `packages/sdk/src/services.ts`.
2. **Complete lifecycle behavior.** Generalize supported key replacement,
   revision-check disconnect/disable behavior, filter obsolete catalogs, and
   preserve login/refresh races. Use existing ProviderService operations and
   stores. Verify existing CLI/TUI auth and runtime consumers see effective routes.
3. **Provider list and logos.** Refactor `packages/app/src/settings/providers.tsx`
   around the host inventory; extract the connect dialog only if it makes the
   file simpler. Add app-owned logo assets/component and local StyleX styles.
   Reuse `settings/{section-layout,unsaved,configuration}.tsx`; maintain
   `settings/navigation.ts` search targets and focus return. Preserve existing
   settings deep links to provider/key controls.
4. **Desktop launch parity.** Update `apps/desktop/src/runtime.ts` and its tests
   for the limited shell recovery. Check provenance/notices in
   `apps/desktop/scripts/notices.mjs` for the copied assets. Do not install or
   restart the user's normal daemon as part of automated validation.
5. **Ship the first release after the acceptance checks below.** Record the
   implementation and any limitations in this plan and canonical docs.
6. **Expand independently.** Add Cerebras, OpenAI API, and custom-provider
   creation after the catalog/request/tool-cycle compatibility checks described
   above. Grow the same table and form; do not create another provider system.

No new Go or npm runtime dependencies are expected.

## Acceptance and validation

### Backend and protocol

- An otherwise unconfigured host with `OPENROUTER_API_KEY` gains an effective
  OpenRouter route and selectable catalog models; remove the variable and restart
  the fixture host and both disappear. Discovery leaves config bytes/revision
  unchanged. A subsequent unrelated settings save must not persist discovered
  provider entries or resolved credentials.
- Saved endpoint/key references override discovery; blank variables, missing
  keys, command references, account fallback, and disabled IDs report accurately.
  An ambient key is never attached to an unrelated custom endpoint.
- A default Inference.net config with no key is not displayed as connected.
  Account fallback receives the correct source label. Existing custom entries
  can be inspected without an unsupported-provider error.
- Subscription login/account scoping, refresh races and API/subscription
  separation retain their existing coverage. Replacing/removing credentials
  cannot publish a stale catalog or change the selected billing route.
- Disconnect/disable/enable obey revisions and survive restart. In-flight work
  and existing session snapshots follow the documented refresh semantics.
- Inventory performs no provider-network requests or secret-command execution.
  Fixture secrets never occur in RPC read results, SDK recovery, logs, or session
  storage. Mutation cancellation does not replay credential submissions.
- A slow/unavailable provider cannot prevent reading the provider list. Catalog
  failures preserve usable cached data, expose bounded sanitized errors, and do
  not keep disabled/removed routes selectable.
- Exercise shared resolution through a normal session and a helper/child route;
  use existing integration fixtures rather than duplicating the agent test suite.

### UI and desktop

- Correct grouping, names/logos, source labels and actions for connected,
  unconfigured, disabled, missing-key, expired-login and offline hosts.
- API-key connect plus both existing account login flows; navigation/host changes
  clear secret drafts through the existing guard, cancel observers, and do not
  leak status/results between hosts. Login polling exists only while active.
- Default model/provider stays unchanged after connect. API and subscription
  model choices remain distinct. Disconnecting the default offers repair rather
  than selecting a replacement.
- Keyboard-only connection, dialog focus return, long labels, narrow layout,
  zoom, light/dark themes, and source help without hover. Verify SVGs under the
  production CSP and in the packaged shared renderer.
- Isolated desktop launch tests cover zsh/bash startup files, inherited-value
  precedence, noisy output, timeout/cancel, and absence of raw keys in diagnostics.
  Reusing a running daemon must not pretend to import new environment values or
  trigger a restart. A remote-host fixture must not receive local credentials.
- New presets require a streamed WHIP tool call → tool result → continuation,
  cancellation, usage, and advertised image/reasoning behavior. Use sanitized
  provider fixtures and a live acceptance call where an authorized test account
  is available; document any live test not performed.

Run affected Go/SDK/app/desktop tests while building, then `task check`, affected
Go race tests, and the existing production Settings/model-picker browser checks
with provider scenarios added. Run the desktop runtime tests and one isolated
packaged-renderer smoke for shell recovery and local assets. Complete the
feature skill's independent correctness/simplicity review before shipping.
Implementation validation is recorded below.

## Documentation and completion criteria

Update `docs/models-providers.md` with supported environment names, exact
precedence, connection-source meanings, disabling, and process restart behavior.
Update `docs/frontend.md` with provider inventory ownership and connection-query
lifecycle, plus `docs/desktop.md` with the narrow shell recovery contract.
Record shipped behavior/code/tests in `docs/features.md`, add/check the matching
`docs/roadmap.md` item, and update the SDK documentation for the new read surface.
Keep this plan as research/history rather than a second current architecture spec.

First-release completion means the existing working providers can be recognized,
connected, inspected, and managed from rows; supported environment keys work
without writing provider configuration; desktop/browser/remote ownership is
accurate; and existing subscription/model-selection behavior remains intact.
Broader preset coverage is tracked separately so it cannot obscure that result.


## Implementation record (2026-09-09)

- Implemented the three existing provider connections and management of configured custom endpoints, using the same daemon-owned configuration and login service.
- Added effective route discovery without saving discovered routes or credentials; explicit saved routes win. A detected environment key can also be selected explicitly after a saved key is removed.
- Added credential-free inventory, actual source/readiness, implicit default-provider reporting, revision-checked disabling and disconnecting, and permanent model-admission checks. Legacy logout remains compatible.
- Added compact grouped rows, bundled MIT-attributed logos, source-specific actions, explicit default repair and existing secret-draft/navigation guards. Model choices honor availability in web, TUI and mobile.
- Reused catalog storage/TTL; explicit SDK/TUI refresh bypasses freshness. Account/route changes reject pending old responses. Inference.net custom endpoints never sign out an unrelated account.
- Local desktop recovers only the two supported shell keys with inherited-value precedence, timeout/output limits and cancellation. Remote connection behavior stays host-owned.
- No runtime dependencies added. New presets and custom-provider creation remain the independent next increment described above.

Independent review found and verified fixes for unrelated account logout, pending account catalogs, retryable disable failures, inconsistent picker readiness, and invalid-key disconnect controls. No remaining blockers were reported. Regression tests cover each issue.

Validation record:

- Production provider workflow: Chromium and Firefox, five grouped checks each; keyboard connect/discard/focus return, real loopback validation and config persistence, disconnect/reload, environment toggling, default repair, source labels, SVG/CSP, light/dark and 390px layout.
- Existing production Settings workflow: Chromium and Firefox, nine workflows each.
- Desktop type check and test suite: 75 passed, nine existing conditional skips; zsh/bash recovery, precedence, noise/failure/timeout/cancel and allowlist parity covered.
- Mobile type check and tests: 145 tests passed across 19 suites, including availability filtering.
- Full `go test -race ./...` passed, including the final admission-readiness checks and subscription runtime regression.
- Full `task check` passed: Go formatting/vet/custom analysis/tests, generated protocol drift, SDK, frontend/UI types and tests, and packaged-asset tests. Five existing server-dialog assertions were updated to include the cancellation signal added by the shared host-manager work; no host-manager behavior was changed for this feature.
- Browser evidence: `/tmp/whip-provider-browser-results/`; validation logs: `/tmp/whip-providers-*.log`.

No live provider authentication or paid inference was needed for this change. Existing subscription transport/login regression coverage was retained. Tests used disposable hosts and synthetic credentials; the user's installed app and active daemon were not restarted.

### Local activation and cleanup (2026-09-09)

The user approved updating and restarting the local daemon, including interrupting
its running server-management agent. The installed backend now supports
`provider.list`. Removed the old provider form, per-provider status component,
capability fallback, and their obsolete tests. Retained the login, secret-lifetime,
account rotation, and host-scoping regressions against the new connection dialogs.

Installed and reopened signed Whip 0.1.3 with backend
`local-provider-connections-20260909` and renderer
`30752cad0e92f48d48301aaae116f363b50a8b2d944efc7dc5336dae371e78a1`.
The original app and executable were retained as local rollback copies.
Verified the live browser shows all three connected provider rows and no old
editor, with no browser errors. The full `task check` passed (382 frontend tests,
including 15 provider tests), and the five provider workflow checks passed in
both Chromium and Firefox. One existing local-runtime test now expands the
recently collapsed diagnostics before checking its contents.
