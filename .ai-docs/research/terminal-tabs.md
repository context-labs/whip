# Terminal tabs for the Whip workspace

Research record, 2026-09-10. Branch `feature/agent-definition`. This is research and a
recommended path, not an approved plan. Open questions are at the end; the plan goes in
`.ai-docs/plans/terminal-tabs/` once they are answered.

## Why

Claude Code desktop and OpenCode both render a real shell beside the conversation. Whip's
workspace already has the slot for it: the split-views plan left "a small, explicit extension
point for future terminal or REPL tabs", `docs/frontend.md` says "chat/REPL and future terminal
descriptors and renderers belong in app", and `docs/roadmap.md:166` lists a standalone terminal
surface as later work. The question is where the PTY lives and what draws it, because those two
choices decide whether the feature fits Whip's architecture or fights it.

## What Whip already has

- **Daemon owns work; clients observe.** `docs/frontend.md` principle 1. Closing a tab, reloading
  or quitting never cancels accepted work. Electron main owns "processes, sockets and OS effects";
  session and product state stay in the shared renderer (`docs/desktop.md`).
- **One renderer for web and desktop.** `apps/web` builds once; Electron serves the exact bytes.
  The Vite guard in `apps/web/vite.config.ts` fails the build if any module id contains `node:`,
  `electron`, `__vite-browser-external`, or the SDK's `node` entry.
- **Strict CSP, treated as a test contract.** `internal/webassets/csp.txt`:
  `script-src 'self'; style-src 'self'; style-src-attr 'none'; worker-src 'self' blob:` and no
  `wasm-unsafe-eval`. Browser fixtures in `apps/web/scripts/*.mjs` fail on any
  `securitypolicyviolation`, and `docs/frontend.md` requires "zero injected rules". The desktop
  asset handler (`apps/desktop/src/assets.ts`) already serves `.wasm` with the right MIME type.
- **A PTY runner in Go.** `internal/tools/bashrun` uses `creack/pty` for the agent's interactive
  bash path, resolves the user's login shell (`userShell()`), manages process groups, and forwards
  keystrokes from a channel. The daemon exposes it as the `terminal.input` runtime operation
  (`internal/protocol/runtime_registry.go:75`, base64 bytes, 4 KiB cap) and emits
  `stream.terminal.started/output/awaiting/completed` events. This path is per command, kills the
  child after 15 s of silence, and allows one terminal per session, so it is a pattern to reuse,
  not a lifetime to reuse.
- **Connection-scoped push.** `tool.invoke` and `hook.invoke` are notifications sent to one
  connection via `lease.conn.notify` (`internal/daemon/executor.go:338`). The SDK dispatches a
  fixed set of notification methods in `packages/sdk/src/client.ts:356` and rejects unknown ones.
- **Text-only transports.** Unix socket frames are newline-delimited JSON strings capped at 1 MiB
  (`apps/desktop/src/transport.ts`); WebSocket accepts text messages only
  (`packages/sdk/src/transport.ts`). Terminal bytes must be base64 inside JSON.
- **Three host kinds.** Local (Unix socket through Electron), SSH (remote Unix socket forwarded
  by the `whipcode _desktop-ssh` control connection), URL (direct WebSocket). The browser client
  only has URL.
- **Tab model.** `packages/app/src/session-tabs.ts` owns one v3 workspace: 4 panes, 32 views,
  20 closed entries, 64 KiB metadata. Tab kinds are `chat | repl | new`. The New Chat tab
  (`.ai-docs/plans/new-chat-tabs/README.md`) is the precedent for a tab that is not backed by a
  session root, and its task list is the template for adding a kind. `parseTab` drops unknown
  kinds, so an older build reading a layout with terminal tabs silently loses them and nothing else.
- **Themes carry terminal colors.** `docs/architecture.md:177`: the Go resolver owns terminal
  ANSI semantics; `host.themes.resolve` returns them. Verify field names before mapping.

## How the peers do it

Verified 2026-09-10 by reading source (OpenCode) and the installed app bundle (Claude Code).

