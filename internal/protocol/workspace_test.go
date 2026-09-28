package protocol

import (
	"encoding/json"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func TestWorkspaceProjectionRejectsPrivateIdentityAndUnknownOutcomes(t *testing.T) {
	value := WorkspaceSnapshotFromDomain(session.WorkspaceSnapshot{
		ID: "snapshot", SessionID: "owner", CaptureID: "capture", State: session.WorkspaceSucceeded,
		CreatedAt: time.Now(), Semantics: session.WorkspaceSemantics,
		Binding: session.WorkspaceBinding{Worktree: "/private/worktree", GitDirectory: "/private/git", CommonDirectory: "/private/common", Scope: "private-scope", WorktreeIdentity: "1:2"}, ObjectID: new(strings.Repeat("a", 40)),
	})
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate("WorkspaceSnapshot", raw); err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"/private", "private-scope", "1:2", strings.Repeat("a", 40), "binding", "object_id"} {
		if strings.Contains(string(raw), private) {
			t.Fatalf("public snapshot leaked %s: %s", private, raw)
		}
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []struct{ key, value string }{{"object_id", "private"}, {"scope", "repository"}, {"state", "failed"}} {
		test := maps.Clone(fields)
		test[mutation.key] = mutation.value
		changed, err := json.Marshal(test)
		if err != nil {
			t.Fatal(err)
		}
		if err := Validate("WorkspaceSnapshot", changed); err == nil {
			t.Fatal("accepted invalid workspace projection", string(changed))
		}
	}
}
