# Native content probe audit

Append-only evidence for the specialized content probes. These checks use the
production renderer artifact, actual v4 runtime/gateway, public SDK and a local
synthetic provider. They never connect to an installed runtime or a real account.

## 2026-09-29 — retained assertion inventory

Starting renderer: `7f299c285a1df691bd43e07ab0f7e2256744c5fb`.
Read `docs/frontend.md` before changing the probes. The three old helpers were
reachable only through the retired chat-activity fixture. Their native entry is
now `native-content-probes.mjs`, independently runnable in Chromium and Firefox.

| Retained behavior | Native evidence |
| --- | --- |
| Local image decode and upload are independent; failures remain removable | Delay actual `content.put`, hold browser image completion, assert both states; drop one upload connection before forwarding and verify the reference was never installed |
| Local preview lifetime, original-size keyboard preview and focus return | Track File-owned object URLs separately from verified transcript Blob URLs; inspect draft/REPL remount, removal and accepted submission |
| Actual submitted files, recipient scope and ordering | Read canonical admitted content parts back through the exact Session and compare verified bytes with the original files |
| Drag/drop overlay, nested handlers, modal exclusion, no navigation or duplicate upload | Preserve original UI assertions while counting native `content.put` identities; preserve focus and reading offset |
| Mixed files, paste, maximum 16 attachments, narrow/light/large-text/reduced-motion layouts | Keep original file events, dimensions, wrapping, upload blocking and accessibility checks |
| Composer sizing and pinned/paused reading behavior | Reuse `composer-reading.mjs` unchanged |
| Historical five-image messages, loading geometry, keyboard original preview | Real scoped uploads and ordinary admitted turns; actual `content.get`/`content.read`, fixed thumbnails and decoded dimensions |
| Failed historical transfer and explicit in-place retry | Inject a native resource failure for one exact owner/reference and assert one explicit retry, then successful decode |
| Virtualization, cold read above selected text, stable anchor | Unmount/revisit real history; delay content reads and retain the following canonical row at 40px with the same selection |
| Internal/tool screenshot delivery, collapsed no-read and explicit expansion | A bounded loopback MCP server produces actual committed operation image references; inspect individual verified images, including an image larger than 1MiB, without replaying the tool |
| Probe cleanup does not send held effects | Hold a native upload, close the proxy and verify `content.get` returns `NOT_FOUND` |

The retired JSON message-body handle is not recreated. Native history already
contains canonical text and content reference IDs. Stored bytes are fetched by
exact session/reference, verified by the SDK, with the shared 4MiB image/download
bound and 1MiB text-preview bound. File names remain local draft presentation;
historical unnamed references use the native `Attachment N` label.

Native pagination has a persistent earlier-page control and remembers reading
positions. The probes wait for real history and only activate the earlier-page
control at the reading edge; they do not let Playwright scroll an offscreen
control into view and accidentally skip the image being inspected. Geometry
equality permits only browser layout precision (less than 1/64 CSS pixel).

## 2026-09-29 — independent prerequisite checkpoints

- `d862fd615`: actual MCP image fixture plus native provider continuation for an
  image-only user record following a tool result. Real runtime/operation/content
  test passed in 1.885s; exact owner, media type and verified bytes were checked.
- `b58c327b3`: same-runtime recovery keeps local draft previews while marking
  unfinished uploads interrupted; explicit detach/identity change revokes them.
  Compositions/composer suites passed 29 tests; full shared-app types passed.
- A PNG regression exposed `ContentRead` attempting UTF-8 text decoding for a
  real image. The repair supports only PNG/JPEG/WebP/GIF as bounded images after
  verified reads, clears/revokes on owner/client/disconnect/unmount, and keeps
  text decoding and explicit download behavior separate. Focused content and
  composition suites passed 34 tests; browser evidence is recorded below when
  each complete run finishes.

## 2026-09-29 — complete production-browser validation

The image-rendering prerequisite above is `c7037c175`. The final renderer was
packed from that source. Its manifest digest is
`4470a60032b87f332848fd342f308d51ef1013f208eaacf0ba6f3adc8d99c552`;
the runner fetched and verified every manifest asset from each actual runtime.

Run the complete gate with:

```sh
npm run pack:web
node apps/web/scripts/native-content-probes.mjs
```

All four modes passed in the final combined invocation, exit 0. Browser and
runtime shutdown completed before returning the browser test slot.

| Browser | Composer/drop | Stored/tool images | Largest host image | Anchor drift | Page/CSP errors |
| --- | --- | --- | ---: | ---: | ---: |
| Chromium | Passed; 16 attachment bound, 6 mixed-drop uploads, zero duplicates | Passed; 5 historical images, exact explicit retry, 25 mounted rows | 2,342,495 bytes | 0px | 0 / 0 |
| Firefox | Passed; 16 attachment bound, 6 mixed-drop uploads, zero duplicates | Passed; 5 historical images, exact explicit retry, 25 mounted rows | 3,089,512 bytes | 0px | 0 / 0 |

All four composer typing/reading samples had zero scroll drift in each browser.
The fixture's held-upload cleanup check confirmed `NOT_FOUND` after close, so
retiring a delayed transport cannot dispatch an upload during teardown. The
probe records at most 4,096 transfer identities/byte counts, not uploaded bodies.

The Firefox runs exposed pending wheel movement and a viewport resize that had
not reached layout before a geometry baseline was sampled. The probes now wait
for a settled baseline before the action; the original scroll and hover geometry
assertions remain unchanged. This is distinct from the two product repairs
recorded above.

Evidence from the final invocation:
`/tmp/whip-native-content-final-run.log` and
`/tmp/whip-native-content-final-results/results.json`, with screenshots beside
the result file. The invocation set only
`WHIP_CONTENT_RESULTS=/tmp/whip-native-content-final-results`; both browsers and
both modes used their default selection. Syntax checks and `git diff --check`
also passed. The three migrated helpers and their native runner/transport have
no retired SDK or `/api/v3` references.

This evidence covers Chromium and Firefox against disposable native hosts. It
does not claim Safari, Finder launch, Developer ID signing or notarization.

## Attachment preview confirmation (2026-09-29)

The mixed-host probe exposed an open attachment dialog closing when its exact
local authored input became canonical history. Timeline now keeps one
`MessageAttachments` subtree for both stages. Verified upload metadata may seed
its existing scoped metadata query; there is no second byte cache. An owner,
runtime/client, reference/digest change or retired row still closes the preview.

`node apps/web/scripts/attachment-confirmation.mjs` runs the actual native
runtime and production assets in Chromium and Firefox. It holds only the exact
owner's canonical observation containing one uploaded input, opens and verifies
its local preview, releases the real reply, and verifies the same dialog DOM and
bytes remain. Each browser sent one upload and one body read, with no additional
metadata read during confirmation and no page/CSP errors. Both owned process
lifetimes joined. Renderer digest:
`c06ea03a00b18e848547bcad4d0ea1446cc37adea7e6cbab3ff4c1f12ed87ff7`.

Focused regressions also cover confirmation during a pending byte read,
foreign-owner metadata refusal, owner/reference/runtime replacement, and row
retirement aborting a pending read. The earlier implementation failed both
same-input confirmation regressions (dialog unmounted and its read cancelled).
