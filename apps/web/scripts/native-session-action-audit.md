# Native session action acceptance

Run after `npm run pack:web`:

```sh
node apps/web/scripts/session-actions.mjs
```

`WHIP_WEB_BROWSERS=chromium` or `firefox` narrows a diagnostic. Set
`WHIP_SESSION_ACTION_RESULTS` to retain screenshots and `report.json` elsewhere.
The default uses both browsers against an actual disposable native runtime,
private home and local fake provider, with the production asset/CSP bundle.

| Retained scenario | Native evidence |
| --- | --- |
| Session details → Rename nested dialog | Escape closes the action and preserves Session details. |
| Inactive row rename | Full long title comes from `sessions.get` + `trees.get`; the exact background owner receives no transcript, observation or turn-page read. The selected root and URL remain unchanged. |
| Keyboard Open in submenu | Right click, keyboard focus and ArrowRight expose Copy directory. |
| Archive and archived search | Canonical `trees.update` metadata, undo affordance, Search state → Archived sessions with active roots excluded, nested rename dialog, explicit restore. |
| Other-client archive/restore | A separately connected SDK client performs revision-checked metadata edits; the open archived search updates from catalog invalidation. |
| Same-directory fork | Browser explicitly admits `sessions.fork` with a stable request identity and captured history/config revisions; a distinct root/tree retains exact committed content and directory, is unarchived, and has no copied attempt charges. |
| Archive an open fork | The open view and unsent draft remain; explicit restore returns the row. |
| Delete an open fork | Actual native delete removes open/closed saved views and owner-specific local draft keys; the source and previously selected root remain unchanged. |
| Pointer and touch appearance | Dark/light submenu screenshots and 390 px mobile submenu remain visible without document overflow. |
| Strict CSP | Bounded page-error and CSP collection survives document reloads and must remain empty. |

The old `root.snapshot` count is replaced with exact-owner native read evidence;
metadata reads remain allowed and do not hydrate execution/transcript pages.
Native metadata mutations use the current tree revision, rather than retired
session rename/archive command envelopes. No old client facade, fake reducer or
seeded history is involved. The fork fixture now includes a real completed model
turn instead of testing only an empty history.

Setup is inside its resource-owning `try`. Cleanup always joins the browser and
native runtime. Request evidence retains only method/owner/tree identifiers,
with a 2,048-frame ceiling; error/CSP evidence has 64 entries and failure text
is capped at 16 KiB. This is browser acceptance, not signed desktop acceptance.

During the migration, the actual native probe exposed archived roots still being
grouped into the normal sidebar. The narrow sidebar projection fix excludes
those rows before directory grouping while retaining the authoritative catalog
and open archived tabs. Archived-only discovery is explicitly available through
Search sessions → Search state; the old separate Archived sessions entry is
replaced by that native filtered read, not an all-status search approximation.

## Native validation checkpoint (2026-09-29)

`WHIP_SESSION_ACTION_RESULTS=/tmp/whip-native-session-actions-final node apps/web/scripts/session-actions.mjs`
passed all 11 workflow groups in Chromium 153.0.8010.12 and Firefox 155.0, with no
page errors or CSP violations. Reports and screenshots are in that directory;
the production renderer digest is
`fd6267094c1b50a95efe39f382f7c613ef19e311dd4fa64bb8916ad12685f731`.
Owned browser/runtime cleanup joined. Syntax and diff checks pass. Independent
read-only review found no lifecycle or assertion blocker.

The associated sidebar repair passed 28 focused tests; archived-only search
passed 29 discovery/action tests, including retired-filter reply cancellation,
exact large revision cursors and alternate-host/child-owner negatives. Full
shared-app TypeScript checks passed for both product leaves. No broader browser
or signed Electron claim is made by this checkpoint.
