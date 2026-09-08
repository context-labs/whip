package daemon

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

func TestModelAdmissionWaitingForActorDoesNotHoldAccountingLock(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	actorDone := make(chan error, 1)
	go func() {
		actorDone <- root.routeControl(t.Context(), func(context.Context) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	admitted := make(chan error, 1)
	go func() {
		permit, err := root.BeginModelAttempt(t.Context(), llm.ModelAttempt{
			LogicalID: "queued", Number: 1, Model: "fixture", MaxTokens: 1, Timeout: time.Second,
		})
		if err == nil {
			err = permit.Settle(llm.ModelAttemptResult{})
		}
		admitted <- err
	}()
	deadline := time.Now().Add(time.Second)
	for {
		root.supervisor.mu.Lock()
		queued := len(root.supervisor.events) > 0
		root.supervisor.mu.Unlock()
		if queued {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("admission did not reach the occupied actor")
		}
		time.Sleep(time.Millisecond)
	}
	// A child being stopped can still need this lock to finish its host call.
	if !root.accountingMu.TryLock() {
		t.Fatal("queued model admission prevents an active host call from finishing")
	}
	root.accountingMu.Unlock()
	unblock()
	if err := <-actorDone; err != nil {
		t.Fatal(err)
	}
	if err := <-admitted; err != nil {
		t.Fatal(err)
	}
}
