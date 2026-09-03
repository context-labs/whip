package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/capability"
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
	RootID             string
	Cursor             int64
	Meta               Meta
	Messages           []llm.Message
	Presentation       []SnapshotEvent
	AgentPresentations map[string][]SnapshotEvent
	Agents             []RuntimeAgent
	Inbox              []InboxItem
	Blackboard         []StateValue
	Budgets            []SnapshotBudget
	Capabilities       []CapabilityRecord
	Schedules          []Schedule
	Permissions        []PermissionSnapshot
}

// SnapshotEvent is presentation-only state that has been durably observed but
// not yet folded into the authoritative conversation history. It lets a
// reconnecting client rebuild an in-progress response without replaying events
// at or before the snapshot cursor.
type SnapshotEvent struct {
	Seq     int64
	Kind    string
	Payload []byte
}

type SnapshotBudget struct {
	AgentID string
	State   BudgetState
}

// PermissionSnapshot intentionally omits raw tool arguments. A reconnecting
// client gets enough immutable provenance to make a decision without copying
// credentials or other sensitive arguments into the protocol snapshot.
type PermissionSnapshot struct {
	ID                   string
	AgentID              string
	OperationID          string
	Operation            string
	CanonicalPath        string
	RequestDigest        string
	CapabilityID         string
	CapabilityGeneration int64
	Status               string
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

// AppendRootEvent appends an opaque, bounded client event. Daemon root
// actors call it after worker callbacks cross the supervisor mailbox.
func (s *Store) AppendRootEvent(ctx context.Context, rootID, kind string, payload RuntimePayload) (int64, error) {
	if rootID == "" || kind == "" {
		return 0, errors.New("root event requires root and kind")
	}
	prepared, err := s.prepareRuntimeValue(payload, ContentGrant{RootID: rootID, Scope: ContentGrantRoot})
	if err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	stamp := now()
	if err := insertRuntimeValue(ctx, tx, prepared, stamp); err != nil {
		return 0, err
	}
	var seq int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0)+1 FROM events WHERE root_id=?`, rootID).Scan(&seq); err != nil {
		return 0, err
	}
	inline, reference := runtimeValueColumns(prepared.RuntimeValue)
	if _, err := tx.ExecContext(ctx, `INSERT INTO events(root_id,seq,kind,payload_inline,payload_ref,created_at) VALUES(?,?,?,?,?,?)`,
		rootID, seq, kind, inline, reference, stamp); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM events WHERE root_id=? AND seq<=?`, rootID, seq-EventRetention); err != nil {
		return 0, err
	}
	return seq, tx.Commit()
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
	if err := tx.QueryRowContext(ctx, `SELECT id,kind,title,model,provider,cwd,goal,forked_from,fork_seq,tags,pinned,effort,
		usage_in,usage_cached,usage_out,updated_at FROM sessions WHERE id=?`, rootID).Scan(
		&snapshot.Meta.ID, &snapshot.Meta.Kind, &snapshot.Meta.Title, &snapshot.Meta.Model, &snapshot.Meta.Provider, &snapshot.Meta.CWD,
		&snapshot.Meta.Goal, &snapshot.Meta.ForkedFrom, &snapshot.Meta.ForkSeq, &tags, &pinned,
		&snapshot.Meta.Effort, &snapshot.Meta.UsageIn, &snapshot.Meta.UsageCached, &snapshot.Meta.UsageOut,
		&updated); err != nil {
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
	if err := readSnapshotBlackboard(ctx, tx, rootID, &snapshot); err != nil {
		return RootSnapshot{}, err
	}
	if err := readSnapshotBudgets(ctx, tx, rootID, &snapshot); err != nil {
		return RootSnapshot{}, err
	}
	if err := readSnapshotCapabilities(ctx, tx, rootID, &snapshot); err != nil {
		return RootSnapshot{}, err
	}
	if err := readSnapshotSchedules(ctx, tx, rootID, &snapshot); err != nil {
		return RootSnapshot{}, err
	}
	if err := s.readSnapshotPermissions(ctx, tx, rootID, &snapshot); err != nil {
		return RootSnapshot{}, err
	}
	deriveSnapshotAgentState(&snapshot)
	if err := s.readSnapshotPresentation(ctx, tx, rootID, &snapshot); err != nil {
		return RootSnapshot{}, err
	}
	return snapshot, tx.Commit()
}

