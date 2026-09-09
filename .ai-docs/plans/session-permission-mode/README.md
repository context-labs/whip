# Persistent session permission mode

Branch: `codex/desktop-release`

## Goal

Save each root session's Ask/Full Access choice and restore it before resumed
work or retained children can run. The user approved this design after the
research in this task. New and legacy sessions default to Ask. A new fork is a
new session and retains the existing Ask default.

## Design

- Add a checked `sessions.permission_mode` column with an in-place v12 → v13
  migration; retain the v10/v11 upgrade chain and runtime identity.
- Keep the existing top-level `RootSnapshot.permission_mode` wire field and
  `permission.mode` command. No second client state or protocol shape is needed.
- Commit the session value and update event together, then apply the live mode
  inside the existing root actor. Read snapshots from the durable session row.
- Restore mode in `Daemon.open` before binding the root/runtime and children.
  Keep headless/deny execution policies independent of the saved consent choice.
- ACP reads the mode when loading/creating and follows other clients' updates.
  TUI launch flags explicitly update the initial session; reconnect/switch does
  not reapply a client-owned mode.

## Scope and prior research

Storage: `internal/session/{migrations,event,permission_mode}.go` and tests.
Runtime: `internal/daemon/{daemon,client_control,session,view}.go` and tests.
Clients: `internal/acp`, `internal/tui`, CLI help. Documentation:
`docs/{frontend,features,protocol-v2}.md`.

Current implementation: `internal/daemon/client_control.go` only changes runner
state; `cmd/whip/daemon.go:daemonToolServices` initializes prompts on every
construction. `internal/acp/bridge.go:LoadSession` resets Ask; the TUI's
`yoloCommand` reapplies automatic mode on reconnect. The isolated research test
proved Full Access was lost on daemon restart while permission rules persisted.
This is a correction to the existing feature, not a port from another harness.

No new permission levels, capability changes, permission UI, or dependencies.

## Tasks and validation

- [x] Durable storage, migration, atomic event, authoritative snapshots.
- [x] Restore before root/child work; preserve live reload policy.
- [x] ACP and terminal attachment behavior and regression tests.
- [x] Restart both modes, independent sessions, retry, storage failure,
  migration/reopen and retained child behavior.
- [x] Targeted Go tests/race checks; existing picker/SDK tests; `task check`.
- [x] Review correctness and simplicity; update canonical docs.

## Implementation notes

- The saved choice remains a top-level snapshot field, read directly from the
  session row; `Meta` and generated protocol shapes stay unchanged.
- Migration publishes the Ask default for legacy roots so clients reconnecting
  with a retained event cursor cannot display a stale pre-upgrade live mode.
- New regression tests exercise actual file-write approval after a full daemon
  and store restart, stale command replay, independent roots and child input
  queued before runtime binding. Focused daemon tests pass with race detection.
- ACP/TUI race tests and CLI tests pass. Store migration and failure tests pass
  with race detection. The protocol generation drift check passes.
- Full-suite validation exposed test fixtures that relied on implicit approval;
  tool fixtures now opt into automatic mode explicitly, and MCP gate fixtures
  retain their separate native/remembered-consent setup.

## Final validation

- `task check` passed: formatting, vet, whipvet, all Go packages, generated
  contract checks, SDK/example checks, web production build, app/UI tests,
  theme drift and artifact packaging tests.
- `go test ./internal/daemon -count=1 -timeout=90s` passed.
- Permission/restart, MCP reload and cross-transport daemon tests passed with
  `-race`; ACP/TUI package race tests passed.
- Final permission/migration tests passed with `-race`, including rollback,
  v10/v11/v12 upgrades, replay from a pre-upgrade cursor and an event log at the
  retention boundary. Explicit numbered SQLite parameters preserve all retained
  history when the migration inserts its mode event.
- Final independent review found no remaining actionable issues; diff whitespace
  check passed. No running user daemon was restarted or user database opened.
