package acp

// permission.go adapts a session's tools.Gate consent seam to ACP's
// session/request_permission. "Allow always" rules stay in that ACP session;
// the TUI persists its corresponding rules to disk.

import (
	"context"
	"fmt"

	acp "github.com/coder/acp-go-sdk"

	"github.com/context-labs/whip/internal/tools"
)

const (
	optAllowOnce   = "allow-once"
	optAllowAlways = "allow-always"
	optReject      = "reject"
)

func (b *Bridge) bindPermissionGate(s *acpSession) {
	s.ag.Services.SetGate(func(ctx context.Context, req tools.GateRequest) (tools.GateDecision, string) {
		s.turnMu.Lock()
		mode := s.mode
		s.turnMu.Unlock()
		if mode != ModeAsk {
			return tools.GateAllowOnce, ""
		}
		return b.requestPermission(ctx, s, req)
	})
}

// requestPermission round-trips one GateRequest through the client. The ctx
// is the turn's, so session/cancel unblocks a pending prompt (the client
// answers "cancelled" per spec; a dead client fails closed via ctx).
// A cancelled or errored prompt is a reject — fail-closed.
func (b *Bridge) requestPermission(ctx context.Context, s *acpSession, req tools.GateRequest) (tools.GateDecision, string) {
	if b.conn == nil {
		return tools.GateReject, "permission client is unavailable"
	}

	// "Always allow" rules cover repeat calls without re-prompting.
	rule := req.Rule
	if req.Tool != "bash" {
		rule = req.Command // path rules are exact (matches the TUI)
	}
	key := req.Tool + ":" + rule
	s.turnMu.Lock()
	covered := s.allowed[key]
	s.turnMu.Unlock()
	if covered {
		return tools.GateAllowOnce, ""
	}

	name := req.Tool
	if req.Command != "" {
		name = fmt.Sprintf("%s %q", req.Tool, req.Command)
	}
	options := []acp.PermissionOption{
		{OptionId: optAllowOnce, Name: "Allow once", Kind: acp.PermissionOptionKindAllowOnce},
		{OptionId: optReject, Name: "Reject", Kind: acp.PermissionOptionKindRejectOnce},
	}
	if req.Rule != "" {
		options = append(options, acp.PermissionOption{
			OptionId: optAllowAlways,
			Name:     fmt.Sprintf("Always allow %q", req.Rule),
			Kind:     acp.PermissionOptionKindAllowAlways,
		})
	}

	resp, err := b.conn.RequestPermission(ctx, acp.RequestPermissionRequest{
		SessionId: s.id,
		ToolCall: acp.ToolCallUpdate{
			ToolCallId: acp.ToolCallId("perm-" + req.Tool),
			Title:      new(name),
			Kind:       new(toolKind(req.Tool)),
		},
		Options: options,
	})
	if err != nil {
		if ctx.Err() != nil {
			return tools.GateReject, "the user cancelled the permission prompt"
		}
		return tools.GateReject, "permission request failed: " + err.Error()
	}
	switch {
	case resp.Outcome.Selected != nil:
		switch string(resp.Outcome.Selected.OptionId) {
		case optAllowOnce:
			return tools.GateAllowOnce, ""
		case optAllowAlways:
			s.turnMu.Lock()
			s.allowed[key] = true
			s.turnMu.Unlock()
			return tools.GateAllowAlways, ""
		default:
			return tools.GateReject, "the user rejected this action"
		}
	default: // cancelled
		return tools.GateReject, "the user cancelled the permission prompt"
	}
}
