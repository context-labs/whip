package rpc_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/rpc"
	"github.com/context-labs/whip/internal/session"
)

func TestTraceRPCExactProjectionExportAndConflict(t *testing.T) {
	r, c := fixture(t)
	root := create(t, c).Root
	empty := call[protocol.TracePageResult](t, c, "trace.page", protocol.TracePageParams{RootID: root.ID, After: new(protocol.Counter(0)), Limit: 10, MaxBytes: 4096})
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
	page := call[protocol.TracePageResult](t, c, "trace.page", protocol.TracePageParams{RootID: root.ID, After: new(protocol.Counter(0)), Limit: 10, MaxBytes: 524288})
	if len(page.Items) != 2 || page.HasMore || page.Next != page.Revision {
		t.Fatal(page)
	}
	newest := call[protocol.TracePageResult](t, c, "trace.page", protocol.TracePageParams{RootID: root.ID, Before: new((*protocol.Counter)(nil)), Limit: 1, MaxBytes: 524288})
	if len(newest.Items) != 1 || !newest.HasMore || newest.Items[0].Sequence != page.Items[1].Sequence {
		t.Fatal("newest trace page", newest)
	}
	older := call[protocol.TracePageResult](t, c, "trace.page", protocol.TracePageParams{RootID: root.ID, Before: new(&newest.Next), ExpectedRevision: &newest.Revision, Limit: 1, MaxBytes: 524288})
	if len(older.Items) != 1 || older.HasMore || older.Next != 0 || older.Items[0].Sequence != page.Items[0].Sequence {
		t.Fatal("older trace page", older)
	}
	for _, cursor := range []string{`"after":"0","before":null`, `"after":null`, `"before":-1`, `"before":"-1"`, `"unrelated":null`} {
		var invalid json.RawMessage
		request := json.RawMessage(`{"root_id":"` + string(root.ID) + `","expected_revision":null,"trace_id":"","roots_only":false,"limit":10,"max_bytes":4096,` + cursor + `}`)
		if err := c.Call(t.Context(), "trace.page", request, &invalid); err == nil {
			t.Fatal("invalid cursor accepted by client", cursor)
		}
		if _, err := rpc.Dispatch(t.Context(), r, rpc.HostServices{}, "trace.page", request); !errors.Is(err, session.ErrInvalid) {
			t.Fatal("invalid cursor accepted by server", cursor, err)
		}
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
	if err := c.Call(t.Context(), "trace.page", protocol.TracePageParams{RootID: root.ID, After: new(protocol.Counter(0)), ExpectedRevision: new(protocol.Counter(0)), Limit: 10, MaxBytes: 4096}, &response); !errors.As(err, &wire) || wire.Kind != "CONFLICT" {
		t.Fatal(err)
	}
	foreign := create(t, c).Root
	if err := c.Call(t.Context(), "content.read", protocol.ReadContentParams{SessionID: foreign.ID, ReferenceID: exported.Reference.ID}, &response); !errors.As(err, &wire) || wire.Kind != "NOT_FOUND" {
		t.Fatal(err)
	}
}
