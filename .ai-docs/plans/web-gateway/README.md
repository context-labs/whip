# One web gateway, outside the daemon

Branch: `whip-rlm` (existing worktree; no new branch created)
Status: Phases 1–4 implemented following explicit user authorization. Automated gateway acceptance and adversarial review passed; Phase 5 release sign-off remains open for the validation exceptions below.
Date: 2026-09-23

## Goal

Replace the daemon's in-process HTTP/WebSocket server with one gateway implementation that runs as a separate process above the daemon protocol. Ordinary daemon and desktop startup must not open a web listener. `whip web` runs the gateway in the foreground; `WHIP_NETWORK=1` optionally launches the same gateway as a daemon-managed child.

This replaces, rather than supplements, the existing serving path. Separate launch modes are acceptable; separate web-serving implementations are not.

The user requested this plan after reviewing the [original proposal](http://localhost:8080/h/2e87357c8b0602d385877a718cff0b6c/s/n326aqbqzae7w7yqgq6q) (final proposal: message 143). That proposal intentionally retained the in-daemon listener. This plan supersedes that decision and the default-on behavior recorded in [the historical listener plan](../default-network-listener/README.md).

## Non-goals

- No second backend, session store, execution engine, or HTTP business API.
- No Node runtime, separately installed gateway binary, new frontend build, or new dependency unless implementation proves one necessary.
- No authentication/pairing, TLS termination, Tailscale automation, arbitrary upstream relay, or Internet-safe hosting claim. Remote access still requires a trusted network or authenticated proxy.
- No general-purpose process supervisor, gateway registry, automatic crash-restart loop, or new persistent service database.
- No automatic replacement/restart of a running daemon to make a new gateway work.
- No frontend redesign or changes to desktop's local IPC/Unix-socket data path.
- No changes to users' running daemons or home directories during implementation/testing. Use isolated fixtures.

## End state and ownership

```text
Browser / mobile / remote SDK
              |
           HTTP / WS
              |
       web gateway process
              |
    existing Unix-socket protocol
              |
            daemon <--- Unix socket --- desktop / local CLI
              |
     execution, policy, storage
```

The executable can still embed the production frontend assets. The process boundary, not how assets are packaged, is what changes.

| Owner | Responsibilities |
| --- | --- |
| Daemon | Runtime/store singleton; sessions and execution; protocol operations; content grants and uploads; connection-scoped policy, including network-terminal restrictions. No HTTP listener. |
| Gateway | Static assets, `/api/v3/web`, `/api/v3/ws`, `/api/v3/content/`; exact Host/Origin checks; bounded HTTP/WS-to-RPC adaptation; its own connections and listener. No direct store access. |
| CLI startup orchestration | Launch configuration, optional gateway child ownership, readiness/status reporting, and shutdown. The execution engine does not become a supervisor. |
| Desktop | Existing native transport. Default startup does not require or start a gateway. |

### Proposed behavior contract

These are the recommended defaults for implementation, not claims about current behavior.

| Action/configuration | Result |
| --- | --- |
| `whip daemon start`, desktop startup, or automatic local daemon startup | Start the daemon only, unless managed web startup is explicitly enabled. |
| `whip web` | Require a compatible running daemon; start a foreground gateway; print its ready URL, open the browser, and wait. Never restart/reconfigure the daemon. |
| `whip web --no-open` | Same foreground lifecycle, but do not open a browser. Document that this no longer merely prints a URL and exits. |
| `whip web --url <origin>` | Explicit open/check-existing-endpoint mode; never start another server. Retain URL validation and `--no-open`. Do not require an in-daemon listener. |
| `WHIP_NETWORK=1 whip daemon start` | Once the daemon is ready, launch the same gateway implementation as a managed child, without opening a browser. Honor this opt-in for other daemon launch paths too. |
| `WHIP_NETWORK=0` or unset | Do not auto-launch a gateway. This does not prohibit an explicit foreground `whip web`. |
| `WHIP_LISTEN` | Configure the gateway address; on its own, do not opt ordinary daemon startup into networking. |
| No explicit listen address | Try `127.0.0.1:4444`; only on address-in-use, fall back to `127.0.0.1:0`. Report the actual address. |
| Explicit listen address, including port `0` | Bind exactly as requested; no silent fallback for a failed fixed address. |
| `WHIP_ALLOWED_HOSTS` / `WHIP_ALLOWED_ORIGINS` | Apply existing exact-match security rules to the gateway. Do not broaden defaults. |
| `WHIP_NETWORK_TERMINALS` | Remains daemon-owned authorization for network-origin connections, independent of whether a managed gateway is enabled. |

Apply the same behavior to distribution-specific `WHIPCODE_*` names through the existing buildinfo mechanism.

`whip web` always owns its foreground instance. If a managed gateway already exists, do not silently attach to it and imply Ctrl+C owns it. The foreground command may run an independent instance using the normal port rules; `--url` is the explicit way to open an existing instance. This permits multiple explicitly requested processes, not multiple implementations. No new singleton lock or instance registry is needed.

Starting an already-running daemon retains its existing launch configuration; changing an environment variable is not live reconfiguration. An explicit daemon restart can apply changed managed-start settings, with the existing warning that restarting interrupts work. A foreground gateway never requires that restart solely to enable HTTP.

## Pre-migration baseline and relevant prior art

This section records the starting point, not the current serving architecture. The daemon network files referenced here have now been deleted; see the validation record below and canonical docs for the implemented behavior.

- [`cmd/whip/daemon.go`](../../../cmd/whip/daemon.go): `daemonNetworkEnvironment` currently defaults networking on and resolves distribution-specific settings.
- [`cmd/whip/web.go`](../../../cmd/whip/web.go): `whip web` currently discovers an existing daemon endpoint and opens it; it does not own a listener.
- [`internal/daemon/network.go`](../../../internal/daemon/network.go), [`network_server.go`](../../../internal/daemon/network_server.go): existing routes, Host/Origin rules, WebSocket admission, endpoint publication, and HTTP lifecycle. A serving failure currently cancels the daemon server.
- [`internal/daemon/transport.go`](../../../internal/daemon/transport.go): existing bounded Unix/WebSocket framing. WebSockets carry JSON text messages; Unix transport supplies newline framing. This is not an unframed `io.Copy` of socket bytes.
- [`internal/daemon/server.go`](../../../internal/daemon/server.go), [`terminal_rpc.go`](../../../internal/daemon/terminal_rpc.go): local socket connections currently enter as non-network clients; network terminal policy depends on that classification.
- [`internal/daemon/client.go`](../../../internal/daemon/client.go), [`content_client.go`](../../../internal/daemon/content_client.go), [`network_content.go`](../../../internal/daemon/network_content.go): upload/read RPCs exist. The high-level client owns initialization/read loops, and `Client.Upload` accepts an entire byte slice; neither is a drop-in streaming bridge.
- [`internal/webassets/assets.go`](../../../internal/webassets/assets.go): reusable embedded asset serving and response security headers.
- [`docs/frontend.md`](../../../docs/frontend.md), [`docs/features.md`](../../../docs/features.md), [`docs/roadmap.md`](../../../docs/roadmap.md), [`docs/web-app.md`](../../../docs/web-app.md): current architecture and shipped behavior. Update the canonical docs when behavior lands, not to describe this proposal as already shipped.

The roadmap records packaged web access, the default-on listener, and shared socket/WebSocket protocol as shipped. This work revises their deployment boundary; it is not a new frontend or protocol replacement. This is an extraction of existing WHIP behavior, not a port from another harness; no external harness research is required.

## Phase 1 — Establish a safe protocol boundary

**Outcome:** A gateway can connect through the local socket without granting browser traffic local-client privileges. No public serving behavior changes yet.

- [x] Add a restrict-only connection mode to initialization, acknowledged by the daemon. Proposed mechanism: an optional network-origin marker plus an explicit acknowledged mode/capability in the initialization result. Final field names follow existing protocol conventions.
- [x] The daemon can only narrow a connection's policy through this marker; an already-network connection cannot request local privileges. Classification is immutable after initialization and is independent of client-provided `client_kind` or client ID.
- [x] Gateway-originated initialization must force the restricted mode, even if the browser omits or tampers with it. Preserve other initialization fields, request IDs, provider advertisements, and requested capabilities.
- [x] Require the acknowledgement before forwarding subsequent browser requests. Old daemons must fail closed with an actionable compatibility message, never fall back to an ordinary trusted socket. Bound or reject pre-initialization/pipelined traffic.
- [x] Keep terminal authorization in the daemon; separate that policy setting from the soon-to-be-removed HTTP listener options.
- [x] Add generated protocol/schema changes and compatibility tests if the selected wire shape requires them. Existing local desktop/CLI clients must continue to initialize unchanged.
- [x] Pin preservation of connection IDs, subscriptions, uploads, and provider ownership in regression tests before introducing the relay.

**Likely files:** `internal/protocol/types.go` and generated protocol outputs; `internal/daemon/{server,terminal_rpc}.go`; `cmd/whip/daemon.go`; associated protocol/daemon tests.

**Exit gate:** A marked socket client has the same network-terminal restrictions as today's WebSocket client; an ordinary local client retains existing behavior; requested privilege downgrades stick; unsupported servers and spoofed classifications fail safely.

**Dependency:** Must land before any externally reachable gateway. This same-user local socket already trusts local clients; the new marker narrows that trust and is not a new authentication mechanism.

## Phase 2 — Extract and prove the single gateway

**Outcome:** One reusable HTTP/WS gateway implementation runs against a socket-only daemon in isolated tests. The public command/default remains unchanged until cutover.

- [x] Introduce `internal/webgateway` as the HTTP-facing owner. Move, do not redesign, the asset/discovery routing, Host/Origin enforcement, upgrade checks, and content headers.
- [x] Reuse framing and client primitives without an import cycle. Keep the gateway core dependent on protocol types and narrow transport/RPC interfaces; wire the existing concrete daemon client from `cmd/whip`. Move the existing framing implementation into a small shared internal package only where necessary. Do not build a second RPC client or broad backend abstraction.
- [x] No temporary adapter was needed: extraction and public cutover were completed together in this worktree, with the old serving path deleted. There is no second implementation to maintain.
- [x] Give each browser WebSocket its own upstream protocol connection. Bridge bounded messages, not raw WS bytes; do not splice through an already-initialized `daemon.Client`.
- [x] Apply Phase 1's restricted handshake to every forwarded browser connection and gateway-owned content RPC client.
- [x] Implement HTTP content transfers with existing `upload.begin/chunk/finish` and `content.read` RPCs. Preserve request-scoped upload ownership, chunked memory bounds, size/digest validation, content grant checks on each read, interruption cleanup, transfer limits, and safe attachment response headers. No direct `uploadManager` or store access.
- [x] Preserve existing HTTP paths and response contracts for browser, mobile, and SDK consumers. Preserve `/api/v3/web` asset-availability behavior, CSP, deep-link fallback, and hashed-asset caching.
- [x] Bound admission, handshake deadlines, message sizes, outbound backpressure, HTTP transfer concurrency, and shutdown. Explicitly close upgraded/hijacked WebSockets; HTTP shutdown alone does not own them.
- [x] Keep all gateway connections pinned to the selected runtime ID/generation. Do not silently reconnect a surviving gateway to a replacement daemon.

**Likely files:** new `internal/webgateway` package/tests; moved framing helpers if needed; existing daemon network/content/transport files; `internal/webassets` tests; a small CLI-side protocol adapter.

**Exit gate:** Production assets and all API routes work against a daemon with no TCP listener. Two browsers remain independent; streaming, reconnect/replay, questions/permissions, uploads/downloads, and terminal policy pass through unchanged. Closing a browser/gateway does not cancel accepted runtime work.

## Phase 3 — Implement foreground and managed process ownership

**Outcome:** Both launch modes use Phase 2's gateway with deterministic readiness, discovery, failure reporting, and teardown. Exercise them through internal entrypoints/tests before switching defaults.

- [x] Add one gateway run path and a private same-executable entrypoint for managed startup. No browser opening, daemon autostart, or recursive `whip web` behavior in the child entrypoint.
- [x] Foreground startup verifies daemon compatibility and packaged assets, binds the configured address, reports the actual ready URL, optionally opens the browser, and waits for SIGINT/SIGTERM. Failure to open a browser leaves a usable running gateway and prints the URL.
- [x] Managed startup runs only after the selected daemon socket is ready and launches at most one owned gateway child per daemon generation. Use the same selected executable/home; do not resolve a possibly different installation through PATH.
- [x] Use bounded child-to-parent readiness/error reporting. Identify the runtime/generation and actual bound endpoint. Distinguish daemon readiness from gateway readiness so a web failure cannot strand daemon startup waits.
- [x] Keep managed endpoint/state in memory with the owning daemon generation, not a new persistent endpoint registry. Preserve `network_endpoint` compatibility for a ready managed endpoint where feasible; clear it when the child exits. Add separately identifiable web starting/ready/failed state and errors to status as needed, without changing a healthy daemon to unhealthy.
- [x] Foreground gateway instances print their own endpoints; they do not race to overwrite the managed endpoint in daemon status.
- [x] Managed child stdout/stderr follow the existing daemon log location with identifiable gateway messages. Do not log credentials or content bodies.
- [x] Model lifetime explicitly: the parent owns the child process and reaps it; the gateway owns its listener, upgraded sockets, RPC connections, and relay workers. Parent cancellation closes a lifetime pipe, requests shutdown, and escalates to a kill after a bounded grace period.
- [x] Parent death must close that lifetime channel even without graceful shutdown. Prevent descendants from retaining its file descriptors. A held backend connection/runtime check also makes foreground and managed gateways exit on daemon loss, rather than attach to a different generation. Individual browser disconnections are not daemon loss.
- [x] No automatic child restart loop in the first version. Report failure and keep the daemon alive. A foreground `whip web` is the non-disruptive recovery option; an explicit daemon restart can relaunch managed mode.
- [x] For an explicit managed-start request whose gateway fails, report both outcomes clearly, return an actionable unsuccessful managed-start result, and leave the running daemon available. Callers, particularly desktop, must not mistake this for a dead daemon or spawn another one.

**Likely files:** `cmd/whip/{web,daemon,daemon_manage,main}.go`, a focused gateway process helper/test file, existing daemon launcher/status seams, and desktop launch/status parsing/tests where needed. Extend current lifecycle patterns rather than introducing a service framework.

**Exit gate:** SIGINT stops a foreground gateway but not daemon work. Managed gateway crash/bind failure leaves the daemon usable and status truthful. Daemon stop/restart/abrupt death leaves no owned child or listening port. Tests cover readiness races, stalled children, wrong-generation readiness, and repeated start attempts.

## Phase 4 — Cut over all entrypoints and delete the old server

**Outcome:** The gateway becomes the only HTTP/WebSocket serving path. This is the externally visible behavior change.

- [x] Switch `whip web` and `--no-open` to the foreground behavior in the contract above. Keep `--url` as an explicit open-existing-endpoint path. Replace errors that incorrectly require a daemon restart merely to enable web access.
- [x] Default managed networking off. Route explicit `NETWORK=1` through Phase 3 for explicit, automatic, desktop, and binary-replacement daemon launches. Apply distribution-specific environment names consistently.
- [x] Implement and test implicit-4444 fallback, explicit-address failure, and `NETWORK=0`/unset semantics. A `LISTEN` value alone must not enable automatic serving.
- [x] Remove `Server.startNetwork`, the daemon-owned HTTP server/listener lifecycle, direct HTTP content/store plumbing, and the temporary in-process handler adapter. Move any still-shared response-writing helper instead of deleting live protocol code.
- [x] Remove the daemon package's dependency on web assets/HTTP server construction. Keep network-origin authorization, core protocol connection limits, and content RPCs.
- [x] Update discovery/initialization/status consumers to the managed-endpoint meaning. Adapt tests that previously obtained a server-owned WebSocket endpoint to start a real gateway fixture.
- [x] Update development proxy, smoke scripts, SDK acceptance fixtures, and packaging commands that assumed `whip daemon start` opened TCP. Vite remains a development-only frontend server, not a second production gateway.
- [x] Update current canonical docs in this same phase; do not leave the default-on behavior documented until a later cleanup release.

**Likely files:** daemon network/server/options files and tests; CLI web/daemon/manage tests; `internal/daemon/v2_*` integration fixtures; desktop runtime tests; web dev/acceptance scripts; SDK README/examples; affected build scripts.

**Exit gate:** Under every supported launch mode the daemon process owns zero HTTP listeners. Desktop/local CLI still operate with no gateway. Browser/mobile/proxy access runs through the same gateway implementation. No configuration flag revives the old in-process path, and no released build requires maintaining both paths.

## Phase 5 — Compatibility, failure acceptance, and release

**Outcome:** The complete cutover is validated and its breaking behavior changes are documented. This phase adds confidence and release evidence, not deferred architectural cleanup.

- [ ] Exercise one production-bundle browser flow with a socket-only daemon: create a session, stream a turn, approve a request, upload/read an attachment, reconnect and recover history, and refresh a deep link.
- [ ] Verify desktop and TUI continue observing/running the same session while the gateway is interrupted or killed. Verify no implicit gateway on default desktop startup, including when 4444 is occupied.
- [ ] Run SDK HTTP/WS and mobile/reverse-proxy configuration smoke checks through the gateway. Retain exact origin/host rejection, native origin-less access where currently supported, and the warning that these checks are not authentication.
- [ ] Verify upgrade combinations: old local clients against the new daemon; new gateway against an unsupported old daemon; old compatible browser/mobile clients against the new gateway. Fail with explicit upgrade guidance rather than silently broadening trust or restarting a daemon.
- [ ] Test multiple homes, concurrent foreground instances, explicit port conflicts, managed+foreground coexistence, daemon generation changes, gateway startup timeout, parent SIGKILL, and SIGINT during active upload/streaming.
- [ ] Run focused Go tests/race checks per phase; generated protocol/SDK checks when their contracts change; desktop and web checks where affected. At the release gate, run `task check` and the repository-required race validation. Record actual commands/results, including opt-in integration fixtures; do not imply skipped device checks passed.
- [x] Do an adversarial review focused on permission classification, admission limits, orphan processes, stale endpoint discovery, and deletion completeness.
- [x] Record evidence and any deviations in this plan. Do not check off implementation tasks based only on unit coverage or a successful build.

**Docs to update:** `README.md`, `docs/{frontend,features,roadmap,web-app,desktop,mobile,protocol-v2,setup}.md` where applicable; `packages/sdk/README.md`; CLI help/errors; release notes. Explain changed default networking, foreground `--no-open`, explicit `--url`, `NETWORK` as managed-start opt-in, `LISTEN` behavior, gateway status/logs, and mixed-version upgrade guidance. Update the feature map with behavior → code → tests and replace/supersede the roadmap's default-on listener item.

**Exit gate:** All mandatory checks pass; unperformed manual acceptance is identified explicitly; user-facing docs describe only the new architecture; the final diff contains no legacy in-daemon server.

## Suggested change/release sequence

1. Phase 1: independently reviewable, additive trust-boundary change with tests.
2. Phase 2: extraction plus gateway implementation and isolated acceptance; no new public serving mode yet.
3. Phase 3: lifecycle/status integration behind internal entrypoints, with process tests.
4. Phase 4: one public cutover, docs update, and old-path deletion together.
5. Phase 5: release acceptance and recorded evidence.

Phases 1–3 may be separate preparatory changes. Do not release the new public gateway behavior before Phase 4 is complete. Rollback is an explicit version rollback, not a permanent runtime toggle for the retired web server. Never restart the user's active daemon as part of rollout validation without separate authorization.

## Final acceptance checklist

- [x] Exactly one production gateway implementation and no HTTP-serving daemon path.
- [x] No web listener on ordinary daemon/desktop startup.
- [x] Foreground `whip web` owns its lifetime; Ctrl+C leaves runtime work intact.
- [x] `NETWORK=1` launches that same gateway with bounded managed-child ownership.
- [x] Default port 4444 falls back only when occupied; explicit binds do not silently move.
- [x] Network-origin privileges cannot become local privileges through the socket hop.
- [x] Content, subscriptions, provider ownership, and disconnect semantics remain correct.
- [x] Gateway failure does not stop the daemon; daemon exit leaves no orphaned gateway.
- [x] Discovery never advertises a dead/wrong-generation managed endpoint.
- [ ] Complete the remaining live-model, native-device, and deployed-proxy checks listed below; automated browser, desktop runtime, local CLI, SDK, and HTTP admission checks are recorded.
- [x] Canonical docs, help, generated contracts, and release notes match the new behavior.


## Implementation and validation record — 2026-09-23

### Implemented boundary

- `internal/webgateway` owns HTTP admission, assets, per-browser WebSocket relays,
  and bounded HTTP-to-content-RPC transfers. It does not import `internal/daemon`.
- `internal/protocoltransport` is the one shared framing implementation. The
  daemon's `network.go`, `network_server.go`, and `network_content.go` serving
  implementation is removed; content storage/grants remain behind daemon RPCs.
- Initialization forces and acknowledges `network_restricted` on every gateway
  connection. Local-client identity is not a privilege bypass; unsupported
  daemons fail closed. Connections remain pinned to the selected runtime.
- CLI foreground and managed-child modes share the same gateway. Default daemon
  startup is socket-only. Managed state/readiness belongs to the daemon generation;
  foreground endpoints do not overwrite it. Parent EOF, signals, crashes,
  readiness timeouts, and backend loss have bounded cleanup.
- SDK, daemon, and integration-tag TUI fixtures now use the actual gateway.
  Desktop readiness, development proxy, local installer, and Docker onboarding
  assumptions are updated. Canonical documentation and generated protocol
  contracts describe the new behavior.

### Passing validation

All runtime tests used disposable homes or in-process fixtures. The user's active
runtime was not restarted or reconfigured. No commit or release was made.

- `go vet ./... && go run ./cmd/whipvet ./... && go test ./...` — passed.
- Repository race gate:
  `go list ./... | grep -v -e /internal/browser -e /internal/tui | xargs go test -race -shuffle=on`
  — passed (including CLI lifecycle and full daemon suites).
- `go test -tags=integration ./... -run '^$'` — all integration-tag packages compile.
- `go test -race -tags=integration ./internal/tui -run '^TestInteractiveSessionOverTrustedProtocol$'`
  — passed for direct Unix and real gateway WebSocket sessions.
- Repeated gateway/transport race tests (`-shuffle=on -count=5`) — passed.
  Focused daemon tests cover restricted initialization, independent provider and
  subscription ownership, content grants and interrupted upload cleanup, accepted
  work surviving gateway shutdown, and crash/recovery.
- `WHIP_SDK_RACE=1 npm run acceptance` — all 49 tests passed using the real gateway,
  including content, permissions/questions, streaming, reconnect, and process crash.
- `task contract sdk` — passed generated drift/type checks, 14 protocol interop tests,
  453 SDK tests, and both example packages (9 agent-example tests).
- `npm run test:package` — packed SDK/protocol entry points import outside the repo.
- `npm run test:web` — 89 files / 1,143 tests passed on final rerun.
- `node apps/desktop/scripts/test.mjs` — 138 passed, 10 explicitly skipped.
  `npm run check:desktop` — passed.
- Web production/type checks, UI type checks/tests, theme drift, dev-proxy tests,
  asset-packaging tests, local-update tests, and Docker onboarding/provenance tests
  passed independently of the exceptions below.
- `node scripts/web-gateway-smoke.mjs /path/to/built/whip --browser` — passed twice
  against a freshly built production binary with packaged web assets. Exercises
  real Chromium bootstrap and SDK restricted admission; socket-only defaults;
  repeated start without reconfiguration; concurrent foreground and managed
  instances; exact Host rejection; terminal denial; Ctrl+C daemon isolation;
  explicit bind failure leaving the daemon alive; managed readiness and teardown;
  and foreground cleanup on backend loss. Child waits are bounded and a forced
  kill is a test failure, not successful cleanup.

To reproduce the binary acceptance without installing or restarting the active
runtime:

```sh
task web-assets
d=$(mktemp -d /tmp/whip-gateway-build.XXXXXX)
go build -o "$d/whip" ./cmd/whip
node scripts/web-gateway-smoke.mjs "$d/whip" --browser
rm -rf "$d"
```

The browser option requires the existing Playwright Chromium installation; omit
`--browser` for the process/HTTP/SDK checks alone. The harness creates and removes
its own homes and stops only its own daemons and foreground gateways.

### Adversarial review and fixes

Independent read-only review examined privilege downgrades and old-server
rejection, handshake canonicalization/smuggling, admission/backpressure, per-client
ownership, content grants, runtime pinning, stale status, parent death, child
reaping, and deletion completeness. Its concrete P2 finding was fixed: HTTP
socket deadlines alone did not bound stalled content RPCs. Content requests now
have an explicit shared two-minute context deadline; deterministic
`testing/synctest` regressions stall both `upload.begin` and `content.read` and
verify upstream closure, deadline propagation, and admission-slot release.

Other review fixes preserve downloads larger than the upload ceiling, retry
unknown/empty gateway readiness only for newly launched runtimes, preserve the
already-running no-reconfigure contract, and derive child HOME correctly from
`runtime-v2` paths. All were covered by the final tests above.

### Release-gate exceptions / not claimed

- The final `task check` still stops at its recursive `gofmt -s -l .` gate on two
  unrelated nested-worktree files:
  `.claude/worktrees/session-trace/internal/daemon/session.go` and
  `.claude/worktrees/session-trace/internal/session/otlp_export.go`.
  They were left untouched. The Git-visible current-worktree Go formatting and
  `git diff --check` pass; remaining checks were run separately.
- `apps/web/scripts/stylex-dev.test.mjs` stalled on repeated independent runs;
  a bounded run timed out at 60 seconds and its job was stopped. No StyleX files
  were changed for this migration. This failure is unresolved, not counted as a
  passing web release gate or asserted to be a baseline defect.
- The production Chromium smoke validates UI boot and real SDK admission, not
  a credentialed live-model session with approvals and attachments in the UI.
  Those protocol workflows passed deterministic gateway SDK/daemon acceptance.
  A native desktop/TUI/browser shared live session, installed mobile hardware,
  deployed reverse proxy, and separately installed old client binaries were not
  manually exercised. Compatibility/security variants are covered by automated
  old-capability, legacy local initialization, Host/Origin, and runtime-pin tests.
- Phase 5 and the roadmap release checkbox deliberately remain open for those
  gates. This record does not claim release readiness or modify the active runtime.
