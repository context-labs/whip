# Native model-budget acceptance

Run after `npm run pack:web`:

```sh
node apps/web/scripts/model-budgets.mjs
```

The default covers Chromium and Firefox at 1280/390 px in dark/light appearance,
including full-page reloads. `WHIP_WEB_BROWSERS` narrows a diagnostic and
`WHIP_BUDGET_RESULTS` changes the report/screenshot directory. Every page uses the
production bundle/CSP and an actual disposable native runtime with private home.
No real provider account, installed runtime or seeded legacy snapshot is used.

| Retained guarantee | Native assertion |
| --- | --- |
| No default monetary/token/elapsed model cap | Exact `budgets.list` evidence has null limits and zero used, reserved and uncertain values; the additional canonical model-call category is checked too. |
| No invented unlimited host authority | “No local cap” replaces “Unlimited”; native resource limits remain separate and active operations retains limit 64. Logical-write budgets remain finite. |
| Read-only usage inspector | No Set cap control, Budget agent selector or budget/resource mutation is sent. Inspection admits no input, tool or shell operation and invokes no model. |
| Exact empty ledger values | Whole-tree reported cost is zero **from zero attempts**; input tokens display Not reported. Zero cost does not manufacture token evidence. Model cost budget precision is nanodollars (nine decimal places), replacing the old six-decimal string. |
| Reload and appearance | All eight browser/appearance/width combinations reload with the same canonical values; no page errors or CSP violations. |

The old root snapshot and `budget.cap` command are not reproduced. Native
`budgets.list`, `resources.list` and `usage.get` are the existing authoritative
read paths. Reads are checked not to change budget/accounting state, and the
provider effect log must stay empty. Whole-tree totals are not derived from a
visible transcript or execution window.

Browser/context/runtime setup and cleanup are nested. Request evidence retains
at most 128 method counters and refuses more than 2,048 frames per case;
page/CSP evidence retains 64 entries, failure body text at most 16 KiB. This is
browser acceptance, not signed desktop or real-provider billing acceptance.

## Native validation checkpoint (2026-09-29)

`WHIP_BUDGET_RESULTS=/tmp/whip-native-budgets-first node apps/web/scripts/model-budgets.mjs`
passed all eight combinations in Chromium 153.0.8010.12 and Firefox 155.0. Each
combination checked both initial inspection and reload; every page-error/CSP
array is empty. The native budget and whole-tree accounting values remained
identical, and the model effect log stayed empty. Reports and screenshots are
in that directory. Owned contexts/browsers/runtime joined on exit.

The production renderer digest is
`fd6267094c1b50a95efe39f382f7c613ef19e311dd4fa64bb8916ad12685f731`.
Runner syntax and diff checks pass. This leaf changes only the native probe and
its scenario/evidence map; no budget or rendering behavior was changed.
