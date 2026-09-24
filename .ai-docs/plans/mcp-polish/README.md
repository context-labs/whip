# MCP polish: never stuck, always know why — and the config doctor

Branch: `mcp-support` (continues from the MCP client/server work)
Status: PLANNED — awaiting sign-off; items are deliberately ordered and each
lands as its own commit so we can go one by one.

## What this does

Twelve improvements to the MCP feature, grouped in three tiers. The unifying
theme: **an MCP server should never be able to make whip feel stuck, silent,
or mysterious** — and configuring one should be debuggable without launching
the TUI.

## Goal

Ship tiers in order. Tier 1 is the "never stuck, always know why" pass — the
highest experience-leverage per line. Tier 2 makes the config story airtight.
Tier 3 is bigger swings, each gated on a real need.

## Non-goals

- Full OAuth dance for remote servers (stays out; `headers` + env-resolved
  bearer tokens cover the 80% case — item 10)
- MCP resources/prompts/elicitation (still out; the roadmap tracks them)
- A generic plugin system (item 12 is skill-scoped, deliberately)

## The items

### Tier 1 — never stuck, always know why

**1. Fail-fast MCP calls.** Today a tool call to a `connecting` server blocks
inside the turn up to `startupTimeout` (30s) — the one place the model can
still get parked. Change `server.call` (`internal/mcp/manager.go`): check
status before `<-ready` — failed → instant `Error: mcp server X failed: <err>
(/mcp X reconnect)`; connecting → cap the wait at a short grace (5s) then
return "still connecting — retry in a moment". The turn keeps moving; the
model learns to retry. Files: `manager.go`, tests in `manager_test.go`.

