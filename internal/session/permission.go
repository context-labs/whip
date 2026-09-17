package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/capability"
)

type CapabilityDelegation struct {
	ID           string
	Issuer       capability.Reference
	AgentID      string
	Operations   []string
	Scopes       []string
	InheritScope bool
	MCP          []capability.MCPSelector
	MCPAll       bool
	Generation   int64
	ExpiresAt    time.Time
}

type CapabilityRecord struct {
	ID                      string                   `json:"id"`
	RootID                  string                   `json:"root_id"`
	AgentID                 string                   `json:"agent_id"`
	IssuerAgentID           string                   `json:"issuer_agent_id"`
	Operations              []string                 `json:"operations"`
	Scopes                  []string                 `json:"scopes"`
	FileScope               string                   `json:"file_scope,omitempty"`
	FileIssuerID            string                   `json:"file_issuer_id,omitempty"`
	FileIssuerGeneration    int64                    `json:"file_issuer_generation,string,omitempty"`
	MCP                     []capability.MCPSelector `json:"mcp"`
	MCPAll                  bool                     `json:"mcp_all"`
	Browser                 *capability.BrowserScope `json:"browser,omitempty"`
	BrowserIssuerID         string                   `json:"browser_issuer_id,omitempty"`
	BrowserIssuerGeneration int64                    `json:"browser_issuer_generation,omitempty"`
	BrowserDelegationOnly   bool                     `json:"browser_delegation_only,omitempty"`
	Generation              int64                    `json:"generation,string"`
	Status                  string                   `json:"status"`
	ExpiresAt               time.Time                `json:"expires_at"`
	CreatedAt               time.Time                `json:"created_at"`
	UpdatedAt               time.Time                `json:"updated_at"`
}

