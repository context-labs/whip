# WHIP release operations

One version and GitHub release contains the CLI/TUI for Linux/macOS x64/arm64
and the signed macOS Apple Silicon Desktop DMG/ZIP. Desktop is required, not an
optional attachment. The TUI ships in `whipcode`; it has no separate version.

## One source, one candidate, one publisher

[The release workflow](../.github/workflows/publish-cli.yml) keeps its historical
filename and run counter. Main pushes produce `v1.0.0-alpha.N`; deliberate main
dispatch with `channel=stable` builds `v1.0.0` and requires one final approval.
The base version is declared once in workflow metadata; advance it deliberately.
No `desktop-v*` trigger or independent CLI-only publisher remains.

The triggering commit is immutable: shared CI/security, the renderer, all four
standalone binaries and the signed Desktop app use that SHA. The source must
remain reachable from protected main and descend from `WHIP_RELEASE_BASELINE`;
it need not remain main's tip throughout signing. New commits produce subsequent
candidates, never substitute source inside an existing version.

The single serialized workflow does not cancel active publication. Pending main
pushes may coalesce; there is no guarantee of one release per commit. Waiting for
stable approval can delay alphas. Keep this simple until measured queue delays
justify separating build and publication concurrency.

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
`CodeQL` findings result. The release reuses those validation definitions and
includes real packaged, native, renderer and signing acceptance. A missing,
skipped or failed required component must not yield a CLI-only release.

Reuse the existing environments without copying credentials:

| Environment | Use | Policy |
| --- | --- | --- |
| `desktop-signing` | Temporary Developer ID keychain and notarization | Main only; no admin bypass |
| `desktop-beta-stage` | Entire alpha publication, CDN staging and beta-feed promotion | Main only, automatic, no admin bypass |
| `desktop-stable-stage` | Entire stable publication, CDN staging and stable-feed promotion | Main only, Sam approval once, no admin bypass |

The old environment names are retained to avoid re-provisioning secrets. The
separate `*-promote` and `whipcode-*` publishing environments are not in the new
route. Approval must precede public staging. Signing secrets stay in the signing
environment; R2 secrets stay in the appropriate final publishing environment and
are passed only to storage steps, not dependency installation scripts. See
[Desktop signing/storage configuration](desktop-releases.md).

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
For example: `desktop/beta/darwin/arm64/v1.0.0-alpha.7/whipcode-desktop-darwin-arm64.zip`.
The channel's `RELEASES.json` discovery URL remains fixed. Preserve existing
historical object URLs and feed entries; never overwrite or rename old releases.

The app inside still has its normal Whip/Whip Beta identity. `install.sh`,
`SHA256SUMS`, `artifact-manifest.json`, `RELEASES.json` and evidence/notices retain
their conventional names. This naming scheme does not add supported platforms.

## Complete artifacts and publication order

The existing candidate verifier owns one exact inventory: four CLI binaries,
installer, signed Desktop archives, update metadata, evidence/SBOM/notices,
checksums and attestation. Hash final signed bytes, reject missing/extra/duplicate
files, and bind source/version/channel/runtime identity. CLI consumers require
the CLI subset and accept additional well-formed Desktop/evidence assets; they
still verify selected bytes and safe, unique checksum records. The candidate has
16 files: 14 payload/evidence files, a manifest hashing those 14, and one
`SHA256SUMS` containing 15 entries including the manifest, never itself. GitHub
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
# A main push does this automatically when enabled.
gh workflow run publish-cli.yml --ref main -f channel=alpha

# Deliberate stable candidate; final environment approval is still required.
gh workflow run publish-cli.yml --ref main -f channel=stable
```

Verify the run's SHA/tag/version, full inventory, checksums and attestations;
clean CLI installation and Chromium/daemon startup; signed Desktop installation,
matching backend, and update-feed discovery. Use disposable homes and candidate
apps, not `task update:local` against a user's installation. Record both GitHub
release and feed outcomes. Desktop never updates an SSH/URL host silently.

Before v1 stable, install explicitly with `WHIPCODE_CHANNEL=prerelease` through
`main/install.sh`. Pre-unification alpha installers that insist on six assets may
need reinstalling from that current script; no compatibility shim is maintained.
See [setup](setup.md#standalone-releases-and-updates) and the
[manual reset checklist](team-reset.md).

For emergency stop, disable `publish-cli.yml` and cancel active/queued runs.
`WHIP_RELEASE_ENABLED=false` alone is not immediate cancellation: GitHub may have
resolved it before approval. Live workflow-state checks precede public effects,
but interrupted requests can already have changed external state. Inspect before
retrying. Do not weaken source, signing, artifact or environment checks to resume.
