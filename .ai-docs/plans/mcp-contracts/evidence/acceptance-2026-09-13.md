# MCP contracts acceptance run, 2026-09-13

Build: the `mcp-contracts` worktree at the B5 commit, `go build ./cmd/whip`.
Home: a scratch `WHIP_HOME` under the session scratchpad, deleted after the
run, so the real `~/.whip` was never touched. Sources: the real
`~/.codex/config.toml` and `~/.claude.json`; the project source left off.
Model for headless calls: `glm-5.2-fast` through Inference.net with the
machine key copied from `~/.whipcode/inference-net.json` (also deleted).
Credential-looking values were redacted before any output was read.

## Import and trust

| Step | Result |
| --- | --- |
| `whip mcp import --dry-run` on a fresh home | nothing to import: a fresh config disables the user sources, every discovered server listed as `blocked` |
| enable claude and codex (`exclude: ["node_repl"]`), `whip mcp import` | 18 servers materialized; output ends "they are now native: trusted like hand-written entries, no per-call consent" |
| `whip mcp list` afterwards | 18 rows `enabled … whip config`, `computer-use` `disabled` (its own flag), `node_repl` still `blocked … codex config` |

## Live probes (`whip mcp test`, in-process fallback transport)

| Server | Transport | Result |
| --- | --- | --- |
| inference (Catalyst) | HTTP, `Authorization: Bearer $INFERENCE_API_KEY` | first probe: `Unauthorized` in 955 ms with the config path named, because the shell lacked the variable and the header was dropped (the documented behaviour, also visible in `whip.log`); with the variable exported: **connected in 1.4 s, 427 tools** |
| ahrefs | HTTP, header auth | connected in 1.07 s, 135 tools |
| chrome-devtools | stdio, `npx chrome-devtools-mcp@latest` | connected in 1.83 s, 29 tools |
| firecrawl | stdio, Claude file, env reference | connected in 1.41 s, 26 tools |
| exa | HTTP, Claude file | connected in 0.76 s, 3 tools |

## End-to-end calls through a daemon (`whip run -no-session -permission-mode automatic`)

All servers were native after import, so no consent prompt was involved.
Argument validation ran against the live schema before transmission: exa
rejected `{query}` (missing `objective`) and `{objective}` (missing `query`)
with `invalid tool arguments: validating root: required: missing properties`;
chrome-devtools rejected `take_screenshot {}` (missing `pageId`); ahrefs
rejected `{target}` (missing `targets`).

| Call | Result |
| --- | --- |
| exa `web_search_exa {query, objective, numResults: 2}` | text returned: "Title: What is the Model Context Protocol (MCP)?  URL: https://modelcontextprotocol.io/docs/…" |
| ahrefs `public-domain-rating-free {targets: ["example.com"]}` | one output carrying the text payload (`domain_rating … 94.0`, the `render-scorecard` instruction) followed by the server's structured content as JSON (`{"apiUsageCosts":{…}}`) — structured content no longer dropped |
| chrome-devtools `new_page {url}` then `take_screenshot {pageId}` | `Took a screenshot of the current page's viewport.` `[image 1: image/png, 109996 bytes; handle bc391b0960b8b2f2de460616a93bd8ba]` — the image stored as a content handle owned by the caller and named in the text |

## Not exercised live

- Reconnect after killing a stdio server's process, and the three-attempt
  give-up against a stopped HTTP server: covered by
  `TestManagerAutoReconnectRecoversAfterFailedRedial` and
  `TestManagerAutoReconnectGivesUp` with in-process servers.
- A `[[table]]` and a malformed server table in the real Codex file: covered by
  `TestParseCodexIgnoresUnrelatedSections` and `TestSourceErrorsAreStatusRows`;
  the real file was not edited.
- Image parts reaching the root's next turn as vision input: the sink call is
  covered by `TestMCPAttachmentsBecomeHandlesAndImagesSteer`; the headless run
  ended the turn after printing, so no following turn observed the image.

## Harness note

One headless run appeared to hang for eight minutes. Its goroutine dump
showed the client in `io.ReadAll(os.Stdin)` inside `whip run`: the tool
harness had moved the command to the background without closing stdin, and
`whip run` appends piped stdin to the prompt. Redirecting stdin from
`/dev/null` fixed it. Not a whip defect.
