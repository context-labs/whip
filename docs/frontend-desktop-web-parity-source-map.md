# Desktop/web parity source map

Research checkpoint: 2026-09-29. Companion to the [decision inventory](/private/tmp/whip-native-final-acceptance/docs/frontend-desktop-web-parity-inventory.md). This is a mechanical coverage index, not a record that every file, test assertion or native behavior passed a new audit. The main inventory records the actual findings and comparison obligations.

## Method and bounds

Reference and native identities are pinned in the main inventory. The approved 14-file overlay was verified with SHA-256. Production counts compare actual working-file bytes for `.ts`, `.tsx`, `.css`, `.mjs` and `.js`, excluding `.test.`, `.spec.` and `__tests__`. Added data bindings count as changes even when UI styling is unchanged. SDK/protocol/host files appear as dependencies in the main inventory; this is not a whole-backend audit.

The production map assigns every included file to a user-facing surface or retained gate. This assignment is a coverage pointer, not a claim that every file contains a defect. Generated/module-export files are included mechanically. Build configuration/assets and platform acceptance are covered by P12, not included in these source-file counts.

## Production scope totals

| Area | Reference | Changed | Added | Removed | Unchanged |
| --- | ---: | ---: | ---: | ---: | ---: |
| `packages/app/src` | 141 | 89 | 21 | 0 | 52 |
| `packages/ui/src` | 35 | 2 | 0 | 0 | 33 |
| `apps/web/src` | 6 | 2 | 0 | 0 | 4 |
| `apps/desktop/src` | 34 | 9 | 1 | 0 | 25 |

No percentage is inferred from these numbers. In particular, shared UI primitives and native Browser foundations largely remain, but changing data, lifetimes or action semantics can alter their behavior.

## Production file coverage

### packages/app/src

