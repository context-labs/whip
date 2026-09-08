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
- [x] Attach-only TypeScript SDK, durable command handles, bounded synchronized
  views, scoped content, permission helpers and minimal React example.
- [x] Trusted-client approvals: no enrollment, signer or first-run pairing prompt.
  Protocol v3 preserves permission decisions, rules and internal agent authority.
- [x] React web implementation with separate UI/app/web source packages, TanStack
  application primitives, Base UI controls and extracted StyleX styles.
- [x] All 66 TUI themes, automatic appearance, custom-theme resolution and themed
  read-only code, with deterministic generation and component contrast checks.
- [x] Packaged browser assets and explicit `whip web` attachment/launch command.
- [x] Session tabs with window-local restoration, preserved drafts/reading position,
  bounded background activity, and responsive themed navigation. See the
  [session-tabs implementation and acceptance](../.ai-docs/plans/session-tabs/README.md).
- [x] Nested in-window split panes with movable tabs, duplicate chat views, independent
  reading/agent selection and bounded observation. See the
  [split-view implementation](../.ai-docs/plans/split-views/README.md).
- [x] Read-only session REPL notebook with tab-mode switching, independent split
  views and bounded live/recorded execution evidence. See the
  [session REPL implementation](../.ai-docs/plans/session-repl-viewer/README.md).
- [ ] Complete the web application's release acceptance: full workflow/recovery
  matrix, actual mobile devices, VoiceOver/keyboard review and documented
  performance gates. Implementation does not by itself complete this milestone.
- [ ] Electron desktop release acceptance — implementation and signed local packages are available; notarized installation, real updates and manual device gates remain. See [desktop guide](desktop.md) and [verified progress](../.ai-docs/plans/desktop-app/progress.md).
- [ ] Editing, code review and standalone terminal product surfaces (later work).
- [ ] Hosted execution, connection authentication and relay infrastructure.

See [protocol-v2.md](protocol-v2.md) and `.ai-docs/plans/protocol-v2/README.md`
for the approved scope, implementation inventory and validation.

The accepted web scope, source-package boundaries and phase evidence live in
[the web application plan](../.ai-docs/plans/web-app/README.md). Repeatable browser
and package checks are documented in [web-app.md](web-app.md); physical-device and
assistive-technology checks remain explicit manual gates until recorded.
