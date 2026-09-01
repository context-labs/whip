package acp

// Permission bridge tests: ask mode routes gated tools through
// session/request_permission; auto mode doesn't. Mode flips live via
// session/set_mode and echoes current_mode_update.

import (
	"context"
	"encoding/json"
	"testing"

	acp "github.com/coder/acp-go-sdk"

	"github.com/context-labs/whip/internal/tools"
)

// gatedTool records whether it actually ran, and refuses without a gate
// decision only when the gate denies (it goes through checkGate like the
// real bash/write/edit tools do).
func gatedProbe(ran *bool) tools.Tool {
	*ran = false
	return tools.Tool{
		Def: llmTool("probe"),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			if deny := checkGateForTest(ctx, "bash", "rm -rf x"); deny != "" {
				return "", errString(deny)
			}
			*ran = true
			return "ran", nil
		},
	}
}

func TestPermissionWithoutClientFailsClosed(t *testing.T) {
	decision, _ := (&Bridge{}).requestPermission(context.Background(), &acpSession{}, tools.GateRequest{Tool: "bash", Command: "true"})
	if decision != tools.GateReject {
		t.Fatalf("decision = %v, want reject", decision)
	}
}

func TestAskModePermissionAllowOnce(t *testing.T) {
	dir := t.TempDir()
	srv := scriptServer(t, []step{
		{toolName: "probe", toolArgs: `{}`},
		{text: "done"},
	})
	ran := false
	f := newFixture(t, &fakeClient{
		answer: func(p acp.RequestPermissionRequest) permAnswer {
			return permAnswer{optionID: optAllowOnce}
		},
	}, nil, factoryFor(srv, []tools.Tool{gatedProbe(&ran)}))
	f.initialize(t)
	id := f.newSession(t, dir)

	if _, err := f.conn.SetSessionMode(context.Background(), acp.SetSessionModeRequest{SessionId: id, ModeId: ModeAsk}); err != nil {
		t.Fatal(err)
	}
	// The mode change echoes back as current_mode_update.
	f.client.waitFor(t, func(n acp.SessionNotification) bool {
		return n.Update.CurrentModeUpdate != nil && n.Update.CurrentModeUpdate.CurrentModeId == ModeAsk
	}, "current_mode_update")

	resp, err := f.prompt(t, id, "do the thing")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StopReason != acp.StopReasonEndTurn {
		t.Errorf("stopReason = %v", resp.StopReason)
	}
	if !ran {
		t.Error("tool did not run despite allow-once")
	}
	if len(f.client.perms) != 1 {
		t.Fatalf("permission requests = %d, want 1", len(f.client.perms))
	}
	// Options must include allow_once + allow_always + reject.
	var kinds []acp.PermissionOptionKind
	for _, o := range f.client.perms[0].Options {
		kinds = append(kinds, o.Kind)
	}
	want := map[acp.PermissionOptionKind]bool{
		acp.PermissionOptionKindAllowOnce:   true,
		acp.PermissionOptionKindAllowAlways: true,
		acp.PermissionOptionKindRejectOnce:  true,
	}
	for _, k := range kinds {
		delete(want, k)
	}
	if len(want) != 0 {
		t.Errorf("missing option kinds: %v (got %v)", want, kinds)
	}
}

func TestAskModePermissionReject(t *testing.T) {
	srv := scriptServer(t, []step{
		{toolName: "probe", toolArgs: `{}`},
		{text: "ok, skipped"},
	})
	ran := false
	f := newFixture(t, &fakeClient{
		answer: func(acp.RequestPermissionRequest) permAnswer { return permAnswer{optionID: optReject} },
	}, nil, factoryFor(srv, []tools.Tool{gatedProbe(&ran)}))
	f.initialize(t)
	id := f.newSession(t, t.TempDir())
	if _, err := f.conn.SetSessionMode(context.Background(), acp.SetSessionModeRequest{SessionId: id, ModeId: ModeAsk}); err != nil {
		t.Fatal(err)
	}

	resp, err := f.prompt(t, id, "do it")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StopReason != acp.StopReasonEndTurn {
		t.Errorf("stopReason = %v — a rejected tool must not kill the turn", resp.StopReason)
	}
	if ran {
		t.Error("tool ran despite reject")
	}
	// The model saw the denial (turn completed with the follow-up text).
	f.client.waitFor(t, func(n acp.SessionNotification) bool {
		c := n.Update.AgentMessageChunk
		return c != nil && c.Content.Text != nil && c.Content.Text.Text == "ok, skipped"
	}, "post-rejection reply")
}