| File | Byte comparison | Workflow / gate | Sources |
| --- | --- | --- | --- |
| `agent-dock.tsx` | Changed | Children/turn notices; UX-14–15/33, P04/P07 | [reference](/private/tmp/whip-ux-reference/packages/app/src/agent-dock.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/agent-dock.tsx) |
| `agent-turn-notice.tsx` | Changed | Children/turn notices; UX-14–15/33, P04/P07 | [reference](/private/tmp/whip-ux-reference/packages/app/src/agent-turn-notice.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/agent-turn-notice.tsx) |
| `attention-notifications.ts` | Changed | Requests/attention; UX-22–23/32, P06/P11 | [reference](/private/tmp/whip-ux-reference/packages/app/src/attention-notifications.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/attention-notifications.ts) |
| `attention.tsx` | Changed | Requests/attention; UX-22–23/32, P06/P11 | [reference](/private/tmp/whip-ux-reference/packages/app/src/attention.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/attention.tsx) |
| `browser-address.ts` | Unchanged | Browser/content; UX-25/28–29/42, P08 | [reference](/private/tmp/whip-ux-reference/packages/app/src/browser-address.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/browser-address.ts) |
| `browser-agent-types.ts` | Changed | Browser/content; UX-25/28–29/42, P08 | [reference](/private/tmp/whip-ux-reference/packages/app/src/browser-agent-types.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/browser-agent-types.ts) |
| `browser-design-attachment.tsx` | Unchanged | Browser/content; UX-25/28–29/42, P08 | [reference](/private/tmp/whip-ux-reference/packages/app/src/browser-design-attachment.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/browser-design-attachment.tsx) |
| `browser-design-controller.ts` | Unchanged | Browser/content; UX-25/28–29/42, P08 | [reference](/private/tmp/whip-ux-reference/packages/app/src/browser-design-controller.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/browser-design-controller.ts) |
| `browser-design-geometry.ts` | Unchanged | Browser/content; UX-25/28–29/42, P08 | [reference](/private/tmp/whip-ux-reference/packages/app/src/browser-design-geometry.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/browser-design-geometry.ts) |
| `browser-design-overlay.tsx` | Unchanged | Browser/content; UX-25/28–29/42, P08 | [reference](/private/tmp/whip-ux-reference/packages/app/src/browser-design-overlay.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/browser-design-overlay.tsx) |
| `browser-design-presentation.ts` | Changed | Browser/content; UX-25/28–29/42, P08 | [reference](/private/tmp/whip-ux-reference/packages/app/src/browser-design-presentation.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/browser-design-presentation.ts) |
| `browser-design-types.ts` | Unchanged | Browser/content; UX-25/28–29/42, P08 | [reference](/private/tmp/whip-ux-reference/packages/app/src/browser-design-types.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/browser-design-types.ts) |
| `browser-design.tsx` | Changed | Browser/content; UX-25/28–29/42, P08 | [reference](/private/tmp/whip-ux-reference/packages/app/src/browser-design.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/browser-design.tsx) |
| `browser-preview-controls.tsx` | Changed | Browser/content; UX-25/28–29/42, P08 | [reference](/private/tmp/whip-ux-reference/packages/app/src/browser-preview-controls.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/browser-preview-controls.tsx) |
| `browser-provider-controls.tsx` | Changed | Browser/content; UX-25/28–29/42, P08 | [reference](/private/tmp/whip-ux-reference/packages/app/src/browser-provider-controls.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/browser-provider-controls.tsx) |
| `browser-provider.ts` | Changed | Browser/content; UX-25/28–29/42, P08 | [reference](/private/tmp/whip-ux-reference/packages/app/src/browser-provider.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/browser-provider.ts) |
| `browser-types.ts` | Unchanged | Browser/content; UX-25/28–29/42, P08 | [reference](/private/tmp/whip-ux-reference/packages/app/src/browser-types.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/browser-types.ts) |
| `browser-view.tsx` | Unchanged | Browser/content; UX-25/28–29/42, P08 | [reference](/private/tmp/whip-ux-reference/packages/app/src/browser-view.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/browser-view.tsx) |
| `browser-workspace.ts` | Unchanged | Browser/content; UX-25/28–29/42, P08 | [reference](/private/tmp/whip-ux-reference/packages/app/src/browser-workspace.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/browser-workspace.ts) |
| `chat-activity-rows.ts` | Changed | Reading/status; UX-08/10–16/24/38, P03/P05 | [reference](/private/tmp/whip-ux-reference/packages/app/src/chat-activity-rows.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/chat-activity-rows.ts) |
| `chat-activity.tsx` | Changed | Reading/status; UX-08/10–16/24/38, P03/P05 | [reference](/private/tmp/whip-ux-reference/packages/app/src/chat-activity.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/chat-activity.tsx) |
| `chat-file-drop.tsx` | Unchanged | Shared shell/presentation; P01–P05/P11/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/chat-file-drop.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/chat-file-drop.tsx) |
| `chat-submission.ts` | Changed | Draft/send/queue; UX-08/10/28–29, P04 | [reference](/private/tmp/whip-ux-reference/packages/app/src/chat-submission.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/chat-submission.ts) |
| `client-query-key.ts` | Added | Shared shell/presentation; P01–P05/P11/P12 | [native](/private/tmp/whip-native-final-acceptance/packages/app/src/client-query-key.ts) |
| `compaction-settings.tsx` | Changed | Configuration/integrations; UX-18/20/39, P10 | [reference](/private/tmp/whip-ux-reference/packages/app/src/compaction-settings.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/compaction-settings.tsx) |
| `completion-picker.tsx` | Changed | Draft/send/queue; UX-08/10/28–29, P04 | [reference](/private/tmp/whip-ux-reference/packages/app/src/completion-picker.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/completion-picker.tsx) |
| `composer-attachments.tsx` | Changed | Draft/send/queue; UX-08/10/28–29, P04 | [reference](/private/tmp/whip-ux-reference/packages/app/src/composer-attachments.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/composer-attachments.tsx) |
| `composer-panels.stylex.ts` | Unchanged | Draft/send/queue; UX-08/10/28–29, P04 | [reference](/private/tmp/whip-ux-reference/packages/app/src/composer-panels.stylex.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/composer-panels.stylex.ts) |
| `composer-queue.tsx` | Changed | Draft/send/queue; UX-08/10/28–29, P04 | [reference](/private/tmp/whip-ux-reference/packages/app/src/composer-queue.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/composer-queue.tsx) |
| `composer.tsx` | Changed | Draft/send/queue; UX-08/10/28–29, P04 | [reference](/private/tmp/whip-ux-reference/packages/app/src/composer.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/composer.tsx) |
| `compositions.ts` | Changed | Draft/send/queue; UX-08/10/28–29, P04 | [reference](/private/tmp/whip-ux-reference/packages/app/src/compositions.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/compositions.ts) |
| `connection-dialog.tsx` | Unchanged | Connection/ownership; UX-06/08/16/27, P10/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/connection-dialog.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/connection-dialog.tsx) |
| `connection-notice.tsx` | Changed | Connection/ownership; UX-06/08/16/27, P10/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/connection-notice.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/connection-notice.tsx) |
| `connections.ts` | Changed | Connection/ownership; UX-06/08/16/27, P10/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/connections.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/connections.ts) |
| `context.tsx` | Unchanged | Shared shell/presentation; P01–P05/P11/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/context.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/context.tsx) |
| `conversation-history.ts` | Added | Reading/status; UX-08/10–16/24/38, P03/P05 | [native](/private/tmp/whip-native-final-acceptance/packages/app/src/conversation-history.ts) |
| `conversation-rows.ts` | Changed | Reading/status; UX-08/10–16/24/38, P03/P05 | [reference](/private/tmp/whip-ux-reference/packages/app/src/conversation-rows.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/conversation-rows.ts) |
| `conversation.tsx` | Changed | Reading/status; UX-08/10–16/24/38, P03/P05 | [reference](/private/tmp/whip-ux-reference/packages/app/src/conversation.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/conversation.tsx) |
| `definitions.ts` | Changed | Entry/setup; UX-01–04/07/17/19/21/27, P04/P10 | [reference](/private/tmp/whip-ux-reference/packages/app/src/definitions.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/definitions.ts) |
| `design-input-attachments.tsx` | Changed | Browser/content; UX-25/28–29/42, P08 | [reference](/private/tmp/whip-ux-reference/packages/app/src/design-input-attachments.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/design-input-attachments.tsx) |
| `desktop-bridge.ts` | Changed | Connection/ownership; UX-06/08/16/27, P10/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/desktop-bridge.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/desktop-bridge.ts) |
| `details/content-read.tsx` | Added | Inspectors; UX-20/22/25/34–41/45, P07 | [native](/private/tmp/whip-native-final-acceptance/packages/app/src/details/content-read.tsx) |
| `details/external-browser.tsx` | Added | Inspectors; UX-20/22/25/34–41/45, P07 | [native](/private/tmp/whip-native-final-acceptance/packages/app/src/details/external-browser.tsx) |
| `details/integrations.tsx` | Changed | Inspectors; UX-20/22/25/34–41/45, P07 | [reference](/private/tmp/whip-ux-reference/packages/app/src/details/integrations.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/details/integrations.tsx) |
| `details/model-inspection.tsx` | Added | Inspectors; UX-20/22/25/34–41/45, P07 | [native](/private/tmp/whip-native-final-acceptance/packages/app/src/details/model-inspection.tsx) |
| `details/observation.tsx` | Changed | Inspectors; UX-20/22/25/34–41/45, P07 | [reference](/private/tmp/whip-ux-reference/packages/app/src/details/observation.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/details/observation.tsx) |
| `details/session-controls.tsx` | Changed | Inspectors; UX-20/22/25/34–41/45, P07 | [reference](/private/tmp/whip-ux-reference/packages/app/src/details/session-controls.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/details/session-controls.tsx) |
| `details/session-reload.tsx` | Added | Inspectors; UX-20/22/25/34–41/45, P07 | [native](/private/tmp/whip-native-final-acceptance/packages/app/src/details/session-reload.tsx) |
| `details/shared.tsx` | Changed | Inspectors; UX-20/22/25/34–41/45, P07 | [reference](/private/tmp/whip-ux-reference/packages/app/src/details/shared.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/details/shared.tsx) |
| `details/standing-grant.tsx` | Added | Inspectors; UX-20/22/25/34–41/45, P07 | [native](/private/tmp/whip-native-final-acceptance/packages/app/src/details/standing-grant.tsx) |
| `details/state-read.tsx` | Added | Inspectors; UX-20/22/25/34–41/45, P07 | [native](/private/tmp/whip-native-final-acceptance/packages/app/src/details/state-read.tsx) |
| `details/usage.tsx` | Added | Inspectors; UX-20/22/25/34–41/45, P07 | [native](/private/tmp/whip-native-final-acceptance/packages/app/src/details/usage.tsx) |
| `directory-picker.tsx` | Changed | Entry/setup; UX-01–04/07/17/19/21/27, P04/P10 | [reference](/private/tmp/whip-ux-reference/packages/app/src/directory-picker.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/directory-picker.tsx) |
| `directory-queries.ts` | Changed | Entry/setup; UX-01–04/07/17/19/21/27, P04/P10 | [reference](/private/tmp/whip-ux-reference/packages/app/src/directory-queries.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/directory-queries.ts) |
| `empty-workspace.tsx` | Changed | Entry/setup; UX-01–04/07/17/19/21/27, P04/P10 | [reference](/private/tmp/whip-ux-reference/packages/app/src/empty-workspace.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/empty-workspace.tsx) |
| `error-feedback.tsx` | Unchanged | Shared shell/presentation; P01–P05/P11/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/error-feedback.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/error-feedback.tsx) |
| `execution-output.ts` | Added | Evidence views; UX-12–13/15/41, P07 | [native](/private/tmp/whip-native-final-acceptance/packages/app/src/execution-output.ts) |
| `execution-time.tsx` | Changed | Evidence views; UX-12–13/15/41, P07 | [reference](/private/tmp/whip-ux-reference/packages/app/src/execution-time.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/execution-time.tsx) |
| `history-gap.tsx` | Changed | Reading/status; UX-08/10–16/24/38, P03/P05 | [reference](/private/tmp/whip-ux-reference/packages/app/src/history-gap.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/history-gap.tsx) |
| `host-connection-dialog.tsx` | Unchanged | Connection/ownership; UX-06/08/16/27, P10/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/host-connection-dialog.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/host-connection-dialog.tsx) |
| `host-connection.stylex.ts` | Unchanged | Connection/ownership; UX-06/08/16/27, P10/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/host-connection.stylex.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/host-connection.stylex.ts) |
| `host-dialog.tsx` | Changed | Connection/ownership; UX-06/08/16/27, P10/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/host-dialog.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/host-dialog.tsx) |
| `host-prompt-controller.ts` | Unchanged | Connection/ownership; UX-06/08/16/27, P10/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/host-prompt-controller.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/host-prompt-controller.ts) |
| `host-prompt-form.tsx` | Unchanged | Connection/ownership; UX-06/08/16/27, P10/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/host-prompt-form.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/host-prompt-form.tsx) |
| `host-prompts.tsx` | Unchanged | Connection/ownership; UX-06/08/16/27, P10/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/host-prompts.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/host-prompts.tsx) |
| `host-selector.tsx` | Unchanged | Connection/ownership; UX-06/08/16/27, P10/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/host-selector.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/host-selector.tsx) |
| `hosts.ts` | Changed | Connection/ownership; UX-06/08/16/27, P10/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/hosts.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/hosts.ts) |
| `index.tsx` | Unchanged | Shared shell/presentation; P01–P05/P11/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/index.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/index.tsx) |
| `input-attachment.tsx` | Changed | Browser/content; UX-25/28–29/42, P08 | [reference](/private/tmp/whip-ux-reference/packages/app/src/input-attachment.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/input-attachment.tsx) |
| `input-presentation.ts` | Changed | Draft/send/queue; UX-08/10/28–29, P04 | [reference](/private/tmp/whip-ux-reference/packages/app/src/input-presentation.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/input-presentation.ts) |
| `inspector.tsx` | Changed | Shared shell/presentation; P01–P05/P11/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/inspector.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/inspector.tsx) |
| `markdown-code-block.tsx` | Unchanged | Shared shell/presentation; P01–P05/P11/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/markdown-code-block.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/markdown-code-block.tsx) |
| `mcp-brand.tsx` | Changed | Configuration/integrations; UX-18/20/39, P10 | [reference](/private/tmp/whip-ux-reference/packages/app/src/mcp-brand.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/mcp-brand.tsx) |
| `mcp-import.tsx` | Changed | Configuration/integrations; UX-18/20/39, P10 | [reference](/private/tmp/whip-ux-reference/packages/app/src/mcp-import.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/mcp-import.tsx) |
| `mcp-refresh.ts` | Changed | Configuration/integrations; UX-18/20/39, P10 | [reference](/private/tmp/whip-ux-reference/packages/app/src/mcp-refresh.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/mcp-refresh.ts) |
| `message-attachments.tsx` | Added | Browser/content; UX-25/28–29/42, P08 | [native](/private/tmp/whip-native-final-acceptance/packages/app/src/message-attachments.tsx) |
| `model-options.ts` | Changed | Entry/setup; UX-01–04/07/17/19/21/27, P04/P10 | [reference](/private/tmp/whip-ux-reference/packages/app/src/model-options.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/model-options.ts) |
| `model-selection.tsx` | Changed | Entry/setup; UX-01–04/07/17/19/21/27, P04/P10 | [reference](/private/tmp/whip-ux-reference/packages/app/src/model-selection.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/model-selection.tsx) |
| `navigation.ts` | Unchanged | Shared shell/presentation; P01–P05/P11/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/navigation.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/navigation.ts) |
| `new-chat.ts` | Added | Entry/setup; UX-01–04/07/17/19/21/27, P04/P10 | [native](/private/tmp/whip-native-final-acceptance/packages/app/src/new-chat.ts) |
| `permission-mode.tsx` | Changed | Requests/attention; UX-22–23/32, P06/P11 | [reference](/private/tmp/whip-ux-reference/packages/app/src/permission-mode.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/permission-mode.tsx) |
| `permission-scope.ts` | Unchanged | Requests/attention; UX-22–23/32, P06/P11 | [reference](/private/tmp/whip-ux-reference/packages/app/src/permission-scope.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/permission-scope.ts) |
| `platform.ts` | Unchanged | Shared shell/presentation; P01–P05/P11/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/platform.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/platform.ts) |
| `presentation.ts` | Changed | Shared shell/presentation; P01–P05/P11/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/presentation.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/presentation.ts) |
| `provider-logo.tsx` | Unchanged | Entry/setup; UX-01–04/07/17/19/21/27, P04/P10 | [reference](/private/tmp/whip-ux-reference/packages/app/src/provider-logo.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/provider-logo.tsx) |
| `provider-readiness.ts` | Changed | Entry/setup; UX-01–04/07/17/19/21/27, P04/P10 | [reference](/private/tmp/whip-ux-reference/packages/app/src/provider-readiness.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/provider-readiness.ts) |
| `provider-setup.tsx` | Changed | Entry/setup; UX-01–04/07/17/19/21/27, P04/P10 | [reference](/private/tmp/whip-ux-reference/packages/app/src/provider-setup.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/provider-setup.tsx) |
| `reading-list.tsx` | Changed | Reading/status; UX-08/10–16/24/38, P03/P05 | [reference](/private/tmp/whip-ux-reference/packages/app/src/reading-list.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/reading-list.tsx) |
| `reading-positions.ts` | Changed | Reading/status; UX-08/10–16/24/38, P03/P05 | [reference](/private/tmp/whip-ux-reference/packages/app/src/reading-positions.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/reading-positions.ts) |
| `recent-projects.ts` | Added | Catalog/actions; UX-09/24/30–31, P03 | [native](/private/tmp/whip-native-final-acceptance/packages/app/src/recent-projects.ts) |
| `recovery-storage.ts` | Added | Shared shell/presentation; P01–P05/P11/P12 | [native](/private/tmp/whip-native-final-acceptance/packages/app/src/recovery-storage.ts) |
| `remote-directory-dialog.stylex.ts` | Changed | Entry/setup; UX-01–04/07/17/19/21/27, P04/P10 | [reference](/private/tmp/whip-ux-reference/packages/app/src/remote-directory-dialog.stylex.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/remote-directory-dialog.stylex.ts) |
| `remote-directory-dialog.tsx` | Changed | Entry/setup; UX-01–04/07/17/19/21/27, P04/P10 | [reference](/private/tmp/whip-ux-reference/packages/app/src/remote-directory-dialog.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/remote-directory-dialog.tsx) |
| `repl-view.stylex.ts` | Unchanged | Evidence views; UX-12–13/15/41, P07 | [reference](/private/tmp/whip-ux-reference/packages/app/src/repl-view.stylex.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/repl-view.stylex.ts) |
| `repl-view.tsx` | Changed | Evidence views; UX-12–13/15/41, P07 | [reference](/private/tmp/whip-ux-reference/packages/app/src/repl-view.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/repl-view.tsx) |
| `requests.tsx` | Changed | Requests/attention; UX-22–23/32, P06/P11 | [reference](/private/tmp/whip-ux-reference/packages/app/src/requests.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/requests.tsx) |
| `routeTree.gen.ts` | Unchanged | Shared shell/presentation; P01–P05/P11/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/routeTree.gen.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/routeTree.gen.ts) |
| `routes/__root.tsx` | Unchanged | Routes; entry-point table, P03/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/routes/__root.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/routes/__root.tsx) |
| `routes/browser.$viewId.tsx` | Unchanged | Routes; entry-point table, P03/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/routes/browser.$viewId.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/routes/browser.$viewId.tsx) |
| `routes/h.$runtimeId.s.$rootId.tsx` | Unchanged | Routes; entry-point table, P03/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/routes/h.$runtimeId.s.$rootId.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/routes/h.$runtimeId.s.$rootId.tsx) |
| `routes/h.$runtimeId.t.$terminalId.tsx` | Changed | Routes; entry-point table, P03/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/routes/h.$runtimeId.t.$terminalId.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/routes/h.$runtimeId.t.$terminalId.tsx) |
| `routes/index.tsx` | Unchanged | Routes; entry-point table, P03/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/routes/index.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/routes/index.tsx) |
| `routes/new.$draftId.tsx` | Unchanged | Routes; entry-point table, P03/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/routes/new.$draftId.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/routes/new.$draftId.tsx) |
| `routes/settings.tsx` | Unchanged | Routes; entry-point table, P03/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/routes/settings.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/routes/settings.tsx) |
| `runtime.ts` | Changed | Connection/ownership; UX-06/08/16/27, P10/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/runtime.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/runtime.ts) |
| `scheduled-wake-notice.tsx` | Changed | Reading/status; UX-08/10–16/24/38, P03/P05 | [reference](/private/tmp/whip-ux-reference/packages/app/src/scheduled-wake-notice.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/scheduled-wake-notice.tsx) |
| `session-actions.tsx` | Changed | Catalog/actions; UX-09/24/30–31, P03 | [reference](/private/tmp/whip-ux-reference/packages/app/src/session-actions.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/session-actions.tsx) |
| `session-search-dialog.tsx` | Changed | Catalog/actions; UX-09/24/30–31, P03 | [reference](/private/tmp/whip-ux-reference/packages/app/src/session-search-dialog.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/session-search-dialog.tsx) |
| `session-sidebar.stylex.ts` | Unchanged | Catalog/actions; UX-09/24/30–31, P03 | [reference](/private/tmp/whip-ux-reference/packages/app/src/session-sidebar.stylex.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/session-sidebar.stylex.ts) |
| `session-sidebar.tsx` | Changed | Catalog/actions; UX-09/24/30–31, P03 | [reference](/private/tmp/whip-ux-reference/packages/app/src/session-sidebar.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/session-sidebar.tsx) |
| `session-status.ts` | Changed | Shared shell/presentation; P01–P05/P11/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/session-status.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/session-status.ts) |
| `session-tab-routing.ts` | Changed | Workspace/tabs; UX-06/43, P02/P03 | [reference](/private/tmp/whip-ux-reference/packages/app/src/session-tab-routing.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/session-tab-routing.ts) |
| `session-tab-strip.tsx` | Changed | Workspace/tabs; UX-06/43, P02/P03 | [reference](/private/tmp/whip-ux-reference/packages/app/src/session-tab-strip.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/session-tab-strip.tsx) |
| `session-tabs.ts` | Changed | Workspace/tabs; UX-06/43, P02/P03 | [reference](/private/tmp/whip-ux-reference/packages/app/src/session-tabs.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/session-tabs.ts) |
| `session-top-bar.tsx` | Unchanged | Shared shell/presentation; P01–P05/P11/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/session-top-bar.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/session-top-bar.tsx) |
| `settings.tsx` | Unchanged | Shared shell/presentation; P01–P05/P11/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/settings.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings.tsx) |
| `settings/about.tsx` | Unchanged | Settings; UX-17–21/25/39, P10/P11 | [reference](/private/tmp/whip-ux-reference/packages/app/src/settings/about.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/about.tsx) |
| `settings/agents.tsx` | Changed | Settings; UX-17–21/25/39, P10/P11 | [reference](/private/tmp/whip-ux-reference/packages/app/src/settings/agents.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/agents.tsx) |
| `settings/appearance.tsx` | Changed | Settings; UX-17–21/25/39, P10/P11 | [reference](/private/tmp/whip-ux-reference/packages/app/src/settings/appearance.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/appearance.tsx) |
| `settings/browser.tsx` | Unchanged | Settings; UX-17–21/25/39, P10/P11 | [reference](/private/tmp/whip-ux-reference/packages/app/src/settings/browser.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/browser.tsx) |
| `settings/configuration.tsx` | Changed | Settings; UX-17–21/25/39, P10/P11 | [reference](/private/tmp/whip-ux-reference/packages/app/src/settings/configuration.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/configuration.tsx) |
| `settings/connections.tsx` | Unchanged | Settings; UX-17–21/25/39, P10/P11 | [reference](/private/tmp/whip-ux-reference/packages/app/src/settings/connections.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/connections.tsx) |
| `settings/custom-themes.tsx` | Changed | Settings; UX-17–21/25/39, P10/P11 | [reference](/private/tmp/whip-ux-reference/packages/app/src/settings/custom-themes.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/custom-themes.tsx) |
| `settings/external-browser.tsx` | Added | Settings; UX-17–21/25/39, P10/P11 | [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/external-browser.tsx) |
| `settings/general.tsx` | Changed | Settings; UX-17–21/25/39, P10/P11 | [reference](/private/tmp/whip-ux-reference/packages/app/src/settings/general.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/general.tsx) |
| `settings/mcp-import.tsx` | Changed | Settings; UX-17–21/25/39, P10/P11 | [reference](/private/tmp/whip-ux-reference/packages/app/src/settings/mcp-import.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/mcp-import.tsx) |
| `settings/navigation.ts` | Changed | Settings; UX-17–21/25/39, P10/P11 | [reference](/private/tmp/whip-ux-reference/packages/app/src/settings/navigation.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/navigation.ts) |
| `settings/provider-connections.tsx` | Changed | Settings; UX-17–21/25/39, P10/P11 | [reference](/private/tmp/whip-ux-reference/packages/app/src/settings/provider-connections.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/provider-connections.tsx) |
| `settings/provider-defaults.tsx` | Added | Settings; UX-17–21/25/39, P10/P11 | [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/provider-defaults.tsx) |
| `settings/provider-login.stylex.ts` | Changed | Settings; UX-17–21/25/39, P10/P11 | [reference](/private/tmp/whip-ux-reference/packages/app/src/settings/provider-login.stylex.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/provider-login.stylex.ts) |
| `settings/provider-login.tsx` | Changed | Settings; UX-17–21/25/39, P10/P11 | [reference](/private/tmp/whip-ux-reference/packages/app/src/settings/provider-login.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/provider-login.tsx) |
| `settings/providers.tsx` | Changed | Settings; UX-17–21/25/39, P10/P11 | [reference](/private/tmp/whip-ux-reference/packages/app/src/settings/providers.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/providers.tsx) |
| `settings/recovery.tsx` | Added | Settings; UX-17–21/25/39, P10/P11 | [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/recovery.tsx) |
| `settings/section-layout.tsx` | Unchanged | Settings; UX-17–21/25/39, P10/P11 | [reference](/private/tmp/whip-ux-reference/packages/app/src/settings/section-layout.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/section-layout.tsx) |
| `settings/sections.tsx` | Unchanged | Settings; UX-17–21/25/39, P10/P11 | [reference](/private/tmp/whip-ux-reference/packages/app/src/settings/sections.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/sections.tsx) |
| `settings/unsaved.tsx` | Unchanged | Settings; UX-17–21/25/39, P10/P11 | [reference](/private/tmp/whip-ux-reference/packages/app/src/settings/unsaved.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/settings/unsaved.tsx) |
| `shell.tsx` | Changed | Shared shell/presentation; P01–P05/P11/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/shell.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/shell.tsx) |
| `sidebar-layout.tsx` | Unchanged | Workspace/tabs; UX-06/43, P02/P03 | [reference](/private/tmp/whip-ux-reference/packages/app/src/sidebar-layout.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/sidebar-layout.tsx) |
| `sidebar-state.ts` | Changed | Catalog/actions; UX-09/24/30–31, P03 | [reference](/private/tmp/whip-ux-reference/packages/app/src/sidebar-state.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/sidebar-state.ts) |
| `skill-completion.ts` | Changed | Draft/send/queue; UX-08/10/28–29, P04 | [reference](/private/tmp/whip-ux-reference/packages/app/src/skill-completion.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/skill-completion.ts) |
| `skill-suggestions.ts` | Added | Draft/send/queue; UX-08/10/28–29, P04 | [native](/private/tmp/whip-native-final-acceptance/packages/app/src/skill-suggestions.ts) |
| `ssh-profile-picker.stylex.ts` | Unchanged | Shared shell/presentation; P01–P05/P11/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/ssh-profile-picker.stylex.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/ssh-profile-picker.stylex.ts) |
| `startup-screen.tsx` | Unchanged | Shared shell/presentation; P01–P05/P11/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/startup-screen.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/startup-screen.tsx) |
| `streaming-markdown.tsx` | Changed | Reading/status; UX-08/10–16/24/38, P03/P05 | [reference](/private/tmp/whip-ux-reference/packages/app/src/streaming-markdown.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/streaming-markdown.tsx) |
| `styles.ts` | Unchanged | Shared shell/presentation; P01–P05/P11/P12 | [reference](/private/tmp/whip-ux-reference/packages/app/src/styles.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/styles.ts) |
| `terminal-open.ts` | Added | Terminal; UX-05–07/43, P09 | [native](/private/tmp/whip-native-final-acceptance/packages/app/src/terminal-open.ts) |
| `terminal-output.ts` | Added | Terminal; UX-05–07/43, P09 | [native](/private/tmp/whip-native-final-acceptance/packages/app/src/terminal-output.ts) |
| `terminal-view.tsx` | Changed | Terminal; UX-05–07/43, P09 | [reference](/private/tmp/whip-ux-reference/packages/app/src/terminal-view.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/terminal-view.tsx) |
| `theme-presentation.ts` | Changed | Appearance; P01/P11 | [reference](/private/tmp/whip-ux-reference/packages/app/src/theme-presentation.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/theme-presentation.ts) |
| `timeline.stylex.ts` | Unchanged | Reading/status; UX-08/10–16/24/38, P03/P05 | [reference](/private/tmp/whip-ux-reference/packages/app/src/timeline.stylex.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/timeline.stylex.ts) |
| `timeline.tsx` | Changed | Reading/status; UX-08/10–16/24/38, P03/P05 | [reference](/private/tmp/whip-ux-reference/packages/app/src/timeline.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/timeline.tsx) |
| `trace-math.ts` | Changed | Evidence views; UX-12–13/15/41, P07 | [reference](/private/tmp/whip-ux-reference/packages/app/src/trace-math.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/trace-math.ts) |
| `trace-resize-handle.tsx` | Unchanged | Evidence views; UX-12–13/15/41, P07 | [reference](/private/tmp/whip-ux-reference/packages/app/src/trace-resize-handle.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/trace-resize-handle.tsx) |
| `trace-view.stylex.ts` | Unchanged | Evidence views; UX-12–13/15/41, P07 | [reference](/private/tmp/whip-ux-reference/packages/app/src/trace-view.stylex.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/trace-view.stylex.ts) |
| `trace-view.tsx` | Changed | Evidence views; UX-12–13/15/41, P07 | [reference](/private/tmp/whip-ux-reference/packages/app/src/trace-view.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/trace-view.tsx) |
| `transcript-activity.tsx` | Changed | Reading/status; UX-08/10–16/24/38, P03/P05 | [reference](/private/tmp/whip-ux-reference/packages/app/src/transcript-activity.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/transcript-activity.tsx) |
| `transcript-motion.tsx` | Unchanged | Reading/status; UX-08/10–16/24/38, P03/P05 | [reference](/private/tmp/whip-ux-reference/packages/app/src/transcript-motion.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/transcript-motion.tsx) |
| `usage-presentation.ts` | Added | Shared shell/presentation; P01–P05/P11/P12 | [native](/private/tmp/whip-native-final-acceptance/packages/app/src/usage-presentation.ts) |
| `use-skill-completion.tsx` | Changed | Draft/send/queue; UX-08/10/28–29, P04 | [reference](/private/tmp/whip-ux-reference/packages/app/src/use-skill-completion.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/use-skill-completion.tsx) |
| `welcome-host-picker.tsx` | Unchanged | Entry/setup; UX-01–04/07/17/19/21/27, P04/P10 | [reference](/private/tmp/whip-ux-reference/packages/app/src/welcome-host-picker.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/welcome-host-picker.tsx) |
| `welcome.tsx` | Changed | Entry/setup; UX-01–04/07/17/19/21/27, P04/P10 | [reference](/private/tmp/whip-ux-reference/packages/app/src/welcome.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/welcome.tsx) |
| `workspace-views.ts` | Changed | Workspace/tabs; UX-06/43, P02/P03 | [reference](/private/tmp/whip-ux-reference/packages/app/src/workspace-views.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/src/workspace-views.ts) |

