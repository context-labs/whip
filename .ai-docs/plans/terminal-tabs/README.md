# Terminal tabs

Branch: `feature/agent-definition` (plan written 2026-09-11; implementation branch
`feat/terminal-tabs` off the merged definition work). Research and peer findings live in
[../../research/terminal-tabs.md](../../research/terminal-tabs.md). This document is the
shared source of truth for the build: keep tasks checked and deviations recorded here.

## Decisions

1. **PTY runs in the whipcode daemon**, not Electron main. Every connected client, on every
   host kind, gets the same shell path.
2. **Renderer is ghostty-web** (`ghostty-web@0.4.0`, MIT), loaded once per window. The only
   CSP change is `'wasm-unsafe-eval'` in `script-src`.
3. **All three host kinds** ship together: This Mac, SSH and URL. Unix-socket clients (This Mac,
   and SSH, whose forwarded socket reaches the remote daemon as a Unix client) are always
   allowed. Network listeners refuse `terminal.*` unless `WHIPCODE_NETWORK_TERMINALS=1`.
4. **Closing the tab kills the shell.** Reload, window hide, and a dropped connection keep it
   running; the daemon replays buffered output on reattach.
5. **Working directory** is the focused session's `cwd`; the host-level New terminal uses the
   daemon's home directory.
