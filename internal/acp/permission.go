package acp

import (
	"context"
	"encoding/json"
	"strings"

	acp "github.com/coder/acp-go-sdk"
)

const (
	optAllowOnce   = "allow-once"
	optAllowAlways = "allow-always"
	optReject      = "reject"
)

type pendingPermission struct {
	PermissionID  string `json:"permission_id"`
	OperationID   string `json:"operation_id"`
	Operation     string `json:"operation"`
	CanonicalPath string `json:"canonical_path"`
	Command       string `json:"command"`
	Rule          string `json:"rule"`
}

func (b *Bridge) handlePermission(s *acpSession, payload []byte) {
	var pending pendingPermission
	if json.Unmarshal(payload, &pending) != nil || pending.PermissionID == "" {
		return
	}
	s.mu.Lock()
	mode := s.mode
	s.mu.Unlock()
	if mode != ModeAsk {
		return
	}
	if b.conn == nil {
		b.decidePermission(s, pending, false, "permission client is unavailable", "")
		return
	}
	name := pending.Operation
	if pending.Command != "" {
		name += " " + pending.Command
	}
	options := []acp.PermissionOption{
		{OptionId: optAllowOnce, Name: "Allow once", Kind: acp.PermissionOptionKindAllowOnce},
		{OptionId: optReject, Name: "Reject", Kind: acp.PermissionOptionKindRejectOnce},
	}
	if pending.Rule != "" {
		label := "Always allow " + pending.Operation + " " + pending.Rule + " in this tree"
		if pending.Operation == "mcp.call" {
			label = "Always allow this MCP tool and server definition in this tree"
		}
		options = append(options, acp.PermissionOption{
			OptionId: optAllowAlways, Name: label, Kind: acp.PermissionOptionKindAllowAlways,
		})
	}
	toolCall := acp.ToolCallUpdate{
		ToolCallId: acp.ToolCallId("perm-" + pending.PermissionID),
		Title:      new(name), Kind: new(toolKind(pending.Operation)),
	}
	if pending.Operation == "mcp.call" {
		title, _, _ := strings.Cut(name, "\n")
		toolCall.Title = &title
		toolCall.Content = []acp.ToolCallContent{acp.ToolContent(acp.TextBlock(pending.Command))}
	}
	response, err := b.conn.RequestPermission(s.lifecycle, acp.RequestPermissionRequest{
		SessionId: s.id,
		ToolCall:  toolCall,
		Options:   options,
	})
	if err != nil || response.Outcome.Selected == nil {
		reason := "the user cancelled the permission prompt"
		if err != nil && s.lifecycle.Err() == nil {
			reason = "permission request failed: " + err.Error()
		}
		b.decidePermission(s, pending, false, reason, "")
		return
	}
	switch string(response.Outcome.Selected.OptionId) {
	case optAllowOnce:
		b.decidePermission(s, pending, true, "approved by ACP client", "")
	case optAllowAlways:
		if pending.Rule == "" {
			b.decidePermission(s, pending, false, "allow-always requires a rule", "")
			return
		}
		b.decidePermission(s, pending, true, "approved by ACP client for this tree", "tree")
	default:
		b.decidePermission(s, pending, false, "the user rejected this action", "")
	}
}

func (b *Bridge) decidePermission(s *acpSession, pending pendingPermission, allow bool, reason, remember string) {
	ctx, cancel := context.WithCancel(s.lifecycle)
	defer cancel()
	action, err := s.root.NewAction("permission.decide", struct{}{})
	if err != nil {
		return
	}
	_, _ = s.root.DecidePermission(ctx, action, pending.PermissionID, allow, reason, remember)
}
