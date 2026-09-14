# MCP contracts: repair the boundaries, make the surfaces honest

Status: IMPLEMENTED 2026-09-13 on branch `mcp-contracts`, one commit per
work item (A1–A11, B1–B5). Decisions recorded from the September 13
conversation with Sam. The branch was cut from `compaction-loop-and-ui-cleanup`
at `cac044b3b` rather than from `main`, at Sam's request, in the worktree
`.claude/worktrees/mcp-contracts`. See the implementation record and the
acceptance run at the end.

Written against the working tree of `compaction-loop-and-ui-cleanup` at
`cac044b3b`. Line numbers below are from that tree.

Supersedes the improvement sequence in the 2026-09-11 MCP review
([evidence/2026-09-11-review.md](evidence/2026-09-11-review.md)). Continues
[mcp-polish](../mcp-polish/README.md); its item 8 (overlay entries) is closed
by decision 5 below.

## Request and recorded decisions

Take over the September 11 MCP review as an executable plan. Decisions from
the September 13 conversation:

1. **Scope.** Contract repairs plus a truthfulness pass on existing surfaces.
   Progressive discovery (paged listing, lexical search, describe) is a
   separate plan, written when a large-catalog server in the acceptance
   matrix shows the need.
2. **The definition's server list is an authority boundary.** Every manager
   construction path (startup, reload, attach) applies it. A named agent
   never sees servers outside its list.
3. **Eager connect stays.** No lazy connect, no connect permission. The
   repo's `.mcp.json` becomes its own import source (`project`), default
   off, with the same only/exclude filters and blocked-row display as
   `claude` and `codex`. Enabling a source authorizes running its programs at
   session start; the docs say so.
4. **Precedence.** Native config, then the project's `.mcp.json`, then the
   Codex user file, then the global Claude file. Project-local beats
   user-global imports because the project author knows its servers.
5. **`whip mcp import` confers native trust.** Materialized entries are
   written without import provenance and become trusted like hand-written
   entries. The command prints what becomes trusted; `--dry-run` remains the
   preview. A materialized import is native, not a patch, so mcp-polish item
   8 (overlay entries) is closed.
6. **The web attach box goes.** `mcp.attach` becomes an ACP-facing operation:
   additive, definition-filtered, untrusted. The panel points at the CLI for
   adding servers.
7. **Results.** `structuredContent` is always appended after the text. Image,
   audio and binary parts become content handles owned by the calling agent,
   and images are also queued as vision parts for the calling root's next
   turn through the existing screenshot sink. Children get the handle only
   (authority clones do not carry the sink).
8. **Acceptance matrix.** inference (Catalyst), ahrefs, chrome-devtools,
   firecrawl and exa, all reached through `whip mcp import`.

Defaults recorded without a question. Object to any of them and the plan
changes:

9. Plan and evidence live in `.ai-docs/plans/mcp-contracts/`. The review
   write-up and its appendices are copied under `evidence/` because the
   originals live only in a session database and `/tmp`.
10. Remote servers refuse cross-origin redirects with an error; same-origin
    redirects keep configured headers. An MCP endpoint that bounces to
    another origin is misconfigured, not a case to support.
11. Tool listing follows cursors to at most 64 pages inside the startup
    timeout, detects cursor cycles, and publishes the catalog atomically
    after the loop. Refresh uses the same loop.
12. Auto-reconnect makes exactly three attempts (1 s, 2 s, 4 s); a failed
    attempt re-arms the next until the cap; a successful connect or a manual
    reconnect resets the count.
13. The Codex TOML reader ignores tables outside `mcp_servers.*`, including
    `[[array]]` headers and their bodies; malformed content inside an
    `mcp_servers.*` table still errors. Discovery errors reach `/mcp`,
    `whip mcp list|import|test` and the startup report.
14. Secret resolution has one entry point that both transports call at
    spawn/connect, bounded by the caller's context and the existing helper
    timeout.
15. The real-Codex-config smoke test runs only with `WHIP_TEST_REAL_CODEX=1`
    and never logs header values.
