# Provider onboarding: from installation to the first useful response

Branch: `codex/provider-onboarding`

Status: **Implemented and validated.** Product direction confirmed September 9, 2026, including subtle Inference.net preference. The user explicitly skipped the proposed usability study. Researched against Whip `7058e007fec0a4f2e926f3b26dcb9e06ed75be5b` and OpenCode `9f8db119fcbd4999379129ac7734375ac23460fb`.

## Implementation record

Both planned releases are implemented together on this branch. Current architecture is documented in [docs/frontend.md](../../../docs/frontend.md); the research and wireframes below record the original design rationale.

- [x] Host-owned local readiness, shared preset preference, selected-provider catalog queries and explicit revision-checked default model/provider updates.
- [x] Reusable TUI provider setup before session creation; `/connect` and `/auth`, masked entry, available connections, preserved drafts and no optional preference questionnaire.
- [x] Reusable web/desktop connection and login UI, prompt-first welcome, folder and permission controls, explicit model confirmation and return to the draft.
- [x] Bounded first-message recovery with original create/submit identities, cross-window admission and draft revisions that preserve later edits.
- [x] One-action native install/connect using verified bundled bytes and the existing host owner.
- [x] Singleton Inference.net workspace/project login choices advance on the host; actual choices remain explicit.
- [x] Existing session/default choices are preserved; fresh external MCP imports are opt-in; obsolete setup markers are unused.
- [x] Auxiliary-model routing rejects a built-in alias sent to an unrelated provider, allowing existing title/compaction fallback to the selected main model.
- [x] Final repository, race, packaged-desktop and isolated acceptance checks recorded in [VALIDATION.md](VALIDATION.md).
- [x] Usability study omitted at the user's request; no study materials or testers required.

The session-creation contract adds optional `permission_mode`, stored in the same transaction as creation/acceptance using the existing column. No database migration or new authentication operation was necessary. Whip's saved subscription credentials already support completing a missing route.

Recommendation candidates are host-owned and used only when the selected route supports the exact model. Inference.net retains the shipped `kimi-k3-fast`, then `kimi-k3`; OpenRouter checks `moonshotai/kimi-k3`, `moonshotai/kimi-k2.5`, then `anthropic/claude-sonnet-4.6`. ChatGPT checks `gpt-5.6-luna`, `gpt-5.6-sol`, `gpt-5.6-terra`, `gpt-5.5`, then `gpt-5.3-codex`, restricted by its account catalog and existing supported output limits. A valid saved choice takes precedence. Custom endpoints receive no preset-model ranking; a sole explicit model can be offered. Catalog order is never used as a quality ranking.

These are conservative route suggestions, not comparative quality or pricing claims. Streaming, real Starlark execution, title and compaction wiring are exercised with a local provider fixture. Existing [ChatGPT live acceptance](../openai-subscriptions/README.md) includes Luna, tools, context handling and restart; it is prior evidence, not a new paid test of every candidate. No billable call is run automatically to validate onboarding or a model recommendation.

## Recommendation

Make setup depend on what is missing to send a request. A user with a usable provider/model selection should see the composer immediately. A user without one should connect a provider in that same context, receive a suitable model recommendation, and return to their intact prompt.

The largest gap is not missing credential detection. Whip already has host-owned discovery and authentication infrastructure. The gaps are that the TUI's first-run wizard does not consume that inventory, provider authentication does not necessarily select a usable default model/provider pair, and web/desktop sends users into Settings to finish setup.

**Final ordering:** keep a valid existing selection; otherwise offer usable detected credentials first; otherwise show the three supported provider presets with clear authentication choices. Inference.net comes first within comparable choices and receives a subtle “Recommended” label in the connection chooser. Its preference must not add steps, obscure a working alternative, or change the account used for a request without the user's choice.

The design was sequenced in two releases: first readiness/provider setup, then the first-prompt screen and desktop handoff. Both are included in this implementation. No new onboarding framework, authentication store, provider marketplace, or dependency was needed.

## Goal and non-goals

**Goal:** a person installing Whip can understand which account will run their request, connect it with minimal input, and receive a response without visiting an advanced settings screen or learning provider/model configuration syntax. Reopening Whip should retain the connection and chosen defaults. Reopening a session should retain that session's own choices.

Success means a successful user-initiated request, not an existing config file, an API key being present, or a completed wizard marker.

In scope:

- Interactive TUI startup and reconnect/provider repair.
- The shared web/desktop welcome screen and reusable provider connection UI.
- Native desktop's missing-backend installation and connection handoff.
- Existing Inference.net, OpenRouter, and ChatGPT subscription support, plus already-configured custom providers.
- Clear model defaults, retry behavior, keyboard access, host ownership, and first-run acceptance tests.

Out of scope:

- Adding a broad provider catalog, a new provider integration, free credits, or a Whip billing system.
- Importing arbitrary credentials from other products or scanning the machine for secrets.
- Automatically sending a test prompt, creating project files, importing MCP servers, or broadening permissions.
- A new folderless chat/scratch-workspace mode, a tutorial carousel, or a general product redesign.
- Changing unattended CLI behavior into an interactive wizard.

## Baseline before implementation

These are historical source findings at the research revision, not current implementation requirements or measured usability results. Paths below are repository-relative to this plan; line numbers refer to that baseline.

