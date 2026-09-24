# Provider picker, known endpoints, and automatic connections

Status: **Implemented and verified.**
September 9, 2026.

This is the next iteration of the file-backed TUI provider work. The latest user
request replaces the earlier request to remove manual/advanced controls: replicate
the supplied OpenCode picker and key-prompt experience, add known compatible
providers, show connection checkmarks, and recognize local credentials.
Research completed before implementation. The user approved execution on September 9, 2026.

## Proposed experience

Make the ordinary path **choose provider → enter key → choose model**. If a key
is already available, skip key entry. Keep the existing explicit model/default
confirmation and require an explicit send before running a prompt.

```text
  Connect a provider                                  esc

  Search

  Popular
✓ Inference.net                             Recommended
  OpenRouter
✓ OpenAI                    ChatGPT subscription or API key

  Providers
  Cerebras
✓ DeepInfra
  DeepSeek
  Fireworks AI
  Groq
  Together AI
  xAI

  Other…                                Custom endpoint

  enter select                              ctrl+e manage
```

The illustration shows example connection states, not a report about this host.

- Reproduce the neutral filled modal, bold title, right-aligned `esc`, quiet
  search placeholder, colored section headings, compact rows and full-width
  selection highlight. Use Whip's selected theme rather than hardcoded purple.
- Use **Popular** and **Providers** instead of separating already-connected
  providers into a second list. Connections stay in familiar positions.
- Inference.net is first in Popular with one subdued recommendation. OpenRouter
  and OpenAI follow; remaining providers sort by display name. Search relevance
  takes priority over promotional ordering. Never override a saved route.
- Reserve a two-cell status gutter. Show a green `✓` for a usable connection,
  blank for an unconfigured connection, and an attention marker for a saved
  connection that needs repair. The mark is a connection indicator, not a toggle
  that enables/disables a provider when Space is pressed.
- Remove the literal `>` search prompt. Keep the normal insertion cursor while
  typing, as in the supplied second screenshot. Search stays focused; arrows
  move selection without making the search box unreachable.
- Match provider names, IDs, aliases and categories, prioritizing name matches.
  Preserve selection by stable ID when status refreshes. Keep Other discoverable
  when no known provider matches.
- Enter on a connected provider opens its model choices. `ctrl+e` exposes Manage
  for credential replacement, disconnect/disable and applicable custom edits.
- Show one OpenAI family row, but keep `openai` (API billing) and `openai-codex`
  (ChatGPT subscription) as distinct execution routes. When a choice is needed,
  show a short authentication-method picker with the connection mark beside each
  method. If only one is connected, Enter can use it directly; Manage still
  exposes both. A family row must never become a persisted provider ID.

For an unconfigured API-key preset:

```text
  API key                                             esc

  Groq

  Paste API key

  enter connect
```

Use a masked input, a small provider label, and an optional “Get an API key” link.
No name, URL, provider ID, auth selector, model entry, or Advanced section appears
in this path. Single-method providers go straight here. Inference.net retains its
working browser-login option and API-key alternative; OpenAI retains subscription
login. Successful connection refreshes inventory and opens the existing model
picker. Errors remain inline and preserve the rest of the user's draft.

**Other/custom remains an escape hatch.** Keep custom endpoint support and its
file-backed API. Use short sequential prompts for name, URL and authentication,
matching the key modal, instead of the current tall form. Manual model entry and
advanced limits remain confined to custom setup/management; they do not appear
in a known-provider key prompt. This proposal does not delete existing manual
model configurations or backend/SDK capabilities.

## What OpenCode actually does

Inspected `anomalyco/opencode` dev commit
`b6914b39db86e196ebcc95e92a0188cdf58ef67a` on September 9, 2026.

