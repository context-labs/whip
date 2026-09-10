# Configure providers in the TUI, persist them in files

Branch: `codex/provider-onboarding`

Presentation update: the picker, key prompt and tall form described below are
superseded by [the approved picker redesign](PICKER-REDESIGN.md). The file-backed
configuration and management contracts remain in place.

Status: **Implemented and validated.** September 9, 2026. This extends the integrated provider dialog already on this branch. See [the validation record](VALIDATION.md).

## Implementation notes

- Shared configuration CRUD, no-auth requests, generated contracts, SDK methods and the complete TUI form/management flow are implemented.
- `task check`, focused and full race checks, generated-artifact checks and independent adversarial review passed. The SDK suite includes 285 tests and the protocol interop suite includes 12 tests.
- Clean Docker acceptance passed: API-key creation, explicit tool execution, endpoint edit and same-session reload, manual/no-auth fallback, shared web inventory/defaults, daemon restart and TUI resume.
- Review fixes include conflict refresh rebasing, browser re-login reload, explicit selection of a new manual alias after endpoint changes, and no-auth admission for standalone ACP.
- Narrow scope adjustment: deleting the last configured provider when there are no model aliases is blocked with disable guidance. This preserves the existing empty-configuration recovery guard without introducing another persistence format or weakening ordinary writes.
- Docker acceptance uses host port 4001 because the user's existing onboarding container owns 4000. The launcher and its normal port remain unchanged.

## Goal

A user can connect a built-in provider or add a custom OpenAI-compatible endpoint entirely inside the TUI, select a model, and start a session. They can reopen the dialog to edit the connection. Restarting Whip preserves the configuration because the execution host writes its existing configuration files.

The TUI edits the same provider definitions that advanced users can edit by hand. There is one configuration model and one daemon mutation path, with no provider table in SQLite and no second source of editable truth.

## Scope and non-goals

In scope:

- Add and edit custom OpenAI-compatible providers from first-run setup and `/connect`.
- Keep the current built-in API-key and browser-login experiences, backed by shared provider configuration logic.
- API-key entry, a named environment-variable reference, and explicitly unauthenticated local/custom endpoints. The last option requires a small, backward-compatible runtime extension; an empty key does not currently mean “no authentication.”
- Model discovery, manual model entry when discovery is unavailable, explicit model/provider selection, and safe connection management.
- Host APIs and typed Go/TypeScript clients reusable by web/desktop later.

Out of scope:

- Web/desktop custom-provider forms in this implementation, a new onboarding program, or a general configuration editor.
- Native Anthropic/Gemini protocols, a generic Responses adapter, custom headers, arbitrary OAuth providers, or a provider marketplace.
- A database migration, a new credential vault, moving existing account files, or changing session ownership of model/provider selections.
- TUI authoring of secret commands, a complete model-alias editor, automatic paid test requests, or a usability study.

## Current implementation and the gap

| Existing surface | What to reuse or extend |
| --- | --- |
| [`internal/config/config.go`](../../../internal/config/config.go), [`providers.go`](../../../internal/config/providers.go) | `Provider`, `Model`, presets, effective configuration, environment discovery, and the existing file writer. Custom endpoints already fit the provider map. |
| [`internal/config/revision.go`](../../../internal/config/revision.go) | Read the current revision and patch freshly loaded configuration through `UpdateVersioned`. |
| [`internal/daemon/provider_service.go`](../../../internal/daemon/provider_service.go) | Host-owned provisioning and validation. `SetProviderKey` accepts configured providers and known presets, but cannot create an unknown provider's endpoint definition. |
| [`provider_list.go`](../../../internal/daemon/provider_list.go), [`provider_model.go`](../../../internal/daemon/provider_model.go) | Redacted inventory, readiness, model discovery, and common runtime client construction. Inventory currently lacks enough metadata to populate an editor. |
| [`internal/tui/setup.go`](../../../internal/tui/setup.go), [`setup_host.go`](../../../internal/tui/setup_host.go), [`startup.go`](../../../internal/tui/startup.go) | Existing modal, host client, request ownership, cancellation, search, model selection, and preserved composer draft. Extend these instead of adding another setup flow. |
| [`internal/daemon/client_control.go`](../../../internal/daemon/client_control.go) | Existing session reload. Changing a provider's endpoint or credentials does not automatically replace an already constructed session client; reselecting the same model/provider can also skip reconstruction. |

