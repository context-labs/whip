package protocol

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func TestForkReceiptPreservesExactBoundaryAfterDeletion(t *testing.T) {
	value, err := ForkResultFromDomain(session.ForkResult{Fork: session.Fork{
		ForkRequest: session.ForkRequest{ID: "fork", SessionID: "source", ExpectedHistoryRevision: 9007199254740993, ExpectedConfigRevision: 9007199254740994, ObservedThrough: 9007199254740995},
		TreeID:      "tree", RootID: "root", CreatedAt: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC),
	}, Deleted: true})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(value)
	if err != nil || Validate("ForkResult", raw) != nil {
		t.Fatal("invalid projected fork", string(raw), err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["root"] != nil || fields["tree"] != nil || fields["deleted"] != true || fields["fork"].(map[string]any)["observed_through"] != "9007199254740995" {
		t.Fatal("deletion or exact fork coordinates lost", string(raw))
	}
	for _, invalid := range []string{
		`{"fork_id":"fork","session_id":"source","expected_history_revision":1,"expected_config_revision":"1","observed_through":"0","keep_through":"0","title":null}`,
		`{"fork_id":"fork","session_id":"source","expected_history_revision":"1","expected_config_revision":"1","observed_through":"9223372036854775808","keep_through":"0","title":null}`,
	} {
		if Validate("ForkParams", json.RawMessage(invalid)) == nil {
			t.Fatal("accepted lossy fork boundary", invalid)
		}
	}
}
