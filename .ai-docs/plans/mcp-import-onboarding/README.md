# MCP import onboarding: pick the servers other agents already have, make them native

Branch: `mcp-import-onboarding` (cut from `compaction-loop-and-ui-cleanup`, worktree like `mcp-contracts`)
Status: SHIPPED on branch `mcp-import-onboarding` (2026-09-15), one commit per
task; see "Implementation record" at the end. Design frame: Paper board
`WHIPCODE`, "New session · 14 · Import MCP servers · Done last" (flat list,
importable servers A→Z, already-native servers A→Z at the bottom). Frames
10–13 are the rejected variants and stay on the board for reference.

## Why this matters

Whip already knows about the MCP servers a person configured for Codex,
Claude, and (after this plan) OpenCode: `mcp.LoadMergedFiltered` reads those
files on every daemon start (`internal/mcp/config.go:319-407`). What the web
and desktop app cannot do today is turn that knowledge into servers Whip
trusts:

- The only trust path is the CLI. `whip mcp import` materializes discovered
  servers into the native `mcp` block (`cmd/whip/mcp.go:277-334`); nothing in
  the web app writes that block. A whipcode user who never opens a terminal
  cannot get a trusted server at all.
- The CLI is all-or-nothing per source and obeys the `mcpImport` gates, and
  whipcode ships with those gates off. The visible result was the "Howdy"
  session: every server `blocked`, 0 tools, and no control that leads anywhere.
- Even with the gates on, runtime imports stay untrusted: consent on every
  call, and an "always" rule bound to one tool definition. That is the right
  default for a file another program wrote, but it means the common case (I
  set these servers up myself last month in Codex) is stuck in the cautious
  path forever unless the person finds the CLI.
- Per-server choice does not exist. The `node_repl` surprise (the ChatGPT app
  writing into `~/.codex/config.toml`) is why `exclude` lists were added; a
  screen where you tick what you want is the same control without editing
  JSON.

The screen is one decision surface: here is what we found, tick what you want,
it becomes Whip's own configuration. Everything below keeps the existing trust
model exactly; it only adds a second door to the state `whip mcp import`
already produces.

## Decisions (Sam, 2026-09-15)

1. **Entry points.** Shown automatically on the New session screen the first
   time a host reports at least one importable server and the host has not
   been offered before; reachable afterwards from Settings › Configuration
   ("Import servers from other agents…"). Same component in both places.
2. **Skip for now** sets one per-host flag (`mcpImport.offered`) in that host's
   Whip config. New servers appearing later in Codex or Claude do not re-open
   the screen; Settings is the way back.
3. **Row states are config-derived only.** Whip never connects to, or launches,
   a server the person has not imported. States: already in Whip, off in its
   source, excluded by your rules, and (new) unsupported sign-in when the
   source itself says the server uses OAuth. Connection problems appear after
   import in the existing status rows.
4. **OpenCode is a fourth source** (`~/.config/opencode/{config,opencode}.json[c]`),
   lowest precedence, with its own gate like the others.

Defaults chosen by the plan, override on sign-off:

5. Default selection: every importable server is checked, except servers
   turned off in their source and servers excluded by your rules, which start
   unchecked. Already-native servers are not selectable. A checked server is
   imported enabled, even if its source had it off (checking it is the
   choice).
6. The screen ignores the per-source `enabled` gates when listing candidates
   (the gates govern runtime imports; this screen is the explicit path) but
   honours explicit `only`/`exclude` lists as the "Excluded by your rules ·
   Include" state.
7. Import writes native entries exactly as the CLI does (provenance stripped,
   so they load trusted) and sets `mcpImport.offered`. Sessions already
   running are not touched; the next session on that host starts with the new
   servers. This matches the CLI today and avoids a cross-session reload
   fan-out.
8. No brand icons in this plan (Sam, 2026-09-15): every row gets a monogram
   tile (first letter of the id on the element colour). Icons are a later
   change; the daemon still sends `BrandHint` so that change is web-only. No
   favicon fetching, ever: a list of the servers on your machine must not leak
   to third parties to draw icons.
9. The list shows the server id from the source file, not a friendly name. The
   id is what appears in Whip's config and in `mcp.call`; a name table would be
   a second thing to maintain.

## Goal

A whipcode user on a fresh host sees the servers they already configured
elsewhere, ticks the ones they want, and ends with native, trusted entries in
that host's Whip config, without a terminal and without learning what
`mcpImport` means. The CLI keeps working unchanged, on the same core.

## Non-goals