6. **Shortcut** Ctrl+` opens a terminal in the focused pane, as a preference beside the existing
   command and composer shortcuts.
7. **Human-only surface.** The agent never sees terminal input or output.

Defaults taken without a separate question (object in review if any is wrong):

- One live attachment per terminal; a second window that attaches takes it over and the first
  shows "Attached in another window".
- Terminal tabs move between panes but are never duplicated, like New Chat tabs.
- Closed terminal tabs do not enter the Reopen closed tab history, because their shell is gone.
- 16 live terminals per daemon, 1 MiB replay ring per terminal, 16 KiB per write, 32 KiB output
  chunks, cols and rows 1–1000, cwd at most 4096 bytes.
- The theme catalog has no 16-color ANSI palette (`internal/theme/resolve.go:18–39`), so the
  view maps background, foreground, cursor and selection from the applied theme and keeps
  ghostty-web's default ANSI 16.

## What this does

A person opens a real login shell as a tab in the Whip workspace, beside chat and REPL tabs,
in the working directory of the session they are looking at. The shell runs on the machine
that runs that session's daemon. Typing, resizing, scrollback, selection and copy behave like
a normal terminal. Reloading the window or losing the connection does not kill the shell;
the tab reattaches and the daemon replays what was missed. Closing the tab ends the shell.

## Why it matters

- Whip asks people to direct coding work and then inspect the result. Today inspecting means
  leaving the app for Terminal.app, or asking the agent to run a command for you, which spends
  a model turn and lands in the transcript. Claude Code desktop and OpenCode both closed this
  gap with an embedded terminal; it is the one surface where Whip's desktop is visibly behind.
- The architecture already reserved the slot. `docs/frontend.md` names "future terminal
  descriptors" as app-owned tab content, the split-views plan left an explicit extension point,
  and `docs/roadmap.md:166` lists the surface as later work.
- Doing it in the daemon rather than Electron is what keeps the rest of Whip's promises intact:
  one renderer for web and desktop, remote hosts that behave like local ones, and "the daemon
  owns work" so a UI reload never loses a shell.

## Goal

- A `terminal` tab kind in the shared workspace with the same order, split, move, close,
  capacity and persistence behavior as existing tabs.
- A daemon-owned PTY with reattach and replay, reachable over the existing JSON-RPC connection
  on Unix sockets and WebSockets.
- No new desktop bridge methods, no native module, no renderer platform branching.
- Green: `task check`, `go test -race` for the new package and daemon RPC tests,
  `npm run check`, `npm run check:web`, `npm run test:web`, the new browser fixture, and a staged
  Electron smoke check on This Mac and one SSH host.

## Non-goals

- Agent-visible terminals, `!` shell escape in the web composer, or transcript echo.
- Terminal-to-agent context ("send selection to chat"). A later plan can add it as a composer
  action once the surface exists.
- On-disk scrollback logs, redaction, or session-scoped terminal history across daemon restarts.
- Multiple simultaneous viewers of one shell, detached windows, or a terminal drawer outside
  the tab system.
- Windows ConPTY. The daemon builds for macOS and Linux today; `creack/pty` covers both.
- Mobile terminal input polish. The companion app only consumes `@whip/app/presentation`.
- xterm.js. It is the recorded fallback if ghostty-web fails acceptance; not built in parallel.

## Design

### Surfaces

| Surface | Change | Files |
| --- | --- | --- |
| Protocol | Five RPC operations, three notifications, minor bump to 6.5 | `internal/protocol/terminal.go` (new), `registry.go`, `types.go`, `docs/protocol-v2.md` |
| Daemon | New `internal/terminal` package; RPC handler; disconnect detach; network gate | `internal/terminal/{terminal,ring}.go` (new), `internal/daemon/terminal_rpc.go` (new), `server.go`, `network_server.go`, `daemon.go` |
| SDK | Generalized notification dispatch; `client.terminals` service | `packages/sdk/src/client.ts`, `terminals.ts` (new), `index.ts` |
| App | `terminal` descriptor, route, strip integration, view, shortcut, pane and tab menus | `packages/app/src/session-tabs.ts`, `session-tab-routing.ts`, `session-tab-strip.tsx`, `terminal-view.tsx` (new), `routes/h.$runtimeId.t.$terminalId.tsx` (new), `runtime.ts`, `settings/general.tsx`, `shell.tsx`, `workspace-views.ts` |
| Build | CSP directive, renderer boundary exemption, WASM asset | `internal/webassets/csp.txt`, `apps/web/vite.config.ts`, `packages/app/package.json` |
| Docs | Frontend guide, protocol, features, desktop, roadmap | `docs/frontend.md`, `docs/protocol-v2.md`, `docs/features.md`, `docs/desktop.md`, `docs/roadmap.md` |

Electron main, preload, `desktop-bridge.ts` and the mobile app do not change.

### Protocol (6.5, additive)

All operations are connection-scoped RPCs registered in `rpcOperations`
(`internal/protocol/registry.go:73`), execution `Ephemeral`, permission `host-terminal`. They
are not durable commands: a lost reply is answered by `terminal.attach`, never by replay.

| Operation | Params | Result |
| --- | --- | --- |
| `terminal.open` | `cwd` (absolute, optional), `cols`, `rows`, `root_id` (optional, title and cwd hint) | `id`, `shell`, `cursor: 0` |
| `terminal.attach` | `id`, `cursor` (byte offset; `-1` = tail) | `cursor` (first replayed byte), `exited`, `exit_code?`, `cols`, `rows`; replay follows as notifications |
| `terminal.write` | `id`, `bytes` (base64, ≤16 KiB) | `Accepted` |
| `terminal.resize` | `id`, `cols`, `rows` | `Accepted` |
| `terminal.close` | `id` | `Accepted` |

Notifications, added to `protocol.Events()` so the contract generator emits their types:

- `terminal.output {id, cursor, bytes}` — `cursor` is the absolute offset of the first byte.
- `terminal.exited {id, exit_code, signal?}`
- `terminal.detached {id}` — another connection attached, or the daemon is closing the terminal.

Errors: `-32003` not found or exited (with `exited: true` detail), `-32602` invalid params,
`-32009` limit reached, and a new `-32012 terminals_disabled` for gated network clients.

### Daemon: `internal/terminal`

Pure lifecycle at the core, transport at the edge. The package knows nothing about JSON-RPC.

```go
// Sink receives ordered output for one attachment. Output returns false when the
// receiver is gone; the terminal then detaches and keeps buffering.
type Sink interface {
    Output(id string, cursor int64, data []byte) bool
    Exited(id string, code int, signal string)
    Detached(id string)
}

type Options struct { Shell, Cwd string; Env map[string]string; Cols, Rows uint16 }

type Manager struct {
    mu        sync.Mutex
    terminals map[string]*Terminal
    limit     int           // 16
}

type Terminal struct {
    ID     string
    ptmx   *os.File
    proc   *os.Process     // Setsid + Setctty; killed as a group
    ring   *ring           // 1 MiB, absolute start/end cursors
    done   chan struct{}   // closed once by the reader when the process exits
    mu     sync.Mutex
    sink   *attachment     // nil when detached
    exit   *exitStatus     // set once, before done closes
}

