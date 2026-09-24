# MCP discovery: search first, describe before calling

Status: implemented 2026-09-16 (items 1–4); live check recorded below. The follow-up the
[MCP contracts plan](../mcp-contracts/PLAN.md) recorded but did not schedule
(roadmap line "MCP progressive discovery"). Decisions from the 2026-09-16
conversation are recorded below; defaults I chose are marked as mine.

## Why

The model discovers MCP tools by pulling a server's whole catalog into a cell
and narrowing it with code it writes on the spot. That was the right first
shape and it does not scale to the servers actually configured. At the
2026-09-13 acceptance run: Catalyst 427 tools, ahrefs 135, chrome-devtools 29,
firecrawl 26, exa 3.

- `mcp.list_tools(server)` returns every tool with its full input schema. A
  cell may print 64 KB (`defaultOutputBytes`); a Catalyst listing with schemas
  is hundreds of kilobytes by estimate, so a naive print truncates and the
  model learns nothing about the tools that did not fit.
- Nothing ranks or filters in the daemon. The model guesses a substring, loops
  over descriptions in Starlark or JavaScript, prints the survivors, and pays
  those tokens on every turn that needs MCP. There is no way to fetch one
  tool's schema without fetching all of them.
- The prompt names no servers, so a turn learns them with a call, and what an
  earlier turn discovered lives only in history that compaction folds away.
- `list_tools` runs one `AuthorizeMCP` store check per tool to mark
  `authorized`; on Catalyst that is 427 checks to answer one question.

This is the pattern Claude Code solves with deferred tools and `ToolSearch`:
the catalog stays out of context until a name is known; a keyword search
returns candidates; selecting one loads its schema.

## What exists and stays

- The daemon holds every server's catalog in memory: eager connect at startup
  loads every `tools/list` page (`listAllTools`), `ListTools` serves it sorted
  by name, and a server's `tools/list_changed` notification refreshes it in the
  background (`refreshCatalog`). Discovery reads that cache; it never touches a
  server.
- Discovery needs no consent; only `mcp.call` does. `list_tools` marks each
  tool `authorized` through `AuthorizeMCP` against the agent's MCP selectors.
  Both rules carry over unchanged.
- Host results above `InlineValueLimit` (8 KB) become content handles through
  `boundedText`; a frame is capped at 1 MiB. Search and describe results fit
  inline by construction.
- Module operations are declared once in `internal/rlm/modules.go` and
  dispatched in `recursiveHost.mcp` (`internal/daemon/recursive_runtime.go`).
  No protocol RPC changes, so no `npm run generate` and no workflow inventory
  row.

## Decisions (2026-09-16)

1. **No server names in the base prompt.** The prompt is message 0; a change
   there invalidates the whole cached prefix. Server sets change on attach,
   enable, disable and import toggles. Discovery is on demand.
2. **Nothing in the trailing notice either, for now.** It would be cache-safe,
   but one `mcp.list_servers()` call is cheap. Revisit only if traces show the
   model wasting a round trip.
3. **Search spans all ready servers by default.** The model usually knows what
   it wants, not which server has it. `server` narrows.
4. **Daemon-side, standard library only.** Catalogs are hundreds of tools; a
   linear scan per call is microseconds. No index, no fuzzy or semantic
   matching, no new dependency.
5. **The web and TUI tool browser is a later batch.** Model-facing first,
   measured on real sessions.

## Design

### 1. `mcp.search(query="...", server="", limit=20)`

Tokens are the whitespace-split, lower-cased query; every token must appear
in the tool's name, title, description, or the top-level property names of its
input schema (`trace_id`, `run_id` and friends are often the best keywords).
Rank: a name hit outranks a title hit outranks a description or property hit;
ties sort by server then name. Empty query is an error. Result:

```
[{"server": "...", "name": "...", "title": "...", "summary": "...", "authorized": true}]
```

`summary` is the description's first line cut at 160 characters with a
trailing `…` when cut (mine). No schemas. `limit` defaults to 20, ceiling 100
(mine). Authorization is checked for the returned matches only, so the store
cost is bounded by `limit`, not by the catalog.

### 2. `mcp.describe(server="...", tool="...")`

One tool, everything `list_tools` returns for it today: `name`, `title`,
`description`, `input_schema`, `authorized`, `definition`, `generation`, plus
`server`. Unknown tool: the error names up to three nearest names from the
same matcher, because `mcp.call` requires exact names and near misses are the
common failure.

