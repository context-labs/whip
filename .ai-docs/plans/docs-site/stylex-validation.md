# StyleX cold-cache validation

Recorded 2026-09-24 while validating the static docs worktree. This records a test-harness issue, not a new frontend architectural requirement.

## Finding

`apps/web/scripts/stylex-dev.test.mjs` passed with the original checkout's warm Vite dependency cache, but hung with a cold cache. Adding docs dependencies invalidated the cache and exposed an existing StyleX/Vite wait cycle. No existing dependency version changed in the docs lockfile (174 package records added, zero removed or version-changed). Relevant installed versions matched across trees: Vite 8.2.2, rolldown 1.2.7, StyleX unplugin/Babel plugin 0.19.0, Lightning CSS 1.33.0, Babel 7.29.7.

During comparison, original checkout HEAD was `d097d59483348f3659c7cc6f548e04a057382c15`; docs worktree HEAD was `071fe7e34e75b04fcce19af20f47e5525fb46db7`. The original branch changed externally during the task. Exact web test/config comparisons were identical, and the commit comparison had no changes in `apps/web`, `packages/ui`, `packages/sdk`, `package.json`, or `package-lock.json`. Node was v24.14.1.

## Bounded reproduction

All probes used `createServer` with the existing web Vite config and the test's `server: { host: '127.0.0.1', port: 0, preTransformRequests: false }`. Each requested the activity-indicator TSX over HTTP, then `/virtual:stylex.css`, and awaited `server.close()`. An independent 20-second process deadline bounded each probe. `DEBUG=vite:deps,vite:load,vite:transform` exposed the stalled stage.

| Probe | Result |
| --- | --- |
| Original checkout, existing warm cache | Hash consistent; module/CSS 200; CSS 4642 bytes; closed normally. Parent separately measured original exact test at about 1.25 seconds. |
| Docs worktree, existing cold cache | Dependency scan/bundle completed; token transform stalled; exited at the 20-second deadline. |
| Original checkout, inline `optimizeDeps: { force: true }` | Reproduced the same token-transform hang and 20-second deadline. This rules out new docs dependencies as necessary to trigger it. |
| Docs, inline `optimizeDeps: { noDiscovery: true, include: [] }` | Module/CSS 200; closed normally. Diagnostic only, not the chosen fix. |
| Docs, inline `optimizeDeps: { force: true, holdUntilCrawlEnd: false }` | Full dependency scan and bundle completed; module/CSS 200; CSS 4642 bytes; closed in under a second. |

Forced optimization reproduces the cold-start condition without creating another worktree. Probes wrote only generated cache artifacts, not tracked application, config, or dependency files. All probe processes ended; none remain running.

## Wait cycle

Installed package source provides the chain:

1. `@stylexjs/unplugin/lib/es/core.mjs:364–375` eagerly resolves and awaits `ctx.load()` for every output import in watch mode. Transforming `tokens.stylex.ts` therefore loads its optimized `@stylexjs/stylex` dependency.
2. Vite's `optimizedDepsPlugin` in `vite/dist/node/chunks/node.js:5396` awaits that dependency's `info.processing` promise.
3. With default `holdUntilCrawlEnd: true`, the optimizer waits for `environment.waitForRequestsIdle()` (`node.js:34450`) before committing its completed bundle.
4. The token transform is still registered as pending (`node.js:20594`, `35615–35650`), so the crawl cannot become idle while the transform awaits the optimizer.

The test deliberately exercises a partial graph without browser-driven import traversal. Node's test timeout can report failure without exiting while the server/transform remains stuck, so a timeout alone is not a fix.

## Narrow fix

Only the test's `createServer` options changed:

```js
// Keep the cache cold without waiting for a browser to finish this partial import graph.
optimizeDeps: { force: true, holdUntilCrawlEnd: false },
```

`force` guarantees the purported cold test actually uses a cold dependency cache. `holdUntilCrawlEnd: false` lets the optimizer publish its completed bundle without waiting on the partial import graph. Dependency scanning/optimization and all existing CSS assertions remain enabled. No product code, web Vite config, packages, or lockfile changed for this fix.

## Validation after patch

Three consecutive runs of `node --test apps/web/scripts/stylex-dev.test.mjs` in the docs worktree passed and exited normally:

- Run 1: one test passed; total 991.535 ms.
- Run 2: one test passed; total 1003.486 ms.
- Run 3: one test passed; total 1004.139 ms.

A fourth run with `node --test --test-timeout=30000 apps/web/scripts/stylex-dev.test.mjs` passed and exited normally in 1000.614 ms. All four were additionally guarded by an external 35-second process kill timer, which never fired. `git diff --check -- apps/web/scripts/stylex-dev.test.mjs` passed.

Broader integration gates remain the parent agent's responsibility; this focused test fix does not waive any of them.
