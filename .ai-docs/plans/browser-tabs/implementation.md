# Browser tabs implementation ledger

Branch: `desktop-browser-tabs`

Worktree: `/Users/samheutmaker/Desktop/context-labs/src/rlm/whip-browser-tabs`

## Authorization and baseline

- User authorized committing all outstanding worktree changes, then implementing and testing every phase of [the plan](README.md).
- User authorized testing with the connected remote host. Use isolated temporary fixtures; preserve existing services, credentials, SSH configuration and unrelated work. Clean up only processes/files/forwards created by these tests.
- Baseline commit: `e261d9b5139d85ade9837eedccb8e9bdd7aaa5f1` — 73 inherited changed/new files, including both planning documents. Staged additions were inspected for credential patterns and passed `git diff --cached --check`. Baseline tests were not asserted green at commit time.
- Original checkout and reference OpenCode checkout remain untouched.

## Phase status

| Phase | State | Evidence |
| --- | --- | --- |
| 0 — native/CDP/SSH proofs and contracts | Boundary proofs passing; integration still gated | Actual Electron production Go helper parity passes; selected-host native SSH gate reported passed on `sam@kuzco-4090` |
| 1 — secure native browser | Implemented; focused native validation passing | Manager, scoped CDP, IPC/preload, lifecycle and preview authority exercised against actual Electron |
| 2 — workspace tabs | Implemented; packaged-discovered restore seam fixed | Human tabs/overlays, explicit provider choice and SSH entry implemented; strict metadata projection regression proved red/green; full frontend 709/709 passes |
| 3 — scoped permission/provider broker | Implemented; scoped tests passing | Durable Once-only grants, selected holder/epoch, transfer/revoke and readable sanitized consent details verified; independent transport/source review completed without an additional identified authority gap; not a clean release security audit |
| 4 — agent browser control | Full development-native path passing | SDK 365/365; real WebSocket upload provenance and real SSH daemon→SDK→preload/IPC→guest screenshot/cancellation/detach pass; no replay or Mac-decoy hits |
| 5 — SSH preview environments | Selected-host native gates and packaged human IPv4 checks pass | Native expansion/transport coverage remains separate; signed Beta human deny/approve, remote marker/cookie/Host, negative port/family, exact-master loss, renewed approval and fail-closed restore pass with all six Mac decoys untouched. See [packaged SSH evidence](evidence-integration.md#consented-packaged-ssh-acceptance--2026-09-17-22342303-utc) |
| 6 — integrated lifecycle/MCP/product finish | Core transport/lifecycle integration passing; matrix incomplete | Full Go suite re-passes after final broker changes; MCP fail-closed checks, exact-connection upload, actual cancellation/detach/unbind and human-page preservation pass; remaining matrix stays gated |
| 7 — packaged acceptance/docs/rollout | Local and human SSH IPv4 acceptance pass; release gated | Signed shipping-fuse Beta: default-OFF entry/exit; consented normal-HOME local loading/input/navigation and tab/profile continuity; human SSH loading, fresh-approval recovery, fail-closed restore and both enabled quits pass. Packaged agent control and remaining manual/performance gates remain open. Final-run local/remote fixtures cleaned up without force; evidence archived and rechecked. See [root integration evidence](evidence-integration.md). Earlier keychain wait's exact cause and dependency audit remain unresolved; notarized=false. Browser tabs are now default ON by explicit user decision, with `WHIP_DESKTOP_BROWSER_TABS=0` as the launch-time opt-out; this does not close the remaining gates |

No mock, skipped test, source inspection, or development window counts as packaged acceptance. Runtime verification and unavailable hardware/manual gates must be reported separately.

## Work ownership

Initial independent lanes: native embedding/CDP proof; SSH routing proof; Go permissions/broker/automation contract proof; workspace/overlay integration proof. Shared production contracts must be agreed before parallel implementation touches their consumers. Root owns cross-lane integration, generated protocol coordination, canonical docs, final acceptance and commits.

## Initial validation

- Dependency installation: `npm ci` started in the worktree.
- Baseline Go validation: `go test ./internal/browser/... ./internal/tools/... ./internal/capability/... ./internal/session/... ./internal/rlm/... ./internal/daemon/... ./internal/mcp/... ./internal/llm/...` started.
- Toolchain observed: Node v24.14.1; Go go1.27.0 darwin/arm64.
- `npm ci` passed (1613 packages installed). Its audit reported 43 pre-existing dependency vulnerabilities: 3 low, 14 moderate, 25 high, 1 critical. Triage before release; do not run an indiscriminate force upgrade.
- The targeted baseline Go command above passed all packages on 2026-09-17 UTC. This was a normal test run, not race or packaged acceptance.
- Baseline `npm run check && npm run check:desktop && npm run test:web && npm run test:desktop` completed successfully; final desktop script suite reported 92 passed, zero failed/skipped, and startup probe self-test passed.
- [Root-approved implementation contracts](contracts.md) resolve type ownership, v3 persistence with non-destructive backup, retained over-cap restore metadata, exact Once-only browser permission policy, explicit child attachment delegation, selected-provider routing and private SSH proxy lifetime. They do not waive incomplete acceptance.
- Phase0 lane evidence: [native](evidence-native.md), [workspace](evidence-workspace.md), [agent](evidence-agent.md), [SSH/proxy](evidence-ssh.md). Actual native compositor capture passed with already-granted Screen Recording consent; the BrowserWindow capture API alone is not compositor evidence.
- User confirmed `sam@kuzco-4090`. SSH/native lanes used isolated test-owned helpers/daemon/fixtures and native master with existing verified SSH configuration; no unrelated SSH process was reused.

## Integration validation — 2026-09-17 15:10 UTC

- Root added `packages/sdk/src/browser.ts`, opt-in `desktop-browser-v1` initialization, typed command/cancel/revoked notifications, exact-root selection, native-ACK gating, no replay/rebind, same-holder scoped screenshot upload, event forwarding, and explicit epoch-bound release/unbind. `npm run check` passed (`job-2f2d1a33`), including protocol generation drift/interop, SDK **347 passed / 0 failed / 0 skipped**, and client-example build. These SDK boundary tests use scripted transports, not full native acceptance.
- Root request-card Once-only defense and separate agent-control/preview-network explanation: focused `requests.test.tsx` **22 passed** (`job-ef7a982b`). Go lane is adding readable server-resolved Browser resource details; empty operation-only consent is not acceptable.
- Root `go test ./...` (`job-f00b48bc`) passed the Browser/daemon/capability/session/tools/protocol/runtime packages but failed **two MCP inventory assertions** (`cmd/whip: TestMCPTestCLIReady`, `internal/mcp: TestServeInProcess`) after adding five lifecycle tools. Assigned exact assertion/doctor-output correction; whole suite not green yet.
- Actual native selected-host gate reported by native lane (`job-b8cddd43`, `/tmp/whip-browser-preview-native-evidence.json`): remote marker instead of same-port Mac decoy; Host/HttpOnly cookies; no proxy credentials at website; WebSocket/SSE; unapproved redirect/service-worker route denial; CDP JPEG; detach keeps approved human tab route; master loss destroys guest/revokes control and fails closed; explicit reconnect changes connection generation with stable environment storage; final tab close removes routes/proxy. This is **not** yet the complete daemon→SDK→preload agent path or a packaged release run.
- Actual production Go `NewDesktopBackend`/helper-parser tests against native Electron passed under race (lane artifact `410890a952f55c463d86dd2c52e14d99`); prior Fill/HiDPI issues are fixed. Delivered cancelled mutations remain explicitly outcome-unknown, never replayed.
- Audit triage: 43 existing findings remain. Critical `tar` extraction issues are in dependency chains including Electron Forge/build tooling; npm's proposed automatic fix downgrades Forge major versions. Expo/build tooling also appears among direct advisory chains. No blanket forced upgrade applied. This is not a clean security audit or a release waiver.
- Historical lane reports archived at artifact `e40a5f81ca6e15219f04452ce4d5fa4f` (49,371 bytes). Per-lane evidence files hold details; receipt of a report does not replace independent release acceptance.
