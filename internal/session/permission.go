package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/context-labs/whip/internal/capability"
)

type CapabilityDelegation struct {
	ID         string
	Issuer     capability.Reference
	AgentID    string
	Operations []string
	Scopes     []string
	Generation int64
	ExpiresAt  time.Time
}

type CapabilityRecord struct {
	ID            string
	RootID        string
	AgentID       string
	IssuerAgentID string
	Operations    []string
	Scopes        []string
	Generation    int64
	Status        string
	ExpiresAt     time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (s *Store) InspectCapability(ctx context.Context, rootID, callerAgentID, capabilityID string) (CapabilityRecord, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return CapabilityRecord{}, err
	}
	defer tx.Rollback()
	record, err := loadCapabilityRecordTx(ctx, tx, rootID, capabilityID)
	if err != nil {
		return CapabilityRecord{}, err
	}
	if _, err := loadAgentTx(ctx, tx, rootID, callerAgentID); err != nil {
		if errors.Is(err, ErrAgentAccess) {
			return CapabilityRecord{}, capability.ErrDenied
		}
		return CapabilityRecord{}, err
	}
	allowed, err := agentInSubtreeTx(ctx, tx, rootID, callerAgentID, record.AgentID)
	if err != nil {
		return CapabilityRecord{}, err
	}
	if !allowed {
		return CapabilityRecord{}, capability.ErrDenied
	}
	return record, tx.Commit()
}

func (s *Store) DelegateCapability(ctx context.Context, rootID, callerAgentID string, delegation CapabilityDelegation) (CapabilityRecord, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CapabilityRecord{}, err
	}
	defer tx.Rollback()
	record, err := s.delegateCapabilityTx(ctx, tx, rootID, callerAgentID, delegation)
	if err != nil {
		return CapabilityRecord{}, err
	}
	return record, tx.Commit()
}

