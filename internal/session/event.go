package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/llm"
)

const (
	EventRetention = 10_000
	MaxEventReplay = 1_000
)

var (
	ErrCursorExpired = errors.New("event cursor expired")
	ErrCursorAhead   = errors.New("event cursor is ahead of the durable stream")
)

type EventEnvelope struct {
	RootID  string
	Seq     int64
	Kind    string
	Payload RuntimeValue
	Created time.Time
}

type RootSnapshot struct {
	RootID   string
	Cursor   int64
	Meta     Meta
	Messages []llm.Message
	Agents   []RuntimeAgent
	Inbox    []InboxItem
}

// ReplayEvents returns retained envelopes strictly after cursor. A cursor
// older than the retained prefix must be replaced by an actor-consistent
// snapshot before replay continues.
func (s *Store) ReplayEvents(ctx context.Context, rootID string, cursor int64, limit int) ([]EventEnvelope, int64, error) {
	if rootID == "" || cursor < 0 || limit < 1 || limit > MaxEventReplay {
		return nil, 0, errors.New("event replay requires a root, nonnegative cursor, and bounded limit")
	}
	var earliest, latest int64
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MIN(seq),0),COALESCE(MAX(seq),0) FROM events WHERE root_id=?`, rootID).Scan(&earliest, &latest); err != nil {
		return nil, 0, err
	}
	if cursor > latest {
		return nil, latest, ErrCursorAhead
	}
	if earliest > 0 && cursor < earliest-1 {
		return nil, latest, ErrCursorExpired
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.seq,e.kind,substr(e.payload_inline,1,?),COALESCE(e.payload_ref,''),
		COALESCE(r.digest,''),COALESCE(r.size,0),COALESCE(r.media_type,''),COALESCE(r.source,''),e.created_at
		FROM events e LEFT JOIN content_references r ON r.id=e.payload_ref
		WHERE e.root_id=? AND e.seq>? ORDER BY e.seq LIMIT ?`, InlineValueLimit+1, rootID, cursor, limit)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	var events []EventEnvelope
	for rows.Next() {
		event := EventEnvelope{RootID: rootID}
		var referenceID, created string
		if err := rows.Scan(&event.Seq, &event.Kind, &event.Payload.Inline, &referenceID,
			&event.Payload.Digest, &event.Payload.Size, &event.Payload.MediaType, &event.Payload.Source, &created); err != nil {
			return nil, 0, err
		}
		if referenceID != "" {
			event.Payload.ReferenceID = referenceID
			event.Payload.Inline = nil
		} else if len(event.Payload.Inline) > InlineValueLimit {
			return nil, 0, fmt.Errorf("event %d has an oversized inline payload", event.Seq)
		}
		event.Created, _ = time.Parse(time.RFC3339, created)
		events = append(events, event)
	}
	return events, latest, rows.Err()
}

func (s *Store) ResolveRuntimeValue(ctx context.Context, rootID string, value RuntimeValue) ([]byte, error) {
	if value.ReferenceID == "" {
		return append([]byte(nil), value.Inline...), nil
	}
	data, _, err := s.ReadContent(ctx, value.ReferenceID, rootID, "", 0, MaxContentRead)
	return data, err
}

