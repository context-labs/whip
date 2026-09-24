# Real TUI acceptance

September 9, 2026. Passed using the current `./cmd/whip` build in a real PTY,
with a local OpenAI-compatible HTTP fixture and disposable project directories.

Tested binary SHA-256:
`2d270e6463e7674a1c823cbafa1936009077b7218f919286fde4c2edcf0a024c`.
The complete run was repeated after the final source changes, including the
post-lock path revalidation fix; the result below is from that final build.

## Observed behavior

1. `whip --yolo up <prompt>` displayed **full access** and the synthetic model.
   The provider issued a real `rlm_exec` cell that listed a sibling directory,
   read its seed file, and wrote `first_pass.txt` there. The tool returned `True`,
   the expected file content existed, and the final response appeared in the TUI.
2. The first TUI exited and its isolated daemon was stopped.
3. `whip --resume <same-session-id> up <prompt>`, **without `--yolo`**, displayed
   full access and completed the same sibling operations, writing
   `resume_pass.txt`. Its final response also appeared in the TUI.
4. Direct inspection of the fixture database confirmed schema **14**, exactly
   one session, unchanged session identity/cwd, and saved mode **automatic**.
5. Both TUI processes exited; the fixture daemon and HTTP server stopped. A
   process-list check found no remaining acceptance processes.

The provider received one `/v1/models` catalog request and five
`/v1/chat/completions` requests, all for `fixture-model`. This includes the title
request and two tool/final-response pairs. Both Starlark results were `True`.

## Isolation and reproducibility

The child processes received a minimal environment with separate `HOME`,
`WHIP_HOME`, and `WHIPCODE_HOME`. Configuration contained only a loopback provider
and a synthetic key; external MCP imports were disabled. The fixture project was
explicitly trusted. No installed app, production daemon, saved provider account,
or user database was changed. No paid models were called.

Run from the repository root:

```sh
go build -o /tmp/whip-filesystem-access-tui ./cmd/whip
python3 .ai-docs/plans/session-filesystem-access/evidence/tui_acceptance.py \
  --binary /tmp/whip-filesystem-access-tui \
  --output .ai-docs/plans/session-filesystem-access/evidence/tui-result.json
```

[Machine-readable results](tui-result.json) contain the session ID, observed
terminal markers, tool results, and request metadata. Temporary raw terminal
output contains only synthetic fixture data and stays in the fixture directory
recorded there. [The harness](tui_acceptance.py) bounds each TUI run and cleans up
its processes and server even if an assertion fails.

This proves actual TUI-to-daemon-to-Starlark execution and persistence. The
broader policy matrix—including Ask denial, retained children, and navigation
after downgrade—is covered by the daemon/store regression tests.