### packages/ui/src

| File | Byte comparison | Workflow / gate | Sources |
| --- | --- | --- | --- |
| `actions.tsx` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/actions.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/actions.tsx) |
| `activity-indicator.tsx` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/activity-indicator.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/activity-indicator.tsx) |
| `appearance-data.ts` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/appearance-data.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/appearance-data.ts) |
| `clipboard.ts` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/clipboard.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/clipboard.ts) |
| `code-block.tsx` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/code-block.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/code-block.tsx) |
| `code-data.ts` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/code-data.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/code-data.ts) |
| `code-highlight.ts` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/code-highlight.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/code-highlight.ts) |
| `fonts.css` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/fonts.css) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/fonts.css) |
| `forms.tsx` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/forms.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/forms.tsx) |
| `generated/theme-catalog.ts` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/generated/theme-catalog.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/generated/theme-catalog.ts) |
| `generated/themes.stylex.ts` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/generated/themes.stylex.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/generated/themes.stylex.ts) |
| `index.ts` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/index.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/index.ts) |
| `mermaid-block.tsx` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/mermaid-block.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/mermaid-block.tsx) |
| `mermaid-data.ts` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/mermaid-data.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/mermaid-data.ts) |
| `mermaid-fonts.d.ts` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/mermaid-fonts.d.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/mermaid-fonts.d.ts) |
| `mermaid-renderer.ts` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/mermaid-renderer.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/mermaid-renderer.ts) |
| `mermaid-worker-bootstrap.ts` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/mermaid-worker-bootstrap.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/mermaid-worker-bootstrap.ts) |
| `mermaid-worker.ts` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/mermaid-worker.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/mermaid-worker.ts) |
| `native-surfaces.tsx` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/native-surfaces.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/native-surfaces.tsx) |
| `overlays.tsx` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/overlays.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/overlays.tsx) |
| `presentation.tsx` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/presentation.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/presentation.tsx) |
| `reset.css` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/reset.css) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/reset.css) |
| `slider.tsx` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/slider.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/slider.tsx) |
| `styles.stylex.ts` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/styles.stylex.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/styles.stylex.ts) |
| `textarea-suggestions.tsx` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/textarea-suggestions.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/textarea-suggestions.tsx) |
| `theme-contrast.ts` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/theme-contrast.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/theme-contrast.ts) |
| `theme-data.ts` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/theme-data.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/theme-data.ts) |
| `themes.tsx` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/themes.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/themes.tsx) |
| `tokens.stylex.ts` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/tokens.stylex.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/tokens.stylex.ts) |
| `workspace-drag.tsx` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/workspace-drag.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/workspace-drag.tsx) |
| `workspace-layout.stylex.ts` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/workspace-layout.stylex.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/workspace-layout.stylex.ts) |
| `workspace-layout.tsx` | Changed | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/workspace-layout.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/workspace-layout.tsx) |
| `workspace-tab-drag.tsx` | Changed | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/workspace-tab-drag.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/workspace-tab-drag.tsx) |
| `workspace-tabs.stylex.ts` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/workspace-tabs.stylex.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/workspace-tabs.stylex.ts) |
| `workspace-tabs.tsx` | Unchanged | Shared UI; P01–03/P05/P08 | [reference](/private/tmp/whip-ux-reference/packages/ui/src/workspace-tabs.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/src/workspace-tabs.tsx) |

