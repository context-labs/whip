package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
)

// AgentTurnStart describes what a claimed turn is about. An inbox turn claims
// exactly one queued row (one-at-a-time); a mailbox turn claims nothing and
// tells the agent that ready mail exists.
type AgentTurnStart struct {
	TurnID  string
	Trigger string // "inbox" or "mailbox"
	Items   []InboxItem
}

// AgentTurnCommit settles one agent turn. DeliveredMessages are the mailbox
// rows the turn showed or read; they are marked delivered only when the turn
// succeeded, so a failed turn redelivers them.
type AgentTurnCommit struct {
	TurnID            string
	Status            string
	AcknowledgedInbox []int64
	DeliveredMessages []string
	Transcript        []llm.Message
	Error             string
}

func (s *Store) StartAgentTurn(ctx context.Context, rootID, agentID, turnID string) (AgentTurnStart, error) {
	if rootID == "" || agentID == "" || turnID == "" {
		return AgentTurnStart{}, errors.New("agent turn requires root, agent, and turn identities")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AgentTurnStart{}, err
	}
	defer func() { _ = tx.Rollback() }()
	agent, err := loadAgentTx(ctx, tx, rootID, agentID)
	if err != nil {
		return AgentTurnStart{}, err
	}
	if isTerminalAgentStatus(agent.Status) || agent.Status == "running" {
		return AgentTurnStart{}, ErrAgentTerminal
	}
	rows, err := tx.QueryContext(ctx, `SELECT i.seq,i.kind,i.status,substr(i.payload_inline,1,?),COALESCE(i.payload_ref,''),
		COALESCE(r.digest,''),COALESCE(r.size,0),COALESCE(r.media_type,''),COALESCE(r.source,'')
		FROM inbox i LEFT JOIN content_references r ON r.id=i.payload_ref
		WHERE i.root_id=? AND i.agent_id=? AND i.status='queued' ORDER BY i.seq LIMIT 1`,
		InlineValueLimit+1, rootID, agentID)
	if err != nil {
		return AgentTurnStart{}, err
	}
	items, err := scanInboxRows(rows, rootID, agentID)
	closeErr := rows.Close() //nolint:sqlclosecheck // rows must close before the next statement on this tx
	if err != nil {
		return AgentTurnStart{}, err
	}
	if closeErr != nil {
		return AgentTurnStart{}, closeErr
	}
	start := AgentTurnStart{TurnID: turnID, Trigger: "inbox", Items: items}
	if len(items) == 0 {
		var ready bool
		// Same readiness predicate as AgentWorkStatus: next_turn mail rides
		// along with a turn but never starts one.
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agent_messages WHERE root_id=? AND recipient_agent_id=? AND status='pending' AND available_at<=? AND delivery<>'next_turn')`,
			rootID, agentID, now()).Scan(&ready); err != nil {
			return AgentTurnStart{}, err
		}
		if !ready {
			return AgentTurnStart{}, errors.New("agent has no queued turn input")
		}
		start.Trigger = "mailbox"
	}
	if err := syncChildBudgetReservationsTx(ctx, tx, rootID); err != nil {
		return AgentTurnStart{}, err
	}
	if agent.ParentID != "" {
		if err := reserveCapabilityBudgets(ctx, tx, rootID, agent.ParentID, []capability.Reservation{{
			Kind: string(BudgetConcurrentChildTurns), Amount: 1,
		}}); err != nil {
			return AgentTurnStart{}, err
		}
	}
	stamp := now()
	var inboxSeq int64
	for index := range items {
		result, err := tx.ExecContext(ctx, `UPDATE inbox SET status='running'
			WHERE root_id=? AND agent_id=? AND seq=? AND status='queued'`, rootID, agentID, items[index].Seq)
		if err != nil {
			return AgentTurnStart{}, err
		}
		if changed, err := result.RowsAffected(); err != nil || changed != 1 {
			if err != nil {
				return AgentTurnStart{}, err
			}
			return AgentTurnStart{}, ErrInboxTerminal
		}
		items[index].Status = "running"
		inboxSeq = items[index].Seq
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO turns(id,root_id,agent_id,status,trigger,created_at,updated_at)
		VALUES(?,?,?,'running',?,?,?)`, turnID, rootID, agentID, start.Trigger, stamp, stamp); err != nil {
		return AgentTurnStart{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agents SET status='running',updated_at=? WHERE root_id=? AND id=?`, stamp, rootID, agentID); err != nil {
		return AgentTurnStart{}, err
	}
	if err := syncChildBudgetReservationsTx(ctx, tx, rootID); err != nil {
		return AgentTurnStart{}, err
	}
	_, err = s.insertActorEventTx(ctx, tx, rootID, "agent.turn.started", actorEvent{
		AgentID: agentID, InboxSeq: inboxSeq, InboxKind: start.Trigger, Phase: "running", Status: "running",
	}, stamp)
	if err != nil {
		return AgentTurnStart{}, err
	}
	if err := tx.Commit(); err != nil {
		return AgentTurnStart{}, err
	}
	return start, nil
}

// FinishAgentTurn terminalizes the turn, not the retained agent. Ordinary
// success, failure, cancellation, and interruption all return it to idle.
func (s *Store) FinishAgentTurn(ctx context.Context, rootID, agentID string, commit AgentTurnCommit) error {
	status := commit.Status
	if status != "succeeded" && status != "failed" && status != "cancelled" && status != "interrupted" {
		return fmt.Errorf("invalid turn status %q", status)
	}
	if commit.TurnID == "" {
		return errors.New("agent turn commit requires a turn id")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stamp := now()
	result, err := tx.ExecContext(ctx, `UPDATE turns SET status=?,updated_at=? WHERE id=? AND root_id=? AND agent_id=? AND status='running'`,
		status, stamp, commit.TurnID, rootID, agentID)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		if err != nil {
			return err
		}
		return errors.New("agent turn is not running")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agents SET status='idle',updated_at=? WHERE root_id=? AND id=? AND status='running'`, stamp, rootID, agentID); err != nil {
		return err
	}
	seen := make(map[int64]struct{}, len(commit.AcknowledgedInbox))
	for _, seq := range commit.AcknowledgedInbox {
		if seq < 1 {
			return errors.New("acknowledged inbox sequence must be positive")
		}
		if _, duplicate := seen[seq]; duplicate {
			continue
		}
		seen[seq] = struct{}{}
		if _, err := s.consumeInboxTx(ctx, tx, rootID, agentID, seq, stamp); err != nil && !errors.Is(err, ErrInboxTerminal) {
			return err
		}
	}
	// A failed turn retries its claimed input a bounded number of times so a
	// transient provider error never silently drops a human's message. A
	// cancelled or interrupted turn drops it: that is the user's intent.
	if status == "failed" {
		if _, err := tx.ExecContext(ctx, `UPDATE inbox SET status='queued',retries=retries+1
			WHERE root_id=? AND agent_id=? AND status='running' AND retries<?`, rootID, agentID, MaxInboxRetries); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE inbox SET status='interrupted'
		WHERE root_id=? AND agent_id=? AND status='running'`, rootID, agentID); err != nil {
		return err
	}
	if status == "succeeded" {
		if err := s.markMessagesDeliveredTx(ctx, tx, rootID, agentID, commit.TurnID, commit.DeliveredMessages, stamp); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM transcript_messages WHERE root_id=? AND agent_id=?`, rootID, agentID); err != nil {
		return err
	}
	for seq, message := range commit.Transcript {
		if message.Role == "" {
			continue
		}
		body, err := json.Marshal(message)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO transcript_messages(root_id,agent_id,seq,role,content) VALUES(?,?,?,?,?)`,
			rootID, agentID, seq, message.Role, string(body)); err != nil {
			return err
		}
	}
	if err := syncChildBudgetReservationsTx(ctx, tx, rootID); err != nil {
		return err
	}
	if _, err := s.insertActorEventTx(ctx, tx, rootID, "agent.turn."+status, actorEvent{
		AgentID: agentID, Phase: "idle", Status: status, TerminalCause: status,
		Error: commit.Error, Acknowledged: commit.AcknowledgedInbox,
	}, stamp); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) LoadAgentTranscript(ctx context.Context, rootID, agentID string) ([]llm.Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT content FROM transcript_messages WHERE root_id=? AND agent_id=? ORDER BY seq`, rootID, agentID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var messages []llm.Message
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		var message llm.Message
		if err := json.Unmarshal([]byte(body), &message); err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, rows.Err()
}

func (s *Store) LoadAgent(ctx context.Context, rootID, agentID string) (RuntimeAgent, error) {
	if rootID == "" || agentID == "" {
		return RuntimeAgent{}, ErrAgentAccess
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return RuntimeAgent{}, err
	}
	defer func() { _ = tx.Rollback() }()
	value, err := loadAgentTx(ctx, tx, rootID, agentID)
	if err != nil {
		return RuntimeAgent{}, err
	}
	return value, tx.Commit()
}

func (s *Store) LoadRetainedAgents(ctx context.Context, rootID string) ([]RuntimeAgent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,root_id,COALESCE(parent_id,''),name,model,provider,effort,cwd,status
		FROM agents WHERE root_id=? AND parent_id IS NOT NULL AND status NOT IN ('stopped','deleted','failed') ORDER BY created_at,id`, rootID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []RuntimeAgent
	for rows.Next() {
		var value RuntimeAgent
		if err := rows.Scan(&value.ID, &value.RootID, &value.ParentID, &value.Name, &value.Model, &value.Provider, &value.Effort, &value.CWD, &value.Status); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}
