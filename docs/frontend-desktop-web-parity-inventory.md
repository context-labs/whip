# Desktop/web UX parity inventory

Research checkpoint: 2026-09-29. **The source inventory is complete for the scoped entry points; comparative runtime acceptance remains open.** The initial inventory was research-only. Subsequent user-approved provider/onboarding changes and their focused checks are recorded in the [implementation record](frontend-ux-restoration-progress.md#increment-12--provider-onboarding-decisions).

The provider/onboarding decisions below and UX-17 (preserving compatible reasoning effort in Settings) are implemented. Adjacent provider-management decisions and ordinary continuity remain open. The visual foundations largely survived. Remaining differences concern defaults, state transitions, collection behavior, scope and everyday presentation details.

## Reference, scope and confidence

- Native research baseline: `/private/tmp/whip-native-final-acceptance`, branch `codex/frontend-ux-parity-acceptance`, commit `e10d77a3e40d2bd788cab5431096f7353d6119c1`. Subsequent implementation commits are recorded in the progress log.
- Reference: `/private/tmp/whip-ux-reference`, development commit `12f0ea0768b7d769765596c35c049fe80edfaeba` plus the approved 14-file overlay. All 14 hashes were reverified. Overlay SHA-256: `04255978030cbe6289a7eeb2d02b92d1f47c7d52361a0d3ef0f6f6a70140c7c6`. The commit alone does not reconstruct the reference.
- Existing modified research/handoff files were preserved. Current ownership follows the [frontend guide](/private/tmp/whip-native-final-acceptance/docs/frontend.md). See the [reference manifest](/private/tmp/whip-native-final-acceptance/docs/frontend-ux-reference.json) and [handoff](/private/tmp/whip-native-final-acceptance/docs/backend-redesign-handoff-2026-09-29.md).
- In scope: shared desktop/web renderer, routes, menus/commands, all Settings and inspector sections, shared UI, web bootstrap and desktop bridges. Mobile, CLI/TUI/ACP and the docs site are excluded. Shared contract changes may require separate acceptance for those clients later.
- The [source map](/private/tmp/whip-native-final-acceptance/docs/frontend-desktop-web-parity-source-map.md) enumerates production files and test/audit entry points. File coverage is not line-by-line verification or a runtime parity percentage.
- **Source:** concrete mapping, branch, limit or copy differs. **Conditional:** impact requires the stated precondition. **Semantic:** old/new concepts need a product decision. **Hypothesis:** visible effect remains unproven. None means newly reproduced failure.
- UX-01–27 preserve handoff IDs. **UX-26 is mobile and excluded.** UX-28–45 add desktop/web findings: **44 difference/decision entries and 12 retained-behavior gates**, not 44 proven bugs.

**Every decision is pending user review.** Options and acceptance criteria do not authorize implementation. Related sub-decisions still need individual agreement. Sources identify the implementation boundary: app owns presentation/workflows, SDK owns observation/recovery, desktop owns native resources. Backend dependencies are called out. Shared desktop/web is the default client scope; native browser/notification features apply where supported, and UX-44 is desktop local/SSH.

## Entry-point coverage

| Surface | Journeys/actions accounted for | Entries / gates |
| --- | --- | --- |
| Startup / New Chat | `/`, `/new/$draftId`; host, credentials/default, definition/model/effort, directory, draft, submit | 01–04, 06–08, 17, 19, 21, 27; P01/P04 |
| Session workspace | `/h/$runtimeId/s/$rootId`; root/child, chat/REPL/trace, composer, queue, attachments, history, inspector | 08, 10–16, 22–24, 28–29, 33–34, 41; P03–P07 |
| Navigation | Projects, attention, search; rename/fork/archive/restore/undo/delete; directory/editor actions | 06–09, 24, 30–32; P02/P03/P10 |
| Tabs / commands | New session/browser/terminal; focus composer; search sessions/open tabs; next/previous/close/reopen; split/drag/reorder; inspector/settings commands | 05–07, 25, 43; P02/P03/P09 |
| Terminal | `/h/$runtimeId/t/$terminalId`; open here, input/output, resize, selection, reconnect, close/recovery | 05–07, 43–44; P09 |
| Browser | `/browser/$viewId`; toolbar, native surface, preview/share, agent access, design selection/screenshot | 25, 42; P08 |
| General Settings | Shortcuts, attention notifications, browser addresses, Saved commands | 25, 32; P02/P11 |
| Appearance Settings | Theme/font/density/wrapping/contrast/motion/custom import | P01/P11 |
| Providers and models Settings | Connections/login/defaults/effort/accounts/custom routes/import ownership/save/discard | 02–04, 17–20; P10 |
| Agents and execution Settings | Definitions, engine, compaction, budgets/continuations/retries, imports, external Chrome | 18, 20–21, 25, 39; P10 |
| Servers Settings | Local/URL/SSH; diagnostics/install/update/restart/editor integration | 06, 44; P10/P12 |
| About and updates Settings | Version/build, updates/errors/restart | P12 |
| Agent tree inspector | Hierarchy, lifecycle, activity, intervention, navigation | 14, 33–34, 36 |
| Mail and shared state inspector | Recipient/state, paging, excerpts/bodies/values, scope/version | 35–36, 45 |
| Executions inspector | Turn selection, executions/operations/events, output/content/read failures | 12–13, 15–16; P07 |
| Goals and schedules inspector | Goal draft/save/run/resume/cancel; schedule prompt/time/cancel/history | 37–38 |
| Usage and authority inspector | Usage, budgets/resource limits, grants/revoke | 18, 22, 36; P07 |
| Context and model inspector | Applied context, model/runtime, reload, compaction/history/undo | 18, 24, 39–40 |
| Permissions inspector | Policy, authority and pending requests | 22–23; P06 |
| Host integrations inspector | MCP/LSP/Browser/Computer/Tools; inventory/refresh/apply/inspect | 20, 25; P08 |
| UI / platform | Layout/focus/themes, clipboard/downloads/dialogs/links, notifications, windows/update, bootstrap/CSP | 06, 28–29, 32, 44; P01–P03/P08–P12 |

`__root` is the route shell. Unchanged routes or styles do not establish unchanged behavior.

## Comparison contract

Use disposable profiles with identical credentials, defaults, definitions, content, directories, host identity, root/child selection and permission policy. Record fresh/existing state, local/remote host, connectivity, viewport, theme and motion. Prefer synthetic/no-inference fixtures. This document authorizes no paid calls, normal-runtime changes, signed installation, merge or release.

Capture entry → pending → success → navigation back → reconnect → failure/recovery where relevant. Record action count, automatic choice, copy, focus/caret, selection, scroll, visible rows and recipient. Final screenshots alone are insufficient. Shared web results do not establish desktop bridge behavior.

Preserve native invariants: verified identities; exact request/recovery identity; daemon-owned work; truthful accepted/running/uncertain states; bounded collections/content; immutable scoped references; revisions; exact authority/delegation; one SDK state owner. Preserve restoration fixes. Do not copy legacy reducers back to regain a UI detail.

## Difference and decision ledger

### UX-01 — Draft before connecting

**Source; also user-observed.** Native welcome previously added this bypass; the reference does not expose it.
- **Decision/constraint:** Removed by user decision. Provider setup has no draft-before-connecting action or bypass state.
- **Compare/pass:** Fresh host without credentials stays in setup until a provider is selected or connected. Existing drafts remain retained.
- **Sources:** welcome.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/welcome.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/welcome.tsx:261)

