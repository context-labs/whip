# Docker onboarding validation

Completed September 9, 2026 on macOS with OrbStack, Linux arm64, Docker 29.4.0,
Node 24, and Go 1.27.0. Tested the working files on top of
`7058e007fec0a4f2e926f3b26dcb9e06ed75be5b`, including uncommitted onboarding changes.

## Delivered workflow

`task onboarding:docker` builds the current local directory and starts a fresh,
unprivileged container with a TUI and embedded production web app. Both use one
daemon. The launcher binds port 4000 to host loopback, forwards only terminal
presentation variables, and uses no host mounts or persistent application volume.
It reports the source version, exact image ID, renderer digest, and container name.
Quitting removes application state; build caches remain.

The README documents prerequisites, browser state reset, the trust prompt, shared
provider setup, the disposable `/workspace` project, device login, and diagnostics.
Container renderer metadata is explicitly local and cannot pass release provenance
verification. Ordinary Git-based production builds keep their existing checks.

## Automated checks

- `task check` passed: Go formatting, vet, repository checks and tests; protocol,
  SDK, client example, web and UI checks; asset packaging; local-update and Docker
  launcher tests.
- `node --test scripts/onboarding-docker.test.mjs scripts/renderer-artifact.test.mjs
  scripts/pack-web.test.mjs` passed all 36 tests, including subtests. Coverage
  includes linked worktrees, environment isolation, the exact new image ID,
  occupied-port preservation, unavailable/remote Docker, build/create/start
  failures, interruption cleanup, and release provenance rejection.
- The complete `internal/rlm` test suite passed inside the Linux container using a
  cross-compiled non-race test binary. The new memory regression and existing
  kernel limit/restart tests also passed three consecutive repetitions.
- `bash -n scripts/docker/onboarding-entrypoint.sh` and `git diff --check` passed.

## Actual container acceptance

- Fresh containers reported no usable providers and zero sessions. The TUI showed
  Inference.net first with the Recommended label. Fresh Chromium contexts showed
  the web welcome over both `localhost:4000` and `127.0.0.1:4000`.
- A deterministic local OpenRouter-compatible fixture served model discovery and
  inference. Through the browser, entered a masked API key, selected a model,
  preserved the first-message draft, chose `/workspace`, enabled Full Access,
  and sent the request. The real Starlark worker wrote `hello.txt`, read it with
  `shell.run`, calculated `6 * 7`, and streamed `Verified Docker onboarding: 42`.
  There were no browser errors or billable provider calls.
- The TUI refreshed and displayed the provider/model configured from the browser;
  accepting it opened the normal chat UI. Terminal resize and masked bracketed
  paste were exercised separately.
- Final successful browser runtime: `75489698ceebcf523eaafa1145afc069`.
  Renderer digest:
  `c7f3c6d5d680f56ca9a85e0a9e6f4ba573aef142e4014329cd486e2d5fd8d955`.
  Docker inspection confirmed user `whip`, no mounts, no CPU override, and only
  `127.0.0.1:4000:4000` published.
- A subsequent fresh run had a new runtime identity, no provider configuration,
  zero sessions, and no previous `hello.txt`. Normal quit, interruption before
  trust, and external `docker stop` removed the owned container and stopped the
  daemon. The launcher preserves interrupted exit status; ordinary TUI quit exits
  successfully.
- A real temporary linked worktree included a tracked HTML marker and an untracked
  Go source marker. The served page and binary both contained their respective
  markers. Introducing invalid Go in that worktree made the build fail and did not
  start the previous image. The temporary worktree was removed after testing.

## Measured build times

| Build | Observed duration |
| --- | ---: |
| First uncached Docker build | 81.62 seconds in BuildKit history |
| Changed linked-worktree source | 11.88 seconds in BuildKit history |
| Unchanged cached rebuild | 1.63 seconds CLI wall time; 1.25 seconds in BuildKit |

These measurements describe this local engine and cache state, not a startup SLA.
Runtime home directories are not image layers or cache inputs.

## Linux runtime correction found during acceptance

The existing worker address-space ceiling was the configured RAM budget plus
4 GiB. Go 1.27 had already reserved 5,496,176 KiB of virtual address space in an
observed worker, while resident usage was only 27,920 KiB. The old ceiling was
4,563,402,752 bytes. Small later allocations therefore failed with ENOMEM or a
runtime segmentation fault despite low RAM usage. Linux applies `RLIMIT_AS` to
virtual address space, including mappings and stack growth.
[Linux getrlimit documentation](https://man7.org/linux/man-pages/man2/getrlimit.2.html)

The fix adds measured startup reservations to the existing allowance and budget,
with checked arithmetic. The configured Go memory target and parent RSS enforcement
remain unchanged. The finite address-space limit still rejects oversized mappings.

`TestMemoryLimitPreservesRuntimeReservations` reserves virtual address space
without consuming that RAM, verifies that a small allocation succeeds, and verifies
that a mapping exceeding the ceiling fails. It failed against the original code
and passed with the correction. Each test runs in a subprocess because a hard
process limit cannot be restored by ordinary unprivileged code.

A diagnostic with 20 worker starts and three cells per worker found 19 failures
at 16 CPUs and 15 at 2 CPUs before the fix; the same diagnostic had zero failures
in all 40 runs afterward. The final browser walkthrough passed with no container
CPU override. The existing runaway-memory termination and restart tests passed.

## Scope of acceptance

Real account/device authorization, paid inference, and native macOS clipboard,
Keychain, computer-use, and desktop-installation behavior were not part of this
Docker acceptance. The README explains these platform boundaries. Browser storage
still requires a fresh private session when repeating web onboarding.
