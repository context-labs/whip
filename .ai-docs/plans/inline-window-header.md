# Inline window header (hiddenInset titlebar) for the Whip desktop app

Research + plan, 2026-09-08. Goal: hide the native macOS title bar and inset the
traffic-light buttons into Whip's own UI, like the HALO desktop app. The top of
the window becomes Whip's own chrome — the session tab strip — with an empty
drag strip over the sidebar, so the whole top edge reads as one inline header.

## How HALO does it (ElectroBun — reference only)

HALO (`context-labs/src/halo/app`) combines three pieces:

1. **Window setup** — `app/src/bun/index.ts`: `titleBarStyle: "hiddenInset"`
   plus `trafficLightOffset: { x: 24, y: 22 }`. The dots sit over the sidebar's
   56 px header strip; the comment there notes macOS fixes their size, only the
   position is ours.
2. **One fixed drag header** — `src/mainview/components/AppHeader.tsx`: a fixed
   `h-14` drag region spanning the full window width, split into a left cell
   over the sidebar (empty in the desktop shell — it's the traffic-light zone,
   with the wordmark moved below it into `WorkspaceNav`) and a right cell with
   breadcrumbs, status, and actions. Interactive children are wrapped in
   `electrobun-webkit-app-region-no-drag`.
3. **Drag regions without CSS** — ElectroBun's renderer preload
   (`node_modules/electrobun/.../dragRegions.ts`) detects its
   `.electrobun-webkit-app-region-drag` / `-no-drag` classes natively; HALO
   never writes `-webkit-app-region` CSS itself.

## What changes in Electron

Whip's `apps/desktop` is Electron (macOS-only: forge makers are dmg/zip darwin).
The mapping is direct, with one renderer-side difference:

| HALO / ElectroBun                    | Whip / Electron                                        |
| ------------------------------------ | ------------------------------------------------------ |
| `titleBarStyle: "hiddenInset"`       | Same option exists on `BrowserWindow` (macOS)          |
| `trafficLightOffset: {x, y}`         | `trafficLightPosition: {x, y}` (position of *left* dot)|
| `.electrobun-webkit-app-region-drag` | Plain CSS: `-webkit-app-region: drag` / `no-drag`      |

Current state:

- `apps/desktop/src/main.ts:86-89` creates the `BrowserWindow` with no
  `titleBarStyle` — the default native title bar sits above the window.
- The renderer (`packages/app`) has no drag-region CSS at all.
- The app shell (`packages/app/src/shell.tsx`) is `[sidebar][main]`; the
  sidebar's top row is the WHIP brand + collapse toggle
  (`session-sidebar.tsx` `brandRow`, `minHeight: 40`), and the main column's
  top chrome is the 48 px tab strip (`WorkspaceTabs`, `row.height: 48` in
  `packages/ui/src/workspace-tabs.stylex.ts`).
- The renderer already knows it's on desktop: `window.whipDesktop`
  (versioned `DesktopBridge`), surfaced to the app as `AppPlatform`
  via `createDesktopPlatform` in `apps/web/src/platform/desktop.ts`.

## Design

Row heights: keep the tab strip at 48 px and make the sidebar's brand row
48 px on desktop so both are flush; vertically center the dots in that row:
`trafficLightPosition: { x: 14, y: 18 }` (12 px-tall dots, y = (48−12)/2).
This matches how Electron's own `hiddenInset` default (x 12, vertically
centered in a ~38-40 px strip) scales up; tune x by eye so the first dot
aligns with the sidebar's 8 px padding + brand inset.

Two interacting behaviors:

- **Sidebar hidden** (or not matched to a session route): the tab strip's
  `leading` cell (already where the restore toggle goes) slides flush left and
  must reserve the traffic-light zone — ~84 px of empty, draggable padding.
- **Split panes** (`WorkspaceLayout` renders a strip per pane): only the
  leftmost pane's strip may be a drag region; other panes' strips stay
  interactive/no-drag or their headers would stop accepting pointer input.

Web and mobile (`compact`) are untouched: the bridge is absent in browsers and
the mobile sheet/header keep their current layout.

## Plan

1. **Bridge contract** (`packages/app/src/desktop-bridge.ts`): add
   `readonly chrome: 'inset'` to `DesktopBridge` (still `version: 1` — additive;
   bump only if the host/renderer compatibility check in
   `apps/web/src/main.tsx` should hard-fail on older hosts).