### UX-02 — Extra Inference.net model confirmation

**Source; also user-observed; approved correction implemented.** The extra onboarding model picker and confirmation panel are removed.
- **Decision/constraint:** Never select a provider automatically. Explicit provider selection (or connection completion) applies a known preset model and effort and opens the composer. Providers without a preset open the composer with the provider selected and model unset.
- **Compare/pass:** Focused tests and Chromium/Firefox check single provider selection, preset effort, no unsolicited submission, and persistent provider-only drafts. Broader C1 comparisons remain scoped separately.
- **Sources:** config.go: [reference](/private/tmp/whip-ux-reference/internal/config/config.go) host.go: [native](/private/tmp/whip-native-final-acceptance/internal/config/host.go) provider-setup.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/provider-setup.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/provider-setup.tsx)

### UX-03 — Credential discovery versus publication

**Source/semantic.** Old setup discovered/published missing routes. The initial native implementation exposed environment credentials as passive candidates; the approved follow-up imports them at host startup.
- **Decision/constraint:** Environment-discovered providers are automatically configured and available without **Use**. Preserve existing routes (including disabled ones), fallback variable order and model defaults. Provider choice remains explicit. Onboarding applies known model/effort presets on that choice; providers without presets continue to model selection in the composer. Saved account candidates remain explicit.
- **Compare/pass:** Saved/environment credentials, login completion, reconnect/custom routes. No redundant setup action; passive inspection does not publish credentials.
- **Sources:** provider-setup.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/provider-setup.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/provider-setup.tsx) provider-connections.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/settings/provider-connections.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/provider-connections.tsx)

### UX-04 — Contextual model suggestion

**Source/conditional.** Old selection considers current/configured choices; native starts with the first preset suggestion.
- **Decision/constraint:** The user approved known presets for explicit onboarding selection. A custom endpoint does not inherit a canonical provider's preset. Existing configured-host Settings suggestions remain a separate comparison obligation.
- **Compare/pass:** Custom model, multiple routes, already-selected provider. Suggestions respect intended route without silent substitution.
- **Sources:** provider_selection.go: [reference](/private/tmp/whip-ux-reference/internal/daemon/provider_selection.go) provider-setup.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/provider-setup.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/provider-setup.tsx)

### UX-05 — Terminal pending versus uncertain

