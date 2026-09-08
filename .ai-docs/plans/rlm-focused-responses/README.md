# Focused agent responses and MCP discovery

Status: item 1 implemented in the working tree; item 2 remains planned.
Prepared 2026-09-08 UTC against checkout `a7e1a2e61`.

## Item 1 implementation

Spawn now returns only the five receipt fields below. Ordinary inspection
keeps state, capabilities, and budgets; `include_grants=True` explicitly adds
`mcp_grants` with JSON fields `all` and `selectors`, using the existing
inline-output/content-handle convention. Requested grant read failures are
reported. Authority inheritance and resource limits are unchanged.

The new integration regression reproduced the original output-limit failure
and scratch omission before the fix. With the fix, a 780-tool fixture prints a
119-byte receipt while retaining 112,208 bytes of exact selectors internally.
The test restores the saved child ID after worker suspension, invokes the last
inherited tool, and retrieves/searches the complete grant details. Additional
cases cover root all-tools grants, absent MCP capability, related-agent access,
private content handles, invalid arguments, and grant-chain revocation errors.
Both CI workflows and `task acceptance` run these tests with race detection.

Validation: the new integration tests, the race suite for daemon/MCP/RLM/session
packages, and `task check` pass. The latter
includes all regular Go tests, vet, protocol generation checks, SDK tests, and
web checks/tests. Repository-wide lint with integration tags reports nine
existing findings in unrelated files (including concurrent `host.go` work),
with none in this change's Go files; patch-scoped lint confirms zero issues.
Workflow validation passes with ShellCheck
disabled; full validation reports the same two pre-existing `SC2086` warnings
in `ci.yml` as the checked-in baseline.

## Objective and priorities

Protect model context by returning information needed for the next decision,
while making complete details explicitly retrievable. Keep existing permission
inheritance, consent checks, execution limits, and model budgets.

Implement these two changes in order:

1. Compact agent spawn and inspection responses. This fixes the reproduced
   successful-spawn/failed-response incident.
2. Targeted, bounded MCP discovery. This removes another unbounded response
   path before it produces the same failure during tool discovery.

The [context and limits audit](../../research/rlm-context-and-limits.md) records
the evidence. The affected child inherited 780 exact MCP selectors. Returning
them to its parent produced roughly 111 KB of printed output, exceeding the
64 KiB cell output cap after the child had already started. The returned value
also exceeded the separate scratch checkpoint cap. The MCP catalog is not
automatically injected into the child's prompt; discovery responses can bring
it into context when requested and printed.

## 1. Compact agent receipts; retrieve grant details explicitly

### Resulting behavior

`agents.spawn(...)` returns only the admitted child's identity and lifecycle
receipt: `id`, `name`, `parent_id`, `status`, and `report`. Preserve `status:
"queued"` as the admission acknowledgement; inspection reports current state.
Remove the full MCP selector list, capabilities, and effective budget breakdown
from this default response.

`agents.inspect(id=...)` keeps its existing useful state: identity, current
status, model/provider/effort, working directory, unread message count, report
mode, capabilities, and budgets. It omits the full MCP selector list.

`agents.inspect(id=..., include_grants=True)` additionally returns `mcp_grants`
through the existing inline-output/content-handle convention. Its JSON contains
the stored all-tools flag and exact selectors, so an unrestricted root is not
misrepresented as an empty grant set. Large results have a preview, size, and
handle for `context.inspect/search/read`; they never become an unbounded list
inside the inspection response. These are admitted grants; current availability,
ancestor revocations, and consent still determine whether a call can execute.

### Implementation

- In `internal/daemon/recursive_runtime.go`, shrink the return map after
  successful spawn and remove the now-unnecessary post-admission budget read.
  Do not create a grant-detail artifact during spawn: metadata persistence
  should not introduce another failure after child admission.
- Add the explicit inspection option to the agent dispatcher and inspection
  implementation. Load/encode MCP selectors only when requested. Use the
  existing `boundedText` and content store; any handle belongs to the caller.
  Preserve the current parent/child/sibling access checks. Surface errors
  fetching requested details instead of representing failed reads as no grants.
- Keep `delegatedMCPTools`, persisted selectors, child admission, wakeup,
  narrowing, revocation, and call-time authorization unchanged.
- Update the concise helper instructions in `internal/rlm/prompt.go` and the
  corresponding examples in `docs/tools.md` and `docs/rlm-runtime.md`.

### Acceptance criteria

- A regression test with at least the observed 780 realistic selectors executes
  an actual Starlark cell that assigns and prints the spawn response. It
  succeeds under the current default output and scratch limits, creates exactly
  one child, and restores the child ID after worker eviction/checkpoint restore.
  A direct `host.Call` test alone would miss the original failure.
- Default spawn and inspection output do not grow with the number of MCP
  grants. Full authority remains persisted even though it is absent from the
  response. Existing inheritance, narrowing, and revocation tests continue to
  pass, including a call using a grant near the end of the large fixture.
