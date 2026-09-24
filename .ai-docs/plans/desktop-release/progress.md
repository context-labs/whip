# Desktop release implementation record

2026-09-08. The implementation landed through
[PR #138](https://github.com/context-labs/whip/pull/138). The first protected tag,
`desktop-v0.2.0-beta.1`, identifies `0ec4a08b6f7e976f61ebed0e9b408830e2f39290`.
[Its release run](https://github.com/context-labs/whip/actions/runs/34291545971)
is the authoritative record of candidate creation, staging, and promotion.

## Implemented

- Dedicated `desktop-v*` release graph with shared verified renderer, macOS arm64
  signed/notarized DMG and ZIP, and matching standalone Linux x64 backend.
- Reusable native desktop gate, exact candidate manifest/checksums, GitHub
  artifact attestations, immutable staging, and separately approved promotion.
- Desktop-managed canonical backend updates, persistent one-release restart
  approval, maintenance locking through readiness, and explicit external-binary
  ownership. Standalone CLI updates remain independent.
- Beta/stable bundle identity and deep-link checks, app icon, CycloneDX inventory,
  third-party notices, and recorded build toolchain.
- Operational runbook: [desktop releases](../../../docs/desktop-releases.md).

## GitHub configuration completed

- Created `desktop-signing`, beta/stable staging, and beta/stable promotion
  environments, restricted to `desktop-v*` tags.
- Promotion requires Sam's review. Self-review is currently permitted.
- Release-tag rules prevent update/deletion and restrict creation to admins.
- `whip-rlm` requires the GitHub Actions `go`, `govulncheck`, and `codeql` checks
  with strict current-base validation and no deletion/force pushes.
- Developer ID P12, its password, and the Apple notary API key were transferred
  from HALO directly into `desktop-signing` as ciphertext sealed to GitHub's
  environment public key. The temporary source branch was removed. No plaintext
  secret was emitted in logs or artifacts.
- CI successfully imported the Developer ID identity from those secrets.
- The `whipcode-releases` bucket and its TLS 1.2 custom domain are active. Both
  channels use separate paths within this bucket. The dedicated object token is
  configured in all four stage/promote environments, alongside channel feed URLs.
  [R2 acceptance](evidence/cloudflare-r2.json) records byte/range delivery, cache
  headers, create-only uploads, rejected stale ETags, and successful conditional
  updates. The initial non-browser HTTP 1010 response was fixed with a GET/HEAD
  Browser Integrity Check exception scoped to the release hostname.

## Validation

- Native desktop: 72 tests passed with the actual signed `whipcode` SSH helper;
  packaging/publication: 86 tests passed, plus startup-probe self-test.
- Real compiled N-to-N+1 backend update passed under the race detector: approval
  before interruption, changed binary and daemon build, retained session/config,
  idempotent retry, and desktop-owned CLI update behavior.
- Staged Electron lifecycle smoke passed with isolated homes and no renderer
  errors. Signed/notarized local packages and archive verification succeeded.
- LaunchServices acceptance passed 61 measured launches: one initial install,
  30 warm attachments, and 30 retained-session launches after daemon restart.
  This caught and fixed an overlapping startup synchronization error and a beta
  fixture that used the stable deep-link scheme.
- Go vulnerability scan found no reachable vulnerabilities. Production npm audit
  found no high/critical findings; moderate findings were in the mobile/Expo tree.
- Final `task check`, golangci-lint v2.13.1 (zero findings), and workflow syntax
  validation passed. The two-build race test passed again after the maintenance
  validator extraction. The final portable race/shuffle suite passed; `go tool
  cover` reports **90.0%**, satisfying the existing CI floor without changing it.
- [Local validation evidence](evidence/local-validation.json) records signed
  archive/native hashes and all 61 successful launch measurements.
- Final review also found a missing inherited-descriptor cleanup edge case.
  The guard rejects descriptor reuse before creating a second file wrapper;
  subprocess regression tests and the compiled two-version update pass with it.

Local validation artifacts use `0.2.0-beta.1` with dirty development provenance
and no update feed. They are test builds, not publishable release candidates.
Actual release packaging requires a clean tagged commit and configured feed.

## First release execution

[PR #139](https://github.com/context-labs/whip/pull/139) corrects two test timing
assumptions exposed by release CI. The SDK test now waits for committed history
as well as replayed cursor convergence; all 33 race-enabled acceptance tests pass.
The SSH test applies its 100 ms deadline only to the deadline scenario; normal
local process startup gets a realistic allowance while bounded cleanup remains
required. The SSH suite passed five race-enabled repetitions. Production behavior
and coverage requirements are unchanged.

The first desktop run's failed SDK job was retried after root-cause analysis;
its CI and security gates passed, and signed packaging started. Its Linux payload
also passed the embedded-renderer and daemon smoke test. Consult the release run
for the final packaging/publication result rather than interpreting this dated
progress record as a release authorization.

A clean signed/notarized `0.2.0-beta.0` QA build from the same source is available
only under the isolated `qa-20260908` R2 prefix for the first real updater test.
Its renderer digest matches the release CI artifact. It passed all 61 measured
launches on an Apple M1 virtual machine running macOS 14, including retained
session startup, with fixture cleanup confirmed. The native dialog automation
permission preflight also passed. [Baseline run and full evidence](https://github.com/context-labs/whip/actions/runs/34292930904).
This establishes actual macOS 14 VM coverage, not a physical-device TCC review.

Before the initial feed is promoted, the final CI candidate must pass the actual
signed Squirrel app/backend update on a clean runner and be reviewed with its
checksums and attestations. The runbook retains the manual download, device,
helper-permission, and distribution-notice review checklist. The candidate's
GitHub release and acceptance runs should carry the final evidence links.

Unrelated permission-card UI work in the shared checkout is excluded from this
release implementation change.