### apps/web/src

| File | Byte comparison | Workflow / gate | Sources |
| --- | --- | --- | --- |
| `bootstrap.tsx` | Changed | Web bootstrap/platform; P12 | [reference](/private/tmp/whip-ux-reference/apps/web/src/bootstrap.tsx) · [native](/private/tmp/whip-native-final-acceptance/apps/web/src/bootstrap.tsx) |
| `design.tsx` | Unchanged | Web bootstrap/platform; P12 | [reference](/private/tmp/whip-ux-reference/apps/web/src/design.tsx) · [native](/private/tmp/whip-native-final-acceptance/apps/web/src/design.tsx) |
| `main.tsx` | Unchanged | Web bootstrap/platform; P12 | [reference](/private/tmp/whip-ux-reference/apps/web/src/main.tsx) · [native](/private/tmp/whip-native-final-acceptance/apps/web/src/main.tsx) |
| `platform/browser.ts` | Unchanged | Web bootstrap/platform; P12 | [reference](/private/tmp/whip-ux-reference/apps/web/src/platform/browser.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/web/src/platform/browser.ts) |
| `platform/desktop.ts` | Changed | Web bootstrap/platform; P12 | [reference](/private/tmp/whip-ux-reference/apps/web/src/platform/desktop.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/web/src/platform/desktop.ts) |
| `platform/storage.ts` | Unchanged | Web bootstrap/platform; P12 | [reference](/private/tmp/whip-ux-reference/apps/web/src/platform/storage.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/web/src/platform/storage.ts) |

### apps/desktop/src

