# Mobile merge evidence — 2026-09-08

The approved [merge plan](MERGE-BACK.md) was executed in
`/Users/samheutmaker/Desktop/context-labs/src/rlm/whip-mobile-integration`
on `codex/mobile-integration`. The destination is the existing `whip-rlm`
checkout at `/Users/samheutmaker/Desktop/context-labs/src/rlm/whip`.

## Recovery and source accounting

- Destination starting commit: `879ad8782b63a40a31bffdff78c15c542583a1e1`.
- Backup ref: `codex/backup-whip-rlm-before-mobile-20260908T165714Z`.
- Mobile checkpoint: `0245ec9e6`, on `mobile-app`, containing all authored mobile
  work and documentation. Its original worktree remains intact.
- Before staging, a binary patch, untracked-file archive and manifest were saved
  under the original worktree's ignored
  `.ai-docs/plans/mobile-app/artifacts/merge-backup-20260908T165714Z`.
- Added the missing native-module Android build exclusion. Generated output,
  dependencies, credentials and signed/native build artifacts were not committed.
- The integration uses a normal merge with both histories; it does not reset the
  destination or replace its newer desktop/distribution implementation.

## Integration decisions

The 18 conflicted paths were resolved by behavior. Desktop scripts and the
renderer provenance step remain; mobile scripts and pure presentation exports
are added. SDK directory picking, expected runtime identity, mobile lifecycle,
crypto injection, native errors and permission recovery coexist. Web/desktop
retain independent clients and drafts for multiple hosts; mobile retains its
single-active-host runtime.

The web keeps single-choice text/option replacement and correct final-page skip
submission, while one-item batches now use the batch wire contract. The newer
TUI already includes the relevant batch fixes; obsolete copied code was not
reintroduced. Fixture lifetime/external origin controls coexist with multiple
browser origins and retained failure evidence. Integration fixes pass a context
to the newer listener API and a runtime ID to submitted-input acknowledgement.
Mobile attaches and probes with the SDK's initial runtime pin; startup errors
retain actionable mobile wording.

The generated protocol was regenerated and matches the newer destination's
contract, including host directory picking. CI requires both desktop and mobile.
The combined EAS exclusions include desktop output/staging, renderer artifacts,
native projects and native-module build caches.

The lockfile was reconciled with npm and validated with a fresh `npm ci` plus
`npm ls --package-lock-only --all`. All pre-existing main package versions and
mobile native Expo/RN versions are retained. Hoisted transitive dependencies were
resolved for the combined workspace; an intermediate Jest `signal-exit` conflict
was corrected before acceptance.

## Validation

Node 24.14.1, npm 11.11.0 and Go 1.27.0 were used. Final logs and local native/
archive evidence remain under the integration worktree's ignored `artifacts`.

| Check | Result |
| --- | --- |
| `npm run generate`; contract drift in `task check` | Pass |
| `task check` after final lock reconciliation | Pass: formatting, vet, whipvet, Go tests, contracts, 254 SDK tests, example, 293 web tests, UI checks and packaging tests |
| `go test -race -tags=integration ./internal/daemon ./internal/acp ./internal/tui -run 'Test.*(Question\|Permission\|SDKFixture)'` | Pass |
| Built SDK + `WHIP_SDK_RACE=1 node packages/sdk/scripts/acceptance.mjs` | 33 tests pass |
| `npm run check:mobile`; `npm run test:mobile` | Pass; 145 tests / 19 suites |
| `npm run export:mobile` | Production iOS and Android bundles pass |
| Pinned Expo Doctor 1.20.4, `EXPO_OFFLINE=1` | 21/21 installed-SDK checks pass |
| `node apps/web/scripts/multiple-hosts.mjs` | Chromium and Firefox: nine workflows each pass |
| `npm run check:desktop`; `go test -race -shuffle=on -count=1 ./cmd/whip -run '^TestDesktop'` | Pass |
| `npm run package:desktop -- --renderer-ready`; desktop verifier | Pass; local unsigned/ad-hoc package, no publication |
| `npm run test:desktop` with the staged native helper | 66 native tests and 75 distribution/publishing/renderer tests pass; startup self-test passes |
| `node apps/desktop/scripts/smoke.mjs` | Pass: GUI exit preserves daemon, relaunch reattaches, independent URL hosts restore, no renderer errors |
| `scripts/test-distributions.py`; `scripts/test-install-whipcode.py` | Both binary identities, home/socket isolation, independent restart/update and 12 installer tests pass |
| EAS CLI 23.2.0 local archive inspection | 1,711 files; required native/shared sources present, excluded artifacts and credential paths absent |
| Independent adversarial merge review | No remaining actionable regressions |

Expo's online compatibility table had advanced to newer patches during the merge.
The mobile CI check now explicitly uses the installed SDK's bundled compatibility
table. An intentional SDK upgrade and corresponding native rebuild remain a
separate change; this does not disable the other Expo configuration checks.

The renderer digest is
`c193f2a25ad02652d272685189703df95ac813f32d8822cd4962a0b4bd3b4e32`.
It is byte-identical before/after the final lock reconciliation; its provenance
was refreshed before desktop packaging. Go's embedded assets and Electron's
renderer input passed byte verification.

## Native smoke and its scope

A new owned iOS 26.2 simulator, `Whip Merge Acceptance`
(`F5CA4966-78B3-4364-A7B1-2B881B3913A3`), ran an embedded production JS bundle
through official `@expo/repack-app@0.10.3`. All compiled iOS module versions and
native implementation files remain unchanged from the accepted simulator binary.
The complete fingerprint differs because dependency hoisting adds config-parser
source entries and the original worktree contains an Android manifest modified
by its native build. The native diff was inspected; those differences do not
change the iOS module ABI. This is a JS-only simulator check, not a new signed
phone build or proof of an OTA-compatible fingerprint.

Through private Tailscale HTTPS/WSS and isolated fake-provider daemons, the UI
passed connection testing, a descriptive HTTP 404 failure, cancellation during
an unresponsive request, normal attachment, message/response display, three-part
question review/submission with a skipped final page, and session creation with
an initial message. Force-quit/relaunch restored the saved host and unsent draft.
Replacing the endpoint with a second fixture was rejected both during startup
and Test Connection. Restoring the original host retained the same draft.
An independent SDK client verified the committed batch answer, created session
and working directory, no pending questions, and no accidental draft submission.
The final follow-up only refined startup identity-error wording; mobile types,
all 145 tests and both production exports were rerun afterward.

The simulator was shut down and the temporary Serve mapping was removed,
restoring the original empty mapping. Temporary fixture homes/processes were
removed. Terminal Ctrl-C also reached the first fixture child, so its cleanup
reported SIGINT instead of a graceful exit; removal was verified independently.
No production daemon, session, provider credential or installed phone app was
changed by this merge. Existing full device/network/accessibility and store
release gates remain separate from this source integration.

## Promotion

The integration merge is the commit introducing this evidence file, with the
starting destination and mobile checkpoint as its parents. Promotion uses
`git merge --ff-only codex/mobile-integration` in the main folder after rechecking
its clean status and unchanged HEAD. The destination keeps branch `whip-rlm`.
Checkout-local dependency refresh and a build/type smoke complete promotion;
full unchanged suites are not repeated there. Original and integration worktrees
and the backup ref are retained. No push, release tag or daemon rollout is part
of this operation.
