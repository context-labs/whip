package protocol

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func TestReloadContractPreservesExactCapturedEvidenceAndRejectsFalseOutcomes(t *testing.T) {
	config, err := session.Resolve(session.Configuration{}, session.Builtins()[0], session.ConfigPatch{})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	edit, err := ReloadEditFromDomain(session.ReloadEdit{ID: "Reload.Mixed", SessionID: "root", ExpectedRevision: 9007199254740993, TreeID: "tree", HostRevision: strings.Repeat("a", 64), Configuration: config, State: session.ReloadPending, CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	valid := func(value ReloadEdit) bool {
		raw, err := json.Marshal(value)
		return err == nil && Validate("ReloadEdit", raw) == nil
	}
	if !valid(edit) {
		t.Fatal(edit)
	}
	edit.Revision = new(Counter(9007199254740994))
	if valid(edit) {
		t.Fatal("pending result has applied revision")
	}
	edit.State = "applied"
	if valid(edit) {
		t.Fatal("applied result has no settlement")
	}
	edit.SettledAt = new(at.Format(time.RFC3339Nano))
	if !valid(edit) {
		t.Fatal(edit)
	}
	raw, err := json.Marshal(edit)
	if err != nil || !strings.Contains(string(raw), `"revision":"9007199254740994"`) {
		t.Fatal(string(raw), err)
	}
	edit.State = "interrupted"
	if valid(edit) {
		t.Fatal("interrupted result claims applied revision")
	}
	edit.Revision = nil
	if !valid(edit) {
		t.Fatal(edit)
	}
	for _, raw := range []string{`{"edit_id":"reload","session_id":"root","expected_revision":"0"}`, `{"edit_id":"reload","session_id":"root","expected_revision":9007199254740993}`, `{"edit_id":"reload","session_id":"root","expected_revision":"01"}`} {
		if Validate("ReloadSessionParams", []byte(raw)) == nil {
			t.Fatal(raw)
		}
	}
}