| File | Byte comparison | Workflow / gate | Sources |
| --- | --- | --- | --- |
| `assets.ts` | Unchanged | Desktop shell/platform; P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/assets.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/assets.ts) |
| `browser-cdp.ts` | Unchanged | Native Browser; UX-25/42, P08/P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/browser-cdp.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/browser-cdp.ts) |
| `browser-control.ts` | Changed | Native Browser; UX-25/42, P08/P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/browser-control.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/browser-control.ts) |
| `browser-design-ipc.ts` | Unchanged | Native Browser; UX-25/42, P08/P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/browser-design-ipc.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/browser-design-ipc.ts) |
| `browser-design-page.ts` | Unchanged | Native Browser; UX-25/42, P08/P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/browser-design-page.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/browser-design-page.ts) |
| `browser-design-policy.ts` | Unchanged | Native Browser; UX-25/42, P08/P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/browser-design-policy.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/browser-design-policy.ts) |
| `browser-design-preload.ts` | Unchanged | Native Browser; UX-25/42, P08/P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/browser-design-preload.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/browser-design-preload.ts) |
| `browser-design.ts` | Unchanged | Native Browser; UX-25/42, P08/P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/browser-design.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/browser-design.ts) |
| `browser-feature.ts` | Unchanged | Native Browser; UX-25/42, P08/P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/browser-feature.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/browser-feature.ts) |
| `browser-human-confirmation.ts` | Unchanged | Native Browser; UX-25/42, P08/P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/browser-human-confirmation.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/browser-human-confirmation.ts) |
| `browser-human-preview.ts` | Unchanged | Native Browser; UX-25/42, P08/P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/browser-human-preview.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/browser-human-preview.ts) |
| `browser-ipc.ts` | Changed | Native Browser; UX-25/42, P08/P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/browser-ipc.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/browser-ipc.ts) |
| `browser-manager.ts` | Unchanged | Native Browser; UX-25/42, P08/P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/browser-manager.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/browser-manager.ts) |
| `browser-policy.ts` | Unchanged | Native Browser; UX-25/42, P08/P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/browser-policy.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/browser-policy.ts) |
| `browser-preload.ts` | Changed | Native Browser; UX-25/42, P08/P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/browser-preload.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/browser-preload.ts) |
| `browser-preview-authority.ts` | Unchanged | Native Browser; UX-25/42, P08/P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/browser-preview-authority.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/browser-preview-authority.ts) |
| `browser-preview.ts` | Unchanged | Native Browser; UX-25/42, P08/P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/browser-preview.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/browser-preview.ts) |
| `development.ts` | Unchanged | Desktop shell/platform; P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/development.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/development.ts) |
| `links.ts` | Unchanged | Desktop shell/platform; P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/links.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/links.ts) |
| `main.ts` | Changed | Desktop shell/platform; P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/main.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/main.ts) |
| `native.ts` | Unchanged | Desktop shell/platform; P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/native.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/native.ts) |
| `preload.ts` | Changed | Desktop shell/platform; P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/preload.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/preload.ts) |
| `preview-budget.ts` | Unchanged | Native Browser; UX-25/42, P08/P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/preview-budget.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/preview-budget.ts) |
| `preview-environments.ts` | Unchanged | Native Browser; UX-25/42, P08/P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/preview-environments.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/preview-environments.ts) |
| `preview-proxy.ts` | Unchanged | Native Browser; UX-25/42, P08/P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/preview-proxy.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/preview-proxy.ts) |
| `preview-ssh-route.ts` | Unchanged | Native Browser; UX-25/42, P08/P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/preview-ssh-route.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/preview-ssh-route.ts) |
| `project-open.ts` | Changed | Desktop shell/platform; P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/project-open.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/project-open.ts) |
| `provider-environment.ts` | Unchanged | Desktop shell/platform; P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/provider-environment.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/provider-environment.ts) |
| `runtime.ts` | Changed | Native host/startup; UX-27/44, P09/P10/P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/runtime.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/runtime.ts) |
| `ssh-profiles.ts` | Unchanged | Native host/startup; UX-27/44, P09/P10/P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/ssh-profiles.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/ssh-profiles.ts) |
| `ssh.ts` | Changed | Native host/startup; UX-27/44, P09/P10/P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/ssh.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/ssh.ts) |
| `startup-probe.ts` | Unchanged | Native host/startup; UX-27/44, P09/P10/P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/startup-probe.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/startup-probe.ts) |
| `transport.ts` | Changed | Native host/startup; UX-27/44, P09/P10/P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/transport.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/transport.ts) |
| `unix-frames.ts` | Added | Native host/startup; UX-27/44, P09/P10/P12 | [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/unix-frames.ts) |
| `updates.ts` | Unchanged | Desktop shell/platform; P12 | [reference](/private/tmp/whip-ux-reference/apps/desktop/src/updates.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/src/updates.ts) |

## Existing test, runner and audit entry points

The list below is discovery coverage only. None was executed for this inventory. Existing runners may seed different defaults, adapt assertions to native behavior, depend on credentials, launch runtimes or contain historical assumptions. Helpers and packaging scripts are included to expose platform dependencies; a listed script is not necessarily a test. Review its setup before running.

Test files include `.test.`/`.spec.` and code under `test`, `tests` or `renderer-tests`; script entry points are direct children of the scoped `scripts` directories. Nested script fixtures are dependencies, not separately counted entry points. Audit Markdown files are included as recorded evidence, not proof of current parity.

### packages/app: 123 discovered entries

<details>
<summary>Expand source and evidence entry points</summary>

