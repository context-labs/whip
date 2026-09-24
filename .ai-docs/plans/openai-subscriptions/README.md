# OpenAI subscription provider

**Branch:** implementation in the existing `codex/desktop-release` working tree;
unrelated concurrent changes are preserved.
**Status:** implementation, live account acceptance and final validation complete
on 2026-09-09. Changes are in the working tree; the installed application was not replaced.
**Researched:** 2026-09-08, against WHIP `40699639bb6f3096daa6cefb69968877f25a801a`
and the working tree, including the settings work currently in progress.

## Recommendation

Add a built-in **OpenAI (ChatGPT subscription)** provider, with the stable route
ID `openai-codex`. Keep WHIP's recursive runtime, tools, transcripts, and client
interfaces. Add device-code sign-in on the execution host and a Codex Responses
transport inside the existing model client.

The implementation supports sign-in from web/desktop and CLI/TUI, including
remote hosts, as approved by the user.
Other attached clients, including mobile, can use the configured provider through
the existing daemon contract; a new native-mobile onboarding screen is deferred.

“Official support” here means a maintained, built-in WHIP integration. OpenCode
ships this capability, but its implementation is not a public OpenAI API contract.
The OpenAI documentation reviewed describes subscription authentication in Codex
and a supported product integration through Codex app-server. It does not establish
a general third-party OAuth client-registration process or stability guarantee for
direct access to the subscription endpoints. This is an evidence boundary, not a
claim that third-party use is prohibited. Do not describe WHIP as OpenAI-endorsed.

## What the research establishes

