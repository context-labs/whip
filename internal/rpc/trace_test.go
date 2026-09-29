package rpc_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

func TestTraceRPCExactProjectionExportAndConflict(t *testing.T) {
	r, c := fixture(t)
	root := create(t, c).Root
	empty := call[protocol.TracePageResult](t, c, "trace.page", protocol.TracePageParams{RootID: root.ID, Limit: 10, MaxBytes: 4096})
	if len(empty.Items) != 0 || empty.Revision != 0 {
		t.Fatal(empty)
	}
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	identity := protocol.RequestIdentity{ClientID: "trace", RequestID: "input"}
	call[protocol.Admission](t, c, "sessions.submit", protocol.SubmitParams{Identity: identity, SessionID: root.ID, Source: "user", Parts: []protocol.Part{{Type: "text", Text: "trace response"}}})
	deadline := time.Now().Add(5 * time.Second)
	for {
		a := call[protocol.Admission](t, c, "receipts.get", identity)
		if a.Turn != nil && a.Turn.FinishedAt != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("completion timed out")
		}
		time.Sleep(time.Millisecond)
	}
	page := call[protocol.TracePageResult](t, c, "trace.page", protocol.TracePageParams{RootID: root.ID, Limit: 10, MaxBytes: 524288})
	if len(page.Items) != 2 || page.HasMore || page.Next != page.Revision {
		t.Fatal(page)
	}
	exported := call[protocol.TraceExportResult](t, c, "trace.export", protocol.TraceExportParams{RootID: root.ID, ExpectedRevision: &page.Revision})
	if exported.Spans != 2 || exported.Traces != 1 || exported.Reference.SessionID != root.ID {
		t.Fatal(exported)
	}
	content := call[protocol.ReadContentResult](t, c, "content.read", protocol.ReadContentParams{SessionID: root.ID, ReferenceID: exported.Reference.ID})
	body, err := base64.StdEncoding.DecodeString(content.DataBase64)
	if err != nil || !json.Valid(body) {
		t.Fatal(err)
	}
	var wire *client.Error
	var response json.RawMessage
	if err := c.Call(t.Context(), "trace.page", protocol.TracePageParams{RootID: root.ID, ExpectedRevision: new(protocol.Counter(0)), Limit: 10, MaxBytes: 4096}, &response); !errors.As(err, &wire) || wire.Kind != "CONFLICT" {
		t.Fatal(err)
	}
	foreign := create(t, c).Root
	if err := c.Call(t.Context(), "content.read", protocol.ReadContentParams{SessionID: foreign.ID, ReferenceID: exported.Reference.ID}, &response); !errors.As(err, &wire) || wire.Kind != "NOT_FOUND" {
		t.Fatal(err)
	}
}
