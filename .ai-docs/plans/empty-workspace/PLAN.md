# Empty workspace: a front door, not a dead end

Status: decisions 1–4 taken as recommended and built 2026-09-13 (working tree, uncommitted). Suite and build green; live pass on the scratch harness recorded under Verification results.
Related: [loading-states](../loading-states/PLAN.md) (landed the same day; shares the "reserve the footprint" rule and the New Chat first-paint work this plan builds on).

## Why this matters

The empty workspace is what a person sees in three situations, and today it treats all three as the same dead end:

| Situation | How they got here | What they need |
|---|---|---|
| Closed the last tab | Cmd-W, the tab's ×, or deleting the session | to keep working, usually in the same project |
| Sidebar hidden and nothing open | collapsed the sidebar earlier | a way to navigate at all |
| First run | fresh install or cleared storage | to learn the model: a host, a folder, a task |

What it shows now (`packages/app/src/empty-workspace.tsx`, 17 lines): an unstyled `<h1>` in the browser's default bold (heavier than any heading in the app; the New Chat heading is 24 px / 550, the conversation's empty state is 28 px / 500), a verdict as copy ("Your workspace is empty"), a secondary-styled "New Chat" button, and no trace of the keyboard even though the app binds three shortcuts and has a command palette. The only "continue" affordance is the toast's Reopen, which times out after five seconds. The vocabulary also drifts across the three sibling empty states: "Your workspace is empty", "What would you like to work on?" (conversation), "What do you want to work on?" (New Chat).

The highest-leverage change is not visual: after someone closes their last session, do not show this page at all. Browsers and editors keep a place to type. The page then exists for first run and for stale URLs, and can be designed as exactly that: the front door.

## Decisions (taken 2026-09-13)

1. **Closing the last tab opens a New Chat, unless the closed tab was itself a New Chat.** The New Chat takes the closed tab's host and folder (the sidebar catalog already knows the folder), so "close and start again in this project" is zero clicks. The Reopen toast stays. Desktop Cmd-W becomes: session → New Chat → empty → hide window, one press more than today, each step legible. Deleting the last open session lands the same way. Rejected alternative: hide the window directly from a pristine New Chat on desktop; the web has no window to hide and the two platforms should behave alike.
2. **Startup with nothing saved keeps the empty state; no draft is auto-created.** Hosts are still connecting at startup, so a draft made then has no host to show. After decision 1 this state only occurs on first run or after storage loss, which is exactly when a front door matters.
3. **Signature: type to start.** *Reverted the same day on Sam's review: the caret read as a stray cursor and the behavior was confusing. The page now has no signature gesture; the ledger and the reused heading carry it.* Original decision: On the empty workspace, pressing a printable key opens a New Chat with that character already in the composer. The heading ends with a blinking block caret that says so without a sentence. One deliberate risk, justified because the page's single job is getting into a session, typing is the shortest path there, and a prompt caret is the vernacular of the product's own terminal. Guards keep it safe: no modifier held, not composing, focus not inside an input or an open dialog. Static caret under reduced motion. Recommended.
4. **First-run line.** When every connected host reports an empty session catalog, one 13 px line under the heading explains the model. Cheap, and it is the only onboarding copy the product has. Recommended.

## Design plan

**Constraint stated as a choice.** Whip is themed by the user (light, dark, contrast, imported palettes), so this page introduces no color of its own. It spends nothing on accent: `colors.primary` stays reserved for the running dot elsewhere. Distinctiveness comes from type, alignment, and the caret, not from a hue.

**Palette (roles, all existing tokens).**

| Role | Token |
|---|---|
| Ground | `colors.background` |
| Heading, primary row label | `colors.foreground` |
| Row labels at rest, keycaps, first-run line, caret | `surface.secondaryText` |
| Keycap hairline | `surface.quietBorder` |
| Row hover | `colors.hover` |

