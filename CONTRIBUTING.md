# Contributing

Thanks for contributing to whip. This doc covers what CI runs on your pull
request and how to run the same checks locally before you push.

## Before you push

Run the local Go/runtime gate:

```sh
task ci
```

This composes four tasks. It is **not** a substitute for the complete hosted
platform/frontend/package/security graph:

| Task | What it runs | CI equivalent |
| --- | --- | --- |
| `task check` | `gofmt -s`, `go vet`, `go test ./...` | format + vet + tests |
| `task lint` | `golangci-lint run ./...` | the lint gate (`.golangci.yml`) |
| `task vuln` | `govulncheck ./...` | the dependency-vulnerability gate |
| `task acceptance` | real daemon/kernel recovery plus focused runtime contract tests under `-race` | Linux/macOS runtime jobs |

`task lint-fix` auto-fixes what golangci-lint can. The pre-commit hook
(`task hooks` to enable) runs `task check`.

## What CI checks on every PR

Two shared workflows gate pull requests: **ci** and **security**. The release
callers reuse these definitions rather than maintaining product-specific copies.
Target `main`; the archived branch is reference-only, not a maintenance target.

### `ci` — code health

- **gofmt -s** — the tree must be formatted (simplified).
- **go vet** — built-in static analysis.
- **golangci-lint** — the linters in [`.golangci.yml`](.golangci.yml). Beyond
  the defaults (errcheck, govet, ineffassign, staticcheck, unused) we enable:
  - *correctness*: errorlint (`errors.Is/As`, `%w`), nilerr, copyloopvar,
    predeclared, reassign, unconvert
  - *idiomatic modernization*: modernize, intrange, exptostd, usestdlibvars,
    perfsprint, misspell, dupword
  - *security & resources*: gosec, bidichk + asciicheck (trojan-source),
    bodyclose, noctx
  - *meta*: nolintlint (every `//nolint` must name its linter and give a reason)
- **go test -race -shuffle=on** — the portable package set (internal/browser
  and internal/tui need a Chromium sandbox / tty that hosted runners don't
  have; they stay local).
- **coverage floor (90%)** — total statement coverage of the portable set must
  stay at or above the floor. It's a self-contained `go tool cover` check (no
  external service), so it works identically on fork PRs. The floor is a
  ratchet: it only goes up as coverage improves. New code should come with
  tests; a PR that drops the total below the floor fails.
- **go mod tidy is a no-op** — commit a tidy `go.mod`/`go.sum`.
- **cross-compile** for the four release targets.
- **runtime acceptance on Linux and macOS** — owner-only sockets, real daemon
  detach/reconnect/recovery, RLM worker containment, deadlines, process-group
  cleanup, and memory limits.
- **Swift driver build** (macOS) so a driver-breaking change fails the PR, not
  the next release.

The graph also checks JavaScript production dependency vulnerabilities, SDK/web
renderer provenance and tests, packaged
installation and daemon/web acceptance for the sole `whipcode` product, Desktop
(including native acceptance), and mobile. The required aggregate `go` fails
closed when a required dependency fails or is skipped; it is not only a Go test
status. Consult [ci.yml](.github/workflows/ci.yml) for exact platform/job coverage.
Reusable workflow check-name prefixes must be verified against live branch rules.

### `security` — vulnerability & SAST

- **govulncheck** — fails if your change makes the module depend on a *reachable*
  known vulnerability.
- **CodeQL** (`security-and-quality` suite) — semantic analysis. Main requires
  both the Actions `codeql` status and the separate **CodeQL** findings check
  (App 57789); a successful scan is not equivalent to a clean findings gate.

Dependabot keeps Go modules and GitHub Actions current (grouped weekly PRs).

## Notes for outside (fork) contributors

- **Your PR runs with a read-only token and no secrets.** Both workflows use
  the default `pull_request` trigger and a least-privilege `permissions` block;
  there is no `pull_request_target` trigger, so forked code can never execute
  with a writable token or repository secrets. The first time you contribute, a
  maintainer approves the workflow run (GitHub's standard fork gate).
- **Fix lint locally first.** `task lint` catches the same findings CI will;
  it's much faster to iterate locally than through CI round-trips.
- **Don't suppress linters to get green.** If a gosec/staticcheck finding looks
  like a false positive, prefer fixing the root cause. If a suppression is
  genuinely warranted, use a targeted directive with a justification —
  `//nolint:gosec // why this is safe here` — which `nolintlint` enforces.
- **Keep `go mod tidy` clean** and don't bump dependencies unless your change
  needs it (Dependabot handles routine upgrades).

## Development and release boundaries

Use `npm ci && task build` for the packaged `./whipcode`, or `task install` for
GOBIN/GOPATH/bin. There is no separate `whip` build or identity-specific task.
Read [setup](docs/setup.md) and [frontend architecture](docs/frontend.md) before
changing the affected area. Run the focused workspace tests too; CI supplies the
native/OS coverage unavailable on a single workstation.

For publication, follow [CLI releases](docs/releases.md) and
[Desktop releases](docs/desktop-releases.md). Publishing is disabled until the
clean release baseline and explicit enablement are configured and verified.
The new CLI track starts at `v1.0.0-alpha.N`, then intentionally approved `v1.0.0`.
Old internal installs require the [manual reset](docs/team-reset.md), not migration.