type attachment struct { sink Sink; queue chan chunk; stop chan struct{} }
```

Lifecycles:

- **Open**: validate options, `pty.StartWithSize(cmd, &pty.Winsize{Rows, Cols})` with
  `cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}` (the same attributes
  `bashrun.startProcess` uses, `internal/tools/bashrun/bashrun.go:194`). Shell resolution reuses
  `bashrun`'s `userShell()` by exporting it as `bashrun.UserShell()`. Args `-l`. Environment is
  `bashrun.childEnvironment` plus `TERM=xterm-256color`, `COLORTERM=truecolor`; the existing
  `WHIP=1` marker stays so shell integration can detect Whip later.
- **Reader goroutine** (one per terminal, owner is the Terminal): `ptmx.Read` into a 32 KiB
  buffer, append to the ring, then hand the chunk to the attachment queue. It exits on read
  error, records the wait status, calls `Exited` on the current sink, and closes `done`.
- **Attachment goroutine** (one per attach): drains `queue` (capacity 64 chunks) into
  `Sink.Output`. The reader's send into a full queue blocks, which stalls the PTY exactly as a
  slow physical terminal would; the ring still records what was read. `Detach` closes `stop`,
  the drain goroutine returns, and the reader's pending send is released by selecting on `stop`.
  This keeps `serverConn.send` (`internal/daemon/server.go:788`) from ever hitting its 8 MiB
  outbound limit and closing the whole connection because of a `cat` on a large file.
- **Attach**: replaces the current attachment, calling `Detached` on the old sink first. Replays
  the ring from `max(cursor, ring.start)` in 32 KiB chunks through the new queue before live
  output, under the terminal mutex so no live chunk interleaves. Returns the first replayed
  cursor so the client can tell a gap from a seamless resume.
- **Close**: `SIGHUP` to the process group, `SIGKILL` after two seconds if still running, then
  remove from the manager. Exited terminals stay attachable for ten minutes or until the limit
  needs the slot; a timer owned by the manager collects them.
- **Shutdown**: `Manager.Close` closes every terminal and waits for reader goroutines. Called
  from `Daemon.Close` next to the executor registry.

Ring: a fixed `[]byte` with `start` and `end` absolute cursors; `Append` advances `start` when
full; `Read(from)` returns a copy from `max(from, start)`. Small, pure, and the first thing
tested.

### Daemon: RPC and gating

`internal/daemon/terminal_rpc.go` adds `handleTerminal(connection, request)` to the chain in
`Server.handle` (`server.go:349`). The connection implements `terminal.Sink` through a thin
adapter that calls `connection.notify` (`server.go:818`) and returns its boolean.
`Server.unregister` (`server.go:342`) calls `terminals.detach(connection)` beside
`executors.disconnect`, so a dropped WebSocket detaches without killing.

Network gating needs the connection to know how it arrived. `serveTransport` (`server.go:221`)
gains a `network bool` set by the WebSocket accept path in `network_server.go:18`. The handler
refuses `terminal.*` with `-32012` when `network && !options.NetworkTerminals`. The option comes
from `WHIPCODE_NETWORK_TERMINALS` through the same `buildinfo.Env` lookup as `NETWORK` and
`LISTEN` (`internal/buildinfo/buildinfo.go:44–46`) and is read once at startup, matching how the
other network settings behave.

### SDK

- `client.ts:356` currently dispatches four hard-coded notification methods. Replace the branch
  with a lookup against `manifest.events`: validate against the generated type for that method
  and notify listeners registered through `onNotification`. Unknown methods still disconnect.
  `ExecutorNotifications` becomes `Notifications` covering executor and terminal methods.
- `terminals.ts`: `class Terminals { open, attach, write, resize, close, onOutput, onExited,
  onDetached }`. `write` and `onOutput` convert with the existing `encodeBase64` and
  `decodeBase64` in `util.ts`. Exposed as `client.terminals`, alongside `client.host`.
- No state module. The view owns its terminal instance; the SDK stays a typed pipe.

### App: descriptor, route, strip

- `session-tabs.ts`: add

  ```ts
  export interface TerminalTab {
    readonly id: string; readonly kind: 'terminal';
    readonly runtimeId: string; readonly terminalId: string;
    readonly cwd: string; readonly titleHint: string;
  }
  ```

  to `SessionTab`; parse it in `parseTab` (identity checks as for `new`, cwd rules as for
  `NewChatTab.cwd`); add `openTerminal(runtimeId, terminalId, cwd, paneId?)` and
  `updateTerminal(id, { terminalId?, titleHint? })`. Replace the `tab.kind !== 'new'`
  narrowings (16 in `session-tabs.ts`, 6 in `session-tab-strip.tsx`, 2 in
  `session-tab-routing.ts` as of this plan) with an exported `isSessionTab` guard so terminal
  descriptors never reach root-keyed code (`visit`, `updateLocation`, `titles`, `close`,
  `purgeRoot`, `preferred`, `canOpen`, the `add` collision check, `hasSessionDraft` callers). `removeViews` skips terminal tabs when building
  `closed`. `split` on a terminal tab moves it, as it does for `new`.
- Route `routes/h.$runtimeId.t.$terminalId.tsx` renders the same missing-state fallback as the
  draft route. `session-tab-routing.ts` gains `terminalDestination(pathname)`, a `tabDestination`
  branch, and a `bindSessionTabs` branch that activates the matching open tab and otherwise
  shows the missing state without creating anything. `settingsBackDestination` and the desktop
  close-tab path already route through `tabDestination`.
- `session-tab-strip.tsx`: `matched` recognizes the terminal route; `title` uses `titleHint`
  or "Terminal"; `project` uses the cwd basename; status icon is `SquareTerminal`;
  `useWorkspaceViews` receives `visibleTabs.filter(isSessionTab)`; `panels` renders
  `<TerminalView>` for terminal tabs; `actions` offers close, close others, close right, move,
  move-to-split and "New terminal here" (same host and cwd); session tab menus and the pane menu
  gain "New terminal", using the pane's selected session summary `cwd` and falling back to the
  host home. The compact picker lists terminal tabs with the same metadata line.
- `shell.tsx` and `runtime.ts`: `terminalShortcut` preference (default `` Ctrl+` ``), a General
  settings row beside the existing two, and a palette entry "New terminal".
