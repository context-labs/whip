# `whip up <prompt>` — start the TUI with a first-turn prompt from argv

Branch: `whip-up-cmd` (worktree `.worktrees/whip-up-cmd`)

## What this does

`whip up <words...>` joins every argv token after `up` with spaces and opens
the interactive TUI with that text submitted as the first user turn — the
exact submission path of a typed message (`submitTurn` with `authored=true`,
so it lands in up-arrow history and the transcript).

## Goal

Type the prompt at the shell, land in a running whip session:

```
whip up fix the flaky test in internal/agent
```

## Non-goals

- Non-interactive mode (`whip run` already covers headless one-turn use).
- Quoting/escaping smarts beyond `strings.Join(args, " ")` — the shell owns
  tokenization; we just stitch tokens back.
- Slash-command dispatch of the prompt text (a leading `/` is sent as a
  normal message; the input box's command parsing is a keypath concern).

## Design

Surfaces touched: CLI dispatch (`cmd/whip/main.go`), TUI startup
(`internal/tui/tui.go`). No new config, no persistence shape, no new tool.

- `tui.Run` gains an `initialPrompt string` parameter (only caller: main.go;
  tests build `model` directly and may set the field).
- `model` gains an `initialPrompt string` field. `Run` sets it from the
  parameter. `Init()` returns a cmd that emits a new `initialPromptMsg{}`
  when the field is non-empty; `Update` handles it by calling
  `m.submitTurn(m.initialPrompt, true)` (nil-guarded for headless tests that
  never call `Init`).
  - Why a msg and not a direct `submitTurn` call in `Run` before
    `tea.NewProgram`: the turn goroutine's `send` closure reads `m.prog`,
    which is only assigned after `NewProgram`. Calling it early would drop
    all streamed events. `Init` is the first hook that runs with `m.prog`
    installed (bubbletea awaits the returned cmd's completion, so the turn
    can block indefinitely without stalling the event loop).
  - Resume stays first: `--resume` replays history, then the queued initial
    prompt fires as the next turn — matches `whip run`'s prompt-after-resume
    precedent (`cmd/whip/run.go`).
- `main.go`: a `flag.NArg() > 0 && flag.Arg(0) == "up"` case joining
  `flag.Args()[1:]` with `" "`. Because Go's `flag` package stops parsing at
  the first non-flag token, flags must precede `up`
  (`whip -m kimi up do the thing`); the `up` handler deliberately reads raw
  post-`up` args so prompts may start with `-` (`whip up -m is a flag, right?`
  sends "-m is a flag, right?" — claude-style rest-args).

Prior art: `claude "<prompt>"` positional arg; `whip run`'s
prompt-from-argv join (`cmd/whip/run.go`).

## Test plan

- `internal/tui/up_test.go`: headless `model` + `initialPrompt` field set →
  `Init()` returns non-nil cmd; cmd's msg through `Update` submits the turn
  (agent busy / user message appended with `Authored: true`). Empty field →
  `Init` behavior unchanged (no msg).
- `cmd/whip/main_test.go`-style: join-args helper if extraction warrants a
  pure function; otherwise the dispatch is one `strings.Join` — covered by
  the tui test plus a manual smoke (`whip up hello`).

## Docs plan

- `docs/features.md`: new section (behavior → code → tests).
- README: add `whip up` to the usage list if one exists.

## Tasks

- [x] Explore surfaces, choose Init-kickoff design
- [x] tui: `initialPrompt` field, `Run` param, `initialPromptMsg` + Init/Update wiring
- [x] main: `up` dispatch
- [x] tests (`internal/tui/up_test.go`, race-clean)
- [x] docs/features.md + roadmap entry (README has no command list — nothing to add)
- [x] `task check`: all packages pass except **pre-existing, unrelated**
  `internal/browser` `TestProfileScanFindsPortFile` (needs a live Chrome with
  remote debugging on the machine; fails identically on unmodified main) and
  a pre-existing `whip-transcript-.md` test leak, also on main.
