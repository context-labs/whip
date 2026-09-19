# Composer queue implementation and validation

Implemented on 2026-09-18 in the existing shared desktop/web renderer. Native
mobile presentation stays unchanged; new controls require both advertised inbox
operations. The developer's main daemon and real conversations were not restarted
or used as test fixtures.

## Delivered

- Active-turn Send/Enter queues normally. The strip above the composer supports
  multiple messages, image-only/mixed attachments, full read-only preview, Steer
  and Remove. The composer shows one primary action: Send for text or attachments,
  otherwise Stop during an active turn.
- One durable inbox retains input identity, original command correlation,
  attachment references, bounded previews and a pending steer's exact target.
  The schema-19-to-20 migration preserves old data and only backfills proven
  root-client provenance.
- Transactional controls never cancel a claimed turn or recreate an input.
  Unconsumed steering intent expires at turn completion/failure/cancellation or
  recovery. Other roots' pending intent remains isolated. Root input commands
  removed while waiting settle as cancelled; completed child admissions stay
  complete.
- SDK snapshot/page reconciliation retains bounded unverified waiting evidence,
  disables stale controls, and retains scoped keys through sending, receipt,
  reload and transcript handoff. Live and reopened stream fragments stay on the
  correct sides of a steered message. Partial snapshots cannot imply delivery.
- Claimed inputs keep attachment thumbnails above the transcript bubble. Scoped
  image reads reserve geometry and clean up object URLs. Long queues virtualize;
  queue mutation leaves current drafts, newer files and reading anchors alone.

## Automated checks

The run logs are under `/tmp/whip-message-queue-*.log`.

| Check | Evidence |
| --- | --- |
| Go session, protocol and daemon suites | `go test ./internal/session ./internal/protocol ./internal/daemon` |
| Queue store/daemon races | `go test -race ./internal/session ./internal/daemon -run 'Queue\|InboxControl\|ClientInbox'`; storage isolation/bounds rerun after final review |
| Real model boundary on both engines | `TestQueuePromotionAtRealModelBoundary/{starlark,quickjs}`; B is injected into the current turn, then A/C execute in original order |
| Protocol generation and drift | `npm run check -w @whip/protocol`: 13 tests plus generated schema/type/validator drift check |
| SDK | `npm run check -w @whip/sdk`: 398 tests, including lost queue-control acknowledgement, exact recipient/turn identity, old-daemon negotiation, partial snapshots, replay and byte accounting |
| SDK live daemon acceptance | `npm run acceptance`: 49 tests over isolated Unix/WebSocket fixtures and real agent runtimes |
| App | `npm run test:web`: 847 tests across 77 suites; final focused queue/composer/projection/submission/timeline run: 66 tests |
| Web/desktop builds | App TypeScript check and production renderer packaging; staged desktop build; `npm run check:desktop` |
| Desktop tests | `npm run test:desktop`, including desktop runtime and distribution/startup checks |
| Native mobile compatibility | `npm run check:mobile`; `npm run test:mobile`: 160 tests across 25 suites |

The first broad app run found stale test fixtures that lacked newly used client
capability/identity and composition-store fields. Those fixtures now represent
the actual runtime contract. The workflow inventory includes the two new commands.
A race-instrumented QuickJS worker startup exceeded the test's deadline under
concurrent build load; its isolated race run and subsequent combined queue race
run passed. No runtime timeout was relaxed.

## Product fixture

Run after `npm run build:desktop` (which also packages the production web renderer):

```sh
WHIP_WEB_BROWSERS=chromium,firefox,electron node apps/web/scripts/composer-queue.mjs
```

`apps/web/scripts/composer-queue.mjs` uses an isolated database and the actual SDK,
protocol, storage, production steer hook and turn commit. The model's output and
its boundary/finish gates are deterministic. Electron runs the staged desktop
app with temporary home/data directories and a separately attached fixture host.

The fixture checks FIFO A/B/C submission, steering B with two images, keyboard
removal, preserved draft/focus, active-turn and settled-history reload, obsolete
steer fallback, queue virtualization, and reading stability. It captures light,
dark, narrow, enlarged-text, increased-contrast and reduced-motion configurations,
accessibility trees, screenshots, videos and a JSON report under
`/tmp/whip-composer-queue-results`.

The existing long-conversation typing probe recorded **0 px maximum scroll drift**
in all four scenarios on each browser: multiline draft with attachment at the
tail, while reading history, maximum-height draft, and a narrow pane.

Keyboard controls, dialog focus return, message-specific accessible names and
screen-reader-facing accessibility trees were exercised. A hands-on VoiceOver
or NVDA audio pass was not performed; the automation is not a substitute for that
manual check. No paid/live model or main-daemon session was used.

## Rollout

The desktop development bundle is staged locally. Connecting it to an older
running daemon intentionally retains the delivery dropdown. The queue strip
activates after attaching to a daemon built with the new inbox controls. This
work does not replace or restart the developer's running daemon automatically.
