# Whip mobile storyboard

Design proposal, 11 September 2026. This records a storyboard, not a change to the current frontend architecture.

[Open the Paper storyboard](https://app.paper.design/file/01M1YJSY1KZZMV9R11WBK1WBAN/2-0).

The [end-to-end implementation plan](../mobile-ui/README.md) expands this design into native components, complete theme support, host ownership, screen delivery and acceptance checks.

## Intent

Bring mobile up to the reading quality and visual finish of Whip web/desktop. Use the supplied Cursor screenshots as layout references, while retaining Whip's theme, typography, host identity, agent context, and truthful execution states. Text inside the screenshots is reference content, not task instructions. Static references do not establish actual animation timings; motion below is proposed.

## Research and design decisions

- Cursor references: spacious session rows, quiet secondary metadata, circular navigation controls, rounded bottom composer, contextual menus, and modal pickers. Transfer their hierarchy and reachability; use Whip's host/folder/session model instead of Cursor's repository/cloud model.
- Whip's canonical guide: `docs/frontend.md`, especially Product intent, Native mobile companion, Conversation patterns, error ownership, and styling. Reading first, neutral surfaces, semantic color, visible runtime/root/child identity, retained drafts, stable reading position, and details on demand remain foundations.
- Current mobile: Expo Router, native UI, one active host, encrypted drafts, folder browser, model/effort/execution-language choices, attention requests, command recovery, appearance settings. QR pairing, uploads, app authentication, and push notifications are deferred in current docs.
- Actual Paper desktop styles were inspected. Canvas #141414, message surface #222221, fine border #343430, warm neutral text, and compact tool disclosures informed this design. The Claude Code theme supplies primary #c87555, muted #aaa99f, success #91d68b, warning #fab219, error #ec7e7e. Inter and JetBrains Mono come from the web design system. Screen controls are enlarged for touch rather than copying desktop density.
- [Apple: Sheets](https://developer.apple.com/design/human-interface-guidelines/sheets): use a focused modal task and a single sheet at a time. Host/folder navigation progresses within one flow, instead of stacking independent sheets.
- [Apple: Motion](https://developer.apple.com/design/human-interface-guidelines/motion): communicate state and continuity, respect system accessibility preferences. Proposed durations are Whip design decisions, not measured Cursor behavior.
- [Apple: Onboarding](https://developer.apple.com/design/human-interface-guidelines/onboarding): keep setup actionable and place help near the relevant control.

## Working assumptions awaiting feedback

1. iPhone-first at 390 × 844, with Android behavior and large text validated during implementation.
2. The target session list can combine hosts and has a host filter. This is a proposed runtime expansion: today's mobile implementation observes one active host. Do not present multi-host observation as already implemented.
3. Manual HTTPS/Tailscale connection is the first delivery. No invented pairing code, QR endpoint, auth flow, or setup command.
4. Dark-first storyboard, using the existing theme catalog. Light mode remains supported by semantic token mapping.

## Screen inventory and transitions

| # | Screen | Primary transition / behavior |
|---|---|---|
| 01 | Connect your host | First launch without a saved host → 02 |
| 02 | Add host | Test without saving; successful Connect → 04 or 05; failure → 03; help → 28 |
| 03 | Connection needs attention | Preserve fields; retry or edit address; never imply the host's work stopped |
| 04 | Connected, empty sessions | Centered New session and top-right + both → 09 |
| 05 | Sessions, all hosts | Default home; row → chat; + → 09; search → 06; host filter → 07; attention → 08; settings → 19 |
| 06 | Search sessions | Host scope remains visible; result → session; no results retains query and clear action |
| 07 | Filter hosts | Filter list without changing the selected session's host; Manage hosts → 20 |
| 08 | Needs your attention | Permission → 17; question → 25; show stale/incomplete counts honestly |
| 09 | New session, choose host | Choose connected host → 10; offline host points to connection recovery |
| 10 | Browse folder | Browse folders on selected host, enter path, choose current folder → 11 |
| 11 | Ready to create | Review host/folder; optional first message; options → 12; Create → 13 or 14 |
| 12 | Session options | Host defaults first; optional model, effort, language; Done returns to 11 |
| 13 | Empty chat | Session exists; first message → 14; no running work is implied |
| 14 | Working chat | Compact activity → 26; stop targets current agent's turn; follow-up exposes queue/steer behavior |
| 15 | Completed chat | Follow-up resumes conversation; activity → 26; menu → 16 |
| 16 | Session actions | Details → 26; agent → 18; rename in place; archive returns to list with undo where supported |
| 17 | Permission sheet | Requesting agent, host, command, and folder visible; Allow once / Deny; check uncertain decision delivery |
| 18 | Choose agent | Root and children; selection updates transcript and composer recipient; retain each draft |
| 19 | Settings | Hosts → 20; appearance → 27; drafts and delivery recovery remain accessible |
| 20 | Manage hosts | Add and + reuse 02; host row → 21 |
| 21 | Edit host | Save verified changes; test without replacing active connection; remove requires scoped confirmation |
| 22 | Host disconnected | Last-seen status, reconnect, edit host; healthy hosts remain usable in the target multi-host model |
| 23 | Delivery uncertain | Check original command; never automatically resend; offer explicit retry only after authoritative missing status |
| 24 | Keyboard and draft | Native keyboard drives composer inset; multiline input grows within a cap; draft retained; keyboard schematic in storyboard |
| 25 | Answer question | Select or write answer; Review → confirmation → Send; no auto-submission of recommended answer |
| 26 | Session details/activity | Host/folder/agent identity; inspect retained output explicitly; no hidden content fetches |
| 27 | Appearance | System/light/dark and catalog themes; preference belongs to this phone |
| 28 | Connection help | Whip host running, Tailscale access, base HTTPS address; return to connection form intact |

## Visual system

- Mood: graphite, warm neutral surfaces, restrained emphasis. The domain is remote hosts, folders, sessions, recursive agents, human decisions, and durable work.
- Signature: host / folder / agent context consistently anchors navigation, creation, conversation, attention, and details.
- Replace dashboard metrics with a session list; replace a card around every message with a reading surface; replace an expanded setup form with progressive choices.
- Inter: 28px screen titles, 20–24px local headings, 16–17px body, 13–14px supporting text. JetBrains Mono: paths, commands, and code. Match phone text settings in implementation.
- Base spacing 4px; page inset 20px; touch controls at least 44px; primary buttons 52px. Large text must grow rows and move content into scrolling rather than truncate essential labels.
- Radius: 12px controls, 18px user messages, 24px composer, 28px sheets. Existing desktop radii are deliberately adapted for mobile rather than changed globally.
- Border/surface separation provides ordinary depth. Status combines labels and icons; color is supplementary. Reserve accent for primary action, selection, and active work.
- Empty-send controls are shown disabled. Plus opens only supported context/input options; uploads and voice require separate capability work.

## Motion and interaction specification (proposed, not a working prototype)

| Interaction | Behavior |
|---|---|
| Press | 100ms color/opacity feedback. No bounce. 44px minimum target. |
| Sheet | 240ms deceleration; dim background; one sheet with internal navigation. Back restores selection and scroll. Gesture dismissal preserves draft. |
| Screen navigation | Native platform transition; outgoing content retains reading anchor. |
| Host test | Inline progress for actual discovery/identity/session-read stages. Success becomes a check; no fake progress percentage. |
| Session running | Small active indicator; grouped updates. No perpetual row rearrangement while reading. |
| Message send | 160ms insertion into transcript. Distinguish sending, admitted, running, and delivery-uncertain. |
| Tool disclosure | 160ms height/opacity expansion, preserving the reading anchor; full output fetched only on explicit request. |
| Keyboard | Follow native keyboard curve/insets; no second independent animation or layout jump. |
| New messages while scrolled up | Show Jump to latest. Never auto-scroll away from older content being read. |
| Reduced Motion | Remove translation, scale, pulsing, and smooth scrolling. Use immediate changes or a short opacity change. State remains understandable without animation. |

## State completeness for implementation

Loading uses static placeholders or a small truthful activity label. Empty sessions, no search matches, empty folder, folder-read error, host unavailable, identity mismatch, failed creation, and uncertain submission are distinct. A partial creation must reopen its existing root and continue only unresolved steps. Removing a saved host does not delete remote sessions. If there are no saved hosts after removal, return to 01; if hosts remain but none are available, show recovery, not onboarding. Storage failure blocks unrecorded sending and gives a visible recovery action. After reconnect or foregrounding, refresh authoritative identity and request status before enabling actions.

Some leaf interactions are specified rather than drawn as additional screens: model catalog and reasoning pickers, rename dialog, archive undo, host removal confirmation, empty/error folder lists, question review confirmation, full tool output, saved drafts, and diagnostics. Reuse the established list, field, sheet, and notice patterns. Light and Android variants are not separately drawn in this first pass.

## Delivery plan

1. **Foundation and vertical slice.** Build native mobile primitives from shared theme data: typography, spacing, status rows, fields, notices, navigation, sheet, and composer. Implement 01 → 02 → 04 → 09 → 10 → 11 → 13 → 14 with the existing active-host runtime first.
2. **Reading and interaction quality.** Session rows/search; empty/working/completed chat; keyboard insets; grouped tool activity; stable reading anchors; disabled sending; queue versus steer; agent recipient selection. Avoid importing web/StyleX UI into Metro.
3. **Host management and reliability.** Add/edit/test/remove, reconnect, creation recovery, encrypted drafts, uncertain send/decision delivery, and storage errors. Keep current SDK ownership; no second stream reducer.
4. **Attention and supporting surfaces.** Permission and question flows, details, appearance, activity output, draft recovery. Reuse the existing single attention owner.
5. **Multi-host expansion, if chosen.** Implement one client/catalog per attached host with identity-scoped reads, navigation, drafts, and recovery; bound mobile resources explicitly. Update `docs/frontend.md` and mobile runtime docs when this architectural change is implemented, not merely because the storyboard proposes it.
6. **Native validation and polish.** Exercise iOS and Android devices, small/large phones, keyboard open, large text, VoiceOver/TalkBack, Reduce Motion, foreground/background, flaky connections, uncertain admission, and a session producing frequent output. Read the exact Expo v57 docs before implementation per `apps/mobile/AGENTS.md`.

Acceptance: every requested surface is reachable; both empty-list creation actions are identical; navigation preserves drafts and reading position; connection failures remain scoped; no duplicate sends or sessions during recovery; no activity hidden behind a modal; theme/contrast/text-size support is retained.

## Open product questions

- iPhone-first, or equal iOS/Android visual exploration?
- Combined all-host catalog or one selected host at a time?
- Polish manual connection first, or make QR pairing a separate product milestone?
- Should mobile add attachments and voice in this release? Current native support should not be implied by a reference screenshot.