- Closing a terminal tab calls `client.terminals.close` and ignores the result; the descriptor is
  gone either way and a late failure has nowhere truthful to show.

### App: `terminal-view.tsx`

- Module-level `ghosttyReady = Ghostty.load(wasmUrl)` where `wasmUrl` is
  `import wasm from 'ghostty-web/dist/ghostty-vt.wasm?url'`; one WASM compile per window.
  `new Terminal({ ghostty, scrollback: 10_000, fontFamily: JetBrains Mono stack, theme })`.
- Mount: `term.open(el)`, `FitAddon`, `ResizeObserver` → `fit()`; `onResize` debounced 100 ms →
  `terminals.resize`. `onData` → `terminals.write`. Output listener for this `terminalId` →
  `term.write(bytes)` and remember `cursor + bytes.length`. `onTitleChange` →
  `tabs.updateTerminal(id, { titleHint })`.
- Attach on mount with `cursor: 0` after `term.clear()`, so a reloaded window redraws from the
  ring. On client reconnect (`ConnectionSnapshot.state` returns to `connected`) attach with the
  remembered cursor so nothing is drawn twice. A `-32003` reply renders "This shell has ended"
  with Restart, which opens a new terminal in the same cwd and updates `terminalId` in place.
- `terminal.exited` renders "Shell exited (code N)" with Restart; `terminal.detached` renders
  "Attached in another window" with Reattach.
- Focus: when the tab becomes the pane's selected view on desktop, `term.focus()` under the same
  rule the composer uses (not in compact layout, not over an open overlay).
- Keys: `attachCustomKeyEventHandler` returns `false` for the command, composer and terminal
  shortcuts so TanStack hotkeys receive them; Cmd+C with a selection copies through
  `platform.copy`; everything else reaches the shell. Cmd+W is an Electron menu accelerator and
  is unaffected. Paste relies on the DOM `paste` event on ghostty-web's hidden textarea;
  the Electron smoke check verifies it, because the permission handler in `main.ts` denies
  `clipboard-read` and a `readText` fallback would not work there.
