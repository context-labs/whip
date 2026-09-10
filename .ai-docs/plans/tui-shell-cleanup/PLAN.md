# TUI shell cleanup: hide the agents dock behind /dock, trim the footer, add ctrl+r, unfreeze the spinner

Status: implemented September 10, 2026 (uncommitted on `codex/provider-onboarding`).
Goldens regenerated for the footer and the hidden dock; the layout oracle in
`hit_test.go` now keys on `ctrl+p` since `ctrl+x` left the idle footer.

Written against the working tree on `codex/provider-onboarding` at `e92ff15bf`
plus the uncommitted host-call lifecycle change
([plan](../tui-host-call-events/PLAN.md)). Line numbers below are from that tree.

## Request

From screenshots of the running TUI next to opencode, the user asked for:

1. No sub-agent rows under the input by default, and no status text on their
   right. In the screenshots that is the agents dock: `idle root`,
   `idle repo-scout`, and the right-aligned activity such as
   `In[9] files.write(path="hello.txt", …`.
2. The footer's right side shows only `ctrl+p commands`, as opencode does, plus
   a new `ctrl+r repl` binding beside it.
3. The busy spinner (`· ▮▮▮ · · ·  esc interrupt`) stopped animating.

Decision recorded September 10, 2026: the dock is kept, hidden by default, and
a `/dock` command shows or hides it. An earlier draft of this plan removed it.

Interpretation recorded here: "status text on the right" is the dock rows'
activity column, which the toggle hides with the rows. The footer's key hints
are item 2. opencode's usage and cost readout on the footer's right was not
requested and is out of scope.

## Findings

### The spinner freezes because one arming site drops its own command

- `internal/tui/thin_update.go:22-31`. `Update` arms the tick loop after every
  message when `busy && !spinning`. The `spinner.TickMsg` handler at
  `thin_update.go:562-572` lapses the loop when `!busy && !anyAgentRunning()`.
  The arm and lapse conditions disagree: a running child with an idle root
  lapses nothing but also arms nothing.
- `thin_update.go:76-80`. The `clientUpdateMsg` handler has a second arming
  site for that case: it sets `spinning = true` and appends `m.spin.Tick` to a
  local `commands` slice.
- `thin_update.go:96-106`. When the update carries an `Event`, all three
  returns build `tea.Batch(waitClientUpdate(m.client), command)` and never
  include `commands`. The tick is dropped, `spinning` stays true, and nothing
  can re-arm the loop for the rest of the session: `Update`'s post-hook only
  arms when `spinning` is false.
- Trigger matches the screenshot: sub-agents running, root turn active, bar
  frozen. Elapsed-time columns on agent rows ride the same tick and freeze too.

### The agents dock

- `internal/tui/agents.go:197-215` renders it whenever `!leftVisible()`; there
  is no user control. `compose.go:57,96-98,115,171` budgets its rows and
  `tui.go:1416-1419` appends it under the input.
- `client.go:1231-1237` (`ctrl+t`) and `client.go:1243-1247` (↓ on an empty
  input) focus the tree regardless of whether anything shows it. Once the dock
  can be hidden, focusing while nothing shows the tree would hand ↑/↓/enter to
  an invisible list.
- `/repl` is the model for a display toggle: a `case "repl"` in the slash
  command switch at `client.go:1886`, a registry entry at `registry.go:59`
  (`Category: "Display"`), and a startup config key read at `client.go:125-126`.
- Tests: `repl_panel_test.go:383-400` asserts the dock appears whenever the
  left column is absent; `client_test.go:853` reads `agentsDock()` after
  `ctrl+t` on an unsized model to see a blocked child.

### Footer and keys

- `opencode.go:327-331` (`footerRight`) lists `ctrl+x r`, `ctrl+x t`,
  `ctrl+x b`, and adds `ctrl+p commands` only at `sidebarMinWidth` or wider.
- `ctrl+p` already opens the command palette (`client.go:1214`). `ctrl+r` is
  unbound in the main input: the textarea does not bind it, and the only uses
  are inside the provider-setup overlay (`setup.go:525`, `setup_provider.go:575`).
- The REPL toggle lives in the `ctrl+x` chord table at `opencode.go:627`.
  `registry.go:59` advertises `/repl` with keybind `ctrl+x r`.
- Tests: `routing_test.go:266,291` pin the current right-side hints at wide and
  narrow widths.

## Change

### 1. One arming site for the spinner

File: `internal/tui/thin_update.go`.

- Post-hook in `Update`: arm when `(mm.busy || mm.anyAgentRunning()) && !mm.spinning`.
  This mirrors the lapse condition in the tick handler.
- Delete the arming block at lines 76-80. Nothing else in that handler needs
  `commands` for the spinner.
- Leave the `command == nil` safety net at line 569. With a single arm site it
  only fires for a genuinely orphaned tick.
- Do not fold `commands` into the event-branch returns unless the update
  producer can set `StateChanged` and `Event` on one message. Check the
  producer before deciding; if it cannot, the slice only ever holds
  state-change commands and the returns are correct as written.

### 2. Hide the agents dock by default; `/dock` toggles it

Files: `internal/tui/tui.go`, `agents.go`, `client.go`, `registry.go`.

- Model field beside `sidebarHide` and `replPanel`:
  `dockShow bool // /dock: the agent tree under the input when the left column is hidden`.
  Zero value means hidden, which is the requested default.
