package daemon

import (
	"context"
	"encoding/json"
	"testing"
)

type blockedSubmissionConnection struct {
	*staticRootConnection
	entered chan CommandParams
	release chan struct{}
}

func (c *blockedSubmissionConnection) Command(ctx context.Context, p CommandParams) (CommandResult, error) {
	c.entered <- p
	select {
	case <-ctx.Done():
		return CommandResult{Status: "queued"}, ctx.Err()
	case <-c.release:
		return CommandResult{Status: "succeeded"}, nil
	}
}

func TestRootCancellationTargetsSubmittedCommandBeforeTurnEvent(t *testing.T) {
	connection := &blockedSubmissionConnection{staticRootConnection: newStaticRootConnection(), entered: make(chan CommandParams, 2), release: make(chan struct{})}
	client := &RootClient{clientID: "client", instanceID: "test", rootID: "root", state: RootLive, conn: connection, activeTurns: map[string]string{"root": "stale-turn"}}
	action, err := client.NewAction("submit", map[string]string{"text": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	// Merely constructing an action must not redirect cancellation.
	before, _ := client.NewAction("cancel", map[string]string{})
	var target struct {
		TurnID    string `json:"turn_id"`
		CommandID string `json:"target_command_id"`
	}
	if err := json.Unmarshal(before.Payload, &target); err != nil {
		t.Fatal(err)
	}
	if target.TurnID != "stale-turn" || target.CommandID != "" {
		t.Fatalf("unsubmitted target: %+v", target)
	}
	done := make(chan struct{})
	go func() { defer close(done); _, _ = client.Command(t.Context(), action) }()
	<-connection.entered
	cancel, _ := client.NewAction("cancel", map[string]string{})
	target.TurnID, target.CommandID = "", ""
	if err := json.Unmarshal(cancel.Payload, &target); err != nil {
		t.Fatal(err)
	}
	if target.CommandID != action.CommandID || target.TurnID != "" {
		t.Fatalf("queued target: %+v", target)
	}
	close(connection.release)
	<-done
	client.mu.RLock()
	remaining := client.submittedCommands["root"]
	client.mu.RUnlock()
	if remaining != "" {
		t.Fatalf("terminal command retained: %s", remaining)
	}
	// The already-created cancellation retains its exact identity after completion.
	if string(cancel.Payload) == string(before.Payload) {
		t.Fatal("cancellation target changed")
	}
}
