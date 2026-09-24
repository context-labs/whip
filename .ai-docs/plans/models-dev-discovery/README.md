# Models.dev catalog and local provider discovery

Branch: `codex/provider-onboarding`

Status: Implementation plan. Research and repository inspection complete;
implementation and acceptance are pending. Prepared September 9, 2026.

## Goal

Make supported providers available as soon as Whip can find their credentials on
the execution host. Use Models.dev for shared provider/model metadata, persist
provider definitions with named credential references, and continue fetching
models directly from providers when users connect or select them.

The first TUI screen keeps the current compact provider chooser. Existing keys
produce checkmarks; selecting a provider uses its key without asking for a paste.
Inference.net remains first and quietly recommended. Web and desktop observe the
same host-owned inventory.

## Scope

- Replace manually duplicated catalog metadata with a checked-in Models.dev
  snapshot and one repeatable update command.
- Automatically create missing provider definitions referencing available keys.
- Resolve named keys from the process environment and explicitly configured
  local files/directories, without copying file contents into provider config.
- Remove the OpenCode credential-store importer and its routing probes.
- Keep the ten existing API-key presets and separate ChatGPT subscription route.
  Additional provider transports require separate compatibility work.
- Preserve live discovery, custom endpoints, saved routes, disabled providers,
  existing authentication flows, and current TUI defaults.

Non-goals: a provider database, a new secrets vault, importing OpenCode accounts,
automatic model/endpoint changes during an update, runtime Models.dev polling,
arbitrary home-directory scans, shell execution to parse key files, new TUI
advanced forms, or a usability study. Existing direct key entry remains supported.

## Findings and source boundaries

