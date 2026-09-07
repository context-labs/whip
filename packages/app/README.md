# @whip/app

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

There is one Query client per attached host. It contains host reads and detail
pages, never a second session snapshot or transcript. Unobserved queries are
immediately discarded; mailbox pagination retains at most four bounded pages.
A route leases an SDK view (8 MiB retained payload, 512 messages per opened agent),
with at most four retained roots and 30 seconds of unused-view retention. Route
preloading never opens roots. Explicit content reads are limited to 1 MiB of text;
code rendering separately caps retained tokens, and downloads have a 64 MiB limit.
These are payload bounds, not a claim about total JavaScript heap overhead.

Draft text is saved separately from command recovery metadata: up to 32 drafts,
256 KiB each, within a 1 MiB total budget. Files are uploaded only on explicit
selection, paste or drop; incomplete uploads are cancelled on view teardown and
never resubmitted automatically. Command identities are stored before delivery.
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

See `docs/web-app.md` for local setup, test evidence and remaining manual checks.
Editing, review, interactive terminals, hosting authentication and Electron
packaging are outside this application milestone.
