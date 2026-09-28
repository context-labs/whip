package session

import (
	"errors"
	"regexp"
	"testing"
)

func TestCompactAgentIDs(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	forkID, err := store.Fork(rootID, 0, "fork")
	if err != nil {
		t.Fatal(err)
	}
	childID := NewAgentID()
	admitTestChild(t, store, rootID, rootAgentID, childID)
	for _, id := range []string{rootID, forkID, childID} {
		if !regexp.MustCompile(`^[a-z2-7]{20}$`).MatchString(id) {
			t.Fatalf("unexpected ID %q", id)
		}
		if data, err := agentIDEncoding.DecodeString(id); err != nil || len(data) != 12 {
			t.Fatalf("ID does not encode 96 bits: %q %v", id, err)
		}
	}
	if rootID != rootAgentID {
		t.Fatal("root/session identity differs")
	}
	for _, id := range []string{rootID, forkID, childID} {
		if _, err := store.AdmitAgent(t.Context(), AgentAdmission{RootID: rootID, ParentAgentID: rootAgentID, ChildAgentID: id, Name: "collision"}); err == nil {
			t.Fatalf("accepted existing identity %q", id)
		}
	}
	tx, err := store.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	attempt := 0
	id, err := unusedAgentID(t.Context(), tx, func() string {
		attempt++
		if attempt == 1 {
			return forkID
		}
		if attempt == 2 {
			return childID
		}
		return "unused-legacy-compatible"
	})
	if err != nil || id != "unused-legacy-compatible" || attempt != 3 {
		t.Fatalf("collision retry: %s %d %v", id, attempt, err)
	}
	if _, err := unusedAgentID(t.Context(), tx, func() string { return rootID }); !errors.Is(err, ErrAgentIDCollision) {
		t.Fatalf("exhausted collisions: %v", err)
	}
}
