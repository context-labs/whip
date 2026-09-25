# Desktop releases

Desktop ships in the same `v<semver>` release as the standalone CLI/TUI.
Main pushes produce complete alpha releases; stable dispatch requires one final
approval. A signed/notarized Desktop failure blocks the whole release. The
supported Desktop target is Apple Silicon, macOS 14 or newer; CI builds on macOS
15 and records Xcode, Go, Node and runner versions. No historical migration
contract is provided.

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
workflow and exact main source SHA/ref, never the retired Desktop workflow.

One final publishing job creates/verifies the private GitHub draft, stages and
reads back immutable CDN objects, publishes the complete GitHub release, then
advances the Desktop feed last. Alpha maps to the internal beta app/feed and never
claims GitHub latest; stable advances latest only without downgrading an already
newer stable. See [release operations](releases.md) for pinned-source semantics,
artifact completeness, failure states and retry rules.

## GitHub configuration

All active release environments permit only the protected `main` branch. No
release secrets are available to feature PRs. Credentials are passed only to the
relevant signing/storage steps, not dependency installation lifecycle scripts.

| Environment | Purpose | Required review |
| --- | --- | --- |
| `desktop-signing` | Temporary Developer ID keychain and notarization | Trusted main; no admin bypass |
| `desktop-beta-stage` | Complete alpha GitHub/CDN/feed publication | Automatic; no admin bypass |
| `desktop-stable-stage` | Complete stable GitHub/CDN/feed publication | Sam once after candidate validation; no admin bypass |

Existing environment names avoid copying secrets. Separate `*-promote`
environments are not called. Approval precedes public CDN staging. Main requires
PRs, passing shared checks, zero required approving reviews and no routine bypass.
Keep tag update/deletion protections and coverage floors.

`WHIP_RELEASE_ENABLED` is the single admission gate and `WHIP_RELEASE_BASELINE`
binds the clean source boundary. Desktop is always required; a missing, skipped
or failed Desktop build blocks the entire release rather than permitting CLI-only
publication. The candidate is the immutable triggering SHA; it may finish when
main advances, but must remain a validated baseline descendant in protected main's history.

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
| Secret | `WHIP_DESKTOP_R2_ACCESS_KEY_ID` | Object read/write key scoped to `whipcode-releases` |
| Secret | `WHIP_DESKTOP_R2_SECRET_ACCESS_KEY` | Matching S3 secret |
| Variable | `WHIP_DESKTOP_BUCKET` | `whipcode-releases` |
| Variable | `WHIP_DESKTOP_R2_ENDPOINT` | `https://<account-id>.r2.cloudflarestorage.com` |
| Variable | `WHIP_DESKTOP_UPDATE_URL` | Exactly the same channel feed URL embedded at signing |

Both channels use the `whipcode-releases` bucket in the Inference.net Cloudflare
account. R2 credentials are bucket-scoped, not prefix-scoped: the publishing token
can write both channels. GitHub environment review and the publisher enforce the
channel boundary; they do not provide storage-level credential isolation. Separate
buckets/tokens can add that isolation later. The publisher uses region `auto` and
removes any inherited AWS session token. There is no AWS role/OIDC assumption in this R2 path.

The bucket uses `https://whipcode-releases.inference.net` with no path rewrite:

- Beta: `https://whipcode-releases.inference.net/desktop/beta/darwin/arm64/RELEASES.json`
- Stable: `https://whipcode-releases.inference.net/desktop/stable/darwin/arm64/RELEASES.json`

The URL path maps directly to the R2 object key. The custom domain requires TLS
1.2 or newer; the public `r2.dev` endpoint is disabled. A configuration rule disables
Browser Integrity Check only for GET/HEAD requests to this exact hostname, so
non-browser download clients do not receive Cloudflare error 1010. ZIP/DMG
responses are immutable; feed responses must revalidate.
Verify TLS, public downloads, range requests, content types, and no login/bot
challenge. An empty feed may return 404 for the first release; a DNS/TLS failure
is a configuration failure, not an empty feed.

Use the GitHub environment settings page or `gh secret set --env NAME` reading
from stdin. Never put secrets in command history, release evidence, or chat.
Existing GitHub secret values cannot be read back through the API. Retrieve their
original secret-manager export, issue a new scoped credential, or use a reviewed
one-time workflow that seals selected source secrets to the destination GitHub
environment public key. Never expose plaintext through logs or artifacts.

The initial Apple identity and notarization key were transferred from HALO using
that encrypted workflow. The dedicated Cloudflare account token
`whipcode-releases-github-actions` has Object Read & Write permission on this
bucket only. Rotate it by creating a replacement with the same scope, updating
both R2 secrets in the two active publishing environments, validating an isolated upload
and download, and then revoking the old token. GitHub stores the persistent copy;
delete local transfer files after validation.

## Cut and validate a release

1. Merge through required checks; verify source/baseline, complete candidate,
   environment permissions and signing/storage configuration. Use the main-push
   alpha flow or explicit main dispatch, not a manually created Desktop tag.
2. Verify the complete release inventory, startup/runtime evidence, checksums,
   SBOM/notices, app icon and bundle identity. Stable gets one final approval;
   alphas proceed automatically only after all required checks.
3. Test a quarantined downloaded DMG as a normal user with no global CLI needed.
   Verify the public release and CDN feed name the exact signed candidate.
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
artifacts require a new release version. The detailed procedure is in
[release operations](releases.md#complete-artifacts-and-publication-order).

## Matching remote backend

Each unified GitHub release includes `whipcode-linux-x64` from the same source,
semantic version, and renderer. Its standalone CLI build ID uses `v<version>`
(for example `v1.0.0-alpha.4`); Electron and the macOS managed backend use the bare
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

Final downloadable filenames use hyphens instead of spaces so GitHub, R2, the update feed, and checksums name identical artifacts. The app inside the archive retains its normal display name (Whip or Whip Beta).