**Source.** Native navigation precedes open acknowledgement and can show uncertainty/recovery during normal sending. Old flow waited, then showed Starting terminal.
- **Decision/constraint:** Preserve uncertain-open recovery; choose ordinary pending presentation separately.
- **Compare/pass:** Delay successful response, then lose acknowledgement. Normal waiting looks normal; actual uncertainty offers recovery without duplicate shells.
- **Sources:** session-tab-routing.ts: [reference](/private/tmp/whip-ux-reference/packages/app/src/session-tab-routing.ts), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/session-tab-routing.ts) terminal-view.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/terminal-view.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/terminal-view.tsx:208)

### UX-06 — Reconnect removes workspace presentation

**Source; interaction impact partly untested.** Native withdraws usable client/clears queries. New Chat, terminal, Projects/search and folder picker can lose presentation; the picker closes. Retained draft text/chat history does not prove retained focus, selection or canvas.
- **Decision/constraint:** Invalid clients remain unusable. Decide retained stale presentation versus necessary identity-change reset.
- **Compare/pass:** Disconnect/reconnect while typing, selecting terminal output, reading lists or choosing a folder. Keep work/place, mark stale data, gate actions; repeat with changed host identity.
- **Sources:** hosts.ts: [reference](/private/tmp/whip-ux-reference/packages/app/src/hosts.ts), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/hosts.ts) runtime.ts: [reference](/private/tmp/whip-ux-reference/packages/app/src/runtime.ts), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/runtime.ts) directory-picker.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/directory-picker.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/directory-picker.tsx) session-tab-strip.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/session-tab-strip.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/session-tab-strip.tsx)

### UX-07 — Open terminal here can use HOME

**Source/conditional.** Native fallback is `~` before explicit cwd arrives. Reference passed root identity for host directory resolution.
- **Decision/constraint:** Resolve verified session cwd first or provide native host resolution; avoid silent HOME fallback.
- **Compare/pass:** Open immediately from a session outside HOME with metadata delayed. Correct directory or clear wait.
- **Sources:** session-tab-routing.ts: [reference](/private/tmp/whip-ux-reference/packages/app/src/session-tab-routing.ts), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/session-tab-routing.ts) terminal_rpc.go: [reference](/private/tmp/whip-ux-reference/internal/daemon/terminal_rpc.go)

### UX-08 — Composer blocked by partial read failure

**Source/conditional.** Send availability depends on observation plus independent selected/root metadata. Read failure can disable it. Chat history reads use host-connected state; REPL uses stricter state.
- **Decision/constraint:** Preserve identity/authority gates. Identify genuinely required reads and whether cached verified metadata suffices.
- **Compare/pass:** Fail metadata, observation and execution reads separately on a valid host. Distinguish unreadable evidence from unavailable submission; retain draft/caret/scope.
- **Sources:** conversation.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/conversation.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/conversation.tsx) repl-view.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/repl-view.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/repl-view.tsx)

### UX-09 — Projects paging and freshness

**Source.** Native uses 100-row pages, 500-item/2 MiB retained window. Periodic activity refresh stops after expansion/older paging; metadata invalidation still works. Reference continued refresh.
- **Decision/constraint:** Keep bounds; decide freshness, overflow and reading stability.
- **Compare/pass:** More than 100/500 sessions; expand/page, change activity/rename elsewhere. Discoverable work, truthful freshness, stable place.
- **Sources:** session-sidebar.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/session-sidebar.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/session-sidebar.tsx)

### UX-10 — Queue Load more replaces rows

**Source.** Old paging appended; native replaces cursor page and offers Return to first queued messages.
- **Decision/constraint:** Keep bounds/exact identity. Choose bounded append or clearly labeled pages.
- **Compare/pass:** Multiple pages while running; page/cancel/acknowledge/reconnect. Keep orientation, show work once, never imply disappeared rows completed.
- **Sources:** composer-queue.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/composer-queue.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/composer-queue.tsx) conversation.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/conversation.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/conversation.tsx)

### UX-11 — Large message reading

**Source.** Old auto-hydration up to 64 MiB supported normal reading. Native size notice/raw dialog uses 1 MiB read, 128 KiB displayed code, up to 4 MiB JSON download.
- **Decision/constraint:** Keep bounded reading. Choose rendered incremental reader/export or explicitly accept reduced limit. These are not all content limits.
- **Compare/pass:** Markdown beyond thresholds and above 4 MiB. Approved readable/exportable content, truthful truncation.
- **Sources:** timeline.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/timeline.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/timeline.tsx) history-gap.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/history-gap.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/history-gap.tsx) conversation.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/conversation.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/conversation.tsx)

### UX-12 — REPL loses argument/path summaries

**Source.** Old host-call rows included argument/path summaries; native shows capability only.
- **Decision/constraint:** Restore safe summaries from native evidence without another execution store.
- **Compare/pass:** File/command/parameterized calls, live and retained. Identify action without raw details; capability-label-only checks do not pass.
- **Sources:** repl-view.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/repl-view.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/repl-view.tsx:29)

### UX-13 — Worker restart narration

**Source; omission unproven.** Old independent restart rows became cell-associated results. Unmatched restart loss is not established.
- **Decision/constraint:** Confirm evidence contract before choosing independent marker.
- **Compare/pass:** Restart between cells, after failure, without retained matching cell. Correct visible narration, no fabricated output.
- **Sources:** repl-view.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/repl-view.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/repl-view.tsx) execution-output.ts: [native](/private/tmp/whip-native-final-acceptance/packages/app/src/execution-output.ts)

