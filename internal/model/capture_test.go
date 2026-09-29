package model

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestInspectFreezesOnlyPublicInstructionEvidence(t *testing.T) {
	request := Request{Instructions: "base\nnotice", Notices: "\nnotice", CacheKey: "secret-cache", Messages: []Message{{ID: "message", Role: session.User, Continuation: &session.ModelContinuation{Scope: strings.Repeat("c", 64), Data: `["private-continuation"]`}, Parts: []session.Part{{Type: "text", Text: "private-message"}}}}, Contents: map[string]Content{"image": {Data: []byte("private-image")}}, Tools: []Tool{{Name: "execute", InputSchema: json.RawMessage(`{"type":"object"}`)}}}
	capture := Inspect(request)
	request.Messages[0].Parts[0].Text = "changed"
	if string(capture.Instructions.Data) != "base" || string(capture.Notices.Data) != "\nnotice" || capture.Messages[0].ID != "message" || capture.Messages[0].PartsDigest == session.CaptureDigest([]byte("changed")) {
		t.Fatal(capture)
	}
	encoded, err := json.Marshal(capture)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"secret-cache", "private-message", "private-image", "private-continuation", "base", "notice"} {
		if strings.Contains(string(encoded), `"`+private+`"`) {
			t.Fatalf("private material escaped: %s", private)
		}
	}
	oversized := Inspect(Request{Instructions: strings.Repeat("x", session.MaxModelCaptureBytes+1)})
	if oversized.Instructions.Status != "oversized" || len(oversized.Instructions.Data) != 0 || oversized.Instructions.Bytes != session.MaxModelCaptureBytes+1 {
		t.Fatal("unbounded capture")
	}
}