- Theme: `background`, `foreground` from the applied theme colors, cursor from `primary`,
  selection from `element`; `// ponytail: theme catalog carries no ANSI 16; add roles to
  internal/theme when a theme needs its own palette`.

### Build

- `internal/webassets/csp.txt`: `script-src 'self' 'wasm-unsafe-eval'`. The renderer manifest
  and desktop asset handler read this file; the fixtures already fail on any violation.
- `apps/web/vite.config.ts`: the boundary guard exempts the one ghostty-web shim whose id ends in
  `ghostty-web/dist/__vite-browser-external-…js`; its body is an empty object and the browser
  path falls through to `fetch`. Nothing else about the guard changes.
- `packages/app/package.json`: `"ghostty-web": "0.4.0"`, exact. Justification: the only
  terminal renderer that runs under the existing style CSP; OpenCode ships a fork of it in
  production; API is xterm-shaped so the fallback to xterm.js is a rename plus a CSP change.

## Preserved, changed, not built

Preserved: every existing tab behavior and limit; chat and REPL tab code paths; the
`terminal.input` agent path and `stream.terminal.*` events; the desktop bridge at version 2;
Electron fuses, sandbox and permission handlers; the renderer boundary guard for Node and
Electron imports; the "zero injected style rules" contract.

Changed: CSP gains one directive; the SDK notification dispatch generalizes; `serveTransport`
learns whether a connection is a network client; protocol minor becomes 6.5.

Not built: see Non-goals.

## Prior art

- OpenCode `packages/core/src/pty.ts`: server-side PTY, `BUFFER_LIMIT` 2 MB ring with absolute
  cursors, `EXITED_LIMIT` 25, shell from `SHELL` with `-l`, `TERM=xterm-256color`.
  `packages/core/src/pty/protocol.ts`: cursor handoff after replay. `packages/app/src/context/
  terminal.tsx`: reattach with a saved cursor so only new output streams. `packages/app/src/
  components/terminal.tsx`: ghostty-web, `FitAddon`, `scrollback: 10_000`, 100 ms resize
  debounce. Whip keeps the ring and cursor idea, drops the client-side screen serialization
  (a full ring replay is at most 1 MiB), and moves the transport onto the existing RPC pipe.
- Claude Code desktop: PTY host outlives the renderer so closing the pane keeps the shell;
  docs limit the terminal to local sessions. Whip keeps the first property and avoids the second
  by hosting the PTY in the daemon.
- Whip `internal/daemon/interactive.go` and `executor.go:334–346`: keystrokes as base64 RPC
  params, connection-scoped `notify`, bounded queues.
- Whip `.ai-docs/plans/new-chat-tabs/README.md`: the task shape for adding a non-session tab kind.

## Test plan

Go (`task check`, plus `go test -race ./internal/terminal/... ./internal/daemon/...`):

- `internal/terminal/ring_test.go`: append, wrap, read from clamped cursor, empty read.
- `internal/terminal/terminal_test.go`: open `sh`, write `echo ok`, receive through a fake sink;
  resize observed by `stty size`; replay after detach and reattach with a gap; exit code and
  signal; close kills a grandchild `sleep`; limit refusal; blocked queue stalls the reader and
  detach releases it. Every goroutine test passes under `-race`.
- `internal/daemon/terminal_rpc_test.go` on `startTestServer`: open, attach, write, output
  notification round trip; disconnect detaches and a new connection reattaches with replay;
  second attach emits `terminal.detached` to the first; network connection refused without the
  environment switch and accepted with it; validation errors.
- `internal/protocol/registry_test.go`: new operations and events present, permission label
  known, minor bumped; `npm run check -w @whip/protocol` regenerates and interop-tests.

SDK (`npm run test -w @whip/sdk`): `packages/sdk/test/terminals.test.ts` on the transport
fixture: call shapes, base64 round trip, notification dispatch by manifest, unknown method still
disconnects.

App (`npm run test:web`):

- `session-tabs.test.ts`: parse and persist a terminal descriptor; close leaves no closed-history
  entry; split moves rather than duplicates; capacity counts terminals; legacy layouts unaffected.