| | OpenCode (`anomalyco/opencode`, HEAD 193de13) | Claude Code desktop (`/Applications/Claude.app` 1.52386.0) |
| --- | --- | --- |
| Shell | Electron 42 + SolidJS (moved off Tauri in PR #25822, 2026-05) | Electron 44.2.0 |
| Renderer library | `ghostty-web` fork (Ghostty VT parser in WASM, canvas 2D), `FitAddon`, hand-ported serialize addon, `scrollback: 10_000` | Unverified. No `@xterm/*` or `ghostty-web` in `app.asar`; UI loads from claude.ai |
| PTY host | The opencode **server** (`packages/core/src/pty.ts`, `bun-pty` under Bun, `@lydell/node-pty` under Node), not Electron | Electron **utility process** "Claude Desktop PTY Host" with `node-pty` 1.2.0-beta.14 prebuilds in `app.asar.unpacked` |
| Wire | HTTP `POST/GET/PUT/DELETE /pty[/:id]` + ticketed WebSocket `/pty/:id/connect?cursor=N`; raw UTF-8 text frames out, keystrokes in, one binary `0x00`+JSON cursor frame after replay | MessagePort messages `spawn/resize/kill/read-scrollback` and `data/exit`; replay buffer plus optional on-disk scrollback log |
| Reconnect | 2 MB ring buffer per PTY with absolute cursors; client saves serialized screen + cursor, reattaches and streams only new output; exited sessions queryable (limit 25) | PTY host outlives the renderer, so closing the pane keeps the shell; exact "replaying buffered output" string unverified |
| Scope | Terminal runs wherever the server runs, including remote | Docs: "available in local sessions only", opens in the session's working directory |
| Shell/cwd | `SHELL`, login `-l`, `TERM=xterm-256color`, cwd = instance directory | zsh shell integration injects OSC 133 prompt marks |

Library state, npm registry 2026-09-10:

- `@xterm/xterm` 6.0.0 removed the canvas renderer; DOM or WebGL only. Probed locally: core
  creates 4 `<style>` elements (viewport, DOM renderer theme and dimensions, VS Code scrollable
  helper) and the WebGL addon 1 more, with no nonce hook. Under Whip's `style-src 'self'` these are
  blocked and the fixtures fail.
- `ghostty-web` 0.4.0 (MIT, Coder, built for Mux): probed locally, **no** `<style>` creation and
  no `style` attribute writes. Ships `ghostty-vt.wasm` (423 KB) and a canvas renderer. API is
  xterm-shaped: `Terminal`, `FitAddon`, `onData/onResize/onTitleChange/onBell`,
  `write(string|Uint8Array)`, `resize`, `scrollback`, `theme`, `attachCustomKeyEventHandler`.
  Needs `'wasm-unsafe-eval'` in `script-src`. Its loader tries Bun, then a dynamic import of a
  Vite shim file literally named `__vite-browser-external-…js`, then `fetch(path)`; pass the WASM
  URL through `Ghostty.load(path)` with a Vite `?url` import, and exempt that one shim from the
  renderer boundary guard.
- `node-pty` 1.1.0 remains the Electron default and is N-API, but requires `asar.unpack` for
  `pty.node` and `spawn-helper`, hardened-runtime signing of both, and an exec bit fix.

## Recommendation

**Run the PTY in the Go daemon and draw it with ghostty-web in the shared renderer.** Add a
`terminal` tab kind beside `chat`, `repl` and `new`.

Why the daemon and not Electron main:

1. It already links `creack/pty` with `CGO_ENABLED=0`, so there is no native module to unpack,
   sign, or rebuild per Electron version. That is the whole packaging cost of the Claude Code path.
2. The shell runs where the agent's shell runs. On an SSH or URL host the terminal is remote, the
   same as the `bash` tool. Claude Code's "local sessions only" limitation exists because its PTY
   lives in Electron; OpenCode avoided it by hosting the PTY in the server.
3. The browser client gets the feature for free. Electron main and the desktop bridge stay
   connection-only, and the renderer does not branch on platform.
4. The daemon owning the shell matches "the daemon owns work": a window reload or a dropped
   WebSocket reattaches and replays, exactly like OpenCode's ring buffer.

Why ghostty-web and not xterm.js: the only CSP change is `script-src 'self' 'wasm-unsafe-eval'`.
xterm.js needs `style-src 'unsafe-inline'` or a fork that replaces five style injections. xterm is
the fallback if ghostty-web fails acceptance on fonts, IME, or selection.

What is preserved: every existing tab behavior (order, split, move, close/reopen, 32 views, 4
panes, mobile picker), the CSP posture except one directive, the renderer import guard except one
named shim, the desktop bridge contract at version 2, and the existing `terminal.input` agent path.

### Protocol sketch (additive, next minor)

Operations, all `Ephemeral` except the query, keyed on the connection like executor leases:

| Operation | Params | Result |
| --- | --- | --- |
| `terminal.open` | `cwd`, `cols`, `rows`, optional `root_id` for the session's cwd and title | `id`, `pid`, `shell` |
| `terminal.attach` | `id`, `cursor` (-1 = tail) | `cursor`, `exited`, replay follows as output notifications |
| `terminal.write` | `id`, `bytes` (base64, ≤16 KiB) | accepted |
| `terminal.resize` | `id`, `cols`, `rows` | accepted |
| `terminal.close` | `id` | accepted |
| `terminals.list` (query) | | running and recently exited terminals with cwd and title |

Notifications to the attached connection: `terminal.output {id, cursor, bytes}` chunked at 32 KiB,
`terminal.exited {id, code}`. Daemon side: a small `internal/terminal` package that reuses
`bashrun.userShell()` and its process-group kill, with `TERM=xterm-256color`, login shell, a
1 MiB ring buffer per terminal, and a daemon-wide cap (proposed 8 live terminals). SDK: one
`client.terminals` service and the new notification methods in `client.ts`; regenerate
`@whip/protocol` with `npm run generate`.

Security note: connected clients are already fully trusted (protocol 3.0 removed enrollment; any
client can approve permissions, restart the daemon, and edit provider configuration). A terminal
adds no new trust class, but it is the first operation that hands a network client a shell
directly, without an agent turn. A `terminals.enabled` configuration switch that defaults to on
for Unix-socket clients and is explicit for network listeners is cheap and worth having.

### Tab-system integration

Mirror the New Chat task list. Touch points:

- `session-tabs.ts`: `TerminalTab { id, kind: 'terminal', runtimeId, terminalId, cwd,
  titleHint }`; `parseTab`, `freezeNode`, `openTerminal`, `updateTerminal` (title changes from
  OSC 0/2), capacity within the existing 32-view budget. No new storage key.
- Route `routes/h.$runtimeId.t.$terminalId.tsx`; `tabDestination`, `bindSessionTabs`, and
  `settingsBackDestination` learn the third destination shape.
- `session-tab-strip.tsx`: `title/project/hostName/status/actions` narrowing, a `TerminalView`
  branch in `panels`, "New terminal" in the pane menu, tab context menu ("Open terminal here",
  cwd from the session), and the command palette. `useWorkspaceViews` receives only
  `chat | repl` tabs.
- `packages/app/src/terminal-view.tsx`: mounts ghostty-web once per view, `FitAddon` driven by
  a `ResizeObserver`, theme from the applied theme's ANSI palette, `onData` → `terminal.write`,
  output → `write(bytes)`, `onResize` → debounced `terminal.resize`. Selection copy goes through
  `platform.copy`. On mount: `terminal.attach` with the saved cursor; on "not found" show
  "This shell has ended" with a New terminal action, never auto-recreate.
- CSP: add `'wasm-unsafe-eval'` in `internal/webassets/csp.txt`; the Playwright fixtures and
  `apps/desktop/tests/assets.test.ts` pick it up. Vite: `?url` import for the WASM; a one-file
  exemption in the renderer boundary plugin.
- Mobile: excluded; it only consumes `@whip/app/presentation`.
- Docs: `docs/frontend.md` tab kinds, retention table, and CSP sentence; `docs/protocol-v2.md`
  minor bump; `docs/features.md` row; roadmap line.

### Alternatives considered

- **node-pty in Electron main (Claude Code).** Local-only, native module packaging under the
  `OnlyLoadAppFromAsar` fuse, a bridge v3, and a renderer that behaves differently on web. Only
  right if Whip wants a local-machine shell regardless of which host the session is on.
- **Reuse `bashrun` interactive runs as-is.** Wrong lifetime: one command, 15 s inactivity kill,
  one terminal per session.
- **xterm.js.** Fallback only, for the CSP reason above.
- **Renderer-hosted shell.** Impossible: the renderer is sandboxed with no Node.

### Risks

- ghostty-web is 0.4.0. OpenCode runs a fork in production, which is the strongest signal, but
  expect gaps (no serialize addon upstream; OpenCode ported one). Acceptance should cover IME,
  bracketed paste, wide glyphs, and the Claude Code theme.
- Base64 in JSON adds a third to output bytes; fine at terminal bandwidth, and the 1 MiB frame cap
  is far above 32 KiB chunks.
- Per-frame acknowledgement on the desktop Unix transport (`acknowledgeTransport`) adds one IPC
  round trip per chunk; busy output such as `yes` should be tested, and the daemon should coalesce.
- An older build opening a v3 layout with terminal tabs drops them silently. Acceptable, document it.

## Decisions recorded 2026-09-11

- Lifetime: close the PTY when its tab closes; keep it alive across reload and reconnect.
- Working directory: the focused session's `cwd`; a host-level New terminal falls back to the
  daemon home.
- Shortcut: Ctrl+` as a configurable hotkey preference.
- Agent visibility: human-only surface for the first release.
- Renderer: ghostty-web with `'wasm-unsafe-eval'`.
- Hosts: all three (This Mac, SSH, URL), with `WHIPCODE_NETWORK_TERMINALS=1` required for
  network listeners.

The implementation plan is [../plans/terminal-tabs/README.md](../plans/terminal-tabs/README.md).

## Open questions

1. **Lifetime on tab close.** Close the PTY when the tab closes (OpenCode), or keep it alive until
   the daemon stops or an explicit Kill (Claude Code)? Recommendation: close on tab close, keep on
   reload/disconnect, list exited terminals for 24 h or 25 entries.
2. **Which hosts.** All three, or Local only for the first release? The daemon path makes all
   three the same cost; the difference is the network-listener security switch above.
3. **Working directory and title.** Default cwd from the focused session's `cwd`, with a
   host-level "New terminal" that uses the daemon home when no session is focused?
4. **Shortcut.** Ctrl+` like Claude Code, as a configurable hotkey preference beside the existing
   composer and command shortcuts?
5. **Agent visibility.** Should a terminal opened from a session be visible to the agent in any
   way (transcript note, `!`-style echo as in the shell-escape plan), or stay strictly a human
   surface? Recommendation: human only for the first release.
6. **Renderer decision.** Accept ghostty-web plus `'wasm-unsafe-eval'`, or would you rather relax
   `style-src` and take xterm.js for its maturity?

## Sources

- Repo: `docs/frontend.md`, `docs/desktop.md`, `docs/architecture.md`, `docs/roadmap.md`,
  `.ai-docs/plans/split-views/README.md`, `.ai-docs/plans/new-chat-tabs/README.md`,
  `internal/tools/bashrun/bashrun.go`, `internal/daemon/interactive.go`,
  `internal/daemon/executor.go`, `internal/protocol/registry.go`, `internal/webassets/csp.txt`,
  `packages/app/src/session-tabs.ts`, `packages/app/src/session-tab-strip.tsx`,
  `packages/app/src/desktop-bridge.ts`, `apps/desktop/src/{main,transport,assets}.ts`,
  `apps/web/vite.config.ts`.
- OpenCode: https://github.com/anomalyco/opencode `packages/core/src/pty.ts`,
  `packages/core/src/pty/protocol.ts`, `packages/app/src/components/terminal.tsx`,
  `packages/app/src/context/terminal.tsx`, PR https://github.com/anomalyco/opencode/pull/25822.
- Claude Code desktop docs: https://code.claude.com/docs/en/desktop and
  https://code.claude.com/docs/en/desktop-quickstart; bundle inspection of the installed app.
- ghostty-web: https://github.com/coder/ghostty-web (npm 0.4.0, probed in a scratch install).
- xterm.js 6.0.0 release notes: https://github.com/xtermjs/xterm.js/releases/tag/6.0.0.
- node-pty: https://github.com/microsoft/node-pty (1.1.0), https://github.com/microsoft/node-pty/issues/864.
