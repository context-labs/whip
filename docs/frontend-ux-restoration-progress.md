# Frontend UX restoration implementation record

Status: in progress. This is not a claim of complete UX parity or release acceptance.
The [approved plan](frontend-ux-restoration-plan.md) defines scope and A1–A12.

## Reference and authority

Reference: development `12f0ea0768b7d769765596c35c049fe80edfaeba` plus the
14-file overlay verified against every hash in
[frontend-ux-reference.json](frontend-ux-reference.json). Reconstructed in the
isolated `/private/tmp/whip-ux-reference` checkout. Protocol generation and
`npm run pack:web` pass; renderer artifact:
`16092ad369f4c920d46d3aa169bf93c670b3348bd9395dd432030d6439019e46`.
The original development checkout and installed runtimes are untouched.

The user authorized implementation end to end. Ordinary UX follows this exact
reference. Rare truthful recovery remains. Existing native permission scopes
remain exact; broader old Remember scopes are still a flagged parity gap, not
an implicit authority expansion. No performance campaign, merge or deployment.

## Increment 1 — opening and readiness

- Startup checks the selected provider immediately after inventory; optional
  presets, catalog, execution defaults and MCP reads warm independently.
- Pending provider readiness retains the composer and control footprint.
  Command credentials and refreshable accounts stay eligible for explicit use;
  opening the app does not run commands, refresh accounts or infer.
- A live owned session no longer waits for tree decoration to allow submission.
  Tree errors stay a scoped details error; draft, focus and live activity remain.
  Host/session identity, selected/root scope and live-observation checks remain.
- The session-opening indicator itself already matches the reference. It was
  retained; its gating was the regression.

Regression evidence: four added welcome/runtime cases failed before the change;
all 83 tests in those two files pass afterward. The held/failing tree regression
failed before the session change; all seven conversation composition tests pass
afterward. The combined opening, provider, conversation, submission and recovery selection
passes 138 tests across eight files. `npm run check:web` passes the production
renderer build and app TypeScript; the final narrow diff was rechecked with
those 138 tests and app TypeScript. The build warns about existing large chunks;
jsdom logs its existing unimplemented `scrollTo` warning. Neither is a failure.

## Remaining work

Backend/provider/settings/terminal and durable presentation prerequisites are
being implemented in isolated branches. SDK bindings, restored ordinary forms,
activity/REPL presentation, response actions and latest reference polish follow.
A1–A12 comparative browser and desktop acceptance is still outstanding, including
the previously open NATIVE findings; unit tests alone do not close those gates.
