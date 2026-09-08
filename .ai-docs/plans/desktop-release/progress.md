# Desktop release implementation record

2026-09-08. Work is on `codex/desktop-release`. No desktop release tag, public
release, or update feed has been published. The installed `/Applications/Whip.app`
and `/usr/local/bin/whipcode` remain unchanged.

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
- Public signing identity, team, notary key ID, and issuer variables are set.
  Private credentials and download URLs are not configured.

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
  validator extraction. Portable race coverage is undergoing its final run.
- [Local validation evidence](evidence/local-validation.json) records signed
  archive/native hashes and all 61 successful launch measurements.

Local validation artifacts use `0.2.0-beta.1` with dirty development provenance
and no update feed. They are test builds, not publishable release candidates.
Actual release packaging requires a clean tagged commit and configured feed.

## Remaining release gates and inputs

1. Supply working signing credentials for `desktop-signing`: Developer ID P12
   including its private key, P12 password, and raw Apple notary API `.p8` key.
   Local Keychain signing/notarization works, but the configured API-key file is
   missing. Retrieve credentials from their original vault or set environment
   secrets directly; never paste secrets into chat or logs.
2. Supply working R2 credentials and select two bucket/custom-domain pairs.
   Existing local credentials fail authentication. Configure bucket-scoped keys
   and matching feed URLs in the environments listed in the runbook. Verify
   public TLS, byte/range delivery, caching, and conditional writes on real R2.
3. Land the implementation through green CI on the actual source. The checkout
   also contains earlier mobile/canonical-install commits not yet on remote
   `whip-rlm`. Require the actual aggregate checks on that branch before tagging.
4. Run the signed CI candidate, Linux runtime smoke, and actual Squirrel app
   N-to-N+1 installation in an isolated QA channel. Backend handoff integration
   and mock updater tests do not substitute for Squirrel replacing the app.
5. Test quarantined download/install and macOS 14/current macOS on physical Apple
   Silicon. Review the declared MIT dependency `@tanstack/markdown@0.0.13`, which
   lacks an upstream/package license file; the inventory records the omission.
6. Review the exact candidate and approve promotion. Record its workflow URL,
   tag, source, artifact hashes, final feed URL, and device acceptance evidence.

Unrelated permission-card UI work in the shared checkout is excluded from this
release implementation change.
