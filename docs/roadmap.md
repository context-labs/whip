# Roadmap

whip is converging on one recursive runtime rather than maintaining separate
direct-tool and RLM agents.

## Implemented in the recursive-runtime overhaul

- [x] One model execution path and one model-facing tool: `rlm_exec`.
- [x] One `AgentSession` type for root, child, and grandchild sessions.
- [x] Clean runtime-v2 home/database boundary; old session data is untouched.
- [x] Root IDs are root agent IDs; mode fields and mode configuration removed.
- [x] Retained multi-turn children with persisted route, effort, cwd,
  transcript, capabilities, and budgets.
- [x] Explicit `messages.send/list/read/ack`; no automatic child-answer fan-in.
- [x] Metadata-only, coalesced mailbox and agent-change notifications.
- [x] Capability inheritance/narrowing and a default two-edge depth limit.
- [x] Kernel capacity reservation before durable child admission.
- [x] MCP list/call operations available through the same Starlark module to
  roots and children.
- [x] `/agents` as the single user-facing tree command; old task commands
  removed from the daemon protocol and current TUI surface.
- [x] Restart reconstruction for retained recursive agents.
- [x] Deterministic single-runtime evaluation and parity-focused integration
  tests.

## Public documentation site

- [x] Independent `apps/docs` site: Quickstart entry, static docs routes,
  21 V1 pages, app-owned Base UI/CSS components, light/dark themes, build-time
  syntax highlighting and static-artifact/content/browser test coverage. See
  [the feature map](features.md#public-documentation-site) for code and test paths.
- [ ] Write and review V1 article content under the approved headings.
- [ ] Public deployment: select and review the production origin/host, validate
  the referenced live release artifacts and publish only `dist/client`. This
  remains separate from repository implementation; no domain or host is assumed.

## Cleanup still worth doing

- [ ] Remove the remaining unreachable embedded direct-tool TUI/agent helpers
  and their historical task persistence tables after downstream integrations
  no longer compile against them.
- [ ] Split mixed historical/new swarm storage code into focused `agents`,
  `messages`, and `budgets` files.
- [ ] Replace residual terminology in historical test names and comments.
- [ ] Add an explicit session kind for protocol-only tool hosts so
  `whip mcp serve` does not identify itself through model/provider sentinel
  strings.

## Effectiveness work

- [x] Canonical Frontier evaluation CLI with fixed 8/15/30-task profiles, up to
  32 resource-admitted trials, versioned reports, paired comparisons and automatic
  baseline acceptance. [Workflow](../evals/README.md); implemented and tested offline.
- [ ] Qualify the evaluation host and initialize the first canonical Full baseline
  using the documented promotion campaign. Live execution deferred by request.
- [x] Selectable Starlark/QuickJS engines with immutable session selection,
  shared host authority, settled checkpoints, and language-aware clients.
  See [runtime semantics](rlm-runtime.md#execution-language-and-checkpoints).
- [x] Complete the matched Kimi K3 runtime benchmark and report all attempts
  in [the results](../evals/runtime-ab/RESULTS.md), with [reproduction](../evals/runtime-ab/README.md).
- [ ] Add realistic multi-agent benchmark tasks: repository survey, parallel
  review, implementation plus verification, and adversarial message volume.
- [ ] Measure useful work per root token, child utilization, time-to-first
  evidence, redundant reads, and coordination overhead.
- [ ] Tune prompts for when to use `models.batch` versus retained agents.
- [ ] Add child scheduling/fairness policy when the kernel pool is saturated.
- [ ] Surface concise child/mailbox state in the TUI without exposing message
  bodies automatically.
- [ ] Evaluate optional child summaries as an explicit message helper, while
  keeping transport and context admission separate.

## Safety and operations

- [x] Publish isolated `whipcode` branch builds with their own installer and update channel.

- [ ] Harden kernel containment beyond process/resource limits where supported
  by the host OS.
- [ ] Add operator diagnostics for leaked processes, stuck permission requests,
  budget pressure, and repeated worker crashes.
- [ ] Expand Linux/macOS race and restart coverage for recursive trees and MCP
  reconnection.
- [x] Repair the MCP contracts and make the MCP surfaces honest: one selection
  step for the definition's server list, additive untrusted attach, a separate
  project import source, import as the trust path, origin-bound credentials,
  complete paged discovery, chained reconnects, visible source errors, and
  structured/binary results kept. See the
  [MCP contracts plan](../.ai-docs/plans/mcp-contracts/PLAN.md).
- [x] MCP progressive discovery for the model: `mcp.search` across servers,
  `mcp.describe` for one schema, windowed schema-free `list_tools`, all over
  the daemon's cached catalogs
  ([MCP discovery plan](../.ai-docs/plans/mcp-discovery/PLAN.md)).
- [x] Session-scoped additive MCP refresh and root-agent reconnect helpers,
  with automatic current-session refresh after Settings import and a manual
  Integrations refresh control
  ([Live MCP refresh plan](../.ai-docs/plans/mcp-live-refresh/README.md)).
- [ ] MCP tool browser in the web and TUI over the same daemon search.

The original runtime plan and implementation learnings live in
[`docs/plans/2026-08-29-1740-feat-rlm-swarm-runtime-plan.md`](plans/2026-08-29-1740-feat-rlm-swarm-runtime-plan.md).
The consolidation plan for completing the single recursive architecture lives
in
[`docs/plans/2026-09-02-1200-refactor-single-recursive-agent-runtime-plan.md`](plans/2026-09-02-1200-refactor-single-recursive-agent-runtime-plan.md).

## WHIP client foundation

- [x] One typed protocol over Unix sockets and WebSockets; v1 removed.
- [x] Durable acceptance/status, consistent reconnect and bounded transcript views.
- [x] Generated TypeScript/Ajv contract for thin React and Electron clients.
- [x] Host-owned providers/configuration and complete existing Go client cutover.
- [x] Provider logos and connect/manage rows; host environment discovery for
  Inference.net/OpenRouter, revision-checked disable/disconnect and local desktop
  shell recovery. See the [provider connections plan](../.ai-docs/plans/provider-connections/README.md).
- [x] Built-in ChatGPT subscription provider (`openai-codex`): host-owned login,
  model discovery, Responses streaming and durable continuation; live account,
  runtime and browser acceptance passed. See the [subscription plan](../.ai-docs/plans/openai-subscriptions/README.md).
- [x] Provider onboarding across TUI, web and desktop: detected connections,
  restrained Inference.net preference, explicit saved model/provider pairs,
  reusable `/connect`, prompt-first welcome with durable first-send recovery,
  and one-action native backend setup. See the
  [onboarding implementation and validation](../.ai-docs/plans/provider-onboarding/README.md).
- [x] Provider connection inside the normal TUI: shared themed dialog, one
  composer and client, deferred root creation, draft preservation and removal
  of the standalone onboarding and legacy authentication UI. See
  [TUI integration](../.ai-docs/plans/provider-onboarding/TUI-INTEGRATION.md).
- [x] Disposable Docker onboarding workflow: build dirty working files, open a
  clean TUI and shared web app at localhost:4000, and remove test state on exit.
  See [the Docker workflow](setup.md#test-fresh-onboarding-in-docker).
- [x] File-backed custom provider configuration in the TUI: endpoint/key/environment/
  no-auth forms, discovery and manual models, revision-safe management, explicit
  session reload, and reusable host/SDK APIs. See
  [implementation and acceptance](../.ai-docs/plans/tui-provider-configuration/README.md).
- [x] Compact TUI provider picker, key-only known presets, stable connection marks,
  separate OpenAI API/subscription routes, and conservative local credential
  discovery. See [picker implementation](../.ai-docs/plans/tui-provider-configuration/PICKER-REDESIGN.md).
- [x] Models.dev metadata and named local-key discovery: reviewed offline bundle,
  generated desktop key names, explicit file sources, idempotent provider-reference
  persistence and removal of the OpenCode credential importer. See
  [implementation and validation](../.ai-docs/plans/models-dev-discovery/README.md).
- [x] One-step TUI popular-provider model/effort defaults, quiet picker footers,
  and Astra API tool support through Responses. See [selectors and validation](../.ai-docs/plans/tui-provider-configuration/AUTO-MODELS.md).
- [x] Attach-only TypeScript SDK, durable command handles, bounded synchronized
  views, scoped content, permission helpers and minimal React example.
- [x] Trusted-client approvals: no enrollment, signer or first-run pairing prompt.
  Protocol v3 preserves permission decisions, rules and internal agent authority.
- [x] React web implementation with separate UI/app/web source packages, TanStack
  application primitives, Base UI controls and extracted StyleX styles.
- [x] MCP import screen in the web and desktop app: the servers other agents
  configured on a host (Codex, Claude, OpenCode, project file) offered once on
  New session and from Settings, ticked servers written as native trusted
  entries, no probing before import. See the
  [MCP import onboarding plan](../.ai-docs/plans/mcp-import-onboarding/README.md).
- [x] Shared web/desktop startup splash with Whip's wordmark, HALO animations,
  reduced-motion support and bounded loading. See [startup splash](../.ai-docs/plans/startup-splash/README.md).
- [x] All 66 TUI themes, automatic appearance, custom-theme resolution and themed
  read-only code, with deterministic generation and component contrast checks.
- [x] Packaged browser assets in the executable; no production Node server.
- [ ] Single out-of-process web gateway: socket-only daemon by default, foreground
  `whip web`, and optional owned child via `WHIP_NETWORK=1`. This supersedes the
  prior default-on in-daemon listener; exact Host/Origin checks and protocol paths
  remain unchanged. Migration validation is tracked in the
  [gateway acceptance plan](../.ai-docs/plans/web-gateway/README.md); see
  [web access](web-app.md#run-the-packaged-application-locally) for the new contract.
- [x] Session tabs with window-local restoration, preserved drafts/reading position,
  bounded background activity, and responsive themed navigation. See the
  [session-tabs implementation and acceptance](../.ai-docs/plans/session-tabs/README.md).
- [x] Desktop/web slash skill suggestions in existing and first-message composers:
  prefix filtering, keyboard insertion of `$name`, and read-only pre-session
  discovery, including negotiated user-global skills before selecting a project.
  Implementation/validation in [slash skill suggestions](../.ai-docs/plans/desktop-slash-skills/README.md)
  and [global discovery](../.ai-docs/plans/global-skill-discovery/README.md).
- [x] Independent New Chat workspace tabs, durable first-message recovery and
  in-place session promotion. Implementation and feature acceptance recorded in
  [New Chat tabs](../.ai-docs/plans/new-chat-tabs/README.md).
- [x] Desktop Command+T / File > New session reuses New Chat creation, including
  embedded website focus; no new web shortcut. See the
  [implementation and checks](../.ai-docs/plans/new-session-shortcut/README.md).
- [x] Desktop Reopen closed tab shortcut: Shift+Cmd+T uses existing bounded tab
  history, including from Settings and a hidden window; no web shortcut. See the
  [implementation and checks](../.ai-docs/plans/reopen-tab-shortcut/README.md).
- [x] Nested in-window split panes with movable tabs, duplicate chat views, independent
  reading/agent selection and bounded observation. See the
  [split-view implementation](../.ai-docs/plans/split-views/README.md).
- [x] Contoured tabs and a session information bar with host/project, selected
  agent, current activity and scoped actions; Open REPL creates a fresh tab to
  the right. See [session chrome](../.ai-docs/plans/zed-session-chrome/README.md).
- [x] Read-only session REPL notebook with adjacent view opening, independent split
  views and bounded live/recorded execution evidence. See the
  [session REPL implementation](../.ai-docs/plans/session-repl-viewer/README.md).
- [x] Compact chat execution groups, shared current host-operation status,
  named-agent activity, bounded disclosures and shared reduced-motion controls.
  See the [chat activity implementation](../.ai-docs/plans/chat-activity/README.md).
- [x] Restore a compact composer agent dock with active work first, finished turns
  collapsed, compact inline launch evidence and reusable right-split child chats.
  See the [implementation and validation plan](../.ai-docs/plans/subagent-composer-dock/README.md).
- [x] Dedicated full-window Settings with category navigation/search, guarded
  host-specific forms, exact workspace return and working Appearance controls
  for density, wrapping, fonts, contrast and motion. See the
  [implementation and acceptance record](../.ai-docs/plans/settings-redesign/README.md).
- [ ] Complete the web application's release acceptance: full workflow/recovery
  matrix, actual mobile devices, VoiceOver/keyboard review and documented
  performance gates. Implementation does not by itself complete this milestone.
- [ ] Electron desktop release acceptance — implementation and signed local packages are available; notarized installation, real updates and manual device gates remain. See [desktop guide](desktop.md) and [verified progress](../.ai-docs/plans/desktop-app/progress.md).
- [ ] Browser Design Mode acceptance: multi-element selection, trusted floating
  composer and scoped context-to-chat pass automated app/renderer/native checks;
  manual IME/accessibility and packaged local/SSH checks remain before broad release.
  See the [plan and validation](../.ai-docs/plans/browser-design-mode/README.md).
- [ ] Experimental desktop Browser rollout: human workspace tabs, explicit
  conversation/agent access and saved-SSH previews are implemented. Browser tabs
  are **enabled by default**; `WHIP_DESKTOP_BROWSER_TABS=0` disables them at launch.
  Default availability does not complete the remaining release gates. See the
  [behavior → implementation → tests map](features.md#experimental-desktop-browser-tabs)
  and [implementation ledger](../.ai-docs/plans/browser-tabs/implementation.md).
  Completion requires the remaining [Phase 7 gates](../.ai-docs/plans/browser-tabs/README.md):
  shipping-security packaged local/SSH and overlay/focus acceptance, security and
  dependency review, supported-macOS/VoiceOver/IME/manual checks, performance and
  lifecycle budgets, and disabled/old-client/rollback validation. Passing unit or
  development-native fixtures does not complete this rollout milestone.
- [ ] Unified development-alpha rollout and Desktop release acceptance. See the
  [rollout procedure](releases.md#pause-rollout-and-recovery); configured
  infrastructure does not complete release/install/update acceptance:
  - [x] Create `whipcode-alpha-releases` and the native
    `whipcode-alpha-releases.inference.net` domain with minimum TLS 1.2; configure
    matching signing/publishing feed variables and exact environment allowlists.
  - [ ] Create/verify alpha-only credentials and public downloads; prove canary
    denial on stable storage and safely revoke broad old keys. Removing secret
    copies does not revoke credentials.
  - [ ] Complete paused branch/bootstrap checks, development integrity rules and
    first complete `v1.0.1-alpha.N` dispatch per the rollout procedure.
  - [ ] Accept all 17 candidate files/checksums/attestations, fresh CLI, signed/
    notarized Desktop/backend/renderer and the one-time existing-Beta reinstall.
  - [ ] Prove automatic publication on a legitimate development push and actual
    N→N+1 updates with a second accepted alpha, including clean-machine/minimum-OS/
    hardware/manual acceptance. CI startup alone is insufficient.
  - [ ] Reconfirm stable latest/feed and production web/docs sites unchanged;
    record receipts and remaining manual gates. See [Desktop operations](desktop-releases.md).
- [x] Canonical desktop whipcode integration: one selected installed executable,
  a shared default `~/.whipcode` home, verified installation payload and explicit
  local connection diagnostics, installation and restart controls. Machine cleanup,
  signed/notarized installation, desktop/CLI startup, WebSocket recovery and a live
  provider message passed. See the
  [canonical installation record](../.ai-docs/plans/canonical-whipcode/README.md).
- [x] Mobile connection diagnostics: modal-local errors and explicit HTTPS,
  WebSocket and session API testing, with cancellation and native error details.
- [x] Mobile UI implementation: native component library, complete generated
  theme catalog and Appearance settings, independent hosts, combined sessions,
  guided creation, chat and request/settings surfaces. Android UI exercised
  against an isolated host; remaining iOS/device acceptance is recorded in the
  [UI evidence](../.ai-docs/plans/mobile-ui/EVIDENCE.md).
- [ ] Native mobile beta: Expo companion with manual Tailscale HTTPS setup,
  shared SDK WebSockets, messages, questions, permissions and session creation.
  Implementation is in `apps/mobile`; completion requires the physical iOS beta
  and Android acceptance in the [mobile plan](../.ai-docs/plans/mobile-app/IMPLEMENTATION.md).
- [ ] Mobile follow-ups: authenticated device pairing/QR and push notifications,
  after the private-host companion is accepted.
- [x] Conversation row actions: shared rename, same-directory fork, archive/restore, delete, and local/SSH editor opening. See [features](features.md#conversation-row-actions).
- [x] Terminal tabs: daemon-hosted login shells beside the conversation on every host kind, drawn with ghostty-web. See the [plan](../.ai-docs/plans/terminal-tabs/README.md).
- [ ] Editing and code review product surfaces (later work).
- [ ] Hosted execution, connection authentication and relay infrastructure.

See [protocol-v2.md](protocol-v2.md) and `.ai-docs/plans/protocol-v2/README.md`
for the approved scope, implementation inventory and validation.

The accepted web scope, source-package boundaries and phase evidence live in
[the web application plan](../.ai-docs/plans/web-app/README.md). Repeatable browser
and package checks are documented in [web-app.md](web-app.md); physical-device and
assistive-technology checks remain explicit manual gates until recorded.
