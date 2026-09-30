package daemon

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSubscriptionRetirementCannotDeleteReplacement(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "runtime.db"))
	rootID := createRoot(t, store)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	old := &subscription{id: "reused", rootID: rootID, cancel: func() {}}
	replacement := &subscription{id: "reused", rootID: rootID, cancel: func() {}}
	server := &Server{ctx: t.Context(), daemon: &Daemon{store: store}}
	connection := &serverConn{subscriptions: map[string]*subscription{"reused": replacement}, out: make(chan []byte, 1)}
	server.pumpSubscription(ctx, connection, old, 0)
	if connection.subscriptions["reused"] != replacement {
		t.Fatal("retiring stream removed replacement")
	}
	if len(connection.out) != 0 {
		t.Fatal("normal unsubscribe emitted a recovery failure")
	}
}
