# Whip MCP research supporting dossier

Research-only review, September 11, 2026. These are the full source-referenced research appendices supporting the consolidated report. No source changes were made. Whip reviewed MCP implementation: 4f29f87e2c13e7ab066ab1255db60784ba5c5945; concurrent mobile/docs commit advanced checkout to fc92cccf043a83c2834f2ce63f24cd793dd8c903 without changing reviewed MCP source. Executor: eaa1f3a57ffff88aede8e83783ea7ed4471aec1f, cloned at /tmp/whip-mcp-review-executor. Individual reports distinguish executed tests and probes from source-supported behavior and untested hypotheses.


---

# Research appendix: Backend

Source artifact: 1286df01ce2349f008862b59b93981b9, bytes 0–24131.

# Whip MCP backend review (research only, 2026-09-11)

Scope: internal/mcp; config discovery/import/secrets; daemon lifecycle and MCP control actions; protocol MCP wire types; cmd/whip/mcp.go. No frontend/model-SDK review. No repository edits, staging, or commits. Only /tmp overlay tests and a coverage profile were created. Existing unrelated mobile/docs work was left untouched (other concurrent changes appeared during the review). No real MCP servers, credentials, or home configuration were read by the test runs. All my shell jobs have finished.

## Executive summary

The backend has unusually strong call-admission and connection-retirement coverage (93.5% statement coverage for internal/mcp in my isolated run), but several untested boundaries fail. Most actionable: **managed stdio sends secret references literally**, **HTTP redirects leak configured credentials across hosts**, and **untrusted configurations can execute programs during startup before tool-call consent**. Additional verified defects: tool catalog pagination is ignored, automatic reconnect stops after the first failed retry, unrelated valid TOML can break all Codex discovery, and secret-command resolution ignores cancellation/startup lifetime. OAuth is not implemented; support is static headers/environment references/external !command helpers, not an OAuth flow.

Severity labels are review priorities, not CVSS: P1/high = fix promptly; P2/medium = normal-priority correctness/security boundary issue; P3/low = limitation/documentation concern. Evidence distinguishes direct local reproductions from static findings and product limitations. No unauthenticated network-level daemon exploit was demonstrated.

## Verified findings

### 1. P1 — Production managed stdio bypasses environment-secret resolution

**Evidence:** internal/mcp/manager.go:386-409 builds the production managed transport. It merges m.processEnv and cfg.Env with maps.Copy, without config.ResolveEnvMap. internal/mcp/manager.go:421-433 sends that unchanged map into ProcessManager.StartPiped. internal/capability/process.go:143-153 calls m.environment and assigns cmd.Env; :379-399 copies override values literally. In contrast, the fallback (unmanaged) transport explicitly resolves cfg.Env at internal/mcp/manager.go:1074-1087.

**Production reachability:** internal/daemon/daemon.go:274-286 always wires root.store.Processes() via SetProcessOptions before Start; the CLI diagnostic also chooses managed processes at internal/mcp/manager.go:908-916. So this is not an unused alternative branch. Native WHIP and imported Claude/Codex stdio configurations are both affected. "$TOKEN", "${TOKEN}", templates, and !command env references remain literal instead of resolving. The documented guarantee in docs/features.md:166-170 is therefore false for managed stdio. The ProcessManager's allowlisted base environment also does not incidentally inherit arbitrary API-key variables (internal/capability/process.go:358-376).

**Reproduction:** TestReviewManagedTransportResolvesEnvironmentReferences launched a real managed /bin/sh child with a fake test-only source variable. Configured REVIEW_DEST="$WHIP_REVIEW_TOKEN"; the child wrote exactly "$WHIP_REVIEW_TOKEN" into a temp marker file rather than "test-only-resolved". No real secrets involved.