16. Web labels name their real scope: enable/disable are session-only
    (`clientMCP` never saves config, `internal/daemon/client_control.go:1251-1256`),
    import toggles persist to host config, reconnect is a request. Controls
    follow status; blocked rows show none.
17. Host permission rules stay read-only in the web panel. `permission.forget`
    deletes root-scoped rules only (`internal/daemon/client_control.go:1173-1178`),
    so host-rule revocation needs a protocol operation and is a recorded
    follow-up, not part of this plan.
18. `whip mcp serve` gets one documented sentence: it cannot obtain new
    consent; saved rules still apply. No behavior change.
19. Parked until a named need: OAuth, account/credential ownership, an
    integration abstraction, generated TypeScript for tools, aggregate
    outward MCP, MCP resources/prompts, ToolListChanged live re-list for
    stdio.

## Why this matters

- **The documented happy path is broken for managed processes.** The README
  example passes `"env": { "API_KEY": "$DOCS_KEY" }`. `Manager.defaultTransport`
  copies `cfg.Env` into the process manager verbatim
  (`internal/mcp/manager.go:386-433`) while only the fallback path resolves
  references (`manager.go:1081`). Every daemon-hosted stdio server with an
  env reference starts with the literal string.
- **Configured bearer tokens follow redirects to other origins.**
  `headerTransport.RoundTrip` sets headers on every request after Go's
  redirect logic has already decided to strip `Authorization`
  (`manager.go:1111-1116`); no `CheckRedirect` exists.
- **The SDK collapses an empty server allowlist to "all servers".** `list([])`
  returns `null` (`packages/sdk/src/agents.ts:230,263`) while the daemon treats
  `null` as every host server and `[]` as none
  (`internal/agentdef/document.go:46-55`).
- **Attach ignores the definition.** Startup filters discovery by
  `definition.MCP.Servers` (`cmd/whip/daemon.go:181-195`); `attachMCP` rebuilds
  the manager from attachments plus all native config
  (`internal/daemon/mcp.go:47-58`). One authorized attach widens a restricted
  agent.
- **Only the first `tools/list` page is loaded** (`manager.go:526`). Catalyst
  advertises several hundred tools; anything past page one does not exist to
  the model.
- **Auto-reconnect gives up after one failed redial.** `kickAutoReconnect` is
  called from the disconnect watcher (`manager.go:596`) but a failed connect
  attempt ends in the failure tail (`manager.go:604-622`) without re-arming.
- **One unrelated `[[table]]` in `~/.codex/config.toml` fails all Codex
  discovery** (`internal/mcp/codextoml.go:244-247`), and `Filtered.Errs` is
  read only by `whip mcp list` and ACP (`cmd/whip/mcp.go:84`,
  `cmd/whip/acp.go:185`), so the daemon shows "no tools" for "failed to parse".
- **`!cmd` secret helpers ignore cancellation.** They run under
  `context.Background()` (`internal/config/secret.go:64-70`); a cancelled
  connect keeps running the helper.
- **Results are lossy.** `flattenResult` drops `structuredContent` whenever text
  exists and reduces images to placeholders (`manager.go:774-816`). Ahrefs
  tools return text plus a render payload; chrome-devtools returns
  screenshots. Both are lost.
- **A user-global Codex entry silently overrides a repo's `.mcp.json`** of the
  same name (`internal/mcp/config.go:175-182`).
- **The web panel misleads.** "Available tools" lists built-in schemas under
  the MCP heading (`packages/app/src/details/integrations.tsx:274-284`),
  Reconnect/Enable/Disable appear on blocked rows (`integrations.tsx:70-84`),
  and one generic success message covers three different scopes.
- **The TUI palette builds server rows from local `cfg.MCPServers`**
  (`internal/tui/client.go:1758-1766`), so imported, attached and
  remote-daemon servers appear in status but not in the palette.

## Target state

- One pure selection step, `mcp.Select(discovery, allowed)`, applied by the
  only two manager builders: the daemon factory and additive attach. Startup,
  reload and attach cannot disagree.
