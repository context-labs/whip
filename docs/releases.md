# WHIP release operations

One version and GitHub release contains the CLI/TUI for Linux/macOS x64/arm64
and the signed macOS Apple Silicon Desktop DMG/ZIP. Desktop is required, not an
optional attachment. The TUI ships in `whipcode`; it has no separate version.

## One source, one candidate, one publisher

[The release workflow](../.github/workflows/publish-cli.yml) keeps its historical
filename and run counter. The development-alpha configuration contract is below;
configured infrastructure is distinct from release acceptance. Follow the
[rollout procedure](#pause-rollout-and-recovery) and [acceptance checklist](roadmap.md).
No `desktop-v*` trigger or independent CLI-only publisher remains.

| Event | Validation | Publication when enabled |
| --- | --- | --- |
| Push to `development` | CI/security and full release gates at the push SHA | Automatic `v1.0.1-alpha.N`, GitHub prerelease/not-latest, Desktop Beta feed |
| PR targeting `development` or `main` | CI/security at the PR merge SHA | None; no signing/publishing environments |
| Push to `main` | CI/security | None |
| Manual `channel=alpha` on `development` | Full unified graph | Same alpha publication |
| Manual `channel=stable` on `main` | Full unified graph | `v1.0.1`, one final approval before public staging |

Release admission accepts exactly `(development, alpha)` and `(main, stable)`.
Wrong branch/mode pairs, tags and forks are rejected. Alpha remains Desktop
channel `beta` / Whip Beta and CLI channel `prerelease`; no channel is renamed.

The base version is declared once as `1.0.1`. `N` is the existing workflow run
number, may have gaps and need not start at 1. This train sorts above stable
`1.0.0`; further `1.0.0-alpha.N` versions would not. After stable `1.0.1`, deliberately
advance the base before the next alpha train (for example `1.0.2` for another
patch train). Do not reset the counter, infer semantic version intent automatically,
or overwrite a used tag.

The triggering commit is immutable: shared CI/security, the renderer, all four
standalone binaries and the signed Desktop app use that SHA. The source must
descend from `WHIP_RELEASE_BASELINE` and remain reachable from `origin/development`
for alpha or `origin/main` for stable, at admission and before external publication
effects. It need not remain the branch tip; alpha need not already be on main.
Attestations bind the exact SHA and validated branch ref, expected signer workflow
and digest, and hosted-runner policy. Never use an unchecked source-ref wildcard.
New commits produce new candidates, never substitute source inside a version.

Alpha and stable have separate non-canceling release concurrency groups. An active
run is not interrupted by newer pushes; the latest pending run can replace an
older pending run. There is no release-per-commit guarantee or commit-order
queue guarantee. A stable approval cannot hold the alpha queue, and an alpha
cannot displace a pending stable request. Keep feed CAS, immutable writes and
semantic no-downgrade checks. Ordinary branch CI may cancel obsolete checks;
publication must not.

`main` stays the default branch. Day-to-day integration targets `development`;
direct pushes are allowed and PRs optional. Promote through a reviewed/tested
`development` → `main` PR under main's existing protections. Merge main hotfixes
and release-version changes back into development without resetting either branch.
Stable is rebuilt and revalidated from the main merge SHA, not a retagged alpha
binary: version, app identity and channel differ.

The shared renderer is built once. Desktop remote-backend acceptance reuses the
standalone `whipcode-linux-x64` bytes rather than rebuilding them. The app's
bundled signed backend retains Desktop update ownership; standalone downloads
retain standalone ownership. Same version does not mean interchangeable payloads.

## Trust and environment configuration

The publisher uses GitHub's built-in `GITHUB_TOKEN` with `contents: write`.
No App, PAT or additional account is required. Keep main PR/CI protection with
zero required reviews and no bypass, and block update/deletion of existing tags.
Do not reintroduce a blanket tag-creation block: it also blocks the built-in token.
Repository writers may create tags; creating a tag does not trigger publication.

Required main checks include `go`, `govulncheck`, `codeql`, and the separate
`CodeQL` findings result. Leave main's protections unchanged. Development's exact
branch rules block deletion and non-fast-forward updates, but require neither PRs
nor pre-push status checks. Passing CI is a release prerequisite, not a guarantee
that direct-push development stays green; fix or revert a bad push with a new commit.
The release reuses the full validation definitions, including packaged, native,
renderer and signing acceptance. A missing, skipped or failed required component
must not yield a CLI-only release.

Configure exact selected branch names, never broad protected/all-branch rules:

| Environment | Use | Policy |
| --- | --- | --- |
| `desktop-signing` | Temporary Developer ID keychain and notarization | Exactly `main` and `development`; no admin bypass |
| `desktop-beta-stage` | Entire alpha publication, isolated CDN staging and beta-feed promotion | `development` only, automatic, no admin bypass |
| `desktop-stable-stage` | Entire stable publication, CDN staging and stable-feed promotion | `main` only, Sam approval once, no admin bypass |

Keep the environment names; do not revive separate `*-promote` or `whipcode-*`
publishing environments. Approval precedes public staging. Signing secrets stay
step-scoped in the signing environment, outside dependency-install lifecycles.
R2 secrets stay only in the matching publishing environment. Alpha requires a new
bucket-scoped credential before development publishing is enabled; do not copy
broad existing credentials. See [Desktop signing/storage configuration](desktop-releases.md#isolated-alpha-storage)
for the configured `whipcode-alpha-releases` bucket/domain, credential retirement,
and canary checks. Stable storage/feed remain unchanged.

**Trust limit:** alpha storage isolation is not full release-authority isolation.
Development writers control code/workflows with access to the shared signing
identity and repository-scoped GitHub `contents: write` token. Stable R2 credentials
and final approval stay unavailable to development, but signing and the GitHub
release repository remain trusted surfaces. Restrict development write access to
trusted release operators; this is not an untrusted sandbox. No new PAT/App,
`pull_request_target` release execution or tag-trigger publisher is introduced.

`WHIP_RELEASE_BASELINE` is the accepted clean-source SHA. `WHIP_RELEASE_ENABLED`
is the single admission gate for the complete release. Desktop remains required:
a missing, skipped or failed Desktop build blocks publication, never permits a
CLI-only release. Verify live settings before enabling the flow.

## Download names and immutable URLs

Public payload names are lowercase, product first, OS then architecture. Versions
and channels are carried by the release tag/feed, not the filename:

| Download | Filename |
| --- | --- |
| Linux CLI x64 / ARM64 | `whipcode-linux-x64` / `whipcode-linux-arm64` |
| macOS CLI x64 / ARM64 | `whipcode-darwin-x64` / `whipcode-darwin-arm64` |
| macOS Apple Silicon Desktop installer | `whipcode-desktop-darwin-arm64.dmg` |
| macOS Apple Silicon Desktop update archive | `whipcode-desktop-darwin-arm64.zip` |

Alpha and stable use the same basenames. GitHub isolates assets under
`releases/download/<tag>/`. Desktop CDN objects use
`desktop/<channel>/darwin/arm64/<tag>/<filename>` so each version stays immutable.
For example: `desktop/beta/darwin/arm64/v1.0.1-alpha.42/whipcode-desktop-darwin-arm64.zip`.
Within each configured channel origin, `RELEASES.json` stays at the channel root.
The new alpha feed uses an isolated origin and requires a one-time Beta reinstall;
stable keeps its existing origin. Preserve old object URLs and the old Beta feed
as readable, non-advancing history; never overwrite or rename old releases.

The app inside still has its normal Whip/Whip Beta identity. `install.sh`, `latest.sh`,
`SHA256SUMS`, `artifact-manifest.json`, `RELEASES.json` and evidence/notices retain
their conventional names. This naming scheme does not add supported platforms.

## Complete artifacts and publication order

The existing candidate verifier owns one exact inventory: four CLI binaries,
installer, signed Desktop archives, update metadata, evidence/SBOM/notices,
checksums and attestation. Hash final signed bytes, reject missing/extra/duplicate
files, and bind source/version/channel/runtime identity. CLI consumers require
the CLI subset and accept additional well-formed Desktop/evidence assets; they
still verify selected bytes and safe, unique checksum records. The candidate has
17 files: 15 payload/evidence files, a manifest hashing those 15, and one
`SHA256SUMS` containing 16 entries including the manifest, never itself. GitHub
attestations cover the final files without adding a recursive checksum envelope.

1. All required builds, acceptance and candidate checks succeed.
2. Alpha proceeds automatically; stable receives its single final approval.
3. Populate or resume the matching private GitHub draft and verify its exact bytes.
4. Upload/read back immutable CDN payloads. These URLs are public even before
   feed promotion; no preannouncement confidentiality is promised.
5. Publish the complete GitHub release once. Alpha is prerelease/not-latest.
   Stable advances latest only when no newer stable has already published.
6. Advance/read back the Desktop feed last, retaining CAS and no-downgrade checks.

A failure before GitHub publication leaves no public GitHub release or feed
change; a partial draft and unadvertised immutable CDN files may remain. A feed
failure after publication means **complete release published, feed pending**.
Retry the failed publication/promotion job using the exact retained candidate,
not all builds. Conflicting source/bytes fail; never retag or overwrite public
assets. Already published identical content is verified, not replaced. If saved
candidate artifacts expire or source must change, issue a new version.

## Run and verify

```sh
# A development push does this automatically after rollout/enablement.
# Use an explicit dispatch for bootstrap or a deliberate new alpha candidate.
gh workflow run publish-cli.yml --ref development -f channel=alpha

# Deliberate stable candidate; final environment approval is still required.
gh workflow run publish-cli.yml --ref main -f channel=stable
```

Verify the run's SHA/tag/version, full inventory, checksums and attestations;
clean CLI installation and Chromium/daemon startup; signed Desktop installation,
matching backend, and update-feed discovery. Use disposable homes and candidate
apps, not `task update:local` against a user's installation. Record both GitHub
release and feed outcomes. Desktop never updates an SSH/URL host silently.

Each release contains two generated installers from the one source `install.sh`:

| Published asset | Selection policy |
| --- | --- |
| `install.sh` | Exact embedded release tag, including alpha/beta; no version environment variable needed |
| `latest.sh` | Newest complete stable v1+ release at execution time; never alpha/beta or legacy v0 |

Neither generated script can be redirected by inherited `WHIPCODE_VERSION` or
`WHIPCODE_CHANNEL`. Other controls, such as the destination and authentication,
retain their existing behavior. `latest.sh` fails clearly when no stable v1+
release exists. A release-hosted copy is a snapshot of installer code, even though
its stable selection is dynamic.

Use the command in that release's notes, for example (substitute a published tag):

```sh
curl -fsSL https://github.com/context-labs/whip/releases/download/<tag>/install.sh | sh
curl -fsSL https://github.com/context-labs/whip/releases/download/<tag>/latest.sh | sh
```

The repository's `main/install.sh` remains the shared source and supports explicit
selection overrides used by development and `whipcode update`. There are not two
separately maintained installer implementations. Generate both assets before
candidate hashing/attestation; verify both byte-for-byte against the expected
source/tag generation before any public effect. Old published assets stay intact.
See [setup](setup.md#standalone-releases-and-updates) and the
[manual reset checklist](team-reset.md).

## Pause, rollout and recovery

Before changing release admission, credentials or environments, set
`WHIP_RELEASE_ENABLED=false` and inspect both release queues. This does not cancel
already admitted jobs: drain them or deliberately cancel them after inspecting
prior external effects. Arrange a pause window if stable publication is in flight.
For an emergency stop, disable `publish-cli.yml` and cancel active/queued runs.
Live workflow-state checks precede public effects, but interrupted requests may
already have changed external state. Inspect before retrying.

While paused:

1. Land tested workflow/publisher/docs changes on main through its normal protected
   PR flow. Do not create/push development early under the old workflow.
2. Provision and verify isolated alpha storage; retire broad alpha-accessible keys
   without breaking stable. Verify exact environment allowlists and development
   integrity rules. Keep main protections and stable latest/feed unchanged.
3. Create development at the tested main SHA and push while admission remains
   paused. Verify branch CI, environment restrictions and no publication.
4. Enable admission only after those checks pass; bootstrap with one explicit
   alpha dispatch on development, not an empty production commit.
5. Record the first alpha's tag/source/run, all 17 candidate files, checksums and
   attestations, fresh CLI install and signed/notarized Desktop/backend/renderer
   acceptance. Existing Beta testers must install the new signed DMG once; see
   [setup](setup.md#desktop-installation-and-upgrades).
6. A subsequent legitimate development push must prove automatic triggering;
   a second accepted alpha must prove actual update-feed advancement. Signed
   startup or mocked updater tests do not satisfy that update gate.
7. Reconfirm stable latest/feed and production web/docs sites are unchanged.

Record bucket/domain ownership, canary-scope results, environment/ruleset checks,
first-install and subsequent-update receipts in the development-alpha rollout
record. Until exercised, each remains **unverified**, not completed by code review
or these instructions. This rollout creates installable alpha releases only;
there is no hosted web/docs staging or production-site change.

Retry partial publication only with the exact retained candidate as described
[above](#complete-artifacts-and-publication-order). A new dispatch creates a new
candidate, not recovery of old bytes. If artifacts expired or source/version must
change, use a new version; never rebuild under a used tag. Roll back source with
a new commit and new alpha version, not a downgraded feed or rewritten history.
Do not weaken source, signing, artifact or environment checks to resume.
