# Instant slash-skill filtering

Date: 2026-09-23
Status: implemented; Chromium/Firefox product acceptance passed; native Desktop and visual inspection not performed

## Decision and scope

Keep the existing `workspace.complete` (`kind: skill`) and
`host.skills.complete` RPCs. Increase the skill-result limit rather than adding a
catalog endpoint. Fetch authorized metadata once per composer context and filter
locally as the user types. Both existing-session and New Chat composers share
this behavior. Preserve case-sensitive prefix matching, host ordering, Unicode
names, duplicate precedence, and `$name` insertion.

This follow-up supersedes the original README's per-prefix query, 32-item fetch,
120 ms typing debounce, and no-full-catalog decisions. Its original implementation
and validation record remain historical evidence, not evidence for this change.

## 1. Fetch 1,024; render 32

- Raise the maximum request limit to **1,024 for skills only**, matching the
  existing discovery ceiling. Keep file/path completion limits at 64.
- Request `prefix: ''`, `limit: 1024` from the existing applicable RPC.
- Preserve the response shape and existing per-name, description, and warning
  bounds. Enforce an aggregate serialized completion-response budget of 1 MiB;
  never silently represent a partial result as complete. Return `truncated`
  when output bounds omit metadata. Discovery above its existing 1,024-skill
  ceiling continues to report its existing explicit error, not silent success.
- Return only completion metadata, never skill bodies or filesystem paths.
- Filter canonical reference names using case-sensitive `startsWith(prefix)`;
  do not normalize Unicode or introduce fuzzy matching. Preserve backend order.
- Filter the full fetched collection BEFORE slicing to 32 visible rows. When
  more local matches exist, show that only the first 32 are displayed and suggest
  narrowing the prefix. This display cap does not mean the catalog is incomplete.

## 2. One scope-keyed Query resource, not a query per character

- Retain the existing Query owner; do not add a second cache or persistent store.
- Key metadata by selected runtime/client lifetime and composer owner plus
  root/agent OR planned CWD/definition/effective permission mode, and fetch limit.
  The typed prefix is NOT part of the catalog query key.
- Begin loading when the active composer receives focus and connection,
  capability, project, and effective permission context are ready. If readiness
  arrives while focused, start then. Do not warm every mounted/background tab.
- Keep the completed catalog observed for the current mounted composer context,
  so Escape/reopen does not throw it away. Preserve the existing 10-second
  freshness policy and zero inactive retention; no disk persistence or polling.
- On a new focus/picker-open boundary, reuse fresh data; refresh stale data in
  the background. Merely typing while the picker is open never refetches, even
  when freshness expires. Reconnect invalidates/refetches through existing runtime
  lifecycle behavior.
- Scope/client changes and unmount cancel obsolete reads and release their
  unobserved entries. Never show or select another scope's candidates. During
  disconnect or unresolved authority changes, stop offering cached candidates.
- Escape closes the menu and blocks late results from reopening it. An in-scope
  preload may finish and warm the cache without opening the menu; cancellation
  tests must distinguish dismissal from actual scope disposal.

## 3. Stable, immediate interaction

- Remove the normal-path 120 ms debounce and network-dependent candidate gating.
  After catalog readiness, typing, deletion, and caret-prefix changes synchronously
  compute the visible matches; there is no loading transition per keystroke.
- Keep the current same-scope list visible during routine freshness refreshes.
  Swap in refreshed results without opening a dismissed popup or moving focus.
- For a genuinely cold fetch, show a reserved empty panel and delay the loading
  label by 150 ms. This delays only the indicator, never the request or results.
  Cancel the indicator timer when data arrives, scope changes, or the menu closes.
- Preserve distinct empty, failed, unsupported, folder-required, and disconnected
  states. A refresh failure with cached data must be non-destructive but visible,
  with retry; do not silently represent stale data as freshly verified.