- A small priority map creates Popular; remaining entries are alphabetical.
  Connection marks derive from the synchronized connected-provider inventory.
  One auth method opens directly, while multiple methods get a selector. After
  key save, it refreshes state and opens model selection. Its Other flow stores
  a credential and directs users to configuration for the actual endpoint.
  [Provider dialog](https://github.com/anomalyco/opencode/blob/b6914b39db86e196ebcc95e92a0188cdf58ef67a/packages/tui/src/component/dialog-provider.tsx).
- The shared picker searches names and categories, weighting names more highly;
  it uses a real focused input with no shell-style prompt prefix.
  [Select component](https://github.com/anomalyco/opencode/blob/b6914b39db86e196ebcc95e92a0188cdf58ef67a/packages/tui/src/ui/dialog-select.tsx),
  [prompt component](https://github.com/anomalyco/opencode/blob/b6914b39db86e196ebcc95e92a0188cdf58ef67a/packages/tui/src/ui/dialog-prompt.tsx).
- Its provider assembly recognizes configured environment-variable names and
  stored API credentials, and skips disabled providers. Its broader provider
  support also depends on SDK adapters and model metadata; it is not just a URL
  table. [Provider assembly](https://github.com/anomalyco/opencode/blob/b6914b39db86e196ebcc95e92a0188cdf58ef67a/packages/opencode/src/provider/provider.ts#L1582).
- The standard credential store is `auth.json` under its data directory. Records
  distinguish API keys, OAuth and well-known auth; the writer uses mode `0600`.
  [Auth storage](https://github.com/anomalyco/opencode/blob/b6914b39db86e196ebcc95e92a0188cdf58ef67a/packages/opencode/src/auth/index.ts).

Replicate the visual and interaction pattern while retaining Whip's daemon,
revision-checked file writes, masked inputs, cancellation ownership and explicit
model/default selection. Do not add OpenTUI, the AI SDK or another state store.

## Known provider catalog

Extend the existing `config.ProviderPresets()` rather than adding a parallel
registry in the TUI. Bundle reviewed definitions so the initial picker works
offline and no remote catalog can silently change where a key is sent.

Initial target set below. These are documented endpoints to integrate, not a
claim that every provider/model has passed Whip acceptance yet. Every enabled
preset needs the discovery and tool-call checks described below.

| Provider / stable ID | API root | Recognized environment key | Source |
| --- | --- | --- | --- |
| Inference.net / `inference-net` | `https://api.inference.net/v1` | `INFERENCE_API_KEY` | [Official quickstart](https://docs.inference.net/api/api-quickstart) |
| OpenRouter / `openrouter` | `https://openrouter.ai/api/v1` | `OPENROUTER_API_KEY` | [Official quickstart](https://openrouter.ai/docs/quickstart) |
| OpenAI API / `openai` | `https://api.openai.com/v1` | `OPENAI_API_KEY` | [Chat API](https://developers.openai.com/api/reference/resources/chat), [model list](https://platform.openai.com/docs/api-reference/models) |
| Cerebras / `cerebras` | `https://api.cerebras.ai/v1` | `CEREBRAS_API_KEY` | [Compatibility](https://inference-docs.cerebras.ai/resources/openai) |
| Groq / `groq` | `https://api.groq.com/openai/v1` | `GROQ_API_KEY` | [Compatibility](https://console.groq.com/docs/openai) |
| DeepSeek / `deepseek` | `https://api.deepseek.com` | `DEEPSEEK_API_KEY` | [Official quickstart](https://api-docs.deepseek.com/) |
| Fireworks AI / `fireworks-ai` | `https://api.fireworks.ai/inference/v1` | `FIREWORKS_API_KEY` | [Quickstart](https://docs.fireworks.ai/getting-started/quickstart), [compatibility](https://docs.fireworks.ai/tools-sdks/openai-compatibility) |
| Together AI / `togetherai` | `https://api.together.ai/v1` | `TOGETHER_API_KEY` | [Compatibility](https://docs.together.ai/docs/inference/openai-compatibility) |
| DeepInfra / `deepinfra` | `https://api.deepinfra.com/v1/openai` | `DEEPINFRA_API_KEY`, fallback `DEEPINFRA_TOKEN` | [Official API](https://docs.deepinfra.com/chat/overview), [OpenCode ecosystem key name](https://github.com/anomalyco/models.dev/blob/5dc2ef24dc14a5820e8fb2423134035e11d07576/providers/deepinfra/provider.toml) |
| xAI / `xai` | `https://api.x.ai/v1` | `XAI_API_KEY` | [Chat compatibility](https://docs.x.ai/developers/model-capabilities/legacy/chat-completions) |

ChatGPT subscription keeps the existing dedicated `openai-codex` adapter and
account files. It is not configured from `OPENAI_API_KEY`.

Use a small preset extension for category/family labels, key-page link, recognized
environment names and reviewed model capabilities. Keep endpoint definitions,
credential rules and ordering on the host; publish only required non-secret
presentation metadata through `ProviderEntry` and regenerate SDK contracts.
Keep the desktop's fixed environment allowlist synchronized with these presets.

Models.dev is useful research input and a source of capability metadata, with a
pinned/reviewed subset where needed. Its provider definitions sometimes delegate
endpoint behavior to SDK adapters, so they cannot be imported blindly. The JSON
endpoint returned 403 during this research; source definitions were inspected at
commit `5dc2ef24dc14a5820e8fb2423134035e11d07576` instead. No runtime dependency on
that service is proposed. [Models.dev](https://models.dev/).

Do not populate the list with every provider in the OpenCode screenshots merely
because it appears there. Azure/Bedrock, Copilot, native Anthropic/Gemini and other
integrations can require additional endpoint inputs or adapters. They need their
own verified connection path. xAI documents Chat Completions as legacy; support
its compatible model subset without promising newer Responses-only features.

## Automatic credential detection

“Automatic configuration” should mean a usable provider is immediately checked
and selectable without entering its URL or pasting a key. It does not require
copying environment secrets into `config.json` or making provider-list reads write
files. Extend Whip's existing effective-provider resolution.

1. Preserve explicit saved definitions and credential precedence. A configured
   endpoint override must never inherit a key intended for a different endpoint.
2. Recognize only the preset's named environment variables on the execution
   host. Support documented aliases in a deterministic order; show the selected
   variable name in Manage, never its value. If `OPENAI_BASE_URL` or
   `OPENAI_API_BASE` points elsewhere, do not automatically send `OPENAI_API_KEY`
   to OpenAI's canonical endpoint; treat that combination as custom setup.
3. Keep existing Whip account sources and supported Inference.net CLI discovery.
   Continue using Whip's subscription credential owner for ChatGPT.
4. **Approved implementation default:** also recognize
   ordinary API-key records in OpenCode's standard `auth.json`. Use a bounded,
   read-only parser, an explicit preset-ID mapping, and the canonical endpoint.
   Accept only `type: "api"`; skip OAuth, well-known credentials, unknown IDs,
   required metadata that Whip cannot interpret, and placeholder/dummy values.
   Do not run OpenCode or execute its plugins/configuration. Read the standard
   global provider overrides only to establish the intended endpoint: a matching
   auth-record ID alone is not proof that its key belongs to the canonical URL.
   Skip custom destinations and unresolved/project-specific routing rather than
   guessing. If this cannot be established reliably, show the source as detected
   but require explicit connection instead of auto-reusing its key.
5. Treat OpenCode as a fallback after explicit Whip credentials and preset env
   keys. Display `From OpenCode` in Manage. Reuse the existing external-source
   credential semantics: do not copy all keys into Whip or delete them from
   OpenCode. Resolve external credentials consistently for status and requests,
   with endpoint restrictions enforced by the shared resolver.
6. Disabled providers stay disabled even if a key is discovered later.
   Disconnecting an externally sourced connection disables it in Whip; it never
   edits the source application. Changes to credentials invalidate stale catalog
   results using the existing route/credential guards.

Resolve the standard OpenCode data location with its XDG/home rules; do not
assume a macOS Application Support directory. Read one bounded snapshot per
inventory operation rather than reparsing the credential file for every row.

The checkmark means **credentials are available for this connection**, including
an explicitly configured no-auth endpoint. It is not proof of a paid inference
call. Configured-but-missing, disabled and unchecked command-reference states get
distinct text/indicators. A successful public model listing must not be described
as authenticated-key validation.

Discovery is local metadata work: no prompt, completion, key guessing by prefix,
network scan, general home-directory crawl, `.env` scan, or secret-command
execution. This research did not inspect any real credential values.

For TUI/web/desktop, “local” means the **selected execution host**. The browser
does not read client-machine secrets and a remote connection never receives
desktop-machine keys. Extend the desktop's existing bounded login-shell recovery
only for launching its local daemon. An already-running daemon cannot acquire
new terminal environment variables automatically; show refresh/restart guidance
instead of silently mutating process environment or copying keys into files.

The clean Docker workflow keeps its isolation: no automatic host home/key mounts.
Acceptance supplies fake environment keys or a fake OpenCode auth file inside
the disposable container. A fresh container stays genuinely fresh.

## Model discovery and compatibility

Whip currently treats `GET <base>/models` as discovery and the new-key setup
validator. Its `ModelInfo` does not describe Chat Completions/tool eligibility.
That is insufficient for a broad preset list: model inventories can contain
embeddings, audio, non-tool or Responses-only models, and public catalogs do not
validate credentials. [Current parser](../../../internal/llm/openai.go),
[current key setup](../../../internal/daemon/provider_service.go).

For each initial preset:

- Verify model-list URL, response format and whether it authenticates. Prefer the
  current generic path; add only documented exceptions. For example, Cerebras
  publishes `/public/v1/models`, and DeepInfra documents `/models/list` with a
  different shape. Those facts alone do not establish whether the generic route
  is also available. [Cerebras catalog](https://inference-docs.cerebras.ai/api-reference/models/public-models),
  [DeepInfra catalog](https://docs.deepinfra.com/api-reference/models/models-list).
- Normalize enough capability metadata to show Chat Completions models usable
  with Whip's `rlm_exec`. Where a generic list is sparse, use a reviewed bundled
  model subset rather than guessing from an arbitrary name or forcing manual entry.
- Keep known/default routes and configured aliases authoritative. Refresh cannot
  silently select another provider or model. Do not write discovered catalogs
  wholesale into configuration.
- Separate key storage, catalog availability and inference success. If a preset
  has only a public/bundled catalog, save the explicitly submitted key with an
  honest unverified state and show models; the first user-sent prompt checks it.
  Reject observed 401/403, preserve input context on network errors, and offer
  retry. No automatic billable validation call.
- Test exact request shape, streamed tool-call assembly, tool result round-trip,
  output/reasoning parameters and usage. A URL plus a passing model-list request
  is not sufficient compatibility evidence. Record unverified/live-test limits.

## Implementation sequence

1. **Shared preset inventory.** Extend `internal/config/providers.go`, introduce
   only necessary non-secret `ProviderEntry` metadata, generate protocol/SDK
   outputs and update the desktop environment allowlist. Cover saved custom IDs
   that collide with newly introduced presets: no overwritten endpoint, credential
   or model route, and noncanonical overrides remain visibly custom/manageable.
2. **Credential discovery.** Reuse `EffectiveProviders`, `KeyStatus`, `ResolveKey`
   and `providerStatus`; add the agreed OpenCode reader and alias rules. Keep
   configuration writes in existing host APIs. No database migration.
3. **Catalog readiness.** Verify each planned provider and add small discovery/
   capability exceptions where needed. Split public model discovery from actual
   credential validation in the shared host setup path.
4. **Picker and key prompt.** Update `internal/tui/setup.go` and the existing
   `ui.List` status gutter. Keep one provider dialog/state owner; add OpenAI family
   presentation without replacing its two route IDs. Use the current host API to
   save a preset key. Use the same compact prompt renderer for custom steps.
5. **Rendering corrections.** Paint left padding as well as content/tail with the
   panel background. Account for prompt/cursor cells in text-input width. The
   current form prepends unstyled spaces, and its input view can exceed the width
   by its cursor cell, explaining the jagged left edge and stray ellipses. Fix the
   shared rendering path used by the affected dialogs, not individual screenshots.
6. **Verification and docs.** Update current provider/README/feature/frontend docs,
   mark the superseded form presentation in the earlier plan, and record actual
   terminal screenshots plus deterministic integration evidence.

Implementation was authorized after research. Tests used isolated runtime homes
and an owned Docker container on port 4001; the installed app and user container
on port 4000 were not modified.

## Acceptance criteria

- Empty installation: matching modal geometry, prompt-free focused search,
  Inference.net first, stable status gutter, correct scroll and keyboard behavior.
- Known API-key provider: selecting it asks only for its key; successful setup
  reaches model selection without a URL/name/model-configuration form.
- Existing env/Whip/OpenCode fixture keys: checked on first display; Enter skips
  key entry. Disabled and explicit override cases do not auto-reconfigure.
- OpenAI API and subscription: correct checkmark/method choice and preserved
  concrete provider ID in session/default writes.
- No unsupported model types in the ordinary preset picker. Discovery failures,
  public catalogs, invalid keys and offline startup have truthful recovery text.
- At 32, 48, 80 and 120 columns and short/tall heights, in dark/light themes:
  equal-width panel rows, colored padding, no phantom trailing ellipses, visible
  cursor/focus, masked secrets and reachable actions. Test colored output and
  actual terminal rendering; ANSI-stripped width tests alone missed this defect.
- Cancel, refresh and concurrent edits retain draft/request ownership. No secret
  enters a durable session command, log, error, browser response or clipboard.
- Shared daemon inventory agrees in TUI and web/desktop. Restart retains saved
  choices and rediscovers available external credentials without migrating stores.
- Clean Docker: fake local keys, preset key entry, one explicit deterministic
  `rlm_exec` call, restart/resume, and no effect on the user's port-4000 container.
- Focused Go/TUI/credential tests, protocol/SDK checks, desktop allowlist tests,
  `task check`, race coverage for changed concurrent paths, and final review.

## Decisions to carry into implementation

- Reproduce OpenCode's visual interaction; retain Whip's current backend and
  persistence architecture.
- Use ten reviewed API-key presets plus the existing ChatGPT subscription route.
- Interpret “remove caret” as removing the `>` prompt, not hiding the typing cursor.
- Treat the checkbox/checkmark as status; manage/disconnect remains an explicit action.
- Keep advanced/manual support behind Other/custom, following the latest
  “scratch that” instruction; known-provider setup is one key field.
- Execution used the stated default of eligible OpenCode plain API-key discovery.
  The implementation skips ambiguous sources instead of suggesting that a detected
  key belongs to a canonical endpoint without sufficient evidence.

## Completion and verification

Implemented the shared registry, credential reader, provider family/method picker,
compact prompts, status gutter, request-owned clipboard handling, desktop local
shell recovery, and compatible model discovery. The existing explicit model
confirmation remains: a valid saved/suggested model is offered with **ctrl+k** to
choose another; otherwise the model picker opens. Authentication alone does not
change the saved pair or send a draft.

Review caught and fixed two additional issues: delayed search clipboard results
could enter a newly opened key prompt, and a catalog-only saved default became
ambiguous when multiple providers advertised the same model ID. Default pairs
now survive readiness, session creation, runtime construction and restart while
explicit model requests retain their existing routing precedence.

OpenCode discovery reads one bounded snapshot per inventory operation. A narrow
immutable/read-only SQLite metadata query checks for organization routing; it does
not store Whip providers or read database credentials. Active journals, unknown
schemas, custom/channel databases and unresolved routing suppress this optional
fallback. Finder recovery propagates a private non-secret override guard, rather
than putting fake paths or inline configuration into OpenCode environment variables.

Validation completed:

- `task check` passed, including Go format/vet/whipvet/tests, protocol drift/tests,
  SDK checks, web checks/build/tests, and local-update/Docker launcher tests.
- `go test -race ./...` passed. Additional focused default-pair race tests passed.
- Desktop TypeScript and 82 desktop tests passed; 9 existing tests were skipped.
- Independent credential/catalog and TUI review completed. Findings were addressed
  before final acceptance.
- Built the dirty checkout using the onboarding Dockerfile. Final acceptance image:
  `sha256:610f3cd901c01b696af405432bab89b2fe78881d240e913bf0804e55f7e1e09b`.
  Renderer digest: `1b5e5e8ad6719c971ffcf20de19bab3f6fa66f09b7dec6024ab2ef9d45ebd985`.
- Actual PTY acceptance covered a clean picker, Groq's key-only prompt, sequential
  custom setup, model/default confirmation, and an explicit deterministic
  `rlm_exec` call returning `{"answer":42}`. No completion occurred before send.
- An eligible fake OpenCode Groq key and `DEEPINFRA_TOKEN` appeared as connected in
  TUI and web. The saved Whip config contained neither discovered secret and no
  Groq/DeepInfra provider definition. Web showed the respective OpenCode/environment
  sources and the persisted `fixture-coding` / `fixture-endpoint` default pair.
- Restarted the daemon (generation 3) and reopened the same session. Its model and
  provider remained selected without onboarding. A 42-column, 18-row PTY retained
  a visible focused search, connection mark, selection and footer. Automated layout
  coverage also tested smaller/larger terminals and dark/light themes, including
  colored edge cells rather than only stripped string widths.
- Closed the temporary browser tab and removed the owned acceptance container and
  fixture server. The user's port-4000 container and unrelated services remained.

Evidence logs: `/tmp/whip-picker-check.log`, `/tmp/whip-picker-race.log`,
`/tmp/whip-picker-default-pair-race.log`, `/tmp/whip-picker-docker-build.log`.
PTY captures: `/tmp/whip-picker-screens/final-clean-picker.png`,
`final-groq-key.png`, `custom-review.png`, `fixture-tool-result.txt`,
`after-daemon-restart.txt`, `narrow-detected-search.png`, `resumed-session.txt`.
The first PTY renderer lacked ANSI repeat-character support; final captures use
that support and do not treat stale emulator cells as application output.
One combined daemon restart attempt timed out while another client started the
replacement; explicit stop/start and the later persistence checks succeeded.

Compatibility remains deliberately bounded: sparse provider catalogs expose a
reviewed subset, DeepSeek V4 uses non-thinking mode, and public/bundled discovery
is not proof of authenticated inference. No real keys or paid live completions
were used. See [catalog contracts, sources and fixture evidence](CATALOG-COMPATIBILITY.md).