func TestAutoModeSkipsPermission(t *testing.T) {
	srv := scriptServer(t, []step{
		{toolName: "probe", toolArgs: `{}`},
		{text: "done"},
	})
	ran := false
	client := &fakeClient{
		answer: func(acp.RequestPermissionRequest) permAnswer {
			t.Error("permission requested in auto mode")
			return permAnswer{optionID: optReject}
		},
	}
	f := newFixture(t, client, nil, factoryFor(srv, []tools.Tool{gatedProbe(&ran)}))
	f.initialize(t)
	id := f.newSession(t, t.TempDir()) // default mode is auto

	if _, err := f.prompt(t, id, "go"); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Error("tool did not run in auto mode")
	}
	if len(client.perms) != 0 {
		t.Errorf("auto mode issued %d permission requests", len(client.perms))
	}
}

func TestSetSessionModeUnknownMode(t *testing.T) {
	srv := scriptServer(t, []step{{text: "ok"}})
	f := newFixture(t, nil, nil, factoryFor(srv, nil))
	f.initialize(t)
	id := f.newSession(t, t.TempDir())
	_, err := f.conn.SetSessionMode(context.Background(), acp.SetSessionModeRequest{SessionId: id, ModeId: "yolo"})
	if err == nil {
		t.Fatal("expected invalid params for unknown mode")
	}
}

func TestPermissionCancelledOutcomeIsReject(t *testing.T) {
	srv := scriptServer(t, []step{
		{toolName: "probe", toolArgs: `{}`},
		{text: "fine"},
	})
	ran := false
	f := newFixture(t, &fakeClient{
		answer: func(acp.RequestPermissionRequest) permAnswer { return permAnswer{optionID: ""} }, // cancelled
	}, nil, factoryFor(srv, []tools.Tool{gatedProbe(&ran)}))
	f.initialize(t)
	id := f.newSession(t, t.TempDir())
	if _, err := f.conn.SetSessionMode(context.Background(), acp.SetSessionModeRequest{SessionId: id, ModeId: ModeAsk}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.prompt(t, id, "go"); err != nil {
		t.Fatal(err)
	}
	if ran {
		t.Error("tool ran on cancelled permission outcome — must fail closed")
	}
}

// Regression for review finding #5: allow-always must actually stick within
// the session — a second gated call with the same rule must not re-prompt.
func TestAllowAlwaysCoversRepeatCalls(t *testing.T) {
	srv := scriptServer(t, []step{
		{toolName: "probe", toolArgs: `{}`},
		{toolName: "probe", toolArgs: `{}`},
		{text: "done"},
	})
	ran := false
	client := &fakeClient{
		answer: func(acp.RequestPermissionRequest) permAnswer { return permAnswer{optionID: optAllowAlways} },
	}
	f := newFixture(t, client, nil, factoryFor(srv, []tools.Tool{gatedProbe(&ran)}))
	f.initialize(t)
	id := f.newSession(t, t.TempDir())
	if _, err := f.conn.SetSessionMode(context.Background(), acp.SetSessionModeRequest{SessionId: id, ModeId: ModeAsk}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.prompt(t, id, "twice"); err != nil {
		t.Fatal(err)
	}
	if len(client.perms) != 1 {
		t.Errorf("permission requests = %d, want 1 (second call covered by always-rule)", len(client.perms))
	}
}
