# `whip up` — defer the trust gate into the TUI for non-TTY stdin

Branch: `feat/whip-up-in-tui-trust`

## What this does

Today, `whip up <prompt>` in a context where **stdin is not a TTY** (piped,
cron, an editor spawning whip, `script`) can never show the folder-trust
prompt: `checkTrust` (internal/tui/trust.go:29) sees a non-char-device stdin
and returns an error, `tui.Run` aborts before the TUI opens, `main` prints
`whip: folder … is not trusted` and exits 1. The user never sees a dialog and
the `up` prompt is silently dropped.

The fix: **when stdin isn't a TTY, don't gate in `checkTrust`.** Open the TUI,
surface the trust question *inside* the session as a one-shot inline prompt
(the `namePrompt` machinery — same as `/fork`, `/rename`, `/auth`), and hold
the `initialPrompt` until trust resolves:

- **Approve** (enter / `y`) → record trust (`config.Trust`), then fire the
  held `initialPrompt` as the first turn — the "do the thing" the user asked
  for.
- **Decline** (esc / `n`) → **exit the session** (user decision: decline means
  "I don't want to run here," so don't strand them in an untrusted folder).
  The held prompt is discarded.

Trusted cwd or a real TTY: behavior unchanged (fast path in `checkTrust`, or
the existing pre-TUI `[Y/n]` gate).

## Goal

`whip up <prompt>` never silently dies on the trust gate. Either it can ask
(pre-TUI on a TTY, in-TUI on non-TTY) and proceed on approval, or it lands the
user in a session with a clear trust prompt.

## Non-goals

- Changing `whip run`'s headless-implies-trusted stance (no TUI, no trust
  prompt — already the documented contract for scripting).
- A new modal widget — reuse `namePrompt` (ponytail: extend, don't build).
- Changing the interactive-TTY trust flow at all.
- Auto-trusting on non-TTY (that would drop the gate entirely; the point is to
  *ask*, just in the TUI where it's renderable).

## Design

Surfaces: `cmd/whip/main.go` (no change — it already passes `initialPrompt`),
`internal/tui/trust.go` (split the non-TTY branch), `internal/tui/tui.go`
(`Run`, `Init`, the `initialPromptMsg` handler, model field).

- **`checkTrust` outcome becomes tri-state.** Today: `(ok bool, err error)`.
  Add the third state for "couldn't ask (non-TTY), not yet trusted — defer to
  the TUI." Cleanest shape: a small result enum
  (`trustGranted` / `trustDenied` / `trustDeferred`) rather than overloading
  bool+error. `tui.Run` maps:
  - `trustDenied` → keep current `errors.New("folder not trusted")`.
  - `trustGranted` → as today.
  - `trustDeferred` → continue startup, set a new `model.trustPending`
    (the cwd string), do **not** error.
- **`Init` gating.** Today `Init` emits the `initialPromptMsg` kickoff
  whenever `initialPrompt != ""` (tui.go:1432). New: only emit when
  `trustPending == ""`. When `trustPending != ""`, `Init` instead opens the
  inline trust prompt via `openNamePrompt("trust <cwd>? [y/N]:", …)`.
- **Trust resolution** reuses the `namePrompt` Enter commit (tui.go:2881) and
  Esc cancel (tui.go:2683) paths:
  - approve → `config.Trust(cwd)`, then submit the held prompt: set
    `m.initialPrompt` back and emit a fresh `initialPromptMsg` (the handler at
    tui.go:1803 already guards busy + consumes one-shot) — or call
    `m.submit(text)` directly since we're on the UI thread and idle.
  - decline → drop the held prompt, stay in the session.
- **The held prompt lives on the model.** Add `heldPrompt string` (distinct
  from `initialPrompt`, which `Init`/the msg handler consume). On approve we
  route `heldPrompt` into a turn; on decline we clear it. Keeping it separate
  avoids racing the one-shot `initialPromptMsg` semantics.

### Why a `namePrompt` and not the `permDialog` modal

`permDialog` (permission.go) is tool-gate machinery: a gate goroutine blocks
on a reply channel, the dialog answers it. That couples to `tools.Gate` and a
channel handshake we don't need. The trust ask is a simple UI-thread y/n —
exactly what `namePrompt` already does for `/fork` and `/auth` (label prefix,
Enter commits `onOK(value)`, Esc cancels). Reusing it is the ponytail move.

## Prior art

- The `up`-kickoff load-bearing choice (fire from `Init`, not before
  `tea.NewProgram`, because the turn goroutine's `p.Send` needs `m.prog`) —
  see `.ai-docs/plans/whip-up/README.md`. This change respects it: the deferred
  prompt still fires via `initialPromptMsg` after `m.prog` exists.
- claude-code's per-folder trust dialog (referenced in trust.go) — we keep the
  same gate, just render it in-TUI when the pre-TUI terminal isn't available.

## Test plan

- `internal/tui/up_test.go` (extend):
  - deferred trust + approve → `config.Trust` recorded, held prompt submitted
    as first turn (busy, authored user message, history).
  - deferred trust + decline → held prompt dropped, no turn, session alive.
  - trusted cwd (no `trustPending`) → `Init` still emits the kickoff as today.
  - `trustPending` set → `Init` opens the namePrompt and does *not* emit the
    kickoff.
- `internal/tui/trust_test.go` / `internal/config/trust_test.go`: the tri-state
  mapping (granted / denied / deferred) — pure logic, unit-testable.
- Race-clean under `go test -race ./internal/tui`.

## Docs plan

- `docs/features.md`: extend the `whip up` section — the trust gate now asks
  in-TUI when stdin isn't a TTY, holding the prompt until approval.
- `docs/roadmap.md`: the `whip up` checkbox (line 136) gains a note.
- README: no command-list change needed (matches the `whip up` plan's call).

## Tasks

- [x] tri-state trust outcome in `internal/tui/trust.go` + `Run` mapping
- [x] model `trustPending` + `heldPrompt` fields; `Init` gating + namePrompt open
- [x] approve/decline resolution — approve: `config.Trust` + submit held prompt; decline: drop + `tea.Quit`
- [x] tests (`up_test.go`: deferred-open/approve/decline; `trust_test.go`: granted/defer/dev-tty) — race-clean
- [x] `task check` green (`internal/browser` `TestE2EDedicated` Chrome tempdir flake is pre-existing, passes on retry, zero browser files touched)
- [x] docs/features.md + roadmap note

## Deviations from the design above

- **Terminal selection got smarter.** Instead of "stdin non-TTY → defer,"
  `checkTrust` now asks on `/dev/tty` when stdin is piped (so `git diff | whip
  up` still prompts on the controlling terminal) and only defers when *no*
  terminal is available at all. `trustStdin`/`trustDevTTY` are package vars so
  tests inject a pipe + a stub tty.
- **Decline = exit** (per user), not stay-open: `trustAnswerMsg{approved:false}`
  returns `tea.Quit`.
- **`namePrompt.onCancel` hook added** (fork.go) so Esc on the trust prompt is
  treated as decline, not a silent cancel.
- **Detached send:** the trust callbacks run on the UI thread (namePrompt
  commit inside Update), so the `trustAnswerMsg` send is `go m.prog.Send(...)`
  to satisfy `whipvet`'s `uilock` and avoid self-deadlock.