### UX-14 — Child heading uses definition ID

**Source.** Native uses ID instead of human name available through lookup.
- **Decision/constraint:** Keep internal identity; decide readable primary label with IDs as detail.
- **Compare/pass:** Named children with opaque IDs, including completed children. Correct heading/recipient; both versions retain Message this agent composer.
- **Sources:** conversation.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/conversation.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/conversation.tsx) definitions.ts: [reference](/private/tmp/whip-ux-reference/packages/app/src/definitions.ts), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/definitions.ts)

### UX-15 — Last turn coupled to history page

**Source/conditional.** Native uses active turn or first item in selected execution window. Old last_turn was independent; older paging can alter notice.
- **Decision/constraint:** Stable latest summary without duplicating SDK execution reducer.
- **Compare/pass:** Finish turn, page older evidence, return to chat. Last turn means actual latest turn, separately from selected history.
- **Sources:** conversation.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/conversation.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/conversation.tsx) agent-turn-notice.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/agent-turn-notice.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/agent-turn-notice.tsx) execution-state.ts: [native](/private/tmp/whip-native-final-acceptance/packages/sdk/src/execution-state.ts)

### UX-16 — Independent refresh cadence

**Architecture difference; visible jank hypothesis.** Session/execution/queue/metadata update independently. No new measurement proves this causes roughness.
- **Decision/constraint:** One owner per domain; fix demonstrated contradictions, no broad performance rewrite. Accepted 72 ms typing p95 remains accepted.
- **Compare/pass:** Send through accepted/queued/running/waiting/completed/reconnect. Surfaces agree on identity/progress.
- **Sources:** runtime.ts: [reference](/private/tmp/whip-ux-reference/packages/app/src/runtime.ts), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/runtime.ts) conversation.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/conversation.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/conversation.tsx)

### UX-17 — Changing model clears supported effort

**Source; approved correction implemented.** The Settings default-model form now retains the selected effort when supported by the new model/provider.
- **Decision/constraint:** User approved preservation of compatible effort and reset to Default only when unsupported. Explicit Off stays distinct from Default. Existing sessions are outside this Settings change.
- **Compare/pass:** Regression cases for supported High, supported Off and unsupported Low verify displayed choice and the atomic save payload; the preservation cases failed before the fix and pass afterward. No write occurs before Save.
- **Sources:** configuration.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/settings/configuration.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/configuration.tsx) provider-defaults.tsx: [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/provider-defaults.tsx:62)

### UX-18 — Defaults, limits and stale saves

**Source/semantic.** Default/Off, continuation/retry explanations and conflict policy differ. Atomic category Save is already restored.
- **Decision/constraint:** Keep revision checks/host-ancestor limits/atomicity. Agree semantics/conflict policy per field.
- **Compare/pass:** Multi-field edit/cancel/save, dirty reconnect/competing writer, zero/default/off. No silent overwrite/partial save.
- **Sources:** configuration.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/settings/configuration.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/configuration.tsx) provider-defaults.tsx: [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/provider-defaults.tsx)

### UX-19 — Inapplicable external credential Disconnect

**Source.** Old UI suppressed action/explained external ownership. Native can offer Disconnect then error; advanced route fields expanded.
- **Decision/constraint:** Protect ownership/secrets. Choose hidden/disabled action plus explanation and deliberate advanced disclosure.
- **Compare/pass:** Environment/imported/local credentials. Accurate available actions before acting; no secret exposure.
- **Sources:** provider-connections.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/settings/provider-connections.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/provider-connections.tsx)

### UX-20 — Import and MCP application

**Source/semantic.** Explicit native import review/publication and Refresh/Reload differ from old configuration/application.
- **Decision/constraint:** Keep discovery/host save/session application distinct internally; choose user action composition.
- **Compare/pass:** Toggle source/import/refresh/apply to existing session. Clear what changes, where and when.
- **Sources:** configuration.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/settings/configuration.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/configuration.tsx) mcp-import.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/settings/mcp-import.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/mcp-import.tsx) integrations.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/details/integrations.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/details/integrations.tsx)

### UX-21 — Definition authoring and labels

**Source/semantic.** Persona/Rules became ID/Display name/merged Instructions/module visibility. Choices may expose hashes/executor explanations.
- **Decision/constraint:** Keep immutable definitions/executor ownership; decide grouping, readable choices and disclosure individually.
- **Compare/pass:** Create/edit/select data-only definitions; inspect SDK definitions/revisions. Understandable authoring, no implication renderer hosts code.
- **Sources:** agents.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/settings/agents.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/agents.tsx) definitions.ts: [reference](/private/tmp/whip-ux-reference/packages/app/src/definitions.ts), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/definitions.ts)

### UX-22 — Remember scopes became grants

