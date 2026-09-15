package session

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"time"
	"unicode/utf8"
)

// Spans are the durable trace of a session: one row per unit of work the
// daemon already observes (a turn, a model attempt, a model tool call, a host
// call inside a cell, a wait on a human), with nanosecond start and end times
// captured on the goroutine that saw the boundary. Rows are an index over
// bodies Whip already stores; attrs hold pointers and short excerpts, never
// whole prompts or tool outputs. The journal carries span.started and
// span.ended so a live client sees a span the moment it begins; the table
// outlives the journal's retention window so a trace can always be paged in
// full.
const (
	SpanKindAgent = "agent" // one turn of one agent
	SpanKindLLM   = "llm"   // one provider attempt
	SpanKindTool  = "tool"  // one model tool call (rlm_exec cell, bash, custom tool)
	SpanKindHost  = "host"  // one host call made from inside a cell
	SpanKindWait  = "wait"  // a permission prompt or user question

	SpanStatusRunning     = "running"
	SpanStatusOK          = "ok"
	SpanStatusError       = "error"
	SpanStatusCancelled   = "cancelled"
	SpanStatusInterrupted = "interrupted"

	// SpanExcerptLimit bounds every excerpt kept in span attrs. Full bodies
	// stay in transcripts, operations, and the content store.
	SpanExcerptLimit = 512
	MaxSpanPage      = 2048
)

// SpanRecord is one span, on disk and on the wire. Nanosecond stamps are
// decimal strings in JSON like every other int64 in the protocol.
type SpanRecord struct {
	ID         string          `json:"id"`
	TraceID    string          `json:"trace_id"`
	ParentID   string          `json:"parent_id,omitempty"`
	RootID     string          `json:"root_id"`
	AgentID    string          `json:"agent_id"`
	TurnID     string          `json:"turn_id,omitempty"`
	Kind       string          `json:"kind"`
	Name       string          `json:"name"`
	Status     string          `json:"status"`
	StartNS    int64           `json:"start_ns,string"`
	EndNS      int64           `json:"end_ns,string"` // 0 while the span is open
	Attrs      json.RawMessage `json:"attrs,omitempty"`
	Links      json.RawMessage `json:"links,omitempty"` // [{"trace_id","span_id"}] other causes of this span
	UpdatedSeq int64           `json:"updated_seq,string"`
}

// SpanLink names another span that contributed to this one without being its
// parent, such as the second and later mailbox messages a digest turn consumed.
type SpanLink struct {
	TraceID string `json:"trace_id"`
	SpanID  string `json:"span_id"`
}

// SpanPage is one bounded read of a root's spans ordered by their last write,
// so a client can page from zero and then apply span.* events after NextSeq.
type SpanPage struct {
	RootID       string       `json:"root_id"`
	Spans        []SpanRecord `json:"spans"`
	NextSeq      int64        `json:"next_seq,string"`
	HasMore      bool         `json:"has_more"`
	ServerTimeNS int64        `json:"server_time_ns,string"`
}

func spanHash(size int, parts ...string) string {
	digest := sha256.New()
	for _, part := range parts {
		digest.Write([]byte(part))
		digest.Write([]byte{0})
	}
	sum := digest.Sum(nil)
	return hex.EncodeToString(sum[:size])
}

// TraceIDForTurn is the 16-byte trace identity of a turn that starts its own
// trace: every root turn, and a child turn nothing caused.
func TraceIDForTurn(turnID string) string { return spanHash(16, "whip-trace", turnID) }

// Span identities are derived, not stored, so a start and its end agree
// without coordination and a repeated write is idempotent.
func TurnSpanID(rootID, agentID, turnID string) string {
	return spanHash(8, SpanKindAgent, rootID, agentID, turnID)
}

func ModelCallSpanID(rootID, callID string) string { return spanHash(8, SpanKindLLM, rootID, callID) }

func ToolSpanID(rootID, agentID, turnID, toolCallID string) string {
	return spanHash(8, SpanKindTool, rootID, agentID, turnID, toolCallID)
}

func HostSpanID(rootID, agentID, turnID, toolCallID, invocationID string) string {
	return spanHash(8, SpanKindHost, rootID, agentID, turnID, toolCallID, invocationID)
}

func WaitSpanID(rootID, waitID string) string { return spanHash(8, SpanKindWait, rootID, waitID) }

// SpanExcerpt bounds a body for span attrs on a rune boundary.
func SpanExcerpt(text string) string {
	if len(text) <= SpanExcerptLimit {
		return text
	}
	cut := SpanExcerptLimit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "…"
}

