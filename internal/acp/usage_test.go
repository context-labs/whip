package acp

import (
	"context"
	"encoding/json"
	"testing"

	acpsdk "github.com/coder/acp-go-sdk"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

func knownUsage() protocol.Usage {
	return protocol.Usage{
		SessionID: "owner", Attempts: protocol.UsageAttempts{Settled: 2},
		InputTokens: protocol.UsageQuantity{Value: 10, KnownAttempts: 2}, OutputTokens: protocol.UsageQuantity{Value: 4, KnownAttempts: 2},
		ReasoningTokens: protocol.UsageQuantity{Value: 2, KnownAttempts: 2}, CachedInput: protocol.UsageQuantity{Value: 3, KnownAttempts: 2},
		CachedOutput: protocol.UsageQuantity{Value: 0, KnownAttempts: 1, MissingAttempts: 1},
	}
}

func TestACPPerTurnTokensRequireExactCompleteEvidence(t *testing.T) {
	value := turnUsage(knownUsage())
	if value == nil || value.InputTokens != 10 || value.OutputTokens != 4 || value.TotalTokens != 14 || value.ThoughtTokens == nil || *value.ThoughtTokens != 2 || value.CachedReadTokens == nil || *value.CachedReadTokens != 3 || value.CachedWriteTokens != nil {
		t.Fatal(value)
	}
	for name, edit := range map[string]func(*protocol.Usage){
		"missing":        func(v *protocol.Usage) { v.InputTokens.MissingAttempts = 1 },
		"partial":        func(v *protocol.Usage) { v.OutputTokens.KnownAttempts = 1 },
		"overflow":       func(v *protocol.Usage) { v.OutputTokens.Overflow = true },
		"uncertain":      func(v *protocol.Usage) { v.Attempts.Uncertain = 1 },
		"in_flight":      func(v *protocol.Usage) { v.Attempts.InFlight = 1 },
		"reserved":       func(v *protocol.Usage) { v.Attempts.Reserved = 1 },
		"unmeasured":     func(v *protocol.Usage) { v.Attempts.Settled = 0 },
		"inexact_number": func(v *protocol.Usage) { v.InputTokens.Value = protocol.Counter(maxACPTokens) + 1 },
		"inexact_sum":    func(v *protocol.Usage) { v.InputTokens.Value = protocol.Counter(maxACPTokens) },
	} {
		t.Run(name, func(t *testing.T) {
			v := knownUsage()
			edit(&v)
			if got := turnUsage(v); got != nil {
				t.Fatal(got)
			}
		})
	}
	zero := knownUsage()
	zero.InputTokens.Value = 0
	zero.OutputTokens.Value = 0
	zero.ReasoningTokens.Value = 0
	zero.CachedInput.Value = 0
	if got := turnUsage(zero); got == nil || got.TotalTokens != 0 {
		t.Fatal("reported zero was lost", got)
	}
}

func TestACPNativePromptAccountingSurvivesReloadWithoutCumulativeRelabeling(t *testing.T) {
	f := nativeFixture(t, func(_ context.Context, _ model.Request, _ func(model.Chunk)) (model.Response, error) {
		value := textResponse("accounted")
		value.Usage = session.ModelUsage{Input: new(int64(7)), Output: new(int64(3)), CachedInput: new(int64(2))}
		return value, nil
	}, nil)
	id := f.newSession(t)
	s := f.bridge.getSession(id)
	owner, err := s.handle.Get(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.native.Call(t.Context(), "sessions.configure", protocol.UpdateConfigurationParams{SessionID: owner.ID, ExpectedRevision: owner.ConfigRevision, Patch: protocol.ConfigPatch{AutomaticTitle: new(false)}}, &owner); err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 3; n++ {
		if n == 3 {
			if _, err := f.conn.CloseSession(t.Context(), acpsdk.CloseSessionRequest{SessionId: id}); err != nil {
				t.Fatal(err)
			}
			if _, err := f.conn.LoadSession(t.Context(), acpsdk.LoadSessionRequest{SessionId: id, Cwd: f.cwd, McpServers: []acpsdk.McpServer{}}); err != nil {
				t.Fatal(err)
			}
		}
		value := f.prompt(t, id, "measure this turn")
		if value.Usage == nil || value.Usage.InputTokens != 7 || value.Usage.OutputTokens != 3 || value.Usage.TotalTokens != 10 || value.Usage.ThoughtTokens != nil {
			t.Fatalf("turn %d: %+v", n, value)
		}
		var cumulative protocol.Usage
		decodeMetadata(t, value.Meta["whip_cumulative_usage"], &cumulative)
		if cumulative.InputTokens.Value != protocol.Counter(n*7) {
			t.Fatal(cumulative)
		}
		var turn protocol.TurnUsage
		decodeMetadata(t, value.Meta["whip_turn_usage"], &turn)
		if turn.Usage.InputTokens.Value != 7 || turn.TurnID == "" {
			t.Fatal(turn)
		}
		var evidence protocol.ContextUsage
		decodeMetadata(t, value.Meta["whip_context_usage"], &evidence)
		if evidence.Basis != "latest_prefill" || evidence.Prefill == nil || evidence.Prefill.TurnID != turn.TurnID || evidence.Prefill.InputTokens != 7 || evidence.Prefill.InputSource != "reported" || !evidence.Prefill.Stale || evidence.Prefill.ContextWindowTokens != nil {
			t.Fatalf("context=%+v prefill=%+v", evidence, evidence.Prefill)
		}
	}
	f.editor.mu.Lock()
	defer f.editor.mu.Unlock()
	for _, event := range f.editor.updates {
		if event.Update.UsageUpdate != nil {
			t.Fatal("latest prefill mislabeled as current occupancy")
		}
	}
}

func decodeMetadata(t *testing.T, value, result any) {
	t.Helper()
	if value == nil {
		t.Fatal("accounting evidence missing")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, result); err != nil {
		t.Fatal(err)
	}
}