**Source/authority decision.** Old inline Remember absent; native Allow once/Deny plus separate exact grants. Root-authority prompts are intentional; readable root attribution reduced.
- **Decision/constraint:** No silent broad-rule-to-exact-grant translation. Decide supported persistence UX/readable attribution without widened authority.
- **Compare/pass:** One-time, recurring, child/root-authority requests. Clear requester, scope and duration.
- **Sources:** requests.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/requests.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/requests.tsx) standing-grant.tsx: [native](/private/tmp/whip-native-final-acceptance/packages/app/src/details/standing-grant.tsx)

### UX-23 — Four-question visible window

**Source.** Native has four visible items; exact reply/draft retention restored. Old backend was also bounded.
- **Decision/constraint:** Choose capacity/overflow while retaining exact identity/bounds.
- **Compare/pass:** Five/many questions, edited answer/new arrival/navigation/reconnect. Discoverable remainder and correct retained draft/focus.
- **Sources:** requests.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/requests.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/requests.tsx)

### UX-24 — History language and recovery

**Source/semantic; old file behavior unresolved.** Native exchanges/checkpoints/revisions/tails and no-file-restore copy replace message-relative language describing possible tracked-file restoration. Recovery can detour to Settings.
- **Decision/constraint:** Keep revisions/duplicate protection. Decide copy/location; verify old filesystem semantics before promising rollback.
- **Compare/pass:** Fork/rewind from visible message, racing history/lost acknowledgement. Correct point, clear effects/conflicts/recovery; independently verify file behavior.
- **Sources:** conversation.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/conversation.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/conversation.tsx) session-actions.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/session-actions.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/session-actions.tsx) recovery.tsx: [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/recovery.tsx)

### UX-25 — Advanced additions/recovery destinations

**Source/addition decision.** Saved commands, external Chrome, computer/browser/model inspection expanded. Some shared copy says Settings → Command recovery; General labels it Saved commands.
- **Decision/constraint:** Keep needed capabilities/recovery; decide each surface’s placement and align destination names.
- **Compare/pass:** Reach features normally/from errors. Common actions near subject; instructions lead to named controls.
- **Sources:** recovery.tsx: [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/recovery.tsx) external-browser.tsx: [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/external-browser.tsx) model-inspection.tsx: [native](/private/tmp/whip-native-final-acceptance/packages/app/src/details/model-inspection.tsx) shared.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/details/shared.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/details/shared.tsx:173)

### UX-27 — Readiness/search warmup

**Source; loading regression unmeasured.** Startup/host-picker/sidebar layout foundations unchanged. Native separates readiness/inventory; old search warmup absent.
- **Decision/constraint:** Keep truthful readiness/bounded startup ceiling. Establish impact with equal cold/warm comparisons first.
- **Compare/pass:** Fast/slow/unavailable local host, saved remotes, first search. Appropriate splash release/recovery; no replay on navigation.
- **Sources:** runtime.ts: [reference](/private/tmp/whip-ux-reference/packages/app/src/runtime.ts), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/runtime.ts) startup-screen.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/startup-screen.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/startup-screen.tsx)

### UX-28 — Attachment filenames become generic

**Source; additional finding.** Old upload/presentation kept filename. Native references in this path do not carry it and render Attachment 1, etc. That label also becomes the suggested download filename.
- **Decision/constraint:** Keep verified references. Choose durable name metadata; local-only fix misses reopening/other clients. Possible protocol dependency.
- **Compare/pass:** Two named files through draft/sending/queue/history/reopen/download. Useful names wherever promised, honest unknown-name fallback.
- **Sources:** compositions.ts: [reference](/private/tmp/whip-ux-reference/packages/app/src/compositions.ts), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/compositions.ts) design-input-attachments.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/design-input-attachments.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/design-input-attachments.tsx:22) input-attachment.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/input-attachment.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/input-attachment.tsx)

### UX-29 — New 4 MiB attachment ceiling

**Source; reference acceptance host-dependent.** Native adds 4 MiB upload/image-preview/download bounds. Old uploads respected host limits and had larger image-preview bounds. Both keep 20 MiB total unsent drafts and 256 KiB text uploads.
- **Decision/constraint:** Agree supported sizes/bounded transfers. Do not claim every old host accepted 20 MiB per file.
- **Compare/pass:** Valid 5 MiB image on capable reference host, text/total-draft boundaries. Consistent policy, clear rejection, preserved draft.
- **Sources:** compositions.ts: [reference](/private/tmp/whip-ux-reference/packages/app/src/compositions.ts), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/compositions.ts:257) input-attachment.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/input-attachment.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/input-attachment.tsx)

### UX-30 — Search filters, dates and reopening

**Source; mixed additions/drift.** Native adds host/archive filters, creation instead of update dates and immediate disposal of unused results. Archived sessions remain reachable; removed-shortcut claims are not established against this reference.
- **Decision/constraint:** Keep revision-aware search. Decide date meaning, filters and bounded warm retention separately.
- **Compare/pass:** Two hosts, old-created/recently-updated session, archives, keyboard/reopen/revision change. Approved date/selection/loading behavior.
- **Sources:** session-search-dialog.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/session-search-dialog.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/session-search-dialog.tsx:145)