**Type.** Heading in `typography.sans` at `size24`, weight 550, line-height 32 px, letter-spacing −0.025em: byte-identical to the New Chat heading, because this page is the New Chat's older sibling and the two must read as one system. Rows at `size13` (the chrome size). Keycaps use the existing `Kbd` (mono `size10`, hairline border). The caret is the one mono glyph in the heading line: `▍` (U+258D) in `typography.mono`, `surface.secondaryText`, sized to the heading's x-height.

**Layout.** A column `min(100%, 380px)`, centered in the pane, contents start-aligned. Not a centered hero: a ledger of actions, the same shape as the sidebar's destinations, so the eye reads it as navigation rather than as a landing page.

```
┌ pane ─────────────────────────────────────────────────────────────┐
│                                                                    │
│                                                                    │
│              What do you want to work on?▍                         │  24/550, caret blinks 1 Hz
│              Choose a project folder on a host, then describe the  │  first run only, 13, secondary
│              task.                                                 │
│                                                                    │
│              +  New session                                        │  foreground, 550; no key bound
│                 Search sessions                                    │  secondary
│                 New terminal                          ⌃ `          │  Kbd from preferences.terminalShortcut
│                 Commands                              ⌘ K          │  Kbd from preferences.commandShortcut
│                 Reopen closed tab                                  │  only while closed history exists
│                                                                    │
│                                                                    │
└────────────────────────────────────────────────────────────────────┘
```

Rows are full-width buttons on `layout.subtleButton` (flex, gap 8, padding 8, radius 6, hover `colors.hover`, 32 px tall, 44 on touch), label left, keycap pushed right with `marginInlineStart: auto`. Only actions that actually have a binding show a keycap; the caps are formatted with `formatForDisplay` from `@tanstack/react-hotkeys` (already a dependency; ⌘⇧S on macOS, Ctrl+Shift+S elsewhere) and read the live preference, so they follow Settings.

**Signature.** The caret and what it promises: type, and you are in a session with your first character already typed.

**Copy** (vocabulary is signposting, so every label matches its existing home):

| Where | Copy | Matches |
|---|---|---|
| Heading | What do you want to work on? | New Chat heading; also replaces "What would you like to work on?" in the conversation empty state |
| First-run line | Choose a project folder on a host, then describe the task. | the two choices New Chat needs, in the order the toolbar shows them |
| Rows | New session · Search sessions · New terminal · Commands · Reopen closed tab | sidebar, palette, Settings, tab picker |
| Palette | "Browse sessions" → "Search sessions" | the sidebar's label for the same dialog |
| Missing draft | This New Chat isn't open here. / Its tab may belong to another window, or the draft is no longer saved on this device. | existing meaning, sentence case, no "not available" |
| Missing terminal | This terminal isn't open here. / Its tab may belong to another window, or its shell has ended. | existing meaning |

Removed: "Your workspace is empty", "Open a saved session or start a New Chat.", the lone "New Chat" button.

**Review against the default.** The generic answer to this brief is a centered heading, a sentence, a primary button, and maybe a faint logo. Changed: start-aligned ledger instead of a centered stack (structure that is navigation, not decoration); heading reused from New Chat instead of a new phrase (system consistency over novelty); no wordmark (the sidebar and the splash already carry it; a third would be an accessory); no illustration; the only motion is a 1 Hz caret; the one risk is behavioral (typing works), not visual. What stays boring on purpose: colors, spacing, row shape.

## Items

### 1. Closing the last tab lands in a New Chat

**Why.** Every path that empties the workspace today navigates to `/` (`session-tab-strip.tsx:133–135` for the tab ×, Cmd-W and "Close tabs" menus; `session-actions.tsx:133–137` after deleting the current session). Landing on a page whose only content is "start over" is one click worse than starting over.

**Target.** After the last tab closes, if that tab was a session or terminal, the workspace shows a New Chat pre-set to the tab's host and folder. If it was a New Chat, the workspace becomes empty as today. The toast and Reopen history are unchanged.

**Steps.**
1. `session-tab-routing.ts`: add `openAfterLastClose(runtime, navigate, closed?: SessionTab, cwd?: string)`: when `closed` exists and `closed.kind !== 'new'`, call `openNewChat(runtime, navigate, { runtimeId: closed.runtimeId, cwd }, true)`; otherwise `navigate({ to: '/', replace: true })`.
2. `session-tab-strip.tsx` `close()`: read the active tab before `closeViews`; when `next === null`, call the helper with that tab and `knownCwd(tab)` (terminal tabs pass `tab.cwd`).
3. `session-actions.tsx` delete path: when no next tab, call the helper with a synthetic `{ kind: 'chat', runtimeId }`; the folder is gone with the session, so none is passed.
4. Reopen from the toast leaves the fresh New Chat open (browser behavior). No cleanup logic; a pristine draft is harmless and reused by the next New Chat.

**Tests.** `desktop-close-tab.test.tsx`: rewrite "Cmd-W returns to New session after the final tab, then hides the window" as three presses: session → New Chat on host `mac` (navigate to `/new/$draftId`), New Chat → `/`, `/` → `hideWindow` once; "Cmd-W closes a draft before hiding the window" stays green as written. `session-tabs.test.ts` unchanged (`closeViews` semantics untouched). Add: closing the last session tab pre-fills `cwd` from the sidebar catalog when it holds the session. `session-actions.test.tsx`: deleting the current and only session opens a New Chat on that host.

### 2. The front door (`empty-workspace.tsx`)

**Why.** See Design plan. The component is 17 lines; this is a rewrite, not a patch.

**Steps.**
1. Heading + optional first-run line + optional missing explanation + rows. `WelcomeRecovery` was kept at first, then removed on Sam's review (the block looked out of place here); later the same day the whole first-message recovery layer was removed (see the loading-states plan's follow-up), so nothing renders it anywhere.
2. Actions already exist in the shell's palette handler (`shell.tsx:147–160`). Name that switch `runCommand(action)` and provide it through a five-line `ShellCommandsContext` from `AppShell`; `EmptyWorkspace` renders rows from a static list of `{ action, label, shortcut? }` and calls `runCommand`. Reopen uses `runtime.tabs.reopenView()` + `tabDestination` like the strip. This also lets the palette's "Browse sessions" label change to "Search sessions" in one place.
3. Keycaps: `<Kbd>{formatForDisplay(preferences.commandShortcut)}</Kbd>` for Commands, `terminalShortcut` for New terminal; nothing for rows without a binding.
4. Caret: a `stylex.keyframes` step blink at 1 s on an `aria-hidden` mono span; `[scale.reducedMotion]: 'none'` on the animation name. Rendered only for the true empty state, not the missing variants.
5. Type to start (decision 3): a `keydown` listener on `document` while the true empty state is mounted. Accept only `event.key.length === 1`, non-whitespace, no `ctrlKey`/`metaKey`/`altKey`, not `isComposing`, `event.target` is `body` or inside the component, and no `[role="dialog"], [role="alertdialog"]` in the document (the same guard the shell uses for Cmd-W). Then `preventDefault()`, `const tab = openNewChat(runtime, navigate)`, and if a tab came back `runtime.setDraft(welcomeDraftKey(tab.id), event.key)`. The New Chat textarea autofocuses and places its caret at the end of the value.
6. First-run line (decision 4): shown when every host with a `list` has answered with an empty page. Subscribed through `useSyncExternalStore` over the hosts' list views; status is ignored because polling flips it to `stale` and back.
7. Missing variants keep the same shell and rows (minus caret and Reopen) with the copy from the table.
8. Conversation empty state (`conversation.tsx:319`): copy to "What do you want to work on?"; its 28 px size is out of scope here (note as optional follow-up: one phrase, one size).

**Tests.** New `empty-workspace.test.tsx`: rows render and call the shell actions; keycaps equal `formatForDisplay` of the current preferences (asserting through the same function avoids platform coupling in jsdom); typing `h` opens a New Chat whose draft is `h` and navigates to its route; typing with ⌘ held, or while a dialog is open, does nothing; Reopen appears only with closed history; the missing variant shows its explanation and no caret; reduced motion produces no animation name. Keep green: `desktop-close-tab.test.tsx` ("palette New session allocates fresh tabs from `/`"), `welcome-recovery.test.tsx`, `settings-navigation.test.ts` (back destination `/` when no tab).

### 3. Docs

`docs/frontend.md`, Conversation and navigation patterns: one paragraph stating the rule ("the workspace always offers a place to type: closing the last session opens a New Chat in its folder; the empty state is the first-run front door and the stale-URL fallback") and the vocabulary row (New session · Search sessions · New terminal · Commands · Reopen closed tab). `docs/features.md` if it describes the empty workspace.

## Preserved / changed / not built

**Preserved.** Reopen toast and the 20-entry closed history; Cmd-W hiding the desktop window from an empty workspace; drafts kept on close; the 32-tab capacity rules and their notices; the missing-URL variants' meaning; the sidebar, palette and Settings as they are apart from one label.

**Changed.** Where the workspace lands after its last tab closes (two call sites through one helper); `EmptyWorkspace` rewritten; the palette label "Browse sessions"; the conversation empty-state phrase; a `ShellCommandsContext` exposing the existing palette handler.

**Not built.** A resume list or project launcher (redundant with the sidebar); a wordmark watermark; auto-creating a draft at startup; removing the auto-opened New Chat when Reopen is clicked; a tab min-width; any new color or type token.

## Verification

- `npm run test:web`, `npm run check:web`.
- Live on the scratch harness from the loading-states plan (working tree on port 3100 proxied to the onboarding container): close the last session tab → New Chat in that session's folder; close it → front door; press `h` → New Chat with `h` in the composer and focus there; keycaps match the Settings page; toggle reduced motion → static caret; light theme and increased contrast; 375 px width (rows 44 px under the compact header); sidebar hidden.
- Screenshot pass at 1440 and 375 in dark and light, attached under `evidence/`.

## Verification results (2026-09-13)

- `npm run test:web`: 62 files, 651 tests passing (4 new in `empty-workspace.test.tsx`, 1 new in `session-actions.test.tsx`, the Cmd-W test rewritten as three presses with the folder carry-over asserted). `npm run check:web` green.
- Review: Sam asked to remove type-to-start (confusing, and the caret looked like a stray cursor) and to enlarge the keycaps. Both done: the listener, caret and their tests are gone; `Kbd` accepts `xstyle` and the page sets it to 12 px.
- Live, working tree on the scratch harness (port 3100 → onboarding container). The container had been recreated during the day, so the host was fresh with no sessions and no provider: a real first-run environment, but no way to create a session live.
  - Closing the three restored tabs one by one: the last session tab opened a New Chat; closing that New Chat landed on the front door with the Reopen row present and focus on the strip's plus button.
  - Front door: heading with caret, first-run line, rows with `⌃ \`` and `⌘ K` keycaps from the live preferences. Pressing `h` with focus on the plus button opened a New Chat on Local; the host had no provider, so provider setup took over (correct) and the draft is held for when the composer appears.
  - 375 px: rows at 44 px, heading wraps to two lines cleanly. Light scheme: correct on tokens alone.
- Two defects found and fixed during the pass, both now covered by tests: a closed tab whose host no longer exists inherited that host (New Chat opened on "Choose a host"); it now falls back to the default host. The type-to-start guard accepted only `body` or the page as the key target, but after a close the focus rests on the strip's plus button; it now accepts any target that is not a text field, with dialogs still excluded.
- One design defect found in the light pass: the first-run line disappeared because the catalog read was unsubscribed and keyed on `status === 'live'`, which every poll flips to `stale`. It now subscribes to the host lists and keys on "answered with zero sessions".
- Observed, not changed: the tab-closed toast wraps into a narrow column at 375 px (`session-tab-strip.tsx` `toast` style, pre-existing).
- Not verified live: a real session's folder carrying into the New Chat (no provider on the fresh container; covered by the Cmd-W unit test), the caret landing at the end of the seeded draft (needs the composer), reduced motion (CSS only).
