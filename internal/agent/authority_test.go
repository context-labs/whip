package agent

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

func bindTestAgent(t *testing.T, ag *Agent, root string) {
	t.Helper()
	bindTestServices(t, ag.Services, root)
}

func bindTestServices(t *testing.T, services *tools.Services, root string) {
	t.Helper()
	store, err := session.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	rootID, err := store.Create(session.SessionKindAgent, root, "m", "p")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := store.EnsureAuthority(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	services.SetProcessMarkers(rootID, "m")
	if err := services.BindDispatcher(store, store.Workspaces(), store.Processes(), authority); err != nil {
		t.Fatal(err)
	}
}