## TUI experience

### Entry and provider list

Use the same dialog for initial setup and `/connect`:

```text
Connect a provider                                      esc
> Type to search…

Inference.net                                  Recommended
OpenRouter
OpenAI (ChatGPT subscription)

+ Add custom provider…
```

Working connections remain in the available group when present. Preserve the host's order: Inference.net first among comparable preset choices, with one subdued recommendation. Never replace a valid saved route to promote it. Show “Connect another provider” only when connections already exist.

“Add custom provider…” is an explicit action, not a fabricated provider ID. Keep it discoverable when filtering produces no matches. Search remains a focused text input; arrow keys move the list. Existing providers expose a visible **Manage** action with edit, reconnect, disable, and applicable removal actions.

### Custom connection form

```text
Add custom provider                                    esc

Name          My endpoint
Base URL      https://api.example.com/v1
Authentication  API key / Environment variable / None
API key       ••••••••••••••••••••

Advanced      Provider ID: my-endpoint

                         [Save and choose model]
```

- Name and base URL are required. Explain that the URL is the API root, such as a `/v1` endpoint, not a `/chat/completions` URL. Do not guess or append `/v1`.
- The protocol is OpenAI-compatible Chat Completions. Do not expose a selector offering unsupported transports.
- Suggest a stable ID from the name; allow adjustment before creation under Advanced. Once saved, the ID is immutable because sessions and model aliases reference it. Display names remain editable.
- API keys are masked and never prefilled from stored secrets. On edit, show “Keep existing credential”; replacing it is explicit. Environment mode asks for the variable name and shows whether it exists on the **execution host**, not the TUI machine.
- “None” is an explicit choice for endpoints that require no authentication; it must not silently follow from an empty input.
- Keep advanced settings collapsed. Tab/Shift+Tab change fields, arrows change options, and Enter activates the focused action. Escape returns one level and retains the in-memory form until the dialog is closed. Closing the dialog releases its secret input and leaves the chat draft intact.
- At small heights, scroll the form around the focused field and keep the action reachable. Reuse current theme tokens, input rendering, and list windowing; do not squeeze an entire form into a fixed-height screenshot layout.

### Models and completion

1. Validate the proposed connection with one bounded model-discovery request. A successful `/models` response means models were loaded, not that inference has been tested.
2. If models are returned, use the existing searchable model picker. Do not select the first arbitrary catalog model as a recommendation.
3. Offer **Enter model manually…** in the model picker and on discovery failures. Ask for the exact API model ID; context size and maximum output are optional advanced overrides using existing model semantics. Store an explicit route under a unique alias, suggesting `<provider-id>/<model-id>`, and never overwrite a colliding alias.
4. Authentication failures keep the form open for correction. Unsupported/empty model listings and network failures allow explicit **Save without verification** with a manual model. A missing environment variable can be saved for later, but the connection remains unavailable until the host can resolve it. None of these paths claim a successful model call.
5. Confirm the exact model/provider pair through the existing selection flow. “Use for new sessions” controls saved defaults; authentication alone never changes them. Return to the preserved draft and wait for the user's explicit send.

The connection is persisted when the user saves it. Canceling the later model picker does not secretly delete the connection. A manual alias is persisted only when its own save succeeds; unrelated aliases stay unchanged.

### Editing and removing