- Explicit grant inspection returns exact small data inline and exact large
  data through a readable/searchable handle, including middle and final
  selectors. Related-agent access restrictions still apply.

## 2. Search tool summaries; retrieve one schema when needed

### Resulting behavior

Keep the existing server-oriented discovery flow and add focused retrieval:

```python
servers = mcp.list_servers()
matches = mcp.list_tools(server="docs", query="search")
print(matches)
definition = mcp.describe_tool(server="docs", tool="search_documents")
print(definition)
# Inspect/read the definition's content handle if it is large.
# Use the exact schema to construct arguments, then call the tool.
```

- `mcp.list_tools(server=..., query="", cursor=...)` returns an object with
  `items` and `next`. Each item contains the exact tool name, title, and a short
  description preview. It contains no full schema or per-tool fingerprint.
  An omitted query browses that server; a query filters names, titles, and
  descriptions using deterministic, case-insensitive text matching.
- Show tools granted to the requesting agent. A grant still does not imply
  that call-time consent has been satisfied. An unavailable server is an error,
  distinct from an empty match list. An exact lookup of a tool without a grant
  reports that condition explicitly rather than claiming the tool does not exist.
- Pages fit the existing inline response allowance, accounting for the complete
  encoded envelope. A continuation cursor retrieves remaining matches in stable
  name order. Do not introduce a separate arbitrary tool-count ceiling.
- `mcp.describe_tool(server=..., tool=...)` retrieves one complete description
  and exact argument schema using the existing inline-output/content-handle
  convention. A large schema is fully available through context retrieval.
  Discovery must not invoke the tool or request consent to run it.
- Ordinary MCP calls continue to resolve and authorize the current tool
  definition. Discovery results cannot bypass or replace those checks.

### Implementation

- Extend the `list_tools` branch and add `describe_tool` in
  `internal/daemon/recursive_runtime.go`. Reuse existing content storage and
  retrieval for large individual definitions.
- Add narrowly scoped catalog-summary and single-definition reads in
  `internal/mcp/manager.go` / `manager_call.go`, sharing existing connection
  state and locking. Avoid the current `ListTools` path encoding every schema
  merely to return summaries. Keep the existing internal enumeration used by
  authority delegation intact; no new index, cache, or MCP connection is needed.
- Tie page continuation to the server catalog generation and query. Recheck
  grants on each request; a catalog change yields a clear restart-discovery
  result. Ensure cursors advance, and never silently skip an oversized item.
  Description previews may shorten; if an item's essential identity alone
  exceeds the allowance, use an explicit content reference for that item.
- Register `describe_tool` in `internal/rlm/modules.go`. Update module tests,
  prompt help, and the current MCP documentation in the same change. Teach the
  short sequence: choose server, filter summaries, retrieve the needed schema,
  call. Do not add the catalog to the static prompt.

### Acceptance criteria

- Use a large fake MCP catalog, including a relevant tool near the end, long
  descriptions, Unicode/escaped text, and an oversized schema. Filtered lookup
  finds the intended tool without unrelated schemas in model-visible output.
- Unfiltered browsing remains bounded. Following continuations returns every
  eligible match exactly once for an unchanged catalog; empty results, denied
  lookups, unavailable servers, and stale cursors are distinguishable.
- A real RLM execution test prints discovery and description results within
  default cell limits. Handle reads recover exact schema content, including
  middle sections; a scripted provider then calls the intended tool with valid
  arguments. Existing exact-argument and large-call-result tests still pass.
- Revocation between discovery and call still blocks the call. Catalog reload
  invalidates stale pagination and existing definition/consent protections remain
  effective. Record response bytes and schema exposure for the large fixture;
  do not infer task-quality or token savings solely from byte reduction.

## Delivery and compatibility

Deliver as two independently testable changes, starting with compact receipts.
There is no database migration, permission migration, or public SDK/protocol
change required for this plan.

The Starlark response shapes intentionally change: scripts consuming
`effective_mcp_tools` must request grant inspection; scripts iterating the old
`list_tools` array must use `items` and continuation, then describe a tool for
its schema. Source search found no active in-repository consumers of the removed
grant fields beyond their producers; custom or retained Starlark helpers may
need updating. Update examples and runtime help alongside each implementation.
Do not retain an unbounded compatibility response.

During implementation, run targeted regression tests first, then
`go test -race ./internal/daemon ./internal/mcp ./internal/rlm ./internal/session`,
the repository lint checks, and `task check`. Use isolated fake-provider/MCP
fixtures, not the user's running agent or external MCP side effects.

General handling of cell-output overflow, scratch/frame limits, compaction,
restoration, skill catalogs, and budget policy are outside these two changes.
Compact receipts fix the demonstrated oversized-grant failure; they do not
claim to make every possible post-effect output failure recoverable. Evaluate
those broader behaviors separately with evidence of their effect on task
completion and context use.
