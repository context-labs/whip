# Feature map

whip is a local recursive coding-agent runtime. This page describes the
current architecture; package and test names are included where they make a
contract easier to locate.

## Recursive agent runtime

- One `AgentSession` implementation is used for roots and descendants
  (`internal/daemon/recursive_runtime.go`).
- Agents are definitions, not daemon wiring. `internal/agentdef` registers
  `coding` and `junior-developer`; each names its persona and rules,
  project-file, skill, and standing-instruction discovery, host modules,
  capabilities, model and compaction defaults, and surface flags. A session
  records its definition id at creation (`session.create.definition`,
  `whip run -agent`, `whip --agent`; default `coding`), restores it on restart,
  copies it on fork, and cannot resume under a different one. The prompt
  composer derives the module catalog from the definition, the kernel installs
  only its modules, the host refuses calls to any other module, and a new
  root's grants cover only its capabilities. A child's effective definition is
  its parent's narrowed by the spawn arguments. Definitions without the goal
  loop reject goal commands (`TestDefinitionPromptGolden`,
  `TestJuniorDeveloperSessionIsConstrained`, `TestChildDefinitionNarrowsParentDefinition`).
- Definitions are also authored outside the binary. `definitions.register`
  stores a JSON document under its content revision (SHA-256 of the canonical
  encoding); `definitions.get` and `definitions.list` read built-ins and
  registrations together; `session.create.definition` resolves a registered id
  to its latest revision and pins it, so a daemon that no longer knows the
  revision refuses the session rather than running a different agent. The
  TypeScript SDK authors them with `defineAgent` and `tool`
  (`@whip/sdk/agents`), and the JuniorDeveloper fixture is written both ways:
  `internal/agentdef/testdata/junior-developer.json` must equal the Go built-in
  and the SDK output (`TestTypeScriptJuniorDeveloperMatchesBuiltIn`,
  `examples/agents`).
- A definition's `tools` are custom operations served outside the daemon. The
  kernel installs them as the reserved `tools` module (`tools.<name>`, keyword
  arguments), the runtime guide catalogs them from their JSON Schema, and the
  agent holds a `tools` grant beside files, shell, and MCP. A call is admitted
  through the capability ledger, validated against the schema, and handed to
  the executor bound for the definition revision (`client.agents.serve`); the
  default timeout is 5 minutes and the ceiling 15. No executor fails the call
  after a bounded wait, a deadline or cancelled turn settles it with
  `tool.cancel`, and a disconnected executor's calls fail and are never
  replayed (`TestCustomToolInvocationRoundTrip`, `TestCustomToolFailureSemantics`).
- `agents.spawn(definition="name")` selects a named child of the parent's
  definition; its instructions, modules, capabilities, tools, budgets, and
  report mode apply as defaults and explicit arguments still only narrow.
  `tools=[...]` narrows custom tools like `capabilities` narrows authority; the
  narrowing is a grant and survives restart (`TestNamedChildDefinitionsApplyAndRestore`,
  `TestCustomToolChildNarrowingIsEnforced`).
- A definition's `hooks` let its executor observe and gate what sessions do.
  `before_tool` runs before every host operation a cell calls (narrowable to
  named operations) and may deny with a reason or rewrite the arguments, which
  then take the same validated path fresh arguments do; `before_spawn` runs
  after a spawn request is parsed and resolved and may deny or rewrite the
  request, which is resolved again so it cannot widen; `turn_start` contributes
  ephemeral context to each turn and never gates. Every reply field is
  optional and an empty reply allows unchanged. A required hook that goes
  unanswered (no executor, timeout, disconnect) or whose handler throws denies
  the operation; an `optional` hook proceeds with a notice. Hooks only
  narrow: they never grant authority the ledger denies and never skip the
  user's permission mode. Deny, rewrite, and skip emit `stream.hook.decision`,
  and rewrites and skips add a bounded notice to the turn's next provider
  request so the model learns what ran (`TestBeforeToolGatesEveryHostOperation`,
  `TestBeforeSpawnSeesResolvedChildAndCannotWiden`, `TestTurnStartContributesEphemeralContext`).
- JuniorDeveloper is a deliberately limited agent for exercising those seams:
  seven modules (`context`, `files`, `shell`, `state`, `artifacts`,
  `permissions`, `user`), the `read`, `write`, and `shell` capabilities, no
  skill catalog, and no goal loop. It cannot delegate, reach MCP, or drive a
  browser or the desktop.
- `run.configure` state (system override, turn cap, cache key) belongs to the
  session and is re-applied when a model change or reload replaces the runtime
  (`TestRunConfigurationSurvivesModelReplacement`).
- Every provider request exposes exactly `rlm_exec`; MCP discovery cannot
  widen the model-facing catalog (`internal/agent/rlm_test.go`).
- `agents.spawn` creates a retained asynchronous child with the same interface
  as its parent. `inspect`, `list`, `stop`, and `delete` provide lifecycle
  control.
- Omitted capabilities inherit; explicit capabilities narrow. Budgets roll up
  through ancestry and enforce tokens, cost, elapsed time, depth, active
  children, and concurrent turns.
- Worker capacity is reserved before child persistence. Rejection leaves no
  child record (`TestRecursiveSpawnRejectsCapacityBeforeAdmission`).
- The default recursion limit is root → child → grandchild.
- Restart reconstructs retained nodes, transcripts, authority, route, and
  kernels (`TestRecursiveRuntimeRestoresRetainedAgentAndTranscript`).

## Selectable execution engines

Each root session chooses `starlark` (the default) or `quickjs` (JavaScript).
The choice is immutable and inherited by every descendant, preserved by forks,
and asserted on an explicit resume. `rlm.defaultEngine` affects future sessions.
CLI creation uses `--rlm-engine`; web and mobile creation use the daemon's
advertised engine list. Retry journals retain the original choice.

`internal/rlm` owns both trusted bundled engines, private worker protocol 2,
and one daemon-hosted module registry. QuickJS runs bundled WASM in wazero
inside the existing stripped-environment worker subprocess.

- Each kernel serializes its cells so small globals persist within a worker.
- Cells are bounded by engine compute, host requests, memory, output bytes,
  and frame bytes. JavaScript supports top-level await and bounded asynchronous
  host calls; `rlm.maxConcurrentHostCalls` defaults to 16 (Starlark stays serial).
- The worker has no ambient daemon/provider credentials or host I/O API.
- Completed cells save engine-qualified checkpoints. Starlark retains its
  tagged partial codec; QuickJS restores the settled heap, including closures,
  lexical bindings, classes, and cycles. Active operations are not checkpointed.
- Large cell results and host outputs become content handles.
- Result version 2 carries engine/language, explicit value presence, and
  engine-specific metrics. SDK, web, mobile, and TUI accept legacy Starlark
  history; JavaScript completion does not depend on Starlark step counts.
- Public protocol major 6 requires compatible clients and advertises the
  `execution_engines` capability. The language picker is a creation control.

