package client_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func TestSessionEvidenceReadsPinExactOwnerAndKeepUnknowns(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(map[bool]string{false: "scoped", true: "wrong_owner"}[mismatch], func(t *testing.T) {
			owner := protocol.ID("owner")
			if mismatch {
				owner = "other"
			}
			c, _ := peer(t, func(_ context.Context, r protocol.Request) *protocol.Response {
				var params protocol.TurnUsageParams
				if err := json.Unmarshal(r.Params, &params); err != nil || params.SessionID != "owner" {
					t.Errorf("scope: %+v %v", params, err)
				}
				switch r.Method {
				case "usage.get":
					return success(t, r, protocol.Usage{SessionID: owner})
				case "usage.turn":
					if params.TurnID != "turn" {
						t.Error("turn scope changed", params.TurnID)
					}
					return success(t, r, protocol.TurnUsage{TurnID: "turn", Usage: protocol.Usage{SessionID: owner}})
				case "context.usage":
					return success(t, r, protocol.ContextUsage{SessionID: owner, ConfigRevision: 1, HistoryRevision: 1, ContextRevision: 1, ThroughSequence: 4, Basis: "latest_prefill", Prefill: &protocol.ContextPrefill{AttemptID: "attempt", TurnID: "turn", Model: protocol.ModelSelection{Provider: "fixture", Name: "model"}, ThroughSequence: 3, InputTokens: 0, InputSource: "reported", Stale: true}})
				case "cells.output":
					return success(t, r, protocol.CellOutput{Epoch: "boot", Preview: &protocol.CellOutputPreview{SessionID: owner, TurnID: "turn", CellID: "cell", CallMessageID: "message", CallID: "call", HistoryRevision: 1, Revision: 2, Text: "provisional", Truncated: true}})
				default:
					t.Error("unexpected method", r.Method)
					return nil
				}
			})
			s, err := c.Session("owner")
			if err != nil {
				t.Fatal(err)
			}
			usage, err := s.Usage(t.Context())
			if (err != nil) != mismatch || !mismatch && usage.SessionID != "owner" {
				t.Fatal(usage, err)
			}
			turn, err := s.TurnUsage(t.Context(), "turn")
			if (err != nil) != mismatch || !mismatch && turn.TurnID != "turn" {
				t.Fatal(turn, err)
			}
			ctx, err := s.ContextUsage(t.Context())
			if (err != nil) != mismatch {
				t.Fatal(ctx, err)
			}
			if !mismatch && (ctx.Prefill == nil || ctx.Prefill.InputTokens != 0 || ctx.Prefill.InputSource != "reported" || ctx.Prefill.ContextWindowTokens != nil || !ctx.Prefill.Stale || ctx.ThroughSequence == ctx.Prefill.ThroughSequence) {
				t.Fatal("context evidence changed", ctx)
			}
			output, err := s.CellOutput(t.Context())
			if (err != nil) != mismatch || !mismatch && (!output.Preview.Truncated || output.Epoch != "boot") {
				t.Fatal(output, err)
			}
		})
	}
}

func TestSessionEvidenceRejectsWrongTurnAndOversizedUTF8Preview(t *testing.T) {
	c, _ := peer(t, func(_ context.Context, r protocol.Request) *protocol.Response {
		switch r.Method {
		case "usage.turn":
			return success(t, r, protocol.TurnUsage{TurnID: "other_turn", Usage: protocol.Usage{SessionID: "owner"}})
		case "cells.output":
			return success(t, r, protocol.CellOutput{Epoch: "boot", Preview: &protocol.CellOutputPreview{SessionID: "owner", TurnID: "turn", CellID: "cell", CallMessageID: "message", CallID: "call", HistoryRevision: 1, Revision: 2, Text: strings.Repeat("界", 22000)}})
		default:
			t.Error(r.Method)
			return nil
		}
	})
	s, err := c.Session("owner")
	if err != nil {
		t.Fatal(err)
	}
	for _, read := range []func(context.Context) error{
		func(ctx context.Context) error { _, err := s.TurnUsage(ctx, "turn"); return err },
		func(ctx context.Context) error { _, err := s.CellOutput(ctx); return err },
	} {
		if err := read(t.Context()); err == nil {
			t.Fatal("invalid evidence accepted")
		}
	}
}
