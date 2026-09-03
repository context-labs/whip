package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/context-labs/whip/internal/capability"
)

var (
	ErrAgentAccess   = errors.New("agent is outside authenticated relative scope")
	ErrAgentTerminal = errors.New("agent is terminal")
)

type AgentAdmission struct {
	RootID        string
	ParentAgentID string
	ChildAgentID  string
	Name          string
	Model         string
	Provider      string
	Effort        string
	CWD           string
	Prompt        RuntimePayload
	Budgets       []BudgetLimit
	Capabilities  []CapabilityDelegation
}

type AgentRelatives struct {
	Parent   *RuntimeAgent
	Children []RuntimeAgent
	Siblings []RuntimeAgent
}

const subtreeCTE = `WITH RECURSIVE subtree(id) AS (
	SELECT id FROM agents WHERE root_id=? AND id=?
	UNION ALL
	SELECT a.id FROM agents a JOIN subtree s ON a.parent_id=s.id WHERE a.root_id=?
) `

// AdmitAgent atomically persists a retained recursive agent and its initial
// queued prompt. Worker availability is intentionally not part of admission.
func (s *Store) AdmitAgent(ctx context.Context, admission AgentAdmission) (int64, error) {
	if admission.RootID == "" || admission.ParentAgentID == "" || admission.ChildAgentID == "" || admission.ParentAgentID == admission.ChildAgentID {
		return 0, errors.New("agent admission requires distinct root, parent, and child identities")
	}
	if admission.Name == "" {
		admission.Name = admission.ChildAgentID
	}
	var prompt preparedRuntimeValue
	if len(admission.Prompt.Data) > 0 {
		var err error
		prompt, err = s.prepareRuntimeValue(admission.Prompt, ContentGrant{
			RootID: admission.RootID, AgentID: admission.ChildAgentID, Scope: ContentGrantAgent,
		})
		if err != nil {
			return 0, err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	parent, err := loadAgentTx(ctx, tx, admission.RootID, admission.ParentAgentID)
	if err != nil {
		return 0, err
	}
	if isTerminalAgentStatus(parent.Status) {
		return 0, ErrAgentTerminal
	}
	if err := syncChildBudgetReservationsTx(ctx, tx, admission.RootID); err != nil {
		return 0, err
	}
	requested := make(map[BudgetKind]int64, len(admission.Budgets))
	for _, budget := range admission.Budgets {
		if budget.Kind == "" || budget.Limit < 0 {
			return 0, errors.New("child budgets require a kind and nonnegative limit")
		}
		if _, duplicate := requested[budget.Kind]; duplicate {
			return 0, errors.New("child budget kinds must be unique")
		}
		requested[budget.Kind] = budget.Limit
	}
	parentDepth, err := agentDepthTx(ctx, tx, admission.RootID, admission.ParentAgentID)
	if err != nil {
		return 0, err
	}
	childDepth := parentDepth + 1
	depthRows, err := loadBudgetRowsTx(ctx, tx, admission.RootID, admission.ParentAgentID, BudgetDepth)
	if err != nil {
		return 0, err
	}
	for _, row := range depthRows {
		if childDepth > row.limit {
			return 0, capability.ErrDenied
		}
	}
	activeChild := []capability.Reservation{{Kind: string(BudgetActiveChildren), Amount: 1}}
	if err := reserveCapabilityBudgets(ctx, tx, admission.RootID, admission.ParentAgentID, activeChild); err != nil {
		return 0, err
	}
	stamp := now()
	var duplicateName int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM agents WHERE root_id=? AND parent_id=? AND name=? AND status!='deleted'`,
		admission.RootID, admission.ParentAgentID, admission.Name).Scan(&duplicateName); err != nil {
		return 0, err
	}
	if duplicateName != 0 {
		return 0, fmt.Errorf("agent name %q already exists under this parent", admission.Name)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO agents(id,root_id,parent_id,name,model,provider,effort,cwd,status,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,'idle',?,?)`, admission.ChildAgentID, admission.RootID, admission.ParentAgentID,
		admission.Name, admission.Model, admission.Provider, admission.Effort, admission.CWD, stamp, stamp); err != nil {
		return 0, err
	}
	for kind, requestedLimit := range requested {
		rows, err := loadBudgetRowsTx(ctx, tx, admission.RootID, admission.ParentAgentID, kind)
		if err != nil {
			return 0, err
		}
		limit := requestedLimit
		for _, row := range rows {
			remaining, valid := budgetRemaining(row)
			if !valid {
				return 0, capability.ErrDenied
			}
			if remaining < limit {
				limit = remaining
			}
		}
		if kind == BudgetDepth && childDepth > limit {
			return 0, capability.ErrDenied
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO budgets(root_id,agent_id,kind,limit_value,updated_at) VALUES(?,?,?,?,?)`,
			admission.RootID, admission.ChildAgentID, kind, limit, stamp); err != nil {
			return 0, err
		}
	}
	for _, delegation := range admission.Capabilities {
		if delegation.AgentID != admission.ChildAgentID {
			return 0, capability.ErrDenied
		}
		if _, err := s.delegateCapabilityTx(ctx, tx, admission.RootID, admission.ParentAgentID, delegation); err != nil {
			return 0, err
		}
	}
	if len(admission.Prompt.Data) > 0 {
		if _, err := s.enqueueInboxTx(ctx, tx, InboxEnqueue{
			RootID: admission.RootID, AgentID: admission.ChildAgentID, Kind: "submit", Payload: admission.Prompt,
		}, prompt, "agent.prompt.queued", actorEvent{AgentID: admission.ChildAgentID, Status: "queued"}); err != nil {
			return 0, err
		}
	}
	if _, err := s.insertActorEventTx(ctx, tx, admission.RootID, "budget.active_child.reserved", actorEvent{
		AgentID: admission.ParentAgentID, BudgetKind: string(BudgetActiveChildren), Amount: 1,
	}, stamp); err != nil {
		return 0, err
	}
	if err := syncChildBudgetReservationsTx(ctx, tx, admission.RootID); err != nil {
		return 0, err
	}
	eventSeq, err := s.insertActorEventTx(ctx, tx, admission.RootID, "agent.admitted", actorEvent{
		AgentID: admission.ChildAgentID, Status: "queued",
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
	defer func() { _ = tx.Rollback() }()
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
		result.Siblings, err = loadAgentsTx(ctx, tx, `SELECT id,root_id,COALESCE(parent_id,''),name,model,provider,effort,cwd,status FROM agents WHERE root_id=? AND parent_id=? AND id<>? ORDER BY name,id`, rootID, caller.ParentID, callerAgentID)
		if err != nil {
			return AgentRelatives{}, err
		}
	}
	result.Children, err = loadAgentsTx(ctx, tx, `SELECT id,root_id,COALESCE(parent_id,''),name,model,provider,effort,cwd,status FROM agents WHERE root_id=? AND parent_id=? ORDER BY name,id`, rootID, callerAgentID)
	if err != nil {
		return AgentRelatives{}, err
	}
	if err := tx.Commit(); err != nil {
		return AgentRelatives{}, err
	}
	return result, nil
}

func (s *Store) TerminalizeSubtree(ctx context.Context, rootID, callerAgentID, targetAgentID, status string) (int64, error) {
	if rootID == "" || callerAgentID == "" || targetAgentID == "" || status != "stopped" && status != "deleted" {
		return 0, errors.New("subtree terminalization requires root, caller, target, and stopped or deleted status")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
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
	if err := s.cancelPendingPermissionsTx(ctx, tx, rootID, targetAgentID, "", "interrupted", "", status+" subtree"); err != nil {
		return 0, err
	}
	if err := s.settleInterruptedOperationReservations(ctx, tx, rootID, targetAgentID); err != nil {
		return 0, err
	}
	agentWhere := `status NOT IN ('failed','stopped','cancelled','interrupted','deleted','succeeded')`
	if status == "deleted" {
		agentWhere = `status!='deleted'`
	}
	if _, err := tx.ExecContext(ctx, subtreeCTE+`UPDATE agents SET status=?,updated_at=? WHERE root_id=? AND id IN (SELECT id FROM subtree) AND `+agentWhere,
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
	if err := syncChildBudgetReservationsTx(ctx, tx, rootID); err != nil {
		return 0, err
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
	err := tx.QueryRowContext(ctx, `SELECT id,root_id,COALESCE(parent_id,''),name,model,provider,effort,cwd,status FROM agents WHERE root_id=? AND id=?`, rootID, agentID).
		Scan(&agent.ID, &agent.RootID, &agent.ParentID, &agent.Name, &agent.Model, &agent.Provider, &agent.Effort, &agent.CWD, &agent.Status)
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
	defer func() { _ = rows.Close() }()
	var agents []RuntimeAgent
	for rows.Next() {
		var agent RuntimeAgent
		if err := rows.Scan(&agent.ID, &agent.RootID, &agent.ParentID, &agent.Name, &agent.Model, &agent.Provider, &agent.Effort, &agent.CWD, &agent.Status); err != nil {
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

func agentDepthTx(ctx context.Context, tx *sql.Tx, rootID, agentID string) (int64, error) {
	var depth int64
	err := tx.QueryRowContext(ctx, `WITH RECURSIVE ancestors(id,parent_id,depth) AS (
		SELECT id,parent_id,0 FROM agents WHERE root_id=? AND id=?
		UNION ALL
		SELECT a.id,a.parent_id,p.depth+1 FROM agents a JOIN ancestors p ON a.id=p.parent_id WHERE a.root_id=?
	) SELECT COALESCE(MAX(depth),-1) FROM ancestors`, rootID, agentID, rootID).Scan(&depth)
	if err != nil || depth < 0 {
		if err != nil {
			return 0, err
		}
		return 0, ErrAgentAccess
	}
	return depth, nil
}

func syncChildBudgetReservationsTx(ctx context.Context, tx *sql.Tx, rootID string) error {
	query := `SELECT root_id,agent_id,kind FROM budgets WHERE kind IN (?,?)`
	args := []any{BudgetActiveChildren, BudgetConcurrentChildTurns}
	if rootID != "" {
		query += ` AND root_id=?`
		args = append(args, rootID)
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	type budget struct {
		rootID, agentID string
		kind            BudgetKind
	}
	var budgets []budget
	for rows.Next() {
		var row budget
		if err := rows.Scan(&row.rootID, &row.agentID, &row.kind); err != nil {
			return err
		}
		budgets = append(budgets, row)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, budget := range budgets {
		var reserved int64
		if budget.kind == BudgetActiveChildren {
			status := `status IN ('queued','running','waiting','idle')`
			if budget.agentID == "" {
				err = tx.QueryRowContext(ctx, `SELECT count(*) FROM agents
					WHERE root_id=? AND parent_id IS NOT NULL AND `+status, budget.rootID).Scan(&reserved)
			} else {
				err = tx.QueryRowContext(ctx, `WITH RECURSIVE descendants(id) AS (
					SELECT id FROM agents WHERE root_id=? AND id=?
					UNION ALL
					SELECT a.id FROM agents a JOIN descendants d ON a.parent_id=d.id WHERE a.root_id=?
				) SELECT count(*) FROM agents WHERE root_id=? AND parent_id IN (SELECT id FROM descendants) AND `+status,
					budget.rootID, budget.agentID, budget.rootID, budget.rootID).Scan(&reserved)
			}
		} else if budget.agentID == "" {
			err = tx.QueryRowContext(ctx, `SELECT count(*) FROM turns t
				JOIN agents a ON a.root_id=t.root_id AND a.id=t.agent_id
				WHERE t.root_id=? AND a.parent_id IS NOT NULL AND t.status='running'`, budget.rootID).Scan(&reserved)
		} else {
			err = tx.QueryRowContext(ctx, `WITH RECURSIVE descendants(id) AS (
				SELECT id FROM agents WHERE root_id=? AND id=?
				UNION ALL
				SELECT a.id FROM agents a JOIN descendants d ON a.parent_id=d.id WHERE a.root_id=?
			) SELECT count(*) FROM turns
				WHERE root_id=? AND agent_id IN (SELECT id FROM descendants) AND agent_id!=? AND status='running'`,
				budget.rootID, budget.agentID, budget.rootID, budget.rootID, budget.agentID).Scan(&reserved)
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE budgets SET reserved_value=?,updated_at=? WHERE root_id=? AND agent_id=? AND kind=?`,
			reserved, now(), budget.rootID, budget.agentID, budget.kind); err != nil {
			return err
		}
	}
	return nil
}

func isTerminalAgentStatus(status string) bool {
	return status == "failed" || status == "stopped" || status == "cancelled" || status == "interrupted" || status == "deleted" || status == "succeeded"
}
