# Desktop releases

Whip desktop releases use protected `desktop-v<semver>` tags on the approved
current `main` commit, descending from the recorded clean release baseline. A suffix selects beta; a plain version selects stable. The first
supported desktop target is Apple Silicon with macOS 14 or newer. CI builds on
macOS 15 and records the actual Xcode, Go, Node, and runner image versions.
No historical protocol, state, or configuration migration contract is provided.

## Release graph

`.github/workflows/release-desktop.yml` runs the shared CI and security
gates, including the native desktop suite. Its one production renderer artifact
feeds the signed macOS app and matching Linux x64 backend. Packaging verifies
the app, native helpers, ASAR, fuses, notarization, and the actual app tree inside
both DMG and ZIP. Native SSH, staged desktop, and signed LaunchServices acceptance
run before a candidate is assembled.

The candidate contains the final DMG/ZIP, feed, Linux executable, checksums,
package evidence, startup evidence, a CycloneDX dependency inventory, and notices.
An artifact attestation binds each file to the exact release source and workflow.
Both staging and promotion verify those attestations with source SHA, source ref,
signer workflow, signer SHA, and hosted-runner requirements.

Staging creates or resumes a GitHub draft, verifies immutable asset bytes, and
uploads versioned R2 objects with create-only writes. It leaves the live feed
unchanged. After environment approval, promotion verifies the same candidate,
publishes the complete GitHub release, and advances the R2 feed last using its
authoritative ETag. A retry reuses exact bytes; a conflicting published file or
source/tag identity fails. Never rebuild under an existing release version.

Desktop releases never claim the repository-wide GitHub “latest” slot. The
independent standalone CLI release path still owns its ordinary latest-release
discovery. The CLI `v*` graph cannot publish a desktop update feed.

## GitHub configuration

The repository uses these environments. Deployment rules must allow only
`desktop-v*` **tags**, not arbitrary branches. Publishing credentials are exposed
only to the relevant verified publication step, never to `npm ci` lifecycle scripts.

| Environment | Purpose | Required review |
| --- | --- | --- |
| `desktop-signing` | Import a temporary Developer ID keychain and notarize | Trusted release tags |
| `desktop-beta-stage`, `desktop-stable-stage` | Upload immutable candidate assets publicly | Sam Heutmaker; no admin bypass |
| `desktop-beta-promote`, `desktop-stable-promote` | Publish the completed release and feed | Sam Heutmaker; no admin bypass |

Configure approvals **before staging**, not only before feed promotion: staging
already exposes public GitHub/R2 bytes. Verify reviewers, self-review policy,
admin bypass, tag deployment rules, and token scopes in the live repository;
this document does not assert those settings have already been applied.
Protect `desktop-v*` against moving/deletion and limit tag creation to the chosen
release actors. `main` requires PRs and passing shared checks with zero required
approving reviewers and no routine bypass. Do not lower the coverage floor.

Repository variables `WHIP_RELEASE_ENABLED=true` and
`WHIP_DESKTOP_RELEASE_ENABLED=true` must both be deliberately enabled.
`WHIP_RELEASE_BASELINE` must be the full 40-character clean release commit SHA.
The release metadata gate requires the tagged source to equal current
`origin/main` and descend from that baseline. Main ancestry alone is insufficient
because archived pre-reset source is also in main's history. See
[release operations](releases.md) before enabling publication.

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

Set these independently in each channel's stage and promote environments:

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
both R2 secrets in all four publishing environments, validating an isolated upload
and download, and then revoking the old token. GitHub stores the persistent copy;
delete local transfer files after validation.

## Cut and validate a release

1. Merge the implementation into `main` through its checks. Ensure the exact
   current main commit is clean, descends from `WHIP_RELEASE_BASELINE`, has passed
   the shared CI/security graph, and contains the release workflows. Confirm
   enablement, environments, secrets, domains, and immutable tag rules.
2. Choose an unused version. Create and push its `desktop-v<version>` tag on the
   approved commit. This is the release trigger; a development branch push does
   not publish desktop. Tags created with `GITHUB_TOKEN` do not trigger another
   push workflow, so use the maintainer release procedure.
