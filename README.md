```
        ▄ ▄   ▄ ▄ ▄ ▄   ▄
        █ █   █   █ █▀▀▀█▀▀▄
        ▀█▀ ▄▄█   █ █   █
        whip — a fast coding-agent harness in Go
```

An LLM tool-use loop (bash / read / write / edit / subagent), an interactive
bubbletea session, and provider-routable models. One binary, no runtime,
config you can read.

## Why whip

- **Agent harnesses should be FAST** — literally as fast as possible. whip is
  built in Go around that constraint: parallel tool calls, streaming
  everything, nothing between you and the model but a loop.
- **Defaults across other harnesses suck.** They all have awesome patterns,
  but none of them bring them all together. whip cherry-picks the best ideas
  from pi, opencode, codex, and exo into one opinionated harness
  (see [docs/roadmap.md](docs/roadmap.md) — every feature cites its source).
- **Go is great for networking-heavy applications**, and harnesses do a whole
  lot of networking. whip leans on channels where the TypeScript reference
  designs hand-roll promises — per-path file locks and background subagents
  collapse into primitives the compiler checks
  ([docs/concurrency.md](docs/concurrency.md)).
- **whip is focused on a future where open-source models are the preferred
  models.** Keeping up with those models is hard — whip brings you an
  opinion on what model you should be using: live discovery from every
  provider's catalog, new models surfaced in the picker, a fast default that
  tracks the frontier.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/context-labs/whip/main/install.sh | sh
```

Checksum-verified prebuilt binaries (Linux/macOS, x64/arm64). Or from source
(Go ≥ 1.27):

```sh
go install github.com/context-labs/whip/cmd/whip@latest
```

Then `whip` and you're in. Defaults to inference.net models — any
OpenAI-compatible endpoint works as a provider. One command wires up
OpenRouter's whole catalog (`/model` lists every model, no per-model
config):

```sh
whip auth openrouter   # masked key prompt — or /auth openrouter in-session
```

To update to the latest release later, run `whip update` — it re-runs the
install script above.

## First things to try

```
/context-doctor     audit what a fresh session injects, in tokens
/goal <text>        work until done
/model              pick a model — type to filter (new) entries come from the
                    provider catalog, no config needed
```

Drop a `.mcp.json` in your repo and MCP servers just appear (`/mcp` to see
them). ctrl+c once interrupts; twice quits.

Sessions save automatically. Use `whip -c` to continue the most recent
session in this directory, `whip -r` to browse this directory's previous
sessions, or `whip --resume <id>` to open a known session from `whip sessions`.

## Docs

The full setup, config reference, MCP, browser/computer-use, and how
everything works: **[docs/README.md](docs/README.md)**.

Highlights:

- [docs/architecture.md](docs/architecture.md) — the moving parts, keystroke
  to tool call
- [docs/agent-loop.md](docs/agent-loop.md) — one loop, one function
- [docs/concurrency.md](docs/concurrency.md) — channels where others use
  promises
- [docs/features.md](docs/features.md) — full feature map, linked to code
  and tests
- [docs/roadmap.md](docs/roadmap.md) — shipped vs. next, sources cited