func (s *Store) delegateCapabilityTx(ctx context.Context, tx *sql.Tx, rootID, callerAgentID string, delegation CapabilityDelegation) (CapabilityRecord, error) {
	if rootID == "" || callerAgentID == "" || delegation.ID == "" || delegation.Issuer.ID == "" || delegation.AgentID == "" || len(delegation.Operations) == 0 {
		return CapabilityRecord{}, capability.ErrDenied
	}
	caller, err := loadAgentTx(ctx, tx, rootID, callerAgentID)
	if err != nil {
		if errors.Is(err, ErrAgentAccess) {
			return CapabilityRecord{}, capability.ErrDenied
		}
		return CapabilityRecord{}, err
	}
	subject, err := loadAgentTx(ctx, tx, rootID, delegation.AgentID)
	if err != nil {
		if errors.Is(err, ErrAgentAccess) {
			return CapabilityRecord{}, capability.ErrDenied
		}
		return CapabilityRecord{}, err
	}
	if callerAgentID == delegation.AgentID || isTerminalAgentStatus(caller.Status) || isTerminalAgentStatus(subject.Status) {
		return CapabilityRecord{}, capability.ErrDenied
	}
	allowed, err := agentInSubtreeTx(ctx, tx, rootID, callerAgentID, delegation.AgentID)
	if err != nil {
		return CapabilityRecord{}, err
	}
	if !allowed {
		return CapabilityRecord{}, capability.ErrDenied
	}
	issuer, err := loadCapabilityRecordTx(ctx, tx, rootID, delegation.Issuer.ID)
	if err != nil {
		return CapabilityRecord{}, err
	}
	checkTime := time.Now()
	if issuer.AgentID != callerAgentID || issuer.Status != "active" || issuer.Generation != delegation.Issuer.Generation || (!issuer.ExpiresAt.IsZero() && !issuer.ExpiresAt.After(checkTime)) {
		return CapabilityRecord{}, capability.ErrDenied
	}
	generation := delegation.Generation
	if generation == 0 {
		generation = 1
	}
	if generation != 1 {
		return CapabilityRecord{}, capability.ErrDenied
	}
	seen := make(map[string]struct{}, len(delegation.Operations))
	hasShell, hasWriter := false, false
	for _, operation := range delegation.Operations {
		if operation == "" || !slices.Contains(issuer.Operations, operation) {
			return CapabilityRecord{}, capability.ErrDenied
		}
		if _, duplicate := seen[operation]; duplicate {
			return CapabilityRecord{}, capability.ErrDenied
		}
		seen[operation] = struct{}{}
		hasShell = hasShell || isShellOperation(operation)
		hasWriter = hasWriter || operation == "workspace.write"
	}
	var workspaceRoot string
	if err := tx.QueryRowContext(ctx, `SELECT cwd FROM sessions WHERE id=?`, rootID).Scan(&workspaceRoot); err != nil {
		return CapabilityRecord{}, err
	}
	workspace, err := s.workspaces.Open(workspaceRoot)
	if err != nil {
		return CapabilityRecord{}, err
	}
	scopes := make([]string, 0, len(delegation.Scopes))
	for _, scope := range delegation.Scopes {
		canonical, err := workspace.Resolve(scope)
		if err != nil || !scopeContains(issuer.Scopes, canonical) {
			return CapabilityRecord{}, capability.ErrDenied
		}
		if !slices.Contains(scopes, canonical) {
			scopes = append(scopes, canonical)
		}
	}
	if (hasShell && len(scopes) != 0) || (hasWriter && len(scopes) == 0) {
		return CapabilityRecord{}, capability.ErrDenied
	}
	expiresAt := delegation.ExpiresAt
	if (!expiresAt.IsZero() && !expiresAt.After(checkTime)) || (!issuer.ExpiresAt.IsZero() && (expiresAt.IsZero() || expiresAt.After(issuer.ExpiresAt))) {
		return CapabilityRecord{}, capability.ErrDenied
	}
	operationsJSON, err := json.Marshal(delegation.Operations)
	if err != nil {
		return CapabilityRecord{}, err
	}
	storedScopes := storedCapabilityScopes{Paths: scopes}
	if !expiresAt.IsZero() {
		storedScopes.ExpiresAt = expiresAt.UTC().Format(time.RFC3339Nano)
	}
	scopesJSON, err := json.Marshal(storedScopes)
	if err != nil {
		return CapabilityRecord{}, err
	}
	stamp := now()
	if _, err := tx.ExecContext(ctx, `INSERT INTO capabilities(id,root_id,agent_id,issuer_agent_id,operations,scopes,generation,status,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,'active',?,?)`, delegation.ID, rootID, delegation.AgentID, callerAgentID, operationsJSON, scopesJSON, generation, stamp, stamp); err != nil {
		return CapabilityRecord{}, err
	}
	if _, err := s.insertActorEventTx(ctx, tx, rootID, "capability.delegated", actorEvent{
		AgentID: delegation.AgentID, SenderAgentID: callerAgentID, CapabilityID: delegation.ID, Generation: generation, Status: "active",
	}, stamp); err != nil {
		return CapabilityRecord{}, err
	}
	return loadCapabilityRecordTx(ctx, tx, rootID, delegation.ID)
}

func (s *Store) RevokeCapabilityFor(ctx context.Context, rootID, callerAgentID, capabilityID string) (CapabilityRecord, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CapabilityRecord{}, err
	}
	defer tx.Rollback()
	record, err := loadCapabilityRecordTx(ctx, tx, rootID, capabilityID)
	if err != nil {
		return CapabilityRecord{}, err
	}
	caller, err := loadAgentTx(ctx, tx, rootID, callerAgentID)
	if err != nil {
		if errors.Is(err, ErrAgentAccess) {
			return CapabilityRecord{}, capability.ErrDenied
		}
		return CapabilityRecord{}, err
	}
	if isTerminalAgentStatus(caller.Status) || record.Status != "active" {
		return CapabilityRecord{}, capability.ErrDenied
	}
	allowed, err := agentInSubtreeTx(ctx, tx, rootID, callerAgentID, record.AgentID)
	if err != nil {
		return CapabilityRecord{}, err
	}
	if !allowed {
		return CapabilityRecord{}, capability.ErrDenied
	}
	stamp := now()
	result, err := tx.ExecContext(ctx, `UPDATE capabilities SET status='revoked',generation=generation+1,updated_at=?
		WHERE id=? AND root_id=? AND generation=? AND status='active'`, stamp, capabilityID, rootID, record.Generation)
	if err != nil {
		return CapabilityRecord{}, err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		if err != nil {
			return CapabilityRecord{}, err
		}
		return CapabilityRecord{}, capability.ErrDenied
	}
	if err := s.cancelPendingPermissionsTx(ctx, tx, rootID, "", capabilityID, "denied", callerAgentID, "capability revoked"); err != nil {
		return CapabilityRecord{}, err
	}
	if _, err := s.insertActorEventTx(ctx, tx, rootID, "capability.revoked", actorEvent{
		AgentID: record.AgentID, SenderAgentID: callerAgentID, CapabilityID: capabilityID, Generation: record.Generation + 1, Status: "revoked",
	}, stamp); err != nil {
		return CapabilityRecord{}, err
	}
	record, err = loadCapabilityRecordTx(ctx, tx, rootID, capabilityID)
	if err != nil {
		return CapabilityRecord{}, err
	}
	return record, tx.Commit()
}