Implementation and validation: `internal/rlm/quickjs_test.go`,
`internal/session/execution_engine_test.go`,
`internal/daemon/execution_engine_test.go`, SDK execution tests, web creation
and REPL tests, mobile creation tests, and `internal/tui/repl_result_test.go`.
See [runtime semantics](rlm-runtime.md#execution-language-and-checkpoints) and
the [benchmark workflow](../evals/runtime-ab/README.md).

Available modules are summarized in [tools.md](tools.md).

## Focused context

- Full history and oversized inputs are stored behind content handles.
- A request carries at most four recent user/assistant exchanges and one
  bounded summary.
- `context.inspect/search/read` returns source metadata and byte spans.
- Proactive and reactive compaction protect the provider context window.
- Large values are immutable, content-addressed, and separately authorized.

The deterministic evaluation expands a corpus above 500 KB and proves the
answer can be found through bounded reads without copying the corpus into the
root prompt (`evals/rlm`).

## Messages and collaboration

- A child’s ordinary assistant response is local to its transcript.
- `messages.send(delivery="steer"|"queued"|"next_turn")` stores a durable
  body; readiness is derived from pending mail, not a separate wake row.
- `messages.list/read/complete/defer` make body admission and lifecycle
  (`pending → delivered → done`) explicit; delivery is committed with the turn.
- Bursts retain every message and derive one ready signal
  (`TestMailboxBurstDerivesOneReadySignal`); a turn receives one bounded digest.
- Evidence handles can be granted to a direct relative with the message.
- Child turn results arrive as `agent.completed|failed|cancelled` messages
  with a short preview and evidence handle; they do not inject child output.
- Private state is agent-scoped; blackboard state is shared and supports
  append, compare-and-swap, history, and subscriptions.

## MCP

- Stdio and streamable HTTP servers are discovered from project, Codex, and
  WHIP configuration (`internal/mcp`).
- Root and child kernels use `mcp.list_servers/list_tools/call`.
- Provider tool catalogs remain stable at one tool while MCP servers change.
- Connections have startup/call deadlines, per-server serialization,
  reconnect generation guards, and bounded structured/media flattening.
- Remote HTTP requests remain tied to the transport lifetime through SSE body
  reads, including startup before a session is published. Retirement cancels
  stalled streams and permits a one-second best-effort session DELETE. Healthy
  notification streams survive startup completion and catalog refresh
  (`internal/mcp/http_transport.go`, `http_transport_test.go`).
- `whip mcp serve` is a daemon protocol tool host, not a model agent.
- Secrets stay references: `$VAR`/`${VAR}`/`!cmd` in env and headers resolve
  at connect time (`config.ResolveSecret`/`ResolveEnvMap`/`ExpandTemplate`)
  inside the daemon's environment — run `whip daemon restart` after exporting
  new vars. Codex `bearer_token_env_var` imports as `Authorization: Bearer
  $VAR`; `http_headers`/`env_http_headers` import as headers.

## Built-in capabilities

- `files`: list/search/read/write/patch with canonical path authorization and
  same-path mutation ordering.
- `shell`: managed process groups, bounded output, interactive PTY support,
  and workspace-wide effect authority.
- `browser`: live/dedicated/headless/extension backends behind daemon policy.
- `computer`: macOS accessibility and screenshots with per-app policy.
- LSP diagnostics can be attached after file changes.
- Permission requests are durable and any connected client can approve or deny.
  The daemon revalidates authority before the exact operation resumes; there is
  no client pairing, signing key or first-run approver enrollment prompt.
- Ask for approval / Full Access is saved per root session and inherited by its
  child agents. Changing it while idle persists across daemon restarts, client
  reconnects and session switches. Full Access allows filesystem operations and
  working directories outside the project under the execution host's OS user.
  Explicit child path and operation limits still apply. Ask retains the original
  project file boundary; changing cwd never grants access. New sessions, including
  forks, default to Ask; upgrades preserve an existing saved choice. Explicit terminal launch flags update the initial
  session; ACP loading preserves the saved mode and follows other clients'
  changes. Remembered allow rules and headless/deny execution policies remain
  separate. Implementation: `internal/session/permission_mode.go`, daemon
  startup/control, ACP bridge and TUI client. Coverage: session and daemon
  `permission_mode_test.go`, `migrations_test.go`, ACP bridge and TUI client tests.
  Schema 14 tags known root grants as session-scoped. Legacy child grants keep
  their recorded path limits because their original scope intent is ambiguous;
  newly created default children inherit the session policy. Shell commands
  retain ordinary OS-user authority and Ask is not a filesystem sandbox.

## Provider loop and models

- OpenAI-compatible streaming with retry events and usage accounting.
- Model-to-provider routing, live catalog discovery, context/output limits,
  reasoning effort, vision flags, sampling parameters, and pricing.
- `models.call` and `models.batch` provide stateless analysis without creating
  durable child identities; batch results retain input order.
- Prompt-cache keys are stable per retained session: the daemon stamps
  `prompt_cache_key` with the session id. Headless `whip run -cache-key <key>`
  pins a stable key (e.g. `repo/reviewer`) so one-off runs reuse the cached
  system prefix.

### Models.dev metadata and named local keys

Ten supported API presets use a checked-in Models.dev subset with upstream
provenance and MIT attribution. Whip policy controls supported protocols,
endpoints, recommendation order and defaults. Live lists own membership; exact
metadata enrichment preserves explicit false/zero/empty fields and timestamps.
`task models:update` refreshes metadata; `task models:check` validates it offline
and verifies the generated desktop shell-key names.

`providerKeySources` declares env files, mapped raw-key files and directories of
exact variable filenames. Bounded parsing never executes shell content. Named
keys resolve consistently through inventory, connection validation and actual
model clients. Daemon startup and `provider.discover` persist only missing
`apiKeyEnv` references using revision-safe, no-op-aware writes. Sources-only config,
existing routes, disabled providers, defaults and sessions survive discovery.
File keys reread on discovery/new clients; existing sessions require reload after
rotation. OpenCode credential/config/database import and desktop probes are removed.

Code: `cmd/modelgen`, `internal/config/modelsdev`,
`internal/config/{modelsdev,provider_credentials,provider_models,revision}.go`,
`internal/daemon/provider_{discovery,list,configuration,model,service}.go`,
`internal/tui/setup.go`, `packages/app/src/provider-setup.tsx`,
`apps/desktop/src/provider-environment.ts`.
Tests: importer/presence/pricing tests in `cmd/modelgen` and
`internal/config/modelsdev`; `internal/config/provider_credentials_test.go`;
`TestDiscoveredFileKeyPersistsReferenceAndReachesInference`,
`TestProviderDiscoveryRPCUsesHostSourcesAndPreservesOptOut`,
`TestDiscoveredOpenRouterKeyStillRequiresAuthentication`; CLI file-key checks in
`TestClientEntryPathsSendAssembledPromptToProvider` and
`TestAuthOpenRouterEnvironmentModeUsesNamedFileWithoutPrompt`; SDK/app source and
host-refresh tests; desktop generated-name and shell-recovery tests. See
[implementation and validation](../.ai-docs/plans/models-dev-discovery/README.md).

### Provider connections and environment discovery

Settings renders a host-owned provider inventory with bundled logos, credential
source labels and per-provider connect/manage dialogs. Existing API/account
flows are reused. Known-preset environment and declared key-file discovery save
missing provider references during daemon startup and setup/Refresh; regular
inventory reads remain read-only. Secret values stay in their original sources.
Saved overrides win; revision-checked disable/disconnect preserves aliases and
defaults. Disabled routes stop at model admission across root, child, helper and
compaction requests. Model menus exclude unavailable routes; catalog publication
rejects stale route/account responses. Desktop recovers only the supported local
shell keys, bounded and without forwarding them to remote hosts.

Code: `internal/config/providers.go`, `internal/daemon/provider_{list,disconnect,model}.go`,
`internal/daemon/budget.go`, `packages/app/src/settings/provider-connections.tsx`,
`packages/app/src/settings/provider-login.tsx`, and `apps/desktop/src/runtime.ts`.
Tests: `internal/config/providers_test.go`,
`internal/daemon/provider_connections_test.go`, `packages/sdk/test/services.test.ts`,
`packages/app/test/provider-connections.test.tsx`, `packages/app/test/model-selection.test.tsx`,
`apps/desktop/tests/provider-environment.test.ts`, and the production
`apps/web/scripts/provider-connections.mjs` workflow. See the
[implementation plan](../.ai-docs/plans/provider-connections/README.md) for scope
and validation results.

### Provider onboarding and the first message

TUI startup and the web/desktop welcome composer use host-owned route readiness.
A usable selection skips provider setup; otherwise the same reusable chooser
offers detected connections, masked key entry and account login. Inference.net
leads the Popular group with one quiet Recommended label; search relevance and
saved selections take priority. Detected connections keep their positions.
An explicit TUI selection or successful connection of Inference.net, OpenRouter,
or OpenAI automatically chooses `kimi-k3-fast` (high), `z-ai/glm-5.3` (max, its
provider default), or `gpt-6-astra` (medium) and returns to the composer. Fresh
onboarding saves model, provider and effort together with a revision check.
Existing sessions persist the selection locally; saved startup choices remain
unchanged. Missing models or incompatible destinations retain the model picker.
Other provider flows offer model choices directly. Enter on a model applies it
and returns to the composer without a second confirmation. First-time onboarding
saves the choice for new sessions; an existing session keeps the change local.
Explicit manual models use the same direct selection after their configuration
is saved. Failed saves retain the model picker with refresh/retry guidance.
Singleton Inference.net team/project choices advance on the host; genuine choices
remain explicit.

TUI startup opens one normal Bubble Tea interface and one reconnecting client
without a folder-trust prompt. Tool approvals follow the session's saved permission
level. Before a session exists, host queries power a floating
provider dialog over the normal composer. Esc returns to drafting; `/connect`,
`/auth`, Model commands or submitting an unconfigured draft reopen the dialog.
Provider/model/workspace/project lists use themed selection and height-bounded
windows. Legacy inline-key commands open the same masked confirmation. The old
standalone setup program, duplicate composer and separate auth prompts are gone.

Session creation waits for a usable model/provider selection and retains one
create identity across reconnects. Permission setup completes before execution.
An onboarding draft requires explicit Enter after connection; late login replies,
poll ticks or earlier launch prompts cannot submit or clear an edited draft.
Preparation errors allow Enter to retry; a terminal connection failure displays
its cause and instructions to quit/relaunch. Fresh installations
leave external Claude/Codex MCP imports off and no longer use `setup.done`.

Welcome retains the draft during setup, requires a project folder, and creates
the session with the chosen tool permission mode before sending. Its bounded
host-scoped journal retains original create/submit identities across reloads;
uncertain delivery is checked before explicit retry. A later draft edit is never
cleared by an earlier acceptance. Native **Set up this Mac** composes verified
backend installation and the existing connection owner in one action. Existing
installations, advanced diagnostics and remote hosts retain their policies.

Code: `internal/daemon/provider_selection.go`, `internal/daemon/provider_service.go`,
`internal/tui/{setup,startup}.go`, `internal/daemon/root_client.go`,
`internal/session/command.go`,
`packages/app/src/{provider-setup,welcome,welcome-submission,runtime}.ts*`,
`packages/app/src/host-dialog.tsx`, and `apps/desktop/src/runtime.ts`.
Tests: `internal/daemon/{provider_selection,root_client}_test.go`,
`internal/tui/{setup,startup,client,cursor}_test.go`,
`internal/session/command_test.go`, `cmd/whip/daemon_test.go`,
`packages/app/test/{provider-connections,welcome-submission,sidebar-creation,runtime,local-runtime}.test.ts*`,
`apps/desktop/tests/runtime.test.ts`, and `apps/desktop/scripts/onboarding-smoke.mjs`.
See the [implementation and acceptance record](../.ai-docs/plans/provider-onboarding/README.md)
and [integrated TUI completion](../.ai-docs/plans/provider-onboarding/TUI-INTEGRATION.md).

`task onboarding:docker` builds the current checkout into a disposable Linux TUI
and embedded web server at `localhost:4000`. Both clients share an empty runtime
home; quitting removes test state while retaining build caches. The launcher also
supports dirty linked worktrees without copying Git internals. See
[fresh onboarding in Docker](../README.md#test-fresh-onboarding-in-docker).
Code: `scripts/onboarding-docker.mjs`, `scripts/docker/onboarding.Dockerfile`,
`scripts/docker/onboarding-entrypoint.sh`, and `scripts/renderer-artifact.mjs`.
Tests: `scripts/onboarding-docker.test.mjs` and `scripts/renderer-artifact.test.mjs`.
Linux worker startup also accounts for Go's existing virtual memory reservations
before setting its address-space ceiling, while retaining the resident RAM limit.
Code: `internal/rlm/memory_linux.go`; regression:
`TestMemoryLimitPreservesRuntimeReservations` in `internal/rlm/memory_linux_test.go`.

### Known provider picker and local credentials

The TUI uses compact provider, authentication-method and masked key dialogs.
Popular contains Inference.net, OpenRouter and an OpenAI family row; seven more
known compatible providers follow alphabetically. Connection checkmarks stay in
stable positions, search has no prompt prefix, and refresh preserves selection.
OpenAI API billing and ChatGPT subscription keep distinct stored route IDs.
Other/custom setup uses small prompts with optional manual models and limits.

The host's bundled preset registry supplies URLs, environment aliases, category,
family and key-page metadata, augmented from a pinned Models.dev subset. Effective
connections reuse named environment/file keys without copying secrets or writing
configuration during inventory. Setup/discovery persists missing references. Explicit overrides and disabled routes
remain authoritative. Desktop recovers only bounded local shell values and routing
guards; remote hosts resolve their own credentials. OpenRouter discovery checks
the authenticated `/key` endpoint before its public model list, rejecting bad
credentials in the API-key dialog before saving. DeepInfra's public catalog and
bundled discovery remain unverified in host metadata and management;
TUI provider/model pickers omit informational discovery footers. Observed
authentication failures still reject a submitted key.
All ten API presets use live model membership, accepting new IDs with sparse
metadata and excluding explicit incompatibilities. Bundled models only fill
missing metadata or provide offline candidates. Old allowlist caches refresh on
next use, and failed refreshes preserve the last catalog. Stream/tool fixtures
cover the ten API presets. Canonical OpenAI Astra API calls use the shared Responses codec, including
output caps, accounting and credential-scoped reasoning continuation; other custom
OpenAI-compatible endpoints retain Chat Completions. No paid live-provider
acceptance is implied.

Code: `internal/tui/setup_picker.go`, `internal/tui/ui/list.go`,
`internal/config/{providers,provider_credentials,provider_models}.go`,
`internal/daemon/provider_{list,model,service}.go`,
`internal/llm/provider_compatibility.go`, `apps/desktop/src/runtime.ts`.
Tests: `internal/tui/setup_picker_test.go`, credential/preset model tests under
`internal/config`, preset discovery/key tests under `internal/daemon`,
`internal/llm/provider_compatibility_test.go`, and desktop provider environment tests.
See [picker implementation and evidence](../.ai-docs/plans/tui-provider-configuration/PICKER-REDESIGN.md).
One-step defaults: `internal/tui/setup_default{,_test}.go`,
`internal/daemon/provider_default_test.go`, and
`internal/llm/openai_responses{,_test}.go`. See
[verified selectors and validation](../.ai-docs/plans/tui-provider-configuration/AUTO-MODELS.md).

### File-backed custom provider configuration

The TUI's `/connect` dialog includes **Other…** and **ctrl+e**
management. Users can configure an OpenAI-compatible Chat Completions endpoint
with a masked API key, a host environment-variable reference, or explicit no
authentication. Discovery populates the model picker; manual models and explicit
unverified saves support endpoints without `/models`. **ctrl+d** controls whether
the selected pair becomes the default for new sessions.

The execution host writes definitions, credentials and manual model aliases to
its existing configuration file through revision-checked operations. Reads are
redacted, refreshing conflicts preserves edited fields, and changing an endpoint
requires an explicit credential decision. Current sessions can explicitly reload
changed connections; references block removal without rewriting model routes or
history. Web/desktop inventory sees the same connections. No provider database or
web custom-provider form is introduced.

Code: `internal/tui/setup_provider.go`, `internal/tui/setup_host.go`,
`internal/daemon/provider_configuration.go`, `internal/config/providers.go`,
`internal/llm/openai.go`, `internal/protocol/provider_types.go`, and
`packages/sdk/src/services.ts`.
Tests: `internal/tui/setup_provider_test.go`,
`internal/daemon/provider_configuration_test.go`,
`internal/config/provider_auth_test.go`, `internal/llm/openai_noauth_test.go`,
`cmd/whip/acp_test.go`, and `packages/sdk/test/services.test.ts`.
See [configuration instructions](models-providers.md#supported-provider-types-and-custom-endpoints)
and [implementation and acceptance](../.ai-docs/plans/tui-provider-configuration/README.md).

### ChatGPT subscription provider

`openai-codex` adds host-owned device login, restart-safe rotating credentials,
account-scoped model discovery, and a Responses adapter to the existing model
client. Settings, CLI and TUI share daemon login/status/logout; API-key routes
remain separate. Public history excludes opaque model continuation, while the
durable transcript retains it through restart, fork and rewind. Unknown cost,
natural output reservations, and explicit-cap rejection preserve accounting.
Completed stream items survive an empty terminal output array. Model menus
include discovered models and select explicit model/provider pairs.
See [models and providers](models-providers.md#openai-chatgpt-subscription) for
setup and limits; live acceptance is tracked in the
[implementation plan](../.ai-docs/plans/openai-subscriptions/README.md).

Code: `internal/openaiauth`, `internal/llm/{subscription,responses}.go`,
`internal/daemon/provider_{openai,model}.go`, `cmd/whip/auth_openai.go`,
`internal/tui/auth_cmd.go`, `packages/app/src/settings/providers.tsx`, and
`packages/app/src/model-options.ts`.
Tests: `internal/openaiauth/auth_test.go`, `internal/llm/{subscription,responses}_test.go`,
`internal/daemon/provider_openai_test.go`, `internal/session/continuation_test.go`,
and `packages/app/test/{providers,model-selection}.test.tsx` cover rotation races,
cross-transport login recovery, secret isolation, stream completion, budgeting,
model/provider selection and client state. Live Pro-account acceptance also
verified tools, helpers, a child, images, title/compaction and restart recovery.

## Daemon and clients

- The daemon is the only runtime/store owner.
- TUI, `whip run`, sessions commands, ACP, and MCP stdio are protocol clients.
- WHIP v5 is one typed JSON-RPC 2.0 contract over Unix sockets and optional
  WebSockets; compatible builds attach without replacing the daemon. The operation
  and event registry generates TypeScript declarations and Ajv validators.
- Command submission returns committed acceptance. Stable client/command IDs
  deduplicate retries; changed payloads conflict. Status exposes queued, running,
  waiting and terminal outcomes. Disconnecting does not cancel accepted execution.
- Dynamic subscriptions have explicit unsubscribe, durable replay and a 16-root
  connection cap. Consistent bounded snapshots, history/collection revisions and
  replaced-subscription filtering let reconnects converge.
- Recent transcripts bootstrap quickly; older root/child messages are pageable.
  Large values use granted content references. HTTP transfers reuse the content
  store and limits. Raw human transcript inspection never changes model context.
- Omitted session model/provider routes resolve from host defaults after command
  deduplication. Provider readiness determines setup even when daemon startup has
  already created configuration (`session_defaults_test.go`, `setup_test.go`).
- TUI startup, initial host queries, the first prompt and automatic title delivery
  run against both transports in release acceptance (`client_integration_test.go`).
- Provider setup/login, versioned configuration updates and completion execute on
  the daemon host. TUI themes/keybindings remain local. Secret credentials and
  ephemeral terminal input are excluded from command journals.
- Implementation: `internal/protocol`, `internal/daemon/{server,subscription,
  transport,network,provider_service,completion}.go`, `internal/session`, and
  `packages/protocol`. Coverage: `v2_acceptance_test.go`, `runtime_parity_test.go`,
  `transport_test.go`, `client_admission_test.go`, provider/config tests and the
  generated contract/browser interoperability checks. See [protocol-v2.md](protocol-v2.md).
- Slow clients lose their bounded connection instead of blocking a root.
- Workspace terminals: `internal/terminal` owns one PTY per terminal tab with a
  1 MiB replay ring and one live attachment whose bounded queue stalls the shell
  behind a slow receiver instead of overflowing the connection; `terminal.*`
  RPCs and `terminal.output/exited/detached` notifications live in
  `internal/daemon/terminal_rpc.go`, with the network gate on
  `WHIPCODE_NETWORK_TERMINALS`. Coverage: `internal/terminal/*_test.go` (ring,
  replay, resize, hangup, backpressure, limits, shutdown under `-race`) and
  `internal/daemon/terminal_rpc_test.go` (round trip, exit ordering, detach on
  disconnect, takeover, gating, validation).
- Schedules and blackboard subscriptions create durable wakeups.
- Process shutdown is root-owned and waits for supervised workers.

## TypeScript client SDK

- Private Node 24 ESM workspace: `@whip/protocol` generates typed RPC/runtime
  maps and standalone CSP-safe validators; `@whip/sdk` attaches over native
  WebSockets or Node Unix sockets without owning daemon processes.
- Stable session handles, committed command acceptance, typed terminal outcomes,
  metadata-only application recovery storage, explicit identical-request retries,
  runtime identity checks and targeted cancellation share one connection engine.
- Optional `/state` views reconstruct bounded snapshots/history/live output,
  questions, permissions and recursive agent state. History revisions prevent
  mixing rewind epochs; catalog polling is observed and never opens every root.
  Live and snapshot presentation share delta/cumulative update rules and stable
  row keys; interleaved tool updates do not create duplicate transcript rows.
- `/react` hooks subscribe to those immutable views. The working example owns
  drafts and storage; no React dependency is loaded by core/state consumers.
  Consecutive identical internal mailbox digests share one expandable row with
  a delivery count; raw transcript entries and authored messages remain intact.
- Content reads verify root/agent grants, size and SHA-256. Permission helpers
  send typed decisions from trusted clients without a signer; the example always
  exposes Allow once and Deny. Provider configuration and terminal input are
  ephemeral and never enter SDK recovery storage.
- `/agents` authors agent definitions: `defineAgent` builds the canonical
  document, `tool` declares a custom tool with its JSON Schema and handler, and
  `client.agents.register/get/list/serve` register definitions and run an
  executor that serves tool invocations for one definition revision, re-binding
  after reconnect. Handlers receive the invocation id, root, agent, turn,
  deadline, an `AbortSignal`, and a progress reporter. `hooks.beforeTool`,
  `beforeSpawn`, and `turnStart` are served by the same executor; returning
  nothing allows unchanged. `examples/agents/incident-commander.ts` uses every
  primitive at once, and `incident-commander.acceptance.mjs` drives it through
  a live daemon (the SDK fixture with `WHIP_SDK_AGENTS_FIXTURE=1` runs the
  recursive runtime behind a scripted model): registration, a session pinned
  to the revision with the definition's model defaults, custom tool calls with
  progress and handle-backed results, hook denials and rewrites, a spawn
  redirected to a named child that runs its own narrowed tools, and the
  fail-fast behavior of a closed executor (`npm run acceptance -w @whip/agents-example`).
- Implementation: `packages/sdk`, `examples/client`. Coverage: SDK TypeScript
  unit tests, `daemon.acceptance.mjs`, isolated `TestV2SDKBridge`, actual SDK
  strict-CSP Chromium/Firefox/Safari and React StrictMode smoke tests, plus packed
  package installation. See [SDK usage](../packages/sdk/README.md).

## macOS desktop application

The Electron host packages the same production renderer as the web application.
The [desktop guide](desktop.md) documents local builds, canonical runtime installation,
signing and release configuration. [Desktop acceptance](../.ai-docs/plans/desktop-app/progress.md)
is still open for notarized distribution, actual updates and manual device checks.

Local connections use one selected installed `whipcode` executable, with
`~/.whipcode` as the default home in both normal desktop channels. The packaged
backend is an installation payload, not a privately retained daemon. The initial
canonical path on this Mac is `/usr/local/bin/whipcode`. The historical-state
cleanup and signed/notarized local installation are complete. Actual desktop
diagnostics, CLI/desktop startup, shared WebSocket sessions, reconnects and a live
provider message passed; see the
[canonical installation record](../.ai-docs/plans/canonical-whipcode/README.md).

| Behavior | Implementation | Validation |
| --- | --- | --- |
| One bootstrap and UI in browser and desktop, with independent SDK clients per host; native effects behind an adapter | `apps/web/src/{main,bootstrap}.tsx`, `apps/web/src/platform/`, `packages/app/src/{platform,desktop-bridge}.ts` | App architecture/bootstrap/desktop-adapter tests; packed app/UI consumer and production renderer native-import guard |
| Exact shared renderer in Go embed and Electron ASAR, verified native companions and full DMG/ZIP contents | `scripts/{renderer-artifact,pack-web}.mjs`, `apps/desktop/scripts/{build,package,verify,distribution}.mjs`, `apps/desktop/forge.config.cjs` | Renderer/provenance/distribution tests, actual signed archive extraction/mount and signature/fuse checks |
| One-command local source update of the signed app and shared backend; verify before quit, retain previous binaries, gracefully restart and check the running build | `scripts/update-local.mjs`, `Taskfile.yaml` (`update:local`), `cmd/whip/desktop_runtime_sync.go`; [usage](../README.md#update-your-local-installation-from-source) | `scripts/update-local.test.mjs` (real filesystem staging and failure preservation), `TestDesktopCompiledUpdate` (real backend handoff) |
| Stable local/URL/SSH profiles, safe migration, explicit replacement identity and stale connection disposal | `packages/app/src/{connections,hosts,runtime}.ts`, `packages/app/src/{host-dialog,connection-dialog}.tsx`, `packages/sdk/src/client.ts` | App connections/runtime/replacement-runtime/session-navigator tests; SDK changed-runtime regression |
| Canonical installed whipcode selection, compatible attach-before-start, owner-proven stale socket recovery, no daemon shutdown on GUI exit or backend replacement during an app update | `apps/desktop/src/{main,runtime,transport}.ts`, `cmd/whip/daemon_manage.go`, `cmd/whip/desktop_runtime.go` | `apps/desktop/tests/runtime.test.ts`: saved-path precedence, missing-path refusal, compatible reuse, explicit restart, port conflicts and bounded/cancelled processes; Go owner/socket tests |
| Verified whipcode payload with source/build/distribution provenance and the matching embedded Swift helper; explicit installation refuses a different existing executable | `apps/desktop/scripts/{build,verify,distribution}.mjs`, `apps/desktop/src/runtime.ts`, `cmd/whip/desktop_runtime.go` | Native runtime manifest/integrity, explicit-install, concurrent-publication and cancelled-copy tests; distribution checks; signed/notarized installed artifact and matching canonical executable verified |
| This Mac setup before daemon availability, read-only Test Connection, native executable choice, explicit installation/restart and expandable path/build diagnostics | `packages/app/src/{platform,desktop-bridge}.ts`, `packages/app/src/host-dialog.tsx`, `apps/web/src/platform/desktop.ts`, `apps/desktop/src/{main,preload,runtime}.ts` | `packages/app/test/{local-runtime,desktop-adapter,architecture}.test.ts*`; `TestDaemonStatusDoesNotInitializeHome`, `TestDaemonStatusPreservesExistingRuntime`; Chromium missing-daemon UI check |
| System SSH configuration, private Unix forwarding, in-app prompts and owned helper cleanup on GUI death | `apps/desktop/src/ssh.ts`, `cmd/whip/desktop_{ssh,askpass,wait_darwin,wait_linux}.go`, `packages/app/src/host-prompts.tsx` | Real isolated sshd native tests, Go race/integration process-group and askpass tests, shared prompt stale/cancel tests |
| Native save/copy/folder/link effects, opt-in attention notifications, restored tabs and draft-aware close | `apps/desktop/src/{main,native,links}.ts`, `packages/app/src/{attention-notifications,session-tab-routing,session-tab-strip,settings}.ts*` | Native save/disposal tests, app attention/close-tab/settings tests, signed Finder launch and tab/draft checks |
| Deferred updater, one-action managed backend synchronization, attested candidate staging and conditional feed promotion | `apps/desktop/src/{updates,runtime}.ts`, `cmd/whip/desktop_runtime_sync.go`, `internal/daemon/maintenance_unix.go`, `apps/desktop/scripts/{ci-signing,publish,publish-github,release-candidate,notices}.mjs`, `.github/workflows/release-desktop.yml` | Updater release-name/approval/retry tests, `TestDesktopCompiledUpdate` (real two-build handoff and session/config preservation), maintenance-lock race tests, candidate/publisher identity and conditional-write tests; actual Squirrel N→N+1 remains a release gate ([runbook](desktop-releases.md)) |

## React web application

The private web application is implemented as a thin consumer of the same SDK.
The release gate remains open for the manual device/accessibility checks recorded
in the [web plan](../.ai-docs/plans/web-app/README.md); this table maps implemented
behavior to its owning code and repeatable validation.

| Behavior | Implementation | Validation |
| --- | --- | --- |
| Attach to existing hosts, discover each directory tree, and route to retained sessions | `apps/web/src/main.tsx`, `packages/app/src/runtime.ts`, `packages/app/src/{shell,directory-picker}.tsx`, `internal/daemon/host.go` | `packages/app/test/runtime.test.ts`, `internal/daemon/host_test.go`, `apps/web/scripts/browser.mjs` |
| Choose a Local working directory in the OS-native folder dialog (osascript/zenity/kdialog/PowerShell), falling back to the web directory browser; Remote uses its daemon directory browser | `host.directory.pick` in `internal/{protocol,daemon}/host.go`, `packages/sdk/src/services.ts`, `packages/app/src/directory-picker.tsx` | `TestDirectoryPickCommand`/`TestHostDirectoryPickValidation` in `internal/daemon/host_test.go` |
| Multiple daemon connections, Local-owned saved profiles, verified identities, isolated disconnects and guided Local/Remote session creation | `packages/app/src/{hosts,runtime}.ts`, `{host-dialog,welcome,settings}.tsx`, `internal/config/remote_hosts.go`, daemon configuration service | `packages/app/test/hosts.test.ts`, `runtime.test.ts`, `sidebar-creation.test.tsx`; `internal/config/remote_hosts_test.go`; `TestProviderClientRemoteHostsPreserveConfigurationAndRejectConflicts` |
| Search and advisory attention across hosts, source labels/filter, independent bounded pagination and partial failures without root hydration | `packages/app/src/{session-search-dialog,attention}.tsx` | `packages/app/test/multi-host-discovery.test.tsx` |
| Author data-only agent definitions in Settings (persona, rules, discovery, modules, capabilities, surface), copy built-ins, add revisions to registered ids, and pick the agent a new session runs | `packages/app/src/settings/agents.tsx`, `packages/app/src/definitions.ts`, `packages/app/src/welcome.tsx`, `session-tabs.ts` (`definition`) | `packages/app/test/settings-agents.test.tsx`, `sidebar-creation.test.tsx` (agent picker), `session-tabs.test.ts` |
| Window-local session tabs across hosts, v3 layout and retained v1/v2 recovery, overflow/search/reorder/close/reopen, preserved attachments and reading anchors, bounded background activity | `packages/app/src/{session-tabs,session-tab-routing,session-tab-strip,compositions,reading-positions}.ts*`, `packages/ui/src/workspace-tabs.tsx`, `internal/daemon/session_summaries.go`, `internal/session/navigation.go` | App tab/routing/composition tests, `apps/web/scripts/session-tabs.mjs`, UI all-theme/CSP tab tests, `TestSessionSummariesAcrossTransports` and navigation bounds tests |
| Independent New Chat tabs, host/setup persistence, original-ID first-message recovery, focus-safe in-place promotion and closed/orphan recovery | `packages/app/src/{session-tabs,session-tab-routing,welcome,welcome-submission,welcome-recovery,runtime}.ts*` | App tab/routing/Welcome/submission/runtime/settings/desktop-close tests; `apps/web/scripts/new-chat-tabs.mjs` |
| Nested split views, draggable tabs between panes, duplicate chats with independent agents/scroll, shared drafts, and responsive layout restoration | `packages/app/src/{session-tabs,session-tab-strip,session-tab-routing,workspace-views,runtime,conversation,composer}.ts*`, `packages/ui/src/workspace-layout.tsx` | App model/routing/runtime/workspace/composer tests; `apps/web/scripts/workspace-layout.mjs`; UI layout Chromium/Firefox, Axe and strict-CSP fixture |
| Read-only session REPL, adjacent Open REPL and nearest same-agent Open chat, independent split modes/agents, live cells and bounded history | `packages/app/src/{repl-view,reading-list,conversation,session-tab-strip}.tsx`, `packages/sdk/src/{executions,state}.ts`, mode-aware tab routing | SDK execution/state tests; app REPL, reader and routing tests; `apps/web/scripts/repl-viewer.mjs` with opt-in `v2_sdk_repl_test.go` fixtures |
| Root/child conversations, grouped tool calls, read-only Starlark, bounded history and recipient-scoped drafts | `packages/app/src/{conversation,timeline,composer}.tsx`, SDK session views | `packages/app/test/{timeline,composer}.test.tsx`, production browser fixture; `apps/web/scripts/performance.mjs` exercises 10,000 root messages, 100 retained children, stable selection/scroll and 32 drafts under 16 concurrent streams |
| Compact growing composer, shared model/reasoning picker for idle root sessions, and neutral input focus borders | `packages/app/src/{composer,model-selection}.tsx`, shared UI form styles | Composer tests; `apps/web/scripts/browser.mjs` (growth/shrink, explicit model/effort changes, busy state, draft/reload preservation); `apps/web/scripts/model-picker.mjs` (detail-card bounds, side flipping, scrolling, keyboard and resize in Chromium/Firefox); split workspace browser fixture |
| Right-aligned user bubbles, hover/focus timestamps and controls, immediate submission previews, queued/running inbox messages | `packages/app/src/{input-presentation,runtime}.ts`, `packages/app/src/{conversation,timeline,composer}.tsx` | `packages/app/test/input-presentation.test.tsx`, composer/runtime tests, `apps/web/scripts/user-messages.mjs` (Chromium/Firefox delayed request, running turn, reload, duplicate text, hover/focus and responsive themes) |
| Errors owned by application, host, session, turn, execution, submission, resource, action or validation, each with one canonical display | [Ownership rules](frontend.md#error-ownership-and-canonical-displays), `packages/app/src/error-feedback.tsx`; latest turn outcome only, recorded execution failures retained | `local-errors.test.tsx`, `welcome-recovery.test.tsx`, error ownership browser fixture |
| Questions, permission decisions, remembered rules and exact-turn cancellation | `packages/app/src/{requests,conversation}.tsx`, SDK permission/command helpers | `packages/app/test/requests.test.tsx`, two-client production browser fixture, existing daemon permission tests |
| Recursive work, mailbox/evidence inspection, goals, schedules, budgets, context and integrations | `packages/app/src/inspector.tsx`, `packages/app/src/details/`, host read services | `packages/app/test/inspector.test.tsx`, `internal/daemon/host_test.go`, generated SDK operation coverage |
| Full-window Settings with seven categories, local control search, responsive navigation and exact workspace return | `packages/app/src/settings.tsx`, `settings/navigation.ts`, `shell.tsx`, `runtime.ts` | `settings-navigation.test.ts`, `desktop-close-tab.test.tsx`, `apps/web/scripts/settings.mjs` and `settings-conversation.mjs` |
| Terminal tabs: a login shell on the session's host in a fourth tab kind, opened from pane and tab menus, the palette or the terminal shortcut; drawn by ghostty-web; reattached with replay after reload or reconnect; closing the tab ends the shell | `packages/app/src/terminal-view.tsx`, `session-tabs.ts` (`TerminalTab`, `openTerminal`, `updateTerminal`), `session-tab-routing.ts` (`openTerminalTab`, `terminalDestination`), `routes/h.$runtimeId.t.$terminalId.tsx`, `session-tab-strip.tsx`, `packages/sdk/src/terminals.ts` | `terminal-view.test.tsx`, `session-tabs.test.ts`, `session-tab-routing.test.ts`, `packages/sdk/test/terminals.test.ts`, `apps/web/scripts/terminal-tabs.mjs`, `apps/desktop/scripts/terminal-smoke.mjs` |
| Host-scoped configuration, login cleanup, unsaved-edit guards and offline draft recovery | `packages/app/src/settings/{configuration,providers,recovery,unsaved}.tsx`, SDK/daemon services | `settings-configuration.test.tsx`, `settings-host-selection.test.tsx`, `settings-unsaved.test.tsx`, provider tests and production Settings workflow |
| Working Appearance controls: bounded tool density, code wrapping, UI/code fonts and sizes, contrast/motion, preview and resets | `packages/app/src/settings/appearance.tsx`, `timeline.tsx`, `runtime.ts`, `packages/ui/src/{appearance-data,themes,tokens.stylex,code-block}.*`, native contrast bridge | `settings-density.test.tsx`, UI appearance/theme tests, desktop-adapter tests, production Settings/conversation workflows |
| Accessible controls, all TUI themes, custom-theme resolution, auto appearance and portaled overlays | `packages/ui`, `internal/theme`, `cmd/themegen`, `internal/daemon/host.go` | Theme parity/drift tests, 66-theme Axe fixtures, thirteen component interaction scenarios, Chromium/Firefox/actual Safari CSP smoke |
| Packaged same-origin web assets and explicit `whip web` launch | `internal/webassets`, `cmd/whip/web.go`, `scripts/pack-web.mjs` | `internal/webassets/assets_test.go`, `cmd/whip/web_test.go`, `scripts/pack-web.test.mjs`, isolated packed-source consumer builds |

React 19 and TanStack Router/Query/Form/Virtual compose the product. Base UI owns
accessible component interactions; StyleX extracts authored CSS. Source UI/app
packages expose explicit public entry points and are tested as real installed
archives in both production and Vite development builds. No editor, code-review
surface, account, pairing or signer UI is included. Tool requests that require
terminal input direct the user to the TUI; terminal tabs are a human-only shell
beside the conversation, not an agent input path.

Closing a page detaches the client. It neither cancels accepted work nor sends
unsent drafts. A command outcome is separate from completion of descendant agents,
mailboxes or schedules. See [web-app.md](web-app.md) for exact startup commands,
trusted-network setup and current browser evidence.

## Session information bar and contoured tabs

Desktop and web use contoured tabs and a compact bar showing host/project,
selected agent and current activity. The bar exposes REPL, agent details and
existing session actions; narrow panes move secondary actions into its menu.
New Chat shows setup identity and **Not started**. REPL uses the same agent
inspector, including pagination, and retains its language/history toolbar.

Every **Open REPL** creates a fresh view immediately right of the source in its
pane. **Open chat** selects the nearest same-agent chat there or creates one.
Drafts, source view state, shared root subscriptions and the 32-view cap remain
intact. History restores exact view identities, including expired closed entries.

- UI: `packages/app/src/{session-info-bar,chat-activity,conversation,welcome}.tsx`,
  `packages/ui/src/workspace-tabs{,.stylex}.ts*`.
- Navigation: `packages/app/src/{session-tabs,session-tab-routing,session-tab-strip}.ts*`.
- Coverage: `session-info-bar.test.tsx`, tab model/routing tests, UI all-theme/CSP
  tests, and the packaged `repl-viewer.mjs` and `chat-activity.mjs` browser workflows.

## Chat activity

Web and desktop share compact execution groups, one current status in the session
information bar, and bounded named-agent rows above the composer. Authored messages retain their order;
expansion exposes bounded code/output and host-operation evidence with an
**Open in REPL** action that creates a fresh adjacent tab. Reading aliases and stable detail windows protect
navigation and focus. Appearance density, typography and reduced motion apply.

- Runtime: `internal/rlm/kernel.go` emits host starts before dispatch and correlated
  completions afterward. `recursive_runtime.go` publishes additive protocol 5.1
  events without a database migration. Cancellation remains distinct from failure.
- State: `packages/sdk/src/executions.ts` joins root/agent/turn/invocation evidence,
  handles replay, late completions and bounded eviction; it is shared by both views.
- UI: `packages/app/src/chat-activity{,-rows}.ts*`, `conversation.tsx`, `timeline.tsx`,
  `reading-positions.ts`, `execution-time.tsx`, and `packages/ui/src/activity-indicator.tsx`.
- Coverage: kernel cancellation/ordering and daemon journal tests, SDK execution
  regressions, `chat-activity.test.tsx`, existing reading tests, and the isolated
  production browser workflow `apps/web/scripts/chat-activity.mjs`.

## Conversation row actions

Sidebar, search, and Session details share Open in, Rename, same-directory Fork,
Archive/Restore, and confirmed Delete. Archived roots retain execution, Attention,
open tabs, and drafts; active/archived/all search and Undo restore their visibility.
Desktop opens local folders or remote SSH aliases in installed Cursor, VS Code,
and Zed; Finder is local-only and browsers can copy the exact directory.

| Behavior | Implementation | Verification |
| --- | --- | --- |
| Bounded full metadata without transcript hydration | `internal/session/metadata.go`, `sessions.get`, `packages/sdk/src/session.ts` | Metadata bounds/store tests, SDK command tests, browser frame assertions |
| Durable archive, filtered cursor revisions, one-way v10→v11 preservation | `internal/session/{migrations,metadata,catalog_page}.go`, `internal/daemon/client_control.go` | Migration rollback/reopen and catalog tests, busy archive/dedup/event tests, race suite |
| Shared host-bound actions and deletion cleanup | `packages/app/src/session-actions.tsx`, `session-search-dialog.tsx`, `runtime.ts`, `session-tabs.ts`, `compositions.ts` | `session-actions.test.tsx`, runtime/tabs/compositions tests, `apps/web/scripts/session-actions.mjs` |
| Fixed native editor launchers and verified runtime identity | `apps/desktop/src/project-open.ts`, main/preload bridge and web desktop adapter | Project opening/adapter tests, real Electron IPC, local launches and Cursor/VS Code SSH handoff; [native acceptance limits](../.ai-docs/plans/conversation-row-actions/README.md#implementation-record--2026-09-08) |

## Terminal UI behavior

- Streaming text, reasoning, tool, plan, permission, usage, and terminal
  events are rendered from daemon events.
- `/agents` inspects or controls the recursive tree; `/mcp` manages server
  lifecycle.
- `/model`, `/effort`, `/goal`, `/compact`, `/rewind`, `/fork`, `/schedule`,
  `/browser`, and `/computer-use` are daemon commands.
- ACP maps editor sessions and permission decisions onto the same root
  protocol. It does not own a second agent loop.
- The TUI is a single full-screen (alternate-screen) interface laid out like
  opendocker: a left column of panels, the transcript with its input box, and
  a key-hint footer across the whole last row. On exit it prints a resume line
  to the scrollback. The former inline mode and the `uiMode` config key are
  gone.
- The empty-transcript home screen centers a "whipcode" wordmark drawn in the
  opencode ▀▄█ block-glyph pixel font, rendered in the active theme's
  foreground (bold), so it recolors on every theme or light/dark swap like the
  web wordmark's `currentColor` (`opencodeLogo` in `internal/tui/opencode.go`).
- The left column shows on terminals of 120 columns or more and holds three
  panels: `[1] Agents`, `[2] Context` (tokens, share of the window, spend) and
  `[3] LSP`. One is expanded and the others collapse to their header row;
  `ctrl+x 1/2/3` pick the expanded one, `ctrl+x b` hides the column. The
  `sidebar` and `panel` config keys set the startup state.
- Agent rows are structured: a lifecycle badge (running, blocked, idle, done,
  failed, queued…), the name indented by depth, and what the agent is doing
  (its running REPL cell or tool with the elapsed time, or pending mail). The
  root heads the tree. `ctrl+t` or ↓ on an empty input focuses the panel
  (its bar lights up), ↑/↓ select, enter opens an agent, `ctrl+x s` stops
  the selected one, esc leaves; enter on the root, or esc with an empty
  input, returns from a child to the main transcript. When the left column
  is hidden or the terminal is narrow, `/dock` shows the same rows under the
  input; they are hidden by default, and `ctrl+t` shows them to focus them.
- `ctrl+r` (or `ctrl+x r`, `/repl`, config key `repl`) opens the REPL panel
  on the right: the open agent's live Starlark cells, code as the model
  writes it, print output as it happens, each host call from start to
  outcome, results, errors, and worker restarts. Below 150 columns the panel
  takes the left column's place; from 150 the two share the screen. The panel
  takes half of the width right of the left column (half the terminal when
  the column is hidden). The wheel over the panel scrolls it
  independently of the chat (it follows the newest cell until you scroll up,
  then a "↓ N more lines" chip and a scrollbar mark the position). The panel
  keeps every cell seen during the TUI session, even after snapshots drop
  idle children.
- `user.ask` from the root agent opens a floating dialog over the dimmed
  session: the question, the numbered options with their descriptions (a ★
  marks the agent's recommended option), and key hints. ↑↓ (j/k) move,
  1–6 jump, space toggles when several answers are allowed, enter answers,
  esc dismisses. A batched ask (`user.ask(questions=[...])`) pages: the title
  reads "Question 2/4", enter answers and advances, tab/→ next,
  shift+tab/← back, s skips the page, / types a written response, and the
  last page's enter submits the batch; the transcript line notes
  "answered 3/4". The dialog stays up until the daemon
  records the answer (it may come from another client), then a dim transcript
  line notes what was chosen.
- The frame has a one-row margin above the columns and a two-row footer band
  at the bottom (a blank row, then the key hints on the last row) under the
  prompt or, when `/dock` shows the agents dock, under it. The hints' right
  side lists `ctrl+r repl` and `ctrl+p commands`; the left side follows the
  keyboard's owner: the running turn (spinner, `esc interrupt`), an armed
  `ctrl+x` leader (every chord), the focused Agents panel, or the working
  directory.
- `shift+enter`, `ctrl+j` and `alt+enter` insert a newline. Bubble Tea v2
  requests kitty key disambiguation and modifyOtherKeys at startup; inside
  tmux a modified key only reaches the pane when the server option
  `extended-keys` is on. whip never changes your tmux server: when the option
  is off it warns and suggests `set -s extended-keys on` in `~/.tmux.conf`.
  mosh collapses shift+enter before tmux or whip see it — use ctrl+j there.
- Pasted images show as chips. A clipboard image (`ctrl+v`) lands in the
  input as `[Image N]`; a pasted or dropped image path, or a macOS screenshot
  preview, as `[Image N: name.png]` (long names shortened to 24 columns), with
  the bytes copied to `~/.whip/pastes/`. The live transcript echoes the chip;
  only the text sent to the daemon expands it to the real `@path` mention,
  which is what a resumed or rebuilt transcript shows. The registry resets on
  `/clear` and when the TUI switches root session, so a recalled chip from
  before stays literal text.

## Storage and recovery

Runtime-v2 stores command/event journals, agents, per-agent transcripts,
messages, state, capabilities, budgets, permissions, schedules, and content
references in SQLite WAL plus immutable content files.

On restart:

- committed command outcomes remain final;
- running descendant sessions become retained idle sessions;
- queued recursive notifications remain actionable;
- uncertain operations are interrupted and reservations are reconciled;
- external side effects are never guessed or replayed automatically.

See [architecture.md](architecture.md), [rlm-runtime.md](rlm-runtime.md), and
[concurrency.md](concurrency.md) for the contracts behind these features.

## Transcript navigation

The transcript scrolls with the wheel and PgUp/PgDn. When it is longer than
the window a scrollbar sits in the column right of the text; scrolled away from
the newest rows, a "↓ N more lines" chip marks how far, and clicking it (or a
new turn, when following) jumps back to the bottom. Drag to select and copy
(OSC 52, with a clipboard tool fallback); double-click selects a word,
triple-click a row, both copying immediately. Clicking a user or assistant
message opens Message Actions (revert, copy, fork); clicking a tool result
expands it. Failed local commands report in the top-right toast rather than in
the conversation.

## Skills

SKILL.md skills load from `.agents/skills` in the working directory,
`~/.whip/skills`, and `~/.agents/skills` (`skills.DirsFor`).

CLI: `whip skills list` (names, sources, warnings) and `whip skills import
[--dry-run]` — copies skills from other harnesses' user dirs
(`~/.codex/skills`, `~/.claude/skills` — `skills.ForeignDirs`) into
`~/.agents/skills`, deduped by name against what whip already loads and
across the sources (codex wins on a dup). Never overwrites an existing
skill. Tests: `cmd/whip/skills_test.go`.

**Spec compliance** (agentskills.io, matching pi's `core/skills.ts`): name
validated (≤64 chars, lowercase a-z/0-9/hyphens, no leading/trailing/double
hyphens), description ≤1024 chars (a *validity* ceiling, not a prompt budget),
`disable-model-invocation: true` skills excluded from the catalog but still
invocable via `$name`. Violations load with a `Warning` (surfaced in the
startup report), never silently disappear. Tests: `skills/spec_test.go`.

## Themes

Every color whip paints comes from one theme: text, muted, accents, the
selection fill, the raised surfaces behind cards and the prompt box, and the
syntax colors inside code blocks (markdown and tool output share them). `auto`
follows the terminal background; `light` and `dark` pin the built-ins.

`/theme` with no argument opens the switcher. `/theme <name>` pins a theme and
saves it to the config (`"theme": "<name>"`).

The whole view is painted with the theme's background and text colour, so a
light theme reads on a dark terminal and the terminal's own colours follow
the theme while whip runs (they are restored on exit). Besides whip's `light`
and `dark`, the switcher lists opencode's theme catalog, converted from its
assets with `internal/theme/themes/convert_opencode.py`: aura, ayu,
carbonfox, catppuccin (latte/frappe/macchiato), cobalt2, cursor, dracula,
everforest, flexoki, github, gruvbox, kanagawa, lucent-orng, material, matrix,
mercury, monokai, nightowl, nord, one-dark, opencode, orng, osaka-jade,
palenight, rosepine, solarized, synthwave84, tokyonight, vercel, vesper and
zenburn — each as `<name>` (dark) and `<name>-light`. Catalog themes pin their
surfaces, syntax colours and markdown accents; whip's own themes derive them.

User themes are JSON files in `~/.whip/themes/<name>.json` (or under
`$WHIP_HOME`). Any token you leave out defaults from the built-in of the same
darkness; unknown keys and malformed colors are reported with the allowed keys
when you run `/theme`. Colors are `#rrggbb` or an ANSI palette index `0`-`255`.

```json
{
  "dark": true,
  "palette": {
    "text": "#e0e0e0", "muted": "#808080", "faint": "#5a5a5a",
    "primary": "#00aaff", "accent": "#c678dd",
    "success": "#98c379", "warning": "#e5c07b", "error": "#e06c75", "info": "#61afef",
    "link": "#56b6c2", "emphasis": "#e5c07b", "onPrimary": "#0a0a0a",
    "border": "#3a3a3a", "borderFocus": "#61afef", "bg": "#1e1e1e",
    "diffAdd": "22", "diffDel": "52"
  },
  "surfaces": { "panel": "#262626", "element": "#303030", "hover": "#3a3a3a" },
  "chroma": "dracula"
}
```

`diffAdd`/`diffDel` are the background tints behind added and removed diff
lines. Optional `syntax` (`keyword`, `string`, `number`, `comment`, `function`,
`type`, `operator`, `punctuation`) and `markdown` (`heading`, `strong`, `code`,
`quote`) blocks pin those colours instead of deriving them from the palette.
`surfaces` is optional: without it the card and prompt fills are derived from
the terminal's real background so they read as raised layers on any terminal.
`chroma` is optional: without it the code colors are generated from the
palette; with it, that registered chroma style is used instead.


The renderer-independent specification, catalog and ANSI/Chroma resolver live in
`internal/theme`; the TUI retains terminal-specific rendering and background
handling in `internal/tui/theme`. The browser generates all 66 named palettes with
`cmd/themegen`, follows `prefers-color-scheme` for `auto`, and stores selection on
the viewing device. Its accessible surface/text derivation leaves source palettes
unchanged and retains full Chroma code styling. Host custom discovery and pasted
JSON import share the Go resolver; the browser never compiles arbitrary CSS.

**Claude Code** (`claude-code`) is a built-in dark theme based on the supplied
Paper desktop screens: `#141414` canvas, `#111110` sidebar, warm gray text,
`#222221` composer/hover fills, and `#343434` selected rows. Select it under
**Settings → Appearance → Color theme**, or use `/theme claude-code` in the TUI.
The source is `internal/theme/themes/claude-code.json`; research and the role
mapping are in `.ai-docs/plans/claude-code-theme/README.md`. Optional `displayName`
and optional `web` fields (`navigation`, `quietBorder`, `codeBackground`,
`inlineCodeBackground`) preserve the stable theme ID and pin browser surfaces; themes without overrides retain the existing derivation.
The browser still adjusts insufficient text contrast, including inline code.
Tests: `internal/theme/resolve_test.go`, `packages/ui/tests/themes.test.ts`,
and `apps/web/scripts/claude-code-theme.mjs` cover catalog parity, validation,
source colors, rendered surfaces, selection, reload and switching away.

## Web directory navigation

The saved-session sidebar follows the compact Claude/Paper hierarchy while using
WHIP's themes. It groups the loaded SDK catalog by exact directory, keeps
worktrees distinct, preserves pin/recency order, and offers New session, Search
sessions and Settings. Hosts have separate headings and connection status within
one sidebar. Directory + preselects its source host and folder in the Local/Remote
creation form; it does not create work until submitted. Search opens a centered
dialog with host labels/filter, recent sessions, debounced host search, independent
bounded paging, partial errors and arrow/Enter navigation (`session-search-dialog.tsx`; `apps/web/scripts/session-search.mjs`).
Native session links and
background-tab menus preserve remembered child/inspector locations.

The sidebar has a 320 px default, keyboard/pointer resizing (256–420 px), a
hide/show toggle, window-local layout and bounded host-specific collapse state.
Mobile uses a contained Sheet with touch targets and a fixed footer. Virtualized
catalog updates preserve the visible reading anchor and never hydrate roots to
obtain labels. See [frontend navigation](frontend.md#saved-session-navigation).

Code: `packages/app/src/session-sidebar.tsx`, `sidebar-state.ts`,
`sidebar-layout.tsx`, `welcome.tsx`, and `session-tab-routing.ts`.
Tests: `sidebar-state.test.ts`, `sidebar-layout.test.tsx`,
`sidebar-creation.test.tsx`, `session-tab-routing.test.ts`, and the isolated
production-browser workflow `apps/web/scripts/sidebar.mjs`.


## Multiple execution hosts in the web workspace

Local owns a `remote_hosts` registry in its existing distribution-specific
configuration file. Saved IDs, names, URLs, verified runtime IDs and startup
connection preferences are shared across browsers through revision-checked config
updates. Browser-only addresses remain explicitly importable. Separate SDK clients
connect directly to existing LAN/Tailscale daemons, verify identity, and reject
runtime aliases or unaccepted replacements. Losing one host preserves the others;
losing Local blocks profile edits while attached remote sessions remain usable.

One v3 window layout carries runtime identity on every tab and allows mixed-host
panes. The existing four-pane, 32-tab and four-root-view budgets apply to the whole
window. Migration adopts the last-used legacy host layout and keeps original
v1/v2 data, with remaining layouts available under **Restore previous host tabs**.
If a complete layout cannot fit, open individual previous tabs, including closed
entries, without consuming the original layout.
Runtime/root/recipient and runtime/client/command identities prevent collisions
in drafts, views, content and command observations.

New sessions explicitly choose Local or Remote, host, folder and that host's
model/default. Settings identify the target execution host; saved-host edits
always target Local and viewing preferences stay local to the browser. Search
and attention show source hosts, filters, separate bounded pages and partial
failures. Responses open their owning sessions; neither index hydrates roots.
Listener setup and exact browser Origin allowlists remain explicit trusted-network
configuration; automatic stable-Origin handling is deferred.
The desktop origin `whip-app://bundle` can be explicitly allowed through
`WHIP_ALLOWED_ORIGINS` (`WHIPCODE_ALLOWED_ORIGINS` for the whipcode distribution).
Other custom origins, wildcards, suffixes, ports and paths remain rejected.
`internal/daemon/network_test.go:TestNetworkDesktopOriginIsExplicitAndExact`
checks validation, explicit opt-in and CORS response headers.

Code: `internal/config/remote_hosts.go`, `internal/daemon/provider_service.go`,
`packages/app/src/{hosts,runtime,session-tabs,workspace-views}.ts`,
`{host-dialog,welcome,settings,session-search-dialog,attention}.tsx`.
Tests: `internal/config/remote_hosts_test.go`, the remote-host configuration test
in `internal/daemon/provider_client_behavior_test.go`, and
`packages/app/test/{hosts,runtime,session-tabs,session-tab-routing,workspace-views}.test.ts`,
`sidebar-creation.test.tsx`, `multi-host-discovery.test.tsx`.
See [architecture](frontend.md#runtime-construction-and-lifetimes) and
[operation](web-app.md#multiple-execution-hosts). Validation evidence is tracked in
the [phased plan](../.ai-docs/plans/web-multiple-hosts/README.md).


## Model usage budgets

New root sessions and uncapped descendants have unlimited cumulative cost,
tokens, and elapsed usage. Optional `agents.spawn(..., budgets=...)` limits still
constrain a subtree; zero is a zero allowance. Worker/concurrency, recursion,
storage, and provider response limits remain independent and bounded.

Web and TUI budget inspection is read-only. Per-attempt accounting uses each
route's model prices, including child overrides, compaction, and helper calls.
Unknown provider usage is marked incomplete and retained separately from known
usage; finite caps account for that uncertainty conservatively. Catalog-derived
costs are estimates, not provider invoices. Protocol 4 carries nullable limits;
the fresh runtime schema is version 8. Older stores are rejected without being
modified; this change includes no session migration or automatic data deletion.


## Whipcode distribution

The `whip-rlm` branch publishes a separate `whipcode` executable through copied
CI, security, and release workflows. Both distributions use one Go runtime and
the same embedded web application. A compiled `internal/buildinfo` identity
selects CLI instructions, `.whipcode` home paths, `WHIPCODE_HOME`, and isolated
network controls. Application-owned config, auth, sessions, locks, notices,
skills, browser profiles/extension state, and macOS helper extraction follow
that home; renaming a binary does not switch its identity.

`install-whipcode.sh` verifies complete, versioned prerelease assets and SHA-256
checksums before atomic replacement. `whipcode update` stays in its channel,
replaces the invoked installation, and requests only its daemon's restart.
Stable `whip` release discovery remains unchanged. See [installation](../README.md#whipcode-branch-builds).

Code: `internal/buildinfo`, `internal/config`, `internal/update/whipcode.go`,
`cmd/whip/update.go`, `install-whipcode.sh`, `scripts/publish-whipcode.sh`, and
`.github/workflows/{ci,security,release}-whipcode.yml`.
Tests: `TestDistribution*` in the affected Go packages, `TestFetchWhipcodePages`,
`TestWhipcodeVersionComparison`, `scripts/test-install-whipcode.py`, and
`scripts/test-distributions.py` (both compiled binaries, independent sockets,
restart, and self-update).
## Native mobile companion (development)

The Expo workspace in `apps/mobile` provides manual private-host setup, themed
Sessions/Attention/Settings, root and child conversations, queued/steering input,
turn-specific Stop, question forms and one-shot permission decisions. It consumes
the existing SDK WebSocket protocol; execution stays on the host. Application auth,
QR pairing and push notifications remain outside this release.

- Connection diagnostics: `apps/mobile/src/runtime/connection-test.ts` and
  `app/server.tsx`; transient HTTPS/WSS/session probes, per-step deadlines,
  identity checks and modal-local actionable errors. `connection-test.test.ts`
  and `server-screen.test.tsx` cover HTTP/protocol failures, headless hosts,
  response bounds, cancellation and late results. SDK transport tests preserve
  React Native close reasons without leaking callbacks after disposal.
- Native runtime and lifecycle: `apps/mobile/src/runtime/runtime.ts`, SDK
  `client.pause/resume` and synchronized views; covered by SDK client/state tests
  and mobile runtime tests.
- Durable local identity, revisioned drafts and atomic correlation:
  `apps/mobile/src/runtime/storage.ts`, local `WhipStorage` native module;
  `storage.test.ts` and `storage.native.test.ts` cover SQLite atomicity, quotas,
  key/database mismatch and native setup boundaries.
- Partial creation recovery: `apps/mobile/src/features/creation.ts` and
  `creation.test.ts`; separate create/effort/input identities preserve the created
  root without automatic continuation after restart.
- Foreground Attention and its qualified tab badge:
  `apps/mobile/src/features/attention.tsx` and `attention.test.tsx`; one observer
  handles polling, bounded pagination, focus refresh and stale/partial counts.
- Bounded native text: `apps/mobile/src/components/paged-text.tsx` and
  `conversation.tsx`; paging, recycling, full-copy and explicit body-read tests
  cover the rendering boundary. `markdown.test.tsx` covers source fallback for
  images/HTML and the external-link allowlist.
- Permission recovery: SDK `permissions.status`, daemon permission outcome
  normalization/legacy decoding; SDK services and daemon server tests cover both
  transports, failed outcomes and original decision identity.
- Pure reuse: `@whip/app/presentation` and `@whip/ui/theme-data`; web retains its
  own renderer. See [frontend.md](frontend.md) for package boundaries.

Native device validation and distribution are not implied by this entry.
[Mobile setup](mobile.md) and the
[implementation evidence](../.ai-docs/plans/mobile-app/EVIDENCE.md) track the actual
build/device/release state.

## Canonical Frontier evaluations

[Evaluation workflow](../evals/README.md): `uv run --project evals --locked whip-eval`
runs fixed, nested Smoke 8 / Medium 15 / Full 30 profiles against Kimi K3 on
Inference.net. All code and locks live under `evals/`; native Harbor/Pier graders
remain authoritative. No live evaluations or qualification containers were run
as part of this implementation, and the accepted baseline starts uninitialized.

- `whip_evals/tasks.py`, `prepare.py`, and `frontier/` pin source/files, OCI images,
  native resources/deadlines, model catalog and build/configuration. Runtime A/B
  captures one shared binary; dirty snapshots remain development-only.
  `test_contract.py` covers workload expansion, limits, vision capabilities,
  immutable task preparation, native parsing and shared build identity.
- `execution.py` owns a cross-suite pool of at most 32 native trial subprocesses,
  reserves separate-verifier capacity, and cancels workers before waiting for
  shutdown. Cleanup verifies native container identity and log mounts.
  `test_pool.py` proves overlap, admission, failed-cleanup dispatch stop and
  callback-failure cancellation; historical cleanup tests remain applicable.
- `adapter.py` / `observe.py` retain whole-tree finality, scoped SQLite/content
  export and cost accounting. `report.py` cross-checks ledger/state/metrics and
  emits one JSON result plus Markdown/CSV under `evals/reports/<run-id>/`.
  Native failures, missing grades, cost uncertainty and timeout causes remain
  distinct. `test_reports.py` covers stale accounting, corrupt evidence, planned
  denominators, timing, paired comparisons and immutable publication.
- `baseline.py` accepts only explicit clean Full campaigns with three repetitions,
  complete evidence and the versioned quality/cost/latency gates. The accepted
  pointer uses locked compare-and-swap plus immutable history.
  `test_baseline.py` covers first acceptance, replacement guards, stale/concurrent
  publishers, evidence tampering and interrupted pointer writes.
- `doctor --integration` is an explicit future-machine check using authored fake
  model responses and both engines/runners/verifier modes. `fixtures/native/`
  verifies paging, background service survival and a committed patch collected
  into a separate verifier. It is excluded from proficiency scores.
- Reports can be regenerated offline into new analysis directories. Interrupted
  execution is not resumed. There is no eval CI or automatic artifact pruning.
  Existing `evals/runtime-ab` studies and `evals/rlm` tests are retained.
