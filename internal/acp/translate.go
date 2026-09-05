// Package acp serves whip as an Agent Client Protocol agent: the bridge
// between the editor-facing JSON-RPC protocol (github.com/coder/acp-go-sdk)
// and whip's agent loop. translate.go holds the pure conversions — no I/O,
// no connection state — so the wire mapping is trivially testable.
package acp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	acp "github.com/coder/acp-go-sdk"

	"github.com/context-labs/whip/internal/llm"
)

// toolKind maps whip tool names to ACP tool kinds (protocol-notes.md §5).
func toolKind(name string) acp.ToolKind {
	switch name {
	case "read":
		return acp.ToolKindRead
	case "write", "edit":
		return acp.ToolKindEdit
	case "bash", "shell_start", "workspace_process":
		return acp.ToolKindExecute
	default:
		// browser_exec, computer_exec, mcp__* tools: "other" is honest.
		return acp.ToolKindOther
	}
}

// pathArg extracts the file path a tool call touches, for the location list.
func pathArg(name, args string) string {
	switch name {
	case "read", "write", "edit":
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
		verb := map[string]string{"read": "Read", "write": "Write", "edit": "Edit"}[name]
		return verb + " " + p
	}
	switch name {
	case "bash":
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

// todoStatusToACP maps whip's todo statuses onto ACP plan entry statuses
// (whip has an extra "cancelled" state; ACP doesn't — it maps to pending,
// the honest "not done" reading).
func todoStatusToACP(s string) acp.PlanEntryStatus {
	switch s {
	case "in_progress":
		return acp.PlanEntryStatusInProgress
	case "completed":
		return acp.PlanEntryStatusCompleted
	default:
		return acp.PlanEntryStatusPending
	}
}

// isErrorResult matches the loop's error-as-output convention (tools.Execute).
func isErrorResult(result string) bool {
	return strings.HasPrefix(result, "Error: ")
}

// endToolCall builds the terminal `tool_call_update` for a finished call.
// result is the exact string fed back to the model.
func endToolCall(id, name, args, result string) acp.SessionUpdate {
	status := acp.ToolCallStatusCompleted
	if isErrorResult(result) {
		status = acp.ToolCallStatusFailed
	}
	return acp.UpdateToolCall(acp.ToolCallId(id),
		acp.WithUpdateStatus(status),
		acp.WithUpdateContent(toolCallContent(name, args, result)),
	)
}

// toolCallContent renders the result: a text block always, plus a diff card
// for successful write/edit built from the call args (edit's old_text is the
// exact replaced span; write to an existing file can't know the pre-image,
// so oldText is left nil — the client renders it as a full-file diff).
func toolCallContent(name, args, result string) []acp.ToolCallContent {
	out := []acp.ToolCallContent{acp.ToolContent(acp.TextBlock(result))}
	if isErrorResult(result) {
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

// promptFromBlocks flattens ACP prompt content blocks into the user-message
// text plus multimodal parts for the agent loop. vision reports whether the
// resolved model accepts images; without it images degrade to a placeholder
// note (the client shouldn't send them — promptCapabilities gates that).
func promptFromBlocks(blocks []acp.ContentBlock, vision bool) (text string, parts []llm.ContentPart) {
	var sb strings.Builder
	sep := func() {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
	}
	for _, b := range blocks {
		switch {
		case b.Text != nil:
			sep()
			sb.WriteString(b.Text.Text)
		case b.ResourceLink != nil:
			// Mirror whip's @file convention: name the path, the agent reads
			// it with its own tools.
			sep()
			fmt.Fprintf(&sb, "@%s", b.ResourceLink.Uri)
		case b.Resource != nil:
			sep()
			r := b.Resource.Resource
			switch {
			case r.TextResourceContents != nil:
				fmt.Fprintf(&sb, "File: %s\n```\n%s\n```", r.TextResourceContents.Uri, r.TextResourceContents.Text)
			case r.BlobResourceContents != nil:
				fmt.Fprintf(&sb, "[binary resource: %s]", r.BlobResourceContents.Uri)
			}
		case b.Image != nil:
			if vision {
				if data, err := base64.StdEncoding.DecodeString(b.Image.Data); err == nil {
					ext, data := llm.NormalizeImage(mimeExt(b.Image.MimeType), data) // same caps as paste/@mention
					parts = append(parts, llm.ImagePart(ext, data))
					continue
				}
			}
			sep()
			fmt.Fprintf(&sb, "[image: %s]", b.Image.MimeType)
		case b.Audio != nil:
			sep()
			fmt.Fprintf(&sb, "[audio: %s — not supported]", b.Audio.MimeType)
		}
	}
	return sb.String(), parts
}

// mimeExt maps an image MIME type to the extension llm.ImagePart wants for
// its data URL (it re-derives the mime from the extension).
func mimeExt(mime string) string {
	return strings.TrimPrefix(mime, "image/")
}

// replayUpdates converts stored conversation messages into the session/update
// stream session/load must send before responding: user/agent message chunks
// plus tool-call cards in their terminal state (protocol-notes.md §3.4).
func replayUpdates(msgs []llm.Message) []acp.SessionUpdate {
	var out []acp.SessionUpdate
	// tool call args by id, so the paired result can build its card content.
	argsByID := map[string]struct{ name, args string }{}
	for _, m := range msgs {
		switch m.Role {
		case "user":
			if t := strings.TrimSpace(m.TextContent()); t != "" {
				out = append(out, acp.UpdateUserMessageText(t))
			}
		case "assistant":
			if t := m.Content; t != "" {
				out = append(out, acp.UpdateAgentMessageText(t))
			}
			for _, tc := range m.ToolCalls {
				argsByID[tc.ID] = struct{ name, args string }{tc.Function.Name, tc.Function.Arguments}
				out = append(out, acp.StartToolCall(acp.ToolCallId(tc.ID),
					toolTitle(tc.Function.Name, tc.Function.Arguments),
					acp.WithStartKind(toolKind(tc.Function.Name)),
					acp.WithStartStatus(acp.ToolCallStatusInProgress),
				))
			}
		case "tool":
			info, ok := argsByID[m.ToolCallID]
			if !ok {
				continue // orphaned result (e.g. post-compaction) — no card to close
			}
			out = append(out, endToolCall(m.ToolCallID, info.name, info.args, m.Content))
		}
	}
	return out
}
