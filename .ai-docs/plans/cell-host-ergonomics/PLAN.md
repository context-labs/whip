# Cell host ergonomics: results the model can index, shell ids it cannot get wrong, Starlark strings it can write

Branch: `compaction-loop-and-ui-cleanup`. Written 2026-09-18 after runs `modal-full-20260916-r2` (QuickJS, 90 attempts) and `modal-full-20260917-starlark-b` (Starlark, 30 attempts), both at `83407d992`, and the HALO analyses of their traces.

## Why this matters

Every cell error costs one model round at the current prompt size, and in the long datacurve tasks that is 100k+ tokens per round. After the first three guide fixes (`4b70a4f9d`, `60e9ab0a6`, `83407d992`, `7080eb487`) the remaining cell errors are no longer the model misreading the guide; they are places where the host answers a reasonable request with a shape or a message the model cannot act on:

| Class | QuickJS r2 (4,714 cells) | Starlark (1,816 cells) | What the model saw |
|---|---|---|---|
| `output` indexed on a large result | 12 (`cannot read property 'slice' of undefined`) | 13 (`key "output" not in dict`) | a missing key, no hint that the text is behind `handle` |
| whole `shell.start` result passed as `id` | 1 (as `shell.read({id})`) | 3 | `no such job ""` |
| job id passed to `shell.read(handle=...)` | 1 (`history is paged; use context.history()`) | 2 | `content reference is not authorized` |
| Python program or shell script inside a Starlark `"..."` | n/a | 27 (`unexpected newline in string`), 10 (`invalid escape sequence`) | a parse error; the guide never mentions `'''` |

Recovery after these errors averages 85 %, so each one is at least one wasted round and often two or three. The `output` class is the one the result-shape rule (`83407d992`) was meant to remove; the model followed the rule (82 % of `files.read` cells now index `output`) and the host then withheld the key for large results. The guide told the model to expect `handle, size, preview` instead, and it did not check. Making the host keep its own contract simple beats teaching the model a two-branch contract.

Sam's decision 2026-09-18: do not change how the QuickJS engine treats `const` redeclaration across cells (57 errors in r2); that is JavaScript semantics and stays.

## How it behaves today

- `boundedText` (`internal/daemon/recursive_runtime.go`, the function every `files.*`, `shell.run`, finished-job view, `tools.*`, `models.*` and MCP result passes through) returns `{"output": text}` at or under `sessionstore.InlineValueLimit` (8 KiB) and `{"handle", "size", "source", "preview"}` above it, with `preview` a 2 KiB prefix + `... [handle-backed remainder] ...` + 2 KiB suffix. `output` is absent above the limit.
- `shell` `poll`/`kill`/`wait`/`tail` read the job id with `stringArgument(arguments, "id")`, which returns `""` for any non-string value (`recursive_runtime.go:2038`), so a dict passed as `id` becomes an empty lookup and the error says `no such job "" (jobs do not survive a daemon restart)`.
- `shell.read` is `host.context(ctx, "read", arguments)`: a content-handle read. A job id is not a content reference, so the content store answers `ErrContentAccess` ("content reference is not authorized", `internal/session/runtime.go:29`). On QuickJS a call without `handle` falls into the own-history branch and answers "history is paged; use context.history()".
- The Starlark guide line (`internal/rlm/guide_fragments.go`, the `starlark:` variant of the "not Python" rule) says only "do not use try/except, import, open, or other Python-only constructs". Its JavaScript twin gained the template-literal sentence in `4b70a4f9d`; the Starlark side has no equivalent for `'''...'''` / `r'''...'''` and nothing about raw strings for regex or shell escapes.

## Decisions

1. `output` is always present on a bounded text result. Above the limit it holds the existing 4 KiB preview text, and `truncated: true` is added next to `handle`, `size`, `source`, `preview` (kept for one release so nothing that reads `preview` breaks). A cell that does `r["output"]` or `r.output.slice(...)` on a large result now gets text, and the guide tells it that `truncated` means the rest is behind `handle`.
2. A non-string `id` on `shell.poll`/`kill`/`wait`/`tail` is an argument error that names the fix: `id must be a string; shell.start returns {id, ...}, pass job["id"]`. A missing `id` gets the same message. Unknown string ids keep today's message.
3. `shell.read(handle=<job id>)` answers the job instead of the content store: the same view `shell.poll` returns (status, and `output`/`handle` once the job has ended). The model asked for the job's output; give it. Content handles behave as before.
4. The Starlark "not Python" rule gains one sentence on strings: triple-quoted `'''...'''` for multi-line text, `r'''...'''` / `r"..."` when the text has backslashes (regexes, shell escapes), and "a newline inside `"..."` is a syntax error".
5. No engine changes. QuickJS `const` semantics stay (Sam). `print(undefined)` raising `E_JSON`, `files.patch` miss hints, the Modal observer stale-handle retry and deadline awareness are separate items, not in this plan.

