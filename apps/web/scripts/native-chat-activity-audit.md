# Native activity entrypoint audit

The retained `chat-activity.mjs` command now runs the tested native fixtures.
It has no retired SDK, event injection, legacy snapshot or `/api/v3` dependency.
The current source base is `87bf016f1`; disposable desktop lifecycle prerequisite
`937e8b9e1` waits for initial application readiness and joins owned shutdown.
No installed application, browser profile, account or runtime is modified.

## Retained command modes

| Selection | Native runner and evidence |
| --- | --- |
| Default | `native-chat-activity.mjs`, then `native-history-recovery.mjs` |
| `WHIP_CHAT_COMPOSER_ONLY=1` | `native-content-probes.mjs`, composer mode |
| `WHIP_CHAT_MESSAGES_ONLY=1` | `native-content-probes.mjs`, composer and stored modes |
| `WHIP_CHAT_HISTORY_ONLY=1` | `native-history-recovery.mjs` |
| `WHIP_CHAT_AGENTS_ONLY=1` | `native-agent-dock.mjs` |
| `WHIP_CHAT_NATIVE_DROP=1` | Explicit interactive Finder check; Electron only |

`WHIP_WEB_BROWSERS` selects Chromium, Firefox or Electron. The default remains
Chromium and Firefox. At most one specialized mode may be selected. Each runner
owns and joins its fixture before the next runner starts; the entrypoint reports
the locations of their individual results.

Native Electron activity uses the production local IPC path. History, content
and agent-dock fault probes run the staged main/preload and renderer, open the
ordinary URL connection UI, and connect to a distinct disposable gateway. Their
existing exact WebSocket faults therefore remain real transport tests. They do
not claim to intercept native IPC. The extra local launcher host and the remote
fixture have different verified runtime IDs and independent joined lifetimes.

## Behavioral map

The activity runner retains keyboard disclosure, grouped real file operations,
the shared running/settled REPL, child creation/wait and scoped inline metadata,
reduced motion, narrow layouts and extreme type sizes, selection through
settlement, automatic folding, the 128-operation virtual tree, streaming
Markdown/Unicode/code/lists and all wheel/Latest reading-intent assertions.

The staged Electron run additionally checks the actual 48px drag region,
interactive no-drag tabs, and native 400% zoom with visible activity and no
horizontal overflow. Separate content and history runners retain the original
attachment, delayed/failed read, exact retry, draft, selection and anchor checks.
The existing content audit documents canonical content references replacing
retired JSON body handles.

Native reasoning is an ephemeral active preview: it survives an active reload
while retained by the running host but is absent after settlement or host restart.
The earlier description of the old fixture's durable reasoning as invented was
incorrect: the old production session presentation path persisted it and tested
history/fork/rewind behavior. This is an open parity gap, covered by G1 in the
[frontend UX restoration plan](../../../docs/frontend-ux-restoration-plan.md).
The native fixture passes below establish current native behavior, not reasoning
parity. Real file reads finish before the fixture executor hold, so the UI
reports that actual held tool. `agents.wait_after_cell` releases execution and
reports waiting for work to continue, not an invented active wait operation.
The canonical tree and subsequent prose remain actual accepted native work.

The manual Finder mode creates a separately identified temporary Electron bundle
and one PNG. It requires an interactive terminal, waits at most four minutes,
and succeeds only after a trusted one-file drop, available preview and enabled
send button. The default gate never invokes it. Manual Finder dragging, signed
packaging, notarization and physical display timing remain unverified here.

## Disposable lifecycle evidence

The first fast Electron run attempted route navigation before the initial app
was ready and timed out. Its existing inspector-based cleanup also stalled.
Only the owned fixture Electron was terminated, then native cleanup joined.
The corrected runner waits for the production sidebar before test navigation.
Shutdown now attempts app exit, signals only the owned child after 1.5s/5s, and
joins both process and inspector within the 10s bound. An actual Node child
ignoring SIGTERM proves forced termination and inspector joining; all four
desktop-helper tests passed. This is a fixture repair, not a claimed product
navigation fix.

Renderer under test:
`187ddeaeefc2117ea5befac4e7c664296abc8fd3e2275fcd7c0e9aa8911e08d8`.

- Electron activity: 14 groups passed; maximum 38 mounted rows for 128 operations.
  `/tmp/whip-native-activity-electron-ready/results.json`.
- Electron history: held/failed exact cursor, explicit retry and shared evidence,
  preserved DOM/selection/anchor/draft, canonical tail and no provider invocation
  passed. `/tmp/whip-native-history-electron/results.json`.
- Electron content: composer and stored modes passed, 16-attachment bound, six
  mixed-drop uploads with zero duplicates, five stored images, explicit retry,
  2,342,495-byte actual MCP image, and zero anchor/hover drift.
  `/tmp/whip-native-content-electron/results.json`.
- Electron agent dock, invoked through the original `WHIP_CHAT_AGENTS_ONLY`
  entrypoint: eight actual children, metadata-only roster, selected child
  history, split/tab identity, isolated drafts and attachment, and zero reading
  drift passed. `/tmp/whip-native-entry-agents-electron/results.json`.
- Final default entrypoint: Chromium and Firefox each passed all 12 activity
  groups and four history-recovery groups; maximum operation-tree rows were
  bounded below 80 and page/CSP assertions passed. Both runners' browser and
  fixture cleanup completed before the combined command returned exit 0.
  `/tmp/whip-native-entry-browsers/results.json` and its `activity`/`history`
  result directories retain the exact evidence.

All eight changed runner/helper scripts passed syntax checks. The headless
manual-mode validation refuses before creating a fixture; it is not Finder
acceptance. Desktop staging passed from an owned lockfile installation. The
fresh dependency checkout initially lacked Electron's downloaded binary; its
pinned local install completed before the successful staging build.

The earlier intermittent Firefox agent-dock 28px reading-anchor failure remains
a separate open finding; later passes do not erase it. This entrypoint does not
claim the outstanding 50ms native paint or aggregate desktop memory targets.
