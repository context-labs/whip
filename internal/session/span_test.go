package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
)

// dropTraceSchema removes the schema 19 additions so legacy fixtures built
// from the clean schema can be stamped as older versions and upgraded.
const dropTraceSchema = `DROP TABLE spans;
 ALTER TABLE inbox DROP COLUMN parent_span_id; ALTER TABLE inbox DROP COLUMN span_trace_id;
 ALTER TABLE agent_messages DROP COLUMN sender_span_id; ALTER TABLE agent_messages DROP COLUMN span_trace_id;
 ALTER TABLE model_calls DROP COLUMN cost_input_micros; ALTER TABLE model_calls DROP COLUMN cost_cache_read_micros; ALTER TABLE model_calls DROP COLUMN cost_output_micros;
 `

func spansByID(t *testing.T, store *Store, root, trace string) map[string]SpanRecord {
	t.Helper()
	page, err := store.PageSpans(context.Background(), root, trace, 0, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if page.HasMore {
		t.Fatal("unexpected second page")
	}
	result := make(map[string]SpanRecord, len(page.Spans))
	for _, span := range page.Spans {
		result[span.ID] = span
	}
	return result
}

func spanAttrs(t *testing.T, span SpanRecord) map[string]any {
	t.Helper()
	attrs := map[string]any{}
	if len(span.Attrs) > 0 {
		if err := json.Unmarshal(span.Attrs, &attrs); err != nil {
			t.Fatal(err)
		}
	}
	return attrs
}

func startRootTurnForTest(t *testing.T, store *Store, root, agent, input string) (turnID string, seq int64) {
	t.Helper()
	queued, err := store.EnqueueInbox(context.Background(), InboxEnqueue{RootID: root, AgentID: agent, Kind: "submit", Payload: RuntimePayload{Data: []byte(input)}})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.StartRootTurn(context.Background(), root, agent, queued.InboxSeq); err != nil {
		t.Fatal(err)
	}
	return rootTurnID(agent, queued.InboxSeq), queued.InboxSeq
}

func TestStampsAreFixedWidthNanosecondsThatParseAndOrder(t *testing.T) {
	stamp := now()
	if len(stamp) != len("2026-01-02T15:04:05.000000000Z") || !strings.HasSuffix(stamp, "Z") {
		t.Fatalf("stamp %q is not fixed-width nanosecond RFC 3339", stamp)
	}
	parsed, err := time.Parse(time.RFC3339, stamp)
	if err != nil || parsed.Nanosecond() == 0 && !strings.HasSuffix(stamp, ".000000000Z") {
		t.Fatalf("stamp %q does not parse with fractional seconds: %v", stamp, err)
	}
	earlier := formatStamp(time.Date(2026, 9, 14, 12, 0, 0, 500_000_000, time.UTC))
	later := formatStamp(time.Date(2026, 9, 14, 12, 0, 0, 500_000_001, time.UTC))
	if !(earlier < later) {
		t.Fatalf("stamps do not order as text: %q vs %q", earlier, later)
	}
}

func TestSpansRecordATurnItsCallsAndOutliveTheJournalWindow(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	turnID, seq := startRootTurnForTest(t, store, root, agent, "fix the failing test")
	turnSpan := TurnSpanID(root, agent, turnID)
	trace := TraceIDForTurn(turnID)

	spans := spansByID(t, store, root, trace)
	turn, ok := spans[turnSpan]
	if !ok || turn.Kind != SpanKindAgent || turn.ParentID != "" || turn.EndNS != 0 || turn.Status != SpanStatusRunning || turn.StartNS <= 0 {
		t.Fatalf("turn span not open at start: %+v", turn)
	}
	if attrs := spanAttrs(t, turn); attrs["input"] != "fix the failing test" || attrs["inbox_seq"] != float64(seq) {
		t.Fatalf("turn span attrs=%v", attrs)
	}

	// A model call, the cell it emitted, and a host call inside that cell.
	call := ModelCallSpanID(root, "call-1")
	cell := ToolSpanID(root, agent, turnID, "tc-1")
	host := HostSpanID(root, agent, turnID, "tc-1", "1:1")
	base := time.Now().UnixNano()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(store.RecordSpanStart(context.Background(), SpanRecord{ID: call, TraceID: trace, ParentID: turnSpan, RootID: root, AgentID: agent, TurnID: turnID, Kind: SpanKindLLM, Name: "provider/model", StartNS: base, Attrs: SpanAttrs(map[string]any{"model": "model"})}))
	must(store.RecordSpanStart(context.Background(), SpanRecord{ID: cell, TraceID: trace, ParentID: turnSpan, RootID: root, AgentID: agent, TurnID: turnID, Kind: SpanKindTool, Name: "rlm_exec", StartNS: base + 1000}))
	must(store.RecordSpanStart(context.Background(), SpanRecord{ID: host, TraceID: trace, ParentID: cell, RootID: root, AgentID: agent, TurnID: turnID, Kind: SpanKindHost, Name: "files.read", StartNS: base + 1500}))
	// A repeated start is a no-op: no second event, no changed row.
	before := len(spansByID(t, store, root, trace))
	must(store.RecordSpanStart(context.Background(), SpanRecord{ID: host, TraceID: trace, ParentID: cell, RootID: root, AgentID: agent, TurnID: turnID, Kind: SpanKindHost, Name: "files.read", StartNS: base + 9999}))
	var hostEvents int
	if err := store.db.QueryRowContext(context.Background(), `SELECT count(*) FROM events WHERE root_id=? AND kind='span.started' AND json_extract(payload_inline,'$.id')=?`, root, host).Scan(&hostEvents); err != nil || hostEvents != 1 {
		t.Fatalf("repeated start emitted %d events, %v", hostEvents, err)
	}
	must(store.RecordSpanEnd(context.Background(), SpanRecord{ID: host, TraceID: trace, ParentID: cell, RootID: root, AgentID: agent, TurnID: turnID, Kind: SpanKindHost, Name: "files.read", EndNS: base + 1541_000, Attrs: SpanAttrs(map[string]any{"operation_id": "op-1"})}))
	must(store.RecordSpanEnd(context.Background(), SpanRecord{ID: cell, TraceID: trace, ParentID: turnSpan, RootID: root, AgentID: agent, TurnID: turnID, Kind: SpanKindTool, Name: "rlm_exec", EndNS: base + 2_000_000}))
	must(store.RecordSpanEnd(context.Background(), SpanRecord{ID: call, TraceID: trace, ParentID: turnSpan, RootID: root, AgentID: agent, TurnID: turnID, Kind: SpanKindLLM, Name: "provider/model", EndNS: base + 3_000_000, Attrs: SpanAttrs(map[string]any{"prompt_tokens": 12, "cost_micros": int64(40)})}))

	spans = spansByID(t, store, root, trace)
	if len(spans) != before {
		t.Fatalf("expected %d spans, got %d", before, len(spans))
	}
	if got := spans[host]; got.ParentID != cell || got.StartNS != base+1500 || got.EndNS != base+1541_000 || got.Status != SpanStatusOK || spanAttrs(t, got)["operation_id"] != "op-1" {
		t.Fatalf("host span settled wrong: %+v", got)
	}
	if got := spans[call]; spanAttrs(t, got)["model"] != "model" || spanAttrs(t, got)["prompt_tokens"] != float64(12) {
		t.Fatalf("llm span lost start attrs on end: %+v", got)
	}
	// Ending twice changes nothing.
	must(store.RecordSpanEnd(context.Background(), SpanRecord{ID: call, TraceID: trace, ParentID: turnSpan, RootID: root, AgentID: agent, TurnID: turnID, Kind: SpanKindLLM, Name: "provider/model", EndNS: base + 9_000_000, Status: SpanStatusError}))
	if got := spansByID(t, store, root, trace)[call]; got.EndNS != base+3_000_000 || got.Status != SpanStatusOK {
		t.Fatalf("second end rewrote the span: %+v", got)
	}

	// The turn commit closes the turn span with the visible outcome.
	if err := store.CommitRootTurn(context.Background(), RootTurnCommit{RootID: root, AgentID: agent, InboxSeq: seq, Messages: []llm.Message{{Role: "assistant", Content: "All green."}}, Model: "model", Provider: "provider"}); err != nil {
		t.Fatal(err)
	}
	turn = spansByID(t, store, root, trace)[turnSpan]
	if turn.EndNS == 0 || turn.Status != SpanStatusOK || spanAttrs(t, turn)["output"] != "All green." {
		t.Fatalf("turn span not closed by commit: %+v", turn)
	}

	// Pages order by last write and resume from a cursor.
	first, err := store.PageSpans(context.Background(), root, trace, 0, 2, false)
	if err != nil || len(first.Spans) != 2 || !first.HasMore {
		t.Fatalf("first page=%+v err=%v", first, err)
	}
	rest, err := store.PageSpans(context.Background(), root, trace, first.NextSeq, 0, false)
	if err != nil || len(rest.Spans) != 2 || rest.HasMore || rest.Spans[len(rest.Spans)-1].ID != turnSpan {
		t.Fatalf("second page=%+v err=%v", rest, err)
	}
	roots, err := store.PageSpans(context.Background(), root, "", 0, 0, true)
	if err != nil || len(roots.Spans) != 1 || roots.Spans[0].ID != turnSpan {
		t.Fatalf("roots page=%+v err=%v", roots, err)
	}

	// The journal window scrolls past every span event; the table does not.
	for range EventRetention + 1 {
		if _, err := store.AppendRootEvent(context.Background(), root, "stream.text", RuntimePayload{Data: []byte(`{"text":"x"}`)}); err != nil {
			t.Fatal(err)
		}
	}
	var spanEvents int
	if err := store.db.QueryRowContext(context.Background(), `SELECT count(*) FROM events WHERE root_id=? AND kind LIKE 'span.%'`, root).Scan(&spanEvents); err != nil || spanEvents != 0 {
		t.Fatalf("expected span events evicted, found %d (%v)", spanEvents, err)
	}
	if got := len(spansByID(t, store, root, trace)); got != before {
		t.Fatalf("spans did not survive eviction: %d", got)
	}
}

func TestChildTurnsJoinTheTraceOfTheirCause(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	turnID, _ := startRootTurnForTest(t, store, root, agent, "delegate")
	trace := TraceIDForTurn(turnID)
	spawn := HostSpanID(root, agent, turnID, "tc-1", "1:1")

	// A spawn carries its host span onto the child's first input.
	if _, err := store.AdmitAgent(context.Background(), AgentAdmission{
		RootID: root, ParentAgentID: agent, ChildAgentID: "child", Name: "child", Model: "model", Provider: "provider", CWD: t.TempDir(),
		Prompt: RuntimePayload{Data: []byte("do the thing")}, ParentSpanID: spawn, SpanTraceID: trace,
	}); err != nil {
		t.Fatal(err)
	}
	start, err := store.StartAgentTurn(context.Background(), root, "child", "child-turn-1")
	if err != nil {
		t.Fatal(err)
	}
	if start.TraceID != trace || start.SpanID != TurnSpanID(root, "child", "child-turn-1") {
		t.Fatalf("child turn start=%+v", start)
	}
	child := spansByID(t, store, root, trace)[start.SpanID]
	if child.ParentID != spawn || child.TraceID != trace || child.AgentID != "child" {
		t.Fatalf("child turn span not parented under the spawn: %+v", child)
	}
	if err := store.FinishAgentTurn(context.Background(), root, "child", AgentTurnCommit{TurnID: "child-turn-1", Status: "failed", Error: "boom", AcknowledgedInbox: []int64{start.Items[0].Seq}}); err != nil {
		t.Fatal(err)
	}
	if got := spansByID(t, store, root, trace)[start.SpanID]; got.Status != SpanStatusError || spanAttrs(t, got)["error"] != "boom" {
		t.Fatalf("failed child turn span=%+v", got)
	}

	// Mail from a second child parents the recipient's next turn under the
	// sender's host call; a second message becomes a link.
	admitTestChild(t, store, root, agent, "sibling")
	senderTurn := "sibling-turn-1"
	if _, err := store.StartAgentTurn(context.Background(), root, "sibling", senderTurn); err == nil {
		t.Fatal("sibling has no input yet; expected no turn")
	}
	send := HostSpanID(root, "sibling", senderTurn, "tc-9", "1:1")
	for _, body := range []string{"first message", "second message"} {
		if _, err := store.SendMailboxMessage(context.Background(), root, "sibling", "child", MailboxSend{Body: body, SenderSpanID: send, SpanTraceID: trace}); err != nil {
			t.Fatal(err)
		}
	}
	start, err = store.StartAgentTurn(context.Background(), root, "child", "child-turn-2")
	if err != nil || start.Trigger != "mailbox" {
		t.Fatalf("mailbox turn start=%+v err=%v", start, err)
	}
	mailTurn := spansByID(t, store, root, trace)[start.SpanID]
	var links []SpanLink
	if err := json.Unmarshal(mailTurn.Links, &links); err != nil {
		t.Fatal(err)
	}
	if mailTurn.ParentID != send || mailTurn.TraceID != trace || len(links) != 1 || links[0].SpanID != send {
		t.Fatalf("mailbox turn span=%+v links=%v", mailTurn, links)
	}

	// A child input nothing caused starts its own trace.
	admitTestChild(t, store, root, agent, "orphan")
	if _, err := store.EnqueueInbox(context.Background(), InboxEnqueue{RootID: root, AgentID: "orphan", Kind: "submit", Payload: RuntimePayload{Data: []byte("hello")}}); err != nil {
		t.Fatal(err)
	}
	start, err = store.StartAgentTurn(context.Background(), root, "orphan", "orphan-turn-1")
	if err != nil || start.TraceID != TraceIDForTurn("orphan-turn-1") {
		t.Fatalf("orphan turn start=%+v err=%v", start, err)
	}
}

func TestPermissionWaitIsASpanUnderTheTurn(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	turnID, _ := startRootTurnForTest(t, store, root, agent, "run a command")
	trace := TraceIDForTurn(turnID)
	authority, err := store.EnsureAuthority(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	admission := capability.Admission{Request: capability.Request{
		RootID: root, AgentID: agent, CapabilityID: authority.Files.ID, CapabilityGeneration: authority.Files.Generation,
		OperationID: "pending-read", Operation: "read", TraceID: "dispatcher-trace",
		Reservations: []capability.Reservation{{Kind: "active_operations", Amount: 1}},
	}, RequirePermission: true}
	ticket, err := store.Begin(context.Background(), admission)
	if err != nil || ticket.PermissionID == "" {
		t.Fatalf("begin ticket=%+v err=%v", ticket, err)
	}
	wait := spansByID(t, store, root, trace)[WaitSpanID(root, ticket.PermissionID)]
	if wait.Kind != SpanKindWait || wait.ParentID != TurnSpanID(root, agent, turnID) || wait.EndNS != 0 || spanAttrs(t, wait)["operation"] != "read" {
		t.Fatalf("wait span not open under the turn: %+v", wait)
	}
	if _, err := store.Decide(context.Background(), admission, ticket.PermissionID, capability.Decision{Allow: false, PrincipalID: "human", Reason: "no"}); err == nil {
		t.Fatal("expected denial error")
	}
	wait = spansByID(t, store, root, trace)[WaitSpanID(root, ticket.PermissionID)]
	if wait.EndNS == 0 || wait.Status != SpanStatusOK || spanAttrs(t, wait)["decision"] != "denied" {
		t.Fatalf("wait span not closed by the decision: %+v", wait)
	}
}
