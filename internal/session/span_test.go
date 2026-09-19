package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
)

// dropTraceSchema removes the schema 19 additions so legacy fixtures built
// from the clean schema can be stamped as older versions and upgraded.
const dropTraceSchema = dropQueueSchema + `DROP TABLE spans;
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
	if earlier >= later {
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

func TestSpanRecordsRejectIncompleteOrInvertedSpans(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	turnID, _ := startRootTurnForTest(t, store, root, agent, "validate")
	trace := TraceIDForTurn(turnID)
	base := SpanRecord{ID: "abc", TraceID: trace, RootID: root, AgentID: agent, TurnID: turnID, Kind: SpanKindTool, Name: "bash", StartNS: 10}
	for name, broken := range map[string]func(SpanRecord) SpanRecord{
		"no id":       func(r SpanRecord) SpanRecord { r.ID = ""; return r },
		"no trace":    func(r SpanRecord) SpanRecord { r.TraceID = ""; return r },
		"no start":    func(r SpanRecord) SpanRecord { r.StartNS = 0; return r },
		"no name":     func(r SpanRecord) SpanRecord { r.Name = ""; return r },
		"ends before": func(r SpanRecord) SpanRecord { r.EndNS = 5; return r },
	} {
		record := broken(base)
		var err error
		if name == "ends before" {
			err = store.RecordSpanEnd(context.Background(), record)
		} else {
			err = store.RecordSpanStart(context.Background(), record)
		}
		if err == nil {
			t.Fatalf("%s: expected a validation error", name)
		}
	}
	// A missing turn yields the derived identity, never an error.
	spanID, traceID, err := store.TurnSpan(context.Background(), root, agent, "never-started")
	if err != nil || spanID != TurnSpanID(root, agent, "never-started") || traceID != TraceIDForTurn("never-started") {
		t.Fatalf("turn span fallback=%s %s %v", spanID, traceID, err)
	}
	for status, want := range map[string]string{"succeeded": SpanStatusOK, "failed": SpanStatusError, "cancelled": SpanStatusCancelled, "interrupted": SpanStatusInterrupted, "weird": SpanStatusError} {
		if got := spanStatusForTurn(status); got != want {
			t.Fatalf("spanStatusForTurn(%s)=%s", status, got)
		}
	}
	if excerpt := SpanExcerpt(strings.Repeat("é", SpanExcerptLimit)); !strings.HasSuffix(excerpt, "…") || len(excerpt) > SpanExcerptLimit+len("…") {
		t.Fatalf("excerpt did not cut on a rune boundary: %d bytes", len(excerpt))
	}
	if _, err := store.PageSpans(context.Background(), "", "", 0, 0, false); err == nil {
		t.Fatal("paging without a root must fail")
	}
	if _, err := store.SpansForTrace(context.Background(), "", ""); err == nil {
		t.Fatal("reading a trace without a root must fail")
	}
}

func TestSpanHelpersFilterTracesAndSurfaceStoreErrors(t *testing.T) {
	if got := SpanExcerpt(strings.Repeat("é", SpanExcerptLimit)); !utf8.ValidString(got) || !strings.HasSuffix(got, "…") {
		t.Fatalf("excerpt must cut on a rune boundary: %q", got[len(got)-6:])
	}
	if got := string(SpanAttrs(map[string]any{"nil": nil, "empty": "", "zero": int64(0)})); got != "{}" {
		t.Fatalf("empty values must be dropped: %s", got)
	}
	store, root, agent := newSwarmFixture(t)
	ctx := context.Background()
	turnID, _ := startRootTurnForTest(t, store, root, agent, "first")
	other := SpanRecord{ID: TurnSpanID(root, agent, "t-other"), TraceID: TraceIDForTurn("t-other"), RootID: root, AgentID: agent, TurnID: "t-other", Kind: SpanKindAgent, Name: "root", StartNS: time.Now().UnixNano()}
	if err := store.RecordSpanStart(ctx, other); err != nil {
		t.Fatal(err)
	}
	spans, err := store.SpansForTrace(ctx, root, other.TraceID)
	if err != nil || len(spans) != 1 || spans[0].ID != other.ID {
		t.Fatalf("trace filter: %d spans, err=%v", len(spans), err)
	}
	if all, err := store.SpansForTrace(ctx, root, ""); err != nil || len(all) != 2 {
		t.Fatalf("unfiltered read: %d spans, err=%v", len(all), err)
	}
	if _, err := store.SpansForTrace(ctx, "", ""); err == nil {
		t.Fatal("a trace read needs a root")
	}
	// A turn that has not started yet still has deterministic ids, so callers
	// can parent onto it before its span row exists.
	if spanID, traceID, err := store.TurnSpan(ctx, root, agent, "never-started"); err != nil || spanID != TurnSpanID(root, agent, "never-started") || traceID != TraceIDForTurn("never-started") {
		t.Fatalf("unknown turn ids: %q %q %v", spanID, traceID, err)
	}
	if spanID, _, err := store.TurnSpan(ctx, root, agent, turnID); err != nil || spanID != TurnSpanID(root, agent, turnID) {
		t.Fatalf("turn span lookup: %q %v", spanID, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSpanStart(ctx, other); err == nil {
		t.Fatal("span start on a closed store must fail")
	}
	other.EndNS = other.StartNS + 1
	if err := store.RecordSpanEnd(ctx, other); err == nil {
		t.Fatal("span end on a closed store must fail")
	}
	if _, err := store.PageSpans(ctx, root, "", 0, 10, false); err == nil {
		t.Fatal("paging a closed store must fail")
	}
	if _, err := store.SpansForTrace(ctx, root, ""); err == nil {
		t.Fatal("reading a closed store must fail")
	}
	if _, _, err := store.TurnSpan(ctx, root, agent, turnID); err == nil {
		t.Fatal("turn lookup on a closed store must fail")
	}
}

// Interned bodies are shared by digest within a root, so a prompt repeated
// over many calls costs one reference; a span learns a late fact (the summary
// a compaction produced) through a patch that keeps its settlement and moves
// its cursor so live clients see the merge.
func TestInternContentReusesReferencesAndPatchesSettledSpans(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	ctx := context.Background()
	first, err := store.InternContent(ctx, root, "prompt.system", []byte("the prompt"))
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.InternContent(ctx, root, "prompt.system", []byte("the prompt"))
	if err != nil || again.ReferenceID != first.ReferenceID || again.Digest != first.Digest || again.Size != 10 {
		t.Fatalf("same body and source in one root must reuse the reference: %+v then %+v (%v)", first, again, err)
	}
	other, err := store.InternContent(ctx, root, "prompt.summary", []byte("the prompt"))
	if err != nil || other.ReferenceID == first.ReferenceID {
		t.Fatalf("a different source keeps its own reference: %+v (%v)", other, err)
	}
	if body, _, err := store.ReadContent(ctx, first.ReferenceID, root, agent, 0, MaxContentRead); err != nil || string(body) != "the prompt" {
		t.Fatalf("interned body must read back through the root grant: %q %v", body, err)
	}
	if _, err := store.InternContent(ctx, root, "prompt.system", nil); err == nil {
		t.Fatal("an empty body must be rejected")
	}
	otherRoot, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	elsewhere, err := store.InternContent(ctx, otherRoot, "prompt.system", []byte("the prompt"))
	if err != nil || elsewhere.ReferenceID == first.ReferenceID || elsewhere.Digest != first.Digest {
		t.Fatalf("another root shares the blob but not the reference: %+v (%v)", elsewhere, err)
	}

	turnID, _ := startRootTurnForTest(t, store, root, agent, "fold")
	span := SpanRecord{
		ID: ModelCallSpanID(root, "k1"), TraceID: TraceIDForTurn(turnID), ParentID: TurnSpanID(root, agent, turnID), RootID: root, AgentID: agent, TurnID: turnID,
		Kind: SpanKindLLM, Name: "compaction", StartNS: time.Now().UnixNano(), Attrs: SpanAttrs(map[string]any{"purpose": "compaction", "model_call_id": "k1"}),
	}
	if err := store.RecordSpanStart(ctx, span); err != nil {
		t.Fatal(err)
	}
	span.EndNS, span.Attrs = span.StartNS+5, SpanAttrs(map[string]any{"prompt_tokens": 9})
	if err := store.RecordSpanEnd(ctx, span); err != nil {
		t.Fatal(err)
	}
	read := func() SpanRecord {
		t.Helper()
		spans, err := store.SpansForTrace(ctx, root, "")
		if err != nil {
			t.Fatal(err)
		}
		for _, record := range spans {
			if record.ID == span.ID {
				return record
			}
		}
		t.Fatal("span missing")
		return SpanRecord{}
	}
	settled := read()
	if err := store.PatchSpanAttrs(ctx, root, span.ID, map[string]any{"output_ref": other.ReferenceID, "raw_cutoff": 7}); err != nil {
		t.Fatal(err)
	}
	patched := read()
	attrs := map[string]any{}
	if err := json.Unmarshal(patched.Attrs, &attrs); err != nil {
		t.Fatal(err)
	}
	if attrs["purpose"] != "compaction" || attrs["prompt_tokens"] != 9.0 || attrs["output_ref"] != other.ReferenceID || attrs["raw_cutoff"] != 7.0 {
		t.Fatalf("patch must merge over start and settlement attrs: %v", attrs)
	}
	if patched.EndNS != settled.EndNS || patched.Status != settled.Status || patched.UpdatedSeq <= settled.UpdatedSeq {
		t.Fatalf("patch must keep the settlement and move the cursor: before %+v after %+v", settled, patched)
	}
	page, err := store.PageSpans(ctx, root, "", settled.UpdatedSeq, 10, false)
	if err != nil || len(page.Spans) != 1 || page.Spans[0].ID != span.ID {
		t.Fatalf("a client paging after the settlement must see the patched span: %+v %v", page, err)
	}
	if err := store.PatchSpanAttrs(ctx, root, ModelCallSpanID(root, "missing"), map[string]any{"x": 1}); err == nil {
		t.Fatal("patching an unknown span must fail")
	}
	if err := store.PatchSpanAttrs(ctx, "", span.ID, map[string]any{"x": 1}); err == nil {
		t.Fatal("patching without a root must fail")
	}
}