// SpanAttrs encodes attrs, dropping empty values so json_patch never has to
// merge noise.
func SpanAttrs(values map[string]any) json.RawMessage {
	clean := make(map[string]any, len(values))
	for key, value := range values {
		switch v := value.(type) {
		case nil:
			continue
		case string:
			if v == "" {
				continue
			}
		case int64:
			if v == 0 {
				continue
			}
		case int:
			if v == 0 {
				continue
			}
		}
		clean[key] = value
	}
	if len(clean) == 0 {
		return json.RawMessage(`{}`)
	}
	encoded, err := json.Marshal(clean)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return encoded
}

func spanLinksJSON(links []SpanLink) json.RawMessage {
	if len(links) == 0 {
		return json.RawMessage(`[]`)
	}
	encoded, err := json.Marshal(links)
	if err != nil {
		return json.RawMessage(`[]`)
	}
	return encoded
}

func validateSpan(record SpanRecord) error {
	if record.RootID == "" || record.ID == "" || record.TraceID == "" || record.AgentID == "" || record.Kind == "" || record.Name == "" || record.Status == "" || record.StartNS <= 0 {
		return errors.New("span requires root, id, trace, agent, kind, name, status, and a start")
	}
	if record.EndNS != 0 && record.EndNS < record.StartNS {
		return errors.New("span cannot end before it starts")
	}
	return nil
}

// RecordSpanStart opens a span. A span that already exists is left untouched
// and emits nothing, so a repeated start (a re-admitted model attempt) is
// harmless.
func (s *Store) RecordSpanStart(ctx context.Context, record SpanRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.startSpanTx(ctx, tx, record, now()); err != nil {
		return err
	}
	return tx.Commit()
}

// RecordSpanEnd closes a span. When the start was never written the whole
// record is inserted closed, so a lost start cannot hide a finished unit of
// work; a span that already ended is left untouched.
func (s *Store) RecordSpanEnd(ctx context.Context, record SpanRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.endSpanTx(ctx, tx, record, now()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) startSpanTx(ctx context.Context, tx *sql.Tx, record SpanRecord, stamp string) error {
	record.EndNS = 0
	if record.Status == "" {
		record.Status = SpanStatusRunning
	}
	if err := validateSpan(record); err != nil {
		return err
	}
	seq, err := nextEventSeqTx(ctx, tx, record.RootID)
	if err != nil {
		return err
	}
	record.UpdatedSeq = seq
	result, err := tx.ExecContext(ctx, `INSERT INTO spans(root_id,id,trace_id,parent_id,agent_id,turn_id,kind,name,start_ns,end_ns,status,attrs,links,updated_seq)
		VALUES(?,?,?,?,?,?,?,?,?,0,?,?,?,?) ON CONFLICT(id) DO NOTHING`,
		record.RootID, record.ID, record.TraceID, record.ParentID, record.AgentID, record.TurnID, record.Kind, record.Name,
		record.StartNS, record.Status, string(attrsOrEmpty(record.Attrs)), string(linksOrEmpty(record.Links)), seq)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return err
	}
	return s.emitSpanEventTx(ctx, tx, record.RootID, record.ID, "span.started", seq, stamp)
}

func (s *Store) endSpanTx(ctx context.Context, tx *sql.Tx, record SpanRecord, stamp string) error {
	if record.EndNS <= 0 {
		record.EndNS = time.Now().UnixNano()
	}
	if record.StartNS <= 0 {
		record.StartNS = record.EndNS
	}
	if record.Status == "" || record.Status == SpanStatusRunning {
		record.Status = SpanStatusOK
	}
	if err := validateSpan(record); err != nil {
		return err
	}
	seq, err := nextEventSeqTx(ctx, tx, record.RootID)
	if err != nil {
		return err
	}
	record.UpdatedSeq = seq
	// json_patch merges the settlement attrs over the start attrs; the WHERE
	// keeps a span that already ended untouched.
	result, err := tx.ExecContext(ctx, `INSERT INTO spans(root_id,id,trace_id,parent_id,agent_id,turn_id,kind,name,start_ns,end_ns,status,attrs,links,updated_seq)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET
		end_ns=excluded.end_ns, status=excluded.status, attrs=json_patch(spans.attrs,excluded.attrs),
		links=CASE WHEN json_array_length(excluded.links)>0 THEN excluded.links ELSE spans.links END,
		parent_id=CASE WHEN spans.parent_id='' THEN excluded.parent_id ELSE spans.parent_id END,
		updated_seq=excluded.updated_seq WHERE spans.end_ns=0`,
		record.RootID, record.ID, record.TraceID, record.ParentID, record.AgentID, record.TurnID, record.Kind, record.Name,
		record.StartNS, record.EndNS, record.Status, string(attrsOrEmpty(record.Attrs)), string(linksOrEmpty(record.Links)), seq)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return err
	}
	return s.emitSpanEventTx(ctx, tx, record.RootID, record.ID, "span.ended", seq, stamp)
}

