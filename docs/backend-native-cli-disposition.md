# Native CLI retirement

This slice removes the unreachable retained CLI daemon, client connector and
separate gateway subprocess implementation. Public commands already use the
native runtime. `_daemon` and `_web-gateway` now fail before initialization or
prompt admission. They are not compatibility routes. `_kernel` remains the
shared authority-free worker for Starlark and QuickJS; its descriptor no longer
includes a digest of the retired RLM prompt guide.

The default and integration-tagged `cmd/whip` source no longer imports
`internal/daemon`, `internal/legacy`, `internal/agent`, `internal/agentdef`,
`internal/rlm`, `internal/llm`, `internal/tools` or `internal/webgateway`. The terminal package
contained retained code at this checkpoint. The later [terminal retirement](native-terminal-retirement.md) and [core retirement](backend-native-core-retirement.md) record its final removal.
No installed executable, live runtime or original development checkout is used.

## Retained scenarios and replacement evidence

| Removed fixture family | Native replacement or intentional contract change |
| --- | --- |
| Legacy daemon boot, readiness, clean stop and checkpoint restart | `internal/hostcmd/lifecycle_test.go`, `internal/localruntime/local_test.go`, `cmd/whip/native_cli_integration_test.go` and `update_native_test.go` exercise actual readiness, runtime identity, process epochs, shutdown and restart. |
| Legacy RLM-only daemon factory | The compiled CLI integration runs real Starlark and QuickJS workers through the native host, reads their bundled descriptors and verifies recovery does not resubmit the completed input. Runtime engine/binding tests cover captured modules, limits and checkpoints. |
| Legacy provider prices, selected/default route, helper selection and subscription limits | `internal/providerhost/{host,models,catalog}_test.go` and `internal/hostcmd/{provider,execution_defaults,subscription}_test.go` cover exact nullable prices, captured route/default selection, explicit compaction selection, subscription ceilings and credential lifetime. The native ACP subscription test remains. |
| Legacy eager browser/computer service wiring and screenshot conversion | Native runtime browser/computer tests cover explicit grants, host policy, revocation, normalized owner-scoped image results, both-engine provider hydration and worker eviction. Bindings/builtins tests enforce definition module ceilings. An eager legacy service pointer is no longer a capability boundary. |
| Legacy network environment parser | `daemon_network_test.go` now checks the actual native launch arguments, explicit enablement, invalid booleans and preserved host/origin lists. `daemon_manage_test.go` checks terminal enablement and disabled-network behavior. |
| Legacy unsafe startup and desktop status printer | `desktop_runtime_sync_test.go` now exercises native flag/network validation, missing home, a file in place of a runtime directory, maintenance exclusion and an unusable native database. Diagnostics inspect an actual unsafe directory and cannot report an unverified PID. |
| Legacy force-stop PID signaling | Native stop deliberately requires verified runtime/epoch identity even with `--force`; `daemon_manage_test.go`, `update_native_test.go` and `localruntime/local_test.go` cover stale sockets, locked unresponsive owners, foreign epochs and refusal to signal an unverifiable process. Old PID-only TERM/KILL behavior is retired. |
| Separate gateway child pipes, readiness records and parent-death helpers | The native host owns its gateway and joins it. `internal/localruntime/local_test.go` covers network readiness/failure isolation; `internal/hostcmd/lifecycle_test.go` rejects network shutdown authority; `internal/gateway` tests cover native handshakes and connection ownership. The separate `_web-gateway` subprocess, its generation records and restart protocol are retired. |
| Foreground gateway serving and asset discovery | `cmd/whip/web_test.go` checks compatible native discovery, absent assets, invalid origins/URLs and no implicit runtime launch. Existing actual browser/distribution acceptance exercises packaged assets and native endpoint lifetime. |
| Unused legacy CLI client fixture/store helpers | Semantic references show no supported caller. All command fixtures already use the native socket and typed Go client. The obsolete helpers are deleted with the implementation they exercised. |
| Compiled desktop update using legacy tables and clients | `desktop_runtime_sync_integration_test.go` now builds two disposable native binaries. It preserves approval, byte hashes, stable runtime identity, new process identity, ordinary session creation, existing history, a future schedule, an unconfigured model selection, unchanged native/retired configuration, retry idempotency and desktop-owned update routing. |

The old update fixture expected a history read to resume a worker and fail on an
unconfigured model. Native reads must remain available without starting work.
The replacement explicitly reads that owner's preserved history and schedule
through the public native client after replacement. It does not invent a second
worker-restore log or silently reset the missing selection.

## Verification

Semantic caller checks preceded deletion. The selected native CLI/desktop
baseline passed under race detection in 8.132s; the full CLI suite after deletion
passed shuffled race detection in 73.225s, followed by vet and clean semantic
diagnostics. Compiled native replacement passed in 22.67s and both real engines
passed in 11.03s (36.573s for the race-enabled integration run). The initial
replacement fixture incorrectly used an immediately due repeating schedule for
a future-only preservation assertion; the final fixture explicitly schedules
the occurrence 24 hours ahead. Final Linux/macOS hosted validation and the
remaining terminal/core removal are separate requirements.