- OAuth sign-in for remote servers (stays parked, as in the contracts plan).
- Probing candidates before import (decision 3).
- Re-offering when new servers appear (decision 2).
- A TUI version of the screen; the TUI keeps its gate toggles and `/mcp`.
- Editing entries, secrets or headers in the screen.
- A Settings switch for the project or OpenCode gates on the Configuration
  page (that page covers Claude and Codex only today; unchanged).
- Friendly names.

## Preserved / changed / not built

- **Preserved:** `whip mcp import` semantics (gated, `--dry-run`, idempotent,
  trust note); `mcpImport` gate semantics; native-only trust; the Host
  integrations panel; `mcp.import.status/configure`.
- **Changed:** import logic moves out of `mcpImportCLI` into `internal/mcp`
  and the CLI calls it; `Merge` and `ImportPolicy` gain OpenCode;
  `config.MCPImport` gains `opencode` and `offered`; two host-level protocol
  operations; the Welcome screen and Settings › Configuration gain the entry.
- **Not built:** probes, OAuth, favicon fetch, TUI screen, re-offer logic.

## Design

### Surfaces

| Surface | Files |
| --- | --- |
| Discovery (Go) | `internal/mcp/config.go` (+ `opencode.go`, new) |
| Import core (Go) | `internal/mcp/import.go` (new); `cmd/whip/mcp.go` calls it |
| Config | `internal/config/config.go` (`MCPImport.Opencode`, `MCPImport.Offered`) |
| Protocol | `internal/protocol/{runtime_contract,runtime_types}.go`, host service registry next to `configuration.get/update`; `npm run generate` |
| Daemon | `internal/daemon/mcp_import_service.go` (new), beside `provider_service.go:172-215` |
| SDK | `packages/sdk/src/services.ts` (`client.mcpImport.candidates/apply`) |
| Web | `packages/app/src/mcp-import.tsx`, `mcp-brand.tsx`, `assets/mcp-brands.svg` (new); `welcome.tsx`, `settings/configuration.tsx`, `details/integrations.tsx` |
| Docs | `docs/features.md` (MCP), `docs/README.md` (MCP), `docs/tools.md` (trust paragraph), `docs/roadmap.md` |

### A. OpenCode source

`internal/mcp/opencode.go`, mirroring `codextoml.go` / `claude.go`:

- `OpenCodePaths` (test-overridable) = `~/.config/opencode/config.json`,
  `opencode.json`, `opencode.jsonc`, merged in that order (OpenCode's own
  order, `reference/opencode/packages/opencode/src/config/config.ts:272-274`).
  Missing files are not errors; a file that fails to parse is one `Errs` entry
  labelled `opencode config`.
- Entry shape (`mcp.<name>`): `type: "local"` → `Command = command[]`,
  `Env = environment`; `type: "remote"` → `URL = url`, `Headers = headers`;
  `enabled: false` → `Enabled = &false`. `oauth` present and not `false` →
  `Enabled = &false`, `Note = "needs a sign-in Whip can't do yet"` (same move
  as the Claude `sse` transport at `claude.go:58-60`; the runtime import would
  only fail with a 401). `timeout` is ignored.
- JSONC: reuse whatever `internal/config` uses to strip the header comment from
  `config.json`; if that is a bespoke strip, lift it into a shared helper
  rather than adding a dependency.
- Wiring: `Merge(whip, project, codex, claudeGlobal, opencode)`; `ImportPolicy`
  gains `Opencode` (default on, like Claude and Codex); `LoadMergedFiltered`
  reads it as the lowest source; `SourceLabel` → `"opencode config"`; the
  `split` source name is `"opencode"`.
- Gate surfaces: `config.MCPImport.Opencode`, `importSourceSlot`/`importState`
  (`client_control.go:1301-1339`), `MCPImportStatusResult.Opencode`, the
  `importSources` list in `details/integrations.tsx:42-46`, and the TUI
  palette toggle rows (`internal/tui`, where the project toggle was added).
  The Configuration page's `import_claude/import_codex` switches stay as they
  are.

### B. Candidates and apply (the shared core)

`internal/mcp/import.go`:

```go
type CandidateState string // "importable" | "native" | "disabled" | "excluded" | "unsupported"

// Candidate is one server discovered outside native configuration, with
// enough to render a row and nothing that could leak a secret.
type Candidate struct {
    Name, Source, SourcePath string // Source: codex|claude|project|opencode
    Transport string                // stdio|http
    State     CandidateState
    Note      string                // the source's own reason, when it gave one
    BrandHint string                // URL host, or the command's package/binary name
    config    ServerConfig          // full entry, used by Apply only
}

// Candidates lists every discovered server once, resolved by source
// precedence, sorted by name. Gates are ignored; only/exclude lists are not.
func Candidates(cwd string, native map[string]ServerConfig, policy ImportPolicy) ([]Candidate, map[string]error)

// Apply copies the named candidates into cfg.MCPServers as native entries
// (Origin/Source dropped, Enabled cleared) and reports what it skipped.
func Apply(cfg *config.Config, cands []Candidate, names []string) (added []string, skipped map[string]string)
```

