# CLI release operations

> Cutover status (2026-09-24): publication is intentionally paused by Sam's
> decision. GitHub rejects its built-in Actions App as a restricted tag creator
> (HTTP 422). Both release flags remain false and new `v*` creation is blocked.
> Before enabling the flow below, configure an explicitly approved release
> identity (for example a dedicated repo-only GitHub App) and validate its
> creation-only authority. Do not relax main protection or reuse old workflow IDs.

`whipcode` is the sole CLI; `main` is the sole maintained source. Historical
branches/tags are records, not release or upgrade paths. This runbook describes
the clean-project workflow; it does not authorize publication or claim that
repository settings and external credentials have already been configured.

## Contracts

| Product/artifact | Version and trigger | Discovery and ownership |
| --- | --- | --- |
| Standalone CLI prerelease | Validated current main push after enablement; `v1.0.0-alpha.N` | Explicit prerelease channel, never GitHub latest |
| Standalone CLI stable | Deliberate workflow dispatch on main, `channel=stable`; initially `v1.0.0` | Stable channel and GitHub latest |
| Desktop | Approved current main commit tagged `desktop-v<semver>` | Separate beta/stable feeds; app and managed backend update together |

`.github/workflows/publish-cli.yml` is the single CLI orchestrator. The retired
`release-whipcode.yml` workflow ID stays disabled; do not re-enable it or rerun
historical jobs to release the new product. It calls
the same `ci.yml` and `security.yml` validation used for PRs/main. A release
renderer is built once and its provenance-verified artifact feeds the four
standalone targets: Linux/macOS, x64/arm64. Assets are `whipcode-<os>-<arch>`,
`install.sh`, and `SHA256SUMS`. The publisher refuses missing/inconsistent sets
and uses the exact validated source; no old-product identity matrix exists.

`N` comes from the orchestrator's run number; gaps are expected. Semver ordering
is numeric (`alpha.10` follows `alpha.9`). Stable publication and advancing the
base version are deliberate changes, not automatic consequences of tagging.
Publisher-created CLI tags do not start a second publication graph. Desktop
retains separate signing, native acceptance, attestation, staging, and feed-last
promotion: see [Desktop releases](desktop-releases.md).

The obsolete Loupe workflows and configuration are removed; no replacement
advisory AI review pipeline is part of this release flow.

## Enable only after the clean baseline is accepted

1. Land the clean identity/installer/shared-validation changes through PRs.
   Verify real hosted check names and outcomes, including aggregate `go`,
   `govulncheck`, and `codeql`, plus the separate **CodeQL** findings check
   (App 57789); reusable-workflow prefixes must match protection.
   Local `task ci` alone is not the complete hosted gate.
2. Record the full accepted clean commit SHA as repository variable
   `WHIP_RELEASE_BASELINE`. Candidates must descend from it and equal current
   main. Main ancestry alone could authorize pre-reset source and is insufficient.
3. Audit effective branch/tag rules, workflow permissions, release actor,
   environments/reviewers, old publisher/dispatch access, secret scopes, and
   external integrations with their owners. Protect existing tags from changes;
   retire old execution paths without deleting history or published assets.
4. Configure `whipcode-alpha` for trusted main prereleases and `whipcode-stable`
   for Sam's explicit stable approval (no admin bypass); both permit only main.
   Verify PRs have no publication credentials. Confirm Desktop signing and both
   **stage and promote** approvals separately; stage uploads are already public.
5. Only with explicit authorization, set `WHIP_RELEASE_ENABLED=true`.
   `WHIP_DESKTOP_RELEASE_ENABLED=true` is an additional independent Desktop gate.
   Keep publication disabled until all checks, settings, and credential ownership
   are verified. Do not create release tags just to test preparation.

## Publish and verify

- Let a validated current-main push produce an alpha, or deliberately dispatch
  the CLI workflow from main with `channel=stable` for `v1.0.0`. A superseded SHA
  is skipped; do not bypass source guards to publish it. Approve the concrete
  stable candidate only after its shared validation completes.
- Inspect the run's source SHA, version, asset set, and checksums. Confirm alpha
  remains prerelease/not-latest, or the approved stable becomes latest.
- Test a fresh temporary-home installation and the actual embedded web renderer.
  Verify `whipcode --version`, daemon socket/status, explicit web startup, and
  update ownership. Verify Desktop-managed binaries refuse standalone update.
  Do not run `task update:local` against a user's installation as release testing.
- Record run URL, source/baseline, tag, approval, asset digests, and acceptance.
  The recommended public entrypoint is `main/install.sh`; no branch-pinned or
  old-install compatibility URL remains. Before v1 stable, installation requires
  explicit `WHIPCODE_CHANNEL=prerelease`; stable requests fail rather than choose
  old tags. See [setup](setup.md#standalone-releases-and-updates).

## Failures and retries

Never move a release tag, overwrite a published asset, or delete a release to
retry it. Retry only the same source/version and a matching unpublished draft;
conflicting identity or bytes must fail. If source must change, validate and
publish a new version. Preserve old immutable downloads as historical records.

If publication or an external service fails, inspect the existing state before
retrying; an interrupted job may already have uploaded assets. For an emergency stop, **disable the publication workflows and cancel active
and queued runs** through the authorized maintainer process. The publisher
rechecks live workflow state before side effects. `WHIP_RELEASE_ENABLED=false`
is an admission gate, not immediate cancellation: expressions may have been
evaluated before approval. Never weaken a check to resume publication.
Pre-reset machines follow the [manual reset checklist](team-reset.md), not an
in-place installer or database migration.
