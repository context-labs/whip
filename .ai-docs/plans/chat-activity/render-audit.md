# DeepSeek chat rendering audit

Inspected 2026-09-09. Session `mvzwbn76ab5jef2ohvnq`, model
`deepseek-v4-flash-0731`, titled “Please kick off some explore agents for the repo!”.
The live SQLite store and SDK snapshot were inspected read-only. No session was
stopped, changed, or replayed into a provider.

## Findings and changes

1. **Large cumulative events lost their identity.** At the final sample, 366
   `stream.tool.call`, 22 completion, seven output, and two start events had
   reference-backed payloads. All 4,386 inline tool events had call IDs. Above
   the 8 KiB inline limit, replay previously emitted only `{content,truncated}`.
   SDK presentation defaulted their unknown owner to the root, while the app
   used each event sequence as a new tool-row identity. This explains both the
   empty cards and the apparent per-token flood, including child updates in the
   root chat. New daemon emissions preserve stream identity and complete small
   fields beside the full event's content handle. SDK/app ignore anonymous legacy
   tool rows while retaining cursor and unavailable-state semantics. Completion
   closes an identified execution even when its result is unavailable.
2. **Accounting displaced useful reconnect presentation.** One captured root
   snapshot contained only ten `stream.accounting` events despite active work.
   Presentation now filters accounting, usage, and unrelated lifecycle events
   before applying the bounded suffix limit. Busy children can still exhaust that
   shared window; omitted-prefix signaling and SDK retention remain necessary.
3. **Copy belonged to individual paragraphs.** Assistant articles reserved an
   action area even while streaming. One response footer now copies retained
   assistant prose across intervening work/deliveries after completion. Queued
   input does not finish a response. Partial retained windows are labeled
   “Copy visible response”; derived copy strings are capped at 256K characters.
   Virtual rows and reading identities remain separate. Empty assistant messages
   and empty streamed text no longer create blank articles.
4. **Some errors are actual model behavior.** The root attempted `files.list`
   and `context.inspect` as top-level tools and received “unknown tool” results.
   Those are not fabricated UI errors. Fallback disclosures retain the tool name
   as it arrives and keep real errors inspectable, using quieter presentation.

## OpenCode reference

The current [session-turn component](https://github.com/anomalyco/opencode/blob/dev/packages/session-ui/src/components/session-turn.tsx)
groups assistant work under the user's input and withholds assistant copy while
the turn is working. The [message-part component](https://github.com/anomalyco/opencode/blob/dev/packages/session-ui/src/components/message-part.tsx)
filters empty text and groups selected adjacent context tools using stable part
identities. WHIP adopts the response-level action placement and stable updates;
its existing SDK execution projection, mailbox semantics, and virtualized
reading rows remain authoritative.

## Validation

- SDK: 275 tests passed, including repeated large child/root payloads, anonymous
  old-daemon references, one-call identity, and unavailable completion semantics.
- App: 29 affected tests passed. Full run: 366 passed, one unrelated workflow
  inventory failure for `provider.list` / `provider.disconnect` added elsewhere.
- Generated protocol schemas, interoperability, and drift checks passed.
- Initial shared app typecheck and production build passed. The final typecheck
  is blocked by a concurrent provider-settings change using `toSorted` outside
  the configured TypeScript library target (`provider-connections.tsx:139`).
  The focused chat tests and final production renderer build still pass.
- Actual production renderer + isolated daemon, Chromium and Firefox:
  streamed large root/child events, legacy payloads, stable activity grouping,
  footer lifecycle, mailbox inspection, keyboard controls, 530/390/320 px layouts,
  touch target sizes, no document overflow or CSP errors. Screenshots inspected.
- Targeted Go tests, including `-race`, passed for both WebSocket and Unix replay,
  full content retention, schema validity, and accounting-resistant snapshot bounds.

Code and renderer are rebuilt in this checkout. Updating the installed daemon
is a separate runtime rollout; this audit did not restart the user's active work.
