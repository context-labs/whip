# Default localhost listener

Branch: `codex/mobile-ui`

## Goal

Enable the daemon's localhost HTTP/WebSocket listener on ordinary startup.
`WHIP_NETWORK=0` disables it even with an explicit `WHIP_LISTEN`. The user's
2026-09-11 request authorizes this implementation.

## Design and non-goals

Change the existing environment resolver in `cmd/whip/daemon.go`; use existing
listener lifecycle and environment inheritance for explicit/automatic startup.
Preserve distribution-specific names (`WHIPCODE_` for whipcode), automatic
loopback port selection, exact Host/Origin checks and disabled network terminals.
No new dependencies, concurrency, persistence, wildcard access, automatic
Tailscale configuration or changes to the user's running daemon.

For a persistent proxy, the existing `WHIP_LISTEN=127.0.0.1:9876` supplies a
fixed port. Automatic exact Tailscale hostname/origin configuration is a proposed
follow-up, not implemented here. These headers protect browser access, not
against arbitrary native clients; remote access still requires a trusted network.

## Prior art

- Existing listener and guards: `internal/daemon/network.go`, `network_server.go`.
- Existing explicit transport setup: `docs/mobile.md`, `docs/web-app.md`.
- [Microsoft UFO advisory](https://github.com/microsoft/UFO/security/advisories/GHSA-vf4c-mf32-gf2h)
  demonstrates why a loopback agent still needs Host/Origin protection against
  malicious browser pages and DNS rebinding.

## Tasks and validation

- [x] Default the resolver to enabled; retain explicit boolean overrides.
- [x] Extend environment tests for default startup and zero overriding a bind.
- [x] Extend daemon startup acceptance to check loopback discovery, native and
  same-origin HTTP requests, and rejection of foreign hosts/origins.
- [x] Update CLI guidance, README, web guide, features and roadmap.
- [x] Run focused daemon/CLI tests and `task check` (both passed, 2026-09-11).
- [x] Adversarial review: no actionable correctness or simplicity findings.
- [x] Commit this phase.

Validation log: `/tmp/whip-default-network-check.log`. Full `task check` passed
formatting, vet, whipvet, Go tests, generated protocol checks, SDK/web checks,
local update tests and the onboarding Docker workflow tests. The focused run
covered `TestDaemonNetworkEnvironment`,
`TestRunDaemonPublishesProtocolAndStopsCleanly`, `TestNetwork*` and `TestWeb*`.

Existing `internal/daemon/network_test.go` covers rejected WebSocket upgrades,
disabled listeners and exact custom origins. No live daemon restart is needed
for these isolated tests.
