# @whip/app

Start with the canonical [frontend architecture and design guide](../../docs/frontend.md)
for design decisions, state ownership, package boundaries, and implementation patterns.
This README documents the app package's integration contract.

WHIP's shared React application for the web shell and a future Electron renderer.
This private ESM package distributes TypeScript source. It owns navigation and
presentation; `@whip/sdk` owns connections and synchronized session state, and
the execution host owns all work and configuration.

```tsx
import { createWhipApplication } from '@whip/app';
import { initializeTheme } from '@whip/ui/themes';
import '@whip/ui/fonts.css';
import '@whip/ui/reset.css';

initializeTheme({ storage: platform.storage });
const application = createWhipApplication(platform);
root.render(<application.Application />);
await application.runtime.connect();
// On permanent shell teardown: application.dispose();
```

Use `AppPlatform` from `@whip/app/platform` for device storage, clipboard,
external links and downloads. Storage provides `keys()`, `getItem`, `setItem` and
`removeItem`; the browser shell adapts native `localStorage`. Per-recipient draft
entries let synchronous page-exit writes preserve drafts from other tabs. Changes
to the same recipient use the last explicit write. Storage transactions must serialize recovery
updates between clients sharing that storage; the web shell uses native Web
Locks. Keep a single application instance outside React rendering. StrictMode
components lease the same SDK views and never create additional connections.

The consumer needs the StyleX Vite transform, React and TanStack Router's file
route transform. See `apps/web/vite.config.ts` for the build contract, including
nested CommonJS shims needed by Vite dependency discovery. `@whip/ui/tokens.stylex`
is the explicit cross-package variable module. Production styles are extracted,
fonts are served as files, and dynamic positioning is limited to runtime geometry.
Packed-package tests build and run an independently installed consumer in both
production and development.

There is one Query client per application runtime, cleared when detaching a host.
It contains host reads and detail pages, never a second session snapshot or
transcript. Unobserved queries are
immediately discarded; mailbox pagination retains at most four bounded pages.
The workspace leases one SDK view per distinct visible root (8 MiB retained payload, 512 messages per opened agent),
with at most four retained roots and 30 seconds of unused-view retention. Route
preloading never opens roots. Explicit content reads are limited to 1 MiB of text;
code rendering separately caps retained tokens, and downloads have a 64 MiB limit.
These are payload bounds, not a claim about total JavaScript heap overhead.


The workspace model lives in `session-tabs.ts`: up to four nested panes and 32
view instances, with one selected tab per pane. `session-tab-routing.ts` applies
the focused view's URL and browser-history hint. `workspace-views.ts` releases
obsolete consumers before acquiring their replacements. Duplicate chat/REPL views share
root/child SDK views and recipient drafts/uploads/locks; mode, scroll, caret and
agent selection belong to each view. Generic geometry and dragging live in
`@whip/ui/workspace-layout`. Window storage migrates the old flat tab list to
`whip.web.workspace.v2`, retaining the original v1 entry. It never stores a
transcript. The full tree survives single-pane presentation on narrow screens.

A tab's **Open REPL** action creates a fresh read-only execution view immediately
to its right in the same pane. **Open chat** selects the nearest same-agent chat
there, or creates one to the right, preserving both views and their reading positions.
The shared session information bar shows host/project, selected agent and current
activity; requests and child activity remain above the composer.
`?view=repl` is the shareable mode; legacy and ordinary session URLs use chat.
`SessionContent` shares leases and human-request controls, `ReplView` renders SDK
`executionRows`, and `ReadingList` owns virtual reading/selection behavior for
both modes. The viewer submits no work and loads older history only on request.

Draft text is saved separately from command recovery metadata: up to 32 drafts,
256 KiB each, within a 1 MiB total budget. Files are uploaded only on explicit
selection, paste or drop. The window's composition store preserves files and
uploads across view teardown and tab close/reopen. Host replacement interrupts
transfers and marks files unavailable; reload requires reselecting them. Explicit
removal, accepted submission, or draft discard clears the matching attachments.
Uploads are never resubmitted automatically. Command identities are stored
before delivery.
Uncertain acceptance checks the original command; retry is explicit and retains
its original identity and payload. Closing the app, aborting a wait or changing
hosts never cancels accepted daemon work. Cancellation uses the exact active turn.

Theme definitions and resolution come from the shared Go catalog through
`cmd/themegen`; this package adds no palette or theme resolver. The UI package
owns tokens, all built-in variants, automatic appearance and portaled controls.
Appearance settings discover or import custom JSON through the host's bounded
resolution API. Device theme and keyboard preferences never update host settings.

Run from the repository root:

```sh
npm ci
npm run check:web
npm run test:web
npm run build:storybook
npm run test:packed -w @whip/ui
```

See [web-app.md](../../docs/web-app.md) for local setup, test evidence and remaining manual checks.
Editing, review, interactive terminals, hosting authentication and Electron
packaging are outside this application milestone.
