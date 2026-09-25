# Desktop releases

Desktop ships in the same `v<semver>` release as the standalone CLI/TUI.
Development pushes produce complete `v1.0.1-alpha.N` releases when enabled; main
pushes run CI only. Manual stable dispatch on main requires one final approval.
A signed/notarized Desktop failure blocks the whole release. The supported Desktop
target is Apple Silicon, macOS 14 or newer; CI builds on macOS 15 and records
Xcode, Go, Node and runner versions. The development-alpha configuration below
is an operational contract, not evidence that live setup or install/update
acceptance is complete. No historical data migration contract is provided.

## Release graph

The single `.github/workflows/publish-cli.yml` graph reuses shared CI/security and
one verified renderer for all standalone binaries and the signed app. It calls
`desktop-release.yml` for signing, notarization, runtime and LaunchServices
acceptance. The Linux remote backend is the standalone matrix's exact Linux x64
binary, not a second compilation. The independent `desktop-v*` trigger is retired.

The full candidate binds all CLI downloads, installer, DMG/ZIP, feed, checksums,
package/runtime/startup evidence, SBOM and notices to one source/version. Desktop
signing evidence still describes its signed subset; standalone binaries do not
inherit an app signature. Verify GitHub attestations against the unified parent
workflow and exact source SHA/ref: `refs/heads/development` for alpha,
`refs/heads/main` for stable, never a wildcard or the retired Desktop workflow.

One final publishing job creates/verifies the private GitHub draft, stages and
reads back immutable CDN objects, publishes the complete GitHub release, then
advances the Desktop feed last. Alpha maps to the internal beta app/feed and never
claims GitHub latest; stable advances latest only without downgrading an already
newer stable. See [release operations](releases.md) for pinned-source semantics,
artifact completeness, failure states and retry rules.

## GitHub configuration

