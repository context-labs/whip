# Frontend migration comparison

`frontend-migration-parity.mjs` runs the same product assertions against an
explicit reference or native repository. Each repository must already have its
own built SDK and packaged renderer. The script never builds or changes product
sources in either target, discovers installed daemons, or reads actual user shell
configuration. Both modes use disposable state and synthetic provider responses.
The shell case requires `/bin/zsh`; it supplies its own HOME, ZDOTDIR, prompt,
alias and PATH fixture. The reference child's inherited environment is removed.

```sh
WHIP_PARITY_RESULTS=/tmp/whip-reference-parity node apps/web/scripts/frontend-migration-parity.mjs reference /absolute/reference
WHIP_PARITY_RESULTS=/tmp/whip-native-parity node apps/web/scripts/frontend-migration-parity.mjs native /absolute/native
```

The default runs Chromium and Firefox. `WHIP_WEB_BROWSERS` chooses one of these
for diagnostics; `WHIP_PARITY_CASES=A1,A2,A3,A11` selects groups. Reports retain
bounded RPC method names, terminal output counts, errors, and fixture-only
screenshots; they do not store ordinary provider/transcript frames.

| Group | Shared assertions |
| --- | --- |
| A1 | A configured New Chat remains usable while actual nonessential catalog/MCP reads are held; startup does not visibly flash onboarding. |
| A2 | Canonical conversation appears, draft and caret survive metadata completion, reload preserves the draft, only one conversation tree remains mounted. Native tree/queue reads are held independently; the reference has one atomic snapshot and cannot model that transport split. |
| A3 | A real submission acknowledgement is held; duplicate Enter causes no duplicate submission. Queued input remains out of transcript, queue and draft survive reload, actual execution happens once. |
| A11 | Host prompt, alias, custom environment and profile PATH load; resize reaches the host, reload reuses the shell without replaying input, and keyboard context-menu close works. Native reads additionally use bounded output waits. |

The keyboard terminal assertion restores the desktop viewport after its resize
check: the narrower responsive layout intentionally hides desktop tab controls.
It waits for initial terminal focus effects before deliberately focusing the tab.
The reference catalog query is mapped through its existing `query` operation
envelope; no production compatibility facade is introduced.

This focused comparison supplements, rather than replaces, existing startup,
session-opening, submission/recovery, terminal-view/open, shell-environment, and
terminal lifecycle tests. It does not claim browser coverage of all lost-ACK,
storage-failure, epoch, ring-truncation, clipboard, or remote-host scenarios.
Use the existing dedicated tests and `terminal-tabs.mjs`/`composer-queue.mjs`
for those retained scenarios. Signed desktop and real user acceptance remain
separate integration gates.

The new opt-in `terminalProfile` native fixture flag is independently checked by
`node --test apps/web/scripts/native-terminal-profile-fixture.test.mjs` after
`npm run pack:web`. Default native fixture behavior remains unchanged.
