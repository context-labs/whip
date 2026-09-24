# Legacy intent and port ledger — 2026-09-24

Reference inventory, not a merge queue or a claim of missing RLM functionality.
`whip-v1` is **read-only reference material**: no legacy CI, maintenance releases,
or compatibility support. Old-only changes remain deferred. Any selected intent
gets a new, narrowly scoped main PR with current-runtime evidence and tests;
never merge the old branch back wholesale.

## Frozen inputs

Remote archive **tags**, verified read-only on 2026-09-24:

- `archive/main-20260924`: `3c7ff8f76a37fe2c014477c0a0e23d0aa09b80d2`
- `archive/rlm-20260924`: `9edec15d90b6d1dd7ec839acb5bea17f6bf3f634`
- Common ancestor: `37188abf3c0d77fc3c40073cd340e6d92d967ed9`

The frozen comparison contains 100 main-only commits: **71 non-merge commits**
below and 29 merge commits. RLM-only: 324. The two tips preserve 980 distinct
reachable commits. Ancestry and patch identity are not semantic parity: RLM has
independent implementations of several intents. Use these frozen tags, not
post-cutover `main..whip-rlm`, to reproduce the inventory:

```sh
git log --reverse --no-merges --format='%H%x09%s' \
  archive/rlm-20260924..archive/main-20260924
```

## Commit inventory

Categories describe intent, including its tests/docs: binary safety (11),
providers/auth (16), workflow (7), TUI/resume (20), themes (9), deps/CI/docs (8).
Every initial status is **deferred/unreviewed**. A later disposition should link
its issue/owner, current equivalent or reproduction, and accepted PR/test evidence;
"already covered" and "not applicable" are valid outcomes.