### UX-31 — Archive feedback/rename conflicts

**Source.** Old archive optimistic with rollback; native metadata read/write then refresh. Rename has reload-current-metadata conflict path and retains draft.
- **Decision/constraint:** Keep revisions/draft. Choose optimistic row behavior or clear pending feedback.
- **Compare/pass:** Delayed/failed archive, undo in sidebar/search, competing rename. Immediate feedback/stable place/no lost intended title.
- **Sources:** session-actions.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/session-actions.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/session-actions.tsx:157)

### UX-32 — Replacement-question notification

**Source/conditional.** Old attention comparison included question IDs; native counts only. Q1 replaced by Q2 between scans at count one does not trigger change. Preserve improved child routing.
- **Decision/constraint:** Keep bounded/no-duplicate alerts. Determine whether IDs/activity revision should support detection; host response dependency.
- **Compare/pass:** Unfocused app, notifications on, replacement without observed zero. New question alerts and opens correct root/child.
- **Sources:** attention-notifications.ts: [reference](/private/tmp/whip-ux-reference/packages/app/src/attention-notifications.ts), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/attention-notifications.ts:67)

### UX-33 — Dock filters after first 16 tree records

**Source/conditional.** Native fetches 16 whole-tree records, then filters selected parent’s children, without paging. Other branches can crowd out relevant children. Old snapshot was also bounded.
- **Decision/constraint:** Choose parent-scoped retrieval, bounded paging or clear route to omitted children.
- **Compare/pass:** Over 16 records/multiple parents/relevant child beyond page one. Reachable children; honest partial-roster scope.
- **Sources:** agent-dock.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/agent-dock.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/agent-dock.tsx:89)

### UX-34 — Agent tree scanability/intervention

**Source.** Native replacement pages/IDs omit old report/blocking/terminal-cause/mail summaries. Activity/Cancel current turn appear only for selected agent. Stop subtree/Resume/Delete are separate lifecycle actions.
- **Decision/constraint:** Keep lifecycle/authority. Decide readable summaries and bounded nonselected activity/actions.
- **Compare/pass:** Running/failed/waiting/completed children. Understand/intervene without confusing turn cancellation and subtree stop.
- **Sources:** observation.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/details/observation.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/details/observation.tsx:105)

### UX-35 — Mailbox excerpts/pages

**Source.** Old excerpts/accumulated bounded pages became metadata/IDs/revision/bytes, explicit body reads and replacement pages.
- **Decision/constraint:** Read never delivers/acknowledges mail. Decide safe excerpts and append-versus-page.
- **Compare/pass:** Scan/filter/page multiple recipients during arrivals, inspect long body. Identify messages without needless inspect cycles; retain scope/place.
- **Sources:** observation.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/details/observation.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/details/observation.tsx:186)

### UX-36 — Inspector pager exhaustion

**Source/conditional.** Several callers derive Next from last row even on short final page. Agents/mail/blackboard/compaction/grants can offer an empty next page; schedules uses explicit continuation. Generic First/Next replaces pages.
- **Decision/constraint:** Respect individual contracts. Choose probe/end semantics; not every API has has-more.
- **Compare/pass:** Empty/short/exact-size/multiple pages per caller. Understandable exhaustion, no misleading continuation or unexpected lost selection.
- **Sources:** shared.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/details/shared.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/details/shared.tsx:208) observation.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/details/observation.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/details/observation.tsx) session-controls.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/details/session-controls.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/details/session-controls.tsx)

### UX-37 — Goal authoring/lifecycle

**Source/semantic.** Native state, continuation allowance default 100, Resume/Cancel/Refresh differ from Save/Run/Draft/Clear. Cancelling recorded work is not clearing old goal text.
- **Decision/constraint:** Keep host-owned continuation; agree authoring and any clear/archive presentation.
- **Compare/pass:** Root/child save-without-run/start/pause-resume/cancel/disconnect. Names describe persisted state/ongoing work.
- **Sources:** session-controls.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/details/session-controls.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/details/session-controls.tsx:49)

### UX-38 — Schedule history/prompt/recipient

**Source/semantic.** Native retains cancelled history, preview/full-read prompt and selected-session targeting. Reference Delete/inline prompt differed; root/child wake semantics need equal-recipient comparison.
- **Decision/constraint:** Keep durable scheduling. Decide history/disclosure/child scheduling.
- **Compare/pass:** Root/child schedules, long prompt, cancel/reconnect/due wake. Correct recipient; UI closure never implies cancellation.
- **Sources:** session-controls.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/details/session-controls.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/details/session-controls.tsx:234) scheduled-wake-notice.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/scheduled-wake-notice.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/scheduled-wake-notice.tsx)

### UX-39 — Compaction Apply changed scope

