# Frozen backend compatibility gate

Run from the candidate checkout, using Node 24 and the Go version in `go.mod`:

```sh
node scripts/backend-compat/run.mjs --candidate "$PWD" --base <immediate-parent-commit-sha> --output /tmp/whip-backend-compat
```

Use a new output directory for review evidence. The runner archives the pinned
revision in `baseline.json`, installs its exact dependencies, generates its
protocol, and builds its SDK. It separately regenerates candidate artifacts and
compiles the fixed reference, explicit immediate PR base, and candidate Go
integration binaries. PR CI supplies its actual base SHA; push runs use the
previous commit, with the current commit as the fallback when no previous
revision is available. The baseline SDK is loaded by absolute
path from the baseline archive and is never rebuilt from candidate Go types.
The fixture launcher executes the selected binary directly; it does not use the
SDK test helper that compiles Go from its own repository.

`evidence.json` records source revisions, actual tool versions, binary and SDK
hashes, generated protocol hashes, commands and completed checks. Local
`candidateRevision` identifies HEAD and `candidateDiff` records tracked edits;
stage or commit new Go files before running so added implementation sources are
included in that evidence. CI tests the checked-out commit. The initial local
harness validation contains only scripts/CI edits, so its daemon implementation
is exactly its recorded parent revision. Failed runs
retain logs and isolated homes for diagnosis. These homes contain synthetic test
data; fixture processes receive a minimal environment without inherited provider
credentials. Linux and macOS run this job, and the existing aggregate gate
requires both. All pre-existing SDK, browser, Go, platform and distribution checks
remain required.

The checks are complementary:

- Tracked SDK/protocol/generator/framing/schema source stays identical to the
  fixed revision. Schema and generated TypeScript are ignored by Git, so both
  sides are freshly generated and compared file by file. Built SDK artifacts
  must also match exactly.
- Fixed SDK smoke scenarios run twice against baseline to calibrate and once
  against candidate and immediate PR base, independently for Unix and WebSocket. They compare full
  selected responses/errors, ordered live and replay events, snapshots/history,
  content-backed streams, provider configuration, configuration conflicts,
  retry identity and cursor rejection.
- Lifecycle checks exercise disconnect and waiter abort without cancelling
  accepted work, explicit cancellation, real process crash, queued attachment
  recovery, and no repeated uncertain effects. These check allowed outcomes,
  rather than equating incidental response/notification interleaving or the
  accepted command's transient queued/running status.
- Both fixed and immediate base → candidate → base rollback cover real root and retained child
  checkpoints in Starlark and QuickJS, new state written by candidate, chunked
  content, unchanged schema, SQLite integrity, checkpoint and content hashes,
  complete trace pages and OTLP exports. A baseline-only round trip calibrates
  this fixture first. Trace pages and export documents are compared across
  binaries on identical persisted state, preserving every field except the
  trace page's current server clock.

The smoke normalizer has a narrow list of random identity and clock fields.
It preserves identity relationships, exact cursor/sequence values, omissions,
nulls, empty values, error details, event order/multiplicity and content digests.
A stream larger than the existing supervisor's 32 KiB coalescing threshold
forces two separate fixture fragments; smaller fragments could validly coalesce
and would be a poor exact-event regression oracle. Comparator tests prove that
response, omission, error, cursor, event order/count and identity mutations fail.

This is a cross-cutting regression gate, not exhaustive behavioral equivalence.
It does not replace existing provider encoding/retry tests, detailed trace body
and pagination tests, REPL/mail/budget tests, or product acceptance. Add focused
characterizations before a move when those existing assertions leave a gap.
Provider configuration checks do not contact live model providers. Arbitrary
concurrent schedules are checked by existing invariants, not by sorting events
or accepting a candidate's new expected transcript.

Changing the baseline pin or frozen paths requires an explicit compatibility
decision. Dependency maintenance is separate and does not silently replace the
original SDK oracle. Do not update assertions to make a structural change pass.