func (s *Store) readSnapshotPresentation(ctx context.Context, tx *sql.Tx, rootID string, snapshot *RootSnapshot) error {
	rows, err := tx.QueryContext(ctx, `SELECT seq,kind,payload_inline,payload_ref FROM events
		WHERE root_id=? AND (kind LIKE 'stream.%' OR kind IN (
			'turn.started','turn.succeeded','turn.failed','turn.cancelled','turn.interrupted',
			'agent.turn.started','agent.turn.succeeded','agent.turn.failed','agent.turn.cancelled','agent.turn.interrupted'
		)) ORDER BY seq`, rootID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	type pendingEvent struct {
		event     SnapshotEvent
		inline    []byte
		reference sql.NullString
	}
	var pending []pendingEvent
	for rows.Next() {
		var item pendingEvent
		if err := rows.Scan(&item.event.Seq, &item.event.Kind, &item.inline, &item.reference); err != nil {
			return err
		}
		pending = append(pending, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	snapshot.AgentPresentations = make(map[string][]SnapshotEvent)
	for _, item := range pending {
		item.event.Payload, err = s.readRuntimeValueTx(ctx, tx, item.inline, item.reference)
		if err != nil {
			return err
		}
		switch item.event.Kind {
		case "turn.started":
			snapshot.Presentation = nil
		case "turn.succeeded", "turn.failed", "turn.cancelled", "turn.interrupted":
			snapshot.Presentation = nil
		case "agent.turn.started":
			var event LifecycleEvent
			if json.Unmarshal(item.event.Payload, &event) == nil && event.AgentID != "" {
				delete(snapshot.AgentPresentations, event.AgentID)
			}
		case "agent.turn.succeeded", "agent.turn.failed", "agent.turn.cancelled", "agent.turn.interrupted":
			var event LifecycleEvent
			if json.Unmarshal(item.event.Payload, &event) == nil && event.AgentID != "" {
				delete(snapshot.AgentPresentations, event.AgentID)
			}
		default:
			var owner struct {
				AgentID string `json:"agent_id"`
			}
			if json.Unmarshal(item.event.Payload, &owner) != nil || owner.AgentID == "" {
				snapshot.Presentation = append(snapshot.Presentation, item.event)
			} else {
				snapshot.AgentPresentations[owner.AgentID] = append(snapshot.AgentPresentations[owner.AgentID], item.event)
			}
		}
	}
	for _, agent := range snapshot.Agents {
		if agent.ParentID != "" && agent.LifecyclePhase != "running" {
			delete(snapshot.AgentPresentations, agent.ID)
		}
	}
	return nil
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
	rows, err := tx.QueryContext(ctx, `SELECT a.id,a.root_id,COALESCE(a.parent_id,''),a.name,a.model,a.provider,a.effort,a.cwd,a.status,
		(SELECT count(*) FROM agent_messages m WHERE m.root_id=a.root_id AND m.recipient_agent_id=a.id AND m.status='pending')
		FROM agents a WHERE a.root_id=? ORDER BY a.created_at,a.id`, rootID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var agent RuntimeAgent
		if err := rows.Scan(&agent.ID, &agent.RootID, &agent.ParentID, &agent.Name, &agent.Model, &agent.Provider, &agent.Effort, &agent.CWD, &agent.Status, &agent.PendingMail); err != nil {
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

func readSnapshotBlackboard(ctx context.Context, tx *sql.Tx, rootID string, snapshot *RootSnapshot) error {
	rows, err := tx.QueryContext(ctx, stateSelect(`FROM blackboard s LEFT JOIN content_references r ON r.id=s.payload_ref
		WHERE s.root_id=? ORDER BY s.key`), InlineValueLimit+1, rootID)
	if err != nil {
		return err
	}
	snapshot.Blackboard, err = scanStateRows(rows)
	return err
}

func readSnapshotBudgets(ctx context.Context, tx *sql.Tx, rootID string, snapshot *RootSnapshot) error {
	rows, err := tx.QueryContext(ctx, `SELECT agent_id,kind,limit_value,used_value,reserved_value
		FROM budgets WHERE root_id=? ORDER BY agent_id,kind`, rootID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var agentID string
		var row budgetRow
		if err := rows.Scan(&agentID, &row.kind, &row.limit, &row.used, &row.reserved); err != nil {
			return err
		}
		snapshot.Budgets = append(snapshot.Budgets, SnapshotBudget{AgentID: agentID, State: budgetStateFromRow(row)})
	}
	return rows.Err()
}

func readSnapshotCapabilities(ctx context.Context, tx *sql.Tx, rootID string, snapshot *RootSnapshot) error {
	rows, err := tx.QueryContext(ctx, `SELECT id,root_id,agent_id,issuer_agent_id,operations,scopes,generation,status,created_at,updated_at
		FROM capabilities WHERE root_id=? ORDER BY agent_id,id`, rootID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var record CapabilityRecord
		var operationsJSON, scopesJSON []byte
		var createdAt, updatedAt string
		if err := rows.Scan(&record.ID, &record.RootID, &record.AgentID, &record.IssuerAgentID, &operationsJSON, &scopesJSON,
			&record.Generation, &record.Status, &createdAt, &updatedAt); err != nil {
			return err
		}
		var scopes storedCapabilityScopes
		if err := json.Unmarshal(operationsJSON, &record.Operations); err != nil {
			return err
		}
		if err := json.Unmarshal(scopesJSON, &scopes); err != nil {
			return err
		}
		record.Scopes = scopes.Paths
		if scopes.ExpiresAt != "" {
			record.ExpiresAt, err = time.Parse(time.RFC3339Nano, scopes.ExpiresAt)
			if err != nil {
				return err
			}
		}
		record.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			return err
		}
		record.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
		if err != nil {
			return err
		}
		snapshot.Capabilities = append(snapshot.Capabilities, record)
	}
	return rows.Err()
}

func readSnapshotSchedules(ctx context.Context, tx *sql.Tx, rootID string, snapshot *RootSnapshot) error {
	rows, err := tx.QueryContext(ctx, `SELECT id,schedule,prompt,anchor,last_fire FROM schedules WHERE session_id=? ORDER BY id`, rootID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var schedule Schedule
		var anchor, lastFire string
		if err := rows.Scan(&schedule.ID, &schedule.Schedule, &schedule.Prompt, &anchor, &lastFire); err != nil {
			return err
		}
		schedule.Anchor, err = time.Parse(time.RFC3339, anchor)
		if err != nil {
			return err
		}
		if lastFire != "" {
			schedule.LastFire, err = time.Parse(time.RFC3339, lastFire)
			if err != nil {
				return err
			}
		}
		snapshot.Schedules = append(snapshot.Schedules, schedule)
	}
	return rows.Err()
}

func (s *Store) readSnapshotPermissions(ctx context.Context, tx *sql.Tx, rootID string, snapshot *RootSnapshot) error {
	rows, err := tx.QueryContext(ctx, `SELECT p.id,p.agent_id,p.operation_id,p.status,o.payload_inline,o.payload_ref
		FROM permission_requests p JOIN operations o ON o.root_id=p.root_id AND o.id=p.operation_id
		WHERE p.root_id=? AND p.status='pending' ORDER BY p.created_at,p.id`, rootID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	type pendingPermission struct {
		permission PermissionSnapshot
		inline     []byte
		reference  sql.NullString
	}
	var pending []pendingPermission
	for rows.Next() {
		var item pendingPermission
		if err := rows.Scan(&item.permission.ID, &item.permission.AgentID, &item.permission.OperationID,
			&item.permission.Status, &item.inline, &item.reference); err != nil {
			return err
		}
		pending = append(pending, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range pending {
		payload, err := s.readRuntimeValueTx(ctx, tx, item.inline, item.reference)
		if err != nil {
			return err
		}
		var admission capability.Admission
		if err := json.Unmarshal(payload, &admission); err != nil {
			return err
		}
		item.permission.Operation = admission.Request.Operation
		item.permission.CanonicalPath = admission.CanonicalPath
		item.permission.RequestDigest = admission.RequestDigest
		item.permission.CapabilityID = admission.Request.CapabilityID
		item.permission.CapabilityGeneration = admission.Request.CapabilityGeneration
		snapshot.Permissions = append(snapshot.Permissions, item.permission)
	}
	return nil
}

func deriveSnapshotAgentState(snapshot *RootSnapshot) {
	blocked := make(map[string]string)
	active := make(map[string]bool)
	for _, permission := range snapshot.Permissions {
		blocked[permission.AgentID] = "permission"
	}
	// Only the root's own inbox rows mean live work for the root actor; a
	// child is running iff its agents row says so.
	for _, item := range snapshot.Inbox {
		if item.Status == "running" {
			active[item.AgentID] = true
		}
	}
	for i := range snapshot.Agents {
		agent := &snapshot.Agents[i]
		switch {
		case isTerminalAgentStatus(agent.Status):
			agent.LifecyclePhase = "terminal"
			agent.TerminalCause = agent.Status
			if agent.ParentID != "" && agent.Status != "deleted" {
				agent.AllowedControls = []string{"agent.delete"}
			}
		case blocked[agent.ID] != "":
			agent.LifecyclePhase = "blocked"
			agent.BlockingReason = blocked[agent.ID]
		case agent.Status == "running" || active[agent.ID]:
			agent.LifecyclePhase = "running"
		default:
			agent.LifecyclePhase = "idle"
		}
		if agent.LifecyclePhase == "terminal" {
			continue
		}
		if agent.ParentID == "" {
			agent.AllowedControls = []string{"budget.cap", "capability.revoke"}
			if active[agent.ID] {
				agent.AllowedControls = append([]string{"cancel"}, agent.AllowedControls...)
			}
		} else {
			agent.AllowedControls = []string{"agent.stop", "budget.cap", "capability.revoke"}
		}
	}
}

// ActiveRootIDs finds detached roots whose durable schedulers,
// subscriptions, or recursive-agent notifications need reconstruction.
func (s *Store) ActiveRootIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT root_id FROM (
		SELECT session_id AS root_id FROM schedules
		UNION ALL SELECT root_id FROM subscriptions WHERE status='active'
		UNION ALL SELECT root_id FROM agent_messages WHERE status='pending'
			AND recipient_agent_id IN (SELECT id FROM agents WHERE parent_id IS NOT NULL)
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
	rows, err := s.db.QueryContext(ctx, `SELECT id,COALESCE((SELECT MAX(seq) FROM events WHERE root_id=sessions.id),0) FROM sessions ORDER BY id`)
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
