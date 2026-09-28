# Plan: native `anthropic-messages` API flavor (inference.net)

Status: phase 1 implemented (slice 1, thinking off); phase 0 live-run pending
Implementation: internal/llm/messages.go, llm.Flavor dispatch in openai.go/accounting.go,
config.APIMessages + KnownAPI/GenericChatAPI in config.go, daemon ModelClient wiring,
API gate checks now via config.KnownAPI. Tests: internal/llm/messages_test.go.
Date: 2026-09-28
Related: `.ai-docs/plans/agent-ux-batch/opencode-speed.md` (mentions anthropic-messages);
`docs/models-providers.md:133` (documents the current two-flavor limitation)

## Problem

Claude models on inference.net (`claude-opus-5-5` etc.) advertise
`supported_endpoints: ["messages"]` only — they speak Anthropic Messages, not
chat-completions. whip (`api: "openai-completions"`) posts chat-completions
anyway, and inference.net's OpenAI→Anthropic translation layer breaks on
tool-round replays. Result: opaque `400 invalid_request_error "the upstream
provider returned an error"` on the second tool round of a thinking-enabled
Claude session.

Root cause chain (diagnosed on main @ 801b3a4f):
effort "low" default → translator enables Anthropic thinking → Anthropic
requires thinking blocks replayed with tool_use turns (with signature) →
whip's history stores only text/tool calls → second-round request is
protocol-invalid.

Interim hotfix already in the tree (uncommitted):
`internal/llm/provider_compatibility.go` strips `reasoning_effort` from
Claude-via-OpenRouter routes (same treatment DeepSeek V4 gets). This makes the
400 stop happening, at the cost of Claude thinking being unreachable.

## Phase 0 — evidence before code (no build)

Run the extracted repro matrix against `https://api.inference.net/v1/chat/completions`
(bodies producible via `go run ./cmd/dumprequest <model> <effort>`, added in this
branch, plus the hand-built round-2 replay in this plan's discussion):

| Variant | Confirms/refutes |
|---|---|
| round 1, effort present vs absent | whether inf.net maps effort→thinking |
| round 2 (assistant tool_calls + tool result, content ""), effort present | the 400 repro |
| same round 2, effort removed | thinking-replay invariant is the trigger |
| same round 2, `"content": null` | empty-text-block translation bug |

Findings decide whether slice 1 needs any extra guard. If the opaque wrapper
swallows Anthropic's field-level detail, note it: it justifies the adapter's
error-surfacing behavior (real `type`/`message` from /v1/messages).

## Phase 1 — slice 1: native Messages flavor, thinking OFF (~250 lines)

Smallest thing that fixes the protocol mismatch without new invariants.

1. **Config opt-in, not catalog magic.** Add `"anthropic-messages"` as a
   valid `api` value (`internal/config/config.go:21` comment, validation at
   `config.go:196`). User sets `"api": "anthropic-messages"` on the
   inference-net provider (config is guarded/atomic; no catalog-keyed silent
   protocol switches in v1 of this feature — a stale `~/.whip/models.json`
   must never change wire protocol).
2. **`internal/llm/messages.go`** — mirror `responses.go`:
   - `encodeMessages(req Request)`: system role → top-level `system`;
     user Parts → text/image blocks; tool → `{role:"user",
     content:[{type:"tool_result",...}]}`; tools → `{name, description,
     input_schema}`; **no reasoning_effort** (thinking stays off in slice 1 —
     the budget mapping is unguessable and wrong values are hard 400s);
     no prompt_cache_key equivalent.
   - `messagesOnce(...)`: Anthropic SSE (`message_start`, content_block_*,
     `message_delta` usage, `message_stop`) → onText/onToolCall; posts to
     `{BaseURL}/messages` (parameterize `httpRequest`).
     onThink unused in slice 1.
3. **Dispatch**: `Client.Stream`/`Complete` select Messages when the
   provider `api` says so (a `Client.flavor` field set at client
   construction from config; NOT derived from BaseURL string-matching for
   inference.net — the existing encodeChatRequest switch stays
   for-compat only). Reuses `runAttempt` retry/regeneration untouched.
4. **Docs**: `docs/models-providers.md` flavor list; `docs/features.md` entry.
5. **Tests**: encode golden tests (every role, tool round-trip — the exact
   400 case minus thinking must encode cleanly); SSE parse tests; an
   end-to-end fake-server tool round-trip; the provider_compatibility
   suite extended so no Claude-with-effort request reaches chat-completions.
6. **Live verification**: extend `cmd/dumprequest` with flavor awareness,
   then run the phase-0 matrix against real `/v1/messages`. Turn 1 + tool
   round 2 must go green with native error text visible on failure.

Shippable on its own: Claude-on-inf.net gets native protocol, real errors,
and the same thinking-off behavior as the hotfix, but now as a deliberate
correct protocol rather than a suppressed parameter.

## Phase 2 — thinking: capture + replay WITH signatures (decoupled follow-up)

Only after slice 1 is live and we can harvest real signatures.

1. Replay must echo the encrypted signature Anthropic returns — not the raw
   `reasoning_content` text whip sees on the chat path. So: capture
   `{type:"thinking", thinking, signature}` blocks from /v1/messages
   `content_block_start/delta` events into a new internal `Message` field
   (persisted; old sessions unmarshal with it empty — backward compatible).
2. `encodeMessages` emits thinking blocks before tool_use on replay.
3. Effort → `thinking.budget_tokens`: derive mapping from live errors and
   docs per model family; keep the hotfix guard for any chat-completions
   Claude route that remains (OpenRouter etc.).
4. Swap inference-net's own preset in `internal/config/providers.go` to
   `anthropic-messages` for Claude ids only after phase 2 verification, OR
   leave user-opt-in and revisit.
5. Optional: reject `attendance` mismatch early — validate a session's
   replayed history against Anthropic's requirements client-side (thinking
   present iff tool_use present on replay) so bugs surface as clear local
   errors, not upstream 400s.

## Explicit non-goals (for now)

- Catalog-driven flavor selection (`supported_endpoints` → protocol). Deferred
  until phase 2 is proven; risk of silent mid-session protocol flip.
- `cache_control` prompt caching breakpoints for Anthropic. Follow-up;
  `prompt_cache_key` has no Anthropic meaning.
- Bedrock/Vertex variants, OpenRouter Claude conversion to native (the
  hotfix guard continues to cover it).

## Risks / notes

- Thinking MUST stay off in slice 1 (both by omission and by guarding against
  config leftovers). A `reasoning_effort` on a messages-flavor request is a
  silent no-op at best; the guard belongs in `encodeMessages` as a lint-level
  check so it can be removed in phase 2, not forgotten.
- Session store schema is untouched in slice 1 (phase 2 adds one field).
- No new dependencies. All dispatch passes through existing retry machinery.

## Verification checklist

- [ ] Phase 0 matrix run and results recorded here
- [ ] slice 1: fake-server tool round-trip test green
- [ ] slice 1: live inf.net turn 1 + tool round 2 green via /v1/messages
- [ ] slice 1: `task check` green, docs updated
- [ ] phase 2: signature-carrying replay verified against live error text
- [ ] phase 2: effort→budget mapping derived from real responses, not guessed
