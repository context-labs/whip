package acp

import (
	"testing"

	acpsdk "github.com/coder/acp-go-sdk"
)

func TestBridgeFailedSessionSetupClosesClientAndAllowsRetry(t *testing.T) {
	f := nativeFixture(t, nil, nil)
	id := f.newSession(t)
	if _, err := f.conn.CloseSession(t.Context(), acpsdk.CloseSessionRequest{SessionId: id}); err != nil {
		t.Fatal(err)
	}
	for _, request := range []acpsdk.LoadSessionRequest{{SessionId: "missing", Cwd: f.cwd, McpServers: []acpsdk.McpServer{}}, {SessionId: id, Cwd: "/wrong", McpServers: []acpsdk.McpServer{}}} {
		if _, err := f.conn.LoadSession(t.Context(), request); err == nil {
			t.Fatal("invalid attachment succeeded")
		}
		if f.bridge.getSession(request.SessionId) != nil {
			t.Fatal("failed attachment retained observer")
		}
	}
	if _, err := f.conn.LoadSession(t.Context(), acpsdk.LoadSessionRequest{SessionId: id, Cwd: f.cwd, McpServers: []acpsdk.McpServer{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.conn.LoadSession(t.Context(), acpsdk.LoadSessionRequest{SessionId: id, Cwd: f.cwd, McpServers: []acpsdk.McpServer{}}); err == nil {
		t.Fatal("duplicate attachment replaced observer")
	}
	if _, err := f.conn.SetSessionMode(t.Context(), acpsdk.SetSessionModeRequest{SessionId: id, ModeId: ModeAuto}); err != nil {
		t.Fatalf("failed attachment closed borrowed client: %v", err)
	}
}
