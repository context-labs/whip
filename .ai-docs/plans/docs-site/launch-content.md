# Launch content and provenance

Prepared 2026-09-24 against the `feat/docs-site` worktree based on `071fe7e34e75b04fcce19af20f47e5525fb46db7`. This is implementation guidance, not a public article or a new source of product behavior.

## Landing page copy

Use the following copy in `/`. Keep the page restrained: one hero, two actions, three capability descriptions, and a short choice of interfaces. No pricing, account dashboard, invented product URL, benchmark claim, download asset name or community link is needed.

- **Title / H1:** A coding agent for work beyond one context window.
- **Lead:** Give whipcode a task, let it work across your project, and follow the agents doing the work. Use Desktop or the terminal with your own model connection.
- **Primary action:** Get started → `/docs/getting-started`
- **Secondary action:** View on GitHub → `https://github.com/context-labs/whip`

### Capability descriptions

1. **Delegate focused work.** Agents can split a task into smaller jobs and exchange results while you keep the original goal in view.
2. **Keep context within reach.** Search history, files and large tool outputs without putting everything into one prompt.
3. **Inspect and steer.** Follow the conversation and execution trace, review tool approvals and send a new instruction when the work needs direction.

### Interface section

- **Heading:** Choose how you work.
- **Desktop:** Work with conversations, project files and agent activity in one window. Desktop Beta runs on Apple Silicon Macs with macOS 14 or newer.
- **Desktop action:** Use Desktop → `/docs/using-whipcode/desktop`
- **CLI:** Start an interactive terminal session or run a task from a script. The standalone CLI supports macOS and Linux on x64 and arm64.
- **CLI action:** Use the CLI → `/docs/using-whipcode/cli`
- **Closing note:** Model access is separate from installing whipcode. Connect an API-key provider or a supported account login.
- **Optional installation action:** Installation → `/docs/installation`

For `/docs`, use **Documentation** as the H1 and **Install whipcode, connect a model provider and choose how your agents work.** as the lead. Generate article cards from manifest metadata, grouped by the three contract sections rather than maintaining a second page list.

## Six-page launch inventory

| Slug | Section / order | Reader outcome |
| --- | --- | --- |
| `getting-started` | `start` / 1 | Choose an interface, connect a provider and send a bounded first task. |
| `installation` | `start` / 2 | Install Desktop Beta or the explicit prerelease CLI; identify update ownership. |
| `using-whipcode/desktop` | `usage` / 1 | Start and inspect sessions, distinguish closing a window from stopping work. |
| `using-whipcode/cli` | `usage` / 2 | Run interactively or headlessly, reconnect, inspect the daemon and open local web access. |
| `configuration` | `reference` / 1 | Configure a provider, credential source, custom endpoint and new-session defaults. |
| `tools-and-permissions` | `reference` / 2 | Understand tool scope, Ask/Full Access, MCP trust and browser/computer consent. |

All six articles live at `apps/docs/src/content/docs/<slug>/index.mdx`. Their frontmatter has only `title`, `description`, `section`, and `order`. The layout supplies H1; article H2/H3 headings generate the TOC. No MDX imports are required: provide `Callout`, `CodeTabs`, and `CodeTab` through the agreed component mapping.

The installation article has `CodeTabs > CodeTab label` with actual Bash fences in both tabs. The content uses `bash` and `json` fences only, both supported by the compiler. It does not add artificial Go/Python/TypeScript samples just to exercise syntax grammars; pipeline tests should cover all supported languages. GFM tables and info, warning and alert callouts appear across the articles. Internal links are `/docs/<slug>` without trailing slashes; fragment links match H2 text.

## Source provenance

References below are repository-relative at the audited worktree. These identify authoring evidence, not public URLs that readers must follow.

