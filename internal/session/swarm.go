package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

var (
	ErrAgentAccess   = errors.New("agent is outside authenticated relative scope")
	ErrAgentTerminal = errors.New("agent is terminal")
)

type ChildAdmission struct {
	RootID        string
	ParentAgentID string
	ChildAgentID  string
	ExecutionID   string
}

type AgentRelatives struct {
	Parent   *RuntimeAgent
	Children []RuntimeAgent
	Siblings []RuntimeAgent
}

type MessageDelivery string

const (
	DeliveryQueued    MessageDelivery = "queued"
	DeliveryNextTurn  MessageDelivery = "next_turn"
	DeliveryImmediate MessageDelivery = "immediate"
)

type AgentMessage struct {
	Delivery            MessageDelivery
	Body                string
	EvidenceReferenceID string
}

type AgentMessageEnvelope struct {
	SenderAgentID       string          `json:"sender_agent_id"`
	RecipientAgentID    string          `json:"recipient_agent_id"`
	Delivery            MessageDelivery `json:"delivery"`
	Body                string          `json:"body,omitempty"`
	EvidenceReferenceID string          `json:"evidence_reference_id,omitempty"`
}

const subtreeCTE = `WITH RECURSIVE subtree(id) AS (
	SELECT id FROM agents WHERE root_id=? AND id=?
	UNION ALL
	SELECT a.id FROM agents a JOIN subtree s ON a.parent_id=s.id WHERE a.root_id=?
) `