**Source/material semantic decision.** Old inspector edited host defaults and reloaded idle session. Native edits selected-agent policy; host defaults stay in Settings. Model/threshold fields became more literal text/numeric controls.
- **Decision/constraint:** Keep host/session distinction. Agree scope/inheritance/model-entry UX; never silently change Apply meaning.
- **Compare/pass:** Edit root/child/Settings then create another session. Unmistakable scope and inherited defaults.
- **Sources:** session-controls.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/details/session-controls.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/details/session-controls.tsx:626) compaction-settings.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/compaction-settings.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/compaction-settings.tsx)

### UX-40 — Reload location/contradictory copy

**Source.** Reload moved to Applied context with request/check/cancel states. Model/runtime still says captured-default reload is unavailable in this inspector although SessionReload is mounted nearby.
- **Decision/constraint:** Keep truthful native reload/recovery; choose location/progress language, align guidance.
- **Compare/pass:** Change defaults/find Reload/delay acknowledgement/revisit. Findable, correct pending/applied status, consistent copy.
- **Sources:** session-controls.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/details/session-controls.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/details/session-controls.tsx:588) session-reload.tsx: [native](/private/tmp/whip-native-final-acceptance/packages/app/src/details/session-reload.tsx)

### UX-41 — Trace default/browsing state

**Source.** Old latest trace/input labels became All traces · loaded window, turn-ID labels, explicit older/latest windows. Window/epoch changes reset selection/zoom; offline trace selection disabled. Chart foundations remain.
- **Decision/constraint:** Keep bounded SDK evidence/truthful timing. Agree default, labels and retained viewing state.
- **Compare/pass:** Multiple turns, select/zoom/page/reconnect/latest. Task-appropriate default, clear history window, intentional state changes.
- **Sources:** trace-view.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/trace-view.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/trace-view.tsx:239) trace-state.ts: [native](/private/tmp/whip-native-final-acceptance/packages/sdk/src/trace-state.ts)

### UX-42 — Browser selection clears on unrelated revision

**Source/conditional.** Share recipient and preview cwd clear on catalog revision, including unrelated conversation changes.
- **Decision/constraint:** Keep fresh recipient verification; choose selected-item revalidation versus unconditional reset.
- **Compare/pass:** Select recipient/project, rename/add another conversation. Valid choice survives; deletion/identity change invalidates visibly before mutation.
- **Sources:** browser-provider-controls.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/browser-provider-controls.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/browser-provider-controls.tsx:80) browser-preview-controls.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/browser-preview-controls.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/browser-preview-controls.tsx)

### UX-43 — Closing an offline terminal

**Source/semantic.** Old best-effort close discarded tab; native waits for acknowledgement and can retain on offline/error/uncertain outcomes. Terminal close can end shell; conversation view close differs.
- **Decision/constraint:** Never claim shell ended without evidence. Choose Close view/End terminal or another truthful contract.
- **Compare/pass:** Online/offline/lost-ack close. Clear shell/view outcomes and unresolved recovery.
- **Sources:** session-tab-strip.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/session-tab-strip.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/session-tab-strip.tsx)

### UX-44 — Stale local/SSH recovery

**Source policy; classification untested. Desktop only.** Old stale-socket recovery differs from refusing native unhealthy status. Exact classification of a stale socket needs reproduction.
- **Decision/constraint:** Preserve compatibility/identity checks. Establish classification before targeted recovery; no normal-runtime killing implied.
- **Compare/pass:** Disposable local/SSH homes: stopped/stale socket/incompatible/unhealthy. Safe understandable recovery.
- **Sources:** runtime.ts: [reference](/private/tmp/whip-ux-reference/apps/desktop/src/runtime.ts), [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/runtime.ts) ssh.ts: [reference](/private/tmp/whip-ux-reference/apps/desktop/src/ssh.ts), [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/ssh.ts)

### UX-45 — Shared-state scope/access

**Source/additive.** Native explicit tree/agent scope and immutable revision. Text preview 1 MiB; explicit downloads up to 64 MiB, distinct from message/attachment bounds.
- **Decision/constraint:** Keep scope/version/bounds. Agree primary scope/disclosure while preserving useful additions.
- **Compare/pass:** Same-named tree/agent keys, changed version, large value/interrupted read. Correct scope/revision; inspection neither publishes nor admits state into model context.
- **Sources:** observation.tsx: [reference](/private/tmp/whip-ux-reference/packages/app/src/details/observation.tsx), [native](/private/tmp/whip-native-final-acceptance/packages/app/src/details/observation.tsx:253) state-read.tsx: [native](/private/tmp/whip-native-final-acceptance/packages/app/src/details/state-read.tsx)

## Retained behavior gates

Existing targeted passes support their own fixtures. These gates protect restored behavior while completing parity.

