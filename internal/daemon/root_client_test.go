package daemon

import (
	"context"
	"database/sql"
	"net"
	"path/filepath"
	"testing"
	"time"

	daemonclient "github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

func TestRootClientReceivesEventsAfterExpiredCursorSnapshot(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "sessions.db")
	store := openStore(t, databasePath)
	t.Cleanup(func() { _ = store.Close() })
	rootID := createRoot(t, store)
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().UTC().Format(time.RFC3339)
	if _, err := database.ExecContext(t.Context(), `INSERT INTO events(root_id,seq,kind,created_at) VALUES(?,?,?,?),(?,?,?,?)`,
		rootID, 2, "fixture", stamp, rootID, session.EventRetention+1, "fixture", stamp); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	processes := newTestProcesses(t)
	value, err := New(store, processes, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(value, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Close()
		<-served
	})

	client, err := daemonclient.NewRootClient(daemonclient.RootClientOptions{
		ClientID: "expired-cursor", RootID: rootID, RetryMin: time.Millisecond, RetryMax: time.Millisecond,
		Connector: func(ctx context.Context, cursors map[string]int64) (daemonclient.RootConnection, error) {
			connection, err := (&net.Dialer{}).DialContext(ctx, "tcp", listener.Addr().String())
			if err != nil {
				return nil, err
			}
			return daemonclient.NewClient(ctx, connection, InitializeParams{
				ProtocolMajor: ProtocolMajor, ClientID: "expired-cursor", ClientKind: "test", Cursors: cursors,
			})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client.Start()
	t.Cleanup(func() { _ = client.Close() })
	if err := client.WaitLive(t.Context()); err != nil {
		t.Fatal(err)
	}
	sequence, err := store.AppendRootEvent(t.Context(), rootID, "fresh", session.RuntimePayload{Data: []byte(`{"ok":true}`)})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.After(2 * time.Second)
	for {
		select {
		case update := <-client.Updates():
			if update.Event != nil && update.Event.Seq == sequence && update.Event.Kind == "fresh" {
				return
			}
		case <-deadline:
			t.Fatal("live event pump stopped after the expired-cursor snapshot")
		}
	}
}
