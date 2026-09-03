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
