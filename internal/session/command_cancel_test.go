package session

import (
	"errors"
	"testing"
)

func TestQueuedCancellationCommitsInboxOutcomeAndEventTogether(t *testing.T) {
	store, rootID, agentID := newSwarmFixture(t)
	queued := inputTestCommand(t, store, rootID, agentID, "queued", "submit")
	outcome := []byte(`{"code":-32000,"message":"cancelled"}`)
	exec(t, store, `CREATE TRIGGER reject_cancel BEFORE INSERT ON events WHEN NEW.kind='command.cancelled' BEGIN SELECT RAISE(ABORT,'event failure'); END`)
	if _, _, err := store.CancelQueuedInput(t.Context(), rootID, rootID, "queued", outcome); err == nil {
		t.Fatal("ignored event failure")
	}
	inputTestStatus(t, store, rootID, agentID, queued.Command.IngressSeq, "queued")
	inputCommandStatus(t, store, rootID, "queued", "queued")
	exec(t, store, `DROP TRIGGER reject_cancel`)
	record, cancelled, err := store.CancelQueuedInput(t.Context(), rootID, rootID, "queued", outcome)
	if err != nil || !cancelled || record.Status != "cancelled" {
		t.Fatalf("record=%+v cancelled=%t error=%v", record, cancelled, err)
	}
	inputTestStatus(t, store, rootID, agentID, queued.Command.IngressSeq, "cancelled")
	if _, _, err := store.CancelQueuedInput(t.Context(), rootID, rootID, "queued", outcome); !errors.Is(err, ErrCommandTarget) {
		t.Fatalf("terminal target=%v", err)
	}
	if err := store.StartRootTurn(t.Context(), rootID, agentID, queued.Command.IngressSeq); err == nil {
		t.Fatal("cancelled command admitted to model")
	}
}