Follow the canonical [environment allowlists and trust policy](releases.md#trust-and-environment-configuration):
signing permits exactly main/development, beta publishing development only, and
stable publishing main only with final approval. No PR release secrets, broad
branch rules or dependency-lifecycle credential exposure. Storage isolation does
not isolate shared signing or repository-wide GitHub release authority.
Admission, source checks and queue behavior are defined in [release operations](releases.md).

Set these in `desktop-signing`:

| Type | Name | Value |
| --- | --- | --- |
| Secret | `WHIP_DESKTOP_CERTIFICATE_P12_BASE64` | Base64 Developer ID Application identity **including its private key** |
| Secret | `WHIP_DESKTOP_CERTIFICATE_PASSWORD` | Password protecting that P12 |
| Secret | `WHIP_DESKTOP_NOTARY_PRIVATE_KEY` | Raw Apple API `.p8` PEM, not base64 |
| Variable | `WHIP_DESKTOP_SIGN_IDENTITY` | Developer ID identity name |
| Variable | `WHIP_DESKTOP_TEAM_ID` | Apple team ID |
| Variable | `WHIP_DESKTOP_NOTARY_KEY_ID` | API key ID |
| Variable | `WHIP_DESKTOP_NOTARY_ISSUER` | API issuer UUID |
| Variable | `WHIP_DESKTOP_BETA_UPDATE_URL` | Beta HTTPS `.../RELEASES.json` URL |
| Variable | `WHIP_DESKTOP_STABLE_UPDATE_URL` | Stable HTTPS `.../RELEASES.json` URL |

Set these independently in each channel's final publishing environment:

| Type | Name | Value |
| --- | --- | --- |
| Secret | `WHIP_DESKTOP_R2_ACCESS_KEY_ID` | Object read/write key scoped solely to that environment's bucket |
| Secret | `WHIP_DESKTOP_R2_SECRET_ACCESS_KEY` | Matching S3 secret |
| Variable | `WHIP_DESKTOP_BUCKET` | Alpha: `whipcode-alpha-releases`; stable: unchanged `whipcode-releases` |
| Variable | `WHIP_DESKTOP_R2_ENDPOINT` | `https://<account-id>.r2.cloudflarestorage.com` |
| Variable | `WHIP_DESKTOP_UPDATE_URL` | Exactly the same channel feed URL embedded at signing |

### Isolated alpha storage

The configured alpha bucket/domain and matching variables are below. Verify scoped
credentials before enablement; see the [rollout checklist](roadmap.md).
R2 keys are bucket-scoped, not prefix-scoped: an old shared-bucket alpha key could
still write stable objects until revoked.

| Purpose | Bucket / feed |
| --- | --- |
| New alpha (Desktop `beta`) | `whipcode-alpha-releases`; `https://whipcode-alpha-releases.inference.net/desktop/beta/darwin/arm64/RELEASES.json` |
| Stable, unchanged | `whipcode-releases`; `https://whipcode-releases.inference.net/desktop/stable/darwin/arm64/RELEASES.json` |
| Old Beta, readable but no longer advanced after cutover | `whipcode-releases`; `https://whipcode-releases.inference.net/desktop/beta/darwin/arm64/RELEASES.json` |

Store the new alpha key only in `desktop-beta-stage`, never repository-wide.
The alpha feed in `desktop-signing`'s `WHIP_DESKTOP_BETA_UPDATE_URL` must exactly
match beta publishing's `WHIP_DESKTOP_UPDATE_URL`: the URL is embedded in the
signed app, and publisher evidence binds it. Feed and ZIP URLs must share the
configured origin/path. Keep the stable bucket/domain/feed and signing URL unchanged.

Use the native R2 custom domain `whipcode-alpha-releases.inference.net`; no proxy
Worker, redirect or path rewrite. The URL path maps directly to the object key.
Preserve object layout, conditional writes, read-back checks and cache policy:
ZIP/DMG responses are immutable; feed responses revalidate. For example,
`desktop/beta/darwin/arm64/v1.0.1-alpha.42/whipcode-desktop-darwin-arm64.zip`
names the version-scoped ZIP; its DMG is `whipcode-desktop-darwin-arm64.dmg`.
`RELEASES.json` stays at the channel root. Local candidates remain flat for
checksums and GitHub uploads; old signed objects are never rewritten.

Require TLS 1.2 or newer and disable public `r2.dev` access. Verify public GET/HEAD,
range requests, content types and no login/bot challenge on the new exact hostname.
If needed, scope a Browser Integrity Check exemption to GET/HEAD on that hostname,
not production sites. An empty feed may return 404 before the first release;
DNS/TLS failure is configuration failure, not an empty feed. The publisher uses
region `auto`, removes inherited AWS session tokens and does not assume an AWS role.

Replacing a GitHub secret does not revoke its old credential. Inventory shared
stable/alpha and retired-environment keys without printing values; remove every
broad alpha-accessible copy and revoke the superseded alpha key. If stable shares
that key, first provision/update and verify a replacement scoped to the original
bucket and stored only in the stable environment, then revoke the shared key.
Do not break stable during rotation. Obtain Cloudflare credential-management and
secret-manager authority if unavailable; do not treat secret replacement as revocation.

Verify the alpha principal can write/delete a disposable object in its own bucket
and is **denied** a write to a harmless unique canary key in the stable bucket,
never a live feed/asset. If the negative check writes, stop, remove only that
canary, correct scope and retest. Record verified policy and outcomes without secrets.

Leave old Beta objects/feed readable but non-advancing after cutover; do not redirect.
The first new alpha's release notes must link the [one-time Beta reinstall](setup.md#desktop-installation-and-upgrades)
without prescribing data cleanup. CLI discovery still uses GitHub Releases.

Use the GitHub environment settings page or `gh secret set --env NAME` reading
from stdin. Never put secrets in command history, release evidence, or chat.
Existing GitHub secret values cannot be read back through the API. Retrieve their
original secret-manager export, issue a new scoped credential, or use a reviewed
one-time workflow that seals selected source secrets to the destination GitHub
environment public key. Never expose plaintext through logs or artifacts.

The initial Apple identity and notarization key were transferred from HALO using
that encrypted workflow. The historical `whipcode-releases-github-actions` token
was scoped to the original bucket, not its channel prefixes. Follow the isolation
and retirement procedure above rather than copying it into alpha. Future rotation
replaces only the matching environment's scoped key, verifies disposable
upload/download, then revokes the old key. Delete local transfer files after validation.

## Cut and validate a release

1. Follow [release admission and rollout](releases.md#pause-rollout-and-recovery);
   verify the complete candidate and signing/storage configuration. Do not create
   a separate Desktop tag.
2. Verify the complete release inventory, startup/runtime evidence, checksums,
   SBOM/notices, app icon and bundle identity. Stable gets one final approval;
   alphas proceed automatically only after all required checks.
3. Test a quarantined downloaded DMG as a normal user with no global CLI needed.
   Verify the public release and CDN feed name the exact signed candidate; record
   first-install and subsequent-update acceptance separately.
4. Exercise signed Squirrel N→N+1 between post-reset builds in an isolated QA
   home/channel: running work preserved while downloading; one Restart and update
   approval; installed bytes and daemon match N+1; sessions/config survive;
   CLI/web reconnect. Also test manual DMG replacement, Later, failed/cancelled
   shutdown, external replacement, read-only destination and recovery. No
   pre-reset migration is promised; use the [reset checklist](team-reset.md).
5. Verify macOS 14 and current macOS on physical Apple Silicon, provider setup,
   helper permissions and network use. A hosted macOS 15 run does not prove
   minimum-OS hardware acceptance. Review SBOM property
   `whip:license-declarations-without-files`; `@tanstack/markdown@0.0.13` supplies
   an MIT declaration but no license file.
6. Record run/tag/source, GitHub asset and feed outcomes, and actual acceptance.
   Signed startup is not Squirrel replacement or physical minimum-OS evidence;
   label any unavailable manual/hardware checks explicitly.

### Signed startup contract

The LaunchServices collector measures **zero-interaction empty-frontdoor** startup
at bare `/` with empty device/window stores; it must not seed a draft, auto-open a
tab, or redirect first launch. Success requires the actual frontdoor's visible,
enabled New session action, an affirmative **current verified local renderer SDK
connection**, and `StartupScreen`'s `visible` phase. Missing/unknown connection
state, hidden/inert controls, stale-route empty states and sidebar-only actions
fail closed. Exact bundle origin/path, notices, document visibility, shell mark,
fonts/two animation frames and existing deadlines/sample counts remain gates.
A running private daemon alone is not renderer connectivity.

A separate disposable `new-session-onboarding` functional launch waits for that
same frontdoor, clicks its scoped New session action exactly once, and requires
provider setup or a configured composer on the exact newly generated draft route
and visible workspace view. It does **not** enter startup timing percentiles.
Retained-session measurements keep their exact route, connected controls and
fixture-transcript requirements. Neither scenario changes production first-run UX.

The fixture-only renderer observation is enabled by a literal native argument
only when both fixture and probe flags are set. Preload exposes only an immutable
boolean; bootstrap projects current bounded SDK/tab state without new IPC or
exposing runtime/client capabilities, and removes the getter on disposal. Actual
bootstrap tests fake the protocol transport, not application routes/connections or
startup completion. Component selector tests and collector lifecycle self-tests
remain narrower checks, not substitutes for a signed LaunchServices run.

### Failed signed startup evidence

The package job always attempts a separate `desktop-startup-diagnostics-arm64`
artifact upload, retained for **7 days**. Its sole file is
`apps/desktop/out/diagnostics/startup.json`, produced by the collector's
`--diagnostics-output` option once startup collection is reached. Earlier build/signing
failures may have no startup diagnostic; missing files warn rather than hide the
original failure. The release payload artifact remains **success-only**.

This schema-1 diagnostic is an explicit allowlist, at most **8 KiB**, containing
source/renderer/archive hashes, signing booleans, record count, and only the last
attempt's enumerated state/target, bounded timings, boolean readiness checks,
probe counters, fixed route/UI/SDK/startup-phase enums, launcher exit health and
verified fixture-daemon health. It never
copies raw reports, logs, errors, PIDs, paths, route/session IDs, configuration,
keys, transcripts or environment. Do not replace its exact upload path with a
fixture directory or the full `signed-startup.json` on failure.

On acceptance failure, the collector checks the isolated daemon using the existing
bounded private-socket verifier, prints the same minimized diagnostic and saves
the sidecar **before owned GUI/daemon cleanup**. It describes measurement health,
not successful cleanup or release acceptance. Diagnostic failures cannot replace
the acceptance error. Passing measurements make no additional health calls.
A missing `frontdoor`/`home` check warrants checking the observed route/UI variant
and renderer connection against the scenario contract. `painted: false` alone
does not prove a paint failure: frame waits are gated on the target UI readiness. Renderer contract tests run with
`npm run test:web`; the collector lifecycle/self-test remains in `test:desktop`.
A source-level fix is not signed LaunchServices acceptance: require a fresh hosted
check on a new reviewed commit/release version, and never move a failed release tag.

The two-build Go integration test proves the canonical backend handoff and session
preservation. Mock updater tests prove event handling and approval persistence.
Neither substitutes for step 4's actual Squirrel replacement or the minimum-OS
hardware gate. Do not label a release accepted until those checks are recorded.

## Recovery

For upload, draft-publication or feed failures, rerun only the failed publication
job while the exact candidate artifacts are retained. Do not rebuild signed
packages under an existing version. Matching public content is verified/skipped;
conflicting bytes/source fail. A public GitHub release with a pending feed is a
partial promotion, not permission to retag, clobber or downgrade a feed.

Disable `publish-cli.yml` and cancel active/queued runs for an emergency stop.
Variables are admission gates, not instant cancellation. Inspect effects before
retrying; no database downgrade or automatic feed rollback. Expired candidate
artifacts require a new release version. Revert source via a new commit/new alpha
version, never rewritten branch/release history. See
[release recovery](releases.md#pause-rollout-and-recovery).

## Matching remote backend

Each unified GitHub release includes `whipcode-linux-x64` from the same source,
semantic version, and renderer. Its standalone CLI build ID uses `v<version>`
(for example `v1.0.1-alpha.42`); Electron and the macOS managed backend use the bare
`<version>`. Candidate verification requires that exact relationship and matching
protocol/schema, not a loose version comparison. Download it and `SHA256SUMS` from that **exact unified version tag**,
verify the executable checksum and GitHub attestation, and stop the remote daemon
deliberately before replacing its executable. Keep its home/configuration intact,
start the new executable, and verify `daemon status --json` reports the expected
build. Desktop never silently updates SSH or URL hosts.

The Linux artifact is standalone, so explicit `whipcode update` follows the same
unified version track. To pin a remote host to a particular Desktop version,
install that version's Linux artifact explicitly. Desktop never silently updates
SSH or URL hosts.

Reference: [GitHub artifact verification](https://cli.github.com/manual/gh_attestation_verify),
[environment protection](https://docs.github.com/en/rest/deployments/environments),
[R2 token scopes](https://developers.cloudflare.com/r2/api/tokens/).

Final downloads are `whipcode-desktop-darwin-arm64.dmg` and
`whipcode-desktop-darwin-arm64.zip`. The packaging boundary normalizes names before
final notarization, evidence and checksums; GitHub uploads must not rename them
afterward. The app inside retains its normal display name (Whip or Whip Beta).
