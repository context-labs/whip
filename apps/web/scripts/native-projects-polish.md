# Projects and compact controls acceptance

Build the SDK and renderer once in the target checkout, then run:

```
node apps/web/scripts/native-projects-polish.mjs /absolute/checkout
node apps/web/scripts/native-projects-zoom.mjs /absolute/checkout
```

`WHIP_PROJECTS_POLISH_RESULTS` and `WHIP_PROJECTS_ZOOM_RESULTS` select artifact directories. The browser check defaults to Chromium and Firefox (`WHIP_WEB_BROWSERS` can select either for diagnosis). Both scripts load the target checkout's fixture/SDK and record the renderer digest. All hosts, keys, profiles, directories, app data and shell homes are disposable. The zoom check uses actual `webContents.setZoomFactor(2)` in isolated stock Electron, without installing Whip.

The checks exercise actual native Projects recency across two hosts, explicit one-folder creation and choice without session admission, keyboard context menus with close actions last, Session details, compact agent options and retained drafts, and Projects/dialog geometry at 530/390/320 CSS pixels. The browser matrix also uses the maximum 20px product UI font; the separate Electron run verifies real 200% browser zoom, reachable agent registration controls and the wordmark inside the sheet. Electron screenshots use native `webContents.capturePage()` because Playwright page capture clips the Retina surface at page zoom.

These complement `sidebar.mjs`, whose retained assertions cover seven-row More/Less, collapse, navigation, scroll anchoring across recency changes, paging, search, themes and mobile controls. A real scroll range is required before its anchor assertion: navigation remount resets expanded directory limits. They also complement macOS's unit assertion that native `showOpenDialog` receives `createDirectory`. OS-native picker chrome, installed editor apps, signed/Finder acceptance and hardware/accessibility validation remain separate.