- Load a redacted editable view on entry; preserve credential references when changing unrelated fields.
- Changing an endpoint requires an explicit credential decision before any request goes to the new URL. Do not send the old key or resolve its secret command against a newly entered destination without that decision.
- Built-in account endpoints stay fixed in their normal connection UI. A different endpoint is a custom provider with a distinct ID. Continue supporting existing file-authored overrides without silently resetting their URL or enabling incompatible browser login.
- After an endpoint, credential, or model-limit edit affecting the current session, offer **Reload current session** using the existing daemon operation, preserving its model/provider and permissions. A running turn completes under the existing reload rules. Other open sessions use the changes when reloaded or reopened; do not imply that saving has replaced every live client.
- Keep **Disconnect/disable** separate from **Remove custom provider**. Built-ins use the existing disconnect/disable behavior. Custom removal is allowed only when no configured model alias, default, or compaction route references it; otherwise explain the references and offer disable. Do not silently rewrite defaults, shared aliases, or historical sessions to make removal succeed.
- Removing an unused custom definition also clears its stale disabled entry and cached catalog. Historical sessions retain their provider ID and show the existing unavailable-route recovery if resumed without that definition.

## Host API and data design

Add a small set of host operations alongside the current provider APIs:

| Operation | Request and result |
| --- | --- |
| `provider.get` | Provider ID → revision, editable non-secret definition, credential-source summary, relevant configured aliases, and removal blockers. Reading never executes credential commands or contacts a provider. |
| `provider.create` | Expected configuration revision, new ID, definition, explicit credential input, optional manual model, and explicit unverified-save intent → committed revision, redacted provider view, and discovery outcome. Reject an existing or reserved ID. |
| `provider.update` | Expected revision, existing ID, supported field patch, explicit credential action, and optional manual-model patch → the same result shape. Omitted fields remain unchanged. |
| `provider.remove` | Expected revision and custom ID → new revision; reject references and built-in IDs. |

The definition contains `name`, `base_url`, and the supported `api` value. Credential input is a tagged choice: **keep**, **api_key**, **environment**, or **none**; keep is edit-only. Secret fields appear only in mutation requests. Returned source metadata includes whether a credential is configured and the environment-variable name when applicable, never the key or a secret command's contents.

Use named request/result structs in [`internal/protocol/provider_types.go`](../../../internal/protocol/provider_types.go) and register operations in [`registry.go`](../../../internal/protocol/registry.go). Secret-bearing create/update operations use the existing sensitive-payload handling and host-scoped ephemeral RPC path. They never enter session command history, draft storage, recovery journals, or SQLite. Generate protocol artifacts from Go and add methods to the existing Go clients and SDK provider service.

Factor shared definition validation and configuration patching out of existing provider setup code. Built-in key setup and the new API use those helpers. Browser-login adapters retain their account-specific behavior and write through the same configuration model; do not replace functioning login flows with a generic form or duplicate their state.

### Persistence and authentication

| Data | Storage |
| --- | --- |
| Provider identity, name, URL, API, explicit key/reference, optional no-auth setting | Execution host's existing `config.json`, under `providers[id]`. |
| Manually configured model ID, alias, route and optional limits | Existing `models` map in the same file. Discovered catalogs are not copied wholesale into configuration. |
| Default model/provider pair | Existing configuration fields, changed only by explicit selection. |
| Browser/account credentials | Existing provider-specific account files and auth managers. |
| Discovered models | Existing rebuildable catalog cache. |
| Session's selected model/provider and permissions | Existing session storage, unchanged by this feature. |

Resolve the actual directory through `config.Dir()`, including Whip/whipcode and their home overrides. Never write client-machine files for a remote execution host. Hand editing remains supported, with current reload semantics.

For unauthenticated endpoints, add optional `auth: "none"` to `config.Provider`; omission retains today's key-resolution behavior. Reject contradictory key/reference fields with this value. Update key status, readiness, common client construction, model discovery, and every generic request path so no `Authorization` header is emitted. Do not bypass the dedicated subscription adapter. Existing configs require no migration, and existing secret-command references remain valid without a TUI editor for them.

Literal-key input must not accidentally become a `!command` or environment reference through the existing secret parser. Reject reference syntax in that mode with guidance to choose environment mode or keep the existing file-authored reference. Inventory and editor reads never resolve those commands.

## Validation, writes, and operation lifetime

