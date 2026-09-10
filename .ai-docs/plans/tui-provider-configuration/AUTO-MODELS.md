# One-step TUI provider selection

Branch: `codex/provider-onboarding`

## Goal and scope

An explicit selection or successful connection of a popular provider returns to
WHIP's composer with a model and thinking level ready. Keep drafts unsent; keep
saved startup selections unchanged. Other providers and custom endpoints retain
model selection. Remove informational discovery footers from the TUI pickers;
retain actionable failures and keyboard shortcuts.

## Verified defaults (2026-09-09)

| Provider | API model ID | Thinking |
| --- | --- | --- |
| Inference.net | `kimi-k3-fast` | `high` |
| OpenAI API and ChatGPT subscription | `gpt-6-astra` | `medium` |
| OpenRouter | `z-ai/glm-5.3` | `max` (provider default) |

Sources: [Inference model](https://inference.net/models/kimi-k3-fast/),
[Inference reasoning](https://docs.inference.net/api/reasoning),
[OpenAI model](https://developers.openai.com/api/docs/models/gpt-6-astra),
[OpenAI tool transport](https://developers.openai.com/api/docs/guides/latest-model),
[OpenRouter model](https://openrouter.ai/z-ai/glm-5.3).
Public `/v1/models` responses from Inference.net and OpenRouter verified exact IDs,
limits, and efforts. Astra's official page supplies its API limits. Astra tools
require Responses; ChatGPT already uses it. API keys now use the shared Responses
codec only for Astra at the canonical OpenAI URL, with bounded output and separate
credential-scoped opaque history. Other model/endpoint combinations keep their
existing transport. No real keys or paid inference were used for validation.

## Implementation

- Preset defaults and reviewed model metadata: `internal/config/{providers,provider_models}.go`.
- Selection and clean footers: `internal/tui/setup{,_default,_provider}.go`.
- First-session preparation applies effort before enabling send: `internal/tui/startup.go`.
- Existing sessions rebuild with model + optional effort in one command:
  `internal/protocol/runtime_types.go`, `internal/daemon/client_control.go`.
  `internal/session/session.go` persists the route and effort in one SQL update.
- Canonical Astra API transport reuses Responses streaming, replay, retries, and
  accounting: `internal/llm/{openai_responses,openai,accounting,responses}.go`.
- Fresh onboarding writes model/provider/effort together through the existing
  revision-checked config API. Existing-session choices do not rewrite globals.
- Check catalog membership and exact destination before automatic selection;
  refresh when the cached suggestion differs. Missing models, conflicting aliases,
  unsupported efforts and discovery failures keep the picker usable.
- API and ChatGPT remain distinct stored provider identities. Late replies stay
  guarded by the existing request owner/cancellation checks.

## Validation

- [x] Default selection for detected keys, pasted keys, completed login, and existing sessions.
- [x] Missing model/effort, custom destination, alias conflict, catalog failure and stale config fallback.
- [x] Draft preservation and explicit first send after model and effort preparation.
- [x] API Astra streamed tool call, persisted encrypted history replay, budget cap,
  token accounting, and generic custom-endpoint behavior.
- [x] Session effort persistence/resume and full required checks.

`task check` passed (Go, contract generation/drift, SDK, web and launcher tests).
Race tests passed for TUI, daemon, LLM, and config. The final combined session
selection write also passed the targeted persistence/resume race test; Go vet
passed for TUI, daemon, and session. Logs are in `/tmp/whip-auto-model-*.log`.

Independent review identified reload payload handling, alias shadowing, and stale
preset effort after switching to manual selection. All three were corrected and
covered by the relevant state or protocol paths.
