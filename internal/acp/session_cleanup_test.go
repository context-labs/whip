package acp

import (
	"context"
	"testing"

	acpsdk "github.com/coder/acp-go-sdk"

	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/mcp"
)

type seededACPBackend struct {
	*fakeACPBackend
	id string
}

func (b *seededACPBackend) NewRoot(ctx context.Context, _ string, servers map[string]mcp.ServerConfig) (*daemon.RootClient, error) {
	return b.fakeACPBackend.LoadRoot(ctx, b.id, "", servers)
}

func (b *seededACPBackend) LoadRoot(ctx context.Context, _, _ string, servers map[string]mcp.ServerConfig) (*daemon.RootClient, error) {
	return b.NewRoot(ctx, "", servers)
}

func TestBridgeFailedSessionSetupClosesClientAndAllowsRetry(t *testing.T) {
	for _, failure := range []string{"new snapshot", "load snapshot", "wrong identity", "wrong directory"} {
		t.Run(failure, func(t *testing.T) {
			base := newFakeBackend(t)
			cwd := t.TempDir()
			id := base.seed(cwd)
			backend := &seededACPBackend{fakeACPBackend: base, id: id}
			root := base.roots[id]
			bridge := NewBridge("test", backend, false, nil)
			t.Cleanup(bridge.CloseAll)
			request := acpsdk.LoadSessionRequest{SessionId: acpsdk.SessionId(id), Cwd: cwd}
			switch failure {
			case "new snapshot", "load snapshot":
				root.snapshotError = "session snapshot unavailable"
			case "wrong identity":
				request.SessionId = "different-root"
			case "wrong directory":
				request.Cwd = t.TempDir()
			}
			var err error
			if failure == "new snapshot" {
				_, err = bridge.NewSession(t.Context(), acpsdk.NewSessionRequest{Cwd: cwd})
			} else {
				_, err = bridge.LoadSession(t.Context(), request)
			}
			if err == nil {
				t.Fatal("invalid session setup succeeded")
			}
			if bridge.getSession(acpsdk.SessionId(id)) != nil {
				t.Fatal("failed setup retained an active session")
			}
			root.mu.Lock()
			closed := root.closeCount
			root.snapshotError = ""
			root.mu.Unlock()
			if closed != 1 {
				t.Fatalf("failed setup closed %d connections, want 1", closed)
			}
			if _, err := bridge.LoadSession(t.Context(), acpsdk.LoadSessionRequest{SessionId: acpsdk.SessionId(id), Cwd: cwd}); err != nil {
				t.Fatalf("corrected setup could not retry: %v", err)
			}
			if _, err := bridge.NewSession(t.Context(), acpsdk.NewSessionRequest{Cwd: cwd}); err == nil {
				t.Fatal("duplicate root replaced the active session")
			}
			if _, err := bridge.SetSessionMode(t.Context(), acpsdk.SetSessionModeRequest{SessionId: acpsdk.SessionId(id), ModeId: ModeAuto}); err != nil {
				t.Fatalf("duplicate attempt closed the original client: %v", err)
			}
		})
	}
}
