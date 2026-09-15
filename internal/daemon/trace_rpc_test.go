package daemon

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

// The trace RPCs page durable spans with the daemon's clock and render the
// export inline when it is small, or behind a root-scoped content reference
// when it is not.
func TestTraceRPCsPageSpansAndExportInlineOrByReference(t *testing.T) {
	fixture := newV2Fixture(t, &fakeRunner{})
	root := fixture.rootID
	start := time.Now().UnixNano()
	turn := session.SpanRecord{ID: session.TurnSpanID(root, root, "t1"), TraceID: session.TraceIDForTurn("t1"), RootID: root, AgentID: root, TurnID: "t1", Kind: session.SpanKindAgent, Name: "root", StartNS: start}
	if err := fixture.store.RecordSpanStart(t.Context(), turn); err != nil {
		t.Fatal(err)
	}
	client := fixture.dial("unix", "trace-rpc")
	page, err := client.TracePage(t.Context(), protocol.TracePageParams{RootID: root})
	if err != nil || len(page.Spans) != 1 || page.HasMore || page.ServerTimeNS <= 0 || page.Spans[0].EndNS != 0 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	roots, err := client.TracePage(t.Context(), protocol.TracePageParams{RootID: root, RootsOnly: true, AfterSeq: 0, Limit: 10})
	if err != nil || len(roots.Spans) != 1 || roots.Spans[0].ID != turn.ID {
		t.Fatalf("roots=%+v err=%v", roots, err)
	}
	if later, err := client.TracePage(t.Context(), protocol.TracePageParams{RootID: root, AfterSeq: page.NextSeq}); err != nil || len(later.Spans) != 0 {
		t.Fatalf("cursor did not exclude seen spans: %+v err=%v", later, err)
	}
	small, err := client.TraceExport(t.Context(), protocol.TraceExportParams{RootID: root})
	if err != nil || small.Spans != 1 || small.Traces != 1 || small.Content.ReferenceID == "" || small.Content.Size <= 0 {
		t.Fatalf("small export=%+v err=%v", small, err)
	}
	var body protocol.ContentReadResult
	if err := client.Call(t.Context(), "content.read", protocol.ContentReadParams{RootID: root, ReferenceID: small.Content.ReferenceID, Limit: MaxContentChunk}, &body); err != nil {
		t.Fatal(err)
	}
	var document struct {
		ResourceSpans []json.RawMessage `json:"resourceSpans"`
	}
	if err := json.Unmarshal(body.Data, &document); err != nil || len(document.ResourceSpans) != 1 {
		t.Fatalf("export is not one OTLP request: %v %s", err, body.Data)
	}
	// A much larger export is the same shape behind the same kind of reference.
	for i := range 64 {
		span := session.SpanRecord{ID: session.ToolSpanID(root, root, "t1", strings.Repeat("c", 8)+string(rune('a'+i%26))+string(rune('a'+i/26))), TraceID: turn.TraceID, ParentID: turn.ID, RootID: root, AgentID: root, TurnID: "t1",
			Kind: session.SpanKindTool, Name: "bash", StartNS: start + int64(i+1)*1000, EndNS: start + int64(i+2)*1000, Status: session.SpanStatusOK,
			Attrs: session.SpanAttrs(map[string]any{"summary": strings.Repeat("s", 200), "input": strings.Repeat("i", 300)})}
		if err := fixture.store.RecordSpanEnd(t.Context(), span); err != nil {
			t.Fatal(err)
		}
	}
	large, err := client.TraceExport(t.Context(), protocol.TraceExportParams{RootID: root, TraceID: turn.TraceID})
	if err != nil || large.Spans != 65 || large.Content.ReferenceID == "" || large.Content.Size <= small.Content.Size {
		t.Fatalf("large export=%+v err=%v", large, err)
	}
	var read protocol.ContentReadResult
	if err := client.Call(t.Context(), "content.read", protocol.ContentReadParams{RootID: root, ReferenceID: large.Content.ReferenceID, Limit: MaxContentChunk}, &read); err != nil || len(read.Data) == 0 || read.Content.Size != large.Content.Size {
		t.Fatalf("content read of the export: %+v err=%v", read.Content, err)
	}
	if _, err := client.TraceExport(t.Context(), protocol.TraceExportParams{}); err == nil {
		t.Fatal("an export without a root must fail")
	}
}
