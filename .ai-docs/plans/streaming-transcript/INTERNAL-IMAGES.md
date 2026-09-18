# Internal screenshot deliveries

September 18, 2026: fixed the desktop/web transcript treating tool screenshots as
human-authored messages. `timelineRows(..., true)` now distinguishes the provider
role from the existing `authored` metadata on full messages and bounded history
entries. Unauthored deliveries use a collapsed Activity details row. Mailbox
digests keep their existing presentation; native mobile keeps its portable
projection. Human attachments retain the wrapping thumbnail strip.

Internal image bodies are read through the same scoped content reader only
after explicit expansion, including bodies above the former 1 MiB inspection
limit. Closing the disclosure unmounts the reader, cancels pending reads, and
releases its inactive cache. These deliveries do not create user controls or
split assistant response-copy boundaries. No caption matching or guessed tool
association is used, and stored/provider messages are unchanged.

Validation: 744 tests across 71 web suites, app/desktop TypeScript checks and
production desktop build passed. The isolated browser fixture persists an
authored multi-image message followed by small and large unauthored image
deliveries. It verifies collapsed deliveries neither show images nor fetch stored
bodies, then explicitly expands each and verifies the image read. The large
internal body exceeds 1 MiB. The same fixture also retains the existing authored
attachment, retry, keyboard, theme, narrow-layout and reading-anchor checks.

Final browser evidence: [results](evidence/internal-images/results.json).
The slow-transfer fixture starts a fresh view to guarantee a cold content read;
cache disposal itself remains covered by the component tests.

Staged renderer: `d349f0df724c865a20d5d2bcdaf788238be2c7c0951094020178023ee9a43104`.
Desktop output: `apps/desktop/.stage/app`. The running user app was not replaced.