| Original SHA | Category | Original subject | Status |
| --- | --- | --- | --- |
| `fc6d79ebe8c726a10cc2e1eab4fb4f8bccf8281e` | Workflow | Add dynamic workflows: a workflow tool for deterministic multi-agent orchestration | deferred/unreviewed |
| `4881fffdef5a69fed84abe24e3816d8baf112c09` | Binary safety | fix(tools): replace binary tool output with a compact [binary] placeholder | deferred/unreviewed |
| `ed0143d4161c7b049f82e0b926c5da471869a3d4` | Binary safety | lint: modernize binary probe length clamp to min() | deferred/unreviewed |
| `9d97491df4fb907a2480bfdd3a34cdfa12f64553` | Binary safety | fix(tools): UTF-8 probe-boundary straddle + ANSI-colored output no longer read as binary | deferred/unreviewed |
| `3fcd1e182af7b805101b33e761afd3d2bf5aa59d` | Binary safety | fix(tools): scan the whole buffer for NUL, not just the 1KB probe | deferred/unreviewed |
| `4b912ae5320e2996d7c8a00a2e42f94d64bd8e6f` | Binary safety | fix(tools): trimToLastRune only backs off a trailing incomplete rune (utf8.UTFMax cap) | deferred/unreviewed |
| `8a092a8ec65ca20d4cdb24e0635e021c1304f42f` | Binary safety | fix(tools): trimToLastRune only trims a valid rune prefix, not a genuine invalid byte | deferred/unreviewed |
| `14356277410168cba27d86e414103f1e110e7471` | Binary safety | fix(tools): trimToLastRune validates the full rune prefix (RFC 3629), not just the lead length class | deferred/unreviewed |
| `f050be569a68f786e2f0be9629def36e4060afa8` | Binary safety | fix(tools): validate UTF-8 over the whole buffer; keep exit/timeout suffixes on binary bash output | deferred/unreviewed |
| `cc793d436ce73cb7287db4e3ad6cc9ae2f0b3f0e` | Binary safety | fix(tui): gate the `!` shell escape through the same binary sniff as the bash tool | deferred/unreviewed |
| `aaf069cfa15179b94e6ac01ce4f3292c360d034a` | Binary safety | refactor(tools): remove dead probe-cut machinery now that checks are whole-buffer | deferred/unreviewed |
| `30784f157f77e275e7274f724819f2cbe729d618` | Binary safety | test(tui): make the shell-escape binary test portable | deferred/unreviewed |
| `67356164a992d6ff073eb9ec26bcf60f4bcc5d4e` | Deps/CI/docs | chore(deps): bump the gomod-minor-patch group with 2 updates | deferred/unreviewed |
| `421d7098b5b26a70c8322e54e2ffc484822c9721` | Deps/CI/docs | chore(deps): bump Infisical/secrets-action in the actions group | deferred/unreviewed |
| `8b7ae9a8c0f9c34ea18db4468e6932093c48a880` | Deps/CI/docs | chore(deps): bump actions/checkout from 5 to 7 | deferred/unreviewed |
| `8b15e7b96c03613900501b0df16befbcf81a1770` | Providers/auth | Merge PR #14 (codex subscription) onto main | deferred/unreviewed |
| `45151eef5989f26565230093737daf1cfca6cb8c` | Providers/auth | test: cover Codex Clone/Endpoint and prompt_cache_key | deferred/unreviewed |
| `60e96e6b302a45114e90ab0542685fda3828a94d` | Providers/auth | refactor: share provider client constructor between run and TUI | deferred/unreviewed |
| `da42cd999fcfd6f30300a175b7b6b5588a107d1f` | Providers/auth | fix: default Codex route to gpt-5.5 | deferred/unreviewed |
| `e66bb837252335914ca39cb7eedaa81630b9e95e` | Deps/CI/docs | chore(loupe): halve maxTurns and drop reasoning one tier (cost) | deferred/unreviewed |
| `722d7940c9c7b3a9f4dbfbbce221e1c20ce3ea30` | Providers/auth | auth: add logout for openrouter/codex, --help, group catalog routes by provider | deferred/unreviewed |
| `cfdd97b09ddd3ab5ae7e0fa325723243d598ccd5` | Providers/auth | tui: label the Codex endpoint as 'ChatGPT Codex subscription' in model pickers | deferred/unreviewed |
| `d35b76727c942b33092e3ca0fab2a0b3e35a3e7b` | Providers/auth | codex: /usage, codex-subscription provider name, generalize subagent routing | deferred/unreviewed |
| `8d5b8e5b7fedbfc2ca45b7e129cc0af2ed3fe6de` | Providers/auth | lint: canonical header in test, avoid predeclared min, gosec-friendly logout check | deferred/unreviewed |
| `99e9bf6425de23da876e7c7784735f385f66c018` | Providers/auth | palette: compact/subagent pickers show model@provider routes like /model | deferred/unreviewed |
| `88dec5d8f34ec3001a1607a5635e391b4ba4178f` | Providers/auth | codex: retry transient failures, robust logout, safe auth-file save, login-result ownership | deferred/unreviewed |
| `0bfdb00c9d887b67a9e9cb67bd01cb1c58d61f9f` | Providers/auth | codex: drop truncated tool calls on early EOF, read flat error events | deferred/unreviewed |
| `088fbaa32cc847ec6db354d0dc9eac278e23e6ef` | Workflow | docs: tighten the dynamic workflows section in features.md | deferred/unreviewed |
| `0e0f038534831a3964f6f9203505bff0fbf5a8e6` | TUI/resume | feat(config): boot new installs in opencode UI mode | deferred/unreviewed |
| `5308fe18a1014aead57f896ffbdde6b7092cdc68` | TUI/resume | feat(config): default UIMode to opencode for new installs | deferred/unreviewed |
| `da599c165ea84fb802adbe1086d3ca478321ab84` | Workflow | feat: gate the workflow tool behind an experimental config flag | deferred/unreviewed |
| `ed1c8f81a1c8a0e194a5d71d206e8468323232b9` | TUI/resume | feat: whip -c/--continue, -r/--resume shorthand, and --browse session picker | deferred/unreviewed |
| `48332f969a9c2d39945b75ebe99f988841a9fea8` | TUI/resume | fix(tui): shorten the cwd so the spend survives status-line truncation | deferred/unreviewed |
| `5f395cf753be90f398844c31148fe170c1c9f19d` | TUI/resume | fix(lint): range over int + drop empty-branch in picker test | deferred/unreviewed |
| `84b30c0e71ed08f0d0b560ecfc4fa66db772d471` | TUI/resume | docs: trim startup resume flags section to the essentials | deferred/unreviewed |
| `a0f98963a032270401fdea44c5e963b0e1a90fb0` | Workflow | fix(workflow): address CI race + lint, and PR #132 round-2 review | deferred/unreviewed |
| `18286ba75a8f11542f5908f759a1db1bdb1bb97b` | Workflow | test(workflow): cover runWorkflowAgent schema path, Manager.Stop, model-override | deferred/unreviewed |
| `e82845ed06f95d7913e5a6ecd808f25fa9968af5` | Workflow | test(workflow): lift coverage above the 90% floor | deferred/unreviewed |
| `49ea95a64bb4b2e9227b35406b208c2f5b7eaa72` | Deps/CI/docs | docs(ascii): fix the README wordmark to spell whip | deferred/unreviewed |
| `4f6016824c933f8ba4a8cb7c3157ab333b80d134` | TUI/resume | palette: window compact/subagent route lists to the terminal height | deferred/unreviewed |
| `f507670719b43bf9fcfd98980779abc97df84759` | TUI/resume | palette: window the Model panel to the terminal height | deferred/unreviewed |
| `5b7effc855d73fcba99d59754fa8b2cdd9f323f6` | Deps/CI/docs | chore(loupe): switch main review model to glm-5.3 (cost) | deferred/unreviewed |
| `dcb5968b6cbbb0daf01ca278b20ba816e9867b0f` | TUI/resume | tui: /model picker windows on the real selection row | deferred/unreviewed |
| `5d507ddc69f162a7e389063dedb090a37b6a0a44` | Providers/auth | docs: cover /usage, auth logout/--help, subscription subagent default | deferred/unreviewed |
| `01e223f668e581e36eb4f375deeeaf5b18ec1047` | Providers/auth | config: resolve catalog models before applying defaultProvider | deferred/unreviewed |
| `a23e6309e96c917775944d0e7536cefb54e205d5` | Providers/auth | llm: one retry policy for every provider; Codex 401 refresh, usage limits, server_error | deferred/unreviewed |
| `33635a09f148400beb1bee58cd39a789151fd229` | Providers/auth | llm: tool-call rows count as shown output; zero usage on failed stream; stripInternal copies ToolCalls | deferred/unreviewed |
| `ce0837002aa74f0280dc2eed3ba8bd1eca01aad9` | Providers/auth | docs: Codex subscription limits, retries, and logout in models-providers | deferred/unreviewed |
| `acb32049d0110405b3fcc0a315a63d41a29d9836` | TUI/resume | fix(tui): keep inline view bottom-anchored | deferred/unreviewed |
| `600670d7a872cdf62c302bd91f01a5b5adac3731` | TUI/resume | fix(tui): cap inline frame height | deferred/unreviewed |
| `8dd61be6f41708a3bb501682faaed8af021d65ca` | TUI/resume | test(tui): model inline renderer clipping | deferred/unreviewed |
| `b4575789143bb590443ff48d8b0d3fe9ad926507` | TUI/resume | fix(tui): bottom-anchor opencode view so the final markdown flush doesn't jump | deferred/unreviewed |
| `b23abf711bf461c11abedde22d31e0fe6083b4c3` | TUI/resume | fix(tui): splice opencode overlays onto the anchored frame, not the content | deferred/unreviewed |
| `437951fd54b7965acdc798cf59ac1833b0487883` | TUI/resume | test(tui): use max for wantTop clamp (modernize) | deferred/unreviewed |
| `1c4f284cceed3416f24d9e84454c8b3e95dd4d28` | Deps/CI/docs | ci(loupe): run loupe from main instead of the v0 tag | deferred/unreviewed |
| `7d4a016b821ee2a3b6322e34d85d546fee37e32a` | TUI/resume | fix(tui): budget live streaming area by actual height, not full cap | deferred/unreviewed |
| `39dbd4adca11a7449de58cca3528485547734341` | TUI/resume | fix(tui): keep assistant marker on the live partial for the whole turn | deferred/unreviewed |
| `dcf0642a95b487762e8d5ee8c7a5f1cbffeb8a5a` | TUI/resume | test(tui): use integer range in stream test (intrange) | deferred/unreviewed |
| `6d7b25d3b6d43d12fa2581d558be6b3d1572f961` | TUI/resume | fix(tui): match committed block's hanging indent on the live partial | deferred/unreviewed |
| `5e555e5a80add3e20c1b3bcdafb1440fb26317de` | TUI/resume | fix(tui): pass OSC 8 hyperlinks through the drag-selection highlight (#147) | deferred/unreviewed |
| `0a3f243560d1359ec17f420dcdb43bab89ee6b15` | Themes | feat(theme): add built-in theme catalog | deferred/unreviewed |
| `d32151bd44bf1f564baff5fa5fb32d8a937ee95e` | Themes | feat(tui): add live theme selection and preview | deferred/unreviewed |
| `a0cb2d650ffedd73fb6847c4606623164c0420a1` | Themes | docs: document built-in terminal themes | deferred/unreviewed |
| `03b7ab482556647040652e2b78c20b0d07187dbf` | Workflow | fix(workflow): persist failures and lock workdirs | deferred/unreviewed |
| `baee8cbf9c835377811a7cde8bac77bea7306076` | Themes | fix(tui): apply theme colors throughout UI | deferred/unreviewed |
| `265fc143f4e98536726b8188b03b60f1c429d0c5` | Deps/CI/docs | test: restore portable coverage floor | deferred/unreviewed |
| `a9ffd19247a1481b9ccdd85b755a58093d8fb5ad` | Themes | fix(theme): validate ANSI index bounds before conversion | deferred/unreviewed |
| `6a30b2097dc46eaf43ac3b5f9206725ff964b2cd` | Themes | fix(theme): remove mislabeled light palettes | deferred/unreviewed |
| `c21e1bb94822f2d702f749b736a13afe59714641` | Themes | perf(tui): cache themed transcript rendering | deferred/unreviewed |
| `9969dd20c35d2341068f1cce6f981c4c47381fb0` | Themes | fix(tui): stabilize live theme preview | deferred/unreviewed |
| `ab06efd8b408c47ac86ee2cbc841682855c4c3bd` | Themes | fix(tui): truncate theme labels before styling | deferred/unreviewed |

## Open old-main PR triage

Read-only snapshot, 2026-09-24 18:08 UTC. All ten PRs were **OPEN** with base
`main`. Recommendations below are for maintainer review only: none was closed,
retargeted, rebased, fetched or merged. No old-main PR should be retargeted to the
read-only `whip-v1` branch. Keep author credit and original PR links on any port.

Evidence scope: PR titles/descriptions, up to 100 changed-file names, and head
metadata exposed by `context-labs/whip`; no private fork repository reads or
fork checkouts. #102 has 545 changed files, so its file listing was incomplete;
its closure recommendation instead uses exact local ancestry. Descriptions are
claims, not independently validated behavior. Recorded head SHAs identify the
reviewed proposal; this document does **not** create protected refs for PR heads.

| PR / original title | Observed head repository:branch @ SHA | Candidate disposition / next check |
| --- | --- | --- |
| [#9](https://github.com/context-labs/whip/pull/9) fix: make the bash global lock actually exclude path mutations | `beardthelion/whip:fix/tool-lock-and-mcp-close`<br>`c2728e02dd55fa09d7da3a61a4e5fdd0dcf2e6b1` | Port candidate: preserve the bash/write exclusion, parallel-write and real MCP-close regression cases; reproduce against RLM dispatch before selecting any code. Old `internal/agent/filelocks.go` is not the integration target. |
| [#73](https://github.com/context-labs/whip/pull/73) feat(hooks): implement portable lifecycle hooks for command execution | `Unmesh100/whip:main`<br>`1ac6eb116b3485daa2b9ddb9abdb2783e2749462` | Keep as design input; port only agreed gaps. RLM already has executor hooks (`docs/features.md:60-74` at the RLM snapshot), but these command hooks have different events/config/trust semantics. Do not bulk-merge the 36-file legacy-loop integration. |
| [#80](https://github.com/context-labs/whip/pull/80) Port/tps gauge | `anishthite/whip:port/tps-gauge`<br>`fb6a19d7173b1f2b0701156de9748e854adb9233` | Keep pending author scope clarification; then consider an isolated throughput UI port. Empty PR description and 55 changed paths include auth, themes, hashline editing and locks, not just the gauge. Do not merge the mixed branch. |
| [#92](https://github.com/context-labs/whip/pull/92) fix(llm): send whip user agent | `zerone0x/whip:fix/user-agent-whip`<br>`bf4a4e6d4cab719181f0ec1d116f55a94f2638ff` | Small port candidate: check current provider header behavior and use the final WhipCode identity consistently. Preserve model-list/stream/completion coverage; do not assume the old `User-Agent: whip` value is correct. |
| [#102](https://github.com/context-labs/whip/pull/102) feat(runtime): establish durable swarm runtime foundations | `context-labs/whip:feat/rlm-runtime-u4-clean`<br>`9b2aa6dfea57dc0985c24d20ce1bbbbf3bbec7d7` | Close candidate as already represented in preserved RLM history: this exact head is an ancestor of the RLM snapshot (`git merge-base --is-ancestor` succeeds). Keep the PR and linked #94 as historical design/review evidence, not a second runtime merge. |
| [#103](https://github.com/context-labs/whip/pull/103) feat(tui): defer the whip up trust gate into the TUI when no terminal | `context-labs/whip:feat/whip-up-in-tui-trust`<br>`d1789a0b6678f4ff0c48536283c6376ba9f9e092` | Close candidate as obsolete flow: RLM startup deliberately has no folder-trust prompt (`docs/features.md:394-398` at the snapshot). Preserve the piped-input/prompt-loss scenario for current startup regression review; do not restore the old trust gate. |
| [#125](https://github.com/context-labs/whip/pull/125) feat: add optional you.com search integration | `mouse-value-add/whip:feat/youcom-search-integration`<br>`33c219f77886604380b187ae16649b1ccdf2b5b8` | Docs/skill port candidate if the integration is still desired: validate the remote MCP example, credential-reference semantics and new namespace; retain opt-in/no-secret guidance. No reason to resurrect the legacy loop. |
| [#158](https://github.com/context-labs/whip/pull/158) chore(deps): bump Infisical/secrets-action from 1.0.17 to 1.0.18 in the actions group | `context-labs/whip:dependabot/github_actions/actions-4494371661`<br>`f7d41d87c6a1517605b60073f18c8b8a0215e823` | Close candidate after clean-cutover removal is accepted: its only two files are the retired Loupe chat/review workflows. Do not restore those workflows merely to update their Infisical action. |
| [#159](https://github.com/context-labs/whip/pull/159) chore(deps): bump the gomod-minor-patch group across 1 directory with 7 updates | `context-labs/whip:dependabot/go_modules/gomod-minor-patch-f0d44ebe56`<br>`d851d2c3c4f02f5e33bc72d33e8c2848b32cd64c` | Keep dependency intent; regenerate/rebase a fresh update against final main, then run current CI. Seven old-base dependency changes may already differ from RLM; do not apply the old go.sum wholesale. |
| [#161](https://github.com/context-labs/whip/pull/161) run: wire MCP servers in headless `whip run` | `AdamGeorgesForges/whip:fix/run-headless-mcp`<br>`47b7d3a34265cfd24d0866dc7f77436aa2d2050c` | Port the acceptance scenario, not old startup wiring: verify configured MCP tools are available on the first headless turn, with bounded readiness/failure reporting, through the daemon-owned RLM path. Only implement a current-runtime fix if reproduced. |