| Gate | Required acceptance |
| --- | --- |
| P01 Layout/startup | Same hierarchy/width/tabs/sidebar, responsive/theme/motion/loading footprint; native browser hidden behind splash/overlays. Styling alone does not prove loading parity. |
| P02 Keyboard/focus | Commands/search/settings/tab menus, Escape/Enter, focus return, IME and ordinary editing. |
| P03 Place/navigation | Independent split anchors; draft/caret/recipient through root/child/tabs/settings/back-forward/close-reopen/host changes; no background focus/scroll theft. |
| P04 Ordinary send | Correct model/effort/definition/cwd; one authored message through acceptance/queue/live/history; attachment staging, duplicate-safe recovery, completed-child composer. |
| P05 Reading | Markdown/code/copy/Mermaid, activity/errors/timestamps/wrapping, long history, deliberate autoscroll, stable older reading. |
| P06 Intervention | Exact allow/deny/reply and retained drafts, authority attribution, cancel/stop/steer; navigation never cancels accepted work. |
| P07 Inspection | Execution/model evidence, truthful unknown/partial usage/budgets, scoped reads/lifecycle/grants/context; failure never fabricates completion. |
| P08 Browser/design | Toolbar/native geometry/overlays, preview/share authority, exact recipient/context/screenshot; new external browser/computer behavior separately accepted. |
| P09 Terminal | Correct cwd/input/output/resize/profile, retained canvas/selection, reconnect/truthful close; human-owned shell. |
| P10 Settings/servers | Login/routes/atomic Save/nested discard focus/dirty reconnect/conflicts; local/URL/SSH identity/editor actions; clear import save/apply. |
| P11 Preferences/notifications | Themes/imports/shortcuts/addresses persist; correct notification recipient; credentials not stored as presentation preferences. |
| P12 Platform | Downloads/names/clipboard/dialogs/links/windows/menus/updates, web fallbacks/desktop bootstrap/CSP. Renderer tests alone insufficient. |

## Existing evidence and limits

The prior [acceptance index](/private/tmp/whip-native-final-acceptance/docs/frontend-ux-restoration-acceptance.md) records 1,480 renderer tests, 223 SDK tests, 18 interoperability checks and focused backend/desktop/browser journeys. These are retained reports, not runs for this inventory or complete parity acceptance.

Provider comparisons had unequal starting conditions: reference defaults were seeded; native tests accepted extra model confirmation. Content checks used small files and accepted generic Attachment names. REPL capability checks missed argument summaries. A prior reference nested-discard/focus failure and baseline contrast failure are not explained by narrower passing reruns. The product-UX task is native-focused, not the complete differential matrix. Assertions must follow approved product expectations, not silently ratify current behavior.

The earlier review found PR #298 draft/unmerged with no reported checks; remote status was not refreshed here. Current workflow PR-base filters also omit this frontend-UX base naming. Hosted validation/release readiness remain separate obligations.

## Next steps

1. **Finish adjacent provider settings decisions:** UX-01–03, the onboarding portion of UX-04 and UX-17 are implemented. UX-19 and configured-host suggestions remain to review. The user accepts the current credential status wording for now.
2. **Verify ordinary continuity:** UX-05–08, 10, 14–16, 28, 33. Reproduce conditional paths, then implement only agreed behavior.
3. **Make scope/authority decisions individually:** UX-18, 20–24, 37–40, 43. Prioritize remembered approval, compaction scope, goals, history/file expectations and terminal close.
4. **Complete navigation/inspection/platform comparisons:** remaining entries and P01–P12. Track approved differences, baseline defects and unresolved hypotheses separately.
5. **Accept a pinned build:** record user decision, exact fixture/build, result/evidence per entry; complete required hosted checks. Source coverage is not parity, merge or release acceptance.

| Comparison pack | Same-precondition scenarios | Entries / gates |
| --- | --- | --- |
| C1 Onboarding | No/environment credentials, explicit defaults, custom route, retry | 01–04, 17, 19, 27; P01/P04/P10 |
| C2 Continuity | Root/child drafts, failed metadata, queue overflow, split/settings/navigation, reconnect/identity change | 06, 08, 10, 14–16, 23, 33; P02–P06 |
| C3 Content/REPL | Named files, capable-host 5 MiB image, large Markdown, call summaries, restarts/older evidence | 11–13, 15, 28–29; P05/P07 |
| C4 Navigation | Over 100/500 sessions, delayed/failed archive, rename conflict, cross-host search, question replacement | 09, 30–32; P02/P03/P11 |
| C5 Authority/history | Root/child/recurring requests, many questions, fork/rewind races/lost acknowledgement | 22–24; P06/P07 |
| C6 Settings | Atomic save/discard/conflicts, imports/definitions, host-versus-agent compaction, pending reload | 17–21, 39–40; P10/P11 |
| C7 Inspectors | Over 16 records, mixed child states, mail/state/pages/limits, goals/schedules/traces | 25, 33–41, 45; P07 |
| C8 Browser | Native surfaces/overlays, picker during revision, design context/screenshot, external browser/computer | 25, 42; P08 |
| C9 Terminal/server | Slow open/delayed cwd/retained selection/offline close/disposable stale local-SSH | 05–07, 43–44; P09/P12 |
| C10 Shell/platform | Keyboard/IME/focus/themes/motion, clipboard/downloads/dialogs/windows/updates, web-only fallback/bootstrap | 25, 27–29, 32; P01–P03/P11/P12 |

These packs remain proposed comparisons; completed focused checks are linked from the implementation record. Continue with explicit product decisions and scoped evidence, not another broad migration-complete declaration.