**Fix direction:** resolve MCP env values at the shared point of use before the managed/unmanaged split (while retaining ProcessManager's safety validation). Keep import/persistence reference-preserving. Add the same reference/template/missing-variable cases to managed child tests, not only fallback transport construction.

**Coverage hole:** internal/mcp/manager_more_test.go:72-90 checks scope and literal env merging only; :36-69 exercises the fallback transport. Existing import tests prove references survive import, not that the daemon finally resolves them.

### 2. P1 — Configured HTTP credentials are reattached to cross-host redirects

**Evidence:** internal/mcp/manager.go:1051-1065 resolves headers and supplies http.Client{Transport: headerTransport(headers)} with the default redirect policy. internal/mcp/manager.go:1111-1115 unconditionally adds every configured header to every outgoing request, without comparing the request origin with the configured endpoint. net/http's usual stripping of Authorization on a cross-domain redirect is defeated because the RoundTripper re-adds it after redirect processing. bindHTTPContext copies the client and wraps its RoundTripper, but does not add origin checks (internal/mcp/http_transport.go:22-33).

**Impact:** a redirecting/compromised MCP origin or redirect endpoint can forward Authorization and custom secret headers to a different host. HTTPS-to-HTTP downgrade is also not explicitly rejected. The latter is static reasoning; the cross-host leak itself was tested.

**Reproduction:** TestReviewRedirectDoesNotLeakAuthorization used two local httptest servers, redirecting from 127.0.0.1 to localhost. The destination received the fake "Bearer TEST-ONLY-NOT-A-SECRET" configured only for the original endpoint. No real token or external request.

**Fix direction:** bind sensitive/configured headers to an exact permitted origin; reject cross-origin/downgrade redirects or deliberately rebuild safe headers under CheckRedirect. Test custom API-key headers as well as Authorization, and both normal requests and redirected DELETE cleanup.

**Coverage hole:** internal/mcp/coverage_test.go:77-94 only tests ordinary header injection. internal/mcp/http_transport_test.go:296-341 tests redirect cleanup deadlines, not credential scoping.

### 3. P1, conditional exposure — Tool consent does not protect startup execution of untrusted imported/attached definitions

**Evidence/flow:** imported definitions are explicitly untrusted (internal/mcp/config.go:262-269); attachments are forced to untrusted origin/source (internal/mcp/config.go:74-83). But newServer/Start select every enabled valid config, not trusted configs (internal/mcp/manager.go:212-237, :363-383). configureMCP eagerly starts them (internal/daemon/daemon.go:274-286). Managed startup executes the configured command at internal/mcp/manager.go:421-433. Project discovery is wired into root creation at cmd/whip/daemon.go:175-195. Attachments build/start a replacement before swapping ownership at internal/daemon/mcp.go:47-60.

**Important exposure qualification:** fresh defaults explicitly disable Claude/Codex imports (internal/config/config.go:642-645); do NOT describe this as unconditional fresh-install repository RCE. However absent mcpImport in a legacy/handwritten config means both sources enabled (internal/mcp/config.go:203-229), and enabling the Claude source admits project .mcp.json files. Once a source/config is admitted, a malicious project definition executes at connect time even though its later tool calls remain untrusted and require consent. The same applies to authorized clients supplying attachments; this does not establish unauthenticated client access.

**More surprising remote-only variant:** imported/attached HTTP headers may contain !command. defaultTransport resolves them before any network request (internal/mcp/manager.go:1051-1059); ResolveHeader delegates !values to ResolveSecret (internal/config/secret.go:261-265), which executes the local program (internal/config/secret.go:64-75). Thus a nominally remote HTTP definition can execute an arbitrary local command as a secret provider before tool admission. Header references can also read daemon environment variables and send them to that endpoint; that follows statically from :54-62 and header injection.

**Reproduction:** TestReviewUntrustedStartupRequiresConsent used the production ProcessManager wiring and an explicitly untrusted attachment whose /bin/sh command created only a temporary marker. The marker appeared without any tool admission. TestReviewRemoteHeaderCommandRequiresConsent separately proved an untrusted HTTP header !/usr/bin/touch created a temp marker merely while constructing its transport; no server was contacted.

**Interpretation:** high-confidence behavior and an important trust-boundary/product-policy gap. If enabling an import source is intended to authorize arbitrary startup execution from all its future project configs, document that explicitly; per-tool consent is not a sandbox. Safer policy is per-definition connect/spawn consent (including secret providers and endpoint/env disclosure), bound to config identity, separate from tool invocation consent. Fresh-default-off is useful mitigation, not a substitute once a source is enabled.

### 4. P2 — Only the first tools/list page is retained

**Evidence:** startup calls sess.ListTools(ctx, nil) once at internal/mcp/manager.go:521-526 and publishes listed.Tools at :563; the surrounding loop only retries if a catalog notification raced, not for NextCursor. Refresh repeats the same mistake at internal/mcp/manager_call.go:313-326. No cursor is consumed in either path. ListTools exposes exactly server.defs (internal/mcp/manager.go:669-693), so all later-page tools are absent from discovery and cannot be resolved/called.

**Reproduction:** TestReviewCatalogIncludesAllPages used an in-memory SDK server whose first page has one tool and nextCursor="second", and whose subsequent tools/list response would provide a second tool. Manager.ListTools returned only one. This tests the actual SDK integration, not just a hypothetical interpretation of ListTools semantics.

**Fix direction:** use the SDK all-pages iteration facility or an explicit bounded cursor loop under one startup/refresh deadline. Bound total tools/bytes and repeated cursors; atomically publish the full catalog and preserve generation invalidation semantics. Exercise both initial listing and list_changed refresh across pages.

### 5. P2 — Automatic reconnect gives up after one failed redial, not three

**Evidence:** the successful-session watcher is the only call site of kickAutoReconnect (internal/mcp/manager.go:581-597). The scheduler increments autoTries and queues one reconnect (:142-177). A failed connect merely sets StatusFailed and returns (:605-620), while run goes back to waiting for another reconnect request (:466-484); it never schedules the next attempt. Therefore the advertised autoReconnectMax=3 / exponential 1,2,4 second sequence is not executed when the first reconnect fails.

**Reproduction:** TestReviewReconnectRetriesAfterFailedAttempt established a healthy in-memory server, dropped its session, and made subsequent connect attempts fail. With a 1ms test backoff, after 250ms there were exactly 2 total connects (initial + 1 retry), not initial + 3. Manual reconnect still works; initial-connect automatic retry is separately not implemented and may be intentional.

**Coverage hole:** TestManagerAutoReconnectGivesUp at internal/mcp/manager_test.go:365-407 claims exhaustion of three retries, waits up to five seconds, but only asserts StatusFailed and attempts <= maximum; it never asserts the lower bound/exhaustion. Thus it passes precisely this broken behavior. The successful-first-redial test at :323-362 also passes.

**Fix direction:** reschedule eligible failed automatic attempts until the cap, preserving disabled/removed/closed cancellation and avoiding replay of calls. Strengthen the existing test to assert exactly initial + configured retries and eventual recovery after an intermediate failed retry.

### 6. P2 — Valid unrelated TOML syntax can erase all discovered Codex servers

**Evidence:** ParseCodex parses the entire config before filtering mcp_servers tables (internal/mcp/codextoml.go:31-49). The home-grown table reader rejects any array-of-tables header ([[...]]) at :244-247 and parses every value at :255-264. Valid multiline arrays/strings and additional TOML constructs also exceed the deliberately small parser documented at :26-30. Therefore unrelated non-MCP Codex settings can cause all otherwise-valid MCP entries to disappear. LoadMergedFiltered records the parse error and proceeds without Codex definitions (internal/mcp/config.go:287-291).

**Reproduction:** TestReviewValidUnrelatedTOMLDoesNotBreakMCP supplied a valid [mcp_servers.good] command followed by [[unrelated.entries]]. Parsing failed with "line 3: unsupported table header". This is a compatibility gap explicitly anticipated as a small-parser limitation, but its collateral effect on unrelated configuration is a concrete defect.

**Diagnostic aggravation (static):** normal daemon root construction consumes discovery.Merged/Blocked but never discovery.Errs (cmd/whip/daemon.go:175-195); mcp import similarly ignores disc.Errs and can report "nothing to import" (cmd/whip/mcp.go:280-300). mcp list does print errors (cmd/whip/mcp.go:84-86); ACP also emits discovery errors (cmd/whip/acp.go:184-187). Users can therefore see a silent empty catalog in normal use and only learn the cause by manually running list.

**Fix direction:** use a compliant TOML parser, or truly isolate/skip non-MCP syntax rather than partially parsing the whole file. Keep errors visible in the daemon status/diagnostic surface and make failed imports distinguishable from an empty successful import.

### 7. P2 — Header secret commands outlive cancellation and can exceed the startup budget

**Evidence:** defaultTransport accepts ctx but header resolution calls config.ResolveHeader without it, serially for each header (internal/mcp/manager.go:1044-1059). !command resolution creates its own context.Background() with a fixed 5s timeout (internal/config/secret.go:12-13, :64-75). Manager's startup timeout is established before transport construction (internal/mcp/manager.go:497-510), but cannot stop these commands. Multiple secret headers can therefore consume up to their independent command timeout each, even after startup/manager cancellation. Unmanaged stdio env resolution shares this helper; managed stdio currently misses resolution entirely (finding 1).

**Reproduction:** TestReviewCancelledHeaderResolutionStopsPromptly passed an already-cancelled context and a fake !/bin/sleep 1 header provider. Transport construction still executed/waited approximately 1.016 seconds instead of promptly returning. This does not rely on any remote server.

**Fix direction:** propagate caller/connection-startup context through secret/header/env resolvers and cap with min(parent deadline, helper timeout). Bound helper stdout; authorize helper execution as part of finding 3. Preserve successful transport lifetime after startup ends.

## Auth / feature inventory and non-bugs

- **No native OAuth implementation found.** ServerConfig exposes command/env/cwd/url/headers/enabled/timeouts/provenance only (internal/mcp/config.go:31-51); remote transport is a static-header http.Client (internal/mcp/manager.go:1044-1065); CLI routes list/add/remove/import/serve/test with no login/logout/auth flow (cmd/whip/mcp.go:22-48, :142). No MCP OAuth discovery, registration, PKCE/browser callback, token persistence/refresh, or 401 re-auth state is wired in the reviewed backend. An OAuth-only server will not work merely because it works in Claude/Codex; their token stores are not imported. This is a capability gap, not evidence of a broken implemented OAuth flow.
- Codex bearer_token_env_var becomes "Authorization: Bearer $VAR"; header references survive import (internal/mcp/codextoml.go:142-154, :193-204). These are resolved once per connection, not refreshed each request. External !helpers can supply bearer tokens, but expiry requires reconnect; this is not OAuth orchestration.
- Legacy SSE is intentionally disabled with a clear note by the Claude parser (internal/mcp/claude.go:56-66). Streamable HTTP includes standalone SSE for notifications (internal/mcp/manager.go:1060-1065); that is distinct from the old SSE transport.
- Import order is native/materialized WHIP > Codex > project .mcp.json > global ~/.claude.json (internal/mcp/config.go:275-341). Exclude wins over Only, and filtered names remain visible as disabled blocked records (:232-243, :304-343). Import materialization preserves Origin/Source, references, enabled flags, and timeouts and never overwrites an existing WHIP map entry (cmd/whip/mcp.go:285-319). Because WHIP map entries always pass the import gate, materialized imported definitions remain when a live external source is switched off; this is current precedence behavior, not automatically an authorization bug.
- Codex tool-level approval sub-tables are deliberately skipped and unknown keys ignored (internal/mcp/codextoml.go:60-66, :188-190). Consequently Codex-specific enabled_tools/disabled_tools/tool approvals are not imported into equivalent WHIP restrictions. WHIP has its own consent/delegation policy, so do not claim a bypass of WHIP consent; documentation should warn that foreign per-tool restrictions are not preserved.
- Reconnect/enable/disable are live manager mutations, not persisted config changes (internal/daemon/client_control.go:1242-1261; internal/mcp/manager.go:857-896, :987-1006). Source import toggling persists config and reloads the root (:1264-1302), so it is a broader lifecycle action. Model/config reload replaces the entire manager (:1446-1472). Ephemeral attachments are likewise replacements, not additive updates (internal/daemon/mcp.go:47-60).
- Exposed content remains text-first: images/audio/binary embedded resources are replaced by omission markers; StructuredContent is emitted only when the ordinary flattened output is empty (internal/mcp/manager.go:774-809). No resources/prompts/sampling/elicitation host operations are provided by this MCP manager. Treat these as scope/feature limitations, not protocol crashes.

## Positive architecture and tested defenses

- Imported/attached origin cannot spoof native trust through Source/Trusted serialization: ServerConfig.UnmarshalJSON clears trust; AttachedConfigs force-clears; native definitions overwrite attachment collisions (internal/mcp/config.go:54-83; internal/daemon/mcp.go:52-56; internal/protocol/mcp.go:38-53). Exact definitions are hashed including config and tool; raw server/tool names and connection generation bind admission (internal/mcp/manager_call.go:60-93).
- CallChecked validates schema and exact identity before queueing, after acquiring the per-server slot, and again after the admission callback, and checks cancellation before dispatch (internal/mcp/manager_call.go:184-259). It does not retry transmitted calls. Connection retirement cancels active/queued admission contexts before dropping catalogs (:33-44), and catalog notification invalidation precedes refetch (:298-326). This is good TOCTOU design notwithstanding startup execution being outside the boundary.
- Tools share a root-owned manager; children resolve that owner dynamically and snapshot narrowed selectors at delegation (internal/daemon/mcp.go:16-44, :65-116). Registry has explicit status, reconnect, enable/disable, source import status/configure, and ephemeral attach actions (internal/protocol/runtime_registry.go:63-68, :82); protocol wire timeouts use snake_case and trust is omitted (internal/protocol/mcp.go:9-21, :27-53).
- HTTP connection lifetime handles detached SDK SSE body reads and cleanup; DELETE has a shared 1-second deadline even across redirects (internal/mcp/http_transport.go:13-51, :62-94). Healthy SSE survives startup completion; Close waits owned workers. ProcessManager owns stdio groups and confines environment overrides, instead of ordinary inherited exec.Cmd processes in production.
- whip mcp serve creates a daemon tool-host root, requests deny_permissions, serves dispatcher-backed raw tool schemas/calls, and deletes the root after serving (cmd/whip/mcp.go:145-179, :184-215; internal/mcp/serve.go:28-59). It does not expose a model-agent rlm_exec entrypoint.

## Validation run and evidence artifacts

Executed through shell.start, with a temporary HOME, existing Go build/module cache paths explicitly supplied, GOTOOLCHAIN=local, GOPROXY=off, GOSUMDB=off, and WHIP_TEST_SELFHOST empty. The Go binary was the already-installed Go 1.27.0 toolchain. This kept tests offline and away from actual home configuration. The first attempt using the generic go launcher failed before tests because automatic toolchain checksum verification was disabled; using the already-installed binary fixed this without downloading anything.

1. go test -race -count=1 -timeout=120s ./internal/mcp -skip TestRealCodexConfigSmoke — PASS, 26.902s. No race report.
2. go test -count=1 -timeout=120s ./internal/daemon ./cmd/whip -run TestMCP — PASS, daemon 7.171s, CLI 1.543s. Includes CLI add/list/remove/test/import, daemon attachment/admission/delegation/reload tests.
3. go test -count=1 -timeout=120s -coverprofile=/tmp/whip-mcp-backend-review/coverage.out ./internal/mcp -skip TestRealCodexConfigSmoke — PASS, 24.618s, **93.5% statements**.
4. go test -overlay=/tmp/whip-mcp-backend-review/overlay.json -count=1 -timeout=30s ./internal/mcp -run TestReview -v — eight deliberately failing desired-invariant assertions reproduced the seven findings above (two separate startup tests), final package runtime 1.849s, exit 1. This is not an existing test-suite regression; these are research-only probes overlaid in place of realconfig_smoke_test.go.

The actual TestRealCodexConfigSmoke was explicitly skipped: internal/mcp/realconfig_smoke_test.go:12-32 reads the real ~/.codex/config.toml when present and can log header maps. It is not opt-in despite its CI-smoke comment; that is a test hygiene/security concern (static, no real configuration inspected). Existing selfhost/e2e tests requiring WHIP_TEST_SELFHOST stayed skipped. No full-repo test/build/lint or real OAuth/server interoperability run was performed.

Strong existing suites: internal/mcp/manager_checked_test.go:68-515 (schema/no-network refs/exact large IDs, authority queue checks, catalog invalidation, close/reconnect, retained large result); internal/mcp/http_transport_test.go:149-341 (stalled SSE/startup timeout/healthy notifications/DELETE cancellation); internal/mcp/config_trust_test.go:12-105 (import provenance/spoof resistance); internal/daemon/mcp_test.go:110-409 and mcp_reload_test.go:16-176 (host admission, child narrowing, ownership replacement, saved consent, reload). Gaps exposed here are cross-origin credentials, actual managed reference resolution, paginated catalogs, connect/spawn consent, unsuccessful reconnect chains, unrelated full-TOML input, and secret-helper cancellation.

Artifact with exact research test source: 462b4d6e4ccf1326f54e6ab347129d27 (5248 bytes). Artifact with final failure output: 1a69459697181172a24a75a874a959e2 (1980 bytes). Both contain only fake secrets/local tests. Source/overlay/coverage remain under /tmp/whip-mcp-backend-review for follow-up, not in the repository.

## Unproven hardening leads (not promoted to findings)

- Wire/catalog/result size caps and malformed catalog entries deserve fuzzing before claiming a memory-exhaustion or panic vulnerability; application-level schema/instruction limits do not by themselves prove transport allocation bounds. I did not run adversarial memory-exhaustion tests.
- Extremely large integer schema constraints are validated through map[string]any float representations even though exact argument wire JSON is preserved (internal/mcp/manager_call.go:128-158); potential constraint-rounding discrepancies were not tested and are not a confirmed finding.
- mcp serve uses context.Background and deletes its tool-host root only on the normal post-Serve path (cmd/whip/mcp.go:150-179). Early configuration errors/signal interruption may leave a persisted tool-host root; this needs a daemon lifecycle reproduction, not a confident claim of a process leak.

Recommended repair order: managed env regression; credential origin scoping; explicit connection/secret-provider trust policy; pagination; reconnect chain; TOML/error visibility; cancellable secret helpers. Preserve the current strong generation/admission/retirement semantics while addressing these boundaries.


---

# Research appendix: Model and TypeScript SDK

Source artifact: 97dd8f25e24edde7aadfb56dbc51e7ed, bytes 0–14482.

# Whip MCP model / TypeScript SDK review (research only)

Scope: RLM MCP surface, authority, permission/grant lifecycle, ACP attachments, SDK/protocol ergonomics. No source edits. Read root AGENTS.md context, docs/frontend.md (architecture/protocol boundaries), Go orchestrator/security/style and ponytail guidance; find found no nested AGENTS.md/CLAUDE.md in internal/packages/cmd. Existing unrelated mobile/docs working-tree edits were left alone.

## Findings, ranked

### P1 / High — SDK empty MCP server selection silently becomes unrestricted
**Verified source path + executable normalization reproduction.** Public AgentInput.mcp.servers is exposed at packages/sdk/src/agents.ts:111-112. defineAgent builds mcp via list(input.mcp?.servers) at :180; list at :211-213 maps both undefined and [] to null. Go explicitly distinguishes null (all configured servers) from an explicit list (including empty: no servers), internal/agentdef/definition.go:155-159. Canonical normalization intentionally preserves empty MCP lists, internal/agentdef/document.go:46-55. Runtime startup filters only a nonnil list, cmd/whip/daemon.go:178-189.

Repro executed against existing compiled SDK (source matches):
node --input-type=module -e 'import {defineAgent} from "./packages/sdk/dist/agents.js"; for (const servers of [undefined,[],["docs"]]) console.log(JSON.stringify({input:servers,output:defineAgent({id:"test-mcp",modules:["mcp"],capabilities:["mcp"],mcp:{servers}}).document.mcp}));'
Output: omitted input -> {servers:null}; input [] -> {servers:null}; input ["docs"] -> {servers:["docs"]}.

Impact: code calculating zero allowed servers can register an agent that sees/uses every native configured server (native calls need no further prompt), contrary to the authoritative explicit-empty contract. Fix direction: preserve [] for MCP specifically, add omitted/empty/named normalization and wire/runtime tests. Do not globally change list() because other canonical fields deliberately collapse empty values. No 'mcp' text found anywhere in packages/sdk/test; existing agent fixture tests at packages/sdk/test/agents.test.ts:1-67 exercise canonical definitions but not this distinction.

### P1 / High — root definition MCP allowlist is only an initial-manager filter; mcp.attach bypasses it
**Verified call path by source; no new end-to-end reproduction added.** Definition's allowed server names are filtered when the initial manager is constructed, cmd/whip/daemon.go:177-195. Root capability is only a blanket MCP boolean, not a server constraint: internal/daemon/definition.go:97-104. mcp.attach invokes attachMCP from internal/daemon/client_control.go:864-873. attachMCP constructs a fresh manager from all attachments plus ALL native config and swaps it, with no definition allowlist intersection, internal/daemon/mcp.go:47-58. Calling then resolves the new manager, internal/tools/mcp.go:89-101; root authority admits all selectors.

Concrete scenario: a registered definition restricted to ["docs"] is live; a client invokes mcp.attach with an unrelated attachment (or {} via generic API). The new manager includes native servers omitted by the definition (and attached servers excluded by it). Native definitions are trusted, so a root carrying MCP capability can call them without new consent. This is not an unauthenticated network exploit: attachment is a trusted-client operation, but unexpectedly widens the agent's declared server sandbox. ACP is an actual caller: internal/acp/bridge.go:213,258 -> cmd/whip/acp.go:108-140 -> mcp.attach when nonempty attached set. Inspect reload similarly (backend sibling owns that implementation).

Fix direction: one definition-aware manager-configuration filter used at startup/attach/reload AND preserve allowed server scope in authority if it is meant as a security boundary. Add regression with docs-only definition and disallowed native+attached servers across attach/reload.

Correction to preliminary note: independently configurable named-child MCP allowlists do NOT exist: internal/agentdef/definition.go:188-197 Child has no MCP field. Children inherit Definition.MCP (:330-335), so do not report an independent named-child restriction bug. A fresh child spawned after manager expansion does inherit the expanded available catalog because delegatedMCPTools uses manager + parent's live grant, not selection, internal/daemon/mcp.go:65-92; existing children retain their snapshots.

### P2 / Medium — discovery has no bounded/handle-backed path and large catalogs fail the entire cell
**Verified source chain; threshold reproduction not executed.** internal/daemon/recursive_runtime.go:1632-1648 returns all tool entries with their complete schemas; no query/filter/cursor/max_bytes and no boundedText. Manager ListTools likewise marshals every input schema, internal/mcp/manager.go:667-693. Unlike results/instructions/grant inspection, the catalog has no handle fallback.

QuickJS host replies marshal this array directly, internal/rlm/quickjs_kernel.go:84-94. writeFrame rejects serialized replies over its limit, internal/rlm/protocol.go:44-51; default is 1 MiB, internal/rlm/worker.go:30. The returned error calls fail rather than becoming a catchable per-operation host error. Thus a catalog over ~1 MiB is not discoverable via model MCP at all and can abort a cell, despite successful MCP connection. Smaller catalogs still force the entire catalog through memory/output budget.

Fix direction: bounded discovery responses with handles or paging, optional narrow search/server/tool introspection; a fixture with a >1MiB total schema catalog and recovery assertions. Existing large-grants work is already a model: internal/daemon/recursive_runtime.go:1470-1485; successes use boundedText :1779-1794. Existing large result test internal/daemon/mcp_test.go:155-179 is not catalog coverage.

### P2 / Medium — large tool-error content bypasses output bounding and its handle is lost
**Verified source chain; oversized-error runtime reproduction not executed.** Remote result.IsError becomes an error containing the whole flattened output, internal/mcp/manager_call.go:272-274. RLM host stores/bounds output but returns that value alongside the original error, internal/daemon/recursive_runtime.go:1671-1675. The QuickJS kernel explicitly drops reply.Value on any error and copies out.err.Error() in full, internal/rlm/quickjs_kernel.go:87-93.

Consequences: even a 50KiB failed result loses its model-visible content handle (though the text may still be visible in the error), and an error >1MiB fails the worker frame/cell instead of allowing inspection via a reference. Error result data and transport/execution errors are conflated. This is particularly bad for tools that may have partially performed an effect: it inhibits inspection needed before deciding whether to retry. Existing test internal/tools/mcp_test.go:581-597 establishes no automatic retry on uncertain remote failures, which is good but does not cover large errors.

Fix direction: retain a structured result envelope including is_error/content handle, keeping host transport/authorization failures as exceptions; or put bounded error evidence in a small typed host error instead of duplicating its unbounded content. Cover medium/oversized isError results and ensure catchable errors preserve evidence.

### P2 / Medium compatibility limitation — MCP results are lossy text-only, not the full result contract
**Verified intentional limitation, not a regression/security flaw.** Actual CallChecked path calls flattenResult at internal/mcp/manager_call.go:272. internal/mcp/manager.go:768-809 turns images/audio/binary embedded resources into placeholders (:783-792), turns resource links into labels (:793-794), and only serializes StructuredContent when there is no text at all (:797-801). A server returning human-summary text plus a richer structured object loses the object; screenshot tools return no usable image to model/SDK. RLM then exposes only output string/handle (:1671-1676), so discarded data cannot be recovered by opening a handle. No resources/read or prompts/get RLM operations exist (internal/rlm/modules.go:23-36), so a resource URI cannot be dereferenced through this module.

This is explicitly documented as a ponytail shortcut in source, so describe it as interoperability/product gap. Fix direction: preserve structured content and typed/binary content references before flattening display text; prioritize image and text+structured tools. Manager sibling may fold this into its protocol-compatibility findings.

### P3 / Low ergonomics gap — TypeScript MCP management exists only through generic operations, without a first-class service
**Verified API inventory, not a claim MCP is unavailable in the SDK.** WhipClient's dedicated services are sessions/providers/configuration/permissions/host/agents/terminals, packages/sdk/src/client.ts:64-74. Session offers generic command/query/invoke at packages/sdk/src/session.ts:14-21 but no mcp service. Protocol DOES expose typed generated mcp.status, reconnect, enable, disable, import.status, import.configure (internal/protocol/runtime_registry.go:63-68) and mcp.attach as Ephemeral (:82); consumers can use session.query('mcp.status',{}), session.command('mcp.reconnect',{name}), session.invoke('mcp.attach',{servers}). Therefore absence of convenience methods is not a functional blocker.

Potential developer benefit: one documented root-scoped mcp facade that makes ephemeral attachment semantics explicit, explains configuration is native/trusted versus attachments/untrusted, offers status + lifecycle methods, and hides snake_case wire keys where relevant. No dedicated generated RPC operations expose remote list_tools/instructions/call; those are model-host operations, not equivalent to SDK management. If client-side catalog inspection/testing is desired, add it at Go protocol + SDK boundary, not through a UI JSON-RPC workaround. docs/frontend.md:1458-1467 mandates generated maps / SDK services.

## What is already correct / tested

* Model gets one RLM module with four operations, not one JSON tool per remote tool: internal/rlm/modules.go:32; host dispatch internal/daemon/recursive_runtime.go:829-830. Host also enforces allowed modules independently of kernel installs, :801-804.
* list_tools shows exact configured tool/server identity, current definition/generation and authorized flag (capability grant, NOT consent), recursive_runtime.go:1632-1646. list_servers includes source/trusted but not connection env/headers (:1624-1631). Instructions are bounded with generation/source (:1649-1659). Prompt explicitly warns instructions/annotations are not authority, discovery required, separate consent, uncertain effects: internal/rlm/guide_fragments.go:61; inheritance snapshot documented :63.
* Calling validates a JSON object, resolves canonical server/tool+definition+generation, validates arguments before admission, binds current connection lifetime/cancellation: internal/tools/mcp.go:81-129. It supplies its own envelope, not model-controlled trusted/source flags. Ordinary tools route into capability dispatcher, tools.go:684-748.
* Calls validate BOTH grant and consent; native config/saved rules/automatic mode may satisfy consent but cannot widen capabilities or bypass an explicit rejecting Gate. internal/tools/mcp.go:132-178,196-251; policy matrix test internal/tools/mcp_test.go:123-179.
* After queueing, provider descriptor, generation, source/trust, complete grant chain and permission-policy revision are checked again before transmission, tools/mcp.go:160-177. Tests explicitly cover revoke/gate/headless/automatic/manager/definition/cancel while queued, internal/tools/mcp_test.go:223-278.
* MCPSelector binds server/tool/stable definition while generation is ephemeral, internal/capability/mcp.go:5-20. Child grants snapshot currently ready tool selectors, never blanket root grant; explicit mcp_tools must be an exact parent-authorized pair, daemon/mcp.go:65-116. Grandchild narrowing / revocation is exercised at daemon/mcp_test.go:181-213.
* AuthorizeMCP resolves complete persisted live ancestry, internal/session/capability.go:230-258,276-338. Root-only unrestricted grants and finite descendants documented :246-247. Tests cover exact delegation surviving reopen, escalation rejection, ancestor revoke pending descendants, rollback, cross-root and terminal ancestors: internal/session/mcp_capability_test.go:48,110,149,190,223,256. Revocation updates generation + settles permissions transactionally, internal/session/permission.go:262-279.
* Trusted cannot be forged on mcp.attach wire: protocol/mcp.go:23 and :43-55 clear/omit trust, daemon/mcp.go:52-55 rederive native trust from daemon config and native entries override attachment collisions. Attachment owner is root and services resolve its current manager at use time, daemon/mcp.go:16-44; tests daemon/mcp_test.go:216,272,296 cover manager replacement, native collision and remembered definition binding.
* ACP maps stdio and HTTP servers, native/base definitions win collisions, internal/acp/bridge.go:170-204; Initialize advertises Http but not SSE (:117), so lack of SSE input is not false capability advertising. Merge coverage bridge_test.go:890-902. ACP MCP approval displays concrete command/arguments and offers once/reject/tree definition-specific approval, internal/acp/permission.go:41-90; mcp_permission_test.go:9 tests concrete payload. Permission decisions route to durable daemon API (:94-101), not to a kernel self-approval.
* Exact integer IDs and large SUCCESS payload handles are tested through actual RLM at internal/daemon/mcp_test.go:155-179. MCP errors are not silently retried, internal/tools/mcp_test.go:581-597. Late permissions and retired generations have dedicated tests in tools/mcp_permission_test.go:30 and tools/mcp_test.go:470,528.

## Validation performed

PASS: go test ./internal/tools ./internal/session ./internal/daemon ./internal/acp -run 'TestMCP|TestBridgeMCP' -count=1
Results: tools 0.771s, session 1.060s, daemon 7.842s, acp 1.021s. Job completed; no process left running.
PASS (reproduces bug): Node one-liner above on existing SDK dist confirms [] -> null.
Not run: full TS SDK suite/build, new security fixtures, large-catalog/error runtime repro, real external MCP providers, OAuth/resource/prompt interoperability. Findings clearly distinguish source-proven chains from executed repro. No source changes.


---

# Research appendix: Outward bridge and attachment proof

Source artifact: e246332e4b6b376c171ab324a24f60aa, bytes 0–5797.

# MCP outward bridge and attach reproduction follow-up

## Three different flows
1. INBOUND MCP (Whip as client): Whip model -> RLM mcp discovery/call -> root-owned manager -> external MCP server via stdio/HTTP. Agent selectors + separate consent apply.
2. OUTBOUND MCP (Whip as server): external harness -> whip mcp serve subprocess over stdio -> daemon tool-host root -> built-in read/bash/edit/write. NO model loop, rlm_exec, imported catalog re-export, SDK custom tools, or full session/agent API. Sources: internal/mcp/serve.go:21-28; cmd/whip/mcp.go:150-175; internal/daemon/tool_runner.go:13-32; internal/tools/tools.go:484-520.
3. SDK EXECUTOR: client.agents.serve(definition) registers authored tools/hooks, binds definition revision and lease generation, receives tool.invoke/hook notifications, returns tool.result/hook.result over Whip daemon protocol. It is NOT MCP client/server. Sources: packages/sdk/src/agents.ts:218-258,290,330. SDK custom tools enter the model through definition tools module; Executor does not automatically appear in either MCP direction. Generic SDK MCP management query/command/invoke is separate again.

## Outbound surface, transport, lifecycle (verified)
* Exactly FOUR advertised tools: bash/read/write/edit, internal/tools/tools.go:516-520. Browser/computer/process tools not advertised (:521-524). Server only registers returned definitions, internal/mcp/serve.go:38-54. Test asserts 4/read/no rlm_exec, internal/mcp/serve_test.go:85-90.
* CLI creates SessionKindToolHost at cwd with automation client kind, cmd/whip/mcp.go:146-155; tool.schema/tool.call are actual RPC adapter actions (:184-215); toolRunner dispatches via Services, internal/daemon/tool_runner.go:45-50. No model turns (:29-32). Test internal/daemon/client_control_test.go:50-95 checks tool-host restriction and real read. CLI deletes temporary root after server exit, cmd/whip/mcp.go:174-179.
* Stdio only, internal/mcp/serve.go:56. No HTTP listener, bearer/OAuth auth, or remote MCP endpoint here. Authority is local subprocess/stdio access from launching harness. Internal daemon connector uses automation identity, cmd/whip/daemon_client.go:15-36; local Unix socket chmod 0600, internal/daemon/socket_unix.go:147-164. Do NOT call absent HTTP auth an exposed unauthenticated network service.
* Tools-only: new Server(nil options), AddTool loop; no AddResource/AddPrompt/approval-elicitation adapters, internal/mcp/serve.go:33-56. Results always one TextContent plus IsError (:46-53). Provider errors overwrite separate partial output (:48-49), a compatibility limitation.

## Approval/headless behavior
CLI always sends tool.configure {deny_permissions:true}, cmd/whip/mcp.go:165-172. Handler calls DenyToolPermissions, internal/daemon/client_control.go:895-905. Tool runner disables external prompting and installs GateReject with 'this automation client cannot approve side effects', internal/daemon/tool_runner.go:53-57. It does not treat an MCP invocation as user consent and has no forwarding/elicitation path.

Read has no permission flag; bash/write/edit do, internal/tools/tools.go:517-520. NEW decisions hit rejecting gate, internal/tools/permission.go:128-152. Thus write/edit/bash don't work on a fresh root merely because outer harness approved. Preexisting GLOBAL rules (or root rules installed via another daemon client) can preauthorize before Gate runs: internal/session/capability.go:554-561; permission_rules.go:100-126. Accurate wording: 'bridge cannot obtain new consent; already-preauthorized operations remain possible', NOT 'all side effects always denied'. No serve approval CLI flag: mcp.go:38-39 directly dispatches serve and :165 always selects deny.

P2/P3 product/documentation gap: bridge advertises write/edit/bash without a harness approval workflow. Document preauthorization or provide explicit trusted approval channel; do not silently equate MCP invocation with consent. Existing in-process test does NOT install production rejecting gate (internal/mcp/serve_test.go:40-43). Self-host test expects bash echo (internal/mcp/selfhost_test.go:42-47) but is env-gated (:18-20) and wasn't run here; it does not establish fresh-home denial semantics.

## Attach allowlist bypass: now executed via temporary Go overlay
No repo changes. Overlay augments existing internal/daemon/mcp_test.go with TestReviewMCPAttachWidensDefinitionServerSelection. Reuses localMCPFixture+mcpRuntimeFixture; installs definition MCP.Servers=[local] on runtime/root node while initial manager only contains local; saves native config {local,admin}; asserts admin absent; invokes ACTUAL clientCommand mcp.attach with empty server map; calls excluded admin/mutate through rootNode.host.Call. MCP automatic mode remains false.

Command:
go test -overlay /var/folders/q1/vxdpc0bd0f59c7k86x2vcsrh0000gn/T/whip-mcp-review-y89dgxil/overlay.json ./internal/daemon -run '^TestReviewMCPAttachWidensDefinitionServerSelection$' -count=1 -v

Observed PASS:
mcp_test.go:458: REPRO: definition servers=[local]; excluded admin call succeeded, effects=1 output=map[output:effect occurred]
Test 0.09s; daemon package 0.862s.

Caveat: fixture-level test installs selection directly rather than registering SDK definition and running full cmd startup. It exercises REAL daemon attachment, native trust/config, manager replacement, RLM call, grant/consent checks, network MCP fixture and counted effect. Startup restricted manager is independently source-backed cmd/whip/daemon.go:177-195. This upgrades static finding to demonstrated effect, not full registration E2E.

Also PASS: go test ./internal/mcp ./internal/daemon -run 'TestServeInProcess|TestToolHostRunsOnlyRestrictedToolCommands' -count=1 (0.357s,0.792s). No processes running. Temp overlay retained for parent rerun; no git edits.


---

# Research appendix: Web desktop and TUI UX

Source artifact: 64a03279ff3d80c3ee4fad665298ef9d, bytes 0–16815.

# Whip MCP UX review — web/desktop/shared app and TUI
Date: 2026-09-11. Research only; no source edits, staging, real credentials, real MCP servers, or provider requests. Existing dirty mobile/docs files were left alone.

## Evidence and limits
- Inspected root AGENTS.md guidance, docs/frontend.md (architecture, lifetimes, query/command, permission, styling and validation guidance), and Go how-to, CLI, testing, security and ponytail skills. find of apps/packages/internal/cmd/docs instruction files found only nested mobile rules, outside this review.
- **Source inspection establishes nearly all MCP-specific findings below.** It is not live end-to-end MCP UI verification, native Electron verification, screen-reader verification, or an OAuth trial.
- Executed existing isolated tests: npx vitest run --config apps/web/vitest.config.ts packages/app/test/inspector.test.tsx packages/app/test/requests.test.tsx — **39 passed** (20 inspector, 19 requests; 4.31 s). The only MCP-specific inspector test is the one-shot private attachment test at packages/app/test/inspector.test.tsx:472-503. Its fake HTTPS URL/header never leaves the mocked client.
- Executed go test ./internal/tui -run 'TestClientCLIParameters|TestMCPPermission' -count=1 -v — **4 top-level tests passed**, plus parameter subtests. Tests cover named disable/import fields, query routing, long MCP argument display, and paging. internal/tui/client_parameters_test.go:9-45; internal/tui/mcp_permission_test.go:12-64.
- Executed go test ./internal/mcp -run 'TestFlatten' -count=1 -v — **3 passed**: remaining content types, edge cases, large text preservation. Pure result conversion, no live server.
- Executed WHIP_PERMISSION_RESULTS=/tmp/whip-mcp-ux-permissions-20260911 node apps/web/scripts/permission-requests.mjs — **24 browser layouts passed**, Chromium + Firefox, desktop/phone/narrow-long/short, light/dark/claude-code themes. Checks bounded operation text, horizontal geometry, 44px narrow-screen approval target, keyboard scope, queue advancement/reset, and CSP. Outputs report.json, preview.png and scenario screenshots in that temporary directory. Build emitted a nonfatal >500 kB chunk warning.
- That browser fixture is **generic approval UI**, not MCP-specific: apps/web/scripts/fixtures/permission-requests/main.tsx:28-42 supplies bash/read requests and mocks decisions. It does not establish MCP server management coverage, actual Electron, Safari/WebKit, Axe, VoiceOver or physical device coverage. Script closes browsers and preview server; all jobs completed.

## Product flow and parity inventory
### Entry points and ownership
- Web and desktop render the same product code: apps/web/src/main.tsx:1-19 selects browser vs desktop platform and calls shared bootstrap; apps/web/src/bootstrap.tsx:3-13 uses @whip/app. docs/frontend.md:123-131 specifies identical renderer bytes, route tree, DOM/focus/layout. Thus no separate native MCP manager was found; desktop gains host transport/native effects, not additional MCP setup UI.
- Session inspector -> Host integrations -> MCP servers: packages/app/src/navigation.ts:10, inspector.tsx:10-36, details/integrations.tsx:17-44. MCP is nested in session inspection, not a top-level host onboarding destination. Host Settings only offers import-default toggles (settings/configuration.tsx:165-174).
- TUI /mcp command and MCP palette: internal/tui/registry.go:44; client_parameters.go:79-102; client.go:1749-1773. Both frontends direct daemon-owned work; neither should start MCP in the viewing browser/native renderer.

### Configure/add/remove/import
- Shared UI provides a **raw JSON server-name map**, not a structured persistent add wizard. It uses ephemeral session invoke('mcp.attach'), validates generated MCPAttachParams and 256 KiB limit, clears editor before sending, aborts local wait on unmount, invalidates query caches afterward. Text explicitly promises no drafts/query cache/recovery/browser storage. details/integrations.tsx:116-148. This secret-handling posture is good and is covered by the attachment test.
- No dedicated persistent add/remove, per-server edit, import preview/dry-run, include/exclude editor, connection probe or OAuth controls appear in that component. TUI parser exposes status/import/reconnect/enable/disable only; it has no /mcp add, remove, attach, auth or test branch (client_parameters.go:79-102).
- **CLI is materially more complete than TUI/web**: cmd/whip/mcp.go:22-45 and :56-142 provides list/add/remove/import/serve/test. list is a merged configuration view (enabled/disabled/blocked + target/source), not a live health check; add persists native config and explicitly says starts next launch (:89-115); remove persists native deletion, explains imported/blocked sources cannot be removed here (:118-139). Import dry-run/materialization is separate from live-import switches (:27-33, :47-48).
- Shared import toggles show source and Enabled/Disabled with Action wrappers (integrations.tsx:88-114). They change host config and queue session reload (daemon/client_control.go:1264-1322), unlike session-only manager enable/disable. The Settings configuration view also exposes the same host defaults (configuration.tsx:165-174), with host-shared scope text and revision-conflict preservation (:174-177).

### Status/reconnect
- Shared list shows server name, status badge, tool count, source, note, contextual error and three action buttons (integrations.tsx:43-86); status polls every 3 s through useDetailQuery (shared.tsx:26-47), with offline/unsupported/loading/retry handling (:49-80). Inspector gives retained/offline notice and keys subtree by runtime/root/agent/section (inspector.tsx:20-27). Good isolation and truthful offline foundation.
- Status is daemon live + blocked union, sorted by name (client_control.go:1227-1237). Underlying lifecycle states are disabled/connecting/ready/failed, not separate authenticated/authorized states (mcp/manager.go:30-64).
- TUI prints name/status/source/count/error-or-note, no table of credentials or target (daemon_views.go:12-36). It appends a snapshot on request; source shows no dedicated live MCP screen (thin_update.go:283-300). Web polling is stronger than TUI for noticing transition completion.
- Reconnect/enable/disable all call the same daemon actions (client_control.go:1242-1261). These mutate current manager only; no config persistence is performed in that path (manager.go:854-896). Reconnect is a request, not proof of readiness (manager.go:985-1006).

### Discovery and consent
- **The Available tools UI is not an MCP catalog**. It uses tool.schema (integrations.tsx:273-308), whose daemon implementation calls ToolDefinitions (client_control.go:907-913), explicitly returning public built-in schemas only (tools/tools.go:495-499). It has name search, 64-row disclosure cap and 32 KiB schema code cap, but no per-server MCP tool names, instructions, schemas or authorization indicator. MCP metadata exists separately in manager.go:66-72 and RLM module operations; UI currently exposes counts rather than a browse/inspect workflow.
- Incoming approval has server/tool/source/arguments in daemon-generated text (capability/rule.go:91-101), so the shared generic card does present meaningful MCP identity without custom parsing. It is labeled, aria-busy, keyboard-focusable/scrollable requested-operation pre; one-at-a-time queue with remaining count, scope reset via key, disabled state, and refresh-before-retry on uncertain outcome (requests.tsx:33-50, :91-177, :219). Good safety/recovery behavior.
- TUI specially pages large MCP details with pgup/pgdown and replaces the opaque MCP rule selector with “this MCP tool and server definition in this tree” (permission.go:60-79, :100-105; client.go:1525-1534). Tests explicitly verify final arguments and server identity remain reachable.
- Shared card allows once, tree remember, host/global remember, or deny; TUI offers once/tree/reject-with-message, not global remember (requests.tsx:148-176; permission.go:20-26; client.go:1536-1558, :1575-1585). Global remember in web is real supported host persistence, not a frontend authorization bypass (daemon/permission.go:139-156).
- Delegation inspector exposes raw MCP authority selectors and grant revocation (details/session-controls.tsx:200-225). Permission inspector exposes session-rule forget but **global host rules are read-only** (:571-600).

### Call outputs/errors
- Shared conversation uses generic tool disclosures, density preferences, compact 512-character/three-line preview, args code, full text, images if provided, and stored-message access (timeline.tsx:332-369). Generic rows only expose in-progress, not structured MCP server/tool/error state; stream projection updates args/result and stops live flag on completed (conversation-rows.ts:149-185). Nested RLM host calls may instead be inspected as execution evidence; no bespoke MCP result renderer found.
- Upstream result flattening is the limiting factor across clients: MCP text concatenated, images/audio/binary resources become omission placeholders, resource links become text, structuredContent used only when no text exists, isError becomes an “Error: ” prefix (mcp/manager.go:768-809). A successful transport with an MCP application error therefore is not necessarily a failed command/turn badge. Do not infer success from completed execution alone; richer display requires preserving typed result/error metadata upstream rather than adding one special web card.
- Remote transport configuration is static HTTP headers; no MCP OAuth initiation/callback/token state machine was found in MCP manager/config, TUI command grammar, CLI switch or shared UI. Whole-tree OAuth search found provider login code only. Treat OAuth as **unsupported**, not merely a hidden sign-in button. No authentication negotiation/live credentials tested.

## Prioritized findings and minimal improvements
### P1 — misleading attachment replacement semantics (high confidence, source)
UI says “Attach session MCP servers,” suggesting additive behavior (integrations.tsx:116-148). Actual attach builds a new manager from supplied map plus native cfg.MCPServers, swaps it, closes old manager, and does not merge old attachments/live imports (daemon/mcp.go:47-61). A second one-server attach can discard other session definitions; empty map can remove nonnative servers. At minimum rename to “Replace session MCP attachments,” explicitly describe native precedence and loss of previous attachments/imports, and show affected names before send. Do not persist private JSON just to improve retry convenience. Add second-attachment/empty-map tests and inspect desired backend semantics before implementing a merge.

### P1 — scope/durability and outcome ambiguity (high confidence, source)
Shared controls do not distinguish session-only enable/disable from host-persistent import settings, or queued reload/reconnect from actual readiness. Action always reports “Applied” when run resolves (shared.tsx:82-124); successful commands invalidate queries after result, correctly (runtime.ts:761-777), but daemon reconnect only queues work and import configure only queues reload (client_control.go:1298-1322). Minimal fix: “Disable for this session,” “Host import defaults,” “Reconnect requested—waiting for ready,” “Saved; session reload pending” based on actual response/status. Keep status visible and terminal transitions explicit; do not add ad hoc optimistic local server state.

### P1 — MCP authorization rules are opaque; host-wide grant cannot be revoked here (high confidence, source)
When remembering, requests.tsx:130 prints raw permission.rule; capability/rule.go:96-101 derives JSON selector including definition identity. TUI already supplies readable explanation. Reuse same semantics in web: “this tool on this exact server definition,” with digest in expandable technical detail and explicit selected scope. Web supports host-wide remember (:175), yet permission inspector only renders host rule code (:595-599), no removal. Minimal immediate fix: explicit host-config revoke instructions and confirmation copy; next targeted host-rule removal API/UI rather than implying Forget session rule revokes global approval. This is UX friction, not a demonstrated authorization escape.

### P1/P2 — discover/manage dead end (feature gap, high confidence)
Configured count -> cannot inspect individual MCP tool schemas/permissions/instructions; “Available tools” is built-ins only. Persistent add/remove and OAuth unavailable in both shared UI/TUI. Minimal immediate improvement: rename Built-in tools, link MCP status to exact host-side CLI/config guidance, show “OAuth not supported; configure host credentials externally” only for relevant auth errors, distinguish live import vs materialize native config. Next minimal feature: read-only per-server MCP tool catalog using daemon discovery contract, not renderer-side network calls. Structured add/edit, OAuth and full import preview are separate work, not cosmetic fixes.

### P2 — TUI palette is not sourced from daemon server inventory (high confidence, source)
client.go:1758-1773 only reads m.cfg.MCPServers; host live status includes imported/attached/blocked entries. This can omit remote-host or session-attached server actions even when /mcp status names them. Reuse daemon status names for palette; until then hint that manual /mcp <name> reconnect|enable|disable supports names not in palette. Add imported/attached/remote name fixture.

### P2 — actions shown despite invalid applicability/capability (high confidence, source)
Every row gets all three buttons and only connection gating (integrations.tsx:70-83); even blocked entries get enable/reconnect although absent from live manager and can return “no MCP server named” (client_control.go:1228, :1258-1259). Disabled servers should offer Enable first, not suggest reconnect overrides disable (manager.go:474-476). Attachment/import mutation buttons are not individually gated on operation support although reads are (shared.tsx:33-45). Minimal fix: show only meaningful state actions; explain blocked policy; gate each command/invoke on advertised support. Keep backend authority checks authoritative.

### P2 — accessibility context and generic error/result presentation (source findings; not live a11y violations)
Repeated Reconnect/Enable/Disable/Inspect schema/Forget rule buttons have identical accessible names without explicit subject labels; articles are not aria-labelledby (integrations.tsx:58-82, :291-302; session-controls.tsx:574-589). Add subject-qualified accessible names and aria-expanded/controls to schema toggle. Existing Base UI fields/selects, semantic headings, role=status, approval focus/scroll and bounded geometry are strong foundations. Avoid claiming screen-reader failures without testing.
MCP application errors are plain text within generic output, often behind collapsed disclosure, and rich media was already dropped upstream. First show an accurate textual error state when metadata exists; preserving typed MCP content is separate backend/protocol work. Never automatically replay an MCP tool on a renderer retry button because effects may have happened.

### P2 — incomplete specific regression matrix
Existing inspector MCP test proves private send/clear/cache behavior only; generic browser fixture never contains mcp.call. Add isolated MCP rows for ready/connecting/failed/disabled/blocked/unsupported/offline, per-server action names, second attachment and submission failure/uncertainty, two-host targeting, actual MCP argument JSON + opaque selector + scope/revoke copy, imported palette names, and isError/large result output. Run shared browser fixture in Chromium/Firefox first; actual desktop and screen reader remain separate validation gates.

## Recommended smallest sequence
1. Correct labels/help/subject accessibility and readable consent scopes; clarify global revoke and attachment replacement. Add focused regression tests; no new dependencies.
2. Make action availability/status truthful and source TUI names from daemon status. Reuse Query/SDK command machinery, no extra state store.
3. Add a read-only MCP discovery inspector to existing integration section; keep built-in tools distinct.
4. Decide persistent add/edit/import/host-rule revoke contracts; build once in shared app and daemon for both web/desktop, exposing equivalent TUI commands.
5. Treat OAuth and structured/rich MCP output as explicit separately scoped protocol/runtime capabilities with isolated test servers, not frontend-only work.

No source files were edited by this review. All confidence claims above distinguish source evidence from the executed generic browser fixture.


---

# Research appendix: Executor comparison

Source artifact: d9a06f7a92179b2ea08c24f02328aae6, bytes 0–28512.

# Executor comparative implementation review

## Scope and evidence

Repository: https://github.com/UsefulSoftwareCo/executor, read-only clone /tmp/whip-mcp-review-executor. Reviewed HEAD **eaa1f3a57ffff88aede8e83783ea7ed4471aec1f**. Whip HEAD observed **4f29f87e2c13e7ab066ab1255db60784ba5c5945**. Executor git status --short remained empty. Read root AGENTS.md and CLAUDE.md before exploration, checked nested instructions (only e2e/AGENTS.md found under apps/packages/tests/e2e), and read e2e instructions before those scenarios. Did not run bootstrap, install dependencies, start a product server, invoke external MCP/OAuth services, or alter source/config. Test statements below describe implementation inspected, NOT tests executed or passing. No live credentials read. All source citations below are relative to Executor HEAD unless explicitly labeled Whip. Permanent link pattern: https://github.com/UsefulSoftwareCo/executor/blob/eaa1f3a57ffff88aede8e83783ea7ed4471aec1f/<path>#L<line>.

## Executive judgment

Executor is primarily an **integration/catalog/credential broker with a code-mode MCP facade and an operator console**, not a durable agent runtime. Its strongest transferable ideas are progressive discovery with compact TypeScript descriptions, distinct integration vs account/connection identities, runtime-observed output shape provenance, and a complete integration/tool/policy management UI. It has substantial protocol compatibility and lifecycle engineering beyond a demo. Whip should borrow narrow UX and discovery mechanisms, not replace its authority or durable execution model with this stack.

Three caveats materially change the comparison:

1. **Approval does not imply human approval by default.** Missing/unknown elicitation_mode selects model; resume accepts the model's accept/decline/cancel. Separately, upstream tools require approval by default only if destructiveHint is explicitly true. This is incompatible with Whip's kernel-never-approves and consent/grant model unless deliberately redesigned.
2. **The TypeScript experience is generated guidance, not runtime static type checking.** Sucrase removes syntax and tools is a lazy proxy; runtime tool/schema validation remains the real boundary.
3. **A code-supported QuickJS timeout bypass deserves isolated validation before reuse:** pending tool dispatch disables the interrupt deadline, so an unawaited tool call followed by an allocation-free infinite loop appears able to wedge the in-process host. This was not executed; detailed trace below.

## 1. Architecture and actual call path

### Two MCP directions, not just one MCP client

- Outbound plugin: packages/plugins/mcp/src/sdk/{plugin,discover,connection,invoke}.ts implements connecting to upstream MCP servers. Remote transports are streamable-http, SSE, auto; stdio is separate. Types distinguish one integration/server from a connection/credential (types.ts:13-32).
- Inbound MCP host: packages/hosts/mcp/src/tool-server.ts:1550-1562 registers execute with {code:string}. skills is registered at 1566 onward, resume at 1593-1642. Tools search_<integration> are optional at 1646-1696.
- Common execution: packages/core/execution/src/tool-invoker.ts:307-318 transforms the sandbox dotted path to a ToolAddress and invokes executor.execute(address,args,invokeOptions). The carrier-neutral SDK then resolves policy, plugin, connection, credential and actual provider call.
- In SDK dynamic execution, blocking policy is enforced first (executor.ts:6389-6403); plugin/connection existence is checked (6405-6427); argument validation precedes an approval pause if supported (6442-6450); enforceApproval runs before credential resolution (6452-6479).
- Product composition chooses the runtime, rather than coupling MCP to a single execution backend. Local wires QuickJS (apps/local/src/main.ts:75-99); self-host selects QuickJS (apps/host-selfhost/src/execution.ts:71); cloud builds the dynamic-worker executor (apps/cloud/src/engine/execution-stack.ts:118). Runtime packages also include deno-subprocess/workerd-subprocess; their existence does not mean they are the default product path.

This separation is worth borrowing conceptually: discovery/credentials/policies need not be protocol-specific or owned by a React screen. It also means this project is much broader than a thin MCP adapter, with Effect services, multi-host storage and OAuth deployment concerns that would be costly to transplant into Whip.

### Compact tool surface, but qualify the marketing shorthand

execute description is dynamically assembled from a short workflow pointer plus connected integration inventory (packages/core/execution/src/description.ts:19-47). Full instructions are behind skills. Search and describe are called inside execute, not thousands of initial model tool definitions.

However, it is **not unconditionally a three-tool server**. readArtifactsEnabled defaults ON (packages/hosts/mcp/src/browser-approval.ts:54-65); tool-server additionally registers create/edit/show/list artifact operations and app-only execute-action/resume (2090-2318). search_<integration> is opt-in, designed to surface namespaces in clients that ignore descriptions without dumping schemas (1646-1696). A fair comparison must mention these modes.

## 2. Discovery and model-facing typed surfaces

### Discovery funnel

- tools.search({query,namespace?,limit?,offset?}) defaults to 12 results; blank query with no namespace returns an empty page, deliberately avoiding an arbitrary whole-catalog dump (tool-invoker.ts:657-685).
- It obtains tools.list({includeAnnotations:false}), maps searchable entries, filters/scores and sorts (688-722). Search weights are path=12, integration=8, name=10, description=5; normalization splits camelCase and punctuation (484-502). This is lexical ranking, **not embedding/vector search**.
- Blank query WITH namespace is a complete exact-integration enumeration ordered by path, specifically excluding prefix sibling integrations; total should reconcile to inventory counts (699-716).
- Pagination explicitly returns items,total,hasMore,nextOffset (430-460). Models need not infer truncation or synthesize offsets.
- tools.describe.tool({path}) supplies inputTypeScript, a discriminated ToolResult output type, and named definitions. Unknown paths return a tool_not_found value with up to five suggestions, preferring namespace matches (823-887).
- The execute skill gives the search -> describe -> call workflow and explicitly discusses paginating, limiting discovery, ToolResult, and emit (packages/core/execution/src/skills.ts:35-59).

**Strength:** This is a well-designed low-context discoverability funnel that supports both intent search and exhaustive namespace census. Good candidate for Whip's mcp tools UX without changing dispatch names or grants.

**Ceiling:** Default search loads all visible tool summaries and scores/sorts in memory per query (688-722). It is not a database-backed search index despite emitting small pages. Its injectable ToolDiscoveryProvider (423-426,735-737) is a sensible upgrade seam. Do not confuse bounded model output with bounded catalog work.

### What “TypeScript” means here

- Static host/plugin authors get useful inference: defineExecutorConfig uses const TPlugins to preserve the factory's plugin tuple without module augmentation (packages/core/sdk/src/config.ts:33-51).
- Dynamic JSONC plugins are intentionally loose at the import boundary: factories return AnyPlugin and author types do not propagate across runtime discovery (packages/core/config/src/load-plugins.ts:33-38).
- Model code is syntactically transpiled with Sucrase typescript transform, no semantic checking (packages/kernel/core/src/strip-types.ts:3-34).
- QuickJS implements tools as a lazy callable Proxy. Property access accumulates a dotted path; enumeration throws a helpful search instruction; calling dispatches the first argument and parses JSON results (runtime-quickjs/src/index.ts:228-251).
- Therefore a generated inputTypeScript string is not a compiled capability-checked binding. A guessed path can still be dispatched; the SDK checks actual address/policy/connection. This should not be sold as compile-time safe LLM code.
- Output wrapper is {ok:true,data:T,http?:ToolHttpMeta}|{ok:false,error:ToolError}; expected operational failures become inspectable values, while internal/plugin defects become opaque errors with correlation IDs (tool-invoker.ts:25-39,307-360). Good model ergonomics: programs can branch on ok rather than scraping exceptions.
- The QuickJS bridge uses ordinary JSON.stringify/JSON.parse (index.ts:114-123,248,332-339), not Whip's lossless BigInt/exact-number payload codec. Wholesale reuse would regress Whip's numerical and passive-payload guarantees.

### Runtime-observed schemas

A notably borrowable feature: successful tool outputs are folded into owner-scoped shape memory after dynamic invocation; static tools and failed results are skipped (SDK executor.ts:6569-6581). Inference stores field names and broad types, not values, merges optional fields/unions, and caps depth=6, sampled array entries=5, object keys=24, union width=4, serialized size=16000 chars (shape-inference.ts:1-17,32-43,178-209). Large-key objects degrade to maps to reduce data-bearing-key retention. Shape memory loads/writes plugin storage and skips unchanged-schema writes (shape-memory.ts:38-73).

Describe clearly labels observed types, including inline “observed; may be incomplete” and observation count (tool-invoker.ts:37-39,872-885). This is useful for MCP servers with no outputSchema, while preserving declared-vs-inferred provenance.

Caveats: inferred shape is evidence, not authorization or authoritative schema; field names can still be sensitive even without values. The cache maps in shape-memory.ts:39-40 have no obvious count eviction in that module. A failed storage write is swallowed and its schema marked persisted at 69-72, so unchanged shape will not retry that write for this live instance. These are scaling/reliability ceilings rather than blockers to the idea.

## 3. MCP compatibility and operational behavior

- Discovery collects paginated tools and server initialize instructions. It has MAX_LIST_TOOLS_PAGES=100 against nonterminating cursors and a 15-second overall discovery bound (discover.ts:17-32; pagination loop 56-119).
- Negotiation explicitly supports compatibility fallbacks: discovery can retry legacy mode for modern-contract violations on remote transports (discover.ts:137-153). Transport configuration supports streamable HTTP/SSE/auto and legacy-vs-auto stdio negotiation (types.ts:28-39).
- MCP catalog fidelity is preserved: actual upstream tool name, upstream annotations and _meta are stamped into persisted annotations, rather than reconstructing them from normalized tool names (plugin.ts:535-556).
- During invoke, list_changed is captured synchronously and persisted catalog marked stale after the call exits; unknown-tool error envelopes also mark it stale (plugin.ts:1699-1735). Next tools read can rediscover instead of leaving a permanently stale catalog.
- Missing API-key placement variables fail explicitly before dialing unauthenticated (plugin.ts:1653-1667). Credential values are resolved at invocation time, supporting OAuth refresh rather than baking secrets into model code (types.ts:15-21; SDK executor.ts:6454-6486).
- Connection pool is per plugin instance, exclusively leases a connection per invocation, retains at most one idle connection per identity, and applies lazy 5-minute idle eviction plus one fresh-dial retry when a reused session gets 404 (connection-pool.ts:63-95). Invocation pool key includes template, values, owner and connection (plugin.ts:1681-1690).

**Strength:** A lot of protocol edge cases are implementation and tests, not just aspirations. Useful regression-checklist material for Whip: paginated catalogs/cycling cursors, initial instructions, stale-session 404, catalog changes mid-call, missing credentials, stdio safe env and interrupted cleanup.

**Ceiling:** Lazy idle sweep is activity-driven, not a hard wall-clock expiration when the whole pool goes unused (connection-pool.ts:76-95). Do not describe it as revocation at an exact deadline. There are many provider-specific adaptations (Codex appserver/repl/browser, Slack recovery, Cloudflare code-mode opt-out); not all of this complexity is general-purpose MCP infrastructure worth adopting.

## 4. Authority, security and isolation

### Approval model: significant mismatch with Whip

- MCP requiresApproval is derived ONLY from destructiveHint===true (plugin.ts:539-548). Missing annotation, readOnlyHint false, or an undeclared mutation do not by themselves force approval.
- Plugin default resolves to require_approval when requiresApproval is true, otherwise approve; explicit owner rules override plugin default (SDK policies.ts:235-254).
- Owner rules themselves are well structured: each owner's first matching rule is considered and the more restrictive action wins across owners, preventing user policy weakening a matched org guardrail (policies.ts:183-230,257-278).
- Missing or unknown elicitation_mode defaults to model, not browser/native (hosts/mcp/browser-approval.ts:34-51).
- In model mode, resume(executionId,action,content) exposes accept/decline/cancel directly to the calling model (tool-server.ts:1593-1621). In browser mode the resume tool only asks for human approval/status; native mode omits resume and uses client elicitation (1625-1642,1594-1596).

This is not necessarily a bug relative to their chosen product contract, but it is **not a non-bypassable human approval gate** by default. Whip should retain its consent, capability inheritance/snapshot, and kernel-never-approves rules. Borrow their approval presentation and validation-before-prompt, not model-resume defaults or trust in destructiveHint. Also the UI deliberately hides the plugin-default always-approve badge (react/components/tool-detail.tsx:40-46), which makes sense only with their policy philosophy; Whip should not hide authority provenance that users need to understand.

### Process and network boundary

- QuickJS creates a fresh runtime and context per execution, applies memory/stack limits and an interrupt handler, then disposes the runtime (runtime-quickjs/index.ts:437-456,545-547). Defaults are 5-minute timeout, 64MiB heap, 1MiB stack (62-64). fetch is explicitly disabled in the guest (259-261); tools are the intended bridge.
- Dynamic workers route globalOutbound to null by default in construction (runtime-dynamic-worker/executor.ts:648; interface comments 65-70), denying direct guest fetch/connect.
- Stdio upstream MCP is a separate trust boundary: it runs configured commands via StdioClientTransport, not inside the code sandbox (stdio-connector.ts:131-153). Plugin defaults dangerouslyAllowStdioMCP false (plugin.ts:944), self-host enables it only with EXECUTOR_ALLOW_STDIO_MCP=true (apps/host-selfhost/executor.config.ts:43).
- The subprocess env correctly merges an infrastructure allowlist and explicit declared env over SDK safe defaults, NOT process.env. Source explains avoiding leakage of host auth tokens, database URL and secret-store master key (stdio-connector.ts:115-151). OS user privileges/filesystem of an authorized subprocess remain a broader boundary than the guest sandbox.

### Static finding: potentially unbounded QuickJS CPU despite advertised timeout

**Confidence: high code-supported hypothesis; NOT dynamically reproduced.**

runtime-quickjs/index.ts:138-159 makes deadlineMs null while inFlight>0 and returns false from shouldInterruptAfterDeadline then. createToolBridge calls deadline.dispatchStarted() synchronously (324), runs host invocation with runPromise, and only calls dispatchReturned in Promise success/failure callbacks (325-343). evalCode runs synchronously on the host thread (472). Thus guest code shaped as tools.someTool({}); while(true){} starts an asynchronous dispatch and enters a nonallocating loop before host promise callbacks can settle. The in-flight flag appears stuck positive while the host is blocked, disabling the only interrupt hook. Memory limit does not terminate an allocation-free loop.

Existing tests cover slow awaited dispatch, continuous compute alone, and infinite loop AFTER awaited dispatch (index.test.ts:153-198), not unawaited dispatch followed by infinite loop. Local/selfhost use in-process QuickJS, so a same-event-loop timer is not an adequate independent watchdog. Validate only in a killable external process with hard deadline; fix should preserve CPU interrupt accounting while excluding time actually waiting for host completion, rather than disable interrupts for any outstanding request. Do not reuse this timeout algorithm in Whip.

### Authentication, tenancy and secrets

- Local bearer is minted from 32 random bytes, persisted and chmod 0600; rotates only through explicit rotation (apps/local/src/auth.ts:36,49-73). Local gate uses timingSafeEqual; deliberately no Host allowlist, bearer is the boundary, CORS defaults to loopback names (serve-shared.ts:1-35). Console bootstrap token URLs are intentional (serve.ts:543); no actual token was read in this review.
- Session ownership compares accountId AND organizationId and also binds session resource (default vs toolkit), preventing session-id reuse across those capability sets (hosts/mcp/seams.ts:90-115). Untrusted org-write header is overwritten server-side (80-87).
- Host-scoped SDK binds organizationId as tenant and accountId as subject, rather than trusting an arbitrary request owner to choose a partition (core/api/server/scoped-executor.ts:310-320).
- Encrypted secret provider uses AES-256-GCM with master key supplied by host; storage addresses use opaque provider item IDs and owner partition (plugins/encrypted-secrets/index.ts:19-32,39-79,91-113). This is a provider option, not proof all local/cloud deployments use that provider. Selfhost composes it (apps/host-selfhost/executor.config.ts:48).
- Hosted HTTP guard rejects non-HTTP(S), metadata endpoints, literal/private hostnames; follows redirects manually and strips authorization/proxy-authorization/cookie cross-origin (SDK hosted-http-client.ts:138-205,221-246). Important qualification: DNS resolution checks execute only when resolveHostname is supplied (173-198); common scoped executor passes only allowLocalNetwork (scoped-executor.ts:303-308). Do not call this complete DNS-rebinding-proof SSRF protection based on this helper. Deployment network policy and custom secret header redirect handling deserve separate audit; I did not attempt network exploits.

## 5. Persistence and lifecycle: durable catalog, not durable heap

- Local schema persists integrations, owner/subject-scoped connections, OAuth clients/sessions, tools, definitions, policies, plugin storage and blobs in SQLite-backed storage (apps/local/src/db/executor-schema.ts:15-229). Unique keys include tenant/owner/subject on relevant rows (e.g. connection 52-63, tools 130-140).
- Local in-process MCP session store owns transport/server/engine maps, tracks owner, supports browser approval and has a 30-minute idle-request ceiling; an open silent SSE stream does not retain a session indefinitely (hosts/mcp/in-memory-session-store.ts:36-90).
- Execution pause/resume is live Effect fibers + Deferred responses in in-memory maps, with bounded completed-outcome/settled-ID caches (core/execution/engine.ts:51-55,534-572,669-729). shutdown interrupts sandbox fibers and waits before the storage owner closes (842-861).
- Cloud uses Durable Objects for session metadata, event replay, browser approvals and live runtime management; paused execution leases and keepalive bookkeeping prevent ordinary inactivity eviction while awaiting a decision (hosts/cloudflare/mcp/agent-session-durable-object.ts:1764-1834,2030-2169). Runtime residency has a soft per-isolate cap of 32, with non-evictable active/paused work allowed over it (session-runtime-residency.ts:91-106).
- This **does not make code cells checkpointed**. Each QuickJS call constructs/disposes a runtime, and the engine holds live fibers, not a serialized JS heap (citations above). Persistent MCP events/metadata can survive a cold session restore, but that is not transparent recovery of arbitrary in-flight guest execution after process loss/deployment. Do not equate “Durable Object” with Whip heap checkpoint semantics.

Whip's maintained frontend contract explicitly says daemon owns work and disconnect/unmount does not cancel it, with bounded state and truthful uncertain/stale outcomes (Whip docs/frontend.md:19-32); later validation guidance requires disconnect/restart and uncertain admission tests (1576-1580). Executor's console-centric workflow is a different center of gravity from Whip's long-lived agent/session/turn state. Parent should anchor detailed Whip implementation comparison in its own code trace; I did not do a second full Go subsystem review here.

## 6. Frontend UX worth studying

- Add MCP remote integration is a state machine: url -> probing -> probed -> adding/error (plugins/mcp/react/AddMcpIntegration.tsx:74-100). Probe supplies server name, tool count, auth hints, instructions and legacy negotiation; auth methods are declared on integration, actual accounts are connected later (56-62,77-88). This cleanly separates “register a server” from “choose whose credentials”.
- Tools UI has a tree/detail split, schema explorer, TypeScript/code preview, policy actions and run panel (react/pages/tools.tsx:5-10; components/tool-detail.tsx:4-38).
- Run panel offers account choice plus form/JSON input derived from schemas. Form and JSON share a single argsJson state; required top-level inputs gate Run; UI does not maintain two divergent argument models (tool-run-panel.tsx:94-111,160-204).
- Explicit Run autoApproves because the operator clicked it, and dispatches the same execute API with JSON-encoded arguments (219-224). Good distinction between a human direct action and background model action, but do not generalize this flag to untrusted model-supplied requests in Whip.
- Policy screen has owner choice, pattern, action, reorder and remove confirmation. It imports shared policy grammar/evaluation rather than inventing a client-side matcher (react/pages/policies.tsx:101-117,317-371,520-537).
- Shared UI data access is Effect AtomHttpApi over typed ExecutorApi with bearer and active org headers (react/api/client.tsx:101-117), not Whip's SDK event/store architecture. Copy UX behaviors, not the entire client state stack.

### Interactive artifacts are a real differentiated feature

Models can create/edit portable React artifacts backed by live tools, rather than only dump static code or results. Stored artifact bindings map short integration tool calls to selected account connections; resolveArtifactAction parses the allowed call code, fetches artifact through scoped ownership, resolves binding, and reformats canonical call (hosts/mcp/artifact-action.ts:44-86). Model-facing create description disallows hardcoded .user./.org. connection addressing (tool-server.ts:2099). The edit path applies exact replacements then the same validate -> smoke-render -> bind -> save pipeline, and returns current source on failure to avoid an extra recovery roundtrip (1979-1989). App tool availability is negotiated independently of ordinary model tool availability (1721-1739,2266-2318).

Borrowable later: minimal live-result views or tool-generated forms with explicit saved connection binding and ownership checks. Not a prerequisite for Whip MCP support; full arbitrary React artifact hosting adds substantial rendering, sandbox, mutation and persistence scope. I did not validate browser CSP or run a rendered artifact, so no claim of complete frontend isolation or accessibility quality.

## 7. Test evidence and what remains unvalidated

The test surface is broad and includes meaningful boundary assertions, not only snapshots:

- packages/plugins/mcp/src/sdk/owner-isolation.test.ts:41-115 creates org and user connections, finds distinct tool addresses, invokes both, and checks actual recorded HTTP Authorization is the correct owner's value, never the other's.
- runtime-quickjs/index.test.ts:153-198 checks suspended timeout around a slow awaited tool and compute timeout before/after dispatch; the dangerous concurrent shape described above is absent there.
- e2e/scenarios/mcp-execute.test.ts:22-36 uses real MCP session/service to list tools then execute return 6*7 after headless OAuth. Following scenarios explicitly target exact structured-content/UTF-8 fidelity (39 onward).
- e2e/scenarios/mcp-catalog-sync.test.ts:26-44 exercises the agent path through tool-addressed execute and ToolResult envelope; fixtures cover mutable/paginated upstream catalogs and upstream downtime (55-60).
- Additional inspected inventory includes stdio env isolation/merge/interrupt cleanup, pool key/sweep/socket release, elicitation during discovery, health missing credential, output-schema content/image tests, per-owner policies, OAuth refresh and rejected refresh, browser resume, namespace search size, artifact binding, local DB file permissions and session lifecycle tests. Existence is not evidence all currently pass.
- E2E guidance uses capability-driven targets, fresh identities, ensuring cleanup, real typed API/browser/MCP ledgers, and recordings (e2e/AGENTS.md:9-40). Root demands Effect Vitest rather than bun test (AGENTS.md:9-15).

No tests executed in this read-only comparative review. No performance benchmark, production penetration test, physical-mobile/accessibility check, or live third-party protocol compatibility claim was established.

## 8. Recommendations for Whip, prioritized

### Borrow now, small and compatible

1. **Progressive catalog UX:** short server inventory -> paginated ranked search -> detailed JSON Schema/optional generated TS -> authorized call. Preserve Whip's discover-before-call, consent and exact naming. Include explicit hasMore/nextOffset and exact namespace enumeration; do not just cap output silently.
2. **Integration vs account clarity:** server definition and auth template are different from owner-specific connection credentials. Make active account/connection visible before invocation and approval.
3. **Schema-driven inspect/run:** one canonical args value with JSON/form views, required-argument precheck, input/output schema details and explicit manual run confirmation. Reuse Whip protocol/SDK ownership instead of introducing Effect atoms or duplicate stores.
4. **Protocol regression cases:** paginate catalogs, handle list_changed mid-call, preserve upstream name/_meta, classify unknown-tool drift, bound discovery, ensure interrupted stdio cleanup and safe environment inheritance.
5. **Truthful operational failures:** distinguish unavailable integration, missing/dead credential, blocked tool, user decline, and plugin defect; redact defects with correlation IDs rather than leak URLs or provider stack traces.

### Explore after core ergonomics

6. **Observed output shapes**, bounded and owner-scoped with declared/observed provenance and privacy review. Keep original JSON Schema/raw result available; never use observed shape as permission or exhaustive validation.
7. **Portable tool-backed result views** with explicit bindings, if user demand warrants. Start with narrow existing UI primitives, not full arbitrary React artifact infrastructure.

### Do not transplant

- Model-side accept/resume as default human consent, or trusting provider destructiveHint to identify every mutating tool.
- Stateless runtime/disposable code model as replacement for Whip checkpoints and durable ownership.
- JSON-only bridge where Whip promises exact numbers and passive JSON validation.
- QuickJS in-flight timeout suspension logic without an independent process watchdog and adversarial test.
- Whole Effect/FumaDB/Cloudflare architecture or broad provider-specific compatibility hacks merely to add MCP discoverability.

**Bottom line:** Executor is a strong reference for making a large heterogeneous tool catalog understandable and usable, with serious integration lifecycle and console investment. Its authority defaults, live-fiber pause model, JSON boundary and potential CPU watchdog hole are reasons to borrow selectively rather than treat it as a stronger replacement for Whip's runtime/security substrate.


---

# Final synthesis addenda

## Backend source: f86e68b66666a9e702e8aeb6a1c35d74, bytes 24131–32651


# Synthesis addendum: exact discovery scope, attach allowlist, startup trust classification

This addendum supersedes any imprecise phrasing about "project" configuration or startup consent above. Line references below reflect the live checkout on 2026-09-11; unrelated concurrent work can shift later line numbers.

## Exact discovery precedence and scope

| Priority (high first) | Actual source | Policy / trust | Scope details |
| --- | --- | --- | --- |
| 1 | Native/materialized MCP map loaded from WHIP_HOME/config.json, default ~/.whip/config.json | Always passes external-import source gate. Origin empty/whip is trusted; preserved claude/codex Origin stays untrusted even after materialization. | config.Load reads this one global/user config. There is NO separate project-native .whip/config.json MCP merge in this backend. WHIP_HOME can explicitly redirect the user config. |
| 2 | ~/.codex/config.toml [mcp_servers.*] | mcpImport.codex Enabled/Only/Exclude; untrusted | One home file; no project .codex/config.toml or ancestor lookup in MCP discovery. |
| 3 | <session CWD>/.mcp.json | mcpImport.claude Enabled/Only/Exclude; untrusted | Exactly filepath.Join(cwd,".mcp.json"). NO ancestor walking, Git-root search, or merging of several .mcp.json files. CLI list/test/import pass os.Getwd(); daemon factory passes meta.CWD. A session intentionally created at repo root sees the root file; a subdirectory CWD does not inherit it via this function. |
| 4 | ~/.claude.json top-level mcpServers | same mcpImport.claude gate; untrusted | claudeFile has top-level MCPServers only, so Claude's project-indexed entries nested under projects in this file are not separately discovered. No ~/.claude/.mcp.json or ancestor source. |

Conflicts replace the **whole server entry**, never fields. Source gates are applied before Merge; an excluded higher-priority imported entry does not suppress an admitted lower-priority entry. Exclude wins over Only. Native map ownership prevents a blocked ghost row with the same name. The factory then applies definition.MCP.Servers to both merged and blocked maps before creating the manager.

Evidence: internal/config/config.go:289-309 (WHIP_HOME/path), :320-345 (Load only one config); internal/mcp/config.go:170-181 (whole-entry merge order), :203-243 (gates), :275-294 (exact reads), :295-327 (trust and filtering), :341-359 (merge), :424-439 (default Codex/Claude home paths); internal/mcp/claude.go:14-17 (top-level mcpServers); cmd/whip/daemon.go:105-116 and :176-195 (config load/session CWD/definition allowlist); cmd/whip/mcp.go:225-227 and :284-285 (CLI cwd).

Do not add a project-native/global-native precedence row claiming both files are loaded. Only the configurable global native file is loaded. Per-project agent definitions can choose server *names*; that is a selection layer, not another MCP definition/config map.

## Additional concrete static finding: attachment bypasses the definition server allowlist

Classification: P1/P2 depending on the intended definition isolation contract; high-confidence static inconsistency, not locally reproduced by this reviewer (model/SDK sibling independently flagged it). Initial construction reads DefinitionFor and explicitly filters discovery.Merged AND discovery.Blocked by definition.MCP.Servers when the slice is non-nil (cmd/whip/daemon.go:116-121, :178-191). Non-nil empty means no MCP servers; nil means unrestricted by this selector.

By contrast mcp.attach goes straight through internal/daemon/client_control.go:864-873 to attachMCP. internal/daemon/mcp.go:47-60 loads the entire global native map, merges all of it over the attachments, constructs and starts a manager, then swaps it in. It does not consult s.definition.MCP.Servers, does not run the normal discovery factory, and does not reapply import gates. Even an empty attachment can therefore reintroduce globally configured native servers that the definition intentionally excluded. Native entries stay trusted. The startup widening is immediate; whether a specific later call is permitted still depends on that agent's separate MCP capability/selector authority. Attached foreign entries remain untrusted for tool consent, so do not describe trust spoofing here.

**Reload path is not the same bypass:** source import configuration persists and calls reloadSession (internal/daemon/client_control.go:1275-1302). reloadSession queues a deferred replacement (:1319-1322). startPendingReload invokes prepareReplacement with the stored factory (:1832-1855), which reloads the root metadata/history and calls factory(ctx,meta,history) (:1384-1402). That factory goes through DefinitionFor and the allowlist filter above. installReplacement starts and swaps the filtered new manager (:1446-1472). Thus ordinary factory-backed reload restores/reapplies the definition allowlist; attach uniquely circumvents it. Model-change replacement also goes through prepareReplacement. A durable fix should share the allowlist operation between factory and attach, before startup, not only filter returned tool lists.

## Precise startup-consent classification

The local probes prove effects before *tool-call* admission, not that no human opt-in ever occurred. Connecting a stdio server inherently executes its program; it is normal for a deliberately configured/authorized stdio server to run during discovery. An import source being enabled can reasonably be a broad human authorization for startup, depending on the product contract. Therefore phrase finding 3 as a **coarse-grained source trust / repository-discovery policy gap**, not an unconditional tool-consent bypass or unauthenticated RCE.

Mitigations/boundaries present:
- Fresh Default() explicitly sets Claude and Codex imports false (internal/config/config.go:642-645). Merely opening a repo on a fresh installation does not enable its .mcp.json server.
- Users can explicitly enable source import via mcp.import.configure, with Only/Exclude config available; definitions can restrict MCP server names.
- Native user-configured stdio startup is inherently user-chosen code execution and is not itself a vulnerability.
- Attach is an authorized client control action; I did not test or claim an unauthenticated attach route.

Remaining surprising/exposed cases:
- Missing mcpImport in legacy/handwritten config defaults to both sources ON (internal/mcp/config.go:203-229), so no source-enable action is necessary in that compatibility mode.
- Once Claude import is globally enabled, every current/future project <CWD>/.mcp.json is admitted by source/name policy without per-project or per-definition connect consent. Exact-CWD discovery narrows exposure, but is not a repository trust prompt.
- HTTP-only definitions can run a local !command secret provider and access daemon environment variables before tool consent; the "remote" type does not imply remote-only effects. That helper needs to be included in whatever connect authorization is intended.
- The attach allowlist inconsistency above weakens a concrete existing isolation boundary even where source opt-in is otherwise considered adequate.

Recommended synthesis: keep credential leakage and managed-env failure as unconditional high-priority code bugs; describe startup execution separately as verified behavior with conditional exposure and an explicit policy decision, or fold its concrete attach-allowlist violation into the higher-confidence defect list. Do not count inherent authorized stdio launch alone as a security bug.

## Local evidence paths requested for reproduction

- /tmp/whip-mcp-backend-review/review_test.go — exact temporary test source; artifact 462b4d6e4ccf1326f54e6ab347129d27.
- /tmp/whip-mcp-backend-review/overlay.json — replaces only the compiler's view of internal/mcp/realconfig_smoke_test.go; source checkout unchanged.
- /tmp/whip-mcp-backend-review/repro.log — final complete failing-invariant output from shell job job-acaeb83a; eight expected failures, 1.849s; output artifact 1a69459697181172a24a75a874a959e2.
- /tmp/whip-mcp-backend-review/validation.log — command/result summary of passing race/targeted/coverage jobs.
- /tmp/whip-mcp-backend-review/coverage.out — Go coverage profile, 93.5% statements.

All tests used temporary HOME and offline Go module settings, fake environment/token values, in-memory SDK transports or local httptest listeners, and /tmp marker files. No additional tests were run for this addendum; added configuration/allowlist conclusions are static traces.


## Executor source: 5b66e7efd85975ec03295cb12799a221, bytes 0–6017
# Executor synthesis addendum: typed API examples, bridge role, license

Executor HEAD eaa1f3a57ffff88aede8e83783ea7ed4471aec1f; read-only, no tests/external services run.

## Exact model workflow (source vs illustration)

Actual skill instructions (packages/core/execution/src/skills.ts:35-40) contain:

~~~ts
const { items: matches } = await tools.search({ query: "<intent + key nouns>", limit: 12 });
const path = matches[0]?.path;
if (!path) return "No matching tools found.";
const details = await tools.describe.tool({ path });
// Use details.inputTypeScript / details.outputTypeScript / details.typeScriptDefinitions.
// Separately use tools.executor.coreTools.connections.list({}) for saved connections.
// Then call tools.<path>(input).
~~~

Safe illustrative two-call flow (not a recorded execution):
1. MCP execute({code: 'return await tools.search({query:"github issues",limit:12});'}) returns a page of paths/descriptions.
2. MCP execute({code: 'return await tools.describe.tool({path:"<exact path from search>"});'}) returns compact TS shape strings and definitions. Agent reads them before supplying real args in a later execute call.

Expected tool results use the actual documented union (skills.ts:47):
~~~ts
{ ok: true, data }
| { ok: false, error: { code, message, status?, details?, retryable? } }
~~~

Namespaces and connection/address names must come from discovery, not invented example methods. MCP plugin call data may itself contain upstream MCP content/structuredContent; do not assume GitHub REST response shape because integration name is GitHub.

## Three meanings of typed, clearly distinguished

1. REAL TYPESCRIPT AUTHOR API: defineExecutorConfig<TDeps,const TPlugins> preserves plugin tuple inference (SDK config.ts:33-51). Executor intersects PluginExtensions<TPlugins> (executor.ts:538). Promise SDK recursively maps Effect method signatures to Promise<R>, preserving argument/return structure and adapting branded arguments (promise-executor.ts:68-86). Browser AtomHttpApi uses the real ExecutorApi schema for request/response operations (react/api/client.tsx:101-117). These are ordinary developer-facing compile-time types plus runtime schemas where defined.
2. DYNAMIC TOOLS REMAIN DYNAMIC: SDK execute signature is (address: ToolAddress, args: unknown, options?: InvokeOptions) => Effect.Effect<unknown, ExecuteError> (executor.ts:531-535). Even the strongly typed SDK does NOT promise statically inferred parameter/output types for every dynamically discovered server/tool. Plugin tuple types extension APIs, not a compile-time catalog of connected third-party endpoints.
3. MODEL-FACING DESCRIPTIONS: inputTypeScript/outputTypeScript/typeScriptDefinitions are generated strings from JSON Schema (schema-types.ts:753-815; tool-invoker.ts:868-885). Submitted TS is stripped with Sucrase, explicitly no semantic checking (kernel/core/strip-types.ts:17-34). tools is runtime Proxy dispatch (runtime-quickjs/index.ts:230-248). These improve context and program correctness probabilistically but do not enforce TS type safety or authorize calls.

## Outward MCP bridge: actual contrast with Whip

Both can act as MCP servers to an external harness, but export fundamentally different surfaces:

- EXECUTOR: external Claude/Cursor/etc session gets execute(code), skills, mode-dependent resume, and optional/default artifact/search tools. execute orchestrates multiple upstream MCP/OpenAPI/GraphQL calls inside one isolated program. Integration aggregation/code-mode gateway (hosts/mcp/tool-server.ts:1550-1621; execution/tool-invoker.ts:307-318).
- WHIP: whip mcp serve intentionally exports restricted raw built-in tool definitions over stdio. It DOES NOT export rlm_exec. Source says callers use raw definitions (Whip internal/mcp/serve.go:14-29). It enumerates dispatcher ToolDefinitions, registers JSON Schema for each, forwards to provider.CallTool, returns text/IsError (33-56). CLI advertises serve separately from list/add/remove/import/test (Whip cmd/whip/mcp.go:22-39).

An external MCP client driving Whip today is not offered Whip's persistent coding kernel through this bridge. Replacing the restricted endpoint with Executor-style arbitrary code orchestration would be a deliberate product/security contract change, not merely improving MCP compatibility. Internal Whip rlm_exec -> mcp calls are a separate direction. Borrow discovery/schema UX inside current authority model; do not blur inbound imported-server use with outward raw-tool exposure.

## Persistence nuance explicitly supported by source

Executor documents pending artifact-originated approval as reconstructible but general code-mode pause as NOT reconstructible: SDK executor.ts:521-529 points to pending-approval.ts for the distinction. Supports no-general-heap-checkpoint conclusion while avoiding claim that absolutely no approval state survives restart. General code fibers and artifact action records have different durability.

## License

Root LICENSE:1-21 is MIT, copyright (c) 2026 Rhys Sullivan. Permits use/copy/modification/distribution/sublicensing/sale with copyright and permission notice retained in copies or substantial portions; warranty disclaimed. This licenses Executor's own code, not necessarily every transitively imported dependency. Preserve notices and check third-party requirements when copying packages. No legal restrictions inferred from marketing.

## Confirmed behavior vs static hypothesis

Confirmed FROM SOURCE: default model-side resume; destructiveHint-only default approval; syntactic TS stripping; runtime JSON bridge; fresh QuickJS runtimes; paginated lexical discovery; owner-scoped policies; raw-tool-only Whip outward MCP server.

NOT REPRODUCED: unawaited guest dispatch followed by while(true) may disable QuickJS CPU interruption because any in-flight dispatch sets deadline null. Keep this as a separate security-review caveat, not an experimentally verified exploit or primary product conclusion. Parent requested no reproduction; none attempted.
