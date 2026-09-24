# Clean RLM cutover execution log

Started 2026-09-24, authorized by Sam after approving the
[cutover plan](2026-09-24-rlm-main-cutover.md). This is an operational record,
not a claim that all milestones are complete.

## Completed preservation and policy setup

- Refreshed remote refs: main and whip-v1 both
  `3c7ff8f76a37fe2c014477c0a0e23d0aa09b80d2`; RLM
  `9edec15d90b6d1dd7ec839acb5bea17f6bf3f634`.
- Verified full-history Git bundle at `.git/cutover-20260924/main-rlm-v1.bundle`.
  SHA-256: `77d09cb3b142eae688db1339b209091def382a9086f4f73671b714f18a484c2a`.
  Saved pre-change rulesets, workflow states and environments in that local folder.
- Published snapshots `archive/main-20260924` and `archive/rlm-20260924`.
- Ruleset `23953281`: whip-v1 prohibits updates, deletion and force-push; no bypass.
- Ruleset `23953283`: archive/* and retired whipcode-v* tags cannot be created,
  updated or deleted; no bypass (existing snapshots were created first).
- Ruleset `23953285`: v* release tags cannot be updated/deleted; no bypass.
- Temporary ruleset `23953286`: v* creation paused during clean cutover.
- Disabled old release workflow IDs `352788736` (whipcode), `341811731` (legacy),
  `353569113` (Desktop). No active publisher needed cancellation at this stage.
- Main ruleset `21579831`: required PR, zero approvals, strict GitHub Actions
  `go`, `govulncheck`, `codeql`, plus `CodeQL` findings from the Advanced Security
  App; no update restriction or bypass actors; force/delete protection retained.
  Effective current-user bypass is `never`.
- Disabled obsolete Loupe chat/review workflows (`347054875`, `346963083`);
  their old whip harness and installer will not be retained as a second product.
- Set `WHIP_RELEASE_ENABLED=false` explicitly until the new publisher is validated.
- Created `whipcode-stable` approval environment restricted to main. Added approval
  before both Desktop stage uploads as well as promotion. Sam is reviewer;
  self-approval allowed; admin bypass disabled. Desktop tag policies retained.

## Cutover PR and safety gate

[PR #163](https://github.com/context-labs/whip/pull/163) originally preserves both
histories with tree exactly RLM, using reconciliation commit
`df6e33f6ae3388b2caf6d5c44d2b5e795c8d08e9`. Both original tips were verified as
ancestors, and tree equality was checked before push. A disposable PR branch is
used, so GitHub auto-deletion cannot remove either preserved source branch.

PR validation surfaced eight CodeQL alerts (three high severity). Reviewed safety
patch `2a2a623d05623fb95e9f35d74b3aa5db309d593a` was pushed to PR #163: ANSI range
validation, allocation arithmetic removal, read-only child stdin, and explicit
OSC/EOF/continuation semantics with regressions. No suppression or CI bypass.
The candidate is now original RLM plus this explicit patch; both histories remain
intact. The six affected package suites and focused race regressions passed locally;
CodeQL rescan, full hosted CI, final merge and publication are pending.

## Implementation in progress

Follow-up branch: `cutover/clean-whipcode-20260924`.

- Shared CI/security and single v1 CLI release orchestration.
- Sole whipcode identity, installer/updater and fresh-install checks.
- Desktop namespace cleanup and fresh team reset documentation.
- Legacy intent/commit ledger; no old maintenance or upgrade paths.

## Validation checkpoint (18:13 UTC)

- Parent reran actionlint, shellcheck, 22 installer tests and 33 initial publisher
  tests successfully; subsequent reviewed publisher/public-stage regressions have
  37 passing tests in the implementation worker.
- macOS arm64 packaged acceptance passed: real installer, embedded manifest/file
  hashes, fresh config/session, daemon lifecycle, alpha.9→alpha.10 update,
  failed-update safety, Chromium and restricted SDK/gateway handshake. A stable
  candidate fixture also passed. No real user installation was modified.
- Desktop tests: 138 pass / 10 real-SSH-dependent skips; focused Node tests green.
- Full portable Go race suite passed at 18:18 UTC after the MCP test rename fix.
  Final integrated lint v2.13.1 reports zero issues; ACP/update race and complete
  TUI short suites passed. Protocol/SDK, themes and Desktop type checks passed.
- TUI short suite initially found intentional text snapshot drift and a report
  label mismatch. Thirteen one-line golden updates and the label fix passed the
  complete short suite. Golden padding is intentional.
- Independent review found/fixed channel-sensitive update cache behavior, exact
  asset discovery parity, alpha install notes, and immediate Desktop source/state
  rechecks before public writes. Desktop Linux uses canonical v-prefixed semver
  with strict matching candidate evidence; 8 candidate contract tests passed.
- Hosted PR #164 Linux acceptance found post-update endpoint readiness racing
  daemon version readiness. The test is being corrected to wait for the healthy
  gateway and endpoint with bounded diagnostics; renderer verification stays intact.

## Publication permission blocker

GitHub rejected adding its built-in Actions App (`Integration` 15368) to the
CLI tag-creation ruleset with HTTP 422: “Actor GitHub Actions integration must be
part of the ruleset source or owner organization.” The rule remains unchanged:
new v* tags are blocked. Sam requested investigating resolution, not relaxing it.
No broad bypass, new credential or open tag creation has been substituted.
After targeted research found no supported built-in-App fix, Sam explicitly chose
**finish the cutover and leave publishing paused**. Dedicated release-App setup
and first publication are deferred; they are not completion requirements for this
execution. CLI/desktop publication flags stay false and v* creation remains blocked.

Cleanup commit `8a8a9cbbe` and the merged safety patch are in follow-up
[PR #164](https://github.com/context-labs/whip/pull/164), initially draft pending
hosted validation and PR #163. The local working tree is clean at that checkpoint.

New publication must use a fresh workflow filename/ID; old workflows remain
permanently disabled rather than restoring their historical rerun surface.
`WHIP_RELEASE_ENABLED=false` and `WHIP_DESKTOP_RELEASE_ENABLED=false` remain set.
Stable/Desktop publication and wiping installations are not performed as part of
unattended validation. Main has not yet merged.
