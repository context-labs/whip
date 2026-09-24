# WhipCode manual

Start with [installation and local development](setup.md) for Desktop and the
sole `whipcode` CLI. [Benchmark notes](benchmarks.md) explain the
README comparison and its limits.

Everything that used to crowd the top-level README: full setup, config
reference, MCP, browser/computer-use, and the map of how whip works.

Start with [architecture.md](architecture.md) for the moving parts and
[rlm-runtime.md](rlm-runtime.md) for the recursive runtime, limits, recovery,
and troubleshooting.

For frontend work, start with [frontend.md](frontend.md): the canonical coding-agent
guide to design philosophy, packages, components, data fetching, state ownership,
and validation. [web-app.md](web-app.md) covers running the application.
[desktop.md](desktop.md) covers the macOS host, shared renderer, retained daemon,
signing and release setup, with the current acceptance limits.

## Install

For Linux/macOS x64/arm64, install the checksum-verified packaged CLI:

```sh
curl -fsSL https://raw.githubusercontent.com/context-labs/whip/main/install.sh | WHIPCODE_CHANNEL=prerelease sh
```

Before `v1.0.0`, opt into `v1.0.0-alpha.N` as shown. Stable is the default channel
and fails clearly until a new-project stable release exists. `WHIPCODE_VERSION`
pins an exact new-project tag; `WHIPCODE_BIN_DIR` overrides `~/.local/bin`.
For prerequisites, source builds, and update ownership, see [setup](setup.md).
Pre-reset internal installations require the [manual reset](team-reset.md).
Maintainers should read [release operations](releases.md) and
[Desktop releases](desktop-releases.md).

## Setup (with inference.net)

whip defaults to inference.net models; the `inf` CLI provisions the key:

```sh
git clone --branch main https://github.com/context-labs/whip && cd whip
npm ci
task install                        # packaged whipcode; Go 1.27+, Node 24, Task

bun add -g @inference/cli           # the inf CLI
inf auth login                      # log in
inf team switch                     # pick your team
inf project switch                  # pick your project
inf claude on && inf claude off     # mints the API token whip reads from ~/.inf/config.json
```

Then `whip` and you're in. First things to try: `/context-doctor` (audit
what a fresh session injects, in tokens), `/goal <text>` (work until done),
drop a `.mcp.json` in the repo (MCP servers just appear — `/mcp` to see them).

## Run

```sh
task run                 # run locally from source
task run -- -m glm-5.2-fast          # pass flags after --
whipcode                # installed binary, default model
whipcode -m kimi-k3-fast -p inference   # pick model AND provider
whipcode run -cache-key repo/reviewer "prompt"   # headless; stable prompt cache across runs
```

`task --list` shows the rest (build, test, acceptance, fmt, vet, tidy).

In-session: `/model <name> [provider]`, `/agents`, `/clear`, `/help`, and
`/quit`. ctrl+c once interrupts; twice quits. The model creates retained
children with `agents.spawn` inside `rlm_exec`.

See [features.md](features.md) for the full feature map and [concurrency.md](concurrency.md) for the channel design.

## Config — `~/.whipcode/config.json`