Models.dev exposes a JSON catalog with provider environment-variable names and
model metadata. Provider URLs are optional for SDK-specific integrations. Prices
are expressed per million tokens. Its MIT license requires retaining the notice.
Sources: [API and schema](https://github.com/anomalyco/models.dev#api),
[license](https://github.com/anomalyco/models.dev/blob/dev/LICENSE).

OpenCode v1.18.30 matches catalog environment names against its environment to
construct effective connections. That path does not persist newly discovered
provider config. Whip's persistence below is a deliberate product choice.
Sources: [provider discovery, lines 1582–1607](https://github.com/anomalyco/opencode/blob/v1.18.30/packages/opencode/src/provider/provider.ts#L1582-L1607),
[desktop shell environment, lines 36–90](https://github.com/anomalyco/opencode/blob/v1.18.30/packages/desktop/src/main/shell-env.ts#L36-L90).

Two concrete mappings need local policy:

- Models.dev calls Inference.net `inference` and currently lists
  `https://inference.net/v1`; Whip uses `inference-net` and
  `https://api.inference.net/v1`. Preserve Whip's ID, display name and endpoint.
  [Upstream definition](https://github.com/anomalyco/models.dev/blob/dev/providers/inference/provider.toml).
- DeepInfra and Together use SDK-specific definitions without an explicit API
  root. Keep Whip's verified OpenAI-compatible roots and DeepInfra's existing
  `DEEPINFRA_TOKEN` alias.
  [DeepInfra](https://github.com/anomalyco/models.dev/blob/dev/providers/deepinfra/provider.toml),
  [Together](https://github.com/anomalyco/models.dev/blob/dev/providers/togetherai/provider.toml).

Whip already provides most of the required foundation:

| Existing code | Current behavior | Planned change |
| --- | --- | --- |
| `internal/config/providers.go` | Presets, env aliases, effective routes and credential precedence | Compose metadata with local policy; use the common named-key resolver |
| `internal/config/provider_models.go` | Manually curated Models.dev-derived metadata; live membership wins | Replace duplicated specifications with imported metadata; retain transport restrictions |
| `internal/config/opencode_credentials.go` | Reads OpenCode auth/config/SQLite routing state | Delete; move the useful operation-scoped snapshot concept into local credential resolution |
| `internal/config/config.go`, `revision.go` | JSONC, guarded atomic saves, serialized revision updates | Add optional key-source paths and idempotent provider insertion |
| `internal/daemon/provider_model.go`, `provider_configuration.go` | Validation, live catalogs, stale-response guards, key setup | Resolve the same credential sources for setup, discovery and inference |
| `internal/config/catalog.go` | Provider catalog cache with a 24-hour TTL | Preserve live membership and freshness; enrich missing metadata separately |
| `apps/desktop/src/runtime.ts` | Bounded local shell recovery with duplicated key-name list | Generate the key-name list from the shared catalog/policy; remove OpenCode probes |

Historical plans are research context. Current behavior and implementation
requirements remain documented in `docs/models-providers.md`, `docs/features.md`
and `docs/frontend.md` when this ships.

## 1. Bundle the catalog and keep a small Whip policy layer

Add a small Go importer, following the existing `cmd/themegen` convention:

- `task models:update`: fetch the official JSON API, normalize the supported
  subset, and update the bundled asset plus the desktop key-name artifact.
- Support a local input file for reproducible fixture tests and replaying a
  downloaded snapshot. Report provider/model additions, removals and metadata
  changes for review. Endpoint differences must be visible.
- `task models:check`: validate the checked-in asset, policy mappings and generated
  desktop names without network access. Add it to `task check`.
- Record source URL, input SHA-256, retrieval date and importer schema version.
  Preserve provenance for unchanged input; identical input must generate identical
  outputs. Include the upstream license with the bundled data.
- Bound downloads, validate before replacing files, and retain the previous
  snapshot on HTTP, parse, schema or generation failure. Start with a 30-second
  request deadline and 32 MiB input cap; verify against the measured upstream size.
- Ordinary builds, Docker builds, startup and tests use the checked-in snapshot.
  They do not fetch Models.dev. No new Go or JavaScript dependencies are planned.

Use one normalized JSON asset under `internal/config/modelsdev/`, loaded through
Go embedding, plus a generated TypeScript export containing only supported
environment names for desktop startup. Return copies where callers can mutate
maps/slices; do not expose a mutable shared catalog.

Keep Whip-owned policy in the existing preset code: enabled provider IDs,
upstream-to-Whip ID mapping, canonical API roots, adapter choice, auth methods,
environment aliases not present upstream, UI family/category, key-page links,
compatibility exceptions and preferred models/efforts. Missing upstream providers
or policy-referenced defaults are reported by the update/check commands and need
an explicit retained override or policy adjustment.

Imported metadata can supply display names, model IDs, context/output limits,
modalities, capabilities, reasoning choices and supported pricing fields.
Unknown values stay unknown; explicit false, empty lists and zero prices must
remain distinguishable from omitted values. `reasoning: true` alone does not
invent supported thinking levels. Normalize prices into Whip's existing per-token
representation without changing budget/usage accounting semantics.

## 2. Resolve local keys by name

Keep the existing `apiKeyEnv` field as the durable named-key reference. Add one
optional host-config section for where else those names can be resolved:

```json
{
  "providerKeySources": {
    "envFiles": ["~/.secrets/providers.env"],
    "keyFiles": { "CEREBRAS_API_KEY": "~/.secrets/cerebras.key" },
    "keyDirectories": ["~/.secrets/provider-keys"]
  }
}
```

These are examples, not paths to assume exist on the user's machine. With this
section omitted, discovery uses the inherited environment and existing supported
Whip/account sources. No arbitrary `.env` or secrets folder is loaded by default.

Resolution order for a named key is:

1. A nonempty value in the execution daemon's environment.
2. An explicitly mapped `keyFiles` entry.
3. The first definition in the ordered `envFiles` list.
4. A matching file in the ordered `keyDirectories` list.

Directory matching opens only exact known names, such as
`provider-keys/CEREBRAS_API_KEY`; it does not enumerate or guess provider names
from secret contents. Arbitrary filenames use `keyFiles`. Relevant names are
the supported preset aliases plus explicitly configured provider references.
An explicit mapped file or definition that is invalid/unreadable reports a source
error rather than silently selecting a lower-priority file. Missing directory
candidates can be skipped. Empty credentials never produce availability marks.

File paths allow `~/` and absolute paths on the execution host. Reject relative
paths so changing workspace cannot change key resolution. Read regular files
with bounded sizes (initially 16 KiB per raw key and 1 MiB per env file), trim a
raw key's surrounding whitespace, and reject multiline/control-character keys.
For directory discovery, do not follow symlinks outside the selected directory;
explicit file mappings can identify the intended file directly.

Support a documented dotenv subset: blank lines/comments, optional `export`,
valid `NAME=value` assignments, and single/double quoted values. Treat contents
as data: no command substitution, shell sourcing, interpolation or process-wide
`os.Setenv`. Reject unsupported syntax with filename/line diagnostics that never
include the value. Bound source counts and read only once per operation.

For `OPENAI_API_KEY`, retain the existing custom-endpoint guard. Read relevant
`OPENAI_BASE_URL`/`OPENAI_API_BASE` metadata alongside credentials from that source,
as well as the inherited environment; conflicting routing prevents automatic
assignment to the canonical OpenAI endpoint. Endpoint variables do not silently
create or rewrite a custom route.

Refactor the existing `CredentialSnapshot` to accept an already-loaded config
and hold short-lived resolved values with provenance. Configuration/provider
helpers use that same snapshot for inventory and runtime resolution. Pass config
and snapshots explicitly through service boundaries; never recursively call
`config.Load` from `UpdateVersioned` callbacks. Audit standalone `Provider.Key`,
`ResolveKey`, configured-provider checks and all model-client constructors so
headless, TUI, web, ACP, child and helper calls resolve the same way.

Provider `apiKeyEnv` and whole `$NAME`/`${NAME}` provider references use this
resolver. Preserve existing literal, command, explicit no-auth, Inference.net
machine-account and ChatGPT behavior. This change does not extend MCP's separate
environment/template semantics or execute credential commands during inventory.
Existing explicitly mixed `apiKeyEnv`/`apiKey` configurations retain their
documented precedence; generated entries contain only the named reference.

## 3. Persist discovered provider definitions

For example, detecting an OpenRouter key creates this entry in the existing
distribution-specific configuration file:

```json
{
  "providers": {
    "openrouter": {
      "name": "OpenRouter",
      "baseUrl": "https://openrouter.ai/api/v1",
      "api": "openai-completions",
      "apiKeyEnv": "OPENROUTER_API_KEY"
    }
  }
}
```

The key value stays in its original environment/file. A non-primary env alias
stores the actual matched name. A saved canonical preset reference, including
the initial Inference.net template, can resolve from the configured file sources
without rewriting that provider entry.

Run discovery once after the daemon acquires ownership and loads configuration,
before initial inventory is exposed. Also expose one idempotent host operation,
`provider.discover`, for opening connection setup and explicit Settings Refresh.
Keep `provider.list` read-only and free of upstream calls. Discovery returns the
fresh inventory/revision and accepts the same optional selection context as list.
It is an ephemeral host configuration operation, not a durable session command.

Discovery must:

- Build candidates from supported presets and available named keys, then apply
  only missing entries through the existing serialized, atomic config path.
- Recheck the latest config and `disabledProviders` under the write guard. An
  explicit route, literal key, endpoint override or disabled ID always wins.
- Write once for all additions and perform no write or revision churn for a
  repeated no-op. Add a no-op-aware patch path rather than calling the current
  unconditional `UpdateVersioned` save on every inventory opening.
- Preserve every unrelated field. Include `providerKeySources` in snapshots and
  config recovery; a sources-only config must not lose those paths when defaults
  are supplied or an older backup is considered.
- Leave defaults and saved session routes untouched. New metadata does not
  switch a user's selected provider, model, effort, or existing endpoint.
- Keep existing configurations when a key disappears; mark the route unavailable
  with source-specific recovery information. Do not delete or re-enable it.
- On a persistence failure, preserve the prior file, surface the failure in
  management/diagnostics, and keep any resolvable effective route usable for the
  current process. Do not report it as persistently configured.

File sources are reread on discovery and connection/model-client construction.
Rotated keys apply to newly constructed clients; existing clients retain the
current explicit reload/reselection behavior. Environment variables belong to
the daemon process: exporting a new key in another terminal still requires
restarting the daemon. Avoid watchers or per-turn shell probes in this change.

Disconnect keeps the existing durable disabled-provider opt-out, so repeated
discovery cannot resurrect the route. Reconnection is an explicit user action.

## 4. Preserve live model discovery and authentication

- Local credential discovery makes no provider HTTP requests and no paid calls.
  A checkmark means a local credential is available, not that inference succeeded.
- Selecting/connecting a detected provider uses the existing validation and
  targeted live discovery path. Preserve OpenRouter's authenticated `/key`
  check before `/models`; public model lists alone do not validate a key.
- A successful live response owns model membership, including an empty list.
  Do not insert absent bundled models or hide a new sparse model merely because
  it is missing from Models.dev. Preserve explicit incompatibility filtering.
- Fill only missing fields using the exact provider/model pair. Apply existing
  Whip transport restrictions after metadata enrichment; a catalog capability
  does not implement a transport. Custom endpoints do not inherit capabilities
  solely because their provider ID resembles a preset.
- Preserve the existing 24-hour cache, selected-provider refresh, transient-error
  fallback, and route/subscription-account guards. Recheck resolved credentials
  before publishing a delayed model response, including changes to file sources.
  Authentication failures must not be converted into successful bundled discovery.
- Metadata refresh must not renew the timestamp of an old live model response.
  Supplement cached sparse metadata at read time, or invalidate its enrichment
  version, so a new embedded snapshot can take effect without pretending a live
  fetch occurred. Never discard the last usable catalog on a failed refresh.
- Keep explicit configured limits/caps and existing pricing precedence. Use
  Models.dev pricing only as a missing-field estimate for the exact serving route;
  distinguish unknown cost from zero and do not rewrite historical usage.

Retain the approved one-step TUI defaults when live membership permits:

| Whip provider | Model | Thinking |
| --- | --- | --- |
| `inference-net` | `kimi-k3-fast` | `high` |
| `openrouter` | `z-ai/glm-5.3` | `max` |
| `openai` / `openai-codex` | `gpt-6-astra` | `medium` |

Unavailable defaults retain the current model-picker fallback. Other providers
continue to offer their live models; selecting one applies it and closes the
dialog without the removed confirmation screen.

## 5. Client changes and removal of the importer

TUI: call discovery when opening setup, keep selection/search stable, and render
availability from the host. Keep the existing layout, checkmarks, quiet footers,
masked key input and inline errors. Show source details in management only.

Web/desktop: reuse the existing provider query and connection components. Settings
Refresh invokes discovery then invalidates that host's inventory/default/model
queries. Connection setup uses the same operation without adding a prompt or
another onboarding screen. Add source labels `Environment`, `Environment file`
and `Key file`; source summaries contain names/paths only. Keep credentials out
of renderer state, recovery storage, logs and session databases. File-source paths
are initially configured in the host file; no new source-management form is needed.

Desktop: generate its supported key-name export from the same effective registry.
Retain bounded local login-shell recovery and endpoint guards. Remove
OpenCode-specific environment markers, config-path probes and related tests once
the importer is deleted. Do not remove XDG handling used independently elsewhere.
Remote connections continue to resolve credentials on the remote execution host.
An attached existing daemon requires restart to inherit newly exported shell keys.

Delete `internal/config/opencode_credentials.go` and importer-specific tests,
fallback branches, `opencode` source labels, docs and fixtures. Replace snapshot
call sites with the local source implementation. Keep unrelated OpenCode-inspired
themes, layout and research. Users whose only source was OpenCode will need a
normal named-key source or Whip key entry; do not silently copy those credentials
as a migration. Existing Whip-saved keys and account login files are preserved.

Add the operation and source-summary fields to the Go protocol registry, regenerate
TypeScript/schema artifacts, expose it through the SDK and Go client, and follow
the existing capability checks for older daemons. Older hosts may continue to
provide read-only inventory; clients must not pretend discovery persisted anything.

## File map and implementation order

| Step | Main files | Deliverable |
| --- | --- | --- |
| 1 | New `cmd/modelgen/`, `internal/config/modelsdev/`; `Taskfile.yaml` | Checked-in catalog, provenance/license, update/check commands and importer fixtures |
| 2 | `internal/config/providers.go`, `provider_models.go`; generated desktop names | Shared metadata with Whip policy overrides and live membership preserved |
| 3 | New `internal/config/provider_credentials.go`; `config.go`, `providers.go` | Bounded named-key resolver, source config, precedence/provenance and config recovery coverage |
| 4 | New `internal/daemon/provider_discovery.go`; `cmd/whip/daemon.go`, daemon provider service/model/configuration; `internal/config/revision.go` | Idempotent persistence, startup integration and consistent runtime key resolution |
| 5 | `internal/protocol/{provider_types,registry}.go`, daemon provider RPC/client, `packages/protocol`, `packages/sdk/src/services.ts` | Discovery RPC, redacted source summaries and generated client contracts |
| 6 | `internal/tui/{setup,setup_host,setup_picker}.go`, `packages/app/src/{provider-setup,settings/provider-connections}.tsx`, `apps/desktop/src/runtime.ts` | Existing setup/refresh flows use host discovery and the generated env names |
| 7 | `internal/config/opencode_credentials*`, importer callers/tests/docs | Old importer and its supporting probes removed atomically with the replacement |
| 8 | Tests, canonical docs, roadmap, isolated Docker/browser evidence | Verified end-to-end behavior and updated user/developer instructions |

## Acceptance and regression tests

1. **Catalog import:** deterministic output; upstream ID mapping; missing API roots;
   endpoint override retention; extra unknown fields; omitted versus false/zero;
   price-unit conversion; model-ID collisions across providers; malformed and
   oversized responses; failed update preserves the prior files; offline checks.
2. **Credential resolution:** env-only, dotenv-only, raw-file-only, explicit filename
   mapping, aliases and precedence; quote/comment parsing; malformed/empty/missing
   values; no shell execution; file bounds; route guards; source values absent
   from config, protocol, logs and databases; file rotation and removal.
3. **Persistence:** fresh startup saves references once; restart retains them;
   repeated discovery leaves file/revision unchanged; preset templates resolve
   file keys; concurrent discovery and user edits preserve overrides; disabled
   providers stay disabled; sources-only config survives load/recovery; failed
   writes preserve existing config and report failure honestly.
4. **Runtime parity:** the key resolved during inventory/setup reaches the actual
   Authorization header in fake-provider streaming tests. Cover root, child/helper
   clients and headless entry points, not just the provider-list response. A file
   replacement during discovery prevents stale catalog publication.
5. **Models:** a sparse new Cerebras ID remains visible; an empty successful list
   stays empty; negative capability data wins; defaults and efforts persist;
   explicit limits and unknown/free prices retain their meaning; OpenRouter 401
   rejects the selected key despite a public `/models`; transient errors retain
   usable cached models without refreshing their age.
6. **Clients:** TUI and web show the same availability and source; changing hosts
   cannot carry keys or file paths to the remote daemon; refresh preserves drafts
   and focus; the popular-provider quick path and model-picker fallback work;
   older-daemon capability behavior is explicit; desktop key names match Go.
7. **Migration:** OpenCode-only files no longer create a route; Whip literal/env/
   command/account routes continue working. No OpenCode file/database reads remain.

Use isolated fixture homes and fake providers for automated tests. Run targeted
Go/SDK/app/desktop tests while implementing, then `task check` and race coverage
for the changed config/daemon paths. Keep network-dependent catalog updates out
of normal test runs. No new concurrency owner is required; reuse current daemon
ownership and configuration synchronization, and avoid holding locks during I/O.

Finish with the existing Docker workflow from dirty working files: an empty home
must still show clean onboarding, and a separate explicitly seeded fixture run
must discover file/env keys and make a fake-provider request. Keep the default
Docker container isolated from host credentials. Test restart with the same
isolated home before disposal, and capture TUI/provider Settings screenshots at
normal and narrow sizes. Test tooling must not replace the installed application
or stop the user's running container.

## Documentation and completion

- README: `task models:update`, `task models:check`, and concise named-key/file
  configuration examples with daemon restart behavior.
- `docs/models-providers.md`: source configuration, precedence, persistence,
  supported-provider boundaries, live versus bundled metadata, and importer removal.
- `docs/frontend.md`: host discovery operation, read-only inventory contract,
  source labels and host-scoped invalidation. Preserve the canonical package guide.
- `docs/features.md`: shipped behavior, owning code and concrete regression tests.
- `docs/roadmap.md`: one Models.dev/local-key-discovery item, checked only after
  implementation and acceptance pass.

The remaining user-specific detail is the actual secrets-file layout. The design
supports exported variables, a declared env file, arbitrary explicitly mapped key
files, and exact-name files in a declared directory. Implementation can use
fixtures without reading personal keys or waiting for that detail.

- [x] Verify Models.dev/OpenCode behavior and inspect current Whip integration.
- [x] Record architecture, source policy, migration, file map and acceptance plan.
- [x] Implement steps 1–7 above.
- [x] Complete automated and isolated TUI/web/desktop acceptance.
- [x] Update canonical docs and record validation results here.


## Implementation notes

Implemented the ten supported API presets with a normalized 569-model snapshot
from Models.dev. The downloaded source was 4,533,230 bytes; the normalized bundle
is about 340 KiB. The importer retains Whip's Inference.net URL and explicit Kimi
models because upstream currently differs or lacks those IDs. Generated desktop
key names come from the same effective preset registry.

Configuration now supports all three declared source forms. Discovery saves only
missing named references; `provider.list` remains read-only. Startup, TUI setup,
web setup and Settings Refresh use the shared host operation. Old hosts use
read-only inventory fallback. File provenance appears in management/Settings;
pickers keep quiet footers. Existing commands/accounts remain independent of the
removed OpenCode importer. `whip auth openrouter --env` also resolves a declared
file reference on the host without asking for a terminal key.

Adversarial review corrections included in this implementation:

- Keep provider opt-outs when recovering a sources-only file from a backup;
  explicitly supplied empty/replacement lists remain authoritative.
- Snapshot only preset/configured variable names, including pending custom
  provider references. Unrelated env-file variables are not retained as keys or
  probed in key directories.
- Keep a resolved route and its source configuration in one snapshot for model
  construction; reject obsolete routes before catalog HTTP.
- Recheck resolved file credentials before publishing a catalog after key setup,
  and preserve a successful empty live result instead of stale membership.
- Abort browser discovery on host changes/disconnect; do not leave a new host's
  Refresh control disabled by an abandoned request.

Validation uses synthetic credentials and isolated homes/containers. No production
provider completion is required for this change.


## Validation results

- `task check` — passed: offline Models.dev check, formatting, Go vet/whipvet,
  all Go tests, protocol generation/drift/interoperability, SDK, production web
  build, all 490 web/app tests, UI, packaging, update-local and Docker script tests.
- `go test -race ./...` — passed repository-wide, including config/daemon/session
  state and all CLI entry points. Changed-path race runs also passed during review.
- Desktop: 80 source tests passed, 9 existing platform/integration skips; desktop
  TypeScript check passed. Focused provider client tests: 31 passed.
- Docker: clean production entrypoint at 100×36 and 60×28; packaged web at 1280px
  and 390px; detected file/env keys shown in TUI and Settings. Repeated discovery
  preserved revision/config; explicit stop/start preserved references, sources,
  runtime ID and readiness. Removing/restoring a mapped file changed availability
  without deleting/recreating its definition or changing the revision.
- Root/ACP/headless request fixtures prove actual Authorization headers use named
  file keys. Model tests cover live sparse/empty membership, OpenRouter auth,
  rotation during discovery, old-route/new-source races and cache freshness.
- A final TUI rendering change suppresses a duplicate aggregate source warning
  when Manage already shows the provider-specific error. `go test ./internal/tui`
  passed afterward; retained missing-file screenshots in the temporary report
  predate this final deduplication.

Selected evidence: [clean TUI](evidence/clean-tui-xterm.png),
[narrow TUI](evidence/clean-tui-narrow-xterm.png),
[detected key management](evidence/seeded-tui-key-file-xterm.png),
[web key-file source](evidence/seeded-web-key-file.png), and
[narrow environment-file source](evidence/seeded-web-env-file-narrow.png).
[Acceptance details](ACCEPTANCE.md) record the tested Docker image, methods and
limits; the complete logs/harnesses are in `/tmp/whip-modelsdev-acceptance`.

The full check logs are `/tmp/whip-models-task-check-final.log` and
`/tmp/whip-models-full-race.log`. No dependencies were added.

A separate existing lifecycle issue surfaced: `daemon restart --timeout 5s` can
report a stop timeout with an attached, auto-reconnecting TUI even though a new
owner is already running. `cmd/whip/daemon_manage.go` waits for any owner lock to
be absent; a replacement can acquire it before the poll. Closing attached TUI
clients and explicitly stopping/starting the daemon passed. This change preserves
that existing lifecycle implementation and records the follow-up rather than
folding a daemon restart redesign into provider discovery.


Final Docker streaming acceptance also passed on the tested image. A separate
fresh container loaded a custom provider's named key from an env file, sent the
exact synthetic Bearer header to a local mock Chat Completions endpoint, and
`whip run -quiet` streamed the expected response with exit 0 and empty stderr.
Its config retained the reference/path only. The owned container and mock server
were removed. See [the request report](evidence/docker-streaming.json); the
reproducible harness remains at
`/tmp/whip-modelsdev-acceptance/streaming/harness.py`.
