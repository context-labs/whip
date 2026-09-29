# Native draft tabs and catalog titles

After `npm run pack:web`, run `node apps/web/scripts/new-chat-tabs.mjs` and
`node apps/web/scripts/session-title-notifications.mjs`. Both default to Chromium
and Firefox. `WHIP_WEB_BROWSERS` narrows a diagnostic; results go to
`WHIP_WEB_NEW_CHAT_RESULTS` and `WHIP_TITLE_NOTIFICATION_RESULTS` respectively.

Both probes use the packaged renderer, private native runtime, real engines and
an owned no-auth loopback provider. They do not start an installed runtime or
read a user's provider credentials.

| Retained guarantee | Native evidence |
| --- | --- |
| Independent unsent tabs | Explicit New creates separate draft identities; text, permission override, directory, pane and selection survive switching/reload. Untouched permission state inherits the host default. |
| Drafts allocate no sessions | No create, submit, spawn, history, observation or turn-page requests before first submission; draft IDs never appear in root summaries. |
| Close/reopen and layout | Draft reopen preserves identity/text; draft menus omit session actions; a draft moves to a real second pane; mixed session/draft panes and compact picker remain usable. |
| Empty workspace | Closing the final session first creates the established blank draft. Closing that final draft leaves an empty workspace across reload; explicit New opens one draft. |
| File staging | Two images dropped on an unfocused pane remain local to that pane without root allocation or focus theft. |
| Image-only first submission | Exactly one root is created; held native uploads name that owner; both scoped image parts appear in canonical accepted input and previews. |
| Background creation | The actual create request is held before dispatch. Explicit release promotes and submits the original draft without changing another pane's route, focus or unsent text. |
| Failed first upload | A protocol error before upload dispatch retains text/image in the newly created session. Explicit file replacement permits submission without another create. |
| Inactive and unopened titles | Exact-revision tree metadata edits change the catalog revision and update the inactive tab/unopened sidebar row within the original three-second bound. |
| Metadata-only title reads | Default native catalog polling replaces the retired push event and timer patch. The unopened owner receives no history, session observation or turn-page read. |

The original empty-workspace assertion was stale: commit `203695a03b` on
2026-09-13 intentionally made closing the final session open one blank draft.
The native probe preserves that behavior and then tests closing the draft;
`session-tab-routing.ts` and the desktop close-tab regression remain authoritative.
The image preview action is now named `Preview Attachment N`. Draft attachment
checks are scoped to the composer so accepted message attachments are not mistaken
for unsent files. No production behavior was changed for those fixture corrections.

The shared provider fixture now returns a nonempty response for actual image-only
input; echoing its absent text previously produced an invalid empty assistant
response. The input and uploaded bytes remain unchanged.

New-tab request evidence stores only method/owner identifiers (4,096 frames,
128 active proxy connections, 16 upload identities, at most two held uploads).
Held requests retain their original bounded wire message. Cleanup discards held
callbacks, closes both proxy endpoints, joins pending closes and closes the browser
and native fixture. Title evidence has a 2,048-frame ceiling. Neither probe keeps
raw transcript or attachment bodies in its request log. Page/CSP errors are bounded
and must remain empty.

## Validation

The complete new-tab matrix passed all 12 workflow groups in Chromium 153.0.8010.12
and Firefox 155.0. Reports/screenshots are in `/tmp/whip-native-drafts-final`.
Both title probes passed in `/tmp/whip-native-title-first`: observed changes took
1,353 ms and 1,410 ms using normal native polling. The renderer for these runs was
`187ddeaeefc2117ea5befac4e7c664296abc8fd3e2275fcd7c0e9aa8911e08d8`.
The native fixture/stream suites passed four tests in 5.188 s. All owned cleanup
joined. An independent review caught the obsolete `turns.page` name in a negative
assertion; the final assertion includes canonical `sessions.turns` and both history
methods. Both tightened probes pass again in Chromium and Firefox, with all 12 draft
workflows, empty page/CSP error lists and joined cleanup. Final reports are
`/tmp/whip-native-title-final` and `/tmp/whip-native-drafts-tightened`.

These are browser and disposable-provider checks. They do not establish signed
Electron, physical file drag, Safari or live-provider acceptance.