**2. "Did you mean?" on unknown `mcp__*` calls.** A stale/typo'd MCP tool
name currently gets a bare `unknown tool`. Add a Levenshtein pass over the
manager's live tool names: `Error: unknown tool "mcp__doc__greet" — did you
mean "mcp__docs__greet"? (server docs: ready)`. Pure function
(`suggestTool(name, candidates)`), wired in `tools.Execute` via a registry
hook (the manager installs a suggester func on the agent at startup — no
import cycle). Turns a dead turn into a self-correcting one.
Files: `internal/tools/tools.go` (suggester hook), `internal/mcp/manager.go`
(install), unit tests for the distance/suggestion logic.

**3. First-settle transcript note.** Server arrivals are currently invisible
unless you run `/mcp`. On the *first* settle of each server, append one
dimmed transcript line: `⚡ mcp: docs ready (4 tools)` / `✗ mcp: docs failed
— /mcp docs for details`. One shot per server per session (a `seen` set in
the TUI, fed by the existing `mcpStatusMsg`); not a toast — persistent and
cheap, fits whip's transcript-as-truth style. Files: `internal/tui/tui.go`
(mcpStatusMsg case), headless test.

**4. Auto-reconnect with backoff.** A dropped session currently requires
manual `/mcp reconnect`. In the session watcher: on unexpected close, kick a
reconnect with exponential backoff (1s, 2s, 4s; cap 3 attempts), only when
the manager isn't closing and the server isn't disabled. A tool call arriving
during the retry window gets "reconnecting — retry shortly" (composes with
item 1). The gen-guard already makes this race-safe. Manual `/mcp reconnect`
stays as the override. Files: `manager.go` (watcher), tests: disconnect →
auto-recovers without manual intervention; gives up after 3 and stays failed.

**5. Server instructions into the system prompt.** MCP servers ship an
operator manual in the initialize result (`Instructions` — opencode injects
these, `session/system.ts:119-135`, and it materially improves tool *usage*).
Collect `Manager.Instructions()` (name → text, ready servers only, sorted)
and append an `<mcp_instructions><server name="docs">…</server></…>` block to
the turn-time system suffix — the same path skills use (re-rendered every
turn, so late-arriving servers just appear). Files: `manager.go` (capture
`InitializeResult.Instructions` at connect), `cmd/whip/main.go` or TUI
system-prompt assembly, unit test.

### Tier 2 — the config story, airtight

**6. `whip mcp import` — graduate from magic merge to owned config.**
`whip mcp import [--dry-run]`: materializes claude/codex-imported servers
into `~/.whip/config.json` with the merge preview printed. All parsing
exists; the CLI writes through guarded `Config.Save`. `--dry-run` prints the
merged JSONC without writing. Files: `cmd/whip/mcp.go`, tests.

**7. `whip mcp test <name>` — the doctor.** Connect, list tools, print
status + timing + stderr tail + first N tool names. Nobody ships this; today
debugging a server means launching the whole TUI. Also usable in CI to
validate a `.mcp.json` before committing. Reuses `Manager` with
`Start`+status print, exits non-zero on failure. Files: `cmd/whip/mcp.go`,
a `Manager.Probe(ctx, name)` helper, integration test with the in-process
server.

**8. Overlay entries instead of definition copies.** `/mcp disable` on an
imported server currently copies the full definition into whip's config
(correct, but drifts from the source). Add `"overlay": true`: Merge treats
overlay entries as patches (`enabled` only) over the imported definition —
no staleness, and `/mcp enable` after the import file changed picks up the
new definition. Files: `internal/mcp/config.go` (Merge), `config.go`
(MCPServer.Overlay), `internal/tui/mcp.go` (persist overlays), tests for
merge semantics.

### Tier 3 — bigger swings (each gated on a real need; spec when picked up)

**9. `whip mcp serve` project conventions.** `.whip/mcp.json` in a repo
describes the served surface (name, tool allowlist, preamble); default
read-only (bash opt-in). Makes "register whip as a server in claude-code" a
one-line, repo-portable integration.

**10. Codex bearer-token reuse.** Read `bearer_token_env_var` from codex
configs and resolve from env in the importer; `headers` already work. No
OAuth dance.

**11. Live `ToolListChanged`.** SDK has `ToolListChangedHandler`; wire to
`SetMCPTools`. Requires dropping `DisableStandaloneSSE` for remote servers.
Pick up when a real server needs it.

**12. Skill-declared MCP servers.** `SKILL.md` frontmatter gains `mcp:
{command: [...]}`; the skills scanner re-indexes every turn, so project
skills become project tools with zero extra config. Novel composition of two
existing systems — the whip way.

## Design constraints (apply to every item)

- Channels over locks; every new goroutine has an owner and an exit; `-race` clean.
- Errors are tool output, never loop-abort. Bound everything.
- Pure logic at the core (suggest, merge, instructions rendering), I/O at the edges.
- No new dependencies. Items reuse the SDK, stdlib, and existing packages.
- Each item: code + tests + features.md/roadmap touch in the same commit.

## Test plan (per item)

1. Fail-fast: call against failed server returns instantly; connecting server
   respects the short grace; ctx cancel still wins.
2. Suggest: distance table (exact, 1-edit, prefix, no-match), wiring test
   through `tools.Execute`.
3. Note: headless TUI test — one line per server, once (not per settle).
4. Auto-reconnect: kill in-process server session → status recovers without
   `/mcp reconnect`; 3-failures-then-stay-failed with fake clock or short
   backoff constants.
5. Instructions: server with instructions → block in the system prompt;
   server without → absent; sorted by name.
6. Import: dry-run prints, real run materializes, idempotent second run.
7. Test command: exit codes, output contains tool names/timing; bad server →
   actionable stderr line.
8. Overlay: merge semantics (overlay-only entry + changed import → new def
   wins; overlay with no import → inert), disable→edit-import→enable flow.

## Docs plan

`docs/features.md` MCP section grows a bullet per shipped item;
`docs/roadmap.md` MCP block gains checkboxes for each (checked as they land);
README CLI section grows `mcp import`/`mcp test` when those land.

## Addendum: startup resource report (from the live-UX probe)

Shipped: `docs/learnings/other-harnesses/live-ux-probe.md` found that pi's
startup [Skill conflicts] block names broken resources with exact reasons
while whip silently truncated over-long skill descriptions (maxDesc=300).
The report now prints at first paint: `skills: N loaded`, one `⚠` line per
degraded skill (truncation) or unparseable SKILL.md, and `mcp: name ✓ (N
tools) · ghost ✗ · off ○`. Skipped on resume. It immediately exposed ~40 of
this repo's own golang skills with descriptions >300 chars being truncated
in the system prompt — the feature paid for itself on first run.

## Order and status

| # | Item | Tier | Status |
|---|------|------|--------|
| 1 | Fail-fast MCP calls | 1 | ✅ shipped |
| 2 | Did-you-mean suggestions | 1 | ✅ shipped |
| 3 | First-settle note | 1 | ✅ shipped |
| 4 | Auto-reconnect backoff | 1 | ✅ shipped |
| 5 | Server instructions in system prompt | 1 | ✅ shipped |
| 6 | `mcp import` | 2 | ✅ shipped (with import source gating, see `.ai-docs/plans/mcp-import-toggle/`) |
| 7 | `mcp test` doctor | 2 | ✅ shipped |
| 8 | Overlay entries | 2 | closed — `whip mcp import` now writes native, trusted entries; see `../mcp-contracts/PLAN.md` decision 5 |
| 9 | serve project conventions | 3 | parked |
| 10 | codex bearer tokens | 3 | parked |
| 11 | ToolListChanged live re-list | 3 | parked |
| 12 | skill-declared servers | 3 | parked |

Recommended sequencing: 1→2→3 as one commit (the "never stuck, always know
why" pass), 4 next (builds on 1), then 5, then 7 (makes everything after
debuggable), 6+8 together (config semantics change once).
