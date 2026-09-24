# Browser Design Mode

Branch: `compaction-loop-and-ui-cleanup` (research snapshot; no implementation branch selected)

Status: **Implemented end to end; automated app/renderer/native validation passed. Repository-wide check remains blocked by unrelated worktree formatting; manual release gates are listed below.**
Research date: 2026-09-18.

## Follow-up

The approved [hover-outline motion addition](hover-motion.md) records the narrow
100ms transition contract and its validation gates.

## Goal

Make frontend iteration direct: enter Design Mode in a WHIP Browser tab, hover
and select one or several page elements, describe a change in a floating composer,
and send the visual and structural context to a chosen WHIP conversation.
Match the supplied Cursor Design Mode screenshots closely in geometry and behavior,
while retaining WHIP typography, theme tokens, accessibility, and permission boundaries.

## Confirmed product decisions

The user confirmed these answers on 2026-09-18:

1. **Destination:** show the conversation destination in the floating composer and
   allow changing it. Default to the associated conversation only when unambiguous;
   otherwise ask for a destination. Submit from the floating composer through the
   existing chat submission path, rather than requiring a second send in normal chat.
2. **Scope:** hover, single/multiple element selection, describe, and send only.
   No drawing, dragging/reordering, or direct visual/style editing. Requested code
   changes are still performed by the agent through the ordinary conversation.
3. **Context:** user delegated the choice to our recommendation. Default to one
   viewport screenshot plus bounded structural/style context; make evidence inspectable
   and the screenshot removable before send. Component/source hints are best-effort;
   do not dump arbitrary React fiber props.
4. **Page coverage:** local pages, ordinary remote pages, and existing SSH previews
   inside WHIP's Desktop Browser. Selection does not require React or a dev server.
   This does not promise inaccessible iframe/closed-shadow internals or a new browser
   capability for WHIP's web-only client.

## Remaining implementation defaults / uncertainties

- Debugger coexistence was not explicitly answered: recommend a clear busy state
  while DevTools or agent inspection owns the target for v1; never steal ownership.
  This blocks concurrent inspection, not an ordinary running chat/agent turn.
- Screenshot fidelity does not establish add/remove modifiers or all keyboard rules.
  Confirm a short interaction recording if available; otherwise specify and review
  sensible accessible gestures rather than claiming undocumented Cursor parity.
- Native overlay feasibility passed; implementation uses the measured full-viewport
  transparent trusted sibling and bounded wheel forwarding. See evidence below.

## Non-goals

- Drawing/region annotations, a CSS inspector, direct style mutation, drag/reorder
  editing, or voice input.
- Spawning a new agent per edit by default, or a separate chat/provider pipeline.
- Automatic browser-control grants, new SSH/network grants, or exposing app IPC to pages.
- Guaranteed React component/source recovery on arbitrary production websites.
- Persisting live DOM handles across navigation, reload, renderer loss, or restart.

## Prior art: verified versus unknown

Current official references:

- [Design Mode docs](https://cursor.com/docs/agent/design-mode)
- [Design Mode launch, June 5, 2026](https://cursor.com/blog/design-mode)
- [Multi-selection improvements](https://cursor.com/changelog/design-mode-improvements)
- [Older visual editor, December 11, 2025](https://cursor.com/blog/browser-visual-editor)
- [Cursor data use](https://cursor.com/data-use)

The current docs explicitly say: “Select multiple elements and describe how they
should change together.” Element context comprises “the xpath, the component,
attributes, computed styles, and props from the fiber tree,” plus a screenshot of
“the layout, surrounding elements, and the exact page state.”

That is a semantic description, **not a published wire format**. There is no
verified JSON/XML schema, screenshot crop policy, style allowlist, props depth,
source file/line transport guarantee, or redaction contract. Do not imply that
Cursor necessarily sends whole DOM trees, source snippets, cookies, or network logs.

Drawing annotations sit over a **frozen viewport frame**. They are distinct from
selecting several DOM elements. Current documented shortcuts: Cmd+Shift+D toggle,
Shift+drag area, Cmd+L add element to chat, Option+click add element to input.
Multiselect add/remove modifiers and exact submit semantics remain unverified.
The older visual-editor release includes broader CSS/prop/reorder controls that
should not silently expand this feature's scope.

From the supplied screenshots (visual evidence, not inferred internal behavior):

- Toolbar icon becomes a blue Design pill with a close affordance.
- Hover shows a thin blue element outline and a compact identification/help tooltip.
- Single selection shows an element token and “Describe the change” field nearby.
- Multiple selections have blue/purple/green boundaries, subtle fills, and matching
  composer tokens; the composer appears at the lower right.
- Labels such as PH and LJ are visible; their semantics are unknown. WHIP should
  use meaningful element/component labels, not reproduce unexplained abbreviations.

## Existing architecture and reuse

Canonical ownership is described in `docs/frontend.md` (Desktop Browser section)
and `docs/features.md` (Experimental desktop Browser tabs). Historical plans and
`docs/learnings/browser-use-integration.md` are prior art, not current requirements.

- `packages/app/src/browser-workspace.ts`: browser descriptors, observation and routing.
- `packages/app/src/browser-view.tsx`: human-facing toolbar and native viewport geometry.
- `packages/app/src/browser-types.ts`: optional versioned platform boundary.
- `apps/desktop/src/browser-manager.ts`: native WebContentsView pages, profiles,
  lifecycle, presentation. Guest pages are sandboxed and context-isolated with no
  Node integration or application preload.
- `apps/desktop/src/browser-ipc.ts`, `browser-preload.ts`: validated native bridge.
- `apps/desktop/src/browser-cdp.ts`: scoped debugging transport; currently conflicts
  with concurrent debugger/DevTools ownership and restricts methods and child sessions.
- `packages/app/src/composer.tsx:196-262`, `compositions.ts`: existing draft, upload
  and send lifecycle, submission locking, command IDs, child/root and queue/steer
  behavior, and uncertain-acceptance preservation. Reuse/extract these semantics;
  do not build a second message dispatcher.
- `internal/daemon/attachments.go:110-168`: root/agent-scoped text and image content
  validation. Text attachments are separate content parts specifically excluded from
  authored-input mention/skill expansion. This is the preferred transport for DOM
  evidence; image attachments already validate media and reject non-vision models.
- `packages/app/src/browser-provider.ts`: explicit conversation/page associations,
  separate from authority to control pages.

Critical constraint: page content is a **native child view**, not an iframe or DOM
subtree in the main app renderer. Ordinary React portals cannot paint over it.
Existing application overlays await a native hide acknowledgement before mounting.
Design Mode needs a deliberate exception implemented as a trusted native surface,
not removal of the current overlay safety contract.

## Proposed UX contract

1. Add an accessible icon action beside the address field. Active mode expands to
   the screenshot-inspired Design pill; retain the existing input and toolbar borders.
2. Hover identifies the target without clicking it. Highlight through scroll/resize;
   an intercepted selection click must not activate links, submit forms, or toggle controls.
3. Select multiple distinct elements with stable ordering and color identity. Use
   meaningful tokens, remove affordances, and labels/numbers so color is not the only cue.
   Confirm ordinary-click versus modifier-click rules before finalizing them.
4. Single-selection composer anchors near the element and flips/clamps to viewport
   edges. Multi-selection composer docks lower-right. Preserve prompt text when adding
   or removing elements; permit multiline input and IME without accidental sends.
5. The composer shows the exact destination and attachment/capture state. A page with
   no unambiguous recipient asks for one; never silently sends to the focused session.
6. Send the prompt and immutable capture together through existing submission paths.
   Retain drafts on rejection/disconnect/uncertain acceptance; no automatic duplicate retry.
7. Escape, focus return, keyboard target selection/ancestor navigation, removing tokens,
   closing mode, and opening normal app dialogs receive explicit tested behavior.
8. Navigation/reload invalidates live selections. Do not discard authored prompt text
   silently; require recapture, or explicitly offer a clearly labeled historical snapshot.

## Native feasibility spike — passed

`node apps/desktop/scripts/browser-design-native.mjs` passed on macOS with real
Electron and production BrowserManager. The selected architecture is a **transparent
trusted full-viewport sibling WebContentsView**, drawing hover/selection/composer UI
and accepting inspection input. Only bounded wheel events are forwarded to the guest;
page click/key forwarding is deliberately absent during selection.

Measured checks: OS window capture proves live guest and composer compositing at DPR 2;
real macOS pointer events reach the overlay while the guest button activation count
stays zero; committed Japanese text/focus work; guest capture is unchanged when overlay
pixels change; scroll, 150% guest zoom, exclusive debugger busy, hide-before-present ACK,
and debugger/view teardown pass. Hardware IME composition is not established by the
committed-Unicode check and remains a manual verification item.

The following spike alternatives/criteria are retained as decision history:

Test a small **trusted sibling WebContentsView** above the guest for the floating
composer, with a narrow preload/API and host-owned target geometry. Keep all session,
upload, permission, and send authority out of the untrusted guest.

Compare CDP-assisted picking/highlights with isolated-world DOM picking and temporary
presentation-only overlays. Prefer an existing native/CDP mechanism if it supplies
needed behavior; do not create a second unrestricted debugger client. Any page-world
metadata is untrusted input, even if collected by trusted tooling.

Spike acceptance:

- Native z-order and text focus work without hiding or freezing the live page.
- Pointer interception suppresses page activation while selection is enabled; scrolling
  and clicking the composer work. Transparent overlay regions do not swallow all input.
- Works in split panes, narrow widths, page/app zoom, Retina scaling, window movement.
- Trusted overlay cannot be navigated to page content or impersonated through guest IPC.
- Existing dialogs still hide guests safely; overlay closes on tab/window teardown.
- Define debugger contention and DevTools policy. Verify frame targeting/isolated worlds;
  do not relax the agent-facing CDP allowlist just to implement a human picker.

Proposed initial ownership: one active human inspection lease per window, independent
of agent attachment grants. If DevTools or an agent owns the debugger, show an honest
busy state rather than detaching it. Revisit a main-owned debugger arbiter only if
coexistence is a product requirement. Closed-shadow/OOPIF inner-element coverage is
not guaranteed by current transport: explicitly select/label the frame container or
explain unavailability until inner-frame routing is implemented and tested.

A full transparent native overlay with forwarding of all page input is a higher-risk
alternative, not the default. If neither trusted overlay approach proves reliable,
bring the limitation back for a product decision; do not silently substitute a sidebar.

## Proposed capture contract (WHIP design, not Cursor's schema)

Use versioned bounded data, bound to native epoch/tab generation/document revision,
frame identity and capture time. Live IDs are ephemeral and convey no control grant.
Document generation alone is insufficient: same-document React replacement/HMR can
invalidate nodes. Revalidate actual node connectivity/identity at capture time; never
silently substitute a new node merely because an old selector matches it.

Capture contents:

- Sanitized page URL/title, viewport size, scroll position, zoom/DPR as applicable.
- Ordered element IDs, role/tag/name, bounded visible text, bounds and frame identity.
- XPath/selector hints and small relevant structural context; selectors are hints,
  not durable unique authority or proof of source identity.
- An allowlist of layout/typography/color/spacing computed styles, not every property.
- Optional component hierarchy/source hints when genuinely available. Begin without
  arbitrary React prop dumps; safe primitive props require explicit policy and tests.
- A viewport screenshot captured consistently with the selection metadata. Hide
  composer/inspection overlays while capturing and restore them without losing input;
  encode markers separately or produce a clearly labeled annotated derivative.

Proposed initial bounds for validation: 16 selections, 4 KiB serialized metadata per
selection, 64 KiB total metadata, one viewport image constrained by existing image
upload limits. These are product/implementation proposals, not observed Cursor limits.

Exclude scripts, event-handler source, cookies/storage, hidden form values, password
values and uncontrolled object graphs. Strip URL credentials and apply an explicit
query-string policy. Screenshots can still contain visible secrets: preview them and
state the limitation; redaction cannot guarantee sanitization of every website.

Treat captured page content as **untrusted context**, never system instructions. Keep
user-authored instructions distinct from captured data in both the transcript and model
input. Upload only after intentional attachment/send to an explicit host/root/recipient.
Picking a page does not authorize the agent to later read or interact with that page.

## State and send lifecycle

Keep document-bound selection/draft state transient and owned by the relevant browser
workspace/controller; no second persistent workspace store or Query-cache mirror.
Trusted native overlay renders a bounded projection and sends validated user intents.
Freeze a capture and destination tuple before asynchronous upload. If either changes,
reject/reconfirm instead of silently mixing old image/new DOM or switching recipients.

Use a bounded UTF-8 text evidence attachment plus the existing image attachment for
model input, not page content concatenated into the authored prompt. Existing limits
include 16 files/draft, 20 MiB across drafts, and 256 KiB per text file; our proposed
metadata caps fit inside those ceilings but still count against the existing draft.
For non-vision models, offer an explicit switch or metadata-only send; never silently
discard visual context. Preserve any pre-existing destination draft and attachments:
append/stage by explicit intent, or keep a separate transient design draft, without
clearing either until its own submission is accepted.

A user-visible
structured selection chip may need a versioned presentation shape, but only add a new
protocol/content type if existing attachment and transcript contracts cannot represent
it faithfully. Preserve metadata and image association on replay/resume; never try to
restore stale live DOM references. Handle busy agents through existing delivery choices.

## Ordered implementation slices (implementation pending approval)

- [x] Confirm scope, recipient behavior, capture recommendation, and page coverage.
- [x] Finalize gesture/debugger defaults; user approved end-to-end implementation.
- [x] Native overlay/picking spike; document measured constraints and chosen approach.
- [x] Single-element live picker and toolbar mode, selection suppression and cleanup.
- [x] Multi-selection state, color-linked chips, anchor/dock positioning and keyboard flow.
- [x] Bounded capture, revision checks, screenshot consistency (source enrichment deferred).
- [x] Existing draft/upload/submit integration and transcript/replay representation.
- [x] Existing SSH routing reuse, frames/shadow fallback policy, lifecycle, failure, and security hardening (real SSH smoke remains a release gate).
- [x] Automated renderer parity/appearance checks and canonical documentation.
- [ ] Manual visual review, hardware IME/VoiceOver and packaged local/SSH release smoke.

## Implementation decisions and validation ledger

- Mouse: ordinary click replaces; Shift/Cmd/Ctrl-click adds/removes. Keyboard:
  Tab/arrow cycling and Enter/Space select while the picker surface is focused;
  Shift adds, ArrowUp/Backspace chooses an ancestor, Escape leaves inspection.
  Composer text retains ordinary editing behavior and IME guards; Cmd/Ctrl+Enter sends.
- Human inspection uses the exclusive existing debugger attachment boundary; busy
  DevTools/agent inspection is never detached. Same-document node replacement and
  navigation require reselecting, not silently resolving selector hints again.
- Actual evidence: bounded visible text/role/tag/selected attributes/selector/styles,
  CSS bounds plus zoom/viewport/image transforms. No arbitrary props, whole DOM,
  automatic component/source-map extraction, drawing, or direct editing in this slice.
- Existing theme validators mirror custom palettes and display preferences into the
  isolated renderer. Its html/body remain transparent after theme application.
- Queued/steer submissions use shared `submitChatInput`. Design uses an explicit
  CompositionStore surface key; failed evidence releases its own quota. Uncertain or
  absent commands retain the original ID/content and use existing Check/Retry flows;
  no new retry/discard policy. Normal chat drafts are independent.
- Reviewed and fixed: startup debugger leak/ownership loss, dropped discrete selections
  during observational work, delayed prompt echo (including ABA), failed upload quota
  retention, and missing screenshot coordinate transforms.

Measured 2026-09-18 (final automated results):

| Check | Result |
| --- | --- |
| `npm run check:web` | Passed app typecheck, production multi-entry build and renderer artifact; existing large-chunk warning |
| `npm run check:desktop` | Passed SDK build and desktop typecheck |
| `npm run test:web` | Final rerun: 75 files / 802 tests passed |
| `node apps/web/scripts/browser-design.mjs` | 132 Chromium/Firefox production-CSP checks passed; light/dark, 1280/480/320px and custom palette/font-size/contrast/motion |
| Focused real SDK/CompositionStore Design integration | 12 tests passed, including two-element text/PNG uploads, exact child/root recipient, draft preservation, uncertainty Check and identical-ID Retry |
| Shared chat/composition extraction regression | 52 tests passed |
| Browser toolbar renderer regression | Chromium/Firefox light/dark at 1280/480/320px passed, including existing input/divider treatment |
| Native compositor spike | Passed real OS pixels/click routing, focus/committed Unicode, scrolling, zoom and teardown |
| Production native controller/IPC/renderer fixture | Passed `DESIGN_PRODUCTION_NATIVE_OK`: actual React overlay OS compositing, trusted preload/IPC, two selections, startup failure/timeout/nav/ownership, PNG privacy/geometry at 150% zoom, post-capture mutation rejection and teardown |
| Desktop unit suite | 146 total: 136 passed, 10 environment-gated skips, 0 failed |
| Final focused app regression | 32 passed (13 controller, 7 overlay, 12 real SDK integration) |
| `task check` | Blocked before Go tests by unrelated gofmt failures in `.claude/worktrees/session-trace/internal/daemon/session.go` and `.claude/worktrees/session-trace/internal/session/otlp_export.go`; not modified |

Renderer artifacts are generated outside the repo at `/tmp/whip-browser-design-results`.
Automated screenshot generation/layout assertions do not claim manual visual review.
Remaining release gates: hardware IME composition, VoiceOver, minimum-supported-macOS,
and packaged local/SSH smoke tests. Existing SSH routing is reused, not re-granted by
Design Mode. No Go/protocol changes were required for this feature.

## Tests and release gates

- Pure tests: bounds/anchor collision, stable selection identities, dedup/removal, caps,
  redaction, selectors, stale revision rejection, capture state machine, destination checks.
- App tests: toolbar states, chips, draft preservation, unavailable recipient, queued/busy
  delivery, upload failure/cancel, acceptance uncertainty, replay and recipient switching.
- Native Electron fixtures: no page actions on select; native stacking/focus; safe IPC;
  scroll/zoom/DPR; navigation/reload/hot reload; detached/replaced nodes; tab destruction;
  multiple panes; DevTools/debugger contention; existing overlay hide acknowledgement.
- Frame/shadow coverage: same-origin and cross-origin iframes, open/closed shadow roots;
  unsupported cases have explicit feedback rather than selecting the wrong ancestor.
- Privacy/adversarial: password and hidden inputs, token-like props/URLs, giant DOM/props,
  hostile page scripts/messages, forged sender/tab IDs, screenshot/metadata races.
- End-to-end: select two visually distinct targets, prompt once, verify exact recipient,
  image + bounded context reaching model input, transcript association and safe resume.
- Visual checks: light/dark, 1280/480/320px panes, small/large/offscreen elements, edge
  collisions, multiline composer, high zoom, keyboard focus and reduced motion.
- Run affected typechecks/builds/app/native suites; Go tests/race suites and `task check`
  if backend/protocol paths change. Native validation is required, not just web fixtures.

## Documentation plan

Update `docs/frontend.md` for any new native overlay/state ownership contract,
`docs/desktop.md` for controls, coverage and privacy, `docs/features.md` for behavior →
implementation → tests, and `docs/roadmap.md` for the approved feature/rollout checklist.
Update browser-agent docs only if their contracts change. This plan records the proposal;
canonical docs remain the authority for shipped behavior.

## Follow-up: browser-scoped keyboard toggle (2026-09-18)

Add exact Cmd+Shift+D to toggle through the existing app DesignControl owner.
Guest and trusted overlay `before-input-event` handlers forward the existing
Browser shortcut event; browser toolbar/address DOM handling uses the same route.
No global OS shortcut, daemon authority, or second design-state owner. Reject
repeat, extra modifiers, inactive/stale tab identities, hidden native views and
native-surface modal holds. Preserve drafts and existing start/stop error handling.
Validate app scope/address/modal/inactive/stale cases and native guest plus overlay
composer focus using the production fixture. No installation or app restart.

Validation for this follow-up: 57/57 app Browser/design tests; desktop and app
TypeScript checks; desktop suite 136 pass, 10 environment-dependent skips, 0 fail.
Focused actual Electron shortcut mode (`BROWSER_DESIGN_SHORTCUT_ONLY=1` on
`browser-design-production.mjs`) passes guest interception and trusted composer
focus, exact modifiers/repeats, hidden/unfocused native surfaces, and stale identity.
App regressions include delayed stop/restore-sync with pane/generation changes;
focus returns only to the initiating still-active target. Independent source review
is clear. The full existing native compositor/motion run was not green in this
follow-up: unrelated hover/timing and OS tooltip-overlay sampling gates failed;
those gates remain unchanged, and the focused shortcut run does not replace them.
