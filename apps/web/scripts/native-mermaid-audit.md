# Native Mermaid acceptance

Run `node apps/web/scripts/mermaid-diagrams.mjs` after `npm run pack:web`.
The default runs Chromium and Firefox. `WHIP_WEB_BROWSERS=electron` runs the
actual staged desktop after `npm run build:desktop -- --renderer-ready`.
`WHIP_WEB_BROWSER_RESULTS` selects the report/screenshot directory.

| Retained guarantee | Native scenario |
| --- | --- |
| Ordinary code stays ordinary | A real canonical JavaScript fence remains code and never loads the diagram worker. |
| Static versus live completeness | Actual authored diagrams render; two provider-streamed assistant fences remain source-only until the real held turn settles. |
| Exact source authority | Independent duplicate view choice, exact source and whole-response copy, keyboard Expand, Fit/100%, focus return. Clipboard is intercepted only at the normal browser boundary. |
| Durable reload | Canonical native messages reproduce both settled images after full reload. |
| Reader intent | A cold worker asset is held while a later paragraph anchors; image decode and system-theme replacement preserve that exact row/offset. |
| Virtualization | Source/diagram choices survive unmount/remount independently for identical fences. Focus is moved away before requiring row eviction. |
| Older history | 129 actual native turns put the original diagrams outside the tail; explicit `sessions.history_page` reads preserve anchors and load static duplicate fences. Connection-local request IDs, exact session identity and opaque cursors are retained. |
| Bounded source | A complete canonical source larger than the diagram's 32 KiB limit stays source-only, exposes the real 16 KiB display bound and still copies exact full source. |
| Desktop resources | Reused disposable native desktop launcher plus a distinct URL host verifies staged custom-protocol worker/fonts, live settlement, expansion, reload and CSP. |

The retired probe's final case fabricated `history.presentation.omitted` on a
real response. Native committed history has complete typed text or explicit
scoped content references; it does not carry that retired clipping shape. The
native production probe instead proves the actual oversized-source boundary
without mutating a transcript reply. The original component-level incomplete
source contract remains directly covered by `packages/ui/tests/mermaid.mjs`
(`truncated`, no worker/image, explicit excerpt) and
`packages/app/test/mermaid-markdown.test.tsx` (truncation propagation). Live
previews remain provisional and never render an image before settlement.

The runtime, provider, homes, working directory and all sessions are disposable.
No real account, installed runtime, signed/notarized app or Safari acceptance is
claimed. The Electron helper owns an additional distinct local host and joins
it separately; URL transport is not mislabeled local IPC. Native history and
model/provider work are never fabricated in a frontend event reducer.

## Native checkpoint (2026-09-29)

- Production Chromium 153.0.8010.12 and Firefox 155.0: all 12 checks per
  browser passed. Cold image replacement, theme replacement and each of the two
  explicit older pages retained the same anchor with **0 px** drift. The three
  owner-scoped history responses used the exact cursor chain `159` → `59` →
  `null`; the script treats those values as returned identities, not arithmetic.
  Page errors and CSP violations were empty.
- Actual disposable staged Electron 44.2.0 / Chromium 152.0.7977.76: custom
  protocol worker/font/image decode, held live turn, source/expand, reload and
  CSP passed. Native content and document both measured 1280 × 900, inside the
  display work area. The shared remote launcher now uses `setDesktopViewport`
  before connection UI interaction; no emulated viewport extends beyond the
  native window. All Electron, browser, provider and runtime processes joined.
- Existing `packages/ui/tests/mermaid.mjs`: Chromium and Firefox passed all
  seven lifecycle/appearance/CSP/accessibility groups, including the original
  incomplete-source boundary, all 66 themes and narrow touch controls. No
  browser was skipped. This is component evidence, not another runtime.
- `packages/app/test/mermaid-markdown.test.tsx`: 10 tests passed; shared native
  desktop viewport helper: six tests passed. Syntax and whitespace checks passed.

Commands used:

```sh
WHIP_WEB_BROWSER_RESULTS=/tmp/whip-native-mermaid-third node apps/web/scripts/mermaid-diagrams.mjs
WHIP_MERMAID_BROWSERS=chromium,firefox npm run test:mermaid -w @whip/ui
WHIP_WEB_BROWSERS=electron WHIP_WEB_BROWSER_RESULTS=/tmp/whip-native-mermaid-electron node apps/web/scripts/mermaid-diagrams.mjs
npx vitest run --config apps/web/vitest.config.ts packages/app/test/mermaid-markdown.test.tsx
node --test apps/web/scripts/performance-desktop.test.mjs
```

Both runtime and staged app served renderer digest
`484c6721166bf35fde7ce8ebc9585d34f56a9ad9f2a278a3a517bd59266dd1ae`;
the staged runtime digest was
`3a07d0bb738bf3879f29b46a31ebfbc748a1bb756077e23c559c0f1b2d2e1c1f`.
The Firefox older-history screenshot was visually inspected: one duplicate
remained source while the other rendered its diagram after remount.