func loadCapabilityRecordTx(ctx context.Context, tx *sql.Tx, rootID, capabilityID string) (CapabilityRecord, error) {
	var record CapabilityRecord
	var operationsJSON, scopesJSON []byte
	var createdAt, updatedAt string
	err := tx.QueryRowContext(ctx, `SELECT id,root_id,agent_id,issuer_agent_id,operations,scopes,generation,status,created_at,updated_at
		FROM capabilities WHERE id=? AND root_id=?`, capabilityID, rootID).Scan(
		&record.ID, &record.RootID, &record.AgentID, &record.IssuerAgentID, &operationsJSON, &scopesJSON,
		&record.Generation, &record.Status, &createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return CapabilityRecord{}, capability.ErrDenied
	}
	if err != nil {
		return CapabilityRecord{}, err
	}
	var scopes storedCapabilityScopes
	if err := json.Unmarshal(operationsJSON, &record.Operations); err != nil {
		return CapabilityRecord{}, err
	}
	if err := json.Unmarshal(scopesJSON, &scopes); err != nil {
		return CapabilityRecord{}, err
	}
	record.Scopes = scopes.Paths
	if scopes.ExpiresAt != "" {
		record.ExpiresAt, err = time.Parse(time.RFC3339Nano, scopes.ExpiresAt)
		if err != nil {
			return CapabilityRecord{}, err
		}
	}
	record.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return CapabilityRecord{}, err
	}
	record.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	return record, err
}

func (s *Store) cancelPendingPermissionsTx(ctx context.Context, tx *sql.Tx, rootID, targetAgentID, capabilityID, status, principal, reason string) error {
	query := `SELECT p.id,o.payload_inline,o.payload_ref FROM permission_requests p JOIN operations o
		ON o.root_id=p.root_id AND o.id=p.operation_id WHERE p.status='pending' AND o.status='waiting'`
	var args []any
	if targetAgentID != "" {
		query = subtreeCTE + `SELECT p.id,o.payload_inline,o.payload_ref FROM permission_requests p JOIN operations o
			ON o.root_id=p.root_id AND o.id=p.operation_id WHERE p.root_id=? AND p.agent_id IN (SELECT id FROM subtree)
			AND p.status='pending' AND o.status='waiting'`
		args = []any{rootID, targetAgentID, rootID, rootID}
	} else if rootID != "" {
		query += ` AND p.root_id=?`
		args = append(args, rootID)
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	type pendingPermission struct {
		id        string
		admission capability.Admission
	}
	var pending []pendingPermission
	for rows.Next() {
		var item pendingPermission
		var inline []byte
		var reference sql.NullString
		if err := rows.Scan(&item.id, &inline, &reference); err != nil {
			rows.Close()
			return err
		}
		payload, err := s.readRuntimeValueTx(ctx, tx, inline, reference)
		if err != nil {
			rows.Close()
			return err
		}
		if err := json.Unmarshal(payload, &item.admission); err != nil {
			rows.Close()
			return err
		}
		if capabilityID == "" || item.admission.Request.CapabilityID == capabilityID || item.admission.Request.WriterCapabilityID == capabilityID {
			pending = append(pending, item)
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range pending {
		if err := terminalizePermission(ctx, tx, item.admission, item.id, status, principal, reason); err != nil {
			return err
		}
	}
	return nil
}

func isShellOperation(operation string) bool {
	return operation == "bash" || operation == "browser_exec" || operation == "computer_exec" || operation == "workspace_process"
}