- Secrets resolve once, at the point of use, for both transports, under the
  caller's deadline.
- Remote credentials never leave their origin.
- The catalog the model sees is the whole catalog, bounded.
- Reconnect does what its comment says.
- Discovery errors are visible wherever status is visible.
- Precedence: native, project, codex, claude-global. Three import sources,
  each gated.
- Import is the trust path. Attach is additive and untrusted.
- `mcp.call` returns text plus structured JSON; images arrive as handles and,
  for the root, as vision parts next turn.
- Every web and TUI control names its subject and its real scope, and only
  appears when the daemon can honor it.

## Preserved, changed, not built

Preserved:

- Every model-facing operation and its return shape. `mcp.call` output stays a
  string; structured JSON is appended and image handles are described in the
  text.
- Native trust semantics, definition-bound consent, generation guards,
  per-server serialization, no replay of transmitted calls.
- `whip mcp add|remove|list|test|serve`, the `/mcp` commands,
  `mcp.import.configure` for the existing sources, `cmd/whip/acp.go:126`.
- Existing tests in `internal/mcp`, `internal/tools`, `internal/daemon`,
  `internal/session`, `internal/acp`, `packages/sdk`, `packages/app`.

Changed:

- Merge order; a third import source; import writes native entries; attach is
  additive; result flattening; status gains `blocked`; web panel copy and
  controls; TUI palette source.

Not built:

- Everything in decision 19, discovery and search, a connect permission,
  host-rule revocation, an MCP setup wizard, web add/edit/remove.

## Work items

Each item is one commit with a regression test that fails before the change
and the `features.md`/`tools.md`/README touch in the same commit. Order
matters where noted.

### A. Repairs

**A1. Gate the real-config smoke test.** `internal/mcp/realconfig_smoke_test.go`
skips unless `WHIP_TEST_REAL_CODEX=1`; the `t.Logf` of headers goes. First, so
the suite stops reading developer files.

**A2. One secret resolution step for both transports.** Add
`resolveConnectSecrets(ctx, cfg) (env, headers map[string]string, err)` in
`internal/mcp`, calling `config.ResolveEnvMap` and `config.ResolveHeader`.
`Manager.defaultTransport` (managed) and the package-level `defaultTransport`
(fallback) both call it. `config.ResolveSecret` gains a context-taking form so
`!cmd` runs under the caller deadline capped by `SecretCmdTimeout`; the
existing signature wraps it with `context.Background()` for the provider
caller (`internal/config/providers.go:321`). Tests: the managed transport
receives the resolved value (extend `manager_checked_test.go` with a fake
process manager); an already-cancelled context returns before a one-second
helper finishes. Review findings 4 and 8.

**A3. Bind remote credentials to their origin.** The remote `http.Client` gets
a `CheckRedirect` that errors when scheme or host differ from the configured
endpoint; `headerTransport` stays. Tests: the review's two-server redirect
fixture (127.0.0.1 to localhost) fails the connect with an origin error and
the second server sees no Authorization header; a same-origin redirect still
succeeds with headers. Finding 1.

**A4. Preserve the SDK's empty server list.** `packages/sdk/src/agents.ts:230`
sends `input.mcp?.servers === undefined ? null : [...input.mcp.servers]`.
Leave `list()` alone for the other fields, where nil and empty are equal.
Test: `defineAgent({ mcp: { servers: [] } })` serializes `[]`. Finding 2.

**A5. Load every tool page.** In `connect` and refresh, loop `ListTools` on
`NextCursor` with a seen-cursor set, at most 64 pages, under the startup
timeout context; build the catalog in a local slice and publish once. Tests:
in-process server with three pages lists all tools; a cursor cycle terminates
with an error naming the cursor. Pagination row.

**A6. Re-arm reconnect until the cap.** In the connect failure tail, when
`autoTries > 0` and the failure is neither stale nor closing, call
`kickAutoReconnect`; `autoTries` resets on success (already at
`manager.go:572`) and on manual `Reconnect`. Tests: a server that fails twice
then succeeds recovers without manual action; a server that always fails ends
`failed` after exactly three attempts (`WHIP_TEST_MCP_BACKOFF_MS=0`).
Reconnect row.

