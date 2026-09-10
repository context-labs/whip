# Provider connection inside the normal TUI

Branch: `codex/provider-onboarding`

User approved implementation September 9, 2026, including removal of the separate
startup onboarding screen.

## Goal

After folder trust, open the normal themed TUI. A missing provider opens its
shared connection dialog over that same interface. Esc returns to the normal
composer; authentication retains its draft and never sends it automatically.
Keep usable existing routes, explicit CLI choices, saved session choices, and
Inference.net's restrained recommendation policy.

## Design

- Extend `internal/daemon/root_client.go` with explicitly deferred creation: the
  existing reconnecting client can query host services before a root exists.
  Starting the first session freezes its selected route and uses the existing
  stable create identity and reconnect/subscription machinery. No protocol change.
- `internal/tui/client.go`, `tui.go`, and `thin_update.go` launch one Bubble Tea
  program. Readiness and session preparation arrive as messages. Until preparation
  completes, the normal input remains draftable and execution stays gated.
- `internal/tui/setup.go` becomes only a shared floating dialog. Delete its
  alternate-screen View, separate program launcher, explore composer, duplicate
  draft, and startup-only quit handling. Delete the separate setup connection.
- Use existing themed dialog/list primitives for provider choices, selection,
  instructions, masking and bounded rows. The underlying composer/footer retains
  project context and a concise connection hint.
- Preserve cancellation/owner checks; late auth replies cannot replace another
  dialog or submit a prompt. Permissions are configured before any initial send.

Prior art remains the provider plan's cited OpenCode home and reusable provider
dialog, plus `docs/learnings/other-harnesses/opencode/opencode-ux.md`. This change
completes the continuous-interface behavior; it adds no providers or dependencies.
Web/desktop redesign, real account login and paid inference are outside this work.

## Implementation and acceptance

- [x] Deferred client creation, host queries/reconnect, stable create identity.
- [x] One TUI program and normal composer throughout setup; remove old flow.
- [x] Themed provider dialog, narrow terminal behavior, honest status and context.
- [x] Headless tests: clean/ready/explicit routes, cancel/reopen, draft and secret
  isolation, first-session permission ordering, failed and late operations.
- [x] Race tests and `task check`; focused adversarial review.
- [x] Real isolated Docker walkthrough with terminal rendering, first provider
  connection, first request, restart, and cleanup. Preserve any user-run container.
- [x] Update README, features, roadmap and concurrency documentation.


## Validation — September 9, 2026

- `task check` passed, including the full Go and client checks.
- `go test -race ./internal/tui ./internal/daemon` passed (TUI 9.1 s,
  daemon 100.5 s). Focused startup/provider/client regressions also passed.
- Adversarial review covered admission races, preparation retries, terminal
  failure recovery, delayed login polls, secret isolation, and short-terminal
  selection. Findings were fixed and regression tests added.
- Removed the standalone setup program and connection, duplicate draft/explore
  view, legacy authentication workers, numbered auth choice prompt, masked
  name-prompt branches, and their obsolete tests. `/auth` uses the shared dialog;
  inline-key compatibility now requires the same masked confirmation.

The final dirty checkout was built with `task onboarding:docker` into image
`sha256:02366f12142c06c8471f3e69de7fd38652ad5b96e94b334a60b60df30638bc1b`.
An isolated local provider fixture supplied a model catalog and streamed a
Starlark call evaluating `6 * 7`; no real account or paid inference was used.

The actual terminal walkthrough verified:

1. Clean host inventory and zero sessions before connection. The web endpoint
   served HTTP 200 at localhost:4000 and the SDK reached the same execution host.
2. Folder trust followed by the normal themed TUI with the shared provider
   dialog. Inference.net appeared first with its quiet recommendation.
3. Esc returned to the normal composer. A multiline-capable ordinary draft
   reopened setup on Enter and survived provider connection/model confirmation.
4. Checked masked OpenRouter key entry and model confirmation while resizing
   among 32×18, 48×18 and 80×24 terminals. Headless tests cover long selection
   lists, scrolling to the create-project option, and long error details.
5. Connection did not send the draft. Explicit Enter submitted it; the fixture
   received the tool result `{answer: 42}` and the TUI displayed the final answer.
6. A separately launched TUI resumed that session with its saved provider/model
   and transcript. A new TUI launch on the configured host skipped connection
   setup and used the saved pair.
7. Both attached test TUIs exited normally; quitting the launcher TUI stopped
   its daemon and removed its container. The unrelated running container was
   left intact.

Browser account authorization remains covered by fake host/state-machine tests;
this walkthrough exercised the real masked-key path. No usability study was run,
as requested. Current behavior and usage are documented in README, features,
roadmap and concurrency guides.