- `session-tab-routing.test.ts`: terminal route activates an open tab, unknown terminal shows the
  missing state without allocation, back/forward between chat and terminal views.
- `workspace-views.test.ts`: terminal tabs never acquire root leases.
- `terminal-view.test.tsx` with `vi.mock('ghostty-web')`: attach on mount with cursor 0, write
  on data, resize debounce, output written in cursor order, exited and detached states, Restart
  replaces `terminalId` in place, shortcut keys pass through.

Browser (`apps/web/scripts/terminal-tabs.mjs`, Chromium and Firefox, real daemon fixture):
open a terminal from the pane menu, type `echo whip-ok` and Enter, assert a `terminal.write`
frame and a `terminal.output` frame whose decoded bytes contain `whip-ok`, zero CSP violations,
reload restores the tab and the replayed output contains `whip-ok`, close sends `terminal.close`
and a fresh attach returns not found. Screenshots saved to the results directory.

Desktop: `npm run test:desktop` unchanged. Staged Electron smoke check (recorded under
`evidence/`): This Mac terminal, paste, Cmd+W closes the tab, reload keeps the shell; one SSH host
terminal proves the remote path.

## Docs plan

- `docs/frontend.md`: tab kinds and the split-workspace paragraph; state ownership row
  "Terminal attachment: view-owned cursor, daemon-owned shell"; retention row for terminals;
  the CSP sentence; the dependency table row for ghostty-web.
- `docs/protocol-v2.md`: a 6.5 paragraph in the existing style.
- `docs/features.md`: rows in the web application table and a bullet under Daemon and clients
  naming code and tests.
- `docs/desktop.md`: one paragraph on where the shell runs and the network switch.
- `docs/roadmap.md`: add a checked "Terminal tabs" item; leave editing and code review unchecked.

## Implementation record (2026-09-11, branch `feat/terminal-tabs`)

Built in a separate git worktree because other live sessions had uncommitted edits to the
tab files in the main checkout. Deviations from the design above, all recorded here so a
reader of the plan is not misled:

- **No ten-minute timer for exited shells.** `Manager.Open` evicts the oldest exited terminal
  only when the 16-terminal limit needs the slot. Fewer goroutines, same bound.
- **Drain stop semantics.** A halted attachment stops before its next delivery even with
  queued chunks; only the delivery in flight completes. The queue-first rule still applies
  to the exit signal so a replayed exited shell never reports exit early.
- **Sink may block.** `serverConn.Output` waits while the connection's outbound bytes exceed
  half the limit, so a terminal flood stalls the shell instead of tripping `send`'s
  connection-closing overflow. Backpressure propagates through the terminal's 64-chunk queue.
- **Hangup, not just SIGHUP.** `Close` cancels the command (SIGHUP to the shell's group, SIGKILL
  after two seconds via `WaitDelay`) and closes the master, so the foreground job sees a tty
  hangup as when a terminal window closes. Verified with a foreground `sleep` grandchild.
- **WASM path.** ghostty-web exports the WASM at `ghostty-web/ghostty-vt.wasm`, not under
  `dist/`; the Vite guard exemption covers its inert `__vite-browser-external` shim.
- **Shortcut naming.** TanStack hotkeys spells the key as a backtick and the canonical modifier
  as `Control`, so the options are `Control+\``, `Mod+\``, `Mod+Shift+T`.
- **ghostty-web key handler semantics are inverted from xterm.js.** `attachCustomKeyEventHandler`
  returning `true` means "handled, do not send to the shell". The first fixture run typed into a
  shell that never received a byte because the handler followed xterm's convention; the view and
  its unit test now follow ghostty-web's.
- **Focus waits for the renderer.** The prompt arrives before the WASM finishes loading, so the
  view keeps a `ready` flag and only reports `live`/focuses once the terminal element exists.
- **Re-attach after renderer recreation.** The attach effect depends on the renderer's `ready`
  flag and the mount effect forgets the cursor, so a theme change or a replaced client object
  redraws the new canvas from the ring instead of leaving it blank until the next output.
  Found in the adversarial pass; the initial mount therefore attaches twice (cursor 0, then
  the last seen cursor), which the daemon treats as a same-connection re-attach.
