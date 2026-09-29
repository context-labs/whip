// Package acp serves whipcode as an Agent Client Protocol agent: the bridge
// between the editor-facing JSON-RPC protocol (github.com/coder/acp-go-sdk)
// and whip's agent loop. translate.go holds the pure conversions — no I/O,
// no connection state — so the wire mapping is trivially testable.
package acp

import (
	"encoding/json"

	acp "github.com/coder/acp-go-sdk"
)

// toolKind maps whipcode tool names to ACP tool kinds (protocol-notes.md §5).
func toolKind(name string) acp.ToolKind {
	switch name {
	case "read", "files.read":
		return acp.ToolKindRead
	case "write", "edit", "files.write", "files.patch":
		return acp.ToolKindEdit
	case "bash", "shell_start", "workspace_process", "execute", "shell.run":
		return acp.ToolKindExecute
	default:
		// browser_exec, computer_exec, mcp__* tools: "other" is honest.
		return acp.ToolKindOther
	}
}

// pathArg extracts the file path a tool call touches, for the location list.
func pathArg(name, args string) string {
	switch name {
	case "read", "write", "edit", "files.read", "files.write", "files.patch":
	default:
		return ""
	}
	var a struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return ""
	}
	return a.Path
}

// toolTitle is the one-line human summary the client shows on the tool card.
func toolTitle(name, args string) string {
	if p := pathArg(name, args); p != "" {
		verb := map[string]string{"read": "Read", "write": "Write", "edit": "Edit", "files.read": "Read", "files.write": "Write", "files.patch": "Patch"}[name]
		return verb + " " + p
	}
	switch name {
	case "bash", "shell.run":
		var a struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal([]byte(args), &a); err == nil && a.Command != "" {
			return "$ " + a.Command
		}
		return "Run command"
	}
	return name
}

// startToolCall builds the `tool_call` session update for a call about to run.
func startToolCall(id, name, args string) acp.SessionUpdate {
	opts := []acp.ToolCallStartOpt{
		acp.WithStartKind(toolKind(name)),
		acp.WithStartStatus(acp.ToolCallStatusInProgress),
	}
	if p := pathArg(name, args); p != "" {
		opts = append(opts, acp.WithStartLocations([]acp.ToolCallLocation{{Path: p}}))
	}
	var raw any
	if json.Unmarshal([]byte(args), &raw) == nil {
		opts = append(opts, acp.WithStartRawInput(raw))
	}
	return acp.StartToolCall(acp.ToolCallId(id), toolTitle(name, args), opts...)
}

// endToolCall builds the terminal `tool_call_update` for a finished call.
// result is the exact string fed back to the model.
func endToolCall(id, name, args, result string, failed bool) acp.SessionUpdate {
	status := acp.ToolCallStatusCompleted
	if failed {
		status = acp.ToolCallStatusFailed
	}
	return acp.UpdateToolCall(acp.ToolCallId(id),
		acp.WithUpdateStatus(status),
		acp.WithUpdateContent(toolCallContent(name, args, result, failed)),
	)
}

// toolCallContent renders the result: a text block always, plus a diff card
// for successful write/edit built from the call args (edit's old_text is the
// exact replaced span; write to an existing file can't know the pre-image,
// so oldText is left nil — the client renders it as a full-file diff).
func toolCallContent(name, args, result string, failed bool) []acp.ToolCallContent {
	out := []acp.ToolCallContent{acp.ToolContent(acp.TextBlock(result))}
	if failed {
		return out
	}
	switch name {
	case "write":
		var a struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if json.Unmarshal([]byte(args), &a) == nil && a.Path != "" {
			out = append(out, acp.ToolDiffContent(a.Path, a.Content))
		}
	case "edit":
		var a struct {
			Path      string `json:"path"`
			OldString string `json:"old_string"`
			NewString string `json:"new_string"`
		}
		if json.Unmarshal([]byte(args), &a) == nil && a.Path != "" {
			out = append(out, acp.ToolDiffContent(a.Path, a.NewString, a.OldString))
		}
	}
	return out
}