func (s *Store) InspectCapability(ctx context.Context, rootID, callerAgentID, capabilityID string) (CapabilityRecord, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return CapabilityRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
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
	defer func() { _ = tx.Rollback() }()
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
	if issuer.Browser != nil {
		// Attachment transfers require the dedicated browser handoff path.
		return CapabilityRecord{}, capability.ErrDenied
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
	hasShell, hasWriter, hasMCP, hasTools := false, false, false, false
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
		hasMCP = hasMCP || operation == "mcp.call"
		hasTools = hasTools || isToolOperation(operation)
	}
	// Custom tool grants carry no scopes and never mix with other authority.
	if hasTools && (len(delegation.Operations) != len(seen) || len(delegation.Scopes) != 0 || hasShell || hasWriter || hasMCP) {
		return CapabilityRecord{}, capability.ErrDenied
	}
	if delegation.MCPAll || (!hasMCP && len(delegation.MCP) != 0) || (delegation.InheritScope && (hasMCP || hasShell || hasTools || len(delegation.Scopes) != 0)) {
		return CapabilityRecord{}, capability.ErrDenied
	}
	if hasMCP {
		if len(delegation.Operations) != 1 || len(delegation.Scopes) != 0 || !validMCPSelectors(delegation.MCP) {
			return CapabilityRecord{}, capability.ErrDenied
		}
		issuerScopes, err := loadMCPAuthorityTx(ctx, tx, rootID, callerAgentID, delegation.Issuer)
		if err != nil {
			return CapabilityRecord{}, err
		}
		for _, selector := range delegation.MCP {
			if !issuerScopes.MCPAll && !slices.Contains(issuerScopes.MCP, selector) {
				return CapabilityRecord{}, capability.ErrDenied
			}
		}
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
	var fileAuthority fileAccess
	if !hasMCP && !hasShell && !hasTools {
		fileAuthority, err = loadFileAccessTx(ctx, tx, rootID, callerAgentID, delegation.Issuer)
		if err != nil {
			return CapabilityRecord{}, err
		}
	}
	for _, scope := range delegation.Scopes {
		canonical, err := workspace.Canonicalize(scope)
		if err != nil || !fileAuthority.contains(canonical) {
			return CapabilityRecord{}, capability.ErrDenied
		}
		if !slices.Contains(scopes, canonical) {
			scopes = append(scopes, canonical)
		}
	}
	if (hasShell && len(scopes) != 0) || (hasWriter && len(scopes) == 0 && !delegation.InheritScope) {
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
	if !hasMCP && !hasShell && !hasTools {
		storedScopes.FileIssuerID = delegation.Issuer.ID
		storedScopes.FileIssuerGeneration = delegation.Issuer.Generation
		if delegation.InheritScope {
			storedScopes.FileScope = "inherit"
		}
	}
	if hasMCP {
		storedScopes.MCP = delegation.MCP
		storedScopes.MCPIssuerID = delegation.Issuer.ID
		storedScopes.MCPIssuerGeneration = delegation.Issuer.Generation
	}
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
	defer func() { _ = tx.Rollback() }()
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
	if record.Browser != nil {
		if _, err := tx.ExecContext(ctx, `WITH RECURSIVE descendants(id) AS (
			SELECT id FROM capabilities WHERE root_id=? AND json_extract(scopes,'$.browser_issuer_id')=?
			UNION SELECT c.id FROM capabilities c JOIN descendants d
			ON json_extract(c.scopes,'$.browser_issuer_id')=d.id WHERE c.root_id=?
		) UPDATE capabilities SET status='revoked',generation=generation+1,updated_at=?
		WHERE id IN (SELECT id FROM descendants) AND root_id=? AND status='active'`,
			rootID, capabilityID, rootID, stamp, rootID); err != nil {
			return CapabilityRecord{}, err
		}
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
	record.FileScope = scopes.FileScope
	record.FileIssuerID = scopes.FileIssuerID
	record.FileIssuerGeneration = scopes.FileIssuerGeneration
	record.MCP = scopes.MCP
	record.MCPAll = scopes.MCPAll
	record.Browser = scopes.Browser
	record.BrowserIssuerID = scopes.BrowserIssuerID
	record.BrowserIssuerGeneration = scopes.BrowserIssuerGeneration
	record.BrowserDelegationOnly = scopes.BrowserDelegationOnly
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
	defer func() { _ = rows.Close() }()
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
			return err
		}
		payload, err := s.readRuntimeValueTx(ctx, tx, inline, reference)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(payload, &item.admission); err != nil {
			return err
		}
		matches := capabilityID == "" || item.admission.Request.CapabilityID == capabilityID || item.admission.Request.WriterCapabilityID == capabilityID
		if !matches && item.admission.Request.Operation == "mcp.call" {
			// Revoking any ancestor invalidates the pending descendant grant.
			// Revalidation also closes already-invalid chains while releasing their
			// reservations in this same revocation transaction.
			_, err := loadMCPAuthorityTx(ctx, tx, item.admission.Request.RootID, item.admission.Request.AgentID, capability.Reference{
				ID: item.admission.Request.CapabilityID, Generation: item.admission.Request.CapabilityGeneration,
			})
			if err != nil && !errors.Is(err, capability.ErrDenied) {
				return err
			}
			matches = errors.Is(err, capability.ErrDenied)
		}
		if !matches && (browserOperation(item.admission.Request.Operation) ||
			item.admission.CanonicalPath != "" || item.admission.Mutation == capability.MutationWorkspace) {
			err := validateCapabilityAdmission(ctx, tx, item.admission)
			if err != nil && !errors.Is(err, capability.ErrDenied) {
				return err
			}
			matches = errors.Is(err, capability.ErrDenied)
		}
		if matches {
			pending = append(pending, item)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range pending {
		if err := s.terminalizePermission(ctx, tx, item.admission, item.id, status, principal, reason); err != nil {
			return err
		}
	}
	return nil
}

func isShellOperation(operation string) bool {
	return operation == "bash" || operation == "shell_start" || operation == "browser_exec" ||
		operation == "computer_exec" || operation == "workspace_process" || browserOperation(operation)
}

// isToolOperation reports a custom tool operation, tools.<name>.
func isToolOperation(operation string) bool {
	return strings.HasPrefix(operation, "tools.")
}