| Claim | Evidence inspected | Editorial decision |
| --- | --- | --- |
| Product purpose, recursive delegation, bounded context, trace and daemon ownership | `README.md:57–70` | Retain plain-language capabilities. Do not copy evaluation or cost-performance claims. |
| Desktop Beta download destination, app name, platform and initial setup | `README.md:26–36`; `docs/desktop.md:1–7`; `docs/setup.md:189–224` | Link to the existing GitHub Desktop release search, not a guessed DMG. State Apple Silicon/macOS 14+, beta, and fresh installation. |
| CLI distribution and prerequisites | `docs/setup.md:9–37`; `install.sh:5–10,19–51,63–64,180–200`; `docs/README.md:20–32` | Use the exact prerelease installer URL and channel. No unverified exact release tag. No unsupported bare `go install` recommendation. |
| Backend update ownership and shared-work interruption | `docs/setup.md:152–175,189–224` | Separate Desktop-managed, standalone, manually selected and remote installs. Do not imply app copying enrolls an existing executable. |
| Provider onboarding and masked setup | `docs/setup.md:39–75`; `docs/models-providers.md:63–86` | Describe the flow without baking in model recommendations that can change. |
| Provider compatibility and credentials | `docs/models-providers.md:88–147`; `internal/config/config.go:143–165` | Distinguish API keys from ChatGPT login; do not claim native Anthropic/Gemini support or successful billing merely from discovery. |
| File-backed key discovery | `docs/setup.md:77–96`; `docs/models-providers.md` local key discovery section | Publish a valid config fragment with local example paths, no key literals, no invented hosted endpoint. Tell readers to merge, create/manage their own files, refresh and reload appropriately. |
| Config home, permission default and engine | `internal/config/config.go:143–165,202,659–668`; `docs/setup.md:109–132` | Use current `~/.whipcode`, not legacy `~/.whip`. Distinguish tool execution language from project language. |
| Terminal commands and flags | `internal/tui/registry.go:25–60`; `cmd/whip/run.go:28–71`; `cmd/whip/main.go:73–87,216`; `cmd/whip/sessions.go` | Flag-first shell examples; explain NDJSON and `--resume` restrictions. Use configured model instead of guessing an available model ID. |
| Noninteractive permission behavior | `internal/daemon/client_control.go:1484–1497`; `docs/tools.md:234–235` | Default example explicitly uses prompt mode; explain that actions needing new consent are denied, not interactively approved. Automatic mode is described but not embedded in a copy-first command. |
| Desktop tab shortcuts and lifecycle | `docs/desktop.md:13–36,194–206`; `packages/app/src/details/observation.tsx:88` | Explain window closure versus stopping work. Do not promise completion notifications. |
| Permission boundaries, inheritance, remembered rules | `docs/features.md:265–295`; `packages/app/src/permission-mode.tsx:25–35`; `docs/tools.md:257–269` | Emphasize Ask is not a shell/OS sandbox; Full Access can reach outside the project; saved rules differ from mode. |
| MCP source trust and startup execution | `docs/README.md:119–155`; `internal/config/config.go:259–286,629–638`; `docs/tools.md:209–249` | Native entries are trusted; enabling programs is itself a trust decision. State fresh Claude/Codex imports off and project opt-in, not that every external source is disabled. |
| Desktop Browser permission and isolation | `docs/browser-computer-use.md:39–91`; `docs/desktop.md:208` onward | Label Browser tabs experimental; listing is not control, child attachments require explicit delegation. Do not republish legacy browser setup commands. |
| Computer access | `docs/browser-computer-use.md:99–133`; `docs/features.md:265–272` | Keep to macOS app/OS consent; no zero-setup or universal-browser promises. |
| Local web gateway lifecycle | `README.md:51–55`; `docs/setup.md:158–170,226–230` | Document foreground/loopback behavior and Ctrl+C scope; omit remote hosting instructions from the six-page launch. |
| Usage and budget limitations | `docs/models-providers.md:479–497`; `cmd/whip/run.go:33–34` | Unknown cost is not free; whole-session budgets include descendants and are not an absolute provider-billing guarantee. |

## Audit findings and publication limits

- The initial audit found legacy executable/home names in `docs/browser-computer-use.md`. A focused follow-up corrected prose, `whipcode browser install`, `~/.whipcode/config.json` and the extension path. Legacy `browser_exec`/`computer_exec` tool identifiers and the extension's actual displayed `whip` name remain unchanged. Public articles use capability descriptions rather than copying legacy tool examples.
- The initial audit found `docs/README.md` claiming Claude/Codex imports were on by default, while `config.Default()` explicitly disables both on a fresh install. A focused follow-up corrected those two bullets and a stale `whip mcp import` command in `docs/tools.md`. Public copy follows current setup/source and does not generalize the claim to OpenCode, whose default differs.
- Source/worktree documentation establishes intended installation behavior. This audit did not download or execute a released installer, verify live asset availability, validate notarization, test a signed app update, or make paid model calls. Check the chosen release notes and actual assets before publication.
- There is no canonical site origin or hosting provider approved here. Do not create canonical-domain claims or external URLs from this copy. Existing verified external destinations used in articles are GitHub repository/release/raw-script URLs and Task's official site.
- This focused public collection does not migrate or replace the full engineering manual. Provider protocol internals, release operations, evaluations, reset procedures, remote networking, and exhaustive runtime/MCP APIs stay out of launch scope.
- Ordinary doc tasks do not require all syntax languages in user-facing content. Bash/JSON are the useful launch examples; compiler tests own the broader grammar matrix.

## Validation

- All six articles compiled with the scaffold's actual MDX options, including GFM, frontmatter, shared heading transform and build-time Refractor adapter.
- The scaffold's `loadDocuments()` validated metadata, exactly six routes, section/order uniqueness, supported explicit fence labels and all internal links/fragments.
- Compiled examples retain `data-raw`; three articles contain syntax token spans and installation compiles its nested code tabs. A bare `whipcode` command correctly needs no token spans. An initial assertion incorrectly required tokens in every code article; correcting that assertion produced a pass for all six pages (31 headings, one tabbed article, four code articles).
- No model calls, installer execution, package edits, route edits, commits, pushes or deployments were performed by the content lane. End-to-end browser/copy/no-JS checks remain the integration lanes' responsibility.
