package session

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/capability"
	contentstore "github.com/context-labs/whip/internal/content"
	"github.com/context-labs/whip/internal/llm"
	schedulepkg "github.com/context-labs/whip/internal/schedule"
)

const (
	InlineValueLimit = 8 << 10
	MaxContentRead   = contentstore.MaxReadSize
	MaxInboxBatch    = 256
)

var (
	ErrContentAccess       = errors.New("content reference is not authorized")
	ErrInboxTerminal       = errors.New("inbox item is terminal")
	ErrScheduleClaimed     = errors.New("schedule fire was already claimed")
	ErrInvalidScheduleSlot = errors.New("schedule fire is not on the next grid slot")
	ErrRootTerminal        = errors.New("root agent is terminal")
)

type CommandScope string

const (
	CommandScopeDaemon CommandScope = "daemon"
	CommandScopeRoot   CommandScope = "root"
)

type ContentGrantScope string

const (
	ContentGrantRoot    ContentGrantScope = "root"
	ContentGrantAgent   ContentGrantScope = "agent"
	ContentGrantSubtree ContentGrantScope = "subtree"
)

type RuntimePayload struct {
	Data      []byte
	MediaType string
	Source    string
}

type RuntimeValue struct {
	Inline      []byte
	ReferenceID string
	Digest      string
	Size        int64
	MediaType   string
	Source      string
}

type ContentMetadata struct {
	ReferenceID string
	Digest      string
	Size        int64
	MediaType   string
	Source      string
}

type ContentGrant struct {
	RootID  string
	AgentID string
	Scope   ContentGrantScope
}

type RuntimeAgent struct {
	ID              string
	RootID          string
	ParentID        string
	Name            string
	Model           string
	Provider        string
	Effort          string
	CWD             string
	Status          string
	PendingMail     int
	LifecyclePhase  string
	BlockingReason  string
	TerminalCause   string
	AllowedControls []string
}

type RuntimeCommand struct {
	ClientID      string
	ID            string
	Scope         CommandScope
	RootID        string
	RequestDigest string
	Status        string
	Payload       RuntimePayload
}

type RuntimeInbox struct {
	RootID  string
	AgentID string
	Seq     int64
	Kind    string
	Status  string
	Payload RuntimePayload
}

type InboxEnqueue struct {
	RootID          string
	AgentID         string
	Kind            string
	CommandClientID string
	CommandID       string
	OperationID     string
	TraceID         string
	Payload         RuntimePayload
}

type InboxSequence struct {
	InboxSeq int64
	EventSeq int64
}

type InboxItem struct {
	RootID  string
	AgentID string
	Seq     int64
	Kind    string
	Status  string
	Payload RuntimeValue
}

type ScheduleFireClaim struct {
	RootID           string
	AgentID          string
	ScheduleID       int
	ExpectedLastFire time.Time
	Slot             time.Time
	CommandClientID  string
	CommandID        string
	OperationID      string
	TraceID          string
}

// LifecycleEvent is the stable payload shared by durable actor events. Clients
// can reduce ordinary lifecycle changes without replacing their whole root
// snapshot after every event.
type LifecycleEvent struct {
	RootID          string  `json:"root_id,omitempty"`
	AgentID         string  `json:"agent_id,omitempty"`
	SenderAgentID   string  `json:"sender_agent_id,omitempty"`
	InboxSeq        int64   `json:"inbox_seq,omitempty"`
	InboxKind       string  `json:"inbox_kind,omitempty"`
	Delivery        string  `json:"delivery,omitempty"`
	MessageID       string  `json:"message_id,omitempty"`
	Phase           string  `json:"phase,omitempty"`
	Status          string  `json:"status,omitempty"`
	TerminalCause   string  `json:"terminal_cause,omitempty"`
	CommandClientID string  `json:"command_client_id,omitempty"`
	CommandID       string  `json:"command_id,omitempty"`
	OperationID     string  `json:"operation_id,omitempty"`
	TraceID         string  `json:"trace_id,omitempty"`
	ScheduleID      int     `json:"schedule_id,omitempty"`
	Slot            string  `json:"slot,omitempty"`
	Error           string  `json:"error,omitempty"`
	Acknowledged    []int64 `json:"acknowledged_inbox,omitempty"`
	SubscriptionID  string  `json:"subscription_id,omitempty"`
	Key             string  `json:"key,omitempty"`
	Version         int64   `json:"version,omitempty"`
	ExpectedVersion int64   `json:"expected_version,omitempty"`
	// Scratch restore outcome (kind scratch.restored).
	Restored      []string      `json:"restored,omitempty"`
	NotRestored   []ScratchSkip `json:"not_restored,omitempty"`
	Attempt       string        `json:"attempt,omitempty"`
	BudgetKind    string        `json:"budget_kind,omitempty"`
	Amount        int64         `json:"amount,omitempty"`
	Limit         int64         `json:"limit,omitempty"`
	Used          int64         `json:"used,omitempty"`
	Reserved      int64         `json:"reserved,omitempty"`
	CapabilityID  string        `json:"capability_id,omitempty"`
	Generation    int64         `json:"generation,omitempty"`
	PermissionID  string        `json:"permission_id,omitempty"`
	Operation     string        `json:"operation,omitempty"`
	CanonicalPath string        `json:"canonical_path,omitempty"`
	RequestDigest string        `json:"request_digest,omitempty"`
	// Permission prompts: what the human sees and the rule "always" installs.
	Command    string `json:"command,omitempty"`
	Rule       string `json:"rule,omitempty"`
	RuleSource string `json:"rule_source,omitempty"`
}

type actorEvent = LifecycleEvent

type RootTurnCommit struct {
	RootID  string
	AgentID string
	// InboxSeq identifies an inbox-triggered turn; a mailbox-triggered turn
	// has InboxSeq 0 and identifies itself with TurnID.
	InboxSeq          int64
	TurnID            string
	AcknowledgedInbox []int64
	DeliveredMessages []string
	Messages          []llm.Message
	Compactions       []RootCompaction
	WorkspaceSeq      int
	WorkspaceRef      string
	ClearGoal         bool
	GoalContinuation  string
	Model             string
	Provider          string
	Status            string
	Error             string
	Outcome           RuntimePayload
}

type RootCompaction struct {
	Summary      string
	Cutoff       int
	RawTailStart int
}

type RuntimeState struct {
	RootID        string
	AgentID       string
	Key           string
	Version       int64
	AuthorAgentID string
	Payload       RuntimePayload
}

type RuntimeEvent struct {
	RootID  string
	Seq     int64
	Kind    string
	Payload RuntimePayload
}

type RuntimeUsage struct {
	ID              string
	RootID          string
	AgentID         string
	CommandClientID string
	CommandID       string
	InputTokens     int64
	CachedTokens    int64
	OutputTokens    int64
	CostMicros      int64
}

type RuntimeTransition struct {
	Agent   *RuntimeAgent
	Command *RuntimeCommand
	Inbox   *RuntimeInbox
	State   *RuntimeState
	Event   *RuntimeEvent
	Usage   *RuntimeUsage
}

type RuntimeResult struct {
	Command RuntimeValue
	Inbox   RuntimeValue
	State   RuntimeValue
	Event   RuntimeValue
}