- Validate IDs, supported API/auth combinations, unique aliases, positive optional limits, and HTTP(S) URLs on the daemon. Reserve preset IDs and legacy `inference`. Reuse URL restrictions against embedded credentials, query strings, and fragments; permit explicit local HTTP endpoints.
- Run discovery with a bounded context against the proposed route before committing a normal create/update. Allow the explicit unverified/manual-model path described above. No completion request is issued automatically.
- Patch current disk state through `UpdateVersioned` at commit, checking the supplied revision again after network work. Provider definition, credential choice, and any submitted manual alias commit in one configuration write. Do not replace configuration with a stale client snapshot.
- Reuse the existing provision ownership and lock order; never hold the configuration file mutex over network I/O. Cancel superseded login flows and invalidate catalogs only for accepted changes. Guard subsequent cache writes against a route or credential changing during discovery.
- Atomic replacement and current restrictive file permissions remain the writer's responsibility. Preserve unrelated supported fields semantically. The existing serializer can reformat JSONC and discard comments/unknown fields; exact text preservation is not promised by this feature.
- The revision check catches concurrent daemon-client updates and external edits visible before the final read. The existing mutex is process-local; this plan does not claim cross-process locking against an editor racing the actual write, or introduce a general file-watcher/locking project.
- Stale revisions return a conflict with a refresh action. Preserve the user's unsaved non-secret form edits for comparison; do not silently overwrite the newer version or automatically replay a key submission.
- Reuse `providerSetup.call`, its owner/request checks, dialog context, and existing timeout/poll lifecycle. No additional WebSocket connection, permanent goroutine, poll loop, or channel ownership is needed. Ignore replies belonging to an old dialog/request.
- Cancellation before commit leaves configuration unchanged. Cancellation or transport loss after commit can have an uncertain client outcome: reread the stable provider ID and revision before offering an explicit retry. Do not promise rollback or transparently duplicate a create.
- Report successful configuration persistence even if subsequent catalog caching fails; show the cache error as recoverable. Clear secret inputs after acknowledged save, cancellation, or dialog closure and keep upstream errors/logging redacted.

## Implementation surfaces

| Area | Expected files |
| --- | --- |
| Configuration/auth mode | `internal/config/{config,providers,secret}.go` and relevant tests; use `revision.go` as the existing writer. |
| Provider definitions and validation | Small focused `internal/daemon/provider_configuration.go` with tests; reuse helpers from `provider_service.go`; extend `provider_{list,model,rpc,client}.go` as needed. |
| Generic unauthenticated requests | `internal/llm/openai.go` and request tests covering models, streaming, and non-streaming completions. |
| Wire contract and clients | `internal/protocol/{provider_types,registry}.go`, generated `packages/protocol` artifacts, `packages/sdk/src/services.ts`, Go/SDK contract tests. |
| TUI | `internal/tui/{setup,setup_host,startup}.go`; extract the custom form into `setup_provider.go` if needed to keep the existing controller readable; reuse `ui.List` and existing text inputs. |
| Current-session reload | Existing TUI control dispatch and `internal/daemon/client_control.go`; reuse `session.reload`, modifying the daemon only if a concrete missing behavior is found. |
| Documentation | README, `docs/models-providers.md`, provider/onboarding sections of `docs/features.md`, API/state ownership notes in `docs/frontend.md`, and `docs/roadmap.md`. Update concurrency docs only if ownership rules actually change. |

## Prior art and what to take from it