func (s *Store) AdmitChild(ctx context.Context, admission ChildAdmission) (int64, error) {
	if admission.RootID == "" || admission.ParentAgentID == "" || admission.ChildAgentID == "" || admission.ExecutionID == "" || admission.ParentAgentID == admission.ChildAgentID {
		return 0, errors.New("child admission requires distinct root, parent, child, and execution identities")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	parent, err := loadAgentTx(ctx, tx, admission.RootID, admission.ParentAgentID)
	if err != nil {
		return 0, err
	}
	if isTerminalAgentStatus(parent.Status) {
		return 0, ErrAgentTerminal
	}
	stamp := now()
	if _, err := tx.ExecContext(ctx, `INSERT INTO agents(id,root_id,parent_id,status,created_at,updated_at) VALUES(?,?,?,'idle',?,?)`,
		admission.ChildAgentID, admission.RootID, admission.ParentAgentID, stamp, stamp); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO child_executions(id,root_id,parent_agent_id,child_agent_id,status,created_at,updated_at) VALUES(?,?,?,?, 'queued',?,?)`,
		admission.ExecutionID, admission.RootID, admission.ParentAgentID, admission.ChildAgentID, stamp, stamp); err != nil {
		return 0, err
	}
	eventSeq, err := s.insertActorEventTx(ctx, tx, admission.RootID, "child.admitted", actorEvent{
		AgentID: admission.ChildAgentID, Status: "queued", ChildExecutionID: admission.ExecutionID,
	}, stamp)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return eventSeq, nil
}

func (s *Store) ListAgentRelatives(ctx context.Context, rootID, callerAgentID string) (AgentRelatives, error) {
	if rootID == "" || callerAgentID == "" {
		return AgentRelatives{}, ErrAgentAccess
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return AgentRelatives{}, err
	}
	defer tx.Rollback()
	caller, err := loadAgentTx(ctx, tx, rootID, callerAgentID)
	if err != nil {
		return AgentRelatives{}, err
	}
	result := AgentRelatives{}
	if caller.ParentID != "" {
		parent, err := loadAgentTx(ctx, tx, rootID, caller.ParentID)
		if err != nil {
			return AgentRelatives{}, err
		}
		result.Parent = &parent
		result.Siblings, err = loadAgentsTx(ctx, tx, `SELECT id,root_id,COALESCE(parent_id,''),status FROM agents WHERE root_id=? AND parent_id=? AND id<>? ORDER BY id`, rootID, caller.ParentID, callerAgentID)
		if err != nil {
			return AgentRelatives{}, err
		}
	}
	result.Children, err = loadAgentsTx(ctx, tx, `SELECT id,root_id,COALESCE(parent_id,''),status FROM agents WHERE root_id=? AND parent_id=? ORDER BY id`, rootID, callerAgentID)
	if err != nil {
		return AgentRelatives{}, err
	}
	if err := tx.Commit(); err != nil {
		return AgentRelatives{}, err
	}
	return result, nil
}

func (s *Store) SendAgentMessage(ctx context.Context, rootID, senderAgentID, recipientAgentID string, message AgentMessage) (InboxSequence, error) {
	if rootID == "" || senderAgentID == "" || recipientAgentID == "" || senderAgentID == recipientAgentID {
		return InboxSequence{}, ErrAgentAccess
	}
	if message.Delivery != DeliveryQueued && message.Delivery != DeliveryNextTurn && message.Delivery != DeliveryImmediate {
		return InboxSequence{}, fmt.Errorf("invalid message delivery %q", message.Delivery)
	}
	if message.Body == "" && message.EvidenceReferenceID == "" {
		return InboxSequence{}, errors.New("message requires a body or evidence reference")
	}
	envelope := AgentMessageEnvelope{
		SenderAgentID: senderAgentID, RecipientAgentID: recipientAgentID, Delivery: message.Delivery,
		Body: message.Body, EvidenceReferenceID: message.EvidenceReferenceID,
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return InboxSequence{}, err
	}
	prepared, err := s.prepareRuntimeValue(RuntimePayload{Data: payload, MediaType: "application/json", Source: "peer message envelope"}, ContentGrant{
		RootID: rootID, AgentID: recipientAgentID, Scope: ContentGrantAgent,
	})
	if err != nil {
		return InboxSequence{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return InboxSequence{}, err
	}
	defer tx.Rollback()
	if err := validateDirectRelativeTx(ctx, tx, rootID, senderAgentID, recipientAgentID); err != nil {
		return InboxSequence{}, err
	}
	stamp := now()
	if message.EvidenceReferenceID != "" {
		authorized, err := contentAuthorizedTx(ctx, tx, message.EvidenceReferenceID, rootID, senderAgentID)
		if err != nil {
			return InboxSequence{}, err
		}
		if !authorized {
			return InboxSequence{}, ErrContentAccess
		}
		recipientAuthorized, err := contentAuthorizedTx(ctx, tx, message.EvidenceReferenceID, rootID, recipientAgentID)
		if err != nil {
			return InboxSequence{}, err
		}
		if !recipientAuthorized {
			if _, err := tx.ExecContext(ctx, `INSERT INTO content_grants(reference_id,root_id,agent_id,scope,created_at) VALUES(?,?,?,'agent',?)
				ON CONFLICT(reference_id,root_id,agent_id,scope) DO UPDATE SET revoked_at='',created_at=excluded.created_at`,
				message.EvidenceReferenceID, rootID, recipientAgentID, stamp); err != nil {
				return InboxSequence{}, err
			}
		}
	}
	kind := "peer.message"
	if message.Delivery == DeliveryImmediate {
		kind = "steer"
	}
	sequence, err := s.enqueueInboxTx(ctx, tx, InboxEnqueue{
		RootID: rootID, AgentID: recipientAgentID, Kind: kind,
	}, prepared, "message.queued", actorEvent{
		SenderAgentID: senderAgentID, Delivery: string(message.Delivery),
	})
	if err != nil {
		return InboxSequence{}, err
	}
	if err := tx.Commit(); err != nil {
		return InboxSequence{}, err
	}
	return sequence, nil
}

func (s *Store) TerminalizeSubtree(ctx context.Context, rootID, callerAgentID, targetAgentID, status string) (int64, error) {
	if rootID == "" || callerAgentID == "" || targetAgentID == "" || status != "stopped" && status != "deleted" {
		return 0, errors.New("subtree terminalization requires root, caller, target, and stopped or deleted status")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	caller, err := loadAgentTx(ctx, tx, rootID, callerAgentID)
	if err != nil {
		return 0, err
	}
	target, err := loadAgentTx(ctx, tx, rootID, targetAgentID)
	if err != nil {
		return 0, err
	}
	if isTerminalAgentStatus(caller.Status) && callerAgentID != targetAgentID {
		return 0, ErrAgentTerminal
	}
	allowed, err := agentInSubtreeTx(ctx, tx, rootID, callerAgentID, targetAgentID)
	if err != nil {
		return 0, err
	}
	if !allowed {
		return 0, ErrAgentAccess
	}
	if status == "stopped" && isTerminalAgentStatus(target.Status) || status == "deleted" && target.Status == "deleted" {
		return 0, ErrAgentTerminal
	}
	stamp := now()
	agentWhere := `status NOT IN ('failed','stopped','cancelled','interrupted','deleted','succeeded')`
	executionWhere := `status IN ('queued','running','waiting','idle')`
	if status == "deleted" {
		agentWhere = `status!='deleted'`
		executionWhere = `status!='deleted'`
	}
	if _, err := tx.ExecContext(ctx, subtreeCTE+`UPDATE agents SET status=?,updated_at=? WHERE root_id=? AND id IN (SELECT id FROM subtree) AND `+agentWhere,
		rootID, targetAgentID, rootID, status, stamp, rootID); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, subtreeCTE+`UPDATE child_executions SET status=?,updated_at=? WHERE root_id=? AND child_agent_id IN (SELECT id FROM subtree) AND `+executionWhere,
		rootID, targetAgentID, rootID, status, stamp, rootID); err != nil {
		return 0, err
	}
	interrupt := func(query string, args ...any) error {
		_, err := tx.ExecContext(ctx, subtreeCTE+query, append([]any{rootID, targetAgentID, rootID}, args...)...)
		return err
	}
	if err := interrupt(`UPDATE inbox SET status='interrupted' WHERE root_id=? AND agent_id IN (SELECT id FROM subtree) AND status IN ('queued','running')`, rootID); err != nil {
		return 0, err
	}
	for _, query := range []string{
		`UPDATE turns SET status='interrupted',updated_at=? WHERE root_id=? AND agent_id IN (SELECT id FROM subtree) AND status IN ('queued','running','waiting')`,
		`UPDATE operations SET status='interrupted',updated_at=? WHERE root_id=? AND agent_id IN (SELECT id FROM subtree) AND status IN ('queued','running','waiting')`,
		`UPDATE leases SET status='interrupted',updated_at=? WHERE root_id=? AND agent_id IN (SELECT id FROM subtree) AND status IN ('queued','running','waiting')`,
		`UPDATE permission_requests SET status='interrupted',updated_at=? WHERE root_id=? AND agent_id IN (SELECT id FROM subtree) AND status='pending'`,
	} {
		if err := interrupt(query, stamp, rootID); err != nil {
			return 0, err
		}
	}
	eventSeq, err := s.insertActorEventTx(ctx, tx, rootID, "agent.subtree."+status, actorEvent{AgentID: targetAgentID, Status: status}, stamp)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return eventSeq, nil
}

func loadAgentTx(ctx context.Context, tx *sql.Tx, rootID, agentID string) (RuntimeAgent, error) {
	var agent RuntimeAgent
	err := tx.QueryRowContext(ctx, `SELECT id,root_id,COALESCE(parent_id,''),status FROM agents WHERE root_id=? AND id=?`, rootID, agentID).
		Scan(&agent.ID, &agent.RootID, &agent.ParentID, &agent.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return RuntimeAgent{}, ErrAgentAccess
	}
	return agent, err
}

func loadAgentsTx(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]RuntimeAgent, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var agents []RuntimeAgent
	for rows.Next() {
		var agent RuntimeAgent
		if err := rows.Scan(&agent.ID, &agent.RootID, &agent.ParentID, &agent.Status); err != nil {
			return nil, err
		}
		agents = append(agents, agent)
	}
	return agents, rows.Err()
}

func validateDirectRelativeTx(ctx context.Context, tx *sql.Tx, rootID, senderAgentID, recipientAgentID string) error {
	sender, err := loadAgentTx(ctx, tx, rootID, senderAgentID)
	if err != nil {
		return err
	}
	recipient, err := loadAgentTx(ctx, tx, rootID, recipientAgentID)
	if err != nil {
		return err
	}
	if isTerminalAgentStatus(sender.Status) || isTerminalAgentStatus(recipient.Status) {
		return ErrAgentTerminal
	}
	if sender.ParentID == recipient.ID || recipient.ParentID == sender.ID || sender.ParentID != "" && sender.ParentID == recipient.ParentID {
		return nil
	}
	return ErrAgentAccess
}

func contentAuthorizedTx(ctx context.Context, tx *sql.Tx, referenceID, rootID, agentID string) (bool, error) {
	var authorized int
	err := tx.QueryRowContext(ctx, `WITH RECURSIVE ancestors(id,parent_id) AS (
		SELECT id,parent_id FROM agents WHERE root_id=? AND id=?
		UNION ALL
		SELECT a.id,a.parent_id FROM agents a JOIN ancestors p ON a.id=p.parent_id WHERE a.root_id=?
	) SELECT EXISTS(
		SELECT 1 FROM content_grants g WHERE g.reference_id=? AND g.root_id=? AND g.revoked_at=''
		AND EXISTS(SELECT 1 FROM ancestors)
		AND (g.scope='root' OR (g.scope='agent' AND g.agent_id=?) OR (g.scope='subtree' AND g.agent_id IN (SELECT id FROM ancestors)))
	)`, rootID, agentID, rootID, referenceID, rootID, agentID).Scan(&authorized)
	return authorized == 1, err
}

func agentInSubtreeTx(ctx context.Context, tx *sql.Tx, rootID, ancestorAgentID, agentID string) (bool, error) {
	var found int
	err := tx.QueryRowContext(ctx, subtreeCTE+`SELECT count(*) FROM subtree WHERE id=?`, rootID, ancestorAgentID, rootID, agentID).Scan(&found)
	return found == 1, err
}

func isTerminalAgentStatus(status string) bool {
	return status == "failed" || status == "stopped" || status == "cancelled" || status == "interrupted" || status == "deleted" || status == "succeeded"
}
