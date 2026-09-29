# Native terminal retirement

The terminal uses `RunNative` and protocol-v4 services. The old terminal model,
legacy client, root-only command dispatcher, transcript store and raw TTY reader
were removed after their supported behaviors had native implementations. This
checkpoint is based on draft #274 (`9bc05d632`). At that checkpoint, separate
external Chrome modes and backend/client migration remained open. Subsequent
[external Chrome controls](native-external-browser-controls.md) and
[core retirement](backend-native-core-retirement.md) close those implementation
items; [final acceptance](backend-native-gate-audit.md) remains open.

## Retained code and tests

The shared `links.go`, `markdown.go`, `matching.go`, `pane_geometry.go`,
`presentation.go`, `selection_geometry.go`, `theme_active.go`, `transcript.go`,
`theme/` and `ui/` remain. `report.go` retains the issue URL and version helper.
Styles and selection fields used only by the old model are removed. All native
tests and the pure theme/UI packages remain.

Go type-reference analysis of every native production and test declaration at
`c76b9e1ee` found no native test dependency on old test-file helpers. Mixed test
files were split by function: 32 pure tests remain, including hyperlink/ANSI
width, Markdown tables, theme contrast, issue URL, padding and style ratchets.
The [exact pure-test disposition](native-terminal-test-disposition.json) also
names the 28 tests of obsolete pure implementations replaced by the families
below. Another 305 old-model-dependent tests are removed with their fixtures.
Those counts are not claims of equivalent line coverage: retained product
behaviors are mapped explicitly below. The style ratchet stays at zero; the literal-padding ratchet tightens from 33 to four.

TestMain retains an isolated client home and deterministic initial dark scheme.
It cleans the home after `m.Run`, before `os.Exit`; it no longer mocks the old
mosh/tmux globals. Real native terminal/environment tests own their helper
processes and injected filesystem trees.

## Behavior disposition

All paths below are in `internal/tui`. The command parser and local dispatch are
`native_commands.go` and `native_command_affordances.go`; the corresponding
`*_test.go` files exercise native host/client behavior.

| Retained behavior | Native implementation and replacement coverage |
| --- | --- |
| `/auth`, `/connect`, `/setup`, masked provider/account setup | `native_setup*`, `native_command_affordances*`; exact presets, explicit publication and preserved draft |
| `/model`, `/model-for-session`, effort and refresh | `native_model_menu.go`, `native_menu_test.go`; exact route/configuration revisions, no selection during refresh |
| Root/child navigation, agents, resume/search/archive, dock and panels | `native_navigation*`, `native_agents*`, `native_layout*`, `native_panels*`; passive bounded reads, explicit selected owner |
| Rename, fork, rewind, clear, history actions | `native_history_controls*`, `native_history_dialog*`, `native_commands_test.go`; stopped-owner boundary and captured revisions, no implied file rollback |
| Export/report | `native_export.go`, `native_utilities_test.go`, `report_cmd_test.go`; bounded verified export and redacted facts |
| REPL, live reasoning/stdout, expansion/diffs and recorded tool output | `native_repl*`, `native_render*`, `native_transcript*`; canonical cell/operation joins, explicit missing evidence, bounded live observations |
| Command/file/skill completion, palette and leader shortcuts | `native_completion*`, `native_palette*`, `native_ui_test.go`; scoped completion and native command parsing |
| Paste, images/design parts, draft recall and global human text recall | `native_paste*`, `native_recall*`; exact local payloads, bounded text-only cross-owner reads, no implicit submission |
| Unicode selection and links | `native_selection*`, `native_links*`, surviving `links_test.go`; exact known-local cwd confinement and safe HTTP links |
| Escape, double Ctrl+C, Ctrl+K and detach | `native_recall_test.go`, `native_commands_test.go`; captured active turn, preserved draft, accepted work survives detach |
| Human shell focus and `!cmd` | `native_shell*`, `native_utilities_test.go`; bounded epoch/operation/sequence input and model-free durable calls, no uncertain replay |
| Permission, standing-grant and question controls | `native_permissions*`, `native_questions*`; exact owner and decision identity |
| `/context-doctor`, `/lsp`, `/me`, `/memory` | `native_context_audit*`, `native_integrations_test.go`, `native_standing*`, `native_memory*`; applied instruction audit, passive LSP, exact standing-source CAS and fresh local notes |
| `/mcp`, Browser driver and computer-use controls | `native_mcp*`, `native_integrations*`; native host configuration and explicit resource control. Subsequent `native_external_browser*` adds [external Chrome configuration and exact root-generation controls](native-external-browser-controls.md), with child read-only and lost-acknowledgement coverage. |
| Goals, formulation, schedules and compaction | `native_goals*`, `native_compaction*`; explicit canonical completion/receipts, no old GOAL_MET text inference |
| Theme, mouse and terminal hints | `native_settings_menu.go`, `native_preferences*`, `native_terminal*`, `native_menu_test.go`; framework-owned terminal input, explicit override priority and bounded helper processes |

The old registry's fresh-context-preview claim did not match its implementation;
context audit continues to report applied evidence. Arbitrary file URLs, implicit
workspace rollback, unscoped permission rules and silent old-client config/note
imports are not native contracts. The unused old `stream.plan` projection had no
production writer. These are documented ownership choices, not missing aliases.

## Validation

On the isolated deletion tree, CLI and terminal compile. Complete terminal
race/shuffle partitions passed in 67.394 and 74.444 seconds; the complementary
second partition includes every surviving pure test. Vet passed. The actual
compiled CLI default-route PTY/native-host test passed in 3.698 seconds. Both
supported Go entry points have no production dependency on the retired daemon,
agent, legacy configuration, provider loop, root-only RLM, tools, memory, gateway,
protocol transport or old Inference.net account package. Final shared-helper/theme/menu race checks passed in 5.642 seconds, pinned lint
reported zero issues, and both disposable CLI/runtime binaries built.
