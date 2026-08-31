# Lifecycle hooks

Lifecycle hooks let trusted shell commands observe or stop an agent turn without
adding a plugin runtime. Whip supports five boundaries:

| Event | Matcher input | What the result can do |
|---|---|---|
| `UserPromptSubmit` | submitted prompt | block the turn or add prompt context |
| `PreToolUse` | Whip tool name | block the call or add tool-result context |
| `PostToolUse` | Whip tool name | add tool-result context |
| `PostToolUseFailure` | Whip tool name | add tool-result context |
| `Stop` | empty | ask the model to revise before finishing |

## Configuration

Whip discovers command hooks in this order:

1. `~/.agents/plugins/*/hooks/hooks.json`
2. `<project>/.agents/plugins/*/hooks/hooks.json`
3. `<project>/.whip/hooks.json`

Plugin and file names are sorted; commands within a rule keep declaration
order. Commands run serially for one event. Different tool calls still run in
parallel.

Plugin hook files use a wrapped configuration:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "^(bash|write|edit)$",
        "hooks": [
          {
            "type": "command",
            "command": "${PLUGIN_ROOT}/scripts/check-policy.sh",
            "timeout": 30,
            "on_failure": "block"
          }
        ]
      }
    ]
  }
}
```

The project-local file may omit the outer `hooks` object and accepts snake-case
or PascalCase event names:

```json
{
  "pre_tool_use": [
    {
      "matcher": "*",
      "hooks": [
        {"command": ".whip/check-policy.sh", "timeout": 30}
      ]
    }
  ]
}
```

Plugin matchers are Go regular expressions. Project-file matchers also accept
`*`, an exact tool name such as `bash`, or `/regular-expression/`. Matchers use
Whip's native tool names, including names such as `mcp__server__tool`.
`${PLUGIN_ROOT}` and the `PLUGIN_ROOT` environment variable point at the
plugin directory; for `.whip/hooks.json` they point at the project. Quote
the expansion in commands that must work when a directory contains spaces,
for example `"$PLUGIN_ROOT/scripts/check-policy.sh"`.

`async: true`, non-command actions, unknown events, invalid matchers, and
invalid actions are skipped with a visible warning. A bad entry does not
discard valid neighbors. Run `/hooks` to inspect loaded commands and warnings.

## Command protocol

Each command runs in the session working directory. It receives one JSON object
on stdin (shown here with all possible fields):

```json
{
  "version": 1,
  "event": "PreToolUse",
  "event_type": "PreToolUse",
  "session_id": "abc123",
  "working_dir": "/repo",
  "matcher_context": "bash",
  "tool_name": "bash",
  "tool_input": {"command": "go test ./..."},
  "tool_response": "...",
  "tool_error": "...",
  "tool_call_id": "call_1",
  "message": "user prompt",
  "last_assistant_message": "final draft"
}
```

Fields that do not apply to the event are omitted. Hook environment variables
are also set:

- `WHIP_HOOK_EVENT`, `WHIP_PROJECT_DIR`, `WHIP_SESSION_ID`, `WHIP_TOOL_NAME`
- `PLUGIN_ROOT`

The command inherits Whip's environment. Write diagnostics to stderr; stdout
is a strict decision channel. Empty stdout with exit code 0 allows the event.
Otherwise print exactly one object:

```json
{"decision":"allow","additionalContext":"Run the focused parser tests too."}
```

`decision` may be `allow`, `block`, or `deny`. A block can
include `reason`; exit code 2 also blocks and uses stderr as its reason. Only
`UserPromptSubmit`, `PreToolUse`, and `Stop` can block. Post-event context is
still accepted, but post-event block decisions do not undo a completed tool.

Malformed output, a timeout, cancellation, or another non-zero exit is visible
but fails open. A `PreToolUse` action may opt into fail-closed behavior with
`"on_failure":"block"`. Stop-hook failures always fail open. A Stop rejection
feeds its reason back to the model and retries at most three times.

## Trust, recovery, and limits

Project hooks are executable code. The TUI loads them only after the normal
folder-trust prompt. `/cd` and ACP load project hooks only for a directory
already recorded as trusted. `whip run` keeps its existing trusted-automation
contract and loads project hooks without an interactive prompt. User plugins
under `~/.agents/plugins` are treated as user-controlled code.

Set `WHIP_DISABLE_HOOKS=1` to disable all hook discovery for recovery or CI.
Current limits are 128 commands, 1 MiB per config, 32 KiB per command, 256 KiB
per event payload, 64 KiB per output stream, composed event context, and
environment, and a maximum 10-minute timeout. Defaults are 30 seconds for
plugin files and 60 seconds for the project-local file.

Hook subprocesses use the same process-group cancellation and exit cleanup as
the `bash` tool. Pre-hooks run before file/global mutation locks; post-hooks
run after those locks are released.