- State rules: name in `native` → `native`; `policy.<source>.Exclude[name]` or
  a non-empty `Only` without the name → `excluded`; `cfg.Disabled()` with a
  sign-in note → `unsupported`; `cfg.Disabled()` otherwise → `disabled`; else
  `importable`. Precedence between sources is `Merge`'s (project > codex >
  claude > opencode), so a server in both Codex and OpenCode appears once,
  under Codex.
- `BrandHint`: for HTTP the URL host (`mcp.figma.com`); for stdio the last
  path element of the binary or the npm package name after `npx [-y]`
  (`chrome-devtools-mcp@latest` → `chrome-devtools-mcp`). Never env, headers,
  or full commands.
- `Candidates` needs the per-source maps before the merge, which
  `LoadMergedFiltered` computes and discards. Factor a `loadSources(cwd)`
  step that both call; no behaviour change for existing callers
  (`TestLoadMergedDiscovery` and friends stay green).
- `Apply` refuses `native` and `unsupported` names (returned in `skipped`),
  accepts `importable`, `disabled` and `excluded` (the UI only sends an
  excluded name after "Include"). Entries are built the way
  `mcpImportCLI` builds them today (`cmd/whip/mcp.go:300-304`) minus the
  `Enabled` copy (decision 5).
- `mcpImportCLI` becomes: candidates → names = every `importable` candidate
  whose source gate admits it → `Apply` → `cfg.Save()`; output and flags
  unchanged; `TestMCPImportDryRunWritesNothing` and
  `TestMCPImportAppliesAndIsIdempotent` must pass untouched.

### C. Protocol and daemon

Host-level operations (the Welcome screen has a host but no session yet, so
these sit beside `configuration.get/update`, not among the session-scoped
`mcp.*` runtime ops):

```go
type MCPImportCandidatesParams struct{ CWD string `json:"cwd,omitempty"` } // project source only when given
type MCPImportCandidate struct {
    Name, Source, SourcePath, Transport, State, Note, BrandHint string
}
type MCPImportCandidatesResult struct {
    Candidates []MCPImportCandidate `json:"candidates"`
    Offered    bool                 `json:"offered"`
    ConfigPath string               `json:"configPath"` // shown in the action bar
    Errors     map[string]string    `json:"errors,omitempty"` // unreadable sources, by label
}
type MCPImportApplyParams struct {
    CWD   string   `json:"cwd,omitempty"`
    Names []string `json:"names"` // empty = "offer seen, import nothing"
}
type MCPImportApplyResult struct {
    Imported []string          `json:"imported"`
    Skipped  map[string]string `json:"skipped,omitempty"`
}
```

- `mcp.import.candidates` (query) and `mcp.import.apply` (command), permission
  `host-configuration` like `mcp.import.configure`; neither carries secrets.
- Daemon (`internal/daemon/mcp_import_service.go`): candidates =
  `config.ReadVersioned()` + `mcp.Candidates(cwd, FromConfigMap(cfg.MCPServers),
  ImportPolicyFrom(cfg.MCPImport))`. Apply = recompute candidates, then one
  `config.UpdateVersioned("", mutate)` where `mutate` runs `Apply` and sets
  `MCPImport.Offered = true` (also for empty `names`). Unknown names are a
  validation error, not a partial write.
- Known cost, unchanged from every daemon config write: `Config.Save`
  re-marshals the struct, so JSONC comments and unknown keys in `config.json`
  do not survive (`internal/config/config.go:443-509`). Not made worse here.
- Regenerate with `npm run generate`; `check:drift` in CI.
- SDK: `client.mcpImport.candidates({cwd})` / `.apply({cwd, names})` following
  the `configuration` service wrapper in `packages/sdk/src/services.ts`.

### D. The screen (`packages/app/src/mcp-import.tsx`)

`MCPImportScreen({ client, hostName, cwd, onDone })`, built from `@whip/ui`
(`Checkbox`, `Button`, `EmptyState`, `useToast`) and tokens only:

- Heading "Bring your MCP servers into Whip"; one sentence under it: "Whip
  found N servers in Codex, Claude, and OpenCode on <host>. Pick the ones to
  add; they run as Whip's own servers, without per-call approval." The trust
  clause is the one line of copy frames 11–14 dropped for cleanliness; it goes
  back because it is the consequence of the click.