2. **Preload** (`apps/desktop/src/preload.ts`): pass `chrome: 'inset'` when
   `process.platform === 'darwin'`. (Makers are darwin-only today; the guard
   keeps a future Windows build on native chrome instead of breaking it.)
3. **Main process** (`apps/desktop/src/main.ts:86`): add
   `titleBarStyle: 'hiddenInset'` and `trafficLightPosition: { x: 14, y: 18 }`
   to the `BrowserWindow` options, macOS-guarded. No window-state, menu, or
   lifecycle changes; `close`/`hide` behavior is unaffected. Note: hiding the
   title bar also removes the native double-click-to-zoom titlebar gesture
   (acceptable — HALO made the same trade).
4. **Platform surface** (`packages/app/src/platform.ts`): add
   `readonly chrome?: 'inset'` to `AppPlatform`; set it in
   `createDesktopPlatform` (`apps/web/src/platform/desktop.ts`) from
   `bridge.chrome`; the browser platform leaves it undefined.
5. **Drag-region styles** (`packages/app/src/styles.ts`): add exported StyleX
   entries — `windowDrag` (`WebkitAppRegion: 'drag'`) and `windowNoDrag`
   (`WebkitAppRegion: 'no-drag'`). This is the one place Electron replaces
   ElectroBun's preload class detection.
6. **Shell + sidebar** (`shell.tsx`, `session-sidebar.tsx`,
   `session-sidebar.stylex.ts`): when `runtime.platform.chrome === 'inset'`
   (and not compact): apply `windowDrag` to the sidebar `<aside>`; raise
   `brandRow` to 48 px; apply `windowNoDrag` to the brand link, the collapse
   toggle, and the sidebar's interactive regions (destinations, session list,
   footer, resize handle). The top 48 px of the aside stays empty — the dots
   live there — with the WHIP brand vertically centered alongside them to the
   right of the light zone (HALO's alternative of moving the wordmark below
   the strip doesn't fit Whip's existing `brandRow` layout).
7. **Tab strip** (`session-tab-strip.tsx`, `packages/ui/src/workspace-tabs*`):
   - Apply `windowDrag` to the strip row when desktop-inset; `windowNoDrag`
     on the tabs list and the leading/trailing utility cells (tabs are already
     drag-sortable via dnd; no-drag also protects text selection).
   - When the strip is the leftmost element (sidebar hidden, or the
     not-matched strip), add ~84 px left inset padding (`traffic-light zone`)
     so the first interactive control clears the dots.
   - Ensure non-leftmost pane strips never get `windowDrag`.
8. **Notices/attention bar**: verify `notices` (rendered above the strip in
   `shell.tsx`) don't sit under the drag region — they're below the strip in
   layout flow, so no change expected; confirm visually.
9. **Docs**: update `docs/frontend.md` — the desktop chrome decision (hiddenInset
   + drag regions + traffic-light reservation) and the sidebar/tab-strip
   geometry paragraph (~line 810) belong there per AGENTS.md.

## Verification

- `apps/desktop` dev run (`WHIP_DESKTOP_DEV_URL` fixture flow): check the dots
  align with the brand row, the top of the sidebar and the empty strip area
  drag the window, double-click on the drag strip doesn't zoom (expected), and
  sidebar hide/show moves the traffic-light reservation correctly between
  sidebar and strip.
- Split panes: leftmost strip drags; right pane strip buttons/menus work.
- Fullscreen and window resize: no layout jump; strip stays 48 px.
- Browser build (`apps/web` dev server / Playwright fixtures): no drag CSS
  applied, unchanged layout — the chrome flag is absent.
- Existing desktop tests (`apps/desktop/tests`) cover main-process units; add a
  small assertion for the window options if a harness exists, otherwise rely on
  the manual checklist.

## Open questions

- Exact `trafficLightPosition` x (12–16 px) and whether to align the first dot
  with the sidebar padding or the brand text — decide visually during
  implementation.
- Whether `chrome` should version the bridge (hard-fail old host + new
  renderer) or degrade gracefully (new renderer on old host shows native title
  bar + inset spacing — ugly but functional). Current leaning: additive field,
  no bump, since a mismatched host simply keeps its native title bar and the
  renderer only adds the reservation when the flag is present.
