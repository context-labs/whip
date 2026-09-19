package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
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
		span := session.SpanRecord{
			ID: session.ToolSpanID(root, root, "t1", strings.Repeat("c", 8)+string(rune('a'+i%26))+string(rune('a'+i/26))), TraceID: turn.TraceID, ParentID: turn.ID, RootID: root, AgentID: root, TurnID: "t1",
			Kind: session.SpanKindTool, Name: "bash", StartNS: start + int64(i+1)*1000, EndNS: start + int64(i+2)*1000, Status: session.SpanStatusOK,
			Attrs: session.SpanAttrs(map[string]any{"summary": strings.Repeat("s", 200), "input": strings.Repeat("i", 300)}),
		}
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

// Historical trace reads must not construct a session actor, even after the
// database is reopened by a new daemon with no roots in memory.
func TestTracePageAfterReopenDoesNotOpenRuntime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	store := openStore(t, path)
	defer store.Close()
	root := createRoot(t, store)
	emptyRoot := createRoot(t, store)
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano()
	turn := session.SpanRecord{
		ID:      session.TurnSpanID(root, root, "historical"),
		TraceID: session.TraceIDForTurn("historical"),
		RootID:  root, AgentID: root, TurnID: "historical", Kind: session.SpanKindAgent,
		Name: "root", StartNS: start, EndNS: start + 1000, Status: session.SpanStatusOK,
	}
	if err := store.RecordSpanEnd(t.Context(), turn); err != nil {
		t.Fatal(err)
	}
	child := session.SpanRecord{
		ID: session.ToolSpanID(root, root, turn.TurnID, "call"), TraceID: turn.TraceID, ParentID: turn.ID,
		RootID: root, AgentID: root, TurnID: turn.TurnID, Kind: session.SpanKindTool,
		Name: "files.read", StartNS: start + 100, EndNS: start + 200, Status: session.SpanStatusOK,
	}
	if err := store.RecordSpanEnd(t.Context(), child); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openStore(t, path)
	opens := 0
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		opens++
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	server := &Server{daemon: owner}
	page := func(params protocol.TracePageParams) session.SpanPage {
		t.Helper()
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		result, failure := server.handle(
			&serverConn{ctx: t.Context()}, rpcMessage{Method: "trace.page", Params: raw},
		)
		if failure != nil {
			t.Fatal(failure)
		}
		return result.(session.SpanPage)
	}
	first := page(protocol.TracePageParams{RootID: root, Limit: 1})
	if len(first.Spans) != 1 || first.Spans[0].ID != turn.ID || first.Spans[0].EndNS != turn.EndNS ||
		!first.HasMore || first.NextSeq <= 0 || first.ServerTimeNS <= start {
		t.Fatalf("first historical page: %+v", first)
	}
	second := page(protocol.TracePageParams{RootID: root, AfterSeq: first.NextSeq, Limit: 1})
	if len(second.Spans) != 1 || second.Spans[0].ID != child.ID ||
		second.HasMore || second.NextSeq <= first.NextSeq {
		t.Fatalf("second historical page: %+v", second)
	}
	last := page(protocol.TracePageParams{RootID: root, AfterSeq: second.NextSeq})
	if last.Spans == nil || len(last.Spans) != 0 || last.HasMore || last.NextSeq != second.NextSeq {
		t.Fatalf("exhausted historical page: %+v", last)
	}
	roots := page(protocol.TracePageParams{RootID: root, TraceID: turn.TraceID, RootsOnly: true})
	if len(roots.Spans) != 1 || roots.Spans[0].ID != turn.ID || roots.HasMore {
		t.Fatalf("historical trace roots: %+v", roots)
	}
	empty := page(protocol.TracePageParams{RootID: emptyRoot})
	if empty.RootID != emptyRoot || empty.Spans == nil || len(empty.Spans) != 0 ||
		empty.HasMore || empty.NextSeq != 0 || empty.ServerTimeNS <= 0 {
		t.Fatalf("empty historical page: %+v", empty)
	}
	if opens != 0 {
		t.Fatalf("historical reads constructed %d session actors", opens)
	}

	// Stopping an actor leaves a tombstone in the registry. Trace reads must
	// still use the durable rows rather than consulting that actor.
	active, err := owner.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	active.Stop()
	select {
	case <-active.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("fixture session did not stop")
	}
	stopped := page(protocol.TracePageParams{RootID: root})
	if len(stopped.Spans) != 2 || stopped.HasMore || opens != 1 {
		t.Fatalf("trace after actor stop: page=%+v opens=%d", stopped, opens)
	}
}