| Entry | Comparison | Sources |
| --- | --- | --- |
| `test/agent-dock.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/agent-dock.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/agent-dock.test.tsx) |
| `test/agent-turn-notice.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/agent-turn-notice.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/agent-turn-notice.test.tsx) |
| `test/architecture.test.ts` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/architecture.test.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/architecture.test.ts) |
| `test/attention-notifications.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/attention-notifications.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/attention-notifications.test.tsx) |
| `test/bootstrap.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/bootstrap.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/bootstrap.test.tsx) |
| `test/browser-design-attachment.test.tsx` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/app/test/browser-design-attachment.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/browser-design-attachment.test.tsx) |
| `test/browser-design-integration.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/browser-design-integration.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/browser-design-integration.test.tsx) |
| `test/browser-design-message.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/browser-design-message.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/browser-design-message.test.tsx) |
| `test/browser-design-overlay.test.tsx` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/app/test/browser-design-overlay.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/browser-design-overlay.test.tsx) |
| `test/browser-design-presentation.test.ts` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/browser-design-presentation.test.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/browser-design-presentation.test.ts) |
| `test/browser-design.test.ts` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/app/test/browser-design.test.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/browser-design.test.ts) |
| `test/browser-preview.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/browser-preview.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/browser-preview.test.tsx) |
| `test/browser-provider.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/browser-provider.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/browser-provider.test.tsx) |
| `test/browser-spike-contract.ts` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/app/test/browser-spike-contract.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/browser-spike-contract.ts) |
| `test/browser-spike-workspace.test.tsx` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/app/test/browser-spike-workspace.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/browser-spike-workspace.test.tsx) |
| `test/browser-workspace.test.tsx` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/app/test/browser-workspace.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/browser-workspace.test.tsx) |
| `test/canonical-conversation-rows.test.ts` | Added | [native](/private/tmp/whip-native-final-acceptance/packages/app/test/canonical-conversation-rows.test.ts) |
| `test/chat-activity.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/chat-activity.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/chat-activity.test.tsx) |
| `test/chat-file-drop.test.tsx` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/app/test/chat-file-drop.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/chat-file-drop.test.tsx) |
| `test/chat-submission.test.ts` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/chat-submission.test.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/chat-submission.test.ts) |
| `test/clipboard-provider.test.tsx` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/app/test/clipboard-provider.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/clipboard-provider.test.tsx) |
| `test/code-block-copy.test.tsx` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/app/test/code-block-copy.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/code-block-copy.test.tsx) |
| `test/completion-picker.test.tsx` | Added | [native](/private/tmp/whip-native-final-acceptance/packages/app/test/completion-picker.test.tsx) |
| `test/composer-attachments.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/composer-attachments.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/composer-attachments.test.tsx) |
| `test/composer-queue.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/composer-queue.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/composer-queue.test.tsx) |
| `test/composer.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/composer.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/composer.test.tsx) |
| `test/compositions.test.ts` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/compositions.test.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/compositions.test.ts) |
| `test/connection-notice.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/connection-notice.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/connection-notice.test.tsx) |
| `test/connections.test.ts` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/connections.test.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/connections.test.ts) |
| `test/conversation-agent-dock.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/conversation-agent-dock.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/conversation-agent-dock.test.tsx) |
| `test/conversation-history-errors.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/conversation-history-errors.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/conversation-history-errors.test.tsx) |
| `test/conversation-history.test.ts` | Added | [native](/private/tmp/whip-native-final-acceptance/packages/app/test/conversation-history.test.ts) |
| `test/custom-themes.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/custom-themes.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/custom-themes.test.tsx) |
| `test/desktop-adapter.test.ts` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/desktop-adapter.test.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/desktop-adapter.test.ts) |
| `test/desktop-close-tab.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/desktop-close-tab.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/desktop-close-tab.test.tsx) |
| `test/desktop-settings.test.tsx` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/app/test/desktop-settings.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/desktop-settings.test.tsx) |
| `test/directory-picker.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/directory-picker.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/directory-picker.test.tsx) |
| `test/directory-queries.test.ts` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/directory-queries.test.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/directory-queries.test.ts) |
| `test/empty-workspace.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/empty-workspace.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/empty-workspace.test.tsx) |
| `test/error-feedback.test.tsx` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/app/test/error-feedback.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/error-feedback.test.tsx) |
| `test/external-browser.test.tsx` | Added | [native](/private/tmp/whip-native-final-acceptance/packages/app/test/external-browser.test.tsx) |
| `test/host-connection-dialog.test.tsx` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/app/test/host-connection-dialog.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/host-connection-dialog.test.tsx) |
| `test/host-prompts.test.tsx` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/app/test/host-prompts.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/host-prompts.test.tsx) |
| `test/hosts.test.ts` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/hosts.test.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/hosts.test.ts) |
| `test/input-attachment.test.tsx` | Added | [native](/private/tmp/whip-native-final-acceptance/packages/app/test/input-attachment.test.tsx) |
| `test/input-identities.test.ts` | Added | [native](/private/tmp/whip-native-final-acceptance/packages/app/test/input-identities.test.ts) |
| `test/input-presentation.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/input-presentation.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/input-presentation.test.tsx) |
| `test/inspector-content.test.tsx` | Added | [native](/private/tmp/whip-native-final-acceptance/packages/app/test/inspector-content.test.tsx) |
| `test/inspector.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/inspector.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/inspector.test.tsx) |
| `test/local-errors.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/local-errors.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/local-errors.test.tsx) |
| `test/local-runtime.test.tsx` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/app/test/local-runtime.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/local-runtime.test.tsx) |
| `test/mcp-brand.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/mcp-brand.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/mcp-brand.test.tsx) |
| `test/mcp-import-fake.ts` | Removed | [reference](/private/tmp/whip-ux-reference/packages/app/test/mcp-import-fake.ts) |
| `test/mcp-import.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/mcp-import.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/mcp-import.test.tsx) |
| `test/mcp-v4-fixture.ts` | Added | [native](/private/tmp/whip-native-final-acceptance/packages/app/test/mcp-v4-fixture.ts) |
| `test/mermaid-markdown.test.tsx` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/app/test/mermaid-markdown.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/mermaid-markdown.test.tsx) |
| `test/message-attachments.test.tsx` | Added | [native](/private/tmp/whip-native-final-acceptance/packages/app/test/message-attachments.test.tsx) |
| `test/model-selection.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/model-selection.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/model-selection.test.tsx) |
| `test/multi-host-discovery.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/multi-host-discovery.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/multi-host-discovery.test.tsx) |
| `test/native-conversation-fixture.tsx` | Added | [native](/private/tmp/whip-native-final-acceptance/packages/app/test/native-conversation-fixture.tsx) |
| `test/native-presentation-fixtures.ts` | Added | [native](/private/tmp/whip-native-final-acceptance/packages/app/test/native-presentation-fixtures.ts) |
| `test/new-chat.test.ts` | Added | [native](/private/tmp/whip-native-final-acceptance/packages/app/test/new-chat.test.ts) |
| `test/ordered-presentation.test.ts` | Added | [native](/private/tmp/whip-native-final-acceptance/packages/app/test/ordered-presentation.test.ts) |
| `test/permission-mode.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/permission-mode.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/permission-mode.test.tsx) |
| `test/permission-scope.test.ts` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/app/test/permission-scope.test.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/permission-scope.test.ts) |
| `test/provider-connections.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/provider-connections.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/provider-connections.test.tsx) |
| `test/provider-defaults.test.tsx` | Added | [native](/private/tmp/whip-native-final-acceptance/packages/app/test/provider-defaults.test.tsx) |
| `test/provider-fixture.tsx` | Added | [native](/private/tmp/whip-native-final-acceptance/packages/app/test/provider-fixture.tsx) |
| `test/reading-positions.test.ts` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/reading-positions.test.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/reading-positions.test.ts) |
| `test/recent-projects.test.ts` | Added | [native](/private/tmp/whip-native-final-acceptance/packages/app/test/recent-projects.test.ts) |
| `test/recovery-storage.test.ts` | Added | [native](/private/tmp/whip-native-final-acceptance/packages/app/test/recovery-storage.test.ts) |
| `test/remote-directory-dialog.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/remote-directory-dialog.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/remote-directory-dialog.test.tsx) |
| `test/repl-view.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/repl-view.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/repl-view.test.tsx) |
| `test/replacement-runtime.test.tsx` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/app/test/replacement-runtime.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/replacement-runtime.test.tsx) |
| `test/requests.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/requests.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/requests.test.tsx) |
| `test/runtime.test.ts` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/runtime.test.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/runtime.test.ts) |
| `test/scheduled-wake-notice.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/scheduled-wake-notice.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/scheduled-wake-notice.test.tsx) |
| `test/server-manager.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/server-manager.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/server-manager.test.tsx) |
| `test/session-actions.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/session-actions.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/session-actions.test.tsx) |
| `test/session-navigator.test.ts` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/session-navigator.test.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/session-navigator.test.ts) |
| `test/session-opening.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/session-opening.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/session-opening.test.tsx) |
| `test/session-sidebar.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/session-sidebar.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/session-sidebar.test.tsx) |
| `test/session-tab-routing.test.ts` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/session-tab-routing.test.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/session-tab-routing.test.ts) |
| `test/session-tab-titles.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/session-tab-titles.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/session-tab-titles.test.tsx) |
| `test/session-tabs.test.ts` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/session-tabs.test.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/session-tabs.test.ts) |
| `test/session-title-notifications.test.ts` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/session-title-notifications.test.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/session-title-notifications.test.ts) |
| `test/session-top-bar.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/session-top-bar.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/session-top-bar.test.tsx) |
| `test/settings-agents.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/settings-agents.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/settings-agents.test.tsx) |
| `test/settings-configuration.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/settings-configuration.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/settings-configuration.test.tsx) |
| `test/settings-density.test.tsx` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/app/test/settings-density.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/settings-density.test.tsx) |
| `test/settings-host-selection.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/settings-host-selection.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/settings-host-selection.test.tsx) |
| `test/settings-mcp-import.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/settings-mcp-import.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/settings-mcp-import.test.tsx) |
| `test/settings-navigation.test.ts` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/app/test/settings-navigation.test.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/settings-navigation.test.ts) |
| `test/settings-recovery.test.tsx` | Added | [native](/private/tmp/whip-native-final-acceptance/packages/app/test/settings-recovery.test.tsx) |
| `test/settings-unsaved.test.tsx` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/app/test/settings-unsaved.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/settings-unsaved.test.tsx) |
| `test/setup.ts` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/app/test/setup.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/setup.ts) |
| `test/sidebar-creation.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/sidebar-creation.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/sidebar-creation.test.tsx) |
| `test/sidebar-layout.test.tsx` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/app/test/sidebar-layout.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/sidebar-layout.test.tsx) |
| `test/sidebar-native.test.tsx` | Added | [native](/private/tmp/whip-native-final-acceptance/packages/app/test/sidebar-native.test.tsx) |
| `test/sidebar-rendering.test.tsx` | Added | [native](/private/tmp/whip-native-final-acceptance/packages/app/test/sidebar-rendering.test.tsx) |
| `test/sidebar-state.test.ts` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/sidebar-state.test.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/sidebar-state.test.ts) |
| `test/skill-catalog.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/skill-catalog.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/skill-catalog.test.tsx) |
| `test/skill-completion.test.ts` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/skill-completion.test.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/skill-completion.test.ts) |
| `test/skill-suggestions.test.ts` | Added | [native](/private/tmp/whip-native-final-acceptance/packages/app/test/skill-suggestions.test.ts) |
| `test/ssh-profile-picker.test.tsx` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/app/test/ssh-profile-picker.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/ssh-profile-picker.test.tsx) |
| `test/standing-grant.test.tsx` | Added | [native](/private/tmp/whip-native-final-acceptance/packages/app/test/standing-grant.test.tsx) |
| `test/startup-screen.test.tsx` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/app/test/startup-screen.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/startup-screen.test.tsx) |
| `test/stored-messages.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/stored-messages.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/stored-messages.test.tsx) |
| `test/streaming-transcript.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/streaming-transcript.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/streaming-transcript.test.tsx) |
| `test/terminal-open.test.ts` | Added | [native](/private/tmp/whip-native-final-acceptance/packages/app/test/terminal-open.test.ts) |
| `test/terminal-output.test.ts` | Added | [native](/private/tmp/whip-native-final-acceptance/packages/app/test/terminal-output.test.ts) |
| `test/terminal-view.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/terminal-view.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/terminal-view.test.tsx) |
| `test/timeline-reading.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/timeline-reading.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/timeline-reading.test.tsx) |
| `test/timeline.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/timeline.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/timeline.test.tsx) |
| `test/trace-math.test.ts` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/trace-math.test.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/trace-math.test.ts) |
| `test/trace-view.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/trace-view.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/trace-view.test.tsx) |
| `test/usage-presentation.test.ts` | Added | [native](/private/tmp/whip-native-final-acceptance/packages/app/test/usage-presentation.test.ts) |
| `test/welcome-fixture.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/welcome-fixture.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/welcome-fixture.tsx) |
| `test/welcome-permission-defaults.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/welcome-permission-defaults.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/welcome-permission-defaults.test.tsx) |
| `test/welcome-skill-completion.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/welcome-skill-completion.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/welcome-skill-completion.test.tsx) |
| `test/welcome.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/welcome.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/welcome.test.tsx) |
| `test/workflow-inventory.test.ts` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/workflow-inventory.test.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/workflow-inventory.test.ts) |
| `test/workspace-views.test.ts` | Changed | [reference](/private/tmp/whip-ux-reference/packages/app/test/workspace-views.test.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/app/test/workspace-views.test.ts) |

</details>

### packages/ui: 23 discovered entries

<details>
<summary>Expand source and evidence entry points</summary>