- Rows, 34px: checkbox slot, 22px brand tile, server id, caveat on the right
  only when one exists ("Off in Codex", "Excluded by your rules · Include",
  "Needs a sign-in Whip can't do yet", "Already in Whip"). Two-pass order:
  everything not native A→Z, then native A→Z. Native rows show the muted check
  instead of a checkbox.
- Selection state is local; defaults per decision 5; "Include" moves an
  excluded row into the selectable set (unchecked). Action bar: "N selected ·
  saved to Whip's configuration on <host>" and `<configPath>`; ghost "Skip
  for now" (apply with `[]`); primary "Import N servers" (apply with the
  checked names; disabled at 0).
- After apply: toast with the imported count, `onDone()`, and invalidate
  `mcp.import.candidates` and `mcp.status` queries. Unreadable sources render
  as one quiet line under the list ("Couldn't read ~/.codex/config.toml: …"),
  not as an error state; an empty candidate list renders `EmptyState` and
  Skip only.
- Brand tile (`mcp-brand.tsx`): `brandFor(hint)` maps hosts and package names
  to sprite ids; `assets/mcp-brands.svg` holds the simple-icons marks
  (`googleanalytics`, `googlechrome`, `figma`, `googlesearchconsole`, `linear`,
  `playwright`, `posthog`, `x`, `openai`) with a NOTICE file like
  `provider-logos-NOTICE.txt`; `inference` reuses `assets/providers.svg`.
  Unknown → monogram (first letter of the id) on the element tile.

### E. Placement

- **Welcome** (`packages/app/src/welcome.tsx`): once the selected host's
  provider is ready (after `ProviderSetup`), query candidates for that host
  with the picked directory as `cwd`. If `offered` is false and at least one
  candidate is `importable`, render the screen in place of the composer;
  `onDone` returns to the composer. Gate on
  `client.supports('mcp.import.candidates')` so an older daemon shows nothing
  new.
- **Settings › Configuration** (`settings/configuration.tsx:165`): in the
  "Configuration imports" group add a row "Servers from other agents" with an
  "Import…" button that opens the screen in a `Dialog` for the selected host
  (no `cwd`, so no project source). Works regardless of `offered`.

### Concurrency and safety notes

- No new goroutines. Discovery is synchronous file reads; apply is one
  versioned config write. Two clients applying at once both go through
  `UpdateVersioned`; the second re-reads and adds its names on top.
- The daemon never spawns or dials a candidate. `BrandHint` is the only
  derived field and is computed without touching headers or env.
- `Config.Save`'s clobber guard is unaffected: apply only adds keys.

## Prior art

- `whip mcp import` and the gates: `.ai-docs/plans/mcp-import-toggle/README.md`
  (why per-name exclude exists); first-run wizard's Claude/Codex questions:
  `.ai-docs/plans/setup-wizard-mcp-toggle/README.md`.
- Trust boundary and import as the trust path:
  `.ai-docs/plans/mcp-contracts/PLAN.md` decisions 5–6; `docs/tools.md:188-200`.
- OpenCode's config shape and file order: `reference/opencode/packages/opencode/src/config/config.ts:141,272-274`,
  `cli/cmd/mcp.ts:483-494,629-640` (`type`, `command`, `environment`, `url`,
  `headers`, `enabled`, `oauth`).
- Provider onboarding placement in Welcome: `docs/features.md:312-346`,
  `packages/app/src/provider-setup.tsx`.

## Test plan

Go (stdlib `testing`, `-race` on touched packages):

- `internal/mcp/opencode_test.go`: local and remote entries, `enabled:false`,
  `oauth` → disabled with note, jsonc comments, missing files, one broken file
  → one `Errs` entry, three-file merge order.
- `internal/mcp/config_test.go`: `TestMergePrecedence` gains the fifth source;
  `TestLoadMergedFilteredPolicy` gains the OpenCode gate.
- `internal/mcp/import_test.go`: state per rule (native, disabled, excluded via
  `exclude` and via `only`, unsupported, importable); gates ignored; a name in
  Codex and OpenCode appears once under Codex; `BrandHint` for `npx -y pkg@tag`,
  `uvx pkg`, a `.app` path and an HTTP URL, and never contains a header or env
  value; `Apply` adds enabled native entries, skips native/unsupported, is
  idempotent, and leaves other keys alone.
- `cmd/whip/mcp_import_test.go`: existing two tests unchanged and green.
- `internal/daemon/mcp_import_service_test.go`: candidates over a `WHIP_HOME`
  fixture with fake Codex/Claude/OpenCode paths; apply writes the block and
  `offered`; empty names sets `offered` only; unknown name is a validation
  error and writes nothing; boundaries test entry for the new operations.