// interruptOpenSpansTx closes every open span of a root, or of the listed
// agents, as interrupted. Recovery and subtree stops call it so a crashed or
// stopped turn never leaves a span drawn to now forever.
func (s *Store) interruptOpenSpansTx(ctx context.Context, tx *sql.Tx, rootID string, agentIDs []string, stamp string) error {
	ids, err := openSpanIDsTx(ctx, tx, rootID)
	if err != nil {
		return err
	}
	endNS := time.Now().UnixNano()
	for _, id := range ids {
		record, err := readSpanTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if len(agentIDs) > 0 && !slices.Contains(agentIDs, record.AgentID) {
			continue
		}
		record.EndNS, record.Status = endNS, SpanStatusInterrupted
		record.Attrs = SpanAttrs(map[string]any{"error": "interrupted before completion"})
		if err := s.endSpanTx(ctx, tx, record, stamp); err != nil {
			return err
		}
	}
	return nil
}

// openSpanIDsTx lists the open spans of one root, or of every root.
func openSpanIDsTx(ctx context.Context, tx *sql.Tx, rootID string) ([]string, error) {
	query := `SELECT id FROM spans WHERE end_ns=0`
	var args []any
	if rootID != "" {
		query += ` AND root_id=?`
		args = append(args, rootID)
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) emitSpanEventTx(ctx context.Context, tx *sql.Tx, rootID, spanID, kind string, seq int64, stamp string) error {
	record, err := readSpanTx(ctx, tx, spanID)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return s.insertEventWithSeqTx(ctx, tx, rootID, seq, kind, payload, "span event", stamp)
}

const spanColumns = `root_id,id,trace_id,parent_id,agent_id,turn_id,kind,name,start_ns,end_ns,status,attrs,links,updated_seq`

func scanSpan(scanner interface{ Scan(...any) error }) (SpanRecord, error) {
	var record SpanRecord
	var attrs, links string
	if err := scanner.Scan(&record.RootID, &record.ID, &record.TraceID, &record.ParentID, &record.AgentID, &record.TurnID,
		&record.Kind, &record.Name, &record.StartNS, &record.EndNS, &record.Status, &attrs, &links, &record.UpdatedSeq); err != nil {
		return SpanRecord{}, err
	}
	if attrs != "" && attrs != "{}" {
		record.Attrs = json.RawMessage(attrs)
	}
	if links != "" && links != "[]" {
		record.Links = json.RawMessage(links)
	}
	return record, nil
}

func readSpanTx(ctx context.Context, tx *sql.Tx, id string) (SpanRecord, error) {
	return scanSpan(tx.QueryRowContext(ctx, `SELECT `+spanColumns+` FROM spans WHERE id=?`, id))
}

// TurnSpan resolves the span and trace identity of a turn. A turn recorded
// before spans existed falls back to its own trace so live writes still land.
func (s *Store) TurnSpan(ctx context.Context, rootID, agentID, turnID string) (spanID, traceID string, err error) {
	spanID = TurnSpanID(rootID, agentID, turnID)
	err = s.db.QueryRowContext(ctx, `SELECT trace_id FROM spans WHERE id=?`, spanID).Scan(&traceID)
	if errors.Is(err, sql.ErrNoRows) {
		return spanID, TraceIDForTurn(turnID), nil
	}
	return spanID, traceID, err
}

// PageSpans reads spans written after afterSeq, oldest write first. A span
// appears again when it ends, so callers merge by id. rootsOnly selects the
// turn spans that start a trace, which is the trace picker's list.
func (s *Store) PageSpans(ctx context.Context, rootID, traceID string, afterSeq int64, limit int, rootsOnly bool) (SpanPage, error) {
	if rootID == "" || afterSeq < 0 {
		return SpanPage{}, errors.New("span page requires a root and a nonnegative cursor")
	}
	if limit < 1 || limit > MaxSpanPage {
		limit = MaxSpanPage
	}
	query := `SELECT ` + spanColumns + ` FROM spans WHERE root_id=? AND updated_seq>?`
	args := []any{rootID, afterSeq}
	if traceID != "" {
		query += ` AND trace_id=?`
		args = append(args, traceID)
	}
	if rootsOnly {
		query += ` AND parent_id='' AND kind='agent'`
	}
	query += ` ORDER BY updated_seq LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return SpanPage{}, err
	}
	defer func() { _ = rows.Close() }()
	page := SpanPage{RootID: rootID, NextSeq: afterSeq, ServerTimeNS: time.Now().UnixNano(), Spans: []SpanRecord{}}
	for rows.Next() {
		record, err := scanSpan(rows)
		if err != nil {
			return SpanPage{}, err
		}
		if len(page.Spans) == limit {
			page.HasMore = true
			break
		}
		page.Spans = append(page.Spans, record)
		page.NextSeq = record.UpdatedSeq
	}
	if err := rows.Err(); err != nil {
		return SpanPage{}, err
	}
	return page, nil
}

// SpansForTrace reads every span of one trace in start order, for export.
func (s *Store) SpansForTrace(ctx context.Context, rootID, traceID string) ([]SpanRecord, error) {
	if rootID == "" {
		return nil, errors.New("trace read requires a root")
	}
	query := `SELECT ` + spanColumns + ` FROM spans WHERE root_id=?`
	args := []any{rootID}
	if traceID != "" {
		query += ` AND trace_id=?`
		args = append(args, traceID)
	}
	query += ` ORDER BY start_ns, id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var spans []SpanRecord
	for rows.Next() {
		record, err := scanSpan(rows)
		if err != nil {
			return nil, err
		}
		spans = append(spans, record)
	}
	return spans, rows.Err()
}

func attrsOrEmpty(attrs json.RawMessage) json.RawMessage {
	if len(attrs) == 0 || !json.Valid(attrs) {
		return json.RawMessage(`{}`)
	}
	return attrs
}

func linksOrEmpty(links json.RawMessage) json.RawMessage {
	if len(links) == 0 || !json.Valid(links) {
		return json.RawMessage(`[]`)
	}
	return links
}

// turnSpanNameTx names a turn span after its agent. The definition rides in
// attrs so a root named "root" still says which agent definition ran.
func turnSpanNameTx(ctx context.Context, tx *sql.Tx, rootID, agentID string) (name, definition string) {
	_ = tx.QueryRowContext(ctx, `SELECT name,definition FROM agents WHERE root_id=? AND id=?`, rootID, agentID).Scan(&name, &definition)
	if name == "" {
		name = "turn"
	}
	if definition == "" {
		_ = tx.QueryRowContext(ctx, `SELECT definition FROM sessions WHERE id=?`, rootID).Scan(&definition)
	}
	return name, definition
}

// inboxCauseTx reads the causal identity a child input carried, if any.
func inboxCauseTx(ctx context.Context, tx *sql.Tx, rootID, agentID string, seq int64) (parentSpanID, traceID string, err error) {
	err = tx.QueryRowContext(ctx, `SELECT parent_span_id,span_trace_id FROM inbox WHERE root_id=? AND agent_id=? AND seq=?`, rootID, agentID, seq).Scan(&parentSpanID, &traceID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	return parentSpanID, traceID, err
}

// mailboxCausesTx lists the sender spans of the messages a mailbox-triggered
// turn is about to consume, in digest order: the first is the parent, the
// rest are links.
func mailboxCausesTx(ctx context.Context, tx *sql.Tx, rootID, agentID, stamp string) ([]SpanLink, error) {
	rows, err := tx.QueryContext(ctx, `SELECT sender_span_id,span_trace_id FROM agent_messages
		WHERE root_id=? AND recipient_agent_id=? AND status='pending' AND available_at<=? AND delivery<>'next_turn' AND sender_span_id<>''
		ORDER BY created_at,rowid LIMIT 64`, rootID, agentID, stamp)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var links []SpanLink
	for rows.Next() {
		var link SpanLink
		if err := rows.Scan(&link.SpanID, &link.TraceID); err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	return links, rows.Err()
}

func spanStatusForTurn(status string) string {
	switch status {
	case "succeeded":
		return SpanStatusOK
	case "failed":
		return SpanStatusError
	case "cancelled":
		return SpanStatusCancelled
	case "interrupted":
		return SpanStatusInterrupted
	}
	return SpanStatusError
}
