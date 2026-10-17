# Development release after backend reorganization

The reorganization froze development at `271c0f8d2a35648d1b45056d57432590b783483c`
(protocol 6.9). The release PR targets older main
`88764670f1c6e2a57e22463e96cfb0d6021aea33` (protocol 6.8). Strict equality with
that older main also rejects intentional features already present before the
reorganization. The frozen baseline, SDK, generated protocol, database schema,
and runtime behavior are unchanged by this release-readiness repair.

## Historical differences checked for this release

| Origin | Observable change in the deterministic compatibility fixture |
| --- | --- |
| [f62b5eec6](https://github.com/context-labs/whip/commit/f62b5eec628b2d583793aa788dc2eb677941e0f8) — concrete effort vocabulary | Protocol minor 8 → 9; new root session and agent effort resolves from empty to `low`. |
| [54fca9c1a](https://github.com/context-labs/whip/commit/54fca9c1af25fb85bbf581f05ed5a401bcc5fb2f) — title generation | Removes `session.autotitle`; advertises `session_title_notifications`; admission emits one `session.title.updated` event before `turn.started`. This advances subsequent event/trace sequences and cursors by one. The fallback title grows from 64 to 128 characters, and user messages retain their originating turn. |
| [12f0ea076](https://github.com/context-labs/whip/commit/12f0ea076) — native folder creation | Adds `host.directory.create` beside `host.directory.pick`. |
| [b8cc56a48](https://github.com/context-labs/whip/commit/b8cc56a48d781998784fc7ce1cc541bea2cd59fb) — conversation cleanup | An empty compaction model selects the conversation's model/provider rather than a built-in DeepSeek route. Configuration/provider revision hashes change accordingly. Assistant messages retain recording timestamps. |

These differences were reproduced in both Linux and macOS release CI artifacts;
their normalized parent and frozen-reference transcripts match across platforms.
They are expectations for this exact historical release pair, not permission
for further changes to these fields.

`scripts/backend-compat/release-transition.mjs` checks the old values before
constructing the expected release transcript. It requires that transcript to
match both the independently built frozen reference and the candidate, including
all untouched fields, event order, payloads, counts, identities and cursors.
The ordinary immediate-base comparison remains strict for every other base SHA.
Existing fixed-SDK, generated-artifact, lifecycle, schema, checkpoint, trace and
base → candidate → base rollback checks remain enabled.

## Docs deployment prerequisite

The deployment workflow omitted protocol generation after generated files
stopped being tracked. The same prerequisite was already restored in regular
docs CI by [567bdebba](https://github.com/context-labs/whip/commit/567bdebbae9aa21a7a09a7686a979824ea998a51).
Set up Go from `go.mod`, generate the protocol and build the SDK before checking
the published examples. The static site still has no SDK or daemon runtime
dependency.

## Release acceptance

Require full CI, the docs preview deployment, and isolated desktop acceptance
before merging development to main. Exercise REPL/child state and traces through
the compatibility lifecycle/rollback scenarios; verify the shared renderer's
REPL, agent, provider and effort controls; and run the staged desktop lifecycle
smoke against temporary homes. These checks use local fixture providers and
must not consume personal provider credentials or existing sessions.