- `internal/daemon/client_control_test.go` / TUI palette test: `opencode` gate
  round-trips through `mcp.import.configure` and the palette row.
- Protocol: `npm run check:drift` clean; runtime parity test picks up the new
  operations.

Web (vitest, `apps/web/vitest.config.ts`):

- `packages/app/test/mcp-import.test.tsx`: order (non-native A→Z, native
  A→Z); default selection; Include flips an excluded row; Import sends exactly
  the checked names; Skip sends `[]`; counts in the button and action bar;
  unreadable-source line; empty state; monogram fallback for an unknown hint.
- `welcome.test.tsx`: screen appears only when `offered` is false, at least
  one importable candidate exists and the provider is ready; disappears after
  apply; hidden when the daemon lacks the operation.
- `settings-configuration.test.tsx`: the Import row opens the dialog for the
  selected host.
- `inspector.test.tsx`: the Host integrations list shows the OpenCode source.

## Docs plan

- `docs/features.md` › MCP: five sources and OpenCode's OAuth handling; a
  bullet for the onboarding screen (behavior → `mcp-import.tsx`,
  `mcp_import_service.go`, `import.go` → the tests above).
- `docs/README.md` › MCP: OpenCode path, `mcpImport.opencode`,
  `mcpImport.offered`, and that Settings › Configuration can import.
- `docs/tools.md:188-200`: "…or by the import screen in the web app" in the
  trust paragraph.
- `docs/roadmap.md`: new checked line under "WHIP client foundation" when
  shipped; leave "MCP progressive discovery" as is.

## Tasks (one commit each, in order)

1. **OpenCode source.** Parser, paths, `Merge`/`ImportPolicy`/gate plumbing,
   panel and palette rows, docs lines. Tests: `opencode_test.go`, precedence
   and policy tests, palette test.
2. **Import core.** `Candidates` + `Apply` in `internal/mcp/import.go`,
   `loadSources` refactor, CLI moved onto it with unchanged output. Tests:
   `import_test.go`, existing CLI tests green.
3. **Protocol + daemon + SDK.** Types, registry entries, service, schema
   regeneration, SDK wrappers. Tests: service test, boundaries, drift check.
4. **Screen.** `mcp-import.tsx`, `mcp-brand.tsx`, sprite + NOTICE. Tests:
   `mcp-import.test.tsx`.
5. **Placement.** Welcome auto-offer and Settings entry. Tests: `welcome`,
   `settings-configuration`.
6. **Close.** `docs/features.md`, `docs/README.md`, `docs/tools.md`, roadmap;
   `task check` + `go test -race` on `internal/mcp`, `internal/daemon`,
   `cmd/whip`; adversarial pass (two clients applying at once, a Codex file
   that changes between candidates and apply, a native name that collides with
   a candidate, a host where every source is unreadable, narrow window).

## Implementation record (2026-09-15)

| Task | Commit subject | Notes |
| --- | --- | --- |
| 1 | Add OpenCode as a fourth MCP import source | Reads `config.json`, `opencode.json`, `opencode.jsonc` in OpenCode's order; `oauth` entries import disabled with `mcp.SignInNote`; `internal/mcp` tests isolate OpenCode paths in `TestMain`. |
| 2 | Share the MCP import core between the CLI and the daemon | `mcp.Candidates` / `mcp.Apply`; `loadSources` refactor. Deviation: the CLI no longer copies servers a source turned off or unsupported ones (it used to copy them as disabled entries). |
| 3 | Expose MCP import candidates and apply as host-level operations | `rpc:mcp.import.candidates`, `rpc:mcp.import.apply` beside `config.*`; `config.MCPImport.Offered`; `client.mcpImport` in the SDK; `config.Path()` exported for the action bar. |
| 4 | Add the MCP import screen to the web app | `packages/app/src/mcp-import.tsx`. Monogram tiles only (decision 8); nullable arrays from the generated contract handled in the component. |
| 5 | Offer the import on New session and from Settings | Welcome shows the screen after the provider is ready, with the host picker under it; Settings › Configuration › "Servers from other agents" opens it in a dialog and reports the count; an older daemon gets one explanatory line instead of the screen. |
| 6 | Docs, roadmap, gates | `docs/features.md`, `docs/README.md`, `docs/tools.md`, `docs/roadmap.md`, web workflow inventory; `task check`, `go test -race` on the touched packages, full web suite. |

Not done, on purpose: brand icons (decision 8), TUI screen, re-offer on new servers, a Settings switch for the project/OpenCode gates.