OpenCode's app custom-provider form collects a provider ID, name, base URL, key, models, and headers, then writes credentials and provider configuration through separate APIs. Its configuration API persists files; a form does not require a database migration. Adopt the form-to-shared-API pattern while using Whip's existing configuration and credential ownership, without copying its SDK-package selector or every advanced field. [Custom provider dialog, inspected commit `b6914b39`](https://github.com/anomalyco/opencode/blob/b6914b39db86e196ebcc95e92a0188cdf58ef67a/packages/app/src/components/dialog-custom-provider.tsx#L138), [form validation](https://github.com/anomalyco/opencode/blob/b6914b39db86e196ebcc95e92a0188cdf58ef67a/packages/app/src/components/dialog-custom-provider-form.ts#L51), [file-backed configuration update](https://github.com/anomalyco/opencode/blob/b6914b39db86e196ebcc95e92a0188cdf58ef67a/packages/opencode/src/config/config.ts#L656).

OpenCode's documented terminal custom-provider setup still separates connecting a credential from declaring endpoint/model configuration. Whip should complete both in the terminal dialog so users can reach the first call without opening JSON. The inspected standard auth path writes `auth.json`; this is a reference for that flow, not a claim about every OpenCode credential subsystem. [Custom-provider documentation](https://opencode.ai/docs/providers/#custom-provider), [standard auth storage](https://github.com/anomalyco/opencode/blob/b6914b39db86e196ebcc95e92a0188cdf58ef67a/packages/opencode/src/auth/index.ts#L10).

Whip's current architecture remains authoritative: [frontend provider ownership](../../../docs/frontend.md), [models and providers](../../../docs/models-providers.md), and [integrated TUI setup](../provider-onboarding/TUI-INTEGRATION.md). Earlier research plans are context, not an additional implementation contract.

## Test and acceptance plan

Use deterministic local provider fixtures; no real keys or paid requests are needed.

| Area | Required evidence |
| --- | --- |
| Persistence and compatibility | Create/edit survives daemon restart; home overrides work; existing presets, account files, disabled providers and secret references retain behavior; unrelated settings survive; malformed input makes no write. |
| Concurrency and failure | Two clients editing the same revision; file edit before final commit; cancel before/after commit; transport loss; disk write failure; stale discovery/cache results; no partial provider/model update or duplicate create. |
| Auth and redaction | Key replacement/keep/environment/none; missing host variable; endpoint change cannot silently reuse credentials; read operations never run commands; no secrets in responses, logs, histories, recovery records, or errors. No-auth emits no bearer header on all generic request paths. |
| Models | Normal discovery, empty/unsupported `/models`, 401/403, timeout, manual model with exact API ID, alias collisions, shared aliases, limits, and explicit unverified save. No arbitrary default/model switch. |
| TUI interaction | Entire flow using keyboard; visible search cursor; focus order, paste/backspace, Escape, resize and scrolling; 32/48/80-column terminals in dark/light themes; draft retention; late replies cannot update another form. |
| Management/session behavior | Edit preserves provider ID; referenced removal is blocked; disable/reconnect works; same-route credential/URL edit is applied through explicit reload; permissions and history survive; resumed unavailable routes offer repair. |
| Shared contract | Strict request validation, sensitive operation registration, generated-artifact consistency, Go client and SDK methods, and clear unsupported-operation behavior against an older host. |

End-to-end acceptance uses `task onboarding:docker` built from the working tree: start clean, add a local fixture endpoint through the TUI without editing JSON, choose a model, explicitly send a prompt, and exercise a deterministic tool call. Confirm web inventory at `http://localhost:4000` sees the same provider even though this scope adds no web form. Restart the TUI/daemon inside the same container to verify persistence; a new clean container should start unconfigured. Also test manual-model/no-auth setup and an edit followed by reload. Clean up only test-owned processes and containers.

Run focused Go/config/daemon/TUI/LLM tests and generated protocol/SDK checks while implementing, then `task check` and race checks for the touched concurrent paths, including `go test -race ./internal/config ./internal/daemon ./internal/tui`. Complete the feature playbook's adversarial review before calling implementation finished. Record actual commands and acceptance results rather than marking planned checks as passed.

## Ordered implementation checklist

- [x] 1. Add the redacted editor view, shared definition/credential validation, optional no-auth behavior, and revision-checked configuration mutations with focused tests.
- [x] 2. Expose create/get/update/remove host operations, route existing key setup through shared helpers, generate protocol artifacts, and add Go/SDK clients and contract tests.
- [x] 3. Extend the existing provider dialog with Add custom provider and Manage, keyboard-accessible fields, masked credentials, and conflict/error recovery.
- [x] 4. Connect discovery and manual-model entry to the existing model/default confirmation flow; preserve drafts and explicit-send behavior.
- [x] 5. Complete edit/disable/remove behavior and explicit current-session reload; test restart/resume and stale-operation handling.
- [x] 6. Run focused checks, the repository gate, race tests, adversarial review, and clean Docker acceptance. Fix discovered issues and record evidence here.
- [x] 7. Update current user/architecture documentation and the roadmap with implemented behavior and source citations; keep this plan's status/checklist accurate.
