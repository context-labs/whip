# Live MCP refresh and reconnect

Branch: whip-rlm

## Goal
Make newly configured MCP servers usable in the current conversation without rebuilding its runtime. Expose root-agent `mcp.refresh()` and `mcp.reconnect(server=...)`; refresh the current session after UI import. User approved the proposed additive design and requested end-to-end implementation.

## Non-goals
No config-editing agent authority, global session synchronization, automatic replay of calls, re-enabling disabled servers, or replacement of existing connections during refresh. Child grants remain snapshots. No new dependencies.

## Design and existing foundations
- Reuse `internal/mcp/manager.go` AddServers/Reconnect and current configuration selection (`internal/mcp/config.go`); retain healthy and disabled entries. Report config changes as needing explicit update.
- `internal/daemon/mcp.go` owns session refresh; fresh config must use the same loader and definition filter as startup (`cmd/whip/daemon.go`). Surface `mcp.refresh` through session control and recursive host.
- `internal/daemon/recursive_runtime.go` and `internal/rlm` expose root-only MCP recovery operations without broadening child grants or bypassing per-call authorization. New tools must be immediately discoverable.
- Existing MCP import UI (`packages/app/src/mcp-import.tsx`) persists host config; refresh only a current session belonging to that host after successful import, and distinguish persisted imports from refresh failures. SDK/protocol expose the shared action.
- Existing patterns are documented in docs/features.md MCP section and prior plans mcp-contracts, mcp-discovery, mcp-import-onboarding; this extends Whip rather than porting an external harness.
- Runtime changes are intentionally live-only; rebuilt runtimes retain current startup semantics. Existing manager owns connection goroutine lifecycles.

## Tasks
- [x] Backend additive refresh, config loader, structured outcome, session control, tests (mcp-backend; root completed rate-limited follow-up).
- [x] Runtime helpers, capability/root restrictions, guide/validation and tests (mcp-host).
- [x] SDK/protocol/frontend automatic refresh and tests (mcp-ui).
- [x] Integration review and adversarial review; both diagnostic findings fixed and regression-tested.
- [x] Focused tests, race checks, full Go and frontend checks (aggregate task formatting blocker below).
- [x] Feature documentation, workflow inventory, and roadmap.

## Validation
Prove fresh server discovery/callability in existing session, idempotent concurrent refresh, session isolation, disabled preservation, definition filtering, changed config non-replacement, imported trust consent, child grant isolation, errors/cancellation, and frontend host/session targeting and truthful partial success. Run focused suites first, then task check and relevant race suites; explicitly record environmental or pre-existing failures. Preserve unrelated worktree changes.

### Final validation (2026-09-23)
- `go vet ./...`, `go run ./cmd/whipvet ./...`, and `go test ./...`: passed, including cmd/whip, daemon, MCP, both RLM engines, and protocol.
- Focused `go test -race ./internal/mcp ./internal/daemon` covering AddServers, reconnect, refresh, attachment isolation, and recursive-host MCP: passed.
- `npm run check`: protocol 15 tests + generation drift, SDK 458 tests/build, and client example build passed.
- `npm run test:web`: 1,240 tests across 93 files passed on final rerun. An earlier run failed the separately modified REPL-copy test; its isolated run and the final full run passed without changing it.
- `npm run check:web`: app TypeScript and production web build passed.
- `task check`: model metadata check passed, then formatting traversal stopped on `.claude/worktrees/session-trace/internal/daemon/session.go` and `internal/session/otlp_export.go` in that nested worktree. Those unrelated files were not modified. Aggregate downstream tasks were not all run; the explicit checks above are the verified results.
- Focused formatting and repository diff whitespace checks passed. No staging or commits.

### Recovery follow-up
All four subagents hit the provider subscription rate limit. Root checked their status, attempted one resume, then completed remaining work locally when it failed again. Fixed disabled-reconnect test expectations, retained the corrected `greet` fixture, and increased the CLI engine/resume fixture token limit from 10,000 to 20,000: the enlarged guide reserves ~5,034 tokens on its first mock request (the provider omits usage), leaving insufficient room for the second request under the old fixture limit. Production accounting is unchanged.

Fresh discovery now replaces stale blocked/source-error diagnostics while retaining attachment refusals. Reconnect refusals for known invalid/closed servers no longer report an unknown name. Regression tests cover both review findings.