| Entry | Comparison | Sources |
| --- | --- | --- |
| `tests/appearance.test.ts` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/ui/tests/appearance.test.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/tests/appearance.test.ts) |
| `tests/browser.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/packages/ui/tests/browser.mjs) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/tests/browser.mjs) |
| `tests/code-benchmark.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/ui/tests/code-benchmark.mjs) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/tests/code-benchmark.mjs) |
| `tests/code.test.ts` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/ui/tests/code.test.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/tests/code.test.ts) |
| `tests/csp.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/ui/tests/csp.mjs) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/tests/csp.mjs) |
| `tests/fixtures/csp/browser-smoke.ts` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/ui/tests/fixtures/csp/browser-smoke.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/tests/fixtures/csp/browser-smoke.ts) |
| `tests/fixtures/csp/main.tsx` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/ui/tests/fixtures/csp/main.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/tests/fixtures/csp/main.tsx) |
| `tests/fixtures/mermaid/main.tsx` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/ui/tests/fixtures/mermaid/main.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/tests/fixtures/mermaid/main.tsx) |
| `tests/fixtures/workspace-layout/external.tsx` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/ui/tests/fixtures/workspace-layout/external.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/tests/fixtures/workspace-layout/external.tsx) |
| `tests/fixtures/workspace-layout/main.tsx` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/ui/tests/fixtures/workspace-layout/main.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/tests/fixtures/workspace-layout/main.tsx) |
| `tests/fixtures/workspace-tabs/main.tsx` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/ui/tests/fixtures/workspace-tabs/main.tsx) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/tests/fixtures/workspace-tabs/main.tsx) |
| `tests/mermaid-data.test.ts` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/ui/tests/mermaid-data.test.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/tests/mermaid-data.test.ts) |
| `tests/mermaid-palette.test.ts` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/ui/tests/mermaid-palette.test.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/tests/mermaid-palette.test.ts) |
| `tests/mermaid-renderer.test.ts` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/ui/tests/mermaid-renderer.test.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/tests/mermaid-renderer.test.ts) |
| `tests/mermaid.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/ui/tests/mermaid.mjs) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/tests/mermaid.mjs) |
| `tests/packed.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/ui/tests/packed.mjs) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/tests/packed.mjs) |
| `tests/textarea-suggestions.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/ui/tests/textarea-suggestions.mjs) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/tests/textarea-suggestions.mjs) |
| `tests/themes.test.ts` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/ui/tests/themes.test.ts) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/tests/themes.test.ts) |
| `tests/visual.config.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/ui/tests/visual.config.mjs) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/tests/visual.config.mjs) |
| `tests/visual.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/ui/tests/visual.mjs) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/tests/visual.mjs) |
| `tests/visual.spec.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/packages/ui/tests/visual.spec.mjs) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/tests/visual.spec.mjs) |
| `tests/workspace-layout.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/packages/ui/tests/workspace-layout.mjs) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/tests/workspace-layout.mjs) |
| `tests/workspace-tabs.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/packages/ui/tests/workspace-tabs.mjs) · [native](/private/tmp/whip-native-final-acceptance/packages/ui/tests/workspace-tabs.mjs) |

</details>

### apps/web: 129 discovered entries

<details>
<summary>Expand source and evidence entry points</summary>

| Entry | Comparison | Sources |
| --- | --- | --- |
| `scripts/agent-dock.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/agent-dock.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/agent-dock.mjs) |
| `scripts/agents.mjs` | Removed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/agents.mjs) |
| `scripts/attachment-confirmation.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/attachment-confirmation.mjs) |
| `scripts/browser-design.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/browser-design.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/browser-design.mjs) |
| `scripts/browser-toolbar.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/browser-toolbar.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/browser-toolbar.mjs) |
| `scripts/browser.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/browser.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/browser.mjs) |
| `scripts/chat-activity.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/chat-activity.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/chat-activity.mjs) |
| `scripts/chat-file-drop.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/chat-file-drop.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/chat-file-drop.mjs) |
| `scripts/chat-polish.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/chat-polish.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/chat-polish.mjs) |
| `scripts/claude-code-theme.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/claude-code-theme.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/claude-code-theme.mjs) |
| `scripts/composer-attachments.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/composer-attachments.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/composer-attachments.mjs) |
| `scripts/composer-panels.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/composer-panels.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/composer-panels.mjs) |
| `scripts/composer-queue.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/composer-queue.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/composer-queue.mjs) |
| `scripts/composer-reading.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/composer-reading.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/composer-reading.mjs) |
| `scripts/dev-proxy.test.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/dev-proxy.test.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/dev-proxy.test.mjs) |
| `scripts/directory-sidebar.mjs` | Removed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/directory-sidebar.mjs) |
| `scripts/error-ownership.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/error-ownership.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/error-ownership.mjs) |
| `scripts/external-browser-controls.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/external-browser-controls.mjs) |
| `scripts/frontend-migration-parity.md` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/frontend-migration-parity.md) |
| `scripts/frontend-migration-parity.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/frontend-migration-parity.mjs) |
| `scripts/history-gap.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/history-gap.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/history-gap.mjs) |
| `scripts/history-prefetch.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/history-prefetch.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/history-prefetch.mjs) |
| `scripts/mermaid-diagrams.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/mermaid-diagrams.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/mermaid-diagrams.mjs) |
| `scripts/model-budgets.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/model-budgets.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/model-budgets.mjs) |
| `scripts/model-picker.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/model-picker.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/model-picker.mjs) |
| `scripts/multiple-hosts.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/multiple-hosts.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/multiple-hosts.mjs) |
| `scripts/native-activity-reading.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-activity-reading.mjs) |
| `scripts/native-activity-response.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-activity-response.mjs) |
| `scripts/native-activity-response.test.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-activity-response.test.mjs) |
| `scripts/native-activity-scroll-trace.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-activity-scroll-trace.mjs) |
| `scripts/native-activity-scroll-trace.test.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-activity-scroll-trace.test.mjs) |
| `scripts/native-agent-dock.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-agent-dock.mjs) |
| `scripts/native-budget-audit.md` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-budget-audit.md) |
| `scripts/native-chat-activity-audit.md` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-chat-activity-audit.md) |
| `scripts/native-chat-activity.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-chat-activity.mjs) |
| `scripts/native-chat-polish-audit.md` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-chat-polish-audit.md) |
| `scripts/native-chat-polish-response.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-chat-polish-response.mjs) |
| `scripts/native-chat-polish.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-chat-polish.mjs) |
| `scripts/native-content-audit.md` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-content-audit.md) |
| `scripts/native-content-probes.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-content-probes.mjs) |
| `scripts/native-content-transport.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-content-transport.mjs) |
| `scripts/native-desktop-remote.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-desktop-remote.mjs) |
| `scripts/native-draft-title-audit.md` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-draft-title-audit.md) |
| `scripts/native-finder-drop.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-finder-drop.mjs) |
| `scripts/native-fixture.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-fixture.mjs) |
| `scripts/native-fixture.test.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-fixture.test.mjs) |
| `scripts/native-history-fixture.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-history-fixture.mjs) |
| `scripts/native-history-fixture.test.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-history-fixture.test.mjs) |
| `scripts/native-history-recovery.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-history-recovery.mjs) |
| `scripts/native-history-seed-audit.md` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-history-seed-audit.md) |
| `scripts/native-history-window.test.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-history-window.test.mjs) |
| `scripts/native-image-fixture.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-image-fixture.mjs) |
| `scripts/native-image-fixture.test.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-image-fixture.test.mjs) |
| `scripts/native-mermaid-audit.md` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-mermaid-audit.md) |
| `scripts/native-multiple-host-audit.md` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-multiple-host-audit.md) |
| `scripts/native-observation-probe.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-observation-probe.mjs) |
| `scripts/native-observation-probe.test.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-observation-probe.test.mjs) |
| `scripts/native-performance-control-audit.md` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-performance-control-audit.md) |
| `scripts/native-performance-fixture.test.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-performance-fixture.test.mjs) |
| `scripts/native-permission-audit.md` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-permission-audit.md) |
| `scripts/native-projects-polish.md` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-projects-polish.md) |
| `scripts/native-projects-polish.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-projects-polish.mjs) |
| `scripts/native-projects-zoom.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-projects-zoom.mjs) |
| `scripts/native-queue-audit.md` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-queue-audit.md) |
| `scripts/native-queue-fixture.test.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-queue-fixture.test.mjs) |
| `scripts/native-queue-response.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-queue-response.mjs) |
| `scripts/native-repl-audit.md` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-repl-audit.md) |
| `scripts/native-repl-fixture.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-repl-fixture.mjs) |
| `scripts/native-repl-response.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-repl-response.mjs) |
| `scripts/native-repl-response.test.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-repl-response.test.mjs) |
| `scripts/native-repl-viewer.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-repl-viewer.mjs) |
| `scripts/native-response-history.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-response-history.mjs) |
| `scripts/native-safari-audit.md` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-safari-audit.md) |
| `scripts/native-safari-driver.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-safari-driver.mjs) |
| `scripts/native-safari-driver.test.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-safari-driver.test.mjs) |
| `scripts/native-safari-rehearsal.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-safari-rehearsal.mjs) |
| `scripts/native-safari-workflows.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-safari-workflows.mjs) |
| `scripts/native-safari.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-safari.mjs) |
| `scripts/native-session-action-audit.md` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-session-action-audit.md) |
| `scripts/native-skills-fixture.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-skills-fixture.mjs) |
| `scripts/native-skills-transport.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-skills-transport.mjs) |
| `scripts/native-skills-transport.test.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-skills-transport.test.mjs) |
| `scripts/native-slash-skills-audit.md` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-slash-skills-audit.md) |
| `scripts/native-slash-skills.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-slash-skills.mjs) |
| `scripts/native-stream-fixture.test.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-stream-fixture.test.mjs) |
| `scripts/native-terminal-audit.md` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-terminal-audit.md) |
| `scripts/native-terminal-profile-fixture.test.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-terminal-profile-fixture.test.mjs) |
| `scripts/native-theme-audit.md` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-theme-audit.md) |
| `scripts/native-tool-output.test.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-tool-output.test.mjs) |
| `scripts/native-turn-failure-audit.md` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-turn-failure-audit.md) |
| `scripts/native-ux-executions.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/native-ux-executions.mjs) |
| `scripts/new-chat-tabs.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/new-chat-tabs.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/new-chat-tabs.mjs) |
| `scripts/performance-anchors.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/performance-anchors.mjs) |
| `scripts/performance-anchors.test.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/performance-anchors.test.mjs) |
| `scripts/performance-desktop.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/performance-desktop.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/performance-desktop.mjs) |
| `scripts/performance-desktop.test.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/performance-desktop.test.mjs) |
| `scripts/performance-input-control.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/performance-input-control.mjs) |
| `scripts/performance-keyboard.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/performance-keyboard.mjs) |
| `scripts/performance-keyboard.test.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/performance-keyboard.test.mjs) |
| `scripts/performance-retention.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/performance-retention.mjs) |
| `scripts/performance-retention.test.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/performance-retention.test.mjs) |
| `scripts/performance-trace.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/performance-trace.mjs) |
| `scripts/performance-trace.test.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/performance-trace.test.mjs) |
| `scripts/performance.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/performance.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/performance.mjs) |
| `scripts/permission-recovery-restoration.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/permission-recovery-restoration.mjs) |
| `scripts/permission-requests.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/permission-requests.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/permission-requests.mjs) |
| `scripts/provider-connections.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/provider-connections.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/provider-connections.mjs) |
| `scripts/provider-settings-reference.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/provider-settings-reference.mjs) |
| `scripts/provider-settings-restoration.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/provider-settings-restoration.mjs) |
| `scripts/reading-position.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/reading-position.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/reading-position.mjs) |
| `scripts/repl-viewer.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/repl-viewer.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/repl-viewer.mjs) |
| `scripts/safari.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/safari.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/safari.mjs) |
| `scripts/session-actions.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/session-actions.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/session-actions.mjs) |
| `scripts/session-search.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/session-search.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/session-search.mjs) |
| `scripts/session-tabs.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/session-tabs.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/session-tabs.mjs) |
| `scripts/session-title-notifications.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/session-title-notifications.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/session-title-notifications.mjs) |
| `scripts/settings-conversation.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/settings-conversation.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/settings-conversation.mjs) |
| `scripts/settings.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/settings.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/settings.mjs) |
| `scripts/sidebar.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/sidebar.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/sidebar.mjs) |
| `scripts/slash-skills.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/slash-skills.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/slash-skills.mjs) |
| `scripts/snapshot-refresh.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/snapshot-refresh.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/snapshot-refresh.mjs) |
| `scripts/ssh-profiles.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/ssh-profiles.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/ssh-profiles.mjs) |
| `scripts/standing-grants.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/standing-grants.mjs) |
| `scripts/stored-messages.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/stored-messages.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/stored-messages.mjs) |
| `scripts/stylex-dev.test.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/stylex-dev.test.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/stylex-dev.test.mjs) |
| `scripts/terminal-tabs.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/terminal-tabs.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/terminal-tabs.mjs) |
| `scripts/turn-failures.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/turn-failures.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/turn-failures.mjs) |
| `scripts/user-messages.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/user-messages.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/user-messages.mjs) |
| `scripts/workspace-layout.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/web/scripts/workspace-layout.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/web/scripts/workspace-layout.mjs) |