**A7. Isolate Codex parsing and surface discovery errors.** `parseTOMLTables`
skips any table header not under `mcp_servers` (including `[[...]]`) and its
body lines until the next header; errors remain for bad content inside
`mcp_servers.*`. `Filtered.Errs` reaches `mcp.status` (one synthetic row per
failed source: status `failed`, source path, error), the startup resource
report, and `whip mcp import` output. Tests: `[[unrelated.entries]]` beside a
valid server parses the server; a malformed `mcp_servers.x` line still
errors; a discovery error appears in `/mcp` status. TOML row and the
diagnostics gap.

**A8. Precedence and the project source.** `Merge(whip, project, codex,
claudeGlobal)` in that priority. `config.MCPImport` gains
`Project *MCPImportSource`; `ImportPolicy` gains `Project`, disabled when
absent. For a nil `mcpImport` block, claude and codex keep today's import-all
behavior and project is off; this is the one behavior change for legacy
configs and the README says so. Enumerations of `claude|codex` extend to
`project` at `internal/daemon/client_control.go:1270-1302`,
`internal/tui/client_parameters.go:87`, the TUI palette rows,
`packages/app/src/details/integrations.tsx:94`, the setup wizard question,
and `protocol.MCPImportParams`/`MCPImportStatusResult` (regenerate schemas).
Blocked project servers carry the note "blocked by mcpImport config
(project)". Tests: merge precedence table; project source off by default
shows `.mcp.json` servers as blocked; toggling on admits them. Docs: README
precedence list, tools.md trust paragraph, features.md.

**A9. Import confers trust.** `mcpImportCLI` (`cmd/whip/mcp.go:280-324`) writes
entries with `Origin` and `Source` empty and prints
`imported N server(s); they are now trusted like hand-written entries` with
the names. `--dry-run` shows the same wording. Blocked and already-native
names are still skipped. Tests: after import, `FromConfigMap`
(`internal/mcp/config.go:405`) marks the entry trusted; a second run is
idempotent. Docs: tools.md replaces "including when saved by the import
command" with the new rule; mcp-polish README marks item 8 closed.

**A10. One selection step; additive, filtered attach.** `mcp.Select(f Filtered,
allowed []string) Filtered` (nil allowed leaves f unchanged) replaces the
inline loops at `cmd/whip/daemon.go:181-195`. `attachMCP` becomes: normalize
attachments as untrusted, `Select` by the session's definition, then
`manager.AddServers` on the live manager (build one through the factory path
when none exists). It never re-reads native config and never replaces a
running manager. `MCPAttachParams` docs say additive. Tests: the review's
fixture (definition restricted to `local`, native `admin` present, attach
`{}`) no longer exposes `admin`; a second attach keeps the first attachment;
an attachment outside an explicit list is recorded as blocked with a note.
Finding 3. After A8 so `Select` sees the final `Filtered` shape.

**A11. Results: structured always, images as handles.** `flattenResult`
appends `structuredContent` JSON after text whenever present and also returns
the binary parts (`ImageContent`, `AudioContent`, blob resources) as
`[]Attachment{MIME, Data}`. `Services.InvokeMCP` stores each through a small
hook the daemon installs, `SetMCPAttachmentStore(func(ctx, mime string, data
[]byte) (handle string, err error))`, backed by `Session.StoreContent`
(`internal/daemon/rlm.go:260`) with the caller's agent id; it replaces the
placeholder line with `[image N: image/png, 48213 bytes; handle <id>]` and,
when the screenshot sink is set (`internal/tools/tools.go:188-206`), forwards
image parts through it with the same normalization as `screenshotParts`.
Children have no sink and get the handle. Tests: pure flatten table (text
plus structured; an image part yields an attachment); `InvokeMCP` with a fake
store records one handle per image and the text names it; the sink receives
normalized parts only on root services. Docs: tools.md and rlm-runtime.md MCP
sections.