### 3. `mcp.list_tools(server="...", offset=0, limit=100, schemas=False)`

Same list shape as today, two changes. Entries omit `input_schema` unless
`schemas=True`; `describe` is the way to read one. `offset` and `limit` window
the name-sorted catalog, default limit 100 (mine); `list_servers` already
reports each server's total, so the list needs no envelope. Authorization is
checked per returned entry, so a window costs at most `limit` store checks.

### 4. Guide text

The `mcp` guide fragment lists the six operations and one rule: search first,
describe before calling, list when browsing a small server. The existing
"discover before calling" fragment keeps its authority sentences.

### 5. Docs

`docs/tools.md` example and table row, `docs/rlm-runtime.md` operation list,
`docs/README.md` MCP section, `docs/features.md` bullet. `docs/roadmap.md`
line 77 splits: the daemon and model part checked, the web and TUI browser
left unchecked.

## Preserved, changed, not built

Preserved: `mcp.call`, its consent model and its result shape; `list_servers`
and `instructions`; eager connect and catalog refresh; the definition's server
list as the authority boundary; a child's MCP snapshot at spawn; the system
prompt, byte for byte.

Changed: `list_tools` entries drop `input_schema` by default and accept a
window. A model that relied on schemas in the listing passes `schemas=True`
or calls `describe`; the guide says so.

Not built: a web or TUI tool browser; fuzzy or semantic search; server names
in any prompt text; per-tool usage annotations.

## Items, in order

1. `internal/mcp/search.go`: `Manager.Search(server, query, limit)` over the
   cached catalogs and a pure `rankTools` the tests drive directly; `Describe`
   with nearest-name suggestions. Unit tests: multi-token AND, rank order,
   property-name hits, limit and ceiling, summary cut marker, empty query,
   unknown tool suggestions, a server that is not ready. About half a day.
2. Host dispatch: `search`, `describe`, and the `list_tools` window and
   `schemas` flag in `recursiveHost.mcp`; operations added to `modules.go`.
   Extend `TestRecursiveHostMCPDiscoveryReportsCurrentAuthority`
   (`internal/daemon/recursive_runtime_host_behavior_test.go`) so the fake
   local server proves `authorized` on search results and describe, and the
   window returns the expected slice. About two hours.
3. Guide fragment and docs. About an hour.
4. Live check on this machine: with Catalyst and ahrefs connected,
   `mcp.search(query="halo run")` returns the Halo tools in under a kilobyte,
   `describe` on one returns its schema, `list_tools(server="ahrefs",
   limit=10)` returns ten light entries. Record the sizes here. Half an hour.

## Live check (2026-09-16)

`WHIP_TEST_REAL_MCP=1 WHIP_HOME=~/.whipcode go test ./internal/mcp/ -run TestRealConfigDiscoverySmoke -v`
against the configured HTTP servers (stdio servers need the daemon's process
manager and were skipped): ahrefs 135 tools, exa 3, executor 7, figma 10,
getleads 47, inference (Catalyst) 428, paper 34; figma_remote, linear and
posthog failed to connect (unauthorized or session errors unrelated to this
work).

| Measure | Result |
| --- | --- |
| `Search("", "halo run", 20)` across all ready servers | 20 matches, 3,445 bytes, 7 ms |
| `Describe("inference", "cancel_halo_run")` | 439 bytes |
| Catalyst full listing | 213,380 bytes with schemas, 87,662 without |
| ahrefs full listing | 102,375 bytes with schemas, 51,434 without |
| getleads full listing | 76,465 bytes with schemas, 17,025 without |

Both large catalogs exceed the 64 KB cell print cap even without schemas, so
the `list_tools` window is load-bearing, not cosmetic; search plus describe
answers the common question in under 4 KB.

## Verification

`go test ./internal/mcp/ ./internal/daemon/ ./internal/rlm/`; golangci-lint on
the three packages; the live check above. No web or protocol changes, so no
`npm` steps.

## Risks

- A search over descriptions misses a tool whose meaning lives only in its
  schema. Property names are in the corpus for that reason; if it still misses,
  add enum values, not a new engine.
- The eval model has learned `list_tools` with schemas. The next eval
  campaign is the measure; the guide change is the lever.