- Preserve native-overlay readiness, textarea focus/selection, highlighted-first
  matching item, keyboard/click insertion, IME protection, repeated-Enter
  suppression, and no-submit behavior while results are pending or empty.

## 4. Compatibility and incomplete results

- Advertise a small additive `skill_catalog_completion` capability indicating
  support for the higher skill limit on the existing completion RPCs. Continue
  requiring the appropriate existing workspace/host completion capability.
  Do not introduce a new endpoint, search mode, or response DTO.
- On older hosts without the new capability, retain the existing bounded
  server-prefix path. Do not optimistically send an invalid 1,024-item request or
  retry arbitrary errors as though they were compatibility failures.
- Treat existing `truncated` conservatively: it can include warning overflow,
  not only omitted candidates. An incomplete response must never produce a
  definitive local no-match result. Fall back to the existing debounced,
  server-filtered 32-result path for that context and retain visible warnings.
  This may be conservative for warning-only truncation, but needs no second
  completeness field and preserves correctness.
- The normal supported, complete-catalog path has zero per-character RPCs.
  Legacy/incomplete fallback is explicitly the exception, not a promise that
  every possible host can provide instant complete matching.
- Authorization remains entirely host-owned. Local metadata filtering grants
  nothing; invocation still re-resolves and authorizes references on send.

## 5. Validation and delivery

Backend/protocol:
- Both RPCs accept 1,024 skills and reject larger limits; file/path bounds stay 64.
- A skill beyond positions 32 and 64 is included and can be found locally.
- Count/byte bounds, truncation/warnings, ordering, Unicode, duplicate winners,
  cancellation, definition/authority isolation, and no-write/no-session behavior.
- Capability negotiation, older-host fallback, generated protocol drift checks.

Frontend (both composers):
- Focus/readiness causes one deduplicated empty-prefix fetch, with no model needed.
- With data ready, `/`, `/p`, `/po`, deletion, and a nonmatch update synchronously
  with no timer advancement and no additional RPCs, including after 10 seconds
  of continued open-menu typing.
- A paused/cold backend cannot block draft editing; pending Enter never submits.
- Full-catalog filtering precedes the 32-row rendering cap.
- Reopen caching, stale-open refresh, non-destructive refresh errors, Escape/late
  completion, context roundtrips, reconnect, and disposal are covered explicitly.
- Legacy/incomplete responses never silently lose matches outside cached results.
- Preserve existing keyboard, pointer, Unicode, IME, and native-overlay tests.

Product validation:
- Trace cold load separately from warm key-to-list update; capture request counts
  in the actual app with a delayed response. Warm typing should update within a
  render frame without loading flashes, not merely pass a mocked network test.
- Run focused suites, frontend types/build/packaging, protocol checks and relevant
  Go tests. Record unrelated blockers explicitly; do not reuse prior validation
  as proof of this follow-up.
- Update `docs/frontend.md` to describe the scope-keyed metadata query, bounded
  local filtering, refresh policy, and compatibility fallback. Update affected
  feature/UI docs and acceptance scripts. Preserve all unrelated dirty work.

## Delivery evidence (2026-09-23)

Implementation and scoped review are complete. The existing endpoints now accept
up to 1,024 skill results, bounded to 1 MiB encoded JSON; mention/path completion
still caps at 64. Both composers preload a negotiated, authorized metadata catalog
and filter locally before the 32-row cap. The SDK requests both host and catalog
capabilities; its real initialize regression covers negotiation.

Fresh validation on the shared worktree (no overlay):

- `npm run test:web`: **1,194 tests / 91 files passed**, including the final
  pending-config/already-focused New Chat regression. Parent log:
  `/tmp/whip-instant-skills-full-web-tests-delivery.log`.
- Focused frontend suite: 86 passed before that final additional regression;
  final Welcome skill suite: 11 passed. App and UI TypeScript checks passed.