### B. Truthfulness

**B1. Status carries `blocked`.** `mcp.Status` gains `StatusBlocked`;
`SetBlocked` uses it; the wire value is `"blocked"` (schema regen). `/mcp` and
`whip mcp list` print it. Test: a policy-filtered server reports `blocked`.

**B2. Web MCP panel.** Remove the "Attach session MCP servers" section and its
state. Rename the tools section to "Built-in tools" with the description
"Public schemas of whip's own tools; MCP tools are counted per server above."
Controls by status: blocked shows none plus the note; disabled shows Enable;
connecting shows Disable; ready and failed show Reconnect and Disable.
Accessible names carry the subject ("Reconnect docs"). Labels: "Disable for
this session", "Enable for this session"; the import section is titled "Host
import defaults" and lists the third source. Success copy: "Reconnect
requested", "Disabled for this session", "Saved to host configuration".
Tests: a `packages/app` panel test for the control matrix and names; the
existing shared inspector tests stay green.

**B3. TUI palette from daemon inventory.** `openThinMCPPalette` lists rows
from the last `mcp.status` result (name plus status) instead of
`m.cfg.MCPServers`; a blocked server gets one "Why is <name> blocked" row that
runs `/mcp status`. Add the project import toggle rows. Test: headless palette
test with a status containing an imported server and a blocked one.

**B4. Permission scope wording.** Where the web renders a remembered MCP rule
(`packages/app/src/details/session-controls.tsx` rules list and the "always"
summary in `requests.tsx`), show "server docs, tool search, this exact
definition" with the digest under a disclosure. The TUI phrase at
`internal/tui/permission.go:60-79` is the source wording. Test: rendering test
with an MCP rule fixture.

**B5. Docs.** README MCP section: precedence, three sources, import trust,
attach is ACP-facing and additive, eager connect runs programs, serve cannot
obtain new consent. `docs/tools.md` and `docs/rlm-runtime.md` MCP sections
match. `docs/features.md` gains one bullet per item. `docs/roadmap.md` checks
the MCP reconnection coverage line when A6 lands and adds an unchecked
"discovery and search" line pointing at the follow-up plan.

## Acceptance matrix

Run after A11 and again after B3 on the `mcp-contracts` build, from a session
whose cwd is this repo, with the Codex and Claude sources materialized by
`whip mcp import` and the project source left off. Record results in
`evidence/acceptance-<date>.md`.

| Server | Transport | Exercises | Pass when |
| --- | --- | --- | --- |
| inference (Catalyst) | HTTP, header auth | A5 pagination, A3 origin binding, Codex per-tool tables skipped | `mcp.list_tools` count equals the server's own total; a call to a tool from the last page succeeds |
| ahrefs | HTTP, header auth | A11 structured content, large catalog | a result with `render_with` metadata shows text and the JSON payload in one call output |
| chrome-devtools | stdio | A2 managed env, A11 images, Codex `tools.*` subtables | a screenshot tool yields a handle in the call text and an image part on the root's next turn |
| firecrawl | stdio, Claude file, env reference | A2, A8 precedence, A9 trust | starts with the resolved key; trusted after import; no consent prompt |
| exa | HTTP, Claude file | A8, A9 | same as firecrawl over HTTP |
| any stdio server | | A6 reconnect | kill its process: status returns to ready without `/mcp reconnect` |
| any HTTP server | | A6 reconnect | stop it for good: status ends `failed` after exactly three attempts |
| Codex file | | A7 diagnostics | add `[[scratch.entry]]`: servers still load and `/mcp` shows no error; set `command = 5` in one server table: `/mcp` shows that one failed source row |

## Test commands that must stay green

```bash
go test -race ./internal/mcp/... ./internal/tools/... ./internal/daemon/... ./internal/session/... ./internal/acp/... ./cmd/whip/...
```

```bash
task check
```

```bash
npm test && npm run test:web
```

## Evidence