| Finding | Evidence and implication |
| --- | --- |
| Subscription access and API-key access are separate authentication paths. | OpenAI's [authentication guide](https://learn.chatgpt.com/docs/auth) distinguishes them. Device-code login is available, with an account/workspace setting prerequisite. Describe eligibility as a ChatGPT account with Codex access, rather than promising every plan/model. |
| OpenCode implements subscription login directly. | Its [provider documentation](https://opencode.ai/docs/providers/#openai) exposes ChatGPT and API-key choices. The pinned [OAuth implementation](https://github.com/anomalyco/opencode/blob/830d5eb5354874105cc31599635a80c1662609e8/packages/opencode/src/plugin/openai/codex.ts#L10) uses a public OAuth client ID, browser PKCE or device codes, access/refresh tokens, and the Codex Responses endpoint. |
| This needs a different request/response format. | OpenAI's [Codex request type](https://github.com/openai/codex/blob/26ce6649a256c00f89121cf56bdefb5c90577319/codex-rs/codex-api/src/common.rs#L282) uses Responses input items. WHIP currently only serializes Chat Completions requests. Changing a base URL and key is insufficient. |
| A documented app-server integration exists, but has a different scope. | [Codex app-server](https://learn.chatgpt.com/docs/app-server#auth-endpoints) owns Codex conversations, agent execution, auth, approvals, and streaming. Using it as the runtime would introduce another agent lifecycle into WHIP. Using it only for auth does not document exporting managed tokens to an independent inference client. |
| Stateless Responses requires preserving more than visible text. | OpenAI's [reasoning guide](https://developers.openai.com/api/docs/guides/reasoning#preserve-reasoning-without-stored-responses) describes encrypted reasoning items and replay of response output. Preserve item order, tool IDs, and assistant phase across tool round trips and restart. This does not expose raw reasoning. |
| Codex has authenticated model discovery. | The [models client](https://github.com/openai/codex/blob/26ce6649a256c00f89121cf56bdefb5c90577319/codex-rs/codex-api/src/endpoint/models.rs#L31) requests `models?client_version=...` and decodes a `models` array. This differs from WHIP's current `/models` decoder and supports reusing WHIP's catalog after normalization. |

OpenCode's current implementation also coalesces refreshes, supplies account and
compute-residency headers, filters its catalog, and omits `maxOutputTokens`.
Its [transport code](https://github.com/anomalyco/opencode/blob/830d5eb5354874105cc31599635a80c1662609e8/packages/opencode/src/plugin/openai/codex.ts#L341)
is useful prior art, not a drop-in adapter. Its model-name heuristics and zero
token-price convention should not become WHIP's source of entitlement or billing
truth. HTTP streaming remains available without adopting its optional
[WebSocket pool](https://github.com/anomalyco/opencode/blob/830d5eb5354874105cc31599635a80c1662609e8/packages/opencode/src/plugin/openai/README.md).

The working local OpenCode research checkout was older (`0e347450...`). This plan
uses freshly fetched public source at `830d5eb5354874105cc31599635a80c1662609e8`;
Codex references are pinned to `26ce6649a256c00f89121cf56bdefb5c90577319`.
The public client ID is shared with [Codex's auth implementation](https://github.com/openai/codex/blob/26ce6649a256c00f89121cf56bdefb5c90577319/codex-rs/login/src/auth/manager.rs#L1724);
its presence in open-source code does not itself establish third-party registration
or endorsement.
Research did not read account credentials. Live acceptance later used an explicitly
authorized Pro account in an isolated WHIP home. Its test credentials were removed
through the production logout flow after validation.

## Implementation evidence

- [x] Private, bounded credential storage and atomic rotation in `internal/openaiauth`.
- [x] Shared refresh ownership with cancellation, logout/replacement guards, and restart tests.
- [x] Provider-aware daemon login/status/logout and setup recovery; existing flows preserved.
- [x] Fixed subscription configuration profile; conflicting custom entries are refused.
- [x] CLI/TUI and shared settings integration, generated contract, and transport coverage.
- [x] Responses adapter, continuation persistence, catalog and accounting policy.
- [x] Recursive-runtime fixture: tool continuation, helpers, child, title, compaction and accounting.
- [x] Documentation, least-code pass and required adversarial review; all four findings resolved and re-reviewed.
- [x] Final `task check` after review fixes (`/tmp/whip-openai-check.log`).
- [x] Final complete race suite: `go test -race -p 1 ./...` (`/tmp/whip-openai-race-final.log`).
- [x] Live account authorization, model/tool/helper/restart smoke test.
- [x] Discovered model selection in new sessions, explicit model/provider pairs,
  and a real browser-created subscription conversation.

Live acceptance passed after the user approved enabling device-code authorization
in ChatGPT Security settings. WHIP's own client identity completed device login,
authenticated catalog discovery, real access/refresh-token rotation, streaming
text, images, tool continuation, model helpers, child execution, titles,
compaction and reuse after daemon restart. The browser created a session using
the catalog-only `gpt-5.6-luna` route and displayed its real response.
See [live evidence](evidence/live-check.json).

Live testing exposed two gaps and drove their fixes. Codex sends complete items
through `response.output_item.done` while terminal `response.completed.output`
can be empty. The parser now retains raw items by output index, still gating
executable tools on successful response completion. The model menus now combine
configured and discovered routes and select explicit model/provider pairs;
choosing the same model on a different provider no longer becomes a no-op.
Both changes have regression tests and passed independent adversarial review.

Passing checks so far: auth/LLM/daemon focused race tests, all LLM/session/config
tests, resolver/logout fixture, 13 provider settings tests, and all 272 SDK tests.
The combined `task check` passed on 2026-09-09 before the final review fixes,
including 361 app tests; its log is `/tmp/whip-chat-task-check.log`. The full
parallel race run passed provider packages but hit three existing timing-sensitive
CLI/kernel tests under load; all three passed isolated reruns. A final serial
package race run passed (`/tmp/whip-openai-race-final.log`). An earlier serial
run caught an unrelated TUI wordmark test mid-edit; its current version passes.
The final `task check` after review changes passed (`/tmp/whip-openai-check.log`).
After the live fixes, `task check` passed again with 272 SDK and 363 app tests
(`/tmp/whip-openai-live-check-final.log`), as did the affected LLM/auth/daemon/
session/CLI race suites (`/tmp/whip-openai-live-race.log`), production packing,
and 20 Chromium/Firefox model-picker scenarios in light/dark and narrow layouts.

The required independent adversarial review identified optional token expiry,
ACP's static-key prerequisite, HTTP/SSE error classification and same-account
relogin during admission. Fixes add a bounded expiry fallback, keep ACP thin,
share the error classifier and check auth generation before dispatch. These are
covered by targeted regressions. No dependency or second agent runtime was added.

The initial probe could not pass the account's security-setting gate, so
implementation continued with fixtures. The later authorized account smoke test
established live endpoint compatibility for the tested Pro account and models.
The protocol generator permits
additive response fields while keeping requests strict, so omitted-provider
Inference.net login remains compatible.

Production browser acceptance also passed with an isolated daemon: selecting
the subscription hides API-key controls and shows accurate signed-out state;
WHIP successfully obtains a real OpenAI device code; reload recovers that flow;
cancel removes verification controls and the code. The test daemon was stopped.
That initial check did not change account settings or save credentials.
Later live acceptance verified connected account display, all five discovered
model choices, browser inference and logout. Logout removed the isolated
credential file and account catalog; the test daemon was stopped.
See [browser evidence](evidence/browser-check.json).

The catalog query uses upstream stable release `0.153.4`, verified against
[Codex's release](https://github.com/openai/codex/releases/tag/rust-v0.153.4).
Natural output ceilings are verified against OpenAI's model documentation for
[current flagship models](https://developers.openai.com/api/docs/models/gpt),
[GPT-5.5](https://developers.openai.com/api/docs/models/gpt-5.5),
[GPT-5.4](https://developers.openai.com/api/docs/models/gpt-5.4), its mini/nano
variants, GPT-5.3-Codex and GPT-5-Codex. Unknown models are omitted. This avoids
confusing context-window metadata with output limits. The live Pro catalog
returned five supported models with 258,400 usable context tokens, advertised
vision and provider-specific effort lists; the natural output ceiling remains
the documented 128,000 tokens, not an inference from context size.

## Existing WHIP boundaries to extend

| Surface | Current implementation | Needed change |
| --- | --- | --- |
| Provider configuration | `internal/config/config.go:17`, `:553`; `internal/config/revision.go:39` | Add one subscription profile; preserve existing routes and revision-checked writes. |
| Login lifecycle | `internal/daemon/provider_service.go:48`, `:266`; `provider_account.go:40` | Reuse bounded flows and account operations, which currently assume Inference.net. |
| Private credential storage | `internal/inferencenet/store.go:75` | Follow owner-only atomic storage with a separate OpenAI credential file. |
| Model construction | `cmd/whip/daemon.go:231` | Stop requiring a static API key for every route; construct subscription clients using one host-owned credential manager. |
| Wire calls and usage | `internal/llm/openai.go:370`, `:687`, `:881`; `accounting.go:346` | Add Responses encoding/decoding while retaining attempts, callbacks, cancellation, and settlement. |
| Model catalog | `internal/daemon/client_control.go:1706`; `internal/config/catalog.go:84`; `internal/protocol/catalog.go:13` | Normalize subscription metadata into the existing cache and wire view. |
| Contract and SDK | `internal/protocol/registry.go:112`, `types.go:361`, `provider_types.go:9`; `packages/sdk/src/services.ts:72` | Identify the login's provider and expose safe subscription account status. |
| Shared settings | `packages/app/src/settings/providers.tsx:30`, `:216`, `:308` | Add subscription sign-in, account state, and logout to the current settings components. |
| Terminal clients | `cmd/whip/auth.go:33`, `internal/tui/auth_cmd.go:19`, `auth_inferencenet_cmd.go:44` | Add terminal entry points using the same daemon flow. |
| Runtime helper calls | `internal/agent/agent.go:513`, `:943`, `:1125`; `internal/daemon/agent_session.go:399`, `:488` | Ensure chat, descendants, model helpers, compaction, and titles all use the adapter. |

Read `docs/frontend.md` before implementation; it remains the frontend authority.
The roadmap requires one recursive runtime, and `docs/features.md` maps the shared
provider loop. Existing OpenCode learnings cover UX but contain no subscription
transport implementation. The historical OpenRouter plan is useful for catalog
reuse, not current credential ownership requirements.

## Scope and explicit deferrals

Deliver one connected ChatGPT account/workspace per execution host, restart-safe
credentials, selectable models, streaming text/tool use, images where advertised,
reasoning effort, helper calls, and actionable auth/quota errors.

Defer browser callback/PKCE as a second login method, OS keychain integration,
automatic import of Codex/OpenCode credentials, multiple accounts, account rotation,
WebSocket optimization, subscription usage dashboards, automatic model failover,
and general-purpose OAuth/provider plugin infrastructure. No Codex binary, proxy
service, Node helper, or new third-party dependency is needed for the proposed path.

Retain existing API-key providers. A separate route ID lets subscription and API
routes coexist without silently switching billing. This work does not require a
new built-in OpenAI API-key onboarding feature or migration of custom providers.

## Design

### 1. Verify the direct integration before building the full UI

Begin implementation with a narrow, disposable compatibility probe using an
explicit test-account sign-in. Validate device-code authorization, refresh, a
model-list read, a streamed `rlm_exec` call, its tool-result continuation, and a
text-only helper call. Use WHIP's own user-agent/originator identity. Record the
tested client ID, endpoint behavior, and sanitized fixtures; do not masquerade as
OpenCode or collect another program's credentials.

Probe these specific uncertainties before expanding the patch:

1. Does the flow work for the target account/workspace and WHIP client identity?
2. Which request fields does this endpoint accept, including `store`, streaming,
   reasoning options, instructions, function schema, and any output-token cap?
3. What model metadata and usable context/output bounds are actually returned?
   The `client_version` query parameter is an upstream compatibility input;
   document its tested meaning instead of substituting an unrelated WHIP version.
4. Are account/residency headers required, and are they respected on inference
   and discovery? Follow upstream semantics; never infer inference residency
   from storage residency or silently drop a declared constraint.

If direct access is unavailable or requires a registered integration, resolve
that concrete dependency before implementation proceeds. App-server is an
alternative product architecture to evaluate separately, not an automatic fallback.

### 2. One subscription profile and one credential owner

Use `openai-codex` as both the built-in route ID and an explicit `Provider.API`
profile. Internally it selects Responses plus subscription-specific behavior.
Keep existing `openai-completions` behavior and the existing `*llm.Client` call
surface. This avoids a new provider interface hierarchy for two wire formats.

The profile owns its fixed OpenAI auth and Codex endpoints. Reject API-key fields
or arbitrary base URLs on this profile, including conflicting existing entries;
do not send ChatGPT bearer tokens to user-supplied endpoints or follow redirects
to a different origin. Test endpoint injection stays internal to tests.

Add a small `internal/openaiauth` package for the device flow, credential file,
and refresh manager. Persist only the necessary access token, refresh token,
expiry, selected account/workspace identity, and safe display metadata under
`config.Dir()/openai-codex.json`. That preserves WHIP/whipcode home isolation.
Use 0600, an owned temporary file, atomic replacement, and cleanup on failure.
Treat malformed/unreadable existing credentials as errors rather than overwriting
them during a read. Configuration keeps references/profile metadata, not tokens.

Create one manager per daemon and share it across every subscription model client.
Resolve credentials at request time, so clients retained across turns do not hold
an expired token forever. Refresh shortly before expiry. Coalesce concurrent
refreshes, use bounded HTTP calls, and preserve rotating refresh tokens atomically
before publishing the new credential to waiting calls. A missing refresh token
in a successful response retains the old one only where the upstream contract
allows it. Network failure preserves the saved login; terminal refresh rejection
becomes “Sign in again.”

Use a credential generation to prevent a late refresh/login from undoing logout
or overwriting a replacement account. One cancelled caller must not cancel a
refresh still needed by other agents. Shutdown cancels owned work and waits for
it. Keep the synchronization local to this manager; no global auth service.

Add a single model-client construction method beside `ProviderService`, reused
by runtime route resolution and catalog fetching. It handles existing static-key
clients and this profile; it must not become a second agent or retry loop.
Create/inject the credential owner before the runtime factory in
`cmd/whip/daemon.go`; the server currently constructs `ProviderService` internally
in `internal/daemon/server.go:126`. Adjust that wiring explicitly so catalog and
runtime clients cannot accidentally get independent refresh managers.

### 3. Extend the existing login lifecycle

Add an optional `provider` to `provider.login.begin`, with omission retaining
Inference.net behavior. Include provider identity in each returned flow. Reuse
begin/list/status/cancel and existing bounded retention; OpenAI goes directly from
authorizing to provisioning to succeeded, without team/project provisioning.

Device flow: daemon requests a code; client displays the verification link/code;
the daemon polls at the upstream interval until success, cancellation, or expiry.
OpenAI's [device-code implementation](https://github.com/openai/codex/blob/26ce6649a256c00f89121cf56bdefb5c90577319/codex-rs/login/src/device_code_auth.rs#L62)
distinguishes pending poll responses from initial-request failure and bounds the
flow to 15 minutes. Do not copy an unbounded poll or apply Inference.net's current
10-minute timeout indiscriminately. Report disabled device login with the relevant
ChatGPT setting and a retry action; browser callback support can follow separately.

Allow at most one active OpenAI login on a host. Return/recover that flow for
duplicate starts and lost acknowledgements. Client detachment does not cancel it;
explicit cancel does. After daemon restart, old flow IDs report interrupted and
saved successful credentials remain usable.

On success, save credentials, merge the provider into fresh configuration, then
refresh its catalog. Preserve user defaults and unrelated configuration. These
are separate files: handle partial completion explicitly. A catalog failure does
not erase a successful login; a config write failure reports signed-in but not
ready and can retry setup without another OAuth exchange. Never report success
before required credential/config writes complete.

Extend safe provider status with auth method and authentication state, plus account
display/plan metadata where supplied. “Configured” alone must not mean “Connected.”
Logout clears WHIP's stored credentials and its catalog, invalidates the manager,
and cancels only this provider's pending logins. Already dispatched calls may finish;
all subsequent dispatches require a login. Never fall back to `OPENAI_API_KEY`.
Token values, token endpoint bodies, and device secrets never enter journals,
transcripts, browser persistence, or diagnostics.

### 4. Responses transport and durable continuation

Add a focused `internal/llm/responses.go`. Keep the existing `Stream` and `Complete`
entry points and the shared per-attempt accounting wrapper; choose the encoder and
transport internally. `Complete` must collect an HTTP stream when the subscription
endpoint requires streaming, so compaction and `models.call` work too.

Translate the established WHIP messages: system content to the appropriate
instructions field; user text/images to input parts; assistant tool calls to
function-call items; results to function-call-output items. Expose exactly WHIP's
existing `rlm_exec` schema on agent requests, with the correct Responses tool
shape. Requests use the verified subscription parameter profile and `store:false`.
Keep per-agent prompt-cache identity; no server-side conversation ID becomes
WHIP's history authority.

Decode SSE incrementally with bounded event/frame/output sizes. Map visible text,
reasoning summaries when present, tool argument deltas, terminal status, and usage
into existing callbacks and return types. Handle split events, multiple output
items, unknown informational events, explicit failure/incomplete events, and EOF
without terminal completion. Do not execute a tool from a truncated or malformed
response. Preserve the existing rule: no automatic replay after any output delta,
even when the caller passed nil callbacks.

Retain a bounded, provider-scoped continuation payload with each assistant message:
ordered response items, encrypted reasoning, function-call identity, and assistant
phase. Extend `llm.Message`'s custom marshal/unmarshal and the transcript round trip;
audit snapshots, clone/copy helpers, fork/rewind, and context compaction. Replay
compatible saved output items once, without also duplicating their synthesized
text/tool representations. Retain all active tool-exchange items together.

Opaque continuation data is model state, not a new visible transcript row or a
second event log. Keep it out of normal UI payloads/logs and unrelated providers'
requests; include it in durable byte limits and existing large-content handling
where needed. On a provider/account/model scope change, rebuild from ordinary
history instead of forwarding incompatible opaque state. Use conservative exact
model scoping initially; family-based reuse is optional later. No unbounded
in-memory response cache or automatic replay of the entire lifetime transcript.
WHIP currently uses `llm.Message` for both persisted data and protocol history:
strip internal continuation data in host-side public projections before measuring
and serializing snapshot/history frames, rather than merely hiding it in React.
Cover `internal/session/transcript_page.go` and `internal/protocol/schema.go` in
that audit; an optional storage field alone is not sufficient isolation.

### 5. Catalog, errors, and accounting

Decode the authenticated Codex catalog into `ModelInfoLite`: model ID/display name,
reasoning efforts, input modalities, and correctly interpreted context limits.
Respect visibility/capability restrictions, and use a small verified fallback for
required limits the response omits. Do not assume that a discovered model is
entitled for every account or accept future models solely by a name regex.
Keep WHIP's prompt and tool definitions; model catalog instruction templates do
not replace the recursive runtime prompt.
Carry advertised reasoning values through validation and the picker. Some current
WHIP configuration checks use a fixed effort list; validate against the selected
route's supported efforts rather than silently dropping or remapping a new value.
Extend catalog display-name/default-effort fields only if needed for this UI.

Reuse `models.json` and existing catalog-model resolution, avoiding generated
config entries for each model. Invalidate on logout/account change; cached data
must be associated with the credential generation/account, not only the base URL.
A failed refresh may use a last-known catalog for that same account with a stale
status. Never fall back to an unrelated OpenAI API model catalog.

Map input/output/cached/reasoning token counts to existing `Usage`, without adding
cached or reasoning counts a second time. Missing usage remains unknown. Label
the route as subscription access and keep dollar cost unknown when no per-call
charge is reported. Do not apply API token prices or manufacture provider-reported
zero cost. Existing finite monetary budgets already reject unknown pricing;
retain that behavior with a clear explanation. A quota dashboard is unnecessary
for initial correctness.

**Output caps are a mandatory compatibility decision.** WHIP currently shrinks
`MaxTokens` when admitting a call (`internal/session/model_call.go:202`) and sends
that grant on the wire. If the verified subscription endpoint supports an enforced
cap, map it normally. If it does not, reserve a vetted natural maximum for the
model and refuse a reduced grant before dispatch, settling it as undispatched.
Never silently omit a smaller budget-enforced cap. Small internal helper targets
are not proof of an upstream ceiling; their reservations need the same policy.
Explicit user output caps that cannot be honored must be reported as unsupported.
Without a trustworthy bound, do not promise finite-token-budget support on that
route. Counting visible text chunks cannot enforce a reasoning-token ceiling.

Classify sign-in required, model unavailable, quota exhausted, transient rate limit,
context limit, and transient server/network errors. A 401 may trigger one shared
refresh and one new inference attempt before any output; account for each actual
dispatch. Hard quota exhaustion should end the current attempt and display reset
information if provided, not enter either WHIP's retry loop or automatic turn
requeue. Ordinary throttling can reuse bounded retries with `Retry-After` support.
Sanitize errors before they reach the existing `HTTPError`/event paths.

### 6. Thin client changes

Web/desktop: extend the current Providers settings section with the subscription
option, sign-in, verification link/code, account status, reconnect, and disconnect.
Use existing host-scoped queries, flow recovery, external-link adapter, and model
picker. Invalidate the relevant host's status/catalog on completion. Keep the
execution-host label visible so remote credentials go to the intended host.

CLI: `whip auth openai-codex login|status|logout`; TUI: `/auth openai-codex`.
Reuse the existing device-flow presentation/polling where it fits; keep
Inference.net's workspace/project steps provider-specific. Help text follows
`buildinfo` so `whipcode` instructions remain accurate. Headless inference uses
the configured host account; it does not initiate interactive login automatically.

Update the Go registry/types first, then regenerate protocol declarations and
validators and update Go/TypeScript clients together. Preserve omitted-provider
behavior where practical. Explicitly verify compatibility/version negotiation;
do not assume adding fields is compatible with strict validators in older clients.

## Ordered implementation and acceptance

1. **Compatibility evidence.** Run the narrow probe above; freeze sanitized fixtures
   and resolve output-limit, model metadata, client identity, and residency behavior.
2. **Host auth and route.** Add private storage/refresh and the provider profile;
   wire it through the existing service, route resolver, login RPC, and CLI so the
   transport can be tested without waiting for UI work.
3. **Inference correctness.** Implement Responses streaming and collecting calls,
   durable continuation, catalog normalization, accounting, and error classification.
4. **Client completion.** Add shared web/desktop and TUI flows, status/disconnect,
   model selection, and scoped refresh/reconnect behavior. Integrate with the current
   settings work rather than restoring an older `settings.tsx` implementation.
5. **Release validation and docs.** Run the matrix below, a small real-account smoke
   test, and repository checks. Record actual evidence separately from the plan.

| Test area | Required proof |
| --- | --- |
| Auth and storage | Pending/success/disabled/expired/cancelled device flows; malformed/bounded responses; atomic write failure; preserved existing config; WHIP/whipcode home separation; no credentials in public results/logs. |
| Concurrency | Simultaneous root, child, helper, and catalog requests cause one refresh; refresh token rotation survives restart; cancellation/logout/relogin defeat late writes; shutdown terminates pollers. Use Go race tests. |
| Wire contract | Text, images, actual `rlm_exec` schema, cumulative tool args, multiple output items, phase, opaque reasoning, failure/incomplete/EOF, and oversized events. Unknown tool types never execute. |
| Continuation | Two consecutive tool round trips, restart, fork/rewind, compaction, and provider/model change retain or discard the correct state without duplication or accidental external forwarding. |
| Accounting/retry | Successful and missing usage, cache/reasoning counts, pre/post-delta disconnects, auth retry, quota versus throttling, helper calls, reduced output grants, and finite cost/token budgets. |
| Full runtime | Root plus child, `models.call`, `models.batch`, title, and compaction use the same route. Existing Chat Completions fixtures still pass. |
| Clients/transports | Unix and WebSocket login/status/cancel/logout; lost begin acknowledgement; reconnect; two clients viewing one flow; remote-host targeting; keyboard-accessible link/code/status; existing Inference.net flow preserved. |
| Live smoke | Explicit test-account login; one small streamed tool turn and continuation; helper/compaction; reuse after daemon restart. Use fixtures to test refresh/quota failures without exhausting a real subscription. |

Expected changes: new `internal/openaiauth/{device,store,manager}.go` and focused
tests, `internal/config/openai_codex.go`, `internal/llm/responses.go` and tests,
small additions to the existing provider service/factory, protocol and clients,
message persistence, and accounting boundaries described above. Avoid splitting
the rest of `llm` or rewriting provider architecture during this feature.

Validation commands after implementation: focused Go tests with `-race` for auth,
LLM, config, session, daemon, and CLI; generated contract checks; SDK and app tests;
the provider browser workflow; then `task check`. Run additional desktop/native
checks only if native code changes. Recheck skill instructions at implementation
time, including required review. Research-only changes do not warrant running the
application test suite.

Documentation: update `docs/models-providers.md` with eligibility, device-login
setup, host ownership, route selection, logout and budget limitations;
`docs/features.md` with implementation/test pointers; `docs/frontend.md` with the
provider-aware login/state contract; CLI/TUI help; and a roadmap checkbox once the
feature is implemented and verified. Do not mark it complete from this plan alone.

## Decisions and compatibility boundaries

- Approved implementation covers shared web/desktop, terminal and remote hosts;
  native-mobile onboarding and browser PKCE remain deferred.
- Direct endpoint compatibility and WHIP's integration identity passed live
  acceptance. Supported models retain vetted output ceilings; smaller explicit
  caps are refused. Availability remains specific to the connected account.
- If “official” requires an OpenAI-published third-party integration agreement/API
  contract, establish that requirement before selecting the direct adapter. The
  documented app-server route is available to investigate, with a larger runtime
  integration cost.

The proposed default remains the smallest native provider path: one host account,
one device flow, one Responses adapter, and reuse of WHIP's existing runtime/UI.
