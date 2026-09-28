package session

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCompactionDraftBoundsAndExactCounters(t *testing.T) {
	valid := CompactionDraft{ID: "summary", ThroughSequence: 1, PinnedMessageIDs: []MessageID{"input"}, Text: "retained obligations"}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*CompactionDraft){
		func(d *CompactionDraft) { d.ID = "" },
		func(d *CompactionDraft) { d.ExpectedRevision = -1 },
		func(d *CompactionDraft) { d.ThroughSequence = 0 },
		func(d *CompactionDraft) { d.BaseID = &d.ID; d.ExpectedRevision = 1 },
		func(d *CompactionDraft) { d.BaseID = new(CompactionID("base")) },
		func(d *CompactionDraft) { d.PinnedMessageIDs = []MessageID{"input", "input"} },
		func(d *CompactionDraft) { d.PinnedMessageIDs = []MessageID{""} },
		func(d *CompactionDraft) { d.PinnedMessageIDs = make([]MessageID, MaxCompactionPins+1) },
		func(d *CompactionDraft) { d.Text = " \n " },
		func(d *CompactionDraft) { d.Text = "bad\x00text" },
		func(d *CompactionDraft) { d.Text = "bad\xfftext" },
		func(d *CompactionDraft) { d.Text = strings.Repeat("x", MaxCompactionBytes+1) },
	} {
		draft := valid
		mutate(&draft)
		if err := draft.Validate(); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid draft accepted: %+v %v", draft, err)
		}
	}
	valid.Text = strings.Repeat("💡", MaxCompactionBytes/4)
	if err := valid.Validate(); err != nil {
		t.Fatal("exact byte bound rejected", err)
	}
	encoded, err := json.Marshal(ContextHead{SessionID: "session", Revision: 9007199254740993})
	if err != nil || !strings.Contains(string(encoded), `"revision":"9007199254740993"`) || !strings.Contains(string(encoded), `"compaction_id":null`) {
		t.Fatalf("inexact/missing context head fields: %s %v", encoded, err)
	}
}