| Area | Finding | User consequence | Evidence |
| --- | --- | --- | --- |
| Detection | Presets cover Inference.net, OpenRouter, and ChatGPT subscriptions. Effective routes can use known environment variables; Inference.net also supports Whip's saved machine key and external Inference CLI credentials. Explicit configuration and disabled-provider choices matter. | Much of auto-detection already exists; expose it consistently rather than reimplementing it in each client. | [Presets and key precedence](../../../internal/config/providers.go#L20), [Inference credentials](../../../internal/config/config.go#L43) |
| Readiness | `provider.list` is an inexpensive local metadata query. It deliberately makes no upstream requests and does not execute credential commands. A present key can yield `connected` without proving an inference request will succeed. | “Detected” and “ready to attempt a request” must not be described as “verified working.” | [Inventory](../../../internal/daemon/provider_list.go#L19), [status calculation](../../../internal/daemon/provider_list.go#L103) |
| TUI setup | Startup runs folder trust, then a separate wizard. Its fixed provider menu offers Inference.net browser login, OpenRouter key entry, or Skip. It omits ChatGPT and does not use provider inventory. | Already-configured users can receive unnecessary setup questions; supported accounts are harder to discover. | [Startup](../../../internal/tui/client.go#L75), [wizard menu](../../../internal/tui/setup.go#L126), [setup host interface](../../../internal/tui/setup_host.go#L12) |
| TUI completion | The wizard asks about reasoning display and Claude/Codex MCP imports, then writes `setup.done`. Key entry is visibly echoed. Provider helper failures or skipping can still lead to completion. | Setup can look finished without a working route; optional preferences delay the first request. | [Wizard sequence](../../../internal/tui/setup.go#L57), [key entry](../../../internal/tui/setup.go#L215), [marker semantics](../../../internal/config/config.go#L320) |
| Defaults after connection | Shipped defaults select `kimi-k3-fast` through Inference.net. `SetProviderKey` validates and saves a provider but does not update `DefaultModel`/`DefaultProvider`. Settings has a separate control that saves the pair. | On a fresh configuration, connecting only OpenRouter can leave the default route pointing at unavailable Inference.net. | [Shipped defaults](../../../internal/config/config.go#L653), [route resolution](../../../internal/config/config.go#L535), [key save](../../../internal/daemon/provider_service.go#L280), [default picker](../../../packages/app/src/settings/configuration.tsx#L121) |
| Web/desktop welcome | The screen leads with Local/Remote, a working-directory field and optional model selector. Its submit creates a session without sending a prompt. Provider configuration is a secondary Settings link. | Users must understand administrative choices before reaching the core interaction. Connectivity alone does not establish provider readiness. | [Welcome](../../../packages/app/src/welcome.tsx#L18), [creation flow](../../../packages/app/src/welcome.tsx#L57) |
| Browser authentication | Existing dialogs handle keys and account flows, but live within Settings. Login requires a separate verification-page action; intermediate states expose workspace/project choices. | Useful infrastructure exists, but the first-run path lacks a direct return to the composer and a selected model. | [Connection dialog](../../../packages/app/src/settings/provider-connections.tsx#L114), [login UI](../../../packages/app/src/settings/provider-login.tsx#L48) |
| Desktop bootstrap | The native runtime can discover, inspect, and install a backend. The current panel exposes Test connection, Choose executable, Install, and instructions to connect separately. | A clean desktop installation can encounter another setup workflow before provider setup. | [Discovery/probe](../../../apps/desktop/src/runtime.ts#L166), [runtime panel](../../../packages/app/src/host-dialog.tsx#L227) |
| Host boundaries | Discovery, login, configuration, and credentials belong to the execution host. Desktop already recovers allowlisted shell environment values. A running daemon retains its launch environment. | Detection must say where it happened. A browser refresh cannot make a daemon see a newly exported key. | [Canonical architecture](../../../docs/frontend.md#L725), [desktop runtime](../../../apps/desktop/src/runtime.ts) |

The existing `/auth` command is a reusable entry point to improve. Add `/connect` as a discoverable alias, keeping `/auth` compatible. Do not build a second authentication command implementation. [Command registry](../../../internal/tui/registry.go#L28), [dispatch](../../../internal/tui/client.go#L1851).

## What to learn from OpenCode

Research used official documentation and a read-only checkout of the official repository. Pinned code links below describe the inspected source snapshot, not necessarily the latest packaged release. OpenCode's app has both legacy and newer connection layouts behind a setting; there is no single universal screen to reproduce. No live comparative timing study was performed.

| Observed pattern | Why it helps | Whip recommendation |
| --- | --- | --- |
| The TUI home centers the prompt; `/connect` is a reusable command. When initial synchronization finds zero providers, the app can open provider selection. | Setup is adjacent to the action the user came to perform. | Reuse a provider picker from startup, the composer, and repair flows. A usable existing route skips onboarding. [Home](https://github.com/anomalyco/opencode/blob/9f8db119fcbd4999379129ac7734375ac23460fb/packages/tui/src/routes/home.tsx#L70), [startup trigger](https://github.com/anomalyco/opencode/blob/9f8db119fcbd4999379129ac7734375ac23460fb/packages/tui/src/app.tsx#L540), [command](https://github.com/anomalyco/opencode/blob/9f8db119fcbd4999379129ac7734375ac23460fb/packages/tui/src/app.tsx#L738) |
| The provider picker groups choices, explains authentication benefits, and skips method selection when only one method exists. | Users select an account they recognize instead of interpreting API terminology. | Put detected connections first. Otherwise show three concise choices, with direct key entry or sign-in and advanced methods secondary. [Picker and methods](https://github.com/anomalyco/opencode/blob/9f8db119fcbd4999379129ac7734375ac23460fb/packages/tui/src/component/dialog-provider.tsx#L47) |
| TUI authentication success refreshes provider state and opens model selection scoped to that provider. | Connecting an account leads to a usable next decision. | Go one step further: recommend a compatible model and offer Change, avoiding a mandatory large model picker. [OAuth handoff](https://github.com/anomalyco/opencode/blob/9f8db119fcbd4999379129ac7734375ac23460fb/packages/tui/src/component/dialog-provider.tsx#L281), [API-key handoff](https://github.com/anomalyco/opencode/blob/9f8db119fcbd4999379129ac7734375ac23460fb/packages/tui/src/component/dialog-provider.tsx#L395) |
| Device authentication presents a link, code-copy action, waiting state, and completion handling. | The UI remains useful while the user switches to a browser. | Provide open/copy/retry/cancel and preserve the initiating screen. [Device flow](https://github.com/anomalyco/opencode/blob/9f8db119fcbd4999379129ac7734375ac23460fb/packages/tui/src/component/dialog-provider.tsx#L239) |
| Model resolution considers explicit input, configured selection, and recent valid choices before fallback. | Returning users retain continuity. | Preserve explicit and session choices. Do not silently redirect an invalid explicit route to a different vendor. [TUI selection](https://github.com/anomalyco/opencode/blob/9f8db119fcbd4999379129ac7734375ac23460fb/packages/tui/src/context/local.tsx#L197), [model documentation](https://opencode.ai/docs/models/) |
| The app's connection UI is reusable, but its success path refreshes providers, closes, and shows a toast; it does not use the same forced model handoff as the TUI. | This highlights a consistency opportunity rather than a complete pattern to copy. | Give both Whip clients the same semantic completion: usable pair selected, original task retained. [App controller/layout switch](https://github.com/anomalyco/opencode/blob/9f8db119fcbd4999379129ac7734375ac23460fb/packages/app/src/components/dialog-connect-provider.tsx#L45), [success path](https://github.com/anomalyco/opencode/blob/9f8db119fcbd4999379129ac7734375ac23460fb/packages/app/src/components/dialog-connect-provider.tsx#L718) |

Do **not** borrow provider popularity as a substitute for local readiness. OpenCode promotes its own offerings, and some connection helpers distinguish paid access from built-in free models. Whip should evaluate whether the chosen route can run the task, independent of commercial preference. [Provider documentation](https://opencode.ai/docs/providers/), [TUI connected heuristic](https://github.com/anomalyco/opencode/blob/9f8db119fcbd4999379129ac7734375ac23460fb/packages/tui/src/component/use-connected.tsx#L4).

Also avoid making repository initialization a first-call requirement. OpenCode documents `/init` as a project setup step; Whip onboarding should not generate files or launch repository analysis merely because the app opened. [OpenCode introduction](https://opencode.ai/docs/).

## Shared behavior: determine the next useful action

Use host-owned facts, not a durable `onboardingComplete` boolean. The old `setup.done` marker must not suppress necessary repair or trigger an unnecessary wizard. Existing provider configuration and session state remain the durable truth.

### Inference.net preference and list ordering

Inference.net is Whip's recommended starting provider. Express that preference through ordering and one understated label, while keeping the shortest usable path available to every user.

1. **Preserve the current choice.** A working session selection or host default remains selected. Its status/summary can precede the provider list. Never show a migration prompt merely because another provider is in use.
2. **Group by usefulness before preference.** “Already available” connections precede providers that need setup. Existing attention/disabled states stay truthful; a broken or disabled Inference.net connection does not displace a working alternative.
3. **Put Inference.net first within each comparable group.** Use the existing preset order for the remaining built-ins: OpenRouter, then OpenAI / ChatGPT. Preserve the established stable ordering of custom providers. Apply this consistently to TUI, welcome, and Settings provider lists.
4. **Keep search and model selection relevant.** Filter by the user's query first; use provider preference only within equally relevant results. Preserve favorites, recent selections, and current choices where those concepts exist. Provider-group ordering may favor Inference.net, but model recommendations still require capability fit; do not rearrange unrelated model results into a promotion.
5. **Use restrained copy.** In the setup chooser, show `Inference.net` with a small secondary `Recommended` label and the method description `Sign in in your browser`. Retain the same row size, interaction cost, and button treatment as alternatives. One label per chooser is enough; ordinary status rows and the ready composer do not need promotional copy. The label means Whip's suggested starting provider, not a claim that it is always cheapest or fastest.
6. **Respect context.** Suppress the label for disabled, known-broken, or custom-endpoint variants. A successful connection to another provider proceeds directly to its model confirmation/composer. No extra Inference.net suggestion, repeated banner, or post-login detour.

| Example | Expected presentation |
| --- | --- |
| No credentials | Inference.net · Recommended, then OpenRouter, then OpenAI / ChatGPT; all directly selectable |
| Only OpenRouter detected | OpenRouter in “Already available”; Inference.net first under “Connect another provider” |
| Inference.net and OpenRouter detected, no usable default | Both in “Already available,” Inference.net first; the user confirms the account/route |
| OpenRouter is the working default | OpenRouter remains selected; opening the general provider list still puts Inference.net first within its group |
| Inference.net disabled or failing, ChatGPT usable | ChatGPT remains the usable path; Inference.net retains its disabled/error state without a recommendation label |
| Search for OpenRouter | Matching OpenRouter results lead; unrelated Inference.net results are not inserted |

### State and next action

| Situation | What the user sees | Next action |
| --- | --- | --- |
| Compatible desktop backend already available | Brief connection status, then normal app | Attach through the existing runtime policy |
| Desktop backend absent | “Set up Whip on this Mac” | One action installs the bundled backend, connects, then shows provider readiness |
| Valid existing provider/model selection | Composer with selected pair | Type and send; no setup questions |
| One detected connection, default unavailable | “Found OpenRouter on This Mac” with a recommended model | “Use [model]” commits the explicit new-work default choice |
| Several detected connections, default unavailable | Compact list headed “Already available” | Choose an account/route once; do not guess which billing account the user intends |
| No usable connection | Provider chooser beside the composer | Sign in or enter a key |
| Saved ChatGPT credentials but missing provider configuration | “Finish connecting ChatGPT” | Reuse valid Whip-owned credentials and finish the route; do not require another login unnecessarily |
| Authentication underway | Provider name, human-readable state, browser/code controls | Complete login or cancel |
| Key detected, catalog unavailable | “Model list unavailable” with a retry and valid saved/cached choices if present | Retain configuration; do not equate network failure with bad credentials |
| Request fails | Error near the retained request with a specific repair action | Retry explicitly after repair; no silent provider switch |

### Selecting a model without making users research models

1. Keep a usable explicit/session selection. Keep a usable host default for new work.
2. If setup is necessary, choose from enabled providers with usable credential metadata; show the credential source and host. Do not revive disabled providers or replace an explicit credential source with a detected one.
3. Recommend one evaluated coding model on the selected provider, with a secondary Change control. Use an existing compatible alias when possible; otherwise use an actual discovered model ID. Never select the first alphabetical model simply because it exists.
4. For a fresh setup, label the confirmation “Use [model]” and explain “Default for new sessions on [host].” Persist `default_model` and `default_provider` together, with a configuration revision check and appropriate effort normalization. Authentication success alone does not silently change defaults.
5. If repairing an existing session, offer the new route for that session; changing defaults for future sessions is separate. Preserve the session's permission level and other settings.
6. A connection opened from ordinary Settings must not unexpectedly change a valid default. Offer “Use for new sessions” as an explicit follow-up there.

For new credentials, the recommended model can appear after login; use a single confirmation rather than a separate model-selection tour. If a recommendation was already known and displayed before connecting, a combined “Connect and use [model]” action may encompass both steps, provided partial success is reported honestly and a revision conflict does not undo a valid connection.

Keep recommendation policy small and host-owned, extending existing preset/catalog metadata. The release must evaluate streaming, tool calling, Whip's `rlm_exec` workflow, and context handling before labeling a model recommended. Test title/compaction calls as well as the first response. Do not promise a particular model or price from this research snapshot; those require provider-specific verification during implementation.

Three separate facts matter: **credentials detected**, **a model route selected**, and **an actual request succeeded**. A successful `/models` check does not establish credits, inference entitlement, or full tool compatibility. Do not run a billable completion merely to turn a status indicator green.

## UI direction

The person is a developer who has just installed Whip and has a task in mind. They should feel that the tool is ready to work and that the next action is obvious.

Domain concepts: execution host, project folder, account, provider/model pair, prompt, session, tool permissions. The visual reference is the existing coding workspace: terminal charcoal, editor-paper white, muted code gray, focused-selection blue, successful-connection green, and attention amber. Map those roles to existing theme tokens, including user-selected themes; do not introduce a new hard-coded palette. Keep the established app typography and terminal font.

The signature is a compact context row that stays with the prompt: **where the work runs · project · provider/model · permission level**. It appears in welcome, connection confirmation, ready composer, and repair; users do not lose context while authenticating.

Replace three common onboarding defaults:

- A full-screen checklist becomes one contextual missing-step panel.
- A wall of provider logos becomes a short list of available accounts and authentication methods.
- A “Setup complete” destination becomes the original composer, with its draft retained and focus restored.

These are structural wireframes, not final visual assets. Reuse existing `ProviderLogo`, dialogs, buttons, model picker, folder picker, theme tokens, and responsive rules.

### TUI

Keep the existing folder-trust step when applicable. After that, resolve readiness before requiring successful agent-session creation: the provider chooser must work against host services even when no model can yet initialize an agent. Reuse the normal TUI authentication/palette machinery in that startup state instead of maintaining a separate questionnaire.

Already configured:

```text
whip   ~/src/project

What would you like to work on?
> |

This Mac · project · <model> / OpenRouter · Ask
/connect providers   /model change model   /help
```

Detected credentials with an unavailable default:

```text
Found OpenRouter on This Mac
Uses OPENROUTER_API_KEY from this host's environment.

  <recommended model>                         Change
  Default for new sessions on This Mac

> Use <recommended model>
  Connect another provider

Enter continue · Esc explore without connecting
```

No connection:

```text
Connect a provider

> Inference.net       Recommended
                      Sign in in your browser
  OpenRouter
                      Paste an API key
  OpenAI / ChatGPT
                      Use your subscription's Codex access

Enter connect · Esc explore · /connect reopen
```

Inference.net leads this list with a dim secondary “Recommended” label. Detected connections precede providers that need setup, following the shared ordering policy above. Existing custom routes are discoverable; adding an entirely new custom API configuration stays an advanced action.

Interaction details:

- The key field masks input and has a visible paste affordance. Never teach users to put a secret in a slash-command transcript or shell argument.
- Entering setup or opening the provider palette must not erase a typed prompt. Returning from login restores it; sending remains explicit.
- A send attempt without a usable route opens the same chooser rather than emitting a low-level missing-provider error.
- Skip/Esc means browse/help/configure later, not “provider successfully configured.” Model-dependent actions remain gated. If normal session creation currently requires a route, keep the user in a lightweight startup view until one is chosen.
- `/auth` remains compatible; a `/connect` alias and palette wording improve discoverability.
- Remove reasoning-display and MCP-import questions from the critical path. Preserve existing preferences; on clean installations defer optional import activation until chosen in Settings or its command. This change in fresh-install behavior must be documented and tested.
- Narrow terminals wrap method descriptions and status text; no mandatory multi-column layout.

### Shared web and desktop app

Use the welcome canvas with a single main column aligned to the existing composer. Connection work appears adjacent to it in the shared connection dialog/panel. Keep host selection compact; for a new local desktop install, “This Mac” is the initial context, with remote setup secondary. Honor existing saved remote selections for returning users.

No connection, proposed final screen:

```text
                       What would you like to work on?

                ┌──────────────────────────────────────────┐
                │ Describe a task…                         │
                │                                          │
                │ Choose folder  ·  Connect a provider     │
                └──────────────────────────────────────────┘

                Connect a provider to send your first message
                Inference.net · Recommended              →
                  Sign in in your browser
                OpenRouter                              →
                  Use API key
                OpenAI / ChatGPT                        →
                  Use subscription

                This Mac                             Change
```

Ready:

```text
                       What would you like to work on?

                ┌──────────────────────────────────────────┐
                │ Explain how authentication works here.   │
                │                                          │
                │ project · <model> / OpenRouter · Ask  ↑   │
                └──────────────────────────────────────────┘

                This Mac
```

Behavior:

- Allow drafting before authentication. Connect replaces the unavailable Send action, with a reason. Successful authentication returns to the same draft and shows the chosen route; it does not automatically submit.
- Show detected credentials instead of the provider list whenever applicable. If the current route is already usable, omit the onboarding panel entirely.
- Replace the primary absolute-path text field with the existing folder picker and a compact selected-folder label. Keep manual path entry available. Browser folders are on the execution host; use its existing directory picker rather than the browser's local filesystem picker.
- A folder is still required for coding work. Ask when it is needed, retain an explicit launch/prefill folder, and preserve the existing trust/permission policy. Do not infer consent to operate in the user's home directory.
- Once folder and provider are ready, the first Send creates the session and submits through existing durable command handling. Do not create an empty session merely to hold onboarding state. This is a second-release change because uncertain creation/acceptance needs careful recovery.
- Put permission level in its normal composer control, defaulting according to existing policy. Do not add a first-run permission questionnaire or select Full access to reduce setup steps.
- Keep errors local to the affected control. Dialogs restore focus; keyboard and screen-reader users can connect, copy a code, select a folder, and send without pointer-only controls.

### Fresh desktop installation

```text
Set up Whip on this Mac

Install the local service that runs your sessions.

[ Set up this Mac ]

Connect to another machine       Advanced options
```

That action composes the existing bundled installation and connection operations, then advances to provider setup. If a compatible installation already exists, attach according to the existing lifecycle policy. Installation errors show Retry and Details; executable paths, health diagnostics, and manual selection remain available under Advanced options.

Preserve runtime compatibility checks and existing ownership/restart safeguards. Do not silently replace an incompatible installation, switch distribution homes, or take over a separately managed remote service. Browser-only users never see an executable installation screen.

## Authentication and failure details

- **API keys:** one masked input, an official provider key-management link, concise billing/account context, and Connect. Keep the existing bounded validation. Clear the secret on submission, dismissal, and unmount per the canonical frontend contract; preserve the non-secret prompt and selection on failure. A retry may require pasting the key again. Never put the key in draft, Query, command recovery, analytics, or logs.
- **Browser login:** start from a user action. Open the verification page automatically where the platform permits; keep a clear fallback link and Copy code button. In browsers, account for popup restrictions around asynchronous URL creation. “Waiting for sign-in” is clearer than internal state names.
- **Workspace/project choices:** skip a sole eligible workspace. A sole eligible existing project can be selected within the explicit connection flow, with the selected account/workspace/project visible. With multiple choices, ask; with no project, require an explicit Create and continue action. Do not silently create resources or make billing choices.
- **ChatGPT:** reuse valid credentials already saved by Whip. Explain the existing device-code requirement when it applies; do not imply that a browser already being signed into ChatGPT guarantees a provider connection. Treat a different OAuth mechanism as a later feasibility study, not a prerequisite.
- **Cancellation and return:** a connection flow belongs to its initiating host. Closing observation does not cancel unrelated daemon work. Explicit Cancel targets that flow. A remount/reconnect observes existing flow state instead of beginning a second login automatically.
- **Credential source:** use copy such as “Found in This Mac's environment” or “Managed by Inference CLI on build-server.” Do not copy a local credential to a remote host or override a saved credential because another source exists.
- **Environment changes:** explain that an already-running host must be restarted to see new exports. Offer the existing explicit restart path when appropriate; never silently interrupt shared sessions during detection.

| Failure | Recovery |
| --- | --- |
| Invalid or expired credentials | Re-enter key or sign in again; keep the prompt |
| No credits or missing account entitlement | Explain the account issue; offer the provider's relevant account page; no automatic purchase |
| Model unavailable or incompatible | Offer compatible models on the same provider, then an explicit provider change |
| Rate limit | Explain retryability and retry timing if known; no silent vendor fallback |
| Network/catalog timeout | Retry discovery; keep valid saved/cached selections and distinguish unknown from unavailable |
| Host disconnected or replaced | Retain the original host context disabled; reconnect or explicitly choose another host |
| Configuration revision conflict | Refresh and show the current selection; do not overwrite another client's change |
| First-send outcome uncertain | Resolve existing command/session status before retrying; never create a duplicate request automatically |

Translate errors into these categories where the upstream response supports them. Do not guess that every 403 means billing, or expose raw response bodies containing account details.

## Architecture and likely implementation surfaces

Follow [the canonical frontend guide](../../../docs/frontend.md), particularly host ownership, effect lifetimes, command recovery, drafts, and provider configuration. Existing roadmap work already covers provider connections and ChatGPT support; this work completes their first-run use, rather than replacing those systems. [Roadmap](../../../docs/roadmap.md#L69).

### Minimal service shape

- Keep provider preference in the existing preset metadata and inventory order, adding only the small recommendation marker needed by both clients. Clients preserve that order within their status groups and share the stated label rules; do not maintain independent priority lists. Presentation preference never participates in automatic request routing or overrides model compatibility.
- Extend the existing `provider.list` response with a small optional **selection/readiness summary**: resolved default pair, reason it needs attention, and a suggested pair when one is supported by known metadata. Reuse existing provider statuses rather than adding a parallel provider registry. Field names are provisional; keep the typed contract additive and versioned through the existing generation workflow.
- Compute that summary in the host from configuration, credential metadata, and existing cached catalogs. `provider.list` stays local and bounded; never make loading the screen execute credential commands or contact every provider.
- Use the existing catalog query/refresh path separately when the user chooses a provider or a needed catalog is missing. Recompute recommendations after that result; include the configuration revision and never apply stale suggestions silently.
- Use `configuration.update` for the explicit default pair write. Authentication and choosing a default are separate durable operations with understandable partial-success recovery. Normalize unsupported reasoning effort as the current Settings picker already does.
- Reuse provider login begin/list/select/cancel and key-setting operations. Add only the smallest operation needed to finish a valid saved subscription's missing route, if the existing service cannot expose that safely without a fresh login.
- Persist no new global wizard state. Provider configuration is host-owned; model/permission overrides remain session-owned; display preferences remain device-owned. Any pending welcome prompt uses existing bounded draft infrastructure or an explicit bounded extension scoped to host identity, never an unbounded new store.

| Surface | Likely files / responsibility |
| --- | --- |
| Host configuration and recommendation | `internal/config/providers.go`, `internal/config/config.go`, `internal/daemon/provider_list.go`, `provider_service.go`, `provider_openai.go`; route validity and explicit revision-checked defaults |
| Contract and SDK | `internal/protocol/provider_types.go`, generated `packages/protocol`, `packages/sdk/src/services.ts`; expose host facts without moving routing policy into clients |
| TUI | `internal/tui/setup.go`, `setup_host.go`, `client.go`, `registry.go`, existing auth/palette code; provider setup before agent initialization, masked entry, draft return, remove unrelated startup questions |
| Shared app | `packages/app/src/welcome.tsx`, `settings/provider-connections.tsx`, `settings/provider-login.tsx`; extract reusable connection UI without a second auth state machine |
| First prompt and recovery | `packages/app/src/composer.tsx`, `compositions.ts`, `runtime.ts`; reuse durable create/send handling and bounded draft ownership |
| Desktop | `apps/desktop/src/runtime.ts`, `packages/app/src/host-dialog.tsx`; compose existing install/connect operations, preserve platform boundaries |
| Session/agent integration | Existing session creation and route resolution paths; verify resumed choices and auxiliary model calls. No new session schema is expected merely for onboarding. |

No background goroutine is needed for local selection policy. Existing login/catalog operations retain their bounded contexts and owners. React effects observe state and clean up subscriptions; mutations originate from explicit actions. Host changes invalidate pending suggestions and stale UI completions.

## Ordered delivery plan

### Release 1 — remove the provider dead ends

1. **Pin baseline cases and model recommendations.** Reproduce clean configuration, OpenRouter-only environment, saved ChatGPT credentials, and multiple-provider startup using temporary homes/fake services. Record current actions and first-call failures. Select/evaluate the small initial recommendation set.
2. **Implement shared readiness and explicit selection.** Extend host metadata and typed contracts, including the shared Inference.net preference; validate the chosen pair; persist both default fields with revision checks. Handle detected credentials, disabled routes, missing configuration, and partial auth success. Leave catalog probing outside `provider.list`.
3. **Replace TUI's provider questionnaire.** Reuse normal authentication UI before model-dependent session creation. Skip setup with a usable route; show detected connections; include all existing presets; mask keys; hand back to the prompt. Remove unrelated preference questions and document fresh-install MCP import behavior.
4. **Bring connection setup into web/desktop welcome.** Extract Settings connection components, retaining their host pinning and secret handling. Add inline readiness/provider setup and model confirmation; gate model-dependent session creation. Keep the current folder/create flow for this release to limit recovery changes.
5. **Improve login details and run cross-client acceptance.** Skip redundant choices, add code copy/open controls, classify failures, retain prompts, verify reconnect and session resume. Test clean native installation so its remaining friction is explicitly tracked for Release 2.

Release 1 is complete when users can connect any currently supported preset and create usable work without manually repairing the default pair in Settings. Existing valid setups see no provider wizard.

### Release 2 — make first launch feel like the normal product

6. **Make welcome a composer.** Add pre-session drafting using existing bounded storage, folder selection, and one explicit first Send. Reuse durable session creation and submission; reconcile uncertain results before any retry. Return focus after authentication.
7. **Compose desktop install and connect.** Add the “Set up this Mac” path over existing operations. Test missing, compatible, incompatible, and externally managed runtimes; preserve remote choice and diagnostics.
8. **Validate and tune the full installation-to-response journey.** Run isolated automated acceptance, fix observed failures, and update first-run documentation/screenshots. The user has skipped the proposed usability study.

Later, only if evidence warrants it: add a specific high-demand provider, investigate a simpler officially supported subscription-login mechanism, or design an explicit scratch workspace. Do not make those expansions dependencies of the initial improvement.

### Implementation handoff

Preparation completed:

- [x] Product direction and exceptions finalized, including Inference.net ordering and label copy.
- [x] Existing source paths, test suites, contract generator, and validation commands identified.
- [x] TUI/web/desktop wireframes and first-release scope aligned with the final ordering policy.
- [x] Acceptance criteria cover provider preference without overriding readiness or saved choices.

Implementation sequence and exit checks:

| Work package | Depends on | Concrete exit check |
| --- | --- | --- |
| Host policy and contract (steps 1–2) | None | Extend `internal/config/providers_test.go`, `internal/daemon/provider_connections_test.go`, and `provider_service_test.go` with the ordering/readiness cases below. Add fixtures for no credentials, OpenRouter-only, both providers, disabled Inference.net, and revision conflicts. Confirm inventory remains free of network/credential-command side effects. Regenerate the TypeScript contract. |
| TUI first-run flow (step 3) | Host policy and contract | Extend `internal/tui/setup_test.go` and existing auth tests; verify pre-session connection, masked entry, `/connect` alias, explicit route confirmation, skip, and preserved prompts. |
| Shared welcome connection flow (steps 4–5) | Host policy and contract | Extend `packages/app/test/provider-connections.test.tsx` and add welcome coverage; verify grouping/label exceptions, direct alternate-provider setup, host pinning, focus, and secret handling. |
| Welcome composer (step 6) | Release 1 acceptance | Extend `packages/app/test/composer.test.tsx` and `runtime.test.ts`; demonstrate recovery from uncertain create/send without duplicate acceptance or lost later draft edits. |
| Native setup (step 7) | Shared welcome flow | Extend `packages/app/test/local-runtime.test.tsx`, `apps/desktop/tests/runtime.test.ts`, and `provider-environment.test.ts`; smoke-test the packaged app using an isolated profile. |

Implementation followed host policy/contract work with the TUI and shared app, then native setup and acceptance. The implementation record above tracks completion; the table retains the planned dependency order.

Validation commands verified in the current repository:

- Contract changes: `npm run generate`, then `npm run check -w @whip/protocol`.
- Go changes: focused package tests during development, then `task check`; run race tests for affected concurrent paths.
- Shared UI changes: `task web`; SDK changes additionally run `npm run check -w @whip/sdk` and `npm test`.
- Native runtime changes: `npm run check:desktop` and `npm run test:desktop`, followed by the isolated packaged-app smoke test.

Use the repository's Node 24 toolchain. Generated contracts come from the Go registry. Model candidates are recorded above; bounded provider errors distinguish rejected credentials, billing, authorization, rate limits, timeouts and connection failures without exposing provider bodies. Application code is implemented; the installed production app and shared daemon have not been upgraded during this work.

## Acceptance and measurement

Measure both **time to a usable composer** and **time to the first streamed response**. Break out provider browser/billing time and inference latency; the app does not control them. These are proposed targets, not observed performance claims:

- Valid existing route: zero provider setup interactions and zero repeated preference questions.
- Sole detected usable connection with a broken default: one confirmation to select the recommended pair, excluding folder trust/choice.
- API-key path: select provider, paste/connect, and at most one recommendation confirmation; no Settings navigation.
- Account login: no redundant authentication-method or singleton workspace/project picker; no forced model-catalog browsing.
- Any failed/canceled connection: the user's non-secret prompt survives, and retry has an obvious action.
- Normal restart/resume: no repeated setup if credentials and the route are still usable; existing session model and permissions are unchanged.
- First Send after recovery: at most one accepted user request for the logical submission.
- Inference.net preference: first among comparable provider choices with one subtle setup label; zero added interactions for connecting or continuing with another provider.

**Deferred by user instruction:** the proposed 6–8 person formative usability study is skipped for now. Automated interaction tests, isolated terminal checks and desktop acceptance remain in scope. Scripted timing samples are diagnostic observations, not human usability or conversion results.

Start with local/manual measurements. A production analytics system is not a dependency. Any later instrumentation should count milestones without recording keys, prompts, account identifiers, or environment values.

| Test case | Required result |
| --- | --- |
| Clean home, no keys | Inference.net first with one “Recommended” label; all three supported choices directly accessible; no false completion; can skip safely |
| Only Inference.net environment key | Detection visible; valid default proceeds without re-entering a key |
| Only OpenRouter environment key, shipped defaults | OpenRouter is the immediate available path; explicit model/provider confirmation makes it usable without an Inference.net detour |
| Several credentials, valid existing default | Preserve the default; alternatives remain discoverable |
| Several credentials, invalid default | Inference.net first within the available group; ask for account/route choice; no arbitrary billing-account selection |
| Disabled provider or custom endpoint | Respect opt-out/explicit endpoint; discovery does not overwrite configuration |
| Disabled/broken/custom-endpoint Inference.net | No recommendation label or automatic enablement; working alternatives remain the immediate path |
| Provider search / existing model selection | Search relevance and selection persist; preference only orders comparable results |
| Alternate provider selected or connected | Same direct setup path; no added promotion, confirmation, or delayed return to composer |
| Credential command configured | Inventory does not execute it; distinguish unchecked from missing |
| Saved ChatGPT token, missing route / expired token | Finish setup or reauthenticate as appropriate; no false connected state |
| `/models` succeeds but inference fails | Actionable request error; no claim that catalog validation guaranteed a call |
| Login canceled, expired, or completed while detached | No duplicate login; observe authoritative state on return |
| Config changed by another client | Conflict shown; no stale recommendation overwrites newer defaults |
| Desktop Finder launch / newly exported environment key | Existing shell recovery works; stale daemon environment gets accurate guidance |
| Fresh native install / install failure / incompatible runtime | Clear install-connect progression or scoped recovery; no takeover |
| Remote host / host removed during login | Credentials and completion remain bound to the original execution host |
| Narrow TUI / keyboard-only app / screen reader | Controls and error recovery remain accessible; focus returns to draft |
| Restart/resume | Credentials/default pair persist; existing session model/permissions survive |
| First-send creation or acknowledgement interrupted | Resolve durable status; no duplicate session/request; preserve later draft edits |
| Tool use, title generation, compaction on selected provider | First-call success is not followed by an unseen dependency on another unconfigured provider |

Implementation validation should use focused service tests with fake provider endpoints, headless TUI tests, shared-app interaction/recovery tests, and a packaged desktop smoke test in an isolated profile. Run the repository's required checks when implementation lands, including race checks for changed concurrent paths. Real provider calls require an explicitly initiated smoke test and an appropriate test account; never automatically use the user's production account during onboarding research.

## Documentation and finalized decisions

Current documentation is maintained in `docs/features.md` (behavior, code and tests), `docs/frontend.md` (welcome/provider composition and draft ownership), `docs/roadmap.md` (delivered milestones), and README first-run instructions for TUI and desktop.

Final product decisions:

- Inference.net first among comparable provider choices, with a subtle “Recommended” setup label; usable detected credentials and saved choices take priority.
- Folder required before coding work; no new scratch-chat mode in the initial releases.
- Existing three presets; model recommendations evaluated before release.
- No forced MCP imports, preference tour, permission escalation, or paid test request.
- `/connect` aliases the existing `/auth` flow; both clients use the same host-owned ordering/readiness policy.

No product questions remain open for the first implementation package. The preference question from research is resolved by the user's instruction to subtly recommend Inference.net without making the experience worse.

Research limitations: OpenCode findings come from repository source and official documentation, not a live comparative test. Implementation acceptance and its precise fixture/package scope are recorded in [VALIDATION.md](VALIDATION.md). Public-provider entitlement and model quality remain account/service dependent; the UI distinguishes detected credentials, usable route metadata and successful execution. No human usability study was conducted, and installed production software was not changed.


## Integrated TUI completion — September 9, 2026

The follow-up [TUI integration and acceptance record](TUI-INTEGRATION.md)
replaces the initial standalone startup screen with the normal TUI and shared
provider dialog. It records the cleanup, reconnect and draft guarantees, review,
full checks, and actual Docker first-call/reopen validation.
