# Dependency security repairs for the backend reorganization

The October 16, 2026 rerun of #320 and #321 found vulnerabilities in the pinned
Go toolchain and npm dependency tree. These repairs are separate from the
behavior-preserving reorganization. SDK/protocol sources and database migrations
remain unchanged.

- Go 1.27.2 replaces 1.27.0. The refreshed `govulncheck` reports no reachable
  vulnerabilities; findings in unused dependency code remain visible.
- `golang.org/x/tools` 0.50.0 and `golangci-lint` 2.14.0 support the patched
  compiler's export format. Older versions fail before running the analyzers.
  The corresponding Go module minimums move with `x/tools`; analyzer rules and
  coverage requirements remain unchanged. Four equivalent `CutLast` replacements
  satisfy newly reported modernization findings in the agent tests and TUI.
- TanStack Start 1.168.60 applies the same targeted version update proposed by
  [#328](https://github.com/context-labs/whip/pull/328). The docs router is aligned
  to Start's matching 1.170.41 version so prerendering resolves compatible SSR
  exports; the desktop/web app's router pin remains unchanged.
- The existing compatible ranges resolve patched `compression`, `shell-quote`,
  and `source-map-js` releases in the root lockfile.

Two packages have no patched npm release. Root overrides temporarily pin reviewed
source archives from **pending, unmerged upstream PRs**:

| Package | Upstream fix | Immutable revision |
| --- | --- | --- |
| braces | [Bound parser and AST depth](https://github.com/micromatch/braces/pull/88) | `a795ee83ecd011131b2d618fe754ac5a8e124dae` |
| node-forge | [Validate nested DigestAlgorithm length](https://github.com/digitalbazaar/forge/pull/1152) | `ceba34402e329f0365134f23fe19898756527d65` |

These archives are fetched from the upstream repositories and recorded with
SHA-512 integrity in `package-lock.json`. They are not published registry
releases or floating branches. The braces archive includes unreleased upstream
parser fixes as well as the depth fix, so product/build suites must pass before
landing this repair.

## Verification and audit behavior

Run `npm run audit:production`. It first runs security regression tests and then
the existing `npm audit --omit=dev --audit-level=high` scan.

The pinned braces archive still identifies itself as 3.0.3. npm consequently
reports [GHSA-vfj7-8cjw-p6xm](https://github.com/advisories/GHSA-vfj7-8cjw-p6xm)
even though its source is fixed. The audit script recognizes only that exact
advisory revision after checking the override, lockfile origin and integrity,
absence of duplicate package copies, installed version and patched-file hashes.
It reports the finding as verified patched; it does not call the raw audit clean.
Its transitive dependents inherit that disposition only through the reported
advisory graph. Other high/critical advisories, revised advisory metadata,
unexpected dependency copies, missing patches, malformed reports and audit
service failures still fail the gate. Moderate findings remain visible at the
existing high-severity threshold.

Tests exercise deep/cyclic brace patterns and ASTs, ordinary brace expansion,
valid RSA signatures, malformed nested RSA DigestInfo, unknown advisories,
registry fallback and patch tampering. The SDK job runs this gate before the
existing frontend and SDK acceptance checks.

The compiler update also changes statement weights in coverage profiles. On
identical source, Go 1.27.0 records 38,455 statements and Go 1.27.2 records 37,134;
702 blocks receive different weights. The hosted tests all passed, but coverage
moved from 90.2% to 89.8%. The 90% floor is retained. Additional regressions cover
provider image/request encoding, malformed responses and continuation limits,
truncation and thinking preservation, cancellation of waiting client calls, and
bounded presentation copy isolation and UTF-8 handling. These tests change no
runtime behavior.

## Returning to registry releases

When upstream publishes fixes, review the release against these patches, remove
the corresponding override and regenerate the lockfile. Remove the associated
pin verification and, for braces, its single-advisory disposition at the same
time. Retain the behavioral security regressions. Run the audit and affected
product/build suites; do not change a version or archive merely to suppress an
advisory.