- `agentsDock()`: return `""` when `m.leftVisible() || !m.dockShow`. Nothing
  else in the dock, its layout accounting, or its comments changes.
- Slash command, beside `case "repl"`:

  ```go
  case "dock":
  	m.dockShow = !m.dockShow
  	if m.dockShow && m.leftVisible() {
  		m.append(dimStyle.Render("(the dock shows under the input when the left column is hidden; ctrl+x b hides the column)"))
  	}
  	return m, nil
  ```

  `Update`'s deferred `layout()` re-measures the rows; no `recalcWidth()`.
- Registry entry beside `/repl`:
  `{Name: "/dock", Hint: "— toggle the agent tree under the input when the left column is hidden", Category: "Display"}`.
- `ctrl+t`: when nothing shows the tree (`!m.leftVisible() && !m.dockShow`),
  set `m.dockShow = true` first, then focus as today. Asking for the tree
  should make it appear.
- ↓ on an empty input: focus only when the tree is visible
  (`m.leftVisible() || m.dockShow`); otherwise fall through to `histNext()`,
  the existing no-children behaviour.

Skipped: a `dock` startup config key. `/dock` is a session toggle. If wanted,
it is one line beside `sidebar` and `repl` at `client.go:125-126` plus the
config field.

### 3. Footer right and ctrl+r

Files: `internal/tui/opencode.go`, `client.go`, `registry.go`.

- `footerRight` after `beforeSession` and `leaderPending`:
  `return []string{"ctrl+r", "repl", "ctrl+p", "commands"}`. No width gate; two
  hints fit any supported width.
- `client.go` key switch, beside `ctrl+p`:

  ```go
  case "ctrl+r":
  	next, command, _ := m.ocLeaderChord("r")
  	return next, command
  ```

  Verify the provider-setup overlay still receives keys before this switch so
  its own `ctrl+r` refresh keeps working.
- `registry.go:59`: keybind `ctrl+r`. Keep the `ctrl+x r` chord and its entry
  in the leader footer; the chord table is the discoverable list.

### 4. Tests

- `spinner_test.go` `TestBusySpinnerTicks`: add a block where the root is idle
  and a child in `clientView.agents` is `running`. `Update` must arm
  (`cmd != nil`, `spinning`). Then clear the child and confirm the next tick
  lapses. Because the handler's own arm is deleted, `spinning && cmd != nil`
  after any message is sufficient evidence the tick was returned.
- `TestAgentsDockReturnsWhenThePanelCannotShowTheTree`: first assert the dock
  is empty at `sidebarMinWidth-1` by default, then set `dockShow = true` and
  keep the existing assertions.
- New test beside it: `/dock` flips `dockShow` both ways; `ctrl+t` at 79
  columns with `dockShow` false turns it on and sets `agentsFocus`; ↓ on an
  empty input at 79 columns with `dockShow` false leaves `agentsFocus` false.
- `client_test.go:842-855`: unchanged. The unsized model has no left column,
  so `ctrl+t` turns the dock on and the `blocked` assertion still reads it.
  Confirm rather than edit.
- `routing_test.go` `TestFooterHintsFollowFocus`: wide and narrow idle footers
  both contain `ctrl+r repl` and `ctrl+p commands` and neither contains
  `ctrl+x t themes` or `ctrl+x b sidebar`. Narrow width stays 79.
- `repl_panel_test.go` `TestReplPanelToggleChordAndCommand`: `ctrlKey('r')`
  toggles `replPanel` like the chord.
- `TestNoAdHocStyles` and the padding ratchet (`literalPadBaseline` 34) must
  not rise. The one new `dimStyle.Render` mirrors the `/repl` note.

Run:

```bash
go test -race ./internal/tui
```

```bash
task check
```

### 5. Docs

`docs/features.md`:

- Lines 543-544: "On narrow terminals the same rows sit under the input."
  becomes "When the left column is hidden or the terminal is narrow, `/dock`
  shows the same rows under the input; they are hidden by default."
- Line 545: `ctrl+r` (or `ctrl+x r`, `/repl`, config key `repl`).
- Lines 569-570: "under the agents dock" becomes "under the agents dock when
  `/dock` shows it"; the right side "lists `ctrl+r repl` and `ctrl+p commands`".

## Optional visual check

The tests assert footer and dock text. For a look at the result, use the
frame-dump recipe from the shell redesign: an env-gated throwaway `_test.go`
that dumps styled frames, converted with the scratchpad `ans2html.py`, viewed
in the in-app browser at 1300×740.

## Out of scope

- opencode's token and cost readout on the footer's right.
- Removing the `ctrl+x t` and `ctrl+x b` chords. They stay reachable through
  the leader footer and the palette; only the idle footer stops advertising them.
- A startup config key for the dock, or a keybind for `/dock`.
- Any change to the agent rows themselves.

## Completion criteria

1. By default nothing renders under the input except the footer band. `/dock`
   shows the agent tree there when the left column is hidden, and hides it again.
2. The idle footer's right side reads `ctrl+r repl  ctrl+p commands` at every
   supported width. `ctrl+r` toggles the REPL panel.
3. The spinner animates while the root or any child runs, lapses when neither
   does, and never freezes after a child-only period.
4. `go test -race ./internal/tui` and `task check` pass.