RLM worker limits are configurable under the `rlm` block; there is no runtime
mode switch. See [rlm-runtime.md](rlm-runtime.md#limits).

Models are routed to providers: a model lists the providers that serve it, and
you can switch providers without touching the model. Written with defaults for
inference.net on first run:

```json
{
  "defaultModel": "kimi-k3-fast",
  "providers": {
    "inference": {
      "name": "Inference.net",
      "baseUrl": "https://api.inference.net/v1",
      "api": "openai-completions",
      "apiKeyEnv": "INFERENCE_API_KEY"
    }
  },
  "models": {
    "kimi-k3-fast": { "providers": ["inference"], "context": 131072 }
  }
}
```

`context` is the model's **input** window (context limit); it drives the header's
% full and proactive compaction. The provider's `/models` `context_length`
overrides it when advertised. `maxOut` (optional) caps **output** tokens; 0 uses
the provider's `max_completion_tokens`, else `context`. The old `maxTokens` field
still parses (it always meant the context window) but is superseded by `context`.

**Catalog models need no config entry.** whip caches each provider's
`GET /models` (24h TTL in `~/.whipcode/models.json`), and any advertised model is
usable directly — `whipcode -m deepseek-v4-pro` or `/model deepseek-v4-pro` — with
context, vision, effort levels, and pricing taken from the catalog. Config
entries are authoritative overrides when present. Newly announced models appear
in the `/model` picker (dim, marked `(new)`) after `/model refresh` or the next
TTL cycle. If several providers advertise the same id, pass a provider
(`-p` / `/model <name> <provider>`) to disambiguate.

Any OpenAI-compatible endpoint works as a provider. Key resolution:
`apiKeyEnv` env var → `apiKey` literal → for api.inference.net, the key stored
in `~/.inf/config.json` by the `inf` CLI.

## MCP

whip connects to MCP servers and exposes them through `mcp.search`,
`mcp.describe`, `mcp.list_servers`, `mcp.list_tools`, and `mcp.call` inside
Starlark. Five sources feed one merged
set; on a name conflict the earlier source in this list wins:

- **whip-native**: an `"mcp"` block in `~/.whipcode/config.json`. The only
  trusted source: its servers skip per-call consent.
- **project**: a `.mcp.json` in the session's working directory
  (`{"mcpServers": {...}}`). Repository-authored, so it is **off until you
  enable it** with `"mcpImport": {"project": {"enabled": true}}` or
  `/mcp import project on`. Enabling a source runs its servers' programs at
  session start; tool consent is not a process sandbox.
- **codex**: `[mcp_servers.*]` tables in `~/.codex/config.toml` (on by default).
- **claude**: `mcpServers` in `~/.claude.json` (on by default).
- **opencode**: the `mcp` block in `~/.config/opencode/{config,opencode}.json[c]`
  (on by default; `local` entries become stdio, `remote` become HTTP,
  `{env:NAME}` placeholders become `${NAME}` references, `{file:…}` stays as
  written, and an entry with `oauth` imports disabled with a "needs a
  sign-in" note because whip has no browser sign-in for MCP servers).

Each import source takes `enabled`, `only` and `exclude`. Servers a source
gate filters out stay visible in `/mcp` as `blocked`. `whipcode mcp import`
copies imported servers into the native block, where they become trusted. The
web and desktop app offer the same import as a screen: once per host on New
session when other agents have servers configured there, and any time from
Settings › Agents & execution › MCP servers. Tick what you want and
Import writes it into the native block; Skip sets `mcpImport.offered` so the
offer does not come back on its own. Each row shows the vendor's logo: the app
bundles marks for the common MCP vendors, and the daemon looks the rest up on
DuckDuckGo by the server's domain, once, caching under `~/.whipcode/icons`. Set
`"brandIcons": false` (or turn off "Server logos" in that Settings group) to
keep every lookup on the host; unresolved rows show a monogram.

```json
{
  "mcp": {
    "docs": { "command": ["npx", "-y", "@mcp"], "env": { "API_KEY": "$DOCS_KEY" } },
    "web":  { "url": "https://mcp.example.com/mcp", "headers": { "Authorization": "Bearer $TOKEN" } }
  }
}
```

`/context-doctor` audits what a fresh session injects (skills, MCP tool schemas,
server instructions, built-in tool schemas) with per-source token estimates —
useful when arriving from a heavier harness.

Servers connect in the background at startup and lazily on first use — a
slow or broken server never blocks the loop (calls fail fast with an
actionable message, and dropped sessions auto-reconnect with backoff).
`/mcp` shows live status; `/mcp <name> reconnect|enable|disable` manages
servers for the current session without restarting (host configuration is
unchanged; `/mcp import <source> on|off` is the host-level switch). Server instructions teach the model how to use
each server's tools automatically. CLI: `whipcode mcp list|add|remove|import`
(`import [--dry-run]` copies imported servers into whip's own config, where
they become trusted like hand-written entries), and
`whipcode mcp test <name>` to doctor one server (status, timing, tool names,
stderr tail; non-zero exit — validate a `.mcp.json` in CI). `whipcode mcp
serve` runs whip's own tools (read/bash/edit/write) as an MCP server for
other harnesses through a daemon-owned root; the stdio adapter never opens
SQLite or invokes tool handlers directly. The bridge cannot obtain new
consent: saved rules still apply, but an outer client's approval of a call is
not forwarded as whip consent. Codex configs with `http_headers` and
`bearer_token_env_var` import correctly (the env var becomes an
`Authorization: Bearer $VAR` header reference, resolved in the daemon's
environment at connect), and codex's `[mcp_servers.X.tools.*]` per-tool
approval tables are skipped — they're codex's config, not servers.

`whipcode skills list` shows loaded skills and where they come from;
`whipcode skills import [--dry-run]` copies skills from other harnesses'
user dirs (`~/.codex/skills`, `~/.claude/skills`) into `~/.agents/skills`,
deduped by name against everything whip already loads — an existing
skill is never overwritten, and a name duplicated across codex/claude
imports once.

## Browser — drive your real, logged-in Chrome

`browser_exec` can drive your everyday browser (real cookies/sessions) four
ways via `browser.mode` in `~/.whipcode/config.json`: `live` (attach to a
running Chrome with debugging on), `dedicated`/`headless` (a whip-owned
Chrome, auto-launched as a fallback when nothing debuggable is running), and
`extension` — the only one that works on Chrome ≥ 136's default profile,
where direct CDP is blocked.

Extension mode uses a tiny unpacked Chrome extension: whipcode runs a local
relay, the extension pipes raw CDP through `chrome.debugger` on the tab you
pin. Set it up once:

```
whipcode browser install
```

That writes the extension to `~/.whipcode/browser/extension/`, mints the relay
token, and opens `chrome://extensions` + the folder. Then three clicks
(Chrome forbids programmatic install): **Developer mode on → Load unpacked →
select the folder**. Set `"browser": { "mode": "extension" }` in
`~/.whipcode/config.json`, open the tab you want, and click the whip extension
icon (a green ● appears) to let whip drive it; click again to detach. While
pinned, Chrome shows a "whip is debugging this browser" bar — that's the
mechanism doing the work.

Gate the claude/codex imports with the `"mcpImport"` block — useful when
another app writes MCP entries into `~/.codex/config.toml` you don't want
(blocked servers stay visible in `/mcp` and `mcp list` instead of silently
loading):

```json
{
  "mcpImport": {
    "codex": { "enabled": true, "exclude": ["node_repl"] }
  }
}
```

Per source: `enabled` kills the whole source, `only` is a name allowlist,
`exclude` a denylist (wins over `only`). No block = import everything.

## Docs

How it works, from the top down:

- [architecture.md](architecture.md) — the moving parts and how a
  command moves through clients, the daemon, policy, workers, and storage.
  Start here.
- [rlm-runtime.md](rlm-runtime.md) — recursive sessions, Starlark modules,
  limits, permissions, reconnect/recovery behavior, release verification,
  and troubleshooting.
- [agent-loop.md](agent-loop.md) — `Agent.Turn` in detail: the
  stream-tools-repeat cycle, parallel tool execution, compaction, steering.
- [concurrency.md](concurrency.md) — root actors, stable commands, replay,
  fan-out, mutation ordering, child broadcasts, and process lifetime.
- [tools.md](tools.md) — the model-facing tool, host modules, and shared
  dispatcher-owned execution rules.
- [models-providers.md](models-providers.md) — provider routing,
  live model discovery, token/cost bookkeeping.
- [browser-computer-use.md](browser-computer-use.md) — driving your
  real Chrome (live / dedicated / headless / extension modes) and your Mac
  desktop.
- [goal-from-context.md](goal-from-context.md) — `/goal-from-context`:
  distill the conversation tail into a goal and let the loop finish it.
- [features.md](features.md) — the full feature map, each section
  linked to code and tests.
- [roadmap.md](roadmap.md) — what's shipped vs. what's next,
  cross-referenced to the harnesses that inspired each item.
- [learnings/](learnings/) — exploration reports from other
  harnesses (pi, opencode, exo) that informed the design.