</details>

### apps/desktop: 77 discovered entries

<details>
<summary>Expand source and evidence entry points</summary>

| Entry | Comparison | Sources |
| --- | --- | --- |
| `renderer-tests/startup-frontdoor.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/apps/desktop/renderer-tests/startup-frontdoor.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/renderer-tests/startup-frontdoor.test.tsx) |
| `renderer-tests/startup-home.test.tsx` | Changed | [reference](/private/tmp/whip-ux-reference/apps/desktop/renderer-tests/startup-home.test.tsx) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/renderer-tests/startup-home.test.tsx) |
| `scripts/browser-control-native.ts` | Changed | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/browser-control-native.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/browser-control-native.ts) |
| `scripts/browser-control-regressions.ts` | Changed | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/browser-control-regressions.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/browser-control-regressions.ts) |
| `scripts/browser-design-native-main.ts` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/browser-design-native-main.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/browser-design-native-main.ts) |
| `scripts/browser-design-native-preload.ts` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/browser-design-native-preload.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/browser-design-native-preload.ts) |
| `scripts/browser-design-native.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/browser-design-native.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/browser-design-native.mjs) |
| `scripts/browser-design-production-main.ts` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/browser-design-production-main.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/browser-design-production-main.ts) |
| `scripts/browser-design-production.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/browser-design-production.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/browser-design-production.mjs) |
| `scripts/browser-discovery-native-renderer.ts` | Changed | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/browser-discovery-native-renderer.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/browser-discovery-native-renderer.ts) |
| `scripts/browser-discovery-native.ts` | Changed | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/browser-discovery-native.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/browser-discovery-native.ts) |
| `scripts/browser-feature-native-preload.ts` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/browser-feature-native-preload.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/browser-feature-native-preload.ts) |
| `scripts/browser-native-diagnostics.ts` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/browser-native-diagnostics.ts) |
| `scripts/browser-native-main.ts` | Changed | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/browser-native-main.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/browser-native-main.ts) |
| `scripts/browser-native.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/browser-native.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/browser-native.mjs) |
| `scripts/browser-packaged.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/browser-packaged.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/browser-packaged.mjs) |
| `scripts/browser-preview-native-main.ts` | Changed | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/browser-preview-native-main.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/browser-preview-native-main.ts) |
| `scripts/browser-preview-native.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/browser-preview-native.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/browser-preview-native.mjs) |
| `scripts/browser-sdk-native-renderer.ts` | Changed | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/browser-sdk-native-renderer.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/browser-sdk-native-renderer.ts) |
| `scripts/browser-spike-preload.cjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/browser-spike-preload.cjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/browser-spike-preload.cjs) |
| `scripts/browser-spike.cjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/browser-spike.cjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/browser-spike.cjs) |
| `scripts/browser-workspace-native-renderer.ts` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/browser-workspace-native-renderer.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/browser-workspace-native-renderer.ts) |
| `scripts/build.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/build.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/build.mjs) |
| `scripts/ci-signing.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/ci-signing.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/ci-signing.mjs) |
| `scripts/continuity.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/continuity.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/continuity.mjs) |
| `scripts/dev.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/dev.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/dev.mjs) |
| `scripts/distribution.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/distribution.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/distribution.mjs) |
| `scripts/icon.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/icon.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/icon.mjs) |
| `scripts/linux-smoke.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/linux-smoke.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/linux-smoke.mjs) |
| `scripts/native-fixture.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/native-fixture.mjs) |
| `scripts/notices.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/notices.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/notices.mjs) |
| `scripts/onboarding-fixture.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/onboarding-fixture.mjs) |
| `scripts/onboarding-fixture.test.mjs` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/onboarding-fixture.test.mjs) |
| `scripts/onboarding-smoke.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/onboarding-smoke.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/onboarding-smoke.mjs) |
| `scripts/package.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/package.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/package.mjs) |
| `scripts/preview-electron-entry.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/preview-electron-entry.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/preview-electron-entry.mjs) |
| `scripts/preview-electron.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/preview-electron.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/preview-electron.mjs) |
| `scripts/preview-fixture.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/preview-fixture.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/preview-fixture.mjs) |
| `scripts/preview-proxy.test.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/preview-proxy.test.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/preview-proxy.test.mjs) |
| `scripts/preview-remote-fixture.py` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/preview-remote-fixture.py) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/preview-remote-fixture.py) |
| `scripts/probe.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/probe.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/probe.mjs) |
| `scripts/project-editors-ipc-smoke.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/project-editors-ipc-smoke.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/project-editors-ipc-smoke.mjs) |
| `scripts/project-editors-smoke.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/project-editors-smoke.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/project-editors-smoke.mjs) |
| `scripts/publish-github.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/publish-github.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/publish-github.mjs) |
| `scripts/publish.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/publish.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/publish.mjs) |
| `scripts/release-candidate.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/release-candidate.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/release-candidate.mjs) |
| `scripts/runtime-acceptance.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/runtime-acceptance.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/runtime-acceptance.mjs) |
| `scripts/runtime-evidence.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/runtime-evidence.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/runtime-evidence.mjs) |
| `scripts/runtime-signing.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/runtime-signing.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/runtime-signing.mjs) |
| `scripts/settings-native.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/settings-native.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/settings-native.mjs) |
| `scripts/smoke.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/smoke.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/smoke.mjs) |
| `scripts/startup-diagnostics.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/startup-diagnostics.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/startup-diagnostics.mjs) |
| `scripts/startup-diagnostics.test.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/startup-diagnostics.test.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/startup-diagnostics.test.mjs) |
| `scripts/startup.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/startup.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/startup.mjs) |
| `scripts/terminal-smoke.mjs` | Changed | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/terminal-smoke.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/terminal-smoke.mjs) |
| `scripts/test.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/test.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/test.mjs) |
| `scripts/verify.mjs` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/scripts/verify.mjs) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/scripts/verify.mjs) |
| `tests/assets.test.ts` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/tests/assets.test.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/tests/assets.test.ts) |
| `tests/browser-control.test.ts` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/desktop/tests/browser-control.test.ts) |
| `tests/browser-design-policy.test.ts` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/tests/browser-design-policy.test.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/tests/browser-design-policy.test.ts) |
| `tests/browser-feature.test.ts` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/tests/browser-feature.test.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/tests/browser-feature.test.ts) |
| `tests/browser-human-confirmation.test.ts` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/tests/browser-human-confirmation.test.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/tests/browser-human-confirmation.test.ts) |
| `tests/browser-human-preview.test.ts` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/tests/browser-human-preview.test.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/tests/browser-human-preview.test.ts) |
| `tests/browser-policy.test.ts` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/tests/browser-policy.test.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/tests/browser-policy.test.ts) |
| `tests/development.test.ts` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/tests/development.test.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/tests/development.test.ts) |
| `tests/links.test.ts` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/tests/links.test.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/tests/links.test.ts) |
| `tests/native-runtime-fixture.ts` | Added | [native](/private/tmp/whip-native-final-acceptance/apps/desktop/tests/native-runtime-fixture.ts) |
| `tests/native.test.ts` | Changed | [reference](/private/tmp/whip-ux-reference/apps/desktop/tests/native.test.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/tests/native.test.ts) |
| `tests/preview-environments.test.ts` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/tests/preview-environments.test.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/tests/preview-environments.test.ts) |
| `tests/project-open.test.ts` | Changed | [reference](/private/tmp/whip-ux-reference/apps/desktop/tests/project-open.test.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/tests/project-open.test.ts) |
| `tests/provider-environment.test.ts` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/tests/provider-environment.test.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/tests/provider-environment.test.ts) |
| `tests/runtime-attach.test.ts` | Changed | [reference](/private/tmp/whip-ux-reference/apps/desktop/tests/runtime-attach.test.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/tests/runtime-attach.test.ts) |
| `tests/runtime.test.ts` | Changed | [reference](/private/tmp/whip-ux-reference/apps/desktop/tests/runtime.test.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/tests/runtime.test.ts) |
| `tests/ssh-profiles.test.ts` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/tests/ssh-profiles.test.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/tests/ssh-profiles.test.ts) |
| `tests/ssh.test.ts` | Changed | [reference](/private/tmp/whip-ux-reference/apps/desktop/tests/ssh.test.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/tests/ssh.test.ts) |
| `tests/transport.test.ts` | Changed | [reference](/private/tmp/whip-ux-reference/apps/desktop/tests/transport.test.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/tests/transport.test.ts) |
| `tests/updates.test.ts` | Unchanged | [reference](/private/tmp/whip-ux-reference/apps/desktop/tests/updates.test.ts) · [native](/private/tmp/whip-native-final-acceptance/apps/desktop/tests/updates.test.ts) |

</details>

## Key acceptance caveats

- The main inventory provides C1–C10 and P01–P12 as proposed acceptance packs, not a request to run every discovered script.
- Reference provider setup seeded defaults; native confirmation assertions are not an equivalent first-run comparison.
- Native content records intentionally use generic Attachment labels and small-file fixtures; those records do not approve filename loss or establish large-file parity.
- REPL capability-name checks do not establish argument/path summaries or unmatched restart visibility.
- The existing native workflow/operation manifest classifies capability ownership; it does not replace retained-feature parity.
- Preserve reference failures and explain the reason for reruns. Record fresh build, profile, data, credential/default state, recipient, pending states and evidence per scenario.