- **Fonts.** Chromium never falls back to system fonts for Private Use Area code points, so
  powerline separators (U+E0B0…) drew as hollow boxes with the UI's JetBrains Mono stack.
  `terminalFontFamily` appends Nerd Font families (`Symbols Nerd Font Mono`, `JetBrainsMono
  Nerd Font Mono`, `MesloLGS NF`, `Hack Nerd Font Mono`, `FiraCode Nerd Font Mono`); installed
  ones supply the glyphs per character, absent ones are skipped. The view also waits for
  `document.fonts.ready` (2 s cap) because ghostty-web sizes cells from one measurement at open.
  The fixture prints the glyphs and saves `*-glyphs.png` for visual review.
- **Keystroke coalescing.** Each key was its own `terminal.write` RPC; typing faster than the
  round trip exceeded the SDK's 32 in-flight request cap and silently dropped characters
  (the glyph check lost everything past the 32nd byte). `createWriteQueue` keeps one write in
  flight per terminal, coalesces what arrives meanwhile, splits pastes at 16 KiB in order, and
  drops pending bytes when a write fails rather than replaying stale keystrokes after reconnect.
- **Browser fixture daemon** (`internal/daemon/v2_sdk_test.go`) enables terminals for network
  clients, because Playwright reaches it over WebSocket like a URL host would.
- **Plan document paths**: research and plan were copied into the worktree so the branch
  carries them.

Validation performed: `go test -race` on `internal/terminal` and the daemon terminal tests;
full `go test ./...`, `go vet`, `whipvet`; `npm run check` (protocol drift and interop, SDK
311 tests), `npm run test:web` (549 tests including the new store, routing and view tests),
`npm run check:desktop`, the production renderer build with the WASM asset, and the
`apps/web/scripts/terminal-tabs.mjs` fixture. Results and screenshots live in the session's
results directory; see the final report. Also performed: the staged Electron smoke on This Mac (task 8b). Not performed: the SSH-host
smoke (task 8c), which needs a remote host.

## Ordered tasks

- [x] 1. `internal/terminal` ring and lifecycle with tests, race-clean. Export
  `bashrun.UserShell`.
- [x] 2. Protocol types, registry entries, events, minor bump; regenerate `@whip/protocol`;
  registry and interop tests green.
- [x] 3. Daemon RPC handler, sink adapter, disconnect detach, network flag and gate, shutdown
  join; `terminal_rpc_test.go`.
- [x] 4. SDK notification generalization and `client.terminals`; SDK tests.
- [x] 5. App descriptor, store operations, route and routing; store and routing tests.
- [x] 6. Dependency, CSP directive, Vite exemption, `terminal-view.tsx` with mocked tests.
- [x] 7. Strip integration, menus, palette, shortcut preference and settings row.
- [x] 8a. Browser fixture in Chromium and Firefox: five workflows each (open from pane menu, type
  and read output under the production CSP, reload with replay, takeover and reattach, close
  ends the shell). Evidence in `evidence/`.
- [x] 8b. Staged Electron smoke on This Mac (`apps/desktop/scripts/terminal-smoke.mjs`): the
  palette opens a shell over the Unix socket, typed keys reach it, Cmd+V pastes through the
  native paste event, reload reattaches the same shell, and File > Close tab ends it. Evidence
  in `evidence/desktop-terminal.json` and screenshots. Cmd+W itself cannot be synthesized by
  Playwright, so the smoke clicks the real menu item; the accelerator path is the same handler.
- [ ] 8c. SSH-host smoke. Not performed: no remote host with a compatible Whip was available to
  this session. The daemon path is identical to This Mac (an SSH connection reaches the remote
  daemon as a Unix-socket client), so the remaining risk is environmental, not code.
- [x] 9. Docs and roadmap; ponytail pass over the diff; adversarial review of reattach races,
  queue stalls, close during replay, reload with a dead daemon, and renderer recreation
  (the review subagent was interrupted twice by app restarts; the pass was completed by hand).

## Open questions

None blocking. The defaults listed under Decisions are the remaining judgment calls; any of them
can be changed before task 5 without rework in the daemon.
