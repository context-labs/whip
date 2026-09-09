package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

func TestSessionMetadataAcrossTransports(t *testing.T) {
	f := newV2Fixture(t, &fakeRunner{})
	title := strings.Repeat("untruncated title 界 ", 24)
	cwd := "/" + strings.Repeat("long-path/", 80)
	if err := f.store.SetTitle(f.rootID, title); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetWorkingDirectory(f.rootID, cwd); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetArchived(t.Context(), f.rootID, true); err != nil {
		t.Fatal(err)
	}
	for _, transport := range []string{"unix", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			client := f.dial(transport, "metadata-"+transport)
			var result session.SessionMetadata
			if err := client.Call(t.Context(), "sessions.get", protocol.RootParams{RootID: f.rootID}, &result); err != nil {
				t.Fatal(err)
			}
			if result.RootID != f.rootID || result.Title != title || result.CWD != cwd || !result.Archived {
				t.Fatalf("metadata %+v", result)
			}
			for _, rootID := range []string{"", "missing", strings.Repeat("界", 128)} {
				if err := client.Call(t.Context(), "sessions.get", protocol.RootParams{RootID: rootID}, &result); err == nil {
					t.Fatalf("invalid metadata root accepted: %q", rootID)
				}
			}
			var page session.SessionCatalogPage
			if err := client.Call(t.Context(), "sessions.list", protocol.SessionCatalogParams{Status: "archived", Limit: 10, MaxBytes: 4096}, &page); err != nil || len(page.Items) != 1 || !page.Items[0].Archived {
				t.Fatalf("archived catalog %+v %v", page, err)
			}
		})
	}
}

func TestSessionMetadataDoesNotOpenRuntime(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	opens := 0
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		opens++
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	server := &Server{daemon: owner}
	params, err := json.Marshal(protocol.RootParams{RootID: rootID})
	if err != nil {
		t.Fatal(err)
	}
	result, failure := server.handle(&serverConn{ctx: t.Context()}, rpcMessage{Method: "sessions.get", Params: params})
	if failure != nil || result.(session.SessionMetadata).RootID != rootID || opens != 0 {
		t.Fatalf("metadata opened runtime: result=%+v failure=%v opens=%d", result, failure, opens)
	}
}
