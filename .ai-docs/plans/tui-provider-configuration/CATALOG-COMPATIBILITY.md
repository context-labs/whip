# Provider catalog compatibility

Implementation evidence for the picker redesign, September 9, 2026. Live catalogs
now determine model membership; the bundled subset only fills missing metadata
and provides offline candidates. A provider
being listed means its connection can be configured; it is not a claim that every
model or feature exposed by its API works with Whip's supported transports.

## Catalog routes and authentication

All ten presets use `GET <canonical API root>/models`. No extra discovery
transport was needed. In particular, Cerebras documents the ordinary authenticated
route in addition to its public catalog; DeepInfra's existing OpenAI-compatible
route returns the ordinary `data` envelope.

- [Cerebras list models](https://inference-docs.cerebras.ai/api-reference/models/list-models)
  documents `/v1/models`, bearer authentication, and an OpenAI-shaped response.
- [DeepInfra OpenAI models](https://docs.deepinfra.com/api-reference/models/openai-models)
  documents its model envelope and nested `metadata.context_length`. A read-only
  request to `https://api.deepinfra.com/v1/openai/models` returned 200 with 190
  entries without a credential during implementation.
- [OpenRouter model listing](https://openrouter.ai/docs/api-reference/models/get-models)
  includes `supported_parameters`, output modalities and nested provider limits.
  Its public endpoint returned 200 with 430 entries without a credential.
- [DeepSeek list models](https://api-docs.deepseek.com/api/list-models),
  [Together compatibility](https://docs.together.ai/docs/inference/openai-compatibility),
  and [Fireworks compatibility](https://docs.fireworks.ai/tools-sdks/openai-compatibility)
  provide the remaining request contracts and discovery shapes. Together's
  top-level model array is accepted alongside the standard envelope.

Unauthenticated read-only probes to the other eight canonical model endpoints
returned 401 or 403. Those results do **not** establish whether a real account/key
works; some 403 responses may be edge/network policy. No real credentials were
read for this research and no live completion was sent.

API-key setup distinguishes the following outcomes:

- A compatible model list loads: store the submitted credential, then show models.
  The message says inference has not been tested.
- OpenRouter or DeepInfra's public list loads: explicitly report that neither the
  API key nor inference has been verified.
- A canonical preset has a transport timeout/DNS failure or an unavailable
  catalog route (404/405 or server failure): use its bundled candidates and state
  that the connection has not been verified. Refresh retries discovery.
- Observed 400/401/402/403/429 responses remain errors. A bundled catalog never
  bypasses key rejection, denied access, billing restrictions or rate limits.
- A successful list is empty or contains only explicitly incompatible entries:
  report the compatibility gap rather than inserting absent bundled models.
- Other/custom endpoints retain the existing explicit manual-model and
  save-without-verification path. They never inherit preset fallbacks merely by
  reusing a provider ID or hostname.

Provider discovery does not run a prompt, completion or tool. The first explicit
user send remains the inference check. `RuntimeConfiguration.discovery` is an
ephemeral result of `provider.key.set`, not persisted configuration or a new
credential database.

## Reviewed model subset

The small bundled subset was reviewed against the existing Whip Inference.net
configuration and [models.dev source at
5dc2ef24dc14a5820e8fb2423134035e11d07576](https://github.com/anomalyco/models.dev/tree/5dc2ef24dc14a5820e8fb2423134035e11d07576/providers).
Model definitions mark tool support and text output; inherited base definitions
were inspected when a provider definition references `base_model`. Bundled
entries carry context/output limits and supported input modalities, not guessed
prices. Fetched price information remains authoritative.

| Preset | Bundled metadata/offline candidates |
| --- | --- |
| Inference.net | `kimi-k3-fast`, `kimi-k3` |
| OpenRouter | `z-ai/glm-5.3`, `moonshotai/kimi-k3` |
| OpenAI API | `gpt-6-astra` (Responses), `gpt-4.1`, `gpt-4.1-mini` |
| Cerebras | `gpt-oss-120b` |
| Groq | `llama-3.3-70b-versatile`, `openai/gpt-oss-120b` |
| DeepSeek | `deepseek-v4-flash` in non-thinking mode |
| Fireworks AI | `accounts/fireworks/models/gpt-oss-120b` |
| Together AI | `meta-llama/Llama-3.3-70B-Instruct-Turbo` |
| DeepInfra | `Qwen/Qwen3-Coder-480B-A35B-Instruct-Turbo` |
| xAI | `grok-4.20-0309-non-reasoning` |

All presets admit newly advertised models even if their metadata is sparse.
Explicit negative tool capability and non-chat/output types take precedence;
missing metadata remains unknown. DeepInfra's `metadata.tags` recognizes chat,
embedding, image/video generation and speech categories. Entries are deduplicated
and missing limits, input modalities and reasoning efforts are supplemented only
for exact bundled IDs. Explicit live values, including empty reasoning efforts,
win. Live lists never acquire models from the bundled subset.

Sparse native OpenAI catalogs mix specialist APIs with chat. Known media,
embedding, moderation, legacy completion and unsupported Responses-only/tool
routes are excluded by native model family. Examples are documented in
[OpenAI's model catalog](https://developers.openai.com/api/docs/models/all),
[GPT-5 Pro](https://developers.openai.com/api/docs/models/gpt-5-pro), and
[o1-mini](https://developers.openai.com/api/docs/models/o1-mini). These exceptions
apply only to the canonical OpenAI preset, not gateways or custom endpoints.
DeepSeek's reasoning-only legacy route remains excluded because Whip cannot
replay its required reasoning content; V4's non-thinking adapter remains usable.
Saved aliases and custom routes are unchanged.

API-key submission fetches immediately. Ordinary host catalog requests refresh
missing caches, caches older than 24 hours, and pre-change caches without the
current discovery version. Forced refresh always fetches. A transient failed
refresh retains the last catalog and fetch timestamp; bundled candidates are used
only when no usable cached catalog exists. The persisted discovery version is
host bookkeeping and does not change the client protocol.

[Cerebras's current authenticated response](https://inference-docs.cerebras.ai/api-reference/models/list-models)
lists `gpt-oss-120b` and `qwen-3.8-27b` with sparse metadata. A fixture reproduces
that shape plus an unknown future model. No real Cerebras credentials were read.
Public read-only probes returned 435 OpenRouter entries and 190 DeepInfra entries;
DeepInfra included 105 chat-tagged entries and non-chat categories. These probes
establish response shapes, not account-specific inference availability.

The initial OpenAI API subset was limited to Chat Completions. The follow-up
[one-step default selection](AUTO-MODELS.md) adds canonical API Astra requests
through the shared Responses codec, including output caps and medium reasoning.
ChatGPT subscription retains its account login and separate catalog. GLM 5.3 and
Kimi K3 Fast IDs, bounds, and efforts were rechecked against their public catalogs.
[xAI's Chat Completions documentation](https://docs.x.ai/developers/model-capabilities/legacy/chat-completions)
labels that interface legacy; newer Responses-only features are outside this
integration.

## Request compatibility

The common transport keeps streaming function calls, assistant/tool history,
explicit output ceilings and usage accounting. Exact canonical Cerebras, Groq,
DeepSeek, Fireworks, Together and DeepInfra requests omit `prompt_cache_key`, which
those compatibility contracts do not document; their automatic prefix caching
remains provider-controlled. Custom endpoint requests keep the existing wire
shape.

[DeepSeek thinking-mode documentation](https://api-docs.deepseek.com/guides/thinking_mode)
states that V4 thinking is enabled by default and tool requests must replay
`reasoning_content`. Whip's Chat Completions history currently does not retain that
field. The initial canonical V4 subset therefore explicitly sends
`thinking.type = disabled` and exposes no reasoning-effort controls. Supporting
thinking mode requires a separate history/protocol change; the picker must not
silently promise it. This rule does not rewrite custom proxy endpoints.

## Automated evidence

- `internal/config/provider_models_test.go`: every API-key preset has a usable
  immutable bundled subset; sparse metadata supplementation; negative capability
  filtering; duplicate removal; sparse live additions; metadata precedence;
  exact-ID/endpoint isolation.
- `internal/llm/models_test.go`: OpenRouter nested metadata, explicit lack of tools,
  DeepInfra context/category tags and Together array normalization. DeepInfra's metadata
  `max_tokens` is not mistaken for an output ceiling.
- `internal/llm/provider_compatibility_test.go`: deterministic transport fixtures
  for all ten presets, verifying destination, bearer authentication, streamed
  `rlm_exec` argument assembly, matching tool-result history, final text,
  nonstream helpers, output/reasoning parameters and usage. No network dispatch.
- `internal/daemon/provider_model_test.go`: public versus authenticated model-list
  messages, safe fallback, rejected keys, custom-route isolation, file-backed key
  persistence and available environment alias selection; full Cerebras connection,
  persistence/restart, cache-version refresh, TTL/forced refresh, removed models,
  outage retention and empty-catalog behavior.
- `internal/tui/setup_catalog_test.go`: newly discovered Cerebras choices render,
  can be selected, and persist as the onboarding default.

These checks verify Whip's request and persistence behavior. They do not replace
live, account-specific acceptance. Real provider model availability, quotas,
pricing and deployment entitlement remain unverified in this task.