- [2026-09-11-review.md](evidence/2026-09-11-review.md): the write-up this
  plan supersedes, exported from session `7ajg76plfgz377fcvm7a`.
- [2026-09-11-review-appendices.md](evidence/2026-09-11-review-appendices.md):
  the sub-agent research dossiers with reproduction commands.
- `acceptance-<date>.md`: matrix runs, added as they happen.

## Follow-ups recorded, not scheduled

- Discovery plan: paged per-server listing, lexical search, describe, built
  once in the daemon and reused by model, web and TUI.
- Host-rule revocation: a protocol operation and a web control.
- Web add/edit/remove for native servers, if the CLI proves insufficient.
- Decision 19 items.

## Implementation record (2026-09-13)

Every item landed as planned with a regression test in the same commit. Test
gates run at the end, all green: `go test -race` over `internal/mcp`,
`internal/tools`, `internal/daemon`, `internal/session`, `internal/acp`,
`cmd/whip`, `internal/tui`, `internal/config`, `internal/protocol`,
`internal/capability`, `internal/agentdef`; `gofmt -s`, `go vet ./...`,
`whipvet`; `npm test` (SDK, 323 tests); `npm run test:web` (60 files, 641
tests); `npm run check -w @whip/protocol` (regenerated schemas, no drift).

| Item | Commit | Deviation from the plan as written |
| --- | --- | --- |
| A1 smoke test gate | `Gate the real Codex config smoke test` | none |
| A2 one secret step | `Resolve MCP secrets once, at the point of use` | `ResolveSecretContext`, `ResolveEnvMapContext`, `ResolveHeaderContext` added; the old names wrap them |
| A3 origin-bound credentials | `Keep remote MCP credentials on their configured origin` | none |
| A4 SDK empty list | `Preserve an explicit empty MCP server list` | none |
| A5 paged discovery | `Load every tools/list page` | `toolLister` interface so the loop is unit-tested with a paged fake; live test uses the SDK server's `PageSize` |
| A6 reconnect chain | `Re-arm MCP auto-reconnect after a failed redial` | the give-up test now asserts the exact attempt count |
| A7 Codex isolation, source errors | `Isolate Codex MCP parsing and show unreadable sources` | source errors are `SourceErrors()` rows on the manager, not synthetic entries in `Blocked()`; `[[array]]` tables *under* `mcp_servers` still error (an existing test required it) |
| A8 project source, precedence | `Give the project .mcp.json its own import source` | blocked notes name their source, e.g. `blocked by mcpImport config (project)`; the client-control test fake gained `SourceErrors` here |
| A9 import confers trust | `Make whip mcp import write native, trusted entries` | dry-run prints the trust note before the JSON fragment (a test parses the fragment) |
| A10 selection step, additive attach | `Apply the definition's MCP server list everywhere and make attach additive` | re-attaching a non-native name replaces that entry (ACP re-attaches on resume); a native name is refused as a blocked row |
| A11 results | `Keep structured MCP content and store binary result parts as handles` | `CallChecked` returns `tools.MCPResult`; the store is bound per agent in `AgentSession.bind` and reads the agent id at call time |
| B1 statuses | `Report blocked and unreadable MCP rows with their own status values` | two values, `blocked` and `unreadable`, so clients can tell a row they can act on from one they cannot |
| B2 web panel | `Make the web MCP panel honest about scope and state` | none |
| B3 TUI palette | `Build the TUI MCP palette from the daemon's status inventory` | an empty inventory offers one row that loads status; rows appear after the first `/mcp status` |
| B4 permission wording | `Explain remembered MCP permission rules in words in the web app` | none |
| B5 docs | `Document the MCP contracts` | roadmap gained a checked line for this work and an unchecked discovery follow-up |

Out of scope and left alone, as decided: discovery/search, host-rule
revocation, web add/edit/remove, OAuth, accounts, generated TypeScript,
aggregate serve.

## Acceptance run (2026-09-13)

Recorded in [evidence/acceptance-2026-09-13.md](evidence/acceptance-2026-09-13.md).
