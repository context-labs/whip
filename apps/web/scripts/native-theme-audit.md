# Native Claude Code theme acceptance

Run `node apps/web/scripts/claude-code-theme.mjs` after `npm run pack:web`.
The default covers Chromium and Firefox; `WHIP_WEB_BROWSERS` selects a diagnostic
browser and `WHIP_THEME_RESULTS` selects the output directory.

All original assertions remain: choose Claude Code from the actual Appearance
picker, persist it through reload, check exact Paper background/foreground,
sidebar and border colors, inspect selected and hovered session rows, run Axe
contrast checks in conversation and session search, inspect compact navigation,
and switch to dark to prove pinned colors reset. Screenshots retain each state.
The conversation is created through native tree creation and receives an actual
local HTTP-provider response; the fixture effect log must contain precisely that
one authored input. There is no seeded retired snapshot or legacy SDK import.

The same-origin Axe script route is a test-only inspection hook and leaves the
production CSP unchanged. Page errors and CSP violations survive reload in
bounded Node-owned collections. Setup and cleanup include fixture construction,
client reads, browser launch and navigation; browser close failure cannot skip
runtime shutdown. No installed runtime, real account, signed desktop or actual
Safari claim is made by this probe.

## Native validation checkpoint (2026-09-29)

`WHIP_THEME_RESULTS=/tmp/whip-native-theme-third node apps/web/scripts/claude-code-theme.mjs`
passed all seven groups in Chromium 153.0.8010.12 and Firefox 155.0. Both page
error/CSP arrays and both conversation/search Axe contrast results are empty.
Each host received exactly one synthetic provider input, and browser/runtime
cleanup joined. The Firefox conversation screenshot was visually inspected.

Native Appearance uses a theme combobox/popover instead of the retired dialog;
Settings occupies the full page and no longer contains a session sidebar. The
probe checks Settings document colors there, then checks the same exact sidebar
colors in the workspace and returns there after resetting the theme. It retains
all original palette, selection, hover, persistence and reset guarantees.

The production renderer digest is
`fd6267094c1b50a95efe39f382f7c613ef19e311dd4fa64bb8916ad12685f731`.
Runner syntax and diff checks pass. No product source changed in this leaf.
