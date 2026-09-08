package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

// The consent mode is runner state, not durable: the root snapshot reports it
// from the runner, and permission.mode publishes the change to subscribed
// clients as session.permission_mode.updated.
func TestPermissionModeSnapshotAndUpdateEvent(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: NewToolRunner(tools.NewServices())}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := root.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.PermissionMode != "automatic" {
		t.Fatalf("initial snapshot permission mode = %q, want automatic", snapshot.PermissionMode)
	}

	result := clientCommand(t, root, "client", "mode-prompt", "permission.mode", map[string]bool{"external_permissions": true})
	if result.Status != "succeeded" {
		t.Fatalf("permission.mode = %+v", result)
	}

	snapshot, err = root.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.PermissionMode != "prompt" {
		t.Fatalf("updated snapshot permission mode = %q, want prompt", snapshot.PermissionMode)
	}

	events, _, err := store.ReplayEvents(t.Context(), rootID, 0, session.MaxEventReplay)
	if err != nil {
		t.Fatal(err)
	}
	var update *protocol.SessionUpdateEvent
	for _, envelope := range events {
		if envelope.Kind != "session.permission_mode.updated" {
			continue
		}
		var event protocol.SessionUpdateEvent
		if err := json.Unmarshal(envelope.Payload.Inline, &event); err != nil {
			t.Fatal(err)
		}
		update = &event
	}
	if update == nil || update.PermissionMode == nil || *update.PermissionMode != "prompt" {
		t.Fatalf("permission mode event = %+v", update)
	}
}
