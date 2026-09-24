# TUI acceptance evidence

Date: September 9, 2026. Branch: `codex/provider-onboarding`.

## Isolation

Built `./cmd/whip` into `/tmp/whip-onboarding-tui`. All interactive runs used a child-process-only temporary `HOME`, `WHIP_HOME`, and working directory beneath `/tmp/whip-tui-smoke.yFayvk`. `INFERENCE_API_KEY` and `OPENROUTER_API_KEY` were unset. No production account was used or changed. The installed application and shared production daemon were not restarted.

The clean case used normal generated defaults. The configured case used a loopback HTTP fixture with a synthetic key, one `fixture-model` catalog entry, provider ID `fixture`, and imports disabled. Its server recorded request methods and paths only. No raw terminal transcripts or key values are retained in this report.

## Actual PTY observations

| Scenario | Observation / assertion |
| --- | --- |
| Clean installation | Folder trust appeared first. The provider chooser then showed Inference.net first with one Recommended label, OpenRouter, and ChatGPT subscription login. No thinking/MCP questions appeared. |
| Skip and draft | Esc opened a draftable home. Typing a task and pressing Enter reopened provider setup without sending it. Direct SQLite inspection reported **0 sessions** while this setup was open. |
| Masked key | OpenRouter selection opened the masked key field. Typed and terminal-pasted synthetic text rendered only as asterisks. Cancel cleared the field. No key-validation request was submitted during this test. |
| Return to startup draft | Cancelling key entry, then choosing explore, restored the entire typed task. |
| Already configured | Restarting against the synthetic saved route skipped provider setup and showed the normal composer with **fixture-model / fixture**. SQLite route records matched that pair. |
| No automatic inference probe | The fixture recorded **GET /v1/models** only; no POST inference request occurred. |
| Connect from command palette | Opening Authentication preserved the existing composer draft. The provider chooser showed the current fixture selection and grouped it before unconnected presets. Masked terminal paste stayed in the connection dialog. |
| Focus restoration | Initial smoke found the old command palette remained underneath the provider chooser. Fixed by dismissing that navigation palette when opening provider setup. A rebuilt binary was rerun in the PTY; one Esc from the chooser returned directly to the composer with its exact draft and visible input cursor. |
| Cleanup | All temporary TUI processes exited. The fixture daemon was stopped with its own isolated `WHIP_HOME`; the HTTP fixture server was terminated. |

Example commands, using the actual temporary fixture paths:

```sh
go build -o /tmp/whip-onboarding-tui ./cmd/whip
env -u INFERENCE_API_KEY -u OPENROUTER_API_KEY \
  HOME=/tmp/whip-tui-smoke.yFayvk/user \
  WHIP_HOME=/tmp/whip-tui-smoke.yFayvk/profile \
  TERM=xterm-256color /tmp/whip-onboarding-tui
```

## Automated validation

`go test ./internal/tui -count=1` passed. After review fixes, `go test -race ./internal/tui -count=1` passed (8.740 s reported by the final package run), and `go build -o /tmp/whip-onboarding-tui ./cmd/whip` succeeded.

Focused cases in `internal/tui/setup_test.go` cover:

- Explicit usable routes bypass setup without default writes; sole detected OpenRouter requires one explicit default-pair confirmation.
- All presets, grouping, restrained promotion, search relevance, and narrow-width wrapping.
- Atomic model/provider default writes, revision conflicts, session-only selection, selected-provider catalog queries, and Change-model behavior.
- Masking and clearing keys; keeping secret paste out of the session draft and transcript.
- No-session exploration and draft preservation through typing, terminal paste, owner-bound clipboard completion, Esc, and reconnect.
- Existing-flow observation, cancellation, reconnect after a late begin, ignored stale replies, and ignored replies after dismissal.
- `/auth` and `/connect` use the same chooser; launching it closes the navigation palette and retains the task draft.

These tests and PTY checks prove the local interaction and routing handoff. Real provider authentication, entitlement, and paid inference were deliberately not exercised here; their host behavior is covered separately by service fixtures and the overall implementation validation.

## Final compatibility and credential-command review

Added two final regressions after the PTY pass:

- An unchecked credential command without a model recommendation remains an available connection. Inventory alone leaves catalog/key operations untouched; explicit provider selection loads only that provider's models, and choosing one persists the route without replacing the credential source.
- A compatible older host that omits readiness metadata shows an actionable host-update message. Startup and the connection dialog do not enter model confirmation or issue configuration writes against that older host.

Validation: `go test -race ./internal/tui -run 'TestSetup|TestConnect' -count=1` passed (1.895 s). The root validation run owns the final repository-wide gate after the source freeze.