3. Wait for the candidate; approve the exact bytes in `desktop-<channel>-stage`
   only when public staging is authorized. Inspect `artifact-manifest.json`,
   `evidence.json`, `signed-startup.json`, checksums, notices, and attestations.
   Verify the app icon and bundle identity. Test a quarantined downloaded DMG
   installation as a normal user with no global `whipcode` installed.
4. Exercise an actual signed Squirrel N→N+1 update between **post-reset** builds
   in an isolated QA channel/home. Old pre-reset installations are not an upgrade
   path; use the [manual reset checklist](team-reset.md).
   Check that downloading preserves running work; one Restart and update approval
   carries across app relaunch; installed bytes and the running daemon both match
   N+1; sessions/config survive; and reconnect works through the CLI and web app.
   Also test manual DMG replacement, Later, failed/cancelled shutdown, an externally
   replaced binary, a read-only destination, and recovery after replacement.
5. Verify macOS 14 and current macOS on physical Apple Silicon, provider setup,
   computer/browser helper permissions, and network access if used. A macOS 15 CI
   runner does not prove macOS 14 compatibility. Review any inventory property
   `whip:license-declarations-without-files` before public distribution. Currently
   `@tanstack/markdown@0.0.13` supplies an MIT declaration but no license file.
6. Approve the concrete candidate in `desktop-<channel>-promote`. Verify the public
   GitHub release and R2 feed, and check discovery from the previous app version.
   Record the run URL, tag, source SHA, feed URL, and acceptance evidence.

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

For upload, draft-publication, or feed-promotion failures, rerun the **failed jobs**
in the same workflow run while `desktop-candidate` is retained (30 days). Do not
rerun successful packaging jobs or use a new artifact with the old tag. A retry
finds existing drafts and verifies existing assets by immutable asset ID/hash.
If GitHub returns a 403/404 while creating a release for a commit that changes
workflows, inspect the token's repository/workflow permissions and the tagged
commit before retrying. Do not replace the job token with a broad personal token
as a default workaround. This tag-push workflow must exist in the tagged commit;
it does not require default-branch registration unless a manual dispatch trigger
is introduced.
If GitHub is public but the feed is not yet promoted, the old feed remains usable;
rerun promotion to finish. No command deletes the existing release to retry it.

Do not automatically downgrade a feed or restore an old database. Ship a new
version with a forward fix. For an emergency, disable the Desktop workflow, cancel active/queued runs, and
document the affected version. The publication steps recheck live workflow state;
changing enablement variables alone is not immediate cancellation. For recovery, any deliberate feed change needs review and must preserve the
referenced archive bytes. Keep released downloads even when old feed entries age
out of its bounded history.

## Matching remote backend

Each desktop GitHub release includes `whipcode-linux-x64` from the same source,
semantic version, and renderer. Its standalone CLI build ID uses `v<version>`
(for example `v1.0.0-beta.1`); Electron and the macOS managed backend use the bare
`<version>`. Candidate verification requires that exact relationship and matching
protocol/schema, not a loose version comparison. Download it and `SHA256SUMS` from that **exact desktop tag**,
verify the executable checksum and GitHub attestation, and stop the remote daemon
deliberately before replacing its executable. Keep its home/configuration intact,
start the new executable, and verify `daemon status --json` reports the expected
build. Desktop never silently updates SSH or URL hosts.

The Linux artifact is standalone, so its explicit `whipcode update` follows the
standalone CLI track. To stay paired to desktop, install the next matching desktop
release's Linux artifact explicitly instead of following standalone CLI releases.

Reference: [GitHub artifact verification](https://cli.github.com/manual/gh_attestation_verify),
[environment protection](https://docs.github.com/en/rest/deployments/environments),
[R2 token scopes](https://developers.cloudflare.com/r2/api/tokens/).

Final downloadable filenames use hyphens instead of spaces so GitHub, R2, the update feed, and checksums name identical artifacts. The app inside the archive retains its normal display name (Whip or Whip Beta).