// SnapshotRoot reads the reconnect baseline and its final event cursor from
// one SQLite read transaction. Callers serialize this with the root actor.
func (s *Store) SnapshotRoot(ctx context.Context, rootID string) (RootSnapshot, error) {
	if rootID == "" {
		return RootSnapshot{}, errors.New("snapshot root is required")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return RootSnapshot{}, err
	}
	defer func() { _ = tx.Rollback() }()
	snapshot := RootSnapshot{RootID: rootID}
	var updated, tags string
	var pinned int
	if err := tx.QueryRowContext(ctx, `SELECT id,title,model,provider,cwd,goal,mode,forked_from,fork_seq,tags,pinned,effort,
		usage_in,usage_cached,usage_out,task_id,updated_at FROM sessions WHERE id=?`, rootID).Scan(
		&snapshot.Meta.ID, &snapshot.Meta.Title, &snapshot.Meta.Model, &snapshot.Meta.Provider, &snapshot.Meta.CWD,
		&snapshot.Meta.Goal, &snapshot.Meta.Mode, &snapshot.Meta.ForkedFrom, &snapshot.Meta.ForkSeq, &tags, &pinned,
		&snapshot.Meta.Effort, &snapshot.Meta.UsageIn, &snapshot.Meta.UsageCached, &snapshot.Meta.UsageOut,
		&snapshot.Meta.TaskID, &updated); err != nil {
		return RootSnapshot{}, err
	}
	if tags != "" {
		snapshot.Meta.Tags = strings.Split(tags, ",")
	}
	snapshot.Meta.Pinned = pinned != 0
	snapshot.Meta.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM events WHERE root_id=?`, rootID).Scan(&snapshot.Cursor); err != nil {
		return RootSnapshot{}, err
	}
	if err := readSnapshotMessages(ctx, tx, rootID, &snapshot); err != nil {
		return RootSnapshot{}, err
	}
	if err := readSnapshotAgents(ctx, tx, rootID, &snapshot); err != nil {
		return RootSnapshot{}, err
	}
	if err := readSnapshotInbox(ctx, tx, rootID, &snapshot); err != nil {
		return RootSnapshot{}, err
	}
	return snapshot, tx.Commit()
}

func readSnapshotMessages(ctx context.Context, tx *sql.Tx, rootID string, snapshot *RootSnapshot) error {
	rows, err := tx.QueryContext(ctx, `SELECT content FROM messages WHERE session_id=? ORDER BY seq`, rootID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var raw string
		var message llm.Message
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(raw), &message); err != nil {
			return err
		}
		snapshot.Messages = append(snapshot.Messages, message)
	}
	return rows.Err()
}

func readSnapshotAgents(ctx context.Context, tx *sql.Tx, rootID string, snapshot *RootSnapshot) error {
	rows, err := tx.QueryContext(ctx, `SELECT id,root_id,COALESCE(parent_id,''),status FROM agents WHERE root_id=? ORDER BY created_at,id`, rootID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var agent RuntimeAgent
		if err := rows.Scan(&agent.ID, &agent.RootID, &agent.ParentID, &agent.Status); err != nil {
			return err
		}
		snapshot.Agents = append(snapshot.Agents, agent)
	}
	return rows.Err()
}

func readSnapshotInbox(ctx context.Context, tx *sql.Tx, rootID string, snapshot *RootSnapshot) error {
	rows, err := tx.QueryContext(ctx, `SELECT seq,agent_id,kind,status,substr(payload_inline,1,?),COALESCE(payload_ref,''),
		COALESCE(r.digest,''),COALESCE(r.size,0),COALESCE(r.media_type,''),COALESCE(r.source,'')
		FROM inbox i LEFT JOIN content_references r ON r.id=i.payload_ref WHERE i.root_id=? AND i.status IN ('queued','running') ORDER BY agent_id,seq`,
		InlineValueLimit+1, rootID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		item := InboxItem{RootID: rootID}
		var referenceID string
		if err := rows.Scan(&item.Seq, &item.AgentID, &item.Kind, &item.Status, &item.Payload.Inline,
			&referenceID, &item.Payload.Digest, &item.Payload.Size, &item.Payload.MediaType, &item.Payload.Source); err != nil {
			return err
		}
		if referenceID != "" {
			item.Payload.ReferenceID = referenceID
			item.Payload.Inline = nil
		}
		snapshot.Inbox = append(snapshot.Inbox, item)
	}
	return rows.Err()
}

// ActiveRootIDs finds detached roots whose durable schedulers or
// subscriptions must be reconstructed at daemon startup.
func (s *Store) ActiveRootIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT root_id FROM (
		SELECT session_id AS root_id FROM schedules
		UNION ALL SELECT root_id FROM subscriptions WHERE status='active'
	) ORDER BY root_id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var roots []string
	for rows.Next() {
		var rootID string
		if err := rows.Scan(&rootID); err != nil {
			return nil, err
		}
		roots = append(roots, rootID)
	}
	return roots, rows.Err()
}

func (s *Store) RootCursors(ctx context.Context) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,COALESCE((SELECT MAX(seq) FROM events WHERE root_id=sessions.id),0) FROM sessions WHERE task_id='' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	cursors := make(map[string]int64)
	for rows.Next() {
		var rootID string
		var cursor int64
		if err := rows.Scan(&rootID, &cursor); err != nil {
			return nil, err
		}
		cursors[rootID] = cursor
	}
	return cursors, rows.Err()
}