type preparedRuntimeValue struct {
	RuntimeValue
	grant ContentGrant
}

func (s *Store) EnqueueInbox(ctx context.Context, item InboxEnqueue) (InboxSequence, error) {
	if err := validateInboxEnqueue(item); err != nil {
		return InboxSequence{}, err
	}
	prepared, err := s.prepareRuntimeValue(item.Payload, ContentGrant{RootID: item.RootID, AgentID: item.AgentID, Scope: ContentGrantAgent})
	if err != nil {
		return InboxSequence{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return InboxSequence{}, err
	}
	defer func() { _ = tx.Rollback() }()
	sequence, err := s.enqueueInboxTx(ctx, tx, item, prepared, "inbox.queued", actorEvent{})
	if err != nil {
		return InboxSequence{}, err
	}
	if err := tx.Commit(); err != nil {
		return InboxSequence{}, err
	}
	return sequence, nil
}

func (s *Store) LoadQueuedInbox(ctx context.Context, rootID, agentID string, afterSeq int64, limit int) ([]InboxItem, error) {
	if rootID == "" || agentID == "" || afterSeq < 0 || limit < 1 || limit > MaxInboxBatch {
		return nil, fmt.Errorf("queued inbox load requires a root, agent, nonnegative cursor, and limit from 1 to %d", MaxInboxBatch)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT i.seq,i.kind,i.status,substr(i.payload_inline,1,?),COALESCE(i.payload_ref,''),
		COALESCE(r.digest,''),COALESCE(r.size,0),COALESCE(r.media_type,''),COALESCE(r.source,'')
		FROM inbox i LEFT JOIN content_references r ON r.id=i.payload_ref
		WHERE i.root_id=? AND i.agent_id=? AND i.status='queued' AND i.seq>? ORDER BY i.seq LIMIT ?`, InlineValueLimit+1, rootID, agentID, afterSeq, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanInboxRows(rows, rootID, agentID)
}

func scanInboxRows(rows *sql.Rows, rootID, agentID string) ([]InboxItem, error) {
	var items []InboxItem
	for rows.Next() {
		item := InboxItem{RootID: rootID, AgentID: agentID}
		var referenceID string
		if err := rows.Scan(&item.Seq, &item.Kind, &item.Status, &item.Payload.Inline, &referenceID,
			&item.Payload.Digest, &item.Payload.Size, &item.Payload.MediaType, &item.Payload.Source); err != nil {
			return nil, err
		}
		if referenceID == "" {
			if len(item.Payload.Inline) > InlineValueLimit {
				return nil, fmt.Errorf("inbox item %d has an oversized inline payload", item.Seq)
			}
			item.Payload.Size = int64(len(item.Payload.Inline))
		} else {
			item.Payload.ReferenceID = referenceID
			item.Payload.Inline = nil
			if item.Payload.Digest == "" {
				return nil, fmt.Errorf("inbox item %d references missing content", item.Seq)
			}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ConsumeInbox(ctx context.Context, rootID, agentID string, seq int64) (int64, error) {
	if rootID == "" || agentID == "" || seq < 1 {
		return 0, errors.New("inbox consume requires a root, agent, and positive sequence")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	eventSeq, err := s.consumeInboxTx(ctx, tx, rootID, agentID, seq, now())
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return eventSeq, nil
}

func (s *Store) consumeInboxTx(ctx context.Context, tx *sql.Tx, rootID, agentID string, seq int64, stamp string) (int64, error) {
	result, err := tx.ExecContext(ctx, `UPDATE inbox SET status='consumed' WHERE root_id=? AND agent_id=? AND seq=? AND status IN ('queued','running')`, rootID, agentID, seq)
	if err != nil {
		return 0, err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		if err != nil {
			return 0, err
		}
		return 0, ErrInboxTerminal
	}
	return s.insertActorEventTx(ctx, tx, rootID, "inbox.consumed", actorEvent{AgentID: agentID, InboxSeq: seq, Status: "consumed"}, stamp)
}

func rootTurnID(agentID string, inboxSeq int64) string {
	return fmt.Sprintf("%s:turn:%d", agentID, inboxSeq)
}

func (s *Store) StartRootTurn(ctx context.Context, rootID, agentID string, inboxSeq int64) error {
	if rootID == "" || agentID == "" || inboxSeq < 1 {
		return errors.New("root turn start requires a root, agent, and positive inbox sequence")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stamp := now()
	result, err := tx.ExecContext(ctx, `UPDATE inbox SET status='running' WHERE root_id=? AND agent_id=? AND seq=? AND status='queued'`, rootID, agentID, inboxSeq)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		if err != nil {
			return err
		}
		return ErrInboxTerminal
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO turns(id,root_id,agent_id,status,created_at,updated_at) VALUES(?,?,?,'running',?,?)`,
		rootTurnID(agentID, inboxSeq), rootID, agentID, stamp, stamp); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE commands SET status='running',updated_at=? WHERE root_id=? AND ingress_seq=? AND status='queued'`, stamp, rootID, inboxSeq); err != nil {
		return err
	}
	if _, err := s.insertActorEventTx(ctx, tx, rootID, "turn.started", actorEvent{
		AgentID: agentID, InboxSeq: inboxSeq, Phase: "running", Status: "running",
	}, stamp); err != nil {
		return err
	}
	return tx.Commit()
}

// StartRootMailboxTurn records a root turn triggered by ready mail rather
// than by a queued inbox row; nothing is claimed, the digest is the input.
func (s *Store) StartRootMailboxTurn(ctx context.Context, rootID, agentID string) (string, error) {
	if rootID == "" || agentID == "" {
		return "", errors.New("root mailbox turn requires a root and agent")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	stamp := now()
	turnID := fmt.Sprintf("%s:mail:%d", agentID, time.Now().UnixNano())
	if _, err := tx.ExecContext(ctx, `INSERT INTO turns(id,root_id,agent_id,status,trigger,created_at,updated_at) VALUES(?,?,?,'running','mailbox',?,?)`,
		turnID, rootID, agentID, stamp, stamp); err != nil {
		return "", err
	}
	if _, err := s.insertActorEventTx(ctx, tx, rootID, "turn.started", actorEvent{
		AgentID: agentID, InboxKind: "mailbox", Phase: "running", Status: "running",
	}, stamp); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return turnID, nil
}

func (s *Store) CommitRootTurn(ctx context.Context, commit RootTurnCommit) error {
	return s.commitRootTurn(ctx, commit, nil)
}

func (s *Store) commitRootTurn(ctx context.Context, commit RootTurnCommit, beforeCommit func() error) error {
	if commit.RootID == "" || commit.AgentID == "" || (commit.InboxSeq < 1 && commit.TurnID == "") {
		return errors.New("root turn commit requires a root, agent, and an inbox sequence or turn id")
	}
	turnID := commit.TurnID
	if turnID == "" {
		turnID = rootTurnID(commit.AgentID, commit.InboxSeq)
	}
	seen := map[int64]bool{}
	if commit.InboxSeq > 0 {
		seen[commit.InboxSeq] = true
	}
	for _, seq := range commit.AcknowledgedInbox {
		if seq < 1 || seen[seq] {
			return errors.New("root turn acknowledged inbox sequences must be positive and unique")
		}
		seen[seq] = true
	}
	if commit.ClearGoal && commit.GoalContinuation != "" {
		return errors.New("root turn cannot clear and continue a goal")
	}
	if commit.WorkspaceRef != "" && commit.WorkspaceSeq < 1 {
		return errors.New("root turn workspace snapshot requires a positive conversation index")
	}
	var outcomeCommandCount int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM commands
		WHERE root_id=? AND scope='root' AND ingress_seq=? AND ingress_seq>0`, commit.RootID, commit.InboxSeq).Scan(&outcomeCommandCount); err != nil {
		return err
	}
	if outcomeCommandCount > 1 {
		return errors.New("root turn inbox has ambiguous commands")
	}
	var commandOutcome preparedRuntimeValue
	if outcomeCommandCount == 1 {
		var err error
		commandOutcome, err = s.prepareRuntimeValue(commit.Outcome, ContentGrant{RootID: commit.RootID, Scope: ContentGrantRoot})
		if err != nil {
			return err
		}
	}
	var goalValue preparedRuntimeValue
	if commit.GoalContinuation != "" {
		var err error
		goalValue, err = s.prepareRuntimeValue(RuntimePayload{Data: []byte(commit.GoalContinuation), MediaType: "text/plain", Source: "goal continuation"}, ContentGrant{
			RootID: commit.RootID, AgentID: commit.AgentID, Scope: ContentGrantAgent,
		})
		if err != nil {
			return err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stamp := now()
	status := commit.Status
	if status == "" {
		status = "succeeded"
		if commit.Error != "" {
			status = "failed"
		}
	}
	if status != "succeeded" && status != "failed" && status != "cancelled" && status != "interrupted" {
		return fmt.Errorf("invalid root turn status %q", status)
	}
	eventKind := "turn." + status
	result, err := tx.ExecContext(ctx, `UPDATE turns SET status=?,updated_at=? WHERE id=? AND root_id=? AND agent_id=? AND status='running'`,
		status, stamp, turnID, commit.RootID, commit.AgentID)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		if err != nil {
			return err
		}
		return errors.New("root turn is not running")
	}
	for seq := range seen {
		result, err := tx.ExecContext(ctx, `UPDATE inbox SET status='consumed' WHERE root_id=? AND agent_id=? AND seq=? AND status IN ('queued','running')`,
			commit.RootID, commit.AgentID, seq)
		if err != nil {
			return err
		}
		if changed, err := result.RowsAffected(); err != nil || changed != 1 {
			if err != nil {
				return err
			}
			return ErrInboxTerminal
		}
	}
	if err := insertRuntimeValue(ctx, tx, commandOutcome, stamp); err != nil {
		return err
	}
	for seq := range seen {
		outcome := preparedRuntimeValue{}
		if seq == commit.InboxSeq {
			outcome = commandOutcome
		}
		var commandCount int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM commands
			WHERE root_id=? AND scope='root' AND ingress_seq=? AND ingress_seq>0`, commit.RootID, seq).Scan(&commandCount); err != nil {
			return err
		}
		if commandCount == 0 {
			continue // internal inbox items do not have an originating client command
		}
		if commandCount != 1 {
			return errors.New("root turn inbox has ambiguous commands")
		}
		inline, reference := runtimeValueColumns(outcome.RuntimeValue)
		result, err := tx.ExecContext(ctx, `UPDATE commands SET status=?,outcome_inline=?,outcome_ref=?,updated_at=?
			WHERE root_id=? AND scope='root' AND ingress_seq=? AND ingress_seq>0 AND status IN ('queued','running','waiting')`,
			status, inline, reference, stamp, commit.RootID, seq)
		if err != nil {
			return err
		}
		if changed, err := result.RowsAffected(); err != nil || changed != 1 {
			if err != nil {
				return err
			}
			return errors.New("root turn command is not running")
		}
	}
	var messageSeq int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM messages WHERE session_id=?`, commit.RootID).Scan(&messageSeq); err != nil {
		return err
	}
	for _, message := range commit.Messages {
		if message.Role == "" || message.Role == "system" {
			continue
		}
		messageSeq++
		data, err := json.Marshal(message)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO messages(session_id,seq,role,content) VALUES(?,?,?,?)`, commit.RootID, messageSeq, message.Role, string(data)); err != nil {
			return err
		}
	}
	var compactionSeq, rawCutoff int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM compactions WHERE session_id=?`, commit.RootID).Scan(&compactionSeq); err != nil {
		return err
	}
	if compactionSeq > 0 {
		if err := tx.QueryRowContext(ctx, `SELECT cutoff FROM compactions WHERE session_id=? AND seq=?`, commit.RootID, compactionSeq).Scan(&rawCutoff); err != nil {
			return err
		}
	}
	for _, compaction := range commit.Compactions {
		if compaction.Cutoff < 1 || compaction.Summary == "" {
			return errors.New("root turn compaction requires a cutoff and summary")
		}
		if compactionSeq > 0 {
			tailStart := compaction.RawTailStart
			if tailStart < 1 {
				tailStart = 2
			}
			rawCutoff += compaction.Cutoff - tailStart
		} else {
			rawCutoff = compaction.Cutoff
			var firstRole string
			if err := tx.QueryRowContext(ctx, `SELECT role FROM messages WHERE session_id=? ORDER BY seq LIMIT 1`, commit.RootID).Scan(&firstRole); err != nil {
				return err
			}
			if firstRole != "system" {
				rawCutoff--
			}
		}
		compactionSeq++
		if _, err := tx.ExecContext(ctx, `INSERT INTO compactions(session_id,seq,cutoff,summary,created_at) VALUES(?,?,?,?,?)`,
			commit.RootID, compactionSeq, rawCutoff, compaction.Summary, stamp); err != nil {
			return err
		}
	}
	if commit.WorkspaceRef != "" {
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO snapshots(session_id,seq,ref,created_at) VALUES(?,?,?,?)`,
			commit.RootID, commit.WorkspaceSeq, commit.WorkspaceRef, stamp); err != nil {
			return err
		}
	}
	title := ""
	for _, message := range commit.Messages {
		if message.Role == "user" {
			title = truncate(strings.Join(strings.Fields(message.TextContent()), " "), 64)
			break
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET updated_at=?,model=?,provider=?,title=CASE WHEN title='' THEN ? ELSE title END WHERE id=?`,
		stamp, commit.Model, commit.Provider, title, commit.RootID); err != nil {
		return err
	}
	if status == "succeeded" {
		if err := s.markMessagesDeliveredTx(ctx, tx, commit.RootID, commit.AgentID, turnID, commit.DeliveredMessages, stamp); err != nil {
			return err
		}
	}
	if commit.ClearGoal {
		if _, err := tx.ExecContext(ctx, `UPDATE sessions SET goal='' WHERE id=?`, commit.RootID); err != nil {
			return err
		}
	} else if commit.GoalContinuation != "" {
		if _, err := s.enqueueInboxTx(ctx, tx, InboxEnqueue{
			RootID: commit.RootID, AgentID: commit.AgentID, Kind: "goal",
			Payload: RuntimePayload{Data: []byte(commit.GoalContinuation), MediaType: "text/plain", Source: "goal continuation"},
		}, goalValue, "goal.continued", actorEvent{AgentID: commit.AgentID, Status: "queued"}); err != nil {
			return err
		}
	}
	acknowledged := append([]int64(nil), commit.AcknowledgedInbox...)
	if commit.InboxSeq > 0 {
		acknowledged = append([]int64{commit.InboxSeq}, acknowledged...)
	}
	if _, err := s.insertActorEventTx(ctx, tx, commit.RootID, eventKind, actorEvent{
		AgentID: commit.AgentID, InboxSeq: commit.InboxSeq, Phase: "idle", Status: status,
		TerminalCause: status, Error: commit.Error, Acknowledged: acknowledged,
	}, stamp); err != nil {
		return err
	}
	if beforeCommit != nil {
		if err := beforeCommit(); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ClaimScheduleFire(ctx context.Context, claim ScheduleFireClaim) (InboxSequence, error) {
	if claim.RootID == "" || claim.AgentID == "" || claim.ScheduleID < 1 || claim.Slot.IsZero() ||
		(claim.CommandClientID == "") != (claim.CommandID == "") {
		return InboxSequence{}, errors.New("schedule claim requires root, agent, schedule, slot, and complete command identity")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return InboxSequence{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var expression, prompt, anchorText, lastFireText string
	if err := tx.QueryRowContext(ctx, `SELECT schedule,prompt,anchor,last_fire FROM schedules WHERE session_id=? AND id=?`, claim.RootID, claim.ScheduleID).
		Scan(&expression, &prompt, &anchorText, &lastFireText); err != nil {
		return InboxSequence{}, err
	}
	anchor, err := time.Parse(time.RFC3339, anchorText)
	if err != nil {
		return InboxSequence{}, err
	}
	var lastFire time.Time
	if lastFireText != "" {
		lastFire, err = time.Parse(time.RFC3339, lastFireText)
		if err != nil {
			return InboxSequence{}, err
		}
	}
	if !lastFire.Equal(claim.ExpectedLastFire) {
		return InboxSequence{}, ErrScheduleClaimed
	}
	parsed, err := schedulepkg.Parse(expression)
	if err != nil {
		return InboxSequence{}, err
	}
	var expected time.Time
	if parsed.Every > 0 {
		if lastFire.IsZero() {
			expected = anchor
		} else {
			expected, _ = parsed.NextAfter(anchor, lastFire)
		}
	} else {
		if !lastFire.IsZero() {
			return InboxSequence{}, ErrScheduleClaimed
		}
		expected = parsed.At
	}
	if !claim.Slot.UTC().Equal(expected.UTC()) {
		return InboxSequence{}, ErrInvalidScheduleSlot
	}
	result, err := tx.ExecContext(ctx, `UPDATE schedules SET last_fire=? WHERE session_id=? AND id=? AND last_fire=?`,
		claim.Slot.UTC().Format(time.RFC3339), claim.RootID, claim.ScheduleID, lastFireText)
	if err != nil {
		return InboxSequence{}, err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		if err != nil {
			return InboxSequence{}, err
		}
		return InboxSequence{}, ErrScheduleClaimed
	}
	item := InboxEnqueue{
		RootID: claim.RootID, AgentID: claim.AgentID, Kind: "schedule", CommandClientID: claim.CommandClientID,
		CommandID: claim.CommandID, OperationID: claim.OperationID, TraceID: claim.TraceID,
		Payload: RuntimePayload{Data: []byte(prompt), MediaType: "text/plain", Source: "schedule prompt"},
	}
	prepared, err := s.prepareRuntimeValue(item.Payload, ContentGrant{RootID: item.RootID, AgentID: item.AgentID, Scope: ContentGrantAgent})
	if err != nil {
		return InboxSequence{}, err
	}
	sequence, err := s.enqueueInboxTx(ctx, tx, item, prepared, "schedule.fired", actorEvent{
		ScheduleID: claim.ScheduleID, Slot: claim.Slot.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return InboxSequence{}, err
	}
	if err := tx.Commit(); err != nil {
		return InboxSequence{}, err
	}
	return sequence, nil
}

func (s *Store) FailRoot(ctx context.Context, rootID, reason string) (int64, error) {
	if rootID == "" {
		return 0, errors.New("root failure requires a root")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	stamp := now()
	agentID, err := rootAgentIDTx(ctx, tx, rootID)
	if err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE agents SET status='failed',updated_at=? WHERE root_id=? AND id=?
		AND status NOT IN ('failed','stopped','cancelled','interrupted','deleted','succeeded')`, stamp, rootID, agentID)
	if err != nil {
		return 0, err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		if err != nil {
			return 0, err
		}
		return 0, ErrRootTerminal
	}
	if err := s.interruptRootTx(ctx, tx, rootID, reason, stamp, false); err != nil {
		return 0, err
	}
	eventSeq, err := s.insertActorEventTx(ctx, tx, rootID, "root.failed", actorEvent{AgentID: agentID, Status: "failed", Error: reason}, stamp)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return eventSeq, nil
}

func (s *Store) InterruptRoot(ctx context.Context, rootID, reason string) (int64, error) {
	if rootID == "" {
		return 0, errors.New("root interruption requires a root")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	stamp := now()
	if err := s.interruptRootTx(ctx, tx, rootID, reason, stamp, true); err != nil {
		return 0, err
	}
	agentID, err := rootAgentIDTx(ctx, tx, rootID)
	if err != nil {
		return 0, err
	}
	eventSeq, err := s.insertActorEventTx(ctx, tx, rootID, "root.interrupted", actorEvent{AgentID: agentID, Status: "interrupted", Error: reason}, stamp)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return eventSeq, nil
}

func (s *Store) StopRoot(ctx context.Context, rootID, reason string) (int64, error) {
	if rootID == "" {
		return 0, errors.New("root stop requires a root")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	stamp := now()
	agentID, err := rootAgentIDTx(ctx, tx, rootID)
	if err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE agents SET status='stopped',updated_at=? WHERE root_id=? AND id=?
		AND status NOT IN ('failed','stopped','cancelled','interrupted','deleted','succeeded')`, stamp, rootID, agentID)
	if err != nil {
		return 0, err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		if err != nil {
			return 0, err
		}
		return 0, ErrRootTerminal
	}
	if err := s.interruptRootTx(ctx, tx, rootID, reason, stamp, false); err != nil {
		return 0, err
	}
	eventSeq, err := s.insertActorEventTx(ctx, tx, rootID, "root.stopped", actorEvent{AgentID: agentID, Status: "stopped", Error: reason}, stamp)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return eventSeq, nil
}

func rootAgentIDTx(ctx context.Context, tx *sql.Tx, rootID string) (string, error) {
	var agentID string
	err := tx.QueryRowContext(ctx, `SELECT id FROM agents WHERE root_id=? AND parent_id IS NULL ORDER BY created_at LIMIT 1`, rootID).Scan(&agentID)
	return agentID, err
}

func (s *Store) interruptRootTx(ctx context.Context, tx *sql.Tx, rootID, reason, stamp string, preserveSchedules bool) error {
	if err := s.cancelPendingPermissionsTx(ctx, tx, rootID, "", "", "interrupted", "", reason); err != nil {
		return err
	}
	if err := s.settleInterruptedOperationReservations(ctx, tx, rootID, ""); err != nil {
		return err
	}
	if err := s.emitInterruptedTurnEventsTx(ctx, tx, rootID, reason, stamp); err != nil {
		return err
	}
	for _, table := range []string{"commands", "turns", "operations", "leases"} {
		if _, err := tx.ExecContext(ctx, `UPDATE `+table+` SET status='interrupted',updated_at=? WHERE root_id=? AND status IN ('queued','running','waiting')`, stamp, rootID); err != nil { //nolint:gosec // table comes from the static allowlist above
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agents SET status='idle',updated_at=?
		WHERE parent_id IS NOT NULL AND status='running'`, stamp); err != nil {
		return err
	}
	if err := syncChildBudgetReservationsTx(ctx, tx, rootID); err != nil {
		return err
	}
	inboxWhere := `status IN ('queued','running')`
	if preserveSchedules {
		inboxWhere = `status='running'`
	}
	if _, err := tx.ExecContext(ctx, `UPDATE inbox SET status='interrupted' WHERE root_id=? AND (`+inboxWhere+`)`, rootID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE permission_requests SET status='interrupted',updated_at=? WHERE root_id=? AND status='pending'`, stamp, rootID); err != nil {
		return err
	}
	return nil
}

// emitInterruptedTurnEventsTx records a terminal turn event for every running
// turn about to be marked interrupted, so reconnecting clients close the
// in-progress presentation instead of showing a phantom turn. An empty rootID
// covers every root (daemon restart).
func (s *Store) emitInterruptedTurnEventsTx(ctx context.Context, tx *sql.Tx, rootID, reason, stamp string) error {
	query := `SELECT t.root_id,t.agent_id,COALESCE(a.parent_id,'') FROM turns t
		JOIN agents a ON a.root_id=t.root_id AND a.id=t.agent_id WHERE t.status='running'`
	args := []any{}
	if rootID != "" {
		query += ` AND t.root_id=?`
		args = append(args, rootID)
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	type running struct{ root, agent, parent string }
	var turns []running
	for rows.Next() {
		var value running
		if err := rows.Scan(&value.root, &value.agent, &value.parent); err != nil {
			_ = rows.Close() //nolint:sqlclosecheck // rows must close before the tx runs more statements
			return err
		}
		turns = append(turns, value)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, turn := range turns {
		kind := "turn.interrupted"
		if turn.parent != "" {
			kind = "agent.turn.interrupted"
		}
		if _, err := s.insertActorEventTx(ctx, tx, turn.root, kind, actorEvent{
			AgentID: turn.agent, Phase: "idle", Status: "interrupted", TerminalCause: "interrupted", Error: reason,
		}, stamp); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) settleInterruptedOperationReservations(ctx context.Context, tx *sql.Tx, rootID, targetAgentID string) error {
	query := `SELECT root_id,payload_inline,payload_ref FROM operations WHERE status IN ('queued','running','waiting')`
	var args []any
	if targetAgentID != "" {
		query = subtreeCTE + `SELECT o.root_id,o.payload_inline,o.payload_ref FROM operations o
			WHERE o.root_id=? AND o.agent_id IN (SELECT id FROM subtree) AND o.status IN ('queued','running','waiting')`
		args = []any{rootID, targetAgentID, rootID, rootID}
	} else if rootID != "" {
		query += ` AND root_id=?`
		args = append(args, rootID)
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	type reserved struct {
		rootID    string
		admission capability.Admission
	}
	var admissions []reserved
	for rows.Next() {
		var storedRoot string
		var inline []byte
		var reference sql.NullString
		if err := rows.Scan(&storedRoot, &inline, &reference); err != nil {
			return err
		}
		if len(inline) == 0 && !reference.Valid {
			continue
		}
		payload, err := s.readRuntimeValueTx(ctx, tx, inline, reference)
		if err != nil {
			return err
		}
		var admission capability.Admission
		if err := json.Unmarshal(payload, &admission); err != nil {
			return err
		}
		if admission.Request.RootID != storedRoot {
			return errors.New("operation admission root does not match stored root")
		}
		admissions = append(admissions, reserved{rootID: storedRoot, admission: admission})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, reserved := range admissions {
		if err := settleCapabilityBudgets(ctx, tx, reserved.rootID, reserved.admission.Request.AgentID, reserved.admission.Request.Reservations, nil); err != nil {
			return err
		}
	}
	return nil
}

func validateInboxEnqueue(item InboxEnqueue) error {
	if item.RootID == "" || item.AgentID == "" || item.Kind == "" || (item.CommandClientID == "") != (item.CommandID == "") {
		return errors.New("inbox enqueue requires root, agent, kind, and complete command identity")
	}
	return nil
}

func (s *Store) enqueueInboxTx(ctx context.Context, tx *sql.Tx, item InboxEnqueue, prepared preparedRuntimeValue, eventKind string, event actorEvent) (InboxSequence, error) {
	if err := validateInboxEnqueue(item); err != nil {
		return InboxSequence{}, err
	}
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM agents WHERE root_id=? AND id=?`, item.RootID, item.AgentID).Scan(&status); err != nil {
		return InboxSequence{}, err
	}
	if status == "failed" || status == "stopped" || status == "cancelled" || status == "interrupted" || status == "deleted" || status == "succeeded" {
		return InboxSequence{}, ErrRootTerminal
	}
	var sequence InboxSequence
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0)+1 FROM inbox WHERE root_id=? AND agent_id=?`, item.RootID, item.AgentID).Scan(&sequence.InboxSeq); err != nil {
		return InboxSequence{}, err
	}
	stamp := now()
	if err := insertRuntimeValue(ctx, tx, prepared, stamp); err != nil {
		return InboxSequence{}, err
	}
	inline, reference := runtimeValueColumns(prepared.RuntimeValue)
	if _, err := tx.ExecContext(ctx, `INSERT INTO inbox(root_id,agent_id,seq,kind,status,payload_inline,payload_ref,created_at) VALUES(?,?,?,?, 'queued',?,?,?)`,
		item.RootID, item.AgentID, sequence.InboxSeq, item.Kind, inline, reference, stamp); err != nil {
		return InboxSequence{}, err
	}
	event.AgentID = item.AgentID
	event.InboxSeq = sequence.InboxSeq
	event.InboxKind = item.Kind
	event.Status = "queued"
	event.CommandClientID = item.CommandClientID
	event.CommandID = item.CommandID
	event.OperationID = item.OperationID
	event.TraceID = item.TraceID
	eventSeq, err := s.insertActorEventTx(ctx, tx, item.RootID, eventKind, event, stamp)
	sequence.EventSeq = eventSeq
	return sequence, err
}

func (s *Store) insertActorEventTx(ctx context.Context, tx *sql.Tx, rootID, kind string, event actorEvent, stamp string) (int64, error) {
	event.RootID = rootID
	payload, err := json.Marshal(event)
	if err != nil {
		return 0, err
	}
	prepared, err := s.prepareRuntimeValue(RuntimePayload{Data: payload, MediaType: "application/json", Source: "actor event"}, ContentGrant{RootID: rootID, Scope: ContentGrantRoot})
	if err != nil {
		return 0, err
	}
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
	return seq, nil
}

func (s *Store) CommitRuntime(ctx context.Context, transition RuntimeTransition) (RuntimeResult, error) {
	return s.commitRuntime(ctx, transition, nil)
}

func (s *Store) commitRuntime(ctx context.Context, transition RuntimeTransition, beforeCommit func() error) (RuntimeResult, error) {
	if transition.Command != nil {
		c := transition.Command
		if c.Scope != CommandScopeRoot && c.Scope != CommandScopeDaemon || c.Scope == CommandScopeRoot && c.RootID == "" || c.Scope == CommandScopeDaemon && c.RootID != "" {
			return RuntimeResult{}, fmt.Errorf("command scope %q does not match root %q", c.Scope, c.RootID)
		}
	}
	var transitionRoot string
	addRoot := func(kind, rootID string) error {
		if rootID == "" {
			return fmt.Errorf("%s requires a root", kind)
		}
		if transitionRoot == "" {
			transitionRoot = rootID
			return nil
		}
		if transitionRoot != rootID {
			return fmt.Errorf("%s root %q does not match transition root %q", kind, rootID, transitionRoot)
		}
		return nil
	}
	type rooted struct{ kind, id string }
	var roots []rooted
	if v := transition.Agent; v != nil {
		roots = append(roots, rooted{"agent", v.RootID})
	}
	if v := transition.Command; v != nil && v.Scope == CommandScopeRoot {
		roots = append(roots, rooted{"command", v.RootID})
	}
	if v := transition.Inbox; v != nil {
		roots = append(roots, rooted{"inbox item", v.RootID})
	}
	if v := transition.State; v != nil {
		roots = append(roots, rooted{"state update", v.RootID})
	}
	if v := transition.Event; v != nil {
		roots = append(roots, rooted{"event", v.RootID})
	}
	if v := transition.Usage; v != nil {
		roots = append(roots, rooted{"usage charge", v.RootID})
	}
	for _, item := range roots {
		if err := addRoot(item.kind, item.id); err != nil {
			return RuntimeResult{}, err
		}
	}
	if transition.Command != nil && transition.Command.Scope == CommandScopeDaemon && transitionRoot != "" {
		return RuntimeResult{}, fmt.Errorf("daemon command cannot share a transition with root %q", transitionRoot)
	}
	var prepared struct {
		command, inbox, state, event preparedRuntimeValue
	}
	var err error
	if transition.Command != nil {
		grant := ContentGrant{RootID: transition.Command.RootID, Scope: ContentGrantRoot}
		if transition.Command.Scope == CommandScopeDaemon && len(transition.Command.Payload.Data) > InlineValueLimit {
			return RuntimeResult{}, errors.New("daemon-scoped large content has no root grant")
		}
		prepared.command, err = s.prepareRuntimeValue(transition.Command.Payload, grant)
	}
	if err == nil && transition.Inbox != nil {
		prepared.inbox, err = s.prepareRuntimeValue(transition.Inbox.Payload, ContentGrant{RootID: transition.Inbox.RootID, AgentID: transition.Inbox.AgentID, Scope: ContentGrantAgent})
	}
	if err == nil && transition.State != nil {
		prepared.state, err = s.prepareRuntimeValue(transition.State.Payload, ContentGrant{RootID: transition.State.RootID, AgentID: transition.State.AgentID, Scope: ContentGrantAgent})
	}
	if err == nil && transition.Event != nil {
		prepared.event, err = s.prepareRuntimeValue(transition.Event.Payload, ContentGrant{RootID: transition.Event.RootID, Scope: ContentGrantRoot})
	}
	if err != nil {
		return RuntimeResult{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RuntimeResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	stamp := now()
	if transition.Agent != nil {
		a := transition.Agent
		if _, err := tx.ExecContext(ctx, `INSERT INTO agents(id,root_id,parent_id,status,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
			a.ID, a.RootID, nullableString(a.ParentID), a.Status, stamp, stamp); err != nil {
			return RuntimeResult{}, err
		}
	}
	for _, value := range []preparedRuntimeValue{prepared.command, prepared.inbox, prepared.state, prepared.event} {
		if err := insertRuntimeValue(ctx, tx, value, stamp); err != nil {
			return RuntimeResult{}, err
		}
	}
	if transition.Command != nil {
		c := transition.Command
		rootID := nullableString(c.RootID)
		inline, reference := runtimeValueColumns(prepared.command.RuntimeValue)
		if _, err := tx.ExecContext(ctx, `INSERT INTO commands(client_id,command_id,scope,root_id,request_digest,status,payload_inline,payload_ref,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			c.ClientID, c.ID, c.Scope, rootID, c.RequestDigest, c.Status, inline, reference, stamp, stamp); err != nil {
			return RuntimeResult{}, err
		}
	}
	if transition.Inbox != nil {
		v := transition.Inbox
		inline, reference := runtimeValueColumns(prepared.inbox.RuntimeValue)
		if _, err := tx.ExecContext(ctx, `INSERT INTO inbox(root_id,agent_id,seq,kind,status,payload_inline,payload_ref,created_at) VALUES(?,?,?,?,?,?,?,?)`,
			v.RootID, v.AgentID, v.Seq, v.Kind, v.Status, inline, reference, stamp); err != nil {
			return RuntimeResult{}, err
		}
	}
	if transition.State != nil {
		v := transition.State
		inline, reference := runtimeValueColumns(prepared.state.RuntimeValue)
		if _, err := tx.ExecContext(ctx, `INSERT INTO agent_state(root_id,agent_id,key,version,author_agent_id,payload_inline,payload_ref,updated_at) VALUES(?,?,?,?,?,?,?,?)`,
			v.RootID, v.AgentID, v.Key, v.Version, v.AuthorAgentID, inline, reference, stamp); err != nil {
			return RuntimeResult{}, err
		}
	}
	if transition.Event != nil {
		v := transition.Event
		inline, reference := runtimeValueColumns(prepared.event.RuntimeValue)
		if _, err := tx.ExecContext(ctx, `INSERT INTO events(root_id,seq,kind,payload_inline,payload_ref,created_at) VALUES(?,?,?,?,?,?)`,
			v.RootID, v.Seq, v.Kind, inline, reference, stamp); err != nil {
			return RuntimeResult{}, err
		}
	}
	if transition.Usage != nil {
		v := transition.Usage
		if v.AgentID != "" {
			var agents int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM agents WHERE root_id=? AND id=?`, v.RootID, v.AgentID).Scan(&agents); err != nil {
				return RuntimeResult{}, err
			}
			if agents != 1 {
				return RuntimeResult{}, errors.New("usage agent is not in root")
			}
		}
		if (v.CommandClientID == "") != (v.CommandID == "") {
			return RuntimeResult{}, errors.New("usage command identity is incomplete")
		}
		if v.CommandID != "" {
			var commands int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM commands WHERE root_id=? AND client_id=? AND command_id=?`, v.RootID, v.CommandClientID, v.CommandID).Scan(&commands); err != nil {
				return RuntimeResult{}, err
			}
			if commands != 1 {
				return RuntimeResult{}, errors.New("usage command is not in root")
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO usage_charges(id,root_id,agent_id,command_client_id,command_id,input_tokens,cached_tokens,output_tokens,cost_micros,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			v.ID, v.RootID, v.AgentID, v.CommandClientID, v.CommandID, v.InputTokens, v.CachedTokens, v.OutputTokens, v.CostMicros, stamp); err != nil {
			return RuntimeResult{}, err
		}
	}
	if beforeCommit != nil {
		if err := beforeCommit(); err != nil {
			return RuntimeResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return RuntimeResult{}, err
	}
	return RuntimeResult{
		Command: prepared.command.RuntimeValue,
		Inbox:   prepared.inbox.RuntimeValue,
		State:   prepared.state.RuntimeValue,
		Event:   prepared.event.RuntimeValue,
	}, nil
}

func (s *Store) FinishCommand(ctx context.Context, clientID, commandID, status string, outcome RuntimePayload) (RuntimeValue, error) {
	if status != "succeeded" && status != "failed" && status != "cancelled" && status != "interrupted" {
		return RuntimeValue{}, fmt.Errorf("command outcome status %q is not terminal", status)
	}
	var scope CommandScope
	var rootID sql.NullString
	var currentStatus string
	if err := s.db.QueryRowContext(ctx, `SELECT scope,root_id,status FROM commands WHERE client_id=? AND command_id=?`, clientID, commandID).Scan(&scope, &rootID, &currentStatus); err != nil {
		return RuntimeValue{}, err
	}
	if currentStatus != "queued" && currentStatus != "running" && currentStatus != "waiting" {
		return RuntimeValue{}, fmt.Errorf("command is already terminal with status %q", currentStatus)
	}
	if scope == CommandScopeDaemon && len(outcome.Data) > InlineValueLimit {
		return RuntimeValue{}, errors.New("daemon-scoped large content has no root grant")
	}
	prepared, err := s.prepareRuntimeValue(outcome, ContentGrant{RootID: rootID.String, Scope: ContentGrantRoot})
	if err != nil {
		return RuntimeValue{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RuntimeValue{}, err
	}
	defer func() { _ = tx.Rollback() }()
	stamp := now()
	if err := insertRuntimeValue(ctx, tx, prepared, stamp); err != nil {
		return RuntimeValue{}, err
	}
	inline, reference := runtimeValueColumns(prepared.RuntimeValue)
	result, err := tx.ExecContext(ctx, `UPDATE commands SET status=?,outcome_inline=?,outcome_ref=?,updated_at=? WHERE client_id=? AND command_id=? AND status IN ('queued','running','waiting')`,
		status, inline, reference, stamp, clientID, commandID)
	if err != nil {
		return RuntimeValue{}, err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		if err != nil {
			return RuntimeValue{}, err
		}
		return RuntimeValue{}, errors.New("command became terminal before outcome commit")
	}
	if scope == CommandScopeRoot {
		if _, err := s.insertActorEventTx(ctx, tx, rootID.String, "command."+status, actorEvent{
			Status: status, CommandClientID: clientID, CommandID: commandID,
		}, stamp); err != nil {
			return RuntimeValue{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return RuntimeValue{}, err
	}
	return prepared.RuntimeValue, nil
}

func (s *Store) StoreContent(ctx context.Context, grant ContentGrant, payload RuntimePayload) (RuntimeValue, error) {
	if err := s.validateContentGrant(ctx, grant); err != nil {
		return RuntimeValue{}, err
	}
	prepared, err := s.prepareContentReference(payload, grant)
	if err != nil {
		return RuntimeValue{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RuntimeValue{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := insertRuntimeValue(ctx, tx, prepared, now()); err != nil {
		return RuntimeValue{}, err
	}
	if err := tx.Commit(); err != nil {
		return RuntimeValue{}, err
	}
	return prepared.RuntimeValue, nil
}

func (s *Store) ReadContent(ctx context.Context, referenceID, rootID, agentID string, offset int64, length int) ([]byte, ContentMetadata, error) {
	var meta ContentMetadata
	meta.ReferenceID = referenceID
	if err := s.db.QueryRowContext(ctx, `SELECT r.digest,r.size,r.media_type,r.source FROM content_references r WHERE r.id=?`, referenceID).
		Scan(&meta.Digest, &meta.Size, &meta.MediaType, &meta.Source); err != nil {
		return nil, ContentMetadata{}, ErrContentAccess
	}
	rows, err := s.db.QueryContext(ctx, `SELECT scope,agent_id FROM content_grants WHERE reference_id=? AND root_id=? AND revoked_at=''`, referenceID, rootID)
	if err != nil {
		return nil, ContentMetadata{}, err
	}
	defer func() { _ = rows.Close() }()
	type grant struct {
		scope   ContentGrantScope
		agentID string
	}
	var grants []grant
	for rows.Next() {
		var g grant
		if err := rows.Scan(&g.scope, &g.agentID); err != nil {
			return nil, ContentMetadata{}, err
		}
		grants = append(grants, g)
	}
	if err := rows.Err(); err != nil {
		return nil, ContentMetadata{}, err
	}
	if err := rows.Close(); err != nil {
		return nil, ContentMetadata{}, err
	}
	authorized := false
	for _, grant := range grants {
		switch grant.scope {
		case ContentGrantRoot:
			authorized = agentID == "" || s.agentInRoot(ctx, rootID, agentID)
		case ContentGrantAgent:
			authorized = agentID != "" && agentID == grant.agentID && s.agentInRoot(ctx, rootID, agentID)
		case ContentGrantSubtree:
			authorized = agentID != "" && s.agentInSubtree(ctx, rootID, grant.agentID, agentID)
		}
		if authorized {
			break
		}
	}
	if !authorized {
		return nil, ContentMetadata{}, ErrContentAccess
	}
	data, err := s.content.Read(meta.Digest, offset, length)
	return data, meta, err
}

func (s *Store) RevokeContentGrant(ctx context.Context, referenceID, rootID, agentID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE content_grants SET revoked_at=? WHERE reference_id=? AND root_id=? AND agent_id=? AND revoked_at=''`,
		now(), referenceID, rootID, agentID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrContentAccess
	}
	return nil
}

type ContentOrphan struct {
	Digest      string
	Size        int64
	FirstSeenAt string
	LastSeenAt  string
}

func (s *Store) OrphanContent(ctx context.Context) ([]ContentOrphan, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT digest,size,first_seen_at,last_seen_at FROM content_orphans ORDER BY digest`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ContentOrphan
	for rows.Next() {
		var orphan ContentOrphan
		if err := rows.Scan(&orphan.Digest, &orphan.Size, &orphan.FirstSeenAt, &orphan.LastSeenAt); err != nil {
			return nil, err
		}
		out = append(out, orphan)
	}
	return out, rows.Err()
}

func (s *Store) prepareRuntimeValue(payload RuntimePayload, grant ContentGrant) (preparedRuntimeValue, error) {
	value := RuntimeValue{Size: int64(len(payload.Data)), MediaType: payload.MediaType, Source: payload.Source}
	if len(payload.Data) <= InlineValueLimit {
		value.Inline = append([]byte(nil), payload.Data...)
		return preparedRuntimeValue{RuntimeValue: value}, nil
	}
	return s.prepareContentReference(payload, grant)
}

func (s *Store) prepareContentReference(payload RuntimePayload, grant ContentGrant) (preparedRuntimeValue, error) {
	if grant.RootID == "" {
		return preparedRuntimeValue{}, errors.New("large content requires a root grant")
	}
	if grant.Scope != ContentGrantRoot && grant.Scope != ContentGrantAgent && grant.Scope != ContentGrantSubtree {
		return preparedRuntimeValue{}, fmt.Errorf("invalid content grant scope %q", grant.Scope)
	}
	if grant.Scope != ContentGrantRoot && grant.AgentID == "" {
		return preparedRuntimeValue{}, fmt.Errorf("%s content grant requires an agent", grant.Scope)
	}
	body, err := s.content.Put(payload.Data)
	if err != nil {
		return preparedRuntimeValue{}, err
	}
	referenceID, err := runtimeID()
	if err != nil {
		return preparedRuntimeValue{}, err
	}
	value := RuntimeValue{
		ReferenceID: referenceID,
		Digest:      body.Digest,
		Size:        int64(len(payload.Data)),
		MediaType:   payload.MediaType,
		Source:      payload.Source,
	}
	return preparedRuntimeValue{RuntimeValue: value, grant: grant}, nil
}

func insertRuntimeValue(ctx context.Context, tx *sql.Tx, value preparedRuntimeValue, stamp string) error {
	if value.ReferenceID == "" {
		return nil
	}
	if value.grant.Scope == ContentGrantRoot {
		if value.grant.AgentID != "" {
			return errors.New("root content grant cannot name an agent")
		}
	} else {
		var agents int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM agents WHERE root_id=? AND id=?`, value.grant.RootID, value.grant.AgentID).Scan(&agents); err != nil {
			return err
		}
		if agents != 1 {
			return errors.New("content grant agent is not in root")
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO content_objects(digest,size,created_at) VALUES(?,?,?)`, value.Digest, value.Size, stamp); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO content_references(id,digest,size,media_type,source,created_at) VALUES(?,?,?,?,?,?)`,
		value.ReferenceID, value.Digest, value.Size, value.MediaType, value.Source, stamp); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO content_grants(reference_id,root_id,agent_id,scope,created_at) VALUES(?,?,?,?,?)`,
		value.ReferenceID, value.grant.RootID, value.grant.AgentID, value.grant.Scope, stamp)
	return err
}

func runtimeValueColumns(value RuntimeValue) (any, any) {
	if value.ReferenceID != "" {
		return nil, value.ReferenceID
	}
	return value.Inline, nil
}

func (s *Store) validateContentGrant(ctx context.Context, grant ContentGrant) error {
	if grant.RootID == "" {
		return errors.New("content grant requires a root")
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE id=?`, grant.RootID).Scan(&exists); err != nil {
		return err
	}
	if exists != 1 {
		return fmt.Errorf("unknown content root %q", grant.RootID)
	}
	if grant.Scope == ContentGrantRoot && grant.AgentID == "" {
		return nil
	}
	if grant.Scope != ContentGrantAgent && grant.Scope != ContentGrantSubtree {
		return errors.New("invalid content grant")
	}
	if !s.agentInRoot(ctx, grant.RootID, grant.AgentID) {
		return errors.New("content grant agent is not in root")
	}
	return nil
}

func (s *Store) agentInRoot(ctx context.Context, rootID, agentID string) bool {
	var n int
	return s.db.QueryRowContext(ctx, `SELECT count(*) FROM agents WHERE root_id=? AND id=?`, rootID, agentID).Scan(&n) == nil && n == 1
}

func (s *Store) agentInSubtree(ctx context.Context, rootID, ancestorID, agentID string) bool {
	var n int
	err := s.db.QueryRowContext(ctx, `WITH RECURSIVE descendants(id) AS (
		SELECT id FROM agents WHERE root_id=? AND id=?
		UNION
		SELECT a.id FROM agents a JOIN descendants d ON a.parent_id=d.id WHERE a.root_id=?
	) SELECT count(*) FROM descendants WHERE id=?`, rootID, ancestorID, rootID, agentID).Scan(&n)
	return err == nil && n == 1
}

func runtimeID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Recover applies daemon-startup recovery after exclusive ownership is held.
// Opening the store alone must not interrupt work owned by a running daemon.
func (s *Store) Recover(ctx context.Context) error {
	if err := recoverRuntime(ctx, s); err != nil {
		return err
	}
	return recordOrphanContent(ctx, s)
}

func recoverRuntime(ctx context.Context, s *Store) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stamp := now()
	if err := s.cancelPendingPermissionsTx(ctx, tx, "", "", "", "interrupted", "", "interrupted by daemon restart"); err != nil {
		return err
	}
	if err := s.settleInterruptedOperationReservations(ctx, tx, "", ""); err != nil {
		return err
	}
	if err := settleInterruptedBudgetReservations(ctx, tx); err != nil {
		return err
	}
	if err := s.emitInterruptedTurnEventsTx(ctx, tx, "", "interrupted by daemon restart", stamp); err != nil {
		return err
	}
	for _, table := range []string{"commands", "turns", "operations", "leases"} {
		if _, err := tx.ExecContext(ctx, `UPDATE `+table+` SET status='interrupted',updated_at=? WHERE status IN ('queued','running','waiting')`, stamp); err != nil { //nolint:gosec // table comes from the static allowlist above
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agents SET status='idle',updated_at=? WHERE parent_id IS NOT NULL AND status='running'`, stamp); err != nil {
		return err
	}
	if err := syncChildBudgetReservationsTx(ctx, tx, ""); err != nil {
		return err
	}
	// Only uncertain running input is interrupted; queued input (including
	// human follow-ups) survives a restart and runs when the node reopens.
	if _, err := tx.ExecContext(ctx, `UPDATE inbox SET status='interrupted' WHERE status='running'`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE permission_requests SET status='interrupted',updated_at=? WHERE status='pending'`, stamp); err != nil {
		return err
	}
	return tx.Commit()
}

func settleInterruptedBudgetReservations(ctx context.Context, tx *sql.Tx) error {
	stamp := now()
	if _, err := tx.ExecContext(ctx, `UPDATE budgets SET used_value=used_value+reserved_value,reserved_value=0,updated_at=?
		WHERE kind NOT IN (?,?,?)`, stamp, BudgetActiveOperations, BudgetActiveChildren, BudgetConcurrentChildTurns); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE budgets SET reserved_value=0,updated_at=? WHERE kind=?`, stamp, BudgetActiveOperations)
	return err
}

func recordOrphanContent(ctx context.Context, s *Store) error {
	referenced := map[string]struct{}{}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT digest FROM content_references`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var digest string
		if err := rows.Scan(&digest); err != nil {
			return err
		}
		referenced[digest] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	orphans, err := s.content.Orphans(referenced)
	if err != nil {
		return err
	}
	if len(orphans) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stamp := now()
	for _, orphan := range orphans {
		if _, err := tx.ExecContext(ctx, `INSERT INTO content_orphans(digest,size,first_seen_at,last_seen_at) VALUES(?,?,?,?)
			ON CONFLICT(digest) DO UPDATE SET size=excluded.size,last_seen_at=excluded.last_seen_at`,
			orphan.Digest, orphan.Size, stamp, stamp); err != nil {
			return err
		}
	}
	return tx.Commit()
}
