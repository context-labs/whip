# Native terminal-tab acceptance

Run after `npm run pack:web`:

```sh
node apps/web/scripts/terminal-tabs.mjs
```

The default covers Chromium and Firefox. `WHIP_WEB_BROWSERS` selects a diagnostic
browser; `WHIP_TERMINAL_RESULTS` selects the report and screenshot directory.
The disposable native fixture opts into the real `-web-terminals` host flag.
The ordinary fixture remains network-terminal restricted; an actual process test
checks both states. No production authorization switch or installed runtime is
changed. Its private HOME, shell environment, working directory and local
provider exclude real accounts and user shell startup files.

| Retained guarantee | Native assertion |
| --- | --- |
| Pane menu opens a host shell in the selected session directory | One `terminal.open`, canonical host-resolved absolute cwd, exact returned terminal ID and process epoch, first bounded cursor read from zero. A foreign epoch cannot read it. |
| Ordered keyboard input and shell output | Typed command/Enter arrive in byte order; actual arithmetic answer returns; decimal output cursors join exactly with byte counts using BigInt. No model work is invoked. |
| Production terminal rendering | Strict CSP; glyph, terminal and corner screenshots; input stays focused and the browser caret stays transparent. |
| Native terminal mouse behavior | Real `cat -v` under mouse tracking receives down/up SGR reports, never arrow keys; Shift+drag selects locally without a write. |
| Reload recovery | Saved tab reads the same existing shell from cursor zero, retains output, and sends no additional open or replayed keystrokes. |
| Second observer | Independent explicit bounded read sees existing output without displacing the page; the original page can still type and read. The host still owns exactly one shell. |
| Close | Context-menu Close sends exact native close, host reads return NOT_FOUND, and the terminal view unmounts. |

The retired `terminal.attach` selected the terminal's live push-output sink. Its
write/resize/close dispatch resolved the handle directly without checking that
sink. “Take over” and “Reattach here” therefore described output routing, not
exclusive write authority. Native cursor reads deliberately support independent
bounded observers; the probe preserves same-shell recovery without recreating
an exclusive writer or retired notification protocol. Common network-terminal
permission still gates every terminal method.

Native terminal IDs expire with their process epoch; they are not durable
session, operation or receipt IDs. The selected session supplies a cwd hint,
not model ownership. Native open/write are never automatically retried. The
probe does not claim crash-restored PTYs, signed Electron acceptance or a
sandbox for shell commands.

Setup is inside the owning cleanup boundary; browser close failure cannot skip
fixture shutdown. Wire evidence uses connection-local request IDs across page
reloads, at most 2,048 terminal frames and 2 MiB retained encoded payloads, with
explicit refusal on overflow. Error/CSP evidence is capped at 64 entries and
failure body text at 16 KiB. All retained text is synthetic fixture shell input
and output.

## Native validation checkpoint (2026-09-29)

`WHIP_TERMINAL_RESULTS=/tmp/whip-native-terminal-second node apps/web/scripts/terminal-tabs.mjs`
passed all six workflow groups in Chromium 153.0.8010.12 and Firefox 155.0.
Both page-error/CSP collections are empty; each shell was explicitly closed and
both browser/runtime owners joined. Native evidence retained 42/63 terminal
frames and 6,228/8,709 encoded payload bytes respectively. The glyph screenshot
was visually inspected. Shell-generated octal UTF-8 sequences preserve the exact
glyph assertion without relying on Playwright's unsupported Unicode key insertion.

The fixture prerequisite's four actual native process tests pass, including the
default network denial and explicit opt-in independent-reader/close proof.
The production renderer digest is
`fd6267094c1b50a95efe39f382f7c613ef19e311dd4fa64bb8916ad12685f731`.
Runner syntax and diff checks pass. These leaves change only fixture/test code;
there is no production terminal behavior change or signing claim.