- `npm run check -w @whip/sdk`: build and **454 tests passed**.
- `npm run check --workspace=@whip/protocol`: types, 15 interop tests, and
  generated Go/schema/JavaScript drift checks passed. No protocol DTO generation
  was needed for this follow-up.
- `go test -race ./internal/daemon -run
  'Test(SkillCatalogCompletion|HostSkillsComplete|WorkspaceSkill|HostSkillCompletion|HostCompletion|HostFileCompletion|HostPathCompletion)'
  -count=1`: passed after backend final edits (12.941s). Independent reviewer
  also passed focused backend/frontend/SDK initialization tests, inspected final
  code, and reported no remaining blocking findings.
- `npm run pack:web`: passed. Renderer artifact:
  `de15c0689d64fdff78fca95d045b2030d9df6533e8a26c73fb5c5a612201b726`.
- Actual packaged-app acceptance: **Chromium and Firefox each passed all 10
  groups** using a freshly compiled isolated daemon, an actual 100-skill catalog,
  and genuine metadata responses delayed by 1,500 ms. No fabricated completion
  results; turn execution uses the fixture's synthetic runner.

Final browser command:

```sh
WHIP_SLASH_SKILLS_RESULTS=/tmp/whip-instant-skills-verified \
WHIP_WEB_BROWSERS=chromium,firefox node apps/web/scripts/slash-skills.mjs
```

Each of four composer/browser windows made exactly one empty-prefix/limit-1024
focus preload before typing. Across over 11 seconds of warm input in each window,
there were **zero additional completion requests and zero observed loading
flashes**; the expected rows were present at the next animation frame.

| Browser | Composer | Samples | p95 input-to-next-frame | Maximum |
| --- | --- | ---: | ---: | ---: |
| Chromium 153.0.8010.12 | New Chat | 68 | 15.5 ms | 16.2 ms |
| Chromium 153.0.8010.12 | Existing session | 68 | 16.9 ms | 17.6 ms |
| Firefox 155.0 | New Chat | 63 | 18 ms | 19 ms |
| Firefox 155.0 | Existing session | 61 | 17 ms | 21 ms |

These are next-animation-frame observations, not an isolated measurement of
filter computation. Cold results took 1,514–1,526 ms with the intentional 1,500 ms
transport delay. Reload/freshness boundaries still fetch: each full browser
scenario made three host and four workspace completion calls, not one per key.

Acceptance also verified matches beyond index 64, the 32-row display cap, no-
provider discovery, pending Enter safety, exactly one explicit create/send,
root/agent scoping, keyboard/pointer insertion, Escape and repeated Enter,
caret-middle insertion, independent list scrolling, light/dark/narrow DOM
layout, zero page errors, and no provider inference requests.

Evidence: `/tmp/whip-instant-skills-verified/report.json`, browser frame traces,
and 16 PNGs; log `/tmp/whip-instant-skills-verified.log`. Script SHA256:
`c10e8f49c0704d5b70eac45d6307d2ca2d60383c8a2e21fabb5ccc20f93e8ecc`.
Earlier browser failures were isolated to startup-readiness and synthetic/no-op
selection events in the harness. The final script waits on semantic startup
readiness, focuses once, and uses native caret events; the reviewer verified no
weakened expectations, forced popup state, or refocus retry loops. No runtime
changes were needed for those harness corrections.

Limitations: this is shared-web packaged renderer acceptance, **not native
Desktop/WebView acceptance**. Screenshots were generated, but visual inspection
was unavailable because Desktop browser attachment returned `attachment_revoked`;
DOM/layout checks are not a substitute for visual/accessibility approval. No
repository-wide all-Go/task-check claim is made. All owned jobs ended and isolated
fixture processes were cleaned up. Unrelated work was preserved; nothing staged
or committed.

## Not included

No new catalog service, daemon cache, filesystem watcher, polling loop, virtualizer,
search dependency, command palette, or browser filesystem access. Any future
backend discovery optimization requires measurements; it is not needed to remove
network requests from normal typing.