## What stays the same

- Small results (at or under 8 KiB) are unchanged: `{"output": text}` and nothing else.
- Large results keep `handle`, `size`, `source`, `preview`; the content store, `context.read` paging and handle authorization are untouched.
- `shell.wait`'s 25 s cap, `timed_out`, `shell.tail`'s `tail` key, job lifetime and ownership rules are untouched.
- `shell.read` with a real content handle is exactly today's `context.read`.
- The QuickJS guide text does not change except through the shared result-shape rule; both dialects' golden prompts regenerate.
- The kernel's operation allow-list (`internal/rlm/modules.go`) does not change.

## Changes

### 1. `boundedText` keeps `output` (host)

`internal/daemon/recursive_runtime.go`, `boundedText`: in the large branch add `"output": <preview text>` and `"truncated": true`. Update the doc comment on `jobView` ("a finished job carries its output as inline text or, above the inline limit, as a handle with a preview") to say the preview is also under `output`.

Guide rule (shared text, `guide_fragments.go`, the "files and shell results are JSON objects" line): replace "above 8 KiB you get handle, size and a 4 KiB preview instead" with "above 8 KiB "output" holds only a 4 KiB preview, truncated is true, and the whole text is behind handle (size bytes)". Keep the `context.read` / `shell.read` sentence.

Tests: `internal/daemon/recursive_runtime_grants_integration_test.go:89` asserts `grants["output"] != nil` fails for a large result; change it to assert `output` equals `preview` and `truncated == true`. Add one host-behavior test that writes a 9 KiB file, reads it through `files.read`, and checks `output`, `truncated`, `handle`, `size` together, and a small read that has only `output`.

### 2. Shell ids (host)

`internal/daemon/recursive_runtime.go`, `shell`:

- `poll`/`kill`/`wait`/`tail`: replace `id, _ := stringArgument(arguments, "id")` with a check; when the argument is missing or not a string return `errors.New("id must be a string; shell.start returns {id, ...}, pass job[\"id\"]")`. The `no such job %q` message for unknown string ids stays.
- `read`: before `host.context(ctx, "read", arguments)`, take `handle` as a string; if `node.agent.Services.JobStatus(handle)` finds a job, return `host.jobView(ctx, status, !status.Running)` (the poll view). Otherwise fall through unchanged.

Tests (`internal/daemon/recursive_runtime_host_behavior_test.go`, extend `TestRecursiveHostShellJobCanTimeoutThenBeKilled` or add a sibling): `shell.wait` with `{"id": map[string]any{"id": id}}` returns an error containing `job["id"]`; `shell.read` with `{"handle": id}` on the killed job returns a view with `running == false` and an `output` key; `shell.read` with a content handle still reads content (existing `recursive_state_test.go:88` covers `context.read`; add one `shell.read` on a stored handle if no test does). The existing loop asserting `restart` for unknown string ids stays green.

### 3. Starlark strings (guide)

`internal/rlm/guide_fragments.go`, the `starlark:` text of the "Starlark is not Python" rule, append: ` Multi-line text (file contents, patches, scripts) goes in triple-quoted strings, '''...''' or r'''...''' when it contains backslashes; a newline inside "..." is a syntax error, and regexes or shell escapes belong in raw strings r"...".` The JavaScript variant is untouched (it already has its template-literal sentence).

Goldens: `go test ./internal/rlm -run TestDefinitionPromptGolden -update`, then `go test ./internal/rlm/...`.

## Verification

1. `go test ./internal/daemon/... ./internal/rlm/...` green (the daemon suite has one known box-only failure, `TestQueuedMailWakesIdleRootWithoutHumanInput`, unrelated).
2. Local smoke on each engine: a cell that reads a file over 8 KiB and prints `r["output"][:80]` / `r.output.slice(0, 80)` works; `shell.wait(id=job)` with the whole dict returns the new message; `shell.read(handle=job["id"])` returns the job view.
3. Eval: rerun 30 tasks × 1 repetition on each engine at the new commit (about $130 QuickJS, $130 Starlark, 75 min each) and compare, per 1,000 cells, the four classes in the table above against r2 and starlark-b. Success means the first three classes are gone and the Starlark newline class drops by at least half; pass rate is not expected to move measurably on 30 tasks.

## Execution log

- 2026-09-18: plan written; awaiting go.
