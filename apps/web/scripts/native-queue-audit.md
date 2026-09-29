# Native queue acceptance audit

Append-only scenario mapping and validation evidence for `composer-queue.mjs`.
The runtime, storage, model preparation, queue control and rendering paths are
production code. Only the loopback HTTP model provider uses bounded test gates.
No installed runtime, real provider account or user configuration is accessed.

## 2026-09-29 — retained scenario mapping

| Retained scenario | Native proof |
| --- | --- |
| Enter and Send enqueue once; pending text is absent from transcript | Actual `sessions.submit`, authoritative input page and unchanged UI assertions |
| Two-image queued message preview | Explicit `inputs.get` dialog with original scoped image references and decoded image dimensions |
| Keyboard removal preserves draft and focus | Actual `inputs.cancel`; canonical cancelled input, unchanged active turn and draft |
| B steers between response fragments while A/C remain FIFO | Gated actual provider replies, immutable `inputs.steer` receipt, canonical same-turn input/parts and verified original image bytes |
| Steering survives document reload | Canonical steering metadata supplies the persistent status without a local command or an event replay |
| Untaken steering falls back if its exact target ends | Cancel the held target before a boundary; the accepted input runs once in a distinct ordinary turn |
| Root/child scope remains exact | A root cannot read or promote a child's input; a child cannot promote the root's input; rejection leaves the child input queued |
| Queue/draft survive reload, including pending steering | Real page reload with unchanged draft and bounded authoritative input observation |
| Twenty-four inputs remain virtualized and preserve reading intent | Fewer than 16 mounted rows, exact final-row hit test, unchanged anchor while queue grows/shrinks and draft changes |
| Agent dock, queue-only and composer-only geometry | Three actual spawned children; retained overlap, inset, height, focus, narrow, large-text, light/dark, contrast and reduced-motion assertions |
| Long-conversation typing remains stable | Existing `composer-reading.mjs` unchanged |

Two obsolete data/timing contracts are explicitly replaced:

- Native queue pages contain bounded metadata and an attachment count. Collapsed
  rows do not hydrate every queued body to restore the retired snapshot's
  automatic thumbnail. Explicit full-message preview retains access to both
  original images and their verified bytes.
- A final native provider response is a valid steering boundary. Merely releasing
  the final HTTP gate can consume the steering input. The target-ended fallback
  test therefore explicitly cancels the held turn before that boundary; it does
  not invent an unconsumed result or bypass runtime settlement.

Browser control evidence contains bounded identities, never full uploaded image
payloads. Each accepted UI promotion has a unique edit ID and the exact root
session ID. Both browser and runtime are owned from setup through joined cleanup.
Optional Electron mode reuses the existing disposable managed-runtime/staged
desktop launcher; its artifact must match the packed web renderer.

## 2026-09-29 — independent prerequisite checkpoints

- `76305cb92`: opt-in queue provider gates plus actual Starlark/QuickJS tests.
  Both passed in 4.398s, including exact edit retry, original content bytes,
  same-turn steering, FIFO order, owner rejection and cancelled-target fallback.
- `9667ad52b`: real browser validation found that queue presentation ignored
  canonical steering intent. The narrow status projection repair preserves
  stale/claimed behavior. Input/identity/queue suites passed 22 tests in 3.72s;
  full shared-app TypeScript checks passed.

The first complete Chromium run passed against renderer
`229f4aa812e464674a3e0046b057d084f55060ad52045c3c657629c03db6e14a`.
Combined browser evidence will be appended after both required modes finish.

## 2026-09-29 — complete Chromium and Firefox gate

Both browsers passed all eight grouped workflows in the final combined run,
exit 0. Each retained four typing samples with zero scroll drift, the exact
provider order `Queue B with images`, `Queue A`, `Queue C`, two distinct UI
steering edit IDs, and zero page/CSP errors across reloads. The runner verified
all production asset hashes before opening each browser. Renderer digest:
`229f4aa812e464674a3e0046b057d084f55060ad52045c3c657629c03db6e14a`.

```sh
npm run pack:web
node --test apps/web/scripts/native-queue-fixture.test.mjs
node apps/web/scripts/composer-queue.mjs
```

Final output is `/tmp/whip-native-queue-final.log`; structured checks,
accessibility snapshots and screenshots are in
`/tmp/whip-native-queue-final-results`. Only `WHIP_QUEUE_RESULTS` selected that
output directory; the default Chromium/Firefox matrix was unchanged. Syntax
and whitespace checks passed. Owned browsers and disposable runtimes joined
cleanup before the next browser probe began.

Optional `WHIP_WEB_BROWSERS=electron` still uses the staged production app and
requires its renderer digest to match. That optional run was not performed in
this checkpoint; neither signing nor notarization evidence is claimed.
